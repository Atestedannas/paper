package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/config"
)

func TestAlipayQRCodeURLUsesAuthUserScope(t *testing.T) {
	cfg := testAlipayConfig()

	loginURL, state, err := NewAlipayService(cfg).GenerateQRCodeURL()
	if err != nil {
		t.Fatalf("GenerateQRCodeURL returned error: %v", err)
	}
	if !strings.HasPrefix(state, "alipay_qr_") {
		t.Fatalf("expected generated alipay state, got %q", state)
	}

	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("generated invalid url: %v", err)
	}
	query := parsed.Query()
	if got := query.Get("scope"); got != "auth_user" {
		t.Fatalf("alipay login scope should be auth_user, got %q", got)
	}
	if got := query.Get("redirect_uri"); got != cfg.Alipay.RedirectURL {
		t.Fatalf("unexpected redirect_uri: %q", got)
	}
	if got := query.Get("state"); got != state {
		t.Fatalf("state in url should match returned state, got %q want %q", got, state)
	}
}

func TestAlipayQRCodeURLRequiresRedirectURL(t *testing.T) {
	cfg := testAlipayConfig()
	cfg.Alipay.RedirectURL = ""

	_, _, err := NewAlipayService(cfg).GenerateQRCodeURL()
	if err == nil {
		t.Fatal("expected missing redirect url to be rejected")
	}
}

func TestAlipayQRCodeURLRequiresHTTPSRedirectURL(t *testing.T) {
	cfg := testAlipayConfig()
	cfg.Alipay.RedirectURL = "http://example.com/api/v1/auth/alipay/callback"

	_, _, err := NewAlipayService(cfg).GenerateQRCodeURL()
	if err == nil {
		t.Fatal("expected non-https redirect url to be rejected")
	}
}

func TestAlipayGenerateSignAcceptsRawPKCS1PrivateKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	rawPKCS1 := base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key))
	cfg := testAlipayConfig()
	cfg.Alipay.AppPrivateKey = rawPKCS1

	params := url.Values{}
	params.Set("app_id", cfg.Alipay.AppID)
	params.Set("method", "alipay.system.oauth.token")
	params.Set("charset", "utf-8")
	params.Set("sign_type", "RSA2")
	params.Set("timestamp", "2026-05-23 14:30:00")
	params.Set("version", "1.0")
	params.Set("grant_type", "authorization_code")
	params.Set("code", "test-auth-code")

	sign, err := NewAlipayService(cfg).generateSign(params)
	if err != nil {
		t.Fatalf("generateSign should accept raw PKCS1 private key: %v", err)
	}
	if sign == "" {
		t.Fatal("generateSign returned empty signature")
	}
}

func TestDecodeAlipayAccessTokenResponseHandlesStringDurations(t *testing.T) {
	body := []byte(`{
		"alipay_system_oauth_token_response": {
			"code": "10000",
			"msg": "Success",
			"user_id": "2088102150477652",
			"access_token": "access-token",
			"expires_in": "3600",
			"refresh_token": "refresh-token",
			"re_expires_in": "86400",
			"auth_token_type": "permanent"
		}
	}`)

	token, err := decodeAlipayAccessTokenResponse(body)
	if err != nil {
		t.Fatalf("decodeAlipayAccessTokenResponse returned error: %v", err)
	}
	if token.ExpiresIn != 3600 || token.ReExpiresIn != 86400 {
		t.Fatalf("unexpected durations: expires=%d refresh=%d", token.ExpiresIn, token.ReExpiresIn)
	}
	if token.TokenType != "permanent" {
		t.Fatalf("unexpected token type: %q", token.TokenType)
	}
}

func TestDecodeAlipayAccessTokenResponseReturnsBusinessError(t *testing.T) {
	body := []byte(`{
		"alipay_system_oauth_token_response": {
			"code": "40002",
			"msg": "Invalid Arguments",
			"sub_code": "isv.code-invalid",
			"sub_msg": "auth_code is invalid"
		}
	}`)

	_, err := decodeAlipayAccessTokenResponse(body)
	if err == nil {
		t.Fatal("expected business error")
	}
	if !strings.Contains(err.Error(), "isv.code-invalid") {
		t.Fatalf("expected sub_code in error, got %v", err)
	}
}

func TestDecodeAlipayUserInfoResponseAllowsMissingOptionalFields(t *testing.T) {
	body := []byte(`{
		"alipay_user_info_share_response": {
			"code": "10000",
			"msg": "Success",
			"user_id": "2088102175794899",
			"nick_name": "支付宝用户"
		}
	}`)

	userInfo, err := decodeAlipayUserInfoResponse(body)
	if err != nil {
		t.Fatalf("decodeAlipayUserInfoResponse returned error: %v", err)
	}
	if userInfo.UserID != "2088102175794899" || userInfo.Nickname != "支付宝用户" {
		t.Fatalf("unexpected user info: %+v", userInfo)
	}
}

func TestDecodeAlipayUserInfoResponseReturnsBusinessError(t *testing.T) {
	body := []byte(`{
		"alipay_user_info_share_response": {
			"code": "40004",
			"msg": "Business Failed",
			"sub_code": "isv.invalid-auth-token",
			"sub_msg": "invalid auth token"
		}
	}`)

	_, err := decodeAlipayUserInfoResponse(body)
	if err == nil {
		t.Fatal("expected business error")
	}
	if !strings.Contains(err.Error(), "isv.invalid-auth-token") {
		t.Fatalf("expected sub_code in error, got %v", err)
	}
}

func TestBuildAccessTokenParamsIncludesOAuthContext(t *testing.T) {
	cfg := testAlipayConfig()
	params := NewAlipayService(cfg).buildAccessTokenParams("auth-code")

	if got := params.Get("format"); got != "JSON" {
		t.Fatalf("format = %q, want JSON", got)
	}
	if got := params.Get("redirect_uri"); got != cfg.Alipay.RedirectURL {
		t.Fatalf("redirect_uri = %q, want %q", got, cfg.Alipay.RedirectURL)
	}
	if got := params.Get("scope"); got != "auth_user" {
		t.Fatalf("scope = %q, want auth_user", got)
	}
	if got := params.Get("code"); got != "auth-code" {
		t.Fatalf("code = %q, want auth-code", got)
	}
}

func TestBuildUserInfoParamsIncludesJSONFormatAndScope(t *testing.T) {
	params := NewAlipayService(testAlipayConfig()).buildUserInfoParams("access-token")

	if got := params.Get("format"); got != "JSON" {
		t.Fatalf("format = %q, want JSON", got)
	}
	if got := params.Get("scope"); got != "auth_user" {
		t.Fatalf("scope = %q, want auth_user", got)
	}
	if got := params.Get("auth_token"); got != "access-token" {
		t.Fatalf("auth_token = %q, want access-token", got)
	}
}

func testAlipayConfig() *config.Config {
	return &config.Config{
		Alipay: config.AlipayConfig{
			AppID:        "2021000000000000",
			RedirectURL:  "https://example.com/api/v1/auth/alipay/callback",
			Scope:        "auth_user",
			AuthorizeURL: "https://openauth.alipay.com/oauth2/publicAppAuthorize.htm",
		},
	}
}
