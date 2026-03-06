package model

import (
	"time"
)

// CasbinRule Casbin 规则模型
type CasbinRule struct {
	ID        int       `gorm:"primaryKey;autoIncrement" json:"id"`
	PType     string    `gorm:"size:20" json:"p_type"`
	V0        string    `gorm:"size:100" json:"v0"`
	V1        string    `gorm:"size:100" json:"v1"`
	V2        string    `gorm:"size:100" json:"v2"`
	V3        string    `gorm:"size:100" json:"v3"`
	V4        string    `gorm:"size:100" json:"v4"`
	V5        string    `gorm:"size:100" json:"v5"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定表名
func (CasbinRule) TableName() string {
	return "casbin_rules"
}

// CasbinPolicyRequest Casbin 策略请求
type CasbinPolicyRequest struct {
	PType string `json:"p_type" binding:"required"`
	V0    string `json:"v0" binding:"required"` // 角色/用户
	V1    string `json:"v1" binding:"required"` // 资源
	V2    string `json:"v2" binding:"required"` // 动作
	V3    string `json:"v3"`
	V4    string `json:"v4"`
	V5    string `json:"v5"`
}

// CasbinEnforceRequest Casbin 权限检查请求
type CasbinEnforceRequest struct {
	Sub string `json:"sub" binding:"required"` // 主体（用户 ID 或角色）
	Obj string `json:"obj" binding:"required"` // 对象（资源路径）
	Act string `json:"act" binding:"required"` // 动作（HTTP 方法）
}

// CasbinEnforceResponse Casbin 权限检查响应
type CasbinEnforceResponse struct {
	Allowed bool   `json:"allowed"`
	Message string `json:"message"`
}
