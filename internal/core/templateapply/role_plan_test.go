package templateapply

import (
	"context"
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/roleclassify"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

func TestFrozenRolePlanIsIdempotentAndUsesBodyRule(t *testing.T) {
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"body": {FontEastAsia: "宋体", FontASCII: "Times New Roman", FontSizeHalfPt: "24", FirstLineChars: "200"},
	}}
	assignments := []roleclassify.Assignment{{
		NodeID: "p:ABC123", Role: "body", Confidence: 0.55, Trusted: true,
	}}
	documentXML := `<w:document xmlns:w="w" xmlns:w14="w14"><w:body><w:p w14:paraId="ABC123"><w:r><w:t>正文</w:t></w:r></w:p></w:body></w:document>`
	plan := BuildRoleFormatPlan(profile, assignments)
	if len(plan) != 1 || !plan[0].Trusted || !plan[0].Apply {
		t.Fatalf("trusted deterministic assignment must remain applicable: %#v", plan)
	}
	updated, changed := applyRoleFormatPlanToDocumentXML(documentXML, profile, assignments)
	if changed != 1 {
		t.Fatalf("first pass changed=%d, want 1", changed)
	}
	for _, want := range []string{`w:eastAsia="宋体"`, `w:ascii="Times New Roman"`, `w:sz w:val="24"`, `w:firstLineChars="200"`} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated paragraph missing %s: %s", want, updated)
		}
	}
	_, changed = applyRoleFormatPlanToDocumentXML(updated, profile, assignments)
	if changed != 0 {
		t.Fatalf("second pass changed=%d, want idempotent 0", changed)
	}
}

func TestRolePlanMapsAbstractBodiesToBodyRules(t *testing.T) {
	if got := roleToProfileKey("abstract_body"); got != "abstract_body" {
		t.Fatalf("abstract body mapped to %q", got)
	}
	if got := roleToProfileKey("abstract_en_body"); got != "abstract_en_body" {
		t.Fatalf("English abstract body mapped to %q", got)
	}
	if got := roleToProfileKey("table_caption"); got != "table_caption" {
		t.Fatalf("table caption mapped to %q", got)
	}
	if got := roleToProfileKey("cover_date"); got != "cover_date" {
		t.Fatalf("cover date mapped to %q", got)
	}
	if got := roleToProfileKey("appendix_title"); got != preferStyleKey("appendix_title", "heading_1") {
		t.Fatalf("appendix title mapped to %q", got)
	}
	if got := roleToProfileKey("appendix"); got != preferStyleKey("appendix", "body") {
		t.Fatalf("appendix body mapped to %q", got)
	}
}

func TestFrozenRolePlanAppliesTemplateFlowOnceAndIsIdempotent(t *testing.T) {
	profile := &templateprofile.Profile{
		Styles: map[string]templateprofile.StyleRule{
			"heading_1":        {FontSizeHalfPt: "32", KeepNext: true, KeepNextSet: true},
			"references_title": {FontSizeHalfPt: "28"},
		},
		Sections: map[string]templateprofile.SectionRule{
			"body_start":       {PageBreakBefore: true, BlankParagraphsBefore: 1, DetectedFrom: "template:body_start"},
			"references_title": {PageBreakBefore: true, DetectedFrom: "template:references_title"},
		},
	}
	assignments := []roleclassify.Assignment{
		{NodeID: "p:AAA111", Role: "heading_1", Confidence: 1, Evidence: []string{"format_similarity:heading_1:0.95"}, Trusted: true, Index: 0},
		{NodeID: "p:BBB222", Role: "heading_1", Confidence: 1, Trusted: true, Index: 1},
		{NodeID: "p:CCC333", Role: "references_title", Confidence: 1, Trusted: true, Index: 2},
	}
	plan := BuildRoleFormatPlan(profile, assignments)
	if plan[0].Flow == nil || !plan[0].Flow.PageBreakBefore || plan[0].Flow.BlankParagraphsBefore != 1 {
		t.Fatalf("first body heading flow missing: %#v", plan[0].Flow)
	}
	if len(plan[0].Evidence) != 1 || plan[0].Evidence[0] != "format_similarity:heading_1:0.95" {
		t.Fatalf("classification evidence missing from plan: %#v", plan[0])
	}
	if plan[1].Flow != nil {
		t.Fatalf("later chapter must not reuse body_start flow: %#v", plan[1].Flow)
	}
	if plan[2].Flow == nil || !plan[2].Flow.PageBreakBefore {
		t.Fatalf("references flow missing: %#v", plan[2].Flow)
	}
	documentXML := `<w:document xmlns:w="w" xmlns:w14="w14"><w:body>` +
		`<w:p w14:paraId="AAA111"><w:r><w:t>1 Intro</w:t></w:r></w:p>` +
		`<w:p w14:paraId="BBB222"><w:r><w:t>2 Method</w:t></w:r></w:p>` +
		`<w:p w14:paraId="CCC333"><w:r><w:t>References</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	updated, changed := applyRoleFormatPlanToDocumentXML(documentXML, profile, assignments)
	if changed != 3 {
		t.Fatalf("first pass changed=%d, want 3", changed)
	}
	if got := strings.Count(updated, `<w:pageBreakBefore/>`); got != 2 {
		t.Fatalf("pageBreakBefore count=%d, want 2: %s", got, updated)
	}
	if got := strings.Count(updated, `<w:keepNext/>`); got != 2 {
		t.Fatalf("keepNext count=%d, want 2: %s", got, updated)
	}
	_, changed = applyRoleFormatPlanToDocumentXML(updated, profile, assignments)
	if changed != 0 {
		t.Fatalf("second pass changed=%d, want 0", changed)
	}
}

func TestRolePlanDoesNotApplyUnsafeSectionFlow(t *testing.T) {
	profile := &templateprofile.Profile{
		Styles: map[string]templateprofile.StyleRule{"references_title": {FontSizeHalfPt: "28"}},
		Sections: map[string]templateprofile.SectionRule{
			"references_title": {PageBreakBefore: true, SectionBreak: true, SectionBreakType: "nextPage"},
		},
	}
	assignments := []roleclassify.Assignment{{NodeID: "p:AAA111", Role: "references_title", Confidence: 1, Trusted: true}}
	plan := BuildRoleFormatPlan(profile, assignments)
	if plan[0].Flow == nil || !plan[0].Flow.ReviewRequired {
		t.Fatalf("unsafe section flow must remain review-only: %#v", plan)
	}
	documentXML := `<w:document xmlns:w="w" xmlns:w14="w14"><w:body><w:p w14:paraId="AAA111"><w:r><w:t>References</w:t></w:r></w:p></w:body></w:document>`
	updated, _ := applyRoleFormatPlanToDocumentXML(documentXML, profile, assignments)
	if strings.Contains(updated, `<w:pageBreakBefore/>`) {
		t.Fatalf("unsafe section flow inserted a page break: %s", updated)
	}
}

func TestValidateRoleFormatPlanReportsAndClearsNodeProperties(t *testing.T) {
	path := writeCQRWSTDocx(t, `<w:p w14:paraId="ABC123"><w:r><w:t>Body</w:t></w:r></w:p>`)
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"body": {FontEastAsia: "Song", FontASCII: "Times", FontSizeHalfPt: "24", FirstLineChars: "200"},
	}}
	assignments := []roleclassify.Assignment{{NodeID: "p:ABC123", Role: "body", Confidence: 1, Trusted: true, Index: 0}}
	issues, err := ValidateRoleFormatPlan(context.Background(), path, profile, assignments)
	if err != nil || len(issues) == 0 {
		t.Fatalf("unformatted document issues=%#v err=%v", issues, err)
	}
	if _, err := ApplyRoleFormatPlan(context.Background(), path, profile, assignments); err != nil {
		t.Fatal(err)
	}
	issues, err = ValidateRoleFormatPlan(context.Background(), path, profile, assignments)
	if err != nil || len(issues) != 0 {
		t.Fatalf("formatted document issues=%#v err=%v", issues, err)
	}
}

func TestRolePlanAddsHeading1StyleOnlyForTrustedHeading(t *testing.T) {
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"heading_1": {
			Alignment: "center", Bold: true, BoldSet: true, FontASCII: "黑体", FontCS: "Times New Roman",
			FontEastAsia: "黑体", FontHAnsi: "黑体", FontSizeHalfPt: "32", ComplexSizeHalfPt: "32", Line: "400", LineRule: "exact",
		},
	}}
	assignments := []roleclassify.Assignment{{NodeID: "p:AAA111", Role: "heading_1", Confidence: 1, Trusted: true, Index: 0}}
	input := `<w:document xmlns:w="w" xmlns:w14="w14"><w:body><w:p w14:paraId="AAA111"><w:r><w:t>1 绪论</w:t></w:r></w:p></w:body></w:document>`
	updated, changed := applyRoleFormatPlanToDocumentXML(input, profile, assignments)
	if changed != 1 || !strings.Contains(updated, `<w:pStyle w:val="Heading1"/>`) || !strings.Contains(updated, `w:before="400"`) || !strings.Contains(updated, `w:after="400"`) || !strings.Contains(updated, `w:firstLineChars="0"`) || strings.Contains(updated, `<w:numPr>`) {
		t.Fatalf("trusted heading did not receive Heading 1 style: %s", updated)
	}
	if repeated, secondChanged := applyRoleFormatPlanToDocumentXML(updated, profile, assignments); secondChanged != 0 {
		t.Fatalf("second pass changed=%d, want 0:\nfirst:  %s\nsecond: %s", secondChanged, updated, repeated)
	}
}

func TestRolePlanHardRulesOverrideBadTemplateSamples(t *testing.T) {
	profile := &templateprofile.Profile{Styles: map[string]templateprofile.StyleRule{
		"title":     {FontEastAsia: "宋体", FontSizeHalfPt: "72", Bold: true, BoldSet: true},
		"body":      {FontEastAsia: "宋体", FontSizeHalfPt: "36", Bold: true, BoldSet: true, Line: "360", LineRule: "auto"},
		"heading_1": {FontEastAsia: "宋体", FontSizeHalfPt: "32", Bold: false, BoldSet: true, Alignment: "left", BeforeTwips: "0", AfterTwips: "0"},
	}}
	assignments := []roleclassify.Assignment{
		{NodeID: "p:TITLE", Role: "title", Confidence: 1, Trusted: true, Index: 0},
		{NodeID: "p:BODY", Role: "body", Confidence: 1, Trusted: true, Index: 1},
		{NodeID: "p:H1", Role: "heading_1", Confidence: 1, Trusted: true, Index: 2},
	}
	input := `<w:document xmlns:w="w" xmlns:w14="w14"><w:body>` +
		`<w:p w14:paraId="TITLE"><w:r><w:t>论文题名</w:t></w:r></w:p>` +
		`<w:p w14:paraId="BODY"><w:r><w:t>正文内容</w:t></w:r></w:p>` +
		`<w:p w14:paraId="H1"><w:r><w:t>1 绪论</w:t></w:r></w:p>` +
		`</w:body></w:document>`

	updated, changed := applyRoleFormatPlanToDocumentXML(input, profile, assignments)
	if changed != 3 {
		t.Fatalf("changed=%d, want 3", changed)
	}
	title := paragraphContaining(updated, "论文题名")
	body := paragraphContaining(updated, "正文内容")
	h1 := paragraphContaining(updated, "1 绪论")
	if strings.Contains(title, `w:val="72"`) || !strings.Contains(title, `w:val="30"`) {
		t.Fatalf("title hard rule not applied: %s", title)
	}
	if strings.Contains(body, `<w:b/>`) || !strings.Contains(body, `w:val="24"`) || !strings.Contains(body, `w:line="400" w:lineRule="exact"`) {
		t.Fatalf("body hard rule not applied: %s", body)
	}
	if !strings.Contains(h1, `w:eastAsia="黑体"`) || !strings.Contains(h1, `w:before="400"`) || !strings.Contains(h1, `w:after="400"`) {
		t.Fatalf("heading hard rule not applied: %s", h1)
	}
}

func TestRemoveHeading1AutoNumbering(t *testing.T) {
	styles := `<w:styles><w:style w:type="paragraph" w:styleId="Heading1"><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr></w:style><w:style w:type="paragraph" w:styleId="Heading2"><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr></w:style></w:styles>`
	updated := removeHeading1AutoNumbering(styles)
	if strings.Contains(updated, `w:styleId="Heading1"><w:pPr><w:numPr`) || !strings.Contains(updated, `w:styleId="Heading2"><w:pPr><w:numPr`) {
		t.Fatalf("Heading1 numbering removal touched the wrong style: %s", updated)
	}
}

func TestApplyHeading1StyleNormalizesPairedStyleElement(t *testing.T) {
	input := `<w:p><w:pPr><w:pStyle w:val="OldStyle"></w:pStyle></w:pPr><w:r><w:t>1 绪论</w:t></w:r></w:p>`
	updated := applyHeading1Style(input)
	if got := strings.Count(updated, `<w:pStyle`); got != 1 || !strings.Contains(updated, `<w:pStyle w:val="Heading1"/>`) {
		t.Fatalf("Heading 1 style was not normalized: %s", updated)
	}
	if again := applyHeading1Style(updated); again != updated {
		t.Fatalf("Heading 1 style is not idempotent:\nfirst:  %s\nsecond: %s", updated, again)
	}
}
