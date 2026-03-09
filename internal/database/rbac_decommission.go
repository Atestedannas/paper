package database

import "fmt"
import "gorm.io/gorm"

// DecommissionLegacyAuthorityTables
// Phase-3 low-traffic operation:
// 1) backup legacy tables
// 2) drop role_authorities and authorities
func DecommissionLegacyAuthorityTables() error {
	if DB == nil {
		return fmt.Errorf("database is not initialized")
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasTable("authorities") {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS authorities_legacy_backup (LIKE authorities INCLUDING ALL)`).Error; err != nil {
				return fmt.Errorf("create authorities backup failed: %w", err)
			}
			if err := tx.Exec(`INSERT INTO authorities_legacy_backup SELECT * FROM authorities ON CONFLICT DO NOTHING`).Error; err != nil {
				return fmt.Errorf("backup authorities failed: %w", err)
			}
		}

		if tx.Migrator().HasTable("role_authorities") {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS role_authorities_legacy_backup (LIKE role_authorities INCLUDING ALL)`).Error; err != nil {
				return fmt.Errorf("create role_authorities backup failed: %w", err)
			}
			if err := tx.Exec(`INSERT INTO role_authorities_legacy_backup SELECT * FROM role_authorities ON CONFLICT DO NOTHING`).Error; err != nil {
				return fmt.Errorf("backup role_authorities failed: %w", err)
			}
		}

		// Drop relation table first due to FK dependency.
		if tx.Migrator().HasTable("role_authorities") {
			if err := tx.Exec(`DROP TABLE IF EXISTS role_authorities`).Error; err != nil {
				return fmt.Errorf("drop role_authorities failed: %w", err)
			}
		}
		if tx.Migrator().HasTable("authorities") {
			if err := tx.Exec(`DROP TABLE IF EXISTS authorities`).Error; err != nil {
				return fmt.Errorf("drop authorities failed: %w", err)
			}
		}

		return nil
	})
}
