//go:build ignore

package templatefiller

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
)

// PrepareRealTemplate reads cqrwst_real.docx, extracts its structure (cover page
// tables, section breaks, styles, headers, footers), removes annotation shapes
// and instructional content, inserts {{PLACEHOLDER}} markers, and writes a clean
// golden template suitable for the OOXML filler.
func PrepareRealTemplate(realTemplatePath, outputPath string) error {
	realBytes, err := os.ReadFile(realTemplatePath)
	if err != nil {
		return fmt.Errorf("read real template: %w", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(realBytes), int64(len(realBytes)))
	if err != nil {
		return fmt.Errorf("open real template zip: %w", err)
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

	if err := os.WriteFile(outputPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	log.Printf("[TemplatePreparer] prepared golden template: %s", outputPath)
	return nil
}

func prepareDocumentXML(xmlContent []byte) ([]byte, error) {
	content := string(xmlContent)

	// Step 1: Extract cover page structure (before first section break)
	coverTables, coverSectPr, err := extractCoverSection(content)
	if err != nil {
		return nil, fmt.Errorf("extract cover section: %w", err)
	}

	// Step 2: Extract section configurations from the real template
	sect2 := extractSectionPr(content, 1) // inner title + abstract section
	if sect2 == "" {
		sect2 = defaultSectPr("", true)
	}
	sect3 := extractSectionPr(content, 2) // English abstract section
	if sect3 == "" {
		sect3 = defaultSectPr(sect2, true)
	}
	sect4 := extractSectionPr(content, 3) // TOC/body section
	defaultPagedSect := pickDefaultPagedSect(sect4, sect3, sect2, coverSectPr)

	// Step 3: Build the new clean document.xml
	var b strings.Builder

	// XML header and document opening
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString("\n")
	writeDocumentOpening(&b)
	b.WriteString("<w:body>")

	// === Cover Page Section ===
	writeCoverPage(&b, coverTables)
	writeSectPrInParagraph(&b, ensureNextPageSectPr(coverSectPr))
	writePageBreak(&b)

	// === Inner Title Page ===
	writePlaceholderPara(&b, "{{INNER_TITLE}}", "center", "黑体", "小二号", true)
	writePlaceholderPara(&b, "{{INNER_SUBTITLE}}", "center", "黑体", "小二号", true)
	writeSectPrInParagraph(&b, ensureNextPageSectPr(sect2))
	writePageBreak(&b)

	// === Chinese Abstract Section ===
	writeAbstractTitlePlaceholder(&b, "{{ABSTRACT_TITLE}}", "center", "黑体", "三号")
	writePlaceholderPara(&b, "{{ABSTRACT_CONTENT}}", "both", "宋体", "小四号", false)
	writePlaceholderPara(&b, "{{KEYWORDS}}", "both", "宋体", "小四号", false)
	writeSectPrInParagraph(&b, sect3)

	// === English Abstract Section ===
	writeAbstractTitlePlaceholder(&b, "{{EN_ABSTRACT_TITLE}}", "center", "Times New Roman", "三号")
	writePlaceholderPara(&b, "{{EN_ABSTRACT_CONTENT}}", "both", "Times New Roman", "小四号", false)
	writePlaceholderPara(&b, "{{EN_KEYWORDS}}", "both", "Times New Roman", "小四号", false)
	writeSectPrInParagraph(&b, defaultSectPr(defaultPagedSect, true))

	// === Table of Contents Section ===
	writeTOCPlaceholder(&b)
	writeSectPrInParagraph(&b, defaultSectPr(defaultPagedSect, true))

	// === Body Section ===
	writePlaceholderPara(&b, "{{BODY}}", "both", "宋体", "小四号", false)
	writeSectPrInParagraph(&b, defaultSectPr(defaultPagedSect, true))

	// === References Section ===
	writePlaceholderPara(&b, "{{REFERENCES_TITLE}}", "center", "黑体", "小三号", true)
	writePlaceholderPara(&b, "{{REFERENCES_CONTENT}}", "both", "宋体", "五号", false)
	writeSectPrInParagraph(&b, defaultSectPr(defaultPagedSect, true))

	// === Acknowledgements Section ===
	writePlaceholderPara(&b, "{{ACKNOWLEDGEMENTS_TITLE}}", "center", "黑体", "小三号", true)
	writePlaceholderPara(&b, "{{ACKNOWLEDGEMENTS_CONTENT}}", "both", "宋体", "小四号", false)
	writeSectPrInParagraph(&b, defaultSectPr(defaultPagedSect, true))

	// === Appendix Section ===
	writePlaceholderPara(&b, "{{APPENDIX_TITLE}}", "center", "黑体", "小三号", true)
	writePlaceholderPara(&b, "{{APPENDIX_CONTENT}}", "both", "宋体", "小四号", false)

	// Final section properties
	b.WriteString(defaultSectPr(defaultPagedSect, true))

	b.WriteString("</w:body></w:document>")

	return []byte(b.String()), nil
}

type coverTablePair struct {
	titleTable  string // Table 1: 妫版娲?+ 閸擃垱鐖ｆ０?
	fieldsTable string // Table 2: 鐎涳箓娅?娑撴挷绗?閻濐厾楠?etc.
	titlePara   string // "閺堫剛顫栧В鏇氱瑹鐠佺儤鏋?鐠佹崘顓? paragraph
}

func extractCoverSection(content string) (*coverTablePair, string, error) {
	result := &coverTablePair{}

	// Find the main title paragraph "閺堫剛顫栧В鏇氱瑹鐠佺儤鏋?鐠佹崘顓?
	titleParaStart := findParagraphContaining(content, "本科毕业论文")
	if titleParaStart >= 0 {
		titleParaEnd := findMatchingCloseFromStart(content, titleParaStart, "w:p")
		if titleParaEnd >= 0 {
			result.titlePara = content[titleParaStart : titleParaEnd+len("</w:p>")]
		}
	}

	// Find Table 1 (cover title/subtitle)
	tbl1Start := strings.Index(content, "<w:tbl>")
	if tbl1Start >= 0 {
		tbl1End := strings.Index(content[tbl1Start:], "</w:tbl>")
		if tbl1End >= 0 {
			tbl1XML := content[tbl1Start : tbl1Start+tbl1End+len("</w:tbl>")]
			result.titleTable = insertPlaceholdersInTitleTable(tbl1XML)
		}

		// Find Table 2 (cover fields)
		searchFrom := tbl1Start + tbl1End + len("</w:tbl>")
		tbl2Start := strings.Index(content[searchFrom:], "<w:tbl>")
		if tbl2Start >= 0 {
			tbl2Abs := searchFrom + tbl2Start
			tbl2End := strings.Index(content[tbl2Abs:], "</w:tbl>")
			if tbl2End >= 0 {
				tbl2XML := content[tbl2Abs : tbl2Abs+tbl2End+len("</w:tbl>")]
				result.fieldsTable = insertPlaceholdersInFieldsTable(tbl2XML)
			}
		}
	}

	// Extract the first section properties
	sectPrStart := strings.Index(content, "<w:sectPr>")
	if sectPrStart < 0 {
		sectPrStart = strings.Index(content, "<w:sectPr ")
	}
	sectPr := ""
	if sectPrStart >= 0 {
		sectPrEnd := strings.Index(content[sectPrStart:], "</w:sectPr>")
		if sectPrEnd >= 0 {
			sectPr = content[sectPrStart : sectPrStart+sectPrEnd+len("</w:sectPr>")]
		}
	}

	return result, sectPr, nil
}

func insertPlaceholdersInTitleTable(tblXML string) string {
	// The title table has rows with:
	// Row 1: "妫版娲? label | title content (XXXXXXX...)
	// Row 2 (or same row): subtitle (閳ユ柡鈧柧浜扻XXX娑撹桨绶?
	//
	// Strategy: work at the row level, find label cell and value cell,
	// replace value cell content with placeholder.

	rows := extractTableRows(tblXML)
	result := tblXML

	for i, row := range rows {
		cells := extractTableCells(row)
		if len(cells) < 2 {
			continue
		}

		labelText := extractCombinedLabelText(cells)
		labelClean := strings.ReplaceAll(labelText, " ", "")
		labelClean = strings.ReplaceAll(labelClean, "閵嗏偓", "")
		valueCell := cells[len(cells)-1]

		if strings.Contains(labelClean, "妫版娲?) {
			// This row's value cell contains the title
			newValueCell := replaceAllTextInCell(valueCell, "{{COVER_TITLE}}")
			newRow := strings.Replace(row, valueCell, newValueCell, 1)
			result = strings.Replace(result, row, newRow, 1)
		} else if i > 0 {
			// Check if this row contains the subtitle (閳ユ柡鈧柧浜扻X娑撹桨绶?pattern)
			valueText := extractAllCellText(valueCell)
			if strings.Contains(valueText, "閳ユ柡鈧?) || strings.Contains(valueText, "XXXX") {
				newLastCell := replaceAllTextInCell(valueCell, "{{COVER_SUBTITLE}}")
				newRow := strings.Replace(row, valueCell, newLastCell, 1)
				result = strings.Replace(result, row, newRow, 1)
			}
		}
	}

	// Fallback: if no row-level replacement worked, use regex on XXXXX patterns
	reXXX := regexp.MustCompile(`X{5,}`)
	if !strings.Contains(result, "{{COVER_TITLE}}") {
		matches := reXXX.FindAllStringIndex(result, -1)
		if len(matches) >= 1 {
			result = result[:matches[0][0]] + "{{COVER_TITLE}}" + result[matches[0][1]:]
		}
	}
	if !strings.Contains(result, "{{COVER_SUBTITLE}}") {
		matches := reXXX.FindAllStringIndex(result, -1)
		if len(matches) >= 1 {
			result = result[:matches[0][0]] + "{{COVER_SUBTITLE}}" + result[matches[0][1]:]
		}
	}

	return result
}

func insertPlaceholdersInFieldsTable(tblXML string) string {
	// Work at the row level: for each <w:tr>, find the label cell and value cell,
	// then replace the value cell's content with a placeholder.
	labelToPlaceholder := []struct {
		labels      []string
		placeholder string
	}{
		{[]string{"鐎涳箓娅?, "闂勩垻閮?}, "{{COVER_COLLEGE}}"},
		{[]string{"娑撴挷绗?}, "{{COVER_MAJOR}}"},
		{[]string{"閻濐厾楠?}, "{{COVER_GRADE}}"},
		{[]string{"鐎涳箑褰?}, "{{COVER_STUDENT_ID}}"},
		{[]string{"婵挸鎮?}, "{{COVER_STUDENT_NAME}}"},
		{[]string{"閹稿洤顕遍弫娆忕瑎", "鐎电厧绗€"}, "{{COVER_ADVISOR}}"},
	}

	result := tblXML
	rows := extractTableRows(result)

	for _, row := range rows {
		cells := extractTableCells(row)
		if len(cells) < 2 {
			continue
		}

		labelText := extractCombinedLabelText(cells)
		labelClean := strings.ReplaceAll(labelText, " ", "")
		labelClean = strings.ReplaceAll(labelClean, "閵嗏偓", "")
		valueCell := cells[len(cells)-1]

		for _, lp := range labelToPlaceholder {
			matched := false
			for _, label := range lp.labels {
				if strings.Contains(labelClean, label) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}

			// Real cover templates often split labels like "鐎?闂? or "妫?閻?
			// across multiple narrow cells, while the final cell holds the value.
			newValueCell := replaceAllTextInCell(valueCell, lp.placeholder)
			result = strings.Replace(result, row, strings.Replace(row, valueCell, newValueCell, 1), 1)
			break
		}
	}

	return result
}

func extractCombinedLabelText(cells []string) string {
	if len(cells) == 0 {
		return ""
	}
	if len(cells) == 1 {
		return extractAllCellText(cells[0])
	}
	var parts []string
	for i := 0; i < len(cells)-1; i++ {
		text := strings.TrimSpace(extractAllCellText(cells[i]))
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return extractAllCellText(cells[0])
	}
	return strings.Join(parts, "")
}

// replaceAllTextInCell replaces all <w:t> content in a cell with the placeholder text,
// or inserts a new <w:r> if the cell has no text.
func replaceAllTextInCell(cellXML, placeholder string) string {
	// Check if there are any <w:t> elements (not <w:tcPr>, <w:tblPr>, etc.)
	hasRealWT := strings.Contains(cellXML, "<w:t>") || strings.Contains(cellXML, "<w:t ")
	if hasRealWT {
		first := true
		result := cellXML
		search := result
		offset := 0

		for {
			tStart := findRealWTStart(search)
			if tStart < 0 {
				break
			}
			tStartAbs := offset + tStart

			tagClose := strings.Index(search[tStart:], ">")
			if tagClose < 0 {
				break
			}
			contentStart := tStart + tagClose + 1
			contentStartAbs := offset + contentStart

			tEnd := strings.Index(search[contentStart:], "</w:t>")
			if tEnd < 0 {
				break
			}
			contentEndAbs := offset + contentStart + tEnd

			if first {
				result = result[:contentStartAbs] + placeholder + result[contentEndAbs:]
				first = false
				offset = contentStartAbs + len(placeholder) + len("</w:t>")
				_ = tStartAbs
				search = result[offset:]
			} else {
				result = result[:contentStartAbs] + result[contentEndAbs:]
				offset = contentStartAbs + len("</w:t>")
				search = result[offset:]
			}
		}
		return result
	}

	// No <w:t> found - inject one before the last </w:p> in the cell
	lastPEnd := strings.LastIndex(cellXML, "</w:p>")
	if lastPEnd >= 0 {
		insertion := fmt.Sprintf(`<w:r><w:t xml:space="preserve">%s</w:t></w:r>`, placeholder)
		return cellXML[:lastPEnd] + insertion + cellXML[lastPEnd:]
	}

	return cellXML
}

func extractAllCellText(cellXML string) string {
	var texts []string
	search := cellXML
	for {
		tStart := strings.Index(search, "<w:t")
		if tStart < 0 {
			break
		}
		// Make sure it's not matching <w:tbl, <w:tc, <w:tr, etc.
		if tStart+4 < len(search) {
			next := search[tStart+4]
			if next != '>' && next != ' ' {
				search = search[tStart+4:]
				continue
			}
		}
		tagClose := strings.Index(search[tStart:], ">")
		if tagClose < 0 {
			break
		}
		contentStart := tStart + tagClose + 1
		tEnd := strings.Index(search[contentStart:], "</w:t>")
		if tEnd < 0 {
			break
		}
		text := search[contentStart : contentStart+tEnd]
		texts = append(texts, text)
		search = search[contentStart+tEnd+6:]
	}
	return strings.Join(texts, "")
}

func findParagraphContaining(content, text string) int {
	pos := strings.Index(content, text)
	if pos < 0 {
		return -1
	}
	// Walk backwards to find the opening <w:p
	for i := pos; i >= 0; i-- {
		if strings.HasPrefix(content[i:], "<w:p ") || strings.HasPrefix(content[i:], "<w:p>") {
			return i
		}
	}
	return -1
}

func extractSectionPr(content string, index int) string {
	pos := 0
	for i := 0; i <= index; i++ {
		found := strings.Index(content[pos:], "<w:sectPr")
		if found < 0 {
			return ""
		}
		if i == index {
			abs := pos + found
			endFound := strings.Index(content[abs:], "</w:sectPr>")
			if endFound >= 0 {
				return content[abs : abs+endFound+len("</w:sectPr>")]
			}
			return ""
		}
		pos += found + 10
	}
	return ""
}

func writeDocumentOpening(b *strings.Builder) {
	b.WriteString(`<w:document xmlns:wpc="http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas"`)
	b.WriteString(` xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"`)
	b.WriteString(` xmlns:o="urn:schemas-microsoft-com:office:office"`)
	b.WriteString(` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`)
	b.WriteString(` xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"`)
	b.WriteString(` xmlns:v="urn:schemas-microsoft-com:vml"`)
	b.WriteString(` xmlns:wp14="http://schemas.microsoft.com/office/word/2010/wordprocessingDrawing"`)
	b.WriteString(` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"`)
	b.WriteString(` xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`)
	b.WriteString(` xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"`)
	b.WriteString(` xmlns:w10="urn:schemas-microsoft-com:office:word"`)
	b.WriteString(` xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml"`)
	b.WriteString(` xmlns:wpg="http://schemas.microsoft.com/office/word/2010/wordprocessingGroup"`)
	b.WriteString(` xmlns:wpi="http://schemas.microsoft.com/office/word/2010/wordprocessingInk"`)
	b.WriteString(` xmlns:wne="http://schemas.microsoft.com/office/word/2006/wordml"`)
	b.WriteString(` xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"`)
	b.WriteString(` mc:Ignorable="w14 w15 wp14">`)
	b.WriteString("\n")
}

func writeCoverPage(b *strings.Builder, tables *coverTablePair) {
	// Title: "閺堫剛顫栧В鏇氱瑹鐠佺儤鏋?鐠佹崘顓?
	if tables.titlePara != "" {
		// Remove any w:pict elements (annotation shapes) from the title paragraph
		cleaned := removePictElements(tables.titlePara)
		b.WriteString(cleaned)
		b.WriteString("\n")
	} else {
		b.WriteString(`<w:p><w:pPr><w:jc w:val="center"/></w:pPr>`)
		b.WriteString(`<w:r><w:rPr><w:rFonts w:ascii="姒涙垳缍? w:eastAsia="姒涙垳缍?/>`)
		b.WriteString(`<w:b/><w:sz w:val="72"/><w:szCs w:val="72"/></w:rPr>`)
		b.WriteString(`<w:t xml:space="preserve">閺堫剛顫栧В鏇氱瑹鐠佺儤鏋?鐠佹崘顓?/w:t></w:r></w:p>`)
		b.WriteString("\n")
	}

	// Table 1: Title/Subtitle
	if tables.titleTable != "" {
		cleaned := removePictElements(tables.titleTable)
		b.WriteString(cleaned)
		b.WriteString("\n")
	}

	// Table 2: Cover Fields
	if tables.fieldsTable != "" {
		cleaned := removePictElements(tables.fieldsTable)
		b.WriteString(cleaned)
		b.WriteString("\n")
	}

	// Date placeholder
	b.WriteString(`<w:p><w:pPr><w:jc w:val="center"/></w:pPr>`)
	b.WriteString(`<w:r><w:rPr><w:rFonts w:ascii="鐎瑰缍? w:eastAsia="鐎瑰缍?/>`)
	b.WriteString(`<w:sz w:val="28"/><w:szCs w:val="28"/></w:rPr>`)
	b.WriteString(`<w:t xml:space="preserve">{{COVER_DATE}}</w:t></w:r></w:p>`)
	b.WriteString("\n")
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

func writeTOCPlaceholder(b *strings.Builder) {
	// TOC heading: "目  录"
	b.WriteString(`<w:p><w:pPr><w:jc w:val="center"/>`)
	b.WriteString(`<w:spacing w:after="320"/>`)
	b.WriteString(`</w:pPr>`)
	b.WriteString(`<w:r><w:rPr>`)
	b.WriteString(`<w:rFonts w:ascii="黑体" w:eastAsia="黑体" w:hAnsi="黑体"/>`)
	b.WriteString(`<w:b/><w:sz w:val="32"/><w:szCs w:val="32"/>`)
	b.WriteString(`</w:rPr>`)
	b.WriteString(`<w:t xml:space="preserve">目  录</w:t>`)
	b.WriteString(`</w:r></w:p>`)
	b.WriteString("\n")

	// TOC placeholder
	b.WriteString(`<w:p><w:pPr><w:spacing w:line="360" w:lineRule="auto"/></w:pPr>`)
	b.WriteString(`<w:r><w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体" w:hAnsi="宋体"/>`)
	b.WriteString(`<w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr>`)
	b.WriteString(`<w:t xml:space="preserve">{{TOC}}</w:t></w:r></w:p>`)
	b.WriteString("\n")
}
func writeTOCField(b *strings.Builder) {
	b.WriteString(`<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:after="320"/></w:pPr>`)
	b.WriteString(`<w:r><w:rPr><w:rFonts w:ascii="黑体" w:eastAsia="黑体" w:hAnsi="黑体"/>`)
	b.WriteString(`<w:b/><w:sz w:val="32"/><w:szCs w:val="32"/></w:rPr>`)
	b.WriteString(`<w:t xml:space="preserve">目  录</w:t></w:r></w:p>`)
	b.WriteString("\n")

	b.WriteString(`<w:p><w:pPr><w:spacing w:line="360" w:lineRule="auto"/></w:pPr>`)
	b.WriteString(`<w:r><w:fldChar w:fldCharType="begin"/></w:r>`)
	b.WriteString(`<w:r><w:instrText xml:space="preserve"> TOC \o "1-3" \h \z \u </w:instrText></w:r>`)
	b.WriteString(`<w:r><w:fldChar w:fldCharType="separate"/></w:r>`)
	b.WriteString(`<w:r><w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体" w:hAnsi="宋体"/>`)
	b.WriteString(`<w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr>`)
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
	if strings.TrimSpace(base) != "" {
		sect := stripPageBreakType(base)
		if withHeaderFooter {
			return sect
		}
		return removeHeaderFooterReferences(sect)
	}

	var b strings.Builder
	b.WriteString(`<w:sectPr>`)
	if withHeaderFooter {
		b.WriteString(`<w:headerReference r:id="rId3" w:type="default"/>`)
		b.WriteString(`<w:footerReference r:id="rId4" w:type="default"/>`)
	}
	b.WriteString(`<w:pgSz w:w="11906" w:h="16838"/>`)
	b.WriteString(`<w:pgMar w:top="1418" w:right="1134" w:bottom="1134" w:left="1418" w:header="851" w:footer="992" w:gutter="0"/>`)
	b.WriteString(`<w:cols w:space="720" w:num="1"/>`)
	b.WriteString(`<w:docGrid w:type="lines" w:linePitch="312" w:charSpace="0"/>`)
	b.WriteString(`</w:sectPr>`)
	return b.String()
}

func pickDefaultPagedSect(candidates ...string) string {
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "<w:headerReference") || strings.Contains(trimmed, "<w:footerReference") {
			if !strings.Contains(strings.ToLower(trimmed), "upperroman") {
				return trimmed
			}
		}
	}
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "<w:headerReference") || strings.Contains(trimmed, "<w:footerReference") {
			return trimmed
		}
	}
	return ""
}

func ensureNextPageSectPr(sectPr string) string {
	sectPr = strings.TrimSpace(sectPr)
	if sectPr == "" {
		return defaultSectPr("", true)
	}
	if strings.Contains(sectPr, `<w:type w:val="nextPage"/>`) || strings.Contains(sectPr, `<w:type w:val="nextPage" />`) {
		return sectPr
	}
	insertAt := strings.Index(sectPr, ">")
	if insertAt < 0 {
		return sectPr
	}
	return sectPr[:insertAt+1] + `<w:type w:val="nextPage"/>` + sectPr[insertAt+1:]
}

func stripPageBreakType(sectPr string) string {
	re := regexp.MustCompile(`<w:type\b[^>]*/>`)
	return re.ReplaceAllString(sectPr, "")
}

func removeHeaderFooterReferences(sectPr string) string {
	re := regexp.MustCompile(`<w:(headerReference|footerReference)\b[^>]*/>`)
	return re.ReplaceAllString(sectPr, "")
}

// removePictElements removes all <w:pict>...</w:pict> elements from XML.
// These contain VML annotation shapes (blue callout boxes with formatting instructions).
func removePictElements(xml string) string {
	result := xml
	for {
		start := strings.Index(result, "<w:pict>")
		if start < 0 {
			start = strings.Index(result, "<w:pict ")
			if start < 0 {
				break
			}
		}
		endTag := strings.Index(result[start:], "</w:pict>")
		if endTag < 0 {
			break
		}
		result = result[:start] + result[start+endTag+len("</w:pict>"):]
	}

	// Also remove any <v:shape> elements that aren't inside <w:pict>
	reShape := regexp.MustCompile(`<v:shape[^>]*>[\s\S]*?</v:shape>`)
	result = reShape.ReplaceAllString(result, "")

	return result
}

// findRealWTStart finds the next <w:t> or <w:t ...> tag, skipping <w:tc>, <w:tbl>, <w:tr>, etc.
func findRealWTStart(s string) int {
	pos := 0
	for {
		idx := strings.Index(s[pos:], "<w:t")
		if idx < 0 {
			return -1
		}
		absIdx := pos + idx
		afterTag := absIdx + 4
		if afterTag >= len(s) {
			return -1
		}
		ch := s[afterTag]
		if ch == '>' || ch == ' ' {
			return absIdx
		}
		pos = afterTag
	}
}

func fontSizeToHalfPoints(desc string) int {
	switch desc {
	case "閸掓繂褰?:
		return 84
	case "鐏忓繐鍨?:
		return 72
	case "娑撯偓閸?:
		return 52
	case "鐏忓繋绔撮崣?:
		return 48
	case "娴滃苯褰?:
		return 44
	case "鐏忓繋绨╅崣?:
		return 36
	case "娑撳褰?:
		return 32
	case "鐏忓繋绗侀崣?:
		return 30
	case "閸ユ稑褰?:
		return 28
	case "鐏忓繐娲撻崣?:
		return 24
	case "娴滄柨褰?:
		return 21
	case "鐏忓繋绨查崣?:
		return 18
	default:
		return 24
	}
}

