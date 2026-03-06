package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/utils"
)

// GetRolePermissions 获取角色的权限列表
func (h *RBACHandler) GetRolePermissions(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的角色ID", err.Error())
		return
	}

	permissions, err := h.rbacService.GetRolePermissions(roleID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取角色权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", permissions)
}
