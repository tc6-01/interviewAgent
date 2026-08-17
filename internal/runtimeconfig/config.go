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
	AuthMode        string
	JWTSecret       string
	SubjectIDPepper string
	CORSOrigins     []string
	CookieSecure    bool
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

	cookieSecure, err := boolEnv("COOKIE_SECURE", false)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTPAddr:        envOrDefault("HTTP_ADDR", defaultHTTPAddr),
		SQLitePath:      envOrDefault("SQLITE_PATH", defaultSQLitePath),
		AuthMode:        envOrDefault("AUTH_MODE", "anonymous"),
		JWTSecret:       strings.TrimSpace(os.Getenv("JWT_SECRET")),
		SubjectIDPepper: strings.TrimSpace(os.Getenv("SUBJECT_ID_PEPPER")),
		CORSOrigins:     splitCSV(os.Getenv("CORS_ALLOWED_ORIGINS")),
		CookieSecure:    cookieSecure,
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
	authMode := strings.TrimSpace(c.AuthMode)
	if authMode == "" {
		authMode = "anonymous"
	}
	switch authMode {
	case "anonymous":
		if len(c.CORSOrigins) > 0 {
			return fmt.Errorf("config: CORS_ALLOWED_ORIGINS requires AUTH_MODE=jwt")
		}
	case "jwt":
		if len(c.JWTSecret) < 32 {
			return fmt.Errorf("config: JWT_SECRET must contain at least 32 characters when AUTH_MODE=jwt")
		}
		if len(c.SubjectIDPepper) < 32 {
			return fmt.Errorf("config: SUBJECT_ID_PEPPER must contain at least 32 characters when AUTH_MODE=jwt")
		}
	default:
		return fmt.Errorf("config: AUTH_MODE must be anonymous or jwt")
	}
	for _, origin := range c.CORSOrigins {
		parsedOrigin, err := url.Parse(origin)
		if err != nil || parsedOrigin.Scheme == "" || parsedOrigin.Host == "" || parsedOrigin.Path != "" || parsedOrigin.RawQuery != "" || parsedOrigin.Fragment != "" || origin == "*" {
			return fmt.Errorf("config: invalid CORS_ALLOWED_ORIGINS entry %q", origin)
		}
		if parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https" {
			return fmt.Errorf("config: CORS_ALLOWED_ORIGINS entries must use http(s)")
		}
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

func boolEnv(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("config: %s: %w", key, err)
	}
	return parsed, nil
}

func splitCSV(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
