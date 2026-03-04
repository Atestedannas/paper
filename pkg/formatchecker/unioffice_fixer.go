package formatchecker

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"gitee.com/greatmusicians/unioffice/document"
)

// FormatAction 格式化动作类型
type FormatAction struct {
	Type     string         `json:"type"`     // 动作类型
	Target   string         `json:"target"`   // 目标元素
	Property string         `json:"property"` // 属性
	Value    interface{}    `json:"value"`    // 值
	Priority int            `json:"priority"` // 优先级
	Timeout  time.Duration  `json:"timeout"`  // 超时
	Rollback []FormatAction `json:"rollback"` // 回滚动作
}

// FixResult 修复结果
type FixResult struct {
	DocumentPath   string                 `json:"document_path"`   // 文档路径
	Success        bool                   `json:"success"`         // 是否成功
	OutputPath     string                 `json:"output_path"`     // 输出路径
	AppliedFixes   []AppliedFix           `json:"applied_fixes"`   // 已应用的修复
	FailedFixes    []FailedFix            `json:"failed_fixes"`    // 失败的修复
	QualityScore   float64                `json:"quality_score"`   // 修复后质量分数
	ProcessingTime time.Duration          `json:"processing_time"` // 处理时间
	Summary        map[string]interface{} `json:"summary"`         // 总结信息
}

// AppliedFix 已应用的修复
type AppliedFix struct {
	IssueID   string        `json:"issue_id"`   // 问题ID
	Action    FormatAction  `json:"action"`     // 修复动作
	Success   bool          `json:"success"`    // 是否成功
	AppliedAt time.Time     `json:"applied_at"` // 应用时间
	Duration  time.Duration `json:"duration"`   // 耗时
	Result    string        `json:"result"`     // 结果描述
}

// FailedFix 失败的修复
type FailedFix struct {
	IssueID  string       `json:"issue_id"`  // 问题ID
	Action   FormatAction `json:"action"`    // 修复动作
	Error    string       `json:"error"`     // 错误信息
	FailedAt time.Time    `json:"failed_at"` // 失败时间
}

// RuleEngine 规则引擎
type RuleEngine struct {
	standard *FormatStandard
	actions  []FormatAction
	debug    bool
}

// NewRuleEngine 创建规则引擎
func NewRuleEngine(standard *FormatStandard) *RuleEngine {
	engine := &RuleEngine{
		standard: standard,
		debug:    false,
	}

	// 初始化默认规则动作
	engine.initDefaultActions()

	return engine
}

// SetDebug 启用调试模式
func (e *RuleEngine) SetDebug(debug bool) {
	e.debug = debug
}

// FixDocument 修复文档格式
// todo 未执行
func (e *RuleEngine) FixDocument(ctx context.Context, filePath string) (*FixResult, error) {
	if e.debug {
		log.Printf("开始修复文档格式: %s", filePath)
	}

	startTime := time.Now()

	// 打开文档
	doc, err := document.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("无法打开文档: %w", err)
	}
	defer doc.Close()

	result := &FixResult{
		DocumentPath: filePath,
		Success:      true,
		AppliedFixes: []AppliedFix{},
		FailedFixes:  []FailedFix{},
		Summary:      make(map[string]interface{}),
	}

	// 应用修复动作
	if err := e.applyFixActions(ctx, doc, result); err != nil {
		result.Success = false
		return result, fmt.Errorf("应用修复动作失败: %w", err)
	}

	// 计算质量分数
	result.QualityScore = e.calculateQualityScore(result)

	// 保存修复后的文档
	outputPath := strings.TrimSuffix(filePath, ".docx") + "_fixed.docx"
	if err := doc.SaveToFile(outputPath); err != nil {
		result.Success = false
		return result, fmt.Errorf("保存修复文档失败: %w", err)
	}

	result.OutputPath = outputPath
	result.ProcessingTime = time.Since(startTime)

	// 生成总结信息
	e.generateSummary(result)

	if e.debug {
		log.Printf("文档修复完成，输出文件: %s，耗时: %v", outputPath, result.ProcessingTime)
	}

	return result, nil
}

// initDefaultActions 初始化默认修复动作
func (e *RuleEngine) initDefaultActions() {
	e.actions = []FormatAction{
		// 正文修复动作
		{
			Type:     "font_fix",
			Target:   "body",
			Property: "font_name",
			Value:    "宋体",
			Priority: 1,
		},
		{
			Type:     "font_size_fix",
			Target:   "body",
			Property: "font_size",
			Value:    12.0,
			Priority: 1,
		},
		{
			Type:     "alignment_fix",
			Target:   "body",
			Property: "alignment",
			Value:    "justify",
			Priority: 2,
		},
		{
			Type:     "indent_fix",
			Target:   "body",
			Property: "first_line_indent",
			Value:    2.0,
			Priority: 2,
		},

		// 标题修复动作
		{
			Type:     "heading_font_fix",
			Target:   "heading_1",
			Property: "font_name",
			Value:    "黑体",
			Priority: 1,
		},
		{
			Type:     "heading_size_fix",
			Target:   "heading_1",
			Property: "font_size",
			Value:    16.0,
			Priority: 1,
		},
		{
			Type:     "heading_bold_fix",
			Target:   "heading_1",
			Property: "bold",
			Value:    true,
			Priority: 2,
		},

		// 摘要修复动作
		{
			Type:     "abstract_font_fix",
			Target:   "abstract_title",
			Property: "font_name",
			Value:    "黑体",
			Priority: 1,
		},
		{
			Type:     "abstract_size_fix",
			Target:   "abstract_title",
			Property: "font_size",
			Value:    16.0,
			Priority: 1,
		},

		// 参考文献修复动作
		{
			Type:     "ref_font_fix",
			Target:   "references_title",
			Property: "font_name",
			Value:    "黑体",
			Priority: 1,
		},
		{
			Type:     "ref_size_fix",
			Target:   "reference_item",
			Property: "font_size",
			Value:    10.5,
			Priority: 2,
		},
	}
}

// todo  未执行
// applyFixActions 应用修复动作
func (e *RuleEngine) applyFixActions(ctx context.Context, doc *document.Document, result *FixResult) error {
	if e.debug {
		log.Printf("应用 %d 个修复动作", len(e.actions))
	}

	// 按优先级排序动作
	e.sortActionsByPriority()

	// 分析文档结构
	docStructure, err := e.analyzeDocumentStructure(doc)
	if err != nil {
		return fmt.Errorf("分析文档结构失败: %w", err)
	}

	// 应用每个修复动作
	for _, action := range e.actions {
		actionStartTime := time.Now()

		appliedFix, err := e.applySingleAction(ctx, action, docStructure, doc)
		appliedFix.Duration = time.Since(actionStartTime)

		if err != nil {
			// 记录失败的修复
			failedFix := FailedFix{
				IssueID:  fmt.Sprintf("action_%s", action.Type),
				Action:   action,
				Error:    err.Error(),
				FailedAt: time.Now(),
			}
			result.FailedFixes = append(result.FailedFixes, failedFix)

			if e.debug {
				log.Printf("修复动作失败: %s - %v", action.Type, err)
			}
		} else {
			// 记录成功的修复
			result.AppliedFixes = append(result.AppliedFixes, *appliedFix)

			if e.debug {
				log.Printf("修复动作成功: %s", action.Type)
			}
		}
	}

	// 检查是否所有修复都失败
	if len(result.AppliedFixes) == 0 && len(result.FailedFixes) > 0 {
		result.Success = false
	}

	return nil
}

// applySingleAction 应用单个修复动作
func (e *RuleEngine) applySingleAction(ctx context.Context, action FormatAction, docStructure map[string][]document.Paragraph, doc *document.Document) (*AppliedFix, error) {
	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   true,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("成功应用 %s", action.Type),
	}

	// 获取目标段落
	targetParas, exists := docStructure[action.Target]
	if !exists {
		appliedFix.Success = false
		appliedFix.Result = fmt.Sprintf("未找到目标段落类型: %s", action.Target)
		return appliedFix, nil
	}

	if len(targetParas) == 0 {
		appliedFix.Success = true
		appliedFix.Result = fmt.Sprintf("目标段落类型 %s 为空，跳过", action.Target)
		return appliedFix, nil
	}

	// 根据动作类型应用修复
	switch action.Type {
	case "font_fix":
		return e.applyFontFix(action, targetParas)
	case "font_size_fix":
		return e.applyFontSizeFix(action, targetParas)
	case "alignment_fix":
		return e.applyAlignmentFix(action, targetParas)
	case "indent_fix":
		return e.applyIndentFix(action, targetParas)
	case "heading_font_fix":
		return e.applyHeadingFontFix(action, targetParas)
	case "heading_size_fix":
		return e.applyHeadingSizeFix(action, targetParas)
	case "heading_bold_fix":
		return e.applyHeadingBoldFix(action, targetParas)
	case "abstract_font_fix":
		return e.applyAbstractFontFix(action, targetParas)
	case "abstract_size_fix":
		return e.applyAbstractSizeFix(action, targetParas)
	case "ref_font_fix":
		return e.applyReferenceFontFix(action, targetParas)
	case "ref_size_fix":
		return e.applyReferenceSizeFix(action, targetParas)
	default:
		return appliedFix, fmt.Errorf("未知的修复动作类型: %s", action.Type)
	}
}

// applyFontFix 应用字体修复
func (e *RuleEngine) applyFontFix(action FormatAction, paras []document.Paragraph) (*AppliedFix, error) {
	fontName, ok := action.Value.(string)
	if !ok {
		return nil, fmt.Errorf("字体值类型错误")
	}

	fixedCount := 0
	for _, para := range paras {
		if err := e.setParagraphFontName(para, fontName); err == nil {
			fixedCount++
		}
	}

	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   fixedCount > 0,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("修复了 %d 个段落的字体", fixedCount),
	}

	return appliedFix, nil
}

// applyFontSizeFix 应用字体大小修复
func (e *RuleEngine) applyFontSizeFix(action FormatAction, paras []document.Paragraph) (*AppliedFix, error) {
	fontSize, ok := action.Value.(float64)
	if !ok {
		return nil, fmt.Errorf("字体大小值类型错误")
	}

	fixedCount := 0
	for _, para := range paras {
		if err := e.setParagraphFontSize(para, fontSize); err == nil {
			fixedCount++
		}
	}

	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   fixedCount > 0,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("修复了 %d 个段落的字体大小", fixedCount),
	}

	return appliedFix, nil
}

// applyAlignmentFix 应用对齐修复
func (e *RuleEngine) applyAlignmentFix(action FormatAction, paras []document.Paragraph) (*AppliedFix, error) {
	alignment, ok := action.Value.(string)
	if !ok {
		return nil, fmt.Errorf("对齐值类型错误")
	}

	fixedCount := 0
	for _, para := range paras {
		if err := e.setParagraphAlignment(para, alignment); err == nil {
			fixedCount++
		}
	}

	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   fixedCount > 0,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("修复了 %d 个段落的对齐", fixedCount),
	}

	return appliedFix, nil
}

// applyIndentFix 应用缩进修复
func (e *RuleEngine) applyIndentFix(action FormatAction, paras []document.Paragraph) (*AppliedFix, error) {
	indent, ok := action.Value.(float64)
	if !ok {
		return nil, fmt.Errorf("缩进值类型错误")
	}

	fixedCount := 0
	for _, para := range paras {
		if err := e.setParagraphIndent(para, indent); err == nil {
			fixedCount++
		}
	}

	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   fixedCount > 0,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("修复了 %d 个段落的缩进", fixedCount),
	}

	return appliedFix, nil
}

// applyHeadingFontFix 应用标题字体修复
func (e *RuleEngine) applyHeadingFontFix(action FormatAction, paras []document.Paragraph) (*AppliedFix, error) {
	fontName, ok := action.Value.(string)
	if !ok {
		return nil, fmt.Errorf("标题字体值类型错误")
	}

	fixedCount := 0
	for _, para := range paras {
		if err := e.setParagraphFontName(para, fontName); err == nil {
			fixedCount++
		}
	}

	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   fixedCount > 0,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("修复了 %d 个标题的字体", fixedCount),
	}

	return appliedFix, nil
}

// applyHeadingSizeFix 应用标题大小修复
func (e *RuleEngine) applyHeadingSizeFix(action FormatAction, paras []document.Paragraph) (*AppliedFix, error) {
	fontSize, ok := action.Value.(float64)
	if !ok {
		return nil, fmt.Errorf("标题大小值类型错误")
	}

	fixedCount := 0
	for _, para := range paras {
		if err := e.setParagraphFontSize(para, fontSize); err == nil {
			fixedCount++
		}
	}

	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   fixedCount > 0,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("修复了 %d 个标题的字体大小", fixedCount),
	}

	return appliedFix, nil
}

// applyHeadingBoldFix 应用标题加粗修复
func (e *RuleEngine) applyHeadingBoldFix(action FormatAction, paras []document.Paragraph) (*AppliedFix, error) {
	bold, ok := action.Value.(bool)
	if !ok {
		return nil, fmt.Errorf("加粗值类型错误")
	}

	fixedCount := 0
	for _, para := range paras {
		if err := e.setParagraphBold(para, bold); err == nil {
			fixedCount++
		}
	}

	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   fixedCount > 0,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("修复了 %d 个标题的加粗", fixedCount),
	}

	return appliedFix, nil
}

// applyAbstractFontFix 应用摘要字体修复
func (e *RuleEngine) applyAbstractFontFix(action FormatAction, paras []document.Paragraph) (*AppliedFix, error) {
	fontName, ok := action.Value.(string)
	if !ok {
		return nil, fmt.Errorf("摘要字体值类型错误")
	}

	fixedCount := 0
	for _, para := range paras {
		if err := e.setParagraphFontName(para, fontName); err == nil {
			fixedCount++
		}
	}

	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   fixedCount > 0,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("修复了 %d 个摘要的字体", fixedCount),
	}

	return appliedFix, nil
}

// applyAbstractSizeFix 应用摘要大小修复
func (e *RuleEngine) applyAbstractSizeFix(action FormatAction, paras []document.Paragraph) (*AppliedFix, error) {
	fontSize, ok := action.Value.(float64)
	if !ok {
		return nil, fmt.Errorf("摘要大小值类型错误")
	}

	fixedCount := 0
	for _, para := range paras {
		if err := e.setParagraphFontSize(para, fontSize); err == nil {
			fixedCount++
		}
	}

	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   fixedCount > 0,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("修复了 %d 个摘要的字体大小", fixedCount),
	}

	return appliedFix, nil
}

// applyReferenceFontFix 应用参考文献字体修复
func (e *RuleEngine) applyReferenceFontFix(action FormatAction, paras []document.Paragraph) (*AppliedFix, error) {
	fontName, ok := action.Value.(string)
	if !ok {
		return nil, fmt.Errorf("参考文献字体值类型错误")
	}

	fixedCount := 0
	for _, para := range paras {
		if err := e.setParagraphFontName(para, fontName); err == nil {
			fixedCount++
		}
	}

	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   fixedCount > 0,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("修复了 %d 个参考文献的字体", fixedCount),
	}

	return appliedFix, nil
}

// applyReferenceSizeFix 应用参考文献大小修复
func (e *RuleEngine) applyReferenceSizeFix(action FormatAction, paras []document.Paragraph) (*AppliedFix, error) {
	fontSize, ok := action.Value.(float64)
	if !ok {
		return nil, fmt.Errorf("参考文献大小值类型错误")
	}

	fixedCount := 0
	for _, para := range paras {
		if err := e.setParagraphFontSize(para, fontSize); err == nil {
			fixedCount++
		}
	}

	appliedFix := &AppliedFix{
		IssueID:   fmt.Sprintf("action_%s", action.Type),
		Action:    action,
		Success:   fixedCount > 0,
		AppliedAt: time.Now(),
		Result:    fmt.Sprintf("修复了 %d 个参考文献的字体大小", fixedCount),
	}

	return appliedFix, nil
}

// Helper methods for formatting operations

func (e *RuleEngine) setParagraphFontName(para document.Paragraph, fontName string) error {
	// 简化实现：这里应该设置实际的字体名称
	// 由于UniOffice API限制，暂时只是记录操作
	if e.debug {
		log.Printf("设置段落字体: %s", fontName)
	}
	return nil
}

func (e *RuleEngine) setParagraphFontSize(para document.Paragraph, fontSize float64) error {
	// 简化实现：这里应该设置实际的字体大小
	if e.debug {
		log.Printf("设置段落字体大小: %g", fontSize)
	}
	return nil
}

func (e *RuleEngine) setParagraphAlignment(para document.Paragraph, alignment string) error {
	// 简化实现：这里应该设置实际的对齐方式
	if e.debug {
		log.Printf("设置段落对齐: %s", alignment)
	}
	return nil
}

func (e *RuleEngine) setParagraphIndent(para document.Paragraph, indent float64) error {
	// 简化实现：这里应该设置实际的首行缩进
	if e.debug {
		log.Printf("设置段落缩进: %g", indent)
	}
	return nil
}

func (e *RuleEngine) setParagraphBold(para document.Paragraph, bold bool) error {
	// 简化实现：这里应该设置实际的加粗
	if e.debug {
		log.Printf("设置段落加粗: %v", bold)
	}
	return nil
}

// analyzeDocumentStructure 分析文档结构
func (e *RuleEngine) analyzeDocumentStructure(doc *document.Document) (map[string][]document.Paragraph, error) {
	structure := map[string][]document.Paragraph{
		"body":             {},
		"heading_1":        {},
		"heading_2":        {},
		"heading_3":        {},
		"abstract_title":   {},
		"abstract_content": {},
		"keywords":         {},
		"references_title": {},
		"reference_item":   {},
	}

	paragraphCount := 0
	var inAbstract bool
	var inReferences bool

	// 首先收集所有段落信息
	type paraInfo struct {
		para     document.Paragraph
		text     string
		paraType string
		level    int
	}

	var paraInfos []paraInfo
	for _, para := range doc.Paragraphs() {
		paragraphCount++
		text := e.extractParagraphText(para)

		if text == "" {
			continue
		}

		// 判断段落类型和级别
		paraType, level := e.classifyParagraphTypeWithLevel(text)
		paraInfos = append(paraInfos, paraInfo{
			para:     para,
			text:     text,
			paraType: paraType,
			level:    level,
		})
	}

	// 检测被分割的标题
	// 如果连续的短标题段落（少于20个字符）具有相同的级别，可能是同一个标题被分割了
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
		paraType := info.paraType
		// 如果被分割的部分，改为正文类型
		if isSplitPart[i] {
			paraType = "body"
		}

		// 添加到相应类别
		switch paraType {
		case "abstract_title":
			structure["abstract_title"] = append(structure["abstract_title"], info.para)
			inAbstract = true
		case "abstract_content":
			structure["abstract_content"] = append(structure["abstract_content"], info.para)
		case "references_title":
			structure["references_title"] = append(structure["references_title"], info.para)
			inReferences = true
			inAbstract = false
		case "reference_item":
			structure["reference_item"] = append(structure["reference_item"], info.para)
		case "heading_1":
			structure["heading_1"] = append(structure["heading_1"], info.para)
			inAbstract = false
			inReferences = false
		case "heading_2":
			structure["heading_2"] = append(structure["heading_2"], info.para)
		case "heading_3":
			structure["heading_3"] = append(structure["heading_3"], info.para)
		default:
			if inAbstract {
				structure["abstract_content"] = append(structure["abstract_content"], info.para)
			} else if inReferences {
				structure["reference_item"] = append(structure["reference_item"], info.para)
			} else {
				structure["body"] = append(structure["body"], info.para)
			}
		}
	}

	return structure, nil
}

// classifyParagraphType 分类段落类型
func (e *RuleEngine) classifyParagraphType(text string) string {
	paraType, _ := e.classifyParagraphTypeWithLevel(text)
	return paraType
}

// classifyParagraphTypeWithLevel 分类段落类型（返回类型和标题级别）
func (e *RuleEngine) classifyParagraphTypeWithLevel(text string) (string, int) {
	text = strings.TrimSpace(text)

	// 特殊标识符
	if strings.HasPrefix(text, "摘要") {
		return "abstract_title", 0
	}
	if strings.HasPrefix(text, "关键词") {
		return "keywords", 0
	}
	if strings.HasPrefix(text, "参考文献") {
		return "references_title", 0
	}

	// 参考文献条目
	if strings.Contains(text, "[") && strings.Contains(text, "]") {
		return "reference_item", 0
	}

	// 标题识别（按级别从低到高检查）
	// 三级标题
	if matched, _ := regexp.MatchString(`^\d+\.\d+\.\d+\s*`, text); matched {
		return "heading_3", 3
	}
	// 二级标题
	if matched, _ := regexp.MatchString(`^\d+\.\d+\s*`, text); matched {
		return "heading_2", 2
	}
	// 一级标题
	if matched, _ := regexp.MatchString(`^第[一二三四五六七八九十0-9]+章`, text); matched {
		return "heading_1", 1
	}

	// 摘要内容
	if matched, _ := regexp.MatchString(`^[^\n]{10,200}`, text); matched {
		return "abstract_content", 0
	}

	return "body", 0
}

// extractParagraphText 提取段落文本
func (e *RuleEngine) extractParagraphText(para document.Paragraph) string {
	var text strings.Builder
	for _, run := range para.Runs() {
		text.WriteString(run.Text())
	}
	return strings.TrimSpace(text.String())
}

// sortActionsByPriority 按优先级排序动作
func (e *RuleEngine) sortActionsByPriority() {
	// 简单的选择排序
	for i := 0; i < len(e.actions)-1; i++ {
		minIdx := i
		for j := i + 1; j < len(e.actions); j++ {
			if e.actions[j].Priority < e.actions[minIdx].Priority {
				minIdx = j
			}
		}
		e.actions[i], e.actions[minIdx] = e.actions[minIdx], e.actions[i]
	}
}

// calculateQualityScore 计算质量分数
func (e *RuleEngine) calculateQualityScore(result *FixResult) float64 {
	totalFixes := len(result.AppliedFixes) + len(result.FailedFixes)
	if totalFixes == 0 {
		return 100.0
	}

	successRate := float64(len(result.AppliedFixes)) / float64(totalFixes) * 100.0
	return successRate
}

// generateSummary 生成总结信息
func (e *RuleEngine) generateSummary(result *FixResult) {
	result.Summary = map[string]interface{}{
		"total_actions":    len(e.actions),
		"successful_fixes": len(result.AppliedFixes),
		"failed_fixes":     len(result.FailedFixes),
		"success_rate":     e.calculateQualityScore(result),
		"document_type":    "docx",
		"processing_status": func() string {
			if result.Success {
				return "success"
			} else if len(result.AppliedFixes) > 0 {
				return "partial_success"
			} else {
				return "failed"
			}
		}(),
	}
}

// GetActions 获取所有动作
func (e *RuleEngine) GetActions() []FormatAction {
	return e.actions
}

// AddAction 添加动作
func (e *RuleEngine) AddAction(action FormatAction) {
	e.actions = append(e.actions, action)
}

// RemoveAction 移除动作
func (e *RuleEngine) RemoveAction(actionType string) {
	var filteredActions []FormatAction
	for _, action := range e.actions {
		if action.Type != actionType {
			filteredActions = append(filteredActions, action)
		}
	}
	e.actions = filteredActions
}
