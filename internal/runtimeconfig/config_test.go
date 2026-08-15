package runtimeconfig

import (
	"strings"
	"testing"
	"time"
)

func TestLoadRequiresOnlyLLMAPIKey(t *testing.T) {
	t.Setenv("LLM_API_KEY", "test-key")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("SQLITE_PATH", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("LLM_TIMEOUT", "")
	t.Setenv("LLM_MAX_CONCURRENCY", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Fatalf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
	if cfg.SQLitePath != defaultSQLitePath {
		t.Fatalf("SQLitePath = %q, want %q", cfg.SQLitePath, defaultSQLitePath)
	}
	if cfg.LLMTimeout != defaultLLMTimeout || cfg.ShutdownTimeout != defaultShutdown {
		t.Fatalf("unexpected duration defaults: %+v", cfg)
	}
}

func TestLoadRejectsMissingLLMAPIKey(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "legacy-key-must-not-be-default")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LLM_API_KEY") {
		t.Fatalf("Load() error = %v, want missing LLM_API_KEY", err)
	}
}

func TestValidateRejectsRemoteAndRuntimeMisconfiguration(t *testing.T) {
	cfg := Config{
		HTTPAddr:        ":9090",
		SQLitePath:      ":memory:",
		LLMBaseURL:      "not-a-url",
		LLMAPIKey:       "test-key",
		LLMModel:        "test-model",
		LLMTimeout:      time.Second,
		LLMConcurrency:  1,
		ShutdownTimeout: time.Second,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid base URL")
	}
}
