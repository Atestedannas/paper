package database

import (
	"log"

	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// Migration20260327AddGoldenTemplatePath adds golden_template_path column to format_templates
// and sets the path for 重庆人文科技学院.
type Migration20260327AddGoldenTemplatePath struct{}

func (m *Migration20260327AddGoldenTemplatePath) Name() string {
	return "20260327_add_golden_template_path"
}

func (m *Migration20260327AddGoldenTemplatePath) Up(tx *gorm.DB) error {
	if !tx.Migrator().HasColumn(&model.FormatTemplate{}, "golden_template_path") {
		log.Println("[Migration] Adding golden_template_path column to format_templates")
		if err := tx.Exec(`ALTER TABLE format_templates ADD COLUMN golden_template_path VARCHAR(500) DEFAULT ''`).Error; err != nil {
			return err
		}
	}

	// Set golden template path for 重庆人文科技学院
	result := tx.Exec(`
		UPDATE format_templates
		SET golden_template_path = 'uploads/golden_templates/cqrwst.docx'
		WHERE university_id IN (
			SELECT id FROM universities WHERE abbr = 'CQRWST' OR name = '重庆人文科技学院'
		)
	`)
	if result.Error != nil {
		return result.Error
	}
	log.Printf("[Migration] Updated %d templates with golden_template_path for CQRWST", result.RowsAffected)
	return nil
}

func (m *Migration20260327AddGoldenTemplatePath) Down(tx *gorm.DB) error {
	return tx.Exec(`ALTER TABLE format_templates DROP COLUMN IF EXISTS golden_template_path`).Error
}
