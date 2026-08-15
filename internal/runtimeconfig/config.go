package runtimeconfig

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultHTTPAddr       = ":9090"
	defaultSQLitePath     = "data/interview.db"
	defaultLLMBaseURL     = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultLLMModel       = "qwen-plus"
	defaultLLMTimeout     = 60 * time.Second
	defaultShutdown       = 10 * time.Second
	defaultMaxConcurrency = 4
)

// Config contains only dependencies required by the lightweight default
// server. Optional Redis, MySQL, Milvus and embedding drivers are deliberately
// absent from this configuration path.
type Config struct {
	HTTPAddr        string
	SQLitePath      string
	LLMBaseURL      string
	LLMAPIKey       string
	LLMModel        string
	LLMTimeout      time.Duration
	LLMConcurrency  int
	ShutdownTimeout time.Duration
}

// Load reads .env when present and then resolves environment variables.
func Load() (Config, error) {
	_ = godotenv.Load()

	llmTimeout, err := durationEnv("LLM_TIMEOUT", defaultLLMTimeout)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := durationEnv("SHUTDOWN_TIMEOUT", defaultShutdown)
	if err != nil {
		return Config{}, err
	}
	concurrency, err := intEnv("LLM_MAX_CONCURRENCY", defaultMaxConcurrency)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTPAddr:        envOrDefault("HTTP_ADDR", defaultHTTPAddr),
		SQLitePath:      envOrDefault("SQLITE_PATH", defaultSQLitePath),
		LLMBaseURL:      envOrDefault("LLM_BASE_URL", defaultLLMBaseURL),
		LLMAPIKey:       strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		LLMModel:        envOrDefault("LLM_MODEL", defaultLLMModel),
		LLMTimeout:      llmTimeout,
		LLMConcurrency:  concurrency,
		ShutdownTimeout: shutdownTimeout,
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks configuration locally and never contacts the model provider.
func (c Config) Validate() error {
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return fmt.Errorf("config: HTTP_ADDR must not be empty")
	}
	if strings.TrimSpace(c.SQLitePath) == "" {
		return fmt.Errorf("config: SQLITE_PATH must not be empty")
	}
	if strings.TrimSpace(c.LLMAPIKey) == "" {
		return fmt.Errorf("config: LLM_API_KEY is required")
	}
	parsed, err := url.Parse(c.LLMBaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("config: LLM_BASE_URL must be an absolute http(s) URL")
	}
	if strings.TrimSpace(c.LLMModel) == "" {
		return fmt.Errorf("config: LLM_MODEL must not be empty")
	}
	if c.LLMTimeout <= 0 {
		return fmt.Errorf("config: LLM_TIMEOUT must be positive")
	}
	if c.LLMConcurrency <= 0 {
		return fmt.Errorf("config: LLM_MAX_CONCURRENCY must be positive")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("config: SHUTDOWN_TIMEOUT must be positive")
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	return parsed, nil
}

func intEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	return parsed, nil
}
