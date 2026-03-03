package database

import (
	"fmt"
	"log"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Migration20250129AddTemplateIDToFormatTemplates 为 format_templates 表添加 template_id 列
type Migration20250129AddTemplateIDToFormatTemplates struct{}

func (m *Migration20250129AddTemplateIDToFormatTemplates) Name() string {
	return "20250129_add_template_id_to_format_templates"
}

func (m *Migration20250129AddTemplateIDToFormatTemplates) Up(tx *gorm.DB) error {
	// 1. 检查列是否存在
	if tx.Migrator().HasColumn("format_templates", "template_id") {
		log.Println("Column template_id already exists in format_templates")
		return nil
	}

	log.Println("Adding column template_id to format_templates...")

	// 2. 添加列 (先允许 NULL)
	if err := tx.Exec(`ALTER TABLE format_templates ADD COLUMN template_id VARCHAR(100)`).Error; err != nil {
		return fmt.Errorf("failed to add column template_id: %w", err)
	}

	// 3. 为现有数据生成 template_id
	// 获取所有 ID
	var ids []uuid.UUID
	tx.Table("format_templates").Pluck("id", &ids)

	for _, id := range ids {
		newTemplateID := uuid.New().String()
		if err := tx.Exec(`UPDATE format_templates SET template_id = ? WHERE id = ?`, newTemplateID, id).Error; err != nil {
			return fmt.Errorf("failed to update template_id for row %s: %w", id, err)
		}
	}

	// 4. 设置为 NOT NULL
	if err := tx.Exec(`ALTER TABLE format_templates ALTER COLUMN template_id SET NOT NULL`).Error; err != nil {
		return fmt.Errorf("failed to set template_id to NOT NULL: %w", err)
	}

	// 5. 添加唯一索引
	if err := tx.Exec(`CREATE UNIQUE INDEX idx_format_templates_template_id ON format_templates(template_id)`).Error; err != nil {
		return fmt.Errorf("failed to create unique index on template_id: %w", err)
	}

	return nil
}

func (m *Migration20250129AddTemplateIDToFormatTemplates) Down(tx *gorm.DB) error {
	// 撤销操作：删除列
	return tx.Exec(`ALTER TABLE format_templates DROP COLUMN IF EXISTS template_id`).Error
}
