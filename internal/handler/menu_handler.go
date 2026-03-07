package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// MenuHandler 菜单处理器
type MenuHandler struct {
	menuService service.MenuService
}

// NewMenuHandler 创建菜单处理器
func NewMenuHandler() *MenuHandler {
	return &MenuHandler{
		menuService: service.NewMenuService(),
	}
}

// CreateMenu 创建菜单
func (h *MenuHandler) CreateMenu(c *gin.Context) {
	var req model.MenuCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	menu, err := h.menuService.CreateMenu(&req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建菜单失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "创建菜单成功", menu)
}

// UpdateMenu 更新菜单
func (h *MenuHandler) UpdateMenu(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的菜单 ID", err.Error())
		return
	}

	var req model.MenuUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	menu, err := h.menuService.UpdateMenu(id, &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新菜单失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新菜单成功", menu)
}

// DeleteMenu 删除菜单
func (h *MenuHandler) DeleteMenu(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的菜单 ID", err.Error())
		return
	}

	if err := h.menuService.DeleteMenu(id); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除菜单失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除菜单成功", nil)
}

// GetMenuByID 获取菜单详情
func (h *MenuHandler) GetMenuByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的菜单 ID", err.Error())
		return
	}

	menu, err := h.menuService.GetMenuByID(id)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "菜单不存在", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取菜单成功", menu)
}

// GetMenuTree 获取菜单树
func (h *MenuHandler) GetMenuTree(c *gin.Context) {
	tree, err := h.menuService.GetMenuTree()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取菜单树失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取菜单树成功", tree)
}

// GetUserMenus 获取用户菜单
func (h *MenuHandler) GetUserMenus(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "未登录", "")
		return
	}

	uid, ok := userID.(uuid.UUID)

	if !ok {

		// 尝试从字符串转换
		uidStr, ok := userID.(string)
		if !ok {
			utils.ErrorResponse(c, http.StatusInternalServerError, "用户 ID 格式错误", "")
			return
		}
		var err error
		uid, err = uuid.Parse(uidStr)
		if err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "用户 ID 格式错误", err.Error())
			return
		}
	}

	tree, err := h.menuService.GetUserMenuTree(uid)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户菜单失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取用户菜单成功", tree)
}

// AssignMenusToRole 为角色分配菜单
func (h *MenuHandler) AssignMenusToRole(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("role_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色 ID", err.Error())
		return
	}

	var req struct {
		MenuIDs []uuid.UUID `json:"menu_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.menuService.AssignMenusToRole(roleID, req.MenuIDs); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "分配菜单失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "分配菜单成功", nil)
}

// GetRoleMenus 获取角色菜单
func (h *MenuHandler) GetRoleMenus(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("role_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色 ID", err.Error())
		return
	}

	menus, err := h.menuService.GetRoleMenus(roleID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "角色不存在", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取角色菜单成功", menus)
}

// GetAllMenus 获取所有菜单
func (h *MenuHandler) GetAllMenus(c *gin.Context) {
	menus, err := h.menuService.GetAllMenus()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取菜单列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取菜单列表成功", menus)
}

// GetMenusWithPagination 分页获取菜单
func (h *MenuHandler) GetMenusWithPagination(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	menus, err := h.menuService.GetAllMenus()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取菜单列表失败", err.Error())
		return
	}

	// 手动分页
	total := len(menus)
	start := (page - 1) * pageSize
	end := start + pageSize

	if start >= total {
		utils.SuccessResponse(c, "获取成功", gin.H{
			"menus":       []model.Menu{},
			"total":       total,
			"page":        page,
			"page_size":   pageSize,
			"total_pages": (total + pageSize - 1) / pageSize,
		})
		return
	}

	if end > total {
		end = total
	}

	pagedMenus := menus[start:end]

	utils.SuccessResponse(c, "获取成功", gin.H{
		"menus":       pagedMenus,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + pageSize - 1) / pageSize,
	})
}
