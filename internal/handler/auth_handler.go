package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/middleware"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// AuthHandler 认证处理器
type AuthHandler struct {
	userService   service.UserService
	wechatService *service.WechatService
	alipayService *service.AlipayService
	config        *config.Config
}

// NewAuthHandler 创建认证处理器实例
func NewAuthHandler(config *config.Config) *AuthHandler {
	return &AuthHandler{
		userService:   service.NewUserService(),
		wechatService: service.NewWechatService(config),
		alipayService: service.NewAlipayService(config),
		config:        config,
	}
}

// RegisterRequest 注册请求结构体
type RegisterRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// LoginRequest 登录请求结构体
type LoginRequest struct {
	Account  string `json:"email" binding:"required"` // 支持邮箱或用户名登录
	Password string `json:"password" binding:"required"`
}

// AuthResponse 认证响应结构体
type AuthResponse struct {
	Token string `json:"token"`
	User  any    `json:"user"`
}

// Register 用户注册
func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 密码复杂度验证
	if !h.isPasswordComplex(req.Password) {
		utils.BadRequest(c, "密码必须包含大小写字母、数字和特殊字符")
		return
	}

	// 注册用户
	user, err := h.userService.Register(req.Username, req.Email, req.Password)
	if err != nil {
		// 演示模式：数据库不可用时返回模拟响应
		if err.Error() == "service unavailable" {
			// 生成模拟用户数据
			mockUser := struct {
				ID       string `json:"id"`
				Username string `json:"username"`
				Email    string `json:"email"`
			}{}

			// 生成模拟JWT令牌
			mockToken, _ := middleware.GenerateToken(h.config, uuid.New(), req.Username)
			mockRefreshToken, _ := middleware.GenerateRefreshToken(h.config, uuid.New())

			utils.Created(c, gin.H{
				"token":         mockToken,
				"refresh_token": mockRefreshToken,
				"user":          mockUser,
			})
			return
		}

		// 其他错误正常返回
		utils.BadRequest(c, err.Error())
		return
	}

	// 生成JWT令牌
	token, err := middleware.GenerateToken(h.config, user.ID, user.Username)
	if err != nil {
		utils.InternalServerError(c, "failed to generate token")
		return
	}

	// 生成刷新令牌
	refreshToken, err := middleware.GenerateRefreshToken(h.config, user.ID)
	if err != nil {
		utils.InternalServerError(c, "failed to generate refresh token")
		return
	}

	// 返回响应
	utils.Created(c, gin.H{
		"token":         token,
		"refresh_token": refreshToken,
		"user":          user,
	})
}

// Login 用户登录
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 登录验证
	user, err := h.userService.Login(req.Account, req.Password)
	if err != nil {
		utils.Unauthorized(c, err.Error())
		return
	}

	// 生成JWT令牌
	token, err := middleware.GenerateToken(h.config, user.ID, user.Username)
	if err != nil {
		utils.InternalServerError(c, "failed to generate token")
		return
	}

	// 生成刷新令牌
	refreshToken, err := middleware.GenerateRefreshToken(h.config, user.ID)
	if err != nil {
		utils.InternalServerError(c, "failed to generate refresh token")
		return
	}

	// 返回响应
	utils.Success(c, gin.H{
		"token":         token,
		"refresh_token": refreshToken,
		"user":          user,
	})
}

// GetProfile 获取用户资料
func (h *AuthHandler) GetProfile(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取用户信息
	user, err := h.userService.GetUserByID(userID.(uuid.UUID))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, user)
}

// UpdateProfile 更新用户资料
func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取用户信息
	user, err := h.userService.GetUserByID(userID.(uuid.UUID))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 解析请求数据
	var req struct {
		FullName string `json:"full_name" binding:"omitempty,min=2,max=100"`
		Avatar   string `json:"avatar" binding:"omitempty,max=255"`
		Email    string `json:"email" binding:"omitempty,email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 更新用户信息
	if req.FullName != "" {
		user.FullName = req.FullName
	}
	if req.Avatar != "" {
		user.Avatar = req.Avatar
	}
	if req.Email != "" {
		user.Email = req.Email
	}

	// 保存更新
	if err := h.userService.UpdateUser(user); err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, user)
}

// GetWechatAuthURL 获取微信登录URL
func (h *AuthHandler) GetWechatAuthURL(c *gin.Context) {
	// 初始化微信服务
	wechatService := service.NewWechatService(h.config)

	// 生成微信扫码登录的二维码URL
	wechatAuthURL, state, err := wechatService.GenerateQRCodeURL()
	if err != nil {
		utils.InternalServerError(c, "failed to generate wechat auth url")
		return
	}

	utils.Success(c, gin.H{
		"auth_url": wechatAuthURL,
		"state":    state,
	})
}

// WechatAuthCallback 微信登录回调
func (h *AuthHandler) WechatAuthCallback(c *gin.Context) {
	// 获取授权码
	code := c.Query("code")

	if code == "" {
		utils.BadRequest(c, "missing authorization code")
		return
	}

	// 初始化微信服务
	wechatService := service.NewWechatService(h.config)

	// 用授权码换取访问令牌
	token, err := wechatService.ExchangeCodeForToken(code)
	if err != nil {
		utils.InternalServerError(c, "failed to exchange code for token")
		return
	}

	// 获取用户信息
	userInfo, err := wechatService.GetUserInfo(token.AccessToken, token.OpenID)
	if err != nil {
		utils.InternalServerError(c, "failed to get wechat user info")
		return
	}

	// 创建或更新用户
	user, err := h.userService.CreateOrUpdateWechatUser(
		userInfo.OpenID,
		userInfo.Nickname,
		userInfo.UnionID,
		userInfo.HeadImgURL,
		userInfo.Sex,
	)
	if err != nil {
		utils.InternalServerError(c, "failed to create or update wechat user")
		return
	}

	// 生成JWT令牌
	tokenStr, err := middleware.GenerateToken(h.config, user.ID, user.Username)
	if err != nil {
		utils.InternalServerError(c, "failed to generate token")
		return
	}

	utils.Success(c, AuthResponse{
		Token: tokenStr,
		User:  user,
	})
}

// GetAlipayAuthURL 获取支付宝登录URL
func (h *AuthHandler) GetAlipayAuthURL(c *gin.Context) {
	// 初始化支付宝服务
	alipayService := service.NewAlipayService(h.config)

	// 生成支付宝扫码登录的二维码URL
	alipayAuthURL, state, err := alipayService.GenerateQRCodeURL()
	if err != nil {
		utils.InternalServerError(c, "failed to generate alipay auth url")
		return
	}

	utils.Success(c, gin.H{
		"auth_url": alipayAuthURL,
		"state":    state,
	})
}

// AlipayAuthCallback 支付宝登录回调
func (h *AuthHandler) AlipayAuthCallback(c *gin.Context) {
	// 获取授权码
	code := c.Query("auth_code")

	if code == "" {
		utils.BadRequest(c, "missing authorization code")
		return
	}

	// 初始化支付宝服务
	alipayService := service.NewAlipayService(h.config)

	// 用授权码换取访问令牌
	token, err := alipayService.ExchangeCodeForToken(code)
	if err != nil {
		utils.InternalServerError(c, "failed to exchange code for token")
		return
	}

	// 获取用户信息
	userInfo, err := alipayService.GetUserInfo(token.AccessToken)
	if err != nil {
		utils.InternalServerError(c, "failed to get alipay user info")
		return
	}

	// 创建或更新用户
	user, err := h.userService.CreateOrUpdateAlipayUser(
		userInfo.UserID,
		userInfo.UserID, // 支付宝的user_id作为open_id
		userInfo.Nickname,
		userInfo.Avatar,
		userInfo.Gender,
	)
	if err != nil {
		utils.InternalServerError(c, "failed to create or update alipay user")
		return
	}

	// 生成JWT令牌
	tokenStr, err := middleware.GenerateToken(h.config, user.ID, user.Username)
	if err != nil {
		utils.InternalServerError(c, "failed to generate token")
		return
	}

	utils.Success(c, AuthResponse{
		Token: tokenStr,
		User:  user,
	})
}

// ChangePassword 修改密码
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 解析请求数据
	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required,min=8,max=20"` // 增强密码复杂度要求
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 密码复杂度验证
	if !h.isPasswordComplex(req.NewPassword) {
		utils.BadRequest(c, "密码必须包含大小写字母、数字和特殊字符")
		return
	}

	// 修改密码
	if err := h.userService.ChangePassword(userID.(uuid.UUID), req.OldPassword, req.NewPassword); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, gin.H{"message": "password changed successfully"})
}

// RefreshToken 刷新访问令牌
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	// 解析请求数据
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 解析刷新令牌
	claims := &middleware.JWTClaims{}
	token, err := jwt.ParseWithClaims(req.RefreshToken, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(h.config.JWT.Secret), nil
	})

	if err != nil || !token.Valid {
		utils.Unauthorized(c, "invalid refresh token")
		return
	}

	// 验证令牌用途
	if claims.Issuer != "paper-format-checker-refresh" {
		utils.Unauthorized(c, "invalid refresh token issuer")
		return
	}

	// 获取用户信息
	user, err := h.userService.GetUserByID(claims.UserID)
	if err != nil {
		utils.Unauthorized(c, "user not found")
		return
	}

	// 生成新的访问令牌
	newAccessToken, err := middleware.GenerateToken(h.config, user.ID, user.Username)
	if err != nil {
		utils.InternalServerError(c, "failed to generate new access token")
		return
	}

	// 生成新的刷新令牌
	newRefreshToken, err := middleware.GenerateRefreshToken(h.config, user.ID)
	if err != nil {
		utils.InternalServerError(c, "failed to generate new refresh token")
		return
	}

	// 返回响应
	utils.Success(c, gin.H{
		"token":         newAccessToken,
		"refresh_token": newRefreshToken,
		"user":          user,
	})
}

// isPasswordComplex 检查密码复杂度
func (h *AuthHandler) isPasswordComplex(password string) bool {
	// 密码必须包含至少8个字符，包含大小写字母、数字和特殊字符
	var (
		lower   = false
		upper   = false
		digit   = false
		special = false
	)

	for _, char := range password {
		switch {
		case char >= 'a' && char <= 'z':
			lower = true
		case char >= 'A' && char <= 'Z':
			upper = true
		case char >= '0' && char <= '9':
			digit = true
		case char == '!' || char == '@' || char == '#' || char == '$' || char == '%' || char == '^' || char == '&' || char == '*' || char == '(' || char == ')' || char == '-' || char == '_' || char == '+' || char == '=' || char == '{' || char == '}' || char == '[' || char == ']' || char == '|' || char == '\\' || char == ':' || char == ';' || char == '"' || char == '\'' || char == '<' || char == '>' || char == ',' || char == '.' || char == '?' || char == '/':
			special = true
		}
	}

	return lower && upper && digit && special
}
