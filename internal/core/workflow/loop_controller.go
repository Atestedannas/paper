package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/verify"
)

const maxPatchAttempts = 3

type LoopController struct {
	store       interface{}
	patchWriter PatchWriter
	verifier    Verifier
}

func NewLoopController(store interface{}, patchWriter PatchWriter, verifier Verifier) *LoopController {
	return &LoopController{
		store:       store,
		patchWriter: patchWriter,
		verifier:    verifier,
	}
}

func (c *LoopController) Run(ctx context.Context, input RunInput) (RunResult, error) {
	if ctx == nil {
		return RunResult{}, fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return RunResult{}, err
	}
	if strings.TrimSpace(input.OutputPath) == "" {
		return RunResult{}, fmt.Errorf("output path is empty")
	}
	if c == nil || c.verifier == nil {
		return RunResult{}, fmt.Errorf("verifier is nil")
	}

	current, err := c.verifier.Verify(ctx, input.OutputPath)
	if err != nil {
		return RunResult{}, err
	}
	result := RunResult{VerifyResult: current}
	seen := map[string]bool{}
	for {
		result.VerifyResult = current
		if current.Passed {
			result.Status = StatusVerifiedPass
			return result, nil
		}
		if len(current.FatalIssues) > 0 || len(current.RepairableIssues) == 0 || c.patchWriter == nil || result.Attempts >= maxPatchAttempts {
			result.Status = StatusManualReview
			return result, nil
		}
		fingerprint := issueFingerprint(current.RepairableIssues)
		if seen[fingerprint] {
			result.Status = StatusManualReview
			return result, nil
		}
		seen[fingerprint] = true
		summary, err := applyPatchWriter(ctx, c.patchWriter, input.OutputPath)
		if err != nil {
			return RunResult{}, err
		}
		result.Attempts++
		result.PatchSummary = append(result.PatchSummary, summary...)
		current, err = c.verifier.Verify(ctx, input.OutputPath)
		if err != nil {
			return RunResult{}, err
		}
	}
}

func applyPatchWriter(ctx context.Context, writer PatchWriter, path string) ([]string, error) {
	if staged, ok := writer.(StagedPatchWriter); ok {
		return staged.ApplyStages(ctx, path)
	}
	return nil, writer.Apply(ctx, path)
}

func issueFingerprint(issues []verify.Issue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, issue.Kind+"|"+issue.Target+"|"+issue.Message)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}
