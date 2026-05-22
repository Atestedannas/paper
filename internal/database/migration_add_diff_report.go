package database

import (
	"log"

	"gorm.io/gorm"
)

// Migration20260327AddDiffReport 为 check_results 表增加 diff_report 字段
type Migration20260327AddDiffReport struct{}

func (m *Migration20260327AddDiffReport) Name() string {
	return "20260327_add_diff_report"
}

func (m *Migration20260327AddDiffReport) Up(tx *gorm.DB) error {
	log.Println("Adding diff_report column to check_results table")
	return tx.Exec(`
		ALTER TABLE check_results
		ADD COLUMN IF NOT EXISTS diff_report jsonb DEFAULT '{}'::jsonb
	`).Error
}

func (m *Migration20260327AddDiffReport) Down(tx *gorm.DB) error {
	return tx.Exec(`ALTER TABLE check_results DROP COLUMN IF EXISTS diff_report`).Error
}
