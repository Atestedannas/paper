package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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
	chineseFormat := friendlyParsedRequirementsToChineseMap(parsedFormat)

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
