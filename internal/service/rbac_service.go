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

// RBACService RBAC鏉冮檺绠＄悊鏈嶅姟鎺ュ彛
type RBACService interface {
	// 鐢ㄦ埛绠＄悊
	CreateUser(username, password, email, fullName string) (*model.User, error)
	GetUserByID(id uuid.UUID) (*model.User, error)
	GetUserByUsername(username string) (*model.User, error)
	UpdateUser(id uuid.UUID, updates map[string]interface{}) error
	DeleteUser(id uuid.UUID) error

	// 瑙掕壊绠＄悊
	CreateRole(name, description, roleType, code string, parentID *uuid.UUID) (*model.Role, error)
	GetRoleByID(id uuid.UUID) (*model.Role, error)
	GetRoles() ([]model.Role, error)
	UpdateRole(id uuid.UUID, updates map[string]interface{}) error
	DeleteRole(id uuid.UUID) error
	AssignRoleToUser(userID, roleID uuid.UUID) error
	RemoveRoleFromUser(userID, roleID uuid.UUID) error

	// 鏉冮檺绠＄悊
	CreatePermission(name, code, resourceType, method, path, description string) (*model.Permission, error)
	GetPermissionByID(id uuid.UUID) (*model.Permission, error)
	GetPermissions() ([]model.Permission, error)
	UpdatePermission(id uuid.UUID, updates map[string]interface{}) error
	DeletePermission(id uuid.UUID) error
	AssignPermissionToRole(roleID uuid.UUID, permissionID uuid.UUID) error
	RemovePermissionFromRole(roleID uuid.UUID, permissionID uuid.UUID) error

	// 鏉冮檺妫€鏌?
	HasPermission(userID uuid.UUID, resource, action string) (bool, error)
	GetUserPermissions(userID uuid.UUID) ([]model.Permission, error)
	GetUserRoles(userID uuid.UUID) ([]model.Role, error)

	// Casbin绛栫暐绠＄悊
	AddPolicy(sub, obj, act string) (bool, error)
	RemovePolicy(sub, obj, act string) (bool, error)
	GetPermissionsForUser(userID string) [][]string
	GetRolesForUser(userID string) []string
	GetUsersForRole(roleID string) []string
	AddRoleForUser(userID, roleID string) (bool, error)
	DeleteRoleForUser(userID, roleID string) (bool, error)
}

// rbacService RBAC鏈嶅姟瀹炵幇
type rbacService struct {
	enforcer *casbin.Enforcer
}

// NewRBACService 鍒涘缓RBAC鏈嶅姟瀹炰緥
func NewRBACService() (RBACService, error) {
	// 鍒濆鍖朇asbin閫傞厤鍣?
	adapter, err := gormadapter.NewAdapterByDB(database.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to create casbin adapter: %w", err)
	}

	// 鍒濆鍖朇asbin Enforcer
	e, err := casbin.NewEnforcer("conf/rbac_model.conf", adapter)
	if err != nil {
		return nil, fmt.Errorf("failed to create casbin enforcer: %w", err)
	}

	// 鍔犺浇绛栫暐
	if err := e.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("failed to load casbin policy: %w", err)
	}

	return &rbacService{
		enforcer: e,
	}, nil
}

// CreateUser 鍒涘缓鐢ㄦ埛
func (s *rbacService) CreateUser(username, password, email, fullName string) (*model.User, error) {
	// 妫€鏌ョ敤鎴锋槸鍚﹀凡瀛樺湪
	var existingUser model.User
	err := database.DB.Where("username = ? OR email = ?", username, email).First(&existingUser).Error
	if err == nil {
		return nil, errors.New("用户名或邮箱已存在")
	}

	// 鐢熸垚瀵嗙爜鍝堝笇
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, err
	}

	// 鍒涘缓鐢ㄦ埛
	user := &model.User{
		Username:     username,
		Email:        email,
		FullName:     fullName,
		PasswordHash: string(hashedPassword),
		Status:       "active",
		Role:         "user", // 榛樿瑙掕壊涓烘櫘閫氱敤鎴?
		FreeChecks:   2,
	}

	if err := database.DB.Create(user).Error; err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserByID 鏍规嵁ID鑾峰彇鐢ㄦ埛
func (s *rbacService) GetUserByID(id uuid.UUID) (*model.User, error) {
	var user model.User
	if err := database.DB.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByUsername 鏍规嵁鐢ㄦ埛鍚嶈幏鍙栫敤鎴?
func (s *rbacService) GetUserByUsername(username string) (*model.User, error) {
	var user model.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateUser 鏇存柊鐢ㄦ埛淇℃伅
func (s *rbacService) UpdateUser(id uuid.UUID, updates map[string]interface{}) error {
	return database.DB.Model(&model.User{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteUser 鍒犻櫎鐢ㄦ埛
func (s *rbacService) DeleteUser(id uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 浠嶤asbin涓Щ闄ょ敤鎴风殑鎵€鏈夌瓥鐣?
		userIDStr := id.String()
		s.enforcer.DeleteUser(userIDStr)

		// 杞垹闄ょ敤鎴?
		return tx.Model(&model.User{}).Where("id = ?", id).Update("status", "deleted").Error
	})
}

// CreateRole 鍒涘缓瑙掕壊
func (s *rbacService) CreateRole(name, description, roleType, code string, parentID *uuid.UUID) (*model.Role, error) {
	// 妫€鏌ヨ鑹蹭唬鐮佹槸鍚﹀凡瀛樺湪
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

// GetRoleByID 鏍规嵁ID鑾峰彇瑙掕壊
func (s *rbacService) GetRoleByID(id uuid.UUID) (*model.Role, error) {
	var role model.Role
	if err := database.DB.Preload("Parent").Preload("Children").First(&role, id).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

// GetRoles 鑾峰彇鎵€鏈夎鑹?
func (s *rbacService) GetRoles() ([]model.Role, error) {
	var roles []model.Role
	if err := database.DB.Preload("Parent").Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// UpdateRole 鏇存柊瑙掕壊淇℃伅
func (s *rbacService) UpdateRole(id uuid.UUID, updates map[string]interface{}) error {
	return database.DB.Model(&model.Role{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteRole 鍒犻櫎瑙掕壊
func (s *rbacService) DeleteRole(id uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 妫€鏌ユ槸鍚︽湁鐢ㄦ埛姝ｅ湪浣跨敤姝よ鑹?
		var userRoleCount int64
		tx.Table("user_roles").Where("role_id = ?", id).Count(&userRoleCount)
		if userRoleCount > 0 {
			return errors.New("无法删除：有用户正在使用该角色")
		}

		// 浠嶤asbin涓Щ闄よ鑹茬殑鎵€鏈夌瓥鐣?
		roleIDStr := id.String()
		s.enforcer.DeleteRole(roleIDStr)

		// 鍒犻櫎瑙掕壊
		return tx.Delete(&model.Role{}, id).Error
	})
}

// AssignRoleToUser 涓虹敤鎴峰垎閰嶈鑹?
func (s *rbacService) AssignRoleToUser(userID, roleID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 妫€鏌ョ敤鎴峰拰瑙掕壊鏄惁瀛樺湪
		var user model.User
		if err := tx.First(&user, userID).Error; err != nil {
			return errors.New("用户不存在")
		}

		var role model.Role
		if err := tx.First(&role, roleID).Error; err != nil {
			return errors.New("角色不存在")
		}

		// 娣诲姞鐢ㄦ埛瑙掕壊鍏宠仈
		if err := tx.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
			userID, roleID).Error; err != nil {
			return err
		}

		// 鍦–asbin涓坊鍔犺鑹插垎閰?
		userIDStr := userID.String()
		roleIDStr := roleID.String()
		_, err := s.enforcer.AddRoleForUser(userIDStr, roleIDStr)
		if err != nil {
			return err
		}

		// 鍚屾鏇存柊鐢ㄦ埛鐨勪富瑙掕壊锛堝鏋滈渶瑕侊級
		if user.Role == "user" || user.Role == "" {
			tx.Model(&user).Update("role", role.Code)
		}

		return nil
	})
}

// RemoveRoleFromUser 浠庣敤鎴风Щ闄よ鑹?
func (s *rbacService) RemoveRoleFromUser(userID, roleID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 绉婚櫎鐢ㄦ埛瑙掕壊鍏宠仈
		if err := tx.Exec("DELETE FROM user_roles WHERE user_id = ? AND role_id = ?",
			userID, roleID).Error; err != nil {
			return err
		}

		// 浠嶤asbin涓Щ闄よ鑹插垎閰?
		userIDStr := userID.String()
		roleIDStr := roleID.String()
		_, err := s.enforcer.DeleteRoleForUser(userIDStr, roleIDStr)
		if err != nil {
			return err
		}

		return nil
	})
}

// CreatePermission 鍒涘缓鏉冮檺
func (s *rbacService) CreatePermission(name, code, resourceType, method, path, description string) (*model.Permission, error) {
	// 妫€鏌ユ潈闄愪唬鐮佹槸鍚﹀凡瀛樺湪
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

// GetPermissionByID 鏍规嵁ID鑾峰彇鏉冮檺
func (s *rbacService) GetPermissionByID(id uuid.UUID) (*model.Permission, error) {
	var permission model.Permission
	if err := database.DB.First(&permission, id).Error; err != nil {
		return nil, err
	}
	return &permission, nil
}

// GetPermissions 鑾峰彇鎵€鏈夋潈闄?
func (s *rbacService) GetPermissions() ([]model.Permission, error) {
	var permissions []model.Permission
	if err := database.DB.Find(&permissions).Error; err != nil {
		return nil, err
	}
	return permissions, nil
}

// UpdatePermission 鏇存柊鏉冮檺淇℃伅
func (s *rbacService) UpdatePermission(id uuid.UUID, updates map[string]interface{}) error {
	return database.DB.Model(&model.Permission{}).Where("id = ?", id).Updates(updates).Error
}

// DeletePermission 鍒犻櫎鏉冮檺
func (s *rbacService) DeletePermission(id uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 妫€鏌ユ槸鍚︽湁瑙掕壊鎷ユ湁姝ゆ潈闄?
		var rolePermissionCount int64
		tx.Table("role_permissions").Where("permission_id = ?", id).Count(&rolePermissionCount)
		if rolePermissionCount > 0 {
			return errors.New("无法删除：有角色拥有此权限")
		}

		// 浠嶤asbin涓Щ闄ゆ潈闄愮浉鍏崇殑鎵€鏈夌瓥鐣?
		permission := &model.Permission{}
		if err := tx.First(permission, id).Error; nil == err {
			// 灏濊瘯浠嶤asbin涓垹闄ゆ墍鏈夌浉鍏崇殑绛栫暐瑙勫垯
			s.enforcer.RemoveFilteredPolicy(1, permission.Code) // 鍋囪鏉冮檺浠ｇ爜浣滀负obj浣跨敤
		}

		// 鍒犻櫎鏉冮檺
		return tx.Delete(&model.Permission{}, id).Error
	})
}

// AssignPermissionToRole 涓鸿鑹插垎閰嶆潈闄?
func (s *rbacService) AssignPermissionToRole(roleID uuid.UUID, permissionID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 妫€鏌ヨ鑹插拰鏉冮檺鏄惁瀛樺湪
		var role model.Role
		if err := tx.First(&role, roleID).Error; err != nil {
			return errors.New("角色不存在")
		}

		var permission model.Permission
		if err := tx.First(&permission, permissionID).Error; err != nil {
			return errors.New("权限不存在")
		}

		// 娣诲姞瑙掕壊鏉冮檺鍏宠仈
		if err := tx.Exec("INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
			roleID, permissionID).Error; err != nil {
			return err
		}

		// 鍦–asbin涓坊鍔犵瓥鐣?
		roleIDStr := roleID.String()
		permissionCode := permission.Code
		_, err := s.enforcer.AddPolicy(roleIDStr, permissionCode, "*") // 鍏佽鎵€鏈夊姩浣?
		if err != nil {
			return err
		}

		return nil
	})
}

// RemovePermissionFromRole 浠庤鑹茬Щ闄ゆ潈闄?
func (s *rbacService) RemovePermissionFromRole(roleID uuid.UUID, permissionID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// 绉婚櫎瑙掕壊鏉冮檺鍏宠仈
		if err := tx.Exec("DELETE FROM role_permissions WHERE role_id = ? AND permission_id = ?",
			roleID, permissionID).Error; err != nil {
			return err
		}

		// 浠嶤asbin涓Щ闄ょ瓥鐣?
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

// HasPermission 妫€鏌ョ敤鎴锋槸鍚︽湁鐗瑰畾鏉冮檺
func (s *rbacService) HasPermission(userID uuid.UUID, resource, action string) (bool, error) {
	userIDStr := userID.String()
	return s.enforcer.Enforce(userIDStr, resource, action)
}

// GetUserPermissions 获取用户的所有权限
func (s *rbacService) GetUserPermissions(userID uuid.UUID) ([]model.Permission, error) {
	// 先拿角色继承权限
	var roles []model.Role
	err := database.DB.Model(&model.UserRole{}).
		Select("roles.*").
		Joins("JOIN roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", userID).
		Find(&roles).Error
	if err != nil {
		return nil, err
	}

	// 如果 user_roles 没有记录，回退到 user.role 字段
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

	permissionMap := make(map[uuid.UUID]model.Permission)

	// 收集角色权限
	if len(roles) > 0 {
		roleIDs := make([]uuid.UUID, 0, len(roles))
		for _, role := range roles {
			roleIDs = append(roleIDs, role.ID)
		}

		var rolePermissions []model.Permission
		err = database.DB.Joins("JOIN role_permissions ON permissions.id = role_permissions.permission_id").
			Where("role_permissions.role_id IN ?", roleIDs).
			Find(&rolePermissions).Error
		if err != nil {
			return nil, err
		}

		for _, perm := range rolePermissions {
			permissionMap[perm.ID] = perm
		}
	}

	// 叠加用户直接权限
	var directPermissions []model.Permission
	err = database.DB.Joins("JOIN user_permissions ON permissions.id = user_permissions.permission_id").
		Where("user_permissions.user_id = ?", userID).
		Find(&directPermissions).Error
	if err != nil {
		return nil, err
	}
	for _, perm := range directPermissions {
		permissionMap[perm.ID] = perm
	}

	permissions := make([]model.Permission, 0, len(permissionMap))
	for _, perm := range permissionMap {
		permissions = append(permissions, perm)
	}

	return permissions, nil
}

// GetUserRoles 鑾峰彇鐢ㄦ埛鐨勬墍鏈夎鑹?
func (s *rbacService) GetUserRoles(userID uuid.UUID) ([]model.Role, error) {
	var roles []model.Role

	// 棣栧厛灏濊瘯浠?user_roles 琛ㄨ幏鍙栬鑹?
	err := database.DB.Model(&model.UserRole{}).
		Select("roles.*").
		Joins("JOIN roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", userID).
		Find(&roles).Error
	if err != nil {
		return nil, err
	}

	// 濡傛灉 user_roles 琛ㄤ腑娌℃湁璁板綍锛屾鏌ョ敤鎴风殑 role 瀛楁
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

// Casbin绛栫暐绠＄悊鏂规硶
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
