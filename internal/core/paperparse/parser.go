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

	paper := parseElements(elements)
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
				if len(row) >= 2 {
					key := strings.TrimSpace(row[0])
					value := strings.TrimSpace(row[1])
					if key != "" && value != "" {
						fields[key] = value
						lastKey = key
					} else if key == "" && value != "" && lastKey == "题目" {
						fields["题目续行"] = value
					}
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

	return "", false
}

func parseHeading(text string) (Heading, bool) {
	matches := headingPattern.FindStringSubmatch(text)
	if matches == nil {
		return Heading{}, false
	}

	level := strings.Count(matches[1], ".") + 1
	return Heading{
		Level: level,
		Text:  strings.TrimSpace(matches[2]),
	}, true
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
