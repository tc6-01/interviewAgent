#!/bin/sh
set -eu

tracked_pathspec=':(exclude)scripts/security-scan.sh'

if git grep -nE 'sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}|-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----' -- . "$tracked_pathspec"; then
  echo "security scan failed: possible credential material found" >&2
  exit 1
fi

if git grep -nE 'Access-Control-Allow-Origin.*\*|getEnvDefault.*JWT_SECRET' -- '*.go' "$tracked_pathspec"; then
  echo "security scan failed: wildcard CORS or fixed JWT secret found" >&2
  exit 1
fi

if git grep -nE '原始内容:|topDoc\.Content\)|resume_text[^\n]*(Print|Info|Debug)|jd_text[^\n]*(Print|Info|Debug)' -- '*.go' "$tracked_pathspec"; then
  echo "security scan failed: sensitive body logging pattern found" >&2
  exit 1
fi

echo "repository security patterns passed"
