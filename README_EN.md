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
        │
        ▼
internal/core/graph → internal/core/agent
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
| `LLM_TIMEOUT` | no | `60s` | future business-request timeout |
| `LLM_MAX_CONCURRENCY` | no | `4` | future global model concurrency limit |
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

### Web frontend

The frontend requires Node.js 20.19+ or 22.12+ and defaults to a built-in mock that runs the complete fixed 15-question flow without a backend or API key:

```bash
cd interview-agent-web
npm ci
npm run dev

# Connect to the real HTTP/SSE API instead of the built-in demo
VITE_API_MODE=real npm run dev

# Typecheck, test, and create a GitHub Pages-ready static build
npm run typecheck
npm run test:run
VITE_API_MODE=mock npm run build
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

The frontend uses hash routing and relative asset URLs, so `interview-agent-web/dist/` can be hosted under a GitHub Pages repository path. See [`interview-agent-web/README.md`](interview-agent-web/README.md) for API mode and SSE reconnection details.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Changes land through pull requests to `master`; run `make check` before submitting.

## License

[MIT](LICENSE)
