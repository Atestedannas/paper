package ooxmlpatch

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

var (
	paragraphPropertiesElement = regexp.MustCompile(`(?s)<w:pPr\b[^>]*>.*?</w:pPr>|<w:pPr\b[^>]*/>`)
	runPropertiesElement       = regexp.MustCompile(`(?s)<w:rPr\b[^>]*>.*?</w:rPr>|<w:rPr\b[^>]*/>`)
	sectionPropertiesElement   = regexp.MustCompile(`(?s)<w:sectPr\b[^>]*/>|<w:sectPr\b[^>]*>.*?</w:sectPr>`)
	tablePropertiesElement     = regexp.MustCompile(`(?s)<w:tblPr\b[^>]*>.*?</w:tblPr>|<w:tblPr\b[^>]*/>`)
	tableBordersElement        = regexp.MustCompile(`(?s)<w:tblBorders\b[^>]*>.*?</w:tblBorders>|<w:tblBorders\b[^>]*/>`)

	jcElement              = regexp.MustCompile(`<w:jc\b[^>]*/>`)
	spacingElement         = regexp.MustCompile(`<w:spacing\b[^>]*/>`)
	indentElement          = regexp.MustCompile(`<w:ind\b[^>]*/>`)
	pageBreakBeforeElement = regexp.MustCompile(`<w:pageBreakBefore\b[^>]*/>`)
	keepNextElement        = regexp.MustCompile(`<w:keepNext\b[^>]*/>`)
	keepLinesElement       = regexp.MustCompile(`<w:keepLines\b[^>]*/>`)
	widowControlElement    = regexp.MustCompile(`<w:widowControl\b[^>]*/>`)
	snapToGridElement      = regexp.MustCompile(`<w:snapToGrid\b[^>]*/>`)
	adjustRightIndElement  = regexp.MustCompile(`<w:adjustRightInd\b[^>]*/>`)
	paragraphStyleElement  = regexp.MustCompile(`<w:pStyle\b[^>]*/>`)
	outlineLevelElement    = regexp.MustCompile(`<w:outlineLvl\b[^>]*/>`)

	runFontsElement         = regexp.MustCompile(`<w:rFonts\b[^>]*/>`)
	runSizeElement          = regexp.MustCompile(`<w:sz\b[^>]*/>`)
	runComplexSizeElement   = regexp.MustCompile(`<w:szCs\b[^>]*/>`)
	runBoldElement          = regexp.MustCompile(`<w:b\b[^>]*/>`)
	runComplexBoldElement   = regexp.MustCompile(`<w:bCs\b[^>]*/>`)
	runItalicElement        = regexp.MustCompile(`<w:i\b[^>]*/>`)
	runComplexItalicElement = regexp.MustCompile(`<w:iCs\b[^>]*/>`)
	runVertAlignElement     = regexp.MustCompile(`<w:vertAlign\b[^>]*/>`)
	runColorElement         = regexp.MustCompile(`<w:color\b[^>]*/>`)

	pageSizeElement              = regexp.MustCompile(`<w:pgSz\b[^>]*/>`)
	pageMarginElement            = regexp.MustCompile(`<w:pgMar\b[^>]*/>`)
	pageNumberTypeElement        = regexp.MustCompile(`<w:pgNumType\b[^>]*/>`)
	headerFooterReferenceElement = regexp.MustCompile(`<w:(?:headerReference|footerReference)\b[^>]*/>`)
	evenAndOddHeadersElement     = regexp.MustCompile(`<w:evenAndOddHeaders\b[^>]*/>`)
	updateFieldsElement          = regexp.MustCompile(`<w:updateFields\b[^>]*/>`)
)

type SectionPropertiesSpec struct {
	PageWidthTwips     int
	PageHeightTwips    int
	PageOrientation    string
	MarginTopTwips     int
	MarginRightTwips   int
	MarginBottomTwips  int
	MarginLeftTwips    int
	GutterTwips        int
	HeaderMarginTwips  int
	FooterMarginTwips  int
	PageNumberFormat   string
	PageNumberStart    int
	RemoveHeaderFooter bool
}

type SettingsPropertiesSpec struct {
	EvenAndOddHeaders  bool
	UpdateFieldsOnOpen bool
}

type ParagraphPropertiesSpec struct {
	StyleID            string
	OutlineLevel       int
	OutlineLevelSet    bool
	Alignment          string
	LineTwips          int
	LineRule           string
	BeforeTwips        int
	AfterTwips         int
	BeforeLines        int
	AfterLines         int
	FirstLineChars     int
	FirstLineTwips     int
	BeforeLinesSet     bool
	AfterLinesSet      bool
	FirstLineCharsSet  bool
	PageBreakBefore    bool
	KeepNext           bool
	KeepNextSet        bool
	KeepLines          bool
	KeepLinesSet       bool
	WidowControl       bool
	WidowControlSet    bool
	SnapToGridOff      bool
	AdjustRightIndZero bool
	RunPropertiesInPPr bool
	RemoveOutlineLevel bool

	EastAsiaFont       string
	AsciiFont          string
	HAnsiFont          string
	ComplexFont        string
	FontHint           string
	FontSizeHalfPoints int
	ComplexSizeHalfPts int
	Bold               bool
	BoldSet            bool
	Italic             bool
	ItalicSet          bool
	Color              string
}

type RunPropertiesSpec struct {
	EastAsiaFont       string
	AsciiFont          string
	HAnsiFont          string
	ComplexFont        string
	FontHint           string
	FontSizeHalfPoints int
	ComplexSizeHalfPts int
	Bold               bool
	BoldSet            bool
	Italic             bool
	ItalicSet          bool
	Color              string
	VerticalAlign      string
}

type TableBordersSpec struct {
	TopSize    int
	HeaderSize int
	BottomSize int
	Color      string
}

func ApplySectionProperties(documentXML string, spec SectionPropertiesSpec) (string, bool) {
	indexes := sectionPropertiesElement.FindAllStringIndex(documentXML, -1)
	if len(indexes) > 0 {
		return ApplySectionPropertiesAt(documentXML, len(indexes)-1, spec)
	}
	sectPr := lastElement(documentXML, sectionPropertiesElement)
	if sectPr == "" {
		sectPr = `<w:sectPr/>`
	}
	updatedSectPr := replaceElementBody(sectPr, updateSectionPropertiesBody(elementBody(sectPr), spec), "w:sectPr")
	if sectPr == updatedSectPr && strings.Contains(documentXML, sectPr) {
		return documentXML, false
	}
	if sectionPropertiesElement.MatchString(documentXML) {
		indexes := sectionPropertiesElement.FindAllStringIndex(documentXML, -1)
		last := indexes[len(indexes)-1]
		return documentXML[:last[0]] + updatedSectPr + documentXML[last[1]:], true
	}
	if idx := strings.LastIndex(documentXML, "</w:body>"); idx >= 0 {
		return documentXML[:idx] + updatedSectPr + documentXML[idx:], true
	}
	return documentXML + updatedSectPr, true
}

// ApplySectionPropertiesAll applies the template page setup to every section
// in the document.  ApplySectionProperties intentionally targets only the
// final sectPr (the OOXML body-level section), which is insufficient when a
// student document contains cover/TOC/body section breaks: earlier sections
// would retain the student's margins.
func ApplySectionPropertiesAll(documentXML string, spec SectionPropertiesSpec) (string, bool) {
	indexes := sectionPropertiesElement.FindAllStringIndex(documentXML, -1)
	if len(indexes) == 0 {
		return ApplySectionProperties(documentXML, spec)
	}
	updated := documentXML
	changed := false
	// Replace from the end so byte offsets remain valid while preserving every
	// section's header/footer references and other unrelated properties.
	for i := len(indexes) - 1; i >= 0; i-- {
		currentIndexes := sectionPropertiesElement.FindAllStringIndex(updated, -1)
		if i >= len(currentIndexes) {
			continue
		}
		idx := currentIndexes[i]
		sectPr := updated[idx[0]:idx[1]]
		next := replaceElementBody(sectPr, updateSectionPropertiesBody(elementBody(sectPr), spec), "w:sectPr")
		if ordered, _, err := ooxmlpkg.RepairPropertyBlock([]byte(next), "sectPr"); err == nil {
			next = string(ordered)
		}
		if next == sectPr {
			continue
		}
		updated = updated[:idx[0]] + next + updated[idx[1]:]
		changed = true
	}
	return updated, changed
}

func ApplySectionPropertiesAt(documentXML string, sectionIndex int, spec SectionPropertiesSpec) (string, bool) {
	indexes := sectionPropertiesElement.FindAllStringIndex(documentXML, -1)
	if sectionIndex < 0 || sectionIndex >= len(indexes) {
		return documentXML, false
	}
	index := indexes[sectionIndex]
	sectPr := documentXML[index[0]:index[1]]
	updated := replaceElementBody(sectPr, updateSectionPropertiesBody(elementBody(sectPr), spec), "w:sectPr")
	if ordered, _, err := ooxmlpkg.RepairPropertyBlock([]byte(updated), "sectPr"); err == nil {
		updated = string(ordered)
	}
	if updated == sectPr {
		return documentXML, false
	}
	return documentXML[:index[0]] + updated + documentXML[index[1]:], true
}

func ApplySettingsProperties(settingsXML string, spec SettingsPropertiesSpec) (string, bool) {
	updated := settingsXML
	if spec.EvenAndOddHeaders {
		updated = evenAndOddHeadersElement.ReplaceAllString(updated, "")
		updated = insertEvenAndOddHeadersInSchemaOrder(updated)
	}
	if spec.UpdateFieldsOnOpen {
		updated = updateFieldsElement.ReplaceAllString(updated, "")
		updated = insertUpdateFieldsInSchemaOrder(updated)
	}
	return updated, updated != settingsXML
}

func insertEvenAndOddHeadersInSchemaOrder(settingsXML string) string {
	element := `<w:evenAndOddHeaders/>`
	for _, later := range []string{
		"<w:bookFoldRevPrinting", "<w:bookFoldPrinting", "<w:bookFoldPrintingSheets",
		"<w:drawingGridHorizontalSpacing", "<w:drawingGridVerticalSpacing",
		"<w:displayHorizontalDrawingGridEvery", "<w:displayVerticalDrawingGridEvery",
		"<w:doNotUseMarginsForDrawingGridOrigin", "<w:drawingGridHorizontalOrigin",
		"<w:drawingGridVerticalOrigin", "<w:doNotShadeFormData", "<w:noPunctuationKerning",
		"<w:characterSpacingControl", "<w:printTwoOnOne", "<w:strictFirstAndLastChars",
		"<w:noLineBreaksAfter", "<w:noLineBreaksBefore", "<w:savePreviewPicture",
		"<w:doNotValidateAgainstSchema", "<w:saveInvalidXml", "<w:ignoreMixedContent",
		"<w:alwaysShowPlaceholderText", "<w:doNotDemarcateInvalidXml", "<w:saveXmlDataOnly",
		"<w:useXSLTWhenSaving", "<w:saveThroughXslt", "<w:showXMLTags", "<w:alwaysMergeEmptyNamespace",
		"<w:updateFields", "<w:hdrShapeDefaults", "<w:footnotePr", "<w:endnotePr",
		"<w:compat", "<w:docVars", "<w:rsids", "<m:mathPr",
	} {
		if index := strings.Index(settingsXML, later); index >= 0 {
			return settingsXML[:index] + element + settingsXML[index:]
		}
	}
	return insertBeforeClosingTag(settingsXML, "w:settings", element)
}

func insertUpdateFieldsInSchemaOrder(settingsXML string) string {
	element := `<w:updateFields w:val="true"/>`
	for _, later := range []string{
		"<w:hdrShapeDefaults", "<w:footnotePr", "<w:endnotePr", "<w:compat",
		"<w:docVars", "<w:rsids", "<m:mathPr", "<w:attachedSchema",
		"<w:themeFontLang", "<w:clrSchemeMapping",
	} {
		if index := strings.Index(settingsXML, later); index >= 0 {
			return settingsXML[:index] + element + settingsXML[index:]
		}
	}
	return insertBeforeClosingTag(settingsXML, "w:settings", element)
}

func ApplyParagraphProperties(paragraphXML string, spec ParagraphPropertiesSpec) (string, bool) {
	pPr := firstElement(paragraphXML, paragraphPropertiesElement)
	if pPr == "" {
		pPr = `<w:pPr/>`
	}
	updatedPPr := replaceElementBody(pPr, updateParagraphPropertiesBody(elementBody(pPr), spec), "w:pPr")
	if ordered, _, err := ooxmlpkg.RepairPropertyBlock([]byte(updatedPPr), "pPr"); err == nil {
		updatedPPr = string(ordered)
	}
	if pPr == updatedPPr && strings.Contains(paragraphXML, pPr) {
		return paragraphXML, false
	}
	if paragraphPropertiesElement.MatchString(paragraphXML) {
		return paragraphPropertiesElement.ReplaceAllString(paragraphXML, updatedPPr), true
	}
	if idx := strings.Index(paragraphXML, ">"); idx >= 0 {
		return paragraphXML[:idx+1] + updatedPPr + paragraphXML[idx+1:], true
	}
	return updatedPPr + paragraphXML, true
}

func ApplyRunProperties(runXML string, spec RunPropertiesSpec) (string, bool) {
	rPr := firstElement(runXML, runPropertiesElement)
	if rPr == "" {
		rPr = `<w:rPr/>`
	}
	updatedRPr := replaceElementBody(rPr, updateRunPropertiesBody(elementBody(rPr), spec), "w:rPr")
	if ordered, _, err := ooxmlpkg.RepairPropertyBlock([]byte(updatedRPr), "rPr"); err == nil {
		updatedRPr = string(ordered)
	}
	if rPr == updatedRPr && strings.Contains(runXML, rPr) {
		return runXML, false
	}
	if runPropertiesElement.MatchString(runXML) {
		return runPropertiesElement.ReplaceAllString(runXML, updatedRPr), true
	}
	if idx := strings.Index(runXML, ">"); idx >= 0 {
		return runXML[:idx+1] + updatedRPr + runXML[idx+1:], true
	}
	return updatedRPr + runXML, true
}

func ApplyThreeLineTableBorders(tableXML string, spec TableBordersSpec) (string, bool) {
	tblPr := firstElement(tableXML, tablePropertiesElement)
	if tblPr == "" {
		tblPr = `<w:tblPr/>`
	}
	body := tableBordersElement.ReplaceAllString(elementBody(tblPr), "")
	body += buildThreeLineBorders(spec)
	updatedTblPr := replaceElementBody(tblPr, body, "w:tblPr")
	if tblPr == updatedTblPr && strings.Contains(tableXML, tblPr) {
		return tableXML, false
	}
	if tablePropertiesElement.MatchString(tableXML) {
		return tablePropertiesElement.ReplaceAllString(tableXML, updatedTblPr), true
	}
	if idx := strings.Index(tableXML, ">"); idx >= 0 {
		return tableXML[:idx+1] + updatedTblPr + tableXML[idx+1:], true
	}
	return updatedTblPr + tableXML, true
}

func updateSectionPropertiesBody(body string, spec SectionPropertiesSpec) string {
	if spec.RemoveHeaderFooter {
		body = headerFooterReferenceElement.ReplaceAllString(body, "")
	}
	if spec.PageWidthTwips > 0 || spec.PageHeightTwips > 0 || spec.PageOrientation != "" {
		updates := make([]xmlAttributeUpdate, 0, 3)
		if spec.PageWidthTwips > 0 {
			updates = append(updates, xmlAttributeUpdate{"w:w", strconv.Itoa(spec.PageWidthTwips)})
		}
		if spec.PageHeightTwips > 0 {
			updates = append(updates, xmlAttributeUpdate{"w:h", strconv.Itoa(spec.PageHeightTwips)})
		}
		if spec.PageOrientation != "" {
			updates = append(updates, xmlAttributeUpdate{"w:orient", spec.PageOrientation})
		}
		body = upsertPropertyElement(body, pageSizeElement, "w:pgSz", updates, nil)
	}
	if sectionMarginsRequested(spec) {
		updates := make([]xmlAttributeUpdate, 0, 7)
		for _, item := range []struct {
			name  string
			value int
		}{
			{"w:top", spec.MarginTopTwips},
			{"w:right", spec.MarginRightTwips},
			{"w:bottom", spec.MarginBottomTwips},
			{"w:left", spec.MarginLeftTwips},
			{"w:gutter", spec.GutterTwips},
			{"w:header", spec.HeaderMarginTwips},
			{"w:footer", spec.FooterMarginTwips},
		} {
			if item.value > 0 {
				updates = append(updates, xmlAttributeUpdate{item.name, strconv.Itoa(item.value)})
			}
		}
		body = upsertPropertyElement(body, pageMarginElement, "w:pgMar", updates, nil)
	}
	if spec.PageNumberFormat != "" || spec.PageNumberStart > 0 {
		updates := make([]xmlAttributeUpdate, 0, 2)
		if spec.PageNumberFormat != "" {
			updates = append(updates, xmlAttributeUpdate{"w:fmt", spec.PageNumberFormat})
		}
		if spec.PageNumberStart > 0 {
			updates = append(updates, xmlAttributeUpdate{"w:start", strconv.Itoa(spec.PageNumberStart)})
		}
		body = upsertPropertyElement(body, pageNumberTypeElement, "w:pgNumType", updates, nil)
	}
	return body
}

func updateParagraphPropertiesBody(body string, spec ParagraphPropertiesSpec) string {
	if spec.StyleID != "" {
		body = upsertPropertyElement(body, paragraphStyleElement, "w:pStyle", []xmlAttributeUpdate{{"w:val", spec.StyleID}}, nil)
	}
	if spec.OutlineLevelSet {
		body = upsertPropertyElement(body, outlineLevelElement, "w:outlineLvl", []xmlAttributeUpdate{{"w:val", strconv.Itoa(spec.OutlineLevel)}}, nil)
	} else if spec.RemoveOutlineLevel {
		body = outlineLevelElement.ReplaceAllString(body, "")
	}
	if spec.Alignment != "" {
		body = upsertPropertyElement(body, jcElement, "w:jc", []xmlAttributeUpdate{{"w:val", spec.Alignment}}, nil)
	}
	if spec.BeforeTwips > 0 || spec.AfterTwips > 0 || spec.BeforeLines > 0 || spec.AfterLines > 0 || spec.BeforeLinesSet || spec.AfterLinesSet || spec.LineTwips > 0 || spec.LineRule != "" {
		updates := make([]xmlAttributeUpdate, 0, 6)
		remove := make([]string, 0, 4)
		if spec.BeforeTwips > 0 {
			updates = append(updates, xmlAttributeUpdate{"w:before", strconv.Itoa(spec.BeforeTwips)})
			if !spec.BeforeLinesSet {
				remove = append(remove, "w:beforeLines", "w:beforeAutospacing")
			}
		}
		if spec.AfterTwips > 0 {
			updates = append(updates, xmlAttributeUpdate{"w:after", strconv.Itoa(spec.AfterTwips)})
			if !spec.AfterLinesSet {
				remove = append(remove, "w:afterLines", "w:afterAutospacing")
			}
		}
		if spec.BeforeLinesSet {
			updates = append(updates, xmlAttributeUpdate{"w:beforeLines", strconv.Itoa(spec.BeforeLines)})
			if spec.BeforeTwips <= 0 {
				remove = append(remove, "w:before", "w:beforeAutospacing")
			}
		}
		if spec.AfterLinesSet {
			updates = append(updates, xmlAttributeUpdate{"w:afterLines", strconv.Itoa(spec.AfterLines)})
			if spec.AfterTwips <= 0 {
				remove = append(remove, "w:after", "w:afterAutospacing")
			}
		}
		if spec.LineTwips > 0 {
			updates = append(updates, xmlAttributeUpdate{"w:line", strconv.Itoa(spec.LineTwips)})
		}
		if spec.LineRule != "" {
			updates = append(updates, xmlAttributeUpdate{"w:lineRule", spec.LineRule})
		}
		body = upsertPropertyElement(body, spacingElement, "w:spacing", updates, remove)
	}
	if spec.FirstLineChars > 0 || spec.FirstLineCharsSet {
		updates := []xmlAttributeUpdate{{"w:firstLineChars", strconv.Itoa(spec.FirstLineChars)}}
		if spec.FirstLineTwips > 0 {
			updates = append(updates, xmlAttributeUpdate{"w:firstLine", strconv.Itoa(spec.FirstLineTwips)})
		}
		body = upsertPropertyElement(body, indentElement, "w:ind", updates, []string{"w:hanging", "w:hangingChars"})
	} else if spec.FirstLineTwips > 0 {
		body = upsertPropertyElement(body, indentElement, "w:ind", []xmlAttributeUpdate{{"w:firstLine", strconv.Itoa(spec.FirstLineTwips)}}, []string{"w:hanging", "w:hangingChars"})
	}
	if spec.PageBreakBefore {
		body = upsertOnOffProperty(body, pageBreakBeforeElement, "w:pageBreakBefore", true)
	}
	if spec.KeepNext || spec.KeepNextSet {
		body = upsertOnOffProperty(body, keepNextElement, "w:keepNext", spec.KeepNext)
	}
	if spec.KeepLines || spec.KeepLinesSet {
		body = upsertOnOffProperty(body, keepLinesElement, "w:keepLines", spec.KeepLines)
	}
	if spec.WidowControl || spec.WidowControlSet {
		body = upsertOnOffProperty(body, widowControlElement, "w:widowControl", spec.WidowControl)
	}
	if spec.SnapToGridOff {
		body = upsertPropertyElement(body, snapToGridElement, "w:snapToGrid", []xmlAttributeUpdate{{"w:val", "0"}}, nil)
	}
	if spec.AdjustRightIndZero {
		body = upsertPropertyElement(body, adjustRightIndElement, "w:adjustRightInd", []xmlAttributeUpdate{{"w:val", "0"}}, nil)
	}
	if spec.RunPropertiesInPPr {
		runSpec := RunPropertiesSpec{
			EastAsiaFont:       spec.EastAsiaFont,
			AsciiFont:          spec.AsciiFont,
			HAnsiFont:          spec.HAnsiFont,
			ComplexFont:        spec.ComplexFont,
			FontHint:           spec.FontHint,
			FontSizeHalfPoints: spec.FontSizeHalfPoints,
			ComplexSizeHalfPts: spec.ComplexSizeHalfPts,
			Bold:               spec.Bold,
			BoldSet:            spec.BoldSet,
			Italic:             spec.Italic,
			ItalicSet:          spec.ItalicSet,
			Color:              spec.Color,
		}
		if hasRunPropertiesRequest(runSpec) {
			body = updateNestedRunProperties(body, runSpec)
		}
	}
	return body
}

func updateRunPropertiesBody(body string, spec RunPropertiesSpec) string {
	if spec.EastAsiaFont != "" || spec.AsciiFont != "" || spec.HAnsiFont != "" || spec.ComplexFont != "" || spec.FontHint != "" {
		updates := make([]xmlAttributeUpdate, 0, 5)
		remove := make([]string, 0, 4)
		for _, font := range []struct {
			name       string
			value      string
			themeNames []string
		}{
			{"w:ascii", spec.AsciiFont, []string{"w:asciiTheme"}},
			{"w:hAnsi", spec.HAnsiFont, []string{"w:hAnsiTheme"}},
			{"w:eastAsia", spec.EastAsiaFont, []string{"w:eastAsiaTheme"}},
			{"w:cs", spec.ComplexFont, []string{"w:cstheme", "w:csTheme"}},
		} {
			if font.value != "" {
				updates = append(updates, xmlAttributeUpdate{font.name, font.value})
				remove = append(remove, font.themeNames...)
			}
		}
		if spec.FontHint != "" {
			updates = append(updates, xmlAttributeUpdate{"w:hint", spec.FontHint})
		}
		body = upsertPropertyElement(body, runFontsElement, "w:rFonts", updates, remove)
	}
	if spec.FontSizeHalfPoints > 0 {
		body = upsertPropertyElement(body, runSizeElement, "w:sz", []xmlAttributeUpdate{{"w:val", strconv.Itoa(spec.FontSizeHalfPoints)}}, nil)
	}
	if spec.ComplexSizeHalfPts > 0 {
		body = upsertPropertyElement(body, runComplexSizeElement, "w:szCs", []xmlAttributeUpdate{{"w:val", strconv.Itoa(spec.ComplexSizeHalfPts)}}, nil)
	}
	if spec.BoldSet || spec.Bold {
		body = upsertOnOffProperty(body, runBoldElement, "w:b", spec.Bold)
		body = upsertOnOffProperty(body, runComplexBoldElement, "w:bCs", spec.Bold)
	}
	if spec.ItalicSet || spec.Italic {
		body = upsertOnOffProperty(body, runItalicElement, "w:i", spec.Italic)
		body = upsertOnOffProperty(body, runComplexItalicElement, "w:iCs", spec.Italic)
	}
	if spec.VerticalAlign != "" {
		body = upsertPropertyElement(body, runVertAlignElement, "w:vertAlign", []xmlAttributeUpdate{{"w:val", spec.VerticalAlign}}, nil)
	}
	if spec.Color != "" {
		body = upsertPropertyElement(body, runColorElement, "w:color", []xmlAttributeUpdate{{"w:val", spec.Color}}, nil)
	}
	return body
}

func buildRunProperties(spec RunPropertiesSpec) string {
	return `<w:rPr>` + buildRunPropertiesBody(spec) + `</w:rPr>`
}

func buildRunPropertiesBody(spec RunPropertiesSpec) string {
	var builder strings.Builder
	if spec.EastAsiaFont != "" || spec.AsciiFont != "" || spec.HAnsiFont != "" || spec.ComplexFont != "" || spec.FontHint != "" {
		builder.WriteString(`<w:rFonts`)
		if spec.EastAsiaFont != "" {
			builder.WriteString(fmt.Sprintf(` w:eastAsia="%s"`, spec.EastAsiaFont))
		}
		if spec.AsciiFont != "" {
			builder.WriteString(fmt.Sprintf(` w:ascii="%s"`, spec.AsciiFont))
		}
		if spec.HAnsiFont != "" {
			builder.WriteString(fmt.Sprintf(` w:hAnsi="%s"`, spec.HAnsiFont))
		}
		if spec.ComplexFont != "" {
			builder.WriteString(fmt.Sprintf(` w:cs="%s"`, spec.ComplexFont))
		}
		if spec.FontHint != "" {
			builder.WriteString(fmt.Sprintf(` w:hint="%s"`, spec.FontHint))
		}
		builder.WriteString(`/>`)
	}
	if spec.BoldSet || spec.Bold {
		if spec.Bold {
			builder.WriteString(`<w:b/>`)
			builder.WriteString(`<w:bCs/>`)
		} else {
			builder.WriteString(`<w:b w:val="false"/>`)
			builder.WriteString(`<w:bCs w:val="false"/>`)
		}
	}
	if spec.ItalicSet || spec.Italic {
		if spec.Italic {
			builder.WriteString(`<w:i/>`)
			builder.WriteString(`<w:iCs/>`)
		} else {
			builder.WriteString(`<w:i w:val="false"/>`)
			builder.WriteString(`<w:iCs w:val="false"/>`)
		}
	}
	if spec.Color != "" {
		builder.WriteString(fmt.Sprintf(`<w:color w:val="%s"/>`, spec.Color))
	}
	if spec.FontSizeHalfPoints > 0 {
		builder.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/>`, spec.FontSizeHalfPoints))
	}
	if spec.ComplexSizeHalfPts > 0 {
		builder.WriteString(fmt.Sprintf(`<w:szCs w:val="%d"/>`, spec.ComplexSizeHalfPts))
	}
	if spec.VerticalAlign != "" {
		builder.WriteString(fmt.Sprintf(`<w:vertAlign w:val="%s"/>`, spec.VerticalAlign))
	}
	return builder.String()
}

func buildSpacing(before, after, beforeLines, afterLines int, beforeLinesSet, afterLinesSet bool, line int, lineRule string) string {
	var builder strings.Builder
	builder.WriteString(`<w:spacing`)
	if before > 0 {
		builder.WriteString(fmt.Sprintf(` w:before="%d"`, before))
	}
	if after > 0 {
		builder.WriteString(fmt.Sprintf(` w:after="%d"`, after))
	}
	if beforeLines > 0 || beforeLinesSet {
		builder.WriteString(fmt.Sprintf(` w:beforeLines="%d"`, beforeLines))
	}
	if afterLines > 0 || afterLinesSet {
		builder.WriteString(fmt.Sprintf(` w:afterLines="%d"`, afterLines))
	}
	if line > 0 {
		builder.WriteString(fmt.Sprintf(` w:line="%d"`, line))
	}
	if lineRule != "" {
		builder.WriteString(fmt.Sprintf(` w:lineRule="%s"`, lineRule))
	}
	builder.WriteString(`/>`)
	return builder.String()
}

func buildPageMargins(spec SectionPropertiesSpec) string {
	if spec.MarginTopTwips <= 0 && spec.MarginRightTwips <= 0 && spec.MarginBottomTwips <= 0 && spec.MarginLeftTwips <= 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf(`<w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d"`, spec.MarginTopTwips, spec.MarginRightTwips, spec.MarginBottomTwips, spec.MarginLeftTwips))
	if spec.GutterTwips > 0 {
		builder.WriteString(fmt.Sprintf(` w:gutter="%d"`, spec.GutterTwips))
	}
	if spec.HeaderMarginTwips > 0 {
		builder.WriteString(fmt.Sprintf(` w:header="%d"`, spec.HeaderMarginTwips))
	}
	if spec.FooterMarginTwips > 0 {
		builder.WriteString(fmt.Sprintf(` w:footer="%d"`, spec.FooterMarginTwips))
	}
	builder.WriteString(`/>`)
	return builder.String()
}

func buildPageNumberType(format string, start int) string {
	var builder strings.Builder
	builder.WriteString(`<w:pgNumType`)
	if format != "" {
		builder.WriteString(fmt.Sprintf(` w:fmt="%s"`, format))
	}
	if start > 0 {
		builder.WriteString(fmt.Sprintf(` w:start="%d"`, start))
	}
	builder.WriteString(`/>`)
	return builder.String()
}

type xmlAttributeUpdate struct {
	name  string
	value string
}

func sectionMarginsRequested(spec SectionPropertiesSpec) bool {
	return spec.MarginTopTwips > 0 ||
		spec.MarginRightTwips > 0 ||
		spec.MarginBottomTwips > 0 ||
		spec.MarginLeftTwips > 0 ||
		spec.GutterTwips > 0 ||
		spec.HeaderMarginTwips > 0 ||
		spec.FooterMarginTwips > 0
}

func hasRunPropertiesRequest(spec RunPropertiesSpec) bool {
	return spec.EastAsiaFont != "" ||
		spec.AsciiFont != "" ||
		spec.HAnsiFont != "" ||
		spec.ComplexFont != "" ||
		spec.FontHint != "" ||
		spec.FontSizeHalfPoints > 0 ||
		spec.ComplexSizeHalfPts > 0 ||
		spec.BoldSet ||
		spec.Bold ||
		spec.ItalicSet ||
		spec.Italic ||
		spec.Color != "" ||
		spec.VerticalAlign != ""
}

func updateNestedRunProperties(body string, spec RunPropertiesSpec) string {
	current := runPropertiesElement.FindString(body)
	if current == "" {
		return buildRunProperties(spec) + body
	}
	updated := replaceElementBody(current, updateRunPropertiesBody(elementBody(current), spec), "w:rPr")
	if ordered, _, err := ooxmlpkg.RepairPropertyBlock([]byte(updated), "rPr"); err == nil {
		updated = string(ordered)
	}
	return strings.Replace(body, current, updated, 1)
}

func upsertOnOffProperty(body string, pattern *regexp.Regexp, name string, enabled bool) string {
	if enabled {
		return upsertPropertyElement(body, pattern, name, nil, []string{"w:val"})
	}
	return upsertPropertyElement(body, pattern, name, []xmlAttributeUpdate{{"w:val", "false"}}, nil)
}

func upsertPropertyElement(
	body string,
	pattern *regexp.Regexp,
	name string,
	updates []xmlAttributeUpdate,
	removeAttributes []string,
) string {
	current := pattern.FindString(body)
	if current == "" {
		var builder strings.Builder
		builder.WriteString("<")
		builder.WriteString(name)
		for _, update := range updates {
			builder.WriteString(" ")
			builder.WriteString(update.name)
			builder.WriteString(`="`)
			builder.WriteString(escapeXMLAttribute(update.value))
			builder.WriteString(`"`)
		}
		builder.WriteString("/>")
		return builder.String() + body
	}

	updated := current
	for _, attribute := range removeAttributes {
		updated = removeXMLAttribute(updated, attribute)
	}
	for _, update := range updates {
		updated = setXMLAttribute(updated, update.name, update.value)
	}
	return strings.Replace(body, current, updated, 1)
}

func removeXMLAttribute(element, name string) string {
	pattern := regexp.MustCompile(`(?i)\s+` + regexp.QuoteMeta(name) + `\s*=\s*("[^"]*"|'[^']*')`)
	return pattern.ReplaceAllString(element, "")
}

func setXMLAttribute(element, name, value string) string {
	pattern := regexp.MustCompile(`(?i)(\s+` + regexp.QuoteMeta(name) + `\s*=\s*)("[^"]*"|'[^']*')`)
	if match := pattern.FindStringSubmatchIndex(element); match != nil {
		replacement := element[match[2]:match[3]] + `"` + escapeXMLAttribute(value) + `"`
		return element[:match[0]] + replacement + element[match[1]:]
	}
	insertAt := strings.LastIndex(element, "/>")
	if insertAt < 0 {
		return element
	}
	return element[:insertAt] + " " + name + `="` + escapeXMLAttribute(value) + `"` + element[insertAt:]
}

func escapeXMLAttribute(value string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		`"`, "&quot;",
		"<", "&lt;",
		">", "&gt;",
	).Replace(value)
}

func buildThreeLineBorders(spec TableBordersSpec) string {
	if spec.TopSize <= 0 {
		spec.TopSize = 12
	}
	if spec.HeaderSize <= 0 {
		spec.HeaderSize = 4
	}
	if spec.BottomSize <= 0 {
		spec.BottomSize = spec.TopSize
	}
	if spec.Color == "" {
		spec.Color = "000000"
	}
	return fmt.Sprintf(`<w:tblBorders><w:top w:val="single" w:sz="%d" w:space="0" w:color="%s"/><w:left w:val="nil"/><w:bottom w:val="single" w:sz="%d" w:space="0" w:color="%s"/><w:right w:val="nil"/><w:insideH w:val="single" w:sz="%d" w:space="0" w:color="%s"/><w:insideV w:val="nil"/></w:tblBorders>`, spec.TopSize, spec.Color, spec.BottomSize, spec.Color, spec.HeaderSize, spec.Color)
}

func firstElement(xmlText string, pattern *regexp.Regexp) string {
	match := pattern.FindString(xmlText)
	return match
}

func lastElement(xmlText string, pattern *regexp.Regexp) string {
	matches := pattern.FindAllString(xmlText, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func elementBody(element string) string {
	if strings.HasSuffix(element, "/>") {
		return ""
	}
	start := tagEndIndex(element)
	end := strings.LastIndex(element, "</")
	if start < 0 || end < 0 || end <= start {
		return ""
	}
	return element[start+1 : end]
}

func replaceElementBody(element, body, name string) string {
	start := tagEndIndex(element)
	if start < 0 || strings.HasSuffix(element, "/>") {
		return "<" + name + ">" + body + "</" + name + ">"
	}
	opening := element[:start+1]
	return opening + body + "</" + name + ">"
}

func tagEndIndex(element string) int {
	quote := byte(0)
	for index := 0; index < len(element); index++ {
		switch element[index] {
		case '\'', '"':
			if quote == 0 {
				quote = element[index]
			} else if quote == element[index] {
				quote = 0
			}
		case '>':
			if quote == 0 {
				return index
			}
		}
	}
	return -1
}

func insertBeforeClosingTag(xmlText, tag, insertion string) string {
	closing := "</" + tag + ">"
	if idx := strings.LastIndex(xmlText, closing); idx >= 0 {
		return xmlText[:idx] + insertion + xmlText[idx:]
	}
	return xmlText + insertion
}
