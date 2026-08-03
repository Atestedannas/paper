package ooxmlpatch

import (
	"regexp"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

var (
	commentRangeElement     = regexp.MustCompile(`(?s)<w:commentRange(?:Start|End)\b[^>]*/>`)
	commentReferenceElement = regexp.MustCompile(`<w:commentReference\b[^>]*/>`)
	trackedInsertionElement = regexp.MustCompile(`(?s)<w:(?:ins|moveTo)\b[^>]*>(.*?)</w:(?:ins|moveTo)>`)
	trackedDeletionElement  = regexp.MustCompile(`(?s)<w:(?:del|moveFrom)\b[^>]*>.*?</w:(?:del|moveFrom)>`)
	commentsRelationship    = regexp.MustCompile(`(?s)<Relationship\b[^>]*\bType="[^"]*/comments"[^>]*(?:/>|>\s*</Relationship>)`)
	commentsContentType     = regexp.MustCompile(`(?s)<Override\b[^>]*\bPartName="/word/comments\.xml"[^>]*(?:/>|>\s*</Override>)`)
)

func FinalizeReviewMarkup(pkg *ooxmlpkg.DocxPackage) int {
	if pkg == nil {
		return 0
	}
	changed := 0
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		if isProtectedAnnotationPart(name) {
			continue
		}
		content, ok := pkg.Get(name)
		if !ok {
			continue
		}
		updated := finalizeReviewXML(string(content))
		if updated != string(content) {
			pkg.Set(name, []byte(updated))
			changed++
		}
	}
	return changed
}

func isProtectedAnnotationPart(name string) bool {
	base := strings.ToLower(name)
	return strings.HasPrefix(base, "word/comments") ||
		base == "word/footnotes.xml" ||
		base == "word/endnotes.xml"
}

func finalizeReviewXML(content string) string {
	content = trackedDeletionElement.ReplaceAllString(content, "")
	content = trackedInsertionElement.ReplaceAllString(content, "$1")
	return content
}

// RemoveComments is intentionally separate from FinalizeReviewMarkup.
// Callers may remove comments that belong to a template skeleton, while
// in-place repairs must preserve comments from the student's source paper.
func RemoveComments(pkg *ooxmlpkg.DocxPackage) int {
	if pkg == nil {
		return 0
	}
	changed := 0
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		content, ok := pkg.Get(name)
		if !ok {
			continue
		}
		updated := commentReferenceElement.ReplaceAllString(string(content), "")
		updated = commentRangeElement.ReplaceAllString(updated, "")
		if updated != string(content) {
			pkg.Set(name, []byte(updated))
			changed++
		}
	}
	if _, ok := pkg.Get("word/comments.xml"); ok {
		pkg.Delete("word/comments.xml")
		changed++
	}
	if content, ok := pkg.Get(documentRelationshipsTarget); ok {
		updated := commentsRelationship.ReplaceAllString(string(content), "")
		if updated != string(content) {
			pkg.Set(documentRelationshipsTarget, []byte(updated))
			changed++
		}
	}
	if content, ok := pkg.Get(contentTypesTarget); ok {
		updated := commentsContentType.ReplaceAllString(string(content), "")
		if updated != string(content) {
			pkg.Set(contentTypesTarget, []byte(updated))
			changed++
		}
	}
	return changed
}
