package templatefiller

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// OOXMLFiller performs high-fidelity template filling by directly manipulating
// the OOXML (ZIP + XML) content of a .docx template.
//
// This version supports deep traversal of all elements including table cells,
// enabling correct replacement of placeholders inside cover page tables.
type OOXMLFiller struct {
	Debug bool
}

func NewOOXMLFiller() *OOXMLFiller {
	return &OOXMLFiller{Debug: false}
}

// FillTemplate fills the prepared golden template with student content.
func (f *OOXMLFiller) FillTemplate(ctx context.Context, templatePath string, sections []SectionContent, outputDir string, baseName string) (string, error) {
	return f.FillTemplateWithMedia(ctx, templatePath, nil, sections, outputDir, baseName)
}

// FillTemplateWithMedia fills the template and optionally migrates media from the student doc.
func (f *OOXMLFiller) FillTemplateWithMedia(ctx context.Context, templatePath string, studentDocBytes []byte, sections []SectionContent, outputDir string, baseName string) (string, error) {
	start := time.Now()

	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		return "", fmt.Errorf("golden template not found: %s", templatePath)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	outputPath := filepath.Join(outputDir, fmt.Sprintf("%s_corrected_%d.docx", baseName, time.Now().UnixMilli()))

	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("read template: %w", err)
	}

	var resultBytes []byte

	if studentDocBytes != nil {
		mediaHandler, mErr := NewMediaHandler(studentDocBytes)
		if mErr != nil {
			log.Printf("[OOXMLFiller] media handler init failed (continuing without media): %v", mErr)
		} else {
			rewriteSectionMediaReferences(sections, mediaHandler)
			resultBytes, err = processZipWithMedia(templateBytes, sectionContentMap(sections), mediaHandler, f)
		}
		if mErr != nil {
			resultBytes, err = f.processZip(templateBytes, sectionContentMap(sections))
		}
	} else {
		resultBytes, err = f.processZip(templateBytes, sectionContentMap(sections))
	}

	if err != nil {
		return "", fmt.Errorf("process template zip: %w", err)
	}

	if err := os.WriteFile(outputPath, resultBytes, 0644); err != nil {
		return "", fmt.Errorf("write output: %w", err)
	}

	elapsed := time.Since(start)
	log.Printf("[OOXMLFiller] completed in %v -> %s", elapsed, outputPath)
	return outputPath, nil
}

func sectionContentMap(sections []SectionContent) map[string]*SectionContent {
	sectionMap := make(map[string]*SectionContent, len(sections))
	for i := range sections {
		sectionMap[sections[i].SectionType] = &sections[i]
	}
	return sectionMap
}

func rewriteSectionMediaReferences(sections []SectionContent, mediaHandler *MediaHandler) {
	if mediaHandler == nil {
		return
	}
	for si := range sections {
		for pi := range sections[si].Paragraphs {
			cp := &sections[si].Paragraphs[pi]
			if !cp.HasComplexContent || cp.SourceXML == "" {
				continue
			}
			cp.SourceXML = mediaHandler.RewriteEmbedReferences(cp.SourceXML)
		}
	}
}

func (f *OOXMLFiller) processZip(templateBytes []byte, sections map[string]*SectionContent) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(templateBytes), int64(len(templateBytes)))
	if err != nil {
		return nil, fmt.Errorf("open template zip: %w", err)
	}

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)

	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %s: %w", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read zip entry %s: %w", file.Name, err)
		}

		if file.Name == "word/document.xml" {
			content, err = f.processDocumentXML(content, sections)
			if err != nil {
				return nil, fmt.Errorf("process document.xml: %w", err)
			}
		}

		header := &zip.FileHeader{
			Name:   file.Name,
			Method: file.Method,
		}
		header.SetModTime(time.Now())

		w, err := writer.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("create zip entry %s: %w", file.Name, err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, fmt.Errorf("write zip entry %s: %w", file.Name, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close zip writer: %w", err)
	}

	return buf.Bytes(), nil
}

// processDocumentXML replaces all {{PLACEHOLDER}} markers in the document XML.
// Uses a deep-scan approach that finds placeholders inside any element including
// table cells, handling both SingleParagraph and MultiParagraph replacements.
func (f *OOXMLFiller) processDocumentXML(xmlContent []byte, sections map[string]*SectionContent) ([]byte, error) {
	content := string(xmlContent)

	// Find ALL placeholders in the document and replace them
	// Use regex to find {{...}} patterns within <w:t> elements
	placeholderRe := regexp.MustCompile(`\{\{[A-Z_]+\}\}`)

	maxIterations := 100
	for i := 0; i < maxIterations; i++ {
		loc := placeholderRe.FindStringIndex(content)
		if loc == nil {
			break
		}

		tag := content[loc[0]:loc[1]]
		ph := FindPlaceholderInText(tag)
		if ph == nil {
			// Unknown placeholder - remove it
			content = content[:loc[0]] + content[loc[1]:]
			continue
		}

		section, ok := sections[ph.SectionType]
		if !ok || len(section.Paragraphs) == 0 {
			if f.Debug {
				log.Printf("[OOXMLFiller] %s: no content, removing paragraph", ph.Tag)
			}
			content = removePlaceholderParagraph(content, loc[0], ph.Tag)
			continue
		}

		if f.Debug {
			log.Printf("[OOXMLFiller] replacing %s with %d paragraphs", ph.Tag, len(section.Paragraphs))
		}

		if ph.Kind == SingleParagraph {
			content = replaceSingleParagraph(content, loc[0], ph.Tag, section)
		} else {
			content = replaceMultiParagraph(content, loc[0], ph.Tag, section)
		}
	}

	return []byte(content), nil
}

// replaceSingleParagraph replaces a {{PLACEHOLDER}} tag with the section's first
// paragraph text, preserving all XML formatting around the tag.
func replaceSingleParagraph(content string, tagPos int, tag string, section *SectionContent) string {
	if len(section.Paragraphs) == 0 {
		return strings.Replace(content, tag, "", 1)
	}

	cp := section.Paragraphs[0]
	if len(cp.Runs) == 0 {
		return strings.Replace(content, tag, escapeXMLText(cp.Text), 1)
	}

	paraStart := findEnclosingParagraphStart(content, tagPos)
	paraEnd := -1
	if paraStart >= 0 {
		closePos := findMatchingCloseFromStart(content, paraStart, "w:p")
		if closePos >= 0 {
			paraEnd = closePos + len("</w:p>")
		}
	}
	if paraStart < 0 || paraEnd < 0 {
		return strings.Replace(content, tag, escapeXMLText(cp.Text), 1)
	}

	originalPara := content[paraStart:paraEnd]
	pPr := extractElement([]byte(originalPara), "w:pPr")
	rPr := extractFirstRunProperties([]byte(originalPara))

	replacement := buildReplacementParagraph(pPr, rPr, cp)
	return content[:paraStart] + replacement + content[paraEnd:]
}

// replaceMultiParagraph replaces a placeholder paragraph with multiple paragraphs.
// It finds the enclosing <w:p>...</w:p> element, extracts its formatting, and
// generates N new <w:p> elements with the content.
func replaceMultiParagraph(content string, tagPos int, tag string, section *SectionContent) string {
	// Find the enclosing <w:p> element
	paraStart := findEnclosingParagraphStart(content, tagPos)
	paraEnd := -1
	if paraStart >= 0 {
		closePos := findMatchingCloseFromStart(content, paraStart, "w:p")
		if closePos >= 0 {
			paraEnd = closePos + len("</w:p>")
		}
	}

	if paraStart < 0 || paraEnd < 0 {
		// Fallback: just replace the text
		return strings.Replace(content, tag, escapeXMLText(section.Paragraphs[0].Text), 1)
	}

	originalPara := content[paraStart:paraEnd]

	// Extract pPr and rPr from the original paragraph
	pPr := extractElement([]byte(originalPara), "w:pPr")
	rPr := extractFirstRunProperties([]byte(originalPara))

	// Check if this paragraph is inside a table cell
	insideTable := isInsideTableCell(content, paraStart)

	var replacement strings.Builder
	for i, cp := range section.Paragraphs {
		if cp.HasComplexContent && cp.SourceXML != "" {
			replacement.WriteString(cp.SourceXML)
			continue
		}

		if insideTable && i > 0 {
			// Inside a table cell, we can't add multiple <w:p> easily
			// Append text with line breaks instead
			replacement.WriteString(`<w:p>`)
			if len(pPr) > 0 {
				replacement.Write(pPr)
			}
			replacement.WriteString(`<w:r>`)
			if len(rPr) > 0 {
				replacement.Write(rPr)
			}
			replacement.WriteString(`<w:t xml:space="preserve">`)
			replacement.WriteString(escapeXMLText(cp.Text))
			replacement.WriteString(`</w:t></w:r></w:p>`)
		} else {
			paraXML := buildReplacementParagraph(pPr, rPr, cp)
			replacement.WriteString(paraXML)
		}
	}

	return content[:paraStart] + replacement.String() + content[paraEnd:]
}

// buildReplacementParagraph builds a <w:p> element with the given formatting and content.
func buildReplacementParagraph(pPr []byte, rPr []byte, cp ContentParagraph) string {
	var buf strings.Builder
	buf.WriteString("<w:p>")

	// Write paragraph properties with style override for headings
	writeParagraphProperties2(&buf, pPr, cp.ParaType)

	if len(cp.Runs) > 0 {
		for _, run := range cp.Runs {
			writeRun2(&buf, rPr, run)
		}
	} else {
		writeRun2(&buf, rPr, ContentRun{Text: cp.Text})
	}

	buf.WriteString("</w:p>")
	return buf.String()
}

func writeParagraphProperties2(buf *strings.Builder, templatePPr []byte, paraType string) {
	styleOverride := paraTypeToStyleID(paraType)
	outlineOverride := paraTypeToOutlineLevel(paraType)

	if styleOverride != "" && len(templatePPr) > 0 {
		pPrStr := string(templatePPr)
		pStyleTag := fmt.Sprintf(`<w:pStyle w:val="%s"/>`, styleOverride)

		if strings.Contains(pPrStr, "<w:pStyle") {
			re := strings.Index(pPrStr, "<w:pStyle")
			end := strings.Index(pPrStr[re:], "/>")
			if end != -1 {
				pPrStr = pPrStr[:re] + pStyleTag + pPrStr[re+end+2:]
			}
		} else {
			closeAngle := strings.Index(pPrStr, ">")
			if closeAngle != -1 {
				pPrStr = pPrStr[:closeAngle+1] + pStyleTag + pPrStr[closeAngle+1:]
			}
		}
		if outlineOverride >= 0 {
			pPrStr = upsertOutlineLevel(pPrStr, outlineOverride)
		}
		buf.WriteString(pPrStr)
	} else if styleOverride != "" {
		if outlineOverride >= 0 {
			buf.WriteString(fmt.Sprintf(`<w:pPr><w:pStyle w:val="%s"/><w:outlineLvl w:val="%d"/></w:pPr>`, styleOverride, outlineOverride))
		} else {
			buf.WriteString(fmt.Sprintf(`<w:pPr><w:pStyle w:val="%s"/></w:pPr>`, styleOverride))
		}
	} else if len(templatePPr) > 0 {
		if outlineOverride >= 0 {
			buf.WriteString(upsertOutlineLevel(string(templatePPr), outlineOverride))
			return
		}
		buf.Write(templatePPr)
	}
}

func paraTypeToOutlineLevel(paraType string) int {
	switch paraType {
	case "heading_1":
		return 0
	case "heading_2":
		return 1
	case "heading_3":
		return 2
	default:
		return -1
	}
}

func upsertOutlineLevel(pPrStr string, level int) string {
	outlineTag := fmt.Sprintf(`<w:outlineLvl w:val="%d"/>`, level)
	if strings.Contains(pPrStr, "<w:outlineLvl") {
		start := strings.Index(pPrStr, "<w:outlineLvl")
		end := strings.Index(pPrStr[start:], "/>")
		if start >= 0 && end >= 0 {
			return pPrStr[:start] + outlineTag + pPrStr[start+end+2:]
		}
	}
	closeAngle := strings.Index(pPrStr, ">")
	if closeAngle == -1 {
		return pPrStr
	}
	return pPrStr[:closeAngle+1] + outlineTag + pPrStr[closeAngle+1:]
}

func writeRun2(buf *strings.Builder, templateRPr []byte, run ContentRun) {
	buf.WriteString("<w:r>")

	if len(templateRPr) > 0 {
		rPrStr := string(templateRPr)
		if run.Bold != nil && *run.Bold {
			if !strings.Contains(rPrStr, "<w:b") {
				insertPos := strings.Index(rPrStr, "</w:rPr>")
				if insertPos != -1 {
					rPrStr = rPrStr[:insertPos] + "<w:b/><w:bCs/>" + rPrStr[insertPos:]
				}
			}
		}
		if run.Italic != nil && *run.Italic {
			if !strings.Contains(rPrStr, "<w:i") {
				insertPos := strings.Index(rPrStr, "</w:rPr>")
				if insertPos != -1 {
					rPrStr = rPrStr[:insertPos] + "<w:i/><w:iCs/>" + rPrStr[insertPos:]
				}
			}
		}
		buf.WriteString(rPrStr)
	}

	buf.WriteString(`<w:t xml:space="preserve">`)
	buf.WriteString(escapeXMLText(run.Text))
	buf.WriteString("</w:t></w:r>")
}

// paraTypeToStyleID maps classification labels to OOXML style IDs.
func paraTypeToStyleID(paraType string) string {
	switch paraType {
	case "heading_1":
		return "Heading1"
	case "heading_2":
		return "Heading2"
	case "heading_3":
		return "Heading3"
	default:
		return ""
	}
}

// removePlaceholderParagraph removes the entire <w:p> containing the placeholder.
func removePlaceholderParagraph(content string, tagPos int, tag string) string {
	paraStart := findEnclosingParagraphStart(content, tagPos)
	if paraStart < 0 {
		return strings.Replace(content, tag, "", 1)
	}

	closePos := findMatchingCloseFromStart(content, paraStart, "w:p")
	if closePos < 0 {
		return strings.Replace(content, tag, "", 1)
	}

	paraEnd := closePos + len("</w:p>")
	return content[:paraStart] + content[paraEnd:]
}

// findEnclosingParagraphStart finds the start of the <w:p> element containing the given position.
func findEnclosingParagraphStart(content string, pos int) int {
	searchBack := content[:pos]
	// Find the last <w:p or <w:p> before pos
	lastP := strings.LastIndex(searchBack, "<w:p ")
	lastP2 := strings.LastIndex(searchBack, "<w:p>")
	if lastP2 > lastP {
		lastP = lastP2
	}
	return lastP
}

// findMatchingCloseFromStart finds the matching </tagName> for an opening tag at startPos.
func findMatchingCloseFromStart(content string, startPos int, tagName string) int {
	openTag := "<" + tagName
	closeTag := "</" + tagName + ">"

	depth := 0
	pos := startPos

	// Skip past the opening tag
	firstClose := strings.Index(content[pos:], ">")
	if firstClose < 0 {
		return -1
	}
	pos += firstClose + 1
	depth = 1

	for pos < len(content) && depth > 0 {
		nextOpen := strings.Index(content[pos:], openTag)
		nextClose := strings.Index(content[pos:], closeTag)

		if nextClose < 0 {
			return -1
		}

		nextOpenAbs := -1
		if nextOpen >= 0 {
			nextOpenAbs = pos + nextOpen
			// Verify it's a proper open tag (not <w:pPr> matching <w:p>)
			afterTag := nextOpenAbs + len(openTag)
			if afterTag < len(content) {
				ch := content[afterTag]
				if ch != '>' && ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' && ch != '/' {
					// Not a real open tag (e.g. <w:pPr> when looking for <w:p>)
					nextOpenAbs = -1
				}
			}
		}

		nextCloseAbs := pos + nextClose

		if nextOpenAbs >= 0 && nextOpenAbs < nextCloseAbs {
			depth++
			pos = nextOpenAbs + len(openTag) + 1
		} else {
			depth--
			if depth == 0 {
				return nextCloseAbs
			}
			pos = nextCloseAbs + len(closeTag)
		}
	}

	return -1
}

// isInsideTableCell checks if a position in the XML is inside a <w:tc> element.
func isInsideTableCell(content string, pos int) bool {
	before := content[:pos]
	lastTcOpen := strings.LastIndex(before, "<w:tc")
	lastTcClose := strings.LastIndex(before, "</w:tc>")
	return lastTcOpen > lastTcClose
}

// extractPlainText extracts all text from <w:t> elements in an XML fragment.
func extractPlainText(xmlFragment []byte) string {
	s := string(xmlFragment)
	var texts []string
	pos := 0
	for pos < len(s) {
		tStart := findRealWTStart(s[pos:])
		if tStart == -1 {
			break
		}
		tStart += pos
		tagClose := strings.Index(s[tStart:], ">")
		if tagClose == -1 {
			break
		}
		contentStart := tStart + tagClose + 1
		tEnd := strings.Index(s[contentStart:], "</w:t>")
		if tEnd == -1 {
			break
		}
		text := s[contentStart : contentStart+tEnd]
		text = strings.ReplaceAll(text, "&amp;", "&")
		text = strings.ReplaceAll(text, "&lt;", "<")
		text = strings.ReplaceAll(text, "&gt;", ">")
		text = strings.ReplaceAll(text, "&quot;", "\"")
		text = strings.ReplaceAll(text, "&apos;", "'")
		texts = append(texts, text)
		pos = contentStart + tEnd + len("</w:t>")
	}
	return strings.Join(texts, "")
}

// extractElement extracts the raw XML of the first occurrence of the named element.
func extractElement(xmlFragment []byte, elemName string) []byte {
	s := string(xmlFragment)
	openTag := "<" + elemName
	idx := strings.Index(s, openTag)
	if idx == -1 {
		return nil
	}

	afterOpen := idx + len(openTag)
	if afterOpen < len(s) {
		ch := s[afterOpen]
		if ch != '>' && ch != ' ' && ch != '/' && ch != '\t' && ch != '\n' {
			return nil
		}
	}

	closeTag := "</" + elemName + ">"
	closeIdx := findMatchingCloseFromStart(s, idx, elemName)
	if closeIdx != -1 {
		return []byte(s[idx : closeIdx+len(closeTag)])
	}

	selfClose := strings.Index(s[idx:], "/>")
	if selfClose != -1 {
		return []byte(s[idx : idx+selfClose+2])
	}
	return nil
}

// extractFirstRunProperties extracts <w:rPr> from the first <w:r> in the fragment.
func extractFirstRunProperties(xmlFragment []byte) []byte {
	s := string(xmlFragment)
	pos := 0
	for {
		idx := strings.Index(s[pos:], "<w:r")
		if idx == -1 {
			return nil
		}
		absIdx := pos + idx
		afterTag := absIdx + 4
		if afterTag >= len(s) {
			return nil
		}
		ch := s[afterTag]
		if ch == '>' || ch == ' ' || ch == '\t' || ch == '\n' {
			rEnd := strings.Index(s[absIdx:], "</w:r>")
			if rEnd == -1 {
				return nil
			}
			runContent := s[absIdx : absIdx+rEnd+len("</w:r>")]
			return extractElement([]byte(runContent), "w:rPr")
		}
		pos = afterTag
	}
}

// escapeXMLText escapes special XML characters in text content.
func escapeXMLText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
