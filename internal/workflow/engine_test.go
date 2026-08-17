package workflow

import (
	"context"
	"encoding/json"
	"fmt"
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

type priorityIndex struct{}

func (*priorityIndex) Check(context.Context) error                  { return nil }
func (*priorityIndex) ReplaceScope(string, []domain.Question) error { return nil }
func (*priorityIndex) RemoveScope(string)                           {}
func (*priorityIndex) Search(_ context.Context, request domain.SearchRequest) ([]domain.SearchHit, error) {
	questionType := request.Types[0]
	if request.Scopes[0] == "user:subject" && questionType == domain.QuestionTypeBasic {
		return []domain.SearchHit{{Question: domain.Question{ID: "user-low", Type: questionType, Topic: "Go", Text: "user question", Source: "user"}, Score: 0.1}}, nil
	}
	counts := map[domain.QuestionType]int{domain.QuestionTypeBasic: 8, domain.QuestionTypeExperience: 5, domain.QuestionTypeDesign: 2}
	hits := make([]domain.SearchHit, 0, counts[questionType])
	for i := 0; i < counts[questionType]; i++ {
		hits = append(hits, domain.SearchHit{Question: domain.Question{ID: fmt.Sprintf("builtin-%s-%d", questionType, i), Type: questionType, Topic: "Go", Text: "builtin question", Source: "builtin"}, Score: 100})
	}
	return hits, nil
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
		`{"position":"Go Engineer","experience_level":"senior","focus_areas":["Go","distributed systems"],"matched_skills":["Go"],"gaps":[],"jd_analysis":{"position":"Go Engineer","company":"ACME","experience_level":"senior","required_skills":["Go"],"responsibilities":["build services"],"key_topics":["distributed systems"]},"resume_match_result":{"overall_score":90,"skill_match":[{"skill_name":"Go","required":true,"matched":true,"match_score":100,"evidence":"invented Kubernetes platform ownership"}],"strengths":["Go"],"weaknesses":[],"focus_areas":["distributed systems"],"resume_gaps":[]}}`,
		`{"position":"Go Engineer","experience_level":"senior","focus_areas":["Go","distributed systems"],"matched_skills":["Go"],"gaps":[],"jd_analysis":{"position":"Go Engineer","company":"ACME","experience_level":"senior","required_skills":["Go"],"responsibilities":["build services"],"key_topics":["distributed systems"]},"resume_match_result":{"overall_score":90,"skill_match":[{"skill_name":"Go","required":true,"matched":true,"match_score":100,"evidence":"Five years building Go services"}],"strengths":["Go"],"weaknesses":[],"focus_areas":["distributed systems"],"resume_gaps":[]}}`,
	)
	direction, err := runtime.GenerateDirection(context.Background(), "subject", "jd fixture", "Five years building Go services")
	if err != nil {
		t.Fatal(err)
	}
	if direction.Position != "Go Engineer" || len(direction.FocusAreas) != 2 || gateway.requests != 2 {
		t.Fatalf("direction=%#v requests=%d", direction, gateway.requests)
	}
}

func TestReportExcludesDegradedScoresFromAggregatesAndWeaknesses(t *testing.T) {
	runtime, _ := newRuntimeForTest(t)

	t.Run("mixed real and degraded scores", func(t *testing.T) {
		artifact, err := runtime.Report(context.Background(), session.Snapshot{
			InterviewID: "mixed",
			QAHistory: []session.QARecord{
				{PromptID: "real-basic", Number: 1, Kind: "primary", Type: "basic", Score: 80},
				{PromptID: "degraded-basic", Number: 2, Kind: "primary", Type: "basic", Score: 0, ScoreDegraded: true, KeyPointsMissed: []string{"provider failure"}},
				{PromptID: "real-experience", Number: 9, Kind: "primary", Type: "experience", Score: 60, KeyPointsMissed: []string{"capacity planning"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		var report struct {
			Overall    float64            `json:"overall_score"`
			Dimensions map[string]float64 `json:"dimension_scores"`
			Weaknesses []string           `json:"weaknesses"`
			Evidence   []struct {
				PromptIDs []string `json:"prompt_ids"`
			} `json:"weakness_evidence"`
		}
		if err := json.Unmarshal(artifact.Value, &report); err != nil {
			t.Fatal(err)
		}
		if report.Overall != 70 || report.Dimensions["basic"] != 80 || report.Dimensions["experience"] != 60 {
			t.Fatalf("aggregates include degraded score: %s", artifact.Value)
		}
		if len(report.Weaknesses) != 1 || len(report.Evidence) != 1 || len(report.Evidence[0].PromptIDs) != 1 || report.Evidence[0].PromptIDs[0] != "real-experience" {
			t.Fatalf("degraded score produced weakness evidence: %s", artifact.Value)
		}
	})

	t.Run("all scores degraded", func(t *testing.T) {
		artifact, err := runtime.Report(context.Background(), session.Snapshot{
			InterviewID: "all-degraded",
			QAHistory: []session.QARecord{
				{PromptID: "d1", Number: 1, Kind: "primary", Type: "basic", Score: 0, ScoreDegraded: true},
				{PromptID: "d2", Number: 9, Kind: "primary", Type: "experience", Score: 0, ScoreDegraded: true},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		var report struct {
			Overall    float64            `json:"overall_score"`
			Dimensions map[string]float64 `json:"dimension_scores"`
			Strengths  []string           `json:"strengths"`
			Weaknesses []string           `json:"weaknesses"`
			Evidence   []map[string]any   `json:"weakness_evidence"`
		}
		if err := json.Unmarshal(artifact.Value, &report); err != nil {
			t.Fatal(err)
		}
		if report.Overall != 0 || len(report.Dimensions) != 0 || len(report.Strengths) != 0 || len(report.Weaknesses) != 0 || len(report.Evidence) != 0 {
			t.Fatalf("all-degraded report invented signal: %s", artifact.Value)
		}
	})
}

func TestReviewPlanReferencesWeakQuestionsSanitizesURLsAndAddsAdvancedDirections(t *testing.T) {
	runtime, _ := newRuntimeForTest(t, `{
		"study_plan":[{"topic":"capacity","objective":"improve","actions":["practice","review"],"time_estimate":"2h"}],
		"resources":[
			{"title":"Go docs","url":"https://go.dev/doc/","type":"article","desc":"official"},
			{"title":"Unverified","url":"https://example.invalid/course","type":"video","desc":"unknown"},
			{"title":"Go packages","url":"https://pkg.go.dev/","type":"article","desc":"reference"}
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
	const fixture = `{"position":"Backend Engineer","experience_level":"mid","focus_areas":["Go"],"matched_skills":["Go"],"gaps":[],"jd_analysis":{"position":"Backend Engineer","company":"ACME","experience_level":"mid","required_skills":["Go"],"responsibilities":["build services"],"key_topics":["Go"]},"resume_match_result":{"overall_score":90,"skill_match":[{"skill_name":"Go","required":true,"matched":true,"match_score":100,"evidence":"Go project"}],"strengths":["Go"],"weaknesses":[],"focus_areas":["Go"],"resume_gaps":[]}}`
	var durations []time.Duration
	for index := 0; index < 5; index++ {
		runtime, _ := newRuntimeForTest(t, fixture)
		started := time.Now()
		if _, err := runtime.GenerateDirection(context.Background(), "subject", "jd fixture", "Go project"); err != nil {
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

func TestReportUsesPersistedQuestionTypesAndHighScoreHasNoWeakness(t *testing.T) {
	runtime, _ := newRuntimeForTest(t)
	records := make([]session.QARecord, 0, 15)
	for i := 1; i <= 15; i++ {
		kind := "basic"
		if i > 8 {
			kind = "experience"
		}
		if i > 13 {
			kind = "design"
		}
		records = append(records, session.QARecord{PromptID: fmt.Sprintf("p%d", i), Number: i, Kind: "primary", Type: kind, Score: 100})
	}
	artifact, err := runtime.Report(context.Background(), session.Snapshot{InterviewID: "high", QAHistory: records})
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Dimensions map[string]float64 `json:"dimension_scores"`
		Weaknesses []string           `json:"weaknesses"`
		Advanced   []string           `json:"advanced_directions"`
	}
	if err := json.Unmarshal(artifact.Value, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Dimensions) != 3 || report.Dimensions["basic"] != 100 || report.Dimensions["experience"] != 100 || report.Dimensions["design"] != 100 {
		t.Fatalf("dimensions=%v", report.Dimensions)
	}
	if len(report.Weaknesses) != 0 || len(report.Advanced) < 2 {
		t.Fatalf("high score report=%s", artifact.Value)
	}
}

func TestPrepareSelectsUserScopeBeforeHigherScoredBuiltin(t *testing.T) {
	runtime, _ := newRuntimeForTest(t)
	runtime.index = &priorityIndex{}
	questions, err := runtime.Prepare(context.Background(), session.CreateInput{SubjectID: "subject", QuestionCount: 15, Direction: &session.Direction{Position: "Go Engineer", ExperienceLevel: "senior", FocusAreas: []string{"Go"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 15 || questions[0].Source != "user" {
		t.Fatalf("first=%#v total=%d", questions[0], len(questions))
	}
}
