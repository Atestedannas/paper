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
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"gorm.io/gorm"
)

func resolveAdminResource(path string) string {
	p := strings.TrimSpace(path)
	p = strings.TrimPrefix(p, "/api/v1/admin/")
	p = strings.TrimPrefix(p, "/api/admin/")
	p = strings.TrimPrefix(p, "api/v1/admin/")
	p = strings.TrimPrefix(p, "api/admin/")
	if p == "" {
		return "system"
	}

	segments := strings.Split(strings.Trim(p, "/"), "/")
	segment := segments[0]
	if segment == "settings" && len(segments) > 1 {
		if strings.HasPrefix(segments[1], "payment") {
			return "payment"
		}
		if strings.HasPrefix(segments[1], "support") {
			return "support"
		}
	}
	switch segment {
	case "users":
		return "user"
	case "papers":
		return "paper"
	case "orders", "order":
		return "order"
	case "roles", "permissions", "permission-packages", "role-permission-assign", "user-role-assign", "user-permission-assign":
		return "rbac"
	case "menus", "settings", "stats", "dashboard":
		return "system"
	case "templates":
		return "template"
	case "universities":
		return "university"
	default:
		if strings.HasSuffix(segment, "s") && len(segment) > 1 {
			return strings.TrimSuffix(segment, "s")
		}
		return segment
	}
}

func isAdminRBACSelfMenuPath(path string) bool {
	p := strings.Trim(strings.TrimSpace(path), "/")
	p = strings.TrimPrefix(p, "api/v1/admin/")
	p = strings.TrimPrefix(p, "api/admin/")
	return p == "menus/user-tree" || p == "menus/user"
}

// JWTClaims JWT声明结构体
type JWTClaims struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
	jwt.RegisteredClaims
}

func effectiveUserRole(user *model.User) string {
	for _, role := range user.Roles {
		if role.Code == "super_admin" {
			return "super_admin"
		}
	}
	return user.Role
}

// AuthMiddleware 认证中间件
func AuthMiddleware(config *config.Config, db *gorm.DB) gin.HandlerFunc {
	tokenBlacklistService := service.NewTokenBlacklistService(db)

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
		if tokenBlacklistService.IsTokenBlacklisted(tokenString) {
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
		c.Set("role", effectiveUserRole(user))

		c.Next()
	}
}

// GenerateToken 生成JWT令牌
func GenerateToken(config *config.Config, userID uuid.UUID, username string) (string, error) {
	// 使用配置中的access_token_expiry，如果没有则使用默认值1小时
	expiry := config.JWT.AccessTokenExpiry
	if expiry == 0 {
		expiry = 1 * time.Hour
	}

	// 设置JWT声明
	claims := JWTClaims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
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
func GenerateRefreshToken(config *config.Config, userID uuid.UUID) (string, time.Time, error) {
	// 使用配置中的refresh_token_expiry，如果没有则使用默认值30天
	expiry := config.JWT.RefreshTokenExpiry
	if expiry == 0 {
		expiry = 30 * 24 * time.Hour
	}

	expiresAt := time.Now().Add(expiry)

	// 刷新令牌有效期更长
	claims := JWTClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
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
		return "", time.Time{}, err
	}

	return tokenString, expiresAt, nil
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
func ConditionalAuthMiddleware(config *config.Config, db *gorm.DB, serviceType ServiceType) gin.HandlerFunc {
	// 历史版本在免费配置下会注入 uuid.Nil/anonymous，导致未登录用户仍可上传或下载。
	// 现在统一收口：所有论文资源接口先登录，是否免费只交给 PaymentMiddleware 判断。
	_ = serviceType
	return AuthMiddleware(config, db)
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
		if isAdminRBACSelfMenuPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		role, exists := c.Get("role")
		if !exists || (role != "admin" && role != "super_admin") {
			c.JSON(http.StatusForbidden, gin.H{"error": "admin access required"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// AdminRBACMiddleware 管理端细粒度权限校验（super_admin 直通，其他 admin 按权限码匹配）
func AdminRBACMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get("role")
		if role == "super_admin" {
			c.Next()
			return
		}

		if isAdminRBACSelfMenuPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		userIDValue, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
			c.Abort()
			return
		}

		userID, ok := userIDValue.(uuid.UUID)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
			c.Abort()
			return
		}

		rbacService, err := service.NewRBACService()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initialize rbac service"})
			c.Abort()
			return
		}

		resource := resolveAdminResource(c.Request.URL.Path)
		action := c.Request.Method
		allowed, err := rbacService.HasPermission(userID, resource, action)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "permission check failed"})
			c.Abort()
			return
		}
		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{
				"error":    "permission denied",
				"resource": resource,
				"action":   action,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
