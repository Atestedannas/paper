package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

func toUserIDString(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case uuid.UUID:
		return v.String(), nil
	default:
		return "", fmt.Errorf("unsupported user_id type: %T", value)
	}
}

// CasbinMiddleware Casbin 权限中间件
func CasbinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取 Casbin 服务
		casbinService := service.NewCasbinService()

		// 获取用户 ID
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, http.StatusUnauthorized, "未登录", "")
			c.Abort()
			return
		}

		// 获取请求路径和方法
		obj := c.Request.URL.Path
		act := c.Request.Method

		// 使用 Casbin 检查权限
		sub, err := toUserIDString(userID)
		if err != nil {
			utils.ErrorResponse(c, http.StatusUnauthorized, "用户 ID 格式错误", err.Error())
			c.Abort()
			return
		}
		pass, err := casbinService.Enforce(sub, obj, act)
		if err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "权限检查失败", err.Error())
			c.Abort()
			return
		}

		if !pass {
			utils.ErrorResponse(c, http.StatusForbidden, "无权限访问该资源", "")
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireRole 需要特定角色
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		casbinService := service.NewCasbinService()

		// 获取用户 ID
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, http.StatusUnauthorized, "未登录", "")
			c.Abort()
			return
		}

		// 检查用户是否有任意一个角色
		userStr, err := toUserIDString(userID)
		if err != nil {
			utils.ErrorResponse(c, http.StatusUnauthorized, "用户 ID 格式错误", err.Error())
			c.Abort()
			return
		}
		hasRole := false
		for _, role := range roles {
			roles, err := casbinService.GetImplicitRolesForUser(userStr)
			if err != nil {
				continue
			}

			for _, userRole := range roles {
				if userRole == role {
					hasRole = true
					break
				}
			}

			if hasRole {
				break
			}
		}

		if !hasRole {
			utils.ErrorResponse(c, http.StatusForbidden, "缺少所需角色", "")
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequirePermission 需要特定权限
func RequirePermission(resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		casbinService := service.NewCasbinService()

		// 获取用户 ID
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, http.StatusUnauthorized, "未登录", "")
			c.Abort()
			return
		}

		// 检查权限
		userStr, err := toUserIDString(userID)
		if err != nil {
			utils.ErrorResponse(c, http.StatusUnauthorized, "用户 ID 格式错误", err.Error())
			c.Abort()
			return
		}
		pass, err := casbinService.Enforce(userStr, resource, action)
		if err != nil || !pass {
			utils.ErrorResponse(c, http.StatusForbidden, "无权限访问该资源", "")
			c.Abort()
			return
		}

		c.Next()
	}
}
