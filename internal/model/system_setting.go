package model

import "time"

// SystemSetting 系统全局设置
type SystemSetting struct {
	Key         string    `gorm:"primaryKey;size:50" json:"key"`
	Value       string    `gorm:"type:text" json:"value"`
	Description string    `gorm:"size:255" json:"description"`
	IsSecret    bool      `gorm:"default:false" json:"is_secret"` // 是否敏感数据（前端不显示明文）
	CreatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}
