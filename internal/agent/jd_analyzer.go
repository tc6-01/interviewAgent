/**
 * @author: 公众号：IT杨秀才
 * @doc:后端，AI Agent知识进阶，后端、AI大模型、场景题面试大全：https://golangstar.cn/
 */

// Package agent 实现 InterviewAgent 系统的各个 Agent
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	imodel "interview-agent/internal/model"
)

const jdAnalyzerPrompt = `你是一个专业的 JD（职位描述）分析专家。请仔细分析以下职位描述，提取关键信息。

请按照以下 JSON 格式输出分析结果（不要输出其他内容，只输出纯 JSON）：

{
  "position": "岗位名称",
  "company": "公司名称（如果JD中有提及）",
  "required_skills": [
    {"name": "技能名称", "category": "language/framework/database/cloud/other", "importance": "must"}
  ],
  "preferred_skills": [
    {"name": "技能名称", "category": "language/framework/database/cloud/other", "importance": "preferred"}
  ],
  "experience_level": "junior/mid/senior",
  "responsibilities": ["职责1", "职责2"],
  "key_topics": ["面试重点方向1", "面试重点方向2"]
}

注意：
1. required_skills 是 JD 中明确要求的必须技能
2. preferred_skills 是"加分项"或"优先考虑"的技能
3. experience_level 根据工作年限和岗位级别判断
4. key_topics 是基于 JD 推断出的面试重点考察方向`

// JDAnalyzer JD 分析 Agent，负责解析岗位描述并提取结构化信息
type JDAnalyzer struct {
	chatModel model.ChatModel
}

// NewJDAnalyzer 创建 JD 分析 Agent
func NewJDAnalyzer(chatModel model.ChatModel) *JDAnalyzer {
	return &JDAnalyzer{chatModel: chatModel}
}

// Analyze 分析 JD 文本，返回结构化的分析结果
func (a *JDAnalyzer) Analyze(ctx context.Context, jdText string) (*imodel.JDAnalysis, error) {
	messages := []*schema.Message{
		schema.SystemMessage(jdAnalyzerPrompt),
		schema.UserMessage(fmt.Sprintf("请分析以下 JD：\n\n%s", jdText)),
	}

	resp, err := a.chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("jd_analyzer: generate: %w", err)
	}

	// 解析 JSON 响应
	result := &imodel.JDAnalysis{}
	content := extractJSON(resp.Content)
	if err := json.Unmarshal([]byte(content), result); err != nil {
		return nil, fmt.Errorf("jd_analyzer: parse response: %w\nraw: %s", err, resp.Content)
	}

	result.RawJD = jdText
	return result, nil
}

// extractJSON 从可能包含 markdown 代码块的文本中提取 JSON
func extractJSON(text string) string {
	// 尝试提取 ```json ... ``` 中的内容
	start := -1
	for i := 0; i < len(text)-2; i++ {
		if text[i] == '{' {
			start = i
			break
		}
	}
	if start == -1 {
		return text
	}

	// 找到最后一个 }
	end := -1
	for i := len(text) - 1; i >= start; i-- {
		if text[i] == '}' {
			end = i + 1
			break
		}
	}
	if end == -1 {
		return text
	}

	return text[start:end]
}
