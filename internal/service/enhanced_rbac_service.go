package service

import (
	"sync"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
)

type EnhancedRBACService interface {
	RBACService

	// 获取角色的所有菜单（权限）
	GetRoleAllMenus(roleID uuid.UUID) ([]model.Menu, error)
	// 获取角色继承的菜单（权限）
	GetRoleInheritedMenus(roleID uuid.UUID) ([]model.Menu, error)
	// 获取用户的所有菜单（权限）
	GetUserAllMenus(userID uuid.UUID) ([]model.Menu, error)
	// 刷新角色缓存
	RefreshRoleCache(roleID uuid.UUID) error
	// 获取角色层级
	GetRoleHierarchy(roleID uuid.UUID) ([]uuid.UUID, error)
}

type enhancedRBACService struct {
	*rbacService
	menuCache  map[uuid.UUID][]model.Menu
	cacheMutex sync.RWMutex
}

func NewEnhancedRBACService() (EnhancedRBACService, error) {
	baseService, err := NewRBACService()
	if err != nil {
		return nil, err
	}

	return &enhancedRBACService{
		rbacService: baseService.(*rbacService),
		menuCache:   make(map[uuid.UUID][]model.Menu),
		cacheMutex:  sync.RWMutex{},
	}, nil
}

func (s *enhancedRBACService) GetRoleAllMenus(roleID uuid.UUID) ([]model.Menu, error) {
	s.cacheMutex.RLock()
	if cached, ok := s.menuCache[roleID]; ok {
		s.cacheMutex.RUnlock()
		return cached, nil
	}
	s.cacheMutex.RUnlock()

	menus, err := s.calculateRoleMenus(roleID)
	if err != nil {
		return nil, err
	}

	s.cacheMutex.Lock()
	s.menuCache[roleID] = menus
	s.cacheMutex.Unlock()

	return menus, nil
}

func (s *enhancedRBACService) GetRoleInheritedMenus(roleID uuid.UUID) ([]model.Menu, error) {
	return s.calculateRoleMenus(roleID)
}

func (s *enhancedRBACService) GetUserAllMenus(userID uuid.UUID) ([]model.Menu, error) {
	// 获取用户所有角色
	userRoles, err := s.GetUserRoles(userID)
	if err != nil {
		return nil, err
	}

	menuMap := make(map[uuid.UUID]model.Menu)
	for _, role := range userRoles {
		menus, err := s.GetRoleAllMenus(role.ID)
		if err != nil {
			return nil, err
		}
		for _, menu := range menus {
			menuMap[menu.ID] = menu
		}
	}

	menus := make([]model.Menu, 0, len(menuMap))
	for _, menu := range menuMap {
		menus = append(menus, menu)
	}

	return menus, nil
}

func (s *enhancedRBACService) RefreshRoleCache(roleID uuid.UUID) error {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	menus, err := s.calculateRoleMenus(roleID)
	if err != nil {
		return err
	}

	s.menuCache[roleID] = menus
	return nil
}

func (s *enhancedRBACService) GetRoleHierarchy(roleID uuid.UUID) ([]uuid.UUID, error) {
	roleIDs := make([]uuid.UUID, 0)

	var collectRoleHierarchy func(roleID uuid.UUID) error
	collectRoleHierarchy = func(currentID uuid.UUID) error {
		roleIDs = append(roleIDs, currentID)

		var role model.Role
		if err := database.DB.Preload("Parent").First(&role, currentID).Error; err != nil {
			return err
		}

		if role.ParentID != nil {
			return collectRoleHierarchy(*role.ParentID)
		}

		return nil
	}

	if err := collectRoleHierarchy(roleID); err != nil {
		return nil, err
	}

	return roleIDs, nil
}

func (s *enhancedRBACService) calculateRoleMenus(roleID uuid.UUID) ([]model.Menu, error) {
	menuMap := make(map[string]model.Menu)

	var collectFromRole func(roleID uuid.UUID) error
	collectFromRole = func(currentID uuid.UUID) error {
		var role model.Role
		if err := database.DB.Preload("Menus").First(&role, currentID).Error; err != nil {
			return err
		}

		for _, menu := range role.Menus {
			if _, exists := menuMap[menu.Permission]; !exists && menu.Permission != "" {
				menuMap[menu.Permission] = menu
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

	menus := make([]model.Menu, 0, len(menuMap))
	for _, menu := range menuMap {
		menus = append(menus, menu)
	}

	return menus, nil
}

func (s *enhancedRBACService) HasPermission(userID uuid.UUID, resource, action string) (bool, error) {
	menus, err := s.GetUserAllMenus(userID)
	if err != nil {
		return false, err
	}

	for _, menu := range menus {
		if menu.MenuType != "api" {
			continue
		}

		if s.matchMenu(menu, resource, action) {
			return true, nil
		}
	}

	return false, nil
}

func (s *enhancedRBACService) matchMenu(menu model.Menu, resource, action string) bool {
	if menu.Permission == "" {
		return false
	}

	// 支持多种权限格式匹配
	// 格式 1: resource:action (如：user:list)
	if menu.Permission == resource+":"+action {
		return true
	}

	// 格式 2: 完整路径匹配
	if menu.Path == resource {
		return true
	}

	// 格式 3: 通配符匹配
	if menu.Permission == "*" || menu.Permission == "admin:*" {
		return true
	}

	return false
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
