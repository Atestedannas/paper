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
