package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// University 高校模型
type University struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"size:100;not null;uniqueIndex" json:"name"` // 高校名称
	Abbr        string    `gorm:"size:50" json:"abbr"`                       // 缩写
	Description string    `gorm:"type:text" json:"description"`              // 描述
	Color       string    `gorm:"size:50" json:"color"`                      // 颜色
	Tags        string    `gorm:"type:jsonb" json:"tags"`                    // 标签列表 JSON格式
	CreatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 虚拟字段，用于API返回
	FormatRequirements json.RawMessage `gorm:"-" json:"format_requirements"` // 格式要求JSON
	FilePath           string          `gorm:"-" json:"file_path"`           // 模板文件路径
	DocxTemplateURL    string          `gorm:"-" json:"docx_template_url"`   // DOCX模板URL
	PdfTemplateURL     string          `gorm:"-" json:"pdf_template_url"`    // PDF模板URL

	// 关联
	Templates []FormatTemplate `gorm:"foreignKey:UniversityID" json:"templates,omitempty"`
}

// FormatTemplate 格式模板模型（核心模型 - 支持从论文解析）
type FormatTemplate struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	TemplateID   string    `gorm:"size:100;uniqueIndex;not null" json:"template_id"` // 业务模板ID
	Name         string    `gorm:"size:100;not null" json:"name"`                    // 模板名称
	UniversityID *int64    `gorm:"index" json:"university_id"`                       // 所属高校ID
	DocumentType string    `gorm:"size:50" json:"document_type"`                     // 文档类型：本科论文、硕士论文、博士论文
	Subject      string    `gorm:"size:20" json:"subject"`                           // 学科类别: 文科/理科/综合
	FilePath     string    `gorm:"size:500" json:"file_path"`                        // 上传的格式要求文件路径
	Source       string    `gorm:"size:50;default:system" json:"source"`             // 来源: system, university_upload, user_upload, auto_parsed
	Version      string    `gorm:"size:20;default:1.0" json:"version"`               // 版本号
	IsPublic     bool      `gorm:"default:true" json:"is_public"`                    // 是否公开
	IsActive     bool      `gorm:"default:true" json:"is_active"`                    // 是否激活

	// 格式规范内容（JSON格式存储完整的格式规范）
	FormatRules string `gorm:"type:jsonb;not null" json:"format_rules"` // 完整的格式规范JSON

	// 解析相关字段（支持从论文自动解析生成模板）
	ParsedFromPaperID *uuid.UUID `gorm:"type:uuid" json:"parsed_from_paper_id"` // 如果是从论文解析而来，记录原论文ID
	ParseConfidence   float64    `gorm:"default:0.0" json:"parse_confidence"`   // 解析置信度 0-1

	// 统计信息
	UsageCount  int     `gorm:"default:0" json:"usage_count"`    // 使用次数
	SuccessRate float64 `gorm:"default:0.0" json:"success_rate"` // 成功率

	Description string    `gorm:"type:text" json:"description"`
	CreatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联
	University *University `gorm:"foreignKey:UniversityID" json:"university,omitempty"`
	Papers     []Paper     `gorm:"foreignKey:SelectedTemplateID" json:"papers,omitempty"`
}
