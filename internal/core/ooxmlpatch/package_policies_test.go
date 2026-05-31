package ooxmlpatch

import (
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

func TestEnsureFixedRelationshipPartWritesPartRelsAndContentType(t *testing.T) {
	docxPath := writePatchTestDocx(t, map[string]string{
		"word/document.xml":            `<w:document/>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"[Content_Types].xml":          `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
	})
	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		t.Fatalf("open docx: %v", err)
	}

	count := EnsureFixedRelationshipPart(pkg, FixedRelationshipPartSpec{
		PartName:         "word/header1.xml",
		Content:          `<w:hdr/>`,
		RelationshipID:   "rIdHeader",
		RelationshipType: HeaderRelationshipType,
		ContentType:      HeaderContentType,
	})

	if count != 3 {
		t.Fatalf("EnsureFixedRelationshipPart() count = %d, want 3", count)
	}
	header, _ := pkg.Get("word/header1.xml")
	rels, _ := pkg.Get("word/_rels/document.xml.rels")
	types, _ := pkg.Get("[Content_Types].xml")
	if string(header) != `<w:hdr/>` {
		t.Fatalf("header part = %s", header)
	}
	if !strings.Contains(string(rels), `Id="rIdHeader"`) || !strings.Contains(string(rels), `Target="header1.xml"`) {
		t.Fatalf("relationships missing fixed header rel:\n%s", rels)
	}
	if !strings.Contains(string(types), `PartName="/word/header1.xml"`) {
		t.Fatalf("content types missing header override:\n%s", types)
	}
}

func TestBuildHeaderFooterXMLSupportsDoubleHeaderAndChineseTotalPages(t *testing.T) {
	header := BuildHeaderXML("重庆人文科技学院2026届护理学专业本科毕业论文/设计", HeaderFooterPolicySpec{
		HeaderLine:   "double",
		FontEastAsia: "宋体",
		FontSizeHalf: 18,
	})
	for _, want := range []string{
		`<w:hdr`,
		`<w:bottom w:val="double" w:sz="4" w:space="1" w:color="auto"/>`,
		`重庆人文科技学院2026届护理学专业本科毕业论文/设计`,
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("header xml missing %s:\n%s", want, header)
		}
	}

	footer := BuildPageFooterXML(PageNumberingPolicySpec{BodyWrapper: "chinese_total"})
	for _, want := range []string{
		`第 `,
		` PAGE \* MERGEFORMAT `,
		` 页 共 `,
		` NUMPAGES \* MERGEFORMAT `,
		` 页`,
	} {
		if !strings.Contains(footer, want) {
			t.Fatalf("footer xml missing %s:\n%s", want, footer)
		}
	}
}
