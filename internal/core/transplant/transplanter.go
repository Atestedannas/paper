package transplant

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/paper-format-checker/backend/internal/core/blockmap"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/templatecompile"
)

const defaultPatchTarget = "word/document.xml"
const contentTableMaxWidth = 8640

var paragraphPattern = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`)
var finalSectPrPattern = regexp.MustCompile(`(?s)<w:sectPr(?:\s[^>]*)?>.*?</w:sectPr>`)
var tablePattern = regexp.MustCompile(`(?s)<w:tbl(?:\s[^>]*)?>.*?</w:tbl>`)
var tableStartPattern = regexp.MustCompile(`<w:tbl(?:\s[^>]*)?>`)
var tablePropertyStartPattern = regexp.MustCompile(`(?s)<w:tblPr(?:\s[^>]*)?>`)
var tablePropertyPattern = regexp.MustCompile(`(?s)<w:tblPr(?:\s[^>]*)?>.*?</w:tblPr>`)
var tableWidthPattern = regexp.MustCompile(`<w:tblW\b[^>]*/>`)
var tableBordersPattern = regexp.MustCompile(`(?s)<w:tblBorders>.*?</w:tblBorders>`)
var tableCellMarginPattern = regexp.MustCompile(`(?s)<w:tblCellMar>.*?</w:tblCellMar>`)
var tableCellBordersPattern = regexp.MustCompile(`(?s)<w:tcBorders>.*?</w:tcBorders>`)
var floatingTablePropertyPattern = regexp.MustCompile(`(?s)<w:tblpPr\b[^>]*/>`)
var tableOverlapPropertyPattern = regexp.MustCompile(`(?s)<w:tblOverlap\b[^>]*/>`)
var autoTableWidthPattern = regexp.MustCompile(`<w:tblW\b[^>]*\bw:w="0"[^>]*/>`)
var tableGridColWidthPattern = regexp.MustCompile(`<w:gridCol\b[^>]*\bw:w="([0-9]+)"[^>]*/>`)
var tableCellWidthPattern = regexp.MustCompile(`<w:tcW\b[^>]*\bw:w="([0-9]+)"[^>]*/>`)
var tableParagraphIndentPattern = regexp.MustCompile(`<w:ind\b[^>]*/>`)
var tableJustifyBothPattern = regexp.MustCompile(`<w:jc\b[^>]*\bw:val="both"[^>]*/>`)
var tableAnyFontSizePattern = regexp.MustCompile(`<w:sz(Cs)?\b[^>]*/>`)
var tableFontSize24Pattern = regexp.MustCompile(`<w:sz(Cs)? w:val="24"/>`)
var tableRowHeightPattern = regexp.MustCompile(`<w:trHeight\b[^>]*/>`)
var tableRowCantSplitPattern = regexp.MustCompile(`<w:cantSplit\b[^>]*/>`)
var tableRowStartPattern = regexp.MustCompile(`<w:tr(?:\s[^>]*)?>`)
var tableRowPropertyStartPattern = regexp.MustCompile(`<w:trPr(?:\s[^>]*)?>`)
var numberedHeadingPattern = regexp.MustCompile(`^\d+(?:\.\d+)*\s*\S+`)
var tocEntryPattern = regexp.MustCompile(`^.+\s+(\d+|[IVXLCDM]+)$`)
var tableCaptionPattern = regexp.MustCompile(`^(?:\x{8868}|\x{7eed}\x{8868})\s*\S+`)
var headerReferencePattern = regexp.MustCompile(`<w:headerReference\b[^>]*/>`)
var headerReferenceIDPattern = regexp.MustCompile(`<w:headerReference\b[^>]*\br:id="([^"]+)"[^>]*/>`)
var footerReferenceIDPattern = regexp.MustCompile(`<w:footerReference\b[^>]*\br:id="([^"]+)"[^>]*/>`)

type GenerateInput struct {
	CompiledTemplate *templatecompile.CompiledTemplatePackage
	Mapping          *blockmap.MappingResult
	OutputPath       string
}

type Transplanter struct{}

type replacementSet struct {
	inline             map[string]string
	paragraph          map[string]string
	fallbackParagraphs string
	fallbackReferences string
	fallbackThanks     string
}

func NewTransplanter() *Transplanter {
	return &Transplanter{}
}

func (t *Transplanter) Generate(ctx context.Context, input GenerateInput) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateInput(input); err != nil {
		return err
	}

	pkg, err := ooxmlpkg.Open(input.CompiledTemplate.SkeletonPath)
	if err != nil {
		return fmt.Errorf("open skeleton docx %q: %w", input.CompiledTemplate.SkeletonPath, err)
	}

	replacements := buildReplacements(input.Mapping.Bindings, input.CompiledTemplate.MappingContract)
	coverFields := input.Mapping.CoverFields
	for _, target := range patchTargets(input.CompiledTemplate.PatchTargets) {
		content, ok := pkg.Get(target)
		if !ok {
			return fmt.Errorf("patch target %q not found in skeleton docx", target)
		}
		patched, applied := applyReplacements(string(content), replacements)
		fullBodyRebuilt := false
		if target == defaultPatchTarget {
			if rebuilt := rebuildCQRWSTDocumentBody(patched, coverFields, replacements.fallbackParagraphs, replacements.fallbackReferences, replacements.fallbackThanks); rebuilt != "" {
				patched = rebuilt
				applied = true
				fullBodyRebuilt = true
			} else if !applied && strings.TrimSpace(replacements.fallbackParagraphs) != "" {
				patched = injectParagraphsBeforeFinalSection(patched, replacements.fallbackParagraphs)
			}
		}
		if target == defaultPatchTarget && len(coverFields) > 0 && !fullBodyRebuilt {
			if rebuilt := rebuildCQRWSTCoverPage(patched, coverFields); rebuilt != "" {
				patched = rebuilt
			} else {
				patched = fillCoverTableFields(patched, coverFields)
			}
		}
		if target == defaultPatchTarget && !fullBodyRebuilt && strings.TrimSpace(replacements.fallbackReferences) != "" {
			patched = injectParagraphsAfterHeading(patched, replacements.fallbackReferences, []string{"References", "参考文献"})
		}
		patched = normalizeRendererIncompatibleXML(patched)
		if target == defaultPatchTarget {
			if err := validateXML(patched); err != nil {
				return fmt.Errorf("generated %s is invalid XML: %w", target, err)
			}
		}
		pkg.Set(target, []byte(patched))
	}
	normalizePackageXML(pkg)
	normalizeCQRWSTMainHeader(pkg, coverFields)
	normalizeCQRWSTMainFooter(pkg)

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(input.OutputPath), 0o755); err != nil {
		return fmt.Errorf("create output dir for generated docx %q: %w", input.OutputPath, err)
	}
	if err := pkg.Write(input.OutputPath); err != nil {
		return fmt.Errorf("write generated docx %q: %w", input.OutputPath, err)
	}

	return nil
}

func validateInput(input GenerateInput) error {
	if input.CompiledTemplate == nil {
		return fmt.Errorf("compiled template is nil")
	}
	if strings.TrimSpace(input.CompiledTemplate.SkeletonPath) == "" {
		return fmt.Errorf("compiled template skeleton path is empty")
	}
	if input.Mapping == nil {
		return fmt.Errorf("mapping is nil")
	}
	if strings.TrimSpace(input.OutputPath) == "" {
		return fmt.Errorf("output path is empty")
	}
	for _, target := range input.CompiledTemplate.PatchTargets {
		if strings.TrimSpace(target) == "" {
			return fmt.Errorf("patch target is empty")
		}
	}
	return nil
}

func patchTargets(targets []string) []string {
	if len(targets) == 0 {
		return []string{defaultPatchTarget}
	}
	return targets
}

func buildReplacements(bindings []blockmap.Binding, contract templatecompile.MappingContract) replacementSet {
	grouped := make(map[string][]string)
	for _, binding := range bindings {
		grouped[binding.BlockID] = append(grouped[binding.BlockID], binding.Payload)
	}

	replacements := replacementSet{
		inline:    make(map[string]string, len(grouped)),
		paragraph: make(map[string]string),
	}
	for blockID, payloads := range grouped {
		replacement := strings.Join(escapePayloads(payloads), "\n")
		replacements.inline["{{"+blockID+"}}"] = replacement
		if token := strings.TrimSpace(contract.BlockBindings[blockID]); token != "" {
			replacements.inline[token] = replacement
			if blockID == "content_blocks" {
				replacements.paragraph[token] = renderParagraphs(payloads)
			}
		}
		if blockID == "content_blocks" {
			paragraphs := renderParagraphs(payloads)
			replacements.paragraph["{{"+blockID+"}}"] = paragraphs
			replacements.fallbackParagraphs = paragraphs
		}
		if blockID == "references" {
			replacements.fallbackReferences = renderReferences(payloads)
		}
		if blockID == "acknowledgement" {
			replacements.fallbackThanks = renderAcknowledgements(payloads)
		}
	}
	return replacements
}

func escapePayloads(payloads []string) []string {
	escaped := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		escaped = append(escaped, html.EscapeString(payload))
	}
	return escaped
}

func renderParagraphs(payloads []string) string {
	payloads = coalesceFragmentedTextPayloads(payloads)
	payloads, hadSourceTOC := removeSourceTOCPayloads(payloads)
	var builder strings.Builder
	generatedTOCWritten := false
	for index, payload := range payloads {
		payload = strings.TrimSpace(payload)
		if payload == "" {
			continue
		}
		normalized := strings.TrimSpace(payload)
		if hadSourceTOC && !generatedTOCWritten && numberedHeadingPattern.MatchString(normalized) {
			builder.WriteString(pageBreakParagraph())
			builder.WriteString(renderGeneratedTOC(payloads))
			builder.WriteString(pageBreakParagraph())
			generatedTOCWritten = true
		}
		if isTableCaption(normalized) && nextNonEmptyPayloadIsTable(payloads, index+1) {
			builder.WriteString(renderTableCaption(normalized, true))
			continue
		}
		builder.WriteString(renderStyledPayload(payload))
	}
	return builder.String()
}

func removeSourceTOCPayloads(payloads []string) ([]string, bool) {
	filtered := make([]string, 0, len(payloads))
	skippingTOC := false
	removed := false
	for _, payload := range payloads {
		normalized := strings.TrimSpace(payload)
		if normalized == "" {
			continue
		}
		if isTOCHeading(normalized) {
			skippingTOC = true
			removed = true
			continue
		}
		if skippingTOC {
			if isTOCEntry(normalized) {
				continue
			}
			skippingTOC = false
		}
		filtered = append(filtered, normalized)
	}
	return filtered, removed
}

func renderGeneratedTOC(payloads []string) string {
	entries := generatedTOCEntries(payloads)
	if len(entries) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(centeredParagraphWithFonts("\u76ee      \u5f55", paragraphStyle{
		Size:       32,
		Line:       360,
		After:      624,
		AfterLines: 200,
	}, "Times New Roman", "黑体"))
	for _, entry := range entries {
		builder.WriteString(paragraphWithStyle(entry, paragraphStyle{Size: 20, FirstLine: 0, Line: 240}))
	}
	return builder.String()
}

func renderReferences(payloads []string) string {
	return renderLinePayloads(payloads, paragraphStyle{Size: 21, FirstLine: 0, Line: 288})
}

func renderAcknowledgements(payloads []string) string {
	return renderLinePayloads(payloads, paragraphStyle{Size: 21, FirstLine: 420, FirstLineChars: 200, Line: 360, AsciiFont: "宋体", EastAsiaFont: "宋体"})
}

func renderLeadLabelParagraph(text string, label string, asciiFont string, eastAsiaFont string) string {
	style := paragraphStyle{Size: 24, FirstLine: 480, FirstLineChars: 200, Line: 360, After: 624, AfterLines: 200}
	remainder := strings.TrimPrefix(text, label)
	var builder strings.Builder
	builder.WriteString(`<w:p><w:pPr>`)
	builder.WriteString(spacingXML(style))
	builder.WriteString(`<w:ind w:firstLineChars="200" w:firstLine="480"/>`)
	builder.WriteString(`<w:rPr>`)
	builder.WriteString(runPropertiesForText(text, style.Size, false))
	builder.WriteString(`</w:rPr></w:pPr>`)
	builder.WriteString(runXMLWithFonts(label, 30, true, asciiFont, eastAsiaFont))
	if strings.TrimSpace(remainder) != "" {
		builder.WriteString(runXMLPreservingText(remainder, style.Size, false))
	}
	builder.WriteString(`</w:p>`)
	return builder.String()
}

func renderLinePayloads(payloads []string, style paragraphStyle) string {
	var builder strings.Builder
	for _, payload := range payloads {
		for _, line := range strings.Split(payload, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			builder.WriteString(paragraphWithStyle(line, style))
		}
	}
	return builder.String()
}

func generatedTOCEntries(payloads []string) []string {
	entries := make([]string, 0, 16)
	seen := make(map[string]bool)
	for _, payload := range payloads {
		normalized := strings.TrimSpace(payload)
		if normalized == "" || !numberedHeadingPattern.MatchString(normalized) {
			continue
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		entries = append(entries, normalized)
	}
	return entries
}

func coalesceFragmentedTextPayloads(payloads []string) []string {
	payloads = splitEmbeddedAbstractPayloads(payloads)
	coalesced := make([]string, 0, len(payloads))
	buffer := ""
	flush := func() {
		if strings.TrimSpace(buffer) != "" {
			coalesced = append(coalesced, strings.TrimSpace(buffer))
			buffer = ""
		}
	}
	for _, payload := range payloads {
		normalized := strings.TrimSpace(payload)
		if normalized == "" {
			continue
		}
		if isStructuralPayload(normalized) {
			flush()
			coalesced = append(coalesced, normalized)
			continue
		}
		if buffer == "" {
			buffer = normalized
			continue
		}
		if shouldMergeTextFragments(buffer, normalized) {
			buffer = joinTextFragments(buffer, normalized)
			continue
		}
		flush()
		buffer = normalized
	}
	flush()
	return coalesced
}

func splitEmbeddedAbstractPayloads(payloads []string) []string {
	split := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		normalized := strings.TrimSpace(payload)
		if normalized == "" {
			continue
		}
		if index := strings.Index(normalized, "Abstract:"); index > 0 {
			before := strings.TrimSpace(normalized[:index])
			after := strings.TrimSpace(normalized[index:])
			if before != "" {
				split = append(split, before)
			}
			if after != "" {
				split = append(split, after)
			}
			continue
		}
		split = append(split, normalized)
	}
	return split
}

func isStructuralPayload(text string) bool {
	normalized := strings.TrimSpace(text)
	return isTableXML(normalized) ||
		isTableCaption(normalized) ||
		isTOCHeading(normalized) ||
		isTOCEntry(normalized) ||
		numberedHeadingPattern.MatchString(normalized) ||
		isAbstractOrKeywordPayload(normalized)
}

func isAbstractOrKeywordPayload(text string) bool {
	normalized := strings.TrimSpace(text)
	return strings.HasPrefix(normalized, "摘要") ||
		strings.HasPrefix(normalized, "关键词") ||
		strings.HasPrefix(normalized, "Abstract") ||
		strings.HasPrefix(normalized, "Key words") ||
		strings.HasPrefix(normalized, "Keywords")
}

func shouldMergeTextFragments(left string, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	if endsWithTerminalPunctuation(left) {
		return false
	}
	if startsWithParagraphBoundary(right) {
		return false
	}
	return true
}

func endsWithTerminalPunctuation(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	last := []rune(text)[len([]rune(text))-1]
	return strings.ContainsRune("銆傦紒锛燂紱.!?;", last)
}

func startsWithParagraphBoundary(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	return numberedHeadingPattern.MatchString(text) ||
		isTableCaption(text) ||
		strings.HasPrefix(text, "）") ||
		strings.HasPrefix(text, "(")
}

func joinTextFragments(left string, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	if isMostlyASCII(left) && isMostlyASCII(right) {
		return left + " " + right
	}
	return left + right
}

func renderStyledPayload(text string) string {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return ""
	}
	if isTableXML(normalized) {
		return renderCleanTableFromOOXML(normalized)
	}
	switch {
	case isTOCHeading(normalized):
		return pageBreakParagraph() + centerParagraph(normalized, 32, true)
	case isTOCEntry(normalized):
		return paragraphWithStyle(normalized, paragraphStyle{Size: 24, FirstLine: 0, Line: 360})
	case isTableCaption(normalized):
		return renderTableCaption(normalized, false)
	case strings.HasPrefix(normalized, "摘要"):
		return renderLeadLabelParagraph(normalized, leadLabel(normalized, "摘要：", "摘要"), "Times New Roman", "黑体")
	case strings.HasPrefix(normalized, "关键词"):
		return renderLeadLabelParagraph(normalized, leadLabel(normalized, "关键词：", "关键词"), "Times New Roman", "黑体")
	case strings.HasPrefix(normalized, "Abstract"):
		return renderLeadLabelParagraph(normalized, leadLabel(normalized, "Abstract:", "Abstract"), "Times New Roman", "Times New Roman")
	case strings.HasPrefix(normalized, "Key words"), strings.HasPrefix(normalized, "Keywords"):
		if strings.HasPrefix(normalized, "Keywords") {
			return renderLeadLabelParagraph(normalized, leadLabel(normalized, "Keywords:", "Keywords"), "Times New Roman", "Times New Roman")
		}
		return renderLeadLabelParagraph(normalized, leadLabel(normalized, "Key words:", "Key words"), "Times New Roman", "Times New Roman")
	case numberedHeadingPattern.MatchString(normalized):
		level := headingLevel(normalized)
		if level <= 1 {
			return paragraphWithStyle(normalized, paragraphStyle{Size: 32, Bold: true, Line: 360, Before: 312, BeforeLines: 100, After: 312, AfterLines: 100, Alignment: "left", SnapToGridOff: true, AdjustRightIndZero: true, AsciiFont: "宋体", EastAsiaFont: "宋体"})
		}
		if level == 2 {
			return paragraphWithStyle(normalized, paragraphStyle{Size: 30, Bold: true, Line: 360, AsciiFont: "宋体", EastAsiaFont: "宋体"})
		}
		return paragraphWithStyle(normalized, paragraphStyle{Size: 28, Bold: true, Line: 360, AsciiFont: "宋体", EastAsiaFont: "宋体"})
	default:
		return paragraphWithStyle(normalized, paragraphStyle{Size: 24, FirstLine: 480, FirstLineChars: 200, Line: 360, AsciiFont: "宋体", EastAsiaFont: "宋体"})
	}
}

func leadLabel(text string, preferred string, fallback string) string {
	if strings.HasPrefix(text, preferred) {
		return preferred
	}
	return fallback
}

func nextNonEmptyPayloadIsTable(payloads []string, start int) bool {
	for _, payload := range payloads[start:] {
		normalized := strings.TrimSpace(payload)
		if normalized == "" {
			continue
		}
		return isTableXML(normalized)
	}
	return false
}

func isTableCaption(text string) bool {
	return tableCaptionPattern.MatchString(strings.TrimSpace(text))
}

func renderTableCaption(text string, keepNext bool) string {
	return paragraphWithStyle(text, paragraphStyle{
		Size:      21,
		FirstLine: 0,
		Line:      300,
		Alignment: "center",
		KeepNext:  keepNext,
	})
}

func isTableXML(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "<w:tbl") && strings.Contains(text, "</w:tbl>")
}

func renderCleanTableFromOOXML(tableXML string) string {
	rows := structuredTableRowsFromOOXML(tableXML)
	if len(rows) == 0 {
		normalized := normalizeRendererIncompatibleXML(tableXML)
		normalized = ensureTableHasStableLayout(normalized)
		normalized = constrainContentTableWidth(normalized, contentTableMaxWidth)
		normalized = normalizeDenseTableReadability(normalized)
		normalized = applyThreeLineTableBorders(normalized)
		normalized = allowTableRowsToSplitAndRepeatHeader(normalized)
		return normalized
	}
	return renderStructuredTable(rows, tableGridWidths(tableXML))
}

type structuredTableCell struct {
	Text     string
	Span     int
	VMerge   string
	Continue bool
}

func structuredTableRowsFromOOXML(tableXML string) [][]structuredTableCell {
	rowXMLs := tableRowPattern.FindAllString(tableXML, -1)
	rows := make([][]structuredTableCell, 0, len(rowXMLs))
	for _, rowXML := range rowXMLs {
		cellXMLs := tableCellPattern.FindAllString(rowXML, -1)
		if len(cellXMLs) == 0 {
			continue
		}
		row := make([]structuredTableCell, 0, len(cellXMLs))
		for _, cellXML := range cellXMLs {
			cell := structuredTableCell{
				Text: strings.TrimSpace(xmlText(cellXML)),
				Span: tableCellGridSpan(cellXML),
			}
			cell.VMerge, cell.Continue = tableCellVMerge(cellXML)
			row = append(row, cell)
		}
		rows = append(rows, row)
	}
	return rows
}

func tableCellGridSpan(cellXML string) int {
	match := regexp.MustCompile(`<w:gridSpan\b[^>]*\bw:val="([0-9]+)"[^>]*/>`).FindStringSubmatch(cellXML)
	if len(match) != 2 {
		return 1
	}
	var span int
	if _, err := fmt.Sscanf(match[1], "%d", &span); err != nil || span < 1 {
		return 1
	}
	return span
}

func tableCellVMerge(cellXML string) (string, bool) {
	match := regexp.MustCompile(`<w:vMerge\b([^>]*)/>`).FindStringSubmatch(cellXML)
	if len(match) != 2 {
		return "", false
	}
	valueMatch := regexp.MustCompile(`w:val="([^"]+)"`).FindStringSubmatch(match[1])
	if len(valueMatch) == 2 {
		return valueMatch[1], true
	}
	return "", true
}

func renderStructuredTable(rows [][]structuredTableCell, sourceWidths []int) string {
	columns := structuredColumnCount(rows)
	if len(sourceWidths) > columns {
		columns = len(sourceWidths)
	}
	if columns == 0 {
		return ""
	}
	widths := sourceWidths
	if len(widths) != columns {
		widths = evenWidths(columns, minInt(contentTableMaxWidth, sumInts(sourceWidths)))
	}
	targetWidth := sumInts(widths)
	if targetWidth <= 0 || targetWidth > contentTableMaxWidth {
		targetWidth = contentTableMaxWidth
		widths = scaledWidths(widths, targetWidth)
	}
	dense := columns >= 6
	if dense {
		widths = enforceMinimumColumnWidths(widths, targetWidth, 900)
	}
	fontSize := 18
	margin := `<w:tblCellMar><w:top w:w="60" w:type="dxa"/><w:start w:w="80" w:type="dxa"/><w:bottom w:w="60" w:type="dxa"/><w:end w:w="80" w:type="dxa"/></w:tblCellMar>`
	if dense {
		fontSize = 16
		margin = `<w:tblCellMar><w:top w:w="20" w:type="dxa"/><w:start w:w="20" w:type="dxa"/><w:bottom w:w="20" w:type="dxa"/><w:end w:w="20" w:type="dxa"/></w:tblCellMar>`
	}

	var builder strings.Builder
	builder.WriteString(`<w:tbl><w:tblPr>`)
	builder.WriteString(`<w:tblBorders><w:top w:val="single" w:sz="12" w:space="0" w:color="000000"/><w:left w:val="nil"/><w:bottom w:val="single" w:sz="12" w:space="0" w:color="000000"/><w:right w:val="nil"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="000000"/><w:insideV w:val="nil"/></w:tblBorders>`)
	builder.WriteString(fmt.Sprintf(`<w:tblW w:w="%d" w:type="dxa"/>`, targetWidth))
	builder.WriteString(`<w:jc w:val="center"/><w:tblLayout w:type="fixed"/>`)
	builder.WriteString(margin)
	builder.WriteString(`</w:tblPr><w:tblGrid>`)
	for _, width := range widths {
		builder.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, width))
	}
	builder.WriteString(`</w:tblGrid>`)
	for rowIndex, row := range rows {
		builder.WriteString(`<w:tr>`)
		if rowIndex == 0 {
			builder.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		column := 0
		for _, cell := range row {
			span := cell.Span
			if span < 1 {
				span = 1
			}
			cellWidth := sumSpanWidths(widths, column, span)
			builder.WriteString(`<w:tc><w:tcPr>`)
			if !dense {
				builder.WriteString(fmt.Sprintf(`<w:tcW w:w="%d" w:type="dxa"/>`, cellWidth))
			}
			if span > 1 {
				builder.WriteString(fmt.Sprintf(`<w:gridSpan w:val="%d"/>`, span))
			}
			if cell.Continue {
				if cell.VMerge == "" {
					builder.WriteString(`<w:vMerge/>`)
				} else {
					builder.WriteString(fmt.Sprintf(`<w:vMerge w:val="%s"/>`, html.EscapeString(cell.VMerge)))
				}
			}
			builder.WriteString(`<w:vAlign w:val="center"/></w:tcPr>`)
			builder.WriteString(cleanTableCellParagraphWithSize(cell.Text, rowIndex == 0, fontSize))
			builder.WriteString(`</w:tc>`)
			column += span
		}
		builder.WriteString(`</w:tr>`)
	}
	builder.WriteString(`</w:tbl>`)
	return builder.String()
}

func structuredColumnCount(rows [][]structuredTableCell) int {
	columns := 0
	for _, row := range rows {
		count := 0
		for _, cell := range row {
			if cell.Span < 1 {
				count++
			} else {
				count += cell.Span
			}
		}
		if count > columns {
			columns = count
		}
	}
	return columns
}

func evenWidths(columns int, total int) []int {
	if columns <= 0 {
		return nil
	}
	if total <= 0 {
		total = contentTableMaxWidth
	}
	widths := make([]int, columns)
	base := total / columns
	for index := range widths {
		widths[index] = base
	}
	widths[len(widths)-1] += total - base*columns
	return widths
}

func sumSpanWidths(widths []int, start int, span int) int {
	total := 0
	for index := start; index < start+span && index < len(widths); index++ {
		total += widths[index]
	}
	if total <= 0 && len(widths) > 0 {
		return widths[0]
	}
	return total
}

func minInt(a int, b int) int {
	if a < b && a > 0 {
		return a
	}
	return b
}

func ensureTableHasStableLayout(tableXML string) string {
	if !strings.Contains(tableXML, "<w:tblPr") {
		return tableStartPattern.ReplaceAllStringFunc(tableXML, func(tableStart string) string {
			return tableStart + `<w:tblPr><w:tblLayout w:type="fixed"/><w:jc w:val="center"/></w:tblPr>`
		})
	}
	tableProperties := tablePropertyPattern.FindString(tableXML)
	tableXML = tablePropertyStartPattern.ReplaceAllStringFunc(tableXML, func(tblPrStart string) string {
		insert := ""
		if !strings.Contains(tableProperties, "<w:tblLayout") {
			insert += `<w:tblLayout w:type="fixed"/>`
		}
		if !strings.Contains(tableProperties, "<w:jc ") {
			insert += `<w:jc w:val="center"/>`
		}
		return tblPrStart + insert
	})
	return tableXML
}

func constrainContentTableWidth(tableXML string, maxWidth int) string {
	if maxWidth <= 0 {
		return tableXML
	}
	gridWidths := tableGridWidths(tableXML)
	total := 0
	for _, width := range gridWidths {
		total += width
	}
	target := total
	if target <= 0 || target > maxWidth {
		target = maxWidth
	}
	if len(gridWidths) > 0 && total > maxWidth {
		scaled := scaledWidths(gridWidths, maxWidth)
		if len(scaled) >= 6 {
			scaled = enforceMinimumColumnWidths(scaled, maxWidth, 900)
		}
		index := 0
		tableXML = tableGridColWidthPattern.ReplaceAllStringFunc(tableXML, func(_ string) string {
			if index >= len(scaled) {
				return fmt.Sprintf(`<w:gridCol w:w="%d"/>`, maxWidth/len(scaled))
			}
			width := scaled[index]
			index++
			return fmt.Sprintf(`<w:gridCol w:w="%d"/>`, width)
		})
		tableXML = tableCellWidthPattern.ReplaceAllStringFunc(tableXML, func(match string) string {
			matches := tableCellWidthPattern.FindStringSubmatch(match)
			if len(matches) != 2 {
				return match
			}
			var width int
			if _, err := fmt.Sscanf(matches[1], "%d", &width); err != nil {
				return match
			}
			scaledWidth := int(float64(width)*float64(maxWidth)/float64(total) + 0.5)
			if scaledWidth < 1 {
				scaledWidth = 1
			}
			return fmt.Sprintf(`<w:tcW w:w="%d" w:type="dxa"/>`, scaledWidth)
		})
	}
	widthXML := fmt.Sprintf(`<w:tblW w:w="%d" w:type="dxa"/>`, target)
	if tableWidthPattern.MatchString(tableXML) {
		return tableWidthPattern.ReplaceAllString(tableXML, widthXML)
	}
	return tablePropertyStartPattern.ReplaceAllStringFunc(tableXML, func(tblPrStart string) string {
		return tblPrStart + widthXML
	})
}

func tableGridWidths(tableXML string) []int {
	matches := tableGridColWidthPattern.FindAllStringSubmatch(tableXML, -1)
	widths := make([]int, 0, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		var width int
		if _, err := fmt.Sscanf(match[1], "%d", &width); err == nil && width > 0 {
			widths = append(widths, width)
		}
	}
	return widths
}

func scaledWidths(widths []int, targetTotal int) []int {
	scaled := make([]int, len(widths))
	total := 0
	for _, width := range widths {
		total += width
	}
	if total <= 0 || len(widths) == 0 {
		return scaled
	}
	running := 0
	for index, width := range widths {
		if index == len(widths)-1 {
			scaled[index] = targetTotal - running
			break
		}
		scaledWidth := int(float64(width)*float64(targetTotal)/float64(total) + 0.5)
		if scaledWidth < 1 {
			scaledWidth = 1
		}
		scaled[index] = scaledWidth
		running += scaledWidth
	}
	return scaled
}

func enforceMinimumColumnWidths(widths []int, targetTotal int, minimum int) []int {
	if len(widths) == 0 || minimum <= 0 || minimum*len(widths) > targetTotal {
		return widths
	}
	adjusted := append([]int(nil), widths...)
	for index, width := range adjusted {
		if width < minimum {
			adjusted[index] = minimum
		}
	}
	for over := sumInts(adjusted) - targetTotal; over > 0; over = sumInts(adjusted) - targetTotal {
		largest := -1
		for index, width := range adjusted {
			if width <= minimum {
				continue
			}
			if largest == -1 || width > adjusted[largest] {
				largest = index
			}
		}
		if largest == -1 {
			break
		}
		reduction := adjusted[largest] - minimum
		if reduction > over {
			reduction = over
		}
		adjusted[largest] -= reduction
	}
	return adjusted
}

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func normalizeDenseTableReadability(tableXML string) string {
	if len(tableGridWidths(tableXML)) < 6 {
		return tableXML
	}
	tableXML = tableCellWidthPattern.ReplaceAllString(tableXML, "")
	tableXML = tableAnyFontSizePattern.ReplaceAllStringFunc(tableXML, func(sizeXML string) string {
		if strings.HasPrefix(sizeXML, "<w:szCs") {
			return `<w:szCs w:val="16"/>`
		}
		return `<w:sz w:val="16"/>`
	})
	margins := `<w:tblCellMar><w:top w:w="20" w:type="dxa"/><w:start w:w="20" w:type="dxa"/><w:bottom w:w="20" w:type="dxa"/><w:end w:w="20" w:type="dxa"/></w:tblCellMar>`
	if tableCellMarginPattern.MatchString(tableXML) {
		return tableCellMarginPattern.ReplaceAllString(tableXML, margins)
	}
	return tablePropertyStartPattern.ReplaceAllStringFunc(tableXML, func(tblPrStart string) string {
		return tblPrStart + margins
	})
}

func applyThreeLineTableBorders(tableXML string) string {
	tableXML = tableCellBordersPattern.ReplaceAllString(tableXML, "")
	borders := `<w:tblBorders><w:top w:val="single" w:sz="12" w:space="0" w:color="000000"/><w:left w:val="nil"/><w:bottom w:val="single" w:sz="12" w:space="0" w:color="000000"/><w:right w:val="nil"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="000000"/><w:insideV w:val="nil"/></w:tblBorders>`
	if tableBordersPattern.MatchString(tableXML) {
		return tableBordersPattern.ReplaceAllString(tableXML, borders)
	}
	return tablePropertyStartPattern.ReplaceAllStringFunc(tableXML, func(tblPrStart string) string {
		return tblPrStart + borders
	})
}

func allowTableRowsToSplitAndRepeatHeader(tableXML string) string {
	tableXML = tableRowCantSplitPattern.ReplaceAllString(tableXML, "")
	firstRow := tableRowPattern.FindString(tableXML)
	if firstRow == "" || strings.Contains(firstRow, "<w:tblHeader") {
		return tableXML
	}
	updated := firstRow
	if tableRowPropertyStartPattern.MatchString(firstRow) {
		updated = tableRowPropertyStartPattern.ReplaceAllStringFunc(firstRow, func(trPrStart string) string {
			return trPrStart + `<w:tblHeader/>`
		})
	} else {
		updated = tableRowStartPattern.ReplaceAllStringFunc(firstRow, func(rowStart string) string {
			return rowStart + `<w:trPr><w:tblHeader/></w:trPr>`
		})
	}
	return strings.Replace(tableXML, firstRow, updated, 1)
}

func renderCleanTable(rows [][]string) string {
	columns := maxColumns(rows)
	if columns == 0 {
		return ""
	}
	widths := cleanTableWidths(columns)
	var builder strings.Builder
	builder.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="8640" w:type="dxa"/><w:jc w:val="center"/><w:tblLayout w:type="fixed"/><w:tblBorders><w:top w:val="single" w:sz="4" w:space="0" w:color="000000"/><w:left w:val="single" w:sz="4" w:space="0" w:color="000000"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="000000"/><w:right w:val="single" w:sz="4" w:space="0" w:color="000000"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="000000"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="000000"/></w:tblBorders><w:tblCellMar><w:top w:w="60" w:type="dxa"/><w:start w:w="80" w:type="dxa"/><w:bottom w:w="60" w:type="dxa"/><w:end w:w="80" w:type="dxa"/></w:tblCellMar><w:tblLook w:val="04A0" w:firstRow="1" w:lastRow="0" w:firstColumn="1" w:lastColumn="0" w:noHBand="0" w:noVBand="1"/></w:tblPr><w:tblGrid>`)
	for _, width := range widths {
		builder.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, width))
	}
	builder.WriteString(`</w:tblGrid>`)
	for rowIndex, row := range rows {
		builder.WriteString(`<w:tr>`)
		for col := 0; col < columns; col++ {
			text := ""
			if col < len(row) {
				text = row[col]
			}
			builder.WriteString(fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:tcBorders><w:top w:val="single" w:color="000000" w:sz="4" w:space="0"/><w:left w:val="single" w:color="000000" w:sz="4" w:space="0"/><w:bottom w:val="single" w:color="000000" w:sz="4" w:space="0"/><w:right w:val="single" w:color="000000" w:sz="4" w:space="0"/></w:tcBorders><w:vAlign w:val="center"/></w:tcPr>`, widths[col]))
			builder.WriteString(cleanTableCellParagraph(text, rowIndex == 0))
			builder.WriteString(`</w:tc>`)
		}
		builder.WriteString(`</w:tr>`)
	}
	builder.WriteString(`</w:tbl>`)
	return builder.String()
}

func maxColumns(rows [][]string) int {
	columns := 0
	for _, row := range rows {
		if len(row) > columns {
			columns = len(row)
		}
	}
	return columns
}

func cleanTableWidths(columns int) []int {
	widths := make([]int, columns)
	if columns == 4 {
		return []int{3000, 2100, 1500, 2040}
	}
	width := 8640 / columns
	for i := range widths {
		widths[i] = width
	}
	return widths
}

func cleanTableCellParagraph(text string, header bool) string {
	return cleanTableCellParagraphWithSize(text, header, 18)
}

func cleanTableCellParagraphWithSize(text string, header bool, size int) string {
	var builder strings.Builder
	builder.WriteString(`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="240" w:lineRule="auto"/><w:jc w:val="center"/><w:rPr>`)
	builder.WriteString(runPropertiesWithFonts(size, header, "Times New Roman", "SimSun"))
	builder.WriteString(`</w:rPr></w:pPr>`)
	builder.WriteString(runXMLWithFonts(text, size, header, "Times New Roman", "SimSun"))
	builder.WriteString(`</w:p>`)
	return builder.String()
}

func isTOCHeading(text string) bool {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), "") == "目录"
}

func isTOCEntry(text string) bool {
	return tocEntryPattern.MatchString(strings.TrimSpace(text))
}

func pageBreakParagraph() string {
	return `<w:p><w:r><w:br w:type="page"/></w:r></w:p>`
}

type paragraphStyle struct {
	Size               int
	Bold               bool
	BoldPrefix         string
	FirstLine          int
	FirstLineChars     int
	Line               int
	Before             int
	After              int
	BeforeLines        int
	AfterLines         int
	Alignment          string
	KeepNext           bool
	SnapToGridOff      bool
	AdjustRightIndZero bool
	AsciiFont          string
	EastAsiaFont       string
}

func paragraphWithStyle(text string, style paragraphStyle) string {
	if style.Size == 0 {
		style.Size = 24
	}
	if style.Line == 0 {
		style.Line = 360
	}
	var builder strings.Builder
	builder.WriteString(`<w:p><w:pPr>`)
	if style.KeepNext {
		builder.WriteString(`<w:keepNext/>`)
	}
	if style.AdjustRightIndZero {
		builder.WriteString(`<w:adjustRightInd w:val="0"/>`)
	}
	if style.SnapToGridOff {
		builder.WriteString(`<w:snapToGrid w:val="0"/>`)
	}
	builder.WriteString(spacingXML(style))
	if style.FirstLine > 0 {
		if style.FirstLineChars > 0 {
			builder.WriteString(fmt.Sprintf(`<w:ind w:firstLineChars="%d" w:firstLine="%d"/>`, style.FirstLineChars, style.FirstLine))
		} else {
			builder.WriteString(fmt.Sprintf(`<w:ind w:firstLine="%d"/>`, style.FirstLine))
		}
	}
	if style.Alignment != "" {
		builder.WriteString(fmt.Sprintf(`<w:jc w:val="%s"/>`, style.Alignment))
	}
	builder.WriteString(`<w:rPr>`)
	builder.WriteString(runPropertiesForParagraphStyle(text, style))
	builder.WriteString(`</w:rPr></w:pPr>`)
	if style.BoldPrefix != "" && strings.HasPrefix(text, style.BoldPrefix) {
		builder.WriteString(runXMLForParagraphStyle(style.BoldPrefix, style, true))
		builder.WriteString(runXMLForParagraphStyle(strings.TrimPrefix(text, style.BoldPrefix), style, style.Bold))
	} else {
		builder.WriteString(runXMLForParagraphStyle(text, style, style.Bold))
	}
	builder.WriteString(`</w:p>`)
	return builder.String()
}

func runPropertiesForParagraphStyle(text string, style paragraphStyle) string {
	if style.AsciiFont != "" || style.EastAsiaFont != "" {
		asciiFont := style.AsciiFont
		if asciiFont == "" {
			asciiFont = "Times New Roman"
		}
		eastAsiaFont := style.EastAsiaFont
		if eastAsiaFont == "" {
			eastAsiaFont = "宋体"
		}
		return runPropertiesWithFonts(style.Size, style.Bold, asciiFont, eastAsiaFont)
	}
	return runPropertiesForText(text, style.Size, style.Bold)
}

func runXMLForParagraphStyle(text string, style paragraphStyle, bold bool) string {
	if style.AsciiFont != "" || style.EastAsiaFont != "" {
		asciiFont := style.AsciiFont
		if asciiFont == "" {
			asciiFont = "Times New Roman"
		}
		eastAsiaFont := style.EastAsiaFont
		if eastAsiaFont == "" {
			eastAsiaFont = "宋体"
		}
		return runXMLWithFonts(text, style.Size, bold, asciiFont, eastAsiaFont)
	}
	return runXML(text, style.Size, bold)
}

func spacingXML(style paragraphStyle) string {
	var builder strings.Builder
	builder.WriteString(`<w:spacing`)
	if style.BeforeLines > 0 {
		builder.WriteString(fmt.Sprintf(` w:beforeLines="%d"`, style.BeforeLines))
	}
	if style.Before > 0 {
		builder.WriteString(fmt.Sprintf(` w:before="%d"`, style.Before))
	}
	if style.AfterLines > 0 {
		builder.WriteString(fmt.Sprintf(` w:afterLines="%d"`, style.AfterLines))
	}
	if style.After > 0 {
		builder.WriteString(fmt.Sprintf(` w:after="%d"`, style.After))
	}
	builder.WriteString(fmt.Sprintf(` w:line="%d" w:lineRule="auto"/>`, style.Line))
	return builder.String()
}

func runXML(text string, size int, bold bool) string {
	return runXMLWithFonts(text, size, bold, "", "")
}

func runXMLPreservingText(text string, size int, bold bool) string {
	var builder strings.Builder
	preserveSpace := text != strings.TrimSpace(text)
	builder.WriteString(`<w:r><w:rPr>`)
	builder.WriteString(runPropertiesForText(text, size, bold))
	if preserveSpace {
		builder.WriteString(`</w:rPr><w:t xml:space="preserve">`)
	} else {
		builder.WriteString(`</w:rPr><w:t>`)
	}
	builder.WriteString(html.EscapeString(text))
	builder.WriteString(`</w:t></w:r>`)
	return builder.String()
}

func runXMLWithFonts(text string, size int, bold bool, asciiFont string, eastAsiaFont string) string {
	var builder strings.Builder
	builder.WriteString(`<w:r><w:rPr>`)
	if asciiFont != "" || eastAsiaFont != "" {
		if asciiFont == "" {
			asciiFont = "Times New Roman"
		}
		if eastAsiaFont == "" {
			eastAsiaFont = "SimSun"
		}
		builder.WriteString(runPropertiesWithFonts(size, bold, asciiFont, eastAsiaFont))
	} else {
		builder.WriteString(runPropertiesForText(text, size, bold))
	}
	builder.WriteString(`</w:rPr><w:t>`)
	builder.WriteString(html.EscapeString(strings.TrimSpace(text)))
	builder.WriteString(`</w:t></w:r>`)
	return builder.String()
}

func runProperties(size int, bold bool) string {
	return runPropertiesWithFonts(size, bold, "宋体", "宋体")
}

func runPropertiesForText(text string, size int, bold bool) string {
	if isMostlyASCII(text) {
		return runPropertiesWithFonts(size, bold, "Times New Roman", "宋体")
	}
	return runPropertiesWithFonts(size, bold, "宋体", "宋体")
}

func runPropertiesWithFonts(size int, bold bool, asciiFont string, eastAsiaFont string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf(`<w:rFonts w:ascii="%s" w:eastAsia="%s" w:hAnsi="%s"/>`, asciiFont, eastAsiaFont, asciiFont))
	if bold {
		builder.WriteString(`<w:b/><w:bCs/>`)
	}
	builder.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, size, size))
	return builder.String()
}

func isMostlyASCII(text string) bool {
	letters := 0
	ascii := 0
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' {
			continue
		}
		letters++
		if r < 128 {
			ascii++
		}
	}
	return letters > 0 && ascii*2 >= letters
}

func headingLevel(text string) int {
	prefix := strings.Fields(text)
	if len(prefix) == 0 {
		return 0
	}
	return strings.Count(prefix[0], ".") + 1
}

func applyReplacements(content string, replacements replacementSet) (string, bool) {
	original := content
	for token, paragraphXML := range replacements.paragraph {
		content = replaceParagraphContainingToken(content, token, paragraphXML)
	}
	content = applyInlineReplacements(content, replacements.inline)
	return content, content != original
}

func replaceParagraphContainingToken(content string, token string, replacement string) string {
	if !strings.Contains(content, token) {
		return content
	}
	return paragraphPattern.ReplaceAllStringFunc(content, func(paragraph string) string {
		if strings.Contains(paragraph, token) {
			return replacement
		}
		return paragraph
	})
}

func injectParagraphsBeforeFinalSection(content string, paragraphs string) string {
	paragraphs = strings.TrimSpace(paragraphs)
	if paragraphs == "" {
		return content
	}
	matches := finalSectPrPattern.FindAllStringIndex(content, -1)
	if len(matches) > 0 {
		last := matches[len(matches)-1]
		return content[:last[0]] + paragraphs + content[last[0]:]
	}
	endBody := strings.LastIndex(content, "</w:body>")
	if endBody >= 0 {
		return content[:endBody] + paragraphs + content[endBody:]
	}
	return content + paragraphs
}

func injectParagraphsAfterHeading(content string, paragraphs string, headings []string) string {
	paragraphs = strings.TrimSpace(paragraphs)
	if paragraphs == "" {
		return content
	}
	for _, paragraph := range paragraphPattern.FindAllString(content, -1) {
		text := strings.TrimSpace(xmlText(paragraph))
		for _, heading := range headings {
			if strings.EqualFold(text, heading) || strings.Contains(text, heading) {
				return strings.Replace(content, paragraph, paragraph+paragraphs, 1)
			}
		}
	}
	return injectParagraphsBeforeFinalSection(content, paragraphs)
}

func fillCoverTableFields(content string, fields map[string]string) string {
	if len(fields) == 0 {
		return content
	}
	fields = coverFieldsWithDefaults(fields)
	lastLabel := ""
	return tableRowPattern.ReplaceAllStringFunc(content, func(row string) string {
		cells := tableCellPattern.FindAllString(row, -1)
		if len(cells) < 2 {
			return row
		}
		label := strings.TrimSpace(xmlText(cells[0]))
		value := strings.TrimSpace(fields[label])
		if label == "" && lastLabel == "题目" {
			value = strings.TrimSpace(fields["题目续行"])
		}
		if value == "" || (label == "" && lastLabel != "题目") {
			if label != "" {
				lastLabel = label
			}
			return row
		}
		updated := replaceCellText(cells[1], value)
		if label != "" {
			lastLabel = label
		} else {
			lastLabel = ""
		}
		return strings.Replace(row, cells[1], updated, 1)
	})
}

var tableRowPattern = regexp.MustCompile(`(?s)<w:tr(?:\s[^>]*)?>.*?</w:tr>`)
var tableCellPattern = regexp.MustCompile(`(?s)<w:tc(?:\s[^>]*)?>.*?</w:tc>`)
var paragraphPropertyPattern = regexp.MustCompile(`(?s)<w:pPr(?:\s[^>]*)?>.*?</w:pPr>`)
var runPropertyPattern = regexp.MustCompile(`(?s)<w:rPr(?:\s[^>]*)?>.*?</w:rPr>`)
var pictPattern = regexp.MustCompile(`(?s)<w:pict>.*?</w:pict>`)
var textElementPattern = regexp.MustCompile(`(?s)<w:t(?:\s[^>]*)?>.*?</w:t>`)
var tagPattern = regexp.MustCompile(`(?s)<[^>]+>`)

func xmlText(fragment string) string {
	texts := textElementPattern.FindAllString(fragment, -1)
	var builder strings.Builder
	for _, text := range texts {
		builder.WriteString(tagPattern.ReplaceAllString(text, ""))
	}
	return html.UnescapeString(builder.String())
}

func replaceCellText(cell string, value string) string {
	firstParagraph := paragraphPattern.FindString(cell)
	if firstParagraph != "" {
		replacement := replaceParagraphTextPreservingStyle(firstParagraph, value)
		return strings.Replace(cell, firstParagraph, replacement, 1)
	}
	end := strings.LastIndex(cell, "</w:tc>")
	if end >= 0 {
		replacement := `<w:p><w:r><w:t>` + html.EscapeString(value) + `</w:t></w:r></w:p>`
		return cell[:end] + replacement + cell[end:]
	}
	return cell
}

func replaceParagraphTextPreservingStyle(paragraph string, value string) string {
	start := regexp.MustCompile(`<w:p(?:\s[^>]*)?>`).FindString(paragraph)
	if start == "" {
		start = `<w:p>`
	}
	pPr := paragraphPropertyPattern.FindString(paragraph)
	rPr := runPropertyPattern.FindString(paragraph)
	return start + pPr + `<w:r>` + rPr + `<w:t>` + html.EscapeString(value) + `</w:t></w:r></w:p>`
}

type cqrwstCoverPart struct {
	prefixEnd   int
	suffixStart int
	cover       string
}

func extractCQRWSTCoverPart(content string) (cqrwstCoverPart, bool) {
	matches := finalSectPrPattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return cqrwstCoverPart{}, false
	}
	first := matches[0]
	bodyStart := strings.Index(content, "<w:body>")
	if bodyStart < 0 || bodyStart+len("<w:body>") > first[0] {
		return cqrwstCoverPart{}, false
	}
	suffixStart := first[1]
	for _, paragraph := range paragraphPattern.FindAllStringIndex(content, -1) {
		if paragraph[0] <= first[0] && first[0] < paragraph[1] {
			suffixStart = paragraph[1]
			break
		}
	}
	prefixEnd := bodyStart + len("<w:body>")
	return cqrwstCoverPart{
		prefixEnd:   prefixEnd,
		suffixStart: suffixStart,
		cover:       content[prefixEnd:suffixStart],
	}, true
}

func fillCQRWSTTemplateCover(content string, fields map[string]string) string {
	content = cleanCQRWSTCoverInstructionArtifacts(content)
	content = compactCQRWSTCoverBlankParagraphs(content)
	content = fillCoverTableFields(content, fields)
	return fillCoverDate(content, fields)
}

func cleanCQRWSTCoverInstructionArtifacts(content string) string {
	content = pictPattern.ReplaceAllStringFunc(content, func(pict string) string {
		text := xmlText(pict)
		if strings.Contains(text, "要求") || strings.Contains(text, "封面格式不要调整") || strings.Contains(text, "选题题目") {
			return ""
		}
		return pict
	})
	return paragraphPattern.ReplaceAllStringFunc(content, func(paragraph string) string {
		text := strings.TrimSpace(xmlText(paragraph))
		if strings.Contains(text, "封面格式不要调整") || strings.Contains(text, "选题题目一般不超过") {
			return ""
		}
		return paragraph
	})
}

func compactCQRWSTCoverBlankParagraphs(content string) string {
	blankRun := 0
	return paragraphPattern.ReplaceAllStringFunc(content, func(paragraph string) string {
		if !isPlainBlankCoverParagraph(paragraph) {
			blankRun = 0
			return paragraph
		}
		blankRun++
		if blankRun > 1 {
			return ""
		}
		return paragraph
	})
}

func isPlainBlankCoverParagraph(paragraph string) bool {
	if strings.TrimSpace(xmlText(paragraph)) != "" {
		return false
	}
	for _, marker := range []string{"<w:pict", "<w:drawing", "<v:shape", "<w:sectPr", "<w:br"} {
		if strings.Contains(paragraph, marker) {
			return false
		}
	}
	return true
}

func coverFieldsWithDefaults(fields map[string]string) map[string]string {
	next := make(map[string]string, len(fields)+1)
	for key, value := range fields {
		next[key] = value
	}
	if strings.TrimSpace(firstNonEmpty(next["完成日期"], next["日期"])) == "" {
		next["完成日期"] = defaultCoverDate()
	}
	return next
}

func defaultCoverDate() string {
	return time.Now().Format("2006年1月")
}

func fillCoverDate(content string, fields map[string]string) string {
	date := strings.TrimSpace(firstNonEmpty(fields["完成日期"], fields["日期"], defaultCoverDate()))
	if date == "" || strings.Contains(xmlText(content), date) {
		return content
	}
	paragraphs := paragraphPattern.FindAllString(content, -1)
	for i := len(paragraphs) - 1; i >= 0; i-- {
		paragraph := paragraphs[i]
		text := strings.TrimSpace(xmlText(paragraph))
		if strings.Contains(text, "202X") || strings.Contains(text, "20XX") {
			updated := replaceParagraphTextPreservingStyle(paragraph, date)
			return strings.Replace(content, paragraph, updated, 1)
		}
	}
	for i := len(paragraphs) - 1; i >= 0; i-- {
		paragraph := paragraphs[i]
		if strings.TrimSpace(xmlText(paragraph)) == "" && strings.Contains(paragraph, `<w:jc w:val="center"`) {
			updated := replaceParagraphTextPreservingStyle(paragraph, date)
			return strings.Replace(content, paragraph, updated, 1)
		}
	}
	return content + centerParagraphWithStyle(date, paragraphStyle{Size: 32, Bold: true, Line: 360})
}

func rebuildCQRWSTCoverPage(content string, fields map[string]string) string {
	if len(fields) == 0 || !strings.Contains(content, "本科毕业论文") {
		return ""
	}
	cover, ok := extractCQRWSTCoverPart(content)
	if !ok {
		return ""
	}
	return content[:cover.prefixEnd] + fillCQRWSTTemplateCover(cover.cover, fields) + content[cover.suffixStart:]
}

func rebuildCQRWSTDocumentBody(content string, fields map[string]string, bodyParagraphs string, references string, thanks string) string {
	if len(fields) == 0 || strings.TrimSpace(bodyParagraphs) == "" || !strings.Contains(content, "本科毕业论文") {
		return ""
	}
	matches := finalSectPrPattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return ""
	}
	bodyStart := strings.Index(content, "<w:body>")
	bodyEnd := strings.LastIndex(content, "</w:body>")
	if bodyStart < 0 || bodyEnd < 0 || bodyStart > bodyEnd {
		return ""
	}
	cover, ok := extractCQRWSTCoverPart(content)
	if !ok {
		return ""
	}

	finalSectPr := selectCQRWSTBodySectPr(content, matches)
	var builder strings.Builder
	builder.WriteString(content[:bodyStart+len("<w:body>")])
	builder.WriteString(fillCQRWSTTemplateCover(cover.cover, fields))
	builder.WriteString(renderCQRWSTFrontMatterTitle(fields))
	builder.WriteString(strings.TrimSpace(bodyParagraphs))
	if strings.TrimSpace(references) != "" {
		builder.WriteString(pageBreakParagraph())
		builder.WriteString(backMatterTitleParagraph("参考文献"))
		builder.WriteString(strings.TrimSpace(references))
	}
	if strings.TrimSpace(thanks) != "" {
		builder.WriteString(pageBreakParagraph())
		builder.WriteString(backMatterTitleParagraph("致      谢"))
		builder.WriteString(strings.TrimSpace(thanks))
	}
	builder.WriteString(finalSectPr)
	builder.WriteString(content[bodyEnd:])
	return builder.String()
}

func renderCQRWSTFrontMatterTitle(fields map[string]string) string {
	title := strings.TrimSpace(firstNonEmpty(fields["题目"], fields["Title"], fields["论文题目"]))
	subtitle := strings.TrimSpace(fields["题目续行"])
	if title == "" && subtitle == "" {
		return ""
	}
	return frontMatterTitleParagraph(joinTextFragments(title, subtitle))
}

func frontMatterTitleParagraph(text string) string {
	style := paragraphStyle{
		Size:        32,
		Bold:        true,
		Line:        360,
		Before:      312,
		BeforeLines: 100,
		After:       624,
		AfterLines:  200,
	}
	var builder strings.Builder
	builder.WriteString(`<w:p><w:pPr><w:snapToGrid w:val="0"/><w:jc w:val="center"/>`)
	builder.WriteString(spacingXML(style))
	builder.WriteString(`<w:rPr>`)
	builder.WriteString(runPropertiesWithFonts(style.Size, true, "黑体", "黑体"))
	builder.WriteString(`</w:rPr></w:pPr>`)
	builder.WriteString(runXMLWithFonts(text, style.Size, true, "黑体", "黑体"))
	builder.WriteString(`</w:p>`)
	return builder.String()
}

func selectCQRWSTBodySectPr(content string, matches [][]int) string {
	if len(matches) == 0 {
		return ""
	}
	headerReference := firstCQRWSTHeaderReference(content, matches)
	for i := len(matches) - 1; i >= 0; i-- {
		sectPr := content[matches[i][0]:matches[i][1]]
		if strings.Contains(sectPr, "<w:footerReference") && strings.Contains(sectPr, `<w:pgNumType w:start="1"`) {
			return ensureSectPrHeaderReference(sectPr, headerReference)
		}
	}
	for i := len(matches) - 1; i >= 0; i-- {
		sectPr := content[matches[i][0]:matches[i][1]]
		if strings.Contains(sectPr, "<w:footerReference") {
			return ensureSectPrHeaderReference(sectPr, headerReference)
		}
	}
	return ensureSectPrHeaderReference(content[matches[len(matches)-1][0]:matches[len(matches)-1][1]], headerReference)
}

func firstCQRWSTHeaderReference(content string, matches [][]int) string {
	for _, match := range matches {
		sectPr := content[match[0]:match[1]]
		if header := headerReferencePattern.FindString(sectPr); header != "" {
			return header
		}
	}
	return ""
}

func ensureSectPrHeaderReference(sectPr string, headerReference string) string {
	if strings.TrimSpace(headerReference) == "" || strings.Contains(sectPr, "<w:headerReference") {
		return sectPr
	}
	start := strings.Index(sectPr, ">")
	if start < 0 {
		return sectPr
	}
	return sectPr[:start+1] + headerReference + sectPr[start+1:]
}

func renderCQRWSTCoverPage(fields map[string]string) string {
	title := firstNonEmpty(fields["题目"], fields["Title"], fields["论文题目"])
	rows := []struct {
		label string
		value string
	}{
		{"学院", fields["学院"]},
		{"专业", fields["专业"]},
		{"班级", fields["班级"]},
		{"学号", fields["学号"]},
		{"姓名", firstNonEmpty(fields["姓名"], fields["学生姓名"])},
		{"指导教师", fields["指导教师"]},
		{"完成日期", firstNonEmpty(fields["完成日期"], fields["日期"])},
	}

	var builder strings.Builder
	builder.WriteString(`<w:p><w:pPr><w:spacing w:after="1200"/></w:pPr></w:p>`)
	builder.WriteString(centerParagraph("本科毕业论文/设计", 72, true))
	builder.WriteString(`<w:p><w:pPr><w:spacing w:after="360"/></w:pPr></w:p>`)
	builder.WriteString(centerParagraph(title, 44, true))
	builder.WriteString(`<w:p><w:pPr><w:spacing w:after="720"/></w:pPr></w:p>`)
	for _, row := range rows {
		if strings.TrimSpace(row.value) == "" {
			continue
		}
		builder.WriteString(centerParagraph(row.label+"："+row.value, 36, true))
	}
	builder.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
	return builder.String()
}

func centerParagraph(text string, size int, bold bool) string {
	return centerParagraphWithStyle(text, paragraphStyle{Size: size, Bold: bold, Line: 360})
}

func backMatterTitleParagraph(text string) string {
	return centeredParagraphWithFonts(text, paragraphStyle{
		Size:       30,
		Bold:       true,
		Line:       360,
		After:      624,
		AfterLines: 200,
	}, "Times New Roman", "黑体")
}

func centerParagraphWithStyle(text string, style paragraphStyle) string {
	return centeredParagraphWithFonts(text, style, "宋体", "宋体")
}

func centeredParagraphWithFonts(text string, style paragraphStyle, asciiFont string, eastAsiaFont string) string {
	if style.Size == 0 {
		style.Size = 24
	}
	if style.Line == 0 {
		style.Line = 360
	}
	var builder strings.Builder
	builder.WriteString(`<w:p><w:pPr><w:jc w:val="center"/>`)
	builder.WriteString(spacingXML(style))
	builder.WriteString(fmt.Sprintf(`<w:rPr><w:rFonts w:ascii="%s" w:eastAsia="%s" w:hAnsi="%s"/>`, asciiFont, eastAsiaFont, asciiFont))
	if style.Bold {
		builder.WriteString(`<w:b/><w:bCs/>`)
	}
	builder.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, style.Size, style.Size))
	builder.WriteString(fmt.Sprintf(`</w:rPr></w:pPr><w:r><w:rPr><w:rFonts w:ascii="%s" w:eastAsia="%s" w:hAnsi="%s"/>`, asciiFont, eastAsiaFont, asciiFont))
	if style.Bold {
		builder.WriteString(`<w:b/><w:bCs/>`)
	}
	builder.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, style.Size, style.Size))
	builder.WriteString(`</w:rPr><w:t>`)
	builder.WriteString(html.EscapeString(strings.TrimSpace(text)))
	builder.WriteString(`</w:t></w:r></w:p>`)
	return builder.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeRendererIncompatibleXML(content string) string {
	content = strings.ReplaceAll(content, `w:val="start"`, `w:val="left"`)
	content = strings.ReplaceAll(content, `w:val='start'`, `w:val='left'`)
	if cover, ok := extractCQRWSTCoverPart(content); ok {
		return content[:cover.suffixStart] + normalizeRendererIncompatibleTables(content[cover.suffixStart:])
	}
	return normalizeRendererIncompatibleTables(content)
}

func normalizeRendererIncompatibleTables(content string) string {
	content = floatingTablePropertyPattern.ReplaceAllString(content, "")
	content = tableOverlapPropertyPattern.ReplaceAllString(content, "")
	content = normalizeTableCellParagraphs(content)
	content = normalizeAutoTableWidths(content)
	return content
}

func normalizeTableCellParagraphs(content string) string {
	return tablePattern.ReplaceAllStringFunc(content, func(table string) string {
		table = tableParagraphIndentPattern.ReplaceAllString(table, "")
		table = tableJustifyBothPattern.ReplaceAllString(table, `<w:jc w:val="center"/>`)
		table = tableRowHeightPattern.ReplaceAllString(table, "")
		table = tableFontSize24Pattern.ReplaceAllStringFunc(table, func(size string) string {
			if strings.Contains(size, "Cs") {
				return `<w:szCs w:val="18"/>`
			}
			return `<w:sz w:val="18"/>`
		})
		return table
	})
}

func normalizeAutoTableWidths(content string) string {
	return tablePattern.ReplaceAllStringFunc(content, func(table string) string {
		if !autoTableWidthPattern.MatchString(table) {
			return table
		}
		width := tableGridWidth(table)
		if width <= 0 {
			return table
		}
		table = autoTableWidthPattern.ReplaceAllString(table, fmt.Sprintf(`<w:tblW w:w="%d" w:type="dxa"/>`, width))
		if !strings.Contains(table, "<w:jc ") {
			table = strings.Replace(table, "<w:tblPr>", `<w:tblPr><w:jc w:val="center"/>`, 1)
		}
		return table
	})
}

func tableGridWidth(table string) int {
	matches := tableGridColWidthPattern.FindAllStringSubmatch(table, -1)
	total := 0
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		var width int
		if _, err := fmt.Sscanf(match[1], "%d", &width); err == nil {
			total += width
		}
	}
	return total
}

func normalizePackageXML(pkg *ooxmlpkg.DocxPackage) {
	if pkg == nil {
		return
	}
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		content, ok := pkg.Get(name)
		if !ok {
			continue
		}
		pkg.Set(name, []byte(normalizeRendererIncompatibleXML(string(content))))
	}
}

func normalizeCQRWSTMainFooter(pkg *ooxmlpkg.DocxPackage) {
	if pkg == nil {
		return
	}
	document, ok := pkg.Get(defaultPatchTarget)
	if !ok {
		return
	}
	footerID := mainBodyFooterReferenceID(string(document))
	if footerID == "" {
		return
	}
	footerTarget := relationshipTarget(pkg, footerID)
	if footerTarget == "" {
		return
	}
	pkg.Set(footerTarget, []byte(renderCQRWSTMainFooterXML()))
}

func normalizeCQRWSTMainHeader(pkg *ooxmlpkg.DocxPackage, fields map[string]string) {
	if pkg == nil || len(fields) == 0 {
		return
	}
	document, ok := pkg.Get(defaultPatchTarget)
	if !ok {
		return
	}
	headerID := mainBodyHeaderReferenceID(string(document))
	if headerID == "" {
		return
	}
	headerTarget := relationshipTarget(pkg, headerID)
	if headerTarget == "" {
		return
	}
	pkg.Set(headerTarget, []byte(renderCQRWSTMainHeaderXML(fields)))
}

func mainBodyHeaderReferenceID(documentXML string) string {
	matches := finalSectPrPattern.FindAllString(documentXML, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		sectPr := matches[i]
		if !strings.Contains(sectPr, `<w:pgNumType w:start="1"`) {
			continue
		}
		if match := headerReferenceIDPattern.FindStringSubmatch(sectPr); len(match) == 2 {
			return match[1]
		}
	}
	for i := 0; i < len(matches); i++ {
		if match := headerReferenceIDPattern.FindStringSubmatch(matches[i]); len(match) == 2 {
			return match[1]
		}
	}
	return ""
}

func mainBodyFooterReferenceID(documentXML string) string {
	matches := finalSectPrPattern.FindAllString(documentXML, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		sectPr := matches[i]
		if !strings.Contains(sectPr, `<w:pgNumType w:start="1"`) {
			continue
		}
		if match := footerReferenceIDPattern.FindStringSubmatch(sectPr); len(match) == 2 {
			return match[1]
		}
	}
	for i := len(matches) - 1; i >= 0; i-- {
		if match := footerReferenceIDPattern.FindStringSubmatch(matches[i]); len(match) == 2 {
			return match[1]
		}
	}
	return ""
}

func relationshipTarget(pkg *ooxmlpkg.DocxPackage, relationshipID string) string {
	rels, ok := pkg.Get("word/_rels/document.xml.rels")
	if !ok || relationshipID == "" {
		return ""
	}
	pattern := regexp.MustCompile(`<Relationship\b[^>]*\bId="` + regexp.QuoteMeta(relationshipID) + `"[^>]*\bTarget="([^"]+)"[^>]*/>`)
	match := pattern.FindStringSubmatch(string(rels))
	if len(match) != 2 {
		return ""
	}
	target := strings.TrimSpace(match[1])
	if target == "" || strings.Contains(target, "://") {
		return ""
	}
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	return "word/" + strings.TrimPrefix(target, "word/")
}

func renderCQRWSTMainHeaderXML(fields map[string]string) string {
	year := coverYear(fields)
	major := strings.TrimSpace(firstNonEmpty(fields["专业"], fields["涓撲笟"], "XXX"))
	text := "重庆人文科技学院" + year + "届" + major + "专业本科毕业论文/设计"
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:p><w:pPr><w:pBdr><w:bottom w:val="double" w:sz="4" w:space="1" w:color="auto"/></w:pBdr><w:jc w:val="center"/><w:rPr>` +
		`<w:rFonts w:ascii="宋体" w:eastAsia="宋体" w:hAnsi="宋体"/><w:sz w:val="18"/><w:szCs w:val="18"/>` +
		`</w:rPr></w:pPr><w:r><w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体" w:hAnsi="宋体"/><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr><w:t>` +
		html.EscapeString(text) +
		`</w:t></w:r></w:p></w:hdr>`
}

func coverYear(fields map[string]string) string {
	date := strings.TrimSpace(firstNonEmpty(fields["完成日期"], fields["日期"], fields["瀹屾垚鏃ユ湡"], fields["鏃ユ湡"], defaultCoverDate()))
	for _, r := range date {
		if r < '0' || r > '9' {
			date = strings.ReplaceAll(date, string(r), " ")
		}
	}
	parts := strings.Fields(date)
	if len(parts) > 0 && len([]rune(parts[0])) >= 4 {
		return string([]rune(parts[0])[:4])
	}
	return time.Now().Format("2006")
}

func renderCQRWSTMainFooterXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:p><w:pPr><w:jc w:val="center"/><w:rPr>` +
		`<w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hAnsi="Times New Roman"/><w:sz w:val="21"/><w:szCs w:val="21"/>` +
		`</w:rPr></w:pPr>` +
		`<w:r><w:rPr><w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hAnsi="Times New Roman"/><w:sz w:val="21"/><w:szCs w:val="21"/></w:rPr><w:t>第 </w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE \* MERGEFORMAT </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>1</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>` +
		`<w:r><w:rPr><w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hAnsi="Times New Roman"/><w:sz w:val="21"/><w:szCs w:val="21"/></w:rPr><w:t> 页 共 </w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> NUMPAGES \* MERGEFORMAT </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>1</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>` +
		`<w:r><w:rPr><w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hAnsi="Times New Roman"/><w:sz w:val="21"/><w:szCs w:val="21"/></w:rPr><w:t> 页</w:t></w:r>` +
		`</w:p></w:ftr>`
}

func validateXML(content string) error {
	decoder := xml.NewDecoder(bytes.NewBufferString(content))
	for {
		if _, err := decoder.Token(); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func applyInlineReplacements(content string, replacements map[string]string) string {
	var builder strings.Builder
	for len(content) > 0 {
		start := strings.Index(content, "{{")
		if start == -1 {
			builder.WriteString(content)
			break
		}

		builder.WriteString(content[:start])
		remaining := content[start:]
		end := strings.Index(remaining, "}}")
		if end == -1 {
			builder.WriteString(remaining)
			break
		}

		end += len("}}")
		token := remaining[:end]
		if replacement, ok := replacements[token]; ok {
			builder.WriteString(replacement)
		} else {
			builder.WriteString(token)
		}
		content = remaining[end:]
	}
	return builder.String()
}
