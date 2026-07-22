package paperparse

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) Parse(ctx context.Context, docPath string) (*ParsedPaper, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(docPath) == "" {
		return nil, errors.New("docPath is empty")
	}

	pkg, err := ooxmlpkg.Open(docPath)
	if err != nil {
		return nil, fmt.Errorf("open docx: %w", err)
	}

	documentXML, ok := pkg.Get("word/document.xml")
	if !ok {
		return nil, errors.New("word/document.xml not found")
	}

	elements, err := extractBodyElements(ctx, documentXML)
	if err != nil {
		return nil, err
	}

	styleLevels := headingStyleLevels(pkg)
	paper := parseElementsWithStyles(elements, styleLevels)
	for key, value := range extractCoverFieldsFromTables(ctx, documentXML) {
		if _, exists := paper.CoverFields[key]; !exists {
			paper.CoverFields[key] = value
		}
	}
	return paper, nil
}

type section int

const (
	sectionCover section = iota
	sectionAbstractCN
	sectionKeywordsCN
	sectionBody
	sectionReferences
	sectionAcknowledgements
)

var headingPattern = regexp.MustCompile(`^(\d+(?:\.\d+)*)(?:\.)?[\s　]+(.+)$`)
var chineseChapterHeadingPattern = regexp.MustCompile(`^第[一二三四五六七八九十百千万\d]+章\s*(.*)$`)
var chineseListHeadingPattern = regexp.MustCompile(`^[一二三四五六七八九十百]+[、．.]\s*(.+)$`)
var paragraphStylePattern = regexp.MustCompile(`<w:pStyle\b[^>]*\bw:val="([^"]+)"`)
var paragraphOutlinePattern = regexp.MustCompile(`<w:outlineLvl\b[^>]*\bw:val="(\d+)"`)
var bodyElementPattern = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>|<w:tbl(?:\s[^>]*)?>.*?</w:tbl>`)
var ooxmlTextElementPattern = regexp.MustCompile(`(?s)<w:t(?:\s[^>]*)?>(.*?)</w:t>`)
var ooxmlTagPattern = regexp.MustCompile(`(?s)<[^>]+>`)
var rejectedReviewMarkupPattern = regexp.MustCompile(`(?s)<w:(?:del|moveFrom)\b[^>]*>.*?</w:(?:del|moveFrom)>`)

type bodyElement struct {
	kind string
	text string
	xml  string
}

func parseParagraphs(paragraphs []string) *ParsedPaper {
	elements := make([]bodyElement, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		elements = append(elements, bodyElement{kind: "p", text: paragraph})
	}
	return parseElements(elements)
}

func parseElements(elements []bodyElement) *ParsedPaper {
	return parseElementsWithStyles(elements, nil)
}

func parseElementsWithStyles(elements []bodyElement, styleLevels map[string]int) *ParsedPaper {
	paper := &ParsedPaper{
		CoverFields: make(map[string]string),
	}
	current := sectionCover

	for _, element := range elements {
		text := strings.TrimSpace(element.text)
		if text == "" {
			continue
		}
		if element.kind == "tbl" {
			if current == sectionBody {
				appendContentBlockWithXML(paper, "table", 0, text, element.xml)
			}
			continue
		}

		if content, ok := splitSectionMarker(text, "摘要"); ok {
			current = sectionAbstractCN
			appendContentBlock(paper, "section_label", 0, text)
			if content != "" {
				paper.AbstractCN = append(paper.AbstractCN, content)
			}
			continue
		}
		if content, ok := splitSectionMarker(text, "关键词"); ok {
			appendContentBlock(paper, "section_label", 0, text)
			if content == "" {
				current = sectionKeywordsCN
			} else {
				paper.KeywordsCN = parseKeywords(content)
				current = sectionBody
			}
			continue
		}
		if content, ok := splitSectionMarker(text, "参考文献"); ok {
			current = sectionReferences
			appendContentBlock(paper, "section_label", 0, text)
			if content != "" {
				paper.References = append(paper.References, content)
			}
			continue
		}
		if content, ok := splitSectionMarker(text, "致谢"); ok {
			current = sectionAcknowledgements
			appendContentBlock(paper, "section_label", 0, text)
			if content != "" {
				paper.Acknowledgements = append(paper.Acknowledgements, content)
			}
			continue
		}

		if current != sectionReferences && current != sectionAcknowledgements {
			heading, ok := parseHeading(text)
			if !ok {
				if level := paragraphHeadingLevel(element.xml, styleLevels); level > 0 {
					heading = Heading{Level: level, Text: text}
					ok = true
				}
			}
			if ok {
				paper.Headings = append(paper.Headings, heading)
				appendContentBlock(paper, "heading", heading.Level, text)
				current = sectionBody
				continue
			}
		}

		switch current {
		case sectionCover:
			if key, value, ok := parseCoverField(text); ok {
				paper.CoverFields[key] = value
				continue
			}
			continue
		case sectionAbstractCN:
			paper.AbstractCN = append(paper.AbstractCN, text)
			appendContentBlock(paper, "abstract_cn", 0, text)
		case sectionKeywordsCN:
			paper.KeywordsCN = parseKeywords(text)
			appendContentBlock(paper, "keywords_cn", 0, text)
			current = sectionBody
		case sectionReferences:
			paper.References = append(paper.References, text)
			appendContentBlock(paper, "references", 0, text)
		case sectionAcknowledgements:
			paper.Acknowledgements = append(paper.Acknowledgements, text)
			appendContentBlock(paper, "acknowledgement", 0, text)
		default:
			paper.Body = append(paper.Body, text)
			appendContentBlock(paper, "body", 0, text)
		}
	}

	return paper
}

type wordStyle struct {
	name     string
	basedOn  string
	outline  int
	hasLevel bool
}

func headingStyleLevels(pkg *ooxmlpkg.DocxPackage) map[string]int {
	content, ok := pkg.Get("word/styles.xml")
	if !ok {
		return nil
	}
	styles := parseWordStyles(content)
	levels := make(map[string]int, len(styles))
	var resolve func(string, map[string]bool, int) int
	resolve = func(id string, seen map[string]bool, depth int) int {
		if depth > 64 {
			return 0
		}
		if level := levels[id]; level > 0 {
			return level
		}
		if seen[id] {
			return 0
		}
		seen[id] = true
		style, ok := styles[id]
		if !ok {
			return headingLevelFromName(id)
		}
		level := 0
		if style.hasLevel {
			level = style.outline + 1
		}
		if level == 0 {
			level = headingLevelFromName(style.name)
		}
		if level == 0 {
			level = headingLevelFromName(id)
		}
		if level == 0 && style.basedOn != "" {
			level = resolve(style.basedOn, seen, depth+1)
		}
		if level >= 1 && level <= 9 {
			levels[id] = level
			return level
		}
		return 0
	}
	for id := range styles {
		resolve(id, map[string]bool{}, 0)
	}
	return levels
}

func parseWordStyles(content []byte) map[string]wordStyle {
	styles := make(map[string]wordStyle)
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var current wordStyle
	styleID := ""
	insideStyle := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return styles
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "style":
				insideStyle = true
				current = wordStyle{}
				styleID = xmlAttr(value.Attr, "styleId")
			case "name":
				if insideStyle {
					current.name = xmlAttr(value.Attr, "val")
				}
			case "basedOn":
				if insideStyle {
					current.basedOn = xmlAttr(value.Attr, "val")
				}
			case "outlineLvl":
				if insideStyle {
					if outline, parseErr := strconv.Atoi(xmlAttr(value.Attr, "val")); parseErr == nil && outline >= 0 && outline <= 8 {
						current.outline = outline
						current.hasLevel = true
					}
				}
			}
		case xml.EndElement:
			if value.Name.Local == "style" {
				if styleID != "" {
					styles[styleID] = current
				}
				insideStyle = false
				styleID = ""
			}
		}
	}
	return styles
}

func xmlAttr(attrs []xml.Attr, name string) string {
	for _, attr := range attrs {
		if attr.Name.Local == name {
			return strings.TrimSpace(attr.Value)
		}
	}
	return ""
}

func paragraphHeadingLevel(paragraphXML string, styleLevels map[string]int) int {
	if match := paragraphOutlinePattern.FindStringSubmatch(paragraphXML); len(match) == 2 {
		if outline, err := strconv.Atoi(match[1]); err == nil && outline >= 0 && outline <= 8 {
			return outline + 1
		}
	}
	match := paragraphStylePattern.FindStringSubmatch(paragraphXML)
	if len(match) != 2 {
		return 0
	}
	if level := styleLevels[match[1]]; level > 0 {
		return level
	}
	return headingLevelFromName(match[1])
}

func headingLevelFromName(name string) int {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(normalized)
	chineseLevels := []string{"一", "二", "三", "四", "五", "六", "七", "八", "九"}
	for level := 1; level <= 9; level++ {
		if normalized == "heading"+strconv.Itoa(level) ||
			normalized == "标题"+strconv.Itoa(level) ||
			normalized == "标题"+chineseLevels[level-1] {
			return level
		}
	}
	return 0
}

func appendContentBlock(paper *ParsedPaper, kind string, level int, text string) {
	appendContentBlockWithXML(paper, kind, level, text, "")
}

func appendContentBlockWithXML(paper *ParsedPaper, kind string, level int, text string, rawXML string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	paper.ContentBlocks = append(paper.ContentBlocks, ContentBlock{Kind: kind, Level: level, Text: text, XML: strings.TrimSpace(rawXML)})
}

func extractBodyElements(ctx context.Context, content []byte) ([]bodyElement, error) {
	bodyXML := string(content)
	start := strings.Index(bodyXML, "<w:body")
	if start < 0 {
		return nil, errors.New("word/document.xml body not found")
	}
	openEnd := strings.Index(bodyXML[start:], ">")
	if openEnd < 0 {
		return nil, errors.New("word/document.xml body start is malformed")
	}
	bodyStart := start + openEnd + 1
	bodyEnd := strings.LastIndex(bodyXML, "</w:body>")
	if bodyEnd < bodyStart {
		return nil, errors.New("word/document.xml body end not found")
	}

	var elements []bodyElement
	for _, match := range bodyElementPattern.FindAllString(bodyXML[bodyStart:bodyEnd], -1) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text, err := textFromOOXMLFragment([]byte(match))
		if err != nil {
			text = looseTextFromOOXMLFragment(match)
		}
		kind := "p"
		if strings.HasPrefix(match, "<w:tbl") {
			kind = "tbl"
		}
		elements = append(elements, bodyElement{
			kind: kind,
			text: strings.TrimSpace(text),
			xml:  match,
		})
	}
	return elements, nil
}

func looseTextFromOOXMLFragment(fragment string) string {
	fragment = rejectedReviewMarkupPattern.ReplaceAllString(fragment, "")
	texts := ooxmlTextElementPattern.FindAllStringSubmatch(fragment, -1)
	var builder strings.Builder
	for _, text := range texts {
		if len(text) < 2 {
			continue
		}
		builder.WriteString(ooxmlTagPattern.ReplaceAllString(text[1], ""))
	}
	return html.UnescapeString(builder.String())
}

func textFromOOXMLFragment(content []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var builder strings.Builder
	inText := false
	rejectedDepth := 0

	for {
		token, err := decoder.Token()
		if err == nil {
			switch value := token.(type) {
			case xml.StartElement:
				switch value.Name.Local {
				case "del", "moveFrom":
					rejectedDepth++
				case "t":
					if rejectedDepth == 0 {
						inText = true
					}
				case "tab":
					if rejectedDepth == 0 {
						builder.WriteByte('\t')
					}
				case "br":
					if rejectedDepth == 0 {
						builder.WriteByte('\n')
					}
				}
			case xml.EndElement:
				if (value.Name.Local == "del" || value.Name.Local == "moveFrom") && rejectedDepth > 0 {
					rejectedDepth--
					inText = false
				} else if value.Name.Local == "t" {
					inText = false
				}
			case xml.CharData:
				if inText && rejectedDepth == 0 {
					builder.Write([]byte(value))
				}
			}
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return "", fmt.Errorf("parse OOXML fragment: %w", err)
	}
	return builder.String(), nil
}

func extractParagraphs(ctx context.Context, content []byte) ([]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var paragraphs []string
	var builder strings.Builder
	inParagraph := false
	inText := false

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		token, err := decoder.Token()
		if err == nil {
			switch value := token.(type) {
			case xml.StartElement:
				switch value.Name.Local {
				case "p":
					inParagraph = true
					builder.Reset()
				case "t":
					if inParagraph {
						inText = true
					}
				case "tab":
					if inParagraph {
						builder.WriteByte('\t')
					}
				case "br":
					if inParagraph {
						builder.WriteByte('\n')
					}
				}
			case xml.EndElement:
				switch value.Name.Local {
				case "p":
					if inParagraph {
						paragraphs = append(paragraphs, strings.TrimSpace(builder.String()))
					}
					inParagraph = false
					inText = false
				case "t":
					inText = false
				}
			case xml.CharData:
				if inParagraph && inText {
					builder.Write([]byte(value))
				}
			}
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return nil, fmt.Errorf("parse document.xml: %w", err)
	}

	return paragraphs, nil
}

func extractCoverFieldsFromTables(ctx context.Context, content []byte) map[string]string {
	fields := make(map[string]string)
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var row []string
	var cell strings.Builder
	inRow := false
	inCell := false
	inText := false
	lastKey := ""

	for {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return fields
			}
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			break
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "tr":
				inRow = true
				row = nil
			case "tc":
				if inRow {
					inCell = true
					cell.Reset()
				}
			case "t":
				if inCell {
					inText = true
				}
			case "tab":
				if inCell {
					cell.WriteByte('\t')
				}
			case "br":
				if inCell {
					cell.WriteByte('\n')
				}
			}
		case xml.EndElement:
			switch value.Name.Local {
			case "t":
				inText = false
			case "tc":
				if inCell {
					row = append(row, strings.TrimSpace(cell.String()))
				}
				inCell = false
				inText = false
			case "tr":
				values := nonEmptyCells(row)
				if len(values) >= 2 {
					fields[values[0]] = strings.Join(values[1:], " ")
					lastKey = values[0]
				} else if len(values) == 1 && lastKey == "题目" {
					fields["题目续行"] = values[0]
				}
				inRow = false
			}
		case xml.CharData:
			if inCell && inText {
				cell.Write([]byte(value))
			}
		}
	}
	return fields
}

func nonEmptyCells(row []string) []string {
	values := make([]string, 0, len(row))
	for _, cell := range row {
		if value := strings.TrimSpace(cell); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func parseKeywords(text string) []string {
	content := strings.TrimSpace(text)
	fields := strings.FieldsFunc(content, func(r rune) bool {
		return r == '，' || r == '、' || r == '；' || r == ';' || r == ','
	})

	keywords := make([]string, 0, len(fields))
	for _, field := range fields {
		keyword := strings.TrimSpace(field)
		if keyword != "" {
			keywords = append(keywords, keyword)
		}
	}
	return keywords
}

func splitSectionMarker(text string, marker string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == marker {
		return "", true
	}
	if !strings.HasPrefix(trimmed, marker) {
		return "", false
	}

	remainder := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
	if remainder == "" {
		return "", true
	}

	remainder = strings.TrimSpace(remainder)
	if strings.HasPrefix(remainder, "：") {
		return strings.TrimSpace(strings.TrimPrefix(remainder, "：")), true
	}
	if strings.HasPrefix(remainder, ":") {
		return strings.TrimSpace(strings.TrimPrefix(remainder, ":")), true
	}
	if strings.HasPrefix(remainder, "；") || strings.HasPrefix(remainder, ";") {
		return strings.TrimSpace(strings.TrimLeft(remainder, "；;")), true
	}

	return "", false
}

func parseHeading(text string) (Heading, bool) {
	matches := headingPattern.FindStringSubmatch(text)
	if matches != nil {
		level := strings.Count(matches[1], ".") + 1
		return Heading{Level: level, Text: strings.TrimSpace(matches[2])}, true
	}
	for _, pattern := range []*regexp.Regexp{chineseChapterHeadingPattern, chineseListHeadingPattern} {
		if matches := pattern.FindStringSubmatch(strings.TrimSpace(text)); len(matches) == 2 {
			label := strings.TrimSpace(matches[1])
			if label == "" {
				label = strings.TrimSpace(text)
			}
			return Heading{Level: 1, Text: label}, true
		}
	}
	return Heading{}, false
}

func parseCoverField(text string) (string, string, bool) {
	key, value, ok := strings.Cut(text, "：")
	if !ok {
		key, value, ok = strings.Cut(text, ":")
	}
	if !ok {
		return "", "", false
	}

	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return "", "", false
	}
	return key, value, true
}
