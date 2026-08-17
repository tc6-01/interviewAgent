package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DocumentError struct {
	Code, Message string
	Err           error
}

func (e *DocumentError) Error() string { return e.Message }
func (e *DocumentError) Unwrap() error { return e.Err }

func ParseDocumentBytes(filename, mime string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	allowed := map[string][]string{
		".pdf":  {"application/pdf", "application/octet-stream"},
		".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/zip", "application/octet-stream"},
		".txt":  {"text/plain", "application/octet-stream"},
		".md":   {"text/markdown", "text/plain", "application/octet-stream"},
	}
	accepted, ok := allowed[ext]
	if !ok {
		return "", &DocumentError{Code: "unsupported_format", Message: "仅支持 .pdf、.docx、.txt、.md 文件"}
	}
	mime = strings.TrimSpace(strings.Split(mime, ";")[0])
	if mime != "" {
		valid := false
		for _, candidate := range accepted {
			if mime == candidate {
				valid = true
				break
			}
		}
		if !valid {
			return "", &DocumentError{Code: "unsupported_format", Message: "文件扩展名与 MIME 类型不匹配"}
		}
	}
	tmp, err := os.CreateTemp("", "interview-document-*"+ext)
	if err != nil {
		return "", fmt.Errorf("loader: create temp file: %w", err)
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("loader: write temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return "", fmt.Errorf("loader: close temp file: %w", err)
	}
	text, err := LoadFile(path)
	if err != nil {
		return "", &DocumentError{Code: "parse_failed", Message: "未能提取文档正文，请粘贴文本重试", Err: err}
	}
	return text, nil
}
