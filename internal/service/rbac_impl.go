package service

import (
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type rbacService struct{}

func NewRBACService() (RBACService, error) {
	return &rbacService{}, nil
}

func (s *rbacService) CreateUser(username, email, password string) (*model.User, error) {
	user := &model.User{
		Username: username,
		Email:    email,
	}
	err := database.DB.Create(user).Error
	return user, err
}

func (s *rbacService) GetUserByID(id uuid.UUID) (*model.User, error) {
	var user model.User
	err := database.DB.First(&user, id).Error
	return &user, err
}

func (s *rbacService) GetUserByUsername(username string) (*model.User, error) {
	var user model.User
	err := database.DB.Where("username = ?", username).First(&user).Error
	return &user, err
}

func (s *rbacService) UpdateUser(id uuid.UUID, updates map[string]interface{}) error {
	return database.DB.Model(&model.User{}).Where("id = ?", id).Updates(updates).Error
}

func (s *rbacService) DeleteUser(id uuid.UUID) error {
	return database.DB.Delete(&model.User{}, id).Error
}

func (s *rbacService) CreateRole(name, description, roleType, code string, parentID *uuid.UUID) (*model.Role, error) {
	role := &model.Role{
		Name:        name,
		Description: description,
		Type:        roleType,
		Code:        code,
		ParentID:    parentID,
	}
	err := database.DB.Create(role).Error
	return role, err
}

func (s *rbacService) GetRoleByID(id uuid.UUID) (*model.Role, error) {
	var role model.Role
	err := database.DB.First(&role, id).Error
	return &role, err
}

func (s *rbacService) GetRoles() ([]model.Role, error) {
	var roles []model.Role
	err := database.DB.Find(&roles).Error
	return roles, err
}

func (s *rbacService) UpdateRole(id uuid.UUID, updates map[string]interface{}) error {
	return database.DB.Model(&model.Role{}).Where("id = ?", id).Updates(updates).Error
}

func (s *rbacService) DeleteRole(id uuid.UUID) error {
	return database.DB.Delete(&model.Role{}, id).Error
}

func (s *rbacService) AssignRoleToUser(userID, roleID uuid.UUID) error {
	userRole := model.UserRole{
		UserID: userID,
		RoleID: roleID,
	}
	return database.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&userRole).Error
}

func (s *rbacService) RemoveRoleFromUser(userID, roleID uuid.UUID) error {
	return database.DB.Where("user_id = ? AND role_id = ?", userID, roleID).Delete(&model.UserRole{}).Error
}

func (s *rbacService) CreatePermission(name, code, resourceType, method, path, description string) (*model.Permission, error) {
	permission := &model.Permission{
		Name:         name,
		Code:         code,
		ResourceType: resourceType,
		Method:       method,
		Path:         path,
		Description:  description,
	}
	err := database.DB.Create(permission).Error
	return permission, err
}

func (s *rbacService) GetPermissionByID(id uuid.UUID) (*model.Permission, error) {
	var permission model.Permission
	err := database.DB.First(&permission, id).Error
	return &permission, err
}

func (s *rbacService) GetPermissions() ([]model.Permission, error) {
	var permissions []model.Permission
	err := database.DB.Find(&permissions).Error
	return permissions, err
}

func (s *rbacService) UpdatePermission(id uuid.UUID, updates map[string]interface{}) error {
	return database.DB.Model(&model.Permission{}).Where("id = ?", id).Updates(updates).Error
}

func (s *rbacService) DeletePermission(id uuid.UUID) error {
	return database.DB.Delete(&model.Permission{}, id).Error
}

func (s *rbacService) AssignPermissionToRole(roleID uuid.UUID, permissionID uuid.UUID) error {
	rolePermission := model.RolePermission{
		RoleID:       roleID,
		PermissionID: permissionID,
	}
	return database.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&rolePermission).Error
}

func (s *rbacService) RemovePermissionFromRole(roleID uuid.UUID, permissionID uuid.UUID) error {
	return database.DB.Where("role_id = ? AND permission_id = ?", roleID, permissionID).Delete(&model.RolePermission{}).Error
}

func (s *rbacService) HasPermission(userID uuid.UUID, resource, action string) (bool, error) {
	permissions, err := s.GetUserPermissions(userID)
	if err != nil {
		return false, err
	}

	normalizedResource := strings.TrimSpace(strings.ToLower(resource))
	normalizedAction := normalizeAction(action)

	for _, perm := range permissions {
		if perm.ResourceType != "" && strings.ToLower(perm.ResourceType) != "api" {
			continue
		}

		if !permissionMatchesResource(perm, normalizedResource) {
			continue
		}

		if permissionMatchesAction(perm, normalizedAction) {
			return true, nil
		}
	}

	return false, nil
}

func (s *rbacService) GetUserPermissions(userID uuid.UUID) ([]model.Permission, error) {
	permissions := make([]model.Permission, 0)
	permissionMap := make(map[uuid.UUID]model.Permission)

	roles, err := s.GetUserRoles(userID)
	if err != nil {
		return nil, err
	}

	if len(roles) > 0 {
		roleIDs := make([]uuid.UUID, 0, len(roles))
		for _, role := range roles {
			roleIDs = append(roleIDs, role.ID)
		}

		var rolePermissions []model.Permission
		if err := database.DB.
			Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
			Where("role_permissions.role_id IN ?", roleIDs).
			Find(&rolePermissions).Error; err != nil {
			return nil, err
		}

		for _, perm := range rolePermissions {
			permissionMap[perm.ID] = perm
		}
	}

	var directPermissions []model.Permission
	if err := database.DB.
		Joins("JOIN user_permissions ON user_permissions.permission_id = permissions.id").
		Where("user_permissions.user_id = ?", userID).
		Find(&directPermissions).Error; err != nil {
		return nil, err
	}

	for _, perm := range directPermissions {
		permissionMap[perm.ID] = perm
	}

	for _, perm := range permissionMap {
		permissions = append(permissions, perm)
	}

	return permissions, nil
}

func (s *rbacService) GetUserRoles(userID uuid.UUID) ([]model.Role, error) {
	var roles []model.Role
	err := database.DB.Joins("JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", userID).
		Find(&roles).Error
	return roles, err
}

func (s *rbacService) GetRolePermissions(roleID uuid.UUID) ([]model.Permission, error) {
	var permissions []model.Permission
	err := database.DB.Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Where("role_permissions.role_id = ?", roleID).
		Find(&permissions).Error
	return permissions, err
}

func (s *rbacService) GetUserDirectPermissionIDs(userID uuid.UUID) ([]uuid.UUID, error) {
	var userPermissions []model.UserPermission
	if err := database.DB.Where("user_id = ?", userID).Find(&userPermissions).Error; err != nil {
		return nil, err
	}

	permissionIDs := make([]uuid.UUID, len(userPermissions))
	for i, up := range userPermissions {
		permissionIDs[i] = up.PermissionID
	}

	return permissionIDs, nil
}

func (s *rbacService) AddPolicy(sub, obj, act string) (bool, error) {
	userUUID, err := uuid.Parse(sub)
	if err != nil {
		return false, err
	}

	normalizedCode := strings.TrimSpace(obj)
	if normalizedCode == "" {
		return false, errors.New("permission code cannot be empty")
	}

	normalizedMethod := normalizeMethod(act)

	var permission model.Permission
	err = database.DB.Where("code = ?", normalizedCode).First(&permission).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}

		permission = model.Permission{
			Name:         normalizedCode,
			Code:         normalizedCode,
			ResourceType: "api",
			Method:       normalizedMethod,
			Path:         "",
			Description:  "created by add policy",
		}
		if err := database.DB.Create(&permission).Error; err != nil {
			return false, err
		}
	}

	userPermission := model.UserPermission{
		UserID:       userUUID,
		PermissionID: permission.ID,
	}
	result := database.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&userPermission)
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}

func (s *rbacService) RemovePolicy(sub, obj, act string) (bool, error) {
	userUUID, err := uuid.Parse(sub)
	if err != nil {
		return false, err
	}

	var permission model.Permission
	if err := database.DB.Where("code = ?", strings.TrimSpace(obj)).First(&permission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}

	result := database.DB.Where("user_id = ? AND permission_id = ?", userUUID, permission.ID).
		Delete(&model.UserPermission{})
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}

func (s *rbacService) GetPermissionsForUser(userID string) [][]string {
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return [][]string{}
	}

	permissions, err := s.GetUserPermissions(userUUID)
	if err != nil {
		return [][]string{}
	}

	result := make([][]string, 0, len(permissions))
	for _, perm := range permissions {
		obj := perm.Code
		if strings.TrimSpace(obj) == "" {
			obj = perm.Path
		}
		result = append(result, []string{userID, obj, normalizeMethod(perm.Method)})
	}

	return result
}

func (s *rbacService) GetRolesForUser(userID string) []string {
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return []string{}
	}

	roles, err := s.GetUserRoles(userUUID)
	if err != nil {
		return []string{}
	}

	result := make([]string, 0, len(roles))
	for _, role := range roles {
		result = append(result, role.ID.String())
	}

	return result
}

func (s *rbacService) GetUsersForRole(roleID string) []string {
	roleUUID, err := uuid.Parse(roleID)
	if err != nil {
		return []string{}
	}

	var userRoles []model.UserRole
	if err := database.DB.Where("role_id = ?", roleUUID).Find(&userRoles).Error; err != nil {
		return []string{}
	}

	result := make([]string, 0, len(userRoles))
	for _, ur := range userRoles {
		result = append(result, ur.UserID.String())
	}

	return result
}

func (s *rbacService) AddRoleForUser(userID, roleID string) (bool, error) {
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return false, err
	}

	roleUUID, err := uuid.Parse(roleID)
	if err != nil {
		return false, err
	}

	err = s.AssignRoleToUser(userUUID, roleUUID)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (s *rbacService) DeleteRoleForUser(userID, roleID string) (bool, error) {
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return false, err
	}

	roleUUID, err := uuid.Parse(roleID)
	if err != nil {
		return false, err
	}

	err = s.RemoveRoleFromUser(userUUID, roleUUID)
	if err != nil {
		return false, err
	}

	return true, nil
}

func normalizeAction(action string) string {
	a := strings.TrimSpace(strings.ToUpper(action))
	switch a {
	case "READ", "LIST", "GET":
		return "GET"
	case "CREATE", "POST":
		return "POST"
	case "UPDATE", "PUT":
		return "PUT"
	case "DELETE":
		return "DELETE"
	case "PATCH":
		return "PATCH"
	case "*":
		return "*"
	default:
		return a
	}
}

func normalizeMethod(method string) string {
	if strings.TrimSpace(method) == "" {
		return "*"
	}
	return strings.ToUpper(strings.TrimSpace(method))
}

func permissionMatchesResource(perm model.Permission, resource string) bool {
	if resource == "" || resource == "*" {
		return true
	}

	codeResource := perm.Code
	if idx := strings.Index(codeResource, ":"); idx > 0 {
		codeResource = codeResource[:idx]
	}
	codeResource = strings.ToLower(strings.TrimSpace(codeResource))

	if codeResource == "*" || codeResource == resource {
		return true
	}

	path := strings.ToLower(strings.TrimSpace(perm.Path))
	if path == "" {
		return false
	}

	if strings.Contains(path, resource) {
		return true
	}

	return false
}

func permissionMatchesAction(perm model.Permission, action string) bool {
	if action == "" || action == "*" {
		return true
	}

	method := normalizeMethod(perm.Method)
	if method == "*" || method == action {
		return true
	}

	parts := strings.Split(strings.ToLower(strings.TrimSpace(perm.Code)), ":")
	if len(parts) >= 2 {
		last := strings.ToUpper(parts[len(parts)-1])
		if normalizeAction(last) == action {
			return true
		}
	}

	return false
}
