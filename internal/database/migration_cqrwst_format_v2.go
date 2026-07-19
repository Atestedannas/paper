package database

import (
	"log"

	"gorm.io/gorm"
)

// Migration20260327FixCQRWSTFormatV2 强制更新重庆人文科技学院所有格式模板的规则
// 根据用户提供的完整格式规范修正 V1 中的错误数据
type Migration20260327FixCQRWSTFormatV2 struct{}

func (m *Migration20260327FixCQRWSTFormatV2) Name() string {
	return "20260327_fix_cqrwst_format_v2"
}

func (m *Migration20260327FixCQRWSTFormatV2) Up(tx *gorm.DB) error {
	log.Println("[CQRWST-V2] 开始强制更新重庆人文科技学院所有格式模板...")

	formatRules := buildCQRWSTFormatRulesJSON()

	result := tx.Exec(`
		UPDATE format_templates 
		SET format_rules = ?, version = '2.0'
		WHERE university_id IN (
			SELECT id FROM universities WHERE name = ?
		)
	`, formatRules, "重庆人文科技学院")

	if result.Error != nil {
		log.Printf("[CQRWST-V2] 更新失败: %v", result.Error)
		return result.Error
	}

	log.Printf("[CQRWST-V2] 已更新 %d 个模板", result.RowsAffected)
	return nil
}

func (m *Migration20260327FixCQRWSTFormatV2) Down(tx *gorm.DB) error {
	return nil
}

// Migration20260327FixCQRWSTFormatV3 基于模板文件 XML 分析校正格式规则
// 修正：致谢字号(小四→五号)、致谢/参考文献/注释/附录标签补全、论文正标题补全
type Migration20260327FixCQRWSTFormatV3 struct{}

func (m *Migration20260327FixCQRWSTFormatV3) Name() string {
	return "20260327_fix_cqrwst_format_v3"
}

func (m *Migration20260327FixCQRWSTFormatV3) Up(tx *gorm.DB) error {
	log.Println("[CQRWST-V3] 开始基于模板文件分析结果强制更新格式模板...")

	formatRules := buildCQRWSTFormatRulesJSON()

	result := tx.Exec(`
		UPDATE format_templates 
		SET format_rules = ?, version = '3.0'
		WHERE university_id IN (
			SELECT id FROM universities WHERE name = ?
		)
	`, formatRules, "重庆人文科技学院")

	if result.Error != nil {
		log.Printf("[CQRWST-V3] 更新失败: %v", result.Error)
		return result.Error
	}

	log.Printf("[CQRWST-V3] 已更新 %d 个模板", result.RowsAffected)
	return nil
}

func (m *Migration20260327FixCQRWSTFormatV3) Down(tx *gorm.DB) error {
	return nil
}

// Migration20260327FixCQRWSTFormatV4 修复页眉页脚不生效的问题
// 处理器现在支持从顶层读取 header/page_number，同时支持"第×页 共×页"总页数格式
type Migration20260327FixCQRWSTFormatV4 struct{}

func (m *Migration20260327FixCQRWSTFormatV4) Name() string {
	return "20260327_fix_cqrwst_format_v4"
}

func (m *Migration20260327FixCQRWSTFormatV4) Up(tx *gorm.DB) error {
	log.Println("[CQRWST-V4] 修复页眉页脚：更新格式规则...")

	formatRules := buildCQRWSTFormatRulesJSON()

	result := tx.Exec(`
		UPDATE format_templates 
		SET format_rules = ?, version = '4.0'
		WHERE university_id IN (
			SELECT id FROM universities WHERE name = ?
		)
	`, formatRules, "重庆人文科技学院")

	if result.Error != nil {
		log.Printf("[CQRWST-V4] 更新失败: %v", result.Error)
		return result.Error
	}

	log.Printf("[CQRWST-V4] 已更新 %d 个模板", result.RowsAffected)
	return nil
}

func (m *Migration20260327FixCQRWSTFormatV4) Down(tx *gorm.DB) error {
	return nil
}

// Migration20260327FixCQRWSTFormatV5 修复段落分类准确率和加粗清除问题
// (1)(2)(3) 长文本不再误判为 heading_3；body 及所有内容区域显式 bold:false
type Migration20260327FixCQRWSTFormatV5 struct{}

func (m *Migration20260327FixCQRWSTFormatV5) Name() string {
	return "20260327_fix_cqrwst_format_v5"
}

func (m *Migration20260327FixCQRWSTFormatV5) Up(tx *gorm.DB) error {
	log.Println("[CQRWST-V5] 修复段落分类+加粗清除：更新格式规则...")

	formatRules := buildCQRWSTFormatRulesJSON()

	result := tx.Exec(`
		UPDATE format_templates 
		SET format_rules = ?, version = '5.0'
		WHERE university_id IN (
			SELECT id FROM universities WHERE name = ?
		)
	`, formatRules, "重庆人文科技学院")

	if result.Error != nil {
		log.Printf("[CQRWST-V5] 更新失败: %v", result.Error)
		return result.Error
	}

	log.Printf("[CQRWST-V5] 已更新 %d 个模板", result.RowsAffected)
	return nil
}

func (m *Migration20260327FixCQRWSTFormatV5) Down(tx *gorm.DB) error {
	return nil
}

// Migration20260719FixCQRWSTHeaderFooter refreshes existing rows with the
// header and dynamic total-page format extracted from the 2026 school template.
type Migration20260719FixCQRWSTHeaderFooter struct{}

func (m *Migration20260719FixCQRWSTHeaderFooter) Name() string {
	return "20260719_fix_cqrwst_header_footer"
}

func (m *Migration20260719FixCQRWSTHeaderFooter) Up(tx *gorm.DB) error {
	return tx.Exec(`
		UPDATE format_templates
		SET format_rules = ?, version = '6.0'
		WHERE university_id IN (
			SELECT id FROM universities WHERE name = ?
		)
	`, buildCQRWSTFormatRulesJSON(), "重庆人文科技学院").Error
}

func (m *Migration20260719FixCQRWSTHeaderFooter) Down(tx *gorm.DB) error {
	return nil
}
