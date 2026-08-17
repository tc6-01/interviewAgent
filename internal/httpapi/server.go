package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"interview-agent/internal/domain"
	"interview-agent/internal/loader"
	"interview-agent/internal/session"
)

type ConfigValidator interface {
	Validate() error
}

type ReadinessReporter interface {
	Checks(context.Context) []domain.CheckResult
}

type InterviewService interface {
	GenerateDirection(context.Context, string, string, string) (session.Direction, error)
	UpdateDirection(context.Context, string, string, session.DirectionPatch) (session.Direction, error)
	ConfirmDirection(context.Context, string, string, int) (session.Direction, error)
	Create(context.Context, session.CreateInput) (session.Snapshot, error)
	Snapshot(context.Context, string, string) (session.Snapshot, error)
	Answer(context.Context, string, string, session.AnswerRequest) error
	Quit(context.Context, string, string, string) error
	Subscribe(context.Context, string, string, int64) ([]session.Event, <-chan session.Event, func(), error)
	Report(context.Context, string, string) (session.Artifact, error)
	ReviewPlan(context.Context, string, string) (session.Artifact, error)
	RetryReviewPlan(context.Context, string, string) error
	RetryReport(context.Context, string, string) error
}

type Server struct {
	handler http.Handler
}

type Option func(*serverOptions)

type serverOptions struct {
	security SecurityConfig
	web      http.Handler
}

func WithSecurity(config SecurityConfig) Option {
	return func(options *serverOptions) { options.security = config }
}

func WithWeb(handler http.Handler) Option {
	return func(options *serverOptions) { options.web = handler }
}

func New(config ConfigValidator, sessions ReadinessReporter, logger *slog.Logger, opts ...Option) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	options := serverOptions{security: SecurityConfig{Mode: "anonymous"}}
	for _, option := range opts {
		option(&options)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(config, sessions, logger))
	if interviews, ok := sessions.(InterviewService); ok {
		mux.HandleFunc("POST /api/v1/documents/parse", parseDocument)
		mux.HandleFunc("POST /api/v1/interview-directions", createDirection(interviews))
		mux.HandleFunc("PATCH /api/v1/interview-directions/{id}", updateDirection(interviews))
		mux.HandleFunc("POST /api/v1/interview-directions/{id}/confirm", confirmDirection(interviews))
		mux.HandleFunc("POST /api/v1/interviews", createInterview(interviews))
		mux.HandleFunc("GET /api/v1/interviews/{id}", getInterview(interviews))
		mux.HandleFunc("GET /api/v1/interviews/{id}/events", interviewEvents(interviews, logger))
		mux.HandleFunc("POST /api/v1/interviews/{id}/answers", answerInterview(interviews))
		mux.HandleFunc("POST /api/v1/interviews/{id}/quit", quitInterview(interviews))
		mux.HandleFunc("GET /api/v1/interviews/{id}/report", getReport(interviews))
		mux.HandleFunc("POST /api/v1/interviews/{id}/report/retry", retryReport(interviews))
		mux.HandleFunc("GET /api/v1/interviews/{id}/review-plan", getReviewPlan(interviews))
		mux.HandleFunc("POST /api/v1/interviews/{id}/review-plan/retry", retryReviewPlan(interviews))
	}
	if options.web != nil {
		mux.Handle("GET /", options.web)
	}
	return &Server{handler: secureHandler(options.security, mux)}
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func readyz(config ConfigValidator, sessions ReadinessReporter, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		checks := map[string]string{}
		ready := true

		if config == nil {
			checks["config"] = "not_initialized"
			ready = false
		} else if err := config.Validate(); err != nil {
			checks["config"] = "failed"
			ready = false
			logger.Warn("readiness check failed", "component", "config", "error", err)
		} else {
			checks["config"] = "ok"
		}

		if sessions == nil {
			checks["session_assembly"] = "not_initialized"
			ready = false
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			for _, result := range sessions.Checks(ctx) {
				if result.Err != nil {
					checks[result.Name] = "failed"
					ready = false
					logger.Warn("readiness check failed", "component", result.Name, "error", result.Err)
					continue
				}
				checks[result.Name] = "ok"
			}
		}

		if !ready {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "checks": checks})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "checks": checks})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type createInterviewRequest struct {
	JDText      string `json:"jd_text"`
	ResumeText  string `json:"resume_text"`
	DirectionID string `json:"direction_id"`
	Options     struct {
		QuestionCount int `json:"question_count"`
	} `json:"options"`
}

func createInterview(service InterviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		var request createInterviewRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		count := request.Options.QuestionCount
		if count == 0 {
			count = 15
		}
		if utf8.RuneCountInString(request.JDText) > 50_000 || utf8.RuneCountInString(request.ResumeText) > 50_000 {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "interview_input_too_large", "JD 和简历文本均不能超过 50000 个字符", nil)
			return
		}
		if request.DirectionID == "" && (strings.TrimSpace(request.JDText) == "" || strings.TrimSpace(request.ResumeText) == "") {
			writeAPIError(w, http.StatusBadRequest, "interview_input_required", "jd_text 和 resume_text 不能为空", nil)
			return
		}
		snapshot, err := service.Create(r.Context(), session.CreateInput{
			SubjectID: subjectID, JDText: request.JDText, ResumeText: request.ResumeText, QuestionCount: count, DirectionID: request.DirectionID,
		})
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"interview_id": snapshot.InterviewID,
			"status":       snapshot.Status,
			"events_url":   "/api/v1/interviews/" + snapshot.InterviewID + "/events",
			"created_at":   snapshot.CreatedAt,
		})
	}
}

func parseDocument(w http.ResponseWriter, r *http.Request) {
	type responseSource struct {
		Type string `json:"type"`
		Name string `json:"name,omitempty"`
		URL  string `json:"url,omitempty"`
	}
	var kind, text string
	source := responseSource{Type: "text"}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, loader.MaxDocumentBytes+(1<<20))
		if err := r.ParseMultipartForm(loader.MaxDocumentBytes); err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				writeAPIError(w, http.StatusRequestEntityTooLarge, "document_too_large", "文件不能超过 10 MB", nil)
				return
			}
			writeAPIError(w, http.StatusBadRequest, "invalid_document", "无法解析上传文件", nil)
			return
		}
		defer r.MultipartForm.RemoveAll()
		kind = strings.TrimSpace(r.FormValue("kind"))
		file, header, err := r.FormFile("file")
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "document_file_required", "file 不能为空", nil)
			return
		}
		defer file.Close()
		body, err := io.ReadAll(io.LimitReader(file, loader.MaxDocumentBytes+1))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_document", "无法读取上传文件", nil)
			return
		}
		if len(body) > loader.MaxDocumentBytes {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "document_too_large", "文件不能超过 10 MB", nil)
			return
		}
		text, err = loader.ParseDocumentBytes(header.Filename, header.Header.Get("Content-Type"), body)
		if err != nil {
			var documentErr *loader.DocumentError
			if errors.As(err, &documentErr) {
				status := http.StatusUnprocessableEntity
				if documentErr.Code == "unsupported_format" || documentErr.Code == "invalid_filename" || documentErr.Code == "invalid_size" || documentErr.Code == "unsafe_archive" {
					status = http.StatusBadRequest
				}
				writeAPIError(w, status, documentErr.Code, documentErr.Message, nil)
				return
			}
			writeAPIError(w, http.StatusUnprocessableEntity, "parse_failed", "文档解析失败，请粘贴文本重试", nil)
			return
		}
		source = responseSource{Type: "file", Name: header.Filename}
	} else {
		var request struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
			URL  string `json:"url"`
		}
		if err := decodeJSON(w, r, &request); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		kind, text = strings.TrimSpace(request.Kind), request.Text
		if strings.TrimSpace(request.URL) != "" {
			writeAPIError(w, http.StatusUnprocessableEntity, "document_url_unsupported", "当前版本不抓取远程 URL，请粘贴文本或上传文件", nil)
			return
		}
	}
	if kind != "jd" && kind != "resume" {
		writeAPIError(w, http.StatusBadRequest, "invalid_document_kind", "kind 必须是 jd 或 resume", nil)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		writeAPIError(w, http.StatusBadRequest, "document_text_required", "文档内容不能为空", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "text": text, "chars": utf8.RuneCountInString(text), "source": source, "warnings": []string{}})
}

func createDirection(service InterviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		var request struct {
			JDText     string `json:"jd_text"`
			ResumeText string `json:"resume_text"`
		}
		if err := decodeJSON(w, r, &request); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		direction, err := service.GenerateDirection(r.Context(), subjectID, request.JDText, request.ResumeText)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, direction)
	}
}

func updateDirection(service InterviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		var patch session.DirectionPatch
		if err := decodeJSON(w, r, &patch); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		direction, err := service.UpdateDirection(r.Context(), subjectID, r.PathValue("id"), patch)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, direction)
	}
}

func confirmDirection(service InterviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		var request struct {
			ExpectedVersion int `json:"expected_version"`
		}
		if err := decodeJSON(w, r, &request); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		direction, err := service.ConfirmDirection(r.Context(), subjectID, r.PathValue("id"), request.ExpectedVersion)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, direction)
	}
}

func getReport(service InterviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		artifact, err := service.Report(r.Context(), subjectID, r.PathValue("id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"report_markdown": artifact.Markdown, "report": json.RawMessage(artifact.Value)})
	}
}

func getReviewPlan(service InterviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		artifact, err := service.ReviewPlan(r.Context(), subjectID, r.PathValue("id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan_markdown": artifact.Markdown, "plan": json.RawMessage(artifact.Value)})
	}
}

func retryReviewPlan(service InterviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		if r.Body != nil && r.ContentLength != 0 {
			var request struct{}
			if err := decodeJSON(w, r, &request); err != nil {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
				return
			}
		}
		if err := service.RetryReviewPlan(r.Context(), subjectID, r.PathValue("id")); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "interview_id": r.PathValue("id"), "artifact": "review_plan"})
	}
}

func retryReport(service InterviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		if err := service.RetryReport(r.Context(), subjectID, r.PathValue("id")); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "interview_id": r.PathValue("id"), "artifact": "report"})
	}
}

func getInterview(service InterviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		snapshot, err := service.Snapshot(r.Context(), subjectID, r.PathValue("id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	}
}

func answerInterview(service InterviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		var request session.AnswerRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		if strings.TrimSpace(request.PromptID) == "" || strings.TrimSpace(request.Text) == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_answer", "prompt_id 和 text 不能为空", nil)
			return
		}
		if utf8.RuneCountInString(request.Text) > 20_000 {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "answer_too_large", "回答不能超过 20000 个字符", nil)
			return
		}
		if err := service.Answer(r.Context(), subjectID, r.PathValue("id"), request); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "prompt_id": request.PromptID})
	}
}

func quitInterview(service InterviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		request := struct {
			Reason string `json:"reason"`
		}{Reason: "user_requested"}
		if r.Body != nil && r.ContentLength != 0 {
			if err := decodeJSON(w, r, &request); err != nil {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
				return
			}
		}
		if err := service.Quit(r.Context(), subjectID, r.PathValue("id"), request.Reason); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
	}
}

func interviewEvents(service InterviewService, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, err := resolveSubject(w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法解析当前主体", nil)
			return
		}
		var lastID int64
		if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
			lastID, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || lastID < 0 {
				writeAPIError(w, http.StatusBadRequest, "invalid_last_event_id", "Last-Event-ID 必须是非负整数", nil)
				return
			}
		}
		replay, events, cancel, err := service.Subscribe(r.Context(), subjectID, r.PathValue("id"), lastID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		defer cancel()
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeAPIError(w, http.StatusInternalServerError, "stream_unsupported", "响应不支持流式输出", nil)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for _, event := range replay {
			if err := writeSSE(w, event); err != nil {
				return
			}
			flusher.Flush()
			if terminalEvent(event.Type) {
				return
			}
		}
		ping := time.NewTicker(15 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ping.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case event, ok := <-events:
				if !ok {
					return
				}
				if err := writeSSE(w, event); err != nil {
					logger.Debug("write SSE event", "error", err)
					return
				}
				flusher.Flush()
				if terminalEvent(event.Type) {
					return
				}
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, event session.Event) error {
	if event.ID > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", event.ID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.Data); err != nil {
		return err
	}
	return nil
}

func terminalEvent(eventType string) bool {
	return eventType == "completed" || eventType == "terminated" || eventType == "failed"
}

func resolveSubject(_ http.ResponseWriter, r *http.Request) (string, error) {
	return subjectFromRequest(r)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid JSON: multiple values")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func writeServiceError(w http.ResponseWriter, err error) {
	if session.IsNotFound(err) {
		writeAPIError(w, http.StatusNotFound, "interview_not_found", "面试不存在", nil)
		return
	}
	var conflict *session.ConflictError
	if errors.As(err, &conflict) {
		writeAPIError(w, http.StatusConflict, conflict.Code, conflict.Message, conflict.Details)
		return
	}
	if strings.Contains(err.Error(), "question_count") || strings.Contains(err.Error(), "required") {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	writeAPIError(w, http.StatusInternalServerError, "internal_error", "内部服务错误", nil)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code": code, "message": message, "request_id": "req_" + uuid.NewString(), "details": details,
	}})
}
