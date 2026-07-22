package ooxmlpkg

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type DocxPackage struct {
	mu         sync.RWMutex
	sourcePath string
	names      map[string]struct{}
	modified   map[string][]byte
	deleted    map[string]bool
}

func Open(path string) (*DocxPackage, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	pkg := &DocxPackage{
		sourcePath: path,
		names:      make(map[string]struct{}, len(reader.File)),
		modified:   make(map[string][]byte),
		deleted:    make(map[string]bool),
	}
	for _, file := range reader.File {
		if err := validateEntryName(file.Name); err != nil {
			return nil, err
		}
		pkg.names[file.Name] = struct{}{}
	}
	return pkg, nil
}

func (p *DocxPackage) Get(name string) ([]byte, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.deleted[name] {
		return nil, false
	}
	if content, ok := p.modified[name]; ok {
		return append([]byte(nil), content...), true
	}
	if _, ok := p.names[name]; !ok {
		return nil, false
	}
	content, err := readZipEntry(p.sourcePath, name)
	if err != nil {
		return nil, false
	}
	return content, true
}

func (p *DocxPackage) Set(name string, content []byte) error {
	if err := validateEntryName(name); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.names[name] = struct{}{}
	p.modified[name] = append([]byte(nil), content...)
	delete(p.deleted, name)
	return nil
}

func (p *DocxPackage) Delete(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deleted[name] = true
	delete(p.modified, name)
}

func (p *DocxPackage) Names() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.namesLocked()
}

func (p *DocxPackage) Write(path string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	names := p.namesLocked()
	source, originals, err := openOriginalEntries(p.sourcePath)
	if err != nil {
		return err
	}
	defer func() {
		if source != nil {
			_ = source.Close()
		}
	}()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".docx-write-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}

	writer := zip.NewWriter(temp)
	for _, name := range names {
		modified, modifiedSet := p.modified[name]
		if err := writeEntry(writer, name, modified, modifiedSet, originals[name]); err != nil {
			_ = writer.Close()
			cleanup()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close DOCX zip writer: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close DOCX output: %w", err)
	}
	if source != nil {
		if err := source.Close(); err != nil {
			_ = os.Remove(tempPath)
			return fmt.Errorf("close source DOCX: %w", err)
		}
		source = nil
	}
	if err := replaceFile(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}

	p.sourcePath = path
	p.modified = make(map[string][]byte)
	p.deleted = make(map[string]bool)
	return nil
}

func (p *DocxPackage) namesLocked() []string {
	names := make([]string, 0, len(p.names))
	for name := range p.names {
		if !p.deleted[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func openOriginalEntries(path string) (*zip.ReadCloser, map[string]*zip.File, error) {
	if path == "" {
		return nil, map[string]*zip.File{}, nil
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	entries := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		entries[file.Name] = file
	}
	return reader, entries, nil
}

func writeEntry(writer *zip.Writer, name string, modified []byte, modifiedSet bool, original *zip.File) error {
	if err := validateEntryName(name); err != nil {
		return err
	}
	if modifiedSet {
		entry, err := writer.Create(name)
		if err != nil {
			return err
		}
		_, err = entry.Write(modified)
		return err
	}
	if original == nil {
		return fmt.Errorf("DOCX entry %q has no content", name)
	}
	return writer.Copy(original)
}

func validateEntryName(name string) error {
	if name == "" || strings.Contains(name, "\\") {
		return fmt.Errorf("unsafe DOCX entry path: %q", name)
	}
	clean := path.Clean(name)
	if strings.HasPrefix(name, "/") || clean == ".." || strings.HasPrefix(clean, "../") || clean != name {
		return fmt.Errorf("unsafe DOCX entry path: %q", name)
	}
	return nil
}

func readZipEntry(path, name string) ([]byte, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name == name {
			return readZipFile(file)
		}
	}
	return nil, os.ErrNotExist
}

func replaceFile(tempPath, targetPath string) error {
	backupPath := targetPath + ".write-backup"
	_ = os.Remove(backupPath)
	if _, err := os.Stat(targetPath); err == nil {
		if err := os.Rename(targetPath, backupPath); err != nil {
			return fmt.Errorf("backup existing DOCX: %w", err)
		}
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		_ = os.Rename(backupPath, targetPath)
		return fmt.Errorf("replace DOCX: %w", err)
	}
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove DOCX backup: %w", err)
	}
	return nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
