package workflow

import (
	"context"
	"fmt"
	"strings"
)

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

	firstResult, err := c.verifier.Verify(ctx, input.OutputPath)
	if err != nil {
		return RunResult{}, err
	}
	if firstResult.Passed {
		return RunResult{Status: StatusVerifiedPass, VerifyResult: firstResult}, nil
	}
	if len(firstResult.FatalIssues) > 0 {
		return RunResult{Status: StatusManualReview, VerifyResult: firstResult}, nil
	}
	if len(firstResult.RepairableIssues) == 0 || c.patchWriter == nil {
		return RunResult{Status: StatusManualReview, VerifyResult: firstResult}, nil
	}

	if err := c.patchWriter.Apply(ctx, input.OutputPath); err != nil {
		return RunResult{}, err
	}

	secondResult, err := c.verifier.Verify(ctx, input.OutputPath)
	if err != nil {
		return RunResult{}, err
	}
	if secondResult.Passed {
		return RunResult{Status: StatusVerifiedPass, VerifyResult: secondResult, Attempts: 1}, nil
	}

	return RunResult{Status: StatusManualReview, VerifyResult: secondResult, Attempts: 1}, nil
}
