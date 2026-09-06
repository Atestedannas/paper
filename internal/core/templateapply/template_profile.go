package templateapply

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpatch"
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
	templateProfileContinuousFigure  = regexp.MustCompile(`^\x{56fe}\d+(?:\s|\x{3000}|$)`)
	templateProfileContinuousTable   = regexp.MustCompile(`^\x{8868}\d+(?:\s|\x{3000}|$)`)
	templateProfileContinuousFormula = regexp.MustCompile(`^\x{5f0f}[\x{ff08}(]\d+[\x{ff09})]`)
	templateProfileSubsectionFigure  = regexp.MustCompile(`^\x{56fe}\d+\.\d+\.\d+`)
	templateProfileSubsectionTable   = regexp.MustCompile(`^\x{8868}\d+\.\d+\.\d+`)
	templateProfileSubsectionFormula = regexp.MustCompile(`^\x{5f0f}[\x{ff08}(]\d+\.\d+\.\d+[\x{ff09})]`)
	templateProfileDocElementPattern = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>|<w:tbl(?:\s[^>]*)?>.*?</w:tbl>`)
	templateProfileAuthorYearRef     = regexp.MustCompile(`^[A-Z][A-Za-z-]+(?:\s+et\s+al\.)?\s*\(\d{4}[a-z]?\)`)
	templateProfileOutlineLevel      = regexp.MustCompile(`<w:outlineLvl\b[^>]*\bw:val="([0-8])"`)
	templateProfileCompactHeading    = regexp.MustCompile(`^(\d+(?:\.\d+){1,3})(?:\.)?([^\d\s].+)$`)
	// Some school templates omit the space after a numbered heading (for
	// example "1.编写要求" or "1.1研究目的"). Keep this separate from the
	// sentence/list detector so long numbered body paragraphs are not promoted.
	templateProfileCompactNumberedHeading = regexp.MustCompile(`^(\d+(?:\.\d+){0,3})[.、．]?\s*([^\d\s].+)$`)
	templateProfileBodyList               = regexp.MustCompile(`^[（(]\d+[)）]`)
	templateProfileFigureCaption          = regexp.MustCompile(`^\x{56fe}\s*\d+(?:[.\-]\d+)*\s+\S+`)
	templateProfileTableCaption           = regexp.MustCompile(`^(?:\x{7eed}\x{8868}|\x{8868})\s*\d+(?:[.\-]\d+)*\s+\S+`)
	templateProfileHeaderReference        = regexp.MustCompile(`<w:headerReference\b[^>]*\br:id="([^"]+)"`)
	templateProfileRelationship           = regexp.MustCompile(`<Relationship\b[^>]*>`)
	templateProfileHeaderRun              = regexp.MustCompile(`(?s)<w:r\b[^>]*>.*?</w:r>`)
	templateProfileRunProperties          = regexp.MustCompile(`(?s)<w:rPr\b[^>]*>.*?</w:rPr>`)
	templateProfileEmptyRunProperties     = regexp.MustCompile(`<w:rPr\s*/>`)
	templateProfileRunFonts               = regexp.MustCompile(`<w:rFonts\b[^>]*/>`)
	templateProfileRunSize                = regexp.MustCompile(`<w:sz\b[^>]*/>`)
	templateProfileRunComplexSize         = regexp.MustCompile(`<w:szCs\b[^>]*/>`)
	templateProfileRunUnderline           = regexp.MustCompile(`<w:u\b[^>]*/>`)
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
type HeaderFooterPolicyProcessor struct{}
type PageNumberingProcessor struct{}
type HeadingNumberingProcessor struct{}
type FigureTableCaptionProcessor struct{}
type ReferenceStyleProcessor struct{}
type SectionBreakProcessor struct{}
type PageSetupProcessor struct{}

type profileStyleProcessor struct{}

func templateProfileProcessors() []TemplateProfileProcessor {
	return []TemplateProfileProcessor{
		profileStyleProcessor{},
		CitationProcessor{},
		ReferenceProcessor{},
		FigureTableProcessor{},
		HeaderFooterPolicyProcessor{},
		PageNumberingProcessor{},
		HeadingNumberingProcessor{},
		FigureTableCaptionProcessor{},
		ReferenceStyleProcessor{},
		RulePackValidationProcessor{},
		SectionBreakProcessor{},
		PageSetupProcessor{},
	}
}

func FixDOCXWithTemplateProfile(ctx context.Context, path string, profile *templateprofile.Profile) (Result, error) {
	if profile == nil {
		return Result{}, fmt.Errorf("selected DOCX template profile is required")
	}
	return fixDOCXWithTemplateProfileProcessors(ctx, path, profile, templateProfileProcessors())
}

func ApplyTemplateProfileStylesAndPageSetup(ctx context.Context, path string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	count := 0
	_, styleCount, err := applyTemplateProfileStyles(path, profile)
	if err != nil {
		return 0, err
	}
	count += styleCount
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	_, pageCount, err := applyTemplateProfilePageSetup(path, profile)
	if err != nil {
		return 0, err
	}
	return count + pageCount, nil
}

// ApplyTemplateProfileCoverStylesAndPageSetup applies only the template's
// cover-region paragraph rules plus page setup.  The V2 engine already applies
// the profile rules to abstract/body/heading paragraphs; re-running the full
// profile here would reclassify mixed front-matter runs and undo those fixes.
// This narrow pass also sees paragraphs inside cover tables, which the V2 body
// classifier intentionally excludes.
func ApplyTemplateProfileCoverStylesAndPageSetup(ctx context.Context, path string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	styleCount, err := applyTemplateProfileCoverStyles(path, profile)
	if err != nil {
		return 0, err
	}
	pageCount, err := ApplyTemplateProfilePageSetup(ctx, path, profile)
	return styleCount + pageCount, err
}

// ApplyTemplateProfileCaptionStyles applies only caption rules. Captions may
// be represented outside the body-level paragraph slice used by the V2
// classifier, so they need a small OOXML pass of their own.
func ApplyTemplateProfileCaptionStyles(ctx context.Context, path string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, fmt.Errorf("missing %s", documentTarget)
	}
	count := 0
	updated := paragraphPattern.ReplaceAllStringFunc(string(content), func(paragraph string) string {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		key := ""
		if templateProfileTableCaption.MatchString(text) {
			key = "table_caption"
		} else if templateProfileFigureCaption.MatchString(text) {
			key = "figure_caption"
		}
		if key == "" {
			return paragraph
		}
		rule, found := resolveTemplateProfileStyle(profile.Styles, key)
		if !found {
			return paragraph
		}
		style, valid := paragraphStyleFromTemplateProfile(rule)
		if !valid {
			return paragraph
		}
		next := applyParagraphStyle(paragraph, style)
		if next != paragraph {
			count++
		}
		return next
	})
	pkg.Set(documentTarget, []byte(updated))
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

// ApplyTemplateProfileBodyListStyles handles numbered list paragraphs such
// as “（2）年龄…”. They are body text, but the V2 classifier may preserve the
// source paragraph's list style and skip their run formatting.
func ApplyTemplateProfileBodyListStyles(ctx context.Context, path string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	rule, ok := resolveTemplateProfileStyle(profile.Styles, "body")
	if !ok {
		return 0, nil
	}
	style, ok := paragraphStyleFromTemplateProfile(rule)
	if !ok {
		return 0, nil
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, fmt.Errorf("missing %s", documentTarget)
	}
	count := 0
	updated := paragraphPattern.ReplaceAllStringFunc(string(content), func(paragraph string) string {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if !templateProfileBodyList.MatchString(text) {
			return paragraph
		}
		next := applyParagraphStyle(paragraph, style)
		if next != paragraph {
			count++
		}
		return next
	})
	pkg.Set(documentTarget, []byte(updated))
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

// ApplyTemplateProfileTOCStyles applies the template's TOC-entry rule to
// existing table-of-contents paragraphs. TOC fields and visible text are
// preserved; only paragraph/run formatting is changed. The main classifier
// intentionally skips TOC entries because they are generated fields, so this
// pass must handle their formatting independently.
func ApplyTemplateProfileTOCStyles(ctx context.Context, path string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	rule, ok := resolveTemplateProfileStyle(profile.Styles, "toc_entry")
	if !ok {
		return 0, nil
	}
	style, ok := paragraphStyleFromTemplateProfile(rule)
	if !ok {
		return 0, nil
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, fmt.Errorf("missing %s", documentTarget)
	}
	tocStylePattern := regexp.MustCompile(`(?i)<w:pStyle\b[^>]*w:val="TOC[0-9]+"`)
	count := 0
	updated := paragraphPattern.ReplaceAllStringFunc(string(content), func(paragraph string) string {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if text == "" || !tocStylePattern.MatchString(paragraph) && !isTemplateProfileTOCParagraph(paragraph) {
			return paragraph
		}
		next := applyParagraphStyle(paragraph, style)
		if next != paragraph {
			count++
		}
		return next
	})
	pkg.Set(documentTarget, []byte(updated))
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

func applyTemplateProfileCoverStyles(path string, profile *templateprofile.Profile) (int, error) {
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, fmt.Errorf("missing %s", documentTarget)
	}
	count := 0
	section := "cover"
	updated := paragraphPattern.ReplaceAllStringFunc(string(content), func(paragraph string) string {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if text == "" {
			return paragraph
		}
		if section == "after_cover" {
			normalized := normalizeChineseLabelText(text)
			if strings.HasPrefix(normalized, "摘要") || strings.HasPrefix(strings.ToLower(text), "abstract") {
				section = ""
				return paragraph
			}
			if len([]rune(text)) > 8 {
				if rule, found := resolveTemplateProfileStyle(profile.Styles, frontMatterTitleStyleKey(profile)); found {
					if style, valid := paragraphStyleFromTemplateProfile(rule); valid {
						return applyParagraphStyle(paragraph, style)
					}
				}
			}
			return paragraph
		}
		if section != "cover" {
			return paragraph
		}
		if isTemplateCoverTitleText(text) {
			if rule, found := resolveTemplateProfileStyle(profile.Styles, "cover_title"); found {
				if style, valid := paragraphStyleFromTemplateProfile(rule); valid {
					return applyParagraphStyle(paragraph, style)
				}
			}
		}
		key := templateProfileStyleKey(paragraph, text, &section)
		if isShortTemplateCoverDateText(text) {
			key = "cover_date"
		}
		if key != "cover" {
			if key != "cover_date" {
				return paragraph
			}
		}
		rule, ok := resolveTemplateProfileStyle(profile.Styles, key)
		if !ok {
			return paragraph
		}
		style, ok := paragraphStyleFromTemplateProfile(rule)
		if !ok {
			return paragraph
		}
		// A sampled rule with no w:b is an explicit normal-weight sample. This
		// prevents stale bold runs from the student's cover table surviving.
		if rule.SampleCount > 0 && !rule.BoldSet {
			style.boldSet = true
			style.bold = false
		}
		next := applyParagraphStyle(paragraph, style)
		if next != paragraph {
			count++
		}
		if isShortTemplateCoverDateText(text) {
			section = "after_cover"
		}
		return next
	})
	// Some student documents repeat the thesis title immediately before the
	// abstract, after the cover date. It is not part of the table metadata and
	// may have been classified as a generic paragraph by the V2 classifier.
	// Use the template's cover-title sample for that long front-matter title,
	// stopping at the first abstract marker.
	frontMatter := true
	updated = paragraphPattern.ReplaceAllStringFunc(updated, func(paragraph string) string {
		if !frontMatter {
			return paragraph
		}
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if text == "" {
			return paragraph
		}
		normalized := normalizeChineseLabelText(text)
		if strings.HasPrefix(normalized, "摘要") || strings.HasPrefix(strings.ToLower(text), "abstract") {
			frontMatter = false
			return paragraph
		}
		if len([]rune(text)) <= 20 {
			return paragraph
		}
		rule, found := resolveTemplateProfileStyle(profile.Styles, frontMatterTitleStyleKey(profile))
		if !found {
			return paragraph
		}
		style, valid := paragraphStyleFromTemplateProfile(rule)
		if !valid {
			return paragraph
		}
		return applyParagraphStyle(paragraph, style)
	})
	pkg.Set(documentTarget, []byte(updated))
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

func frontMatterTitleStyleKey(profile *templateprofile.Profile) string {
	if profile != nil {
		if _, ok := profile.Styles["title"]; ok {
			return "title"
		}
		if _, ok := profile.Styles["body"]; ok {
			return "body"
		}
	}
	return "title"
}

func isTemplateCoverTitleText(text string) bool {
	if strings.Contains(text, "原创性") || strings.Contains(text, "作者") || len([]rune(text)) > 32 {
		return false
	}
	return strings.Contains(text, "毕业论文") || strings.Contains(text, "毕业设计") ||
		strings.Contains(text, "学士学位") || strings.Contains(text, "硕士学位") || strings.Contains(text, "博士学位")
}

func isTemplateCoverDateText(text string) bool {
	return strings.Contains(text, "年") && strings.Contains(text, "月")
}

func isShortTemplateCoverDateText(text string) bool {
	trimmed := strings.TrimSpace(text)
	return len([]rune(trimmed)) <= 20 && strings.Contains(trimmed, "\u5e74") && strings.Contains(trimmed, "\u6708")
}

func isTemplateCoverThesisTitle(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len([]rune(trimmed)) <= 20 || strings.HasPrefix(trimmed, "\u6458\u8981") || strings.HasPrefix(strings.ToLower(trimmed), "abstract") {
		return false
	}
	lower := strings.ToLower(trimmed)
	return !strings.Contains(trimmed, "\u5173\u952e\u8bcd") && !strings.HasPrefix(lower, "keywords") && !strings.HasPrefix(lower, "key words")
}

func ApplyTemplateProfilePageSetup(ctx context.Context, path string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	_, count, err := applyTemplateProfilePageSetup(path, profile)
	return count, err
}

func fixDOCXWithTemplateProfileProcessors(ctx context.Context, path string, profile *templateprofile.Profile, processors []TemplateProfileProcessor) (Result, error) {
	result := Result{}
	original, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return result, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".cqrwst-*.docx")
	if err != nil {
		return result, err
	}
	workPath := temp.Name()
	defer os.Remove(workPath)
	if _, err := temp.Write(original); err != nil {
		temp.Close()
		return result, err
	}
	if err := temp.Close(); err != nil {
		return result, err
	}
	applied := 0
	for _, processor := range processors {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		count, err := processor.Apply(ctx, workPath, profile)
		if err != nil {
			return result, err
		}
		applied += count
	}
	updated, err := os.ReadFile(workPath)
	if err != nil {
		return result, err
	}
	if err := os.WriteFile(path, updated, info.Mode()); err != nil {
		_ = os.WriteFile(path, original, info.Mode())
		return result, err
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

func applyTemplateProfileHeaderFooterAndPageNumbering(path string, profile *templateprofile.Profile, includeHeaderFooter bool, includePageNumbering bool) (int, error) {
	if profile == nil {
		return 0, nil
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	documentContent, _ := pkg.Get(documentTarget)
	headerSpec := ooxmlpatch.HeaderFooterPolicySpec{}
	if includeHeaderFooter {
		headerSpec = ooxmlpatch.HeaderFooterPolicySpec{
			Policy:       profile.RulePack.HeaderPolicy,
			OddText:      profile.RulePack.OddHeaderText,
			EvenText:     profile.RulePack.EvenHeaderText,
			HeaderLine:   profile.RulePack.HeaderLine,
			FontEastAsia: profile.Header.FontEastAsia,
			FontSizeHalf: parseTemplateProfileTwips(profile.Header.FontSizeHalfPt),
		}
		if headerSpec.Policy == "" && profile.Header.Exists {
			headerSpec.Policy = "template"
		}
		if headerSpec.Policy == "template" && profile.Header.Exists && documentHasTemplateProfilePartReference(documentContent, "header") {
			headerSpec = ooxmlpatch.HeaderFooterPolicySpec{}
		} else if headerSpec.Policy == "template" && profile.Header.Exists {
			headerText := profile.Header.Text
			if strings.Contains(headerText, "XXX") {
				if paperHeader := extractHeaderTextFromDocumentXML(string(documentContent)); !strings.Contains(paperHeader, "XXX") {
					headerText = paperHeader
				}
			}
			headerSpec.Policy = "odd_even"
			headerSpec.OddText = headerText
			headerSpec.EvenText = headerText
		}
	}
	pageSpec := ooxmlpatch.PageNumberingPolicySpec{}
	if includePageNumbering {
		pageSpec = ooxmlpatch.PageNumberingPolicySpec{
			Policy:      profile.RulePack.PageNumbering,
			FrontFormat: profile.RulePack.FrontPageFormat,
			BodyFormat:  profile.RulePack.BodyPageFormat,
			BodyStart:   profile.RulePack.BodyPageStart,
			BodyWrapper: profile.RulePack.BodyPageWrapper,
		}
		if templateFooterUsesChineseTotalPages(profile.Footer) {
			pageSpec.BodyWrapper = "chinese_total"
		}
		if profile.Footer.Exists && documentHasTemplateProfilePartReference(documentContent, "footer") {
			pageSpec = ooxmlpatch.PageNumberingPolicySpec{}
		}
	}
	if headerSpec.Policy == "" && pageSpec.Policy == "" && pageSpec.FrontFormat == "" && pageSpec.BodyFormat == "" && pageSpec.BodyWrapper == "" && pageSpec.BodyStart == 0 {
		return 0, nil
	}
	count, err := ooxmlpatch.ApplyHeaderFooterAndPageNumbering(pkg, documentTarget, headerSpec, pageSpec)
	if err != nil || count == 0 {
		return count, err
	}
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

// ApplyTemplateProfileHeaderFormatting makes the selected template's header
// typography explicit on every *referenced* header part.  Existing header text
// and per-section chapter names are preserved.  This is intentionally separate
// from header replacement: Word documents frequently have valid section-level
// header text but inherit font settings ambiguously from a producer's defaults.
func ApplyTemplateProfileHeaderFormatting(ctx context.Context, path string, profile *templateprofile.Profile) (int, error) {
	if profile == nil || !profile.Header.Exists {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	font := strings.TrimSpace(profile.Header.FontEastAsia)
	size := parseTemplateProfileTwips(profile.Header.FontSizeHalfPt)
	if font == "" && size == 0 && !profile.Header.HasUnderline {
		return 0, nil
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	documentXML, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, fmt.Errorf("missing %s", documentTarget)
	}
	relsXML, _ := pkg.Get("word/_rels/document.xml.rels")
	updatedParts := 0
	for _, target := range activeTemplateProfileHeaderParts(string(documentXML), string(relsXML)) {
		raw, found := pkg.Get(target)
		if !found {
			continue
		}
		updated := applyTemplateProfileHeaderRunFormatting(string(raw), font, size, profile.Header.HasUnderline)
		if updated == string(raw) {
			continue
		}
		pkg.Set(target, []byte(updated))
		updatedParts++
	}
	if updatedParts == 0 {
		return 0, nil
	}
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return updatedParts, nil
}

func activeTemplateProfileHeaderParts(documentXML, relsXML string) []string {
	byID := map[string]string{}
	for _, relationship := range templateProfileRelationship.FindAllString(relsXML, -1) {
		if !strings.Contains(relationship, "/header\"") {
			continue
		}
		id := templateProfileXMLAttribute(relationship, "Id")
		target := templateProfileXMLAttribute(relationship, "Target")
		if id != "" && target != "" {
			byID[id] = "word/" + strings.TrimPrefix(target, "/")
		}
	}
	parts, seen := make([]string, 0), map[string]bool{}
	for _, match := range templateProfileHeaderReference.FindAllStringSubmatch(documentXML, -1) {
		if len(match) != 2 {
			continue
		}
		target := byID[match[1]]
		if target == "" || seen[target] {
			continue
		}
		parts = append(parts, target)
		seen[target] = true
	}
	return parts
}

func templateProfileXMLAttribute(raw, name string) string {
	match := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `="([^"]+)"`).FindStringSubmatch(raw)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func applyTemplateProfileHeaderRunFormatting(headerXML, font string, size int, underline bool) string {
	return templateProfileHeaderRun.ReplaceAllStringFunc(headerXML, func(run string) string {
		if !strings.Contains(run, "<w:t") {
			return run
		}
		properties := ""
		if font != "" {
			properties += `<w:rFonts w:ascii="` + font + `" w:hAnsi="` + font + `" w:eastAsia="` + font + `" w:cs="` + font + `"/>`
		}
		if size > 0 {
			properties += `<w:sz w:val="` + strconv.Itoa(size) + `"/><w:szCs w:val="` + strconv.Itoa(size) + `"/>`
		}
		if underline {
			properties += `<w:u w:val="single"/>`
		}
		if properties == "" {
			return run
		}
		update := func(existing string) string {
			inner := existing
			if font != "" {
				fontXML := `<w:rFonts w:ascii="` + font + `" w:hAnsi="` + font + `" w:eastAsia="` + font + `" w:cs="` + font + `"/>`
				if templateProfileRunFonts.MatchString(inner) {
					inner = templateProfileRunFonts.ReplaceAllString(inner, fontXML)
				} else {
					inner = strings.Replace(inner, "</w:rPr>", fontXML+"</w:rPr>", 1)
				}
			}
			if size > 0 {
				sizeXML := `<w:sz w:val="` + strconv.Itoa(size) + `"/>`
				complexSizeXML := `<w:szCs w:val="` + strconv.Itoa(size) + `"/>`
				if templateProfileRunSize.MatchString(inner) {
					inner = templateProfileRunSize.ReplaceAllString(inner, sizeXML)
				} else {
					inner = strings.Replace(inner, "</w:rPr>", sizeXML+"</w:rPr>", 1)
				}
				if templateProfileRunComplexSize.MatchString(inner) {
					inner = templateProfileRunComplexSize.ReplaceAllString(inner, complexSizeXML)
				} else {
					inner = strings.Replace(inner, "</w:rPr>", complexSizeXML+"</w:rPr>", 1)
				}
			}
			if underline {
				underlineXML := `<w:u w:val="single"/>`
				if templateProfileRunUnderline.MatchString(inner) {
					inner = templateProfileRunUnderline.ReplaceAllString(inner, underlineXML)
				} else {
					inner = strings.Replace(inner, "</w:rPr>", underlineXML+"</w:rPr>", 1)
				}
			}
			return inner
		}
		if templateProfileRunProperties.MatchString(run) {
			return templateProfileRunProperties.ReplaceAllStringFunc(run, update)
		}
		if templateProfileEmptyRunProperties.MatchString(run) {
			return templateProfileEmptyRunProperties.ReplaceAllString(run, "<w:rPr>"+properties+"</w:rPr>")
		}
		openEnd := strings.Index(run, ">")
		if openEnd < 0 {
			return run
		}
		return run[:openEnd+1] + "<w:rPr>" + properties + "</w:rPr>" + run[openEnd+1:]
	})
}

func documentHasTemplateProfilePartReference(documentXML []byte, kind string) bool {
	return strings.Contains(string(documentXML), "<w:"+kind+"Reference")
}

func templateFooterUsesChineseTotalPages(footer templateprofile.HeaderFooterRule) bool {
	if !footer.Exists || !footer.HasPageField || !footer.HasNumPages {
		return false
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(footer.Text, " ", ""), "\u00a0", "")
	return normalized == "" ||
		(strings.Contains(normalized, "第") && strings.Contains(normalized, "共")) ||
		(strings.Contains(normalized, "页") && strings.Contains(normalized, "共"))
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

func (HeaderFooterPolicyProcessor) Apply(_ context.Context, path string, profile *templateprofile.Profile) (int, error) {
	return applyTemplateProfileHeaderFooterAndPageNumbering(path, profile, true, false)
}

func (HeaderFooterPolicyProcessor) Check(_ context.Context, path string, documentXML string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	return countHeaderFooterPolicyViolations(path, documentXML, profile.RulePack, profile.Header), nil
}

func (PageNumberingProcessor) Apply(_ context.Context, path string, profile *templateprofile.Profile) (int, error) {
	return applyTemplateProfileHeaderFooterAndPageNumbering(path, profile, false, true)
}

func (PageNumberingProcessor) Check(_ context.Context, path string, documentXML string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	return countPageNumberingViolations(path, documentXML, profile.RulePack), nil
}

func (HeadingNumberingProcessor) Apply(_ context.Context, path string, profile *templateprofile.Profile) (int, error) {
	if profile == nil || len(profile.RulePack.HeadingLevels) == 0 {
		return 0, nil
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	count, err := ooxmlpatch.ApplyHeadingNumberingDefinitions(pkg, profile.RulePack.HeadingLevels)
	if err != nil || count == 0 {
		return count, err
	}
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

func (HeadingNumberingProcessor) Check(_ context.Context, _ string, documentXML string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	return countHeadingLevelViolations(visibleParagraphTexts(documentXML), profile.RulePack.HeadingLevels), nil
}

func (FigureTableCaptionProcessor) Apply(_ context.Context, path string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, nil
	}
	updated, count := applyCaptionPositionRulesToDocumentXML(string(content), profile)
	if count == 0 {
		return 0, nil
	}
	pkg.Set(documentTarget, []byte(updated))
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

func (FigureTableCaptionProcessor) Check(_ context.Context, _ string, documentXML string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	paragraphs := visibleParagraphTexts(documentXML)
	return countAdvancedCaptionViolations(documentXML, paragraphs, profile.RulePack), nil
}

func (ReferenceStyleProcessor) Apply(_ context.Context, path string, profile *templateprofile.Profile) (int, error) {
	if profile == nil || (profile.RulePack.ReferenceStyle != "gb_t_7714_sequence" && !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(profile.RulePack.ReferenceStandard)), "GB/T 7714")) {
		return 0, nil
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, nil
	}
	updated, count := normalizeGBReferenceSequence(string(content))
	if count == 0 {
		return 0, nil
	}
	pkg.Set(documentTarget, []byte(updated))
	return count, pkg.Write(path)
}

func (ReferenceStyleProcessor) Check(_ context.Context, _ string, documentXML string, profile *templateprofile.Profile) (int, error) {
	if profile == nil {
		return 0, nil
	}
	return countReferenceStyleViolations(referenceEntries(visibleParagraphTexts(documentXML)), profile.RulePack.ReferenceStyle), nil
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

func countHeaderFooterPolicyViolations(path string, documentXML string, rules templateprofile.RulePack, header templateprofile.HeaderFooterRule) int {
	switch rules.HeaderPolicy {
	case "":
		return 0
	case "none":
		if strings.Contains(documentXML, "w:headerReference") {
			return 1
		}
	case "template":
		if header.Exists && !strings.Contains(documentXML, "w:headerReference") {
			return 1
		}
	case "odd_even":
		violations := 0
		headerXML := allPackageEntries(path, "word/header")
		if !(strings.Contains(documentXML, `w:type="default"`) || strings.Contains(documentXML, `w:type="odd"`)) {
			violations++
		}
		if !strings.Contains(documentXML, `w:type="even"`) {
			violations++
		}
		if rules.OddHeaderText != "" && rules.OddHeaderText != "chapter" && !strings.Contains(headerXML, rules.OddHeaderText) {
			violations++
		}
		if rules.EvenHeaderText != "" && !strings.Contains(headerXML, rules.EvenHeaderText) {
			violations++
		}
		if rules.HeaderLine != "" && rules.HeaderLine != "none" && !strings.Contains(headerXML, "w:bottom") {
			violations++
		}
		return violations
	}
	return 0
}

func countPageNumberingViolations(path string, documentXML string, rules templateprofile.RulePack) int {
	violations := 0
	if rules.PageNumbering == "body_arabic_footer_center" {
		footerXML := documentXML + allPackageEntries(path, "word/footer")
		if !strings.Contains(footerXML, "PAGE") {
			violations++
		}
		if !strings.Contains(documentXML, `w:start="1"`) {
			violations++
		}
	}
	if rules.PageNumbering == "front_roman_body_arabic_center" || rules.FrontPageFormat != "" || rules.BodyPageFormat != "" {
		if rules.FrontPageFormat != "" && !strings.Contains(documentXML, `w:fmt="`+rules.FrontPageFormat+`"`) {
			violations++
		}
		bodyStart := rules.BodyPageStart
		if bodyStart == 0 {
			bodyStart = 1
		}
		if rules.BodyPageFormat != "" && !hasPageNumberType(documentXML, rules.BodyPageFormat, bodyStart) {
			violations++
		}
	}
	if rules.BodyPageWrapper == "dash" || rules.PageNumbering == "nuaa_dash_arabic_bottom_right" {
		if !footerHasDashPageNumber(allPackageEntries(path, "word/footer")) {
			violations++
		}
	}
	return violations
}

func hasPageNumberType(documentXML string, format string, start int) bool {
	startNeedle := `w:start="` + strconv.Itoa(start) + `"`
	for _, section := range sectionPropertiesPattern.FindAllString(documentXML, -1) {
		if !strings.Contains(section, startNeedle) {
			continue
		}
		if format == "decimal" && !strings.Contains(section, `w:fmt=`) {
			return true
		}
		if strings.Contains(section, `w:fmt="`+format+`"`) {
			return true
		}
	}
	return false
}

func footerHasDashPageNumber(footerXML string) bool {
	return strings.Contains(footerXML, "PAGE") && strings.Contains(footerXML, ">-<")
}

func countHeadingLevelViolations(paragraphs []string, levels []string) int {
	if len(levels) == 0 {
		return 0
	}
	violations := 0
	for _, text := range paragraphs {
		trimmed := strings.TrimSpace(text)
		if !looksLikeHeadingNumber(trimmed) {
			continue
		}
		if !matchesAnyHeadingLevel(trimmed, levels) {
			violations++
		}
	}
	return violations
}

func looksLikeHeadingNumber(text string) bool {
	return matchesHeadingPattern(text, "第1章") ||
		matchesHeadingPattern(text, "第一章") ||
		matchesHeadingPattern(text, "1.1") ||
		matchesHeadingPattern(text, "1.1.1") ||
		matchesHeadingPattern(text, "一") ||
		matchesHeadingPattern(text, "(一)") ||
		matchesHeadingPattern(text, "1")
}

func matchesAnyHeadingLevel(text string, levels []string) bool {
	for _, level := range levels {
		if matchesHeadingPattern(text, level) {
			return true
		}
	}
	return false
}

func matchesHeadingPattern(text string, pattern string) bool {
	switch pattern {
	case "第1章":
		return regexp.MustCompile(`^\x{7b2c}\d+\x{7ae0}`).MatchString(text)
	case "第一章":
		return regexp.MustCompile(`^\x{7b2c}[\x{4e00}\x{4e8c}\x{4e09}\x{56db}\x{4e94}\x{516d}\x{4e03}\x{516b}\x{4e5d}\x{5341}]+\x{7ae0}`).MatchString(text)
	case "1.1":
		return regexp.MustCompile(`^\d+\.\d+(?:\s|\x{3000}|$)`).MatchString(text)
	case "1.1.1":
		return regexp.MustCompile(`^\d+\.\d+\.\d+(?:\s|\x{3000}|$)`).MatchString(text)
	case "一":
		return regexp.MustCompile(`^[\x{4e00}\x{4e8c}\x{4e09}\x{56db}\x{4e94}\x{516d}\x{4e03}\x{516b}\x{4e5d}\x{5341}]+[\x{3001}.．]`).MatchString(text)
	case "(一)":
		return regexp.MustCompile(`^[\(（][\x{4e00}\x{4e8c}\x{4e09}\x{56db}\x{4e94}\x{516d}\x{4e03}\x{516b}\x{4e5d}\x{5341}]+[\)）]`).MatchString(text)
	case "1":
		return regexp.MustCompile(`^\d+[\x{3001}.．、\s]`).MatchString(text) && !strings.Contains(strings.Fields(text)[0], ".")
	default:
		return false
	}
}

func countAdvancedCaptionViolations(documentXML string, paragraphs []string, rules templateprofile.RulePack) int {
	violations := countContinuousOrChapterNumberingViolations(paragraphs, rules)
	violations += countCaptionPositionViolations(documentXML, rules)
	return violations
}

func countContinuousOrChapterNumberingViolations(paragraphs []string, rules templateprofile.RulePack) int {
	violations := 0
	for _, text := range paragraphs {
		trimmed := strings.TrimSpace(text)
		if rules.FigureNumbering == "continuous" && templateProfileAnyFigure.MatchString(trimmed) && !templateProfileContinuousFigure.MatchString(trimmed) {
			violations++
		}
		if rules.TableNumbering == "continuous" && templateProfileAnyTable.MatchString(trimmed) && !templateProfileContinuousTable.MatchString(trimmed) {
			violations++
		}
		if rules.FormulaNumbering == "continuous" && templateProfileAnyFormula.MatchString(trimmed) && !templateProfileContinuousFormula.MatchString(trimmed) {
			violations++
		}
		if rules.FigureNumbering == "subsection" && templateProfileAnyFigure.MatchString(trimmed) && !templateProfileSubsectionFigure.MatchString(trimmed) {
			violations++
		}
		if rules.TableNumbering == "subsection" && templateProfileAnyTable.MatchString(trimmed) && !templateProfileSubsectionTable.MatchString(trimmed) {
			violations++
		}
		if rules.FormulaNumbering == "subsection" && templateProfileAnyFormula.MatchString(trimmed) && !templateProfileSubsectionFormula.MatchString(trimmed) {
			violations++
		}
	}
	return violations
}

func countCaptionPositionViolations(documentXML string, rules templateprofile.RulePack) int {
	if rules.TableCaptionPosition == "" && rules.FigureCaptionPosition == "" {
		return 0
	}
	elements := templateProfileDocElementPattern.FindAllString(documentXML, -1)
	violations := 0
	for index, element := range elements {
		if strings.HasPrefix(element, "<w:tbl") && rules.TableCaptionPosition != "" {
			before := neighborParagraphText(elements, index-1)
			after := neighborParagraphText(elements, index+1)
			violations += captionPositionViolation(before, after, rules.TableCaptionPosition, templateProfileAnyTable)
		}
		if strings.Contains(element, "<w:drawing") && rules.FigureCaptionPosition != "" {
			before := neighborParagraphText(elements, index-1)
			after := neighborParagraphText(elements, index+1)
			violations += captionPositionViolation(before, after, rules.FigureCaptionPosition, templateProfileAnyFigure)
		}
	}
	return violations
}

func applyCaptionPositionRulesToDocumentXML(documentXML string, profile *templateprofile.Profile) (string, int) {
	rules := profile.RulePack
	if rules.TableCaptionPosition == "" && rules.FigureCaptionPosition == "" {
		return documentXML, 0
	}
	elements := templateProfileDocElementPattern.FindAllString(documentXML, -1)
	if len(elements) == 0 {
		return documentXML, 0
	}
	count := 0
	for index := 0; index < len(elements)-1; index++ {
		current := elements[index]
		next := elements[index+1]
		if rules.TableCaptionPosition == "above" && strings.HasPrefix(current, "<w:tbl") && isCaptionParagraph(next, templateProfileAnyTable) {
			elements[index], elements[index+1] = styleCaptionParagraph(next, profile), current
			count++
			index++
			continue
		}
		if rules.FigureCaptionPosition == "below" && isCaptionParagraph(current, templateProfileAnyFigure) && strings.Contains(next, "<w:drawing") {
			elements[index], elements[index+1] = next, styleCaptionParagraph(current, profile)
			count++
			index++
		}
	}
	if count == 0 {
		return documentXML, 0
	}
	return replaceDocumentBodyElements(documentXML, elements), count
}

func isCaptionParagraph(element string, pattern *regexp.Regexp) bool {
	return strings.HasPrefix(element, "<w:p") && pattern.MatchString(strings.TrimSpace(extractParagraphText(element)))
}

func styleCaptionParagraph(paragraph string, profile *templateprofile.Profile) string {
	if profile == nil || profile.RulePack.CaptionStyleKey == "" {
		return paragraph
	}
	rule, ok := resolveTemplateProfileStyle(profile.Styles, profile.RulePack.CaptionStyleKey)
	if !ok {
		return paragraph
	}
	style, ok := paragraphStyleFromTemplateProfile(rule)
	if !ok {
		return paragraph
	}
	return applyParagraphStyle(paragraph, style)
}

func replaceDocumentBodyElements(documentXML string, elements []string) string {
	matches := templateProfileDocElementPattern.FindAllStringIndex(documentXML, -1)
	if len(matches) != len(elements) {
		return documentXML
	}
	var builder strings.Builder
	offset := 0
	for index, match := range matches {
		builder.WriteString(documentXML[offset:match[0]])
		builder.WriteString(elements[index])
		offset = match[1]
	}
	builder.WriteString(documentXML[offset:])
	return builder.String()
}

func neighborParagraphText(elements []string, index int) string {
	if index < 0 || index >= len(elements) || !strings.HasPrefix(elements[index], "<w:p") {
		return ""
	}
	return strings.TrimSpace(extractParagraphText(elements[index]))
}

func captionPositionViolation(before string, after string, position string, pattern *regexp.Regexp) int {
	switch position {
	case "above":
		if !pattern.MatchString(before) && pattern.MatchString(after) {
			return 1
		}
	case "below":
		if !pattern.MatchString(after) && pattern.MatchString(before) {
			return 1
		}
	}
	return 0
}

func countReferenceStyleViolations(entries []string, style string) int {
	if style == "" {
		return 0
	}
	violations := 0
	for _, entry := range entries {
		if !matchesReferenceStyle(entry, style) {
			violations++
		}
	}
	return violations
}

func matchesReferenceStyle(entry string, style string) bool {
	trimmed := strings.TrimSpace(entry)
	switch style {
	case "gb_t_7714_sequence":
		return isBasicGBReferenceEntry(trimmed)
	case "author_year":
		return templateProfileAuthorYearRef.MatchString(trimmed)
	case "sample_book_journal_basic", "custom_school_basic":
		return isSampleBookJournalReference(trimmed)
	default:
		return true
	}
}

func isSampleBookJournalReference(entry string) bool {
	if !referenceEntryPattern.MatchString(entry) || !templateProfileReferenceType.MatchString(entry) {
		return false
	}
	if strings.Contains(entry, "[M]") {
		return strings.Contains(entry, ":") || strings.Contains(entry, "\uff1a")
	}
	if strings.Contains(entry, "[J]") {
		return strings.Contains(entry, ",") && regexp.MustCompile(`\d{4}`).MatchString(entry)
	}
	return false
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
		runXML := `<w:r><w:rPr>` + runProperties + `</w:rPr><w:t>` + html.EscapeString(text) + `</w:t></w:r>`
		updated, _ := ooxmlpatch.ApplyRunProperties(runXML, ooxmlpatch.RunPropertiesSpec{VerticalAlign: "superscript"})
		return updated
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

func normalizeGBReferenceSequence(documentXML string) (string, int) {
	inReferences := false
	ordinal := 0
	count := 0
	updated := paragraphPattern.ReplaceAllStringFunc(documentXML, func(paragraph string) string {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if isReferenceTitleText(text) {
			inReferences = true
			return paragraph
		}
		if !inReferences {
			return paragraph
		}
		if isAcknowledgementsTitle(text) || heading1Pattern.MatchString(text) {
			inReferences = false
			return paragraph
		}
		if !referenceEntryPattern.MatchString(text) {
			return paragraph
		}
		ordinal++
		normalized := fmt.Sprintf("[%d] %s", ordinal, strings.TrimSpace(referenceEntryPattern.ReplaceAllString(text, "")))
		if normalized == text {
			return paragraph
		}
		count++
		return replaceParagraphVisibleText(paragraph, normalized)
	})
	return updated, count
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
	updated, _ := ooxmlpatch.ApplyThreeLineTableBorders(table, ooxmlpatch.TableBordersSpec{
		TopSize:    12,
		HeaderSize: 8,
		BottomSize: 12,
	})
	return updated
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
		case "originality_declaration":
			if strings.Contains(normalized, "\u539f\u521b\u6027\u58f0\u660e") || strings.Contains(normalized, "\u539f\u521b\u6027\u7533\u660e") || strings.Contains(normalized, "\u5b66\u672f\u8bda\u4fe1\u58f0\u660e") {
				return true
			}
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
	// The document starts with cover metadata.  Keeping this explicit state
	// lets the profile-driven path apply the template's sampled cover rule;
	// previously unlabeled cover paragraphs were left with the student's
	// formatting and only the later abstract/body states were recognized.
	currentSection := ""
	if _, hasCoverRule := profile.Styles["cover"]; hasCoverRule {
		currentSection = "cover"
	}
	referenceMisses := 0
	updated := paragraphPattern.ReplaceAllStringFunc(documentXML, func(paragraph string) string {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if text == "" || isTemplateProfileTOCParagraph(paragraph) {
			return paragraph
		}
		if currentSection == "references" && !referenceEntryPattern.MatchString(text) && !isReferenceTitleText(text) {
			referenceMisses++
			if referenceMisses > 2 {
				currentSection = ""
			}
		} else {
			referenceMisses = 0
		}
		// Cover titles are not headings and often have no semantic style name.
		// Resolve them before the generic cover fallback, otherwise the full
		// profile pass downgrades the 36pt title to the cover metadata rule.
		key := ""
		normalizedText := normalizeChineseLabelText(text)
		lowerText := strings.ToLower(text)
		frontMatterLabel := strings.HasPrefix(normalizedText, "\u6458\u8981") ||
			strings.HasPrefix(normalizedText, "\u5173\u952e\u8bcd") ||
			strings.HasPrefix(lowerText, "abstract") ||
			strings.HasPrefix(lowerText, "keywords") ||
			strings.HasPrefix(lowerText, "key words")
		switch {
		case currentSection == "cover" && isTemplateCoverTitleText(text):
			key = "cover_title"
		case currentSection == "cover" && isShortTemplateCoverDateText(text):
			key = "cover_date"
		case currentSection == "cover" && isTemplateCoverThesisTitle(text):
			key = "cover_title"
		case currentSection == "after_cover" && isTemplateCoverThesisTitle(text):
			key = "cover_title"
		case currentSection == "after_cover" && !frontMatterLabel && len([]rune(text)) > 20:
			key = frontMatterTitleStyleKey(profile)
		default:
			key = templateProfileStyleKey(paragraph, text, &currentSection)
		}
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
		if isTemplateProfileLabeledFrontMatterKey(key) {
			next := applyTemplateProfileLabeledFrontMatterParagraph(paragraph, text, key, style, profile)
			if next != paragraph {
				count++
			}
			return next
		}
		next := applyParagraphStyle(paragraph, style)
		if next != paragraph {
			count++
		}
		if key == "cover_date" {
			currentSection = "after_cover"
		}
		return next
	})
	return updated, count
}

func isTemplateProfileTOCParagraph(paragraph string) bool {
	return strings.Contains(paragraph, " TOC ") ||
		strings.Contains(paragraph, `<w:tab w:val="right" w:leader="dot"`) ||
		strings.Contains(paragraph, `<w:tab w:leader="dot"`)
}

func paragraphStyleForTemplateProfileKey(key string, rule templateprofile.StyleRule) (paragraphStyle, bool) {
	// A sampled rule with explicit low confidence is evidence for review, not
	// permission to overwrite a student's formatting. Confidence==0 is kept
	// for hand-authored rules and legacy fixtures that predate confidence.
	if rule.SampleCount > 0 && rule.Confidence > 0 && rule.Confidence < 0.85 {
		return paragraphStyle{}, false
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

func applyTemplateProfileLabeledFrontMatterParagraph(paragraph, text, key string, labelStyle paragraphStyle, profile *templateprofile.Profile) string {
	label, body, ok := splitFrontMatterLabel(text)
	if !ok {
		return applyParagraphStyle(paragraph, labelStyle)
	}
	labelStyle.bold = true
	labelStyle.boldSet = true
	bodyStyle := labelStyle
	bodyStyle.bold = false
	bodyStyle.boldSet = true
	if extracted, ok := templateProfileFrontMatterBodyStyle(profile, key); ok {
		bodyStyle = extracted
	}
	if body == "" {
		return applyParagraphStyle(paragraph, labelStyle)
	}

	labelStart := strings.Index(text, label)
	if labelStart < 0 {
		return applyParagraphStyle(paragraph, bodyStyle)
	}
	labelEnd := labelStart + len(label)
	updated := applyParagraphProperties(paragraph, bodyStyle)
	offset := 0
	return runPattern.ReplaceAllStringFunc(updated, func(run string) string {
		visible := extractParagraphText(run)
		if visible == "" {
			return run
		}
		runStart := offset
		runEnd := runStart + len(visible)
		offset = runEnd
		switch {
		case runEnd <= labelEnd:
			return applyRunProperties(run, labelStyle)
		case runStart >= labelEnd:
			return applyRunProperties(run, bodyStyle)
		default:
			return applyFrontMatterRunBoundaryStyle(run, visible, labelEnd-runStart, labelStyle, bodyStyle)
		}
	})
}

func applyFrontMatterRunBoundaryStyle(run, visible string, boundary int, labelStyle, bodyStyle paragraphStyle) string {
	if boundary <= 0 {
		return applyRunProperties(run, bodyStyle)
	}
	if boundary >= len(visible) {
		return applyRunProperties(run, labelStyle)
	}

	textMatches := textPattern.FindAllStringSubmatchIndex(run, -1)
	if len(textMatches) != 1 || len(textMatches[0]) < 4 {
		// ponytail: mixed-content runs stay intact; split them only with a real XML tree editor.
		return applyRunProperties(run, labelStyle)
	}
	match := textMatches[0]
	if decodeVisibleText(run[match[2]:match[3]]) != visible || !isPlainTextRun(run) {
		return applyRunProperties(run, labelStyle)
	}

	labelRun := run[:match[2]] + html.EscapeString(visible[:boundary]) + run[match[3]:]
	bodyRun := run[:match[2]] + html.EscapeString(visible[boundary:]) + run[match[3]:]
	return applyRunProperties(labelRun, labelStyle) + applyRunProperties(bodyRun, bodyStyle)
}

func isPlainTextRun(run string) bool {
	shell := textPattern.ReplaceAllString(run, "")
	shell = runPropertiesPattern.ReplaceAllString(shell, "")
	openEnd := strings.Index(shell, ">")
	closeStart := strings.LastIndex(shell, "</w:r>")
	return openEnd >= 0 && closeStart >= openEnd && strings.TrimSpace(shell[openEnd+1:closeStart]) == ""
}

func templateProfileFrontMatterBodyStyle(profile *templateprofile.Profile, key string) (paragraphStyle, bool) {
	if profile == nil {
		return paragraphStyle{}, false
	}
	var candidates []string
	for _, candidate := range strings.Split(key, "\x00") {
		switch candidate {
		case "abstract_cn":
			candidates = append(candidates, "abstract_body")
		case "abstract_en":
			candidates = append(candidates, "abstract_en_body")
		case "keywords_cn":
			candidates = append(candidates, "keywords_cn_body", "abstract_body")
		case "keywords_en":
			candidates = append(candidates, "keywords_en_body", "abstract_en_body")
		}
	}
	for _, candidate := range candidates {
		if rule, ok := profile.Styles[candidate]; ok {
			if style, valid := paragraphStyleFromTemplateProfile(rule); valid {
				return style, true
			}
		}
	}
	return paragraphStyle{}, false
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
	updated := applyPageSetupToAllSections(documentXML, profile.PageSetup)
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
	updated, changed := ooxmlpatch.ApplySectionProperties(documentXML, pageSetupRuleToSectionSpec(rule))
	if !changed {
		return documentXML
	}
	return updated
}

func applyPageSetupToAllSections(documentXML string, rule templateprofile.PageSetupRule) string {
	updated, changed := ooxmlpatch.ApplySectionPropertiesAll(documentXML, pageSetupRuleToSectionSpec(rule))
	if !changed {
		return documentXML
	}
	return updated
}

func pageSetupRuleToSectionSpec(rule templateprofile.PageSetupRule) ooxmlpatch.SectionPropertiesSpec {
	return ooxmlpatch.SectionPropertiesSpec{
		PageWidthTwips:    parseTemplateProfileTwips(rule.PageWidthTwips),
		PageHeightTwips:   parseTemplateProfileTwips(rule.PageHeightTwips),
		PageOrientation:   rule.Orientation,
		MarginTopTwips:    parseTemplateProfileTwips(rule.MarginTopTwips),
		MarginRightTwips:  parseTemplateProfileTwips(rule.MarginRightTwips),
		MarginBottomTwips: parseTemplateProfileTwips(rule.MarginBottomTwips),
		MarginLeftTwips:   parseTemplateProfileTwips(rule.MarginLeftTwips),
		HeaderMarginTwips: parseTemplateProfileTwips(rule.HeaderMarginTwips),
		FooterMarginTwips: parseTemplateProfileTwips(rule.FooterMarginTwips),
	}
}

func parseTemplateProfileTwips(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
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

func templateProfileStyleKey(paragraph string, text string, section *string) string {
	if section == nil {
		empty := ""
		section = &empty
	}
	trimmed := strings.TrimSpace(text)
	normalized := normalizeChineseLabelText(trimmed)
	lower := strings.ToLower(trimmed)
	headingLevel := templateProfileParagraphHeadingLevel(paragraph, trimmed)
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
	case *section == "cover":
		// Cover metadata such as "2022级护理学5班" begins with digits and
		// must not be mistaken for a numbered heading while the cover state is
		// active.
		return "cover"
	case templateProfileTableCaption.MatchString(trimmed):
		return "table_caption"
	case templateProfileFigureCaption.MatchString(trimmed):
		return "figure_caption"
	case headingLevel == 4:
		*section = "body"
		return "heading_4"
	case headingLevel == 3:
		*section = "body"
		return "heading_3"
	case headingLevel == 2:
		*section = "body"
		return "heading_2"
	case headingLevel == 1:
		*section = "body"
		if isBodyStartParagraph(trimmed) {
			return preferStyleKey("body_start", "heading_1")
		}
		return "heading_1"
	case templateProfileChineseHeading.MatchString(trimmed):
		*section = "body"
		if isChineseBodyStartParagraph(trimmed) {
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
	case *section == "cover":
		return "cover"
	case *section == "abstract_cn":
		return "abstract_body"
	case *section == "abstract_en":
		return "abstract_en_body"
	default:
		return ""
	}
}

func templateProfileParagraphHeadingLevel(paragraph string, text string) int {
	if match := templateProfileOutlineLevel.FindStringSubmatch(paragraph); len(match) == 2 {
		if level, err := strconv.Atoi(match[1]); err == nil {
			return level + 1
		}
	}
	if match := templateProfileCompactHeading.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 3 {
		return strings.Count(match[1], ".") + 1
	}
	if match := templateProfileCompactNumberedHeading.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 3 && likelyCompactHeadingTitle(match[2]) {
		level := strings.Count(match[1], ".") + 1
		// A single-number heading written as "1.标题" is normally a
		// section below a Chinese/Arabic chapter heading, not a chapter.
		if level == 1 && regexp.MustCompile(`^\d+[.．、]`).MatchString(strings.TrimSpace(text)) {
			level = 2
		}
		return level
	}
	switch {
	case heading4Pattern.MatchString(text):
		return 4
	case heading3Pattern.MatchString(text):
		return 3
	case heading2Pattern.MatchString(text):
		return 2
	case heading1Pattern.MatchString(text):
		return 1
	default:
		return 0
	}
}

func likelyCompactHeadingTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 32 {
		return false
	}
	return !strings.ContainsAny(title, "。！？；;，,")
}

func isChineseBodyStartParagraph(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "\u7b2c\u4e00\u7ae0") || strings.HasPrefix(trimmed, "\u7b2c1\u7ae0")
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
		ruleID:          "cqrwst-template-profile-style",
		message:         "模板画像段落样式",
		eastAsiaFont:    strings.TrimSpace(rule.FontEastAsia),
		asciiFont:       strings.TrimSpace(rule.FontASCII),
		hAnsiFont:       strings.TrimSpace(rule.FontHAnsi),
		complexFont:     strings.TrimSpace(rule.FontCS),
		fontSize:        strings.TrimSpace(rule.FontSizeHalfPt),
		complexSize:     strings.TrimSpace(rule.ComplexSizeHalfPt),
		bold:            rule.Bold,
		boldSet:         rule.BoldSet,
		italic:          rule.Italic,
		italicSet:       rule.ItalicSet,
		keepNext:        rule.KeepNext,
		keepNextSet:     rule.KeepNextSet,
		keepLines:       rule.KeepLines,
		keepLinesSet:    rule.KeepLinesSet,
		widowControl:    rule.WidowControl,
		widowControlSet: rule.WidowControlSet,
		alignment:       strings.TrimSpace(rule.Alignment),
		line:            strings.TrimSpace(rule.Line),
		lineRule:        strings.TrimSpace(rule.LineRule),
	}
	if value, ok := parseTemplateProfileInt(rule.BeforeLines); ok {
		style.beforeLines = intPtr(value)
	}
	if value, ok := parseTemplateProfileInt(rule.AfterLines); ok {
		style.afterLines = intPtr(value)
	}
	if value, ok := parseTemplateProfileInt(rule.BeforeTwips); ok {
		style.beforeTwips = intPtr(value)
	}
	if value, ok := parseTemplateProfileInt(rule.AfterTwips); ok {
		style.afterTwips = intPtr(value)
	}
	if value, ok := parseTemplateProfileInt(rule.FirstLineChars); ok {
		// OOXML firstLineChars is measured in 1/100ths of a character. Values
		// above a page width are extraction artefacts (for example 1151 from a
		// stale tab/indent node), not a usable paragraph indent. Treat them as
		// undefined so the rule's normal fallback remains in effect.
		if value >= 0 && value <= 1000 {
			style.firstLineChars = intPtr(value)
		}
	}
	// A line value is in twentieths of a point. A value larger than 120pt is
	// not a practical thesis paragraph spacing and usually means the sampler
	// read a table/section value instead of paragraph formatting.
	if style.line != "" {
		if value, ok := parseTemplateProfileInt(style.line); ok && value > 2400 {
			style.line = ""
			style.lineRule = ""
		}
	}
	ok := style.eastAsiaFont != "" || style.asciiFont != "" || style.hAnsiFont != "" || style.complexFont != "" || style.fontSize != "" ||
		style.alignment != "" || style.line != "" || style.beforeTwips != nil || style.afterTwips != nil ||
		style.beforeLines != nil || style.afterLines != nil || style.firstLineChars != nil ||
		style.boldSet || style.bold || style.italicSet || style.italic || style.keepNextSet || style.keepLinesSet || style.widowControlSet
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
