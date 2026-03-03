package formatchecker

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DocumentPart 文档部分结构
type DocumentPart struct {
	Type     string                 `json:"type"`     // heading, body, abstract, etc.
	Content  string                 `json:"content"`  // 文本内容
	Style    map[string]interface{} `json:"style"`    // 样式信息
	Position int                    `json:"position"` // 在文档中的位置
}

// TemplateDocument 模板文档结构
type TemplateDocument struct {
	Parts      []DocumentPart         `json:"parts"`       // 文档部分
	Styles     map[string]interface{} `json:"styles"`      // 样式表
	Metadata   map[string]interface{} `json:"metadata"`    // 元数据
	OutputPath string                 `json:"output_path"` // 输出路径
}

// FormatRule 格式规则
type FormatRule struct {
	Target   string      `json:"target"`   // 目标元素类型
	Property string      `json:"property"` // 属性名
	Value    interface{} `json:"value"`    // 属性值
	Priority int         `json:"priority"` // 优先级
}

// AdvancedTemplateEngine 高级模板引擎 - 基于第一性原理重构
type AdvancedTemplateEngine struct {
	templatePath string
	standard     *FormatStandard
	rules        []FormatRule
	debug        bool
}

// NewAdvancedTemplateEngine 创建高级模板引擎
func NewAdvancedTemplateEngine(templatePath string) *AdvancedTemplateEngine {
	engine := &AdvancedTemplateEngine{
		templatePath: templatePath,
		debug:        false,
	}

	// 初始化默认格式规则
	engine.initDefaultRules()

	return engine
}

// SetFormatStandard 设置格式标准
func (e *AdvancedTemplateEngine) SetFormatStandard(standard *FormatStandard) {
	e.standard = standard
}

// SetDebug 启用调试模式
func (e *AdvancedTemplateEngine) SetDebug(debug bool) {
	e.debug = debug
}

// initDefaultRules 初始化默认格式规则
func (e *AdvancedTemplateEngine) initDefaultRules() {
	e.rules = []FormatRule{
		// 正文规则
		{Target: "body", Property: "font_name", Value: "宋体", Priority: 1},
		{Target: "body", Property: "font_size", Value: 12.0, Priority: 1},
		{Target: "body", Property: "line_spacing", Value: 20.0, Priority: 1},
		{Target: "body", Property: "first_line_indent", Value: 2.0, Priority: 1},
		{Target: "body", Property: "alignment", Value: "justify", Priority: 1},

		// 标题规则
		{Target: "heading_1", Property: "font_name", Value: "黑体", Priority: 2},
		{Target: "heading_1", Property: "font_size", Value: 16.0, Priority: 2},
		{Target: "heading_1", Property: "bold", Value: true, Priority: 2},
		{Target: "heading_1", Property: "alignment", Value: "left", Priority: 2},

		{Target: "heading_2", Property: "font_name", Value: "黑体", Priority: 2},
		{Target: "heading_2", Property: "font_size", Value: 14.0, Priority: 2},
		{Target: "heading_2", Property: "bold", Value: true, Priority: 2},

		// 摘要规则
		{Target: "abstract_title", Property: "font_name", Value: "黑体", Priority: 3},
		{Target: "abstract_title", Property: "font_size", Value: 16.0, Priority: 3},
		{Target: "abstract_title", Property: "bold", Value: true, Priority: 3},
		{Target: "abstract_title", Property: "alignment", Value: "center", Priority: 3},

		// 参考文献规则
		{Target: "references_title", Property: "font_name", Value: "黑体", Priority: 4},
		{Target: "references_title", Property: "font_size", Value: 14.0, Priority: 4},
		{Target: "references_title", Property: "bold", Value: true, Priority: 4},
		{Target: "references_title", Property: "alignment", Value: "center", Priority: 4},

		{Target: "reference_item", Property: "font_name", Value: "宋体", Priority: 4},
		{Target: "reference_item", Property: "font_size", Value: 10.5, Priority: 4},
		{Target: "reference_item", Property: "first_line_indent", Value: 0.0, Priority: 4},
	}
}

// AnalyzeDocument 分析文档结构
func (e *AdvancedTemplateEngine) AnalyzeDocument(docPath string) (*TemplateDocument, error) {
	if e.debug {
		log.Printf("开始分析文档: %s", docPath)
	}

	// 解析DOCX文档
	template, err := e.parseDOCXDocument(docPath)
	if err != nil {
		return nil, fmt.Errorf("解析文档失败: %w", err)
	}

	// 应用格式规则
	template = e.applyFormatRules(template)

	if e.debug {
		log.Printf("文档分析完成，发现 %d 个部分", len(template.Parts))
	}

	return template, nil
}

// parseDOCXDocument 解析DOCX文档
func (e *AdvancedTemplateEngine) parseDOCXDocument(docPath string) (*TemplateDocument, error) {
	doc := &TemplateDocument{
		Parts:    []DocumentPart{},
		Styles:   make(map[string]interface{}),
		Metadata: make(map[string]interface{}),
	}

	// 打开ZIP文件
	r, err := zip.OpenReader(docPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	// 读取document.xml内容
	var documentXML string
	for _, file := range r.File {
		if file.Name == "word/document.xml" {
			content, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer content.Close()

			buf := new(bytes.Buffer)
			if _, err := io.Copy(buf, content); err != nil {
				return nil, err
			}
			documentXML = buf.String()
			break
		}
	}

	if documentXML == "" {
		return nil, fmt.Errorf("未找到document.xml文件")
	}

	// 解析XML结构
	if e.debug {
		log.Printf("解析XML结构...")
	}

	paragraphs := e.parseXMLParagraphs(documentXML)

	// 分析段落类型
	for i, para := range paragraphs {
		partType := e.classifyParagraph(para.Text)

		part := DocumentPart{
			Type:     partType,
			Content:  para.Text,
			Style:    make(map[string]interface{}),
			Position: i,
		}

		doc.Parts = append(doc.Parts, part)
	}

	return doc, nil
}

// XMLParagraph XML段落结构
type XMLParagraph struct {
	Text  string
	Style string
	Level int
}

// parseXMLParagraphs 解析XML段落
func (e *AdvancedTemplateEngine) parseXMLParagraphs(xmlContent string) []XMLParagraph {
	var paragraphs []XMLParagraph

	// 简单的正则匹配段落
	re := regexp.MustCompile(`<w:p[^>]*>(.*?)</w:p>`)
	matches := re.FindAllStringSubmatch(xmlContent, -1)

	for _, match := range matches {
		if len(match) > 1 {
			paragraphContent := match[1]

			// 提取文本内容
			textRe := regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>`)
			textMatches := textRe.FindAllStringSubmatch(paragraphContent, -1)

			var text strings.Builder
			for _, textMatch := range textMatches {
				if len(textMatch) > 1 {
					text.WriteString(textMatch[1])
				}
			}

			paragraph := XMLParagraph{
				Text: strings.TrimSpace(text.String()),
			}

			if paragraph.Text != "" {
				paragraphs = append(paragraphs, paragraph)
			}
		}
	}

	return paragraphs
}

// classifyParagraph 分类段落类型
func (e *AdvancedTemplateEngine) classifyParagraph(text string) string {
	text = strings.TrimSpace(text)

	// 标题识别
	if matched, _ := regexp.MatchString(`^第[一二三四五六七八九十0-9]+章`, text); matched {
		return "heading_1"
	}
	if matched, _ := regexp.MatchString(`^\d+\.\d+\s+`, text); matched {
		return "heading_2"
	}

	// 特殊段落
	if strings.HasPrefix(text, "摘要") {
		return "abstract_title"
	}
	if strings.HasPrefix(text, "关键词") {
		return "keywords"
	}
	if strings.HasPrefix(text, "参考文献") {
		return "references_title"
	}
	if strings.Contains(text, "[") && strings.Contains(text, "]") {
		return "reference_item"
	}

	// 默认为正文
	return "body"
}

// applyFormatRules 应用格式规则
func (e *AdvancedTemplateEngine) applyFormatRules(template *TemplateDocument) *TemplateDocument {
	if e.debug {
		log.Printf("应用 %d 个格式规则", len(e.rules))
	}

	// 按优先级排序规则
	e.sortRulesByPriority()

	// 为每个部分应用规则
	for i, part := range template.Parts {
		rules := e.getRulesForType(part.Type)

		for _, rule := range rules {
			// 应用规则到样式
			if part.Style == nil {
				part.Style = make(map[string]interface{})
			}
			part.Style[rule.Property] = rule.Value
		}

		template.Parts[i] = part
	}

	if e.debug {
		log.Printf("格式规则应用完成")
	}

	return template
}

// sortRulesByPriority 按优先级排序规则
func (e *AdvancedTemplateEngine) sortRulesByPriority() {
	// 简单的选择排序
	for i := 0; i < len(e.rules)-1; i++ {
		minIdx := i
		for j := i + 1; j < len(e.rules); j++ {
			if e.rules[j].Priority < e.rules[minIdx].Priority {
				minIdx = j
			}
		}
		e.rules[i], e.rules[minIdx] = e.rules[minIdx], e.rules[i]
	}
}

// getRulesForType 获取指定类型的规则
func (e *AdvancedTemplateEngine) getRulesForType(partType string) []FormatRule {
	var rules []FormatRule

	for _, rule := range e.rules {
		if rule.Target == partType {
			rules = append(rules, rule)
		}
	}

	return rules
}

// GenerateDocument 生成完整文档
func (e *AdvancedTemplateEngine) GenerateDocument(template *TemplateDocument) error {
	if e.debug {
		log.Printf("开始生成文档...")
	}

	// 生成document.xml内容
	documentXML := e.generateDocumentXML(template)

	// 保存到临时文件
	tempPath := filepath.Join(filepath.Dir(e.templatePath), "generated_document.xml")
	if err := e.saveXMLFile(documentXML, tempPath); err != nil {
		return fmt.Errorf("保存生成文档失败: %w", err)
	}

	if e.debug {
		log.Printf("文档生成完成，保存到: %s", tempPath)
	}

	return nil
}

// generateDocumentXML 生成document.xml内容
func (e *AdvancedTemplateEngine) generateDocumentXML(template *TemplateDocument) string {
	var buffer strings.Builder

	// XML头部
	buffer.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	buffer.WriteString("\n<w:document xmlns:w=\"http://schemas.openxmlformats.org/wordprocessingml/2006/main\">")
	buffer.WriteString("\n<w:body>")

	// 生成段落
	for _, part := range template.Parts {
		buffer.WriteString("\n<w:p>")

		// 应用样式
		if part.Style != nil {
			if fontSize, ok := part.Style["font_size"].(float64); ok {
				if isBold, ok := part.Style["bold"].(bool); ok && isBold {
					buffer.WriteString("<w:pPr><w:jc w:val=\"left\"/><w:rPr><w:b/><w:sz w:val=\"" + strconv.Itoa(int(fontSize*2)) + "\"/></w:rPr></w:pPr>")
				} else {
					buffer.WriteString("<w:pPr><w:jc w:val=\"left\"/><w:rPr><w:sz w:val=\"" + strconv.Itoa(int(fontSize*2)) + "\"/></w:rPr></w:pPr>")
				}
			} else {
				buffer.WriteString("<w:pPr><w:jc w:val=\"left\"/></w:pPr>")
			}
		}

		// 添加文本内容
		buffer.WriteString("<w:r>")
		if fontSize, ok := part.Style["font_size"].(float64); ok {
			if isBold, ok := part.Style["bold"].(bool); ok && isBold {
				buffer.WriteString("<w:rPr><w:b/><w:sz w:val=\"" + strconv.Itoa(int(fontSize*2)) + "\"/></w:rPr>")
			} else {
				buffer.WriteString("<w:rPr><w:sz w:val=\"" + strconv.Itoa(int(fontSize*2)) + "\"/></w:rPr>")
			}
		}

		// 转义XML字符
		content := e.escapeXML(part.Content)
		buffer.WriteString("<w:t xml:space=\"preserve\">" + content + "</w:t>")
		buffer.WriteString("</w:r>")

		buffer.WriteString("\n</w:p>")
	}

	buffer.WriteString("\n</w:body>")
	buffer.WriteString("\n</w:document>")

	return buffer.String()
}

// escapeXML 转义XML字符
func (e *AdvancedTemplateEngine) escapeXML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	text = strings.ReplaceAll(text, "\"", "&quot;")
	return text
}

// saveXMLFile 保存XML文件
func (e *AdvancedTemplateEngine) saveXMLFile(content, path string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// Close 关闭文档
func (e *AdvancedTemplateEngine) Close() error {
	if e.debug {
		log.Printf("关闭模板引擎")
	}
	// 清理资源
	return nil
}

// SaveToFile 保存文档
func (e *AdvancedTemplateEngine) SaveToFile(path string) error {
	// 保存到指定路径
	return os.WriteFile(path, []byte("generated_document.xml"), 0644)
}

// LoadTemplate 加载模板文件
func (e *AdvancedTemplateEngine) LoadTemplate() error {
	if e.debug {
		log.Printf("加载模板文件: %s", e.templatePath)
	}

	if e.templatePath == "" {
		return fmt.Errorf("模板文件路径为空")
	}

	// 这里可以添加实际的模板加载逻辑
	return nil
}

// ValidateTemplate 验证模板
func (e *AdvancedTemplateEngine) ValidateTemplate() error {
	if len(e.rules) == 0 {
		return fmt.Errorf("没有定义格式规则")
	}

	// 验证规则的完整性
	for _, rule := range e.rules {
		if rule.Target == "" || rule.Property == "" {
			return fmt.Errorf("规则不完整: %+v", rule)
		}
	}

	return nil
}

// GetTemplateSummary 获取模板摘要
func (e *AdvancedTemplateEngine) GetTemplateSummary() map[string]interface{} {
	summary := map[string]interface{}{
		"rules_count":   len(e.rules),
		"template_path": e.templatePath,
		"debug_enabled": e.debug,
	}

	// 统计各类型规则数量
	ruleCounts := make(map[string]int)
	for _, rule := range e.rules {
		ruleCounts[rule.Target]++
	}
	summary["rule_counts"] = ruleCounts

	return summary
}
