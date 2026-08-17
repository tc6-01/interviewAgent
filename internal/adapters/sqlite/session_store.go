package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"interview-agent/internal/domain"
	"interview-agent/internal/session"
)

func (s *Store) CreateDirection(ctx context.Context, direction session.Direction) error {
	if _, err := s.EnsureSubject(ctx, direction.SubjectID); err != nil {
		return err
	}
	focusAreas, _ := json.Marshal(direction.FocusAreas)
	matchedSkills, _ := json.Marshal(direction.MatchedSkills)
	gaps, _ := json.Marshal(direction.Gaps)
	_, err := s.db.ExecContext(ctx, `INSERT INTO interview_directions
		(id, subject_id, version, status, position, experience_level, focus_areas_json, matched_skills_json, gaps_json, source_sha256, confirmed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
		direction.ID, direction.SubjectID, direction.Version, direction.Status, direction.Position, direction.ExperienceLevel,
		focusAreas, matchedSkills, gaps, direction.SourceSHA256, direction.CreatedAt.Format(time.RFC3339Nano), direction.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("sqlite: create direction: %w", err)
	}
	return nil
}

func (s *Store) GetDirection(ctx context.Context, subjectID, directionID string) (session.Direction, error) {
	var direction session.Direction
	var focusAreas, matchedSkills, gaps, createdAt, updatedAt string
	var confirmedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, subject_id, version, status, position, experience_level,
		focus_areas_json, matched_skills_json, gaps_json, source_sha256, confirmed_at, created_at, updated_at
		FROM interview_directions WHERE id=? AND subject_id=?`, directionID, subjectID).Scan(
		&direction.ID, &direction.SubjectID, &direction.Version, &direction.Status, &direction.Position, &direction.ExperienceLevel,
		&focusAreas, &matchedSkills, &gaps, &direction.SourceSHA256, &confirmedAt, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return session.Direction{}, session.NotFoundError{}
	}
	if err != nil {
		return session.Direction{}, fmt.Errorf("sqlite: get direction: %w", err)
	}
	if err := json.Unmarshal([]byte(focusAreas), &direction.FocusAreas); err != nil {
		return session.Direction{}, fmt.Errorf("sqlite: decode direction focus areas: %w", err)
	}
	if err := json.Unmarshal([]byte(matchedSkills), &direction.MatchedSkills); err != nil {
		return session.Direction{}, fmt.Errorf("sqlite: decode direction matched skills: %w", err)
	}
	if err := json.Unmarshal([]byte(gaps), &direction.Gaps); err != nil {
		return session.Direction{}, fmt.Errorf("sqlite: decode direction gaps: %w", err)
	}
	direction.CreatedAt = parseSessionTime(createdAt)
	direction.UpdatedAt = parseSessionTime(updatedAt)
	if confirmedAt.Valid {
		value := parseSessionTime(confirmedAt.String)
		direction.ConfirmedAt = &value
	}
	return direction, nil
}

func (s *Store) UpdateDirection(ctx context.Context, subjectID, directionID string, patch session.DirectionPatch) (session.Direction, error) {
	focusAreas, _ := json.Marshal(patch.FocusAreas)
	matchedSkills, _ := json.Marshal(patch.MatchedSkills)
	gaps, _ := json.Marshal(patch.Gaps)
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE interview_directions SET version=version+1, position=?, experience_level=?,
		focus_areas_json=?, matched_skills_json=?, gaps_json=?, updated_at=?
		WHERE id=? AND subject_id=? AND version=? AND status='draft'`,
		strings.TrimSpace(patch.Position), strings.TrimSpace(patch.ExperienceLevel), focusAreas, matchedSkills, gaps,
		now.Format(time.RFC3339Nano), directionID, subjectID, patch.ExpectedVersion)
	if err != nil {
		return session.Direction{}, fmt.Errorf("sqlite: update direction: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return session.Direction{}, &session.ConflictError{Code: "direction_version_conflict", Message: "面试方向版本冲突或已确认"}
	}
	return s.GetDirection(ctx, subjectID, directionID)
}

func (s *Store) ConfirmDirection(ctx context.Context, subjectID, directionID string, expectedVersion int) (session.Direction, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE interview_directions SET status='confirmed', confirmed_at=?, updated_at=?
		WHERE id=? AND subject_id=? AND version=? AND status='draft'`,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), directionID, subjectID, expectedVersion)
	if err != nil {
		return session.Direction{}, fmt.Errorf("sqlite: confirm direction: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return session.Direction{}, &session.ConflictError{Code: "direction_confirm_conflict", Message: "面试方向版本冲突或已确认"}
	}
	return s.GetDirection(ctx, subjectID, directionID)
}

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
	summary, _ := json.Marshal(map[string]any{"direction": snapshot.Direction, "jd_analysis": snapshot.JDAnalysis, "match_result": snapshot.MatchResult})
	history, _ := json.Marshal(snapshot.QAHistory)
	if err := s.CreateInterview(ctx, domain.Interview{
		ID: snapshot.InterviewID, SubjectID: snapshot.SubjectID, Status: string(snapshot.Status), SummaryJSON: summary, QAHistoryJSON: history,
	}); err != nil {
		return fmt.Errorf("sqlite: mirror session interview: %w", err)
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
	summary, _ := json.Marshal(map[string]any{"direction": snapshot.Direction, "jd_analysis": snapshot.JDAnalysis, "match_result": snapshot.MatchResult})
	history, _ := json.Marshal(snapshot.QAHistory)
	if _, err := s.db.ExecContext(ctx, `UPDATE interviews SET status=?, summary_json=?, qa_history_json=?, ended_reason=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND subject_id=?`, snapshot.Status, summary, history, endedReason(snapshot), snapshot.InterviewID, snapshot.SubjectID); err != nil {
		return fmt.Errorf("sqlite: mirror session update: %w", err)
	}
	if snapshot.ReportStatus == session.ArtifactReady && len(snapshot.Report) > 0 {
		reviewPlan := []byte(`{}`)
		if snapshot.ReviewPlanStatus == session.ArtifactReady && len(snapshot.ReviewPlan) > 0 {
			reviewPlan = snapshot.ReviewPlan
		}
		if err := s.SaveInterviewResult(ctx, snapshot.SubjectID, domain.InterviewResult{
			InterviewID: snapshot.InterviewID, ReportJSON: snapshot.Report, ReviewPlanJSON: reviewPlan,
		}); err != nil {
			return fmt.Errorf("sqlite: mirror session result: %w", err)
		}
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

func parseSessionTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
