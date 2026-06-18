package verify

import (
	"context"
	"fmt"
	"html"
	"os"
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

var placeholderPattern = regexp.MustCompile(`\{\{[^{}]+\}\}`)
var visibleReviewFieldPattern = regexp.MustCompile(`(?is)<w:instrText\b[^>]*>\s*(?:TOC|REF|PAGEREF|NOTEREF)\b`)
var verifyParagraphPattern = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`)
var verifyTextPattern = regexp.MustCompile(`(?s)<w:t(?:\s[^>]*)?>(.*?)</w:t>`)
var captionTextPattern = regexp.MustCompile(`^(图|表)\s*(\d+(?:[-.．]\d+)*)\s*\S+`)
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
	addTableFormattingIssues(document, &result)
	addImageFormulaReferenceIssues(document, &result)
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
			if seenHeaders[match[1]] > 0 {
				linked = true
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

	cover := sections[0]
	front := sections[1]
	body := sections[len(sections)-1]
	if strings.Contains(cover, "<w:footerReference") || strings.Contains(cover, "<w:pgNumType") {
		appendRepairableIssueOnce(result, "cover_page_number_present", "cover section contains page-number/footer settings; cover pages should not display page numbers.", documentTarget)
	}
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

func addTableFormattingIssues(document string, result *Result) {
	if result == nil {
		return
	}
	for _, table := range verifyTablePattern.FindAllString(document, -1) {
		if !tableHasThreeLineBorders(table) {
			appendRepairableIssueOnce(result, "table_three_line_format", "table borders are not in three-line format; keep top/header/bottom rules and remove vertical/extra inner rules.", documentTarget)
		}
		if !tableLayoutIsStable(table) {
			appendRepairableIssueOnce(result, "table_layout_not_centered", "table should be centered, non-floating, and constrained to the page text area.", documentTarget)
		}
		if !tableHasRepeatingHeader(table) {
			appendRepairableIssueOnce(result, "table_repeating_header_missing", "long tables should mark the first row as a repeating header row.", documentTarget)
		}
		if !tableCellsFollowStyle(table) {
			appendRepairableIssueOnce(result, "table_cell_style_mismatch", "table cell text should use Chinese Songti/English Times New Roman, compact size, single spacing, horizontal and vertical centering.", documentTarget)
		}
	}
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

func tableCellsFollowStyle(table string) bool {
	cells := verifyTableCellPattern.FindAllString(table, -1)
	if len(cells) == 0 {
		return true
	}
	for _, cell := range cells {
		if !strings.Contains(cell, `<w:vAlign w:val="center"`) {
			return false
		}
		if !strings.Contains(cell, `<w:jc w:val="center"`) {
			return false
		}
		if !strings.Contains(cell, `<w:spacing`) || !strings.Contains(cell, `w:line="240"`) {
			return false
		}
		if !strings.Contains(cell, `w:ascii="Times New Roman"`) && !strings.Contains(cell, `w:hAnsi="Times New Roman"`) {
			return false
		}
		if !strings.Contains(cell, `w:eastAsia="宋体"`) && !strings.Contains(cell, `w:eastAsia="SimSun"`) {
			return false
		}
		if !strings.Contains(cell, `<w:sz w:val="18"`) && !strings.Contains(cell, `<w:sz w:val="21"`) {
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

func addImageFormulaReferenceIssues(document string, result *Result) {
	if result == nil {
		return
	}
	paragraphs := verifyParagraphPattern.FindAllString(document, -1)
	for index, paragraph := range paragraphs {
		text := strings.TrimSpace(verifyXMLText(paragraph))
		if strings.Contains(paragraph, "<w:drawing") || strings.Contains(paragraph, "<w:pict") {
			if strings.Contains(paragraph, "<wp:anchor") {
				appendRepairableIssueOnce(result, "floating_image_anchor", "image uses floating/anchored layout; use inline layout so it stays fixed in the paragraph.", documentTarget)
			}
			if imageWidthOverTextArea(paragraph) {
				appendRepairableIssueOnce(result, "image_width_over_text_area", "image width appears larger than the page text area; scale it proportionally within the text area.", documentTarget)
			}
			nextCaption := nextNonBlankParagraphText(paragraphs, index+1)
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
			appendRepairableIssueOnce(result, "manual_cross_reference", "body text appears to contain manually typed figure/table/formula references; use Word cross-reference fields so references update automatically.", documentTarget)
		}
	}

	for _, table := range verifyTablePattern.FindAllString(document, -1) {
		if !strings.Contains(table, "<m:oMath") && !strings.Contains(table, "<m:oMathPara") {
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

func imageWidthOverTextArea(paragraph string) bool {
	for _, match := range imageExtentPattern.FindAllStringSubmatch(paragraph, -1) {
		if len(match) != 2 {
			continue
		}
		width, err := strconv.Atoi(match[1])
		if err == nil && width > 5800000 {
			return true
		}
	}
	return false
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
	return captionTextPattern.MatchString(strings.TrimSpace(text))
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
	}
	if _, ok := pkg.Get("word/comments.xml"); ok {
		result.RepairableIssues = append(result.RepairableIssues, Issue{
			Kind:     "comments_not_finalized",
			Severity: "error",
			Message:  "final delivery still contains Word comments",
			Target:   "word/comments.xml",
		})
	}
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
	goldenOptions.AllowBlankPage = nil
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
			appendRenderIssue(result, issue)
		}
		return
	}
	regression := goldenregression.CompareSnapshots(goldenregression.Options{
		Candidate:      goldenregression.PageSnapshot{Pages: renderResult.PageTexts},
		Golden:         goldenregression.PageSnapshot{Pages: goldenResult.PageTexts},
		CheckPageCount: envBool("GOLDEN_PAGE_COUNT_STRICT"),
		MaxPageDelta:   envInt("GOLDEN_PAGE_COUNT_MAX_DELTA", 0),
		Landmarks: []goldenregression.Landmark{
			{Name: "abstract", Text: "摘要"},
			{Name: "toc", Text: "目录"},
		},
		SamePageLandmark: deriveGoldenSamePageLandmarks(v.closure),
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

func (v *Verifier) configureRenderGateFromEnv() {
	if v == nil || !renderverify.DefaultEnabled() {
		return
	}
	v.renderOptions = &renderverify.Options{
		Enabled: true,
		Strict:  envBoolDefault("RENDER_VERIFY_STRICT", true),
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
		if node.SemanticRole == "abstract_cn" || strings.HasPrefix(compactText(text), "摘要") {
			abstractIndex = index
			abstractText = "摘要"
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
