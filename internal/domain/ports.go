package domain

import "context"

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

// Repository is the persistence boundary consumed by the interview graph.
type Repository interface {
	Ping(context.Context) error
	Close() error
}

// QuestionIndex is the retrieval boundary consumed by the interview graph.
type QuestionIndex interface {
	Check(context.Context) error
}
