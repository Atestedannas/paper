package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestLegacyUploadPathReturnsGoneWhenV2WritePathEnabled(t *testing.T) {
	rec := performLegacyPaperRequest(http.MethodPost, "/api/paper/upload", "/api/paper/upload", (&PaperHandler{}).UploadPaper, "")

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusGone, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, legacyWritePathMessage) {
		t.Fatalf("body = %q, want legacy write path message", body)
	}
}

func TestLegacyApplyCorrectionsPathReturnsGoneWhenV2WritePathEnabled(t *testing.T) {
	paperID := uuid.New().String()
	rec := performLegacyPaperRequest(
		http.MethodPost,
		"/api/paper/:id/apply-corrections",
		"/api/paper/"+paperID+"/apply-corrections",
		(&PaperHandler{}).FixFormat,
		`{"paper_id":"`+paperID+`","check_result_id":"`+uuid.New().String()+`","fix_all":true}`,
	)

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusGone, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, legacyWritePathMessage) {
		t.Fatalf("body = %q, want legacy write path message", body)
	}
}

func TestLegacyFixByTemplatePathReturnsGoneWhenV2WritePathEnabled(t *testing.T) {
	paperID := uuid.New().String()
	rec := performLegacyPaperRequest(
		http.MethodPost,
		"/api/paper/:id/fix-by-template",
		"/api/paper/"+paperID+"/fix-by-template",
		(&PaperHandler{}).FixByTemplate,
		`{"template_id":1}`,
	)

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusGone, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, legacyWritePathMessage) {
		t.Fatalf("body = %q, want legacy write path message", body)
	}
}

func TestLegacyApplyDiffsPathReturnsGoneWhenV2WritePathEnabled(t *testing.T) {
	paperID := uuid.New().String()
	rec := performLegacyPaperRequest(
		http.MethodPost,
		"/api/paper/:id/apply-diffs",
		"/api/paper/"+paperID+"/apply-diffs",
		(&PaperHandler{}).ApplySelectedDiffs,
		`{"selected_diffs":[]}`,
	)

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusGone, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, legacyWritePathMessage) {
		t.Fatalf("body = %q, want legacy write path message", body)
	}
}

func TestLegacyCorrectedDownloadsReturnGoneWhenV2WritePathEnabled(t *testing.T) {
	paperID := uuid.New().String()
	cases := []struct {
		name    string
		path    string
		handler gin.HandlerFunc
	}{
		{name: "corrected-file", path: "/api/paper/:id/corrected-file", handler: (&PaperHandler{}).GetCorrectedPaperFile},
		{name: "export-corrected", path: "/api/paper/:id/export-corrected", handler: (&PaperHandler{}).ExportCorrectedPaper},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := performLegacyPaperRequest(
				http.MethodGet,
				tc.path,
				strings.Replace(tc.path, ":id", paperID, 1),
				tc.handler,
				"",
			)

			if rec.Code != http.StatusGone {
				t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusGone, rec.Body.String())
			}
			if body := rec.Body.String(); !strings.Contains(body, legacyWritePathMessage) {
				t.Fatalf("body = %q, want legacy write path message", body)
			}
		})
	}
}

func performLegacyPaperRequest(method, routePath, requestPath string, handler gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, routePath, handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, requestPath, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(rec, req)
	return rec
}
