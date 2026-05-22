package fileprocessor

import (
	"strings"
	"testing"
)

func TestNormalizeDocxBodyTypographyReappliesTemplateTypographyAfterRawTransplant(t *testing.T) {
	docXML := buildTemplateCloneTypographyDocParts(true)["word/document.xml"]
	bodyXML := extractDocxBodyXML(docXML)

	rules := strictTemplateBlockRules{
		Paragraph: map[strictBlockKind]ParagraphFormatSpec{
			strictBlockBody: {
				FontEastAsia:     "宋体",
				FontAscii:        "SimSun",
				FontSizeHalfPt:   24,
				FontSizeCSHalfPt: 24,
				Bold:             false,
			},
			strictBlockReferencesTitle: {
				FontEastAsia:     "黑体",
				FontAscii:        "SimHei",
				FontSizeHalfPt:   30,
				FontSizeCSHalfPt: 30,
				Bold:             true,
			},
			strictBlockReferencesItem: {
				FontEastAsia:     "宋体",
				FontAscii:        "SimSun",
				FontSizeHalfPt:   24,
				FontSizeCSHalfPt: 24,
				Bold:             false,
			},
			strictBlockAcknowledgementsTitle: {
				FontEastAsia:     "黑体",
				FontAscii:        "SimHei",
				FontSizeHalfPt:   30,
				FontSizeCSHalfPt: 30,
				Bold:             true,
			},
			strictBlockAcknowledgementsBody: {
				FontEastAsia:     "宋体",
				FontAscii:        "SimSun",
				FontSizeHalfPt:   24,
				FontSizeCSHalfPt: 24,
				Bold:             false,
			},
		},
	}

	updatedBodyXML, err := normalizeDocxBodyTypography(bodyXML, rules)
	if err != nil {
		t.Fatalf("normalizeDocxBodyTypography() error = %v", err)
	}

	docXML = replaceDocxBodyXML(docXML, updatedBodyXML)

	bodyPara := extractFirstParagraphContaining(docXML, "This body paragraph should inherit template typography.")
	if bodyPara == "" {
		t.Fatalf("expected output body paragraph, got %s", docXML)
	}
	if strings.Contains(bodyPara, "<w:b") {
		t.Fatalf("expected body paragraph bold to be cleared, got %s", bodyPara)
	}
	if !strings.Contains(bodyPara, `w:ascii="SimSun"`) || strings.Contains(bodyPara, `Times New Roman`) {
		t.Fatalf("expected body paragraph fonts to come from template, got %s", bodyPara)
	}

	refPara := extractFirstParagraphContaining(docXML, "[1] Reference item should use template formatting.")
	if refPara == "" {
		t.Fatalf("expected output reference paragraph, got %s", docXML)
	}
	if strings.Contains(refPara, "<w:b") {
		t.Fatalf("expected reference paragraph bold to be cleared, got %s", refPara)
	}
	if !strings.Contains(refPara, `w:ascii="SimSun"`) || strings.Contains(refPara, `Times New Roman`) {
		t.Fatalf("expected reference paragraph fonts to come from template, got %s", refPara)
	}
}

func TestApplyTypographySpecToParagraphXMLClearsUserBoldAndFonts(t *testing.T) {
	paragraphXML := `<w:p><w:r><w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:eastAsia="仿宋" w:cs="Times New Roman"/><w:sz w:val="24"/><w:szCs w:val="24"/><w:b/><w:bCs/></w:rPr><w:t>Body text</w:t></w:r></w:p>`
	spec := ParagraphFormatSpec{
		FontEastAsia:     "宋体",
		FontAscii:        "SimSun",
		FontSizeHalfPt:   24,
		FontSizeCSHalfPt: 24,
		Bold:             false,
	}

	updated, err := applyTypographySpecToParagraphXML(paragraphXML, spec)
	if err != nil {
		t.Fatalf("applyTypographySpecToParagraphXML() error = %v", err)
	}
	if strings.Contains(updated, "<w:b") {
		t.Fatalf("expected bold tags to be removed, got %s", updated)
	}
	if !strings.Contains(updated, `w:ascii="SimSun"`) || strings.Contains(updated, `Times New Roman`) {
		t.Fatalf("expected font slots to be rewritten, got %s", updated)
	}
}

func buildTemplateCloneTypographyDocParts(userFormatting bool) map[string]string {
	return map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			buildTemplateCloneTypographyTableXML([]string{"Title", "Student Thesis"}) +
			buildTemplateCloneTypographyTableXML([]string{"College", "Nursing"}) +
			buildTemplateCloneTypographyParagraphXML("摘要", false, false) +
			buildTemplateCloneTypographyParagraphXML("Abstract", false, false) +
			buildTemplateCloneTypographyParagraphXML("目录", false, false) +
			buildTemplateCloneTypographyParagraphXML("This body paragraph should inherit template typography.", userFormatting, false) +
			buildTemplateCloneTypographyParagraphXML("参考文献", false, true) +
			buildTemplateCloneTypographyParagraphXML("[1] Reference item should use template formatting.", userFormatting, false) +
			buildTemplateCloneTypographyParagraphXML("致谢", false, true) +
			buildTemplateCloneTypographyParagraphXML("Acknowledgement body should also use template typography.", userFormatting, false) +
			`<w:sectPr/>` +
			`</w:body></w:document>`,
	}
}

func buildTemplateCloneTypographyTableXML(cells []string) string {
	var builder strings.Builder
	builder.WriteString(`<w:tbl><w:tr>`)
	for _, cell := range cells {
		builder.WriteString(`<w:tc>`)
		builder.WriteString(buildTemplateCloneTypographyParagraphXML(cell, false, false))
		builder.WriteString(`</w:tc>`)
	}
	builder.WriteString(`</w:tr></w:tbl>`)
	return builder.String()
}

func buildTemplateCloneTypographyParagraphXML(text string, userFormatting, titleFormatting bool) string {
	return `<w:p><w:r>` + buildTemplateCloneTypographyRunPropertiesXML(userFormatting, titleFormatting) + buildDocxTextElement(text) + `</w:r></w:p>`
}

func buildTemplateCloneTypographyRunPropertiesXML(userFormatting, titleFormatting bool) string {
	switch {
	case titleFormatting:
		return `<w:rPr><w:rFonts w:ascii="SimHei" w:hAnsi="SimHei" w:eastAsia="黑体" w:cs="SimHei"/><w:sz w:val="30"/><w:szCs w:val="30"/><w:b/><w:bCs/></w:rPr>`
	case userFormatting:
		return `<w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:eastAsia="仿宋" w:cs="Times New Roman"/><w:sz w:val="24"/><w:szCs w:val="24"/><w:b/><w:bCs/></w:rPr>`
	default:
		return `<w:rPr><w:rFonts w:ascii="SimSun" w:hAnsi="SimSun" w:eastAsia="宋体" w:cs="SimSun"/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr>`
	}
}
