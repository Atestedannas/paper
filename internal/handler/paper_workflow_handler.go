package handler

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/core/workflow"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
	"gorm.io/gorm"
)

const paperWorkflowDownloadNotReadyMessage = "job not ready for download"
const defaultPaperWorkflowDownloadRoot = "uploads/workflow_outputs"

type PaperWorkflowHandler struct {
	svc          service.PaperWorkflowService
	downloadRoot string
}

func NewPaperWorkflowHandler(svc service.PaperWorkflowService) *PaperWorkflowHandler {
	return NewPaperWorkflowHandlerWithDownloadRoot(svc, defaultPaperWorkflowDownloadRoot)
}

func NewPaperWorkflowHandlerWithDownloadRoot(svc service.PaperWorkflowService, root string) *PaperWorkflowHandler {
	return &PaperWorkflowHandler{svc: svc, downloadRoot: root}
}

func (h *PaperWorkflowHandler) CompileTemplate(c *gin.Context) {
	utils.ErrorResponse(c, http.StatusNotImplemented, "template compile is not implemented", "")
}

func (h *PaperWorkflowHandler) CreatePaperJob(c *gin.Context) {
	if h == nil || h.svc == nil {
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "")
		return
	}

	userID, ok := authenticatedUserID(c)
	if !ok {
		utils.ErrorResponse(c, http.StatusUnauthorized, "user not authenticated", "")
		return
	}

	file, err := c.FormFile("paper")
	if err != nil {
		file, err = c.FormFile("file")
	}
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "paper file is required", err.Error())
		return
	}
	if !strings.EqualFold(filepath.Ext(file.Filename), ".docx") {
		utils.ErrorResponse(c, http.StatusBadRequest, "only .docx files are supported", "")
		return
	}

	safeName := filepath.Base(file.Filename)
	storedName := uuid.New().String() + "_" + safeName
	inputDir := filepath.Join("uploads", "workflow_inputs", userID.String())
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "failed to prepare upload directory", err.Error())
		return
	}
	inputPath := filepath.Join(inputDir, storedName)
	if err := c.SaveUploadedFile(file, inputPath); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "failed to save paper file", err.Error())
		return
	}

	job, err := h.svc.CreatePaperJob(c.Request.Context(), service.CreatePaperJobInput{
		UserID:   userID,
		Title:    strings.TrimSpace(c.PostForm("title")),
		FilePath: inputPath,
		FileName: safeName,
		FileSize: file.Size,
		FileType: "docx",
	})
	if err != nil {
		_ = os.Remove(inputPath)
		h.respondWorkflowError(c, err)
		return
	}

	utils.CreatedResponse(c, "paper job created", h.jobResponse(job))
}

func (h *PaperWorkflowHandler) RunJob(c *gin.Context) {
	if h == nil || h.svc == nil {
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "")
		return
	}

	userID, ok := authenticatedUserID(c)
	if !ok {
		utils.ErrorResponse(c, http.StatusUnauthorized, "user not authenticated", "")
		return
	}

	job, err := h.svc.RunJob(c.Request.Context(), c.Param("job_id"), userID)
	if err != nil {
		h.respondWorkflowError(c, err)
		return
	}

	utils.SuccessResponse(c, "job run completed", h.jobResponse(job))
}

func (h *PaperWorkflowHandler) GetJob(c *gin.Context) {
	if h == nil || h.svc == nil {
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "")
		return
	}

	userID, ok := authenticatedUserID(c)
	if !ok {
		utils.ErrorResponse(c, http.StatusUnauthorized, "user not authenticated", "")
		return
	}

	job, err := h.svc.GetJobForUser(c.Param("job_id"), userID)
	if err != nil {
		h.respondJobLookupError(c, err)
		return
	}

	utils.SuccessResponse(c, "job found", job)
}

func (h *PaperWorkflowHandler) DownloadJob(c *gin.Context) {
	if h == nil || h.svc == nil {
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "")
		return
	}

	userID, ok := authenticatedUserID(c)
	if !ok {
		utils.ErrorResponse(c, http.StatusUnauthorized, "user not authenticated", "")
		return
	}

	job, err := h.svc.GetJobForUser(c.Param("job_id"), userID)
	if err != nil {
		h.respondJobLookupError(c, err)
		return
	}
	if job == nil || !jobDownloadReady(job) {
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "")
		return
	}

	safePath, ok := h.safeDownloadPath(job.DownloadPath)
	if !ok {
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "")
		return
	}

	c.FileAttachment(safePath, filepath.Base(safePath))
}

func jobDownloadReady(job *service.WorkflowJobView) bool {
	if job == nil || strings.TrimSpace(job.DownloadPath) == "" {
		return false
	}
	return job.Status == string(workflow.StatusVerifiedPass) || job.Status == string(workflow.StatusManualReview)
}

func (h *PaperWorkflowHandler) respondJobLookupError(c *gin.Context, err error) {
	h.respondWorkflowError(c, err)
}

func (h *PaperWorkflowHandler) respondWorkflowError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidJobID):
		utils.ErrorResponse(c, http.StatusBadRequest, "invalid job id", err.Error())
	case errors.Is(err, service.ErrInvalidPaperUpload):
		utils.ErrorResponse(c, http.StatusBadRequest, "invalid paper upload", err.Error())
	case errors.Is(err, service.ErrServiceUnavailable):
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, err.Error())
	case errors.Is(err, gorm.ErrRecordNotFound):
		utils.ErrorResponse(c, http.StatusNotFound, "job not found", err.Error())
	default:
		utils.ErrorResponse(c, http.StatusInternalServerError, "paper workflow failed", err.Error())
	}
}

func (h *PaperWorkflowHandler) jobResponse(job *service.WorkflowJobView) gin.H {
	payload := gin.H{
		"job": job,
	}
	if job == nil {
		return payload
	}
	payload["job_id"] = job.ID.String()
	if job.Status == string(workflow.StatusVerifiedPass) {
		payload["download_url"] = "/api/v2/jobs/" + job.ID.String() + "/download"
	}
	return payload
}

func authenticatedUserID(c *gin.Context) (uuid.UUID, bool) {
	userIDValue, exists := c.Get("user_id")
	if !exists {
		return uuid.UUID{}, false
	}

	userID, ok := userIDValue.(uuid.UUID)
	return userID, ok
}

func (h *PaperWorkflowHandler) safeDownloadPath(downloadPath string) (string, bool) {
	root := h.downloadRoot
	if strings.TrimSpace(root) == "" || strings.TrimSpace(downloadPath) == "" {
		return "", false
	}
	if !strings.EqualFold(filepath.Ext(downloadPath), ".docx") {
		return "", false
	}

	cleanRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", false
	}

	var candidate string
	if filepath.IsAbs(downloadPath) {
		candidate = filepath.Clean(downloadPath)
	} else {
		candidate = filepath.Join(cleanRoot, filepath.Clean(downloadPath))
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", false
	}

	if !pathWithinRoot(candidate, cleanRoot) {
		return "", false
	}

	info, err := os.Lstat(candidate)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", false
	}

	realRoot, err := filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		return "", false
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", false
	}
	realRoot, err = filepath.Abs(realRoot)
	if err != nil {
		return "", false
	}
	realCandidate, err = filepath.Abs(realCandidate)
	if err != nil {
		return "", false
	}
	if !pathWithinRoot(realCandidate, realRoot) {
		return "", false
	}

	return candidate, true
}

func pathWithinRoot(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel))
}
