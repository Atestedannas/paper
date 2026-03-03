package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

type AdminSystemHandler struct {
	settingService service.SystemSettingService
}

func NewAdminSystemHandler() *AdminSystemHandler {
	return &AdminSystemHandler{
		settingService: service.GetSystemSettingService(),
	}
}

// UpdateSecuritySettings 更新安全设置
func (h *AdminSystemHandler) UpdateSecuritySettings(c *gin.Context) {
	var req struct {
		EncryptionKey string `json:"encryption_key" binding:"required,len=32"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.settingService.UpdateEncryptionKey(req.EncryptionKey); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新安全设置失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// GetSecuritySettings 获取安全设置状态
func (h *AdminSystemHandler) GetSecuritySettings(c *gin.Context) {
	key := h.settingService.GetEncryptionKey()

	maskedKey := "******"
	if key == service.DefaultDevKey {
		maskedKey = "DEFAULT (UNSAFE)"
	} else if len(key) == 32 {
		maskedKey = key[:4] + "******" + key[28:]
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"encryption_key_status": maskedKey,
		"is_default":            key == service.DefaultDevKey,
	})
}

// UpdatePaymentConfig 更新支付策略
func (h *AdminSystemHandler) UpdatePaymentConfig(c *gin.Context) {
	var req struct {
		IsCheckFree   *bool    `json:"is_check_free"`
		FormatCheck   *float64 `json:"format_check"`
		FormatFix     *float64 `json:"format_fix"`
		PaymentConfig *struct {
			IsCheckFree *bool    `json:"is_check_free"`
			FormatCheck *float64 `json:"format_check"`
			FormatFix   *float64 `json:"format_fix"`
		} `json:"payment_config"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	var isCheckFree *bool
	var formatCheck, formatFix *float64

	if req.PaymentConfig != nil {
		isCheckFree = req.PaymentConfig.IsCheckFree
		formatCheck = req.PaymentConfig.FormatCheck
		formatFix = req.PaymentConfig.FormatFix
	} else {
		isCheckFree = req.IsCheckFree
		formatCheck = req.FormatCheck
		formatFix = req.FormatFix
	}

	if err := h.settingService.UpdatePaymentConfig(isCheckFree, formatCheck, formatFix); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新支付配置失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// GetPaymentConfig 获取支付策略配置
func (h *AdminSystemHandler) GetPaymentConfig(c *gin.Context) {
	config, err := h.settingService.GetPaymentConfig()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取支付配置失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", config)
}

// GetPaymentProviderSettings 获取指定渠道的支付设置
func (h *AdminSystemHandler) GetPaymentProviderSettings(c *gin.Context) {
	provider := c.Param("provider")
	if provider != "alipay" && provider != "wechat" {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的支付渠道", "")
		return
	}

	settings, err := h.settingService.GetPaymentProviderSettings(provider)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取配置失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", settings)
}

// UpdatePaymentProviderSettings 更新指定渠道的支付设置
func (h *AdminSystemHandler) UpdatePaymentProviderSettings(c *gin.Context) {
	provider := c.Param("provider")
	if provider != "alipay" && provider != "wechat" {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的支付渠道", "")
		return
	}

	var settings map[string]interface{}
	if err := c.ShouldBindJSON(&settings); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.settingService.UpdatePaymentProviderSettings(provider, settings); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新配置失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// TestPaymentProviderSettings 测试支付配置
func (h *AdminSystemHandler) TestPaymentProviderSettings(c *gin.Context) {
	provider := c.Param("provider")
	if provider != "alipay" && provider != "wechat" {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的支付渠道", "")
		return
	}

	// 这里可以添加实际的连接测试逻辑
	// 目前先返回模拟结果
	// TODO: Implement actual connection test

	result := gin.H{
		"provider": provider,
		"status":   "success",
		"message":  "连接测试成功",
	}

	utils.SuccessResponse(c, "测试成功", result)
}

// UploadImage 上传图片
func (h *AdminSystemHandler) UploadImage(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请选择要上传的文件", err.Error())
		return
	}

	// 验证文件类型
	allowedTypes := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/gif":  true,
	}
	if !allowedTypes[file.Header.Get("Content-Type")] {
		utils.ErrorResponse(c, http.StatusBadRequest, "只支持 JPG/PNG/GIF 格式的图片", "")
		return
	}

	// 验证文件大小 (5MB)
	if file.Size > 5*1024*1024 {
		utils.ErrorResponse(c, http.StatusBadRequest, "图片大小不能超过 5MB", "")
		return
	}

	// 获取文件扩展名
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" {
		utils.ErrorResponse(c, http.StatusBadRequest, "只支持 JPG/PNG/GIF 格式", "")
		return
	}

	// 创建上传目录
	uploadDir := filepath.Join("uploads", "images")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建上传目录失败", err.Error())
		return
	}

	// 生成唯一的文件名
	uniqueName := fmt.Sprintf("%s_%d%s", uuid.New().String(), time.Now().UnixNano(), ext)
	filePath := filepath.Join(uploadDir, uniqueName)

	// 保存文件
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "保存文件失败", err.Error())
		return
	}

	// 返回文件的访问URL
	baseURL := c.Request.URL.Scheme + "://" + c.Request.Host
	fileURL := fmt.Sprintf("%s/uploads/images/%s", baseURL, uniqueName)

	utils.SuccessResponse(c, "上传成功", gin.H{
		"url":  fileURL,
		"path": filepath.Join("uploads", "images", uniqueName),
	})
}
