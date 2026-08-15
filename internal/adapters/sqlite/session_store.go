package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"interview-agent/internal/session"
)

// Session snapshots deliberately use a dedicated table so the protocol core
// remains deployable before the broader T2 domain migration lands. The table
// is still authoritative SQLite state and can be folded into the normalized
// interview tables without changing the session.Repository contract.
const sessionSchema = `CREATE TABLE IF NOT EXISTS interview_session_snapshots (
	id TEXT PRIMARY KEY,
	subject_id TEXT NOT NULL,
	status TEXT NOT NULL,
	snapshot_json TEXT NOT NULL,
	report_json TEXT,
	review_plan_json TEXT,
	ended_reason TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)`

func (s *Store) ensureSessionSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, sessionSchema); err != nil {
		return fmt.Errorf("sqlite: create session snapshot table: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS interview_session_subject_status_idx
		ON interview_session_snapshots(subject_id, status)`); err != nil {
		return fmt.Errorf("sqlite: create session snapshot index: %w", err)
	}
	return nil
}

func (s *Store) CreateSession(ctx context.Context, snapshot session.Snapshot) error {
	if err := s.ensureSessionSchema(ctx); err != nil {
		return err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("sqlite: encode session snapshot: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO interview_session_snapshots
		(id, subject_id, status, snapshot_json, report_json, review_plan_json, ended_reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, snapshot.InterviewID, snapshot.SubjectID, snapshot.Status,
		string(payload), nullableSessionJSON(snapshot.Report), nullableSessionJSON(snapshot.ReviewPlan), endedReason(snapshot),
		snapshot.CreatedAt.Format(time.RFC3339Nano), snapshot.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("sqlite: create session: %w", err)
	}
	return nil
}

func (s *Store) SaveSession(ctx context.Context, snapshot session.Snapshot) error {
	if err := s.ensureSessionSchema(ctx); err != nil {
		return err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("sqlite: encode session snapshot: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE interview_session_snapshots SET
		status=?, snapshot_json=?, report_json=?, review_plan_json=?, ended_reason=?, updated_at=? WHERE id=?`,
		snapshot.Status, string(payload), nullableSessionJSON(snapshot.Report), nullableSessionJSON(snapshot.ReviewPlan),
		endedReason(snapshot), snapshot.UpdatedAt.Format(time.RFC3339Nano), snapshot.InterviewID)
	if err != nil {
		return fmt.Errorf("sqlite: save session: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return session.NotFoundError{}
	}
	return nil
}

func (s *Store) LoadSession(ctx context.Context, interviewID string) (session.Snapshot, error) {
	if err := s.ensureSessionSchema(ctx); err != nil {
		return session.Snapshot{}, err
	}
	var subjectID, payload string
	var reportJSON, reviewPlanJSON sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT subject_id, snapshot_json, report_json, review_plan_json
		FROM interview_session_snapshots WHERE id=?`, interviewID).Scan(&subjectID, &payload, &reportJSON, &reviewPlanJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return session.Snapshot{}, session.NotFoundError{}
	}
	if err != nil {
		return session.Snapshot{}, fmt.Errorf("sqlite: load session: %w", err)
	}
	var snapshot session.Snapshot
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		return session.Snapshot{}, fmt.Errorf("sqlite: decode session snapshot: %w", err)
	}
	snapshot.SubjectID = subjectID
	if reportJSON.Valid {
		snapshot.Report = json.RawMessage(reportJSON.String)
	}
	if reviewPlanJSON.Valid {
		snapshot.ReviewPlan = json.RawMessage(reviewPlanJSON.String)
	}
	return snapshot, nil
}

func (s *Store) FailActiveSessions(ctx context.Context, reason string) error {
	if err := s.ensureSessionSchema(ctx); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM interview_session_snapshots
		WHERE status IN ('preparing','interviewing','evaluating','planning_review')`)
	if err != nil {
		return fmt.Errorf("sqlite: list interrupted sessions: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("sqlite: scan interrupted session: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("sqlite: close interrupted sessions: %w", err)
	}
	for _, id := range ids {
		snapshot, err := s.LoadSession(ctx, id)
		if err != nil {
			return err
		}
		snapshot.Status = session.StatusFailed
		snapshot.Stage = "failed"
		snapshot.AwaitingAnswer = nil
		snapshot.EndedReason = &reason
		snapshot.UpdatedAt = time.Now().UTC()
		if err := s.SaveSession(ctx, snapshot); err != nil {
			return err
		}
	}
	return nil
}

func nullableSessionJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func endedReason(snapshot session.Snapshot) string {
	if snapshot.EndedReason == nil {
		return ""
	}
	return *snapshot.EndedReason
}
