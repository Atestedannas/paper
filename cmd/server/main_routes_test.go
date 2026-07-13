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

	for _, route := range []string{`"/wechat/login-url"`, `"/alipay/qr-session"`, `"/alipay/qr-session/:session_id/status"`} {
		if count := strings.Count(text, route); count != 2 {
			t.Fatalf("%s registrations = %d, want 2 (/api/auth and /api/v1/auth)", route, count)
		}
	}

	if count := strings.Count(text, `"/wechat/callback"`); count != 4 {
		t.Fatalf("wechat callback registrations = %d, want 4 (GET/POST for /api/auth and /api/v1/auth)", count)
	}
}

func TestMainDoesNotKillExistingProcessOnPortConflict(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)

	if strings.Contains(text, "killProcessUsingPort(cfg.Server.Port)") {
		t.Fatal("server startup must not kill the process already listening on the configured port")
	}
	if !strings.Contains(text, "log.Fatalf(\"Port %d is already in use\"") {
		t.Fatal("server startup should fail fast when the configured port is already in use")
	}
}

func TestMainUsesOneWildcardNameForAdminUserRoutes(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	if strings.Contains(string(source), `"/users/:user_id/`) {
		t.Fatal("admin user routes must use the existing :id wildcard name to avoid Gin route conflicts")
	}
}

func TestMainRegistersFrontendRequiredRoutes(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)

	for _, route := range []string{
		`"/forgot-password"`,
		`"/verify-reset-code"`,
		`"/reset-password"`,
		`"/config/public/billing"`,
		`"/permission-packages"`,
		`"/data-rules"`,
		`"/users/:id/data-scope"`,
		`"/users/:id/data-filter"`,
		`"/users/:id/field-permissions"`,
	} {
		if !strings.Contains(text, route) {
			t.Fatalf("main.go does not register frontend route fragment %s", route)
		}
	}
}

func TestMainRegistersFrontendV2WorkflowRoutes(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)

	for _, route := range []string{
		`apiV2.POST("/templates/compile"`,
		`apiV2.POST("/papers"`,
		`apiV2.POST("/jobs/:job_id/run"`,
		`apiV2.GET("/jobs/:job_id"`,
		`apiV2.GET("/jobs/:job_id/download"`,
		`papers.POST("/upload"`,
	} {
		if !strings.Contains(text, route) {
			t.Fatalf("main.go does not register frontend v2 workflow route fragment %s", route)
		}
	}
}

func TestMainRequiresLoginForPreviouslyPublicServiceRoutes(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)

	for _, route := range []string{
		`api.PUT("/order/:id/status", middleware.AuthMiddleware(cfg, database.DB),`,
		`apiV1.GET("/universities/:id/download-template", middleware.AuthMiddleware(cfg, database.DB),`,
	} {
		if !strings.Contains(text, route) {
			t.Fatalf("service route is missing authentication: %s", route)
		}
	}
}
