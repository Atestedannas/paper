package fileprocessor

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

// dbgLog 写一行NDJSON到调试日志文件（仅Debug模式使用）
func dbgLog(hypothesisID, location, message string, jsonData string) {
	f, err := os.OpenFile("debug-c190b3.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, `{"sessionId":"c190b3","hypothesisId":%q,"location":%q,"message":%q,"data":%s,"timestamp":%d}`+"\n",
		hypothesisID, location, message, jsonData, time.Now().UnixMilli())
}

// ParagraphFormatSpec 从模板OOXML直接提取的段落格式规范
// 所有字段均为OOXML原生类型，零误差，无JSON解析链
type ParagraphFormatSpec struct {
	// Run级别属性
	FontEastAsia   string // 中文字体，如"宋体"
	FontAscii      string // ASCII/西文字体，如"Times New Roman"
	FontSizeHalfPt uint64 // w:sz值（半磅单位），0表示未设置
	Bold           bool
	Italic         bool

	// 段落级别属性
	AlignmentSet    bool      // Alignment字段是否有效
	Alignment       wml.ST_Jc // 对齐方式
	LineSpacingVal  int64     // w:spacing@w:line（twips），0表示未设置
	LineSpacingRule wml.ST_LineSpacingRule
	SpaceBefore     uint64 // w:spacing@w:before（twips）
	SpaceAfter      uint64 // w:spacing@w:after（twips）
	FirstLineIndent uint64 // w:ind@w:firstLine（twips）
	PageBreak       bool   // w:pageBreakBefore

	SampleCount int // 聚合时使用的样本数量

	// === 扩展字段（OOXML全量覆盖）===
	IndentLeft       uint64 // 左缩进 (twips)
	IndentRight      uint64 // 右缩进 (twips)
	Underline        bool   // 下划线
	ColorHex         string // 字体颜色，空=""表示自动
	FontSizeCSHalfPt uint64 // Complex Script 字号（控制中文精准字号）
	OutlineLevel     int    // 大纲级别：0=正文，1-9=标题层级
	KeepWithNext     bool   // 与下段同页
	KeepLines        bool   // 段内不分页
}

// FontSizePt 返回字号（磅值）
func (s ParagraphFormatSpec) FontSizePt() float64 {
	return float64(s.FontSizeHalfPt) / 2.0
}

// IsEmpty 判断规范是否为空（没有任何有效数据）
func (s ParagraphFormatSpec) IsEmpty() bool {
	return s.FontEastAsia == "" && s.FontAscii == "" && s.FontSizeHalfPt == 0 && s.FontSizeCSHalfPt == 0
}

// TemplateFormatLoader 从模板docx自动提取段落格式规范
// 支持内存缓存，同一模板文件只解析一次
type TemplateFormatLoader struct {
	processor *EnhancedProcessor
	mu        sync.Mutex
	cache     map[string]map[string]ParagraphFormatSpec
}

// NewTemplateFormatLoader 创建模板格式加载器
func NewTemplateFormatLoader(proc *EnhancedProcessor) *TemplateFormatLoader {
	return &TemplateFormatLoader{
		processor: proc,
		cache:     make(map[string]map[string]ParagraphFormatSpec),
	}
}

// LoadFromFile 从模板文件加载格式规范，带内存缓存
// templatePath: 模板文件路径（绝对或相对于工作目录）
func (l *TemplateFormatLoader) LoadFromFile(templatePath string) (map[string]ParagraphFormatSpec, error) {
	l.mu.Lock()
	if specs, ok := l.cache[templatePath]; ok {
		l.mu.Unlock()
		log.Printf("[模板加载] 命中缓存: %s (%d种类型)", templatePath, len(specs))
		return specs, nil
	}
	l.mu.Unlock()

	log.Printf("[模板加载] ════════ 开始解析模板格式: %s ════════", templatePath)
	doc, err := document.Open(templatePath)
	if err != nil {
		return nil, fmt.Errorf("无法打开模板文件 %s: %w", templatePath, err)
	}
	defer doc.Close()

	paragraphs := doc.Paragraphs()
	log.Printf("[模板加载] 模板段落总数: %d", len(paragraphs))

	// 使用现有智能分类器对模板段落进行分类
	classified := l.processor.classifyParagraphs(paragraphs)
	log.Printf("[模板加载] 分类完成，共 %d 种类型", len(classified))

	// 对每种类型，从样本中聚合得到共识格式
	specs := make(map[string]ParagraphFormatSpec)
	for category, paras := range classified {
		// 严格模板对齐模式：封面/声明也纳入规范提取
		// 目录条目中有点引导线和页码，字体可能不典型
		if category == "table_of_contents" {
			continue
		}
		if len(paras) == 0 {
			continue
		}

		// 收集有效格式样本
		samples := make([]ParagraphFormatSpec, 0, len(paras))
		for _, para := range paras {
			text := strings.TrimSpace(l.processor.extractParagraphText(para))
			if text == "" {
				continue
			}
			// abstract 类别：过滤掉 "摘要：" 等短标题行（≤12字），避免其Bold=true污染正文规范
			if category == "abstract" || category == "english_abstract" || category == "en_abstract" {
				if len([]rune(text)) <= 12 {
					log.Printf("[模板加载] %s: 跳过短标题行 %q", category, text)
					continue
				}
			}
			spec := extractParaFormatSpec(para)
			// #region agent log H1
			if len(samples) < 3 { // 每类只记录前3个样本，避免日志过多
				preview := []rune(text)
				if len(preview) > 25 {
					preview = preview[:25]
				}
				dbgLog("H1", "template_format_loader.go:extractParaFormatSpec",
					"sample font extraction",
					fmt.Sprintf(`{"category":%q,"text":%q,"fontEastAsia":%q,"fontSizeHalfPt":%d,"hasDirectFont":%v,"bold":%v}`,
						category, string(preview), spec.FontEastAsia, spec.FontSizeHalfPt, spec.FontEastAsia != "", spec.Bold))
			}
			// #endregion agent log H1
			// body类别：过滤掉加粗段落（加粗正文不是标准正文格式；封面/使用说明的黑体加粗段落会污染body spec）
			if category == "body" && spec.Bold {
				continue
			}
			// 只保留有字体或字号信息的样本
			if spec.FontSizeHalfPt > 0 || spec.FontEastAsia != "" {
				samples = append(samples, spec)
			}
		}
		if len(samples) == 0 {
			log.Printf("[模板加载] %s: 无有效样本，跳过", category)
			continue
		}

		consensus := consensusSpec(samples)
		consensus.SampleCount = len(samples)
		specs[category] = consensus
		// #region agent log H1-consensus
		dbgLog("H1", "template_format_loader.go:consensusSpec",
			"consensus spec derived",
			fmt.Sprintf(`{"runId":"post-fix","category":%q,"sampleCount":%d,"fontEastAsia":%q,"fontSizeHalfPt":%d,"bold":%v,"lineSpacingVal":%d,"firstLineIndent":%d}`,
				category, len(samples), consensus.FontEastAsia, consensus.FontSizeHalfPt, consensus.Bold, consensus.LineSpacingVal, consensus.FirstLineIndent))
		// #endregion agent log H1-consensus
		log.Printf("[模板加载] %s: %d个样本 → font=%q size=%.1fpt bold=%v align=%v lineSpacing=%d",
			category, len(samples),
			consensus.FontEastAsia, consensus.FontSizePt(),
			consensus.Bold, jcToAlignString(consensus.Alignment),
			consensus.LineSpacingVal)
	}

	if len(specs) == 0 {
		return nil, fmt.Errorf("模板解析结果为空，无法提取任何格式规范")
	}

	// ── Named Style 补充/覆盖（权威来源，无采样噪声） ──
	// 从 styles.xml 的命名样式定义中提取格式，覆盖采样结果中可能污染的字体/字号/加粗字段
	extractor := NewTemplateStyleExtractor()
	if styleSpecs, err2 := extractor.ExtractFromTemplate(templatePath); err2 == nil && len(styleSpecs) > 0 {
		for category, styleSpec := range styleSpecs {
			if styleSpec.IsEmpty() {
				continue
			}
			if existing, ok := specs[category]; ok {
				// 合并策略：Named Style 提供字体/字号/加粗（更权威），采样保留行距/缩进（更贴近段落实际设置）
				merged := existing
				if styleSpec.FontEastAsia != "" {
					merged.FontEastAsia = styleSpec.FontEastAsia
				}
				if styleSpec.FontAscii != "" {
					merged.FontAscii = styleSpec.FontAscii
				}
				if styleSpec.FontSizeHalfPt > 0 {
					merged.FontSizeHalfPt = styleSpec.FontSizeHalfPt
				}
				merged.Bold = styleSpec.Bold
				if styleSpec.AlignmentSet {
					merged.AlignmentSet = styleSpec.AlignmentSet
					merged.Alignment = styleSpec.Alignment
				}
				// 行距/缩进：若采样结果为0则用Named Style的值
				if merged.LineSpacingVal == 0 && styleSpec.LineSpacingVal != 0 {
					merged.LineSpacingVal = styleSpec.LineSpacingVal
					merged.LineSpacingRule = styleSpec.LineSpacingRule
				}
				if merged.FirstLineIndent == 0 && styleSpec.FirstLineIndent != 0 {
					merged.FirstLineIndent = styleSpec.FirstLineIndent
				}
				specs[category] = merged
				log.Printf("[模板加载] %s: Named Style覆盖 → font=%q size=%.1fpt bold=%v",
					category, merged.FontEastAsia, merged.FontSizePt(), merged.Bold)
			} else {
				// 采样没有该类型，直接用 Named Style 结果
				specs[category] = styleSpec
				log.Printf("[模板加载] %s: 仅Named Style → font=%q size=%.1fpt bold=%v",
					category, styleSpec.FontEastAsia, styleSpec.FontSizePt(), styleSpec.Bold)
			}
		}
	} else {
		log.Printf("[模板加载] ⚠️ Named Style提取失败，使用纯采样结果: %v", err2)
	}

	l.mu.Lock()
	l.cache[templatePath] = specs
	l.mu.Unlock()

	log.Printf("[模板加载] ════════ 完成：提取 %d 种格式规范（含Named Style覆盖） ════════", len(specs))
	return specs, nil
}

// extractParaFormatSpec 从单个段落提取OOXML格式属性
func extractParaFormatSpec(para document.Paragraph) ParagraphFormatSpec {
	spec := ParagraphFormatSpec{}

	// 取可见文本占比最大的 Run 作为“主导字体样本”，避免段首编号/标签污染整段字体判断。
	if dominant, ok := extractDominantTemplateRunFormatSpec(para); ok {
		spec.FontEastAsia = dominant.FontEastAsia
		spec.FontAscii = dominant.FontAscii
		spec.FontSizeHalfPt = dominant.FontSizeHalfPt
		spec.Bold = dominant.Bold
		spec.Italic = dominant.Italic
		spec.Underline = dominant.Underline
		spec.ColorHex = dominant.ColorHex
		spec.FontSizeCSHalfPt = dominant.FontSizeCSHalfPt
	}

	// 段落级属性
	pPr := para.X().PPr
	if pPr == nil {
		return spec
	}

	spec = mergeParagraphDefaultRunSpec(spec, pPr)

	if pPr.Jc != nil {
		spec.Alignment = pPr.Jc.ValAttr
		spec.AlignmentSet = true
	}

	if pPr.Spacing != nil {
		if pPr.Spacing.LineAttr != nil && pPr.Spacing.LineAttr.Int64 != nil {
			spec.LineSpacingVal = *pPr.Spacing.LineAttr.Int64
		}
		spec.LineSpacingRule = pPr.Spacing.LineRuleAttr
		if pPr.Spacing.BeforeAttr != nil && pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber != nil {
			spec.SpaceBefore = *pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber
		}
		if pPr.Spacing.AfterAttr != nil && pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber != nil {
			spec.SpaceAfter = *pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber
		}
	}

	if pPr.Ind != nil && pPr.Ind.FirstLineAttr != nil && pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber != nil {
		spec.FirstLineIndent = *pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber
	}

	spec.PageBreak = pPr.PageBreakBefore != nil

	// 左右缩进
	if pPr.Ind != nil {
		if pPr.Ind.LeftAttr != nil && pPr.Ind.LeftAttr.Int64 != nil && *pPr.Ind.LeftAttr.Int64 > 0 {
			spec.IndentLeft = uint64(*pPr.Ind.LeftAttr.Int64)
		}
		if pPr.Ind.RightAttr != nil && pPr.Ind.RightAttr.Int64 != nil && *pPr.Ind.RightAttr.Int64 > 0 {
			spec.IndentRight = uint64(*pPr.Ind.RightAttr.Int64)
		}
	}
	// 大纲级别
	if pPr.OutlineLvl != nil {
		spec.OutlineLevel = int(pPr.OutlineLvl.ValAttr) + 1
	}
	if pPr.KeepNext != nil {
		spec.KeepWithNext = true
	}
	if pPr.KeepLines != nil {
		spec.KeepLines = true
	}
	return completeParagraphFontSpec(spec)
}

func extractDominantTemplateRunFormatSpec(para document.Paragraph) (ParagraphFormatSpec, bool) {
	bestWeight := 0
	bestSpec := ParagraphFormatSpec{}

	for _, run := range para.Runs() {
		visible := strings.TrimSpace(run.Text())
		if visible == "" {
			continue
		}
		runSpec, ok := extractTemplateRunFormatSpec(run)
		if !ok {
			continue
		}
		weight := len([]rune(stripAllSpaces(visible)))
		if weight == 0 {
			weight = len([]rune(visible))
		}
		if weight > bestWeight {
			bestWeight = weight
			bestSpec = runSpec
		}
	}

	return bestSpec, bestWeight > 0
}

func extractTemplateRunFormatSpec(run document.Run) (ParagraphFormatSpec, bool) {
	rPr := run.X().RPr
	if rPr == nil {
		return ParagraphFormatSpec{}, false
	}

	spec := ParagraphFormatSpec{}
	if rPr.RFonts != nil {
		spec.FontEastAsia = resolveTemplateEastAsiaFont(
			fontAttrValue(rPr.RFonts.EastAsiaAttr),
			fontAttrValue(rPr.RFonts.CsAttr),
			fontAttrValue(rPr.RFonts.HAnsiAttr),
			fontAttrValue(rPr.RFonts.AsciiAttr),
		)
		spec.FontAscii = resolveTemplateAsciiFont(
			fontAttrValue(rPr.RFonts.AsciiAttr),
			fontAttrValue(rPr.RFonts.HAnsiAttr),
			fontAttrValue(rPr.RFonts.CsAttr),
			spec.FontEastAsia,
		)
	}
	if rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
		spec.FontSizeHalfPt = *rPr.Sz.ValAttr.ST_UnsignedDecimalNumber
	}
	spec.Bold = rPr.B != nil
	spec.Italic = rPr.I != nil
	if rPr.U != nil {
		spec.Underline = true
	}
	if rPr.Color != nil && rPr.Color.ValAttr.ST_HexColorRGB != nil {
		spec.ColorHex = *rPr.Color.ValAttr.ST_HexColorRGB
	}
	if rPr.SzCs != nil && rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber != nil {
		spec.FontSizeCSHalfPt = *rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber
	}
	spec = completeParagraphFontSpec(spec)
	if spec.IsEmpty() && spec.ColorHex == "" && !spec.Italic && !spec.Underline {
		return ParagraphFormatSpec{}, false
	}
	return spec, true
}

func completeParagraphFontSpec(spec ParagraphFormatSpec) ParagraphFormatSpec {
	spec.FontEastAsia = strings.TrimSpace(spec.FontEastAsia)
	spec.FontAscii = strings.TrimSpace(spec.FontAscii)

	if spec.FontEastAsia != "" {
		spec.FontEastAsia = getChineseFontName(spec.FontEastAsia)
	}
	if spec.FontAscii != "" && (isChineseFont(spec.FontAscii) || containsChineseChar(spec.FontAscii)) {
		spec.FontAscii = getEnglishFontName(getChineseFontName(spec.FontAscii))
	}

	if spec.FontEastAsia == "" && spec.FontAscii != "" && isChineseFont(spec.FontAscii) {
		spec.FontEastAsia = getChineseFontName(spec.FontAscii)
	}
	if spec.FontAscii == "" && spec.FontEastAsia != "" {
		spec.FontAscii = getEnglishFontName(getChineseFontName(spec.FontEastAsia))
	}

	return spec
}

func resolveTemplateEastAsiaFont(candidates ...string) string {
	for idx, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if idx == 0 {
			return getChineseFontName(candidate)
		}
		if isChineseFont(candidate) || containsChineseChar(candidate) {
			return getChineseFontName(candidate)
		}
	}
	return ""
}

func resolveTemplateAsciiFont(rawASCII, rawHAnsi, rawCS, eastAsia string) string {
	for _, candidate := range []string{rawASCII, rawHAnsi, rawCS} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if isChineseFont(candidate) || containsChineseChar(candidate) {
			return getEnglishFontName(getChineseFontName(candidate))
		}
		return candidate
	}
	if eastAsia != "" {
		return getEnglishFontName(getChineseFontName(eastAsia))
	}
	return ""
}

func fontAttrValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func mergeParagraphDefaultRunSpec(spec ParagraphFormatSpec, pPr *wml.CT_PPr) ParagraphFormatSpec {
	if pPr == nil || pPr.RPr == nil {
		return completeParagraphFontSpec(spec)
	}

	defaultSpec := extractParagraphDefaultRunFormatSpec(pPr.RPr)
	if spec.FontEastAsia == "" {
		spec.FontEastAsia = defaultSpec.FontEastAsia
	}
	if spec.FontAscii == "" {
		spec.FontAscii = defaultSpec.FontAscii
	}
	if spec.FontSizeHalfPt == 0 {
		spec.FontSizeHalfPt = defaultSpec.FontSizeHalfPt
	}
	if spec.FontSizeCSHalfPt == 0 {
		spec.FontSizeCSHalfPt = defaultSpec.FontSizeCSHalfPt
	}
	if spec.ColorHex == "" {
		spec.ColorHex = defaultSpec.ColorHex
	}

	return completeParagraphFontSpec(spec)
}

func extractParagraphDefaultRunFormatSpec(rPr *wml.CT_ParaRPr) ParagraphFormatSpec {
	if rPr == nil {
		return ParagraphFormatSpec{}
	}

	spec := ParagraphFormatSpec{}
	if rPr.RFonts != nil {
		spec.FontEastAsia = resolveTemplateEastAsiaFont(
			fontAttrValue(rPr.RFonts.EastAsiaAttr),
			fontAttrValue(rPr.RFonts.CsAttr),
			fontAttrValue(rPr.RFonts.HAnsiAttr),
			fontAttrValue(rPr.RFonts.AsciiAttr),
		)
		spec.FontAscii = resolveTemplateAsciiFont(
			fontAttrValue(rPr.RFonts.AsciiAttr),
			fontAttrValue(rPr.RFonts.HAnsiAttr),
			fontAttrValue(rPr.RFonts.CsAttr),
			spec.FontEastAsia,
		)
	}
	if rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
		spec.FontSizeHalfPt = *rPr.Sz.ValAttr.ST_UnsignedDecimalNumber
	}
	if rPr.SzCs != nil && rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber != nil {
		spec.FontSizeCSHalfPt = *rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber
	}
	if rPr.Color != nil && rPr.Color.ValAttr.ST_HexColorRGB != nil {
		spec.ColorHex = *rPr.Color.ValAttr.ST_HexColorRGB
	}

	return completeParagraphFontSpec(spec)
}

// consensusSpec 从多个样本中取"众数"格式，返回最具代表性的格式规范
func consensusSpec(samples []ParagraphFormatSpec) ParagraphFormatSpec {
	if len(samples) == 0 {
		return ParagraphFormatSpec{}
	}
	if len(samples) == 1 {
		return samples[0]
	}

	// 各属性的频率计数
	fontCounts := make(map[string]int)
	asciiFontCounts := make(map[string]int)
	sizeCounts := make(map[uint64]int)
	boldCounts := make(map[bool]int)
	alignCounts := make(map[wml.ST_Jc]int)
	lineSpacingCounts := make(map[int64]int)
	lineRuleCounts := make(map[wml.ST_LineSpacingRule]int)
	firstIndentCounts := make(map[uint64]int)

	var totalSpBefore, totalSpAfter uint64

	for _, s := range samples {
		if s.FontEastAsia != "" {
			fontCounts[s.FontEastAsia]++
		}
		if s.FontAscii != "" {
			asciiFontCounts[s.FontAscii]++
		}
		if s.FontSizeHalfPt > 0 {
			sizeCounts[s.FontSizeHalfPt]++
		}
		boldCounts[s.Bold]++
		if s.AlignmentSet {
			alignCounts[s.Alignment]++
		}
		if s.LineSpacingVal > 0 {
			lineSpacingCounts[s.LineSpacingVal]++
		}
		if s.LineSpacingRule != wml.ST_LineSpacingRuleUnset {
			lineRuleCounts[s.LineSpacingRule]++
		}
		if s.FirstLineIndent > 0 {
			firstIndentCounts[s.FirstLineIndent]++
		}
		totalSpBefore += s.SpaceBefore
		totalSpAfter += s.SpaceAfter
	}

	n := uint64(len(samples))
	consensus := ParagraphFormatSpec{}
	consensus.FontEastAsia = mostFrequentStringSpec(fontCounts)
	consensus.FontAscii = mostFrequentStringSpec(asciiFontCounts)
	consensus.FontSizeHalfPt = mostFrequentUint64Spec(sizeCounts)
	consensus.Bold = boldCounts[true] > boldCounts[false]
	if jc, count := mostFrequentJcSpec(alignCounts); count > 0 {
		consensus.Alignment = jc
		consensus.AlignmentSet = true
	}
	if val, count := mostFrequentInt64Spec(lineSpacingCounts); count > 0 {
		consensus.LineSpacingVal = val
	}
	if rule, count := mostFrequentLineRuleSpec(lineRuleCounts); count > 0 {
		consensus.LineSpacingRule = rule
	}
	if val, count := mostFrequentUint64SpecCount(firstIndentCounts); count > 0 {
		consensus.FirstLineIndent = val
	}
	// 段前段后取均值
	consensus.SpaceBefore = totalSpBefore / n
	consensus.SpaceAfter = totalSpAfter / n

	return consensus
}

// jcToAlignString 对齐常量转字符串（用于日志）
func jcToAlignString(jc wml.ST_Jc) string {
	switch jc {
	case wml.ST_JcCenter:
		return "center"
	case wml.ST_JcLeft:
		return "left"
	case wml.ST_JcRight:
		return "right"
	case wml.ST_JcBoth:
		return "justify"
	default:
		return "default"
	}
}

// ── 辅助：众数查找函数 ─────────────────────────────────────────────────

func mostFrequentStringSpec(m map[string]int) string {
	best, count := "", 0
	for k, v := range m {
		if v > count {
			best, count = k, v
		}
	}
	return best
}

func mostFrequentUint64Spec(m map[uint64]int) uint64 {
	var best uint64
	count := 0
	for k, v := range m {
		if v > count {
			best, count = k, v
		}
	}
	return best
}

func mostFrequentUint64SpecCount(m map[uint64]int) (uint64, int) {
	var best uint64
	count := 0
	for k, v := range m {
		if v > count {
			best, count = k, v
		}
	}
	return best, count
}

func mostFrequentInt64Spec(m map[int64]int) (int64, int) {
	var best int64
	count := 0
	for k, v := range m {
		if v > count {
			best, count = k, v
		}
	}
	return best, count
}

func mostFrequentJcSpec(m map[wml.ST_Jc]int) (wml.ST_Jc, int) {
	var best wml.ST_Jc
	count := 0
	for k, v := range m {
		if v > count {
			best, count = k, v
		}
	}
	return best, count
}

func mostFrequentLineRuleSpec(m map[wml.ST_LineSpacingRule]int) (wml.ST_LineSpacingRule, int) {
	var best wml.ST_LineSpacingRule
	count := 0
	for k, v := range m {
		if v > count {
			best, count = k, v
		}
	}
	return best, count
}
