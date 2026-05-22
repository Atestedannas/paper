package cqrwst

import (
	"context"
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

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
	assertParagraphHas(t, documentXML, "\u53c2\u8003\u6587\u732e", []string{`w:sz w:val="28"`, `<w:b/>`, `w:jc w:val="center"`})
}

func TestTemplateQualityRepairsBottomLevelArtifacts(t *testing.T) {
	docxPath := writeCQRWSTDocxWithEntries(t,
		"<w:p><w:r><w:drawing/></w:r></w:p>"+
			"<w:p><w:r><w:t>\u56fe1.1 \u56fe\u793a</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>1 \u7eea\u8bba</w:t></w:r></w:p>"+
			"<w:p><w:pPr><w:rPr><w:rFonts w:eastAsia=\"\u9ed1\u4f53\"/><w:sz w:val=\"32\"/></w:rPr></w:pPr><w:r><w:t>\u53c2\u8003\u6587\u732e</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>[1] Ref item.</w:t></w:r></w:p>",
		map[string]string{"word/header1.xml": `<w:hdr><w:p><w:r><w:t>bad header</w:t></w:r></w:p></w:hdr>`},
	)
	profile := &templateprofile.Profile{Version: templateprofile.Version, Styles: map[string]templateprofile.StyleRule{}}

	before, err := AnalyzeTemplateQuality(docxPath, profile)
	if err != nil {
		t.Fatalf("AnalyzeTemplateQuality() error = %v", err)
	}
	if before.Passed {
		t.Fatal("AnalyzeTemplateQuality() Passed = true, want false for bottom-level artifacts")
	}

	if _, err := FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}

	after, err := AnalyzeTemplateQuality(docxPath, profile)
	if err != nil {
		t.Fatalf("AnalyzeTemplateQuality() after fix error = %v", err)
	}
	if !after.Passed {
		t.Fatalf("AnalyzeTemplateQuality() Passed = false after fix, issues = %#v", after.Issues)
	}
	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if strings.Contains(documentXML, "\u56fe1.1 \u56fe\u793a") {
		t.Fatalf("generic cover caption should be removed: %s", documentXML)
	}
	assertParagraphHas(t, documentXML, "\u53c2\u8003\u6587\u732e", []string{`w:sz w:val="28"`, `w:jc w:val="center"`})
	headerXML := readCQRWSTEntry(t, docxPath, "word/header1.xml")
	if !strings.Contains(headerXML, `w:val="double"`) {
		t.Fatalf("header should have double divider: %s", headerXML)
	}
}
