package fileprocessor

import (
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/ofc/sharedTypes"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
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
	coverFieldSpec        *ParagraphFormatSpec
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
	templateHeaderText    string // 模板页眉原文（用于提取学校名称）
}

// NewV2SmartFormatter 创建智能格式化器。headingSpecs 为模板提取的 heading_1/2/3 格式规范，bodySpec 为正文格式规范，refSpec 为参考文献条目格式规范。
// 🔒 LOCKED: 正文段落格式全部从模板提取
// 🔒 LOCKED: 参考文献条目行距从 refSpec 取值
func NewV2SmartFormatter(proc *EnhancedProcessor, headingSpecs map[string]ParagraphFormatSpec,
	bodySpec, refSpec *ParagraphFormatSpec,
	coverTitleSpec, coverFieldSpec, abstractTitleSpec, abstractContentSpec, keywordsSpec *ParagraphFormatSpec,
	enAbstractTitleSpec, enAbstractContentSpec, enKeywordsSpec *ParagraphFormatSpec,
	tocTitleSpec, tocEntrySpec *ParagraphFormatSpec,
	referencesTitleSpec, sectionTitleSpec, notesSpec, captionSpec, headerSpec *ParagraphFormatSpec,
	templateHeaderText string,
) *V2SmartFormatter {
	if headingSpecs == nil {
		headingSpecs = map[string]ParagraphFormatSpec{}
	}
	return &V2SmartFormatter{
		processor: proc, headingSpecs: headingSpecs, bodySpec: bodySpec, refSpec: refSpec,
		coverTitleSpec: coverTitleSpec, coverFieldSpec: coverFieldSpec,
		abstractTitleSpec:   abstractTitleSpec,
		abstractContentSpec: abstractContentSpec, keywordsSpec: keywordsSpec,
		enAbstractTitleSpec: enAbstractTitleSpec, enAbstractContentSpec: enAbstractContentSpec,
		enKeywordsSpec: enKeywordsSpec,
		tocTitleSpec:   tocTitleSpec, tocEntrySpec: tocEntrySpec,
		referencesTitleSpec: referencesTitleSpec, sectionTitleSpec: sectionTitleSpec,
		notesSpec: notesSpec, captionSpec: captionSpec, headerSpec: headerSpec,
		templateHeaderText: templateHeaderText,
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

	// 节点3a：v2_smart_format 路径 — 打印所有传入的格式 spec
	DiagPrintf(" ====== 节点3a: v2_smart_format 路径 — ApplySmartFormatting ======")
	if f.coverTitleSpec != nil {
		DiagPrintf(" [v2_smart] cover_title: %s", formatSpecCompact(*f.coverTitleSpec))
	}
	if f.coverFieldSpec != nil {
		DiagPrintf(" [v2_smart] cover_field: %s", formatSpecCompact(*f.coverFieldSpec))
	}
	if f.abstractTitleSpec != nil {
		DiagPrintf(" [v2_smart] abstract_title: %s", formatSpecCompact(*f.abstractTitleSpec))
	}
	if f.abstractContentSpec != nil {
		DiagPrintf(" [v2_smart] abstract: %s", formatSpecCompact(*f.abstractContentSpec))
	}
	if f.keywordsSpec != nil {
		DiagPrintf(" [v2_smart] keywords: %s", formatSpecCompact(*f.keywordsSpec))
	}
	if f.enAbstractTitleSpec != nil {
		DiagPrintf(" [v2_smart] en_abstract_title: %s", formatSpecCompact(*f.enAbstractTitleSpec))
	}
	if f.enAbstractContentSpec != nil {
		DiagPrintf(" [v2_smart] en_abstract: %s", formatSpecCompact(*f.enAbstractContentSpec))
	}
	if f.enKeywordsSpec != nil {
		DiagPrintf(" [v2_smart] en_keywords: %s", formatSpecCompact(*f.enKeywordsSpec))
	}
	if f.bodySpec != nil {
		DiagPrintf(" [v2_smart] body: %s", formatSpecCompact(*f.bodySpec))
	}
	if f.refSpec != nil {
		DiagPrintf(" [v2_smart] ref: %s", formatSpecCompact(*f.refSpec))
	}
	if f.tocTitleSpec != nil {
		DiagPrintf(" [v2_smart] toc_title: %s", formatSpecCompact(*f.tocTitleSpec))
	}
	if f.tocEntrySpec != nil {
		DiagPrintf(" [v2_smart] toc_entry: %s", formatSpecCompact(*f.tocEntrySpec))
	}
	if f.referencesTitleSpec != nil {
		DiagPrintf(" [v2_smart] references_title: %s", formatSpecCompact(*f.referencesTitleSpec))
	}
	if f.sectionTitleSpec != nil {
		DiagPrintf(" [v2_smart] section_title: %s", formatSpecCompact(*f.sectionTitleSpec))
	}
	if f.notesSpec != nil {
		DiagPrintf(" [v2_smart] notes: %s", formatSpecCompact(*f.notesSpec))
	}
	if f.captionSpec != nil {
		DiagPrintf(" [v2_smart] caption: %s", formatSpecCompact(*f.captionSpec))
	}
	if f.headerSpec != nil {
		DiagPrintf(" [v2_smart] header: %s", formatSpecCompact(*f.headerSpec))
	}
	for level, spec := range f.headingSpecs {
		DiagPrintf(" [v2_smart] headingSpec[%s]: %s", level, formatSpecCompact(spec))
	}

	f.formatThesisTitle(classified)
	f.formatAbstract(classified)
	f.formatHeading1(classified)
	f.formatTOC(classified)
	if f.processor == nil || strings.TrimSpace(f.processor.templatePath) == "" {
		f.formatSmartHeader(doc, classified)
	}
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

	college := templateprofile.ExtractCollegeName(f.templateHeaderText, "重庆工程学院")
	headerText := college + gradeYear + "届" + major + "专业" + docType
	log.Printf("[V2智能页眉] %q (模板页眉=%q, 班级=%q, 专业=%q)", headerText, f.templateHeaderText, coverInfo["班级"], major)

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

	// B11 fix: write content into ALL existing header parts to ensure header1.xml
	// has content regardless of which index it occupies in doc.Headers().
	// Previous fix only wrote to headers[0], which might not be header1.xml.
	var hdr document.Header
	headers := doc.Headers()

	// Determine header font and underline from spec
	headerFontName := "宋体"
	headerFontSize := 9.0
	headerUnderline := false
	if f.headerSpec != nil {
		if f.headerSpec.FontEastAsia != "" {
			headerFontName = f.headerSpec.FontEastAsia
		}
		if f.headerSpec.FontSizeHalfPt > 0 {
			headerFontSize = float64(f.headerSpec.FontSizeHalfPt) / 2.0
		}
		if f.headerSpec.Underline {
			headerUnderline = true
		}
	}

	headerSuffixes := deriveChapterSuffixes(classified)
	log.Printf("[SMART_HEADER] derived chapter suffixes (count=%d): %v", len(headerSuffixes), headerSuffixes)

	// First pass: read each header's original suffix from template content
	// (before clearing), so we preserve the template's header-to-chapter mapping.
	type headerSuffix struct {
		idx    int
		hdrIdx int // Header.Index() value
		suffix string
	}
	var origSuffixes []headerSuffix
	for i, h := range headers {
		orig := getHeaderPlainText(h)
		_, suffix := splitHeaderCoreSuffix(orig)
		log.Printf("[SMART_HEADER_DEBUG] header[%d] orig=%q suffix=%q", i, orig, suffix)
		if suffix != "" {
			origSuffixes = append(origSuffixes, headerSuffix{i, h.Index(), suffix})
		}
	}
	log.Printf("[SMART_HEADER] template headers=%d, original suffixes=%v",
		len(headers), origSuffixes)

	if len(headers) == 0 {
		hdr = doc.AddHeader()
		doc.BodySection().SetHeader(hdr, wml.ST_HdrFtrDefault)
	} else {
		// Build a lookup: for each header index in doc.Headers(), what suffix to use.
		// Priority: original template suffix > derived chapter suffix by order > empty
		idxSuffix := map[int]string{}
		for _, os := range origSuffixes {
			idxSuffix[os.idx] = os.suffix
		}

		// Dump header indices for debugging
		var hdrIdxList []int
		for _, h := range headers {
			hdrIdxList = append(hdrIdxList, h.Index())
		}
		log.Printf("[SMART_HEADER] Header.Index() order: %v, derived suffixes: %v",
			hdrIdxList, headerSuffixes)

		// Fill in remaining with derived suffixes (for headers that had no original suffix)
		suffixIdx := 0
		for i := range headers {
			if _, ok := idxSuffix[i]; ok {
				continue
			}
			if suffixIdx < len(headerSuffixes) && headerSuffixes[suffixIdx] != "" {
				idxSuffix[i] = headerSuffixes[suffixIdx]
				suffixIdx++
			}
		}

		for i, h := range headers {
			h.Clear()
			textToWrite := headerText
			if s, ok := idxSuffix[i]; ok && s != "" {
				textToWrite = headerText + "\t" + s
			}
			f.processor.buildDoubleLineHeaderParagraphEx(h, textToWrite, headerFontName, headerFontSize, headerUnderline)
		}
		hdr = headers[0]
	}

	if len(headers) == 0 {
		f.processor.buildDoubleLineHeaderParagraphEx(hdr, headerText, headerFontName, headerFontSize, headerUnderline)
		f.processor.propagateHdrFtrToAllSections(doc)
	}

	// 🔒 Header content is now written; original template section-header associations
	// are preserved. Do NOT clear EG_HdrFtrReferences or call per-section SetHeader —
	// template's own sectPr headerReference links to the correct header index already.
	// The loop above writes baseText+suffix to each header; whichever section originally
	// referenced a given header will pick up the new content automatically.

	// Post-cleanup: normalize any remaining headers that may have been
	// created after our initial pass (e.g. by applyHeaderFooter second pass).
	// This regex-replaces patterns like "XXXX大学XXXX届XXXX专业" with the
	// canonical header text to fix header10-style regressions.
	normalizeAllHeaderTexts(doc, headerText, headerFontName, headerFontSize, headerUnderline, f.processor)
}

// normalizeAllHeaderTexts scans all headers and replaces any text matching
// problematic patterns (grade-year + major info) with the canonical headerText.
// Also preserves any chapter suffix found in individual headers.
func normalizeAllHeaderTexts(doc *document.Document, canonicalText, fontName string, fontSize float64, underline bool, proc *EnhancedProcessor) {
	allHeaders := doc.Headers()
	for _, h := range allHeaders {
		currentText := getHeaderPlainText(h)
		if strings.HasPrefix(currentText, canonicalText) {
			continue
		}
		// Check if this header text needs normalization
		// Pattern: contains "届" or "专业" (indicating grade/major info leaked from cover)
		needsFix := strings.Contains(currentText, "届") || strings.Contains(currentText, "专业") ||
			strings.Contains(currentText, "人文科技学院")
		if !needsFix {
			continue
		}

		// Extract suffix from current text (if any)
		_, suffix := splitHeaderCoreSuffix(currentText)

		// Build corrected text
		corrected := canonicalText
		if suffix != "" {
			corrected = canonicalText + "\t" + suffix
		}

		// Only rewrite if actually different
		if corrected != currentText {
			h.Clear()
			proc.buildDoubleLineHeaderParagraphEx(h, corrected, fontName, fontSize, underline)
		}
	}
}

// splitHeaderCoreSuffix splits a header text into (coreText, chapterSuffix).
// coreText is everything up to and including "（论文）" or "（设计）";
// chapterSuffix is the trimmed remainder (e.g. "摘要", "ABSTRACT", "致谢").
func splitHeaderCoreSuffix(text string) (core, suffix string) {
	text = strings.TrimRight(text, " \t\r\n\u3000")
	for _, marker := range []string{"（论文）", "（设计）"} {
		if idx := strings.Index(text, marker); idx != -1 {
			coreEnd := idx + len(marker)
			core = text[:coreEnd]
			suffix = strings.TrimSpace(text[coreEnd:])
			return
		}
	}
	return text, ""
}

// extractHeaderChapterSuffix extracts the chapter name suffix from a header's
// existing text. If the header contains "（论文）" or "（设计）", the part after
// that marker (trimmed) is the chapter suffix (e.g. "摘要", "ABSTRACT").
func extractHeaderChapterSuffix(text string) string {
	_, suffix := splitHeaderCoreSuffix(text)
	return suffix
}

// getHeaderPlainText extracts plain text from all paragraphs/runs in a header.
func getHeaderPlainText(h document.Header) string {
	var sb strings.Builder
	for _, p := range h.Paragraphs() {
		for _, r := range p.Runs() {
			sb.WriteString(r.Text())
		}
	}
	return strings.TrimSpace(sb.String())
}

// deriveChapterSuffixes derives chapter name suffixes from classified paragraphs.
// Scans classified in order and collects unique section markers (abstract title,
// heading1, references title, acknowledgements title) to build a suffix list
// that matches the document section order.
func deriveChapterSuffixes(classified []V2ClassifiedPara) []string {
	var suffixes []string
	seen := map[string]bool{}

	// First pass: collect all suffixes with their type for prioritization
	type typedSuffix struct {
		text     string
		priority int // 1=abstract, 2=enAbstract, 3=toc, 4=heading1, 5=references, 6=acknowledgements
	}
	var collected []typedSuffix

	for _, cp := range classified {
		var ts typedSuffix
		switch cp.Type {
		case V2AbstractTitle:
			ts = typedSuffix{"摘要", 1}
		case V2EnAbstractTitle:
			ts = typedSuffix{"ABSTRACT", 2}
		case V2TOCTitle:
			ts = typedSuffix{"目录", 3}
		case V2Heading1:
			ts = typedSuffix{cp.Text, 4}
		case V2ReferencesTitle:
			ts = typedSuffix{"参考文献", 5}
		case V2AcknowledgementsTitle:
			ts = typedSuffix{"致谢", 6}
		default:
			continue
		}
		if ts.text != "" && !seen[ts.text] {
			seen[ts.text] = true
			collected = append(collected, ts)
		}
	}

	// Sort by priority: lower number = comes first
	sort.Slice(collected, func(i, j int) bool {
		return collected[i].priority < collected[j].priority
	})

	for _, ts := range collected {
		suffixes = append(suffixes, ts.text)
	}
	return suffixes
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
			f.formatAcknowledgementsTitle(classified[i].Para)
		case V2Acknowledgements:
			f.formatAcknowledgements(classified[i].Para)
		case V2AppendixTitle:
			f.formatSectionTitle(classified[i].Para, V2AppendixTitle)
		case V2Appendix:
			if !f.applyExactCategorySpec(classified[i].Para, V2Appendix) {
				f.formatBodyPara(classified[i].Para)
			}
		case V2NotesTitle:
			f.formatSectionTitle(classified[i].Para, V2NotesTitle)
		case V2Cover:
			f.formatCoverField(classified[i].Para)
		case V2Notes:
			f.formatNotesContent(classified[i].Para)
		}
	}
}

// isCoverTitleParagraph 判断是否为封面标题段落（"本科毕业论文/设计" 等）
func (f *V2SmartFormatter) isCoverTitleParagraph(para document.Paragraph) bool {
	fullText := ""
	for _, r := range para.Runs() {
		fullText += r.Text()
	}
	text := strings.TrimSpace(fullText)
	return strings.Contains(text, "毕业论文") || strings.Contains(text, "毕业设计")
}

// formatCoverField 封面字段（学院/专业/姓名/学号/指导教师等）：小二号宋体加粗，行距800，分散对齐
func (f *V2SmartFormatter) formatCoverField(para document.Paragraph) {
	// 优先使用 coverFieldSpec（单个字段标签），回退 coverTitleSpec（封面标题）
	// 特殊情况：如果是封面标题段落（如"本科毕业论文/设计"），使用 coverTitleSpec
	spec := f.coverFieldSpec
	isCoverTitle := f.isCoverTitleParagraph(para)
	if isCoverTitle && f.coverTitleSpec != nil {
		spec = f.coverTitleSpec
	}
	if spec == nil {
		spec = f.coverTitleSpec
	}

	// DIAG: trace formatCoverField calls
	fullText := ""
	for _, r := range para.Runs() {
		fullText += r.Text()
	}
	text := strings.TrimSpace(fullText)
	specName := "coverFieldSpec"
	if isCoverTitle {
		specName = "coverTitleSpec"
	}
	if spec == nil {
		specName = "NIL"
	}
	DiagPrintf("[formatCoverField] text=%-20s isCoverTitle=%v spec=%s", truncStr(text, 20), isCoverTitle, specName)
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}

	// 分散对齐 distribute
	alignment := wml.ST_JcDistribute
	if spec != nil && spec.AlignmentSet {
		alignment = spec.Alignment
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = alignment

	// 行距 800（单倍行距）
	lineRule := wml.ST_LineSpacingRuleExact
	lineVal := int64(800)
	if spec != nil && spec.LineSpacingVal > 0 {
		lineVal = spec.LineSpacingVal
		lineRule = wml.ST_LineSpacingRuleAuto
	}
	pPr.Spacing = wml.NewCT_Spacing()
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = lineRule

	// 清除段前后间距和缩进
	pPr.Spacing.BeforeAttr = nil
	pPr.Spacing.AfterAttr = nil
	pPr.Ind = nil

	fontName := "宋体"
	sizePt := 18.0
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
	if hasSpec && spec.FirstLineIndent > 0 {
		pPr.Ind = wml.NewCT_Ind()
		fl := spec.FirstLineIndent
		pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &fl
	} else {
		pPr.Ind = nil
	}
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
	if hasSpec && spec.FirstLineIndent > 0 {
		pPr.Ind = wml.NewCT_Ind()
		fl := spec.FirstLineIndent
		pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &fl
	} else {
		pPr.Ind = nil
	}
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
	if hasSpec && spec.FirstLineIndent > 0 {
		pPr.Ind = wml.NewCT_Ind()
		fl := spec.FirstLineIndent
		pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &fl
	} else {
		pPr.Ind = nil
	}
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
		// 如果 RFonts 或 EastAsiaAttr 为 nil，说明字体来自样式继承而非直接格式化，
		// 此时无法确认字体正确，必须返回 false 触发格式化。
		if rPr.RFonts != nil && rPr.RFonts.EastAsiaAttr != nil {
			if *rPr.RFonts.EastAsiaAttr != eastAsiaFont {
				return false
			}
		} else {
			return false
		}
	}
	return true
}

func (f *V2SmartFormatter) formatBodyPara(para document.Paragraph) {
	// 委托给统一的 FormatBodyParagraph，确保与 EnhancedProcessor 行为一致。
	bodySpec := BodyTextSpecFromParagraphFormatSpec(f.bodySpec)
	FormatBodyParagraph(para, bodySpec)
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
// ── 致谢格式化 ──

// formatAcknowledgementsTitle 致谢标题：分散对齐，12pt（24 half-pt），黑体
// 与普通 sectionTitle 不同：致谢标题在模板中是分散对齐（distribute），且字号为小四(12pt)
func (f *V2SmartFormatter) formatAcknowledgementsTitle(para document.Paragraph) {
	if f.applyExactCategorySpec(para, V2AcknowledgementsTitle) {
		return
	}
	spec := f.sectionTitleSpec
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	// 致谢标题固定分散对齐
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcDistribute
	// 清除缩进
	pPr.Ind = wml.NewCT_Ind()
	pPr.PageBreakBefore = nil

	// 行距从 bodySpec 取值
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	lineRule := wml.ST_LineSpacingRuleAuto
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

	fontName := "黑体"
	sizePt := 12.0 // 小四 = 12pt = 24 half-pt
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

// formatAcknowledgements 致谢正文：12pt，无首行缩进，宋体/Times New Roman
// 与普通 bodyPara 不同：(1) 无10.5pt字号上限 (2) 无首行缩进
func (f *V2SmartFormatter) formatAcknowledgements(para document.Paragraph) {
	if f.applyExactCategorySpec(para, V2Acknowledgements) {
		return
	}
	spec := f.bodySpec
	eastAsiaFont := "宋体"
	asciiFont := "Times New Roman"
	fontSizePt := 12.0 // 小四 = 12pt
	if spec != nil {
		if spec.FontEastAsia != "" {
			eastAsiaFont = spec.FontEastAsia
		}
		if spec.FontAscii != "" {
			asciiFont = spec.FontAscii
		}
		if spec.FontSizeHalfPt > 0 {
			fs := float64(spec.FontSizeHalfPt) / 2.0
			fontSizePt = fs
		}
	}

	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
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

	// 致谢正文无首行缩进 — 显式清零
	pPr.Ind = wml.NewCT_Ind()

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

// 🔒 LOCKED: 致谢/附录/注释标题 — 格式从 sectionTitleSpec 取值，行距从 bodySpec 取值，不硬编码
func (f *V2SmartFormatter) formatSectionTitle(para document.Paragraph, category string) {
	if f.applyExactCategorySpec(para, category) {
		return
	}
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

	// 🔒 LOCKED: 行距从 bodySpec 取值，fallback 为 360 auto（不硬编码 400 exact）
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	lineRule := wml.ST_LineSpacingRuleAuto
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

func (f *V2SmartFormatter) applyExactCategorySpec(para document.Paragraph, category string) bool {
	spec, ok := f.headingSpecs[category]
	if !ok || spec.IsEmpty() {
		return false
	}
	NewAIFormatApplier(f.processor).ApplySpecToPara(para, spec)
	return true
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

	// 🔒 LOCKED: 字体从 refSpec 取值，上限强制10.5pt（模板为撰写要求时采样值偏大）
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
	// 设置段落默认 run 属性（unioffice 序列化时以此覆盖 run 级属性）
	if pPr.RPr == nil {
		pPr.RPr = wml.NewCT_ParaRPr()
	}
	if pPr.RPr.RFonts == nil {
		pPr.RPr.RFonts = wml.NewCT_Fonts()
	}
	pPr.RPr.RFonts.EastAsiaAttr = f.processor.getCachedFontName(eastAsiaFont)
	pPr.RPr.RFonts.AsciiAttr = f.processor.getCachedFontName(asciiFont)
	pPr.RPr.RFonts.HAnsiAttr = f.processor.getCachedFontName(asciiFont)
	pPr.RPr.RFonts.CsAttr = f.processor.getCachedFontName(asciiFont)
	halfPt := uint64(sizePt * 2)
	pPr.RPr.Sz = wml.NewCT_HpsMeasure()
	pPr.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	pPr.RPr.SzCs = wml.NewCT_HpsMeasure()
	pPr.RPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt

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
