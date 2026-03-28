package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Paper 论文模型（支持模板解析和选择）
type Paper struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	UserID      uuid.UUID `gorm:"type:uuid;index;not null" json:"user_id"`
	Title       string    `gorm:"size:255;not null" json:"title"`
	Description string    `gorm:"type:text" json:"description"`
	FilePath    string    `gorm:"size:255;not null" json:"file_path"` // 文件存储路径
	FileName    string    `gorm:"size:255;not null" json:"file_name"` // 原始文件名
	FileSize    int64     `gorm:"not null" json:"file_size"`          // 文件大小（字节）
	FileType    string    `gorm:"size:20;not null" json:"file_type"`  // 文件类型，如 "pdf", "docx"

	// 模板选择（用户选择的格式模板）
	SelectedTemplateID *uuid.UUID `gorm:"type:uuid;index" json:"selected_template_id"`

	// 修正后的文件
	CorrectedFilePath string `gorm:"size:255" json:"corrected_file_path"` // 修正后的文件路径

	// 解析信息（系统自动解析的信息）
	ParsedInfo            string `gorm:"type:jsonb" json:"parsed_info"`             // 解析出的论文结构信息
	AutoDetectedTemplates string `gorm:"type:jsonb" json:"auto_detected_templates"` // 自动检测到的可能模板列表

	Status    string         `gorm:"size:20;default:uploaded" json:"status"` // uploaded, parsed, template_selected, checked, corrected
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`      // 软删除标记
	CreatedAt time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联
	User             User            `gorm:"foreignKey:UserID" json:"user,omitempty"`
	SelectedTemplate *FormatTemplate `gorm:"foreignKey:SelectedTemplateID" json:"selected_template,omitempty"`
	CheckResults     []CheckResult   `gorm:"foreignKey:PaperID" json:"check_results,omitempty"`
}

// BeforeCreate GORM hook to set default JSON values
func (p *Paper) BeforeCreate(tx *gorm.DB) (err error) {
	if p.ParsedInfo == "" {
		p.ParsedInfo = "{}"
	}
	if p.AutoDetectedTemplates == "" {
		p.AutoDetectedTemplates = "[]"
	}
	return
}

// CheckResult 格式检查结果模型（支持差异对比）
type CheckResult struct {
	ID               uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	PaperID          uuid.UUID `gorm:"type:uuid;index;not null" json:"paper_id"`
	UserID           uuid.UUID `gorm:"type:uuid;index;not null" json:"user_id"`
	TemplateID       uuid.UUID `gorm:"type:uuid;index;not null;column:template_id" json:"template_id"`               // 使用的模板ID
	FormatTemplateID uuid.UUID `gorm:"type:uuid;index;not null;column:format_template_id" json:"format_template_id"` // 冗余字段，解决数据库双重列约束问题

	// 检查结果统计
	TotalIssues  int `gorm:"not null;default:0" json:"total_issues"`
	ErrorCount   int `gorm:"not null;default:0" json:"error_count"`
	WarningCount int `gorm:"not null;default:0" json:"warning_count"`
	InfoCount    int `gorm:"not null;default:0" json:"info_count"`

	// 详细结果（JSON格式存储）
	Issues      string `gorm:"type:jsonb" json:"issues"`                   // 问题详情JSON
	Differences string `gorm:"type:jsonb" json:"differences"`              // 格式差异JSON（用于生成差异对比）
	DiffReport  string `gorm:"type:jsonb;default:'{}'" json:"diff_report"` // OOXML全量属性差异报告

	Status    string    `gorm:"size:20;default:pending" json:"status"` // pending, processing, completed, failed
	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联
	Paper       Paper              `gorm:"foreignKey:PaperID" json:"paper,omitempty"`
	User        User               `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Template    FormatTemplate     `gorm:"foreignKey:TemplateID" json:"template,omitempty"`
	Corrections []FormatCorrection `gorm:"foreignKey:CheckResultID" json:"corrections,omitempty"`
}

// FormatCorrection 格式修正记录模型（支持批量修正）
type FormatCorrection struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	CheckResultID  uuid.UUID `gorm:"type:uuid;index;not null" json:"check_result_id"`
	IssueID        string    `gorm:"size:100;not null" json:"issue_id"`       // 问题唯一标识
	CorrectionType string    `gorm:"size:50;not null" json:"correction_type"` // 修正类型，如 "heading", "paragraph"

	// 修正内容（JSON格式存储）
	OriginalContent  string `gorm:"type:jsonb" json:"original_content"`  // 原始内容
	CorrectedContent string `gorm:"type:jsonb" json:"corrected_content"` // 修正后内容

	// 位置和状态
	Location   string  `gorm:"type:jsonb" json:"location"`      // 位置信息（页码、行号等）
	IsApplied  bool    `gorm:"default:false" json:"is_applied"` // 是否已应用
	Confidence float64 `gorm:"default:1.0" json:"confidence"`   // 修正置信度

	Description string    `gorm:"type:text" json:"description"`
	CreatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联
	CheckResult CheckResult `gorm:"foreignKey:CheckResultID" json:"check_result,omitempty"`
}

// 删除重复的FormatStandard和FormatCheck模型，统一使用FormatTemplate
