package model

import (
	"time"

	"github.com/google/uuid"
)

// Authority 权限模型
type Authority struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Name         string    `gorm:"size:100" json:"name"`
	Code         string    `gorm:"size:100;uniqueIndex" json:"code"`
	Type         string    `gorm:"size:20;default:'api'" json:"type"`
	ResourceType string    `gorm:"size:50" json:"resource_type"`
	ResourcePath string    `gorm:"size:255" json:"resource_path"`
	HTTPMethod   string    `gorm:"size:10" json:"http_method"`
	Description  string    `gorm:"type:text" json:"description"`
	Roles        []Role    `gorm:"many2many:role_authorities;" json:"roles"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName 指定表名
func (Authority) TableName() string {
	return "authorities"
}

// AuthorityCreateRequest 创建权限请求
type AuthorityCreateRequest struct {
	Name         string `json:"name" binding:"required"`
	Code         string `json:"code" binding:"required"`
	Type         string `json:"type"`
	ResourceType string `json:"resource_type"`
	ResourcePath string `json:"resource_path"`
	HTTPMethod   string `json:"http_method"`
	Description  string `json:"description"`
}

// AuthorityUpdateRequest 更新权限请求
type AuthorityUpdateRequest struct {
	Name         string `json:"name"`
	Code         string `json:"code"`
	Type         string `json:"type"`
	ResourceType string `json:"resource_type"`
	ResourcePath string `json:"resource_path"`
	HTTPMethod   string `json:"http_method"`
	Description  string `json:"description"`
}

// AuthorityResponse 权限响应
type AuthorityResponse struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Code         string    `json:"code"`
	Type         string    `json:"type"`
	ResourceType string    `json:"resource_type"`
	ResourcePath string    `json:"resource_path"`
	HTTPMethod   string    `json:"http_method"`
	Description  string    `json:"description"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
