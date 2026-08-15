package questionbank

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"interview-agent/internal/domain"
)

const SchemaVersion = "1"

var promotionNoise = []*regexp.Regexp{
	regexp.MustCompile(`(?i)公众号`),
	regexp.MustCompile(`(?i)扫码`),
	regexp.MustCompile(`(?i)加微信`),
	regexp.MustCompile(`(?i)领取.{0,16}题库`),
	regexp.MustCompile(`(?i)面试题库\s*pdf`),
	regexp.MustCompile(`(?i)golangstar\.cn`),
	regexp.MustCompile(`(?i)it杨秀才`),
}

//go:embed assets/*.json
var assets embed.FS

type BuiltinBank struct {
	SchemaVersion string            `json:"schema_version"`
	Version       string            `json:"version"`
	Name          string            `json:"name"`
	Source        string            `json:"source"`
	Questions     []domain.Question `json:"questions"`
	SHA256        string            `json:"-"`
}

func LoadBuiltin() (BuiltinBank, error) {
	content, err := assets.ReadFile("assets/go_v1.json")
	if err != nil {
		return BuiltinBank{}, fmt.Errorf("questionbank: read builtin asset: %w", err)
	}
	if err := validateBuiltinAssetSchema(content); err != nil {
		return BuiltinBank{}, err
	}
	var bank BuiltinBank
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bank); err != nil {
		return BuiltinBank{}, fmt.Errorf("questionbank: decode builtin asset: %w", err)
	}
	hash := sha256.Sum256(content)
	bank.SHA256 = hex.EncodeToString(hash[:])
	if err := ValidateBuiltin(bank); err != nil {
		return BuiltinBank{}, err
	}
	return bank, nil
}

func validateBuiltinAssetSchema(content []byte) error {
	schemaContent, err := assets.ReadFile("assets/schema.json")
	if err != nil {
		return fmt.Errorf("questionbank: read builtin schema: %w", err)
	}
	var schemaDocument jsonschema.Schema
	if err := json.Unmarshal(schemaContent, &schemaDocument); err != nil {
		return fmt.Errorf("questionbank: decode builtin schema: %w", err)
	}
	resolved, err := schemaDocument.Resolve(nil)
	if err != nil {
		return fmt.Errorf("questionbank: resolve builtin schema: %w", err)
	}
	var document any
	if err := json.Unmarshal(content, &document); err != nil {
		return fmt.Errorf("questionbank: decode builtin asset for schema validation: %w", err)
	}
	if err := resolved.Validate(document); err != nil {
		return fmt.Errorf("questionbank: builtin asset violates schema: %w", err)
	}
	return nil
}

func ValidateBuiltin(bank BuiltinBank) error {
	if bank.SchemaVersion != SchemaVersion {
		return fmt.Errorf("questionbank: schema_version %q is unsupported", bank.SchemaVersion)
	}
	if strings.TrimSpace(bank.Version) == "" || strings.TrimSpace(bank.Name) == "" || strings.TrimSpace(bank.Source) == "" {
		return fmt.Errorf("questionbank: version, name and source are required")
	}
	if len(bank.Questions) == 0 {
		return fmt.Errorf("questionbank: at least one question is required")
	}
	seen := make(map[string]bool, len(bank.Questions))
	for index, question := range bank.Questions {
		if strings.TrimSpace(question.ID) == "" || strings.TrimSpace(question.Text) == "" || strings.TrimSpace(question.Source) == "" {
			return fmt.Errorf("questionbank: question %d is missing id, text or source", index)
		}
		if !question.Type.Valid() {
			return fmt.Errorf("questionbank: question %s has invalid type %q", question.ID, question.Type)
		}
		if seen[question.ID] {
			return fmt.Errorf("questionbank: duplicate question id %s", question.ID)
		}
		seen[question.ID] = true
		for _, pattern := range promotionNoise {
			if pattern.MatchString(question.Text) || pattern.MatchString(question.Answer) || pattern.MatchString(question.Source) {
				return fmt.Errorf("questionbank: question %s contains promotion noise matching %q", question.ID, pattern.String())
			}
		}
	}
	return nil
}

func ValidateQuestionPlan(plan domain.QuestionPlan) error {
	want := domain.FixedQuestionPlan()
	if plan.Total != want.Total {
		return fmt.Errorf("questionbank: plan total=%d, want %d", plan.Total, want.Total)
	}
	for questionType, count := range want.Counts {
		if plan.Counts[questionType] != count {
			return fmt.Errorf("questionbank: plan %s=%d, want %d", questionType, plan.Counts[questionType], count)
		}
	}
	return nil
}
