package service

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MenuService 菜单服务接口
type MenuService interface {
	// 创建菜单
	CreateMenu(menu *model.MenuCreateRequest) (*model.Menu, error)

	// 更新菜单
	UpdateMenu(id uuid.UUID, menu *model.MenuUpdateRequest) (*model.Menu, error)

	// 删除菜单
	DeleteMenu(id uuid.UUID) error

	// 获取菜单详情
	GetMenuByID(id uuid.UUID) (*model.Menu, error)

	// 获取所有菜单
	GetAllMenus() ([]model.Menu, error)

	// 获取菜单树
	GetMenuTree() ([]model.MenuTreeResponse, error)

	// 获取用户菜单树
	GetUserMenuTree(userID uuid.UUID) ([]model.MenuTreeResponse, error)

	// 为角色分配菜单
	AssignMenusToRole(roleID uuid.UUID, menuIDs []uuid.UUID) error

	// 获取角色菜单
	GetRoleMenus(roleID uuid.UUID) ([]model.Menu, error)

	// 从角色移除菜单
	RemoveMenuFromRole(roleID uuid.UUID, menuID uuid.UUID) error
}

type menuService struct {
	db *gorm.DB
}

// NewMenuService 创建菜单服务实例
func NewMenuService() MenuService {
	return &menuService{
		db: database.DB,
	}
}

// CreateMenu 创建菜单
func (s *menuService) CreateMenu(req *model.MenuCreateRequest) (*model.Menu, error) {
	menu := &model.Menu{
		ID:         uuid.New(),
		ParentID:   req.ParentID,
		Name:       req.Name,
		Title:      req.Title,
		Icon:       req.Icon,
		Path:       req.Path,
		Component:  req.Component,
		SortOrder:  req.SortOrder,
		MenuType:   req.MenuType,
		Permission: req.Permission,
		Visible:    req.Visible,
		KeepAlive:  req.KeepAlive,
		Redirect:   req.Redirect,
		Meta:       convertToJSON(req.Meta),
	}

	if err := s.db.Create(menu).Error; err != nil {
		return nil, fmt.Errorf("创建菜单失败：%w", err)
	}

	return menu, nil
}

// UpdateMenu 更新菜单
func (s *menuService) UpdateMenu(id uuid.UUID, req *model.MenuUpdateRequest) (*model.Menu, error) {
	var menu model.Menu
	if err := s.db.First(&menu, id).Error; err != nil {
		return nil, fmt.Errorf("菜单不存在：%w", err)
	}

	// 更新字段
	if req.ParentID != nil {
		menu.ParentID = req.ParentID
	}
	if req.Name != "" {
		menu.Name = req.Name
	}
	if req.Title != "" {
		menu.Title = req.Title
	}
	if req.Icon != "" {
		menu.Icon = req.Icon
	}
	if req.Path != "" {
		menu.Path = req.Path
	}
	if req.Component != "" {
		menu.Component = req.Component
	}
	if req.SortOrder != 0 {
		menu.SortOrder = req.SortOrder
	}
	if req.MenuType != "" {
		menu.MenuType = req.MenuType
	}
	if req.Permission != "" {
		menu.Permission = req.Permission
	}
	if req.Visible {
		menu.Visible = req.Visible
	}
	if req.KeepAlive {
		menu.KeepAlive = req.KeepAlive
	}
	if req.Redirect != "" {
		menu.Redirect = req.Redirect
	}
	if req.Meta != nil {
		menu.Meta = convertToJSON(req.Meta)
	}

	if err := s.db.Save(&menu).Error; err != nil {
		return nil, fmt.Errorf("更新菜单失败：%w", err)
	}

	return &menu, nil
}

// DeleteMenu 删除菜单
func (s *menuService) DeleteMenu(id uuid.UUID) error {
	var menu model.Menu
	if err := s.db.First(&menu, id).Error; err != nil {
		return fmt.Errorf("菜单不存在：%w", err)
	}

	// 检查是否有子菜单
	var count int64
	if err := s.db.Model(&model.Menu{}).Where("parent_id = ?", id).Count(&count).Error; err != nil {
		return fmt.Errorf("检查子菜单失败：%w", err)
	}

	if count > 0 {
		return fmt.Errorf("存在子菜单，无法删除")
	}

	if err := s.db.Delete(&menu).Error; err != nil {
		return fmt.Errorf("删除菜单失败：%w", err)
	}

	return nil
}

// GetMenuByID 获取菜单详情
func (s *menuService) GetMenuByID(id uuid.UUID) (*model.Menu, error) {
	var menu model.Menu
	if err := s.db.Preload("Roles").First(&menu, id).Error; err != nil {
		return nil, fmt.Errorf("获取菜单失败：%w", err)
	}

	return &menu, nil
}

// GetAllMenus 获取所有菜单
func (s *menuService) GetAllMenus() ([]model.Menu, error) {
	var menus []model.Menu
	if err := s.db.Order("sort_order ASC").Find(&menus).Error; err != nil {
		return nil, fmt.Errorf("获取菜单列表失败：%w", err)
	}

	return menus, nil
}

// GetMenuTree 获取菜单树
func (s *menuService) GetMenuTree() ([]model.MenuTreeResponse, error) {
	var menus []model.Menu
	if err := s.db.Order("parent_id, sort_order ASC").Find(&menus).Error; err != nil {
		return nil, fmt.Errorf("获取菜单列表失败：%w", err)
	}

	return s.buildMenuTree(menus, uuid.Nil), nil
}

// GetUserMenuTree 获取用户菜单树（基于 Casbin 权限过滤）
func (s *menuService) GetUserMenuTree(userID uuid.UUID) ([]model.MenuTreeResponse, error) {
	// 获取用户所有角色
	var user model.User
	if err := s.db.First(&user, userID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// 用户不存在，返回空菜单树
			return []model.MenuTreeResponse{}, nil
		}
		return nil, fmt.Errorf("获取用户失败：%w", err)
	}

	// 仅 super_admin 直接返回完整菜单；admin 也走标准权限链路
	superAdminRoleCodes := map[string]bool{"super_admin": true}
	if superAdminRoleCodes[user.Role] {
		return s.GetMenuTree()
	}

	// 获取用户的所有角色
	var roles []model.Role
	if err := s.db.Model(&user).Association("Roles").Find(&roles); err != nil {
		return nil, fmt.Errorf("获取用户角色失败：%w", err)
	}

	// super_admin（user_roles 中任一角色的 code 为 super_admin）：返回完整菜单树
	for _, role := range roles {
		if superAdminRoleCodes[role.Code] {
			return s.GetMenuTree()
		}
	}

	// 获取所有菜单
	var allMenus []model.Menu
	if err := s.db.Order("parent_id, sort_order ASC").Find(&allMenus).Error; err != nil {
		return nil, fmt.Errorf("获取所有菜单失败：%w", err)
	}

	// 普通管理员按数据库 RBAC 权限过滤菜单，和接口鉴权使用同一数据源。
	rbacService, err := NewRBACService()
	if err != nil {
		return nil, fmt.Errorf("初始化 RBAC 服务失败：%w", err)
	}
	permissions, err := rbacService.GetUserPermissions(userID)
	if err != nil {
		return nil, fmt.Errorf("获取用户权限失败：%w", err)
	}
	permissionCodes := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		permissionCodes[permission.Code] = struct{}{}
	}

	var accessibleMenus []model.Menu
	for _, menu := range allMenus {
		// 跳过不可见的菜单
		if !menu.Visible {
			continue
		}

		if menu.Permission == "" {
			if menu.MenuType != "api" {
				accessibleMenus = append(accessibleMenus, menu)
			}
			continue
		}
		if _, allowed := permissionCodes[menu.Permission]; allowed {
			accessibleMenus = append(accessibleMenus, menu)
		}
	}

	// 如果没有菜单，返回空树
	if len(accessibleMenus) == 0 {
		return []model.MenuTreeResponse{}, nil
	}

	return s.buildMenuTree(accessibleMenus, uuid.Nil), nil
}

// AssignMenusToRole 为角色分配菜单
func (s *menuService) AssignMenusToRole(roleID uuid.UUID, menuIDs []uuid.UUID) error {
	var role model.Role
	if err := s.db.First(&role, roleID).Error; err != nil {
		return fmt.Errorf("角色不存在：%w", err)
	}

	// 删除旧的菜单关联
	if err := s.db.Model(&role).Association("Menus").Clear(); err != nil {
		return fmt.Errorf("清除旧菜单关联失败：%w", err)
	}

	// 添加新的菜单关联
	var menus []model.Menu
	if err := s.db.Where("id IN ?", menuIDs).Find(&menus).Error; err != nil {
		return fmt.Errorf("获取菜单列表失败：%w", err)
	}

	if err := s.db.Model(&role).Association("Menus").Append(menus); err != nil {
		return fmt.Errorf("分配菜单失败：%w", err)
	}

	return nil
}

// GetRoleMenus 获取角色菜单
func (s *menuService) GetRoleMenus(roleID uuid.UUID) ([]model.Menu, error) {
	var role model.Role
	if err := s.db.Preload("Menus").First(&role, roleID).Error; err != nil {
		return nil, fmt.Errorf("角色不存在：%w", err)
	}

	return role.Menus, nil
}

// RemoveMenuFromRole 从角色移除菜单
func (s *menuService) RemoveMenuFromRole(roleID uuid.UUID, menuID uuid.UUID) error {
	var role model.Role
	if err := s.db.First(&role, roleID).Error; err != nil {
		return fmt.Errorf("角色不存在：%w", err)
	}

	var menu model.Menu
	if err := s.db.First(&menu, menuID).Error; err != nil {
		return fmt.Errorf("菜单不存在：%w", err)
	}

	if err := s.db.Model(&role).Association("Menus").Delete(&menu); err != nil {
		return fmt.Errorf("移除菜单失败：%w", err)
	}

	return nil
}

// buildMenuTree 构建菜单树（添加 menu: true 标记到 meta）
func (s *menuService) buildMenuTree(menus []model.Menu, parentID uuid.UUID) []model.MenuTreeResponse {
	tree := make([]model.MenuTreeResponse, 0)

	for _, menu := range menus {
		// 如果是顶级菜单或者父菜单匹配
		if (menu.ParentID == nil && parentID == uuid.Nil) ||
			(menu.ParentID != nil && *menu.ParentID == parentID) {

			// 读取现有 meta
			var metaMap map[string]interface{}
			if menu.Meta != nil {
				json.Unmarshal(menu.Meta, &metaMap)
			}
			if metaMap == nil {
				metaMap = make(map[string]interface{})
			}

			// 添加 menu: true 标记（关键！前端需要这个字段来判断是否为菜单）
			metaMap["menu"] = true

			// 转换为 JSON
			metaJSON, _ := json.Marshal(metaMap)

			node := model.MenuTreeResponse{
				ID:         menu.ID,
				ParentID:   menu.ParentID,
				Name:       menu.Name,
				Title:      menu.Title,
				Icon:       menu.Icon,
				Path:       menu.Path,
				Component:  menu.Component,
				SortOrder:  menu.SortOrder,
				MenuType:   menu.MenuType,
				Permission: menu.Permission,
				Visible:    menu.Visible,
				KeepAlive:  menu.KeepAlive,
				Redirect:   menu.Redirect,
				Meta:       datatypes.JSON(metaJSON),
				Children:   s.buildMenuTree(menus, menu.ID),
			}
			tree = append(tree, node)
		}
	}

	return tree
}

// convertToJSON 转换为 JSON
func convertToJSON(data map[string]interface{}) datatypes.JSON {
	if data == nil {
		return nil
	}
	// 使用 json.Marshal 转换
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	return datatypes.JSON(jsonData)
}
