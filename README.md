# InterviewAgent - 开源模拟面试系统

中文 | [English](README_EN.md)

[![CI](https://github.com/tc6-01/interviewAgent/actions/workflows/ci.yml/badge.svg)](https://github.com/tc6-01/interviewAgent/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

InterviewAgent 使用 Go 构建，目标是完整覆盖面试方向生成、检索出题、多轮实时面试、结论与复习计划四个环节。当前默认运行形态是轻量模块化单体：SQLite 作为权威存储，进程内 BM25 作为默认检索，只需要一个 OpenAI-compatible LLM Key。

## 5 分钟本地启动

前置条件：Go 1.26+ 和一个 OpenAI-compatible API Key。默认配置使用 DashScope `qwen-plus`，使用其他兼容供应商时同时修改 Base URL 与模型名。

```bash
git clone https://github.com/tc6-01/interviewAgent.git interview-agent
cd interview-agent
cp .env.example .env
# 编辑 .env，只需填写 LLM_API_KEY
make dev
```

进程启动后无需调用 LLM 即可检查：

```bash
curl http://localhost:9090/healthz
curl http://localhost:9090/readyz
```

- `/healthz` 只检查进程存活，不访问数据库或收费 LLM。
- `/readyz` 只检查配置、SQLite、进程内 BM25 和本地模块装配，不访问 LLM 供应商。
- SQLite 默认写入 `data/interview.db`，目录会自动创建。

## 默认架构与边界

```text
cmd/interview-server
        │
        ▼
internal/httpapi
        │
        ▼
internal/session
        ├──────────────► internal/workflow（Eino DAG）
        │                         │
        ▼                         ▼
internal/core/graph       internal/core/agent
        │
        ▼
internal/domain（接口）
        ▲
        │
internal/adapters/{sqlite,bm25,llm/openai}
```

具体实现只在 `internal/bootstrap` 组合根装配。HTTP 层不依赖 Redis、MySQL、Milvus 等驱动，Agent 层也不感知 HTTP/SSE。`internal/architecture` 的测试会检查默认二进制依赖图，阻止旧重依赖重新进入默认路径。

旧 `cmd`、`internal/handler`、`internal/memory` 和 `internal/rag` 保留用于后续迁移与可选增强，不参与 `make dev` 的默认装配。

## 配置

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `LLM_API_KEY` | 是 | 无 | 唯一必需密钥 |
| `LLM_BASE_URL` | 否 | DashScope compatible-mode | OpenAI-compatible API 地址 |
| `LLM_MODEL` | 否 | `qwen-plus` | 默认模型 |
| `HTTP_ADDR` | 否 | `:9090` | HTTP 监听地址 |
| `SQLITE_PATH` | 否 | `data/interview.db` | SQLite 文件；测试可用 `:memory:` |
| `LLM_TIMEOUT` | 否 | `60s` | 单次模型请求超时 |
| `LLM_MAX_CONCURRENCY` | 否 | `4` | 进程级 LLM 并发上限 |
| `SHUTDOWN_TIMEOUT` | 否 | `10s` | 优雅关停超时 |

`.env.example` 还列出了旧增强驱动变量，它们默认为空，且不会被 `cmd/interview-server` 读取。

## 开发与验证

```bash
make check
```

等价门禁：

```bash
go build ./...
go vet ./...
go test ./...
```

测试脚手架包含配置校验、SQLite 初始化、健康/就绪语义、完整轻量装配和默认依赖图检查。

内置 Go 题库已离线清洗为结构化 JSON，启动时按版本和 SHA-256 幂等装载到 SQLite，并重建进程内 `builtin` BM25 scope。题库维护命令：

```bash
make questionbank-generate  # 从迁移源生成结构化资产
make questionbank-validate  # 校验 schema、题型枚举与推广噪声
```

用户题库使用独立的 `user:{subject_id}` scope，按主体、文件名和内容 SHA-256 幂等替换；删除时同步更新 SQLite 与进程内索引。默认检索不依赖向量服务，缺题通过显式 LLM fallback 接口补齐。

## MVP API 闭环

默认 HTTP 服务已经接通 M1/M3/M4 主流程：

```text
POST  /api/v1/documents/parse
POST  /api/v1/interview-directions
PATCH /api/v1/interview-directions/{id}
POST  /api/v1/interview-directions/{id}/confirm
POST  /api/v1/interviews
GET   /api/v1/interviews/{id}/events
POST  /api/v1/interviews/{id}/answers
POST  /api/v1/interviews/{id}/quit
GET   /api/v1/interviews/{id}/report
GET   /api/v1/interviews/{id}/review-plan
POST  /api/v1/interviews/{id}/review-plan/retry
```

方向生成结果会做结构校验，失败时最多进行一次 JSON 修复；确认后的版本随面试快照持久化。面试固定 15 道主问题，低分回答可触发追问，追问不占用主问题名额。单题评分或追问失败只产生降级警告，不会中断整场。

报告会把弱项关联到失分题；高分候选人会得到进阶方向。复习资源只保留经稳定域名校验的 HTTPS URL，无法确认的链接会被省略。报告已完成而复习计划失败时，可调用独立重试端点；首次返回 `202`，并发重复重试返回 `409`。

LLM Gateway 在进程内统一执行并发 semaphore、单次超时和有限重试：仅网络错误、`429` 与 `5xx` 重试，其他 `4xx` 不重试。指标记录请求、重试、超时、限流、token usage 和最大并发，不记录完整 JD、简历、回答或密钥。

## Docker 与增强部署

Docker 不是本地开发前置。需要便携单容器时：

```bash
docker build -t interview-agent .
docker run --rm -p 9090:9090 --env-file .env interview-agent
```

`docker-compose.yml` 中的 Redis、MySQL、Milvus 仅位于 `enhanced` profile，用于后续可选驱动验证：

```bash
make infra-up
```

它们不会成为轻量服务器的启动条件。

## 旧 CLI（迁移期）

历史 CLI 与 WebSocket 实现仍可通过 `make legacy-run` 启动，但需要其原有的 DashScope、Redis、MySQL、Milvus 等配置，不代表 MVP 默认路径。后续任务会逐步把能力迁移到 HTTP/SSE 与新的领域接口。

## 贡献

请阅读 [CONTRIBUTING.md](CONTRIBUTING.md)。所有改动通过 PR 合入 `master`，提交前运行 `make check`。

## License

[MIT](LICENSE)
