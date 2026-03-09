package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

type ACLHandler struct {
	aclService service.ACLService
}

func NewACLHandler() *ACLHandler {
	return &ACLHandler{
		aclService: service.NewACLService(),
	}
}

func (h *ACLHandler) GrantAccess(c *gin.Context) {
	var req struct {
		ResourceType    string               `json:"resource_type" binding:"required"`
		ResourceID      string               `json:"resource_id" binding:"required"`
		OwnerID         string               `json:"owner_id" binding:"required"`
		AuthorizedUsers []model.ACLUserInput `json:"authorized_users" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	resourceID, err := uuid.Parse(req.ResourceID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的资源ID", err.Error())
		return
	}

	ownerID, err := uuid.Parse(req.OwnerID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的所有者ID", err.Error())
		return
	}

	creatorID, _ := uuid.Parse(c.GetString("user_id"))

	var authorizedUsers []service.ACLUserInput
	for _, user := range req.AuthorizedUsers {
		authorizedUsers = append(authorizedUsers, service.ACLUserInput{
			UserID:      user.UserID,
			AccessLevel: user.AccessLevel,
		})
	}

	if err := h.aclService.GrantAccess(req.ResourceType, resourceID, ownerID, authorizedUsers, creatorID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "授权失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "授权成功", nil)
}

func (h *ACLHandler) RevokeAccess(c *gin.Context) {
	var req struct {
		ResourceType string `json:"resource_type" binding:"required"`
		ResourceID   string `json:"resource_id" binding:"required"`
		UserID       string `json:"user_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	resourceID, err := uuid.Parse(req.ResourceID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的资源ID", err.Error())
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	if err := h.aclService.RevokeAccess(req.ResourceType, resourceID, userID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "撤销授权失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "撤销授权成功", nil)
}

func (h *ACLHandler) CanAccess(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	resourceType := c.Query("resource_type")
	if resourceType == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "资源类型不能为空", "")
		return
	}

	resourceID, err := uuid.Parse(c.Query("resource_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的资源ID", err.Error())
		return
	}

	requiredLevel := c.DefaultQuery("required_level", "read")

	canAccess, err := h.aclService.CanAccess(userID, resourceType, resourceID, requiredLevel)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "检查权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"can_access": canAccess,
	})
}

func (h *ACLHandler) GetAccessibleResources(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	resourceType := c.Query("resource_type")
	if resourceType == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "资源类型不能为空", "")
		return
	}

	resources, err := h.aclService.GetAccessibleResources(userID, resourceType)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取可访问资源失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"resources": resources,
	})
}

func (h *ACLHandler) GetResourceACL(c *gin.Context) {
	resourceType := c.Query("resource_type")
	if resourceType == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "资源类型不能为空", "")
		return
	}

	resourceID, err := uuid.Parse(c.Query("resource_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的资源ID", err.Error())
		return
	}

	acl, err := h.aclService.GetACLWithUsers(resourceType, resourceID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取ACL失败", err.Error())
		return
	}

	if acl == nil {
		utils.SuccessResponse(c, "获取成功", nil)
		return
	}

	utils.SuccessResponse(c, "获取成功", acl)
}

func (h *ACLHandler) GetUserACLs(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	resourceType := c.Query("resource_type")
	if resourceType == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "资源类型不能为空", "")
		return
	}

	acls, err := h.aclService.GetUserACLs(userID, resourceType)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取ACL列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", acls)
}

func (h *ACLHandler) DeleteResourceACL(c *gin.Context) {
	resourceType := c.Query("resource_type")
	if resourceType == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "资源类型不能为空", "")
		return
	}

	resourceID, err := uuid.Parse(c.Query("resource_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的资源ID", err.Error())
		return
	}

	if err := h.aclService.DeleteResourceACL(resourceType, resourceID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除ACL失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}
