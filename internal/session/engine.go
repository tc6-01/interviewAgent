package session

import (
	"context"
	"encoding/json"
	"fmt"
)

// Engine is the session-facing interview workflow boundary. T4 connects the
// Eino DAG behind this interface; the lightweight implementation below keeps
// the browser protocol runnable and deterministic in T3.
type Engine interface {
	Prepare(context.Context, CreateInput) ([]Question, error)
	Score(context.Context, Snapshot, Question, string) (Score, error)
	Report(context.Context, Snapshot) (Artifact, error)
	ReviewPlan(context.Context, Snapshot) (Artifact, error)
}

type DeterministicEngine struct{}

func (DeterministicEngine) Prepare(_ context.Context, input CreateInput) ([]Question, error) {
	count := input.QuestionCount
	if count == 0 {
		count = 15
	}
	questions := make([]Question, 0, count)
	for i := 1; i <= count; i++ {
		kind := "primary"
		questions = append(questions, Question{
			PromptID: fmt.Sprintf("prompt_%d_main", i),
			Number:   i,
			Kind:     kind,
			Content:  fmt.Sprintf("Interview question %d", i),
			Source:   "session:deterministic",
		})
	}
	return questions, nil
}

func (DeterministicEngine) Score(_ context.Context, _ Snapshot, _ Question, _ string) (Score, error) {
	return Score{Value: 80, Feedback: "answer accepted"}, nil
}

func (DeterministicEngine) Report(_ context.Context, snapshot Snapshot) (Artifact, error) {
	value, _ := json.Marshal(map[string]any{
		"interview_id":  snapshot.InterviewID,
		"overall_score": 80,
		"answered":      snapshot.Progress.Answered,
	})
	return Artifact{Markdown: "# Interview report\n\nSession completed.", Value: value}, nil
}

func (DeterministicEngine) ReviewPlan(_ context.Context, snapshot Snapshot) (Artifact, error) {
	value, _ := json.Marshal(map[string]any{
		"interview_id": snapshot.InterviewID,
		"weak_areas":   []any{},
		"study_plan":   []any{},
	})
	return Artifact{Markdown: "# Review plan\n\nReview the interview feedback.", Value: value}, nil
}
