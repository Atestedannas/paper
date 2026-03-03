package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/utils"
)

const (
	KeyEncryptionKey = "security_encryption_key"
	KeyPaymentConfig = "payment_config"
	DefaultDevKey    = "this-is-a-default-32-byte-key-12" // 32 bytes exactly
)

type SystemSettingService interface {
	GetSetting(key string) (string, error)
	GetEncryptionKey() string
	UpdateEncryptionKey(newKey string) error
	GetPaymentConfig() (map[string]interface{}, error)
	UpdatePaymentConfig(isCheckFree *bool, formatCheck, formatFix *float64) error
	GetPaymentProviderSettings(provider string) (map[string]interface{}, error)
	UpdatePaymentProviderSettings(provider string, settings map[string]interface{}) error
}

type systemSettingService struct {
	keyCache string
	mu       sync.RWMutex
}

var (
	instance *systemSettingService
	once     sync.Once
)

// GetSystemSettingService 单例模式获取服务
func GetSystemSettingService() SystemSettingService {
	once.Do(func() {
		instance = &systemSettingService{}
	})
	return instance
}

func (s *systemSettingService) GetSetting(key string) (string, error) {
	var setting model.SystemSetting
	if err := database.DB.Where("key = ?", key).First(&setting).Error; err != nil {
		return "", err
	}
	return setting.Value, nil
}

// GetEncryptionKey 获取加密密钥（优先缓存，其次数据库，最后默认值）
func (s *systemSettingService) GetEncryptionKey() string {
	s.mu.RLock()
	if s.keyCache != "" {
		defer s.mu.RUnlock()
		return s.keyCache
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// 双重检查
	if s.keyCache != "" {
		return s.keyCache
	}

	val, err := s.GetSetting(KeyEncryptionKey)
	if err != nil || val == "" {
		// 数据库无记录，返回默认值
		// 注意：实际生产中，管理员应尽快设置
		return DefaultDevKey
	}

	s.keyCache = val
	return s.keyCache
}

// UpdateEncryptionKey 更新加密密钥（包含重加密逻辑）
func (s *systemSettingService) UpdateEncryptionKey(newKey string) error {
	if len(newKey) != 32 {
		return errors.New("encryption key must be 32 bytes long")
	}

	oldKey := s.GetEncryptionKey()
	if oldKey == newKey {
		return nil
	}

	// 开启事务
	tx := database.DB.Begin()

	// 1. 获取所有 PaymentSetting
	var paymentSettings []model.PaymentSetting
	if err := tx.Find(&paymentSettings).Error; err != nil {
		tx.Rollback()
		return err
	}

	// 2. 重加密 PaymentSetting
	for _, ps := range paymentSettings {
		// 解密
		decrypted, err := utils.Decrypt(ps.Settings, []byte(oldKey))
		if err != nil {
			// 如果解密失败，可能是本身就是明文或者密钥错误，这里选择跳过或报错
			// 为健壮性，记录日志并跳过，或者报错回滚
			// 这里假设数据一致性很重要，报错
			tx.Rollback()
			return fmt.Errorf("failed to decrypt payment setting %s: %w", ps.Provider, err)
		}
		// 加密
		encrypted, err := utils.Encrypt(decrypted, []byte(newKey))
		if err != nil {
			tx.Rollback()
			return err
		}
		// 更新
		if err := tx.Model(&ps).Update("settings", encrypted).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	// 3. 获取所有 PaymentCertificate
	var certs []model.PaymentCertificate
	if err := tx.Find(&certs).Error; err != nil {
		tx.Rollback()
		return err
	}

	// 4. 重加密 PaymentCertificate
	for _, cert := range certs {
		decrypted, err := utils.Decrypt(cert.Content, []byte(oldKey))
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to decrypt certificate %s: %w", cert.Fingerprint, err)
		}
		encrypted, err := utils.Encrypt(decrypted, []byte(newKey))
		if err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Model(&cert).Update("content", encrypted).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	// 5. 更新 SystemSetting
	setting := model.SystemSetting{
		Key:         KeyEncryptionKey,
		Value:       newKey,
		Description: "System global encryption key (AES-256)",
		IsSecret:    true,
	}
	// Upsert
	if err := tx.Save(&setting).Error; err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit().Error; err != nil {
		return err
	}

	// 更新缓存
	s.mu.Lock()
	s.keyCache = newKey
	s.mu.Unlock()

	return nil
}

// GetPaymentConfig 获取支付策略配置
func (s *systemSettingService) GetPaymentConfig() (map[string]interface{}, error) {
	val, err := s.GetSetting(KeyPaymentConfig)
	if err != nil {
		// 如果数据库没有配置，返回默认值
		return map[string]interface{}{
			"is_check_free": true,
			"format_check":  10.0,
			"format_fix":    15.0,
		}, nil
	}

	var config map[string]interface{}
	if err := json.Unmarshal([]byte(val), &config); err != nil {
		return nil, err
	}
	return config, nil
}

// UpdatePaymentConfig 更新支付策略配置
func (s *systemSettingService) UpdatePaymentConfig(isCheckFree *bool, formatCheck, formatFix *float64) error {
	// 1. 获取现有配置
	currentConfig, _ := s.GetPaymentConfig()

	// 2. 更新字段
	if isCheckFree != nil {
		currentConfig["is_check_free"] = *isCheckFree
	}
	if formatCheck != nil {
		currentConfig["format_check"] = *formatCheck
	}
	if formatFix != nil {
		currentConfig["format_fix"] = *formatFix
	}

	// 3. 保存
	configJSON, _ := json.Marshal(currentConfig)

	setting := model.SystemSetting{
		Key:         KeyPaymentConfig,
		Value:       string(configJSON),
		Description: "Global payment strategy configuration",
		IsSecret:    false,
	}

	return database.DB.Save(&setting).Error
}

// GetPaymentProviderSettings 获取指定渠道的支付配置
func (s *systemSettingService) GetPaymentProviderSettings(provider string) (map[string]interface{}, error) {
	var ps model.PaymentSetting
	if err := database.DB.Where("provider = ?", provider).First(&ps).Error; err != nil {
		return nil, nil // 不存在时不报错，返回nil
	}

	// 解密配置
	key := s.GetEncryptionKey()
	decryptedJSON, err := utils.Decrypt(ps.Settings, []byte(key))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt settings: %w", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal([]byte(decryptedJSON), &settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	// 脱敏敏感字段
	sensitiveFields := []string{"private_key", "api_key", "alipay_public_key", "platform_public_key"}
	for _, field := range sensitiveFields {
		if val, ok := settings[field].(string); ok && val != "" {
			if len(val) > 8 {
				settings[field] = val[:4] + "******" + val[len(val)-4:]
			} else {
				settings[field] = "******"
			}
		}
	}

	return settings, nil
}

// UpdatePaymentProviderSettings 更新指定渠道的支付配置
func (s *systemSettingService) UpdatePaymentProviderSettings(provider string, settings map[string]interface{}) error {
	// 1. 获取现有配置（如果存在）用于合并（主要是处理前端传空值表示不修改的情况）
	var ps model.PaymentSetting
	exists := true
	if err := database.DB.Where("provider = ?", provider).First(&ps).Error; err != nil {
		exists = false
	}

	var currentSettings map[string]interface{}
	key := s.GetEncryptionKey()

	if exists {
		decryptedJSON, err := utils.Decrypt(ps.Settings, []byte(key))
		if err == nil {
			json.Unmarshal([]byte(decryptedJSON), &currentSettings)
		}
	}

	if currentSettings == nil {
		currentSettings = make(map[string]interface{})
	}

	// 2. 合并配置
	for k, v := range settings {
		// 如果值为空字符串，且原配置中有值，说明用户没有修改敏感字段（前端通常不回传masked的值，或者回传空）
		// 但前端逻辑是：如果不修改，就不传或者传空？
		// 查看前端代码：updatePaymentSettings直接传了 alipay.privateKey。
		// 如果前端显示的是 mask 的值，用户直接保存，会将 mask 的值传回来。
		// 所以前端在提交前应该判断：如果包含 ****** 则不提交该字段。
		// 但前端代码很简单：updatePaymentSettings('alipay', { app_id: ..., private_key: ... })
		// 如果前端回显了 "1234******5678"，那么提交的就是这个字符串。
		// 这会导致密钥被破坏。
		// 需要确认前端逻辑。前端 PaymentSettingsView.vue loadSettings 中：
		// // 私钥与公钥不做前端持久化与回显 -> 注释说不做回显，所以 alipay.privateKey 是空的。
		// 如果 alipay.privateKey 是空的，前端提交时会传空字符串。
		// 所以逻辑应该是：如果传入的值为空字符串，则保留原值。
		strVal, ok := v.(string)
		if ok && strVal == "" {
			continue
		}
		// 如果传入的是 masked 值，也跳过（防止误操作）
		if ok && len(strVal) >= 6 && strVal[4:6] == "**" {
			continue
		}

		currentSettings[k] = v
	}

	// 3. 加密保存
	settingsJSON, err := json.Marshal(currentSettings)
	if err != nil {
		return err
	}

	encryptedSettings, err := utils.Encrypt(string(settingsJSON), []byte(key))
	if err != nil {
		return err
	}

	if exists {
		ps.Settings = encryptedSettings
		ps.Version++ // 乐观锁简单实现
		return database.DB.Save(&ps).Error
	} else {
		newPs := model.PaymentSetting{
			Provider: provider,
			Version:  1,
			Enabled:  true, // 默认启用
			Settings: encryptedSettings,
		}
		return database.DB.Create(&newPs).Error
	}
}
