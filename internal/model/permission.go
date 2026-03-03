package model

import (
	"time"

	"github.com/google/uuid"
)

// Permission 权限模型
type Permission struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Name         string    `gorm:"size:100;not null" json:"name"`             // 权限名称
	Code         string    `gorm:"size:100;uniqueIndex;not null" json:"code"` // 权限代码
	ResourceType string    `gorm:"size:50;default:api" json:"resource_type"`  // 资源类型：api(接口)/menu(菜单)/button(按钮)
	Method       string    `gorm:"size:10;default:GET" json:"method"`         // HTTP方法：GET/POST/PUT/DELETE等
	Path         string    `gorm:"size:200" json:"path"`                      // 资源路径，如：/api/v1/users
	Description  string    `gorm:"size:200" json:"description"`               // 权限描述

	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`

	// 关联关系
	Roles []Role `gorm:"many2many:role_permissions;" json:"roles,omitempty"`
}

// TableName 指定表名
func (Permission) TableName() string {
	return "permissions"
}
