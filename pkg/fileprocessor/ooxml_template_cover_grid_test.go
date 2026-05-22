package fileprocessor

import (
	"context"
	"html"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateBaseFormatterFormatRejectsIncompatibleCoverTableGrid_New(t *testing.T) {
	tmpDir := t.TempDir()

	userPath := writeTinyDocxFixture(t, tmpDir, "user.docx", buildCoverGridDocParts(coverGridDocFixture{
		coverTitleTableXML: buildCoverGridTableXML([][2]string{{"题目", "用户标题"}}, nil),
		coverInfoTableXML:  buildSingleColumnTableXML([]string{"用户学院", "用户专业", "2026年3月1日"}),
		bodyParagraphs:     []string{"用户正文"},
		referenceItems:     []string{"用户参考文献"},
		ackParagraphs:      []string{"用户致谢"},
	}))
	templatePath := writeTinyDocxFixture(t, tmpDir, "template.docx", buildCoverGridDocParts(coverGridDocFixture{
		coverTitleTableXML: buildSingleColumnTableXML([]string{"模板封面标题"}),
		coverInfoTableXML:  buildSingleColumnTableXML([]string{"模板学院", "模板专业", "2026年3月1日"}),
		bodyParagraphs:     []string{"模板正文"},
		referenceItems:     []string{"模板参考文献"},
		ackParagraphs:      []string{"模板致谢"},
	}))
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	_, err := NewTemplateBaseFormatter().Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	})
	if err == nil {
		t.Fatalf("expected cover table grid mismatch error")
	}
	if !strings.Contains(err.Error(), "cover_title_table") {
		t.Fatalf("error = %v, want cover_title_table mismatch", err)
	}
}

func TestTemplateBaseFormatterFormatSupportsTemplateCoverValueCellsWithoutTextNodes_New(t *testing.T) {
	tmpDir := t.TempDir()

	userPath := writeTinyDocxFixture(t, tmpDir, "user.docx", buildCoverGridDocParts(coverGridDocFixture{
		coverTitleTableXML: buildCoverGridTableXML([][2]string{
			{"题目", "社区2型糖尿病患者疾病知识"},
			{"", "认知现状及影响因素分析"},
		}, nil),
		coverInfoTableXML: buildCoverGridTableXML([][2]string{
			{"学院", "护理学院"},
			{"专业", "护理学"},
			{"班级", "2022级护理学5班"},
			{"学号", "20220152192"},
			{"姓名", "冉怡琴"},
			{"指导教师", "杨严政"},
		}, nil),
		bodyParagraphs: []string{"用户正文"},
		referenceItems: []string{"用户参考文献"},
		ackParagraphs:  []string{"用户致谢"},
	}))
	templatePath := writeTinyDocxFixture(t, tmpDir, "template.docx", buildCoverGridDocParts(coverGridDocFixture{
		coverTitleTableXML: buildCoverGridTableXML([][2]string{
			{"题目", "模板标题占位"},
			{"", ""},
		}, map[int]map[int]bool{
			1: {1: true},
		}),
		coverInfoTableXML: buildCoverGridTableXML([][2]string{
			{"学院", "计算机工程学院"},
			{"专业", "物联网工程"},
			{"班级", "2021级物联网工程2班"},
			{"学号", ""},
			{"姓名", ""},
			{"指导教师", ""},
		}, map[int]map[int]bool{
			3: {1: true},
			4: {1: true},
			5: {1: true},
		}),
		bodyParagraphs: []string{"模板正文"},
		referenceItems: []string{"模板参考文献"},
		ackParagraphs:  []string{"模板致谢"},
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
	for _, want := range []string{
		"社区2型糖尿病患者疾病知识",
		"认知现状及影响因素分析",
		"护理学院",
		"20220152192",
		"冉怡琴",
		"杨严政",
	} {
		if !strings.Contains(outputXML, want) {
			t.Fatalf("expected output to contain %q, got %s", want, outputXML)
		}
	}
}

type coverGridDocFixture struct {
	coverTitleTableXML string
	coverInfoTableXML  string
	bodyParagraphs     []string
	referenceItems     []string
	ackParagraphs      []string
}

func buildCoverGridDocParts(fixture coverGridDocFixture) map[string]string {
	var body strings.Builder
	body.WriteString("<w:body>")
	body.WriteString(fixture.coverTitleTableXML)
	body.WriteString(fixture.coverInfoTableXML)
	body.WriteString(buildCoverGridParagraphXML("摘  要"))
	body.WriteString(buildCoverGridParagraphXML("Abstract"))
	body.WriteString(buildCoverGridParagraphXML("目  录"))
	for _, para := range fixture.bodyParagraphs {
		body.WriteString(buildCoverGridParagraphXML(para))
	}
	body.WriteString(buildCoverGridParagraphXML("参考文献"))
	for _, para := range fixture.referenceItems {
		body.WriteString(buildCoverGridParagraphXML(para))
	}
	body.WriteString(buildCoverGridParagraphXML("致谢"))
	for _, para := range fixture.ackParagraphs {
		body.WriteString(buildCoverGridParagraphXML(para))
	}
	body.WriteString("<w:sectPr/>")
	body.WriteString("</w:body>")

	return map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` + body.String() + `</w:document>`,
	}
}

func buildSingleColumnTableXML(rows []string) string {
	var b strings.Builder
	b.WriteString("<w:tbl>")
	for _, row := range rows {
		b.WriteString("<w:tr><w:tc><w:p><w:r><w:t>")
		b.WriteString(html.EscapeString(row))
		b.WriteString("</w:t></w:r></w:p></w:tc></w:tr>")
	}
	b.WriteString("</w:tbl>")
	return b.String()
}

func buildCoverGridTableXML(rows [][2]string, emptyCells map[int]map[int]bool) string {
	var b strings.Builder
	b.WriteString("<w:tbl>")
	for rowIndex, row := range rows {
		b.WriteString("<w:tr>")
		for cellIndex, text := range row {
			b.WriteString("<w:tc><w:p>")
			if emptyCells != nil && emptyCells[rowIndex][cellIndex] {
				b.WriteString("<w:pPr><w:rPr><w:b/></w:rPr></w:pPr>")
			} else if text != "" {
				b.WriteString("<w:r><w:t>")
				b.WriteString(html.EscapeString(text))
				b.WriteString("</w:t></w:r>")
			}
			b.WriteString("</w:p></w:tc>")
		}
		b.WriteString("</w:tr>")
	}
	b.WriteString("</w:tbl>")
	return b.String()
}

func buildCoverGridParagraphXML(text string) string {
	return "<w:p><w:r><w:t>" + html.EscapeString(text) + "</w:t></w:r></w:p>"
}
