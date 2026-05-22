package database

import (
	"fmt"
	"time"
)

// MigrationAddDeletedAtToPapers 为 papers 表添加 deleted_at 字段（软删除支持）
type MigrationAddDeletedAtToPapers struct{}

// Up 执行迁移
func (m *MigrationAddDeletedAtToPapers) Up() error {
	// 检查列是否已存在
	var exists bool
	err := DB.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns 
			WHERE table_name = 'papers' AND column_name = 'deleted_at'
		)
	`).Scan(&exists).Error
	if err != nil {
		return fmt.Errorf("检查列是否存在失败：%w", err)
	}

	if exists {
		fmt.Println("[Migration] papers.deleted_at 列已存在，跳过迁移")
		return nil
	}

	// 添加 deleted_at 列
	err = DB.Exec(`
		ALTER TABLE papers 
		ADD COLUMN deleted_at TIMESTAMPTZ NULL;
	`).Error
	if err != nil {
		return fmt.Errorf("添加 deleted_at 列失败：%w", err)
	}

	// 为 deleted_at 创建索引（提高软删除查询性能）
	err = DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_papers_deleted_at ON papers(deleted_at);
	`).Error
	if err != nil {
		return fmt.Errorf("创建 deleted_at 索引失败：%w", err)
	}

	fmt.Println("[Migration] 成功为 papers 表添加 deleted_at 字段")
	return nil
}

// Down 回滚迁移
func (m *MigrationAddDeletedAtToPapers) Down() error {
	// 删除索引
	err := DB.Exec(`DROP INDEX IF EXISTS idx_papers_deleted_at;`).Error
	if err != nil {
		return fmt.Errorf("删除索引失败：%w", err)
	}

	// 删除列
	err = DB.Exec(`ALTER TABLE papers DROP COLUMN IF EXISTS deleted_at;`).Error
	if err != nil {
		return fmt.Errorf("删除列失败：%w", err)
	}

	fmt.Println("[Migration] 已回滚 papers 表的 deleted_at 字段")
	return nil
}

// TableName 返回迁移表名
func (m *MigrationAddDeletedAtToPapers) TableName() string {
	return "migrations"
}

// GetVersion 返回迁移版本号
func (m *MigrationAddDeletedAtToPapers) GetVersion() int64 {
	return time.Now().Unix()
}

// GetDescription 返回迁移描述
func (m *MigrationAddDeletedAtToPapers) GetDescription() string {
	return "为 papers 表添加 deleted_at 字段（软删除支持）"
}
