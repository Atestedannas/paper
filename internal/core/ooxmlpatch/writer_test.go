package ooxmlpatch

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

func TestWriterAppliesPatchToDefaultDocumentTarget(t *testing.T) {
	docxPath := writePatchTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>{{title}}</w:body></w:document>`,
	})

	err := NewWriter([]Patch{{Find: "{{title}}", Replace: "Final Title"}}).Apply(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	document := readPatchTestEntry(t, docxPath, "word/document.xml")
	if !strings.Contains(document, "Final Title") {
		t.Fatalf("document.xml = %s, want replacement", document)
	}
}

func TestWriterRejectsNonWhitelistedTarget(t *testing.T) {
	docxPath := writePatchTestDocx(t, map[string]string{
		"word/document.xml": `<w:document/>`,
		"word/header1.xml":  `<w:hdr>{{title}}</w:hdr>`,
	})

	err := NewWriter([]Patch{{Target: "word/header1.xml", Find: "{{title}}", Replace: "Title"}}).Apply(context.Background(), docxPath)
	if err == nil {
		t.Fatal("Apply() error = nil, want whitelist error")
	}
	if !strings.Contains(err.Error(), "not allowed") || !strings.Contains(err.Error(), "word/header1.xml") {
		t.Fatalf("Apply() error = %v, want clear target whitelist error", err)
	}
}

func TestWriterReturnsErrorWhenTargetMissing(t *testing.T) {
	docxPath := writePatchTestDocx(t, map[string]string{
		"word/header1.xml": `<w:hdr/>`,
	})

	err := NewWriter([]Patch{{Find: "{{title}}", Replace: "Title"}}).Apply(context.Background(), docxPath)
	if err == nil {
		t.Fatal("Apply() error = nil, want missing target error")
	}
	if !strings.Contains(err.Error(), "word/document.xml") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Apply() error = %v, want clear missing target error", err)
	}
}

func TestWriterEscapesXMLReplacement(t *testing.T) {
	docxPath := writePatchTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>{{payload}}</w:body></w:document>`,
	})

	err := NewWriter([]Patch{{Find: "{{payload}}", Replace: `A&B <C> "D" 'E'`}}).Apply(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	document := readPatchTestEntry(t, docxPath, "word/document.xml")
	want := `A&amp;B &lt;C&gt; &#34;D&#34; &#39;E&#39;`
	if !strings.Contains(document, want) {
		t.Fatalf("document.xml = %s, want escaped replacement %s", document, want)
	}
}

func TestWriterReturnsContextCanceled(t *testing.T) {
	docxPath := writePatchTestDocx(t, map[string]string{
		"word/document.xml": `<w:document>{{title}}</w:document>`,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := NewWriter([]Patch{{Find: "{{title}}", Replace: "Title"}}).Apply(ctx, docxPath)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Apply() error = %v, want context.Canceled", err)
	}
}

func writePatchTestDocx(t *testing.T, entries map[string]string) string {
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

func readPatchTestEntry(t *testing.T, path, name string) string {
	t.Helper()

	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		t.Fatalf("open docx: %v", err)
	}
	content, ok := pkg.Get(name)
	if !ok {
		t.Fatalf("missing entry %s", name)
	}
	return string(content)
}
