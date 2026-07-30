/**
 * @author: 公众号：IT杨秀才
 * @doc:后端，AI Agent知识进阶，后端、AI大模型、场景题面试大全：https://golangstar.cn/
 */

package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/gorilla/websocket"

	"interview-agent/internal/agent"
	"interview-agent/internal/graph"
	"interview-agent/internal/loader"
	"interview-agent/internal/skill"
	"github.com/cloudwego/eino/schema"
)

// ClientMsg 客户端消息
type ClientMsg struct {
	Type     string `json:"type"`
	Content  string `json:"content,omitempty"`
	JD       string `json:"jd,omitempty"`
	Resume   string `json:"resume,omitempty"`
	Filename string `json:"filename,omitempty"`
	Data     string `json:"data,omitempty"`
}

// ServerMsg 服务端消息
type ServerMsg struct {
	Type            string              `json:"type"`
	Content         string              `json:"content,omitempty"`
	Stage           string              `json:"stage,omitempty"`
	Message         string              `json:"message,omitempty"`
	QuestionNum     int                 `json:"question_num,omitempty"`
	Score           float64             `json:"score,omitempty"`
	Feedback        string              `json:"feedback,omitempty"`
	KeyPointsHit    []string            `json:"key_points_hit,omitempty"`
	KeyPointsMissed []string            `json:"key_points_missed,omitempty"`
}

// WSSession 单个 WebSocket 会话
type WSSession struct {
	conn         *websocket.Conn
	cfg          *ServerConfig
	userID       string
	chatHistory  []*schema.Message
	mu           sync.Mutex
	answerCh     chan string
	activeSkill  skill.Skill      // 当前激活的 Skill（nil 表示无）
	skillState   *skill.SkillState // 当前 Skill 的交互状态
}

// NewWSSession 创建会话
func NewWSSession(conn *websocket.Conn, cfg *ServerConfig, userID string) *WSSession {
	return &WSSession{
		conn:   conn,
		cfg:    cfg,
		userID: userID,
	}
}

// Run 主循环
func (ws *WSSession) Run() {
	defer ws.conn.Close()

	for {
		_, rawMsg, err := ws.conn.ReadMessage()
		if err != nil {
			log.Printf("[WebSocket] 读取失败: %v", err)
			return
		}

		var msg ClientMsg
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			ws.sendError("消息格式错误")
			continue
		}

		switch msg.Type {
		case "chat":
			ws.handleChat(msg.Content)
		case "start_interview":
			ws.handleStartInterview(msg.JD, msg.Resume)
		case "answer":
			ws.handleAnswer(msg.Content)
		case "upload_questions":
			ws.handleUploadQuestions(msg.Filename, msg.Data)
		case "quit_interview":
			ws.handleQuitInterview()
		default:
			ws.sendError("未知消息类型: " + msg.Type)
		}
	}
}

func (ws *WSSession) handleChat(content string) {
	ctx := context.Background()

	// 预先解析输入（可能包含文件/URL），Skill 和 ChatAgent 都需要解析后的文本
	resolved, err := ws.resolveInput(ctx, content)
	if err != nil {
		resolved = content
	}

	// 1. 如果有激活中的 Skill，优先交给它处理
	if ws.activeSkill != nil {
		// 检查退出命令（用原始 content 检测，避免解析后的文本干扰）
		if skill.IsQuitCommand(content) {
			ws.activeSkill = nil
			ws.skillState = nil
			ws.sendMsg(ServerMsg{Type: "chat_reply", Content: "已退出当前技能模式，回到普通聊天。"})
			return
		}
		// 检查 Skill 会话是否过期
		if ws.skillState != nil && ws.skillState.IsExpired() {
			ws.activeSkill = nil
			ws.skillState = nil
			ws.sendMsg(ServerMsg{Type: "chat_reply", Content: "技能会话已超时，回到普通聊天。"})
			return
		}
		// 技能会话进行中，若用户又发出明确的新技能/新测验意图（例如测验做到一半再说
		// 「来几道 mysql 面试题」），则结束当前会话、切换到新技能，而不是把这句话当成
		// 当前题目的回答。普通答题内容不含触发词，不会被误切。
		if ws.cfg.SkillRegistry == nil || ws.cfg.SkillRegistry.Match(content) == nil {
			resp, err := ws.activeSkill.Handle(ctx, resolved, ws.skillState)
			if err != nil {
				ws.sendError("技能处理失败: " + err.Error())
				ws.activeSkill = nil
				ws.skillState = nil
				return
			}
			ws.skillState = resp.State
			if resp.Done {
				ws.activeSkill = nil
				ws.skillState = nil
			}
			reply := resp.Content
			if !resp.Done && resp.NextPrompt != "" {
				reply += "\n\n> " + resp.NextPrompt
			}
			ws.sendMsg(ServerMsg{Type: "chat_reply", Content: reply})
			return
		}
		// 命中新的技能意图：清空当前会话，落到下方「2. Skill 匹配」开启新技能
		ws.activeSkill = nil
		ws.skillState = nil
	}

	// 2. 尝试 Skill 匹配（用原始 content 做关键词匹配，用解析后的 resolved 传入 Handle）
	if ws.cfg.SkillRegistry != nil {
		if matched := ws.cfg.SkillRegistry.Match(content); matched != nil {
			// 首次调用：创建初始 state 并注入 userID（用于 RAG 按用户隔离检索）
			initState := skill.NewSkillState(matched.Name())
			initState.UserID = ws.userID
			resp, err := matched.Handle(ctx, resolved, initState)
			if err != nil {
				ws.sendError("技能启动失败: " + err.Error())
				return
			}
			if !resp.Done {
				ws.activeSkill = matched
				ws.skillState = resp.State
			}
			reply := resp.Content
			if !resp.Done && resp.NextPrompt != "" {
				reply += "\n\n> " + resp.NextPrompt
			}
			ws.sendMsg(ServerMsg{Type: "chat_reply", Content: reply})
			return
		}
	}

	// 3. 兜底：ChatAgent 通用聊天
	chatContent := resolved
	if resolved != content {
		chatContent = "以下是用户提供的内容：\n\n" + resolved
	}

	resp, err := ws.cfg.ChatAgent.Chat(ctx, ws.chatHistory, chatContent)
	if err != nil {
		ws.sendError("聊天处理失败: " + err.Error())
		return
	}
	ws.chatHistory = append(ws.chatHistory, schema.UserMessage(chatContent), schema.AssistantMessage(resp, nil))
	ws.sendMsg(ServerMsg{Type: "chat_reply", Content: resp})
}

func (ws *WSSession) handleStartInterview(jdRaw, resumeRaw string) {
	ctx := context.Background()

	jdText, err := ws.resolveInput(ctx, jdRaw)
	if err != nil {
		ws.sendError("JD 解析失败，请尝试将 JD 内容复制后粘贴到输入框，或上传文件重试")
		ws.sendMsg(ServerMsg{Type: "interview_complete"})
		return
	}

	resumeText, err := ws.resolveInput(ctx, resumeRaw)
	if err != nil {
		ws.sendError("简历解析失败，请尝试将简历内容复制后粘贴到输入框，或上传文件重试")
		ws.sendMsg(ServerMsg{Type: "interview_complete"})
		return
	}

	ws.answerCh = make(chan string, 1)

	orchestrator := graph.NewOrchestrator(&graph.OrchestratorConfig{
		ChatModel:      ws.cfg.ChatModel,
		Store:          ws.cfg.CombinedStore,
		MilvusStore:    ws.cfg.MilvusStore,
		BM25Manager:    ws.cfg.BM25Manager,
		MySQLStore:     ws.cfg.MySQLStore,
		GitHubSearcher: ws.cfg.GitHubSearcher,
		RerankerType:   ws.cfg.RerankerType,
		RerankModel:    ws.cfg.RerankModel,
		APIKey:         ws.cfg.APIKey,
	})

	callbacks := &graph.InterviewCallbacks{
		OnStageChange: func(stage string, msg string) {
			ws.sendMsg(ServerMsg{Type: "stage_change", Stage: stage, Message: msg})
		},
		OnQuestion: func(questionNum int, content string) {
			ws.sendMsg(ServerMsg{Type: "question", QuestionNum: questionNum, Content: content})
		},
		OnScore: func(score *agent.AnswerScore) {
			ws.sendMsg(ServerMsg{
				Type: "score", Score: score.Score, Feedback: score.Feedback,
				KeyPointsHit: score.KeyPointsHit, KeyPointsMissed: score.KeyPointsMissed,
			})
		},
		OnReport: func(report string) {
			ws.sendMsg(ServerMsg{Type: "report", Content: report})
		},
		OnReviewPlan: func(plan string) {
			ws.sendMsg(ServerMsg{Type: "review_plan", Content: plan})
		},
		GetUserAnswer: func() (string, error) {
			answer, ok := <-ws.answerCh
			if !ok {
				return "", graph.ErrUserQuit
			}
			return answer, nil
		},
	}

	go func() {
		_, err := orchestrator.RunInterview(ctx, jdText, resumeText, ws.userID, callbacks)
		if err != nil && !errors.Is(err, graph.ErrUserQuit) {
			ws.sendError("面试流程出错: " + err.Error())
		}
		ws.sendMsg(ServerMsg{Type: "interview_complete"})
		ws.answerCh = nil
	}()
}

func (ws *WSSession) handleAnswer(content string) {
	if ws.answerCh != nil {
		ws.answerCh <- content
	}
}

func (ws *WSSession) handleQuitInterview() {
	if ws.answerCh != nil {
		close(ws.answerCh)
	}
	ws.sendMsg(ServerMsg{Type: "interview_complete"})
}

// handleUploadQuestions 处理题库上传：按文件名判重/更新 → LLM 解析 → 写入
// - 同用户+同文件名+同内容 → 跳过（Redis hash 快速判断）
// - 同用户+同文件名+不同内容 → 删除旧题后写入新题（题库更新）
// - 同用户+不同文件名 → 追加写入（新增题库）
func (ws *WSSession) handleUploadQuestions(filename, base64Data string) {
	ctx := context.Background()

	ws.sendMsg(ServerMsg{Type: "stage_change", Stage: "upload_questions", Message: "正在解析题库文件..."})

	// 1. 解码文件原始字节并计算 SHA256
	rawBytes, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		ws.sendError("文件 base64 解码失败: " + err.Error())
		return
	}
	hash := sha256.Sum256(rawBytes)
	fileHash := hex.EncodeToString(hash[:])

	// 2. Redis 判重：同用户+同文件名，内容未变则跳过
	isUpdate := false
	if ws.cfg.RedisStore != nil {
		existingHash, err := ws.cfg.RedisStore.GetFileHash(ctx, ws.userID, filename)
		if err != nil {
			log.Printf("[Upload] 查询文件 hash 失败: %v", err)
		} else if existingHash == fileHash {
			ws.sendMsg(ServerMsg{Type: "upload_result", Content: "该题库已导入，无需重复上传"})
			return
		} else if existingHash != "" {
			// 同文件名但内容不同，标记为更新
			isUpdate = true
		}
	}

	// 3. 解析文件内容为文本
	fileContent, err := loader.ParseBase64File(filename, base64Data)
	if err != nil {
		ws.sendError("题库文件解析失败: " + err.Error())
		return
	}

	if len(strings.TrimSpace(fileContent)) < 20 {
		ws.sendError("题库文件内容过短，请检查文件是否正确")
		return
	}

	ws.sendMsg(ServerMsg{Type: "stage_change", Stage: "upload_questions", Message: "正在用 LLM 提取结构化题目..."})

	// 4. LLM 解析为结构化题目
	result, err := loader.ParseQuestionBank(ctx, ws.cfg.ChatModel, fileContent)
	if err != nil {
		ws.sendError("题库 LLM 解析失败: " + err.Error())
		return
	}

	if result.Success == 0 {
		ws.sendError(fmt.Sprintf("题库解析完成但无有效题目（识别 %d 道，全部校验失败）", result.Total))
		return
	}

	ws.sendMsg(ServerMsg{Type: "stage_change", Stage: "upload_questions",
		Message: fmt.Sprintf("解析完成，识别 %d 道题目，%d 道通过校验，正在写入知识库...", result.Total, result.Success)})

	// 5. 写入 Milvus：更新时先删除该文件的旧题，新增时直接追加
	if ws.cfg.MilvusStore != nil {
		if isUpdate {
			if err := ws.cfg.MilvusStore.DeleteBySourceFile(ctx, ws.userID, filename); err != nil {
				log.Printf("[Upload] 删除旧版题库失败: %v", err)
			}
		}

		milvusQuestions := make([]struct {
			ID         string
			Content    string
			Reference  string
			Type       string
			Difficulty string
			Skills     []string
		}, len(result.Questions))
		for i, q := range result.Questions {
			milvusQuestions[i] = struct {
				ID         string
				Content    string
				Reference  string
				Type       string
				Difficulty string
				Skills     []string
			}{
				ID: q.ID, Content: q.Content, Reference: q.Reference,
				Type: q.Type, Difficulty: q.Difficulty, Skills: q.Skills,
			}
		}
		if err := ws.cfg.MilvusStore.LoadParsedQuestions(ctx, ws.userID, filename, milvusQuestions); err != nil {
			ws.sendError("写入 Milvus 失败: " + err.Error())
			return
		}
	}

	// 6. BM25 索引：更新时替换该文件，新增时追加
	if ws.cfg.BM25Manager != nil {
		bm25Docs := make([]*schema.Document, len(result.Questions))
		for i, q := range result.Questions {
			bm25Docs[i] = &schema.Document{
				ID:      q.ID,
				Content: q.Content + "\n参考答案：" + q.Reference,
			}
		}
		if isUpdate {
			ws.cfg.BM25Manager.ReplaceDocuments(ws.userID, bm25Docs)
		} else {
			ws.cfg.BM25Manager.AppendDocuments(ws.userID, bm25Docs)
		}
	}

	// 7. 更新 Redis 文件 hash
	if ws.cfg.RedisStore != nil {
		if err := ws.cfg.RedisStore.SaveFileHash(ctx, ws.userID, filename, fileHash); err != nil {
			log.Printf("[Upload] 保存文件 hash 失败: %v", err)
		}
	}

	// 8. 构建反馈消息
	action := "导入"
	if isUpdate {
		action = "更新"
	}
	var feedback strings.Builder
	feedback.WriteString(fmt.Sprintf("题库%s完成：识别 %d 道，成功录入 %d 道", action, result.Total, result.Success))
	if result.Failed > 0 {
		feedback.WriteString(fmt.Sprintf("，校验失败 %d 道", result.Failed))
	}

	ws.sendMsg(ServerMsg{Type: "upload_result", Content: feedback.String(), Message: formatParseErrors(result.Errors)})
}

// formatParseErrors 格式化解析错误列表
func formatParseErrors(errs []loader.ParseError) string {
	if len(errs) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, e := range errs {
		sb.WriteString(fmt.Sprintf("#%d: %s\n", e.Index, e.Reason))
	}
	return sb.String()
}

// resolveInput 解析输入（URL / base64 文件 / 纯文本）
func (ws *WSSession) resolveInput(ctx context.Context, raw string) (string, error) {
	if len(raw) > 6 && raw[:6] == "[FILE:" {
		end := 0
		for i := 6; i < len(raw); i++ {
			if raw[i] == ']' {
				end = i
				break
			}
		}
		if end == 0 {
			return raw, nil
		}
		filename := raw[6:end]
		rest := raw[end+1:]

		// 分离 base64 数据和用户附加文本（以换行分隔）
		base64Data := rest
		extraText := ""
		if idx := strings.Index(rest, "\n"); idx >= 0 {
			base64Data = rest[:idx]
			extraText = strings.TrimSpace(rest[idx+1:])
		}

		fileContent, err := loader.ParseBase64File(filename, base64Data)
		if err != nil {
			return "", err
		}
		if extraText != "" {
			return fileContent + "\n\n" + extraText, nil
		}
		return fileContent, nil
	}

	// 检查内容中是否包含 URL（可能是纯 URL 或混合文本）
	trimmed := strings.TrimSpace(raw)
	if loader.IsURL(trimmed) {
		return loader.ExtractJDFromURL(ctx, trimmed, ws.cfg.ChatModel)
	}

	return raw, nil
}

func (ws *WSSession) sendMsg(msg ServerMsg) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.conn.WriteJSON(msg)
}

func (ws *WSSession) sendError(msg string) {
	ws.sendMsg(ServerMsg{Type: "error", Message: msg})
}
