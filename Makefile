.PHONY: dev run build test vet check questionbank-generate questionbank-validate legacy-run infra-up infra-down infra-status clean

# Default local server: SQLite + BM25, no Docker or external databases.
dev:
	go run ./cmd/interview-server

run: dev

build:
	go build -o bin/interview-server ./cmd/interview-server

test:
	go test ./...

vet:
	go vet ./...

check: questionbank-validate
	go build ./...
	go vet ./...
	go test ./...

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
