package cqrwst

import (
	"context"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

func FixDOCXWithTemplateProfile(ctx context.Context, path string, profile *templateprofile.Profile) (Result, error) {
	result, err := FixDOCX(ctx, path)
	if err != nil {
		return result, err
	}
	if profile == nil {
		return result, nil
	}
	_, styleApplied, err := applyTemplateProfileStyles(path, profile)
	if err != nil {
		return result, err
	}
	_, pageBreakApplied, err := applyTemplateProfilePageBreaks(path, profile)
	if err != nil {
		return result, err
	}
	qualityApplied, err := applyTemplateQualityRepairs(path, profile)
	if err != nil {
		return result, err
	}
	applied := styleApplied + pageBreakApplied + qualityApplied
	if applied > 0 {
		result.FixCount += applied
		result.Issues = append(result.Issues, Issue{
			RuleID:   "cqrwst-template-profile-format",
			Kind:     "repairable_style",
			Severity: "error",
			Message:  "模板画像要求章节分页和段落样式",
			Target:   documentTarget,
		})
		result.Passed = len(result.Issues) == 0
	}
	return result, nil
}

func CheckDOCXWithTemplateProfile(ctx context.Context, path string, profile *templateprofile.Profile) (Result, error) {
	result, err := CheckDOCX(ctx, path)
	if err != nil {
		return result, err
	}
	if profile == nil {
		return result, nil
	}
	if len(profile.Styles) > 0 {
		result.Issues = withoutRepairableStyleIssues(result.Issues)
	}

	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return result, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		result.Passed = len(result.Issues) == 0
		return result, nil
	}
	documentXML := string(content)
	_, styleFixes := applyTemplateProfileStylesToDocumentXML(documentXML, profile)
	_, pageBreakFixes := applyTemplateProfilePageBreaksToDocumentXML(documentXML, profile)
	profileFixes := styleFixes + pageBreakFixes
	qualityReport, err := AnalyzeTemplateQuality(path, profile)
	if err != nil {
		return result, err
	}
	if !qualityReport.Passed {
		profileFixes += len(qualityReport.Issues)
		for _, qualityIssue := range qualityReport.Issues {
			result.Issues = append(result.Issues, Issue{
				RuleID:   qualityIssue.RuleID,
				Kind:     "repairable_template_quality",
				Severity: "error",
				Message:  qualityIssue.Message,
				Target:   qualityIssue.Target,
			})
		}
	}
	if profileFixes > 0 {
		result.FixCount += profileFixes
		result.Issues = append(result.Issues, Issue{
			RuleID:   "cqrwst-template-profile-format",
			Kind:     "repairable_style",
			Severity: "error",
			Message:  "模板画像要求的章节分页或段落样式尚未完全应用",
			Target:   documentTarget,
		})
	}
	result.Passed = len(result.Issues) == 0
	return result, nil
}

func withoutRepairableStyleIssues(issues []Issue) []Issue {
	filtered := issues[:0]
	for _, issue := range issues {
		if issue.Kind == "repairable_style" {
			continue
		}
		filtered = append(filtered, issue)
	}
	return filtered
}

func applyTemplateProfileStyles(path string, profile *templateprofile.Profile) (bool, int, error) {
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return false, 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return false, 0, nil
	}
	documentXML := string(content)
	updatedXML, count := applyTemplateProfileStylesToDocumentXML(documentXML, profile)
	if count == 0 {
		return false, 0, nil
	}
	pkg.Set(documentTarget, []byte(updatedXML))
	if err := pkg.Write(path); err != nil {
		return false, 0, err
	}
	return true, count, nil
}

func applyTemplateProfileStylesToDocumentXML(documentXML string, profile *templateprofile.Profile) (string, int) {
	if profile == nil || len(profile.Styles) == 0 {
		return documentXML, 0
	}
	count := 0
	currentSection := ""
	updated := paragraphPattern.ReplaceAllStringFunc(documentXML, func(paragraph string) string {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		key := templateProfileStyleKey(text, &currentSection)
		if key == "" {
			return paragraph
		}
		styleRule, ok := resolveTemplateProfileStyle(profile.Styles, key)
		if !ok {
			return paragraph
		}
		style, ok := paragraphStyleForTemplateProfileKey(key, styleRule)
		if !ok {
			return paragraph
		}
		next := applyParagraphStyle(paragraph, style)
		if isTemplateProfileLabeledFrontMatterKey(key) && isStructuredFrontMatterParagraph(paragraph, text) {
			next = applyParagraphProperties(paragraph, style)
		}
		if next != paragraph {
			count++
		}
		return next
	})
	return updated, count
}

func paragraphStyleForTemplateProfileKey(key string, rule templateprofile.StyleRule) (paragraphStyle, bool) {
	for _, candidate := range strings.Split(key, "\x00") {
		if candidate == "references_title" {
			return referencesTitleStyle(), true
		}
	}
	return paragraphStyleFromTemplateProfile(rule)
}

func isTemplateProfileLabeledFrontMatterKey(key string) bool {
	for _, candidate := range strings.Split(key, "\x00") {
		switch candidate {
		case "abstract_cn", "keywords_cn", "abstract_en", "keywords_en":
			return true
		}
	}
	return false
}

func applyTemplateProfilePageBreaks(path string, profile *templateprofile.Profile) (bool, int, error) {
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return false, 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return false, 0, nil
	}
	documentXML := string(content)
	updatedXML, count := applyTemplateProfilePageBreaksToDocumentXML(documentXML, profile)
	if count == 0 {
		return false, 0, nil
	}
	pkg.Set(documentTarget, []byte(updatedXML))
	if err := pkg.Write(path); err != nil {
		return false, 0, err
	}
	return true, count, nil
}

func applyTemplateProfilePageBreaksToDocumentXML(documentXML string, profile *templateprofile.Profile) (string, int) {
	if profile == nil || len(profile.Sections) == 0 {
		return documentXML, 0
	}
	sections := map[string]bool{}
	for key, rule := range profile.Sections {
		if rule.PageBreakBefore && isConservativeTemplatePageBreakSection(key) {
			sections[key] = true
		}
	}
	if len(sections) == 0 {
		return documentXML, 0
	}

	count := 0
	updated := paragraphPattern.ReplaceAllStringFunc(documentXML, func(paragraph string) string {
		text := extractParagraphText(paragraph)
		if !matchesTemplateProfileSection(text, sections) {
			return paragraph
		}
		next := ensureParagraphStartsWithPageBreak(paragraph)
		if next != paragraph {
			count++
		}
		return next
	})
	return updated, count
}

func matchesTemplateProfileSection(text string, sections map[string]bool) bool {
	trimmed := strings.TrimSpace(text)
	normalized := normalizeChineseLabelText(trimmed)
	if sections["references_title"] && normalized == "参考文献" {
		return true
	}
	if sections["acknowledgements_title"] && normalized == "致谢" {
		return true
	}
	return false
}

func isConservativeTemplatePageBreakSection(key string) bool {
	switch key {
	case "references_title", "acknowledgements_title", "appendix_title":
		return true
	default:
		return false
	}
}

func templateProfileStyleKey(text string, section *string) string {
	if section == nil {
		empty := ""
		section = &empty
	}
	trimmed := strings.TrimSpace(text)
	normalized := normalizeChineseLabelText(trimmed)
	lower := strings.ToLower(trimmed)
	switch {
	case normalized == "参考文献":
		*section = "references"
		return "references_title"
	case normalized == "致谢":
		*section = "acknowledgements"
		return "acknowledgements_title"
	case strings.HasPrefix(normalized, "摘要"):
		*section = "abstract_cn"
		return "abstract_cn"
	case strings.HasPrefix(normalized, "关键词"):
		*section = ""
		return "keywords_cn"
	case strings.HasPrefix(lower, "abstract"):
		*section = "abstract_en"
		return "abstract_en"
	case strings.HasPrefix(lower, "keywords") || strings.HasPrefix(lower, "key words"):
		*section = ""
		return "keywords_en"
	case heading4Pattern.MatchString(trimmed):
		*section = "body"
		return "heading_4"
	case heading3Pattern.MatchString(trimmed):
		*section = "body"
		return "heading_3"
	case heading2Pattern.MatchString(trimmed):
		*section = "body"
		return "heading_2"
	case heading1Pattern.MatchString(trimmed):
		*section = "body"
		if isBodyStartParagraph(trimmed) {
			return preferStyleKey("body_start", "heading_1")
		}
		return "heading_1"
	case referenceEntryPattern.MatchString(trimmed):
		return "references"
	case *section == "references":
		return "references"
	case *section == "acknowledgements":
		return "acknowledgements"
	case *section == "body":
		return "body"
	case *section == "abstract_cn":
		return "abstract_cn"
	case *section == "abstract_en":
		return "abstract_en"
	default:
		return ""
	}
}

func preferStyleKey(primary string, fallback string) string {
	return primary + "\x00" + fallback
}

func resolveTemplateProfileStyle(styles map[string]templateprofile.StyleRule, key string) (templateprofile.StyleRule, bool) {
	for _, candidate := range strings.Split(key, "\x00") {
		if style, ok := styles[candidate]; ok {
			return style, true
		}
	}
	return templateprofile.StyleRule{}, false
}

func paragraphStyleFromTemplateProfile(rule templateprofile.StyleRule) (paragraphStyle, bool) {
	style := paragraphStyle{
		ruleID:       "cqrwst-template-profile-style",
		message:      "模板画像段落样式",
		eastAsiaFont: strings.TrimSpace(rule.FontEastAsia),
		asciiFont:    strings.TrimSpace(rule.FontASCII),
		fontSize:     strings.TrimSpace(rule.FontSizeHalfPt),
		bold:         rule.Bold,
		alignment:    strings.TrimSpace(rule.Alignment),
		line:         strings.TrimSpace(rule.Line),
	}
	if style.asciiFont == "" && style.eastAsiaFont != "" {
		style.asciiFont = style.eastAsiaFont
	}
	if value, ok := parseTemplateProfileInt(rule.BeforeLines); ok {
		style.beforeLines = intPtr(value)
	}
	if value, ok := parseTemplateProfileInt(rule.AfterLines); ok {
		style.afterLines = intPtr(value)
	}
	if value, ok := parseTemplateProfileInt(rule.FirstLineChars); ok {
		style.firstLineChars = intPtr(value)
	}
	ok := style.eastAsiaFont != "" || style.asciiFont != "" || style.fontSize != "" ||
		style.alignment != "" || style.line != "" || style.beforeLines != nil ||
		style.afterLines != nil || style.firstLineChars != nil || style.bold
	return style, ok
}

func parseTemplateProfileInt(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
