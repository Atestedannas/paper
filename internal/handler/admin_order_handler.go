package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// AdminOrderHandler 管理员订单处理器
type AdminOrderHandler struct {
	orderService service.OrderService
}

// NewAdminOrderHandler 创建管理员订单处理器实例
func NewAdminOrderHandler() *AdminOrderHandler {
	return &AdminOrderHandler{
		orderService: service.NewOrderService(),
	}
}

// checkAdminPermission 检查管理员权限
func (h *AdminOrderHandler) checkAdminPermission(c *gin.Context) bool {
	// 从JWT中获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, 401, "未授权访问", "")
		return false
	}

	// 从数据库中查询用户信息以验证权限
	var user model.User
	result := database.DB.Select("role").Preload("Roles").First(&user, "id = ?", userID)
	if result.Error != nil {
		utils.ErrorResponse(c, 401, "用户不存在", result.Error.Error())
		return false
	}

	// 调试日志：打印用户角色
	fmt.Printf("[AdminOrderHandler] 用户 ID: %s, 旧角色字段：%s, RBAC 角色数：%d\n", userID, user.Role, len(user.Roles))
	for _, role := range user.Roles {
		fmt.Printf("  - RBAC 角色：%s (代码：%s)\n", role.Name, role.Code)
	}

	// 检查用户是否有管理员角色（支持 RBAC 新系统和旧系统）
	isAdmin := false
	// 检查旧的角色字段
	if user.Role == "admin" || user.Role == "super_admin" {
		isAdmin = true
	}
	// 检查 RBAC 角色
	for _, role := range user.Roles {
		if role.Code == "admin" || role.Code == "super_admin" {
			isAdmin = true
			break
		}
	}

	if !isAdmin {
		utils.ErrorResponse(c, 403, "需要管理员权限", "用户角色："+user.Role)
		return false
	}

	return true
}

// GetOrders 获取所有订单（管理员）
func (h *AdminOrderHandler) GetOrders(c *gin.Context) {
	// 检查管理员权限
	if !h.checkAdminPermission(c) {
		return
	}

	// 解析分页和过滤参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	statusFilter := c.Query("status")

	// 兼容limit参数名
	if pageSize == 0 {
		pageSize, _ = strconv.Atoi(c.DefaultQuery("limit", "10"))
	}

	if pageSize > 100 {
		pageSize = 100
	}

	// 调用服务获取所有订单
	orders, total, err := h.orderService.GetAllOrders(page, pageSize, statusFilter)
	if err != nil {
		utils.ErrorResponse(c, 500, "获取订单列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"page":      page,
		"page_size": pageSize,
		"total":     total,
		"items":     orders,
	})
}

// GetOrderStatisticsForAdmin 获取订单统计信息（管理员）
func (h *AdminOrderHandler) GetOrderStatisticsForAdmin(c *gin.Context) {
	// 检查管理员权限
	if !h.checkAdminPermission(c) {
		return
	}

	stats, err := h.orderService.GetOrderStatisticsForAdmin()
	if err != nil {
		utils.ErrorResponse(c, 500, "获取统计信息失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", stats)
}

// GetOrderByID 获取订单详情（管理员）
func (h *AdminOrderHandler) GetOrderByID(c *gin.Context) {
	// 检查管理员权限
	if !h.checkAdminPermission(c) {
		return
	}

	// 解析订单ID
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, 400, "无效的订单ID", err.Error())
		return
	}

	// 获取订单信息
	order, err := h.orderService.GetOrderByID(orderID)
	if err != nil {
		utils.ErrorResponse(c, 404, "订单不存在", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", order)
}

// UpdateOrderStatus 更新订单状态（管理员）
func (h *AdminOrderHandler) UpdateOrderStatus(c *gin.Context) {
	// 检查管理员权限
	if !h.checkAdminPermission(c) {
		return
	}

	// 解析订单ID
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, 400, "无效的订单ID", err.Error())
		return
	}

	// 解析请求数据
	var req struct {
		OrderStatus   string `json:"order_status" binding:"required,oneof=created completed cancelled processing shipped delivered refunded"`
		PaymentStatus string `json:"payment_status" binding:"required,oneof=pending paid failed cancelled refunded"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, 400, "请求参数错误", err.Error())
		return
	}

	// 更新订单状态
	err = h.orderService.UpdateOrderStatus(orderID, req.OrderStatus, req.PaymentStatus)
	if err != nil {
		utils.ErrorResponse(c, 500, "更新订单状态失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// DeleteOrder 删除订单（管理员）
func (h *AdminOrderHandler) DeleteOrder(c *gin.Context) {
	// 检查管理员权限
	if !h.checkAdminPermission(c) {
		return
	}

	// 解析订单ID
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, 400, "无效的订单ID", err.Error())
		return
	}

	// 删除订单
	err = h.orderService.DeleteOrder(orderID)
	if err != nil {
		utils.ErrorResponse(c, 500, "删除订单失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

// BatchUpdateOrderStatus 批量更新订单状态（管理员）
func (h *AdminOrderHandler) BatchUpdateOrderStatus(c *gin.Context) {
	// 检查管理员权限
	if !h.checkAdminPermission(c) {
		return
	}

	// 解析请求数据
	var req struct {
		OrderIDs    []uuid.UUID `json:"order_ids" binding:"required,min=1,max=100"`
		OrderStatus string      `json:"order_status" binding:"required,oneof=created completed cancelled processing shipped delivered refunded"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, 400, "请求参数错误", err.Error())
		return
	}

	// 批量更新订单状态
	err := h.orderService.BatchUpdateOrderStatus(req.OrderIDs, req.OrderStatus)
	if err != nil {
		utils.ErrorResponse(c, 500, "批量更新订单状态失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "批量更新成功", nil)
}
