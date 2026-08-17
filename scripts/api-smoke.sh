#!/bin/sh
set -eu

scratch_root=${PAPERCLIP_RUN_SCRATCH_DIR:-${PAPERCLIP_SCRATCH_DIR:-${TMPDIR:-/tmp}}}
smoke_dir=$(mktemp -d "$scratch_root/interview-api-smoke.XXXXXX")
server_pid=
cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$smoke_dir"
}
trap cleanup EXIT INT TERM

go build -o "$smoke_dir/interview-server" ./cmd/interview-server
HTTP_ADDR=127.0.0.1:19090 \
SQLITE_PATH="$smoke_dir/interview.db" \
LLM_API_KEY=smoke-placeholder \
AUTH_MODE=anonymous \
"$smoke_dir/interview-server" >"$smoke_dir/server.log" 2>&1 &
server_pid=$!

attempt=0
until curl -fsS http://127.0.0.1:19090/healthz >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 50 ]; then
    sed -n '1,160p' "$smoke_dir/server.log" >&2
    exit 1
  fi
  sleep 0.1
done

curl -fsS -o "$smoke_dir/ready.json" http://127.0.0.1:19090/readyz
grep -q '"status":"ready"' "$smoke_dir/ready.json"
curl -fsS -o "$smoke_dir/index.html" http://127.0.0.1:19090/
grep -q 'InterviewAgent' "$smoke_dir/index.html"
curl -fsS -D "$smoke_dir/headers" -o "$smoke_dir/parsed.json" \
  -H 'Content-Type: application/json' \
  --data '{"kind":"jd","text":"Go backend"}' \
  http://127.0.0.1:19090/api/v1/documents/parse
grep -qi '^Set-Cookie: interview_subject=.*HttpOnly.*SameSite=Lax' "$smoke_dir/headers"
grep -q '"kind":"jd"' "$smoke_dir/parsed.json"

if grep -q 'smoke-placeholder' "$smoke_dir/server.log"; then
  echo "smoke failed: server log exposed LLM_API_KEY" >&2
  exit 1
fi

echo "API smoke passed"
