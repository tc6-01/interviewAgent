/**
 * @author: 公众号：IT杨秀才
 * @doc:后端，AI Agent知识进阶，后端、AI大模型、场景题面试大全：https://golangstar.cn/
 */

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"

	"interview-agent/internal/mcp"
	imodel "interview-agent/internal/model"
)

// reviewPlannerInstruction ReAct 系统指令：引导模型在需要时自主调用 GitHub 搜索工具。
const reviewPlannerInstruction = `你是一位技术学习路径规划专家，要根据候选人的面试评估报告制定一份个性化的复习计划。

当候选人存在明显薄弱领域、需要推荐真实可用的开源项目或教程时，自行决定用合适的英文技术关键词
（通常取 1~3 个高优先级薄弱点）调用 search_github_repos 工具，并把搜到的真实项目写进推荐资源、
type 设为 repo、url 用搜到的真实链接。也可以补充经典书籍、官方文档等非 GitHub 资源。
如果工具不可用或没搜到结果，就只用你已知的优质资源，不要编造链接。

规划原则：优先解决高优先级薄弱点；每个学习项给出可执行的具体行动；推荐资源实用、高质量；时间估算合理。

最终只输出纯 JSON（不要输出工具调用过程、思考说明或多余文字），格式：
{
  "weak_areas": [
    {"topic": "薄弱领域名称", "score": 50.0, "priority": "high/medium/low"}
  ],
  "study_plan": [
    {"topic": "学习主题", "objective": "学习目标", "actions": ["具体行动1", "具体行动2"], "time_estimate": "预估时间"}
  ],
  "resources": [
    {"title": "资源标题", "type": "article/video/repo/book", "url": "链接（如有）", "desc": "推荐理由"}
  ]
}`

// ReviewPlanner 复习规划 Agent —— 全项目唯一真正调用外部工具的 Agent，用 Eino ReAct 实现：
// 模型根据评估报告自主决定是否、用什么关键词调用 GitHub 搜索工具，再综合产出复习计划。
type ReviewPlanner struct {
	chatModel      model.ChatModel
	githubSearcher *mcp.GitHubSearcher // 可选，nil 时退化为纯 LLM 生成
}

// NewReviewPlanner 创建复习规划 Agent
func NewReviewPlanner(chatModel model.ChatModel) *ReviewPlanner {
	return &ReviewPlanner{chatModel: chatModel}
}

// SetGitHubSearcher 设置 GitHub MCP 搜索器
func (p *ReviewPlanner) SetGitHubSearcher(searcher *mcp.GitHubSearcher) {
	p.githubSearcher = searcher
}

// Plan 根据评估报告生成复习计划
func (p *ReviewPlanner) Plan(ctx context.Context, report *imodel.EvaluationReport) (*imodel.ReviewPlan, error) {
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	userMsg := fmt.Sprintf("请根据以下面试评估报告生成复习计划：\n\n%s", string(reportJSON))

	// 优先用 ReAct（带 GitHub 工具，由模型自主调用）；失败或无工具时降级为单轮生成
	content, err := p.generateWithReactAgent(ctx, userMsg)
	if err != nil || strings.TrimSpace(content) == "" {
		if err != nil {
			log.Printf("[ReviewPlanner] ReAct 执行失败，降级为单轮生成: %v", err)
		}
		messages := []*schema.Message{
			schema.SystemMessage(reviewPlannerInstruction),
			schema.UserMessage(userMsg),
		}
		resp, gErr := p.chatModel.Generate(ctx, messages)
		if gErr != nil {
			return nil, fmt.Errorf("review_planner: generate: %w", gErr)
		}
		content = resp.Content
	}

	result := &imodel.ReviewPlan{}
	jsonStr := extractJSON(content)
	if err := json.Unmarshal([]byte(jsonStr), result); err != nil {
		return nil, fmt.Errorf("review_planner: parse response: %w\nraw: %s", err, content)
	}

	result.SessionID = report.SessionID
	result.CreatedAt = time.Now()

	return result, nil
}

// generateWithReactAgent 用 Eino ReAct（GitHub 工具由模型自主调用）生成复习计划文本。无工具时返回空串以走降级。
func (p *ReviewPlanner) generateWithReactAgent(ctx context.Context, userMsg string) (string, error) {
	if p.githubSearcher == nil {
		return "", nil // 没有 GitHub 工具，直接走降级
	}

	ghTool, err := utils.InferTool(
		"search_github_repos",
		"根据技术关键词搜索 GitHub 上 star 数较多的开源项目与教程，返回项目清单（名称、star 数、链接、简介）。"+
			"为候选人推荐真实可用的学习项目时调用，关键词用英文技术词。",
		p.searchGitHubRepos,
	)
	if err != nil {
		return "", fmt.Errorf("build github tool: %w", err)
	}

	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		Model:       p.chatModel,
		ToolsConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{ghTool}},
		MessageModifier: func(_ context.Context, input []*schema.Message) []*schema.Message {
			return append([]*schema.Message{schema.SystemMessage(reviewPlannerInstruction)}, input...)
		},
	})
	if err != nil {
		return "", fmt.Errorf("new react agent: %w", err)
	}

	msg, err := agent.Generate(ctx, []*schema.Message{schema.UserMessage(userMsg)})
	if err != nil {
		return "", err
	}
	return msg.Content, nil
}

// githubSearchReq ReAct 调用 GitHub 工具的入参
type githubSearchReq struct {
	Query string `json:"query" jsonschema:"description=技术关键词，用英文，如 redis distributed-lock"`
}

// searchGitHubRepos 工具实现：按关键词搜索 GitHub 仓库，返回格式化文本
func (p *ReviewPlanner) searchGitHubRepos(ctx context.Context, req githubSearchReq) (string, error) {
	repos, err := p.githubSearcher.SearchRepos(ctx, req.Query+" stars:>100", 5)
	if err != nil || len(repos) == 0 {
		return "未找到相关开源项目。", nil
	}
	var sb strings.Builder
	for i, r := range repos {
		sb.WriteString(fmt.Sprintf("%d. **%s** (%d stars)\n   链接：%s\n   简介：%s\n\n",
			i+1, r.Name, r.Stars, r.URL, r.Desc))
	}
	return sb.String(), nil
}

// FormatReviewPlan 将复习计划格式化为 Markdown
func FormatReviewPlan(plan *imodel.ReviewPlan) string {
	md := "# 个性化复习计划\n\n"

	md += "## 薄弱领域\n\n"
	md += "| 领域 | 得分 | 优先级 |\n|------|------|--------|\n"
	for _, area := range plan.WeakAreas {
		md += fmt.Sprintf("| %s | %.1f | %s |\n", area.Topic, area.Score, area.Priority)
	}

	md += "\n## 学习计划\n\n"
	for i, item := range plan.StudyPlan {
		md += fmt.Sprintf("### %d. %s\n\n", i+1, item.Topic)
		md += fmt.Sprintf("**目标**：%s\n\n", item.Objective)
		md += fmt.Sprintf("**预估时间**：%s\n\n", item.TimeEstimate)
		md += "**具体行动**：\n"
		for _, action := range item.Actions {
			md += fmt.Sprintf("- %s\n", action)
		}
		md += "\n"
	}

	if len(plan.Resources) > 0 {
		md += "## 推荐资源\n\n"
		for _, res := range plan.Resources {
			md += fmt.Sprintf("- **[%s](%s)**（%s）：%s\n", res.Title, res.URL, res.Type, res.Desc)
		}
	}

	return md
}
