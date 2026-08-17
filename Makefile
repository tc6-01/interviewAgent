.PHONY: dev run build test test-race vet check smoke security-scan web-install web-check web-build questionbank-generate questionbank-validate legacy-run infra-up infra-down infra-status clean

# Default local server: SQLite + BM25, no Docker or external databases.
dev:
	go run ./cmd/interview-server

run: dev

build:
	go build -o bin/interview-server ./cmd/interview-server

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

web-install:
	cd interview-agent-web && npm ci

web-build:
	cd interview-agent-web && VITE_API_MODE=real npm run build

web-check: web-install
	cd interview-agent-web && npm run typecheck && npm run test:run && VITE_API_MODE=real npm run build

smoke:
	./scripts/api-smoke.sh

security-scan:
	./scripts/security-scan.sh

check: questionbank-validate security-scan
	go build ./...
	go vet ./...
	go test ./...
	go test -race ./...

questionbank-generate:
	go run ./cmd/questionbank-gen

questionbank-validate:
	go test ./internal/questionbank -run '^TestBuiltinAsset'
	@tmp="$$(mktemp "$${TMPDIR:-/tmp}/interview-questionbank.XXXXXX")"; \
	trap 'rm -f "$$tmp"' EXIT; \
	go run ./cmd/questionbank-gen -output "$$tmp"; \
	cmp -s "$$tmp" internal/questionbank/assets/go_v1.json || { echo "builtin question bank is stale; run make questionbank-generate"; exit 1; }

# Legacy CLI requires its historical Redis/MySQL/Milvus configuration.
legacy-run:
	go run ./cmd

infra-up:
	docker compose --profile enhanced up -d

infra-down:
	docker compose --profile enhanced down

infra-status:
	docker compose --profile enhanced ps

clean:
	rm -rf bin/
