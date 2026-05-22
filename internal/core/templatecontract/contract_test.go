package templatecontract

import (
	"encoding/json"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

func TestBuildCreatesExecutableRuleSetFromTemplateProfile(t *testing.T) {
	profile := &templateprofile.Profile{
		Source:      "local+deepseek",
		TemplateSHA: "abc123",
		Confidence:  0.91,
		Sections: map[string]templateprofile.SectionRule{
			"references_title": {Label: "references_title", PageBreakBefore: true, DetectedFrom: "current_paragraph"},
		},
		Styles: map[string]templateprofile.StyleRule{
			"body": {Label: "body", FontEastAsia: "宋体", FontASCII: "Times New Roman", FontSizeHalfPt: "24", Line: "360"},
		},
		Header: templateprofile.HeaderFooterRule{Exists: true, HasDoubleLine: true},
		Footer: templateprofile.HeaderFooterRule{Exists: true, HasPageField: true, HasNumPages: true},
	}

	rules := Build(profile)

	if rules.Version != Version {
		t.Fatalf("Version = %s, want %s", rules.Version, Version)
	}
	if rules.Confirmed {
		t.Fatal("auto extracted rules must not be marked confirmed")
	}
	if rules.Verification.StrictFailurePolicy != "reject_compliance_on_any_error" {
		t.Fatalf("StrictFailurePolicy = %s", rules.Verification.StrictFailurePolicy)
	}
	if !rules.Sections["references_title"].PageBreakBefore {
		t.Fatalf("references page-break rule missing: %#v", rules.Sections)
	}
	if !rules.Header.HasDoubleLine || !rules.Footer.HasPageField {
		t.Fatalf("header/footer rules not preserved: %#v %#v", rules.Header, rules.Footer)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(Marshal(rules)), &decoded); err != nil {
		t.Fatalf("Marshal output is invalid JSON: %v", err)
	}
	if issues := Validate(rules); len(issues) != 0 {
		t.Fatalf("Validate() issues = %#v, want none", issues)
	}
}

func TestValidateRejectsIncompleteRuleSet(t *testing.T) {
	issues := Validate(RuleSet{Version: Version})

	if len(issues) == 0 {
		t.Fatal("Validate() issues = nil, want strict policy/artifact issues")
	}
}
