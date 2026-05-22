package templatefiller

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────────────────────────────
// Golden template generation tests
// ──────────────────────────────────────────────────

func TestGenerateGoldenTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test_golden.docx")

	cfg := DefaultCQRWSTConfig()
	if err := GenerateGoldenTemplate(outputPath, cfg); err != nil {
		t.Fatalf("GenerateGoldenTemplate failed: %v", err)
	}

	// Verify file exists and is a valid ZIP
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(data) < 100 {
		t.Fatal("output file too small")
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a valid ZIP: %v", err)
	}

	// Check required entries exist
	entries := make(map[string]bool)
	for _, f := range reader.File {
		entries[f.Name] = true
	}

	required := []string{
		"[Content_Types].xml",
		"_rels/.rels",
		"word/document.xml",
		"word/styles.xml",
		"word/settings.xml",
		"word/header1.xml",
		"word/footer1.xml",
		"word/_rels/document.xml.rels",
	}
	for _, name := range required {
		if !entries[name] {
			t.Errorf("missing required ZIP entry: %s", name)
		}
	}

	// Verify document.xml contains placeholders
	docXML := readZipEntry(t, reader, "word/document.xml")
	placeholders := []string{
		"{{COVER_TITLE}}", "{{ABSTRACT_TITLE}}", "{{ABSTRACT_CONTENT}}",
		"{{KEYWORDS}}", "{{BODY}}", "{{REFERENCES_TITLE}}",
		"{{REFERENCES_CONTENT}}", "{{ACKNOWLEDGEMENTS_TITLE}}",
	}
	for _, ph := range placeholders {
		if !strings.Contains(docXML, ph) {
			t.Errorf("document.xml missing placeholder: %s", ph)
		}
	}

	// Verify styles.xml has required style definitions
	stylesContent := readZipEntry(t, reader, "word/styles.xml")
	requiredStyles := []string{"Normal", "Heading1", "Heading2", "Heading3", "AbstractTitle", "ReferencesTitle", "References"}
	for _, sid := range requiredStyles {
		if !strings.Contains(stylesContent, `w:styleId="`+sid+`"`) {
			t.Errorf("styles.xml missing style: %s", sid)
		}
	}

	// Verify header contains university name
	headerContent := readZipEntry(t, reader, "word/header1.xml")
	if !strings.Contains(headerContent, cfg.HeaderText) {
		t.Error("header1.xml missing university name")
	}

	// Verify footer has page number fields
	footerContent := readZipEntry(t, reader, "word/footer1.xml")
	if !strings.Contains(footerContent, "PAGE") {
		t.Error("footer1.xml missing PAGE field")
	}
	if !strings.Contains(footerContent, "NUMPAGES") {
		t.Error("footer1.xml missing NUMPAGES field")
	}
}

// ──────────────────────────────────────────────────
// Placeholder system tests
// ──────────────────────────────────────────────────

func TestFindPlaceholderInText(t *testing.T) {
	tests := []struct {
		text     string
		wantTag  string
		wantNil  bool
	}{
		{"{{ABSTRACT_CONTENT}}", "{{ABSTRACT_CONTENT}}", false},
		{"  {{BODY}}  ", "{{BODY}}", false},
		{"Some random text", "", true},
		{"{{REFERENCES_TITLE}}", "{{REFERENCES_TITLE}}", false},
		{"", "", true},
	}

	for _, tt := range tests {
		ph := FindPlaceholderInText(tt.text)
		if tt.wantNil {
			if ph != nil {
				t.Errorf("FindPlaceholderInText(%q) = %v, want nil", tt.text, ph.Tag)
			}
			continue
		}
		if ph == nil {
			t.Errorf("FindPlaceholderInText(%q) = nil, want %s", tt.text, tt.wantTag)
			continue
		}
		if ph.Tag != tt.wantTag {
			t.Errorf("FindPlaceholderInText(%q).Tag = %s, want %s", tt.text, ph.Tag, tt.wantTag)
		}
	}
}

func TestPlaceholderMap(t *testing.T) {
	m := PlaceholderMap()
	if _, ok := m["body"]; !ok {
		t.Error("PlaceholderMap missing 'body'")
	}
	if _, ok := m["abstract"]; !ok {
		t.Error("PlaceholderMap missing 'abstract'")
	}
	if _, ok := m["references"]; !ok {
		t.Error("PlaceholderMap missing 'references'")
	}
}

func TestIsPlaceholderText(t *testing.T) {
	if !IsPlaceholderText("{{BODY}}") {
		t.Error("expected true for {{BODY}}")
	}
	if IsPlaceholderText("Hello world") {
		t.Error("expected false for plain text")
	}
}

func TestValidateSectionContents(t *testing.T) {
	err := ValidateSectionContents([]SectionContent{
		{SectionType: "abstract", Paragraphs: []ContentParagraph{{Text: "摘要"}}},
		{SectionType: "body", Paragraphs: []ContentParagraph{{Text: "正文"}}},
		{SectionType: "references", Paragraphs: []ContentParagraph{{Text: "[1]"}}},
	})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	err = ValidateSectionContents([]SectionContent{
		{SectionType: "body", Paragraphs: []ContentParagraph{{Text: "正文"}}},
	})
	if err == nil {
		t.Error("expected error for missing abstract and references")
	}
}

// ──────────────────────────────────────────────────
// OOXML filler core tests
// ──────────────────────────────────────────────────

func TestOOXMLFillerBasicFill(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate golden template
	templatePath := filepath.Join(tmpDir, "golden.docx")
	cfg := DefaultCQRWSTConfig()
	if err := GenerateGoldenTemplate(templatePath, cfg); err != nil {
		t.Fatalf("generate template: %v", err)
	}

	// Prepare test content
	sections := []SectionContent{
		{SectionType: "cover_title", Paragraphs: []ContentParagraph{
			{Text: "基于深度学习的图像识别研究"},
		}},
		{SectionType: "abstract_title", Paragraphs: []ContentParagraph{
			{Text: "摘  要"},
		}},
		{SectionType: "abstract", Paragraphs: []ContentParagraph{
			{Text: "本文研究了基于深度学习的图像识别技术。通过构建卷积神经网络模型，对图像进行分类识别。"},
			{Text: "实验结果表明，所提出的方法在标准数据集上达到了较高的识别准确率。"},
		}},
		{SectionType: "keywords", Paragraphs: []ContentParagraph{
			{Text: "关键词：深度学习；图像识别；卷积神经网络"},
		}},
		{SectionType: "body", Paragraphs: []ContentParagraph{
			{Text: "第一章 绪论", ParaType: "heading_1"},
			{Text: "随着人工智能技术的快速发展，深度学习在计算机视觉领域取得了显著成果。"},
			{Text: "1.1 研究背景", ParaType: "heading_2"},
			{Text: "图像识别是计算机视觉的核心任务之一，广泛应用于自动驾驶、医疗影像等领域。"},
		}},
		{SectionType: "references_title", Paragraphs: []ContentParagraph{
			{Text: "参考文献"},
		}},
		{SectionType: "references", Paragraphs: []ContentParagraph{
			{Text: "[1] LeCun Y, Bengio Y, Hinton G. Deep learning[J]. Nature, 2015."},
			{Text: "[2] He K, Zhang X, Ren S, et al. Deep residual learning[C]. CVPR, 2016."},
		}},
		{SectionType: "acknowledgements_title", Paragraphs: []ContentParagraph{
			{Text: "致  谢"},
		}},
		{SectionType: "acknowledgements", Paragraphs: []ContentParagraph{
			{Text: "感谢导师在论文写作过程中给予的悉心指导和帮助。"},
		}},
	}

	// Run the filler
	filler := NewOOXMLFiller()
	filler.Debug = true
	outputPath, err := filler.FillTemplate(context.Background(), templatePath, sections, tmpDir, "test_paper")
	if err != nil {
		t.Fatalf("FillTemplate failed: %v", err)
	}

	// Verify output exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("output file not created")
	}

	// Read output and verify content
	outData, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	outReader, err := zip.NewReader(bytes.NewReader(outData), int64(len(outData)))
	if err != nil {
		t.Fatalf("output not valid ZIP: %v", err)
	}

	docXML := readZipEntry(t, outReader, "word/document.xml")

	// Placeholders should be gone
	if strings.Contains(docXML, "{{BODY}}") {
		t.Error("output still contains {{BODY}} placeholder")
	}
	if strings.Contains(docXML, "{{ABSTRACT_CONTENT}}") {
		t.Error("output still contains {{ABSTRACT_CONTENT}} placeholder")
	}

	// Student content should be present
	if !strings.Contains(docXML, "基于深度学习的图像识别研究") {
		t.Error("output missing cover title content")
	}
	if !strings.Contains(docXML, "本文研究了基于深度学习") {
		t.Error("output missing abstract content")
	}
	if !strings.Contains(docXML, "第一章 绪论") {
		t.Error("output missing body heading")
	}
	if !strings.Contains(docXML, "LeCun Y") {
		t.Error("output missing references content")
	}
	if !strings.Contains(docXML, "感谢导师") {
		t.Error("output missing acknowledgements content")
	}

	// Verify formatting is preserved from template
	// Styles.xml should still exist and be unchanged
	stylesXML := readZipEntry(t, outReader, "word/styles.xml")
	if !strings.Contains(stylesXML, `w:styleId="Heading1"`) {
		t.Error("styles.xml missing Heading1 style in output")
	}
	if !strings.Contains(stylesXML, `w:eastAsia="宋体"`) {
		t.Error("styles.xml missing 宋体 font in output")
	}

	// Header and footer should be preserved
	headerXML := readZipEntry(t, outReader, "word/header1.xml")
	if !strings.Contains(headerXML, cfg.HeaderText) {
		t.Error("output header missing university name")
	}

	// Page setup (sectPr) should be preserved
	if !strings.Contains(docXML, "w:pgSz") {
		t.Error("output missing page size settings")
	}
	if !strings.Contains(docXML, "w:pgMar") {
		t.Error("output missing page margin settings")
	}
}

func TestOOXMLFillerEmptySection(t *testing.T) {
	tmpDir := t.TempDir()

	templatePath := filepath.Join(tmpDir, "golden.docx")
	if err := GenerateGoldenTemplate(templatePath, DefaultCQRWSTConfig()); err != nil {
		t.Fatalf("generate template: %v", err)
	}

	// Only provide body content, omit optional sections
	sections := []SectionContent{
		{SectionType: "abstract_title", Paragraphs: []ContentParagraph{{Text: "摘  要"}}},
		{SectionType: "abstract", Paragraphs: []ContentParagraph{{Text: "测试摘要内容"}}},
		{SectionType: "keywords", Paragraphs: []ContentParagraph{{Text: "关键词：测试"}}},
		{SectionType: "body", Paragraphs: []ContentParagraph{{Text: "正文内容测试"}}},
		{SectionType: "references_title", Paragraphs: []ContentParagraph{{Text: "参考文献"}}},
		{SectionType: "references", Paragraphs: []ContentParagraph{{Text: "[1] Test ref."}}},
	}

	filler := NewOOXMLFiller()
	outputPath, err := filler.FillTemplate(context.Background(), templatePath, sections, tmpDir, "test_empty")
	if err != nil {
		t.Fatalf("FillTemplate failed: %v", err)
	}

	outData, _ := os.ReadFile(outputPath)
	outReader, _ := zip.NewReader(bytes.NewReader(outData), int64(len(outData)))
	docXML := readZipEntry(t, outReader, "word/document.xml")

	// Missing sections' placeholders should be removed (not left as-is)
	if strings.Contains(docXML, "{{APPENDIX_CONTENT}}") {
		t.Error("unfilled optional placeholder should be removed, not left in output")
	}
}

func TestOOXMLFillerHeadingStyleOverride(t *testing.T) {
	tmpDir := t.TempDir()

	templatePath := filepath.Join(tmpDir, "golden.docx")
	if err := GenerateGoldenTemplate(templatePath, DefaultCQRWSTConfig()); err != nil {
		t.Fatalf("generate template: %v", err)
	}

	sections := []SectionContent{
		{SectionType: "abstract_title", Paragraphs: []ContentParagraph{{Text: "摘  要"}}},
		{SectionType: "abstract", Paragraphs: []ContentParagraph{{Text: "摘要内容"}}},
		{SectionType: "keywords", Paragraphs: []ContentParagraph{{Text: "关键词：测试"}}},
		{SectionType: "body", Paragraphs: []ContentParagraph{
			{Text: "第一章 绪论", ParaType: "heading_1"},
			{Text: "正文段落", ParaType: "body"},
			{Text: "1.1 研究背景", ParaType: "heading_2"},
			{Text: "更多正文", ParaType: "body"},
		}},
		{SectionType: "references_title", Paragraphs: []ContentParagraph{{Text: "参考文献"}}},
		{SectionType: "references", Paragraphs: []ContentParagraph{{Text: "[1] Test."}}},
	}

	filler := NewOOXMLFiller()
	outputPath, err := filler.FillTemplate(context.Background(), templatePath, sections, tmpDir, "test_heading")
	if err != nil {
		t.Fatalf("FillTemplate failed: %v", err)
	}

	outData, _ := os.ReadFile(outputPath)
	outReader, _ := zip.NewReader(bytes.NewReader(outData), int64(len(outData)))
	docXML := readZipEntry(t, outReader, "word/document.xml")

	// Heading paragraphs should have Heading1/Heading2 style applied
	if !strings.Contains(docXML, `w:val="Heading1"`) {
		t.Error("heading_1 paragraph should have Heading1 style override")
	}
	if !strings.Contains(docXML, `w:val="Heading2"`) {
		t.Error("heading_2 paragraph should have Heading2 style override")
	}
}

// ──────────────────────────────────────────────────
// Classification to sections mapping tests
// ──────────────────────────────────────────────────

func TestClassificationToSections(t *testing.T) {
	cls := ClassificationResult{
		Paragraphs: []ClassificationParagraph{
			{Index: 0, Type: "cover", Text: "重庆人文科技学院"},
			{Index: 1, Type: "abstract_title", Text: "摘  要"},
			{Index: 2, Type: "abstract", Text: "本文研究..."},
			{Index: 3, Type: "keywords", Text: "关键词：..."},
			{Index: 4, Type: "heading_1", Text: "第一章 绪论"},
			{Index: 5, Type: "body", Text: "正文内容"},
			{Index: 6, Type: "heading_2", Text: "1.1 背景"},
			{Index: 7, Type: "body", Text: "更多内容"},
			{Index: 8, Type: "references_title", Text: "参考文献"},
			{Index: 9, Type: "references", Text: "[1] ..."},
			{Index: 10, Type: "acknowledgements_title", Text: "致谢"},
			{Index: 11, Type: "acknowledgements", Text: "感谢..."},
		},
	}

	sections := classificationToSections(cls)

	// Check that body paragraphs are grouped together
	sectionMap := make(map[string]*SectionContent)
	for i := range sections {
		sectionMap[sections[i].SectionType] = &sections[i]
	}

	body, ok := sectionMap["body"]
	if !ok {
		t.Fatal("missing 'body' section")
	}
	if len(body.Paragraphs) != 4 {
		t.Errorf("body should have 4 paragraphs (2 headings + 2 body), got %d", len(body.Paragraphs))
	}

	// Headings should preserve their ParaType
	if body.Paragraphs[0].ParaType != "heading_1" {
		t.Errorf("first body para should be heading_1, got %s", body.Paragraphs[0].ParaType)
	}

	refs, ok := sectionMap["references"]
	if !ok {
		t.Fatal("missing 'references' section")
	}
	if len(refs.Paragraphs) != 1 {
		t.Errorf("references should have 1 paragraph, got %d", len(refs.Paragraphs))
	}
}

func TestClassificationToSectionsPreservesComplexContent(t *testing.T) {
	cls := ClassificationResult{
		Paragraphs: []ClassificationParagraph{
			{Index: 0, Type: "heading_1", Text: "1 绪论"},
			{
				Index:             1,
				Type:              "body",
				Text:              "表格单元",
				Kind:              "table",
				SourceXML:         `<w:tbl><w:tr><w:tc><w:p><w:r><w:t>表格单元</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`,
				HasComplexContent: true,
			},
		},
	}

	sections := classificationToSections(cls)
	var body *SectionContent
	for i := range sections {
		if sections[i].SectionType == "body" {
			body = &sections[i]
			break
		}
	}
	if body == nil {
		t.Fatalf("expected body section, got %#v", sections)
	}
	if len(body.Paragraphs) != 2 {
		t.Fatalf("body paragraph count = %d, want 2", len(body.Paragraphs))
	}
	if !body.Paragraphs[1].HasComplexContent {
		t.Fatalf("expected second body item to keep complex-content flag")
	}
	if body.Paragraphs[1].SourceXML == "" {
		t.Fatalf("expected second body item to keep SourceXML")
	}
}

// ──────────────────────────────────────────────────
// XML parsing helper tests
// ──────────────────────────────────────────────────

func TestExtractPlainText(t *testing.T) {
	xml := `<w:p xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
		<w:r><w:t>Hello </w:t></w:r>
		<w:r><w:t>World</w:t></w:r>
	</w:p>`
	result := extractPlainText([]byte(xml))
	if result != "Hello World" {
		t.Errorf("extractPlainText = %q, want %q", result, "Hello World")
	}
}

func TestExtractElement(t *testing.T) {
	xml := `<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>text</w:t></w:r></w:p>`
	pPr := extractElement([]byte(xml), "w:pPr")
	if pPr == nil {
		t.Fatal("extractElement returned nil for w:pPr")
	}
	if !strings.Contains(string(pPr), "center") {
		t.Error("extracted pPr should contain alignment")
	}
}

func TestExtractFirstRunProperties(t *testing.T) {
	xml := `<w:p><w:r><w:rPr><w:b/><w:sz w:val="24"/></w:rPr><w:t>text</w:t></w:r></w:p>`
	rPr := extractFirstRunProperties([]byte(xml))
	if rPr == nil {
		t.Fatal("extractFirstRunProperties returned nil")
	}
	if !strings.Contains(string(rPr), "w:b") {
		t.Error("rPr should contain bold")
	}
	if !strings.Contains(string(rPr), `w:val="24"`) {
		t.Error("rPr should contain font size")
	}
}

func TestEscapeXMLText(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"a < b", "a &lt; b"},
		{"a & b", "a &amp; b"},
		{`a "b" c`, "a &quot;b&quot; c"},
	}
	for _, tt := range tests {
		got := escapeXMLText(tt.input)
		if got != tt.want {
			t.Errorf("escapeXMLText(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ──────────────────────────────────────────────────
// Integration: end-to-end with TemplateFiller
// ──────────────────────────────────────────────────

func TestTemplateFiller_FillWithGoEngine(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate golden template
	templatePath := filepath.Join(tmpDir, "golden.docx")
	if err := GenerateGoldenTemplate(templatePath, DefaultCQRWSTConfig()); err != nil {
		t.Fatalf("generate template: %v", err)
	}

	// Create a minimal student .docx for input
	studentPath := filepath.Join(tmpDir, "student.docx")
	createMinimalDocx(t, studentPath, "学生论文内容")

	cls := ClassificationResult{
		Paragraphs: []ClassificationParagraph{
			{Index: 0, Type: "abstract_title", Text: "摘  要"},
			{Index: 1, Type: "abstract", Text: "这是摘要内容"},
			{Index: 2, Type: "keywords", Text: "关键词：测试"},
			{Index: 3, Type: "heading_1", Text: "第一章"},
			{Index: 4, Type: "body", Text: "正文"},
			{Index: 5, Type: "references_title", Text: "参考文献"},
			{Index: 6, Type: "references", Text: "[1] Test"},
		},
	}

	tf := &TemplateFiller{UsePythonFallback: false}
	outputPath, err := tf.Fill(context.Background(), studentPath, templatePath, cls, tmpDir)
	if err != nil {
		t.Fatalf("Fill failed: %v", err)
	}

	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("output file not created")
	}

	// Verify output is valid OOXML
	outData, _ := os.ReadFile(outputPath)
	_, err = zip.NewReader(bytes.NewReader(outData), int64(len(outData)))
	if err != nil {
		t.Fatalf("output is not valid ZIP: %v", err)
	}
}

// ──────────────────────────────────────────────────
// Output validity: ensure output is a well-formed OOXML document
// ──────────────────────────────────────────────────

func TestOutputIsWellFormedXML(t *testing.T) {
	tmpDir := t.TempDir()

	templatePath := filepath.Join(tmpDir, "golden.docx")
	if err := GenerateGoldenTemplate(templatePath, DefaultCQRWSTConfig()); err != nil {
		t.Fatalf("generate template: %v", err)
	}

	sections := []SectionContent{
		{SectionType: "abstract_title", Paragraphs: []ContentParagraph{{Text: "摘  要"}}},
		{SectionType: "abstract", Paragraphs: []ContentParagraph{
			{Text: "含特殊字符的摘要：a < b & c > d \"quote\""},
		}},
		{SectionType: "keywords", Paragraphs: []ContentParagraph{{Text: "关键词：测试"}}},
		{SectionType: "body", Paragraphs: []ContentParagraph{{Text: "正文"}}},
		{SectionType: "references_title", Paragraphs: []ContentParagraph{{Text: "参考文献"}}},
		{SectionType: "references", Paragraphs: []ContentParagraph{{Text: "[1] T."}}},
	}

	filler := NewOOXMLFiller()
	outputPath, err := filler.FillTemplate(context.Background(), templatePath, sections, tmpDir, "test_xml")
	if err != nil {
		t.Fatalf("FillTemplate failed: %v", err)
	}

	outData, _ := os.ReadFile(outputPath)
	outReader, _ := zip.NewReader(bytes.NewReader(outData), int64(len(outData)))
	docXML := readZipEntry(t, outReader, "word/document.xml")

	// Parse the output XML to verify well-formedness
	decoder := xml.NewDecoder(strings.NewReader(docXML))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("output document.xml is not well-formed XML: %v", err)
		}
	}
}

// ──────────────────────────────────────────────────
// helpers
// ──────────────────────────────────────────────────

func readZipEntry(t *testing.T, r *zip.Reader, name string) string {
	t.Helper()
	for _, f := range r.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			return string(data)
		}
	}
	t.Fatalf("ZIP entry not found: %s", name)
	return ""
}

func createMinimalDocx(t *testing.T, path string, text string) {
	t.Helper()
	cfg := DefaultCQRWSTConfig()
	cfg.HeaderText = "Test"

	// Use the golden template generator to create a minimal valid docx
	if err := GenerateGoldenTemplate(path, cfg); err != nil {
		t.Fatalf("create minimal docx: %v", err)
	}
}
