package database

import (
	"log"

	"gorm.io/gorm"
)

// Migration20260327UpdateGoldenTemplateToReal 将 golden_template_path 更新为真实模板文件
// cqrwst.docx（4KB）只是空模板，格式样本极少。
// cqrwst_real.docx（414KB）是管理员上传的真实论文格式范例，
// 含完整的正文/标题/摘要/参考文献等各类段落，格式学习效果最佳。
type Migration20260327UpdateGoldenTemplateToReal struct{}

func (m *Migration20260327UpdateGoldenTemplateToReal) Name() string {
	return "20260327_update_golden_template_to_real"
}

func (m *Migration20260327UpdateGoldenTemplateToReal) Up(tx *gorm.DB) error {
	result := tx.Exec(`
		UPDATE format_templates
		SET golden_template_path = 'uploads/golden_templates/cqrwst_real.docx'
		WHERE university_id IN (
			SELECT id FROM universities WHERE abbr = 'CQRWST' OR name = '重庆人文科技学院'
		)
	`)
	if result.Error != nil {
		return result.Error
	}
	log.Printf("[Migration] 更新了 %d 条模板记录，golden_template_path → cqrwst_real.docx", result.RowsAffected)
	return nil
}

func (m *Migration20260327UpdateGoldenTemplateToReal) Down(tx *gorm.DB) error {
	return tx.Exec(`
		UPDATE format_templates
		SET golden_template_path = 'uploads/golden_templates/cqrwst.docx'
		WHERE university_id IN (
			SELECT id FROM universities WHERE abbr = 'CQRWST' OR name = '重庆人文科技学院'
		)
	`).Error
}
