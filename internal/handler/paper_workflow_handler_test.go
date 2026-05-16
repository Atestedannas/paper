package handler

import (
	"context"
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

func (f fakePaperWorkflowService) CreatePaperJob(context.Context, service.CreatePaperJobInput) (*service.WorkflowJobView, error) {
	return f.job, f.err
}

func (f fakePaperWorkflowService) RunJob(context.Context, string, uuid.UUID) (*service.WorkflowJobView, error) {
	return f.job, f.err
}

func (f fakePaperWorkflowService) GetJob(id string) (*service.WorkflowJobView, error) {
	return f.job, f.err
}

func (f fakePaperWorkflowService) GetJobForUser(id string, userID uuid.UUID) (*service.WorkflowJobView, error) {
	return f.job, f.err
}

func TestPaperWorkflowHandlerDownloadJobServiceNilReturnsConflict(t *testing.T) {
	rec := performPaperWorkflowDownload(t, NewPaperWorkflowHandler(nil), uuid.New().String())

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if body := rec.Body.String(); body == "" || !containsWorkflowHandlerText(body, "job not ready for download") {
		t.Fatalf("body = %q, want job not ready message", body)
	}
}

func TestPaperWorkflowHandlerDownloadJobRejectsNonVerifiedPass(t *testing.T) {
	jobID := uuid.New()
	rec := performPaperWorkflowDownloadAsUser(t, NewPaperWorkflowHandler(fakePaperWorkflowService{
		job: &service.WorkflowJobView{
			ID:           jobID,
			UserID:       uuid.New(),
			Status:       string(workflow.StatusPatched),
			Stage:        workflow.StagePatchAttempted,
			DownloadPath: "out/final.docx",
		},
	}), jobID.String(), uuid.New())

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestPaperWorkflowHandlerDownloadJobReturnsFileForVerifiedPass(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "final.docx")
	if err := os.WriteFile(path, []byte("docx bytes"), 0644); err != nil {
		t.Fatalf("write docx: %v", err)
	}

	jobID := uuid.New()
	userID := uuid.New()
	rec := performPaperWorkflowDownloadAsUser(t, NewPaperWorkflowHandlerWithDownloadRoot(fakePaperWorkflowService{
		job: &service.WorkflowJobView{
			ID:           jobID,
			UserID:       userID,
			Status:       string(workflow.StatusVerifiedPass),
			Stage:        workflow.StageVerified,
			DownloadPath: "final.docx",
		},
	}, root), jobID.String(), userID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "docx bytes" {
		t.Fatalf("body = %q, want file bytes", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); !containsWorkflowHandlerText(got, "final.docx") {
		t.Fatalf("Content-Disposition = %q, want final.docx", got)
	}
}

func TestPaperWorkflowHandlerDownloadJobRejectsMissingFile(t *testing.T) {
	jobID := uuid.New()
	userID := uuid.New()
	rec := performPaperWorkflowDownloadAsUser(t, NewPaperWorkflowHandlerWithDownloadRoot(fakePaperWorkflowService{
		job: &service.WorkflowJobView{
			ID:           jobID,
			UserID:       userID,
			Status:       string(workflow.StatusVerifiedPass),
			Stage:        workflow.StageVerified,
			DownloadPath: "missing.docx",
		},
	}, t.TempDir()), jobID.String(), userID)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestPaperWorkflowHandlerDownloadJobRejectsInvalidID(t *testing.T) {
	rec := performPaperWorkflowDownloadAsUser(t, NewPaperWorkflowHandler(fakePaperWorkflowService{
		err: service.ErrInvalidJobID,
	}), "not-a-uuid", uuid.New())

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPaperWorkflowHandlerDownloadJobReturnsNotFound(t *testing.T) {
	rec := performPaperWorkflowDownloadAsUser(t, NewPaperWorkflowHandler(fakePaperWorkflowService{
		err: gorm.ErrRecordNotFound,
	}), uuid.New().String(), uuid.New())

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPaperWorkflowHandlerDownloadJobRequiresAuthenticatedUser(t *testing.T) {
	rec := performPaperWorkflowDownload(t, NewPaperWorkflowHandler(fakePaperWorkflowService{}), uuid.New().String())

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestPaperWorkflowHandlerDownloadJobRejectsRootEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parent, "outside.docx"), []byte("outside"), 0644); err != nil {
		t.Fatalf("write outside docx: %v", err)
	}

	jobID := uuid.New()
	userID := uuid.New()
	rec := performPaperWorkflowDownloadAsUser(t, NewPaperWorkflowHandlerWithDownloadRoot(fakePaperWorkflowService{
		job: &service.WorkflowJobView{
			ID:           jobID,
			UserID:       userID,
			Status:       string(workflow.StatusVerifiedPass),
			Stage:        workflow.StageVerified,
			DownloadPath: "../outside.docx",
		},
	}, root), jobID.String(), userID)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestPaperWorkflowHandlerDownloadJobRejectsNonDocx(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "final.pdf"), []byte("pdf bytes"), 0644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}

	jobID := uuid.New()
	userID := uuid.New()
	rec := performPaperWorkflowDownloadAsUser(t, NewPaperWorkflowHandlerWithDownloadRoot(fakePaperWorkflowService{
		job: &service.WorkflowJobView{
			ID:           jobID,
			UserID:       userID,
			Status:       string(workflow.StatusVerifiedPass),
			Stage:        workflow.StageVerified,
			DownloadPath: "final.pdf",
		},
	}, root), jobID.String(), userID)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestPaperWorkflowHandlerDownloadJobRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.docx")
	link := filepath.Join(root, "linked.docx")
	if err := os.WriteFile(target, []byte("docx bytes"), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	jobID := uuid.New()
	userID := uuid.New()
	rec := performPaperWorkflowDownloadAsUser(t, NewPaperWorkflowHandlerWithDownloadRoot(fakePaperWorkflowService{
		job: &service.WorkflowJobView{
			ID:           jobID,
			UserID:       userID,
			Status:       string(workflow.StatusVerifiedPass),
			Stage:        workflow.StageVerified,
			DownloadPath: "linked.docx",
		},
	}, root), jobID.String(), userID)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func performPaperWorkflowDownload(t *testing.T, handler *PaperWorkflowHandler, jobID string) *httptest.ResponseRecorder {
	return performPaperWorkflowDownloadWithUserValue(t, handler, jobID, nil)
}

func performPaperWorkflowDownloadAsUser(t *testing.T, handler *PaperWorkflowHandler, jobID string, userID uuid.UUID) *httptest.ResponseRecorder {
	return performPaperWorkflowDownloadWithUserValue(t, handler, jobID, userID)
}

func performPaperWorkflowDownloadWithUserValue(t *testing.T, handler *PaperWorkflowHandler, jobID string, userID any) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if userID != nil {
			c.Set("user_id", userID)
		}
		c.Next()
	})
	router.GET("/jobs/:job_id/download", handler.DownloadJob)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs/"+jobID+"/download", nil)
	router.ServeHTTP(rec, req)
	return rec
}

func containsWorkflowHandlerText(s string, substr string) bool {
	return strings.Contains(s, substr)
}
