package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// OrderHandler 订单处理器
type OrderHandler struct {
	orderService service.OrderService
}

// NewOrderHandler 创建订单处理器实例
func NewOrderHandler() *OrderHandler {
	return &OrderHandler{
		orderService: service.NewOrderService(),
	}
}

// CreateOrder 创建订单
func (h *OrderHandler) CreateOrder(c *gin.Context) {
	// 解析请求数据
	var req struct {
		ServiceType   string  `json:"service_type"`
		Amount        float64 `json:"amount"`
		PaperID       string  `json:"paper_id"`
		TemplateID    string  `json:"template_id"`
		PaymentMethod string  `json:"payment_method" binding:"required,oneof=wechat alipay"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 创建论文检查订单（不使用会员等级）
	order, err := h.orderService.CreatePaperCheckOrder(userID.(uuid.UUID), req.ServiceType, req.Amount, req.PaperID, req.TemplateID, req.PaymentMethod)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Created(c, order)
}

// GetOrderByID 根据ID获取订单
func (h *OrderHandler) GetOrderByID(c *gin.Context) {
	// 解析订单ID
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid order id")
		return
	}

	// 获取订单信息
	order, err := h.orderService.GetOrderByID(orderID)
	if err != nil {
		utils.NotFound(c, "订单不存在")
		return
	}

	utils.Success(c, order)
}

// GetOrdersByUserID 获取用户所有订单
func (h *OrderHandler) GetOrdersByUserID(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 解析分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	// 调用服务获取订单列表
	orders, total, err := h.orderService.GetOrdersByUserID(userID.(uuid.UUID), page, pageSize)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Success(c, gin.H{
		"orders":    orders,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// UpdateOrderStatus 更新订单状态
func (h *OrderHandler) UpdateOrderStatus(c *gin.Context) {
	// 解析请求数据
	var req struct {
		OrderID       uuid.UUID `json:"order_id" binding:"required"`
		OrderStatus   string    `json:"order_status" binding:"required"`
		PaymentStatus string    `json:"payment_status" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	if err := h.orderService.UpdateOrderStatus(req.OrderID, req.OrderStatus, req.PaymentStatus); err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Success(c, gin.H{"message": "order status updated successfully"})
}

// CancelOrder 取消订单
func (h *OrderHandler) CancelOrder(c *gin.Context) {
	// 解析订单ID
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid order id")
		return
	}

	// 取消订单
	if err := h.orderService.CancelOrder(orderID); err != nil {
		utils.InternalServerError(c, "取消订单失败")
		return
	}

	utils.Success(c, gin.H{"message": "order canceled successfully"})
}

// GetOrderStatistics 获取订单统计信息
func (h *OrderHandler) GetOrderStatistics(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	stats, err := h.orderService.GetOrderStatistics(userID.(uuid.UUID))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Success(c, stats)
}

// GetOrders 获取所有订单（管理员）
func (h *OrderHandler) GetOrders(c *gin.Context) {
	// 从JWT中获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, 401, "未授权访问", "")
		return
	}

	// 从数据库中查询用户信息以验证权限
	var user struct {
		Role string `json:"role"`
	}
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
	statusFilter := c.Query("status")

	// 调用服务获取所有订单
	orders, total, err := h.orderService.GetAllOrders(page, pageSize, statusFilter)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Success(c, gin.H{
		"orders":    orders,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetOrderStatisticsForAdmin 获取订单统计信息（管理员）
func (h *OrderHandler) GetOrderStatisticsForAdmin(c *gin.Context) {
	// 从JWT中获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, 401, "未授权访问", "")
		return
	}

	// 从数据库中查询用户信息以验证权限
	var user struct {
		Role string `json:"role"`
	}
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

	stats, err := h.orderService.GetOrderStatisticsForAdmin()
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Success(c, stats)
}
