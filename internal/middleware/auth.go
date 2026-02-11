package middleware

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/service"
)

// JWTClaims JWT声明结构体
type JWTClaims struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
	jwt.RegisteredClaims
}

// AuthMiddleware 认证中间件
func AuthMiddleware(config *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取Authorization头
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authorization header is required"})
			c.Abort()
			return
		}

		// 解析Bearer令牌
		parts := strings.SplitN(authHeader, " ", 2)
		if !(len(parts) == 2 && parts[0] == "Bearer") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			c.Abort()
			return
		}

		tokenString := parts[1]

		// 检查令牌是否在黑名单中
		if GlobalTokenBlacklist.IsTokenBlacklisted(tokenString) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "token has been invalidated"})
			c.Abort()
			return
		}

		// 解析JWT令牌
		claims := &JWTClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			// 验证签名算法
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(config.JWT.Secret), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		// 检查令牌是否过期
		if claims.ExpiresAt.Before(time.Now()) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "token has expired"})
			c.Abort()
			return
		}

		// 获取用户信息
		userService := service.NewUserService()
		user, err := userService.GetUserByID(claims.UserID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			c.Abort()
			return
		}

		// 检查用户状态
		if user.Status != "active" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user account is not active"})
			c.Abort()
			return
		}

		// 将用户信息存储到上下文
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("user", user)
		c.Set("role", user.Role)

		c.Next()
	}
}

// GenerateToken 生成JWT令牌
func GenerateToken(config *config.Config, userID uuid.UUID, username string) (string, error) {
	// 设置JWT声明 - 缩短有效期至1小时
	claims := JWTClaims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)), // 令牌有效期1小时
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "paper-format-checker",
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	// 创建令牌
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 签名令牌
	tokenString, err := token.SignedString([]byte(config.JWT.Secret))
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// GenerateRefreshToken 生成刷新令牌
func GenerateRefreshToken(config *config.Config, userID uuid.UUID) (string, error) {
	// 刷新令牌有效期7天
	claims := JWTClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)), // 刷新令牌有效期7天
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "paper-format-checker-refresh",
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	// 创建令牌
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 签名令牌
	tokenString, err := token.SignedString([]byte(config.JWT.Secret))
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// RequireMemberMiddleware 会员权限中间件
func RequireMemberMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取用户ID
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
			c.Abort()
			return
		}

		// 检查会员状态
		memberService := service.NewMemberService()
		isActive, err := memberService.CheckMemberStatus(userID.(uuid.UUID))
		if err != nil || !isActive {
			c.JSON(http.StatusForbidden, gin.H{"error": "member access required"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireCheckPermissionMiddleware 检查权限中间件
func RequireCheckPermissionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取用户ID
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
			c.Abort()
			return
		}

		// 检查会员状态和剩余检查次数
		memberService := service.NewMemberService()
		isActive, err := memberService.CheckMemberStatus(userID.(uuid.UUID))
		if err != nil || !isActive {
			c.JSON(http.StatusForbidden, gin.H{"error": "member access required"})
			c.Abort()
			return
		}

		// 检查剩余检查次数
		remaining, err := memberService.GetMemberRemainingChecks(userID.(uuid.UUID))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check remaining checks"})
			c.Abort()
			return
		}

		if remaining <= 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": "check count limit exceeded"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// ConditionalAuthMiddleware 条件认证中间件 - 根据系统设置决定是否需要认证
func ConditionalAuthMiddleware(config *config.Config, serviceType ServiceType) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取支付配置
		settingService := service.GetSystemSettingService()
		paymentConfig, err := settingService.GetPaymentConfig()
		if err != nil {
			// 配置获取失败，默认需要认证
			AuthMiddleware(config)(c)
			return
		}

		// 检查是否免费
		isFree := false
		if serviceType == ServiceFormatCheck {
			if isCheckFree, ok := paymentConfig["is_check_free"].(bool); ok && isCheckFree {
				isFree = true
			}
		} else if serviceType == ServicePaperDownload {
			if price, ok := paymentConfig["paper_download"].(float64); ok && price == 0 {
				isFree = true
			}
		}

		// 如果免费，跳过认证，直接设置一个默认的用户ID
		if isFree {
			// 为免费用户创建一个临时的用户ID或者设置为nil表示匿名
			c.Set("user_id", uuid.Nil) // 使用nil表示匿名用户
			c.Set("username", "anonymous")
			c.Set("role", "guest")
			c.Next()
			return
		}

		// 如果不免费，执行正常的认证
		AuthMiddleware(config)(c)
	}
}

// AdminMiddleware 管理员权限中间件
func AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查用户是否已认证
		_, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
			c.Abort()
			return
		}

		// 检查用户角色 - 允许 admin 或 super_admin 访问
		role, exists := c.Get("role")
		if !exists || (role != "admin" && role != "super_admin") {
			c.JSON(http.StatusForbidden, gin.H{"error": "admin access required"})
			c.Abort()
			return
		}

		c.Next()
	}
}
