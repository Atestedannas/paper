package ooxmlpatch

import (
	"fmt"
	"regexp"
	"strings"
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
	snapToGridElement      = regexp.MustCompile(`<w:snapToGrid\b[^>]*/>`)
	adjustRightIndElement  = regexp.MustCompile(`<w:adjustRightInd\b[^>]*/>`)
	paragraphStyleElement  = regexp.MustCompile(`<w:pStyle\b[^>]*/>`)
	outlineLevelElement    = regexp.MustCompile(`<w:outlineLvl\b[^>]*/>`)

	runFontsElement       = regexp.MustCompile(`<w:rFonts\b[^>]*/>`)
	runSizeElement        = regexp.MustCompile(`<w:sz\b[^>]*/>`)
	runComplexSizeElement = regexp.MustCompile(`<w:szCs\b[^>]*/>`)
	runBoldElement        = regexp.MustCompile(`<w:b\b[^>]*/>`)
	runComplexBoldElement = regexp.MustCompile(`<w:bCs\b[^>]*/>`)
	runItalicElement      = regexp.MustCompile(`<w:i\b[^>]*/>`)
	runVertAlignElement   = regexp.MustCompile(`<w:vertAlign\b[^>]*/>`)
	runColorElement       = regexp.MustCompile(`<w:color\b[^>]*/>`)

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
	SnapToGridOff      bool
	AdjustRightIndZero bool
	RunPropertiesInPPr bool
	RemoveOutlineLevel bool

	EastAsiaFont       string
	AsciiFont          string
	HAnsiFont          string
	FontSizeHalfPoints int
	ComplexSizeHalfPts int
	Bold               bool
	Italic             bool
	Color              string
}

type RunPropertiesSpec struct {
	EastAsiaFont       string
	AsciiFont          string
	HAnsiFont          string
	FontSizeHalfPoints int
	ComplexSizeHalfPts int
	Bold               bool
	Italic             bool
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

func ApplySettingsProperties(settingsXML string, spec SettingsPropertiesSpec) (string, bool) {
	updated := settingsXML
	if spec.EvenAndOddHeaders {
		updated = evenAndOddHeadersElement.ReplaceAllString(updated, "")
		updated = insertBeforeClosingTag(updated, "w:settings", `<w:evenAndOddHeaders/>`)
	}
	if spec.UpdateFieldsOnOpen {
		updated = updateFieldsElement.ReplaceAllString(updated, "")
		updated = insertBeforeClosingTag(updated, "w:settings", `<w:updateFields w:val="true"/>`)
	}
	return updated, updated != settingsXML
}

func ApplyParagraphProperties(paragraphXML string, spec ParagraphPropertiesSpec) (string, bool) {
	pPr := firstElement(paragraphXML, paragraphPropertiesElement)
	if pPr == "" {
		pPr = `<w:pPr/>`
	}
	updatedPPr := replaceElementBody(pPr, updateParagraphPropertiesBody(elementBody(pPr), spec), "w:pPr")
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
	body = pageSizeElement.ReplaceAllString(body, "")
	body = pageMarginElement.ReplaceAllString(body, "")
	body = pageNumberTypeElement.ReplaceAllString(body, "")
	if spec.RemoveHeaderFooter {
		body = headerFooterReferenceElement.ReplaceAllString(body, "")
	}

	var builder strings.Builder
	if spec.PageWidthTwips > 0 || spec.PageHeightTwips > 0 || spec.PageOrientation != "" {
		builder.WriteString(`<w:pgSz`)
		if spec.PageWidthTwips > 0 {
			builder.WriteString(fmt.Sprintf(` w:w="%d"`, spec.PageWidthTwips))
		}
		if spec.PageHeightTwips > 0 {
			builder.WriteString(fmt.Sprintf(` w:h="%d"`, spec.PageHeightTwips))
		}
		if spec.PageOrientation != "" {
			builder.WriteString(fmt.Sprintf(` w:orient="%s"`, spec.PageOrientation))
		}
		builder.WriteString(`/>`)
	}
	if margin := buildPageMargins(spec); margin != "" {
		builder.WriteString(margin)
	}
	if spec.PageNumberFormat != "" || spec.PageNumberStart > 0 {
		builder.WriteString(buildPageNumberType(spec.PageNumberFormat, spec.PageNumberStart))
	}
	builder.WriteString(body)
	return builder.String()
}

func updateParagraphPropertiesBody(body string, spec ParagraphPropertiesSpec) string {
	if spec.StyleID != "" {
		body = paragraphStyleElement.ReplaceAllString(body, "")
	}
	body = jcElement.ReplaceAllString(body, "")
	body = spacingElement.ReplaceAllString(body, "")
	body = indentElement.ReplaceAllString(body, "")
	body = pageBreakBeforeElement.ReplaceAllString(body, "")
	body = keepNextElement.ReplaceAllString(body, "")
	body = snapToGridElement.ReplaceAllString(body, "")
	body = adjustRightIndElement.ReplaceAllString(body, "")
	if spec.RemoveOutlineLevel || spec.OutlineLevelSet {
		body = outlineLevelElement.ReplaceAllString(body, "")
	}
	if spec.RunPropertiesInPPr {
		body = runPropertiesElement.ReplaceAllString(body, "")
	}

	var builder strings.Builder
	if spec.StyleID != "" {
		builder.WriteString(fmt.Sprintf(`<w:pStyle w:val="%s"/>`, spec.StyleID))
	}
	if spec.OutlineLevelSet {
		builder.WriteString(fmt.Sprintf(`<w:outlineLvl w:val="%d"/>`, spec.OutlineLevel))
	}
	if spec.Alignment != "" {
		builder.WriteString(fmt.Sprintf(`<w:jc w:val="%s"/>`, spec.Alignment))
	}
	if spec.BeforeTwips > 0 || spec.AfterTwips > 0 || spec.BeforeLines > 0 || spec.AfterLines > 0 || spec.BeforeLinesSet || spec.AfterLinesSet || spec.LineTwips > 0 || spec.LineRule != "" {
		builder.WriteString(buildSpacing(spec.BeforeTwips, spec.AfterTwips, spec.BeforeLines, spec.AfterLines, spec.BeforeLinesSet, spec.AfterLinesSet, spec.LineTwips, spec.LineRule))
	}
	if spec.FirstLineChars > 0 || spec.FirstLineCharsSet {
		builder.WriteString(`<w:ind`)
		builder.WriteString(fmt.Sprintf(` w:firstLineChars="%d"`, spec.FirstLineChars))
		if spec.FirstLineTwips > 0 {
			builder.WriteString(fmt.Sprintf(` w:firstLine="%d"`, spec.FirstLineTwips))
		}
		builder.WriteString(`/>`)
	} else if spec.FirstLineTwips > 0 {
		builder.WriteString(fmt.Sprintf(`<w:ind w:firstLine="%d"/>`, spec.FirstLineTwips))
	}
	if spec.PageBreakBefore {
		builder.WriteString(`<w:pageBreakBefore/>`)
	}
	if spec.KeepNext {
		builder.WriteString(`<w:keepNext/>`)
	}
	if spec.SnapToGridOff {
		builder.WriteString(`<w:snapToGrid w:val="0"/>`)
	}
	if spec.AdjustRightIndZero {
		builder.WriteString(`<w:adjustRightInd w:val="0"/>`)
	}
	if spec.RunPropertiesInPPr {
		builder.WriteString(buildRunProperties(RunPropertiesSpec{
			EastAsiaFont:       spec.EastAsiaFont,
			AsciiFont:          spec.AsciiFont,
			HAnsiFont:          spec.HAnsiFont,
			FontSizeHalfPoints: spec.FontSizeHalfPoints,
			ComplexSizeHalfPts: spec.ComplexSizeHalfPts,
			Bold:               spec.Bold,
			Italic:             spec.Italic,
			Color:              spec.Color,
		}))
	}
	builder.WriteString(body)
	return builder.String()
}

func updateRunPropertiesBody(body string, spec RunPropertiesSpec) string {
	body = runFontsElement.ReplaceAllString(body, "")
	body = runSizeElement.ReplaceAllString(body, "")
	body = runComplexSizeElement.ReplaceAllString(body, "")
	body = runBoldElement.ReplaceAllString(body, "")
	body = runComplexBoldElement.ReplaceAllString(body, "")
	body = runItalicElement.ReplaceAllString(body, "")
	body = runVertAlignElement.ReplaceAllString(body, "")
	body = runColorElement.ReplaceAllString(body, "")
	return buildRunPropertiesBody(spec) + body
}

func buildRunProperties(spec RunPropertiesSpec) string {
	return `<w:rPr>` + buildRunPropertiesBody(spec) + `</w:rPr>`
}

func buildRunPropertiesBody(spec RunPropertiesSpec) string {
	var builder strings.Builder
	if spec.EastAsiaFont != "" || spec.AsciiFont != "" || spec.HAnsiFont != "" {
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
		builder.WriteString(`/>`)
	}
	if spec.FontSizeHalfPoints > 0 {
		builder.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/>`, spec.FontSizeHalfPoints))
	}
	if spec.ComplexSizeHalfPts > 0 {
		builder.WriteString(fmt.Sprintf(`<w:szCs w:val="%d"/>`, spec.ComplexSizeHalfPts))
	}
	if spec.Bold {
		builder.WriteString(`<w:b/>`)
		builder.WriteString(`<w:bCs/>`)
	}
	if spec.Italic {
		builder.WriteString(`<w:i/>`)
	}
	if spec.Color != "" {
		builder.WriteString(fmt.Sprintf(`<w:color w:val="%s"/>`, spec.Color))
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
	start := strings.Index(element, ">")
	end := strings.LastIndex(element, "</")
	if start < 0 || end < 0 || end <= start {
		return ""
	}
	return element[start+1 : end]
}

func replaceElementBody(element, body, name string) string {
	start := strings.Index(element, ">")
	if start < 0 || strings.HasSuffix(element, "/>") {
		return "<" + name + ">" + body + "</" + name + ">"
	}
	opening := element[:start+1]
	return opening + body + "</" + name + ">"
}

func insertBeforeClosingTag(xmlText, tag, insertion string) string {
	closing := "</" + tag + ">"
	if idx := strings.LastIndex(xmlText, closing); idx >= 0 {
		return xmlText[:idx] + insertion + xmlText[idx:]
	}
	return xmlText + insertion
}
