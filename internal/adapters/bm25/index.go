package bm25

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"

	"interview-agent/internal/domain"
)

const (
	k1 = 1.5
	b  = 0.75
)

type document struct {
	question domain.Question
	terms    map[string]int
	length   int
}

type scopeIndex struct {
	documents []document
	docFreq   map[string]int
	avgLength float64
}

// Index is the process-local, rebuildable retrieval adapter. Each scope is an
// immutable snapshot, so replacing one user scope is atomic for readers.
type Index struct {
	mu     sync.RWMutex
	scopes map[string]*scopeIndex
	closed bool
}

func New() *Index {
	return &Index{scopes: make(map[string]*scopeIndex)}
}

func (i *Index) Check(context.Context) error {
	if i == nil {
		return fmt.Errorf("bm25: index is nil")
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.closed {
		return fmt.Errorf("bm25: index is closed")
	}
	return nil
}

func (i *Index) ReplaceScope(scope string, questions []domain.Question) error {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return fmt.Errorf("bm25: scope is required")
	}
	newScope := buildScope(questions)
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return fmt.Errorf("bm25: index is closed")
	}
	i.scopes[scope] = newScope
	return nil
}

func (i *Index) RemoveScope(scope string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return
	}
	delete(i.scopes, scope)
}

func (i *Index) Search(ctx context.Context, request domain.SearchRequest) ([]domain.SearchHit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.Limit <= 0 {
		return nil, nil
	}
	queryTerms := tokenize(request.Query)
	if len(queryTerms) == 0 {
		return nil, nil
	}
	allowedTypes := make(map[domain.QuestionType]bool, len(request.Types))
	for _, questionType := range request.Types {
		allowedTypes[questionType] = true
	}

	i.mu.RLock()
	if i.closed {
		i.mu.RUnlock()
		return nil, fmt.Errorf("bm25: index is closed")
	}
	scopes := make([]*scopeIndex, 0, len(request.Scopes))
	for _, name := range request.Scopes {
		if scope := i.scopes[name]; scope != nil {
			scopes = append(scopes, scope)
		}
	}
	i.mu.RUnlock()

	var hits []domain.SearchHit
	for _, scope := range scopes {
		for _, document := range scope.documents {
			if len(allowedTypes) > 0 && !allowedTypes[document.question.Type] {
				continue
			}
			score := score(scope, document, queryTerms)
			if score > 0 {
				hits = append(hits, domain.SearchHit{Question: document.question, Score: score})
			}
		}
	}
	sort.SliceStable(hits, func(left, right int) bool {
		if hits[left].Score == hits[right].Score {
			return hits[left].Question.ID < hits[right].Question.ID
		}
		return hits[left].Score > hits[right].Score
	})
	if len(hits) > request.Limit {
		hits = hits[:request.Limit]
	}
	return hits, nil
}

func (i *Index) ScopeSize(scope string) int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if value := i.scopes[scope]; value != nil {
		return len(value.documents)
	}
	return 0
}

func (i *Index) Close() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = true
	i.scopes = nil
}

func buildScope(questions []domain.Question) *scopeIndex {
	scope := &scopeIndex{docFreq: make(map[string]int)}
	for _, question := range questions {
		terms := tokenize(question.Topic + " " + question.Text + " " + question.Answer)
		if len(terms) == 0 {
			continue
		}
		frequency := make(map[string]int)
		for _, term := range terms {
			frequency[term]++
		}
		for term := range frequency {
			scope.docFreq[term]++
		}
		scope.documents = append(scope.documents, document{question: question, terms: frequency, length: len(terms)})
		scope.avgLength += float64(len(terms))
	}
	if len(scope.documents) > 0 {
		scope.avgLength /= float64(len(scope.documents))
	}
	return scope
}

func score(scope *scopeIndex, document document, queryTerms []string) float64 {
	if scope.avgLength == 0 {
		return 0
	}
	seen := make(map[string]bool)
	var result float64
	for _, term := range queryTerms {
		if seen[term] {
			continue
		}
		seen[term] = true
		frequency := float64(document.terms[term])
		if frequency == 0 {
			continue
		}
		documentFrequency := float64(scope.docFreq[term])
		documentCount := float64(len(scope.documents))
		idf := math.Log(1 + (documentCount-documentFrequency+0.5)/(documentFrequency+0.5))
		normalizedLength := float64(document.length) / scope.avgLength
		result += idf * frequency * (k1 + 1) / (frequency + k1*(1-b+b*normalizedLength))
	}
	return result
}

func tokenize(value string) []string {
	value = strings.ToLower(value)
	var terms []string
	var word []rune
	var hanRun []rune
	flushWord := func() {
		if len(word) > 0 {
			terms = append(terms, string(word))
			word = word[:0]
		}
	}
	flushHan := func() {
		for _, char := range hanRun {
			terms = append(terms, string(char))
		}
		for index := 0; index+1 < len(hanRun); index++ {
			terms = append(terms, string(hanRun[index:index+2]))
		}
		hanRun = hanRun[:0]
	}
	for _, char := range value {
		switch {
		case unicode.Is(unicode.Han, char):
			flushWord()
			hanRun = append(hanRun, char)
		case unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_':
			flushHan()
			word = append(word, char)
		default:
			flushWord()
			flushHan()
		}
	}
	flushWord()
	flushHan()
	return terms
}
