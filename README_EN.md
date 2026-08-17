# InterviewAgent — Open-Source Mock Interview System

[中文文档](README.md) | English

InterviewAgent is a Go application intended to cover interview-direction generation, retrieval-based question planning, multi-turn interviewing, conclusions, and review plans. The default runtime is a lightweight modular monolith: SQLite is authoritative storage, in-process BM25 is the default retrieval adapter, and only one OpenAI-compatible LLM key is required.

## Five-minute local start

Prerequisites: Go 1.26+ and one OpenAI-compatible API key. Defaults target DashScope `qwen-plus`; change the base URL and model together for another provider.

```bash
git clone https://github.com/tc6-01/interviewAgent.git interview-agent
cd interview-agent
cp .env.example .env
# Edit .env and set only LLM_API_KEY
make dev
```

The health endpoints do not make provider requests:

```bash
curl http://localhost:9090/healthz
curl http://localhost:9090/readyz
```

- `/healthz` checks process liveness only.
- `/readyz` checks configuration, SQLite, in-process BM25, and local assembly only.
- SQLite defaults to `data/interview.db`; its directory is created automatically.

## Default architecture

```text
cmd/interview-server
        │
        ▼
internal/httpapi
        │
        ▼
internal/session
        ├──────────────► internal/workflow (Eino DAG)
        │                         │
        ▼                         ▼
internal/core/graph       internal/core/agent
        │
        ▼
internal/domain interfaces
        ▲
        │
internal/adapters/{sqlite,bm25,llm/openai}
```

Concrete implementations are wired only in `internal/bootstrap`. HTTP does not depend on Redis, MySQL, or Milvus drivers, and agents do not know about HTTP/SSE. Architecture tests inspect the default binary dependency graph so legacy services cannot silently become startup requirements again.

The historical `cmd`, `internal/handler`, `internal/memory`, and `internal/rag` packages remain available for migration and optional enhancements, but `make dev` does not assemble them.

## Configuration

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `LLM_API_KEY` | yes | none | the only required secret |
| `LLM_BASE_URL` | no | DashScope compatible-mode | OpenAI-compatible endpoint |
| `LLM_MODEL` | no | `qwen-plus` | default model |
| `HTTP_ADDR` | no | `:9090` | HTTP listen address |
| `SQLITE_PATH` | no | `data/interview.db` | SQLite file; tests may use `:memory:` |
| `LLM_TIMEOUT` | no | `60s` | per-attempt model request timeout |
| `LLM_MAX_CONCURRENCY` | no | `4` | process-wide model concurrency limit |
| `SHUTDOWN_TIMEOUT` | no | `10s` | graceful-shutdown timeout |

`.env.example` also documents legacy enhancement variables. They are empty by default and are not read by `cmd/interview-server`.

## Development and verification

```bash
make check
```

Equivalent gates:

```bash
go build ./...
go vet ./...
go test ./...
```

The test scaffold covers configuration, SQLite bootstrap, liveness/readiness semantics, lightweight end-to-end assembly, and default dependency boundaries.

The builtin Go question bank is cleaned offline into structured JSON. Startup
loads it idempotently by version and SHA-256, then rebuilds the in-process
`builtin` BM25 scope:

```bash
make questionbank-generate  # regenerate the structured asset from migration source
make questionbank-validate  # validate schema, question types, and promotion-noise rules
```

User banks use isolated `user:{subject_id}` scopes and are replaced
idempotently by subject, filename, and content SHA-256. Deletion updates both
SQLite and the process-local index. Vector services are not required; an
explicit LLM fallback interface fills retrieval gaps.

## MVP API loop

The default HTTP server now exposes the complete M1/M3/M4 path:

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

Direction output is schema-validated and receives at most one JSON repair attempt. The confirmed version is persisted with the interview snapshot. Interviews always contain 15 primary questions; low-scoring answers may trigger follow-ups without consuming primary slots. A single scoring or follow-up failure emits a degradation warning and does not abort the interview.

Reports link weaknesses to low-scoring prompts and add advanced directions for high-scoring candidates. Review-resource URLs are kept only when they are HTTPS links on a stable allowlist; uncertain URLs are omitted. When the report is ready but review-plan generation failed, the retry command returns `202`; a concurrent duplicate returns `409`.

The process-wide LLM gateway applies a semaphore, per-attempt timeout, and bounded retries. Only network errors, `429`, and `5xx` responses are retried; other `4xx` responses are not. Metrics cover requests, retries, timeouts, rate limits, token usage, and observed concurrency without logging full JDs, resumes, answers, or secrets.

## Docker and optional enhancements

Docker is not required for local development. For a portable single container:

```bash
docker build -t interview-agent .
docker run --rm -p 9090:9090 --env-file .env interview-agent
```

Redis, MySQL, and Milvus in `docker-compose.yml` are isolated in the `enhanced` profile for future optional-driver verification:

```bash
make infra-up
```

They are not prerequisites for the lightweight server.

## Legacy CLI during migration

The historical CLI and WebSocket implementation can still be started with `make legacy-run`, but it requires its original DashScope, Redis, MySQL, and Milvus configuration. It is not the MVP default path.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Changes land through pull requests to `master`; run `make check` before submitting.

## License

[MIT](LICENSE)
