package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/pkg/fileprocessor"
	"github.com/paper-format-checker/backend/pkg/formatchecker"
)

// TemplateParserService 模板解析服务接口
type TemplateParserService interface {
	// ParsePaperToTemplate 从论文中解析格式模板
	ParsePaperToTemplate(paperID uuid.UUID) (*model.FormatTemplate, error)

	// ParseTemplateFromFile 从文件直接解析模板
	ParseTemplateFromFile(filePath string) (*formatchecker.FormatStandard, error)

	// DetectPossibleTemplates 检测论文可能匹配的模板
	DetectPossibleTemplates(paperID uuid.UUID) ([]TemplateMatch, error)

	// CreateTemplateFromPaper 从论文创建新模板
	CreateTemplateFromPaper(paperID uuid.UUID, templateName string, universityID *int64) (*model.FormatTemplate, error)

	// GetTemplatesByUniversity 获取指定高校的模板
	GetTemplatesByUniversity(universityID int64) ([]model.FormatTemplate, error)

	// GetPublicTemplates 获取公开模板
	GetPublicTemplates() ([]model.FormatTemplate, error)
}

// TemplateMatch 模板匹配结果
type TemplateMatch struct {
	Template   model.FormatTemplate `json:"template"`
	Confidence float64              `json:"confidence"` // 匹配置信度 0-1
	Reasons    []string             `json:"reasons"`    // 匹配原因
}

// templateParserService 模板解析服务实现
type templateParserService struct {
	fileProcessor fileprocessor.FileProcessor
}

// NewTemplateParserService 创建模板解析服务实例
func NewTemplateParserService() TemplateParserService {
	return &templateParserService{
		fileProcessor: fileprocessor.NewBasicFileProcessor(),
	}
}

// ParseTemplateFromFile 从文件直接解析模板
func (s *templateParserService) ParseTemplateFromFile(filePath string) (*formatchecker.FormatStandard, error) {
	parser := formatchecker.NewTemplateParser()
	standard, err := parser.ParseTemplate(filePath)
	if err != nil {
		return nil, fmt.Errorf("解析模板文件失败: %w", err)
	}
	return standard, nil
}

// ParsePaperToTemplate 从论文中解析格式模板
func (s *templateParserService) ParsePaperToTemplate(paperID uuid.UUID) (*model.FormatTemplate, error) {
	// 1. 获取论文信息
	var paper model.Paper
	if err := database.DB.First(&paper, "id = ?", paperID).Error; err != nil {
		return nil, fmt.Errorf("论文不存在: %w", err)
	}

	// 2. 解析论文格式
	standard, err := s.ParseTemplateFromFile(paper.FilePath)
	if err != nil {
		return nil, fmt.Errorf("解析论文格式失败: %w", err)
	}

	// 3. 将格式规则转换为JSON
	formatRulesJSON, err := json.Marshal(standard)
	if err != nil {
		return nil, fmt.Errorf("序列化格式规则失败: %w", err)
	}

	// 4. 创建模板
	template := &model.FormatTemplate{
		TemplateID:        fmt.Sprintf("auto_parsed_%s", uuid.New().String()[:8]),
		Name:              fmt.Sprintf("从《%s》解析的模板", paper.Title),
		Source:            "auto_parsed",
		Version:           "1.0",
		IsPublic:          false, // 自动解析的模板默认不公开
		IsActive:          true,
		FormatRules:       string(formatRulesJSON),
		ParsedFromPaperID: &paperID,
		ParseConfidence:   0.8, // 默认置信度，实际应该根据解析完整度计算
		Description:       fmt.Sprintf("从论文《%s》自动解析生成的格式模板", paper.Title),
	}

	// 5. 保存到数据库
	if err := database.DB.Create(template).Error; err != nil {
		return nil, fmt.Errorf("保存模板失败: %w", err)
	}

	// 6. 更新论文状态
	paper.Status = "parsed"
	database.DB.Save(&paper)

	return template, nil
}

// DetectPossibleTemplates 检测论文可能匹配的模板
func (s *templateParserService) DetectPossibleTemplates(paperID uuid.UUID) ([]TemplateMatch, error) {
	// 1. 获取论文信息
	var paper model.Paper
	if err := database.DB.First(&paper, "id = ?", paperID).Error; err != nil {
		return nil, fmt.Errorf("论文不存在: %w", err)
	}

	// 2. 解析论文格式特征
	paperFeatures, err := s.extractPaperFeatures(paper.FilePath, paper.FileType)
	if err != nil {
		return nil, fmt.Errorf("提取论文特征失败: %w", err)
	}

	// 3. 获取所有可用模板
	var templates []model.FormatTemplate
	if err := database.DB.Where("is_active = ? AND is_public = ?", true, true).Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("获取模板列表失败: %w", err)
	}

	// 4. 计算匹配度
	var matches []TemplateMatch
	for _, template := range templates {
		confidence, reasons := s.calculateTemplateMatch(paperFeatures, template.FormatRules)
		if confidence > 0.3 { // 只返回置信度大于30%的匹配
			matches = append(matches, TemplateMatch{
				Template:   template,
				Confidence: confidence,
				Reasons:    reasons,
			})
		}
	}

	// 5. 按置信度排序
	for i := 0; i < len(matches)-1; i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[i].Confidence < matches[j].Confidence {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}

	// 6. 保存检测结果到论文记录
	detectedTemplatesJSON, _ := json.Marshal(matches)
	paper.AutoDetectedTemplates = string(detectedTemplatesJSON)
	database.DB.Save(&paper)

	return matches, nil
}

// CreateTemplateFromPaper 从论文创建新模板
func (s *templateParserService) CreateTemplateFromPaper(paperID uuid.UUID, templateName string, universityID *int64) (*model.FormatTemplate, error) {
	// 1. 先解析论文格式
	template, err := s.ParsePaperToTemplate(paperID)
	if err != nil {
		return nil, err
	}

	// 2. 更新模板信息
	template.Name = templateName
	template.UniversityID = universityID
	template.Source = "university_upload"
	template.IsPublic = true // 高校上传的模板设为公开

	// 3. 如果指定了高校，生成更规范的模板ID
	if universityID != nil {
		var university model.University
		if err := database.DB.First(&university, "id = ?", *universityID).Error; err == nil {
			template.TemplateID = fmt.Sprintf("%s_%s_%s",
				strings.ToLower(university.Abbr),
				"template",
				uuid.New().String()[:8])
		}
	}

	// 4. 保存更新
	if err := database.DB.Save(template).Error; err != nil {
		return nil, fmt.Errorf("更新模板失败: %w", err)
	}

	return template, nil
}

// GetTemplatesByUniversity 获取指定高校的模板
func (s *templateParserService) GetTemplatesByUniversity(universityID int64) ([]model.FormatTemplate, error) {
	var templates []model.FormatTemplate
	err := database.DB.Where("university_id = ? AND is_active = ?", universityID, true).
		Preload("University").
		Find(&templates).Error

	if err != nil {
		return nil, fmt.Errorf("获取高校模板失败: %w", err)
	}

	return templates, nil
}

// GetPublicTemplates 获取公开模板
func (s *templateParserService) GetPublicTemplates() ([]model.FormatTemplate, error) {
	var templates []model.FormatTemplate
	err := database.DB.Where("is_public = ? AND is_active = ?", true, true).
		Preload("University").
		Order("usage_count DESC, created_at DESC").
		Find(&templates).Error

	if err != nil {
		return nil, fmt.Errorf("获取公开模板失败: %w", err)
	}

	return templates, nil
}

// extractPaperFeatures 提取论文特征用于模板匹配
func (s *templateParserService) extractPaperFeatures(filePath, fileType string) (map[string]interface{}, error) {
	docInfo, err := s.fileProcessor.ExtractDocumentInfo(filePath)
	if err != nil {
		return nil, err
	}

	features := map[string]interface{}{
		"page_count":     docInfo.Pages,
		"word_count":     docInfo.WordCount,
		"has_toc":        strings.Contains(strings.ToLower(docInfo.Title), "目录"),
		"has_abstract":   strings.Contains(strings.ToLower(docInfo.Title), "摘要"),
		"has_references": strings.Contains(strings.ToLower(docInfo.Title), "参考文献"),
		"language":       "chinese", // 简化处理
	}

	return features, nil
}

// calculateTemplateMatch 计算模板匹配度
func (s *templateParserService) calculateTemplateMatch(paperFeatures map[string]interface{}, templateRulesJSON string) (float64, []string) {
	// 尝试解析为标准格式
	var standard formatchecker.FormatStandard
	if err := json.Unmarshal([]byte(templateRulesJSON), &standard); err != nil {
		// 尝试旧格式
		return 0, []string{"模板格式解析失败"}
	}

	var confidence float64
	var reasons []string

	// 基础匹配分数
	confidence = 0.5
	reasons = append(reasons, "基础格式匹配")

	// 根据页数判断文档类型匹配度
	if pageCount, ok := paperFeatures["page_count"].(int); ok {
		if pageCount > 20 && pageCount < 100 {
			confidence += 0.2
			reasons = append(reasons, "页数符合学术论文范围")
		}
	}

	// 根据是否包含摘要等结构判断
	if hasAbstract, ok := paperFeatures["has_abstract"].(bool); ok && hasAbstract {
		confidence += 0.1
		reasons = append(reasons, "包含摘要结构")
	}

	if hasReferences, ok := paperFeatures["has_references"].(bool); ok && hasReferences {
		confidence += 0.1
		reasons = append(reasons, "包含参考文献")
	}

	// 确保置信度在0-1范围内
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence, reasons
}
