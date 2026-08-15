package questionbank

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	enum, ok := typeSchema["enum"].([]any)
	if !ok || len(enum) != 3 {
		t.Fatalf("schema question type enum = %#v, want three values", typeSchema["enum"])
	}
	wantTypes := map[string]bool{"basic": true, "experience": true, "design": true}
	for _, value := range enum {
		if typeName, ok := value.(string); !ok || !wantTypes[typeName] {
			t.Fatalf("schema question type enum = %#v, want basic/experience/design", enum)
		}
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

func TestBuiltinAssetJSONSchemaRejectsInvalidDocuments(t *testing.T) {
	content, err := os.ReadFile("assets/go_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "unknown top-level property", mutate: func(document map[string]any) { document["unexpected"] = true }},
		{name: "unsupported schema version", mutate: func(document map[string]any) { document["schema_version"] = "2" }},
		{name: "invalid question type", mutate: func(document map[string]any) {
			document["questions"].([]any)[0].(map[string]any)["type"] = "trivia"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(content, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(document)
			invalid, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateBuiltinAssetSchema(invalid); err == nil {
				t.Fatal("schema validation accepted an invalid builtin document")
			}
		})
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

func TestConcurrentReplaceAndDeleteCannotPublishStaleScope(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "interview.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	seedCatalog, err := NewCatalog(store, bm25.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	seed := []byte(`{"questions":[{"id":"old","type":"basic","topic":"支付","text":"旧支付题","source":"private"}]}`)
	if _, err := seedCatalog.ReplaceUserBank(ctx, "subject-a", "private.json", seed); err != nil {
		t.Fatal(err)
	}

	blocking := &captureListRepository{
		Repository:  store,
		targetScope: "user:subject-a",
		captured:    make(chan struct{}),
		release:     make(chan struct{}),
	}
	index := bm25.New()
	catalog, err := NewCatalog(blocking, index, nil)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte(`{"questions":[{"id":"new","type":"design","topic":"库存","text":"新库存题","source":"private"}]}`)
	replaceDone := make(chan error, 1)
	go func() {
		_, err := catalog.ReplaceUserBank(ctx, "subject-a", "private.json", replacement)
		replaceDone <- err
	}()
	<-blocking.captured

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- catalog.DeleteUserBank(ctx, "subject-a", "private.json") }()
	select {
	case err := <-deleteDone:
		close(blocking.release)
		<-replaceDone
		t.Fatalf("delete completed before the in-flight replacement published its snapshot: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(blocking.release)
	if err := <-replaceDone; err != nil {
		t.Fatalf("ReplaceUserBank: %v", err)
	}
	if err := <-deleteDone; err != nil {
		t.Fatalf("DeleteUserBank: %v", err)
	}
	if size := index.ScopeSize("user:subject-a"); size != 0 {
		t.Fatalf("stale user scope was published after delete: size=%d", size)
	}
}

func TestConcurrentReplacementsCannotPublishStaleScope(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "interview.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	blocking := &captureListRepository{
		Repository:  store,
		targetScope: "user:subject-a",
		captured:    make(chan struct{}),
		release:     make(chan struct{}),
	}
	index := bm25.New()
	catalog, err := NewCatalog(blocking, index, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := []byte(`{"questions":[{"id":"first","type":"basic","topic":"旧题","text":"第一版问题","source":"private"}]}`)
	firstDone := make(chan error, 1)
	go func() {
		_, err := catalog.ReplaceUserBank(ctx, "subject-a", "private.json", first)
		firstDone <- err
	}()
	<-blocking.captured

	second := []byte(`{"questions":[{"id":"second","type":"design","topic":"新题","text":"第二版最终问题","source":"private"}]}`)
	secondDone := make(chan error, 1)
	go func() {
		_, err := catalog.ReplaceUserBank(ctx, "subject-a", "private.json", second)
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		close(blocking.release)
		<-firstDone
		t.Fatalf("second replacement completed before the first snapshot publication: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(blocking.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first ReplaceUserBank: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second ReplaceUserBank: %v", err)
	}
	hits, err := index.Search(ctx, domain.SearchRequest{Scopes: []string{"user:subject-a"}, Query: "第二版 最终", Limit: 5})
	if err != nil || len(hits) != 1 || !strings.Contains(hits[0].Question.Text, "第二版") {
		t.Fatalf("latest replacement is not the published snapshot: hits=%#v err=%v", hits, err)
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

type captureListRepository struct {
	domain.Repository
	targetScope string
	captured    chan struct{}
	release     chan struct{}
	mu          sync.Mutex
	didCapture  bool
}

func (r *captureListRepository) ListUserQuestions(ctx context.Context, subjectID string) ([]domain.Question, error) {
	questions, err := r.Repository.ListUserQuestions(ctx, subjectID)
	scope := "user:" + subjectID
	if err != nil || scope != r.targetScope {
		return questions, err
	}
	r.mu.Lock()
	shouldCapture := !r.didCapture
	r.didCapture = true
	r.mu.Unlock()
	if shouldCapture {
		close(r.captured)
		<-r.release
	}
	return questions, nil
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
