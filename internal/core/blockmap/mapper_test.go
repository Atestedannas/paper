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
