package cqrwst

import (
	"context"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

var (
	templateProfilePageSizePattern   = regexp.MustCompile(`<w:pgSz\b[^>]*/>`)
	templateProfilePageMarginPattern = regexp.MustCompile(`<w:pgMar\b[^>]*/>`)
	templateProfileCitationPattern   = regexp.MustCompile(`\[\d+(?:\s*[-,]\s*\d+)*\]`)
	templateProfileReferenceType     = regexp.MustCompile(`\[[A-Z]{1,2}(?:/[A-Z]{2})?\]`)
	templateProfileTablePattern      = regexp.MustCompile(`(?s)<w:tbl(?:\s[^>]*)?>.*?</w:tbl>`)
	templateProfileTableStartPattern = regexp.MustCompile(`<w:tbl(?:\s[^>]*)?>`)
	templateProfileTablePrPattern    = regexp.MustCompile(`(?s)<w:tblPr(?:\s[^>]*)?>.*?</w:tblPr>`)
	templateProfileTablePrStart      = regexp.MustCompile(`<w:tblPr(?:\s[^>]*)?>`)
	templateProfileTableBorders      = regexp.MustCompile(`(?s)<w:tblBorders>.*?</w:tblBorders>`)
	templateProfileVertAlignPattern  = regexp.MustCompile(`<w:vertAlign\b[^>]*/>`)
	templateProfileArabicHeading     = regexp.MustCompile(`^\d+(?:\.\d+)*\s+\S+`)
	templateProfileChineseHeading    = regexp.MustCompile(`^\x{7b2c}.+\x{7ae0}|^[\x{4e00}\x{4e8c}\x{4e09}\x{56db}\x{4e94}\x{516d}\x{4e03}\x{516b}\x{4e5d}\x{5341}]+[\x{3001}.\x{ff0e}]`)
	templateProfileChapterFigure     = regexp.MustCompile(`^\x{56fe}\d+\.\d+`)
	templateProfileChapterTable      = regexp.MustCompile(`^\x{8868}\d+\.\d+`)
	templateProfileChapterFormula    = regexp.MustCompile(`^\x{5f0f}[\x{ff08}(]\d+\.\d+[\x{ff09})]`)
	templateProfileAnyFigure         = regexp.MustCompile(`^\x{56fe}\d+`)
	templateProfileAnyTable          = regexp.MustCompile(`^\x{8868}\d+`)
	templateProfileAnyFormula        = regexp.MustCompile(`^\x{5f0f}[\x{ff08}(]\d+`)
)

type TemplateProfileProcessor interface {
	Apply(ctx context.Context, path string, profile *templateprofile.Profile) (int, error)
	Check(ctx context.Context, path string, documentXML string, profile *templateprofile.Profile) (int, error)
}

type FrontMatterProcessor struct{}
type HeadingProcessor struct{}
type BodyProcessor struct{}
type ReferenceProcessor struct{}
type CitationProcessor struct{}
type FigureTableProcessor struct{}
type RulePackValidationProcessor struct{}
type SectionBreakProcessor struct{}
type PageSetupProcessor struct{}

type profileStyleProcessor struct{}

func templateProfileProcessors() []TemplateProfileProcessor {
	return []TemplateProfileProcessor{
		profileStyleProcessor{},
		CitationProcessor{},
		ReferenceProcessor{},
		FigureTableProcessor{},
		RulePackValidationProcessor{},
		SectionBreakProcessor{},
		PageSetupProcessor{},
	}
}

func FixDOCXWithTemplateProfile(ctx context.Context, path string, profile *templateprofile.Profile) (Result, error) {
	if profile == nil {
		return FixDOCX(ctx, path)
	}
	result := Result{}
	applied := 0
	for _, processor := range templateProfileProcessors() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		count, err := processor.Apply(ctx, path, profile)
		if err != nil {
			return result, err
		}
		applied += count
	}
	if applied > 0 {
		result.FixCount += applied
		result.Issues = append(result.Issues, Issue{
			RuleID:   "cqrwst-template-profile-format",
			Kind:     "repairable_style",
			Severity: "error",
			Message:  "template profile rules are not fully satisfied",
			Target:   documentTarget,
		})
	}
	result.Passed = len(result.Issues) == 0
	return result, nil
}

func CheckDOCXWithTemplateProfile(ctx context.Context, path string, profile *templateprofile.Profile) (Result, error) {
	if profile == nil {
		return CheckDOCX(ctx, path)
	}
	result := Result{}

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
	profileFixes := 0
	for _, processor := range templateProfileProcessors() {
		count, err := processor.Check(ctx, path, documentXML, profile)
		if err != nil {
			return result, err
		}
		profileFixes += count
	}
	if profileFixes > 0 {
		result.FixCount += profileFixes
		result.Issues = append(result.Issues, Issue{
			RuleID:   "cqrwst-template-profile-format",
			Kind:     "repairable_style",
			Severity: "error",
			Message:  "template profile rules are not fully satisfied",
			Target:   documentTarget,
		})
	}
	result.Passed = len(result.Issues) == 0
	return result, nil
}

func (profileStyleProcessor) Apply(_ context.Context, path string, profile *templateprofile.Profile) (int, error) {
	_, count, err := applyTemplateProfileStyles(path, profile)
	return count, err
}

func (profileStyleProcessor) Check(_ context.Context, _ string, documentXML string, profile *templateprofile.Profile) (int, error) {
	_, count := applyTemplateProfileStylesToDocumentXML(documentXML, profile)
	return count, nil
}

func (SectionBreakProcessor) Apply(_ context.Context, path string, profile *templateprofile.Profile) (int, error) {
	_, count, err := applyTemplateProfilePageBreaks(path, profile)
	return count, err
}

func (SectionBreakProcessor) Check(_ context.Context, _ string, documentXML string, profile *templateprofile.Profile) (int, error) {
	_, count := applyTemplateProfilePageBreaksToDocumentXML(documentXML, profile)
	return count, nil
}

func (PageSetupProcessor) Apply(_ context.Context, path string, profile *templateprofile.Profile) (int, error) {
	_, count, err := applyTemplateProfilePageSetup(path, profile)
	return count, err
}

func (PageSetupProcessor) Check(_ context.Context, _ string, documentXML string, profile *templateprofile.Profile) (int, error) {
	_, count := applyTemplateProfilePageSetupToDocumentXML(documentXML, profile)
	return count, nil
}

func (CitationProcessor) Apply(_ context.Context, path string, profile *templateprofile.Profile) (int, error) {
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, nil
	}
	updated, count := applyCitationRulesToDocumentXML(string(content), profile)
	if count == 0 {
		return 0, nil
	}
	pkg.Set(documentTarget, []byte(updated))
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

func (CitationProcessor) Check(_ context.Context, _ string, documentXML string, profile *templateprofile.Profile) (int, error) {
	_, count := applyCitationRulesToDocumentXML(documentXML, profile)
	return count, nil
}

func (ReferenceProcessor) Apply(_ context.Context, _ string, _ *templateprofile.Profile) (int, error) {
	return 0, nil
}

func (ReferenceProcessor) Check(_ context.Context, _ string, documentXML string, profile *templateprofile.Profile) (int, error) {
	return countReferenceRuleViolations(documentXML, profile), nil
}

func (FigureTableProcessor) Apply(_ context.Context, path string, profile *templateprofile.Profile) (int, error) {
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, nil
	}
	updated, count := applyTableRulesToDocumentXML(string(content), profile)
	if count == 0 {
		return 0, nil
	}
	pkg.Set(documentTarget, []byte(updated))
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

func (FigureTableProcessor) Check(_ context.Context, _ string, documentXML string, profile *templateprofile.Profile) (int, error) {
	_, count := applyTableRulesToDocumentXML(documentXML, profile)
	return count, nil
}

func (RulePackValidationProcessor) Apply(_ context.Context, _ string, _ *templateprofile.Profile) (int, error) {
	return 0, nil
}

func (RulePackValidationProcessor) Check(_ context.Context, path string, documentXML string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	return countRulePackValidationViolations(path, documentXML, profile), nil
}

func countRulePackValidationViolations(path string, documentXML string, profile *templateprofile.Profile) int {
	rules := profile.RulePack
	paragraphs := visibleParagraphTexts(documentXML)
	violations := 0
	violations += countRequiredSectionViolations(paragraphs, rules.RequiredSections)
	violations += countRequiredFieldViolations(paragraphs, rules.RequiredFields)
	violations += countTitleLengthViolations(paragraphs, rules)
	violations += countKeywordRuleViolations(paragraphs, rules)
	violations += countHeadingNumberingViolations(paragraphs, rules.HeadingNumbering)
	violations += countBodyLengthViolations(paragraphs, rules.BodyMinChars)
	violations += countNumberingViolations(paragraphs, rules)
	violations += countReferenceQuantityViolations(paragraphs, rules)
	violations += countHeaderPagePolicyViolations(path, documentXML, profile)
	if rules.BlindReview {
		violations += countBlindReviewViolations(paragraphs)
	}
	return violations
}

func applyCitationRulesToDocumentXML(documentXML string, profile *templateprofile.Profile) (string, int) {
	if profile == nil || strings.TrimSpace(profile.RulePack.CitationStyle) != "superscript_bracket" {
		return documentXML, 0
	}
	count := 0
	inReferences := false
	updated := paragraphPattern.ReplaceAllStringFunc(documentXML, func(paragraph string) string {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if isReferenceTitleText(text) {
			inReferences = true
			return paragraph
		}
		if inReferences && referenceEntryPattern.MatchString(text) {
			return paragraph
		}
		if isAcknowledgementsTitle(text) || heading1Pattern.MatchString(text) {
			inReferences = false
		}
		next, fixes := superscriptCitationsInParagraph(paragraph)
		count += fixes
		return next
	})
	return updated, count
}

func superscriptCitationsInParagraph(paragraph string) (string, int) {
	count := 0
	updated := runPattern.ReplaceAllStringFunc(paragraph, func(run string) string {
		textMatches := textPattern.FindAllStringSubmatch(run, -1)
		if len(textMatches) != 1 || len(textMatches[0]) < 2 {
			return run
		}
		visible := decodeVisibleText(textMatches[0][1])
		if !templateProfileCitationPattern.MatchString(visible) {
			return run
		}
		if strings.Contains(run, `<w:vertAlign w:val="superscript"`) {
			return run
		}
		replacement := splitRunWithSuperscriptCitations(run, visible)
		if replacement != run {
			count++
		}
		return replacement
	})
	return updated, count
}

func splitRunWithSuperscriptCitations(run string, visible string) string {
	rPr := runPropertiesPattern.FindString(run)
	plainRPr := strings.TrimSuffix(strings.TrimPrefix(rPr, "<w:rPr>"), "</w:rPr>")
	matches := templateProfileCitationPattern.FindAllStringIndex(visible, -1)
	if len(matches) == 0 {
		return run
	}
	var builder strings.Builder
	last := 0
	for _, match := range matches {
		if match[0] > last {
			builder.WriteString(buildTemplateProfileRun(visible[last:match[0]], plainRPr, false))
		}
		builder.WriteString(buildTemplateProfileRun(visible[match[0]:match[1]], plainRPr, true))
		last = match[1]
	}
	if last < len(visible) {
		builder.WriteString(buildTemplateProfileRun(visible[last:], plainRPr, false))
	}
	return builder.String()
}

func buildTemplateProfileRun(text string, rPr string, superscript bool) string {
	if text == "" {
		return ""
	}
	runProperties := strings.TrimSpace(rPr)
	if superscript {
		runProperties = templateProfileVertAlignPattern.ReplaceAllString(runProperties, "")
		runProperties += `<w:vertAlign w:val="superscript"/>`
	}
	if runProperties == "" {
		return `<w:r><w:t>` + html.EscapeString(text) + `</w:t></w:r>`
	}
	return `<w:r><w:rPr>` + runProperties + `</w:rPr><w:t>` + html.EscapeString(text) + `</w:t></w:r>`
}

func isReferenceTitleText(text string) bool {
	return normalizeChineseLabelText(text) == "\u53c2\u8003\u6587\u732e"
}

func countReferenceRuleViolations(documentXML string, profile *templateprofile.Profile) int {
	if profile == nil || !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(profile.RulePack.ReferenceStandard)), "GB/T 7714") {
		return 0
	}
	violations := 0
	inReferences := false
	for _, paragraph := range paragraphPattern.FindAllString(documentXML, -1) {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if isReferenceTitleText(text) {
			inReferences = true
			continue
		}
		if !inReferences {
			continue
		}
		if text == "" {
			continue
		}
		if isAcknowledgementsTitle(text) || heading1Pattern.MatchString(text) {
			inReferences = false
			continue
		}
		if referenceEntryPattern.MatchString(text) && !isBasicGBReferenceEntry(text) {
			violations++
		}
	}
	return violations
}

func isBasicGBReferenceEntry(text string) bool {
	trimmed := strings.TrimSpace(text)
	return regexp.MustCompile(`^\[\d+\]\s*.+\..+`).MatchString(trimmed) &&
		templateProfileReferenceType.MatchString(trimmed)
}

func applyTableRulesToDocumentXML(documentXML string, profile *templateprofile.Profile) (string, int) {
	if profile == nil || strings.TrimSpace(profile.RulePack.TableStyle) != "three-line" {
		return documentXML, 0
	}
	count := 0
	updated := templateProfileTablePattern.ReplaceAllStringFunc(documentXML, func(table string) string {
		next := applyThreeLineBorders(table)
		if next != table {
			count++
		}
		return next
	})
	return updated, count
}

func applyThreeLineBorders(table string) string {
	borders := `<w:tblBorders>` +
		`<w:top w:val="single" w:sz="12" w:space="0" w:color="000000"/>` +
		`<w:left w:val="nil"/>` +
		`<w:bottom w:val="single" w:sz="12" w:space="0" w:color="000000"/>` +
		`<w:right w:val="nil"/>` +
		`<w:insideH w:val="single" w:sz="8" w:space="0" w:color="000000"/>` +
		`<w:insideV w:val="nil"/>` +
		`</w:tblBorders>`
	if templateProfileTableBorders.MatchString(table) {
		return templateProfileTableBorders.ReplaceAllString(table, borders)
	}
	if loc := templateProfileTablePrStart.FindStringIndex(table); loc != nil {
		return table[:loc[1]] + borders + table[loc[1]:]
	}
	if loc := templateProfileTableStartPattern.FindStringIndex(table); loc != nil {
		return table[:loc[1]] + `<w:tblPr>` + borders + `</w:tblPr>` + table[loc[1]:]
	}
	return table
}

func visibleParagraphTexts(documentXML string) []string {
	var texts []string
	for _, paragraph := range paragraphPattern.FindAllString(documentXML, -1) {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

func countRequiredSectionViolations(paragraphs []string, required []string) int {
	violations := 0
	for _, section := range required {
		if !hasRequiredSection(paragraphs, strings.TrimSpace(section)) {
			violations++
		}
	}
	return violations
}

func hasRequiredSection(paragraphs []string, section string) bool {
	for _, text := range paragraphs {
		normalized := normalizeChineseLabelText(text)
		lower := strings.ToLower(strings.TrimSpace(text))
		switch section {
		case "cover":
			if strings.Contains(text, "\u5c01\u9762") || strings.Contains(text, "\u5206\u7c7b\u53f7") || strings.Contains(text, "\u5b66\u6821\u4ee3\u7801") {
				return true
			}
		case "title_page":
			if strings.Contains(text, "\u9898\u540d") || strings.Contains(text, "\u5b66\u4f4d\u6388\u4e88\u5355\u4f4d") {
				return true
			}
		case "abstract_cn":
			if normalized == "\u6458\u8981" || strings.HasPrefix(normalized, "\u6458\u8981\uff1a") || strings.HasPrefix(normalized, "\u6458\u8981:") {
				return true
			}
		case "abstract_en":
			if lower == "abstract" || strings.HasPrefix(lower, "abstract:") {
				return true
			}
		case "toc":
			if normalized == "\u76ee\u5f55" || normalized == "\u76ee\u6b21" {
				return true
			}
		case "body":
			if templateProfileArabicHeading.MatchString(text) {
				return true
			}
		case "references":
			if isReferenceTitleText(text) {
				return true
			}
		case "acknowledgements":
			if isAcknowledgementsTitle(text) {
				return true
			}
		}
	}
	return false
}

func countRequiredFieldViolations(paragraphs []string, fields []string) int {
	violations := 0
	for _, field := range fields {
		if !hasNonEmptyField(paragraphs, strings.TrimSpace(field)) {
			violations++
		}
	}
	return violations
}

func hasNonEmptyField(paragraphs []string, field string) bool {
	if field == "" {
		return true
	}
	for _, text := range paragraphs {
		trimmed := strings.TrimSpace(text)
		if !strings.HasPrefix(trimmed, field) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, field))
		rest = strings.TrimSpace(strings.TrimLeft(rest, ":\uff1a"))
		if rest != "" && !strings.Contains(rest, "XXX") && !strings.Contains(rest, "\u8bf7\u586b\u5199") {
			return true
		}
	}
	return false
}

func countTitleLengthViolations(paragraphs []string, rules templateprofile.RulePack) int {
	violations := 0
	for _, text := range paragraphs {
		label, value, ok := splitProfileLabelValue(text)
		if !ok {
			continue
		}
		normalized := strings.ToLower(strings.TrimSpace(label))
		if rules.TitleMaxCNChars > 0 && (strings.Contains(label, "\u9898\u540d") || strings.Contains(label, "\u8bba\u6587\u9898\u76ee")) && countCJKRunes(value) > rules.TitleMaxCNChars {
			violations++
		}
		if rules.TitleMaxENWords > 0 && (normalized == "title" || strings.Contains(normalized, "english title")) && countASCIIWords(value) > rules.TitleMaxENWords {
			violations++
		}
	}
	return violations
}

func countKeywordRuleViolations(paragraphs []string, rules templateprofile.RulePack) int {
	if rules.KeywordMin == 0 && rules.KeywordMax == 0 {
		return 0
	}
	violations := 0
	for _, text := range paragraphs {
		label, value, ok := splitProfileLabelValue(text)
		if !ok {
			continue
		}
		lowerLabel := strings.ToLower(strings.TrimSpace(label))
		if label != "\u5173\u952e\u8bcd" && lowerLabel != "key words" && lowerLabel != "keywords" {
			continue
		}
		if strings.Contains(value, ",") || strings.Contains(value, "\uff0c") || strings.HasSuffix(strings.TrimSpace(value), ".") || strings.HasSuffix(strings.TrimSpace(value), "\u3002") {
			violations++
			continue
		}
		count := countKeywords(value)
		if rules.KeywordMin > 0 && count < rules.KeywordMin {
			violations++
		}
		if rules.KeywordMax > 0 && count > rules.KeywordMax {
			violations++
		}
	}
	return violations
}

func countHeadingNumberingViolations(paragraphs []string, numbering string) int {
	if strings.TrimSpace(numbering) != "arabic" {
		return 0
	}
	violations := 0
	for _, text := range paragraphs {
		if templateProfileChineseHeading.MatchString(strings.TrimSpace(text)) {
			violations++
		}
	}
	return violations
}

func countBodyLengthViolations(paragraphs []string, minChars int) int {
	if minChars <= 0 {
		return 0
	}
	count := countCJKRunes(bodyText(paragraphs))
	if count < minChars {
		return 1
	}
	return 0
}

func bodyText(paragraphs []string) string {
	inBody := false
	var builder strings.Builder
	for _, text := range paragraphs {
		if isReferenceTitleText(text) {
			break
		}
		if templateProfileArabicHeading.MatchString(text) {
			inBody = true
		}
		if inBody {
			builder.WriteString(text)
		}
	}
	return builder.String()
}

func countNumberingViolations(paragraphs []string, rules templateprofile.RulePack) int {
	violations := 0
	for _, text := range paragraphs {
		trimmed := strings.TrimSpace(text)
		if rules.FigureNumbering == "chapter" && templateProfileAnyFigure.MatchString(trimmed) && !templateProfileChapterFigure.MatchString(trimmed) {
			violations++
		}
		if rules.TableNumbering == "chapter" && templateProfileAnyTable.MatchString(trimmed) && !templateProfileChapterTable.MatchString(trimmed) {
			violations++
		}
		if rules.FormulaNumbering == "chapter" && templateProfileAnyFormula.MatchString(trimmed) && !templateProfileChapterFormula.MatchString(trimmed) {
			violations++
		}
	}
	return violations
}

func countReferenceQuantityViolations(paragraphs []string, rules templateprofile.RulePack) int {
	if rules.ReferenceMinCount <= 0 && rules.ReferenceForeignRatioMin <= 0 {
		return 0
	}
	entries := referenceEntries(paragraphs)
	violations := 0
	if rules.ReferenceMinCount > 0 && len(entries) < rules.ReferenceMinCount {
		violations++
	}
	if rules.ReferenceForeignRatioMin > 0 && len(entries) > 0 {
		foreign := 0
		for _, entry := range entries {
			if isForeignReference(entry) {
				foreign++
			}
		}
		if float64(foreign)/float64(len(entries)) < rules.ReferenceForeignRatioMin {
			violations++
		}
	}
	return violations
}

func referenceEntries(paragraphs []string) []string {
	var entries []string
	inReferences := false
	for _, text := range paragraphs {
		if isReferenceTitleText(text) {
			inReferences = true
			continue
		}
		if inReferences && (isAcknowledgementsTitle(text) || templateProfileArabicHeading.MatchString(text)) {
			break
		}
		if inReferences && referenceEntryPattern.MatchString(text) {
			entries = append(entries, text)
		}
	}
	return entries
}

func countHeaderPagePolicyViolations(path string, documentXML string, profile *templateprofile.Profile) int {
	rules := profile.RulePack
	violations := 0
	if rules.HeaderPolicy == "template" && profile.Header.Exists && !strings.Contains(documentXML, "w:headerReference") {
		violations++
	}
	if rules.PageNumbering == "body_arabic_footer_center" {
		footerXML := documentXML + allPackageEntries(path, "word/footer")
		if !strings.Contains(footerXML, "PAGE") {
			violations++
		}
		if !strings.Contains(documentXML, `w:start="1"`) {
			violations++
		}
	}
	return violations
}

func allPackageEntries(path string, prefix string) string {
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return ""
	}
	var builder strings.Builder
	for _, name := range pkg.Names() {
		if strings.HasPrefix(name, prefix) {
			content, ok := pkg.Get(name)
			if !ok {
				continue
			}
			builder.Write(content)
		}
	}
	return builder.String()
}

func countBlindReviewViolations(paragraphs []string) int {
	violations := 0
	for _, text := range paragraphs {
		label, value, ok := splitProfileLabelValue(text)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		if label == "\u4f5c\u8005" || label == "\u7814\u7a76\u751f" || label == "\u6307\u5bfc\u6559\u5e08" || label == "\u5bfc\u5e08" {
			violations++
		}
	}
	return violations
}

func splitProfileLabelValue(text string) (string, string, bool) {
	trimmed := strings.TrimSpace(text)
	for _, sep := range []string{"\uff1a", ":"} {
		if index := strings.Index(trimmed, sep); index > 0 {
			return strings.TrimSpace(trimmed[:index]), strings.TrimSpace(trimmed[index+len(sep):]), true
		}
	}
	return "", "", false
}

func countKeywords(text string) int {
	parts := strings.Split(text, ";")
	if len(parts) == 1 {
		parts = strings.Split(text, "\uff1b")
	}
	count := 0
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}

func countCJKRunes(text string) int {
	count := 0
	for _, r := range text {
		if r > 127 && !isSpaceRune(r) {
			count++
		}
	}
	return count
}

func countASCIIWords(text string) int {
	return len(regexp.MustCompile(`[A-Za-z]+`).FindAllString(text, -1))
}

func isForeignReference(text string) bool {
	asciiLetters := 0
	cjk := 0
	for _, r := range text {
		switch {
		case (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			asciiLetters++
		case r > 127 && !isSpaceRune(r):
			cjk++
		}
	}
	return asciiLetters > cjk
}

func isSpaceRune(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\u3000'
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
		if isTemplateProfileLabeledFrontMatterKey(key) && !isStructuredFrontMatterParagraph(paragraph, text) {
			next := applyTemplateProfileLabeledFrontMatterParagraph(text, style)
			if next != paragraph {
				count++
			}
			return next
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

func applyTemplateProfileLabeledFrontMatterParagraph(text string, style paragraphStyle) string {
	label, body, ok := splitFrontMatterLabel(text)
	if !ok {
		return buildParagraphXML(text, style)
	}
	labelStyle := style
	labelStyle.bold = true
	bodyStyle := style
	bodyStyle.bold = false
	if body != "" && isASCIIText(label) {
		body = " " + strings.TrimSpace(body)
	}
	return buildLabeledParagraphXML(label, body, labelStyle, bodyStyle, style)
}

func splitFrontMatterLabel(text string) (string, string, bool) {
	trimmed := strings.TrimSpace(text)
	for _, sep := range []string{"\uff1a", ":"} {
		if index := strings.Index(trimmed, sep); index > 0 {
			label := strings.TrimSpace(trimmed[:index+len(sep)])
			body := strings.TrimSpace(trimmed[index+len(sep):])
			return label, body, true
		}
	}
	return "", "", false
}

func isASCIIText(text string) bool {
	for _, r := range text {
		if r > 127 {
			return false
		}
	}
	return true
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

func applyTemplateProfilePageSetup(path string, profile *templateprofile.Profile) (bool, int, error) {
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return false, 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return false, 0, nil
	}
	documentXML := string(content)
	updatedXML, count := applyTemplateProfilePageSetupToDocumentXML(documentXML, profile)
	if count == 0 {
		return false, 0, nil
	}
	pkg.Set(documentTarget, []byte(updatedXML))
	if err := pkg.Write(path); err != nil {
		return false, 0, err
	}
	return true, count, nil
}

func applyTemplateProfilePageSetupToDocumentXML(documentXML string, profile *templateprofile.Profile) (string, int) {
	if profile == nil || isEmptyPageSetup(profile.PageSetup) {
		return documentXML, 0
	}
	updated := applyPageSetupToLastSection(documentXML, profile.PageSetup)
	if updated == documentXML {
		return documentXML, 0
	}
	return updated, 1
}

func isEmptyPageSetup(rule templateprofile.PageSetupRule) bool {
	return rule.PageWidthTwips == "" && rule.PageHeightTwips == "" &&
		rule.MarginTopTwips == "" && rule.MarginRightTwips == "" &&
		rule.MarginBottomTwips == "" && rule.MarginLeftTwips == "" &&
		rule.HeaderMarginTwips == "" && rule.FooterMarginTwips == "" &&
		rule.Orientation == ""
}

func applyPageSetupToLastSection(documentXML string, rule templateprofile.PageSetupRule) string {
	sections := sectionPropertiesPattern.FindAllStringIndex(documentXML, -1)
	if len(sections) == 0 {
		sectPr := buildSectionProperties(rule)
		if sectPr == "" {
			return documentXML
		}
		bodyEnd := strings.LastIndex(documentXML, "</w:body>")
		if bodyEnd < 0 {
			return documentXML
		}
		return documentXML[:bodyEnd] + sectPr + documentXML[bodyEnd:]
	}
	last := sections[len(sections)-1]
	existing := documentXML[last[0]:last[1]]
	next := mergeSectionPageSetup(existing, rule)
	if next == existing {
		return documentXML
	}
	return documentXML[:last[0]] + next + documentXML[last[1]:]
}

func buildSectionProperties(rule templateprofile.PageSetupRule) string {
	inner := buildPageSize(rule) + buildPageMargins(rule)
	if inner == "" {
		return ""
	}
	return "<w:sectPr>" + inner + "</w:sectPr>"
}

func mergeSectionPageSetup(section string, rule templateprofile.PageSetupRule) string {
	if strings.HasSuffix(section, "/>") {
		section = strings.TrimSuffix(section, "/>") + "></w:sectPr>"
	}
	pageSize := buildPageSize(rule)
	if pageSize != "" {
		if templateProfilePageSizePattern.MatchString(section) {
			section = templateProfilePageSizePattern.ReplaceAllString(section, pageSize)
		} else {
			section = strings.Replace(section, "</w:sectPr>", pageSize+"</w:sectPr>", 1)
		}
	}
	pageMargins := buildPageMargins(rule)
	if pageMargins != "" {
		if templateProfilePageMarginPattern.MatchString(section) {
			section = templateProfilePageMarginPattern.ReplaceAllString(section, pageMargins)
		} else {
			section = strings.Replace(section, "</w:sectPr>", pageMargins+"</w:sectPr>", 1)
		}
	}
	return section
}

func buildPageSize(rule templateprofile.PageSetupRule) string {
	if rule.PageWidthTwips == "" && rule.PageHeightTwips == "" && rule.Orientation == "" {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(`<w:pgSz`)
	if rule.PageWidthTwips != "" {
		builder.WriteString(` w:w="` + rule.PageWidthTwips + `"`)
	}
	if rule.PageHeightTwips != "" {
		builder.WriteString(` w:h="` + rule.PageHeightTwips + `"`)
	}
	if rule.Orientation != "" {
		builder.WriteString(` w:orient="` + rule.Orientation + `"`)
	}
	builder.WriteString(`/>`)
	return builder.String()
}

func buildPageMargins(rule templateprofile.PageSetupRule) string {
	if rule.MarginTopTwips == "" && rule.MarginRightTwips == "" &&
		rule.MarginBottomTwips == "" && rule.MarginLeftTwips == "" &&
		rule.HeaderMarginTwips == "" && rule.FooterMarginTwips == "" {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(`<w:pgMar`)
	if rule.MarginTopTwips != "" {
		builder.WriteString(` w:top="` + rule.MarginTopTwips + `"`)
	}
	if rule.MarginRightTwips != "" {
		builder.WriteString(` w:right="` + rule.MarginRightTwips + `"`)
	}
	if rule.MarginBottomTwips != "" {
		builder.WriteString(` w:bottom="` + rule.MarginBottomTwips + `"`)
	}
	if rule.MarginLeftTwips != "" {
		builder.WriteString(` w:left="` + rule.MarginLeftTwips + `"`)
	}
	if rule.HeaderMarginTwips != "" {
		builder.WriteString(` w:header="` + rule.HeaderMarginTwips + `"`)
	}
	if rule.FooterMarginTwips != "" {
		builder.WriteString(` w:footer="` + rule.FooterMarginTwips + `"`)
	}
	builder.WriteString(`/>`)
	return builder.String()
}

func matchesTemplateProfileSection(text string, sections map[string]bool) bool {
	trimmed := strings.TrimSpace(text)
	normalized := normalizeChineseLabelText(trimmed)
	if sections["references_title"] && normalized == "\u53c2\u8003\u6587\u732e" {
		return true
	}
	if sections["acknowledgements_title"] && normalized == "\u81f4\u8c22" {
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
	case normalized == "\u53c2\u8003\u6587\u732e":
		*section = "references"
		return "references_title"
	case normalized == "\u81f4\u8c22":
		*section = "acknowledgements"
		return "acknowledgements_title"
	case strings.HasPrefix(normalized, "\u6458\u8981"):
		*section = "abstract_cn"
		return "abstract_cn"
	case strings.HasPrefix(normalized, "\u5173\u952e\u8bcd"):
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
