package migrations

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MigrationRBACEnhanced RBAC 增强版迁移
type MigrationRBACEnhanced struct{}

// Migrate 执行迁移
func (m *MigrationRBACEnhanced) Migrate(db *gorm.DB) error {
	fmt.Println("开始执行 RBAC 增强版迁移...")

	// 启用外键约束
	if err := db.Exec("SET session_replication_role = 'replica'").Error; err != nil {
		// PostgreSQL 特定，其他数据库可能不需要
		fmt.Println("注意：跳过外键约束设置（非 PostgreSQL 数据库）")
	}

	// 1. 创建菜单表
	if err := m.createMenusTable(db); err != nil {
		return fmt.Errorf("创建 menus 表失败：%w", err)
	}

	// 2. 创建权限表（authorities）
	if err := m.createAuthoritiesTable(db); err != nil {
		return fmt.Errorf("创建 authorities 表失败：%w", err)
	}

	// 3. 创建 Casbin 规则表
	if err := m.createCasbinRulesTable(db); err != nil {
		return fmt.Errorf("创建 casbin_rules 表失败：%w", err)
	}

	// 4. 创建数据权限规则表
	if err := m.createDataPermissionRulesTable(db); err != nil {
		return fmt.Errorf("创建 data_permission_rules 表失败：%w", err)
	}

	// 5. 创建操作日志表
	if err := m.createOperationLogsTable(db); err != nil {
		return fmt.Errorf("创建 operation_logs 表失败：%w", err)
	}

	// 6. 创建角色 - 菜单关联表
	if err := m.createRoleMenusTable(db); err != nil {
		return fmt.Errorf("创建 role_menus 表失败：%w", err)
	}

	// 7. 创建角色 - 权限关联表
	if err := m.createRoleAuthoritiesTable(db); err != nil {
		return fmt.Errorf("创建 role_authorities 表失败：%w", err)
	}

	// 8. 更新 roles 表（添加 parent_id 和 sort_order）
	if err := m.updateRolesTable(db); err != nil {
		return fmt.Errorf("更新 roles 表失败：%w", err)
	}

	// 9. 插入默认数据
	if err := m.insertDefaultData(db); err != nil {
		return fmt.Errorf("插入默认数据失败：%w", err)
	}

	fmt.Println("RBAC 增强版迁移完成！")
	return nil
}

// Rollback 回滚迁移
func (m *MigrationRBACEnhanced) Rollback(db *gorm.DB) error {
	fmt.Println("开始回滚 RBAC 增强版迁移...")

	// 按相反顺序删除表
	tables := []string{
		"role_authorities",
		"role_menus",
		"operation_logs",
		"data_permission_rules",
		"casbin_rules",
		"authorities",
		"menus",
	}

	for _, table := range tables {
		if err := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", table)).Error; err != nil {
			return fmt.Errorf("删除表 %s 失败：%w", table, err)
		}
	}

	fmt.Println("RBAC 增强版迁移已回滚")
	return nil
}

// createMenusTable 创建菜单表
func (m *MigrationRBACEnhanced) createMenusTable(db *gorm.DB) error {
	sql := `
	CREATE TABLE IF NOT EXISTS menus (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		parent_id UUID REFERENCES menus(id) ON DELETE CASCADE,
		name VARCHAR(50) NOT NULL,
		title VARCHAR(100) NOT NULL,
		icon VARCHAR(50),
		path VARCHAR(255),
		component VARCHAR(255),
		sort_order INT DEFAULT 0,
		menu_type VARCHAR(20) DEFAULT 'menu',
		permission VARCHAR(100),
		visible BOOLEAN DEFAULT true,
		keep_alive BOOLEAN DEFAULT false,
		redirect VARCHAR(255),
		meta JSONB DEFAULT '{}',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX idx_menus_parent_id ON menus(parent_id);
	CREATE INDEX idx_menus_sort_order ON menus(sort_order);
	`
	return db.Exec(sql).Error
}

// createAuthoritiesTable 创建权限表
func (m *MigrationRBACEnhanced) createAuthoritiesTable(db *gorm.DB) error {
	sql := `
	CREATE TABLE IF NOT EXISTS authorities (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name VARCHAR(100) NOT NULL,
		code VARCHAR(100) UNIQUE NOT NULL,
		type VARCHAR(20) DEFAULT 'api',
		resource_type VARCHAR(50),
		resource_path VARCHAR(255),
		http_method VARCHAR(10),
		description TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX idx_authorities_code ON authorities(code);
	CREATE INDEX idx_authorities_type ON authorities(type);
	`
	return db.Exec(sql).Error
}

// createCasbinRulesTable 创建 Casbin 规则表
func (m *MigrationRBACEnhanced) createCasbinRulesTable(db *gorm.DB) error {
	sql := `
	CREATE TABLE IF NOT EXISTS casbin_rules (
		id SERIAL PRIMARY KEY,
		p_type VARCHAR(20) NOT NULL,
		v0 VARCHAR(100),
		v1 VARCHAR(100),
		v2 VARCHAR(100),
		v3 VARCHAR(100),
		v4 VARCHAR(100),
		v5 VARCHAR(100),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX idx_casbin_rules_p_type ON casbin_rules(p_type);
	CREATE INDEX idx_casbin_rules_v0 ON casbin_rules(v0);
	CREATE INDEX idx_casbin_rules_v1 ON casbin_rules(v1);
	`
	return db.Exec(sql).Error
}

// createDataPermissionRulesTable 创建数据权限规则表
func (m *MigrationRBACEnhanced) createDataPermissionRulesTable(db *gorm.DB) error {
	sql := `
	CREATE TABLE IF NOT EXISTS data_permission_rules (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
		resource_type VARCHAR(50) NOT NULL,
		scope VARCHAR(20) DEFAULT 'self',
		custom_rule JSONB DEFAULT '{}',
		department_ids UUID[] DEFAULT '{}',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX idx_data_permission_rules_role_id ON data_permission_rules(role_id);
	CREATE INDEX idx_data_permission_rules_resource_type ON data_permission_rules(resource_type);
	`
	return db.Exec(sql).Error
}

// createOperationLogsTable 创建操作日志表
func (m *MigrationRBACEnhanced) createOperationLogsTable(db *gorm.DB) error {
	sql := `
	CREATE TABLE IF NOT EXISTS operation_logs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID REFERENCES users(id),
		operation VARCHAR(100) NOT NULL,
		resource_type VARCHAR(50),
		resource_id UUID,
		request_method VARCHAR(10),
		request_path VARCHAR(255),
		request_body JSONB,
		response_status INT,
		ip_address VARCHAR(50),
		user_agent TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX idx_operation_logs_user_id ON operation_logs(user_id);
	CREATE INDEX idx_operation_logs_created_at ON operation_logs(created_at);
	CREATE INDEX idx_operation_logs_operation ON operation_logs(operation);
	`
	return db.Exec(sql).Error
}

// createRoleMenusTable 创建角色 - 菜单关联表
func (m *MigrationRBACEnhanced) createRoleMenusTable(db *gorm.DB) error {
	sql := `
	CREATE TABLE IF NOT EXISTS role_menus (
		role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
		menu_id UUID NOT NULL REFERENCES menus(id) ON DELETE CASCADE,
		PRIMARY KEY (role_id, menu_id)
	);
	
	CREATE INDEX idx_role_menus_role_id ON role_menus(role_id);
	CREATE INDEX idx_role_menus_menu_id ON role_menus(menu_id);
	`
	return db.Exec(sql).Error
}

// createRoleAuthoritiesTable 创建角色 - 权限关联表
func (m *MigrationRBACEnhanced) createRoleAuthoritiesTable(db *gorm.DB) error {
	sql := `
	CREATE TABLE IF NOT EXISTS role_authorities (
		role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
		authority_id UUID NOT NULL REFERENCES authorities(id) ON DELETE CASCADE,
		PRIMARY KEY (role_id, authority_id)
	);
	
	CREATE INDEX idx_role_authorities_role_id ON role_authorities(role_id);
	CREATE INDEX idx_role_authorities_authority_id ON role_authorities(authority_id);
	`
	return db.Exec(sql).Error
}

// updateRolesTable 更新 roles 表
func (m *MigrationRBACEnhanced) updateRolesTable(db *gorm.DB) error {
	// 检查 parent_id 列是否存在
	var count int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM information_schema.columns 
		WHERE table_name = 'roles' AND column_name = 'parent_id'
	`).Scan(&count).Error; err != nil {
		return err
	}

	if count == 0 {
		// 添加 parent_id 列
		if err := db.Exec("ALTER TABLE roles ADD COLUMN parent_id UUID REFERENCES roles(id) ON DELETE SET NULL").Error; err != nil {
			return err
		}
	}

	// 检查 sort_order 列是否存在
	if err := db.Raw(`
		SELECT COUNT(*) FROM information_schema.columns 
		WHERE table_name = 'roles' AND column_name = 'sort_order'
	`).Scan(&count).Error; err != nil {
		return err
	}

	if count == 0 {
		// 添加 sort_order 列
		if err := db.Exec("ALTER TABLE roles ADD COLUMN sort_order INT DEFAULT 0").Error; err != nil {
			return err
		}
	}

	return nil
}

// insertDefaultData 插入默认数据
func (m *MigrationRBACEnhanced) insertDefaultData(db *gorm.DB) error {
	now := time.Now()

	// 1. 插入默认菜单
	defaultMenus := []map[string]interface{}{
		{
			"name":       "dashboard",
			"title":      "控制台",
			"icon":       "DataAnalysis",
			"path":       "/admin/dashboard",
			"component":  "admin/AdminDashboardView.vue",
			"menu_type":  "menu",
			"permission": "system:dashboard:view",
			"sort_order": 1,
			"meta":       `{"title": "控制台", "icon": "DataAnalysis"}`,
		},
		{
			"name":       "user-management",
			"title":      "用户管理",
			"icon":       "User",
			"path":       "/admin/users",
			"component":  "admin/UserManagementView.vue",
			"menu_type":  "menu",
			"permission": "user:list",
			"sort_order": 2,
			"meta":       `{"title": "用户管理", "icon": "User"}`,
		},
		{
			"name":       "role-management",
			"title":      "角色管理",
			"icon":       "Lock",
			"path":       "/admin/roles",
			"component":  "admin/RoleManagementView.vue",
			"menu_type":  "menu",
			"permission": "rbac:role:manage",
			"sort_order": 3,
			"meta":       `{"title": "角色管理", "icon": "Lock"}`,
		},
		{
			"name":       "permission-management",
			"title":      "权限管理",
			"icon":       "Key",
			"path":       "/admin/permissions",
			"component":  "admin/PermissionManagementView.vue",
			"menu_type":  "menu",
			"permission": "rbac:permission:manage",
			"sort_order": 4,
			"meta":       `{"title": "权限管理", "icon": "Key"}`,
		},
		{
			"name":       "menu-management",
			"title":      "菜单管理",
			"icon":       "Menu",
			"path":       "/admin/menus",
			"component":  "admin/MenuManagementView.vue",
			"menu_type":  "menu",
			"permission": "system:menu:manage",
			"sort_order": 5,
			"meta":       `{"title": "菜单管理", "icon": "Menu"}`,
		},
		{
			"name":       "paper-management",
			"title":      "论文管理",
			"icon":       "Document",
			"path":       "/admin/papers",
			"component":  "admin/PaperManagementView.vue",
			"menu_type":  "menu",
			"permission": "paper:list",
			"sort_order": 6,
			"meta":       `{"title": "论文管理", "icon": "Document"}`,
		},
		{
			"name":       "system-settings",
			"title":      "系统设置",
			"icon":       "Setting",
			"path":       "/admin/settings",
			"component":  "admin/SystemSettingsView.vue",
			"menu_type":  "menu",
			"permission": "system:config:view",
			"sort_order": 7,
			"meta":       `{"title": "系统设置", "icon": "Setting"}`,
		},
	}

	for _, menu := range defaultMenus {
		menuID := uuid.New()
		menu["id"] = menuID
		menu["created_at"] = now
		menu["updated_at"] = now

		// 构建插入 SQL
		sql := `
		INSERT INTO menus (id, name, title, icon, path, component, menu_type, permission, sort_order, meta, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING`

		if err := db.Exec(sql,
			menu["id"],
			menu["name"],
			menu["title"],
			menu["icon"],
			menu["path"],
			menu["component"],
			menu["menu_type"],
			menu["permission"],
			menu["sort_order"],
			menu["meta"],
			menu["created_at"],
			menu["updated_at"],
		).Error; err != nil {
			return fmt.Errorf("插入菜单 %s 失败：%w", menu["name"], err)
		}
	}

	// 2. 插入默认权限
	defaultAuthorities := []map[string]interface{}{
		// 用户管理权限
		{"name": "查看用户列表", "code": "user:list", "type": "api", "resource_path": "/api/v1/admin/users", "http_method": "GET"},
		{"name": "创建用户", "code": "user:create", "type": "api", "resource_path": "/api/v1/admin/users", "http_method": "POST"},
		{"name": "更新用户", "code": "user:update", "type": "api", "resource_path": "/api/v1/admin/users/*", "http_method": "PUT"},
		{"name": "删除用户", "code": "user:delete", "type": "api", "resource_path": "/api/v1/admin/users/*", "http_method": "DELETE"},

		// 角色管理权限
		{"name": "查看角色列表", "code": "rbac:role:list", "type": "api", "resource_path": "/api/v1/admin/roles", "http_method": "GET"},
		{"name": "创建角色", "code": "rbac:role:create", "type": "api", "resource_path": "/api/v1/admin/roles", "http_method": "POST"},
		{"name": "更新角色", "code": "rbac:role:update", "type": "api", "resource_path": "/api/v1/admin/roles/*", "http_method": "PUT"},
		{"name": "删除角色", "code": "rbac:role:delete", "type": "api", "resource_path": "/api/v1/admin/roles/*", "http_method": "DELETE"},
		{"name": "角色管理", "code": "rbac:role:manage", "type": "menu", "resource_path": "", "http_method": ""},

		// 权限管理权限
		{"name": "查看权限列表", "code": "rbac:permission:list", "type": "api", "resource_path": "/api/v1/admin/authorities", "http_method": "GET"},
		{"name": "创建权限", "code": "rbac:permission:create", "type": "api", "resource_path": "/api/v1/admin/authorities", "http_method": "POST"},
		{"name": "更新权限", "code": "rbac:permission:update", "type": "api", "resource_path": "/api/v1/admin/authorities/*", "http_method": "PUT"},
		{"name": "删除权限", "code": "rbac:permission:delete", "type": "api", "resource_path": "/api/v1/admin/authorities/*", "http_method": "DELETE"},
		{"name": "权限管理", "code": "rbac:permission:manage", "type": "menu", "resource_path": "", "http_method": ""},

		// 菜单管理权限
		{"name": "查看菜单列表", "code": "system:menu:list", "type": "api", "resource_path": "/api/v1/admin/menus", "http_method": "GET"},
		{"name": "创建菜单", "code": "system:menu:create", "type": "api", "resource_path": "/api/v1/admin/menus", "http_method": "POST"},
		{"name": "更新菜单", "code": "system:menu:update", "type": "api", "resource_path": "/api/v1/admin/menus/*", "http_method": "PUT"},
		{"name": "删除菜单", "code": "system:menu:delete", "type": "api", "resource_path": "/api/v1/admin/menus/*", "http_method": "DELETE"},
		{"name": "菜单管理", "code": "system:menu:manage", "type": "menu", "resource_path": "", "http_method": ""},

		// 论文管理权限
		{"name": "查看论文列表", "code": "paper:list", "type": "api", "resource_path": "/api/v1/admin/papers", "http_method": "GET"},
		{"name": "删除论文", "code": "paper:delete", "type": "api", "resource_path": "/api/v1/admin/papers/*", "http_method": "DELETE"},

		// 系统设置权限
		{"name": "查看系统配置", "code": "system:config:view", "type": "api", "resource_path": "/api/v1/admin/settings/*", "http_method": "GET"},
		{"name": "更新系统配置", "code": "system:config:update", "type": "api", "resource_path": "/api/v1/admin/settings/*", "http_method": "PUT"},

		// 控制台权限
		{"name": "查看控制台", "code": "system:dashboard:view", "type": "menu", "resource_path": "/api/v1/admin/dashboard", "http_method": "GET"},
	}

	for _, auth := range defaultAuthorities {
		authID := uuid.New()
		auth["id"] = authID
		auth["created_at"] = now
		auth["updated_at"] = now

		sql := `
		INSERT INTO authorities (id, name, code, type, resource_path, http_method, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (code) DO NOTHING`

		if err := db.Exec(sql,
			auth["id"],
			auth["name"],
			auth["code"],
			auth["type"],
			auth["resource_path"],
			auth["http_method"],
			auth["created_at"],
			auth["updated_at"],
		).Error; err != nil {
			return fmt.Errorf("插入权限 %s 失败：%w", auth["code"], err)
		}
	}

	// 3. 为超级管理员角色分配所有菜单和权限
	var superAdminRole struct {
		ID uuid.UUID
	}
	if err := db.Table("roles").Where("code = ?", "super_admin").First(&superAdminRole).Error; err == nil {
		// 分配所有菜单
		var menus []struct {
			ID uuid.UUID
		}
		if err := db.Table("menus").Find(&menus).Error; err == nil {
			for _, menu := range menus {
				if err := db.Exec(
					"INSERT INTO role_menus (role_id, menu_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
					superAdminRole.ID, menu.ID,
				).Error; err != nil {
					fmt.Printf("分配菜单失败：%v\n", err)
				}
			}
		}

		// 分配所有权限
		var authorities []struct {
			ID uuid.UUID
		}
		if err := db.Table("authorities").Find(&authorities).Error; err == nil {
			for _, auth := range authorities {
				if err := db.Exec(
					"INSERT INTO role_authorities (role_id, authority_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
					superAdminRole.ID, auth.ID,
				).Error; err != nil {
					fmt.Printf("分配权限失败：%v\n", err)
				}
			}
		}

		// 插入 Casbin 策略（超级管理员拥有所有权限）
		if err := db.Exec(
			"INSERT INTO casbin_rules (p_type, v0, v1, v2) VALUES ('p', 'super_admin', '*', '*') ON CONFLICT DO NOTHING",
		).Error; err != nil {
			fmt.Printf("插入 Casbin 策略失败：%v\n", err)
		}
	}

	return nil
}
