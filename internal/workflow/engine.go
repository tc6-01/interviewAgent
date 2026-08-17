package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/google/uuid"

	coreagent "interview-agent/internal/core/agent"
	"interview-agent/internal/domain"
	"interview-agent/internal/session"
)

type Engine struct {
	agent      *coreagent.Runtime
	repository domain.Repository
	index      domain.QuestionIndex
}

func New(agent *coreagent.Runtime, repository domain.Repository, index domain.QuestionIndex) (*Engine, error) {
	if agent == nil || repository == nil || index == nil {
		return nil, fmt.Errorf("workflow: agent, repository and question index are required")
	}
	return &Engine{agent: agent, repository: repository, index: index}, nil
}

type directionPayload struct {
	Position        string   `json:"position"`
	ExperienceLevel string   `json:"experience_level"`
	FocusAreas      []string `json:"focus_areas"`
	MatchedSkills   []string `json:"matched_skills"`
	Gaps            []string `json:"gaps"`
}

func (r *Engine) GenerateDirection(ctx context.Context, subjectID, jdText, resumeText string) (session.Direction, error) {
	var payload directionPayload
	graph := compose.NewGraph[string, string]()
	if err := graph.AddLambdaNode("jd_resume_analysis", compose.InvokableLambda(func(nodeCtx context.Context, _ string) (string, error) {
		started := time.Now()
		defer r.logNode("jd_resume_analysis", started)
		return r.completeStructured(nodeCtx, domain.LLMRequest{
			Operation: "direction.generate", JSON: true,
			SystemPrompt: "你是模拟面试方向规划器。仅返回 JSON，不复述简历或 JD。字段必须为 position、experience_level、focus_areas、matched_skills、gaps；数组必须存在，focus_areas 和 matched_skills 非空。",
			UserPrompt:   "JD:\n" + jdText + "\n\nResume:\n" + resumeText,
		}, validateDirectionPayload)
	})); err != nil {
		return session.Direction{}, fmt.Errorf("graph: add direction node: %w", err)
	}
	if err := graph.AddLambdaNode("direction_schema", compose.InvokableLambda(func(_ context.Context, raw string) (string, error) {
		started := time.Now()
		defer r.logNode("direction_schema", started)
		if err := json.Unmarshal([]byte(extractJSON(raw)), &payload); err != nil {
			return "", fmt.Errorf("graph: decode direction: %w", err)
		}
		if err := validateDirectionPayload(payload); err != nil {
			return "", err
		}
		return raw, nil
	})); err != nil {
		return session.Direction{}, fmt.Errorf("graph: add direction schema node: %w", err)
	}
	if err := graph.AddEdge(compose.START, "jd_resume_analysis"); err != nil {
		return session.Direction{}, err
	}
	if err := graph.AddEdge("jd_resume_analysis", "direction_schema"); err != nil {
		return session.Direction{}, err
	}
	if err := graph.AddEdge("direction_schema", compose.END); err != nil {
		return session.Direction{}, err
	}
	runnable, err := graph.Compile(ctx)
	if err != nil {
		return session.Direction{}, fmt.Errorf("graph: compile direction DAG: %w", err)
	}
	if _, err := runnable.Invoke(ctx, ""); err != nil {
		return session.Direction{}, err
	}
	return session.Direction{
		SubjectID: subjectID, Position: payload.Position, ExperienceLevel: payload.ExperienceLevel,
		FocusAreas: payload.FocusAreas, MatchedSkills: payload.MatchedSkills, Gaps: payload.Gaps,
	}, nil
}

func (r *Engine) Prepare(ctx context.Context, input session.CreateInput) ([]session.Question, error) {
	if input.Direction == nil {
		return nil, fmt.Errorf("graph: confirmed direction is required")
	}
	var selected []domain.Question
	var assembled []session.Question
	graph := compose.NewGraph[string, string]()
	if err := graph.AddLambdaNode("question_plan", compose.InvokableLambda(func(_ context.Context, _ string) (string, error) {
		started := time.Now()
		defer r.logNode("question_plan", started)
		plan := domain.FixedQuestionPlan()
		encoded, _ := json.Marshal(plan)
		return string(encoded), nil
	})); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode("rag_retrieval", compose.InvokableLambda(func(nodeCtx context.Context, _ string) (string, error) {
		started := time.Now()
		defer r.logNode("rag_retrieval", started)
		questions, err := r.retrieveQuestions(nodeCtx, input)
		if err != nil {
			return "", err
		}
		selected = questions
		return fmt.Sprint(len(questions)), nil
	})); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode("question_assemble", compose.InvokableLambda(func(_ context.Context, _ string) (string, error) {
		started := time.Now()
		defer r.logNode("question_assemble", started)
		for index, question := range selected {
			assembled = append(assembled, session.Question{
				PromptID: fmt.Sprintf("prompt_%d_main", index+1), Number: index + 1, Kind: "primary",
				Content: question.Text, Source: question.Source,
			})
		}
		if len(assembled) != 15 {
			return "", fmt.Errorf("graph: assembled %d questions, want 15", len(assembled))
		}
		return "ready", nil
	})); err != nil {
		return nil, err
	}
	for _, edge := range [][2]string{{compose.START, "question_plan"}, {"question_plan", "rag_retrieval"}, {"rag_retrieval", "question_assemble"}, {"question_assemble", compose.END}} {
		if err := graph.AddEdge(edge[0], edge[1]); err != nil {
			return nil, err
		}
	}
	runnable, err := graph.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("graph: compile interview DAG: %w", err)
	}
	if _, err := runnable.Invoke(ctx, ""); err != nil {
		return nil, err
	}
	return assembled, nil
}

func (r *Engine) retrieveQuestions(ctx context.Context, input session.CreateInput) ([]domain.Question, error) {
	query := strings.Join(append([]string{input.Direction.Position, input.Direction.ExperienceLevel}, input.Direction.FocusAreas...), " ")
	plan := domain.FixedQuestionPlan()
	var result []domain.Question
	seen := map[string]bool{}
	for _, questionType := range []domain.QuestionType{domain.QuestionTypeBasic, domain.QuestionTypeExperience, domain.QuestionTypeDesign} {
		wanted := plan.Counts[questionType]
		hits, err := r.index.Search(ctx, domain.SearchRequest{
			Scopes: []string{"user:" + input.SubjectID, "builtin"}, Query: query, Types: []domain.QuestionType{questionType}, Limit: wanted * 3,
		})
		if err != nil {
			return nil, err
		}
		for _, hit := range hits {
			if len(result) >= plan.Total || seen[hit.Question.ID] || countType(result, questionType) >= wanted {
				continue
			}
			seen[hit.Question.ID] = true
			result = append(result, hit.Question)
		}
		for countType(result, questionType) < wanted {
			number := countType(result, questionType) + 1
			question, err := r.generateQuestion(ctx, query, questionType, number)
			if err != nil {
				question = domain.Question{
					ID: uuid.NewString(), Type: questionType, Topic: input.Direction.FocusAreas[0],
					Text:   fmt.Sprintf("请结合实际项目说明你对 %s 的理解、取舍与落地结果。", input.Direction.FocusAreas[(number-1)%len(input.Direction.FocusAreas)]),
					Source: "llm:fallback-degraded",
				}
			}
			result = append(result, question)
		}
	}
	return result, nil
}

func (r *Engine) generateQuestion(ctx context.Context, query string, questionType domain.QuestionType, number int) (domain.Question, error) {
	var payload struct {
		Topic string `json:"topic"`
		Text  string `json:"text"`
	}
	raw, err := r.completeStructured(ctx, domain.LLMRequest{
		Operation: "question.generate", JSON: true,
		SystemPrompt: "生成一道模拟面试题，仅返回 JSON：topic、text。题目要有明确考察目标，不返回答案。",
		UserPrompt:   fmt.Sprintf("方向：%s\n题型：%s\n序号：%d", query, questionType, number),
	}, func(value struct {
		Topic string `json:"topic"`
		Text  string `json:"text"`
	}) error {
		if strings.TrimSpace(value.Topic) == "" || strings.TrimSpace(value.Text) == "" {
			return fmt.Errorf("question topic and text are required")
		}
		return nil
	})
	if err != nil {
		return domain.Question{}, err
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &payload); err != nil {
		return domain.Question{}, err
	}
	return domain.Question{ID: uuid.NewString(), Type: questionType, Topic: payload.Topic, Text: payload.Text, Source: "llm:retrieval-gap"}, nil
}

func (r *Engine) Score(ctx context.Context, _ session.Snapshot, question session.Question, answer string) (session.Score, error) {
	var score session.Score
	raw, err := r.completeStructured(ctx, domain.LLMRequest{
		Operation: "answer.score", JSON: true,
		SystemPrompt: "你是严格但友善的面试评分器。只返回 JSON：score(0-100)、feedback、key_points_hit、key_points_missed、follow_up_needed。",
		UserPrompt:   "Question:\n" + question.Content + "\n\nAnswer:\n" + answer,
	}, func(value session.Score) error {
		if value.Value < 0 || value.Value > 100 || strings.TrimSpace(value.Feedback) == "" {
			return fmt.Errorf("score is outside schema")
		}
		return nil
	})
	if err != nil {
		return session.Score{}, err
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &score); err != nil {
		return session.Score{}, err
	}
	return score, nil
}

func (r *Engine) FollowUp(ctx context.Context, _ session.Snapshot, question session.Question, answer string, score session.Score) (*session.Question, error) {
	if !score.FollowUpNeeded || question.Kind == "followup" {
		return nil, nil
	}
	var payload struct {
		Question string `json:"question"`
	}
	raw, err := r.completeStructured(ctx, domain.LLMRequest{
		Operation: "question.follow_up", JSON: true,
		SystemPrompt: "根据失分点生成一道简短追问，仅返回 JSON：question。不要重复原题。",
		UserPrompt:   "Original question:\n" + question.Content + "\nAnswer:\n" + answer + "\nMissed:\n" + strings.Join(score.KeyPointsMissed, ", "),
	}, func(value struct {
		Question string `json:"question"`
	}) error {
		if strings.TrimSpace(value.Question) == "" {
			return fmt.Errorf("follow-up question is required")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &payload); err != nil {
		return nil, err
	}
	return &session.Question{
		PromptID: fmt.Sprintf("prompt_%d_followup", question.Number), Number: question.Number, Kind: "followup", Content: payload.Question, Source: "llm:follow-up",
	}, nil
}

func (r *Engine) Report(_ context.Context, snapshot session.Snapshot) (session.Artifact, error) {
	if len(snapshot.QAHistory) == 0 {
		return session.Artifact{}, fmt.Errorf("graph: report requires at least one answer")
	}
	primary := primaryRecords(snapshot.QAHistory)
	overall := averageScore(primary)
	dimension := map[string]float64{}
	for _, kind := range []string{"basic", "experience", "design"} {
		var scores []float64
		for _, item := range primary {
			if strings.Contains(item.Question, kind) {
				scores = append(scores, item.Score)
			}
		}
		if len(scores) > 0 {
			dimension[kind] = average(scores)
		}
	}
	sorted := append([]session.QARecord(nil), primary...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Score < sorted[j].Score })
	weaknesses := make([]string, 0, 3)
	evidence := make([]map[string]any, 0, 3)
	for _, item := range sorted {
		if item.Score >= 75 && len(weaknesses) > 0 {
			break
		}
		topic := fmt.Sprintf("第 %d 题失分点", item.Number)
		if len(item.KeyPointsMissed) > 0 {
			topic = item.KeyPointsMissed[0]
		}
		weaknesses = append(weaknesses, fmt.Sprintf("%s（第 %d 题，%.0f 分）", topic, item.Number, item.Score))
		evidence = append(evidence, map[string]any{"topic": topic, "prompt_ids": []string{item.PromptID}, "question_no": item.Number, "score": item.Score})
		if len(weaknesses) == 3 {
			break
		}
	}
	strengths := make([]string, 0, 3)
	for index := len(sorted) - 1; index >= 0 && len(strengths) < 3; index-- {
		item := sorted[index]
		if item.Score < 75 {
			continue
		}
		strengths = append(strengths, fmt.Sprintf("第 %d 题表现稳定（%.0f 分）", item.Number, item.Score))
	}
	report := map[string]any{
		"interview_id": snapshot.InterviewID, "position": directionPosition(snapshot), "overall_score": overall,
		"overall_level": scoreLevel(overall), "dimension_scores": dimension, "strengths": strengths, "weaknesses": weaknesses,
		"weakness_evidence": evidence, "detailed_review": detailedReview(snapshot.QAHistory),
		"summary": fmt.Sprintf("完成 %d 道主问题，综合得分 %.0f。", len(primary), overall), "created_at": time.Now().UTC(),
	}
	if overall >= 85 {
		report["advanced_directions"] = []string{"复杂系统边界与容量模型", "跨团队技术决策与演进路线", "故障演练与韧性工程"}
	}
	value, _ := json.Marshal(report)
	return session.Artifact{Markdown: reportMarkdown(report), Value: value}, nil
}

type reviewPlanPayload struct {
	StudyPlan []struct {
		Topic        string   `json:"topic"`
		Objective    string   `json:"objective"`
		Actions      []string `json:"actions"`
		TimeEstimate string   `json:"time_estimate"`
	} `json:"study_plan"`
	Resources []struct {
		Title string `json:"title"`
		URL   string `json:"url,omitempty"`
		Type  string `json:"type"`
		Desc  string `json:"desc"`
	} `json:"resources"`
	AdvancedDirections []string `json:"advanced_directions,omitempty"`
}

func (r *Engine) ReviewPlan(ctx context.Context, snapshot session.Snapshot) (session.Artifact, error) {
	primary := primaryRecords(snapshot.QAHistory)
	if len(primary) == 0 || snapshot.ReportStatus != session.ArtifactReady {
		return session.Artifact{}, fmt.Errorf("graph: review plan prerequisites are not ready")
	}
	weakRecords := append([]session.QARecord(nil), primary...)
	sort.SliceStable(weakRecords, func(i, j int) bool { return weakRecords[i].Score < weakRecords[j].Score })
	if len(weakRecords) > 3 {
		weakRecords = weakRecords[:3]
	}
	weakInput, _ := json.Marshal(weakRecords)
	var payload reviewPlanPayload
	raw, err := r.completeStructured(ctx, domain.LLMRequest{
		Operation: "review_plan.generate", JSON: true,
		SystemPrompt: "生成可执行复习计划，仅返回 JSON：study_plan、resources、advanced_directions。资源优先官方文档或稳定开源仓库；不确定 URL 时省略 url。",
		UserPrompt:   "Weak scored questions:\n" + string(weakInput) + fmt.Sprintf("\nOverall score: %.0f", averageScore(primary)),
	}, validateReviewPlanPayload)
	if err != nil {
		return session.Artifact{}, err
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &payload); err != nil {
		return session.Artifact{}, err
	}
	weakAreas := make([]map[string]any, 0, len(weakRecords))
	for _, item := range weakRecords {
		topic := fmt.Sprintf("第 %d 题失分点", item.Number)
		if len(item.KeyPointsMissed) > 0 {
			topic = item.KeyPointsMissed[0]
		}
		priority := "low"
		if item.Score < 60 {
			priority = "high"
		} else if item.Score < 75 {
			priority = "medium"
		}
		weakAreas = append(weakAreas, map[string]any{
			"topic": topic, "score": item.Score, "priority": priority, "source_prompt_ids": []string{item.PromptID}, "question_no": item.Number,
		})
	}
	resources := make([]map[string]any, 0, len(payload.Resources))
	for _, resource := range payload.Resources {
		entry := map[string]any{"title": resource.Title, "type": resource.Type, "desc": resource.Desc}
		if stableResourceURL(resource.URL) {
			entry["url"] = resource.URL
		}
		resources = append(resources, entry)
	}
	plan := map[string]any{
		"interview_id": snapshot.InterviewID, "weak_areas": weakAreas, "study_plan": payload.StudyPlan,
		"resources": resources, "created_at": time.Now().UTC(),
	}
	if averageScore(primary) >= 85 {
		advanced := payload.AdvancedDirections
		if len(advanced) == 0 {
			advanced = []string{"复杂系统设计", "性能与成本联合优化", "技术领导力场景"}
		}
		plan["advanced_directions"] = advanced
	}
	value, _ := json.Marshal(plan)
	return session.Artifact{Markdown: reviewPlanMarkdown(plan), Value: value}, nil
}

func (r *Engine) completeStructured(ctx context.Context, request domain.LLMRequest, validate any) (string, error) {
	response, err := r.agent.Complete(ctx, request)
	if err != nil {
		return "", err
	}
	if err := decodeAndValidate(response.Content, validate); err == nil {
		r.logLLM(request.Operation, response)
		return response.Content, nil
	}
	repair := domain.LLMRequest{
		Operation: request.Operation + ".repair", JSON: true,
		SystemPrompt: "修复以下 JSON，使其严格满足原始字段和类型约束。只返回修复后的 JSON，不添加说明。",
		UserPrompt:   response.Content,
	}
	repaired, repairErr := r.agent.Complete(ctx, repair)
	if repairErr != nil {
		return "", repairErr
	}
	if err := decodeAndValidate(repaired.Content, validate); err != nil {
		return "", fmt.Errorf("graph: structured response invalid after one repair: %w", err)
	}
	r.logLLM(repair.Operation, repaired)
	return repaired.Content, nil
}

func decodeAndValidate(raw string, validate any) error {
	value := reflectNew(validate)
	if err := json.Unmarshal([]byte(extractJSON(raw)), value); err != nil {
		return err
	}
	switch validator := validate.(type) {
	case func(directionPayload) error:
		return validator(*value.(*directionPayload))
	case func(session.Score) error:
		return validator(*value.(*session.Score))
	case func(reviewPlanPayload) error:
		return validator(*value.(*reviewPlanPayload))
	case func(struct {
		Topic string `json:"topic"`
		Text  string `json:"text"`
	}) error:
		return validator(*value.(*struct {
			Topic string `json:"topic"`
			Text  string `json:"text"`
		}))
	case func(struct {
		Question string `json:"question"`
	}) error:
		return validator(*value.(*struct {
			Question string `json:"question"`
		}))
	default:
		return fmt.Errorf("graph: unsupported validator")
	}
}

func reflectNew(validate any) any {
	switch validate.(type) {
	case func(directionPayload) error:
		return &directionPayload{}
	case func(session.Score) error:
		return &session.Score{}
	case func(reviewPlanPayload) error:
		return &reviewPlanPayload{}
	case func(struct {
		Topic string `json:"topic"`
		Text  string `json:"text"`
	}) error:
		return &struct {
			Topic string `json:"topic"`
			Text  string `json:"text"`
		}{}
	case func(struct {
		Question string `json:"question"`
	}) error:
		return &struct {
			Question string `json:"question"`
		}{}
	default:
		return &struct{}{}
	}
}

func validateDirectionPayload(value directionPayload) error {
	if strings.TrimSpace(value.Position) == "" || strings.TrimSpace(value.ExperienceLevel) == "" || len(value.FocusAreas) == 0 || len(value.MatchedSkills) == 0 || value.Gaps == nil {
		return fmt.Errorf("direction response is incomplete")
	}
	return nil
}

func validateReviewPlanPayload(value reviewPlanPayload) error {
	if len(value.StudyPlan) == 0 {
		return fmt.Errorf("review plan study_plan is required")
	}
	for _, item := range value.StudyPlan {
		if strings.TrimSpace(item.Topic) == "" || strings.TrimSpace(item.Objective) == "" || len(item.Actions) == 0 || strings.TrimSpace(item.TimeEstimate) == "" {
			return fmt.Errorf("review plan item is incomplete")
		}
	}
	return nil
}

func extractJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	start := strings.IndexAny(trimmed, "{[")
	if start > 0 {
		trimmed = trimmed[start:]
	}
	return strings.TrimSpace(trimmed)
}

func (r *Engine) logNode(node string, started time.Time) {
	slog.Default().Info("interview DAG node", "node", node, "duration_ms", time.Since(started).Milliseconds())
}

func (r *Engine) logLLM(operation string, response domain.LLMResponse) {
	slog.Default().Info("LLM operation", "operation", operation, "retries", response.Retries,
		"prompt_tokens", response.Usage.PromptTokens, "completion_tokens", response.Usage.CompletionTokens)
}

func countType(questions []domain.Question, questionType domain.QuestionType) int {
	count := 0
	for _, question := range questions {
		if question.Type == questionType {
			count++
		}
	}
	return count
}

func primaryRecords(records []session.QARecord) []session.QARecord {
	result := make([]session.QARecord, 0, len(records))
	for _, record := range records {
		if record.Kind == "primary" {
			result = append(result, record)
		}
	}
	return result
}

func averageScore(records []session.QARecord) float64 {
	scores := make([]float64, 0, len(records))
	for _, record := range records {
		if !record.ScoreDegraded {
			scores = append(scores, record.Score)
		}
	}
	return average(scores)
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return math.Round(total/float64(len(values))*10) / 10
}

func scoreLevel(score float64) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B+"
	case score >= 70:
		return "B"
	case score >= 60:
		return "C"
	default:
		return "D"
	}
}

func directionPosition(snapshot session.Snapshot) string {
	if snapshot.Direction != nil {
		return snapshot.Direction.Position
	}
	return "Unknown position"
}

func detailedReview(records []session.QARecord) []map[string]any {
	result := make([]map[string]any, 0, len(records))
	for _, item := range records {
		result = append(result, map[string]any{
			"prompt_id": item.PromptID, "question_content": item.Question, "user_answer": item.Answer, "score": item.Score,
			"comment": item.Feedback, "key_points_hit": item.KeyPointsHit, "key_points_missed": item.KeyPointsMissed,
		})
	}
	return result
}

func stableResourceURL(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, stable := range []string{"go.dev", "pkg.go.dev", "github.com", "sre.google", "cloud.google.com", "developer.mozilla.org", "kubernetes.io"} {
		if host == stable || strings.HasSuffix(host, "."+stable) {
			return true
		}
	}
	return false
}

func reportMarkdown(report map[string]any) string {
	return fmt.Sprintf("# 面试评估报告\n\n- 综合得分：%v\n- 等级：%v\n\n%s", report["overall_score"], report["overall_level"], report["summary"])
}

func reviewPlanMarkdown(plan map[string]any) string {
	return fmt.Sprintf("# 复习计划\n\n已基于 %d 个失分证据生成针对性计划。", len(plan["weak_areas"].([]map[string]any)))
}
