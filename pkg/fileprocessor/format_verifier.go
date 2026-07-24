package fileprocessor

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
	"github.com/paper-format-checker/backend/pkg/aiclassifier"
)

// FormatVerifier 格式验证器：提取实际格式 → 与规则比对 → DeepSeek 分析 → 自动修正
type FormatVerifier struct {
	processor *EnhancedProcessor
	client    *aiclassifier.DeepSeekWebClient
}

// FormatDiff 单个格式属性差异
type FormatDiff struct {
	Category string `json:"category"`
	ParaIdx  int    `json:"para_idx"`
	TextSnip string `json:"text_snip"`
	Property string `json:"property"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// ActualParaFormat 段落的实际格式属性
type ActualParaFormat struct {
	FontEastAsia   string  `json:"font_east_asia"`
	FontAscii      string  `json:"font_ascii"`
	FontSizeHalfPt uint64  `json:"font_size_half_pt"`
	FontSizePt     float64 `json:"font_size_pt"`
	Bold           bool    `json:"bold"`
	Italic         bool    `json:"italic"`
	Alignment      string  `json:"alignment"`
	LineSpacing    int64   `json:"line_spacing"`
	LineRule       string  `json:"line_rule"`
	FirstIndent    int64   `json:"first_indent_twips"`
	SpaceBefore    int64   `json:"space_before_twips"`
	SpaceAfter     int64   `json:"space_after_twips"`
	PageBreak      bool    `json:"page_break_before"`
}

func NewFormatVerifier(proc *EnhancedProcessor, client *aiclassifier.DeepSeekWebClient) *FormatVerifier {
	return &FormatVerifier{processor: proc, client: client}
}

// VerifyAndFix 完整验证+修正流程
func (v *FormatVerifier) VerifyAndFix(doc *document.Document, rules map[string]interface{}) int {
	startTime := time.Now()
	log.Println("[格式验证] ═══════════ 开始全量格式验证 ═══════════")

	// 1. 重新分类段落
	classified := v.processor.classifyParagraphs(doc.Paragraphs())

	// 2. 提取实际格式并与规则比对
	diffs := v.compareAll(classified, rules)
	log.Printf("[格式验证] 发现 %d 处格式差异", len(diffs))

	if len(diffs) == 0 {
		log.Printf("[格式验证] 无差异，验证通过 (耗时 %v)", time.Since(startTime))
		return 0
	}

	// 3. 打印差异摘要
	v.logDiffSummary(diffs)

	// 4. 直接修正所有明确的差异（不需要 AI 判断的）
	directFixes := v.autoFixDiffs(classified, diffs, rules)
	log.Printf("[格式验证] 直接修正 %d 处", directFixes)

	// 5. 如果有 DeepSeek 客户端，对复杂/不确定的差异请求 AI 分析
	aiFixes := 0
	remainingDiffs := v.filterUnfixedDiffs(classified, rules)
	if len(remainingDiffs) > 0 && v.client != nil {
		aiFixes = v.deepseekAnalyzeAndFix(doc, classified, remainingDiffs, rules)
		log.Printf("[格式验证] DeepSeek 辅助修正 %d 处", aiFixes)
	}

	total := directFixes + aiFixes
	log.Printf("[格式验证] ═══════════ 验证完成：修正 %d 处 (耗时 %v) ═══════════", total, time.Since(startTime))
	return total
}

// compareAll 对每类段落提取实际格式并与预期规则比较
func (v *FormatVerifier) compareAll(classified map[string][]document.Paragraph, rules map[string]interface{}) []FormatDiff {
	var allDiffs []FormatDiff

	categoryRuleMap := v.buildCategoryRuleMap(rules)

	for category, paras := range classified {
		expectedRules, ok := categoryRuleMap[category]
		if !ok || expectedRules == nil {
			continue
		}
		for i, para := range paras {
			text := strings.TrimSpace(v.processor.extractParagraphText(para))
			if text == "" {
				continue
			}

			actual := v.extractActualFormat(para)
			diffs := v.compareOneParaWithRules(category, i, text, actual, expectedRules)
			allDiffs = append(allDiffs, diffs...)
		}
	}
	return allDiffs
}

// buildCategoryRuleMap 构建 类别→规则 的映射
func (v *FormatVerifier) buildCategoryRuleMap(rules map[string]interface{}) map[string]map[string]interface{} {
	m := make(map[string]map[string]interface{})

	// 正文
	if body, ok := rules["body"].(map[string]interface{}); ok {
		m["body"] = body
	}
	// 标题
	if title, ok := rules["title"].(map[string]interface{}); ok {
		m["title"] = title
	}
	// 各级标题
	for _, level := range []string{"1", "2", "3", "4"} {
		key := "heading_" + level
		if headings, ok := rules["headings"].(map[string]interface{}); ok {
			if h, ok := headings["level_"+level].(map[string]interface{}); ok {
				m[key] = h
			}
		}
	}
	// 参考文献标题
	for _, refKey := range []string{"references", "reference"} {
		if ref, ok := rules[refKey].(map[string]interface{}); ok {
			for _, sub := range []string{"label", "title"} {
				if t, ok := ref[sub].(map[string]interface{}); ok {
					m["references_title"] = t
					break
				}
			}
			if c, ok := ref["content"].(map[string]interface{}); ok {
				m["references"] = c
			} else if b, ok := ref["body"].(map[string]interface{}); ok {
				m["references"] = b
			}
			break
		}
	}
	// 致谢标题/内容
	if ack, ok := rules["acknowledgements"].(map[string]interface{}); ok {
		for _, sub := range []string{"label", "title"} {
			if t, ok := ack[sub].(map[string]interface{}); ok {
				m["acknowledgements_title"] = t
				break
			}
		}
		if c, ok := ack["content"].(map[string]interface{}); ok {
			m["acknowledgements_content"] = c
		}
	}
	// 摘要
	if abs, ok := rules["abstract"].(map[string]interface{}); ok {
		if content, ok := abs["content"].(map[string]interface{}); ok {
			m["abstract"] = content
		}
	}
	// 关键词
	if kw, ok := rules["keywords"].(map[string]interface{}); ok {
		if label, ok := kw["label"].(map[string]interface{}); ok {
			m["keywords"] = label
		}
	}

	return m
}

// extractActualFormat 从段落的 OOXML 中提取实际格式属性
func (v *FormatVerifier) extractActualFormat(para document.Paragraph) ActualParaFormat {
	af := ActualParaFormat{}

	// 从第一个非空 Run 提取字体/字号/加粗
	for _, run := range para.Runs() {
		if strings.TrimSpace(run.Text()) == "" {
			continue
		}
		rPr := run.X().RPr
		if rPr == nil {
			continue
		}
		if rPr.RFonts != nil {
			if rPr.RFonts.EastAsiaAttr != nil {
				af.FontEastAsia = *rPr.RFonts.EastAsiaAttr
			}
			if rPr.RFonts.AsciiAttr != nil {
				af.FontAscii = *rPr.RFonts.AsciiAttr
			}
		}
		if rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
			af.FontSizeHalfPt = *rPr.Sz.ValAttr.ST_UnsignedDecimalNumber
			af.FontSizePt = float64(af.FontSizeHalfPt) / 2.0
		}
		af.Bold = rPr.B != nil
		af.Italic = rPr.I != nil
		break
	}

	// 段落级属性
	pPr := para.X().PPr
	if pPr != nil {
		// 对齐
		if pPr.Jc != nil {
			switch pPr.Jc.ValAttr {
			case wml.ST_JcCenter:
				af.Alignment = "center"
			case wml.ST_JcLeft:
				af.Alignment = "left"
			case wml.ST_JcRight:
				af.Alignment = "right"
			case wml.ST_JcBoth:
				af.Alignment = "justify"
			}
		}
		// 行距
		if pPr.Spacing != nil {
			if pPr.Spacing.LineAttr != nil && pPr.Spacing.LineAttr.Int64 != nil {
				af.LineSpacing = *pPr.Spacing.LineAttr.Int64
			}
			switch pPr.Spacing.LineRuleAttr {
			case wml.ST_LineSpacingRuleExact:
				af.LineRule = "exact"
			case wml.ST_LineSpacingRuleAuto:
				af.LineRule = "auto"
			case wml.ST_LineSpacingRuleAtLeast:
				af.LineRule = "atLeast"
			}
			if pPr.Spacing.BeforeAttr != nil && pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber != nil {
				af.SpaceBefore = int64(*pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber)
			}
			if pPr.Spacing.AfterAttr != nil && pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber != nil {
				af.SpaceAfter = int64(*pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber)
			}
		}
		// 首行缩进
		if pPr.Ind != nil && pPr.Ind.FirstLineAttr != nil && pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber != nil {
			af.FirstIndent = int64(*pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber)
		}
		// 分页
		af.PageBreak = pPr.PageBreakBefore != nil
	}

	return af
}

// compareOneParaWithRules 比较一个段落的实际格式与规则，返回差异
func (v *FormatVerifier) compareOneParaWithRules(category string, idx int, text string, actual ActualParaFormat, expected map[string]interface{}) []FormatDiff {
	var diffs []FormatDiff
	snip := truncText(text, 25)

	// 字体
	if expFont, ok := expected["font_name"].(string); ok && expFont != "" {
		if actual.FontEastAsia != "" && actual.FontEastAsia != expFont {
			diffs = append(diffs, FormatDiff{category, idx, snip, "font_name", expFont, actual.FontEastAsia})
		}
	}

	// 字号
	expSizePt := v.processor.resolveActualFontSizePt(expected)
	if expSizePt > 0 && actual.FontSizePt > 0 {
		if absDiff(actual.FontSizePt, expSizePt) > 0.3 {
			diffs = append(diffs, FormatDiff{category, idx, snip, "font_size",
				fmt.Sprintf("%.1fpt", expSizePt), fmt.Sprintf("%.1fpt", actual.FontSizePt)})
		}
	}

	// 加粗
	if expBold, ok := expected["bold"].(bool); ok {
		if actual.Bold != expBold {
			diffs = append(diffs, FormatDiff{category, idx, snip, "bold",
				fmt.Sprintf("%v", expBold), fmt.Sprintf("%v", actual.Bold)})
		}
	}

	// 对齐
	if expAlign, ok := expected["alignment"].(string); ok && expAlign != "" {
		normalizedExp := expAlign
		if normalizedExp == "justify" {
			normalizedExp = "justify"
		}
		if actual.Alignment != "" && actual.Alignment != normalizedExp {
			diffs = append(diffs, FormatDiff{category, idx, snip, "alignment", normalizedExp, actual.Alignment})
		}
	}

	return diffs
}

// autoFixDiffs 直接修正差异
func (v *FormatVerifier) autoFixDiffs(classified map[string][]document.Paragraph, diffs []FormatDiff, rules map[string]interface{}) int {
	categoryRuleMap := v.buildCategoryRuleMap(rules)
	fixes := 0

	// 按 category 分组差异
	diffsByCategory := make(map[string][]FormatDiff)
	for _, d := range diffs {
		diffsByCategory[d.Category] = append(diffsByCategory[d.Category], d)
	}

	for category, categoryDiffs := range diffsByCategory {
		paras, exists := classified[category]
		if !exists {
			continue
		}
		expectedRules, ok := categoryRuleMap[category]
		if !ok || expectedRules == nil {
			continue
		}

		affectedIdxs := make(map[int]bool)
		for _, d := range categoryDiffs {
			affectedIdxs[d.ParaIdx] = true
		}

		for idx := range affectedIdxs {
			if idx >= len(paras) {
				continue
			}
			para := paras[idx]
			for _, run := range para.Runs() {
				if strings.TrimSpace(run.Text()) == "" {
					continue
				}
				v.processor.applyRunFormatting(run, expectedRules)
				fixes++
			}
		}
	}
	return fixes
}

// filterUnfixedDiffs 修正后再次检查，收集仍然不对的差异
func (v *FormatVerifier) filterUnfixedDiffs(classified map[string][]document.Paragraph, rules map[string]interface{}) []FormatDiff {
	return v.compareAll(classified, rules)
}

// deepseekAnalyzeAndFix 发送差异给 DeepSeek，获取分析和修正建议
func (v *FormatVerifier) deepseekAnalyzeAndFix(doc *document.Document, classified map[string][]document.Paragraph, diffs []FormatDiff, rules map[string]interface{}) int {
	if v.client == nil || len(diffs) == 0 {
		return 0
	}

	// 限制发送的差异数量
	maxDiffs := 30
	if len(diffs) > maxDiffs {
		diffs = diffs[:maxDiffs]
	}

	// 构建 prompt
	diffsJSON, _ := json.MarshalIndent(diffs, "", "  ")
	prompt := fmt.Sprintf(`你是一个论文格式检查专家。以下是修正后的论文仍然存在的格式差异列表。
每条差异包含：段落类型(category)、文本片段(text_snip)、属性名(property)、期望值(expected)、实际值(actual)。

格式差异列表：
%s

请分析这些差异，判断：
1. 哪些是真正的格式错误需要修正？
2. 哪些可能是误报（比如摘要标签段应该加粗但内容不应该加粗）？

请用 JSON 数组返回需要修正的差异编号（从0开始），格式：
{"fix_indices": [0, 1, 3]}

只返回 JSON，不要其他文字。`, string(diffsJSON))

	log.Printf("[DeepSeek验证] 发送 %d 条差异供分析...", len(diffs))
	resp, err := v.client.ChatCompletion(prompt)
	if err != nil {
		log.Printf("[DeepSeek验证] 调用失败: %v", err)
		return 0
	}

	// 解析响应
	type FixResponse struct {
		FixIndices []int `json:"fix_indices"`
	}

	// 提取 JSON
	jsonStr := extractJSON(resp)
	var fixResp FixResponse
	if err := json.Unmarshal([]byte(jsonStr), &fixResp); err != nil {
		log.Printf("[DeepSeek验证] 解析响应失败: %v, 响应: %s", err, truncText(resp, 200))
		return 0
	}

	// 对 AI 确认需要修正的差异，重新应用格式
	categoryRuleMap := v.buildCategoryRuleMap(rules)
	fixes := 0
	for _, idx := range fixResp.FixIndices {
		if idx < 0 || idx >= len(diffs) {
			continue
		}
		d := diffs[idx]
		paras, exists := classified[d.Category]
		if !exists || d.ParaIdx >= len(paras) {
			continue
		}
		expectedRules, ok := categoryRuleMap[d.Category]
		if !ok || expectedRules == nil {
			continue
		}
		para := paras[d.ParaIdx]
		for _, run := range para.Runs() {
			if strings.TrimSpace(run.Text()) == "" {
				continue
			}
			v.processor.applyRunFormatting(run, expectedRules)
		}
		fixes++
		log.Printf("[DeepSeek修正] %s #%d %q: %s %s→%s", d.Category, d.ParaIdx, d.TextSnip, d.Property, d.Actual, d.Expected)
	}

	return fixes
}

// logDiffSummary 打印差异摘要
func (v *FormatVerifier) logDiffSummary(diffs []FormatDiff) {
	byCat := make(map[string]int)
	byProp := make(map[string]int)
	for _, d := range diffs {
		byCat[d.Category]++
		byProp[d.Property]++
	}
	log.Printf("[格式验证] 差异按类别: %v", byCat)
	log.Printf("[格式验证] 差异按属性: %v", byProp)

	// 打印前 10 条详细差异
	limit := 10
	if len(diffs) < limit {
		limit = len(diffs)
	}
	for i := 0; i < limit; i++ {
		d := diffs[i]
		log.Printf("[格式验证]   [%s] #%d %q: %s 期望=%s 实际=%s",
			d.Category, d.ParaIdx, d.TextSnip, d.Property, d.Expected, d.Actual)
	}
	if len(diffs) > limit {
		log.Printf("[格式验证]   ...还有 %d 条差异", len(diffs)-limit)
	}
}

// ════════════════════════════════════════════════════════════════════════
// 基于 ParagraphFormatSpec 的高精度验证（模板直读方案）
// ════════════════════════════════════════════════════════════════════════

// VerifyAndFixWithSpecs 使用模板格式规范做验证和修正（高精度，无JSON解析误差）
func (v *FormatVerifier) VerifyAndFixWithSpecs(doc *document.Document, specs map[string]ParagraphFormatSpec) int {
	startTime := time.Now()
	log.Println("[格式验证] ═══════════ 开始全量格式验证（模板规范模式）═══════════")

	// 重新分类段落
	classified := v.processor.classifyParagraphs(doc.Paragraphs())

	// 提取实际格式并与模板规范比对
	diffs := v.compareAllWithSpecs(classified, specs)
	log.Printf("[格式验证] 发现 %d 处格式差异", len(diffs))

	if len(diffs) == 0 {
		log.Printf("[格式验证] 无差异，验证通过 (耗时 %v)", time.Since(startTime))
		return 0
	}

	// 打印差异摘要
	v.logDiffSummary(diffs)

	// 直接修正：按模板规范重新应用格式
	applier := NewAIFormatApplier(v.processor)
	fixes := v.autoFixDiffsWithSpecs(classified, diffs, specs, applier)
	log.Printf("[格式验证] 直接修正 %d 处", fixes)

	// 二次验证：检查是否还有残留差异
	remaining := v.compareAllWithSpecs(classified, specs)
	if len(remaining) > 0 {
		log.Printf("[格式验证] 残留 %d 处差异", len(remaining))
		// 如有 DeepSeek 客户端，发送残留差异供分析
		if v.client != nil {
			aiFixes := v.deepseekAnalyzeAndFixWithSpecs(classified, remaining, specs, applier)
			if aiFixes > 0 {
				log.Printf("[格式验证] DeepSeek 辅助修正 %d 处", aiFixes)
				fixes += aiFixes
			}
		}
	}

	log.Printf("[格式验证] ═══════════ 验证完成：修正 %d 处 (耗时 %v) ═══════════",
		fixes, time.Since(startTime))
	return fixes
}

// compareAllWithSpecs 用模板规范对每类段落比对实际格式
func (v *FormatVerifier) compareAllWithSpecs(classified map[string][]document.Paragraph, specs map[string]ParagraphFormatSpec) []FormatDiff {
	var allDiffs []FormatDiff
	for category, paras := range classified {
		if isProtectedFormatCategory(category) {
			continue
		}
		spec, ok := specs[category]
		if !ok || spec.IsEmpty() {
			continue
		}
		for i, para := range paras {
			text := strings.TrimSpace(v.processor.extractParagraphText(para))
			if text == "" {
				continue
			}
			actual := v.extractActualFormat(para)
			diffs := v.compareWithSpec(category, i, text, actual, spec)
			allDiffs = append(allDiffs, diffs...)
		}
	}
	return allDiffs
}

// compareWithSpec 比较一个段落的实际格式与模板规范，返回差异列表
func (v *FormatVerifier) compareWithSpec(category string, idx int, text string, actual ActualParaFormat, spec ParagraphFormatSpec) []FormatDiff {
	var diffs []FormatDiff
	snip := truncText(text, 25)

	// 字体（只在实际字体与规范字体均有效时比较）
	if spec.FontEastAsia != "" && actual.FontEastAsia != "" {
		if actual.FontEastAsia != spec.FontEastAsia {
			diffs = append(diffs, FormatDiff{category, idx, snip, "font_name",
				spec.FontEastAsia, actual.FontEastAsia})
		}
	}

	// 字号（允许0.3pt的误差）
	expSizePt := spec.FontSizePt()
	if expSizePt > 0 && actual.FontSizePt > 0 {
		if absDiff(actual.FontSizePt, expSizePt) > 0.3 {
			diffs = append(diffs, FormatDiff{category, idx, snip, "font_size",
				fmt.Sprintf("%.1fpt", expSizePt), fmt.Sprintf("%.1fpt", actual.FontSizePt)})
		}
	}

	// 加粗（只在规范有足够样本时检查）
	if spec.SampleCount >= 3 {
		if actual.Bold != spec.Bold {
			diffs = append(diffs, FormatDiff{category, idx, snip, "bold",
				fmt.Sprintf("%v", spec.Bold), fmt.Sprintf("%v", actual.Bold)})
		}
	}

	// 对齐
	if spec.AlignmentSet && actual.Alignment != "" {
		specAlignStr := jcToAlignString(spec.Alignment)
		if actual.Alignment != specAlignStr {
			diffs = append(diffs, FormatDiff{category, idx, snip, "alignment",
				specAlignStr, actual.Alignment})
		}
	}

	return diffs
}

// autoFixDiffsWithSpecs 按模板规范直接修正差异段落
func (v *FormatVerifier) autoFixDiffsWithSpecs(
	classified map[string][]document.Paragraph,
	diffs []FormatDiff,
	specs map[string]ParagraphFormatSpec,
	applier *AIFormatApplier,
) int {
	fixes := 0
	// 按类别收集需要修正的段落索引
	affectedByCategory := make(map[string]map[int]bool)
	for _, d := range diffs {
		if affectedByCategory[d.Category] == nil {
			affectedByCategory[d.Category] = make(map[int]bool)
		}
		affectedByCategory[d.Category][d.ParaIdx] = true
	}

	for category, idxSet := range affectedByCategory {
		paras, exists := classified[category]
		if !exists {
			continue
		}
		spec, ok := specs[category]
		if !ok || spec.IsEmpty() {
			continue
		}
		for idx := range idxSet {
			if idx >= len(paras) {
				continue
			}
			applier.ApplySpecToPara(paras[idx], spec)
			fixes++
			log.Printf("[格式验证修正] %s #%d 已按模板规范重新应用格式", category, idx)
		}
	}
	return fixes
}

// deepseekAnalyzeAndFixWithSpecs 将残留差异发给DeepSeek，AI确认后按模板规范修正
func (v *FormatVerifier) deepseekAnalyzeAndFixWithSpecs(
	classified map[string][]document.Paragraph,
	diffs []FormatDiff,
	specs map[string]ParagraphFormatSpec,
	applier *AIFormatApplier,
) int {
	if v.client == nil || len(diffs) == 0 {
		return 0
	}

	maxDiffs := 30
	if len(diffs) > maxDiffs {
		diffs = diffs[:maxDiffs]
	}

	diffsJSON, _ := json.MarshalIndent(diffs, "", "  ")
	prompt := fmt.Sprintf(`你是论文格式检查专家。以下是修正后仍存在的格式差异（来自模板精确对比）。
每条差异：category(段落类型)、text_snip(文本片段)、property(属性)、expected(期望)、actual(实际)。

差异列表：
%s

请判断哪些是真正需要修正的错误（排除误报，如摘要"标签"加粗但"内容"不需要加粗）。
只返回JSON，格式：{"fix_indices": [0, 1, 3]}`, string(diffsJSON))

	log.Printf("[DeepSeek验证] 发送 %d 条残留差异供分析", len(diffs))
	resp, err := v.client.ChatCompletion(prompt)
	if err != nil {
		log.Printf("[DeepSeek验证] 调用失败: %v", err)
		return 0
	}

	type FixResponse struct {
		FixIndices []int `json:"fix_indices"`
	}
	var fixResp FixResponse
	if err := json.Unmarshal([]byte(extractJSON(resp)), &fixResp); err != nil {
		log.Printf("[DeepSeek验证] 解析响应失败: %v", err)
		return 0
	}

	fixes := 0
	for _, idx := range fixResp.FixIndices {
		if idx < 0 || idx >= len(diffs) {
			continue
		}
		d := diffs[idx]
		paras, exists := classified[d.Category]
		if !exists || d.ParaIdx >= len(paras) {
			continue
		}
		spec, ok := specs[d.Category]
		if !ok || spec.IsEmpty() {
			continue
		}
		applier.ApplySpecToPara(paras[d.ParaIdx], spec)
		fixes++
		log.Printf("[DeepSeek修正] %s #%d %q: %s %s→%s",
			d.Category, d.ParaIdx, d.TextSnip, d.Property, d.Actual, d.Expected)
	}
	return fixes
}

// extractJSON 从 AI 响应中提取 JSON
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return "{}"
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
