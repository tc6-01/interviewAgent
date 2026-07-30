/**
 * @author: 公众号：IT杨秀才
 * @doc:后端，AI Agent知识进阶，后端、AI大模型、场景题面试大全：https://golangstar.cn/
 */

package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const chatAgentPrompt = `你是 InterviewAgent 系统的智能助手，一个专注于技术面试的 AI 伙伴。

你的能力范围：
1. 回答技术面试相关的问题（面试技巧、知识点讲解、简历建议等）
2. 帮助用户了解本系统的功能（模拟面试、评估报告、复习计划等）
3. 日常技术问题的闲聊和答疑

你的行为规范：
- 友善专业，回答简洁有深度
- 不要主动替用户做决定，保持引导式对话

【重要：面试引导规则】
当用户表达出想要面试的意图时（比如"开始面试"、"模拟面试"、"我想练习面试"、"怎么开始"、"怎么用"等），你必须引导用户点击页面底部的「开始面试」按钮。回复示例：

"请点击页面底部的 **「开始面试」** 按钮来启动标准面试流程。点击后你可以：
- 上传或粘贴 **岗位 JD**（支持链接、文件、文本）
- 上传或粘贴你的 **简历**

系统会自动完成 JD 分析、简历匹配度评估、智能出题、实时评分，最后生成完整的评估报告和个性化复习计划。"

不要在聊天中直接启动面试流程，因为只有通过按钮才能进入包含 JD 分析、简历匹配、RAG 出题等完整环节的标准化面试。

当前对话上下文中可能包含用户之前面试的历史信息，可以据此提供更个性化的建议。`

// ChatAgent 聊天 Agent，处理非面试场景的日常对话
type ChatAgent struct {
	chatModel model.ChatModel
}

// NewChatAgent 创建聊天 Agent
func NewChatAgent(chatModel model.ChatModel) *ChatAgent {
	return &ChatAgent{chatModel: chatModel}
}

// Chat 处理一轮对话，维护对话历史
func (c *ChatAgent) Chat(ctx context.Context, history []*schema.Message, userInput string) (string, error) {
	messages := []*schema.Message{
		schema.SystemMessage(chatAgentPrompt),
	}

	// 添加历史对话（最近 10 轮）
	start := 0
	if len(history) > 20 { // 20 条消息 = 10 轮对话
		start = len(history) - 20
	}
	messages = append(messages, history[start:]...)

	// 添加当前用户输入
	messages = append(messages, schema.UserMessage(userInput))

	resp, err := c.chatModel.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("chat_agent: generate: %w", err)
	}

	return resp.Content, nil
}
