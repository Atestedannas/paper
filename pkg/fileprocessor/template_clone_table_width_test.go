package fileprocessor

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTemplateBaseFormatterNormalizesWideTransplantedTablesAndPreservesContinuationCaptions(t *testing.T) {
	tmpDir := t.TempDir()

	userBody := strings.Join([]string{
		buildTemplateCloneParagraphXML("Table 1-1 Wide Table"),
		buildTemplateCloneWideTableXML("WideCell"),
		buildTemplateCloneParagraphXML("\u7eed\u88681-1 Wide Table"),
		buildTemplateCloneWideTableXML("WideCellCont"),
		buildTemplateCloneParagraphXML("User body tail"),
	}, "")

	userPath := writeTinyDocxFixture(t, tmpDir, "user.docx", buildTemplateCloneDocParts(10000, 1000, 1000, userBody))
	templatePath := writeTinyDocxFixture(t, tmpDir, "template.docx", buildTemplateCloneDocParts(10000, 1000, 1000, buildTemplateCloneParagraphXML("Template body placeholder")))
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	if _, err := NewTemplateBaseFormatter().Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	outputXML := string(readTinyDocxEntries(t, outputPath)["word/document.xml"])
	if !strings.Contains(outputXML, "\u7eed\u88681-1 Wide Table") {
		t.Fatalf("expected continuation caption to survive template clone, got %s", outputXML)
	}

	wideTableXML := findTemplateCloneTableContainingText(outputXML, "WideCell-A")
	if wideTableXML == "" {
		t.Fatalf("expected transplanted wide table in output, got %s", outputXML)
	}

	tableWidth, tableWidthType, ok := extractDocxTableWidth(wideTableXML)
	if !ok {
		t.Fatalf("expected normalized table width in output table, got %s", wideTableXML)
	}
	if tableWidthType != "dxa" {
		t.Fatalf("table width type = %q, want dxa", tableWidthType)
	}
	if tableWidth != 8000 {
		t.Fatalf("table width = %d, want 8000", tableWidth)
	}

	gridTotal := sumDocxWidths(extractDocxGridColumnWidths(wideTableXML))
	if gridTotal > 8000 {
		t.Fatalf("grid width total = %d, want <= 8000", gridTotal)
	}
}

func buildTemplateCloneDocParts(pageWidth, leftMargin, rightMargin int, bodyXML string) map[string]string {
	var body strings.Builder
	body.WriteString("<w:body>")
	body.WriteString(buildTemplateCloneTableXML([]string{"Cover Title"}))
	body.WriteString(buildTemplateCloneTableXML([]string{"College", "Major", "2026-04"}))
	body.WriteString(buildTemplateCloneParagraphXML("\u6458\u8981"))
	body.WriteString(buildTemplateCloneParagraphXML("Abstract"))
	body.WriteString(buildTemplateCloneParagraphXML("\u76ee\u5f55"))
	body.WriteString(bodyXML)
	body.WriteString(buildTemplateCloneParagraphXML("\u53c2\u8003\u6587\u732e"))
	body.WriteString(buildTemplateCloneParagraphXML("Reference item"))
	body.WriteString(buildTemplateCloneParagraphXML("\u81f4\u8c22"))
	body.WriteString(buildTemplateCloneParagraphXML("Acknowledgement body"))
	body.WriteString(buildTemplateCloneSectPrXML(pageWidth, leftMargin, rightMargin))
	body.WriteString("</w:body>")

	return map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` + body.String() + `</w:document>`,
	}
}

func buildTemplateCloneTableXML(rows []string) string {
	var builder strings.Builder
	builder.WriteString("<w:tbl><w:tblPr><w:tblW w:w=\"0\" w:type=\"auto\"/></w:tblPr>")
	for _, row := range rows {
		builder.WriteString("<w:tr><w:tc>")
		builder.WriteString(buildTemplateCloneParagraphXML(row))
		builder.WriteString("</w:tc></w:tr>")
	}
	builder.WriteString("</w:tbl>")
	return builder.String()
}

func buildTemplateCloneWideTableXML(label string) string {
	return `<w:tbl><w:tblPr><w:tblW w:w="5750" w:type="pct"/><w:tblLayout w:type="fixed"/></w:tblPr><w:tblGrid><w:gridCol w:w="2500"/><w:gridCol w:w="2500"/><w:gridCol w:w="6500"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="2500" w:type="dxa"/></w:tcPr><w:p><w:r><w:t>` + label + `-A</w:t></w:r></w:p></w:tc><w:tc><w:tcPr><w:tcW w:w="2500" w:type="dxa"/></w:tcPr><w:p><w:r><w:t>` + label + `-B</w:t></w:r></w:p></w:tc><w:tc><w:tcPr><w:tcW w:w="6500" w:type="dxa"/></w:tcPr><w:p><w:r><w:t>` + label + `-C</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
}

func buildTemplateCloneParagraphXML(text string) string {
	return `<w:p><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

func buildTemplateCloneSectPrXML(pageWidth, leftMargin, rightMargin int) string {
	return `<w:sectPr><w:pgSz w:w="` + intToString(pageWidth) + `" w:h="16838"/><w:pgMar w:top="1418" w:right="` + intToString(rightMargin) + `" w:bottom="1134" w:left="` + intToString(leftMargin) + `"/></w:sectPr>`
}

func findTemplateCloneTableContainingText(documentXML, text string) string {
	for _, tableXML := range extractDocxElements(documentXML, "w:tbl") {
		if strings.Contains(tableXML, text) {
			return tableXML
		}
	}
	return ""
}

func intToString(value int) string {
	return strconv.Itoa(value)
}
