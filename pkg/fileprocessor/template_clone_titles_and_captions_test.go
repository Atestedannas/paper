package fileprocessor

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

func TestTemplateBaseFormatterReplacesInstructionReferenceAndAckTitlesWithUserTitles(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	copyFile(t, repoFixturePath(t, "uploads", "user.docx"), userPath)
	copyFile(t, repoFixturePath(t, "uploads", "template.docx"), templatePath)

	if _, err := NewTemplateBaseFormatter().Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	paragraphs := extractDocxParagraphs(extractDocxBodyXML(docXML))

	hasReferenceTitle := false
	hasAckTitle := false
	for _, paragraph := range paragraphs {
		text := strings.TrimSpace(normalizeVisibleText(extractDocxText(paragraph.xml)))
		compacted := stripAllSpaces(normalizeVisibleText(text))
		switch compacted {
		case "\u53c2\u8003\u6587\u732e":
			hasReferenceTitle = true
		case "\u81f4\u8c22":
			hasAckTitle = true
		}
		if strings.HasPrefix(text, "\u53c2\u8003\u6587\u732e\u6807\u9898\uff1a") {
			t.Fatalf("expected real output to replace template references instruction title, got %q", text)
		}
		if strings.HasPrefix(text, "\u81f4\u8c22\u6807\u9898\uff1a") {
			t.Fatalf("expected real output to replace template acknowledgement instruction title, got %q", text)
		}
	}

	if !hasReferenceTitle {
		t.Fatalf("expected real output to contain exact references title paragraph")
	}
	if !hasAckTitle {
		t.Fatalf("expected real output to contain exact acknowledgement title paragraph")
	}
}

func TestTemplateBaseFormatterReplacesInstructionalFrontMatterWithUserFrontMatter(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	copyFile(t, repoFixturePath(t, "uploads", "user.docx"), userPath)
	copyFile(t, repoFixturePath(t, "uploads", "template.docx"), templatePath)

	if _, err := NewTemplateBaseFormatter().Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	visibleTexts := make([]string, 0, 128)
	for _, paragraph := range extractDocxParagraphs(extractDocxBodyXML(docXML)) {
		text := strings.TrimSpace(normalizeVisibleText(extractDocxText(paragraph.xml)))
		if text != "" {
			visibleTexts = append(visibleTexts, text)
		}
	}
	visibleJoined := strings.Join(visibleTexts, "\n")
	for _, forbidden := range []string{
		"封面格式不要调整",
		"202X年",
		"正标题：",
		"摘要：xxxxxxxx",
		"Abstract: XXXXXXXXX",
		"目录内容：",
	} {
		if strings.Contains(visibleJoined, forbidden) {
			t.Fatalf("did not expect instructional template text %q in output", forbidden)
		}
	}
	for _, required := range []string{
		"社区2型糖尿病患者疾病知识",
		"认知现状及影响因素分析",
		"2026年",
		"摘要：目的",
		"关键词：社区二型糖尿病；认知水平；影响因素",
	} {
		if !strings.Contains(visibleJoined, required) {
			t.Fatalf("expected user front matter text %q in output", required)
		}
	}
}

func TestTemplateBaseFormatterMapsFrontMatterFieldsIntoTemplateSlots(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	copyFile(t, repoFixturePath(t, "uploads", "user.docx"), userPath)
	copyFile(t, repoFixturePath(t, "uploads", "template.docx"), templatePath)

	if _, err := NewTemplateBaseFormatter().Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	texts := visibleDocxParagraphTexts(readDocxEntry(t, outputPath, "word/document.xml"))
	coverTitleIndex := findVisibleTextIndex(texts, "社区2型糖尿病患者疾病知识")
	collegeIndex := findVisibleTextIndex(texts, "护理学院")
	abstractIndex := findVisibleTextIndex(texts, "摘要：目的")
	tocIndex := findVisibleTextIndex(texts, "目      录")

	if coverTitleIndex == -1 {
		t.Fatalf("expected output to keep cover title text before abstract mapping")
	}
	if collegeIndex == -1 {
		t.Fatalf("expected output to keep cover info table text before abstract mapping")
	}
	if abstractIndex == -1 {
		t.Fatalf("expected output to contain user abstract paragraph")
	}
	if tocIndex == -1 {
		t.Fatalf("expected output to contain toc title paragraph")
	}
	if coverTitleIndex > abstractIndex {
		t.Fatalf("expected cover title to stay before abstract, got titleIndex=%d abstractIndex=%d texts=%q", coverTitleIndex, abstractIndex, texts[:testMinInt(len(texts), 18)])
	}
	if collegeIndex > abstractIndex {
		t.Fatalf("expected cover info table to stay before abstract, got collegeIndex=%d abstractIndex=%d texts=%q", collegeIndex, abstractIndex, texts[:testMinInt(len(texts), 18)])
	}
	if tocIndex < abstractIndex {
		t.Fatalf("expected toc title to remain after abstract front matter, got tocIndex=%d abstractIndex=%d texts=%q", tocIndex, abstractIndex, texts[:testMinInt(len(texts), 18)])
	}
}

func TestTemplateBaseFormatterPreservesTemplateMediaAssets(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	copyFile(t, repoFixturePath(t, "uploads", "user.docx"), userPath)
	copyFile(t, repoFixturePath(t, "uploads", "template.docx"), templatePath)

	if _, err := NewTemplateBaseFormatter().Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	templatePkg, err := openDocxPackage(templatePath)
	if err != nil {
		t.Fatalf("openDocxPackage(template) error = %v", err)
	}
	outputPkg, err := openDocxPackage(outputPath)
	if err != nil {
		t.Fatalf("openDocxPackage(output) error = %v", err)
	}

	templateMedia := docxEntryNamesWithPrefix(templatePkg, "word/media/")
	outputMedia := docxEntryNamesWithPrefix(outputPkg, "word/media/")
	if len(templateMedia) == 0 {
		t.Fatalf("expected template to contain media assets")
	}
	if strings.Join(templateMedia, "\n") != strings.Join(outputMedia, "\n") {
		t.Fatalf("expected output to preserve template media assets\nwant=%q\ngot=%q", templateMedia, outputMedia)
	}
}

func TestBuildDocxTextElementPreservesInternalRepeatedSpaces(t *testing.T) {
	got := buildDocxTextElement("目      录")
	if !strings.Contains(got, `xml:space="preserve"`) {
		t.Fatalf("expected repeated internal spaces to keep xml:space preserve, got %s", got)
	}
}

func TestTemplateBaseFormatterPreservesRawSpacingForDateAndTOCTitle(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	copyFile(t, repoFixturePath(t, "uploads", "user.docx"), userPath)
	copyFile(t, repoFixturePath(t, "uploads", "template.docx"), templatePath)

	if _, err := NewTemplateBaseFormatter().Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	rawTexts := rawDocxParagraphTexts(docXML)
	if findVisibleTextIndex(rawTexts, "2026年 3  月") == -1 {
		t.Fatalf("expected output to preserve raw date spacing, got %q", rawTexts[:testMinInt(len(rawTexts), 18)])
	}
	if findVisibleTextIndex(rawTexts, "目      录") == -1 {
		t.Fatalf("expected output to preserve raw toc title spacing, got %q", rawTexts[:testMinInt(len(rawTexts), 24)])
	}
}

func TestTemplateBaseFormatterFormatsFigureAndTableCaptionsWithTemplateCaptionRules(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTemplateCloneCaptionDocx(t, templatePath, []captionDocParagraph{
		{text: "Template body paragraph used to derive the main text typography for cloning.", fontFamily: "SimSun", fontSize: 12, centered: false},
		{text: "\u56fe1.1 Template Figure Caption", fontFamily: "SimSun", fontSize: 11, centered: true},
		{text: "\u88681.1 Template Table Caption", fontFamily: "SimSun", fontSize: 11, centered: true},
	})
	writeTemplateCloneCaptionDocx(t, userPath, []captionDocParagraph{
		{text: "User body paragraph should be normalized separately from captions in the template clone path.", fontFamily: "Calibri", fontSize: 14, bold: true, centered: false},
		{text: "\u56fe3.1 User Figure Caption", fontFamily: "Calibri", fontSize: 14, bold: true, centered: true},
		{text: "\u88683.1 User Table Caption", fontFamily: "Calibri", fontSize: 14, bold: true, centered: true},
	})

	templateDoc, err := document.Open(templatePath)
	if err != nil {
		t.Fatalf("document.Open(%q) error = %v", templatePath, err)
	}
	rules := extractStrictTemplateBlockRules(templateDoc, NewEnhancedProcessor())
	if spec, ok := rules.Paragraph[strictBlockFigureCaption]; !ok || spec.IsEmpty() {
		var texts []string
		processor := NewEnhancedProcessor()
		for _, ref := range strictMainStoryParagraphs(templateDoc) {
			texts = append(texts, strings.TrimSpace(processor.extractParagraphText(ref.Para)))
		}
		templateDoc.Close()
		t.Fatalf("expected template figure caption rule, got %#v texts=%q", spec, texts)
	}
	if spec, ok := rules.Paragraph[strictBlockTableCaption]; !ok || spec.IsEmpty() {
		templateDoc.Close()
		t.Fatalf("expected template table caption rule, got %#v", spec)
	}
	templateDoc.Close()

	if _, err := NewTemplateBaseFormatter().Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")

	figureCaption := extractFirstParagraphContaining(docXML, "\u56fe3.1 User Figure Caption")
	if figureCaption == "" {
		t.Fatalf("expected figure caption paragraph in output")
	}
	if !strings.Contains(figureCaption, `w:eastAsia="宋体"`) || !strings.Contains(figureCaption, `w:sz w:val="22"`) {
		t.Fatalf("expected figure caption to inherit template caption typography, got %s", figureCaption)
	}
	if strings.Contains(figureCaption, `w:sz w:val="28"`) {
		t.Fatalf("did not expect figure caption to keep user font size, got %s", figureCaption)
	}

	tableCaption := extractFirstParagraphContaining(docXML, "\u88683.1 User Table Caption")
	if tableCaption == "" {
		t.Fatalf("expected table caption paragraph in output")
	}
	if !strings.Contains(tableCaption, `w:eastAsia="宋体"`) || !strings.Contains(tableCaption, `w:sz w:val="22"`) {
		t.Fatalf("expected table caption to inherit template caption typography, got %s", tableCaption)
	}
	if strings.Contains(tableCaption, `w:sz w:val="28"`) {
		t.Fatalf("did not expect table caption to keep user font size, got %s", tableCaption)
	}
}

type captionDocParagraph struct {
	text       string
	fontFamily string
	fontSize   int
	bold       bool
	centered   bool
}

func writeTemplateCloneCaptionDocx(t *testing.T, path string, body []captionDocParagraph) {
	t.Helper()

	writeTestDocx(t, path, func(doc *document.Document) {
		coverTitleTable := doc.AddTable()
		coverTitleRow := coverTitleTable.AddRow()
		coverTitleRow.AddCell().AddParagraph().AddRun().AddText("Title")
		coverTitleRow.AddCell().AddParagraph().AddRun().AddText("Template Cover Title")

		coverInfoTable := doc.AddTable()
		coverInfoRow := coverInfoTable.AddRow()
		coverInfoRow.AddCell().AddParagraph().AddRun().AddText("College")
		coverInfoRow.AddCell().AddParagraph().AddRun().AddText("Nursing")

		doc.AddParagraph().AddRun().AddText("\u6458\u8981")
		doc.AddParagraph().AddRun().AddText("Abstract")
		doc.AddParagraph().AddRun().AddText("\u76ee\u5f55")

		for _, item := range body {
			para := doc.AddParagraph()
			if item.centered {
				para.Properties().SetAlignment(wml.ST_JcCenter)
			}
			run := para.AddRun()
			run.AddText(item.text)
			run.Properties().SetFontFamily(item.fontFamily)
			run.Properties().SetSize(measurement.Distance(item.fontSize))
			run.Properties().SetBold(item.bold)
		}

		doc.AddParagraph().AddRun().AddText("\u53c2\u8003\u6587\u732e")
		doc.AddParagraph().AddRun().AddText("[1] Example reference")
		doc.AddParagraph().AddRun().AddText("\u81f4\u8c22")
		doc.AddParagraph().AddRun().AddText("Acknowledgement body")
	})
}

func visibleDocxParagraphTexts(docXML string) []string {
	texts := make([]string, 0, 128)
	for _, paragraph := range extractDocxParagraphs(extractDocxBodyXML(docXML)) {
		text := strings.TrimSpace(normalizeVisibleText(extractDocxText(paragraph.xml)))
		if text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

func rawDocxParagraphTexts(docXML string) []string {
	texts := make([]string, 0, 128)
	for _, paragraph := range extractDocxParagraphs(extractDocxBodyXML(docXML)) {
		text := strings.TrimSpace(extractDocxText(paragraph.xml))
		if text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

func findVisibleTextIndex(texts []string, needle string) int {
	normalizedNeedle := stripAllSpaces(needle)
	for idx, text := range texts {
		if strings.Contains(text, needle) || strings.Contains(stripAllSpaces(text), normalizedNeedle) {
			return idx
		}
	}
	return -1
}

func docxEntryNamesWithPrefix(pkg *docxPackage, prefix string) []string {
	if pkg == nil {
		return nil
	}
	names := make([]string, 0, len(pkg.entries))
	for name := range pkg.entries {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func testMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
