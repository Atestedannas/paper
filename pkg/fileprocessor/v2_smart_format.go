package fileprocessor

import (
	"log"
	"regexp"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/ofc/sharedTypes"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

// V2SmartFormatter 智能格式化器：处理需要特殊逻辑的段落（题目、摘要、页眉等）
// 🔒 LOCKED: 标题格式全部从模板提取 — headingSpecs 从 FormatRuleEngine 传入，不硬编码。
// 🔒 LOCKED: 正文格式全部从模板提取 — bodySpec 从 FormatRuleEngine 传入，不硬编码。
// 🔒 LOCKED: 参考文献条目行距从 refSpec 取值，不硬编码 400 exact。
type V2SmartFormatter struct {
	processor    *EnhancedProcessor
	headingSpecs map[string]ParagraphFormatSpec
	bodySpec     *ParagraphFormatSpec
	refSpec      *ParagraphFormatSpec

	// 新增 spec
	coverTitleSpec        *ParagraphFormatSpec
	abstractTitleSpec     *ParagraphFormatSpec
	abstractContentSpec   *ParagraphFormatSpec
	keywordsSpec          *ParagraphFormatSpec
	enAbstractTitleSpec   *ParagraphFormatSpec
	enAbstractContentSpec *ParagraphFormatSpec
	enKeywordsSpec        *ParagraphFormatSpec
	tocTitleSpec          *ParagraphFormatSpec
	tocEntrySpec          *ParagraphFormatSpec
	referencesTitleSpec   *ParagraphFormatSpec
	sectionTitleSpec      *ParagraphFormatSpec
	notesSpec             *ParagraphFormatSpec
	captionSpec           *ParagraphFormatSpec
	headerSpec            *ParagraphFormatSpec
}

// NewV2SmartFormatter 创建智能格式化器。headingSpecs 为模板提取的 heading_1/2/3 格式规范，bodySpec 为正文格式规范，refSpec 为参考文献条目格式规范。
// 🔒 LOCKED: 正文段落格式全部从模板提取
// 🔒 LOCKED: 参考文献条目行距从 refSpec 取值
func NewV2SmartFormatter(proc *EnhancedProcessor, headingSpecs map[string]ParagraphFormatSpec,
	bodySpec, refSpec *ParagraphFormatSpec,
	coverTitleSpec, abstractTitleSpec, abstractContentSpec, keywordsSpec *ParagraphFormatSpec,
	enAbstractTitleSpec, enAbstractContentSpec, enKeywordsSpec *ParagraphFormatSpec,
	tocTitleSpec, tocEntrySpec *ParagraphFormatSpec,
	referencesTitleSpec, sectionTitleSpec, notesSpec, captionSpec, headerSpec *ParagraphFormatSpec,
) *V2SmartFormatter {
	if headingSpecs == nil {
		headingSpecs = map[string]ParagraphFormatSpec{}
	}
	return &V2SmartFormatter{
		processor: proc, headingSpecs: headingSpecs, bodySpec: bodySpec, refSpec: refSpec,
		coverTitleSpec: coverTitleSpec, abstractTitleSpec: abstractTitleSpec,
		abstractContentSpec: abstractContentSpec, keywordsSpec: keywordsSpec,
		enAbstractTitleSpec: enAbstractTitleSpec, enAbstractContentSpec: enAbstractContentSpec,
		enKeywordsSpec: enKeywordsSpec,
		tocTitleSpec:   tocTitleSpec, tocEntrySpec: tocEntrySpec,
		referencesTitleSpec: referencesTitleSpec, sectionTitleSpec: sectionTitleSpec,
		notesSpec: notesSpec, captionSpec: captionSpec, headerSpec: headerSpec,
	}
}

// ApplySmartFormatting 在XML克隆之后，对特殊段落做精准格式化
func (f *V2SmartFormatter) ApplySmartFormatting(doc *document.Document, classified []V2ClassifiedPara) {
	// #region agent log
	coverCount, absCount, h1Count := 0, 0, 0
	for _, c := range classified {
		switch c.Type {
		case V2Cover:
			coverCount++
		case V2Abstract, V2AbstractTitle:
			absCount++
		case V2Heading1:
			h1Count++
		}
	}
	debugLog("v2_smart_format.go:ApplySmartFormatting", "H3_SMART_FMT_ENTRY", map[string]interface{}{
		"hypothesisId":  "H3",
		"totalParas":    len(classified),
		"coverCount":    coverCount,
		"abstractCount": absCount,
		"heading1Count": h1Count,
	})
	// #endregion

	f.formatThesisTitle(classified)
	f.formatAbstract(classified)
	f.formatHeading1(classified)
	f.formatTOC(classified)
	f.formatSmartHeader(doc, classified)
	log.Println("[V2智能格式] 特殊段落格式化完成")
}

// ── 1. 题目格式化：正标题 + 副标题 ──

func (f *V2SmartFormatter) formatThesisTitle(classified []V2ClassifiedPara) {
	for i := range classified {
		text := classified[i].Text
		if text == "" {
			continue
		}
		if classified[i].Type == V2ThesisTitle {
			f.applyTitleFormat(classified[i].Para, true)
			log.Printf("[V2智能格式] 正标题: %s", truncStr(text, 30))
		} else if classified[i].Type == V2ThesisSubtitle {
			f.applyTitleFormat(classified[i].Para, false)
			log.Printf("[V2智能格式] 副标题: %s", truncStr(text, 30))
		}
	}
}

func isThesisMainTitle(text string) bool {
	if strings.Contains(text, "本科毕业论文") || strings.Contains(text, "本科毕业设计") {
		return false
	}
	if strings.Contains(text, "学院") || strings.Contains(text, "专业") ||
		strings.Contains(text, "学号") || strings.Contains(text, "姓名") ||
		strings.Contains(text, "指导教师") || strings.Contains(text, "班级") {
		return false
	}
	if strings.Contains(text, "年") && strings.Contains(text, "月") && len([]rune(text)) < 15 {
		return false
	}
	// 题目通常是封面中独立的一行，字数 > 4 且 < 50
	runes := []rune(text)
	return len(runes) >= 4 && len(runes) <= 50
}

func isThesisSubtitle(text string) bool {
	return strings.HasPrefix(text, "——") || strings.HasPrefix(text, "—") ||
		strings.HasPrefix(text, "--")
}

// applyTitleFormat 应用题目格式
// isMain: true=正标题(黑体三号加粗居中段前1行), false=副标题(黑体小三加粗居中段后2行)
func (f *V2SmartFormatter) applyTitleFormat(para document.Paragraph, isMain bool) {
	spec := f.coverTitleSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}

	// 居中
	alignment := wml.ST_JcCenter
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment

	// 行距
	lineVal := int64(240) // fallback: 单倍行距 = 240 twips
	lineRule := wml.ST_LineSpacingRuleAuto
	if spec != nil && spec.LineSpacingVal > 0 {
		lineVal = spec.LineSpacingVal
		lineRule = spec.LineSpacingRule
	}
	pPr.Spacing = wml.NewCT_Spacing()
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule

	if isMain {
		before := uint64(312) // fallback: 段前1行
		if spec != nil && spec.SpaceBefore > 0 {
			before = spec.SpaceBefore
		}
		pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber = &before
	} else {
		after := uint64(624) // fallback: 段后2行
		if spec != nil && spec.SpaceAfter > 0 {
			after = spec.SpaceAfter
		}
		pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after
	}

	// Run属性
	fontName := "黑体"
	var fontSize float64
	if isMain {
		fontSize = 16.0 // fallback: 三号 = 16pt
	} else {
		fontSize = 15.0 // fallback: 小三号 = 15pt
	}
	bold := true
	if spec != nil {
		if spec.FontEastAsia != "" {
			fontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			fontSize = float64(spec.FontSizeHalfPt) / 2.0
		}
		bold = spec.Bold
	}
	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, fontName, fontSize, bold)
	}
}

// ── 2. 摘要格式化 ──

func (f *V2SmartFormatter) formatAbstract(classified []V2ClassifiedPara) {
	for i := range classified {
		switch classified[i].Type {
		case V2AbstractTitle:
			f.applyAbstractTitleFormat(classified[i].Para)
		case V2Abstract:
			f.applyAbstractContentFormat(classified[i].Para)
		case V2Keywords:
			f.applyKeywordsFormat(classified[i].Para)
		case V2EnAbstractTitle:
			f.applyEnAbstractTitleFormat(classified[i].Para)
		case V2EnAbstract:
			f.applyEnAbstractContentFormat(classified[i].Para)
		case V2EnKeywords:
			f.applyEnKeywordsFormat(classified[i].Para)
		}
	}
}

// applyAbstractTitleFormat "摘要"二字：黑体小三号加粗居中，段前1行
func (f *V2SmartFormatter) applyAbstractTitleFormat(para document.Paragraph) {
	spec := f.abstractTitleSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcCenter
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment

	pPr.Spacing = wml.NewCT_Spacing()
	before := uint64(312) // fallback: 段前1行
	if spec != nil && spec.SpaceBefore > 0 {
		before = spec.SpaceBefore
	}
	pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber = &before

	fontName := "黑体"
	sizePt := 15.0 // fallback: 小三号=15pt
	bold := true
	if spec != nil {
		if spec.FontEastAsia != "" {
			fontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		bold = spec.Bold
	}
	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, fontName, sizePt, bold)
	}
	log.Printf("[V2智能格式] 摘要标题已格式化")
}

// applyAbstractContentFormat 摘要内容：宋体小四号，1.5倍行距，首行缩进2字符，段后2行
func (f *V2SmartFormatter) applyAbstractContentFormat(para document.Paragraph) {
	spec := f.abstractContentSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcBoth
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment

	// 行距
	lineVal := int64(360) // fallback: 1.5倍行距
	lineRule := wml.ST_LineSpacingRuleAuto
	if spec != nil && spec.LineSpacingVal > 0 {
		lineVal = spec.LineSpacingVal
		lineRule = spec.LineSpacingRule
	}
	pPr.Spacing = wml.NewCT_Spacing()
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule

	// 段后
	after := uint64(624) // fallback: 段后2行
	if spec != nil && spec.SpaceAfter > 0 {
		after = spec.SpaceAfter
	}
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	// 首行缩进
	firstLine := uint64(480) // fallback: 2字符
	if spec != nil && spec.FirstLineIndent > 0 {
		firstLine = spec.FirstLineIndent
	}
	pPr.Ind = wml.NewCT_Ind()
	pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &firstLine

	// Label run 属性：从 spec 取字体/字号/加粗
	labelFontName := "黑体"
	labelSizePt := 15.0
	labelBold := true
	if spec != nil {
		if spec.FontEastAsia != "" {
			labelFontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			labelSizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		labelBold = spec.Bold
	}

	// Body run 属性：从 bodySpec 取字体/字号
	bodyFontName := "宋体"
	bodySizePt := 12.0
	if f.bodySpec != nil {
		if f.bodySpec.FontEastAsia != "" {
			bodyFontName = f.bodySpec.FontEastAsia
		}
		if f.bodySpec.FontSizeHalfPt > 0 {
			bodySizePt = float64(f.bodySpec.FontSizeHalfPt) / 2.0
		}
	}

	// 智能处理 run 格式
	text := strings.TrimSpace(f.processor.extractParagraphText(para))
	runs := para.Runs()
	if strings.HasPrefix(text, "摘要") || strings.HasPrefix(text, "摘 要") {
		labelApplied := false
		for _, r := range runs {
			rText := r.Text()
			if !labelApplied && (strings.Contains(rText, "摘要") || strings.Contains(rText, "：") || strings.Contains(rText, ":")) {
				v2SetRunFont(f.processor, r, labelFontName, labelSizePt, labelBold)
				if strings.Contains(rText, "：") || strings.Contains(rText, ":") {
					labelApplied = true
				}
			} else {
				v2SetRunFont(f.processor, r, bodyFontName, bodySizePt, false)
				labelApplied = true
			}
		}
	} else {
		for _, r := range runs {
			v2SetRunFont(f.processor, r, bodyFontName, bodySizePt, false)
		}
	}
}

// applyKeywordsFormat 关键词格式：首行缩进2字符，1.5倍行距，段后2行
func (f *V2SmartFormatter) applyKeywordsFormat(para document.Paragraph) {
	spec := f.keywordsSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcBoth
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment

	lineVal := int64(360)
	lineRule := wml.ST_LineSpacingRuleAuto
	if spec != nil && spec.LineSpacingVal > 0 {
		lineVal = spec.LineSpacingVal
		lineRule = spec.LineSpacingRule
	}
	pPr.Spacing = wml.NewCT_Spacing()
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule
	after := uint64(624)
	if spec != nil && spec.SpaceAfter > 0 {
		after = spec.SpaceAfter
	}
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	firstLine := uint64(480)
	if spec != nil && spec.FirstLineIndent > 0 {
		firstLine = spec.FirstLineIndent
	}
	pPr.Ind = wml.NewCT_Ind()
	pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &firstLine

	labelFontName := "黑体"
	labelSizePt := 15.0
	labelBold := true
	if spec != nil {
		if spec.FontEastAsia != "" {
			labelFontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			labelSizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		labelBold = spec.Bold
	}
	bodyFontName := "宋体"
	bodySizePt := 12.0
	if f.bodySpec != nil {
		if f.bodySpec.FontEastAsia != "" {
			bodyFontName = f.bodySpec.FontEastAsia
		}
		if f.bodySpec.FontSizeHalfPt > 0 {
			bodySizePt = float64(f.bodySpec.FontSizeHalfPt) / 2.0
		}
	}

	runs := para.Runs()
	labelApplied := false
	for _, r := range runs {
		rText := r.Text()
		if !labelApplied && (strings.Contains(rText, "关键词") || strings.Contains(rText, "关键字")) {
			v2SetRunFont(f.processor, r, labelFontName, labelSizePt, labelBold)
			if strings.Contains(rText, "：") || strings.Contains(rText, ":") {
				labelApplied = true
			}
		} else {
			v2SetRunFont(f.processor, r, bodyFontName, bodySizePt, false)
			labelApplied = true
		}
	}
}

func (f *V2SmartFormatter) applyEnAbstractTitleFormat(para document.Paragraph) {
	spec := f.enAbstractTitleSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcCenter
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment

	fontName := "Times New Roman"
	sizePt := 15.0
	bold := true
	if spec != nil {
		if spec.FontAscii != "" {
			fontName = spec.FontAscii
		} else if spec.FontEastAsia != "" {
			fontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		bold = spec.Bold
	}
	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, fontName, sizePt, bold)
	}
}

func (f *V2SmartFormatter) applyEnAbstractContentFormat(para document.Paragraph) {
	spec := f.enAbstractContentSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcBoth
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment
	lineVal := int64(360)
	lineRule := wml.ST_LineSpacingRuleAuto
	if spec != nil && spec.LineSpacingVal > 0 {
		lineVal = spec.LineSpacingVal
		lineRule = spec.LineSpacingRule
	}
	pPr.Spacing = wml.NewCT_Spacing()
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule
	after := uint64(624)
	if spec != nil && spec.SpaceAfter > 0 {
		after = spec.SpaceAfter
	}
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	firstLine := uint64(480)
	if spec != nil && spec.FirstLineIndent > 0 {
		firstLine = spec.FirstLineIndent
	}
	pPr.Ind = wml.NewCT_Ind()
	pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &firstLine

	labelFontName := "Times New Roman"
	labelSizePt := 15.0
	labelBold := true
	if spec != nil {
		if spec.FontAscii != "" {
			labelFontName = spec.FontAscii
		} else if spec.FontEastAsia != "" {
			labelFontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			labelSizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		labelBold = spec.Bold
	}
	bodyFontName := "Times New Roman"
	bodySizePt := 12.0
	if f.bodySpec != nil {
		if f.bodySpec.FontAscii != "" {
			bodyFontName = f.bodySpec.FontAscii
		}
		if f.bodySpec.FontSizeHalfPt > 0 {
			bodySizePt = float64(f.bodySpec.FontSizeHalfPt) / 2.0
		}
	}

	text := strings.TrimSpace(f.processor.extractParagraphText(para))
	lower := strings.ToLower(text)
	runs := para.Runs()
	if strings.HasPrefix(lower, "abstract") {
		labelApplied := false
		for _, r := range runs {
			rText := r.Text()
			if !labelApplied && (strings.Contains(strings.ToLower(rText), "abstract") || strings.Contains(rText, ":") || strings.Contains(rText, "：")) {
				v2SetRunFont(f.processor, r, labelFontName, labelSizePt, labelBold)
				if strings.Contains(rText, ":") || strings.Contains(rText, "：") {
					labelApplied = true
				}
			} else {
				v2SetRunFont(f.processor, r, bodyFontName, bodySizePt, false)
				labelApplied = true
			}
		}
	} else {
		for _, r := range runs {
			v2SetRunFont(f.processor, r, bodyFontName, bodySizePt, false)
		}
	}
}

func (f *V2SmartFormatter) applyEnKeywordsFormat(para document.Paragraph) {
	spec := f.enKeywordsSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcBoth
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment
	lineVal := int64(360)
	lineRule := wml.ST_LineSpacingRuleAuto
	if spec != nil && spec.LineSpacingVal > 0 {
		lineVal = spec.LineSpacingVal
		lineRule = spec.LineSpacingRule
	}
	pPr.Spacing = wml.NewCT_Spacing()
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule
	after := uint64(624) // fallback: 段后2行
	if spec != nil && spec.SpaceAfter > 0 {
		after = spec.SpaceAfter
	}
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after
	firstLine := uint64(480)
	if spec != nil && spec.FirstLineIndent > 0 {
		firstLine = spec.FirstLineIndent
	}
	pPr.Ind = wml.NewCT_Ind()
	pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &firstLine

	labelFontName := "Times New Roman"
	labelSizePt := 15.0
	labelBold := true
	if spec != nil {
		if spec.FontAscii != "" {
			labelFontName = spec.FontAscii
		} else if spec.FontEastAsia != "" {
			labelFontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			labelSizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		labelBold = spec.Bold
	}
	bodyFontName := "Times New Roman"
	bodySizePt := 12.0
	if f.bodySpec != nil {
		if f.bodySpec.FontAscii != "" {
			bodyFontName = f.bodySpec.FontAscii
		}
		if f.bodySpec.FontSizeHalfPt > 0 {
			bodySizePt = float64(f.bodySpec.FontSizeHalfPt) / 2.0
		}
	}

	runs := para.Runs()
	labelApplied := false
	for _, r := range runs {
		rText := strings.ToLower(r.Text())
		if !labelApplied && (strings.Contains(rText, "keyword") || strings.Contains(rText, "key word")) {
			v2SetRunFont(f.processor, r, labelFontName, labelSizePt, labelBold)
			if strings.Contains(rText, ":") || strings.Contains(rText, "：") {
				labelApplied = true
			}
		} else {
			v2SetRunFont(f.processor, r, bodyFontName, bodySizePt, false)
			labelApplied = true
		}
	}
}

// ── 3. 一级标题格式（绪论等）：三号宋体加粗，1.5倍行距，段前1行段后1行 ──
// 🔒 LOCKED: 正文标题 — 格式全部从 headingSpecs（模板提取）取，硬编码仅 fallback

func (f *V2SmartFormatter) formatHeading1(classified []V2ClassifiedPara) {
	spec, hasSpec := f.headingSpecs["heading_1"]
	fontName := "黑体"
	sizePt := 16.0
	bold := true
	alignment := wml.ST_JcCenter
	if hasSpec {
		if spec.FontEastAsia != "" {
			fontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		bold = spec.Bold
		if spec.AlignmentSet {
			alignment = spec.Alignment
		}
	}

	for i := range classified {
		if classified[i].Type != V2Heading1 || classified[i].Text == "" {
			continue
		}
		para := classified[i].Para

		// #region agent log
		hasPBB := false
		hasStyleRef := ""
		if para.X().PPr != nil && para.X().PPr.PageBreakBefore != nil {
			hasPBB = true
		}
		if para.X().PPr != nil && para.X().PPr.PStyle != nil {
			hasStyleRef = para.X().PPr.PStyle.ValAttr
		}
		debugLog("v2_smart_format.go:formatHeading1", "H5_HEADING1_BEFORE", map[string]interface{}{
			"hypothesisId":       "H5",
			"text":               truncStr(classified[i].Text, 30),
			"hasPageBreakBefore": hasPBB,
			"styleRef":           hasStyleRef,
		})
		// #endregion

		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}

		// 🔒 LOCKED: 一级标题 — 对齐/行距/段前后从 headingSpecs 取
		pPr.Jc = wml.NewCT_Jc()
		pPr.Jc.ValAttr = alignment

		pPr.Spacing = wml.NewCT_Spacing()
		lineVal := int64(360) // 1.5倍行距
		if hasSpec && spec.LineSpacingVal > 0 {
			lineVal = spec.LineSpacingVal
		}
		pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
		pPr.Spacing.LineAttr.Int64 = &lineVal
		pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto
		before := uint64(312) // 段前1行
		after := uint64(312)  // 段后1行
		if hasSpec && spec.SpaceBefore > 0 {
			before = spec.SpaceBefore
		}
		if hasSpec && spec.SpaceAfter > 0 {
			after = spec.SpaceAfter
		}
		pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber = &before
		pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

		// 去掉多余缩进
		pPr.Ind = nil

		// 不要分页符（避免绪论前空太多）
		pPr.PageBreakBefore = nil

		for _, r := range para.Runs() {
			v2SetRunFont(f.processor, r, fontName, sizePt, bold)
		}
	}
}

// ── 4. 页眉智能化：从封面提取 届/专业 ──

var reGradeYear = regexp.MustCompile(`(\d{4})级`)

func (f *V2SmartFormatter) formatSmartHeader(doc *document.Document, classified []V2ClassifiedPara) {
	coverInfo := f.processor.extractCoverInfo(doc)

	// 从班级提取入学年份 → +4 = 毕业届
	gradeYear := ""
	if banJi, ok := coverInfo["班级"]; ok && banJi != "" {
		if m := reGradeYear.FindStringSubmatch(banJi); len(m) >= 2 {
			if year, err := strconv.Atoi(m[1]); err == nil {
				gradeYear = strconv.Itoa(year + 4)
			}
		}
	}
	if gradeYear == "" {
		gradeYear = "XXX"
	}

	major := coverInfo["专业"]
	if major == "" {
		major = "XXX"
	}

	// 判断是论文还是设计：检查封面文字
	// "本科毕业论文/设计" 同时包含两者，默认"论文"；只有纯"设计"才改
	docType := "本科毕业论文"
	for _, cp := range classified {
		if cp.Type == V2Cover {
			t := cp.Text
			if strings.Contains(t, "毕业设计") && !strings.Contains(t, "论文") {
				docType = "本科毕业设计"
				break
			}
		}
	}

	headerText := "重庆人文科技学院" + gradeYear + "届" + major + "专业" + docType
	log.Printf("[V2智能页眉] %q (班级=%q, 专业=%q)", headerText, coverInfo["班级"], major)

	// #region agent log
	debugLog("v2_smart_format.go:formatSmartHeader", "H4_HEADER_GENERATION", map[string]interface{}{
		"hypothesisId": "H4",
		"headerText":   headerText,
		"coverInfo":    coverInfo,
		"gradeYear":    gradeYear,
		"major":        major,
		"docType":      docType,
	})
	// #endregion

	// 清除原有页眉并设置新页眉
	section := doc.BodySection()
	sectPr := section.X()
	if sectPr != nil {
		sectPr.EG_HdrFtrReferences = nil
	}
	hdr := doc.AddHeader()
	headerFontName := "宋体"
	headerFontSize := 9.0
	if f.headerSpec != nil {
		if f.headerSpec.FontEastAsia != "" {
			headerFontName = f.headerSpec.FontEastAsia
		}
		if f.headerSpec.FontSizeHalfPt > 0 {
			headerFontSize = float64(f.headerSpec.FontSizeHalfPt) / 2.0
		}
	}
	f.processor.buildDoubleLineHeaderParagraph(hdr, headerText, headerFontName, headerFontSize)
	section.SetHeader(hdr, wml.ST_HdrFtrDefault)
}

// ── 5. 目录格式化 ──

func (f *V2SmartFormatter) formatTOC(classified []V2ClassifiedPara) {
	for i := range classified {
		switch classified[i].Type {
		case V2TOCTitle:
			f.formatTOCTitle(classified[i].Para)
		case V2TOC:
			f.formatTOCEntry(classified[i].Para)
		}
	}
}

// formatTOCTitle "目录"二字：黑体，三号，居中，两字间空6个空格，段后2行
// 🔒 LOCKED: 目录标题 — 格式从 tocTitleSpec 取值，硬编码仅 fallback
func (f *V2SmartFormatter) formatTOCTitle(para document.Paragraph) {
	spec := f.tocTitleSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcCenter
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment
	pPr.Ind = nil

	pPr.Spacing = wml.NewCT_Spacing()
	after := uint64(624) // 段后2行
	if spec != nil && spec.SpaceAfter > 0 {
		after = spec.SpaceAfter
	}
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	// 修正文本为 "目      录"（两字间空6个空格）
	runs := para.Runs()
	fullText := ""
	for _, r := range runs {
		fullText += r.Text()
	}
	normalized := strings.ReplaceAll(strings.TrimSpace(fullText), " ", "")
	normalized = strings.ReplaceAll(normalized, "\u3000", "")
	if normalized == "目录" {
		for i, r := range runs {
			if i == 0 {
				r.ClearContent()
				r.AddText("目      录")
			} else {
				r.ClearContent()
			}
		}
	}

	fontName := "黑体"
	sizePt := 16.0
	bold := false
	if spec != nil {
		if spec.FontEastAsia != "" {
			fontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		bold = spec.Bold
	}
	for _, r := range runs {
		v2SetRunFont(f.processor, r, fontName, sizePt, bold)
	}
}

// formatTOCEntry 目录内容：宋体五号，1.5倍行距，两端对齐
// 🔒 LOCKED: 目录条目 — 格式从 tocEntrySpec 取值，硬编码仅 fallback
func (f *V2SmartFormatter) formatTOCEntry(para document.Paragraph) {
	spec := f.tocEntrySpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcBoth
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment

	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360) // 1.5倍行距
	lineRule := wml.ST_LineSpacingRuleAuto
	if spec != nil && spec.LineSpacingVal > 0 {
		lineVal = spec.LineSpacingVal
		lineRule = spec.LineSpacingRule
	}
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule

	fontName := "宋体"
	sizePt := 10.5
	if spec != nil {
		if spec.FontEastAsia != "" {
			fontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
	}
	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, fontName, sizePt, false)
		// 如果 spec 指定了西文字体，额外覆盖
		if spec != nil && spec.FontAscii != "" {
			rPr := r.X().RPr
			if rPr != nil && rPr.RFonts != nil {
				af := f.processor.getCachedFontName(spec.FontAscii)
				rPr.RFonts.AsciiAttr = af
				rPr.RFonts.HAnsiAttr = af
			}
		}
	}
}

// ── 6. 正文段落格式（全部段落类型）──

// ApplyBodyFormats 对全文所有段落应用标准格式
func (f *V2SmartFormatter) ApplyBodyFormats(classified []V2ClassifiedPara) {
	for i := range classified {
		switch classified[i].Type {
		case V2Heading2:
			f.formatHeading2(classified[i].Para)
		case V2Heading3:
			f.formatHeading3(classified[i].Para)
		case V2Heading4:
			f.formatHeading4(classified[i].Para)
		case V2Body:
			f.formatBodyPara(classified[i].Para)
		case V2ReferencesTitle:
			f.formatReferencesTitle(classified[i].Para)
		case V2References:
			f.formatReferenceItem(classified[i].Para)
		case V2TOCTitle:
			f.formatTOCTitle(classified[i].Para)
		case V2TOC:
			f.formatTOCEntry(classified[i].Para)
		case V2FigureCaption:
			f.formatCaption(classified[i].Para)
		case V2TableCaption:
			f.formatCaption(classified[i].Para)
		case V2AcknowledgementsTitle:
			f.formatSectionTitle(classified[i].Para)
		case V2Acknowledgements:
			f.formatBodyPara(classified[i].Para)
		case V2AppendixTitle:
			f.formatSectionTitle(classified[i].Para)
		case V2Appendix:
			f.formatBodyPara(classified[i].Para)
		case V2NotesTitle:
			f.formatSectionTitle(classified[i].Para)
		case V2Notes:
			f.formatNotesContent(classified[i].Para)
		}
	}
}

// 🔒 LOCKED: 二级标题 — 格式从 headingSpecs 取
func (f *V2SmartFormatter) formatHeading2(para document.Paragraph) {
	spec, hasSpec := f.headingSpecs["heading_2"]
	fontName := "黑体"
	sizePt := 15.0
	bold := true
	if hasSpec {
		if spec.FontEastAsia != "" {
			fontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		bold = spec.Bold
	}
	if v2RunFontMatches(para, fontName, sizePt, 1) {
		return
	}
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	// 模板克隆阶段可能保留学生稿上的 w:numPr；与正文里手打的「1.1 …」并存会导致编号重复显示，二级标题规范为数字前缀写在 runs 中，此处去掉列表编号。
	pPr.NumPr = nil
	pPr.Ind = nil
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	if hasSpec && spec.LineSpacingVal > 0 {
		lineVal = spec.LineSpacingVal
	}
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto

	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, fontName, sizePt, bold)
	}
}

func (f *V2SmartFormatter) formatHeading3(para document.Paragraph) {
	// 🔒 LOCKED: 三级标题 — 格式从 headingSpecs 取
	spec, hasSpec := f.headingSpecs["heading_3"]
	fontName := "黑体"
	sizePt := 14.0
	bold := true
	if hasSpec {
		if spec.FontEastAsia != "" {
			fontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		bold = spec.Bold
	}
	if v2RunFontMatches(para, fontName, sizePt, 1) {
		return
	}
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Ind = nil
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	if hasSpec && spec.LineSpacingVal > 0 {
		lineVal = spec.LineSpacingVal
	}
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto

	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, fontName, sizePt, bold)
	}
}

// formatHeading4 四级标题：四号宋体，不加粗，1.5倍行距
// 🔒 LOCKED: 四级标题 — 格式从 headingSpecs["heading_4"] 取值，硬编码仅 fallback
func (f *V2SmartFormatter) formatHeading4(para document.Paragraph) {
	spec, hasSpec := f.headingSpecs["heading_4"]
	fontName := "宋体"
	sizePt := 14.0
	bold := false
	lineVal := int64(360)
	lineRule := wml.ST_LineSpacingRuleAuto
	if hasSpec {
		if spec.FontEastAsia != "" {
			fontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		bold = spec.Bold
		if spec.LineSpacingVal > 0 {
			lineVal = spec.LineSpacingVal
			lineRule = spec.LineSpacingRule
		}
	}
	if v2RunFontMatches(para, fontName, sizePt, 1) {
		return
	}
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Ind = nil
	pPr.Spacing = wml.NewCT_Spacing()
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule

	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, fontName, sizePt, bold)
	}
}

// v2RunFontMatches 检查段落所有 run 的东亚字体/字号是否已与目标一致。
// 若全部匹配则说明步骤 7 克隆已生效，步骤 7b 可跳过此段避免无意义覆写。
// targetPt: 目标字号（pt），tolerance half-pt 公差（1 half-pt = 0.5pt）。
func v2RunFontMatches(para document.Paragraph, eastAsiaFont string, targetPt float64, tolerance uint64) bool {
	if len(para.Runs()) == 0 {
		return false
	}
	targetHalfPt := uint64(targetPt * 2)
	for _, r := range para.Runs() {
		if strings.TrimSpace(r.Text()) == "" {
			continue
		}
		rPr := r.X().RPr
		if rPr == nil || rPr.Sz == nil || rPr.Sz.ValAttr.ST_UnsignedDecimalNumber == nil {
			return false
		}
		currHalfPt := *rPr.Sz.ValAttr.ST_UnsignedDecimalNumber
		if currHalfPt < targetHalfPt-tolerance || currHalfPt > targetHalfPt+tolerance {
			return false
		}
		// 仅检查东亚字体（中文段落的关键区分因素）
		if rPr.RFonts != nil && rPr.RFonts.EastAsiaAttr != nil {
			if *rPr.RFonts.EastAsiaAttr != eastAsiaFont {
				return false
			}
		}
	}
	return true
}

func (f *V2SmartFormatter) formatBodyPara(para document.Paragraph) {
	// 🔒 LOCKED: 正文段落格式全部从模板提取 — 行距、字体、缩进从 bodySpec 取值，不硬编码
	spec := f.bodySpec
	eastAsiaFont := "宋体"
	asciiFont := "Times New Roman"
	fontSizePt := 12.0
	if spec != nil {
		if spec.FontEastAsia != "" {
			eastAsiaFont = spec.FontEastAsia
		}
		if spec.FontAscii != "" {
			asciiFont = spec.FontAscii
		}
		if spec.FontSizeHalfPt > 0 {
			fontSizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
	}
	// 先检查 run 格式是否已匹配，决定是否跳过 run 级别覆写
	runFormatMatched := v2RunFontMatches(para, eastAsiaFont, fontSizePt, 1)

	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	// 🔒 LOCKED: 段落属性（对齐/行距/缩进）必须始终设置，不能因 run 匹配而跳过
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcBoth

	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(400)
	lineRule := wml.ST_LineSpacingRuleExact
	if spec != nil {
		if spec.LineSpacingVal > 0 {
			lineVal = spec.LineSpacingVal
		}
		if spec.LineSpacingRule != wml.ST_LineSpacingRuleUnset {
			lineRule = spec.LineSpacingRule
		}
	}
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule

	pPr.Ind = wml.NewCT_Ind()
	fl := uint64(480)
	if spec != nil && spec.FirstLineIndent > 0 {
		fl = spec.FirstLineIndent
	}
	pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &fl

	// 仅当 run 格式未匹配时才覆写 run 属性
	if !runFormatMatched {
		for _, r := range para.Runs() {
			rPr := r.X().RPr
			if rPr == nil {
				rPr = wml.NewCT_RPr()
				r.X().RPr = rPr
			}
			if rPr.RFonts == nil {
				rPr.RFonts = wml.NewCT_Fonts()
			}
			rPr.RFonts.EastAsiaAttr = f.processor.getCachedFontName(eastAsiaFont)
			af := f.processor.getCachedFontName(asciiFont)
			rPr.RFonts.AsciiAttr = af
			rPr.RFonts.HAnsiAttr = af
			halfPt := uint64(fontSizePt * 2)
			rPr.Sz = wml.NewCT_HpsMeasure()
			rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
			rPr.SzCs = wml.NewCT_HpsMeasure()
			rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
			rPr.B = nil
			rPr.BCs = nil
		}
	}
}

// formatCaption 图题/表题：宋体五号，居中
// 🔒 LOCKED: 图题/表题 — 格式从 captionSpec 取值，硬编码仅 fallback
func (f *V2SmartFormatter) formatCaption(para document.Paragraph) {
	spec := f.captionSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcCenter
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment
	pPr.Ind = nil

	eastAsiaFont := "宋体"
	asciiFont := "Times New Roman"
	sizePt := 10.5
	if spec != nil {
		if spec.FontEastAsia != "" {
			eastAsiaFont = spec.FontEastAsia
		}
		if spec.FontAscii != "" {
			asciiFont = spec.FontAscii
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
	}
	for _, r := range para.Runs() {
		rPr := r.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			r.X().RPr = rPr
		}
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		rPr.RFonts.EastAsiaAttr = f.processor.getCachedFontName(eastAsiaFont)
		af := f.processor.getCachedFontName(asciiFont)
		rPr.RFonts.AsciiAttr = af
		rPr.RFonts.HAnsiAttr = af
		halfPt := uint64(sizePt * 2)
		rPr.Sz = wml.NewCT_HpsMeasure()
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		rPr.SzCs = wml.NewCT_HpsMeasure()
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		rPr.B = nil
		rPr.BCs = nil
	}
}

// formatSectionTitle 致谢/附录/注释标题：三号黑体居中(模板规范)
// 🔒 LOCKED: 致谢/附录/注释标题 — 格式从 sectionTitleSpec 取值，行距从 bodySpec 取值，不硬编码
func (f *V2SmartFormatter) formatSectionTitle(para document.Paragraph) {
	spec := f.sectionTitleSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	// 🔒 LOCKED: 对齐从 sectionTitleSpec 取值
	alignment := wml.ST_JcCenter
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment
	// 🔒 LOCKED: 用空 CT_Ind 显式覆盖样式继承的缩进（nil 不足以清除样式级缩进）
	pPr.Ind = wml.NewCT_Ind()
	pPr.PageBreakBefore = nil

	// 🔒 LOCKED: 行距从 bodySpec 取值，不硬编码 360 auto
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(400)
	lineRule := wml.ST_LineSpacingRuleExact
	if f.bodySpec != nil {
		if f.bodySpec.LineSpacingVal > 0 {
			lineVal = f.bodySpec.LineSpacingVal
		}
		if f.bodySpec.LineSpacingRule != wml.ST_LineSpacingRuleUnset {
			lineRule = f.bodySpec.LineSpacingRule
		}
	}
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule
	before := uint64(312)
	after := uint64(312)
	if spec != nil {
		if spec.SpaceBefore > 0 {
			before = spec.SpaceBefore
		}
		if spec.SpaceAfter > 0 {
			after = spec.SpaceAfter
		}
	}
	pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber = &before
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	fontName := "黑体"
	sizePt := 16.0
	bold := true
	if spec != nil {
		if spec.FontEastAsia != "" {
			fontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		bold = spec.Bold
	}
	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, fontName, sizePt, bold)
	}
}

// formatNotesContent 注释内容：宋体五号，顶格，1.5倍行距
// 🔒 LOCKED: 注释内容 — 格式从 notesSpec 取值，硬编码仅 fallback
func (f *V2SmartFormatter) formatNotesContent(para document.Paragraph) {
	spec := f.notesSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcBoth
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment
	pPr.Ind = nil

	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	lineRule := wml.ST_LineSpacingRuleAuto
	if spec != nil && spec.LineSpacingVal > 0 {
		lineVal = spec.LineSpacingVal
		lineRule = spec.LineSpacingRule
	}
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule

	eastAsiaFont := "宋体"
	asciiFont := "Times New Roman"
	sizePt := 10.5
	if spec != nil {
		if spec.FontEastAsia != "" {
			eastAsiaFont = spec.FontEastAsia
		}
		if spec.FontAscii != "" {
			asciiFont = spec.FontAscii
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
	}
	for _, r := range para.Runs() {
		rPr := r.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			r.X().RPr = rPr
		}
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		rPr.RFonts.EastAsiaAttr = f.processor.getCachedFontName(eastAsiaFont)
		af := f.processor.getCachedFontName(asciiFont)
		rPr.RFonts.AsciiAttr = af
		rPr.RFonts.HAnsiAttr = af
		halfPt := uint64(sizePt * 2)
		rPr.Sz = wml.NewCT_HpsMeasure()
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		rPr.SzCs = wml.NewCT_HpsMeasure()
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		rPr.B = nil
		rPr.BCs = nil
	}
}

func (f *V2SmartFormatter) formatReferencesTitle(para document.Paragraph) {
	spec := f.referencesTitleSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcCenter
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment
	pPr.Ind = nil

	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	lineRule := wml.ST_LineSpacingRuleAuto
	if spec != nil && spec.LineSpacingVal > 0 {
		lineVal = spec.LineSpacingVal
		lineRule = spec.LineSpacingRule
	}
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule
	before := uint64(312)
	after := uint64(312)
	if spec != nil {
		if spec.SpaceBefore > 0 {
			before = spec.SpaceBefore
		}
		if spec.SpaceAfter > 0 {
			after = spec.SpaceAfter
		}
	}
	pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber = &before
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	fontName := "黑体"
	sizePt := 16.0
	bold := true
	if spec != nil {
		if spec.FontEastAsia != "" {
			fontName = spec.FontEastAsia
		}
		if spec.FontSizeHalfPt > 0 {
			sizePt = float64(spec.FontSizeHalfPt) / 2.0
		}
		bold = spec.Bold
	}
	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, fontName, sizePt, bold)
	}
}

// formatReferenceItem 参考文献条目：宋体(外文TNR)五号，1.5倍行距，顶格不缩进
// 🔒 LOCKED: 参考文献条目 — 格式从 refSpec 取值，不硬编码
func (f *V2SmartFormatter) formatReferenceItem(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	alignment := wml.ST_JcBoth
	if f.refSpec != nil && f.refSpec.AlignmentSet {
		alignment = f.refSpec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment

	// 模板规范：参考文献顶格不缩进，序号后空一个字符
	pPr.Ind = wml.NewCT_Ind()

	// 🔒 LOCKED: 参考文献条目行距从 refSpec 取值，不硬编码 400 exact
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(400)
	lineRule := wml.ST_LineSpacingRuleExact
	if f.refSpec != nil {
		if f.refSpec.LineSpacingVal > 0 {
			lineVal = f.refSpec.LineSpacingVal
		}
		if f.refSpec.LineSpacingRule != wml.ST_LineSpacingRuleUnset {
			lineRule = f.refSpec.LineSpacingRule
		}
	}
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule

	// 🔒 LOCKED: 字体从 refSpec 取值
	eastAsiaFont := "宋体"
	asciiFont := "Times New Roman"
	sizePt := 10.5
	if f.refSpec != nil {
		if f.refSpec.FontEastAsia != "" {
			eastAsiaFont = f.refSpec.FontEastAsia
		}
		if f.refSpec.FontAscii != "" {
			asciiFont = f.refSpec.FontAscii
		}
		if f.refSpec.FontSizeHalfPt > 0 {
			sizePt = float64(f.refSpec.FontSizeHalfPt) / 2.0
		}
	}
	for _, r := range para.Runs() {
		rPr := r.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			r.X().RPr = rPr
		}
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		rPr.RFonts.EastAsiaAttr = f.processor.getCachedFontName(eastAsiaFont)
		af := f.processor.getCachedFontName(asciiFont)
		rPr.RFonts.AsciiAttr = af
		rPr.RFonts.HAnsiAttr = af
		halfPt := uint64(sizePt * 2)
		rPr.Sz = wml.NewCT_HpsMeasure()
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		rPr.SzCs = wml.NewCT_HpsMeasure()
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		rPr.B = nil
		rPr.BCs = nil
	}
}

// truncStr 截断字符串
func truncStr(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n]) + "..."
	}
	return s
}

// v2SetBodyRunFont 正文专用：中文宋体 + 数字/字母 Times New Roman
func v2SetBodyRunFont(proc *EnhancedProcessor, run document.Run, sizePt float64, bold bool) {
	rPr := run.X().RPr
	if rPr == nil {
		rPr = wml.NewCT_RPr()
		run.X().RPr = rPr
	}
	if rPr.RFonts == nil {
		rPr.RFonts = wml.NewCT_Fonts()
	}
	eastAsiaFont := proc.getCachedFontName("宋体")
	rPr.RFonts.EastAsiaAttr = eastAsiaFont
	tnrFont := proc.getCachedFontName("Times New Roman")
	rPr.RFonts.AsciiAttr = tnrFont
	rPr.RFonts.HAnsiAttr = tnrFont

	halfPt := uint64(sizePt * 2)
	rPr.Sz = wml.NewCT_HpsMeasure()
	rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	rPr.SzCs = wml.NewCT_HpsMeasure()
	rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt

	if bold {
		rPr.B = wml.NewCT_OnOff()
		rPr.BCs = wml.NewCT_OnOff()
	} else {
		rPr.B = nil
		rPr.BCs = nil
	}
}

// v2SetRunFont 设置 run 字体/字号/加粗（支持取消加粗）
func v2SetRunFont(proc *EnhancedProcessor, run document.Run, fontName string, sizePt float64, bold bool) {
	rPr := run.X().RPr
	if rPr == nil {
		rPr = wml.NewCT_RPr()
		run.X().RPr = rPr
	}

	if rPr.RFonts == nil {
		rPr.RFonts = wml.NewCT_Fonts()
	}
	fn := proc.getCachedFontName(fontName)
	rPr.RFonts.EastAsiaAttr = fn
	asciiFont := fontName
	if fontName == "宋体" {
		asciiFont = "SimSun"
	} else if fontName == "黑体" {
		asciiFont = "SimHei"
	}
	af := proc.getCachedFontName(asciiFont)
	rPr.RFonts.AsciiAttr = af
	rPr.RFonts.HAnsiAttr = af

	halfPt := uint64(sizePt * 2)
	rPr.Sz = wml.NewCT_HpsMeasure()
	rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	rPr.SzCs = wml.NewCT_HpsMeasure()
	rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt

	if bold {
		rPr.B = wml.NewCT_OnOff()
		rPr.BCs = wml.NewCT_OnOff()
	} else {
		rPr.B = nil
		rPr.BCs = nil
	}

	_ = measurement.Distance(0) // keep import
}
