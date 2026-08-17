package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type scriptedEngine struct {
	DeterministicEngine
	mu                 sync.Mutex
	failFirstScore     bool
	followUpFirst      bool
	reviewPlanFailures int
	reviewPlanDelay    time.Duration
}

func (e *scriptedEngine) Score(_ context.Context, _ Snapshot, question Question, _ string) (Score, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failFirstScore {
		e.failFirstScore = false
		return Score{}, errors.New("single question provider failure")
	}
	if e.followUpFirst && question.Kind == "primary" && question.Number == 1 {
		return Score{Value: 55, Feedback: "needs depth", KeyPointsMissed: []string{"tradeoff"}, FollowUpNeeded: true}, nil
	}
	return Score{Value: 88, Feedback: "good", KeyPointsHit: []string{"reasoning"}}, nil
}

func (e *scriptedEngine) FollowUp(_ context.Context, _ Snapshot, question Question, _ string, score Score) (*Question, error) {
	if !score.FollowUpNeeded {
		return nil, nil
	}
	return &Question{PromptID: fmt.Sprintf("prompt_%d_followup", question.Number), Number: question.Number, Kind: "followup", Content: "Please explain the tradeoff.", Source: "test"}, nil
}

func (e *scriptedEngine) Report(_ context.Context, snapshot Snapshot) (Artifact, error) {
	value, _ := json.Marshal(map[string]any{"interview_id": snapshot.InterviewID, "weakness_evidence": []map[string]any{{"prompt_ids": []string{"prompt_1_main"}}}})
	return Artifact{Markdown: "# report", Value: value}, nil
}

func (e *scriptedEngine) ReviewPlan(_ context.Context, snapshot Snapshot) (Artifact, error) {
	if e.reviewPlanDelay > 0 {
		time.Sleep(e.reviewPlanDelay)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.reviewPlanFailures > 0 {
		e.reviewPlanFailures--
		return Artifact{}, errors.New("review plan provider failure")
	}
	value, _ := json.Marshal(map[string]any{"interview_id": snapshot.InterviewID, "weak_areas": []map[string]any{{"source_prompt_ids": []string{"prompt_1_main"}}}})
	return Artifact{Markdown: "# plan", Value: value}, nil
}

func TestFollowUpDoesNotConsumeFixedQuestionPlanSlot(t *testing.T) {
	engine := &scriptedEngine{followUpFirst: true}
	manager := newManagerWithScriptedEngine(t, engine)
	created, err := manager.Create(context.Background(), CreateInput{SubjectID: "followup", QuestionCount: 15})
	if err != nil {
		t.Fatal(err)
	}
	first := waitForSnapshot(t, manager, "followup", created.InterviewID, func(snapshot Snapshot) bool { return snapshot.AwaitingAnswer != nil })
	if err := manager.Answer(context.Background(), "followup", created.InterviewID, AnswerRequest{PromptID: first.AwaitingAnswer.PromptID, Text: "partial"}); err != nil {
		t.Fatal(err)
	}
	followUp := waitForSnapshot(t, manager, "followup", created.InterviewID, func(snapshot Snapshot) bool {
		return snapshot.AwaitingAnswer != nil && snapshot.AwaitingAnswer.Kind == "followup"
	})
	if followUp.Progress.Answered != 1 || followUp.Progress.Total != 15 {
		t.Fatalf("follow-up progress = %#v", followUp.Progress)
	}
	if err := manager.Answer(context.Background(), "followup", created.InterviewID, AnswerRequest{PromptID: followUp.AwaitingAnswer.PromptID, Text: "tradeoff detail"}); err != nil {
		t.Fatal(err)
	}
	second := waitForSnapshot(t, manager, "followup", created.InterviewID, func(snapshot Snapshot) bool {
		return snapshot.AwaitingAnswer != nil && snapshot.AwaitingAnswer.Number == 2 && snapshot.AwaitingAnswer.Kind == "primary"
	})
	if second.Progress.Answered != 1 || len(second.QAHistory) != 2 {
		t.Fatalf("second snapshot progress=%#v history=%d", second.Progress, len(second.QAHistory))
	}
}

func TestSingleQuestionFailureContinuesAndReviewPlanRetryIsExclusive(t *testing.T) {
	engine := &scriptedEngine{failFirstScore: true, reviewPlanFailures: 1, reviewPlanDelay: 30 * time.Millisecond}
	manager := newManagerWithScriptedEngine(t, engine)
	created, err := manager.Create(context.Background(), CreateInput{SubjectID: "degraded", QuestionCount: 15})
	if err != nil {
		t.Fatal(err)
	}
	for number := 1; number <= 15; number++ {
		snapshot := waitForSnapshot(t, manager, "degraded", created.InterviewID, func(snapshot Snapshot) bool {
			return snapshot.AwaitingAnswer != nil && snapshot.AwaitingAnswer.Number == number
		})
		if err := manager.Answer(context.Background(), "degraded", created.InterviewID, AnswerRequest{PromptID: snapshot.AwaitingAnswer.PromptID, Text: "answer"}); err != nil {
			t.Fatal(err)
		}
	}
	failedPlan := waitForSnapshot(t, manager, "degraded", created.InterviewID, func(snapshot Snapshot) bool { return snapshot.Status == StatusCompleted })
	if !failedPlan.QAHistory[0].ScoreDegraded || failedPlan.Progress.Answered != 15 || failedPlan.ReportStatus != ArtifactReady || failedPlan.ReviewPlanStatus != ArtifactFailed {
		t.Fatalf("degraded final snapshot = %#v", failedPlan)
	}
	if err := manager.RetryReviewPlan(context.Background(), "degraded", created.InterviewID); err != nil {
		t.Fatal(err)
	}
	if err := manager.RetryReviewPlan(context.Background(), "degraded", created.InterviewID); err == nil {
		t.Fatal("expected concurrent retry conflict")
	} else if conflict, ok := err.(*ConflictError); !ok || conflict.Code != "review_plan_retry_in_progress" {
		t.Fatalf("retry conflict = %v", err)
	}
	ready := waitForSnapshot(t, manager, "degraded", created.InterviewID, func(snapshot Snapshot) bool { return snapshot.ReviewPlanStatus == ArtifactReady })
	if !ready.ReviewPlanReady || len(ready.ReviewPlan) == 0 {
		t.Fatalf("retried snapshot = %#v", ready)
	}
}

func TestQuitAfterAnswerPersistsReport(t *testing.T) {
	manager := newManagerWithScriptedEngine(t, &scriptedEngine{})
	created, _ := manager.Create(context.Background(), CreateInput{SubjectID: "quit-with-answer", QuestionCount: 15})
	first := waitForSnapshot(t, manager, "quit-with-answer", created.InterviewID, func(snapshot Snapshot) bool { return snapshot.AwaitingAnswer != nil })
	if err := manager.Answer(context.Background(), "quit-with-answer", created.InterviewID, AnswerRequest{PromptID: first.AwaitingAnswer.PromptID, Text: "answer"}); err != nil {
		t.Fatal(err)
	}
	second := waitForSnapshot(t, manager, "quit-with-answer", created.InterviewID, func(snapshot Snapshot) bool { return snapshot.Progress.Answered == 1 && snapshot.AwaitingAnswer != nil })
	if err := manager.Quit(context.Background(), "quit-with-answer", second.InterviewID, "user_requested"); err != nil {
		t.Fatal(err)
	}
	final := waitForSnapshot(t, manager, "quit-with-answer", created.InterviewID, func(snapshot Snapshot) bool { return snapshot.Status.Terminal() })
	if final.Status != StatusCompleted || final.ReportStatus != ArtifactReady {
		t.Fatalf("quit-after-answer final = %#v", final)
	}
}

func newManagerWithScriptedEngine(t *testing.T, engine Engine) *Manager {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := NewManager(fakeGraph{}, newMemoryRepository(), WithEngine(engine), WithIdleTimeout(time.Minute), WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}
