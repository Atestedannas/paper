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
	templatePath    string                        // 黄金模板路径（由 SetTemplatePath 设置）
	lastDiffReport  *DocDiffReport                // 最近一次格式修正的差异报告
}

// GetLastDiffReport 返回最近一次格式修正的差异报告
func (p *EnhancedProcessor) GetLastDiffReport() *DocDiffReport {
	return p.lastDiffReport
}

// SetTemplatePath 设置黄金模板路径，调用 ApplyCorrections 前先设置
func (p *EnhancedProcessor) SetTemplatePath(path string) {
	p.templatePath = path
	log.Printf("[模板路径] 已设置黄金模板: %s", path)
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

	// ── 尝试AI模板直读方案（精确，无JSON误差）──────────────────────────
	log.Println("[格式修正] ================= 开始应用格式 =================")
	var templateSpecs map[string]ParagraphFormatSpec

	// 模板路径三级查找：① SetTemplatePath字段 → ② corrections map → ③ 自动扫描目录
	activeTplPath := p.templatePath
	if activeTplPath == "" {
		activeTplPath = getStringFromCorrectionsList(corrections, "template_path")
	}
	if activeTplPath == "" {
		// 自动扫描 golden_templates 目录（按优先级：*_real.docx > *_prepared.docx > *.docx）
		for _, pattern := range []string{
			"uploads/golden_templates/*_real.docx",
			"uploads/golden_templates/*_prepared.docx",
			"uploads/golden_templates/*.docx",
		} {
			if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
				for _, m := range matches {
					if info, err2 := os.Stat(m); err2 == nil && info.Size() > 50000 {
						activeTplPath = m
						log.Printf("[格式修正] 自动发现模板: %s (%.1fKB)", m, float64(info.Size())/1024)
						break
					}
				}
			}
			if activeTplPath != "" {
				break
			}
		}
	}
	log.Printf("[格式修正] 最终模板路径: %q", activeTplPath)

	if activeTplPath != "" {
		loader := NewTemplateFormatLoader(p)
		if specs, loadErr := loader.LoadFromFile(activeTplPath); loadErr == nil && len(specs) > 0 {
			templateSpecs = specs
			log.Printf("[格式修正] ✅ 模板直读方案：成功加载 %d 种格式规范", len(specs))
		} else {
			log.Printf("[格式修正] ⚠️  模板直读加载失败(%v)，回退到JSON规则方案", loadErr)
		}
	}

	if templateSpecs != nil {
		// 新方案：直接从模板OOXML格式规范应用，精确复制模板格式
		if err := p.applyTemplateFormatting(doc, formatRules, templateSpecs); err != nil {
			log.Printf("[格式修正] ❌ 模板格式应用失败: %v", err)
			log.Println("++++++++++++ 格式修正流程 结束（修正失败） ++++++++++++")
			return "", fmt.Errorf("格式修正失败: %w", err)
		}
	} else {
		// 旧方案：JSON规则（兼容回退）
		if err := p.applyPreciseFormatting(doc, formatRules); err != nil {
			log.Printf("[格式修正] ❌ JSON规则应用失败: %v", err)
			log.Println("++++++++++++ 格式修正流程 结束（修正失败） ++++++++++++")
			return "", fmt.Errorf("格式修正失败: %w", err)
		}
	}
	log.Println("[格式修正] ================= 格式应用完成 =================")

	// 验证：输出修正后前5个正文段落的实际格式
	p.verifyFormattingResults(doc)

	// 自动验证+自纠正循环
	var dsClient *aiclassifier.DeepSeekWebClient
	if p.smartClassifier != nil {
		dsClient = p.smartClassifier.GetDeepSeekClient()
	}
	verifier := NewFormatVerifier(p, dsClient)
	var fixCount int
	if templateSpecs != nil {
		// 新方案：用模板规范验证（高精度）
		fixCount = verifier.VerifyAndFixWithSpecs(doc, templateSpecs)
	} else {
		// 旧方案：JSON规则验证
		fixCount = verifier.VerifyAndFix(doc, formatRules)
	}
	if fixCount > 0 {
		log.Printf("[自动纠正] 共修正 %d 处格式偏差", fixCount)
	}

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

	// 步骤 0a: Section 级别格式（A4纸张、标准边距、双线页眉、页脚页码、三线表、上标等）
	log.Println("[步骤0a] ---- 应用 Section 级别格式 ----")
	p.ApplySectionLevelFormatting(doc)

	// 步骤 0b: 修改样式定义
	log.Println("[步骤0b] ---- 修改 styles.xml 样式定义 ----")
	p.applyStyleDefinitions(doc, rules)
	log.Println("[步骤0b] ---- 样式定义修改完成 ----")

	// 步骤 1: 页面设置 (JSON覆盖)
	log.Println("[步骤1] ---- 应用页面设置 ----")
	if err := p.applyPageSetup(doc, rules); err != nil {
		log.Printf("[步骤1] ⚠️  页面设置失败: %v", err)
	}

	// 步骤 1b: 页眉页脚 & 页码 (JSON覆盖)
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

	// 步骤 4: 各级标题（1-4级）
	for level := 1; level <= 4; level++ {
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

	// 步骤 10: 附录
	if appendixTitleParas, exists := classifiedParagraphs["appendix_title"]; exists && len(appendixTitleParas) > 0 {
		log.Printf("[步骤10a] ---- 附录标题 (%d段) ----", len(appendixTitleParas))
		p.applyAppendixTitleFormatting(appendixTitleParas, rules)
	}
	if appendixContentParas, exists := classifiedParagraphs["appendix_content"]; exists && len(appendixContentParas) > 0 {
		log.Printf("[步骤10b] ---- 附录正文 (%d段) ----", len(appendixContentParas))
		p.applyAppendixContentFormatting(appendixContentParas, rules)
	}

	// 步骤 11: 注释
	if notesTitleParas, exists := classifiedParagraphs["notes_title"]; exists && len(notesTitleParas) > 0 {
		log.Printf("[步骤11a] ---- 注释标题 (%d段) ----", len(notesTitleParas))
		p.applyNotesTitleFormatting(notesTitleParas, rules)
	}
	if notesContentParas, exists := classifiedParagraphs["notes_content"]; exists && len(notesContentParas) > 0 {
		log.Printf("[步骤11b] ---- 注释正文 (%d段) ----", len(notesContentParas))
		p.applyNotesContentFormatting(notesContentParas, rules)
	}

	// 步骤 12: 图表标题
	if figCaptionParas, exists := classifiedParagraphs["figure_caption"]; exists && len(figCaptionParas) > 0 {
		log.Printf("[步骤12a] ---- 图标题 (%d段) ----", len(figCaptionParas))
		p.applyFigureCaptionFormatting(figCaptionParas, rules)
	}
	if tblCaptionParas, exists := classifiedParagraphs["table_caption"]; exists && len(tblCaptionParas) > 0 {
		log.Printf("[步骤12b] ---- 表标题 (%d段) ----", len(tblCaptionParas))
		p.applyTableCaptionFormatting(tblCaptionParas, rules)
	}

	// 步骤 13: 表格内文字（全文应用，不再跳过封面/声明）
	log.Println("[步骤14] ---- 应用表格内格式 ----")
	p.applyTableFormatting(doc, rules, map[*wml.CT_P]bool{})

	return nil
}

// getStringFromCorrectionsList 从corrections列表中按key提取字符串值
func getStringFromCorrectionsList(corrections []map[string]interface{}, key string) string {
	for _, c := range corrections {
		if val, ok := c[key]; ok {
			if s, ok := val.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func getSchoolIDFromCorrectionsList(corrections []map[string]interface{}) string {
	for _, c := range corrections {
		if c == nil {
			continue
		}
		if val, ok := c["school_id"]; ok {
			if s, ok := val.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

// applyTemplateFormatting 新方案：直接从模板OOXML格式规范应用格式
// 步骤：页面设置/页眉页脚（保留JSON规则）→ AI分类 → 直接应用模板格式规范 → 表格格式
func (p *EnhancedProcessor) applyTemplateFormatting(doc *document.Document, rules map[string]interface{}, specs map[string]ParagraphFormatSpec) error {
	// 注意：模板模式下不调用 applyStyleDefinitions——该函数会全局修改 docDefaults/Named Styles，
	// 导致封面等跳过段落的字体被意外修改。模板模式通过 AIFormatApplier 直接写入 run-level rPr，
	// 无需再改 styles.xml 层。

	// 步骤 0: Section 级别格式（A4纸张、标准边距、双线页眉、页脚页码、三线表、上标等）
	log.Println("[模板方案][步骤0] 应用 Section 级别格式")
	p.ApplySectionLevelFormatting(doc)

	// 步骤 1: 页面设置（JSON规则覆盖，如果有的话）
	log.Println("[模板方案][步骤1] 应用页面设置(JSON覆盖)")
	if err := p.applyPageSetup(doc, rules); err != nil {
		log.Printf("[模板方案][步骤1] ⚠️  页面设置失败: %v", err)
	}

	// 步骤 1b: 页眉页脚&页码（JSON覆盖）
	log.Println("[模板方案][步骤1b] 应用页眉页脚和页码(JSON覆盖)")
	if err := p.applyHeaderFooter(doc, rules); err != nil {
		log.Printf("[模板方案][步骤1b] ⚠️  页眉页脚设置失败: %v", err)
	}

	// 步骤 2: 段落分类
	log.Println("[模板方案][步骤2] 段落分类")
	paragraphs := doc.Paragraphs()
	log.Printf("[模板方案][步骤2] 文档总段落数: %d", len(paragraphs))
	classified := p.classifyParagraphs(paragraphs)

	// 输出分类详情
	for category, paras := range classified {
		sampleText := ""
		if len(paras) > 0 {
			t := p.extractParagraphText(paras[0])
			if len(t) > 50 {
				t = t[:50] + "..."
			}
			sampleText = t
		}
		log.Printf("[模板方案][步骤2]   %s: %d 个段落, 首段: %q", category, len(paras), sampleText)
	}

	// 步骤 3: 使用AI格式应用器直接按模板规范修正
	log.Printf("[模板方案][步骤3] 应用模板格式规范（%d种类型，全文含封面/声明）", len(specs))
	skipCategories := map[string]bool{}
	applier := NewAIFormatApplier(p)
	totalFixed := applier.Apply(classified, specs, skipCategories)
	log.Printf("[模板方案][步骤3] 共修正 %d 个段落", totalFixed)

	// 步骤 3.5: 生成差异报告（修正后重新扫描，找出仍有偏差的段落）
	diffReport := &DocDiffReport{}
	paraIdx := 0
	for category, paras := range classified {
		if skipCategories[category] {
			continue
		}
		spec, ok := specs[category]
		if !ok || spec.IsEmpty() {
			continue
		}
		for _, para := range paras {
			text := strings.TrimSpace(p.extractParagraphText(para))
			if text == "" {
				paraIdx++
				continue
			}
			actualSpec := extractParaFormatSpec(para)
			diffs := DiffSpec(spec, actualSpec)
			if len(diffs) > 0 {
				preview := []rune(text)
				if len(preview) > 30 {
					preview = preview[:30]
				}
				pd := ParaDiff{
					ParaIndex: paraIdx,
					Category:  category,
					Text:      string(preview) + "...",
					Diffs:     diffs,
				}
				diffReport.ParaDiffs = append(diffReport.ParaDiffs, pd)
				for _, d := range diffs {
					if d.Severity == "error" {
						diffReport.ErrorCount++
					} else {
						diffReport.WarningCount++
					}
				}
			}
			paraIdx++
			diffReport.TotalParas++
		}
	}
	log.Printf("[差异报告] 扫描 %d 段，发现 %d 错误 %d 警告",
		diffReport.TotalParas, diffReport.ErrorCount, diffReport.WarningCount)
	p.lastDiffReport = diffReport

	// 步骤 4: 参考文献"另起页"处理
	// 如果模板规范中 references_title 有 PageBreak，已由applier处理
	// 但为安全起见，仍从JSON规则中读取 new_page/page_break 标志
	for _, category := range []string{"references_title", "acknowledgements_title", "appendix_title", "notes_title"} {
		if paras, ok := classified[category]; ok && len(paras) > 0 {
			// 从规范中获取 PageBreak 设置
			if spec, ok := specs[category]; ok && spec.PageBreak {
				p.setPageBreakBefore(paras[0])
			}
		}
	}

	// 步骤 5: 表格内文字格式（全文应用）
	log.Println("[模板方案][步骤5] 应用表格内格式")
	skipParaSet := map[*wml.CT_P]bool{}
	// 如果有模板规范，用模板中 body 字体/字号覆盖表格内文字（保证一致性）
	if bodySpec, ok := specs["body"]; ok && !bodySpec.IsEmpty() {
		p.applyTableFormattingWithSpec(doc, bodySpec, skipParaSet)
	} else {
		p.applyTableFormatting(doc, rules, skipParaSet)
	}

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

// classifiedInfo 段落分类结果
type classifiedInfo struct {
	para     document.Paragraph
	paraType string
}

// classifyParagraphsFallback 原始分类逻辑（回退路径）
func (p *EnhancedProcessor) classifyParagraphsFallback(paraInfos []fallbackParaInfo) map[string][]document.Paragraph {
	classified := make(map[string][]document.Paragraph)

	var infos []classifiedInfo
	for _, info := range paraInfos {
		// 优先使用 Word 样式名称（100% 可靠信号）
		paraType := p.classifyByWordStyle(info.para)
		if paraType == "" {
			paraType, _ = p.intelligentClassifyParagraphWithLevel(info.text)
		}
		infos = append(infos, classifiedInfo{para: info.para, paraType: paraType})
	}

	// 有序状态机替代手工布尔标志（cover/abstract/en_abstract/toc/body/references 区段）
	sm := aiclassifier.NewThesisStateMachine()
	inOriginalityDecl := false
	inAcknowledgements := false
	inAppendix := false
	inNotes := false
	for i, info := range infos {
		pt := info.paraType
		text := paraInfos[i].text

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

		// 专项区段触发（致谢/附录/注释使用独立标志，状态机不覆盖这些子类型）
		switch pt {
		case "acknowledgements_title":
			inAcknowledgements = true
			inAppendix = false
			inNotes = false
			classified["acknowledgements_title"] = append(classified["acknowledgements_title"], info.para)
			continue
		case "appendix_title":
			inAppendix = true
			inAcknowledgements = false
			inNotes = false
			classified["appendix_title"] = append(classified["appendix_title"], info.para)
			continue
		case "notes_title":
			inNotes = true
			inAppendix = false
			inAcknowledgements = false
			classified["notes_title"] = append(classified["notes_title"], info.para)
			continue
		}

		// 专项区段内容
		if inNotes && (pt == "body" || pt == "references") {
			classified["notes_content"] = append(classified["notes_content"], info.para)
			continue
		}
		if inAppendix && pt == "body" {
			classified["appendix_content"] = append(classified["appendix_content"], info.para)
			continue
		}
		if inAcknowledgements && pt == "body" {
			classified["acknowledgements_content"] = append(classified["acknowledgements_content"], info.para)
			continue
		}

		// 结构边界重置专项区段标志
		if strings.HasPrefix(pt, "heading_") || pt == "references_title" ||
			pt == "table_of_contents_title" || pt == "table_of_contents" {
			inAcknowledgements = false
			inAppendix = false
			inNotes = false
		}

		// 状态机负责主干区段（封面→摘要→英文摘要→目录→正文→参考文献）
		corrected := sm.Reclassify(pt, text)
		if corrected != pt {
			log.Printf("[回退分类器状态机] para#%d: %s → %s", i, pt, corrected)
		}
		classified[corrected] = append(classified[corrected], info.para)
	}

	// 结构顺序约束纠错：利用论文固定结构顺序修正误分类
	classified = p.applyStructureOrderConstraints(classified, infos)

	logClassifiedStats(classified)
	return classified
}

// applyStructureOrderConstraints 利用论文结构顺序约束修正分类错误
// 论文结构固定顺序：封面 -> 摘要 -> 目录 -> 正文 -> 参考文献 -> 致谢 -> 附录
// 如果 body 段落出现在参考文献标题之后，应该被重新分类为 references
func (p *EnhancedProcessor) applyStructureOrderConstraints(classified map[string][]document.Paragraph, infos []classifiedInfo) map[string][]document.Paragraph {
	sectionOrder := []string{
		"cover", "originality_declaration",
		"abstract_title", "abstract", "keywords",
		"en_abstract_title", "en_abstract", "en_keywords",
		"table_of_contents_title", "table_of_contents",
		"body",
		"references_title", "references",
		"notes_title", "notes_content",
		"acknowledgements_title", "acknowledgements_content",
		"appendix_title", "appendix_content",
	}

	sectionIndex := make(map[string]int)
	for i, s := range sectionOrder {
		sectionIndex[s] = i
	}

	// 找到关键锚点段落的位置
	refTitleIdx := -1
	ackTitleIdx := -1
	appendixTitleIdx := -1
	for i, info := range infos {
		switch info.paraType {
		case "references_title":
			if refTitleIdx < 0 {
				refTitleIdx = i
			}
		case "acknowledgements_title":
			if ackTitleIdx < 0 {
				ackTitleIdx = i
			}
		case "appendix_title":
			if appendixTitleIdx < 0 {
				appendixTitleIdx = i
			}
		}
	}

	// 纠正逻辑：参考文献标题之后的 body 段落（且在致谢之前）应归为 references
	if refTitleIdx >= 0 {
		var newBody []document.Paragraph
		for _, bodyPara := range classified["body"] {
			reclassified := false
			for i, info := range infos {
				if info.para.X() == bodyPara.X() {
					if i > refTitleIdx && (ackTitleIdx < 0 || i < ackTitleIdx) {
						classified["references"] = append(classified["references"], bodyPara)
						reclassified = true
						log.Printf("[顺序约束] 段落 %d 从 body 纠正为 references", i)
					}
					break
				}
			}
			if !reclassified {
				newBody = append(newBody, bodyPara)
			}
		}
		classified["body"] = newBody
	}

	return classified
}

// classifyByWordStyle 利用 Word 内建样式名称进行 100% 可靠分类
func (p *EnhancedProcessor) classifyByWordStyle(para document.Paragraph) string {
	pPr := para.X().PPr
	if pPr == nil || pPr.PStyle == nil {
		return ""
	}
	style := strings.ToLower(strings.TrimSpace(pPr.PStyle.ValAttr))

	switch {
	case style == "heading1" || style == "heading 1" || style == "标题 1" || style == "标题1":
		return "heading_1"
	case style == "heading2" || style == "heading 2" || style == "标题 2" || style == "标题2":
		return "heading_2"
	case style == "heading3" || style == "heading 3" || style == "标题 3" || style == "标题3":
		return "heading_3"
	case style == "heading4" || style == "heading 4" || style == "标题 4" || style == "标题4":
		return "heading_4"
	case style == "title" || style == "论文标题":
		return "title"
	case style == "toc1" || style == "toc 1":
		return "table_of_contents"
	case style == "toc2" || style == "toc 2":
		return "table_of_contents"
	case style == "toc3" || style == "toc 3":
		return "table_of_contents"
	case style == "tocheading" || style == "toc heading":
		return "table_of_contents_title"
	}
	return ""
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

	// 副标题识别（以"——"或"—"开头，且较短）
	trimmed := strings.TrimSpace(text)
	if (strings.HasPrefix(trimmed, "——") || strings.HasPrefix(trimmed, "—")) && len([]rune(trimmed)) < 50 {
		return "title", 0
	}

	// 附录识别
	if strings.Contains(normalizedLower, "附录") && len([]rune(normalized)) < 20 {
		return "appendix_title", 0
	}

	// 注释标题识别
	normalizedNoSpace := strings.ReplaceAll(normalized, " ", "")
	normalizedNoSpace = strings.ReplaceAll(normalizedNoSpace, "\u3000", "")
	if (normalizedNoSpace == "注释" || normalizedNoSpace == "注释：" || normalizedNoSpace == "注释:") && len([]rune(normalized)) < 15 {
		return "notes_title", 0
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

	// 图标题识别（"图1.1 xxx" 或 "图 1-1 xxx"）
	if matched, _ := regexp.MatchString(`^图\s*[\d]+[.\-][\d]+`, trimmed); matched {
		return "figure_caption", 0
	}
	if matched, _ := regexp.MatchString(`^Figure\s*[\d]+[.\-][\d]+`, trimmed); matched {
		return "figure_caption", 0
	}

	// 表标题识别（"表2.1 xxx" 或 "表 2-1 xxx"）
	if matched, _ := regexp.MatchString(`^表\s*[\d]+[.\-][\d]+`, trimmed); matched {
		return "table_caption", 0
	}
	if matched, _ := regexp.MatchString(`^Table\s*[\d]+[.\-][\d]+`, trimmed); matched {
		return "table_caption", 0
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
	level1Patterns := []string{
		`^[一二三四五六七八九十]+[、.]`,
		`^[0-9]+[、.]`,
		`^第[一二三四五六七八九十]+[章节]`,
		`^[0-9]+\s+[^\d]`,
	}

	level2Patterns := []string{
		`^[0-9]+\.[0-9]`,
		`^[（(][一二三四五六七八九十]+[)）]`,
	}

	reL4Num := regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+\s*`)
	reL3Num := regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+\s*`)
	reL3Paren := regexp.MustCompile(`^[（(][0-9]+[)）]`)

	if reL4Num.MatchString(text) {
		return 4
	}
	if reL3Num.MatchString(text) {
		return 3
	}
	if loc := reL3Paren.FindStringIndex(text); loc != nil {
		rest := []rune(strings.TrimSpace(text[loc[1]:]))
		if len(rest) <= 20 {
			return 3
		}
		return 0
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

	// 解析页眉页脚距离，兼容多种结构：
	// a) page_setup.header = { "distance": 1.5 }
	// b) page_setup.header_distance = 1.5
	// c) page_setup.header = 1.5 (直接数值)
	if header, ok := pageSetupRules["header"].(map[string]interface{}); ok {
		if v := parseCmValue(header["distance"]); v > 0 {
			headerDistance = v
		}
	} else if v := parseCmValue(pageSetupRules["header_distance"]); v > 0 {
		headerDistance = v
	} else if v := parseCmValue(pageSetupRules["header"]); v > 0 {
		headerDistance = v
	}
	if footer, ok := pageSetupRules["footer"].(map[string]interface{}); ok {
		if v := parseCmValue(footer["distance"]); v > 0 {
			footerDistance = v
		}
	} else if v := parseCmValue(pageSetupRules["footer_distance"]); v > 0 {
		footerDistance = v
	} else if v := parseCmValue(pageSetupRules["footer"]); v > 0 {
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

// extractCoverInfo 从封面表格中提取学院、专业、论文标题等信息
// 返回 map，key 可能有 "学院"、"专业"、"标题"、"题目" 等
func (p *EnhancedProcessor) extractCoverInfo(doc *document.Document) map[string]string {
	info := make(map[string]string)
	// 仅扫描前 5 张表格（封面一般是第 1 张）
	for ti, tbl := range doc.Tables() {
		if ti >= 5 {
			break
		}
		for _, row := range tbl.Rows() {
			cells := row.Cells()
			// 常见格式：label 单元格 | value 单元格
			if len(cells) >= 2 {
				label := strings.TrimSpace(p.extractCellText(cells[0]))
				value := strings.TrimSpace(p.extractCellText(cells[1]))
				if value == "" {
					continue
				}
				for _, key := range []string{"学院", "专业", "题目", "标题", "姓名", "学号", "班级"} {
					if strings.Contains(label, key) {
						info[key] = value
						log.Printf("[封面解析] %s → %q", key, value)
					}
				}
			}
			// 也处理单列合并行：整行只有一格且很长（可能是题目）
			if len(cells) == 1 {
				text := strings.TrimSpace(p.extractCellText(cells[0]))
				if len([]rune(text)) >= 6 && len([]rune(text)) <= 60 && !strings.ContainsAny(text, "：:") {
					if _, exists := info["题目"]; !exists {
						info["题目候选"] = text
					}
				}
			}
		}
	}
	return info
}

// extractCellText 提取表格单元格内所有段落文本
func (p *EnhancedProcessor) extractCellText(cell document.Cell) string {
	var sb strings.Builder
	for _, para := range cell.Paragraphs() {
		sb.WriteString(p.extractParagraphText(para))
	}
	return sb.String()
}

// applyHeaderFooter 应用页眉、页脚和页码设置
func (p *EnhancedProcessor) applyHeaderFooter(doc *document.Document, rules map[string]interface{}) error {
	pageSetupRules, _ := rules["page_setup"].(map[string]interface{})

	section := doc.BodySection()

	sectPr := section.X()
	if sectPr != nil {
		sectPr.EG_HdrFtrReferences = nil
	}

	// ── 页眉 ──
	// 优先从 page_setup.header 取，其次从顶层 header 取
	headerRules, _ := pageSetupRules["header"].(map[string]interface{})
	if headerRules == nil {
		headerRules, _ = rules["header"].(map[string]interface{})
	}
	if headerRules == nil {
		headerRules = make(map[string]interface{})
	}
	// 自动从封面解析学院/专业并注入页眉内容
	coverInfo := p.extractCoverInfo(doc)
	// 从班级字段提取入学年份（如"2022级护理学5班" → "2022"）
	gradeYear := ""
	if banJi, ok := coverInfo["班级"]; ok && banJi != "" {
		for i, r := range banJi {
			if r >= '0' && r <= '9' {
				end := i + 4
				if end <= len(banJi) {
					candidate := banJi[i:end]
					valid := true
					for _, c := range candidate {
						if c < '0' || c > '9' {
							valid = false
							break
						}
					}
					if valid {
						gradeYear = candidate
						log.Printf("[页眉] 从班级 %q 提取年级: %s", banJi, gradeYear)
						break
					}
				}
			}
		}
	}
	if _, hasContent := headerRules["content"]; !hasContent || headerRules["content"] == "" {
		// 规则中无页眉内容时，用封面信息自动构建完整页眉
		major, _ := coverInfo["专业"]
		college, _ := coverInfo["学院"]
		if college == "" {
			college = "重庆人文科技学院"
		}
		if major != "" {
			headerText := college
			if gradeYear != "" {
				headerText += gradeYear + "届"
			}
			headerText += major + "专业本科毕业论文（设计）"
			headerRules["content"] = headerText
			log.Printf("[页眉] 自动构建页眉: %q", headerText)
		}
	} else {
		// 规则中有页眉内容时，替换所有占位符
		content, _ := headerRules["content"].(string)
		if college, ok := coverInfo["学院"]; ok && college != "" {
			content = strings.ReplaceAll(content, "{学院}", college)
			content = strings.ReplaceAll(content, "xx学院", college)
			content = strings.ReplaceAll(content, "XX学院", college)
		}
		if major, ok := coverInfo["专业"]; ok && major != "" {
			content = strings.ReplaceAll(content, "{专业}", major)
			content = strings.ReplaceAll(content, "xx专业", major)
			content = strings.ReplaceAll(content, "XX专业", major)
		}
		// 替换届/年级占位符
		if gradeYear != "" {
			content = strings.ReplaceAll(content, "{届}", gradeYear+"届")
			content = strings.ReplaceAll(content, "XX届", gradeYear+"届")
			content = strings.ReplaceAll(content, "xx届", gradeYear+"届")
		}
		if title, ok := coverInfo["题目"]; ok && title != "" {
			content = strings.ReplaceAll(content, "{题目}", title)
			content = strings.ReplaceAll(content, "{论文题目}", title)
		}
		if content != headerRules["content"] {
			log.Printf("[页眉] 占位符替换: %q → %q", headerRules["content"], content)
		}
		headerRules["content"] = content
	}
	p.setupHeader(doc, section, headerRules)

	// ── 页脚 & 页码 ──
	// 优先从 page_setup 取，其次从顶层取
	pageNumRules, _ := pageSetupRules["page_number"].(map[string]interface{})
	if pageNumRules == nil {
		pageNumRules, _ = rules["page_number"].(map[string]interface{})
	}
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
// applyHeaderFooter 会先清除 sectPr.EG_HdrFtrReferences，所以这里直接创建新页脚
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

	ftr := doc.AddFooter()
	section.SetFooter(ftr, wml.ST_HdrFtrDefault)

	para := ftr.AddParagraph()

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

	parts := p.parsePageNumberFormatParts(format)
	for _, part := range parts {
		switch part.fieldType {
		case "PAGE":
			p.addPageFieldToParagraph(para, fontName, fontSize)
		case "NUMPAGES":
			p.addNumPagesFieldToParagraph(para, fontName, fontSize)
		default:
			if part.text != "" {
				r := para.AddRun()
				r.AddText(part.text)
				p.setRunFont(r, fontName, fontSize, false)
			}
		}
	}

	sectPr := section.X()
	if sectPr.PgNumType == nil {
		sectPr.PgNumType = wml.NewCT_PageNumber()
	}
	startVal := int64(1)
	sectPr.PgNumType.StartAttr = &startVal

	log.Printf("[页脚] 已设置页码: font=%s size=%.1fpt position=%s format=%q", fontName, fontSize, position, format)
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

// addNumPagesFieldToParagraph 在段落中插入 NUMPAGES 域字段（总页数）
func (p *EnhancedProcessor) addNumPagesFieldToParagraph(para document.Paragraph, fontName string, fontSize float64) {
	numField := wml.NewCT_SimpleField()
	numField.InstrAttr = " NUMPAGES "

	pContent := wml.NewEG_PContent()
	rContent := wml.NewEG_ContentRunContent()
	numRun := wml.NewCT_R()
	numText := wml.NewCT_Text()
	numText.Content = "1"
	numRun.EG_RunInnerContent = append(numRun.EG_RunInnerContent, &wml.EG_RunInnerContent{T: numText})

	numRPr := wml.NewCT_RPr()
	numRPr.RFonts = wml.NewCT_Fonts()
	fontPtr := p.getCachedFontName(fontName)
	numRPr.RFonts.AsciiAttr = fontPtr
	numRPr.RFonts.EastAsiaAttr = fontPtr
	numRPr.RFonts.HAnsiAttr = fontPtr
	halfPt := uint64(fontSize * 2)
	numRPr.Sz = wml.NewCT_HpsMeasure()
	numRPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	numRPr.SzCs = wml.NewCT_HpsMeasure()
	numRPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	numRun.RPr = numRPr

	rContent.R = numRun
	pContent.EG_ContentRunContent = append(pContent.EG_ContentRunContent, rContent)
	numField.EG_PContent = append(numField.EG_PContent, pContent)

	para.X().EG_PContent = append(para.X().EG_PContent, &wml.EG_PContent{FldSimple: []*wml.CT_SimpleField{numField}})
}

type pageFormatPart struct {
	fieldType string // "PAGE", "NUMPAGES", or "" for literal text
	text      string
}

// parsePageNumberFormatParts 将页码格式字符串拆分为文本和域字段部分。
// 支持格式如：
//   - "第×页 共×页" → [text:"第", PAGE, text:"页 共", NUMPAGES, text:"页"]
//   - "-1-"        → [text:"-", PAGE, text:"-"]
//   - "第1页"      → [text:"第", PAGE, text:"页"]
//   - "1"          → [PAGE]
func (p *EnhancedProcessor) parsePageNumberFormatParts(format string) []pageFormatPart {
	f := strings.TrimSpace(format)
	if f == "" {
		return []pageFormatPart{{fieldType: "PAGE"}}
	}

	// 占位符正则：匹配 ×、数字序列 或 PAGE/NUMPAGES 关键词
	re := regexp.MustCompile(`×+|\d+|(?i:NUMPAGES)|(?i:PAGE)`)
	matches := re.FindAllStringIndex(f, -1)
	if len(matches) == 0 {
		return []pageFormatPart{{fieldType: "PAGE"}}
	}

	var parts []pageFormatPart
	cursor := 0
	pageFieldCount := 0

	for _, m := range matches {
		if m[0] > cursor {
			parts = append(parts, pageFormatPart{text: f[cursor:m[0]]})
		}
		token := f[m[0]:m[1]]
		upper := strings.ToUpper(token)

		if upper == "NUMPAGES" {
			parts = append(parts, pageFormatPart{fieldType: "NUMPAGES"})
		} else {
			// ×、digits、PAGE → treat as PAGE; the second occurrence is NUMPAGES
			pageFieldCount++
			if pageFieldCount <= 1 {
				parts = append(parts, pageFormatPart{fieldType: "PAGE"})
			} else {
				parts = append(parts, pageFormatPart{fieldType: "NUMPAGES"})
			}
		}
		cursor = m[1]
	}
	if cursor < len(f) {
		parts = append(parts, pageFormatPart{text: f[cursor:]})
	}

	return parts
}

// parsePageNumberFormat 解析页码格式字符串，提取前缀和后缀（向后兼容）
func (p *EnhancedProcessor) parsePageNumberFormat(format string) (string, string) {
	f := strings.TrimSpace(format)
	if f == "" {
		return "", ""
	}

	re := regexp.MustCompile(`×+|\d+|(?i)PAGE`)
	loc := re.FindStringIndex(f)
	if loc == nil {
		return "-", "-"
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

// applyTitleFormatting 应用标题格式（区分正标题和副标题）
func (p *EnhancedProcessor) applyTitleFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	titleRules, ok := rules["title"].(map[string]interface{})
	if !ok {
		return nil
	}

	subtitleRules := titleRules
	if sub, ok := titleRules["subtitle"].(map[string]interface{}); ok {
		subtitleRules = sub
	}

	for _, para := range paragraphs {
		text := strings.TrimSpace(p.extractParagraphText(para))
		if strings.HasPrefix(text, "——") || strings.HasPrefix(text, "--") || strings.HasPrefix(text, "—") {
			log.Printf("[标题] 副标题: %s → 使用 subtitle 规则", text[:min(20, len(text))])
			p.applyParagraphFormatting(para, subtitleRules)
		} else {
			p.applyParagraphFormatting(para, titleRules)
		}
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

// setPageBreakBefore 设置段落的"另起页"属性
func (p *EnhancedProcessor) setPageBreakBefore(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.PageBreakBefore = wml.NewCT_OnOff()
}

// applyReferencesTitleFormatting 应用参考文献标题格式（"参考文献"四个字）
func (p *EnhancedProcessor) applyReferencesTitleFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	titleRules := map[string]interface{}{
		"font_name": "黑体",
		"font_size": "小三",
		"bold":      true,
		"alignment": "center",
	}
	for _, key := range []string{"references", "reference"} {
		if refRules, ok := rules[key].(map[string]interface{}); ok {
			// 兼容 label 和 title 两种键名
			for _, subKey := range []string{"label", "title"} {
				if t, ok := refRules[subKey].(map[string]interface{}); ok {
					for k, v := range t {
						titleRules[k] = v
					}
					break
				}
			}
			break
		}
	}

	needPageBreak := true
	if pb, ok := titleRules["new_page"].(bool); ok {
		needPageBreak = pb
	} else if pb, ok := titleRules["page_break"].(bool); ok {
		needPageBreak = pb
	}

	log.Printf("[参考文献标题] 规则: font=%v size=%v align=%v bold=%v page_break=%v", titleRules["font_name"], titleRules["font_size"], titleRules["alignment"], titleRules["bold"], needPageBreak)
	for i, para := range paragraphs {
		if i == 0 && needPageBreak {
			p.setPageBreakBefore(para)
		}
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
		// 兼容 label 和 title 两种键名
		for _, subKey := range []string{"label", "title"} {
			if t, ok := ack[subKey].(map[string]interface{}); ok {
				for k, v := range t {
					titleRules[k] = v
				}
				break
			}
		}
	}

	needPageBreak := true
	if pb, ok := titleRules["new_page"].(bool); ok {
		needPageBreak = pb
	} else if pb, ok := titleRules["page_break"].(bool); ok {
		needPageBreak = pb
	}

	log.Printf("[致谢标题] 规则: font=%v size=%v align=%v bold=%v page_break=%v", titleRules["font_name"], titleRules["font_size"], titleRules["alignment"], titleRules["bold"], needPageBreak)
	for i, para := range paragraphs {
		if i == 0 && needPageBreak {
			p.setPageBreakBefore(para)
		}
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

// applyAppendixTitleFormatting 应用附录标题格式
func (p *EnhancedProcessor) applyAppendixTitleFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	titleRules := map[string]interface{}{
		"font_name":  "黑体",
		"font_size":  "小三",
		"bold":       true,
		"alignment":  "center",
		"page_break": true,
	}
	if appendix, ok := rules["appendix"].(map[string]interface{}); ok {
		// 兼容 label 和 title 两种键名
		for _, subKey := range []string{"label", "title"} {
			if t, ok := appendix[subKey].(map[string]interface{}); ok {
				for k, v := range t {
					titleRules[k] = v
				}
				break
			}
		}
	}

	needPageBreak := true
	if pb, ok := titleRules["new_page"].(bool); ok {
		needPageBreak = pb
	} else if pb, ok := titleRules["page_break"].(bool); ok {
		needPageBreak = pb
	}

	log.Printf("[附录标题] 规则: font=%v size=%v bold=%v page_break=%v", titleRules["font_name"], titleRules["font_size"], titleRules["bold"], needPageBreak)
	for i, para := range paragraphs {
		if i == 0 && needPageBreak {
			p.setPageBreakBefore(para)
		}
		p.applyParagraphFormatting(para, titleRules)
	}
	return nil
}

// applyAppendixContentFormatting 应用附录正文格式
func (p *EnhancedProcessor) applyAppendixContentFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	contentRules := map[string]interface{}{
		"font_name":         "宋体",
		"font_size":         "五号",
		"alignment":         "justify",
		"first_line_indent": "2",
		"line_space":        1.5,
	}
	if appendix, ok := rules["appendix"].(map[string]interface{}); ok {
		if content, ok := appendix["content"].(map[string]interface{}); ok {
			for k, v := range content {
				contentRules[k] = v
			}
		}
	}
	log.Printf("[附录正文] 规则: font=%v size=%v", contentRules["font_name"], contentRules["font_size"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, contentRules)
	}
	return nil
}

// applyNotesTitleFormatting 应用注释标题格式
func (p *EnhancedProcessor) applyNotesTitleFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	titleRules := map[string]interface{}{
		"font_name":  "黑体",
		"font_size":  "小三",
		"bold":       true,
		"alignment":  "center",
		"page_break": true,
	}
	if notes, ok := rules["notes"].(map[string]interface{}); ok {
		// 兼容 label 和 title 两种键名
		for _, subKey := range []string{"label", "title"} {
			if t, ok := notes[subKey].(map[string]interface{}); ok {
				for k, v := range t {
					titleRules[k] = v
				}
				break
			}
		}
	}

	needPageBreak := true
	if pb, ok := titleRules["new_page"].(bool); ok {
		needPageBreak = pb
	} else if pb, ok := titleRules["page_break"].(bool); ok {
		needPageBreak = pb
	}

	log.Printf("[注释标题] 规则: font=%v size=%v bold=%v page_break=%v", titleRules["font_name"], titleRules["font_size"], titleRules["bold"], needPageBreak)
	for i, para := range paragraphs {
		if i == 0 && needPageBreak {
			p.setPageBreakBefore(para)
		}
		p.applyParagraphFormatting(para, titleRules)
	}
	return nil
}

// applyNotesContentFormatting 应用注释正文格式
func (p *EnhancedProcessor) applyNotesContentFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	contentRules := map[string]interface{}{
		"font_name":  "宋体",
		"font_size":  "五号",
		"alignment":  "left",
		"line_space": 1.5,
	}
	if notes, ok := rules["notes"].(map[string]interface{}); ok {
		if content, ok := notes["content"].(map[string]interface{}); ok {
			for k, v := range content {
				contentRules[k] = v
			}
		}
	}
	log.Printf("[注释正文] 规则: font=%v size=%v", contentRules["font_name"], contentRules["font_size"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, contentRules)
	}
	return nil
}

// applyFigureCaptionFormatting 应用图标题格式
func (p *EnhancedProcessor) applyFigureCaptionFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	captionRules := map[string]interface{}{
		"font_name": "宋体",
		"font_size": "五号",
		"alignment": "center",
	}
	if figure, ok := rules["figure"].(map[string]interface{}); ok {
		if caption, ok := figure["caption"].(map[string]interface{}); ok {
			for k, v := range caption {
				captionRules[k] = v
			}
		}
	}
	log.Printf("[图标题] 规则: font=%v size=%v align=%v", captionRules["font_name"], captionRules["font_size"], captionRules["alignment"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, captionRules)
	}
	return nil
}

// applyTableCaptionFormatting 应用表标题格式
func (p *EnhancedProcessor) applyTableCaptionFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	captionRules := map[string]interface{}{
		"font_name": "宋体",
		"font_size": "五号",
		"alignment": "center",
	}
	if table, ok := rules["table"].(map[string]interface{}); ok {
		if caption, ok := table["caption"].(map[string]interface{}); ok {
			for k, v := range caption {
				captionRules[k] = v
			}
		}
	}
	log.Printf("[表标题] 规则: font=%v size=%v align=%v", captionRules["font_name"], captionRules["font_size"], captionRules["alignment"])
	for _, para := range paragraphs {
		p.applyParagraphFormatting(para, captionRules)
	}
	return nil
}

// applyTableFormatting 对文档中数据表格内的段落应用格式
// 跳过：封面/原创性声明中的表格、流程图/装饰表格
func (p *EnhancedProcessor) applyTableFormatting(doc *document.Document, rules map[string]interface{}, skipParas map[*wml.CT_P]bool) {
	tableRules, _ := rules["table"].(map[string]interface{})
	innerRules := map[string]interface{}{
		"font_name": "宋体",
		"font_size": "五号",
	}
	if tableRules != nil {
		if inner, ok := tableRules["inner_text"].(map[string]interface{}); ok {
			for k, v := range inner {
				innerRules[k] = v
			}
		}
	}

	for _, table := range doc.Tables() {
		rows := table.Rows()
		if len(rows) < 2 {
			continue
		}

		// 检查表格是否属于封面/原创性声明区域：如果任一单元格段落在跳过集合中，整个表格跳过
		isCoverTable := false
		if len(skipParas) > 0 {
			for _, row := range rows {
				for _, cell := range row.Cells() {
					for _, para := range cell.Paragraphs() {
						if skipParas[para.X()] {
							isCoverTable = true
							break
						}
					}
					if isCoverTable {
						break
					}
				}
				if isCoverTable {
					break
				}
			}
		}
		if isCoverTable {
			log.Printf("[表格] 跳过封面/声明区域表格")
			continue
		}

		// 启发式判断：检查是否为数据表格
		totalCells := 0
		nonEmptyCells := 0
		longTextCells := 0
		for _, row := range rows {
			for _, cell := range row.Cells() {
				totalCells++
				cellText := ""
				for _, para := range cell.Paragraphs() {
					cellText += p.extractParagraphText(para)
				}
				cellText = strings.TrimSpace(cellText)
				if cellText != "" {
					nonEmptyCells++
				}
				if len([]rune(cellText)) > 10 {
					longTextCells++
				}
			}
		}

		if totalCells > 0 {
			fillRate := float64(nonEmptyCells) / float64(totalCells)
			longRate := float64(longTextCells) / float64(totalCells)
			if fillRate < 0.3 || (fillRate < 0.5 && longRate < 0.1) {
				log.Printf("[表格] 跳过疑似流程图/装饰表格 (cells=%d, filled=%.0f%%, long=%.0f%%)", totalCells, fillRate*100, longRate*100)
				continue
			}
		}

		for _, row := range rows {
			for _, cell := range row.Cells() {
				for _, para := range cell.Paragraphs() {
					text := strings.TrimSpace(p.extractParagraphText(para))
					if text == "" {
						continue
					}
					p.applyParagraphFormatting(para, innerRules)
				}
			}
		}
	}
}

// chineseFontToEnglish 中文字体名 → 英文字体名映射
var chineseFontToEnglish = map[string]string{
	"宋体":   "SimSun",
	"黑体":   "SimHei",
	"楷体":   "KaiTi",
	"仿宋":   "FangSong",
	"微软雅黑": "Microsoft YaHei",
	"新宋体":  "NSimSun",
	"隶书":   "LiSu",
	"幼圆":   "YouYuan",
}

// getEnglishFontName 获取中文字体的英文名（如果有映射则返回英文名，否则返回原名）
func getEnglishFontName(chineseName string) string {
	if en, ok := chineseFontToEnglish[chineseName]; ok {
		return en
	}
	return chineseName
}

// applyTableFormattingWithSpec 使用模板 ParagraphFormatSpec 修正表格内文字格式
// 只修改字体和字号，不动对齐/行距，保持表格原有排版
func (p *EnhancedProcessor) applyTableFormattingWithSpec(doc *document.Document, bodySpec ParagraphFormatSpec, skipParas map[*wml.CT_P]bool) {
	applier := NewAIFormatApplier(p)
	// 表格内格式专用规范：固定宋体小四（12pt），不加粗
	// 不依赖 bodySpec（bodySpec 经常被模板黑体/封面段落污染）
	tableFont := bodySpec.FontEastAsia
	if tableFont == "" || tableFont == "黑体" || tableFont == "SimHei" {
		tableFont = "宋体"
	}
	tableSize := bodySpec.FontSizeHalfPt
	if tableSize == 0 || tableSize > 28 {
		tableSize = uint64(24) // 小四=12pt
	}
	tableSpec := ParagraphFormatSpec{
		FontEastAsia:   tableFont,
		FontAscii:      getEnglishFontName(tableFont),
		FontSizeHalfPt: tableSize,
		Bold:           false,
	}
	tableCount := 0
	for _, table := range doc.Tables() {
		rows := table.Rows()
		if len(rows) < 2 {
			continue
		}
		// 检查是否为封面/原创性声明表格
		isCoverTable := false
		for _, row := range rows {
			for _, cell := range row.Cells() {
				for _, para := range cell.Paragraphs() {
					if skipParas[para.X()] {
						isCoverTable = true
						break
					}
				}
				if isCoverTable {
					break
				}
			}
			if isCoverTable {
				break
			}
		}
		if isCoverTable {
			continue
		}
		// 应用格式
		for _, row := range rows {
			for _, cell := range row.Cells() {
				for _, para := range cell.Paragraphs() {
					text := strings.TrimSpace(p.extractParagraphText(para))
					if text == "" {
						continue
					}
					applier.ApplyFontOnlyToTableCellPara(para, tableSpec)
					tableCount++
				}
			}
		}
	}
	log.Printf("[模板方案][步骤5] 表格内文字修正 %d 处 (font=%q size=%.1fpt)", tableCount, tableSpec.FontEastAsia, tableSpec.FontSizePt())
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
// 如果段落以"摘要"开头，则对"摘要："标签和内容分别应用不同格式
func (p *EnhancedProcessor) applyAbstractFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	labelRules := map[string]interface{}{
		"font_name": "黑体",
		"font_size": "小三",
		"bold":      true,
	}
	contentRules := map[string]interface{}{
		"font_name":         "宋体",
		"font_size":         "小四",
		"alignment":         "justify",
		"first_line_indent": "2",
	}
	if abstract, ok := rules["abstract"].(map[string]interface{}); ok {
		if label, ok := abstract["label"].(map[string]interface{}); ok {
			for k, v := range label {
				labelRules[k] = v
			}
		}
		if content, ok := abstract["content"].(map[string]interface{}); ok {
			for k, v := range content {
				contentRules[k] = v
			}
		}
	}
	log.Printf("[摘要正文] 标签规则: font=%v size=%v bold=%v", labelRules["font_name"], labelRules["font_size"], labelRules["bold"])
	log.Printf("[摘要正文] 内容规则: font=%v size=%v align=%v", contentRules["font_name"], contentRules["font_size"], contentRules["alignment"])
	for _, para := range paragraphs {
		text := p.extractParagraphText(para)
		normalized := normalizeChineseText(text)
		if strings.HasPrefix(normalized, "摘要") {
			p.applyLabelContentParagraphFormatting(para, "摘要", labelRules, contentRules)
		} else {
			p.applyParagraphFormatting(para, contentRules)
		}
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

// applyLabelContentParagraphFormatting 通用的标签+内容分段格式化
// 用于摘要("摘要：xxx")、英文摘要("Abstract: xxx")等段落
func (p *EnhancedProcessor) applyLabelContentParagraphFormatting(para document.Paragraph, labelPrefix string, labelRules, contentRules map[string]interface{}) {
	paraProps := para.Properties()

	if pPr := para.X().PPr; pPr != nil && pPr.PStyle != nil {
		pPr.PStyle = nil
	}
	for _, run := range para.Runs() {
		if rPr := run.X().RPr; rPr != nil && rPr.RStyle != nil {
			rPr.RStyle = nil
		}
	}

	alignment := ""
	if a, ok := contentRules["alignment"].(string); ok {
		alignment = a
	}
	if a, ok := labelRules["alignment"].(string); ok && a != "" {
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

	if lineSpaceRaw, ok := contentRules["line_space"]; ok {
		p.applyLineSpacingToParagraph(para, lineSpaceRaw)
	} else if lineSpaceRaw, ok := labelRules["line_space"]; ok {
		p.applyLineSpacingToParagraph(para, lineSpaceRaw)
	}

	if fli := contentRules["first_line_indent"]; fli != nil {
		var indentChars float64
		switch v := fli.(type) {
		case float64:
			indentChars = v
		case string:
			indentChars = p.parseFirstLineIndentChars(v)
		}
		if indentChars > 0 {
			fontSize := p.resolveActualFontSizePt(contentRules)
			indentPt := indentChars * fontSize
			paraProps.SetFirstLineIndent(measurement.Distance(indentPt) * measurement.Point)
		}
	}

	labelEnded := false
	for _, run := range para.Runs() {
		text := run.Text()
		if !labelEnded {
			if idx := findLabelEnd(text, labelPrefix); idx >= 0 {
				labelEnded = true
				if idx == len(text) {
					p.applyRunFormatting(run, labelRules)
				} else {
					labelPart := text[:idx]
					if float64(len([]rune(labelPart)))/float64(len([]rune(text))) >= 0.5 {
						p.applyRunFormatting(run, labelRules)
					} else {
						p.applyRunFormatting(run, contentRules)
					}
				}
			} else {
				normalizedText := normalizeChineseText(text)
				if strings.Contains(normalizedText, labelPrefix) || strings.Contains(strings.ToLower(normalizedText), strings.ToLower(labelPrefix)) {
					p.applyRunFormatting(run, labelRules)
				} else {
					p.applyRunFormatting(run, labelRules)
				}
			}
		} else {
			p.applyRunFormatting(run, contentRules)
		}
	}

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
// 如果段落以"Abstract"开头，则对标签和内容分别应用不同格式
func (p *EnhancedProcessor) applyEnglishAbstractFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	labelRules, contentRules := resolveEnglishAbstractRules(rules)
	log.Printf("[英文摘要正文] 标签规则: font=%v size=%v bold=%v", labelRules["font_name"], labelRules["font_size"], labelRules["bold"])
	log.Printf("[英文摘要正文] 内容规则: font=%v size=%v align=%v", contentRules["font_name"], contentRules["font_size"], contentRules["alignment"])
	for _, para := range paragraphs {
		text := p.extractParagraphText(para)
		textLower := strings.ToLower(strings.TrimSpace(text))
		if strings.HasPrefix(textLower, "abstract") {
			p.applyLabelContentParagraphFormatting(para, "Abstract", labelRules, contentRules)
		} else {
			p.applyParagraphFormatting(para, contentRules)
		}
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

	// 统一处理 new_page / page_break 属性 → w:pageBreakBefore
	if np, ok := rules["new_page"].(bool); ok && np {
		p.setPageBreakBefore(para)
	} else if pb, ok := rules["page_break"].(bool); ok && pb {
		p.setPageBreakBefore(para)
	}

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

		// 段落默认字体：EastAsia 用中文名，Ascii/HAnsi/Cs 用英文名
		if fontName, ok := rules["font_name"].(string); ok {
			if paraRPr.RFonts == nil {
				paraRPr.RFonts = wml.NewCT_Fonts()
			}
			eastAsiaPtr := p.getCachedFontName(fontName)
			englishName := getEnglishFontName(fontName)
			asciiPtr := p.getCachedFontName(englishName)
			paraRPr.RFonts.EastAsiaAttr = eastAsiaPtr
			paraRPr.RFonts.AsciiAttr = asciiPtr
			paraRPr.RFonts.HAnsiAttr = asciiPtr
			paraRPr.RFonts.CsAttr = asciiPtr
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
		// EastAsia 用中文名，Ascii/HAnsi/Cs 用英文名，避免 WPS 显示 "黑体;SimHei"
		eastAsiaPtr := p.getCachedFontName(fontName)
		englishName := getEnglishFontName(fontName)
		asciiPtr := p.getCachedFontName(englishName)
		rPr.RFonts.EastAsiaAttr = eastAsiaPtr
		rPr.RFonts.AsciiAttr = asciiPtr
		rPr.RFonts.HAnsiAttr = asciiPtr
		rPr.RFonts.CsAttr = asciiPtr
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

// ---------------------------------------------------------------------------
// Exported wrappers for template filler integration
// ---------------------------------------------------------------------------

// OpenDocument opens a DOCX file and returns the unioffice document.
func OpenDocument(docPath string) (*document.Document, error) {
	return document.Open(docPath)
}

// ClassifyParagraphsExport exposes the classification logic for external callers.
func (p *EnhancedProcessor) ClassifyParagraphsExport(paragraphs []document.Paragraph) map[string][]document.Paragraph {
	return p.classifyParagraphs(paragraphs)
}

// ExtractParagraphTextExport exposes paragraph text extraction for external callers.
func (p *EnhancedProcessor) ExtractParagraphTextExport(para document.Paragraph) string {
	return p.extractParagraphText(para)
}

// ═══════════════════════════════════════════════════════════════════════
// 自动验证 + 自纠正循环
// ═══════════════════════════════════════════════════════════════════════

// verifyAndAutoFix 在格式修正完成后，逐段检查关键格式属性，发现偏差自动修正
func (p *EnhancedProcessor) verifyAndAutoFix(doc *document.Document, rules map[string]interface{}) int {
	log.Println("[验证] ================= 开始自动验证+纠正 =================")

	classified := p.classifyParagraphs(doc.Paragraphs())
	totalFixes := 0

	type verifyTarget struct {
		category string
		getRules func() map[string]interface{}
	}

	// 定义需要验证的段落类别和对应规则的提取方式
	targets := []verifyTarget{
		{"body", func() map[string]interface{} {
			if r, ok := rules["body"].(map[string]interface{}); ok {
				return r
			}
			return nil
		}},
		{"title", func() map[string]interface{} {
			if r, ok := rules["title"].(map[string]interface{}); ok {
				return r
			}
			return nil
		}},
		{"references_title", func() map[string]interface{} {
			for _, key := range []string{"references", "reference"} {
				if ref, ok := rules[key].(map[string]interface{}); ok {
					for _, sub := range []string{"label", "title"} {
						if t, ok := ref[sub].(map[string]interface{}); ok {
							return t
						}
					}
				}
			}
			return nil
		}},
		{"acknowledgements_title", func() map[string]interface{} {
			if ack, ok := rules["acknowledgements"].(map[string]interface{}); ok {
				for _, sub := range []string{"label", "title"} {
					if t, ok := ack[sub].(map[string]interface{}); ok {
						return t
					}
				}
			}
			return nil
		}},
	}

	for _, target := range targets {
		paras, exists := classified[target.category]
		if !exists || len(paras) == 0 {
			continue
		}
		expectedRules := target.getRules()
		if expectedRules == nil {
			continue
		}

		for _, para := range paras {
			fixes := p.verifyParagraphFormat(para, expectedRules, target.category)
			totalFixes += fixes
		}
	}

	log.Printf("[验证] ================= 验证完成，共纠正 %d 处 =================", totalFixes)
	return totalFixes
}

// verifyParagraphFormat 验证单个段落的格式，发现偏差自动修正，返回修正数
func (p *EnhancedProcessor) verifyParagraphFormat(para document.Paragraph, expectedRules map[string]interface{}, category string) int {
	fixes := 0

	// 跳过封面段落
	text := strings.TrimSpace(p.extractParagraphText(para))
	if text == "" {
		return 0
	}

	// 检查每个 Run 的字体和字号
	expectedFont, _ := expectedRules["font_name"].(string)
	expectedSizePt := p.resolveActualFontSizePt(expectedRules)
	expectedBold, hasBoldRule := expectedRules["bold"].(bool)

	for _, run := range para.Runs() {
		runText := run.Text()
		if strings.TrimSpace(runText) == "" {
			continue
		}

		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}

		// 验证字体
		if expectedFont != "" {
			actualEastAsia := ""
			if rPr.RFonts != nil && rPr.RFonts.EastAsiaAttr != nil {
				actualEastAsia = *rPr.RFonts.EastAsiaAttr
			}
			if actualEastAsia != expectedFont {
				eastAsiaPtr := p.getCachedFontName(expectedFont)
				englishName := getEnglishFontName(expectedFont)
				asciiPtr := p.getCachedFontName(englishName)
				if rPr.RFonts == nil {
					rPr.RFonts = wml.NewCT_Fonts()
				}
				rPr.RFonts.EastAsiaAttr = eastAsiaPtr
				rPr.RFonts.AsciiAttr = asciiPtr
				rPr.RFonts.HAnsiAttr = asciiPtr
				rPr.RFonts.CsAttr = asciiPtr
				fixes++
				if fixes <= 3 {
					log.Printf("[验证修正] %s: 字体 %q→%q (text=%q)", category, actualEastAsia, expectedFont, truncText(runText, 20))
				}
			}
		}

		// 验证字号
		if expectedSizePt > 0 {
			expectedHalfPt := uint64(expectedSizePt * 2)
			actualHalfPt := uint64(0)
			if rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
				actualHalfPt = *rPr.Sz.ValAttr.ST_UnsignedDecimalNumber
			}
			if actualHalfPt != expectedHalfPt {
				rPr.Sz = wml.NewCT_HpsMeasure()
				rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &expectedHalfPt
				rPr.SzCs = wml.NewCT_HpsMeasure()
				rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &expectedHalfPt
				fixes++
				if fixes <= 3 {
					log.Printf("[验证修正] %s: 字号 %d→%d half-pt (text=%q)", category, actualHalfPt, expectedHalfPt, truncText(runText, 20))
				}
			}
		}

		// 验证加粗
		if hasBoldRule {
			actualBold := rPr.B != nil
			if actualBold != expectedBold {
				if expectedBold {
					rPr.B = wml.NewCT_OnOff()
					rPr.BCs = wml.NewCT_OnOff()
				} else {
					rPr.B = nil
					rPr.BCs = nil
				}
				fixes++
				if fixes <= 3 {
					log.Printf("[验证修正] %s: 加粗 %v→%v (text=%q)", category, actualBold, expectedBold, truncText(runText, 20))
				}
			}
		}
	}

	return fixes
}

func truncText(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "..."
	}
	return s
}
