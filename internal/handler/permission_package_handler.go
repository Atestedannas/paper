package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

type PermissionPackageHandler struct {
	packageService service.PermissionPackageService
}

func NewPermissionPackageHandler() *PermissionPackageHandler {
	return &PermissionPackageHandler{
		packageService: service.NewPermissionPackageService(),
	}
}

func (h *PermissionPackageHandler) CreatePackage(c *gin.Context) {
	var req struct {
		Name          string   `json:"name" binding:"required,max=100"`
		Code          string   `json:"code" binding:"required,max=100"`
		Description   string   `json:"description" binding:"max=200"`
		PermissionIDs []string `json:"permission_ids"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	permissionIDs := make([]uuid.UUID, 0, len(req.PermissionIDs))
	for _, idStr := range req.PermissionIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限ID", idStr)
			return
		}
		permissionIDs = append(permissionIDs, id)
	}

	pkg, err := h.packageService.CreatePackage(req.Name, req.Code, req.Description, permissionIDs)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建权限包失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "权限包创建成功", pkg)
}

func (h *PermissionPackageHandler) GetPackages(c *gin.Context) {
	query := c.DefaultQuery("q", "")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	packages, total, err := h.packageService.GetPackages(query, page, pageSize)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取权限包列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"items":       packages,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

func (h *PermissionPackageHandler) GetPackageByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限包ID", err.Error())
		return
	}

	pkg, err := h.packageService.GetPackageByID(id)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取权限包失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", pkg)
}

func (h *PermissionPackageHandler) UpdatePackage(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限包ID", err.Error())
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.packageService.UpdatePackage(id, req); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新权限包失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

func (h *PermissionPackageHandler) DeletePackage(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限包ID", err.Error())
		return
	}

	if err := h.packageService.DeletePackage(id); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除权限包失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

func (h *PermissionPackageHandler) GetPackagePermissions(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限包ID", err.Error())
		return
	}

	permissions, err := h.packageService.GetPackagePermissions(id)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取权限包权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", permissions)
}

func (h *PermissionPackageHandler) AddPackagePermissions(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限包ID", err.Error())
		return
	}

	var req struct {
		PermissionIDs []string `json:"permission_ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	permissionIDs := make([]uuid.UUID, 0, len(req.PermissionIDs))
	for _, idStr := range req.PermissionIDs {
		permID, err := uuid.Parse(idStr)
		if err != nil {
			utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限ID", idStr)
			return
		}
		permissionIDs = append(permissionIDs, permID)
	}

	if err := h.packageService.AddPermissionsToPackage(id, permissionIDs); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "添加权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "添加成功", nil)
}

func (h *PermissionPackageHandler) RemovePackagePermissions(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限包ID", err.Error())
		return
	}

	var req struct {
		PermissionIDs []string `json:"permission_ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	permissionIDs := make([]uuid.UUID, 0, len(req.PermissionIDs))
	for _, idStr := range req.PermissionIDs {
		permID, err := uuid.Parse(idStr)
		if err != nil {
			utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限ID", idStr)
			return
		}
		permissionIDs = append(permissionIDs, permID)
	}

	if err := h.packageService.RemovePermissionsFromPackage(id, permissionIDs); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "移除权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "移除成功", nil)
}

func (h *PermissionPackageHandler) AssignPackageToRole(c *gin.Context) {
	packageID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限包ID", err.Error())
		return
	}

	roleID, err := uuid.Parse(c.Param("role_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色ID", err.Error())
		return
	}

	if err := h.packageService.AssignPackageToRole(roleID, packageID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "分配权限包失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "分配成功", nil)
}

func (h *PermissionPackageHandler) ClonePackage(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的权限包ID", err.Error())
		return
	}

	var req struct {
		NewCode string `json:"new_code" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	pkg, err := h.packageService.ClonePackage(id, req.NewCode)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "克隆权限包失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "克隆成功", pkg)
}

func (h *PermissionPackageHandler) GetRolePackages(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("role_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色ID", err.Error())
		return
	}

	packages, err := h.packageService.GetRolePackages(roleID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取角色权限包失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", packages)
}
