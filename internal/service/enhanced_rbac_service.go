package service

import (
	"sync"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
)

type EnhancedRBACService interface {
	RBACService

	GetRoleAllPermissions(roleID uuid.UUID) ([]model.Permission, error)
	GetRoleInheritedPermissions(roleID uuid.UUID) ([]model.Permission, error)
	GetUserAllPermissions(userID uuid.UUID) ([]model.Permission, error)
	RefreshRoleCache(roleID uuid.UUID) error
	GetRoleHierarchy(roleID uuid.UUID) ([]uuid.UUID, error)
}

type enhancedRBACService struct {
	*rbacService
	permissionCache map[uuid.UUID][]model.Permission
	cacheMutex      sync.RWMutex
}

func NewEnhancedRBACService() (EnhancedRBACService, error) {
	baseService, err := NewRBACService()
	if err != nil {
		return nil, err
	}

	return &enhancedRBACService{
		rbacService:     baseService.(*rbacService),
		permissionCache: make(map[uuid.UUID][]model.Permission),
		cacheMutex:      sync.RWMutex{},
	}, nil
}

func (s *enhancedRBACService) GetRoleAllPermissions(roleID uuid.UUID) ([]model.Permission, error) {
	s.cacheMutex.RLock()
	if cached, ok := s.permissionCache[roleID]; ok {
		s.cacheMutex.RUnlock()
		return cached, nil
	}
	s.cacheMutex.RUnlock()

	permissions, err := s.calculateRolePermissions(roleID)
	if err != nil {
		return nil, err
	}

	s.cacheMutex.Lock()
	s.permissionCache[roleID] = permissions
	s.cacheMutex.Unlock()

	return permissions, nil
}

func (s *enhancedRBACService) GetRoleInheritedPermissions(roleID uuid.UUID) ([]model.Permission, error) {
	return s.calculateRolePermissions(roleID)
}

func (s *enhancedRBACService) GetUserAllPermissions(userID uuid.UUID) ([]model.Permission, error) {
	roles, err := s.GetUserRoles(userID)
	if err != nil {
		return nil, err
	}

	permissionMap := make(map[string]model.Permission)
	visitedRoles := make(map[uuid.UUID]bool)

	var collectPermissions func(roleID uuid.UUID) error
	collectPermissions = func(roleID uuid.UUID) error {
		if visitedRoles[roleID] {
			return nil
		}
		visitedRoles[roleID] = true

		rolePermissions, err := s.GetRoleAllPermissions(roleID)
		if err != nil {
			return err
		}

		for _, perm := range rolePermissions {
			if _, exists := permissionMap[perm.Code]; !exists {
				permissionMap[perm.Code] = perm
			}
		}

		var role model.Role
		if err := database.DB.First(&role, roleID).Error; err != nil {
			return err
		}

		if role.ParentID != nil {
			if err := collectPermissions(*role.ParentID); err != nil {
				return err
			}
		}

		return nil
	}

	for _, role := range roles {
		if err := collectPermissions(role.ID); err != nil {
			return nil, err
		}
	}

	permissions := make([]model.Permission, 0, len(permissionMap))
	for _, perm := range permissionMap {
		permissions = append(permissions, perm)
	}

	return permissions, nil
}

func (s *enhancedRBACService) RefreshRoleCache(roleID uuid.UUID) error {
	s.cacheMutex.Lock()
	delete(s.permissionCache, roleID)
	s.cacheMutex.Unlock()

	_, err := s.calculateRolePermissions(roleID)
	return err
}

func (s *enhancedRBACService) GetRoleHierarchy(roleID uuid.UUID) ([]uuid.UUID, error) {
	roleIDs := []uuid.UUID{}

	var collectParents func(roleID uuid.UUID) error
	collectParents = func(currentID uuid.UUID) error {
		var role model.Role
		if err := database.DB.First(&role, currentID).Error; err != nil {
			return err
		}

		roleIDs = append(roleIDs, currentID)

		if role.ParentID != nil {
			if err := collectParents(*role.ParentID); err != nil {
				return err
			}
		}

		return nil
	}

	if err := collectParents(roleID); err != nil {
		return nil, err
	}

	return roleIDs, nil
}

func (s *enhancedRBACService) calculateRolePermissions(roleID uuid.UUID) ([]model.Permission, error) {
	permissionMap := make(map[string]model.Permission)

	var collectFromRole func(roleID uuid.UUID) error
	collectFromRole = func(currentID uuid.UUID) error {
		var role model.Role
		if err := database.DB.Preload("Permissions").First(&role, currentID).Error; err != nil {
			return err
		}

		for _, perm := range role.Permissions {
			if _, exists := permissionMap[perm.Code]; !exists {
				permissionMap[perm.Code] = perm
			}
		}

		if role.ParentID != nil {
			if err := collectFromRole(*role.ParentID); err != nil {
				return err
			}
		}

		return nil
	}

	if err := collectFromRole(roleID); err != nil {
		return nil, err
	}

	permissions := make([]model.Permission, 0, len(permissionMap))
	for _, perm := range permissionMap {
		permissions = append(permissions, perm)
	}

	return permissions, nil
}

func (s *enhancedRBACService) HasPermission(userID uuid.UUID, resource, action string) (bool, error) {
	permissions, err := s.GetUserAllPermissions(userID)
	if err != nil {
		return false, err
	}

	for _, perm := range permissions {
		if perm.ResourceType != "api" {
			continue
		}

		if s.matchPermission(perm, resource, action) {
			return true, nil
		}
	}

	return false, nil
}

func (s *enhancedRBACService) matchPermission(perm model.Permission, resource, action string) bool {
	permResource := perm.Code
	if idx := indexOf(permResource, ':'); idx != -1 {
		permResource = permResource[:idx]
	}

	actionMap := map[string]string{
		"read":   "GET",
		"create": "POST",
		"update": "PUT",
		"delete": "DELETE",
	}

	expectedAction := actionMap[action]
	if expectedAction == "" {
		expectedAction = action
	}

	if perm.Method == "*" || perm.Method == expectedAction {
		if permResource == resource || resource == "*" {
			return true
		}
	}

	return false
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
