package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"interview-agent/internal/domain"
)

// Gateway is the local OpenAI-compatible adapter configuration. Provider
// traffic is initiated only by future business methods, never by readiness.
type Gateway struct {
	baseURL        string
	apiKey         string
	model          string
	timeout        time.Duration
	maxConcurrent  int
	client         *http.Client
	semaphore      chan struct{}
	requests       atomic.Int64
	retries        atomic.Int64
	failures       atomic.Int64
	promptTokens   atomic.Int64
	outputTokens   atomic.Int64
	inFlight       atomic.Int64
	maxInFlight    atomic.Int64
	timeouts       atomic.Int64
	rateLimited    atomic.Int64
	providerErrors atomic.Int64
}

func New(baseURL, apiKey, model string, timeout time.Duration, maxConcurrent int) (*Gateway, error) {
	g := &Gateway{
		baseURL:       strings.TrimSpace(baseURL),
		apiKey:        strings.TrimSpace(apiKey),
		model:         strings.TrimSpace(model),
		timeout:       timeout,
		maxConcurrent: maxConcurrent,
		client:        &http.Client{},
	}
	if err := g.Configured(context.Background()); err != nil {
		return nil, err
	}
	g.semaphore = make(chan struct{}, maxConcurrent)
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

type providerError struct {
	status    int
	retryable bool
	message   string
}

func (e *providerError) Error() string { return e.message }

func (g *Gateway) Complete(ctx context.Context, request domain.LLMRequest) (domain.LLMResponse, error) {
	if err := g.Configured(ctx); err != nil {
		return domain.LLMResponse{}, err
	}
	select {
	case g.semaphore <- struct{}{}:
	case <-ctx.Done():
		return domain.LLMResponse{}, ctx.Err()
	}
	current := g.inFlight.Add(1)
	for {
		maximum := g.maxInFlight.Load()
		if current <= maximum || g.maxInFlight.CompareAndSwap(maximum, current) {
			break
		}
	}
	defer func() {
		g.inFlight.Add(-1)
		<-g.semaphore
	}()

	g.requests.Add(1)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			g.retries.Add(1)
			if err := waitBackoff(ctx, time.Duration(100*(1<<(attempt-1)))*time.Millisecond); err != nil {
				return domain.LLMResponse{}, err
			}
		}
		response, err := g.completeOnce(ctx, request)
		if err == nil {
			response.Retries = attempt
			g.promptTokens.Add(response.Usage.PromptTokens)
			g.outputTokens.Add(response.Usage.CompletionTokens)
			return response, nil
		}
		lastErr = err
		var remote *providerError
		if !errors.As(err, &remote) || !remote.retryable || attempt == 2 {
			break
		}
	}
	g.failures.Add(1)
	return domain.LLMResponse{}, lastErr
}

func (g *Gateway) completeOnce(ctx context.Context, request domain.LLMRequest) (domain.LLMResponse, error) {
	payload := map[string]any{
		"model": g.model,
		"messages": []map[string]string{
			{"role": "system", "content": request.SystemPrompt},
			{"role": "user", "content": request.UserPrompt},
		},
		"temperature": 0.2,
	}
	if request.JSON {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return domain.LLMResponse{}, err
	}
	attemptCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, strings.TrimRight(g.baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return domain.LLMResponse{}, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+g.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := g.client.Do(httpRequest)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(attemptCtx.Err(), context.DeadlineExceeded) {
			g.timeouts.Add(1)
		}
		return domain.LLMResponse{}, &providerError{retryable: ctx.Err() == nil, message: "llm: network request failed"}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return domain.LLMResponse{}, &providerError{retryable: true, message: "llm: read response failed"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		if response.StatusCode == http.StatusTooManyRequests {
			g.rateLimited.Add(1)
		} else if response.StatusCode >= 500 {
			g.providerErrors.Add(1)
		}
		return domain.LLMResponse{}, &providerError{status: response.StatusCode, retryable: retryable, message: "llm: provider returned status " + strconv.Itoa(response.StatusCode)}
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil || len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return domain.LLMResponse{}, &providerError{retryable: false, message: "llm: invalid provider response"}
	}
	return domain.LLMResponse{
		Content: decoded.Choices[0].Message.Content,
		Usage:   domain.TokenUsage{PromptTokens: decoded.Usage.PromptTokens, CompletionTokens: decoded.Usage.CompletionTokens, TotalTokens: decoded.Usage.TotalTokens},
	}, nil
}

func (g *Gateway) Metrics() domain.LLMMetrics {
	if g == nil {
		return domain.LLMMetrics{}
	}
	return domain.LLMMetrics{
		Requests: g.requests.Load(), Retries: g.retries.Load(), Failures: g.failures.Load(),
		PromptTokens: g.promptTokens.Load(), OutputTokens: g.outputTokens.Load(), InFlight: g.inFlight.Load(),
		MaxInFlight: g.maxInFlight.Load(), Timeouts: g.timeouts.Load(), RateLimited: g.rateLimited.Load(), ProviderErrors: g.providerErrors.Load(),
	}
}

func waitBackoff(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
