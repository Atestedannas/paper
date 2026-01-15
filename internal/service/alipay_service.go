package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/paper-format-checker/backend/internal/config"
)

// AlipayAccessToken 支付宝访问令牌
type AlipayAccessToken struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
	TokenType    string `json:"token_type"`
	ReExpiresIn  int    `json:"re_expires_in"`
}

// AlipayUserInfo 支付宝用户信息
type AlipayUserInfo struct {
	UserID      string `json:"user_id"`
	Avatar      string `json:"avatar"`
	Nickname    string `json:"nick_name"`
	Gender      string `json:"gender"`
	Province    string `json:"province"`
	City        string `json:"city"`
	CountryCode string `json:"country_code"`
}

// AlipayService 支付宝认证服务
type AlipayService struct {
	config     *config.AlipayConfig
	httpClient *http.Client
}

// NewAlipayService 创建支付宝认证服务实例
func NewAlipayService(config *config.Config) *AlipayService {
	return &AlipayService{
		config:     &config.Alipay,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// GenerateLoginURL 生成支付宝登录URL
func (s *AlipayService) GenerateLoginURL() (string, error) {
	params := url.Values{}
	params.Add("app_id", s.config.AppID)
	params.Add("redirect_uri", s.config.RedirectURL)
	params.Add("response_type", "code")
	params.Add("scope", s.config.Scope)
	params.Add("state", "alipay_login")

	return fmt.Sprintf("%s?%s", s.config.AuthorizeURL, params.Encode()), nil
}

// ExchangeCodeForToken 用授权码换取访问令牌
func (s *AlipayService) ExchangeCodeForToken(code string) (*AlipayAccessToken, error) {
	params := url.Values{}
	params.Add("app_id", s.config.AppID)
	params.Add("method", "alipay.system.oauth.token")
	params.Add("charset", "utf-8")
	params.Add("sign_type", "RSA2")
	params.Add("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	params.Add("version", "1.0")
	params.Add("grant_type", "authorization_code")
	params.Add("code", code)

	// 实际应用中需要对请求进行签名，这里简化处理
	// sign := s.generateSign(params)
	// params.Add("sign", sign)

	url := fmt.Sprintf("%s?%s", s.config.GatewayURL, params.Encode())

	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to request access token: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode access token response: %w", err)
	}

	// 检查是否返回错误
	if errorResponse, ok := result["error_response"].(map[string]interface{}); ok {
		return nil, fmt.Errorf("alipay api error: %v - %v", errorResponse["code"], errorResponse["msg"])
	}

	// 获取访问令牌
	tokenResponse := result["alipay_system_oauth_token_response"].(map[string]interface{})
	token := &AlipayAccessToken{
		AccessToken:  tokenResponse["access_token"].(string),
		ExpiresIn:    int(tokenResponse["expires_in"].(float64)),
		RefreshToken: tokenResponse["refresh_token"].(string),
		UserID:       tokenResponse["user_id"].(string),
		TokenType:    tokenResponse["token_type"].(string),
		ReExpiresIn:  int(tokenResponse["re_expires_in"].(float64)),
	}

	return token, nil
}

// GetUserInfo 获取支付宝用户信息
func (s *AlipayService) GetUserInfo(accessToken string) (*AlipayUserInfo, error) {
	params := url.Values{}
	params.Add("app_id", s.config.AppID)
	params.Add("method", "alipay.user.info.share")
	params.Add("charset", "utf-8")
	params.Add("sign_type", "RSA2")
	params.Add("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	params.Add("version", "1.0")
	params.Add("auth_token", accessToken)

	// 实际应用中需要对请求进行签名，这里简化处理
	// sign := s.generateSign(params)
	// params.Add("sign", sign)

	url := fmt.Sprintf("%s?%s", s.config.GatewayURL, params.Encode())

	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to request user info: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode user info response: %w", err)
	}

	// 检查是否返回错误
	if errorResponse, ok := result["error_response"].(map[string]interface{}); ok {
		return nil, fmt.Errorf("alipay api error: %v - %v", errorResponse["code"], errorResponse["msg"])
	}

	// 获取用户信息
	userInfoResponse := result["alipay_user_info_share_response"].(map[string]interface{})
	userInfo := &AlipayUserInfo{
		UserID:      userInfoResponse["user_id"].(string),
		Avatar:      userInfoResponse["avatar"].(string),
		Nickname:    userInfoResponse["nick_name"].(string),
		Gender:      userInfoResponse["gender"].(string),
		Province:    userInfoResponse["province"].(string),
		City:        userInfoResponse["city"].(string),
		CountryCode: userInfoResponse["country_code"].(string),
	}

	return userInfo, nil
}

// GenerateQRCodeURL 生成支付宝扫码登录的二维码URL
func (s *AlipayService) GenerateQRCodeURL() (string, string, error) {
	// 生成随机state用于防CSRF攻击
	state := fmt.Sprintf("alipay_qr_%d", time.Now().UnixNano())

	params := url.Values{}
	params.Add("app_id", s.config.AppID)
	params.Add("redirect_uri", s.config.RedirectURL)
	params.Add("response_type", "code")
	params.Add("scope", s.config.Scope)
	params.Add("state", state)

	qrURL := fmt.Sprintf("%s?%s", s.config.AuthorizeURL, params.Encode())
	return qrURL, state, nil
}
