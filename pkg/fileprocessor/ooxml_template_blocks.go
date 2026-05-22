package fileprocessor

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

type templateBlock struct {
	partName  string
	blockType string
	text      string
	index     int
}

type docxParagraph struct {
	xml     string
	index   int
	inTable bool
	tableIndex int
}

func findTemplateBlocks(pkg *docxPackage) (map[string]templateBlock, error) {
	if pkg == nil {
		return nil, fmt.Errorf("docx package is nil")
	}

	documentXML, ok := pkg.entries["word/document.xml"]
	if !ok {
		return nil, fmt.Errorf("missing word/document.xml")
	}

	bodyXML := extractDocxBodyXML(string(documentXML))
	tables := extractDocxElements(bodyXML, "w:tbl")
	paragraphs := extractDocxParagraphs(bodyXML)

	blocks := make(map[string]templateBlock, 7)

	if len(tables) < 1 {
		return nil, fmt.Errorf("missing template block: cover_title_table")
	}
	blocks["cover_title_table"] = templateBlock{
		partName:  "word/document.xml",
		blockType: "table",
		index:     0,
		text:      extractDocxText(tables[0]),
	}

	if len(tables) < 2 {
		return nil, fmt.Errorf("missing template block: cover_info_table")
	}
	blocks["cover_info_table"] = templateBlock{
		partName:  "word/document.xml",
		blockType: "table",
		index:     1,
		text:      extractDocxText(tables[1]),
	}

	searchStart := 0
	abstractCnBlock, ok := findTemplateParagraphBlockAfter(paragraphs, searchStart, func(p docxParagraph) bool {
		return isKnownChineseAbstractTitle(p.xml) || isInlineChineseAbstractAnchorParagraph(p.xml)
	})
	if !ok {
		return nil, fmt.Errorf("missing template block: abstract_cn_title")
	}
	blocks["abstract_cn_title"] = abstractCnBlock

	searchStart = abstractCnBlock.index + 1
	abstractEnBlock, ok := findTemplateParagraphBlockAfter(paragraphs, searchStart, func(p docxParagraph) bool {
		return isExactEnglishAbstractTitleParagraph(p.xml) || isInlineEnglishAbstractAnchorParagraph(p.xml)
	})
	if !ok {
		return nil, fmt.Errorf("missing template block: abstract_en_title")
	}
	blocks["abstract_en_title"] = abstractEnBlock

	searchStart = abstractEnBlock.index + 1
	tocBlock, ok := findTemplateParagraphBlockAfter(paragraphs, searchStart, func(p docxParagraph) bool {
		return isExactTOCTitleParagraph(p.xml) || isInstructionTOCAnchorParagraph(p.xml)
	})
	if !ok {
		return nil, fmt.Errorf("missing template block: toc_title")
	}
	blocks["toc_title"] = tocBlock

	searchStart = tocBlock.index + 1
	referencesBlock, ok := findTemplateParagraphBlockAfter(paragraphs, searchStart, func(p docxParagraph) bool {
		return isExactTemplateTitleParagraph(p.xml, "\u53c2\u8003\u6587\u732e") || isInstructionReferencesAnchorParagraph(p.xml)
	})
	if !ok {
		return nil, fmt.Errorf("missing template block: references_title")
	}
	blocks["references_title"] = referencesBlock

	searchStart = referencesBlock.index + 1
	ackBlock, ok := findTemplateParagraphBlockAfter(paragraphs, searchStart, func(p docxParagraph) bool {
		return isExactTemplateTitleParagraph(p.xml, "\u81f4\u8c22") || isInstructionAcknowledgementAnchorParagraph(p.xml)
	})
	if !ok {
		return nil, fmt.Errorf("missing template block: ack_title")
	}
	blocks["ack_title"] = ackBlock

	return blocks, nil
}

func findTemplateParagraphBlock(paragraphs []docxParagraph, match func(docxParagraph) bool) (templateBlock, bool) {
	return findTemplateParagraphBlockAfter(paragraphs, 0, match)
}

func findTemplateParagraphBlockAfter(paragraphs []docxParagraph, startIndex int, match func(docxParagraph) bool) (templateBlock, bool) {
	if startIndex < 0 {
		startIndex = 0
	}
	for _, paragraph := range paragraphs {
		if paragraph.index < startIndex {
			continue
		}
		if match(paragraph) {
			return templateBlock{
				partName:  "word/document.xml",
				blockType: "paragraph",
				index:     paragraph.index,
				text:      extractDocxText(paragraph.xml),
			}, true
		}
	}
	return templateBlock{}, false
}

func isExactTemplateTitleParagraph(xmlText, title string) bool {
	normalized := strings.TrimSpace(normalizeChineseText(normalizeVisibleText(extractDocxText(xmlText))))
	return normalized == title || strings.EqualFold(normalized, title)
}

func isExactEnglishAbstractTitleParagraph(xmlText string) bool {
	normalized := strings.ToLower(strings.TrimSpace(normalizeVisibleText(extractDocxText(xmlText))))
	return normalized == "abstract"
}

func isExactTOCTitleParagraph(xmlText string) bool {
	normalized := strings.TrimSpace(normalizeChineseText(normalizeVisibleText(extractDocxText(xmlText))))
	return normalized == "\u76ee\u5f55"
}

func isKnownChineseAbstractTitle(xmlText string) bool {
	normalized := strings.TrimSpace(normalizeChineseText(normalizeVisibleText(extractDocxText(xmlText))))
	switch normalized {
	case "\u6458\u8981", "\u6458 \u8981", "\u4e2d\u6587\u6458\u8981", "\u8bba\u6587\u6458\u8981":
		return true
	default:
		return false
	}
}

func isInlineChineseAbstractAnchorParagraph(xmlText string) bool {
	return hasNormalizedPrefix(xmlText, "\u6458\u8981\uff1a", "\u6458\u8981:", "\u4e2d\u6587\u6458\u8981\uff1a", "\u4e2d\u6587\u6458\u8981:", "\u8bba\u6587\u6458\u8981\uff1a", "\u8bba\u6587\u6458\u8981:")
}

func isInlineEnglishAbstractAnchorParagraph(xmlText string) bool {
	normalized := strings.ToLower(strings.TrimSpace(normalizeVisibleText(extractDocxText(xmlText))))
	return strings.HasPrefix(normalized, "abstract:") || strings.HasPrefix(normalized, "abstract\uff1a")
}

func isInstructionTOCAnchorParagraph(xmlText string) bool {
	return hasNormalizedPrefix(xmlText, "\u76ee\u5f55\u5185\u5bb9\uff1a", "\u76ee\u5f55\u5185\u5bb9:")
}

func isInstructionReferencesAnchorParagraph(xmlText string) bool {
	return hasNormalizedPrefix(xmlText, "\u53c2\u8003\u6587\u732e\u6807\u9898\uff1a", "\u53c2\u8003\u6587\u732e\u6807\u9898:")
}

func isInstructionAcknowledgementAnchorParagraph(xmlText string) bool {
	return hasNormalizedPrefix(xmlText, "\u81f4\u8c22\u6807\u9898\uff1a", "\u81f4\u8c22\u6807\u9898:")
}

func hasNormalizedPrefix(xmlText string, prefixes ...string) bool {
	visible := strings.TrimSpace(normalizeVisibleText(extractDocxText(xmlText)))
	for _, prefix := range prefixes {
		if strings.HasPrefix(visible, prefix) {
			return true
		}
	}
	return false
}

func extractDocxElements(xmlText, elementName string) []string {
	pattern := regexp.MustCompile(`(?s)<` + elementName + `\b[^>]*>.*?</` + elementName + `>`)
	return pattern.FindAllString(xmlText, -1)
}

func extractDocxBodyXML(xmlText string) string {
	pattern := regexp.MustCompile(`(?s)<w:body\b[^>]*>(.*)</w:body>`)
	match := pattern.FindStringSubmatch(xmlText)
	if len(match) < 2 {
		return xmlText
	}
	return match[1]
}

func extractDocxParagraphs(xmlText string) []docxParagraph {
	rawParagraphs := extractDocxElements(xmlText, "w:p")
	paragraphs := make([]docxParagraph, 0, len(rawParagraphs))
	searchStart := 0
	for _, paragraphXML := range rawParagraphs {
		start := strings.Index(xmlText[searchStart:], paragraphXML)
		if start == -1 {
			continue
		}
		start += searchStart
		tableIndex := enclosingDocxTableIndex(xmlText, start)
		paragraphs = append(paragraphs, docxParagraph{
			xml:        paragraphXML,
			index:      len(paragraphs),
			inTable:    tableIndex >= 0,
			tableIndex: tableIndex,
		})
		searchStart = start + len(paragraphXML)
	}
	return paragraphs
}

func enclosingDocxTableIndex(xmlText string, limit int) int {
	if limit <= 0 {
		return -1
	}
	tokenPattern := regexp.MustCompile(`(?s)</w:tbl>|<w:tbl\b[^>]*>`)
	matches := tokenPattern.FindAllStringIndex(xmlText[:limit], -1)
	if len(matches) == 0 {
		return -1
	}

	stack := make([]int, 0, 4)
	tableCount := 0
	for _, match := range matches {
		token := xmlText[match[0]:match[1]]
		if strings.HasPrefix(token, "<w:tbl") {
			stack = append(stack, tableCount)
			tableCount++
			continue
		}
		if len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) == 0 {
		return -1
	}
	return stack[len(stack)-1]
}

func extractDocxText(xmlText string) string {
	re := regexp.MustCompile(`(?s)<w:t\b[^>]*>(.*?)</w:t>`)
	matches := re.FindAllStringSubmatch(xmlText, -1)
	if len(matches) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, match := range matches {
		if len(match) > 1 {
			builder.WriteString(html.UnescapeString(match[1]))
		}
	}
	return builder.String()
}
