package templateprofile

import (
	"fmt"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

type templatePart struct {
	Name string
	XML  string
}

type templateParts struct {
	Document              templatePart
	DocumentRelationships templatePart
	Styles                templatePart
	Numbering             templatePart
	Theme                 templatePart
	Headers               []templatePart
	Footers               []templatePart
}

func extractTemplateParts(pkg *ooxmlpkg.DocxPackage) (templateParts, error) {
	document, err := extractDocumentPart(pkg)
	if err != nil {
		return templateParts{}, err
	}
	return templateParts{
		Document:              document,
		DocumentRelationships: readOptionalPart(pkg, "word/_rels/document.xml.rels"),
		Styles:                extractStylesPart(pkg),
		Numbering:             extractNumberingPart(pkg),
		Theme:                 extractThemePart(pkg),
		Headers:               extractHeaderParts(pkg),
		Footers:               extractFooterParts(pkg),
	}, nil
}

func extractDocumentPart(pkg *ooxmlpkg.DocxPackage) (templatePart, error) {
	part := readOptionalPart(pkg, "word/document.xml")
	if part.Name == "" {
		return templatePart{}, fmt.Errorf("word/document.xml missing")
	}
	return part, nil
}

func extractStylesPart(pkg *ooxmlpkg.DocxPackage) templatePart {
	return readOptionalPart(pkg, "word/styles.xml")
}

func extractNumberingPart(pkg *ooxmlpkg.DocxPackage) templatePart {
	return readOptionalPart(pkg, "word/numbering.xml")
}

func extractThemePart(pkg *ooxmlpkg.DocxPackage) templatePart {
	return readOptionalPart(pkg, "word/theme/theme1.xml")
}

func extractHeaderParts(pkg *ooxmlpkg.DocxPackage) []templatePart {
	return extractPartsByPrefix(pkg, "word/header")
}

func extractFooterParts(pkg *ooxmlpkg.DocxPackage) []templatePart {
	return extractPartsByPrefix(pkg, "word/footer")
}

func extractPartsByPrefix(pkg *ooxmlpkg.DocxPackage, prefix string) []templatePart {
	if pkg == nil {
		return nil
	}
	var parts []templatePart
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".xml") {
			continue
		}
		if part := readOptionalPart(pkg, name); part.Name != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func readOptionalPart(pkg *ooxmlpkg.DocxPackage, name string) templatePart {
	if pkg == nil {
		return templatePart{}
	}
	body, ok := pkg.Get(name)
	if !ok {
		return templatePart{}
	}
	return templatePart{Name: name, XML: string(body)}
}
