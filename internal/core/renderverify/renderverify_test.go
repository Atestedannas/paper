package renderverify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckSamePageRulePasses(t *testing.T) {
	result, err := Check(context.Background(), "paper.docx", Options{
		Enabled:       true,
		Strict:        true,
		Renderer:      fakeRenderer{},
		TextExtractor: fakeExtractor{pages: []string{"封面", "论文题目 摘要：正文", "目录"}},
		SamePageRules: []SamePageRule{{Name: "title_abstract", LeftText: "论文题目", RightText: "摘要："}},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.Passed {
		t.Fatalf("Passed = false, issues = %#v", result.Issues)
	}
	if result.PageCount != 3 {
		t.Fatalf("PageCount = %d, want 3", result.PageCount)
	}
}

func TestCheckSamePageRulePassesWhenEarlierPageHasDuplicateLeftText(t *testing.T) {
	result, err := Check(context.Background(), "paper.docx", Options{
		Enabled:       true,
		Strict:        true,
		Renderer:      fakeRenderer{},
		TextExtractor: fakeExtractor{pages: []string{"论文题目 封面", "论文题目 摘要：正文"}},
		SamePageRules: []SamePageRule{{Name: "title_abstract", LeftText: "论文题目", RightText: "摘要："}},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.Passed {
		t.Fatalf("Passed = false, issues = %#v", result.Issues)
	}
}

func TestCheckSamePageRuleFails(t *testing.T) {
	result, err := Check(context.Background(), "paper.docx", Options{
		Enabled:       true,
		Strict:        true,
		Renderer:      fakeRenderer{},
		TextExtractor: fakeExtractor{pages: []string{"论文题目", "摘要：正文"}},
		SamePageRules: []SamePageRule{{Name: "title_abstract", LeftText: "论文题目", RightText: "摘要："}},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Passed = true, want false")
	}
	if len(result.Issues) != 1 || result.Issues[0].Kind != "same_page_rule_failed" {
		t.Fatalf("Issues = %#v, want same_page_rule_failed", result.Issues)
	}
}

func TestCheckStrictRenderFailureIsFatal(t *testing.T) {
	result, err := Check(context.Background(), "paper.docx", Options{
		Enabled:  true,
		Strict:   true,
		Renderer: fakeRenderer{err: errors.New("no renderer")},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Passed = true, want false")
	}
	if len(result.Issues) != 1 || result.Issues[0].Severity != SeverityFatal {
		t.Fatalf("Issues = %#v, want fatal render issue", result.Issues)
	}
}

func TestCheckStoresRenderedPNGPaths(t *testing.T) {
	result, err := Check(context.Background(), "paper.docx", Options{
		Enabled:       true,
		Strict:        true,
		Renderer:      fakeRenderer{},
		Rasterizer:    fakeRasterizer{paths: []string{"pages/page-1.png", "pages/page-2.png"}},
		PNGOutputDir:  "pages",
		TextExtractor: fakeExtractor{pages: []string{"page one", "page two"}},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.Passed {
		t.Fatalf("Passed = false, issues = %#v", result.Issues)
	}
	if len(result.PNGPaths) != 2 || result.PNGPaths[0] != "pages/page-1.png" {
		t.Fatalf("PNGPaths = %#v", result.PNGPaths)
	}
}

func TestCheckChineseTotalFooterMismatchFails(t *testing.T) {
	result, err := Check(context.Background(), "paper.docx", Options{
		Enabled:         true,
		Strict:          true,
		Renderer:        fakeRenderer{},
		TextExtractor:   fakeExtractor{pages: []string{"封面", "正文 第1页 共24页", "致谢 第2页 共24页"}},
		CheckPageFooter: true,
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Passed = true, want false")
	}
	if len(result.Issues) != 1 || result.Issues[0].Kind != "page_footer_total_mismatch" {
		t.Fatalf("Issues = %#v, want page_footer_total_mismatch", result.Issues)
	}
}

func TestCheckValidatesRenderedTextStyle(t *testing.T) {
	result, err := Check(context.Background(), "paper.docx", Options{
		Enabled:  true,
		Strict:   true,
		Renderer: fakeRenderer{},
		TextExtractor: fakeLayoutExtractor{
			pages: []string{"Abstract"},
			spans: []TextSpan{{Page: 1, Text: "Abstract", Font: "ABCDEF+TimesNewRoman", FontSize: 15, X: 250, Width: 95, PageWidth: 595}},
		},
		TextStyleRules: []TextStyleRule{{Name: "abstract", Text: "Abstract", FontContains: "Times New Roman", FontSize: 15, Alignment: "center"}},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.Passed || len(result.TextSpans) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestCheckRejectsRenderedFontSizeMismatch(t *testing.T) {
	result, err := Check(context.Background(), "paper.docx", Options{
		Enabled:        true,
		Strict:         true,
		Renderer:       fakeRenderer{},
		TextExtractor:  fakeLayoutExtractor{pages: []string{"摘要"}, spans: []TextSpan{{Page: 1, Text: "摘要", Font: "SimHei", FontSize: 12}}},
		TextStyleRules: []TextStyleRule{{Name: "abstract", Text: "摘要", FontSize: 15}},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.Passed || len(result.Issues) != 1 || result.Issues[0].Kind != "rendered_font_size_mismatch" {
		t.Fatalf("result = %#v", result)
	}
}

func TestParsePythonPDFTextOutput(t *testing.T) {
	pages, err := parsePythonPDFTextOutput([]byte(`["封面","正文 第1页 共24页"]`))
	if err != nil {
		t.Fatalf("parsePythonPDFTextOutput() error = %v", err)
	}
	if len(pages) != 2 || pages[1] != "正文 第1页 共24页" {
		t.Fatalf("pages = %#v", pages)
	}
}

func TestLibreOfficeUserInstallationArg(t *testing.T) {
	arg := libreOfficeUserInstallationArg(filepath.Join("tmp", "lo profile"))
	if !strings.HasPrefix(arg, "-env:UserInstallation=file:") || !strings.Contains(arg, "lo%20profile") {
		t.Fatalf("libreOfficeUserInstallationArg() = %q", arg)
	}
}

func TestCreateLibreOfficeProfileDirCleanup(t *testing.T) {
	profileDir, cleanup, err := createLibreOfficeProfileDir(t.TempDir())
	if err != nil {
		t.Fatalf("createLibreOfficeProfileDir() error = %v", err)
	}
	if _, err := os.Stat(profileDir); err != nil {
		t.Fatalf("profile dir should exist before cleanup: %v", err)
	}
	cleanup()
	if _, err := os.Stat(profileDir); !os.IsNotExist(err) {
		t.Fatalf("profile dir should be removed after cleanup, stat error = %v", err)
	}
}

type fakeRenderer struct {
	err error
}

func (r fakeRenderer) RenderPDF(context.Context, string, string) (PDFArtifact, error) {
	if r.err != nil {
		return PDFArtifact{}, r.err
	}
	return PDFArtifact{Path: filepath.Join("rendered", "paper.pdf")}, nil
}

type fakeRasterizer struct {
	paths []string
	err   error
}

func (r fakeRasterizer) RasterizePDF(context.Context, string, string) ([]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.paths, nil
}

type fakeExtractor struct {
	pages []string
	err   error
}

type fakeLayoutExtractor struct {
	pages []string
	spans []TextSpan
}

func (e fakeLayoutExtractor) ExtractPageTexts(string) ([]string, error) { return e.pages, nil }
func (e fakeLayoutExtractor) ExtractPageLayout(string) ([]string, []TextSpan, error) {
	return e.pages, e.spans, nil
}

func (e fakeExtractor) ExtractPageTexts(string) ([]string, error) {
	if e.err != nil {
		return nil, e.err
	}
	return e.pages, nil
}
