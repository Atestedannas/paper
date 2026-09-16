package fileprocessor

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"unicode"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
	"github.com/paper-format-checker/backend/pkg/aiclassifier"
)

// AIFormatApplier 基于ParagraphFormatSpec直接写入OOXML的确定性格式应用器。
// AI 只负责上游语义分类，不参与格式值计算。
// 完全绕过JSON规则解析链，消除多层转换带来的误差
type AIFormatApplier struct {
	processor *EnhancedProcessor
}

// NewAIFormatApplier 创建格式应用器
func NewAIFormatApplier(proc *EnhancedProcessor) *AIFormatApplier {
	return &AIFormatApplier{processor: proc}
}

// Apply 将模板格式规范应用到所有已分类的段落
//
// classified:      classifyParagraphs 返回的分类结果
// specs:           LoadFromFile 返回的模板格式规范
// skipCategories:  要跳过的段落类别（不修改格式），如 "cover"
//
// 返回实际修正的段落数量
func (a *AIFormatApplier) Apply(
	classified map[string][]document.Paragraph,
	specs map[string]ParagraphFormatSpec,
	skipCategories map[string]bool,
) int {
	total := 0
	for category, paras := range classified {
		if skipCategories[category] || isProtectedFormatCategory(category) {
			log.Printf("[AI应用] 跳过 %s (%d段)", category, len(paras))
			continue
		}
		if category == V2ThesisTitle {
			// 页顶论文题目：模板无独立 spec（题目在封面图中未采样），
			// 按论文题目规范写入：黑体、二号(22pt=44 halfpt)、加粗、居中（A3）。
			count := 0
			for _, para := range paras {
				if strings.TrimSpace(a.processor.extractParagraphText(para)) == "" {
					continue
				}
				a.applyThesisTitleRule(para)
				count++
			}
			if count > 0 {
				log.Printf("[AI应用] %s: 修正 %d 段 (页顶论文题目: 黑体二号22pt 加粗 居中)", category, count)
				total += count
			}
			continue
		}
		spec, ok := specs[category]
		if !ok || spec.IsEmpty() {
			// 没有该类型的模板格式，静默跳过（不污染日志）
			continue
		}
		count := 0
		for _, para := range paras {
			text := strings.TrimSpace(a.processor.extractParagraphText(para))
			if text == "" {
				continue
			}
			applySpec := spec
			// #region agent log H2
			if count == 0 { // 每类只记第一段
				actualSpec := extractParaFormatSpec(para)
				preview := []rune(text)
				if len(preview) > 25 {
					preview = preview[:25]
				}
				dbgLog("H2", "ai_format_applier.go:Apply",
					"applying spec to para",
					fmt.Sprintf(`{"category":%q,"text":%q,"specFont":%q,"specSizeHalfPt":%d,"specBold":%v,"actualFont":%q,"actualSizeHalfPt":%d,"actualBold":%v}`,
						category, string(preview),
						applySpec.FontEastAsia, applySpec.FontSizeHalfPt, applySpec.Bold,
						actualSpec.FontEastAsia, actualSpec.FontSizeHalfPt, actualSpec.Bold))
			}
			// #endregion agent log H2
			a.ApplySpecToPara(para, applySpec)
			if isAbstractLabelCategory(category) {
				// A3/A4: 摘要/关键词标签 run 差异化（标签加粗、中文标签黑体、
				// 摘要正文两端对齐 + 模板行距 line=324 auto），修复整段统一套格式。
				a.applyAbstractLabelExtras(para, category)
			}
			count++
		}
		if count > 0 {
			log.Printf("[AI应用] %s: 修正 %d 段 (font=%q size=%.1fpt bold=%v align=%s)",
				category, count, spec.FontEastAsia, spec.FontSizePt(),
				spec.Bold, jcToAlignString(spec.Alignment))
			total += count
		}
	}
	return total
}

func isProtectedFormatCategory(category string) bool {
	switch category {
	case aiclassifier.TypeTOC:
		return true
	default:
		return false
	}
}

// ApplySpecToPara 将格式规范直接写入段落的OOXML，无JSON解析
// 按照Word格式优先级链从低到高逐层覆写：段落级→段落RPr→Run级
func (a *AIFormatApplier) ApplySpecToPara(para document.Paragraph, spec ParagraphFormatSpec) {
	patch := buildParagraphSpecPatch(para, spec)
	if !hasParagraphSpecFields(patch) {
		return
	}
	a.applySpecPatchToPara(para, patch)
}

func (a *AIFormatApplier) applySpecPatchToPara(para document.Paragraph, spec ParagraphFormatSpec) {
	// 清除 Word 样式引用，避免样式定义覆盖直接设置的格式
	paraProps := para.Properties()
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}

	// 1. 对齐方式
	if spec.AlignmentSet {
		paraProps.SetAlignment(spec.Alignment)
	}

	// 2. 行距（直接操作XML，高层API行距设置有已知问题）
	if spec.LineSpacingVal > 0 {
		if pPr.Spacing == nil {
			pPr.Spacing = wml.NewCT_Spacing()
		}
		lv := spec.LineSpacingVal
		if pPr.Spacing.LineAttr == nil {
			pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
		}
		pPr.Spacing.LineAttr.Int64 = &lv
		pPr.Spacing.LineRuleAttr = spec.LineSpacingRule
	}

	// 3. 段前段后间距
	if spec.SpaceBefore > 0 {
		paraProps.Spacing().SetBefore(measurement.Distance(spec.SpaceBefore) * measurement.Twips)
	}
	if spec.SpaceAfter > 0 {
		paraProps.Spacing().SetAfter(measurement.Distance(spec.SpaceAfter) * measurement.Twips)
	}

	// 4. 首行缩进（twips → measurement.Distance）
	if spec.FirstLineIndent > 0 {
		paraProps.SetFirstLineIndent(measurement.Distance(spec.FirstLineIndent) * measurement.Twips)
	}

	// 4b. 左右缩进（twips）
	if spec.IndentLeft > 0 || spec.IndentRight > 0 || pPr.Ind != nil {
		if pPr.Ind == nil {
			pPr.Ind = wml.NewCT_Ind()
		}
		if spec.IndentLeft > 0 {
			left := int64(spec.IndentLeft)
			pPr.Ind.LeftAttr = &wml.ST_SignedTwipsMeasure{Int64: &left}
		}
		if spec.IndentRight > 0 {
			right := int64(spec.IndentRight)
			pPr.Ind.RightAttr = &wml.ST_SignedTwipsMeasure{Int64: &right}
		}
	}

	// 5. 分页符
	if spec.PageBreak {
		a.processor.setPageBreakBefore(para)
	}

	// 5b. 与下段同页 / 段内不分页
	{
		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}
		if spec.KeepWithNext {
			pPr.KeepNext = wml.NewCT_OnOff()
		}
		if spec.KeepLines {
			pPr.KeepLines = wml.NewCT_OnOff()
		}
	}

	// 6. 段落级默认Run属性（pPr/rPr）：字体/字号/加粗
	{
		if pPr.RPr == nil {
			pPr.RPr = wml.NewCT_ParaRPr()
		}
		a.applyFontToParaRPr(pPr.RPr, spec)
	}

	// 7. Run级格式（最高优先级）
	for _, run := range para.Runs() {
		a.applySpecToRun(run, spec)
	}
}

// applyFontToParaRPr 将字体信息写入段落级默认Run属性（pPr/rPr）
func (a *AIFormatApplier) applyFontToParaRPr(rPr *wml.CT_ParaRPr, spec ParagraphFormatSpec) {
	if spec.FontEastAsia != "" {
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		// EastAsia 用中文名，Ascii/HAnsi/Cs 用英文名，避免 WPS 显示 "黑体;SimHei"
		eastAsiaPtr := a.processor.getCachedFontName(spec.FontEastAsia)
		englishName := normalizedAsciiFont(spec.FontAscii, spec.FontEastAsia)
		asciiPtr := a.processor.getCachedFontName(englishName)
		rPr.RFonts.EastAsiaAttr = eastAsiaPtr
		rPr.RFonts.AsciiAttr = asciiPtr
		rPr.RFonts.HAnsiAttr = asciiPtr
		rPr.RFonts.CsAttr = asciiPtr
	}
	if spec.FontAscii != "" && spec.FontEastAsia == "" {
		// 纯ASCII字体（如英文参考文献条目）
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		asciiPtr := a.processor.getCachedFontName(spec.FontAscii)
		rPr.RFonts.AsciiAttr = asciiPtr
		rPr.RFonts.HAnsiAttr = asciiPtr
		rPr.RFonts.CsAttr = asciiPtr
	}
	if spec.FontSizeHalfPt > 0 {
		halfPt := spec.FontSizeHalfPt
		rPr.Sz = wml.NewCT_HpsMeasure()
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	}
	if spec.FontSizeCSHalfPt > 0 {
		rPr.SzCs = wml.NewCT_HpsMeasure()
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &spec.FontSizeCSHalfPt
	} else if spec.FontSizeHalfPt > 0 {
		// An omitted w:szCs inherits the normal size.  Do not retain a
		// student's stale complex-script size (for example 15pt on a 12pt
		// keyword label) when the template does not define one.
		rPr.SzCs = nil
	}
	if spec.BoldSet || spec.Bold {
		if spec.Bold {
			rPr.B = wml.NewCT_OnOff()
		} else {
			rPr.B = nil
		}
	}
	if spec.Underline {
		rPr.U = wml.NewCT_Underline()
		rPr.U.ValAttr = wml.ST_UnderlineSingle
	}
}

// applySpecToRun 将格式规范应用到单个Run（Run级优先级最高）
func (a *AIFormatApplier) applySpecToRun(run document.Run, spec ParagraphFormatSpec) {
	text := run.Text()
	_, _, hasComplex, _ := textScriptKinds(text)
	rPr := run.X().RPr
	if rPr == nil {
		rPr = wml.NewCT_RPr()
		run.X().RPr = rPr
	}

	// 字体
	// A paragraph can split its numbering/punctuation and Chinese text into
	// separate runs.  Writing EastAsia only to runs that currently contain CJK
	// leaves the first (often numeric) run with the student's old font slot.
	// The paragraph rule owns the font family, so normalize the slot on every
	// run when a template explicitly supplies it.
	if spec.FontEastAsia != "" {
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		eastAsiaPtr := a.processor.getCachedFontName(spec.FontEastAsia)
		rPr.RFonts.EastAsiaAttr = eastAsiaPtr
	}
	if spec.FontAscii != "" || spec.FontEastAsia != "" {
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		asciiPtr := a.processor.getCachedFontName(normalizedAsciiFont(spec.FontAscii, spec.FontEastAsia))
		rPr.RFonts.AsciiAttr = asciiPtr
		rPr.RFonts.HAnsiAttr = asciiPtr
	}
	if spec.FontAscii != "" {
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		rPr.RFonts.CsAttr = a.processor.getCachedFontName(normalizedAsciiFont(spec.FontAscii, spec.FontEastAsia))
	}

	// 字号（半磅单位，直接写w:sz，避免单位转换问题）
	// Apply the paragraph's size to every visible run, including whitespace or
	// punctuation-only runs. Leaving those runs untouched creates mixed-size
	// references and headings even though the paragraph-level audit passes.
	if spec.FontSizeHalfPt > 0 {
		halfPt := spec.FontSizeHalfPt
		if rPr.Sz == nil {
			rPr.Sz = wml.NewCT_HpsMeasure()
		}
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	}
	if spec.FontSizeCSHalfPt > 0 && (hasComplex || rPr.SzCs != nil) {
		if rPr.SzCs == nil {
			rPr.SzCs = wml.NewCT_HpsMeasure()
		}
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &spec.FontSizeCSHalfPt
	} else if spec.FontSizeHalfPt > 0 && (hasComplex || rPr.SzCs != nil) {
		// Keep complex-script rendering in lockstep with w:sz when the
		// template leaves w:szCs unspecified.
		halfPt := spec.FontSizeHalfPt
		if rPr.SzCs == nil {
			rPr.SzCs = wml.NewCT_HpsMeasure()
		}
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	}

	// 加粗
	if spec.BoldSet || spec.Bold {
		if spec.Bold {
			rPr.B = wml.NewCT_OnOff()
			rPr.BCs = wml.NewCT_OnOff()
		} else {
			rPr.B = nil
			rPr.BCs = nil
		}
	}

	// 斜体
	if spec.Italic {
		rPr.I = wml.NewCT_OnOff()
		rPr.ICs = wml.NewCT_OnOff()
	}

	// 下划线
	if spec.Underline {
		rPr.U = wml.NewCT_Underline()
		rPr.U.ValAttr = wml.ST_UnderlineSingle
	}
}

func buildParagraphSpecPatch(para document.Paragraph, expected ParagraphFormatSpec) ParagraphFormatSpec {
	actual := extractParaFormatSpec(para)
	patch := ParagraphFormatSpec{BoldSet: expected.BoldSet}
	pPr := para.X().PPr
	hasNamedStyle := pPr != nil && pPr.PStyle != nil
	var visibleText strings.Builder
	for _, run := range para.Runs() {
		visibleText.WriteString(run.Text())
	}
	hasCJK, hasASCII, hasComplex, _ := textScriptKinds(visibleText.String())
	typographyAbsent := actual.FontEastAsia == "" && actual.FontAscii == "" && actual.FontSizeHalfPt == 0
	// The dominant run is useful for sampling, but it is not sufficient for
	// applying a rule: Word commonly leaves one short label run (for example
	// "Key words:") at the student's old size. Normalize a requested
	// typography field when any visible run disagrees, while leaving unrelated
	// run properties (fields, hyperlinks, superscript, etc.) untouched.
	runSizeMismatch := false
	runCSSizeMismatch := false
	runEastAsiaMismatch := false
	runAsciiMismatch := false
	runBoldMismatch := false
	for _, run := range para.Runs() {
		if strings.TrimSpace(run.Text()) == "" {
			continue
		}
		runSpec, ok := extractTemplateRunFormatSpec(run)
		if expected.FontSizeHalfPt > 0 && (!ok || runSpec.FontSizeHalfPt != expected.FontSizeHalfPt) {
			runSizeMismatch = true
		}
		if expected.FontSizeCSHalfPt > 0 && hasComplex && (!ok || runSpec.FontSizeCSHalfPt != expected.FontSizeCSHalfPt) {
			runCSSizeMismatch = true
		} else if expected.FontSizeCSHalfPt == 0 && expected.FontSizeHalfPt > 0 && ok &&
			runSpec.FontSizeCSHalfPt > 0 && runSpec.FontSizeCSHalfPt != expected.FontSizeHalfPt {
			runCSSizeMismatch = true
		}
		if expected.FontEastAsia != "" && (!ok || getChineseFontName(runSpec.FontEastAsia) != getChineseFontName(expected.FontEastAsia)) {
			runEastAsiaMismatch = true
		}
		if expected.FontAscii != "" && (!ok || !strings.EqualFold(normalizedAsciiFont(runSpec.FontAscii, runSpec.FontEastAsia), normalizedAsciiFont(expected.FontAscii, expected.FontEastAsia))) {
			runAsciiMismatch = true
		}
		if expected.BoldSet && runSpec.Bold != expected.Bold {
			runBoldMismatch = true
		}
	}

	// Numbered headings (1.1, 1.3.1, ...) also match listLikeParagraphPattern.
	// That pattern is only useful for preserving list indentation; it must not
	// suppress template font/size correction for headings.
	if hasCJK && expected.FontEastAsia != "" &&
		(typographyAbsent || (actual.FontEastAsia != "" && getChineseFontName(actual.FontEastAsia) != getChineseFontName(expected.FontEastAsia))) {
		patch.FontEastAsia = expected.FontEastAsia
	}
	if hasASCII && expected.FontAscii != "" &&
		(typographyAbsent || (actual.FontAscii != "" && !strings.EqualFold(normalizedAsciiFont(actual.FontAscii, actual.FontEastAsia), normalizedAsciiFont(expected.FontAscii, expected.FontEastAsia)))) {
		patch.FontAscii = expected.FontAscii
	}
	if runEastAsiaMismatch {
		patch.FontEastAsia = expected.FontEastAsia
	}
	if runAsciiMismatch {
		patch.FontAscii = expected.FontAscii
	}
	if expected.FontSizeHalfPt > 0 && (actual.FontSizeHalfPt > 0 || typographyAbsent) {
		delta := int64(expected.FontSizeHalfPt) - int64(actual.FontSizeHalfPt)
		if delta > 1 || delta < -1 {
			patch.FontSizeHalfPt = expected.FontSizeHalfPt
		}
	}
	if runSizeMismatch {
		patch.FontSizeHalfPt = expected.FontSizeHalfPt
	}
	if hasComplex && expected.FontSizeCSHalfPt > 0 && actual.FontSizeCSHalfPt != expected.FontSizeCSHalfPt {
		patch.FontSizeCSHalfPt = expected.FontSizeCSHalfPt
	}
	if runCSSizeMismatch {
		patch.FontSizeCSHalfPt = expected.FontSizeCSHalfPt
		if patch.FontSizeCSHalfPt == 0 {
			patch.FontSizeCSHalfPt = expected.FontSizeHalfPt
		}
	}
	if expected.Bold && !actual.Bold {
		patch.Bold = true
	} else if expected.BoldSet && !expected.Bold && actual.Bold {
		// The template explicitly samples normal weight; remove stale student bold.
		patch.BoldSet = true
	}
	if runBoldMismatch {
		patch.BoldSet = true
		patch.Bold = expected.Bold
	}
	if expected.Italic && !actual.Italic && !hasNamedStyle {
		patch.Italic = true
	}
	if expected.Underline && !actual.Underline {
		patch.Underline = true
	}
	if expected.AlignmentSet && (!actual.AlignmentSet || actual.Alignment != expected.Alignment) &&
		!(hasNamedStyle && !actual.AlignmentSet) {
		patch.AlignmentSet = true
		patch.Alignment = expected.Alignment
	}
	if expected.LineSpacingVal > 0 && (actual.LineSpacingVal > 0 || typographyAbsent) {
		delta := expected.LineSpacingVal - actual.LineSpacingVal
		// A paragraph can already have the right numeric line value but the
		// wrong OOXML rule (for example exact instead of auto).  Treat the rule
		// as an independent managed property; otherwise repair rounds leave a
		// persistent visual mismatch while reporting the value as correct.
		ruleMismatch := expected.LineSpacingRule != 0 && actual.LineSpacingRule != expected.LineSpacingRule
		if delta > 20 || delta < -20 || ruleMismatch {
			patch.LineSpacingVal = expected.LineSpacingVal
			patch.LineSpacingRule = expected.LineSpacingRule
		}
	}
	if expected.SpaceBefore > 0 && actual.SpaceBefore > 0 && actual.SpaceBefore != expected.SpaceBefore {
		patch.SpaceBefore = expected.SpaceBefore
	}
	if expected.SpaceAfter > 0 && actual.SpaceAfter > 0 && actual.SpaceAfter != expected.SpaceAfter {
		patch.SpaceAfter = expected.SpaceAfter
	}
	// A numbered heading can still have an explicit first-line indent in the
	// template (the Chongqing sample uses 560 twips for level-3 headings).
	// Do not discard that rule merely because the text looks list-like.
	if expected.FirstLineIndent > 0 {
		hasCharacterIndent := pPr != nil && pPr.Ind != nil && pPr.Ind.FirstLineCharsAttr != nil
		delta := int64(expected.FirstLineIndent) - int64(actual.FirstLineIndent)
		tolerance := int64(expected.FontSizeHalfPt * 3)
		if tolerance < 40 {
			tolerance = 40
		}
		if !hasCharacterIndent && (actual.FirstLineIndent == 0 || delta > tolerance || delta < -tolerance) {
			patch.FirstLineIndent = expected.FirstLineIndent
		}
	}
	if expected.PageBreak && !actual.PageBreak && !hasNamedStyle {
		patch.PageBreak = true
	}
	if expected.KeepWithNext && !actual.KeepWithNext && !hasNamedStyle {
		patch.KeepWithNext = true
	}
	if expected.KeepLines && !actual.KeepLines && !hasNamedStyle {
		patch.KeepLines = true
	}
	return patch
}

// paragraphHasRunFormatMismatch complements paragraph-level verification. A
// paragraph can have a compliant dominant run while a short label run still
// carries stale size/font/bold attributes.
func paragraphHasRunFormatMismatch(para document.Paragraph, expected ParagraphFormatSpec) bool {
	for _, run := range para.Runs() {
		if strings.TrimSpace(run.Text()) == "" {
			continue
		}
		actual, ok := extractTemplateRunFormatSpec(run)
		if expected.FontSizeHalfPt > 0 && (!ok || actual.FontSizeHalfPt != expected.FontSizeHalfPt) {
			return true
		}
		if expected.FontSizeCSHalfPt > 0 && (!ok || actual.FontSizeCSHalfPt != expected.FontSizeCSHalfPt) {
			return true
		}
		if expected.FontSizeCSHalfPt == 0 && expected.FontSizeHalfPt > 0 && ok &&
			actual.FontSizeCSHalfPt > 0 && actual.FontSizeCSHalfPt != expected.FontSizeHalfPt {
			return true
		}
		if expected.FontEastAsia != "" && (!ok || getChineseFontName(actual.FontEastAsia) != getChineseFontName(expected.FontEastAsia)) {
			return true
		}
		if expected.FontAscii != "" && (!ok || !strings.EqualFold(normalizedAsciiFont(actual.FontAscii, actual.FontEastAsia), normalizedAsciiFont(expected.FontAscii, expected.FontEastAsia))) {
			return true
		}
		if expected.BoldSet && actual.Bold != expected.Bold {
			return true
		}
	}
	return false
}

var listLikeParagraphPattern = regexp.MustCompile(`^(?:[\(（]?\d+[\)）、.]|[①-⑳]|[一二三四五六七八九十]+[、）])`)

func paragraphHasExplicitBoldOff(para document.Paragraph) bool {
	isOff := func(value *wml.CT_OnOff) bool {
		return value != nil && value.ValAttr != nil && !styleOnOffEnabled(value)
	}
	if pPr := para.X().PPr; pPr != nil && pPr.RPr != nil && isOff(pPr.RPr.B) {
		return true
	}
	for _, run := range para.Runs() {
		if rPr := run.X().RPr; rPr != nil && (isOff(rPr.B) || isOff(rPr.BCs)) {
			return true
		}
	}
	return false
}

func hasTypographySpecFields(spec ParagraphFormatSpec) bool {
	return spec.FontEastAsia != "" || spec.FontAscii != "" || spec.FontSizeHalfPt > 0 || spec.BoldSet ||
		spec.FontSizeCSHalfPt > 0 || spec.Bold || spec.Italic || spec.Underline || spec.ColorHex != ""
}

func hasParagraphSpecFields(spec ParagraphFormatSpec) bool {
	return hasTypographySpecFields(spec) || spec.AlignmentSet || spec.LineSpacingVal > 0 ||
		spec.SpaceBefore > 0 || spec.SpaceAfter > 0 || spec.FirstLineIndent > 0 ||
		spec.IndentLeft > 0 || spec.IndentRight > 0 || spec.PageBreak ||
		spec.KeepWithNext || spec.KeepLines || spec.OutlineLevel > 0
}

func specManagesDiffField(spec ParagraphFormatSpec, field string) bool {
	switch field {
	case "font_east_asia":
		return spec.FontEastAsia != ""
	case "font_ascii":
		return spec.FontAscii != ""
	case "font_size_half_pt":
		return spec.FontSizeHalfPt > 0
	case "font_size_cs_half_pt":
		return spec.FontSizeCSHalfPt > 0
	case "bold":
		return spec.BoldSet || spec.Bold
	case "italic":
		return spec.Italic
	case "underline":
		return spec.Underline
	case "alignment":
		return spec.AlignmentSet
	case "line_spacing", "line_spacing_rule":
		return spec.LineSpacingVal > 0
	case "space_before":
		return spec.SpaceBefore > 0
	case "space_after":
		return spec.SpaceAfter > 0
	case "first_line_indent":
		return spec.FirstLineIndent > 0
	case "indent_left":
		return spec.IndentLeft > 0
	case "indent_right":
		return spec.IndentRight > 0
	case "color":
		return spec.ColorHex != ""
	case "outline_level":
		return spec.OutlineLevel > 0
	case "page_break":
		return spec.PageBreak
	case "keep_with_next":
		return spec.KeepWithNext
	case "keep_lines":
		return spec.KeepLines
	default:
		return false
	}
}

func normalizedAsciiFont(ascii, eastAsia string) string {
	if ascii == "" || (eastAsia != "" && strings.EqualFold(strings.TrimSpace(ascii), strings.TrimSpace(eastAsia))) {
		return getEnglishFontName(eastAsia)
	}
	return ascii
}

func textScriptKinds(text string) (hasCJK, hasASCII, hasComplex, hasNonComplex bool) {
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		complex := (r >= 0x0590 && r <= 0x08ff) ||
			(r >= 0xfb1d && r <= 0xfdff) ||
			(r >= 0xfe70 && r <= 0xfeff) ||
			(r >= 0x1ee00 && r <= 0x1eeff)
		if complex {
			hasComplex = true
			continue
		}
		hasNonComplex = true
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			hasCJK = true
		}
		if r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			hasASCII = true
		}
	}
	return
}

func isAbstractCategory(category string) bool {
	return category == "abstract" || category == "english_abstract" || category == "en_abstract"
}

// ApplyFontOnlyToTableCellPara 表格单元格专用：只修改字体/字号/加粗，
// 不清除 pStyle，不改段落级属性（行距/缩进/对齐），防止表格列宽重排换行
func (a *AIFormatApplier) ApplyFontOnlyToTableCellPara(para document.Paragraph, spec ParagraphFormatSpec) {
	// 不清除 pPr.PStyle（表格单元格样式需保留）
	// 不清除 rPr.RStyle
	// 不修改段落级属性（行距/对齐/缩进/分页符）

	// 只在 Run 级别修改字体/字号/加粗
	for _, run := range para.Runs() {
		a.applySpecToRun(run, spec)
	}

	// 更新段落级 rPr（pPr/rPr）中的字体，影响 run 继承
	if pPr := para.X().PPr; pPr != nil {
		if pPr.RPr == nil {
			pPr.RPr = wml.NewCT_ParaRPr()
		}
		a.applyFontToParaRPr(pPr.RPr, spec)
	}
}

// abstractLabelPrefixes 摘要/关键词段中"标签 run"的识别前缀（含中英文）。
// 学生模板常把"摘要："/"关键词："/"Abstract:"/"Key words:" 标签与正文写进
// 同一段落，只有按 run 拆分才能做到"标签加粗、正文不加粗"。
var abstractLabelPrefixes = map[string][]string{
	V2Abstract:   {"摘要"},
	V2Keywords:   {"关键词"},
	V2EnAbstract: {"Abstract"},
	V2EnKeywords: {"Key words", "Keywords"},
}

// abstractBodyLineTwips / abstractBodyLineRule 摘要正文行距的模板样例值
// （line=324 twips = 140%，rule=auto）。applyAbstractLabelExtras 按此值写入，
// compareAllWithSpecs 对摘要标签类别用同一值做验证期望，保证"验证/写入"一致：
// 否则 RepairAgent 会把 324auto 当成与 spec(400 exact) 的差异，每轮整段重刷并
// 覆盖标签差异化成果（A3/A4 行距过密、标签不被加粗的根因）。
const abstractBodyLineTwips int64 = 324
const abstractBodyLineRule = wml.ST_LineSpacingRuleAuto

// isAbstractLabelCategory 判断类别是否需要标签/正文差异化后处理（A3/A4）。
func isAbstractLabelCategory(category string) bool {
	_, ok := abstractLabelPrefixes[category]
	return ok
}

// applyAbstractLabelExtras 摘要/关键词段后处理（A3/A4）。
//
// 前置：ApplySpecToPara 已对整段统一套规范（字号/字体/行距），但学生把
// "摘要：+正文"合并进同一段落，整段统一套一个 spec 会导致标签与正文同格式。
// 本函数修正三点：
//  1. 标签 run（首个非空 run 命中前缀）加粗——中文标签另设黑体
//     （模板标注"小四号黑体"），英文标签仅加粗（Times New Roman 加粗）；
//  2. 摘要正文（abstract/en_abstract）行距改回模板样例值 line=324 rule=auto，
//     消除学生 400 exact 造成的行距过密；
//  3. 摘要正文两端对齐 + 首行缩进两字符（480 twips，模板标注"首行缩进两个字符"）。
func (a *AIFormatApplier) applyAbstractLabelExtras(para document.Paragraph, category string) {
	labelChanged := false
	for _, run := range para.Runs() {
		t := strings.TrimSpace(run.Text())
		if t == "" {
			continue
		}
		labelChanged = a.styleLabelRun(run, category, t)
		break // 只处理第一个非空 run（标签）
	}
	if category == V2Abstract || category == V2EnAbstract {
		paraProps := para.Properties()
		paraProps.SetAlignment(wml.ST_JcBoth)
		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}
		if pPr.Spacing == nil {
			pPr.Spacing = wml.NewCT_Spacing()
		}
		lv := abstractBodyLineTwips
		if pPr.Spacing.LineAttr == nil {
			pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
		}
		pPr.Spacing.LineAttr.Int64 = &lv
		pPr.Spacing.LineRuleAttr = abstractBodyLineRule
		// 首行缩进两字符（480 twips，模板标注"首行缩进两个字符"）
		paraProps.SetFirstLineIndent(measurement.Distance(480) * measurement.Twips)
	}
	if labelChanged {
		log.Printf("[AI应用] 摘要/关键词标签已加粗 (category=%s)", category)
	}
}

// styleLabelRun 命中标签前缀的 run 加粗（中文类别另设黑体），返回是否命中。
func (a *AIFormatApplier) styleLabelRun(run document.Run, category, trimmedText string) bool {
	prefixes, ok := abstractLabelPrefixes[category]
	if !ok {
		return false
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(trimmedText, prefix) {
			rPr := run.X().RPr
			if rPr == nil {
				rPr = wml.NewCT_RPr()
				run.X().RPr = rPr
			}
			rPr.B = wml.NewCT_OnOff()
			rPr.BCs = wml.NewCT_OnOff()
			if category == V2Abstract || category == V2Keywords {
				// 中文标签：黑体加粗（模板标注"小四号黑体"）
				if rPr.RFonts == nil {
					rPr.RFonts = wml.NewCT_Fonts()
				}
				heiti := "黑体"
				rPr.RFonts.EastAsiaAttr = &heiti
			}
			return true
		}
	}
	return false
}

// applyThesisTitleRule 页顶论文题目写入层修复（A3）。
// 学生现状：小初(36pt) 宋体加粗居中，超出模板要求。修复为目标规范：
// 黑体、二号(22pt=sz44 halfpt)、加粗、居中（模板标注"三号黑体居中"，
// 字号按任务书取二号22pt）。只改字体/字号/加粗/对齐，不触及段落结构。
func (a *AIFormatApplier) applyThesisTitleRule(para document.Paragraph) {
	para.Properties().SetAlignment(wml.ST_JcCenter)
	for _, run := range para.Runs() {
		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		heiti := "黑体"
		rPr.RFonts.AsciiAttr = &heiti
		rPr.RFonts.EastAsiaAttr = &heiti
		sz := uint64(44) // 二号 = 22pt = 44 halfpt
		if rPr.Sz == nil {
			rPr.Sz = wml.NewCT_HpsMeasure()
		}
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &sz
		if rPr.SzCs == nil {
			rPr.SzCs = wml.NewCT_HpsMeasure()
		}
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &sz
		rPr.B = wml.NewCT_OnOff()
		rPr.BCs = wml.NewCT_OnOff()
	}
}
