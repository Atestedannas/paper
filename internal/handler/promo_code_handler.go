package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

type PromoCodeHandler struct{ service *service.PromoCodeService }

func NewPromoCodeHandler() *PromoCodeHandler {
	return &PromoCodeHandler{service: service.NewPromoCodeService(database.DB)}
}

func (h *PromoCodeHandler) Redeem(c *gin.Context) {
	userID, ok := c.Get("user_id")
	uid, valid := userID.(uuid.UUID)
	if !ok || !valid || uid == uuid.Nil {
		utils.ErrorResponse(c, http.StatusUnauthorized, "请先登录", "")
		return
	}
	var req struct {
		Code        string `json:"code" binding:"required"`
		ServiceType string `json:"service_type" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请输入有效卡密", err.Error())
		return
	}
	grant, remaining, err := h.service.Redeem(c.Request.Context(), uid, req.Code, req.ServiceType)
	if err != nil {
		status := http.StatusBadRequest
		if !errors.Is(err, service.ErrPromoCodeInvalid) && !errors.Is(err, service.ErrPromoCodeLimit) {
			status = http.StatusInternalServerError
		}
		utils.ErrorResponse(c, status, err.Error(), "")
		return
	}
	utils.SuccessResponse(c, "体验权益已解锁", gin.H{
		"grant_id": grant.ID, "service_type": grant.ServiceType, "remaining_uses": remaining, "expires_at": grant.ExpiresAt,
	})
}

func (h *PromoCodeHandler) Generate(c *gin.Context) {
	uid, _ := c.Get("user_id")
	adminID, _ := uid.(uuid.UUID)
	var req struct {
		CampaignName string     `json:"campaign_name" binding:"required"`
		Quantity     int        `json:"quantity" binding:"required,min=1,max=500"`
		ServiceType  string     `json:"service_type" binding:"required"`
		MaxUses      int        `json:"max_uses" binding:"required,min=1"`
		PerUserLimit int        `json:"per_user_limit" binding:"required,min=1"`
		ExpiresAt    *time.Time `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	codes, plain, err := h.service.Generate(c.Request.Context(), adminID, service.GeneratePromoCodesInput{
		CampaignName: req.CampaignName, Quantity: req.Quantity, ServiceType: req.ServiceType,
		MaxUses: req.MaxUses, PerUserLimit: req.PerUserLimit, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.CreatedResponse(c, "卡密生成成功，明文仅本次返回", gin.H{"items": codes, "codes": plain})
}

func (h *PromoCodeHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, err := h.service.List(c.Request.Context(), page, pageSize, c.Query("q"))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.SuccessResponse(c, "获取成功", gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *PromoCodeHandler) SetActive(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "无效卡密 ID")
		return
	}
	var req struct {
		IsActive bool `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	if err := h.service.SetActive(c.Request.Context(), id, req.IsActive); err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.SuccessResponse(c, "状态已更新", gin.H{"id": id, "is_active": req.IsActive})
}

func (h *PromoCodeHandler) ListGrants(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		utils.BadRequest(c, "无效卡密 ID")
		return
	}
	items, err := h.service.ListGrants(c.Request.Context(), id)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.SuccessResponse(c, "获取成功", gin.H{"items": items})
}
