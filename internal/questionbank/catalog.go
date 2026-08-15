package questionbank

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"interview-agent/internal/domain"
)

type Catalog struct {
	repository domain.Repository
	index      domain.QuestionIndex
	fallback   domain.QuestionFallback
}

func NewCatalog(repository domain.Repository, index domain.QuestionIndex, fallback domain.QuestionFallback) (*Catalog, error) {
	if repository == nil || index == nil {
		return nil, fmt.Errorf("questionbank: repository and index are required")
	}
	return &Catalog{repository: repository, index: index, fallback: fallback}, nil
}

func (c *Catalog) LoadBuiltin(ctx context.Context) (bool, error) {
	builtin, err := LoadBuiltin()
	if err != nil {
		return false, err
	}
	changed, err := c.repository.EnsureBuiltinBank(ctx, domain.QuestionBank{
		ID:       "builtin-go",
		Filename: "go_v1.json",
		Version:  builtin.Version,
		SHA256:   builtin.SHA256,
	}, builtin.Questions)
	if err != nil {
		return false, err
	}
	questions, err := c.repository.ListQuestions(ctx, "builtin")
	if err != nil {
		return false, err
	}
	if err := c.index.ReplaceScope("builtin", questions); err != nil {
		return false, err
	}
	return changed, nil
}

// LoadAll performs the startup load: it idempotently installs the builtin
// asset, then rebuilds every persisted user scope into a fresh process index.
func (c *Catalog) LoadAll(ctx context.Context) (bool, error) {
	changed, err := c.LoadBuiltin(ctx)
	if err != nil {
		return false, err
	}
	scopes, err := c.repository.ListQuestionScopes(ctx, "user:")
	if err != nil {
		return false, err
	}
	for _, scope := range scopes {
		questions, err := c.repository.ListQuestions(ctx, scope)
		if err != nil {
			return false, err
		}
		if err := c.index.ReplaceScope(scope, questions); err != nil {
			return false, err
		}
	}
	return changed, nil
}

// ReplaceUserBank stores and indexes a structured JSON question bank. The
// filename + content hash makes retries idempotent; changed content replaces
// the prior bank for that subject and filename.
func (c *Catalog) ReplaceUserBank(ctx context.Context, subjectID, filename string, content []byte) (bool, error) {
	if strings.TrimSpace(subjectID) == "" || strings.TrimSpace(filename) == "" {
		return false, fmt.Errorf("questionbank: subject and filename are required")
	}
	questions, err := parseUserQuestions(content, filename)
	if err != nil {
		return false, err
	}
	hash := sha256.Sum256(content)
	hashText := hex.EncodeToString(hash[:])
	bankIDHash := sha256.Sum256([]byte(subjectID + "\x00" + filepath.Base(filename)))
	bankID := "user-" + hex.EncodeToString(bankIDHash[:8])
	for index := range questions {
		questions[index].ID = bankID + ":" + questions[index].ID
	}
	changed, err := c.repository.ReplaceUserBank(ctx, domain.QuestionBank{
		ID:        bankID,
		SubjectID: subjectID,
		Filename:  filepath.Base(filename),
		Version:   "user-v1",
		SHA256:    hashText,
	}, questions)
	if err != nil {
		return false, err
	}
	if err := c.rebuildUserScope(ctx, subjectID); err != nil {
		return false, err
	}
	return changed, nil
}

func (c *Catalog) DeleteUserBank(ctx context.Context, subjectID, filename string) error {
	if err := c.repository.DeleteQuestionBank(ctx, subjectID, filepath.Base(filename)); err != nil {
		return err
	}
	return c.rebuildUserScope(ctx, subjectID)
}

func (c *Catalog) RebuildUserScope(ctx context.Context, subjectID string) error {
	return c.rebuildUserScope(ctx, subjectID)
}

func (c *Catalog) rebuildUserScope(ctx context.Context, subjectID string) error {
	scope := "user:" + subjectID
	questions, err := c.repository.ListQuestions(ctx, scope)
	if err != nil {
		return err
	}
	if len(questions) == 0 {
		c.index.RemoveScope(scope)
		return nil
	}
	return c.index.ReplaceScope(scope, questions)
}

func (c *Catalog) BuildQuestions(ctx context.Context, subjectID, query string) ([]domain.Question, error) {
	plan := domain.FixedQuestionPlan()
	if err := ValidateQuestionPlan(plan); err != nil {
		return nil, err
	}
	scopes := []string{"user:" + subjectID, "builtin"}
	questions := make([]domain.Question, 0, plan.Total)
	missing := make(map[domain.QuestionType]int)
	for _, questionType := range []domain.QuestionType{domain.QuestionTypeBasic, domain.QuestionTypeExperience, domain.QuestionTypeDesign} {
		limit := plan.Counts[questionType]
		hits, err := c.index.Search(ctx, domain.SearchRequest{Scopes: scopes, Query: query, Types: []domain.QuestionType{questionType}, Limit: limit})
		if err != nil {
			return nil, err
		}
		for _, hit := range hits {
			questions = append(questions, hit.Question)
		}
		if len(hits) < limit {
			missing[questionType] = limit - len(hits)
		}
	}
	if len(missing) > 0 {
		if c.fallback == nil {
			return nil, fmt.Errorf("questionbank: retrieval gap %v and no LLM fallback is configured", missing)
		}
		generated, err := c.fallback.GenerateQuestions(ctx, query, missing)
		if err != nil {
			return nil, fmt.Errorf("questionbank: LLM fallback: %w", err)
		}
		generatedCounts := make(map[domain.QuestionType]int)
		for _, question := range generated {
			if !question.Type.Valid() || strings.TrimSpace(question.Text) == "" || strings.TrimSpace(question.Source) == "" {
				return nil, fmt.Errorf("questionbank: LLM fallback returned an invalid question")
			}
			generatedCounts[question.Type]++
		}
		for questionType, count := range missing {
			if generatedCounts[questionType] != count {
				return nil, fmt.Errorf("questionbank: LLM fallback returned %d %s questions, want %d", generatedCounts[questionType], questionType, count)
			}
		}
		questions = append(questions, generated...)
	}
	if len(questions) != plan.Total {
		return nil, fmt.Errorf("questionbank: produced %d questions, want %d", len(questions), plan.Total)
	}
	return questions, nil
}

func parseUserQuestions(content []byte, filename string) ([]domain.Question, error) {
	var envelope struct {
		Questions []domain.Question `json:"questions"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		var questions []domain.Question
		if listErr := json.Unmarshal(content, &questions); listErr != nil {
			return nil, fmt.Errorf("questionbank: parse user bank %s: %w", filename, err)
		}
		envelope.Questions = questions
	}
	if len(envelope.Questions) == 0 {
		return nil, fmt.Errorf("questionbank: user bank %s is empty", filename)
	}
	seen := make(map[string]bool)
	for index := range envelope.Questions {
		question := &envelope.Questions[index]
		if strings.TrimSpace(question.ID) == "" {
			question.ID = fmt.Sprintf("user-%03d", index+1)
		}
		if seen[question.ID] || strings.TrimSpace(question.Text) == "" || !question.Type.Valid() {
			return nil, fmt.Errorf("questionbank: invalid or duplicate user question %q", question.ID)
		}
		seen[question.ID] = true
		if strings.TrimSpace(question.Source) == "" {
			question.Source = "user:" + filepath.Base(filename)
		}
		for _, pattern := range promotionNoise {
			if pattern.MatchString(question.Text) || pattern.MatchString(question.Answer) {
				return nil, fmt.Errorf("questionbank: user question %s contains promotion noise", question.ID)
			}
		}
	}
	sort.SliceStable(envelope.Questions, func(i, j int) bool { return envelope.Questions[i].ID < envelope.Questions[j].ID })
	return envelope.Questions, nil
}
