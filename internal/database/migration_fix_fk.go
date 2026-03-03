package database

import (
	"fmt"
	"log"

	"gorm.io/gorm"
)

// Migration20250129FixFKCheckResultsTemplate 修复 fk_check_results_template 外键约束
type Migration20250129FixFKCheckResultsTemplate struct{}

func (m *Migration20250129FixFKCheckResultsTemplate) Name() string {
	return "20250129_fix_fk_check_results_template"
}

func (m *Migration20250129FixFKCheckResultsTemplate) Up(tx *gorm.DB) error {
	// 1. 检查 format_templates 表是否存在错误的约束
	var count int64
	tx.Raw(`
		SELECT count(*)
		FROM pg_constraint c
		JOIN pg_class t ON c.conrelid = t.oid
		WHERE t.relname = 'format_templates' AND c.conname = 'fk_check_results_template'
	`).Scan(&count)

	if count > 0 {
		log.Println("Found erroneous constraint fk_check_results_template on format_templates, dropping it...")
		if err := tx.Exec(`ALTER TABLE format_templates DROP CONSTRAINT fk_check_results_template`).Error; err != nil {
			return fmt.Errorf("failed to drop constraint on format_templates: %w", err)
		}
	}

	// 2. 检查 check_results 表是否存在正确的约束
	tx.Raw(`
		SELECT count(*)
		FROM pg_constraint c
		JOIN pg_class t ON c.conrelid = t.oid
		WHERE t.relname = 'check_results' AND c.conname = 'fk_check_results_template'
	`).Scan(&count)

	if count == 0 {
		log.Println("Adding constraint fk_check_results_template to check_results...")

		if err := tx.Exec(`
			ALTER TABLE check_results 
			ADD CONSTRAINT fk_check_results_template 
			FOREIGN KEY (template_id) 
			REFERENCES format_templates(id) 
			ON DELETE RESTRICT ON UPDATE CASCADE
		`).Error; err != nil {
			return fmt.Errorf("failed to add constraint on check_results: %w", err)
		}
	} else {
		log.Println("Constraint fk_check_results_template already exists on check_results")
	}

	return nil
}

func (m *Migration20250129FixFKCheckResultsTemplate) Down(tx *gorm.DB) error {
	// 撤销操作：删除 check_results 上的约束
	return tx.Exec(`ALTER TABLE check_results DROP CONSTRAINT IF EXISTS fk_check_results_template`).Error
}
