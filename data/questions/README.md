# Question bank data

The Markdown files in this directory are migration sources. The default server
does not parse them at startup.

`make questionbank-generate` converts the Go backend source into the versioned
asset at `internal/questionbank/assets/go_v1.json`. The generated asset keeps a
source pointer for every question and uses the schema in
`internal/questionbank/assets/schema.json`.

`make questionbank-validate` is part of CI. It checks the schema version,
question type enum (`basic`, `experience`, `design`), required fields, stable
source pointers, duplicate IDs, and known promotion-noise patterns.

Runtime loading is idempotent by asset version and SHA-256. User banks are
separate from the builtin asset and are replaced by subject, filename, and
content SHA-256.
