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

// Phase 1 prompt：根据 JD + 简历规划出题方向
const directionPlannerPrompt = `你是一个资深的技术面试出题规划专家。根据 JD 分析和简历匹配结果，规划面试的出题方向。

你的任务是：为每道题确定一个考察方向/考点，而不是出具体的题目。

规划原则：
1. 【数量硬性要求，必须严格遵守，每个类型都要按难度分档铺满】：
   - basic 类：每个难度档各 5 个 —— easy 5 个、medium 5 个、hard 5 个，共 15 个
   - experience 类：每个难度档各 4 个 —— easy 4 个、medium 4 个、hard 4 个，共 12 个
   - design 类：medium 2 个、hard 2 个，共 4 个
   - 说明：以上是"候选题池"，面试时会按候选人实时表现自适应抽取，不要求全部问完；
     因此每档必须铺满，保证同一难度有足够候选可供持续抽取
2. 题型包含三类（出题方向以候选人简历的技术栈和项目经历为主，JD 要求为辅）：
   - basic：候选人简历涉及的核心技术知识（如语言特性、框架原理、中间件、数据库等），结合 JD 要求确定考察重点
   - experience：针对候选人简历中的工作经历、实习经历、项目经历的考察方向（必须基于简历真实内容）
   - design：系统设计、架构设计类方向，结合简历中的项目背景出题
3. 每个方向给出一个用于题库检索的关键词（search_query），要简洁精准（如"MySQL索引优化"、"Go channel原理"）
4. experience 类方向必须基于简历中的真实信息，context 字段填写简历中的相关内容摘要
5. 每个方向的 difficulty 必须标注准确，且严格符合第 1 条按难度分档的数量配额（同一 type 下 easy/medium/hard 的方向数量必须达标）
6. 【严禁幻觉】experience 类必须严格基于简历中的真实信息，不得杜撰或假设简历中未提及的技术细节

请按以下 JSON 格式输出（不要输出其他内容）：

{
  "directions": [
    {
      "topic": "考察方向描述（如：Go sync.Map 的并发安全机制）",
      "type": "basic/experience/design",
      "difficulty": "easy/medium/hard",
      "search_query": "题库检索关键词（如：sync.Map 并发）",
      "skills": ["考察的技能点"],
      "context": "简历中相关上下文（experience 类必填，其他类型可为空）"
    }
  ]
}`

// Phase 2 prompt：根据方向 + 题库匹配结果生成最终题目
const questionAssemblerPrompt = `你是一个资深的技术面试出题专家。根据出题方向和题库匹配结果，生成最终的面试题目。

规则：
1. 【数量严格对应，最重要的规则】每个出题方向必须对应生成恰好一道题目，不得合并、删减或跳过任何方向。输入 N 个方向就必须输出 N 道题
2. 如果提供了题库匹配的原题，直接使用原题（content 完全照搬不得改编），source 填题目 ID
3. 如果没有匹配到题库原题，由你根据出题方向自行出题，source 填 "llm"
4. 【LLM 出题基于简历】当 LLM 自行出题时，必须结合候选人简历的技术栈和项目经历来出题，确保题目与候选人背景相关
5. 【严禁幻觉】experience 类题目必须严格基于简历中的真实信息提问，不得杜撰
6. 题目 content 必须简洁精炼，一句话直击考察要点
7. 每道题准备 1-2 个追问，用于深入考察
8. 【难度沿用】每道题的 difficulty 必须与其对应出题方向给定的 difficulty 完全一致，不得更改，以保持整体难度分布的梯度

请按以下 JSON 格式输出（不要输出其他内容）：

{
  "total_questions": 10,
  "distribution": {
    "basic": 0,
    "experience": 0,
    "design": 0
  },
  "questions": [
    {
      "id": "q1",
      "content": "题目内容",
      "type": "basic/experience/design",
      "difficulty": "easy/medium/hard",
      "skills": ["考察的技能点"],
      "follow_ups": ["追问1", "追问2"],
      "reference": "参考答案要点",
      "source": "题库原题ID 或 llm"
    }
  ]
}`

// QuestionPlanner 出题规划 Agent
type QuestionPlanner struct {
	chatModel model.ChatModel
}

// NewQuestionPlanner 创建出题规划 Agent
func NewQuestionPlanner(chatModel model.ChatModel) *QuestionPlanner {
	return &QuestionPlanner{chatModel: chatModel}
}

// PlanDirections Phase 1：根据 JD + 简历规划出题方向
func (p *QuestionPlanner) PlanDirections(ctx context.Context, jd *imodel.JDAnalysis, match *imodel.ResumeMatchResult, weakPoints string) (*imodel.QuestionDirectionPlan, error) {
	jdJSON, _ := json.MarshalIndent(jd, "", "  ")
	matchJSON, _ := json.MarshalIndent(match, "", "  ")

	userMsg := fmt.Sprintf("## JD 分析结果\n\n%s\n\n## 简历匹配结果\n\n%s", string(jdJSON), string(matchJSON))

	if weakPoints != "" {
		userMsg += fmt.Sprintf("\n\n## 候选人历史薄弱点（请针对性加强考察）\n\n%s", weakPoints)
	}

	messages := []*schema.Message{
		schema.SystemMessage(directionPlannerPrompt),
		schema.UserMessage(userMsg),
	}

	resp, err := p.chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("question_planner: plan directions: %w", err)
	}

	result := &imodel.QuestionDirectionPlan{}
	content := extractJSON(resp.Content)
	if err := json.Unmarshal([]byte(content), result); err != nil {
		return nil, fmt.Errorf("question_planner: parse directions: %w\nraw: %s", err, resp.Content)
	}

	return result, nil
}

// AssembleQuestions Phase 2：根据方向 + 题库匹配结果生成最终题目
// directionDocs: 每个方向索引对应的题库匹配文档（可能为空）
func (p *QuestionPlanner) AssembleQuestions(ctx context.Context, jd *imodel.JDAnalysis, match *imodel.ResumeMatchResult, directions *imodel.QuestionDirectionPlan, directionDocs []string) (*imodel.QuestionPlan, error) {
	jdJSON, _ := json.MarshalIndent(jd, "", "  ")
	matchJSON, _ := json.MarshalIndent(match, "", "  ")

	// 构建每个方向 + 对应的题库匹配情况
	var directionsText string
	for i, d := range directions.Directions {
		directionsText += fmt.Sprintf("### 方向 %d: %s\n", i+1, d.Topic)
		directionsText += fmt.Sprintf("- 类型: %s, 难度: %s, 技能: %v\n", d.Type, d.Difficulty, d.Skills)
		if d.Context != "" {
			directionsText += fmt.Sprintf("- 简历上下文: %s\n", d.Context)
		}
		if i < len(directionDocs) && directionDocs[i] != "" {
			directionsText += fmt.Sprintf("- 题库匹配原题:\n%s\n", directionDocs[i])
		} else {
			directionsText += "- 题库匹配: 无匹配，请 LLM 自行出题\n"
		}
		directionsText += "\n"
	}

	userMsg := fmt.Sprintf("## JD 分析结果\n\n%s\n\n## 简历匹配结果\n\n%s\n\n## 出题方向与题库匹配\n\n%s",
		string(jdJSON), string(matchJSON), directionsText)

	messages := []*schema.Message{
		schema.SystemMessage(questionAssemblerPrompt),
		schema.UserMessage(userMsg),
	}

	resp, err := p.chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("question_planner: assemble questions: %w", err)
	}

	result := &imodel.QuestionPlan{}
	content := extractJSON(resp.Content)
	if err := json.Unmarshal([]byte(content), result); err != nil {
		return nil, fmt.Errorf("question_planner: parse questions: %w\nraw: %s", err, resp.Content)
	}

	return result, nil
}

// AdjustDifficulty 根据面试状态动态调整后续题目难度
func (p *QuestionPlanner) AdjustDifficulty(state *imodel.InterviewState) imodel.DifficultyLevel {
	// 连续答对 2 题以上 → 提高难度
	if state.ConsecutiveRight >= 2 {
		switch state.CurrentDifficulty {
		case imodel.DifficultyEasy:
			return imodel.DifficultyMedium
		case imodel.DifficultyMedium:
			return imodel.DifficultyHard
		default:
			return imodel.DifficultyHard
		}
	}

	// 连续答错 2 题以上 → 降低难度
	if state.ConsecutiveWrong >= 2 {
		switch state.CurrentDifficulty {
		case imodel.DifficultyHard:
			return imodel.DifficultyMedium
		case imodel.DifficultyMedium:
			return imodel.DifficultyEasy
		default:
			return imodel.DifficultyEasy
		}
	}

	// 保持当前难度
	return state.CurrentDifficulty
}
