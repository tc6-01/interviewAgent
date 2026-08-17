package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"interview-agent/internal/domain"
)

func TestMigrationsAreRestartSafe(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "interview.db")
	for run := 0; run < 2; run++ {
		store, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("Open() run %d error = %v", run, err)
		}
		var migrations int
		if err := store.db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil {
			t.Fatalf("read migrations: %v", err)
		}
		if migrations != 4 {
			t.Fatalf("migration count = %d, want 4", migrations)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
}

func TestSubjectBoundaryMigrationUpgradesExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "interview.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}
	legacyMigration, err := migrationFiles.ReadFile("migrations/001_domain.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(legacyMigration)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES ('001_domain.sql');
		INSERT INTO subjects(id) VALUES ('subject-a');
		INSERT INTO question_banks(id, subject_id, scope, filename, version, sha256)
		VALUES ('bank-a', 'subject-a', 'user:subject-a', 'private.json', 'v1', 'hash');
		INSERT INTO questions(id, bank_id, type, question_text, source)
		VALUES ('question-a', 'bank-a', 'basic', 'legacy question', 'private.json');
		INSERT INTO interviews(id, subject_id, status) VALUES ('interview-a', 'subject-a', 'completed');
		INSERT INTO interview_results(interview_id, report_json, review_plan_json)
		VALUES ('interview-a', '{"score":88}', '{"days":7}');`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open upgraded database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var resultSubject, report string
	if err := store.db.QueryRowContext(ctx, `SELECT subject_id, report_json FROM interview_results WHERE interview_id='interview-a'`).Scan(&resultSubject, &report); err != nil {
		t.Fatal(err)
	}
	if resultSubject != "subject-a" || report != `{"score":88}` {
		t.Fatalf("migrated result subject=%q report=%q", resultSubject, report)
	}
	questions, err := store.ListUserQuestions(ctx, "subject-a")
	if err != nil || len(questions) != 1 || questions[0].ID != "question-a" {
		t.Fatalf("migrated questions=%#v err=%v", questions, err)
	}
}

func TestSubjectScopedProfileAndInterview(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	for _, subjectID := range []string{"subject-a", "subject-b"} {
		if _, err := store.EnsureSubject(ctx, subjectID); err != nil {
			t.Fatalf("EnsureSubject(%s): %v", subjectID, err)
		}
		if err := store.UpsertProfile(ctx, domain.Profile{SubjectID: subjectID, SummaryJSON: []byte(`{"role":"go"}`)}); err != nil {
			t.Fatalf("UpsertProfile(%s): %v", subjectID, err)
		}
	}
	if err := store.CreateInterview(ctx, domain.Interview{
		ID: "interview-a", SubjectID: "subject-a", Status: "completed", SummaryJSON: []byte(`{"direction":"backend"}`), QAHistoryJSON: []byte(`[]`),
	}); err != nil {
		t.Fatalf("CreateInterview: %v", err)
	}
	if err := store.SaveInterviewResult(ctx, "subject-b", domain.InterviewResult{InterviewID: "interview-a", ReportJSON: []byte(`{"score":0}`), ReviewPlanJSON: []byte(`{}`)}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("cross-subject SaveInterviewResult error = %v, want not found", err)
	}
	if err := store.SaveInterviewResult(ctx, "subject-a", domain.InterviewResult{InterviewID: "interview-a", ReportJSON: []byte(`{"score":88}`), ReviewPlanJSON: []byte(`{"days":7}`)}); err != nil {
		t.Fatalf("SaveInterviewResult: %v", err)
	}
	interview, result, err := store.GetInterview(ctx, "subject-a", "interview-a")
	if err != nil {
		t.Fatalf("GetInterview owner: %v", err)
	}
	if interview.SubjectID != "subject-a" || string(result.ReportJSON) != `{"score":88}` {
		t.Fatalf("unexpected interview/result: %#v %#v", interview, result)
	}
	if _, _, err := store.GetInterview(ctx, "subject-b", "interview-a"); err == nil || !strings.Contains(err.Error(), "no rows") {
		t.Fatalf("cross-subject GetInterview error = %v, want not found", err)
	}
}

func TestDatabaseConstraintsRejectUnscopedBanksAndResults(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	for _, subjectID := range []string{"subject-a", "subject-b"} {
		if _, err := store.EnsureSubject(ctx, subjectID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO question_banks(id, subject_id, scope, filename, sha256)
		VALUES ('invalid-user-bank', NULL, 'user:subject-a', 'invalid.json', 'hash')`); err == nil {
		t.Fatal("question_banks accepted a user scope with NULL subject_id")
	}
	if err := store.CreateInterview(ctx, domain.Interview{
		ID: "interview-a", SubjectID: "subject-a", Status: "completed", SummaryJSON: []byte(`{}`), QAHistoryJSON: []byte(`[]`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO interview_results(interview_id, subject_id, report_json, review_plan_json)
		VALUES ('interview-a', 'subject-b', '{}', '{}')`); err == nil {
		t.Fatal("interview_results accepted a subject that does not own the interview")
	}
}

func TestQuestionBankIdempotencyReplacementAndIsolation(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	builtin := []domain.Question{{ID: "builtin-1", Type: domain.QuestionTypeBasic, Topic: "Go", Text: "什么是 goroutine？", Source: "builtin"}}
	changed, err := store.EnsureBuiltinBank(ctx, domain.QuestionBank{ID: "builtin-go", Filename: "go.json", Version: "v1", SHA256: "hash-v1"}, builtin)
	if err != nil || !changed {
		t.Fatalf("EnsureBuiltinBank first = %v, %v", changed, err)
	}
	changed, err = store.EnsureBuiltinBank(ctx, domain.QuestionBank{ID: "builtin-go", Filename: "go.json", Version: "v1", SHA256: "hash-v1"}, builtin)
	if err != nil || changed {
		t.Fatalf("EnsureBuiltinBank retry = %v, %v", changed, err)
	}

	for _, subjectID := range []string{"subject-a", "subject-b"} {
		questions := []domain.Question{{ID: subjectID + "-1", Type: domain.QuestionTypeBasic, Topic: "private", Text: subjectID + " question", Source: "user.json"}}
		changed, err := store.ReplaceUserBank(ctx, domain.QuestionBank{ID: subjectID + "-bank", SubjectID: subjectID, Filename: "private.json", SHA256: "hash-1"}, questions)
		if err != nil || !changed {
			t.Fatalf("ReplaceUserBank(%s) = %v, %v", subjectID, changed, err)
		}
	}
	aQuestions, err := store.ListUserQuestions(ctx, "subject-a")
	if err != nil || len(aQuestions) != 1 || aQuestions[0].Text != "subject-a question" {
		t.Fatalf("subject-a questions = %#v, %v", aQuestions, err)
	}

	replacement := []domain.Question{{ID: "subject-a-2", Type: domain.QuestionTypeDesign, Topic: "system", Text: "new design question", Source: "user.json"}}
	changed, err = store.ReplaceUserBank(ctx, domain.QuestionBank{ID: "ignored-on-update", SubjectID: "subject-a", Filename: "private.json", SHA256: "hash-2"}, replacement)
	if err != nil || !changed {
		t.Fatalf("ReplaceUserBank changed = %v, %v", changed, err)
	}
	aQuestions, _ = store.ListUserQuestions(ctx, "subject-a")
	bQuestions, _ := store.ListUserQuestions(ctx, "subject-b")
	if len(aQuestions) != 1 || aQuestions[0].Text != "new design question" || len(bQuestions) != 1 || bQuestions[0].Text != "subject-b question" {
		t.Fatalf("replacement leaked: a=%#v b=%#v", aQuestions, bQuestions)
	}
	if err := store.DeleteQuestionBank(ctx, "subject-a", "private.json"); err != nil {
		t.Fatalf("DeleteQuestionBank: %v", err)
	}
	aQuestions, _ = store.ListUserQuestions(ctx, "subject-a")
	if len(aQuestions) != 0 {
		t.Fatalf("deleted questions = %#v, want empty", aQuestions)
	}
	if bQuestions, _ = store.ListUserQuestions(ctx, "subject-b"); len(bQuestions) != 1 {
		t.Fatalf("delete crossed subject boundary: %#v", bQuestions)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "interview.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
