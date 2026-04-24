package paperparse

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParserParsesStudentPaperSections(t *testing.T) {
	docPath := filepath.Join(t.TempDir(), "student.docx")
	createTestDocx(t, docPath, []string{
		"题目：确定性解析测试",
		"学生姓名：张三",
		"摘要",
		"这是第一段中文摘要。",
		"这是第二段中文摘要。",
		"关键词：解析、DOCX；确定性, 测试",
		"1 绪论",
		"正文第一段。",
		"1.1 研究背景",
		"正文第二段。",
		"参考文献",
		"1 张三. 无括号编号文献.",
		"[1] 张三. 测试文献.",
		"[2] 李四. 另一篇文献.",
		"致谢",
		"感谢老师指导。",
	})

	paper, err := NewParser().Parse(context.Background(), docPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got := paper.CoverFields["题目"]; got != "确定性解析测试" {
		t.Fatalf("CoverFields[题目] = %q", got)
	}
	if got := paper.CoverFields["学生姓名"]; got != "张三" {
		t.Fatalf("CoverFields[学生姓名] = %q", got)
	}

	wantAbstract := []string{"这是第一段中文摘要。", "这是第二段中文摘要。"}
	if !reflect.DeepEqual(paper.AbstractCN, wantAbstract) {
		t.Fatalf("AbstractCN = %#v, want %#v", paper.AbstractCN, wantAbstract)
	}

	wantKeywords := []string{"解析", "DOCX", "确定性", "测试"}
	if !reflect.DeepEqual(paper.KeywordsCN, wantKeywords) {
		t.Fatalf("KeywordsCN = %#v, want %#v", paper.KeywordsCN, wantKeywords)
	}

	wantHeadings := []Heading{
		{Level: 1, Text: "绪论"},
		{Level: 2, Text: "研究背景"},
	}
	if !reflect.DeepEqual(paper.Headings, wantHeadings) {
		t.Fatalf("Headings = %#v, want %#v", paper.Headings, wantHeadings)
	}

	wantBody := []string{"正文第一段。", "正文第二段。"}
	if !reflect.DeepEqual(paper.Body, wantBody) {
		t.Fatalf("Body = %#v, want %#v", paper.Body, wantBody)
	}

	wantReferences := []string{"1 张三. 无括号编号文献.", "[1] 张三. 测试文献.", "[2] 李四. 另一篇文献."}
	if !reflect.DeepEqual(paper.References, wantReferences) {
		t.Fatalf("References = %#v, want %#v", paper.References, wantReferences)
	}

	wantAcknowledgements := []string{"感谢老师指导。"}
	if !reflect.DeepEqual(paper.Acknowledgements, wantAcknowledgements) {
		t.Fatalf("Acknowledgements = %#v, want %#v", paper.Acknowledgements, wantAcknowledgements)
	}

	if len(paper.Abnormal) != 0 {
		t.Fatalf("Abnormal = %#v, want empty", paper.Abnormal)
	}
}

func TestParserReturnsErrorsForInvalidPath(t *testing.T) {
	parser := NewParser()

	if _, err := parser.Parse(context.Background(), ""); err == nil {
		t.Fatal("Parse(empty path) error = nil")
	}

	missingPath := filepath.Join(t.TempDir(), "missing.docx")
	if _, err := parser.Parse(context.Background(), missingPath); err == nil {
		t.Fatal("Parse(missing path) error = nil")
	}
}

func createTestDocx(t *testing.T, path string, paragraphs []string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test docx: %v", err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	defer zw.Close()

	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/document.xml":   testDocumentXML(paragraphs),
	}

	for name, content := range entries {
		writer, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
}

func testDocumentXML(paragraphs []string) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	builder.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, paragraph := range paragraphs {
		builder.WriteString(`<w:p><w:r><w:t>`)
		builder.WriteString(paragraph)
		builder.WriteString(`</w:t></w:r></w:p>`)
	}
	builder.WriteString(`</w:body></w:document>`)
	return builder.String()
}
