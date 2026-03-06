package service

import (
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
)

// RBACService RBAC 服务接口
type RBACService interface {
	// 用户管理
	CreateUser(username, email, password string) (*model.User, error)
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
	GetRolePermissions(roleID uuid.UUID) ([]model.Permission, error)
	GetUserDirectPermissionIDs(userID uuid.UUID) ([]uuid.UUID, error)

	// Casbin 策略管理
	AddPolicy(sub, obj, act string) (bool, error)
	RemovePolicy(sub, obj, act string) (bool, error)
	GetPermissionsForUser(userID string) [][]string
	GetRolesForUser(userID string) []string
	GetUsersForRole(roleID string) []string
	AddRoleForUser(userID, roleID string) (bool, error)
	DeleteRoleForUser(userID, roleID string) (bool, error)
}
