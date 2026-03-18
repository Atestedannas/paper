package service

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
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

// generateSign 生成 Alipay RSA2 签名（SHA256WithRSA）
func (s *AlipayService) generateSign(params url.Values) (string, error) {
	// 1. 排序、拼接待签名字符串（排除 sign / sign_type）
	var keys []string
	for k := range params {
		if k != "sign" && k != "sign_type" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, params.Get(k)))
	}
	signStr := strings.Join(parts, "&")

	// 2. 解析 PKCS8 私钥（支持带 PEM 头或纯 base64）
	pkeyStr := strings.TrimSpace(s.config.AppPrivateKey)
	if !strings.Contains(pkeyStr, "-----BEGIN") {
		pkeyStr = "-----BEGIN PRIVATE KEY-----\n" + pkeyStr + "\n-----END PRIVATE KEY-----"
	}
	block, _ := pem.Decode([]byte(pkeyStr))
	if block == nil {
		return "", fmt.Errorf("alipay: failed to decode private key PEM")
	}
	keyIface, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("alipay: failed to parse private key: %w", err)
	}
	rsaKey, ok := keyIface.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("alipay: private key is not RSA")
	}

	// 3. SHA256WithRSA 签名
	h := sha256.New()
	h.Write([]byte(signStr))
	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return "", fmt.Errorf("alipay: signing failed: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// buildSignedParams 构造带签名的参数
func (s *AlipayService) buildSignedParams(method string, bizContent string) (url.Values, error) {
	params := url.Values{}
	params.Set("app_id", s.config.AppID)
	params.Set("method", method)
	params.Set("charset", "utf-8")
	params.Set("sign_type", "RSA2")
	params.Set("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	params.Set("version", "1.0")
	if bizContent != "" {
		params.Set("biz_content", bizContent)
	}

	if s.config.AppPrivateKey == "" {
		return params, nil
	}
	sign, err := s.generateSign(params)
	if err != nil {
		return nil, err
	}
	params.Set("sign", sign)
	return params, nil
}

// gatewayURL 返回生产或沙箱网关
func (s *AlipayService) gatewayURL() string {
	if s.config.SandboxEnabled && s.config.SandboxGatewayURL != "" {
		return s.config.SandboxGatewayURL
	}
	return s.config.GatewayURL
}

// ExchangeCodeForToken 用授权码换取访问令牌
func (s *AlipayService) ExchangeCodeForToken(code string) (*AlipayAccessToken, error) {
	params := url.Values{}
	params.Set("app_id", s.config.AppID)
	params.Set("method", "alipay.system.oauth.token")
	params.Set("charset", "utf-8")
	params.Set("sign_type", "RSA2")
	params.Set("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	params.Set("version", "1.0")
	params.Set("grant_type", "authorization_code")
	params.Set("code", code)

	if s.config.AppPrivateKey != "" {
		sign, err := s.generateSign(params)
		if err != nil {
			return nil, err
		}
		params.Set("sign", sign)
	}

	url := fmt.Sprintf("%s?%s", s.gatewayURL(), params.Encode())

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
	params.Set("app_id", s.config.AppID)
	params.Set("method", "alipay.user.info.share")
	params.Set("charset", "utf-8")
	params.Set("sign_type", "RSA2")
	params.Set("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	params.Set("version", "1.0")
	params.Set("auth_token", accessToken)

	if s.config.AppPrivateKey != "" {
		sign, err := s.generateSign(params)
		if err != nil {
			return nil, err
		}
		params.Set("sign", sign)
	}

	url := fmt.Sprintf("%s?%s", s.gatewayURL(), params.Encode())

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
