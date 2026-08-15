package graph

import (
	"context"
	"fmt"

	coreagent "interview-agent/internal/core/agent"
	"interview-agent/internal/domain"
)

// Runtime is the default graph boundary. Existing Eino graph nodes will be
// connected behind this type in T4 without leaking protocol concerns inward.
type Runtime struct {
	agent      *coreagent.Runtime
	repository domain.Repository
	index      domain.QuestionIndex
}

func New(agent *coreagent.Runtime, repository domain.Repository, index domain.QuestionIndex) (*Runtime, error) {
	if agent == nil || repository == nil || index == nil {
		return nil, fmt.Errorf("graph: agent, repository and question index are required")
	}
	return &Runtime{agent: agent, repository: repository, index: index}, nil
}

func (r *Runtime) Checks(ctx context.Context) []domain.CheckResult {
	if r == nil {
		return []domain.CheckResult{{Name: "graph_assembly", Err: fmt.Errorf("graph: runtime is not initialized")}}
	}
	checks := r.agent.Checks(ctx)
	checks = append(checks,
		domain.CheckResult{Name: "sqlite", Err: r.repository.Ping(ctx)},
		domain.CheckResult{Name: "bm25", Err: r.index.Check(ctx)},
		domain.CheckResult{Name: "graph_assembly"},
	)
	return checks
}
