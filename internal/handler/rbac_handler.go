package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// RBACHandler RBAC权限管理处理器
type RBACHandler struct {
	rbacService service.RBACService
}

// NewRBACHandler 创建RBAC处理器实例
func NewRBACHandler() *RBACHandler {
	rbacService, err := service.NewRBACService()
	if err != nil {
		panic(err)
	}
	return &RBACHandler{
		rbacService: rbacService,
	}
}

// CreateUser 创建用户
func (h *RBACHandler) CreateUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required,min=3,max=50"`
		Password string `json:"password" binding:"required,min=6,max=100"`
		Email    string `json:"email" binding:"required,email,max=100"`
		FullName string `json:"full_name" binding:"max=100"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	user, err := h.rbacService.CreateUser(req.Username, req.Email, req.Password)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建用户失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "用户创建成功", user)
}

// GetUserByID 根据ID获取用户
func (h *RBACHandler) GetUserByID(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	user, err := h.rbacService.GetUserByID(userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", user)
}

// UpdateUser 更新用户信息
func (h *RBACHandler) UpdateUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 移除不允许更新的字段
	delete(req, "id")
	delete(req, "password_hash")
	delete(req, "created_at")
	delete(req, "updated_at")

	if err := h.rbacService.UpdateUser(userID, req); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新用户失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// DeleteUser 删除用户
func (h *RBACHandler) DeleteUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	if err := h.rbacService.DeleteUser(userID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除用户失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

// CreateRole 创建角色
func (h *RBACHandler) CreateRole(c *gin.Context) {
	var req struct {
		Name        string     `json:"name" binding:"required,max=50"`
		Description string     `json:"description" binding:"max=200"`
		Type        string     `json:"type" binding:"required,oneof=system business"`
		Code        string     `json:"code" binding:"required,max=50"`
		ParentID    *uuid.UUID `json:"parent_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	role, err := h.rbacService.CreateRole(req.Name, req.Description, req.Type, req.Code, req.ParentID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建角色失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "角色创建成功", role)
}

// GetRoleByID 根据ID获取角色
func (h *RBACHandler) GetRoleByID(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色ID", err.Error())
		return
	}

	role, err := h.rbacService.GetRoleByID(roleID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取角色失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", role)
}

// GetRoles 获取角色列表
func (h *RBACHandler) GetRoles(c *gin.Context) {
	roles, err := h.rbacService.GetRoles()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取角色列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", roles)
}

// UpdateRole 更新角色信息
func (h *RBACHandler) UpdateRole(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色ID", err.Error())
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 移除不允许更新的字段
	delete(req, "id")
	delete(req, "created_at")
	delete(req, "updated_at")

	if err := h.rbacService.UpdateRole(roleID, req); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新角色失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// DeleteRole 删除角色
func (h *RBACHandler) DeleteRole(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色ID", err.Error())
		return
	}

	if err := h.rbacService.DeleteRole(roleID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除角色失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

// AssignRoleToUser 为用户分配角色
func (h *RBACHandler) AssignRoleToUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	roleID, err := uuid.Parse(c.Param("role_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色ID", err.Error())
		return
	}

	if err := h.rbacService.AssignRoleToUser(userID, roleID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "分配角色失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "角色分配成功", nil)
}

// RemoveRoleFromUser 从用户移除角色
func (h *RBACHandler) RemoveRoleFromUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	roleID, err := uuid.Parse(c.Param("role_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色ID", err.Error())
		return
	}

	if err := h.rbacService.RemoveRoleFromUser(userID, roleID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "移除角色失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "角色移除成功", nil)
}

// CreatePermission 创建权限
func (h *RBACHandler) CreatePermission(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required,max=100"`
		Code         string `json:"code" binding:"required,max=100"`
		ResourceType string `json:"resource_type" binding:"required,oneof=api menu button"`
		Method       string `json:"method" binding:"required,oneof=GET POST PUT DELETE PATCH"`
		Path         string `json:"path" binding:"max=200"`
		Description  string `json:"description" binding:"max=200"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	permission, err := h.rbacService.CreatePermission(
		req.Name,
		req.Code,
		req.ResourceType,
		req.Method,
		req.Path,
		req.Description,
	)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "权限创建成功", permission)
}

// GetPermissionByID 根据ID获取权限
func (h *RBACHandler) GetPermissionByID(c *gin.Context) {
	permissionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限ID", err.Error())
		return
	}

	permission, err := h.rbacService.GetPermissionByID(permissionID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", permission)
}

// GetPermissions 获取权限列表
func (h *RBACHandler) GetPermissions(c *gin.Context) {
	permissions, err := h.rbacService.GetPermissions()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取权限列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", permissions)
}

// UpdatePermission 更新权限信息
func (h *RBACHandler) UpdatePermission(c *gin.Context) {
	permissionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限ID", err.Error())
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 移除不允许更新的字段
	delete(req, "id")
	delete(req, "created_at")

	if err := h.rbacService.UpdatePermission(permissionID, req); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// DeletePermission 删除权限
func (h *RBACHandler) DeletePermission(c *gin.Context) {
	permissionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限ID", err.Error())
		return
	}

	if err := h.rbacService.DeletePermission(permissionID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

// AssignPermissionToRole 为角色分配权限
func (h *RBACHandler) AssignPermissionToRole(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("role_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色ID", err.Error())
		return
	}

	permissionID, err := uuid.Parse(c.Param("permission_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限ID", err.Error())
		return
	}

	if err := h.rbacService.AssignPermissionToRole(roleID, permissionID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "分配权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "权限分配成功", nil)
}

// RemovePermissionFromRole 从角色移除权限
func (h *RBACHandler) RemovePermissionFromRole(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("role_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色ID", err.Error())
		return
	}

	permissionID, err := uuid.Parse(c.Param("permission_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限ID", err.Error())
		return
	}

	if err := h.rbacService.RemovePermissionFromRole(roleID, permissionID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "移除权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "权限移除成功", nil)
}

// AssignPermissionToUser 为用户直接分配权限
func (h *RBACHandler) AssignPermissionToUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	permissionID, err := uuid.Parse(c.Param("permission_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限ID", err.Error())
		return
	}

	var userPermission model.UserPermission
	if err := database.DB.Where("user_id = ? AND permission_id = ?", userID, permissionID).First(&userPermission).Error; err == nil {
		utils.SuccessResponse(c, "权限已存在", nil)
		return
	}

	userPermission = model.UserPermission{
		UserID:       userID,
		PermissionID: permissionID,
	}

	if err := database.DB.Create(&userPermission).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "分配权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "权限分配成功", nil)
}

// RemovePermissionFromUser 从用户移除直接分配的权限
func (h *RBACHandler) RemovePermissionFromUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	permissionID, err := uuid.Parse(c.Param("permission_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限ID", err.Error())
		return
	}

	if err := database.DB.Where("user_id = ? AND permission_id = ?", userID, permissionID).Delete(&model.UserPermission{}).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "移除权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "权限移除成功", nil)
}

// CheckPermission 检查用户权限
func (h *RBACHandler) CheckPermission(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "未找到用户信息", "")
		return
	}

	var req struct {
		Resource string `json:"resource" binding:"required"`
		Action   string `json:"action" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	hasPermission, err := h.rbacService.HasPermission(userID.(uuid.UUID), req.Resource, req.Action)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "权限检查失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "权限检查完成", gin.H{
		"has_permission": hasPermission,
	})
}

// GetUserPermissions 获取用户权限列表
func (h *RBACHandler) GetUserPermissions(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户 ID", err.Error())
		return
	}

	// 获取用户的所有权限（包括角色继承和直接分配的）
	allPermissions, err := h.rbacService.GetUserPermissions(userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户权限失败", err.Error())
		return
	}

	// 获取用户直接分配的权限
	directPermissionIDs, err := h.rbacService.GetUserDirectPermissionIDs(userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户直接权限失败", err.Error())
		return
	}

	// 为每个权限标记来源
	type PermissionWithSource struct {
		model.Permission
		FromRole  bool     `json:"from_role"`  // 是否来自角色继承
		RoleNames []string `json:"role_names"` // 来自哪些角色
		IsDirect  bool     `json:"is_direct"`  // 是否是直接分配的权限
	}

	result := make([]PermissionWithSource, 0, len(allPermissions))
	for _, perm := range allPermissions {
		pws := PermissionWithSource{
			Permission: perm,
			FromRole:   false,
			RoleNames:  []string{},
			IsDirect:   false,
		}

		// 检查是否是直接分配的权限
		for _, directID := range directPermissionIDs {
			if directID == perm.ID {
				pws.IsDirect = true
				break
			}
		}

		// 检查是否来自角色
		userRoles, err := h.rbacService.GetUserRoles(userID)
		if err == nil {
			for _, role := range userRoles {
				rolePermissions, err := h.rbacService.GetRolePermissions(role.ID)
				if err == nil {
					for _, rolePerm := range rolePermissions {
						if rolePerm.ID == perm.ID {
							pws.FromRole = true
							pws.RoleNames = append(pws.RoleNames, role.Name)
							break
						}
					}
				}
			}
		}

		result = append(result, pws)
	}

	utils.SuccessResponse(c, "获取成功", result)
}

// GetUserDirectPermissions 获取用户直接分配的权限（不包括角色继承的）
func (h *RBACHandler) GetUserDirectPermissions(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户 ID", err.Error())
		return
	}

	// 获取用户直接分配的权限 ID
	directPermissionIDs, err := h.rbacService.GetUserDirectPermissionIDs(userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户直接权限失败", err.Error())
		return
	}

	// 获取权限详情
	var permissions []model.Permission
	if len(directPermissionIDs) > 0 {
		err = database.DB.Where("id IN ?", directPermissionIDs).Find(&permissions).Error
		if err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "获取权限详情失败", err.Error())
			return
		}
	}

	utils.SuccessResponse(c, "获取成功", permissions)
}

// GetUserRoles 获取用户角色列表
func (h *RBACHandler) GetUserRoles(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "未找到用户信息", "")
		return
	}

	roles, err := h.rbacService.GetUserRoles(userID.(uuid.UUID))
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户角色失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", roles)
}

// GetUserRoles 获取用户角色列表（通过路径参数）
func (h *RBACHandler) GetUserRolesByID(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	roles, err := h.rbacService.GetUserRoles(userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户角色失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", roles)
}

// GetUserPermissions 获取用户权限列表（通过路径参数）
func (h *RBACHandler) GetUserPermissionsByID(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	permissions, err := h.rbacService.GetUserPermissions(userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", permissions)
}

// GetUsersByRole 根据角色获取用户列表
func (h *RBACHandler) GetUsersByRole(c *gin.Context) {
	roleID := c.Param("role_id")
	if roleID == "" {
		roleID = c.Param("id")
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	// 计算偏移量
	offset := (page - 1) * pageSize

	// 通过Casbin获取拥有特定角色的用户
	userIDs := h.rbacService.GetUsersForRole(roleID)

	// 根据用户ID获取用户详情（这里需要分页处理）
	var users []interface{}
	for i, userIDStr := range userIDs {
		if i >= offset && i < offset+pageSize {
			userID, err := uuid.Parse(userIDStr)
			if err != nil {
				continue
			}

			user, err := h.rbacService.GetUserByID(userID)
			if err != nil {
				continue
			}

			users = append(users, user)
		}
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"users":       users,
		"total":       len(userIDs),
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (len(userIDs) + pageSize - 1) / pageSize,
	})
}
