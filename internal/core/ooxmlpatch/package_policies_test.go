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

func TestEnsurePartRelationshipLinksExistingPart(t *testing.T) {
	docxPath := writePatchTestDocx(t, map[string]string{
		"word/document.xml":            `<w:document/>`,
		"word/settings.xml":            `<w:settings><w:updateFields w:val="true"/></w:settings>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"[Content_Types].xml":          `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
	})
	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		t.Fatalf("open docx: %v", err)
	}

	count := EnsurePartRelationship(pkg, "word/settings.xml", SettingsRelationshipType, SettingsContentType)

	if count != 2 {
		t.Fatalf("EnsurePartRelationship() count = %d, want 2", count)
	}
	settings, _ := pkg.Get("word/settings.xml")
	rels, _ := pkg.Get("word/_rels/document.xml.rels")
	types, _ := pkg.Get("[Content_Types].xml")
	if string(settings) != `<w:settings><w:updateFields w:val="true"/></w:settings>` {
		t.Fatalf("settings content changed: %s", settings)
	}
	if !strings.Contains(string(rels), SettingsRelationshipType) || !strings.Contains(string(types), SettingsContentType) {
		t.Fatalf("settings package links missing:\nrels=%s\ntypes=%s", rels, types)
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

	chapterHeader := BuildHeaderXML("chapter", HeaderFooterPolicySpec{FontEastAsia: "宋体", FontSizeHalf: 18})
	for _, want := range []string{`w:fldCharType="begin"`, ` STYLEREF Heading1 \* MERGEFORMAT `, `w:fldCharType="end"`} {
		if !strings.Contains(chapterHeader, want) {
			t.Fatalf("chapter header missing dynamic field %s:\n%s", want, chapterHeader)
		}
	}
	if strings.Contains(chapterHeader, `<w:t>chapter</w:t>`) {
		t.Fatalf("chapter header contains literal placeholder:\n%s", chapterHeader)
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

func TestApplyHeadingNumberingDefinitionsMergesHeadingStylesWithoutDroppingTemplateStyles(t *testing.T) {
	docxPath := writePatchTestDocx(t, map[string]string{
		"word/document.xml":            `<w:document/>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"[Content_Types].xml":          `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/styles.xml":              `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:style w:type="paragraph" w:styleId="Normal"><w:name w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="TemplateCustom"><w:name w:val="Template Custom"/></w:style></w:styles>`,
		"word/numbering.xml":           `<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:abstractNum w:abstractNumId="3"><w:lvl w:ilvl="0"><w:lvlText w:val="Article %1"/></w:lvl></w:abstractNum><w:num w:numId="7"><w:abstractNumId w:val="3"/></w:num></w:numbering>`,
	})
	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		t.Fatalf("open docx: %v", err)
	}

	count, err := ApplyHeadingNumberingDefinitions(pkg, []string{"1", "1.1", "1.1.1"})
	if err != nil {
		t.Fatalf("ApplyHeadingNumberingDefinitions() error = %v", err)
	}
	if count == 0 {
		t.Fatal("ApplyHeadingNumberingDefinitions() count = 0, want style/numbering writes")
	}
	styles, _ := pkg.Get("word/styles.xml")
	numbering, _ := pkg.Get("word/numbering.xml")
	for _, want := range []string{
		`styleId="TemplateCustom"`,
		`styleId="Heading1"`,
		`<w:basedOn w:val="Normal"/>`,
		`<w:outlineLvl w:val="0"/>`,
		`styleId="Heading2"`,
		`<w:outlineLvl w:val="1"/>`,
		`styleId="Heading3"`,
		`<w:outlineLvl w:val="2"/>`,
	} {
		if !strings.Contains(string(styles), want) {
			t.Fatalf("styles.xml missing %s:\n%s", want, styles)
		}
	}
	if strings.Contains(string(styles), `w:numId w:val="9000"`) {
		t.Fatalf("Heading styles should not auto-number already numbered heading text:\n%s", styles)
	}
	for _, want := range []string{`w:abstractNumId="3"`, `w:numId="7"`, `Article %1`, `<w:pStyle w:val="Heading1"/>`, `<w:pStyle w:val="Heading2"/>`, `<w:pStyle w:val="Heading3"/>`} {
		if !strings.Contains(string(numbering), want) {
			t.Fatalf("numbering.xml missing %s:\n%s", want, numbering)
		}
	}
}
