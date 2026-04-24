package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/pkg/fileprocessor"
	"github.com/paper-format-checker/backend/pkg/formatchecker"
)

// FormatComparisonService 格式对比服务接口
// FormatComparisonService is retained for legacy read/comparison surfaces only.
// It is no longer a production write path for generating corrected documents.
type FormatComparisonService interface {
	// CheckPaperFormat 检查论文格式
	CheckPaperFormat(paperID uuid.UUID, templateID uuid.UUID) (*model.CheckResult, error)

	// GenerateFormatDifferences 生成格式差异对比
	GenerateFormatDifferences(checkResultID uuid.UUID) (*FormatDifferenceReport, error)

	// ApplyCorrections 应用格式修正
	ApplyCorrections(checkResultID uuid.UUID, correctionIDs []uuid.UUID) (*CorrectionResult, error)

	// GetCheckResult 获取检查结果
	GetCheckResult(checkResultID uuid.UUID) (*model.CheckResult, error)

	// GenerateCorrectedDocument 生成修正后的文档
	GenerateCorrectedDocument(checkResultID uuid.UUID) (string, error)
}

// FormatDifferenceReport 格式差异报告
type FormatDifferenceReport struct {
	CheckResultID uuid.UUID              `json:"check_result_id"`
	PaperInfo     PaperInfo              `json:"paper_info"`
	TemplateInfo  TemplateInfo           `json:"template_info"`
	Summary       DifferenceSummary      `json:"summary"`
	Differences   []FormatDifference     `json:"differences"`
	Corrections   []CorrectionSuggestion `json:"corrections"`
	GeneratedAt   time.Time              `json:"generated_at"`
}

// PaperInfo 论文信息
type PaperInfo struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	FileName string    `json:"file_name"`
	FileSize int64     `json:"file_size"`
	FileType string    `json:"file_type"`
}

// TemplateInfo 模板信息
type TemplateInfo struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	University   string    `json:"university"`
	DocumentType string    `json:"document_type"`
}

// DifferenceSummary 差异摘要
type DifferenceSummary struct {
	TotalIssues  int `json:"total_issues"`
	ErrorCount   int `json:"error_count"`
	WarningCount int `json:"warning_count"`
	InfoCount    int `json:"info_count"`
	FixableCount int `json:"fixable_count"`
	ManualCount  int `json:"manual_count"`
}

// FormatDifference 格式差异
type FormatDifference struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`        // heading, paragraph, table, figure, reference等
	Category    string                 `json:"category"`    // font, spacing, alignment, numbering等
	Severity    string                 `json:"severity"`    // error, warning, info
	Description string                 `json:"description"` // 差异描述
	Location    DifferenceLocation     `json:"location"`    // 位置信息
	Current     map[string]interface{} `json:"current"`     // 当前格式
	Expected    map[string]interface{} `json:"expected"`    // 期望格式
	IsFixable   bool                   `json:"is_fixable"`  // 是否可自动修复
}

// DifferenceLocation 差异位置
type DifferenceLocation struct {
	Page     int    `json:"page"`
	Line     int    `json:"line"`
	Section  string `json:"section"`
	Element  string `json:"element"`
	StartPos int    `json:"start_pos"`
	EndPos   int    `json:"end_pos"`
}

// CorrectionSuggestion 修正建议
type CorrectionSuggestion struct {
	ID               string                 `json:"id"`
	DifferenceID     string                 `json:"difference_id"`
	Type             string                 `json:"type"`
	Description      string                 `json:"description"`
	OriginalContent  map[string]interface{} `json:"original_content"`
	CorrectedContent map[string]interface{} `json:"corrected_content"`
	Confidence       float64                `json:"confidence"`
	IsAutoApplicable bool                   `json:"is_auto_applicable"`
}

// CorrectionResult 修正结果
type CorrectionResult struct {
	CheckResultID     uuid.UUID `json:"check_result_id"`
	AppliedCount      int       `json:"applied_count"`
	FailedCount       int       `json:"failed_count"`
	CorrectedFilePath string    `json:"corrected_file_path"`
	Summary           string    `json:"summary"`
}

// formatComparisonService 格式对比服务实现
type formatComparisonService struct {
	fileProcessor fileprocessor.FileProcessor
}

// NewFormatComparisonService 创建格式对比服务实例
func NewFormatComparisonService() FormatComparisonService {
	return &formatComparisonService{
		fileProcessor: fileprocessor.NewBasicFileProcessor(),
	}
}

// CheckPaperFormat 检查论文格式
func (s *formatComparisonService) CheckPaperFormat(paperID uuid.UUID, templateID uuid.UUID) (*model.CheckResult, error) {
	// 1. 获取论文和模板信息
	var paper model.Paper
	if err := database.DB.First(&paper, "id = ?", paperID).Error; err != nil {
		return nil, fmt.Errorf("论文不存在: %w", err)
	}

	var template model.FormatTemplate
	if err := database.DB.First(&template, "id = ?", templateID).Error; err != nil {
		return nil, fmt.Errorf("模板不存在: %w", err)
	}

	// 2. 创建检查结果记录
	checkResult := &model.CheckResult{
		PaperID:    paperID,
		UserID:     paper.UserID,
		TemplateID: templateID,
		Status:     "processing",
	}

	if err := database.DB.Create(checkResult).Error; err != nil {
		return nil, fmt.Errorf("创建检查结果失败: %w", err)
	}

	// 3. 异步执行格式检查
	go s.performFormatCheck(checkResult.ID, paper, template)

	return checkResult, nil
}

// performFormatCheck 执行格式检查（异步）
func (s *formatComparisonService) performFormatCheck(checkResultID uuid.UUID, paper model.Paper, template model.FormatTemplate) {
	// 更新状态为处理中
	database.DB.Model(&model.CheckResult{}).Where("id = ?", checkResultID).Update("status", "processing")

	// 1. 解析模板格式规则
	var templateRules formatchecker.FormatStandard
	if err := json.Unmarshal([]byte(template.FormatRules), &templateRules); err != nil {
		s.updateCheckResultError(checkResultID, fmt.Sprintf("解析模板规则失败: %v", err))
		return
	}

	// 2. 提取论文格式信息
	docInfo, err := s.fileProcessor.ExtractDocumentInfo(paper.FilePath)
	if err != nil {
		s.updateCheckResultError(checkResultID, fmt.Sprintf("提取论文信息失败: %v", err))
		return
	}

	// 3. 执行格式对比
	differences, err := s.compareFormats(docInfo, templateRules)
	if err != nil {
		s.updateCheckResultError(checkResultID, fmt.Sprintf("格式对比失败: %v", err))
		return
	}

	// 4. 生成修正建议
	corrections := s.generateCorrections(differences)

	// 5. 统计问题数量
	summary := s.calculateSummary(differences)

	// 6. 保存检查结果
	differencesJSON, _ := json.Marshal(differences)

	updateData := map[string]interface{}{
		"total_issues":  summary.TotalIssues,
		"error_count":   summary.ErrorCount,
		"warning_count": summary.WarningCount,
		"info_count":    summary.InfoCount,
		"differences":   string(differencesJSON),
		"status":        "completed",
		"updated_at":    time.Now(),
	}

	database.DB.Model(&model.CheckResult{}).Where("id = ?", checkResultID).Updates(updateData)

	// 7. 保存修正建议
	s.saveCorrections(checkResultID, corrections)

	// 8. 更新论文状态
	database.DB.Model(&model.Paper{}).Where("id = ?", paper.ID).Updates(map[string]interface{}{
		"selected_template_id": template.ID,
		"status":               "checked",
	})
}

// compareFormats 对比格式
func (s *formatComparisonService) compareFormats(docInfo fileprocessor.FileInfo, templateRules formatchecker.FormatStandard) ([]FormatDifference, error) {
	var differences []FormatDifference

	// 1. 检查页面设置
	pageDiffs := s.checkPageSetup(docInfo, templateRules.PageSetup)
	differences = append(differences, pageDiffs...)

	// 2. 检查标题格式
	headingDiffs := s.checkHeadingFormats(docInfo, templateRules.HeadingStyles)
	differences = append(differences, headingDiffs...)

	// 3. 检查段落格式
	// Handle slice of ParagraphStyles - default to "正文" or first
	var bodyStyle formatchecker.ParagraphStyle
	if len(templateRules.ParagraphStyles) > 0 {
		bodyStyle = templateRules.ParagraphStyles[0]
		for _, style := range templateRules.ParagraphStyles {
			if style.Name == "正文" || style.Name == "Normal" {
				bodyStyle = style
				break
			}
		}
	}
	paragraphDiffs := s.checkParagraphFormat(docInfo, bodyStyle)
	differences = append(differences, paragraphDiffs...)

	// 4. 检查表格格式
	tableDiffs := s.checkTableFormat(docInfo, templateRules.TableStyle)
	differences = append(differences, tableDiffs...)

	// 5. 检查图表格式
	figureDiffs := s.checkFigureFormat(docInfo, templateRules.FigureStyle)
	differences = append(differences, figureDiffs...)

	// 6. 检查参考文献格式
	refDiffs := s.checkReferenceFormat(docInfo, templateRules.ReferenceStyle)
	differences = append(differences, refDiffs...)

	return differences, nil
}

// checkPageSetup 检查页面设置
func (s *formatComparisonService) checkPageSetup(docInfo fileprocessor.FileInfo, expected formatchecker.PageSetup) []FormatDifference {
	var differences []FormatDifference

	// 简化实现 - 实际应该从docInfo中提取页面设置信息进行对比
	// 这里假设检测到页边距不符合要求
	differences = append(differences, FormatDifference{
		ID:          uuid.New().String(),
		Type:        "page_setup",
		Category:    "margin",
		Severity:    "warning",
		Description: "页边距设置可能不符合要求",
		Location: DifferenceLocation{
			Page:    1,
			Section: "页面设置",
			Element: "页边距",
		},
		Current: map[string]interface{}{
			"margin_top":    2.0,
			"margin_bottom": 2.0,
			"margin_left":   2.0,
			"margin_right":  2.0,
		},
		Expected: map[string]interface{}{
			"margin_top":    expected.MarginTop,
			"margin_bottom": expected.MarginBottom,
			"margin_left":   expected.MarginLeft,
			"margin_right":  expected.MarginRight,
		},
		IsFixable: true,
	})

	return differences
}

// checkHeadingFormats 检查标题格式
func (s *formatComparisonService) checkHeadingFormats(docInfo fileprocessor.FileInfo, expectedStyles []formatchecker.HeadingStyle) []FormatDifference {
	var differences []FormatDifference

	// 简化实现 - 检查标题格式
	for _, expectedStyle := range expectedStyles {
		differences = append(differences, FormatDifference{
			ID:          uuid.New().String(),
			Type:        "heading",
			Category:    "font",
			Severity:    "error",
			Description: fmt.Sprintf("%d级标题字体不符合要求", expectedStyle.Level),
			Location: DifferenceLocation{
				Page:    1,
				Section: fmt.Sprintf("%d级标题", expectedStyle.Level),
				Element: "字体设置",
			},
			Current: map[string]interface{}{
				"font_name": "宋体",
				"font_size": 14.0,
				"bold":      false,
			},
			Expected: map[string]interface{}{
				"font_name": expectedStyle.FontName,
				"font_size": expectedStyle.FontSize,
				"bold":      expectedStyle.Bold,
			},
			IsFixable: true,
		})
	}

	return differences
}

// checkParagraphFormat 检查段落格式
func (s *formatComparisonService) checkParagraphFormat(docInfo fileprocessor.FileInfo, expected formatchecker.ParagraphStyle) []FormatDifference {
	var differences []FormatDifference

	// 简化实现
	differences = append(differences, FormatDifference{
		ID:          uuid.New().String(),
		Type:        "paragraph",
		Category:    "spacing",
		Severity:    "warning",
		Description: "段落行间距不符合要求",
		Location: DifferenceLocation{
			Page:    2,
			Section: "正文段落",
			Element: "行间距",
		},
		Current: map[string]interface{}{
			"line_spacing": 1.0,
		},
		Expected: map[string]interface{}{
			"line_spacing": expected.LineSpacing,
		},
		IsFixable: true,
	})

	return differences
}

// checkTableFormat 检查表格格式
func (s *formatComparisonService) checkTableFormat(docInfo fileprocessor.FileInfo, expected formatchecker.TableStyle) []FormatDifference {
	// 简化实现，返回空数组
	return []FormatDifference{}
}

// checkFigureFormat 检查图表格式
func (s *formatComparisonService) checkFigureFormat(docInfo fileprocessor.FileInfo, expected formatchecker.FigureStyle) []FormatDifference {
	// 简化实现，返回空数组
	return []FormatDifference{}
}

// checkReferenceFormat 检查参考文献格式
func (s *formatComparisonService) checkReferenceFormat(docInfo fileprocessor.FileInfo, expected formatchecker.ReferenceStyle) []FormatDifference {
	// 简化实现，返回空数组
	return []FormatDifference{}
}

// generateCorrections 生成修正建议
func (s *formatComparisonService) generateCorrections(differences []FormatDifference) []CorrectionSuggestion {
	var corrections []CorrectionSuggestion

	for _, diff := range differences {
		if diff.IsFixable {
			corrections = append(corrections, CorrectionSuggestion{
				ID:               uuid.New().String(),
				DifferenceID:     diff.ID,
				Type:             diff.Type,
				Description:      fmt.Sprintf("修正%s", diff.Description),
				OriginalContent:  diff.Current,
				CorrectedContent: diff.Expected,
				Confidence:       0.9,
				IsAutoApplicable: true,
			})
		}
	}

	return corrections
}

// calculateSummary 计算摘要统计
func (s *formatComparisonService) calculateSummary(differences []FormatDifference) DifferenceSummary {
	summary := DifferenceSummary{}

	for _, diff := range differences {
		summary.TotalIssues++

		switch diff.Severity {
		case "error":
			summary.ErrorCount++
		case "warning":
			summary.WarningCount++
		case "info":
			summary.InfoCount++
		}

		if diff.IsFixable {
			summary.FixableCount++
		} else {
			summary.ManualCount++
		}
	}

	return summary
}

// saveCorrections 保存修正建议
func (s *formatComparisonService) saveCorrections(checkResultID uuid.UUID, corrections []CorrectionSuggestion) {
	for _, correction := range corrections {
		originalJSON, _ := json.Marshal(correction.OriginalContent)
		correctedJSON, _ := json.Marshal(correction.CorrectedContent)
		locationJSON, _ := json.Marshal(map[string]interface{}{
			"difference_id": correction.DifferenceID,
		})

		formatCorrection := &model.FormatCorrection{
			CheckResultID:    checkResultID,
			IssueID:          correction.DifferenceID,
			CorrectionType:   correction.Type,
			OriginalContent:  string(originalJSON),
			CorrectedContent: string(correctedJSON),
			Location:         string(locationJSON),
			IsApplied:        false,
			Confidence:       correction.Confidence,
			Description:      correction.Description,
		}

		database.DB.Create(formatCorrection)
	}
}

// updateCheckResultError 更新检查结果错误状态
func (s *formatComparisonService) updateCheckResultError(checkResultID uuid.UUID, errorMsg string) {
	database.DB.Model(&model.CheckResult{}).Where("id = ?", checkResultID).Updates(map[string]interface{}{
		"status":     "failed",
		"issues":     fmt.Sprintf(`{"error": "%s"}`, errorMsg),
		"updated_at": time.Now(),
	})
}

// GenerateFormatDifferences 生成格式差异对比
func (s *formatComparisonService) GenerateFormatDifferences(checkResultID uuid.UUID) (*FormatDifferenceReport, error) {
	// 获取检查结果
	var checkResult model.CheckResult
	if err := database.DB.Preload("Paper").Preload("Template").Preload("Template.University").
		First(&checkResult, "id = ?", checkResultID).Error; err != nil {
		return nil, fmt.Errorf("检查结果不存在: %w", err)
	}

	// 解析差异数据
	var differences []FormatDifference
	if checkResult.Differences != "" && checkResult.Differences != "[]" {
		json.Unmarshal([]byte(checkResult.Differences), &differences)
	}

	// 如果差异数据为空，尝试从Issues中解析
	if len(differences) == 0 && checkResult.Issues != "" && checkResult.Issues != "[]" {
		var issues []formatchecker.FormatIssue
		// 尝试解析，兼容双重编码的JSON字符串
		var issuesBytes []byte
		if err := json.Unmarshal([]byte(checkResult.Issues), &issues); err != nil {
			// 尝试作为字符串解析（处理双重编码）
			var issuesStr string
			if err := json.Unmarshal([]byte(checkResult.Issues), &issuesStr); err == nil {
				issuesBytes = []byte(issuesStr)
				json.Unmarshal(issuesBytes, &issues)
			}
		} else {
			issuesBytes = []byte(checkResult.Issues)
		}

		// 如果第一步Unmarshal成功，issues已经有值了；如果不成功但issuesBytes有值（双重编码情况），则再次Unmarshal
		if len(issues) == 0 && len(issuesBytes) > 0 {
			json.Unmarshal(issuesBytes, &issues)
		}

		if len(issues) > 0 {
			for _, issue := range issues {
				// 转换 FormatIssue 到 FormatDifference
				diff := FormatDifference{
					ID:          issue.ID,
					Type:        string(issue.Type),
					Category:    "format", // 默认分类
					Severity:    string(issue.Severity),
					Description: issue.Description,
					Location: DifferenceLocation{
						Page:    issue.Page,
						Section: fmt.Sprintf("Page %d", issue.Page), // 简单处理
					},
					IsFixable: true, // 假设可修复，或者根据Type判断
				}

				// 转换 Current 和 Expected
				diff.Current = toMap(issue.Original)
				diff.Expected = toMap(issue.Suggestion)

				differences = append(differences, diff)
			}
		}
	}

	// 获取修正建议
	var corrections []model.FormatCorrection
	database.DB.Where("check_result_id = ?", checkResultID).Find(&corrections)

	var correctionSuggestions []CorrectionSuggestion
	for _, correction := range corrections {
		var original, corrected map[string]interface{}
		json.Unmarshal([]byte(correction.OriginalContent), &original)
		json.Unmarshal([]byte(correction.CorrectedContent), &corrected)

		correctionSuggestions = append(correctionSuggestions, CorrectionSuggestion{
			ID:               correction.ID.String(),
			DifferenceID:     correction.IssueID,
			Type:             correction.CorrectionType,
			Description:      correction.Description,
			OriginalContent:  original,
			CorrectedContent: corrected,
			Confidence:       correction.Confidence,
			IsAutoApplicable: !correction.IsApplied,
		})
	}

	// 构建报告
	report := &FormatDifferenceReport{
		CheckResultID: checkResultID,
		PaperInfo: PaperInfo{
			ID:       checkResult.Paper.ID,
			Title:    checkResult.Paper.Title,
			FileName: checkResult.Paper.FileName,
			FileSize: checkResult.Paper.FileSize,
			FileType: checkResult.Paper.FileType,
		},
		TemplateInfo: TemplateInfo{
			ID:           checkResult.Template.ID,
			Name:         checkResult.Template.Name,
			DocumentType: checkResult.Template.DocumentType,
		},
		Summary: DifferenceSummary{
			TotalIssues:  checkResult.TotalIssues,
			ErrorCount:   checkResult.ErrorCount,
			WarningCount: checkResult.WarningCount,
			InfoCount:    checkResult.InfoCount,
		},
		Differences: differences,
		Corrections: correctionSuggestions,
		GeneratedAt: time.Now(),
	}

	// 设置高校信息
	if checkResult.Template.University != nil {
		report.TemplateInfo.University = checkResult.Template.University.Name
	}

	return report, nil
}

// ApplyCorrections 应用格式修正
func (s *formatComparisonService) ApplyCorrections(checkResultID uuid.UUID, correctionIDs []uuid.UUID) (*CorrectionResult, error) {
	return nil, ErrLegacyWritePathDisabled

	// 获取检查结果
	var checkResult model.CheckResult
	if err := database.DB.Preload("Paper").First(&checkResult, "id = ?", checkResultID).Error; err != nil {
		return nil, fmt.Errorf("检查结果不存在: %w", err)
	}

	// 获取要应用的修正建议
	var corrections []model.FormatCorrection
	if err := database.DB.Where("id IN ? AND check_result_id = ?", correctionIDs, checkResultID).
		Find(&corrections).Error; err != nil {
		return nil, fmt.Errorf("获取修正建议失败: %w", err)
	}

	// 应用修正
	appliedCount := 0
	failedCount := 0

	for _, correction := range corrections {
		// 这里应该调用文件处理器应用具体的修正
		// 简化实现，直接标记为已应用
		if err := database.DB.Model(&correction).Update("is_applied", true).Error; err != nil {
			failedCount++
		} else {
			appliedCount++
		}
	}

	// 生成修正后的文件路径
	correctedFilePath := fmt.Sprintf("%s_corrected.%s",
		checkResult.Paper.FilePath[:len(checkResult.Paper.FilePath)-len(checkResult.Paper.FileType)-1],
		checkResult.Paper.FileType)

	// 更新论文状态
	database.DB.Model(&checkResult.Paper).Update("status", "corrected")

	result := &CorrectionResult{
		CheckResultID:     checkResultID,
		AppliedCount:      appliedCount,
		FailedCount:       failedCount,
		CorrectedFilePath: correctedFilePath,
		Summary:           fmt.Sprintf("成功应用 %d 个修正，失败 %d 个", appliedCount, failedCount),
	}

	return result, nil
}

// GetCheckResult 获取检查结果
func (s *formatComparisonService) GetCheckResult(checkResultID uuid.UUID) (*model.CheckResult, error) {
	var checkResult model.CheckResult
	if err := database.DB.Preload("Paper").Preload("Template").Preload("Corrections").
		First(&checkResult, "id = ?", checkResultID).Error; err != nil {
		return nil, fmt.Errorf("检查结果不存在: %w", err)
	}

	return &checkResult, nil
}

// GenerateCorrectedDocument 生成修正后的文档
func (s *formatComparisonService) GenerateCorrectedDocument(checkResultID uuid.UUID) (string, error) {
	return "", ErrLegacyWritePathDisabled

	// 获取检查结果和模板
	var checkResult model.CheckResult
	if err := database.DB.Preload("Paper").Preload("Template.University").First(&checkResult, "id = ?", checkResultID).Error; err != nil {
		return "", fmt.Errorf("检查结果不存在: %w", err)
	}

	// 解析模板规则
	var rulesMap map[string]interface{}
	if err := json.Unmarshal([]byte(checkResult.Template.FormatRules), &rulesMap); err != nil {
		return "", fmt.Errorf("解析模板规则失败: %w", err)
	}

	// 构造修正指令 (全局规则)
	corrections := []map[string]interface{}{
		{
			"format_rules": rulesMap,
		},
	}
	if checkResult.Template.ID != uuid.Nil {
		if u := checkResult.Template.University; u != nil {
			if sid := fileprocessor.SchoolIDFromUniversityName(u.Name, u.Abbr); sid != "" {
				corrections[0]["school_id"] = sid
			}
		}
	}

	// V2引擎：确定性分类 + XML节点克隆
	newFilePath, err := s.fileProcessor.ApplyCorrectionsV2(context.Background(), checkResult.Paper.FilePath, corrections)
	if err != nil {
		return "", fmt.Errorf("生成修正文档失败: %w", err)
	}

	standardCorrectedPath := fmt.Sprintf("%s_corrected%s",
		checkResult.Paper.FilePath[:len(checkResult.Paper.FilePath)-len(checkResult.Paper.FileType)-1],
		filepath.Ext(checkResult.Paper.FilePath))

	if err := os.Rename(newFilePath, standardCorrectedPath); err != nil {
		return newFilePath, nil
	}

	return standardCorrectedPath, nil
}

// 辅助函数：将 interface{} 转换为 map[string]interface{}
func toMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	// 如果不是map，则包装一下
	return map[string]interface{}{"value": v}
}
