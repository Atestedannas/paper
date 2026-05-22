package cqrwst

import (
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

type TemplateQualityIssue struct {
	RuleID  string
	Message string
	Target  string
}

type TemplateQualityReport struct {
	Passed bool
	Issues []TemplateQualityIssue
}

func AnalyzeTemplateQuality(path string, profile *templateprofile.Profile) (TemplateQualityReport, error) {
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return TemplateQualityReport{}, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return TemplateQualityReport{Passed: false, Issues: []TemplateQualityIssue{{
			RuleID:  "template-quality-document",
			Message: "word/document.xml is missing",
			Target:  documentTarget,
		}}}, nil
	}
	report := TemplateQualityReport{}
	report.Issues = append(report.Issues, analyzeDocumentTemplateQuality(string(content), profile)...)
	report.Issues = append(report.Issues, analyzePackageTemplateQuality(pkg, string(content), profile)...)
	report.Passed = len(report.Issues) == 0
	return report, nil
}

func applyTemplateQualityRepairs(path string, profile *templateprofile.Profile) (int, error) {
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	count := 0
	if content, ok := pkg.Get(documentTarget); ok {
		documentXML := string(content)
		next, fixed := repairDocumentTemplateQuality(documentXML, profile)
		if fixed > 0 {
			pkg.Set(documentTarget, []byte(next))
			count += fixed
		}
	}
	if content, ok := pkg.Get(documentTarget); ok {
		headerXML := cqrwstHeaderXML(extractHeaderTextFromDocumentXML(string(content)))
		if ensurePackageEntry(pkg, "word/header1.xml", headerXML) {
			count++
		}
	}
	if count == 0 {
		return 0, nil
	}
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

func repairDocumentTemplateQuality(documentXML string, profile *templateprofile.Profile) (string, int) {
	_ = profile
	updated := documentXML
	count := 0
	next, fixed := ensureFigureAndTableCaptions(updated)
	if fixed > 0 {
		updated = next
		count += fixed
	}
	next, fixed = removeDuplicateCaptionsAfterContinuation(updated)
	if fixed > 0 {
		updated = next
		count += fixed
	}
	next, fixed = normalizeReferencesTitleQuality(updated)
	if fixed > 0 {
		updated = next
		count += fixed
	}
	return updated, count
}

func removeDuplicateCaptionsAfterContinuation(documentXML string) (string, int) {
	matches := documentBodyChildPattern.FindAllStringIndex(documentXML, -1)
	if len(matches) == 0 {
		return documentXML, 0
	}
	remove := map[int]bool{}
	lastContinuation := ""
	for index, match := range matches {
		child := documentXML[match[0]:match[1]]
		if !isParagraphXML(child) {
			continue
		}
		text := strings.TrimSpace(extractParagraphText(child))
		if key, ok := continuationCaptionKey(text); ok {
			lastContinuation = key
			continue
		}
		if key, ok := generatedCaptionForContinuationKey(text); ok && key == lastContinuation {
			remove[index] = true
			continue
		}
		if text != "" && !isGeneratedGenericCaption(text) {
			lastContinuation = ""
		}
	}
	if len(remove) == 0 {
		return documentXML, 0
	}
	var builder strings.Builder
	last := 0
	count := 0
	for index, match := range matches {
		builder.WriteString(documentXML[last:match[0]])
		if remove[index] {
			count++
		} else {
			builder.WriteString(documentXML[match[0]:match[1]])
		}
		last = match[1]
	}
	builder.WriteString(documentXML[last:])
	return builder.String(), count
}

func continuationCaptionKey(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "\u7eed\u8868") {
		return "", false
	}
	fields := strings.Fields(strings.TrimPrefix(trimmed, "\u7eed\u8868"))
	if len(fields) == 0 {
		return "", false
	}
	return strings.ReplaceAll(fields[0], ".", "-"), true
}

func generatedCaptionForContinuationKey(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "\u8868") {
		return "", false
	}
	fields := strings.Fields(strings.TrimPrefix(trimmed, "\u8868"))
	if len(fields) < 2 {
		return "", false
	}
	generatedNumber := strings.ReplaceAll(fields[0], ".", "-")
	originalNumber := strings.ReplaceAll(fields[1], ".", "-")
	if originalNumber == "" {
		return "", false
	}
	_ = generatedNumber
	return originalNumber, true
}

func normalizeReferencesTitleQuality(documentXML string) (string, int) {
	count := 0
	updated := paragraphPattern.ReplaceAllStringFunc(documentXML, func(paragraph string) string {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if normalizeChineseLabelText(text) != "参考文献" {
			return paragraph
		}
		next := applyParagraphStyle(paragraph, referencesTitleStyle())
		if next != paragraph {
			count++
		}
		return next
	})
	return updated, count
}

func analyzeDocumentTemplateQuality(documentXML string, profile *templateprofile.Profile) []TemplateQualityIssue {
	_ = profile
	issues := []TemplateQualityIssue{}
	if hasGeneratedGenericCaptionBeforeBody(documentXML) {
		issues = append(issues, TemplateQualityIssue{
			RuleID:  "template-quality-caption-scope",
			Message: "generated generic figure/table captions exist before the paper body",
			Target:  documentTarget,
		})
	}
	if !referencesTitleQualityOK(documentXML) {
		issues = append(issues, TemplateQualityIssue{
			RuleID:  "template-quality-references-title",
			Message: "references title should be Songti size four, bold, centered, without stale paragraph run style",
			Target:  documentTarget,
		})
	}
	return issues
}

func analyzePackageTemplateQuality(pkg *ooxmlpkg.DocxPackage, documentXML string, profile *templateprofile.Profile) []TemplateQualityIssue {
	_ = profile
	issues := []TemplateQualityIssue{}
	headerXML, ok := pkg.Get("word/header1.xml")
	if !ok || !strings.Contains(string(headerXML), `w:val="double"`) {
		issues = append(issues, TemplateQualityIssue{
			RuleID:  "template-quality-header-double-line",
			Message: "active CQRWST header part should contain a double bottom border",
			Target:  "word/header1.xml",
		})
	}
	if ok && !strings.Contains(string(headerXML), extractHeaderTextFromDocumentXML(documentXML)) {
		issues = append(issues, TemplateQualityIssue{
			RuleID:  "template-quality-header-text",
			Message: "header text should match student metadata extracted from the paper",
			Target:  "word/header1.xml",
		})
	}
	return issues
}

func hasGeneratedGenericCaptionBeforeBody(documentXML string) bool {
	matches := documentBodyChildPattern.FindAllStringIndex(documentXML, -1)
	for _, match := range matches {
		child := documentXML[match[0]:match[1]]
		if !isParagraphXML(child) {
			continue
		}
		text := strings.TrimSpace(extractParagraphText(child))
		if _, ok := chapterNumberFromHeading(text); ok {
			return false
		}
		if isGeneratedGenericCaption(text) {
			return true
		}
	}
	return false
}

func referencesTitleQualityOK(documentXML string) bool {
	found := false
	ok := false
	for _, paragraph := range paragraphPattern.FindAllString(documentXML, -1) {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if normalizeChineseLabelText(text) != "参考文献" {
			continue
		}
		found = true
		ok = strings.Contains(paragraph, `w:jc w:val="center"`) &&
			strings.Contains(paragraph, `w:sz w:val="28"`) &&
			!strings.Contains(paragraph, `w:sz w:val="32"`) &&
			strings.Contains(paragraph, `<w:b`)
	}
	return !found || ok
}
