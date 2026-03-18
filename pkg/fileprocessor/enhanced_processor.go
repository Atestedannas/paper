package fileprocessor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
	"github.com/paper-format-checker/backend/pkg/aiclassifier"
)

// EnhancedProcessor 增强的文档处理器，提供更精确的格式修正
type EnhancedProcessor struct {
	debug           bool
	fontNameMap     map[string]*string            // 缓存字体名，避免悬空指针
	smartClassifier *aiclassifier.SmartClassifier // 智能段落分类器（三级路由）
}

// NewEnhancedProcessor 创建增强处理器
func NewEnhancedProcessor() *EnhancedProcessor {
	log.Println("========================================")
	log.Println("🚀 使用增强处理器 v2.0 - 支持中文键名")
	log.Println("========================================")
	return &EnhancedProcessor{
		debug:       true,
		fontNameMap: make(map[string]*string),
	}
}

// SetSmartClassifier 注入智能分类器
func (p *EnhancedProcessor) SetSmartClassifier(sc *aiclassifier.SmartClassifier) {
	p.smartClassifier = sc
}

// getCachedFontName 获取缓存的字体名指针
func (p *EnhancedProcessor) getCachedFontName(fontName string) *string {
	if ptr, ok := p.fontNameMap[fontName]; ok {
		return ptr
	}
	fontNameCopy := fontName
	p.fontNameMap[fontName] = &fontNameCopy
	return &fontNameCopy
}

// fixme   normalizeFormatRules 规范化格式规则，支持中文和英文键名
func (p *EnhancedProcessor) normalizeFormatRules(rules map[string]interface{}) map[string]interface{} {
	normalized := make(map[string]interface{})

	// 顶层键名映射
	topLevelKeyMap := map[string]string{
		"正文格式":   "body",
		"标题格式":   "title",
		"作者格式":   "author",
		"摘要格式":   "abstract",
		"英文摘要格式": "english_abstract",
		"关键词格式":  "keywords",
		"标题层级格式": "headings",
		"参考文献格式": "references",
		"页面设置":   "page_setup",
		"附录":     "appendix",
		"致谢":     "acknowledgements",
		"目录":     "table_of_contents",
	}

	// 格式参数键名映射
	paramKeyMap := map[string]string{
		"字号":    "font_size",
		"字体名称":  "font_name",
		"行间距":   "line_space",
		"对齐方式":  "alignment",
		"首行缩进":  "first_line_indent",
		"段落间距":  "paragraph_space",
		"段前间距":  "before",
		"段后间距":  "after",
		"是否加粗":  "bold",
		"是否斜体":  "italic",
		"编号格式":  "numbering",
		"分隔符":   "separator",
		"无结尾标点": "no_end_punctuation",
		"文本内容":  "text",
		"标签格式":  "label",
		"内容格式":  "content",
		"一级标题":  "level1",
		"二级标题":  "level2",
		"三级标题":  "level3",
		"上边距":   "top",
		"下边距":   "bottom",
		"左边距":   "left",
		"右边距":   "right",
		"纸张大小":  "paper_size",
		"页面方向":  "orientation",
		"页眉":    "header",
		"页脚":    "footer",
		"距离":    "distance",
	}

	// 递归规范化
	for key, value := range rules {
		// 映射顶层键名
		normalizedKey := key
		if mappedKey, ok := topLevelKeyMap[key]; ok {
			normalizedKey = mappedKey
		}

		// 如果值是map，递归处理
		if valueMap, ok := value.(map[string]interface{}); ok {
			normalizedValue := make(map[string]interface{})
			for subKey, subValue := range valueMap {
				// 映射参数键名
				normalizedSubKey := subKey
				if mappedSubKey, ok := paramKeyMap[subKey]; ok {
					normalizedSubKey = mappedSubKey
				}

				// 如果子值也是map，继续递归
				if subValueMap, ok := subValue.(map[string]interface{}); ok {
					normalizedSubValue := make(map[string]interface{})
					for k, v := range subValueMap {
						if mappedK, ok := paramKeyMap[k]; ok {
							normalizedSubValue[mappedK] = v
						} else {
							normalizedSubValue[k] = v
						}
					}
					normalizedValue[normalizedSubKey] = normalizedSubValue
				} else {
					normalizedValue[normalizedSubKey] = subValue
				}
			}
			normalized[normalizedKey] = normalizedValue
		} else {
			normalized[normalizedKey] = value
		}
	}

	return normalized
}

// ApplyCorrections 应用修正（增强版）
func (p *EnhancedProcessor) ApplyCorrections(ctx context.Context, docPath string, corrections []map[string]interface{}) (string, error) {

	log.Println("================= 格式修正流程 开始 =================")
	log.Printf("[入口] 文件: %s, corrections 数量: %d", docPath, len(corrections))

	if len(corrections) == 0 {
		log.Println("[入口] ⚠️  没有修正规则，返回原文件")
		log.Println("++++++++++++ 格式修正流程 结束（无规则） ++++++++++++")
		return docPath, nil
	}

	// 检查文件类型
	fileExt := strings.ToLower(filepath.Ext(docPath))
	if fileExt != ".docx" && fileExt != ".doc" {
		log.Printf("[入口] 不支持的文件格式: %s", fileExt)
		log.Println("++++++++++++ 格式修正流程 结束（格式不支持） ++++++++++++")
		return p.handleUnsupportedFormat(docPath)
	}

	// 提取格式规则
	log.Println("[规则提取] 开始从 corrections 中提取 format_rules...")
	var formatRules map[string]interface{}
	for i, correction := range corrections {
		if rules, ok := correction["format_rules"]; ok {
			if rulesMap, ok := rules.(map[string]interface{}); ok {
				formatRules = rulesMap
				log.Printf("[规则提取] 在 corrections[%d] 找到 format_rules，顶层键: %v", i, getMapKeys(rulesMap))
				break
			} else {
				log.Printf("[规则提取] corrections[%d] 有 format_rules 但类型不是 map[string]interface{}, 实际类型: %T", i, rules)
			}
		}
	}

	if formatRules == nil {
		log.Println("[规则提取] ❌ 所有 corrections 中都未找到有效的 format_rules")
		log.Println("++++++++++++ 格式修正流程 结束（无规则） ++++++++++++")
		return "", fmt.Errorf("未找到有效的格式规则")
	}

	// 规范化格式规则（支持中文键名）
	log.Println("[规范化] 开始规范化格式规则...")
	formatRules = p.normalizeFormatRules(formatRules)
	log.Printf("[规范化] 规范化后顶层键: %v", getMapKeys(formatRules))

	if bodyRules, ok := formatRules["body"].(map[string]interface{}); ok {
		log.Printf("[规范化] body 规则: %v", bodyRules)
	} else {
		log.Println("[规范化] ⚠️  规范化后未找到 body 规则！")
	}
	if headings, ok := formatRules["headings"].(map[string]interface{}); ok {
		log.Printf("[规范化] headings 规则键: %v", getMapKeys(headings))
		for _, lk := range []string{"level1", "level2", "level3"} {
			if lv, ok := headings[lk].(map[string]interface{}); ok {
				log.Printf("[规范化]   headings.%s: %v", lk, lv)
			}
		}
	}
	if abstractRules, ok := formatRules["abstract"].(map[string]interface{}); ok {
		log.Printf("[规范化] abstract 完整结构: %v", abstractRules)
	}
	if eaRules, ok := formatRules["english_abstract"].(map[string]interface{}); ok {
		log.Printf("[规范化] english_abstract 完整结构: %v", eaRules)
	}
	if kwRules, ok := formatRules["keywords"].(map[string]interface{}); ok {
		log.Printf("[规范化] keywords 完整结构: %v", kwRules)
	}
	if tocRules, ok := formatRules["table_of_contents"].(map[string]interface{}); ok {
		log.Printf("[规范化] table_of_contents 完整结构: %v", tocRules)
	}

	// 测试 parseFontSize
	if bodyRules, ok := formatRules["body"].(map[string]interface{}); ok {
		if fs, ok := bodyRules["font_size"].(string); ok {
			parsed := p.parseFontSize(fs)
			log.Printf("[字号解析测试] parseFontSize(%q) = %.1f pt", fs, parsed)
		} else if fsf, ok := bodyRules["font_size"].(float64); ok {
			log.Printf("[字号解析测试] font_size 是 float64: %.1f pt", fsf)
		} else {
			log.Printf("[字号解析测试] font_size 类型: %T, 值: %v", bodyRules["font_size"], bodyRules["font_size"])
		}
	}

	// 打开文档
	log.Printf("[文档打开] 正在打开: %s", docPath)
	doc, err := document.Open(docPath)
	if err != nil {
		log.Printf("[文档打开] ❌ 失败: %v", err)
		log.Println("++++++++++++ 格式修正流程 结束（打开失败） ++++++++++++")
		return "", fmt.Errorf("无法打开文档: %w", err)
	}
	defer doc.Close()
	log.Printf("[文档打开] ✅ 成功，段落总数: %d", len(doc.Paragraphs()))

	// 执行精确格式修正
	log.Println("[格式修正] ================= 开始应用格式 =================")
	if err := p.applyPreciseFormatting(doc, formatRules); err != nil {
		log.Printf("[格式修正] ❌ 失败: %v", err)
		log.Println("++++++++++++ 格式修正流程 结束（修正失败） ++++++++++++")
		return "", fmt.Errorf("格式修正失败: %w", err)
	}
	log.Println("[格式修正] ================= 格式应用完成 =================")

	// 验证：输出修正后前5个正文段落的实际格式
	p.verifyFormattingResults(doc)

	// 生成输出文件路径
	outputPath := p.generateOutputPath(docPath)

	// 保存修正后的文档
	log.Printf("[保存] 正在保存到: %s", outputPath)
	if err := doc.SaveToFile(outputPath); err != nil {
		log.Printf("[保存] ❌ 失败: %v", err)
		log.Println("++++++++++++ 格式修正流程 结束（保存失败） ++++++++++++")
		return "", fmt.Errorf("保存文档失败: %w", err)
	}

	log.Printf("[保存] ✅ 文档保存成功: %s", outputPath)
	log.Println("++++++++++++ 格式修正流程 结束（成功） ++++++++++++")

	return outputPath, nil
}

// applyPreciseFormatting 应用精确格式修正
func (p *EnhancedProcessor) applyPreciseFormatting(doc *document.Document, rules map[string]interface{}) error {

	// 步骤 0: 修改样式定义
	log.Println("[步骤0] ---- 修改 styles.xml 样式定义 ----")
	p.applyStyleDefinitions(doc, rules)
	log.Println("[步骤0] ---- 样式定义修改完成 ----")

	// 步骤 1: 页面设置
	log.Println("[步骤1] ---- 应用页面设置 ----")
	if err := p.applyPageSetup(doc, rules); err != nil {
		log.Printf("[步骤1] ⚠️  页面设置失败: %v", err)
	}

	// 步骤 1b: 页眉页脚 & 页码
	log.Println("[步骤1b] ---- 应用页眉页脚和页码 ----")
	if err := p.applyHeaderFooter(doc, rules); err != nil {
		log.Printf("[步骤1b] ⚠️  页眉页脚设置失败: %v", err)
	}

	// 步骤 2: 段落分类
	log.Println("[步骤2] ---- 段落分类 ----")
	paragraphs := doc.Paragraphs()
	log.Printf("[步骤2] 文档总段落数（含空段落）: %d", len(paragraphs))
	classifiedParagraphs := p.classifyParagraphs(paragraphs)

	// 输出分类详情
	for category, paras := range classifiedParagraphs {
		sampleText := ""
		if len(paras) > 0 {
			t := p.extractParagraphText(paras[0])
			if len(t) > 50 {
				t = t[:50] + "..."
			}
			sampleText = t
		}
		log.Printf("[步骤2]   %s: %d 个段落, 首段: %q", category, len(paras), sampleText)
	}

	// 步骤 3: 论文标题
	if titleParas, exists := classifiedParagraphs["title"]; exists && len(titleParas) > 0 {
		log.Printf("[步骤3] ---- 应用论文标题格式 (%d段) ----", len(titleParas))
		p.applyTitleFormatting(titleParas, rules)
	}

	// 步骤 4: 各级标题
	for level := 1; level <= 3; level++ {
		key := fmt.Sprintf("heading_%d", level)
		if headings, exists := classifiedParagraphs[key]; exists && len(headings) > 0 {
			log.Printf("[步骤4] ---- 应用 %s 格式 (%d段) ----", key, len(headings))
			p.applyHeadingFormatting(headings, rules, level)
		}
	}

	// 步骤 4b: 目录标题
	if tocTitleParas, exists := classifiedParagraphs["table_of_contents_title"]; exists && len(tocTitleParas) > 0 {
		log.Printf("[步骤4b] ---- 应用目录标题格式 (%d段) ----", len(tocTitleParas))
		p.applyTOCTitleFormatting(tocTitleParas, rules)
	}
	// 步骤 4c: 目录条目
	if tocParas, exists := classifiedParagraphs["table_of_contents"]; exists && len(tocParas) > 0 {
		log.Printf("[步骤4c] ---- 应用目录条目格式 (%d段) ----", len(tocParas))
		p.applyTOCEntryFormatting(tocParas, rules)
	}

	// 步骤 5: 正文
	if bodyParas, exists := classifiedParagraphs["body"]; exists && len(bodyParas) > 0 {
		log.Printf("[步骤5] ---- 应用正文格式 (%d段) ----", len(bodyParas))
		if err := p.applyBodyFormatting(bodyParas, rules); err != nil {
			log.Printf("[步骤5] ⚠️  正文格式应用失败: %v", err)
		}
		log.Println("[步骤5] ---- 正文格式完成 ----")
	} else {
		log.Println("[步骤5] ⚠️  未找到 body 段落！")
	}

	// 步骤 6: 摘要
	if abstractTitleParas, exists := classifiedParagraphs["abstract_title"]; exists && len(abstractTitleParas) > 0 {
		log.Printf("[步骤6a] ---- 中文摘要标题 (%d段) ----", len(abstractTitleParas))
		p.applyAbstractTitleFormatting(abstractTitleParas, rules)
	}
	if abstractParas, exists := classifiedParagraphs["abstract"]; exists && len(abstractParas) > 0 {
		log.Printf("[步骤6b] ---- 中文摘要正文 (%d段) ----", len(abstractParas))
		p.applyAbstractFormatting(abstractParas, rules)
	}
	if enTitleParas, exists := classifiedParagraphs["en_abstract_title"]; exists && len(enTitleParas) > 0 {
		log.Printf("[步骤6c] ---- 英文摘要标题 (%d段) ----", len(enTitleParas))
		p.applyEnglishAbstractTitleFormatting(enTitleParas, rules)
	}
	if enAbstractParas, exists := classifiedParagraphs["en_abstract"]; exists && len(enAbstractParas) > 0 {
		log.Printf("[步骤6d] ---- 英文摘要正文 (%d段) ----", len(enAbstractParas))
		p.applyEnglishAbstractFormatting(enAbstractParas, rules)
	}

	// 步骤 7: 关键词
	if keywordsParas, exists := classifiedParagraphs["keywords"]; exists && len(keywordsParas) > 0 {
		log.Printf("[步骤7a] ---- 中文关键词 (%d段) ----", len(keywordsParas))
		p.applyKeywordsFormatting(keywordsParas, rules)
	}
	if enKwParas, exists := classifiedParagraphs["en_keywords"]; exists && len(enKwParas) > 0 {
		log.Printf("[步骤7b] ---- 英文关键词 (%d段) ----", len(enKwParas))
		p.applyEnglishKeywordsFormatting(enKwParas, rules)
	}

	// 步骤 8a: 参考文献标题（"参考文献" 四个字）
	if refTitleParas, exists := classifiedParagraphs["references_title"]; exists && len(refTitleParas) > 0 {
		log.Printf("[步骤8a] ---- 参考文献标题 (%d段) ----", len(refTitleParas))
		p.applyReferencesTitleFormatting(refTitleParas, rules)
	}
	// 步骤 8b: 参考文献条目
	if referencesParas, exists := classifiedParagraphs["references"]; exists && len(referencesParas) > 0 {
		log.Printf("[步骤8b] ---- 参考文献条目 (%d段) ----", len(referencesParas))
		p.applyReferencesFormatting(referencesParas, rules)
	}

	// 步骤 9a: 致谢标题
	if ackTitleParas, exists := classifiedParagraphs["acknowledgements_title"]; exists && len(ackTitleParas) > 0 {
		log.Printf("[步骤9a] ---- 致谢标题 (%d段) ----", len(ackTitleParas))
		p.applyAcknowledgementsTitleFormatting(ackTitleParas, rules)
	}
	// 步骤 9b: 致谢正文
	if ackContentParas, exists := classifiedParagraphs["acknowledgements_content"]; exists && len(ackContentParas) > 0 {
		log.Printf("[步骤9b] ---- 致谢正文 (%d段) ----", len(ackContentParas))
		p.applyAcknowledgementsContentFormatting(ackContentParas, rules)
	}

	// 步骤 10: 封面 & 原创性声明跳过（不修改格式）
	if coverParas, exists := classifiedParagraphs["cover"]; exists && len(coverParas) > 0 {
		log.Printf("[步骤9] 跳过封面段落: %d 段（保持原样）", len(coverParas))
	}
	if origParas, exists := classifiedParagraphs["originality_declaration"]; exists && len(origParas) > 0 {
		log.Printf("[步骤9] 跳过原创性声明段落: %d 段（保持原样）", len(origParas))
	}

	// 步骤 10: 表格
	log.Println("[步骤10] ---- 应用表格内格式 ----")
	p.applyTableFormatting(doc, rules)

	return nil
}

// ═══════════════════════════════════════════════════════════════════════
// applyStyleDefinitions 修改文档的 styles.xml，从根本上控制格式
//
// Word 格式优先级链（从低到高）：
//
//	Layer 0: docDefaults（文档默认）
//	Layer 1: Named Style（如 Normal / Heading 1）
//	Layer 2: 段落级默认 rPr（pPr/rPr）
//	Layer 3: Run 级直接格式（rPr）
//
// 之前我们只做了 Layer 2+3，但 Layer 0+1 才是 Word 真正的格式根基。
// 本函数负责修改 Layer 0 和 Layer 1，让样式定义本身就符合格式要求。
// ═══════════════════════════════════════════════════════════════════════
func (p *EnhancedProcessor) applyStyleDefinitions(doc *document.Document, rules map[string]interface{}) {
	styles := doc.Styles.X()
	if styles == nil {
		log.Println("[样式修改] 文档没有 styles.xml，跳过样式修改")
		return
	}

	// ── Layer 0: 修改 docDefaults ──────────────────────────────────────
	bodyRules, _ := rules["body"].(map[string]interface{})
	if bodyRules != nil {
		p.applyDocDefaults(styles, bodyRules)
	}

	// ── Layer 1: 修改命名样式 ──────────────────────────────────────────
	for _, style := range styles.Style {
		if style.StyleIdAttr == nil {
			continue
		}
		styleID := *style.StyleIdAttr
		styleName := ""
		if style.Name != nil {
			styleName = style.Name.ValAttr
		}

		switch {
		case styleID == "Normal" || styleName == "Normal" || styleName == "正文":
			if bodyRules != nil {
				log.Printf("[样式修改] 修改 Normal 样式: %v", bodyRules["font_name"])
				p.applyStyleRPr(style, bodyRules)
				p.applyStylePPr(style, bodyRules)
			}
		case styleID == "Heading1" || styleID == "heading 1" || styleName == "heading 1" || styleName == "标题 1":
			if headingRules := p.getHeadingRules(rules, 1); headingRules != nil {
				log.Printf("[样式修改] 修改 Heading1 样式")
				p.applyStyleRPr(style, headingRules)
				p.applyStylePPr(style, headingRules)
			}
		case styleID == "Heading2" || styleID == "heading 2" || styleName == "heading 2" || styleName == "标题 2":
			if headingRules := p.getHeadingRules(rules, 2); headingRules != nil {
				log.Printf("[样式修改] 修改 Heading2 样式")
				p.applyStyleRPr(style, headingRules)
				p.applyStylePPr(style, headingRules)
			}
		case styleID == "Heading3" || styleID == "heading 3" || styleName == "heading 3" || styleName == "标题 3":
			if headingRules := p.getHeadingRules(rules, 3); headingRules != nil {
				log.Printf("[样式修改] 修改 Heading3 样式")
				p.applyStyleRPr(style, headingRules)
				p.applyStylePPr(style, headingRules)
			}
		}
	}
}

// applyDocDefaults 修改文档的 docDefaults 默认格式
func (p *EnhancedProcessor) applyDocDefaults(styles *wml.Styles, bodyRules map[string]interface{}) {
	if styles.DocDefaults == nil {
		styles.DocDefaults = wml.NewCT_DocDefaults()
	}
	dd := styles.DocDefaults

	// 默认 Run 属性（字体+字号）
	if dd.RPrDefault == nil {
		dd.RPrDefault = wml.NewCT_RPrDefault()
	}
	if dd.RPrDefault.RPr == nil {
		dd.RPrDefault.RPr = wml.NewCT_RPr()
	}
	rPr := dd.RPrDefault.RPr

	if fontName, ok := bodyRules["font_name"].(string); ok {
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		ptr := p.getCachedFontName(fontName)
		rPr.RFonts.EastAsiaAttr = ptr
		rPr.RFonts.AsciiAttr = ptr
		rPr.RFonts.HAnsiAttr = ptr
		rPr.RFonts.CsAttr = ptr
		log.Printf("[样式修改] docDefaults 字体 → %s", fontName)
	}

	fontSizePt := p.resolveActualFontSizePt(bodyRules)
	if fontSizePt > 0 {
		halfPt := uint64(fontSizePt * 2)
		rPr.Sz = wml.NewCT_HpsMeasure()
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		rPr.SzCs = wml.NewCT_HpsMeasure()
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		log.Printf("[样式修改] docDefaults 字号 → %.1fpt (half-pt=%d)", fontSizePt, halfPt)
	}

	// 默认段落属性（行距+缩进+对齐）
	if dd.PPrDefault == nil {
		dd.PPrDefault = wml.NewCT_PPrDefault()
	}
	if dd.PPrDefault.PPr == nil {
		dd.PPrDefault.PPr = wml.NewCT_PPrGeneral()
	}
	pPr := dd.PPrDefault.PPr

	if alignment, ok := bodyRules["alignment"].(string); ok {
		if pPr.Jc == nil {
			pPr.Jc = wml.NewCT_Jc()
		}
		switch alignment {
		case "justify":
			pPr.Jc.ValAttr = wml.ST_JcBoth
		case "center":
			pPr.Jc.ValAttr = wml.ST_JcCenter
		case "left":
			pPr.Jc.ValAttr = wml.ST_JcLeft
		case "right":
			pPr.Jc.ValAttr = wml.ST_JcRight
		}
	}
}

// applyStyleRPr 修改命名样式的 Run 属性（字体、字号、加粗等）
func (p *EnhancedProcessor) applyStyleRPr(style *wml.CT_Style, rules map[string]interface{}) {
	if style.RPr == nil {
		style.RPr = wml.NewCT_RPr()
	}
	rPr := style.RPr

	if fontName, ok := rules["font_name"].(string); ok {
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		ptr := p.getCachedFontName(fontName)
		rPr.RFonts.EastAsiaAttr = ptr
		rPr.RFonts.AsciiAttr = ptr
		rPr.RFonts.HAnsiAttr = ptr
		rPr.RFonts.CsAttr = ptr
	}

	fontSizePt := p.resolveActualFontSizePt(rules)
	if fontSizePt > 0 {
		halfPt := uint64(fontSizePt * 2)
		rPr.Sz = wml.NewCT_HpsMeasure()
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		rPr.SzCs = wml.NewCT_HpsMeasure()
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	}

	if bold, ok := rules["bold"].(bool); ok {
		if bold {
			rPr.B = wml.NewCT_OnOff()
			rPr.BCs = wml.NewCT_OnOff()
		} else {
			rPr.B = nil
			rPr.BCs = nil
		}
	}
}

// applyStylePPr 修改命名样式的段落属性（对齐、行距、缩进）
func (p *EnhancedProcessor) applyStylePPr(style *wml.CT_Style, rules map[string]interface{}) {
	if style.PPr == nil {
		style.PPr = wml.NewCT_PPrGeneral()
	}
	pPr := style.PPr

	// 对齐
	if alignment, ok := rules["alignment"].(string); ok {
		if pPr.Jc == nil {
			pPr.Jc = wml.NewCT_Jc()
		}
		switch alignment {
		case "justify":
			pPr.Jc.ValAttr = wml.ST_JcBoth
		case "center":
			pPr.Jc.ValAttr = wml.ST_JcCenter
		case "left":
			pPr.Jc.ValAttr = wml.ST_JcLeft
		case "right":
			pPr.Jc.ValAttr = wml.ST_JcRight
		}
	}

	// 行距
	if lineSpace, ok := rules["line_space"].(string); ok && lineSpace == "fixed" {
		if lineSpaceValue, ok := rules["line_space_value"].(float64); ok && lineSpaceValue > 0 {
			if pPr.Spacing == nil {
				pPr.Spacing = wml.NewCT_Spacing()
			}
			twips := int64(lineSpaceValue * 20)
			if pPr.Spacing.LineAttr == nil {
				pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
			}
			pPr.Spacing.LineAttr.Int64 = &twips
			pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleExact
		}
	} else if lineSpaceRaw, ok := rules["line_space"]; ok {
		var val float64
		switch v := lineSpaceRaw.(type) {
		case float64:
			val = v
		case string:
			val, _ = strconv.ParseFloat(v, 64)
		}
		if val > 0 && val <= 10 {
			if pPr.Spacing == nil {
				pPr.Spacing = wml.NewCT_Spacing()
			}
			twips := int64(val * 240) // 多倍行距：1倍 = 240 twips
			if pPr.Spacing.LineAttr == nil {
				pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
			}
			pPr.Spacing.LineAttr.Int64 = &twips
			pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto
		}
	}

	// 首行缩进（使用字符单位，Word 原生支持）
	if fli := rules["first_line_indent"]; fli != nil {
		var indentChars float64
		switch v := fli.(type) {
		case float64:
			indentChars = v
		case string:
			indentChars = p.parseFirstLineIndentChars(v)
		}
		if indentChars > 0 {
			if pPr.Ind == nil {
				pPr.Ind = wml.NewCT_Ind()
			}
			// FirstLineCharsAttr 单位为百分之一字符（100 = 1字符）
			chars := int64(indentChars * 100)
			pPr.Ind.FirstLineCharsAttr = &chars
		}
	}

	// 段前/段后间距（使用行数单位，避免 sharedTypes 类型问题）
	if before, ok := rules["spacing_before"].(float64); ok && before > 0 {
		if pPr.Spacing == nil {
			pPr.Spacing = wml.NewCT_Spacing()
		}
		// BeforeLinesAttr 单位为 1/100 行
		lines := int64(before * 100 / 12) // 粗略转换：磅值 / 12pt ≈ 行数
		pPr.Spacing.BeforeLinesAttr = &lines
	}
	if after, ok := rules["spacing_after"].(float64); ok && after > 0 {
		if pPr.Spacing == nil {
			pPr.Spacing = wml.NewCT_Spacing()
		}
		lines := int64(after * 100 / 12)
		pPr.Spacing.AfterLinesAttr = &lines
	}
}

// getHeadingRules 从 rules 中提取指定级别的标题格式规则
func (p *EnhancedProcessor) getHeadingRules(rules map[string]interface{}, level int) map[string]interface{} {
	headings, ok := rules["headings"].(map[string]interface{})
	if !ok {
		return nil
	}
	key := fmt.Sprintf("level%d", level)
	if levelRules, ok := headings[key].(map[string]interface{}); ok {
		return levelRules
	}
	return nil
}

// getMapKeys 获取map的所有键（辅助函数）
// verifyFormattingResults 验证修正后的文档格式，输出关键诊断信息
func (p *EnhancedProcessor) verifyFormattingResults(doc *document.Document) {
	log.Println("[验证] ════════ 修正后格式验证 ════════")

	// 验证 docDefaults
	if styles := doc.Styles.X(); styles != nil && styles.DocDefaults != nil {
		dd := styles.DocDefaults
		if dd.RPrDefault != nil && dd.RPrDefault.RPr != nil {
			rPr := dd.RPrDefault.RPr
			font := "<未设置>"
			if rPr.RFonts != nil && rPr.RFonts.EastAsiaAttr != nil {
				font = *rPr.RFonts.EastAsiaAttr
			}
			size := "<未设置>"
			if rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
				size = fmt.Sprintf("%d half-pt (%.1fpt)", *rPr.Sz.ValAttr.ST_UnsignedDecimalNumber, float64(*rPr.Sz.ValAttr.ST_UnsignedDecimalNumber)/2)
			}
			log.Printf("[验证] docDefaults: 字体=%s, 字号=%s", font, size)
		}

		// 验证 Normal 样式
		for _, style := range styles.Style {
			if style.StyleIdAttr == nil {
				continue
			}
			if *style.StyleIdAttr == "Normal" {
				font := "<未设置>"
				if style.RPr != nil && style.RPr.RFonts != nil && style.RPr.RFonts.EastAsiaAttr != nil {
					font = *style.RPr.RFonts.EastAsiaAttr
				}
				size := "<未设置>"
				if style.RPr != nil && style.RPr.Sz != nil && style.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
					size = fmt.Sprintf("%d half-pt (%.1fpt)", *style.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber, float64(*style.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber)/2)
				}
				log.Printf("[验证] Normal 样式: 字体=%s, 字号=%s", font, size)
				break
			}
		}
	}

	// 验证前3个正文段落的实际 Run 格式
	count := 0
	for _, para := range doc.Paragraphs() {
		text := p.extractParagraphText(para)
		if len(strings.TrimSpace(text)) < 10 {
			continue
		}
		pt := p.intelligentClassifyParagraph(text)
		if pt != "body" {
			continue
		}
		count++
		if count > 5 {
			break
		}

		textSnippet := text
		if len(textSnippet) > 40 {
			textSnippet = textSnippet[:40] + "..."
		}

		// 段落样式
		pStyle := "<无>"
		if pPr := para.X().PPr; pPr != nil && pPr.PStyle != nil {
			pStyle = pPr.PStyle.ValAttr
		}

		// 第一个 Run 的格式
		runs := para.Runs()
		if len(runs) > 0 {
			run := runs[0]
			rFont := "<未设置>"
			rSize := "<未设置>"
			if rPr := run.X().RPr; rPr != nil {
				if rPr.RFonts != nil && rPr.RFonts.EastAsiaAttr != nil {
					rFont = *rPr.RFonts.EastAsiaAttr
				}
				if rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
					rSize = fmt.Sprintf("%d half-pt (%.1fpt)", *rPr.Sz.ValAttr.ST_UnsignedDecimalNumber, float64(*rPr.Sz.ValAttr.ST_UnsignedDecimalNumber)/2)
				}
			} else {
				rFont = "<无rPr>"
				rSize = "<无rPr>"
			}
			log.Printf("[验证] 正文[%d] pStyle=%s run0字体=%s 字号=%s | %s", count, pStyle, rFont, rSize, textSnippet)
		} else {
			log.Printf("[验证] 正文[%d] pStyle=%s 无Run | %s", count, pStyle, textSnippet)
		}
	}
	log.Println("[验证] ════════════════════════════════")
}

func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// classifyParagraphs 智能分类段落
// 当 smartClassifier 可用时，使用三级路由（规则→本地模型→AI）；否则回退到原规则引擎
func (p *EnhancedProcessor) classifyParagraphs(paragraphs []document.Paragraph) map[string][]document.Paragraph {
	classified := make(map[string][]document.Paragraph)

	// 收集非空段落
	var paraInfos []fallbackParaInfo
	for _, para := range paragraphs {
		text := p.extractParagraphText(para)
		if strings.TrimSpace(text) == "" {
			continue
		}
		paraInfos = append(paraInfos, fallbackParaInfo{para: para, text: text})
	}

	if len(paraInfos) == 0 {
		return classified
	}

	// ── 使用智能分类器（三级路由） ──
	if p.smartClassifier != nil {
		log.Println("[分类] 使用智能分类器（三级路由: 规则→本地模型→AI）")
		features := make([]aiclassifier.ParagraphFeature, len(paraInfos))
		for i, info := range paraInfos {
			fontSizePt, isBold, alignment := p.extractRunFormatInfo(info.para)
			features[i] = aiclassifier.ExtractFeatures(
				info.text, i, len(paraInfos),
				fontSizePt, isBold, alignment,
			)
		}

		docID := fmt.Sprintf("doc_%d", time.Now().UnixNano())
		results := p.smartClassifier.ClassifyDocument(features, docID)

		for i, r := range results {
			classified[r.Label] = append(classified[r.Label], paraInfos[i].para)
		}

		logClassifiedStats(classified)
		return classified
	}

	// ── 回退：使用原始规则引擎（无 AI/模型） ──
	log.Println("[分类] 使用传统规则引擎（无智能分类器）")
	return p.classifyParagraphsFallback(paraInfos)
}

// fallbackParaInfo 回退路径用的段落信息
type fallbackParaInfo struct {
	para document.Paragraph
	text string
}

// classifyParagraphsFallback 原始分类逻辑（回退路径）
func (p *EnhancedProcessor) classifyParagraphsFallback(paraInfos []fallbackParaInfo) map[string][]document.Paragraph {
	classified := make(map[string][]document.Paragraph)

	type classifiedInfo struct {
		para     document.Paragraph
		paraType string
	}
	var infos []classifiedInfo
	for _, info := range paraInfos {
		paraType, _ := p.intelligentClassifyParagraphWithLevel(info.text)
		infos = append(infos, classifiedInfo{para: info.para, paraType: paraType})
	}

	inEnAbstract := false
	inAbstract := false
	inOriginalityDecl := false
	inAcknowledgements := false
	coverZoneEnded := false
	for _, info := range infos {
		pt := info.paraType

		// 原创性声明：标题段及后续同页段落都归入 originality_declaration
		if pt == "originality_declaration" {
			inOriginalityDecl = true
			classified["originality_declaration"] = append(classified["originality_declaration"], info.para)
			continue
		}
		if inOriginalityDecl {
			isContentStart := pt == "abstract_title" || pt == "abstract" ||
				pt == "en_abstract_title" || pt == "keywords" || pt == "en_keywords" ||
				pt == "references_title" || pt == "table_of_contents" ||
				pt == "table_of_contents_title" ||
				strings.HasPrefix(pt, "heading_")
			if isContentStart {
				inOriginalityDecl = false
			} else {
				classified["originality_declaration"] = append(classified["originality_declaration"], info.para)
				continue
			}
		}

		if !coverZoneEnded {
			isContentStart := pt == "abstract_title" || pt == "abstract" ||
				pt == "en_abstract_title" || pt == "keywords" || pt == "en_keywords" ||
				pt == "references_title" || pt == "table_of_contents" ||
				pt == "table_of_contents_title" ||
				strings.HasPrefix(pt, "heading_")
			if isContentStart {
				coverZoneEnded = true
			} else if pt == "body" || pt == "title" || pt == "cover" {
				classified["cover"] = append(classified["cover"], info.para)
				continue
			}
		}

		switch {
		case pt == "abstract_title":
			inAbstract = true
			inEnAbstract = false
			inAcknowledgements = false
			classified["abstract_title"] = append(classified["abstract_title"], info.para)
		case pt == "en_abstract_title":
			inEnAbstract = true
			inAbstract = false
			inAcknowledgements = false
			classified["en_abstract_title"] = append(classified["en_abstract_title"], info.para)
		case pt == "en_keywords":
			inEnAbstract = false
			classified["en_keywords"] = append(classified["en_keywords"], info.para)
		case pt == "keywords":
			inAbstract = false
			classified["keywords"] = append(classified["keywords"], info.para)
		case pt == "acknowledgements_title":
			inAcknowledgements = true
			inAbstract = false
			inEnAbstract = false
			classified["acknowledgements_title"] = append(classified["acknowledgements_title"], info.para)
		case inAcknowledgements && pt == "body":
			classified["acknowledgements_content"] = append(classified["acknowledgements_content"], info.para)
		case inEnAbstract && (pt == "body" || pt == "abstract" || pt == "en_abstract"):
			classified["en_abstract"] = append(classified["en_abstract"], info.para)
		case inAbstract && (pt == "body" || pt == "abstract"):
			classified["abstract"] = append(classified["abstract"], info.para)
		default:
			if strings.HasPrefix(pt, "heading_") || pt == "references_title" ||
				pt == "table_of_contents_title" || pt == "table_of_contents" {
				inEnAbstract = false
				inAbstract = false
				inAcknowledgements = false
			}
			classified[pt] = append(classified[pt], info.para)
		}
	}

	logClassifiedStats(classified)
	return classified
}

// extractRunFormatInfo 提取段落第一个 Run 的格式信息（字号/加粗/对齐）
func (p *EnhancedProcessor) extractRunFormatInfo(para document.Paragraph) (fontSizePt float64, isBold bool, alignment string) {
	fontSizePt = 0
	alignment = "left"

	// 对齐方式
	if pPr := para.X().PPr; pPr != nil && pPr.Jc != nil {
		switch pPr.Jc.ValAttr {
		case wml.ST_JcCenter:
			alignment = "center"
		case wml.ST_JcRight:
			alignment = "right"
		case wml.ST_JcBoth:
			alignment = "justify"
		}
	}

	// 第一个 Run 的字号和加粗
	runs := para.Runs()
	if len(runs) > 0 {
		if rPr := runs[0].X().RPr; rPr != nil {
			if rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
				fontSizePt = float64(*rPr.Sz.ValAttr.ST_UnsignedDecimalNumber) / 2
			}
			isBold = rPr.B != nil
		}
	}
	return
}

func logClassifiedStats(classified map[string][]document.Paragraph) {
	coverCount := len(classified["cover"])
	bodyCount := len(classified["body"])
	log.Printf("[分类] 封面段落: %d, 正文段落: %d, 总分类: %v", coverCount, bodyCount, func() []string {
		keys := make([]string, 0)
		for k, v := range classified {
			keys = append(keys, fmt.Sprintf("%s(%d)", k, len(v)))
		}
		return keys
	}())
}

// intelligentClassifyParagraph 智能段落分类
func (p *EnhancedProcessor) intelligentClassifyParagraph(text string) string {
	paraType, _ := p.intelligentClassifyParagraphWithLevel(text)
	return paraType
}

// normalizeChineseText 去掉中文短文本中间的空格（如 "摘  要" → "摘要"，"目  录" → "目录"）
func normalizeChineseText(text string) string {
	// 只对短文本（<30字符）做空格去除，避免破坏正文
	if len([]rune(text)) < 30 {
		return strings.ReplaceAll(text, " ", "")
	}
	return text
}

// isTOCEntry 判断是否为目录条目（含 \t + 页码）
func isTOCEntry(text string) bool {
	// 目录条目特征: 标题文字 \t 页码数字  或  标题文字...... 页码
	if strings.Contains(text, "\t") {
		parts := strings.Split(text, "\t")
		lastPart := strings.TrimSpace(parts[len(parts)-1])
		if _, err := strconv.Atoi(lastPart); err == nil {
			return true
		}
	}
	// 含大量连续点号（目录引导符）
	if strings.Count(text, ".") > 5 || strings.Count(text, "…") > 2 || strings.Count(text, "·") > 5 {
		return true
	}
	return false
}

// intelligentClassifyParagraphWithLevel 智能段落分类（返回类型和标题级别）
func (p *EnhancedProcessor) intelligentClassifyParagraphWithLevel(text string) (string, int) {
	text = strings.TrimSpace(text)
	textLower := strings.ToLower(text)

	// 【新增】目录条目检测（必须在标题检测之前！）
	// 目录条目形如 "1 绪  论\t1" 或 "1.1 研究背景和意义\t1"，含 tab+页码
	if isTOCEntry(text) {
		normalized := normalizeChineseText(text)
		normalizedLower := strings.ToLower(normalized)
		// 目录标题行（"目录" / "目 录"）
		if strings.Contains(normalizedLower, "目录") {
			return "table_of_contents_title", 0
		}
		return "table_of_contents", 0
	}

	// 封面识别
	if p.isCoverPage(text) {
		return "cover", 0
	}

	// 原创性声明识别
	if p.isOriginalityDeclaration(text) {
		return "originality_declaration", 0
	}

	// 【新增】对短文本做空格归一化，再进行摘要/目录等关键词匹配
	normalized := normalizeChineseText(text)
	normalizedLower := strings.ToLower(normalized)

	// 目录标题检测（不含 tab 的 "目 录" 或 "目录" 独立段落）
	if strings.Contains(normalizedLower, "目录") && len([]rune(normalized)) < 10 {
		return "table_of_contents_title", 0
	}

	// 摘要识别（使用归一化后的文本）
	if strings.Contains(normalizedLower, "摘要") {
		if len([]rune(normalized)) < 20 {
			return "abstract_title", 0
		}
		return "abstract", 0
	}

	// 英文摘要识别
	hasChinese := containsChineseChar(text)
	if strings.Contains(textLower, "abstract") && !hasChinese {
		if len(text) < 30 {
			return "en_abstract_title", 0
		}
		return "en_abstract", 0
	}

	// 关键词识别
	if strings.Contains(normalizedLower, "关键词") || strings.Contains(normalizedLower, "关键字") {
		return "keywords", 0
	}
	if (strings.Contains(textLower, "keywords") || strings.Contains(textLower, "key words")) && !hasChinese {
		return "en_keywords", 0
	}

	// 各级标题识别（必须在论文标题识别之前）
	if level := p.detectHeadingLevel(text); level > 0 {
		return fmt.Sprintf("heading_%d", level), level
	}

	// 论文标题识别
	if p.isTitleParagraph(text) {
		return "title", 0
	}

	// 致谢识别
	if strings.Contains(normalizedLower, "致谢") || strings.Contains(normalizedLower, "致  谢") {
		if len([]rune(normalized)) < 10 {
			return "acknowledgements_title", 0
		}
	}
	if strings.Contains(textLower, "acknowledgement") || strings.Contains(textLower, "acknowledgment") {
		if len(text) < 30 {
			return "acknowledgements_title", 0
		}
	}

	// 参考文献识别
	if strings.Contains(textLower, "参考文献") || strings.Contains(textLower, "references") {
		if len(text) < 20 {
			return "references_title", 0
		}
		return "references", 0
	}

	// 参考文献条目识别（以 [数字] 或 数字. 开头的参考文献条目）
	if p.isReferenceItem(text) {
		return "references", 0
	}

	// 默认为正文
	return "body", 0
}

// isReferenceItem 判断是否为参考文献条目
// 参考文献条目格式：[1] 作者. 标题. 出版社, 年份. 或 1. 作者. 标题. 出版社, 年份.
func (p *EnhancedProcessor) isReferenceItem(text string) bool {
	// 匹配模式： [数字] 或 数字. 开头
	// 例如：[1] 张三. 某标题. 出版社, 2020.
	//      [10] 张三. 某标题. 出版社, 2020.
	//      1. 张三. 某标题. 出版社, 2020.
	patterns := []string{
		`^\[[0-9]+\]`, // [1] 或 [10]
		`^[0-9]+\.\s`, // 1. 或 10. （数字 + 句点 + 空格）
		`^[0-9]+[、]`,  // 1、 或 10、 （数字 + 顿号）
	}

	text = strings.TrimSpace(text)

	// 参考文献条目通常较长（超过30字符）
	if len(text) < 30 {
		return false
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			return true
		}
	}

	return false
}

// isTitleParagraph 判断是否为论文标题
func (p *EnhancedProcessor) isTitleParagraph(text string) bool {
	// 论文标题特征：
	// 1. 通常在文档最开头（封面之后）
	// 2. 长度适中（通常10-60字符）
	// 3. 不以句号结尾
	// 4. 通常包含研究、设计、分析等核心论文词汇

	if len(text) < 5 || len(text) > 60 {
		return false
	}

	// 以句号结尾的不是标题
	if strings.HasSuffix(text, "。") || strings.HasSuffix(text, ".") {
		return false
	}

	// 论文标题关键词（更精确的匹配）
	titleKeywords := []string{
		"毕业设计", "毕业论文", "学士学位论文", "硕士学位论文", "博士学位论文",
	}

	for _, keyword := range titleKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	// 如果文本很短（<30字符）且包含"研究"、"设计"、"分析"等，可能是论文题目
	if len(text) < 30 {
		researchKeywords := []string{
			"的研究", "的设计", "的分析", "的应用", "的构建",
			"系统设计", "系统分析", "系统开发", "系统实现",
		}
		for _, keyword := range researchKeywords {
			if strings.Contains(text, keyword) {
				return true
			}
		}
	}

	return false
}

// isCoverPage 判断是否为封面内容
func (p *EnhancedProcessor) isCoverPage(text string) bool {
	// 封面特征：
	// 1. 通常在文档最开头
	// 2. 文本较短（通常是单行标题）
	// 3. 居中显示
	// 4. 包含学校名称和毕业设计/论文等关键词

	textLower := strings.ToLower(text)

	// 封面标题关键词 - 这些是封面特有的标识
	coverKeywords := []string{
		"毕业设计（论文）", "毕业论文", "学士学位论文",
		"硕士学位论文", "博士学位论文",
	}

	for _, keyword := range coverKeywords {
		if strings.Contains(textLower, keyword) {
			return true
		}
	}

	// 如果文本很短（<25字符）且包含"大学"、"学院"，可能是封面学校名称
	if len(text) < 25 {
		universityPatterns := []string{
			"大学", "学院", "学校",
		}
		for _, pattern := range universityPatterns {
			if strings.Contains(text, pattern) {
				// 检查是否还有毕业相关关键词
				gradKeywords := []string{"毕业", "学士", "硕士", "博士"}
				for _, kw := range gradKeywords {
					if strings.Contains(text, kw) {
						return true
					}
				}
			}
		}
	}

	return false
}

// isOriginalityDeclaration 判断是否为原创性声明内容
func (p *EnhancedProcessor) isOriginalityDeclaration(text string) bool {
	normalized := normalizeChineseText(text)
	normalizedLower := strings.ToLower(normalized)
	keywords := []string{
		"原创性声明", "版权声明", "学位论文原创性声明",
		"原创性申明", "学术诚信声明", "诚信声明",
		"信誉声明", "信誉保证",
	}
	for _, kw := range keywords {
		if strings.Contains(normalizedLower, kw) {
			return true
		}
	}
	return false
}

// detectHeadingLevel 检测标题级别
func (p *EnhancedProcessor) detectHeadingLevel(text string) int {
	// 一级标题模式（修复：只匹配单个数字后跟标点，且不能是 X.X 格式）
	level1Patterns := []string{
		`^[一二三四五六七八九十]+[、.]`, // 一、 或 一. （必须是中文数字）
		`^[0-9]+[、.]`, // 1、 或 1. （单个数字 + 标点）
		`^第[一二三四五六七八九十]+[章节]`, // 第一章
		`^[0-9]+\s+[^\d]`, // 数字 + 空格 + 非数字（如 "1 绪论"）
	}

	// 二级标题模式（必须先于一级标题检查，因为 1.1 也会匹配一级的 1.）
	level2Patterns := []string{
		`^[0-9]+\.[0-9]`, // 1.1 或 1.1 （X.X 格式，必须先于一级检查）
		`^[（(][一二三四五六七八九十]+[)）]`, // （一）
	}

	// 三级标题模式
	level3Patterns := []string{
		`^[0-9]+\.[0-9]+\.[0-9]+\s*`, // 1.1.1 或 1.1.1 （可能有空格）
		`^[（(][0-9]+[)）]`,            // （1）
	}

	// 检查各级标题

	//fixme  先判断3及标题  2 及标题 1 及标题
	for _, pattern := range level3Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			return 3
		}
	}

	for _, pattern := range level2Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			return 2
		}
	}

	for _, pattern := range level1Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			return 1
		}
	}
	//for _, pattern := range level1Patterns {
	//	if matched, _ := regexp.MatchString(pattern, text); matched {
	//
	//		return 1
	//	}
	//}
	//
	//for _, pattern := range level2Patterns {
	//	if matched, _ := regexp.MatchString(pattern, text); matched {
	//
	//		return 2
	//	}
	//}
	//
	//for _, pattern := range level3Patterns {
	//	if matched, _ := regexp.MatchString(pattern, text); matched {
	//
	//		return 3
	//	}
	//}

	return 0
}

// min 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// applyPageSetup 应用页面设置
func (p *EnhancedProcessor) applyPageSetup(doc *document.Document, rules map[string]interface{}) error {
	pageSetupRules, ok := rules["page_setup"].(map[string]interface{})
	if !ok {
		return nil
	}

	section := doc.BodySection()

	// 解析页边距，默认按重庆工程学院等通用规范 2.5cm
	var marginTop, marginBottom, marginLeft, marginRight float64 = 2.5, 2.5, 2.5, 2.5
	var headerDistance, footerDistance float64 = 1.6, 2.1
	var gutter float64 = 0

	// 兼容两种结构：
	// 1) { "page_setup": { "margins": { "top": 2.5, "bottom": 2.5, ... } } }
	// 2) { "page_setup": { "margin_top": 2.5, "margin_bottom": 2.5, ... } }  // CQIEC 模板

	if margins, ok := pageSetupRules["margins"].(map[string]interface{}); ok {
		if v := parseCmValue(margins["top"]); v > 0 {
			marginTop = v
		}
		if v := parseCmValue(margins["bottom"]); v > 0 {
			marginBottom = v
		}
		if v := parseCmValue(margins["left"]); v > 0 {
			marginLeft = v
		}
		if v := parseCmValue(margins["right"]); v > 0 {
			marginRight = v
		}
	} else {
		if v := parseCmValue(pageSetupRules["margin_top"]); v > 0 {
			marginTop = v
		}
		if v := parseCmValue(pageSetupRules["margin_bottom"]); v > 0 {
			marginBottom = v
		}
		if v := parseCmValue(pageSetupRules["margin_left"]); v > 0 {
			marginLeft = v
		}
		if v := parseCmValue(pageSetupRules["margin_right"]); v > 0 {
			marginRight = v
		}
	}

	log.Printf("[页面设置] 页边距: 上=%.2fcm 下=%.2fcm 左=%.2fcm 右=%.2fcm", marginTop, marginBottom, marginLeft, marginRight)

	// 解析页眉页脚距离
	if header, ok := pageSetupRules["header"].(map[string]interface{}); ok {
		if v := parseCmValue(header["distance"]); v > 0 {
			headerDistance = v
		}
	} else if v := parseCmValue(pageSetupRules["header_distance"]); v > 0 {
		headerDistance = v
	}
	if footer, ok := pageSetupRules["footer"].(map[string]interface{}); ok {
		if v := parseCmValue(footer["distance"]); v > 0 {
			footerDistance = v
		}
	} else if v := parseCmValue(pageSetupRules["footer_distance"]); v > 0 {
		footerDistance = v
	}

	// 装订线/装订边距（gutter），单位 cm
	if g, ok := pageSetupRules["gutter"].(float64); ok {
		gutter = g
	} else if gStr, ok := pageSetupRules["gutter"].(string); ok {
		// 兼容 "0.8cm" / "8mm" / "10mm" 这种写法
		gStr = strings.TrimSpace(gStr)
		if strings.HasSuffix(gStr, "mm") {
			if val, err := strconv.ParseFloat(strings.TrimSuffix(gStr, "mm"), 64); err == nil {
				gutter = val / 10.0
			}
		} else if strings.HasSuffix(gStr, "cm") {
			if val, err := strconv.ParseFloat(strings.TrimSuffix(gStr, "cm"), 64); err == nil {
				gutter = val
			}
		} else if val, err := strconv.ParseFloat(gStr, 64); err == nil {
			gutter = val
		}
	}

	// 应用页面设置
	section.SetPageMargins(
		measurement.Distance(marginTop)*measurement.Centimeter,
		measurement.Distance(marginBottom)*measurement.Centimeter,
		measurement.Distance(marginLeft)*measurement.Centimeter,
		measurement.Distance(marginRight)*measurement.Centimeter,
		measurement.Distance(headerDistance)*measurement.Centimeter,
		measurement.Distance(footerDistance)*measurement.Centimeter,
		measurement.Distance(gutter)*measurement.Centimeter,
	)

	return nil
}

// applyHeaderFooter 应用页眉、页脚和页码设置
func (p *EnhancedProcessor) applyHeaderFooter(doc *document.Document, rules map[string]interface{}) error {
	pageSetupRules, ok := rules["page_setup"].(map[string]interface{})
	if !ok {
		log.Println("[页眉页脚] 未找到 page_setup 规则，跳过")
		return nil
	}

	section := doc.BodySection()

	// 先清除已有的 section 级别 header/footer 引用
	sectPr := section.X()
	if sectPr != nil {
		sectPr.EG_HdrFtrReferences = nil
	}

	// ── 页眉 ──
	if headerRules, ok := pageSetupRules["header"].(map[string]interface{}); ok {
		p.setupHeader(doc, section, headerRules)
	}

	// ── 页脚 & 页码 ──
	// 合并 footer 和 page_number 规则：page_number 中未指定的字段从 footer 中继承
	pageNumRules, _ := pageSetupRules["page_number"].(map[string]interface{})
	footerRules, _ := pageSetupRules["footer"].(map[string]interface{})

	merged := make(map[string]interface{})
	for k, v := range footerRules {
		if k != "distance" {
			merged[k] = v
		}
	}
	for k, v := range pageNumRules {
		merged[k] = v
	}

	if len(merged) > 0 {
		p.setupFooterWithPageNumber(doc, section, merged)
	} else {
		p.setupFooterWithPageNumber(doc, section, nil)
	}

	return nil
}

// setupHeader 设置页眉（支持奇偶页、论文题目引用、下边框线）
func (p *EnhancedProcessor) setupHeader(doc *document.Document, section document.Section, headerRules map[string]interface{}) {
	fontName := "宋体"
	fontSize := 10.5 // 五号
	if fn, ok := headerRules["font_name"].(string); ok {
		fontName = fn
	}
	if fs, ok := headerRules["font_size"].(string); ok {
		fontSize = p.parseFontSize(fs)
	} else if fs, ok := headerRules["font_size"].(float64); ok {
		fontSize = fs
	}

	content, _ := headerRules["content"].(string)
	if content == "" {
		log.Println("[页眉] 无页眉内容，跳过")
		return
	}

	paperTitle := p.extractPaperTitle(doc)

	oddText, evenText := p.parseHeaderContent(content, paperTitle)
	if oddText == "" && evenText == "" {
		log.Println("[页眉] 解析后页眉文本为空，跳过")
		return
	}

	hasOddEven := oddText != "" && evenText != "" && oddText != evenText

	if hasOddEven {
		// 奇偶页不同页眉 → 需要开启 EvenAndOddHeaders
		sectPr := section.X()
		if sectPr != nil {
			// 在 settings 中设置 (但 unioffice section 不直接暴露)
			// 通过 sectPr 的 type 来确保分节正确
		}

		// 默认页眉 = 奇数页
		hdrOdd := doc.AddHeader()
		p.buildHeaderParagraph(hdrOdd, oddText, fontName, fontSize)
		section.SetHeader(hdrOdd, wml.ST_HdrFtrDefault)

		// 偶数页页眉
		hdrEven := doc.AddHeader()
		p.buildHeaderParagraph(hdrEven, evenText, fontName, fontSize)
		section.SetHeader(hdrEven, wml.ST_HdrFtrEven)

		// 开启奇偶页页眉设置 (在 document settings 中)
		p.enableEvenOddHeaders(doc)

		log.Printf("[页眉] 已设置奇偶页: odd=%q even=%q font=%s size=%.1fpt", oddText, evenText, fontName, fontSize)
	} else {
		text := oddText
		if text == "" {
			text = evenText
		}
		hdr := doc.AddHeader()
		p.buildHeaderParagraph(hdr, text, fontName, fontSize)
		section.SetHeader(hdr, wml.ST_HdrFtrDefault)

		log.Printf("[页眉] 已设置: text=%q font=%s size=%.1fpt", text, fontName, fontSize)
	}
}

// buildHeaderParagraph 构建页眉段落（居中、字体、下边框线）
func (p *EnhancedProcessor) buildHeaderParagraph(hdr document.Header, text string, fontName string, fontSize float64) {
	hdr.Clear()
	para := hdr.AddParagraph()
	para.Properties().SetAlignment(wml.ST_JcCenter)

	// 添加下边框线（学术论文页眉标准格式）
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.PBdr = wml.NewCT_PBdr()
	pPr.PBdr.Bottom = wml.NewCT_Border()
	pPr.PBdr.Bottom.ValAttr = wml.ST_BorderSingle
	sz4 := uint64(4) // 0.5pt
	pPr.PBdr.Bottom.SzAttr = &sz4
	pPr.PBdr.Bottom.SpaceAttr = new(uint64)
	*pPr.PBdr.Bottom.SpaceAttr = 1
	pPr.PBdr.Bottom.ColorAttr = &wml.ST_HexColor{ST_HexColorAuto: wml.ST_HexColorAutoAuto}

	run := para.AddRun()
	run.AddText(text)
	p.setRunFont(run, fontName, fontSize, false)
}

// parseHeaderContent 解析页眉内容，返回 (奇数页文本, 偶数页文本)
func (p *EnhancedProcessor) parseHeaderContent(content string, paperTitle string) (string, string) {
	// 先规范化
	c := strings.TrimSpace(content)

	// 描述性规则："奇数页：XX，偶数页：YY" 或 "奇数页为论文题目，偶数页为学校名称"
	reOddEven := regexp.MustCompile(`(?:奇数页[：:为]?\s*)([^，,；;]+)[，,；;]\s*(?:偶数页[：:为]?\s*)([^，,；;]+)`)
	if m := reOddEven.FindStringSubmatch(c); len(m) >= 3 {
		odd := strings.TrimSpace(m[1])
		even := strings.TrimSpace(m[2])
		odd = p.resolveHeaderPlaceholder(odd, paperTitle)
		even = p.resolveHeaderPlaceholder(even, paperTitle)
		return odd, even
	}

	// 如果内容是纯描述性的（包含"题目"但无分隔），尝试用论文标题
	if strings.Contains(c, "题目") || strings.Contains(c, "论文标题") {
		if paperTitle != "" {
			return paperTitle, paperTitle
		}
	}

	// 纯文本内容，直接作为统一页眉
	if !strings.Contains(c, "页眉内容") && !strings.Contains(c, "奇数页") && !strings.Contains(c, "偶数页") {
		return c, c
	}

	return "", ""
}

// resolveHeaderPlaceholder 替换页眉中的占位描述
func (p *EnhancedProcessor) resolveHeaderPlaceholder(text string, paperTitle string) string {
	if strings.Contains(text, "题目") || strings.Contains(text, "论文标题") || strings.Contains(text, "标题") {
		if paperTitle != "" {
			return paperTitle
		}
		return text
	}
	return text
}

// extractPaperTitle 从文档中提取论文标题（取第一段非空、非"封面"段落中最像标题的文本）
func (p *EnhancedProcessor) extractPaperTitle(doc *document.Document) string {
	paragraphs := doc.Paragraphs()
	for i, para := range paragraphs {
		if i > 20 {
			break
		}
		text := strings.TrimSpace(p.extractParagraphText(para))
		if len(text) == 0 {
			continue
		}
		// 跳过明显的非标题段落
		if strings.Contains(text, "摘要") || strings.Contains(text, "Abstract") ||
			strings.Contains(text, "关键词") || strings.Contains(text, "Key") ||
			strings.Contains(text, "目录") || strings.Contains(text, "学号") ||
			strings.Contains(text, "指导教师") || strings.Contains(text, "专业") ||
			strings.Contains(text, "学院") || strings.Contains(text, "日期") ||
			strings.Contains(text, "年") {
			continue
		}
		// 标题通常 5-60 字符，不含换行
		runeCount := len([]rune(text))
		if runeCount >= 4 && runeCount <= 60 && !strings.Contains(text, "\n") {
			log.Printf("[页眉] 提取到论文标题: %q", text)
			return text
		}
	}
	return ""
}

// enableEvenOddHeaders 在文档 settings 中开启奇偶页页眉
func (p *EnhancedProcessor) enableEvenOddHeaders(doc *document.Document) {
	settings := doc.Settings.X()
	if settings == nil {
		return
	}
	if settings.EvenAndOddHeaders == nil {
		settings.EvenAndOddHeaders = wml.NewCT_OnOff()
	}
}

// setupFooterWithPageNumber 设置页脚（含页码）
func (p *EnhancedProcessor) setupFooterWithPageNumber(doc *document.Document, section document.Section, pageNumRules map[string]interface{}) {
	fontName := "宋体"
	fontSize := 10.5 // 五号
	position := "bottom_center"
	format := "-PAGE-"

	if pageNumRules != nil {
		if fn, ok := pageNumRules["font_name"].(string); ok {
			fontName = fn
		}
		if fs, ok := pageNumRules["font_size"].(string); ok {
			fontSize = p.parseFontSize(fs)
		} else if fs, ok := pageNumRules["font_size"].(float64); ok {
			fontSize = fs
		}
		if pos, ok := pageNumRules["position"].(string); ok {
			position = pos
		}
		if f, ok := pageNumRules["format"].(string); ok && f != "" {
			format = f
		}
	}

	// 解析页码格式中的前后缀，如 "-1-"、"— 1 —"、"第1页"
	prefix, suffix := p.parsePageNumberFormat(format)

	ftr := doc.AddFooter()
	para := ftr.AddParagraph()

	// 对齐方式
	switch position {
	case "bottom_center", "center":
		para.Properties().SetAlignment(wml.ST_JcCenter)
	case "bottom_right", "right":
		para.Properties().SetAlignment(wml.ST_JcRight)
	case "bottom_left", "left":
		para.Properties().SetAlignment(wml.ST_JcLeft)
	default:
		para.Properties().SetAlignment(wml.ST_JcCenter)
	}

	// 前缀 run
	if prefix != "" {
		prefixRun := para.AddRun()
		prefixRun.AddText(prefix)
		p.setRunFont(prefixRun, fontName, fontSize, false)
	}

	// PAGE 域字段（自动页码）
	p.addPageFieldToParagraph(para, fontName, fontSize)

	// 后缀 run
	if suffix != "" {
		suffixRun := para.AddRun()
		suffixRun.AddText(suffix)
		p.setRunFont(suffixRun, fontName, fontSize, false)
	}

	section.SetFooter(ftr, wml.ST_HdrFtrDefault)

	// 页码起始值
	sectPr := section.X()
	if sectPr.PgNumType == nil {
		sectPr.PgNumType = wml.NewCT_PageNumber()
	}
	startVal := int64(1)
	sectPr.PgNumType.StartAttr = &startVal

	log.Printf("[页脚] 已设置页码: font=%s size=%.1fpt position=%s prefix=%q suffix=%q", fontName, fontSize, position, prefix, suffix)
}

// addPageFieldToParagraph 在段落中插入 PAGE 域字段
func (p *EnhancedProcessor) addPageFieldToParagraph(para document.Paragraph, fontName string, fontSize float64) {
	pageField := wml.NewCT_SimpleField()
	pageField.InstrAttr = " PAGE "

	pContent := wml.NewEG_PContent()
	rContent := wml.NewEG_ContentRunContent()
	pageRun := wml.NewCT_R()
	pageText := wml.NewCT_Text()
	pageText.Content = "1"
	pageRun.EG_RunInnerContent = append(pageRun.EG_RunInnerContent, &wml.EG_RunInnerContent{T: pageText})

	pageRPr := wml.NewCT_RPr()
	pageRPr.RFonts = wml.NewCT_Fonts()
	fontPtr := p.getCachedFontName(fontName)
	pageRPr.RFonts.AsciiAttr = fontPtr
	pageRPr.RFonts.EastAsiaAttr = fontPtr
	pageRPr.RFonts.HAnsiAttr = fontPtr
	halfPt := uint64(fontSize * 2)
	pageRPr.Sz = wml.NewCT_HpsMeasure()
	pageRPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	pageRPr.SzCs = wml.NewCT_HpsMeasure()
	pageRPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	pageRun.RPr = pageRPr

	rContent.R = pageRun
	pContent.EG_ContentRunContent = append(pContent.EG_ContentRunContent, rContent)
	pageField.EG_PContent = append(pageField.EG_PContent, pContent)

	para.X().EG_PContent = append(para.X().EG_PContent, &wml.EG_PContent{FldSimple: []*wml.CT_SimpleField{pageField}})
}

// parsePageNumberFormat 解析页码格式字符串，提取前缀和后缀
// 例如："-1-" → prefix="-", suffix="-"
//
//	"— 1 —" → prefix="— ", suffix=" —"
//	"第1页" → prefix="第", suffix="页"
//	"1" → prefix="", suffix=""
func (p *EnhancedProcessor) parsePageNumberFormat(format string) (string, string) {
	f := strings.TrimSpace(format)
	if f == "" {
		return "", ""
	}

	// 查找数字部分的位置
	re := regexp.MustCompile(`\d+`)
	loc := re.FindStringIndex(f)
	if loc == nil {
		// 没有数字, 检查是否包含 PAGE 关键词
		rePage := regexp.MustCompile(`(?i)PAGE`)
		loc = rePage.FindStringIndex(f)
		if loc == nil {
			return "-", "-"
		}
	}

	prefix := f[:loc[0]]
	suffix := f[loc[1]:]
	return prefix, suffix
}

// setRunFont 设置 run 的字体和字号
func (p *EnhancedProcessor) setRunFont(run document.Run, fontName string, fontSizePt float64, bold bool) {
	rPr := run.X().RPr
	if rPr == nil {
		rPr = wml.NewCT_RPr()
		run.X().RPr = rPr
	}
	if rPr.RFonts == nil {
		rPr.RFonts = wml.NewCT_Fonts()
	}
	fontPtr := p.getCachedFontName(fontName)
	rPr.RFonts.EastAsiaAttr = fontPtr
	rPr.RFonts.AsciiAttr = fontPtr
	rPr.RFonts.HAnsiAttr = fontPtr
	rPr.RFonts.CsAttr = fontPtr

	halfPt := uint64(fontSizePt * 2)
	rPr.Sz = wml.NewCT_HpsMeasure()
	rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	rPr.SzCs = wml.NewCT_HpsMeasure()
	rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt

	if bold {
		rPr.B = wml.NewCT_OnOff()
	}
}

// parseCmValue 解析可能是 float64 或 string ("2.5cm", "2.5") 的值，返回 cm 单位的浮点数
func parseCmValue(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case string:
		s := strings.TrimSpace(val)
		s = strings.TrimSuffix(s, "cm")
		s = strings.TrimSpace(s)
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return 0
}

// parseMargin 解析页边距值
func (p *EnhancedProcessor) parseMargin(margin string) float64 {
	// 支持格式：2.54cm, 1in, 72pt
	margin = strings.TrimSpace(margin)

	if strings.HasSuffix(margin, "cm") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(margin, "cm"), 64); err == nil {
			return val * 28.35 // cm to points
		}
	} else if strings.HasSuffix(margin, "in") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(margin, "in"), 64); err == nil {
			return val * 72 // inch to points
		}
	} else if strings.HasSuffix(margin, "pt") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(margin, "pt"), 64); err == nil {
			return val
		}
	}

	return 0
}

// applyTitleFormatting 应用标题格式
func (p *EnhancedProcessor) applyTitleFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	titleRules, ok := rules["title"].(map[string]interface{})
	if !ok {
		return nil
	}

	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, titleRules)
	}

	return nil
}

// applyHeadingFormatting 应用各级标题格式
func (p *EnhancedProcessor) applyHeadingFormatting(paragraphs []document.Paragraph, rules map[string]interface{}, level int) error {
	headingsRules, ok := rules["headings"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("未找到 headings 规则")
	}

	levelKey := fmt.Sprintf("level%d", level)
	levelRules, ok := headingsRules[levelKey].(map[string]interface{})
	if !ok {
		return fmt.Errorf("未找到 %s 规则", levelKey)
	}

	for _, para := range paragraphs {
		if err := p.applyParagraphFormatting(para, levelRules); err != nil {
		}
	}

	return nil
}

// applyBodyFormatting 应用正文格式
func (p *EnhancedProcessor) applyBodyFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	bodyRules, ok := rules["body"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("未找到 body 规则，可用键: %v", getMapKeys(rules))
	}

	// 解析目标字号
	targetSizePt := p.resolveActualFontSizePt(bodyRules)
	log.Printf("[body] body 规则: font_name=%v, font_size=%v (解析后=%.1fpt), alignment=%v, first_line_indent=%v, line_space=%v",
		bodyRules["font_name"], bodyRules["font_size"], targetSizePt,
		bodyRules["alignment"], bodyRules["first_line_indent"], bodyRules["line_space"])

	for i, para := range paragraphs {
		// 前 3 个段落打印修正前格式
		if i < 3 {
			text := p.extractParagraphText(para)
			if len(text) > 50 {
				text = text[:50] + "..."
			}
			beforeFont, beforeSize := p.getRunFontInfo(para)
			log.Printf("[body] 段落[%d] 修正前: 字体=%s 字号=%s | %s", i, beforeFont, beforeSize, text)
		}

		if err := p.applyParagraphFormatting(para, bodyRules); err != nil {
			log.Printf("[body] 段落[%d] 格式应用失败: %v", i, err)
		}

		// 前 3 个段落打印修正后格式
		if i < 3 {
			afterFont, afterSize := p.getRunFontInfo(para)
			log.Printf("[body] 段落[%d] 修正后: 字体=%s 字号=%s", i, afterFont, afterSize)
		}
	}

	return nil
}

// getRunFontInfo 获取段落第一个 Run 的字体和字号信息（调试用）
func (p *EnhancedProcessor) getRunFontInfo(para document.Paragraph) (font string, size string) {
	font = "<无Run>"
	size = "<无Run>"
	runs := para.Runs()
	if len(runs) == 0 {
		return
	}
	run := runs[0]
	rPr := run.X().RPr
	if rPr == nil {
		font = "<无rPr>"
		size = "<无rPr>"
		return
	}
	if rPr.RFonts != nil {
		if rPr.RFonts.EastAsiaAttr != nil {
			font = *rPr.RFonts.EastAsiaAttr
		} else if rPr.RFonts.AsciiAttr != nil {
			font = *rPr.RFonts.AsciiAttr
		} else {
			font = "<RFonts无值>"
		}
	} else {
		font = "<无RFonts>"
	}
	if rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
		hp := *rPr.Sz.ValAttr.ST_UnsignedDecimalNumber
		size = fmt.Sprintf("%d半磅(%.1fpt)", hp, float64(hp)/2)
	} else {
		size = "<无Sz>"
	}
	return
}

// (resolveAbstractLabelRules / resolveAbstractContentRules / old apply functions 已被新版本替代)

// applyReferencesTitleFormatting 应用参考文献标题格式（"参考文献"四个字）
func (p *EnhancedProcessor) applyReferencesTitleFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	titleRules := map[string]interface{}{
		"font_name": "黑体",
		"font_size": "三号",
		"bold":      true,
		"alignment": "center",
	}
	for _, key := range []string{"references", "reference"} {
		if refRules, ok := rules[key].(map[string]interface{}); ok {
			if t, ok := refRules["title"].(map[string]interface{}); ok {
				for k, v := range t {
					titleRules[k] = v
				}
				break
			}
		}
	}
	log.Printf("[参考文献标题] 规则: font=%v size=%v align=%v bold=%v", titleRules["font_name"], titleRules["font_size"], titleRules["alignment"], titleRules["bold"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, titleRules)
	}
	return nil
}

// applyReferencesFormatting 应用参考文献条目格式
// 兼容 references.content / references.body / reference.body 等备用路径
func (p *EnhancedProcessor) applyReferencesFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	// 尝试 "references" 和 "reference" 两个顶层键
	var refRules map[string]interface{}
	for _, key := range []string{"references", "reference"} {
		if m, ok := rules[key].(map[string]interface{}); ok {
			refRules = m
			break
		}
	}
	if refRules == nil {
		return nil
	}

	var contentRules map[string]interface{}
	for _, k := range []string{"content", "body"} {
		if m, ok := refRules[k].(map[string]interface{}); ok {
			contentRules = m
			break
		}
	}
	if contentRules == nil {
		contentRules = refRules
	}
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, contentRules)
	}
	return nil
}

// applyAcknowledgementsTitleFormatting 应用致谢标题格式
func (p *EnhancedProcessor) applyAcknowledgementsTitleFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	titleRules := map[string]interface{}{
		"font_name":  "黑体",
		"font_size":  "小三",
		"bold":       true,
		"alignment":  "center",
		"line_space": 1.5,
	}
	if ack, ok := rules["acknowledgements"].(map[string]interface{}); ok {
		if t, ok := ack["title"].(map[string]interface{}); ok {
			for k, v := range t {
				titleRules[k] = v
			}
		}
	}
	log.Printf("[致谢标题] 规则: font=%v size=%v align=%v bold=%v", titleRules["font_name"], titleRules["font_size"], titleRules["alignment"], titleRules["bold"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, titleRules)
	}
	return nil
}

// applyAcknowledgementsContentFormatting 应用致谢正文格式
func (p *EnhancedProcessor) applyAcknowledgementsContentFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	contentRules := map[string]interface{}{
		"font_name":         "宋体",
		"font_size":         "小四",
		"alignment":         "justify",
		"first_line_indent": "2",
		"line_space":        "22磅",
	}
	if ack, ok := rules["acknowledgements"].(map[string]interface{}); ok {
		if c, ok := ack["content"].(map[string]interface{}); ok {
			for k, v := range c {
				contentRules[k] = v
			}
		}
	}
	log.Printf("[致谢正文] 规则: font=%v size=%v align=%v line_space=%v", contentRules["font_name"], contentRules["font_size"], contentRules["alignment"], contentRules["line_space"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, contentRules)
	}
	return nil
}

// applyTableFormatting 对文档中所有表格内的段落应用正文格式
func (p *EnhancedProcessor) applyTableFormatting(doc *document.Document, rules map[string]interface{}) {
	bodyRules, ok := rules["body"].(map[string]interface{})
	if !ok {
		return
	}
	for _, table := range doc.Tables() {
		for _, row := range table.Rows() {
			for _, cell := range row.Cells() {
				for _, para := range cell.Paragraphs() {
					text := strings.TrimSpace(p.extractParagraphText(para))
					if text == "" {
						continue
					}
					p.applyParagraphFormatting(para, bodyRules)
				}
			}
		}
	}
}

// isChineseFont 判断字体名是否为中文字体族
func isChineseFont(name string) bool {
	chineseFonts := []string{
		"宋体", "黑体", "楷体", "仿宋", "微软雅黑", "微软正黑",
		"方正", "华文", "中易", "新宋体", "隶书", "幼圆",
		"SimSun", "SimHei", "KaiTi", "FangSong", "Microsoft YaHei",
	}
	nameLower := strings.ToLower(name)
	for _, cf := range chineseFonts {
		if strings.Contains(strings.ToLower(cf), nameLower) ||
			strings.Contains(nameLower, strings.ToLower(cf)) {
			return true
		}
	}
	return false
}

// containsChineseChar 判断字符串是否含有汉字
func containsChineseChar(s string) bool {
	for _, r := range s {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

// resolveEnglishAbstractRules 从 rules 中取英文摘要规则，若无则提供合理默认值
//
// 规范（四川师范大学）：
//   - "Abstract" 标签：四号（14pt）、黑体加粗、居中
//   - 摘要正文：小四号（12pt）、推荐 Arial、两端对齐
func resolveEnglishAbstractRules(rules map[string]interface{}) (titleRules, contentRules map[string]interface{}) {
	// 尝试读取 english_abstract 节
	if ea, ok := rules["english_abstract"].(map[string]interface{}); ok {
		if label, ok := ea["label"].(map[string]interface{}); ok {
			titleRules = label
		} else {
			titleRules = ea
		}
		if content, ok := ea["content"].(map[string]interface{}); ok {
			contentRules = content
		} else {
			contentRules = map[string]interface{}{
				"font_name": "Arial",
				"font_size": "小四",
				"alignment": "justify",
			}
		}
		return
	}

	// 没有 english_abstract 节，使用默认值（符合四川师范大学规范）
	// "Abstract" 标签：四号 14pt，黑体加粗，居中
	titleRules = map[string]interface{}{
		"font_name":    "Arial",
		"font_name_cn": "黑体",
		"font_size":    "四号", // 14pt
		"bold":         true,
		"alignment":    "center",
	}
	// 摘要正文：小四 12pt，Arial
	contentRules = map[string]interface{}{
		"font_name": "Arial",
		"font_size": "小四", // 12pt
		"alignment": "justify",
	}
	return
}

// applyTOCTitleFormatting 应用目录标题格式（如 "目 录"）
func (p *EnhancedProcessor) applyTOCTitleFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	titleRules := map[string]interface{}{
		"font_name": "黑体",
		"font_size": "三号",
		"bold":      true,
		"alignment": "center",
	}
	if toc, ok := rules["table_of_contents"].(map[string]interface{}); ok {
		if t, ok := toc["title"].(map[string]interface{}); ok {
			titleRules = t
		}
	}
	log.Printf("[TOC标题] 规则: font=%v size=%v align=%v", titleRules["font_name"], titleRules["font_size"], titleRules["alignment"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, titleRules)
	}
	return nil
}

// applyTOCEntryFormatting 应用目录条目格式（区分一级/二级/三级）
func (p *EnhancedProcessor) applyTOCEntryFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	tocRules, _ := rules["table_of_contents"].(map[string]interface{})

	defaultL1 := map[string]interface{}{"font_name": "黑体", "font_size": "小四", "alignment": "left"}
	defaultL2 := map[string]interface{}{"font_name": "宋体", "font_size": "小四", "alignment": "left", "first_line_indent": "2"}
	defaultL3 := map[string]interface{}{"font_name": "宋体", "font_size": "小四", "alignment": "left", "first_line_indent": "4"}

	if tocRules != nil {
		// 优先使用 level1/level2/level3 分级规则
		if l1, ok := tocRules["level1"].(map[string]interface{}); ok {
			defaultL1 = l1
		}
		if l2, ok := tocRules["level2"].(map[string]interface{}); ok {
			defaultL2 = l2
		}
		if l3, ok := tocRules["level3"].(map[string]interface{}); ok {
			defaultL3 = l3
		}

		// 若无分级规则，回退到 content 平铺规则（AI 输出的格式）
		// 将 content 规则作为所有级别的基础，仅一级标题加粗
		if _, hasLevel := tocRules["level1"]; !hasLevel {
			if content, ok := tocRules["content"].(map[string]interface{}); ok {
				baseRules := make(map[string]interface{})
				for k, v := range content {
					baseRules[k] = v
				}
				// 一级：加粗，无缩进
				l1 := make(map[string]interface{})
				for k, v := range baseRules {
					l1[k] = v
				}
				l1["bold"] = true
				l1["alignment"] = "left"
				defaultL1 = l1

				// 二级：不加粗，缩进2字符
				l2 := make(map[string]interface{})
				for k, v := range baseRules {
					l2[k] = v
				}
				l2["bold"] = false
				l2["first_line_indent"] = "2"
				defaultL2 = l2

				// 三级：不加粗，缩进4字符
				l3 := make(map[string]interface{})
				for k, v := range baseRules {
					l3[k] = v
				}
				l3["bold"] = false
				l3["first_line_indent"] = "4"
				defaultL3 = l3

				log.Printf("[TOC条目] 使用 content 平铺规则: font=%v size=%v", baseRules["font_name"], baseRules["font_size"])
			}
		}
	}

	log.Printf("[TOC条目] L1: font=%v size=%v | L2: font=%v size=%v | L3: font=%v size=%v",
		defaultL1["font_name"], defaultL1["font_size"],
		defaultL2["font_name"], defaultL2["font_size"],
		defaultL3["font_name"], defaultL3["font_size"])

	for _, para := range paragraphs {
		text := p.extractParagraphText(para)
		level := p.detectTOCEntryLevel(text)
		switch level {
		case 1:
			p.applyParagraphFormatting(para, defaultL1)
		case 2:
			p.applyParagraphFormatting(para, defaultL2)
		default:
			p.applyParagraphFormatting(para, defaultL3)
		}
	}
	return nil
}

// detectTOCEntryLevel 根据文本判断目录条目的级别
func (p *EnhancedProcessor) detectTOCEntryLevel(text string) int {
	text = strings.TrimSpace(text)
	// "1 绪  论\t1" / "摘  要\t1" — 纯数字或中文标题 → 一级
	// "1.1 研究背景\t1" → 二级
	// "1.1.1 xxx\t1" → 三级
	if matched, _ := regexp.MatchString(`^\d+\.\d+\.\d+`, text); matched {
		return 3
	}
	if matched, _ := regexp.MatchString(`^\d+\.\d+`, text); matched {
		return 2
	}
	return 1
}

// applyAbstractTitleFormatting 应用中文摘要标题格式
func (p *EnhancedProcessor) applyAbstractTitleFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	titleRules := map[string]interface{}{
		"font_name": "黑体",
		"font_size": "三号",
		"bold":      true,
		"alignment": "center",
	}
	if abstract, ok := rules["abstract"].(map[string]interface{}); ok {
		if label, ok := abstract["label"].(map[string]interface{}); ok {
			titleRules = label
		}
	}
	log.Printf("[摘要标题] 规则: font=%v size=%v align=%v", titleRules["font_name"], titleRules["font_size"], titleRules["alignment"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, titleRules)
	}
	return nil
}

// applyAbstractFormatting 应用中文摘要正文格式
func (p *EnhancedProcessor) applyAbstractFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	contentRules := map[string]interface{}{
		"font_name":         "宋体",
		"font_size":         "小四",
		"alignment":         "justify",
		"first_line_indent": "2",
	}
	if abstract, ok := rules["abstract"].(map[string]interface{}); ok {
		if content, ok := abstract["content"].(map[string]interface{}); ok {
			contentRules = content
		}
	}
	log.Printf("[摘要正文] 规则: font=%v size=%v align=%v", contentRules["font_name"], contentRules["font_size"], contentRules["alignment"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, contentRules)
	}
	return nil
}

// applyKeywordsFormatting 应用中文关键词格式
// 关键词段落由两部分组成：标签（"关键词："）和内容（实际关键词），
// 需要分别应用不同的字体格式：标签用黑体，内容用仿宋。
func (p *EnhancedProcessor) applyKeywordsFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	labelRules := map[string]interface{}{
		"font_name": "黑体",
		"font_size": "小四",
		"bold":      true,
	}
	contentRules := map[string]interface{}{
		"font_name": "宋体",
		"font_size": "小四",
		"bold":      false,
	}

	if kw, ok := rules["keywords"].(map[string]interface{}); ok {
		if label, ok := kw["label"].(map[string]interface{}); ok {
			for k, v := range label {
				labelRules[k] = v
			}
		}
		if content, ok := kw["content"].(map[string]interface{}); ok {
			for k, v := range content {
				contentRules[k] = v
			}
		}
	}

	log.Printf("[关键词] 标签规则: font=%v size=%v bold=%v", labelRules["font_name"], labelRules["font_size"], labelRules["bold"])
	log.Printf("[关键词] 内容规则: font=%v size=%v bold=%v", contentRules["font_name"], contentRules["font_size"], contentRules["bold"])

	for _, para := range paragraphs {
		p.applyKeywordsParagraphFormatting(para, labelRules, contentRules)
	}
	return nil
}

// applyKeywordsParagraphFormatting 对关键词段落分别设置标签和内容的格式
func (p *EnhancedProcessor) applyKeywordsParagraphFormatting(para document.Paragraph, labelRules, contentRules map[string]interface{}) {
	paraProps := para.Properties()

	// 清除内建样式引用
	if pPr := para.X().PPr; pPr != nil && pPr.PStyle != nil {
		pPr.PStyle = nil
	}
	for _, run := range para.Runs() {
		if rPr := run.X().RPr; rPr != nil && rPr.RStyle != nil {
			rPr.RStyle = nil
		}
	}

	// 段落级属性：对齐方式（取标签或内容中有定义的）
	alignment := ""
	if a, ok := labelRules["alignment"].(string); ok {
		alignment = a
	}
	if a, ok := contentRules["alignment"].(string); ok && alignment == "" {
		alignment = a
	}
	switch alignment {
	case "center":
		paraProps.SetAlignment(wml.ST_JcCenter)
	case "left":
		paraProps.SetAlignment(wml.ST_JcLeft)
	case "right":
		paraProps.SetAlignment(wml.ST_JcRight)
	case "justify":
		paraProps.SetAlignment(wml.ST_JcBoth)
	}

	// 段落行距（取 label 或 content 中有 line_space 的）
	lineSpaceRules := contentRules
	if _, ok := labelRules["line_space"]; ok {
		lineSpaceRules = labelRules
	}
	if lineSpaceRaw, ok := lineSpaceRules["line_space"]; ok {
		p.applyLineSpacingToParagraph(para, lineSpaceRaw)
	}

	// 段落间距（"与摘要正文间隔一行"：默认段前 1 行 ≈ 12pt = 240twips）
	hasParagraphSpace := false
	if paraSpace, ok := contentRules["paragraph_space"].(map[string]interface{}); ok {
		hasParagraphSpace = true
		if before, ok := paraSpace["before"].(string); ok {
			beforeTwips := p.parseSpacing(before)
			paraProps.Spacing().SetBefore(measurement.Distance(beforeTwips) * measurement.Twips)
		}
		if after, ok := paraSpace["after"].(string); ok {
			afterTwips := p.parseSpacing(after)
			paraProps.Spacing().SetAfter(measurement.Distance(afterTwips) * measurement.Twips)
		}
	}
	if !hasParagraphSpace {
		// 格式模板要求关键词与摘要正文间隔一行，默认设置段前 1 行
		paraProps.Spacing().SetBefore(measurement.Distance(240) * measurement.Twips)
	}

	// 逐 run 设置字体格式：标签部分用 labelRules，内容部分用 contentRules
	labelEnded := false
	for _, run := range para.Runs() {
		text := run.Text()
		if !labelEnded {
			// 标签部分：匹配到 "关键词" + 冒号（全角或半角）
			if idx := findLabelEnd(text, "关键词"); idx >= 0 {
				labelEnded = true
				if idx == len(text) {
					// 整个 run 都是标签
					p.applyRunFormatting(run, labelRules)
				} else {
					// run 中同时包含标签和内容，需要拆分
					// 由于 unioffice 不方便拆 run，整个 run 用标签样式
					// 但如果标签文字很短（如"关键词："只有4字符），后续内容也在同一 run
					// 这里折中：如果标签占比 < 50%，用内容规则（因为内容是主体）
					labelPart := text[:idx]
					if float64(len([]rune(labelPart)))/float64(len([]rune(text))) >= 0.5 {
						p.applyRunFormatting(run, labelRules)
					} else {
						p.applyRunFormatting(run, contentRules)
					}
				}
			} else {
				// 还没遇到冒号，整个 run 是标签
				p.applyRunFormatting(run, labelRules)
			}
		} else {
			// 内容部分
			p.applyRunFormatting(run, contentRules)
		}
	}

	// 设置段落默认 rPr 为内容规则（因为内容是主体部分）
	p.setParagraphDefaultRPr(para, contentRules)
}

// findLabelEnd 查找标签结束位置（含冒号），返回内容开始的 byte 索引，-1 表示未找到
func findLabelEnd(text string, labelPrefix string) int {
	idx := strings.Index(text, labelPrefix)
	if idx < 0 {
		return -1
	}
	// 跳过标签文字
	pos := idx + len(labelPrefix)
	runes := []rune(text[pos:])
	for i, r := range runes {
		if r == '：' || r == ':' {
			// 冒号后即为内容开始
			byteOffset := pos + len(string(runes[:i+1]))
			return byteOffset
		}
		if r == ' ' || r == '\t' || r == '\u3000' {
			continue // 跳过空白
		}
		// 遇到非空白非冒号字符，标签可能没有冒号
		break
	}
	// 标签后没有冒号，标签结束位置就是标签文字末尾
	return pos
}

// setParagraphDefaultRPr 设置段落级默认 Run 属性
func (p *EnhancedProcessor) setParagraphDefaultRPr(para document.Paragraph, rules map[string]interface{}) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	if pPr.RPr == nil {
		pPr.RPr = wml.NewCT_ParaRPr()
	}
	paraRPr := pPr.RPr

	if fontName, ok := rules["font_name"].(string); ok {
		if paraRPr.RFonts == nil {
			paraRPr.RFonts = wml.NewCT_Fonts()
		}
		fontNamePtr := p.getCachedFontName(fontName)
		paraRPr.RFonts.EastAsiaAttr = fontNamePtr
		paraRPr.RFonts.AsciiAttr = fontNamePtr
		paraRPr.RFonts.HAnsiAttr = fontNamePtr
		paraRPr.RFonts.CsAttr = fontNamePtr
	}

	fontSizePt := p.resolveActualFontSizePt(rules)
	if fontSizePt > 0 {
		halfPt := uint64(fontSizePt * 2)
		paraRPr.Sz = wml.NewCT_HpsMeasure()
		paraRPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		paraRPr.SzCs = wml.NewCT_HpsMeasure()
		paraRPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	}

	if bold, ok := rules["bold"].(bool); ok {
		if bold {
			paraRPr.B = wml.NewCT_OnOff()
		} else {
			paraRPr.B = nil
		}
	}
}

// applyLineSpacingToParagraph 将行距设置应用到段落（从 applyParagraphFormatting 中提取的公共逻辑）
func (p *EnhancedProcessor) applyLineSpacingToParagraph(para document.Paragraph, lineSpaceRaw interface{}) {
	var lineSpace string
	var lineSpaceFloat float64

	if lineSpaceStr, ok := lineSpaceRaw.(string); ok {
		lineSpace = lineSpaceStr
	} else if lineSpaceF, ok := lineSpaceRaw.(float64); ok {
		lineSpaceFloat = lineSpaceF
		lineSpace = fmt.Sprintf("%f", lineSpaceF)
	} else {
		return
	}

	spacing := p.parseLineSpacing(lineSpace)

	var lineRule wml.ST_LineSpacingRule
	if strings.HasPrefix(lineSpace, "fixed_") {
		lineRule = wml.ST_LineSpacingRuleExact
	} else if strings.HasSuffix(lineSpace, "磅") || strings.HasSuffix(lineSpace, "pt") {
		lineRule = wml.ST_LineSpacingRuleExact
	} else if lineSpaceFloat > 0 && lineSpaceFloat <= 10 {
		lineRule = wml.ST_LineSpacingRuleAuto
	} else if val, err := strconv.ParseFloat(lineSpace, 64); err == nil && val > 0 && val <= 10 {
		lineRule = wml.ST_LineSpacingRuleAuto
	} else if strings.HasSuffix(lineSpace, "倍") {
		lineRule = wml.ST_LineSpacingRuleAuto
	} else {
		lineRule = wml.ST_LineSpacingRuleAuto
	}

	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	if pPr.Spacing == nil {
		pPr.Spacing = wml.NewCT_Spacing()
	}

	twips := int64(spacing)
	if pPr.Spacing.LineAttr == nil {
		pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	}
	pPr.Spacing.LineAttr.Int64 = &twips
	pPr.Spacing.LineRuleAttr = lineRule
}

// applyEnglishAbstractTitleFormatting 应用英文摘要标题格式（如 "ABSTRACT"）
func (p *EnhancedProcessor) applyEnglishAbstractTitleFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	titleRules, _ := resolveEnglishAbstractRules(rules)
	log.Printf("[英文摘要标题] 规则: font=%v size=%v align=%v bold=%v", titleRules["font_name"], titleRules["font_size"], titleRules["alignment"], titleRules["bold"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, titleRules)
	}
	return nil
}

// applyEnglishAbstractFormatting 应用英文摘要正文格式
func (p *EnhancedProcessor) applyEnglishAbstractFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	_, contentRules := resolveEnglishAbstractRules(rules)
	log.Printf("[英文摘要正文] 规则: font=%v size=%v align=%v", contentRules["font_name"], contentRules["font_size"], contentRules["alignment"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, contentRules)
	}
	return nil
}

// applyEnglishKeywordsFormatting 应用英文关键词格式
// 与中文关键词类似，标签 ("Key Words:") 和内容需要分别设置格式
func (p *EnhancedProcessor) applyEnglishKeywordsFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	labelRules := map[string]interface{}{
		"font_name": "Times New Roman",
		"font_size": "小四",
		"bold":      true,
	}
	contentRules := map[string]interface{}{
		"font_name": "Times New Roman",
		"font_size": "小四",
		"bold":      false,
	}

	if ekw, ok := rules["english_keywords"].(map[string]interface{}); ok {
		if label, ok := ekw["label"].(map[string]interface{}); ok {
			for k, v := range label {
				labelRules[k] = v
			}
		}
		if content, ok := ekw["content"].(map[string]interface{}); ok {
			for k, v := range content {
				contentRules[k] = v
			}
		}
	}

	log.Printf("[英文关键词] 标签规则: font=%v size=%v bold=%v", labelRules["font_name"], labelRules["font_size"], labelRules["bold"])
	log.Printf("[英文关键词] 内容规则: font=%v size=%v bold=%v", contentRules["font_name"], contentRules["font_size"], contentRules["bold"])

	for _, para := range paragraphs {
		p.applyEnglishKeywordsParagraphFormatting(para, labelRules, contentRules)
	}
	return nil
}

// applyEnglishKeywordsParagraphFormatting 对英文关键词段落分别设置标签和内容格式
func (p *EnhancedProcessor) applyEnglishKeywordsParagraphFormatting(para document.Paragraph, labelRules, contentRules map[string]interface{}) {
	paraProps := para.Properties()

	// 清除内建样式引用
	if pPr := para.X().PPr; pPr != nil && pPr.PStyle != nil {
		pPr.PStyle = nil
	}
	for _, run := range para.Runs() {
		if rPr := run.X().RPr; rPr != nil && rPr.RStyle != nil {
			rPr.RStyle = nil
		}
	}

	// 段落对齐
	if a, ok := labelRules["alignment"].(string); ok {
		switch a {
		case "center":
			paraProps.SetAlignment(wml.ST_JcCenter)
		case "left":
			paraProps.SetAlignment(wml.ST_JcLeft)
		case "justify":
			paraProps.SetAlignment(wml.ST_JcBoth)
		}
	}

	// 行距
	if ls, ok := contentRules["line_space"]; ok {
		p.applyLineSpacingToParagraph(para, ls)
	} else if ls, ok := labelRules["line_space"]; ok {
		p.applyLineSpacingToParagraph(para, ls)
	}

	// 段前间距（"与摘要正文间隔一行"）
	hasParagraphSpace := false
	if paraSpace, ok := contentRules["paragraph_space"].(map[string]interface{}); ok {
		hasParagraphSpace = true
		if before, ok := paraSpace["before"].(string); ok {
			beforeTwips := p.parseSpacing(before)
			paraProps.Spacing().SetBefore(measurement.Distance(beforeTwips) * measurement.Twips)
		}
		if after, ok := paraSpace["after"].(string); ok {
			afterTwips := p.parseSpacing(after)
			paraProps.Spacing().SetAfter(measurement.Distance(afterTwips) * measurement.Twips)
		}
	}
	if !hasParagraphSpace {
		paraProps.Spacing().SetBefore(measurement.Distance(240) * measurement.Twips)
	}

	labelEnded := false
	for _, run := range para.Runs() {
		text := run.Text()
		textLower := strings.ToLower(text)
		if !labelEnded {
			if idx := strings.Index(textLower, "key"); idx >= 0 {
				// 找到 "Key Words" 或 "Keywords" 标签
				colonIdx := strings.IndexAny(text[idx:], ":：")
				if colonIdx >= 0 {
					labelEnded = true
				}
			}
			p.applyRunFormatting(run, labelRules)
			if labelEnded {
				continue
			}
		} else {
			p.applyRunFormatting(run, contentRules)
		}
	}

	p.setParagraphDefaultRPr(para, contentRules)
}

// applyParagraphFormatting 应用段落格式
func (p *EnhancedProcessor) applyParagraphFormatting(para document.Paragraph, rules map[string]interface{}) error {
	_ = p.extractParagraphText(para)

	paraProps := para.Properties()

	// 清除段落的 Word 内建样式引用（如 "Normal"、"Heading1" 等），
	// 避免样式定义覆盖我们直接设置的格式。
	if pPr := para.X().PPr; pPr != nil && pPr.PStyle != nil {
		pPr.PStyle = nil
	}

	// 同时清除所有 Run 上可能继承的 rStyle
	for _, run := range para.Runs() {
		if rPr := run.X().RPr; rPr != nil && rPr.RStyle != nil {
			rPr.RStyle = nil
		}
	}

	if alignment, ok := rules["alignment"].(string); ok {
		switch alignment {
		case "center":
			paraProps.SetAlignment(wml.ST_JcCenter)
		case "left":
			paraProps.SetAlignment(wml.ST_JcLeft)
		case "right":
			paraProps.SetAlignment(wml.ST_JcRight)
		case "justify":
			paraProps.SetAlignment(wml.ST_JcBoth)
		}
	}

	// 应用行距
	if lineSpaceRaw, ok := rules["line_space"]; ok {

		var lineSpace string
		var lineSpaceFloat float64

		// 支持 string 和 float64 类型
		if lineSpaceStr, ok := lineSpaceRaw.(string); ok {
			lineSpace = lineSpaceStr
		} else if lineSpaceF, ok := lineSpaceRaw.(float64); ok {
			lineSpaceFloat = lineSpaceF
			lineSpace = fmt.Sprintf("%f", lineSpaceF)
		} else {
			// line_space 类型不支持（如 int），跳过行距设置但不中断后续格式应用
			goto applyOtherFormatting
		}

		// 如果 line_space 是 "fixed"，使用 line_space_value 字段
		if lineSpace == "fixed" {
			if lineSpaceValue, ok := rules["line_space_value"].(float64); ok && lineSpaceValue > 0 {
				// 使用 line_space_value 作为固定行距值（单位为磅，需要转换为twips）
				spacing := lineSpaceValue * 20 // 磅转twips

				// 直接设置 XML 属性来正确处理行距
				pPr := para.X().PPr
				if pPr == nil {
					pPr = wml.NewCT_PPr()
					para.X().PPr = pPr
				}
				if pPr.Spacing == nil {
					pPr.Spacing = wml.NewCT_Spacing()
				}

				// 设置行距值 (w:line)
				twips := int64(spacing)
				if pPr.Spacing.LineAttr == nil {
					pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
				}
				pPr.Spacing.LineAttr.Int64 = &twips

				// 设置行距模式为固定值
				pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleExact

				// 继续处理其他格式
				goto applyOtherFormatting
			}
		}

		spacing := p.parseLineSpacing(lineSpace)

		// 判断行距模式
		var lineRule wml.ST_LineSpacingRule

		// 首先检查是否为固定值行距
		if strings.HasPrefix(lineSpace, "fixed_") {
			lineRule = wml.ST_LineSpacingRuleExact
		} else if strings.HasSuffix(lineSpace, "磅") || strings.HasSuffix(lineSpace, "pt") {
			lineRule = wml.ST_LineSpacingRuleExact
		} else if lineSpaceFloat > 0 && lineSpaceFloat <= 10 {
			// 1-10之间的值视为多倍行距，使用 auto 模式
			lineRule = wml.ST_LineSpacingRuleAuto
		} else if val, err := strconv.ParseFloat(lineSpace, 64); err == nil && val > 0 && val <= 10 {
			lineRule = wml.ST_LineSpacingRuleAuto
		} else if strings.HasSuffix(lineSpace, "倍") {
			lineRule = wml.ST_LineSpacingRuleAuto
		} else {
			lineRule = wml.ST_LineSpacingRuleAuto
		}

		// 直接设置 XML 属性来正确处理行距
		// 参考 docx_checker.go 中的正确 API 用法
		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}
		if pPr.Spacing == nil {
			pPr.Spacing = wml.NewCT_Spacing()
		}

		// 设置行距值 (w:line)
		twips := int64(spacing)
		if pPr.Spacing.LineAttr == nil {
			pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
		}
		pPr.Spacing.LineAttr.Int64 = &twips

		// 设置行距模式 (w:lineRule)
		pPr.Spacing.LineRuleAttr = lineRule
	}

applyOtherFormatting:
	// 应用首行缩进（使用实际字号）
	{
		fontSize := p.resolveActualFontSizePt(rules) // point 单位

		var indentChars float64
		switch v := rules["first_line_indent"].(type) {
		case string:
			indentChars = p.parseFirstLineIndentChars(v)
		case float64:
			indentChars = v
		}
		if indentChars > 0 {
			// Distance 基本单位是 Point，缩进 = 字符数 × 字号pt
			indentPt := indentChars * fontSize
			paraProps.SetFirstLineIndent(measurement.Distance(indentPt) * measurement.Point)
		}
	}

	// 应用段落间距 — parseSpacing 返回 twips，需要转为 point（÷20）
	if paraSpace, ok := rules["paragraph_space"].(map[string]interface{}); ok {
		if before, ok := paraSpace["before"].(string); ok {
			beforeTwips := p.parseSpacing(before)
			paraProps.Spacing().SetBefore(measurement.Distance(beforeTwips) * measurement.Twips)
		}
		if after, ok := paraSpace["after"].(string); ok {
			afterTwips := p.parseSpacing(after)
			paraProps.Spacing().SetAfter(measurement.Distance(afterTwips) * measurement.Twips)
		}
	}

	// ── 同时在段落级别的默认 Run 属性（pPr/rPr）中设置字体和字号 ──
	// 这是 Word 格式优先级链中的关键一环：段落默认 rPr 会应用于
	// 所有没有直接 rPr 的文本，也会作为样式被继承的基底。
	{
		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}
		if pPr.RPr == nil {
			pPr.RPr = wml.NewCT_ParaRPr()
		}
		paraRPr := pPr.RPr

		// 段落默认字体（全部四个字体槽，与 Run 级别保持一致）
		if fontName, ok := rules["font_name"].(string); ok {
			if paraRPr.RFonts == nil {
				paraRPr.RFonts = wml.NewCT_Fonts()
			}
			fontNamePtr := p.getCachedFontName(fontName)
			paraRPr.RFonts.EastAsiaAttr = fontNamePtr
			paraRPr.RFonts.AsciiAttr = fontNamePtr
			paraRPr.RFonts.HAnsiAttr = fontNamePtr
			paraRPr.RFonts.CsAttr = fontNamePtr
		}
		if latinFont, ok := rules["font_name_latin"].(string); ok && latinFont != "" {
			if paraRPr.RFonts == nil {
				paraRPr.RFonts = wml.NewCT_Fonts()
			}
			latinPtr := p.getCachedFontName(latinFont)
			paraRPr.RFonts.AsciiAttr = latinPtr
			paraRPr.RFonts.HAnsiAttr = latinPtr
			paraRPr.RFonts.CsAttr = latinPtr
		}

		// 段落默认字号（w:sz / w:szCs，单位 half-point）
		fontSizePt := p.resolveActualFontSizePt(rules)
		if fontSizePt > 0 {
			halfPt := uint64(fontSizePt * 2)
			paraRPr.Sz = wml.NewCT_HpsMeasure()
			paraRPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
			paraRPr.SzCs = wml.NewCT_HpsMeasure()
			paraRPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		}

		// 段落默认加粗
		if bold, ok := rules["bold"].(bool); ok {
			if bold {
				paraRPr.B = wml.NewCT_OnOff()
			} else {
				paraRPr.B = nil // 清除加粗
			}
		}
	}

	// 应用字体格式到所有运行（Run 级别，最高优先级）
	for _, run := range para.Runs() {
		p.applyRunFormatting(run, rules)
	}

	return nil
}

// applyRunFormatting 应用运行格式（字体、大小、样式等）
func (p *EnhancedProcessor) applyRunFormatting(run document.Run, rules map[string]interface{}) error {
	// 应用字体名称
	// 中文字体（宋体/黑体/楷体/仿宋等）设置全部四个字体槽，确保中英文都显示正确。
	// 西文字体（Arial/Times New Roman 等）只设置 ASCII/HAnsi/CS。
	// 若同时指定 font_name_latin，则用它覆盖 ASCII/HAnsi。
	if fontName, ok := rules["font_name"].(string); ok {
		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		fontNamePtr := p.getCachedFontName(fontName)

		// 直接操作 XML 属性，不走 SetFontFamily（后者在某些 unioffice 版本对中文字体处理不完善）
		rPr.RFonts.EastAsiaAttr = fontNamePtr
		rPr.RFonts.AsciiAttr = fontNamePtr
		rPr.RFonts.HAnsiAttr = fontNamePtr
		rPr.RFonts.CsAttr = fontNamePtr
	}

	// 可选：单独指定西文字体（如正文中"宋体+Times New Roman"双字体方案）
	if latinFont, ok := rules["font_name_latin"].(string); ok && latinFont != "" {
		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		latinPtr := p.getCachedFontName(latinFont)
		rPr.RFonts.AsciiAttr = latinPtr
		rPr.RFonts.HAnsiAttr = latinPtr
		rPr.RFonts.CsAttr = latinPtr
	}

	// 应用字体大小（兼容 string "小四" 和 float64 12.0 两种格式）
	var fontSizePt float64
	switch fs := rules["font_size"].(type) {
	case string:
		fontSizePt = p.parseFontSize(fs)
	case float64:
		fontSizePt = fs
	}
	if fsPt, ok := rules["font_size_pt"].(float64); ok && fsPt > 0 {
		fontSizePt = fsPt
	}
	if fontSizePt > 0 {
		// 直接设置 XML 属性（w:sz / w:szCs，单位 half-point），
		// 避免 unioffice SetSize 的内部单位转换问题。
		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}
		halfPt := uint64(fontSizePt * 2)
		rPr.Sz = wml.NewCT_HpsMeasure()
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		rPr.SzCs = wml.NewCT_HpsMeasure()
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	}

	// 应用加粗（显式设置 true/false，确保能取消加粗）
	if bold, ok := rules["bold"].(bool); ok {
		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}
		if bold {
			rPr.B = wml.NewCT_OnOff()
			rPr.BCs = wml.NewCT_OnOff()
		} else {
			rPr.B = nil
			rPr.BCs = nil
		}
	}

	// 应用斜体（显式设置）
	if italic, ok := rules["italic"].(bool); ok {
		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}
		if italic {
			rPr.I = wml.NewCT_OnOff()
			rPr.ICs = wml.NewCT_OnOff()
		} else {
			rPr.I = nil
			rPr.ICs = nil
		}
	}

	// 应用下划线
	if underline, ok := rules["underline"].(bool); ok && underline {
		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}
		rPr.U = wml.NewCT_Underline()
		rPr.U.ValAttr = wml.ST_UnderlineSingle
	}

	return nil
}

// 辅助解析方法

func (p *EnhancedProcessor) parseLineSpacing(spacing string) float64 {
	spacing = strings.TrimSpace(spacing)

	// 精确匹配常见行距值
	switch spacing {
	case "single", "1":
		return 240 // 单倍行距 = 240 twips
	case "1.5":
		return 360 // 1.5倍行距 = 360 twips
	case "double", "2":
		return 480 // 2倍行距 = 480 twips
	}

	// 处理带"倍"的格式，如 "1.5倍行距"、"2倍行距"
	if strings.HasSuffix(spacing, "倍") {
		numStr := strings.TrimSuffix(spacing, "倍")
		if val, err := strconv.ParseFloat(numStr, 64); err == nil {
			return val * 240
		}
	}

	// 处理固定值行距（磅或pt后缀）- 转换为twips（1磅 = 20 twips）
	if strings.HasSuffix(spacing, "磅") || strings.HasSuffix(spacing, "pt") {
		suffix := "磅"
		if strings.HasSuffix(spacing, "pt") {
			suffix = "pt"
		}
		numStr := strings.TrimSuffix(spacing, suffix)
		if val, err := strconv.ParseFloat(numStr, 64); err == nil {
			return val * 20 // 磅转twips
		}
	}

	// 处理 fixed_ 前缀格式 - 转换为twips
	if strings.HasPrefix(spacing, "fixed_") {
		ptStr := strings.TrimPrefix(spacing, "fixed_")
		ptStr = strings.TrimSuffix(ptStr, "_pt")
		if val, err := strconv.ParseFloat(ptStr, 64); err == nil {
			return val * 20 // 磅转twips
		}
	}

	// 处理纯数字
	if val, err := strconv.ParseFloat(spacing, 64); err == nil {
		if val > 0 && val <= 10 {
			return val * 240
		}
		return val
	}

	return 0
}

// resolveActualFontSizePt 从规则中提取字号（point），支持 string 和 float64
func (p *EnhancedProcessor) resolveActualFontSizePt(rules map[string]interface{}) float64 {
	if fontSizeStr, ok := rules["font_size"].(string); ok {
		if pt := p.parseFontSize(fontSizeStr); pt > 0 {
			return pt
		}
	}
	if fontSizePt, ok := rules["font_size_pt"].(float64); ok && fontSizePt > 0 {
		return fontSizePt
	}
	if fontSizeF, ok := rules["font_size"].(float64); ok && fontSizeF > 0 {
		return fontSizeF
	}
	return 12.0 // 默认小四号
}

// parseFirstLineIndentChars 从字符串中提取首行缩进字符数
// 支持 "2"、"2字符"、"1.27cm"
func (p *EnhancedProcessor) parseFirstLineIndentChars(indent string) float64 {
	indent = strings.TrimSpace(indent)
	if strings.HasSuffix(indent, "字符") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(indent, "字符"), 64); err == nil {
			return val
		}
	}
	// 纯数字视为字符数
	if val, err := strconv.ParseFloat(indent, 64); err == nil {
		return val
	}
	return 0
}

func (p *EnhancedProcessor) parseIndent(indent string) float64 {
	indent = strings.TrimSpace(indent)

	if strings.HasSuffix(indent, "字符") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(indent, "字符"), 64); err == nil {
			return val * 14 * 20
		}
	} else if strings.HasSuffix(indent, "cm") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(indent, "cm"), 64); err == nil {
			return val * 567
		}
	}

	return 0
}

func (p *EnhancedProcessor) parseIndentWithFontSize(indent string, fontSize float64) float64 {
	indent = strings.TrimSpace(indent)

	if strings.HasSuffix(indent, "字符") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(indent, "字符"), 64); err == nil {
			return val * fontSize * 20
		}
	} else if strings.HasSuffix(indent, "cm") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(indent, "cm"), 64); err == nil {
			return val * 567
		}
	}
	// 纯数字视为字符数
	if val, err := strconv.ParseFloat(indent, 64); err == nil {
		return val * fontSize * 20
	}

	return 0
}

func (p *EnhancedProcessor) parseSpacing(spacing string) float64 {
	spacing = strings.TrimSpace(spacing)

	// 正确处理"0"值
	if spacing == "0" {
		return 0
	} else if strings.HasSuffix(spacing, "磅") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(spacing, "磅"), 64); err == nil {
			return val * 20 // points to twips
		}
	} else if strings.HasSuffix(spacing, "pt") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(spacing, "pt"), 64); err == nil {
			return val * 20 // points to twips
		}
	}

	return 0
}

// fixme  设置字体相关
func (p *EnhancedProcessor) parseFontSize(size string) float64 {
	size = strings.TrimSpace(size)

	// 去掉常见后缀（"号"、"pt"、"磅"）
	for _, suffix := range []string{"号", "pt", "磅"} {
		size = strings.TrimSuffix(size, suffix)
	}
	size = strings.TrimSpace(size)

	// 中文字号转换（支持 "小四" / "小四号" / "四号" / "四" 等变体）
	sizeMap := map[string]float64{
		"初号": 42, "初": 42,
		"小初号": 36, "小初": 36,
		"一号": 26, "一": 26,
		"小一号": 24, "小一": 24,
		"二号": 22, "二": 22,
		"小二号": 18, "小二": 18,
		"三号": 16, "三": 16,
		"小三号": 15, "小三": 15,
		"四号": 14, "四": 14,
		"小四号": 12, "小四": 12,
		"五号": 10.5, "五": 10.5,
		"小五号": 9, "小五": 9,
		"六号": 7.5, "六": 7.5,
		"小六号": 6.5, "小六": 6.5,
		"七号": 5.5, "七": 5.5,
		"八号": 5, "八": 5,
	}

	if val, ok := sizeMap[size]; ok {
		return val
	}

	// 直接数字，视为 point
	if val, err := strconv.ParseFloat(size, 64); err == nil {
		return val
	}

	log.Printf("[parseFontSize] ⚠️  无法识别的字号: %q", size)
	return 0
}

// 辅助方法

func (p *EnhancedProcessor) extractParagraphText(para document.Paragraph) string {
	var text strings.Builder
	for _, run := range para.Runs() {
		text.WriteString(run.Text())
	}
	return text.String()
}

func (p *EnhancedProcessor) generateOutputPath(originalPath string) string {
	dir := filepath.Dir(originalPath)
	filename := filepath.Base(originalPath)
	ext := filepath.Ext(filename)
	baseName := strings.TrimSuffix(filename, ext)

	return filepath.Join(dir, fmt.Sprintf("%s_enhanced_corrected_%d%s", baseName, time.Now().Unix(), ext))
}

func (p *EnhancedProcessor) handleUnsupportedFormat(docPath string) (string, error) {
	outputPath := p.generateOutputPath(docPath)

	// 复制原文件
	input, err := os.Open(docPath)
	if err != nil {
		return "", err
	}
	defer input.Close()

	output, err := os.Create(outputPath)
	if err != nil {
		return "", err
	}
	defer output.Close()

	_, err = output.ReadFrom(input)
	return outputPath, err
}

// ExtractDocInfo 提取文档信息（实现接口）
func (p *EnhancedProcessor) ExtractDocInfo(ctx context.Context, docPath string) (map[string]interface{}, error) {
	// 基本实现，可以根据需要扩展
	return map[string]interface{}{
		"file_path": docPath,
		"processor": "enhanced",
	}, nil
}

// ExtractDocumentInfo 提取文档信息（实现接口）
func (p *EnhancedProcessor) ExtractDocumentInfo(filePath string) (FileInfo, error) {
	// 基本实现，返回默认信息
	return FileInfo{
		Format:    "docx",
		Pages:     1,
		WordCount: 0,
		CharCount: 0,
	}, nil
}

// ExtractHeadings 提取标题（实现接口）
func (p *EnhancedProcessor) ExtractHeadings(ctx context.Context, docPath string) ([]map[string]interface{}, error) {
	// 基本实现，可以根据需要扩展
	return []map[string]interface{}{}, nil
}

// ExtractParagraphs 提取段落（实现接口）
func (p *EnhancedProcessor) ExtractParagraphs(ctx context.Context, docPath string) ([]map[string]interface{}, error) {
	// 基本实现，可以根据需要扩展
	return []map[string]interface{}{}, nil
}
