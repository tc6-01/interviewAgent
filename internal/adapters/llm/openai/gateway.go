package openai

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Gateway is the local OpenAI-compatible adapter configuration. Provider
// traffic is initiated only by future business methods, never by readiness.
type Gateway struct {
	baseURL       string
	apiKey        string
	model         string
	timeout       time.Duration
	maxConcurrent int
}

func New(baseURL, apiKey, model string, timeout time.Duration, maxConcurrent int) (*Gateway, error) {
	g := &Gateway{
		baseURL:       strings.TrimSpace(baseURL),
		apiKey:        strings.TrimSpace(apiKey),
		model:         strings.TrimSpace(model),
		timeout:       timeout,
		maxConcurrent: maxConcurrent,
	}
	if err := g.Configured(context.Background()); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *Gateway) Configured(context.Context) error {
	if g == nil {
		return fmt.Errorf("llm: gateway is nil")
	}
	parsed, err := url.Parse(g.baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("llm: invalid base URL")
	}
	if g.apiKey == "" {
		return fmt.Errorf("llm: API key is required")
	}
	if g.model == "" {
		return fmt.Errorf("llm: model is required")
	}
	if g.timeout <= 0 || g.maxConcurrent <= 0 {
		return fmt.Errorf("llm: timeout and concurrency must be positive")
	}
	return nil
}

func (g *Gateway) Model() string {
	return g.model
}
