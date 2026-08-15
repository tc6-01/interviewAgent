package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"interview-agent/internal/domain"
)

type GraphRuntime interface {
	Checks(context.Context) []domain.CheckResult
}

type Repository interface {
	CreateSession(context.Context, Snapshot) error
	SaveSession(context.Context, Snapshot) error
	LoadSession(context.Context, string) (Snapshot, error)
	FailActiveSessions(context.Context, string) error
}

type Option func(*Manager)

func WithEngine(engine Engine) Option { return func(m *Manager) { m.engine = engine } }
func WithIdleTimeout(timeout time.Duration) Option {
	return func(m *Manager) { m.idleTimeout = timeout }
}
func WithEventBuffer(size int) Option       { return func(m *Manager) { m.eventBuffer = size } }
func WithLogger(logger *slog.Logger) Option { return func(m *Manager) { m.logger = logger } }

type archivedSession struct {
	snapshot Snapshot
	events   []Event
}

// Manager owns one actor per active interview and a bounded process-local
// replay archive for terminal interviews.
type Manager struct {
	graph       GraphRuntime
	repository  Repository
	engine      Engine
	idleTimeout time.Duration
	eventBuffer int
	logger      *slog.Logger

	mu       sync.RWMutex
	actors   map[string]*actor
	archived map[string]archivedSession
	closed   bool
	wg       sync.WaitGroup
}

func NewManager(graph GraphRuntime, repository Repository, options ...Option) (*Manager, error) {
	if graph == nil {
		return nil, fmt.Errorf("session: graph runtime is required")
	}
	if repository == nil {
		return nil, fmt.Errorf("session: repository is required")
	}
	m := &Manager{
		graph: graph, repository: repository, engine: DeterministicEngine{},
		idleTimeout: 30 * time.Minute, eventBuffer: 256, logger: slog.Default(),
		actors: make(map[string]*actor), archived: make(map[string]archivedSession),
	}
	for _, option := range options {
		option(m)
	}
	if m.engine == nil || m.idleTimeout <= 0 || m.eventBuffer <= 0 {
		return nil, fmt.Errorf("session: engine, positive idle timeout and event buffer are required")
	}
	if err := repository.FailActiveSessions(context.Background(), "server_restart"); err != nil {
		return nil, fmt.Errorf("session: clean interrupted interviews: %w", err)
	}
	return m, nil
}

func (m *Manager) Checks(ctx context.Context) []domain.CheckResult {
	if m == nil || m.graph == nil {
		return []domain.CheckResult{{Name: "session_assembly", Err: fmt.Errorf("session: manager is not initialized")}}
	}
	checks := m.graph.Checks(ctx)
	return append(checks, domain.CheckResult{Name: "session_assembly"})
}

func (m *Manager) Create(ctx context.Context, input CreateInput) (Snapshot, error) {
	if input.SubjectID == "" {
		return Snapshot{}, fmt.Errorf("session: subject is required")
	}
	if input.QuestionCount == 0 {
		input.QuestionCount = 15
	}
	if input.QuestionCount != 15 {
		return Snapshot{}, fmt.Errorf("session: question_count must be 15")
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Snapshot{}, fmt.Errorf("session: manager is closed")
	}
	for _, active := range m.actors {
		if active.subjectID == input.SubjectID {
			existing := active.initial.InterviewID
			m.mu.Unlock()
			return Snapshot{}, &ConflictError{Code: "interview_in_progress", Message: "已有进行中的面试", Details: map[string]any{"interview_id": existing}}
		}
	}
	now := time.Now().UTC()
	snapshot := Snapshot{
		InterviewID: "int_" + uuid.NewString(), SubjectID: input.SubjectID,
		Status: StatusPreparing, Stage: "jd_analysis", Progress: Progress{Total: input.QuestionCount},
		QAHistory: []QARecord{}, ReportStatus: ArtifactNotStarted, ReviewPlanStatus: ArtifactNotStarted,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := m.repository.CreateSession(ctx, snapshot); err != nil {
		m.mu.Unlock()
		return Snapshot{}, err
	}
	actorCtx, cancel := context.WithCancel(context.Background())
	a := newActor(actorCtx, cancel, input, snapshot, m.repository, m.engine, m.idleTimeout, m.eventBuffer, m.logger, m.actorDone)
	m.actors[snapshot.InterviewID] = a
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		a.run()
	}()
	return snapshot, nil
}

func (m *Manager) Snapshot(ctx context.Context, subjectID, interviewID string) (Snapshot, error) {
	if a := m.active(interviewID); a != nil {
		result, err := a.snapshot(ctx)
		if err != nil {
			return Snapshot{}, err
		}
		if result.SubjectID != subjectID {
			return Snapshot{}, NotFoundError{}
		}
		return result, nil
	}
	m.mu.RLock()
	archived, ok := m.archived[interviewID]
	m.mu.RUnlock()
	if ok {
		if archived.snapshot.SubjectID != subjectID {
			return Snapshot{}, NotFoundError{}
		}
		return archived.snapshot, nil
	}
	result, err := m.repository.LoadSession(ctx, interviewID)
	if err != nil {
		return Snapshot{}, err
	}
	if result.SubjectID != subjectID {
		return Snapshot{}, NotFoundError{}
	}
	return result, nil
}

func (m *Manager) Answer(ctx context.Context, subjectID, interviewID string, request AnswerRequest) error {
	a := m.active(interviewID)
	if a == nil {
		snapshot, err := m.Snapshot(ctx, subjectID, interviewID)
		if err != nil {
			return err
		}
		return finishedConflict(snapshot)
	}
	if a.subjectID != subjectID {
		return NotFoundError{}
	}
	return a.answer(ctx, request)
}

func (m *Manager) Quit(ctx context.Context, subjectID, interviewID, reason string) error {
	a := m.active(interviewID)
	if a == nil {
		snapshot, err := m.Snapshot(ctx, subjectID, interviewID)
		if err != nil {
			return err
		}
		return finishedConflict(snapshot)
	}
	if a.subjectID != subjectID {
		return NotFoundError{}
	}
	if reason == "" {
		reason = "user_requested"
	}
	return a.quit(ctx, reason)
}

func (m *Manager) Subscribe(ctx context.Context, subjectID, interviewID string, lastEventID int64) ([]Event, <-chan Event, func(), error) {
	if a := m.active(interviewID); a != nil {
		if a.subjectID != subjectID {
			return nil, nil, nil, NotFoundError{}
		}
		return a.subscribe(ctx, lastEventID)
	}
	m.mu.RLock()
	archived, ok := m.archived[interviewID]
	m.mu.RUnlock()
	if !ok {
		snapshot, err := m.Snapshot(ctx, subjectID, interviewID)
		if err != nil {
			return nil, nil, nil, err
		}
		if !snapshot.Status.Terminal() {
			return nil, nil, nil, fmt.Errorf("session: active interview actor unavailable")
		}
		closed := make(chan Event)
		close(closed)
		return nil, closed, func() {}, nil
	}
	if archived.snapshot.SubjectID != subjectID {
		return nil, nil, nil, NotFoundError{}
	}
	closed := make(chan Event)
	close(closed)
	return eventsAfter(archived.events, lastEventID), closed, func() {}, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	actors := make([]*actor, 0, len(m.actors))
	for _, a := range m.actors {
		actors = append(actors, a)
	}
	m.mu.Unlock()
	for _, a := range actors {
		a.cancel()
	}
	m.wg.Wait()
	return nil
}

func (m *Manager) active(interviewID string) *actor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.actors[interviewID]
}

func (m *Manager) actorDone(a *actor, snapshot Snapshot, events []Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current := m.actors[snapshot.InterviewID]; current == a {
		delete(m.actors, snapshot.InterviewID)
	}
	m.archived[snapshot.InterviewID] = archivedSession{snapshot: snapshot, events: events}
}

func finishedConflict(snapshot Snapshot) error {
	return &ConflictError{Code: "interview_finished", Message: "面试已结束", Details: map[string]any{"status": snapshot.Status}}
}

func eventsAfter(events []Event, lastID int64) []Event {
	result := make([]Event, 0, len(events))
	for _, event := range events {
		if !event.Authoritative() || event.ID > lastID {
			result = append(result, event)
		}
	}
	return result
}

func IsNotFound(err error) bool {
	var target NotFoundError
	return errors.As(err, &target)
}
