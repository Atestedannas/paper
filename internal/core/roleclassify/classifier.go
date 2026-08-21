package roleclassify

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/evidence"
	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

type Client interface{ ChatCompletion(string) (string, error) }

type Assignment struct {
	NodeID     string   `json:"nodeId"`
	Role       string   `json:"role"`
	Confidence float64  `json:"confidence"`
	Evidence   []string `json:"evidence,omitempty"`
	Trusted    bool     `json:"-"`
	Index      int      `json:"-"`
}

// ClassifyStructured is the production LLM boundary. It submits only short,
// ambiguous candidate nodes with local OOXML evidence, never the entire paper.
// Classify remains for compatibility with historical callers/tests.
func ClassifyStructured(ctx context.Context, client Client, nodes []paperast.Node) ([]Assignment, error) {
	return ClassifyStructuredWithEvidence(ctx, client, nodes, nil)
}

// ClassifyStructuredWithEvidence uses pre-format PDF coordinates as a
// structural cue while keeping font/size observations out of role decisions.
func ClassifyStructuredWithEvidence(ctx context.Context, client Client, nodes []paperast.Node, pdfEvidenceByNode map[string][]evidence.Item) ([]Assignment, error) {
	if ctx == nil || client == nil {
		return nil, fmt.Errorf("role classifier context or client is nil")
	}
	requests := make([]evidence.Request, 0, 24)
	for i, node := range nodes {
		if len(requests) == 24 { // a hard request budget per document stage
			break
		}
		if !needsSemanticArbitration(node) {
			continue
		}
		if request, ok := evidence.RoleRequestWithEvidence(nodes, i, "student-paper", pdfEvidenceByNode[node.NodeID]); ok {
			requests = append(requests, request)
		}
	}
	if len(requests) == 0 {
		return nil, nil
	}
	assignments := make([]Assignment, 0, len(requests))
	for _, request := range requests {
		payload, err := json.Marshal(request)
		if err != nil {
			return nil, err
		}
		prompt := `You classify exactly one thesis node subject from structured evidence. Return JSON only. Do not invent format values. A source_evidence_ids item must be copied from this subject's evidence. If evidence is insufficient choose unknown.
Allowed roles: cover, cover_title, cover_date, title, originality_declaration, abstract_title, abstract_body, abstract_cn, abstract_en, abstract_en_body, keywords_cn, keywords_en, toc_title, toc_entry, heading_1, heading_2, heading_3, heading_4, body, table_caption, figure_caption, formula, references_title, references, acknowledgements_title, acknowledgements, appendix_title, appendix, unknown.
Format: {"node_id":"...","role":"...","confidence":0.0,"source_evidence_ids":["ev:subject"],"uncertainties":[]}
Subject:
` + string(payload)
		response, err := client.ChatCompletion(prompt)
		if err != nil {
			return nil, err
		}
		response = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(response), "```json"), "```"), "```"))
		var candidate evidence.CandidateRole
		if err := json.Unmarshal([]byte(response), &candidate); err != nil {
			return nil, fmt.Errorf("structured role classifier invalid JSON: %w", err)
		}
		if candidate.NodeID != request.Subject.NodeID {
			return nil, fmt.Errorf("structured role classifier returned an unrequested node %q", candidate.NodeID)
		}
		if !validRoles[candidate.Role] {
			return nil, fmt.Errorf("structured role classifier returned invalid role %q", candidate.Role)
		}
		if err := evidence.ValidateRoleCandidate(request, candidate); err != nil {
			return nil, err
		}
		evidenceRefs := make([]string, 0, len(candidate.SourceEvidenceIDs)+1)
		for _, id := range candidate.SourceEvidenceIDs {
			evidenceRefs = append(evidenceRefs, "deepseek_evidence:"+id)
		}
		if len(candidate.Uncertainties) > 0 {
			evidenceRefs = append(evidenceRefs, "deepseek_uncertainty:"+strings.Join(candidate.Uncertainties, " | "))
		}
		assignments = append(assignments, Assignment{NodeID: candidate.NodeID, Role: candidate.Role, Confidence: candidate.Confidence, Evidence: evidenceRefs})
	}
	return assignments, nil
}

func needsSemanticArbitration(node paperast.Node) bool {
	if node.NodeType != "paragraph" || strings.TrimSpace(node.Text) == "" || node.SectionID == "toc" || (node.SourcePart != "" && node.SourcePart != "word/document.xml") {
		return false
	}
	// Do not ask the model to relabel reliable deterministic markers. For weak
	// short paragraphs, however, the format may be wrong and role still matters.
	if node.Confidence >= .9 && node.SemanticRole != "body_paragraph" {
		return false
	}
	return len([]rune(strings.TrimSpace(node.Text))) <= 100
}

type ValidationIssue struct {
	NodeID  string `json:"nodeId"`
	Message string `json:"message"`
}

var validRoles = map[string]bool{
	"cover": true, "cover_title": true, "cover_date": true, "title": true, "originality_declaration": true,
	"abstract_title": true, "abstract_body": true, "abstract_cn": true,
	"abstract_en": true, "abstract_en_body": true, "keywords_cn": true,
	"keywords_en": true, "toc_title": true, "toc_entry": true,
	"heading_1": true, "heading_2": true, "heading_3": true, "heading_4": true,
	"body": true, "body_paragraph": true, "table_caption": true, "figure_caption": true, "formula": true, "references_title": true,
	"references": true, "acknowledgements_title": true, "acknowledgements": true,
	"appendix_title": true, "appendix": true, "unknown": true,
}

type inputNode struct {
	NodeID      string `json:"nodeId"`
	Text        string `json:"text"`
	CurrentRole string `json:"currentRole"`
	Level       int    `json:"level,omitempty"`
	Section     string `json:"section,omitempty"`
	Index       int    `json:"index"`
}

// Classify is intentionally disabled. Its historical implementation serialized
// all document nodes into one prompt; callers must use ClassifyStructured.
func Classify(ctx context.Context, client Client, nodes []paperast.Node) ([]Assignment, error) {
	return nil, fmt.Errorf("unbounded role classification is disabled; use ClassifyStructured")
}

// Freeze converts the final, sequence-arbitrated AST into the single semantic
// plan consumed by the formatter. Body text becomes actionable only after a
// section boundary has established its context.
func Freeze(nodes []paperast.Node) []Assignment {
	result := make([]Assignment, 0, len(nodes))
	abstractBodyRole := "abstract_body"
	for _, node := range nodes {
		if node.NodeType != "paragraph" || strings.TrimSpace(node.NodeID) == "" {
			continue
		}
		role := node.SemanticRole
		if role == "abstract_cn" {
			abstractBodyRole = "abstract_body"
		} else if role == "abstract_en" {
			abstractBodyRole = "abstract_en_body"
		}
		switch role {
		case "heading":
			if node.LogicalLevel < 1 || node.LogicalLevel > 4 {
				continue
			}
			role = fmt.Sprintf("heading_%d", node.LogicalLevel)
		case "body_paragraph":
			switch node.SectionID {
			case "body":
				role = "body"
			case "abstract":
				role = abstractBodyRole
			case "references":
				role = "references"
			case "acknowledgements":
				role = "body"
			default:
				continue
			}
		case "blank", "table", "unknown", "header", "footer":
			continue
		}
		evidence := append([]string(nil), node.Evidence...)
		evidence = append(evidence, "deterministic:frozen_ast")
		result = append(result, Assignment{
			NodeID: node.NodeID, Role: role, Confidence: node.Confidence,
			Evidence: evidence, Trusted: true, Index: node.Index,
		})
	}
	return result
}

// Merge keeps deterministic high-confidence roles and only accepts a model
// candidate when it is sufficiently confident. Unknown candidates preserve the
// original role and therefore never trigger a generic body-format fallback.
func Merge(nodes []paperast.Node, assignments []Assignment, threshold float64) []paperast.Node {
	if threshold <= 0 {
		threshold = 0.85
	}
	byID := make(map[string]Assignment, len(assignments))
	for _, assignment := range assignments {
		byID[assignment.NodeID] = assignment
	}
	original := append([]paperast.Node(nil), nodes...)
	result := append([]paperast.Node(nil), nodes...)
	for i := range result {
		candidate, ok := byID[result[i].NodeID]
		if !ok || candidate.Confidence < threshold || candidate.Role == "unknown" {
			continue
		}
		if result[i].Confidence >= 0.9 && result[i].SemanticRole != "body_paragraph" {
			continue
		}
		role, level := normalizeRole(candidate.Role)
		result[i].SemanticRole = role
		if level > 0 {
			result[i].LogicalLevel = level
		}
		result[i].Confidence = candidate.Confidence
		result[i].Evidence = append([]string{"deepseek:role"}, candidate.Evidence...)
	}
	// Lightweight sequence arbitration: an isolated model decision must not
	// create two adjacent chapter-level headings or reopen body sections after
	// references/acknowledgements have started.
	for i := range result {
		if result[i].SemanticRole != "heading" {
			continue
		}
		if i > 0 && result[i].LogicalLevel == 1 && result[i-1].SemanticRole == "heading" && result[i-1].LogicalLevel == 1 {
			result[i] = original[i]
		}
		if i > 0 && (result[i-1].SectionID == "references" || result[i-1].SectionID == "acknowledgements") {
			result[i] = original[i]
		}
	}
	return result
}

// ApplyFormatEvidence compares the student's effective OOXML style with the
// selected template. It may recover a short, weakly classified heading, but
// never overrides an explicit numbered/semantic role.
func ApplyFormatEvidence(nodes []paperast.Node, profile *templateprofile.Profile) []paperast.Node {
	if profile == nil || len(profile.Styles) == 0 {
		return nodes
	}
	result := append([]paperast.Node(nil), nodes...)
	for i := range result {
		node := &result[i]
		if node.NodeType != "paragraph" || node.EffectiveStyle == nil || strings.TrimSpace(node.Text) == "" {
			continue
		}
		roles := []string{"body", "heading_1", "heading_2", "heading_3", "heading_4"}
		bestRole, bestScore, secondScore := "", 0.0, 0.0
		for _, role := range roles {
			rule, ok := profile.Styles[role]
			if !ok {
				continue
			}
			score, compared := formatSimilarity(*node.EffectiveStyle, rule)
			if compared < 3 {
				continue
			}
			if score > bestScore {
				secondScore, bestScore, bestRole = bestScore, score, role
			} else if score > secondScore {
				secondScore = score
			}
		}
		if bestRole == "" {
			continue
		}
		node.Evidence = append(node.Evidence, fmt.Sprintf("format_similarity:%s:%.2f", bestRole, bestScore))
		if roleMatchesNode(bestRole, *node) && bestScore >= 0.75 {
			if bestScore > node.Confidence {
				node.Confidence = bestScore
			}
			continue
		}
		if node.SemanticRole != "body_paragraph" || !strings.HasPrefix(bestRole, "heading_") ||
			bestScore < 0.86 || bestScore-secondScore < 0.15 || !looksLikeHeading(node.Text) || hasStrongSemanticEvidence(node.Evidence) {
			continue
		}
		level, _ := strconv.Atoi(strings.TrimPrefix(bestRole, "heading_"))
		node.SemanticRole, node.LogicalLevel, node.Confidence = "heading", level, bestScore
		node.Evidence = append(node.Evidence, "format_similarity:role_recovered")
	}
	return result
}

// EnforceDocumentTree applies whole-document ordering and heading-parent
// constraints after all local classifiers have voted. Invalid nodes become
// unknown so the formatter preserves their original OOXML for review.
func EnforceDocumentTree(nodes []paperast.Node) []paperast.Node {
	result := append([]paperast.Node(nil), nodes...)
	section, sectionRank := "cover", 0
	headingSeen := [5]bool{}
	for i := range result {
		node := &result[i]
		if node.NodeType != "paragraph" {
			continue
		}
		nextSection, nextRank := sectionForRole(*node, section, sectionRank)
		if nextRank < sectionRank {
			invalidateTreeNode(node, "tree:section_regression")
			continue
		}
		section, sectionRank = nextSection, nextRank
		if node.SemanticRole == "heading" {
			level := node.LogicalLevel
			if level < 1 || level > 4 || level > 1 && !headingSeen[level-1] {
				invalidateTreeNode(node, "tree:missing_heading_parent")
				continue
			}
			headingSeen[level] = true
			for deeper := level + 1; deeper < len(headingSeen); deeper++ {
				headingSeen[deeper] = false
			}
		}
		if node.SemanticRole == "body_paragraph" || node.SemanticRole == "references" || node.SemanticRole == "acknowledgements" {
			node.SectionID = section
		}
	}
	return result
}

func sectionForRole(node paperast.Node, current string, rank int) (string, int) {
	switch node.SemanticRole {
	case "abstract_cn", "abstract_en", "abstract_title", "abstract_body":
		return "abstract", 1
	case "toc_title", "toc_entry":
		return "toc", 2
	case "heading":
		if node.LogicalLevel == 1 {
			return "body", 3
		}
	case "references_title", "references":
		return "references", 4
	case "acknowledgements_title", "acknowledgements":
		return "acknowledgements", 5
	case "appendix_title", "appendix":
		return "appendix", 6
	}
	return current, rank
}

func invalidateTreeNode(node *paperast.Node, evidence string) {
	node.SemanticRole = "unknown"
	node.LogicalLevel = 0
	if node.Confidence > 0.5 {
		node.Confidence = 0.5
	}
	node.Evidence = append(node.Evidence, evidence)
}

func formatSimilarity(actual, expected templateprofile.StyleRule) (float64, int) {
	total, matched, compared := 0.0, 0.0, 0
	compareText := func(a, b string, weight float64) {
		if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
			return
		}
		compared++
		total += weight
		if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) {
			matched += weight
		}
	}
	compareNumber := func(a, b string, tolerance int, weight float64) {
		left, leftErr := strconv.Atoi(strings.TrimSpace(a))
		right, rightErr := strconv.Atoi(strings.TrimSpace(b))
		if leftErr != nil || rightErr != nil {
			return
		}
		compared++
		total += weight
		delta := left - right
		if delta < 0 {
			delta = -delta
		}
		if delta <= tolerance {
			matched += weight
		}
	}
	compareText(actual.FontEastAsia, expected.FontEastAsia, 0.22)
	compareText(actual.FontASCII, expected.FontASCII, 0.08)
	compareNumber(actual.FontSizeHalfPt, expected.FontSizeHalfPt, 1, 0.22)
	compareText(actual.Alignment, expected.Alignment, 0.12)
	compareNumber(actual.Line, expected.Line, 20, 0.12)
	compareNumber(actual.FirstLineChars, expected.FirstLineChars, 10, 0.12)
	if actual.BoldSet && expected.BoldSet {
		compared++
		total += 0.12
		if actual.Bold == expected.Bold {
			matched += 0.12
		}
	}
	if total == 0 {
		return 0, compared
	}
	return matched / total, compared
}

func roleMatchesNode(role string, node paperast.Node) bool {
	if role == "body" {
		return node.SemanticRole == "body_paragraph"
	}
	if strings.HasPrefix(role, "heading_") && node.SemanticRole == "heading" {
		level, _ := strconv.Atoi(strings.TrimPrefix(role, "heading_"))
		return level == node.LogicalLevel
	}
	return false
}

func looksLikeHeading(text string) bool {
	trimmed := strings.TrimSpace(text)
	runes := []rune(trimmed)
	if len(runes) == 0 || len(runes) > 60 {
		return false
	}
	return !strings.ContainsAny(string(runes[len(runes)-1]), "。！？；，,.!?;")
}

func hasStrongSemanticEvidence(evidence []string) bool {
	for _, item := range evidence {
		if strings.HasPrefix(item, "regex:") || strings.HasPrefix(item, "keyword:") || strings.HasPrefix(item, "state:") || strings.HasPrefix(item, "ooxml:outline") {
			return true
		}
	}
	return false
}

// EffectiveAssignments reflects sequence arbitration, so the formatter never
// applies a candidate that the state constraints rejected.
func EffectiveAssignments(nodes []paperast.Node, candidates []Assignment) []Assignment {
	byID := make(map[string]Assignment, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.NodeID] = candidate
	}
	result := make([]Assignment, 0, len(candidates))
	for _, node := range nodes {
		candidate, ok := byID[node.NodeID]
		if !ok || node.Confidence != candidate.Confidence {
			continue
		}
		accepted := false
		for _, evidence := range node.Evidence {
			if evidence == "deepseek:role" {
				accepted = true
				break
			}
		}
		if !accepted {
			continue
		}
		role := node.SemanticRole
		if role == "heading" {
			role = fmt.Sprintf("heading_%d", node.LogicalLevel)
		}
		if role == "body_paragraph" {
			role = "body"
		}
		candidate.Role = role
		result = append(result, candidate)
	}
	return result
}

func Validate(nodes []paperast.Node, assignments []Assignment) []ValidationIssue {
	byID := make(map[string]paperast.Node, len(nodes))
	for _, node := range nodes {
		byID[node.NodeID] = node
	}
	issues := []ValidationIssue{}
	for _, assignment := range assignments {
		node, ok := byID[assignment.NodeID]
		if !ok {
			issues = append(issues, ValidationIssue{assignment.NodeID, "stable node missing after formatting"})
			continue
		}
		want, level := normalizeRole(assignment.Role)
		mismatch := false
		if want == "heading" {
			mismatch = node.SemanticRole != "heading" || (level > 0 && node.LogicalLevel != level)
		} else if want == "body_paragraph" {
			mismatch = node.SemanticRole == "heading"
		}
		if mismatch {
			issues = append(issues, ValidationIssue{assignment.NodeID, fmt.Sprintf("role changed after formatting: want %s/%d got %s/%d", want, level, node.SemanticRole, node.LogicalLevel)})
		}
	}
	return issues
}

func normalizeRole(role string) (string, int) {
	switch role {
	case "heading_1":
		return "heading", 1
	case "heading_2":
		return "heading", 2
	case "heading_3":
		return "heading", 3
	case "heading_4":
		return "heading", 4
	case "body":
		return "body_paragraph", 0
	case "abstract_body":
		return "abstract_cn", 0
	case "abstract_en_body":
		return "abstract_en", 0
	case "references":
		return "references", 0
	default:
		return role, 0
	}
}
