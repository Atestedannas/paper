package database

import (
	"log"

	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// Migration20250306CreateRBACTables 创建 RBAC 相关表
type Migration20250306CreateRBACTables struct{}

func (m *Migration20250306CreateRBACTables) Name() string {
	return "20250306_create_rbac_tables"
}

func (m *Migration20250306CreateRBACTables) Up(tx *gorm.DB) error {
	log.Println("Creating RBAC tables...")

	// 创建角色表
	if !tx.Migrator().HasTable(&model.Role{}) {
		if err := tx.AutoMigrate(&model.Role{}); err != nil {
			return err
		}
		log.Println("Created roles table")
	}

	// 创建权限表
	if !tx.Migrator().HasTable(&model.Permission{}) {
		if err := tx.AutoMigrate(&model.Permission{}); err != nil {
			return err
		}
		log.Println("Created permissions table")
	}

	// 创建用户角色关联表
	if !tx.Migrator().HasTable(&model.UserRole{}) {
		if err := tx.AutoMigrate(&model.UserRole{}); err != nil {
			return err
		}
		log.Println("Created user_roles table")
	}

	// 创建用户权限关联表
	if !tx.Migrator().HasTable(&model.UserPermission{}) {
		if err := tx.AutoMigrate(&model.UserPermission{}); err != nil {
			return err
		}
		log.Println("Created user_permissions table")
	}

	// 创建角色权限关联表
	if !tx.Migrator().HasTable(&model.RolePermission{}) {
		if err := tx.AutoMigrate(&model.RolePermission{}); err != nil {
			return err
		}
		log.Println("Created role_permissions table")
	}

	// 创建角色菜单关联表（菜单即权限）
	if !tx.Migrator().HasTable(&model.RoleMenu{}) {
		if err := tx.AutoMigrate(&model.RoleMenu{}); err != nil {
			return err
		}
		log.Println("Created role_menus table")
	}

	return nil
}

func (m *Migration20250306CreateRBACTables) Down(tx *gorm.DB) error {
	tx.Migrator().DropTable(&model.RolePermission{})
	tx.Migrator().DropTable(&model.UserPermission{})
	tx.Migrator().DropTable(&model.UserRole{})
	tx.Migrator().DropTable(&model.Permission{})
	tx.Migrator().DropTable(&model.Role{})
	return nil
}
