package templatefiller

import (
	"fmt"
	"regexp"
	"strings"
)

// PlaceholderType distinguishes single-paragraph from multi-paragraph placeholders.
type PlaceholderType int

const (
	SingleParagraph PlaceholderType = iota
	MultiParagraph
)

// Placeholder describes a named slot inside the golden template .docx.
type Placeholder struct {
	Tag         string
	Kind        PlaceholderType
	SectionType string
}

// All placeholders expected in the golden template.
var AllPlaceholders = []Placeholder{
	// Cover page fields (single paragraph each, may be inside table cells)
	{Tag: "{{COVER_TITLE}}", Kind: SingleParagraph, SectionType: "cover_title"},
	{Tag: "{{COVER_SUBTITLE}}", Kind: SingleParagraph, SectionType: "cover_subtitle"},
	{Tag: "{{COVER_COLLEGE}}", Kind: SingleParagraph, SectionType: "cover_college"},
	{Tag: "{{COVER_MAJOR}}", Kind: SingleParagraph, SectionType: "cover_major"},
	{Tag: "{{COVER_GRADE}}", Kind: SingleParagraph, SectionType: "cover_grade"},
	{Tag: "{{COVER_STUDENT_ID}}", Kind: SingleParagraph, SectionType: "cover_student_id"},
	{Tag: "{{COVER_STUDENT_NAME}}", Kind: SingleParagraph, SectionType: "cover_student_name"},
	{Tag: "{{COVER_ADVISOR}}", Kind: SingleParagraph, SectionType: "cover_advisor"},
	{Tag: "{{COVER_DATE}}", Kind: SingleParagraph, SectionType: "cover_date"},

	// Inner title page
	{Tag: "{{INNER_TITLE}}", Kind: SingleParagraph, SectionType: "inner_title"},
	{Tag: "{{INNER_SUBTITLE}}", Kind: SingleParagraph, SectionType: "inner_subtitle"},

	// Chinese Abstract
	{Tag: "{{ABSTRACT_TITLE}}", Kind: SingleParagraph, SectionType: "abstract_title"},
	{Tag: "{{ABSTRACT_CONTENT}}", Kind: MultiParagraph, SectionType: "abstract"},
	{Tag: "{{KEYWORDS}}", Kind: SingleParagraph, SectionType: "keywords"},

	// English Abstract
	{Tag: "{{EN_ABSTRACT_TITLE}}", Kind: SingleParagraph, SectionType: "en_abstract_title"},
	{Tag: "{{EN_ABSTRACT_CONTENT}}", Kind: MultiParagraph, SectionType: "en_abstract"},
	{Tag: "{{EN_KEYWORDS}}", Kind: SingleParagraph, SectionType: "en_keywords"},

	// Table of Contents
	{Tag: "{{TOC}}", Kind: MultiParagraph, SectionType: "table_of_contents"},

	// Body (all chapters/sections/paragraphs)
	{Tag: "{{BODY}}", Kind: MultiParagraph, SectionType: "body"},

	// References
	{Tag: "{{REFERENCES_TITLE}}", Kind: SingleParagraph, SectionType: "references_title"},
	{Tag: "{{REFERENCES_CONTENT}}", Kind: MultiParagraph, SectionType: "references"},

	// Acknowledgements
	{Tag: "{{ACKNOWLEDGEMENTS_TITLE}}", Kind: SingleParagraph, SectionType: "acknowledgements_title"},
	{Tag: "{{ACKNOWLEDGEMENTS_CONTENT}}", Kind: MultiParagraph, SectionType: "acknowledgements"},

	// Appendix
	{Tag: "{{APPENDIX_TITLE}}", Kind: SingleParagraph, SectionType: "appendix_title"},
	{Tag: "{{APPENDIX_CONTENT}}", Kind: MultiParagraph, SectionType: "appendix"},
}

var placeholderRe = regexp.MustCompile(`\{\{[A-Z_]+\}\}`)

func FindPlaceholderInText(text string) *Placeholder {
	trimmed := strings.TrimSpace(text)
	for i := range AllPlaceholders {
		if strings.Contains(trimmed, AllPlaceholders[i].Tag) {
			return &AllPlaceholders[i]
		}
	}
	return nil
}

func IsPlaceholderText(text string) bool {
	return placeholderRe.MatchString(text)
}

func PlaceholderMap() map[string]*Placeholder {
	m := make(map[string]*Placeholder, len(AllPlaceholders))
	for i := range AllPlaceholders {
		m[AllPlaceholders[i].SectionType] = &AllPlaceholders[i]
	}
	return m
}

// SectionContent holds the student's extracted content for one placeholder section.
type SectionContent struct {
	SectionType string
	Paragraphs  []ContentParagraph
}

// ContentParagraph is a single paragraph extracted from the student paper.
type ContentParagraph struct {
	Text              string
	Runs              []ContentRun
	SourceXML         string
	HasComplexContent bool
	ParaType          string
}

// ContentRun is a text run within a paragraph.
type ContentRun struct {
	Text   string
	Bold   *bool
	Italic *bool
}

func ValidateSectionContents(sections []SectionContent) error {
	has := make(map[string]bool)
	for _, s := range sections {
		has[s.SectionType] = true
	}
	required := []string{"abstract", "body", "references"}
	var missing []string
	for _, r := range required {
		if !has[r] {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required sections: %s", strings.Join(missing, ", "))
	}
	return nil
}

var BodyParaTypes = map[string]bool{
	"body":      true,
	"heading_1": true,
	"heading_2": true,
	"heading_3": true,
}
