package agent

import (
	"context"
	"fmt"

	"interview-agent/internal/domain"
)

// Runtime owns model-facing agent capabilities without depending on protocol
// or infrastructure implementations.
type Runtime struct {
	llm domain.LLMGateway
}

func New(llm domain.LLMGateway) (*Runtime, error) {
	if llm == nil {
		return nil, fmt.Errorf("agent: LLM gateway is required")
	}
	return &Runtime{llm: llm}, nil
}

func (r *Runtime) Checks(ctx context.Context) []domain.CheckResult {
	if r == nil || r.llm == nil {
		return []domain.CheckResult{{Name: "agent_assembly", Err: fmt.Errorf("agent: runtime is not initialized")}}
	}
	return []domain.CheckResult{
		{Name: "llm_config", Err: r.llm.Configured(ctx)},
		{Name: "agent_assembly"},
	}
}
