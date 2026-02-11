package handler

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rsc.io/pdf"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nguyenthenguyen/docx"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
	"github.com/paper-format-checker/backend/pkg/formatchecker"
)

// PaperHandler 论文处理器
type PaperHandler struct {
	paperService            service.PaperService
	templateParserService   service.TemplateParserService
	formatComparisonService service.FormatComparisonService
	formatParserService     *service.FormatParserService
	settingService          service.SystemSettingService
	config                  *config.Config
}

// NewPaperHandler 创建论文处理器实例
func NewPaperHandler(config *config.Config) *PaperHandler {
	return &PaperHandler{
		paperService:            service.NewPaperService(config),
		templateParserService:   service.NewTemplateParserService(),
		formatComparisonService: service.NewFormatComparisonService(),
		formatParserService:     service.NewFormatParserService(),
		settingService:          service.GetSystemSettingService(),
		config:                  config,
	}
}

// UploadPaperRequest 上传论文请求结构体
type UploadPaperRequest struct {
	Title              string                 `form:"title" json:"title" binding:"omitempty,min=1,max=255"`
	Description        string                 `form:"description" json:"description" binding:"omitempty"`
	FormatStandardID   uuid.UUID              `form:"format_standard_id" json:"format_standard_id" binding:"omitempty"`
	TemplateID         int64                  `form:"template_id" json:"template_id" binding:"omitempty"`     // 前端传递的高校ID
	DocumentType       string                 `form:"document_type" json:"document_type" binding:"omitempty"` // 文档类型：本科论文、硕士论文
	Subject            string                 `form:"subject" json:"subject" binding:"omitempty"`             // 学科类别：文科、理科
	ParsedRequirements map[string]interface{} `form:"parsed_requirements" json:"parsed_requirements" binding:"omitempty"`
	Requirements       string                 `form:"requirements" json:"requirements" binding:"omitempty"`
}

// FormatCheckRequest 格式检查请求结构体
type FormatCheckRequest struct {
	PaperID          uuid.UUID `json:"paper_id" binding:"required"`
	FormatStandardID uuid.UUID `json:"format_standard_id" binding:"omitempty"`
}

// FormatFixRequest 格式修改请求结构体
type FormatFixRequest struct {
	PaperID     uuid.UUID `json:"paper_id" binding:"required"`
	CheckResult uuid.UUID `json:"check_result_id" binding:"required"`
	FixAll      bool      `json:"fix_all" binding:"omitempty"`
	IssueIDs    []string  `json:"issue_ids" binding:"omitempty"`
}

// UploadPaper 上传论文
func (h *PaperHandler) UploadPaper(c *gin.Context) {
	// 获取支付配置

	paymentConfig, err := h.settingService.GetPaymentConfig()
	if err != nil {
		utils.InternalServerError(c, fmt.Sprintf("获取支付配置失败: %v", err))
		return
	}

	// 检查是否需要付费
	isCheckFree, ok := paymentConfig["is_check_free"].(bool)
	if !ok {
		isCheckFree = true // 默认免费
	}

	// 如果需要付费，返回402 Payment Required状态码
	if !isCheckFree {
		// 获取付费金额
		formatCheckPrice, _ := paymentConfig["format_check"].(float64)
		formatFixPrice, _ := paymentConfig["format_fix"].(float64)

		paymentInfo := gin.H{
			"is_check_free":    isCheckFree,
			"format_check":     formatCheckPrice,
			"format_fix":       formatFixPrice,
			"total_amount":     formatCheckPrice + formatFixPrice,
			"payment_required": true,
		}
		paymentInfoJSON, _ := json.Marshal(paymentInfo)
		utils.ErrorResponse(c, http.StatusPaymentRequired, "论文格式检查和修正需要付费", string(paymentInfoJSON))
		return
	}

	// 验证和解析请求参数
	userID, req, file, fileType, err := h.validateUploadRequest(c)
	if err != nil {
		// 错误已经在validateUploadRequest中处理
		return
	}

	// 处理文件上传
	paper, err := h.performFileUpload(c, userID, req, file, fileType)
	if err != nil {
		// 错误已经在performFileUpload中处理
		return
	}

	// 执行后续处理（格式检查、修正等）
	response, err := h.handlePostUploadProcessing(c, userID, paper, req)
	if err != nil {
		// 错误已经在handlePostUploadProcessing中处理
		return
	}

	// 返回统一的响应
	utils.Created(c, response)
}

// validateUploadRequest 验证上传请求参数
func (h *PaperHandler) validateUploadRequest(c *gin.Context) (interface{}, UploadPaperRequest, *multipart.FileHeader, string, error) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取上传文件
	file, err := c.FormFile("file")
	if err != nil {
		utils.BadRequest(c, "file is required")
		return nil, UploadPaperRequest{}, nil, "", err
	}

	// 验证文件类型
	fileExt := strings.ToLower(filepath.Ext(file.Filename))
	fileType := ""
	switch fileExt {
	case ".pdf":
		fileType = "pdf"
	case ".docx":
		fileType = "docx"
	default:
		utils.BadRequest(c, "invalid file type, only pdf and docx are allowed")
		return nil, UploadPaperRequest{}, nil, "", fmt.Errorf("invalid file type")
	}

	// 验证文件大小
	if file.Size > h.config.File.MaxSize {
		utils.BadRequest(c, "file size exceeds limit")
		return nil, UploadPaperRequest{}, nil, "", fmt.Errorf("file size exceeds limit")
	}

	// 手动解析表单字段
	req := UploadPaperRequest{
		Title:        c.PostForm("title"),
		Description:  c.PostForm("description"),
		TemplateID:   parseInt64OrZero(c.PostForm("template_id")),
		DocumentType: c.PostForm("document_type"),
		Subject:      c.PostForm("subject"),
		Requirements: c.PostForm("requirements"),
	}

	// 如果提供了template_id，查找对应的格式标准ID
	if req.TemplateID != 0 {
		var template model.FormatTemplate
		if err := database.DB.Where("university_id = ? AND is_active = ? AND document_type = ?",
			req.TemplateID, true,
			func(dt string) string {
				if dt == "" {
					return "本科论文"
				}
				return dt
			}(req.DocumentType)).First(&template).Error; err == nil {
			req.FormatStandardID = template.ID
		}
	} else {
		// 尝试解析format_standard_id
		if formatStandardIDStr := c.PostForm("format_standard_id"); formatStandardIDStr != "" {
			if formatStandardID, err := uuid.Parse(formatStandardIDStr); err == nil {
				req.FormatStandardID = formatStandardID
			}
		}
	}

	return userID, req, file, fileType, nil
}

// 辅助函数：安全地解析int64
func parseInt64OrZero(s string) int64 {
	if s == "" {
		return 0
	}
	if val, err := strconv.ParseInt(s, 10, 64); err == nil {
		return val
	}
	return 0
}

// performFileUpload 执行文件上传操作
func (h *PaperHandler) performFileUpload(c *gin.Context, userID interface{}, req UploadPaperRequest, file *multipart.FileHeader, fileType string) (*model.Paper, error) {
	// 处理文件上传
	paper, err := h.paperService.UploadPaper(
		userID.(uuid.UUID),
		req.Title,
		req.Description,
		req.FormatStandardID,
		file,
		fileType,
		c, // 传递context用于保存文件
	)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return nil, err
	}

	return paper, nil
}

// handlePostUploadProcessing 处理上传后的后续操作
func (h *PaperHandler) handlePostUploadProcessing(c *gin.Context, userID interface{}, paper *model.Paper, req UploadPaperRequest) (gin.H, error) {

	var checkResult *model.CheckResult
	var err error

	// 优先使用用户提供的FormatStandardID，如果没有则根据TemplateID查找
	if req.TemplateID != 0 {
		// 如果前端传递了高校ID但没有FormatStandardID，尝试根据高校ID查找对应的模板
		var template model.FormatTemplate

		query := database.DB.Where("university_id = ? AND is_active = ?", req.TemplateID, true)

		// 如果指定了文档类型，增加过滤条件
		if req.DocumentType != "" {
			query = query.Where("document_type = ?", req.DocumentType)
		} else {
			// 默认查找本科论文
			query = query.Where("document_type = ?", "本科论文")
		}

		// 如果指定了学科类别，增加过滤条件
		if req.Subject != "" {
			query = query.Where("subject = ?", req.Subject)
		}

		// 查找该高校下的符合条件的模板
		if err := query.First(&template).Error; err == nil {
			req.FormatStandardID = template.ID
			// 更新paper记录中的SelectedTemplateID
			paper.SelectedTemplateID = &template.ID
			database.DB.Model(paper).Update("selected_template_id", template.ID)
		} else {
			// 如果带具体条件的没找到，尝试只按高校ID找一个兜底
			if err := database.DB.Where("university_id = ? AND is_active = ?", req.TemplateID, true).First(&template).Error; err == nil {
				req.FormatStandardID = template.ID
				paper.SelectedTemplateID = &template.ID
				database.DB.Model(paper).Update("selected_template_id", template.ID)
			} else {
				fmt.Printf("Warning: Could not find active template for university ID %d: %v\n", req.TemplateID, err)
			}
		}
	}

	// 根据是否有格式要求决定是否执行格式检查
	if req.FormatStandardID != uuid.Nil {

		// 使用标准模板进行格式检查
		checkResult, err = h.paperService.CheckPaperFormat(userID.(uuid.UUID), paper.ID, req.FormatStandardID)
		if err != nil {
			// 格式检查失败，但仍返回上传成功的响应
			response := gin.H{
				"paper":   paper,
				"message": "paper uploaded successfully, but format check failed",
				"error":   fmt.Sprintf("failed to perform automatic format check: %v", err),
			}
			return response, nil
		}
	} else if req.ParsedRequirements != nil && len(req.ParsedRequirements) > 0 {
		// 如果没有FormatStandardID但有ParsedRequirements，则使用解析的格式要求
		fixResult, err := h.paperService.FixPaperFormatByParsedRequirements(userID.(uuid.UUID), paper.ID, req.ParsedRequirements)
		if err != nil {
			fmt.Printf("Fix by parsed requirements failed: %v\n", err)
		}
		// 返回修正结果，但不执行格式检查
		response := gin.H{
			"paper":   paper,
			"message": "paper uploaded and format fix completed successfully",
		}
		if result, ok := fixResult.(map[string]interface{}); ok {
			if path, exists := result["corrected_file_path"]; exists {
				response["corrected_file_path"] = path
				response["download_url"] = fmt.Sprintf("/api/v1/papers/%s/corrected-file", paper.ID.String())
			}
		}
		return response, nil
	}

	// 构建包含上传信息和格式检查结果的综合响应
	response := gin.H{
		"paper":        paper,
		"check_result": checkResult,
		"message":      "paper uploaded and format check completed successfully",
	}

	if checkResult != nil {
		response["comparison_url"] = fmt.Sprintf("/api/v1/papers/%s/comparison/%s", paper.ID.String(), checkResult.ID.String())
	} else {
		response["comparison_url"] = ""
	}

	// 自动修正并生成下载链接（只有在有检查结果且尚未修复时才进行修正）
	if checkResult != nil {
		// 检查论文是否已经被格式修复（状态为 "corrected" 表示已在上传时完成修复）
		if paper.Status != "corrected" {
			// 执行自动修正
			fixResult, err := h.paperService.FixPaperFormat(userID.(uuid.UUID), paper.ID, checkResult.ID)
			if err == nil {
				// 如果修正成功，添加修正结果和下载链接
				response["fix_result"] = fixResult
				response["download_url"] = fmt.Sprintf("/api/v1/papers/%s/corrected-file", paper.ID.String())

				// 尝试获取修正后的文件路径
				if result, ok := fixResult.(*service.CorrectionResult); ok && result != nil {
					response["corrected_file_path"] = result.CorrectedFilePath
				} else if resultMap, ok := fixResult.(map[string]interface{}); ok {
					if path, exists := resultMap["corrected_file_path"]; exists {
						response["corrected_file_path"] = path
					}
				}
			} else {
				// 记录修正错误
				fmt.Printf("Auto fix failed: %v\n", err)
				response["fix_error"] = err.Error()
			}

			// 生成格式差异报告
			diffReport, err := h.formatComparisonService.GenerateFormatDifferences(checkResult.ID)
			if err == nil {
				response["format_comparison"] = diffReport
			}
		} else {
			// 论文状态已经是 "corrected"，说明已在上传时完成修复，跳过重复修复
			fmt.Printf("Paper %s already corrected during upload, skipping duplicate fix\n", paper.ID.String())
			// 仍然生成格式差异报告
			diffReport, err := h.formatComparisonService.GenerateFormatDifferences(checkResult.ID)
			if err == nil {
				response["format_comparison"] = diffReport
			}
		}
	}

	return response, nil
}

// GetPapers 获取用户的论文列表
func (h *PaperHandler) GetPapers(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	// 获取论文列表
	papers, total, err := h.paperService.GetPapersByUserID(userID.(uuid.UUID), page, pageSize)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, gin.H{
		"papers":    papers,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetPaper 获取论文详情
func (h *PaperHandler) GetPaper(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 获取论文详情
	paper, err := h.paperService.GetPaperByID(userID.(uuid.UUID), paperID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, paper)
}

// DeletePaper 删除论文
func (h *PaperHandler) DeletePaper(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 删除论文
	if err := h.paperService.DeletePaper(paperID, userID.(uuid.UUID)); err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, gin.H{"message": "paper deleted successfully"})
}

// CheckFormat 检查论文格式
func (h *PaperHandler) CheckFormat(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 解析请求参数
	var req FormatCheckRequest
	// 设置默认值
	req.PaperID = paperID
	if err := c.ShouldBind(&req); err != nil {
		// 如果没有请求体，使用URL中的ID
		req.PaperID = paperID
	}

	// 如果请求中没有指定论文ID，使用URL中的ID
	if req.PaperID == uuid.Nil {
		req.PaperID = paperID
	}

	// 检查论文格式
	checkResult, err := h.paperService.CheckPaperFormat(userID.(uuid.UUID), req.PaperID, req.FormatStandardID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, checkResult)
}

// FixFormat 修复论文格式
func (h *PaperHandler) FixFormat(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 解析请求参数
	var req FormatFixRequest
	// 设置默认值
	req.PaperID = paperID
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 如果请求中没有指定论文ID，使用URL中的ID
	if req.PaperID == uuid.Nil {
		req.PaperID = paperID
	}

	// 修复论文格式
	_, err = h.paperService.FixPaperFormat(userID.(uuid.UUID), req.PaperID, req.CheckResult)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, gin.H{"message": "格式修复成功"})
}

// GetFormatStandards 获取格式标准列表，支持按机构和文档类型过滤
func (h *PaperHandler) GetFormatStandards(c *gin.Context) {
	// 从查询参数中获取过滤条件

	// 返回响应
	utils.Success(c, gin.H{})
}

// GetPaperCheckResults 获取论文的检查结果列表
func (h *PaperHandler) GetPaperCheckResults(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 获取检查结果列表
	results, err := h.paperService.GetPaperCheckResults(userID.(uuid.UUID), paperID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, results)
}

// GetPaperFile 获取论文文件
func (h *PaperHandler) GetPaperFile(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 获取论文信息
	paper, err := h.paperService.GetPaperByID(userID.(uuid.UUID), paperID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 设置下载响应头
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(paper.FilePath)))

	// 规范化路径，处理 \ 和 / 的问题
	cleanPath := filepath.Clean(paper.FilePath)
	// 获取绝对路径日志，方便排查
	absPath, _ := filepath.Abs(cleanPath)
	fmt.Printf("Download Paper: ID=%s, Path=%s, AbsPath=%s\n", paperID, cleanPath, absPath)

	// 检查文件是否存在
	if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
		utils.ErrorResponse(c, http.StatusNotFound, "文件在服务器上不存在", fmt.Sprintf("Path: %s, Error: %v", cleanPath, err))
		return
	}

	// 根据文件类型设置正确的Content-Type
	fileExt := strings.ToLower(filepath.Ext(cleanPath))
	switch fileExt {
	case ".docx":
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	case ".pdf":
		c.Header("Content-Type", "application/pdf")
	default:
		c.Header("Content-Type", "application/octet-stream")
	}

	// 返回文件
	c.File(cleanPath)
}

// GetCorrectedPaperFile 获取修正后的论文文件
func (h *PaperHandler) GetCorrectedPaperFile(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 获取修正后的文件路径
	filePath, err := h.paperService.ExportCorrectedPaper(userID.(uuid.UUID), paperID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 检查文件是否存在
	cleanPath := filepath.Clean(filePath)
	absPath, _ := filepath.Abs(cleanPath)
	fmt.Printf("Download Corrected Paper: ID=%s, Path=%s, AbsPath=%s\n", paperID, cleanPath, absPath)

	if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
		utils.ErrorResponse(c, http.StatusNotFound, "修正后的文件在服务器上不存在", fmt.Sprintf("Path: %s, Error: %v", cleanPath, err))
		return
	}

	// 设置下载响应头
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(cleanPath)))

	// 根据文件类型设置正确的Content-Type
	fileExt := strings.ToLower(filepath.Ext(cleanPath))
	switch fileExt {
	case ".docx":
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	case ".pdf":
		c.Header("Content-Type", "application/pdf")
	default:
		c.Header("Content-Type", "application/octet-stream")
	}

	// 返回文件
	c.File(cleanPath)
}

// ComparePaperFormats 对比论文格式
func (h *PaperHandler) ComparePaperFormats(c *gin.Context) {
	// 获取检查结果ID
	checkResultID, err := uuid.Parse(c.Param("check_result_id"))
	if err != nil {
		utils.BadRequest(c, "invalid check result id")
		return
	}

	// 对比论文格式
	comparison, err := h.formatComparisonService.GenerateFormatDifferences(checkResultID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, comparison)
}

// ExportCorrectedPaper 导出修正后的论文
func (h *PaperHandler) ExportCorrectedPaper(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 导出修正后的论文
	filePath, err := h.paperService.ExportCorrectedPaper(userID.(uuid.UUID), paperID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回文件
	c.File(filePath)
}

// ExportCheckReport 导出检查报告
func (h *PaperHandler) ExportCheckReport(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	_, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 获取检查结果ID
	checkResultID, err := uuid.Parse(c.Param("check_result_id"))
	if err != nil {
		utils.BadRequest(c, "invalid check result id")
		return
	}

	// 导出检查报告
	report, err := h.paperService.ExportCheckReport(userID.(uuid.UUID), checkResultID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, report)
}

// DownloadCheckReport 下载检查报告
func (h *PaperHandler) DownloadCheckReport(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	_, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 获取检查结果ID
	checkResultID, err := uuid.Parse(c.Param("check_result_id"))
	if err != nil {
		utils.BadRequest(c, "invalid check result id")
		return
	}

	// 导出检查报告
	report, err := h.paperService.ExportCheckReport(userID.(uuid.UUID), checkResultID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 设置响应头
	c.Header("Content-Disposition", "attachment; filename=check_report.txt")
	c.Header("Content-Type", "text/plain; charset=utf-8")

	// 返回报告内容
	c.String(200, report)
}

// ParseFormatRequirementsRequest 解析格式要求请求结构体
type ParseFormatRequirementsRequest struct {
	FormatText string `json:"format_text" binding:"required"`
}

// DeepSeek客户端配置
type DeepSeekClient struct {
	baseURL    string
	httpClient *http.Client
	cookies    []*http.Cookie
}

// 创建新的DeepSeek客户端
func NewDeepSeekClient() *DeepSeekClient {
	return &DeepSeekClient{
		baseURL: "https://chat.deepseek.com",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// 调用DeepSeek API提取论文格式要求
func (d *DeepSeekClient) ExtractFormatRequirements(text string) (string, error) {
	// 构建请求体
	//todo  ai
	return "", nil
}

// ParseFormatRequirements 解析论文格式要求，集成DeepSeek API
func (h *PaperHandler) ParseFormatRequirements(c *gin.Context) {
	// 解析请求数据
	var req ParseFormatRequirementsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	// 初始化解析结果
	var parsedFormat *ParsedFormatRequirements
	var parseErr error
	if parsedFormat == nil {
		parsedFormat = h.parseDetailedFormatText(req.FormatText)
	}

	// 将解析结果转换为汉字键值对结构
	chineseFormat := h.convertToChineseFormat(parsedFormat)

	// 将汉字键值对结构转换为JSON字符串，以便保存到数据库
	settingsJSON, err := json.Marshal(chineseFormat)
	if err != nil {
		utils.InternalServerError(c, fmt.Sprintf("failed to marshal format settings: %v", err))
		return
	}

	// 创建格式模板记录
	// 确保TemplateID是唯一的
	// 注意：错误提示 invalid input syntax for type uuid 表明某个期望 UUID 的字段接收到了非 UUID 格式的字符串
	// 检查 ParseFormatRequirements 函数中 TemplateID 的生成方式
	// 可能是 CreateFormatStandard 中的 TemplateID 或者是 ParseFormatRequirements 中的 TemplateID
	// 这里是 ParseFormatRequirements 函数

	newTemplateID := uuid.New().String()

	formatTemplate := model.FormatTemplate{
		TemplateID:   newTemplateID, // 使用标准UUID字符串，不带前缀
		Name:         fmt.Sprintf("%s格式标准", parsedFormat.Institution),
		DocumentType: "本科论文",
		Source:       "auto_parsed",
		Version:      "1.0",
		IsPublic:     false,
		IsActive:     true,
		FormatRules:  string(settingsJSON),
		Description:  fmt.Sprintf("从文本解析生成的格式标准: %s", parsedFormat.Institution),
	}

	// 保存到数据库
	if err := database.DB.Create(&formatTemplate).Error; err != nil {
		utils.InternalServerError(c, fmt.Sprintf("failed to create format template: %v", err))
		return
	}

	// 返回响应，使用汉字键值对结构
	response := map[string]interface{}{
		"id":                  formatTemplate.ID,
		"name":                formatTemplate.Name,
		"description":         formatTemplate.Description,
		"is_public":           formatTemplate.IsPublic,
		"format_requirements": chineseFormat,
		"parse_warning":       parseErr,
	}

	utils.Created(c, response)
}

// convertToChineseFormat 将解析后的格式要求转换为汉字键值对结构
func (h *PaperHandler) convertToChineseFormat(parsedFormat *ParsedFormatRequirements) map[string]interface{} {
	chineseFormat := make(map[string]interface{})

	// 基本信息
	chineseFormat["学校名称"] = parsedFormat.Institution
	chineseFormat["文档类型"] = parsedFormat.DocumentType

	// 基本要求
	if len(parsedFormat.BasicRequirements) > 0 {
		chineseFormat["基本要求"] = parsedFormat.BasicRequirements
	}

	// 页面设置
	pageSetup := make(map[string]interface{})
	pageSetup["纸张大小"] = parsedFormat.PageSetup.PaperSize
	pageSetup["页面方向"] = parsedFormat.PageSetup.Orientation

	// 页边距
	margins := make(map[string]interface{})
	margins["上边距"] = parsedFormat.PageSetup.Margins.Top
	margins["下边距"] = parsedFormat.PageSetup.Margins.Bottom
	margins["左边距"] = parsedFormat.PageSetup.Margins.Left
	margins["右边距"] = parsedFormat.PageSetup.Margins.Right
	pageSetup["页边距"] = margins

	// 页眉页脚
	headerFooter := make(map[string]interface{})
	headerFooter["页眉高度"] = parsedFormat.PageSetup.HeaderFooter.HeaderHeight
	headerFooter["页脚高度"] = parsedFormat.PageSetup.HeaderFooter.FooterHeight
	headerFooter["页眉左侧内容"] = parsedFormat.PageSetup.HeaderFooter.HeaderLeft
	headerFooter["页眉右侧内容"] = parsedFormat.PageSetup.HeaderFooter.HeaderRight
	headerFooter["页眉居中内容"] = parsedFormat.PageSetup.HeaderFooter.HeaderCenter
	pageSetup["页眉页脚"] = headerFooter

	pageSetup["打印方式"] = parsedFormat.PageSetup.PrintingSide
	chineseFormat["页面设置"] = pageSetup

	// 字体设置
	fontSettings := make(map[string]interface{})

	// 正文字体
	mainFont := make(map[string]interface{})
	mainFont["字体名称"] = parsedFormat.FontSettings.MainFont.FontName
	mainFont["字体大小"] = parsedFormat.FontSettings.MainFont.FontSize
	mainFont["行间距"] = parsedFormat.FontSettings.MainFont.LineSpacing
	fontSettings["正文字体"] = mainFont

	// 标题字体
	titleFont := make(map[string]interface{})

	// 章标题
	chapterTitle := make(map[string]interface{})
	chapterTitle["字体名称"] = parsedFormat.FontSettings.TitleFont.ChapterTitle.FontName
	chapterTitle["字体大小"] = parsedFormat.FontSettings.TitleFont.ChapterTitle.FontSize
	chapterTitle["对齐方式"] = parsedFormat.FontSettings.TitleFont.ChapterTitle.Alignment
	titleFont["章标题"] = chapterTitle

	// 节标题
	sectionTitle := make(map[string]interface{})
	sectionTitle["字体名称"] = parsedFormat.FontSettings.TitleFont.SectionTitle.FontName
	sectionTitle["字体大小"] = parsedFormat.FontSettings.TitleFont.SectionTitle.FontSize
	sectionTitle["对齐方式"] = parsedFormat.FontSettings.TitleFont.SectionTitle.Alignment
	titleFont["节标题"] = sectionTitle

	// 小节标题
	subsectionTitle := make(map[string]interface{})
	subsectionTitle["字体名称"] = parsedFormat.FontSettings.TitleFont.SubsectionTitle.FontName
	subsectionTitle["字体大小"] = parsedFormat.FontSettings.TitleFont.SubsectionTitle.FontSize
	subsectionTitle["对齐方式"] = parsedFormat.FontSettings.TitleFont.SubsectionTitle.Alignment
	titleFont["小节标题"] = subsectionTitle

	fontSettings["标题字体"] = titleFont

	// 其他字体设置
	abstractFont := make(map[string]interface{})
	abstractFont["字体名称"] = parsedFormat.FontSettings.AbstractFont.FontName
	abstractFont["字体大小"] = parsedFormat.FontSettings.AbstractFont.FontSize
	fontSettings["摘要字体"] = abstractFont

	directoryFont := make(map[string]interface{})
	directoryFont["字体名称"] = parsedFormat.FontSettings.DirectoryFont.FontName
	directoryFont["字体大小"] = parsedFormat.FontSettings.DirectoryFont.FontSize
	fontSettings["目录字体"] = directoryFont

	tableFont := make(map[string]interface{})
	tableFont["字体名称"] = parsedFormat.FontSettings.TableFont.FontName
	tableFont["字体大小"] = parsedFormat.FontSettings.TableFont.FontSize
	fontSettings["表格字体"] = tableFont

	figureFont := make(map[string]interface{})
	figureFont["字体名称"] = parsedFormat.FontSettings.FigureFont.FontName
	figureFont["字体大小"] = parsedFormat.FontSettings.FigureFont.FontSize
	fontSettings["图片字体"] = figureFont

	chineseFormat["字体设置"] = fontSettings

	// 文档结构
	structure := make(map[string]interface{})

	// 前置部分
	frontMatter := make(map[string]interface{})
	frontMatter["封面"] = parsedFormat.Structure.FrontMatter.CoverPage
	frontMatter["版权声明"] = parsedFormat.Structure.FrontMatter.CopyrightStatement
	frontMatter["摘要"] = parsedFormat.Structure.FrontMatter.Abstract
	frontMatter["目录"] = parsedFormat.Structure.FrontMatter.TableOfContents
	frontMatter["插图清单"] = parsedFormat.Structure.FrontMatter.ListOfFigures
	frontMatter["表格清单"] = parsedFormat.Structure.FrontMatter.ListOfTables
	structure["前置部分"] = frontMatter

	// 主体部分
	mainBody := make(map[string]interface{})
	mainBody["引言"] = parsedFormat.Structure.MainBody.Introduction
	mainBody["正文"] = parsedFormat.Structure.MainBody.MainContent
	mainBody["结论"] = parsedFormat.Structure.MainBody.Conclusion
	structure["主体部分"] = mainBody

	// 后置部分
	backMatter := make(map[string]interface{})
	backMatter["参考文献"] = parsedFormat.Structure.BackMatter.References
	backMatter["致谢"] = parsedFormat.Structure.BackMatter.Acknowledgements
	backMatter["附录"] = parsedFormat.Structure.BackMatter.Appendices
	structure["后置部分"] = backMatter

	chineseFormat["文档结构"] = structure

	// 引用规则
	citationRules := make(map[string]interface{})
	citationRules["参考文献格式"] = parsedFormat.CitationRules.ReferenceFormat
	if len(parsedFormat.CitationRules.ReferenceTypes) > 0 {
		citationRules["参考文献类型"] = parsedFormat.CitationRules.ReferenceTypes
	}
	chineseFormat["引用规则"] = citationRules

	// 附录规则
	appendixRules := make(map[string]interface{})
	appendixRules["附录格式"] = parsedFormat.AppendixRules.AppendixFormat
	if len(parsedFormat.AppendixRules.AttachmentList) > 0 {
		appendixRules["附件列表"] = parsedFormat.AppendixRules.AttachmentList
	}
	chineseFormat["附录规则"] = appendixRules

	return chineseFormat
}

// ParsedFormatRequirements 解析后的格式要求
type ParsedFormatRequirements struct {
	Institution       string            `json:"institution"`        // 学校名称
	DocumentType      string            `json:"document_type"`      // 文档类型
	BasicRequirements []string          `json:"basic_requirements"` // 基本要求
	PageSetup         PageSetup         `json:"page_setup"`         // 页面设置
	FontSettings      FontSettings      `json:"font_settings"`      // 字体设置
	Structure         DocumentStructure `json:"structure"`          // 文档结构
	CitationRules     CitationRules     `json:"citation_rules"`     // 引用规则
	AppendixRules     AppendixRules     `json:"appendix_rules"`     // 附录规则
}

// PageSetup 页面设置
type PageSetup struct {
	PaperSize    string       `json:"paper_size"`    // 纸张大小
	Orientation  string       `json:"orientation"`   // 页面方向
	Margins      Margins      `json:"margins"`       // 页边距
	HeaderFooter HeaderFooter `json:"header_footer"` // 页眉页脚
	PrintingSide string       `json:"printing_side"` // 打印面
}

// Margins 页边距
type Margins struct {
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
}

// HeaderFooter 页眉页脚
type HeaderFooter struct {
	HeaderHeight float64 `json:"header_height"` // 页眉高度
	FooterHeight float64 `json:"footer_height"` // 页脚高度
	HeaderLeft   string  `json:"header_left"`   // 页眉左侧内容
	HeaderRight  string  `json:"header_right"`  // 页眉右侧内容
	HeaderCenter string  `json:"header_center"` // 页眉居中内容
}

// FontSettings 字体设置
type FontSettings struct {
	MainFont      MainFont      `json:"main_font"`      // 正文字体
	TitleFont     TitleFont     `json:"title_font"`     // 标题字体
	AbstractFont  AbstractFont  `json:"abstract_font"`  // 摘要字体
	DirectoryFont DirectoryFont `json:"directory_font"` // 目录字体
	TableFont     TableFont     `json:"table_font"`     // 表格字体
	FigureFont    FigureFont    `json:"figure_font"`    // 图片字体
}

// MainFont 正文字体
type MainFont struct {
	FontName    string  `json:"font_name"`
	FontSize    float64 `json:"font_size"`
	LineSpacing float64 `json:"line_spacing"`
}

// TitleFont 标题字体
type TitleFont struct {
	ChapterTitle    ChapterTitle    `json:"chapter_title"`    // 章标题
	SectionTitle    SectionTitle    `json:"section_title"`    // 节标题
	SubsectionTitle SubsectionTitle `json:"subsection_title"` // 小节标题
}

// ChapterTitle 章标题
type ChapterTitle struct {
	FontName  string  `json:"font_name"`
	FontSize  float64 `json:"font_size"`
	Alignment string  `json:"alignment"`
}

// SectionTitle 节标题
type SectionTitle struct {
	FontName  string  `json:"font_name"`
	FontSize  float64 `json:"font_size"`
	Alignment string  `json:"alignment"`
}

// SubsectionTitle 小节标题
type SubsectionTitle struct {
	FontName  string  `json:"font_name"`
	FontSize  float64 `json:"font_size"`
	Alignment string  `json:"alignment"`
}

// AbstractFont 摘要字体
type AbstractFont struct {
	FontName string  `json:"font_name"`
	FontSize float64 `json:"font_size"`
}

// DirectoryFont 目录字体
type DirectoryFont struct {
	FontName string  `json:"font_name"`
	FontSize float64 `json:"font_size"`
}

// TableFont 表格字体
type TableFont struct {
	FontName string  `json:"font_name"`
	FontSize float64 `json:"font_size"`
}

// FigureFont 图片字体
type FigureFont struct {
	FontName string  `json:"font_name"`
	FontSize float64 `json:"font_size"`
}

// DocumentStructure 文档结构
type DocumentStructure struct {
	FrontMatter FrontMatter `json:"front_matter"` // 前置部分
	MainBody    MainBody    `json:"main_body"`    // 主体部分
	BackMatter  BackMatter  `json:"back_matter"`  // 后置部分
}

// FrontMatter 前置部分
type FrontMatter struct {
	CoverPage          bool `json:"cover_page"`          // 封面
	CopyrightStatement bool `json:"copyright_statement"` // 版权声明
	Abstract           bool `json:"abstract"`            // 摘要
	TableOfContents    bool `json:"table_of_contents"`   // 目录
	ListOfFigures      bool `json:"list_of_figures"`     // 插图清单
	ListOfTables       bool `json:"list_of_tables"`      // 表格清单
}

// MainBody 主体部分
type MainBody struct {
	Introduction bool `json:"introduction"` // 引言
	MainContent  bool `json:"main_content"` // 正文
	Conclusion   bool `json:"conclusion"`   // 结论
}

// BackMatter 后置部分
type BackMatter struct {
	References       bool `json:"references"`       // 参考文献
	Acknowledgements bool `json:"acknowledgements"` // 致谢
	Appendices       bool `json:"appendices"`       // 附录
}

// CitationRules 引用规则
type CitationRules struct {
	ReferenceFormat string   `json:"reference_format"` // 参考文献格式
	ReferenceTypes  []string `json:"reference_types"`  // 参考文献类型
}

// AppendixRules 附录规则
type AppendixRules struct {
	AppendixFormat string   `json:"appendix_format"` // 附录格式
	AttachmentList []string `json:"attachment_list"` // 附件列表
}

// isSpecialFormat 检查是否是特殊格式
func (h *PaperHandler) isSpecialFormat(text string) bool {
	// 检查是否包含特定的关键字
	keywords := []string{"中文摘要", "英文摘要", "关键词", "目录", "主体部分", "标题序号与格式"}
	matchCount := 0
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			matchCount++
		}
	}
	// 如果匹配到超过一半的关键字，则认为是特殊格式
	return matchCount >= len(keywords)/2
}

// parseSpecialFormat 解析特殊格式
func (h *PaperHandler) parseSpecialFormat(text string, format *ParsedFormatRequirements) {
	// 解析摘要部分
	h.parseAbstractSection(text, format)

	// 解析目录部分
	h.parseDirectorySection(text, format)

	// 解析主体部分
	h.parseMainBodySection(text, format)

	// 解析标题格式
	h.parseTitleFormat(text, format)

	// 解析字体要求
	h.parseFontRequirements(text, format)
}

// parseAbstractSection 解析摘要部分
func (h *PaperHandler) parseAbstractSection(text string, format *ParsedFormatRequirements) {
	// 查找中文摘要部分
	cnAbstractRegex := regexp.MustCompile(`中文摘要[：:]([^。]+)`)

	cnMatches := cnAbstractRegex.FindStringSubmatch(text)

	if len(cnMatches) > 1 {
		// 提取摘要要求

		abstractReq := cnMatches[1]
		if strings.Contains(abstractReq, "300汉字") {
			format.BasicRequirements = append(format.BasicRequirements, "中文摘要约300汉字")
		}
		if strings.Contains(abstractReq, "第三人称") {
			format.BasicRequirements = append(format.BasicRequirements, "中文摘要以第三人称陈述")
		}
		if strings.Contains(abstractReq, "目的") && strings.Contains(abstractReq, "方法") &&
			strings.Contains(abstractReq, "结果") && strings.Contains(abstractReq, "结论") {
			format.BasicRequirements = append(format.BasicRequirements, "中文摘要包含目的、方法、结果、结论")
		}
		if strings.Contains(abstractReq, "重点在结果和结论") {
			format.BasicRequirements = append(format.BasicRequirements, "中文摘要重点在结果和结论")
		}
	}

	// 查找英文摘要部分
	if strings.Contains(text, "英文摘要") {
		format.BasicRequirements = append(format.BasicRequirements, "包含英文摘要")
	}

	// 查找关键词部分
	keywordRegex := regexp.MustCompile(`关键词[：:]([^。]+)`)
	keywordMatches := keywordRegex.FindStringSubmatch(text)
	if len(keywordMatches) > 1 {
		keywordsReq := keywordMatches[1]
		if strings.Contains(keywordsReq, "3-5个") {
			format.BasicRequirements = append(format.BasicRequirements, "关键词3-5个")
		}
		if strings.Contains(keywordsReq, "左对齐") {
			format.BasicRequirements = append(format.BasicRequirements, "关键词左对齐")
		}
		if strings.Contains(keywordsReq, "中英文关键词间用分号分隔") {
			format.BasicRequirements = append(format.BasicRequirements, "中英文关键词间用分号分隔")
		}
	}

}

// parseDirectorySection 解析目录部分
func (h *PaperHandler) parseDirectorySection(text string, format *ParsedFormatRequirements) {
	if strings.Contains(text, "目录") && strings.Contains(text, "另起一页") {
		format.Structure.FrontMatter.TableOfContents = true
		format.BasicRequirements = append(format.BasicRequirements, "目录另起一页，排在摘要之后")
	}

	if strings.Contains(text, "包含章、节、条、附录的序号、名称和页码") {
		format.BasicRequirements = append(format.BasicRequirements, "目录包含章、节、条、附录的序号、名称和页码")
	}

	if strings.Contains(text, "目录层次为2-4级") {
		format.BasicRequirements = append(format.BasicRequirements, "目录层次为2-4级")
	}

	if strings.Contains(text, "下级依次右缩进两个字符") {
		format.BasicRequirements = append(format.BasicRequirements, "下级目录依次右缩进两个字符")
	}

	if strings.Contains(text, "小四号宋体") {
		format.FontSettings.DirectoryFont.FontName = "宋体"
		format.FontSettings.DirectoryFont.FontSize = 12.0 // 小四号
	}
}

// parseMainBodySection 解析主体部分
func (h *PaperHandler) parseMainBodySection(text string, format *ParsedFormatRequirements) {
	if strings.Contains(text, "主体部分") && strings.Contains(text, "必须从右页") {
		format.BasicRequirements = append(format.BasicRequirements, "主体部分必须从右页（奇数页）开始")
	}

	if strings.Contains(text, "一级标题（章）之间应换页") {
		format.BasicRequirements = append(format.BasicRequirements, "一级标题（章）之间应换页")
	}

	// 标记主体部分内容结构
	format.Structure.MainBody.Introduction = true
	format.Structure.MainBody.MainContent = true
	format.Structure.MainBody.Conclusion = true
}

// parseTitleFormat 解析标题格式
func (h *PaperHandler) parseTitleFormat(text string, format *ParsedFormatRequirements) {
	if strings.Contains(text, "标题序号与格式") {
		format.BasicRequirements = append(format.BasicRequirements, "遵循标题序号与格式要求")
	}

	// 理工类格式
	if strings.Contains(text, "理工类") {
		format.BasicRequirements = append(format.BasicRequirements, "使用理工类标题序号格式: 1 → 1.1 → 1.1.1 → ① → 1） → a．")
	}

	// 文科类格式
	if strings.Contains(text, "文科类") {
		format.BasicRequirements = append(format.BasicRequirements, "使用文科类标题序号格式: 一 → (一） → 1. → (1) → 第一")
	}
}

// parseFontRequirements 解析字体要求
func (h *PaperHandler) parseFontRequirements(text string, format *ParsedFormatRequirements) {
	// 一级标题（章）
	if strings.Contains(text, "一级标题（章）") && strings.Contains(text, "三号黑体") && strings.Contains(text, "居中") {
		format.FontSettings.TitleFont.ChapterTitle.FontName = "黑体"
		format.FontSettings.TitleFont.ChapterTitle.FontSize = 16.0 // 三号
		format.FontSettings.TitleFont.ChapterTitle.Alignment = "center"
	}

	// 二级标题（节）
	if strings.Contains(text, "二级标题（节）") && strings.Contains(text, "小三号黑体") && strings.Contains(text, "居左") {
		format.FontSettings.TitleFont.SectionTitle.FontName = "黑体"
		format.FontSettings.TitleFont.SectionTitle.FontSize = 15.0 // 小三号
		format.FontSettings.TitleFont.SectionTitle.Alignment = "left"
	}

	// 三级标题（条）
	if strings.Contains(text, "三级标题（条）") && strings.Contains(text, "四号黑体") && strings.Contains(text, "居左") && strings.Contains(text, "右缩进两字") {
		format.FontSettings.TitleFont.SubsectionTitle.FontName = "黑体"
		format.FontSettings.TitleFont.SubsectionTitle.FontSize = 14.0 // 四号
		format.FontSettings.TitleFont.SubsectionTitle.Alignment = "left"
	}

	// 更下级标题
	if strings.Contains(text, "更下级标题") && strings.Contains(text, "与正文同大小宋体") && strings.Contains(text, "右缩进两字") {
		format.BasicRequirements = append(format.BasicRequirements, "更下级标题与正文同大小宋体，右缩进两字")
	}
}

// cleanText 清理文本，去除多余空格和换行符
func (h *PaperHandler) cleanText(text string) string {
	// 替换多个连续的空白字符为单个空格
	re := regexp.MustCompile(`\s+`)
	cleaned := re.ReplaceAllString(text, " ")
	return strings.TrimSpace(cleaned)
}

// extractInstitution 提取学校名称
func (h *PaperHandler) extractInstitution(text string, format *ParsedFormatRequirements) {
	// 使用更智能的方式匹配学校名称，结合分词结果
	institutionRegex := regexp.MustCompile(`([\x{4e00}-\x{9fa5}]+大学|[^\s]+学院)`)
	matches := institutionRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		format.Institution = matches[1]
	} else {
		// 如果正则表达式没有匹配到，尝试从分词结果中查找
		institutionKeywords := []string{"大学", "学院"}
		words := strings.Split(text, " ")
		for i, word := range words {
			for _, keyword := range institutionKeywords {
				if strings.Contains(word, keyword) && i > 0 {
					// 找到可能的学校名称
					format.Institution = words[i-1] + word
					return
				}
			}
		}
	}
}

// extractBasicRequirements 提取基本要求
func (h *PaperHandler) extractBasicRequirements(text string, format *ParsedFormatRequirements) {
	// 查找基本要求部分
	basicReqRegex := regexp.MustCompile(`一、基本要求\\s*([\\s\\S]*?)\\s*二、`)
	matches := basicReqRegex.FindStringSubmatch(text)

	if len(matches) > 1 {
		// 分割成要点
		points := strings.Split(matches[1], "\n")
		for _, point := range points {
			trimmed := strings.TrimSpace(point)
			if trimmed != "" && !strings.HasPrefix(trimmed, "一、") {
				format.BasicRequirements = append(format.BasicRequirements, trimmed)
			}
		}
	} else {
		// 如果正则表达式没有匹配到，尝试从分词结果中查找基本要求
		words := strings.Split(text, " ")
		startIndex := -1
		endIndex := -1
		for i, word := range words {
			if strings.Contains(word, "基本要求") {
				startIndex = i
			} else if startIndex != -1 && strings.Contains(word, "要求") && i > startIndex {
				endIndex = i
				break
			}
		}

		//fmt.Println(111)
		//fmt.Println(words)
		//fmt.Println(222)

		if startIndex != -1 && endIndex != -1 {
			// 提取基本要求内容
			for i := startIndex; i < endIndex; i++ {
				if len(words[i]) > 3 { // 过滤掉太短的词语
					format.BasicRequirements = append(format.BasicRequirements, words[i])
				}
			}
		}
	}
}

// extractPageSetup 提取页面设置
func (h *PaperHandler) extractPageSetup(text string, format *ParsedFormatRequirements) {
	// 设置默认值

	format.PageSetup.Orientation = "portrait"

	// 提取纸张尺寸
	pattern := `([A-Za-z0-9]+)\(([0-9.]+)[×x]([0-9.]+)([a-z]+)\)`
	re := regexp.MustCompile(pattern)

	if re.MatchString(text) {
		matches := re.FindStringSubmatch(text)
		if len(matches) >= 5 {
			paperType := matches[1] // A4
			width := matches[2]     // 21
			height := matches[3]    // 29.7
			unit := matches[4]      // cm
			format.PageSetup.PaperSize = paperType + width + height + unit
		}
	} else {
		format.PageSetup.PaperSize = "A4"

	}

	// 提取页边距
	result := make(map[string]float64)

	// 先找到页边距设置的整个段落
	marginSectionPattern := `页边距设置[：:]\s*([^。\n]+(?:。|$))`
	marginSectionRe := regexp.MustCompile(marginSectionPattern)
	marginSectionMatch := marginSectionRe.FindStringSubmatch(text)

	if marginSectionMatch == nil || len(marginSectionMatch) < 2 {
		// 如果没有找到页边距设置，使用默认值
		format.PageSetup.Margins.Top = 2.5
		format.PageSetup.Margins.Right = 2.5
		format.PageSetup.Margins.Left = 2.5
		format.PageSetup.Margins.Bottom = 2.5
	} else {
		// 如果找到了页边距设置，解析具体的值
		marginText := marginSectionMatch[1]

		// 处理"均为"的情况
		if strings.Contains(marginText, "均为") {
			commonPattern := `均为\s*(\d+(?:\.\d+)?)\s*(?:厘米|cm)`
			commonRe := regexp.MustCompile(commonPattern)
			commonMatch := commonRe.FindStringSubmatch(marginText)

			if commonMatch != nil && len(commonMatch) >= 2 {
				value, _ := strconv.ParseFloat(commonMatch[1], 64)

				// 设置四个方向的值
				result["上"] = value
				result["下"] = value
				result["左"] = value
				result["右"] = value

				format.PageSetup.Margins.Top = result["上"]
				format.PageSetup.Margins.Right = result["下"]
				format.PageSetup.Margins.Left = result["左"]
				format.PageSetup.Margins.Bottom = result["右"]
			}
		}

		// 分别提取各个方向
		directionPattern := `([上下左右])[^，、]*?(\d+(?:\.\d+)?)\s*(?:厘米|cm)`
		directionRe := regexp.MustCompile(directionPattern)
		directionMatches := directionRe.FindAllStringSubmatch(marginText, -1)

		for _, match := range directionMatches {
			if len(match) >= 3 {
				direction := match[1]
				value, _ := strconv.ParseFloat(match[2], 64)

				result[direction] = value
			}
		}

		// 验证是否四个方向都有值
		marginRegex := regexp.MustCompile(`([上下左右])[^，。]*?(\d+(?:\.\d+)?)\s*(?:厘米|cm)`)
		marginMatches := marginRegex.FindAllStringSubmatch(text, -1)
		for _, match := range marginMatches {
			if len(match) > 2 {
				marginValue, err := strconv.ParseFloat(match[2], 64)
				if err == nil {
					switch match[1] {
					case "上":
						format.PageSetup.Margins.Top = marginValue
					case "下":
						format.PageSetup.Margins.Bottom = marginValue
					case "左":
						format.PageSetup.Margins.Left = marginValue
					case "右":
						format.PageSetup.Margins.Right = marginValue
					}
				}
			}
		}

		// 如果没有匹配到具体的页边距，设置默认值
		if format.PageSetup.Margins.Top == 0 && format.PageSetup.Margins.Bottom == 0 &&
			format.PageSetup.Margins.Left == 0 && format.PageSetup.Margins.Right == 0 {
			format.PageSetup.Margins.Top = 2.5
			format.PageSetup.Margins.Bottom = 2.5
			format.PageSetup.Margins.Left = 2.5
			format.PageSetup.Margins.Right = 2.5
		}
	}

	// 提取页眉页脚高度
	headerRegex := regexp.MustCompile(`页眉[：:][^，。]*?(\d+(?:\.\d+)?)\s*(?:厘米|cm)`)
	headerMatch := headerRegex.FindStringSubmatch(text)

	if len(headerMatch) > 1 {
		headerHeight, err := strconv.ParseFloat(headerMatch[1], 64)
		if err == nil {
			format.PageSetup.HeaderFooter.HeaderHeight = headerHeight
		}
	}

	footerRegex := regexp.MustCompile(`页脚[：:](\d+(?:\.\d+)?)\s*(?:厘米|cm)`)
	footerMatch := footerRegex.FindStringSubmatch(text)
	if len(footerMatch) > 1 {
		footerHeight, err := strconv.ParseFloat(footerMatch[1], 64)
		if err == nil {
			format.PageSetup.HeaderFooter.FooterHeight = footerHeight
		}
	}

	// 提取页眉内容和格式
	if strings.Contains(text, "单面印制") {
		// 单面印制的页眉设置
		if strings.Contains(text, "左对齐") && strings.Contains(text, "重庆工程学院本科生毕业设计（论文）") {
			format.PageSetup.HeaderFooter.HeaderLeft = "重庆工程学院本科生毕业设计（论文）"
		}
		if strings.Contains(text, "右对齐") && strings.Contains(text, "各章章名") {
			format.PageSetup.HeaderFooter.HeaderRight = "各章章名"
		}
	}

	if strings.Contains(text, "双面印制") || strings.Contains(text, "双面打印") {
		// 双面印制的页眉设置
		if strings.Contains(text, "左页居中") && strings.Contains(text, "重庆工程学院本科生毕业设计（论文）") {
			format.PageSetup.HeaderFooter.HeaderCenter = "重庆工程学院本科生毕业设计（论文）"
		}
		if strings.Contains(text, "右页居中") && strings.Contains(text, "各章章名") {
			// 这里可以根据需要设置其他属性来表示右页居中章名
		}
	}

	// 提取页眉字体
	if strings.Contains(text, "页眉字号为5号宋体") {
		format.FontSettings.TitleFont.ChapterTitle.FontName = "宋体"
		format.FontSettings.TitleFont.ChapterTitle.FontSize = 10.5 // 5号字
	}

	// 提取打印方式规则
	if strings.Contains(text, "50页以上") && strings.Contains(text, "双面打印") {
		format.BasicRequirements = append(format.BasicRequirements, "总页数50页以上必须双面打印")
		format.PageSetup.PrintingSide = "double"
	}

	if strings.Contains(text, "50页以下") && strings.Contains(text, "单面打印") {
		format.BasicRequirements = append(format.BasicRequirements, "总页数50页以下单面打印即可")
		if format.PageSetup.PrintingSide == "" {
			format.PageSetup.PrintingSide = "single"
		}
	}

	// 提取页码编排规则
	if strings.Contains(text, "主体部分") && strings.Contains(text, "引言或绪论") {
		format.BasicRequirements = append(format.BasicRequirements, "主体部分从引言或绪论开始用阿拉伯数字连续编页")
	}

	if strings.Contains(text, "主体之前部分") && strings.Contains(text, "中文摘要") && strings.Contains(text, "英文摘要") && strings.Contains(text, "目录") {
		format.BasicRequirements = append(format.BasicRequirements, "主体之前部分（中文摘要、英文摘要、目录）用罗马数字单独编页")
	}

	// 如果没有明确指定打印方式，默认设置为双面打印
	if format.PageSetup.PrintingSide == "" {
		format.PageSetup.PrintingSide = "double"
	}
}

// extractFontSettings 提取字体设置
func (h *PaperHandler) extractFontSettings(text string, format *ParsedFormatRequirements) {
	// 查找字体与间距部分
	fontRegex := regexp.MustCompile(`\(2\)\\s*字体与间距[\\s\\S]*?(?:字体为|用)([^，。]*)`)
	matches := fontRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		fontText := matches[1]

		// 提取正文字体
		if strings.Contains(fontText, "宋体") {
			format.FontSettings.MainFont.FontName = "宋体"
		}

		// 提取字号
		fontSizeRegex := regexp.MustCompile(`(?:(小四号|四号|小三号|三号|小二号|二号|小一号|一号)|(\\d+(?:\\.\\d+)?)\\s*(?:号|pt|磅))`)
		fontSizeMatch := fontSizeRegex.FindStringSubmatch(fontText)
		if len(fontSizeMatch) > 1 {
			if fontSizeMatch[1] != "" {
				// 中文字号转换
				switch fontSizeMatch[1] {
				case "小四号":
					format.FontSettings.MainFont.FontSize = 12.0
				case "四号":
					format.FontSettings.MainFont.FontSize = 14.0
				case "小三号":
					format.FontSettings.MainFont.FontSize = 15.0
				case "三号":
					format.FontSettings.MainFont.FontSize = 16.0
				case "小二号":
					format.FontSettings.MainFont.FontSize = 18.0
				case "二号":
					format.FontSettings.MainFont.FontSize = 22.0
				case "小一号":
					format.FontSettings.MainFont.FontSize = 24.0
				case "一号":
					format.FontSettings.MainFont.FontSize = 26.0
				}
			} else if fontSizeMatch[2] != "" {
				// 数字字号
				fontSize, err := strconv.ParseFloat(fontSizeMatch[2], 64)
				if err == nil {
					format.FontSettings.MainFont.FontSize = fontSize
				}
			}
		}

		// 提取行间距
		lineSpaceRegex := regexp.MustCompile(`(?:行间距|行距)[：:](\\d+(?:\\.\\d+)?)\\s*(?:磅|pt)`)
		lineSpaceMatch := lineSpaceRegex.FindStringSubmatch(fontText)
		if len(lineSpaceMatch) > 1 {
			lineSpace, err := strconv.ParseFloat(lineSpaceMatch[1], 64)
			if err == nil {
				format.FontSettings.MainFont.LineSpacing = lineSpace
			}
		}
	}
}

// extractDocumentStructure 提取文档结构要求
func (h *PaperHandler) extractDocumentStructure(text string, format *ParsedFormatRequirements) {
	// 查找前置部分要求
	frontMatterRegex := regexp.MustCompile(`\(1\)\\s*前置部分\\s*([\\s\\S]*?)\\s*\(?2\)?`)
	frontMatches := frontMatterRegex.FindStringSubmatch(text)
	if len(frontMatches) > 1 {
		frontText := frontMatches[1]

		if strings.Contains(frontText, "封面") {
			format.Structure.FrontMatter.CoverPage = true
		}
		if strings.Contains(frontText, "原创性声明") || strings.Contains(frontText, "版权声明") {
			format.Structure.FrontMatter.CopyrightStatement = true
		}
		if strings.Contains(frontText, "摘要") {
			format.Structure.FrontMatter.Abstract = true
		}
		if strings.Contains(frontText, "目次页") || strings.Contains(frontText, "目录") {
			format.Structure.FrontMatter.TableOfContents = true
		}
		if strings.Contains(frontText, "插图") && strings.Contains(frontText, "清单") {
			format.Structure.FrontMatter.ListOfFigures = true
		}
		if strings.Contains(frontText, "表格") && strings.Contains(frontText, "清单") {
			format.Structure.FrontMatter.ListOfTables = true
		}
	}

	// 查找主体部分要求
	mainBodyRegex := regexp.MustCompile(`\(2\)\\s*主体部分\\s*([\\s\\S]*?)\\s*\(?3\)?`)
	mainMatches := mainBodyRegex.FindStringSubmatch(text)
	if len(mainMatches) > 1 {
		mainText := mainMatches[1]

		if strings.Contains(mainText, "引言") || strings.Contains(mainText, "绪论") {
			format.Structure.MainBody.Introduction = true
		}
		if strings.Contains(mainText, "正文") {
			format.Structure.MainBody.MainContent = true
		}
		if strings.Contains(mainText, "结论") {
			format.Structure.MainBody.Conclusion = true
		}
	}

	// 查找后置部分要求
	// 修复正则表达式，使其正确匹配参考文献部分
	backMatterRegex := regexp.MustCompile(`(?:\(3\)|5\))\\s*参考文献`)
	backMatches := backMatterRegex.FindStringSubmatch(text)
	if len(backMatches) > 0 {
		format.Structure.BackMatter.References = true
	}

	if strings.Contains(text, "致谢") {
		format.Structure.BackMatter.Acknowledgements = true
	}

	if strings.Contains(text, "附录") {
		format.Structure.BackMatter.Appendices = true
	}
}

// extractCitationRules 提取引用规则
func (h *PaperHandler) extractCitationRules(text string, format *ParsedFormatRequirements) {
	// 查找参考文献部分
	citationRegex := regexp.MustCompile(`\(3\)\\s*参考文献\\s*([\\s\\S]*?)\\s*\(?4\)?`)
	matches := citationRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		citationText := matches[1]

		// 提取参考文献格式
		formatRegex := regexp.MustCompile(`(?:格式为|格式：|格式[：:])([^。]*)`)
		formatMatches := formatRegex.FindStringSubmatch(citationText)
		if len(formatMatches) > 1 {
			format.CitationRules.ReferenceFormat = strings.TrimSpace(formatMatches[1])
		}

		// 提取常见的文献类型
		refTypes := []string{"专著", "期刊", "论文集", "学位论文", "报告", "标准", "专利", "报纸", "电子公告"}
		for _, refType := range refTypes {
			if strings.Contains(citationText, refType) {
				// 简化处理，实际应该提取完整的标识符
				format.CitationRules.ReferenceTypes = append(format.CitationRules.ReferenceTypes, refType)
			}
		}
	}
}

// extractAppendixRules 提取附录规则
func (h *PaperHandler) extractAppendixRules(text string, format *ParsedFormatRequirements) {
	// 查找附录部分
	appendixRegex := regexp.MustCompile(`\(4\)\\s*附录\\s*([\\s\\S]*?)\\s*$`)
	matches := appendixRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		appendixText := matches[1]

		// 提取附录格式要求
		formatRegex := regexp.MustCompile(`(?:格式为|格式：|格式[：:])([^。]*)`)
		formatMatches := formatRegex.FindStringSubmatch(appendixText)
		if len(formatMatches) > 1 {
			format.AppendixRules.AppendixFormat = strings.TrimSpace(formatMatches[1])
		}

		// 提取附件列表
		attachmentRegex := regexp.MustCompile(`(?:包括|包含)([^。]*)`)
		attachmentMatches := attachmentRegex.FindStringSubmatch(appendixText)
		if len(attachmentMatches) > 1 {
			attachments := strings.Split(attachmentMatches[1], "、")
			for _, attachment := range attachments {
				trimmed := strings.TrimSpace(attachment)
				if trimmed != "" {
					format.AppendixRules.AttachmentList = append(format.AppendixRules.AttachmentList, trimmed)
				}
			}
		}
	}
}

// ParseDetailedFormatText 公共方法用于解析详细的格式文本内容并提取格式要求
func (h *PaperHandler) ParseDetailedFormatText(formatText string) *ParsedFormatRequirements {
	return h.parseDetailedFormatText(formatText)
}

// parseDetailedFormatText 解析详细的格式文本内容并提取格式要求
func (h *PaperHandler) parseDetailedFormatText(formatText string) *ParsedFormatRequirements {
	// 初始化默认格式要求
	format := &ParsedFormatRequirements{
		Institution:       "重庆工程学院",     //学校名称
		DocumentType:      "本科毕业设计（论文）", // 文档类型
		BasicRequirements: []string{},   // 存储各种基本格式要求的字符串数组
		PageSetup: PageSetup{ // 页面设置
			PaperSize:   "A4",       // 纸张大小
			Orientation: "portrait", // 页面方向（纵向）
			Margins: Margins{ // 页边距设置
				Top:    2.5, // 上边距2.5厘米
				Bottom: 2.5, // 下边距2.5厘米
				Left:   2.5, // 左边距2.5厘米
				Right:  2.5, // 右边距2.5厘米
			},

			HeaderFooter: HeaderFooter{ // 页眉页脚设置
				HeaderHeight: 1.6,                 // 页眉高度1.6厘米
				FooterHeight: 2.1,                 // 页脚高度2.1厘米
				HeaderLeft:   "重庆工程学院本科生毕业设计（论文）", // 页眉左侧内容
				HeaderRight:  "",                  // 页眉右侧内容
				HeaderCenter: "",                  // 页眉居中内容
			},
			PrintingSide: "single", // 打印面（单面打印）
		},
		FontSettings: FontSettings{ // 字体设置
			MainFont: MainFont{ // 正文字体
				FontName:    "宋体", // 字体名称
				FontSize:    12.0, // 字体大小（小四号）
				LineSpacing: 20.0, // 20磅  // 行间距20磅
			},
			TitleFont: TitleFont{ // 标题字体设置
				ChapterTitle: ChapterTitle{ // 章标题
					FontName:  "黑体",     // 黑体
					FontSize:  16.0,     // 三号
					Alignment: "center", // 居中对齐
				},
				SectionTitle: SectionTitle{ // 节标题
					FontName:  "黑体",   // 黑体
					FontSize:  15.0,   // 小三号
					Alignment: "left", // 左对齐
				},
				SubsectionTitle: SubsectionTitle{ // 小节标题
					FontName:  "黑体",   // 黑体
					FontSize:  14.0,   // 四号
					Alignment: "left", // 左对齐
				},
			},
			AbstractFont: AbstractFont{ // 摘要字体
				FontName: "宋体", // 字体名称
				FontSize: 12.0, // 小四号
			},
			DirectoryFont: DirectoryFont{ // 目录字体
				FontName: "宋体", // 字体名称
				FontSize: 12.0, // 小四号
			},
			TableFont: TableFont{ // 表格字体
				FontName: "宋体", // 字体名称
				FontSize: 10.5, // 五号
			},
			FigureFont: FigureFont{ // 图片说明字体
				FontName: "宋体", // 字体名称
				FontSize: 10.5, // 五号
			},
		},
		Structure: DocumentStructure{ // 文档结构
			FrontMatter: FrontMatter{ // 前置部分
				CoverPage:          true, // 需要封面
				CopyrightStatement: true, // 需要版权声明
				Abstract:           true, // 需要摘要
				TableOfContents:    true, // 需要目录
				ListOfFigures:      true, // 需要插图清单
				ListOfTables:       true, // 需要表格清单
			},
			MainBody: MainBody{ // 主体部分
				Introduction: true, // 需要引言
				MainContent:  true, // 需要正文
				Conclusion:   true, // 需要结论
			},
			BackMatter: BackMatter{ // 后置部分
				References:       true, // 需要参考文献
				Acknowledgements: true, // 需要致谢
				Appendices:       true, // 需要附录
			},
		},
		CitationRules: CitationRules{ // 引用规则
			ReferenceFormat: "[序号] 作者.题名[文献类型标识].出版地:出版者,出版年.",                                                                  // 参考文献格式
			ReferenceTypes:  []string{"专著(M)", "期刊(J)", "论文集(C)", "学位论文(D)", "报告(R)", "标准(S)", "专利(P)", "报纸(N)", "电子公告(EB/OL)"}, // 支持的文献类型
		},
		AppendixRules: AppendixRules{ // 附录规则
			AppendixFormat: "附录+字母编号",                                // 附录格式
			AttachmentList: []string{"任务书", "开题报告", "相关图纸", "光盘等资料"}, // 需要提交的附件列表
		},
	}

	// 检查是否是您提供的特定格式
	//if h.isSpecialFormat(formatText) {
	//	// 使用特殊格式解析函数
	//	h.parseSpecialFormat(formatText, format)
	//} else {
	// 使用更智能的文本处理方式来提取格式要求
	// 清理文本，去除多余空格和换行符

	cleanText := h.cleanText(formatText)

	// 从清理后的文本中提取学校名称
	h.extractInstitution(cleanText, format)

	// 提取基本要求
	h.extractBasicRequirements(cleanText, format)

	// 提取页面设置
	h.extractPageSetup(cleanText, format)

	// 提取字体设置
	h.extractFontSettings(cleanText, format)

	// 提取文档结构要求
	h.extractDocumentStructure(cleanText, format)

	// 提取引用规则
	h.extractCitationRules(cleanText, format)

	// 提取附录规则
	h.extractAppendixRules(cleanText, format)

	return format
}

// CQCECFormatRequest 重庆工程学院格式处理请求结构体
type CQCECFormatRequest struct {
	File *multipart.FileHeader `form:"file" binding:"required"`
}

// CQCECFormatResponse 重庆工程学院格式处理响应结构体
type CQCECFormatResponse struct {
	Success       bool                        `json:"success"`
	Message       string                      `json:"message"`
	Issues        []formatchecker.FormatIssue `json:"issues,omitempty"`
	Corrections   []formatchecker.Correction  `json:"corrections,omitempty"`
	CorrectedFile string                      `json:"corrected_file,omitempty"`
}

// HandleCQCECFormat 处理重庆工程学院格式要求
func (h *PaperHandler) HandleCQCECFormat(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 解析请求参数
	var req CQCECFormatRequest
	if err := c.ShouldBind(&req); err != nil {
		utils.BadRequest(c, "文件上传失败: "+err.Error())
		return
	}

	// 验证文件类型
	fileExt := strings.ToLower(filepath.Ext(req.File.Filename))
	if fileExt != ".docx" {
		utils.BadRequest(c, "仅支持DOCX格式文件")
		return
	}

	// 验证文件大小
	if req.File.Size > h.config.File.MaxSize {
		utils.BadRequest(c, "文件大小超过限制")
		return
	}

	// 创建临时论文记录
	tempPaperID := uuid.New()
	_ = &model.Paper{
		ID:          tempPaperID,
		UserID:      userID.(uuid.UUID),
		Title:       "重庆工程学院格式检查 - " + req.File.Filename,
		Description: "重庆工程学院本科毕业设计（论文）格式检查",
		FileName:    req.File.Filename,
		FileType:    "docx",
		Status:      "processing",
	}

	// 保存上传的文件
	filePath, err := h.saveUploadedFile(c, req.File, tempPaperID.String())
	if err != nil {
		utils.InternalServerError(c, "文件保存失败: "+err.Error())
		return
	}

	// 创建临时论文记录到数据库
	paper := &model.Paper{
		ID:                    tempPaperID,
		UserID:                userID.(uuid.UUID),
		Title:                 "重庆工程学院格式检查 - " + req.File.Filename,
		Description:           "重庆工程学院本科毕业设计（论文）格式检查",
		FileName:              req.File.Filename,
		FileType:              "docx",
		FilePath:              filePath,
		Status:                "uploaded",
		ParsedInfo:            "{}",
		AutoDetectedTemplates: "[]",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	// 保存论文记录到数据库
	if err := database.DB.Create(paper).Error; err != nil {
		utils.InternalServerError(c, "论文记录创建失败: "+err.Error())
		return
	}

	// 使用paperService进行格式检查
	checkResult, err := h.paperService.CheckPaperFormat(userID.(uuid.UUID), tempPaperID, uuid.Nil)
	if err != nil {
		utils.InternalServerError(c, "格式检查失败: "+err.Error())
		return
	}

	// 解析检查结果中的问题列表
	var issues []formatchecker.FormatIssue
	if err := json.Unmarshal([]byte(checkResult.Issues), &issues); err != nil {
		utils.InternalServerError(c, "检查结果解析失败: "+err.Error())
		return
	}

	// 生成修正建议
	_, err = h.paperService.FixPaperFormat(userID.(uuid.UUID), tempPaperID, checkResult.ID)
	if err != nil {
		// 即使修正失败，也返回检查结果
		response := CQCECFormatResponse{
			Success: true,
			Message: "格式检查完成，但修正文件生成失败: " + err.Error(),
			Issues:  issues,
		}
		utils.Success(c, response)
		return
	}

	// 返回成功响应
	response := CQCECFormatResponse{
		Success: true,
		Message: "格式检查完成",
		Issues:  issues,
	}
	utils.Success(c, response)
}

// saveUploadedFile 保存上传的文件
func (h *PaperHandler) saveUploadedFile(c *gin.Context, file *multipart.FileHeader, paperID string) (string, error) {
	// 创建上传目录
	uploadDir := filepath.Join(h.config.File.UploadPath, paperID)
	if err := utils.CreateDirIfNotExists(uploadDir); err != nil {
		return "", err
	}

	// 保存文件
	filePath := filepath.Join(uploadDir, file.Filename)
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		return "", err
	}

	return filePath, nil
}

// applyCorrectionsAndGenerateFile 应用修正并生成修正后的文件
func (h *PaperHandler) applyCorrectionsAndGenerateFile(originalPath string, issues []formatchecker.FormatIssue, corrections []formatchecker.Correction) (string, error) {
	// 这里应该实现具体的修正逻辑
	// 由于时间关系，暂时返回原始文件路径作为占位符
	// 实际实现应该使用unioffice库来修改文档格式

	// 创建修正后的文件路径
	correctedPath := strings.Replace(originalPath, ".docx", "_corrected.docx", 1)

	// 复制原始文件到修正路径（临时实现）
	// 实际应该使用unioffice库来应用修正
	originalContent, err := os.ReadFile(originalPath)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(correctedPath, originalContent, 0644); err != nil {
		return "", err
	}

	return correctedPath, nil
}

// UploadTemplate 上传高校论文格式模板
func (h *PaperHandler) UploadTemplate(c *gin.Context) {
	var formatText string

	//  解析模板
	//  保存到数据库
	//

	// 优先从表单文本字段获取
	formatText = c.PostForm("format_text")
	// 如果没有文本，尝试从文件获取
	// 验证文件类型
	file, err := c.FormFile("file")
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请提供格式文本或上传文件", "")
		return
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".txt" && ext != ".doc" && ext != ".docx" && ext != ".pdf" {
		utils.ErrorResponse(c, http.StatusBadRequest, "只支持TXT、DOC、DOCX、PDF格式", "")
		return
	}

	// 创建上传目录
	uploadDir := filepath.Join("uploads", "templates")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建上传目录失败", err.Error())
		return
	}

	// 保存文件
	tempFileName := fmt.Sprintf("%s_%d%s", uuid.New().String(), time.Now().Unix(), ext)
	tempFilePath := filepath.Join(uploadDir, tempFileName)

	if err := c.SaveUploadedFile(file, tempFilePath); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "保存文件失败", err.Error())
		return
	}

	// 根据文件类型提取文本
	var extractErr error
	switch ext {
	case ".txt":
		formatText, extractErr = h.extractTextFromTXT(tempFilePath)
	case ".doc", ".docx":
		formatText, extractErr = h.extractTextFromDOCX(tempFilePath)
	case ".pdf":
		formatText, extractErr = h.extractTextFromPDF(tempFilePath)
	default:
		extractErr = fmt.Errorf("不支持的文件格式: %s", ext)
	}

	if extractErr != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "提取文件内容失败", extractErr.Error())
		return
	}

	// 清理临时文件（可选）
	//defer os.Remove(tempFilePath)

	if formatText == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "格式文本不能为空", "")
		return
	}

	// 获取表单参数
	universityName := c.PostForm("university_name")
	documentType := c.PostForm("document_type")
	subject := c.PostForm("subject")
	description := c.PostForm("description")

	// 如果没有提供高校名称，尝试从文本中提取
	if universityName == "" {
		universityInfo := h.formatParserService.ExtractUniversityInfo(formatText)
		if name, ok := universityInfo["name"]; ok {
			universityName = name
		}
		if docType, ok := universityInfo["document_type"]; ok && documentType == "" {
			documentType = docType
		}
	}

	if universityName == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "高校名称不能为空", "")
		return
	}

	if documentType == "" {
		documentType = "本科论文"
	}

	if subject == "" {
		subject = "综合"
	}

	// 解析格式规范
	formatRules, err := h.formatParserService.ParseFormatFromText(formatText)

	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "解析格式失败", err.Error())
		return
	}

	// 将 formatRules 转换为 JSON 字符串
	formatRulesJSON, err := json.Marshal(formatRules)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "序列化格式规则失败", err.Error())
		return
	}

	// 查找或创建高校记录
	var university model.University
	result := database.DB.Where("name = ?", universityName).First(&university)
	if result.Error != nil {
		// 如果高校不存在，创建新的高校记录
		university = model.University{
			Name:        universityName,
			Abbr:        universityName,
			Description: description,
			Tags:        "[]",
		}
		if err := database.DB.Create(&university).Error; err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "创建高校记录失败", err.Error())
			return
		}
	} else {
		// 如果高校已存在，更新描述
		if description != "" {
			database.DB.Model(&university).Update("description", description)
		}
	}

	// 查找是否存在相同的模板（高校+文档类型+学科）
	var existingTemplate model.FormatTemplate
	err = database.DB.Where("university_id = ? AND document_type = ? AND subject = ?", university.ID, documentType, subject).First(&existingTemplate).Error

	if err == nil {
		// 更新现有模板
		existingTemplate.FormatRules = string(formatRulesJSON)
		existingTemplate.FilePath = tempFilePath
		existingTemplate.UpdatedAt = time.Now()
		if description != "" {
			existingTemplate.Description = description
		}

		if err := database.DB.Save(&existingTemplate).Error; err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "更新格式模板失败", err.Error())
			return
		}
	} else {
		// 创建格式模板记录
		// 确保TemplateID是唯一的，避免UUID解析错误

		newTemplateID := uuid.New().String()

		newTemplate := model.FormatTemplate{
			TemplateID:   newTemplateID, // 使用纯UUID字符串
			Name:         fmt.Sprintf("%s%s格式标准", universityName, documentType),
			UniversityID: &university.ID,
			DocumentType: documentType,
			Subject:      subject,
			FilePath:     tempFilePath,
			Source:       "university_upload",
			IsActive:     true,
			IsPublic:     true,
			FormatRules:  string(formatRulesJSON),
			Description:  description,
		}

		if err := database.DB.Create(&newTemplate).Error; err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "创建格式模板失败", err.Error())
			return
		}
	}

	utils.Success(c, "上传并解析成功")
}

// extractTextFromTXT 从TXT文件提取文本
func (h *PaperHandler) extractTextFromTXT(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// extractTextFromDOCX 从DOCX文件提取文本（改进版）
func (h *PaperHandler) extractTextFromDOCX(filePath string) (string, error) {
	// 尝试使用 nguyenthenguyen/docx 库
	doc, err := docx.ReadDocxFile(filePath)
	if err != nil {
		// 如果失败，回退到简单实现
		return h.extractTextFromDOCXSimple(filePath)
	}
	defer doc.Close()

	// 获取文档内容
	docx1 := doc.Editable()
	content := docx1.GetContent()

	// 清理内容
	content = h.cleanDocxContent(content)

	if content == "" {
		// 如果内容为空，尝试简单实现
		return h.extractTextFromDOCXSimple(filePath)
	}

	return content, nil
}

// cleanDocxContent 清理DOCX内容
func (h *PaperHandler) cleanDocxContent(content string) string {
	// 移除多余的空白字符
	content = regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")
	// 移除首尾空白
	content = strings.TrimSpace(content)
	return content
}

// extractTextFromDOCXSimple DOCX文本提取的简化实现（改进版）
func (h *PaperHandler) extractTextFromDOCXSimple(filePath string) (string, error) {
	// DOCX 是一个 ZIP 文件，包含 word/document.xml
	// 这里提供一个改进的实现

	// 打开 ZIP 文件
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("无法打开DOCX文件: %v", err)
	}
	defer r.Close()

	// 查找 word/document.xml
	var documentXML *zip.File
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			documentXML = f
			break
		}
	}

	if documentXML == nil {
		return "", fmt.Errorf("DOCX文件格式错误：找不到document.xml")
	}

	// 读取 XML 内容
	rc, err := documentXML.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	content, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	// 解析 XML 内容
	text := string(content)

	// 提取所有文本内容，不仅仅是<w:t>标签
	// 使用更全面的正则表达式来提取文本
	textRe := regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>|<w:instrText[^>]*>([^<]*)</w:instrText>|<w:delText[^>]*>([^<]*)</w:delText>`)
	matches := textRe.FindAllStringSubmatch(text, -1)

	var extractedText strings.Builder

	for _, match := range matches {
		// 检查不同的捕获组
		for i := 1; i < len(match); i++ {
			if match[i] != "" {
				textContent := match[i]
				// 解码可能的XML实体
				textContent = strings.ReplaceAll(textContent, "&lt;", "<")
				textContent = strings.ReplaceAll(textContent, "&gt;", ">")
				textContent = strings.ReplaceAll(textContent, "&amp;", "&")
				textContent = strings.ReplaceAll(textContent, "&quot;", "\"")
				textContent = strings.ReplaceAll(textContent, "&#39;", "'")
				extractedText.WriteString(textContent)
				break
			}
		}
	}

	// 另一种方法：使用更简单的正则表达式移除所有XML标签
	cleanText := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, " ")
	// 清理多个空格
	cleanText = regexp.MustCompile(`\s+`).ReplaceAllString(cleanText, " ")
	// 如果通过标签提取的内容为空，使用清理标签的方法
	if strings.TrimSpace(extractedText.String()) == "" {
		return cleanText, nil
	}
	// 否则，使用标签提取的内容
	text = extractedText.String()

	result := extractedText.String()

	// 清理结果
	result = h.cleanExtractedText(result)

	if result == "" {
		return "", fmt.Errorf("无法从DOCX文件中提取文本内容")
	}

	return result, nil
}

// cleanExtractedText 清理提取的文本
func (h *PaperHandler) cleanExtractedText(text string) string {
	// 移除多余的空格
	text = regexp.MustCompile(`[ \t]+`).ReplaceAllString(text, " ")
	// 移除多余的换行
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	// 移除首尾空白
	text = strings.TrimSpace(text)
	return text
}

// extractTextFromPDF 从PDF文件提取文本（改进版）
func (h *PaperHandler) extractTextFromPDF(filePath string) (string, error) {
	// 打开PDF文件
	r, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("无法打开PDF文件: %v", err)
	}

	var textBuilder strings.Builder
	totalPages := r.NumPage()

	// 限制只读取前20页（格式规范通常在前几页）
	maxPages := min(20, totalPages)

	for pageNum := 1; pageNum <= maxPages; pageNum++ {
		page := r.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		// 提取页面文本内容
		content := page.Content()

		// 遍历文本对象
		for _, text := range content.Text {
			// 过滤空白文本
			if strings.TrimSpace(text.S) != "" {
				textBuilder.WriteString(text.S)
				textBuilder.WriteString(" ")
			}
		}
		textBuilder.WriteString("\n")
	}

	result := textBuilder.String()

	// 清理文本
	result = h.cleanPDFContent(result)

	if result == "" {
		return "", fmt.Errorf("无法从PDF文件中提取文本内容")
	}

	return result, nil
}

// cleanPDFContent 清理PDF提取的内容
func (h *PaperHandler) cleanPDFContent(content string) string {
	// 移除多余的空白字符
	content = regexp.MustCompile(`[ \t]+`).ReplaceAllString(content, " ")
	// 移除多余的换行
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")
	// 移除首尾空白
	content = strings.TrimSpace(content)
	return content
}

// extractTextFromPDFSimple PDF文本提取的简化实现（备用）
func (h *PaperHandler) extractTextFromPDFSimple(filePath string) (string, error) {
	// 打开PDF文件
	r, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("无法打开PDF文件: %v", err)
	}

	var textBuilder strings.Builder
	totalPages := r.NumPage()

	// 限制只读取前10页（避免处理时间过长）
	maxPages := min(10, totalPages)

	for pageNum := 1; pageNum <= maxPages; pageNum++ {
		page := r.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		// 提取页面文本内容
		// rsc.io/pdf 库的文本提取比较基础
		content := page.Content()

		// 遍历文本对象
		for _, text := range content.Text {
			textBuilder.WriteString(text.S)
			textBuilder.WriteString(" ")
		}
		textBuilder.WriteString("\n")
	}

	result := textBuilder.String()
	if result == "" {
		return "", fmt.Errorf("无法从PDF文件中提取文本内容")
	}

	return result, nil
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// FixByTemplate 根据模板修复论文
func (h *PaperHandler) FixByTemplate(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 获取请求参数
	var req struct {
		TemplateID       int64  `form:"template_id" json:"template_id"`               // 高校ID
		FormatConfigJSON string `form:"format_config_json" json:"format_config_json"` // 格式配置JSON
	}
	// 使用 ShouldBind 支持 multipart/form-data 和 json
	if err := c.ShouldBind(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 获取论文信息
	paper, err := h.paperService.GetPaperByID(userID.(uuid.UUID), paperID)
	if err != nil {
		utils.InternalServerError(c, "论文不存在或无权限访问: "+err.Error())
		return
	}

	var checkResult *model.CheckResult
	var fixResult interface{}

	// 如果有TemplateID，使用模板进行修正
	if req.TemplateID != 0 {
		// 查找该高校下的第一个激活模板
		var template model.FormatTemplate
		if err := database.DB.Where("university_id = ? AND is_active = ?", req.TemplateID, true).First(&template).Error; err != nil {
			utils.BadRequest(c, "未找到该高校的格式模板: "+err.Error())
			return
		}

		// 执行格式检查
		checkResult, err = h.paperService.CheckPaperFormat(userID.(uuid.UUID), paper.ID, template.ID)
		if err != nil {
			utils.InternalServerError(c, "格式检查失败: "+err.Error())
			return
		}

		// 执行自动修正
		fixResult, err = h.paperService.FixPaperFormat(userID.(uuid.UUID), paper.ID, checkResult.ID)
		if err != nil {
			utils.InternalServerError(c, "格式修正失败: "+err.Error())
			return
		}

		// 更新论文状态
		database.DB.Model(&paper).Update("selected_template_id", template.ID)
	} else if req.FormatConfigJSON != "" {
		// 如果没有TemplateID但有格式配置，使用配置进行修正
		var requirements map[string]interface{}
		if err := json.Unmarshal([]byte(req.FormatConfigJSON), &requirements); err != nil {
			utils.BadRequest(c, "格式配置JSON解析失败: "+err.Error())
			return
		}

		// 使用配置进行修正
		fixResult, err = h.paperService.FixPaperFormatByParsedRequirements(userID.(uuid.UUID), paper.ID, requirements)
		if err != nil {
			utils.InternalServerError(c, "格式修正失败: "+err.Error())
			return
		}
	} else {
		utils.BadRequest(c, "必须提供template_id或format_config_json")
		return
	}

	// 构建响应
	response := gin.H{
		"message":      "格式修复完成",
		"fix_result":   fixResult,
		"download_url": fmt.Sprintf("/api/v1/papers/%s/corrected-file", paper.ID.String()),
	}

	if checkResult != nil {
		response["check_result"] = checkResult

		// 生成格式差异报告
		diffReport, err := h.formatComparisonService.GenerateFormatDifferences(checkResult.ID)
		if err == nil {
			response["format_comparison"] = diffReport
		}
	}

	// 尝试获取修正后的文件路径
	if result, ok := fixResult.(*service.CorrectionResult); ok && result != nil {
		response["corrected_file_path"] = result.CorrectedFilePath
	} else if resultMap, ok := fixResult.(map[string]interface{}); ok {
		if path, exists := resultMap["corrected_file_path"]; exists {
			response["corrected_file_path"] = path
		}
	}

	utils.Success(c, response)
}

// CreateFormatStandard 创建格式标准
func (h *PaperHandler) CreateFormatStandard(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required"`
		UniversityID *int64 `json:"university_id"`
		DocumentType string `json:"document_type"`
		FormatRules  string `json:"format_rules" binding:"required"`
		Description  string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	newTemplateID := uuid.New().String()

	template := &model.FormatTemplate{
		TemplateID:   newTemplateID, // 使用纯UUID
		Name:         req.Name,
		UniversityID: req.UniversityID,
		DocumentType: req.DocumentType,
		Source:       "system",
		IsPublic:     true,
		IsActive:     true,
		FormatRules:  req.FormatRules,
		Description:  req.Description,
	}

	if err := database.DB.Create(template).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建格式标准失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "创建成功", template)
}

// GetFormatStandardForDisplay 获取格式标准用于前端展示
func (h *PaperHandler) GetFormatStandardForDisplay(c *gin.Context) {
	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	var template model.FormatTemplate
	if err := database.DB.Preload("University").First(&template, "id = ?", templateUUID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "模板不存在", err.Error())
		return
	}

	// 解析格式规则
	var formatRules map[string]interface{}
	if err := json.Unmarshal([]byte(template.FormatRules), &formatRules); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "解析格式规则失败", err.Error())
		return
	}

	// 转换为友好展示格式
	friendlyFormat := h.convertToFriendlyFormat(formatRules)

	// 构建响应
	response := gin.H{
		"id":            template.ID,
		"name":          template.Name,
		"university":    template.University,
		"document_type": template.DocumentType,
		"description":   template.Description,
		"format_rules":  friendlyFormat,
	}

	utils.SuccessResponse(c, "获取成功", response)
}

// convertToFriendlyFormat 将格式规则转换为友好展示格式
func (h *PaperHandler) convertToFriendlyFormat(formatRules map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range formatRules {
		switch key {
		case "body":
			if bodyMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertBodyFormat(bodyMap)
			}
		case "title":
			if titleMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertTitleFormat(titleMap)
			}
		case "author":
			if authorMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertAuthorFormat(authorMap)
			}
		case "abstract":
			if abstractMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertAbstractFormat(abstractMap)
			}
		case "headings":
			if headingsMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertHeadingsFormat(headingsMap)
			}
		case "keywords":
			if keywordsMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertKeywordsFormat(keywordsMap)
			}
		case "page_setup":
			if pageSetupMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertPageSetupFormat(pageSetupMap)
			}
		case "references":
			if referencesMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertReferencesFormat(referencesMap)
			}
		default:
			result[key] = value
		}
	}

	return result
}

// convertBodyFormat 转换正文格式
func (h *PaperHandler) convertBodyFormat(bodyMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range bodyMap {
		switch key {
		case "font_size":
			if fontSize, ok := value.(string); ok {
				result[key] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
			} else {
				result[key] = value
			}
		case "alignment":
			if alignment, ok := value.(string); ok {
				result[key] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
			} else {
				result[key] = value
			}
		case "line_space":
			if lineSpace, ok := value.(string); ok {
				result[key] = lineSpace + "（" + h.lineSpaceToChinese(lineSpace) + "）"
			} else {
				result[key] = value
			}
		case "first_line_indent":
			if indent, ok := value.(string); ok {
				result[key] = indent + "（" + h.indentToChinese(indent) + "）"
			} else {
				result[key] = value
			}
		default:
			result[key] = value
		}
	}

	return result
}

// convertTitleFormat 转换标题格式
func (h *PaperHandler) convertTitleFormat(titleMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range titleMap {
		switch key {
		case "font_size":
			if fontSize, ok := value.(string); ok {
				result[key] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
			} else {
				result[key] = value
			}
		case "alignment":
			if alignment, ok := value.(string); ok {
				result[key] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
			} else {
				result[key] = value
			}
		case "font_name":
			if fontName, ok := value.(string); ok {
				result[key] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
			} else {
				result[key] = value
			}
		default:
			result[key] = value
		}
	}

	return result
}

// convertAuthorFormat 转换作者格式
func (h *PaperHandler) convertAuthorFormat(authorMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range authorMap {
		switch key {
		case "font_size":
			if fontSize, ok := value.(string); ok {
				result[key] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
			} else {
				result[key] = value
			}
		case "alignment":
			if alignment, ok := value.(string); ok {
				result[key] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
			} else {
				result[key] = value
			}
		case "font_name":
			if fontName, ok := value.(string); ok {
				result[key] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
			} else {
				result[key] = value
			}
		default:
			result[key] = value
		}
	}

	return result
}

// convertAbstractFormat 转换摘要格式
func (h *PaperHandler) convertAbstractFormat(abstractMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range abstractMap {
		switch key {
		case "label":
			if labelMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertLabelFormat(labelMap)
			}
		case "content":
			if contentMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertContentFormat(contentMap)
			}
		default:
			result[key] = value
		}
	}

	return result
}

// convertHeadingsFormat 转换标题层级格式
func (h *PaperHandler) convertHeadingsFormat(headingsMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range headingsMap {
		if headingMap, ok := value.(map[string]interface{}); ok {
			result[key] = h.convertHeadingFormat(headingMap)
		} else {
			result[key] = value
		}
	}

	return result
}

// convertKeywordsFormat 转换关键词格式
func (h *PaperHandler) convertKeywordsFormat(keywordsMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range keywordsMap {
		switch key {
		case "label":
			if labelMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertLabelFormat(labelMap)
			}
		case "content":
			if contentMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertContentFormat(contentMap)
			}
		default:
			result[key] = value
		}
	}

	return result
}

// convertPageSetupFormat 转换页面设置格式
func (h *PaperHandler) convertPageSetupFormat(pageSetupMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range pageSetupMap {
		switch key {
		case "margins":
			if marginsMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertMarginsFormat(marginsMap)
			}
		case "header":
			if headerMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertHeaderFooterFormat(headerMap)
			}
		case "footer":
			if footerMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertHeaderFooterFormat(footerMap)
			}
		default:
			result[key] = value
		}
	}

	return result
}

// convertReferencesFormat 转换参考文献格式
func (h *PaperHandler) convertReferencesFormat(referencesMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range referencesMap {
		switch key {
		case "title":
			if titleMap, ok := value.(map[string]interface{}); ok {
				result[key] = h.convertTitleFormat(titleMap)
			}
		default:
			result[key] = value
		}
	}

	return result
}

// convertLabelFormat 转换标签格式
func (h *PaperHandler) convertLabelFormat(labelMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range labelMap {
		switch key {
		case "font_size":
			if fontSize, ok := value.(string); ok {
				result[key] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
			} else {
				result[key] = value
			}
		case "font_name":
			if fontName, ok := value.(string); ok {
				result[key] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
			} else {
				result[key] = value
			}
		default:
			result[key] = value
		}
	}

	return result
}

// convertContentFormat 转换内容格式
func (h *PaperHandler) convertContentFormat(contentMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range contentMap {
		switch key {
		case "font_size":
			if fontSize, ok := value.(string); ok {
				result[key] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
			} else {
				result[key] = value
			}
		case "font_name":
			if fontName, ok := value.(string); ok {
				result[key] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
			} else {
				result[key] = value
			}
		case "alignment":
			if alignment, ok := value.(string); ok {
				result[key] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
			} else {
				result[key] = value
			}
		default:
			result[key] = value
		}
	}

	return result
}

// convertHeadingFormat 转换单个标题格式
func (h *PaperHandler) convertHeadingFormat(headingMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range headingMap {
		switch key {
		case "font_size":
			if fontSize, ok := value.(string); ok {
				result[key] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
			} else {
				result[key] = value
			}
		case "alignment":
			if alignment, ok := value.(string); ok {
				result[key] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
			} else {
				result[key] = value
			}
		case "font_name":
			if fontName, ok := value.(string); ok {
				result[key] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
			} else {
				result[key] = value
			}
		default:
			result[key] = value
		}
	}

	return result
}

// convertMarginsFormat 转换页边距格式
func (h *PaperHandler) convertMarginsFormat(marginsMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range marginsMap {
		result[key] = value
	}

	return result
}

// convertHeaderFooterFormat 转换页眉页脚格式
func (h *PaperHandler) convertHeaderFooterFormat(headerFooterMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range headerFooterMap {
		result[key] = value
	}

	return result
}

// fontSizeToChinese 将字号转换为中文描述
func (h *PaperHandler) fontSizeToChinese(fontSize string) string {
	switch fontSize {
	case "小四号":
		return "12磅"
	case "四号":
		return "14磅"
	case "三号":
		return "16磅"
	case "小三号":
		return "15磅"
	case "五号":
		return "10.5磅"
	case "小五号":
		return "9磅"
	case "二号":
		return "22磅"
	case "一号":
		return "26磅"
	default:
		return fontSize
	}
}

// alignmentToChinese 将对齐方式转换为中文描述
func (h *PaperHandler) alignmentToChinese(alignment string) string {
	switch alignment {
	case "left":
		return "左对齐"
	case "center":
		return "居中"
	case "right":
		return "右对齐"
	case "justify":
		return "两端对齐"
	default:
		return alignment
	}
}

// lineSpaceToChinese 将行距转换为中文描述
func (h *PaperHandler) lineSpaceToChinese(lineSpace string) string {
	switch lineSpace {
	case "1.0", "single":
		return "单倍行距"
	case "1.5":
		return "1.5倍行距"
	case "2.0", "double":
		return "双倍行距"
	case "multiple":
		return "多倍行距"
	default:
		// 处理其他格式，如固定值格式 "fixed_20_pt"
		if strings.HasPrefix(lineSpace, "fixed_") {
			ptStr := strings.TrimPrefix(lineSpace, "fixed_")
			ptStr = strings.TrimSuffix(ptStr, "_pt")
			if _, err := strconv.ParseFloat(ptStr, 64); err == nil {
				return ptStr + "磅固定值"
			}
		}
		// 处理 "30" 这样的数值（可能是多倍行距倍数）
		if _, err := strconv.ParseFloat(lineSpace, 64); err == nil {
			return lineSpace + "倍行距"
		}
		return lineSpace
	}
}

// indentToChinese 将缩进转换为中文描述
func (h *PaperHandler) indentToChinese(indent string) string {
	switch indent {
	case "2字符":
		return "2个字符宽度"
	case "0字符":
		return "无缩进"
	default:
		return indent
	}
}

// fontNameToChinese 将字体名称转换为中文描述
func (h *PaperHandler) fontNameToChinese(fontName string) string {
	switch fontName {
	case "宋体":
		return "SimSun"
	case "黑体":
		return "SimHei"
	case "仿宋":
		return "FangSong"
	case "楷体":
		return "KaiTi"
	case "Times New Roman":
		return "西文字体"
	default:
		return fontName
	}
}

// UpdateFormatStandard 更新格式标准
func (h *PaperHandler) UpdateFormatStandard(c *gin.Context) {
	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	var req struct {
		Name         string `json:"name"`
		UniversityID *int64 `json:"university_id"`
		DocumentType string `json:"document_type"`
		FormatRules  string `json:"format_rules"`
		Description  string `json:"description"`
		IsActive     *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	updateData := make(map[string]interface{})
	if req.Name != "" {
		updateData["name"] = req.Name
	}
	if req.UniversityID != nil {
		updateData["university_id"] = *req.UniversityID
	}
	if req.DocumentType != "" {
		updateData["document_type"] = req.DocumentType
	}
	if req.FormatRules != "" {
		updateData["format_rules"] = req.FormatRules
	}
	if req.Description != "" {
		updateData["description"] = req.Description
	}
	if req.IsActive != nil {
		updateData["is_active"] = *req.IsActive
	}

	if err := database.DB.Model(&model.FormatTemplate{}).Where("id = ?", templateUUID).Updates(updateData).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新格式标准失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// DeleteFormatStandard 删除格式标准
func (h *PaperHandler) DeleteFormatStandard(c *gin.Context) {
	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	if err := database.DB.Model(&model.FormatTemplate{}).Where("id = ?", templateUUID).Update("is_active", false).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除格式标准失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

// GetFormatStandardByID 根据ID获取格式标准
func (h *PaperHandler) GetFormatStandardByID(c *gin.Context) {
	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	var template model.FormatTemplate
	if err := database.DB.Preload("University").First(&template, "id = ?", templateUUID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "格式标准不存在", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", template)
}
