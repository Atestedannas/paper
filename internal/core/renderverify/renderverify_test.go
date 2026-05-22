package renderverify

import (
	"context"
	"errors"
	"path/filepath"
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

type fakeRenderer struct {
	err error
}

func (r fakeRenderer) RenderPDF(context.Context, string, string) (PDFArtifact, error) {
	if r.err != nil {
		return PDFArtifact{}, r.err
	}
	return PDFArtifact{Path: filepath.Join("rendered", "paper.pdf")}, nil
}

type fakeExtractor struct {
	pages []string
	err   error
}

func (e fakeExtractor) ExtractPageTexts(string) ([]string, error) {
	if e.err != nil {
		return nil, e.err
	}
	return e.pages, nil
}
