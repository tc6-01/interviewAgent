package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"interview-agent/internal/domain"
)

type ConfigValidator interface {
	Validate() error
}

type ReadinessReporter interface {
	Checks(context.Context) []domain.CheckResult
}

type Server struct {
	handler http.Handler
}

func New(config ConfigValidator, sessions ReadinessReporter, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(config, sessions, logger))
	return &Server{handler: mux}
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func readyz(config ConfigValidator, sessions ReadinessReporter, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		checks := map[string]string{}
		ready := true

		if config == nil {
			checks["config"] = "not_initialized"
			ready = false
		} else if err := config.Validate(); err != nil {
			checks["config"] = "failed"
			ready = false
			logger.Warn("readiness check failed", "component", "config", "error", err)
		} else {
			checks["config"] = "ok"
		}

		if sessions == nil {
			checks["session_assembly"] = "not_initialized"
			ready = false
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			for _, result := range sessions.Checks(ctx) {
				if result.Err != nil {
					checks[result.Name] = "failed"
					ready = false
					logger.Warn("readiness check failed", "component", result.Name, "error", result.Err)
					continue
				}
				checks[result.Name] = "ok"
			}
		}

		if !ready {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "checks": checks})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "checks": checks})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
