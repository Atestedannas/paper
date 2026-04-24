package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/core/workflow"
	"github.com/paper-format-checker/backend/internal/service"
	"gorm.io/gorm"
)

type fakePaperWorkflowService struct {
	job *service.WorkflowJobView
	err error
}

func (f fakePaperWorkflowService) GetJob(id string) (*service.WorkflowJobView, error) {
	return f.job, f.err
}

func TestPaperWorkflowHandlerDownloadJobServiceNilReturnsConflict(t *testing.T) {
	rec := performPaperWorkflowDownload(t, NewPaperWorkflowHandler(nil), uuid.New().String())

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if body := rec.Body.String(); body == "" || !contains(body, "job not ready for download") {
		t.Fatalf("body = %q, want job not ready message", body)
	}
}

func TestPaperWorkflowHandlerDownloadJobRejectsNonVerifiedPass(t *testing.T) {
	jobID := uuid.New()
	rec := performPaperWorkflowDownload(t, NewPaperWorkflowHandler(fakePaperWorkflowService{
		job: &service.WorkflowJobView{
			ID:           jobID,
			Status:       string(workflow.StatusPatched),
			Stage:        workflow.StagePatchAttempted,
			DownloadPath: "out/final.docx",
		},
	}), jobID.String())

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestPaperWorkflowHandlerDownloadJobReturnsFileForVerifiedPass(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "final.docx")
	if err := os.WriteFile(path, []byte("docx bytes"), 0644); err != nil {
		t.Fatalf("write docx: %v", err)
	}

	jobID := uuid.New()
	rec := performPaperWorkflowDownload(t, NewPaperWorkflowHandler(fakePaperWorkflowService{
		job: &service.WorkflowJobView{
			ID:           jobID,
			Status:       string(workflow.StatusVerifiedPass),
			Stage:        workflow.StageVerified,
			DownloadPath: path,
		},
	}), jobID.String())

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "docx bytes" {
		t.Fatalf("body = %q, want file bytes", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); !contains(got, "final.docx") {
		t.Fatalf("Content-Disposition = %q, want final.docx", got)
	}
}

func TestPaperWorkflowHandlerDownloadJobRejectsMissingFile(t *testing.T) {
	jobID := uuid.New()
	rec := performPaperWorkflowDownload(t, NewPaperWorkflowHandler(fakePaperWorkflowService{
		job: &service.WorkflowJobView{
			ID:           jobID,
			Status:       string(workflow.StatusVerifiedPass),
			Stage:        workflow.StageVerified,
			DownloadPath: filepath.Join(t.TempDir(), "missing.docx"),
		},
	}), jobID.String())

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestPaperWorkflowHandlerDownloadJobRejectsInvalidID(t *testing.T) {
	rec := performPaperWorkflowDownload(t, NewPaperWorkflowHandler(fakePaperWorkflowService{
		err: service.ErrInvalidJobID,
	}), "not-a-uuid")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPaperWorkflowHandlerDownloadJobReturnsNotFound(t *testing.T) {
	rec := performPaperWorkflowDownload(t, NewPaperWorkflowHandler(fakePaperWorkflowService{
		err: gorm.ErrRecordNotFound,
	}), uuid.New().String())

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func performPaperWorkflowDownload(t *testing.T, handler *PaperWorkflowHandler, jobID string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/jobs/:job_id/download", handler.DownloadJob)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs/"+jobID+"/download", nil)
	router.ServeHTTP(rec, req)
	return rec
}

func contains(s string, substr string) bool {
	return strings.Contains(s, substr)
}
