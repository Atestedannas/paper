package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/paper-format-checker/backend/internal/config"
)

type WechatAccessToken struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	OpenID       string `json:"openid"`
	Scope        string `json:"scope"`
	UnionID      string `json:"unionid"`
}

type WechatUserInfo struct {
	OpenID     string `json:"openid"`
	Nickname   string `json:"nickname"`
	Sex        int    `json:"sex"`
	Province   string `json:"province"`
	City       string `json:"city"`
	Country    string `json:"country"`
	HeadimgURL string `json:"headimgurl"`
	UnionID    string `json:"unionid"`
}

type WechatLoginService struct {
	config     *config.WechatConfig
	httpClient *http.Client
}

func NewWechatLoginService(config *config.Config) *WechatLoginService {
	return &WechatLoginService{
		config:     &config.Wechat,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *WechatLoginService) GenerateLoginURL(state string) (string, error) {
	if s.config.AppID == "" {
		return "", fmt.Errorf("wechat login is not configured: missing app id")
	}

	scope := "snsapi_login"

	params := url.Values{}
	params.Add("appid", s.config.AppID)
	params.Add("redirect_uri", s.config.RedirectURL)
	params.Add("response_type", "code")
	params.Add("scope", scope)
	if state != "" {
		params.Add("state", state)
	}

	return fmt.Sprintf("https://open.weixin.qq.com/connect/qrconnect?%s#wechat_redirect", params.Encode()), nil
}

func (s *WechatLoginService) GenerateQRCodeURL() (string, string, error) {
	state := fmt.Sprintf("wechat_qr_%d", time.Now().UnixNano())
	qrURL, err := s.GenerateLoginURL(state)
	return qrURL, state, err
}

func (s *WechatLoginService) ExchangeCodeForToken(code string) (*WechatAccessToken, error) {
	if s.config.AppID == "" || s.config.ApiKey == "" {
		return nil, fmt.Errorf("wechat login config is incomplete")
	}

	params := url.Values{}
	params.Set("appid", s.config.AppID)
	params.Set("secret", s.config.ApiKey)
	params.Set("code", code)
	params.Set("grant_type", "authorization_code")

	urlStr := fmt.Sprintf("https://api.weixin.qq.com/sns/oauth2/access_token?%s", params.Encode())

	resp, err := s.httpClient.Get(urlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to request access token: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode access token response: %w", err)
	}

	if errcode, ok := result["errcode"]; ok && errcode.(float64) != 0 {
		return nil, fmt.Errorf("wechat api error: %v - %v", errcode, result["errmsg"])
	}

	token := &WechatAccessToken{
		AccessToken:  result["access_token"].(string),
		ExpiresIn:    int(result["expires_in"].(float64)),
		RefreshToken: result["refresh_token"].(string),
		OpenID:       result["openid"].(string),
		Scope:        result["scope"].(string),
	}

	if unionid, ok := result["unionid"]; ok {
		token.UnionID = unionid.(string)
	}

	return token, nil
}

func (s *WechatLoginService) GetUserInfo(accessToken, openID string) (*WechatUserInfo, error) {
	params := url.Values{}
	params.Set("access_token", accessToken)
	params.Set("openid", openID)
	params.Set("lang", "zh_CN")

	urlStr := fmt.Sprintf("https://api.weixin.qq.com/sns/userinfo?%s", params.Encode())

	resp, err := s.httpClient.Get(urlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to request user info: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode user info response: %w", err)
	}

	if errcode, ok := result["errcode"]; ok && errcode.(float64) != 0 {
		return nil, fmt.Errorf("wechat api error: %v - %v", errcode, result["errmsg"])
	}

	userInfo := &WechatUserInfo{
		OpenID: result["openid"].(string),
		Nickname: func() string {
			if n, ok := result["nickname"]; ok {
				return n.(string)
			}
			return ""
		}(),
		Sex: func() int {
			if s, ok := result["sex"]; ok {
				return int(s.(float64))
			}
			return 0
		}(),
		Province: func() string {
			if p, ok := result["province"]; ok {
				return p.(string)
			}
			return ""
		}(),
		City: func() string {
			if c, ok := result["city"]; ok {
				return c.(string)
			}
			return ""
		}(),
		Country: func() string {
			if c, ok := result["country"]; ok {
				return c.(string)
			}
			return ""
		}(),
		HeadimgURL: func() string {
			if h, ok := result["headimgurl"]; ok {
				return h.(string)
			}
			return ""
		}(),
	}

	if unionid, ok := result["unionid"]; ok {
		userInfo.UnionID = unionid.(string)
	}

	return userInfo, nil
}

func (s *WechatLoginService) RefreshAccessToken(refreshToken string) (*WechatAccessToken, error) {
	if s.config.AppID == "" || s.config.ApiKey == "" {
		return nil, fmt.Errorf("wechat login config is incomplete")
	}

	params := url.Values{}
	params.Set("appid", s.config.AppID)
	params.Set("secret", s.config.ApiKey)
	params.Set("refresh_token", refreshToken)
	params.Set("grant_type", "refresh_token")

	urlStr := fmt.Sprintf("https://api.weixin.qq.com/sns/oauth2/refresh_token?%s", params.Encode())

	resp, err := s.httpClient.Get(urlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh access token: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode refresh token response: %w", err)
	}

	if errcode, ok := result["errcode"]; ok && errcode.(float64) != 0 {
		return nil, fmt.Errorf("wechat api error: %v - %v", errcode, result["errmsg"])
	}

	token := &WechatAccessToken{
		AccessToken:  result["access_token"].(string),
		ExpiresIn:    int(result["expires_in"].(float64)),
		RefreshToken: result["refresh_token"].(string),
		OpenID:       result["openid"].(string),
		Scope:        result["scope"].(string),
	}

	if unionid, ok := result["unionid"]; ok {
		token.UnionID = unionid.(string)
	}

	return token, nil
}