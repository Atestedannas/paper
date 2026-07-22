package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paper-format-checker/backend/internal/config"
	"gorm.io/gorm"
)

func TestAuthHandler_Register(t *testing.T) {
	// 1. 准备依赖（最小化配置）
	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:             "test-secret",
			AccessTokenExpiry:  3600,
			RefreshTokenExpiry: 86400,
			MaxRefreshCount:    5,
		},
	}
	// 如果不需要真实数据库，可以传 nil 或 mock 对象
	var db *gorm.DB = nil // 实际需要替换为 mock 或测试数据库

	handler := NewAuthHandler(cfg, db)

	// 2. 构造 HTTP 请求
	reqBody := map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
		"password": "Abc123!@#",
	}
	jsonData, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/register", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")

	// 3. 模拟 Gin 上下文
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	// 4. 调用目标函数
	handler.Register(c)

	// 5. 断言结果
	if w.Code != http.StatusInternalServerError {
		t.Errorf("期望数据库不可用时返回 500，实际 %d", w.Code)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("access_token")) {
		t.Fatal("数据库不可用时不得签发 JWT")
	}
}

func TestOAuthStateValidationFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, matches := range map[string]func(*gin.Context, string) bool{
		"alipay": alipayLoginStateMatches,
		"wechat": wechatLoginStateMatches,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/callback?state=attacker", nil)
			if matches(c, "attacker") {
				t.Fatal("missing state cookie must not be accepted")
			}
		})
	}
}

func TestSendResetCodeDoesNotExposeCode(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/reset-code", bytes.NewBufferString(`{"email":"user@example.com"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	(&AuthHandler{}).SendResetCode(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("reset_code")) {
		t.Fatal("password reset code leaked in response")
	}
}

func TestGetAlipayAuthURL_Real(t *testing.T) {
	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:            "your-secret-key-change-this-in-production",
			AccessTokenExpiry: 86400,
		},
		Alipay: config.AlipayConfig{
			SandboxEnabled:  true, // 也许内部不用，但保留
			AppID:           "2088102181253034",
			AppPrivateKey:   `MIIEog...`, // 使用同一个私钥
			AlipayPublicKey: "",          // 沙箱公钥可选？
			GatewayURL:      "https://openapi.alipaydev.com/gateway.do",
			NotifyURL:       "http://mapi.fufentong.com/master/api/alipay/alipayNotifyUrl",
			ReturnURL:       "http://mapi.fufentong.com/master/api/alipay/alipayReturnUrl",
			RedirectURL:     "https://liyian.top/auth/callback/alipay",
			Scope:           "auth_base",
			AuthorizeURL:    "https://openauth.alipay.com/oauth2/publicAppAuthorize.htm",
		},
	}

	handler := NewAuthHandler(cfg, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/alipay/auth-url", nil)

	handler.GetAlipayAuthURL(c)

	// 先打印状态码和响应体，这是调试的关键
	t.Logf("Status Code: %d", w.Code)
	t.Logf("Response Body: %s", w.Body.String())

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，错误信息: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应JSON失败: %v", err)
	}

	// 根据你的 utils.Success 包装，成功时数据在 data 字段内
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("响应中没有 data 字段或格式错误: %v", resp)
	}

	authURL, ok := data["auth_url"]
	if !ok || authURL == "" {
		t.Fatalf("未返回 auth_url")
	}

	t.Logf("✅ 支付宝登录URL: %v", authURL)
}
