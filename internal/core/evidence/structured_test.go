package evidence

import (
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/paperast"
)

func TestCompileRuleRequestBoundsEvidencePacket(t *testing.T) {
	items := make([]Item, 0, 20)
	for i := 0; i < 20; i++ {
		items = append(items, Item{EvidenceID: "ev:" + string(rune('a'+i)), SourceType: "pdf_observation", Text: strings.Repeat("x", 500), BBox: []float64{1, 2, 3, 4, 5}})
	}
	request := CompileRuleRequest("template", "heading_1", "", "p:1", strings.Repeat("y", 500), items)
	if len(request.Evidence) != maxEvidenceItems || len([]rune(request.Subject.Text)) != maxEvidenceText {
		t.Fatalf("unbounded request: evidence=%d subject=%d", len(request.Evidence), len([]rune(request.Subject.Text)))
	}
	for _, item := range request.Evidence {
		if len([]rune(item.Text)) != maxEvidenceText || item.BBox != nil {
			t.Fatalf("unsafe evidence item: %#v", item)
		}
	}
}

func TestRoleRequestIsBoundedAndCitable(t *testing.T) {
	nodes := []paperast.Node{
		{NodeID: "body:p:1", NodeType: "paragraph", Text: "上一段正文"},
		{NodeID: "body:p:2", NodeType: "paragraph", Text: "一 员工绩效考核基本理论综述", SemanticRole: "body_paragraph", SectionID: "body", Evidence: []string{"regex:chinese_list_heading"}},
		{NodeID: "body:p:3", NodeType: "paragraph", Text: "下一段正文"},
	}
	request, ok := RoleRequest(nodes, 1, "cqie-requirement-2022")
	if !ok || request.Subject.NodeID != "body:p:2" || len(request.Evidence) < 6 {
		t.Fatalf("unexpected request: %#v", request)
	}
	for _, item := range request.Evidence {
		if item.Property == "font.east_asia" || item.Property == "font_size" || item.Property == "paragraph_style" {
			t.Fatalf("role request leaked formatting observation: %#v", item)
		}
	}
	if err := ValidateRoleCandidate(request, CandidateRole{NodeID: "body:p:2", Role: "heading_1", Confidence: .97, SourceEvidenceIDs: []string{"ev:subject", "ev:structure:1"}}); err != nil {
		t.Fatalf("valid candidate rejected: %v", err)
	}
	if err := ValidateRoleCandidate(request, CandidateRole{NodeID: "body:p:2", Role: "heading_1", Confidence: .97, SourceEvidenceIDs: []string{"invented"}}); err == nil {
		t.Fatal("unknown evidence id was accepted")
	}
}

func TestRoleRequestAddsOnlyPDFPositionEvidence(t *testing.T) {
	nodes := []paperast.Node{{NodeID: "body:p:2", NodeType: "paragraph", Text: "1 Introduction", SemanticRole: "heading", LogicalLevel: 1}}
	request, ok := RoleRequestWithEvidence(nodes, 0, "student", []Item{
		{EvidenceID: "pdf:bbox", SourceType: "pdf_observation", SourceNodeID: "body:p:2", Property: "bbox", Page: 8, BBox: []float64{100, 80, 300, 100}, Confidence: .99},
		{EvidenceID: "pdf:normalized-bbox", SourceType: "pdf_observation", SourceNodeID: "body:p:2", Property: "normalized_bbox", RawValue: []float64{.16, .09, .50, .12}, Unit: "page_fraction", Page: 8, BBox: []float64{100, 80, 300, 100}, Confidence: .99},
		{EvidenceID: "pdf:region", SourceType: "pdf_observation", SourceNodeID: "body:p:2", Property: "page_region", RawValue: "top", Page: 8, BBox: []float64{100, 80, 300, 100}, Confidence: .99},
		{EvidenceID: "pdf:font", SourceType: "pdf_observation", SourceNodeID: "body:p:2", Property: "font.rendered", RawValue: "NotoSans-Regular"},
		{EvidenceID: "pdf:size", SourceType: "pdf_observation", SourceNodeID: "body:p:2", Property: "size_pt", RawValue: 16.0},
	})
	if !ok {
		t.Fatal("RoleRequestWithEvidence() returned false")
	}
	positionCount := 0
	for _, item := range request.Evidence {
		if item.SourceType != "pdf_observation" {
			continue
		}
		if (item.Property != "bbox" && item.Property != "normalized_bbox" && item.Property != "page_region") || item.Page != 8 || len(item.BBox) != 4 {
			t.Fatalf("role request leaked a non-positional PDF fact: %#v", item)
		}
		positionCount++
	}
	if positionCount != 3 {
		t.Fatalf("PDF position evidence count = %d, want 3: %#v", positionCount, request.Evidence)
	}
}

func TestRoleRequestPrefersNodeLevelPDFGeometry(t *testing.T) {
	nodes := []paperast.Node{{NodeID: "body:p:2", NodeType: "paragraph", Text: "1 Introduction", SemanticRole: "heading", LogicalLevel: 1}}
	request, ok := RoleRequestWithEvidence(nodes, 0, "student", []Item{
		{EvidenceID: "pdf:line", SourceType: "pdf_observation", SourceNodeID: "body:p:2", Property: "bbox", Page: 8, BBox: []float64{100, 80, 300, 100}},
		{EvidenceID: "pdf:union", SourceType: "pdf_observation", SourceNodeID: "body:p:2", Property: "bbox_union", Page: 8, BBox: []float64{100, 80, 300, 140}},
		{EvidenceID: "pdf:union-normalized", SourceType: "pdf_observation", SourceNodeID: "body:p:2", Property: "normalized_bbox_union", Page: 8, BBox: []float64{100, 80, 300, 140}},
		{EvidenceID: "pdf:region", SourceType: "pdf_observation", SourceNodeID: "body:p:2", Property: "page_region", RawValue: "top", Page: 8, BBox: []float64{100, 80, 300, 140}},
	})
	if !ok {
		t.Fatal("role request was not created")
	}
	got := map[string]bool{}
	for _, item := range request.Evidence {
		got[item.EvidenceID] = true
	}
	if !got["pdf:union"] || !got["pdf:union-normalized"] || !got["pdf:region"] || got["pdf:line"] {
		t.Fatalf("role request did not prefer node-level geometry: %#v", request.Evidence)
	}
}

func TestRuleCandidateRejectsUnsupportedInference(t *testing.T) {
	request := Request{Version: Version, Task: "compile_format_rule", Subject: Subject{CandidateRole: "heading_1"}, Evidence: []Item{{EvidenceID: "ev:001", SourceType: "requirement_text"}}}
	valid := CandidateRule{RuleID: "CQIE.H1", Role: "heading_1", Confidence: .98, SourceEvidenceIDs: []string{"ev:001"}, Constraints: map[string]any{"font.east_asia": "黑体", "size_pt": 16}}
	if err := ValidateRuleCandidate(request, valid); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
	valid.Constraints["invented_property"] = true
	if err := ValidateRuleCandidate(request, valid); err == nil {
		t.Fatal("unsupported rule constraint accepted")
	}
}

func TestRuleCandidateRejectsInvalidConstraintValues(t *testing.T) {
	request := Request{Task: "compile_format_rule", Subject: Subject{CandidateRole: "heading_1"}, Evidence: []Item{{EvidenceID: "ev:001"}}}
	base := CandidateRule{RuleID: "CQIE.H1", Role: "heading_1", Confidence: .98, SourceEvidenceIDs: []string{"ev:001"}}
	for name, constraints := range map[string]map[string]any{
		"zero size":         {"size_pt": "0"},
		"invalid alignment": {"alignment": "middle"},
		"string page break": {"page_break_before": "true"},
		"empty font":        {"font": map[string]any{"east_asia": ""}},
	} {
		t.Run(name, func(t *testing.T) {
			base.Constraints = constraints
			if err := ValidateRuleCandidate(request, base); err == nil {
				t.Fatal("invalid constraint was accepted")
			}
		})
	}
}

func TestCompileRuleRequestKeepsHeterogeneousEvidence(t *testing.T) {
	request := CompileRuleRequest("cqie-2022", "heading_1", "humanities", "body:p:214", "一 员工绩效考核基本理论综述", []Item{{EvidenceID: "ev:001", SourceType: "requirement_text", SourceNodeID: "body:table:2:row:3:cell:1", Text: "一××××（三号黑体，居中）"}, {EvidenceID: "ev:005", SourceType: "pdf_observation", Page: 23, BBox: []float64{235.2, 89.8, 368, 111.59}, SizePt: 16}})
	if request.Task != "compile_format_rule" || request.Subject.CandidateRole != "heading_1" || request.Subject.SampleNodeID != "body:p:214" || request.Evidence[1].Page != 23 || !request.RequiredOutput.MustIncludeSourceIDs {
		t.Fatalf("unexpected structured request: %#v", request)
	}
}

func TestValidateRuleCandidateRequiresMatchingTypedEvidence(t *testing.T) {
	request := CompileRuleRequest("template", "heading_1", "", "p:1", "Heading", []Item{
		{EvidenceID: "ev:size", SourceType: "ooxml_observation", Property: "size_pt", RawValue: 32, RawUnit: "half_point", Normalized: 16.0, Unit: "pt"},
		{EvidenceID: "ev:alignment", SourceType: "ooxml_observation", Property: "alignment", RawValue: "center"},
	})
	candidate := CandidateRule{RuleID: "H1", Role: "heading_1", Constraints: map[string]any{"alignment": "center", "size_pt": 16.0}, SourceEvidenceIDs: []string{"ev:size"}, Confidence: .98}
	if err := ValidateRuleCandidate(request, candidate); err == nil {
		t.Fatal("candidate that cites size evidence for alignment was accepted")
	}
	candidate.SourceEvidenceIDs = []string{"ev:size", "ev:alignment"}
	if err := ValidateRuleCandidate(request, candidate); err != nil {
		t.Fatalf("candidate with matching typed evidence rejected: %v", err)
	}
}

func TestRuleCandidateRejectsPDFOnlyStyleInference(t *testing.T) {
	request := CompileRuleRequest("template", "body", "", "p:1", "Body", []Item{
		{EvidenceID: "pdf:alignment", SourceType: "pdf_observation", Property: "alignment", RawValue: "center", Page: 1, BBox: []float64{100, 80, 300, 100}},
	})
	candidate := CandidateRule{RuleID: "BODY.ALIGN", Role: "body", Constraints: map[string]any{"alignment": "center"}, SourceEvidenceIDs: []string{"pdf:alignment"}, Confidence: .99}
	if err := ValidateRuleCandidate(request, candidate); err == nil {
		t.Fatal("candidate inferred reusable style from PDF-only alignment")
	}
}
