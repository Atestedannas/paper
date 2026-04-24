package paperparse

import (
	"archive/zip"
	"context"
	"encoding/json"
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

func TestParserParsesInlineSectionMarkers(t *testing.T) {
	docPath := filepath.Join(t.TempDir(), "inline-markers.docx")
	createTestDocx(t, docPath, []string{
		"摘要：同段摘要内容",
		"关键词: 解析，确定性",
		"1 绪论",
		"正文内容",
		" 参考文献： ",
		"[1] 参考文献内容",
		"致谢：同段致谢内容",
	})

	paper, err := NewParser().Parse(context.Background(), docPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if want := []string{"同段摘要内容"}; !reflect.DeepEqual(paper.AbstractCN, want) {
		t.Fatalf("AbstractCN = %#v, want %#v", paper.AbstractCN, want)
	}
	if want := []string{"解析", "确定性"}; !reflect.DeepEqual(paper.KeywordsCN, want) {
		t.Fatalf("KeywordsCN = %#v, want %#v", paper.KeywordsCN, want)
	}
	if want := []string{"[1] 参考文献内容"}; !reflect.DeepEqual(paper.References, want) {
		t.Fatalf("References = %#v, want %#v", paper.References, want)
	}
	if want := []string{"同段致谢内容"}; !reflect.DeepEqual(paper.Acknowledgements, want) {
		t.Fatalf("Acknowledgements = %#v, want %#v", paper.Acknowledgements, want)
	}
}

func TestParserParsesMarkerOnlyKeywordSection(t *testing.T) {
	docPath := filepath.Join(t.TempDir(), "marker-only-keywords.docx")
	createTestDocx(t, docPath, []string{
		"摘要",
		"摘要内容",
		"关键词",
		"解析、DOCX、确定性",
		"1 绪论",
		"正文内容",
	})

	paper, err := NewParser().Parse(context.Background(), docPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	wantKeywords := []string{"解析", "DOCX", "确定性"}
	if !reflect.DeepEqual(paper.KeywordsCN, wantKeywords) {
		t.Fatalf("KeywordsCN = %#v, want %#v", paper.KeywordsCN, wantKeywords)
	}

	wantBody := []string{"正文内容"}
	if !reflect.DeepEqual(paper.Body, wantBody) {
		t.Fatalf("Body = %#v, want %#v", paper.Body, wantBody)
	}
}

func TestParserPreservesRealOOXMLParagraphText(t *testing.T) {
	docPath := filepath.Join(t.TempDir(), "real-ooxml.docx")
	documentXML := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:xml="http://www.w3.org/XML/1998/namespace"><w:body>` +
		`<w:p><w:r><w:t>摘要</w:t></w:r><w:r><w:t>：</w:t></w:r><w:r><w:t xml:space="preserve"> 前半</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>后半</w:t></w:r><w:r><w:br/></w:r><w:r><w:t>换行</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>关键词</w:t></w:r><w:r><w:t>：</w:t></w:r><w:r><w:t>解析</w:t></w:r><w:r><w:t>、</w:t></w:r><w:r><w:t>OOXML</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>1</w:t></w:r><w:r><w:t xml:space="preserve"> 绪论</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>正文</w:t></w:r><w:r><w:t xml:space="preserve"> token</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>分隔</w:t></w:r><w:r><w:br/></w:r><w:r><w:t>下一行</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	createTestDocxWithDocumentXML(t, docPath, documentXML)

	paper, err := NewParser().Parse(context.Background(), docPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if want := []string{"前半\t后半\n换行"}; !reflect.DeepEqual(paper.AbstractCN, want) {
		t.Fatalf("AbstractCN = %#v, want %#v", paper.AbstractCN, want)
	}
	if want := []string{"解析", "OOXML"}; !reflect.DeepEqual(paper.KeywordsCN, want) {
		t.Fatalf("KeywordsCN = %#v, want %#v", paper.KeywordsCN, want)
	}
	if want := []Heading{{Level: 1, Text: "绪论"}}; !reflect.DeepEqual(paper.Headings, want) {
		t.Fatalf("Headings = %#v, want %#v", paper.Headings, want)
	}
	if want := []string{"正文 token\t分隔\n下一行"}; !reflect.DeepEqual(paper.Body, want) {
		t.Fatalf("Body = %#v, want %#v", paper.Body, want)
	}
}

func TestParserMarshalsSnakeCaseJSON(t *testing.T) {
	paper := ParsedPaper{
		CoverFields:      map[string]string{"题目": "测试"},
		AbstractCN:       []string{"摘要"},
		KeywordsCN:       []string{"关键词"},
		Headings:         []Heading{{Level: 1, Text: "绪论"}},
		Body:             []string{"正文"},
		References:       []string{"参考文献"},
		Acknowledgements: []string{"致谢"},
		Abnormal:         []string{"异常"},
	}

	content, err := json.Marshal(paper)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	jsonText := string(content)

	for _, field := range []string{"cover_fields", "abstract_cn", "keywords_cn", "acknowledgements"} {
		if !strings.Contains(jsonText, `"`+field+`"`) {
			t.Fatalf("marshaled JSON %s does not contain field %q", jsonText, field)
		}
	}
	for _, field := range []string{"CoverFields", "AbstractCN", "KeywordsCN", "Acknowledgements"} {
		if strings.Contains(jsonText, `"`+field+`"`) {
			t.Fatalf("marshaled JSON %s contains Go field name %q", jsonText, field)
		}
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

	writeTestDocxEntries(t, zw, testDocumentXML(paragraphs))
}

func createTestDocxWithDocumentXML(t *testing.T, path string, documentXML string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test docx: %v", err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	defer zw.Close()

	writeTestDocxEntries(t, zw, documentXML)
}

func writeTestDocxEntries(t *testing.T, zw *zip.Writer, documentXML string) {
	t.Helper()

	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/document.xml":   documentXML,
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
