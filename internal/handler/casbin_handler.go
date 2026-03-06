package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// CasbinHandler Casbin 处理器
type CasbinHandler struct {
	casbinService service.CasbinService
}

// NewCasbinHandler 创建 Casbin 处理器
func NewCasbinHandler() *CasbinHandler {
	return &CasbinHandler{
		casbinService: service.NewCasbinService(),
	}
}

// Enforce 权限检查
func (h *CasbinHandler) Enforce(c *gin.Context) {
	var req model.CasbinEnforceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	allowed, err := h.casbinService.Enforce(req.Sub, req.Obj, req.Act)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "权限检查失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "权限检查成功", model.CasbinEnforceResponse{
		Allowed: allowed,
		Message: map[bool]string{true: "允许访问", false: "拒绝访问"}[allowed],
	})
}

// AddPolicy 添加策略
func (h *CasbinHandler) AddPolicy(c *gin.Context) {
	var req model.CasbinPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.casbinService.AddPolicy(req.V0, req.V1, req.V2); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "添加策略失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "添加策略成功", nil)
}

// RemovePolicy 移除策略
func (h *CasbinHandler) RemovePolicy(c *gin.Context) {
	var req model.CasbinPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.casbinService.RemovePolicy(req.V0, req.V1, req.V2); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "移除策略失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "移除策略成功", nil)
}

// AddGroupingPolicy 添加角色继承
func (h *CasbinHandler) AddGroupingPolicy(c *gin.Context) {
	var req struct {
		User string `json:"user" binding:"required"`
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.casbinService.AddGroupingPolicy(req.User, req.Role); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "添加角色继承失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "添加角色继承成功", nil)
}

// RemoveGroupingPolicy 移除角色继承
func (h *CasbinHandler) RemoveGroupingPolicy(c *gin.Context) {
	var req struct {
		User string `json:"user" binding:"required"`
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.casbinService.RemoveGroupingPolicy(req.User, req.Role); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "移除角色继承失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "移除角色继承成功", nil)
}

// GetPermissionsForUser 获取用户所有权限
func (h *CasbinHandler) GetPermissionsForUser(c *gin.Context) {
	user := c.Query("user")
	if user == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "缺少用户参数", "")
		return
	}

	permissions, err := h.casbinService.GetImplicitPermissionsForUser(user)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取用户权限成功", gin.H{
		"permissions": permissions,
	})
}

// GetRolesForUser 获取用户所有角色
func (h *CasbinHandler) GetRolesForUser(c *gin.Context) {
	user := c.Query("user")
	if user == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "缺少用户参数", "")
		return
	}

	roles, err := h.casbinService.GetImplicitRolesForUser(user)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户角色失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取用户角色成功", gin.H{
		"roles": roles,
	})
}

// GetPolicy 获取所有策略
func (h *CasbinHandler) GetPolicy(c *gin.Context) {
	policy, err := h.casbinService.GetPolicy()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取策略失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取策略成功", gin.H{
		"policy": policy,
	})
}

// LoadPolicy 加载策略
func (h *CasbinHandler) LoadPolicy(c *gin.Context) {
	if err := h.casbinService.LoadPolicy(); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "加载策略失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "加载策略成功", nil)
}

// SavePolicy 保存策略
func (h *CasbinHandler) SavePolicy(c *gin.Context) {
	if err := h.casbinService.SavePolicy(); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "保存策略失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "保存策略成功", nil)
}
