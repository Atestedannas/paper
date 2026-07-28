package fileprocessor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

func TestExtractParaFormatSpecUsesDominantRunFormatting(t *testing.T) {
	doc := document.New()
	para := doc.AddParagraph()

	labelRun := para.AddRun()
	labelRun.AddText("[1] ")
	labelRun.X().RPr = wml.NewCT_RPr()
	labelRun.X().RPr.RFonts = wml.NewCT_Fonts()
	labelRun.X().RPr.RFonts.AsciiAttr = stringPtr("Calibri")
	labelRun.X().RPr.RFonts.HAnsiAttr = stringPtr("Calibri")
	labelSize := uint64(18)
	labelRun.X().RPr.Sz = wml.NewCT_HpsMeasure()
	labelRun.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &labelSize

	bodyRun := para.AddRun()
	bodyRun.AddText("社区二型糖尿病患者疾病知识认知水平")
	bodyRun.X().RPr = wml.NewCT_RPr()
	bodyRun.X().RPr.RFonts = wml.NewCT_Fonts()
	bodyRun.X().RPr.RFonts.EastAsiaAttr = stringPtr("宋体")
	bodyRun.X().RPr.RFonts.AsciiAttr = stringPtr("SimSun")
	bodyRun.X().RPr.RFonts.HAnsiAttr = stringPtr("SimSun")
	bodySize := uint64(24)
	bodyRun.X().RPr.Sz = wml.NewCT_HpsMeasure()
	bodyRun.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &bodySize

	spec := extractParaFormatSpec(para)
	if spec.FontEastAsia != "宋体" {
		t.Fatalf("expected dominant run EastAsia font Songti, got %#v", spec)
	}
	if spec.FontAscii != "SimSun" {
		t.Fatalf("expected dominant run ASCII font SimSun, got %#v", spec)
	}
	if spec.FontSizeHalfPt != 24 {
		t.Fatalf("expected dominant run size 24, got %#v", spec)
	}
}

func TestExtractParaFormatSpecCompletesTemplateFontSlotsFromAsciiOnlyRun(t *testing.T) {
	doc := document.New()
	para := doc.AddParagraph()

	labelRun := para.AddRun()
	labelRun.AddText("[1] ")
	labelRun.X().RPr = wml.NewCT_RPr()
	labelRun.X().RPr.RFonts = wml.NewCT_Fonts()
	labelRun.X().RPr.RFonts.AsciiAttr = stringPtr("Calibri")
	labelRun.X().RPr.RFonts.HAnsiAttr = stringPtr("Calibri")

	bodyRun := para.AddRun()
	bodyRun.AddText("社区2型糖尿病患者疾病知识认知水平研究")
	bodyRun.X().RPr = wml.NewCT_RPr()
	bodyRun.X().RPr.RFonts = wml.NewCT_Fonts()
	bodyRun.X().RPr.RFonts.AsciiAttr = stringPtr("宋体")
	bodyRun.X().RPr.RFonts.HAnsiAttr = stringPtr("宋体")
	bodyRun.X().RPr.RFonts.CsAttr = stringPtr("宋体")
	bodySize := uint64(24)
	bodyRun.X().RPr.Sz = wml.NewCT_HpsMeasure()
	bodyRun.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &bodySize

	spec := extractParaFormatSpec(para)
	if spec.FontEastAsia != "宋体" {
		t.Fatalf("expected extracted EastAsia font Songti, got %#v", spec)
	}
	if spec.FontAscii != "SimSun" {
		t.Fatalf("expected extracted ASCII font SimSun for complete overwrite, got %#v", spec)
	}
}

func TestApplyStrictSpecToParagraphFullyOverwritesExistingFontSlots(t *testing.T) {
	doc := document.New()
	para := doc.AddParagraph()
	run := para.AddRun()
	run.AddText("[1] 刘娟娟,姬学光.全媒体健康教育对2型糖尿病患者疾病认知水平及健康生活方式的影响[J].")
	run.X().RPr = wml.NewCT_RPr()
	run.X().RPr.RFonts = wml.NewCT_Fonts()
	run.X().RPr.RFonts.EastAsiaAttr = stringPtr("仿宋")
	run.X().RPr.RFonts.AsciiAttr = stringPtr("Times New Roman")
	run.X().RPr.RFonts.HAnsiAttr = stringPtr("Times New Roman")
	run.X().RPr.RFonts.CsAttr = stringPtr("Times New Roman")

	spec := ParagraphFormatSpec{
		FontAscii:        "宋体",
		FontSizeHalfPt:   24,
		FontSizeCSHalfPt: 24,
	}

	applyStrictSpecToParagraph(NewEnhancedProcessor(), para, spec)

	rFonts := run.X().RPr.RFonts
	if got := testStringValue(rFonts.EastAsiaAttr); got != "宋体" {
		t.Fatalf("expected EastAsia font Songti after overwrite, got %q", got)
	}
	if got := testStringValue(rFonts.AsciiAttr); got != "SimSun" {
		t.Fatalf("expected ASCII font SimSun after overwrite, got %q", got)
	}
	if got := testStringValue(rFonts.HAnsiAttr); got != "SimSun" {
		t.Fatalf("expected HAnsi font SimSun after overwrite, got %q", got)
	}
	if got := testStringValue(rFonts.CsAttr); got != "SimSun" {
		t.Fatalf("expected CS font SimSun after overwrite, got %q", got)
	}
}

func TestExtractParaFormatSpecFallsBackToParagraphDefaultRunProperties(t *testing.T) {
	doc := document.New()
	para := doc.AddParagraph()
	para.X().PPr = wml.NewCT_PPr()
	para.X().PPr.RPr = wml.NewCT_ParaRPr()
	para.X().PPr.RPr.RFonts = wml.NewCT_Fonts()
	para.X().PPr.RPr.RFonts.AsciiAttr = stringPtr("宋体")
	para.X().PPr.RPr.RFonts.HAnsiAttr = stringPtr("宋体")
	para.X().PPr.RPr.RFonts.CsAttr = stringPtr("宋体")
	defaultSize := uint64(24)
	para.X().PPr.RPr.Sz = wml.NewCT_HpsMeasure()
	para.X().PPr.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &defaultSize
	para.X().PPr.RPr.SzCs = wml.NewCT_HpsMeasure()
	para.X().PPr.RPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &defaultSize

	run := para.AddRun()
	run.AddText("行文至此，感激将尽。")

	spec := extractParaFormatSpec(para)
	if spec.FontEastAsia != "宋体" {
		t.Fatalf("expected paragraph default EastAsia font Songti, got %#v", spec)
	}
	if spec.FontAscii != "SimSun" {
		t.Fatalf("expected paragraph default ASCII font SimSun, got %#v", spec)
	}
	if spec.FontSizeHalfPt != 24 || spec.FontSizeCSHalfPt != 24 {
		t.Fatalf("expected paragraph default size 24, got %#v", spec)
	}
}

func TestExtractStrictTemplateBlockRulesFallsBackToBodyWhenAcknowledgementsHasNoBodySample(t *testing.T) {
	doc := document.New()

	body := doc.AddParagraph()
	bodyRun := body.AddRun()
	bodyRun.AddText("这是一段足够长的正文样例，用来给 strict formatter 提供正文字体基准。")
	bodyRun.X().RPr = wml.NewCT_RPr()
	bodyRun.X().RPr.RFonts = wml.NewCT_Fonts()
	bodyRun.X().RPr.RFonts.EastAsiaAttr = stringPtr("宋体")
	bodyRun.X().RPr.RFonts.AsciiAttr = stringPtr("SimSun")
	bodySize := uint64(24)
	bodyRun.X().RPr.Sz = wml.NewCT_HpsMeasure()
	bodyRun.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &bodySize

	ackTitle := doc.AddParagraph()
	ackTitleRun := ackTitle.AddRun()
	ackTitleRun.AddText("致      谢")
	ackTitleRun.X().RPr = wml.NewCT_RPr()
	ackTitleRun.X().RPr.RFonts = wml.NewCT_Fonts()
	ackTitleRun.X().RPr.RFonts.EastAsiaAttr = stringPtr("黑体")
	ackTitleRun.X().RPr.RFonts.AsciiAttr = stringPtr("SimHei")
	ackTitleSize := uint64(30)
	ackTitleRun.X().RPr.Sz = wml.NewCT_HpsMeasure()
	ackTitleRun.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &ackTitleSize

	appendixTitle := doc.AddParagraph()
	appendixTitleRun := appendixTitle.AddRun()
	appendixTitleRun.AddText("附录 A  附录题目")
	appendixTitleRun.X().RPr = wml.NewCT_RPr()
	appendixTitleRun.X().RPr.RFonts = wml.NewCT_Fonts()
	appendixTitleRun.X().RPr.RFonts.EastAsiaAttr = stringPtr("黑体")
	appendixTitleRun.X().RPr.RFonts.AsciiAttr = stringPtr("SimHei")
	appendixTitleRun.X().RPr.Sz = wml.NewCT_HpsMeasure()
	appendixTitleRun.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &ackTitleSize

	appendixBody := doc.AddParagraph()
	appendixBodyRun := appendixBody.AddRun()
	appendixBodyRun.AddText("这是一段足够长的附录正文样例，本来不应该被当成致谢正文去抽取。")
	appendixBodyRun.X().RPr = wml.NewCT_RPr()
	appendixBodyRun.X().RPr.RFonts = wml.NewCT_Fonts()
	appendixBodyRun.X().RPr.RFonts.EastAsiaAttr = stringPtr("仿宋")
	appendixBodyRun.X().RPr.RFonts.AsciiAttr = stringPtr("FangSong")
	appendixSize := uint64(24)
	appendixBodyRun.X().RPr.Sz = wml.NewCT_HpsMeasure()
	appendixBodyRun.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &appendixSize

	rules := extractStrictTemplateBlockRules(doc, NewEnhancedProcessor())
	got, ok := rules.Paragraph[strictBlockAcknowledgementsBody]
	if !ok {
		t.Fatalf("expected acknowledgements body rule to fall back to body spec")
	}
	if got.FontEastAsia != "宋体" {
		t.Fatalf("expected acknowledgements body to fall back to body font Songti, got %#v", got)
	}
}

func TestCopyTemplateHeaderFooterPackageCopiesHeaderMediaAndContentTypes(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "output.docx")

	templateEntries := map[string][]byte{
		"[Content_Types].xml":          []byte(`<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/><Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/></Types>`),
		"word/document.xml":            []byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body><w:p><w:r><w:t>Template</w:t></w:r></w:p><w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:footerReference w:type="default" r:id="rId9"/></w:sectPr></w:body></w:document>`),
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId8" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/><Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/></Relationships>`),
		"word/header1.xml":             []byte(`<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Template Header</w:t></w:r></w:p></w:hdr>`),
		"word/footer1.xml":             []byte(`<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Template Footer</w:t></w:r></w:p></w:ftr>`),
		"word/_rels/header1.xml.rels":  []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdImg1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/></Relationships>`),
		"word/media/image1.png":        []byte("png-bytes"),
	}
	outputEntries := map[string][]byte{
		"[Content_Types].xml":          []byte(`<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/></Types>`),
		"word/document.xml":            []byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Body</w:t></w:r></w:p><w:sectPr></w:sectPr></w:body></w:document>`),
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`),
	}
	if err := writeDocxEntries(templatePath, templateEntries); err != nil {
		t.Fatalf("write template entries: %v", err)
	}
	if err := writeDocxEntries(outputPath, outputEntries); err != nil {
		t.Fatalf("write output entries: %v", err)
	}

	if err := copyTemplateHeaderFooterPackage(templatePath, outputPath); err != nil {
		t.Fatalf("copyTemplateHeaderFooterPackage() error = %v", err)
	}

	imageEntry := readDocxEntry(t, outputPath, "word/media/image1.png")
	if imageEntry == "" {
		t.Fatalf("expected header image asset to be copied")
	}

	contentTypes := readDocxEntry(t, outputPath, "[Content_Types].xml")
	if !strings.Contains(contentTypes, `Extension="png"`) {
		t.Fatalf("expected image content type to be merged, got %s", contentTypes)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(docXML, `w:headerReference`) || !strings.Contains(docXML, `w:footerReference`) {
		t.Fatalf("expected document.xml to include template header/footer refs, got %s", docXML)
	}
}

func TestCopyAndMaterializeTemplateHeaderFooterPreservesStructure(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "output.docx")
	headerXML := `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:tabs><w:tab w:val="center" w:pos="4252"/></w:tabs><w:pBdr><w:bottom w:val="single" w:sz="6"/></w:pBdr></w:pPr><w:r><w:rPr><w:u w:val="single"/></w:rPr><w:t>重庆工程学院</w:t></w:r><w:r><w:t>XXX届</w:t><w:tab/></w:r><w:r><w:t>XXX专业</w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:r><w:t>本科毕业论文</w:t></w:r></w:p></w:hdr>`
	footerXML := `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> NUMPAGES </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p></w:ftr>`
	templateEntries := map[string][]byte{
		"[Content_Types].xml":          []byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/><Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/></Types>`),
		"word/document.xml":            []byte(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body><w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:footerReference w:type="default" r:id="rId9"/></w:sectPr></w:body></w:document>`),
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId8" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/><Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/></Relationships>`),
		"word/header1.xml":             []byte(headerXML),
		"word/footer1.xml":             []byte(footerXML),
	}
	outputEntries := map[string][]byte{
		"[Content_Types].xml":          []byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml" PartName="/word/header1.xml"/></Types>`),
		"word/document.xml":            []byte(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:sectPr/></w:body></w:document>`),
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId8" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`),
	}
	if err := writeDocxEntries(templatePath, templateEntries); err != nil {
		t.Fatal(err)
	}
	if err := writeDocxEntries(outputPath, outputEntries); err != nil {
		t.Fatal(err)
	}

	if err := copyAndMaterializeTemplateHeaderFooter(templatePath, outputPath, map[string]string{
		"专业": "护理学",
		"班级": "2022级护理学1班",
	}); err != nil {
		t.Fatal(err)
	}

	gotHeader := readDocxEntry(t, outputPath, "word/header1.xml")
	if strings.Join(extractDocxTextNodes(gotHeader), "") != "重庆工程学院2026届护理学专业本科毕业论文" {
		t.Fatalf("unexpected materialized header text: %s", gotHeader)
	}
	blank := func(xmlText string) string {
		return replaceDocxTextNodes(xmlText, make([]string, len(extractDocxTextNodes(xmlText))))
	}
	if blank(gotHeader) != blank(headerXML) {
		t.Fatalf("header structure changed\nwant: %s\ngot:  %s", blank(headerXML), blank(gotHeader))
	}
	if gotFooter := readDocxEntry(t, outputPath, "word/footer1.xml"); gotFooter != footerXML {
		t.Fatalf("footer structure changed\nwant: %s\ngot:  %s", footerXML, gotFooter)
	}
	if contentTypes := readDocxEntry(t, outputPath, "[Content_Types].xml"); strings.Count(contentTypes, `PartName="/word/header1.xml"`) != 1 {
		t.Fatalf("header content type must not be duplicated: %s", contentTypes)
	}
	rels := readDocxEntry(t, outputPath, "word/_rels/document.xml.rels")
	if strings.Count(rels, `Id="rId8"`) != 1 || !strings.Contains(rels, `Target="styles.xml"`) {
		t.Fatalf("existing relationship must remain unique: %s", rels)
	}
	if documentXML := readDocxEntry(t, outputPath, "word/document.xml"); strings.Contains(documentXML, `<w:headerReference w:type="default" r:id="rId8"/>`) {
		t.Fatalf("template header reference must be remapped away from an existing relationship: %s", documentXML)
	}
	if headerCount, footerCount, ok := verifyCopiedHeaderFooterStructure(templatePath, outputPath); !ok || headerCount != 1 || footerCount != 1 {
		t.Fatalf("copied header/footer did not verify: headers=%d footers=%d ok=%v", headerCount, footerCount, ok)
	}
}

func TestStrictTemplateFormatterPreservesTemplateHeaderAssetsInSyntheticDoc(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		doc.AddParagraph().AddRun().AddText("User Body")
	})
	writeTestDocx(t, templatePath, func(doc *document.Document) {
		header := doc.AddHeader()
		header.AddParagraph().AddRun().AddText("Template Header")
		doc.BodySection().SetHeader(header, wml.ST_HdrFtrDefault)
		footer := doc.AddFooter()
		footer.AddParagraph().AddRun().AddText("Template Footer")
		doc.BodySection().SetFooter(footer, wml.ST_HdrFtrDefault)
		doc.AddParagraph().AddRun().AddText("Template Body")
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
	relsXML := readDocxEntry(t, outputPath, "word/_rels/document.xml.rels")
	if !strings.Contains(docXML, `w:headerReference`) || !strings.Contains(docXML, `w:footerReference`) {
		t.Fatalf("expected output document to retain header/footer references, got %s", docXML)
	}
	if !strings.Contains(relsXML, `/header`) || !strings.Contains(relsXML, `/footer`) {
		t.Fatalf("expected output rels to retain header/footer relationships, got %s", relsXML)
	}
}

func TestMergeDocumentSectionHeaderFooterRefsKeepsFooterWhenTemplateLastSectionHasOnlyHeader(t *testing.T) {
	outputEntries := map[string][]byte{
		"word/document.xml":            []byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body><w:p><w:r><w:t>Body</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`),
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`),
	}
	templateEntries := map[string][]byte{
		"word/document.xml": []byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body><w:p><w:r><w:t>T1</w:t></w:r></w:p><w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:footerReference w:type="default" r:id="rId9"/></w:sectPr><w:p><w:r><w:t>T2</w:t></w:r></w:p><w:sectPr><w:footerReference w:type="default" r:id="rId10"/></w:sectPr><w:p><w:r><w:t>T3</w:t></w:r></w:p><w:sectPr><w:headerReference w:type="default" r:id="rId22"/></w:sectPr></w:body></w:document>`),
	}

	mergeDocumentSectionHeaderFooterRefs(outputEntries, templateEntries)

	docXML := string(outputEntries["word/document.xml"])
	if !strings.Contains(docXML, `w:headerReference`) {
		t.Fatalf("expected merged section refs to include header, got %s", docXML)
	}
	if !strings.Contains(docXML, `w:footerReference`) {
		t.Fatalf("expected merged section refs to include footer, got %s", docXML)
	}
}

func testStringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
