package service

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
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

type alipayAPIError struct {
	Code    string `json:"code"`
	Msg     string `json:"msg"`
	SubCode string `json:"sub_code"`
	SubMsg  string `json:"sub_msg"`
}

func (e alipayAPIError) Error() string {
	if e.SubCode != "" || e.SubMsg != "" {
		return fmt.Sprintf("alipay api error: code=%s msg=%s sub_code=%s sub_msg=%s", e.Code, e.Msg, e.SubCode, e.SubMsg)
	}
	return fmt.Sprintf("alipay api error: code=%s msg=%s", e.Code, e.Msg)
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
	return s.buildAuthorizeURL("alipay_login")
}

// generateSign 生成 Alipay RSA2 签名（SHA256WithRSA）
func (s *AlipayService) generateSign(params url.Values) (string, error) {
	// 1. 排序、拼接待签名字符串（只排除 sign）
	var keys []string
	for k := range params {
		if k != "sign" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, params.Get(k)))
	}
	signStr := strings.Join(parts, "&")

	// 2. 解析私钥（支持 PKCS1 和 PKCS8 格式）
	pkeyStr := strings.TrimSpace(s.config.AppPrivateKey)
	if !strings.Contains(pkeyStr, "-----BEGIN") {
		if strings.Contains(s.config.AppPrivateKey, "MIIEvwIBADANBgk") {
			pkeyStr = "-----BEGIN RSA PRIVATE KEY-----\n" + pkeyStr + "\n-----END RSA PRIVATE KEY-----"
		} else {
			pkeyStr = "-----BEGIN PRIVATE KEY-----\n" + pkeyStr + "\n-----END PRIVATE KEY-----"
		}
	}
	block, _ := pem.Decode([]byte(pkeyStr))
	if block == nil {
		return "", fmt.Errorf("alipay: failed to decode private key PEM")
	}

	keyIface, err := parseAlipayPrivateKey(block.Bytes)
	if err != nil {
		return "", err
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

func parseAlipayPrivateKey(der []byte) (interface{}, error) {
	if keyIface, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		return keyIface, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}

	pkcs8Err := "unknown"
	if _, err := x509.ParsePKCS8PrivateKey(der); err != nil {
		pkcs8Err = err.Error()
	}
	pkcs1Err := "unknown"
	if _, err := x509.ParsePKCS1PrivateKey(der); err != nil {
		pkcs1Err = err.Error()
	}
	return nil, fmt.Errorf("alipay: failed to parse private key as PKCS8 (%s) or PKCS1 (%s)", pkcs8Err, pkcs1Err)
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
	return s.ExchangeCodeForTokenWithRedirectURL(code, s.config.RedirectURL)
}

func (s *AlipayService) ExchangeCodeForTokenWithRedirectURL(code, redirectURL string) (*AlipayAccessToken, error) {
	params := s.buildAccessTokenParamsWithRedirectURL(code, redirectURL)

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read access token response: %w", err)
	}
	return decodeAlipayAccessTokenResponse(body)
}

func (s *AlipayService) buildAccessTokenParams(code string) url.Values {
	return s.buildAccessTokenParamsWithRedirectURL(code, s.config.RedirectURL)
}

func (s *AlipayService) buildAccessTokenParamsWithRedirectURL(code, redirectURL string) url.Values {
	params := url.Values{}
	params.Set("app_id", s.config.AppID)
	params.Set("method", "alipay.system.oauth.token")
	params.Set("format", "JSON")
	params.Set("charset", "utf-8")
	params.Set("sign_type", "RSA2")
	params.Set("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	params.Set("version", "1.0")
	params.Set("grant_type", "authorization_code")
	params.Set("code", code)
	params.Set("redirect_uri", redirectURL)
	params.Set("scope", s.effectiveScope())
	return params
}

func decodeAlipayAccessTokenResponse(body []byte) (*AlipayAccessToken, error) {
	var result struct {
		Response *struct {
			alipayAPIError
			AccessToken   string          `json:"access_token"`
			ExpiresIn     json.RawMessage `json:"expires_in"`
			RefreshToken  string          `json:"refresh_token"`
			UserID        string          `json:"user_id"`
			OpenID        string          `json:"open_id"`
			TokenType     string          `json:"token_type"`
			AuthTokenType string          `json:"auth_token_type"`
			ReExpiresIn   json.RawMessage `json:"re_expires_in"`
		} `json:"alipay_system_oauth_token_response"`
		Error *alipayAPIError `json:"error_response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode access token response: %w", err)
	}
	if result.Error != nil {
		return nil, *result.Error
	}
	if result.Response == nil {
		return nil, fmt.Errorf("missing alipay_system_oauth_token_response")
	}
	if result.Response.Code != "" && result.Response.Code != "10000" {
		return nil, result.Response.alipayAPIError
	}
	tokenType := result.Response.TokenType
	if tokenType == "" {
		tokenType = result.Response.AuthTokenType
	}
	expiresIn, err := alipayIntField(result.Response.ExpiresIn, "expires_in")
	if err != nil {
		return nil, err
	}
	reExpiresIn, err := alipayIntField(result.Response.ReExpiresIn, "re_expires_in")
	if err != nil {
		return nil, err
	}
	userID := result.Response.UserID
	if userID == "" {
		userID = result.Response.OpenID
	}
	if result.Response.AccessToken == "" || userID == "" {
		return nil, fmt.Errorf("alipay token response missing access_token or user_id/open_id: %s", sanitizeAlipayResponseBody(body))
	}
	return &AlipayAccessToken{
		AccessToken:  result.Response.AccessToken,
		ExpiresIn:    expiresIn,
		RefreshToken: result.Response.RefreshToken,
		UserID:       userID,
		TokenType:    tokenType,
		ReExpiresIn:  reExpiresIn,
	}, nil
}

func sanitizeAlipayResponseBody(body []byte) string {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return string(body)
	}
	redactAlipayValue(payload)
	var sanitized bytes.Buffer
	encoder := json.NewEncoder(&sanitized)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return string(body)
	}
	return strings.TrimSpace(sanitized.String())
}

func redactAlipayValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			switch key {
			case "access_token", "refresh_token", "auth_token":
				typed[key] = "<redacted>"
			default:
				redactAlipayValue(nested)
			}
		}
	case []any:
		for _, nested := range typed {
			redactAlipayValue(nested)
		}
	}
}

func alipayIntField(raw json.RawMessage, field string) (int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("invalid alipay %s: %w", field, err)
	}
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid alipay %s: %w", field, err)
	}
	return n, nil
}

// GetUserInfo 获取支付宝用户信息
func (s *AlipayService) GetUserInfo(accessToken string) (*AlipayUserInfo, error) {
	params := s.buildUserInfoParams(accessToken)

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read user info response: %w", err)
	}
	return decodeAlipayUserInfoResponse(body)
}

func (s *AlipayService) buildUserInfoParams(accessToken string) url.Values {
	params := url.Values{}
	params.Set("app_id", s.config.AppID)
	params.Set("method", "alipay.user.info.share")
	params.Set("format", "JSON")
	params.Set("charset", "utf-8")
	params.Set("sign_type", "RSA2")
	params.Set("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	params.Set("version", "1.0")
	params.Set("auth_token", accessToken)
	params.Set("scope", s.effectiveScope())
	return params
}

func decodeAlipayUserInfoResponse(body []byte) (*AlipayUserInfo, error) {
	var result struct {
		Response *struct {
			alipayAPIError
			UserID      string `json:"user_id"`
			OpenID      string `json:"open_id"`
			Avatar      string `json:"avatar"`
			Nickname    string `json:"nick_name"`
			Gender      string `json:"gender"`
			Province    string `json:"province"`
			City        string `json:"city"`
			CountryCode string `json:"country_code"`
		} `json:"alipay_user_info_share_response"`
		Error *alipayAPIError `json:"error_response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode user info response: %w", err)
	}
	if result.Error != nil {
		return nil, *result.Error
	}
	if result.Response == nil {
		return nil, fmt.Errorf("missing alipay_user_info_share_response")
	}
	if result.Response.Code != "" && result.Response.Code != "10000" {
		return nil, result.Response.alipayAPIError
	}
	userID := result.Response.UserID
	if userID == "" {
		userID = result.Response.OpenID
	}
	if userID == "" {
		return nil, fmt.Errorf("alipay user info response missing user_id/open_id")
	}
	return &AlipayUserInfo{
		UserID:      userID,
		Avatar:      result.Response.Avatar,
		Nickname:    result.Response.Nickname,
		Gender:      result.Response.Gender,
		Province:    result.Response.Province,
		City:        result.Response.City,
		CountryCode: result.Response.CountryCode,
	}, nil
}

// GenerateQRCodeURL 生成支付宝扫码登录的二维码URL
func (s *AlipayService) GenerateQRCodeURL() (string, string, error) {
	// 生成随机state用于防CSRF攻击
	state := fmt.Sprintf("alipay_qr_%d", time.Now().UnixNano())

	qrURL, err := s.buildAuthorizeURL(state)
	return qrURL, state, err
}

func (s *AlipayService) GenerateQRCodeURLWithState(state string) (string, error) {
	return s.buildAuthorizeURL(state)
}

func (s *AlipayService) GenerateQRSessionURLWithState(state string) (string, error) {
	return s.buildAuthorizeURLWithRedirect(state, s.effectiveQRRedirectURL())
}

func (s *AlipayService) QRRedirectURL() string {
	return s.effectiveQRRedirectURL()
}

func (s *AlipayService) buildAuthorizeURL(state string) (string, error) {
	return s.buildAuthorizeURLWithRedirect(state, s.config.RedirectURL)
}

func (s *AlipayService) buildAuthorizeURLWithRedirect(state, redirect string) (string, error) {
	if strings.TrimSpace(s.config.AppID) == "" {
		return "", fmt.Errorf("alipay login is not configured: missing app id")
	}
	if strings.TrimSpace(redirect) == "" {
		return "", fmt.Errorf("alipay login is not configured: missing redirect url")
	}
	redirectURL, err := url.Parse(strings.TrimSpace(redirect))
	if err != nil || redirectURL.Scheme != "https" || redirectURL.Host == "" {
		return "", fmt.Errorf("alipay login redirect url must be a public https url")
	}
	if strings.TrimSpace(s.config.AuthorizeURL) == "" {
		return "", fmt.Errorf("alipay login is not configured: missing authorize url")
	}

	params := url.Values{}
	params.Add("app_id", s.config.AppID)
	params.Add("redirect_uri", redirect)
	params.Add("response_type", "code")
	params.Add("scope", s.effectiveScope())
	params.Add("state", state)

	return fmt.Sprintf("%s?%s", s.config.AuthorizeURL, params.Encode()), nil
}

func (s *AlipayService) effectiveQRRedirectURL() string {
	redirect := strings.TrimSpace(s.config.QRRedirectURL)
	if redirect != "" {
		return redirect
	}
	return s.config.RedirectURL
}

func (s *AlipayService) effectiveScope() string {
	scope := strings.TrimSpace(s.config.Scope)
	if scope == "" {
		return "auth_user"
	}
	return scope
}
