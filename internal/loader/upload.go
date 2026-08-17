package loader

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const MaxDocumentBytes = 10 << 20

const maxExtractedDocumentRunes = 200_000

type DocumentError struct {
	Code, Message string
	Err           error
}

func (e *DocumentError) Error() string { return e.Message }
func (e *DocumentError) Unwrap() error { return e.Err }

func ParseDocumentBytes(filename, mime string, data []byte) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" || strings.ContainsAny(filename, "/\\\x00") || filepath.Base(filename) != filename {
		return "", &DocumentError{Code: "invalid_filename", Message: "文件名不合法"}
	}
	if len(data) == 0 || len(data) > MaxDocumentBytes {
		return "", &DocumentError{Code: "invalid_size", Message: "文件大小必须在 1 字节到 10 MB 之间"}
	}
	ext := strings.ToLower(filepath.Ext(filename))
	allowed := map[string][]string{
		".pdf":  {"application/pdf"},
		".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/zip"},
		".txt":  {"text/plain"},
		".md":   {"text/markdown", "text/plain"},
	}
	accepted, ok := allowed[ext]
	if !ok {
		return "", &DocumentError{Code: "unsupported_format", Message: "仅支持 .pdf、.docx、.txt、.md 文件"}
	}
	declaredMIME := strings.TrimSpace(strings.Split(mime, ";")[0])
	detectedMIME := strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0])
	if declaredMIME != "" && !containsMIME(accepted, declaredMIME) {
		return "", &DocumentError{Code: "unsupported_format", Message: "文件扩展名与 MIME 类型不匹配"}
	}
	if !containsMIME(accepted, detectedMIME) {
		return "", &DocumentError{Code: "unsupported_format", Message: "文件内容与扩展名不匹配"}
	}
	if ext == ".docx" {
		if err := validateDOCXArchive(data); err != nil {
			return "", &DocumentError{Code: "unsafe_archive", Message: "DOCX 压缩包不安全或已损坏", Err: err}
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
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > maxExtractedDocumentRunes {
		return "", &DocumentError{Code: "document_text_too_large", Message: "提取后的文档正文过大"}
	}
	return text, nil
}

func containsMIME(allowed []string, value string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateDOCXArchive(data []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	var total uint64
	foundDocument := false
	for _, file := range reader.File {
		cleaned := filepath.Clean(file.Name)
		if filepath.IsAbs(file.Name) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive contains unsafe path")
		}
		total += file.UncompressedSize64
		if total > 20<<20 {
			return fmt.Errorf("archive expands beyond limit")
		}
		if file.Name == "word/document.xml" {
			foundDocument = true
		}
	}
	if !foundDocument {
		return fmt.Errorf("archive has no word/document.xml")
	}
	return nil
}
