package templatefiller

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PrepareRealTemplate converts the annotated school template into a prepared
// shell that preserves the template package structure while replacing the
// fillable content regions with placeholders.
func PrepareRealTemplate(realTemplatePath, outputPath string) error {
	if strings.TrimSpace(realTemplatePath) == "" {
		return fmt.Errorf("empty template path")
	}
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("empty output path")
	}

	siblingPrepared := strings.TrimSuffix(realTemplatePath, filepath.Ext(realTemplatePath)) + "_prepared.docx"
	if !strings.EqualFold(filepath.Clean(siblingPrepared), filepath.Clean(outputPath)) {
		if ok, err := hasUsablePreparedTemplate(siblingPrepared); err == nil && ok {
			return copyPreparedTemplateFile(siblingPrepared, outputPath)
		}
	}

	src, err := os.ReadFile(realTemplatePath)
	if err != nil {
		return fmt.Errorf("read real template: %w", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		return fmt.Errorf("open template zip: %w", err)
	}

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)

	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("read zip entry %s: %w", file.Name, err)
		}

		if file.Name == "word/document.xml" {
			content, err = prepareDocumentXML(content)
			if err != nil {
				return fmt.Errorf("prepare document.xml: %w", err)
			}
		}

		w, err := writer.Create(file.Name)
		if err != nil {
			return fmt.Errorf("create zip entry %s: %w", file.Name, err)
		}
		if _, err := w.Write(content); err != nil {
			return fmt.Errorf("write zip entry %s: %w", file.Name, err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("close zip writer: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	if err := os.WriteFile(outputPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write prepared template: %w", err)
	}
	return nil
}

func copyPreparedTemplateFile(srcPath, dstPath string) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return os.WriteFile(dstPath, src, 0644)
}

func hasUsablePreparedTemplate(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false, err
	}
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return false, err
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return false, err
		}
		xmlText := string(content)
		requiredMarkers := []string{"{{COVER_TITLE}}", "{{COVER_COLLEGE}}", "{{COVER_DATE}}", "{{BODY}}"}
		for _, marker := range requiredMarkers {
			if !strings.Contains(xmlText, marker) {
				return false, nil
			}
		}
		if strings.Count(xmlText, "<w:tbl") < 2 {
			return false, nil
		}
		return true, nil
	}
	return false, fmt.Errorf("missing word/document.xml in %s", path)
}

type coverTemplateFragments struct {
	titlePara   string
	titleTable  string
	fieldsTable string
	datePara    string
	tocTitle    string
	tocField    string
	coverSectPr string
}

func prepareDocumentXML(xmlContent []byte) ([]byte, error) {
	content := string(xmlContent)
	cover := extractCoverTemplateFragments(content)

	innerSect := firstNonEmpty(extractSectionPr(content, 1), cover.coverSectPr)
	abstractSect := firstNonEmpty(extractSectionPr(content, 2), innerSect, cover.coverSectPr)
	englishSect := firstNonEmpty(extractSectionPr(content, 3), abstractSect, innerSect, cover.coverSectPr)
	pagedSect := firstNonEmpty(extractSectionPr(content, 4), englishSect, abstractSect, innerSect, cover.coverSectPr)

	var b strings.Builder
	writeDocumentOpening(&b)
	b.WriteString("<w:body>")

	if cover.titlePara != "" {
		b.WriteString(removePictElements(cover.titlePara))
		b.WriteString("\n")
	}
	if cover.titleTable != "" {
		b.WriteString(removePictElements(insertPlaceholdersInTitleTable(cover.titleTable)))
		b.WriteString("\n")
	}
	if cover.fieldsTable != "" {
		b.WriteString(removePictElements(insertPlaceholdersInFieldsTable(cover.fieldsTable)))
		b.WriteString("\n")
	}
	if cover.datePara != "" {
		b.WriteString(removePictElements(replaceTextInXMLFragment(cover.datePara, "{{COVER_DATE}}")))
		b.WriteString("\n")
	} else {
		writePlaceholderPara(&b, "{{COVER_DATE}}", "center", "宋体", "三号", true)
	}

	writeSectPrInParagraph(&b, ensureNextPageSectPr(defaultSectPr(cover.coverSectPr, true)))
	writePageBreak(&b)

	writePlaceholderPara(&b, "{{INNER_TITLE}}", "center", "黑体", "小二号", true)
	writePlaceholderPara(&b, "{{INNER_SUBTITLE}}", "center", "黑体", "小三号", true)
	writeSectPrInParagraph(&b, ensureNextPageSectPr(defaultSectPr(innerSect, true)))
	writePageBreak(&b)

	writeAbstractTitlePlaceholder(&b, "{{ABSTRACT_TITLE}}", "center", "黑体", "三号")
	writePlaceholderPara(&b, "{{ABSTRACT_CONTENT}}", "both", "宋体", "小四号", false)
	writePlaceholderPara(&b, "{{KEYWORDS}}", "both", "宋体", "小四号", false)
	writeSectPrInParagraph(&b, defaultSectPr(abstractSect, true))

	writeAbstractTitlePlaceholder(&b, "{{EN_ABSTRACT_TITLE}}", "center", "Times New Roman", "三号")
	writePlaceholderPara(&b, "{{EN_ABSTRACT_CONTENT}}", "both", "Times New Roman", "小四号", false)
	writePlaceholderPara(&b, "{{EN_KEYWORDS}}", "both", "Times New Roman", "小四号", false)
	writeSectPrInParagraph(&b, defaultSectPr(englishSect, true))

	if cover.tocTitle != "" {
		b.WriteString(removePictElements(cover.tocTitle))
		b.WriteString("\n")
	} else {
		writeTOCTitle(&b)
	}
	if cover.tocField != "" {
		b.WriteString(removePictElements(cover.tocField))
		b.WriteString("\n")
	} else {
		writeTOCField(&b)
	}
	writeSectPrInParagraph(&b, defaultSectPr(pagedSect, true))

	writePlaceholderPara(&b, "{{BODY}}", "both", "宋体", "小四号", false)
	writeSectPrInParagraph(&b, defaultSectPr(pagedSect, true))

	writePlaceholderPara(&b, "{{REFERENCES_TITLE}}", "center", "黑体", "小三号", true)
	writePlaceholderPara(&b, "{{REFERENCES_CONTENT}}", "both", "宋体", "五号", false)
	writeSectPrInParagraph(&b, defaultSectPr(pagedSect, true))

	writePlaceholderPara(&b, "{{ACKNOWLEDGEMENTS_TITLE}}", "center", "黑体", "小三号", true)
	writePlaceholderPara(&b, "{{ACKNOWLEDGEMENTS_CONTENT}}", "both", "宋体", "小四号", false)
	writeSectPrInParagraph(&b, defaultSectPr(pagedSect, true))

	writePlaceholderPara(&b, "{{APPENDIX_TITLE}}", "center", "黑体", "小三号", true)
	writePlaceholderPara(&b, "{{APPENDIX_CONTENT}}", "both", "宋体", "小四号", false)

	b.WriteString(defaultSectPr(pagedSect, true))
	b.WriteString("</w:body></w:document>")
	return []byte(removeInstructionalColors(b.String())), nil
}

func extractCoverTemplateFragments(content string) coverTemplateFragments {
	coverContent := content
	if idx := strings.Index(content, "<w:sectPr"); idx >= 0 {
		coverContent = content[:idx]
	}

	var fragments coverTemplateFragments
	paragraphs := extractParagraphXMLFragments(content)
	tables := extractAllTables(coverContent)

	for _, para := range paragraphs {
		text := normalizeTemplateText(extractAllText(para))
		switch {
		case fragments.titlePara == "" && strings.Contains(text, "本科毕业论文"):
			fragments.titlePara = para
		case fragments.datePara == "" && looksLikeDateParagraph(text):
			fragments.datePara = para
		case fragments.tocTitle == "" && text == "目录":
			fragments.tocTitle = para
		case fragments.tocField == "" && strings.Contains(para, `TOC \o "1-3" \h \z \u`):
			fragments.tocField = para
		}
	}

	if len(tables) > 0 {
		fragments.titleTable = tables[0]
	}
	if len(tables) > 1 {
		fragments.fieldsTable = tables[1]
	}
	fragments.coverSectPr = extractSectionPr(content, 0)
	return fragments
}

func extractParagraphXMLFragments(content string) []string {
	re := regexp.MustCompile(`(?s)<w:p\b[^>]*>.*?</w:p>`)
	return re.FindAllString(content, -1)
}

func normalizeTemplateText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "：", "")
	text = strings.ReplaceAll(text, ":", "")
	text = strings.ReplaceAll(text, " ", "")
	text = strings.ReplaceAll(text, "　", "")
	return text
}

func looksLikeDateParagraph(text string) bool {
	dateRe := regexp.MustCompile(`20\d{2}年\d{1,2}月`)
	compact := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(text), " ", ""), "　", "")
	return dateRe.MatchString(compact)
}

func extractSectionPr(content string, index int) string {
	pos := 0
	for i := 0; i <= index; i++ {
		start := strings.Index(content[pos:], "<w:sectPr")
		if start < 0 {
			return ""
		}
		absStart := pos + start
		end := strings.Index(content[absStart:], "</w:sectPr>")
		if end < 0 {
			return ""
		}
		if i == index {
			return content[absStart : absStart+end+len("</w:sectPr>")]
		}
		pos = absStart + end + len("</w:sectPr>")
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func writeDocumentOpening(b *strings.Builder) {
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString("\n")
	b.WriteString(`<w:document xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
}

func writePlaceholderPara(b *strings.Builder, tag, jc, fontName, sizeDesc string, bold bool) {
	szVal := fontSizeToHalfPoints(sizeDesc)
	b.WriteString(`<w:p><w:pPr>`)
	b.WriteString(fmt.Sprintf(`<w:jc w:val="%s"/>`, jc))
	if jc == "both" {
		b.WriteString(`<w:ind w:firstLineChars="200" w:firstLine="480"/>`)
	}
	b.WriteString(`<w:spacing w:line="360" w:lineRule="auto"/>`)
	b.WriteString(`</w:pPr>`)
	b.WriteString(`<w:r><w:rPr>`)
	b.WriteString(fmt.Sprintf(`<w:rFonts w:ascii="%s" w:eastAsia="%s" w:hAnsi="%s"/>`, fontName, fontName, fontName))
	if bold {
		b.WriteString(`<w:b/><w:bCs/>`)
	}
	b.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, szVal, szVal))
	b.WriteString(`</w:rPr>`)
	b.WriteString(fmt.Sprintf(`<w:t xml:space="preserve">%s</w:t>`, tag))
	b.WriteString(`</w:r></w:p>`)
	b.WriteString("\n")
}

func writeAbstractTitlePlaceholder(b *strings.Builder, tag, jc, fontName, sizeDesc string) {
	szVal := fontSizeToHalfPoints(sizeDesc)
	b.WriteString(`<w:p><w:pPr>`)
	b.WriteString(fmt.Sprintf(`<w:jc w:val="%s"/>`, jc))
	b.WriteString(`<w:spacing w:before="120" w:after="120" w:line="240" w:lineRule="auto"/>`)
	b.WriteString(`</w:pPr>`)
	b.WriteString(`<w:r><w:rPr>`)
	b.WriteString(fmt.Sprintf(`<w:rFonts w:ascii="%s" w:eastAsia="%s" w:hAnsi="%s"/>`, fontName, fontName, fontName))
	b.WriteString(`<w:b/><w:bCs/>`)
	b.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, szVal, szVal))
	b.WriteString(`</w:rPr>`)
	b.WriteString(fmt.Sprintf(`<w:t xml:space="preserve">%s</w:t>`, tag))
	b.WriteString(`</w:r></w:p>`)
	b.WriteString("\n")
}

func writeTOCTitle(b *strings.Builder) {
	b.WriteString(`<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:after="320"/></w:pPr>`)
	b.WriteString(`<w:r><w:rPr><w:rFonts w:ascii="黑体" w:eastAsia="黑体" w:hAnsi="黑体"/><w:b/><w:sz w:val="32"/><w:szCs w:val="32"/></w:rPr>`)
	b.WriteString(`<w:t xml:space="preserve">目  录</w:t></w:r></w:p>`)
	b.WriteString("\n")
}

func writeTOCPlaceholder(b *strings.Builder) {
	writeTOCTitle(b)
	b.WriteString(`<w:p><w:pPr><w:spacing w:line="360" w:lineRule="auto"/></w:pPr>`)
	b.WriteString(`<w:r><w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体" w:hAnsi="宋体"/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr>`)
	b.WriteString(`<w:t xml:space="preserve">{{TOC}}</w:t></w:r></w:p>`)
	b.WriteString("\n")
}

func writeTOCField(b *strings.Builder) {
	b.WriteString(`<w:p><w:pPr><w:spacing w:line="360" w:lineRule="auto"/></w:pPr>`)
	b.WriteString(`<w:r><w:fldChar w:fldCharType="begin"/></w:r>`)
	b.WriteString(`<w:r><w:instrText xml:space="preserve"> TOC \o "1-3" \h \z \u </w:instrText></w:r>`)
	b.WriteString(`<w:r><w:fldChar w:fldCharType="separate"/></w:r>`)
	b.WriteString(`<w:r><w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体" w:hAnsi="宋体"/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr>`)
	b.WriteString(`<w:t xml:space="preserve">目录将在打开文档后自动更新</w:t></w:r>`)
	b.WriteString(`<w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`)
	b.WriteString("\n")
}

func writeSectPrInParagraph(b *strings.Builder, sectPr string) {
	b.WriteString(`<w:p><w:pPr>`)
	b.WriteString(sectPr)
	b.WriteString(`</w:pPr></w:p>`)
	b.WriteString("\n")
}

func writePageBreak(b *strings.Builder) {
	b.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
	b.WriteString("\n")
}

func defaultSectPr(base string, withHeaderFooter bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		var b strings.Builder
		b.WriteString(`<w:sectPr>`)
		if withHeaderFooter {
			b.WriteString(`<w:headerReference r:id="rId3" w:type="default"/>`)
			b.WriteString(`<w:footerReference r:id="rId4" w:type="default"/>`)
		}
		b.WriteString(`<w:pgSz w:w="11906" w:h="16838"/>`)
		b.WriteString(`<w:pgMar w:top="1418" w:right="1134" w:bottom="1134" w:left="1418" w:header="851" w:footer="992" w:gutter="0"/>`)
		b.WriteString(`<w:cols w:space="720" w:num="1"/>`)
		b.WriteString(`</w:sectPr>`)
		return b.String()
	}

	base = stripPageBreakType(base)
	if withHeaderFooter {
		return base
	}
	return removeHeaderFooterReferences(base)
}

func ensureNextPageSectPr(sectPr string) string {
	if strings.TrimSpace(sectPr) == "" {
		return defaultSectPr("", true)
	}
	if strings.Contains(sectPr, `w:type w:val="nextPage"`) {
		return sectPr
	}
	if idx := strings.Index(sectPr, ">"); idx >= 0 {
		return sectPr[:idx+1] + `<w:type w:val="nextPage"/>` + sectPr[idx+1:]
	}
	return sectPr
}

func stripPageBreakType(sectPr string) string {
	re := regexp.MustCompile(`<w:type\b[^>]*/>`)
	return re.ReplaceAllString(sectPr, "")
}

func removeHeaderFooterReferences(sectPr string) string {
	re := regexp.MustCompile(`<w:(headerReference|footerReference)\b[^>]*/>`)
	return re.ReplaceAllString(sectPr, "")
}

func removePictElements(xmlText string) string {
	result := xmlText
	for {
		start := strings.Index(result, "<w:pict")
		if start < 0 {
			break
		}
		end := strings.Index(result[start:], "</w:pict>")
		if end < 0 {
			break
		}
		result = result[:start] + result[start+end+len("</w:pict>"):]
	}
	shapeRe := regexp.MustCompile(`(?s)<v:shape\b[^>]*>.*?</v:shape>`)
	return shapeRe.ReplaceAllString(result, "")
}

func removeInstructionalColors(xmlText string) string {
	colorRe := regexp.MustCompile(`(?i)<w:color\b[^>]*w:val="FF0000"[^>]*/>`)
	return colorRe.ReplaceAllString(xmlText, "")
}

func insertPlaceholdersInTitleTable(tblXML string) string {
	rows := extractTableRows(tblXML)
	if len(rows) == 0 {
		return tblXML
	}

	result := tblXML
	if len(rows) >= 1 {
		cells := extractTableCells(rows[0])
		if len(cells) >= 2 {
			valueCell := cells[len(cells)-1]
			newValueCell := replaceTextInXMLFragment(valueCell, "{{COVER_TITLE}}")
			result = strings.Replace(result, valueCell, newValueCell, 1)
		}
	}
	if len(rows) >= 2 {
		cells := extractTableCells(rows[1])
		if len(cells) >= 1 {
			valueCell := cells[len(cells)-1]
			newValueCell := replaceTextInXMLFragment(valueCell, "{{COVER_SUBTITLE}}")
			result = strings.Replace(result, valueCell, newValueCell, 1)
		}
	}
	return result
}

func insertPlaceholdersInFieldsTable(tblXML string) string {
	labelToPlaceholder := []struct {
		label       string
		placeholder string
	}{
		{"学院", "{{COVER_COLLEGE}}"},
		{"专业", "{{COVER_MAJOR}}"},
		{"班级", "{{COVER_GRADE}}"},
		{"学号", "{{COVER_STUDENT_ID}}"},
		{"姓名", "{{COVER_STUDENT_NAME}}"},
		{"指导教师", "{{COVER_ADVISOR}}"},
	}

	result := tblXML
	rows := extractTableRows(tblXML)
	for _, row := range rows {
		cells := extractTableCells(row)
		if len(cells) < 2 {
			continue
		}
		labelText := normalizeTemplateText(extractCombinedLabelText(cells))
		valueCell := cells[len(cells)-1]
		for _, item := range labelToPlaceholder {
			if !strings.Contains(labelText, item.label) {
				continue
			}
			newValueCell := replaceTextInXMLFragment(valueCell, item.placeholder)
			result = strings.Replace(result, valueCell, newValueCell, 1)
			break
		}
	}
	return result
}

func replaceTextInXMLFragment(fragment, replacement string) string {
	hasRealText := strings.Contains(fragment, "<w:t>") || strings.Contains(fragment, "<w:t ")
	if !hasRealText {
		lastPEnd := strings.LastIndex(fragment, "</w:p>")
		if lastPEnd >= 0 {
			insertion := fmt.Sprintf(`<w:r><w:t xml:space="preserve">%s</w:t></w:r>`, replacement)
			return fragment[:lastPEnd] + insertion + fragment[lastPEnd:]
		}
		return fragment
	}

	result := fragment
	first := true
	searchOffset := 0
	for {
		tStart := findRealWTStart(result[searchOffset:])
		if tStart < 0 {
			break
		}
		tStart += searchOffset
		tagClose := strings.Index(result[tStart:], ">")
		if tagClose < 0 {
			break
		}
		contentStart := tStart + tagClose + 1
		tEnd := strings.Index(result[contentStart:], "</w:t>")
		if tEnd < 0 {
			break
		}
		contentEnd := contentStart + tEnd
		if first {
			result = result[:contentStart] + replacement + result[contentEnd:]
			searchOffset = contentStart + len(replacement) + len("</w:t>")
			first = false
			continue
		}
		result = result[:contentStart] + result[contentEnd:]
		searchOffset = contentStart + len("</w:t>")
	}
	return result
}

func fontSizeToHalfPoints(desc string) int {
	switch desc {
	case "二号":
		return 44
	case "小二号":
		return 36
	case "三号":
		return 32
	case "小三号":
		return 30
	case "四号":
		return 28
	case "小四号":
		return 24
	case "五号":
		return 21
	default:
		return 24
	}
}

func extractCombinedLabelText(cells []string) string {
	if len(cells) == 0 {
		return ""
	}
	if len(cells) == 1 {
		return extractAllText(cells[0])
	}
	var parts []string
	for i := 0; i < len(cells)-1; i++ {
		text := strings.TrimSpace(extractAllText(cells[i]))
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return extractAllText(cells[0])
	}
	return strings.Join(parts, "")
}

func findRealWTStart(s string) int {
	pos := 0
	for {
		idx := strings.Index(s[pos:], "<w:t")
		if idx < 0 {
			return -1
		}
		abs := pos + idx
		after := abs + 4
		if after >= len(s) {
			return -1
		}
		ch := s[after]
		if ch == '>' || ch == ' ' {
			return abs
		}
		pos = after
	}
}
