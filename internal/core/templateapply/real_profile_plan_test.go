package templateapply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/repaircontract"
	"github.com/paper-format-checker/backend/internal/core/roleclassify"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

func TestRealSelectedTemplateFrozenPlanClosesWithoutStructureLoss(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	templatePath := filepath.Join(root, "uploads", "templates", "af53c666-6ed5-4a64-96a7-39f859c5d129", "template.docx")
	studentPath := filepath.Join(root, "uploads", "user.docx")
	for _, path := range []string{templatePath, studentPath} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("real regression fixture missing: %s", path)
		}
	}
	profile, err := templateprofile.Extract(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	studentAST, err := paperast.Extract(studentPath)
	if err != nil {
		t.Fatal(err)
	}
	studentAST.Nodes = roleclassify.EnforceDocumentTree(studentAST.Nodes)
	assignments := roleclassify.Freeze(studentAST.Nodes)
	if len(assignments) < 50 {
		t.Fatalf("too few stable assignments: %d", len(assignments))
	}
	before, err := repaircontract.CaptureStructure(studentPath)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "output.docx")
	data, err := os.ReadFile(studentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRoleFormatPlan(context.Background(), output, profile, assignments); err != nil {
		t.Fatal(err)
	}
	issues, err := ValidateRoleFormatPlan(context.Background(), output, profile, assignments)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		limit := len(issues)
		if limit > 10 {
			limit = 10
		}
		t.Fatalf("stable format plan did not close: first=%#v total=%d", issues[:limit], len(issues))
	}
	after, err := repaircontract.CaptureStructure(output)
	if err != nil {
		t.Fatal(err)
	}
	if issues := repaircontract.ValidateStructurePreserved(before, after); len(issues) != 0 {
		t.Fatalf("format plan damaged protected OOXML: %#v", issues)
	}
}
