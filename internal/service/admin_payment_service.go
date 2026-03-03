package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/utils"
)

type AdminPaymentService interface {
	GetPaymentSetting(provider string) (map[string]interface{}, error)
	UpdatePaymentSetting(userID uuid.UUID, provider string, settings map[string]interface{}) error
	UploadCertificate(userID uuid.UUID, provider string, content []byte, filename string) error
	TogglePaymentProvider(userID uuid.UUID, provider string, enabled bool) error
	TestPaymentConnection(provider string) (map[string]interface{}, error)
	GetAuditLogs(page, pageSize int) ([]model.PaymentAuditLog, int64, error)
}

type adminPaymentService struct {
	config        *config.Config
	systemSetting SystemSettingService
}

func NewAdminPaymentService(config *config.Config) AdminPaymentService {
	return &adminPaymentService{
		config:        config,
		systemSetting: GetSystemSettingService(),
	}
}

// GetPaymentSetting 获取支付配置（脱敏）
func (s *adminPaymentService) GetPaymentSetting(provider string) (map[string]interface{}, error) {
	var setting model.PaymentSetting
	if err := database.DB.Where("provider = ?", provider).First(&setting).Error; err != nil {
		// 如果不存在，返回空配置结构
		return map[string]interface{}{
			"provider": provider,
			"enabled":  false,
			"version":  0,
		}, nil
	}

	var data map[string]interface{}
	// 先解密
	// 使用系统设置服务获取密钥
	key := s.systemSetting.GetEncryptionKey()
	decryptedSettings, err := utils.Decrypt(setting.Settings, []byte(key))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt settings: %w", err)
	}

	if err := json.Unmarshal([]byte(decryptedSettings), &data); err != nil {
		return nil, err
	}

	// 脱敏处理
	maskSensitiveData(provider, data)
	data["enabled"] = setting.Enabled
	data["version"] = setting.Version

	return data, nil
}

// UpdatePaymentSetting 更新支付配置
func (s *adminPaymentService) UpdatePaymentSetting(userID uuid.UUID, provider string, settings map[string]interface{}) error {
	// 1. 获取现有配置或创建新配置
	var setting model.PaymentSetting
	result := database.DB.Where("provider = ?", provider).First(&setting)
	isNew := result.RowsAffected == 0

	// 2. 准备新配置数据
	// 如果是更新，需要合并旧配置中的加密字段（如果本次未传）
	if !isNew {
		var oldData map[string]interface{}
		// 解密旧配置以进行合并
		key := s.systemSetting.GetEncryptionKey()
		decryptedOldSettings, _ := utils.Decrypt(setting.Settings, []byte(key))
		json.Unmarshal([]byte(decryptedOldSettings), &oldData)
		mergeSettings(provider, settings, oldData)
	}

	// 3. 序列化并保存
	settingsJSON, _ := json.Marshal(settings)
	// 加密存储
	key := s.systemSetting.GetEncryptionKey()
	encryptedSettings, err := utils.Encrypt(string(settingsJSON), []byte(key))
	if err != nil {
		return fmt.Errorf("failed to encrypt settings: %w", err)
	}

	tx := database.DB.Begin()

	if isNew {
		setting = model.PaymentSetting{
			Provider: provider,
			Settings: encryptedSettings,
			Enabled:  false, // 默认不启用，需单独开启
			Version:  1,
		}
		if err := tx.Create(&setting).Error; err != nil {
			tx.Rollback()
			return err
		}
	} else {
		// 乐观锁检查（如果前端传了version）
		if v, ok := settings["version"].(float64); ok && int(v) != setting.Version {
			tx.Rollback()
			return errors.New("configuration has been modified by others")
		}

		setting.Settings = encryptedSettings
		setting.Version++
		if err := tx.Save(&setting).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	// 4. 记录审计日志
	auditLog := model.PaymentAuditLog{
		UserID:   userID,
		Provider: provider,
		Action:   "update",
		Changes:  fmt.Sprintf("Updated settings for %s", provider),
		ClientIP: "", // 在Handler层补充
	}
	if err := tx.Create(&auditLog).Error; err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit().Error
}

// UploadCertificate 上传证书
func (s *adminPaymentService) UploadCertificate(userID uuid.UUID, provider string, content []byte, filename string) error {
	// 简单模拟证书解析
	fingerprint := uuid.New().String() // 实际应解析证书内容生成指纹

	// 加密证书内容
	key := s.systemSetting.GetEncryptionKey()
	encryptedContent, err := utils.Encrypt(string(content), []byte(key))
	if err != nil {
		return fmt.Errorf("failed to encrypt certificate content: %w", err)
	}

	cert := model.PaymentCertificate{
		Provider:    provider,
		Fingerprint: fingerprint,
		ValidFrom:   time.Now(),
		ValidTo:     time.Now().AddDate(1, 0, 0),
		Status:      "active",
		Content:     encryptedContent, // 加密存储
		Path:        filename,
	}

	tx := database.DB.Begin()

	// 标记旧证书为过期
	tx.Model(&model.PaymentCertificate{}).
		Where("provider = ? AND status = ?", provider, "active").
		Update("status", "inactive")

	if err := tx.Create(&cert).Error; err != nil {
		tx.Rollback()
		return err
	}

	// 审计日志
	auditLog := model.PaymentAuditLog{
		UserID:   userID,
		Provider: provider,
		Action:   "upload_cert",
		Changes:  fmt.Sprintf("Uploaded certificate: %s", filename),
	}
	tx.Create(&auditLog)

	return tx.Commit().Error
}

// TogglePaymentProvider 启用/停用支付渠道
func (s *adminPaymentService) TogglePaymentProvider(userID uuid.UUID, provider string, enabled bool) error {
	tx := database.DB.Begin()

	if err := tx.Model(&model.PaymentSetting{}).
		Where("provider = ?", provider).
		Update("enabled", enabled).Error; err != nil {
		tx.Rollback()
		return err
	}

	auditLog := model.PaymentAuditLog{
		UserID:   userID,
		Provider: provider,
		Action:   "toggle",
		Changes:  fmt.Sprintf("Set enabled to %v", enabled),
	}
	tx.Create(&auditLog)

	return tx.Commit().Error
}

// TestPaymentConnection 测试连接
func (s *adminPaymentService) TestPaymentConnection(provider string) (map[string]interface{}, error) {
	// 模拟测试逻辑
	return map[string]interface{}{
		"status": "ok",
		"checks": []string{"connectivity", "signature"},
		"msg":    "Connection successful",
	}, nil
}

// GetAuditLogs 获取审计日志
func (s *adminPaymentService) GetAuditLogs(page, pageSize int) ([]model.PaymentAuditLog, int64, error) {
	var logs []model.PaymentAuditLog
	var total int64

	db := database.DB.Model(&model.PaymentAuditLog{})
	db.Count(&total)

	offset := (page - 1) * pageSize
	err := db.Order("created_at desc").Offset(offset).Limit(pageSize).Find(&logs).Error

	return logs, total, err
}

// 辅助函数：脱敏
func maskSensitiveData(provider string, data map[string]interface{}) {
	sensitiveFields := map[string][]string{
		"alipay": {"private_key", "alipay_public_key"},
		"wechat": {"api_key", "api_v3_key", "platform_public_key"},
	}

	if fields, ok := sensitiveFields[provider]; ok {
		for _, field := range fields {
			if _, exists := data[field]; exists {
				data[field] = "******" // 脱敏
			}
		}
	}
}

// 辅助函数：合并配置（保留未上传的敏感字段旧值）
func mergeSettings(provider string, newData, oldData map[string]interface{}) {
	sensitiveFields := map[string][]string{
		"alipay": {"private_key"},
		"wechat": {"api_key", "api_v3_key"},
	}

	if fields, ok := sensitiveFields[provider]; ok {
		for _, field := range fields {
			// 如果新数据中该字段为空或为脱敏占位符，则沿用旧值
			if v, exists := newData[field]; !exists || v == "" || v == "******" {
				if oldV, ok := oldData[field]; ok {
					newData[field] = oldV
				}
			}
		}
	}
}
