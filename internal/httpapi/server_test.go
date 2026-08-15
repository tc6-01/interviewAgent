package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"interview-agent/internal/domain"
)

type fakeConfig struct {
	err error
}

func (f fakeConfig) Validate() error { return f.err }

type fakeReadiness struct {
	calls  int
	checks []domain.CheckResult
}

func (f *fakeReadiness) Checks(context.Context) []domain.CheckResult {
	f.calls++
	return f.checks
}

func TestHealthzDoesNotRunDependencyOrLLMChecks(t *testing.T) {
	readiness := &fakeReadiness{checks: []domain.CheckResult{{Name: "llm_config", Err: errors.New("must not run")}}}
	server := New(fakeConfig{err: errors.New("must not run")}, readiness, discardLogger())

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if readiness.calls != 0 {
		t.Fatalf("readiness calls = %d, want 0", readiness.calls)
	}
}

func TestReadyzReportsLocalAssembly(t *testing.T) {
	readiness := &fakeReadiness{checks: []domain.CheckResult{
		{Name: "llm_config"},
		{Name: "sqlite"},
		{Name: "bm25"},
		{Name: "session_assembly"},
	}}
	server := New(fakeConfig{}, readiness, discardLogger())

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	for _, component := range []string{"config", "llm_config", "sqlite", "bm25", "session_assembly"} {
		if !strings.Contains(recorder.Body.String(), `"`+component+`":"ok"`) {
			t.Fatalf("body missing %s readiness: %s", component, recorder.Body.String())
		}
	}
}

func TestReadyzReturnsUnavailableWhenSQLiteFails(t *testing.T) {
	readiness := &fakeReadiness{checks: []domain.CheckResult{{Name: "sqlite", Err: errors.New("database unavailable")}}}
	server := New(fakeConfig{}, readiness, discardLogger())

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
