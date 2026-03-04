package formatchecker

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gitee.com/greatmusicians/unioffice/document"
)

// UniOfficeFormatChecker 高精度格式检查器 - 基于第一性原理重构
type UniOfficeFormatChecker struct {
	standard *FormatStandard
	debug    bool
	doc      *document.Document
	issues   []FormatIssue
}

// NewUniOfficeFormatChecker 创建UniOffice检查器
func NewUniOfficeFormatChecker(standard *FormatStandard) *UniOfficeFormatChecker {
	checker := &UniOfficeFormatChecker{
		standard: standard,
		debug:    false,
		issues:   []FormatIssue{},
	}

	return checker
}

// SetDebug 启用调试模式
func (c *UniOfficeFormatChecker) SetDebug(debug bool) {
	c.debug = debug
}

// Check 检查文档格式
func (c *UniOfficeFormatChecker) Check(ctx context.Context, filePath string) (*CheckResult, error) {
	if c.debug {
		log.Printf("开始检查文档格式: %s", filePath)
	}

	// 打开文档
	doc, err := document.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("无法打开文档: %w", err)
	}
	defer doc.Close()

	c.doc = doc
	c.issues = []FormatIssue{}

	// 执行检查
	if err := c.performAdvancedChecks(); err != nil {
		return nil, fmt.Errorf("检查过程失败: %w", err)
	}

	// 生成检查结果
	result := c.generateCheckResult(filePath)

	if c.debug {
		log.Printf("检查完成，发现 %d 个问题 (错误: %d, 警告: %d, 信息: %d)",
			result.TotalIssues, result.ErrorCount, result.WarningCount, result.InfoCount)
	}

	return result, nil
}

// performAdvancedChecks 执行高级检查
func (c *UniOfficeFormatChecker) performAdvancedChecks() error {
	if c.doc == nil {
		return fmt.Errorf("文档未加载")
	}

	// 分析文档结构
	docStructure, err := c.analyzeDocumentStructure()
	if err != nil {
		return fmt.Errorf("分析文档结构失败: %w", err)
	}

	// 执行各种检查
	c.checkFontFormatting(docStructure)
	c.checkParagraphFormatting(docStructure)
	c.checkHeadingStructure(docStructure)
	c.checkSpecialSections(docStructure)

	return nil
}

// todo  未执行
// analyzeDocumentStructure 分析文档结构
func (c *UniOfficeFormatChecker) analyzeDocumentStructure() (map[string][]map[string]interface{}, error) {
	structure := map[string][]map[string]interface{}{
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
		text     string
		paraType string
		level    int
		position int
		para     document.Paragraph
	}

	var paraInfos []paraInfo
	for _, para := range c.doc.Paragraphs() {
		paragraphCount++
		text := c.extractParagraphText(para)

		if text == "" {
			continue
		}

		// 判断段落类型和级别
		paraType, level := c.classifyParagraphTypeWithLevel(text)
		paraInfos = append(paraInfos, paraInfo{
			text:     text,
			paraType: paraType,
			level:    level,
			position: paragraphCount,
			para:     para,
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

		// 构建段落信息
		paraInfo := map[string]interface{}{
			"text":      info.text,
			"position":  info.position,
			"paragraph": info.para,
		}

		// 添加到相应类别
		switch paraType {
		case "abstract_title":
			structure["abstract_title"] = append(structure["abstract_title"], paraInfo)
			inAbstract = true
		case "abstract_content":
			structure["abstract_content"] = append(structure["abstract_content"], paraInfo)
		case "references_title":
			structure["references_title"] = append(structure["references_title"], paraInfo)
			inReferences = true
			inAbstract = false
		case "reference_item":
			structure["reference_item"] = append(structure["reference_item"], paraInfo)
		case "heading_1":
			structure["heading_1"] = append(structure["heading_1"], paraInfo)
			inAbstract = false
			inReferences = false
		case "heading_2":
			structure["heading_2"] = append(structure["heading_2"], paraInfo)
		case "heading_3":
			structure["heading_3"] = append(structure["heading_3"], paraInfo)
		default:
			if inAbstract {
				structure["abstract_content"] = append(structure["abstract_content"], paraInfo)
			} else if inReferences {
				structure["reference_item"] = append(structure["reference_item"], paraInfo)
			} else {
				structure["body"] = append(structure["body"], paraInfo)
			}
		}
	}

	if c.debug {
		log.Printf("文档结构分析完成:")
		for paraType, paras := range structure {
			log.Printf("  %s: %d 个段落", paraType, len(paras))
		}
	}

	return structure, nil
}

// classifyParagraphType 分类段落类型
func (c *UniOfficeFormatChecker) classifyParagraphType(text string) string {
	paraType, _ := c.classifyParagraphTypeWithLevel(text)
	return paraType
}

// classifyParagraphTypeWithLevel 分类段落类型（返回类型和标题级别）
func (c *UniOfficeFormatChecker) classifyParagraphTypeWithLevel(text string) (string, int) {
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
func (c *UniOfficeFormatChecker) extractParagraphText(para document.Paragraph) string {
	var text strings.Builder
	for _, run := range para.Runs() {
		text.WriteString(run.Text())
	}
	return strings.TrimSpace(text.String())
}

// checkFontFormatting 检查字体格式
func (c *UniOfficeFormatChecker) checkFontFormatting(structure map[string][]map[string]interface{}) {
	if c.debug {
		log.Printf("检查字体格式...")
	}

	// 检查正文字体
	bodyParas := structure["body"]
	for i, paraInfo := range bodyParas {
		issue := c.generateFontIssue("body", i+1, paraInfo, "正文字体应为宋体", IssueTypeParagraph, SeverityWarning)
		c.issues = append(c.issues, issue)
	}

	// 检查标题字体
	headingParas := structure["heading_1"]
	for i, paraInfo := range headingParas {
		issue := c.generateFontIssue("heading_1", i+1, paraInfo, "一级标题应为黑体", IssueTypeHeading, SeverityError)
		c.issues = append(c.issues, issue)
	}
}

// checkParagraphFormatting 检查段落格式
func (c *UniOfficeFormatChecker) checkParagraphFormatting(structure map[string][]map[string]interface{}) {
	if c.debug {
		log.Printf("检查段落格式...")
	}

	// 检查首行缩进
	bodyParas := structure["body"]
	for i, paraInfo := range bodyParas {
		issue := c.generateParagraphIssue(i+1, paraInfo, "正文应有2字符首行缩进", IssueTypeParagraph, SeverityWarning)
		c.issues = append(c.issues, issue)
	}
}

// checkHeadingStructure 检查标题结构
func (c *UniOfficeFormatChecker) checkHeadingStructure(structure map[string][]map[string]interface{}) {
	if c.debug {
		log.Printf("检查标题结构...")
	}

	// 检查标题层级
	heading1Count := len(structure["heading_1"])
	heading2Count := len(structure["heading_2"])
	heading3Count := len(structure["heading_3"])

	if heading2Count > heading1Count {
		issue := c.generateStructureIssue("标题层级错误：二级标题数量不应超过一级标题", IssueTypeHeading, SeverityError)
		c.issues = append(c.issues, issue)
	}

	if heading3Count > heading2Count {
		issue := c.generateStructureIssue("标题层级错误：三级标题数量不应超过二级标题", IssueTypeHeading, SeverityError)
		c.issues = append(c.issues, issue)
	}
}

// checkSpecialSections 检查特殊章节
func (c *UniOfficeFormatChecker) checkSpecialSections(structure map[string][]map[string]interface{}) {
	if c.debug {
		log.Printf("检查特殊章节...")
	}

	// 检查摘要
	abstractTitleCount := len(structure["abstract_title"])
	if abstractTitleCount == 0 {
		issue := c.generateStructureIssue("缺少摘要章节", IssueTypeAbstract, SeverityWarning)
		c.issues = append(c.issues, issue)
	} else if abstractTitleCount > 1 {
		issue := c.generateStructureIssue("摘要章节重复", IssueTypeAbstract, SeverityWarning)
		c.issues = append(c.issues, issue)
	}

	// 检查参考文献
	referenceTitleCount := len(structure["references_title"])
	if referenceTitleCount == 0 {
		issue := c.generateStructureIssue("缺少参考文献章节", IssueTypeReference, SeverityWarning)
		c.issues = append(c.issues, issue)
	}

	referenceItemCount := len(structure["reference_item"])
	if referenceItemCount == 0 && referenceTitleCount > 0 {
		issue := c.generateStructureIssue("参考文献章节为空", IssueTypeReference, SeverityWarning)
		c.issues = append(c.issues, issue)
	}
}

// generateFontIssue 生成字体问题
func (c *UniOfficeFormatChecker) generateFontIssue(targetType string, position int, paraInfo map[string]interface{}, description string, issueType IssueType, severity SeverityLevel) FormatIssue {
	return FormatIssue{
		ID:          c.generateIssueID(),
		Type:        issueType,
		Severity:    severity,
		Page:        c.estimatePage(position),
		Position:    position,
		Description: fmt.Sprintf("%s: %s", paraInfo["text"].(string), description),
		Original:    "当前字体格式",
		Suggestion:  "应用标准字体格式",
		Details: map[string]interface{}{
			"target_type": targetType,
			"position":    position,
			"text":        paraInfo["text"],
		},
	}
}

// generateParagraphIssue 生成段落问题
func (c *UniOfficeFormatChecker) generateParagraphIssue(position int, paraInfo map[string]interface{}, description string, issueType IssueType, severity SeverityLevel) FormatIssue {
	return FormatIssue{
		ID:          c.generateIssueID(),
		Type:        issueType,
		Severity:    severity,
		Page:        c.estimatePage(position),
		Position:    position,
		Description: fmt.Sprintf("%s: %s", paraInfo["text"].(string), description),
		Original:    "当前段落格式",
		Suggestion:  "调整段落格式",
		Details: map[string]interface{}{
			"position": position,
			"text":     paraInfo["text"],
		},
	}
}

// generateStructureIssue 生成结构问题
func (c *UniOfficeFormatChecker) generateStructureIssue(description string, issueType IssueType, severity SeverityLevel) FormatIssue {
	return FormatIssue{
		ID:          c.generateIssueID(),
		Type:        issueType,
		Severity:    severity,
		Page:        1,
		Position:    0,
		Description: description,
		Original:    "文档结构",
		Suggestion:  "修正文档结构",
		Details: map[string]interface{}{
			"category": "structure",
		},
	}
}

// generateIssueID 生成问题ID
func (c *UniOfficeFormatChecker) generateIssueID() string {
	return "issue_" + strconv.Itoa(len(c.issues)+1)
}

// estimatePage 估算页码
func (c *UniOfficeFormatChecker) estimatePage(position int) int {
	return (position + 20) / 21 // 简化估算
}

// generateCheckResult 生成检查结果
func (c *UniOfficeFormatChecker) generateCheckResult(filePath string) *CheckResult {
	// 统计问题数量
	errorCount := 0
	warningCount := 0
	infoCount := 0

	for _, issue := range c.issues {
		switch issue.Severity {
		case SeverityError:
			errorCount++
		case SeverityWarning:
			warningCount++
		case SeverityInfo:
			infoCount++
		}
	}

	totalIssues := errorCount + warningCount + infoCount

	// 计算质量分数
	qualityScore := 100.0
	if totalIssues > 0 {
		errorPenalty := float64(errorCount) * 5.0     // 每个错误扣5分
		warningPenalty := float64(warningCount) * 2.0 // 每个警告扣2分
		qualityScore = qualityScore - errorPenalty - warningPenalty
		if qualityScore < 0 {
			qualityScore = 0
		}
	}

	return &CheckResult{
		DocumentPath: filePath,
		DocInfo: map[string]interface{}{
			"paragraphs_count": len(c.doc.Paragraphs()),
			"issues_found":     totalIssues,
			"quality_score":    qualityScore,
			"processing_time":  time.Since(time.Now()).String(),
		},
		Issues:       c.issues,
		TotalIssues:  totalIssues,
		ErrorCount:   errorCount,
		WarningCount: warningCount,
		InfoCount:    infoCount,
	}
}

// GenerateCorrections 生成修正建议
func (c *UniOfficeFormatChecker) GenerateCorrections(ctx context.Context, result *CheckResult) ([]Correction, error) {
	var corrections []Correction

	for _, issue := range result.Issues {
		if issue.Severity != SeverityInfo { // 信息级别不生成修正建议
			correction := Correction{
				ID:          c.generateIssueID(),
				IssueID:     issue.ID,
				Type:        CorrectionTypeFont,
				Description: fmt.Sprintf("修复 %s: %s", issue.Type, issue.Suggestion),
				Original: map[string]interface{}{
					"current_format": "未知格式",
				},
				Corrected: map[string]interface{}{
					"target_format": issue.Suggestion,
				},
				Applied: false,
				Location: CorrectionLocation{
					Page:        issue.Page,
					StartPos:    issue.Position,
					EndPos:      issue.Position,
					ParagraphID: fmt.Sprintf("para_%d", issue.Position),
				},
				Action: "apply_format",
				Parameters: map[string]interface{}{
					"priority":   c.getPriorityFromSeverity(issue.Severity),
					"auto_apply": issue.Severity != SeverityWarning,
					"issue_type": issue.Type,
				},
			}
			corrections = append(corrections, correction)
		}
	}

	return corrections, nil
}

// getPriorityFromSeverity 从严重程度获取优先级
func (c *UniOfficeFormatChecker) getPriorityFromSeverity(severity SeverityLevel) int {
	switch severity {
	case SeverityError:
		return 1
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 3
	default:
		return 3
	}
}

// ApplyCorrections 应用修正建议
func (c *UniOfficeFormatChecker) ApplyCorrections(ctx context.Context, docPath string, corrections []Correction) (string, error) {
	// 这个功能应该由格式修复器处理
	return docPath, nil
}

// FixDocumentDirectly 直接修复文档
func (c *UniOfficeFormatChecker) FixDocumentDirectly(ctx context.Context, docPath string, standard FormatStandard) (string, error) {
	// 这个功能应该由格式修复器处理
	return docPath, nil
}
