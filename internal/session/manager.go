package session

import (
	"context"
	"fmt"

	coregraph "interview-agent/internal/core/graph"
	"interview-agent/internal/domain"
)

// Manager is the process-local session boundary. T3 will add actors and
// command mailboxes behind this API.
type Manager struct {
	graph *coregraph.Runtime
}

func NewManager(graph *coregraph.Runtime) (*Manager, error) {
	if graph == nil {
		return nil, fmt.Errorf("session: graph runtime is required")
	}
	return &Manager{graph: graph}, nil
}

func (m *Manager) Checks(ctx context.Context) []domain.CheckResult {
	if m == nil || m.graph == nil {
		return []domain.CheckResult{{Name: "session_assembly", Err: fmt.Errorf("session: manager is not initialized")}}
	}
	checks := m.graph.Checks(ctx)
	return append(checks, domain.CheckResult{Name: "session_assembly"})
}
