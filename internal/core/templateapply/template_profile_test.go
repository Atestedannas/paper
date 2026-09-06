package templateapply

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

type testTemplateProfileProcessor struct {
	apply func(string) error
}

func (processor testTemplateProfileProcessor) Apply(_ context.Context, path string, _ *templateprofile.Profile) (int, error) {
	if processor.apply == nil {
		return 0, nil
	}
	return 1, processor.apply(path)
}

func (testTemplateProfileProcessor) Check(context.Context, string, string, *templateprofile.Profile) (int, error) {
	return 0, nil
}

func TestFixDOCXWithTemplateProfileRollsBackProcessorFailure(t *testing.T) {
	docxPath := writeCQRWSTDocx(t, `<w:p><w:r><w:t>original</w:t></w:r></w:p>`)
	original, err := os.ReadFile(docxPath)
	if err != nil {
		t.Fatal(err)
	}
	processors := []TemplateProfileProcessor{
		testTemplateProfileProcessor{apply: func(path string) error { return os.WriteFile(path, []byte("partial"), 0o600) }},
		testTemplateProfileProcessor{apply: func(string) error { return errors.New("processor failed") }},
	}

	if _, err := fixDOCXWithTemplateProfileProcessors(context.Background(), docxPath, &templateprofile.Profile{}, processors); err == nil {
		t.Fatal("expected processor failure")
	}
	got, err := os.ReadFile(docxPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatal("processor failure changed original document")
	}
}

func TestApplyTemplateProfileHeaderRunFormattingMakesInheritedStyleExplicit(t *testing.T) {
	input := `<w:hdr><w:p><w:r><w:rPr><w:rFonts w:hint="eastAsia"/><w:u w:val="single"/></w:rPr><w:t>重庆工程学院本科生毕业设计（论文）</w:t></w:r></w:p></w:hdr>`
	got := applyTemplateProfileHeaderRunFormatting(input, "宋体", 21, true)
	for _, want := range []string{
		`w:eastAsia="宋体"`,
		`w:ascii="宋体"`,
		`<w:sz w:val="21"/>`,
		`<w:szCs w:val="21"/>`,
		`<w:u w:val="single"/>`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("header formatting missing %s: %s", want, got)
		}
	}
	if !strings.Contains(got, `重庆工程学院本科生毕业设计（论文）`) {
		t.Fatalf("header text changed: %s", got)
	}
}

func TestApplyTemplateProfileHeaderFormattingOnlyTouchesReferencedParts(t *testing.T) {
	docxPath := writeCQRWSTDocxWithEntries(t,
		`<w:p><w:r><w:t>正文</w:t></w:r></w:p><w:sectPr><w:headerReference w:type="default" r:id="rId7"/></w:sectPr>`,
		map[string]string{
			"word/_rels/document.xml.rels": `<Relationships><Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header-active.xml"/></Relationships>`,
			"word/header-active.xml":       `<w:hdr><w:p><w:r><w:rPr><w:u w:val="single"/></w:rPr><w:t>重庆工程学院本科生毕业设计（论文）</w:t></w:r></w:p></w:hdr>`,
			"word/header-orphan.xml":       `<w:hdr><w:p><w:r><w:rPr/><w:t>历史页眉</w:t></w:r></w:p></w:hdr>`,
		})
	profile := &templateprofile.Profile{Header: templateprofile.HeaderFooterRule{Exists: true, FontEastAsia: "宋体", FontSizeHalfPt: "21", HasUnderline: true}}
	changed, err := ApplyTemplateProfileHeaderFormatting(context.Background(), docxPath, profile)
	if err != nil || changed != 1 {
		t.Fatalf("ApplyTemplateProfileHeaderFormatting() changed=%d err=%v", changed, err)
	}
	active := readCQRWSTEntry(t, docxPath, "word/header-active.xml")
	for _, want := range []string{`重庆工程学院本科生毕业设计（论文）`, `w:eastAsia="宋体"`, `<w:sz w:val="21"/>`, `<w:u w:val="single"/>`} {
		if !strings.Contains(active, want) {
			t.Fatalf("active header missing %s: %s", want, active)
		}
	}
	orphan := readCQRWSTEntry(t, docxPath, "word/header-orphan.xml")
	if strings.Contains(orphan, `w:eastAsia="宋体"`) || strings.Contains(orphan, `<w:sz w:val="21"/>`) {
		t.Fatalf("unreferenced header was modified: %s", orphan)
	}
}

func TestTemplateProfileReferenceStateExpires(t *testing.T) {
	documentXML := `<w:document><w:body>` +
		`<w:p><w:r><w:t>参考文献</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>continuation one</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>continuation two</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>ordinary paragraph</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"references": {FontEastAsia: "SimSun", FontSizeHalfPt: "21"},
	}}

	updated, _ := applyTemplateProfileStylesToDocumentXML(documentXML, profile)
	if strings.Contains(paragraphContaining(updated, "ordinary paragraph"), "SimSun") {
		t.Fatal("references state leaked beyond continuation limit")
	}
}

func TestFixDOCXWithTemplateProfileAppliesProfilePageBreaks(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		"<w:p><w:r><w:t>1 \u7eea\u8bba</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>\u6b63\u6587\u5185\u5bb9\u3002</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>\u53c2\u8003\u6587\u732e</w:t></w:r></w:p>"+
			`<w:p><w:r><w:t>[1] Zhang San. Title[J]. Journal,2024,1(1):1-2.</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		Sections: map[string]templateprofile.SectionRule{
			"body_start":       {PageBreakBefore: true},
			"references_title": {PageBreakBefore: true},
		},
	}

	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	bodyParagraph := paragraphContaining(documentXML, "1 \u7eea\u8bba")
	if strings.Contains(bodyParagraph, `<w:br w:type="page"/>`) {
		t.Fatalf("body_start should not force page break in conservative pagination mode: %s", bodyParagraph)
	}
	referenceParagraph := paragraphContaining(documentXML, "\u53c2\u8003\u6587\u732e")
	if !strings.Contains(referenceParagraph, `<w:br w:type="page"/>`) {
		t.Fatalf("reference title should start with page break: %s", referenceParagraph)
	}
}

func TestFixDOCXWithTemplateProfileAppliesProfileStyles(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		"<w:p><w:r><w:t>1 \u7eea\u8bba</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>\u6b63\u6587\u5185\u5bb9\u3002</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>\u53c2\u8003\u6587\u732e</w:t></w:r></w:p>"+
			`<w:p><w:r><w:t>[1] Zhang San. Title[J]. Journal,2024,1(1):1-2.</w:t></w:r></w:p>`+
			"<w:p><w:r><w:t>\u81f4\u8c22</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>\u611f\u8c22\u5185\u5bb9\u3002</w:t></w:r></w:p>",
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		Styles: map[string]templateprofile.StyleRule{
			"heading_1": {
				FontEastAsia:   "\u4eff\u5b8b",
				FontASCII:      "Times New Roman",
				FontSizeHalfPt: "30",
				Bold:           true,
				Alignment:      "center",
				Line:           "360",
				BeforeLines:    "200",
				AfterLines:     "200",
			},
			"body": {
				FontEastAsia:   "\u6977\u4f53",
				FontASCII:      "Times New Roman",
				FontSizeHalfPt: "26",
				Alignment:      "both",
				Line:           "420",
				LineRule:       "exact",
				BeforeTwips:    "240",
				AfterTwips:     "120",
				FirstLineChars: "200",
			},
			"references": {
				FontEastAsia:   "\u5b8b\u4f53",
				FontASCII:      "Times New Roman",
				FontSizeHalfPt: "21",
				Alignment:      "left",
				Line:           "360",
				FirstLineChars: "0",
			},
			"acknowledgements_title": {
				FontEastAsia:   "\u9ed1\u4f53",
				FontASCII:      "Times New Roman",
				FontSizeHalfPt: "30",
				Bold:           true,
				Alignment:      "center",
				Line:           "360",
			},
		},
	}

	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	assertParagraphHas(t, documentXML, "1 \u7eea\u8bba", []string{
		"w:eastAsia=\"\u4eff\u5b8b\"",
		`w:sz w:val="30"`,
		`<w:b/>`,
		`w:jc w:val="center"`,
		`w:beforeLines="200"`,
	})
	assertParagraphHas(t, documentXML, "\u6b63\u6587\u5185\u5bb9\u3002", []string{
		"w:eastAsia=\"\u6977\u4f53\"",
		`w:sz w:val="26"`,
		`w:firstLineChars="200"`,
		`w:line="420"`,
		`w:lineRule="exact"`,
		`w:before="240"`,
		`w:after="120"`,
	})
	assertParagraphHas(t, documentXML, "[1] Zhang San", []string{
		`w:sz w:val="21"`,
		`w:firstLineChars="0"`,
		`w:jc w:val="left"`,
	})
	assertParagraphHas(t, documentXML, "\u81f4\u8c22", []string{
		"w:eastAsia=\"\u9ed1\u4f53\"",
		`w:sz w:val="30"`,
		`w:jc w:val="center"`,
	})
}

func TestApplyTemplateProfileStylesSkipsTOCEntries(t *testing.T) {
	documentXML := `<w:document><w:body><w:p><w:pPr><w:tabs><w:tab w:val="right" w:leader="dot" w:pos="9000"/></w:tabs><w:spacing w:line="240"/></w:pPr><w:r><w:t>1 绪论</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>1</w:t></w:r></w:p></w:body></w:document>`
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"heading_1": {FontEastAsia: "黑体", FontSizeHalfPt: "36", Alignment: "center"},
	}}
	updated, count := applyTemplateProfileStylesToDocumentXML(documentXML, profile)
	if count != 0 || updated != documentXML {
		t.Fatalf("TOC entry was modified: count=%d xml=%s", count, updated)
	}
}

func TestApplyTemplateProfileTOCStylesFormatsExistingEntries(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:pPr><w:pStyle w:val="TOC1"/><w:tabs><w:tab w:val="right" w:leader="dot" w:pos="9000"/></w:tabs></w:pPr><w:r><w:t>1 绪论</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>1</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"toc_entry": {FontEastAsia: "宋体", FontASCII: "Times New Roman", FontSizeHalfPt: "24", Alignment: "distribute", Line: "400", LineRule: "exact"},
	}}
	count, err := ApplyTemplateProfileTOCStyles(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("TOC style change count = %d, want 1", count)
	}
	updated := readCQRWSTDocumentXML(t, docxPath)
	assertParagraphHas(t, updated, "1 绪论", []string{
		`w:eastAsia="宋体"`, `w:ascii="Times New Roman"`, `w:sz w:val="24"`,
		`w:jc w:val="distribute"`, `w:line="400"`, `w:lineRule="exact"`,
	})
}

func TestApplyTemplateProfileStylesUsesOutlineLevelsAndTemplateCaptions(t *testing.T) {
	documentXML := `<w:document><w:body>` +
		`<w:p><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:r><w:t>3Results</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:outlineLvl w:val="1"/></w:pPr><w:r><w:t>4.2Analysis</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>` + "\u88683-1 Data" + `</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Ordinary body paragraph.</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"heading_1": {
			FontEastAsia: "TemplateHeadingOne", FontASCII: "TemplateHeadingOne",
			FontSizeHalfPt: "32", Alignment: "center",
		},
		"heading_2": {
			FontEastAsia: "TemplateHeadingTwo", FontASCII: "TemplateHeadingTwo",
			FontSizeHalfPt: "28", Alignment: "left",
		},
		"table_caption": {
			FontEastAsia: "TemplateCaption", FontASCII: "TemplateCaption",
			FontSizeHalfPt: "21", Alignment: "center", Line: "300",
		},
		"body": {
			FontEastAsia: "TemplateBody", FontASCII: "TemplateBody",
			FontSizeHalfPt: "24", Alignment: "both", Line: "400",
		},
	}}

	updated, count := applyTemplateProfileStylesToDocumentXML(documentXML, profile)

	if count != 4 {
		t.Fatalf("style change count = %d, want 4", count)
	}
	assertParagraphHas(t, updated, "3Results", []string{`w:eastAsia="TemplateHeadingOne"`, `w:sz w:val="32"`, `w:jc w:val="center"`})
	assertParagraphHas(t, updated, "4.2Analysis", []string{`w:eastAsia="TemplateHeadingTwo"`, `w:sz w:val="28"`, `w:jc w:val="left"`})
	assertParagraphHas(t, updated, "\u88683-1 Data", []string{`w:eastAsia="TemplateCaption"`, `w:sz w:val="21"`, `w:jc w:val="center"`, `w:line="300"`})
	assertParagraphHas(t, updated, "Ordinary body paragraph.", []string{`w:eastAsia="TemplateBody"`, `w:sz w:val="24"`, `w:jc w:val="both"`})
}

func TestCheckDOCXWithTemplateProfileUsesProfileStyles(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Body paragraph content long enough.</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		Styles: map[string]templateprofile.StyleRule{
			"body": {
				FontEastAsia:   "Courier New",
				FontASCII:      "Courier New",
				FontSizeHalfPt: "26",
				Alignment:      "both",
				Line:           "420",
				FirstLineChars: "200",
			},
		},
	}

	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}

	hardcodedResult, err := CheckDOCX(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("CheckDOCX() error = %v", err)
	}
	if hardcodedResult.Passed {
		t.Fatal("CheckDOCX() Passed = true, want false because hardcoded style differs from profile")
	}

	profileResult, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if !profileResult.Passed {
		t.Fatalf("CheckDOCXWithTemplateProfile() Passed = false, issues = %#v", profileResult.Issues)
	}
}

func TestFixDOCXWithTemplateProfilePreservesStructuredFrontMatterRuns(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>Abstract: Objective text.</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Keywords: A; B; C</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		Styles: map[string]templateprofile.StyleRule{
			"abstract_en": {
				FontASCII:      "Courier New",
				FontSizeHalfPt: "26",
				Alignment:      "both",
				Line:           "420",
			},
			"keywords_en": {
				FontASCII:      "Courier New",
				FontSizeHalfPt: "26",
				Alignment:      "both",
				Line:           "420",
			},
		},
	}

	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}

	profileResult, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if !profileResult.Passed {
		t.Fatalf("CheckDOCXWithTemplateProfile() Passed = false, issues = %#v", profileResult.Issues)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	abstractParagraph := paragraphContaining(documentXML, "Abstract:")
	if runs := runPattern.FindAllString(abstractParagraph, -1); len(runs) < 2 {
		t.Fatalf("structured abstract should keep separate label/body runs: %s", abstractParagraph)
	}
	if !strings.Contains(abstractParagraph, `<w:b/>`) {
		t.Fatalf("structured abstract should keep bold label run: %s", abstractParagraph)
	}
}

func TestApplyTemplateProfileStylesUsesSeparateTemplateDerivedFrontMatterBodyStyles(t *testing.T) {
	documentXML := `<w:document><w:body>` +
		`<w:p><w:r><w:t>摘要：正文内容</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>关键词：模板；格式</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"abstract_cn": {
			FontEastAsia: "TemplateLabel", FontSizeHalfPt: "32",
			Bold: true, Alignment: "center",
		},
		"abstract_body": {
			FontEastAsia: "TemplateBody", FontSizeHalfPt: "24",
			Alignment: "both", Line: "400", FirstLineChars: "200",
		},
		"keywords_cn": {
			FontEastAsia: "TemplateKeywordLabel", FontSizeHalfPt: "24",
			Bold: true, Alignment: "left", Line: "400",
		},
		"keywords_cn_body": {
			FontEastAsia: "TemplateKeywordBody", FontSizeHalfPt: "24",
		},
	}}

	updated, count := applyTemplateProfileStylesToDocumentXML(documentXML, profile)
	if count != 2 {
		t.Fatalf("style change count = %d, want 2", count)
	}
	if got := len(paragraphPattern.FindAllString(updated, -1)); got != 2 {
		t.Fatalf("paragraph count = %d, want original 2: %s", got, updated)
	}
	abstractParagraph := paragraphContaining(updated, "摘要：")
	abstractRuns := runPattern.FindAllString(abstractParagraph, -1)
	if len(abstractRuns) != 2 {
		t.Fatalf("abstract paragraph should contain label/body runs: %s", abstractParagraph)
	}
	if !strings.Contains(abstractRuns[0], `w:eastAsia="TemplateLabel"`) ||
		!strings.Contains(abstractRuns[0], `<w:b/>`) ||
		!strings.Contains(abstractRuns[1], `w:eastAsia="TemplateBody"`) {
		t.Fatalf("abstract label/body styles did not come from their template samples: %s", abstractParagraph)
	}
	assertParagraphHas(t, updated, "摘要：", []string{
		`w:jc w:val="both"`,
		`w:line="400"`, `w:firstLineChars="200"`,
	})
	keywordParagraph := paragraphContaining(updated, "关键词：")
	runs := runPattern.FindAllString(keywordParagraph, -1)
	if len(runs) != 2 {
		t.Fatalf("keyword paragraph should contain label/body runs: %s", keywordParagraph)
	}
	if !strings.Contains(runs[0], `w:eastAsia="TemplateKeywordLabel"`) ||
		!strings.Contains(runs[1], `w:eastAsia="TemplateKeywordBody"`) {
		t.Fatalf("keyword label/body styles did not come from their template samples: %s", keywordParagraph)
	}
}

func TestApplyTemplateProfileCoverStylesDoesNotUseCoverTitleForInnerThesisTitle(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>2026年 6 月</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>社区2型糖尿病患者疾病知识认知现状及影响因素分析</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘要</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"cover":       {FontEastAsia: "宋体", FontSizeHalfPt: "21"},
		"cover_title": {FontEastAsia: "黑体", FontSizeHalfPt: "72", Bold: true, BoldSet: true},
		"title":       {FontEastAsia: "黑体", FontSizeHalfPt: "30", Bold: true, BoldSet: true, Alignment: "center"},
	}}

	if _, err := ApplyTemplateProfileCoverStylesAndPageSetup(context.Background(), docxPath, profile); err != nil {
		t.Fatal(err)
	}
	titleParagraph := paragraphContaining(readCQRWSTDocumentXML(t, docxPath), "社区2型糖尿病")
	if strings.Contains(titleParagraph, `w:val="72"`) || !strings.Contains(titleParagraph, `w:val="30"`) {
		t.Fatalf("inner thesis title used the wrong style: %s", titleParagraph)
	}
}

func TestApplyTemplateProfileStylesPreservesFrontMatterBookmarksHyperlinksAndFields(t *testing.T) {
	documentXML := `<w:document><w:body><w:p>` +
		`<w:bookmarkStart w:id="7" w:name="_RefAbstract"/><w:r><w:t>摘要：</w:t></w:r><w:bookmarkEnd w:id="7"/>` +
		`<w:hyperlink r:id="rId9"><w:r><w:t>正文内容</w:t></w:r></w:hyperlink>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:instrText xml:space="preserve"> PAGEREF _RefAbstract </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>1</w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="end"/></w:r></w:p></w:body></w:document>`
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"abstract_cn": {
			FontEastAsia: "TemplateLabel", FontSizeHalfPt: "32", Bold: true, Alignment: "center",
		},
		"abstract_body": {
			FontEastAsia: "TemplateBody", FontSizeHalfPt: "24", Alignment: "both", Line: "400",
		},
	}}

	updated, count := applyTemplateProfileStylesToDocumentXML(documentXML, profile)
	if count != 1 {
		t.Fatalf("style change count = %d, want 1", count)
	}
	for _, tag := range []string{"<w:bookmarkStart", "<w:bookmarkEnd", "<w:hyperlink", "<w:fldChar", "<w:instrText"} {
		if got, want := strings.Count(updated, tag), strings.Count(documentXML, tag); got != want {
			t.Fatalf("%s count = %d, want %d: %s", tag, got, want, updated)
		}
	}
	if !strings.Contains(updated, `<w:hyperlink r:id="rId9"><w:r>`) ||
		!strings.Contains(updated, `PAGEREF _RefAbstract`) {
		t.Fatalf("hyperlink/field structure was changed: %s", updated)
	}
	if body := paragraphContaining(updated, "正文内容"); !strings.Contains(body, `w:eastAsia="TemplateBody"`) {
		t.Fatalf("body run did not receive template body style: %s", body)
	}
}

func TestParagraphStyleFromTemplateProfilePreservesExplicitFalse(t *testing.T) {
	style, ok := paragraphStyleFromTemplateProfile(templateprofile.StyleRule{
		BoldSet: true, ItalicSet: true,
	})
	if !ok {
		t.Fatal("explicit false style was treated as undefined")
	}

	run := `<w:r><w:rPr><w:b/><w:bCs/><w:i/><w:iCs/><w:vertAlign w:val="subscript"/></w:rPr><w:t>x</w:t></w:r>`
	updated := applyRunProperties(run, style)
	for _, want := range []string{
		`<w:b w:val="false"/>`,
		`<w:bCs w:val="false"/>`,
		`<w:i w:val="false"/>`,
		`<w:iCs w:val="false"/>`,
		`<w:vertAlign w:val="subscript"/>`,
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("template profile tri-state missing %s: %s", want, updated)
		}
	}
}

func TestParagraphStyleFromTemplateProfilePreservesAllFontSlots(t *testing.T) {
	style, ok := paragraphStyleFromTemplateProfile(templateprofile.StyleRule{
		FontASCII: "ASCII", FontHAnsi: "HANSI", FontEastAsia: "EAST", FontCS: "CS",
	})
	if !ok {
		t.Fatal("font-only template style was treated as empty")
	}
	properties := buildRunProperties(style)
	for _, want := range []string{
		`w:ascii="ASCII"`,
		`w:hAnsi="HANSI"`,
		`w:eastAsia="EAST"`,
		`w:cs="CS"`,
	} {
		if !strings.Contains(properties, want) {
			t.Fatalf("missing %s in %s", want, properties)
		}
	}
}

func TestFixDOCXWithTemplateProfileDoesNotOverrideReferencesTitleRule(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		"<w:p><w:r><w:t>1 \u7eea\u8bba</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>\u53c2\u8003\u6587\u732e</w:t></w:r></w:p>"+
			`<w:p><w:r><w:t>[1] Ref item.</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		Styles: map[string]templateprofile.StyleRule{
			"references_title": {
				FontEastAsia:   "\u9ed1\u4f53",
				FontSizeHalfPt: "32",
				Alignment:      "left",
				Line:           "360",
			},
		},
	}

	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	assertParagraphHas(t, documentXML, "\u53c2\u8003\u6587\u732e", []string{"w:eastAsia=\"\u9ed1\u4f53\"", `w:sz w:val="32"`, `w:jc w:val="left"`})
}

func TestFixDOCXWithTemplateProfileAppliesPageSetupFromProfile(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p>`+
			`<w:sectPr><w:pgSz w:w="10000" w:h="10000"/><w:pgMar w:top="100" w:right="100" w:bottom="100" w:left="100"/></w:sectPr>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		PageSetup: templateprofile.PageSetupRule{
			PageWidthTwips:    "11906",
			PageHeightTwips:   "16838",
			MarginTopTwips:    "1701",
			MarginRightTwips:  "1417",
			MarginBottomTwips: "1417",
			MarginLeftTwips:   "1701",
			HeaderMarginTwips: "907",
			FooterMarginTwips: "851",
		},
	}

	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	for _, want := range []string{
		`<w:pgSz w:w="11906" w:h="16838"/>`,
		`w:top="1701"`,
		`w:right="1417"`,
		`w:bottom="1417"`,
		`w:left="1701"`,
		`w:header="907"`,
		`w:footer="851"`,
	} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("document XML missing page setup %s: %s", want, documentXML)
		}
	}
}

func TestFixDOCXWithTemplateProfileAppliesPageSetupToEverySection(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:pPr><w:sectPr><w:pgMar w:top="100" w:right="100" w:bottom="100" w:left="100"/></w:sectPr></w:pPr><w:r><w:t>Cover</w:t></w:r></w:p>`+
			`<w:sectPr><w:pgMar w:top="200" w:right="200" w:bottom="200" w:left="200"/></w:sectPr>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		PageSetup: templateprofile.PageSetupRule{
			MarginTopTwips: "1418", MarginRightTwips: "1418",
			MarginBottomTwips: "1418", MarginLeftTwips: "1418",
		},
	}
	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}
	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if got := strings.Count(documentXML, `w:top="1418" w:right="1418" w:bottom="1418" w:left="1418"`); got != 2 {
		t.Fatalf("expected both section margins to be updated, got %d: %s", got, documentXML)
	}
}

func TestFixDOCXWithTemplateProfileSuperscriptsBracketCitations(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>Research result </w:t></w:r><w:r><w:t>[1]</w:t></w:r><w:r><w:t> is stable.</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>参考文献</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>[1] Zhang San. Title[J]. Journal, 2024, 1(1):1-2.</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version:  templateprofile.Version,
		RulePack: templateprofile.RulePack{CitationStyle: "superscript_bracket"},
	}

	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	bodyParagraph := paragraphContaining(documentXML, "Research result")
	if !strings.Contains(bodyParagraph, `<w:vertAlign w:val="superscript"/>`) {
		t.Fatalf("body citation should be superscript: %s", bodyParagraph)
	}
	referenceParagraph := paragraphContaining(documentXML, "Zhang San")
	if strings.Contains(referenceParagraph, `<w:vertAlign w:val="superscript"/>`) {
		t.Fatalf("reference list marker should not be superscripted: %s", referenceParagraph)
	}
}

func TestCheckDOCXWithTemplateProfileRejectsMalformedGBReferenceEntries(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>参考文献</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>[1] Missing type marker and source fields.</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version:  templateprofile.Version,
		RulePack: templateprofile.RulePack{ReferenceStandard: "GB/T 7714-2005"},
	}

	result, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}

	if result.Passed {
		t.Fatalf("malformed GB/T reference should fail verification: %#v", result)
	}
}

func TestFixDOCXWithTemplateProfileAppliesThreeLineTableBorders(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:tbl><w:tblPr><w:tblBorders><w:left w:val="single"/><w:right w:val="single"/><w:insideV w:val="single"/></w:tblBorders></w:tblPr>`+
			`<w:tr><w:tc><w:p><w:r><w:t>Header</w:t></w:r></w:p></w:tc></w:tr>`+
			`<w:tr><w:tc><w:p><w:r><w:t>Cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`,
	)
	profile := &templateprofile.Profile{
		Version:  templateprofile.Version,
		RulePack: templateprofile.RulePack{TableStyle: "three-line"},
	}

	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	for _, want := range []string{
		`<w:top w:val="single" w:sz="12"`,
		`<w:bottom w:val="single" w:sz="12"`,
		`<w:insideH w:val="single" w:sz="8"`,
		`<w:left w:val="nil"/>`,
		`<w:right w:val="nil"/>`,
		`<w:insideV w:val="nil"/>`,
	} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("three-line table missing %s: %s", want, documentXML)
		}
	}
}

func TestCheckDOCXWithTemplateProfileValidatesExpandedRequiredSectionsAndFields(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>`+"\u5c01\u9762"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>`+"\u5b66\u6821\u4ee3\u7801\uff1a"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>`+"\u6458\u8981"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		RulePack: templateprofile.RulePack{
			RequiredSections: []string{"cover", "abstract_cn", "abstract_en", "toc", "body", "references"},
			RequiredFields:   []string{"\u5b66\u6821\u4ee3\u7801"},
		},
	}

	result, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("missing sections and empty fields should fail verification: %#v", result)
	}
}

func TestRequiredOriginalityDeclarationIsDetectedWithoutFabricatingContent(t *testing.T) {
	if !hasRequiredSection([]string{"原创性声明"}, "originality_declaration") {
		t.Fatal("originality declaration was not recognized")
	}
	if hasRequiredSection([]string{"摘要", "1 绪论"}, "originality_declaration") {
		t.Fatal("missing originality declaration was accepted")
	}
}

func TestCheckDOCXWithTemplateProfileValidatesTitleKeywordsHeadingsAndBodyLength(t *testing.T) {
	longTitle := strings.Repeat("\u8bba", 26)
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>`+"\u9898\u540d\uff1a"+longTitle+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Title: one two three four five six seven eight nine ten eleven</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>`+"\u5173\u952e\u8bcd\uff1aA,B"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>`+"\u7b2c\u4e00\u7ae0 \u7eea\u8bba"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>`+"\u77ed\u6b63\u6587"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>`+"\u53c2\u8003\u6587\u732e"+`</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		RulePack: templateprofile.RulePack{
			TitleMaxCNChars:  25,
			TitleMaxENWords:  10,
			KeywordMin:       3,
			KeywordMax:       5,
			HeadingNumbering: "arabic",
			BodyMinChars:     30,
		},
	}

	result, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("title, keyword, heading, and body length violations should fail: %#v", result)
	}
}

func TestCheckDOCXWithTemplateProfileValidatesNumberingAndReferenceQuantities(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>`+"\u56fe1 \u9519\u8bef\u56fe\u9898"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>`+"\u88681 \u9519\u8bef\u8868\u9898"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>`+"\u5f0f(1)"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>`+"\u53c2\u8003\u6587\u732e"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>[1] `+"\u5f20\u4e09"+`. Title[J]. Journal, 2024, 1(1):1-2.</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>[2] `+"\u674e\u56db"+`. Title[J]. Journal, 2024, 1(1):1-2.</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		RulePack: templateprofile.RulePack{
			FigureNumbering:          "chapter",
			TableNumbering:           "chapter",
			FormulaNumbering:         "chapter",
			ReferenceMinCount:        3,
			ReferenceForeignRatioMin: 0.5,
		},
	}

	result, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("numbering and reference quantity violations should fail: %#v", result)
	}
}

func TestCheckDOCXWithTemplateProfileValidatesHeaderPageNumberingAndBlindReview(t *testing.T) {
	docxPath := writeCQRWSTDocxWithEntries(t,
		`<w:p><w:r><w:t>`+"\u4f5c\u8005\uff1a\u5f20\u4e09"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p>`+
			`<w:sectPr><w:footerReference w:type="default" r:id="rIdFooter"/><w:pgNumType w:start="2"/></w:sectPr>`,
		map[string]string{"word/footer1.xml": `<w:ftr><w:p><w:r><w:t>1</w:t></w:r></w:p></w:ftr>`},
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		Header:  templateprofile.HeaderFooterRule{Exists: true},
		RulePack: templateprofile.RulePack{
			HeaderPolicy:  "template",
			PageNumbering: "body_arabic_footer_center",
			BlindReview:   true,
		},
	}

	result, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("header, page numbering, and blind review violations should fail: %#v", result)
	}
}

func TestCheckDOCXWithTemplateProfileValidatesOddEvenHeaders(t *testing.T) {
	docxPath := writeCQRWSTDocxWithEntries(t,
		`<w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p>`+
			`<w:sectPr><w:headerReference w:type="default" r:id="rIdOdd"/></w:sectPr>`,
		map[string]string{
			"word/header1.xml": `<w:hdr><w:p><w:r><w:t>chapter</w:t></w:r></w:p></w:hdr>`,
		},
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		RulePack: templateprofile.RulePack{
			HeaderPolicy:   "odd_even",
			EvenHeaderText: "university thesis",
			HeaderLine:     "single_0_75pt",
		},
	}

	result, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("odd/even header policy violations should fail: %#v", result)
	}
}

func TestCheckDOCXWithTemplateProfileValidatesComplexPageNumbering(t *testing.T) {
	docxPath := writeCQRWSTDocxWithEntries(t,
		`<w:p><w:pPr><w:sectPr><w:pgNumType w:fmt="upperRoman" w:start="1"/></w:sectPr></w:pPr></w:p>`+
			`<w:p><w:pPr><w:sectPr><w:footerReference w:type="default" r:id="rIdFooter"/><w:pgNumType w:start="2" w:fmt="decimal"/></w:sectPr></w:pPr></w:p>`,
		map[string]string{
			"word/footer1.xml": `<w:ftr><w:p><w:r><w:instrText> PAGE </w:instrText></w:r></w:p></w:ftr>`,
		},
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		RulePack: templateprofile.RulePack{
			PageNumbering:   "front_roman_body_arabic_center",
			FrontPageFormat: "lowerRoman",
			BodyPageFormat:  "decimal",
			BodyPageStart:   1,
			BodyPageWrapper: "dash",
		},
	}

	result, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("complex page numbering violations should fail: %#v", result)
	}
}

func TestCheckDOCXWithTemplateProfileValidatesConfiguredHeadingLevels(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>`+"\u7b2c\u4e00\u7ae0 \u7eea\u8bba"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1.1 Section</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1.1.1 Subsection</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		RulePack: templateprofile.RulePack{
			HeadingLevels: []string{"\u7b2c1\u7ae0", "1.1", "1.1.1"},
		},
	}

	result, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("configured heading level violations should fail: %#v", result)
	}
}

func TestCheckDOCXWithTemplateProfileValidatesCaptionNumberingAndPosition(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>`+"\u56fe1.1 \u9519\u8bef\u8fde\u7eed\u56fe\u9898"+`</w:t></w:r></w:p>`+
			`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`+
			`<w:p><w:r><w:t>`+"\u88681 \u8868\u9898\u5728\u8868\u540e"+`</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		RulePack: templateprofile.RulePack{
			FigureNumbering:      "continuous",
			TableCaptionPosition: "above",
		},
	}

	result, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("caption numbering and position violations should fail: %#v", result)
	}
}

func TestCheckDOCXWithTemplateProfileValidatesSubsectionCaptionNumbering(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>图1.1 错误图题</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>表1.1.1 正确表题</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version:  templateprofile.Version,
		RulePack: templateprofile.RulePack{FigureNumbering: "subsection", TableNumbering: "subsection"},
	}

	result, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if result.Passed {
		t.Fatal("subsection figure numbering violation should fail")
	}
}

func TestCheckDOCXWithTemplateProfileValidatesReferenceStyles(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>`+"\u53c2\u8003\u6587\u732e"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>[1] Missing marker and source.</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>[2] Zhang San. Book[M]. Beijing: Press, 2020.</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		RulePack: templateprofile.RulePack{
			ReferenceStyle: "sample_book_journal_basic",
		},
	}

	result, err := CheckDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("CheckDOCXWithTemplateProfile() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("reference style violations should fail: %#v", result)
	}
}

func TestFixDOCXWithTemplateProfileAppliesOddEvenHeadersAndPageNumbering(t *testing.T) {
	docxPath := writeCQRWSTDocxWithEntries(t,
		`<w:p><w:r><w:t>目录</w:t></w:r></w:p>`+
			`<w:p><w:pPr><w:sectPr/></w:pPr></w:p>`+
			`<w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p>`+
			`<w:sectPr/>`,
		map[string]string{
			"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
			"word/settings.xml":            `<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"></w:settings>`,
		},
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		RulePack: templateprofile.RulePack{
			HeaderPolicy:    "odd_even",
			OddHeaderText:   "chapter",
			EvenHeaderText:  "重庆大学硕士论文",
			HeaderLine:      "single_0_75pt",
			PageNumbering:   "front_roman_body_arabic_center",
			FrontPageFormat: "lowerRoman",
			BodyPageFormat:  "decimal",
			BodyPageStart:   1,
			BodyPageWrapper: "dash",
		},
	}

	result, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}
	if result.FixCount == 0 {
		t.Fatalf("FixDOCXWithTemplateProfile() fix count = 0, want generated OOXML parts")
	}

	documentXML := readCQRWSTEntry(t, docxPath, "word/document.xml")
	relsXML := readCQRWSTEntry(t, docxPath, "word/_rels/document.xml.rels")
	settingsXML := readCQRWSTEntry(t, docxPath, "word/settings.xml")
	contentTypesXML := readCQRWSTEntry(t, docxPath, "[Content_Types].xml")
	allHeaders := readCQRWSTEntry(t, docxPath, "word/header1.xml") + readCQRWSTEntry(t, docxPath, "word/header2.xml")
	footerXML := readCQRWSTEntry(t, docxPath, "word/footer1.xml")

	for _, want := range []string{
		`w:type="default"`,
		`w:type="even"`,
		`footerReference`,
		`w:fmt="lowerRoman"`,
		`w:fmt="decimal" w:start="1"`,
	} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("document.xml missing %s:\n%s", want, documentXML)
		}
	}
	if !strings.Contains(settingsXML, `<w:evenAndOddHeaders/>`) {
		t.Fatalf("settings.xml missing evenAndOddHeaders:\n%s", settingsXML)
	}
	if !strings.Contains(relsXML, `/header`) || !strings.Contains(relsXML, `/footer`) {
		t.Fatalf("document.xml.rels missing header/footer relationships:\n%s", relsXML)
	}
	if !strings.Contains(contentTypesXML, `word/header1.xml`) || !strings.Contains(contentTypesXML, `word/footer1.xml`) {
		t.Fatalf("[Content_Types].xml missing header/footer overrides:\n%s", contentTypesXML)
	}
	if !strings.Contains(allHeaders, "重庆大学硕士论文") || !strings.Contains(allHeaders, "w:bottom") {
		t.Fatalf("headers missing even text or line:\n%s", allHeaders)
	}
	if !strings.Contains(footerXML, "PAGE") || !strings.Contains(footerXML, ">-<") {
		t.Fatalf("footer missing dash page field:\n%s", footerXML)
	}
}

func TestFixDOCXWithTemplateProfilePrefersTemplateChineseTotalFooterOverDashRule(t *testing.T) {
	docxPath := writeCQRWSTDocxWithEntries(t,
		`<w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p>`+
			`<w:sectPr/>`,
		map[string]string{
			"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		},
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		Footer: templateprofile.HeaderFooterRule{
			Exists:       true,
			Text:         "第页 共页",
			HasPageField: true,
			HasNumPages:  true,
		},
		RulePack: templateprofile.RulePack{
			PageNumbering:   "front_roman_body_arabic_center",
			BodyPageFormat:  "decimal",
			BodyPageStart:   1,
			BodyPageWrapper: "dash",
		},
	}

	result, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}
	if result.FixCount == 0 {
		t.Fatalf("FixDOCXWithTemplateProfile() fix count = 0, want page footer fix")
	}
	footerXML := readCQRWSTEntry(t, docxPath, "word/footer1.xml")
	for _, want := range []string{"第 ", "PAGE", " 页 共 ", "SECTIONPAGES"} {
		if !strings.Contains(footerXML, want) {
			t.Fatalf("footer should follow template chinese total page style %q:\n%s", want, footerXML)
		}
	}
	if strings.Contains(footerXML, "NUMPAGES") {
		t.Fatalf("footer should count the numbered section, not the whole document:\n%s", footerXML)
	}
	if strings.Contains(footerXML, ">-<") {
		t.Fatalf("footer should not use dash page wrapper when template footer has total pages:\n%s", footerXML)
	}
}

func TestFixDOCXWithTemplateProfileUsesTemplateHeaderByDefaultAndFillsPlaceholders(t *testing.T) {
	docxPath := writeCQRWSTDocxWithEntries(t,
		`<w:p><w:r><w:t>本科毕业论文/设计</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>专业</w:t></w:r></w:p><w:p><w:r><w:t>护理学</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>班级</w:t></w:r></w:p><w:p><w:r><w:t>2022级护理学5班</w:t></w:r></w:p>`+
			`<w:sectPr/>`,
		map[string]string{
			"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		},
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		Header: templateprofile.HeaderFooterRule{
			Exists: true,
			Text:   "重庆人文科技学院2026届XXX专业本科毕业论文/设计",
		},
	}

	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}
	headerXML := readCQRWSTEntry(t, docxPath, "word/header1.xml")
	want := "重庆人文科技学院2026届护理学专业本科毕业论文"
	if !strings.Contains(headerXML, want) || strings.Contains(headerXML, "XXX") || strings.Contains(headerXML, " 或 ") {
		t.Fatalf("header should contain one materialized title %q:\n%s", want, headerXML)
	}
}

func TestFixDOCXWithTemplateProfilePreservesReferencedTemplateHeaderAndFooter(t *testing.T) {
	headerXML := `<w:hdr><w:p><w:pPr><w:tabs><w:tab w:val="right" w:pos="9000"/></w:tabs><w:pBdr><w:bottom w:val="single" w:sz="8"/></w:pBdr></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="仿宋"/><w:sz w:val="18"/></w:rPr><w:t>学校名称</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>论文标题</w:t></w:r></w:p></w:hdr>`
	footerXML := `<w:ftr><w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Times New Roman"/><w:sz w:val="21"/></w:rPr><w:t>第</w:t></w:r><w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:t>页</w:t></w:r></w:p></w:ftr>`
	docxPath := writeCQRWSTDocxWithEntries(t,
		`<w:p><w:r><w:t>正文内容</w:t></w:r></w:p><w:sectPr><w:headerReference w:type="default" r:id="rIdHeader"/><w:footerReference w:type="default" r:id="rIdFooter"/></w:sectPr>`,
		map[string]string{
			"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdHeader" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/><Relationship Id="rIdFooter" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/></Relationships>`,
			"word/header1.xml":             headerXML,
			"word/footer1.xml":             footerXML,
		},
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		Header:  templateprofile.HeaderFooterRule{Exists: true, Text: "学校名称论文标题", FontEastAsia: "仿宋", FontSizeHalfPt: "18"},
		Footer:  templateprofile.HeaderFooterRule{Exists: true, HasPageField: true, Text: "第 页", FontSizeHalfPt: "21"},
	}

	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}
	if got := readCQRWSTEntry(t, docxPath, "word/header1.xml"); got != headerXML {
		t.Fatalf("referenced template header was rebuilt:\n%s", got)
	}
	if got := readCQRWSTEntry(t, docxPath, "word/footer1.xml"); got != footerXML {
		t.Fatalf("referenced template footer was rebuilt:\n%s", got)
	}
}

func TestFixDOCXWithTemplateProfileAppliesHeadingNumberingDefinitionsAndCaptionPositions(t *testing.T) {
	docxPath := writeCQRWSTDocxWithEntries(t,
		`<w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p>`+
			`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`+
			`<w:p><w:r><w:t>`+"\u88681 \u8868\u9898"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>`+"\u56fe1 \u56fe\u9898"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:drawing/></w:r></w:p>`,
		map[string]string{
			"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		},
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		RulePack: templateprofile.RulePack{
			HeadingLevels:         []string{"1", "1.1", "1.1.1"},
			TableCaptionPosition:  "above",
			FigureCaptionPosition: "below",
			FigureNumbering:       "continuous",
			TableNumbering:        "continuous",
			CaptionStyleKey:       "caption",
		},
		Styles: map[string]templateprofile.StyleRule{
			"caption": {FontEastAsia: "\u5b8b\u4f53", FontASCII: "Times New Roman", FontSizeHalfPt: "18", Alignment: "center"},
		},
	}

	_, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}

	documentXML := readCQRWSTEntry(t, docxPath, "word/document.xml")
	numberingXML := readCQRWSTEntry(t, docxPath, "word/numbering.xml")
	stylesXML := readCQRWSTEntry(t, docxPath, "word/styles.xml")
	if strings.Index(documentXML, "\u88681 \u8868\u9898") > strings.Index(documentXML, "<w:tbl") {
		t.Fatalf("table caption should be moved above table:\n%s", documentXML)
	}
	if strings.Index(documentXML, "\u56fe1 \u56fe\u9898") < strings.Index(documentXML, "<w:drawing") {
		t.Fatalf("figure caption should be moved below figure:\n%s", documentXML)
	}
	if !strings.Contains(numberingXML, `<w:abstractNumId w:val="9000"`) || !strings.Contains(stylesXML, `styleId="Heading1"`) {
		t.Fatalf("heading numbering definitions missing:\nnumbering=%s\nstyles=%s", numberingXML, stylesXML)
	}
}

func TestFixDOCXWithTemplateProfileAppliesReferenceParagraphStyle(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>`+"\u53c2\u8003\u6587\u732e"+`</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>[1] Zhang San. Book[M]. Beijing: Press, 2020.</w:t></w:r></w:p>`,
	)
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		RulePack: templateprofile.RulePack{
			ReferenceStyle: "sample_book_journal_basic",
		},
		Styles: map[string]templateprofile.StyleRule{
			"references": {FontEastAsia: "\u5b8b\u4f53", FontASCII: "Times New Roman", FontSizeHalfPt: "18", Line: "320", Alignment: "left"},
		},
	}

	_, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile)
	if err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}
	documentXML := readCQRWSTEntry(t, docxPath, "word/document.xml")
	if !strings.Contains(documentXML, `<w:sz w:val="18"/>`) || !strings.Contains(documentXML, `<w:spacing w:line="320"`) {
		t.Fatalf("reference paragraph style was not applied:\n%s", documentXML)
	}
}

func TestNormalizeGBReferenceSequenceRenumbersExistingEntries(t *testing.T) {
	documentXML := `<w:document><w:body>` +
		`<w:p><w:r><w:t>参考文献</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>[8] Zhang San. Book[M]. Beijing: Press, 2020.</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>[3] Li Si. Article[J]. Journal, 2021, 1(2): 3-4.</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>致谢</w:t></w:r></w:p>` +
		`</w:body></w:document>`

	updated, count := normalizeGBReferenceSequence(documentXML)
	if count != 2 {
		t.Fatalf("normalizeGBReferenceSequence() count = %d, want 2", count)
	}
	for _, want := range []string{"[1] Zhang San", "[2] Li Si", "致谢"} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated references missing %q: %s", want, updated)
		}
	}
}

func TestTemplateProfileRecognizesCompactNumberedHeadings(t *testing.T) {
	for _, test := range []struct {
		text  string
		level int
	}{
		{"1.编写要求", 2},
		{"1.1研究目的", 2},
		{"1.1.1研究范围", 3},
	} {
		if got := templateProfileParagraphHeadingLevel("", test.text); got != test.level {
			t.Fatalf("heading level for %q = %d, want %d", test.text, got, test.level)
		}
	}
	if got := templateProfileParagraphHeadingLevel("", "1.毕业设计（论文）必须由学生本人独立完成，不得弄虚作假，不得抄袭他人成果。"); got != 0 {
		t.Fatalf("long numbered sentence got heading level %d", got)
	}
}

func TestApplyTemplateProfileStylesUpdatesParagraphDefaultRunProperties(t *testing.T) {
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"heading_1": {FontEastAsia: "黑体", FontSizeHalfPt: "32", Alignment: "center"},
	}}
	input := `<w:document><w:body><w:p><w:pPr><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="24"/></w:rPr></w:pPr><w:r><w:t>一、基本要求</w:t></w:r></w:p></w:body></w:document>`
	updated, count := applyTemplateProfileStylesToDocumentXML(input, profile)
	if count != 1 || !strings.Contains(updated, `<w:rFonts w:eastAsia="黑体"`) || !strings.Contains(updated, `<w:sz w:val="32"`) {
		t.Fatalf("paragraph default run properties were not updated: count=%d xml=%s", count, updated)
	}
}

func TestApplyTemplateProfileStylesPreservesCoverTitleRules(t *testing.T) {
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"cover":         {FontEastAsia: "宋体", FontSizeHalfPt: "32", Alignment: "center"},
		"cover_title":   {FontEastAsia: "宋体", FontASCII: "Times New Roman", FontSizeHalfPt: "72", Alignment: "center", Bold: true, BoldSet: true},
		"cover_date":    {FontEastAsia: "宋体", FontASCII: "Times New Roman", FontSizeHalfPt: "28", Alignment: "center", Line: "720", LineRule: "auto"},
		"abstract_cn":   {FontEastAsia: "黑体", FontASCII: "Times New Roman", FontSizeHalfPt: "32", Alignment: "center"},
		"abstract_body": {FontEastAsia: "宋体", FontASCII: "宋体", FontSizeHalfPt: "24", Line: "400", LineRule: "exact"},
	}}
	input := `<w:document><w:body>` +
		`<w:p><w:r><w:t>本科毕业论文/设计</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>2026年 3 月</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>社区2型糖尿病患者疾病知识认知现状及影响因素分析</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>摘要：</w:t></w:r><w:r><w:t>这是摘要正文。</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	updated, count := applyTemplateProfileStylesToDocumentXML(input, profile)
	if count != 4 || strings.Count(updated, `w:sz w:val="72"`) < 2 {
		t.Fatalf("cover title/date rules were not applied deterministically: count=%d xml=%s", count, updated)
	}
	if !strings.Contains(updated, `w:sz w:val="32"`) || !strings.Contains(updated, `w:sz w:val="24"`) {
		t.Fatalf("abstract title/body rules were not preserved: %s", updated)
	}
}
