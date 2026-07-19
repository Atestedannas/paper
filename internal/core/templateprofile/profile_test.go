package templateprofile

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeChatClient struct {
	response string
	prompt   string
}

func (f *fakeChatClient) ChatCompletion(prompt string) (string, error) {
	f.prompt = prompt
	return f.response, nil
}

func TestExtractDetectsTemplateSectionPageBreaksHeaderFooterAndStyles(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeTemplateProfileDocx(t, templatePath)

	profile, err := Extract(templatePath)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	if profile.Version != Version {
		t.Fatalf("Version = %s, want %s", profile.Version, Version)
	}
	for _, key := range []string{"body_start", "references_title", "acknowledgements_title"} {
		rule := profile.Sections[key]
		if !rule.PageBreakBefore {
			t.Fatalf("%s PageBreakBefore = false, profile=%#v", key, profile.Sections)
		}
	}
	if !profile.Header.Exists || !profile.Header.HasDoubleLine || !strings.Contains(profile.Header.Text, "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u9662") {
		t.Fatalf("header not extracted correctly: %#v", profile.Header)
	}
	if !profile.Footer.Exists || !profile.Footer.HasPageField || !profile.Footer.HasNumPages {
		t.Fatalf("footer not extracted correctly: %#v", profile.Footer)
	}
	if profile.RulePack.CitationStyle != "superscript_bracket" || profile.RulePack.ReferenceStandard != "GB/T 7714" {
		t.Fatalf("local rule pack not extracted correctly: %#v", profile.RulePack)
	}
	if profile.PageSetup.PageWidthTwips != "11906" ||
		profile.PageSetup.PageHeightTwips != "16838" ||
		profile.PageSetup.MarginTopTwips != "1701" ||
		profile.PageSetup.MarginLeftTwips != "1701" ||
		profile.PageSetup.HeaderMarginTwips != "907" ||
		profile.PageSetup.FooterMarginTwips != "851" {
		t.Fatalf("page setup not extracted correctly: %#v", profile.PageSetup)
	}
	refStyle := profile.Styles["references_title"]
	if refStyle.FontEastAsia != "宋体" || refStyle.FontSizeHalfPt != "28" || !refStyle.Bold {
		t.Fatalf("references title style not extracted: %#v", refStyle)
	}
}

func TestExtractDetectsHighNumberedHeaderFooterParts(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeDocxEntries(t, templatePath, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/header8.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/><Override PartName="/word/footer8.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/></Types>`,
		"word/document.xml":   `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>body</w:t></w:r></w:p></w:body></w:document>`,
		"word/header8.xml":    `<?xml version="1.0" encoding="UTF-8"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:pBdr><w:bottom w:val="double"/></w:pBdr></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="SimSun"/><w:sz w:val="18"/></w:rPr><w:t>High Header</w:t></w:r></w:p></w:hdr>`,
		"word/footer8.xml":    `<?xml version="1.0" encoding="UTF-8"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:instrText> NUMPAGES </w:instrText></w:r></w:p></w:ftr>`,
	})

	profile, err := Extract(templatePath)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	if !profile.Header.Exists || profile.Header.Text != "High Header" || !profile.Header.HasDoubleLine {
		t.Fatalf("high-numbered header not extracted correctly: %#v", profile.Header)
	}
	if !profile.Footer.Exists || !profile.Footer.HasPageField || !profile.Footer.HasNumPages {
		t.Fatalf("high-numbered footer not extracted correctly: %#v", profile.Footer)
	}
}

func TestExtractChoosesFooterWithTotalPagesAcrossMultipleParts(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeDocxEntries(t, templatePath, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"word/document.xml":   `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>body</w:t></w:r></w:p></w:body></w:document>`,
		"word/footer1.xml":    `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:t>2</w:t></w:r></w:p></w:ftr>`,
		"word/footer2.xml":    `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p/></w:ftr>`,
		"word/footer3.xml":    `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>第</w:t></w:r><w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:t>0页 共12页</w:t></w:r></w:p></w:ftr>`,
	})

	profile, err := Extract(templatePath)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if profile.Footer.Text != "第0页 共12页" || !profile.Footer.HasPageField || !profile.Footer.HasNumPages {
		t.Fatalf("footer = %#v, want the total-page footer", profile.Footer)
	}
}

func TestBuildAttachesDeepSeekSummary(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeTemplateProfileDocx(t, templatePath)
	client := &fakeChatClient{response: `{"sections":{"references_title":{"page_break_before":true,"evidence":"ai_confirmed"}},"styles":{"body":{"font_east_asia":"\u6977\u4f53","font_ascii":"Times New Roman","font_size_half_pt":"26","line":"420","first_line_chars":"200"},"references":{"font_east_asia":"宋体","font_size_half_pt":"21","first_line_chars":"0"}},"rule_pack":{"citation_style":"superscript_bracket","reference_standard":"GB/T 7714-2005","table_style":"three-line"},"header":{"exists":true,"has_double_line":true},"confidence":0.91}`}

	profile, err := Build(context.Background(), templatePath, Options{AIEnabled: true, AIClient: client})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if profile.Source != "local+deepseek" {
		t.Fatalf("Source = %s, want local+deepseek", profile.Source)
	}
	if profile.AI == nil || profile.AI.Error != "" || profile.AI.RawJSON == nil {
		t.Fatalf("AI summary not attached: %#v", profile.AI)
	}
	if profile.Styles["body"].FontEastAsia != "\u6977\u4f53" || profile.Styles["body"].Line != "420" {
		t.Fatalf("AI styles should merge into profile styles: %#v", profile.Styles["body"])
	}
	if !profile.Sections["references_title"].PageBreakBefore || profile.Sections["references_title"].DetectedFrom != "ai_confirmed" {
		t.Fatalf("AI sections should merge into profile sections: %#v", profile.Sections["references_title"])
	}
	if profile.Confidence != 0.91 {
		t.Fatalf("Confidence = %v, want 0.91", profile.Confidence)
	}
	if profile.RulePack.CitationStyle != "superscript_bracket" ||
		profile.RulePack.ReferenceStandard != "GB/T 7714-2005" ||
		profile.RulePack.TableStyle != "three-line" {
		t.Fatalf("AI rule pack should merge into profile: %#v", profile.RulePack)
	}
	for _, want := range []string{
		"\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587 DOCX \u6a21\u677f\u683c\u5f0f\u89c4\u8303\u89e3\u6790\u4e13\u5bb6",
		"\u7ae0\u8282\u53e6\u8d77页",
		"references_title",
		"acknowledgements_title",
		"页\u7709页\u811a",
		"\u6837\u5f0f\u753b\u50cf",
		"\u672c\u5730\u89e3\u6790 JSON",
	} {
		if !strings.Contains(client.prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, client.prompt)
		}
	}
}

func TestParseRoundTrip(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeTemplateProfileDocx(t, templatePath)
	profile, err := Extract(templatePath)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	parsed, err := Parse(Marshal(profile))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.TemplateSHA != profile.TemplateSHA {
		t.Fatalf("TemplateSHA = %s, want %s", parsed.TemplateSHA, profile.TemplateSHA)
	}
}

func TestBuildMergesRulePackSidecar(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeTemplateProfileDocx(t, templatePath)
	rules := `{"rule_pack":{"citation_style":"superscript_bracket","reference_standard":"GB/T 7714-2005","table_style":"three-line","required_sections":["cover","title_page","abstract_cn","abstract_en","toc","body","references","acknowledgements"],"required_fields":["\u5206\u7c7b\u53f7","\u5b66\u6821\u4ee3\u7801","UDC","\u5bc6\u7ea7","\u4f5c\u8005","\u6307\u5bfc\u6559\u5e08"],"title_max_cn_chars":25,"title_max_en_words":10,"keyword_min":3,"keyword_max":5,"heading_numbering":"arabic","body_min_chars":30000,"figure_numbering":"chapter","table_numbering":"chapter","formula_numbering":"chapter","reference_min_count":20,"reference_foreign_ratio_min":0.3333,"header_policy":"odd_even","odd_header_text":"chapter","even_header_text":"university thesis","header_line":"single_0_75pt","page_numbering":"front_roman_body_arabic_center","front_page_format":"lowerRoman","body_page_format":"decimal","body_page_start":1,"body_page_wrapper":"dash","heading_levels":["第1\u7ae0","1.1","1.1.1"],"figure_caption_position":"below","table_caption_position":"above","caption_style_key":"caption","reference_style":"author_year","blind_review":true}}`
	if err := os.WriteFile(templatePath+".rules.json", []byte(rules), 0644); err != nil {
		t.Fatalf("write sidecar rule pack: %v", err)
	}

	profile, err := Build(context.Background(), templatePath, Options{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if profile.RulePack.CitationStyle != "superscript_bracket" ||
		profile.RulePack.ReferenceStandard != "GB/T 7714-2005" ||
		profile.RulePack.TableStyle != "three-line" {
		t.Fatalf("sidecar rule pack not merged: %#v", profile.RulePack)
	}
	if len(profile.RulePack.RequiredSections) != 8 ||
		len(profile.RulePack.RequiredFields) != 6 ||
		profile.RulePack.TitleMaxCNChars != 25 ||
		profile.RulePack.TitleMaxENWords != 10 ||
		profile.RulePack.KeywordMin != 3 ||
		profile.RulePack.KeywordMax != 5 ||
		profile.RulePack.HeadingNumbering != "arabic" ||
		profile.RulePack.BodyMinChars != 30000 ||
		profile.RulePack.FigureNumbering != "chapter" ||
		profile.RulePack.TableNumbering != "chapter" ||
		profile.RulePack.FormulaNumbering != "chapter" ||
		profile.RulePack.ReferenceMinCount != 20 ||
		profile.RulePack.ReferenceForeignRatioMin != 0.3333 ||
		profile.RulePack.HeaderPolicy != "odd_even" ||
		profile.RulePack.OddHeaderText != "chapter" ||
		profile.RulePack.EvenHeaderText != "university thesis" ||
		profile.RulePack.HeaderLine != "single_0_75pt" ||
		profile.RulePack.PageNumbering != "front_roman_body_arabic_center" ||
		profile.RulePack.FrontPageFormat != "lowerRoman" ||
		profile.RulePack.BodyPageFormat != "decimal" ||
		profile.RulePack.BodyPageStart != 1 ||
		profile.RulePack.BodyPageWrapper != "dash" ||
		len(profile.RulePack.HeadingLevels) != 3 ||
		profile.RulePack.FigureCaptionPosition != "below" ||
		profile.RulePack.TableCaptionPosition != "above" ||
		profile.RulePack.CaptionStyleKey != "caption" ||
		profile.RulePack.ReferenceStyle != "author_year" ||
		!profile.RulePack.BlindReview {
		t.Fatalf("expanded sidecar rule pack not merged: %#v", profile.RulePack)
	}
}

func writeTemplateProfileDocx(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/><Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/></Types>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:pPr><w:sectPr><w:type w:val="nextPage"/><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1701" w:right="1417" w:bottom="1417" w:left="1701" w:header="907" w:footer="851"/></w:sectPr></w:pPr><w:r><w:t>封面</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:rPr><w:rFonts w:eastAsia="黑体" w:ascii="Times New Roman"/><w:sz w:val="30"/><w:b/></w:rPr></w:pPr><w:r><w:t>摘要：</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:type w:val="nextPage"/></w:sectPr></w:pPr><w:r><w:t>目录</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pageBreakBefore/><w:spacing w:beforeLines="100" w:afterLines="100" w:line="360"/><w:rPr><w:rFonts w:eastAsia="宋体" w:ascii="Times New Roman"/><w:sz w:val="32"/><w:b/></w:rPr></w:pPr><w:r><w:t>1 绪论</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:spacing w:line="360"/><w:ind w:firstLineChars="200"/><w:rPr><w:rFonts w:eastAsia="宋体" w:ascii="Times New Roman"/><w:sz w:val="24"/></w:rPr></w:pPr><w:r><w:t>正文。</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>文献引用：按照其在正文中出现的先后顺序以方括号加阿拉伯数字连续编码，如[1]、[2]，以上标形式进行标注。GB7714-2015</w:t></w:r></w:p>` +
			`<w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
			`<w:p><w:pPr><w:jc w:val="center"/><w:rPr><w:rFonts w:eastAsia="宋体" w:ascii="Times New Roman"/><w:sz w:val="28"/><w:b/></w:rPr></w:pPr><w:r><w:t>参考文献</w:t></w:r></w:p>` +
			`<w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
			`<w:p><w:pPr><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="24"/></w:rPr></w:pPr><w:r><w:t>致谢</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
		"word/header1.xml": `<?xml version="1.0" encoding="UTF-8"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:pBdr><w:bottom w:val="double"/></w:pBdr></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="18"/></w:rPr><w:t>重庆人文科技学院2026届护理学专业本科毕业论文</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml": `<?xml version="1.0" encoding="UTF-8"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>第</w:t></w:r><w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:t>页 \u5171</w:t></w:r><w:r><w:instrText> NUMPAGES </w:instrText></w:r><w:r><w:t>页</w:t></w:r></w:p></w:ftr>`,
	}
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write entry %s: %v", name, err)
		}
	}
}

func writeDocxEntries(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write entry %s: %v", name, err)
		}
	}
}
