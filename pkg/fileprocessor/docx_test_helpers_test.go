package fileprocessor

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeTinyDocxFixture(t *testing.T, dir, name string, parts map[string]string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%q) error = %v", path, err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	for _, partName := range sortedPartNames(parts) {
		w, err := zw.Create(partName)
		if err != nil {
			t.Fatalf("Create(%q) zip entry error = %v", partName, err)
		}
		if _, err := w.Write([]byte(parts[partName])); err != nil {
			t.Fatalf("Write(%q) zip entry error = %v", partName, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return path
}

func readTinyDocxEntries(t *testing.T, path string) map[string][]byte {
	t.Helper()

	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("OpenReader(%q) error = %v", path, err)
	}
	defer r.Close()

	entries := make(map[string][]byte, len(r.File))
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("Open(%q) error = %v", f.Name, err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			rc.Close()
			t.Fatalf("ReadFrom(%q) error = %v", f.Name, err)
		}
		rc.Close()
		entries[f.Name] = append([]byte(nil), buf.Bytes()...)
	}
	return entries
}

func tinyDocxEntriesEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for name, got := range a {
		want, ok := b[name]
		if !ok {
			return false
		}
		if !bytes.Equal(got, want) {
			return false
		}
	}
	return true
}

func sortedPartNames(parts map[string]string) []string {
	names := make([]string, 0, len(parts))
	for name := range parts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
