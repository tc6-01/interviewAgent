package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"interview-agent/internal/adapters/bm25"
	llmadapter "interview-agent/internal/adapters/llm/openai"
	"interview-agent/internal/adapters/sqlite"
	coreagent "interview-agent/internal/core/agent"
	coregraph "interview-agent/internal/core/graph"
	"interview-agent/internal/session"
)

type retryContractEngine struct {
	session.DeterministicEngine
	mu       sync.Mutex
	failPlan bool
}

func (e *retryContractEngine) ReviewPlan(ctx context.Context, snapshot session.Snapshot) (session.Artifact, error) {
	e.mu.Lock()
	fail := e.failPlan
	e.failPlan = false
	e.mu.Unlock()
	if fail {
		return session.Artifact{}, context.DeadlineExceeded
	}
	time.Sleep(30 * time.Millisecond)
	return e.DeterministicEngine.ReviewPlan(ctx, snapshot)
}

func TestDirectionConfirmationFeedsInterviewAndReviewPlanRetryContract(t *testing.T) {
	server := newT4ContractServer(t, &retryContractEngine{failPlan: true})
	client := server.Client()
	subject := "subject-direction"

	directionResponse := postJSON(t, client, server.URL+"/api/v1/interview-directions", subject, map[string]any{
		"jd_text": "Go backend engineer", "resume_text": "Five years of Go services",
	})
	if directionResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create direction status=%d body=%s", directionResponse.StatusCode, readBody(directionResponse))
	}
	var direction session.Direction
	if err := json.NewDecoder(directionResponse.Body).Decode(&direction); err != nil {
		t.Fatal(err)
	}
	_ = directionResponse.Body.Close()

	patchedResponse := patchJSON(t, client, server.URL+"/api/v1/interview-directions/"+direction.ID, subject, map[string]any{
		"expected_version": direction.Version,
		"position":         "Senior Go Backend Engineer", "experience_level": "senior",
		"focus_areas":    []string{"Go runtime", "distributed systems", "system design"},
		"matched_skills": []string{"Go", "microservices"}, "gaps": []string{"capacity planning"},
	})
	if patchedResponse.StatusCode != http.StatusOK {
		t.Fatalf("patch direction status=%d body=%s", patchedResponse.StatusCode, readBody(patchedResponse))
	}
	if err := json.NewDecoder(patchedResponse.Body).Decode(&direction); err != nil {
		t.Fatal(err)
	}
	_ = patchedResponse.Body.Close()
	confirmResponse := postJSON(t, client, server.URL+"/api/v1/interview-directions/"+direction.ID+"/confirm", subject, map[string]any{"expected_version": direction.Version})
	if confirmResponse.StatusCode != http.StatusOK {
		t.Fatalf("confirm direction status=%d body=%s", confirmResponse.StatusCode, readBody(confirmResponse))
	}
	_ = confirmResponse.Body.Close()

	interviewResponse := postJSON(t, client, server.URL+"/api/v1/interviews", subject, map[string]any{
		"direction_id": direction.ID, "options": map[string]any{"question_count": 15},
	})
	if interviewResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create interview status=%d body=%s", interviewResponse.StatusCode, readBody(interviewResponse))
	}
	var created createResponse
	if err := json.NewDecoder(interviewResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	_ = interviewResponse.Body.Close()
	for number := 1; number <= 15; number++ {
		snapshot := waitForHTTPSnapshot(t, client, server.URL, subject, created.InterviewID, func(snapshot session.Snapshot) bool {
			return snapshot.AwaitingAnswer != nil && snapshot.AwaitingAnswer.Number == number
		})
		if snapshot.Direction == nil || snapshot.Direction.Version != direction.Version || snapshot.Direction.Position != "Senior Go Backend Engineer" {
			t.Fatalf("confirmed direction not attached: %#v", snapshot.Direction)
		}
		response := postJSON(t, client, server.URL+"/api/v1/interviews/"+created.InterviewID+"/answers", subject, map[string]any{"prompt_id": snapshot.AwaitingAnswer.PromptID, "text": "answer"})
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("answer %d status=%d body=%s", number, response.StatusCode, readBody(response))
		}
		_ = response.Body.Close()
	}
	failed := waitForHTTPSnapshot(t, client, server.URL, subject, created.InterviewID, func(snapshot session.Snapshot) bool { return snapshot.Status == session.StatusCompleted })
	if failed.ReportStatus != session.ArtifactReady || failed.ReviewPlanStatus != session.ArtifactFailed {
		t.Fatalf("artifact statuses = report:%s review:%s", failed.ReportStatus, failed.ReviewPlanStatus)
	}
	reportResponse := getWithSubject(t, client, server.URL+"/api/v1/interviews/"+created.InterviewID+"/report", subject)
	if reportResponse.StatusCode != http.StatusOK {
		t.Fatalf("report status=%d body=%s", reportResponse.StatusCode, readBody(reportResponse))
	}
	_ = reportResponse.Body.Close()
	retry := postJSON(t, client, server.URL+"/api/v1/interviews/"+created.InterviewID+"/review-plan/retry", subject, map[string]any{})
	if retry.StatusCode != http.StatusAccepted {
		t.Fatalf("retry status=%d body=%s", retry.StatusCode, readBody(retry))
	}
	_ = retry.Body.Close()
	duplicate := postJSON(t, client, server.URL+"/api/v1/interviews/"+created.InterviewID+"/review-plan/retry", subject, map[string]any{})
	assertAPIError(t, duplicate, http.StatusConflict, "review_plan_retry_in_progress")
	ready := waitForHTTPSnapshot(t, client, server.URL, subject, created.InterviewID, func(snapshot session.Snapshot) bool { return snapshot.ReviewPlanStatus == session.ArtifactReady })
	if !ready.ReviewPlanReady {
		t.Fatalf("review plan retry did not finish: %#v", ready)
	}
}

func newT4ContractServer(t *testing.T, engine session.Engine) *httptest.Server {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "interview.db"))
	if err != nil {
		t.Fatal(err)
	}
	index := bm25.New()
	llm, _ := llmadapter.New("https://provider.invalid/v1", "test-key", "test-model", time.Second, 1)
	agentRuntime, _ := coreagent.New(llm)
	graphRuntime, _ := coregraph.New(agentRuntime, store, index)
	manager, err := session.NewManager(graphRuntime, store, session.WithEngine(engine), session.WithLogger(discardLogger()))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(fakeConfig{}, manager, discardLogger()).Handler())
	t.Cleanup(func() { server.Close(); _ = manager.Close(); index.Close(); _ = store.Close() })
	return server
}

func patchJSON(t *testing.T, client *http.Client, url, subject string, body any) *http.Response {
	t.Helper()
	payload, _ := json.Marshal(body)
	request, _ := http.NewRequest(http.MethodPatch, url, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Subject-ID", subject)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
