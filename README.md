# InterviewAgent - AI 模拟面试系统

中文 | [English](README_EN.md)

[![CI](https://github.com/wangyangyang/interview-agent/actions/workflows/ci.yml/badge.svg)](../../actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

基于 Go + [Eino](https://github.com/cloudwego/eino) 框架构建的 AI 模拟面试系统。支持多 Agent 协作、RAG 多路召回、动态难度调节、面试评估报告生成等核心能力。

## 系统架构

```
用户交互层（CLI）
       ↓
Agent 编排层（Eino Graph DAG）
  ┌─────────────────────────────────────────────┐
  │  JD分析 → 简历匹配 → RAG检索 → 出题规划      │
  │                                   ↓          │
  │              面试官（多轮对话+动态难度调节）    │
  │                                   ↓          │
  │                         评估报告 → 复习规划    │
  └─────────────────────────────────────────────┘
       ↓
基础能力层
  ┌─────────────┬─────────────┬──────────────┐
  │ RAG 多路召回  │ 记忆系统      │ MCP 工具      │
  │ Milvus+BM25  │ Redis+MySQL  │ GitHub/Web   │
  │ RRF+Rerank   │ 短期+长期     │              │
  └─────────────┴─────────────┴──────────────┘
       ↓
基础设施层（Docker Compose 一键启动）
  ┌───────────┬───────────┬───────────┐
  │  Milvus   │   Redis   │   MySQL   │
  │ 向量数据库  │  会话缓存   │  持久化    │
  └───────────┴───────────┴───────────┘
```

## 核心特性

- **多 Agent 协作**：7 个专职 Agent 通过 DAG 编排协作（聊天 Agent + JD 分析 / 简历匹配 / 出题规划 / 面试官 / 评估 / 复习规划）
- **RAG 多路召回**：Milvus 向量检索 + BM25 关键词检索 + RRF 融合 + LLM Rerank 重排序
- **RAG 质量评估**：Faithfulness / Relevance / Completeness 三维评估
- **动态难度调节**：根据作答表现实时调整题目难度（连续答对升难度，连续答错降难度）
- **Agent 记忆系统**：短期对话记忆（滑动窗口） + 长期用户画像（薄弱点追踪），Redis 缓存 + MySQL 持久化
- **MCP 协议集成**：通过 Playwright MCP Server 实现 JS 渲染网页抓取（招聘页面 JD 提取），GitHub 项目搜索（复习资源推荐）
- **流式输出**：面试官提问支持流式响应
- **面试记录持久化**：每次面试的评估报告和复习计划写入 MySQL，支持历史查询

## 快速开始（Docker 一键启动）

只需 Docker Desktop 和一个[通义千问 DashScope API Key](https://dashscope.console.aliyun.com/)：

```bash
git clone <项目下载地址>
cd interview-agent

cp .env.example .env
# 编辑 .env，填入 DASHSCOPE_API_KEY=sk-你的key

docker compose up -d --build
```

该命令会启动完整 demo：etcd + MinIO + Milvus（向量数据库）、Redis、MySQL，以及 InterviewAgent Web 服务（API 地址 <http://localhost:9090>）。前端界面见「详细运行指南」第七步（`interview-agent-web`，开发服务器 <http://localhost:5173>）。

```bash
docker compose ps        # 查看各容器状态（首次拉镜像约 3~5 分钟）
docker compose down      # 停止（保留数据卷）
docker compose down -v   # 停止并清除数据卷
```

如需本地开发（CLI 模式、单独跑基础设施），请看下方「详细运行指南」。

## 项目结构

```
interview-agent/
├── cmd/main.go                        # 程序入口（CLI）
├── internal/
│   ├── agent/                         # 7 个 Agent 实现
│   │   ├── chat_agent.go              #   聊天 Agent（日常对话/面试引导）
│   │   ├── jd_analyzer.go             #   JD 分析
│   │   ├── resume_matcher.go          #   简历匹配
│   │   ├── question_planner.go        #   出题规划 + 动态难度调节
│   │   ├── interviewer.go             #   面试官（提问/评分/追问/流式输出）
│   │   ├── evaluator.go              #   评估报告生成
│   │   └── review_planner.go         #   复习计划生成
│   ├── rag/                           # RAG 多路召回
│   │   ├── embedding.go               #   DashScope Embedding（text-embedding-v3）
│   │   ├── retriever_vector.go        #   Milvus 向量检索（Eino 官方组件）
│   │   ├── retriever_bm25.go         #   BM25 关键词检索
│   │   ├── fusion.go                  #   RRF 多路融合算法
│   │   ├── rerank.go                  #   LLM Rerank 重排序
│   │   └── evaluation.go             #   RAG 质量评估（三维指标）
│   ├── memory/                        # 记忆系统
│   │   ├── short_term.go             #   短期记忆（对话上下文滑动窗口）
│   │   ├── long_term.go              #   长期记忆（用户画像/薄弱点追踪）
│   │   ├── store.go                  #   存储接口 + Redis 实现
│   │   ├── mysql_store.go            #   MySQL 持久化（自动建表）
│   │   └── combined_store.go         #   组合存储（Redis缓存+MySQL持久化）
│   ├── mcp/                           # MCP 协议工具集成
│   │   ├── github_tool.go            #   GitHub 搜索工具（MCP）
│   │   └── web_scraper.go            #   网页抓取（Playwright MCP Server + stdio）
│   ├── graph/
│   │   └── orchestrator.go           # Graph DAG 全局编排（串联7个Agent+RAG+记忆）
│   ├── model/
│   │   └── types.go                  # 数据模型定义
│   ├── loader/                        # 文档加载
│   │   ├── document.go               #   统一入口（自动识别格式）
│   │   ├── pdf.go                    #   PDF 解析
│   │   ├── docx.go                   #   DOCX 解析
│   │   └── web.go                    #   MCP 网页抓取 + LLM JD 提取
│   └── config/
│       ├── config.go                 # 配置管理（环境变量）
│       └── llm.go                    # ChatModel 工厂（DashScope OpenAI兼容）
├── data/questions/                    # 面试题库（JSON格式，共 22 道）
│   ├── golang.json                    #   Go 语言（6题）
│   ├── distributed_system.json        #   分布式系统（6题）
│   ├── mysql.json                     #   MySQL（5题）
│   └── microservice.json              #   微服务（5题）
├── docker-compose.yml                 # 基础设施编排（Milvus+Redis+MySQL）
├── Makefile                           # 常用命令
└── .env.example                       # 环境变量模板
```

---

## 详细运行指南

### 第一步：环境准备

你需要以下工具，逐个确认：

**1) Go 1.22+**

```bash
# 检查是否已安装
go version

# 如果没有，macOS 用 Homebrew 安装：
brew install go

# 或从官网下载安装包：https://go.dev/dl/
```

**2) Node.js 18+（MCP 网页抓取依赖）**

```bash
# 检查是否已安装
node --version
npx --version

# 如果没有，macOS 用 Homebrew 安装：
brew install node

# 或从官网下载安装包：https://nodejs.org/
```

> Node.js 用于运行 Playwright MCP Server，支持 JS 渲染的网页抓取（招聘页面 JD 提取）。
> 首次运行时 npx 会自动下载 `@playwright/mcp` 包，无需手动安装。

**3) Docker Desktop**

```bash
# 检查是否已安装
docker --version
docker compose version

# 如果没有，下载安装：https://www.docker.com/products/docker-desktop/
# 安装后确保 Docker Desktop 已启动（状态栏有鲸鱼图标）
```

**4) 通义千问 API Key**

1. 打开 https://dashscope.console.aliyun.com/
2. 用支付宝/淘宝账号登录（个人即可，无需企业认证）
3. 点击左侧「API-KEY 管理」→「创建新的 API-KEY」
4. 复制生成的 Key（以 `sk-` 开头）

> 新用户有免费额度，qwen-plus 模型足够跑完多次完整面试。

### 第二步：克隆项目

```bash
git clone <项目下载地址>
cd interview-agent
```

### 第三步：配置环境变量

```bash
cp .env.example .env
```

用任意编辑器打开 `.env`，**只需改第一行**，把 API Key 填进去：

```
DASHSCOPE_API_KEY=sk-你的真实key粘贴在这里
```

其余配置项（Milvus/Redis/MySQL 地址）保持默认，和 docker-compose.yml 中的端口一一对应。

### 第四步：启动基础设施

```bash
# 启动 Milvus（向量数据库）+ Redis（缓存）+ MySQL（持久化）
make infra-up

# 首次启动需要拉取 Docker 镜像，耐心等 3~5 分钟
# Milvus 依赖 etcd + minio，共 5 个容器
```

等待启动完成后，验证所有服务是否正常：

```bash
make infra-status
```

你应该看到 5 个容器全部 running：

```
NAME                    STATUS
interview-agent-etcd    Up (healthy)
interview-agent-minio   Up (healthy)
interview-agent-milvus  Up (healthy)
interview-agent-redis   Up (healthy)
interview-agent-mysql   Up (healthy)
```

如果某个服务不是 healthy，等 30 秒再查一次（Milvus 启动较慢）。

### 第五步：安装 Go 依赖

```bash
go mod tidy
```

首次执行会下载所有依赖包（Eino框架、Milvus SDK、Redis客户端等），耗时 1-2 分钟。

验证编译通过：

```bash
go build ./...
# 没有任何输出 = 编译成功
```

### 第六步：启动后端服务

```bash
go run cmd/main.go web
```

启动时会依次连接所有基础设施：

```
=== InterviewAgent - AI 模拟面试系统 ===

模型: qwen-plus
[MCP] Playwright Web Scraper 就绪
[MySQL] 已连接
[MySQL] 表结构就绪
[Milvus] 已连接: localhost:19530
[Milvus] Indexer 就绪，集合: interview_questions
[Milvus] Retriever 就绪，TopK: 10
[Auth] 用户表就绪

[Web] 服务器启动: http://localhost:9090
[Web] 前端请访问: http://localhost:5173
```

> 如果 Milvus 初始化卡住或报 `collection 加载超时`，请参见下方「常见问题」中的解决方案。

### 第七步：启动前端

前端项目 `interview-agent-web`：

```bash
cd ../interview-agent-web
npm install        # 首次运行需安装依赖
npm run dev        # 启动开发服务器
```

启动后访问 http://localhost:5173 即可使用。

### 第八步：使用面试系统

1. **注册/登录**：在页面上注册账号并登录
2. **上传题库**（可选）：上传 PDF/TXT/MD 格式的面试题库文件，系统自动解析并向量化存入 Milvus。不上传也可以面试，系统会由 LLM 直接出题
3. **开始面试**：输入 JD 和简历，系统自动执行 JD 分析 → 简历匹配 → RAG 检索 → 出题规划 → 逐题面试 → 评估报告 → 复习规划 全流程。JD 支持三种输入方式：
   - **URL 抓取**：粘贴招聘页面链接，通过 Playwright MCP 自动抓取并提取 JD 正文
   - **文件上传**：支持 PDF/TXT/DOCX/MD 格式
   - **手动粘贴**：直接粘贴 JD 文本

   > **关于 URL 抓取**：大多数主流招聘网站（Boss 直聘、拉勾、猎聘等）有登录墙和反爬机制，即使通过 Playwright 渲染也会被拦截，导致无法自动抓取。遇到这种情况请改用文件上传或手动粘贴方式。以下是经过验证可正常抓取的测试链接：
   > ```
   > https://hewa.cn/positionDetails/F0kSXTx79mu6WjmQ1et2Dg_hw2_.html
   > ```
4. **查看历史**：历次面试的评估报告和复习计划均持久化在 MySQL 中，可随时查看

> 系统也支持 CLI 模式：`go run cmd/main.go` 进入聊天模式，`go run cmd/main.go interview` 直接启动命令行面试。

---

## 常见问题

**Q: 启动时卡在 `[Milvus] 已连接` 不动，或报 `collection 加载超时`？**

这是 Milvus standalone 的已知问题（从 2.4 到 2.6 均存在）：Milvus 重启后内部节点 ID 会变更，但旧的 collection 加载元数据没有同步清理，导致 `LoadCollection` 永久卡住。执行以下命令清空 Milvus 数据后重启即可恢复：

```bash
docker compose down
docker volume rm interview-agent_milvus_data
docker compose up -d
```

> ⚠️ 这会清空 Milvus 中已上传的题库数据，重启后需要重新上传题库（通过前端上传或执行 `go run cmd/main.go load-data`）。MySQL 和 Redis 的数据不受影响。

**Q: `make infra-up` 后容器一直不 healthy？**

Milvus 首次启动较慢（需要初始化存储），等 1-2 分钟。如果持续不健康：
```bash
docker compose logs milvus   # 查看 Milvus 日志
docker compose logs mysql    # 查看 MySQL 日志
```

**Q: `连接 Milvus 失败` / `连接 Redis 失败` / `连接 MySQL 失败`？**

确认 Docker 容器在运行：`make infra-status`。确认 `.env` 中的地址和 `docker-compose.yml` 端口映射一致（默认 Milvus 19530、Redis 6379、MySQL 3306）。

**Q: `config: DASHSCOPE_API_KEY is required`？**

`.env` 文件不存在或 Key 未填。执行 `cp .env.example .env` 后填入真实 Key。

**Q: 大模型返回解析失败（`parse response` 错误）？**

偶发的 JSON 格式异常，重试即可。频繁出现可在 `.env` 中换更强的模型：`LLM_MODEL=qwen-max`。

**Q: 端口被占用（如 3306）？**

本地已有 MySQL/Redis 实例。修改 `docker-compose.yml` 中的端口映射，同步修改 `.env` 中对应地址。

---

## 技术栈

| 类别 | 选型 | 用途 |
|------|------|------|
| 语言 | Go 1.22+ | 主语言 |
| AI 框架 | [Eino](https://github.com/cloudwego/eino)（CloudWeGo） | Agent 编排 / 工具调用 / RAG |
| 大模型 | 通义千问 DashScope | LLM 推理（qwen-plus / qwen-max） |
| Embedding | text-embedding-v3 | 文本向量化（1024维） |
| 向量数据库 | Milvus 2.4 | 向量存储与 COSINE 检索 |
| 缓存 | Redis 7 | 会话缓存 / 用户画像缓存 |
| 持久化 | MySQL 8.0 | 用户画像 / 面试历史 / 评估报告 |
| 容器化 | Docker Compose | 一键部署基础设施 |

## 常用命令

```bash
make run           # 运行项目
make build         # 编译到 bin/interview-agent
make test          # 运行测试
make infra-up      # 启动 Milvus + Redis + MySQL
make infra-down    # 停止基础设施
make infra-status  # 查看容器状态
```

## 面试流程示意

```
输入 JD + 简历
      │
      ▼
┌─────────────┐    ┌──────────────┐
│  JD 分析     │───▶│  简历匹配     │
│  Agent       │    │  Agent       │
└─────────────┘    └──────────────┘
                          │
                          ▼
                   ┌──────────────┐
                   │  RAG 多路召回  │ ◀─── Milvus 向量检索
                   │  RRF + Rerank │ ◀─── BM25 关键词检索
                   └──────┬───────┘
                          │
                          ▼
                   ┌──────────────┐
                   │  出题规划     │
                   │  Agent       │
                   └──────┬───────┘
                          │
                          ▼
                   ┌─────────────┐
                   │  面试官       │◀─── 动态难度调节
                   │  Agent       │◀─── 短期记忆（对话窗口）
                   │  (多轮对话)   │
                   └──────┬──────┘
                          │
            ┌─────────────┴──────────────┐
            ▼                            ▼
     ┌─────────────┐          ┌──────────────┐
     │  评估报告    │          │  复习规划     │
     │  Agent       │          │  Agent       │
     └──────┬──────┘          └──────┬───────┘
            │                        │
            ▼                        ▼
     ┌──────────────────────────────────────┐
     │  持久化：MySQL 面试记录 + Redis 缓存   │
     │  长期记忆：用户画像 + 薄弱点追踪       │
     └──────────────────────────────────────┘
```

## 贡献

欢迎提 Issue 和 Pull Request，流程与规范见 [CONTRIBUTING.md](CONTRIBUTING.md)。CI（GitHub Actions）会在每个 PR 上自动运行编译、静态检查、测试和 Docker 构建。

## 许可证

[MIT](LICENSE)
