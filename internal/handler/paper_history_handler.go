package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/utils"
)

// PaperHistoryHandler 论文检查记录处理器
type PaperHistoryHandler struct{}

// NewPaperHistoryHandler 创建论文检查记录处理器实例
func NewPaperHistoryHandler() *PaperHistoryHandler {
	return &PaperHistoryHandler{}
}

// GetPaperCheckRecords 获取论文检查记录（普通用户）
func (h *PaperHistoryHandler) GetPaperCheckRecords(c *gin.Context) {
	// 从上下文获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, 401, "未授权访问", "")
		return
	}

	// 解析分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	if pageSize > 100 {
		pageSize = 100
	}

	var records []model.CheckResult
	var total int64

	// 计算偏移量
	offset := (page - 1) * pageSize

	// 获取总记录数
	if err := database.DB.Model(&model.CheckResult{}).Where("user_id = ?", userID).Count(&total).Error; err != nil {
		utils.ErrorResponse(c, 500, "获取记录总数失败", err.Error())
		return
	}

	// 获取分页数据
	if err := database.DB.Preload("Paper").Preload("Template").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&records).Error; err != nil {
		utils.ErrorResponse(c, 500, "获取记录列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"page":      page,
		"page_size": pageSize,
		"total":     total,
		"items":     records,
	})
}

// GetPaperCheckRecordByID 获取论文检查记录详情
func (h *PaperHistoryHandler) GetPaperCheckRecordByID(c *gin.Context) {
	// 从上下文获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, 401, "未授权访问", "")
		return
	}

	// 解析记录ID
	recordID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, 400, "无效的记录ID", err.Error())
		return
	}

	var record model.CheckResult

	// 获取记录详情，只允许用户访问自己的记录
	if err := database.DB.Preload("Paper").Preload("Template").Preload("Corrections").
		Where("id = ? AND user_id = ?", recordID, userID).
		First(&record).Error; err != nil {
		utils.ErrorResponse(c, 404, "记录不存在或无权限访问", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", record)
}

// GetPaperCheckRecordsForAdmin 获取论文检查记录（管理员）
func (h *PaperHistoryHandler) GetPaperCheckRecordsForAdmin(c *gin.Context) {
	// 检查管理员权限
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, 401, "未授权访问", "")
		return
	}

	// 从数据库中查询用户信息以验证权限
	var user model.User
	result := database.DB.Select("role").First(&user, "id = ?", userID)
	if result.Error != nil {
		utils.ErrorResponse(c, 401, "用户不存在", result.Error.Error())
		return
	}

	// 检查用户角色是否为管理员
	if user.Role != "admin" {
		utils.ErrorResponse(c, 403, "需要管理员权限", "用户角色: "+user.Role)
		return
	}

	// 解析分页和过滤参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	userFilter := c.Query("user_id")         // 按用户过滤
	templateFilter := c.Query("template_id") // 按模板过滤

	if pageSize > 100 {
		pageSize = 100
	}

	var records []model.CheckResult
	var total int64

	// 计算偏移量
	offset := (page - 1) * pageSize

	// 构建查询条件
	dbQuery := database.DB.Model(&model.CheckResult{}).Preload("Paper").Preload("Template").Preload("User")

	if userFilter != "" {
		userUUID, err := uuid.Parse(userFilter)
		if err == nil {
			dbQuery = dbQuery.Where("user_id = ?", userUUID)
		}
	}

	if templateFilter != "" {
		templateUUID, err := uuid.Parse(templateFilter)
		if err == nil {
			dbQuery = dbQuery.Where("template_id = ?", templateUUID)
		}
	}

	// 获取总记录数
	if err := dbQuery.Count(&total).Error; err != nil {
		utils.ErrorResponse(c, 500, "获取记录总数失败", err.Error())
		return
	}

	// 获取分页数据
	if err := dbQuery.Order("created_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&records).Error; err != nil {
		utils.ErrorResponse(c, 500, "获取记录列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"page":      page,
		"page_size": pageSize,
		"total":     total,
		"items":     records,
	})
}

// GetPaperCheckRecordByIDForAdmin 获取论文检查记录详情（管理员）
func (h *PaperHistoryHandler) GetPaperCheckRecordByIDForAdmin(c *gin.Context) {
	// 检查管理员权限
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, 401, "未授权访问", "")
		return
	}

	// 从数据库中查询用户信息以验证权限
	var user model.User
	result := database.DB.Select("role").First(&user, "id = ?", userID)
	if result.Error != nil {
		utils.ErrorResponse(c, 401, "用户不存在", result.Error.Error())
		return
	}

	// 检查用户角色是否为管理员
	if user.Role != "admin" {
		utils.ErrorResponse(c, 403, "需要管理员权限", "用户角色: "+user.Role)
		return
	}

	// 解析记录ID
	recordID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, 400, "无效的记录ID", err.Error())
		return
	}

	var record model.CheckResult

	// 获取记录详情（管理员可以访问所有记录）
	if err := database.DB.Preload("Paper").Preload("Template").Preload("Corrections").Preload("User").
		Where("id = ?", recordID).
		First(&record).Error; err != nil {
		utils.ErrorResponse(c, 404, "记录不存在", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", record)
}

// DeletePaperCheckRecord 删除论文检查记录（管理员）
func (h *PaperHistoryHandler) DeletePaperCheckRecord(c *gin.Context) {
	// 检查管理员权限
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, 401, "未授权访问", "")
		return
	}

	// 从数据库中查询用户信息以验证权限
	var user model.User
	result := database.DB.Select("role").First(&user, "id = ?", userID)
	if result.Error != nil {
		utils.ErrorResponse(c, 401, "用户不存在", result.Error.Error())
		return
	}

	// 检查用户角色是否为管理员
	if user.Role != "admin" {
		utils.ErrorResponse(c, 403, "需要管理员权限", "用户角色: "+user.Role)
		return
	}

	// 解析记录ID
	recordID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, 400, "无效的记录ID", err.Error())
		return
	}

	var record model.CheckResult

	// 检查记录是否存在
	if err := database.DB.Where("id = ?", recordID).First(&record).Error; err != nil {
		utils.ErrorResponse(c, 404, "记录不存在", err.Error())
		return
	}

	// 删除记录
	if err := database.DB.Delete(&record).Error; err != nil {
		utils.ErrorResponse(c, 500, "删除记录失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}
