package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type answerCommand struct {
	request AnswerRequest
	reply   chan error
}

type quitCommand struct {
	reason string
	reply  chan error
}

type snapshotCommand struct{ reply chan snapshotResult }
type snapshotResult struct {
	snapshot Snapshot
	err      error
}
type subscribeCommand struct {
	lastID int64
	reply  chan subscribeResult
}
type subscribeResult struct {
	replay []Event
	events <-chan Event
	cancel func()
	err    error
}
type unsubscribeCommand struct{ id uint64 }

type actor struct {
	ctx         context.Context
	cancel      context.CancelFunc
	input       CreateInput
	initial     Snapshot
	subjectID   string
	repository  Repository
	engine      Engine
	idleTimeout time.Duration
	eventLimit  int
	logger      *slog.Logger
	onDone      func(*actor, Snapshot, []Event)
	mailbox     chan any

	snapshotState  Snapshot
	questions      []Question
	events         []Event
	subscribers    map[uint64]chan Event
	nextSubscriber uint64
	firstEventAt   time.Time
}

func newActor(ctx context.Context, cancel context.CancelFunc, input CreateInput, snapshot Snapshot, repository Repository, engine Engine, idleTimeout time.Duration, eventLimit int, logger *slog.Logger, onDone func(*actor, Snapshot, []Event)) *actor {
	return &actor{
		ctx: ctx, cancel: cancel, input: input, initial: snapshot, subjectID: input.SubjectID,
		repository: repository, engine: engine, idleTimeout: idleTimeout, eventLimit: eventLimit,
		logger: logger, onDone: onDone, mailbox: make(chan any, 32), snapshotState: snapshot,
		events: make([]Event, 0, eventLimit), subscribers: make(map[uint64]chan Event),
	}
}

func (a *actor) run() {
	idle := time.NewTimer(a.idleTimeout)
	defer idle.Stop()
	defer a.cancel()
	defer func() {
		for _, subscriber := range a.subscribers {
			close(subscriber)
		}
		a.onDone(a, a.snapshotState, append([]Event(nil), a.events...))
	}()

	if err := a.prepare(); err != nil {
		a.fail("llm_failure", err)
		return
	}
	for {
		select {
		case <-a.ctx.Done():
			if !a.snapshotState.Status.Terminal() {
				a.fail("server_shutdown", a.ctx.Err())
			}
			return
		case <-idle.C:
			a.terminate("idle_timeout")
			return
		case raw := <-a.mailbox:
			switch command := raw.(type) {
			case answerCommand:
				question, err := a.acceptAnswer(command.request)
				command.reply <- err
				if err == nil {
					resetTimer(idle, a.idleTimeout)
					a.processAnswer(command.request, question)
					if a.snapshotState.Status.Terminal() {
						return
					}
				}
			case quitCommand:
				hasAnswers, err := a.acceptQuit()
				command.reply <- err
				if err == nil {
					if hasAnswers {
						a.complete()
					} else {
						a.terminate(command.reason)
					}
					if a.snapshotState.Status.Terminal() {
						return
					}
				}
			case snapshotCommand:
				command.reply <- snapshotResult{snapshot: cloneSnapshot(a.snapshotState)}
			case subscribeCommand:
				a.nextSubscriber++
				id := a.nextSubscriber
				channel := make(chan Event, a.eventLimit+8)
				a.subscribers[id] = channel
				command.reply <- subscribeResult{
					replay: eventsAfter(a.events, command.lastID), events: channel,
					cancel: func() {
						select {
						case a.mailbox <- unsubscribeCommand{id: id}:
						case <-a.ctx.Done():
						}
					},
				}
			case unsubscribeCommand:
				if subscriber, ok := a.subscribers[command.id]; ok {
					delete(a.subscribers, command.id)
					close(subscriber)
				}
			}
		}
	}
}

func (a *actor) prepare() error {
	a.emit("stage", map[string]any{"stage": "jd_analysis", "message": "analyzing job description"}, true)
	if a.input.Direction != nil {
		a.snapshotState.Direction = a.input.Direction
		a.snapshotState.JDAnalysis = &JDAnalysis{
			Position: a.input.Direction.Position, ExperienceLevel: a.input.Direction.ExperienceLevel, FocusAreas: a.input.Direction.FocusAreas,
		}
		a.emit("jd_analysis", a.snapshotState.JDAnalysis, true)
	}
	a.emit("stage", map[string]any{"stage": "resume_match", "message": "matching resume"}, true)
	if a.input.Direction != nil {
		a.snapshotState.MatchResult = &MatchResult{
			OverallScore: directionMatchScore(*a.input.Direction), MatchedSkills: a.input.Direction.MatchedSkills, Gaps: a.input.Direction.Gaps,
		}
		a.emit("resume_match", a.snapshotState.MatchResult, true)
	}
	a.emit("stage", map[string]any{"stage": "question_plan", "message": "planning interview"}, true)
	questions, err := a.engine.Prepare(a.ctx, a.input)
	if err != nil {
		return err
	}
	if len(questions) != a.input.QuestionCount {
		return fmt.Errorf("session: engine returned %d questions, want %d", len(questions), a.input.QuestionCount)
	}
	a.questions = questions
	a.emit("question_plan", map[string]any{"total_questions": len(questions), "distribution": map[string]int{"primary": len(questions)}}, true)
	a.snapshotState.Status = StatusInterviewing
	a.snapshotState.Stage = "interview"
	a.emit("stage", map[string]any{"stage": "interview", "message": "interview started"}, true)
	a.ask(questions[0])
	return nil
}

func (a *actor) ask(question Question) {
	for _, delta := range splitDeltas(question.Content) {
		a.emit("question_delta", map[string]any{"prompt_id": question.PromptID, "delta": delta}, false)
	}
	a.snapshotState.CurrentQuestion = &question
	a.snapshotState.AwaitingAnswer = &AwaitingAnswer{PromptID: question.PromptID, Number: question.Number, Kind: question.Kind}
	a.emit("question", question, true)
	a.emit("awaiting_answer", a.snapshotState.AwaitingAnswer, true)
}

func (a *actor) acceptAnswer(request AnswerRequest) (Question, error) {
	request.PromptID = strings.TrimSpace(request.PromptID)
	request.Text = strings.TrimSpace(request.Text)
	if a.snapshotState.Status.Terminal() {
		return Question{}, finishedConflict(a.snapshotState)
	}
	for _, record := range a.snapshotState.QAHistory {
		if record.PromptID == request.PromptID {
			return Question{}, &ConflictError{Code: "answer_already_submitted", Message: "该回答已提交"}
		}
	}
	if a.snapshotState.Status != StatusInterviewing || a.snapshotState.AwaitingAnswer == nil {
		return Question{}, &ConflictError{Code: "interview_not_awaiting_answer", Message: "当前会话不在等待回答状态"}
	}
	if request.PromptID != a.snapshotState.AwaitingAnswer.PromptID {
		return Question{}, &ConflictError{Code: "prompt_mismatch", Message: "prompt_id 与当前问题不匹配", Details: map[string]any{"current_prompt_id": a.snapshotState.AwaitingAnswer.PromptID}}
	}
	if request.Text == "" {
		return Question{}, fmt.Errorf("answer text is required")
	}
	question := *a.snapshotState.CurrentQuestion
	a.snapshotState.AwaitingAnswer = nil
	a.emit("answer", map[string]any{"prompt_id": request.PromptID, "accepted": true}, true)
	return question, nil
}

func (a *actor) processAnswer(request AnswerRequest, question Question) {
	started := time.Now()
	score, err := a.engine.Score(a.ctx, cloneSnapshot(a.snapshotState), question, request.Text)
	degraded := err != nil
	if err != nil {
		a.emit("warning", map[string]any{"code": "score_failed", "message": "answer scoring degraded"}, true)
		score = Score{Feedback: "score unavailable"}
	}
	a.snapshotState.QAHistory = append(a.snapshotState.QAHistory, QARecord{
		PromptID: question.PromptID, Number: question.Number, Kind: question.Kind,
		Question: question.Content, Answer: request.Text, Score: score.Value, Feedback: score.Feedback,
		KeyPointsHit: score.KeyPointsHit, KeyPointsMissed: score.KeyPointsMissed, ScoreDegraded: degraded,
		AnsweredAt: time.Now().UTC(),
	})
	if question.Kind == "primary" {
		a.snapshotState.Progress.Answered++
	}
	a.emit("score", map[string]any{
		"prompt_id": request.PromptID, "score": score.Value, "feedback": score.Feedback,
		"key_points_hit": score.KeyPointsHit, "key_points_missed": score.KeyPointsMissed, "is_follow_up": question.Kind == "followup",
	}, true)
	if question.Kind == "primary" && !degraded && score.FollowUpNeeded {
		followUp, followErr := a.engine.FollowUp(a.ctx, cloneSnapshot(a.snapshotState), question, request.Text, score)
		if followErr != nil {
			a.emit("warning", map[string]any{"code": "follow_up_failed", "message": "follow-up generation degraded"}, true)
		} else if followUp != nil {
			a.ask(*followUp)
			return
		}
	}
	if a.snapshotState.Progress.Answered < len(a.questions) {
		next := a.questions[a.snapshotState.Progress.Answered]
		a.ask(next)
		a.logger.Info("interview round ready", "interview_id", a.snapshotState.InterviewID, "question_no", next.Number, "round_latency_ms", time.Since(started).Milliseconds())
		return
	}
	a.complete()
}

func (a *actor) acceptQuit() (bool, error) {
	if a.snapshotState.Status.Terminal() {
		return false, finishedConflict(a.snapshotState)
	}
	a.snapshotState.AwaitingAnswer = nil
	return a.snapshotState.Progress.Answered > 0, nil
}

func (a *actor) complete() {
	a.snapshotState.Status = StatusEvaluating
	a.snapshotState.Stage = "evaluation"
	a.snapshotState.ReportStatus = ArtifactGenerating
	a.emit("stage", map[string]any{"stage": "evaluation", "message": "generating report"}, true)
	report, err := a.engine.Report(a.ctx, cloneSnapshot(a.snapshotState))
	if err != nil {
		a.snapshotState.ReportStatus = ArtifactFailed
		a.snapshotState.Status = StatusCompleted
		a.snapshotState.Stage = "completed"
		a.emit("warning", map[string]any{"code": "report_generation_failed", "message": "report generation failed; retry is available"}, true)
		a.emit("completed", map[string]any{"status": StatusCompleted, "interview_id": a.snapshotState.InterviewID}, true)
		a.logger.Error("interview report failed", "interview_id", a.snapshotState.InterviewID, "error_type", "generation")
		return
	}
	a.snapshotState.Report = report.Value
	a.snapshotState.ReportStatus = ArtifactReady
	a.snapshotState.ReportReady = true
	a.emit("report", map[string]any{"report_markdown": report.Markdown, "report": json.RawMessage(report.Value)}, true)

	a.snapshotState.Status = StatusPlanningReview
	a.snapshotState.Stage = "review_plan"
	a.snapshotState.ReviewPlanStatus = ArtifactGenerating
	a.emit("stage", map[string]any{"stage": "review_plan", "message": "generating review plan"}, true)
	plan, err := a.engine.ReviewPlan(a.ctx, cloneSnapshot(a.snapshotState))
	if err != nil {
		a.snapshotState.ReviewPlanStatus = ArtifactFailed
		a.snapshotState.Status = StatusCompleted
		a.snapshotState.Stage = "completed"
		a.emit("warning", map[string]any{"code": "review_plan_generation_failed", "message": "review plan generation failed; retry is available"}, true)
		a.emit("completed", map[string]any{"status": StatusCompleted, "interview_id": a.snapshotState.InterviewID}, true)
		a.logger.Error("interview review plan failed", "interview_id", a.snapshotState.InterviewID, "error_type", "generation")
		return
	}
	a.snapshotState.ReviewPlan = plan.Value
	a.snapshotState.ReviewPlanStatus = ArtifactReady
	a.snapshotState.ReviewPlanReady = true
	a.emit("review_plan", map[string]any{"plan_markdown": plan.Markdown, "plan": json.RawMessage(plan.Value)}, true)
	a.snapshotState.Status = StatusCompleted
	a.snapshotState.Stage = "completed"
	a.snapshotState.AwaitingAnswer = nil
	a.emit("completed", map[string]any{"status": StatusCompleted, "interview_id": a.snapshotState.InterviewID}, true)
}

func (a *actor) terminate(reason string) {
	a.snapshotState.Status = StatusTerminated
	a.snapshotState.Stage = "terminated"
	a.snapshotState.AwaitingAnswer = nil
	a.snapshotState.EndedReason = &reason
	a.emit("terminated", map[string]any{"status": StatusTerminated, "ended_reason": reason}, true)
}

func (a *actor) fail(reason string, err error) {
	a.snapshotState.Status = StatusFailed
	a.snapshotState.Stage = "failed"
	a.snapshotState.AwaitingAnswer = nil
	a.snapshotState.EndedReason = &reason
	a.emit("failed", map[string]any{"status": StatusFailed, "ended_reason": reason}, true)
	a.logger.Error("interview session failed", "interview_id", a.snapshotState.InterviewID, "stage", a.snapshotState.Stage, "error", err)
}

func (a *actor) emit(eventType string, value any, authoritative bool) {
	now := time.Now().UTC()
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(`{"code":"event_encoding_failed"}`)
	}
	event := Event{Type: eventType, Data: data, CreatedAt: now}
	if authoritative {
		a.snapshotState.LastEventID++
		event.ID = a.snapshotState.LastEventID
		a.events = append(a.events, event)
		if len(a.events) > a.eventLimit {
			a.events = append([]Event(nil), a.events[len(a.events)-a.eventLimit:]...)
		}
	}
	a.snapshotState.UpdatedAt = now
	for _, subscriber := range a.subscribers {
		select {
		case subscriber <- event:
		default:
			a.logger.Warn("dropping event for slow SSE subscriber", "interview_id", a.snapshotState.InterviewID, "event", eventType)
		}
	}
	if authoritative {
		a.persist()
	}
	if a.firstEventAt.IsZero() {
		a.firstEventAt = now
		a.logger.Info("interview first event", "interview_id", a.snapshotState.InterviewID, "first_event_latency_ms", now.Sub(a.snapshotState.CreatedAt).Milliseconds())
	}
}

func (a *actor) persist() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.repository.SaveSession(ctx, cloneSnapshot(a.snapshotState)); err != nil {
		a.logger.Error("persist interview snapshot", "interview_id", a.snapshotState.InterviewID, "stage", a.snapshotState.Stage, "error", err)
	}
}

func (a *actor) answer(ctx context.Context, request AnswerRequest) error {
	reply := make(chan error, 1)
	select {
	case a.mailbox <- answerCommand{request: request, reply: reply}:
	case <-ctx.Done():
		return ctx.Err()
	case <-a.ctx.Done():
		return &ConflictError{Code: "interview_finished", Message: "面试已结束"}
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-a.ctx.Done():
		return &ConflictError{Code: "interview_finished", Message: "面试已结束"}
	}
}

func (a *actor) quit(ctx context.Context, reason string) error {
	reply := make(chan error, 1)
	select {
	case a.mailbox <- quitCommand{reason: reason, reply: reply}:
	case <-ctx.Done():
		return ctx.Err()
	case <-a.ctx.Done():
		return &ConflictError{Code: "interview_finished", Message: "面试已结束"}
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-a.ctx.Done():
		return &ConflictError{Code: "interview_finished", Message: "面试已结束"}
	}
}

func (a *actor) snapshot(ctx context.Context) (Snapshot, error) {
	reply := make(chan snapshotResult, 1)
	select {
	case a.mailbox <- snapshotCommand{reply: reply}:
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-a.ctx.Done():
		return Snapshot{}, a.ctx.Err()
	}
	select {
	case result := <-reply:
		return result.snapshot, result.err
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-a.ctx.Done():
		return Snapshot{}, a.ctx.Err()
	}
}

func (a *actor) subscribe(ctx context.Context, lastID int64) ([]Event, <-chan Event, func(), error) {
	reply := make(chan subscribeResult, 1)
	select {
	case a.mailbox <- subscribeCommand{lastID: lastID, reply: reply}:
	case <-ctx.Done():
		return nil, nil, nil, ctx.Err()
	case <-a.ctx.Done():
		return nil, nil, nil, a.ctx.Err()
	}
	select {
	case result := <-reply:
		return result.replay, result.events, result.cancel, result.err
	case <-ctx.Done():
		return nil, nil, nil, ctx.Err()
	case <-a.ctx.Done():
		return nil, nil, nil, a.ctx.Err()
	}
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	clone := snapshot
	clone.QAHistory = append([]QARecord(nil), snapshot.QAHistory...)
	clone.Report = append(json.RawMessage(nil), snapshot.Report...)
	clone.ReviewPlan = append(json.RawMessage(nil), snapshot.ReviewPlan...)
	if snapshot.AwaitingAnswer != nil {
		value := *snapshot.AwaitingAnswer
		clone.AwaitingAnswer = &value
	}
	if snapshot.CurrentQuestion != nil {
		value := *snapshot.CurrentQuestion
		clone.CurrentQuestion = &value
	}
	if snapshot.EndedReason != nil {
		value := *snapshot.EndedReason
		clone.EndedReason = &value
	}
	return clone
}

func splitDeltas(content string) []string {
	if len(content) < 2 {
		return []string{content}
	}
	middle := len(content) / 2
	return []string{content[:middle], content[middle:]}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

func directionMatchScore(direction Direction) float64 {
	if len(direction.Gaps) == 0 {
		return 100
	}
	total := len(direction.MatchedSkills) + len(direction.Gaps)
	if total == 0 {
		return 0
	}
	return float64(len(direction.MatchedSkills)) / float64(total) * 100
}
