/**
 * @author: 公众号：IT杨秀才
 * @doc:后端，AI Agent知识进阶，后端、AI大模型、场景题面试大全：https://golangstar.cn/
 */

package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	imodel "interview-agent/internal/model"
)

const resumeMatcherPrompt = `你是一个专业的简历匹配分析专家。你需要将候选人的简历与目标岗位 JD 进行深度匹配分析。

请按照以下 JSON 格式输出匹配结果（不要输出其他内容，只输出纯 JSON）：

{
  "overall_score": 75.0,
  "skill_match": [
    {
      "skill_name": "技能名称",
      "required": true,
      "matched": true,
      "match_score": 80.0,
      "evidence": "从简历中找到的匹配证据"
    }
  ],
  "strengths": ["优势1", "优势2"],
  "weaknesses": ["薄弱点1", "薄弱点2"],
  "focus_areas": ["面试重点考察方向1", "面试重点考察方向2"],
  "resume_gaps": ["简历空白点1（可深挖的地方）"]
}

评分标准：
- overall_score: 0-100 分，综合考虑技能匹配度、经验相关性、项目质量
- skill_match: 逐项列出 JD 要求的技能，标注是否在简历中匹配到
- strengths: 候选人明显的优势（与岗位相关的）
- weaknesses: 候选人的不足或需要提升的地方
- focus_areas: 基于匹配分析推荐的面试重点考察方向
- resume_gaps: 简历中可以深挖或追问的空白点`

// ResumeMatcher 简历匹配 Agent，负责分析简历与 JD 的匹配度
type ResumeMatcher struct {
	chatModel model.ChatModel
}

// NewResumeMatcher 创建简历匹配 Agent
func NewResumeMatcher(chatModel model.ChatModel) *ResumeMatcher {
	return &ResumeMatcher{chatModel: chatModel}
}

// Match 分析简历与 JD 的匹配度
func (m *ResumeMatcher) Match(ctx context.Context, jdAnalysis *imodel.JDAnalysis, resume *imodel.Resume) (*imodel.ResumeMatchResult, error) {
	// 构造 JD 分析摘要
	jdSummary := formatJDForMatching(jdAnalysis)
	resumeSummary := formatResumeForMatching(resume)

	messages := []*schema.Message{
		schema.SystemMessage(resumeMatcherPrompt),
		schema.UserMessage(fmt.Sprintf("## 岗位 JD 分析结果\n\n%s\n\n## 候选人简历\n\n%s", jdSummary, resumeSummary)),
	}

	resp, err := m.chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("resume_matcher: generate: %w", err)
	}

	result := &imodel.ResumeMatchResult{}
	content := extractJSON(resp.Content)
	if err := json.Unmarshal([]byte(content), result); err != nil {
		return nil, fmt.Errorf("resume_matcher: parse response: %w\nraw: %s", err, resp.Content)
	}

	return result, nil
}

// formatJDForMatching 将 JD 分析结果格式化为便于匹配的文本
func formatJDForMatching(jd *imodel.JDAnalysis) string {
	data, _ := json.MarshalIndent(jd, "", "  ")
	return string(data)
}

// formatResumeForMatching 将简历格式化为便于匹配的文本
func formatResumeForMatching(resume *imodel.Resume) string {
	if resume.RawText != "" {
		return resume.RawText
	}
	data, _ := json.MarshalIndent(resume, "", "  ")
	return string(data)
}
