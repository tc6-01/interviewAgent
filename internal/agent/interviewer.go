/**
 * @author: 公众号：IT杨秀才
 * @doc:后端，AI Agent知识进阶，后端、AI大模型、场景题面试大全：https://golangstar.cn/
 */

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	imodel "interview-agent/internal/model"
)

const interviewerSystemPrompt = `你是一位资深的技术面试官，风格专业但友善。你正在进行一场技术面试。

面试规则：
1. 每次只问一个问题，等候选人回答后再继续
2. 根据候选人回答质量决定是否追问
3. 对优秀的回答给予肯定，对不完整的回答进行引导
4. 保持专业、友善的语气
5. 不要直接告诉候选人答案

当前面试上下文：
- 岗位：%s
- 当前第 %d/%d 题
- 当前难度：%s
%s`

const updateProfilePrompt = `请基于以下信息更新候选人画像。要求：简洁、结构化、不超过200字。

%s

本轮新信息：
- 第 %d 题，考察技能：%s
- 得分：%.0f/100
- 命中要点：%s
- 遗漏要点：%s

请输出更新后的完整画像（纯文本，不要 JSON）。画像应包含：
1. 技能强项（哪些领域表现好）
2. 薄弱领域（哪些方面需加强）
3. 答题风格特征（如：偏理论/偏实践、善于举例/偏抽象等）`

const scorePrompt = `请对候选人的回答进行客观评分和反馈。

题目：%s
候选人回答：%s
参考答案要点：%s

【核心原则】严格基于候选人实际回答的内容进行评分：
- 只认定候选人明确表述出来的知识点，不要推测、脑补、或替候选人补充任何内容
- 候选人没有提到的知识点，一律算作遗漏（key_points_missed）
- 候选人说"不会"、"不知道"、"不太了解"、"跳过"等，得分应为 0-10 分
- 候选人回答偏题或答非所问，得分应为 0-20 分
- feedback 要指出候选人具体哪里答得好、哪里没有覆盖到，不要笼统夸奖

请先逐条对照参考答案要点，列出候选人命中了哪些、遗漏了哪些，再根据命中比例和深度给出分数。

请输出纯 JSON 格式：
{
  "score": <0-100的数值，根据下方评分标准和实际命中比例计算>,
  "feedback": "具体指出哪些点答得好、哪些点遗漏了",
  "key_points_hit": ["候选人明确提到的知识点1", "知识点2"],
  "key_points_missed": ["候选人未提到的知识点1", "知识点2"],
  "should_follow_up": true
}

评分标准：
- 90-100：完美回答，覆盖所有要点且有深度
- 70-89：良好回答，覆盖主要要点
- 50-69：基本回答，有明显遗漏
- 30-49：较差回答，只覆盖少量要点
- 0-29：未能回答或完全偏题`

// Interviewer 面试官 Agent，负责主持面试对话
type Interviewer struct {
	chatModel model.ChatModel
}

// NewInterviewer 创建面试官 Agent
func NewInterviewer(chatModel model.ChatModel) *Interviewer {
	return &Interviewer{chatModel: chatModel}
}

// AskQuestion 提出面试题目（支持流式输出）
func (iv *Interviewer) AskQuestion(ctx context.Context, state *imodel.InterviewState, question *imodel.PlannedQuestion, position string) (string, error) {
	profileSection := ""
	if state.CandidateProfile != "" {
		profileSection = fmt.Sprintf("\n候选人画像（根据前面的作答动态生成）：\n%s", state.CandidateProfile)
	}
	systemMsg := fmt.Sprintf(interviewerSystemPrompt,
		position,
		state.CurrentQuestion,
		state.TotalQuestions,
		state.CurrentDifficulty,
		profileSection,
	)

	// 构建对话历史
	messages := []*schema.Message{
		schema.SystemMessage(systemMsg),
	}

	// 添加之前的问答历史（最近 3 轮）
	historyStart := 0
	if len(state.QAHistory) > 3 {
		historyStart = len(state.QAHistory) - 3
	}
	for _, qa := range state.QAHistory[historyStart:] {
		messages = append(messages,
			schema.AssistantMessage(qa.Question.Content, nil),
			schema.UserMessage(qa.UserAnswer),
		)
	}

	// 添加当前要提出的问题指令
	messages = append(messages,
		schema.UserMessage(fmt.Sprintf("请以面试官的身份直接提出以下面试题，保持简洁，不要加额外的铺垫、背景说明或解释：\n\n%s", question.Content)),
	)

	resp, err := iv.chatModel.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("interviewer: ask question: %w", err)
	}

	return resp.Content, nil
}

// AskQuestionStream 提出面试题目（流式输出）
func (iv *Interviewer) AskQuestionStream(ctx context.Context, state *imodel.InterviewState, question *imodel.PlannedQuestion, position string) (*schema.StreamReader[*schema.Message], error) {
	profileSection := ""
	if state.CandidateProfile != "" {
		profileSection = fmt.Sprintf("\n候选人画像（根据前面的作答动态生成）：\n%s", state.CandidateProfile)
	}
	systemMsg := fmt.Sprintf(interviewerSystemPrompt,
		position,
		state.CurrentQuestion,
		state.TotalQuestions,
		state.CurrentDifficulty,
		profileSection,
	)

	messages := []*schema.Message{
		schema.SystemMessage(systemMsg),
	}

	historyStart := 0
	if len(state.QAHistory) > 3 {
		historyStart = len(state.QAHistory) - 3
	}
	for _, qa := range state.QAHistory[historyStart:] {
		messages = append(messages,
			schema.AssistantMessage(qa.Question.Content, nil),
			schema.UserMessage(qa.UserAnswer),
		)
	}

	messages = append(messages,
		schema.UserMessage(fmt.Sprintf("请以面试官的身份直接提出以下面试题，保持简洁，不要加额外的铺垫、背景说明或解释：\n\n%s", question.Content)),
	)

	stream, err := iv.chatModel.Stream(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("interviewer: stream question: %w", err)
	}

	return stream, nil
}

// ScoreAnswer 评估候选人的回答
func (iv *Interviewer) ScoreAnswer(ctx context.Context, question *imodel.PlannedQuestion, answer string) (*AnswerScore, error) {
	prompt := fmt.Sprintf(scorePrompt, question.Content, answer, question.Reference)

	messages := []*schema.Message{
		schema.UserMessage(prompt),
	}

	resp, err := iv.chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("interviewer: score answer: %w", err)
	}

	result := &AnswerScore{}
	content := extractJSON(resp.Content)
	if err := json.Unmarshal([]byte(content), result); err != nil {
		return nil, fmt.Errorf("interviewer: parse score: %w\nraw: %s", err, resp.Content)
	}

	return result, nil
}

// UpdateCandidateProfile 根据本轮评分结果更新候选人动态画像
func (iv *Interviewer) UpdateCandidateProfile(ctx context.Context, currentProfile string, questionNum int, question *imodel.PlannedQuestion, score *AnswerScore) (string, error) {
	prevProfile := "（首次作答，暂无历史画像）"
	if currentProfile != "" {
		prevProfile = "当前画像：\n" + currentProfile
	}

	messages := []*schema.Message{
		schema.UserMessage(fmt.Sprintf(updateProfilePrompt,
			prevProfile,
			questionNum,
			strings.Join(question.Skills, "、"),
			score.Score,
			strings.Join(score.KeyPointsHit, "、"),
			strings.Join(score.KeyPointsMissed, "、"),
		)),
	}

	resp, err := iv.chatModel.Generate(ctx, messages)
	if err != nil {
		return currentProfile, fmt.Errorf("interviewer: update profile: %w", err)
	}

	return resp.Content, nil
}

// FollowUp 基于候选人的实际回答动态生成追问
// question: 当前题目，answer: 候选人的实际回答，feedback: 评分反馈，missedPoints: 遗漏的知识点
func (iv *Interviewer) FollowUp(ctx context.Context, state *imodel.InterviewState, question *imodel.PlannedQuestion, answer string, feedback string, missedPoints []string, position string) (string, error) {
	profileSection := ""
	if state.CandidateProfile != "" {
		profileSection = fmt.Sprintf("\n候选人画像（根据前面的作答动态生成）：\n%s", state.CandidateProfile)
	}
	systemMsg := fmt.Sprintf(interviewerSystemPrompt,
		position,
		state.CurrentQuestion,
		state.TotalQuestions,
		state.CurrentDifficulty,
		profileSection,
	)

	messages := []*schema.Message{
		schema.SystemMessage(systemMsg),
		// 当前这轮的问答（不是历史里的，是正在进行的）
		schema.AssistantMessage(question.Content, nil),
		schema.UserMessage(answer),
	}

	prompt := fmt.Sprintf(`候选人的回答有部分遗漏，请基于以下信息生成一个简短的追问（一句话），引导候选人补充未覆盖的内容。

评分反馈：%s
遗漏的知识点：%s

要求：
- 追问必须基于候选人实际回答的内容来衔接，不要捏造候选人没说过的话
- 追问要简短自然，像真实面试官一样
- 不要重复候选人已经回答过的内容`, feedback, strings.Join(missedPoints, "、"))

	messages = append(messages, schema.UserMessage(prompt))

	resp, err := iv.chatModel.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("interviewer: follow up: %w", err)
	}

	return resp.Content, nil
}

// AnswerScore 回答评分结果
type AnswerScore struct {
	Score          float64  `json:"score"`
	Feedback       string   `json:"feedback"`
	KeyPointsHit   []string `json:"key_points_hit"`
	KeyPointsMissed []string `json:"key_points_missed"`
	ShouldFollowUp bool     `json:"should_follow_up"`
}

// CollectStreamContent 收集流式输出的完整内容
func CollectStreamContent(stream *schema.StreamReader[*schema.Message]) (string, error) {
	var content string
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return content, err
		}
		content += msg.Content
	}
	return content, nil
}
