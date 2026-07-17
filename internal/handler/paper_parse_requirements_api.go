package handler

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/utils"
)

// ParseFormatRequirementsRequest 解析格式要求请求结构体
type ParseFormatRequirementsRequest struct {
	FormatText string `json:"format_text" binding:"required"`
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
	parsedFormat := h.parseDetailedFormatText(req.FormatText)
	parseResult, err := h.formatParserService.ParseFormatFromTextDetailed(req.FormatText)
	if err != nil {
		utils.InternalServerError(c, fmt.Sprintf("failed to parse format requirements: %v", err))
		return
	}

	// 将解析结果转换为汉字键值对结构
	formatRules := parseResult.Rules

	// 将汉字键值对结构转换为JSON字符串，以便保存到数据库
	settingsJSON, err := json.Marshal(formatRules)
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
		TemplateID:      newTemplateID, // 使用标准UUID字符串，不带前缀
		Name:            fmt.Sprintf("%s格式标准", parsedFormat.Institution),
		DocumentType:    "本科论文",
		Source:          "auto_parsed",
		Version:         "1.0",
		IsPublic:        false,
		IsActive:        true,
		FormatRules:     string(settingsJSON),
		ParseConfidence: parseResult.Quality.QualityScore,
		Description:     fmt.Sprintf("从文本解析生成的格式标准: %s", parsedFormat.Institution),
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
		"format_requirements": formatRules,
		"parse_quality":       parseResult.Quality,
	}

	utils.Created(c, response)
}
