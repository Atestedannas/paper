package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// BillingHandler 计费配置处理器
type BillingHandler struct {
	billingService service.BillingService
}

// NewBillingHandler 创建计费配置处理器
func NewBillingHandler() *BillingHandler {
	return &BillingHandler{
		billingService: service.NewBillingService(),
	}
}

// GetServicePricings 获取所有服务定价
func (h *BillingHandler) GetServicePricings(c *gin.Context) {
	pricings, err := h.billingService.GetAllServicePricings()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取服务定价失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", pricings)
}

// GetServicePricing 获取单个服务定价
func (h *BillingHandler) GetServicePricing(c *gin.Context) {
	serviceType := c.Param("type")
	if serviceType == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "服务类型不能为空", "")
		return
	}

	pricing, err := h.billingService.GetServicePricingByType(serviceType)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取服务定价失败", err.Error())
		return
	}

	if pricing == nil {
		utils.ErrorResponse(c, http.StatusNotFound, "服务定价不存在", "")
		return
	}

	utils.SuccessResponse(c, "获取成功", pricing)
}

// CreateServicePricing 创建服务定价
func (h *BillingHandler) CreateServicePricing(c *gin.Context) {
	var pricing struct {
		ServiceType  string  `json:"service_type" binding:"required"`
		ServiceName  string  `json:"service_name" binding:"required"`
		PricingModel string  `json:"pricing_model" binding:"required"`
		IsEnabled    bool    `json:"is_enabled"`
		IsFree       bool    `json:"is_free"`
		Price        float64 `json:"price"`
		Currency     string  `json:"currency"`
		FreeCount    int     `json:"free_count"`
		Description  string  `json:"description"`
		SortOrder    int     `json:"sort_order"`
	}

	if err := c.ShouldBindJSON(&pricing); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	// 检查服务类型是否已存在
	existing, _ := h.billingService.GetServicePricingByType(pricing.ServiceType)
	if existing != nil {
		utils.ErrorResponse(c, http.StatusConflict, "服务类型已存在", "")
		return
	}

	newPricing := &model.ServicePricing{
		ServiceType:  pricing.ServiceType,
		ServiceName:  pricing.ServiceName,
		PricingModel: model.PricingModel(pricing.PricingModel),
		IsEnabled:    pricing.IsEnabled,
		IsFree:       pricing.IsFree,
		Price:        pricing.Price,
		Currency:     pricing.Currency,
		FreeCount:    pricing.FreeCount,
		Description:  pricing.Description,
		SortOrder:    pricing.SortOrder,
	}

	if err := h.billingService.CreateServicePricing(newPricing); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "创建成功", nil)
}

// UpdateServicePricing 更新服务定价
func (h *BillingHandler) UpdateServicePricing(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "ID不能为空", "")
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	if err := h.billingService.UpdateServicePricing(id, updates); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// DeleteServicePricing 删除服务定价
func (h *BillingHandler) DeleteServicePricing(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "ID不能为空", "")
		return
	}

	if err := h.billingService.DeleteServicePricing(id); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

// ToggleServicePricing 启用/禁用服务
func (h *BillingHandler) ToggleServicePricing(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "ID不能为空", "")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	if err := h.billingService.ToggleServicePricing(id, req.Enabled); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "操作失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "操作成功", nil)
}

// SetServiceFree 设置服务是否免费
func (h *BillingHandler) SetServiceFree(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "ID不能为空", "")
		return
	}

	var req struct {
		IsFree bool `json:"is_free"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	if err := h.billingService.SetServiceFree(id, req.IsFree); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "设置失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "设置成功", nil)
}

// GetAllPlans 获取所有套餐
func (h *BillingHandler) GetAllPlans(c *gin.Context) {
	plans, err := h.billingService.GetAllPlans()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取套餐失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", plans)
}

// GetPlans 获取套餐列表
func (h *BillingHandler) GetPlans(c *gin.Context) {
	serviceType := c.Query("service_type")
	if serviceType == "" {
		h.GetAllPlans(c)
		return
	}

	plans, err := h.billingService.GetPlansByServiceType(serviceType)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取套餐失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", plans)
}

// CreatePlan 创建套餐
func (h *BillingHandler) CreatePlan(c *gin.Context) {
	var plan struct {
		PlanName    string  `json:"plan_name" binding:"required"`
		PlanType    string  `json:"plan_type" binding:"required"`
		ServiceType string  `json:"service_type"`
		Price       float64 `json:"price" binding:"required"`
		Currency    string  `json:"currency"`
		PeriodDays  int     `json:"period_days"`
		CheckCount  int     `json:"check_count"`
		Description string  `json:"description"`
		Features    string  `json:"features"`
		IsActive    bool    `json:"is_active"`
		SortOrder   int     `json:"sort_order"`
	}

	if err := c.ShouldBindJSON(&plan); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	newPlan := &model.PricingPlan{
		PlanName:    plan.PlanName,
		PlanType:    plan.PlanType,
		ServiceType: plan.ServiceType,
		Price:       plan.Price,
		Currency:    plan.Currency,
		PeriodDays:  plan.PeriodDays,
		CheckCount:  plan.CheckCount,
		Description: plan.Description,
		Features:    plan.Features,
		IsActive:    plan.IsActive,
		SortOrder:   plan.SortOrder,
	}

	if err := h.billingService.CreatePlan(newPlan); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "创建成功", nil)
}

// UpdatePlan 更新套餐
func (h *BillingHandler) UpdatePlan(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "ID不能为空", "")
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	// 处理数字类型转换
	if price, ok := updates["price"].(string); ok {
		if p, err := strconv.ParseFloat(price, 64); err == nil {
			updates["price"] = p
		}
	}

	if err := h.billingService.UpdatePlan(id, updates); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// DeletePlan 删除套餐
func (h *BillingHandler) DeletePlan(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "ID不能为空", "")
		return
	}

	if err := h.billingService.DeletePlan(id); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

// GetServiceConfig 获取完整的服务配置（公开接口）
func (h *BillingHandler) GetServiceConfig(c *gin.Context) {
	config, err := h.billingService.GetServiceConfig()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取配置失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", config)
}

// CheckServicePricing 检查服务是否免费/获取价格（公开接口）
func (h *BillingHandler) CheckServicePricing(c *gin.Context) {
	serviceType := c.Query("service_type")
	if serviceType == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "服务类型不能为空", "")
		return
	}

	isFree, err := h.billingService.IsServiceFree(serviceType)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "查询失败", err.Error())
		return
	}

	price, _ := h.billingService.GetServicePrice(serviceType)

	utils.SuccessResponse(c, "获取成功", gin.H{
		"service_type": serviceType,
		"is_free":      isFree,
		"price":        price,
	})
}
