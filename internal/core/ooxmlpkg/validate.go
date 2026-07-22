package ooxmlpkg

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	maxPackageEntries    = 10000
	maxUncompressedBytes = 512 << 20
	maxXMLBytes          = 16 << 20
	maxTotalXMLBytes     = 64 << 20
	maxCompressionRatio  = 1000
)

func Validate(pathname string) error {
	reader, err := zip.OpenReader(pathname)
	if err != nil {
		return fmt.Errorf("invalid DOCX package: %w", err)
	}
	defer reader.Close()
	if len(reader.File) == 0 || len(reader.File) > maxPackageEntries {
		return fmt.Errorf("invalid DOCX entry count: %d", len(reader.File))
	}

	var total, xmlTotal uint64
	hasContentTypes, hasDocument := false, false
	for _, file := range reader.File {
		name := strings.ReplaceAll(file.Name, "\\", "/")
		clean := path.Clean(name)
		if strings.HasPrefix(name, "/") || clean == ".." || strings.HasPrefix(clean, "../") || clean != name {
			return fmt.Errorf("unsafe DOCX entry path: %q", file.Name)
		}
		lower := strings.ToLower(name)
		if lower == "word/vbaproject.bin" || strings.HasPrefix(lower, "word/activex/") {
			return fmt.Errorf("active content is not allowed: %s", name)
		}
		total += file.UncompressedSize64
		if total > maxUncompressedBytes {
			return fmt.Errorf("DOCX uncompressed size exceeds %d bytes", maxUncompressedBytes)
		}
		if file.CompressedSize64 > 0 && file.UncompressedSize64 > 10<<20 && file.UncompressedSize64/file.CompressedSize64 > maxCompressionRatio {
			return fmt.Errorf("suspicious compression ratio in %s", name)
		}
		if name == "[Content_Types].xml" {
			hasContentTypes = true
		}
		if name == "word/document.xml" {
			hasDocument = true
		}
		if !strings.HasSuffix(lower, ".xml") && !strings.HasSuffix(lower, ".rels") {
			continue
		}
		if file.UncompressedSize64 > maxXMLBytes {
			return fmt.Errorf("XML entry too large: %s", name)
		}
		xmlTotal += file.UncompressedSize64
		if xmlTotal > maxTotalXMLBytes {
			return fmt.Errorf("DOCX XML content exceeds %d bytes", maxTotalXMLBytes)
		}
		body, err := readLimitedZipFile(file, maxXMLBytes)
		if err != nil {
			return err
		}
		upper := bytes.ToUpper(body)
		if bytes.Contains(upper, []byte("<!DOCTYPE")) || bytes.Contains(upper, []byte("<!ENTITY")) {
			return fmt.Errorf("XML declarations with external entities are not allowed: %s", name)
		}
	}
	if !hasContentTypes || !hasDocument {
		return fmt.Errorf("DOCX package is missing required parts")
	}
	return nil
}

func readLimitedZipFile(file *zip.File, limit uint64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open DOCX entry %s: %w", file.Name, err)
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("read DOCX entry %s: %w", file.Name, err)
	}
	if uint64(len(body)) > limit {
		return nil, fmt.Errorf("DOCX entry exceeds limit: %s", file.Name)
	}
	return body, nil
}
