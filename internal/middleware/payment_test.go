package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
)

func TestPaymentMiddlewareRequiresLoginEvenWhenServiceIsFree(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Payment.PaperDownload = 0

	router := gin.New()
	router.GET("/download", PaymentMiddleware(cfg, ServicePaperDownload), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/download", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated free download to be rejected with 401, got %d", w.Code)
	}
}

func TestGlobalFreeSwitchCoversPaperServices(t *testing.T) {
	config := map[string]interface{}{
		"is_check_free":  true,
		"format_fix":     15.0,
		"paper_download": 15.0,
	}
	for _, serviceType := range []ServiceType{ServiceFormatCheck, ServiceFormatFix, ServicePaperDownload} {
		if !serviceIsFreeBySetting(config, serviceType) {
			t.Fatalf("expected %s to be free", serviceType)
		}
	}
}

func TestPaymentMiddlewareRejectsAnonymousNilUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Payment.PaperDownload = 0

	router := gin.New()
	router.GET("/download", func(c *gin.Context) {
		c.Set("user_id", uuid.Nil)
		c.Next()
	}, PaymentMiddleware(cfg, ServicePaperDownload), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/download", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous nil user to be rejected with 401, got %d", w.Code)
	}
}

func TestConditionalAuthMiddlewareNoLongerCreatesAnonymousUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.JWT.Secret = "test-secret"

	router := gin.New()
	router.GET("/papers", ConditionalAuthMiddleware(cfg, nil, ServiceFormatCheck), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/papers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected conditional auth to require login, got %d", w.Code)
	}
}
