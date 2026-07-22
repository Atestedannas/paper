package ooxmlpkg

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRejectsActiveContentAndDoctype(t *testing.T) {
	for _, test := range []struct{ name, entry, content string }{
		{"macro", "word/vbaProject.bin", "macro"},
		{"doctype", "word/styles.xml", `<!DOCTYPE x><styles/>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			pathname := filepath.Join(t.TempDir(), test.name+".docx")
			writeValidationDocx(t, pathname, test.entry, test.content)
			if err := Validate(pathname); err == nil {
				t.Fatal("unsafe DOCX was accepted")
			}
		})
	}
}

func writeValidationDocx(t *testing.T, pathname, extraName, extraContent string) {
	t.Helper()
	file, err := os.Create(pathname)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types/>`,
		"word/document.xml":   `<?xml version="1.0"?><document/>`,
		extraName:             extraContent,
	}
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
