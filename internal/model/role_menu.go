package model

import (
	"time"

	"github.com/google/uuid"
)

// RoleMenu 角色菜单关联表（多对多）
type RoleMenu struct {
	RoleID uuid.UUID `gorm:"type:uuid;primaryKey;not null"`
	MenuID uuid.UUID `gorm:"type:uuid;primaryKey;not null"`
	// 关联关系
	Role *Role `gorm:"foreignKey:RoleID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	Menu *Menu `gorm:"foreignKey:MenuID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	// 审计字段
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (RoleMenu) TableName() string {
	return "role_menus"
}
