package templatecompile

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCompilerBuildsCompiledTemplatePackage(t *testing.T) {
	templatePath := writeSimpleTemplateDocx(t)
	outputDir := t.TempDir()
	compiler := NewCompiler()

	result, err := compiler.Compile(context.Background(), templatePath, CompileOptions{
		SchoolID:     "cq-test",
		TemplateName: "official-template",
		Version:      "v1",
		OutputDir:    outputDir,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if result.Manifest.SchoolID != "cq-test" {
		t.Fatalf("unexpected school id: %s", result.Manifest.SchoolID)
	}
	if result.Manifest.TemplateName != "official-template" {
		t.Fatalf("unexpected template name: %s", result.Manifest.TemplateName)
	}
	if result.Manifest.Version != "v1" {
		t.Fatalf("unexpected version: %s", result.Manifest.Version)
	}
	if result.Manifest.DocxHash == "" {
		t.Fatal("expected docx hash")
	}
	if result.Manifest.CompilerVersion == "" {
		t.Fatal("expected compiler version")
	}
	if result.Manifest.CompiledAt.IsZero() {
		t.Fatal("expected compiled at")
	}

	if result.SkeletonPath == "" {
		t.Fatal("expected skeleton path")
	}
	if filepath.Dir(result.SkeletonPath) != outputDir {
		t.Fatalf("skeleton path should be inside output dir, got %s", result.SkeletonPath)
	}
	assertFileBytesEqual(t, templatePath, result.SkeletonPath)
	if result.SkeletonSource != templatePath {
		t.Fatalf("unexpected skeleton source: %s", result.SkeletonSource)
	}

	if len(result.BlockCatalog) == 0 {
		t.Fatal("expected non-empty block catalog")
	}
	requiredBlocks := []string{
		"cover_title",
		"abstract_cn_body",
		"heading_1",
	}
	for _, kind := range requiredBlocks {
		block, err := result.MustBlock(kind)
		if err != nil {
			t.Fatalf("MustBlock(%s) error = %v", kind, err)
		}
		if block.Kind != kind {
			t.Fatalf("MustBlock(%s) returned kind %q", kind, block.Kind)
		}
	}
	if _, err := result.MustBlock("missing_block"); err == nil {
		t.Fatal("expected MustBlock to fail for unknown block")
	}

	requiredPatchTargets := []string{
		"word/document.xml",
		"word/_rels/document.xml.rels",
		"word/settings.xml",
	}
	for _, target := range requiredPatchTargets {
		if !contains(result.PatchTargets, target) {
			t.Fatalf("patch targets missing %s: %#v", target, result.PatchTargets)
		}
	}

	if len(result.StyleProfiles) == 0 {
		t.Fatal("expected style profiles")
	}
	if len(result.MappingContract.Bindings) == 0 {
		t.Fatal("expected mapping contract bindings")
	}
	if len(result.VerificationRules) == 0 {
		t.Fatal("expected verification rules")
	}
}

func writeSimpleTemplateDocx(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "template.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	entries := map[string]string{
		"[Content_Types].xml":          `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels":                  `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/document.xml":            `<w:document><w:body><w:p><w:r><w:t>{{cover_title}}</w:t></w:r></w:p></w:body></w:document>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/settings.xml":            `<w:settings></w:settings>`,
	}

	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}

	return path
}

func assertFileBytesEqual(t *testing.T, wantPath, gotPath string) {
	t.Helper()

	want, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read want file: %v", err)
	}
	got, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("read got file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("copied skeleton differs from source")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
