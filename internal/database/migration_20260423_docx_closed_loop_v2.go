package database

import (
	"fmt"

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
	if err := recreateForeignKeyWithDeleteRule(
		tx,
		"paper_workflow_jobs",
		"paper_id",
		"fk_paper_workflow_jobs_paper",
		"papers",
		"id",
		"CASCADE",
	); err != nil {
		return err
	}
	if err := recreateForeignKeyWithDeleteRule(
		tx,
		"paper_workflow_jobs",
		"user_id",
		"fk_paper_workflow_jobs_user",
		"users",
		"id",
		"CASCADE",
	); err != nil {
		return err
	}
	if err := recreateForeignKeyWithDeleteRule(
		tx,
		"paper_workflow_issues",
		"job_id",
		"fk_paper_workflow_issues_job",
		"paper_workflow_jobs",
		"id",
		"CASCADE",
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

func recreateForeignKeyWithDeleteRule(
	tx *gorm.DB,
	tableName string,
	columnName string,
	constraintName string,
	referencedTable string,
	referencedColumn string,
	deleteRule string,
) error {
	var existingConstraintNames []string
	if err := tx.Raw(`
		SELECT tc.constraint_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name
		 AND tc.table_schema = kcu.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_schema = current_schema()
		  AND tc.table_name = ?
		  AND kcu.column_name = ?
	`, tableName, columnName).Scan(&existingConstraintNames).Error; err != nil {
		return err
	}

	for _, existingConstraintName := range existingConstraintNames {
		if err := tx.Exec(fmt.Sprintf(
			`ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s`,
			tableName,
			existingConstraintName,
		)).Error; err != nil {
			return err
		}
	}

	return tx.Exec(fmt.Sprintf(
		`ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s) ON UPDATE CASCADE ON DELETE %s`,
		tableName,
		constraintName,
		columnName,
		referencedTable,
		referencedColumn,
		deleteRule,
	)).Error
}
