package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"interview-agent/internal/adapters/bm25"
	llmadapter "interview-agent/internal/adapters/llm/openai"
	"interview-agent/internal/adapters/sqlite"
	coreagent "interview-agent/internal/core/agent"
	coregraph "interview-agent/internal/core/graph"
	"interview-agent/internal/session"
)

func TestInterviewHTTPAndSSEContract(t *testing.T) {
	server, manager := newInterviewTestServer(t, time.Minute)
	client := server.Client()

	created := createInterviewHTTP(t, client, server.URL, "subject-a")
	snapshot := waitForHTTPSnapshot(t, client, server.URL, "subject-a", created.InterviewID, func(s session.Snapshot) bool { return s.AwaitingAnswer != nil })
	firstPrompt := snapshot.AwaitingAnswer.PromptID
	lastID := snapshot.LastEventID

	liveRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/interviews/"+created.InterviewID+"/events", nil)
	setBearer(t, liveRequest, "subject-a")
	liveRequest.Header.Set("Last-Event-ID", fmt.Sprint(lastID))
	liveResponse, err := client.Do(liveRequest)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	if liveResponse.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d", liveResponse.StatusCode)
	}

	answerResponse := postJSON(t, client, server.URL+"/api/v1/interviews/"+created.InterviewID+"/answers", "subject-a", map[string]any{"prompt_id": firstPrompt, "text": "answer"})
	if answerResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("answer status = %d body=%s", answerResponse.StatusCode, readBody(answerResponse))
	}
	_ = answerResponse.Body.Close()

	events := readSSEUntil(t, liveResponse.Body, "question")
	_ = liveResponse.Body.Close()
	var sawDelta, sawFinal bool
	for _, event := range events {
		if event.Type == "question_delta" {
			sawDelta = true
			if event.ID != "" {
				t.Fatalf("question_delta replay id = %q", event.ID)
			}
		}
		if event.Type == "question" {
			sawFinal = true
			if event.ID == "" {
				t.Fatal("final question missing SSE id")
			}
		}
	}
	if !sawDelta || !sawFinal {
		t.Fatalf("live events missing delta/final: %#v", events)
	}

	reconnectRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/interviews/"+created.InterviewID+"/events", nil)
	setBearer(t, reconnectRequest, "subject-a")
	reconnectRequest.Header.Set("Last-Event-ID", fmt.Sprint(lastID))
	reconnectResponse, err := client.Do(reconnectRequest)
	if err != nil {
		t.Fatalf("reconnect SSE: %v", err)
	}
	replayed := readSSEUntil(t, reconnectResponse.Body, "question")
	_ = reconnectResponse.Body.Close()
	for _, event := range replayed {
		if event.Type == "question_delta" {
			t.Fatal("transient question_delta was replayed")
		}
		if event.ID == "" {
			t.Fatalf("replayed event missing id: %#v", event)
		}
	}

	duplicate := postJSON(t, client, server.URL+"/api/v1/interviews/"+created.InterviewID+"/answers", "subject-a", map[string]any{"prompt_id": firstPrompt, "text": "again"})
	assertAPIError(t, duplicate, http.StatusConflict, "answer_already_submitted")
	wrong := postJSON(t, client, server.URL+"/api/v1/interviews/"+created.InterviewID+"/answers", "subject-a", map[string]any{"prompt_id": "wrong", "text": "answer"})
	assertAPIError(t, wrong, http.StatusConflict, "prompt_mismatch")

	unauthorized := getWithSubject(t, client, server.URL+"/api/v1/interviews/"+created.InterviewID, "subject-b")
	assertAPIError(t, unauthorized, http.StatusNotFound, "interview_not_found")

	for number := 2; number <= 15; number++ {
		snapshot = waitForHTTPSnapshot(t, client, server.URL, "subject-a", created.InterviewID, func(s session.Snapshot) bool {
			return s.AwaitingAnswer != nil && s.AwaitingAnswer.Number == number
		})
		response := postJSON(t, client, server.URL+"/api/v1/interviews/"+created.InterviewID+"/answers", "subject-a", map[string]any{"prompt_id": snapshot.AwaitingAnswer.PromptID, "text": "answer"})
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("question %d status = %d body=%s", number, response.StatusCode, readBody(response))
		}
		_ = response.Body.Close()
	}
	final := waitForHTTPSnapshot(t, client, server.URL, "subject-a", created.InterviewID, func(s session.Snapshot) bool { return s.Status == session.StatusCompleted })
	if final.Progress.Answered != 15 || !final.ReportReady || !final.ReviewPlanReady {
		t.Fatalf("final snapshot = %#v", final)
	}
	storedSubject, err := jwtSubject("Bearer "+signedTestToken(t, "subject-a"), testJWTSecret, testSubjectIDPepper)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(context.Background(), storedSubject, created.InterviewID); err != nil {
		t.Fatalf("repository-backed final snapshot: %v", err)
	}
}

func TestInterviewHTTPQuitAndTimeout(t *testing.T) {
	// Keep enough margin for SQLite migrations and direction persistence on
	// slower CI runners while still exercising the actor idle-timeout branch.
	server, _ := newInterviewTestServer(t, 150*time.Millisecond)
	client := server.Client()

	quit := createInterviewHTTP(t, client, server.URL, "quit-subject")
	waitForHTTPSnapshot(t, client, server.URL, "quit-subject", quit.InterviewID, func(s session.Snapshot) bool { return s.AwaitingAnswer != nil })
	response := postJSON(t, client, server.URL+"/api/v1/interviews/"+quit.InterviewID+"/quit", "quit-subject", map[string]any{"reason": "user_requested"})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("quit status = %d", response.StatusCode)
	}
	_ = response.Body.Close()
	quitFinal := waitForHTTPSnapshot(t, client, server.URL, "quit-subject", quit.InterviewID, func(s session.Snapshot) bool { return s.Status.Terminal() })
	if quitFinal.Status != session.StatusTerminated {
		t.Fatalf("quit status = %s", quitFinal.Status)
	}

	timed := createInterviewHTTP(t, client, server.URL, "idle-subject")
	timedFinal := waitForHTTPSnapshot(t, client, server.URL, "idle-subject", timed.InterviewID, func(s session.Snapshot) bool { return s.Status.Terminal() })
	if timedFinal.Status != session.StatusTerminated || timedFinal.EndedReason == nil || *timedFinal.EndedReason != "idle_timeout" {
		t.Fatalf("timeout snapshot = %#v", timedFinal)
	}
}

type createResponse struct {
	InterviewID string `json:"interview_id"`
}

func createInterviewHTTP(t *testing.T, client *http.Client, baseURL, subject string) createResponse {
	t.Helper()
	response := postJSON(t, client, baseURL+"/api/v1/interviews", subject, map[string]any{
		"jd_text": "Go backend", "resume_text": "Go engineer", "options": map[string]any{"question_count": 15},
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", response.StatusCode, readBody(response))
	}
	defer response.Body.Close()
	var created createResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return created
}

func newInterviewTestServer(t *testing.T, idleTimeout time.Duration) (*httptest.Server, *session.Manager) {
	return newInterviewTestServerWithSecurity(t, idleTimeout, SecurityConfig{
		Mode: "jwt", JWTSecret: testJWTSecret, SubjectIDPepper: testSubjectIDPepper,
	})
}

func newInterviewTestServerWithSecurity(t *testing.T, idleTimeout time.Duration, security SecurityConfig) (*httptest.Server, *session.Manager) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "interview.db"))
	if err != nil {
		t.Fatal(err)
	}
	index := bm25.New()
	llm, err := llmadapter.New("https://provider.invalid/v1", "test-key", "test-model", time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	agentRuntime, err := coreagent.New(llm)
	if err != nil {
		t.Fatal(err)
	}
	graphRuntime, err := coregraph.New(agentRuntime, store, index)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := session.NewManager(graphRuntime, store, session.WithIdleTimeout(idleTimeout), session.WithLogger(discardLogger()))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(fakeConfig{}, manager, discardLogger(), WithSecurity(security)).Handler())
	t.Cleanup(func() { server.Close(); _ = manager.Close(); index.Close(); _ = store.Close() })
	return server, manager
}

func postJSON(t *testing.T, client *http.Client, url, subject string, body any) *http.Response {
	t.Helper()
	payload, _ := json.Marshal(body)
	request, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	setBearer(t, request, subject)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func getWithSubject(t *testing.T, client *http.Client, url, subject string) *http.Response {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, url, nil)
	setBearer(t, request, subject)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

const testJWTSecret = "test-only-secret-with-at-least-thirty-two-characters"
const testSubjectIDPepper = "test-only-stable-subject-pepper-at-least-thirty-two-characters"

func setBearer(t *testing.T, request *http.Request, subject string) {
	t.Helper()
	request.Header.Set("Authorization", "Bearer "+signedTestToken(t, subject))
}

func signedTestToken(t *testing.T, subject string) string {
	return signedTestTokenWithSecret(t, subject, testJWTSecret)
}

func signedTestTokenWithSecret(t *testing.T, subject, secret string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: subject, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), IssuedAt: jwt.NewNumericDate(time.Now()),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func waitForHTTPSnapshot(t *testing.T, client *http.Client, baseURL, subject, interviewID string, ready func(session.Snapshot) bool) session.Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response := getWithSubject(t, client, baseURL+"/api/v1/interviews/"+interviewID, subject)
		if response.StatusCode == http.StatusOK {
			var snapshot session.Snapshot
			if err := json.NewDecoder(response.Body).Decode(&snapshot); err == nil && ready(snapshot) {
				_ = response.Body.Close()
				return snapshot
			}
		}
		_ = response.Body.Close()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for HTTP snapshot %s", interviewID)
	return session.Snapshot{}
}

type sseEvent struct{ ID, Type, Data string }

func readSSEUntil(t *testing.T, body io.Reader, terminalType string) []sseEvent {
	t.Helper()
	scanner := bufio.NewScanner(body)
	var events []sseEvent
	current := sseEvent{}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			current.ID = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			current.Type = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			current.Data = strings.TrimPrefix(line, "data: ")
		case line == "" && current.Type != "":
			events = append(events, current)
			if current.Type == terminalType {
				return events
			}
			current = sseEvent{}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read SSE: %v", err)
	}
	return events
}

func assertAPIError(t *testing.T, response *http.Response, status int, code string) {
	t.Helper()
	defer response.Body.Close()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status || payload.Error.Code != code {
		t.Fatalf("API error = status %d code %q, want %d %q", response.StatusCode, payload.Error.Code, status, code)
	}
}

func readBody(response *http.Response) string {
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return string(body)
}
