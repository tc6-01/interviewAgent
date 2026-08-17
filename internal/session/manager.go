package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	CreateDirection(context.Context, Direction) error
	GetDirection(context.Context, string, string) (Direction, error)
	UpdateDirection(context.Context, string, string, DirectionPatch) (Direction, error)
	ConfirmDirection(context.Context, string, string, int) (Direction, error)
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
	retries  map[string]bool
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
		actors: make(map[string]*actor), archived: make(map[string]archivedSession), retries: make(map[string]bool),
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

func (m *Manager) GenerateDirection(ctx context.Context, subjectID, jdText, resumeText string) (Direction, error) {
	if strings.TrimSpace(subjectID) == "" {
		return Direction{}, fmt.Errorf("session: subject is required")
	}
	started := time.Now()
	direction, err := m.engine.GenerateDirection(ctx, subjectID, jdText, resumeText)
	if err != nil {
		return Direction{}, err
	}
	now := time.Now().UTC()
	if direction.ID == "" {
		direction.ID = "dir_" + uuid.NewString()
	}
	direction.SubjectID = subjectID
	direction.Version = 1
	direction.Status = DirectionDraft
	direction.SourceSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(jdText+"\x00"+resumeText)))
	direction.CreatedAt = now
	direction.UpdatedAt = now
	if err := validateDirection(direction); err != nil {
		return Direction{}, err
	}
	if err := m.repository.CreateDirection(ctx, direction); err != nil {
		return Direction{}, err
	}
	m.logger.Info("interview direction generated", "direction_id", direction.ID, "latency_ms", time.Since(started).Milliseconds())
	return direction, nil
}

func (m *Manager) UpdateDirection(ctx context.Context, subjectID, directionID string, patch DirectionPatch) (Direction, error) {
	current, err := m.repository.GetDirection(ctx, subjectID, directionID)
	if err != nil {
		return Direction{}, err
	}
	if strings.TrimSpace(patch.JDAnalysis.Position) == "" {
		patch.JDAnalysis = current.JDAnalysis
	}
	if patch.ResumeMatch.SkillMatch == nil {
		patch.ResumeMatch = current.ResumeMatch
	}
	direction := Direction{
		ID: directionID, SubjectID: subjectID, Version: patch.ExpectedVersion, Status: DirectionDraft,
		Position: patch.Position, ExperienceLevel: patch.ExperienceLevel, FocusAreas: patch.FocusAreas,
		MatchedSkills: patch.MatchedSkills, Gaps: patch.Gaps, JDAnalysis: patch.JDAnalysis, ResumeMatch: patch.ResumeMatch,
	}
	if err := validateDirection(direction); err != nil {
		return Direction{}, err
	}
	return m.repository.UpdateDirection(ctx, subjectID, directionID, patch)
}

func (m *Manager) ConfirmDirection(ctx context.Context, subjectID, directionID string, expectedVersion int) (Direction, error) {
	return m.repository.ConfirmDirection(ctx, subjectID, directionID, expectedVersion)
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
	if input.DirectionID != "" {
		direction, err := m.repository.GetDirection(ctx, input.SubjectID, input.DirectionID)
		if err != nil {
			return Snapshot{}, err
		}
		if direction.Status != DirectionConfirmed {
			return Snapshot{}, &ConflictError{Code: "direction_not_confirmed", Message: "面试方向尚未确认"}
		}
		input.Direction = &direction
	} else {
		direction, err := m.GenerateDirection(ctx, input.SubjectID, input.JDText, input.ResumeText)
		if err != nil {
			return Snapshot{}, err
		}
		direction, err = m.ConfirmDirection(ctx, input.SubjectID, direction.ID, direction.Version)
		if err != nil {
			return Snapshot{}, err
		}
		input.DirectionID = direction.ID
		input.Direction = &direction
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
		QAHistory: []QARecord{}, Direction: input.Direction, ReportStatus: ArtifactNotStarted, ReviewPlanStatus: ArtifactNotStarted,
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

func (m *Manager) Report(ctx context.Context, subjectID, interviewID string) (Artifact, error) {
	snapshot, err := m.Snapshot(ctx, subjectID, interviewID)
	if err != nil {
		return Artifact{}, err
	}
	if snapshot.ReportStatus != ArtifactReady || len(snapshot.Report) == 0 {
		return Artifact{}, &ConflictError{Code: "report_not_ready", Message: "评估报告尚未就绪"}
	}
	return Artifact{Markdown: snapshot.ReportMarkdown, Value: snapshot.Report}, nil
}

func (m *Manager) ReviewPlan(ctx context.Context, subjectID, interviewID string) (Artifact, error) {
	snapshot, err := m.Snapshot(ctx, subjectID, interviewID)
	if err != nil {
		return Artifact{}, err
	}
	if snapshot.ReviewPlanStatus != ArtifactReady || len(snapshot.ReviewPlan) == 0 {
		return Artifact{}, &ConflictError{Code: "review_plan_not_ready", Message: "复习计划尚未就绪"}
	}
	return Artifact{Markdown: snapshot.ReviewPlanMarkdown, Value: snapshot.ReviewPlan}, nil
}

func (m *Manager) RetryReport(ctx context.Context, subjectID, interviewID string) error {
	snapshot, err := m.Snapshot(ctx, subjectID, interviewID)
	if err != nil {
		return err
	}
	key := "report:" + interviewID
	m.mu.Lock()
	if m.retries[key] || snapshot.ReportStatus == ArtifactGenerating {
		m.mu.Unlock()
		return &ConflictError{Code: "report_retry_in_progress", Message: "评估报告正在重试"}
	}
	if snapshot.ReportStatus != ArtifactFailed || len(snapshot.QAHistory) == 0 {
		m.mu.Unlock()
		return &ConflictError{Code: "report_retry_not_allowed", Message: "当前状态不能重试评估报告"}
	}
	m.retries[key] = true
	snapshot.ReportStatus = ArtifactGenerating
	snapshot.UpdatedAt = time.Now().UTC()
	if archived, ok := m.archived[interviewID]; ok {
		archived.snapshot = snapshot
		m.archived[interviewID] = archived
	}
	m.mu.Unlock()
	if err := m.repository.SaveSession(ctx, snapshot); err != nil {
		m.mu.Lock()
		delete(m.retries, key)
		m.mu.Unlock()
		return err
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		report, reportErr := m.engine.Report(context.Background(), cloneSnapshot(snapshot))
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.retries, key)
		if reportErr != nil {
			snapshot.ReportStatus = ArtifactFailed
		} else {
			snapshot.Report = report.Value
			snapshot.ReportMarkdown = report.Markdown
			snapshot.ReportStatus = ArtifactReady
			snapshot.ReportReady = true
			if snapshot.ReviewPlanStatus == ArtifactNotStarted {
				snapshot.ReviewPlanStatus = ArtifactFailed
			}
		}
		snapshot.UpdatedAt = time.Now().UTC()
		if archived, ok := m.archived[interviewID]; ok {
			archived.snapshot = snapshot
			m.archived[interviewID] = archived
		}
		persistCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.repository.SaveSession(persistCtx, snapshot); err != nil {
			m.logger.Error("persist report retry", "interview_id", interviewID, "error_type", "storage")
		}
	}()
	return nil
}

func (m *Manager) RetryReviewPlan(ctx context.Context, subjectID, interviewID string) error {
	snapshot, err := m.Snapshot(ctx, subjectID, interviewID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	key := "review_plan:" + interviewID
	if m.retries[key] || snapshot.ReviewPlanStatus == ArtifactGenerating {
		m.mu.Unlock()
		return &ConflictError{Code: "review_plan_retry_in_progress", Message: "复习计划正在重试"}
	}
	if snapshot.ReportStatus != ArtifactReady {
		m.mu.Unlock()
		return &ConflictError{Code: "review_plan_prerequisites_not_ready", Message: "评估报告尚未就绪"}
	}
	if snapshot.ReviewPlanStatus != ArtifactFailed {
		m.mu.Unlock()
		return &ConflictError{Code: "review_plan_retry_not_allowed", Message: "当前状态不能重试复习计划"}
	}
	m.retries[key] = true
	snapshot.ReviewPlanStatus = ArtifactGenerating
	snapshot.UpdatedAt = time.Now().UTC()
	if archived, ok := m.archived[interviewID]; ok {
		archived.snapshot = snapshot
		m.archived[interviewID] = archived
	}
	m.mu.Unlock()
	if err := m.repository.SaveSession(ctx, snapshot); err != nil {
		m.mu.Lock()
		delete(m.retries, key)
		m.mu.Unlock()
		return err
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		plan, planErr := m.engine.ReviewPlan(context.Background(), cloneSnapshot(snapshot))
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.retries, key)
		if planErr != nil {
			snapshot.ReviewPlanStatus = ArtifactFailed
		} else {
			snapshot.ReviewPlan = plan.Value
			snapshot.ReviewPlanMarkdown = plan.Markdown
			snapshot.ReviewPlanStatus = ArtifactReady
			snapshot.ReviewPlanReady = true
		}
		snapshot.UpdatedAt = time.Now().UTC()
		if archived, ok := m.archived[interviewID]; ok {
			archived.snapshot = snapshot
			m.archived[interviewID] = archived
		}
		persistCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.repository.SaveSession(persistCtx, snapshot); err != nil {
			m.logger.Error("persist review plan retry", "interview_id", interviewID, "error_type", "storage")
		}
	}()
	return nil
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

func validateDirection(direction Direction) error {
	if strings.TrimSpace(direction.Position) == "" || strings.TrimSpace(direction.ExperienceLevel) == "" {
		return fmt.Errorf("session: direction position and experience_level are required")
	}
	if len(direction.FocusAreas) == 0 || len(direction.MatchedSkills) == 0 || direction.Gaps == nil {
		return fmt.Errorf("session: direction focus_areas, matched_skills and gaps are required")
	}
	if strings.TrimSpace(direction.JDAnalysis.Position) == "" || direction.JDAnalysis.RequiredSkills == nil || direction.JDAnalysis.Responsibilities == nil || direction.JDAnalysis.KeyTopics == nil {
		return fmt.Errorf("session: jd_analysis is incomplete")
	}
	if direction.ResumeMatch.SkillMatch == nil || direction.ResumeMatch.Strengths == nil || direction.ResumeMatch.Weaknesses == nil || direction.ResumeMatch.FocusAreas == nil || direction.ResumeMatch.ResumeGaps == nil {
		return fmt.Errorf("session: resume_match_result is incomplete")
	}
	for _, skill := range direction.ResumeMatch.SkillMatch {
		if skill.Matched && strings.TrimSpace(skill.Evidence) == "" {
			return fmt.Errorf("session: matched skill evidence is required")
		}
	}
	if direction.Version <= 0 {
		return fmt.Errorf("session: direction version must be positive")
	}
	return nil
}
