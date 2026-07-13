package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
)

func TestEffectiveUserRoleUsesAssignedSuperAdminRole(t *testing.T) {
	user := &model.User{Role: "admin", Roles: []model.Role{{Code: "super_admin"}}}

	if got := effectiveUserRole(user); got != "super_admin" {
		t.Fatalf("effectiveUserRole() = %q, want super_admin", got)
	}
}

func TestResolveAdminResourceMatchesPermissionPrefixes(t *testing.T) {
	tests := map[string]string{
		"/api/v1/admin/settings/payment/alipay":  "payment",
		"/api/v1/admin/settings/support-contact": "support",
		"/api/v1/admin/permission-packages":      "rbac",
	}

	for path, want := range tests {
		if got := resolveAdminResource(path); got != want {
			t.Fatalf("resolveAdminResource(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestAdminRBACAllowsCurrentUserMenuTreeWithoutPermissionLookup(t *testing.T) {
	allowed := []string{
		"/api/v1/admin/menus/user-tree",
		"/api/admin/menus/user-tree",
		"/api/v1/admin/menus/user",
		"/api/admin/menus/user",
	}

	for _, path := range allowed {
		t.Run(path, func(t *testing.T) {
			if !isAdminRBACSelfMenuPath(path) {
				t.Fatalf("isAdminRBACSelfMenuPath(%q) = false, want true", path)
			}
		})
	}
}

func TestAdminRBACDoesNotBypassGeneralMenuManagement(t *testing.T) {
	blocked := []string{
		"/api/v1/admin/menus",
		"/api/v1/admin/menus/tree",
		"/api/admin/menus",
		"/api/admin/menus/tree",
	}

	for _, path := range blocked {
		t.Run(path, func(t *testing.T) {
			if isAdminRBACSelfMenuPath(path) {
				t.Fatalf("isAdminRBACSelfMenuPath(%q) = true, want false", path)
			}
		})
	}
}

func TestAdminMiddlewareAllowsAuthenticatedUserMenuTreeForNonAdmin(t *testing.T) {
	rec := performAdminMiddlewareRequest("/api/v1/admin/menus/user-tree", "user")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAdminMiddlewareStillBlocksNonAdminMenuManagement(t *testing.T) {
	rec := performAdminMiddlewareRequest("/api/v1/admin/menus", "user")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func performAdminMiddlewareRequest(path, role string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/*path", func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("role", role)
		c.Next()
	}, AdminMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(rec, req)
	return rec
}
