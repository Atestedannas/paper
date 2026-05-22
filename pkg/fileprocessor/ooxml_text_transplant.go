package fileprocessor

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
)

type userTemplatePayload struct {
	coverTitleCells                 [][]string
	coverInfoCells                  [][]string
	coverHeadingText                string
	coverDateText                   string
	frontTitleText                  string
	frontSubtitleText               string
	cnAbstractText                  string
	cnKeywordsText                  string
	enAbstractText                  string
	enKeywordsText                  string
	tocTitleText                    string
	bodyParagraphs                  []string
	referenceItems                  []string
	acknowledgementParagraphs       []string
	frontMatterParagraphXMLs        []string
	bodyFragmentXML                 string
	referenceTitleFragmentXML       string
	referenceFragmentXML            string
	acknowledgementTitleFragmentXML string
	acknowledgementFragmentXML      string
}

func normalizeTemplatePayloadForTemplate(templatePkg *docxPackage, payload *userTemplatePayload) error {
	if templatePkg == nil || payload == nil {
		return nil
	}

	documentXML, ok := templatePkg.entries["word/document.xml"]
	if !ok {
		return fmt.Errorf("missing word/document.xml")
	}

	maxWidth := extractDocxMinimumContentWidth(string(documentXML))
	if maxWidth <= 0 {
		return nil
	}

	var err error
	payload.bodyFragmentXML, err = normalizeDocxTablesToMaxWidth(payload.bodyFragmentXML, maxWidth)
	if err != nil {
		return fmt.Errorf("normalize body tables: %w", err)
	}
	payload.referenceFragmentXML, err = normalizeDocxTablesToMaxWidth(payload.referenceFragmentXML, maxWidth)
	if err != nil {
		return fmt.Errorf("normalize reference tables: %w", err)
	}
	payload.acknowledgementFragmentXML, err = normalizeDocxTablesToMaxWidth(payload.acknowledgementFragmentXML, maxWidth)
	if err != nil {
		return fmt.Errorf("normalize acknowledgement tables: %w", err)
	}
	return nil
}

func normalizeTemplateCloneTypography(templatePath string, outputPkg *docxPackage) error {
	if templatePath == "" || outputPkg == nil {
		return nil
	}

	documentXML, ok := outputPkg.entries["word/document.xml"]
	if !ok {
		return fmt.Errorf("missing word/document.xml")
	}

	templateDoc, err := document.Open(templatePath)
	if err != nil {
		return fmt.Errorf("open template: %w", err)
	}
	defer templateDoc.Close()

	rules := extractStrictTemplateBlockRules(templateDoc, NewEnhancedProcessor())
	bodyXML := extractDocxBodyXML(string(documentXML))
	normalizedBodyXML, err := normalizeDocxBodyTypography(bodyXML, rules)
	if err != nil {
		return err
	}

	outputPkg.entries["word/document.xml"] = []byte(replaceDocxBodyXML(string(documentXML), normalizedBodyXML))
	return nil
}

func extractUserPayload(pkg *docxPackage, blocks map[string]templateBlock) (*userTemplatePayload, error) {
	if pkg == nil {
		return nil, fmt.Errorf("docx package is nil")
	}
	documentXML, ok := pkg.entries["word/document.xml"]
	if !ok {
		return nil, fmt.Errorf("missing word/document.xml")
	}

	required := []string{"toc_title", "references_title", "ack_title"}
	for _, name := range required {
		if _, ok := blocks[name]; !ok {
			return nil, fmt.Errorf("missing template block %q", name)
		}
	}

	bodyXML := extractDocxBodyXML(string(documentXML))
	tables := extractDocxElements(bodyXML, "w:tbl")
	paragraphs := extractDocxParagraphs(bodyXML)
	var err error

	payload := &userTemplatePayload{}
	if len(tables) > 0 {
		payload.coverTitleCells = extractDocxTableCellTextGrid(tables[0])
	}
	if len(tables) > 1 {
		payload.coverInfoCells = extractDocxTableCellTextGrid(tables[1])
	}
	payload.coverHeadingText = findParagraphTextAfter(paragraphs, 0, blocks["abstract_cn_title"].index, isFrontMatterHeadingText)
	payload.coverDateText = findParagraphTextAfter(paragraphs, 0, blocks["abstract_cn_title"].index, isFrontMatterDateText)
	payload.frontTitleText = firstNonEmptyTableCellText(payload.coverTitleCells, 0, 1)
	payload.frontSubtitleText = firstNonEmptyTableCellText(payload.coverTitleCells, 1, 1)
	payload.cnAbstractText = paragraphTextAt(paragraphs, blocks["abstract_cn_title"].index)
	payload.cnKeywordsText = findParagraphTextAfter(paragraphs, blocks["abstract_cn_title"].index+1, blocks["abstract_en_title"].index, isChineseKeywordsParagraphText)
	payload.enAbstractText = paragraphTextAt(paragraphs, blocks["abstract_en_title"].index)
	payload.enKeywordsText = findParagraphTextAfter(paragraphs, blocks["abstract_en_title"].index+1, blocks["toc_title"].index+1, isEnglishKeywordsParagraphText)
	payload.tocTitleText = paragraphTextAt(paragraphs, blocks["toc_title"].index)

	bodyStart := blocks["toc_title"].index + 1
	referencesStart := blocks["references_title"].index
	ackStart := blocks["ack_title"].index

	payload.frontMatterParagraphXMLs = collectMeaningfulDocxParagraphFragments(paragraphs, 0, blocks["toc_title"].index+1)
	payload.bodyParagraphs = collectParagraphTexts(paragraphs, bodyStart, referencesStart)
	payload.referenceItems = collectParagraphTexts(paragraphs, referencesStart+1, ackStart)
	payload.acknowledgementParagraphs = collectParagraphTexts(paragraphs, ackStart+1, len(paragraphs))
	payload.bodyFragmentXML, err = extractDocxParagraphRangeXML(bodyXML, bodyStart, referencesStart)
	if err != nil {
		return nil, fmt.Errorf("extract body fragment: %w", err)
	}
	payload.referenceTitleFragmentXML, err = extractDocxParagraphRangeXML(bodyXML, referencesStart, referencesStart+1)
	if err != nil {
		return nil, fmt.Errorf("extract references title fragment: %w", err)
	}
	payload.referenceFragmentXML, err = extractDocxParagraphRangeXML(bodyXML, referencesStart+1, ackStart)
	if err != nil {
		return nil, fmt.Errorf("extract references fragment: %w", err)
	}
	payload.acknowledgementTitleFragmentXML, err = extractDocxParagraphRangeXML(bodyXML, ackStart, ackStart+1)
	if err != nil {
		return nil, fmt.Errorf("extract acknowledgement title fragment: %w", err)
	}
	payload.acknowledgementFragmentXML, err = extractDocxParagraphRangeXML(bodyXML, ackStart+1, len(paragraphs))
	if err != nil {
		return nil, fmt.Errorf("extract acknowledgement fragment: %w", err)
	}
	return payload, nil
}

func transplantUserPayload(pkg *docxPackage, blocks map[string]templateBlock, payload *userTemplatePayload, rules strictTemplateBlockRules) error {
	if pkg == nil {
		return fmt.Errorf("docx package is nil")
	}
	if payload == nil {
		return fmt.Errorf("payload is nil")
	}

	documentXML, ok := pkg.entries["word/document.xml"]
	if !ok {
		return fmt.Errorf("missing word/document.xml")
	}

	bodyXML := extractDocxBodyXML(string(documentXML))
	var err error
	templateParagraphCount := countDocxParagraphs(bodyXML)

	if len(payload.coverTitleCells) > 0 {
		bodyXML, err = replaceNthTableCellTextGrid(bodyXML, 0, payload.coverTitleCells)
		if err != nil {
			return err
		}
	}
	if len(payload.coverInfoCells) > 0 {
		bodyXML, err = replaceNthTableCellTextGrid(bodyXML, 1, payload.coverInfoCells)
		if err != nil {
			return err
		}
	}

	bodyXML, err = replaceDocxParagraphRangeXML(bodyXML, blocks["ack_title"].index+1, templateParagraphCount, payload.acknowledgementFragmentXML)
	if err != nil {
		return err
	}
	if payload.acknowledgementTitleFragmentXML != "" {
		bodyXML, err = replaceDocxParagraphRangeXML(bodyXML, blocks["ack_title"].index, blocks["ack_title"].index+1, payload.acknowledgementTitleFragmentXML)
		if err != nil {
			return err
		}
	}
	bodyXML, err = replaceDocxParagraphRangeXML(bodyXML, blocks["references_title"].index+1, blocks["ack_title"].index, payload.referenceFragmentXML)
	if err != nil {
		return err
	}
	if payload.referenceTitleFragmentXML != "" {
		bodyXML, err = replaceDocxParagraphRangeXML(bodyXML, blocks["references_title"].index, blocks["references_title"].index+1, payload.referenceTitleFragmentXML)
		if err != nil {
			return err
		}
	}
	bodyXML, err = replaceDocxParagraphRangeXML(bodyXML, blocks["toc_title"].index+1, blocks["references_title"].index, payload.bodyFragmentXML)
	if err != nil {
		return err
	}
	if shouldTransplantInstructionalFrontMatterVerbatim(blocks) {
		bodyXML, err = rewriteInstructionalFrontMatter(bodyXML, blocks, payload, rules)
		if err != nil {
			return err
		}
	}

	pkg.entries["word/document.xml"] = []byte(replaceDocxBodyXML(string(documentXML), bodyXML))
	return nil
}

func shouldTransplantInstructionalFrontMatterVerbatim(blocks map[string]templateBlock) bool {
	for _, name := range []string{"abstract_cn_title", "abstract_en_title", "toc_title"} {
		block, ok := blocks[name]
		if !ok {
			continue
		}
		if isTemplateInstructionText(block.text) {
			return true
		}
	}
	return false
}

func collectMeaningfulDocxParagraphFragments(paragraphs []docxParagraph, startIndex, endIndex int) []string {
	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > len(paragraphs) {
		endIndex = len(paragraphs)
	}
	if startIndex >= endIndex {
		return nil
	}
	fragments := make([]string, 0, endIndex-startIndex)
	for _, paragraph := range paragraphs[startIndex:endIndex] {
		if paragraph.inTable {
			continue
		}
		text := strings.TrimSpace(normalizeVisibleText(extractDocxText(paragraph.xml)))
		if text == "" {
			continue
		}
		fragments = append(fragments, paragraph.xml)
	}
	return fragments
}

func firstMeaningfulNonTableParagraphIndex(paragraphs []docxParagraph, endIndex int) int {
	if endIndex > len(paragraphs) {
		endIndex = len(paragraphs)
	}
	for _, paragraph := range paragraphs[:endIndex] {
		if paragraph.inTable {
			continue
		}
		text := strings.TrimSpace(normalizeVisibleText(extractDocxText(paragraph.xml)))
		if text == "" {
			continue
		}
		return paragraph.index
	}
	return -1
}

type docxParagraphRewrite struct {
	apply bool
	xml   string
}

func rewriteInstructionalFrontMatter(bodyXML string, blocks map[string]templateBlock, payload *userTemplatePayload, rules strictTemplateBlockRules) (string, error) {
	paragraphs := extractDocxParagraphs(bodyXML)
	if len(paragraphs) == 0 {
		return bodyXML, nil
	}

	rewrites := make(map[int]docxParagraphRewrite)
	tocLimit := blocks["toc_title"].index

	for _, paragraph := range paragraphs {
		if paragraph.index > tocLimit || paragraph.inTable {
			continue
		}

		text := docxParagraphVisibleText(paragraph)
		if text == "" {
			continue
		}

		switch {
		case strings.HasPrefix(text, "封面格式不要调整"):
			if payload.coverHeadingText != "" {
				rewrites[paragraph.index] = rewriteParagraphText(paragraph.xml, payload.coverHeadingText)
			} else {
				rewrites[paragraph.index] = docxParagraphRewrite{apply: true}
			}
		case strings.HasPrefix(text, "选题题目一般不超过"), strings.HasPrefix(text, "要求：宋体、二号"), strings.HasPrefix(text, "要求：从学院到指导教师"), strings.HasPrefix(text, "要求：宋体、三号"):
			rewrites[paragraph.index] = docxParagraphRewrite{apply: true}
		case isTemplateDatePlaceholderText(text):
			rewrites[paragraph.index] = rewriteParagraphText(paragraph.xml, payload.coverDateText)
		case strings.HasPrefix(text, "正标题："):
			if payload.frontTitleText != "" {
				rewrites[paragraph.index] = rewriteParagraphText(paragraph.xml, payload.frontTitleText)
			} else {
				rewrites[paragraph.index] = docxParagraphRewrite{apply: true}
			}
		case strings.HasPrefix(text, "副标题："), strings.HasPrefix(text, "正标题和副标题之间设置"):
			rewrites[paragraph.index] = docxParagraphRewrite{apply: true}
		case isTemplateSubtitlePlaceholderText(text):
			subtitleText := payload.frontSubtitleText
			if subtitleText == "" {
				subtitleText = payload.frontTitleText
			}
			if subtitleText != "" && !strings.HasPrefix(subtitleText, "——") {
				subtitleText = "——" + subtitleText
			}
			rewrites[paragraph.index] = rewriteParagraphText(paragraph.xml, subtitleText)
		case isInlineChineseAbstractAnchorParagraph(paragraph.xml):
			rewrites[paragraph.index] = rewriteInlineParagraph(paragraph.xml, payload.cnAbstractText, rules.Inline[strictBlockAbstractCN])
		case isChineseKeywordsParagraphText(text):
			rewrites[paragraph.index] = rewriteInlineParagraph(paragraph.xml, payload.cnKeywordsText, rules.Inline[strictBlockKeywordsCN])
		case isInlineEnglishAbstractAnchorParagraph(paragraph.xml):
			rewrites[paragraph.index] = rewriteInlineParagraph(paragraph.xml, payload.enAbstractText, rules.Inline[strictBlockAbstractEN])
		case isEnglishKeywordsParagraphText(text):
			rewrites[paragraph.index] = rewriteInlineParagraph(paragraph.xml, payload.enKeywordsText, rules.Inline[strictBlockKeywordsEN])
		case strings.Contains(text, "目录”二字") || strings.Contains(text, "目录\"二字"):
			rewrites[paragraph.index] = rewriteParagraphText(paragraph.xml, payload.tocTitleText)
		case isInstructionTOCAnchorParagraph(paragraph.xml), strings.HasPrefix(text, "摘要页码："), strings.HasPrefix(text, "“绪论”具体内容"), strings.HasPrefix(text, "\"绪论\"具体内容"), strings.HasPrefix(text, "目录中的页码从"):
			rewrites[paragraph.index] = docxParagraphRewrite{apply: true}
		}
	}

	return rewriteDocxParagraphsByIndex(bodyXML, rewrites)
}

func rewriteDocxParagraphsByIndex(xmlText string, rewrites map[int]docxParagraphRewrite) (string, error) {
	if len(rewrites) == 0 {
		return xmlText, nil
	}

	offsets := extractDocxParagraphOffsets(xmlText)
	var builder strings.Builder
	last := 0
	for idx, offset := range offsets {
		builder.WriteString(xmlText[last:offset.start])
		if rewrite, ok := rewrites[idx]; ok && rewrite.apply {
			builder.WriteString(rewrite.xml)
		} else {
			builder.WriteString(xmlText[offset.start:offset.end])
		}
		last = offset.end
	}
	builder.WriteString(xmlText[last:])
	return builder.String(), nil
}

func rewriteParagraphText(paragraphXML, text string) docxParagraphRewrite {
	if strings.TrimSpace(text) == "" {
		return docxParagraphRewrite{apply: true}
	}
	rewritten, err := rewriteDocxParagraphTextWithTemplateDefaults(paragraphXML, text)
	if err != nil {
		return docxParagraphRewrite{apply: true, xml: paragraphXML}
	}
	return docxParagraphRewrite{apply: true, xml: rewritten}
}

func rewriteInlineParagraph(paragraphXML, text string, rule inlinePrefixRule) docxParagraphRewrite {
	if strings.TrimSpace(text) == "" {
		return docxParagraphRewrite{apply: true}
	}
	if rule.Prefix != "" {
		rewritten, err := rewriteDocxInlineParagraphWithRule(paragraphXML, text, rule)
		if err == nil {
			return docxParagraphRewrite{apply: true, xml: rewritten}
		}
	}
	return rewriteParagraphText(paragraphXML, text)
}

func rewriteDocxParagraphTextWithTemplateDefaults(paragraphXML, text string) (string, error) {
	startTagMatch := regexp.MustCompile(`(?s)^<w:p\b[^>]*>`).FindString(paragraphXML)
	if startTagMatch == "" {
		return "", fmt.Errorf("paragraph missing opening tag")
	}
	endTagIndex := strings.LastIndex(paragraphXML, "</w:p>")
	if endTagIndex == -1 {
		return "", fmt.Errorf("paragraph missing closing tag")
	}

	pPrXML := regexp.MustCompile(`(?s)<w:pPr\b[^>]*>.*?</w:pPr>`).FindString(paragraphXML)

	var builder strings.Builder
	builder.WriteString(startTagMatch)
	if pPrXML != "" {
		builder.WriteString(pPrXML)
	}
	builder.WriteString(buildDocxInsertedRun(paragraphXML, text))
	builder.WriteString(paragraphXML[endTagIndex:])
	return builder.String(), nil
}

func paragraphTextAt(paragraphs []docxParagraph, index int) string {
	if index < 0 || index >= len(paragraphs) {
		return ""
	}
	return docxParagraphVisibleText(paragraphs[index])
}

func docxParagraphVisibleText(paragraph docxParagraph) string {
	return strings.TrimSpace(normalizeVisibleText(extractDocxText(paragraph.xml)))
}

func findParagraphTextAfter(paragraphs []docxParagraph, startIndex, endIndex int, match func(string) bool) string {
	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > len(paragraphs) {
		endIndex = len(paragraphs)
	}
	for _, paragraph := range paragraphs[startIndex:endIndex] {
		if paragraph.inTable {
			continue
		}
		text := docxParagraphVisibleText(paragraph)
		if text == "" {
			continue
		}
		if match(text) {
			return text
		}
	}
	return ""
}

func firstNonEmptyTableCellText(grid [][]string, rowIndex, colIndex int) string {
	if rowIndex < 0 || rowIndex >= len(grid) {
		return ""
	}
	row := grid[rowIndex]
	if colIndex < 0 || colIndex >= len(row) {
		return ""
	}
	return strings.TrimSpace(normalizeVisibleText(row[colIndex]))
}

func isFrontMatterHeadingText(text string) bool {
	return strings.Contains(text, "本科毕业论文") || strings.Contains(text, "毕业论文/设计")
}

func isFrontMatterDateText(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.Contains(trimmed, "年") && strings.Contains(trimmed, "月") && strings.ContainsAny(trimmed, "0123456789")
}

func isTemplateDatePlaceholderText(text string) bool {
	return strings.Contains(text, "202X年") || strings.Contains(text, "202X")
}

func isChineseKeywordsParagraphText(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "关键词：") || strings.HasPrefix(strings.TrimSpace(text), "关键词:")
}

func isEnglishKeywordsParagraphText(text string) bool {
	normalized := strings.TrimSpace(text)
	return strings.HasPrefix(normalized, "Key words:") || strings.HasPrefix(normalized, "Key words：") || strings.HasPrefix(normalized, "Keywords:") || strings.HasPrefix(normalized, "Keywords：")
}

func isTemplateSubtitlePlaceholderText(text string) bool {
	normalized := strings.TrimSpace(text)
	return strings.HasPrefix(normalized, "——") && strings.Contains(normalized, "XXX")
}

func collectParagraphTexts(paragraphs []docxParagraph, startIndex, endIndex int) []string {
	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > len(paragraphs) {
		endIndex = len(paragraphs)
	}
	if startIndex >= endIndex {
		return nil
	}
	texts := make([]string, 0, endIndex-startIndex)
	for _, paragraph := range paragraphs[startIndex:endIndex] {
		if paragraph.inTable {
			continue
		}
		text := strings.TrimSpace(normalizeVisibleText(extractDocxText(paragraph.xml)))
		if text == "" {
			continue
		}
		texts = append(texts, text)
	}
	return texts
}

func extractDocxTableCellTextGrid(tableXML string) [][]string {
	rows := extractDocxElements(tableXML, "w:tr")
	grid := make([][]string, 0, len(rows))
	for _, rowXML := range rows {
		cells := extractDocxElements(rowXML, "w:tc")
		if len(cells) == 0 {
			continue
		}
		rowTexts := make([]string, 0, len(cells))
		for _, cellXML := range cells {
			rowTexts = append(rowTexts, normalizeVisibleText(extractDocxText(cellXML)))
		}
		grid = append(grid, rowTexts)
	}
	return grid
}

func replaceNthTableCellTextGrid(xmlText string, occurrence int, replacements [][]string) (string, error) {
	pattern := regexp.MustCompile(`(?s)<w:tbl\b[^>]*>.*?</w:tbl>`)
	matches := pattern.FindAllStringIndex(xmlText, -1)
	if occurrence < 0 || occurrence >= len(matches) {
		return "", fmt.Errorf("missing w:tbl occurrence %d", occurrence)
	}

	start, end := matches[occurrence][0], matches[occurrence][1]
	fragment := xmlText[start:end]
	updated, err := replaceDocxTableCellTextGrid(fragment, replacements)
	if err != nil {
		return "", err
	}
	return xmlText[:start] + updated + xmlText[end:], nil
}

func replaceDocxTableCellTextGrid(tableXML string, replacements [][]string) (string, error) {
	rows := extractDocxElements(tableXML, "w:tr")
	if len(rows) == 0 {
		return tableXML, nil
	}

	var builder strings.Builder
	last := 0
	for rowIndex, rowXML := range rows {
		rowStart := strings.Index(tableXML[last:], rowXML)
		if rowStart == -1 {
			return "", fmt.Errorf("locate table row %d", rowIndex)
		}
		rowStart += last
		rowEnd := rowStart + len(rowXML)

		builder.WriteString(tableXML[last:rowStart])
		rowReplacements := []string(nil)
		if rowIndex < len(replacements) {
			rowReplacements = replacements[rowIndex]
		}
		updatedRow, err := replaceDocxTableRowCellTexts(rowXML, rowReplacements)
		if err != nil {
			return "", err
		}
		builder.WriteString(updatedRow)
		last = rowEnd
	}
	builder.WriteString(tableXML[last:])
	return builder.String(), nil
}

func replaceDocxTableRowCellTexts(rowXML string, replacements []string) (string, error) {
	cells := extractDocxElements(rowXML, "w:tc")
	if len(cells) == 0 {
		return rowXML, nil
	}

	var builder strings.Builder
	last := 0
	for cellIndex, cellXML := range cells {
		cellStart := strings.Index(rowXML[last:], cellXML)
		if cellStart == -1 {
			return "", fmt.Errorf("locate table cell %d", cellIndex)
		}
		cellStart += last
		cellEnd := cellStart + len(cellXML)

		builder.WriteString(rowXML[last:cellStart])
		replacement := ""
		if cellIndex < len(replacements) {
			replacement = replacements[cellIndex]
		}
		updatedCell, err := replaceDocxTableCellText(cellXML, replacement)
		if err != nil {
			return "", err
		}
		builder.WriteString(updatedCell)
		last = cellEnd
	}
	builder.WriteString(rowXML[last:])
	return builder.String(), nil
}

func replaceDocxTableCellText(cellXML, replacement string) (string, error) {
	paragraphs := extractDocxElements(cellXML, "w:p")
	if len(paragraphs) == 0 {
		if replacement == "" {
			return cellXML, nil
		}
		return "", fmt.Errorf("table cell has no paragraph to receive text")
	}

	var builder strings.Builder
	last := 0
	for paragraphIndex, paragraphXML := range paragraphs {
		paragraphStart := strings.Index(cellXML[last:], paragraphXML)
		if paragraphStart == -1 {
			return "", fmt.Errorf("locate table cell paragraph %d", paragraphIndex)
		}
		paragraphStart += last
		paragraphEnd := paragraphStart + len(paragraphXML)

		builder.WriteString(cellXML[last:paragraphStart])
		text := ""
		if paragraphIndex == 0 {
			text = replacement
		}
		updatedParagraph, err := replaceDocxParagraphText(paragraphXML, text)
		if err != nil {
			return "", err
		}
		builder.WriteString(updatedParagraph)
		last = paragraphEnd
	}
	builder.WriteString(cellXML[last:])
	return builder.String(), nil
}

func replaceDocxParagraphText(paragraphXML, replacement string) (string, error) {
	if countDocxTextNodes(paragraphXML) > 0 {
		return replaceDocxTextNodes(paragraphXML, []string{replacement}), nil
	}
	if replacement == "" {
		return paragraphXML, nil
	}

	insertIndex := strings.LastIndex(paragraphXML, "</w:p>")
	if insertIndex == -1 {
		return "", fmt.Errorf("paragraph missing </w:p>")
	}

	runXML := buildDocxInsertedRun(paragraphXML, replacement)
	return paragraphXML[:insertIndex] + runXML + paragraphXML[insertIndex:], nil
}

func buildDocxInsertedRun(paragraphXML, text string) string {
	var builder strings.Builder
	builder.WriteString("<w:r>")
	if runProps := extractParagraphDefaultRunProperties(paragraphXML); runProps != "" {
		builder.WriteString(runProps)
	}
	builder.WriteString(buildDocxTextElement(text))
	builder.WriteString("</w:r>")
	return builder.String()
}

func extractParagraphDefaultRunProperties(paragraphXML string) string {
	pPrMatch := regexp.MustCompile(`(?s)<w:pPr\b[^>]*>.*?</w:pPr>`).FindString(paragraphXML)
	if pPrMatch == "" {
		return ""
	}
	return regexp.MustCompile(`(?s)<w:rPr\b[^>]*>.*?</w:rPr>`).FindString(pPrMatch)
}

func buildDocxTextElement(text string) string {
	escaped := html.EscapeString(text)
	if text != strings.TrimSpace(text) || strings.Contains(text, "  ") {
		return `<w:t xml:space="preserve">` + escaped + `</w:t>`
	}
	return `<w:t>` + escaped + `</w:t>`
}

func replaceParagraphRangeTexts(xmlText string, startIndex, endIndex int, replacements []string) (string, error) {
	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex < startIndex {
		return xmlText, nil
	}

	pattern := regexp.MustCompile(`(?s)<w:p\b[^>]*>.*?</w:p>`)
	matches := pattern.FindAllStringIndex(xmlText, -1)
	if endIndex > len(matches) {
		endIndex = len(matches)
	}
	if startIndex > len(matches) {
		return xmlText, nil
	}

	var builder strings.Builder
	last := 0
	for i, match := range matches {
		builder.WriteString(xmlText[last:match[0]])
		fragment := xmlText[match[0]:match[1]]
		if i >= startIndex && i < endIndex {
			replacement := ""
			if replacementIndex := i - startIndex; replacementIndex < len(replacements) {
				replacement = replacements[replacementIndex]
			}
			fragment = replaceDocxTextNodes(fragment, []string{replacement})
		}
		builder.WriteString(fragment)
		last = match[1]
	}
	builder.WriteString(xmlText[last:])
	return builder.String(), nil
}

func extractDocxParagraphRangeXML(xmlText string, startIndex, endIndex int) (string, error) {
	start, end, err := resolveDocxParagraphRangeBounds(xmlText, startIndex, endIndex)
	if err != nil {
		return "", err
	}
	return xmlText[start:end], nil
}

func replaceDocxParagraphRangeXML(xmlText string, startIndex, endIndex int, replacement string) (string, error) {
	start, end, err := resolveDocxParagraphRangeBounds(xmlText, startIndex, endIndex)
	if err != nil {
		return "", err
	}
	return xmlText[:start] + replacement + xmlText[end:], nil
}

func replaceDocxParagraphSlotsXML(xmlText string, paragraphs []docxParagraph, startIndex, endIndex int, replacements []string) (string, error) {
	offsets := extractDocxParagraphOffsets(xmlText)
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex > len(offsets) {
		startIndex = len(offsets)
	}
	if endIndex < startIndex {
		endIndex = startIndex
	}
	if endIndex > len(offsets) {
		endIndex = len(offsets)
	}

	var builder strings.Builder
	last := 0
	replacementIndex := 0
	for idx, offset := range offsets {
		builder.WriteString(xmlText[last:offset.start])
		if idx >= startIndex && idx < endIndex {
			if idx < len(paragraphs) && paragraphs[idx].inTable {
				builder.WriteString(xmlText[offset.start:offset.end])
			} else if replacementIndex < len(replacements) {
				builder.WriteString(replacements[replacementIndex])
				replacementIndex++
			}
		} else {
			builder.WriteString(xmlText[offset.start:offset.end])
		}
		last = offset.end
	}
	builder.WriteString(xmlText[last:])
	return builder.String(), nil
}

func resolveDocxParagraphRangeBounds(xmlText string, startIndex, endIndex int) (int, int, error) {
	offsets := extractDocxParagraphOffsets(xmlText)
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex > len(offsets) {
		startIndex = len(offsets)
	}
	if endIndex < startIndex {
		endIndex = startIndex
	}
	if endIndex > len(offsets) {
		endIndex = len(offsets)
	}

	start := 0
	if startIndex > 0 {
		start = offsets[startIndex-1].end
	}
	end := len(xmlText)
	if trailingSectPrStart := findTrailingBodySectPrStart(xmlText); trailingSectPrStart >= 0 {
		end = trailingSectPrStart
	}
	if endIndex < len(offsets) {
		end = offsets[endIndex].start
	}
	if start > end {
		return 0, 0, fmt.Errorf("invalid paragraph range bounds start=%d end=%d", start, end)
	}
	return start, end, nil
}

type docxParagraphOffset struct {
	start int
	end   int
}

func extractDocxParagraphOffsets(xmlText string) []docxParagraphOffset {
	pattern := regexp.MustCompile(`(?s)<w:p\b[^>]*>.*?</w:p>`)
	matches := pattern.FindAllStringIndex(xmlText, -1)
	offsets := make([]docxParagraphOffset, 0, len(matches))
	for _, match := range matches {
		offsets = append(offsets, docxParagraphOffset{start: match[0], end: match[1]})
	}
	return offsets
}

func findTrailingBodySectPrStart(xmlText string) int {
	return strings.LastIndex(xmlText, "<w:sectPr")
}

func replaceDocxBodyXML(documentXML, bodyXML string) string {
	pattern := regexp.MustCompile(`(?s)(<w:body\b[^>]*>)(.*?)(</w:body>)`)
	return pattern.ReplaceAllString(documentXML, `${1}`+bodyXML+`${3}`)
}

func extractDocxTextNodes(xmlText string) []string {
	re := regexp.MustCompile(`(?s)<w:t\b[^>]*>(.*?)</w:t>`)
	matches := re.FindAllStringSubmatch(xmlText, -1)
	texts := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			texts = append(texts, html.UnescapeString(match[1]))
		}
	}
	return texts
}

func replaceDocxTextNodes(fragment string, replacements []string) string {
	re := regexp.MustCompile(`(?s)(<w:t\b[^>]*>)(.*?)(</w:t>)`)
	matches := re.FindAllStringSubmatchIndex(fragment, -1)
	if len(matches) == 0 {
		return fragment
	}

	var builder strings.Builder
	last := 0
	for i, match := range matches {
		builder.WriteString(fragment[last:match[4]])
		text := ""
		if i < len(replacements) {
			text = replacements[i]
		}
		builder.WriteString(html.EscapeString(text))
		last = match[5]
	}
	builder.WriteString(fragment[last:])
	return builder.String()
}

func countDocxParagraphs(xmlText string) int {
	pattern := regexp.MustCompile(`(?s)<w:p\b[^>]*>.*?</w:p>`)
	return len(pattern.FindAllStringIndex(xmlText, -1))
}

func normalizeDocxBodyTypography(bodyXML string, rules strictTemplateBlockRules) (string, error) {
	paragraphs := extractDocxParagraphs(bodyXML)
	if len(paragraphs) == 0 {
		return bodyXML, nil
	}

	var builder strings.Builder
	var state strictParagraphState
	searchStart := 0
	last := 0
	for _, paragraph := range paragraphs {
		start := strings.Index(bodyXML[searchStart:], paragraph.xml)
		if start == -1 {
			continue
		}
		start += searchStart
		end := start + len(paragraph.xml)

		builder.WriteString(bodyXML[last:start])

		updatedParagraph := paragraph.xml
		text := strings.TrimSpace(normalizeVisibleText(extractDocxText(paragraph.xml)))
		if text != "" && (paragraph.tableIndex < 0 || paragraph.tableIndex >= 2) && !isTemplateInstructionText(text) {
			kind := detectTemplateCloneTypographyBlock(text, &state)
			var err error
			switch {
			case kind == strictBlockAbstractCN || kind == strictBlockAbstractEN || kind == strictBlockKeywordsCN || kind == strictBlockKeywordsEN:
				if rule, ok := rules.Inline[kind]; ok {
					updatedParagraph, err = rewriteDocxInlineParagraphWithRule(updatedParagraph, text, rule)
				}
			default:
				if spec, ok := rules.Paragraph[kind]; ok && !spec.IsEmpty() {
					updatedParagraph, err = applyTypographySpecToParagraphXML(updatedParagraph, spec)
				}
			}
			if err != nil {
				return "", err
			}
		}

		builder.WriteString(updatedParagraph)
		last = end
		searchStart = end
	}

	builder.WriteString(bodyXML[last:])
	return builder.String(), nil
}

func rewriteDocxInlineParagraphWithRule(paragraphXML, text string, rule inlinePrefixRule) (string, error) {
	if !strings.HasPrefix(text, rule.Prefix) {
		return paragraphXML, nil
	}

	bodyText := strings.TrimPrefix(text, rule.Prefix)
	segments := []styledRunSegment{
		{Text: rule.Prefix, Spec: normalizeTypographySpec(rule.LabelSpec)},
		{Text: bodyText, Spec: normalizeTypographySpec(rule.BodySpec)},
	}
	return rewriteDocxParagraphRunsWithSpecs(paragraphXML, segments)
}

func applyTypographySpecToParagraphXML(paragraphXML string, spec ParagraphFormatSpec) (string, error) {
	spec = normalizeTypographySpec(spec)
	return replaceDocxRuns(paragraphXML, func(runXML string) (string, error) {
		if strings.TrimSpace(extractDocxText(runXML)) == "" {
			return runXML, nil
		}
		return upsertDocxRunTypographySpec(runXML, spec)
	})
}

func normalizeTypographySpec(spec ParagraphFormatSpec) ParagraphFormatSpec {
	spec = sanitizeParagraphFormatSpec(spec)
	spec = completeParagraphFontSpec(spec)
	if spec.FontSizeHalfPt == 0 && spec.FontSizeCSHalfPt > 0 {
		spec.FontSizeHalfPt = spec.FontSizeCSHalfPt
	}
	if spec.FontSizeCSHalfPt == 0 && spec.FontSizeHalfPt > 0 {
		spec.FontSizeCSHalfPt = spec.FontSizeHalfPt
	}
	return spec
}

func detectTemplateCloneTypographyBlock(text string, state *strictParagraphState) strictBlockKind {
	normalizedText := strings.TrimSpace(normalizeChineseText(normalizeVisibleText(text)))
	switch {
	case normalizedText == "摘要" || normalizedText == "中文摘要" || normalizedText == "论文摘要":
		state.CoverDateSeen = true
		return strictBlockUnknown
	case strings.EqualFold(strings.TrimSpace(normalizeVisibleText(text)), "Abstract"):
		state.CoverDateSeen = true
		return strictBlockUnknown
	case normalizedText == "目录":
		state.CoverDateSeen = true
		return strictBlockTOCTitle
	default:
		return detectStrictParagraphBlock(strictParagraphRef{}, text, state)
	}
}

func rewriteDocxParagraphRunsWithSpecs(paragraphXML string, segments []styledRunSegment) (string, error) {
	startTagMatch := regexp.MustCompile(`(?s)^<w:p\b[^>]*>`).FindString(paragraphXML)
	if startTagMatch == "" {
		return "", fmt.Errorf("paragraph missing opening tag")
	}
	endTagIndex := strings.LastIndex(paragraphXML, "</w:p>")
	if endTagIndex == -1 {
		return "", fmt.Errorf("paragraph missing closing tag")
	}

	pPrXML := regexp.MustCompile(`(?s)<w:pPr\b[^>]*>.*?</w:pPr>`).FindString(paragraphXML)

	var builder strings.Builder
	builder.WriteString(startTagMatch)
	if pPrXML != "" {
		builder.WriteString(pPrXML)
	}
	for _, segment := range segments {
		if segment.Text == "" {
			continue
		}
		builder.WriteString(buildDocxRunWithSpecXML(segment.Text, segment.Spec))
	}
	builder.WriteString(paragraphXML[endTagIndex:])
	return builder.String(), nil
}

func buildDocxRunWithSpecXML(text string, spec ParagraphFormatSpec) string {
	spec = normalizeTypographySpec(spec)
	var builder strings.Builder
	builder.WriteString("<w:r>")
	if rPrXML := buildDocxRunPropertiesXML(spec); rPrXML != "" {
		builder.WriteString(rPrXML)
	}
	builder.WriteString(buildDocxTextElement(text))
	builder.WriteString("</w:r>")
	return builder.String()
}

func replaceDocxRuns(xmlText string, update func(runXML string) (string, error)) (string, error) {
	pattern := regexp.MustCompile(`(?s)<w:r\b[^>]*>.*?</w:r>`)
	matches := pattern.FindAllStringIndex(xmlText, -1)
	if len(matches) == 0 {
		return xmlText, nil
	}

	var builder strings.Builder
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		builder.WriteString(xmlText[last:start])
		updated, err := update(xmlText[start:end])
		if err != nil {
			return "", err
		}
		builder.WriteString(updated)
		last = end
	}
	builder.WriteString(xmlText[last:])
	return builder.String(), nil
}

func upsertDocxRunTypographySpec(runXML string, spec ParagraphFormatSpec) (string, error) {
	rPrXML := buildDocxRunPropertiesXML(spec)
	if rPrXML == "" {
		return runXML, nil
	}

	rPrPattern := regexp.MustCompile(`(?s)<w:rPr\b[^>]*>.*?</w:rPr>`)
	if loc := rPrPattern.FindStringIndex(runXML); loc != nil {
		current := runXML[loc[0]:loc[1]]
		contentMatch := regexp.MustCompile(`(?s)<w:rPr\b[^>]*>(.*)</w:rPr>`).FindStringSubmatch(current)
		content := ""
		if len(contentMatch) > 1 {
			content = stripManagedDocxRunPropertyTags(contentMatch[1])
		}
		replacement := "<w:rPr>" + managedDocxRunPropertiesInnerXML(spec) + content + "</w:rPr>"
		return runXML[:loc[0]] + replacement + runXML[loc[1]:], nil
	}

	insertIndex := strings.Index(runXML, ">")
	if insertIndex == -1 {
		return "", fmt.Errorf("run missing opening tag terminator")
	}
	insertIndex++
	return runXML[:insertIndex] + rPrXML + runXML[insertIndex:], nil
}

func buildDocxRunPropertiesXML(spec ParagraphFormatSpec) string {
	inner := managedDocxRunPropertiesInnerXML(spec)
	if inner == "" {
		return ""
	}
	return "<w:rPr>" + inner + "</w:rPr>"
}

func managedDocxRunPropertiesInnerXML(spec ParagraphFormatSpec) string {
	spec = normalizeTypographySpec(spec)
	var builder strings.Builder
	if spec.FontEastAsia != "" || spec.FontAscii != "" {
		builder.WriteString(`<w:rFonts`)
		if spec.FontAscii != "" {
			ascii := html.EscapeString(spec.FontAscii)
			builder.WriteString(` w:ascii="` + ascii + `"`)
			builder.WriteString(` w:hAnsi="` + ascii + `"`)
			builder.WriteString(` w:cs="` + ascii + `"`)
		}
		if spec.FontEastAsia != "" {
			builder.WriteString(` w:eastAsia="` + html.EscapeString(spec.FontEastAsia) + `"`)
		}
		builder.WriteString(`/>`)
	}
	if spec.FontSizeHalfPt > 0 {
		builder.WriteString(`<w:sz w:val="` + strconv.FormatUint(spec.FontSizeHalfPt, 10) + `"/>`)
	}
	if spec.FontSizeCSHalfPt > 0 {
		builder.WriteString(`<w:szCs w:val="` + strconv.FormatUint(spec.FontSizeCSHalfPt, 10) + `"/>`)
	}
	if spec.Bold {
		builder.WriteString(`<w:b/><w:bCs/>`)
	}
	if spec.Italic {
		builder.WriteString(`<w:i/><w:iCs/>`)
	}
	if spec.Underline {
		builder.WriteString(`<w:u w:val="single"/>`)
	}
	if spec.ColorHex != "" {
		builder.WriteString(`<w:color w:val="` + html.EscapeString(spec.ColorHex) + `"/>`)
	}
	return builder.String()
}

func stripManagedDocxRunPropertyTags(xmlText string) string {
	patterns := []string{
		`(?s)<w:rFonts\b[^>]*/>`,
		`(?s)<w:sz\b[^>]*/>`,
		`(?s)<w:szCs\b[^>]*/>`,
		`(?s)<w:b\b[^>]*/>`,
		`(?s)<w:b\b[^>]*>.*?</w:b>`,
		`(?s)<w:bCs\b[^>]*/>`,
		`(?s)<w:bCs\b[^>]*>.*?</w:bCs>`,
		`(?s)<w:i\b[^>]*/>`,
		`(?s)<w:i\b[^>]*>.*?</w:i>`,
		`(?s)<w:iCs\b[^>]*/>`,
		`(?s)<w:iCs\b[^>]*>.*?</w:iCs>`,
		`(?s)<w:u\b[^>]*/>`,
		`(?s)<w:u\b[^>]*>.*?</w:u>`,
		`(?s)<w:color\b[^>]*/>`,
		`(?s)<w:color\b[^>]*>.*?</w:color>`,
	}
	cleaned := xmlText
	for _, pattern := range patterns {
		cleaned = regexp.MustCompile(pattern).ReplaceAllString(cleaned, "")
	}
	return cleaned
}

func extractDocxMinimumContentWidth(documentXML string) int {
	sectionPattern := regexp.MustCompile(`(?s)<w:sectPr\b[^>]*>.*?</w:sectPr>`)
	sections := sectionPattern.FindAllString(documentXML, -1)
	minWidth := 0
	for _, sectionXML := range sections {
		pageWidth, ok := extractDocxTagAttrInt(sectionXML, "w:w")
		if !ok {
			continue
		}
		leftMargin, _ := extractDocxTagAttrInt(sectionXML, "w:left")
		rightMargin, _ := extractDocxTagAttrInt(sectionXML, "w:right")
		contentWidth := pageWidth - leftMargin - rightMargin
		if contentWidth <= 0 {
			continue
		}
		if minWidth == 0 || contentWidth < minWidth {
			minWidth = contentWidth
		}
	}
	return minWidth
}

func normalizeDocxTablesToMaxWidth(xmlText string, maxWidth int) (string, error) {
	if maxWidth <= 0 || xmlText == "" {
		return xmlText, nil
	}

	tablePattern := regexp.MustCompile(`(?s)<w:tbl\b[^>]*>.*?</w:tbl>`)
	matches := tablePattern.FindAllStringIndex(xmlText, -1)
	if len(matches) == 0 {
		return xmlText, nil
	}

	var builder strings.Builder
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		builder.WriteString(xmlText[last:start])

		tableXML := xmlText[start:end]
		normalized, err := normalizeDocxTableToMaxWidth(tableXML, maxWidth)
		if err != nil {
			return "", err
		}
		builder.WriteString(normalized)
		last = end
	}

	builder.WriteString(xmlText[last:])
	return builder.String(), nil
}

func normalizeDocxTableToMaxWidth(tableXML string, maxWidth int) (string, error) {
	if maxWidth <= 0 || tableXML == "" {
		return tableXML, nil
	}

	gridWidths := extractDocxGridColumnWidths(tableXML)
	gridTotal := sumDocxWidths(gridWidths)

	tableWidth, tableWidthType, hasTableWidth := extractDocxTableWidth(tableXML)
	needsWidthClamp := false
	switch tableWidthType {
	case "pct":
		needsWidthClamp = tableWidth > 5000
	case "dxa":
		needsWidthClamp = tableWidth > maxWidth
	}

	scaleRatio := 1.0
	if gridTotal > maxWidth {
		scaleRatio = float64(maxWidth) / float64(gridTotal)
	}
	if scaleRatio >= 1.0 && !needsWidthClamp {
		return tableXML, nil
	}

	updated := tableXML
	var err error
	if scaleRatio < 1.0 {
		scaledGrid := scaleDocxWidthsToTotal(gridWidths, maxWidth)
		updated, err = replaceDocxSequentialTagWidths(updated, "w:gridCol", scaledGrid)
		if err != nil {
			return "", err
		}
		updated, err = scaleDocxCellWidths(updated, scaleRatio)
		if err != nil {
			return "", err
		}
	}

	if scaleRatio < 1.0 || needsWidthClamp || !hasTableWidth {
		updated, err = upsertDocxTableWidth(updated, maxWidth)
		if err != nil {
			return "", err
		}
	}

	return updated, nil
}

func extractDocxGridColumnWidths(tableXML string) []int {
	pattern := regexp.MustCompile(`(?s)<w:gridCol\b[^>]*/>`)
	tags := pattern.FindAllString(tableXML, -1)
	widths := make([]int, 0, len(tags))
	for _, tagXML := range tags {
		width, ok := extractDocxTagAttrInt(tagXML, "w:w")
		if !ok || width <= 0 {
			continue
		}
		widths = append(widths, width)
	}
	return widths
}

func extractDocxTableWidth(tableXML string) (int, string, bool) {
	tagXML := regexp.MustCompile(`(?s)<w:tblW\b[^>]*/>`).FindString(tableXML)
	if tagXML == "" {
		return 0, "", false
	}

	width, ok := extractDocxTagAttrInt(tagXML, "w:w")
	if !ok {
		return 0, "", false
	}
	return width, extractDocxTagAttr(tagXML, "w:type"), true
}

func replaceDocxSequentialTagWidths(xmlText, tag string, widths []int) (string, error) {
	if len(widths) == 0 {
		return xmlText, nil
	}
	index := 0
	return replaceDocxEmptyTags(xmlText, tag, func(tagXML string) (string, error) {
		if index >= len(widths) {
			return tagXML, nil
		}
		updated, err := setDocxTagAttr(tagXML, "w:w", strconv.Itoa(widths[index]))
		if err != nil {
			return "", err
		}
		index++
		return updated, nil
	})
}

func scaleDocxCellWidths(tableXML string, ratio float64) (string, error) {
	return replaceDocxEmptyTags(tableXML, "w:tcW", func(tagXML string) (string, error) {
		width, ok := extractDocxTagAttrInt(tagXML, "w:w")
		if !ok || width <= 0 {
			return tagXML, nil
		}

		widthType := extractDocxTagAttr(tagXML, "w:type")
		switch widthType {
		case "", "dxa":
			return setDocxTagAttr(tagXML, "w:w", strconv.Itoa(scaleDocxWidth(width, ratio)))
		case "pct":
			scaled := scaleDocxWidth(width, ratio)
			if scaled > 5000 {
				scaled = 5000
			}
			return setDocxTagAttr(tagXML, "w:w", strconv.Itoa(scaled))
		default:
			return tagXML, nil
		}
	})
}

func upsertDocxTableWidth(tableXML string, maxWidth int) (string, error) {
	newTableWidth := `<w:tblW w:w="` + strconv.Itoa(maxWidth) + `" w:type="dxa"/>`

	tblPrPattern := regexp.MustCompile(`(?s)<w:tblPr\b[^>]*>.*?</w:tblPr>`)
	if loc := tblPrPattern.FindStringIndex(tableXML); loc != nil {
		tblPrXML := tableXML[loc[0]:loc[1]]
		tblWPattern := regexp.MustCompile(`(?s)<w:tblW\b[^>]*/>`)
		if inner := tblWPattern.FindStringIndex(tblPrXML); inner != nil {
			tblPrXML = tblPrXML[:inner[0]] + newTableWidth + tblPrXML[inner[1]:]
		} else {
			insertIndex := strings.Index(tblPrXML, ">")
			if insertIndex == -1 {
				return "", fmt.Errorf("table properties missing >")
			}
			insertIndex++
			tblPrXML = tblPrXML[:insertIndex] + newTableWidth + tblPrXML[insertIndex:]
		}
		return tableXML[:loc[0]] + tblPrXML + tableXML[loc[1]:], nil
	}

	tableStart := regexp.MustCompile(`(?s)<w:tbl\b[^>]*>`).FindStringIndex(tableXML)
	if tableStart == nil {
		return "", fmt.Errorf("table missing <w:tbl>")
	}
	insertIndex := tableStart[1]
	return tableXML[:insertIndex] + `<w:tblPr>` + newTableWidth + `</w:tblPr>` + tableXML[insertIndex:], nil
}

func replaceDocxEmptyTags(xmlText, tag string, update func(tagXML string) (string, error)) (string, error) {
	pattern := regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(tag) + `\b[^>]*/>`)
	matches := pattern.FindAllStringIndex(xmlText, -1)
	if len(matches) == 0 {
		return xmlText, nil
	}

	var builder strings.Builder
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		builder.WriteString(xmlText[last:start])

		updated, err := update(xmlText[start:end])
		if err != nil {
			return "", err
		}
		builder.WriteString(updated)
		last = end
	}

	builder.WriteString(xmlText[last:])
	return builder.String(), nil
}

func extractDocxTagAttr(tagXML, attr string) string {
	pattern := regexp.MustCompile(regexp.QuoteMeta(attr) + `="([^"]*)"`)
	match := pattern.FindStringSubmatch(tagXML)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func extractDocxTagAttrInt(tagXML, attr string) (int, bool) {
	value := extractDocxTagAttr(tagXML, attr)
	if value == "" {
		return 0, false
	}
	width, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return width, true
}

func setDocxTagAttr(tagXML, attr, value string) (string, error) {
	pattern := regexp.MustCompile(`(` + regexp.QuoteMeta(attr) + `=")([^"]*)(")`)
	loc := pattern.FindStringSubmatchIndex(tagXML)
	if loc == nil {
		insertIndex := strings.LastIndex(tagXML, "/>")
		if insertIndex == -1 {
			return "", fmt.Errorf("tag %q missing self-closing suffix", tagXML)
		}
		return tagXML[:insertIndex] + ` ` + attr + `="` + value + `"` + tagXML[insertIndex:], nil
	}
	return tagXML[:loc[2]] + value + tagXML[loc[3]:], nil
}

func scaleDocxWidthsToTotal(widths []int, targetTotal int) []int {
	if len(widths) == 0 {
		return nil
	}
	currentTotal := sumDocxWidths(widths)
	if currentTotal <= 0 || currentTotal <= targetTotal {
		return append([]int(nil), widths...)
	}

	scaled := make([]int, len(widths))
	remainingSource := currentTotal
	remainingTarget := targetTotal
	for i, width := range widths {
		if i == len(widths)-1 {
			scaled[i] = remainingTarget
			break
		}
		next := int(float64(width)*float64(remainingTarget)/float64(remainingSource) + 0.5)
		if next < 1 && width > 0 {
			next = 1
		}
		if next > remainingTarget {
			next = remainingTarget
		}
		scaled[i] = next
		remainingSource -= width
		remainingTarget -= next
	}
	return scaled
}

func sumDocxWidths(widths []int) int {
	total := 0
	for _, width := range widths {
		total += width
	}
	return total
}

func scaleDocxWidth(width int, ratio float64) int {
	if width <= 0 || ratio <= 0 {
		return width
	}
	scaled := int(float64(width)*ratio + 0.5)
	if scaled < 1 {
		return 1
	}
	return scaled
}
