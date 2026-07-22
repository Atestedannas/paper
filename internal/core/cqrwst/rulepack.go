package cqrwst

import (
	"context"
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpatch"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

const documentTarget = "word/document.xml"

const (
	debugEnabledEnv     = "CQRWST_DEBUG"
	debugDirEnv         = "CQRWST_DEBUG_DIR"
	contentNormalizeEnv = "CQRWST_ALLOW_CONTENT_NORMALIZATION"
)

var (
	dateWithSpacesPattern        = regexp.MustCompile(`(\d{4})年\s+(\d{1,2})\s*月`)
	numberedHeadingNoGap         = regexp.MustCompile(`(\d+(?:\.\d+)+)([\p{Han}A-Za-z])`)
	numberedHeadingWithDunda     = regexp.MustCompile(`(\d+、)([\p{Han}A-Za-z])`)
	researchStatusHeading        = regexp.MustCompile(`1\.3\s+国内外研究现状[？?]?`)
	researchStatusSubheading1    = regexp.MustCompile(`1\.3\.1(\s+国外研究现状)`)
	researchStatusSubheading2    = regexp.MustCompile(`1\.3\.2(\s+国内研究现状)`)
	conclusionSlashHeading       = regexp.MustCompile(`5\s+结论/总结`)
	shiheziDegreeThesisPattern   = regexp.MustCompile(`\[D\]\.石河子大学,?\s*2016\.`)
	paragraphPattern             = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`)
	paragraphPropertiesPattern   = regexp.MustCompile(`(?s)<w:pPr\b[^>]*>.*?</w:pPr>`)
	pPrRunPropertiesPattern      = regexp.MustCompile(`(?s)<w:rPr\b[^>]*>.*?</w:rPr>`)
	tablePropertiesPattern       = regexp.MustCompile(`(?s)<w:tblPr\b[^>]*>.*?</w:tblPr>`)
	runPattern                   = regexp.MustCompile(`(?s)<w:r(?:\s[^>]*)?>.*?</w:r>`)
	runPropertiesPattern         = regexp.MustCompile(`(?s)<w:rPr\b[^>]*>.*?</w:rPr>`)
	textPattern                  = regexp.MustCompile(`(?s)<w:t\b[^>]*>(.*?)</w:t>`)
	spacingPattern               = regexp.MustCompile(`<w:spacing\b[^>]*/>`)
	indentPattern                = regexp.MustCompile(`<w:ind\b[^>]*/>`)
	justificationPattern         = regexp.MustCompile(`<w:jc\b[^>]*/>`)
	runFontsPattern              = regexp.MustCompile(`<w:rFonts\b[^>]*/>`)
	runSizePattern               = regexp.MustCompile(`<w:sz\b[^>]*/>`)
	runComplexSizePattern        = regexp.MustCompile(`<w:szCs\b[^>]*/>`)
	runBoldPattern               = regexp.MustCompile(`<w:b\b[^>]*/>`)
	runComplexBoldPattern        = regexp.MustCompile(`<w:bCs\b[^>]*/>`)
	documentOpenPattern          = regexp.MustCompile(`<w:document\b[^>]*>`)
	sectionPropertiesPattern     = regexp.MustCompile(`(?s)<w:sectPr\b[^>]*/>|<w:sectPr\b[^>]*>.*?</w:sectPr>`)
	headerFooterReferencePattern = regexp.MustCompile(`<w:(?:headerReference|footerReference)\b[^>]*/>`)
	contentTypesEndPattern       = regexp.MustCompile(`</Types>`)
	relationshipsEndPattern      = regexp.MustCompile(`</Relationships>`)
	heading1Pattern              = regexp.MustCompile(`^\d+\s+\S+`)
	heading2Pattern              = regexp.MustCompile(`^\d+\.\d+\s+\S+`)
	heading3Pattern              = regexp.MustCompile(`^\d+\.\d+\.\d+\s+\S+`)
	heading4Pattern              = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+\s+\S+`)
	referenceEntryPattern        = regexp.MustCompile(`^\[\d+\]\s*`)
	tocPageRefPattern            = regexp.MustCompile(`^[IVXLCDMivxlcdm\d]+$`)
	singleLevelHeadingNoGap      = regexp.MustCompile(`^([1-9]|1[0-9]|20)([\p{Han}A-Za-z].*?)(\d*)$`)
	documentBodyChildPattern     = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>|<w:tbl(?:\s[^>]*)?>.*?</w:tbl>`)
	figureShapePattern           = regexp.MustCompile(`(?s)<w:(drawing|pict)\b|<v:(shape|group|rect|line|textbox)\b`)
	tableCaptionPattern          = regexp.MustCompile(`^(?:续)?表\s*\d+(?:[.-]\d+)?\s+.+`)
	figureCaptionPattern         = regexp.MustCompile(`^图\s*\d+(?:[.-]\d+)?\s+.+`)
)

type Issue struct {
	RuleID   string
	Kind     string
	Severity string
	Message  string
	Target   string
}

type Result struct {
	Passed   bool
	FixCount int
	Issues   []Issue
}

type debugTrace struct {
	enabled bool
	dir     string
}

type paragraphMatch struct {
	start int
	end   int
	text  string
}

type bodyChildMatch struct {
	start       int
	end         int
	text        string
	isParagraph bool
}

type textRule struct {
	id      string
	message string
	apply   func(string) (string, int)
}

type paragraphStyle struct {
	ruleID         string
	message        string
	eastAsiaFont   string
	asciiFont      string
	fontSize       string
	bold           bool
	firstLineChars *int
	beforeTwips    *int
	afterTwips     *int
	beforeLines    *int
	afterLines     *int
	line           string
	lineRule       string
	alignment      string
}

// FixDOCX 鏄?CQRWST 瑙勫垯鍖呯殑鈥滆嚜鍔ㄤ慨澶嶅叆鍙ｂ€濄€?//
// 杩欎釜鍑芥暟鍙礋璐ｅ鐞嗕竴涓凡缁忓瓨鍦ㄧ殑 docx 鏂囦欢锛?// 1. 鎵撳紑 docx 鍘嬬缉鍖咃紱
// 2. 鍙栧嚭 word/document.xml锛屼篃灏辨槸姝ｆ枃 OOXML锛?// 3. 渚濇鎵ц鏂囨湰瑙勫垯銆佹钀芥牱寮忚鍒欍€佸垎鑺傝鍒欙紱
// 4. 琛ラ綈椤电湁椤佃剼绛?docx 鍖呭唴鏂囦欢锛?// 5. 濡傛灉纭疄鍙戠敓浜嗕慨澶嶏紝鍐嶆妸淇敼鍚庣殑 document.xml 鍐欏洖鍘?docx銆?
func FixDOCX(ctx context.Context, docxPath string) (Result, error) {
	if err := validateInput(ctx, docxPath); err != nil {
		return Result{}, err
	}

	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		return Result{}, fmt.Errorf("open docx %q: %w", docxPath, err)
	}

	content, ok := pkg.Get(documentTarget)
	if !ok {
		return Result{}, fmt.Errorf("%s missing", documentTarget)
	}

	trace := newDebugTrace("fix", docxPath)
	fixed := string(content)
	trace.writeFile("00_fix_input_document.xml", fixed)
	if sanitized := sanitizeCQRWSTDocumentXML(fixed); sanitized != fixed {
		fixed = sanitized
		result := Result{
			FixCount: 1,
			Issues: []Issue{{
				RuleID:   "cqrwst-ooxml-wellformedness",
				Kind:     "repairable_xml",
				Severity: "error",
				Message:  "document.xml contains malformed paragraph run properties",
				Target:   documentTarget,
			}},
		}
		return fixSanitizedDOCX(ctx, pkg, docxPath, fixed, result, trace)
	}

	fixed, result := applyTextRules(fixed)
	trace.writeFile("01_after_text_rules.xml", fixed)

	fixed, styleResult := applyStyleRulesUntilStable(fixed, 5)

	result.FixCount += styleResult.FixCount
	result.Issues = append(result.Issues, styleResult.Issues...)
	trace.writeFile("02_after_style_rules.xml", fixed)

	fixed, sectionResult := applySectionRules(fixed)
	result.FixCount += sectionResult.FixCount
	result.Issues = append(result.Issues, sectionResult.Issues...)
	fixed = sanitizeCQRWSTDocumentXML(fixed)
	fixed, postSectionStyleResult := applyStyleRulesUntilStable(fixed, 5)
	result.FixCount += postSectionStyleResult.FixCount
	result.Issues = append(result.Issues, postSectionStyleResult.Issues...)
	fixed = sanitizeCQRWSTDocumentXML(fixed)
	trace.writeFile("03_after_section_rules.xml", fixed)

	packageResult := ensureHeaderFooterParts(pkg, fixed)
	result.FixCount += packageResult.FixCount
	result.Issues = append(result.Issues, packageResult.Issues...)

	trace.writeFile("04_fix_issues.txt", debugResultReport("fix", result))
	trace.writeFile("05_fix_paragraphs.txt", debugParagraphReport(fixed))

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	if result.FixCount > 0 {
		pkg.Set(documentTarget, []byte(fixed))
		if err := pkg.Write(docxPath); err != nil {
			return Result{}, fmt.Errorf("write fixed docx %q: %w", docxPath, err)
		}
	}

	result.Passed = len(result.Issues) == 0
	trace.printSummary("fix", result)
	return result, nil
}

func fixSanitizedDOCX(ctx context.Context, pkg *ooxmlpkg.DocxPackage, docxPath string, fixed string, result Result, trace debugTrace) (Result, error) {
	trace.writeFile("00a_after_xml_sanitize.xml", fixed)
	textFixed, textResult := applyTextRules(fixed)
	fixed = textFixed
	result.FixCount += textResult.FixCount
	result.Issues = append(result.Issues, textResult.Issues...)
	trace.writeFile("01_after_text_rules.xml", fixed)

	styleFixed, styleResult := applyStyleRules(fixed)
	fixed = styleFixed
	result.FixCount += styleResult.FixCount
	result.Issues = append(result.Issues, styleResult.Issues...)
	trace.writeFile("02_after_style_rules.xml", fixed)

	sectionFixed, sectionResult := applySectionRules(fixed)
	fixed = sanitizeCQRWSTDocumentXML(sectionFixed)
	result.FixCount += sectionResult.FixCount
	result.Issues = append(result.Issues, sectionResult.Issues...)
	trace.writeFile("03_after_section_rules.xml", fixed)

	packageResult := ensureHeaderFooterParts(pkg, fixed)
	result.FixCount += packageResult.FixCount
	result.Issues = append(result.Issues, packageResult.Issues...)
	trace.writeFile("04_fix_issues.txt", debugResultReport("fix", result))
	trace.writeFile("05_fix_paragraphs.txt", debugParagraphReport(fixed))
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	pkg.Set(documentTarget, []byte(fixed))
	if err := pkg.Write(docxPath); err != nil {
		return Result{}, fmt.Errorf("write fixed docx %q: %w", docxPath, err)
	}
	result.Passed = len(result.Issues) == 0
	trace.printSummary("fix", result)
	return result, nil
}

func CheckDOCX(ctx context.Context, docxPath string) (Result, error) {
	if err := validateInput(ctx, docxPath); err != nil {
		return Result{}, err
	}

	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		return Result{}, fmt.Errorf("open docx %q: %w", docxPath, err)
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return Result{}, fmt.Errorf("%s missing", documentTarget)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	trace := newDebugTrace("check", docxPath)
	checked := sanitizeCQRWSTDocumentXML(string(content))
	trace.writeFile("10_check_input_document.xml", string(content))
	trace.writeFile("10a_check_after_xml_sanitize.xml", checked)
	checked, result := applyTextRules(checked)
	trace.writeFile("11_check_after_text_rules.xml", checked)
	_, styleResult := applyStyleRules(checked)
	result.FixCount += styleResult.FixCount
	result.Issues = append(result.Issues, styleResult.Issues...)
	_, sectionResult := applySectionRules(checked)
	result.FixCount += sectionResult.FixCount
	result.Issues = append(result.Issues, sectionResult.Issues...)
	if sanitized := sanitizeCQRWSTDocumentXML(checked); sanitized != checked {
		checked = sanitized
		result.FixCount++
		result.Issues = append(result.Issues, Issue{
			RuleID:   "cqrwst-ooxml-wellformedness",
			Kind:     "repairable_xml",
			Severity: "error",
			Message:  "document.xml contains malformed paragraph run properties",
			Target:   documentTarget,
		})
	}
	packageResult := checkHeaderFooterParts(pkg, checked)
	result.FixCount += packageResult.FixCount
	result.Issues = append(result.Issues, packageResult.Issues...)
	result.Passed = len(result.Issues) == 0
	trace.writeFile("12_check_issues.txt", debugResultReport("check", result))
	trace.writeFile("13_check_paragraphs.txt", debugParagraphReport(checked))
	trace.printSummary("check", result)
	return result, nil
}

func validateInput(ctx context.Context, docxPath string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(docxPath) == "" {
		return fmt.Errorf("docx path is empty")
	}
	return nil
}

func newDebugTrace(operation string, docxPath string) debugTrace {
	if !isDebugEnabled() {
		return debugTrace{}
	}

	root := strings.TrimSpace(os.Getenv(debugDirEnv))
	if root == "" {
		root = filepath.Join(os.TempDir(), "cqrwst-debug")
	}
	cleanupOldDebugTraces(root, time.Now().Add(-7*24*time.Hour))
	dir := filepath.Join(root, debugTraceName(docxPath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("component=cqrwst_debug operation=%q stage=create error=%q", operation, err)
		return debugTrace{}
	}

	log.Printf("component=cqrwst_debug operation=%q stage=ready", operation)
	return debugTrace{enabled: true, dir: dir}
}

func cleanupOldDebugTraces(root string, cutoff time.Time) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}

func isDebugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(debugEnabledEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func debugTraceName(docxPath string) string {
	base := strings.TrimSuffix(filepath.Base(docxPath), filepath.Ext(docxPath))
	parent := filepath.Base(filepath.Dir(docxPath))
	name := strings.Trim(parent+"_"+base, "_")
	if name == "" || name == "." {
		name = "document"
	}
	return sanitizeDebugPathSegment(name)
}

func sanitizeDebugPathSegment(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	cleaned := strings.Trim(builder.String(), "_.")
	if cleaned == "" {
		return "document"
	}
	return cleaned
}

func (trace debugTrace) writeFile(name string, content string) {

	if !trace.enabled {
		return
	}

	path := filepath.Join(trace.dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		log.Printf("component=cqrwst_debug stage=write error=%q", err)
	}
}

func (trace debugTrace) printSummary(operation string, result Result) {
	if !trace.enabled {
		return
	}
	log.Printf(
		"component=cqrwst_debug operation=%q passed=%t fix_count=%d issues=%d",
		operation,
		result.Passed,
		result.FixCount,
		len(result.Issues),
	)
}

func debugResultReport(operation string, result Result) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("operation=%s\n", operation))
	builder.WriteString(fmt.Sprintf("passed=%t\n", result.Passed))
	builder.WriteString(fmt.Sprintf("fix_count=%d\n", result.FixCount))
	builder.WriteString(fmt.Sprintf("issues=%d\n", len(result.Issues)))
	for index, issue := range result.Issues {
		builder.WriteString(fmt.Sprintf(
			"issue[%03d] rule=%s kind=%s severity=%s target=%s message=%q\n",
			index,
			issue.RuleID,
			issue.Kind,
			issue.Severity,
			issue.Target,
			issue.Message,
		))
	}
	return builder.String()
}

func debugParagraphReport(content string) string {
	paragraphs := collectParagraphs(content)
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("paragraphs=%d\n", len(paragraphs)))
	section := ""
	for index, paragraph := range paragraphs {
		styleID := "none"
		style, ok := styleForParagraph(paragraph.text, &section)
		if ok {
			styleID = style.ruleID
		}
		paragraphXML := content[paragraph.start:paragraph.end]
		builder.WriteString(fmt.Sprintf(
			"p[%03d] text=%q style=%s section_state=%s sect_pr=%s\n",
			index,
			strings.TrimSpace(paragraph.text),
			styleID,
			section,
			debugSectionSummary(paragraphXML),
		))
	}
	return builder.String()
}

func debugSectionSummary(paragraphXML string) string {
	if !strings.Contains(paragraphXML, "<w:sectPr") {
		return "none"
	}

	parts := []string{"present"}
	if strings.Contains(paragraphXML, `w:type w:val="nextPage"`) {
		parts = append(parts, "nextPage")
	}
	if strings.Contains(paragraphXML, "w:headerReference") {
		parts = append(parts, "header")
	}
	if strings.Contains(paragraphXML, "w:footerReference") {
		parts = append(parts, "footer")
	}
	if strings.Contains(paragraphXML, `w:fmt="upperRoman"`) {
		parts = append(parts, "upperRoman")
	}
	if strings.Contains(paragraphXML, `w:fmt="decimal"`) {
		parts = append(parts, "decimal")
	}
	if strings.Contains(paragraphXML, "w:pgMar") {
		parts = append(parts, "margins")
	}
	return strings.Join(parts, ",")
}

func applyTextRules(content string) (string, Result) {
	result := Result{}
	updated := content

	next, count := normalizeTableCompatibilityProperties(updated)
	if count > 0 {
		updated = next
		result.FixCount += count
		for i := 0; i < count; i++ {
			result.Issues = append(result.Issues, Issue{
				RuleID:   "cqrwst-ooxml-table-compatibility",
				Kind:     "repairable_xml",
				Severity: "error",
				Message:  "table alignment values should be compatible with DOCX renderers",
				Target:   documentTarget,
			})
		}
	}

	for _, rule := range cqrwstTextRules() {
		next, count := rule.apply(updated)
		if count == 0 {
			continue
		}
		updated = next
		result.FixCount += count
		for i := 0; i < count; i++ {
			result.Issues = append(result.Issues, Issue{
				RuleID:   rule.id,
				Kind:     "repairable_text",
				Severity: "error",
				Message:  rule.message,
				Target:   documentTarget,
			})
		}
	}
	if allowContentNormalization() {
		next, count = applyParagraphVisibleTextRules(updated)
		if count > 0 {
			updated = next
			result.FixCount += count
			for i := 0; i < count; i++ {
				result.Issues = append(result.Issues, Issue{
					RuleID:   "cqrwst-paragraph-visible-text",
					Kind:     "repairable_text",
					Severity: "error",
					Message:  "paragraph visible text does not comply with CQRWST deterministic text rules",
					Target:   documentTarget,
				})
			}
		}
	}
	next, count = applyStructuralParagraphRules(updated)
	if count > 0 {
		updated = next
		result.FixCount += count
		for i := 0; i < count; i++ {
			result.Issues = append(result.Issues, Issue{
				RuleID:   "cqrwst-frontmatter-structure",
				Kind:     "repairable_text",
				Severity: "error",
				Message:  "abstract and keyword paragraphs should follow CQRWST front-matter structure",
				Target:   documentTarget,
			})
		}
	}
	next, count = pruneBlankParagraphsBeforeForcedPageStartTitles(updated)
	if count > 0 {
		updated = next
		result.FixCount += count
		for i := 0; i < count; i++ {
			result.Issues = append(result.Issues, Issue{
				RuleID:   "cqrwst-forced-new-page-spacing",
				Kind:     "repairable_text",
				Severity: "error",
				Message:  "references and acknowledgements should not keep extra blank paragraphs before forced new pages",
				Target:   documentTarget,
			})
		}
	}
	next, count = ensureFigureAndTableCaptions(updated)
	if count > 0 {
		updated = next
		result.FixCount += count
		for i := 0; i < count; i++ {
			result.Issues = append(result.Issues, Issue{
				RuleID:   "cqrwst-figure-table-caption",
				Kind:     "repairable_text",
				Severity: "error",
				Message:  "figures and tables should have chapter-based captions such as 鍥?.1 and 琛?.1",
				Target:   documentTarget,
			})
		}
	}

	return updated, result
}

func applyParagraphVisibleTextRules(content string) (string, int) {
	count := 0
	updated := paragraphPattern.ReplaceAllStringFunc(content, func(paragraph string) string {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if text == "" {
			return paragraph
		}
		normalized, ok := normalizeCQRWSTVisibleParagraphText(text)
		if !ok || normalized == text {
			return paragraph
		}
		count++
		return replaceParagraphVisibleText(paragraph, normalized)
	})
	return updated, count
}

func normalizeCQRWSTVisibleParagraphText(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if match := regexp.MustCompile(`^(\d{4})年\s*(\d{1,2})\s*月`).FindStringSubmatch(trimmed); len(match) == 3 {
		return match[1] + "年" + match[2] + "月", true
	}
	if match := regexp.MustCompile(`^\d+\.\d+\s*国内外研究现状[：:]?\s*(\d*)$`).FindStringSubmatch(trimmed); len(match) == 2 {
		return appendOptionalPageNumber("1.4 国内外研究现状", match[1]), true
	}
	if match := regexp.MustCompile(`^\d+\.\d+\.\d+\s*国外研究现状[：:]?\s*(\d*)$`).FindStringSubmatch(trimmed); len(match) == 2 {
		return appendOptionalPageNumber("1.4.1 国外研究现状", match[1]), true
	}
	if match := regexp.MustCompile(`^\d+\.\d+\.\d+\s*国内(?:研究)?现状[：:]?\s*(\d*)$`).FindStringSubmatch(trimmed); len(match) == 2 {
		return appendOptionalPageNumber("1.4.2 国内研究现状", match[1]), true
	}
	if strings.Contains(trimmed, "结论/总结") {
		return strings.ReplaceAll(trimmed, "结论/总结", "结论"), true
	}
	if match := regexp.MustCompile(`^(\d+(?:\.\d+)+)([\p{Han}A-Za-z].*)$`).FindStringSubmatch(trimmed); len(match) == 3 {
		return match[1] + " " + strings.TrimSpace(match[2]), true
	}
	if match := singleLevelHeadingNoGap.FindStringSubmatch(trimmed); len(match) == 4 && isSingleLevelHeadingTitle(match[2]) {
		return appendOptionalPageNumber(match[1]+" "+strings.TrimSpace(match[2]), match[3]), true
	}
	return text, false
}

func isSingleLevelHeadingTitle(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len([]rune(trimmed)) > 30 {
		return false
	}
	for _, keyword := range []string{"绪论", "引言", "研究", "对象", "方法", "结果", "分析", "结论", "总结", "参考文献", "致谢"} {
		if strings.Contains(trimmed, keyword) {
			return true
		}
	}
	return false
}

func isLikelyTOCHeadingEntry(text string) bool {
	trimmed := strings.TrimSpace(text)
	if strings.Contains(trimmed, "\t") {
		return true
	}
	if regexp.MustCompile(`^\d+(?:\.\d+)*\s+\S.*\d$`).MatchString(trimmed) {
		return true
	}
	return regexp.MustCompile(`^(摘要|关键词|Abstract|Keywords|参考文献|致谢)\s*.*\d$`).MatchString(trimmed)
}

func appendOptionalPageNumber(text string, pageNumber string) string {
	pageNumber = strings.TrimSpace(pageNumber)
	if pageNumber == "" {
		return text
	}
	return text + " " + pageNumber
}

func applyStyleRules(content string) (string, Result) {
	result := Result{}
	matches := paragraphPattern.FindAllStringIndex(content, -1)

	if len(matches) == 0 {
		result.Passed = true
		return content, result
	}

	var builder strings.Builder
	last := 0
	section := ""

	for _, match := range matches {
		start, end := match[0], match[1]
		builder.WriteString(content[last:start])
		paragraph := content[start:end]
		style, ok := styleForParagraph(extractParagraphText(paragraph), &section)
		if !ok {
			builder.WriteString(paragraph)
			last = end
			continue
		}

		text := extractParagraphText(paragraph)
		styled := applyParagraphStyle(paragraph, style)
		if isForcedPageStartTitle(text) {
			styled = ensureParagraphStartsWithPageBreak(styled)
		}
		if styled != paragraph {
			result.FixCount++
			result.Issues = append(result.Issues, Issue{
				RuleID:   style.ruleID,
				Kind:     "repairable_style",
				Severity: "error",
				Message:  style.message,
				Target:   documentTarget,
			})
		}
		builder.WriteString(styled)
		last = end
	}

	builder.WriteString(content[last:])
	result.Passed = len(result.Issues) == 0
	return builder.String(), result
}

func pruneBlankParagraphsBeforeForcedPageStartTitles(content string) (string, int) {
	return pruneBlankParagraphsBefore(content, func(text string) bool {
		return isForcedPageStartTitle(text)
	})
}

func pruneBlankParagraphsBefore(content string, isBoundary func(string) bool) (string, int) {
	paragraphs := collectParagraphs(content)
	if len(paragraphs) == 0 {
		return content, 0
	}

	remove := map[int]bool{}
	for index, paragraph := range paragraphs {
		if !isBoundary(paragraph.text) {
			continue
		}
		for previous := index - 1; previous >= 0; previous-- {
			if !isBlankParagraphText(paragraphs[previous].text) {
				break
			}
			remove[previous] = true
		}
	}
	if len(remove) == 0 {
		return content, 0
	}

	var builder strings.Builder
	last := 0
	for index, paragraph := range paragraphs {
		builder.WriteString(content[last:paragraph.start])
		if !remove[index] {
			builder.WriteString(content[paragraph.start:paragraph.end])
		}
		last = paragraph.end
	}
	builder.WriteString(content[last:])
	return builder.String(), len(remove)
}

func applyStructuralParagraphRules(content string) (string, int) {
	count := 0
	updated := paragraphPattern.ReplaceAllStringFunc(content, func(paragraph string) string {
		text := strings.TrimSpace(extractParagraphText(paragraph))
		if isStructuredFrontMatterParagraph(paragraph, text) {
			return paragraph
		}
		replacement, ok := structuredFrontMatterParagraph(text)
		if !ok {
			return paragraph
		}
		count++
		return replacement
	})
	return updated, count
}

func isStructuredFrontMatterParagraph(paragraph string, text string) bool {
	trimmed := strings.TrimSpace(text)
	if !(strings.HasPrefix(trimmed, "关键词：") ||
		strings.HasPrefix(trimmed, "关键词:") ||
		strings.HasPrefix(trimmed, "摘要：") ||
		strings.HasPrefix(trimmed, "摘要:") ||
		strings.HasPrefix(trimmed, "Abstract:") ||
		strings.HasPrefix(trimmed, "Abstract：") ||
		strings.HasPrefix(trimmed, "Key words:") ||
		strings.HasPrefix(trimmed, "Key words：") ||
		strings.HasPrefix(trimmed, "Keywords:") ||
		strings.HasPrefix(trimmed, "Keywords：")) {
		return false
	}
	return len(runPattern.FindAllString(paragraph, -1)) >= 2 &&
		strings.Contains(paragraph, `<w:b/>`) &&
		(strings.Contains(paragraph, `w:eastAsia="榛戜綋"`) || strings.Contains(paragraph, `w:ascii="Times New Roman"`))
}

func structuredFrontMatterParagraph(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if label, body, ok := splitLabeledParagraph(trimmed, []string{"摘要：", "摘要:"}); ok {
		if isLikelyTOCPageReference(body) {
			return "", false
		}
		return buildLabeledParagraphXML(label, body, keywordCNLabelStyle(), abstractCNBodyStyle(), abstractCNBodyStyle()), true
	}
	if label, body, ok := splitLabeledParagraph(trimmed, []string{"关键词：", "关键词:"}); ok {
		if isLikelyTOCPageReference(body) {
			return "", false
		}
		return buildKeywordParagraphXML(label, body, true), true
	}
	if label, body, ok := splitLabeledParagraph(trimmed, []string{"Abstract：", "Abstract:", "ABSTRACT：", "ABSTRACT:"}); ok {
		if isLikelyTOCPageReference(body) {
			return "", false
		}
		return buildLabeledParagraphXML(label, body, keywordENLabelStyle(), abstractENBodyStyle(), abstractENBodyStyle()), true
	}
	if label, body, ok := splitLabeledParagraph(trimmed, []string{"Keywords：", "Keywords:", "Key words：", "Key words:"}); ok {
		if isLikelyTOCPageReference(body) {
			return "", false
		}
		return buildKeywordParagraphXML(label, body, false), true
	}
	return "", false
}

func ensureFigureAndTableCaptions(content string) (string, int) {
	matches := documentBodyChildPattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return content, 0
	}
	semanticBlocks := buildSemanticBlocks(content)
	var builder strings.Builder
	last := 0
	count := 0
	currentChapter := "1"
	tableCounters := map[string]int{}
	figureCounters := map[string]int{}
	lastOutputWasTableCaption := false
	inBody := false
	for index, match := range matches {
		child := content[match[0]:match[1]]
		semanticBlock := semanticBlockByIndex(semanticBlocks, index)
		builder.WriteString(content[last:match[0]])
		last = match[1]

		if isParagraphXML(child) {
			text := strings.TrimSpace(extractParagraphText(child))
			if chapter, ok := chapterNumberFromHeading(text); ok {
				currentChapter = chapter
				inBody = true
			}
			if !inBody {
				if isGeneratedGenericCaption(text) {
					count++
					lastOutputWasTableCaption = false
					continue
				}
				builder.WriteString(child)
				lastOutputWasTableCaption = false
				continue
			}
			if isUnnumberedTableCaption(text) {
				tableCounters[currentChapter]++
				next := replaceParagraphVisibleText(child, numberedCaption("\u8868", currentChapter, tableCounters[currentChapter], trimCaptionLabelPrefix(text, "\u8868")))
				builder.WriteString(applyParagraphStyle(next, captionStyle()))
				count++
				lastOutputWasTableCaption = true
				continue
			}
			if isUnnumberedFigureCaption(text) {
				figureCounters[currentChapter]++
				next := replaceParagraphVisibleText(child, numberedCaption("\u56fe", currentChapter, figureCounters[currentChapter], trimCaptionLabelPrefix(text, "\u56fe")))
				builder.WriteString(applyParagraphStyle(next, captionStyle()))
				count++
				lastOutputWasTableCaption = false
				continue
			}
			builder.WriteString(child)
			lastOutputWasTableCaption = isTableCaption(text)
			if semanticBlock.Kind == semanticBlockFigure && currentChapter != "" && !nextNonBlankParagraphIsCaption(content, matches, index, "figure") {
				figureCounters[currentChapter]++
				caption := numberedCaption("\u56fe", currentChapter, figureCounters[currentChapter], figureCaptionNameFromContext(content, matches, index))
				builder.WriteString(buildParagraphXML(caption, captionStyle()))
				count++
				lastOutputWasTableCaption = false
			}
			continue
		}

		if semanticBlock.Kind == semanticBlockDataTable && inBody && isTableXML(child) && currentChapter != "" && !lastOutputWasTableCaption && !previousNonBlankParagraphIsCaption(content, matches, index, "table") {
			tableCounters[currentChapter]++
			caption := numberedCaption("\u8868", currentChapter, tableCounters[currentChapter], tableCaptionNameFromContext(semanticBlocks, index))
			builder.WriteString(buildParagraphXML(caption, captionStyle()))
			count++
		}
		builder.WriteString(child)
		lastOutputWasTableCaption = false
	}
	builder.WriteString(content[last:])
	return builder.String(), count
}

func isParagraphXML(child string) bool {
	return strings.HasPrefix(child, "<w:p") || strings.HasPrefix(child, "<w:p ")
}

func isTableXML(child string) bool {
	return strings.HasPrefix(child, "<w:tbl") || strings.HasPrefix(child, "<w:tbl ")
}

func containsFigureShape(paragraph string) bool {
	return figureShapePattern.MatchString(paragraph)
}

func chapterNumberFromHeading(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if templateprofile.IsBodyStartParagraph(trimmed) {
		return "1", true
	}
	if !heading1Pattern.MatchString(trimmed) || heading2Pattern.MatchString(trimmed) {
		return "", false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", false
	}
	return strings.Trim(fields[0], "."), true
}

func isTableCaption(text string) bool {
	return tableCaptionPattern.MatchString(strings.TrimSpace(text))
}

func isFigureCaption(text string) bool {
	return figureCaptionPattern.MatchString(strings.TrimSpace(text))
}

func isUnnumberedTableCaption(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "\u8868") && !strings.HasPrefix(trimmed, "\u7eed\u8868") && !isTableCaption(trimmed) && len([]rune(trimmed)) > 1
}

func isUnnumberedFigureCaption(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "\u56fe") && !isFigureCaption(trimmed) && len([]rune(trimmed)) > 1
}

func isGeneratedGenericCaption(text string) bool {
	trimmed := strings.TrimSpace(text)
	return regexp.MustCompile(`^图\d+(?:[.-]\d+)?\s+图示$`).MatchString(trimmed) ||
		regexp.MustCompile(`^表\d+(?:[.-]\d+)?\s+表格$`).MatchString(trimmed)
}

func trimCaptionLabelPrefix(text string, prefix string) string {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
	trimmed = strings.TrimLeft(trimmed, "：:.-— 　")
	if trimmed == "" {
		if prefix == "\u8868" {
			return "\u76f8\u5173\u6570\u636e"
		}
		return "\u56fe\u793a"
	}
	return trimmed
}

func previousNonBlankParagraphIsCaption(content string, matches [][]int, index int, kind string) bool {
	for i := index - 1; i >= 0; i-- {
		child := content[matches[i][0]:matches[i][1]]
		if !isParagraphXML(child) {
			continue
		}
		text := strings.TrimSpace(extractParagraphText(child))
		if text == "" {
			continue
		}
		if kind == "table" {
			return isTableCaption(text)
		}
		return isFigureCaption(text)
	}
	return false
}

func nextNonBlankParagraphIsCaption(content string, matches [][]int, index int, kind string) bool {
	for i := index + 1; i < len(matches); i++ {
		child := content[matches[i][0]:matches[i][1]]
		if !isParagraphXML(child) {
			continue
		}
		text := strings.TrimSpace(extractParagraphText(child))
		if text == "" {
			continue
		}
		if kind == "table" {
			return isTableCaption(text)
		}
		return isFigureCaption(text)
	}
	return false
}

func figureCaptionNameFromContext(content string, matches [][]int, index int) string {
	for i := index - 1; i >= 0; i-- {
		child := content[matches[i][0]:matches[i][1]]
		if !isParagraphXML(child) {
			continue
		}
		text := strings.TrimSpace(extractParagraphText(child))
		if text == "" {
			continue
		}
		if heading1Pattern.MatchString(text) || heading2Pattern.MatchString(text) || heading3Pattern.MatchString(text) || heading4Pattern.MatchString(text) {
			name := strings.TrimSpace(regexp.MustCompile(`^\d+(?:\.\d+)*\s+`).ReplaceAllString(text, ""))
			if strings.Contains(name, "\u56fe") {
				return name
			}
			break
		}
	}
	return "\u56fe\u793a"
}

func splitLabeledParagraph(text string, labels []string) (string, string, bool) {
	for _, label := range labels {
		if strings.HasPrefix(text, label) {
			body := strings.TrimSpace(strings.TrimPrefix(text, label))
			if body == "" {
				return label, "", false
			}
			return label, body, true
		}
	}
	return "", "", false
}

func isLikelyTOCPageReference(text string) bool {
	return tocPageRefPattern.MatchString(strings.TrimSpace(text))
}

func applyStyleRulesUntilStable(content string, maxPasses int) (string, Result) {
	updated := content
	result := Result{}
	if maxPasses < 1 {
		maxPasses = 1
	}
	for pass := 0; pass < maxPasses; pass++ {
		next, passResult := applyStyleRules(updated)
		result.FixCount += passResult.FixCount
		result.Issues = append(result.Issues, passResult.Issues...)
		updated = next
		if passResult.FixCount == 0 {
			break
		}
	}
	result.Passed = len(result.Issues) == 0
	return updated, result
}

func applySectionRules(content string) (string, Result) {
	result := Result{}
	updated := ensureRelationshipNamespace(content)
	updated = ensureSectionProperties(updated)
	if updated != content {
		result.FixCount = 1
		result.Issues = append(result.Issues, Issue{
			RuleID:   "cqrwst-section-page-header-footer",
			Kind:     "repairable_section",
			Severity: "error",
			Message:  "??????????????????????????",
			Target:   documentTarget,
		})
	}
	result.Passed = len(result.Issues) == 0
	return updated, result
}

func sanitizeCQRWSTDocumentXML(content string) string {
	return paragraphPropertiesPattern.ReplaceAllStringFunc(content, func(properties string) string {
		updated := properties
		for countOpenRunProperties(updated) > strings.Count(updated, "</w:rPr>") {
			index := strings.LastIndex(updated, "<w:rPr/>")
			if index < 0 {
				break
			}
			updated = updated[:index] + "</w:rPr>" + updated[index+len("<w:rPr/>"):]
		}
		return updated
	})
}

func normalizeTableCompatibilityProperties(content string) (string, int) {
	count := 0
	matches := documentBodyChildPattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return content, 0
	}

	var builder strings.Builder
	last := 0
	inBody := false
	for _, match := range matches {
		builder.WriteString(content[last:match[0]])
		child := content[match[0]:match[1]]
		text := strings.TrimSpace(extractParagraphText(child))
		if isBodyStartParagraph(text) {
			inBody = true
		}
		if inBody && strings.HasPrefix(child, "<w:tbl") {
			child = tablePropertiesPattern.ReplaceAllStringFunc(child, func(properties string) string {
				next := strings.ReplaceAll(properties, `w:val="start"`, `w:val="left"`)
				next = strings.ReplaceAll(next, `w:val="end"`, `w:val="right"`)
				if next != properties {
					count++
				}
				return next
			})
		}
		builder.WriteString(child)
		last = match[1]
	}
	builder.WriteString(content[last:])
	return builder.String(), count
}

func replaceParagraphVisibleText(paragraph string, text string) string {
	written := false
	return textPattern.ReplaceAllStringFunc(paragraph, func(node string) string {
		match := textPattern.FindStringSubmatch(node)
		if len(match) != 2 {
			return node
		}
		openEnd := strings.Index(node, ">")
		closeStart := strings.LastIndex(node, "</w:t>")
		if openEnd < 0 || closeStart < 0 || closeStart < openEnd {
			return node
		}
		if written {
			return node[:openEnd+1] + node[closeStart:]
		}
		written = true
		return node[:openEnd+1] + html.EscapeString(text) + node[closeStart:]
	})
}

func countOpenRunProperties(xmlText string) int {
	count := 0
	for _, match := range regexp.MustCompile(`<w:rPr(?:\s[^>/]*)?>`).FindAllString(xmlText, -1) {
		if strings.HasSuffix(match, "/>") {
			continue
		}
		count++
	}
	return count
}

func ensureRelationshipNamespace(content string) string {
	if strings.Contains(content, `xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`) {
		return content
	}
	return documentOpenPattern.ReplaceAllStringFunc(content, func(openTag string) string {
		return strings.TrimSuffix(openTag, ">") + ` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`
	})
}

func ensureSectionProperties(content string) string {
	if updated, ok := ensureMultiSectionProperties(content); ok {
		return updated
	}
	return ensureBodyEndSectionProperties(content, cqrwstSectionProperties())
}

func ensureMultiSectionProperties(content string) (string, bool) {
	children := collectBodyChildren(content)
	if len(children) == 0 {
		return content, false
	}

	tocIndex := -1
	abstractIndex := -1
	bodyIndex := -1
	for index, child := range children {
		if !child.isParagraph {
			continue
		}
		text := strings.TrimSpace(child.text)
		switch {
		case tocIndex == -1 && isTOCParagraph(text):
			tocIndex = index
		case abstractIndex == -1 && isChineseAbstractParagraph(text):
			abstractIndex = index
		case bodyIndex == -1 && isBodyStartParagraph(text):
			bodyIndex = index
		}
	}
	titleIndex := thesisTitleIndexBeforeAbstract(children, abstractIndex)

	sections := cqrwstSectionBreaks(tocIndex, abstractIndex, bodyIndex, titleIndex)
	if len(sections) == 0 {
		return content, false
	}
	sections, removeChildren := moveSectionBreaksBeforeTrailingBlankBodyChildren(children, sections)

	var builder strings.Builder
	last := 0
	for index, child := range children {
		builder.WriteString(content[last:child.start])
		if removeChildren[index] {
			last = child.end
			continue
		}
		childXML := content[child.start:child.end]
		if sectionProperties, ok := sections[index]; ok {
			if child.isParagraph {
				childXML = addOrReplaceParagraphSectionProperties(stripParagraphSectionProperties(childXML), sectionProperties)
			} else {
				childXML += buildSectionBreakParagraph(sectionProperties)
			}
		} else if child.isParagraph && strings.Contains(childXML, "<w:sectPr") {
			childXML = stripParagraphHeaderFooterReferences(childXML)
		}
		builder.WriteString(childXML)
		last = child.end
	}
	builder.WriteString(content[last:])

	return ensureBodyEndSectionProperties(builder.String(), cqrwstBodySectionProperties()), true
}

func moveSectionBreaksBeforeTrailingBlankBodyChildren(children []bodyChildMatch, sections map[int]string) (map[int]string, map[int]bool) {
	updatedSections := map[int]string{}
	removeChildren := map[int]bool{}
	for index, sectionProperties := range sections {
		target := index
		for target >= 0 && children[target].isParagraph && isBlankParagraphText(children[target].text) {
			removeChildren[target] = true
			target--
		}
		if target < 0 {
			updatedSections[index] = sectionProperties
			delete(removeChildren, index)
			continue
		}
		updatedSections[target] = sectionProperties
	}
	return updatedSections, removeChildren
}

func thesisTitleIndexBeforeAbstract(children []bodyChildMatch, abstractIndex int) int {
	if abstractIndex <= 0 || abstractIndex > len(children) {
		return -1
	}
	for index := abstractIndex - 1; index >= 0; index-- {
		child := children[index]
		if !child.isParagraph {
			continue
		}
		text := strings.TrimSpace(child.text)
		if text == "" {
			continue
		}
		if isLikelyThesisTitleParagraph(text) {
			return index
		}
		return -1
	}
	return -1
}

func cqrwstSectionBreaks(tocIndex int, abstractIndex int, bodyIndex int, titleIndex int) map[int]string {
	sections := map[int]string{}
	if titleIndex > 0 && abstractIndex > titleIndex && (tocIndex < 0 || abstractIndex < tocIndex) {
		sections[titleIndex-1] = cqrwstCoverSectionProperties()
		if tocIndex > abstractIndex {
			sections[tocIndex-1] = cqrwstAbstractSectionProperties()
			if bodyIndex > tocIndex {
				sections[bodyIndex-1] = cqrwstTOCSectionProperties()
			}
		} else if bodyIndex > abstractIndex {
			sections[bodyIndex-1] = cqrwstAbstractSectionProperties()
		}
		return sections
	}
	if abstractIndex > 0 && (tocIndex < 0 || abstractIndex < tocIndex) {
		sections[abstractIndex-1] = cqrwstCoverSectionProperties()
		if tocIndex > abstractIndex {
			sections[tocIndex-1] = cqrwstAbstractSectionProperties()
			if bodyIndex > tocIndex {
				sections[bodyIndex-1] = cqrwstTOCSectionProperties()
			}
		} else if bodyIndex > abstractIndex {
			sections[bodyIndex-1] = cqrwstAbstractSectionProperties()
		}
		return sections
	}

	if tocIndex > 0 {
		sections[tocIndex-1] = cqrwstCoverSectionProperties()
		if abstractIndex > tocIndex {
			sections[abstractIndex-1] = cqrwstTOCSectionProperties()
			if bodyIndex > abstractIndex {
				sections[bodyIndex-1] = cqrwstAbstractSectionProperties()
			}
		} else if bodyIndex > tocIndex {
			sections[bodyIndex-1] = cqrwstTOCSectionProperties()
		}
		return sections
	}

	if bodyIndex > 0 {
		sections[bodyIndex-1] = cqrwstCoverSectionProperties()
	}
	return sections
}

func isLikelyThesisTitleParagraph(text string) bool {
	trimmed := strings.TrimSpace(text)
	compact := normalizeChineseLabelText(trimmed)
	if compact == "" {
		return false
	}
	if isCoverFieldLabel(compact) || isTOCParagraph(trimmed) || isChineseAbstractParagraph(trimmed) || isBodyStartParagraph(trimmed) {
		return false
	}
	if strings.Contains(compact, "本科毕业论文") || strings.Contains(compact, "本科毕业设计") {
		return false
	}
	if regexp.MustCompile(`^\d{4}年\d{1,2}月$`).MatchString(compact) {
		return false
	}
	runeCount := len([]rune(compact))
	return runeCount >= 4 && runeCount <= 80
}

func collectParagraphs(content string) []paragraphMatch {
	matches := paragraphPattern.FindAllStringIndex(content, -1)
	paragraphs := make([]paragraphMatch, 0, len(matches))
	for _, match := range matches {
		paragraph := content[match[0]:match[1]]
		paragraphs = append(paragraphs, paragraphMatch{
			start: match[0],
			end:   match[1],
			text:  extractParagraphText(paragraph),
		})
	}
	return paragraphs
}

func collectBodyChildren(content string) []bodyChildMatch {
	matches := documentBodyChildPattern.FindAllStringIndex(content, -1)
	children := make([]bodyChildMatch, 0, len(matches))
	for _, match := range matches {
		child := content[match[0]:match[1]]
		isParagraph := strings.HasPrefix(child, "<w:p") || strings.HasPrefix(child, "<w:p ")
		children = append(children, bodyChildMatch{
			start:       match[0],
			end:         match[1],
			text:        extractParagraphText(child),
			isParagraph: isParagraph,
		})
	}
	return children
}

func buildSectionBreakParagraph(sectionProperties string) string {
	return "<w:p><w:pPr><w:sectPr>" + sectionProperties + "</w:sectPr></w:pPr></w:p>"
}

func addOrReplaceParagraphSectionProperties(paragraph string, sectionProperties string) string {
	sectionXML := "<w:sectPr>" + sectionProperties + "</w:sectPr>"
	if paragraphPropertiesPattern.MatchString(paragraph) {
		return paragraphPropertiesPattern.ReplaceAllStringFunc(paragraph, func(existing string) string {
			inner := paragraphPropertiesInner(existing)
			inner = sectionPropertiesPattern.ReplaceAllString(inner, "")
			return "<w:pPr>" + inner + sectionXML + "</w:pPr>"
		})
	}

	openEnd := strings.Index(paragraph, ">")
	if openEnd < 0 {
		return paragraph
	}
	return paragraph[:openEnd+1] + "<w:pPr>" + sectionXML + "</w:pPr>" + paragraph[openEnd+1:]
}

func stripParagraphHeaderFooterReferences(paragraph string) string {
	return paragraphPropertiesPattern.ReplaceAllStringFunc(paragraph, func(existing string) string {
		return headerFooterReferencePattern.ReplaceAllString(existing, "")
	})
}

func stripParagraphSectionProperties(paragraph string) string {
	if !strings.Contains(paragraph, "<w:sectPr") {
		return paragraph
	}
	return paragraphPropertiesPattern.ReplaceAllStringFunc(paragraph, func(existing string) string {
		inner := paragraphPropertiesInner(existing)
		inner = sectionPropertiesPattern.ReplaceAllString(inner, "")
		if strings.TrimSpace(inner) == "" {
			return ""
		}
		return "<w:pPr>" + inner + "</w:pPr>"
	})
}

func paragraphPropertiesInner(properties string) string {
	openEnd := strings.Index(properties, ">")
	closeStart := strings.LastIndex(properties, "</w:pPr>")
	if openEnd < 0 || closeStart < 0 || closeStart < openEnd {
		return ""
	}
	return properties[openEnd+1 : closeStart]
}

func ensureBodyEndSectionProperties(content string, sectionProperties string) string {
	required := "<w:sectPr>" + sectionProperties + "</w:sectPr>"
	bodyEnd := strings.LastIndex(content, "</w:body>")
	if bodyEnd < 0 {
		return content + required
	}

	bodyPrefix := content[:bodyEnd]
	bodySuffix := content[bodyEnd:]
	lastParagraphEnd := strings.LastIndex(bodyPrefix, "</w:p>")
	searchStart := lastParagraphEnd + len("</w:p>")
	if lastParagraphEnd < 0 {
		searchStart = strings.LastIndex(bodyPrefix, ">") + 1
		if searchStart < 0 {
			searchStart = 0
		}
	}

	tail := bodyPrefix[searchStart:]
	if match := sectionPropertiesPattern.FindStringIndex(tail); match != nil {
		after := strings.TrimSpace(tail[match[1]:])
		if after == "" {
			return bodyPrefix[:searchStart+match[0]] + required + bodySuffix
		}
	}
	return bodyPrefix + required + bodySuffix
}

func isTOCParagraph(text string) bool {
	return strings.ReplaceAll(strings.TrimSpace(text), " ", "") == "\u76ee\u5f55"
}

func isChineseAbstractParagraph(text string) bool {
	trimmed := strings.ReplaceAll(strings.TrimSpace(text), " ", "")
	return strings.HasPrefix(text, "\u6458\u8981\uff1a") ||
		strings.HasPrefix(text, "\u6458\u8981:") ||
		trimmed == "\u6458\u8981"
}

func isBodyStartParagraph(text string) bool {
	trimmed := strings.TrimSpace(text)
	return templateprofile.IsBodyStartParagraph(trimmed) && !isLikelyTOCHeadingEntry(trimmed)
}

func cqrwstSectionProperties() string {
	return cqrwstBodySectionProperties()
}

func cqrwstCoverSectionProperties() string {
	return `<w:type w:val="nextPage"/>`
}

func cqrwstTOCSectionProperties() string {
	return `<w:type w:val="nextPage"/>` + cqrwstHeaderReference() + cqrwstPageSettings()
}

func cqrwstAbstractSectionProperties() string {
	return `<w:type w:val="nextPage"/>` +
		cqrwstHeaderFooterReferences() +
		cqrwstPageSettings() +
		cqrwstPageNumber("upperRoman")
}

func cqrwstBodySectionProperties() string {
	return cqrwstHeaderFooterReferences() +
		cqrwstPageSettings() +
		cqrwstPageNumber("decimal")
}

func cqrwstHeaderFooterReferences() string {
	return cqrwstHeaderReference() +
		`<w:footerReference w:type="default" r:id="rIdCQRWSTFooter1"/>`
}

func cqrwstHeaderReference() string {
	return `<w:headerReference w:type="default" r:id="rIdCQRWSTHeader1"/>`
}

func cqrwstPageSettings() string {
	return `<w:pgSz w:w="11906" w:h="16838"/>` +
		`<w:pgMar w:top="1418" w:right="1134" w:bottom="1134" w:left="1418" w:header="851" w:footer="992" w:gutter="0"/>`
}

func cqrwstPageNumber(format string) string {
	return fmt.Sprintf(`<w:pgNumType w:start="1" w:fmt="%s"/>`, format)
}

func ensureHeaderFooterParts(pkg *ooxmlpkg.DocxPackage, documentXML string) Result {
	result := Result{}
	headerXML := cqrwstHeaderXML(extractHeaderTextFromDocumentXML(documentXML))
	if count := ensureCQRWSTHeaderParts(pkg, headerXML); count > 0 {
		result.FixCount += count
		for i := 0; i < count; i++ {
			result.Issues = append(result.Issues, packageIssue("cqrwst-header-part", "CQRWST header should use compact text and 0.5pt double divider", "word/header*.xml"))
		}
	}
	if ensurePackageEntry(pkg, "word/footer1.xml", cqrwstFooterXML()) {
		result.FixCount++
		result.Issues = append(result.Issues, packageIssue("cqrwst-footer-part", "CQRWST footer should use centered page fields", "word/footer1.xml"))
	}
	packageFixes := ensureCQRWSTHeaderFooterRelationships(pkg, headerXML)
	if packageFixes > 0 {
		result.FixCount += packageFixes
		result.Issues = append(result.Issues, packageIssue("cqrwst-package-relationships", "document relationships and content types should include generated CQRWST header and footer", "word/_rels/document.xml.rels"))
	}
	result.Passed = len(result.Issues) == 0

	return result
}

func ensureCQRWSTHeaderFooterRelationships(pkg *ooxmlpkg.DocxPackage, headerXML string) int {
	count := 0
	count += ooxmlpatch.EnsureFixedRelationshipPart(pkg, ooxmlpatch.FixedRelationshipPartSpec{
		PartName:         "word/header1.xml",
		Content:          headerXML,
		RelationshipID:   "rIdCQRWSTHeader1",
		RelationshipType: ooxmlpatch.HeaderRelationshipType,
		ContentType:      ooxmlpatch.HeaderContentType,
	})
	count += ooxmlpatch.EnsureFixedRelationshipPart(pkg, ooxmlpatch.FixedRelationshipPartSpec{
		PartName:         "word/footer1.xml",
		Content:          cqrwstFooterXML(),
		RelationshipID:   "rIdCQRWSTFooter1",
		RelationshipType: ooxmlpatch.FooterRelationshipType,
		ContentType:      ooxmlpatch.FooterContentType,
	})
	return count
}

func ensureCQRWSTHeaderParts(pkg *ooxmlpkg.DocxPackage, headerXML string) int {
	count := 0
	if ensurePackageEntry(pkg, "word/header1.xml", headerXML) {
		count++
	}
	for index := 2; index <= 12; index++ {
		name := fmt.Sprintf("word/header%d.xml", index)
		if _, ok := pkg.Get(name); !ok {
			continue
		}
		if ensurePackageEntry(pkg, name, headerXML) {
			count++
		}
	}
	return count
}

func checkHeaderFooterParts(pkg *ooxmlpkg.DocxPackage, documentXML string) Result {
	result := Result{}
	headerXML := cqrwstHeaderXML(extractHeaderTextFromDocumentXML(documentXML))
	if !strings.Contains(documentXML, `r:id="rIdCQRWSTHeader1"`) || !strings.Contains(documentXML, `r:id="rIdCQRWSTFooter1"`) {
		result.FixCount++
		result.Issues = append(result.Issues, packageIssue("cqrwst-section-page-header-footer", "??????????????????????????", documentTarget))
	}
	if !entryEquals(pkg, "word/header1.xml", headerXML) {
		result.FixCount++
		result.Issues = append(result.Issues, packageIssue("cqrwst-header-part", "???????????????????", "word/header1.xml"))
	}
	if !entryEquals(pkg, "word/footer1.xml", cqrwstFooterXML()) {
		result.FixCount++
		result.Issues = append(result.Issues, packageIssue("cqrwst-footer-part", "???????????????????", "word/footer1.xml"))
	}
	if content, ok := pkg.Get("word/_rels/document.xml.rels"); !ok || ensureDocumentRelationships(string(content)) != string(content) {
		result.FixCount++
		result.Issues = append(result.Issues, packageIssue("cqrwst-document-rels", "document.xml ???????????", "word/_rels/document.xml.rels"))
	}
	if content, ok := pkg.Get("[Content_Types].xml"); !ok || ensureContentTypes(string(content)) != string(content) {
		result.FixCount++
		result.Issues = append(result.Issues, packageIssue("cqrwst-content-types", "Content Types ?????????", "[Content_Types].xml"))
	}
	result.Passed = len(result.Issues) == 0
	return result
}

func ensurePackageEntry(pkg *ooxmlpkg.DocxPackage, name string, content string) bool {
	current, ok := pkg.Get(name)
	if ok && string(current) == content {
		return false
	}
	pkg.Set(name, []byte(content))
	return true
}

func entryEquals(pkg *ooxmlpkg.DocxPackage, name string, content string) bool {
	current, ok := pkg.Get(name)
	return ok && string(current) == content
}

func packageIssue(ruleID string, message string, target string) Issue {
	return Issue{RuleID: ruleID, Kind: "repairable_section", Severity: "error", Message: message, Target: target}
}

func ensureDocumentRelationships(content string) string {
	if strings.TrimSpace(content) == "" {
		return cqrwstDocumentRelsXML()
	}
	updated := content
	if !strings.Contains(updated, `Id="rIdCQRWSTHeader1"`) {
		updated = relationshipsEndPattern.ReplaceAllString(updated, cqrwstHeaderRelationship()+"</Relationships>")
	}
	if !strings.Contains(updated, `Id="rIdCQRWSTFooter1"`) {
		updated = relationshipsEndPattern.ReplaceAllString(updated, cqrwstFooterRelationship()+"</Relationships>")
	}
	return updated
}

func ensureContentTypes(content string) string {
	updated := content
	if !strings.Contains(updated, `PartName="/word/header1.xml"`) {
		updated = contentTypesEndPattern.ReplaceAllString(updated, cqrwstHeaderContentType()+"</Types>")
	}
	if !strings.Contains(updated, `PartName="/word/footer1.xml"`) {
		updated = contentTypesEndPattern.ReplaceAllString(updated, cqrwstFooterContentType()+"</Types>")
	}
	return updated
}

func cqrwstDocumentRelsXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + cqrwstHeaderRelationship() + cqrwstFooterRelationship() + `</Relationships>`
}

func cqrwstHeaderRelationship() string {
	return `<Relationship Id="rIdCQRWSTHeader1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>`
}

func cqrwstFooterRelationship() string {
	return `<Relationship Id="rIdCQRWSTFooter1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>`
}

func cqrwstHeaderContentType() string {
	return `<Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/>`
}

func cqrwstFooterContentType() string {
	return `<Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>`
}

func cqrwstHeaderXML(header string) string {
	header = normalizeCQRWSTHeaderText(header)
	if header == "" {
		header = defaultHeaderText()
	}
	return `<?xml version="1.0" encoding="UTF-8"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:jc w:val="center"/><w:pBdr><w:bottom w:val="double" w:sz="4" w:space="0" w:color="000000"/></w:pBdr></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:eastAsia="` + fontSimSun() + `"/><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr><w:t>` + html.EscapeString(header) + `</w:t></w:r></w:p></w:hdr>`
}

func normalizeCQRWSTHeaderText(header string) string {
	header = strings.TrimSpace(header)
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, header)
}

func cqrwstFooterXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:eastAsia="` + fontSimSun() + `"/><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr><w:t>` + textPagePrefix() + `</w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:r><w:t>` + textPageMiddle() + `</w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> NUMPAGES </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:r><w:t>` + textPageSuffix() + `</w:t></w:r></w:p></w:ftr>`
}

func fontSimSun() string { return "\u5b8b\u4f53" }

func defaultHeaderText() string {
	return "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u9662XXX\u5c4aXXX\u4e13\u4e1a\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587"
}

func coverFieldValues(content string) []string {
	values := []string{}
	for _, child := range documentBodyChildPattern.FindAllString(content, -1) {
		text := strings.TrimSpace(extractParagraphText(child))
		if text == "" {
			continue
		}
		if isBodyStartParagraph(text) || isChineseAbstractParagraph(text) || isTOCParagraph(text) {
			break
		}
		runs := extractTextRuns(child)
		if len(runs) == 0 {
			values = append(values, text)
			continue
		}
		values = append(values, runs...)
	}
	return values
}

func extractTextRuns(xmlText string) []string {
	values := []string{}
	for _, match := range textPattern.FindAllStringSubmatch(xmlText, -1) {
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(html.UnescapeString(match[1]))
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func extractHeaderTextFromDocumentXML(content string) string {
	values := coverFieldValues(content)
	major := valueAfterLabel(values, "\u4e13\u4e1a")
	grade := gradeYearFromClass(valueAfterLabel(values, "\u73ed\u7ea7"))
	docType := "\u8bba\u6587"
	for _, text := range values {
		if strings.Contains(text, "\u6bd5\u4e1a\u8bbe\u8ba1") && !strings.Contains(text, "\u6bd5\u4e1a\u8bba\u6587/\u8bbe\u8ba1") {
			docType = "\u8bbe\u8ba1"
			break
		}
	}

	if major == "" {
		major = "XXX"
	}
	if grade == "" {
		grade = "XXX"
	}
	return "\u91cd\u5e86\u4eba\u6587\u79d1\u6280\u5b66\u9662" + grade + "\u5c4a" + major + "\u4e13\u4e1a\u672c\u79d1\u6bd5\u4e1a" + docType
}

func valueAfterLabel(values []string, label string) string {
	for index, text := range values {
		normalized := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(text, "\uff1a"), ":"))
		if normalized == label && index+1 < len(values) {
			return cleanCoverFieldValue(joinCoverFieldValueRuns(values[index+1:]))
		}
		if strings.HasPrefix(text, label+"\uff1a") {
			return cleanCoverFieldValue(strings.TrimPrefix(text, label+"\uff1a"))
		}
		if strings.HasPrefix(text, label+":") {
			return cleanCoverFieldValue(strings.TrimPrefix(text, label+":"))
		}
	}
	return ""
}

func joinCoverFieldValueRuns(values []string) string {
	parts := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if isCoverFieldLabel(value) {
			break
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, "")
}

func isCoverFieldLabel(value string) bool {
	normalized := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(value, "\uff1a"), ":"))
	switch normalized {
	case "\u9898\u76ee", "\u5b66\u9662", "\u4e13\u4e1a", "\u73ed\u7ea7", "\u5b66\u53f7", "\u59d3\u540d", "\u6307\u5bfc\u6559\u5e08", "\u6559\u5e08", "\u5b66\u751f":
		return true
	default:
		return false
	}
}

func cleanCoverFieldValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\uff1a: \t\r\n")
	return value
}

func gradeYearFromClass(className string) string {
	match := regexp.MustCompile(`(20\d{2})\s*级`).FindStringSubmatch(className)
	if len(match) != 2 {
		return ""
	}
	year := 0
	if _, err := fmt.Sscanf(match[1], "%d", &year); err != nil || year == 0 {
		return ""
	}
	return fmt.Sprintf("%d", year+4)
}

func textPagePrefix() string { return "\u7b2c" }
func textPageMiddle() string { return "\u9875 \u5171" }
func textPageSuffix() string { return "\u9875" }

func styleForParagraph(text string, section *string) (paragraphStyle, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return paragraphStyle{}, false
	}

	switch {
	case trimmed == "\u6458 \u8981" || trimmed == "\u6458\u3000\u3000\u8981" || trimmed == "\u6458\u8981":
		*section = "abstract_cn"
		return abstractCNTitleStyle(), true
	case trimmed == "\u6458\u8981\uff1a" || trimmed == "\u6458\u8981:":
		*section = "abstract_cn"
		return abstractCNLabelStyle(), true
	case trimmed == "\u5173\u952e\u8bcd\uff1a" || trimmed == "\u5173\u952e\u8bcd:":
		*section = ""
		return abstractCNLabelStyle(), true
	case strings.HasPrefix(trimmed, "\u5173\u952e\u8bcd\uff1a") || strings.HasPrefix(trimmed, "\u5173\u952e\u8bcd:"):
		*section = ""
		return paragraphStyle{}, false
	case trimmed == "Abstract":
		*section = "abstract_en"
		return abstractENTitleStyle(), true
	case trimmed == "Abstract\uff1a" || trimmed == "Abstract:":
		*section = "abstract_en"
		return abstractENLabelStyle(), true
	case trimmed == "Key words\uff1a" || trimmed == "Key words:" || trimmed == "Keywords\uff1a" || trimmed == "Keywords:":
		*section = ""
		return abstractENLabelStyle(), true
	case strings.HasPrefix(trimmed, "Keywords\uff1a") || strings.HasPrefix(trimmed, "Keywords:") || strings.HasPrefix(trimmed, "Key words\uff1a") || strings.HasPrefix(trimmed, "Key words:"):
		*section = ""
		return paragraphStyle{}, false
	case isLikelyTOCHeadingEntry(trimmed):
		return paragraphStyle{}, false
	case isTableCaption(trimmed) || isFigureCaption(trimmed):
		return captionStyle(), true
	case heading4Pattern.MatchString(trimmed):
		*section = "body"
		return heading4Style(), true
	case heading3Pattern.MatchString(trimmed):
		*section = "body"
		return heading3Style(), true
	case heading2Pattern.MatchString(trimmed):
		*section = "body"
		return heading2Style(), true
	case heading1Pattern.MatchString(trimmed):
		*section = "body"
		return heading1Style(), true
	case normalizeChineseLabelText(trimmed) == "\u53c2\u8003\u6587\u732e":
		*section = "references"
		return referencesTitleStyle(), true
	case referenceEntryPattern.MatchString(trimmed):
		return referenceStyle(), true
	case isAcknowledgementsTitle(trimmed):
		*section = "body"
		return heading1Style(), true
	case strings.HasPrefix(trimmed, "\u9644\u5f55"):
		*section = "body"
		return heading1Style(), true
	case *section == "abstract_cn":
		return abstractCNBodyStyle(), true
	case *section == "abstract_en":
		return abstractENBodyStyle(), true
	default:
		if *section == "body" || *section == "references" {
			return bodyStyle(), true
		}
		return paragraphStyle{}, false
	}
}

func abstractCNTitleStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-abstract-cn-title-style", message: "Chinese abstract title style", eastAsiaFont: "\u9ed1\u4f53", asciiFont: "Times New Roman", fontSize: "32", bold: true, line: "360", alignment: "center"}
}

func abstractCNLabelStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-abstract-cn-label-style", message: "Chinese abstract label style", eastAsiaFont: "\u9ed1\u4f53", asciiFont: "Times New Roman", fontSize: "30", bold: true, firstLineChars: intPtr(200), afterLines: intPtr(200), line: "360"}
}

func abstractCNBodyStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-abstract-cn-body-style", message: "Chinese abstract body style", eastAsiaFont: "\u5b8b\u4f53", asciiFont: "Times New Roman", fontSize: "24", firstLineChars: intPtr(200), afterLines: intPtr(200), line: "360", alignment: "both"}
}

func keywordCNLabelStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-keyword-cn-label-style", message: "Chinese keywords label style", eastAsiaFont: "\u9ed1\u4f53", asciiFont: "Times New Roman", fontSize: "24", bold: true}
}

func keywordCNBodyStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-keyword-cn-body-style", message: "Chinese keywords body style", eastAsiaFont: "\u5b8b\u4f53", asciiFont: "Times New Roman", fontSize: "24"}
}

func keywordParagraphStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-keyword-paragraph-style", message: "Keywords paragraph style", line: "360", alignment: "both"}
}

func abstractENTitleStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-abstract-en-title-style", message: "English abstract title style", eastAsiaFont: "Times New Roman", asciiFont: "Times New Roman", fontSize: "30", bold: true, line: "360", alignment: "center"}
}

func abstractENLabelStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-abstract-en-label-style", message: "English abstract label style", eastAsiaFont: "Times New Roman", asciiFont: "Times New Roman", fontSize: "30", bold: true, firstLineChars: intPtr(200), afterLines: intPtr(200), line: "360"}
}

func keywordENLabelStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-keyword-en-label-style", message: "English keywords label style", eastAsiaFont: "Times New Roman", asciiFont: "Times New Roman", fontSize: "24", bold: true}
}

func keywordENBodyStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-keyword-en-body-style", message: "English keywords body style", eastAsiaFont: "Times New Roman", asciiFont: "Times New Roman", fontSize: "24"}
}

func abstractENBodyStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-abstract-en-body-style", message: "English abstract body style", eastAsiaFont: "Times New Roman", asciiFont: "Times New Roman", fontSize: "24", afterLines: intPtr(200), line: "360", alignment: "both"}
}

func heading1Style() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-heading1-style", message: "Heading 1 style", eastAsiaFont: "\u5b8b\u4f53", asciiFont: "Times New Roman", fontSize: "32", bold: true, beforeTwips: intPtr(240), afterTwips: intPtr(240), line: "360", alignment: "center"}
}

func heading2Style() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-heading2-style", message: "Heading 2 style", eastAsiaFont: "\u5b8b\u4f53", asciiFont: "Times New Roman", fontSize: "30", bold: true, line: "360", alignment: "left"}
}

func heading3Style() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-heading3-style", message: "Heading 3 style", eastAsiaFont: "\u5b8b\u4f53", asciiFont: "Times New Roman", fontSize: "28", bold: true, line: "360", alignment: "left"}
}

func heading4Style() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-heading4-style", message: "Heading 4 style", eastAsiaFont: "\u5b8b\u4f53", asciiFont: "Times New Roman", fontSize: "28", line: "360", alignment: "left"}
}

func bodyStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-body-style", message: "Body style", eastAsiaFont: "\u5b8b\u4f53", asciiFont: "Times New Roman", fontSize: "24", firstLineChars: intPtr(200), line: "360", alignment: "both"}
}

func referenceStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-reference-style", message: "Reference entry style", eastAsiaFont: "\u5b8b\u4f53", asciiFont: "Times New Roman", fontSize: "21", firstLineChars: intPtr(0), line: "360"}
}

func referencesTitleStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-references-title-style", message: "References title style", eastAsiaFont: "\u5b8b\u4f53", asciiFont: "Times New Roman", fontSize: "28", bold: true, line: "360", alignment: "center"}
}

func captionStyle() paragraphStyle {
	return paragraphStyle{ruleID: "cqrwst-figure-table-caption-style", message: "Figure and table caption style", eastAsiaFont: "\u5b8b\u4f53", asciiFont: "Times New Roman", fontSize: "21", firstLineChars: intPtr(0), line: "360", alignment: "center"}
}

func cqrwstTextRules() []textRule {
	if !allowContentNormalization() {
		return []textRule{
			{id: "cqrwst-visible-html-entity-decode", message: "visible text should not expose HTML entities", apply: decodeDoubleEscapedVisibleEntities},
		}
	}
	return []textRule{
		{id: "cqrwst-visible-html-entity-decode", message: "visible text should not expose HTML entities", apply: decodeDoubleEscapedVisibleEntities},
		{id: "cqrwst-cover-date-spacing", message: "date spacing should be normalized", apply: func(s string) (string, int) {
			return replaceRegexpChanged(s, dateWithSpacesPattern, func(match []string) string { return match[1] + "\u5e74" + match[2] + "\u6708" })
		}},
		{id: "cqrwst-heading-number-with-dunda", message: "heading number should have a following space", apply: func(s string) (string, int) {
			return replaceRegexpChanged(s, numberedHeadingWithDunda, func(match []string) string { return match[1] + " " + match[2] })
		}},
		{id: "cqrwst-heading-number-gap", message: "heading number should have a following space", apply: func(s string) (string, int) {
			return replaceRegexpChanged(s, numberedHeadingNoGap, func(match []string) string { return match[1] + " " + match[2] })
		}},
		{id: "cqrwst-research-status-heading-number", message: "research status heading should be 1.4", apply: func(s string) (string, int) {
			return replaceRegexpChanged(s, researchStatusHeading, func([]string) string { return "1.4 \u56fd\u5185\u5916\u7814\u7a76\u73b0\u72b6" })
		}},
		{id: "cqrwst-research-status-subheading-number", message: "research status subheadings should follow 1.4", apply: func(s string) (string, int) {
			next, count1 := replaceRegexpChanged(s, researchStatusSubheading1, func(match []string) string { return "1.4.1" + match[1] })
			next, count2 := replaceRegexpChanged(next, researchStatusSubheading2, func(match []string) string { return "1.4.2" + match[1] })
			return next, count1 + count2
		}},
		{id: "cqrwst-conclusion-heading", message: "conclusion heading should not use slash form", apply: func(s string) (string, int) {
			return replaceRegexpChanged(s, conclusionSlashHeading, func([]string) string { return "5 \u7ed3\u8bba" })
		}},
		{id: "cqrwst-chi-square-statistic", message: "chi-square statistic should use chi-square", apply: func(s string) (string, int) { return replaceLiteralChanged(s, "2/Z", "\u03c7\u00b2/Z") }},
		{id: "cqrwst-wald-chi-square", message: "Wald statistic should use Wald chi-square", apply: func(s string) (string, int) { return replaceLiteralChanged(s, "Wald2", "Wald \u03c7\u00b2") }},
		{id: "cqrwst-degree-thesis-place", message: "degree thesis references should include saved place", apply: func(s string) (string, int) {
			return replaceRegexpChanged(s, shiheziDegreeThesisPattern, func([]string) string { return "[D].\u77f3\u6cb3\u5b50: \u77f3\u6cb3\u5b50\u5927\u5b66,2016." })
		}},
	}
}

func allowContentNormalization() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(contentNormalizeEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func applyParagraphStyle(paragraph string, style paragraphStyle) string {
	if style.ruleID == "cqrwst-references-title-style" {
		paragraph = removeParagraphPropertyRunProperties(paragraph)
	}
	paragraph = applyParagraphProperties(paragraph, style)
	return runPattern.ReplaceAllStringFunc(paragraph, func(run string) string {
		return applyRunProperties(run, style)
	})
}

func removeParagraphPropertyRunProperties(paragraph string) string {
	return paragraphPropertiesPattern.ReplaceAllStringFunc(paragraph, func(properties string) string {
		return pPrRunPropertiesPattern.ReplaceAllString(properties, "")
	})
}

func applyParagraphProperties(paragraph string, style paragraphStyle) string {
	updated, _ := ooxmlpatch.ApplyParagraphProperties(paragraph, paragraphStyleToPatchSpec(style))
	return updated
}

func buildParagraphProperties(style paragraphStyle) string {
	var builder strings.Builder

	if style.line != "" || style.beforeTwips != nil || style.afterTwips != nil || style.beforeLines != nil || style.afterLines != nil {
		builder.WriteString(`<w:spacing`)
		if style.beforeTwips != nil {
			builder.WriteString(fmt.Sprintf(` w:before="%d"`, *style.beforeTwips))
		}
		if style.afterTwips != nil {
			builder.WriteString(fmt.Sprintf(` w:after="%d"`, *style.afterTwips))
		}
		if style.beforeLines != nil {
			builder.WriteString(fmt.Sprintf(` w:beforeLines="%d"`, *style.beforeLines))
		}
		if style.afterLines != nil {
			builder.WriteString(fmt.Sprintf(` w:afterLines="%d"`, *style.afterLines))
		}
		if style.line != "" {
			lineRule := style.lineRule
			if lineRule == "" {
				lineRule = "auto"
			}
			builder.WriteString(fmt.Sprintf(` w:line="%s" w:lineRule="%s"`, style.line, lineRule))
		}
		builder.WriteString(`/>`)
	}
	if style.firstLineChars != nil {
		builder.WriteString(fmt.Sprintf(`<w:ind w:firstLineChars="%d"/>`, *style.firstLineChars))
	}
	if style.alignment != "" {
		builder.WriteString(fmt.Sprintf(`<w:jc w:val="%s"/>`, style.alignment))
	}

	return builder.String()
}

func applyRunProperties(run string, style paragraphStyle) string {
	updated, _ := ooxmlpatch.ApplyRunProperties(run, paragraphStyleToRunPatchSpec(style))
	return updated
}

func paragraphStyleToPatchSpec(style paragraphStyle) ooxmlpatch.ParagraphPropertiesSpec {
	spec := ooxmlpatch.ParagraphPropertiesSpec{
		Alignment:         style.alignment,
		LineRule:          style.lineRule,
		BeforeTwips:       intPointerValue(style.beforeTwips),
		AfterTwips:        intPointerValue(style.afterTwips),
		FirstLineChars:    intPointerValue(style.firstLineChars),
		BeforeLines:       intPointerValue(style.beforeLines),
		AfterLines:        intPointerValue(style.afterLines),
		FirstLineCharsSet: style.firstLineChars != nil,
		BeforeLinesSet:    style.beforeLines != nil,
		AfterLinesSet:     style.afterLines != nil,
	}
	if line, err := strconv.Atoi(strings.TrimSpace(style.line)); err == nil {
		spec.LineTwips = line
	}
	if spec.LineTwips > 0 && spec.LineRule == "" {
		spec.LineRule = "auto"
	}
	if spec.LineTwips == 0 && spec.BeforeLines == 0 && spec.AfterLines == 0 {
		spec.LineRule = ""
	}
	return spec
}

func paragraphStyleToRunPatchSpec(style paragraphStyle) ooxmlpatch.RunPropertiesSpec {
	size, _ := strconv.Atoi(strings.TrimSpace(style.fontSize))
	asciiFont := style.asciiFont
	if asciiFont == "" {
		asciiFont = style.eastAsiaFont
	}
	return ooxmlpatch.RunPropertiesSpec{
		EastAsiaFont:       style.eastAsiaFont,
		AsciiFont:          asciiFont,
		HAnsiFont:          asciiFont,
		FontSizeHalfPoints: size,
		ComplexSizeHalfPts: size,
		Bold:               style.bold,
	}
}

func intPointerValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func buildRunProperties(style paragraphStyle) string {
	var builder strings.Builder
	eastAsiaFont := style.eastAsiaFont
	asciiFont := style.asciiFont
	if asciiFont == "" {
		asciiFont = eastAsiaFont
	}
	if eastAsiaFont != "" || asciiFont != "" {
		builder.WriteString(fmt.Sprintf(
			`<w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s"/>`,
			asciiFont,
			asciiFont,
			eastAsiaFont,
		))
	}
	if style.fontSize != "" {
		builder.WriteString(fmt.Sprintf(`<w:sz w:val="%s"/><w:szCs w:val="%s"/>`, style.fontSize, style.fontSize))
	}
	if style.bold {
		builder.WriteString(`<w:b/><w:bCs/>`)
	}
	return builder.String()
}

func buildParagraphXML(text string, style paragraphStyle) string {
	return `<w:p><w:pPr>` + buildParagraphProperties(style) + `</w:pPr>` +
		buildRunXML(text, style) +
		`</w:p>`
}

func buildKeywordParagraphXML(label string, body string, chinese bool) string {
	labelStyle := keywordENLabelStyle()
	bodyStyle := keywordENBodyStyle()
	if chinese {
		labelStyle = keywordCNLabelStyle()
		bodyStyle = keywordCNBodyStyle()
	}
	content := strings.TrimSpace(body)
	if !chinese && content != "" {
		content = " " + content
	}
	return buildLabeledParagraphXML(label, content, labelStyle, bodyStyle, keywordParagraphStyle())
}

func buildLabeledParagraphXML(label string, body string, labelStyle paragraphStyle, bodyStyle paragraphStyle, paragraphLayout paragraphStyle) string {
	return `<w:p><w:pPr>` + buildParagraphProperties(paragraphLayout) + `</w:pPr>` +
		buildRunXML(label, labelStyle) +
		buildRunXML(body, bodyStyle) +
		`</w:p>`
}

func buildRunXML(text string, style paragraphStyle) string {
	return `<w:r><w:rPr>` + buildRunProperties(style) + `</w:rPr><w:t>` + html.EscapeString(text) + `</w:t></w:r>`
}

func decodeDoubleEscapedVisibleEntities(content string) (string, int) {
	replacements := []struct {
		old string
		new string
	}{
		{`&amp;lt;`, `&lt;`},
		{`&amp;gt;`, `&gt;`},
		{`&amp;#39;`, `'`},
		{`&amp;apos;`, `'`},
		{`&amp;quot;`, `&quot;`},
	}
	updated := content
	count := 0
	for _, replacement := range replacements {
		count += strings.Count(updated, replacement.old)
		updated = strings.ReplaceAll(updated, replacement.old, replacement.new)
	}
	return updated, count
}

func ensureParagraphStartsWithPageBreak(paragraph string) string {
	if strings.Contains(paragraph, `<w:br w:type="page"/>`) {
		return paragraph
	}
	pageBreakRun := `<w:r><w:br w:type="page"/></w:r>`
	if match := paragraphPropertiesPattern.FindStringIndex(paragraph); match != nil {
		insertAt := match[1]
		return paragraph[:insertAt] + pageBreakRun + paragraph[insertAt:]
	}
	openEnd := strings.Index(paragraph, ">")
	if openEnd < 0 {
		return paragraph
	}
	return paragraph[:openEnd+1] + pageBreakRun + paragraph[openEnd+1:]
}

func isForcedPageStartTitle(text string) bool {
	normalized := normalizeChineseLabelText(text)
	return normalized == "\u53c2\u8003\u6587\u732e" || normalized == "\u81f4\u8c22"
}

func isAcknowledgementsTitle(text string) bool {
	return normalizeChineseLabelText(text) == "\u81f4\u8c22"
}

func normalizeChineseLabelText(text string) string {
	replacer := strings.NewReplacer(" ", "", "\t", "", "\u00a0", "", "銆€", "")
	return replacer.Replace(strings.TrimSpace(text))
}

func isBlankParagraphText(text string) bool {
	return strings.TrimSpace(text) == ""
}

func extractParagraphText(paragraph string) string {
	var builder strings.Builder
	for _, match := range textPattern.FindAllStringSubmatch(paragraph, -1) {
		if len(match) > 1 {
			builder.WriteString(decodeVisibleText(match[1]))
		}
	}
	return builder.String()
}

func decodeVisibleText(text string) string {
	decoded := text
	for i := 0; i < 3; i++ {
		next := html.UnescapeString(decoded)
		if next == decoded {
			break
		}
		decoded = next
	}
	return decoded
}

func intPtr(value int) *int {
	return &value
}

func replaceLiteralChanged(s string, old string, replacement string) (string, int) {
	if old == replacement {
		return s, 0
	}
	count := strings.Count(s, old)
	if count == 0 {
		return s, 0
	}
	return strings.ReplaceAll(s, old, replacement), count
}

func replaceRegexpChanged(s string, pattern *regexp.Regexp, replacement func([]string) string) (string, int) {
	matches := pattern.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s, 0
	}

	var builder strings.Builder
	last := 0
	count := 0
	for _, matchIndexes := range matches {
		fullStart, fullEnd := matchIndexes[0], matchIndexes[1]
		builder.WriteString(s[last:fullStart])

		match := make([]string, len(matchIndexes)/2)
		for i := 0; i < len(matchIndexes); i += 2 {
			start, end := matchIndexes[i], matchIndexes[i+1]
			if start >= 0 && end >= 0 {
				match[i/2] = s[start:end]
			}
		}

		replaced := replacement(match)
		if replaced != match[0] {
			count++
		}
		builder.WriteString(replaced)
		last = fullEnd
	}
	builder.WriteString(s[last:])

	if count == 0 {
		return s, 0
	}
	return builder.String(), count
}
