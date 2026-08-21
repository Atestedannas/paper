package evidence

import (
	"context"
	"testing"
)

type fakeClient string

func (f fakeClient) ChatCompletion(string) (string, error) { return string(f), nil }

func TestCompileRuleRequiresCitedTypedCandidate(t *testing.T) {
	request := CompileRuleRequest("template", "heading_1", "humanities", "p:1", "一 标题", []Item{{EvidenceID: "ev:1", SourceType: "requirement_text", Text: "三号黑体，居中"}})
	candidate, err := CompileRule(context.Background(), fakeClient(`{"rule_id":"CQIE.H1","role":"heading_1","variant":"humanities","constraints":{"font":{"east_asia":"黑体"},"size_pt":16,"alignment":"center"},"source_evidence_ids":["ev:1"],"confidence":0.98}`), request)
	if err != nil || candidate.RuleID != "CQIE.H1" {
		t.Fatalf("CompileRule() = %#v, %v", candidate, err)
	}
}

func TestCompileRuleAcceptsSelectorContract(t *testing.T) {
	request := CompileRuleRequest("template", "heading_1", "humanities", "body:p:214", "一 标题", []Item{{EvidenceID: "ev:1", SourceType: "requirement_text", Text: "三号黑体，居中"}})
	candidate, err := CompileRule(context.Background(), fakeClient(`{"rule_id":"CQIE.HUMANITIES.H1","selector":{"role":"heading_1","variant":"humanities"},"constraints":{"size_pt":16},"source_evidence_ids":["ev:1"],"confidence":0.98}`), request)
	if err != nil || candidate.Role != "heading_1" || candidate.Variant != "humanities" || candidate.Selector.Role != "heading_1" {
		t.Fatalf("CompileRule() = %#v, %v", candidate, err)
	}
}
