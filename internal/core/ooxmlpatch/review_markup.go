package ooxmlpatch

import (
	"regexp"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

var (
	commentRangeElement     = regexp.MustCompile(`(?s)<w:commentRange(?:Start|End)\b[^>]*/>`)
	commentReferenceRun     = regexp.MustCompile(`(?s)<w:r\b[^>]*>\s*(?:<w:rPr\b[^>]*>.*?</w:rPr>\s*)?<w:commentReference\b[^>]*/>\s*</w:r>`)
	trackedInsertionElement = regexp.MustCompile(`(?s)<w:(?:ins|moveTo)\b[^>]*>(.*?)</w:(?:ins|moveTo)>`)
	trackedDeletionElement  = regexp.MustCompile(`(?s)<w:(?:del|moveFrom)\b[^>]*>.*?</w:(?:del|moveFrom)>`)
	commentsRelationship    = regexp.MustCompile(`(?s)<Relationship\b[^>]*\bType="[^"]*/comments"[^>]*/>`)
	commentsContentType     = regexp.MustCompile(`(?s)<Override\b[^>]*\bPartName="/word/comments\.xml"[^>]*/>`)
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
	if _, ok := pkg.Get("word/comments.xml"); ok {
		pkg.Delete("word/comments.xml")
		changed++
	}
	changed += removeReviewPackageLinks(pkg)
	return changed
}

func finalizeReviewXML(content string) string {
	content = commentReferenceRun.ReplaceAllString(content, "")
	content = commentRangeElement.ReplaceAllString(content, "")
	content = trackedDeletionElement.ReplaceAllString(content, "")
	content = trackedInsertionElement.ReplaceAllString(content, "$1")
	return content
}

func removeReviewPackageLinks(pkg *ooxmlpkg.DocxPackage) int {
	changed := 0
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
