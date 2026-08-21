package roleclassify

import (
	"context"
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

type fakeRoleClient struct{ response string }

func (f fakeRoleClient) ChatCompletion(string) (string, error) { return f.response, nil }

type recordingRoleClient struct {
	responses []string
	prompts   []string
}

func (f *recordingRoleClient) ChatCompletion(prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	return f.responses[len(f.prompts)-1], nil
}

func TestClassifyRejectsUnboundedDocumentPrompt(t *testing.T) {
	if _, err := Classify(context.Background(), fakeRoleClient{}, []paperast.Node{{NodeID: "p:AB12", NodeType: "paragraph", Text: "1 绪论"}}); err == nil {
		t.Fatal("unbounded compatibility classifier was not disabled")
	}
}

func TestHeaderFooterPartsNeverEnterRoleModelOrFormatPlan(t *testing.T) {
	for _, node := range []paperast.Node{
		{NodeID: "header:p:1", NodeType: "paragraph", SourcePart: "word/header1.xml", SemanticRole: "header", Text: "论文页眉", Confidence: 0.1},
		{NodeID: "footer:p:1", NodeType: "paragraph", SourcePart: "word/footer1.xml", SemanticRole: "footer", Text: "1", Confidence: 0.1},
		{NodeID: "footnote:p:1", NodeType: "paragraph", SourcePart: "word/footnotes.xml", SemanticRole: "body_paragraph", Text: "注释", Confidence: 0.1},
	} {
		if needsSemanticArbitration(node) {
			t.Fatalf("non-body OOXML part became a model subject: %#v", node)
		}
	}
	assignments := Freeze([]paperast.Node{
		{NodeID: "header:p:1", NodeType: "paragraph", SourcePart: "word/header1.xml", SemanticRole: "header", Text: "论文页眉"},
		{NodeID: "footer:p:1", NodeType: "paragraph", SourcePart: "word/footer1.xml", SemanticRole: "footer", Text: "1"},
	})
	if len(assignments) != 0 {
		t.Fatalf("header/footer entered body format plan: %#v", assignments)
	}
}

func TestClassifyStructuredRejectsUncitedModelResult(t *testing.T) {
	_, err := ClassifyStructured(context.Background(), fakeRoleClient{`{"node_id":"p:1","role":"heading_1","confidence":0.95,"source_evidence_ids":["not-provided"]}`}, []paperast.Node{{NodeID: "p:1", NodeType: "paragraph", Text: "Potential heading", SemanticRole: "body_paragraph", Confidence: 0.5}})
	if err == nil {
		t.Fatal("uncited structured model result was accepted")
	}
}

func TestClassifyStructuredProducesCitableAssignment(t *testing.T) {
	got, err := ClassifyStructured(context.Background(), fakeRoleClient{`{"node_id":"p:1","role":"heading_1","confidence":0.95,"source_evidence_ids":["ev:subject"]}`}, []paperast.Node{{NodeID: "p:1", NodeType: "paragraph", Text: "Potential heading", SemanticRole: "body_paragraph", Confidence: 0.5}})
	if err != nil || len(got) != 1 || got[0].Evidence[0] != "deepseek_evidence:ev:subject" {
		t.Fatalf("ClassifyStructured() = %#v, %v", got, err)
	}
}

func TestClassifyStructuredAcceptsCaptionRole(t *testing.T) {
	got, err := ClassifyStructured(context.Background(), fakeRoleClient{`{"node_id":"p:1","role":"figure_caption","confidence":0.95,"source_evidence_ids":["ev:subject"]}`}, []paperast.Node{{NodeID: "p:1", NodeType: "paragraph", Text: "图1-1 系统架构", SemanticRole: "body_paragraph", Confidence: 0.5}})
	if err != nil || len(got) != 1 || got[0].Role != "figure_caption" {
		t.Fatalf("ClassifyStructured() = %#v, %v", got, err)
	}
}

func TestClassifyStructuredAcceptsOriginalityDeclaration(t *testing.T) {
	got, err := ClassifyStructured(context.Background(), fakeRoleClient{`{"node_id":"p:1","role":"originality_declaration","confidence":0.95,"source_evidence_ids":["ev:subject"]}`}, []paperast.Node{{NodeID: "p:1", NodeType: "paragraph", Text: "原创性声明", SemanticRole: "body_paragraph", Confidence: 0.5}})
	if err != nil || len(got) != 1 || got[0].Role != "originality_declaration" {
		t.Fatalf("ClassifyStructured() = %#v, %v", got, err)
	}
}

func TestClassifyStructuredSendsOneSubjectPerCall(t *testing.T) {
	client := &recordingRoleClient{responses: []string{
		`{"node_id":"p:1","role":"heading_1","confidence":0.95,"source_evidence_ids":["ev:subject"]}`,
		`{"node_id":"p:2","role":"body","confidence":0.95,"source_evidence_ids":["ev:subject"]}`,
	}}
	got, err := ClassifyStructured(context.Background(), client, []paperast.Node{
		{NodeID: "p:1", NodeType: "paragraph", Text: "First ambiguous subject", SemanticRole: "body_paragraph", Confidence: 0.5},
		{NodeID: "p:2", NodeType: "paragraph", Text: "Second ambiguous subject", SemanticRole: "body_paragraph", Confidence: 0.5},
	})
	if err != nil || len(got) != 2 || len(client.prompts) != 2 {
		t.Fatalf("ClassifyStructured() assignments=%#v prompts=%d err=%v", got, len(client.prompts), err)
	}
	if strings.Count(client.prompts[0], `"subject":{"node_id":"p:1"`) != 1 || strings.Count(client.prompts[1], `"subject":{"node_id":"p:2"`) != 1 {
		t.Fatalf("role calls did not contain exactly one requested subject: %#v", client.prompts)
	}
}

func TestMergePreservesUnknownAndLowConfidenceOriginalRole(t *testing.T) {
	nodes := []paperast.Node{{NodeID: "p:1", NodeType: "paragraph", SemanticRole: "body_paragraph"}, {NodeID: "p:2", NodeType: "paragraph", SemanticRole: "body_paragraph"}}
	merged := Merge(nodes, []Assignment{{NodeID: "p:1", Role: "unknown", Confidence: 0.99}, {NodeID: "p:2", Role: "heading_1", Confidence: 0.6}}, 0.85)
	for _, node := range merged {
		if node.SemanticRole != "body_paragraph" {
			t.Fatalf("node %s changed unexpectedly: %#v", node.NodeID, node)
		}
	}
}

func TestFreezeUsesSectionContextAndKeepsCoverUnknown(t *testing.T) {
	nodes := []paperast.Node{
		{NodeID: "p:COVER", NodeType: "paragraph", SemanticRole: "body_paragraph", SectionID: "cover", Confidence: 0.55},
		{NodeID: "p:CN", NodeType: "paragraph", SemanticRole: "abstract_cn", SectionID: "abstract", Confidence: 0.95},
		{NodeID: "p:CNBODY", NodeType: "paragraph", SemanticRole: "body_paragraph", SectionID: "abstract", Confidence: 0.55},
		{NodeID: "p:EN", NodeType: "paragraph", SemanticRole: "abstract_en", SectionID: "abstract", Confidence: 0.95},
		{NodeID: "p:ENBODY", NodeType: "paragraph", SemanticRole: "body_paragraph", SectionID: "abstract", Confidence: 0.55},
		{NodeID: "p:H1", NodeType: "paragraph", SemanticRole: "heading", LogicalLevel: 1, SectionID: "body", Confidence: 0.82},
		{NodeID: "p:BODY", NodeType: "paragraph", SemanticRole: "body_paragraph", SectionID: "body", Confidence: 0.55},
	}
	got := Freeze(nodes)
	want := map[string]string{
		"p:CN": "abstract_cn", "p:CNBODY": "abstract_body",
		"p:EN": "abstract_en", "p:ENBODY": "abstract_en_body",
		"p:H1": "heading_1", "p:BODY": "body",
	}
	if len(got) != len(want) {
		t.Fatalf("Freeze() returned %d assignments, want %d: %#v", len(got), len(want), got)
	}
	for _, assignment := range got {
		if want[assignment.NodeID] != assignment.Role || !assignment.Trusted {
			t.Fatalf("unexpected frozen assignment: %#v", assignment)
		}
	}
}

func TestApplyFormatEvidenceRecoversOnlyWeakHeading(t *testing.T) {
	heading := templateprofile.StyleRule{FontEastAsia: "Hei", FontASCII: "Times", FontSizeHalfPt: "32", Alignment: "center", Line: "400", Bold: true, BoldSet: true}
	body := templateprofile.StyleRule{FontEastAsia: "Song", FontASCII: "Times", FontSizeHalfPt: "24", Alignment: "both", Line: "360", FirstLineChars: "200", Bold: false, BoldSet: true}
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{"heading_1": heading, "body": body}}
	nodes := []paperast.Node{
		{NodeID: "p:1", NodeType: "paragraph", Text: "Research Method", SemanticRole: "body_paragraph", Confidence: 0.55, EffectiveStyle: &heading},
		{NodeID: "p:2", NodeType: "paragraph", Text: "1 Explicit Heading", SemanticRole: "body_paragraph", Confidence: 0.95, Evidence: []string{"regex:numbered_heading"}, EffectiveStyle: &heading},
	}
	got := ApplyFormatEvidence(nodes, profile)
	if got[0].SemanticRole != "heading" || got[0].LogicalLevel != 1 {
		t.Fatalf("weak heading not recovered: %#v", got[0])
	}
	if got[1].SemanticRole != "body_paragraph" {
		t.Fatalf("strong semantic result was overwritten: %#v", got[1])
	}
}

func TestMergePreservesHighConfidenceDeterministicRole(t *testing.T) {
	nodes := []paperast.Node{{NodeID: "p:1", NodeType: "paragraph", SemanticRole: "references_title", Confidence: 0.95}}
	got := Merge(nodes, []Assignment{{NodeID: "p:1", Role: "body", Confidence: 0.99}}, 0.85)
	if got[0].SemanticRole != "references_title" {
		t.Fatalf("high-confidence deterministic role overwritten: %#v", got[0])
	}
}

func TestEnforceDocumentTreeRejectsOrphanAndPostReferenceHeadings(t *testing.T) {
	nodes := []paperast.Node{
		{NodeID: "p:1", NodeType: "paragraph", SemanticRole: "heading", LogicalLevel: 2, Confidence: 0.9},
		{NodeID: "p:2", NodeType: "paragraph", SemanticRole: "heading", LogicalLevel: 1, Confidence: 0.9},
		{NodeID: "p:3", NodeType: "paragraph", SemanticRole: "body_paragraph", Confidence: 0.7},
		{NodeID: "p:4", NodeType: "paragraph", SemanticRole: "references_title", Confidence: 0.95},
		{NodeID: "p:5", NodeType: "paragraph", SemanticRole: "body_paragraph", Confidence: 0.7},
		{NodeID: "p:6", NodeType: "paragraph", SemanticRole: "heading", LogicalLevel: 1, Confidence: 0.95},
	}
	got := EnforceDocumentTree(nodes)
	if got[0].SemanticRole != "unknown" || got[0].Evidence[len(got[0].Evidence)-1] != "tree:missing_heading_parent" {
		t.Fatalf("orphan heading was accepted: %#v", got[0])
	}
	if got[2].SectionID != "body" || got[4].SectionID != "references" {
		t.Fatalf("body sections not rebuilt: body=%#v references=%#v", got[2], got[4])
	}
	if got[5].SemanticRole != "unknown" || got[5].Evidence[len(got[5].Evidence)-1] != "tree:section_regression" {
		t.Fatalf("post-reference heading was accepted: %#v", got[5])
	}
}
