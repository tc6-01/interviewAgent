package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"interview-agent/internal/adapters/bm25"
	llmadapter "interview-agent/internal/adapters/llm/openai"
	"interview-agent/internal/adapters/sqlite"
	coreagent "interview-agent/internal/core/agent"
	coregraph "interview-agent/internal/core/graph"
	"interview-agent/internal/httpapi"
	"interview-agent/internal/questionbank"
	"interview-agent/internal/runtimeconfig"
	"interview-agent/internal/session"
	"interview-agent/internal/workflow"
)

// App is the composition root. Concrete adapters are only wired here; inner
// layers depend on domain interfaces.
type App struct {
	handler  http.Handler
	store    *sqlite.Store
	index    *bm25.Index
	sessions *session.Manager
}

func New(ctx context.Context, cfg runtimeconfig.Config, logger *slog.Logger) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	store, err := sqlite.Open(ctx, cfg.SQLitePath)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*App, error) {
		_ = store.Close()
		return nil, err
	}

	index := bm25.New()
	catalog, err := questionbank.NewCatalog(store, index, nil)
	if err != nil {
		return fail(err)
	}
	if _, err := catalog.LoadAll(ctx); err != nil {
		return fail(err)
	}
	llm, err := llmadapter.New(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMTimeout, cfg.LLMConcurrency)
	if err != nil {
		return fail(err)
	}
	agentRuntime, err := coreagent.New(llm)
	if err != nil {
		return fail(err)
	}
	graphRuntime, err := coregraph.New(agentRuntime, store, index)
	if err != nil {
		return fail(err)
	}
	workflowEngine, err := workflow.New(agentRuntime, store, index)
	if err != nil {
		return fail(err)
	}
	sessions, err := session.NewManager(graphRuntime, store, session.WithEngine(workflowEngine), session.WithLogger(logger))
	if err != nil {
		return fail(err)
	}
	server := httpapi.New(cfg, sessions, logger)
	return &App{handler: server.Handler(), store: store, index: index, sessions: sessions}, nil
}

func (a *App) Handler() http.Handler {
	return a.handler
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}
	if a.sessions != nil {
		if err := a.sessions.Close(); err != nil {
			return fmt.Errorf("bootstrap: close sessions: %w", err)
		}
	}
	if a.index != nil {
		a.index.Close()
	}
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			return fmt.Errorf("bootstrap: close sqlite: %w", err)
		}
	}
	return nil
}
