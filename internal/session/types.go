package session

import (
	"encoding/json"
	"time"
)

type Status string

const (
	StatusPreparing      Status = "preparing"
	StatusInterviewing   Status = "interviewing"
	StatusEvaluating     Status = "evaluating"
	StatusPlanningReview Status = "planning_review"
	StatusCompleted      Status = "completed"
	StatusTerminated     Status = "terminated"
	StatusFailed         Status = "failed"
)

func (s Status) Terminal() bool {
	return s == StatusCompleted || s == StatusTerminated || s == StatusFailed
}

type ArtifactStatus string

const (
	ArtifactNotStarted ArtifactStatus = "not_started"
	ArtifactGenerating ArtifactStatus = "generating"
	ArtifactReady      ArtifactStatus = "ready"
	ArtifactFailed     ArtifactStatus = "failed"
)

type CreateInput struct {
	SubjectID     string
	JDText        string
	ResumeText    string
	QuestionCount int
}

type Question struct {
	PromptID string `json:"prompt_id"`
	Number   int    `json:"question_no"`
	Kind     string `json:"kind"`
	Content  string `json:"content"`
	Source   string `json:"source"`
}

type AwaitingAnswer struct {
	PromptID string `json:"prompt_id"`
	Number   int    `json:"question_no"`
	Kind     string `json:"kind"`
}

type QARecord struct {
	PromptID   string    `json:"prompt_id"`
	Number     int       `json:"question_no"`
	Kind       string    `json:"kind"`
	Question   string    `json:"question"`
	Answer     string    `json:"answer"`
	Score      float64   `json:"score"`
	Feedback   string    `json:"feedback"`
	AnsweredAt time.Time `json:"answered_at"`
}

type Progress struct {
	Answered int `json:"answered"`
	Total    int `json:"total"`
}

type Snapshot struct {
	InterviewID      string          `json:"interview_id"`
	SubjectID        string          `json:"-"`
	Status           Status          `json:"status"`
	Stage            string          `json:"stage"`
	AwaitingAnswer   *AwaitingAnswer `json:"awaiting_answer"`
	CurrentQuestion  *Question       `json:"current_question"`
	Progress         Progress        `json:"progress"`
	QAHistory        []QARecord      `json:"qa_history"`
	EndedReason      *string         `json:"ended_reason"`
	ReportStatus     ArtifactStatus  `json:"report_status"`
	ReviewPlanStatus ArtifactStatus  `json:"review_plan_status"`
	ReportReady      bool            `json:"report_ready"`
	ReviewPlanReady  bool            `json:"review_plan_ready"`
	Report           json.RawMessage `json:"-"`
	ReviewPlan       json.RawMessage `json:"-"`
	LastEventID      int64           `json:"last_event_id,string"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type Event struct {
	ID        int64           `json:"id,omitempty"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

func (e Event) Authoritative() bool { return e.ID > 0 }

type Score struct {
	Value    float64 `json:"score"`
	Feedback string  `json:"feedback"`
}

type Artifact struct {
	Markdown string          `json:"markdown"`
	Value    json.RawMessage `json:"value"`
}

type AnswerRequest struct {
	PromptID string `json:"prompt_id"`
	Text     string `json:"text"`
}

type ConflictError struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *ConflictError) Error() string { return e.Message }

type NotFoundError struct{}

func (NotFoundError) Error() string { return "interview not found" }
