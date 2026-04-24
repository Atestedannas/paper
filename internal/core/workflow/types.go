package workflow

import (
	"context"

	"github.com/paper-format-checker/backend/internal/core/verify"
)

type Status string

const (
	StatusUploaded      Status = "uploaded"
	StatusPatched       Status = "patched"
	StatusVerifiedPass  Status = "verified_pass"
	StatusManualReview  Status = "manual_review"
	StageVerified       string = "verified"
	StageManualReview   string = "manual_review"
	StagePatchAttempted string = "patch_attempted"
)

type RunInput struct {
	OutputPath string
}

type RunResult struct {
	Status       Status
	VerifyResult verify.Result
	Attempts     int
}

type PatchWriter interface {
	Apply(context.Context, string) error
}

type Verifier interface {
	Verify(context.Context, string) (verify.Result, error)
}
