package fileprocessor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalSemanticHTMLConverterConvertDocxToHTMLPreservesHeadingsAndTables(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := writeTinyDocxFixture(t, tmpDir, "input.docx", map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>这是正文。</w:t></w:r></w:p>` +
			`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>A</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>B</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
			`</w:body></w:document>`,
	})
	outputPath := filepath.Join(tmpDir, "source.html")

	converter := NewLocalSemanticHTMLConverter()
	got, err := converter.ConvertDocxToHTML(context.Background(), inputPath, outputPath)
	if err != nil {
		t.Fatalf("ConvertDocxToHTML() error = %v", err)
	}
	if got != outputPath {
		t.Fatalf("ConvertDocxToHTML() = %q, want %q", got, outputPath)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(output) error = %v", err)
	}
	htmlText := string(htmlBytes)
	if !strings.Contains(htmlText, "<h1>1 绪论</h1>") {
		t.Fatalf("html = %q, want heading", htmlText)
	}
	if !strings.Contains(htmlText, "<p>这是正文。</p>") {
		t.Fatalf("html = %q, want paragraph", htmlText)
	}
	if !strings.Contains(htmlText, "<table>") || !strings.Contains(htmlText, "<td>A</td>") {
		t.Fatalf("html = %q, want table cells", htmlText)
	}
}

func TestLocalSemanticHTMLConverterConvertHTMLToDocxWritesDocument(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "rebuilt.html")
	outputPath := filepath.Join(tmpDir, "rebuilt.docx")
	htmlText := `<html><body><h1>1 绪论</h1><p>这是正文。</p><table><tr><td>A</td><td>B</td></tr></table></body></html>`
	if err := os.WriteFile(inputPath, []byte(htmlText), 0644); err != nil {
		t.Fatalf("WriteFile(input) error = %v", err)
	}

	converter := NewLocalSemanticHTMLConverter()
	got, err := converter.ConvertHTMLToDocx(context.Background(), inputPath, outputPath)
	if err != nil {
		t.Fatalf("ConvertHTMLToDocx() error = %v", err)
	}
	if got != outputPath {
		t.Fatalf("ConvertHTMLToDocx() = %q, want %q", got, outputPath)
	}

	pkg, err := openDocxPackage(outputPath)
	if err != nil {
		t.Fatalf("openDocxPackage(output) error = %v", err)
	}
	documentXML := string(pkg.entries["word/document.xml"])
	if !strings.Contains(documentXML, "1 绪论") {
		t.Fatalf("document.xml = %q, want heading text", documentXML)
	}
	if !strings.Contains(documentXML, "这是正文。") {
		t.Fatalf("document.xml = %q, want paragraph text", documentXML)
	}
	if !strings.Contains(documentXML, "A") || !strings.Contains(documentXML, "B") {
		t.Fatalf("document.xml = %q, want table text", documentXML)
	}
}
