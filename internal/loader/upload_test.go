package loader

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParseDocumentBytesSupportsConfirmedFormats(t *testing.T) {
	tests := []struct {
		name, mime string
		data       []byte
		want       string
	}{
		{"resume.txt", "text/plain", []byte("Go backend engineer"), "Go backend"},
		{"resume.md", "text/markdown", []byte("# Resume\n\nDistributed systems"), "Distributed systems"},
		{"resume.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", docxFixture(t, "DOCX backend engineer"), "DOCX backend"},
		{"resume.pdf", "application/pdf", pdfFixture("PDF backend engineer"), "PDF backend"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseDocumentBytes(test.name, test.mime, test.data)
			if err != nil {
				t.Fatalf("%v: %v", err, err.(*DocumentError).Err)
			}
			if !strings.Contains(got, test.want) {
				t.Fatalf("text=%q", got)
			}
		})
	}
}

func TestParseDocumentBytesRejectsInvalidFormatAndEmptyText(t *testing.T) {
	if _, err := ParseDocumentBytes("resume.exe", "application/octet-stream", []byte("x")); err == nil || err.(*DocumentError).Code != "unsupported_format" {
		t.Fatalf("unsupported error=%v", err)
	}
	if _, err := ParseDocumentBytes("resume.txt", "text/plain", []byte("  ")); err == nil || err.(*DocumentError).Code != "parse_failed" {
		t.Fatalf("empty error=%v", err)
	}
}

func TestParseDocumentBytesRejectsUnsafeNamesMIMEAndSize(t *testing.T) {
	tests := []struct {
		name, filename, mime string
		data                 []byte
		code                 string
	}{
		{name: "path traversal", filename: "../resume.txt", mime: "text/plain", data: []byte("resume"), code: "invalid_filename"},
		{name: "windows path", filename: `..\\resume.txt`, mime: "text/plain", data: []byte("resume"), code: "invalid_filename"},
		{name: "generic binary mime", filename: "resume.txt", mime: "application/octet-stream", data: []byte("resume"), code: "unsupported_format"},
		{name: "spoofed pdf", filename: "resume.pdf", mime: "application/pdf", data: []byte("not a pdf"), code: "unsupported_format"},
		{name: "oversized", filename: "resume.txt", mime: "text/plain", data: make([]byte, MaxDocumentBytes+1), code: "invalid_size"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseDocumentBytes(test.filename, test.mime, test.data)
			var documentErr *DocumentError
			if !errors.As(err, &documentErr) {
				t.Fatalf("error=%v, want DocumentError %q", err, test.code)
			}
			if documentErr.Code != test.code {
				t.Fatalf("error=%v code=%q, want %q", err, documentErr.Code, test.code)
			}
		})
	}
}

func docxFixture(t *testing.T, text string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	files := map[string]string{"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`, "_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`, "word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`, "word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`}
	for name, body := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func pdfFixture(text string) []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>", "", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	stream := "BT /F1 12 Tf 72 720 Td (" + text + ") Tj ET"
	objects[3] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, obj := range objects {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i < len(offsets); i++ {
		b.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(b.String())
}
