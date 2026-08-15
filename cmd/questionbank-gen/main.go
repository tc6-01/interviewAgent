package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"interview-agent/internal/domain"
)

var (
	sectionPattern  = regexp.MustCompile(`^##\s+\*\*(.+?)\*\*\s*$`)
	questionPattern = regexp.MustCompile(`^###\s+\*\*(.+?)\*\*\s*$`)
	numberPattern   = regexp.MustCompile(`^\d+(?:\.\d+)*\.?\s*`)
	htmlPattern     = regexp.MustCompile(`<[^>]+>`)
)

type bank struct {
	SchemaVersion string            `json:"schema_version"`
	Version       string            `json:"version"`
	Name          string            `json:"name"`
	Source        string            `json:"source"`
	Questions     []domain.Question `json:"questions"`
}

func main() {
	input := flag.String("input", "data/questions/go_interview/go_interview.md", "source markdown")
	output := flag.String("output", "internal/questionbank/assets/go_v1.json", "generated JSON")
	flag.Parse()

	questions, err := parse(*input)
	if err != nil {
		fatal(err)
	}
	payload, err := json.MarshalIndent(bank{
		SchemaVersion: "1",
		Version:       "go-2026.08.15",
		Name:          "Go backend builtin question bank",
		Source:        *input,
		Questions:     questions,
	}, "", "  ")
	if err != nil {
		fatal(err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(*output, payload, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("generated %d questions in %s\n", len(questions), *output)
}

func parse(path string) ([]domain.Question, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var questions []domain.Question
	var topic string
	var current *domain.Question
	var answer strings.Builder
	flush := func() {
		if current == nil {
			return
		}
		current.Answer = cleanAnswer(answer.String())
		questions = append(questions, *current)
		current = nil
		answer.Reset()
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if match := sectionPattern.FindStringSubmatch(line); match != nil {
			flush()
			topic = cleanHeading(match[1])
			continue
		}
		if match := questionPattern.FindStringSubmatch(line); match != nil {
			flush()
			id := fmt.Sprintf("go-%03d", len(questions)+1)
			current = &domain.Question{
				ID:     id,
				Type:   domain.QuestionTypeBasic,
				Topic:  topic,
				Text:   cleanHeading(match[1]),
				Source: path + "#" + id,
			}
			continue
		}
		if current != nil {
			answer.WriteString(line)
			answer.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	if len(questions) == 0 {
		return nil, fmt.Errorf("no questions parsed from %s", path)
	}
	return questions, nil
}

func cleanHeading(value string) string {
	value = strings.ReplaceAll(value, "&#x20;", "")
	value = numberPattern.ReplaceAllString(value, "")
	return strings.TrimSpace(value)
}

func cleanAnswer(value string) string {
	value = htmlPattern.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "&#x20;", " ")
	value = strings.ReplaceAll(value, "\\[", "[")
	value = strings.ReplaceAll(value, "\\]", "]")
	lines := strings.Split(value, "\n")
	cleaned := lines[:0]
	blank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
