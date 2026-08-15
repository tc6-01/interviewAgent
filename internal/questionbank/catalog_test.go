package questionbank

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"interview-agent/internal/adapters/bm25"
	"interview-agent/internal/adapters/sqlite"
	"interview-agent/internal/domain"
)

func TestBuiltinAssetSchemaNoiseAndIdempotentStartup(t *testing.T) {
	builtin, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("LoadBuiltin: %v", err)
	}
	if len(builtin.Questions) < 100 {
		t.Fatalf("builtin questions = %d, want at least 100", len(builtin.Questions))
	}
	for _, question := range builtin.Questions {
		if question.Type != domain.QuestionTypeBasic {
			t.Fatalf("builtin question %s type = %s", question.ID, question.Type)
		}
		if !strings.Contains(question.Source, "go_interview.md#go-") {
			t.Fatalf("question %s source = %q", question.ID, question.Source)
		}
	}
	schema, err := os.ReadFile("assets/schema.json")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var schemaDocument map[string]any
	if err := json.Unmarshal(schema, &schemaDocument); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	properties := schemaDocument["properties"].(map[string]any)
	questionsSchema := properties["questions"].(map[string]any)
	itemsSchema := questionsSchema["items"].(map[string]any)
	questionProperties := itemsSchema["properties"].(map[string]any)
	typeSchema := questionProperties["type"].(map[string]any)
	if enum, ok := typeSchema["enum"].([]any); !ok || len(enum) != 3 {
		t.Fatalf("schema question type enum = %#v, want three values", typeSchema["enum"])
	}

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "interview.db")
	for run := 0; run < 2; run++ {
		store, err := sqlite.Open(ctx, dbPath)
		if err != nil {
			t.Fatal(err)
		}
		index := bm25.New()
		catalog, err := NewCatalog(store, index, nil)
		if err != nil {
			t.Fatal(err)
		}
		changed, err := catalog.LoadBuiltin(ctx)
		if err != nil {
			t.Fatalf("LoadBuiltin run %d: %v", run, err)
		}
		if changed != (run == 0) {
			t.Fatalf("LoadBuiltin run %d changed = %v", run, changed)
		}
		if index.ScopeSize("builtin") != len(builtin.Questions) {
			t.Fatalf("builtin index size = %d, want %d", index.ScopeSize("builtin"), len(builtin.Questions))
		}
		index.Close()
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUserBankUploadReplaceDeleteAndScopeRebuild(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "interview.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	index := bm25.New()
	catalog, err := NewCatalog(store, index, nil)
	if err != nil {
		t.Fatal(err)
	}

	first := []byte(`{"questions":[{"id":"pay","type":"basic","topic":"支付","text":"如何保证支付回调幂等？","answer":"使用业务幂等键","source":"private"}]}`)
	changed, err := catalog.ReplaceUserBank(ctx, "subject-a", "private.json", first)
	if err != nil || !changed {
		t.Fatalf("first upload = %v, %v", changed, err)
	}
	changed, err = catalog.ReplaceUserBank(ctx, "subject-a", "private.json", first)
	if err != nil || changed {
		t.Fatalf("idempotent upload = %v, %v", changed, err)
	}
	hits, _ := index.Search(ctx, domain.SearchRequest{Scopes: []string{"user:subject-a"}, Query: "支付 幂等", Limit: 5})
	if len(hits) != 1 {
		t.Fatalf("first upload hits = %#v", hits)
	}
	hits, _ = index.Search(ctx, domain.SearchRequest{Scopes: []string{"user:subject-b"}, Query: "支付 幂等", Limit: 5})
	if len(hits) != 0 {
		t.Fatalf("subject isolation leak = %#v", hits)
	}

	replacement := []byte(`{"questions":[{"id":"stock","type":"design","topic":"库存","text":"设计库存扣减一致性方案","answer":"事务消息","source":"private"}]}`)
	changed, err = catalog.ReplaceUserBank(ctx, "subject-a", "private.json", replacement)
	if err != nil || !changed {
		t.Fatalf("replacement = %v, %v", changed, err)
	}
	hits, _ = index.Search(ctx, domain.SearchRequest{Scopes: []string{"user:subject-a"}, Query: "支付", Limit: 5})
	if len(hits) != 0 {
		t.Fatalf("old indexed question remained: %#v", hits)
	}
	hits, _ = index.Search(ctx, domain.SearchRequest{Scopes: []string{"user:subject-a"}, Query: "库存 一致性", Limit: 5})
	if len(hits) != 1 || hits[0].Question.Type != domain.QuestionTypeDesign {
		t.Fatalf("replacement hits = %#v", hits)
	}

	index.RemoveScope("user:subject-a")
	if err := catalog.RebuildUserScope(ctx, "subject-a"); err != nil {
		t.Fatalf("RebuildUserScope: %v", err)
	}
	if index.ScopeSize("user:subject-a") != 1 {
		t.Fatalf("rebuilt scope size = %d", index.ScopeSize("user:subject-a"))
	}
	if err := catalog.DeleteUserBank(ctx, "subject-a", "private.json"); err != nil {
		t.Fatalf("DeleteUserBank: %v", err)
	}
	if index.ScopeSize("user:subject-a") != 0 {
		t.Fatalf("deleted scope size = %d", index.ScopeSize("user:subject-a"))
	}
}

func TestLoadAllRebuildsPersistedUserScopesAfterRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "interview.db")
	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	firstIndex := bm25.New()
	firstCatalog, err := NewCatalog(store, firstIndex, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"questions":[{"id":"private","type":"basic","topic":"支付","text":"支付回调如何幂等？","source":"private"}]}`)
	if _, err := firstCatalog.ReplaceUserBank(ctx, "subject-a", "private.json", content); err != nil {
		t.Fatal(err)
	}
	firstIndex.Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restartedStore, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restartedStore.Close() })
	restartedIndex := bm25.New()
	restartedCatalog, err := NewCatalog(restartedStore, restartedIndex, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restartedCatalog.LoadAll(ctx); err != nil {
		t.Fatalf("LoadAll after restart: %v", err)
	}
	if restartedIndex.ScopeSize("builtin") < 100 || restartedIndex.ScopeSize("user:subject-a") != 1 {
		t.Fatalf("rebuilt sizes builtin=%d user=%d", restartedIndex.ScopeSize("builtin"), restartedIndex.ScopeSize("user:subject-a"))
	}
	hits, err := restartedIndex.Search(ctx, domain.SearchRequest{Scopes: []string{"user:subject-a"}, Query: "支付 幂等", Limit: 5})
	if err != nil || len(hits) != 1 {
		t.Fatalf("restarted user scope hits = %#v, %v", hits, err)
	}
}

func TestFixedPlanUsesExplicitFallbackForRetrievalGaps(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "interview.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	index := bm25.New()
	fallback := &fallbackStub{}
	catalog, err := NewCatalog(store, index, fallback)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.LoadBuiltin(ctx); err != nil {
		t.Fatal(err)
	}
	questions, err := catalog.BuildQuestions(ctx, "subject-a", "channel goroutine")
	if err != nil {
		t.Fatalf("BuildQuestions: %v", err)
	}
	if len(questions) != 15 || fallback.calls != 1 {
		t.Fatalf("questions=%d fallback.calls=%d", len(questions), fallback.calls)
	}
	counts := make(map[domain.QuestionType]int)
	for _, question := range questions {
		counts[question.Type]++
	}
	if counts[domain.QuestionTypeBasic] != 8 || counts[domain.QuestionTypeExperience] != 5 || counts[domain.QuestionTypeDesign] != 2 {
		t.Fatalf("question distribution = %#v", counts)
	}
	planJSON, err := json.Marshal(domain.FixedQuestionPlan())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(planJSON), "channel") || strings.Contains(string(planJSON), "text") {
		t.Fatalf("question plan leaked question text: %s", planJSON)
	}
}

func TestBuiltinRetrievalQualitySamples(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "interview.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	index := bm25.New()
	catalog, err := NewCatalog(store, index, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.LoadBuiltin(ctx); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		query string
		want  string
	}{
		{query: "channel 关闭", want: "channel"},
		{query: "slice 扩容", want: "slice"},
		{query: "垃圾回收 GC", want: "gc"},
	} {
		hits, err := index.Search(ctx, domain.SearchRequest{Scopes: []string{"builtin"}, Query: sample.query, Limit: 3})
		if err != nil || len(hits) == 0 {
			t.Fatalf("query %q hits = %#v, %v", sample.query, hits, err)
		}
		combined := strings.ToLower(hits[0].Question.Topic + " " + hits[0].Question.Text)
		if !strings.Contains(combined, sample.want) {
			t.Fatalf("query %q top hit = %q, want fragment %q", sample.query, combined, sample.want)
		}
	}
}

type fallbackStub struct {
	calls int
}

func (f *fallbackStub) GenerateQuestions(_ context.Context, _ string, missing map[domain.QuestionType]int) ([]domain.Question, error) {
	f.calls++
	var questions []domain.Question
	for _, questionType := range []domain.QuestionType{domain.QuestionTypeBasic, domain.QuestionTypeExperience, domain.QuestionTypeDesign} {
		for index := 0; index < missing[questionType]; index++ {
			questions = append(questions, domain.Question{ID: string(questionType) + "-fallback-" + string(rune('a'+index)), Type: questionType, Text: "LLM generated", Source: "llm"})
		}
	}
	return questions, nil
}
