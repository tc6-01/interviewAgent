package bm25

import (
	"context"
	"testing"

	"interview-agent/internal/domain"
)

func TestScopedSearchIsolationAndAtomicReplacement(t *testing.T) {
	ctx := context.Background()
	index := New()
	if err := index.ReplaceScope("builtin", []domain.Question{
		{ID: "builtin-go", Type: domain.QuestionTypeBasic, Text: "Go channel 并发原理", Source: "builtin"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := index.ReplaceScope("user:a", []domain.Question{
		{ID: "a-private", Type: domain.QuestionTypeBasic, Text: "订单支付幂等设计", Source: "user:a"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := index.ReplaceScope("user:b", []domain.Question{
		{ID: "b-private", Type: domain.QuestionTypeBasic, Text: "推荐系统召回设计", Source: "user:b"},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := index.Search(ctx, domain.SearchRequest{Scopes: []string{"builtin", "user:a"}, Query: "支付 幂等", Limit: 5})
	if err != nil || len(hits) != 1 || hits[0].Question.ID != "a-private" {
		t.Fatalf("subject a hits = %#v, %v", hits, err)
	}
	hits, err = index.Search(ctx, domain.SearchRequest{Scopes: []string{"builtin", "user:b"}, Query: "支付 幂等", Limit: 5})
	if err != nil || len(hits) != 0 {
		t.Fatalf("subject b leaked hits = %#v, %v", hits, err)
	}

	if err := index.ReplaceScope("user:a", []domain.Question{
		{ID: "a-replaced", Type: domain.QuestionTypeDesign, Text: "库存扣减一致性设计", Source: "user:a"},
	}); err != nil {
		t.Fatal(err)
	}
	if index.ScopeSize("user:a") != 1 {
		t.Fatalf("scope size = %d, want 1", index.ScopeSize("user:a"))
	}
	hits, _ = index.Search(ctx, domain.SearchRequest{Scopes: []string{"user:a"}, Query: "支付", Limit: 5})
	if len(hits) != 0 {
		t.Fatalf("old document remained after replacement: %#v", hits)
	}
	hits, _ = index.Search(ctx, domain.SearchRequest{Scopes: []string{"user:a"}, Query: "库存 一致性", Types: []domain.QuestionType{domain.QuestionTypeDesign}, Limit: 5})
	if len(hits) != 1 || hits[0].Question.ID != "a-replaced" {
		t.Fatalf("replacement not searchable: %#v", hits)
	}

	index.RemoveScope("user:a")
	if index.ScopeSize("user:a") != 0 {
		t.Fatalf("removed scope size = %d", index.ScopeSize("user:a"))
	}
}
