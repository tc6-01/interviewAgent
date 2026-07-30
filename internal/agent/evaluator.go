/**
 * @author: 公众号：IT杨秀才
 * @doc:后端，AI Agent知识进阶，后端、AI大模型、场景题面试大全：https://golangstar.cn/
 */

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	imodel "interview-agent/internal/model"
)

const evaluatorPrompt = `你是一位经验丰富的面试评估专家。请根据候选人的完整面试表现，生成一份详细的评估报告。

请输出纯 JSON 格式：

{
  "overall_score": 75.0,
  "overall_level": "B",
  "dimension_score": {
    "������知识": 80.0,
    "项目��验": 70.0,
    "系统设计": 65.0,
    "编程能力": 75.0,
    "沟通表达": 80.0
  },
  "strengths": ["表现优秀的方面1", "方面2"],
  "weaknesses": ["需要提升的方面1", "方面2"],
  "detailed_review": [
    {
      "question_content": "题目内容",
      "user_answer": "候选人回答摘要",
      "score": 75.0,
      "comment": "点评",
      "key_points_hit": ["命中要点"],
      "key_points_missed": ["遗漏要��"]
    }
  ],
  "summary": "综合评语（2-3句话）"
}

评级标准：
- A（90-100）：表现出色，强烈推荐
- B（70-89）：表现良好，推荐
- C（50-69）���表现一般，需要提升
- D（0-49）：表现不佳，不推荐`

// Evaluator 评估 Agent，负责生成面试评估报告
type Evaluator struct {
	chatModel model.ChatModel
}

// NewEvaluator 创建评估 Agent
func NewEvaluator(chatModel model.ChatModel) *Evaluator {
	return &Evaluator{chatModel: chatModel}
}

// Evaluate 生成面试评估报告。userTerminated 表示面试是否由用户主动终止。
func (e *Evaluator) Evaluate(ctx context.Context, state *imodel.InterviewState, position string, candidateName string, userTerminated bool) (*imodel.EvaluationReport, error) {
	// 构建面试过程摘要
	var qaText string
	for i, qa := range state.QAHistory {
		qaText += fmt.Sprintf("### 第 %d 题（%s / %s）\n", i+1, qa.Question.Type, qa.Question.Difficulty)
		qaText += fmt.Sprintf("**题目**：%s\n", qa.Question.Content)
		qaText += fmt.Sprintf("**回答**：%s\n", qa.UserAnswer)
		qaText += fmt.Sprintf("**即时得分**：%.0f\n\n", qa.Score)
	}

	terminatedNote := ""
	if userTerminated {
		terminatedNote = fmt.Sprintf("\n\n> **注意：本次面试由候选人主动终止。原计划 %d 道题，实际完成 %d 道题。请在综合评语中说明面试未完成的情况，评估仅基于已作答题目。**\n",
			state.TotalQuestions, len(state.QAHistory))
	}

	userMsg := fmt.Sprintf("## 面试信息\n- 岗位：%s\n- 候选人：%s\n- 计划题目数：%d\n- 实际完成：%d\n- 面试状态：%s%s\n\n## 面试过程\n\n%s",
		position, candidateName, state.TotalQuestions, len(state.QAHistory),
		func() string {
			if userTerminated {
				return "用户主动终止"
			}
			return "正常完成"
		}(), terminatedNote, qaText)

	messages := []*schema.Message{
		schema.SystemMessage(evaluatorPrompt),
		schema.UserMessage(userMsg),
	}

	resp, err := e.chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("evaluator: generate: %w", err)
	}

	result := &imodel.EvaluationReport{}
	content := extractJSON(resp.Content)
	if err := json.Unmarshal([]byte(content), result); err != nil {
		return nil, fmt.Errorf("evaluator: parse response: %w\nraw: %s", err, resp.Content)
	}

	result.SessionID = state.SessionID
	result.CandidateName = candidateName
	result.Position = position
	result.CreatedAt = time.Now()

	return result, nil
}

// FormatReport 将评估报告格式化为 Markdown
func FormatReport(report *imodel.EvaluationReport) string {
	md := fmt.Sprintf("# 面试评估报告\n\n")
	md += fmt.Sprintf("- **候选人**：%s\n", report.CandidateName)
	md += fmt.Sprintf("- **目标岗位**：%s\n", report.Position)
	md += fmt.Sprintf("- **综合得分**：%.1f / 100（%s）\n", report.OverallScore, report.OverallLevel)
	md += fmt.Sprintf("- **评估时间**：%s\n\n", report.CreatedAt.Format("2006-01-02 15:04"))

	md += "## 各维度得分\n\n"
	md += "| 维度 | 得分 |\n|------|------|\n"
	for dim, score := range report.DimensionScore {
		md += fmt.Sprintf("| %s | %.1f |\n", dim, score)
	}

	md += "\n## 优势\n\n"
	for _, s := range report.Strengths {
		md += fmt.Sprintf("- %s\n", s)
	}

	md += "\n## 待提升\n\n"
	for _, w := range report.Weaknesses {
		md += fmt.Sprintf("- %s\n", w)
	}

	md += "\n## 逐题点评\n\n"
	for i, review := range report.DetailedReview {
		md += fmt.Sprintf("### 第 %d 题（%.0f分）\n", i+1, review.Score)
		md += fmt.Sprintf("**题目**：%s\n\n", review.QuestionContent)
		md += fmt.Sprintf("**点评**：%s\n\n", review.Comment)
	}

	md += fmt.Sprintf("\n## 综合评语\n\n%s\n", report.Summary)

	return md
}
