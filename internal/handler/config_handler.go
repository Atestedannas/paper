package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// ConfigHandler 系统配置处理器
type ConfigHandler struct{}

// NewConfigHandler 创建系统配置处理器实例
func NewConfigHandler() *ConfigHandler {
	return &ConfigHandler{}
}

// GetSystemConfig 获取系统配置
func (h *ConfigHandler) GetSystemConfig(c *gin.Context) {
	var settings []model.SystemSetting

	// 获取所有系统配置
	if err := database.DB.Find(&settings).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取系统配置失败", err.Error())
		return
	}

	// 构建配置映射
	configMap := make(map[string]interface{})
	for _, setting := range settings {
		configMap[setting.Key] = setting.Value
	}

	// 获取论文格式检查是否需要付费的配置
	isPaperCheckPaid := true // 默认付费

	if val, exists := configMap["paper_check_paid"]; exists {
		if val == "false" || val == "0" {
			isPaperCheckPaid = false
		}
	}

	// 获取其他相关配置
	maxFreeChecks := 2 // 默认免费次数
	if val, exists := configMap["free_check_limit"]; exists {
		if strVal, ok := val.(string); ok {
			maxFreeChecks = utils.StringToInt(strVal, 2)
		}
	}

	response := gin.H{
		"is_paper_check_paid": isPaperCheckPaid,                // 论文格式检查是否付费
		"max_free_checks":     maxFreeChecks,                   // 最大免费检查次数
		"site_name":           configMap["site_name"],          // 网站名称
		"site_description":    configMap["site_description"],   // 网站描述
		"max_file_size":       configMap["max_file_size"],      // 最大文件大小
		"allowed_file_types":  configMap["allowed_file_types"], // 允许的文件类型
	}

	utils.SuccessResponse(c, "获取系统配置成功", response)
}

// GetPaperCheckConfigPublic 公开获取论文格式检查配置（不需要认证）
func (h *ConfigHandler) GetPaperCheckConfigPublic(c *gin.Context) {
	// 获取支付配置来检查是否免费
	settingService := service.GetSystemSettingService()
	paymentConfig, err := settingService.GetPaymentConfig()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取支付配置失败", err.Error())
		return
	}

	// 检查是否设置了免费检查
	isCheckFree := false
	if isCheckFreeVal, ok := paymentConfig["is_check_free"].(bool); ok {
		isCheckFree = isCheckFreeVal
	}

	// 如果是免费的，返回免费配置
	if isCheckFree {
		utils.Success(c, gin.H{
			"is_paper_check_paid": false,
			"max_free_checks":     999,      // 免费时设置很大的检查次数
			"max_file_size":       52428800, // 50MB
			"allowed_file_types":  "pdf,docx",
			"check_timeout":       300, // 5分钟
			"enable_pdf_check":    true,
			"enable_docx_check":   true,
		})
		return
	}

	// 如果不是免费的，调用原有的配置获取方法
	h.GetPaperCheckConfig(c)
}

// GetPaperCheckConfig 获取论文格式检查配置
func (h *ConfigHandler) GetPaperCheckConfig(c *gin.Context) {
	// 只获取与论文格式检查相关的配置
	keys := []string{
		"paper_check_paid",   // 论文格式检查是否付费
		"free_check_limit",   // 免费检查次数限制
		"max_file_size",      // 最大文件大小
		"allowed_file_types", // 允许的文件类型
		"check_timeout",      // 检查超时时间
		"enable_pdf_check",   // 是否启用PDF检查
		"enable_docx_check",  // 是否启用DOCX检查
	}

	var configSettings []model.SystemSetting
	if err := database.DB.Where("key IN ?", keys).Find(&configSettings).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取论文检查配置失败", err.Error())
		return
	}

	// 构建配置映射
	configMap := make(map[string]string)
	for _, setting := range configSettings {
		configMap[setting.Key] = setting.Value
	}

	// 确定论文格式检查是否需要付费
	isPaperCheckPaid := true
	if val, exists := configMap["paper_check_paid"]; exists {
		if val == "false" || val == "0" {
			isPaperCheckPaid = false
		}
	}

	// 获取最大免费检查次数
	maxFreeChecks := 2
	if val, exists := configMap["free_check_limit"]; exists {
		maxFreeChecks = utils.StringToInt(val, 2)
	}

	// 获取最大文件大小
	maxFileSize := 52428800 // 默认50MB
	if val, exists := configMap["max_file_size"]; exists {
		maxFileSize = utils.StringToInt(val, 52428800)
	}

	// 获取允许的文件类型
	allowedFileTypes := "pdf,docx"
	if val, exists := configMap["allowed_file_types"]; exists {
		allowedFileTypes = val
	}

	// 检查功能开关
	enablePDFCheck := true
	if val, exists := configMap["enable_pdf_check"]; exists {
		if val == "false" || val == "0" {
			enablePDFCheck = false
		}
	}

	enableDOCXCheck := true
	if val, exists := configMap["enable_docx_check"]; exists {
		if val == "false" || val == "0" {
			enableDOCXCheck = false
		}
	}

	response := gin.H{
		"is_paper_check_paid": isPaperCheckPaid,           // 论文格式检查是否付费
		"max_free_checks":     maxFreeChecks,              // 最大免费检查次数
		"max_file_size":       maxFileSize,                // 最大文件大小（字节）
		"allowed_file_types":  allowedFileTypes,           // 允许的文件类型
		"enable_pdf_check":    enablePDFCheck,             // 是否启用PDF检查
		"enable_docx_check":   enableDOCXCheck,            // 是否启用DOCX检查
		"check_timeout":       configMap["check_timeout"], // 检查超时时间（秒）
	}

	utils.SuccessResponse(c, "获取论文格式检查配置成功", response)
}

// GetContactInfo 获取联系信息（公开接口）
func (h *ConfigHandler) GetContactInfo(c *gin.Context) {
	// 从数据库获取联系信息配置
	var settings []model.SystemSetting

	contactInfoKeys := []string{
		"contact_email_support",  // 技术支持邮箱
		"contact_email_business", // 商务合作邮箱
		"contact_wechat_qrcode",  // 微信二维码图片URL
		"contact_phone",          // 联系电话
		"contact_address",        // 联系地址
		"contact_work_hours",     // 工作时间
	}

	if err := database.DB.Where("key IN ?", contactInfoKeys).Find(&settings).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取联系信息失败", err.Error())
		return
	}

	// 构建配置映射
	configMap := make(map[string]string)
	for _, setting := range settings {
		configMap[setting.Key] = setting.Value
	}

	// 构建响应，默认值从原硬编码数据获取
	response := gin.H{
		"email_support":  getStringValue(configMap, "contact_email_support", "2673078804@qq.com"),
		"email_business": getStringValue(configMap, "contact_email_business", "2673078804@qq.com"),
		"wechat_qrcode":  getStringValue(configMap, "contact_wechat_qrcode", "https://img1.baidu.com/it/u=3719270913,2773989566&fm=253&fmt=auto&app=138&f=JPEG?w=500&h=500"),
		"phone":          getStringValue(configMap, "contact_phone", ""),
		"address":        getStringValue(configMap, "contact_address", ""),
		"work_hours":     getStringValue(configMap, "contact_work_hours", ""),
	}

	utils.SuccessResponse(c, "获取联系信息成功", response)
}

// getStringValue 安全获取字符串值
func getStringValue(configMap map[string]string, key, defaultValue string) string {
	if val, exists := configMap[key]; exists && val != "" {
		return val
	}
	return defaultValue
}
