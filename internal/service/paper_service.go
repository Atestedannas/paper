package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/pkg/fileprocessor"
	"github.com/paper-format-checker/backend/pkg/formatchecker"
)

// PaperService 论文服务
type PaperService struct {
	config *config.Config
}

// NewPaperService 创建论文服务
func NewPaperService(config *config.Config) PaperService {
	return PaperService{
		config: config,
	}
}

// CheckPaperFormat 检查论文格式
func (s PaperService) CheckPaperFormat(userID, paperID, templateID uuid.UUID) (*model.CheckResult, error) {
	// 1. 获取论文信息

	paper, err := s.GetPaperByID(userID, paperID)
	if err != nil {
		return nil, fmt.Errorf("failed to get paper: %v", err)
	}

	// 2. 确定使用的模板ID
	if templateID == uuid.Nil {
		if paper.SelectedTemplateID != nil {
			templateID = *paper.SelectedTemplateID
		} else {
			// 如果没有指定模板，返回错误或使用默认逻辑
			return nil, fmt.Errorf("no template selected for paper")
		}
	}

	// 3. 获取格式模板
	var template model.FormatTemplate
	if err := database.DB.Where("id = ?", templateID).First(&template).Error; err != nil {
		return nil, fmt.Errorf("failed to get format template: %v", err)
	}

	// 4. 解析格式规则
	var rulesMap map[string]interface{}
	// 尝试直接解析
	if err := json.Unmarshal([]byte(template.FormatRules), &rulesMap); err != nil {
		// 如果失败，尝试先解析为字符串（处理双重序列化的情况）
		var jsonString string
		if err2 := json.Unmarshal([]byte(template.FormatRules), &jsonString); err2 == nil {
			// 如果解析为字符串成功，再尝试解析该字符串的内容
			if err3 := json.Unmarshal([]byte(jsonString), &rulesMap); err3 != nil {
				return nil, fmt.Errorf("failed to unmarshal format rules (double encoded): %v", err3)
			}
		} else {
			return nil, fmt.Errorf("failed to unmarshal format rules: %v", err)
		}
	}

	// 5. 创建检查器
	standard := formatchecker.ParseRequirementsToStandard(rulesMap)

	processor := fileprocessor.NewBasicFileProcessor() // 使用基本处理器进行检查
	factory := formatchecker.NewCheckerFactory()

	checker, err := factory.CreateChecker(paper.FileType, processor, standard)
	if err != nil {
		return nil, fmt.Errorf("failed to create checker: %v", err)
	}

	// 6. 执行检查
	ctx := context.Background()
	checkResult, err := checker.Check(ctx, paper.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to check paper format: %v", err)
	}

	// 7. 保存检查结果
	issuesJSON, _ := json.Marshal(checkResult.Issues)

	result := &model.CheckResult{
		ID:               uuid.New(),
		PaperID:          paperID,
		UserID:           userID,
		TemplateID:       templateID,
		FormatTemplateID: templateID, // 同时赋值以满足数据库约束
		Status:           "completed",
		TotalIssues:      checkResult.TotalIssues,
		ErrorCount:       checkResult.ErrorCount,
		WarningCount:     checkResult.WarningCount,
		InfoCount:        checkResult.InfoCount,
		Issues:           string(issuesJSON),
		Differences:      "[]", // 初始化为空 JSON 数组，避免 PostgreSQL 报错
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := database.DB.Create(result).Error; err != nil {
		return nil, fmt.Errorf("failed to save check result: %v", err)
	}

	return result, nil
}

// FixPaperFormatByParsedRequirements 根据解析的要求修复论文格式
func (s PaperService) FixPaperFormatByParsedRequirements(userID, paperID uuid.UUID, requirements map[string]interface{}) (interface{}, error) {
	// 获取论文信息

	paper, err := s.GetPaperByID(userID, paperID)
	if err != nil {
		return nil, fmt.Errorf("failed to get paper: %v", err)
	}

	// 创建检查器配置
	standard := formatchecker.ParseRequirementsToStandard(requirements)
	processor := fileprocessor.NewBasicFileProcessor() // 使用基本处理器进行检查
	factory := formatchecker.NewCheckerFactory()
	checker, err := factory.CreateChecker(paper.FileType, processor, standard)

	if err != nil {
		return nil, fmt.Errorf("failed to create checker: %v", err)
	}

	// 修复文档
	ctx := context.Background()
	fixedPath, err := checker.FixDocumentDirectly(ctx, paper.FilePath, standard)
	if err != nil {
		return nil, fmt.Errorf("failed to fix document: %v", err)
	}
	if fixedPath == "" {
		return nil, fmt.Errorf("failed to fix document: empty corrected file path")
	}

	// 更新论文记录
	paper.CorrectedFilePath = fixedPath
	paper.Status = "corrected"
	if err := database.DB.Save(paper).Error; err != nil {
		return nil, fmt.Errorf("failed to update paper record: %v", err)
	}

	return map[string]interface{}{
		"corrected_file_path": fixedPath,
		"download_url":        fmt.Sprintf("/api/v1/papers/%s/corrected-file", paper.ID.String()),
	}, nil
}

// FixPaperFormat 修复论文格式
func (s PaperService) FixPaperFormat(userID, paperID, checkResultID uuid.UUID) (interface{}, error) {
	// 1. 获取论文和检查结果
	paper, err := s.GetPaperByID(userID, paperID)
	if err != nil {
		return nil, fmt.Errorf("failed to get paper: %v", err)
	}

	var checkResult model.CheckResult
	if err := database.DB.Where("id = ?", checkResultID).First(&checkResult).Error; err != nil {
		return nil, fmt.Errorf("failed to get check result: %v", err)
	}

	// 2. 获取格式模板
	var template model.FormatTemplate
	if err := database.DB.Where("id = ?", checkResult.TemplateID).First(&template).Error; err != nil {
		return nil, fmt.Errorf("failed to get format template: %v", err)
	}

	// 3. 准备检查器
	var rulesMap map[string]interface{}
	// 尝试直接解析
	if err := json.Unmarshal([]byte(template.FormatRules), &rulesMap); err != nil {
		// 如果失败，尝试先解析为字符串（处理双重序列化的情况）
		var jsonString string
		if err2 := json.Unmarshal([]byte(template.FormatRules), &jsonString); err2 == nil {
			// 如果解析为字符串成功，再尝试解析该字符串的内容
			if err3 := json.Unmarshal([]byte(jsonString), &rulesMap); err3 != nil {
				return nil, fmt.Errorf("failed to unmarshal format rules (double encoded): %v", err3)
			}
		} else {
			return nil, fmt.Errorf("failed to unmarshal format rules: %v", err)
		}
	}

	processor := fileprocessor.NewEnhancedProcessor()

	// 4. 修复文档
	ctx := context.Background()
	var fixedPath string
	var fixErr error

	// 使用增强处理器进行精确格式修正
	fixedPath, fixErr = processor.ApplyCorrections(ctx, paper.FilePath, []map[string]interface{}{
		{"format_rules": rulesMap},
	})
	if fixErr != nil {
		return nil, fmt.Errorf("failed to fix document: %v", fixErr)
	}
	if fixedPath == "" {
		return nil, fmt.Errorf("failed to fix document: empty corrected file path")
	}

	// 5. 更新论文记录
	paper.CorrectedFilePath = fixedPath
	paper.Status = "corrected"
	if err := database.DB.Save(paper).Error; err != nil {
		return nil, fmt.Errorf("failed to update paper record: %v", err)
	}

	return map[string]interface{}{
		"corrected_file_path": fixedPath,
		"download_url":        fmt.Sprintf("/api/v1/papers/%s/corrected-file", paper.ID.String()),
	}, nil
}

// UploadPaper 上传论文
func (s PaperService) UploadPaper(userID uuid.UUID, title, description string, formatStandardID uuid.UUID, file *multipart.FileHeader, fileType string, c *gin.Context) (*model.Paper, error) {
	// 处理匿名用户的情况
	// 保存文件到本地
	fileName := fmt.Sprintf("%s.%s", uuid.New().String(), fileType)
	filePath := filepath.Join("uploads", "papers", fileName)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	// 保存文件
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		return nil, fmt.Errorf("failed to save file: %w", err)
	}

	var selectedTemplateID *uuid.UUID
	if formatStandardID != uuid.Nil {
		selectedTemplateID = &formatStandardID
	}

	// 创建论文记录
	paper := &model.Paper{
		ID:                    uuid.New(),
		UserID:                userID,
		Title:                 title,
		Description:           description,
		FilePath:              filePath,
		FileName:              file.Filename,
		FileSize:              file.Size,
		FileType:              fileType,
		SelectedTemplateID:    selectedTemplateID,
		Status:                "uploaded",
		ParsedInfo:            "{}", // 初始化为空JSON对象
		AutoDetectedTemplates: "[]", // 初始化为空JSON数组
	}

	// 保存到数据库
	if err := database.DB.Create(paper).Error; err != nil {
		return nil, fmt.Errorf("failed to save paper to database: %w", err)
	}

	// 如果提供了格式标准ID，则立即应用格式修复
	if formatStandardID != uuid.Nil {
		// 获取格式模板
		var template model.FormatTemplate
		if err := database.DB.Where("id = ?", formatStandardID).First(&template).Error; err != nil {
		} else {
			// 解析格式规则
			var rulesMap map[string]interface{}
			if err := json.Unmarshal([]byte(template.FormatRules), &rulesMap); err != nil {
				// 尝试先解析为字符串（处理双重序列化的情况）
				var jsonString string
				if err2 := json.Unmarshal([]byte(template.FormatRules), &jsonString); err2 == nil {
					if err3 := json.Unmarshal([]byte(jsonString), &rulesMap); err3 != nil {
					} else {
					}
				} else {
				}
			} else {

				// 如果是直接结构，使用 ParseRequirementsToStandard 函数
				//standard := formatchecker.ParseRequirementsToStandard(rulesMap) // 使用标准解析，但直接使用rulesMap
				processor := fileprocessor.NewEnhancedProcessor()

				ctx := context.Background()
				// 使用增强处理器直接应用格式规则
				correctedPath, err := processor.ApplyCorrections(ctx, filePath, []map[string]interface{}{
					{"format_rules": rulesMap},
				})
				if err != nil {
				} else if correctedPath != "" {
					// 更新论文记录，使用修正后的文件
					paper.FilePath = correctedPath
					paper.Status = "corrected"
					database.DB.Save(paper)
				}
			}
		}
	}

	return paper, nil
}

// GetPapersByUserID 获取用户的论文列表
func (s PaperService) GetPapersByUserID(userID uuid.UUID, page, pageSize int) ([]model.Paper, int64, error) {
	var papers []model.Paper
	var total int64

	offset := (page - 1) * pageSize
	database.DB.Where("user_id = ?", userID).Count(&total)
	database.DB.Where("user_id = ?", userID).Offset(offset).Limit(pageSize).Find(&papers)

	return papers, total, nil
}

// GetPaperByID 根据ID获取论文
func (s PaperService) GetPaperByID(userID, paperID uuid.UUID) (*model.Paper, error) {
	var paper model.Paper
	err := database.DB.Where("id = ? AND user_id = ?", paperID, userID).First(&paper).Error
	return &paper, err
}

// DeletePaper 删除论文
func (s PaperService) DeletePaper(userID, paperID uuid.UUID) error {
	return database.DB.Where("id = ? AND user_id = ?", paperID, userID).Delete(&model.Paper{}).Error
}

// GetPaperCheckResults 获取论文的检查结果列表
func (s PaperService) GetPaperCheckResults(userID, paperID uuid.UUID) ([]model.CheckResult, error) {
	var results []model.CheckResult
	err := database.DB.Where("paper_id = ? AND user_id = ?", paperID, userID).Find(&results).Error
	return results, err
}

// GetAllPapers 获取所有论文（管理员用）
func (s PaperService) GetAllPapers(page, pageSize int) ([]model.Paper, int64, error) {
	var papers []model.Paper
	var total int64

	offset := (page - 1) * pageSize
	database.DB.Model(&model.Paper{}).Count(&total)
	database.DB.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&papers)

	return papers, total, nil
}

// ExportCorrectedPaper 导出修正后的论文
func (s PaperService) ExportCorrectedPaper(userID, paperID uuid.UUID) (string, error) {
	paper, err := s.GetPaperByID(userID, paperID)
	if err != nil {
		return "", fmt.Errorf("failed to get paper: %v", err)
	}

	if paper.CorrectedFilePath != "" {
		if _, err := os.Stat(filepath.Clean(paper.CorrectedFilePath)); err == nil {
			return paper.CorrectedFilePath, nil
		}
	}

	ext := filepath.Ext(paper.FilePath)
	baseNoExt := strings.TrimSuffix(filepath.Base(paper.FilePath), ext)
	dir := filepath.Dir(paper.FilePath)

	standardPath := filepath.Join(dir, baseNoExt+"_corrected"+ext)
	if _, err := os.Stat(filepath.Clean(standardPath)); err == nil {
		paper.CorrectedFilePath = standardPath
		_ = database.DB.Save(paper).Error
		return standardPath, nil
	}

	pattern := filepath.Join(dir, baseNoExt+"_corrected_*"+ext)
	matches, _ := filepath.Glob(pattern)
	if len(matches) > 0 {
		latestPath := ""
		var latestTime time.Time
		for _, m := range matches {
			fi, err := os.Stat(filepath.Clean(m))
			if err != nil {
				continue
			}
			if latestPath == "" || fi.ModTime().After(latestTime) {
				latestPath = m
				latestTime = fi.ModTime()
			}
		}
		if latestPath != "" {
			paper.CorrectedFilePath = latestPath
			_ = database.DB.Save(paper).Error
			return latestPath, nil
		}
	}

	if paper.SelectedTemplateID != nil {
		var template model.FormatTemplate
		if err := database.DB.Where("id = ?", *paper.SelectedTemplateID).First(&template).Error; err == nil {
			var rulesMap map[string]interface{}
			if err := json.Unmarshal([]byte(template.FormatRules), &rulesMap); err != nil {
				var jsonString string
				if err2 := json.Unmarshal([]byte(template.FormatRules), &jsonString); err2 == nil {
					_ = json.Unmarshal([]byte(jsonString), &rulesMap)
				}
			}
			if rulesMap != nil {
				// 使用增强处理器进行格式修正
				log.Println("========================================")
				log.Println("🚀 使用增强处理器进行格式修正")
				log.Println("========================================")

				fp := fileprocessor.NewEnhancedProcessor()
				newFilePath, err := fp.ApplyCorrections(context.Background(), paper.FilePath, []map[string]interface{}{
					{"format_rules": rulesMap},
				})
				if err == nil && newFilePath != "" {
					paper.CorrectedFilePath = newFilePath
					paper.Status = "corrected"
					_ = database.DB.Save(paper).Error
					log.Printf("✅ Python服务格式修正成功: %s", newFilePath)
					return newFilePath, nil
				} else {
					log.Printf("❌ Python服务格式修正失败: %v", err)
					log.Println("提示: 请确保Python服务已启动 (python backend/python_service/src/server.py)")
					return "", fmt.Errorf("格式修正失败: %w", err)
				}
			}
		}
	}

	return "", fmt.Errorf("corrected file not found for paper %s", paperID)
}

// ComparePaperFormats 对比论文格式
func (s PaperService) ComparePaperFormats(userID, paperID, checkResultID uuid.UUID) (interface{}, error) {
	// TODO: 实现格式对比逻辑
	return nil, nil
}

// ExportCheckReport 导出检查报告
func (s PaperService) ExportCheckReport(userID, checkResultID uuid.UUID) (string, error) {
	// TODO: 实现导出报告逻辑
	return "", nil
}
