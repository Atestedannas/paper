package templatecontract

import (
	"encoding/json"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

const Version = "template-rule-v1"

type RuleSet struct {
	Version           string                                 `json:"version"`
	Source            string                                 `json:"source"`
	TemplateSHA       string                                 `json:"template_sha,omitempty"`
	Confirmed         bool                                   `json:"confirmed"`
	Confidence        float64                                `json:"confidence"`
	Sections          map[string]templateprofile.SectionRule `json:"sections"`
	Styles            map[string]templateprofile.StyleRule   `json:"styles"`
	Header            templateprofile.HeaderFooterRule       `json:"header"`
	Footer            templateprofile.HeaderFooterRule       `json:"footer"`
	Verification      VerificationRules                      `json:"verification"`
	DeterministicOnly bool                                   `json:"deterministic_only"`
	Evidence          map[string]string                      `json:"evidence,omitempty"`
	RawAI             map[string]interface{}                 `json:"raw_ai,omitempty"`
}

type VerificationRules struct {
	RequireOOXMLReadable        bool     `json:"require_ooxml_readable"`
	RequireNoPlaceholders       bool     `json:"require_no_placeholders"`
	RequireTemplateProfileMatch bool     `json:"require_template_profile_match"`
	StrictFailurePolicy         string   `json:"strict_failure_policy"`
	RequiredArtifacts           []string `json:"required_artifacts"`
}

type ValidationIssue struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func Build(profile *templateprofile.Profile) RuleSet {
	rules := RuleSet{
		Version:           Version,
		Source:            "empty",
		Confirmed:         false,
		Confidence:        0,
		Sections:          map[string]templateprofile.SectionRule{},
		Styles:            map[string]templateprofile.StyleRule{},
		Verification:      defaultVerificationRules(),
		DeterministicOnly: true,
		Evidence:          map[string]string{},
	}
	if profile == nil {
		return rules
	}
	rules.Source = strings.TrimSpace(profile.Source)
	if rules.Source == "" {
		rules.Source = "local"
	}
	rules.TemplateSHA = profile.TemplateSHA
	rules.Confidence = profile.Confidence
	rules.Sections = cloneSections(profile.Sections)
	rules.Styles = cloneStyles(profile.Styles)
	rules.Header = profile.Header
	rules.Footer = profile.Footer
	for key, rule := range rules.Sections {
		if rule.DetectedFrom != "" {
			rules.Evidence["section."+key] = rule.DetectedFrom
		}
	}
	if profile.AI != nil && profile.AI.RawJSON != nil {
		rules.RawAI = profile.AI.RawJSON
	}
	return rules
}

func Marshal(rules RuleSet) string {
	data, err := json.Marshal(rules)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func Parse(data string) (RuleSet, error) {
	if strings.TrimSpace(data) == "" {
		return Build(nil), nil
	}
	var rules RuleSet
	if err := json.Unmarshal([]byte(data), &rules); err != nil {
		return RuleSet{}, err
	}
	return rules, nil
}

func Validate(rules RuleSet) []ValidationIssue {
	var issues []ValidationIssue
	if rules.Version != Version {
		issues = append(issues, ValidationIssue{Kind: "template_rule_version", Message: "template rule version is missing or unsupported"})
	}
	if rules.Verification.StrictFailurePolicy != "reject_compliance_on_any_error" {
		issues = append(issues, ValidationIssue{Kind: "template_rule_policy", Message: "strict failure policy must reject compliance on any error"})
	}
	if len(rules.Verification.RequiredArtifacts) == 0 {
		issues = append(issues, ValidationIssue{Kind: "template_rule_artifacts", Message: "required closure artifacts are not declared"})
	}
	for _, artifact := range []string{"template_rule_json", "paper_ast_snapshot", "repair_contract", "verify_result"} {
		if !containsArtifact(rules.Verification.RequiredArtifacts, artifact) {
			issues = append(issues, ValidationIssue{Kind: "template_rule_artifacts", Message: "required artifact is missing: " + artifact})
		}
	}
	if !rules.DeterministicOnly {
		issues = append(issues, ValidationIssue{Kind: "template_rule_determinism", Message: "template rule must be deterministic-only for compliance output"})
	}
	return issues
}

func defaultVerificationRules() VerificationRules {
	return VerificationRules{
		RequireOOXMLReadable:        true,
		RequireNoPlaceholders:       true,
		RequireTemplateProfileMatch: true,
		StrictFailurePolicy:         "reject_compliance_on_any_error",
		RequiredArtifacts: []string{
			"template_rule_json",
			"paper_ast_snapshot",
			"repair_contract",
			"verify_result",
		},
	}
}

func cloneSections(in map[string]templateprofile.SectionRule) map[string]templateprofile.SectionRule {
	out := map[string]templateprofile.SectionRule{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStyles(in map[string]templateprofile.StyleRule) map[string]templateprofile.StyleRule {
	out := map[string]templateprofile.StyleRule{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func containsArtifact(artifacts []string, want string) bool {
	for _, artifact := range artifacts {
		if artifact == want {
			return true
		}
	}
	return false
}
