package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"interview-agent/internal/domain"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store is the default authoritative persistence adapter.
type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("sqlite: path is required")
	}
	if path != ":memory:" {
		dir := filepath.Dir(path)
		if dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("sqlite: create data directory: %w", err)
			}
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db}
	if err := store.bootstrap(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) bootstrap(ctx context.Context) error {
	for _, statement := range []string{"PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL"} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("sqlite: pragma: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("sqlite: create migration table: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("sqlite: read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var applied int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", entry.Name()).Scan(&applied); err != nil {
			return fmt.Errorf("sqlite: check migration %s: %w", entry.Name(), err)
		}
		if applied > 0 {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("sqlite: read migration %s: %w", entry.Name(), err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("sqlite: begin migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: apply migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", entry.Name()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: record migration %s: %w", entry.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("sqlite: commit migration %s: %w", entry.Name(), err)
		}
	}
	return s.Ping(ctx)
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlite: store is not initialized")
	}
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("sqlite: ping: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) EnsureSubject(ctx context.Context, id string) (domain.Subject, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.Subject{}, fmt.Errorf("sqlite: subject id is required")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO subjects(id) VALUES (?) ON CONFLICT(id) DO UPDATE SET updated_at=CURRENT_TIMESTAMP`, id); err != nil {
		return domain.Subject{}, fmt.Errorf("sqlite: ensure subject: %w", err)
	}
	var subject domain.Subject
	var createdAt, updatedAt string
	if err := s.db.QueryRowContext(ctx, "SELECT id, created_at, updated_at FROM subjects WHERE id = ?", id).Scan(&subject.ID, &createdAt, &updatedAt); err != nil {
		return domain.Subject{}, fmt.Errorf("sqlite: get subject: %w", err)
	}
	subject.CreatedAt = parseTime(createdAt)
	subject.UpdatedAt = parseTime(updatedAt)
	return subject, nil
}

func (s *Store) UpsertProfile(ctx context.Context, profile domain.Profile) error {
	if _, err := s.EnsureSubject(ctx, profile.SubjectID); err != nil {
		return err
	}
	if !validJSON(profile.SummaryJSON) {
		return fmt.Errorf("sqlite: profile summary must be JSON")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO profiles(subject_id, summary_json) VALUES (?, ?)
		ON CONFLICT(subject_id) DO UPDATE SET summary_json=excluded.summary_json, updated_at=CURRENT_TIMESTAMP`, profile.SubjectID, profile.SummaryJSON)
	if err != nil {
		return fmt.Errorf("sqlite: upsert profile: %w", err)
	}
	return nil
}

func (s *Store) GetProfile(ctx context.Context, subjectID string) (domain.Profile, error) {
	var profile domain.Profile
	var createdAt, updatedAt string
	if err := s.db.QueryRowContext(ctx, `SELECT subject_id, summary_json, created_at, updated_at FROM profiles WHERE subject_id = ?`, subjectID).
		Scan(&profile.SubjectID, &profile.SummaryJSON, &createdAt, &updatedAt); err != nil {
		return domain.Profile{}, fmt.Errorf("sqlite: get profile: %w", err)
	}
	profile.CreatedAt = parseTime(createdAt)
	profile.UpdatedAt = parseTime(updatedAt)
	return profile, nil
}

func (s *Store) EnsureBuiltinBank(ctx context.Context, bank domain.QuestionBank, questions []domain.Question) (bool, error) {
	bank.Scope = "builtin"
	bank.SubjectID = ""
	return s.replaceBank(ctx, bank, questions)
}

func (s *Store) ReplaceUserBank(ctx context.Context, bank domain.QuestionBank, questions []domain.Question) (bool, error) {
	if strings.TrimSpace(bank.SubjectID) == "" {
		return false, fmt.Errorf("sqlite: user bank subject id is required")
	}
	if _, err := s.EnsureSubject(ctx, bank.SubjectID); err != nil {
		return false, err
	}
	bank.Scope = "user:" + bank.SubjectID
	return s.replaceBank(ctx, bank, questions)
}

func (s *Store) replaceBank(ctx context.Context, bank domain.QuestionBank, questions []domain.Question) (bool, error) {
	if strings.TrimSpace(bank.ID) == "" || strings.TrimSpace(bank.Filename) == "" || strings.TrimSpace(bank.SHA256) == "" {
		return false, fmt.Errorf("sqlite: bank id, filename and sha256 are required")
	}
	if len(questions) == 0 {
		return false, fmt.Errorf("sqlite: question bank must not be empty")
	}
	for _, question := range questions {
		if strings.TrimSpace(question.ID) == "" || strings.TrimSpace(question.Text) == "" || strings.TrimSpace(question.Source) == "" || !question.Type.Valid() {
			return false, fmt.Errorf("sqlite: invalid question %q", question.ID)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("sqlite: begin bank replace: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var existingID, existingHash string
	err = tx.QueryRowContext(ctx, `SELECT id, sha256 FROM question_banks WHERE scope = ? AND filename = ?`, bank.Scope, bank.Filename).Scan(&existingID, &existingHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("sqlite: find question bank: %w", err)
	}
	if err == nil && existingHash == bank.SHA256 {
		return false, nil
	}
	if err == nil {
		bank.ID = existingID
		if _, err := tx.ExecContext(ctx, `UPDATE question_banks SET version=?, sha256=?, subject_id=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, bank.Version, bank.SHA256, nullable(bank.SubjectID), bank.ID); err != nil {
			return false, fmt.Errorf("sqlite: update question bank: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM questions WHERE bank_id = ?", bank.ID); err != nil {
			return false, fmt.Errorf("sqlite: clear questions: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `INSERT INTO question_banks(id, subject_id, scope, filename, version, sha256) VALUES (?, ?, ?, ?, ?, ?)`,
			bank.ID, nullable(bank.SubjectID), bank.Scope, bank.Filename, bank.Version, bank.SHA256); err != nil {
			return false, fmt.Errorf("sqlite: insert question bank: %w", err)
		}
	}
	for _, question := range questions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO questions(id, bank_id, type, topic, question_text, answer_text, source) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			question.ID, bank.ID, question.Type, question.Topic, question.Text, question.Answer, question.Source); err != nil {
			return false, fmt.Errorf("sqlite: insert question %s: %w", question.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("sqlite: commit bank replace: %w", err)
	}
	return true, nil
}

func (s *Store) DeleteQuestionBank(ctx context.Context, subjectID, filename string) error {
	scope := "user:" + strings.TrimSpace(subjectID)
	result, err := s.db.ExecContext(ctx, `DELETE FROM question_banks WHERE subject_id = ? AND scope = ? AND filename = ?`, subjectID, scope, filename)
	if err != nil {
		return fmt.Errorf("sqlite: delete question bank: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("sqlite: question bank rows affected: %w", err)
	} else if affected == 0 {
		return fmt.Errorf("sqlite: question bank not found: %w", sql.ErrNoRows)
	}
	return nil
}

func (s *Store) ListQuestions(ctx context.Context, scope string) ([]domain.Question, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT q.id, q.bank_id, q.type, q.topic, q.question_text, q.answer_text, q.source, q.created_at, q.updated_at
		FROM questions q JOIN question_banks b ON b.id=q.bank_id WHERE b.scope=? ORDER BY q.id`, scope)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list questions: %w", err)
	}
	defer rows.Close()
	var questions []domain.Question
	for rows.Next() {
		var question domain.Question
		var createdAt, updatedAt string
		if err := rows.Scan(&question.ID, &question.BankID, &question.Type, &question.Topic, &question.Text, &question.Answer, &question.Source, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan question: %w", err)
		}
		question.CreatedAt = parseTime(createdAt)
		question.UpdatedAt = parseTime(updatedAt)
		questions = append(questions, question)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate questions: %w", err)
	}
	return questions, nil
}

func (s *Store) ListQuestionScopes(ctx context.Context, prefix string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT scope FROM question_banks WHERE scope LIKE ? ORDER BY scope`, prefix+"%")
	if err != nil {
		return nil, fmt.Errorf("sqlite: list question scopes: %w", err)
	}
	defer rows.Close()
	var scopes []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, fmt.Errorf("sqlite: scan question scope: %w", err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate question scopes: %w", err)
	}
	return scopes, nil
}

func (s *Store) CreateInterview(ctx context.Context, interview domain.Interview) error {
	if _, err := s.EnsureSubject(ctx, interview.SubjectID); err != nil {
		return err
	}
	if !validJSON(interview.SummaryJSON) || !validJSON(interview.QAHistoryJSON) {
		return fmt.Errorf("sqlite: interview summary and history must be JSON")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO interviews(id, subject_id, status, summary_json, qa_history_json, ended_reason) VALUES (?, ?, ?, ?, ?, ?)`,
		interview.ID, interview.SubjectID, interview.Status, interview.SummaryJSON, interview.QAHistoryJSON, interview.EndedReason)
	if err != nil {
		return fmt.Errorf("sqlite: create interview: %w", err)
	}
	return nil
}

func (s *Store) SaveInterviewResult(ctx context.Context, result domain.InterviewResult) error {
	if !validJSON(result.ReportJSON) || !validJSON(result.ReviewPlanJSON) {
		return fmt.Errorf("sqlite: interview result must be JSON")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO interview_results(interview_id, report_json, review_plan_json) VALUES (?, ?, ?)
		ON CONFLICT(interview_id) DO UPDATE SET report_json=excluded.report_json, review_plan_json=excluded.review_plan_json, updated_at=CURRENT_TIMESTAMP`,
		result.InterviewID, result.ReportJSON, result.ReviewPlanJSON)
	if err != nil {
		return fmt.Errorf("sqlite: save interview result: %w", err)
	}
	return nil
}

func (s *Store) GetInterview(ctx context.Context, subjectID, interviewID string) (domain.Interview, domain.InterviewResult, error) {
	var interview domain.Interview
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, subject_id, status, summary_json, qa_history_json, ended_reason, created_at, updated_at
		FROM interviews WHERE id=? AND subject_id=?`, interviewID, subjectID).
		Scan(&interview.ID, &interview.SubjectID, &interview.Status, &interview.SummaryJSON, &interview.QAHistoryJSON, &interview.EndedReason, &createdAt, &updatedAt)
	if err != nil {
		return domain.Interview{}, domain.InterviewResult{}, fmt.Errorf("sqlite: get interview: %w", err)
	}
	interview.CreatedAt = parseTime(createdAt)
	interview.UpdatedAt = parseTime(updatedAt)

	var result domain.InterviewResult
	err = s.db.QueryRowContext(ctx, `SELECT interview_id, report_json, review_plan_json, created_at, updated_at FROM interview_results WHERE interview_id=?`, interviewID).
		Scan(&result.InterviewID, &result.ReportJSON, &result.ReviewPlanJSON, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return interview, result, nil
	}
	if err != nil {
		return domain.Interview{}, domain.InterviewResult{}, fmt.Errorf("sqlite: get interview result: %w", err)
	}
	result.CreatedAt = parseTime(createdAt)
	result.UpdatedAt = parseTime(updatedAt)
	return interview, result, nil
}

func validJSON(value []byte) bool {
	return len(value) > 0 && json.Valid(value)
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func parseTime(value string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
