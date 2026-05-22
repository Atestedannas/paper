package fileprocessor

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/ofc/sharedTypes"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

func TestStrictTemplateFormatterPreservesUserContentAndCopiesTemplatePageSize(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		title := doc.AddParagraph()
		title.AddRun().AddText("User Title")
		body := doc.AddParagraph()
		body.AddRun().AddText("User Body")
		sectPr := doc.BodySection().X()
		sectPr.PgSz = wml.NewCT_PageSz()
		w := uint64(11906)
		h := uint64(16838)
		sectPr.PgSz.WAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &w}
		sectPr.PgSz.HAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &h}
	})
	writeTestDocx(t, templatePath, func(doc *document.Document) {
		doc.AddParagraph().AddRun().AddText("Template Title")
		sectPr := doc.BodySection().X()
		sectPr.PgSz = wml.NewCT_PageSz()
		w := uint64(12240)
		h := uint64(15840)
		sectPr.PgSz.WAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &w}
		sectPr.PgSz.HAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &h}
	})

	formatter := NewStrictTemplateFormatter()
	got, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	})
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if got != outputPath {
		t.Fatalf("Format() output = %q, want %q", got, outputPath)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(docXML, "User Title") || !strings.Contains(docXML, "User Body") {
		t.Fatalf("expected output to preserve user content, got %s", docXML)
	}
	if strings.Contains(docXML, "Template Title") {
		t.Fatalf("did not expect template text to replace user text: %s", docXML)
	}
	if !strings.Contains(docXML, `w:pgSz w:w="12240" w:h="15840"`) {
		t.Fatalf("expected template page size in output, got %s", docXML)
	}
}

func TestStrictTemplateFormatterCopiesTemplateHeaderFooterAndTableFormatting(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		cover := doc.AddParagraph()
		cover.AddRun().AddText("College: Nursing")
		heading := doc.AddParagraph()
		heading.AddRun().AddText("1 Introduction")

		coverTitleTable := doc.AddTable()
		coverTitleRow := coverTitleTable.AddRow()
		coverTitleRow.AddCell().AddParagraph().AddRun().AddText("Title")
		coverTitleRow.AddCell().AddParagraph().AddRun().AddText("User Cover Title")

		coverInfoTable := doc.AddTable()
		coverInfoRow := coverInfoTable.AddRow()
		coverInfoRow.AddCell().AddParagraph().AddRun().AddText("College")
		coverInfoRow.AddCell().AddParagraph().AddRun().AddText("Nursing")

		dataTable := doc.AddTable()
		dataRow := dataTable.AddRow()
		dataCell := dataRow.AddCell()
		dataCell.AddParagraph().AddRun().AddText("User Table")
	})
	writeTestDocx(t, templatePath, func(doc *document.Document) {
		cover := doc.AddParagraph()
		cover.Properties().SetAlignment(wml.ST_JcCenter)
		coverRun := cover.AddRun()
		coverRun.AddText("College: Sample")
		coverRun.Properties().SetFontFamily("SimSun")
		coverRun.Properties().SetSize(14)

		heading := doc.AddParagraph()
		heading.Properties().SetAlignment(wml.ST_JcCenter)
		headingRun := heading.AddRun()
		headingRun.AddText("1 Introduction")
		headingRun.Properties().SetBold(true)
		headingRun.Properties().SetFontFamily("SimHei")
		headingRun.Properties().SetSize(16)

		header := doc.AddHeader()
		header.AddParagraph().AddRun().AddText("Template Header")
		doc.BodySection().SetHeader(header, wml.ST_HdrFtrDefault)

		footer := doc.AddFooter()
		footer.AddParagraph().AddRun().AddText("Template Footer")
		doc.BodySection().SetFooter(footer, wml.ST_HdrFtrDefault)

		coverTitleTable := doc.AddTable()
		coverTitleRow := coverTitleTable.AddRow()
		coverTitleRow.AddCell().AddParagraph().AddRun().AddText("Title")
		coverTitleValue := coverTitleRow.AddCell().AddParagraph()
		coverTitleValue.Properties().SetAlignment(wml.ST_JcCenter)
		coverTitleValue.AddRun().AddText("Template Cover Title")

		coverInfoTable := doc.AddTable()
		coverInfoRow := coverInfoTable.AddRow()
		coverInfoRow.AddCell().AddParagraph().AddRun().AddText("College")
		coverInfoValue := coverInfoRow.AddCell().AddParagraph()
		coverInfoValue.Properties().SetAlignment(wml.ST_JcLeft)
		coverInfoValue.AddRun().AddText("Template College")

		dataTable := doc.AddTable()
		dataRow := dataTable.AddRow()
		dataRow.AddCell().AddParagraph().AddRun().AddText("Template Table")
	})

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(docXML, "College: Nursing") {
		t.Fatalf("expected cover text to stay unchanged, got %s", docXML)
	}
	if !strings.Contains(docXML, `<w:tblBorders>`) {
		t.Fatalf("expected table borders in output, got %s", docXML)
	}

	headerXML := readDocxEntry(t, outputPath, "word/header1.xml")
	footerXML := readDocxEntry(t, outputPath, "word/footer1.xml")
	if !strings.Contains(headerXML, "Template Header") {
		t.Fatalf("expected template header content, got %s", headerXML)
	}
	if !strings.Contains(footerXML, "Template Footer") {
		t.Fatalf("expected template footer content, got %s", footerXML)
	}
}

func TestMergeTemplateHeaderFooterPackageRewritesDanglingSectionRelationshipIDs(t *testing.T) {
	outputEntries := map[string][]byte{
		"word/document.xml":            []byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body><w:p><w:r><w:t>Body</w:t></w:r></w:p><w:sectPr><w:headerReference w:type="default" r:id="rId3"/><w:footerReference w:type="default" r:id="rId4"/></w:sectPr><w:sectPr><w:headerReference w:type="default" r:id="rId5"/><w:footerReference w:type="default" r:id="rId6"/></w:sectPr></w:body></w:document>`),
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`),
	}
	templateEntries := map[string][]byte{
		"word/document.xml":            []byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body><w:p><w:r><w:t>Template</w:t></w:r></w:p><w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:footerReference w:type="default" r:id="rId9"/></w:sectPr></w:body></w:document>`),
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId8" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/><Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/></Relationships>`),
	}

	mergeDocumentRelationships(outputEntries, templateEntries)
	mergeDocumentSectionHeaderFooterRefs(outputEntries, templateEntries)

	docXML := string(outputEntries["word/document.xml"])
	relsXML := string(outputEntries["word/_rels/document.xml.rels"])
	refIDs := collectRegexSubmatches(docXML, `<w:(?:headerReference|footerReference)[^>]*r:id="([^"]+)"`)
	relIDs := collectRegexSubmatches(relsXML, `Id="([^"]+)"`)
	relSet := make(map[string]bool, len(relIDs))
	for _, id := range relIDs {
		relSet[id] = true
	}

	for _, id := range refIDs {
		if !relSet[id] {
			t.Fatalf("expected sectPr reference %q to exist in rels; document.xml=%s rels=%s", id, docXML, relsXML)
		}
	}
}

func TestMergeDocumentSectionHeaderFooterRefsUsesTemplateSectionMapping(t *testing.T) {
	outputEntries := map[string][]byte{
		"word/document.xml": []byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
			`<w:p><w:r><w:t>Cover</w:t></w:r></w:p>` +
			`<w:sectPr><w:headerReference w:type="default" r:id="oldHeader1"/><w:footerReference w:type="default" r:id="oldFooter1"/></w:sectPr>` +
			`<w:p><w:r><w:t>Body</w:t></w:r></w:p>` +
			`<w:sectPr><w:headerReference w:type="default" r:id="oldHeader2"/><w:footerReference w:type="default" r:id="oldFooter2"/></w:sectPr>` +
			`<w:p><w:r><w:t>Refs</w:t></w:r></w:p>` +
			`<w:sectPr><w:headerReference w:type="default" r:id="oldHeader3"/><w:footerReference w:type="default" r:id="oldFooter3"/></w:sectPr>` +
			`</w:body></w:document>`),
	}
	templateEntries := map[string][]byte{
		"word/document.xml": []byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
			`<w:p><w:r><w:t>Template Cover</w:t></w:r></w:p>` +
			`<w:sectPr><w:type w:val="nextPage"/></w:sectPr>` +
			`<w:p><w:r><w:t>Template Body</w:t></w:r></w:p>` +
			`<w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:footerReference w:type="default" r:id="rId9"/></w:sectPr>` +
			`</w:body></w:document>`),
	}

	mergeDocumentSectionHeaderFooterRefs(outputEntries, templateEntries)

	sectPrs := extractSectPrBlocks(string(outputEntries["word/document.xml"]))
	if len(sectPrs) != 3 {
		t.Fatalf("expected 3 sectPr blocks, got %d", len(sectPrs))
	}
	if strings.Contains(sectPrs[0], "headerReference") || strings.Contains(sectPrs[0], "footerReference") {
		t.Fatalf("expected first output section to inherit template cover section with no header/footer refs, got %s", sectPrs[0])
	}
	for idx, sectPr := range sectPrs[1:] {
		if !strings.Contains(sectPr, `r:id="rId8"`) || !strings.Contains(sectPr, `r:id="rId9"`) {
			t.Fatalf("expected output section %d to reuse template body header/footer refs, got %s", idx+2, sectPr)
		}
	}
}

func TestMergeDocumentSectionHeaderFooterRefsCopiesTemplateSectionProperties(t *testing.T) {
	outputEntries := map[string][]byte{
		"word/document.xml": []byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
			`<w:p><w:r><w:t>Cover</w:t></w:r></w:p>` +
			`<w:sectPr><w:type w:val="nextPage"/><w:pgMar w:top="1418" w:right="1134" w:bottom="1134" w:left="1418"/><w:pgNumType w:fmt="upperRoman" w:start="1"/></w:sectPr>` +
			`<w:p><w:r><w:t>Body</w:t></w:r></w:p>` +
			`<w:sectPr><w:headerReference w:type="default" r:id="oldHeader2"/><w:footerReference w:type="default" r:id="oldFooter2"/><w:type w:val="nextPage"/><w:pgMar w:top="1418" w:right="1134" w:bottom="1134" w:left="1418"/><w:pgNumType w:fmt="decimal" w:start="1"/></w:sectPr>` +
			`<w:p><w:r><w:t>Refs</w:t></w:r></w:p>` +
			`<w:sectPr><w:headerReference w:type="default" r:id="oldHeader3"/><w:footerReference w:type="default" r:id="oldFooter3"/><w:type w:val="nextPage"/><w:pgMar w:top="1418" w:right="1134" w:bottom="1134" w:left="1418"/><w:pgNumType w:fmt="decimal" w:start="1"/></w:sectPr>` +
			`</w:body></w:document>`),
	}
	templateEntries := map[string][]byte{
		"word/document.xml": []byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
			`<w:p><w:r><w:t>Template Cover</w:t></w:r></w:p>` +
			`<w:sectPr><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134"/></w:sectPr>` +
			`<w:p><w:r><w:t>Template Body</w:t></w:r></w:p>` +
			`<w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:footerReference w:type="default" r:id="rId9"/><w:type w:val="continuous"/><w:pgMar w:top="1418" w:right="1134" w:bottom="1134" w:left="1418"/><w:pgNumType w:fmt="upperRoman" w:start="0"/><w:titlePg/></w:sectPr>` +
			`</w:body></w:document>`),
	}

	mergeDocumentSectionHeaderFooterRefs(outputEntries, templateEntries)

	sectPrs := extractSectPrBlocks(string(outputEntries["word/document.xml"]))
	if len(sectPrs) != 3 {
		t.Fatalf("expected 3 sectPr blocks, got %d", len(sectPrs))
	}
	if !strings.Contains(sectPrs[0], `w:top="1134"`) || strings.Contains(sectPrs[0], `w:start="1"`) {
		t.Fatalf("expected first output section to inherit template cover section properties, got %s", sectPrs[0])
	}
	for idx, sectPr := range sectPrs[1:] {
		if !strings.Contains(sectPr, `r:id="rId8"`) || !strings.Contains(sectPr, `r:id="rId9"`) {
			t.Fatalf("expected output section %d to reuse template body header/footer refs, got %s", idx+2, sectPr)
		}
		if !strings.Contains(sectPr, `w:type w:val="continuous"`) {
			t.Fatalf("expected output section %d to inherit template section break type, got %s", idx+2, sectPr)
		}
		if !strings.Contains(sectPr, `w:pgNumType w:fmt="upperRoman" w:start="0"`) {
			t.Fatalf("expected output section %d to inherit template page numbering, got %s", idx+2, sectPr)
		}
		if !strings.Contains(sectPr, `<w:titlePg/>`) {
			t.Fatalf("expected output section %d to inherit template titlePg flag, got %s", idx+2, sectPr)
		}
	}
}

func TestApplyStrictRuleFallbacksForcesPageBreakBeforeReferencesAndAcknowledgementsTitles(t *testing.T) {
	rules := strictTemplateBlockRules{
		Paragraph: map[strictBlockKind]ParagraphFormatSpec{
			strictBlockReferencesTitle: {
				FontEastAsia:   "黑体",
				FontAscii:      "SimHei",
				FontSizeHalfPt: 32,
			},
			strictBlockAcknowledgementsTitle: {
				FontEastAsia:   "黑体",
				FontAscii:      "SimHei",
				FontSizeHalfPt: 32,
			},
		},
	}

	applyStrictRuleFallbacks(&rules)

	if !rules.Paragraph[strictBlockReferencesTitle].PageBreak {
		t.Fatalf("expected references title fallback to force page break, got %#v", rules.Paragraph[strictBlockReferencesTitle])
	}
	if !rules.Paragraph[strictBlockAcknowledgementsTitle].PageBreak {
		t.Fatalf("expected acknowledgements title fallback to force page break, got %#v", rules.Paragraph[strictBlockAcknowledgementsTitle])
	}
}

func TestApplyStrictRuleFallbacksRepairsEnglishAbstractBodySpecFromBodyRule(t *testing.T) {
	rules := strictTemplateBlockRules{
		Paragraph: map[strictBlockKind]ParagraphFormatSpec{
			strictBlockBody: {
				FontEastAsia:     "宋体",
				FontAscii:        "Times New Roman",
				FontSizeHalfPt:   24,
				FontSizeCSHalfPt: 24,
				Bold:             false,
			},
		},
		Inline: map[strictBlockKind]inlinePrefixRule{
			strictBlockAbstractEN: {
				Prefix: "Abstract:",
				LabelSpec: ParagraphFormatSpec{
					FontAscii:        "Times New Roman",
					FontSizeHalfPt:   30,
					FontSizeCSHalfPt: 30,
					Bold:             true,
				},
				BodySpec: ParagraphFormatSpec{
					FontAscii:        "Times New Roman",
					FontSizeHalfPt:   30,
					FontSizeCSHalfPt: 30,
					Bold:             true,
				},
			},
		},
	}

	applyStrictRuleFallbacks(&rules)

	rule := rules.Inline[strictBlockAbstractEN]
	if rule.BodySpec.Bold {
		t.Fatalf("expected English abstract body spec to stop inheriting label bold, got %#v", rule.BodySpec)
	}
	if rule.BodySpec.FontSizeHalfPt != 24 {
		t.Fatalf("expected English abstract body spec to inherit body font size, got %#v", rule.BodySpec)
	}
}

func TestStrictTemplateFormatterClearsDirectRunColorWhenTemplateColorIsUnset(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		para := doc.AddParagraph()
		run := para.AddRun()
		run.AddText("Red Body")
		if run.X().RPr == nil {
			run.X().RPr = wml.NewCT_RPr()
		}
		run.X().RPr.Color = wml.NewCT_Color()
		run.X().RPr.Color.ValAttr.ST_HexColorRGB = stringPtr("FF0000")
	})

	writeTestDocx(t, templatePath, func(doc *document.Document) {
		para := doc.AddParagraph()
		run := para.AddRun()
		run.AddText("Template Body")
		run.Properties().SetFontFamily("SimSun")
		run.Properties().SetSize(12)
	})

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Contains(docXML, `w:color w:val="FF0000"`) {
		t.Fatalf("expected direct red run color to be removed, got %s", docXML)
	}
}

func TestStrictTemplateFormatterCopiesTemplateCoverTableCellFormatting(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		titleTable := doc.AddTable()
		titleRow := titleTable.AddRow()
		titleRow.AddCell().AddParagraph().AddRun().AddText("Title")
		titleRow.AddCell().AddParagraph().AddRun().AddText("Community Health")

		infoTable := doc.AddTable()
		infoRow := infoTable.AddRow()
		infoRow.AddCell().AddParagraph().AddRun().AddText("College")
		infoValue := infoRow.AddCell()
		infoPara := infoValue.AddParagraph()
		infoPara.Properties().SetAlignment(wml.ST_JcCenter)
		infoRun := infoPara.AddRun()
		infoRun.AddText("Nursing")
		infoRun.Properties().SetFontFamily("Calibri")
		infoRun.Properties().SetSize(20)
	})

	writeTestDocx(t, templatePath, func(doc *document.Document) {
		titleTable := doc.AddTable()
		titleRow := titleTable.AddRow()
		titleLabel := titleRow.AddCell().AddParagraph()
		titleLabel.Properties().SetAlignment(wml.ST_JcCenter)
		titleLabelRun := titleLabel.AddRun()
		titleLabelRun.AddText("Title")
		titleLabelRun.Properties().SetFontFamily("SimSun")
		titleLabelRun.Properties().SetSize(14)

		titleValue := titleRow.AddCell().AddParagraph()
		titleValue.Properties().SetAlignment(wml.ST_JcCenter)
		titleValueRun := titleValue.AddRun()
		titleValueRun.AddText("Template Title")
		titleValueRun.Properties().SetFontFamily("SimHei")
		titleValueRun.Properties().SetSize(22)

		infoTable := doc.AddTable()
		infoRow := infoTable.AddRow()
		infoLabel := infoRow.AddCell().AddParagraph()
		infoLabel.Properties().SetAlignment(wml.ST_JcLeft)
		infoLabelRun := infoLabel.AddRun()
		infoLabelRun.AddText("College")
		infoLabelRun.Properties().SetFontFamily("SimSun")
		infoLabelRun.Properties().SetSize(14)

		infoValue := infoRow.AddCell().AddParagraph()
		infoValue.Properties().SetAlignment(wml.ST_JcLeft)
		infoValueRun := infoValue.AddRun()
		infoValueRun.AddText("Template Value")
		infoValueRun.Properties().SetFontFamily("SimSun")
		infoValueRun.Properties().SetSize(14)
	})

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(docXML, "Nursing") {
		t.Fatalf("expected user cell text to remain, got %s", docXML)
	}
	if !strings.Contains(docXML, `w:sz w:val="28"`) {
		t.Fatalf("expected personal info cells to be formatted as fourth-size Songti, got %s", docXML)
	}
	if !strings.Contains(docXML, `w:jc w:val="left"`) {
		t.Fatalf("expected personal info cell alignment to be applied, got %s", docXML)
	}
	if !strings.Contains(docXML, `w:sz w:val="44"`) {
		t.Fatalf("expected title cells to be formatted as bold second-size heading, got %s", docXML)
	}
}

func TestStrictTemplateFormatterDoesNotFormatBodyTableAsCoverInfoTable(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		titleTable := doc.AddTable()
		titleRow := titleTable.AddRow()
		titleRow.AddCell().AddParagraph().AddRun().AddText("Title")
		titleRow.AddCell().AddParagraph().AddRun().AddText("Community Health")

		infoTable := doc.AddTable()
		infoRow := infoTable.AddRow()
		infoRow.AddCell().AddParagraph().AddRun().AddText("College")
		infoRow.AddCell().AddParagraph().AddRun().AddText("Nursing")

		doc.AddParagraph().AddRun().AddText("摘要：用户摘要正文")

		bodyTable := doc.AddTable()
		bodyRow := bodyTable.AddRow()
		bodyRow.AddCell().AddParagraph().AddRun().AddText("College")
		bodyValuePara := bodyRow.AddCell().AddParagraph()
		bodyValuePara.Properties().SetAlignment(wml.ST_JcCenter)
		bodyValueRun := bodyValuePara.AddRun()
		bodyValueRun.AddText("Should Stay")
		bodyValueRun.Properties().SetFontFamily("Calibri")
		bodyValueRun.Properties().SetSize(20)
	})

	writeTestDocx(t, templatePath, func(doc *document.Document) {
		titleTable := doc.AddTable()
		titleRow := titleTable.AddRow()
		titleRow.AddCell().AddParagraph().AddRun().AddText("Title")
		titleValue := titleRow.AddCell().AddParagraph()
		titleValue.Properties().SetAlignment(wml.ST_JcCenter)
		titleValueRun := titleValue.AddRun()
		titleValueRun.AddText("Template Title")
		titleValueRun.Properties().SetFontFamily("SimHei")
		titleValueRun.Properties().SetSize(22)

		infoTable := doc.AddTable()
		infoRow := infoTable.AddRow()
		infoRow.AddCell().AddParagraph().AddRun().AddText("College")
		infoValue := infoRow.AddCell().AddParagraph()
		infoValue.Properties().SetAlignment(wml.ST_JcLeft)
		infoValueRun := infoValue.AddRun()
		infoValueRun.AddText("Template Value")
		infoValueRun.Properties().SetFontFamily("SimSun")
		infoValueRun.Properties().SetSize(14)
	})

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	bodyValuePara := extractFirstParagraphContaining(readDocxEntry(t, outputPath, "word/document.xml"), "Should Stay")
	if bodyValuePara == "" {
		t.Fatalf("expected body table value paragraph in output")
	}
	if !strings.Contains(bodyValuePara, `w:sz w:val="40"`) {
		t.Fatalf("expected body table value to keep original font size, got %s", bodyValuePara)
	}
	if strings.Contains(bodyValuePara, `w:sz w:val="28"`) {
		t.Fatalf("did not expect body table to be reformatted as cover info, got %s", bodyValuePara)
	}
}

func TestStrictTemplateFormatterPreservesTemplateCoverBordersWhileKeepingBodyTableThreeLine(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		titleTable := doc.AddTable()
		titleRow := titleTable.AddRow()
		titleRow.AddCell().AddParagraph().AddRun().AddText("Title")
		titleRow.AddCell().AddParagraph().AddRun().AddText("Community Health")

		infoTable := doc.AddTable()
		infoRow := infoTable.AddRow()
		infoRow.AddCell().AddParagraph().AddRun().AddText("College")
		infoRow.AddCell().AddParagraph().AddRun().AddText("Nursing")

		bodyTable := doc.AddTable()
		bodyRow := bodyTable.AddRow()
		bodyRow.AddCell().AddParagraph().AddRun().AddText("Body")
		bodyRow.AddCell().AddParagraph().AddRun().AddText("Data")
	})

	writeTestDocx(t, templatePath, func(doc *document.Document) {
		titleTable := doc.AddTable()
		titleRow := titleTable.AddRow()
		titleRow.AddCell().AddParagraph().AddRun().AddText("Title")
		titleRow.AddCell().AddParagraph().AddRun().AddText("Template Title")
		setTableBoxBorders(titleTable)

		infoTable := doc.AddTable()
		infoRow := infoTable.AddRow()
		infoRow.AddCell().AddParagraph().AddRun().AddText("College")
		infoRow.AddCell().AddParagraph().AddRun().AddText("Template College")
		setTableBoxBorders(infoTable)

		bodyTable := doc.AddTable()
		bodyRow := bodyTable.AddRow()
		bodyRow.AddCell().AddParagraph().AddRun().AddText("Template Body")
		bodyRow.AddCell().AddParagraph().AddRun().AddText("Template Data")
	})

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	tables := extractTables(docXML)
	if len(tables) < 3 {
		t.Fatalf("expected 3 tables in output, got %d", len(tables))
	}

	for idx, coverTable := range tables[:2] {
		if !strings.Contains(coverTable, `<w:left w:val="single"`) {
			t.Fatalf("expected cover table %d to preserve template left border, got %s", idx, coverTable)
		}
		if !strings.Contains(coverTable, `<w:right w:val="single"`) {
			t.Fatalf("expected cover table %d to preserve template right border, got %s", idx, coverTable)
		}
		if !strings.Contains(coverTable, `<w:insideV w:val="single"`) {
			t.Fatalf("expected cover table %d to preserve template inside vertical border, got %s", idx, coverTable)
		}
	}

	bodyTable := tables[2]
	if !strings.Contains(bodyTable, `<w:left w:val="none"`) {
		t.Fatalf("expected body table to keep three-line left border clearing, got %s", bodyTable)
	}
	if !strings.Contains(bodyTable, `<w:right w:val="none"`) {
		t.Fatalf("expected body table to keep three-line right border clearing, got %s", bodyTable)
	}
}

func TestStrictTemplateFormatterNormalizesTemplateRedColorToBlack(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		para := doc.AddParagraph()
		para.AddRun().AddText("User Thesis Title")
	})

	writeTestDocx(t, templatePath, func(doc *document.Document) {
		para := doc.AddParagraph()
		run := para.AddRun()
		run.AddText("Template Thesis Title")
		run.Properties().SetFontFamily("SimHei")
		run.Properties().SetSize(22)
		if run.X().RPr == nil {
			run.X().RPr = wml.NewCT_RPr()
		}
		run.X().RPr.Color = wml.NewCT_Color()
		run.X().RPr.Color.ValAttr.ST_HexColorRGB = stringPtr("FF0000")
	})

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Contains(docXML, `w:color w:val="FF0000"`) {
		t.Fatalf("expected template red formatting to be neutralized, got %s", docXML)
	}
}

func TestStrictTemplateFormatterNormalizesCoverDateSpacing(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		doc.AddParagraph().AddRun().AddText("Cover")
		datePara := doc.AddParagraph()
		datePara.AddRun().AddText("2026 \u5e74 ")
		datePara.AddRun().AddText(" 3 \u6708")
	})

	writeTestDocx(t, templatePath, func(doc *document.Document) {
		doc.AddParagraph().AddRun().AddText("Cover")
		datePara := doc.AddParagraph()
		datePara.Properties().SetAlignment(wml.ST_JcCenter)
		dateRun := datePara.AddRun()
		dateRun.AddText("2026\u5e743\u6708")
		dateRun.Properties().SetFontFamily("SimSun")
		dateRun.Properties().SetSize(16)
		dateRun.Properties().SetBold(true)
	})

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(docXML, "2026年3月") {
		t.Fatalf("expected normalized cover date, got %s", docXML)
	}
}

func TestStrictTemplateFormatterDoesNotHardCodeCoverDateYear(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		doc.AddParagraph().AddRun().AddText("Cover")
		doc.AddParagraph().AddRun().AddText(" 2027年 4 月")
		doc.AddParagraph().AddRun().AddText("摘要：用户摘要正文")
	})

	writeTestDocx(t, templatePath, func(doc *document.Document) {
		doc.AddParagraph().AddRun().AddText("Cover")
		date := doc.AddParagraph()
		date.Properties().SetAlignment(wml.ST_JcCenter)
		dateRun := date.AddRun()
		dateRun.AddText("202X年 4月")
		dateRun.Properties().SetFontFamily("SimSun")
		dateRun.Properties().SetSize(16)
		doc.AddParagraph().AddRun().AddText("摘要：模板摘要正文")
	})

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(docXML, "2027年4月") {
		t.Fatalf("expected original year to be preserved while normalizing spacing, got %s", docXML)
	}
	if strings.Contains(docXML, "2026年4月") {
		t.Fatalf("did not expect cover date normalization to hard-code 2026, got %s", docXML)
	}
}

func TestStrictTemplateFormatterPreservesRealCoverTableLayoutNodes(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	copyFile(t, repoFixturePath(t, "uploads", "user.docx"), userPath)
	copyFile(t, repoFixturePath(t, "uploads", "template.docx"), templatePath)

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	tables := extractTables(docXML)
	if len(tables) < 2 {
		t.Fatalf("expected at least 2 tables in output, got %d", len(tables))
	}
	if !strings.Contains(tables[0], "<w:tblpPr") || !strings.Contains(tables[0], "<w:tblW") || !strings.Contains(tables[0], "<w:gridCol") || !strings.Contains(tables[0], "<w:tcW") {
		t.Fatalf("expected first cover table to keep layout nodes, got %s", tables[0])
	}
	if !strings.Contains(tables[1], "<w:tblW") || !strings.Contains(tables[1], "<w:gridCol") || !strings.Contains(tables[1], "<w:tcW") {
		t.Fatalf("expected second cover table to keep layout nodes, got %s", tables[1])
	}
}

func TestStrictTemplateFormatterDoesNotUseCoverTitleFontForRealCoverDate(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	copyFile(t, repoFixturePath(t, "uploads", "user.docx"), userPath)
	copyFile(t, repoFixturePath(t, "uploads", "template.docx"), templatePath)

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	datePara := extractFirstParagraphContaining(docXML, "2026")
	if datePara == "" {
		t.Fatalf("expected a cover date paragraph in output")
	}
	if strings.Contains(datePara, `w:sz w:val="72"`) {
		t.Fatalf("expected cover date not to inherit title size 72, got %s", datePara)
	}
	if strings.Contains(datePara, `w:rFonts w:ascii="黑体"`) || strings.Contains(datePara, `w:eastAsia="黑体"`) {
		t.Fatalf("expected cover date not to inherit title Heiti font, got %s", datePara)
	}
}

func TestStrictTemplateFormatterFormatsInlineChineseAbstractLabelAndBodySeparately(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		doc.AddParagraph().AddRun().AddText("Cover")
		abstract := doc.AddParagraph()
		abstract.AddRun().AddText("摘要：")
		abstract.AddRun().AddText("用户摘要正文")
	})

	writeTestDocx(t, templatePath, func(doc *document.Document) {
		doc.AddParagraph().AddRun().AddText("Cover")
		abstract := doc.AddParagraph()
		labelRun := abstract.AddRun()
		labelRun.AddText("摘要：")
		labelRun.Properties().SetFontFamily("SimHei")
		labelRun.Properties().SetSize(16)
		labelRun.Properties().SetBold(true)
		bodyRun := abstract.AddRun()
		bodyRun.AddText("模板摘要正文")
		bodyRun.Properties().SetFontFamily("SimSun")
		bodyRun.Properties().SetSize(12)
	})

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	abstractPara := extractFirstParagraphContaining(docXML, "用户摘要正文")
	if abstractPara == "" {
		t.Fatalf("expected abstract paragraph in output, got %s", docXML)
	}
	if !strings.Contains(abstractPara, "摘要：") {
		t.Fatalf("expected abstract label to remain, got %s", abstractPara)
	}
	if !(strings.Contains(abstractPara, `w:sz w:val="32"`) && strings.Contains(abstractPara, `w:sz w:val="24"`)) {
		t.Fatalf("expected separate label/body font sizes in abstract paragraph, got %s", abstractPara)
	}
}

func TestExtractStrictTemplateBlockRulesFromRealTemplate(t *testing.T) {
	templatePath := repoFixturePath(t, "uploads", "template.docx")

	templateDoc, err := document.Open(templatePath)
	if err != nil {
		t.Fatalf("document.Open(%q) error = %v", templatePath, err)
	}
	defer templateDoc.Close()

	rules := extractStrictTemplateBlockRules(templateDoc, NewEnhancedProcessor())

	if rule, ok := rules.Inline[strictBlockAbstractCN]; !ok || rule.LabelSpec.IsEmpty() || rule.BodySpec.IsEmpty() {
		t.Fatalf("expected Chinese abstract inline rule from real template, got %#v", rule)
	} else {
		if rule.LabelSpec.FontSizeHalfPt != 30 {
			t.Fatalf("expected real template abstract label size 30, got %#v", rule.LabelSpec)
		}
		if rule.BodySpec.FontSizeHalfPt != 24 {
			t.Fatalf("expected real template abstract body size 24, got %#v", rule.BodySpec)
		}
	}
	if spec, ok := rules.Paragraph[strictBlockHeading1]; !ok || spec.IsEmpty() {
		t.Fatalf("expected heading_1 rule from real template, got %#v", spec)
	}
	if spec, ok := rules.Paragraph[strictBlockBody]; !ok || spec.IsEmpty() {
		t.Fatalf("expected body rule from real template, got %#v", spec)
	} else {
		if spec.FontSizeHalfPt != 24 {
			t.Fatalf("expected real template body size 24, got %#v", spec)
		}
		if spec.Bold {
			t.Fatalf("expected real template body not bold, got %#v", spec)
		}
	}
	if rules.CoverTitleValue.IsEmpty() {
		t.Fatalf("expected cover title value rule from real template")
	}
	if rules.CoverInfoValue.IsEmpty() {
		t.Fatalf("expected cover info value rule from real template")
	}
}

func TestStrictTemplateFormatterFormatsRealChineseAbstractLabelSeparately(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	copyFile(t, repoFixturePath(t, "uploads", "user.docx"), userPath)
	copyFile(t, repoFixturePath(t, "uploads", "template.docx"), templatePath)

	formatter := NewStrictTemplateFormatter()
	if _, err := formatter.Format(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	abstractPara := extractFirstParagraphContaining(docXML, "\u63a2\u8ba8\u793e\u533a")
	if abstractPara == "" {
		t.Fatalf("expected real Chinese abstract paragraph in output, got %s", docXML)
	}
	if !strings.Contains(abstractPara, "\u6458\u8981\uff1a") {
		t.Fatalf("expected Chinese abstract label to remain, got %s", abstractPara)
	}
	if !strings.Contains(abstractPara, `w:sz w:val="30"`) {
		t.Fatalf("expected Chinese abstract label size from template, got %s", abstractPara)
	}
	if !strings.Contains(abstractPara, `w:sz w:val="24"`) {
		t.Fatalf("expected Chinese abstract body size from template, got %s", abstractPara)
	}
	if !(strings.Contains(abstractPara, `w:eastAsia="黑体"`) || strings.Contains(abstractPara, `w:ascii="黑体"`)) {
		t.Fatalf("expected Chinese abstract label font from template, got %s", abstractPara)
	}
}

func TestApplyStrictParagraphBlockRulesFormatsRealChineseAbstractBeforePackagePostProcess(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	copyFile(t, repoFixturePath(t, "uploads", "user.docx"), userPath)
	copyFile(t, repoFixturePath(t, "uploads", "template.docx"), templatePath)

	userDoc, err := document.Open(userPath)
	if err != nil {
		t.Fatalf("document.Open(%q) error = %v", userPath, err)
	}
	defer userDoc.Close()

	templateDoc, err := document.Open(templatePath)
	if err != nil {
		t.Fatalf("document.Open(%q) error = %v", templatePath, err)
	}
	defer templateDoc.Close()

	formatter := NewStrictTemplateFormatter()
	CloneStyles(templateDoc, userDoc)
	applyTemplateSectionLayout(userDoc, templateDoc)
	state := strictParagraphState{}
	detected := strictBlockUnknown
	for _, ref := range strictMainStoryParagraphs(userDoc) {
		text := strings.TrimSpace(formatter.processor.extractParagraphText(ref.Para))
		if !strings.Contains(text, "\u63a2\u8ba8\u793e\u533a") {
			_ = detectStrictParagraphBlock(ref, text, &state)
			continue
		}
		detected = detectStrictParagraphBlock(ref, text, &state)
		break
	}
	if detected != strictBlockAbstractCN {
		t.Fatalf("expected abstract paragraph to detect as %q, got %q", strictBlockAbstractCN, detected)
	}
	if err := formatter.applyParagraphAndRunFormatting(userDoc, templateDoc); err != nil {
		t.Fatalf("applyParagraphAndRunFormatting() error = %v", err)
	}
	if err := userDoc.SaveToFile(outputPath); err != nil {
		t.Fatalf("SaveToFile(%q) error = %v", outputPath, err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	abstractPara := extractFirstParagraphContaining(docXML, "\u63a2\u8ba8\u793e\u533a")
	if abstractPara == "" {
		t.Fatalf("expected real Chinese abstract paragraph in output, got %s", docXML)
	}
	if !strings.Contains(abstractPara, `w:sz w:val="30"`) || !strings.Contains(abstractPara, `w:sz w:val="24"`) {
		t.Fatalf("expected abstract formatting before package post-process, got %s", abstractPara)
	}
}

func TestPostProcessStrictOutputDeduplicatesFallbackAttributes(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := writeTinyDocxFixture(t, tmpDir, "formatted.docx", map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:body><w:p><w:r><w:t>Body</w:t></w:r></w:p>` +
			`<mc:Fallback mc:Ignorable="w10" xmlns="" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:w10="urn:schemas-microsoft-com:office:word" mc:Ignorable="w10" xmlns=""><w:pict/></mc:Fallback>` +
			`</w:body></w:document>`,
	})

	if err := postProcessStrictOutput(outputPath); err != nil {
		t.Fatalf("postProcessStrictOutput() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	fallbackTag := regexp.MustCompile(`<mc:Fallback[^>]*>`).FindString(docXML)
	if fallbackTag == "" {
		t.Fatalf("expected mc:Fallback tag in output, got %s", docXML)
	}
	if got := strings.Count(fallbackTag, `mc:Ignorable=`); got != 1 {
		t.Fatalf("expected single mc:Ignorable attribute after sanitization, got %d in %s", got, fallbackTag)
	}
	if got := strings.Count(fallbackTag, `xmlns=""`); got != 1 {
		t.Fatalf("expected single default xmlns attribute after sanitization, got %d in %s", got, fallbackTag)
	}
}

func writeTestDocx(t *testing.T, path string, fill func(*document.Document)) {
	t.Helper()

	doc := document.New()
	fill(doc)
	if err := doc.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile(%q) error = %v", path, err)
	}
}

func readDocxEntry(t *testing.T, path, entry string) string {
	t.Helper()

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("zip.OpenReader(%q) error = %v", path, err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.Name != entry {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("Open(%q) error = %v", entry, err)
		}
		defer rc.Close()

		content, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ReadAll(%q) error = %v", entry, err)
		}
		return string(content)
	}

	t.Fatalf("entry %q not found in %q", entry, path)
	return ""
}

func collectRegexSubmatches(input string, pattern string) []string {
	re := regexp.MustCompile(pattern)
	matches := re.FindAllStringSubmatch(input, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			out = append(out, match[1])
		}
	}
	return out
}

func stringPtr(v string) *string {
	return &v
}

func setTableBoxBorders(table document.Table) {
	if table.X().TblPr == nil {
		table.X().TblPr = wml.NewCT_TblPr()
	}
	table.X().TblPr.TblBorders = wml.NewCT_TblBorders()
	for _, border := range []*wml.CT_Border{
		func() *wml.CT_Border {
			table.X().TblPr.TblBorders.Top = wml.NewCT_Border()
			return table.X().TblPr.TblBorders.Top
		}(),
		func() *wml.CT_Border {
			table.X().TblPr.TblBorders.Bottom = wml.NewCT_Border()
			return table.X().TblPr.TblBorders.Bottom
		}(),
		func() *wml.CT_Border {
			table.X().TblPr.TblBorders.Left = wml.NewCT_Border()
			return table.X().TblPr.TblBorders.Left
		}(),
		func() *wml.CT_Border {
			table.X().TblPr.TblBorders.Right = wml.NewCT_Border()
			return table.X().TblPr.TblBorders.Right
		}(),
		func() *wml.CT_Border {
			table.X().TblPr.TblBorders.InsideH = wml.NewCT_Border()
			return table.X().TblPr.TblBorders.InsideH
		}(),
		func() *wml.CT_Border {
			table.X().TblPr.TblBorders.InsideV = wml.NewCT_Border()
			return table.X().TblPr.TblBorders.InsideV
		}(),
	} {
		border.ValAttr = wml.ST_BorderSingle
	}
}

func repoFixturePath(t *testing.T, elems ...string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	parts := append([]string{filepath.Dir(file), "..", ".."}, elems...)
	path := filepath.Clean(filepath.Join(parts...))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture path %q error = %v", path, err)
	}
	return path
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", dst, err)
	}
}

func extractTables(docXML string) []string {
	return regexp.MustCompile(`<w:tbl[\s\S]*?</w:tbl>`).FindAllString(docXML, -1)
}

func extractFirstParagraphContaining(docXML, needle string) string {
	for _, para := range regexp.MustCompile(`<w:p[\s\S]*?</w:p>`).FindAllString(docXML, -1) {
		if strings.Contains(para, needle) {
			return para
		}
	}
	return ""
}
