package fileprocessor

import (
	"gitee.com/greatmusicians/unioffice/document"
)

// BodyLevelParagraphsOnly returns paragraphs in the main document story, in order,
// excluding paragraphs inside table cells.
//
// This matches python-docx Document.paragraphs (body-level only), which
// style_formatter.py and docvalidate.py use for index-based classification and validation.
//
// unioffice Document.Paragraphs() appends all table-cell paragraphs after body-level
// paragraphs, so its indices diverge from python-docx and cause mass false violations.
func BodyLevelParagraphsOnly(doc *document.Document) []document.Paragraph {
	if doc == nil || doc.Document == nil || doc.Document.Body == nil {
		return nil
	}
	var result []document.Paragraph
	for _, blk := range doc.Document.Body.EG_BlockLevelElts {
		for _, cbc := range blk.EG_ContentBlockContent {
			for _, p := range cbc.P {
				result = append(result, document.Paragraph{Document: doc, WParagraph: p})
			}
		}
	}
	return result
}
