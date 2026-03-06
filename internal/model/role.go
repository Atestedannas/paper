package model

import (
	"time"

	"github.com/google/uuid"
)

// Role 角色模型
type Role struct {
	ID          uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Name        string     `gorm:"size:50;uniqueIndex;not null" json:"name"` // 角色名称
	Description string     `gorm:"size:200" json:"description"`              // 角色描述
	Type        string     `gorm:"size:20;default:business" json:"type"`     // 角色类型：system(系统角色)/business(业务角色)
	ParentID    *uuid.UUID `gorm:"type:uuid" json:"parent_id"`               // 父角色 ID，支持角色继承
	Code        string     `gorm:"size:50;uniqueIndex;not null" json:"code"` // 角色代码
	SortOrder   int        `gorm:"default:0" json:"sort_order"`              // 排序顺序

	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联关系
	Parent   *Role  `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Children []Role `gorm:"foreignKey:ParentID" json:"children,omitempty"`
	Users    []User `gorm:"many2many:user_roles;" json:"users,omitempty"`
	Menus    []Menu `gorm:"many2many:role_menus;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"menus,omitempty"` // 菜单即权限
}

// TableName 指定表名
func (Role) TableName() string {
	return "roles"
}
