package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/verify"
)

func TestLoopControllerPatchesRepairableIssueOnceAndPassesAfterReverify(t *testing.T) {
	verifier := &sequenceVerifier{results: []verify.Result{
		{Passed: false, RepairableIssues: []verify.Issue{{Kind: "placeholder", Severity: "error", Message: "placeholder", Target: "word/document.xml"}}},
		{Passed: true},
	}}
	patchWriter := &countingPatchWriter{}

	result, err := NewLoopController(nil, patchWriter, verifier).Run(context.Background(), RunInput{OutputPath: "out.docx"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusVerifiedPass {
		t.Fatalf("Status = %s, want %s", result.Status, StatusVerifiedPass)
	}
	if result.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", result.Attempts)
	}
	if patchWriter.calls != 1 {
		t.Fatalf("patch calls = %d, want 1", patchWriter.calls)
	}
	if verifier.calls != 2 {
		t.Fatalf("verify calls = %d, want 2", verifier.calls)
	}
}

func TestLoopControllerFatalIssueGoesDirectlyToManualReview(t *testing.T) {
	verifier := &sequenceVerifier{results: []verify.Result{
		{Passed: false, FatalIssues: []verify.Issue{{Kind: "docx_open", Severity: "fatal", Message: "open failed", Target: "test.docx"}}},
	}}
	patchWriter := &countingPatchWriter{}

	result, err := NewLoopController(nil, patchWriter, verifier).Run(context.Background(), RunInput{OutputPath: "out.docx"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusManualReview {
		t.Fatalf("Status = %s, want %s", result.Status, StatusManualReview)
	}
	if patchWriter.calls != 0 {
		t.Fatalf("patch calls = %d, want 0", patchWriter.calls)
	}
	if verifier.calls != 1 {
		t.Fatalf("verify calls = %d, want 1", verifier.calls)
	}
}

func TestLoopControllerRepairableWithoutPatchWriterGoesToManualReview(t *testing.T) {
	verifier := &sequenceVerifier{results: []verify.Result{
		{Passed: false, RepairableIssues: []verify.Issue{{Kind: "placeholder", Severity: "error", Message: "placeholder", Target: "word/document.xml"}}},
	}}

	result, err := NewLoopController(nil, nil, verifier).Run(context.Background(), RunInput{OutputPath: "out.docx"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusManualReview {
		t.Fatalf("Status = %s, want %s", result.Status, StatusManualReview)
	}
	if result.Attempts != 0 {
		t.Fatalf("Attempts = %d, want 0", result.Attempts)
	}
}

func TestLoopControllerValidationErrors(t *testing.T) {
	tests := []struct {
		name       string
		ctx        context.Context
		input      RunInput
		verifier   Verifier
		wantErrSub string
	}{
		{name: "nil context", ctx: nil, input: RunInput{OutputPath: "out.docx"}, verifier: &sequenceVerifier{}, wantErrSub: "context"},
		{name: "empty output path", ctx: context.Background(), input: RunInput{}, verifier: &sequenceVerifier{}, wantErrSub: "output path"},
		{name: "nil verifier", ctx: context.Background(), input: RunInput{OutputPath: "out.docx"}, verifier: nil, wantErrSub: "verifier"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLoopController(nil, nil, tt.verifier).Run(tt.ctx, tt.input)
			if err == nil {
				t.Fatal("Run() error = nil, want validation error")
			}
			if !containsWorkflowText(err.Error(), tt.wantErrSub) {
				t.Fatalf("Run() error = %v, want substring %q", err, tt.wantErrSub)
			}
		})
	}
}

type sequenceVerifier struct {
	results []verify.Result
	err     error
	calls   int
}

func (v *sequenceVerifier) Verify(context.Context, string) (verify.Result, error) {
	v.calls++
	if v.err != nil {
		return verify.Result{}, v.err
	}
	if len(v.results) == 0 {
		return verify.Result{Passed: true}, nil
	}
	index := v.calls - 1
	if index >= len(v.results) {
		index = len(v.results) - 1
	}
	return v.results[index], nil
}

type countingPatchWriter struct {
	calls int
	err   error
}

func (w *countingPatchWriter) Apply(context.Context, string) error {
	w.calls++
	return w.err
}

func TestLoopControllerReturnsPatchError(t *testing.T) {
	patchErr := errors.New("patch failed")
	verifier := &sequenceVerifier{results: []verify.Result{
		{Passed: false, RepairableIssues: []verify.Issue{{Kind: "placeholder", Severity: "error", Message: "placeholder", Target: "word/document.xml"}}},
	}}
	patchWriter := &countingPatchWriter{err: patchErr}

	_, err := NewLoopController(nil, patchWriter, verifier).Run(context.Background(), RunInput{OutputPath: "out.docx"})
	if !errors.Is(err, patchErr) {
		t.Fatalf("Run() error = %v, want patch error", err)
	}
}

func TestLoopControllerConvergesAcrossMultiplePatchAttempts(t *testing.T) {
	verifier := &sequenceVerifier{results: []verify.Result{
		{RepairableIssues: []verify.Issue{{Kind: "page", Target: "document"}}},
		{RepairableIssues: []verify.Issue{{Kind: "paragraph", Target: "document"}}},
		{Passed: true},
	}}
	writer := &countingPatchWriter{}
	result, err := NewLoopController(nil, writer, verifier).Run(context.Background(), RunInput{OutputPath: "out.docx"})
	if err != nil || !result.VerifyResult.Passed || result.Attempts != 2 {
		t.Fatalf("Run() = %#v, %v", result, err)
	}
}

func TestLoopControllerStopsOnOscillatingIssues(t *testing.T) {
	issueA := verify.Result{RepairableIssues: []verify.Issue{{Kind: "font", Target: "document"}}}
	issueB := verify.Result{RepairableIssues: []verify.Issue{{Kind: "spacing", Target: "document"}}}
	verifier := &sequenceVerifier{results: []verify.Result{issueA, issueB, issueA}}
	writer := &countingPatchWriter{}
	result, err := NewLoopController(nil, writer, verifier).Run(context.Background(), RunInput{OutputPath: "out.docx"})
	if err != nil || result.Status != StatusManualReview || result.Attempts != 2 || writer.calls != 2 {
		t.Fatalf("Run() = %#v, %v; patch calls = %d", result, err, writer.calls)
	}
}

func containsWorkflowText(got, want string) bool {
	return len(want) == 0 || (len(got) >= len(want) && stringContains(got, want))
}

func stringContains(got, want string) bool {
	for i := 0; i+len(want) <= len(got); i++ {
		if got[i:i+len(want)] == want {
			return true
		}
	}
	return false
}
