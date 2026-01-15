package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// AdminHandler 管理员处理器
type AdminHandler struct {
	userService   service.UserService
	orderService  service.OrderService
	memberService service.MemberService
	config        *config.Config
}

// NewAdminHandler 创建管理员处理器实例
func NewAdminHandler(config *config.Config) *AdminHandler {
	return &AdminHandler{
		userService:   service.NewUserService(),
		orderService:  service.NewOrderService(),
		memberService: service.NewMemberService(),
		config:        config,
	}
}

// GetDashboard 获取管理员控制台数据
func (h *AdminHandler) GetDashboard(c *gin.Context) {
	utils.SuccessResponse(c, "获取成功", gin.H{
		"user_growth":    []int{120, 200, 150, 250, 300, 400},
		"recent_users":   5,
		"pending_orders": 3,
		"total_papers":   150,
		"today_checks":   20,
	})
}

// GetSystemStats 获取系统统计数据
func (h *AdminHandler) GetSystemStats(c *gin.Context) {
	utils.SuccessResponse(c, "获取成功", gin.H{
		"total_users":  1000,
		"total_papers": 5000,
		"total_checks": 10000,
		"total_orders": 500,
	})
}

// GetUsers 获取用户列表
func (h *AdminHandler) GetUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	users, total, err := h.userService.GetAllUsers(page, pageSize)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"users":       users,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// UpdateUserRole 更新用户角色
func (h *AdminHandler) UpdateUserRole(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	var req struct {
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.userService.UpdateUserRole(userID, req.Role); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新用户角色失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// UpdateUserStatus 更新用户状态
func (h *AdminHandler) UpdateUserStatus(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.userService.UpdateUserStatus(userID, req.Status); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新用户状态失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// DeleteUser 删除用户
func (h *AdminHandler) DeleteUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	if err := h.userService.DeleteUser(userID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除用户失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

// GetPapers 获取论文列表
func (h *AdminHandler) GetPapers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	var papers []model.Paper
	var total int64

	database.DB.Model(&model.Paper{}).Count(&total)
	offset := (page - 1) * pageSize
	if err := database.DB.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&papers).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取论文列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"papers":      papers,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// GetOrders 获取订单列表
func (h *AdminHandler) GetOrders(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	var orders []model.Order
	var total int64

	database.DB.Model(&model.Order{}).Count(&total)
	offset := (page - 1) * pageSize
	if err := database.DB.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&orders).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取订单列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"orders":      orders,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}
