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

// ContactHandler 联系处理器
type ContactHandler struct {
	contactService service.ContactService
}

// NewContactHandler 创建联系处理器实例
func NewContactHandler() *ContactHandler {
	return &ContactHandler{
		contactService: service.NewContactService(),
	}
}

// CreateContactMessage 创建联系消息
func (h *ContactHandler) CreateContactMessage(c *gin.Context) {
	var req struct {
		Name    string `json:"name" binding:"required,min=2,max=50"`
		Email   string `json:"email" binding:"required,email"`
		Phone   string `json:"phone" binding:"omitempty,max=20"`
		Subject string `json:"subject" binding:"omitempty,max=100"`
		Message string `json:"message" binding:"required,min=5,max=1000"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	contactMessage := &model.ContactMessage{
		Name:    req.Name,
		Email:   req.Email,
		Phone:   req.Phone,
		Subject: req.Subject,
		Message: req.Message,
		Status:  "pending", // 默认待处理状态
	}

	if err := h.contactService.CreateContactMessage(contactMessage); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "提交留言失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "留言提交成功", contactMessage)
}

// GetContactMessages 获取联系消息列表
func (h *ContactHandler) GetContactMessages(c *gin.Context) {
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

	// 解析分页和过滤参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	statusFilter := c.Query("status")

	if pageSize > 100 {
		pageSize = 100
	}

	var messages []model.ContactMessage
	var total int64

	// 计算偏移量
	offset := (page - 1) * pageSize

	// 构建查询条件
	dbQuery := database.DB.Model(&model.ContactMessage{})

	if statusFilter != "" {
		dbQuery = dbQuery.Where("status = ?", statusFilter)
	}

	// 获取总记录数
	if err := dbQuery.Count(&total).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取记录总数失败", err.Error())
		return
	}

	// 获取分页数据
	if err := dbQuery.Order("created_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&messages).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取记录列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"page":      page,
		"page_size": pageSize,
		"total":     total,
		"items":     messages,
	})
}

// GetContactMessageByID 获取联系消息详情
func (h *ContactHandler) GetContactMessageByID(c *gin.Context) {
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

	// 解析消息ID
	messageID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的消息ID", err.Error())
		return
	}

	var message model.ContactMessage

	if err := database.DB.First(&message, "id = ?", messageID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "消息不存在", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", message)
}

// UpdateContactMessage 更新联系消息
func (h *ContactHandler) UpdateContactMessage(c *gin.Context) {
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

	// 解析消息ID
	messageID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的消息ID", err.Error())
		return
	}

	var req struct {
		Status string `json:"status" binding:"required,oneof=pending processing resolved closed"`
		Reply  string `json:"reply" binding:"omitempty,max=1000"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	var message model.ContactMessage

	if err := database.DB.First(&message, "id = ?", messageID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "消息不存在", err.Error())
		return
	}

	// 更新消息状态和回复
	updates := map[string]interface{}{
		"status": req.Status,
		"reply":  req.Reply,
	}

	if err := database.DB.Model(&message).Updates(updates).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新消息失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", message)
}

// DeleteContactMessage 删除联系消息
func (h *ContactHandler) DeleteContactMessage(c *gin.Context) {
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

	// 解析消息ID
	messageID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的消息ID", err.Error())
		return
	}

	var message model.ContactMessage

	if err := database.DB.First(&message, "id = ?", messageID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "消息不存在", err.Error())
		return
	}

	if err := database.DB.Delete(&message).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除消息失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}
