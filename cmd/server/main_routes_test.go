package main

import (
	"os"
	"strings"
	"testing"
)

func TestMainDoesNotRegisterDuplicateAlipayLoginURLRoutes(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	count := strings.Count(string(source), `"/alipay/login-url"`)
	if count != 2 {
		t.Fatalf("alipay login-url route registrations = %d, want 2 (/api/auth and /api/v1/auth)", count)
	}
}

func TestMainPreservesOAuthRoutesForBaseAndV1Auth(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)

	for _, route := range []string{`"/wechat/login-url"`, `"/alipay/qr-session"`} {
		if count := strings.Count(text, route); count != 2 {
			t.Fatalf("%s registrations = %d, want 2 (/api/auth and /api/v1/auth)", route, count)
		}
	}

	if count := strings.Count(text, `"/wechat/callback"`); count != 4 {
		t.Fatalf("wechat callback registrations = %d, want 4 (GET/POST for /api/auth and /api/v1/auth)", count)
	}
}
