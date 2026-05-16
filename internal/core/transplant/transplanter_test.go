package transplant

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/blockmap"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/templatecompile"
)

func TestGenerateWritesDocxWithReplacedDocumentXML(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p>{{cover_title}}</w:p><w:p>{{abstract_cn_body}}</w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "cover_title", Payload: "Cover Title"},
			{BlockID: "abstract_cn_body", Payload: "Abstract Body"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	pkg, err := ooxmlpkg.Open(outputPath)
	if err != nil {
		t.Fatalf("generated docx is not openable: %v", err)
	}
	document, ok := pkg.Get("word/document.xml")
	if !ok {
		t.Fatal("generated docx missing word/document.xml")
	}
	got := string(document)
	if !strings.Contains(got, "Cover Title") {
		t.Fatalf("document.xml missing cover title: %s", got)
	}
	if !strings.Contains(got, "Abstract Body") {
		t.Fatalf("document.xml missing abstract body: %s", got)
	}
}

func TestGenerateEscapesXMLPayload(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>{{abstract_cn_body}}</w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "abstract_cn_body", Payload: `A&B <C> "D" 'E'`},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	want := `A&amp;B &lt;C&gt; &#34;D&#34; &#39;E&#39;`
	if !strings.Contains(document, want) {
		t.Fatalf("document.xml = %s, want escaped payload %s", document, want)
	}
}

func TestGeneratePatchTargetsOnlyReplaceSpecifiedTargets(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document>{{cover_title}}</w:document>`,
		"word/header1.xml":  `<w:hdr>{{cover_title}}</w:hdr>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{
			SkeletonPath: skeletonPath,
			PatchTargets: []string{"word/header1.xml"},
		},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "cover_title", Payload: "Header Title"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(document, "{{cover_title}}") {
		t.Fatalf("document.xml was unexpectedly patched: %s", document)
	}
	header := readDocxEntry(t, outputPath, "word/header1.xml")
	if !strings.Contains(header, "Header Title") {
		t.Fatalf("header1.xml was not patched: %s", header)
	}
}

func TestGenerateMissingPatchTargetReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document>{{cover_title}}</w:document>`,
	})

	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{
			SkeletonPath: skeletonPath,
			PatchTargets: []string{"word/missing.xml"},
		},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "cover_title", Payload: "Title"},
		}},
		OutputPath: filepath.Join(tmpDir, "output.docx"),
	})
	if err == nil {
		t.Fatal("Generate() error = nil, want missing patch target error")
	}
	if !strings.Contains(err.Error(), "word/missing.xml") {
		t.Fatalf("Generate() error = %v, want target name", err)
	}
}

func TestGenerateValidatesRequiredInput(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document/>`,
	})

	tests := []struct {
		name  string
		input GenerateInput
		want  string
	}{
		{
			name:  "nil template",
			input: GenerateInput{Mapping: &blockmap.MappingResult{}, OutputPath: filepath.Join(tmpDir, "out.docx")},
			want:  "compiled template",
		},
		{
			name:  "empty skeleton path",
			input: GenerateInput{CompiledTemplate: &templatecompile.CompiledTemplatePackage{}, Mapping: &blockmap.MappingResult{}, OutputPath: filepath.Join(tmpDir, "out.docx")},
			want:  "skeleton path",
		},
		{
			name:  "nil mapping",
			input: GenerateInput{CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath}, OutputPath: filepath.Join(tmpDir, "out.docx")},
			want:  "mapping",
		},
		{
			name:  "empty output path",
			input: GenerateInput{CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath}, Mapping: &blockmap.MappingResult{}},
			want:  "output path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewTransplanter().Generate(context.Background(), tt.input)
			if err == nil {
				t.Fatal("Generate() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Generate() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestGenerateMissingDefaultDocumentXMLReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/header1.xml": `<w:hdr/>`,
	})

	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping:          &blockmap.MappingResult{},
		OutputPath:       filepath.Join(tmpDir, "output.docx"),
	})
	if err == nil {
		t.Fatal("Generate() error = nil, want missing document.xml error")
	}
	if !strings.Contains(err.Error(), "word/document.xml") {
		t.Fatalf("Generate() error = %v, want document.xml in error", err)
	}
}

func TestGenerateRepeatableBindingsJoinWithNewline(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>{{references}}</w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "references", Payload: "Ref 1"},
			{BlockID: "references", Payload: "Ref 2"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(document, "Ref 1\nRef 2") {
		t.Fatalf("document.xml = %s, want joined repeatable payloads", document)
	}
}

func TestGenerateDoesNotRecursivelyReplaceInsertedPayload(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>{{a}} {{b}}</w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "a", Payload: "{{b}}"},
			{BlockID: "b", Payload: "B"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	want := `<w:document><w:body>{{b}} B</w:body></w:document>`
	if document != want {
		t.Fatalf("document.xml = %s, want non-recursive replacement %s", document, want)
	}
}

func TestGenerateUsesCompiledTemplateMappingContractTokens(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p>{{body}}</w:p><w:p>{{heading_1}}</w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "nested", "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{
			SkeletonPath: skeletonPath,
			MappingContract: templatecompile.MappingContract{
				BlockBindings: map[string]string{
					"block-body":    "{{body}}",
					"block-heading": "{{heading_1}}",
				},
			},
		},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "block-body", Payload: "Body from parsed paper"},
			{BlockID: "block-heading", Payload: "Introduction"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	for _, want := range []string{"Body from parsed paper", "Introduction"} {
		if !strings.Contains(document, want) {
			t.Fatalf("document.xml missing %q: %s", want, document)
		}
	}
	for _, forbidden := range []string{"{{body}}", "{{heading_1}}"} {
		if strings.Contains(document, forbidden) {
			t.Fatalf("document.xml still contains placeholder %q: %s", forbidden, document)
		}
	}
}

func TestGenerateExpandsContentBlocksIntoSeparateParagraphs(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{
			SkeletonPath: skeletonPath,
			MappingContract: templatecompile.MappingContract{
				BlockBindings: map[string]string{
					"content_blocks": "{{content_blocks}}",
				},
			},
		},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "1 Introduction"},
			{BlockID: "content_blocks", Payload: "First body paragraph"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	for _, want := range []string{"1 Introduction", "First body paragraph", `<w:ind w:firstLineChars="200" w:firstLine="480"/>`, `<w:sz w:val="32"/>`, `<w:b/><w:bCs/>`} {
		if !strings.Contains(document, want) {
			t.Fatalf("document.xml missing %q: %s", want, document)
		}
	}
	if strings.Contains(document, "<w:t><w:p>") || strings.Contains(document, "{{content_blocks}}") {
		t.Fatalf("document.xml has invalid or unreplaced content block placeholder: %s", document)
	}
}

func TestGenerateCoalescesFragmentedBodyLines(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "4.1 单因素影响分析"},
			{BlockID: "content_blocks", Payload: "经单因素分析结果显示，不同年龄、职业、居住地、"},
			{BlockID: "content_blocks", Payload: "文化程度、家庭月收入、吸烟、家族史、"},
			{BlockID: "content_blocks", Payload: "接受过健康教育八个变量具有统计学意义。"},
			{BlockID: "content_blocks", Payload: "表4-1 单因素分析结果"},
			{BlockID: "content_blocks", Payload: `<w:tbl><w:tr><w:tc><w:p><w:r><w:t>A</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	merged := "经单因素分析结果显示，不同年龄、职业、居住地、文化程度、家庭月收入、吸烟、家族史、接受过健康教育八个变量具有统计学意义。"
	if !strings.Contains(document, merged) {
		t.Fatalf("document.xml missing merged body paragraph %q: %s", merged, document)
	}
	if strings.Count(document, `<w:ind w:firstLineChars="200" w:firstLine="480"/>`) != 1 {
		t.Fatalf("fragmented body lines should render as one indented paragraph: %s", document)
	}
	if !strings.Contains(document, "表4-1 单因素分析结果") || !strings.Contains(document, "<w:tbl>") {
		t.Fatalf("caption and table should stay separate after body coalescing: %s", document)
	}
}

func TestGenerateSplitsEmbeddedEnglishAbstractFromChineseKeywords(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "关键词：社区二型糖尿病；认知水平；影响因素 Abstract: Objective To explore the influencing factors."},
			{BlockID: "content_blocks", Payload: "Methods 190 patients were selected."},
			{BlockID: "content_blocks", Payload: "Key words: Community type 2 diabetes; Cognitive level"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Contains(document, "影响因素 Abstract:") {
		t.Fatalf("embedded Abstract marker should be split out of Chinese keywords: %s", document)
	}
	if strings.Count(document, "Abstract") != 1 || strings.Count(document, "Key words") != 1 {
		t.Fatalf("English abstract/keywords should be preserved as separate blocks: %s", document)
	}
	if strings.Count(document, "<w:p>") < 3 {
		t.Fatalf("Chinese keywords, English abstract, and key words should be separate paragraphs: %s", document)
	}
}

func TestGenerateDropsSourceTableOfContentsAndBuildsCleanTOC(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "\u6458\u8981\uff1a source abstract"},
			{BlockID: "content_blocks", Payload: "\u76ee      \u5f55"},
			{BlockID: "content_blocks", Payload: "\u6458\u8981\uff1a I"},
			{BlockID: "content_blocks", Payload: "1 \u7eea\u8bba 1"},
			{BlockID: "content_blocks", Payload: "\u81f4      \u8c22 13"},
			{BlockID: "content_blocks", Payload: "1 \u7eea\u8bba"},
			{BlockID: "content_blocks", Payload: "1.1 \u7814\u7a76\u80cc\u666f"},
			{BlockID: "content_blocks", Payload: "\u6b63\u6587\u5185\u5bb9\u3002"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Contains(document, "\u6458\u8981\uff1a I") || strings.Contains(document, "\u81f4      \u8c22 13") {
		t.Fatalf("source TOC entries with stale page numbers should be dropped: %s", document)
	}
	if strings.Count(document, "\u76ee      \u5f55") != 1 {
		t.Fatalf("clean generated TOC should be emitted exactly once: %s", document)
	}
	if strings.Count(document, "1 \u7eea\u8bba") < 2 {
		t.Fatalf("heading should appear in generated TOC and body: %s", document)
	}
	if !strings.Contains(document, `<w:spacing w:line="240"`) || !strings.Contains(document, `<w:sz w:val="20"`) {
		t.Fatalf("generated TOC entries should use compact typography: %s", document)
	}
}

func TestGenerateAppliesTemplateTypographySpacing(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "\u6458\u8981\uff1a abstract text"},
			{BlockID: "content_blocks", Payload: "1 \u7eea\u8bba"},
			{BlockID: "content_blocks", Payload: "1.1 \u7814\u7a76\u80cc\u666f"},
			{BlockID: "content_blocks", Payload: "1.1.1 \u7814\u7a76\u5bf9\u8c61"},
			{BlockID: "content_blocks", Payload: "5.5 \u603b\u7ed3"},
			{BlockID: "content_blocks", Payload: "\u6b63\u6587\u5c0f\u56db\u5b8b\u4f53\u6bb5\u843d\u3002"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	chapter := paragraphContainingText(t, document, "1 \u7eea\u8bba")
	for _, want := range []string{
		`w:eastAsia="宋体"`,
		`w:ascii="宋体"`,
		`<w:b/><w:bCs/>`,
		`w:beforeLines="100"`,
		`w:afterLines="100"`,
		`<w:adjustRightInd w:val="0"/>`,
		`<w:snapToGrid w:val="0"/>`,
		`<w:jc w:val="left"/>`,
		`<w:sz w:val="32"/>`,
	} {
		if !strings.Contains(chapter, want) {
			t.Fatalf("chapter heading missing %q: %s", want, chapter)
		}
	}
	if strings.Contains(chapter, `<w:jc w:val="center"/>`) {
		t.Fatalf("chapter heading should be top/left aligned, not centered: %s", chapter)
	}

	section := paragraphContainingText(t, document, "1.1 \u7814\u7a76\u80cc\u666f")
	for _, want := range []string{`w:eastAsia="宋体"`, `w:ascii="宋体"`, `<w:b/><w:bCs/>`, `<w:sz w:val="30"/>`, `w:line="360"`} {
		if !strings.Contains(section, want) {
			t.Fatalf("section heading missing %q: %s", want, section)
		}
	}

	third := paragraphContainingText(t, document, "1.1.1 \u7814\u7a76\u5bf9\u8c61")
	for _, want := range []string{`w:eastAsia="宋体"`, `w:ascii="宋体"`, `<w:b/><w:bCs/>`, `<w:sz w:val="28"/>`, `w:line="360"`} {
		if !strings.Contains(third, want) {
			t.Fatalf("third-level heading missing %q: %s", want, third)
		}
	}

	mixed := paragraphContainingText(t, document, "5.5 \u603b\u7ed3")
	if strings.Contains(mixed, `Times New Roman`) {
		t.Fatalf("numbered Chinese heading should not fall back to Times New Roman: %s", mixed)
	}

	body := paragraphContainingText(t, document, "\u6b63\u6587\u5c0f\u56db\u5b8b\u4f53\u6bb5\u843d")
	for _, want := range []string{`w:eastAsia="宋体"`, `w:ascii="宋体"`, `<w:sz w:val="24"/>`, `<w:ind w:firstLineChars="200" w:firstLine="480"/>`, `w:line="360"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body paragraph missing %q: %s", want, body)
		}
	}

	abstract := paragraphContainingText(t, document, "abstract text")
	if !strings.Contains(abstract, `w:afterLines="200"`) || !strings.Contains(abstract, `w:after="624"`) {
		t.Fatalf("abstract paragraph should keep template paragraph-after spacing: %s", abstract)
	}
	if !strings.Contains(abstract, `<w:ind w:firstLineChars="200" w:firstLine="480"/>`) {
		t.Fatalf("abstract paragraph should use template-style first-line chars indentation: %s", abstract)
	}
	for _, want := range []string{`w:eastAsia="黑体"`, `<w:b/><w:bCs/>`, `<w:sz w:val="30"/>`} {
		if !strings.Contains(abstract, want) {
			t.Fatalf("abstract lead label should be black small-three bold, missing %q: %s", want, abstract)
		}
	}
}

func TestRenderBackMatterUsesTemplateStyles(t *testing.T) {
	title := backMatterTitleParagraph("\u53c2\u8003\u6587\u732e")
	for _, want := range []string{
		`<w:jc w:val="center"/>`,
		`w:eastAsia="黑体"`,
		`<w:b/><w:bCs/>`,
		`<w:sz w:val="30"/>`,
		`w:afterLines="200"`,
		`w:after="624"`,
	} {
		if !strings.Contains(title, want) {
			t.Fatalf("back matter title missing %q: %s", want, title)
		}
	}

	references := renderReferences([]string{"[1] first\n[2] second"})
	if strings.Count(references, "<w:p>") != 2 {
		t.Fatalf("references should render each entry as its own paragraph: %s", references)
	}
	for _, want := range []string{`<w:sz w:val="21"/>`, `w:line="288"`} {
		if !strings.Contains(references, want) {
			t.Fatalf("references missing %q: %s", want, references)
		}
	}

	thanks := renderAcknowledgements([]string{"\u611f\u8c22\u6307\u5bfc\u8001\u5e08\u3002"})
	for _, want := range []string{`w:eastAsia="宋体"`, `<w:sz w:val="21"/>`, `<w:ind w:firstLineChars="200" w:firstLine="420"/>`, `w:line="360"`} {
		if !strings.Contains(thanks, want) {
			t.Fatalf("acknowledgement missing %q: %s", want, thanks)
		}
	}
}

func TestRenderCQRWSTFrontMatterTitleMergesContinuation(t *testing.T) {
	title := renderCQRWSTFrontMatterTitle(map[string]string{
		"题目":   "社区2型糖尿病患者疾病知识",
		"题目续行": "认知现状及影响因素分析",
	})

	if strings.Count(title, "<w:p>") != 1 {
		t.Fatalf("front-matter title should render as one paragraph: %s", title)
	}
	for _, want := range []string{
		"社区2型糖尿病患者疾病知识认知现状及影响因素分析",
		`<w:jc w:val="center"/>`,
		`<w:snapToGrid w:val="0"/>`,
		`w:ascii="黑体"`,
		`w:eastAsia="黑体"`,
		`<w:b/><w:bCs/>`,
		`<w:sz w:val="32"/>`,
		`w:line="360"`,
		`w:afterLines="200"`,
	} {
		if !strings.Contains(title, want) {
			t.Fatalf("front-matter title missing %q: %s", want, title)
		}
	}
	if strings.Contains(title, `Times New Roman`) {
		t.Fatalf("front-matter title should keep the complete title, including digits, in black font: %s", title)
	}
}

func TestGenerateInjectsContentBlocksWhenTemplateHasNoAnchors(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml":  `<w:document><w:body><w:p><w:r><w:t>Template front matter</w:t></w:r></w:p><w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr></w:body></w:document>`,
		"word/header1.xml":   `<w:hdr><w:p><w:r><w:t>Template header</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml":   `<w:ftr><w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> PAGE </w:instrText></w:r></w:p></w:ftr>`,
		"word/footnotes.xml": `<w:footnotes><w:footnote w:id="1"><w:p><w:r><w:t>Template footnote</w:t></w:r></w:p></w:footnote></w:footnotes>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "1 Introduction"},
			{BlockID: "content_blocks", Payload: "Source body paragraph"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	for _, want := range []string{"Template front matter", "1 Introduction", "Source body paragraph"} {
		if !strings.Contains(document, want) {
			t.Fatalf("document.xml missing %q: %s", want, document)
		}
	}
	if strings.Index(document, "Source body paragraph") > strings.Index(document, "<w:sectPr") {
		t.Fatalf("source content was inserted after final sectPr: %s", document)
	}
	if !strings.Contains(readDocxEntry(t, outputPath, "word/header1.xml"), "Template header") {
		t.Fatal("generated docx did not preserve template header")
	}
	if !strings.Contains(readDocxEntry(t, outputPath, "word/footer1.xml"), "PAGE") {
		t.Fatal("generated docx did not preserve template page field footer")
	}
	if !strings.Contains(readDocxEntry(t, outputPath, "word/footnotes.xml"), "Template footnote") {
		t.Fatal("generated docx did not preserve template footnotes")
	}
}

func TestGeneratePreservesContentBlockTablesAsOOXML(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	tableXML := `<w:tbl><w:tblPr><w:tblpPr w:tblpX="1"/><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid><w:gridCol w:w="1200"/><w:gridCol w:w="2400"/></w:tblGrid><w:tr><w:tc><w:p><w:r><w:t>A1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>B1</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "Before"},
			{BlockID: "content_blocks", Payload: tableXML},
			{BlockID: "content_blocks", Payload: "After"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(document, "<w:tbl>") || !strings.Contains(document, "<w:t>A1</w:t>") || !strings.Contains(document, "<w:t>B1</w:t>") {
		t.Fatalf("document.xml missing inserted table OOXML: %s", document)
	}
	if strings.Contains(document, "&lt;w:tbl") {
		t.Fatalf("document.xml escaped table instead of inserting OOXML: %s", document)
	}
	if strings.Contains(document, "w:tblpPr") {
		t.Fatalf("document.xml kept floating table properties: %s", document)
	}
	insertedTable := tablePattern.FindString(document)
	if strings.Contains(insertedTable, "firstLine") || strings.Contains(insertedTable, `w:val="both"`) {
		t.Fatalf("inserted table kept paragraph formatting that crushes table cells: %s", insertedTable)
	}
	if strings.Contains(insertedTable, "w:trHeight") {
		t.Fatalf("inserted table kept fixed row heights: %s", insertedTable)
	}
}

func TestGenerateKeepsTableCaptionWithFollowingTable(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	tableXML := `<w:tbl><w:tr><w:tc><w:p><w:r><w:t>A1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>B1</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "\u88683-1 \u60a3\u8005\u57fa\u672c\u8d44\u6599"},
			{BlockID: "content_blocks", Payload: tableXML},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	captionStart := strings.Index(document, "\u88683-1")
	tableStart := strings.Index(document, "<w:tbl>")
	if captionStart < 0 || tableStart < 0 || captionStart > tableStart {
		t.Fatalf("caption should be emitted immediately before table: %s", document)
	}
	captionXML := document[captionStart:]
	if previousParagraphStart := strings.LastIndex(document[:captionStart], "<w:p>"); previousParagraphStart >= 0 {
		captionXML = document[previousParagraphStart:captionStart]
	}
	if !strings.Contains(captionXML, "<w:keepNext/>") || !strings.Contains(captionXML, `<w:jc w:val="center"/>`) {
		t.Fatalf("table caption should be centered and kept with following table: %s", captionXML)
	}
	if strings.Contains(captionXML, "firstLine") {
		t.Fatalf("table caption should not use body first-line indent: %s", captionXML)
	}
}

func TestGeneratePreservesTableMergeStructure(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	tableXML := `<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid><w:gridCol w:w="1800"/><w:gridCol w:w="1800"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:gridSpan w:val="2"/></w:tcPr><w:p><w:r><w:t>Merged header</w:t></w:r></w:p></w:tc></w:tr><w:tr><w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr><w:p><w:r><w:t>A</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>B</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: tableXML},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	insertedTable := tablePattern.FindString(document)
	for _, want := range []string{`<w:gridSpan w:val="2"/>`, `<w:vMerge w:val="restart"/>`, `<w:tblLayout w:type="fixed"/>`, `<w:jc w:val="center"/>`, `<w:tblW w:w="3600" w:type="dxa"/>`} {
		if !strings.Contains(insertedTable, want) {
			t.Fatalf("inserted table missing preserved/stabilized structure %q: %s", want, insertedTable)
		}
	}
}

func TestGenerateConstrainsContentTableWidthAndCellWidths(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	tableXML := `<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid><w:gridCol w:w="6000"/><w:gridCol w:w="6000"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="6000" w:type="dxa"/></w:tcPr><w:p><w:r><w:t>A</w:t></w:r></w:p></w:tc><w:tc><w:tcPr><w:tcW w:w="6000" w:type="dxa"/></w:tcPr><w:p><w:r><w:t>B</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: tableXML},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	insertedTable := tablePattern.FindString(readDocxEntry(t, outputPath, "word/document.xml"))
	for _, want := range []string{`<w:tblW w:w="8640" w:type="dxa"/>`, `<w:gridCol w:w="4320"/>`, `<w:tcW w:w="4320" w:type="dxa"/>`} {
		if !strings.Contains(insertedTable, want) {
			t.Fatalf("inserted table missing constrained width %q: %s", want, insertedTable)
		}
	}
}

func TestGenerateAppliesThreeLineTableBordersAndRepeatingHeader(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	tableXML := `<w:tbl><w:tblPr><w:tblBorders><w:left w:val="single"/><w:right w:val="single"/><w:insideV w:val="single"/></w:tblBorders></w:tblPr><w:tblGrid><w:gridCol w:w="2400"/><w:gridCol w:w="2400"/></w:tblGrid><w:tr><w:trPr><w:cantSplit/></w:trPr><w:tc><w:p><w:r><w:t>H1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>H2</w:t></w:r></w:p></w:tc></w:tr><w:tr><w:trPr><w:cantSplit/></w:trPr><w:tc><w:p><w:r><w:t>A</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>B</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: tableXML},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	insertedTable := tablePattern.FindString(readDocxEntry(t, outputPath, "word/document.xml"))
	for _, want := range []string{
		`<w:top w:val="single" w:sz="12" w:space="0" w:color="000000"/>`,
		`<w:bottom w:val="single" w:sz="12" w:space="0" w:color="000000"/>`,
		`<w:insideH w:val="single" w:sz="4" w:space="0" w:color="000000"/>`,
		`<w:left w:val="nil"/>`,
		`<w:right w:val="nil"/>`,
		`<w:insideV w:val="nil"/>`,
		`<w:tblHeader/>`,
	} {
		if !strings.Contains(insertedTable, want) {
			t.Fatalf("inserted table missing three-line/repeating-header property %q: %s", want, insertedTable)
		}
	}
	if strings.Contains(insertedTable, "cantSplit") {
		t.Fatalf("inserted table should allow long rows to split across pages: %s", insertedTable)
	}
}

func TestGenerateMakesDenseTablesReadableWithinTextWidth(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	tableXML := `<w:tbl><w:tblPr><w:tblW w:w="12000" w:type="dxa"/></w:tblPr><w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="1600"/><w:gridCol w:w="800"/><w:gridCol w:w="1500"/><w:gridCol w:w="1500"/><w:gridCol w:w="1400"/><w:gridCol w:w="1200"/><w:gridCol w:w="1000"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="800" w:type="dxa"/></w:tcPr><w:p><w:r><w:rPr><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr><w:t>文化程度</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: tableXML},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	insertedTable := tablePattern.FindString(readDocxEntry(t, outputPath, "word/document.xml"))
	if strings.Contains(insertedTable, `<w:gridCol w:w="800"/>`) || strings.Contains(insertedTable, `<w:tcW `) {
		t.Fatalf("dense table should not keep unreadably narrow grid/cell widths: %s", insertedTable)
	}
	for _, want := range []string{`<w:gridCol w:w="900"/>`, `<w:sz w:val="16"/>`, `<w:szCs w:val="16"/>`, `<w:tblCellMar><w:top w:w="20" w:type="dxa"/><w:start w:w="20" w:type="dxa"/><w:bottom w:w="20" w:type="dxa"/><w:end w:w="20" w:type="dxa"/></w:tblCellMar>`} {
		if !strings.Contains(insertedTable, want) {
			t.Fatalf("dense table missing readability normalization %q: %s", want, insertedTable)
		}
	}
}

func TestGenerateFillsTemplateCoverTableFields(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:tbl>` +
			`<w:tr><w:tc><w:p><w:r><w:t>Title</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>XXXXXXXX</w:t></w:r></w:p></w:tc></w:tr>` +
			`<w:tr><w:tc><w:p><w:r><w:t>College</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>YYYYYYYY</w:t></w:r></w:p></w:tc></w:tr>` +
			`</w:tbl>` +
			`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr>` +
			`</w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{
				"Title":   "Community Diabetes Study",
				"College": "Nursing College",
			},
			Bindings: []blockmap.Binding{
				{BlockID: "content_blocks", Payload: "1 Introduction"},
			},
		},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	for _, want := range []string{"Title", "Community Diabetes Study", "College", "Nursing College"} {
		if !strings.Contains(document, want) {
			t.Fatalf("document.xml missing %q: %s", want, document)
		}
	}
	for _, forbidden := range []string{"XXXXXXXX", "YYYYYYYY"} {
		if strings.Contains(document, forbidden) {
			t.Fatalf("document.xml still contains old cover placeholder %q: %s", forbidden, document)
		}
	}
}

func TestGenerateRebuildsCQRWSTCoverPageWithStableOOXML(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>本科毕业论文/设计</w:t></w:r></w:p><w:tbl><w:tblPr><w:tblpPr w:tblpX="2181" w:tblpY="554"/><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tr><w:tc><w:p><w:r><w:t>题目</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>XXXXXXXXXXXXXXXX</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{
				"题目":   "社区2型糖尿病患者疾病知识",
				"学院":   "护理学院",
				"专业":   "护理学",
				"班级":   "2022级护理学5班",
				"学号":   "20220152192",
				"姓名":   "张三",
				"指导教师": "李四",
				"完成日期": "2026年4月",
			},
			Bindings: []blockmap.Binding{{BlockID: "content_blocks", Payload: "1 Introduction"}},
		},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Contains(document, "XXXXXXXXXXXXXXXX") {
		t.Fatalf("document.xml still contains unstable original cover fragments: %s", document)
	}
	for _, want := range []string{"社区2型糖尿病患者疾病知识", "2026年4月", "w:tblpPr", "1 Introduction"} {
		if !strings.Contains(document, want) {
			t.Fatalf("document.xml missing %q: %s", want, document)
		}
	}
}

func TestGenerateRebuildsCQRWSTBodyWithMainFooterSection(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>本科毕业论文/设计</w:t></w:r></w:p>` +
			`<w:tbl><w:tblPr><w:tblpPr w:tblpX="2181" w:tblpY="554"/><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tr><w:tc><w:p><w:r><w:t>题目</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>XXXXXXXXXXXXXXXX</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
			`<w:p><w:pPr><w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:footerReference w:type="default" r:id="rId9"/><w:pgNumType w:fmt="upperRoman" w:start="0"/></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:footerReference w:type="default" r:id="rId11"/><w:pgNumType w:start="1"/></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rId22"/></w:sectPr></w:pPr></w:p>` +
			`</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId11" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer3.xml"/></Relationships>`,
		"word/footer3.xml":             `<w:ftr><w:p><w:r><w:t>第 </w:t></w:r><w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:t> 页</w:t></w:r></w:p></w:ftr>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{
				"题目": "社区2型糖尿病患者疾病知识",
			},
			Bindings: []blockmap.Binding{
				{BlockID: "content_blocks", Payload: "1 Introduction"},
				{BlockID: "content_blocks", Payload: "Body paragraph"},
			},
		},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	documentXML := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(documentXML, `<w:footerReference w:type="default" r:id="rId11"`) {
		t.Fatalf("rebuilt body should use the main body footer section: %s", documentXML)
	}
	if !strings.Contains(documentXML, `<w:headerReference w:type="default" r:id="rId8"`) {
		t.Fatalf("rebuilt body should preserve the template running header section: %s", documentXML)
	}
	if !strings.Contains(documentXML, `<w:pgNumType w:start="1"`) {
		t.Fatalf("rebuilt body footer should restart at page 1: %s", documentXML)
	}
	if strings.Contains(documentXML, `r:id="rId22"`) {
		t.Fatalf("rebuilt body should not use the final non-footer section as the body section: %s", documentXML)
	}
	footerXML := readDocxEntry(t, outputPath, "word/footer3.xml")
	if !strings.Contains(footerXML, "NUMPAGES") || strings.Contains(footerXML, "12页") {
		t.Fatalf("main footer should use dynamic page fields instead of stale template text: %s", footerXML)
	}
}

func TestGenerateInjectsReferencesAfterTemplateReferenceHeading(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p>` +
			`<w:p><w:r><w:br w:type="page"/></w:r><w:r><w:t>References</w:t></w:r></w:p>` +
			`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr>` +
			`</w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "1 Introduction"},
			{BlockID: "references", Payload: "[1] Reference"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(document, "References") || !strings.Contains(document, "[1] Reference") {
		t.Fatalf("document.xml missing reference section content: %s", document)
	}
	if strings.Index(document, "[1] Reference") < strings.Index(document, "References") {
		t.Fatalf("reference payload should appear after template reference heading: %s", document)
	}
}

func TestGenerateNormalizesStartAlignmentForRendererCompatibility(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:pPr><w:jc w:val="start"/></w:pPr><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
		"word/styles.xml":   `<w:styles><w:style><w:pPr><w:jc w:val="start"/></w:pPr></w:style></w:styles>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "Body"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, entry := range []string{"word/document.xml", "word/styles.xml"} {
		content := readDocxEntry(t, outputPath, entry)
		if strings.Contains(content, `w:val="start"`) {
			t.Fatalf("%s still contains renderer-incompatible start alignment: %s", entry, content)
		}
	}
	if !strings.Contains(readDocxEntry(t, outputPath, "word/styles.xml"), `w:val="left"`) {
		t.Fatalf("styles.xml missing normalized left alignment")
	}
}

func TestGenerateNormalizesFloatingTablesForRendererCompatibility(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:tbl><w:tblPr><w:tblpPr w:tblpX="2181" w:tblpY="554"/><w:tblOverlap w:val="never"/><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid><w:gridCol w:w="1176"/><w:gridCol w:w="6643"/></w:tblGrid><w:tr><w:tc><w:p><w:r><w:t>Title</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>XXXXX</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{"Title": "Community Diabetes Study"},
		},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	for _, forbidden := range []string{"w:tblpPr", "w:tblOverlap"} {
		if strings.Contains(document, forbidden) {
			t.Fatalf("document.xml still contains renderer-incompatible %s: %s", forbidden, document)
		}
	}
	if !strings.Contains(document, "Community Diabetes Study") {
		t.Fatalf("document.xml missing filled cover field: %s", document)
	}
	if !strings.Contains(document, `<w:tblW w:w="7819" w:type="dxa"/>`) {
		t.Fatalf("document.xml missing fixed table width from grid: %s", document)
	}
	if !strings.Contains(document, `<w:jc w:val="center"/>`) {
		t.Fatalf("document.xml missing centered table alignment: %s", document)
	}
}

func TestGenerateReturnsContextError(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document>{{cover_title}}</w:document>`,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := NewTransplanter().Generate(ctx, GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping:          &blockmap.MappingResult{},
		OutputPath:       filepath.Join(tmpDir, "output.docx"),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Generate() error = %v, want context.Canceled", err)
	}
}

func writeTestDocx(t *testing.T, path string, entries map[string]string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test docx: %v", err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	defer zw.Close()

	baseEntries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
	}
	for name, content := range entries {
		baseEntries[name] = content
	}

	for name, content := range baseEntries {
		writer, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
}

func readDocxEntry(t *testing.T, path, name string) string {
	t.Helper()

	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		t.Fatalf("open docx %s: %v", path, err)
	}
	content, ok := pkg.Get(name)
	if !ok {
		t.Fatalf("missing docx entry %s", name)
	}
	return string(content)
}

func paragraphContainingText(t *testing.T, document string, text string) string {
	t.Helper()
	for _, match := range paragraphPattern.FindAllString(document, -1) {
		if strings.Contains(match, text) {
			return match
		}
	}
	t.Fatalf("missing paragraph containing %q in %s", text, document)
	return ""
}
