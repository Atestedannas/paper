package database

import "gorm.io/gorm"

type Migration20260713AdminPaperListIndexes struct{}

func (m *Migration20260713AdminPaperListIndexes) Name() string {
	return "20260713_admin_paper_list_indexes"
}

func (m *Migration20260713AdminPaperListIndexes) Up(tx *gorm.DB) error {
	if err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_papers_admin_order
		ON papers(created_at DESC, id DESC)
	`).Error; err != nil {
		return err
	}
	if err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_papers_admin_list
		ON papers(deleted_at, created_at DESC, id DESC)
	`).Error; err != nil {
		return err
	}
	return tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_check_results_paper_latest
		ON check_results(paper_id, created_at DESC, id DESC)
	`).Error
}

func (m *Migration20260713AdminPaperListIndexes) Down(tx *gorm.DB) error {
	if err := tx.Exec(`DROP INDEX IF EXISTS idx_check_results_paper_latest`).Error; err != nil {
		return err
	}
	if err := tx.Exec(`DROP INDEX IF EXISTS idx_papers_admin_list`).Error; err != nil {
		return err
	}
	return tx.Exec(`DROP INDEX IF EXISTS idx_papers_admin_order`).Error
}
