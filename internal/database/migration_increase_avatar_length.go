package database

import (
	"log"

	"gorm.io/gorm"
)

// Migration20250306IncreaseAvatarLength 增加 avatar 字段长度
type Migration20250306IncreaseAvatarLength struct{}

func (m *Migration20250306IncreaseAvatarLength) Name() string {
	return "20250306_increase_avatar_length"
}

func (m *Migration20250306IncreaseAvatarLength) Up(tx *gorm.DB) error {
	log.Println("Increasing avatar field length...")

	// 检查是否是 PostgreSQL 数据库
	dialect := tx.Dialector.Name()

	if dialect == "postgres" {
		// PostgreSQL: 使用 ALTER COLUMN 修改字段类型
		if err := tx.Exec("ALTER TABLE users ALTER COLUMN avatar TYPE varchar(10000)").Error; err != nil {
			return err
		}
		log.Println("Increased avatar field length for PostgreSQL")
	} else if dialect == "mysql" {
		// MySQL: 使用 MODIFY COLUMN 修改字段类型
		if err := tx.Exec("ALTER TABLE users MODIFY COLUMN avatar varchar(10000)").Error; err != nil {
			return err
		}
		log.Println("Increased avatar field length for MySQL")
	} else {
		// SQLite: 需要重建表
		log.Println("SQLite detected, avatar field will be updated on next table recreation")
	}

	return nil
}

func (m *Migration20250306IncreaseAvatarLength) Down(tx *gorm.DB) error {
	dialect := tx.Dialector.Name()

	if dialect == "postgres" {
		return tx.Exec("ALTER TABLE users ALTER COLUMN avatar TYPE varchar(255)").Error
	} else if dialect == "mysql" {
		return tx.Exec("ALTER TABLE users MODIFY COLUMN avatar varchar(255)").Error
	}
	return nil
}
