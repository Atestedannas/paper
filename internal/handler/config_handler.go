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

// contactFieldMap 前端字段名 -> 数据库 key 映射
var contactFieldMap = map[string]string{
	"name":           "contact_name",
	"email_support":  "contact_email_support",
	"email_business": "contact_email_business",
	"wechat_qrcode":  "contact_wechat_qrcode",
	"phone":          "contact_phone",
	"address":        "contact_address",
	"work_hours":     "contact_work_hours",
	"remarks":        "contact_remarks",
}

// GetContactInfo 获取联系信息（公开接口）
func (h *ConfigHandler) GetContactInfo(c *gin.Context) {
	dbKeys := make([]string, 0, len(contactFieldMap))
	for _, v := range contactFieldMap {
		dbKeys = append(dbKeys, v)
	}

	var settings []model.SystemSetting
	if err := database.DB.Where("key IN ?", dbKeys).Find(&settings).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取联系信息失败", err.Error())
		return
	}

	configMap := make(map[string]string)
	for _, s := range settings {
		configMap[s.Key] = s.Value
	}

	response := gin.H{
		"name":           getStringValue(configMap, "contact_name", ""),
		"email_support":  getStringValue(configMap, "contact_email_support", ""),
		"email_business": getStringValue(configMap, "contact_email_business", ""),
		"wechat_qrcode":  getStringValue(configMap, "contact_wechat_qrcode", ""),
		"phone":          getStringValue(configMap, "contact_phone", ""),
		"address":        getStringValue(configMap, "contact_address", ""),
		"work_hours":     getStringValue(configMap, "contact_work_hours", ""),
		"remarks":        getStringValue(configMap, "contact_remarks", ""),
	}

	utils.SuccessResponse(c, "获取联系信息成功", response)
}

// UpdateContactInfo 更新联系信息（管理员接口）
func (h *ConfigHandler) UpdateContactInfo(c *gin.Context) {
	var req struct {
		Name          string `json:"name"`
		EmailSupport  string `json:"email_support"`
		EmailBusiness string `json:"email_business"`
		WechatQrcode  string `json:"wechat_qrcode"`
		Phone         string `json:"phone"`
		Address       string `json:"address"`
		WorkHours     string `json:"work_hours"`
		Remarks       string `json:"remarks"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 将前端字段逐一 upsert 到 system_settings 表
	updates := map[string]string{
		"contact_name":           req.Name,
		"contact_email_support":  req.EmailSupport,
		"contact_email_business": req.EmailBusiness,
		"contact_wechat_qrcode":  req.WechatQrcode,
		"contact_phone":          req.Phone,
		"contact_address":        req.Address,
		"contact_work_hours":     req.WorkHours,
		"contact_remarks":        req.Remarks,
	}

	for key, value := range updates {
		setting := model.SystemSetting{Key: key, Value: value}
		if err := database.DB.Where(model.SystemSetting{Key: key}).
			Assign(model.SystemSetting{Value: value, Description: "客服联系信息"}).
			FirstOrCreate(&setting).Error; err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "保存联系信息失败", err.Error())
			return
		}
		// 更新已存在的值
		if err := database.DB.Model(&model.SystemSetting{}).
			Where("key = ?", key).
			Update("value", value).Error; err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "更新联系信息失败", err.Error())
			return
		}
	}

	utils.SuccessResponse(c, "联系信息保存成功", nil)
}

// getStringValue 安全获取字符串值
func getStringValue(configMap map[string]string, key, defaultValue string) string {
	if val, exists := configMap[key]; exists && val != "" {
		return val
	}
	return defaultValue
}
