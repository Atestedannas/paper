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

	"gitee.com/greatmusicians/unioffice/color"
	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

// EnhancedProcessor 增强的文档处理器，提供更精确的格式修正
type EnhancedProcessor struct {
	debug       bool
	fontNameMap map[string]*string // 缓存字体名，避免悬空指针
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

	if len(corrections) == 0 {
		log.Println("⚠️  没有修正规则，返回原文件")
		return docPath, nil
	}

	// 检查文件类型
	fileExt := strings.ToLower(filepath.Ext(docPath))
	if fileExt != ".docx" && fileExt != ".doc" {
		return p.handleUnsupportedFormat(docPath)
	}

	// 提取格式规则
	var formatRules map[string]interface{}
	for _, correction := range corrections {
		if rules, ok := correction["format_rules"]; ok {
			if rulesMap, ok := rules.(map[string]interface{}); ok {
				formatRules = rulesMap
				break
			}
		}
	}

	if formatRules == nil {
		log.Println("❌ 未找到有效的格式规则")
		return "", fmt.Errorf("未找到有效的格式规则")
	}

	// 规范化格式规则（支持中文键名）
	formatRules = p.normalizeFormatRules(formatRules)

	// 打开文档
	doc, err := document.Open(docPath)
	if err != nil {
		return "", fmt.Errorf("无法打开文档: %w", err)
	}
	defer doc.Close()

	// 执行精确格式修正
	if err := p.applyPreciseFormatting(doc, formatRules); err != nil {
		return "", fmt.Errorf("格式修正失败: %w", err)
	}

	// 生成输出文件路径
	outputPath := p.generateOutputPath(docPath)

	// 保存修正后的文档
	if err := doc.SaveToFile(outputPath); err != nil {
		return "", fmt.Errorf("保存文档失败: %w", err)
	}

	if p.debug {
		log.Printf("格式修正完成，输出文件: %s", outputPath)
	}

	return outputPath, nil
}

// applyPreciseFormatting 应用精确格式修正
func (p *EnhancedProcessor) applyPreciseFormatting(doc *document.Document, rules map[string]interface{}) error {

	// 1. 应用页面设置
	if err := p.applyPageSetup(doc, rules); err != nil {
	}

	// 2. 分析和分类段落
	paragraphs := doc.Paragraphs()
	classifiedParagraphs := p.classifyParagraphs(paragraphs)

	// 3. 应用标题格式
	if titleParas, exists := classifiedParagraphs["title"]; exists && len(titleParas) > 0 {
		p.applyTitleFormatting(titleParas, rules)
	}

	// 4. 应用各级标题格式
	for level := 1; level <= 3; level++ {
		key := fmt.Sprintf("heading_%d", level)
		if headings, exists := classifiedParagraphs[key]; exists && len(headings) > 0 {
			p.applyHeadingFormatting(headings, rules, level)
		}
	}

	// 5. 应用正文格式（注意：跳过封面内容）
	if bodyParas, exists := classifiedParagraphs["body"]; exists && len(bodyParas) > 0 {
		p.applyBodyFormatting(bodyParas, rules)
	}

	// 6. 应用摘要格式
	if abstractParas, exists := classifiedParagraphs["abstract"]; exists && len(abstractParas) > 0 {
		p.applyAbstractFormatting(abstractParas, rules)
	}

	// 7. 应用关键词格式
	if keywordsParas, exists := classifiedParagraphs["keywords"]; exists && len(keywordsParas) > 0 {
		p.applyKeywordsFormatting(keywordsParas, rules)
	}

	// 8. 应用参考文献格式
	if referencesParas, exists := classifiedParagraphs["references"]; exists && len(referencesParas) > 0 {
		p.applyReferencesFormatting(referencesParas, rules)
	}

	// 9. 封面内容跳过，不进行格式修改
	return nil
}

// getMapKeys 获取map的所有键（辅助函数）
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// classifyParagraphs 智能分类段落
func (p *EnhancedProcessor) classifyParagraphs(paragraphs []document.Paragraph) map[string][]document.Paragraph {
	classified := make(map[string][]document.Paragraph)

	// 首先收集所有非空段落及其类型
	type paraInfo struct {
		para     document.Paragraph
		text     string
		paraType string
		level    int // 标题级别，0表示非标题
	}

	var paraInfos []paraInfo
	for _, para := range paragraphs {
		text := p.extractParagraphText(para)
		if strings.TrimSpace(text) == "" {
			continue
		}
		paraType, level := p.intelligentClassifyParagraphWithLevel(text)
		paraInfos = append(paraInfos, paraInfo{
			para:     para,
			text:     text,
			paraType: paraType,
			level:    level,
		})
	}

	// 检测被分割的标题
	// 如果连续的短标题段落（少于20个字符）具有相同的级别，可能是同一个标题被分割了
	// 在这种情况下，只保留第一个段落作为标题，其他段落改为正文类型
	isSplitPart := make([]bool, len(paraInfos))
	for i := 0; i < len(paraInfos); i++ {
		if isSplitPart[i] {
			continue
		}

		current := paraInfos[i]

		// 检查是否是可能被分割的短标题
		if current.level > 0 && len([]rune(current.text)) < 20 {
			// 向后查找连续的同级别短段落
			j := i + 1
			combinedText := current.text
			for j < len(paraInfos) {
				next := paraInfos[j]
				// 检查是否是同级别的短段落
				if next.level == current.level && len([]rune(next.text)) < 20 {
					// 检查合并后的文本是否合理（不应该太长）
					if len([]rune(combinedText+next.text)) <= 60 {
						combinedText += next.text
						isSplitPart[j] = true // 标记为被分割的部分
						j++
						continue
					}
				}
				break
			}
		}
	}

	// 根据检测结果分类段落
	for i, info := range paraInfos {
		if isSplitPart[i] {
			// 被分割的标题部分改为正文类型，避免应用标题的段前段后间距
			classified["body"] = append(classified["body"], info.para)
		} else {
			classified[info.paraType] = append(classified[info.paraType], info.para)
		}
	}

	return classified
}

// intelligentClassifyParagraph 智能段落分类
func (p *EnhancedProcessor) intelligentClassifyParagraph(text string) string {
	paraType, _ := p.intelligentClassifyParagraphWithLevel(text)
	return paraType
}

// intelligentClassifyParagraphWithLevel 智能段落分类（返回类型和标题级别）
func (p *EnhancedProcessor) intelligentClassifyParagraphWithLevel(text string) (string, int) {
	text = strings.TrimSpace(text)
	textLower := strings.ToLower(text)

	// 封面识别（封面通常在文档开头，包含学校名称、毕业设计/论文等关键词）
	if p.isCoverPage(text) {
		return "cover", 0
	}

	// 各级标题识别（必须在论文标题识别之前，否则会误判）
	// 例如 "6.1 软件测试评估的指标和方法" 会匹配 "设计" 被误判为论文标题
	if level := p.detectHeadingLevel(text); level > 0 {
		return fmt.Sprintf("heading_%d", level), level
	}

	// 标题识别（通常居中，字数较少，不以句号结尾）
	// 只有在不是章节标题的情况下才判断是否为论文主标题
	if p.isTitleParagraph(text) {
		return "title", 0
	}

	// 摘要识别
	if strings.Contains(textLower, "摘要") || strings.Contains(textLower, "abstract") {
		if len(text) < 20 { // 摘要标题
			return "abstract_title", 0
		}
		return "abstract", 0
	}

	// 关键词识别
	if strings.Contains(textLower, "关键词") || strings.Contains(textLower, "keywords") {
		return "keywords", 0
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
		if top, ok := margins["top"].(float64); ok {
			marginTop = top
		}
		if bottom, ok := margins["bottom"].(float64); ok {
			marginBottom = bottom
		}
		if left, ok := margins["left"].(float64); ok {
			marginLeft = left
		}
		if right, ok := margins["right"].(float64); ok {
			marginRight = right
		}
	} else {
		// 扁平结构：margin_top / margin_bottom / margin_left / margin_right
		if top, ok := pageSetupRules["margin_top"].(float64); ok {
			marginTop = top
		}
		if bottom, ok := pageSetupRules["margin_bottom"].(float64); ok {
			marginBottom = bottom
		}
		if left, ok := pageSetupRules["margin_left"].(float64); ok {
			marginLeft = left
		}
		if right, ok := pageSetupRules["margin_right"].(float64); ok {
			marginRight = right
		}
	}

	// 解析页眉页脚距离
	if header, ok := pageSetupRules["header"].(map[string]interface{}); ok {
		if distance, ok := header["distance"].(float64); ok {
			headerDistance = distance
		}
	} else if d, ok := pageSetupRules["header_distance"].(float64); ok {
		headerDistance = d
	}
	if footer, ok := pageSetupRules["footer"].(map[string]interface{}); ok {
		if distance, ok := footer["distance"].(float64); ok {
			footerDistance = distance
		}
	} else if d, ok := pageSetupRules["footer_distance"].(float64); ok {
		footerDistance = d
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
		return fmt.Errorf("未找到 body 规则")
	}

	for _, para := range paragraphs {
		if err := p.applyParagraphFormatting(para, bodyRules); err != nil {
		}
	}

	return nil
}

// applyAbstractFormatting 应用摘要格式
func (p *EnhancedProcessor) applyAbstractFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	abstractRules, ok := rules["abstract"].(map[string]interface{})
	if !ok {
		return nil
	}

	for _, para := range paragraphs {
		if contentRules, ok := abstractRules["content"].(map[string]interface{}); ok {
			p.applyParagraphFormatting(para, contentRules)
		}
	}

	return nil
}

// applyKeywordsFormatting 应用关键词格式
func (p *EnhancedProcessor) applyKeywordsFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	keywordsRules, ok := rules["keywords"].(map[string]interface{})
	if !ok {
		return nil
	}

	for _, para := range paragraphs {
		if contentRules, ok := keywordsRules["content"].(map[string]interface{}); ok {
			p.applyParagraphFormatting(para, contentRules)
		}
	}

	return nil
}

// applyReferencesFormatting 应用参考文献格式
func (p *EnhancedProcessor) applyReferencesFormatting(paragraphs []document.Paragraph, rules map[string]interface{}) error {
	referencesRules, ok := rules["references"].(map[string]interface{})
	if !ok {
		return nil
	}

	for _, para := range paragraphs {
		if contentRules, ok := referencesRules["content"].(map[string]interface{}); ok {
			p.applyParagraphFormatting(para, contentRules)
		}
	}

	return nil
}

// applyParagraphFormatting 应用段落格式
func (p *EnhancedProcessor) applyParagraphFormatting(para document.Paragraph, rules map[string]interface{}) error {
	_ = p.extractParagraphText(para)

	paraProps := para.Properties()

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
			return nil
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
	if indent, ok := rules["first_line_indent"].(string); ok {
		// 获取实际字号
		fontSize := 12.0 // 默认小四号
		if fontSizeStr, ok := rules["font_size"].(string); ok {
			// 这里需要使用实际的 point 值，供缩进换算使用
			fontSize = p.parseFontSize(fontSizeStr)
		}
		if indentVal := p.parseIndentWithFontSize(indent, fontSize); indentVal > 0 {
			paraProps.SetFirstLineIndent(measurement.Distance(indentVal))
		}
	}

	// 应用段落间距
	if paraSpace, ok := rules["paragraph_space"].(map[string]interface{}); ok {
		if before, ok := paraSpace["before"].(string); ok {
			beforeVal := p.parseSpacing(before)
			paraProps.Spacing().SetBefore(measurement.Distance(beforeVal))

		}
		if after, ok := paraSpace["after"].(string); ok {
			afterVal := p.parseSpacing(after)
			paraProps.Spacing().SetAfter(measurement.Distance(afterVal))
		}
	}

	// 应用字体格式到所有运行
	runs := para.Runs()

	for _, run := range runs {
		_ = run.Text()
		p.applyRunFormatting(run, rules)
	}

	return nil
}

// applyRunFormatting 应用运行格式（字体、大小、样式等）
func (p *EnhancedProcessor) applyRunFormatting(run document.Run, rules map[string]interface{}) error {
	runProps := run.Properties()

	// 应用字体名称 - 同时设置所有字体属性以确保中文字体正确显示
	if fontName, ok := rules["font_name"].(string); ok {
		// 使用标准API设置字体
		runProps.SetFontFamily(fontName)

		// 直接设置XML字体属性以确保中文字体正确显示
		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		// 使用缓存的字体名指针，避免悬空指针问题
		fontNamePtr := p.getCachedFontName(fontName)
		// 设置所有字体属性（东亚、ASCII、西文等）
		rPr.RFonts.EastAsiaAttr = fontNamePtr
		rPr.RFonts.AsciiAttr = fontNamePtr
		rPr.RFonts.HAnsiAttr = fontNamePtr
		rPr.RFonts.CsAttr = fontNamePtr
	}

	// 应用字体大小
	if fontSize, ok := rules["font_size"].(string); ok {
		if size := p.parseFontSize(fontSize); size > 0 {
			runProps.SetSize(measurement.Distance(size))
		}
	}

	// 应用加粗
	if bold, ok := rules["bold"].(bool); ok && bold {
		runProps.SetBold(true)
	}

	// 应用斜体
	if italic, ok := rules["italic"].(bool); ok && italic {
		runProps.SetItalic(true)
	}

	// 应用下划线
	if underline, ok := rules["underline"].(bool); ok && underline {
		runProps.SetUnderline(wml.ST_UnderlineSingle, color.Auto)
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

func (p *EnhancedProcessor) parseIndent(indent string) float64 {
	indent = strings.TrimSpace(indent)

	if strings.HasSuffix(indent, "字符") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(indent, "字符"), 64); err == nil {
			return val * 14 * 20 // 假设14磅字体，转换为twips
		}
	} else if strings.HasSuffix(indent, "cm") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(indent, "cm"), 64); err == nil {
			return val * 567 // cm to twips
		}
	}

	return 0
}

// parseIndentWithFontSize 根据字号解析缩进
func (p *EnhancedProcessor) parseIndentWithFontSize(indent string, fontSize float64) float64 {
	indent = strings.TrimSpace(indent)

	if strings.HasSuffix(indent, "字符") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(indent, "字符"), 64); err == nil {
			// 使用实际字号计算：字符数 * 字号(pt) * 20(twips/pt)
			return val * fontSize * 20
		}
	} else if strings.HasSuffix(indent, "cm") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(indent, "cm"), 64); err == nil {
			return val * 567 // cm to twips
		}
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

	// 中文字号转换
	sizeMap := map[string]float64{
		"初号": 42,
		"小初": 36,
		"一号": 26,
		"小一": 24,
		"二号": 22,
		"小二": 18,
		"三号": 16,
		"小三": 15,
		"四号": 14,
		"小四": 12,
		"五号": 10.5,
		"小五": 9,
		"六号": 7.5,
		"小六": 6.5,
		"七号": 5.5,
		"八号": 5,
	}

	if val, ok := sizeMap[size]; ok {
		// 返回 point 值，RunProperties.SetSize 内部会负责转换为 w:sz 的 half-points
		return val
	}

	// 直接数字，视为 point
	if val, err := strconv.ParseFloat(size, 64); err == nil {
		return val
	}

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
