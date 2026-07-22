package cqrwst

import (
	"regexp"
	"strconv"
	"strings"
)

type semanticBlockKind string

const (
	semanticBlockFrontMatter      semanticBlockKind = "frontmatter"
	semanticBlockTOC              semanticBlockKind = "toc"
	semanticBlockBodyHeading      semanticBlockKind = "body_heading"
	semanticBlockBodyParagraph    semanticBlockKind = "body_paragraph"
	semanticBlockFigure           semanticBlockKind = "figure"
	semanticBlockLayoutTable      semanticBlockKind = "layout_table"
	semanticBlockDataTable        semanticBlockKind = "data_table"
	semanticBlockTableCaption     semanticBlockKind = "table_caption"
	semanticBlockFigureCaption    semanticBlockKind = "figure_caption"
	semanticBlockReferencesTitle  semanticBlockKind = "references_title"
	semanticBlockReferenceEntry   semanticBlockKind = "reference_entry"
	semanticBlockAcknowledgements semanticBlockKind = "acknowledgements_title"
)

type semanticBlock struct {
	Index          int
	XML            string
	Text           string
	Kind           semanticBlockKind
	Chapter        string
	InBody         bool
	IsParagraph    bool
	IsTable        bool
	HasFigureShape bool
	Rows           int
	Cells          int
	AverageCellLen int
	HasMergedCells bool
}

var (
	tableRowPattern   = regexp.MustCompile(`(?s)<w:tr\b[^>]*>.*?</w:tr>`)
	tableCellPattern  = regexp.MustCompile(`(?s)<w:tc\b[^>]*>.*?</w:tc>`)
	tableMergePattern = regexp.MustCompile(`<w:(?:gridSpan|vMerge)\b`)
)

func buildSemanticBlocks(documentXML string) []semanticBlock {
	matches := documentBodyChildPattern.FindAllStringIndex(documentXML, -1)
	blocks := make([]semanticBlock, 0, len(matches))
	currentChapter := "1"
	inBody := false
	inTOC := false
	inReferences := false

	for index, match := range matches {
		child := documentXML[match[0]:match[1]]
		block := semanticBlock{
			Index:       index,
			XML:         child,
			Text:        strings.TrimSpace(extractParagraphText(child)),
			Chapter:     currentChapter,
			InBody:      inBody,
			IsParagraph: isParagraphXML(child),
			IsTable:     isTableXML(child),
		}
		block.HasFigureShape = block.IsParagraph && containsFigureShape(child)

		if block.IsTable {
			block.Rows = len(tableRowPattern.FindAllString(child, -1))
			block.Cells = len(tableCellPattern.FindAllString(child, -1))
			block.AverageCellLen = averageVisibleCellLength(child)
			block.HasMergedCells = tableMergePattern.MatchString(child)
		}

		normalizedText := normalizeChineseLabelText(block.Text)
		switch {
		case block.IsParagraph && isTOCParagraph(block.Text):
			inTOC = true
			block.Kind = semanticBlockTOC
		case block.IsParagraph && isBodyStartParagraph(block.Text):
			inTOC = false
			inReferences = false
			inBody = true
			if chapter, ok := chapterNumberFromHeading(block.Text); ok {
				currentChapter = chapter
			}
			block.InBody = true
			block.Chapter = currentChapter
			block.Kind = semanticBlockBodyHeading
		case block.IsParagraph && normalizedText == "\u53c2\u8003\u6587\u732e":
			inTOC = false
			inReferences = true
			block.InBody = true
			block.Kind = semanticBlockReferencesTitle
		case block.IsParagraph && isAcknowledgementsTitle(block.Text):
			inTOC = false
			inReferences = false
			block.InBody = true
			block.Kind = semanticBlockAcknowledgements
		case block.IsParagraph && inReferences && referenceEntryPattern.MatchString(block.Text):
			block.InBody = true
			block.Kind = semanticBlockReferenceEntry
		case block.IsParagraph && isTableCaption(block.Text):
			block.Kind = semanticBlockTableCaption
		case block.IsParagraph && isFigureCaption(block.Text):
			block.Kind = semanticBlockFigureCaption
		case block.IsParagraph && inTOC:
			block.Kind = semanticBlockTOC
		case block.IsParagraph && block.HasFigureShape && inBody:
			block.InBody = true
			block.Kind = semanticBlockFigure
		case block.IsParagraph && inBody:
			if chapter, ok := chapterNumberFromHeading(block.Text); ok {
				currentChapter = chapter
				block.Chapter = currentChapter
				block.Kind = semanticBlockBodyHeading
			} else {
				block.Chapter = currentChapter
				block.Kind = semanticBlockBodyParagraph
			}
			block.InBody = true
		case block.IsTable:
			block.InBody = inBody
			block.Chapter = currentChapter
			if shouldTreatAsDataTable(block) {
				block.Kind = semanticBlockDataTable
			} else {
				block.Kind = semanticBlockLayoutTable
			}
		default:
			block.Kind = semanticBlockFrontMatter
		}

		blocks = append(blocks, block)
	}
	return blocks
}

func semanticBlockByIndex(blocks []semanticBlock, index int) semanticBlock {
	if index >= 0 && index < len(blocks) && blocks[index].Index == index {
		return blocks[index]
	}
	for _, block := range blocks {
		if block.Index == index {
			return block
		}
	}
	return semanticBlock{Index: index}
}

func shouldTreatAsDataTable(block semanticBlock) bool {
	if !block.IsTable || !block.InBody {
		return false
	}
	score := 1
	if block.AverageCellLen > 15 {
		score += 2
	}
	if block.Rows > 8 {
		score++
	}
	if block.HasMergedCells {
		score -= 2
	}
	return score > 0
}

func containsCoverLayoutLabels(text string) bool {
	compact := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(text), " ", ""), "\t", "")
	if compact == "" {
		return false
	}
	labels := []string{
		"\u9898\u76ee", "\u5b66\u9662", "\u4e13\u4e1a", "\u73ed\u7ea7",
		"\u5b66\u53f7", "\u59d3\u540d", "\u6307\u5bfc\u6559\u5e08",
		"\u6559\u5e08", "\u5b66\u751f",
	}
	hits := 0
	for _, label := range labels {
		if strings.Contains(compact, label) {
			hits++
		}
	}
	return hits >= 2
}

func averageVisibleCellLength(tableXML string) int {
	cells := tableCellPattern.FindAllString(tableXML, -1)
	if len(cells) == 0 {
		return 0
	}
	total := 0
	visible := 0
	for _, cell := range cells {
		text := strings.TrimSpace(extractParagraphText(cell))
		if text == "" {
			continue
		}
		total += len([]rune(text))
		visible++
	}
	if visible == 0 {
		return 0
	}
	return total / visible
}

func tableCaptionNameFromContext(blocks []semanticBlock, index int) string {
	for i := index - 1; i >= 0 && index-i <= 8; i-- {
		block := semanticBlockByIndex(blocks, i)
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		if block.Kind == semanticBlockTableCaption {
			return trimCaptionLabelPrefix(text, "\u8868")
		}
		if block.Kind == semanticBlockBodyHeading {
			name := strings.TrimSpace(regexp.MustCompile(`^\d+(?:\.\d+)*\s+`).ReplaceAllString(text, ""))
			if strings.Contains(name, "\u8868") {
				return name
			}
		}
	}
	return "\u8868\u683c"
}

func numberedCaption(prefix string, chapter string, number int, name string) string {
	return prefix + chapter + "." + strconv.Itoa(number) + " " + strings.TrimSpace(name)
}
