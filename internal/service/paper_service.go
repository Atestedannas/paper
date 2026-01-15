package service

import (
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
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
	// TODO: 实现格式检查逻辑
	result := &model.CheckResult{
		ID:      uuid.New(),
		PaperID: paperID,
		UserID:  userID,
		Status:  "pending",
	}
	return result, nil
}

// FixPaperFormatByParsedRequirements 根据解析的要求修复论文格式
func (s PaperService) FixPaperFormatByParsedRequirements(userID, paperID uuid.UUID, requirements map[string]interface{}) (interface{}, error) {
	// TODO: 实现格式修复逻辑
	return nil, nil
}

// FixPaperFormat 修复论文格式
func (s PaperService) FixPaperFormat(userID, paperID, checkResultID uuid.UUID) (interface{}, error) {
	// TODO: 实现格式修复逻辑
	return nil, nil
}

// UploadPaper 上传论文
func (s PaperService) UploadPaper(userID uuid.UUID, title, description string, formatStandardID uuid.UUID, file *multipart.FileHeader, fileType string, c *gin.Context) (*model.Paper, error) {
	// 处理匿名用户的情况
	var actualUserID uuid.UUID
	if userID == uuid.Nil {
		// 匿名用户，使用一个特殊的匿名用户ID
		actualUserID, _ = uuid.Parse("00000000-0000-0000-0000-000000000001")
	} else {
		actualUserID = userID
	}

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

	// 创建论文记录
	paper := &model.Paper{
		ID:                 uuid.New(),
		UserID:             actualUserID,
		Title:              title,
		Description:        description,
		FilePath:           filePath,
		FileName:           file.Filename,
		FileSize:           file.Size,
		FileType:           fileType,
		SelectedTemplateID: &formatStandardID,
		Status:             "uploaded",
	}

	// 保存到数据库
	if err := database.DB.Create(paper).Error; err != nil {
		return nil, fmt.Errorf("failed to save paper to database: %w", err)
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
	// TODO: 实现导出逻辑
	return "", nil
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
