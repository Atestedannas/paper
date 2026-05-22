package fileprocessor

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"sort"
)

type docxPackage struct {
	entries map[string][]byte
}

func openDocxPackage(path string) (*docxPackage, error) {
	entries, err := ooxmlReadDocxEntries(path)
	if err != nil {
		return nil, err
	}
	return &docxPackage{entries: entries}, nil
}

func (p *docxPackage) Clone() *docxPackage {
	if p == nil {
		return nil
	}
	return &docxPackage{
		entries: cloneDocxEntries(p.entries),
	}
}

func (p *docxPackage) WriteTo(path string) error {
	if p == nil {
		return fmt.Errorf("docx package is nil")
	}
	return ooxmlWriteDocxEntries(path, p.entries)
}

func cloneDocxEntries(entries map[string][]byte) map[string][]byte {
	if entries == nil {
		return nil
	}
	cloned := make(map[string][]byte, len(entries))
	for name, content := range entries {
		cloned[name] = append([]byte(nil), content...)
	}
	return cloned
}

func ooxmlReadDocxEntries(path string) (map[string][]byte, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open zip %q: %w", path, err)
	}
	defer reader.Close()

	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %q: %w", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read zip entry %q: %w", file.Name, err)
		}
		entries[file.Name] = content
	}
	return entries, nil
}

func ooxmlWriteDocxEntries(path string, entries map[string][]byte) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create zip %q: %w", path, err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	for _, name := range sortedDocxEntryNames(entries) {
		w, err := writer.Create(name)
		if err != nil {
			writer.Close()
			return fmt.Errorf("create zip entry %q: %w", name, err)
		}
		if _, err := w.Write(entries[name]); err != nil {
			writer.Close()
			return fmt.Errorf("write zip entry %q: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close zip writer: %w", err)
	}
	return nil
}

func sortedDocxEntryNames(entries map[string][]byte) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
