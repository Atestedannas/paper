package fileprocessor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

type task4Fixture struct {
	coverTitle      string
	coverInfoRows   []string
	introParagraphs []string
	bodyParagraphs  []string
	referenceItems  []string
	ackParagraphs   []string
	omitAckTitle    bool
	headerXML       string
	footerXML       string
}

func TestFindTemplateBlocksLocatesKnownTemplateBlocks(t *testing.T) {
	pkg := mustOpenFixturePackage(t, task4Fixture{
		coverTitle:      "封面标题",
		coverInfoRows:   []string{"学院", "软件工程", "2026年3月1日"},
		introParagraphs: []string{"前言"},
		bodyParagraphs:  []string{"这不是参考文献标题", "这是致谢说明"},
		referenceItems:  []string{"参考文献条目"},
		ackParagraphs:   []string{"致谢内容"},
	})

	blocks, err := findTemplateBlocks(pkg)
	if err != nil {
		t.Fatalf("findTemplateBlocks() error = %v", err)
	}

	checks := map[string]struct {
		index     int
		blockType string
		text      string
	}{
		"cover_title_table": {index: 0, blockType: "table", text: "封面标题"},
		"cover_info_table":  {index: 1, blockType: "table", text: "学院软件工程2026年3月1日"},
		"abstract_cn_title": {index: 5, blockType: "paragraph", text: "摘  要"},
		"abstract_en_title": {index: 6, blockType: "paragraph", text: "Abstract"},
		"toc_title":         {index: 7, blockType: "paragraph", text: "目  录"},
		"references_title":  {index: 10, blockType: "paragraph", text: "参考文献"},
		"ack_title":         {index: 12, blockType: "paragraph", text: "致谢"},
	}

	for name, want := range checks {
		got, ok := blocks[name]
		if !ok {
			t.Fatalf("missing block %q", name)
		}
		if got.index != want.index {
			t.Fatalf("%s index = %d, want %d", name, got.index, want.index)
		}
		if got.blockType != want.blockType {
			t.Fatalf("%s blockType = %q, want %q", name, got.blockType, want.blockType)
		}
		if got.text != want.text {
			t.Fatalf("%s text = %q, want %q", name, got.text, want.text)
		}
	}
}

func TestFindTemplateBlocksReportsMissingAnchor(t *testing.T) {
	pkg := mustOpenFixturePackage(t, task4Fixture{
		coverTitle:      "封面标题",
		coverInfoRows:   []string{"学院", "软件工程", "2026年3月1日"},
		introParagraphs: []string{"前言"},
		bodyParagraphs:  []string{"正文"},
		referenceItems:  []string{"参考文献条目"},
		ackParagraphs:   nil,
		omitAckTitle:    true,
	})

	_, err := findTemplateBlocks(pkg)
	if err == nil {
		t.Fatalf("expected missing anchor error")
	}
	if !strings.Contains(err.Error(), "ack_title") {
		t.Fatalf("error = %v, want missing ack_title", err)
	}
}

func TestFindTemplateBlocksDoesNotLetEarlierShortChineseHeadingStealAbstractTitle(t *testing.T) {
	pkg := mustOpenFixturePackage(t, task4Fixture{
		coverTitle:      "封面标题",
		coverInfoRows:   []string{"学院", "软件工程", "2026年3月1日"},
		introParagraphs: []string{"前言"},
		bodyParagraphs:  []string{"正文"},
		referenceItems:  []string{"参考文献条目"},
		ackParagraphs:   []string{"致谢内容"},
	})

	blocks, err := findTemplateBlocks(pkg)
	if err != nil {
		t.Fatalf("findTemplateBlocks() error = %v", err)
	}

	if got := blocks["abstract_cn_title"]; got.index != 5 || got.text != "摘  要" {
		t.Fatalf("abstract_cn_title = %+v, want index 5 text %q", got, "摘  要")
	}
}

func TestFindTemplateBlocksSkipsTOCCollisionsForReferencesAndAcknowledgements(t *testing.T) {
	pkg := mustOpenFixturePackage(t, task4Fixture{
		coverTitle:      "封面标题",
		coverInfoRows:   []string{"学院", "软件工程", "2026年3月1日"},
		introParagraphs: []string{"前言"},
		bodyParagraphs:  []string{"参考文献的说明", "致谢说明"},
		referenceItems:  []string{"参考文献条目"},
		ackParagraphs:   []string{"致谢内容"},
	})

	blocks, err := findTemplateBlocks(pkg)
	if err != nil {
		t.Fatalf("findTemplateBlocks() error = %v", err)
	}

	if got := blocks["references_title"]; got.index != 10 || got.text != "参考文献" {
		t.Fatalf("references_title = %+v, want index 10 text %q", got, "参考文献")
	}
	if got := blocks["ack_title"]; got.index != 12 || got.text != "致谢" {
		t.Fatalf("ack_title = %+v, want index 12 text %q", got, "致谢")
	}
}

func TestFindTemplateBlocksRecognizesInstructionStyleRealTemplateAnchors(t *testing.T) {
	templatePkg, err := openDocxPackage(repoFixturePath(t, "uploads", "template.docx"))
	if err != nil {
		t.Fatalf("openDocxPackage(real template) error = %v", err)
	}

	blocks, err := findTemplateBlocks(templatePkg)
	if err != nil {
		t.Fatalf("findTemplateBlocks(real template) error = %v", err)
	}

	checks := map[string]string{
		"abstract_cn_title": "摘要：",
		"abstract_en_title": "Abstract:",
		"toc_title":         "目录内容：",
		"references_title":  "参考文献标题：",
		"ack_title":         "致谢标题：",
	}
	for name, prefix := range checks {
		block, ok := blocks[name]
		if !ok {
			t.Fatalf("missing block %q", name)
		}
		if !strings.HasPrefix(block.text, prefix) {
			t.Fatalf("%s text = %q, want prefix %q", name, block.text, prefix)
		}
	}
}

func TestTemplateBaseFormatterTransplantsCoverTextAndPreservesTemplateTableStructure(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := writeTinyDocxFixture(t, tmpDir, "user.docx", buildTask4Parts(task4Fixture{
		coverTitle:      "用户封面标题",
		coverInfoRows:   []string{"用户学院", "用户专业", "2026年3月1日"},
		introParagraphs: []string{"前言"},
		bodyParagraphs:  []string{"用户正文"},
		referenceItems:  []string{"用户参考文献"},
		ackParagraphs:   []string{"用户致谢"},
	}))
	templatePath := writeTinyDocxFixture(t, tmpDir, "template.docx", buildTask4Parts(task4Fixture{
		coverTitle:      "模板封面标题",
		coverInfoRows:   []string{"模板学院", "模板专业", "2025年12月31日"},
		introParagraphs: []string{"前言"},
		bodyParagraphs:  []string{"模板正文"},
		referenceItems:  []string{"模板参考文献"},
		ackParagraphs:   []string{"模板致谢"},
		headerXML:       `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Template Header</w:t></w:r></w:p></w:hdr>`,
		footerXML:       `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Template Footer</w:t></w:r></w:p></w:ftr>`,
	}))
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	if _, err := NewTemplateBaseFormatter().Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	outputXML := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(outputXML, "用户封面标题") || !strings.Contains(outputXML, "用户学院") || !strings.Contains(outputXML, "2026年3月1日") {
		t.Fatalf("expected output to contain user cover text, got %s", outputXML)
	}
	if strings.Count(outputXML, "2026年3月1日") != 1 {
		t.Fatalf("expected date line to appear once, got %d occurrences in %s", strings.Count(outputXML, "2026年3月1日"), outputXML)
	}

	templateXML := readDocxEntry(t, templatePath, "word/document.xml")
	outputTables := extractDocxElements(extractDocxBodyXML(outputXML), "w:tbl")
	templateTables := extractDocxElements(extractDocxBodyXML(templateXML), "w:tbl")
	if len(outputTables) < 2 || len(templateTables) < 2 {
		t.Fatalf("expected at least two cover tables, got template=%d output=%d", len(templateTables), len(outputTables))
	}
	for i := 0; i < 2; i++ {
		got := neutralizeDocxText(outputTables[i])
		want := neutralizeDocxText(templateTables[i])
		if got != want {
			t.Fatalf("cover table %d skeleton changed\nwant: %s\ngot:  %s", i, want, got)
		}
	}
}

func TestTemplateBaseFormatterTransplantsBodyTextAndLeavesHeaderFooterUntouched(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := writeTinyDocxFixture(t, tmpDir, "user.docx", buildTask4Parts(task4Fixture{
		coverTitle:      "用户封面标题",
		coverInfoRows:   []string{"用户学院", "用户专业", "2026年3月1日"},
		introParagraphs: []string{"前言"},
		bodyParagraphs:  []string{"用户正文一"},
		referenceItems:  []string{"用户参考文献"},
		ackParagraphs:   []string{"用户致谢"},
	}))
	templatePath := writeTinyDocxFixture(t, tmpDir, "template.docx", buildTask4Parts(task4Fixture{
		coverTitle:      "模板封面标题",
		coverInfoRows:   []string{"模板学院", "模板专业", "2025年12月31日"},
		introParagraphs: []string{"前言"},
		bodyParagraphs:  []string{"模板正文一", "模板正文二"},
		referenceItems:  []string{"模板参考文献一", "模板参考文献二"},
		ackParagraphs:   []string{"模板致谢一", "模板致谢二"},
		headerXML:       `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Template Header</w:t></w:r></w:p></w:hdr>`,
		footerXML:       `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Template Footer</w:t></w:r></w:p></w:ftr>`,
	}))
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	if _, err := NewTemplateBaseFormatter().Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	outputXML := readDocxEntry(t, outputPath, "word/document.xml")
	for _, want := range []string{"用户正文一", "用户参考文献", "用户致谢"} {
		if !strings.Contains(outputXML, want) {
			t.Fatalf("expected output to contain %q, got %s", want, outputXML)
		}
	}
	for _, want := range []string{"模板正文一", "模板正文二", "模板参考文献一", "模板参考文献二", "模板致谢一", "模板致谢二"} {
		if strings.Contains(outputXML, want) {
			t.Fatalf("did not expect template text %q to remain, got %s", want, outputXML)
		}
	}

	templateEntries := readTinyDocxEntries(t, templatePath)
	outputEntries := readTinyDocxEntries(t, outputPath)
	if string(outputEntries["word/header1.xml"]) != string(templateEntries["word/header1.xml"]) {
		t.Fatalf("header changed\nwant: %s\ngot:  %s", templateEntries["word/header1.xml"], outputEntries["word/header1.xml"])
	}
	if string(outputEntries["word/footer1.xml"]) != string(templateEntries["word/footer1.xml"]) {
		t.Fatalf("footer changed\nwant: %s\ngot:  %s", templateEntries["word/footer1.xml"], outputEntries["word/footer1.xml"])
	}
}

func TestDocxPackageCloneAndWriteToRoundTripsEntries(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := writeTinyDocxFixture(t, tmpDir, "template.docx", map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Template</w:t></w:r></w:p></w:body></w:document>`,
		"word/header1.xml":    `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Header</w:t></w:r></w:p></w:hdr>`,
	})

	pkg, err := openDocxPackage(templatePath)
	if err != nil {
		t.Fatalf("openDocxPackage() error = %v", err)
	}
	clone := pkg.Clone()
	if clone == nil {
		t.Fatalf("Clone() returned nil")
	}

	outputPath := filepath.Join(tmpDir, "clone.docx")
	if err := clone.WriteTo(outputPath); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}

	if !tinyDocxEntriesEqual(readTinyDocxEntries(t, templatePath), readTinyDocxEntries(t, outputPath)) {
		t.Fatalf("expected cloned package to round-trip all entries")
	}
}

func mustOpenFixturePackage(t *testing.T, fixture task4Fixture) *docxPackage {
	t.Helper()

	tmpDir := t.TempDir()
	path := writeTinyDocxFixture(t, tmpDir, "fixture.docx", buildTask4Parts(fixture))
	pkg, err := openDocxPackage(path)
	if err != nil {
		t.Fatalf("openDocxPackage() error = %v", err)
	}
	return pkg
}

func buildTask4Parts(fixture task4Fixture) map[string]string {
	bodyXML := buildTask4BodyXML(fixture)
	parts := map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   buildTask4DocumentXML(bodyXML),
	}
	if fixture.headerXML != "" {
		parts["word/header1.xml"] = fixture.headerXML
	}
	if fixture.footerXML != "" {
		parts["word/footer1.xml"] = fixture.footerXML
	}
	return parts
}

func buildTask4DocumentXML(bodyXML string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` + bodyXML + `</w:document>`
}

func buildTask4BodyXML(fixture task4Fixture) string {
	var b strings.Builder
	b.WriteString("<w:body>")
	b.WriteString(buildTask4TableXML([]string{fixture.coverTitle}))
	b.WriteString(buildTask4TableXML(fixture.coverInfoRows))
	for _, para := range fixture.introParagraphs {
		b.WriteString(buildTask4ParagraphXML(para))
	}
	b.WriteString(buildTask4ParagraphXML("摘  要"))
	b.WriteString(buildTask4ParagraphXML("Abstract"))
	b.WriteString(buildTask4ParagraphXML("目  录"))
	for _, para := range fixture.bodyParagraphs {
		b.WriteString(buildTask4ParagraphXML(para))
	}
	b.WriteString(buildTask4ParagraphXML("参考文献"))
	for _, para := range fixture.referenceItems {
		b.WriteString(buildTask4ParagraphXML(para))
	}
	if !fixture.omitAckTitle {
		b.WriteString(buildTask4ParagraphXML("致谢"))
	}
	for _, para := range fixture.ackParagraphs {
		b.WriteString(buildTask4ParagraphXML(para))
	}
	b.WriteString("<w:sectPr/>")
	b.WriteString("</w:body>")
	return b.String()
}

func buildTask4TableXML(rows []string) string {
	var b strings.Builder
	b.WriteString("<w:tbl>")
	for _, row := range rows {
		b.WriteString("<w:tr><w:tc>")
		b.WriteString(buildTask4ParagraphXML(row))
		b.WriteString("</w:tc></w:tr>")
	}
	b.WriteString("</w:tbl>")
	return b.String()
}

func buildTask4ParagraphXML(text string) string {
	return "<w:p><w:r><w:t>" + text + "</w:t></w:r></w:p>"
}
