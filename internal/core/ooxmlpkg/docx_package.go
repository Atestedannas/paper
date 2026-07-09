package ooxmlpkg

import (
	"archive/zip"
	"io"
	"os"
	"sort"
)

type DocxPackage struct {
	entries map[string][]byte
}

func Open(path string) (*DocxPackage, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	pkg := &DocxPackage{
		entries: make(map[string][]byte, len(reader.File)),
	}

	for _, file := range reader.File {
		content, err := readZipFile(file)
		if err != nil {
			return nil, err
		}
		pkg.entries[file.Name] = content
	}

	return pkg, nil
}

func (p *DocxPackage) Get(name string) ([]byte, bool) {
	content, ok := p.entries[name]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), content...), true
}

func (p *DocxPackage) Set(name string, content []byte) {
	p.entries[name] = append([]byte(nil), content...)
}

func (p *DocxPackage) Delete(name string) {
	delete(p.entries, name)
}

func (p *DocxPackage) Names() []string {
	names := make([]string, 0, len(p.entries))
	for name := range p.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (p *DocxPackage) Write(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	names := make([]string, 0, len(p.entries))
	for name := range p.entries {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			writer.Close()
			return err
		}
		if _, err := entry.Write(p.entries[name]); err != nil {
			writer.Close()
			return err
		}
	}

	return writer.Close()
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}
