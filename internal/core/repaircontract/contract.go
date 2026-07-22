package repaircontract

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/templatecontract"
)

const Version = "repair-contract-v1"

var generatedTOCPagePattern = regexp.MustCompile(`^(\d+(?:\.\d+)*\s+\S.*?)\d+$`)

type Contract struct {
	Version       string                 `json:"version"`
	TemplateRules string                 `json:"template_rules_version"`
	PaperAST      string                 `json:"paper_ast_version"`
	Mode          string                 `json:"mode"`
	Steps         []Step                 `json:"steps"`
	Blocked       []BlockedAction        `json:"blocked_actions"`
	Targets       []TargetPolicy         `json:"targets"`
	Artifacts     []string               `json:"artifacts"`
	Stats         map[string]interface{} `json:"stats"`
}

type Step struct {
	ID          string   `json:"id"`
	Engine      string   `json:"engine"`
	Determinism string   `json:"determinism"`
	Inputs      []string `json:"inputs"`
	Outputs     []string `json:"outputs"`
	Policy      string   `json:"policy"`
}

type BlockedAction struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

type TargetPolicy struct {
	NodeID         string   `json:"node_id"`
	SourcePart     string   `json:"source_part"`
	Index          int      `json:"index"`
	SemanticRole   string   `json:"semantic_role"`
	AllowedActions []string `json:"allowed_actions"`
}

type ValidationIssue struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func Build(rules templatecontract.RuleSet, ast paperast.Snapshot) Contract {
	contract := Contract{
		Version:       Version,
		TemplateRules: rules.Version,
		PaperAST:      ast.Version,
		Mode:          "template_driven_deterministic",
		Steps: []Step{
			{
				ID:          "preserve_source_content_and_cover_layout",
				Engine:      "docx-copy-then-repair",
				Determinism: "required",
				Inputs:      []string{"student_docx"},
				Outputs:     []string{"working_docx"},
				Policy:      "do_not_transplant_skeleton_unless_explicitly_enabled",
			},
			{
				ID:          "apply_template_sections",
				Engine:      "cqrwst-rulepack",
				Determinism: "required",
				Inputs:      []string{"template_rule_json", "paper_ast_snapshot"},
				Outputs:     []string{"section_breaks", "page_number_scopes"},
				Policy:      "only_insert_page_breaks_when_template_rule_requires",
			},
			{
				ID:          "apply_template_styles",
				Engine:      "cqrwst-rulepack",
				Determinism: "required",
				Inputs:      []string{"template_rule_json", "paper_ast_snapshot"},
				Outputs:     []string{"paragraph_styles", "header_footer_styles"},
				Policy:      "style_first_no_content_rewrite",
			},
			{
				ID:          "verify_before_download",
				Engine:      "workflow-verifier",
				Determinism: "required",
				Inputs:      []string{"final_docx", "template_rule_json"},
				Outputs:     []string{"verify_result"},
				Policy:      "no_download_unless_verified_pass",
			},
			{
				ID:          "render_and_regression_gate",
				Engine:      "libreoffice-renderer+golden-regression",
				Determinism: "required_when_enabled",
				Inputs:      []string{"final_docx", "golden_template_docx", "paper_ast_snapshot"},
				Outputs:     []string{"render_result", "golden_regression_result"},
				Policy:      "rendered layout drift blocks compliance when render verification is enabled",
			},
		},
		Blocked: []BlockedAction{
			{
				Action: "visible_content_rewrite",
				Reason: "format system may change layout and styles, not thesis content",
			},
			{
				Action: "ai_self_certified_compliance",
				Reason: "compliance must be decided by deterministic verifier",
			},
		},
		Artifacts: []string{
			"template_rule_json",
			"paper_ast_snapshot",
			"repair_contract",
			"render_result",
			"golden_regression_result",
			"verify_result",
		},
		Stats: map[string]interface{}{
			"ast_nodes":         len(ast.Nodes),
			"ast_headings":      ast.Stats.Headings,
			"template_styles":   len(rules.Styles),
			"template_sections": len(rules.Sections),
		},
	}
	contract.Targets = make([]TargetPolicy, 0, len(ast.Nodes))
	for _, node := range ast.Nodes {
		contract.Targets = append(contract.Targets, TargetPolicy{
			NodeID: node.NodeID, SourcePart: node.SourcePart, Index: node.Index, SemanticRole: node.SemanticRole,
			AllowedActions: []string{"style_update", "layout_update"},
		})
	}
	if contentNormalizationEnabled() {
		contract.Blocked = removeBlocked(contract.Blocked, "visible_content_rewrite")
		contract.Stats["content_normalization_override"] = true
	}
	return contract
}

func (contract Contract) Blocks(action string) bool {
	return hasBlockedAction(contract, action)
}

func ValidateVisibleContentPreserved(before, after paperast.Snapshot) []ValidationIssue {
	remaining := visibleNodeTexts(after.Nodes)
	position := 0
	var issues []ValidationIssue
	for _, text := range visibleNodeTexts(before.Nodes) {
		found := false
		for position < len(remaining) {
			if remaining[position] == text {
				found = true
				position++
				break
			}
			position++
		}
		if !found {
			issues = append(issues, ValidationIssue{Kind: "visible_content_rewrite", Message: "repair removed, reordered, or rewrote visible content: " + text})
			break
		}
	}
	return issues
}

func visibleNodeTexts(nodes []paperast.Node) []string {
	texts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.SectionID == "toc" {
			continue
		}
		text := strings.TrimSpace(node.Text)
		if match := generatedTOCPagePattern.FindStringSubmatch(text); len(match) == 2 {
			text = match[1]
		}
		if text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

func Marshal(contract Contract) string {
	data, err := json.Marshal(contract)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func Parse(data string) (Contract, error) {
	if strings.TrimSpace(data) == "" {
		return Contract{Version: Version}, nil
	}
	var contract Contract
	if err := json.Unmarshal([]byte(data), &contract); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

func Validate(contract Contract) []ValidationIssue {
	var issues []ValidationIssue
	if contract.Version != Version {
		issues = append(issues, ValidationIssue{Kind: "repair_contract_version", Message: "repair contract version is missing or unsupported"})
	}
	if contract.Mode != "template_driven_deterministic" {
		issues = append(issues, ValidationIssue{Kind: "repair_contract_mode", Message: "repair contract must use template-driven deterministic mode"})
	}
	for _, artifact := range []string{"template_rule_json", "paper_ast_snapshot", "repair_contract", "render_result", "golden_regression_result", "verify_result"} {
		if !containsString(contract.Artifacts, artifact) {
			issues = append(issues, ValidationIssue{Kind: "repair_contract_artifacts", Message: "repair contract artifact is missing: " + artifact})
		}
	}
	if !hasStep(contract, "verify_before_download") {
		issues = append(issues, ValidationIssue{Kind: "repair_contract_verification_gate", Message: "repair contract must include verify_before_download step"})
	}
	if !hasStep(contract, "render_and_regression_gate") {
		issues = append(issues, ValidationIssue{Kind: "repair_contract_render_gate", Message: "repair contract must include render_and_regression_gate step"})
	}
	if !contentNormalizationEnabled() && !hasBlockedAction(contract, "visible_content_rewrite") {
		issues = append(issues, ValidationIssue{Kind: "repair_contract_content_guard", Message: "visible content rewrite must be blocked unless explicitly enabled"})
	}
	if !hasBlockedAction(contract, "ai_self_certified_compliance") {
		issues = append(issues, ValidationIssue{Kind: "repair_contract_ai_guard", Message: "AI self-certified compliance must be blocked"})
	}
	return issues
}

func hasStep(contract Contract, id string) bool {
	for _, step := range contract.Steps {
		if step.ID == id {
			return true
		}
	}
	return false
}

func hasBlockedAction(contract Contract, action string) bool {
	for _, blocked := range contract.Blocked {
		if blocked.Action == action {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func contentNormalizationEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CQRWST_ALLOW_CONTENT_NORMALIZATION"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func removeBlocked(blocked []BlockedAction, action string) []BlockedAction {
	out := make([]BlockedAction, 0, len(blocked))
	for _, item := range blocked {
		if item.Action != action {
			out = append(out, item)
		}
	}
	return out
}
