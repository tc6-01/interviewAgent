/**
 * @author: 公众号：IT杨秀才
 * @doc:后端，AI Agent知识进阶，后端、AI大模型、场景题面试大全：https://golangstar.cn/
 */

// Package loader 实现文档加载，支持 PDF / TXT / DOCX / Markdown 文件解析
package loader

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadFile 根据文件扩展名自动选择解析器，返回纯文本内容
func LoadFile(path string) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("loader: 文件不存在: %s", path)
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".markdown":
		return loadText(path)
	case ".pdf":
		return loadPDF(path)
	case ".docx":
		return loadDOCX(path)
	case ".doc":
		return "", fmt.Errorf("loader: 不支持 .doc 格式（Word 97-2003），请转换为 .docx")
	default:
		return "", fmt.Errorf("loader: 不支持的文件格式 %q", ext)
	}
}

// ParseBase64File 解码 base64 文件并提取文本
func ParseBase64File(filename string, base64Data string) (string, error) {
	if base64.StdEncoding.DecodedLen(len(base64Data)) > MaxDocumentBytes {
		return "", fmt.Errorf("base64 文件超过 10 MB 限制")
	}
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("base64 解码失败: %w", err)
	}

	return ParseDocumentBytes(filename, "", data)
}

// loadText 读取纯文本文件
func loadText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("loader: 读取文件失败: %w", err)
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", fmt.Errorf("loader: 文件内容为空: %s", path)
	}
	return content, nil
}
