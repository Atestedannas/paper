package database

import "gorm.io/gorm"

// Migration20260815FixCQIEPageSetup preserves the written CQIE requirements
// when a user-registered template sample carries different page distances.
// It only touches the named CQIE template and leaves all other rule fields
// (including its learned role styles) intact.
type Migration20260815FixCQIEPageSetup struct{}

func (m *Migration20260815FixCQIEPageSetup) Name() string {
	return "20260815_fix_cqie_page_setup_written_requirements"
}

func (m *Migration20260815FixCQIEPageSetup) Up(tx *gorm.DB) error {
	return tx.Exec(`
		UPDATE format_templates
		SET format_rules = jsonb_set(
			jsonb_set(format_rules, '{page_setup,header_margin_twips}', to_jsonb('907'::text), true),
			'{page_setup,footer_margin_twips}', to_jsonb('1191'::text), true
		), updated_at = NOW()
		WHERE name = '重庆工程学院本科论文格式标准'
	`).Error
}

func (m *Migration20260815FixCQIEPageSetup) Down(tx *gorm.DB) error {
	return nil
}
