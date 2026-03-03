package database

import (
	"fmt"
	"log"

	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// Migration20250129DropBadConstraint 强制删除错误的 fk_format_templates_check_results 约束
type Migration20250129DropBadConstraint struct{}

func (m *Migration20250129DropBadConstraint) Name() string {
	return "20250129_drop_bad_constraint"
}

func (m *Migration20250129DropBadConstraint) Up(tx *gorm.DB) error {
	log.Println("Scanning for and dropping fk_format_templates_check_results constraint...")

	// 检查 format_templates 表是否存在名为 fk_format_templates_check_results 的约束
	// 这是一个具体的、错误的约束名，通常由 AutoMigrate 或早期手动 SQL 产生
	var count int64
	tx.Raw(`
		SELECT count(*)
		FROM pg_constraint c
		JOIN pg_class t ON c.conrelid = t.oid
		WHERE t.relname = 'format_templates' AND c.conname = 'fk_format_templates_check_results'
	`).Scan(&count)

	if count > 0 {
		log.Println("Found erroneous constraint fk_format_templates_check_results on format_templates, dropping it...")
		if err := tx.Exec(`ALTER TABLE format_templates DROP CONSTRAINT fk_format_templates_check_results`).Error; err != nil {
			return fmt.Errorf("failed to drop constraint fk_format_templates_check_results: %w", err)
		}
	} else {
		log.Println("Constraint fk_format_templates_check_results not found on format_templates (this is good)")
	}

	// 为了保险起见，同时也检查是否有 check_result_id 列在 format_templates 中
	// 如果有，说明结构定义曾经错误，需要清理
	if tx.Migrator().HasColumn(&model.FormatTemplate{}, "check_result_id") {
		log.Println("Found check_result_id column in format_templates, dropping it...")
		if err := tx.Migrator().DropColumn(&model.FormatTemplate{}, "check_result_id"); err != nil {
			return fmt.Errorf("failed to drop column check_result_id: %w", err)
		}
	}

	return nil
}

func (m *Migration20250129DropBadConstraint) Down(tx *gorm.DB) error {
	// 这是一个修复性迁移，不提供回滚
	return nil
}
