package transplant

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
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

func TestNormalizeFinalDOCXRemovesWhiteShadingAndPaginationArtifacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "final.docx")
	writeTestDocx(t, path, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:pPr><w:shd w:val="clear" w:fill="FFFFFF"/><w:pageBreakBefore/><w:keepLines/></w:pPr><w:r><w:t>正文</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:shd w:val="clear" w:fill="D9EAD3"/></w:pPr><w:r><w:t>保留底纹</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})

	changed, err := NormalizeFinalDOCX(path)
	if err != nil {
		t.Fatalf("NormalizeFinalDOCX() error = %v", err)
	}
	if changed == 0 {
		t.Fatal("NormalizeFinalDOCX() changed = 0")
	}
	documentXML := readDocxEntry(t, path, "word/document.xml")
	for _, forbidden := range []string{`w:fill="FFFFFF"`, "pageBreakBefore", "keepLines"} {
		if strings.Contains(documentXML, forbidden) {
			t.Fatalf("document.xml still contains %q: %s", forbidden, documentXML)
		}
	}
	if !strings.Contains(documentXML, `w:fill="D9EAD3"`) {
		t.Fatalf("non-white shading was removed: %s", documentXML)
	}
	settingsXML := readDocxEntry(t, path, "word/settings.xml")
	if !strings.Contains(settingsXML, `<w:updateFields w:val="true"/>`) {
		t.Fatalf("settings.xml does not update fields on open: %s", settingsXML)
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

func TestGenerateFinalizesTemplateReviewMarkup(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/comments.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"/></Types>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdComments" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="comments.xml"/>` +
			`</Relationships>`,
		"word/comments.xml": `<w:comments><w:comment w:id="0"><w:p><w:r><w:t>review note</w:t></w:r></w:p></w:comment></w:comments>`,
		"word/document.xml": `<w:document><w:body><w:p>` +
			`<w:commentRangeStart w:id="0"/>` +
			`<w:r><w:t>{{abstract_cn_body}}</w:t></w:r>` +
			`<w:ins><w:r><w:t>accepted text</w:t></w:r></w:ins>` +
			`<w:del><w:r><w:delText>deleted text</w:delText></w:r></w:del>` +
			`<w:r><w:commentReference w:id="0"/></w:r>` +
			`<w:commentRangeEnd w:id="0"/>` +
			`</w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "abstract_cn_body", Payload: "clean body"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	rels := readDocxEntry(t, outputPath, "word/_rels/document.xml.rels")
	types := readDocxEntry(t, outputPath, "[Content_Types].xml")
	if _, ok := openTestPackage(t, outputPath).Get("word/comments.xml"); ok {
		t.Fatal("generated docx should remove comments.xml")
	}
	for _, forbidden := range []string{"commentRange", "commentReference", "<w:ins", "<w:del", "deleted text", "comments"} {
		if strings.Contains(document+rels+types, forbidden) {
			t.Fatalf("generated docx still contains review markup %q:\n%s", forbidden, document)
		}
	}
	for _, want := range []string{"clean body", "accepted text"} {
		if !strings.Contains(document, want) {
			t.Fatalf("generated docx missing %q after finalizing review markup: %s", want, document)
		}
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

func TestGenerateDoesNotOverwriteGenericTemplateHeaderFooter(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
			`<w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p>` +
			`<w:sectPr><w:headerReference w:type="default" r:id="rIdHeader"/><w:footerReference w:type="default" r:id="rIdFooter"/></w:sectPr>` +
			`</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdHeader" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>` +
			`<Relationship Id="rIdFooter" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>` +
			`</Relationships>`,
		"word/header1.xml": `<w:hdr><w:p><w:r><w:t>Generic University Header</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml": `<w:ftr><w:p><w:r><w:t>Generic Footer</w:t></w:r></w:p></w:ftr>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{"Title": "Any title", "Major": "Any major"},
			Bindings: []blockmap.Binding{
				{BlockID: "content_blocks", Payload: "Body from source"},
			},
		},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	header := readDocxEntry(t, outputPath, "word/header1.xml")
	if !strings.Contains(header, "Generic University Header") || strings.Contains(header, "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u9662") {
		t.Fatalf("generic template header was overwritten: %s", header)
	}
	footer := readDocxEntry(t, outputPath, "word/footer1.xml")
	if !strings.Contains(footer, "Generic Footer") || strings.Contains(footer, "NUMPAGES") {
		t.Fatalf("generic template footer was overwritten: %s", footer)
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
			{BlockID: "content_blocks", Payload: "4.1 Single factor analysis"},
			{BlockID: "content_blocks", Payload: "The first fragment continues"},
			{BlockID: "content_blocks", Payload: "with the second fragment"},
			{BlockID: "content_blocks", Payload: "and ends in the third fragment."},
			{BlockID: "content_blocks", Payload: "\u8868 1 Analysis results"},
			{BlockID: "content_blocks", Payload: `<w:tbl><w:tr><w:tc><w:p><w:r><w:t>A</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	merged := "The first fragment continues with the second fragment and ends in the third fragment."
	if !strings.Contains(document, merged) {
		t.Fatalf("document.xml missing merged body paragraph %q: %s", merged, document)
	}
	if strings.Count(document, `<w:ind w:firstLineChars="200" w:firstLine="480"/>`) != 1 {
		t.Fatalf("fragmented body lines should render as one indented paragraph: %s", document)
	}
	if !strings.Contains(document, "\u8868 1 Analysis results") || !strings.Contains(document, "<w:tbl>") {
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
			{BlockID: "content_blocks", Payload: "\u5173\u952e\u8bcd\uff1a\u793e\u533a\u4e8c\u578b\u7cd6\u5c3f\u75c5\uff1b\u8ba4\u77e5\u6c34\u5e73\uff1b\u5f71\u54cd\u56e0\u7d20 Abstract: Objective To explore the influencing factors."},
			{BlockID: "content_blocks", Payload: "Methods 190 patients were selected."},
			{BlockID: "content_blocks", Payload: "Key words: Community type 2 diabetes; Cognitive level"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Contains(document, "\u5f71\u54cd\u56e0\u7d20 Abstract:") {
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
			{BlockID: "content_blocks", Payload: "\u76ee      \u5f55"},
			{BlockID: "content_blocks", Payload: "\u6458\u8981\uff1a I"},
			{BlockID: "content_blocks", Payload: "1 \u7eea\u8bba"},
			{BlockID: "content_blocks", Payload: "1.1 \u7814\u7a76\u80cc\u666f"},
			{BlockID: "content_blocks", Payload: "\u81f4      \u8c22 13"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Contains(document, "\u6458\u8981\uff1a I") {
		t.Fatalf("source TOC entries with stale page numbers should be dropped: %s", document)
	}
	if strings.Count(document, "\u76ee      \u5f55") != 1 {
		t.Fatalf("clean generated TOC should be emitted exactly once: %s", document)
	}
	if strings.Count(document, "1 \u7eea\u8bba") < 2 {
		t.Fatalf("heading should appear in generated TOC and body: %s", document)
	}
	if !strings.Contains(document, `<w:spacing w:line="240"`) || !strings.Contains(document, `<w:sz w:val="20"`) ||
		!strings.Contains(document, `<w:tab w:val="right" w:leader="dot" w:pos="9000"/>`) || !strings.Contains(document, `<w:tab/>`) {
		t.Fatalf("generated TOC entries should use compact typography: %s", document)
	}
	for _, want := range []string{`TOC \o "1-3" \h \z \u`, `w:fldCharType="begin"`, `w:fldCharType="separate"`, `w:fldCharType="end"`} {
		if !strings.Contains(document, want) {
			t.Fatalf("generated TOC should include real Word field %q: %s", want, document)
		}
	}
	settings := readDocxEntry(t, outputPath, "word/settings.xml")
	if !strings.Contains(settings, `<w:updateFields w:val="true"/>`) {
		t.Fatalf("settings.xml should request field updates on open: %s", settings)
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
			{BlockID: "content_blocks", Payload: "1.1\u7814\u7a76\u80cc\u666f"},
			{BlockID: "content_blocks", Payload: "1.1.1 \u7814\u7a76\u5bf9\u8c61"},
			{BlockID: "content_blocks", Payload: "5.5 \u603b\u7ed3"},
			{BlockID: "content_blocks", Payload: "190\u4f8b\u60a3\u8005\u7eb3\u5165\u7814\u7a76\u3002"},
			{BlockID: "content_blocks", Payload: "\u6b63\u6587\u5c0f\u56db\u5b8b\u4f53\u6bb5\u843d\u3002"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	chapter := paragraphContainingText(t, document, "1 \u7eea\u8bba")
	for _, want := range []string{`<w:pStyle w:val="Heading1"/>`, `<w:outlineLvl w:val="0"/>`, "w:eastAsia=\"\u5b8b\u4f53\"", "w:ascii=\"\u5b8b\u4f53\"", `<w:b/><w:bCs/>`, `w:beforeLines="100"`, `w:afterLines="100"`, `<w:adjustRightInd w:val="0"/>`, `<w:snapToGrid w:val="0"/>`, `<w:jc w:val="left"/>`, `<w:sz w:val="32"/>`} {
		if !strings.Contains(chapter, want) {
			t.Fatalf("chapter heading missing %q: %s", want, chapter)
		}
	}
	if strings.Contains(chapter, `<w:jc w:val="center"/>`) {
		t.Fatalf("chapter heading should be top/left aligned, not centered: %s", chapter)
	}

	section := paragraphContainingText(t, document, "1.1 \u7814\u7a76\u80cc\u666f")
	if strings.Contains(document, "1.1\u7814\u7a76\u80cc\u666f") {
		t.Fatalf("numbered heading should normalize spacing after the number: %s", document)
	}
	for _, want := range []string{`<w:pStyle w:val="Heading2"/>`, `<w:outlineLvl w:val="1"/>`, "w:eastAsia=\"\u5b8b\u4f53\"", "w:ascii=\"\u5b8b\u4f53\"", `<w:b/><w:bCs/>`, `<w:sz w:val="30"/>`, `w:line="360"`} {
		if !strings.Contains(section, want) {
			t.Fatalf("section heading missing %q: %s", want, section)
		}
	}

	third := paragraphContainingText(t, document, "1.1.1 \u7814\u7a76\u5bf9\u8c61")
	for _, want := range []string{`<w:pStyle w:val="Heading3"/>`, `<w:outlineLvl w:val="2"/>`, "w:eastAsia=\"\u5b8b\u4f53\"", "w:ascii=\"\u5b8b\u4f53\"", `<w:b/><w:bCs/>`, `<w:sz w:val="28"/>`, `w:line="360"`} {
		if !strings.Contains(third, want) {
			t.Fatalf("third-level heading missing %q: %s", want, third)
		}
	}

	mixed := paragraphContainingText(t, document, "5.5 \u603b\u7ed3")
	if strings.Contains(mixed, `Times New Roman`) {
		t.Fatalf("numbered Chinese heading should not fall back to Times New Roman: %s", mixed)
	}

	numericBody := paragraphContainingText(t, document, "190\u4f8b\u60a3\u8005")
	if strings.Contains(numericBody, "Heading") || strings.Contains(numericBody, "outlineLvl") {
		t.Fatalf("numeric-leading body paragraph should not be promoted to heading: %s", numericBody)
	}

	body := paragraphContainingText(t, document, "\u6b63\u6587\u5c0f\u56db\u5b8b\u4f53\u6bb5\u843d")
	for _, want := range []string{"w:eastAsia=\"\u5b8b\u4f53\"", "w:ascii=\"\u5b8b\u4f53\"", `<w:sz w:val="24"/>`, `<w:ind w:firstLineChars="200" w:firstLine="480"/>`, `w:line="360"`} {
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
	for _, want := range []string{"w:eastAsia=\"\u9ed1\u4f53\"", `<w:b/><w:bCs/>`, `<w:sz w:val="30"/>`} {
		if !strings.Contains(abstract, want) {
			t.Fatalf("abstract lead label should be black small-three bold, missing %q: %s", want, abstract)
		}
	}
}
func TestRenderBackMatterUsesTemplateStyles(t *testing.T) {
	title := backMatterTitleParagraph("\u53c2\u8003\u6587\u732e")
	for _, want := range []string{`<w:jc w:val="center"/>`, "w:eastAsia=\"\u9ed1\u4f53\"", `<w:b/><w:bCs/>`, `<w:sz w:val="30"/>`, `w:afterLines="200"`, `w:after="624"`} {
		if !strings.Contains(title, want) {
			t.Fatalf("back matter title missing %q: %s", want, title)
		}
	}

	references := renderReferences([]string{"[1] first\n[2] second"})
	if strings.Count(references, "<w:p>") != 2 {
		t.Fatalf("references should render each entry as its own paragraph: %s", references)
	}
	if strings.Contains(references, `<w:vertAlign w:val="superscript"/>`) {
		t.Fatalf("reference list marker should not be superscripted: %s", references)
	}
	for _, want := range []string{`<w:sz w:val="21"/>`, `w:line="288"`} {
		if !strings.Contains(references, want) {
			t.Fatalf("references missing %q: %s", want, references)
		}
	}
	body := renderParagraphs([]string{"Body text [1] and [3-4]."})
	if strings.Count(body, `<w:vertAlign w:val="superscript"/>`) != 2 {
		t.Fatalf("body citations should be superscripted: %s", body)
	}

	thanks := renderAcknowledgements([]string{"\u611f\u8c22\u6307\u5bfc\u8001\u5e08\u3002"})
	for _, want := range []string{"w:eastAsia=\"\u5b8b\u4f53\"", `<w:sz w:val="21"/>`, `<w:ind w:firstLineChars="200" w:firstLine="420"/>`, `w:line="360"`} {
		if !strings.Contains(thanks, want) {
			t.Fatalf("acknowledgement missing %q: %s", want, thanks)
		}
	}
}
func TestRenderCQRWSTFrontMatterTitleMergesContinuation(t *testing.T) {
	title := renderCQRWSTFrontMatterTitle(map[string]string{"Title": "Community diabetes knowledge"})
	if strings.Count(title, "<w:p>") != 1 {
		t.Fatalf("front-matter title should render as one paragraph: %s", title)
	}
	for _, want := range []string{"Community diabetes knowledge", `<w:jc w:val="center"/>`, `<w:snapToGrid w:val="0"/>`, "w:ascii=\"\u9ed1\u4f53\"", "w:eastAsia=\"\u9ed1\u4f53\"", `<w:b/><w:bCs/>`, `<w:sz w:val="32"/>`, `w:line="360"`, `w:afterLines="200"`} {
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

func TestGenerateFillsCoverFieldsInsideDrawingTextBox(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:drawing><wp:inline><a:graphic><a:graphicData><wps:wsp><wps:txbx><w:txbxContent><w:p><w:r><w:t>Title</w:t></w:r></w:p><w:p><w:r><w:t>XXXXXXXXXXXXXXXX</w:t></w:r></w:p></w:txbxContent></wps:txbx></wps:wsp></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p><w:sectPr/><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{"Title": "Community Diabetes Study"},
			Bindings:    []blockmap.Binding{{BlockID: "content_blocks", Payload: "1 Introduction"}},
		},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(document, "<w:txbxContent>") || !strings.Contains(document, "Community Diabetes Study") {
		t.Fatalf("cover field inside drawing text box was not filled: %s", document)
	}
	if strings.Contains(document, "XXXXXXXXXXXXXXXX") {
		t.Fatalf("text box cover placeholder should be removed: %s", document)
	}
}

func TestRenderPolicyDoesNotRewriteEnglishKeywords(t *testing.T) {
	rendered := renderStyledPayloadWithPolicy("Keywords: pH; RNA-seq", false)
	if !strings.Contains(rendered, "Keywords:") || !strings.Contains(rendered, " pH; RNA-seq") || strings.Contains(rendered, "pH,  RNA-seq") {
		t.Fatalf("rendered content was rewritten: %s", rendered)
	}
}

func TestGeneratePreservesComplexCrossReferenceFieldsAndUpdatesOnOpen(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	crossReference := `<w:p><w:r><w:t>See figure </w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:instrText xml:space="preserve"> PAGEREF _Ref123456 \h </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r><w:t>2</w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>` +
		`<w:p><w:bookmarkStart w:id="42" w:name="_Ref123456"/><w:r><w:t>Figure 1 Technical route</w:t></w:r><w:bookmarkEnd w:id="42"/></w:p>`
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "1 Introduction"},
			{BlockID: "content_blocks", Payload: crossReference},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	for _, want := range []string{`PAGEREF _Ref123456 \h`, `w:fldCharType="begin"`, `w:fldCharType="separate"`, `w:fldCharType="end"`, `w:bookmarkStart`, `w:bookmarkEnd`} {
		if !strings.Contains(document, want) {
			t.Fatalf("cross-reference field should preserve %q: %s", want, document)
		}
	}
	settings := readDocxEntry(t, outputPath, "word/settings.xml")
	if !strings.Contains(settings, "updateFields") {
		t.Fatalf("settings.xml should request field refresh on open: %s", settings)
	}
}

func TestGenerateNormalizesFloatingImagesAndKeepsCaptionTogether(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:drawing><wp:anchor><wp:extent cx="5000000" cy="2000000"/></wp:anchor></w:drawing></w:r></w:p>` +
			`<w:p><w:r><w:t>` + "\u56fe1-1 \u7cfb\u7edf\u67b6\u6784\u56fe" + `</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping:          &blockmap.MappingResult{},
		OutputPath:       outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Contains(document, "<wp:anchor") || !strings.Contains(document, "<wp:inline") {
		t.Fatalf("floating drawing should be normalized to inline drawing: %s", document)
	}
	imageParagraph := paragraphContainingText(t, strings.Replace(document, "<w:drawing>", "<w:t>DRAWING</w:t><w:drawing>", 1), "DRAWING")
	for _, want := range []string{`<w:keepNext/>`, `<w:jc w:val="center"/>`} {
		if !strings.Contains(imageParagraph, want) {
			t.Fatalf("image paragraph missing %q: %s", want, imageParagraph)
		}
	}
	settings := readDocxEntry(t, outputPath, "word/settings.xml")
	if !strings.Contains(settings, `<w:updateFields w:val="true"/>`) {
		t.Fatalf("settings.xml should request field refresh on open: %s", settings)
	}
}

func TestGenerateRemovesWhiteShadingWithoutDroppingTemplateShading(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:background w:color="FFFFFF"/><w:body>` +
			`<w:p><w:pPr><w:shd w:val="clear" w:color="auto" w:fill="FFFFFF"/></w:pPr><w:r><w:rPr><w:shd w:val="clear" w:fill="auto"/></w:rPr><w:t>copied white shading</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:shd w:val="clear" w:fill="D9EAD3"/></w:pPr><w:r><w:t>template green shading</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping:          &blockmap.MappingResult{},
		OutputPath:       outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	for _, forbidden := range []string{`w:fill="FFFFFF"`, `w:fill="auto"`, `<w:background w:color="FFFFFF"/>`} {
		if strings.Contains(document, forbidden) {
			t.Fatalf("document.xml still contains white/default shading %q: %s", forbidden, document)
		}
	}
	if !strings.Contains(document, `w:fill="D9EAD3"`) {
		t.Fatalf("document.xml should preserve non-white template shading: %s", document)
	}
}

func TestGeneratePreservesEnglishAbstractBodyCase(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "Abstract"},
			{BlockID: "content_blocks", Payload: "objective to explore DNA and pH effects in COVID-19 patients."},
			{BlockID: "content_blocks", Payload: "Key words: diabetes; AI model"},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(document, "objective to explore DNA and pH effects in COVID-19 patients.") {
		t.Fatalf("english abstract body content was modified: %s", document)
	}
	if !strings.Contains(document, "diabetes,  AI model") || strings.Contains(document, "Diabetes; AI Model") {
		t.Fatalf("keywords paragraph should normalize separators without changing identifier case: %s", document)
	}
}

func TestRenderParagraphsUsesCompiledTemplateStyle(t *testing.T) {
	profile := templatecompile.StyleProfile{
		Name: "body",
		Properties: templatecompile.StyleProperties{
			EastAsiaFont:       "仿宋",
			ASCIIFont:          "Arial",
			FontSizeHalfPoints: 22,
			Alignment:          "both",
			LineTwips:          400,
			LineRule:           "exact",
			FirstLineChars:     200,
		},
	}
	xml := renderParagraphs([]string{"Template-defined body."}, profile)
	for _, want := range []string{`w:eastAsia="仿宋"`, `w:ascii="Arial"`, `w:sz w:val="22"`, `w:line="400"`, `w:lineRule="exact"`, `w:jc w:val="both"`} {
		if !strings.Contains(xml, want) {
			t.Fatalf("rendered body missing %s: %s", want, xml)
		}
	}
}

func TestGenerateRemovesBlankPaginationParagraphsBeforeTables(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:pPr><w:pageBreakBefore/><w:keepNext/><w:keepLines/><w:widowControl/></w:pPr><w:r><w:t></w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:keepLines/><w:widowControl/></w:pPr><w:r><w:t>Paragraph before table.</w:t></w:r></w:p>` +
			`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>A1</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
			`</w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping:          &blockmap.MappingResult{},
		OutputPath:       outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	for _, forbidden := range []string{"pageBreakBefore", "keepLines", "widowControl"} {
		if strings.Contains(document, forbidden) {
			t.Fatalf("document.xml still contains pagination control %q: %s", forbidden, document)
		}
	}
	if strings.Contains(document, `<w:p><w:pPr><w:keepNext/>`) {
		t.Fatalf("blank keep-next paragraph before table should be removed: %s", document)
	}
	if !strings.Contains(document, "Paragraph before table.") || !strings.Contains(document, "<w:tbl>") {
		t.Fatalf("content and table should be preserved: %s", document)
	}
}

func TestGeneratePromotesFrontMatterAndBackMatterTitlesForNavigationPane(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>摘要</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>Abstract</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>参考文献</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>致谢</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>ordinary body paragraph</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping:          &blockmap.MappingResult{},
		OutputPath:       outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	for _, text := range []string{"摘要", "Abstract", "参考文献", "致谢"} {
		paragraph := paragraphContainingText(t, document, text)
		if !strings.Contains(paragraph, `<w:pStyle w:val="Heading1"/>`) || !strings.Contains(paragraph, `<w:outlineLvl w:val="0"/>`) {
			t.Fatalf("%q should be visible in navigation pane as Heading1: %s", text, paragraph)
		}
	}
	body := paragraphContainingText(t, document, "ordinary body paragraph")
	if strings.Contains(body, "Heading1") || strings.Contains(body, "outlineLvl") {
		t.Fatalf("body paragraph should not be promoted to heading: %s", body)
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

func TestGenerateStartsLongTablesOnFreshPage(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "\u88684-1 \u5355\u56e0\u7d20\u5206\u6790"},
			{BlockID: "content_blocks", Payload: testTableRows(8)},
			{BlockID: "content_blocks", Payload: "\u7eed\u88684-1 \u5355\u56e0\u7d20\u5206\u6790"},
			{BlockID: "content_blocks", Payload: testTableRows(8)},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	captionStart := strings.Index(document, "\u88684-1")
	if captionStart < 0 {
		t.Fatalf("document missing table caption: %s", document)
	}
	pageBreak := `<w:br w:type="page"/>`
	if strings.Count(document, pageBreak) != 1 || strings.Index(document, pageBreak) > captionStart {
		t.Fatalf("long table caption should start after a page break: %s", document)
	}
	continuedStart := strings.Index(document, "\u7eed\u88684-1")
	if continuedStart < 0 {
		t.Fatalf("document missing continued table caption: %s", document)
	}
}

func TestGenerateSplitsVeryLongTableWithAutomaticContinuationCaption(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "表4-1 单因素分析"},
			{BlockID: "content_blocks", Payload: testTableRows(25)},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Count(document, "<w:tbl>") != 2 {
		t.Fatalf("very long table should be split into two tables: %s", document)
	}
	if !strings.Contains(document, "续表4-1 单因素分析") {
		t.Fatalf("generated continuation caption missing: %s", document)
	}
	if strings.Count(document, "<w:tblHeader/>") != 2 {
		t.Fatalf("every table chunk should repeat the header row: %s", document)
	}
}

func TestGenerateNormalizesContinuedTableCaptionNumber(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{Bindings: []blockmap.Binding{
			{BlockID: "content_blocks", Payload: "\u88684-4 \u591a\u5206\u7c7blogistic\u56de\u5f52\u5206\u6790"},
			{BlockID: "content_blocks", Payload: testTableRows(2)},
			{BlockID: "content_blocks", Payload: "\u7eed\u88684-2 \u591a\u5206\u7c7blogistic\u56de\u5f52\u5206\u6790"},
			{BlockID: "content_blocks", Payload: testTableRows(2)},
		}},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	document := readDocxEntry(t, outputPath, "word/document.xml")
	if !strings.Contains(document, "\u7eed\u88684-4 \u591a\u5206\u7c7blogistic\u56de\u5f52\u5206\u6790") || strings.Contains(document, "\u7eed\u88684-2") {
		t.Fatalf("continued caption should follow previous table number: %s", document)
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

	tableXML := `<w:tbl><w:tblPr><w:tblW w:w="12000" w:type="dxa"/></w:tblPr><w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="1600"/><w:gridCol w:w="800"/><w:gridCol w:w="1500"/><w:gridCol w:w="1500"/><w:gridCol w:w="1400"/><w:gridCol w:w="1200"/><w:gridCol w:w="1000"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="800" w:type="dxa"/></w:tcPr><w:p><w:r><w:rPr><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr><w:t>闂傚倸鍊风粈渚€骞栭锕€纾圭紒瀣紩濞差亜围闁搞儻绲芥禍鎯归敐鍛殭闁汇劏娅ｇ槐鎺楊敊閻ｅ本鍣梺閫炲苯澧剧紓宥呮缁傚秴顭ㄩ崼顐ｆ櫓?/w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
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

func TestRenderFormulaTablePreservesOfficeMathAndAlignsNumber(t *testing.T) {
	table := `<w:tbl><w:tblPr><w:tblBorders><w:top w:val="single"/></w:tblBorders></w:tblPr><w:tr>` +
		`<w:tc><w:p><m:oMath><m:r><m:t>E=mc2</m:t></m:r></m:oMath></w:p></w:tc>` +
		`<w:tc><w:p><w:r><w:t>(2-1)</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`

	rendered := renderCleanTableFromOOXML(table)
	if !strings.Contains(rendered, "<m:oMath>") {
		t.Fatalf("formula table lost Office Math object: %s", rendered)
	}
	if !strings.Contains(rendered, `<w:jc w:val="center"/>`) || !strings.Contains(rendered, `<w:jc w:val="right"/>`) {
		t.Fatalf("formula and number cells should be centered/right-aligned: %s", rendered)
	}
	if strings.Contains(rendered, `w:val="single"`) {
		t.Fatalf("formula layout table should be borderless: %s", rendered)
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
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>` + "\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587" + `</w:t></w:r></w:p><w:tbl><w:tblPr><w:tblpPr w:tblpX="2181" w:tblpY="554"/><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tr><w:tc><w:p><w:r><w:t>Title</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>XXXXXXXXXXXXXXXX</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{"Title": "Community diabetes knowledge"},
			Bindings:    []blockmap.Binding{{BlockID: "content_blocks", Payload: "1 Introduction"}},
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
	for _, want := range []string{"Community diabetes knowledge", "w:tblpPr", "1 Introduction"} {
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
			`<w:p><w:r><w:t>` + "\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587" + `</w:t></w:r></w:p>` +
			`<w:tbl><w:tblPr><w:tblpPr w:tblpX="2181" w:tblpY="554"/><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tr><w:tc><w:p><w:r><w:t>Title</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>XXXXXXXXXXXXXXXX</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
			`<w:p><w:pPr><w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:footerReference w:type="default" r:id="rId9"/><w:pgNumType w:fmt="upperRoman" w:start="0"/></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:footerReference w:type="default" r:id="rId9"/><w:pgNumType w:fmt="upperRoman" w:start="1"/></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:footerReference w:type="default" r:id="rId11"/><w:pgNumType w:start="1"/></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rId22"/></w:sectPr></w:pPr></w:p>` +
			`</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/><Relationship Id="rId11" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer3.xml"/></Relationships>`,
		"word/footer1.xml":             `<w:ftr><w:p><w:r><w:pict><v:shape><v:textbox><w:txbxContent><w:p><w:r><w:instrText> PAGE </w:instrText></w:r></w:p></w:txbxContent></v:textbox></v:shape></w:pict></w:r></w:p></w:ftr>`,
		"word/footer3.xml":             `<w:ftr><w:p><w:r><w:t>Page </w:t></w:r><w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:t> end</w:t></w:r></w:p></w:ftr>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{"Title": "Community diabetes knowledge"},
			Bindings: []blockmap.Binding{
				{BlockID: "content_blocks", Payload: "1 Introduction"},
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
	if strings.Count(documentXML, "<w:sectPr") < 3 {
		t.Fatalf("rebuilt body should keep cover, front matter, and body sections: %s", documentXML)
	}
	if !strings.Contains(documentXML, `<w:pgNumType w:fmt="upperRoman"`) {
		t.Fatalf("rebuilt body should preserve front-matter roman page numbering: %s", documentXML)
	}
	if !strings.Contains(documentXML, `<w:pgNumType w:fmt="upperRoman" w:start="1"`) {
		t.Fatalf("front-matter page numbering should start at 1: %s", documentXML)
	}
	if !regexp.MustCompile(`(?s)<w:sectPr\b[^>]*>.*w:footerReference.*w:pgNumType w:fmt="upperRoman" w:start="1"`).MatchString(documentXML) {
		t.Fatalf("front-matter roman page numbering should keep a visible footer reference: %s", documentXML)
	}
	if regexp.MustCompile(`(?s)<w:p><w:pPr><w:sectPr\b[^>]*>.*w:fmt="upperRoman".*</w:sectPr></w:pPr></w:p>`).MatchString(documentXML) {
		t.Fatalf("front-matter section break should not create an empty paragraph: %s", documentXML)
	}
	firstTitle := strings.Index(documentXML, "Community diabetes knowledge")
	secondTitle := -1
	if firstTitle >= 0 {
		next := strings.Index(documentXML[firstTitle+1:], "Community diabetes knowledge")
		if next >= 0 {
			secondTitle = firstTitle + 1 + next
		}
	}
	secondHeading := strings.LastIndex(documentXML, "1 Introduction")
	if secondTitle < 0 || secondHeading < 0 || strings.Index(documentXML, `<w:pgNumType w:fmt="upperRoman"`) > secondHeading {
		t.Fatalf("front-matter section break should be before the real body heading: %s", documentXML)
	}
	coverSectionIndex := strings.Index(documentXML, `<w:pgSz w:w="11906" w:h="16838"`)
	if coverSectionIndex < 0 || coverSectionIndex > secondTitle {
		t.Fatalf("front-matter title should start after the cover section break: %s", documentXML)
	}
	if !strings.Contains(documentXML, `<w:pgNumType w:start="1"`) {
		t.Fatalf("rebuilt body footer should restart at page 1: %s", documentXML)
	}
	if strings.Contains(documentXML, `r:id="rId22"`) {
		t.Fatalf("rebuilt body should not use the final non-footer section as the body section: %s", documentXML)
	}
	footerXML := readDocxEntry(t, outputPath, "word/footer3.xml")
	if !strings.Contains(footerXML, "SECTIONPAGES") || strings.Contains(footerXML, "NUMPAGES") || strings.Contains(footerXML, ">12<") {
		t.Fatalf("main footer should use dynamic page fields instead of stale template text: %s", footerXML)
	}
	frontFooterXML := readDocxEntry(t, outputPath, "word/footer1.xml")
	if strings.Contains(frontFooterXML, "<w:pict") || !strings.Contains(frontFooterXML, " PAGE ") || strings.Contains(frontFooterXML, "NUMPAGES") {
		t.Fatalf("front-matter footer should use one plain Roman PAGE field without VML text boxes: %s", frontFooterXML)
	}
}
func TestGenerateReplacesDocumentTotalWithBodySectionTotal(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	templateFooter := `<w:ftr><w:p><w:r><w:t>Page </w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE \* MERGEFORMAT </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>0</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>` +
		`<w:r><w:t> of </w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> NUMPAGES \* MERGEFORMAT </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>12</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>` +
		`<w:r><w:t> pages</w:t></w:r></w:p></w:ftr>`
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>` + "\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587" + `</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:footerReference w:type="default" r:id="rId11"/><w:pgNumType w:start="1"/></w:sectPr></w:pPr></w:p>` +
			`</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId11" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer3.xml"/></Relationships>`,
		"word/footer3.xml":             templateFooter,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{"Major": "Nursing", "Date": "2026-05"},
			Bindings:    []blockmap.Binding{{BlockID: "content_blocks", Payload: "1 Introduction"}},
		},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	footerXML := readDocxEntry(t, outputPath, "word/footer3.xml")
	for _, want := range []string{"PAGE", "SECTIONPAGES"} {
		if !strings.Contains(footerXML, want) {
			t.Fatalf("footer should contain dynamic field %q: %s", want, footerXML)
		}
	}
	if strings.Contains(footerXML, "NUMPAGES") || strings.Contains(footerXML, ">12<") {
		t.Fatalf("footer should not count front matter or preserve stale results: %s", footerXML)
	}
	if strings.Contains(footerXML, ">-<") {
		t.Fatalf("footer should not be rewritten to dash page style: %s", footerXML)
	}
}
func TestNormalizeCQRWSTMainHeaderBuildsTextFromTemplateHeaderAndCoverFields(t *testing.T) {
	tmpDir := t.TempDir()
	docxPath := filepath.Join(tmpDir, "input.docx")
	writeTestDocx(t, docxPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:pgNumType w:start="1"/></w:sectPr></w:pPr></w:p>` +
			`</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId8" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/></Relationships>`,
		"word/header1.xml":             `<w:hdr><w:p><w:r><w:t>` + "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u9662X\u5800\u5800\u5c4aX\u5800\u5800\u4e13\u4e1a\u672c\u79d1\u6bd5\u4e1a\u8bbe\u8ba1" + `</w:t></w:r></w:p></w:hdr>`,
	})
	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	normalizeCQRWSTMainHeader(pkg, map[string]string{
		"\u4e13\u4e1a":             "\u62a4\u7406\u5b66",
		"\u5b8c\u6210\u65e5\u671f": "2026\u5e745\u6708",
	})

	header, ok := pkg.Get("word/header1.xml")
	if !ok {
		t.Fatal("header1.xml missing")
	}
	text := xmlText(string(header))
	want := "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u96622026\u5c4a\u62a4\u7406\u5b66\u4e13\u4e1a\u672c\u79d1\u6bd5\u4e1a\u8bbe\u8ba1"
	if text != want {
		t.Fatalf("header text = %q, want %q", text, want)
	}
	if strings.Contains(text, "\u8bba\u6587/\u8bbe\u8ba1") {
		t.Fatalf("header text should resolve the document type, got %q", text)
	}
}

func TestGenerateNormalizesCQRWSTHeaderWhenTemplateMarkerOnlyExistsInHeaderPart(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:pgNumType w:start="1"/></w:sectPr></w:pPr></w:p>` +
			`</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId8" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/></Relationships>`,
		"word/header1.xml":             `<w:hdr><w:p><w:r><w:t>` + "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u9662X\u5800\u5800\u5c4aX\u5800\u5800\u4e13\u4e1a\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587" + `</w:t></w:r></w:p></w:hdr>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{
				"\u4e13\u4e1a":             "\u62a4\u7406\u5b66",
				"\u5b8c\u6210\u65e5\u671f": "2026\u5e745\u6708",
			},
			Bindings: []blockmap.Binding{{BlockID: "content_blocks", Payload: "1 Introduction"}},
		},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	header := readDocxEntry(t, outputPath, "word/header1.xml")
	text := xmlText(header)
	want := "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u96622026\u5c4a\u62a4\u7406\u5b66\u4e13\u4e1a\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587"
	if text != want {
		t.Fatalf("header text = %q, want %q", text, want)
	}
}

func TestGenerateNormalizesCQRWSTHeaderWhenRelationshipAttributesAreReordered(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rId8"/><w:pgNumType w:start="1"/></w:sectPr></w:pPr></w:p>` +
			`</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Target="header1.xml" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Id="rId8"/></Relationships>`,
		"word/header1.xml":             `<w:hdr><w:p><w:r><w:t>` + "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u9662X\u5800\u5800\u5c4aX\u5800\u5800\u4e13\u4e1a\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587" + `</w:t></w:r></w:p></w:hdr>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{
				"\u4e13\u4e1a":             "\u62a4\u7406\u5b66",
				"\u5b8c\u6210\u65e5\u671f": "2026\u5e745\u6708",
			},
			Bindings: []blockmap.Binding{{BlockID: "content_blocks", Payload: "1 Introduction"}},
		},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	header := readDocxEntry(t, outputPath, "word/header1.xml")
	text := xmlText(header)
	want := "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u96622026\u5c4a\u62a4\u7406\u5b66\u4e13\u4e1a\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587"
	if text != want {
		t.Fatalf("header text = %q, want %q", text, want)
	}
}

func TestGenerateNormalizesAllReferencedCQRWSTHeaderParts(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="first" r:id="rIdFirst"/><w:headerReference w:type="default" r:id="rIdDefault"/><w:pgNumType w:start="1"/></w:sectPr></w:pPr></w:p>` +
			`</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdFirst" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>` +
			`<Relationship Id="rIdDefault" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header2.xml"/>` +
			`</Relationships>`,
		"word/header1.xml": `<w:hdr><w:p><w:r><w:t>` + "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u9662X\u5800\u5800\u5c4aX\u5800\u5800\u4e13\u4e1a\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587" + `</w:t></w:r></w:p></w:hdr>`,
		"word/header2.xml": `<w:hdr><w:p><w:r><w:t>` + "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u9662X\u5800\u5800\u5c4aX\u5800\u5800\u4e13\u4e1a\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587" + `</w:t></w:r></w:p></w:hdr>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			CoverFields: map[string]string{
				"\u4e13\u4e1a":             "\u62a4\u7406\u5b66",
				"\u5b8c\u6210\u65e5\u671f": "2026\u5e745\u6708",
			},
			Bindings: []blockmap.Binding{{BlockID: "content_blocks", Payload: "1 Introduction"}},
		},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	want := "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u96622026\u5c4a\u62a4\u7406\u5b66\u4e13\u4e1a\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587"
	for _, name := range []string{"word/header1.xml", "word/header2.xml"} {
		text := xmlText(readDocxEntry(t, outputPath, name))
		if text != want {
			t.Fatalf("%s text = %q, want %q", name, text, want)
		}
	}
}

func TestGeneratePreservesComplexHeaderFooterReferences(t *testing.T) {
	tmpDir := t.TempDir()
	skeletonPath := filepath.Join(tmpDir, "skeleton.docx")
	writeTestDocx(t, skeletonPath, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="first" r:id="rIdFirstHeader"/><w:headerReference w:type="even" r:id="rIdEvenHeader"/><w:headerReference w:type="default" r:id="rIdDefaultHeader"/><w:footerReference w:type="first" r:id="rIdFirstFooter"/><w:footerReference w:type="even" r:id="rIdEvenFooter"/><w:footerReference w:type="default" r:id="rIdDefaultFooter"/><w:pgNumType w:start="1"/></w:sectPr></w:pPr></w:p>` +
			`</w:body></w:document>`,
		"word/settings.xml": `<w:settings><w:evenAndOddHeaders/></w:settings>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdFirstHeader" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>` +
			`<Relationship Id="rIdEvenHeader" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header2.xml"/>` +
			`<Relationship Id="rIdDefaultHeader" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header3.xml"/>` +
			`<Relationship Id="rIdFirstFooter" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>` +
			`<Relationship Id="rIdEvenFooter" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer2.xml"/>` +
			`<Relationship Id="rIdDefaultFooter" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer3.xml"/>` +
			`</Relationships>`,
		"word/header1.xml": `<w:hdr><w:p><w:r><w:t>first header</w:t></w:r></w:p></w:hdr>`,
		"word/header2.xml": `<w:hdr><w:p><w:r><w:t>even header</w:t></w:r></w:p></w:hdr>`,
		"word/header3.xml": `<w:hdr><w:p><w:r><w:t>default header</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml": `<w:ftr><w:p><w:r><w:t>first footer</w:t></w:r></w:p></w:ftr>`,
		"word/footer2.xml": `<w:ftr><w:p><w:r><w:t>even footer</w:t></w:r></w:p></w:ftr>`,
		"word/footer3.xml": `<w:ftr><w:p><w:r><w:t>default footer</w:t></w:r></w:p></w:ftr>`,
	})

	outputPath := filepath.Join(tmpDir, "output.docx")
	err := NewTransplanter().Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: skeletonPath},
		Mapping: &blockmap.MappingResult{
			Bindings: []blockmap.Binding{{BlockID: "content_blocks", Payload: "1 Introduction"}},
		},
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	documentXML := readDocxEntry(t, outputPath, "word/document.xml")
	settingsXML := readDocxEntry(t, outputPath, "word/settings.xml")
	for _, want := range []string{
		`<w:headerReference w:type="first" r:id="rIdFirstHeader"/>`,
		`<w:headerReference w:type="even" r:id="rIdEvenHeader"/>`,
		`<w:headerReference w:type="default" r:id="rIdDefaultHeader"/>`,
		`<w:footerReference w:type="first" r:id="rIdFirstFooter"/>`,
		`<w:footerReference w:type="even" r:id="rIdEvenFooter"/>`,
		`<w:footerReference w:type="default" r:id="rIdDefaultFooter"/>`,
	} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("document XML missing %s: %s", want, documentXML)
		}
	}
	if !strings.Contains(settingsXML, `<w:evenAndOddHeaders/>`) {
		t.Fatalf("settings XML missing evenAndOddHeaders: %s", settingsXML)
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

	pkg := openTestPackage(t, path)
	content, ok := pkg.Get(name)
	if !ok {
		t.Fatalf("missing docx entry %s", name)
	}
	return string(content)
}

func testTableRows(rows int) string {
	var builder strings.Builder
	builder.WriteString(`<w:tbl>`)
	for i := 0; i < rows; i++ {
		builder.WriteString(`<w:tr><w:tc><w:p><w:r><w:t>A</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>B</w:t></w:r></w:p></w:tc></w:tr>`)
	}
	builder.WriteString(`</w:tbl>`)
	return builder.String()
}

func openTestPackage(t *testing.T, path string) *ooxmlpkg.DocxPackage {
	t.Helper()
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		t.Fatalf("open docx %s: %v", path, err)
	}
	return pkg
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
