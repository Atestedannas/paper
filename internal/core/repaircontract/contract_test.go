package repaircontract

import (
	"testing"

	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/templatecontract"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

func TestBuildCreatesDeterministicRepairContract(t *testing.T) {
	t.Setenv("CQRWST_ALLOW_CONTENT_NORMALIZATION", "false")
	rules := templatecontract.Build(&templateprofile.Profile{
		Sections: map[string]templateprofile.SectionRule{"body_start": {Label: "body_start"}},
		Styles:   map[string]templateprofile.StyleRule{"body": {Label: "body"}},
	})
	ast := paperast.Snapshot{
		Version: paperast.Version,
		Stats:   paperast.Stats{Headings: 2},
		Nodes:   []paperast.Node{{NodeID: "n_000001"}, {NodeID: "n_000002"}},
	}

	contract := Build(rules, ast)

	if contract.Version != Version {
		t.Fatalf("Version = %s, want %s", contract.Version, Version)
	}
	if len(contract.Steps) != 5 {
		t.Fatalf("Steps len = %d, want 5", len(contract.Steps))
	}
	if !hasStep(contract, "verify_before_download") || !hasStep(contract, "render_and_regression_gate") {
		t.Fatalf("verification gate missing: %#v", contract.Steps)
	}
	if !hasBlockedActionForTest(contract, "visible_content_rewrite") {
		t.Fatalf("content rewrite must be blocked by default: %#v", contract.Blocked)
	}
	if contract.Stats["ast_nodes"] != 2 || contract.Stats["template_sections"] != 1 {
		t.Fatalf("stats not populated: %#v", contract.Stats)
	}
	if issues := Validate(contract); len(issues) != 0 {
		t.Fatalf("Validate() issues = %#v, want none", issues)
	}
}

func TestBuildHonorsExplicitContentNormalizationOverride(t *testing.T) {
	t.Setenv("CQRWST_ALLOW_CONTENT_NORMALIZATION", "true")

	contract := Build(templatecontract.Build(nil), paperast.Snapshot{Version: paperast.Version})

	if hasBlockedActionForTest(contract, "visible_content_rewrite") {
		t.Fatalf("visible content rewrite should not be listed as blocked when explicitly enabled: %#v", contract.Blocked)
	}
	if contract.Stats["content_normalization_override"] != true {
		t.Fatalf("override flag missing: %#v", contract.Stats)
	}
}

func TestValidateRejectsContractWithoutVerificationGate(t *testing.T) {
	t.Setenv("CQRWST_ALLOW_CONTENT_NORMALIZATION", "false")

	issues := Validate(Contract{
		Version: Version,
		Mode:    "template_driven_deterministic",
		Blocked: []BlockedAction{
			{Action: "visible_content_rewrite"},
			{Action: "ai_self_certified_compliance"},
		},
		Artifacts: []string{"template_rule_json", "paper_ast_snapshot", "repair_contract", "verify_result"},
	})

	if len(issues) == 0 {
		t.Fatal("Validate() issues = nil, want missing verification gate issue")
	}
}

func hasBlockedActionForTest(contract Contract, action string) bool {
	for _, blocked := range contract.Blocked {
		if blocked.Action == action {
			return true
		}
	}
	return false
}
