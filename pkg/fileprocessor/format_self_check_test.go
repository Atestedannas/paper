package fileprocessor

import "testing"

func TestExecuteFormattingSelfCheckPublishesResult(t *testing.T) {
	p := NewEnhancedProcessor()

	var got FormattingSelfCheckResult
	p.formatSelfCheckHook = func(result FormattingSelfCheckResult) {
		got = result
	}

	want := FormattingSelfCheckResult{
		FunctionName: "applyKeywordsFormatting",
		Scope:        "paragraphs",
		TargetCount:  3,
		CheckedCount: 2,
		FixesApplied: 1,
	}

	result := p.executeFormattingSelfCheck(want)

	if result != want {
		t.Fatalf("executeFormattingSelfCheck() = %#v, want %#v", result, want)
	}
	if got != want {
		t.Fatalf("hook received %#v, want %#v", got, want)
	}
}

func TestRunDocumentFormattingSelfCheckWithNilDocument(t *testing.T) {
	p := NewEnhancedProcessor()

	result := p.runDocumentFormattingSelfCheck("applyA4PageSize", nil)

	if result.FunctionName != "applyA4PageSize" {
		t.Fatalf("FunctionName = %q, want %q", result.FunctionName, "applyA4PageSize")
	}
	if result.Scope != "document" {
		t.Fatalf("Scope = %q, want %q", result.Scope, "document")
	}
	if result.TargetCount != 0 || result.CheckedCount != 0 || result.FixesApplied != 0 {
		t.Fatalf("unexpected document self-check result: %#v", result)
	}
}
