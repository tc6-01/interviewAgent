package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"interview-agent/internal/domain"
)

func TestGatewayRetries429AndRecordsUsage(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`)
	}))
	defer server.Close()
	gateway, err := New(server.URL+"/v1", "key", "model", time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	response, err := gateway.Complete(context.Background(), domain.LLMRequest{Operation: "test", JSON: true, UserPrompt: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Retries != 1 || response.Usage.TotalTokens != 18 {
		t.Fatalf("response = %#v", response)
	}
	metrics := gateway.Metrics()
	if metrics.Requests != 1 || metrics.Retries != 1 || metrics.RateLimited != 1 || metrics.PromptTokens != 11 || metrics.OutputTokens != 7 {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestGatewayDoesNotRetryBusiness4xx(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	gateway, _ := New(server.URL, "key", "model", time.Second, 1)
	if _, err := gateway.Complete(context.Background(), domain.LLMRequest{Operation: "test"}); err == nil {
		t.Fatal("expected provider error")
	}
	if calls.Load() != 1 || gateway.Metrics().Retries != 0 {
		t.Fatalf("calls=%d metrics=%#v", calls.Load(), gateway.Metrics())
	}
}

func TestGatewaySemaphoreBoundsConcurrentRequests(t *testing.T) {
	var inFlight atomic.Int64
	var maximum atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
	}))
	defer server.Close()
	gateway, _ := New(server.URL, "key", "model", time.Second, 2)
	var group sync.WaitGroup
	for index := 0; index < 6; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := gateway.Complete(context.Background(), domain.LLMRequest{Operation: "concurrency"}); err != nil {
				t.Errorf("Complete() error = %v", err)
			}
		}()
	}
	group.Wait()
	if maximum.Load() > 2 || gateway.Metrics().MaxInFlight > 2 {
		t.Fatalf("provider max=%d gateway metrics=%#v", maximum.Load(), gateway.Metrics())
	}
}
