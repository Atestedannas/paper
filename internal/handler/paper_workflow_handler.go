package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
	"gorm.io/gorm"
)

const paperWorkflowDownloadNotReadyMessage = "job not ready for download"
const defaultPaperWorkflowDownloadRoot = "uploads/workflow_outputs"

type PaperWorkflowHandler struct {
	svc          service.PaperWorkflowService
	downloadRoot string
	promoCodes   *service.PromoCodeService
}

func NewPaperWorkflowHandler(svc service.PaperWorkflowService) *PaperWorkflowHandler {
	return NewPaperWorkflowHandlerWithDownloadRoot(svc, defaultPaperWorkflowDownloadRoot)
}

func NewPaperWorkflowHandlerWithDownloadRoot(svc service.PaperWorkflowService, root string) *PaperWorkflowHandler {
	return &PaperWorkflowHandler{svc: svc, downloadRoot: root, promoCodes: service.NewPromoCodeService(database.DB)}
}

func (h *PaperWorkflowHandler) CompileTemplate(c *gin.Context) {
	if h == nil || h.svc == nil {
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "")
		return
	}

	if _, ok := authenticatedUserID(c); !ok {
		utils.ErrorResponse(c, http.StatusUnauthorized, "user not authenticated", "")
		return
	}

	file, err := c.FormFile("template")
	if err != nil {
		file, err = c.FormFile("file")
	}
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "template file is required", err.Error())
		return
	}
	if !strings.EqualFold(filepath.Ext(file.Filename), ".docx") {
		utils.ErrorResponse(c, http.StatusBadRequest, "only .docx files are supported", "")
		return
	}

	safeName := filepath.Base(file.Filename)
	inputDir := filepath.Join("uploads", "workflow_templates")
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "failed to prepare template directory", err.Error())
		return
	}
	inputPath := filepath.Join(inputDir, uuid.New().String()+"_"+safeName)
	if err := c.SaveUploadedFile(file, inputPath); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "failed to save template file", err.Error())
		return
	}
	if err := ooxmlpkg.Validate(inputPath); err != nil {
		_ = os.Remove(inputPath)
		utils.ErrorResponse(c, http.StatusBadRequest, "unsafe or invalid DOCX", err.Error())
		return
	}

	template, err := h.svc.CompileTemplate(c.Request.Context(), service.CompileTemplateInput{
		SchoolID:     strings.TrimSpace(c.PostForm("school_id")),
		TemplateName: strings.TrimSpace(c.PostForm("template_name")),
		Version:      strings.TrimSpace(c.PostForm("version")),
		FilePath:     inputPath,
	})
	if err != nil {
		_ = os.Remove(inputPath)
		h.respondWorkflowError(c, err)
		return
	}

	utils.CreatedResponse(c, "template compiled", gin.H{
		"template":    template,
		"template_id": template.ID.String(),
	})
}

func (h *PaperWorkflowHandler) CreatePaperJob(c *gin.Context) {
	log.Printf("[WORKFLOW_FLOW] create upload request path=%s method=%s python_url=%q", c.Request.URL.Path, c.Request.Method, strings.TrimSpace(os.Getenv("PYTHON_SERVICE_URL")))
	if h == nil || h.svc == nil {
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "")
		return
	}

	userID, ok := authenticatedUserID(c)
	if !ok {
		utils.ErrorResponse(c, http.StatusUnauthorized, "user not authenticated", "")
		return
	}

	grantID, orderID, err := h.authorizePaperJob(c, userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusPaymentRequired, err.Error(), "")
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
	if err := ooxmlpkg.Validate(inputPath); err != nil {
		_ = os.Remove(inputPath)
		utils.ErrorResponse(c, http.StatusBadRequest, "unsafe or invalid DOCX", err.Error())
		return
	}
	var templateID uuid.UUID
	raw := strings.TrimSpace(c.PostForm("template_id"))
	if raw == "" {
		_ = os.Remove(inputPath)
		utils.ErrorResponse(c, http.StatusBadRequest, "请选择学校模板", "template_id is required")
		return
	}
	templateID, err = resolveWorkflowFormatTemplateID(raw, c.PostForm("document_type"))

	if err != nil {
		_ = os.Remove(inputPath)
		utils.ErrorResponse(c, http.StatusBadRequest, "invalid template_id", err.Error())
		return
	}

	job, err := h.svc.CreatePaperJob(c.Request.Context(), service.CreatePaperJobInput{
		UserID:           userID,
		FormatTemplateID: templateID,
		Title:            strings.TrimSpace(c.PostForm("title")),
		FilePath:         inputPath,
		FileName:         safeName,
		FileSize:         file.Size,
		FileType:         "docx",
	})
	if err != nil {
		_ = os.Remove(inputPath)
		h.respondWorkflowError(c, err)
		return
	}
	if err := h.consumePaperJobAccess(c, userID, job.PaperID, grantID, orderID); err != nil {
		utils.ErrorResponse(c, http.StatusConflict, "服务授权绑定失败", err.Error())
		return
	}
	log.Printf("[WORKFLOW_FLOW] create upload accepted job=%s paper=%s compiled_template=%s file=%s", job.ID, job.PaperID, job.CompiledTemplateID, inputPath)

	utils.CreatedResponse(c, "paper job created", h.jobResponse(job))
}

func resolveWorkflowFormatTemplateID(raw string, documentType string) (uuid.UUID, error) {
	if id, err := uuid.Parse(strings.TrimSpace(raw)); err == nil {
		return id, nil
	}
	universityID, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || universityID <= 0 {
		return uuid.Nil, fmt.Errorf("template_id must be a template UUID or university ID")
	}

	query := database.DB.Where("university_id = ? AND is_active = ?", universityID, true)
	if documentType = strings.TrimSpace(documentType); documentType != "" {
		query = query.Where("document_type = ?", documentType)
	} else {
		query = query.Where("document_type = ?", "本科论文")
	}
	var preferredTemplates []model.FormatTemplate
	if err := query.Order("updated_at DESC").Find(&preferredTemplates).Error; err != nil {
		return uuid.Nil, err
	}
	for _, template := range preferredTemplates {
		if service.WorkflowTemplatePath(template) != "" {
			return template.ID, nil
		}
	}
	var templates []model.FormatTemplate
	if err := database.DB.Where("university_id = ? AND is_active = ?", universityID, true).
		Order("updated_at DESC").Find(&templates).Error; err != nil {
		return uuid.Nil, err
	}
	for _, template := range templates {
		if service.WorkflowTemplatePath(template) != "" {
			return template.ID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("学校 %d 的模板文件缺失，请联系管理员上传模板", universityID)
}

func (h *PaperWorkflowHandler) authorizePaperJob(c *gin.Context, userID uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	if config, err := service.GetSystemSettingService().GetPaymentConfig(); err == nil {
		if free, _ := config["is_check_free"].(bool); free {
			return uuid.Nil, uuid.Nil, nil
		}
	}
	var user model.User
	if err := database.DB.Select("role", "is_free_user").First(&user, "id = ?", userID).Error; err == nil && (user.Role == "admin" || user.Role == "super_admin" || user.IsFreeUser) {
		return uuid.Nil, uuid.Nil, nil
	}

	if value := strings.TrimSpace(c.PostForm("promo_grant_id")); value != "" {
		grantID, err := uuid.Parse(value)
		if err != nil {
			return uuid.Nil, uuid.Nil, service.ErrPromoGrantInvalid
		}
		if _, err := h.promoCodes.ValidateGrant(c.Request.Context(), grantID, userID, "check_and_fix"); err != nil {
			return uuid.Nil, uuid.Nil, err
		}
		return grantID, uuid.Nil, nil
	}

	if value := strings.TrimSpace(c.PostForm("order_id")); value != "" {
		orderID, err := uuid.Parse(value)
		if err != nil {
			return uuid.Nil, uuid.Nil, errors.New("支付订单无效")
		}
		var order model.Order
		if err := database.DB.Where("id = ? AND user_id = ? AND payment_status = ? AND used_at IS NULL", orderID, userID, "paid").First(&order).Error; err == nil {
			if order.ServiceType == "" || order.ServiceType == "check_and_fix" {
				return uuid.Nil, orderID, nil
			}
		}
	}
	return uuid.Nil, uuid.Nil, errors.New("请先完成支付或使用体验卡密")
}

func (h *PaperWorkflowHandler) consumePaperJobAccess(c *gin.Context, userID, paperID, grantID, orderID uuid.UUID) error {
	if grantID != uuid.Nil {
		return h.promoCodes.BindGrant(c.Request.Context(), grantID, userID, paperID)
	}
	if orderID != uuid.Nil {
		now := time.Now()
		result := database.DB.Model(&model.Order{}).
			Where("id = ? AND user_id = ? AND payment_status = ? AND used_at IS NULL", orderID, userID, "paid").
			Updates(map[string]interface{}{"paper_id": paperID, "used_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("支付订单已使用")
		}
	}
	return nil
}

func (h *PaperWorkflowHandler) RunJob(c *gin.Context) {
	log.Printf("[WORKFLOW_FLOW] run request job=%s python_url=%q", c.Param("job_id"), strings.TrimSpace(os.Getenv("PYTHON_SERVICE_URL")))
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
		log.Printf("[WORKFLOW_FLOW] run failed job=%s err=%v", c.Param("job_id"), err)
		h.respondWorkflowError(c, err)
		return
	}
	log.Printf("[WORKFLOW_FLOW] run completed job=%s status=%s stage=%s download=%q ready=%t", job.ID, job.Status, job.Stage, job.DownloadPath, job.DownloadReady)

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
	log.Printf("[WORKFLOW_FLOW] download request job=%s", c.Param("job_id"))
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
		detail := "generated DOCX is not available"
		if job != nil {
			detail = fmt.Sprintf("status=%s stage=%s download_path=%q download_ready=%t", job.Status, job.Stage, job.DownloadPath, job.DownloadReady)
		}
		log.Printf("[WORKFLOW_DOWNLOAD] not ready job=%s %s", c.Param("job_id"), detail)
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, detail)
		return
	}

	safePath, ok := h.safeDownloadPath(job.DownloadPath)
	if !ok {
		log.Printf("[WORKFLOW_DOWNLOAD] unsafe or missing path job=%s path=%q root=%q", c.Param("job_id"), job.DownloadPath, h.downloadRoot)
		utils.ErrorResponse(c, http.StatusConflict, paperWorkflowDownloadNotReadyMessage, "download path is missing, outside the download root, or the file does not exist")
		return
	}

	c.FileAttachment(safePath, filepath.Base(safePath))
	log.Printf("[WORKFLOW_FLOW] download served job=%s path=%s", c.Param("job_id"), safePath)
}

func jobDownloadReady(job *service.WorkflowJobView) bool {
	if job == nil || strings.TrimSpace(job.DownloadPath) == "" {
		return false
	}
	// Manual review remains visible in the job status, but must not block
	// retrieval of the generated artifact.
	return true
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
	case errors.Is(err, service.ErrTemplateDOCXMissing):
		utils.ErrorResponse(c, http.StatusUnprocessableEntity, "selected template is not usable", err.Error())
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
	if strings.TrimSpace(job.DownloadURL) != "" {
		payload["download_url"] = job.DownloadURL
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
