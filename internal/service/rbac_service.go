package service

import (
	"errors"
	"fmt"

	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// RBACService RBAC权限管理服务接口
type RBACService interface {
	// 用户管理
	CreateUser(username, password, email, fullName string) (*model.User, error)
	GetUserByID(id uuid.UUID) (*model.User, error)
	GetUserByUsername(username string) (*model.User, error)
	UpdateUser(id uuid.UUID, updates map[string]interface{}) error
	DeleteUser(id uuid.UUID) error

	// 角色管理
	CreateRole(name, description, roleType, code string, parentID *uuid.UUID) (*model.Role, error)
	GetRoleByID(id uuid.UUID) (*model.Role, error)
	GetRoles() ([]model.Role, error)
	UpdateRole(id uuid.UUID, updates map[string]interface{}) error
	DeleteRole(id uuid.UUID) error
	AssignRoleToUser(userID, roleID uuid.UUID) error
	RemoveRoleFromUser(userID, roleID uuid.UUID) error

	// 权限管理
	CreatePermission(name, code, resourceType, method, path, description string) (*model.Permission, error)
	GetPermissionByID(id uuid.UUID) (*model.Permission, error)
	GetPermissions() ([]model.Permission, error)
	UpdatePermission(id uuid.UUID, updates map[string]interface{}) error
	DeletePermission(id uuid.UUID) error
	AssignPermissionToRole(roleID uuid.UUID, permissionID uuid.UUID) error
	RemovePermissionFromRole(roleID uuid.UUID, permissionID uuid.UUID) error

	// 权限检查
	HasPermission(userID uuid.UUID, resource, action string) (bool, error)
	GetUserPermissions(userID uuid.UUID) ([]model.Permission, error)
	GetUserRoles(userID uuid.UUID) ([]model.Role, error)

	// Casbin策略管理
	AddPolicy(sub, obj, act string) (bool, error)
	RemovePolicy(sub, obj, act string) (bool, error)
	GetPermissionsForUser(userID string) [][]string
	GetRolesForUser(userID string) []string
	GetUsersForRole(roleID string) []string
	AddRoleForUser(userID, roleID string) (bool, error)
	DeleteRoleForUser(userID, roleID string) (bool, error)
}

// rbacService RBAC服务实现
type rbacService struct {
	enforcer *casbin.Enforcer
}

// NewRBACService 创建RBAC服务实例
func NewRBACService() (RBACService, error) {
	// 初始化Casbin适配器
	adapter, err := gormadapter.NewAdapterByDB(database.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to create casbin adapter: %w", err)
	}

	// 初始化Casbin Enforcer
	e, err := casbin.NewEnforcer("conf/rbac_model.conf", adapter)
	if err != nil {
		return nil, fmt.Errorf("failed to create casbin enforcer: %w", err)
	}

	// 加载策略
	if err := e.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("failed to load casbin policy: %w", err)
	}

	return &rbacService{
		enforcer: e,
	}, nil
}

// CreateUser 创建用户
func (s *rbacService) CreateUser(username, password, email, fullName string) (*model.User, error) {
	// 检查用户是否已存在
	var existingUser model.User
	err := database.DB.Where("username = ? OR email = ?", username, email).First(&existingUser).Error
	if err == nil {
		return nil, errors.New("用户名或邮箱已存在")
	}

	// 生成密码哈希
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, err
	}

	// 创建用户
	user := &model.User{
		Username:     username,
		Email:        email,
		FullName:     fullName,
		PasswordHash: string(hashedPassword),
		Status:       "active",
		Role:         "user", // 默认角色为普通用户
		FreeChecks:   2,
	}

	if err := database.DB.Create(user).Error; err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserByID 根据ID获取用户
func (s *rbacService) GetUserByID(id uuid.UUID) (*model.User, error) {
	var user model.User
	if err := database.DB.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByUsername 根据用户名获取用户
func (s *rbacService) GetUserByUsername(username string) (*model.User, error) {
	var user model.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateUser 更新用户信息
func (s *rbacService) UpdateUser(id uuid.UUID, updates map[string]interface{}) error {
	return database.DB.Model(&model.User{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteUser 删除用户
func (s *rbacService) DeleteUser(id uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 从Casbin中移除用户的所有策略
		userIDStr := id.String()
		s.enforcer.DeleteUser(userIDStr)

		// 软删除用户
		return tx.Model(&model.User{}).Where("id = ?", id).Update("status", "deleted").Error
	})
}

// CreateRole 创建角色
func (s *rbacService) CreateRole(name, description, roleType, code string, parentID *uuid.UUID) (*model.Role, error) {
	// 检查角色代码是否已存在
	var existingRole model.Role
	err := database.DB.Where("code = ?", code).First(&existingRole).Error
	if err == nil {
		return nil, errors.New("角色代码已存在")
	}

	role := &model.Role{
		Name:        name,
		Description: description,
		Type:        roleType,
		Code:        code,
		ParentID:    parentID,
	}

	if err := database.DB.Create(role).Error; err != nil {
		return nil, err
	}

	return role, nil
}

// GetRoleByID 根据ID获取角色
func (s *rbacService) GetRoleByID(id uuid.UUID) (*model.Role, error) {
	var role model.Role
	if err := database.DB.Preload("Parent").Preload("Children").First(&role, id).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

// GetRoles 获取所有角色
func (s *rbacService) GetRoles() ([]model.Role, error) {
	var roles []model.Role
	if err := database.DB.Preload("Parent").Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// UpdateRole 更新角色信息
func (s *rbacService) UpdateRole(id uuid.UUID, updates map[string]interface{}) error {
	return database.DB.Model(&model.Role{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteRole 删除角色
func (s *rbacService) DeleteRole(id uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 检查是否有用户正在使用此角色
		var userRoleCount int64
		tx.Table("user_roles").Where("role_id = ?", id).Count(&userRoleCount)
		if userRoleCount > 0 {
			return errors.New("无法删除：有用户正在使用此角色")
		}

		// 从Casbin中移除角色的所有策略
		roleIDStr := id.String()
		s.enforcer.DeleteRole(roleIDStr)

		// 删除角色
		return tx.Delete(&model.Role{}, id).Error
	})
}

// AssignRoleToUser 为用户分配角色
func (s *rbacService) AssignRoleToUser(userID, roleID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 检查用户和角色是否存在
		var user model.User
		if err := tx.First(&user, userID).Error; err != nil {
			return errors.New("用户不存在")
		}

		var role model.Role
		if err := tx.First(&role, roleID).Error; err != nil {
			return errors.New("角色不存在")
		}

		// 添加用户角色关联
		if err := tx.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
			userID, roleID).Error; err != nil {
			return err
		}

		// 在Casbin中添加角色分配
		userIDStr := userID.String()
		roleIDStr := roleID.String()
		_, err := s.enforcer.AddRoleForUser(userIDStr, roleIDStr)
		if err != nil {
			return err
		}

		// 同步更新用户的主角色（如果需要）
		if user.Role == "user" || user.Role == "" {
			tx.Model(&user).Update("role", role.Code)
		}

		return nil
	})
}

// RemoveRoleFromUser 从用户移除角色
func (s *rbacService) RemoveRoleFromUser(userID, roleID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 移除用户角色关联
		if err := tx.Exec("DELETE FROM user_roles WHERE user_id = ? AND role_id = ?",
			userID, roleID).Error; err != nil {
			return err
		}

		// 从Casbin中移除角色分配
		userIDStr := userID.String()
		roleIDStr := roleID.String()
		_, err := s.enforcer.DeleteRoleForUser(userIDStr, roleIDStr)
		if err != nil {
			return err
		}

		return nil
	})
}

// CreatePermission 创建权限
func (s *rbacService) CreatePermission(name, code, resourceType, method, path, description string) (*model.Permission, error) {
	// 检查权限代码是否已存在
	var existingPermission model.Permission
	err := database.DB.Where("code = ?", code).First(&existingPermission).Error
	if err == nil {
		return nil, errors.New("权限代码已存在")
	}

	permission := &model.Permission{
		Name:         name,
		Code:         code,
		ResourceType: resourceType,
		Method:       method,
		Path:         path,
		Description:  description,
	}

	if err := database.DB.Create(permission).Error; err != nil {
		return nil, err
	}

	return permission, nil
}

// GetPermissionByID 根据ID获取权限
func (s *rbacService) GetPermissionByID(id uuid.UUID) (*model.Permission, error) {
	var permission model.Permission
	if err := database.DB.First(&permission, id).Error; err != nil {
		return nil, err
	}
	return &permission, nil
}

// GetPermissions 获取所有权限
func (s *rbacService) GetPermissions() ([]model.Permission, error) {
	var permissions []model.Permission
	if err := database.DB.Find(&permissions).Error; err != nil {
		return nil, err
	}
	return permissions, nil
}

// UpdatePermission 更新权限信息
func (s *rbacService) UpdatePermission(id uuid.UUID, updates map[string]interface{}) error {
	return database.DB.Model(&model.Permission{}).Where("id = ?", id).Updates(updates).Error
}

// DeletePermission 删除权限
func (s *rbacService) DeletePermission(id uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 检查是否有角色拥有此权限
		var rolePermissionCount int64
		tx.Table("role_permissions").Where("permission_id = ?", id).Count(&rolePermissionCount)
		if rolePermissionCount > 0 {
			return errors.New("无法删除：有角色拥有此权限")
		}

		// 从Casbin中移除权限相关的所有策略
		permission := &model.Permission{}
		if err := tx.First(permission, id).Error; nil == err {
			// 尝试从Casbin中删除所有相关的策略规则
			s.enforcer.RemoveFilteredPolicy(1, permission.Code) // 假设权限代码作为obj使用
		}

		// 删除权限
		return tx.Delete(&model.Permission{}, id).Error
	})
}

// AssignPermissionToRole 为角色分配权限
func (s *rbacService) AssignPermissionToRole(roleID uuid.UUID, permissionID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 检查角色和权限是否存在
		var role model.Role
		if err := tx.First(&role, roleID).Error; err != nil {
			return errors.New("角色不存在")
		}

		var permission model.Permission
		if err := tx.First(&permission, permissionID).Error; err != nil {
			return errors.New("权限不存在")
		}

		// 添加角色权限关联
		if err := tx.Exec("INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
			roleID, permissionID).Error; err != nil {
			return err
		}

		// 在Casbin中添加策略
		roleIDStr := roleID.String()
		permissionCode := permission.Code
		_, err := s.enforcer.AddPolicy(roleIDStr, permissionCode, "*") // 允许所有动作
		if err != nil {
			return err
		}

		return nil
	})
}

// RemovePermissionFromRole 从角色移除权限
func (s *rbacService) RemovePermissionFromRole(roleID uuid.UUID, permissionID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 移除角色权限关联
		if err := tx.Exec("DELETE FROM role_permissions WHERE role_id = ? AND permission_id = ?",
			roleID, permissionID).Error; err != nil {
			return err
		}

		// 从Casbin中移除策略
		var permission model.Permission
		if err := tx.First(&permission, permissionID).Error; err != nil {
			return err
		}

		roleIDStr := roleID.String()
		permissionCode := permission.Code
		_, err := s.enforcer.RemovePolicy(roleIDStr, permissionCode, "*")
		if err != nil {
			return err
		}

		return nil
	})
}

// HasPermission 检查用户是否有特定权限
func (s *rbacService) HasPermission(userID uuid.UUID, resource, action string) (bool, error) {
	userIDStr := userID.String()
	return s.enforcer.Enforce(userIDStr, resource, action)
}

// GetUserPermissions 获取用户的所有权限
func (s *rbacService) GetUserPermissions(userID uuid.UUID) ([]model.Permission, error) {
	// 直接从数据库获取用户的所有角色
	var roles []model.Role
	err := database.DB.Model(&model.UserRole{}).
		Select("roles.*").
		Joins("JOIN roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", userID).
		Find(&roles).Error
	if err != nil {
		return nil, err
	}

	// 如果 user_roles 表中没有记录，检查用户的 role 字段
	if len(roles) == 0 {
		var user model.User
		if err := database.DB.Select("id, role").Where("id = ?", userID).First(&user).Error; err == nil {
			if user.Role != "" {
				var role model.Role
				if err := database.DB.Where("code = ?", user.Role).First(&role).Error; err == nil {
					roles = append(roles, role)
				}
			}
		}
	}

	// 如果仍然没有角色，返回空数组
	if len(roles) == 0 {
		return []model.Permission{}, nil
	}

	// 收集所有角色ID
	roleIDs := make([]string, len(roles))
	for i, role := range roles {
		roleIDs[i] = role.ID.String()
	}

	// 查询所有角色拥有的权限
	var permissions []model.Permission
	err = database.DB.Joins("JOIN role_permissions ON permissions.id = role_permissions.permission_id").
		Joins("JOIN roles ON role_permissions.role_id = roles.id").
		Where("roles.id IN ?", roleIDs).
		Find(&permissions).Error
	if err != nil {
		return nil, err
	}

	return permissions, nil
}

// GetUserRoles 获取用户的所有角色
func (s *rbacService) GetUserRoles(userID uuid.UUID) ([]model.Role, error) {
	var roles []model.Role

	// 首先尝试从 user_roles 表获取角色
	err := database.DB.Model(&model.UserRole{}).
		Select("roles.*").
		Joins("JOIN roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", userID).
		Find(&roles).Error
	if err != nil {
		return nil, err
	}

	// 如果 user_roles 表中没有记录，检查用户的 role 字段
	if len(roles) == 0 {
		var user model.User
		if err := database.DB.Select("id, role").Where("id = ?", userID).First(&user).Error; err == nil {
			if user.Role != "" {
				var role model.Role
				if err := database.DB.Where("code = ?", user.Role).First(&role).Error; err == nil {
					roles = append(roles, role)
				}
			}
		}
	}

	return roles, nil
}

// Casbin策略管理方法
func (s *rbacService) AddPolicy(sub, obj, act string) (bool, error) {
	return s.enforcer.AddPolicy(sub, obj, act)
}

func (s *rbacService) RemovePolicy(sub, obj, act string) (bool, error) {
	return s.enforcer.RemovePolicy(sub, obj, act)
}

func (s *rbacService) GetPermissionsForUser(userID string) [][]string {
	permissions, _ := s.enforcer.GetPermissionsForUser(userID)
	return permissions
}

func (s *rbacService) GetRolesForUser(userID string) []string {
	roles, _ := s.enforcer.GetRolesForUser(userID)
	return roles
}

func (s *rbacService) GetUsersForRole(roleID string) []string {
	users, _ := s.enforcer.GetUsersForRole(roleID)
	return users
}

func (s *rbacService) AddRoleForUser(userID, roleID string) (bool, error) {
	return s.enforcer.AddRoleForUser(userID, roleID)
}

func (s *rbacService) DeleteRoleForUser(userID, roleID string) (bool, error) {
	return s.enforcer.DeleteRoleForUser(userID, roleID)
}
