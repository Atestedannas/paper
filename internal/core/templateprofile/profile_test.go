package templateprofile

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

type fakeChatClient struct {
	response string
	prompt   string
}

func TestCollectParagraphsIgnoresNestedTextBoxAnnotations(t *testing.T) {
	documentXML := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:drawing><w:txbxContent><w:p><w:r><w:t>三号黑体，居中</w:t></w:r></w:p></w:txbxContent></w:drawing></w:r>` +
		`<w:r><w:rPr><w:rFonts w:eastAsia="黑体"/><w:sz w:val="32"/></w:rPr><w:t>1 绪论</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>正文</w:t></w:r></w:p></w:body></w:document>`

	paragraphs := collectParagraphs(documentXML)
	if len(paragraphs) != 2 {
		t.Fatalf("collectParagraphs() count = %d, want 2", len(paragraphs))
	}
	if paragraphs[0].Text != "1 绪论" || strings.Contains(paragraphs[0].XML, "三号黑体") {
		t.Fatalf("nested annotation polluted paragraph: text=%q xml=%s", paragraphs[0].Text, paragraphs[0].XML)
	}
	style := extractStyle("heading_1", paragraphs[0].XML)
	if style.FontEastAsia != "黑体" || style.FontSizeHalfPt != "32" {
		t.Fatalf("heading style = %+v", style)
	}
}

func TestExtractDoesNotMixMultipleEmbeddedExamplePapers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multi-example.docx")
	documentXML := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:t>摘  要</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:spacing w:line="400" w:lineRule="exact"/><w:ind w:firstLine="480"/></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="24"/></w:rPr><w:t>这是第一套示例论文的摘要正文，用于提取稳定的摘要正文格式。</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>` +
		`<w:p><w:r><w:rPr><w:rFonts w:eastAsia="黑体"/><w:sz w:val="28"/></w:rPr><w:t>1.1.1 第一套三级标题</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>致  谢</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>摘  要</w:t></w:r></w:p>` +
		`<w:p><w:r><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="40"/><w:b/></w:rPr><w:t>1.1.1 第二套冲突标题</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	writeDocxEntries(t, path, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"word/document.xml":   documentXML,
	})

	profile, err := Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	heading := profile.Styles["heading_3"]
	if heading.FontEastAsia != "黑体" || heading.FontSizeHalfPt != "28" || heading.Bold {
		t.Fatalf("later embedded example polluted heading_3: %#v", heading)
	}
}

func TestExtractResolvesParagraphStyleInheritance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "style-inheritance.docx")
	writeDocxEntries(t, path, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
			`</Types>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>&#25688;&#35201;</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="HeadingCustom"/></w:pPr><w:r><w:t>1 Introduction</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
		"word/styles.xml": `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:eastAsia="DocDefaultFont" w:ascii="DefaultASCII"/><w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>` +
			`<w:style w:type="paragraph" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
			`<w:style w:type="paragraph" w:styleId="HeadingBase"><w:basedOn w:val="Normal"/><w:pPr><w:jc w:val="center"/></w:pPr></w:style>` +
			`<w:style w:type="paragraph" w:styleId="HeadingCustom"><w:basedOn w:val="HeadingBase"/><w:rPr><w:rFonts w:eastAsia="InheritedHeadingFont"/><w:sz w:val="30"/></w:rPr></w:style>` +
			`</w:styles>`,
	})

	profile, err := Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	heading := profile.Styles["heading_1"]
	if heading.FontEastAsia != "InheritedHeadingFont" || heading.FontASCII != "DefaultASCII" ||
		heading.FontSizeHalfPt != "30" || heading.Alignment != "center" {
		t.Fatalf("inherited heading style = %#v", heading)
	}
}

func TestExtractResolvesAllThemeFontSlotsAndCharacterStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "theme-fonts.docx")
	writeDocxEntries(t, path, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>&#25688;&#35201;</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="HeadingCustom"/></w:pPr><w:r><w:rPr><w:rStyle w:val="EmphasisEast"/></w:rPr><w:t>1 Introduction</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
		"word/styles.xml": `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:docDefaults><w:rPrDefault><w:rPr>` +
			`<w:rFonts w:asciiTheme="minorAscii" w:hAnsiTheme="minorHAnsi" w:eastAsiaTheme="minorEastAsia" w:cstheme="minorBidi"/>` +
			`<w:lang w:val="en-US" w:eastAsia="zh-CN" w:bidi="ar-SA"/>` +
			`</w:rPr></w:rPrDefault></w:docDefaults>` +
			`<w:style w:type="paragraph" w:styleId="Normal"><w:name w:val="Normal"/><w:rPr><w:rFonts w:ascii="ParagraphAscii"/></w:rPr></w:style>` +
			`<w:style w:type="paragraph" w:styleId="HeadingCustom"><w:basedOn w:val="Normal"/><w:rPr><w:sz w:val="30"/></w:rPr></w:style>` +
			`<w:style w:type="character" w:styleId="EmphasisEast"><w:rPr><w:rFonts w:eastAsiaTheme="majorEastAsia"/></w:rPr></w:style>` +
			`</w:styles>`,
		"word/theme/theme1.xml": `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:fontScheme>` +
			`<a:majorFont><a:latin typeface="MajorLatin"/><a:ea typeface=""/><a:cs typeface=""/><a:font script="Hans" typeface="MajorHans"/><a:font script="Arab" typeface="MajorArab"/></a:majorFont>` +
			`<a:minorFont><a:latin typeface="MinorLatin"/><a:ea typeface=""/><a:cs typeface=""/><a:font script="Hans" typeface="MinorHans"/><a:font script="Arab" typeface="MinorArab"/></a:minorFont>` +
			`</a:fontScheme></a:themeElements></a:theme>`,
	})

	profile, err := Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stylesXML, _ := pkg.Get("word/styles.xml")
	resolver := extractThemeFontResolver(pkg, string(stylesXML))
	definitions, _ := extractStyleDefinitions(materializeThemeFonts(string(stylesXML), resolver))
	if charStyle := definitions["EmphasisEast"]; charStyle.FontEastAsia != "MajorHans" || charStyle.FontASCII != "" {
		t.Fatalf("character style definition must keep undefined slots: %#v", charStyle)
	}
	documentXML, _ := pkg.Get("word/document.xml")
	paragraphs := collectParagraphs(materializeThemeFonts(string(documentXML), resolver))
	base := extractStyleWithDefinitions("heading_1", paragraphs[1].XML, definitions)
	effective := extractRepresentativeRunStyleWithDefinitions("heading_1", paragraphs[1].XML, base, definitions)
	if effective.FontEastAsia != "MajorHans" {
		t.Fatalf("representative character style not applied: base=%#v effective=%#v paragraph=%s", base, effective, paragraphs[1].XML)
	}
	got := profile.Styles["heading_1"]
	if got.FontASCII != "ParagraphAscii" || got.FontASCIITheme != "" ||
		got.FontHAnsi != "MinorLatin" || got.FontHAnsiTheme != "minorHAnsi" ||
		got.FontEastAsia != "MajorHans" || got.FontEastAsiaTheme != "majorEastAsia" ||
		got.FontCS != "MinorArab" || got.FontCSTheme != "minorBidi" {
		t.Fatalf("effective themed fonts = %#v", got)
	}
	if got.SampleCount != 1 || got.Confidence != 1 || len(got.Sources) != 1 ||
		got.Sources[0].Part != "word/document.xml" || got.Sources[0].ParagraphIndex != 2 ||
		got.Sources[0].ParagraphStyleID != "HeadingCustom" || got.Sources[0].RunStyleID != "EmphasisEast" ||
		strings.Join(got.Sources[0].InheritanceChain, "/") != "Normal/HeadingCustom/EmphasisEast" {
		t.Fatalf("style provenance = %#v", got)
	}
}

func TestExtractCascadesNumberingLevelBetweenParagraphStyleAndInheritedDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "numbering-style-cascade.docx")
	writeDocxEntries(t, path, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>&#25688;&#35201;</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="HeadingCustom"/><w:numPr><w:ilvl w:val="2"/></w:numPr><w:ind w:firstLine="480"/></w:pPr>` +
			`<w:r><w:rPr><w:rStyle w:val="CharCustom"/><w:rFonts w:eastAsia="DirectEast"/><w:i w:val="0"/></w:rPr><w:t>1 Introduction</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
		"word/styles.xml": `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:eastAsia="DefaultEast" w:ascii="DefaultAscii" w:hAnsi="DefaultHAnsi" w:cs="DefaultCS"/><w:sz w:val="18"/><w:lang w:bidi="ar-SA"/></w:rPr></w:rPrDefault></w:docDefaults>` +
			`<w:style w:type="paragraph" w:styleId="Normal"><w:name w:val="Normal"/><w:rPr><w:rFonts w:eastAsia="InheritedEast"/></w:rPr></w:style>` +
			`<w:style w:type="paragraph" w:styleId="Base"><w:basedOn w:val="Normal"/><w:pPr><w:numPr><w:numId w:val="42"/></w:numPr><w:jc w:val="right"/><w:spacing w:line="300"/></w:pPr><w:rPr><w:sz w:val="20"/></w:rPr></w:style>` +
			`<w:style w:type="paragraph" w:styleId="HeadingCustom"><w:basedOn w:val="Base"/><w:pPr><w:jc w:val="left"/></w:pPr><w:rPr><w:rFonts w:hAnsi="ParagraphHAnsi"/><w:b w:val="0"/></w:rPr></w:style>` +
			`<w:style w:type="character" w:styleId="CharCustom"><w:rPr><w:rFonts w:ascii="CharacterAscii"/></w:rPr></w:style>` +
			`</w:styles>`,
		"word/numbering.xml": `<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:abstractNum w:abstractNumId="9"><w:lvl w:ilvl="2"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:pStyle w:val="HeadingCustom"/><w:lvlText w:val="%1.%2.%3"/>` +
			`<w:pPr><w:jc w:val="center"/><w:spacing w:line="400" w:lineRule="exact"/><w:ind w:firstLine="200"/></w:pPr>` +
			`<w:rPr><w:rFonts w:cstheme="majorBidi"/><w:sz w:val="28"/><w:b/><w:i/></w:rPr></w:lvl></w:abstractNum>` +
			`<w:num w:numId="42"><w:abstractNumId w:val="9"/></w:num></w:numbering>`,
		"word/theme/theme1.xml": `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:fontScheme>` +
			`<a:majorFont><a:latin typeface="MajorLatin"/><a:ea typeface=""/><a:cs typeface=""/><a:font script="Arab" typeface="MajorArabic"/></a:majorFont>` +
			`<a:minorFont><a:latin typeface="MinorLatin"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont>` +
			`</a:fontScheme></a:themeElements></a:theme>`,
	})

	profile, err := Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	got := profile.Styles["heading_1"]
	if got.FontEastAsia != "DirectEast" || got.FontASCII != "CharacterAscii" ||
		got.FontHAnsi != "ParagraphHAnsi" || got.FontCS != "MajorArabic" || got.FontCSTheme != "majorBidi" {
		t.Fatalf("four-slot precedence = %#v", got)
	}
	if got.Alignment != "left" || got.Line != "400" || got.LineRule != "exact" ||
		got.FirstLineTwips != "480" || got.FontSizeHalfPt != "28" {
		t.Fatalf("paragraph/numbering precedence = %#v", got)
	}
	if !got.BoldSet || got.Bold || !got.ItalicSet || got.Italic {
		t.Fatalf("tri-state precedence = %#v", got)
	}
	wantChain := "Normal/Base/numbering:9:2/HeadingCustom/CharCustom"
	if chain := strings.Join(got.InheritanceChain, "/"); chain != wantChain {
		t.Fatalf("inheritance chain = %q, want %q", chain, wantChain)
	}
	level := profile.Numbering.AbstractNums[9][0].Style
	if level.Alignment != "center" || level.Line != "400" || level.FontCS != "MajorArabic" ||
		!level.BoldSet || !level.Bold || !level.ItalicSet || !level.Italic {
		t.Fatalf("numbering level pPr/rPr = %#v", level)
	}
}

func TestThemeReferenceSelectsItsDeclaredCharacterRange(t *testing.T) {
	resolver := themeFontResolver{
		Major:          themeFontFamily{Latin: "MajorLatin", Scripts: map[string]string{"Hans": "MajorHans"}},
		Minor:          themeFontFamily{Latin: "MinorLatin", Scripts: map[string]string{"Hans": "MinorHans"}},
		EastAsiaScript: "Hans",
	}
	style := extractStyle("body", materializeThemeFonts(
		`<w:rPr><w:rFonts w:asciiTheme="minorEastAsia" w:cstheme="majorAscii"/></w:rPr>`,
		resolver,
	))
	if style.FontASCII != "MinorHans" || style.FontCS != "MajorLatin" {
		t.Fatalf("cross-range theme references = %#v", style)
	}
}

func TestExtractSamplesTOCStylesWithoutTreatingEntriesAsBodyHeadings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "toc-styles.docx")
	writeDocxEntries(t, path, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>&#25688;&#35201;</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>&#30446;&#24405;</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="TOCLevel1"/></w:pPr><w:r><w:t>1 Introduction2</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="TOCLevel2"/></w:pPr><w:r><w:t>1.1 Background3</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="HeadingActual"/><w:pageBreakBefore/></w:pPr><w:r><w:t>1 Introduction</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
		"word/styles.xml": `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:style w:type="paragraph" w:styleId="TOCLevel1"><w:name w:val="toc 1"/><w:rPr><w:rFonts w:eastAsia="TOCFont"/><w:sz w:val="20"/></w:rPr></w:style>` +
			`<w:style w:type="paragraph" w:styleId="TOCLevel2"><w:name w:val="toc 2"/><w:rPr><w:rFonts w:eastAsia="TOCFont"/><w:sz w:val="18"/></w:rPr></w:style>` +
			`<w:style w:type="paragraph" w:styleId="HeadingActual"><w:name w:val="heading 1"/><w:rPr><w:rFonts w:eastAsia="HeadingFont"/><w:sz w:val="30"/></w:rPr></w:style>` +
			`</w:styles>`,
	})

	profile, err := Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := profile.Styles["toc_entry"]; got.FontEastAsia != "TOCFont" || got.FontSizeHalfPt != "20" {
		t.Fatalf("toc_entry style = %#v", got)
	}
	if got := profile.Styles["toc_entry_2"]; got.FontSizeHalfPt != "18" {
		t.Fatalf("toc_entry_2 style = %#v", got)
	}
	if got := profile.Styles["heading_1"]; got.FontEastAsia != "HeadingFont" || got.FontSizeHalfPt != "30" {
		t.Fatalf("real heading style was polluted by TOC entries: %#v", got)
	}
}

func TestExtractSamplesCaptionFormattingFromCenteredCaptionParagraphs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "caption-styles.docx")
	writeDocxEntries(t, path, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>&#25688;&#35201;</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>Keywords: sample</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:line="300"/></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="CaptionFont"/><w:sz w:val="21"/></w:rPr><w:t>&#22270;1.1 Architecture</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:line="320"/></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="TableCaptionFont"/><w:sz w:val="22"/></w:rPr><w:t>&#34920;1.1Data</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:jc w:val="both"/></w:pPr><w:r><w:rPr><w:sz w:val="24"/></w:rPr><w:t>&#22270;1.2describes ordinary body content.</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})

	profile, err := Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := profile.Styles["figure_caption"]; got.FontEastAsia != "CaptionFont" || got.FontSizeHalfPt != "21" || got.Alignment != "center" {
		t.Fatalf("figure caption style = %#v", got)
	}
	if got := profile.Styles["table_caption"]; got.FontEastAsia != "TableCaptionFont" || got.FontSizeHalfPt != "22" || got.Line != "320" {
		t.Fatalf("table caption style = %#v", got)
	}
	if profile.Styles["body"].FontSizeHalfPt != "24" {
		t.Fatalf("ordinary body paragraph was not kept separate: %#v", profile.Styles["body"])
	}
}

func TestExtractDoesNotSampleChapterSummarySentencesAsHeadings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chapter-summary.docx")
	writeDocxEntries(t, path, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>&#25688;&#35201;</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>Keywords: sample</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="HeadingFont"/><w:sz w:val="32"/></w:rPr><w:t>1 Introduction</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:jc w:val="both"/></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="BodyFont"/><w:sz w:val="24"/></w:rPr><w:t>` +
			"\u7b2c\u4e00\u7ae0 This sentence explains the chapter in ordinary body prose." +
			`</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})

	profile, err := Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := profile.Styles["heading_1"]; got.FontEastAsia != "HeadingFont" || got.FontSizeHalfPt != "32" {
		t.Fatalf("heading style was polluted by a chapter summary sentence: %#v", got)
	}
	if got := profile.Styles["body"]; got.FontEastAsia != "BodyFont" || got.FontSizeHalfPt != "24" {
		t.Fatalf("chapter summary sentence was not sampled as body: %#v", got)
	}
}

func TestRealTemplateProfileCandidates(t *testing.T) {
	path := os.Getenv("PAPER_TEMPLATE_DOCX")
	if path == "" {
		t.Skip("set PAPER_TEMPLATE_DOCX")
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	documentXML, ok := pkg.Get("word/document.xml")
	if !ok {
		t.Fatal("word/document.xml missing")
	}
	patterns := map[string]*regexp.Regexp{}
	if numbering, err := parseNumberingXML(pkg); err == nil && numbering != nil {
		patterns = numbering.BuildHeadingPatterns()
	}
	for index, para := range collectParagraphs(string(documentXML)) {
		key := classifyParagraphNumberingAware(para.Text, patterns)
		if key == "" {
			continue
		}
		style := extractStyle(key, para.XML)
		t.Logf("%d key=%s text=%q style=%+v", index, key, shortProfileText(para.Text), style)
	}
}

func shortProfileText(text string) string {
	runes := []rune(text)
	if len(runes) > 50 {
		runes = runes[:50]
	}
	return string(runes)
}

func TestExtractCollegeName(t *testing.T) {
	for _, test := range []struct {
		header   string
		fallback string
		want     string
	}{
		{"重庆工程学院本科生毕业设计（论文）", "重庆人文科技学院", "重庆工程学院"},
		{"页眉：重庆人文科技学院2026届护理学专业本科毕业论文", "重庆工程学院", "重庆人文科技学院"},
		{"Undergraduate Thesis", "重庆工程学院", "重庆工程学院"},
	} {
		if got := ExtractCollegeName(test.header, test.fallback); got != test.want {
			t.Fatalf("ExtractCollegeName(%q) = %q, want %q", test.header, got, test.want)
		}
	}
}

func TestExtractLeadingLabelRunStyleOverridesParagraphDefault(t *testing.T) {
	paragraph := `<w:p><w:pPr><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="24"/></w:rPr><w:spacing w:after="624"/></w:pPr>` +
		`<w:r><w:rPr><w:rFonts w:eastAsia="黑体"/><w:sz w:val="30"/><w:b/></w:rPr><w:t>摘要：</w:t></w:r>` +
		`<w:r><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="24"/></w:rPr><w:t>正文</w:t></w:r></w:p>`
	base := extractStyle("abstract_cn", paragraph)
	got := extractLeadingLabelRunStyle("abstract_cn", paragraph, base)
	if got.FontEastAsia != "黑体" || got.FontSizeHalfPt != "30" || !got.Bold || got.AfterTwips != "624" {
		t.Fatalf("leading label run style not extracted: %#v", got)
	}
}

func TestSectionFormatMapSeparatesAbstractBody(t *testing.T) {
	paragraph := `<w:p>` +
		`<w:r><w:rPr><w:rFonts w:eastAsia="黑体"/><w:sz w:val="30"/><w:b/></w:rPr><w:t>摘要：</w:t></w:r>` +
		`<w:r><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="24"/></w:rPr><w:t>正文内容</w:t></w:r></w:p>`
	body, ok := extractTrailingContentRunStyle("abstract_body", paragraph)
	if !ok || body.FontEastAsia != "宋体" || body.FontSizeHalfPt != "24" {
		t.Fatalf("abstract body style = %#v, ok=%v", body, ok)
	}
	sections := buildSectionFormatMap(map[string]StyleRule{
		"heading_1":     {FontSizeHalfPt: "32"},
		"body":          {FontSizeHalfPt: "24"},
		"abstract_body": body,
	})
	if sections["chapter_title"].FontSizeHalfPt != "32" ||
		sections["body_text"].FontSizeHalfPt != "24" ||
		sections["abstract_body"].FontEastAsia != "宋体" {
		t.Fatalf("section formats = %#v", sections)
	}
}

func TestExtractTrailingContentRunStyleSupportsAllFrontMatterLabels(t *testing.T) {
	paragraph := `<w:p>` +
		`<w:r><w:rPr><w:rFonts w:eastAsia="黑体"/><w:b/></w:rPr><w:t>关键词：</w:t></w:r>` +
		`<w:r><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="24"/></w:rPr><w:t>格式；模板</w:t></w:r></w:p>`

	body, ok := extractTrailingContentRunStyle("keywords_cn_body", paragraph)
	if !ok || body.FontEastAsia != "宋体" || body.FontSizeHalfPt != "24" {
		t.Fatalf("keyword body style = %#v, ok=%v", body, ok)
	}
}

func TestExtractStyleReadsNamedStyleRunProperties(t *testing.T) {
	styleXML := `<w:style w:type="paragraph" w:styleId="1"><w:name w:val="heading 1"/>` +
		`<w:pPr><w:jc w:val="left"/></w:pPr><w:rPr><w:b/><w:i/><w:sz w:val="32"/></w:rPr></w:style>`
	got := extractStyle("heading_1", styleXML)
	if !got.BoldSet || !got.Bold || !got.ItalicSet || !got.Italic || got.FontSizeHalfPt != "32" {
		t.Fatalf("named style run properties not extracted: %#v", got)
	}
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

func TestExtractStylePreservesAdvancedOOXMLProperties(t *testing.T) {
	style := extractStyle("body", `<w:p><w:pPr><w:outlineLvl w:val="2"/><w:ind w:firstLine="480"/><w:rPr><w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hint="eastAsia"/><w:sz w:val="24"/><w:szCs w:val="22"/><w:i/></w:rPr></w:pPr><w:r><w:t>Text</w:t></w:r></w:p>`)
	if style.ComplexSizeHalfPt != "22" || style.FontHint != "eastAsia" || style.FirstLineTwips != "480" || style.OutlineLevel != "2" || !style.ItalicSet || !style.Italic {
		t.Fatalf("style = %#v", style)
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

func TestExtractDetectsChineseChapterAndPageBreakAcrossBlankParagraphs(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeDocxEntries(t, templatePath, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
			`<w:p><w:r><w:t></w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:spacing w:before="240" w:after="120" w:line="400" w:lineRule="exact"/><w:rPr><w:rFonts w:eastAsia="SimSun"/><w:sz w:val="32"/><w:b/></w:rPr></w:pPr><w:r><w:t>第一章 绪论</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})

	profile, err := Extract(templatePath)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	rule := profile.Sections["body_start"]
	if !rule.PageBreakBefore || rule.DetectedFrom != "previous_paragraph" {
		t.Fatalf("body_start page break not detected across blank paragraphs: %#v", rule)
	}
	if style := profile.Styles["body_start"]; style.FontEastAsia != "SimSun" || style.FontSizeHalfPt != "32" || !style.Bold || style.LineRule != "exact" || style.BeforeTwips != "240" || style.AfterTwips != "120" {
		t.Fatalf("Chinese chapter style not extracted: %#v", style)
	}
	if style := profile.Styles["heading_1"]; style.FontEastAsia != "SimSun" || style.FontSizeHalfPt != "32" {
		t.Fatalf("first chapter style should contribute to heading_1: %#v", style)
	}
}

func TestAggregateStyleRulesRequiresBoldSupermajority(t *testing.T) {
	style := aggregateStyleRules("heading_1", []StyleRule{{Bold: true, BoldSet: true}, {Bold: false, BoldSet: true}})
	if style.Bold {
		t.Fatal("50 percent bold samples should not produce a bold aggregate")
	}
}

func TestExtractStyleLimitsBoldToRepresentativeRunProperties(t *testing.T) {
	raw := `<w:p><w:pPr><w:rPr><w:b w:val="0"/></w:rPr></w:pPr><w:r><w:rPr><w:b/></w:rPr><w:t>mixed</w:t></w:r></w:p>`
	if style := extractStyle("body", raw); style.Bold {
		t.Fatalf("later bold run should not override paragraph style: %#v", style)
	}
}

func TestApplyFormatRulesSupportsExplicitIndentAndSpacingUnits(t *testing.T) {
	profile := &Profile{Styles: map[string]StyleRule{}}
	err := ApplyFormatRules(profile, `{"body":{"first_line_indent":0.74,"first_line_indent_unit":"cm","paragraph_before_twips":240,"paragraph_after_twips":120}}`)
	if err != nil {
		t.Fatalf("ApplyFormatRules() error = %v", err)
	}
	style := profile.Styles["body_start"]
	if style.FirstLineChars != "200" || style.BeforeTwips != "240" || style.AfterTwips != "120" {
		t.Fatalf("explicit units not converted correctly: %#v", style)
	}
}

func TestApplyFormatRulesSupportsFixedLineSpacing(t *testing.T) {
	profile := &Profile{Styles: map[string]StyleRule{"body_start": {}}}
	err := ApplyFormatRules(profile, `{"body":{"line_space":"fixed","line_space_value":20}}`)
	if err != nil {
		t.Fatal(err)
	}
	style := profile.Styles["body_start"]
	if style.Line != "400" || style.LineRule != "exact" {
		t.Fatalf("fixed line spacing = %#v", style)
	}
}

func TestClassifyParagraphDoesNotTreatSubheadingAsBodyStart(t *testing.T) {
	if got := classifyParagraph("1.1 研究背景"); got != "heading_2" {
		t.Fatalf("classifyParagraph() = %q, want heading_2", got)
	}
}

func TestExtractAggregatesRepeatedStylesByMode(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	paragraph := func(font, size string) string {
		return `<w:p><w:pPr><w:rPr><w:rFonts w:eastAsia="` + font + `"/><w:sz w:val="` + size + `"/></w:rPr></w:pPr><w:r><w:t>参考文献</w:t></w:r></w:p>`
	}
	writeDocxEntries(t, templatePath, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"word/document.xml":   `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + paragraph("SimSun", "24") + paragraph("KaiTi", "22") + paragraph("SimSun", "24") + `</w:body></w:document>`,
	})

	profile, err := Extract(templatePath)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if style := profile.Styles["references_title"]; style.FontEastAsia != "SimSun" || style.FontSizeHalfPt != "24" {
		t.Fatalf("aggregated style = %#v, want modal font and size", style)
	}
}

func TestBuildAllowsAIToCorrectLocalSectionAndBold(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeTemplateProfileDocx(t, templatePath)
	client := &fakeChatClient{response: `{"sections":{"references_title":{"page_break_before":false,"evidence":"ai_corrected"}},"styles":{"references_title":{"bold":false,"font_size_half_pt":"30"}},"confidence":0.91}`}

	profile, err := Build(context.Background(), templatePath, Options{AIEnabled: true, AIClient: client})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if profile.Sections["references_title"].PageBreakBefore || profile.Sections["references_title"].DetectedFrom != "ai_corrected" {
		t.Fatalf("AI section correction not applied: %#v", profile.Sections["references_title"])
	}
	if style := profile.Styles["references_title"]; style.Bold || style.FontSizeHalfPt != "30" {
		t.Fatalf("AI style correction not applied: %#v", style)
	}
}

func TestBuildKeepsLocalStyleWhenAIConfidenceIsLower(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeTemplateProfileDocx(t, templatePath)
	client := &fakeChatClient{response: `{"styles":{"references_title":{"bold":false,"font_size_half_pt":"30"}},"confidence":0.5}`}

	profile, err := Build(context.Background(), templatePath, Options{AIEnabled: true, AIClient: client})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if style := profile.Styles["references_title"]; !style.Bold || style.FontSizeHalfPt == "30" {
		t.Fatalf("low-confidence AI overrode local style: %#v", style)
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

func TestExtractKeepsFirstAndEvenHeaderFooterVariants(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeDocxEntries(t, templatePath, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body><w:sectPr>` +
			`<w:headerReference w:type="default" r:id="rId1"/><w:headerReference w:type="first" r:id="rId2"/><w:headerReference w:type="even" r:id="rId3"/>` +
			`<w:footerReference w:type="first" r:id="rId4"/><w:footerReference w:type="even" r:id="rId5"/>` +
			`</w:sectPr></w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships><Relationship Id="rId1" Target="header1.xml"/><Relationship Id="rId2" Target="header2.xml"/><Relationship Id="rId3" Target="header3.xml"/><Relationship Id="rId4" Target="footer1.xml"/><Relationship Id="rId5" Target="footer2.xml"/></Relationships>`,
		"word/header1.xml":             `<w:hdr><w:p><w:r><w:t>odd header</w:t></w:r></w:p></w:hdr>`,
		"word/header2.xml":             `<w:hdr><w:p><w:r><w:t>first header</w:t></w:r></w:p></w:hdr>`,
		"word/header3.xml":             `<w:hdr><w:p><w:r><w:t>even header</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml":             `<w:ftr><w:p><w:r><w:t>first footer</w:t></w:r></w:p></w:ftr>`,
		"word/footer2.xml":             `<w:ftr><w:p><w:r><w:t>even footer</w:t></w:r></w:p></w:ftr>`,
	})

	profile, err := Extract(templatePath)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if profile.Header.Text != "odd header" || profile.HeaderFirst.Text != "first header" || profile.HeaderEven.Text != "even header" {
		t.Fatalf("header variants collapsed: %#v %#v %#v", profile.Header, profile.HeaderFirst, profile.HeaderEven)
	}
	if profile.FooterFirst.Text != "first footer" || profile.FooterEven.Text != "even footer" {
		t.Fatalf("footer variants collapsed: %#v %#v", profile.FooterFirst, profile.FooterEven)
	}
	if profile.RulePack.HeaderPolicy != "odd_even" || profile.RulePack.EvenHeaderText != "even header" {
		t.Fatalf("odd/even header policy not inferred: %#v", profile.RulePack)
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
		t.Fatalf("AI section evidence should merge into local profile: %#v", profile.Sections["references_title"])
	}
	if profile.Confidence != 0.76 {
		t.Fatalf("Confidence = %v, want local confidence 0.76", profile.Confidence)
	}
	if profile.RulePack.CitationStyle != "superscript_bracket" ||
		profile.RulePack.ReferenceStandard != "GB/T 7714" ||
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
