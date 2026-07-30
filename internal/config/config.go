/**
 * @author: 公众号：IT杨秀才
 * @doc:后端，AI Agent知识进阶，后端、AI大模型、场景题面试大全：https://golangstar.cn/
 */

// Package config 管理 InterviewAgent 的全局配置
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config 全局配置
type Config struct {
	// 大模型配置
	LLM LLMConfig
	// 向量数据库
	Milvus MilvusConfig
	// Redis
	Redis RedisConfig
	// MySQL
	MySQL MySQLConfig
	// GitHub
	GitHub GitHubConfig
	// JWT
	JWT JWTConfig
}

// JWTConfig JWT 认证配置
type JWTConfig struct {
	Secret string // JWT 签名密钥
}

// GitHubConfig GitHub MCP 配置
type GitHubConfig struct {
	Token string // GitHub Personal Access Token（可选，用于复习计划推荐开源项目）
}

// LLMConfig 大模型相关配置
type LLMConfig struct {
	APIKey         string // DashScope API Key
	BaseURL        string // API Base URL
	Model          string // 默认模型（qwen-plus）
	EmbeddingModel string // Embedding 模型
	RerankerType   string // 重排策略：cross-encoder（默认）/ llm / none
	RerankModel    string // cross-encoder 使用的重排模型（默认 gte-rerank-v2）
}

// MilvusConfig Milvus 向量数据库配置
type MilvusConfig struct {
	Addr string // 连接地址
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// MySQLConfig MySQL 配置
type MySQLConfig struct {
	DSN string
}

// Load 从环境变量加载配置（自动尝试读取 .env 文件）
func Load() (*Config, error) {
	// 尝试加载 .env 文件，不存在也不报错
	_ = godotenv.Load()

	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("config: DASHSCOPE_API_KEY is required")
	}

	redisDB := 0
	if v := os.Getenv("REDIS_DB"); v != "" {
		var err error
		redisDB, err = strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("config: invalid REDIS_DB: %w", err)
		}
	}

	return &Config{
		LLM: LLMConfig{
			APIKey:         apiKey,
			BaseURL:        getEnvDefault("LLM_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
			Model:          getEnvDefault("LLM_MODEL", "qwen-plus"),
			EmbeddingModel: getEnvDefault("EMBEDDING_MODEL", "text-embedding-v3"),
			RerankerType:   getEnvDefault("RERANKER_TYPE", "cross-encoder"),
			RerankModel:    getEnvDefault("RERANK_MODEL", "gte-rerank-v2"),
		},
		Milvus: MilvusConfig{
			Addr: getEnvDefault("MILVUS_ADDR", "localhost:19530"),
		},
		Redis: RedisConfig{
			Addr:     getEnvDefault("REDIS_ADDR", "localhost:6379"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       redisDB,
		},
		MySQL: MySQLConfig{
			DSN: getEnvDefault("MYSQL_DSN", "root:interview@tcp(localhost:3306)/interview_agent?charset=utf8mb4&parseTime=True&loc=Local"),
		},
		GitHub: GitHubConfig{
			Token: os.Getenv("GITHUB_TOKEN"),
		},
		JWT: JWTConfig{
			Secret: getEnvDefault("JWT_SECRET", "interview-agent-default-secret"),
		},
	}, nil
}

func getEnvDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
