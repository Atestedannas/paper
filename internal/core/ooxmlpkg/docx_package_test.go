package ooxmlpkg

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestDocxPackageRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "minimal.docx")
	createMinimalDocx(t, srcPath)

	pkg, err := Open(srcPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	content, ok := pkg.Get("word/document.xml")
	if !ok {
		t.Fatal("Get(word/document.xml) ok = false")
	}
	if string(content) != "<w:document><w:body><w:p/></w:body></w:document>" {
		t.Fatalf("document.xml = %q", string(content))
	}

	outPath := filepath.Join(tmpDir, "roundtrip.docx")
	if err := pkg.Write(outPath); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("written docx does not exist: %v", err)
	}

	reopened, err := Open(outPath)
	if err != nil {
		t.Fatalf("Open(written) error = %v", err)
	}
	if _, ok := reopened.Get("word/document.xml"); !ok {
		t.Fatal("Get(word/document.xml) after round trip ok = false")
	}
}

func TestDocxPackageSetWritesNewAndReplacedEntries(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "minimal.docx")
	createMinimalDocx(t, srcPath)

	pkg, err := Open(srcPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	pkg.Set("word/document.xml", []byte("<w:document><w:body><w:p>changed</w:p></w:body></w:document>"))
	pkg.Set("custom/item.xml", []byte("<item>new</item>"))

	outPath := filepath.Join(tmpDir, "updated.docx")
	if err := pkg.Write(outPath); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	reopened, err := Open(outPath)
	if err != nil {
		t.Fatalf("Open(written) error = %v", err)
	}

	content, ok := reopened.Get("word/document.xml")
	if !ok {
		t.Fatal("Get(word/document.xml) after Set ok = false")
	}
	if string(content) != "<w:document><w:body><w:p>changed</w:p></w:body></w:document>" {
		t.Fatalf("replaced document.xml = %q", string(content))
	}

	content, ok = reopened.Get("custom/item.xml")
	if !ok {
		t.Fatal("Get(custom/item.xml) after Set ok = false")
	}
	if string(content) != "<item>new</item>" {
		t.Fatalf("new custom/item.xml = %q", string(content))
	}
}

func TestDocxPackageDeleteRemovesEntry(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "minimal.docx")
	createMinimalDocx(t, srcPath)
	pkg, err := Open(srcPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	pkg.Delete("word/document.xml")

	outPath := filepath.Join(tmpDir, "deleted.docx")
	if err := pkg.Write(outPath); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	reopened, err := Open(outPath)
	if err != nil {
		t.Fatalf("Open(written) error = %v", err)
	}
	if _, ok := reopened.Get("word/document.xml"); ok {
		t.Fatal("word/document.xml still exists after Delete")
	}
}

func createMinimalDocx(t *testing.T, path string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test docx: %v", err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	defer zw.Close()

	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/document.xml":   "<w:document><w:body><w:p/></w:body></w:document>",
	}

	for name, content := range entries {
		writer, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
}
