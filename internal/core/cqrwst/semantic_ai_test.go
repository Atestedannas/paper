package cqrwst

import (
	"strings"
	"testing"
)

func TestApplySemanticAIDecisionsAddsDataTableCaptionName(t *testing.T) {
	documentXML := `<w:document xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
		`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>` +
		`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>年龄</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
		`</w:body></w:document>`

	updated, count := applySemanticAIDecisionsToDocumentXML(documentXML, []semanticAIDecision{{
		Index:       1,
		Kind:        "data_table",
		CaptionName: "\u6837\u672c\u57fa\u672c\u60c5\u51b5\u8868",
		Confidence:  0.92,
	}})
	if count == 0 {
		t.Fatal("expected semantic AI repair count")
	}
	if !strings.Contains(updated, "\u88681.1 \u6837\u672c\u57fa\u672c\u60c5\u51b5\u8868") {
		t.Fatalf("missing AI table caption: %s", updated)
	}
}

func TestApplySemanticAIDecisionsRemovesLayoutTableGenericCaption(t *testing.T) {
	documentXML := `<w:document xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
		`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>表1.1 表格</w:t></w:r></w:p>` +
		`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>专业</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>护理学</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
		`</w:body></w:document>`

	updated, count := applySemanticAIDecisionsToDocumentXML(documentXML, []semanticAIDecision{{
		Index:      2,
		Kind:       "layout_table",
		Confidence: 0.95,
	}})
	if count == 0 {
		t.Fatal("expected semantic AI repair count")
	}
	if strings.Contains(updated, "\u88681.1 \u8868\u683c") {
		t.Fatalf("generic table caption should be removed: %s", updated)
	}
}

func TestSemanticAIPromptConstrainsDeepSeekToSafeStructuralDecisions(t *testing.T) {
	prompt := semanticAIPrompt(`{"blocks":[]}`)
	required := []string{
		"语义安全分类器",
		"不能改论文正文内容",
		"不能决定分页",
		"只输出需要程序采取动作的 block",
		"confidence 必须 >= 0.85",
		"封面/基本信息表",
		"返回 {\"decisions\":[]}",
	}
	for _, text := range required {
		if !strings.Contains(prompt, text) {
			t.Fatalf("semanticAIPrompt() missing %q:\n%s", text, prompt)
		}
	}
}

func TestNormalizeSemanticAIDecisionsRequiresHighConfidence(t *testing.T) {
	decisions := normalizeSemanticAIDecisions([]semanticAIDecision{
		{Index: 1, Kind: "data_table", CaptionName: "低置信表", Confidence: 0.84},
		{Index: 2, Kind: "figure", CaptionName: "技术路线图", Confidence: 0.85},
	})
	if len(decisions) != 1 {
		t.Fatalf("decisions len = %d, want 1", len(decisions))
	}
	if decisions[0].Index != 2 || decisions[0].CaptionName != "技术路线图" {
		t.Fatalf("unexpected decisions: %#v", decisions)
	}
}
