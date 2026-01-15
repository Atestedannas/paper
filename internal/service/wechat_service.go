package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/paper-format-checker/backend/internal/config"
)

// WechatAccessToken 微信访问令牌
type WechatAccessToken struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	OpenID       string `json:"openid"`
	Scope        string `json:"scope"`
	UnionID      string `json:"unionid,omitempty"`
}

// WechatUserInfo 微信用户信息
type WechatUserInfo struct {
	OpenID     string   `json:"openid"`
	Nickname   string   `json:"nickname"`
	Sex        int      `json:"sex"`
	Province   string   `json:"province"`
	City       string   `json:"city"`
	Country    string   `json:"country"`
	HeadImgURL string   `json:"headimgurl"`
	UnionID    string   `json:"unionid,omitempty"`
	Privilege  []string `json:"privilege"`
}

// WechatService 微信认证服务
type WechatService struct {
	config     *config.WechatConfig
	httpClient *http.Client
}

// NewWechatService 创建微信认证服务实例
func NewWechatService(config *config.Config) *WechatService {
	return &WechatService{
		config:     &config.Wechat,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// GenerateLoginURL 生成微信登录URL
func (s *WechatService) GenerateLoginURL() (string, error) {
	params := url.Values{}
	params.Add("appid", s.config.AppID)
	params.Add("redirect_uri", s.config.RedirectURL)
	params.Add("response_type", "code")
	params.Add("scope", s.config.Scope)
	params.Add("state", "wechat_login")

	return fmt.Sprintf("%s?%s", s.config.AuthorizeURL, params.Encode()), nil
}

// ExchangeCodeForToken 用授权码换取访问令牌
func (s *WechatService) ExchangeCodeForToken(code string) (*WechatAccessToken, error) {
	params := url.Values{}
	params.Add("appid", s.config.AppID)
	params.Add("secret", s.config.AppSecret)
	params.Add("code", code)
	params.Add("grant_type", "authorization_code")

	url := fmt.Sprintf("%s?%s", s.config.AccessTokenURL, params.Encode())

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
	if errCode, ok := result["errcode"].(float64); ok && errCode != 0 {
		return nil, fmt.Errorf("wechat api error: %v - %v", errCode, result["errmsg"])
	}

	// 转换为结构体
	token := &WechatAccessToken{
		AccessToken:  result["access_token"].(string),
		ExpiresIn:    int(result["expires_in"].(float64)),
		RefreshToken: result["refresh_token"].(string),
		OpenID:       result["openid"].(string),
		Scope:        result["scope"].(string),
	}

	// 处理UnionID
	if unionID, ok := result["unionid"].(string); ok {
		token.UnionID = unionID
	}

	return token, nil
}

// GetUserInfo 获取微信用户信息
func (s *WechatService) GetUserInfo(accessToken, openID string) (*WechatUserInfo, error) {
	params := url.Values{}
	params.Add("access_token", accessToken)
	params.Add("openid", openID)
	params.Add("lang", "zh_CN")

	url := fmt.Sprintf("%s?%s", s.config.UserInfoURL, params.Encode())

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
	if errCode, ok := result["errcode"].(float64); ok && errCode != 0 {
		return nil, fmt.Errorf("wechat api error: %v - %v", errCode, result["errmsg"])
	}

	// 转换为结构体
	userInfo := &WechatUserInfo{
		OpenID:     result["openid"].(string),
		Nickname:   result["nickname"].(string),
		Sex:        int(result["sex"].(float64)),
		Province:   result["province"].(string),
		City:       result["city"].(string),
		Country:    result["country"].(string),
		HeadImgURL: result["headimgurl"].(string),
		Privilege:  []string{},
	}

	// 处理UnionID
	if unionID, ok := result["unionid"].(string); ok {
		userInfo.UnionID = unionID
	}

	// 处理Privilege
	if privilege, ok := result["privilege"].([]interface{}); ok {
		for _, p := range privilege {
			userInfo.Privilege = append(userInfo.Privilege, p.(string))
		}
	}

	return userInfo, nil
}

// GenerateQRCodeURL 生成微信扫码登录的二维码URL
func (s *WechatService) GenerateQRCodeURL() (string, string, error) {
	// 生成随机state用于防CSRF攻击
	state := fmt.Sprintf("wechat_qr_%d", time.Now().UnixNano())

	params := url.Values{}
	params.Add("appid", s.config.AppID)
	params.Add("redirect_uri", s.config.RedirectURL)
	params.Add("response_type", "code")
	params.Add("scope", s.config.Scope)
	params.Add("state", state)

	qrURL := fmt.Sprintf("%s?%s", s.config.AuthorizeURL, params.Encode())
	return qrURL, state, nil
}
