package model

import (
	"time"

	"github.com/google/uuid"
)

// PaymentSetting 支付配置模型
type PaymentSetting struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Provider  string    `gorm:"size:20;uniqueIndex;not null" json:"provider"` // alipay, wechat
	Version   int       `gorm:"default:1" json:"version"`                     // 配置版本，用于乐观锁
	Enabled   bool      `gorm:"default:false" json:"enabled"`                 // 是否启用
	Settings  string    `gorm:"type:text" json:"settings"`                    // 加密存储的配置JSON（包含敏感信息）
	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// PaymentCertificate 支付证书模型
type PaymentCertificate struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Provider    string    `gorm:"size:20;index;not null" json:"provider"`  // alipay, wechat
	Fingerprint string    `gorm:"size:100;uniqueIndex" json:"fingerprint"` // 证书指纹
	ValidFrom   time.Time `json:"valid_from"`                              // 有效期开始
	ValidTo     time.Time `json:"valid_to"`                                // 有效期结束
	Status      string    `gorm:"size:20;default:active" json:"status"`    // active, inactive, expired
	Content     string    `gorm:"type:text" json:"-"`                      // 证书内容（加密存储）
	Path        string    `gorm:"size:255" json:"path"`                    // 文件路径（可选）
	CreatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// PaymentAuditLog 支付审计日志
type PaymentAuditLog struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;index" json:"user_id"` // 操作人ID
	Provider  string    `gorm:"size:20;index" json:"provider"`  // 支付渠道
	Action    string    `gorm:"size:50" json:"action"`          // update, toggle, upload_cert
	Changes   string    `gorm:"type:text" json:"changes"`       // 变更内容（不包含敏感值）
	ClientIP  string    `gorm:"size:50" json:"client_ip"`       // 操作IP
	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}
