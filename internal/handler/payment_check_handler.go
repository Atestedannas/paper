package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// PaymentCheckHandler 付费检查处理器
type PaymentCheckHandler struct {
	settingService service.SystemSettingService
}

// NewPaymentCheckHandler 创建付费检查处理器实例
func NewPaymentCheckHandler() *PaymentCheckHandler {
	return &PaymentCheckHandler{
		settingService: service.GetSystemSettingService(),
	}
}

// CheckPaperPaymentStatus 检查论文相关服务的付费状态
func (h *PaymentCheckHandler) CheckPaperPaymentStatus(c *gin.Context) {
	config, err := h.settingService.GetPaymentConfig()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取支付配置失败", err.Error())
		return
	}

	// 获取查询参数
	serviceType := c.Query("service_type")

	// 根据服务类型计算价格
	price := 0.0
	isCheckFree := false
	if isCheckFreeValue, ok := config["is_check_free"].(bool); ok {
		isCheckFree = isCheckFreeValue
	}

	switch serviceType {
	case "format_check":
		if isCheckFree {
			// 检查免费，价格为0
			price = 0
		} else if formatCheckPrice, ok := config["format_check"].(float64); ok {
			price = formatCheckPrice
		}
	case "format_fix":
		if formatFixPrice, ok := config["format_fix"].(float64); ok {
			price = formatFixPrice
		}
	case "check_and_fix":
		if isCheckFree {
			// 检查免费，价格为0
			price = 0
		} else {
			// 检查+修正都收费
			if formatCheckPrice, ok := config["format_check"].(float64); ok {
				price += formatCheckPrice
			}
			if formatFixPrice, ok := config["format_fix"].(float64); ok {
				price += formatFixPrice
			}
		}
	}

	// 返回配置和计算后的价格
	response := gin.H{
		"format_check":  config["format_check"],
		"format_fix":    config["format_fix"],
		"is_check_free": isCheckFree,
		"price":         price,
	}

	utils.SuccessResponse(c, "获取成功", response)
}

// CheckServicePaymentStatus 检查通用服务的付费状态
func (h *PaymentCheckHandler) CheckServicePaymentStatus(c *gin.Context) {
	// 从上下文获取用户ID
	userID, exists := c.Get("user_id")
	var userUUID uuid.UUID
	var isAnonymous bool

	if !exists {
		// 没有user_id，可能是匿名用户
		userUUID = uuid.Nil
		isAnonymous = true
	} else {
		var ok bool
		userUUID, ok = userID.(uuid.UUID)
		if !ok {
			// user_id格式错误，可能是匿名用户
			userUUID = uuid.Nil
			isAnonymous = true
		}
	}

	// 获取查询参数
	serviceType := c.Query("service_type")

	// 验证参数
	if serviceType == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "缺少必要参数", "service_type 参数不能为空")
		return
	}

	// 验证服务类型
	validServiceType := false
	switch serviceType {
	case "paper_download":
		validServiceType = true
	case "format_check":
		validServiceType = true
	case "format_fix":
		validServiceType = true
	case "report_download":
		validServiceType = true
	case "compare":
		validServiceType = true
	}

	if !validServiceType {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的服务类型", "支持的服务类型: paper_download, format_check, format_fix, report_download, compare")
		return
	}

	// 获取支付配置
	config, err := h.settingService.GetPaymentConfig()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取支付配置失败", err.Error())
		return
	}

	// 根据管理员设置判断是否需要付费
	isPaid := false
	price := 0.0

	// 检查管理员设置
	if serviceType == "format_check" {
		if isCheckFree, ok := config["is_check_free"].(bool); ok {
			if isCheckFree {
				isPaid = false // 管理员设置格式检查免费
			} else {
				isPaid = true
				if formatCheckPrice, ok := config["format_check"].(float64); ok {
					price = formatCheckPrice
				}
			}
		} else {
			isPaid = true
			if formatCheckPrice, ok := config["format_check"].(float64); ok {
				price = formatCheckPrice
			}
		}
	} else if serviceType == "format_fix" {
		isPaid = true
		if formatFixPrice, ok := config["format_fix"].(float64); ok {
			price = formatFixPrice
		}
	} else if serviceType == "paper_download" {
		// 论文下载价格
		if paperDownloadPrice, ok := config["paper_download"].(float64); ok {
			price = paperDownloadPrice
			if paperDownloadPrice == 0 {
				isPaid = false // 价格为0则免费
			} else {
				isPaid = true
			}
		}
	} else if serviceType == "report_download" {
		if reportDownloadPrice, ok := config["report_download"].(float64); ok {
			price = reportDownloadPrice
			if reportDownloadPrice == 0 {
				isPaid = false // 价格为0则免费
			} else {
				isPaid = true
			}
		}
	} else if serviceType == "compare" {
		if comparePrice, ok := config["compare"].(float64); ok {
			price = comparePrice
			if comparePrice == 0 {
				isPaid = false // 价格为0则免费
			} else {
				isPaid = true
			}
		}
	}

	// 如果不是匿名用户，检查是否为管理员或会员
	if !isAnonymous {
		// 检查用户是否为管理员，管理员免费
		var user model.User
		err := database.DB.Select("role").First(&user, "id = ?", userUUID).Error
		if err == nil {
			if user.Role == "admin" {
				isPaid = true // 管理员免费
			}
		}

		// 如果不是管理员且没有免费，需要检查付费状态
		if !isPaid {
			// 检查用户会员状态
			var member model.Member
			err = database.DB.Where("user_id = ? AND status = ? AND end_date > NOW()", userUUID, "active").First(&member).Error
			if err == nil {
				isPaid = true // 会员免费
			}
		}
	}

	// 构建响应
	response := gin.H{
		"service_type": serviceType,
		"is_paid":      isPaid,
		"price":        price,
		"message":      "",
	}

	if isPaid {
		response["message"] = "服务可用"
	} else {
		response["message"] = "需要付费才能使用此服务"
		response["solution"] = "请前往支付页面完成付款或购买会员套餐"
	}

	utils.SuccessResponse(c, "检查完成", response)
}

// getUserFreeChecks 获取用户剩余免费检查次数
func (h *PaymentCheckHandler) GetUserFreeChecks(c *gin.Context) {
	// 从上下文获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "未授权访问", "请先登录")
		return
	}

	userUUID, ok := userID.(uuid.UUID)
	if !ok {
		utils.ErrorResponse(c, http.StatusUnauthorized, "用户ID格式错误", "用户身份验证失败")
		return
	}

	// 获取用户信息
	var user model.User
	if err := database.DB.Select("id, free_checks").First(&user, "id = ?", userUUID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取用户信息失败", err.Error())
		return
	}

	response := gin.H{
		"user_id":      userUUID,
		"free_checks":  user.FreeChecks,
		"message":      "剩余免费检查次数",
		"can_use_free": user.FreeChecks > 0,
	}

	utils.SuccessResponse(c, "获取成功", response)
}
