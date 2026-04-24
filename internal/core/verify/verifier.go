package verify

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

const documentTarget = "word/document.xml"

var placeholderPattern = regexp.MustCompile(`\{\{[^{}]+\}\}`)

type Issue struct {
	Kind     string
	Severity string
	Message  string
	Target   string
}

type Result struct {
	Passed           bool
	FatalIssues      []Issue
	RepairableIssues []Issue
	Warnings         []Issue
}

type Verifier struct{}

func NewVerifier() *Verifier {
	return &Verifier{}
}

func (v *Verifier) Verify(ctx context.Context, docxPath string) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		return Result{
			Passed: false,
			FatalIssues: []Issue{{
				Kind:     "docx_open",
				Severity: "fatal",
				Message:  fmt.Sprintf("open docx %q failed: %v", docxPath, err),
				Target:   docxPath,
			}},
		}, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	content, ok := pkg.Get(documentTarget)
	if !ok {
		return Result{
			Passed: false,
			FatalIssues: []Issue{{
				Kind:     "missing_document_xml",
				Severity: "fatal",
				Message:  "required document XML is missing",
				Target:   documentTarget,
			}},
		}, nil
	}

	result := Result{}
	document := string(content)
	if placeholderPattern.MatchString(document) {
		result.RepairableIssues = append(result.RepairableIssues, Issue{
			Kind:     "placeholder",
			Severity: "error",
			Message:  "document still contains template placeholders",
			Target:   documentTarget,
		})
	}

	if len(strings.TrimSpace(document)) < 20 {
		result.Warnings = append(result.Warnings, Issue{
			Kind:     "short_document",
			Severity: "warning",
			Message:  "document XML is empty or unexpectedly short",
			Target:   documentTarget,
		})
	}

	result.Passed = len(result.FatalIssues) == 0 && len(result.RepairableIssues) == 0
	return result, nil
}
