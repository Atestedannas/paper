package paperparse

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
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

	paragraphs, err := extractParagraphs(ctx, documentXML)
	if err != nil {
		return nil, err
	}

	return parseParagraphs(paragraphs), nil
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

func parseParagraphs(paragraphs []string) *ParsedPaper {
	paper := &ParsedPaper{
		CoverFields: make(map[string]string),
	}
	current := sectionCover

	for _, paragraph := range paragraphs {
		text := strings.TrimSpace(paragraph)
		if text == "" {
			continue
		}

		if content, ok := splitSectionMarker(text, "摘要"); ok {
			current = sectionAbstractCN
			if content != "" {
				paper.AbstractCN = append(paper.AbstractCN, content)
			}
			continue
		}
		if content, ok := splitSectionMarker(text, "关键词"); ok {
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
			if content != "" {
				paper.References = append(paper.References, content)
			}
			continue
		}
		if content, ok := splitSectionMarker(text, "致谢"); ok {
			current = sectionAcknowledgements
			if content != "" {
				paper.Acknowledgements = append(paper.Acknowledgements, content)
			}
			continue
		}

		if current != sectionReferences && current != sectionAcknowledgements {
			heading, ok := parseHeading(text)
			if ok {
				paper.Headings = append(paper.Headings, heading)
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
			paper.Body = append(paper.Body, text)
		case sectionAbstractCN:
			paper.AbstractCN = append(paper.AbstractCN, text)
		case sectionKeywordsCN:
			paper.KeywordsCN = parseKeywords(text)
			current = sectionBody
		case sectionReferences:
			paper.References = append(paper.References, text)
		case sectionAcknowledgements:
			paper.Acknowledgements = append(paper.Acknowledgements, text)
		default:
			paper.Body = append(paper.Body, text)
		}
	}

	return paper
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
