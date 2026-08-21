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
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/></Relationships>`),
		"word/media/image1.png":        []byte("student-png-bytes"),
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

	if imageEntry := readDocxEntry(t, outputPath, "word/media/image1.png"); imageEntry != "student-png-bytes" {
		t.Fatalf("student image must not be overwritten, got %q", imageEntry)
	}
	copiedImage := readDocxEntry(t, outputPath, "word/media/image1_template1.png")
	if copiedImage != "png-bytes" {
		t.Fatalf("expected colliding template header image to be copied under a new name, got %q", copiedImage)
	}
	headerRels := readDocxEntry(t, outputPath, "word/_rels/header1.xml.rels")
	if !strings.Contains(headerRels, `Target="media/image1_template1.png"`) {
		t.Fatalf("expected header relationship to be rewritten to renamed image, got %s", headerRels)
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

func TestEnsureEvenOddHeadersSettingAddsPackageSwitch(t *testing.T) {
	entries := map[string][]byte{
		"word/document.xml": []byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:sectPr><w:headerReference w:type="even"/></w:sectPr></w:body></w:document>`),
		"word/settings.xml": []byte(`<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>`),
	}

	ensureEvenOddHeadersSetting(entries)
	if !strings.Contains(string(entries["word/settings.xml"]), "evenAndOddHeaders") {
		t.Fatalf("expected settings.xml to enable even/odd headers, got %s", entries["word/settings.xml"])
	}
}

func TestSyncEvenOddHeadersSettingPreservesTemplateDisabledSwitch(t *testing.T) {
	output := map[string][]byte{
		"word/document.xml": []byte(`<w:document><w:sectPr><w:headerReference w:type="even"/></w:sectPr></w:document>`),
		"word/settings.xml": []byte(`<w:settings><w:evenAndOddHeaders/></w:settings>`),
	}
	template := map[string][]byte{
		"word/settings.xml": []byte(`<w:settings/>`),
	}
	syncEvenOddHeadersSetting(output, template)
	if strings.Contains(string(output["word/settings.xml"]), "evenAndOddHeaders") {
		t.Fatalf("template-disabled even/odd switch was retained: %s", output["word/settings.xml"])
	}
	if strings.Contains(string(output["word/document.xml"]), `w:type="even"`) {
		t.Fatalf("template-disabled even references were retained: %s", output["word/document.xml"])
	}
}

func TestRemapRelationshipIDsDoesNotChainMappings(t *testing.T) {
	xml := `<w:sectPr><w:footerReference w:type="default" r:id="rId6"/><w:headerReference w:type="default" r:id="rId12"/></w:sectPr>`
	got := remapRelationshipIDs(xml, map[string]string{"rId6": "rId12", "rId12": "rId19"})
	if !strings.Contains(got, `footerReference w:type="default" r:id="rId12"`) {
		t.Fatalf("footer reference was remapped through a second mapping: %s", got)
	}
	if !strings.Contains(got, `headerReference w:type="default" r:id="rId19"`) {
		t.Fatalf("header reference was not remapped: %s", got)
	}
}

func TestCopyTemplateHeaderFooterPackageMigratesReferencedStyleClosure(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "output.docx")

	templateEntries := map[string][]byte{
		"[Content_Types].xml":          []byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/></Types>`),
		"word/document.xml":            []byte(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body><w:p><w:r><w:t>Template</w:t></w:r></w:p><w:sectPr><w:headerReference w:type="default" r:id="rId8"/></w:sectPr></w:body></w:document>`),
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId8" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/><Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`),
		"word/header1.xml":             []byte(`<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:pStyle w:val="HeaderStyle"/></w:pPr><w:r><w:rPr><w:rStyle w:val="HeaderChar"/><w:rFonts w:eastAsiaTheme="majorEastAsia"/></w:rPr><w:t>Header</w:t></w:r></w:p></w:hdr>`),
		"word/styles.xml": []byte(`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:style w:type="paragraph" w:styleId="Base"><w:name w:val="Template Base"/><w:pPr><w:tabs><w:tab w:val="center" w:pos="4153"/></w:tabs><w:spacing w:line="240"/><w:jc w:val="center"/></w:pPr><w:rPr><w:rFonts w:asciiTheme="minorHAnsi" w:hAnsiTheme="minorHAnsi" w:eastAsiaTheme="minorEastAsia" w:cstheme="minorBidi"/><w:lang w:eastAsia="zh-CN" w:bidi="ar-SA"/></w:rPr></w:style>` +
			`<w:style w:type="character" w:styleId="DefaultChar"><w:name w:val="Template Default Character"/></w:style>` +
			`<w:style w:type="character" w:styleId="HeaderChar"><w:name w:val="Template Header Character"/><w:basedOn w:val="DefaultChar"/></w:style>` +
			`<w:style w:type="paragraph" w:styleId="NextStyle"><w:name w:val="Template Next"/><w:basedOn w:val="Base"/></w:style>` +
			`<w:style w:type="paragraph" w:styleId="HeaderStyle"><w:name w:val="Template Header"/><w:basedOn w:val="Base"/><w:link w:val="HeaderChar"/><w:next w:val="NextStyle"/><w:qFormat/><w:uiPriority w:val="99"/></w:style>` +
			`</w:styles>`),
		"word/theme/theme1.xml": []byte(`<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:fontScheme name="Template"><a:majorFont><a:latin typeface="Template Major Latin"/><a:ea typeface=""/><a:cs typeface=""/><a:font script="Hans" typeface="Template Major Hans"/></a:majorFont><a:minorFont><a:latin typeface="Template Minor Latin"/><a:ea typeface=""/><a:cs typeface=""/><a:font script="Hans" typeface="Template Minor Hans"/><a:font script="Arab" typeface="Template Minor Arabic"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`),
		"word/fontTable.xml":    []byte(`<w:fonts xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:font w:name="Template Major Hans"/><w:font w:name="Template Minor Latin"/><w:font w:name="Template Minor Hans"/><w:font w:name="Template Minor Arabic"/></w:fonts>`),
	}
	outputEntries := map[string][]byte{
		"[Content_Types].xml":          []byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`),
		"word/document.xml":            []byte(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Body</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`),
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`),
		"word/styles.xml": []byte(`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:style w:type="paragraph" w:styleId="Base"><w:name w:val="Student Base"/><w:pPr><w:jc w:val="left"/></w:pPr></w:style>` +
			`<w:style w:type="character" w:styleId="DefaultChar"><w:name w:val="Student Default Character"/></w:style>` +
			`<w:style w:type="character" w:styleId="HeaderChar"><w:name w:val="Student Header Character"/></w:style>` +
			`<w:style w:type="paragraph" w:styleId="NextStyle"><w:name w:val="Student Next"/></w:style>` +
			`<w:style w:type="paragraph" w:styleId="HeaderStyle"><w:name w:val="Student Header"/></w:style>` +
			`</w:styles>`),
		"word/theme/theme1.xml": []byte(`<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Student Theme"/>`),
		"word/fontTable.xml":    []byte(`<w:fonts xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:font w:name="Student Font"/></w:fonts>`),
	}
	if err := writeDocxEntries(templatePath, templateEntries); err != nil {
		t.Fatal(err)
	}
	if err := writeDocxEntries(outputPath, outputEntries); err != nil {
		t.Fatal(err)
	}

	if err := copyTemplateHeaderFooterPackage(templatePath, outputPath); err != nil {
		t.Fatalf("copyTemplateHeaderFooterPackage() error = %v", err)
	}

	header := readDocxEntry(t, outputPath, "word/header1.xml")
	for _, want := range []string{
		`<w:pStyle w:val="HeaderStyle_template1"/>`,
		`<w:rStyle w:val="HeaderChar_template1"/>`,
		`w:eastAsia="Template Major Hans"`,
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("copied header is missing %s: %s", want, header)
		}
	}
	for _, attribute := range []string{"asciiTheme", "hAnsiTheme", "eastAsiaTheme", "cstheme"} {
		if strings.Contains(header, attribute+"=") {
			t.Fatalf("copied header retained %s: %s", attribute, header)
		}
	}

	styles := readDocxEntry(t, outputPath, "word/styles.xml")
	for _, want := range []string{
		`w:styleId="Base"><w:name w:val="Student Base"`,
		`w:styleId="HeaderStyle_template1"`,
		`<w:basedOn w:val="Base_template1"/>`,
		`<w:link w:val="HeaderChar_template1"/>`,
		`<w:next w:val="NextStyle_template1"/>`,
		`w:ascii="Template Minor Latin"`,
		`w:eastAsia="Template Minor Hans"`,
		`w:cs="Template Minor Arabic"`,
		`<w:tabs><w:tab w:val="center" w:pos="4153"/></w:tabs>`,
	} {
		if !strings.Contains(styles, want) {
			t.Fatalf("migrated style closure is missing %s: %s", want, styles)
		}
	}
	if strings.Contains(styles, `w:asciiTheme=`) ||
		strings.Contains(styles, `w:hAnsiTheme=`) ||
		strings.Contains(styles, `w:eastAsiaTheme=`) ||
		strings.Contains(styles, `w:cstheme=`) {
		t.Fatalf("migrated styles retained template theme references: %s", styles)
	}
	migratedHeaderStyle := strictStyleDefinitions(styles)["HeaderStyle_template1"]
	if strings.Index(migratedHeaderStyle, "<w:next") > strings.Index(migratedHeaderStyle, "<w:link") ||
		strings.Index(migratedHeaderStyle, "<w:uiPriority") > strings.Index(migratedHeaderStyle, "<w:qFormat") {
		t.Fatalf("migrated style metadata is not in OOXML schema order: %s", migratedHeaderStyle)
	}
	if theme := readDocxEntry(t, outputPath, "word/theme/theme1.xml"); theme != string(outputEntries["word/theme/theme1.xml"]) {
		t.Fatalf("student global theme must not be replaced: %s", theme)
	}
	if relationships := readDocxEntry(t, outputPath, "word/_rels/document.xml.rels"); !strings.Contains(relationships, `/relationships/styles`) {
		t.Fatalf("migrated style part has no document relationship: %s", relationships)
	}
	if contentTypes := readDocxEntry(t, outputPath, "[Content_Types].xml"); !strings.Contains(contentTypes, `PartName="/word/styles.xml"`) {
		t.Fatalf("migrated style part has no content type: %s", contentTypes)
	}
	fontTable := readDocxEntry(t, outputPath, "word/fontTable.xml")
	for _, font := range []string{"Student Font", "Template Major Hans", "Template Minor Latin", "Template Minor Hans", "Template Minor Arabic"} {
		if !strings.Contains(fontTable, `w:name="`+font+`"`) {
			t.Fatalf("font table is missing %q: %s", font, fontTable)
		}
	}
}

func TestCopyTemplateHeaderFooterPackageMigratesStyleNumberingClosure(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "template-numbering.docx")
	outputPath := filepath.Join(tmpDir, "output-numbering.docx")

	templateEntries := map[string][]byte{
		"[Content_Types].xml": []byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Default Extension="png" ContentType="image/png"/>` +
			`<Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/>` +
			`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
			`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>` +
			`</Types>`),
		"word/document.xml": []byte(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body><w:sectPr><w:headerReference w:type="default" r:id="rIdHeader"/></w:sectPr></w:body></w:document>`),
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdHeader" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>` +
			`<Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
			`<Relationship Id="rIdNumbering" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>` +
			`</Relationships>`),
		"word/header1.xml": []byte(`<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:pStyle w:val="HeaderStyle"/></w:pPr><w:r><w:t>Header</w:t></w:r></w:p></w:hdr>`),
		"word/styles.xml": []byte(`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:style w:type="paragraph" w:styleId="ListBase"><w:name w:val="Template List Base"/></w:style>` +
			`<w:style w:type="paragraph" w:styleId="NumberingLinked"><w:name w:val="Template Numbering Linked"/></w:style>` +
			`<w:style w:type="paragraph" w:styleId="HeaderStyle"><w:name w:val="Template Header"/><w:basedOn w:val="ListBase"/><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="7"/></w:numPr></w:pPr></w:style>` +
			`</w:styles>`),
		"word/numbering.xml": []byte(`<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:v="urn:schemas-microsoft-com:vml">` +
			`<w:numPicBullet w:numPicBulletId="5"><w:pict><v:shape><v:imagedata r:id="rIdBullet"/></v:shape></w:pict></w:numPicBullet>` +
			`<w:abstractNum w:abstractNumId="4"><w:lvl w:ilvl="0"><w:pStyle w:val="NumberingLinked"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:lvlPicBulletId w:val="5"/></w:lvl></w:abstractNum>` +
			`<w:num w:numId="7"><w:abstractNumId w:val="4"/></w:num>` +
			`</w:numbering>`),
		"word/_rels/numbering.xml.rels":  []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdBullet" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/template-bullet.png"/></Relationships>`),
		"word/media/template-bullet.png": []byte("template bullet"),
	}
	outputEntries := map[string][]byte{
		"[Content_Types].xml": []byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/></Types>`),
		"word/document.xml":   []byte(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:sectPr/></w:body></w:document>`),
		"word/_rels/document.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
			`</Relationships>`),
		"word/styles.xml": []byte(`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:style w:type="paragraph" w:styleId="ListBase"><w:name w:val="Student List Base"/></w:style>` +
			`<w:style w:type="paragraph" w:styleId="NumberingLinked"><w:name w:val="Student Numbering Linked"/></w:style>` +
			`<w:style w:type="paragraph" w:styleId="HeaderStyle"><w:name w:val="Student Header"/></w:style>` +
			`</w:styles>`),
		"word/numbering.xml": []byte(`<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:v="urn:schemas-microsoft-com:vml">` +
			`<w:numPicBullet w:numPicBulletId="5"><w:pict><v:shape><v:imagedata r:id="rIdBullet"/></v:shape></w:pict></w:numPicBullet>` +
			`<w:abstractNum w:abstractNumId="4"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="-"/></w:lvl></w:abstractNum>` +
			`<w:num w:numId="7"><w:abstractNumId w:val="4"/></w:num>` +
			`</w:numbering>`),
		"word/_rels/numbering.xml.rels":  []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdBullet" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/template-bullet.png"/></Relationships>`),
		"word/media/template-bullet.png": []byte("student bullet"),
	}
	if err := writeDocxEntries(templatePath, templateEntries); err != nil {
		t.Fatal(err)
	}
	if err := writeDocxEntries(outputPath, outputEntries); err != nil {
		t.Fatal(err)
	}

	if err := copyTemplateHeaderFooterPackage(templatePath, outputPath); err != nil {
		t.Fatalf("copyTemplateHeaderFooterPackage() error = %v", err)
	}

	styles := strictStyleDefinitions(readDocxEntry(t, outputPath, "word/styles.xml"))
	migratedHeader := styles["HeaderStyle_template1"]
	if !strings.Contains(migratedHeader, `<w:numId w:val="1"/>`) || strings.Contains(migratedHeader, `<w:numId w:val="7"/>`) {
		t.Fatalf("migrated style numbering ID was not remapped: %s", migratedHeader)
	}
	if _, ok := styles["NumberingLinked_template1"]; !ok {
		t.Fatalf("numbering-linked style was not included in the style closure: %#v", styles)
	}

	numbering := readDocxEntry(t, outputPath, "word/numbering.xml")
	for _, want := range []string{
		`<w:abstractNum w:abstractNumId="4"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/>`,
		`<w:num w:numId="7"><w:abstractNumId w:val="4"/></w:num>`,
		`<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:pStyle w:val="NumberingLinked_template1"/>`,
		`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`,
		`<w:numPicBullet w:numPicBulletId="0">`,
		`<w:lvlPicBulletId w:val="0"/>`,
		`r:id="rId1"`,
	} {
		if !strings.Contains(numbering, want) {
			t.Fatalf("merged numbering is missing %s: %s", want, numbering)
		}
	}
	if relationships := readDocxEntry(t, outputPath, "word/_rels/document.xml.rels"); !strings.Contains(relationships, `/relationships/numbering`) {
		t.Fatalf("migrated numbering part has no document relationship: %s", relationships)
	}
	if contentTypes := readDocxEntry(t, outputPath, "[Content_Types].xml"); !strings.Contains(contentTypes, `PartName="/word/numbering.xml"`) {
		t.Fatalf("migrated numbering part has no content type: %s", contentTypes)
	}
	if relationships := readDocxEntry(t, outputPath, "word/_rels/numbering.xml.rels"); !strings.Contains(relationships, `Id="rId1"`) || !strings.Contains(relationships, `Target="media/template-bullet_template1.png"`) {
		t.Fatalf("picture-bullet relationship closure was not remapped: %s", relationships)
	}
	if got := readDocxEntry(t, outputPath, "word/media/template-bullet_template1.png"); got != "template bullet" {
		t.Fatalf("picture-bullet media was not copied: %q", got)
	}
	if contentTypes := readDocxEntry(t, outputPath, "[Content_Types].xml"); !strings.Contains(contentTypes, `Extension="png"`) {
		t.Fatalf("picture-bullet media content type was not copied: %s", contentTypes)
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
