package database

import (
	"fmt"
	"log"

	"gorm.io/gorm"
)

// Migration20250129CleanupAndFixFK 清理冗余表并修复外键约束
type Migration20250129CleanupAndFixFK struct{}

func (m *Migration20250129CleanupAndFixFK) Name() string {
	return "20250129_cleanup_and_fix_fk"
}

func (m *Migration20250129CleanupAndFixFK) Up(tx *gorm.DB) error {
	// 1. 删除冗余表
	redundantTables := []string{"format_standards", "format_checks"}
	for _, table := range redundantTables {
		if tx.Migrator().HasTable(table) {
			log.Printf("Dropping redundant table: %s", table)
			if err := tx.Migrator().DropTable(table); err != nil {
				return fmt.Errorf("failed to drop table %s: %w", table, err)
			}
		}
	}

	// 2. 彻底清理 format_templates 表上的所有异常外键
	// 查询 format_templates 表上的所有外键约束
	type ConstraintInfo struct {
		ConName string
	}
	var constraints []ConstraintInfo
	tx.Raw(`
		SELECT c.conname 
		FROM pg_constraint c
		JOIN pg_class t ON c.conrelid = t.oid
		WHERE t.relname = 'format_templates' AND c.contype = 'f'
	`).Scan(&constraints)

	for _, c := range constraints {
		// 如果发现约束名包含 "check_results" 或者看起来像是指向 check_results 的
		// 或者就是我们要清理的 fk_check_results_template
		if c.ConName == "fk_check_results_template" {
			log.Printf("Dropping constraint %s from format_templates", c.ConName)
			if err := tx.Exec(fmt.Sprintf("ALTER TABLE format_templates DROP CONSTRAINT %s", c.ConName)).Error; err != nil {
				return fmt.Errorf("failed to drop constraint %s: %w", c.ConName, err)
			}
		}
	}

	// 3. 确保 check_results 表有正确的外键指向 format_templates
	// 先检查是否存在
	var count int64
	tx.Raw(`
		SELECT count(*)
		FROM pg_constraint c
		JOIN pg_class t ON c.conrelid = t.oid
		WHERE t.relname = 'check_results' AND c.conname = 'fk_check_results_template'
	`).Scan(&count)

	if count == 0 {
		log.Println("Adding constraint fk_check_results_template to check_results...")

		// 确保数据一致性：检查 check_results 中的 template_id 是否都存在于 format_templates 中
		// 如果有无效的 template_id，将其设置为 NULL (前提是字段允许 NULL) 或者删除这些记录
		// 这里选择将无效的 template_id 设置为 format_templates 中的第一个 ID，或者如果 format_templates 为空，则清空 check_results

		// 简单起见，如果 template_id 无效，我们将其设为 NULL（如果之前迁移设为了 NOT NULL，这可能会失败，所以我们要小心）
		// 之前 Migration20250129FixCheckResultsTemplateID 已经将 template_id 设为 NOT NULL

		// 检查是否有无效引用
		var invalidCount int64
		tx.Raw(`
			SELECT count(*) FROM check_results 
			WHERE template_id NOT IN (SELECT id FROM format_templates)
		`).Scan(&invalidCount)

		if invalidCount > 0 {
			log.Printf("Found %d check_results with invalid template_id", invalidCount)
			// 尝试找到一个有效的 template_id
			var validID string
			tx.Raw("SELECT id FROM format_templates LIMIT 1").Scan(&validID)

			if validID != "" {
				log.Printf("Updating invalid records to use template_id: %s", validID)
				tx.Exec("UPDATE check_results SET template_id = ? WHERE template_id NOT IN (SELECT id FROM format_templates)", validID)
			} else {
				log.Println("No valid templates found, cannot add FK constraint safely without deleting data. Deleting invalid check_results...")
				tx.Exec("DELETE FROM check_results WHERE template_id NOT IN (SELECT id FROM format_templates)")
			}
		}

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

func (m *Migration20250129CleanupAndFixFK) Down(tx *gorm.DB) error {
	// 这是一个修复性迁移，通常不需要回滚，或者回滚就是"什么都不做"
	return nil
}
