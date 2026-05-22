package templatefiller

import (
	"archive/zip"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// GoldenTemplateConfig holds formatting parameters for the university template.
type GoldenTemplateConfig struct {
	UniversityName string
	HeaderText     string

	// Page size in twips (1 cm = 567 twips)
	PageWidthTwips  int
	PageHeightTwips int
	TopMarginTwips  int
	BottomMarginTwips int
	LeftMarginTwips   int
	RightMarginTwips  int
	HeaderDistTwips   int
	FooterDistTwips   int
}

// DefaultCQRWSTConfig returns the formatting config for 重庆人文科技学院 本科论文.
func DefaultCQRWSTConfig() GoldenTemplateConfig {
	return GoldenTemplateConfig{
		UniversityName:    "重庆人文科技学院",
		HeaderText:        "重庆人文科技学院本科毕业论文（设计）",
		PageWidthTwips:    11906, // 21.0 cm
		PageHeightTwips:   16838, // 29.7 cm
		TopMarginTwips:    1417,  // 2.5 cm
		BottomMarginTwips: 1134,  // 2.0 cm
		LeftMarginTwips:   1417,  // 2.5 cm
		RightMarginTwips:  1134,  // 2.0 cm
		HeaderDistTwips:   851,   // 1.5 cm
		FooterDistTwips:   992,   // 1.75 cm
	}
}

// GenerateGoldenTemplate creates a .docx file with correct formatting and
// placeholder markers. The .docx is built at the raw OOXML/ZIP level so we
// have full control over every XML attribute with zero library abstraction loss.
func GenerateGoldenTemplate(outputPath string, cfg GoldenTemplateConfig) error {
	dir := filepath.Dir(outputPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	// [Content_Types].xml
	writeZipEntry(w, "[Content_Types].xml", contentTypesXML())

	// _rels/.rels
	writeZipEntry(w, "_rels/.rels", relsXML())

	// word/_rels/document.xml.rels
	writeZipEntry(w, "word/_rels/document.xml.rels", documentRelsXML())

	// word/styles.xml - complete style definitions
	writeZipEntry(w, "word/styles.xml", stylesXML())

	// word/settings.xml - document settings (incl. updateFields)
	writeZipEntry(w, "word/settings.xml", settingsXML())

	// word/header1.xml
	writeZipEntry(w, "word/header1.xml", headerXML(cfg.HeaderText))

	// word/footer1.xml
	writeZipEntry(w, "word/footer1.xml", footerXML())

	// word/document.xml - the body with placeholders
	writeZipEntry(w, "word/document.xml", documentXML(cfg))

	log.Printf("[GoldenTemplate] generated: %s", outputPath)
	return nil
}

func writeZipEntry(w *zip.Writer, name, content string) {
	f, _ := w.Create(name)
	f.Write([]byte(content))
}

func contentTypesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Default Extension="png" ContentType="image/png"/>
  <Default Extension="jpeg" ContentType="image/jpeg"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
  <Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>
  <Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/>
  <Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>
</Types>`
}

func relsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`
}

func documentRelsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>
  <Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>
</Relationships>`
}

func settingsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <w:updateFields w:val="true"/>
  <w:defaultTabStop w:val="420"/>
  <w:characterSpacingControl w:val="compressPunctuation"/>
</w:settings>`
}

// stylesXML returns a complete styles.xml with all paragraph/character styles
// matching the university formatting requirements.
func stylesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">

  <!-- Default run/paragraph properties -->
  <w:docDefaults>
    <w:rPrDefault>
      <w:rPr>
        <w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hAnsi="Times New Roman" w:cs="Times New Roman"/>
        <w:sz w:val="24"/>
        <w:szCs w:val="24"/>
        <w:lang w:val="en-US" w:eastAsia="zh-CN"/>
      </w:rPr>
    </w:rPrDefault>
    <w:pPrDefault>
      <w:pPr>
        <w:spacing w:line="360" w:lineRule="auto"/>
      </w:pPr>
    </w:pPrDefault>
  </w:docDefaults>

  <!-- Normal: 宋体小四(12pt) 首行缩进2字符 两端对齐 1.5倍行距 -->
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal">
    <w:name w:val="Normal"/>
    <w:pPr>
      <w:jc w:val="both"/>
      <w:ind w:firstLineChars="200" w:firstLine="480"/>
      <w:spacing w:line="360" w:lineRule="auto"/>
    </w:pPr>
    <w:rPr>
      <w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hAnsi="Times New Roman"/>
      <w:sz w:val="24"/>
      <w:szCs w:val="24"/>
    </w:rPr>
  </w:style>

  <!-- Heading1: 黑体三号(16pt) 加粗 左对齐 段前1行段后1行 -->
  <w:style w:type="paragraph" w:styleId="Heading1">
    <w:name w:val="heading 1"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:keepNext/>
      <w:jc w:val="left"/>
      <w:ind w:firstLineChars="0" w:firstLine="0"/>
      <w:spacing w:before="320" w:after="320" w:line="360" w:lineRule="auto"/>
      <w:outlineLvl w:val="0"/>
    </w:pPr>
    <w:rPr>
      <w:rFonts w:ascii="黑体" w:eastAsia="黑体" w:hAnsi="黑体"/>
      <w:b/>
      <w:bCs/>
      <w:sz w:val="32"/>
      <w:szCs w:val="32"/>
    </w:rPr>
  </w:style>

  <!-- Heading2: 黑体小三(15pt) 加粗 左对齐 -->
  <w:style w:type="paragraph" w:styleId="Heading2">
    <w:name w:val="heading 2"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:keepNext/>
      <w:jc w:val="left"/>
      <w:ind w:firstLineChars="0" w:firstLine="0"/>
      <w:spacing w:before="240" w:after="240" w:line="360" w:lineRule="auto"/>
      <w:outlineLvl w:val="1"/>
    </w:pPr>
    <w:rPr>
      <w:rFonts w:ascii="黑体" w:eastAsia="黑体" w:hAnsi="黑体"/>
      <w:b/>
      <w:bCs/>
      <w:sz w:val="30"/>
      <w:szCs w:val="30"/>
    </w:rPr>
  </w:style>

  <!-- Heading3: 黑体四号(14pt) 加粗 左对齐 -->
  <w:style w:type="paragraph" w:styleId="Heading3">
    <w:name w:val="heading 3"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:keepNext/>
      <w:jc w:val="left"/>
      <w:ind w:firstLineChars="0" w:firstLine="0"/>
      <w:spacing w:before="200" w:after="200" w:line="360" w:lineRule="auto"/>
      <w:outlineLvl w:val="2"/>
    </w:pPr>
    <w:rPr>
      <w:rFonts w:ascii="黑体" w:eastAsia="黑体" w:hAnsi="黑体"/>
      <w:b/>
      <w:bCs/>
      <w:sz w:val="28"/>
      <w:szCs w:val="28"/>
    </w:rPr>
  </w:style>

  <!-- AbstractTitle: 黑体小三(15pt) 加粗 居中 -->
  <w:style w:type="paragraph" w:styleId="AbstractTitle">
    <w:name w:val="AbstractTitle"/>
    <w:basedOn w:val="Heading1"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:jc w:val="center"/>
      <w:ind w:firstLineChars="0" w:firstLine="0"/>
    </w:pPr>
    <w:rPr>
      <w:rFonts w:ascii="黑体" w:eastAsia="黑体" w:hAnsi="黑体"/>
      <w:b/>
      <w:bCs/>
      <w:sz w:val="30"/>
      <w:szCs w:val="30"/>
    </w:rPr>
  </w:style>

  <!-- ReferencesTitle: 黑体小三(15pt) 加粗 居中 -->
  <w:style w:type="paragraph" w:styleId="ReferencesTitle">
    <w:name w:val="ReferencesTitle"/>
    <w:basedOn w:val="Heading1"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:jc w:val="center"/>
      <w:ind w:firstLineChars="0" w:firstLine="0"/>
    </w:pPr>
    <w:rPr>
      <w:rFonts w:ascii="黑体" w:eastAsia="黑体" w:hAnsi="黑体"/>
      <w:b/>
      <w:bCs/>
      <w:sz w:val="30"/>
      <w:szCs w:val="30"/>
    </w:rPr>
  </w:style>

  <!-- References: 宋体五号(10.5pt) 无缩进 1.5倍行距 -->
  <w:style w:type="paragraph" w:styleId="References">
    <w:name w:val="References"/>
    <w:basedOn w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:jc w:val="left"/>
      <w:ind w:firstLineChars="0" w:firstLine="0"/>
      <w:spacing w:line="360" w:lineRule="auto"/>
    </w:pPr>
    <w:rPr>
      <w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hAnsi="Times New Roman"/>
      <w:sz w:val="21"/>
      <w:szCs w:val="21"/>
    </w:rPr>
  </w:style>

  <!-- AckTitle: 致谢标题 黑体小三(15pt) 加粗 居中 -->
  <w:style w:type="paragraph" w:styleId="AckTitle">
    <w:name w:val="AckTitle"/>
    <w:basedOn w:val="Heading1"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:jc w:val="center"/>
      <w:ind w:firstLineChars="0" w:firstLine="0"/>
    </w:pPr>
  </w:style>

  <!-- Title: 封面标题 黑体二号(22pt) 加粗 居中 -->
  <w:style w:type="paragraph" w:styleId="Title">
    <w:name w:val="Title"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:jc w:val="center"/>
      <w:ind w:firstLineChars="0" w:firstLine="0"/>
      <w:spacing w:before="600" w:after="600"/>
    </w:pPr>
    <w:rPr>
      <w:rFonts w:ascii="黑体" w:eastAsia="黑体" w:hAnsi="黑体"/>
      <w:b/>
      <w:bCs/>
      <w:sz w:val="44"/>
      <w:szCs w:val="44"/>
    </w:rPr>
  </w:style>

</w:styles>`
}

func headerXML(headerText string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <w:p>
    <w:pPr>
      <w:jc w:val="center"/>
      <w:pBdr>
        <w:bottom w:val="double" w:sz="4" w:space="1" w:color="auto"/>
      </w:pBdr>
    </w:pPr>
    <w:r>
      <w:rPr>
        <w:rFonts w:ascii="宋体" w:eastAsia="宋体" w:hAnsi="宋体"/>
        <w:sz w:val="18"/>
        <w:szCs w:val="18"/>
      </w:rPr>
      <w:t xml:space="preserve">%s</w:t>
    </w:r>
  </w:p>
</w:hdr>`, headerText)
}

func footerXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <w:p>
    <w:pPr><w:jc w:val="center"/></w:pPr>
    <w:r>
      <w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体" w:hAnsi="宋体"/><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr>
      <w:t xml:space="preserve">第</w:t>
    </w:r>
    <w:fldSimple w:instr=" PAGE ">
      <w:r><w:rPr><w:sz w:val="18"/></w:rPr><w:t>1</w:t></w:r>
    </w:fldSimple>
    <w:r>
      <w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体" w:hAnsi="宋体"/><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr>
      <w:t xml:space="preserve">页 共</w:t>
    </w:r>
    <w:fldSimple w:instr=" NUMPAGES ">
      <w:r><w:rPr><w:sz w:val="18"/></w:rPr><w:t>1</w:t></w:r>
    </w:fldSimple>
    <w:r>
      <w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体" w:hAnsi="宋体"/><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr>
      <w:t xml:space="preserve">页</w:t>
    </w:r>
  </w:p>
</w:ftr>`
}

// documentXML builds the main document body with section breaks and placeholders.
func documentXML(cfg GoldenTemplateConfig) string {
	var b strings.Builder

	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:wpc="http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas"
            xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"
            xmlns:o="urn:schemas-microsoft-com:office:office"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
            xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"
            xmlns:v="urn:schemas-microsoft-com:vml"
            xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
            xmlns:w10="urn:schemas-microsoft-com:office:word"
            xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"
            xmlns:wpg="http://schemas.microsoft.com/office/word/2010/wordprocessingGroup"
            xmlns:wpi="http://schemas.microsoft.com/office/word/2010/wordprocessingInk"
            xmlns:wne="http://schemas.microsoft.com/office/word/2006/wordml"
            xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"
            mc:Ignorable="w14 wp14">
<w:body>`)

	// ── Cover page (no header/footer for first section) ──
	writePlaceholder(&b, "{{COVER_TITLE}}", "Title")
	writePlaceholder(&b, "{{COVER_SUBTITLE}}", "Normal")
	writePlaceholder(&b, "{{COVER_COLLEGE}}", "Normal")
	writePlaceholder(&b, "{{COVER_MAJOR}}", "Normal")
	writePlaceholder(&b, "{{COVER_GRADE}}", "Normal")
	writePlaceholder(&b, "{{COVER_STUDENT_NAME}}", "Normal")
	writePlaceholder(&b, "{{COVER_STUDENT_ID}}", "Normal")
	writePlaceholder(&b, "{{COVER_ADVISOR}}", "Normal")
	writePlaceholder(&b, "{{COVER_DATE}}", "Normal")

	// Section break: cover page section (no header/footer)
	writeSectionBreak(&b, cfg, false)

	// ── Abstract section ──
	writePlaceholder(&b, "{{ABSTRACT_TITLE}}", "AbstractTitle")
	writePlaceholderBody(&b, "{{ABSTRACT_CONTENT}}")
	writePlaceholder(&b, "{{KEYWORDS}}", "Normal")

	writeSectionBreak(&b, cfg, true)

	// ── English Abstract ──
	writePlaceholder(&b, "{{EN_ABSTRACT_TITLE}}", "AbstractTitle")
	writePlaceholderBody(&b, "{{EN_ABSTRACT_CONTENT}}")
	writePlaceholder(&b, "{{EN_KEYWORDS}}", "Normal")

	writeSectionBreak(&b, cfg, true)

	// ── Table of contents ──
	writePlaceholder(&b, "{{TOC}}", "Normal")

	writeSectionBreak(&b, cfg, true)

	// ── Body (all chapters) ──
	writePlaceholderBody(&b, "{{BODY}}")

	writeSectionBreak(&b, cfg, true)

	// ── References ──
	writePlaceholder(&b, "{{REFERENCES_TITLE}}", "ReferencesTitle")
	writePlaceholderRef(&b, "{{REFERENCES_CONTENT}}")

	writeSectionBreak(&b, cfg, true)

	// ── Acknowledgements ──
	writePlaceholder(&b, "{{ACKNOWLEDGEMENTS_TITLE}}", "AckTitle")
	writePlaceholderBody(&b, "{{ACKNOWLEDGEMENTS_CONTENT}}")

	writeSectionBreak(&b, cfg, true)

	// ── Appendix ──
	writePlaceholder(&b, "{{APPENDIX_TITLE}}", "AckTitle")
	writePlaceholderBody(&b, "{{APPENDIX_CONTENT}}")

	// Final section properties (document-level)
	b.WriteString(fmt.Sprintf(`<w:sectPr>
    <w:headerReference w:type="default" r:id="rId3"/>
    <w:footerReference w:type="default" r:id="rId4"/>
    <w:pgSz w:w="%d" w:h="%d"/>
    <w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="%d" w:footer="%d" w:gutter="0"/>
  </w:sectPr>`,
		cfg.PageWidthTwips, cfg.PageHeightTwips,
		cfg.TopMarginTwips, cfg.RightMarginTwips, cfg.BottomMarginTwips, cfg.LeftMarginTwips,
		cfg.HeaderDistTwips, cfg.FooterDistTwips))

	b.WriteString(`
</w:body>
</w:document>`)

	return b.String()
}

func writePlaceholder(b *strings.Builder, tag, styleID string) {
	b.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="%s"/></w:pPr><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, styleID, tag))
	b.WriteByte('\n')
}

func writePlaceholderBody(b *strings.Builder, tag string) {
	// Normal style with first-line indent
	b.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="Normal"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hAnsi="Times New Roman"/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, tag))
	b.WriteByte('\n')
}

func writePlaceholderRef(b *strings.Builder, tag string) {
	// References style: 五号 no indent
	b.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="References"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hAnsi="Times New Roman"/><w:sz w:val="21"/><w:szCs w:val="21"/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, tag))
	b.WriteByte('\n')
}

func writeSectionBreak(b *strings.Builder, cfg GoldenTemplateConfig, withHeaderFooter bool) {
	b.WriteString(`<w:p><w:pPr><w:sectPr>`)
	if withHeaderFooter {
		b.WriteString(`<w:headerReference w:type="default" r:id="rId3"/>`)
		b.WriteString(`<w:footerReference w:type="default" r:id="rId4"/>`)
	}
	b.WriteString(fmt.Sprintf(`<w:type w:val="nextPage"/>
    <w:pgSz w:w="%d" w:h="%d"/>
    <w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="%d" w:footer="%d" w:gutter="0"/>`,
		cfg.PageWidthTwips, cfg.PageHeightTwips,
		cfg.TopMarginTwips, cfg.RightMarginTwips, cfg.BottomMarginTwips, cfg.LeftMarginTwips,
		cfg.HeaderDistTwips, cfg.FooterDistTwips))
	b.WriteString(`</w:sectPr></w:pPr></w:p>`)
	b.WriteByte('\n')
}
