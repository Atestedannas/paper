package service

import (
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

func testAlipayConfig() *config.Config {
	return &config.Config{
		Alipay: config.AlipayConfig{
			AppID:        "2021000000000000",
			RedirectURL:  "https://example.com/api/v1/auth/alipay/callback",
			Scope:        "auth_user",
			AuthorizeURL: "https://open.auth.alipay.com/oauth2/publicAppAuthorize.htm",
		},
	}
}
