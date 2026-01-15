package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// MemberHandler 会员处理器
type MemberHandler struct {
	memberService service.MemberService
}

// NewMemberHandler 创建会员处理器实例
func NewMemberHandler() *MemberHandler {
	return &MemberHandler{
		memberService: service.NewMemberService(),
	}
}

// GetAllMemberLevels 获取所有会员等级
func (h *MemberHandler) GetAllMemberLevels(c *gin.Context) {
	levels, err := h.memberService.GetAllMemberLevels()
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Success(c, levels)
}

// GetMemberLevelByID 根据ID获取会员等级
func (h *MemberHandler) GetMemberLevelByID(c *gin.Context) {
	// 解析会员等级ID
	levelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid member level id")
		return
	}

	level, err := h.memberService.GetMemberLevelByID(levelID)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}

	utils.Success(c, level)
}

// GetMemberInfo 获取会员信息
func (h *MemberHandler) GetMemberInfo(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	member, err := h.memberService.GetMemberByUserID(userID.(uuid.UUID))
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}

	utils.Success(c, member)
}

// CheckMemberStatus 检查会员状态
func (h *MemberHandler) CheckMemberStatus(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	isActive, err := h.memberService.CheckMemberStatus(userID.(uuid.UUID))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Success(c, gin.H{"is_active": isActive})
}

// GetMemberRemainingChecks 获取会员剩余检查次数
func (h *MemberHandler) GetMemberRemainingChecks(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	remaining, err := h.memberService.GetMemberRemainingChecks(userID.(uuid.UUID))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Success(c, gin.H{"remaining_checks": remaining})
}

// CreateMemberLevel 创建会员等级 (管理员接口)
func (h *MemberHandler) CreateMemberLevel(c *gin.Context) {
	// 解析请求数据
	var req struct {
		LevelName    string  `json:"level_name" binding:"required,max=50"`
		Price        float64 `json:"price" binding:"required,gte=0"`
		DurationDays int     `json:"duration_days" binding:"required,gt=0"`
		MaxChecks    int     `json:"max_checks" binding:"required,gt=0"`
		MaxFileSize  int64   `json:"max_file_size" binding:"required,gt=0"`
		Features     string  `json:"features" binding:"required"`
		Description  string  `json:"description"`
		SortOrder    int     `json:"sort_order"`
		IsActive     bool    `json:"is_active"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 创建会员等级
	level := &model.MemberLevel{
		LevelName:    req.LevelName,
		Price:        req.Price,
		DurationDays: req.DurationDays,
		MaxChecks:    req.MaxChecks,
		MaxFileSize:  req.MaxFileSize,
		Features:     req.Features,
		Description:  req.Description,
		SortOrder:    req.SortOrder,
		IsActive:     req.IsActive,
	}

	if err := h.memberService.CreateMemberLevel(level); err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Created(c, level)
}

// UpdateMemberLevel 更新会员等级 (管理员接口)
func (h *MemberHandler) UpdateMemberLevel(c *gin.Context) {
	// 解析会员等级ID
	levelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid member level id")
		return
	}

	// 获取现有会员等级
	level, err := h.memberService.GetMemberLevelByID(levelID)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}

	// 解析请求数据
	var req struct {
		LevelName    string  `json:"level_name" binding:"omitempty,max=50"`
		Price        float64 `json:"price" binding:"omitempty,gte=0"`
		DurationDays int     `json:"duration_days" binding:"omitempty,gt=0"`
		MaxChecks    int     `json:"max_checks" binding:"omitempty,gt=0"`
		MaxFileSize  int64   `json:"max_file_size" binding:"omitempty,gt=0"`
		Features     string  `json:"features"`
		Description  string  `json:"description"`
		SortOrder    int     `json:"sort_order"`
		IsActive     bool    `json:"is_active"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 更新会员等级信息
	if req.LevelName != "" {
		level.LevelName = req.LevelName
	}
	if req.Price > 0 {
		level.Price = req.Price
	}
	if req.DurationDays > 0 {
		level.DurationDays = req.DurationDays
	}
	if req.MaxChecks > 0 {
		level.MaxChecks = req.MaxChecks
	}
	if req.MaxFileSize > 0 {
		level.MaxFileSize = req.MaxFileSize
	}
	if req.Features != "" {
		level.Features = req.Features
	}
	if req.Description != "" {
		level.Description = req.Description
	}
	level.SortOrder = req.SortOrder
	level.IsActive = req.IsActive

	if err := h.memberService.UpdateMemberLevel(level); err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Success(c, level)
}
