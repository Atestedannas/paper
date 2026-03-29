package middleware

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
)

// ServiceType 服务类型
type ServiceType string

// 服务类型常量
const (
	ServicePaperDownload  ServiceType = "paper_download"
	ServiceFormatCheck    ServiceType = "format_check"
	ServiceFormatFix      ServiceType = "format_fix"
	ServiceReportDownload ServiceType = "report_download"
	ServiceCompare        ServiceType = "compare"
)

// PaymentMiddleware 付费检查中间件
func PaymentMiddleware(config *config.Config, serviceType ServiceType) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取服务价格
		var price float64
		switch serviceType {
		case ServicePaperDownload:
			price = config.Payment.PaperDownload
		case ServiceFormatCheck:
			price = config.Payment.FormatCheck
		case ServiceFormatFix:
			price = config.Payment.FormatFix
		case ServiceReportDownload:
			price = config.Payment.ReportDownload
		case ServiceCompare:
			price = config.Payment.Compare
		}

		// 获取支付配置，用于检查是否有免费设置
		settingService := service.GetSystemSettingService()
		paymentConfig, err := settingService.GetPaymentConfig()
		if err != nil {
			// 配置获取失败，默认使用价格配置
			paymentConfig = map[string]interface{}{
				"is_check_free": false,
			}
		}

		// 检查是否免费
		isFree := false
		if serviceType == ServiceFormatCheck {
			if isCheckFree, ok := paymentConfig["is_check_free"].(bool); ok && isCheckFree {
				isFree = true
			}
		} else if price <= 0 {
			isFree = true
		}
		if !isFree {
			keyMap := map[ServiceType]string{
				ServiceFormatFix:      "format_fix",
				ServiceReportDownload: "report_download",
				ServiceCompare:        "compare",
				ServicePaperDownload:  "paper_download",
			}
			if k, ok := keyMap[serviceType]; ok {
				if sp, ok2 := paymentConfig[k].(float64); ok2 && sp == 0 {
					isFree = true
				}
			}
		}

		// 从上下文获取用户ID
		userIDInterface, exists := c.Get("user_id")
		var userID uuid.UUID
		if exists {
			var ok bool
			userID, ok = userIDInterface.(uuid.UUID)
			if !ok {
				// 如果user_id格式错误，但服务是免费的，允许匿名访问
				if isFree {
					userID = uuid.Nil
				} else {
					c.AbortWithStatusJSON(401, gin.H{
						"error":   "用户ID格式错误",
						"message": "用户身份验证失败",
					})
					return
				}
			}
		} else {
			// 如果没有user_id，且服务是免费的，允许匿名访问
			if isFree {
				userID = uuid.Nil
			} else {
				c.AbortWithStatusJSON(401, gin.H{
					"error":   "未授权访问",
					"message": "请先登录",
				})
				return
			}
		}

		// 检查用户付费状态
		paid, err := CheckUserPaymentStatus(userID, serviceType, price)
		if err != nil {
			c.AbortWithStatusJSON(500, gin.H{
				"error":   "检查付费状态失败",
				"message": err.Error(),
			})
			return
		}

		if !paid {
			c.AbortWithStatusJSON(402, gin.H{
				"error":        "需要付费",
				"message":      "此服务需要付费才能使用",
				"service_type": string(serviceType),
				"price":        price,
				"solution":     "请前往支付页面完成付款或购买会员套餐",
			})
			return
		}

		// 付费检查通过，继续处理请求
		c.Next()
	}
}

// CheckUserPaymentStatus 检查用户付费状态（公共函数）
func CheckUserPaymentStatus(userID uuid.UUID, serviceType ServiceType, price float64) (bool, error) {
	// 获取支付配置
	settingService := service.GetSystemSettingService()
	config, err := settingService.GetPaymentConfig()
	if err != nil {
		return false, fmt.Errorf("获取支付配置失败: %v", err)
	}

	// 根据管理员设置判断是否需要付费
	isPaid := false

	// 检查管理员设置
	switch serviceType {
	case ServiceFormatCheck:
		if isCheckFree, ok := config["is_check_free"].(bool); ok {
			if isCheckFree {
				isPaid = true // 管理员设置格式检查免费
			}
		}
	case ServiceFormatFix:
		if servicePrice, ok := config["format_fix"].(float64); ok && servicePrice == 0 {
			isPaid = true
		}
	case ServicePaperDownload, ServiceReportDownload, ServiceCompare:
		var priceKey string
		switch serviceType {
		case ServicePaperDownload:
			priceKey = "paper_download"
		case ServiceReportDownload:
			priceKey = "report_download"
		case ServiceCompare:
			priceKey = "compare"
		}
		if servicePrice, ok := config[priceKey].(float64); ok && servicePrice == 0 {
			isPaid = true
		}
	}

	// 如果价格为0，则直接免费
	if price <= 0 {
		return true, nil
	}

	// 如果userID为nil，表示匿名用户
	if userID == uuid.Nil {
		// 匿名用户只能访问免费服务
		return isPaid, nil
	}

	// 获取用户信息
	var user model.User
	if err := database.DB.First(&user, "id = ?", userID).Error; err != nil {
		return false, fmt.Errorf("获取用户信息失败: %v", err)
	}

	// 管理员或管理员指定的免费用户，直接放行
	if user.Role == "admin" || user.IsFreeUser {
		return true, nil
	}

	// 如果管理员设置免费或价格为0，则跳过付费检查
	if isPaid {
		return true, nil
	}

	// 检查用户会员状态
	var member model.Member
	err = database.DB.Where("user_id = ? AND status = ? AND end_date > NOW()", userID, "active").First(&member).Error
	if err == nil {
		// 用户有有效会员
		var memberLevel model.MemberLevel
		if err := database.DB.First(&memberLevel, "id = ?", member.MemberLevelID).Error; err != nil {
			return false, fmt.Errorf("获取会员等级信息失败: %v", err)
		}

		// 检查服务是否在会员特权范围内
		features := make(map[string]interface{})
		if err := json.Unmarshal([]byte(memberLevel.Features), &features); err != nil {
			return false, fmt.Errorf("解析会员特权失败: %v", err)
		}

		// 检查对应的服务是否可用
		serviceKey := getServiceFeatureKey(serviceType)
		if feature, exists := features[serviceKey]; exists {
			if allowed, ok := feature.(bool); ok && allowed {
				// 检查使用次数限制
				if memberLevel.MaxChecks > 0 && member.TotalChecks >= memberLevel.MaxChecks {
					return false, errors.New("已达到会员最大使用次数限制")
				}

				// 增加使用次数
				if err := database.DB.Model(&member).Update("total_checks", member.TotalChecks+1).Error; err != nil {
					return false, fmt.Errorf("更新使用次数失败: %v", err)
				}

				return true, nil
			}
		}
	}

	// 检查是否有对应的支付记录
	var paymentLink model.PaymentResourceLink
	err = database.DB.Joins("JOIN payment_records ON payment_resource_links.payment_id = payment_records.id").
		Where("payment_resource_links.user_id = ? AND payment_resource_links.service_type = ? AND payment_records.payment_status = ?",
			userID, string(serviceType), "success").
		First(&paymentLink).Error

	if err == nil {
		// 有有效的支付记录
		return true, nil
	}

	// 所有检查都不通过，需要付费
	return false, nil
}

// getServiceFeatureKey 获取服务对应的会员特权键名
func getServiceFeatureKey(serviceType ServiceType) string {
	switch serviceType {
	case ServicePaperDownload:
		return "paper_download"
	case ServiceFormatCheck:
		return "format_check"
	case ServiceFormatFix:
		return "format_fix"
	case ServiceReportDownload:
		return "report_download"
	case ServiceCompare:
		return "compare"
	default:
		return string(serviceType)
	}
}
