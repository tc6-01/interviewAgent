# InterviewAgent — AI Mock Interview System

[中文文档](README.md) | English

An AI mock interview system built with Go and the [Eino](https://github.com/cloudwego/eino) framework. It orchestrates multiple agents through a DAG to run a full interview loop: JD analysis → resume matching → RAG-based question planning → multi-turn interviewing with dynamic difficulty → evaluation report → review planning.

## Features

- **Multi-agent orchestration**: 7 specialized agents (chat, JD analyzer, resume matcher, question planner, interviewer, evaluator, review planner) coordinated via an Eino Graph DAG.
- **Hybrid RAG retrieval**: Milvus vector search + BM25 keyword search, fused with RRF and re-ranked by an LLM / cross-encoder.
- **RAG quality evaluation**: faithfulness / relevance / completeness scoring.
- **Dynamic difficulty**: question difficulty adapts in real time to the candidate's performance.
- **Agent memory**: short-term conversation window + long-term user profile (weak-point tracking), backed by Redis and MySQL.
- **MCP integration**: Playwright MCP server for JS-rendered job-posting scraping; GitHub search for review-resource recommendations.
- **Streaming output** for interviewer questions, and persisted interview reports with history queries.

## Quick Start (Docker, one command)

Prerequisites: Docker Desktop (with Docker Compose) and a [DashScope API key](https://dashscope.console.aliyun.com/).

```bash
git clone <repo-url>
cd interview-agent

cp .env.example .env
# Edit .env and set DASHSCOPE_API_KEY=sk-...

docker compose up -d --build
```

This starts the full stack: etcd + MinIO + Milvus (vector DB), Redis, MySQL, and the InterviewAgent web service (API at <http://localhost:9090>). For the web UI, see the frontend section below (`interview-agent-web`, dev server at <http://localhost:5173>).

Check status:

```bash
docker compose ps
```

Stop everything:

```bash
docker compose down          # keep data volumes
docker compose down -v       # remove data volumes too
```

## Local Development

Requirements: Go 1.22+, Node.js 18+ (for the Playwright MCP server), Docker.

```bash
# Start infrastructure only (Milvus + Redis + MySQL)
make infra-up

# Run the CLI
go run cmd/main.go chat        # chat mode
go run cmd/main.go interview   # interview mode
go run cmd/main.go web         # HTTP/SSE web service on :9090
go run cmd/main.go load-data   # seed the question bank

# Frontend (web UI, requires Node.js 18+)
cd interview-agent-web && npm install && npm run dev   # http://localhost:5173

# Build & test
go build ./...
go vet ./...
go test ./...
```

See the [Chinese README](README.md) for a step-by-step setup walkthrough, project structure, and FAQ.

## Tech Stack

| Layer | Technology |
| --- | --- |
| Language | Go 1.22+ |
| Agent framework | CloudWeGo Eino (Graph DAG) |
| LLM / Embedding | Qwen (DashScope): qwen-plus, text-embedding-v3, gte-rerank |
| Vector DB | Milvus 2.4 (standalone) |
| Cache / Memory | Redis 7 |
| Persistence | MySQL 8 |
| Tooling | MCP (Playwright, GitHub) |
| Frontend | `interview-agent-web/` (web UI) |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). CI (GitHub Actions) runs build, vet, tests, and Docker builds on every PR.

## License

[MIT](LICENSE)
