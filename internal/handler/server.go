/**
 * @author: 公众号：IT杨秀才
 * @doc:后端，AI Agent知识进阶，后端、AI大模型、场景题面试大全：https://golangstar.cn/
 */

package handler

import (
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"

	"interview-agent/internal/agent"
	"interview-agent/internal/auth"
	"interview-agent/internal/mcp"
	"interview-agent/internal/memory"
	"interview-agent/internal/rag"
	"interview-agent/internal/skill"

	"github.com/cloudwego/eino/components/model"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		parsed, err := url.Parse(origin)
		return err == nil && strings.EqualFold(parsed.Host, r.Host)
	},
}

// ServerConfig Web 服务配置
type ServerConfig struct {
	ChatModel      model.ChatModel
	CombinedStore  memory.Store
	MilvusStore    *rag.MilvusStore   // Milvus 向量存储（题库读写+按用户隔离）
	BM25Manager    *rag.BM25Manager   // BM25 按用户管理
	RedisStore     *memory.RedisStore // Redis（文件 hash 判重）
	MySQLStore     *memory.MySQLStore
	ChatAgent      *agent.ChatAgent
	Router         *agent.IntentRouter
	SkillRegistry  *skill.SkillRegistry // Skill 技能注册中心
	GitHubSearcher *mcp.GitHubSearcher
	AuthService    *auth.Service // 认证服务
	RerankerType   string        // 重排策略：cross-encoder（默认）/ llm / none
	RerankModel    string        // cross-encoder 重排模型（默认 gte-rerank-v2）
	APIKey         string        // DashScope API Key（cross-encoder rerank 调用用）
}

// Server Web 服务器
type Server struct {
	cfg         *ServerConfig
	authHandler *auth.Handler
}

// NewServer 创建 Web 服务器
func NewServer(cfg *ServerConfig) *Server {
	s := &Server{cfg: cfg}
	if cfg.AuthService != nil {
		s.authHandler = auth.NewHandler(cfg.AuthService)
	}
	return s
}

// Start 启动 HTTP 服务器
func (s *Server) Start(addr string) error {
	// 认证 API
	if s.authHandler != nil {
		http.HandleFunc("/api/register", corsMiddleware(s.authHandler.HandleRegister))
		http.HandleFunc("/api/login", corsMiddleware(s.authHandler.HandleLogin))
	}

	http.HandleFunc("/ws", s.handleWebSocket)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	log.Printf("[Web] 服务器启动: %s", addr)
	return http.ListenAndServe(addr, nil)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// JWT 鉴权：令牌仅通过 HttpOnly cookie 传递，避免出现在 URL 和日志中。
	var userID string
	if s.cfg.AuthService != nil {
		cookie, err := r.Cookie("interview_token")
		if err != nil || cookie.Value == "" {
			http.Error(w, "未授权：缺少 token", http.StatusUnauthorized)
			return
		}
		username, err := s.cfg.AuthService.ValidateToken(cookie.Value)
		if err != nil {
			http.Error(w, "未授权：token 无效", http.StatusUnauthorized)
			return
		}
		userID = username
	} else {
		userID = "default_user"
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WebSocket] 升级失败: %v", err)
		return
	}
	log.Printf("[WebSocket] 新连接已建立")

	session := NewWSSession(conn, s.cfg, userID)
	go session.Run()
}

// corsMiddleware 处理跨域请求（前端开发模式需要）
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}
