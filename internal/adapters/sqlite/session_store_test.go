package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"interview-agent/internal/session"
)

func TestSessionSnapshotPersistsAndInterruptedSessionIsFailedOnStartupCleanup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "interview.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	snapshot := session.Snapshot{
		InterviewID: "int_restart", SubjectID: "subject-a", Status: session.StatusInterviewing, Stage: "interview",
		AwaitingAnswer:  &session.AwaitingAnswer{PromptID: "prompt_1_main", Number: 1, Kind: "primary"},
		CurrentQuestion: &session.Question{PromptID: "prompt_1_main", Number: 1, Kind: "primary", Content: "question", Source: "test"},
		Progress:        session.Progress{Total: 15}, QAHistory: []session.QARecord{},
		ReportStatus: session.ArtifactNotStarted, ReviewPlanStatus: session.ArtifactNotStarted,
		LastEventID: 7, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateSession(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Report = json.RawMessage(`{"overall_score":80}`)
	snapshot.ReportStatus = session.ArtifactReady
	snapshot.ReportReady = true
	if err := store.SaveSession(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	restored, err := store.LoadSession(ctx, snapshot.InterviewID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.SubjectID != snapshot.SubjectID || restored.LastEventID != 7 || string(restored.Report) != string(snapshot.Report) {
		t.Fatalf("restored snapshot = %#v", restored)
	}
	interview, result, err := store.GetInterview(ctx, snapshot.SubjectID, snapshot.InterviewID)
	if err != nil {
		t.Fatal(err)
	}
	if interview.Status != string(snapshot.Status) || string(result.ReportJSON) != string(snapshot.Report) {
		t.Fatalf("normalized mirror interview=%#v result=%s", interview, result.ReportJSON)
	}
	if err := store.FailActiveSessions(ctx, "server_restart"); err != nil {
		t.Fatal(err)
	}
	failed, err := store.LoadSession(ctx, snapshot.InterviewID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != session.StatusFailed || failed.EndedReason == nil || *failed.EndedReason != "server_restart" || failed.AwaitingAnswer != nil {
		t.Fatalf("failed snapshot = %#v", failed)
	}
}
