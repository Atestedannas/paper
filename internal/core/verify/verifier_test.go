package verify

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifierReturnsFatalIssueWhenDocxCannotOpen(t *testing.T) {
	result, err := NewVerifier().Verify(context.Background(), filepath.Join(t.TempDir(), "missing.docx"))
	if err != nil {
		t.Fatalf("Verify() error = %v, want result with fatal issue", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false")
	}
	if len(result.FatalIssues) != 1 {
		t.Fatalf("FatalIssues len = %d, want 1", len(result.FatalIssues))
	}
	if result.FatalIssues[0].Kind != "docx_open" {
		t.Fatalf("fatal kind = %q, want docx_open", result.FatalIssues[0].Kind)
	}
}

func TestVerifierReturnsFatalIssueWhenDocumentXMLMissing(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{"word/header1.xml": `<w:hdr/>`})

	result, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false")
	}
	if len(result.FatalIssues) != 1 {
		t.Fatalf("FatalIssues len = %d, want 1", len(result.FatalIssues))
	}
	if result.FatalIssues[0].Target != "word/document.xml" {
		t.Fatalf("fatal target = %q, want word/document.xml", result.FatalIssues[0].Target)
	}
}

func TestVerifierReturnsRepairableIssueForPlaceholders(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>{{title}}</w:body></w:document>`,
	})

	result, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false")
	}
	if len(result.RepairableIssues) != 1 {
		t.Fatalf("RepairableIssues len = %d, want 1", len(result.RepairableIssues))
	}
	if result.RepairableIssues[0].Kind != "placeholder" {
		t.Fatalf("repairable kind = %q, want placeholder", result.RepairableIssues[0].Kind)
	}
}

func TestVerifierAllowsOnlyWarningsToPass(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{"word/document.xml": `  <w/>  `})

	result, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !result.Passed {
		t.Fatal("Verify() Passed = false, want true for warnings only")
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings len = %d, want 1", len(result.Warnings))
	}
}

func TestVerifierPassesCleanDocument(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p></w:body></w:document>`,
	})

	result, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !result.Passed {
		t.Fatalf("Verify() Passed = false, result = %#v", result)
	}
	if len(result.FatalIssues) != 0 || len(result.RepairableIssues) != 0 || len(result.Warnings) != 0 {
		t.Fatalf("Verify() result has unexpected issues: %#v", result)
	}
}

func TestVerifierReturnsContextCanceled(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{"word/document.xml": `<w:document/>`})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewVerifier().Verify(ctx, docxPath)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify() error = %v, want context.Canceled", err)
	}
}

func writeVerifyTestDocx(t *testing.T, entries map[string]string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.docx")
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

	return path
}
