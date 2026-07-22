package verify

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/cqrwst"
	"github.com/paper-format-checker/backend/internal/core/goldenregression"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/renderverify"
	"github.com/paper-format-checker/backend/internal/core/repaircontract"
	"github.com/paper-format-checker/backend/internal/core/templatecontract"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

const documentTarget = "word/document.xml"
const wordprocessingMLNamespace = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

var placeholderPattern = regexp.MustCompile(`\{\{[^{}]+\}\}`)
var visibleReviewFieldPattern = regexp.MustCompile(`(?is)<w:instrText\b[^>]*>\s*(?:TOC|REF|PAGEREF|NOTEREF)\b`)
var unmaterializedTOCPagePattern = regexp.MustCompile(`(?s)TOC \\o .*?<w:tab/>.*?<w:t>0</w:t>.*?w:fldCharType="end"`)
var verifyParagraphPattern = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`)
var verifyTextPattern = regexp.MustCompile(`(?s)<w:t(?:\s[^>]*)?>(.*?)</w:t>`)
var captionTextPattern = regexp.MustCompile(`(?i)^(图|表|Figure|Table)\s*(\d+(?:[-.．]\d+)*)\s*\S+`)
var captionNumberSeparatorPattern = regexp.MustCompile(`[-.．]`)
var verifySectionPropertiesPattern = regexp.MustCompile(`(?s)<w:sectPr\b[^>]*/>|<w:sectPr\b[^>]*>.*?</w:sectPr>`)
var verifyHeaderReferenceIDPattern = regexp.MustCompile(`<w:headerReference\b[^>]*\br:id="([^"]+)"[^>]*/>`)
var verifyFooterReferenceIDPattern = regexp.MustCompile(`<w:footerReference\b[^>]*\br:id="([^"]+)"[^>]*/>`)
var verifyRelationshipPattern = regexp.MustCompile(`<Relationship\b[^>]*/>`)
var verifyTablePattern = regexp.MustCompile(`(?s)<w:tbl(?:\s[^>]*)?>.*?</w:tbl>`)
var verifyTableRowPattern = regexp.MustCompile(`(?s)<w:tr(?:\s[^>]*)?>.*?</w:tr>`)
var verifyTableCellPattern = regexp.MustCompile(`(?s)<w:tc(?:\s[^>]*)?>.*?</w:tc>`)
var verifyAttributePattern = regexp.MustCompile(`\b([A-Za-z0-9_:.]+)="([^"]*)"`)
var imageExtentPattern = regexp.MustCompile(`<wp:extent\b[^>]*\bcx="(\d+)"[^>]*/>`)
var formulaNumberPattern = regexp.MustCompile(`[\(（]\s*\d+(?:[-.．]\d+)?\s*[\)）]`)
var manualObjectReferencePattern = regexp.MustCompile(`(?:图|表)\s*\d+(?:[-.．]\d+)?|式\s*[\(（]\s*\d+(?:[-.．]\d+)?\s*[\)）]`)
var manualCaptionPattern = regexp.MustCompile(`^(?:图|表)\s*\d+(?:[-.．]\d+)*\s*\S+`)
var continuedTableCaptionPattern = regexp.MustCompile(`^续表\s*\d+(?:[-.．]\d+)*\s*\S+`)

type Issue struct {
	Kind     string
	Severity string
	Message  string
	Target   string
}

type Result struct {
	Passed           bool                     `json:"passed"`
	ComplianceStatus string                   `json:"compliance_status"`
	ComplianceReason string                   `json:"compliance_reason"`
	FatalIssues      []Issue                  `json:"fatal_issues,omitempty"`
	RepairableIssues []Issue                  `json:"repairable_issues,omitempty"`
	Warnings         []Issue                  `json:"warnings,omitempty"`
	RenderResult     *renderverify.Result     `json:"render_result,omitempty"`
	GoldenRegression *goldenregression.Result `json:"golden_regression,omitempty"`
}

type Verifier struct {
	templateProfile *templateprofile.Profile
	closure         *ClosureArtifacts
	renderOptions   *renderverify.Options
	goldenPath      string
	skipCQRWST      bool
}

type ClosureArtifacts struct {
	TemplateRules  templatecontract.RuleSet
	PaperAST       paperast.Snapshot
	RepairContract repaircontract.Contract
}

func NewVerifier() *Verifier {
	return &Verifier{}
}

func NewVerifierWithTemplateProfile(profile *templateprofile.Profile) *Verifier {
	return &Verifier{templateProfile: profile}
}

func NewVerifierWithTemplateProfileAndClosure(profile *templateprofile.Profile, rules templatecontract.RuleSet, ast paperast.Snapshot, contract repaircontract.Contract) *Verifier {
	verifier := &Verifier{
		templateProfile: profile,
		closure: &ClosureArtifacts{
			TemplateRules:  rules,
			PaperAST:       ast,
			RepairContract: contract,
		},
	}
	verifier.configureRenderGateFromEnv()
	return verifier
}

func (v *Verifier) WithRenderGate(options renderverify.Options, goldenPath string) *Verifier {
	if v == nil {
		return nil
	}
	v.renderOptions = &options
	v.goldenPath = strings.TrimSpace(goldenPath)
	return v
}

func (v *Verifier) WithoutRenderGate() *Verifier {
	v.renderOptions = nil
	v.goldenPath = ""
	return v
}

func (v *Verifier) WithoutCQRWSTRules() *Verifier {
	if v == nil {
		return nil
	}
	v.skipCQRWST = true
	return v
}

func (v *Verifier) Verify(ctx context.Context, docxPath string) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		return finalizeResult(Result{
			Passed: false,
			FatalIssues: []Issue{{
				Kind:     "docx_open",
				Severity: "fatal",
				Message:  fmt.Sprintf("open docx %q failed: %v", docxPath, err),
				Target:   docxPath,
			}},
		}), nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	content, ok := pkg.Get(documentTarget)
	if !ok {
		return finalizeResult(Result{
			Passed: false,
			FatalIssues: []Issue{{
				Kind:     "missing_document_xml",
				Severity: "fatal",
				Message:  "required document XML is missing",
				Target:   documentTarget,
			}},
		}), nil
	}

	result := Result{}
	document := string(content)
	if placeholderPattern.MatchString(document) {
		result.RepairableIssues = append(result.RepairableIssues, Issue{
			Kind:     "placeholder",
			Severity: "error",
			Message:  "document still contains template placeholders",
			Target:   documentTarget,
		})
	}

	if v == nil || !v.skipCQRWST {
		cqrwstResult, err := v.checkCQRWST(ctx, docxPath)
		if err != nil {
			result.FatalIssues = append(result.FatalIssues, Issue{
				Kind:     "cqrwst_check",
				Severity: "fatal",
				Message:  fmt.Sprintf("CQRWST rule check failed: %v", err),
				Target:   documentTarget,
			})
		} else {
			for _, issue := range cqrwstResult.Issues {
				result.RepairableIssues = append(result.RepairableIssues, Issue{
					Kind:     "cqrwst_rule",
					Severity: issue.Severity,
					Message:  issue.Message,
					Target:   issue.Target,
				})
			}
		}
	}

	if len(strings.TrimSpace(document)) < 20 {
		result.Warnings = append(result.Warnings, Issue{
			Kind:     "short_document",
			Severity: "warning",
			Message:  "document XML is empty or unexpectedly short",
			Target:   documentTarget,
		})
	}
	checkFinalDeliveryOOXML(pkg, &result)
	addFieldShadingDisplayWarning(pkg, &result)
	addManualCaptionWarning(document, &result)
	addCaptionNumberingIssues(document, &result)
	addSectionHeaderFooterIssues(document, &result)
	addPageNumberingIssues(pkg, document, &result)
	addTableFormattingIssues(document, &result, v.templateProfile)
	addImageFormulaReferenceIssues(document, &result, v.templateProfile)
	addFieldUpdateIssues(pkg, &result)
	v.checkClosureArtifacts(&result)
	v.checkRenderedOutput(ctx, docxPath, &result)

	result.Passed = len(result.FatalIssues) == 0 && len(result.RepairableIssues) == 0
	return finalizeResult(result), nil
}

func addFieldShadingDisplayWarning(pkg *ooxmlpkg.DocxPackage, result *Result) {
	if pkg == nil || result == nil {
		return
	}
	for _, name := range pkg.Names() {
		if name != documentTarget {
			continue
		}
		content, ok := pkg.Get(name)
		if ok && visibleReviewFieldPattern.Match(content) {
			result.Warnings = append(result.Warnings, Issue{
				Kind:     "field_shading_display_only",
				Severity: "info",
				Message:  "Word/WPS may show gray field shading when a table of contents, page number, or cross-reference field is selected; this is a client display option and is not treated as a format error.",
				Target:   name,
			})
			return
		}
	}
}

func addManualCaptionWarning(document string, result *Result) {
	if result == nil {
		return
	}
	foundManualCaption := false
	for _, paragraph := range verifyParagraphPattern.FindAllString(document, -1) {
		text := strings.TrimSpace(verifyXMLText(paragraph))
		if text == "" || !manualCaptionPattern.MatchString(text) {
			continue
		}
		if strings.Contains(paragraph, "<w:instrText") && strings.Contains(paragraph, "SEQ") {
			continue
		}
		foundManualCaption = true
		break
	}
	if !foundManualCaption {
		return
	}
	result.Warnings = append(result.Warnings, Issue{
		Kind:     "manual_caption_not_dynamic",
		Severity: "warning",
		Message:  "figure/table captions appear to be manually typed instead of Word SEQ caption fields; numbering may not update automatically after edits.",
		Target:   documentTarget,
	})
}

func verifyXMLText(fragment string) string {
	var builder strings.Builder
	for _, match := range verifyTextPattern.FindAllStringSubmatch(fragment, -1) {
		if len(match) == 2 {
			builder.WriteString(html.UnescapeString(match[1]))
		}
	}
	return builder.String()
}

type captionNumber struct {
	Label      string
	Number     string
	Format     string
	Chapter    string
	Ordinal    int
	HasChapter bool
}

func addCaptionNumberingIssues(document string, result *Result) {
	if result == nil {
		return
	}
	captions := captionNumbersFromDocument(document)
	if len(captions) == 0 {
		return
	}

	formatByLabel := map[string]map[string]bool{}
	chapteredByLabel := map[string]bool{}
	simpleByLabel := map[string]bool{}
	lastByGroup := map[string]int{}
	sequenceIssue := false

	for _, caption := range captions {
		if formatByLabel[caption.Label] == nil {
			formatByLabel[caption.Label] = map[string]bool{}
		}
		formatByLabel[caption.Label][caption.Format] = true
		if caption.HasChapter {
			chapteredByLabel[caption.Label] = true
		} else {
			simpleByLabel[caption.Label] = true
		}

		group := caption.Label + "|" + caption.Format
		if caption.HasChapter {
			group += "|" + caption.Chapter
		}
		previous := lastByGroup[group]
		if caption.Ordinal <= 0 || caption.Ordinal != previous+1 {
			sequenceIssue = true
		}
		if caption.Ordinal > previous {
			lastByGroup[group] = caption.Ordinal
		}
	}

	if sequenceIssue {
		appendRepairableIssueOnce(result, "caption_number_sequence", "figure/table caption numbers are not continuous; delete stale manual captions or regenerate Word captions so numbers update in order.", documentTarget)
	}

	missingChapter := false
	mixedFormat := false
	for label, formats := range formatByLabel {
		if len(formats) > 1 {
			mixedFormat = true
		}
		if chapteredByLabel[label] && simpleByLabel[label] {
			missingChapter = true
			mixedFormat = true
		}
	}
	if missingChapter {
		appendRepairableIssueOnce(result, "caption_missing_chapter_number", "some figure/table captions omit chapter numbers while other captions include them; confirm the school rule and regenerate captions with chapter numbers when required.", documentTarget)
	}
	if mixedFormat {
		appendRepairableIssueOnce(result, "caption_numbering_mixed_format", "figure/table captions use mixed numbering formats such as simple numbers and chapter-based numbers; keep one numbering policy across the document.", documentTarget)
	}
}

func captionNumbersFromDocument(document string) []captionNumber {
	var captions []captionNumber
	for _, paragraph := range verifyParagraphPattern.FindAllString(document, -1) {
		text := strings.TrimSpace(verifyXMLText(paragraph))
		match := captionTextPattern.FindStringSubmatch(text)
		if len(match) != 3 {
			continue
		}
		if parsed, ok := parseCaptionNumber(match[1], match[2]); ok {
			captions = append(captions, parsed)
		}
	}
	return captions
}

func parseCaptionNumber(label string, number string) (captionNumber, bool) {
	number = strings.TrimSpace(number)
	parts := captionNumberSeparatorPattern.Split(number, -1)
	if len(parts) == 0 {
		return captionNumber{}, false
	}

	format := "simple"
	chapter := ""
	ordinalText := parts[len(parts)-1]
	hasChapter := len(parts) > 1
	if hasChapter {
		chapter = parts[0]
		if strings.Contains(number, "-") {
			format = "chapter-hyphen"
		} else {
			format = "chapter-dot"
		}
	}

	ordinal, err := strconv.Atoi(ordinalText)
	if err != nil {
		return captionNumber{}, false
	}
	return captionNumber{
		Label:      label,
		Number:     number,
		Format:     format,
		Chapter:    chapter,
		Ordinal:    ordinal,
		HasChapter: hasChapter,
	}, true
}

func addSectionHeaderFooterIssues(document string, result *Result) {
	if result == nil {
		return
	}
	sections := verifySectionPropertiesPattern.FindAllString(document, -1)
	if len(sections) < 2 {
		return
	}

	seenHeaders := map[string]int{}
	seenFooters := map[string]int{}
	previousHadHeader := false
	previousHadFooter := false
	inherited := false
	linked := false

	for index, section := range sections {
		headerIDs := verifyHeaderReferenceIDPattern.FindAllStringSubmatch(section, -1)
		footerIDs := verifyFooterReferenceIDPattern.FindAllStringSubmatch(section, -1)
		if index > 0 {
			if previousHadHeader && len(headerIDs) == 0 {
				inherited = true
			}
			if previousHadFooter && len(footerIDs) == 0 {
				inherited = true
			}
		}
		for _, match := range headerIDs {
			if len(match) != 2 {
				continue
			}
			seenHeaders[match[1]]++
		}
		for _, match := range footerIDs {
			if len(match) != 2 {
				continue
			}
			if seenFooters[match[1]] > 0 {
				linked = true
			}
			seenFooters[match[1]]++
		}
		previousHadHeader = previousHadHeader || len(headerIDs) > 0
		previousHadFooter = previousHadFooter || len(footerIDs) > 0
	}

	if inherited {
		appendRepairableIssueOnce(result, "section_header_footer_inherited", "one or more sections do not define their own header/footer references and may inherit content from the previous section; break header/footer links before setting chapter-specific text or page numbers.", documentTarget)
	}
	if linked {
		appendRepairableIssueOnce(result, "linked_header_footer_sections", "multiple sections reference the same header/footer part; chapter-specific headers, footer formats, and page numbering may change together instead of independently.", documentTarget)
	}
}

func appendRepairableIssueOnce(result *Result, kind string, message string, target string) {
	if result == nil || hasIssueKind(result.RepairableIssues, kind) {
		return
	}
	result.RepairableIssues = append(result.RepairableIssues, Issue{
		Kind:     kind,
		Severity: "error",
		Message:  message,
		Target:   target,
	})
}

func hasIssueKind(issues []Issue, kind string) bool {
	for _, issue := range issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}

func addPageNumberingIssues(pkg *ooxmlpkg.DocxPackage, document string, result *Result) {
	if pkg == nil || result == nil {
		return
	}
	sections := verifySectionPropertiesPattern.FindAllString(document, -1)
	if len(sections) < 3 {
		return
	}

	frontIndex, bodyIndex := pageNumberSectionIndexes(sections)
	for index := 0; index < frontIndex; index++ {
		if strings.Contains(sections[index], "<w:footerReference") || strings.Contains(sections[index], "<w:pgNumType") {
			appendRepairableIssueOnce(result, "cover_page_number_present", "cover section contains page-number/footer settings; cover pages should not display page numbers.", documentTarget)
			break
		}
	}
	front := sections[frontIndex]
	body := sections[bodyIndex]
	if !sectionHasPageFormat(front, map[string]bool{"upperRoman": true, "lowerRoman": true}, 1) {
		appendRepairableIssueOnce(result, "front_page_number_not_roman", "abstract/catalog front matter should use Roman page numbers starting from 1.", documentTarget)
	}
	if !sectionHasPageFormat(body, map[string]bool{"decimal": true, "": true}, 1) {
		appendRepairableIssueOnce(result, "body_page_number_not_decimal_start", "body section should use Arabic page numbers and restart from 1.", documentTarget)
	}

	footerTargets := footerTargetsByRelationshipID(pkg)
	for index, section := range sections {
		if index == 0 {
			continue
		}
		for _, match := range verifyFooterReferenceIDPattern.FindAllStringSubmatch(section, -1) {
			if len(match) != 2 {
				continue
			}
			target := footerTargets[match[1]]
			if target == "" {
				continue
			}
			contentBytes, ok := pkg.Get(target)
			if !ok {
				continue
			}
			footerXML := string(contentBytes)
			if !footerHasDynamicPageField(footerXML) && footerHasVisibleManualPageNumber(footerXML) {
				appendRepairableIssueOnce(result, "manual_page_number_not_dynamic", "footer page number appears to be manually typed; use a Word PAGE field so page numbers update automatically.", target)
			}
			if !footerPageNumberCentered(footerXML) {
				appendRepairableIssueOnce(result, "footer_page_number_not_centered", "page-number footer is not centered; use the centered footer paragraph style for front matter and body pages.", target)
			}
		}
	}
}

func pageNumberSectionIndexes(sections []string) (int, int) {
	frontIndex, bodyIndex := -1, -1
	for index, section := range sections {
		format := attributeValue(regexp.MustCompile(`<w:pgNumType\b[^>]*/>`).FindString(section), "w:fmt")
		if frontIndex < 0 && (format == "upperRoman" || format == "lowerRoman") {
			frontIndex = index
			continue
		}
		if frontIndex >= 0 && bodyIndex < 0 && (format == "decimal" || format == "") && strings.Contains(section, "<w:pgNumType") {
			bodyIndex = index
		}
	}
	if frontIndex < 0 {
		frontIndex = minInt(1, len(sections)-1)
	}
	if bodyIndex < 0 || bodyIndex <= frontIndex {
		bodyIndex = len(sections) - 1
	}
	return frontIndex, bodyIndex
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func sectionHasPageFormat(section string, allowedFormats map[string]bool, start int) bool {
	pgNumType := regexp.MustCompile(`<w:pgNumType\b[^>]*/>`).FindString(section)
	if pgNumType == "" {
		return false
	}
	format := attributeValue(pgNumType, "w:fmt")
	if !allowedFormats[format] {
		return false
	}
	if start > 0 {
		startValue := attributeValue(pgNumType, "w:start")
		if startValue != strconv.Itoa(start) {
			return false
		}
	}
	return true
}

func footerTargetsByRelationshipID(pkg *ooxmlpkg.DocxPackage) map[string]string {
	targets := map[string]string{}
	if pkg == nil {
		return targets
	}
	relsBytes, ok := pkg.Get("word/_rels/document.xml.rels")
	if !ok {
		return targets
	}
	for _, rel := range verifyRelationshipPattern.FindAllString(string(relsBytes), -1) {
		if !strings.Contains(rel, "/relationships/footer") {
			continue
		}
		id := attributeValue(rel, "Id")
		target := attributeValue(rel, "Target")
		if id == "" || target == "" {
			continue
		}
		if !strings.HasPrefix(target, "word/") {
			target = "word/" + strings.TrimPrefix(target, "/")
		}
		targets[id] = target
	}
	return targets
}

func footerHasDynamicPageField(footerXML string) bool {
	return strings.Contains(footerXML, "<w:instrText") && strings.Contains(footerXML, "PAGE")
}

func footerHasVisibleManualPageNumber(footerXML string) bool {
	text := strings.TrimSpace(verifyXMLText(footerXML))
	if text == "" {
		return false
	}
	return regexp.MustCompile(`^\D*\d+\D*$`).MatchString(text)
}

func footerPageNumberCentered(footerXML string) bool {
	return strings.Contains(footerXML, `<w:jc w:val="center"`)
}

func addTableFormattingIssues(document string, result *Result, profiles ...*templateprofile.Profile) {
	if result == nil {
		return
	}
	var profile *templateprofile.Profile
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	tableStyle := templateTableStyle(profile)
	for _, table := range verifyTablePattern.FindAllString(document, -1) {
		if isCoverLayoutTable(table) {
			continue
		}
		if profile != nil && strings.EqualFold(strings.TrimSpace(profile.RulePack.TableStyle), "three-line") && !tableHasThreeLineBorders(table) {
			appendRepairableIssueOnce(result, "table_three_line_format", "table borders are not in three-line format; keep top/header/bottom rules and remove vertical/extra inner rules.", documentTarget)
		}
		if !tableLayoutIsStable(table) {
			appendRepairableIssueOnce(result, "table_layout_not_centered", "table should be centered, non-floating, and constrained to the page text area.", documentTarget)
		}
		if !tableHasRepeatingHeader(table) {
			appendRepairableIssueOnce(result, "table_repeating_header_missing", "long tables should mark the first row as a repeating header row.", documentTarget)
		}
		if tableStyle != nil && !tableCellsFollowStyle(table, tableStyle) {
			appendRepairableIssueOnce(result, "table_cell_style_mismatch", "table cell text does not match the table style extracted from the template.", documentTarget)
		}
	}
}

func templateTableStyle(profile *templateprofile.Profile) *templateprofile.StyleRule {
	if profile == nil {
		return nil
	}
	for _, key := range []string{"table_body", "table_content", "table"} {
		if style, ok := profile.Styles[key]; ok {
			return &style
		}
	}
	return nil
}

func isCoverLayoutTable(table string) bool {
	text := verifyXMLText(table)
	matches := 0
	for _, marker := range []string{"题目", "学院", "专业", "班级", "学号", "姓名", "指导教师"} {
		if strings.Contains(text, marker) || strings.Contains(table, ">"+marker+"<") {
			matches++
		}
	}
	return matches >= 2 || strings.Contains(text, "题目") || strings.Contains(table, ">题目<")
}

func tableHasThreeLineBorders(table string) bool {
	if !strings.Contains(table, "<w:tblBorders") {
		return false
	}
	required := []string{"<w:top ", "<w:bottom ", "<w:insideH "}
	for _, token := range required {
		if !strings.Contains(table, token) {
			return false
		}
	}
	for _, token := range []string{"<w:left ", "<w:right ", "<w:insideV "} {
		for _, border := range regexp.MustCompile(token+`[^>]*/>`).FindAllString(table, -1) {
			value := attributeValue(border, "w:val")
			if value != "nil" && value != "none" {
				return false
			}
		}
	}
	return true
}

func tableLayoutIsStable(table string) bool {
	if strings.Contains(table, "<w:tblpPr") || strings.Contains(table, "<w:tblOverlap") {
		return false
	}
	if !strings.Contains(table, `<w:jc w:val="center"`) {
		return false
	}
	if strings.Contains(table, `w:type="auto"`) || strings.Contains(table, `w:w="0"`) {
		return false
	}
	return true
}

func tableHasRepeatingHeader(table string) bool {
	firstRow := verifyTableRowPattern.FindString(table)
	return firstRow != "" && strings.Contains(firstRow, "<w:tblHeader")
}

func tableCellsFollowStyle(table string, styles ...*templateprofile.StyleRule) bool {
	if len(styles) == 0 || styles[0] == nil {
		return true
	}
	style := styles[0]
	cells := verifyTableCellPattern.FindAllString(table, -1)
	if len(cells) == 0 {
		return true
	}
	for _, cell := range cells {
		if style.Alignment != "" && !strings.Contains(cell, `<w:jc w:val="`+style.Alignment+`"`) {
			return false
		}
		if style.Line != "" && (!strings.Contains(cell, `<w:spacing`) || !strings.Contains(cell, `w:line="`+style.Line+`"`)) {
			return false
		}
		if style.FontASCII != "" && !strings.Contains(cell, `w:ascii="`+style.FontASCII+`"`) && !strings.Contains(cell, `w:hAnsi="`+style.FontASCII+`"`) {
			return false
		}
		if style.FontEastAsia != "" && !strings.Contains(cell, `w:eastAsia="`+style.FontEastAsia+`"`) {
			return false
		}
		if style.FontSizeHalfPt != "" && !strings.Contains(cell, `<w:sz w:val="`+style.FontSizeHalfPt+`"`) {
			return false
		}
	}
	return true
}

func attributeValue(element string, name string) string {
	for _, match := range verifyAttributePattern.FindAllStringSubmatch(element, -1) {
		if len(match) == 3 && match[1] == name {
			return match[2]
		}
	}
	return ""
}

func addImageFormulaReferenceIssues(document string, result *Result, profiles ...*templateprofile.Profile) {
	if result == nil {
		return
	}
	paragraphs := verifyParagraphPattern.FindAllString(document, -1)
	bodyStarted := false
	inTOC := false
	for index, paragraph := range paragraphs {
		if strings.Contains(paragraph, `TOC \o "1-3"`) || strings.Contains(paragraph, " TOC ") {
			inTOC = true
			continue
		}
		if inTOC {
			if strings.Contains(paragraph, `w:fldCharType="end"`) {
				inTOC = false
			}
			continue
		}
		text := strings.TrimSpace(verifyXMLText(paragraph))
		if strings.HasPrefix(strings.Join(strings.Fields(text), ""), "1绪论") || strings.HasPrefix(strings.ToLower(strings.Join(strings.Fields(text), " ")), "1 introduction") {
			bodyStarted = true
		}
		if strings.Contains(paragraph, "<w:drawing") || strings.Contains(paragraph, "<w:pict") {
			if !bodyStarted {
				continue
			}
			if strings.Contains(paragraph, "<wp:anchor") {
				appendRepairableIssueOnce(result, "floating_image_anchor", "image uses floating/anchored layout; use inline layout so it stays fixed in the paragraph.", documentTarget)
			}
			if imageWidthOverTextArea(paragraph, profileTextAreaWidthEMU(profiles...)) {
				appendRepairableIssueOnce(result, "image_width_over_text_area", "image width appears larger than the page text area; scale it proportionally within the text area.", documentTarget)
			}
			nextCaption := nextNonBlankParagraphText(paragraphs, index+1)
			if strings.Contains(paragraph, "<wpg:wgp") {
				continue
			}
			if nextCaption == "" || !strings.HasPrefix(strings.TrimSpace(nextCaption), "图") || !strings.Contains(paragraph, "<w:keepNext") {
				appendRepairableIssueOnce(result, "image_keep_with_caption_missing", "image paragraph should be kept with the following figure caption so they do not split across pages.", documentTarget)
			}
			if nextCaption != "" && strings.HasPrefix(strings.TrimSpace(nextCaption), "图") {
				if parsed, ok := parseCaptionNumber("图", strings.TrimPrefix(strings.Fields(nextCaption)[0], "图")); !ok || !parsed.HasChapter {
					appendRepairableIssueOnce(result, "figure_caption_missing_chapter_number", "figure caption does not include a chapter number; regenerate the caption with chapter numbering when required.", documentTarget)
				}
			}
		}
		if isCaptionText(text) {
			continue
		}
		if manualObjectReferencePattern.MatchString(text) && !paragraphHasReferenceField(paragraph) {
			appendWarningIssueOnce(result, "manual_cross_reference", "body text appears to contain manually typed figure/table/formula references; Word cross-reference fields would update more safely after edits.", documentTarget)
		}
	}

	for _, table := range verifyTablePattern.FindAllString(document, -1) {
		hasEquationObject := strings.Contains(table, "<m:oMath") || strings.Contains(table, "<m:oMathPara")
		if !hasEquationObject && looksLikeNumberedFormulaTable(table) {
			appendRepairableIssueOnce(result, "formula_not_equation_editor", "numbered formula is plain text rather than an Office Math equation object.", documentTarget)
		}
		if !hasEquationObject {
			continue
		}
		if formulaNumberPattern.MatchString(verifyXMLText(table)) && !strings.Contains(table, "SEQ") {
			appendRepairableIssueOnce(result, "manual_formula_number_not_dynamic", "formula number appears to be manually typed; use a formula SEQ caption field so numbering updates automatically.", documentTarget)
		}
		if !formulaTableLayoutLooksAligned(table) {
			appendRepairableIssueOnce(result, "formula_layout_mismatch", "formula layout should center the formula and right-align the formula number, usually with a borderless equation table.", documentTarget)
		}
	}
}

func looksLikeNumberedFormulaTable(table string) bool {
	cells := verifyTableCellPattern.FindAllString(table, -1)
	if len(cells) < 2 {
		return false
	}
	formula := strings.TrimSpace(verifyXMLText(cells[0]))
	number := strings.TrimSpace(verifyXMLText(cells[len(cells)-1]))
	return formula != "" && strings.ContainsAny(formula, "=+-×÷∑√∫∬∭∮∏∆∂∞≈≠≤≥[]{}|∇") && formulaNumberPattern.MatchString(number)
}

func appendWarningIssueOnce(result *Result, kind string, message string, target string) {
	if result == nil || hasIssueKind(result.Warnings, kind) {
		return
	}
	result.Warnings = append(result.Warnings, Issue{
		Kind:     kind,
		Severity: "warning",
		Message:  message,
		Target:   target,
	})
}

func imageWidthOverTextArea(paragraph string, widths ...int) bool {
	maxWidth := 5800000
	if len(widths) > 0 && widths[0] > 0 {
		maxWidth = widths[0]
	}
	for _, match := range imageExtentPattern.FindAllStringSubmatch(paragraph, -1) {
		if len(match) != 2 {
			continue
		}
		width, err := strconv.Atoi(match[1])
		if err == nil && width > maxWidth {
			return true
		}
	}
	return false
}

func profileTextAreaWidthEMU(profiles ...*templateprofile.Profile) int {
	if len(profiles) == 0 || profiles[0] == nil {
		return 0
	}
	page := profiles[0].PageSetup
	width, _ := strconv.Atoi(page.PageWidthTwips)
	left, _ := strconv.Atoi(page.MarginLeftTwips)
	right, _ := strconv.Atoi(page.MarginRightTwips)
	if width <= left+right {
		return 0
	}
	return (width - left - right) * 635
}

func nextNonBlankParagraphText(paragraphs []string, start int) string {
	for i := start; i < len(paragraphs); i++ {
		text := strings.TrimSpace(verifyXMLText(paragraphs[i]))
		if text != "" {
			return text
		}
	}
	return ""
}

func isCaptionText(text string) bool {
	text = strings.TrimSpace(text)
	return captionTextPattern.MatchString(text) || continuedTableCaptionPattern.MatchString(text)
}

func paragraphHasReferenceField(paragraph string) bool {
	if !strings.Contains(paragraph, "<w:instrText") {
		return false
	}
	return strings.Contains(paragraph, " REF ") || strings.Contains(paragraph, " PAGEREF ")
}

func formulaTableLayoutLooksAligned(table string) bool {
	if !strings.Contains(table, `<w:jc w:val="center"`) {
		return false
	}
	if !strings.Contains(table, `<w:jc w:val="right"`) {
		return false
	}
	if strings.Contains(table, "<w:tblBorders") {
		for _, border := range regexp.MustCompile(`<w:(?:top|left|bottom|right|insideH|insideV)\b[^>]*/>`).FindAllString(table, -1) {
			value := attributeValue(border, "w:val")
			if value != "nil" && value != "none" {
				return false
			}
		}
	}
	return true
}

func addFieldUpdateIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	if pkg == nil || result == nil || !packageHasRefreshableField(pkg) {
		return
	}
	settings, _ := pkg.Get("word/settings.xml")
	if !strings.Contains(string(settings), "<w:updateFields") {
		appendRepairableIssueOnce(result, "fields_not_marked_for_update", "document contains refreshable fields such as TOC, SEQ, REF, or PAGEREF but settings.xml does not request field updates on open.", "word/settings.xml")
	}
	document, _ := pkg.Get(documentTarget)
	if unmaterializedTOCPagePattern.Match(document) {
		appendRepairableIssueOnce(result, "toc_page_numbers_not_materialized", "table of contents still contains placeholder page numbers; render and write the real page numbers into the TOC cache.", documentTarget)
	}
}

func packageHasRefreshableField(pkg *ooxmlpkg.DocxPackage) bool {
	if pkg == nil {
		return false
	}
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		content, ok := pkg.Get(name)
		if ok && regexp.MustCompile(`(?is)<w:instrText\b[^>]*>\s*(?:TOC|REF|PAGEREF|NOTEREF|SEQ)\b`).Match(content) {
			return true
		}
	}
	return false
}

func checkFinalDeliveryOOXML(pkg *ooxmlpkg.DocxPackage, result *Result) {
	if pkg == nil || result == nil {
		return
	}
	for _, name := range pkg.Names() {
		contentBytes, ok := pkg.Get(name)
		if !ok {
			continue
		}
		content := string(contentBytes)
		if strings.HasPrefix(name, "word/") && strings.HasSuffix(name, ".xml") && strings.Contains(content, `w:val="start"`) {
			result.FatalIssues = append(result.FatalIssues, Issue{
				Kind:     "renderer_incompatible_ooxml",
				Severity: "fatal",
				Message:  "Word XML contains w:val=\"start\", which is incompatible with the renderer used for final verification",
				Target:   name,
			})
		}
		addWordXMLStructureIssues(name, content, result)
	}
	if _, ok := pkg.Get("word/comments.xml"); ok {
		result.RepairableIssues = append(result.RepairableIssues, Issue{
			Kind:     "comments_not_finalized",
			Severity: "error",
			Message:  "final delivery still contains Word comments",
			Target:   "word/comments.xml",
		})
	}
	addNotePackageIssues(pkg, result)
	addStyleNumberingPackageIssues(pkg, result)
	addEvenHeaderFooterSwitchIssues(pkg, result)
	addHeaderFooterRelationshipIssues(pkg, result)
	addHeaderFooterContentTypeIssues(pkg, result)
	addMediaContentTypeIssues(pkg, result)
	addStyleReferenceIssues(pkg, result)
	addNumberingReferenceIssues(pkg, result)
	addBookmarkPairingIssues(pkg, result)
	addBookmarkReferenceIssues(pkg, result)
	addRelationshipTargetIssues(pkg, result)
}

func addStyleNumberingPackageIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	contentTypesBytes, _ := pkg.Get("[Content_Types].xml")
	relsBytes, _ := pkg.Get("word/_rels/document.xml.rels")
	contentTypes := string(contentTypesBytes)
	rels := string(relsBytes)
	for _, part := range []struct {
		name        string
		kind        string
		contentType string
		relType     string
	}{
		{"word/styles.xml", "styles", "wordprocessingml.styles+xml", "/relationships/styles"},
		{"word/numbering.xml", "numbering", "wordprocessingml.numbering+xml", "/relationships/numbering"},
		{"word/fontTable.xml", "font_table", "wordprocessingml.fontTable+xml", "/relationships/fontTable"},
		{"word/theme/theme1.xml", "theme", "theme+xml", "/relationships/theme"},
		{"word/settings.xml", "settings", "wordprocessingml.settings+xml", "/relationships/settings"},
		{"word/webSettings.xml", "web_settings", "wordprocessingml.webSettings+xml", "/relationships/webSettings"},
	} {
		if _, ok := pkg.Get(part.name); !ok {
			continue
		}
		if !strings.Contains(contentTypes, `PartName="/`+part.name+`"`) || !strings.Contains(contentTypes, part.contentType) {
			appendRepairableIssueOnce(result, part.kind+"_content_type_missing", part.kind+" part exists but [Content_Types].xml does not declare its OOXML content type.", "[Content_Types].xml")
		}
		if !strings.Contains(rels, part.relType) || !strings.Contains(rels, strings.TrimPrefix(part.name, "word/")) {
			appendRepairableIssueOnce(result, part.kind+"_relationship_missing", part.kind+" part exists but word/_rels/document.xml.rels does not link it from the main document.", "word/_rels/document.xml.rels")
		}
	}
}

func addEvenHeaderFooterSwitchIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	documentBytes, _ := pkg.Get("word/document.xml")
	documentXML := string(documentBytes)
	if !strings.Contains(documentXML, `w:type="even"`) || !(strings.Contains(documentXML, "<w:headerReference") || strings.Contains(documentXML, "<w:footerReference")) {
		return
	}
	settingsBytes, _ := pkg.Get("word/settings.xml")
	if !strings.Contains(string(settingsBytes), "<w:evenAndOddHeaders") {
		appendRepairableIssueOnce(result, "even_headers_not_enabled", "document uses even-page header/footer references but settings.xml does not enable even/odd headers.", "word/settings.xml")
	}
}

func addHeaderFooterRelationshipIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	documentBytes, _ := pkg.Get("word/document.xml")
	documentXML := string(documentBytes)
	if !strings.Contains(documentXML, "Reference") {
		return
	}
	rels := documentRelationshipsByID(pkg)
	for _, ref := range regexp.MustCompile(`<w:(headerReference|footerReference)\b[^>]*/>`).FindAllStringSubmatch(documentXML, -1) {
		if len(ref) < 2 {
			continue
		}
		rid := attributeValue(ref[0], "r:id")
		if rid == "" {
			appendRepairableIssueOnce(result, "header_footer_reference_relationship_missing", "header/footer reference is missing an r:id relationship.", documentTarget)
			continue
		}
		attrs, ok := rels[rid]
		if !ok {
			appendRepairableIssueOnce(result, "header_footer_reference_relationship_missing", "header/footer reference points to a missing relationship: "+rid, "word/_rels/document.xml.rels")
			continue
		}
		want := "/relationships/" + strings.TrimSuffix(ref[1], "Reference")
		if !strings.Contains(attrs["Type"], want) {
			appendRepairableIssueOnce(result, "header_footer_reference_type_mismatch", "header/footer reference relationship has the wrong OOXML type: "+rid, "word/_rels/document.xml.rels")
		}
	}
}

func documentRelationshipsByID(pkg *ooxmlpkg.DocxPackage) map[string]map[string]string {
	rels := map[string]map[string]string{}
	if pkg == nil {
		return rels
	}
	contentBytes, ok := pkg.Get("word/_rels/document.xml.rels")
	if !ok {
		return rels
	}
	decoder := xml.NewDecoder(strings.NewReader(string(contentBytes)))
	for {
		token, err := decoder.Token()
		if err != nil {
			return rels
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		attrs := xmlAttrs(start)
		if id := attrs["Id"]; id != "" {
			rels[id] = attrs
		}
	}
}

func addHeaderFooterContentTypeIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	contentTypesBytes, _ := pkg.Get("[Content_Types].xml")
	contentTypes := string(contentTypesBytes)
	for _, attrs := range documentRelationshipsByID(pkg) {
		relType := attrs["Type"]
		partKind := ""
		contentType := ""
		switch {
		case strings.Contains(relType, "/relationships/header"):
			partKind = "header"
			contentType = "wordprocessingml.header+xml"
		case strings.Contains(relType, "/relationships/footer"):
			partKind = "footer"
			contentType = "wordprocessingml.footer+xml"
		default:
			continue
		}
		target := strings.TrimSpace(attrs["Target"])
		if target == "" {
			continue
		}
		part := resolveRelationshipTarget("word/_rels/document.xml.rels", target)
		if part == "" {
			continue
		}
		if !strings.Contains(contentTypes, `PartName="/`+part+`"`) || !strings.Contains(contentTypes, contentType) {
			appendRepairableIssueOnce(result, "header_footer_content_type_missing", partKind+" part exists but [Content_Types].xml does not declare its OOXML content type.", "[Content_Types].xml")
		}
	}
}

func addMediaContentTypeIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	contentTypesBytes, _ := pkg.Get("[Content_Types].xml")
	contentTypes := string(contentTypesBytes)
	defaults := map[string]bool{}
	for _, match := range regexp.MustCompile(`<Default\b[^>]*\bExtension="([^"]+)"`).FindAllStringSubmatch(contentTypes, -1) {
		if len(match) == 2 {
			defaults[strings.ToLower(match[1])] = true
		}
	}
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/media/") {
			continue
		}
		ext := ""
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			ext = strings.ToLower(name[dot+1:])
		}
		if defaults[ext] || strings.Contains(contentTypes, `PartName="/`+name+`"`) {
			continue
		}
		appendRepairableIssueOnce(result, "media_content_type_missing", "media part exists but [Content_Types].xml does not declare its extension or part: "+name, "[Content_Types].xml")
	}
}

func addStyleReferenceIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	stylesBytes, _ := pkg.Get("word/styles.xml")
	defined := map[string]bool{}
	for _, match := range regexp.MustCompile(`<w:style\b[^>]*\bw:styleId="([^"]+)"`).FindAllStringSubmatch(string(stylesBytes), -1) {
		if len(match) == 2 {
			defined[match[1]] = true
		}
	}
	if len(defined) == 0 {
		return
	}
	refPattern := regexp.MustCompile(`<w:(?:pStyle|rStyle|tblStyle)\b[^>]*\bw:val="([^"]+)"`)
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") || name == "word/styles.xml" {
			continue
		}
		content, ok := pkg.Get(name)
		if !ok {
			continue
		}
		for _, match := range refPattern.FindAllStringSubmatch(string(content), -1) {
			if len(match) == 2 && !defined[match[1]] {
				appendRepairableIssueOnce(result, "style_reference_missing", "Word XML references a style that is not defined in styles.xml: "+match[1], name)
			}
		}
	}
}

func addNumberingReferenceIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	numberingBytes, hasNumbering := pkg.Get("word/numbering.xml")
	abstractIDs := map[string]bool{}
	numToAbstract := map[string]string{}
	if hasNumbering {
		numberingXML := string(numberingBytes)
		for _, match := range regexp.MustCompile(`<w:abstractNum\b[^>]*\bw:abstractNumId="([^"]+)"`).FindAllStringSubmatch(numberingXML, -1) {
			if len(match) == 2 {
				abstractIDs[match[1]] = true
			}
		}
		for _, match := range regexp.MustCompile(`(?s)<w:num\b[^>]*\bw:numId="([^"]+)"[^>]*>.*?<w:abstractNumId\b[^>]*\bw:val="([^"]+)"`).FindAllStringSubmatch(numberingXML, -1) {
			if len(match) == 3 {
				numToAbstract[match[1]] = match[2]
				if !abstractIDs[match[2]] {
					appendRepairableIssueOnce(result, "numbering_abstract_missing", "numbering.xml maps a numId to a missing abstractNumId: "+match[2], "word/numbering.xml")
				}
			}
		}
	}
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") || name == "word/numbering.xml" {
			continue
		}
		content, ok := pkg.Get(name)
		if !ok || !strings.Contains(string(content), "<w:numId") {
			continue
		}
		for _, match := range regexp.MustCompile(`<w:numId\b[^>]*\bw:val="([^"]+)"`).FindAllStringSubmatch(string(content), -1) {
			if len(match) == 2 && (!hasNumbering || numToAbstract[match[1]] == "") {
				appendRepairableIssueOnce(result, "numbering_reference_missing", "Word XML references a numId that is not defined in numbering.xml: "+match[1], name)
			}
		}
	}
}

func addBookmarkReferenceIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	bookmarks := map[string]bool{}
	instructionRefs := map[string]string{}
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		content, ok := pkg.Get(name)
		if !ok {
			continue
		}
		text := string(content)
		for _, match := range regexp.MustCompile(`<w:bookmarkStart\b[^>]*\bw:name="([^"]+)"`).FindAllStringSubmatch(text, -1) {
			if len(match) == 2 {
				bookmarks[match[1]] = true
			}
		}
		for _, match := range regexp.MustCompile(`(?is)<w:instrText\b[^>]*>\s*(?:REF|PAGEREF|NOTEREF)\s+([^\\\s<]+)`).FindAllStringSubmatch(text, -1) {
			if len(match) == 2 {
				instructionRefs[match[1]] = name
			}
		}
	}
	for ref, target := range instructionRefs {
		if strings.HasPrefix(ref, "_Toc") {
			continue
		}
		if !bookmarks[ref] {
			appendRepairableIssueOnce(result, "bookmark_reference_missing", "Word field references a missing bookmark: "+ref, target)
		}
	}
}

func addBookmarkPairingIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		content, ok := pkg.Get(name)
		if !ok || !strings.Contains(string(content), "bookmark") {
			continue
		}
		starts := map[string]bool{}
		ends := map[string]bool{}
		for _, match := range regexp.MustCompile(`<w:bookmarkStart\b[^>]*\bw:id="([^"]+)"`).FindAllStringSubmatch(string(content), -1) {
			if len(match) == 2 {
				starts[match[1]] = true
			}
		}
		for _, match := range regexp.MustCompile(`<w:bookmarkEnd\b[^>]*\bw:id="([^"]+)"`).FindAllStringSubmatch(string(content), -1) {
			if len(match) == 2 {
				ends[match[1]] = true
			}
		}
		for id := range starts {
			if !ends[id] {
				appendRepairableIssueOnce(result, "bookmark_pair_missing", "Word bookmarkStart is missing a matching bookmarkEnd: "+id, name)
			}
		}
		for id := range ends {
			if !starts[id] {
				appendRepairableIssueOnce(result, "bookmark_pair_missing", "Word bookmarkEnd is missing a matching bookmarkStart: "+id, name)
			}
		}
	}
}

func addRelationshipTargetIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	entries := map[string]bool{}
	for _, name := range pkg.Names() {
		entries[name] = true
	}
	for _, relsName := range pkg.Names() {
		if !strings.HasSuffix(relsName, ".rels") {
			continue
		}
		contentBytes, ok := pkg.Get(relsName)
		if !ok {
			continue
		}
		decoder := xml.NewDecoder(strings.NewReader(string(contentBytes)))
		for {
			token, err := decoder.Token()
			if err != nil {
				break
			}
			start, ok := token.(xml.StartElement)
			if !ok || start.Name.Local != "Relationship" {
				continue
			}
			attrs := xmlAttrs(start)
			if strings.EqualFold(attrs["TargetMode"], "External") {
				continue
			}
			target := strings.TrimSpace(attrs["Target"])
			if target == "" || strings.HasPrefix(target, "#") {
				continue
			}
			resolved := resolveRelationshipTarget(relsName, target)
			if resolved != "" && !entries[resolved] {
				appendRepairableIssueOnce(result, "relationship_target_missing", "OOXML relationship target is missing: "+target, relsName)
			}
		}
	}
}

func xmlAttrs(start xml.StartElement) map[string]string {
	attrs := map[string]string{}
	for _, attr := range start.Attr {
		attrs[attr.Name.Local] = attr.Value
	}
	return attrs
}

func resolveRelationshipTarget(relsName string, target string) string {
	if relsName == "_rels/.rels" {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	before, _, ok := strings.Cut(relsName, "/_rels/")
	if !ok {
		return ""
	}
	return strings.TrimPrefix(path.Clean(path.Join(before, target)), "/")
}

func addNotePackageIssues(pkg *ooxmlpkg.DocxPackage, result *Result) {
	contentTypesBytes, _ := pkg.Get("[Content_Types].xml")
	relsBytes, _ := pkg.Get("word/_rels/document.xml.rels")
	contentTypes := string(contentTypesBytes)
	rels := string(relsBytes)
	for _, note := range []struct {
		part        string
		kind        string
		element     string
		contentType string
		relType     string
	}{
		{"word/footnotes.xml", "footnotes", "footnote", "wordprocessingml.footnotes+xml", "/relationships/footnotes"},
		{"word/endnotes.xml", "endnotes", "endnote", "wordprocessingml.endnotes+xml", "/relationships/endnotes"},
	} {
		noteBytes, ok := pkg.Get(note.part)
		if !ok {
			continue
		}
		if !strings.Contains(contentTypes, `PartName="/`+note.part+`"`) || !strings.Contains(contentTypes, note.contentType) {
			appendRepairableIssueOnce(result, note.kind+"_content_type_missing", note.kind+" part exists but [Content_Types].xml does not declare its OOXML content type.", "[Content_Types].xml")
		}
		if !strings.Contains(rels, note.relType) || !strings.Contains(rels, strings.TrimPrefix(note.part, "word/")) {
			appendRepairableIssueOnce(result, note.kind+"_relationship_missing", note.kind+" part exists but word/_rels/document.xml.rels does not link it from the main document.", "word/_rels/document.xml.rels")
		}
		addNoteReferenceConsistencyIssues(pkg, string(noteBytes), note.kind, note.element, result)
	}
}

func addNoteReferenceConsistencyIssues(pkg *ooxmlpkg.DocxPackage, noteXML, kind, element string, result *Result) {
	documentBytes, _ := pkg.Get(documentTarget)
	references := noteIDs(string(documentBytes), element+`Reference`)
	definitions := noteIDs(noteXML, element)
	target := "word/" + kind + ".xml"
	for id := range references {
		if !definitions[id] {
			appendRepairableIssueOnce(result, kind+"_reference_missing_definition", kind+" contains a body marker without a matching note definition.", target)
			break
		}
	}
	for id := range definitions {
		if !references[id] {
			appendRepairableIssueOnce(result, kind+"_definition_unreferenced", kind+" contains a note definition without a matching body marker.", target)
			break
		}
	}
}

func noteIDs(content, element string) map[int]bool {
	pattern := regexp.MustCompile(`<w:` + regexp.QuoteMeta(element) + `\b[^>]*\bw:id="(-?\d+)"`)
	ids := map[int]bool{}
	for _, match := range pattern.FindAllStringSubmatch(content, -1) {
		id, err := strconv.Atoi(match[1])
		if err == nil && id > 0 {
			ids[id] = true
		}
	}
	return ids
}

func addWordXMLStructureIssues(name string, content string, result *Result) {
	if result == nil || !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") || !strings.Contains(content, "xmlns:w=") {
		return
	}
	decoder := xml.NewDecoder(strings.NewReader(content))
	runDepth := 0
	runPropertiesDepth := 0
	textBoxDepth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			if err != io.EOF {
				result.FatalIssues = append(result.FatalIssues, Issue{
					Kind:     "invalid_word_xml",
					Severity: "fatal",
					Message:  "Word XML is not well-formed and may fail to open or render",
					Target:   name,
				})
			}
			return
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Space != wordprocessingMLNamespace {
				continue
			}
			switch typed.Name.Local {
			case "txbxContent":
				textBoxDepth++
			case "r":
				if runDepth > 0 && textBoxDepth == 0 {
					appendFatalIssueOnce(result, "nested_word_run", "Word XML contains a run nested inside another run, which can break Word/LibreOffice compatibility.", name)
				}
				runDepth++
			case "rPr":
				if runDepth > 0 {
					runPropertiesDepth++
				}
			case "bookmarkStart":
				if runPropertiesDepth > 0 {
					appendFatalIssueOnce(result, "bookmark_in_run_properties", "Word XML contains a bookmark inside run properties instead of document content.", name)
				}
			case "ins", "del", "moveFrom", "moveTo":
				appendRepairableIssueOnce(result, "tracked_changes_not_finalized", "final delivery still contains tracked changes; accept or reject revisions before proving thesis format compliance.", name)
			case "commentRangeStart", "commentReference":
				appendRepairableIssueOnce(result, "comments_not_finalized", "final delivery still contains Word comment anchors; remove comments before proving thesis format compliance.", name)
			}
		case xml.EndElement:
			if typed.Name.Space != wordprocessingMLNamespace {
				continue
			}
			switch typed.Name.Local {
			case "txbxContent":
				if textBoxDepth > 0 {
					textBoxDepth--
				}
			case "rPr":
				if runPropertiesDepth > 0 {
					runPropertiesDepth--
				}
			case "r":
				if runDepth > 0 {
					runDepth--
				}
			}
		}
	}
}

func appendFatalIssueOnce(result *Result, kind string, message string, target string) {
	if result == nil || hasIssueKind(result.FatalIssues, kind) {
		return
	}
	result.FatalIssues = append(result.FatalIssues, Issue{
		Kind:     kind,
		Severity: "fatal",
		Message:  message,
		Target:   target,
	})
}

func (v *Verifier) checkCQRWST(ctx context.Context, docxPath string) (cqrwst.Result, error) {
	if v != nil && v.templateProfile != nil {
		return cqrwst.CheckDOCXWithTemplateProfile(ctx, docxPath, v.templateProfile)
	}
	return cqrwst.CheckDOCX(ctx, docxPath)
}

func (v *Verifier) checkClosureArtifacts(result *Result) {
	if v == nil || v.closure == nil || result == nil {
		return
	}
	for _, issue := range templatecontract.Validate(v.closure.TemplateRules) {
		result.FatalIssues = append(result.FatalIssues, Issue{
			Kind:     "closure_template_rule",
			Severity: "fatal",
			Message:  issue.Message,
			Target:   issue.Kind,
		})
	}
	for _, issue := range paperast.Validate(v.closure.PaperAST) {
		result.FatalIssues = append(result.FatalIssues, Issue{
			Kind:     "closure_paper_ast",
			Severity: "fatal",
			Message:  issue.Message,
			Target:   issue.Kind,
		})
	}
	for _, issue := range repaircontract.Validate(v.closure.RepairContract) {
		result.FatalIssues = append(result.FatalIssues, Issue{
			Kind:     "closure_repair_contract",
			Severity: "fatal",
			Message:  issue.Message,
			Target:   issue.Kind,
		})
	}
}

func (v *Verifier) checkRenderedOutput(ctx context.Context, docxPath string, result *Result) {
	if v == nil || result == nil || v.renderOptions == nil || !v.renderOptions.Enabled {
		return
	}
	options := *v.renderOptions
	if len(options.SamePageRules) == 0 && v.closure != nil {
		options.SamePageRules = deriveRenderSamePageRules(v.closure.PaperAST)
	}
	renderResult, err := renderverify.Check(ctx, docxPath, options)
	if err != nil {
		result.FatalIssues = append(result.FatalIssues, Issue{
			Kind:     "render_verify",
			Severity: "fatal",
			Message:  fmt.Sprintf("render verification failed: %v", err),
			Target:   docxPath,
		})
		return
	}
	result.RenderResult = &renderResult
	for _, issue := range renderResult.Issues {
		appendRenderIssue(result, issue)
	}
	if !renderResult.Passed || strings.TrimSpace(v.goldenPath) == "" {
		return
	}
	goldenOptions := options
	goldenOptions.RequiredText = nil
	goldenOptions.ForbiddenText = nil
	goldenOptions.SamePageRules = nil
	goldenOptions.TextStyleRules = nil
	goldenOptions.AllowBlankPage = nil
	goldenOptions.CheckPageFooter = false
	goldenResult, err := renderverify.Check(ctx, v.goldenPath, goldenOptions)
	if err != nil {
		result.FatalIssues = append(result.FatalIssues, Issue{
			Kind:     "golden_render",
			Severity: "fatal",
			Message:  fmt.Sprintf("render golden sample failed: %v", err),
			Target:   v.goldenPath,
		})
		return
	}
	if !goldenResult.Passed {
		for _, issue := range goldenResult.Issues {
			result.Warnings = append(result.Warnings, Issue{Kind: "golden_baseline_unavailable", Severity: "warning", Message: issue.Message, Target: v.goldenPath})
		}
		return
	}
	landmarks := deriveGoldenLandmarks(v.closure)
	regression := goldenregression.CompareSnapshots(goldenregression.Options{
		Candidate:        goldenregression.PageSnapshot{Pages: renderResult.PageTexts, Spans: goldenSpans(renderResult.TextSpans)},
		Golden:           goldenregression.PageSnapshot{Pages: goldenResult.PageTexts, Spans: goldenSpans(goldenResult.TextSpans)},
		CheckPageCount:   envBool("GOLDEN_PAGE_COUNT_STRICT"),
		MaxPageDelta:     envInt("GOLDEN_PAGE_COUNT_MAX_DELTA", 0),
		Landmarks:        landmarks,
		SamePageLandmark: deriveGoldenSamePageLandmarks(v.closure),
		CompareStyles:    true,
	})
	result.GoldenRegression = &regression
	for _, issue := range regression.Issues {
		switch issue.Severity {
		case goldenregression.SeverityError:
			result.RepairableIssues = append(result.RepairableIssues, Issue{
				Kind:     "golden_regression_" + issue.Kind,
				Severity: string(issue.Severity),
				Message:  issue.Message,
				Target:   issue.Target,
			})
		default:
			result.Warnings = append(result.Warnings, Issue{
				Kind:     "golden_regression_" + issue.Kind,
				Severity: string(issue.Severity),
				Message:  issue.Message,
				Target:   issue.Target,
			})
		}
	}
}

func goldenSpans(spans []renderverify.TextSpan) []goldenregression.TextSpan {
	result := make([]goldenregression.TextSpan, 0, len(spans))
	for _, span := range spans {
		result = append(result, goldenregression.TextSpan{Page: span.Page, Text: span.Text, Font: span.Font, FontSize: span.FontSize, X: span.X})
	}
	return result
}

func deriveGoldenLandmarks(closure *ClosureArtifacts) []goldenregression.Landmark {
	if closure == nil {
		return nil
	}
	seen := map[string]bool{}
	var landmarks []goldenregression.Landmark
	for _, node := range closure.PaperAST.Nodes {
		role := strings.ToLower(strings.TrimSpace(node.SemanticRole))
		if role != "abstract_cn" && role != "abstract_en" && role != "toc_title" {
			continue
		}
		text := strings.TrimSpace(node.Text)
		if text == "" || seen[role] {
			continue
		}
		seen[role] = true
		landmarks = append(landmarks, goldenregression.Landmark{Name: role, Text: text})
	}
	return landmarks
}

func (v *Verifier) configureRenderGateFromEnv() {
	if v == nil || !renderverify.DefaultEnabled() {
		return
	}
	v.renderOptions = &renderverify.Options{
		Enabled:         true,
		Strict:          envBoolDefault("RENDER_VERIFY_STRICT", true),
		CheckPageFooter: true,
	}
	if python := strings.TrimSpace(os.Getenv("PDF_TEXT_PYTHON")); python != "" {
		v.renderOptions.TextExtractor = renderverify.PythonPDFTextExtractor{Binary: python}
	}
	if pngDir := strings.TrimSpace(os.Getenv("RENDER_VERIFY_PNG_OUTPUT_DIR")); pngDir != "" {
		v.renderOptions.PNGOutputDir = pngDir
		v.renderOptions.Rasterizer = renderverify.PopplerRasterizer{
			Binary: strings.TrimSpace(os.Getenv("PDFTOPPM_BIN")),
			DPI:    envInt("RENDER_VERIFY_PNG_DPI", 120),
		}
	}
	v.goldenPath = strings.TrimSpace(os.Getenv("GOLDEN_TEMPLATE_PATH"))
}

func appendRenderIssue(result *Result, issue renderverify.Issue) {
	converted := Issue{
		Kind:     "render_" + issue.Kind,
		Severity: string(issue.Severity),
		Message:  issue.Message,
		Target:   issue.Target,
	}
	switch issue.Severity {
	case renderverify.SeverityFatal:
		result.FatalIssues = append(result.FatalIssues, converted)
	case renderverify.SeverityError:
		result.RepairableIssues = append(result.RepairableIssues, converted)
	default:
		result.Warnings = append(result.Warnings, converted)
	}
}

func deriveRenderSamePageRules(ast paperast.Snapshot) []renderverify.SamePageRule {
	title, abstract := findTitleAndAbstractLandmarks(ast)
	if title == "" || abstract == "" {
		return nil
	}
	return []renderverify.SamePageRule{{
		Name:      "title_and_abstract_same_page",
		LeftText:  title,
		RightText: abstract,
	}}
}

func deriveGoldenSamePageLandmarks(closure *ClosureArtifacts) []goldenregression.SamePageLandmark {
	if closure == nil {
		return nil
	}
	title, abstract := findTitleAndAbstractLandmarks(closure.PaperAST)
	if title == "" || abstract == "" {
		return nil
	}
	return []goldenregression.SamePageLandmark{{
		Name:  "title_and_abstract_same_page",
		Left:  title,
		Right: abstract,
	}}
}

func findTitleAndAbstractLandmarks(ast paperast.Snapshot) (string, string) {
	abstractIndex := -1
	abstractText := ""
	for index, node := range ast.Nodes {
		text := strings.TrimSpace(node.Text)
		if text == "" {
			continue
		}
		if node.SemanticRole == "abstract_cn" || node.SemanticRole == "abstract_en" || strings.HasPrefix(compactText(text), "摘要") || strings.HasPrefix(strings.ToLower(compactText(text)), "abstract") {
			abstractIndex = index
			abstractText = text
			break
		}
	}
	if abstractIndex <= 0 {
		return "", ""
	}
	for index := abstractIndex - 1; index >= 0; index-- {
		node := ast.Nodes[index]
		if node.NodeType != "paragraph" {
			continue
		}
		text := strings.TrimSpace(node.Text)
		if isLikelyTitleLandmark(text) {
			return text, abstractText
		}
	}
	return "", ""
}

func isLikelyTitleLandmark(text string) bool {
	compact := compactText(text)
	runeCount := len([]rune(compact))
	if runeCount < 6 || runeCount > 80 {
		return false
	}
	blockedPrefixes := []string{"摘要", "关键词", "Abstract", "KeyWords", "Keywords", "目录", "重庆人文科技学院", "本科毕业论文", "本科毕业设计"}
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(compact, compactText(prefix)) {
			return false
		}
	}
	return true
}

func compactText(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\u3000", " ")
	return strings.Join(strings.Fields(value), "")
}

func envBool(name string) bool {
	return envBoolDefault(name, false)
}

func envBoolDefault(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func finalizeResult(result Result) Result {
	switch {
	case result.Passed:
		result.ComplianceStatus = "format_compliant"
		result.ComplianceReason = "all deterministic verification checks passed"
	case len(result.FatalIssues) > 0:
		result.ComplianceStatus = "rejected"
		result.ComplianceReason = "fatal verification issues prevent compliance proof"
	default:
		result.ComplianceStatus = "review_required"
		result.ComplianceReason = "repairable verification issues remain"
	}
	return result
}
