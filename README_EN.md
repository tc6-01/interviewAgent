# InterviewAgent — Open-Source Mock Interview System

[中文文档](README.md) | English

InterviewAgent is a Go application intended to cover interview-direction generation, retrieval-based question planning, multi-turn interviewing, conclusions, and review plans. The default runtime is a lightweight modular monolith: SQLite is authoritative storage, in-process BM25 is the default retrieval adapter, and only one OpenAI-compatible LLM key is required.

## Five-minute local start

Prerequisites: Go 1.26.6+ and one OpenAI-compatible API key. Defaults target DashScope `qwen-plus`; change the base URL and model together for another provider.

```bash
git clone https://github.com/tc6-01/interviewAgent.git interview-agent
cd interview-agent
cp .env.example .env
# Edit .env and set only LLM_API_KEY
make dev
```

Open <http://localhost:9090> for the embedded React application; no separate static server is required. Anonymous mode issues an HttpOnly, SameSite=Lax subject cookie automatically.

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
| `AUTH_MODE` | no | `anonymous` | `anonymous` uses a same-origin subject cookie; `jwt` accepts Bearer only |
| `JWT_SECRET` | in JWT mode | none | HMAC secret with at least 32 characters; there is no fixed default |
| `CORS_ALLOWED_ORIGINS` | no | empty | explicit comma-separated allowlist for JWT mode only; `*` is rejected |
| `COOKIE_SECURE` | no | `false` | set to `true` for HTTPS anonymous deployments |
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
go test -race ./...
make smoke
make security-scan
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

The authoritative bank schema is `internal/questionbank/assets/schema.json`. Every question contains `id`, `type`, `topic`, `text`, `answer`, and `source`; `type` is exactly one of `basic`, `experience`, or `design`. Run `make questionbank-generate questionbank-validate` after changes.

## Repository layout

```text
cmd/interview-server/          default single-binary entrypoint
internal/httpapi/              REST, SSE, anonymous/JWT, and CORS boundary
internal/session/              actors, session state, and restart cleanup
internal/workflow/             Eino DAG, report, and review plan
internal/adapters/             SQLite, BM25, and LLM adapters
internal/questionbank/assets/  builtin bank, schema, and generated assets
internal/webui/dist/           production React bundle embedded with go:embed
interview-agent-web/           React/Vite source; dev proxy targets :9090
scripts/                       API smoke and security gates
```

## Identity and deployment boundary

- Anonymous deployment is same-origin only. The cookie is HttpOnly and SameSite=Lax, and client-supplied `X-Subject-ID` values are ignored.
- JWT mode uses `Authorization: Bearer <token>` for both REST and SSE. Cross-origin requests are accepted only from `CORS_ALLOWED_ORIGINS` and never rely on cookies.
- Production Web assets are served from the Go binary through `go:embed`; Vite is a development proxy. GitHub Pages is a static `VITE_API_MODE=mock` preview only, never a formal anonymous deployment.

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

### Web frontend

The frontend requires Node.js 20.19+ or 22.12+ and defaults to a built-in mock that runs the complete fixed 15-question flow without a backend or API key:

```bash
cd interview-agent-web
npm ci
npm run dev

# Connect to the real HTTP/SSE API instead of the built-in demo
VITE_API_MODE=real npm run dev

# Typecheck, test, and create the production embedded build
npm run typecheck
npm run test:run
VITE_API_MODE=real npm run build
```

The production same-origin build uses `VITE_API_MODE=real` and is embedded by the Go service. GitHub Pages is only for the static mock preview. See [`interview-agent-web/README.md`](interview-agent-web/README.md) for details.

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

The production build is written to `internal/webui/dist/` and embedded in the Go binary. A GitHub Pages mock preview must use a temporary output override or copied build artifacts and must not host anonymous production data. See [`interview-agent-web/README.md`](interview-agent-web/README.md) for details.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Changes land through pull requests to `master`; run `make check` before submitting.

## License

[MIT](LICENSE)
