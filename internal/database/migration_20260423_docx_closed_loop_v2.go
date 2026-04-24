package database

import (
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

type Migration20260423CreateDocxClosedLoopV2Tables struct{}

func (m *Migration20260423CreateDocxClosedLoopV2Tables) Name() string {
	return "20260423_create_docx_closed_loop_v2_tables"
}

func (m *Migration20260423CreateDocxClosedLoopV2Tables) Up(tx *gorm.DB) error {
	if err := tx.AutoMigrate(
		&model.CompiledTemplate{},
		&model.PaperWorkflowJob{},
		&model.PaperWorkflowIssue{},
	); err != nil {
		return err
	}

	return tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_paper_workflow_jobs_status_stage
		ON paper_workflow_jobs(status, stage)
	`).Error
}

func (m *Migration20260423CreateDocxClosedLoopV2Tables) Down(tx *gorm.DB) error {
	if err := tx.Migrator().DropTable(&model.PaperWorkflowIssue{}); err != nil {
		return err
	}
	if err := tx.Migrator().DropTable(&model.PaperWorkflowJob{}); err != nil {
		return err
	}
	return tx.Migrator().DropTable(&model.CompiledTemplate{})
}
