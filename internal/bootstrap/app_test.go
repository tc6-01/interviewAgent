package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"interview-agent/internal/runtimeconfig"
)

func TestAppServesHealthAndReadinessWithoutProviderCall(t *testing.T) {
	cfg := runtimeconfig.Config{
		HTTPAddr:        ":0",
		SQLitePath:      filepath.Join(t.TempDir(), "interview.db"),
		LLMBaseURL:      "https://provider.invalid/v1",
		LLMAPIKey:       "test-key",
		LLMModel:        "test-model",
		LLMTimeout:      time.Second,
		LLMConcurrency:  1,
		ShutdownTimeout: time.Second,
	}
	app, err := New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	for _, path := range []string{"/healthz", "/readyz", "/"} {
		recorder := httptest.NewRecorder()
		app.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d; body=%s", path, recorder.Code, recorder.Body.String())
		}
		if path == "/" && !strings.Contains(recorder.Body.String(), "InterviewAgent") {
			t.Fatalf("embedded web shell missing: %s", recorder.Body.String())
		}
	}
}
