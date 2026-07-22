package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"mime/multipart"
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
	job      *service.WorkflowJobView
	template *service.CompiledTemplateView
	err      error
}

func (f fakePaperWorkflowService) CompileTemplate(context.Context, service.CompileTemplateInput) (*service.CompiledTemplateView, error) {
	return f.template, f.err
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

func TestPaperWorkflowHandlerRunJobReturnsFrontendDownloadURL(t *testing.T) {
	jobID := uuid.New()
	userID := uuid.New()
	downloadURL := "/api/v2/jobs/" + jobID.String() + "/download"
	rec := performPaperWorkflowRunAsUser(t, NewPaperWorkflowHandler(fakePaperWorkflowService{
		job: &service.WorkflowJobView{
			ID:          jobID,
			UserID:      userID,
			Status:      string(workflow.StatusVerifiedPass),
			Stage:       workflow.StageVerified,
			DownloadURL: downloadURL,
		},
	}), jobID.String(), userID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if body := rec.Body.String(); !containsWorkflowHandlerText(body, `"job_id":"`+jobID.String()+`"`) || !containsWorkflowHandlerText(body, `"download_url":"`+downloadURL+`"`) {
		t.Fatalf("body = %q, want frontend job_id and download_url", body)
	}
}

func TestPaperWorkflowHandlerCompileTemplateReturnsFrontendTemplateID(t *testing.T) {
	templateID := uuid.New()
	userID := uuid.New()
	rec := performPaperWorkflowCompileAsUser(t, NewPaperWorkflowHandler(fakePaperWorkflowService{
		template: &service.CompiledTemplateView{
			ID:              templateID,
			SchoolID:        "school",
			TemplateName:    "template.docx",
			TemplateVersion: "runtime",
			Status:          "compiled",
		},
	}), userID)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if body := rec.Body.String(); !containsWorkflowHandlerText(body, `"template_id":"`+templateID.String()+`"`) {
		t.Fatalf("body = %q, want frontend template_id", body)
	}
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

func TestPaperWorkflowHandlerDownloadJobReturnsDraftForManualReview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "draft.docx")
	if err := os.WriteFile(path, []byte("draft bytes"), 0644); err != nil {
		t.Fatalf("write draft docx: %v", err)
	}

	jobID := uuid.New()
	userID := uuid.New()
	rec := performPaperWorkflowDownloadAsUser(t, NewPaperWorkflowHandlerWithDownloadRoot(fakePaperWorkflowService{
		job: &service.WorkflowJobView{
			ID:           jobID,
			UserID:       userID,
			Status:       string(workflow.StatusManualReview),
			Stage:        workflow.StageManualReview,
			DownloadPath: "draft.docx",
		},
	}, root), jobID.String(), userID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "draft bytes" {
		t.Fatalf("body = %q, want draft bytes", rec.Body.String())
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

func performPaperWorkflowRunAsUser(t *testing.T, handler *PaperWorkflowHandler, jobID string, userID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Next()
	})
	router.POST("/jobs/:job_id/run", handler.RunJob)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/run", nil)
	router.ServeHTTP(rec, req)
	return rec
}

func performPaperWorkflowCompileAsUser(t *testing.T, handler *PaperWorkflowHandler, userID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("template", "template.docx")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	var docx bytes.Buffer
	zipWriter := zip.NewWriter(&docx)
	for name, content := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types/>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body/></w:document>`,
	} {
		entry, createErr := zipWriter.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(docx.Bytes()); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Next()
	})
	router.POST("/templates/compile", handler.CompileTemplate)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/templates/compile", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(rec, req)
	return rec
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
