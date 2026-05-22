package database

import (
	"log"

	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// Migration20260307UnifyRBACModel
// Phase-1 non-breaking migration:
// - Keep legacy authorities/role_authorities tables
// - Mirror data into permissions/role_permissions as canonical source
type Migration20260307UnifyRBACModel struct{}

func (m *Migration20260307UnifyRBACModel) Name() string {
	return "20260307_unify_rbac_model_authority_to_permission"
}

func (m *Migration20260307UnifyRBACModel) Up(tx *gorm.DB) error {
	// Ensure canonical tables exist (idempotent)
	if err := tx.AutoMigrate(&model.Permission{}, &model.RolePermission{}); err != nil {
		return err
	}

	// If legacy tables do not exist, nothing to migrate.
	if !tx.Migrator().HasTable("authorities") {
		log.Println("authorities table not found, skip authority->permission sync")
		return nil
	}

	// 1) Backfill authorities -> permissions by code (non-destructive).
	if err := tx.Exec(`
		INSERT INTO permissions (id, name, code, resource_type, method, path, description, created_at)
		SELECT
			gen_random_uuid(),
			a.name,
			a.code,
			COALESCE(NULLIF(a.type, ''), 'api') AS resource_type,
			COALESCE(NULLIF(a.http_method, ''), 'GET') AS method,
			COALESCE(a.resource_path, '') AS path,
			'[compat] migrated from authorities',
			COALESCE(a.created_at, NOW())
		FROM authorities a
		ON CONFLICT (code) DO NOTHING
	`).Error; err != nil {
		return err
	}

	// 2) Backfill role_authorities -> role_permissions by authority.code mapping.
	if tx.Migrator().HasTable("role_authorities") {
		if err := tx.Exec(`
			INSERT INTO role_permissions (role_id, permission_id)
			SELECT
				ra.role_id,
				p.id
			FROM role_authorities ra
			JOIN authorities a ON a.id = ra.authority_id
			JOIN permissions p ON p.code = a.code
			ON CONFLICT (role_id, permission_id) DO NOTHING
		`).Error; err != nil {
			return err
		}
	}

	log.Println("RBAC model compatibility sync completed (authority -> permission)")
	return nil
}

func (m *Migration20260307UnifyRBACModel) Down(tx *gorm.DB) error {
	// Non-destructive migration: do not remove data on rollback.
	return nil
}

