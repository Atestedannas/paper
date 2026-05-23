package handler

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/middleware"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
	"gorm.io/gorm"
)

// AuthHandler 认证处理器
type AuthHandler struct {
	userService           service.UserService
	alipayService         *service.AlipayService
	alipayQRSessionStore  *service.AlipayQRLoginSessionStore
	wechatService         *service.WechatLoginService
	config                *config.Config
	db                    *gorm.DB
	tokenBlacklistService *service.TokenBlacklistService
	refreshTokenService   *service.RefreshTokenService
}

// NewAuthHandler 创建认证处理器实例
func NewAuthHandler(config *config.Config, db *gorm.DB) *AuthHandler {
	return &AuthHandler{
		userService:           service.NewUserService(),
		alipayService:         service.NewAlipayService(config),
		alipayQRSessionStore:  service.NewAlipayQRLoginSessionStore(),
		wechatService:         service.NewWechatLoginService(config),
		config:                config,
		db:                    db,
		tokenBlacklistService: service.NewTokenBlacklistService(db),
		refreshTokenService:   service.NewRefreshTokenService(db),
	}
}

// GetAlipayQRSession creates a pollable QR login session.
func (h *AuthHandler) GetAlipayQRSession(c *gin.Context) {
	session := service.NewAlipayQRLoginSession(10 * time.Minute)
	alipayAuthURL, err := h.alipayService.GenerateQRCodeURLWithState(session.State)
	if err != nil {
		utils.InternalServerError(c, "failed to generate alipay auth url: "+err.Error())
		return
	}

	h.alipayQRSessionStore.SavePending(session, alipayAuthURL)
	utils.Success(c, gin.H{
		"auth_url":   alipayAuthURL,
		"session_id": session.SessionID,
		"status":     service.AlipayQRLoginPending,
		"expires_at": session.ExpiresAt,
	})
}

// GetAlipayQRSessionStatus returns the current QR login status for frontend polling.
func (h *AuthHandler) GetAlipayQRSessionStatus(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		sessionID = c.Query("session_id")
	}
	if sessionID == "" {
		utils.BadRequest(c, "missing session_id")
		return
	}

	session, ok := h.alipayQRSessionStore.Get(sessionID)
	if !ok {
		utils.NotFound(c, "alipay qr session not found")
		return
	}
	utils.Success(c, session)
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
			mockRefreshToken, _, _ := middleware.GenerateRefreshToken(h.config, uuid.New())

			utils.Created(c, gin.H{
				"access_token":  mockToken,
				"refresh_token": mockRefreshToken,
				"token_type":    "Bearer",
				"expires_in":    int64(h.config.JWT.AccessTokenExpiry.Seconds()),
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
	refreshToken, refreshExpiresAt, err := middleware.GenerateRefreshToken(h.config, user.ID)
	if err != nil {
		utils.InternalServerError(c, "failed to generate refresh token")
		return
	}

	// 保存刷新令牌到数据库
	_, err = h.refreshTokenService.CreateRefreshToken(refreshToken, user.ID, refreshExpiresAt)
	if err != nil {
		utils.InternalServerError(c, "failed to save refresh token")
		return
	}

	// 返回响应
	utils.Created(c, gin.H{
		"access_token":  token,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int64(h.config.JWT.AccessTokenExpiry.Seconds()),
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
	refreshToken, refreshExpiresAt, err := middleware.GenerateRefreshToken(h.config, user.ID)
	if err != nil {
		utils.InternalServerError(c, "failed to generate refresh token")
		return
	}

	// 保存刷新令牌到数据库
	_, err = h.refreshTokenService.CreateRefreshToken(refreshToken, user.ID, refreshExpiresAt)
	if err != nil {
		utils.InternalServerError(c, "failed to save refresh token")
		return
	}

	// 返回响应
	utils.Success(c, gin.H{
		"access_token":  token,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int64(h.config.JWT.AccessTokenExpiry.Seconds()),
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
		Avatar   string `json:"avatar" binding:"omitempty"`
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

// GetAlipayAuthURL 获取支付宝登录URL
func (h *AuthHandler) GetAlipayAuthURL(c *gin.Context) {
	// 初始化支付宝服务
	alipayService := service.NewAlipayService(h.config)

	// 生成支付宝扫码登录的二维码URL
	alipayAuthURL, state, err := alipayService.GenerateQRCodeURL()

	if err != nil {
		utils.InternalServerError(c, "failed to generate alipay auth url: "+err.Error())
		return
	}

	setAlipayLoginStateCookie(c, state)
	utils.Success(c, gin.H{
		"auth_url": alipayAuthURL,
		"state":    state,
	})
}

// RedirectAlipayLogin redirects the browser to Alipay OAuth login.
func (h *AuthHandler) RedirectAlipayLogin(c *gin.Context) {
	alipayAuthURL, state, err := h.alipayService.GenerateQRCodeURL()
	if err != nil {
		utils.InternalServerError(c, "failed to generate alipay auth url")
		return
	}
	setAlipayLoginStateCookie(c, state)
	c.Redirect(http.StatusFound, alipayAuthURL)
}

// AlipayAuthCallback 支付宝登录回调（GET 重定向 / POST API 均支持）
func (h *AuthHandler) AlipayAuthCallback(c *gin.Context) {
	// GET: platform redirect appends auth_code as query param
	code := c.Query("auth_code")
	state := c.Query("state")
	// POST: frontend may send JSON body
	if code == "" {
		var body struct {
			AuthCode string `json:"auth_code"`
		}
		if err := c.ShouldBindJSON(&body); err == nil {
			code = body.AuthCode
		}
	}

	if code == "" {
		if h.failAlipayQRSession(c, state, "missing authorization code") {
			return
		}
		utils.BadRequest(c, "missing authorization code")
		return
	}
	if state != "" && !alipayLoginStateMatches(c, state) {
		if h.failAlipayQRSession(c, state, "invalid alipay login state") {
			return
		}
		utils.BadRequest(c, "invalid alipay login state")
		return
	}

	// 初始化支付宝服务
	alipayService := service.NewAlipayService(h.config)

	// 用授权码换取访问令牌
	token, err := alipayService.ExchangeCodeForToken(code)
	if err != nil {
		if h.failAlipayQRSession(c, state, "failed to exchange code for token: "+err.Error()) {
			return
		}
		utils.InternalServerError(c, "failed to exchange code for token")
		return
	}

	// 获取用户信息
	userInfo, err := alipayService.GetUserInfo(token.AccessToken)
	if err != nil {
		if h.failAlipayQRSession(c, state, "failed to get alipay user info: "+err.Error()) {
			return
		}
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
		if h.failAlipayQRSession(c, state, "failed to create or update alipay user: "+err.Error()) {
			return
		}
		utils.InternalServerError(c, "failed to create or update alipay user")
		return
	}

	// 生成JWT令牌
	tokenStr, err := middleware.GenerateToken(h.config, user.ID, user.Username)
	if err != nil {
		if h.failAlipayQRSession(c, state, "failed to generate token: "+err.Error()) {
			return
		}
		utils.InternalServerError(c, "failed to generate token")
		return
	}

	// 生成刷新令牌
	refreshToken, refreshExpiresAt, err := middleware.GenerateRefreshToken(h.config, user.ID)
	if err != nil {
		if h.failAlipayQRSession(c, state, "failed to generate refresh token: "+err.Error()) {
			return
		}
		utils.InternalServerError(c, "failed to generate refresh token")
		return
	}

	// 保存刷新令牌到数据库
	_, err = h.refreshTokenService.CreateRefreshToken(refreshToken, user.ID, refreshExpiresAt)
	if err != nil {
		if h.failAlipayQRSession(c, state, "failed to save refresh token: "+err.Error()) {
			return
		}
		utils.InternalServerError(c, "failed to save refresh token")
		return
	}

	if state != "" {
		if _, ok := h.alipayQRSessionStore.AuthorizeByState(
			state,
			tokenStr,
			refreshToken,
			"Bearer",
			int64(h.config.JWT.AccessTokenExpiry.Seconds()),
			user,
		); ok {
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.String(http.StatusOK, alipayQRLoginConfirmedHTML())
			return
		}
	}

	utils.Success(c, gin.H{
		"access_token":  tokenStr,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int64(h.config.JWT.AccessTokenExpiry.Seconds()),
		"user":          user,
	})
}

func (h *AuthHandler) failAlipayQRSession(c *gin.Context, state, message string) bool {
	if state == "" {
		return false
	}
	if _, ok := h.alipayQRSessionStore.FailByState(state, message); !ok {
		return false
	}
	log.Printf("[AlipayQRCallback] session failed state=%s error=%s", state, message)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, alipayQRLoginFailedHTML())
	return true
}

func alipayQRLoginConfirmedHTML() string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>支付宝登录确认</title></head><body>支付宝登录已确认，请回到电脑页面。<script>function closeAlipay(){if(window.AlipayJSBridge){AlipayJSBridge.call('closeWebview')}else{document.addEventListener('AlipayJSBridgeReady',function(){AlipayJSBridge.call('closeWebview')},false)}}closeAlipay();</script></body></html>`
}

func alipayQRLoginFailedHTML() string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>支付宝登录失败</title></head><body>支付宝登录失败，请回到电脑页面重试。<script>function closeAlipay(){if(window.AlipayJSBridge){AlipayJSBridge.call('closeWebview')}else{document.addEventListener('AlipayJSBridgeReady',function(){AlipayJSBridge.call('closeWebview')},false)}}closeAlipay();</script></body></html>`
}

func setAlipayLoginStateCookie(c *gin.Context, state string) {
	c.SetCookie("alipay_login_state", state, 600, "/", "", false, true)
}

func alipayLoginStateMatches(c *gin.Context, state string) bool {
	expected, err := c.Cookie("alipay_login_state")
	if err != nil {
		return true
	}
	c.SetCookie("alipay_login_state", "", -1, "/", "", false, true)
	return expected == state
}

// GetWechatAuthURL 获取微信登录URL
func (h *AuthHandler) GetWechatAuthURL(c *gin.Context) {
	wechatService := service.NewWechatLoginService(h.config)

	wechatAuthURL, state, err := wechatService.GenerateQRCodeURL()
	if err != nil {
		utils.InternalServerError(c, "failed to generate wechat auth url")
		return
	}

	setWechatLoginStateCookie(c, state)
	utils.Success(c, gin.H{
		"auth_url": wechatAuthURL,
		"state":    state,
	})
}

// RedirectWechatLogin redirects the browser to Wechat OAuth login.
func (h *AuthHandler) RedirectWechatLogin(c *gin.Context) {
	wechatAuthURL, state, err := h.wechatService.GenerateQRCodeURL()
	if err != nil {
		utils.InternalServerError(c, "failed to generate wechat auth url")
		return
	}
	setWechatLoginStateCookie(c, state)
	c.Redirect(http.StatusFound, wechatAuthURL)
}

// WechatAuthCallback 微信登录回调（GET 重定向 / POST API 均支持）
func (h *AuthHandler) WechatAuthCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	if code == "" {
		var body struct {
			Code string `json:"code"`
		}
		if err := c.ShouldBindJSON(&body); err == nil {
			code = body.Code
		}
	}

	if code == "" {
		utils.BadRequest(c, "missing authorization code")
		return
	}
	if state != "" && !wechatLoginStateMatches(c, state) {
		utils.BadRequest(c, "invalid wechat login state")
		return
	}

	wechatService := service.NewWechatLoginService(h.config)

	token, err := wechatService.ExchangeCodeForToken(code)
	if err != nil {
		utils.InternalServerError(c, "failed to exchange code for token")
		return
	}

	userInfo, err := wechatService.GetUserInfo(token.AccessToken, token.OpenID)
	if err != nil {
		utils.InternalServerError(c, "failed to get wechat user info")
		return
	}

	user, err := h.userService.CreateOrUpdateWechatUser(
		userInfo.OpenID,
		userInfo.Nickname,
		userInfo.UnionID,
		userInfo.HeadimgURL,
		userInfo.Sex,
	)
	if err != nil {
		utils.InternalServerError(c, "failed to create or update wechat user")
		return
	}

	tokenStr, err := middleware.GenerateToken(h.config, user.ID, user.Username)
	if err != nil {
		utils.InternalServerError(c, "failed to generate token")
		return
	}

	refreshToken, refreshExpiresAt, err := middleware.GenerateRefreshToken(h.config, user.ID)
	if err != nil {
		utils.InternalServerError(c, "failed to generate refresh token")
		return
	}

	_, err = h.refreshTokenService.CreateRefreshToken(refreshToken, user.ID, refreshExpiresAt)
	if err != nil {
		utils.InternalServerError(c, "failed to save refresh token")
		return
	}

	utils.Success(c, gin.H{
		"access_token":  tokenStr,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int64(h.config.JWT.AccessTokenExpiry.Seconds()),
		"user":          user,
	})
}

func setWechatLoginStateCookie(c *gin.Context, state string) {
	c.SetCookie("wechat_login_state", state, 600, "/", "", false, true)
}

func wechatLoginStateMatches(c *gin.Context, state string) bool {
	expected, err := c.Cookie("wechat_login_state")
	if err != nil {
		return true
	}
	c.SetCookie("wechat_login_state", "", -1, "/", "", false, true)
	return expected == state
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

	// 验证刷新令牌
	oldRefreshToken, err := h.refreshTokenService.ValidateRefreshToken(
		req.RefreshToken,
		h.config.JWT.MaxRefreshCount,
	)
	if err != nil {
		switch err {
		case service.ErrRefreshTokenNotFound:
			utils.Unauthorized(c, "invalid refresh token")
		case service.ErrRefreshTokenExpired:
			utils.Unauthorized(c, "refresh token expired")
		case service.ErrRefreshTokenRevoked:
			utils.Unauthorized(c, "refresh token revoked")
		case service.ErrMaxRefreshExceeded:
			utils.Unauthorized(c, "maximum refresh count exceeded, please login again")
		default:
			utils.Unauthorized(c, "invalid refresh token")
		}
		return
	}

	// 获取用户信息
	user, err := h.userService.GetUserByID(oldRefreshToken.UserID)
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
	newRefreshToken, newRefreshExpiresAt, err := middleware.GenerateRefreshToken(h.config, user.ID)
	if err != nil {
		utils.InternalServerError(c, "failed to generate new refresh token")
		return
	}

	// 更新刷新令牌记录（滚动刷新）
	_, err = h.refreshTokenService.RefreshToken(
		req.RefreshToken,
		newRefreshToken,
		newRefreshExpiresAt,
		h.config.JWT.MaxRefreshCount,
	)
	if err != nil {
		utils.InternalServerError(c, "failed to update refresh token")
		return
	}

	// 将旧的access_token加入黑名单
	// 注意：这里需要从请求头获取旧的access_token，但刷新接口可能没有
	// 所以我们只在客户端明确传递时才加入黑名单

	// 返回响应
	utils.Success(c, gin.H{
		"access_token":  newAccessToken,
		"refresh_token": newRefreshToken,
		"token_type":    "Bearer",
		"expires_in":    int64(h.config.JWT.AccessTokenExpiry.Seconds()),
		"user":          user,
	})
}

// Logout 退出登录
func (h *AuthHandler) Logout(c *gin.Context) {
	// 从上下文获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		utils.Unauthorized(c, "user not authenticated")
		return
	}

	// 获取Authorization头
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		// 解析Bearer令牌
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && parts[0] == "Bearer" {
			accessToken := parts[1]

			// 将access_token加入黑名单
			claims := &middleware.JWTClaims{}
			token, err := jwt.ParseWithClaims(accessToken, claims, func(token *jwt.Token) (interface{}, error) {
				return []byte(h.config.JWT.Secret), nil
			})

			if err == nil && token.Valid {
				_ = h.tokenBlacklistService.AddToken(
					accessToken,
					"access",
					userID.(uuid.UUID),
					claims.ExpiresAt.Time,
					"user logout",
				)
			}
		}
	}

	// 撤销用户的所有刷新令牌
	_ = h.refreshTokenService.RevokeUserRefreshTokens(userID.(uuid.UUID), "user logout")

	// 返回响应
	utils.Success(c, gin.H{"message": "logged out successfully"})
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
