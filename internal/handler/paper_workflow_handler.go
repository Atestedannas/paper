package handler

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/paper-format-checker/backend/internal/core/workflow"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
	"gorm.io/gorm"
)

const paperWorkflowDownloadNotReadyMessage = "job not ready for download"

type PaperWorkflowHandler struct {
	svc service.PaperWorkflowService
}

func NewPaperWorkflowHandler(svc service.PaperWorkflowService) *PaperWorkflowHandler {
	return &PaperWorkflowHandler{svc: svc}
}

func (h *PaperWorkflowHandler) CompileTemplate(c *gin.Context) {
	utils.ErrorResponse(c, http.StatusNotImplemented, "template compile is not implemented", "")
}

func (h *PaperWorkflowHandler) CreatePaperJob(c *gin.Context) {
	utils.ErrorResponse(c, http.StatusNotImplemented, "paper job creation is not implemented", "")
}

func (h *PaperWorkflowHandler) RunJob(c *gin.Context) {
	utils.ErrorResponse(c, http.StatusNotImplemented, "job run is not implemented", "")
}

func (h *PaperWorkflowHandler) GetJob(c *gin.Context) {
	if h == nil || h.svc == nil {
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "")
		return
	}

	job, err := h.svc.GetJob(c.Param("job_id"))
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

	job, err := h.svc.GetJob(c.Param("job_id"))
	if err != nil {
		h.respondJobLookupError(c, err)
		return
	}
	if job == nil || job.Status != string(workflow.StatusVerifiedPass) || job.DownloadPath == "" {
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "")
		return
	}

	info, err := os.Stat(job.DownloadPath)
	if err != nil || !info.Mode().IsRegular() {
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "")
		return
	}

	c.FileAttachment(job.DownloadPath, filepath.Base(job.DownloadPath))
}

func (h *PaperWorkflowHandler) respondJobLookupError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidJobID):
		utils.ErrorResponse(c, http.StatusBadRequest, "invalid job id", err.Error())
	case errors.Is(err, gorm.ErrRecordNotFound):
		utils.ErrorResponse(c, http.StatusNotFound, "job not found", err.Error())
	default:
		utils.ErrorResponse(c, http.StatusInternalServerError, "failed to get job", err.Error())
	}
}
