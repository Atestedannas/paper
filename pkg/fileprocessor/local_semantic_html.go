package fileprocessor

import (
	"context"
	"fmt"
	htmlstd "html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/color"
	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
	nethtml "golang.org/x/net/html"
)

type LocalSemanticHTMLConverter struct{}

type semanticHTMLBlock struct {
	Kind  string
	Level int
	Text  string
	Table [][]string
}

func NewLocalSemanticHTMLConverter() *LocalSemanticHTMLConverter {
	return &LocalSemanticHTMLConverter{}
}

func (c *LocalSemanticHTMLConverter) ConvertDocxToHTML(_ context.Context, inputPath, outputPath string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("local semantic html converter is nil")
	}
	pkg, err := openDocxPackage(inputPath)
	if err != nil {
		return "", fmt.Errorf("open docx package: %w", err)
	}

	documentXML, ok := pkg.entries["word/document.xml"]
	if !ok {
		return "", fmt.Errorf("word/document.xml not found in %s", inputPath)
	}
	bodyXML := extractDocxBodyXML(string(documentXML))
	blocks := extractTopLevelDocxBodyBlocks(bodyXML)

	var html strings.Builder
	html.WriteString("<html><head><meta charset=\"utf-8\"><style>")
	html.WriteString("body{font-family:'Songti SC','SimSun','Times New Roman',serif;line-height:1.6;}h1,h2,h3,h4{font-weight:700;}table{border-collapse:collapse;margin:12px 0;}td,th{border:1px solid #444;padding:4px 8px;vertical-align:top;}")
	html.WriteString("</style></head><body>")

	for _, block := range blocks {
		switch block.Kind {
		case "heading":
			level := block.Level
			if level < 1 || level > 4 {
				level = 1
			}
			fmt.Fprintf(&html, "<h%d>%s</h%d>", level, htmlstd.EscapeString(block.Text), level)
		case "paragraph":
			fmt.Fprintf(&html, "<p>%s</p>", htmlstd.EscapeString(block.Text))
		case "table":
			html.WriteString("<table>")
			for _, row := range block.Table {
				html.WriteString("<tr>")
				for _, cell := range row {
					fmt.Fprintf(&html, "<td>%s</td>", htmlstd.EscapeString(cell))
				}
				html.WriteString("</tr>")
			}
			html.WriteString("</table>")
		}
	}

	html.WriteString("</body></html>")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return "", fmt.Errorf("create html output dir: %w", err)
	}
	if err := os.WriteFile(outputPath, []byte(html.String()), 0644); err != nil {
		return "", fmt.Errorf("write html output: %w", err)
	}
	return outputPath, nil
}

func (c *LocalSemanticHTMLConverter) ConvertHTMLToDocx(_ context.Context, inputPath, outputPath string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("local semantic html converter is nil")
	}
	htmlBytes, err := os.ReadFile(inputPath)
	if err != nil {
		return "", fmt.Errorf("read html input: %w", err)
	}

	doc := document.New()
	doc.Settings.SetUpdateFieldsOnOpen(true)

	blocks := parseSemanticHTMLBlocks(string(htmlBytes))
	if len(blocks) == 0 {
		blocks = []semanticHTMLBlock{{Kind: "paragraph", Text: strings.TrimSpace(string(htmlBytes))}}
	}

	for _, block := range blocks {
		switch block.Kind {
		case "heading":
			writeSemanticHeading(doc, block)
		case "table":
			writeSemanticTable(doc, block.Table)
		default:
			writeSemanticParagraph(doc, block.Text)
		}
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return "", fmt.Errorf("create docx output dir: %w", err)
	}
	if err := doc.SaveToFile(outputPath); err != nil {
		return "", fmt.Errorf("save local semantic docx: %w", err)
	}
	return outputPath, nil
}

func extractTopLevelDocxBodyBlocks(bodyXML string) []semanticHTMLBlock {
	blocks := make([]semanticHTMLBlock, 0, 32)
	for cursor := 0; cursor < len(bodyXML); {
		idx, kind := nextDocxBodyElement(bodyXML, cursor)
		if idx < 0 {
			break
		}
		cursor = idx

		switch kind {
		case "w:p":
			xmlText, end, ok := sliceDocxElement(bodyXML, cursor, kind)
			if !ok {
				cursor++
				continue
			}
			cursor = end
			text := strings.TrimSpace(normalizeVisibleText(extractDocxText(xmlText)))
			if text == "" {
				continue
			}
			level, isHeading := detectSemanticHeadingLevel(text)
			if isHeading {
				blocks = append(blocks, semanticHTMLBlock{Kind: "heading", Level: level, Text: text})
				continue
			}
			blocks = append(blocks, semanticHTMLBlock{Kind: "paragraph", Text: text})
		case "w:tbl":
			xmlText, end, ok := sliceDocxElement(bodyXML, cursor, kind)
			if !ok {
				cursor++
				continue
			}
			cursor = end
			grid := extractDocxTableCellTextGrid(xmlText)
			if len(grid) == 0 {
				continue
			}
			blocks = append(blocks, semanticHTMLBlock{Kind: "table", Table: grid})
		case "w:sectPr":
			_, end, ok := sliceDocxElement(bodyXML, cursor, kind)
			if !ok {
				cursor++
				continue
			}
			cursor = end
		default:
			cursor++
		}
	}
	return blocks
}

func nextDocxBodyElement(bodyXML string, offset int) (int, string) {
	candidates := []struct {
		token string
		kind  string
	}{
		{"<w:p", "w:p"},
		{"<w:tbl", "w:tbl"},
		{"<w:sectPr", "w:sectPr"},
	}

	bestIdx := -1
	bestKind := ""
	for _, candidate := range candidates {
		idx := strings.Index(bodyXML[offset:], candidate.token)
		if idx < 0 {
			continue
		}
		idx += offset
		if bestIdx == -1 || idx < bestIdx {
			bestIdx = idx
			bestKind = candidate.kind
		}
	}
	return bestIdx, bestKind
}

func sliceDocxElement(xmlText string, start int, tag string) (string, int, bool) {
	if start < 0 || start >= len(xmlText) {
		return "", start, false
	}
	openEnd := strings.Index(xmlText[start:], ">")
	if openEnd < 0 {
		return "", start, false
	}
	openEnd += start
	if xmlText[openEnd-1] == '/' {
		return xmlText[start : openEnd+1], openEnd + 1, true
	}

	closeTag := "</" + tag + ">"
	closeIdx := strings.Index(xmlText[openEnd+1:], closeTag)
	if closeIdx < 0 {
		return "", start, false
	}
	closeIdx += openEnd + 1
	end := closeIdx + len(closeTag)
	return xmlText[start:end], end, true
}

var semanticHeadingNumberPattern = regexp.MustCompile(`^(\d+(?:\.\d+){0,3})\s+\S`)

func detectSemanticHeadingLevel(text string) (int, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}

	switch text {
	case "摘要", "Abstract", "目录", "参考文献", "致谢":
		return 1, true
	}

	match := semanticHeadingNumberPattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return 0, false
	}
	level := 1
	if number := strings.TrimSpace(match[1]); number != "" {
		level = 1 + strings.Count(number, ".")
	}
	if level > 4 {
		level = 4
	}
	return level, true
}

func parseSemanticHTMLBlocks(input string) []semanticHTMLBlock {
	root, err := nethtml.Parse(strings.NewReader(input))
	if err != nil {
		text := strings.TrimSpace(stripHTMLLikeText(input))
		if text == "" {
			return nil
		}
		return []semanticHTMLBlock{{Kind: "paragraph", Text: text}}
	}

	body := findSemanticHTMLBody(root)
	if body == nil {
		body = root
	}

	blocks := make([]semanticHTMLBlock, 0, 32)
	collectSemanticHTMLBlocks(body, &blocks)
	return compactSemanticHTMLBlocks(blocks)
}

func findSemanticHTMLBody(node *nethtml.Node) *nethtml.Node {
	if node == nil {
		return nil
	}
	if node.Type == nethtml.ElementNode && strings.EqualFold(node.Data, "body") {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findSemanticHTMLBody(child); found != nil {
			return found
		}
	}
	return nil
}

func collectSemanticHTMLBlocks(node *nethtml.Node, blocks *[]semanticHTMLBlock) {
	if node == nil {
		return
	}
	if node.Type == nethtml.ElementNode {
		tag := strings.ToLower(node.Data)
		switch tag {
		case "style", "script", "head":
			return
		case "h1", "h2", "h3", "h4":
			level, _ := strconv.Atoi(strings.TrimPrefix(tag, "h"))
			text := strings.TrimSpace(extractSemanticHTMLText(node))
			if text != "" {
				*blocks = append(*blocks, semanticHTMLBlock{Kind: "heading", Level: level, Text: text})
			}
			return
		case "p":
			text := strings.TrimSpace(extractSemanticHTMLText(node))
			if text != "" {
				*blocks = append(*blocks, semanticHTMLBlock{Kind: "paragraph", Text: text})
			}
			return
		case "table":
			if grid := extractSemanticHTMLTable(node); len(grid) > 0 {
				*blocks = append(*blocks, semanticHTMLBlock{Kind: "table", Table: grid})
			}
			return
		case "ul", "ol":
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type != nethtml.ElementNode || !strings.EqualFold(child.Data, "li") {
					continue
				}
				text := strings.TrimSpace(extractSemanticHTMLText(child))
				if text == "" {
					continue
				}
				prefix := "• "
				if tag == "ol" {
					prefix = ""
				}
				*blocks = append(*blocks, semanticHTMLBlock{Kind: "paragraph", Text: prefix + text})
			}
			return
		case "div", "section", "article", "main":
			if !hasSemanticBlockChildren(node) {
				text := strings.TrimSpace(extractSemanticHTMLText(node))
				if text != "" {
					*blocks = append(*blocks, semanticHTMLBlock{Kind: "paragraph", Text: text})
					return
				}
			}
		}
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectSemanticHTMLBlocks(child, blocks)
	}
}

func hasSemanticBlockChildren(node *nethtml.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != nethtml.ElementNode {
			continue
		}
		switch strings.ToLower(child.Data) {
		case "p", "h1", "h2", "h3", "h4", "table", "ul", "ol", "div", "section", "article":
			return true
		}
	}
	return false
}

func extractSemanticHTMLText(node *nethtml.Node) string {
	if node == nil {
		return ""
	}
	switch node.Type {
	case nethtml.TextNode:
		return node.Data
	case nethtml.ElementNode:
		switch strings.ToLower(node.Data) {
		case "br":
			return "\n"
		case "style", "script":
			return ""
		}
	}

	var text strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(extractSemanticHTMLText(child))
	}
	return strings.Join(strings.Fields(text.String()), " ")
}

func extractSemanticHTMLTable(table *nethtml.Node) [][]string {
	rows := make([][]string, 0, 4)
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		if node == nil {
			return
		}
		if node.Type == nethtml.ElementNode && strings.EqualFold(node.Data, "tr") {
			row := make([]string, 0, 4)
			for cell := node.FirstChild; cell != nil; cell = cell.NextSibling {
				if cell.Type != nethtml.ElementNode {
					continue
				}
				if !strings.EqualFold(cell.Data, "td") && !strings.EqualFold(cell.Data, "th") {
					continue
				}
				row = append(row, strings.TrimSpace(extractSemanticHTMLText(cell)))
			}
			if len(row) > 0 {
				rows = append(rows, row)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(table)
	return rows
}

func compactSemanticHTMLBlocks(blocks []semanticHTMLBlock) []semanticHTMLBlock {
	result := make([]semanticHTMLBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Kind {
		case "table":
			if len(block.Table) == 0 {
				continue
			}
		default:
			block.Text = strings.TrimSpace(block.Text)
			if block.Text == "" {
				continue
			}
		}
		result = append(result, block)
	}
	return result
}

func stripHTMLLikeText(input string) string {
	re := regexp.MustCompile(`(?s)<[^>]+>`)
	return htmlstd.UnescapeString(re.ReplaceAllString(input, " "))
}

func writeSemanticHeading(doc *document.Document, block semanticHTMLBlock) {
	para := doc.AddParagraph()
	level := block.Level
	if level < 1 || level > 4 {
		level = 1
	}
	para.SetStyle(fmt.Sprintf("Heading%d", level))
	props := para.Properties()
	props.SetAlignment(wml.ST_JcLeft)
	props.SetSpacing(measurement.Zero, measurement.Zero)
	para.SetLineSpacing(18*measurement.Point, wml.ST_LineSpacingRuleAuto)

	run := para.AddRun()
	run.AddText(block.Text)
	runProps := run.Properties()
	runProps.SetBold(true)
	runProps.SetFontFamily("宋体")
	runProps.SetSize(semanticHeadingSize(level))
}

func writeSemanticParagraph(doc *document.Document, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	para := doc.AddParagraph()
	props := para.Properties()
	props.SetAlignment(wml.ST_JcBoth)
	props.SetFirstLineIndent(2 * measurement.Character)
	props.SetSpacing(measurement.Zero, measurement.Zero)
	para.SetLineSpacing(18*measurement.Point, wml.ST_LineSpacingRuleAuto)

	run := para.AddRun()
	run.AddText(text)
	runProps := run.Properties()
	runProps.SetFontFamily("宋体")
	runProps.SetSize(12 * measurement.Point)
}

func writeSemanticTable(doc *document.Document, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	table := doc.AddTable()
	borders := table.Properties().Borders()
	borders.SetAll(wml.ST_BorderSingle, color.Black, measurement.Point)

	for _, rowValues := range rows {
		row := table.AddRow()
		for _, cellValue := range rowValues {
			cell := row.AddCell()
			para := cell.AddParagraph()
			props := para.Properties()
			props.SetAlignment(wml.ST_JcCenter)
			props.SetSpacing(measurement.Zero, measurement.Zero)

			run := para.AddRun()
			run.AddText(cellValue)
			runProps := run.Properties()
			runProps.SetFontFamily("宋体")
			runProps.SetSize(10.5 * measurement.Point)
		}
	}
}

func semanticHeadingSize(level int) measurement.Distance {
	switch level {
	case 1:
		return 16 * measurement.Point
	case 2:
		return 15 * measurement.Point
	case 3, 4:
		return 14 * measurement.Point
	default:
		return 16 * measurement.Point
	}
}
