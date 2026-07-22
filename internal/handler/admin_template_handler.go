package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// AdminTemplateHandler 后台模板管理处理器
type AdminTemplateHandler struct {
	templateParserService service.TemplateParserService
}

// NewAdminTemplateHandler 创建后台模板管理处理器
func NewAdminTemplateHandler() *AdminTemplateHandler {
	return &AdminTemplateHandler{
		templateParserService: service.NewTemplateParserService(),
	}
}

// GetTemplates 获取模板列表
func (h *AdminTemplateHandler) GetTemplates(c *gin.Context) {
	// 检查管理员权限
	if !h.checkAdminPermission(c) {
		return
	}

	// 解析查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	universityID := c.Query("university_id")
	source := c.Query("source")
	isActiveStr := c.Query("is_active")

	// 构建查询条件
	query := database.DB.Model(&model.FormatTemplate{}).Preload("University")

	if universityID != "" {
		if uid, err := strconv.ParseInt(universityID, 10, 64); err == nil {
			query = query.Where("university_id = ?", uid)
		}
	}

	if source != "" {
		query = query.Where("source = ?", source)
	}

	if isActiveStr != "" {
		if isActive, err := strconv.ParseBool(isActiveStr); err == nil {
			query = query.Where("is_active = ?", isActive)
		}
	}

	// 获取总数
	var total int64
	query.Count(&total)

	// 分页查询
	var templates []model.FormatTemplate
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&templates).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取模板列表失败", err.Error())
		return
	}

	// 兼容前端的 items 读取习惯
	items := make([]gin.H, 0, len(templates))
	for _, t := range templates {
		universityName := ""
		if t.University != nil {
			universityName = t.University.Name
		}
		items = append(items, gin.H{
			"id":            t.ID,
			"template_id":   t.TemplateID,
			"name":          t.Name,
			"university_id": t.UniversityID,
			"university":    universityName,
			"document_type": t.DocumentType,
			"subject":       t.Subject,
			"source":        t.Source,
			"version":       t.Version,
			"enabled":       t.IsActive,
			"is_active":     t.IsActive,
			"is_public":     t.IsPublic,
			"description":   t.Description,
			"created_at":    t.CreatedAt,
			"updated_at":    t.UpdatedAt,
		})
	}

	utils.Success(c, gin.H{
		"items":     items,
		"templates": templates,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetTemplate 获取模板详情
func (h *AdminTemplateHandler) GetTemplate(c *gin.Context) {
	if !h.checkAdminPermission(c) {
		return
	}

	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	var template model.FormatTemplate
	if err := database.DB.Preload("University").First(&template, "id = ?", templateUUID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "模板不存在", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", template)
}

// CreateTemplate 创建模板
func (h *AdminTemplateHandler) CreateTemplate(c *gin.Context) {
	if !h.checkAdminPermission(c) {
		return
	}

	var req CreateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 验证必填字段
	if req.Name == "" || req.FormatRules == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "模板名称和格式规则不能为空", "")
		return
	}

	// 生成模板ID
	templateID := fmt.Sprintf("admin_%s", uuid.New().String()[:8])

	// 创建模板
	template := &model.FormatTemplate{
		TemplateID:   templateID,
		Name:         req.Name,
		UniversityID: req.UniversityID,
		DocumentType: req.DocumentType,
		Source:       "system",
		Version:      req.Version,
		IsPublic:     req.IsPublic,
		IsActive:     true,
		FormatRules:  req.FormatRules,
		Description:  req.Description,
	}

	if err := database.DB.Create(template).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建模板失败", err.Error())
		return
	}

	// 预加载关联数据
	database.DB.Preload("University").First(template, "id = ?", template.ID)

	utils.SuccessResponse(c, "创建成功", template)
}

// UpdateTemplate 更新模板
func (h *AdminTemplateHandler) UpdateTemplate(c *gin.Context) {
	if !h.checkAdminPermission(c) {
		return
	}

	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	var req UpdateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 获取现有模板
	var template model.FormatTemplate
	if err := database.DB.First(&template, "id = ?", templateUUID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "模板不存在", err.Error())
		return
	}

	// 更新字段
	updateData := make(map[string]interface{})

	if req.Name != "" {
		updateData["name"] = req.Name
	}
	if req.UniversityID != nil {
		updateData["university_id"] = *req.UniversityID
	}
	if req.DocumentType != "" {
		updateData["document_type"] = req.DocumentType
	}
	if req.Version != "" {
		updateData["version"] = req.Version
	}
	if req.IsPublic != nil {
		updateData["is_public"] = *req.IsPublic
	}
	if req.IsActive != nil {
		updateData["is_active"] = *req.IsActive
	}
	if req.FormatRules != "" {
		updateData["format_rules"] = req.FormatRules
	}
	if req.Description != "" {
		updateData["description"] = req.Description
	}

	// 执行更新
	if err := database.UpdateFormatTemplateWithAudit(&template, updateData, auditActorID(c)); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新模板失败", err.Error())
		return
	}

	// 重新加载数据
	database.DB.Preload("University").First(&template, "id = ?", template.ID)

	utils.SuccessResponse(c, "更新成功", template)
}

func auditActorID(c *gin.Context) *uuid.UUID {
	value, exists := c.Get("user_id")
	if !exists {
		return nil
	}
	id, ok := value.(uuid.UUID)
	if !ok || id == uuid.Nil {
		return nil
	}
	return &id
}

// DeleteTemplate 删除模板
func (h *AdminTemplateHandler) DeleteTemplate(c *gin.Context) {
	if !h.checkAdminPermission(c) {
		return
	}

	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	// 检查模板是否存在
	var template model.FormatTemplate
	if err := database.DB.First(&template, "id = ?", templateUUID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "模板不存在", err.Error())
		return
	}

	tx := database.DB.Begin()

	// 1. 删除该模板关联的 format_corrections（叶子节点，最先删）
	if err := tx.Where("check_result_id IN (SELECT id FROM check_results WHERE template_id = ?)", templateUUID).
		Delete(&model.FormatCorrection{}).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除修正记录失败", err.Error())
		return
	}

	// 2. 删除该模板关联的 check_results
	if err := tx.Where("template_id = ?", templateUUID).
		Delete(&model.CheckResult{}).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除检查结果失败", err.Error())
		return
	}

	// 3. 解除 papers 表中对该模板的引用
	if err := tx.Model(&model.Paper{}).
		Where("selected_template_id = ?", templateUUID).
		Update("selected_template_id", nil).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(c, http.StatusInternalServerError, "解除论文关联失败", err.Error())
		return
	}

	// 4. 物理删除模板
	if err := tx.Delete(&template).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除模板失败", err.Error())
		return
	}

	tx.Commit()
	utils.SuccessResponse(c, "删除成功", nil)
}

// ToggleTemplate 启停模板
func (h *AdminTemplateHandler) ToggleTemplate(c *gin.Context) {
	if !h.checkAdminPermission(c) {
		return
	}

	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := database.DB.Model(&model.FormatTemplate{}).
		Where("id = ?", templateUUID).
		Update("is_active", req.Enabled).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新模板状态失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", gin.H{
		"id":      templateUUID,
		"enabled": req.Enabled,
	})
}

// GetTemplateVersions 获取模板版本列表（当前返回简化版本视图）
func (h *AdminTemplateHandler) GetTemplateVersions(c *gin.Context) {
	if !h.checkAdminPermission(c) {
		return
	}

	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	var template model.FormatTemplate
	if err := database.DB.First(&template, "id = ?", templateUUID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "模板不存在", err.Error())
		return
	}

	utils.Success(c, gin.H{
		"items": []gin.H{
			{
				"id":         template.ID,
				"version":    template.Version,
				"created_at": template.UpdatedAt.Format(time.RFC3339),
			},
		},
	})
}

// PromoteTemplateVersion 设为当前版本（当前实现为兼容接口）
func (h *AdminTemplateHandler) PromoteTemplateVersion(c *gin.Context) {
	if !h.checkAdminPermission(c) {
		return
	}

	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	// 校验模板存在
	var template model.FormatTemplate
	if err := database.DB.First(&template, "id = ?", templateUUID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "模板不存在", err.Error())
		return
	}

	// 目前无独立版本表，返回兼容成功响应
	utils.SuccessResponse(c, "设置成功", gin.H{
		"id":      template.ID,
		"version": template.Version,
	})
}

// ParsePaperToTemplate 从论文解析模板
func (h *AdminTemplateHandler) ParsePaperToTemplate(c *gin.Context) {
	if !h.checkAdminPermission(c) {
		return
	}

	var req ParseTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	paperUUID, err := uuid.Parse(req.PaperID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的论文ID", err.Error())
		return
	}

	// 从论文创建模板
	template, err := h.templateParserService.CreateTemplateFromPaper(paperUUID, req.TemplateName, req.UniversityID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "解析模板失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "解析成功", template)
}

// GetTemplateUsageStats 获取模板使用统计
func (h *AdminTemplateHandler) GetTemplateUsageStats(c *gin.Context) {
	if !h.checkAdminPermission(c) {
		return
	}

	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	// 获取使用统计
	var stats TemplateUsageStats

	// 使用次数
	database.DB.Model(&model.Paper{}).Where("selected_template_id = ?", templateUUID).Count(&stats.UsageCount)

	// 成功检查次数
	database.DB.Model(&model.CheckResult{}).Where("template_id = ? AND status = ?", templateUUID, "completed").Count(&stats.SuccessfulChecks)

	// 失败检查次数
	database.DB.Model(&model.CheckResult{}).Where("template_id = ? AND status = ?", templateUUID, "failed").Count(&stats.FailedChecks)

	// 计算成功率
	totalChecks := stats.SuccessfulChecks + stats.FailedChecks
	if totalChecks > 0 {
		stats.SuccessRate = float64(stats.SuccessfulChecks) / float64(totalChecks)
	}

	// 最近使用时间
	var lastUsed time.Time
	database.DB.Model(&model.CheckResult{}).Where("template_id = ?", templateUUID).
		Order("created_at DESC").Limit(1).Pluck("created_at", &lastUsed)
	stats.LastUsedAt = &lastUsed

	utils.SuccessResponse(c, "获取成功", stats)
}

// checkAdminPermission 检查管理员权限
func (h *AdminTemplateHandler) checkAdminPermission(c *gin.Context) bool {
	// 从JWT中获取用户角色（AuthMiddleware 中键名为 role）
	role, exists := c.Get("role")
	if !exists || (role != "admin" && role != "super_admin") {
		utils.ErrorResponse(c, http.StatusForbidden, "需要管理员权限", "")
		return false
	}
	return true
}

// 请求和响应结构体
type TemplateListResponse struct {
	Templates []model.FormatTemplate `json:"templates"`
	Total     int64                  `json:"total"`
	Page      int                    `json:"page"`
	PageSize  int                    `json:"page_size"`
}

type CreateTemplateRequest struct {
	Name         string `json:"name" binding:"required"`
	UniversityID *int64 `json:"university_id"`
	DocumentType string `json:"document_type"`
	Version      string `json:"version"`
	IsPublic     bool   `json:"is_public"`
	FormatRules  string `json:"format_rules" binding:"required"`
	Description  string `json:"description"`
}

type UpdateTemplateRequest struct {
	Name         string `json:"name"`
	UniversityID *int64 `json:"university_id"`
	DocumentType string `json:"document_type"`
	Version      string `json:"version"`
	IsPublic     *bool  `json:"is_public"`
	IsActive     *bool  `json:"is_active"`
	FormatRules  string `json:"format_rules"`
	Description  string `json:"description"`
}

type ParseTemplateRequest struct {
	PaperID      string `json:"paper_id" binding:"required"`
	TemplateName string `json:"template_name" binding:"required"`
	UniversityID *int64 `json:"university_id"`
}

type TemplateUsageStats struct {
	UsageCount       int64      `json:"usage_count"`
	SuccessfulChecks int64      `json:"successful_checks"`
	FailedChecks     int64      `json:"failed_checks"`
	SuccessRate      float64    `json:"success_rate"`
	LastUsedAt       *time.Time `json:"last_used_at"`
}
