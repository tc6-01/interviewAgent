package architecture

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultServerHasNoLegacyHardDependencies(t *testing.T) {
	root := moduleRoot(t)
	command := exec.Command("go", "list", "-deps", "./cmd/interview-server")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list default server: %v\n%s", err, output)
	}

	dependencies := string(output)
	for _, forbidden := range []string{
		"github.com/redis/go-redis",
		"github.com/go-sql-driver/mysql",
		"github.com/milvus-io/",
		"github.com/cloudwego/eino-ext/components/embedding/",
		"github.com/cloudwego/eino-ext/components/indexer/milvus",
		"github.com/cloudwego/eino-ext/components/retriever/milvus",
		"interview-agent/internal/handler",
		"interview-agent/internal/memory",
		"interview-agent/internal/rag",
	} {
		if strings.Contains(dependencies, forbidden) {
			t.Errorf("default server dependency graph contains forbidden package %q", forbidden)
		}
	}
}

func TestLayerImportsPointInward(t *testing.T) {
	root := moduleRoot(t)
	tests := []struct {
		pkg       string
		forbidden []string
	}{
		{pkg: "./internal/httpapi", forbidden: []string{"/internal/adapters/", "/internal/bootstrap", "/internal/core/"}},
		{pkg: "./internal/session", forbidden: []string{"/internal/adapters/", "/internal/bootstrap", "/internal/httpapi"}},
		{pkg: "./internal/core/graph", forbidden: []string{"/internal/adapters/", "/internal/bootstrap", "/internal/httpapi", "/internal/session"}},
		{pkg: "./internal/core/agent", forbidden: []string{"/internal/adapters/", "/internal/bootstrap", "/internal/httpapi", "/internal/session", "/internal/core/graph"}},
	}

	for _, test := range tests {
		t.Run(test.pkg, func(t *testing.T) {
			command := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", test.pkg)
			command.Dir = root
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("go list %s: %v\n%s", test.pkg, err, output)
			}
			imports := string(output)
			for _, forbidden := range test.forbidden {
				if strings.Contains(imports, forbidden) {
					t.Errorf("%s imports forbidden layer %q\n%s", test.pkg, forbidden, imports)
				}
			}
		})
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
