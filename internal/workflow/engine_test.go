package workflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"interview-agent/internal/adapters/bm25"
	"interview-agent/internal/adapters/sqlite"
	coreagent "interview-agent/internal/core/agent"
	"interview-agent/internal/domain"
	"interview-agent/internal/session"
)

type queuedGateway struct {
	mu        sync.Mutex
	responses []string
	requests  int
}

func (g *queuedGateway) Configured(context.Context) error { return nil }
func (g *queuedGateway) Model() string                    { return "fake" }
func (g *queuedGateway) Metrics() domain.LLMMetrics {
	return domain.LLMMetrics{Requests: int64(g.requests)}
}
func (g *queuedGateway) Complete(_ context.Context, _ domain.LLMRequest) (domain.LLMResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests++
	if len(g.responses) == 0 {
		return domain.LLMResponse{}, context.DeadlineExceeded
	}
	response := g.responses[0]
	g.responses = g.responses[1:]
	return domain.LLMResponse{Content: response, Usage: domain.TokenUsage{TotalTokens: 10}}, nil
}

func newRuntimeForTest(t *testing.T, responses ...string) (*Engine, *queuedGateway) {
	t.Helper()
	gateway := &queuedGateway{responses: append([]string(nil), responses...)}
	agent, err := coreagent.New(gateway)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "interview.db"))
	if err != nil {
		t.Fatal(err)
	}
	index := bm25.New()
	t.Cleanup(func() { index.Close(); _ = store.Close() })
	runtime, err := New(agent, store, index)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, gateway
}

func TestDirectionSchemaRepairRunsAtMostOnce(t *testing.T) {
	runtime, gateway := newRuntimeForTest(t,
		`{"position":"Go Engineer"}`,
		`{"position":"Go Engineer","experience_level":"senior","focus_areas":["Go","distributed systems"],"matched_skills":["Go"],"gaps":[]}`,
	)
	direction, err := runtime.GenerateDirection(context.Background(), "subject", "jd fixture", "resume fixture")
	if err != nil {
		t.Fatal(err)
	}
	if direction.Position != "Go Engineer" || len(direction.FocusAreas) != 2 || gateway.requests != 2 {
		t.Fatalf("direction=%#v requests=%d", direction, gateway.requests)
	}
}

func TestReviewPlanReferencesWeakQuestionsSanitizesURLsAndAddsAdvancedDirections(t *testing.T) {
	runtime, _ := newRuntimeForTest(t, `{
		"study_plan":[{"topic":"capacity","objective":"improve","actions":["practice"],"time_estimate":"2h"}],
		"resources":[
			{"title":"Go docs","url":"https://go.dev/doc/","type":"article","desc":"official"},
			{"title":"Unverified","url":"https://example.invalid/course","type":"video","desc":"unknown"}
		],
		"advanced_directions":["performance engineering"]
	}`)
	snapshot := session.Snapshot{
		InterviewID: "int_test", ReportStatus: session.ArtifactReady,
		QAHistory: []session.QARecord{
			{PromptID: "p1", Number: 1, Kind: "primary", Score: 70, KeyPointsMissed: []string{"capacity"}},
			{PromptID: "p2", Number: 2, Kind: "primary", Score: 100},
		},
	}
	artifact, err := runtime.ReviewPlan(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		WeakAreas []struct {
			SourcePromptIDs []string `json:"source_prompt_ids"`
		} `json:"weak_areas"`
		Resources []map[string]any `json:"resources"`
		Advanced  []string         `json:"advanced_directions"`
	}
	if err := json.Unmarshal(artifact.Value, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.WeakAreas) == 0 || len(plan.WeakAreas[0].SourcePromptIDs) == 0 {
		t.Fatalf("weak areas missing evidence: %s", artifact.Value)
	}
	if _, ok := plan.Resources[0]["url"]; !ok {
		t.Fatalf("stable URL removed: %#v", plan.Resources)
	}
	if _, ok := plan.Resources[1]["url"]; ok {
		t.Fatalf("unverified URL retained: %#v", plan.Resources)
	}
	if len(plan.Advanced) == 0 {
		t.Fatalf("advanced directions missing: %s", artifact.Value)
	}
}

func TestDirectionFixtureP50(t *testing.T) {
	const fixture = `{"position":"Backend Engineer","experience_level":"mid","focus_areas":["Go"],"matched_skills":["Go"],"gaps":[]}`
	var durations []time.Duration
	for index := 0; index < 5; index++ {
		runtime, _ := newRuntimeForTest(t, fixture)
		started := time.Now()
		if _, err := runtime.GenerateDirection(context.Background(), "subject", "jd fixture", "resume fixture"); err != nil {
			t.Fatal(err)
		}
		durations = append(durations, time.Since(started))
	}
	for i := 0; i < len(durations); i++ {
		for j := i + 1; j < len(durations); j++ {
			if durations[j] < durations[i] {
				durations[i], durations[j] = durations[j], durations[i]
			}
		}
	}
	t.Logf("fixed fixture direction generation P50=%s", durations[len(durations)/2])
}
