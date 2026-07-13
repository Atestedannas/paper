package database

import (
	"log"

	"github.com/paper-format-checker/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// InitRBACData 初始化RBAC相关基础数据
func InitRBACData() {
	log.Println("开始初始化RBAC数据...")

	// 创建默认角色
	defaultRoles := []struct {
		name        string
		description string
		code        string
		roleType    string
	}{
		{
			name:        "超级管理员",
			description: "拥有系统最高权限的管理员角色",
			code:        "super_admin",
			roleType:    "system",
		},
		{
			name:        "系统管理员",
			description: "系统管理员，拥有大部分管理权限",
			code:        "admin",
			roleType:    "system",
		},
		{
			name:        "普通用户",
			description: "普通用户，拥有基本使用权限",
			code:        "user",
			roleType:    "system",
		},
		{
			name:        "审核员",
			description: "负责审核内容的用户角色",
			code:        "reviewer",
			roleType:    "business",
		},
		{
			name:        "财务人员",
			description: "负责财务管理的用户角色",
			code:        "finance",
			roleType:    "business",
		},
	}

	for _, roleData := range defaultRoles {
		var existingRole model.Role
		err := DB.Where("code = ?", roleData.code).First(&existingRole).Error
		if err != nil {
			// 角色不存在，创建新角色
			newRole := &model.Role{
				Name:        roleData.name,
				Description: roleData.description,
				Type:        roleData.roleType,
				Code:        roleData.code,
			}
			DB.Create(newRole)
		}
	}

	// 创建默认权限
	defaultPermissions := []struct {
		name         string
		code         string
		resourceType string
		method       string
		path         string
		description  string
	}{
		// 用户管理权限
		{
			name:         "用户列表查看",
			code:         "user:list",
			resourceType: "api",
			method:       "GET",
			path:         "/api/v1/admin/users",
			description:  "允许查看用户列表",
		},
		{
			name:         "用户角色更新",
			code:         "user:update_role",
			resourceType: "api",
			method:       "PUT",
			path:         "/api/v1/admin/users/*/role",
			description:  "允许更新用户角色",
		},
		{
			name:         "用户状态更新",
			code:         "user:update_status",
			resourceType: "api",
			method:       "PUT",
			path:         "/api/v1/admin/users/*/status",
			description:  "允许更新用户状态",
		},
		{
			name:         "用户删除",
			code:         "user:delete",
			resourceType: "api",
			method:       "DELETE",
			path:         "/api/v1/admin/users/*",
			description:  "允许删除用户",
		},

		// 论文管理权限
		{
			name:         "论文列表查看",
			code:         "paper:list",
			resourceType: "api",
			method:       "GET",
			path:         "/api/v1/admin/papers",
			description:  "允许查看论文列表",
		},
		{
			name:         "论文详情查看",
			code:         "paper:read",
			resourceType: "api",
			method:       "GET",
			path:         "/api/v1/papers/*",
			description:  "允许查看论文详情",
		},
		{
			name:         "论文上传",
			code:         "paper:create",
			resourceType: "api",
			method:       "POST",
			path:         "/api/v1/papers/upload",
			description:  "允许上传论文",
		},
		{
			name:         "论文格式检查",
			code:         "paper:check",
			resourceType: "api",
			method:       "POST",
			path:         "/api/v1/papers/*/check-format",
			description:  "允许检查论文格式",
		},
		{
			name:         "论文格式修复",
			code:         "paper:fix",
			resourceType: "api",
			method:       "POST",
			path:         "/api/v1/papers/*/apply-corrections",
			description:  "允许修复论文格式",
		},
		{
			name:         "论文格式管理",
			code:         "paper:format:manage",
			resourceType: "api",
			method:       "*",
			path:         "/api/v1/admin/papers/format*",
			description:  "允许管理论文格式",
		},

		// 订单管理权限
		{
			name:         "订单列表查看",
			code:         "order:list",
			resourceType: "api",
			method:       "GET",
			path:         "/api/v1/admin/orders",
			description:  "允许查看订单列表",
		},
		{
			name:         "订单状态更新",
			code:         "order:update_status",
			resourceType: "api",
			method:       "PUT",
			path:         "/api/v1/admin/orders/*/status",
			description:  "允许更新订单状态",
		},

		// 系统管理权限
		{
			name:         "系统配置查看",
			code:         "system:config:read",
			resourceType: "api",
			method:       "GET",
			path:         "/api/v1/admin/settings/*",
			description:  "允许查看系统配置",
		},
		{
			name:         "系统配置更新",
			code:         "system:config:update",
			resourceType: "api",
			method:       "PUT",
			path:         "/api/v1/admin/settings/*",
			description:  "允许更新系统配置",
		},

		// RBAC管理权限
		{
			name:         "角色管理",
			code:         "rbac:role:manage",
			resourceType: "api",
			method:       "*",
			path:         "/api/v1/admin/roles/*",
			description:  "允许管理角色",
		},
		{
			name:         "权限管理",
			code:         "rbac:permission:manage",
			resourceType: "api",
			method:       "*",
			path:         "/api/v1/admin/permissions/*",
			description:  "允许管理权限",
		},

		// 系统日志权限
		{
			name:         "系统日志查看",
			code:         "system:logs:view",
			resourceType: "api",
			method:       "GET",
			path:         "/api/v1/admin/settings/logs",
			description:  "允许查看系统日志",
		},

		// 支付管理权限
		{
			name:         "支付管理",
			code:         "payment:manage",
			resourceType: "api",
			method:       "*",
			path:         "/api/v1/admin/settings/payment/*",
			description:  "允许管理支付配置",
		},

		// 客服支持权限
		{
			name:         "客服支持管理",
			code:         "support:manage",
			resourceType: "api",
			method:       "*",
			path:         "/api/v1/admin/settings/support-contact",
			description:  "允许管理客服支持",
		},

		// 论文格式管理权限
		{
			name:         "论文格式管理",
			code:         "paper:format:manage",
			resourceType: "api",
			method:       "*",
			path:         "/api/v1/admin/papers/format*",
			description:  "允许管理论文格式",
		},
	}

	for _, permData := range defaultPermissions {
		var existingPermission model.Permission
		err := DB.Where("code = ?", permData.code).First(&existingPermission).Error
		if err != nil {
			// 权限不存在，创建新权限
			newPermission := &model.Permission{
				Name:         permData.name,
				Code:         permData.code,
				ResourceType: permData.resourceType,
				Method:       permData.method,
				Path:         permData.path,
				Description:  permData.description,
			}
			DB.Create(newPermission)
		}
	}

	// 为超级管理员分配所有权限
	superAdminRole := &model.Role{}
	if err := DB.Where("code = ?", "super_admin").First(superAdminRole).Error; err == nil {
		// 获取所有权限
		allPermissions := []model.Permission{}
		if err := DB.Find(&allPermissions).Error; err == nil {
			for _, permission := range allPermissions {
				// 为超级管理员分配此权限
				DB.Exec("INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
					superAdminRole.ID, permission.ID)
			}
		}
	}

	// 为管理员分配常用权限
	adminRole := &model.Role{}
	if err := DB.Where("code = ?", "admin").First(adminRole).Error; err == nil {
		// 获取管理员常用权限
		adminPermissions := []string{
			"user:list", "user:update_role", "user:update_status", "user:delete",
			"paper:list", "paper:read", "paper:create", "paper:check", "paper:fix",
			"order:list", "order:update_status",
			"system:config:read", "system:config:update",
		}

		for _, permCode := range adminPermissions {
			var permission model.Permission
			if err := DB.Where("code = ?", permCode).First(&permission).Error; err == nil {
				DB.Exec("INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
					adminRole.ID, permission.ID)
			}
		}
	}

	// 为审核员分配相关权限
	reviewerRole := &model.Role{}
	if err := DB.Where("code = ?", "reviewer").First(reviewerRole).Error; err == nil {
		// 获取审核员相关权限
		reviewerPermissions := []string{
			"paper:list", "paper:read", "paper:check",
		}

		for _, permCode := range reviewerPermissions {
			var permission model.Permission
			if err := DB.Where("code = ?", permCode).First(&permission).Error; err == nil {
				DB.Exec("INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
					reviewerRole.ID, permission.ID)
			}
		}
	}

	// 为财务人员分配相关权限
	financeRole := &model.Role{}
	if err := DB.Where("code = ?", "finance").First(financeRole).Error; err == nil {
		// 获取财务人员相关权限
		financePermissions := []string{
			"order:list", "order:update_status",
		}

		for _, permCode := range financePermissions {
			var permission model.Permission
			if err := DB.Where("code = ?", permCode).First(&permission).Error; err == nil {
				DB.Exec("INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
					financeRole.ID, permission.ID)
			}
		}
	}

	// 固定 admin 账号为超级管理员，并修复历史数据中的角色不同步。
	var superAdminUser model.User
	if err := DB.Where("username = ?", "admin").First(&superAdminUser).Error; err != nil {
		// 创建默认超级管理员用户
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("admin123"), 12)
		if err != nil {
			return
		}

		superAdminUser = model.User{
			Username:     "admin",
			Email:        "admin@localhost.com",
			FullName:     "超级管理员",
			PasswordHash: string(hashedPassword),
			Status:       "active",
			Role:         "super_admin",
			FreeChecks:   9999,
		}

		if err := DB.Create(&superAdminUser).Error; err != nil {
			log.Printf("创建默认超级管理员失败：%v", err)
			return
		}
	}

	if err := DB.Model(&superAdminUser).Update("role", "super_admin").Error; err != nil {
		log.Printf("同步 admin 超级管理员标识失败：%v", err)
	}
	if err := DB.Where("code = ?", "super_admin").First(superAdminRole).Error; err == nil {
		DB.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
			superAdminUser.ID, superAdminRole.ID)
	}

	log.Println("RBAC数据初始化完成")
}
