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

type ServiceType string

const (
	ServicePaperDownload  ServiceType = "paper_download"
	ServiceFormatCheck    ServiceType = "format_check"
	ServiceFormatFix      ServiceType = "format_fix"
	ServiceReportDownload ServiceType = "report_download"
	ServiceCompare        ServiceType = "compare"
)

func PaymentMiddleware(config *config.Config, serviceType ServiceType) gin.HandlerFunc {
	return func(c *gin.Context) {
		price := servicePriceFromConfig(config, serviceType)

		userIDInterface, exists := c.Get("user_id")
		if !exists {
			c.AbortWithStatusJSON(401, gin.H{
				"error":   "unauthorized",
				"message": "请先登录",
			})
			return
		}
		userID, ok := userIDInterface.(uuid.UUID)
		if !ok || userID == uuid.Nil {
			c.AbortWithStatusJSON(401, gin.H{
				"error":   "invalid_user",
				"message": "用户身份验证失败",
			})
			return
		}

		paid, err := CheckUserPaymentStatus(userID, serviceType, price)
		if err != nil {
			c.AbortWithStatusJSON(500, gin.H{
				"error":   "payment_check_failed",
				"message": err.Error(),
			})
			return
		}

		if !paid {
			c.AbortWithStatusJSON(402, gin.H{
				"error":        "payment_required",
				"message":      "此服务需要付费后才能使用",
				"service_type": string(serviceType),
				"price":        price,
				"solution":     "请先完成付款或购买会员套餐",
			})
			return
		}

		c.Next()
	}
}

func servicePriceFromConfig(config *config.Config, serviceType ServiceType) float64 {
	if config == nil {
		return 0
	}
	switch serviceType {
	case ServicePaperDownload:
		return config.Payment.PaperDownload
	case ServiceFormatCheck:
		return config.Payment.FormatCheck
	case ServiceFormatFix:
		return config.Payment.FormatFix
	case ServiceReportDownload:
		return config.Payment.ReportDownload
	case ServiceCompare:
		return config.Payment.Compare
	default:
		return 0
	}
}

func CheckUserPaymentStatus(userID uuid.UUID, serviceType ServiceType, price float64) (bool, error) {
	if userID == uuid.Nil {
		return false, nil
	}

	if price <= 0 {
		return true, nil
	}

	settingService := service.GetSystemSettingService()
	paymentConfig, err := settingService.GetPaymentConfig()
	if err != nil {
		return false, fmt.Errorf("获取支付配置失败: %v", err)
	}

	isFreeBySetting := serviceIsFreeBySetting(paymentConfig, serviceType)
	if isFreeBySetting {
		return true, nil
	}

	var user model.User
	if err := database.DB.First(&user, "id = ?", userID).Error; err != nil {
		return false, fmt.Errorf("获取用户信息失败: %v", err)
	}

	if user.Role == "admin" || user.IsFreeUser {
		return true, nil
	}

	var member model.Member
	err = database.DB.Where("user_id = ? AND status = ? AND end_date > NOW()", userID, "active").First(&member).Error
	if err == nil {
		var memberLevel model.MemberLevel
		if err := database.DB.First(&memberLevel, "id = ?", member.MemberLevelID).Error; err != nil {
			return false, fmt.Errorf("获取会员等级信息失败: %v", err)
		}

		features := make(map[string]interface{})
		if err := json.Unmarshal([]byte(memberLevel.Features), &features); err != nil {
			return false, fmt.Errorf("解析会员特权失败: %v", err)
		}

		serviceKey := getServiceFeatureKey(serviceType)
		if feature, exists := features[serviceKey]; exists {
			if allowed, ok := feature.(bool); ok && allowed {
				if memberLevel.MaxChecks > 0 && member.TotalChecks >= memberLevel.MaxChecks {
					return false, errors.New("已达到会员最大使用次数限制")
				}

				if err := database.DB.Model(&member).Update("total_checks", member.TotalChecks+1).Error; err != nil {
					return false, fmt.Errorf("更新使用次数失败: %v", err)
				}

				return true, nil
			}
		}
	}

	var paymentLink model.PaymentResourceLink
	err = database.DB.Joins("JOIN payment_records ON payment_resource_links.payment_id = payment_records.id").
		Where("payment_resource_links.user_id = ? AND payment_resource_links.service_type = ? AND payment_records.payment_status = ?",
			userID, string(serviceType), "success").
		First(&paymentLink).Error
	if err == nil {
		return true, nil
	}

	return false, nil
}

func serviceIsFreeBySetting(config map[string]interface{}, serviceType ServiceType) bool {
	if isCheckFree, ok := config["is_check_free"].(bool); ok && isCheckFree {
		return true
	}

	keyMap := map[ServiceType]string{
		ServiceFormatFix:      "format_fix",
		ServiceReportDownload: "report_download",
		ServiceCompare:        "compare",
		ServicePaperDownload:  "paper_download",
	}
	key, ok := keyMap[serviceType]
	if !ok {
		return false
	}

	servicePrice, ok := config[key].(float64)
	return ok && servicePrice == 0
}

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
