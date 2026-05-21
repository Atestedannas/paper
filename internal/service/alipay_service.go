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
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/paper-format-checker/backend/internal/config"
)

// AlipayAccessToken is the token returned by alipay.system.oauth.token.
type AlipayAccessToken struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
	TokenType    string `json:"token_type"`
	ReExpiresIn  int    `json:"re_expires_in"`
}

// AlipayUserInfo is the user profile returned by alipay.user.info.share.
type AlipayUserInfo struct {
	UserID      string `json:"user_id"`
	Avatar      string `json:"avatar"`
	Nickname    string `json:"nick_name"`
	Gender      string `json:"gender"`
	Province    string `json:"province"`
	City        string `json:"city"`
	CountryCode string `json:"country_code"`
}

// AlipayService handles Alipay OAuth login.
type AlipayService struct {
	config     *config.AlipayConfig
	httpClient *http.Client
}

// NewAlipayService 鍒涘缓鏀粯瀹濊璇佹湇鍔″疄渚?
func NewAlipayService(config *config.Config) *AlipayService {
	return &AlipayService{
		config:     &config.Alipay,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *AlipayService) GenerateLoginURL() (string, error) {
	params := url.Values{}
	params.Add("app_id", s.config.AppID)
	params.Add("redirect_uri", s.config.RedirectURL)
	params.Add("response_type", "code")
	params.Add("scope", s.config.Scope)
	params.Add("state", "alipay_login")
	params.Add("source", "alipay_wallet")

	return fmt.Sprintf("%s?%s", s.config.AuthorizeURL, params.Encode()), nil
}

// BuildAuthURL builds the Alipay authorization URL used as the QR content.
func (s *AlipayService) BuildAuthURL(redirectURL, state string) (string, error) {
	params := url.Values{}
	params.Add("app_id", s.config.AppID)
	params.Add("redirect_uri", redirectURL)
	params.Add("response_type", "code")
	params.Add("scope", s.config.Scope)
	params.Add("state", state)
	params.Add("source", "alipay_wallet")
	return fmt.Sprintf("%s?%s", s.config.AuthorizeURL, params.Encode()), nil
}

// generateSign signs request parameters according to Alipay RSA2 rules.
func (s *AlipayService) generateSign(params url.Values) (string, error) {
	var keys []string
	for k := range params {
		if k != "sign" && params.Get(k) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, params.Get(k)))
	}
	signStr := strings.Join(parts, "&")

	rsaKey, err := parseAlipayPrivateKey(s.config.AppPrivateKey)
	if err != nil {
		return "", err
	}

	h := sha256.New()
	h.Write([]byte(signStr))
	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return "", fmt.Errorf("alipay: signing failed: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func (s *AlipayService) buildCommonParams(method string) url.Values {
	params := url.Values{}
	params.Set("app_id", s.config.AppID)
	params.Set("method", method)
	params.Set("format", "JSON")
	params.Set("charset", "utf-8")
	params.Set("sign_type", "RSA2")
	params.Set("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	params.Set("version", "1.0")
	return params
}

// buildSignedParams builds signed Alipay request parameters.
func (s *AlipayService) buildSignedParams(method string, bizContent string) (url.Values, error) {
	params := s.buildCommonParams(method)
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

func parseAlipayPrivateKey(raw string) (*rsa.PrivateKey, error) {
	keyStr := strings.TrimSpace(raw)
	if keyStr == "" {
		return nil, fmt.Errorf("alipay: empty private key")
	}
	if (strings.Contains(keyStr, "/") || strings.Contains(keyStr, "\\")) && !strings.Contains(keyStr, "-----BEGIN") {
		if data, err := os.ReadFile(keyStr); err == nil {
			keyStr = strings.TrimSpace(string(data))
		}
	}
	if !strings.Contains(keyStr, "-----BEGIN") {
		p8 := "-----BEGIN PRIVATE KEY-----\n" + keyStr + "\n-----END PRIVATE KEY-----" // gitleaks:allow
		if blk, _ := pem.Decode([]byte(p8)); blk != nil {
			if k, err := x509.ParsePKCS8PrivateKey(blk.Bytes); err == nil {
				if rk, ok := k.(*rsa.PrivateKey); ok {
					return rk, nil
				}
			}
		}
		p1 := "-----BEGIN RSA PRIVATE KEY-----\n" + keyStr + "\n-----END RSA PRIVATE KEY-----"
		if blk, _ := pem.Decode([]byte(p1)); blk != nil {
			if k, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
				return k, nil
			}
		}
		return nil, fmt.Errorf("alipay: unable to parse private key")
	}
	blk, _ := pem.Decode([]byte(keyStr))
	if blk == nil {
		return nil, fmt.Errorf("alipay: failed to decode private key PEM")
	}
	if k, err := x509.ParsePKCS8PrivateKey(blk.Bytes); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
	}
	if k, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
		return k, nil
	}
	return nil, fmt.Errorf("alipay: unsupported private key format")
}

func (s *AlipayService) gatewayURL() string {
	if s.config.SandboxEnabled && s.config.SandboxGatewayURL != "" {
		return s.config.SandboxGatewayURL
	}
	return s.config.GatewayURL
}

// ExchangeCodeForToken exchanges an auth_code for an Alipay access token.
func (s *AlipayService) ExchangeCodeForToken(code string) (*AlipayAccessToken, error) {
	params := s.buildCommonParams("alipay.system.oauth.token")
	params.Set("grant_type", "authorization_code")
	params.Set("code", code)
	sign, err := s.generateSign(params)
	if err != nil {
		return nil, err
	}
	params.Set("sign", sign)

	req, err := http.NewRequest(http.MethodPost, s.gatewayURL(), bytes.NewBufferString(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create access token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request access token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read access token response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode access token response: %w", err)
	}

	if errorResponse, ok := result["error_response"].(map[string]interface{}); ok {
		log.Printf("[Alipay OAuth] token exchange failed: http=%d body=%s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("alipay api error: %v - %v", errorResponse["code"], errorResponse["msg"])
	}

	tokenResponse, ok := result["alipay_system_oauth_token_response"].(map[string]interface{})
	if !ok {
		log.Printf("[Alipay OAuth] token exchange unexpected response: http=%d body=%s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("alipay api error: unexpected token response shape")
	}
	if code := mapString(tokenResponse, "code"); code != "" && code != "10000" {
		log.Printf("[Alipay OAuth] token exchange business error: http=%d body=%s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("alipay api error: code=%s msg=%s sub_code=%s sub_msg=%s",
			code,
			mapString(tokenResponse, "msg"),
			mapString(tokenResponse, "sub_code"),
			mapString(tokenResponse, "sub_msg"),
		)
	}

	token := buildAlipayAccessToken(tokenResponse)
	if token.AccessToken == "" || token.UserID == "" {
		log.Printf("[Alipay OAuth] token exchange missing fields: http=%d body=%s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("alipay api error: token response missing access_token or user identity")
	}

	return token, nil
}
func (s *AlipayService) GetUserInfo(accessToken string) (*AlipayUserInfo, error) {
	params := s.buildCommonParams("alipay.user.info.share")
	params.Set("auth_token", accessToken)
	sign, err := s.generateSign(params)
	if err != nil {
		return nil, err
	}
	params.Set("sign", sign)

	req, err := http.NewRequest(http.MethodPost, s.gatewayURL(), bytes.NewBufferString(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create user info request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request user info: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read user info response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode user info response: %w", err)
	}

	if errorResponse, ok := result["error_response"].(map[string]interface{}); ok {
		log.Printf("[Alipay OAuth] user info failed: http=%d body=%s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("alipay api error: %v - %v", errorResponse["code"], errorResponse["msg"])
	}

	userInfoResponse, ok := result["alipay_user_info_share_response"].(map[string]interface{})
	if !ok {
		log.Printf("[Alipay OAuth] user info unexpected response: http=%d body=%s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("alipay api error: unexpected user info response shape")
	}
	if code := mapString(userInfoResponse, "code"); code != "" && code != "10000" {
		log.Printf("[Alipay OAuth] user info business error: http=%d body=%s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("alipay api error: code=%s msg=%s sub_code=%s sub_msg=%s",
			code,
			mapString(userInfoResponse, "msg"),
			mapString(userInfoResponse, "sub_code"),
			mapString(userInfoResponse, "sub_msg"),
		)
	}

	userInfo := &AlipayUserInfo{
		UserID:      mapFirstString(userInfoResponse, "user_id", "open_id", "union_id"),
		Avatar:      mapString(userInfoResponse, "avatar"),
		Nickname:    mapString(userInfoResponse, "nick_name"),
		Gender:      mapString(userInfoResponse, "gender"),
		Province:    mapString(userInfoResponse, "province"),
		City:        mapString(userInfoResponse, "city"),
		CountryCode: mapString(userInfoResponse, "country_code"),
	}
	if userInfo.UserID == "" {
		log.Printf("[Alipay OAuth] user info missing fields: http=%d body=%s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("alipay api error: user info response missing user identity")
	}

	return userInfo, nil
}
func (s *AlipayService) GenerateQRCodeURL() (string, string, error) {
	// 鐢熸垚闅忔満state鐢ㄤ簬闃睠SRF鏀诲嚮
	state := fmt.Sprintf("alipay_qr_%d", time.Now().UnixNano())

	params := url.Values{}
	params.Add("app_id", s.config.AppID)
	params.Add("redirect_uri", s.config.RedirectURL)
	params.Add("response_type", "code")
	params.Add("scope", s.config.Scope)
	params.Add("state", state)
	params.Add("source", "alipay_wallet")

	qrURL := fmt.Sprintf("%s?%s", s.config.AuthorizeURL, params.Encode())
	return qrURL, state, nil
}

func buildAlipayAccessToken(tokenResponse map[string]interface{}) *AlipayAccessToken {
	return &AlipayAccessToken{
		AccessToken:  mapString(tokenResponse, "access_token"),
		ExpiresIn:    mapInt(tokenResponse, "expires_in"),
		RefreshToken: mapString(tokenResponse, "refresh_token"),
		UserID:       mapFirstString(tokenResponse, "user_id", "open_id", "union_id"),
		TokenType:    mapString(tokenResponse, "token_type"),
		ReExpiresIn:  mapInt(tokenResponse, "re_expires_in"),
	}
}

func mapFirstString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := mapString(m, key); value != "" {
			return value
		}
	}
	return ""
}

func mapString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func mapInt(m map[string]interface{}, key string) int {
	raw := mapString(m, key)
	if raw == "" {
		return 0
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return int(f)
	}
	return 0
}
