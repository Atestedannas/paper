package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// Menu 菜单模型（菜单即权限）
type Menu struct {
	ID         uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ParentID   *uuid.UUID     `gorm:"type:uuid;index" json:"parent_id"`
	Name       string         `gorm:"size:50;not null" json:"name"`            // 菜单名称（唯一标识）
	Title      string         `gorm:"size:100;not null" json:"title"`          // 菜单标题（显示名称）
	Icon       string         `gorm:"size:50" json:"icon"`                     // 菜单图标
	Path       string         `gorm:"size:255;not null" json:"path"`           // 路由路径
	Component  string         `gorm:"size:255" json:"component"`               // 组件路径
	SortOrder  int            `gorm:"default:0" json:"sort_order"`             // 排序顺序
	MenuType   string         `gorm:"size:20;default:'menu'" json:"menu_type"` // 菜单类型：menu(菜单)/button(按钮)/api(接口)
	Permission string         `gorm:"size:100;index" json:"permission"`        // 权限标识（如：user:list, user:create）
	Visible    bool           `gorm:"default:true" json:"visible"`             // 是否可见
	KeepAlive  bool           `gorm:"default:false" json:"keep_alive"`         // 是否缓存
	Redirect   string         `gorm:"size:255" json:"redirect"`                // 重定向地址
	Meta       datatypes.JSON `gorm:"type:jsonb" json:"meta"`                  // 额外元数据
	Roles      []Role         `gorm:"many2many:role_menus;" json:"roles"`      // 关联角色
	Parent     *Menu          `gorm:"foreignKey:ParentID" json:"parent"`
	Children   []Menu         `gorm:"foreignKey:ParentID" json:"children"`
	CreatedAt  time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// TableName 指定表名
func (Menu) TableName() string {
	return "menus"
}

// MenuCreateRequest 创建菜单请求
type MenuCreateRequest struct {
	ParentID   *uuid.UUID             `json:"parent_id"`
	Name       string                 `json:"name" binding:"required"`
	Title      string                 `json:"title" binding:"required"`
	Icon       string                 `json:"icon"`
	Path       string                 `json:"path" binding:"required"`
	Component  string                 `json:"component"`
	SortOrder  int                    `json:"sort_order"`
	MenuType   string                 `json:"menu_type"`
	Permission string                 `json:"permission"`
	Visible    bool                   `json:"visible"`
	KeepAlive  bool                   `json:"keep_alive"`
	Redirect   string                 `json:"redirect"`
	Meta       map[string]interface{} `json:"meta"`
}

// MenuUpdateRequest 更新菜单请求
type MenuUpdateRequest struct {
	ParentID   *uuid.UUID             `json:"parent_id"`
	Name       string                 `json:"name"`
	Title      string                 `json:"title"`
	Icon       string                 `json:"icon"`
	Path       string                 `json:"path"`
	Component  string                 `json:"component"`
	SortOrder  int                    `json:"sort_order"`
	MenuType   string                 `json:"menu_type"`
	Permission string                 `json:"permission"`
	Visible    bool                   `json:"visible"`
	KeepAlive  bool                   `json:"keep_alive"`
	Redirect   string                 `json:"redirect"`
	Meta       map[string]interface{} `json:"meta"`
}

// MenuTreeResponse 菜单树响应
type MenuTreeResponse struct {
	ID         uuid.UUID          `json:"id"`
	ParentID   *uuid.UUID         `json:"parent_id"`
	Name       string             `json:"name"`
	Title      string             `json:"title"`
	Icon       string             `json:"icon"`
	Path       string             `json:"path"`
	Component  string             `json:"component"`
	SortOrder  int                `json:"sort_order"`
	MenuType   string             `json:"menu_type"`
	Permission string             `json:"permission"`
	Visible    bool               `json:"visible"`
	KeepAlive  bool               `json:"keep_alive"`
	Redirect   string             `json:"redirect"`
	Meta       datatypes.JSON     `json:"meta"`
	Children   []MenuTreeResponse `json:"children"`
}
