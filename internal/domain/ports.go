package domain

import (
	"context"
	"time"
)

// CheckResult is a local readiness result. Readiness checks must never make
// billable or remote LLM calls.
type CheckResult struct {
	Name string
	Err  error
}

// LLMGateway is the domain-facing boundary for model access. Configured only
// validates local configuration; later business methods may perform requests.
type LLMGateway interface {
	Configured(context.Context) error
	Model() string
}

type QuestionType string

const (
	QuestionTypeBasic      QuestionType = "basic"
	QuestionTypeExperience QuestionType = "experience"
	QuestionTypeDesign     QuestionType = "design"
)

func (t QuestionType) Valid() bool {
	switch t {
	case QuestionTypeBasic, QuestionTypeExperience, QuestionTypeDesign:
		return true
	default:
		return false
	}
}

type Subject struct {
	ID        string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Profile struct {
	SubjectID   string
	SummaryJSON []byte
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type QuestionBank struct {
	ID        string
	SubjectID string
	Scope     string
	Filename  string
	Version   string
	SHA256    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Question struct {
	ID        string       `json:"id"`
	BankID    string       `json:"-"`
	Type      QuestionType `json:"type"`
	Topic     string       `json:"topic"`
	Text      string       `json:"text"`
	Answer    string       `json:"answer,omitempty"`
	Source    string       `json:"source"`
	CreatedAt time.Time    `json:"-"`
	UpdatedAt time.Time    `json:"-"`
}

type Interview struct {
	ID            string
	SubjectID     string
	Status        string
	SummaryJSON   []byte
	QAHistoryJSON []byte
	EndedReason   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type InterviewResult struct {
	InterviewID    string
	ReportJSON     []byte
	ReviewPlanJSON []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Repository is the authoritative persistence boundary consumed by the
// interview graph and question-bank service. Multi-row bank operations are
// atomic in concrete implementations.
type Repository interface {
	Ping(context.Context) error
	Close() error

	EnsureSubject(context.Context, string) (Subject, error)
	UpsertProfile(context.Context, Profile) error
	GetProfile(context.Context, string) (Profile, error)

	EnsureBuiltinBank(context.Context, QuestionBank, []Question) (bool, error)
	ReplaceUserBank(context.Context, QuestionBank, []Question) (bool, error)
	DeleteQuestionBank(context.Context, string, string) error
	ListBuiltinQuestions(context.Context) ([]Question, error)
	ListUserQuestions(context.Context, string) ([]Question, error)
	ListUserQuestionSubjects(context.Context) ([]string, error)

	CreateInterview(context.Context, Interview) error
	SaveInterviewResult(context.Context, string, InterviewResult) error
	GetInterview(context.Context, string, string) (Interview, InterviewResult, error)
}

type SearchRequest struct {
	Scopes []string
	Query  string
	Types  []QuestionType
	Limit  int
}

type SearchHit struct {
	Question Question
	Score    float64
}

// QuestionIndex is process-local and rebuildable from the Repository.
type QuestionIndex interface {
	Check(context.Context) error
	ReplaceScope(string, []Question) error
	RemoveScope(string)
	Search(context.Context, SearchRequest) ([]SearchHit, error)
}

// QuestionFallback is the explicit LLM escape hatch for retrieval gaps. It is
// optional and is called only for the missing slots after BM25 retrieval.
type QuestionFallback interface {
	GenerateQuestions(context.Context, string, map[QuestionType]int) ([]Question, error)
}

type QuestionPlan struct {
	Total  int                  `json:"total"`
	Counts map[QuestionType]int `json:"counts"`
}

func FixedQuestionPlan() QuestionPlan {
	return QuestionPlan{
		Total: 15,
		Counts: map[QuestionType]int{
			QuestionTypeBasic:      8,
			QuestionTypeExperience: 5,
			QuestionTypeDesign:     2,
		},
	}
}
