package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/utils"
)

// BatchHandler 批量操作处理器
type BatchHandler struct{}

// NewBatchHandler 创建批量操作处理器实例
func NewBatchHandler() *BatchHandler {
	return &BatchHandler{}
}

// BatchUpdateStatus 批量更新状态
func (h *BatchHandler) BatchUpdateStatus(c *gin.Context) {
	// 从JWT中获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "未授权访问", "")
		return
	}

	// 从数据库中查询用户信息以验证权限
	var user model.User
	result := database.DB.Select("role").First(&user, "id = ?", userID)
	if result.Error != nil {
		utils.ErrorResponse(c, http.StatusUnauthorized, "用户不存在", result.Error.Error())
		return
	}

	// 检查用户角色是否为管理员
	if user.Role != "admin" {
		utils.ErrorResponse(c, http.StatusForbidden, "需要管理员权限", "用户角色: "+user.Role)
		return
	}

	var req struct {
		ResourceType string      `json:"resource_type" binding:"required,oneof=orders messages papers"`
		ResourceIDs  []uuid.UUID `json:"resource_ids" binding:"required,min=1,max=100"`
		Status       string      `json:"status" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 根据资源类型执行不同的批量更新操作
	var affectedRows int64
	var err error

	switch req.ResourceType {
	case "orders":
		affectedRows, err = h.updateOrderStatus(req.ResourceIDs, req.Status)
	case "messages":
		affectedRows, err = h.updateMessageStatus(req.ResourceIDs, req.Status)
	case "papers":
		affectedRows, err = h.updatePaperStatus(req.ResourceIDs, req.Status)
	default:
		utils.ErrorResponse(c, http.StatusBadRequest, "不支持的资源类型", "")
		return
	}

	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "批量更新失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "批量更新成功", gin.H{
		"affected_rows": affectedRows,
		"resource_type": req.ResourceType,
	})
}

// updateOrderStatus 更新订单状态
func (h *BatchHandler) updateOrderStatus(orderIDs []uuid.UUID, status string) (int64, error) {
	result := database.DB.Model(&model.Order{}).
		Where("id IN ?", orderIDs).
		Update("order_status", status)

	return result.RowsAffected, result.Error
}

// updateMessageStatus 更新消息状态
func (h *BatchHandler) updateMessageStatus(messageIDs []uuid.UUID, status string) (int64, error) {
	result := database.DB.Model(&model.ContactMessage{}).
		Where("id IN ?", messageIDs).
		Update("status", status)

	return result.RowsAffected, result.Error
}

// updatePaperStatus 更新论文状态
func (h *BatchHandler) updatePaperStatus(paperIDs []uuid.UUID, status string) (int64, error) {
	result := database.DB.Model(&model.Paper{}).
		Where("id IN ?", paperIDs).
		Update("status", status)

	return result.RowsAffected, result.Error
}

// BatchDelete 批量删除
func (h *BatchHandler) BatchDelete(c *gin.Context) {
	// 从JWT中获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "未授权访问", "")
		return
	}

	// 从数据库中查询用户信息以验证权限
	var user model.User
	result := database.DB.Select("role").First(&user, "id = ?", userID)
	if result.Error != nil {
		utils.ErrorResponse(c, http.StatusUnauthorized, "用户不存在", result.Error.Error())
		return
	}

	// 检查用户角色是否为管理员
	if user.Role != "admin" {
		utils.ErrorResponse(c, http.StatusForbidden, "需要管理员权限", "用户角色: "+user.Role)
		return
	}

	var req struct {
		ResourceType string      `json:"resource_type" binding:"required,oneof=orders messages papers"`
		ResourceIDs  []uuid.UUID `json:"resource_ids" binding:"required,min=1,max=100"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 根据资源类型执行不同的批量删除操作
	var affectedRows int64
	var err error

	switch req.ResourceType {
	case "orders":
		affectedRows, err = h.deleteOrders(req.ResourceIDs)
	case "messages":
		affectedRows, err = h.deleteMessages(req.ResourceIDs)
	case "papers":
		affectedRows, err = h.deletePapers(req.ResourceIDs)
	default:
		utils.ErrorResponse(c, http.StatusBadRequest, "不支持的资源类型", "")
		return
	}

	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "批量删除失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "批量删除成功", gin.H{
		"affected_rows": affectedRows,
		"resource_type": req.ResourceType,
	})
}

// deleteOrders 删除订单
func (h *BatchHandler) deleteOrders(orderIDs []uuid.UUID) (int64, error) {
	result := database.DB.Where("id IN ?", orderIDs).Delete(&model.Order{})
	return result.RowsAffected, result.Error
}

// deleteMessages 删除消息
func (h *BatchHandler) deleteMessages(messageIDs []uuid.UUID) (int64, error) {
	result := database.DB.Where("id IN ?", messageIDs).Delete(&model.ContactMessage{})
	return result.RowsAffected, result.Error
}

// deletePapers 删除论文
func (h *BatchHandler) deletePapers(paperIDs []uuid.UUID) (int64, error) {
	result := database.DB.Where("id IN ?", paperIDs).Delete(&model.Paper{})
	return result.RowsAffected, result.Error
}

// QuickAction 执行快捷操作
func (h *BatchHandler) QuickAction(c *gin.Context) {
	// 从JWT中获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "未授权访问", "")
		return
	}

	// 从数据库中查询用户信息以验证权限
	var user model.User
	result := database.DB.Select("role").First(&user, "id = ?", userID)
	if result.Error != nil {
		utils.ErrorResponse(c, http.StatusUnauthorized, "用户不存在", result.Error.Error())
		return
	}

	// 检查用户角色是否为管理员
	if user.Role != "admin" {
		utils.ErrorResponse(c, http.StatusForbidden, "需要管理员权限", "用户角色: "+user.Role)
		return
	}

	var req struct {
		Action       string      `json:"action" binding:"required,oneof=mark_as_read mark_as_unread mark_as_resolved mark_as_pending"`
		ResourceType string      `json:"resource_type" binding:"required,oneof=orders messages papers"`
		ResourceIDs  []uuid.UUID `json:"resource_ids" binding:"required,min=1,max=100"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 根据操作类型和资源类型执行不同的快捷操作
	var affectedRows int64
	var err error

	statusMap := map[string]string{
		"mark_as_read":     "read",
		"mark_as_unread":   "unread",
		"mark_as_resolved": "resolved",
		"mark_as_pending":  "pending",
	}

	targetStatus, exists := statusMap[req.Action]
	if !exists {
		utils.ErrorResponse(c, http.StatusBadRequest, "不支持的操作类型", "")
		return
	}

	switch req.ResourceType {
	case "orders":
		affectedRows, err = h.updateOrderStatus(req.ResourceIDs, targetStatus)
	case "messages":
		affectedRows, err = h.updateMessageStatus(req.ResourceIDs, targetStatus)
	case "papers":
		affectedRows, err = h.updatePaperStatus(req.ResourceIDs, targetStatus)
	default:
		utils.ErrorResponse(c, http.StatusBadRequest, "不支持的资源类型", "")
		return
	}

	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "快捷操作失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "快捷操作成功", gin.H{
		"affected_rows": affectedRows,
		"action":        req.Action,
		"resource_type": req.ResourceType,
	})
}
