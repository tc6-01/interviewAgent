package session

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"interview-agent/internal/domain"
)

type fakeGraph struct{}

func (fakeGraph) Checks(context.Context) []domain.CheckResult { return nil }

type memoryRepository struct {
	mu         sync.Mutex
	sessions   map[string]Snapshot
	directions map[string]Direction
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{sessions: make(map[string]Snapshot), directions: make(map[string]Direction)}
}

func (r *memoryRepository) CreateDirection(_ context.Context, direction Direction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.directions[direction.ID] = direction
	return nil
}

func (r *memoryRepository) GetDirection(_ context.Context, subjectID, directionID string) (Direction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	direction, ok := r.directions[directionID]
	if !ok || direction.SubjectID != subjectID {
		return Direction{}, NotFoundError{}
	}
	return direction, nil
}

func (r *memoryRepository) UpdateDirection(_ context.Context, subjectID, directionID string, patch DirectionPatch) (Direction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	direction, ok := r.directions[directionID]
	if !ok || direction.SubjectID != subjectID {
		return Direction{}, NotFoundError{}
	}
	if direction.Version != patch.ExpectedVersion || direction.Status != DirectionDraft {
		return Direction{}, &ConflictError{Code: "direction_version_conflict", Message: "conflict"}
	}
	direction.Version++
	direction.Position, direction.ExperienceLevel = patch.Position, patch.ExperienceLevel
	direction.FocusAreas, direction.MatchedSkills, direction.Gaps = patch.FocusAreas, patch.MatchedSkills, patch.Gaps
	direction.JDAnalysis, direction.ResumeMatch = patch.JDAnalysis, patch.ResumeMatch
	r.directions[directionID] = direction
	return direction, nil
}

func (r *memoryRepository) ConfirmDirection(_ context.Context, subjectID, directionID string, expectedVersion int) (Direction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	direction, ok := r.directions[directionID]
	if !ok || direction.SubjectID != subjectID {
		return Direction{}, NotFoundError{}
	}
	if direction.Version != expectedVersion || direction.Status != DirectionDraft {
		return Direction{}, &ConflictError{Code: "direction_confirm_conflict", Message: "conflict"}
	}
	now := time.Now().UTC()
	direction.Status, direction.ConfirmedAt = DirectionConfirmed, &now
	r.directions[directionID] = direction
	return direction, nil
}

func (r *memoryRepository) CreateSession(_ context.Context, snapshot Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sessions[snapshot.InterviewID]; exists {
		return errors.New("duplicate interview")
	}
	r.sessions[snapshot.InterviewID] = cloneSnapshot(snapshot)
	return nil
}

func (r *memoryRepository) SaveSession(_ context.Context, snapshot Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[snapshot.InterviewID] = cloneSnapshot(snapshot)
	return nil
}

func (r *memoryRepository) LoadSession(_ context.Context, interviewID string) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, ok := r.sessions[interviewID]
	if !ok {
		return Snapshot{}, NotFoundError{}
	}
	return cloneSnapshot(snapshot), nil
}

func (r *memoryRepository) FailActiveSessions(_ context.Context, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, snapshot := range r.sessions {
		if snapshot.Status.Terminal() {
			continue
		}
		snapshot.Status = StatusFailed
		snapshot.Stage = "failed"
		snapshot.AwaitingAnswer = nil
		snapshot.EndedReason = &reason
		r.sessions[id] = snapshot
	}
	return nil
}

func TestManagerRejectsEditedDirectionEvidenceOutsideVerifiedResumeQuotes(t *testing.T) {
	repository := newMemoryRepository()
	manager := newTestManager(t, repository, time.Minute)
	direction, err := manager.GenerateDirection(context.Background(), "subject-direction", "backend jd", "backend development")
	if err != nil {
		t.Fatal(err)
	}

	invalidMatch := direction.ResumeMatch
	invalidMatch.SkillMatch = append([]SkillMatch(nil), direction.ResumeMatch.SkillMatch...)
	invalidMatch.SkillMatch[0].Evidence = "invented Kubernetes platform ownership"
	patch := DirectionPatch{
		ExpectedVersion: direction.Version,
		Position:        direction.Position, ExperienceLevel: direction.ExperienceLevel,
		FocusAreas: direction.FocusAreas, MatchedSkills: direction.MatchedSkills, Gaps: direction.Gaps,
		JDAnalysis: direction.JDAnalysis, ResumeMatch: invalidMatch,
	}
	if _, err := manager.UpdateDirection(context.Background(), direction.SubjectID, direction.ID, patch); err == nil || !strings.Contains(err.Error(), "verified resume quote") {
		t.Fatalf("UpdateDirection() error = %v, want verified evidence rejection", err)
	}

	patch.ResumeMatch = direction.ResumeMatch
	updated, err := manager.UpdateDirection(context.Background(), direction.SubjectID, direction.ID, patch)
	if err != nil {
		t.Fatalf("UpdateDirection() with original evidence error = %v", err)
	}
	if updated.Version != direction.Version+1 || updated.ResumeMatch.SkillMatch[0].Evidence != direction.ResumeMatch.SkillMatch[0].Evidence {
		t.Fatalf("updated direction = %#v", updated)
	}
}

func TestManagerCompletesFifteenQuestionsAndReplaysAuthoritativeEvents(t *testing.T) {
	manager := newTestManager(t, newMemoryRepository(), time.Minute)
	snapshot, err := manager.Create(context.Background(), CreateInput{SubjectID: "subject-a", QuestionCount: 15})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	snapshot = waitForSnapshot(t, manager, "subject-a", snapshot.InterviewID, func(s Snapshot) bool { return s.AwaitingAnswer != nil })

	lastID := snapshot.LastEventID
	_, live, cancel, err := manager.Subscribe(context.Background(), "subject-a", snapshot.InterviewID, lastID)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	firstPrompt := snapshot.AwaitingAnswer.PromptID
	if err := manager.Answer(context.Background(), "subject-a", snapshot.InterviewID, AnswerRequest{PromptID: firstPrompt, Text: "first answer"}); err != nil {
		t.Fatalf("Answer() error = %v", err)
	}
	var sawDelta, sawFinal bool
	deadline := time.After(time.Second)
	for !sawFinal {
		select {
		case event := <-live:
			if event.Type == "question_delta" {
				sawDelta = true
				if event.ID != 0 {
					t.Fatalf("question_delta id = %d, want 0", event.ID)
				}
			}
			if event.Type == "question" {
				sawFinal = true
				if event.ID == 0 {
					t.Fatal("final question must be authoritative")
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for next question events")
		}
	}
	if !sawDelta {
		t.Fatal("live stream did not receive transient question_delta")
	}
	cancel()

	replay, _, replayCancel, err := manager.Subscribe(context.Background(), "subject-a", snapshot.InterviewID, lastID)
	if err != nil {
		t.Fatalf("reconnect Subscribe() error = %v", err)
	}
	replayCancel()
	for _, event := range replay {
		if event.Type == "question_delta" {
			t.Fatal("question_delta must not be replayed")
		}
		if event.ID <= lastID {
			t.Fatalf("replayed event id = %d, want > %d", event.ID, lastID)
		}
	}

	if err := manager.Answer(context.Background(), "subject-a", snapshot.InterviewID, AnswerRequest{PromptID: firstPrompt, Text: "duplicate"}); conflictCode(err) != "answer_already_submitted" {
		t.Fatalf("duplicate answer error = %v", err)
	}

	for question := 2; question <= 15; question++ {
		snapshot = waitForSnapshot(t, manager, "subject-a", snapshot.InterviewID, func(s Snapshot) bool {
			return s.AwaitingAnswer != nil && s.AwaitingAnswer.Number == question
		})
		if err := manager.Answer(context.Background(), "subject-a", snapshot.InterviewID, AnswerRequest{PromptID: snapshot.AwaitingAnswer.PromptID, Text: "answer"}); err != nil {
			t.Fatalf("answer question %d: %v", question, err)
		}
	}
	final := waitForSnapshot(t, manager, "subject-a", snapshot.InterviewID, func(s Snapshot) bool { return s.Status == StatusCompleted })
	if final.Progress.Answered != 15 || len(final.QAHistory) != 15 {
		t.Fatalf("final progress/history = %d/%d, want 15/15", final.Progress.Answered, len(final.QAHistory))
	}
	if !final.ReportReady || !final.ReviewPlanReady {
		t.Fatalf("final artifacts not ready: report=%v plan=%v", final.ReportReady, final.ReviewPlanReady)
	}
	persisted, _ := manager.repository.LoadSession(context.Background(), final.InterviewID)
	if persisted.Status != StatusCompleted || persisted.LastEventID != final.LastEventID {
		t.Fatalf("persisted snapshot = %#v, want final event %d", persisted, final.LastEventID)
	}
}

func TestManagerRejectsWrongPromptAndCrossSubjectAccess(t *testing.T) {
	manager := newTestManager(t, newMemoryRepository(), time.Minute)
	snapshot, err := manager.Create(context.Background(), CreateInput{SubjectID: "owner", QuestionCount: 15})
	if err != nil {
		t.Fatal(err)
	}
	snapshot = waitForSnapshot(t, manager, "owner", snapshot.InterviewID, func(s Snapshot) bool { return s.AwaitingAnswer != nil })
	if err := manager.Answer(context.Background(), "owner", snapshot.InterviewID, AnswerRequest{PromptID: "wrong", Text: "answer"}); conflictCode(err) != "prompt_mismatch" {
		t.Fatalf("wrong prompt error = %v", err)
	}
	if _, err := manager.Snapshot(context.Background(), "other", snapshot.InterviewID); !IsNotFound(err) {
		t.Fatalf("cross-subject Snapshot() error = %v, want not found", err)
	}
}

func TestManagerQuitAndIdleTimeoutProduceObservableTerminalState(t *testing.T) {
	manager := newTestManager(t, newMemoryRepository(), 30*time.Millisecond)
	quitSession, _ := manager.Create(context.Background(), CreateInput{SubjectID: "quit", QuestionCount: 15})
	waitForSnapshot(t, manager, "quit", quitSession.InterviewID, func(s Snapshot) bool { return s.AwaitingAnswer != nil })
	if err := manager.Quit(context.Background(), "quit", quitSession.InterviewID, "user_requested"); err != nil {
		t.Fatal(err)
	}
	quitFinal := waitForSnapshot(t, manager, "quit", quitSession.InterviewID, func(s Snapshot) bool { return s.Status.Terminal() })
	if quitFinal.Status != StatusTerminated || quitFinal.EndedReason == nil || *quitFinal.EndedReason != "user_requested" {
		t.Fatalf("quit final = %#v", quitFinal)
	}

	timedSession, _ := manager.Create(context.Background(), CreateInput{SubjectID: "idle", QuestionCount: 15})
	timedFinal := waitForSnapshot(t, manager, "idle", timedSession.InterviewID, func(s Snapshot) bool { return s.Status.Terminal() })
	if timedFinal.Status != StatusTerminated || timedFinal.EndedReason == nil || *timedFinal.EndedReason != "idle_timeout" {
		t.Fatalf("timeout final = %#v", timedFinal)
	}
}

func TestManagerStartupCleanupAndRepeatedCloseDoNotLeakActors(t *testing.T) {
	repository := newMemoryRepository()
	interrupted := Snapshot{InterviewID: "int_interrupted", SubjectID: "subject", Status: StatusInterviewing, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	repository.sessions[interrupted.InterviewID] = interrupted
	manager := newTestManager(t, repository, time.Minute)
	cleaned, _ := repository.LoadSession(context.Background(), interrupted.InterviewID)
	if cleaned.Status != StatusFailed || cleaned.EndedReason == nil || *cleaned.EndedReason != "server_restart" {
		t.Fatalf("startup cleanup = %#v", cleaned)
	}

	baseline := runtime.NumGoroutine()
	for i := 0; i < 10; i++ {
		if _, err := manager.Create(context.Background(), CreateInput{SubjectID: "subject-" + time.Now().Add(time.Duration(i)).String(), QuestionCount: 15}); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	if delta := runtime.NumGoroutine() - baseline; delta > 3 {
		t.Fatalf("goroutine delta after 10 session exits = %d", delta)
	}
}

type failingEngine struct{ DeterministicEngine }

func (failingEngine) Prepare(context.Context, CreateInput) ([]Question, error) {
	return nil, errors.New("provider unavailable")
}

func TestManagerFailureHasObservableStageAndReason(t *testing.T) {
	repository := newMemoryRepository()
	manager, err := NewManager(fakeGraph{}, repository, WithEngine(failingEngine{}), WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	created, err := manager.Create(context.Background(), CreateInput{SubjectID: "failure", QuestionCount: 15})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForSnapshot(t, manager, "failure", created.InterviewID, func(s Snapshot) bool { return s.Status == StatusFailed })
	if failed.Stage != "failed" || failed.EndedReason == nil || *failed.EndedReason != "llm_failure" {
		t.Fatalf("failed snapshot = %#v", failed)
	}
}

func newTestManager(t *testing.T, repository *memoryRepository, idleTimeout time.Duration) *Manager {
	t.Helper()
	manager, err := NewManager(fakeGraph{}, repository, WithIdleTimeout(idleTimeout), WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

func waitForSnapshot(t *testing.T, manager *Manager, subjectID, interviewID string, ready func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := manager.Snapshot(context.Background(), subjectID, interviewID)
		if err == nil && ready(snapshot) {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %s", interviewID)
	return Snapshot{}
}

func conflictCode(err error) string {
	var conflict *ConflictError
	if errors.As(err, &conflict) {
		return conflict.Code
	}
	return ""
}
