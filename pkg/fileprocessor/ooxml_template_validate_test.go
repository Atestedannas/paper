package fileprocessor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateBaseFormatterFormatAllowsBodyFragmentLongerThanTemplateRange(t *testing.T) {
	tmpDir := t.TempDir()

	userPath := writeTinyDocxFixture(t, tmpDir, "user.docx", buildTask5DocParts(task5DocFixture{
		coverTitleLines: []string{"用户封面标题"},
		coverInfoRows:   []string{"用户学院", "用户专业", "2026年3月1日"},
		bodyParagraphs:  []string{"用户正文一", "用户正文二"},
		referenceItems:  []string{"用户参考文献"},
		ackParagraphs:   []string{"用户致谢"},
	}))
	templatePath := writeTinyDocxFixture(t, tmpDir, "template.docx", buildTask5DocParts(task5DocFixture{
		coverTitleLines: []string{"模板封面标题"},
		coverInfoRows:   []string{"模板学院", "模板专业", "2026年3月1日"},
		bodyParagraphs:  []string{"模板正文一"},
		referenceItems:  []string{"模板参考文献"},
		ackParagraphs:   []string{"模板致谢"},
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
	for _, want := range []string{"用户正文一", "用户正文二"} {
		if !strings.Contains(outputXML, want) {
			t.Fatalf("expected output to contain %q, got %s", want, outputXML)
		}
	}
}

func TestTemplateBaseFormatterFormatSupportsUserPayloadTableInBodyRange(t *testing.T) {
	tmpDir := t.TempDir()

	userParts := buildTask5DocParts(task5DocFixture{
		coverTitleLines: []string{"用户封面标题"},
		coverInfoRows:   []string{"用户学院", "用户专业", "2026年3月1日"},
		bodyParagraphs:  []string{"用户正文一"},
		referenceItems:  []string{"用户参考文献"},
		ackParagraphs:   []string{"用户致谢"},
	})
	userParts["word/document.xml"] = injectXMLBeforeParagraph(userParts["word/document.xml"], "用户正文一", `<w:tbl><w:tr><w:tc><w:p><w:r><w:t>UserRangeTable</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`)

	userPath := writeTinyDocxFixture(t, tmpDir, "user.docx", userParts)
	templatePath := writeTinyDocxFixture(t, tmpDir, "template.docx", buildTask5DocParts(task5DocFixture{
		coverTitleLines: []string{"模板封面标题"},
		coverInfoRows:   []string{"模板学院", "模板专业", "2026年3月1日"},
		bodyParagraphs:  []string{"模板正文一"},
		referenceItems:  []string{"模板参考文献"},
		ackParagraphs:   []string{"模板致谢"},
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
	if !strings.Contains(outputXML, "UserRangeTable") {
		t.Fatalf("expected output to contain transplanted body table, got %s", outputXML)
	}
	if !strings.Contains(outputXML, "用户正文一") {
		t.Fatalf("expected output to contain user body paragraph, got %s", outputXML)
	}
}

func TestValidateTransplantedTemplateAllowsTextOnlyChanges(t *testing.T) {
	templatePkg := mustOpenTask5Package(t, buildTask5DocParts(task5DocFixture{
		coverTitleLines: []string{"模板封面标题"},
		coverInfoRows:   []string{"模板学院", "模板专业", "2026年3月1日"},
		bodyParagraphs:  []string{"模板正文一"},
		referenceItems:  []string{"模板参考文献"},
		ackParagraphs:   []string{"模板致谢"},
	}))
	outputPkg := mustOpenTask5Package(t, buildTask5DocParts(task5DocFixture{
		coverTitleLines: []string{"用户封面标题"},
		coverInfoRows:   []string{"用户学院", "用户专业", "2026年3月1日"},
		bodyParagraphs:  []string{"用户正文一"},
		referenceItems:  []string{"用户参考文献"},
		ackParagraphs:   []string{"用户致谢"},
	}))

	templateBlocks, err := findTemplateBlocks(templatePkg)
	if err != nil {
		t.Fatalf("findTemplateBlocks() error = %v", err)
	}
	if err := validateTransplantedTemplate(templatePkg, outputPkg, templateBlocks); err != nil {
		t.Fatalf("validateTransplantedTemplate() error = %v", err)
	}
}

type task5DocFixture struct {
	coverTitleLines    []string
	coverTitleTableXML string
	coverInfoRows      []string
	bodyParagraphs     []string
	referenceItems     []string
	ackParagraphs      []string
	omitAckTitle       bool
}

func buildTask5DocParts(fixture task5DocFixture) map[string]string {
	return map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   buildTask5DocumentXML(fixture),
	}
}

func buildTask5DocumentXML(fixture task5DocFixture) string {
	var body strings.Builder
	body.WriteString("<w:body>")
	if fixture.coverTitleTableXML != "" {
		body.WriteString(fixture.coverTitleTableXML)
	} else {
		body.WriteString(buildTask5TableXML(fixture.coverTitleLines))
	}
	body.WriteString(buildTask5TableXML(fixture.coverInfoRows))
	body.WriteString(buildTask5ParagraphXML("摘  要"))
	body.WriteString(buildTask5ParagraphXML("Abstract"))
	body.WriteString(buildTask5ParagraphXML("目  录"))
	for _, para := range fixture.bodyParagraphs {
		body.WriteString(buildTask5ParagraphXML(para))
	}
	body.WriteString(buildTask5ParagraphXML("参考文献"))
	for _, para := range fixture.referenceItems {
		body.WriteString(buildTask5ParagraphXML(para))
	}
	if !fixture.omitAckTitle {
		body.WriteString(buildTask5ParagraphXML("致谢"))
	}
	for _, para := range fixture.ackParagraphs {
		body.WriteString(buildTask5ParagraphXML(para))
	}
	body.WriteString("<w:sectPr/>")
	body.WriteString("</w:body>")
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` + body.String() + `</w:document>`
}

func buildTask5TableXML(rows []string) string {
	var b strings.Builder
	b.WriteString("<w:tbl>")
	for _, row := range rows {
		b.WriteString("<w:tr><w:tc>")
		b.WriteString(buildTask5ParagraphXML(row))
		b.WriteString("</w:tc></w:tr>")
	}
	b.WriteString("</w:tbl>")
	return b.String()
}

func buildTask5ParagraphXML(text string) string {
	return `<w:p><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

func injectXMLBeforeParagraph(docXML, anchorText, insertXML string) string {
	anchor := buildTask5ParagraphXML(anchorText)
	return strings.Replace(docXML, anchor, insertXML+anchor, 1)
}

func mustOpenTask5Package(t *testing.T, parts map[string]string) *docxPackage {
	t.Helper()

	tmpDir := t.TempDir()
	path := writeTinyDocxFixture(t, tmpDir, "fixture.docx", parts)
	pkg, err := openDocxPackage(path)
	if err != nil {
		t.Fatalf("openDocxPackage() error = %v", err)
	}
	return pkg
}
