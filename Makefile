.PHONY: dev run build test vet check legacy-run infra-up infra-down infra-status clean

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

check:
	go build ./...
	go vet ./...
	go test ./...

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
