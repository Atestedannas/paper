// Package evidence contains the bounded payloads that may be sent to an LLM.
// It deliberately represents observations, not compliance decisions: Go keeps
// ownership of rule validation, conflict handling and document modification.
package evidence

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/paperast"
)

const Version = "structured-evidence-v1"

const (
	maxEvidenceItems = 16
	maxEvidenceText  = 180
)

type Item struct {
	EvidenceID   string    `json:"evidence_id"`
	SourceType   string    `json:"source_type"`
	SourceNodeID string    `json:"source_node_id,omitempty"`
	Text         string    `json:"text,omitempty"`
	Property     string    `json:"property,omitempty"`
	RawValue     any       `json:"raw_value,omitempty"`
	RawUnit      string    `json:"raw_unit,omitempty"`
	Normalized   any       `json:"normalized_value,omitempty"`
	Unit         string    `json:"normalized_unit,omitempty"`
	Relation     string    `json:"relation,omitempty"`
	Page         int       `json:"page,omitempty"`
	BBox         []float64 `json:"bbox,omitempty"`
	SizePt       float64   `json:"size_pt,omitempty"`
	Confidence   float64   `json:"confidence,omitempty"`
}

type Subject struct {
	NodeID        string `json:"node_id"`
	SampleNodeID  string `json:"sample_node_id,omitempty"`
	CandidateRole string `json:"candidate_role"`
	Variant       string `json:"variant,omitempty"`
	Text          string `json:"text"`
}

type Request struct {
	Version        string  `json:"version"`
	Task           string  `json:"task"`
	DocumentID     string  `json:"document_id,omitempty"`
	Subject        Subject `json:"subject"`
	Evidence       []Item  `json:"evidence"`
	RequiredOutput struct {
		Format                    string `json:"format"`
		MustIncludeSourceIDs      bool   `json:"must_include_source_ids"`
		AllowUnsupportedInference bool   `json:"allow_unsupported_inference"`
	} `json:"required_output"`
}

// CompileRuleRequest constructs the exact bounded evidence envelope used for
// a single role/variant rule. Evidence is supplied by deterministic extractors
// (OOXML, PDF or layout services); this function never invents observations.
func CompileRuleRequest(documentID, role, variant, sampleNodeID, text string, items []Item) Request {
	r := Request{Version: Version, Task: "compile_format_rule", DocumentID: documentID,
		Subject: Subject{NodeID: sampleNodeID, SampleNodeID: sampleNodeID, CandidateRole: role, Variant: variant, Text: truncate(text, maxEvidenceText)}, Evidence: boundedEvidence(items)}
	r.RequiredOutput.Format = "json"
	r.RequiredOutput.MustIncludeSourceIDs = true
	r.RequiredOutput.AllowUnsupportedInference = false
	return r
}

// RoleRequest exposes only a candidate paragraph and a bounded structural
// context. It intentionally excludes current font/size/style observations:
// a role must survive malformed formatting so compliance can repair it later.
// This prevents the historical "send all 26 pages" behavior.
func RoleRequest(nodes []paperast.Node, index int, documentID string) (Request, bool) {
	return RoleRequestWithEvidence(nodes, index, documentID, nil)
}

// RoleRequestWithEvidence attaches only positional PDF facts to role
// classification. Rendered fonts and sizes intentionally remain outside this
// packet: they belong to compliance, not role recognition.
func RoleRequestWithEvidence(nodes []paperast.Node, index int, documentID string, pdfEvidence []Item) (Request, bool) {
	if index < 0 || index >= len(nodes) {
		return Request{}, false
	}
	n := nodes[index]
	if n.NodeType != "paragraph" || strings.TrimSpace(n.NodeID) == "" || strings.TrimSpace(n.Text) == "" {
		return Request{}, false
	}
	r := Request{Version: Version, Task: "classify_document_role", DocumentID: documentID,
		Subject: Subject{NodeID: n.NodeID, CandidateRole: n.SemanticRole, Text: truncate(n.Text, 180)}}
	r.RequiredOutput.Format = "json"
	r.RequiredOutput.MustIncludeSourceIDs = true
	r.RequiredOutput.AllowUnsupportedInference = false
	r.Evidence = append(r.Evidence, Item{EvidenceID: "ev:subject", SourceType: "ooxml_node", SourceNodeID: n.NodeID, Text: truncate(n.Text, maxEvidenceText), Confidence: n.Confidence})
	r.Evidence = append(r.Evidence,
		Item{EvidenceID: "ev:role", SourceType: "ooxml_structure", SourceNodeID: n.NodeID, Property: "candidate_role", RawValue: n.SemanticRole, Confidence: n.Confidence},
		Item{EvidenceID: "ev:section", SourceType: "ooxml_structure", SourceNodeID: n.NodeID, Property: "section_id", RawValue: n.SectionID, Confidence: .99},
		Item{EvidenceID: "ev:part", SourceType: "ooxml_structure", SourceNodeID: n.NodeID, Property: "source_part", RawValue: n.SourcePart, Confidence: .99},
	)
	if n.LogicalLevel > 0 {
		r.Evidence = append(r.Evidence, Item{EvidenceID: "ev:level", SourceType: "ooxml_structure", SourceNodeID: n.NodeID, Property: "logical_level", RawValue: n.LogicalLevel, Confidence: .99})
	}
	for evidenceIndex, fact := range n.Evidence {
		if evidenceIndex == 6 || strings.TrimSpace(fact) == "" {
			break
		}
		r.Evidence = append(r.Evidence, Item{EvidenceID: fmt.Sprintf("ev:structure:%d", evidenceIndex+1), SourceType: "ooxml_structure", SourceNodeID: n.NodeID, Property: "role_signal", Text: truncate(fact, 120), Confidence: n.Confidence})
	}
	for _, neighbor := range []int{index - 1, index + 1} {
		if neighbor < 0 || neighbor >= len(nodes) || nodes[neighbor].NodeType != "paragraph" {
			continue
		}
		pos := "previous"
		if neighbor > index {
			pos = "next"
		}
		r.Evidence = append(r.Evidence, Item{EvidenceID: "ev:" + pos, SourceType: "ooxml_neighbor", SourceNodeID: nodes[neighbor].NodeID, Text: truncate(nodes[neighbor].Text, 120), Property: pos + "_paragraph", Confidence: nodes[neighbor].Confidence})
	}
	positionFacts := 0
	// Prefer a node/page union over arbitrary wrapped lines.  Line evidence is
	// retained for audit, but role classification needs one stable geometry.
	for _, property := range []string{"bbox_union", "normalized_bbox_union", "page_region", "bbox", "normalized_bbox"} {
		for _, item := range pdfEvidence {
			if item.SourceType != "pdf_observation" || item.SourceNodeID != n.NodeID || item.Page <= 0 || item.Property != property {
				continue
			}
			if (property == "bbox_union" || property == "normalized_bbox_union" || property == "bbox" || property == "normalized_bbox") && len(item.BBox) != 4 {
				continue
			}
			if property == "page_region" && strings.TrimSpace(fmt.Sprint(item.RawValue)) == "" {
				continue
			}
			r.Evidence = append(r.Evidence, item)
			positionFacts++
			if positionFacts == 3 {
				break
			}
		}
		if positionFacts == 3 {
			break
		}
	}
	r.Evidence = boundedEvidence(r.Evidence)
	return r, true
}

// boundedEvidence is the last common guard before a packet becomes a model
// prompt. The compiler's role-specific selection is more precise; this guard
// prevents a future caller from accidentally serializing a document-sized item.
func boundedEvidence(items []Item) []Item {
	result := make([]Item, 0, min(len(items), maxEvidenceItems))
	seen := map[string]bool{}
	for _, item := range items {
		if len(result) == maxEvidenceItems {
			break
		}
		item.EvidenceID = truncate(item.EvidenceID, 160)
		if item.EvidenceID == "" || seen[item.EvidenceID] {
			continue
		}
		seen[item.EvidenceID] = true
		item.Text = truncate(item.Text, maxEvidenceText)
		if len(item.BBox) != 4 {
			item.BBox = nil
		}
		if item.Page < 0 {
			item.Page = 0
		}
		result = append(result, item)
	}
	return result
}

// CandidateRole is the only LLM result accepted for role classification.
// The source IDs must refer to evidence supplied in the request.
type CandidateRole struct {
	NodeID            string   `json:"node_id"`
	Role              string   `json:"role"`
	Confidence        float64  `json:"confidence"`
	SourceEvidenceIDs []string `json:"source_evidence_ids"`
	Uncertainties     []string `json:"uncertainties,omitempty"`
}

// CandidateRule is deliberately a proposal. The constraint map is accepted
// only after Go validates both its source evidence and its fixed vocabulary.
type CandidateRule struct {
	RuleID            string         `json:"rule_id"`
	Selector          RuleSelector   `json:"selector,omitempty"`
	Role              string         `json:"role"`
	Variant           string         `json:"variant,omitempty"`
	Constraints       map[string]any `json:"constraints"`
	SourceEvidenceIDs []string       `json:"source_evidence_ids"`
	Confidence        float64        `json:"confidence"`
	Uncertainties     []string       `json:"uncertainties,omitempty"`
}

type RuleSelector struct {
	Role    string `json:"role"`
	Variant string `json:"variant,omitempty"`
}

var allowedRuleConstraints = map[string]bool{
	"font.east_asia": true, "font.ascii": true, "size_pt": true,
	"font":      true,
	"alignment": true, "spacing_before_pt": true, "spacing_after_pt": true,
	"line_spacing_pt": true, "line_rule": true, "page_break_before": true,
}

var ruleIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func ValidateRuleCandidate(request Request, candidate CandidateRule) error {
	var err error
	candidate, err = normalizeRuleCandidate(candidate)
	if err != nil {
		return err
	}
	if request.Task != "compile_format_rule" || candidate.Role != request.Subject.CandidateRole {
		return fmt.Errorf("candidate rule subject does not match structured request")
	}
	if !ruleIDPattern.MatchString(candidate.RuleID) || candidate.Confidence < 0 || candidate.Confidence > 1 {
		return fmt.Errorf("candidate rule identity or confidence is invalid")
	}
	if request.Subject.Variant != "" && candidate.Variant != request.Subject.Variant {
		return fmt.Errorf("candidate rule variant does not match structured request")
	}
	if len(candidate.Constraints) == 0 {
		return fmt.Errorf("candidate rule has no constraints")
	}
	if len(candidate.SourceEvidenceIDs) == 0 {
		return fmt.Errorf("candidate rule has no source evidence ids")
	}
	allowedEvidence := map[string]bool{}
	for _, item := range request.Evidence {
		allowedEvidence[item.EvidenceID] = true
	}
	for _, id := range candidate.SourceEvidenceIDs {
		if !allowedEvidence[id] {
			return fmt.Errorf("candidate rule cites unknown evidence %q", id)
		}
	}
	for key, value := range candidate.Constraints {
		if key == "font" {
			font, ok := value.(map[string]any)
			if !ok || len(font) == 0 {
				return fmt.Errorf("candidate font constraint must be an object")
			}
			for fontKey, fontValue := range font {
				if fontKey != "east_asia" && fontKey != "ascii" {
					return fmt.Errorf("candidate font has unsupported property %q", fontKey)
				}
				if !validString(fontValue) {
					return fmt.Errorf("candidate font %s must be a non-empty string", fontKey)
				}
			}
			continue
		}
		if !allowedRuleConstraints[key] {
			return fmt.Errorf("candidate rule has unsupported constraint %q", key)
		}
		if err := validateConstraint(key, value); err != nil {
			return err
		}
	}
	if err := validateTypedConstraintCitations(request, candidate); err != nil {
		return err
	}
	return nil
}

// validateTypedConstraintCitations prevents a model from citing an unrelated
// fact (for example, a font-size observation) to invent an alignment rule. If
// the packet contains a typed observation for a claimed property, the model
// must cite a same-valued observation for that property. Requirement prose is
// still allowed when no typed observation exists for that property.
func validateTypedConstraintCitations(request Request, candidate CandidateRule) error {
	cited := map[string]bool{}
	for _, id := range candidate.SourceEvidenceIDs {
		cited[id] = true
	}
	for key, value := range candidate.Constraints {
		if key == "font" {
			font, _ := value.(map[string]any)
			for name, fontValue := range font {
				if err := validateTypedPropertyCitation(request.Evidence, cited, "font."+name, fontValue); err != nil {
					return err
				}
			}
			continue
		}
		if err := validateTypedPropertyCitation(request.Evidence, cited, key, value); err != nil {
			return err
		}
	}
	return nil
}

func validateTypedPropertyCitation(items []Item, cited map[string]bool, property string, expected any) error {
	foundTyped := false
	foundNonPDF := false
	matchedNonPDF := false
	matchedPDF := false
	for _, item := range items {
		if item.Property != property || item.SourceType == "requirement_text" || item.SourceType == "instruction_textbox" {
			continue
		}
		foundTyped = true
		if item.SourceType != "pdf_observation" {
			foundNonPDF = true
		}
		if !cited[item.EvidenceID] || !evidenceValueMatches(item, expected) {
			continue
		}
		if item.SourceType == "pdf_observation" {
			matchedPDF = true
			continue
		}
		matchedNonPDF = true
	}
	if matchedNonPDF {
		return nil
	}
	if foundNonPDF {
		return fmt.Errorf("candidate %s must cite a matching typed observation", property)
	}
	if foundTyped && matchedPDF {
		return fmt.Errorf("candidate %s cannot be inferred from PDF evidence alone", property)
	}
	if foundTyped {
		return fmt.Errorf("candidate %s must cite a matching typed observation", property)
	}
	return nil
}

func evidenceValueMatches(item Item, expected any) bool {
	actual := item.Normalized
	if actual == nil {
		actual = item.RawValue
	}
	if wantNumber, ok := finiteNumber(expected); ok {
		gotNumber, gotOK := finiteNumber(actual)
		return gotOK && math.Abs(wantNumber-gotNumber) < 0.000001
	}
	return strings.EqualFold(strings.TrimSpace(fmt.Sprint(actual)), strings.TrimSpace(fmt.Sprint(expected)))
}

func NormalizeRuleCandidate(candidate CandidateRule) (CandidateRule, error) {
	return normalizeRuleCandidate(candidate)
}

func normalizeRuleCandidate(candidate CandidateRule) (CandidateRule, error) {
	if candidate.Selector.Role == "" {
		return candidate, nil
	}
	if candidate.Role != "" && candidate.Role != candidate.Selector.Role {
		return candidate, fmt.Errorf("candidate selector role conflicts with candidate role")
	}
	if candidate.Variant != "" && candidate.Selector.Variant != "" && candidate.Variant != candidate.Selector.Variant {
		return candidate, fmt.Errorf("candidate selector variant conflicts with candidate variant")
	}
	candidate.Role = candidate.Selector.Role
	if candidate.Variant == "" {
		candidate.Variant = candidate.Selector.Variant
	}
	return candidate, nil
}

func validateConstraint(key string, value any) error {
	switch key {
	case "font.east_asia", "font.ascii":
		if !validString(value) {
			return fmt.Errorf("candidate %s must be a non-empty string", key)
		}
	case "size_pt":
		if n, ok := finiteNumber(value); !ok || n < 1 || n > 200 {
			return fmt.Errorf("candidate size_pt must be a number from 1 to 200")
		}
	case "spacing_before_pt", "spacing_after_pt", "line_spacing_pt":
		if n, ok := finiteNumber(value); !ok || n < 0 || n > 1000 {
			return fmt.Errorf("candidate %s must be a number from 0 to 1000", key)
		}
	case "alignment":
		if value, ok := value.(string); !ok || !map[string]bool{"left": true, "right": true, "center": true, "both": true, "justify": true, "distribute": true}[value] {
			return fmt.Errorf("candidate alignment is invalid")
		}
	case "line_rule":
		if value, ok := value.(string); !ok || !map[string]bool{"auto": true, "exact": true, "atLeast": true}[value] {
			return fmt.Errorf("candidate line_rule is invalid")
		}
	case "page_break_before":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("candidate page_break_before must be boolean")
		}
	}
	return nil
}

func validString(value any) bool {
	s, ok := value.(string)
	return ok && strings.TrimSpace(s) != "" && len([]rune(s)) <= 128
}

func finiteNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case float32:
		return float64(v), !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
	default:
		return 0, false
	}
}

func ValidateRoleCandidate(request Request, candidate CandidateRole) error {
	if request.Task != "classify_document_role" || candidate.NodeID != request.Subject.NodeID {
		return fmt.Errorf("candidate subject does not match structured request")
	}
	if candidate.Confidence < 0 || candidate.Confidence > 1 || strings.TrimSpace(candidate.Role) == "" {
		return fmt.Errorf("candidate role or confidence is invalid")
	}
	allowed := map[string]bool{}
	for _, item := range request.Evidence {
		allowed[item.EvidenceID] = true
	}
	if len(candidate.SourceEvidenceIDs) == 0 {
		return fmt.Errorf("candidate has no source evidence ids")
	}
	for _, id := range candidate.SourceEvidenceIDs {
		if !allowed[id] {
			return fmt.Errorf("candidate cites unknown evidence %q", id)
		}
	}
	return nil
}

func truncate(text string, max int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}
