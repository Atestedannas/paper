package database

import (
	"log"

	"gorm.io/gorm"
)

// Migration20260312FixCQWLMargins 修正重庆文理学院模板左右页边距 3.17 -> 3.18
type Migration20260312FixCQWLMargins struct{}

func (m *Migration20260312FixCQWLMargins) Name() string {
	return "20260312_fix_cqwl_margins_3_17_to_3_18"
}

func (m *Migration20260312FixCQWLMargins) Up(tx *gorm.DB) error {
	log.Println("修正重庆文理学院模板页边距：3.17cm → 3.18cm")

	// 使用 jsonb_set 精确更新 format_rules 中的 margin_left 和 margin_right
	result := tx.Exec(`
		UPDATE format_templates
		SET format_rules = jsonb_set(
			jsonb_set(
				format_rules::jsonb,
				'{page_setup,margin_left}',
				'3.18'
			),
			'{page_setup,margin_right}',
			'3.18'
		)
		WHERE
			name ILIKE '%重庆文理学院%'
			AND (format_rules::jsonb -> 'page_setup' ->> 'margin_left')::numeric = 3.17
	`)
	if result.Error != nil {
		return result.Error
	}
	log.Printf("已更新 %d 条重庆文理学院模板记录", result.RowsAffected)
	return nil
}

func (m *Migration20260312FixCQWLMargins) Down(tx *gorm.DB) error {
	return tx.Exec(`
		UPDATE format_templates
		SET format_rules = jsonb_set(
			jsonb_set(
				format_rules::jsonb,
				'{page_setup,margin_left}',
				'3.17'
			),
			'{page_setup,margin_right}',
			'3.17'
		)
		WHERE
			name ILIKE '%重庆文理学院%'
			AND (format_rules::jsonb -> 'page_setup' ->> 'margin_left')::numeric = 3.18
	`).Error
}
