package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenBootstrapsSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "interview.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	var version string
	if err := store.db.QueryRow("SELECT value FROM app_meta WHERE key = 'schema_version'").Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "1" {
		t.Fatalf("schema version = %q, want 1", version)
	}
}
