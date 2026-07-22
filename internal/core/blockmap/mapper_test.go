package blockmap

import (
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/paperparse"
	"github.com/paper-format-checker/backend/internal/core/templatecompile"
)

func TestMapUsesTemplateOrderBeforePaperOrder(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "refs", Kind: "references", SlotType: "single", OrderIndex: 30, Accepts: []string{"references"}},
			{BlockID: "title", Kind: "cover_title", SlotType: "single", OrderIndex: 10, Accepts: []string{"cover_title"}},
			{BlockID: "abstract", Kind: "abstract_cn_body", SlotType: "single", OrderIndex: 20, Accepts: []string{"abstract_cn_body"}},
		},
	}
	paper := &paperparse.ParsedPaper{
		CoverFields: map[string]string{"cover_title": "A Template First Paper"},
		AbstractCN:  []string{"摘要正文"},
		References:  []string{"[1] first"},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	got := bindingIDs(result.Bindings)
	want := []string{"title", "abstract", "refs"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("binding order = %v, want %v", got, want)
	}
}

func TestMapCarriesAbnormalEntriesToUnmappedBlocks(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "title", Kind: "cover_title", SlotType: "single", OrderIndex: 1},
		},
	}
	paper := &paperparse.ParsedPaper{
		CoverFields: map[string]string{"cover_title": "Title"},
		Abnormal:    []string{"orphan paragraph", "bad table"},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	got := strings.Join(result.UnmappedBlocks, "|")
	want := "orphan paragraph|bad table"
	if got != want {
		t.Fatalf("UnmappedBlocks = %q, want %q", got, want)
	}
}

func TestMapAddsBackMatterFallbackBindingsWithoutTemplateSlots(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "content_blocks", Kind: "body", SlotType: "repeatable", OrderIndex: 1, Accepts: []string{"content_blocks"}},
		},
	}
	paper := &paperparse.ParsedPaper{
		ContentBlocks: []paperparse.ContentBlock{{Kind: "body", Text: "1 \u7eea\u8bba"}},
		References:    []string{"[1] first reference", "[2] second reference"},
		Acknowledgements: []string{
			"\u611f\u8c22\u6307\u5bfc\u8001\u5e08\u3002",
		},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	bindings := map[string]string{}
	for _, binding := range result.Bindings {
		bindings[binding.BlockID] = binding.Payload
	}
	if !strings.Contains(bindings["references"], "[1] first reference") || !strings.Contains(bindings["references"], "[2] second reference") {
		t.Fatalf("references fallback binding missing or incomplete: %#v", result.Bindings)
	}
	if !strings.Contains(bindings["acknowledgement"], "\u611f\u8c22\u6307\u5bfc\u8001\u5e08") {
		t.Fatalf("acknowledgement fallback binding missing: %#v", result.Bindings)
	}
}

func TestMapSplitsAcknowledgementSwallowedByReferences(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "content_blocks", Kind: "content_blocks", SlotType: "repeatable", OrderIndex: 1, Accepts: []string{"content_blocks"}},
		},
	}
	paper := &paperparse.ParsedPaper{
		ContentBlocks: []paperparse.ContentBlock{{Kind: "body", Text: "1 \u7eea\u8bba"}},
		References: []string{
			"[1] first reference",
			"\u81f4      \u8c22",
			"\u611f\u8c22\u6307\u5bfc\u8001\u5e08\u3002",
		},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	bindings := map[string]string{}
	for _, binding := range result.Bindings {
		bindings[binding.BlockID] = binding.Payload
	}
	if strings.Contains(bindings["references"], "\u81f4") || strings.Contains(bindings["references"], "\u611f\u8c22") {
		t.Fatalf("references should not contain acknowledgement text: %#v", result.Bindings)
	}
	if !strings.Contains(bindings["acknowledgement"], "\u611f\u8c22\u6307\u5bfc\u8001\u5e08") {
		t.Fatalf("acknowledgement should be split from swallowed references: %#v", result.Bindings)
	}
}

func TestMapBindsCoverTitleFromParserShapedChineseKey(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "title", Kind: "cover_title", SlotType: "single", OrderIndex: 1},
		},
	}
	paper := &paperparse.ParsedPaper{
		CoverFields: map[string]string{
			"\u9898\u76ee": "Parser Shaped Title",
		},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	if len(result.Bindings) != 1 {
		t.Fatalf("len(Bindings) = %d, want 1", len(result.Bindings))
	}
	if result.Bindings[0].BlockID != "title" || result.Bindings[0].Payload != "Parser Shaped Title" {
		t.Fatalf("cover title binding = %#v, want parser-shaped title payload", result.Bindings[0])
	}
}

func TestMapRepeatableHeadingOneProducesMultipleBindings(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "h1", Kind: "heading_1", SlotType: "repeatable", OrderIndex: 1, Accepts: []string{"heading_1"}},
		},
	}
	paper := &paperparse.ParsedPaper{
		Headings: []paperparse.Heading{
			{Level: 2, Text: "Ignored subsection"},
			{Level: 1, Text: "Introduction"},
			{Level: 1, Text: "Conclusion"},
		},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	if len(result.Bindings) != 2 {
		t.Fatalf("len(Bindings) = %d, want 2", len(result.Bindings))
	}
	if result.Bindings[0].BlockID != "h1" || result.Bindings[0].Payload != "Introduction" {
		t.Fatalf("first heading binding = %#v, want block h1 payload Introduction", result.Bindings[0])
	}
	if result.Bindings[1].BlockID != "h1" || result.Bindings[1].Payload != "Conclusion" {
		t.Fatalf("second heading binding = %#v, want block h1 payload Conclusion", result.Bindings[1])
	}
}

func TestMapRepeatableBodyProducesParagraphBindings(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "body-slot", Kind: "body", SlotType: "repeatable", OrderIndex: 1, Accepts: []string{"body"}},
		},
	}
	paper := &paperparse.ParsedPaper{
		Body: []string{"First body paragraph", "Second body paragraph"},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	if len(result.Bindings) != 2 {
		t.Fatalf("len(Bindings) = %d, want 2", len(result.Bindings))
	}
	if result.Bindings[0].BlockID != "body-slot" || result.Bindings[0].Payload != "First body paragraph" {
		t.Fatalf("first body binding = %#v", result.Bindings[0])
	}
	if result.Bindings[1].BlockID != "body-slot" || result.Bindings[1].Payload != "Second body paragraph" {
		t.Fatalf("second body binding = %#v", result.Bindings[1])
	}
}

func TestMapContentBlocksPreservesSourceOrder(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "content-slot", Kind: "content_blocks", SlotType: "repeatable", OrderIndex: 1, Accepts: []string{"content_blocks"}},
		},
	}
	paper := &paperparse.ParsedPaper{
		ContentBlocks: []paperparse.ContentBlock{
			{Kind: "heading", Level: 1, Text: "1 Introduction"},
			{Kind: "body", Text: "First body paragraph"},
			{Kind: "heading", Level: 2, Text: "1.1 Background"},
		},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	got := make([]string, 0, len(result.Bindings))
	for _, binding := range result.Bindings {
		got = append(got, binding.Payload)
	}
	want := []string{"1 Introduction", "First body paragraph", "1.1 Background"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("content block payloads = %v, want %v", got, want)
	}
}

func TestMapContentBlocksUsesTableXMLPayload(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "content-slot", Kind: "content_blocks", SlotType: "repeatable", OrderIndex: 1, Accepts: []string{"content_blocks"}},
		},
	}
	tableXML := `<w:tbl><w:tr><w:tc><w:p><w:r><w:t>A1</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	paper := &paperparse.ParsedPaper{
		ContentBlocks: []paperparse.ContentBlock{
			{Kind: "body", Text: "Before"},
			{Kind: "table", Text: "A1", XML: tableXML},
			{Kind: "body", Text: "After"},
		},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	if len(result.Bindings) != 3 {
		t.Fatalf("Bindings = %#v, want three content bindings", result.Bindings)
	}
	if result.Bindings[1].Payload != tableXML {
		t.Fatalf("table payload = %q, want raw table XML", result.Bindings[1].Payload)
	}
	if result.Bindings[1].PayloadKind != "table" || result.Bindings[1].PayloadXML != tableXML || result.Bindings[1].SourceIndex != 1 {
		t.Fatalf("table metadata = %#v", result.Bindings[1])
	}
}

func TestMapContentBlocksExcludesBackMatter(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "content-slot", Kind: "content_blocks", SlotType: "repeatable", OrderIndex: 1, Accepts: []string{"content_blocks"}},
		},
	}
	paper := &paperparse.ParsedPaper{
		ContentBlocks: []paperparse.ContentBlock{
			{Kind: "heading", Level: 1, Text: "1 Introduction"},
			{Kind: "body", Text: "Main body"},
			{Kind: "section_label", Text: "References"},
			{Kind: "references", Text: "[1] Reference"},
			{Kind: "acknowledgement", Text: "Thanks"},
		},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	got := make([]string, 0, len(result.Bindings))
	for _, binding := range result.Bindings {
		got = append(got, binding.Payload)
	}
	want := []string{"1 Introduction", "Main body"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("content block payloads = %v, want %v", got, want)
	}
}

func TestMapContentBlocksExcludesCoverFieldFragments(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "content_blocks", Kind: "content_blocks", SlotType: "repeatable", OrderIndex: 1},
		},
	}
	paper := &paperparse.ParsedPaper{
		CoverFields: map[string]string{
			"题目": "社区2型糖尿病患者疾病知识",
			"学院": "护理学院",
			"学号": "20220152192",
		},
		ContentBlocks: []paperparse.ContentBlock{
			{Kind: "body", Text: "本科毕业论文/设计"},
			{Kind: "body", Text: "题目"},
			{Kind: "body", Text: "社区2型糖尿病患者疾病知识"},
			{Kind: "body", Text: "学院"},
			{Kind: "body", Text: "护理学院"},
			{Kind: "body", Text: "摘要：这是正文应该保留"},
		},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	if len(result.Bindings) != 1 || result.Bindings[0].Payload != "摘要：这是正文应该保留" {
		t.Fatalf("Bindings = %#v, want only non-cover content", result.Bindings)
	}
}

func TestMapSplitsReferencePayloadsFromContentBlocks(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "content_blocks", Kind: "content_blocks", SlotType: "repeatable", OrderIndex: 1},
			{BlockID: "references", Kind: "references", SlotType: "single", OrderIndex: 2},
		},
	}
	paper := &paperparse.ParsedPaper{
		CoverFields: map[string]string{"题目": "Source"},
		ContentBlocks: []paperparse.ContentBlock{
			{Kind: "body", Text: "1 绪论"},
			{Kind: "body", Text: "正文内容"},
			{Kind: "body", Text: "参考文献"},
			{Kind: "body", Text: "[1] 张三. 文献."},
			{Kind: "body", Text: "[2] 李四. 文献."},
		},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	var contentPayloads []string
	var referencePayload string
	for _, binding := range result.Bindings {
		switch binding.BlockID {
		case "content_blocks":
			contentPayloads = append(contentPayloads, binding.Payload)
		case "references":
			referencePayload = binding.Payload
		}
	}
	if strings.Contains(strings.Join(contentPayloads, "\n"), "参考文献") || strings.Contains(strings.Join(contentPayloads, "\n"), "[1]") {
		t.Fatalf("content payloads still contain references: %#v", contentPayloads)
	}
	if !strings.Contains(referencePayload, "[1] 张三. 文献.") || !strings.Contains(referencePayload, "[2] 李四. 文献.") {
		t.Fatalf("reference payload = %q, want derived references", referencePayload)
	}
}

func TestMapDoesNotTreatReferenceTOCEntryAsReferenceHeading(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "content_blocks", Kind: "content_blocks", SlotType: "repeatable", OrderIndex: 1},
			{BlockID: "references", Kind: "references", SlotType: "single", OrderIndex: 2},
		},
	}
	paper := &paperparse.ParsedPaper{
		ContentBlocks: []paperparse.ContentBlock{
			{Kind: "body", Text: "\u76ee      \u5f55"},
			{Kind: "body", Text: "\u53c2\u8003\u6587\u732e\t12"},
			{Kind: "heading", Level: 1, Text: "1 \u7eea\u8bba"},
			{Kind: "body", Text: "\u6b63\u6587\u5185\u5bb9"},
			{Kind: "section_label", Text: "\u53c2\u8003\u6587\u732e"},
			{Kind: "references", Text: "[1] reference"},
		},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	var contentPayloads []string
	for _, binding := range result.Bindings {
		if binding.BlockID == "content_blocks" {
			contentPayloads = append(contentPayloads, binding.Payload)
		}
	}
	got := strings.Join(contentPayloads, "|")
	if !strings.Contains(got, "\u6b63\u6587\u5185\u5bb9") {
		t.Fatalf("content payloads = %q, want body after reference TOC entry", got)
	}
	if strings.Contains(got, "[1] reference") {
		t.Fatalf("content payloads include actual references: %q", got)
	}
}

func TestMapRejectsEmptyInputs(t *testing.T) {
	mapper := NewMapper()
	paper := &paperparse.ParsedPaper{CoverFields: map[string]string{"cover_title": "Title"}}
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{{BlockID: "title", Kind: "cover_title", OrderIndex: 1}},
	}

	if _, err := mapper.Map(nil, paper); err == nil {
		t.Fatal("Map(nil template) error = nil, want error")
	}
	if _, err := mapper.Map(&templatecompile.CompiledTemplatePackage{}, paper); err == nil {
		t.Fatal("Map(empty template) error = nil, want error")
	}
	if _, err := mapper.Map(template, nil); err == nil {
		t.Fatal("Map(nil paper) error = nil, want error")
	}
	if _, err := mapper.Map(template, &paperparse.ParsedPaper{}); err == nil {
		t.Fatal("Map(empty paper) error = nil, want error")
	}
}

func TestMapReportsAmbiguousBlocks(t *testing.T) {
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "dup", Kind: "cover_title", SlotType: "single", OrderIndex: 1},
			{BlockID: "dup", Kind: "abstract_cn_body", SlotType: "single", OrderIndex: 2},
			{BlockID: "h1-single", Kind: "heading_1", SlotType: "single", OrderIndex: 3},
		},
	}
	paper := &paperparse.ParsedPaper{
		CoverFields: map[string]string{"cover_title": "Title"},
		AbstractCN:  []string{"摘要"},
		Headings: []paperparse.Heading{
			{Level: 1, Text: "One"},
			{Level: 1, Text: "Two"},
		},
	}

	result, err := NewMapper().Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	if !contains(result.AmbiguousBlocks, "dup") {
		t.Fatalf("AmbiguousBlocks = %v, want duplicate block id dup", result.AmbiguousBlocks)
	}
	if !contains(result.AmbiguousBlocks, "h1-single") {
		t.Fatalf("AmbiguousBlocks = %v, want single heading block h1-single", result.AmbiguousBlocks)
	}
}

func bindingIDs(bindings []Binding) []string {
	ids := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		ids = append(ids, binding.BlockID)
	}
	return ids
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
