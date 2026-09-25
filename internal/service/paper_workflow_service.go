package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/core/blockmap"
	"github.com/paper-format-checker/backend/internal/core/evidence"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/paperparse"
	"github.com/paper-format-checker/backend/internal/core/repaircontract"
	"github.com/paper-format-checker/backend/internal/core/roleclassify"
	"github.com/paper-format-checker/backend/internal/core/templateapply"
	"github.com/paper-format-checker/backend/internal/core/templatecompile"
	"github.com/paper-format-checker/backend/internal/core/templatecontract"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
	"github.com/paper-format-checker/backend/internal/core/transplant"
	"github.com/paper-format-checker/backend/internal/core/verify"
	"github.com/paper-format-checker/backend/internal/core/workflow"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/pkg/aiclassifier"
	"github.com/paper-format-checker/backend/pkg/fileprocessor"
	"gorm.io/gorm"
)

var (
	ErrInvalidJobID        = errors.New("invalid job id")
	ErrInvalidPaperUpload  = errors.New("invalid paper upload")
	ErrTemplateDOCXMissing = errors.New("selected template has no readable DOCX file")
	ErrServiceUnavailable  = errors.New("paper workflow service unavailable")
)

func workflowVisualSpec(templateID string, profile *templateprofile.Profile) map[string]interface{} {
	rules := make([]interface{}, 0)
	if profile == nil {
		return map[string]interface{}{"specVersion": "1.0", "templateId": templateID, "rules": rules}
	}
	roles := []string{"body", "heading_1", "heading_2", "heading_3", "heading_4"}
	for _, role := range roles {
		style, ok := profile.Styles[role]
		if !ok || style.Alignment != "center" {
			continue
		}
		target := role
		if role == "body" {
			target = "body_paragraph"
		}
		rules = append(rules, map[string]interface{}{
			"ruleId": "workflow-visual-alignment-" + role, "targetRole": target,
			"severity": "error", "property": "visual_horizontal_alignment",
			"expected":     map[string]interface{}{"offsetMm": 0},
			"tolerance":    map[string]interface{}{"offsetMm": 2},
			"verification": []string{"visual"},
		})
	}
	if margins, ok := workflowVisualMargins(profile.PageSetup); ok {
		for _, role := range roles {
			target := role
			if role == "body" {
				target = "body_paragraph"
			}
			rules = append(rules, map[string]interface{}{
				"ruleId": "workflow-content-boundary-" + role, "targetRole": target,
				"severity": "error", "property": "content_overflow",
				"expected":  map[string]interface{}{"marginsMm": margins},
				"tolerance": map[string]interface{}{}, "verification": []string{"visual"},
			})
		}
		for _, role := range []string{"table", "image"} {
			rules = append(rules, map[string]interface{}{
				"ruleId": "workflow-object-boundary-" + role, "targetRole": role,
				"severity": "error", "property": "table_image_boundary",
				"expected":  map[string]interface{}{"marginsMm": margins},
				"tolerance": map[string]interface{}{}, "verification": []string{"visual"},
			})
		}
	}
	if headerFooter, ok := workflowHeaderFooterVisualBounds(profile); ok {
		for role, expected := range headerFooter {
			rules = append(rules, map[string]interface{}{
				"ruleId": "workflow-" + role + "-position", "targetRole": role,
				"severity": "error", "property": "header_footer_position",
				"expected": expected, "tolerance": map[string]interface{}{}, "verification": []string{"visual"},
			})
		}
	}
	for _, captionRule := range []struct {
		role, objectRole, position string
	}{
		{"table_caption", "table", profile.RulePack.TableCaptionPosition},
		{"figure_caption", "image", profile.RulePack.FigureCaptionPosition},
	} {
		if captionRule.position != "above" && captionRule.position != "below" {
			continue
		}
		rules = append(rules, map[string]interface{}{
			"ruleId": "workflow-" + strings.ReplaceAll(captionRule.role, "_", "-") + "-position", "targetRole": captionRule.role,
			"severity": "error", "property": "caption_position",
			"expected":  map[string]interface{}{"position": captionRule.position, "relatedRole": captionRule.objectRole, "maxGapMm": 20, "minHorizontalOverlap": 0.6},
			"tolerance": map[string]interface{}{}, "verification": []string{"visual"},
		})
	}
	return map[string]interface{}{"specVersion": "1.0", "templateId": templateID, "rules": rules}
}

func workflowVisualMargins(page templateprofile.PageSetupRule) (map[string]interface{}, bool) {
	values := []struct {
		name string
		raw  string
	}{{"topMm", page.MarginTopTwips}, {"rightMm", page.MarginRightTwips}, {"bottomMm", page.MarginBottomTwips}, {"leftMm", page.MarginLeftTwips}}
	result := make(map[string]interface{}, len(values))
	for _, value := range values {
		twips, err := strconv.ParseFloat(strings.TrimSpace(value.raw), 64)
		if err != nil || twips <= 0 {
			return nil, false
		}
		result[value.name] = twips * 25.4 / 1440
	}
	return result, true
}

func workflowHeaderFooterVisualBounds(profile *templateprofile.Profile) (map[string]map[string]interface{}, bool) {
	if profile == nil {
		return nil, false
	}
	pageHeight, heightErr := strconv.ParseFloat(strings.TrimSpace(profile.PageSetup.PageHeightTwips), 64)
	header, headerErr := strconv.ParseFloat(strings.TrimSpace(profile.PageSetup.HeaderMarginTwips), 64)
	footer, footerErr := strconv.ParseFloat(strings.TrimSpace(profile.PageSetup.FooterMarginTwips), 64)
	if heightErr != nil || headerErr != nil || footerErr != nil || pageHeight <= 0 || header <= 0 || footer <= 0 {
		return nil, false
	}
	const twipToMM = 25.4 / 1440
	result := map[string]map[string]interface{}{}
	// A glyph's bounding box legitimately extends several millimetres from its
	// Word header/footer anchor. These bounds catch intrusion into the body,
	// not harmless renderer baseline differences.
	if profile.Header.Exists {
		result["header"] = map[string]interface{}{"maxBottomMm": header*twipToMM + 6}
	}
	if profile.Footer.Exists {
		result["footer"] = map[string]interface{}{"minTopMm": (pageHeight-footer)*twipToMM - 6}
	}
	return result, len(result) > 0
}

const defaultWorkflowOutputRoot = "uploads/workflow_outputs"
const defaultCQRWSTTemplatePath = "uploads/template.docx"
const cqrwstTemplatePathEnv = "CQRWST_TEMPLATE_PATH"
const cqrwstTemplateTransplantEnabledEnv = "CQRWST_TEMPLATE_TRANSPLANT_ENABLED"
const defaultPythonVisualTimeout = 15 * time.Minute

var renderedChineseTotalFooterPattern = regexp.MustCompile(`第(\d+)页共(\d+)页`)
var totalPagesFieldBlockPattern = regexp.MustCompile(`(?s)<w:r><w:fldChar\b[^>]*w:fldCharType="begin"[^>]*/></w:r>\s*<w:r><w:instrText\b[^>]*>\s*(?:NUMPAGES|SECTIONPAGES)\b.*?</w:instrText></w:r>\s*<w:r><w:fldChar\b[^>]*w:fldCharType="separate"[^>]*/></w:r>\s*<w:r><w:t\b[^>]*>.*?</w:t></w:r>\s*<w:r><w:fldChar\b[^>]*w:fldCharType="end"[^>]*/></w:r>`)
var materializedTotalPagePattern = regexp.MustCompile(`(?s)(共\s*</w:t></w:r>\s*<w:r><w:t\b[^>]*>)\d+(</w:t>)`)
var manualCaptionLinePattern = regexp.MustCompile(`^\s*([图表])\s*(\d+)(?:[-.．](\d+))?\s+(.+)$`)
var captionReferencePattern = regexp.MustCompile(`([图表])\s*(\d+)(?:[-.．](\d+))?`)
var continuedTableCaptionLinePattern = regexp.MustCompile(`^\s*\x{7eed}\x{8868}\s*(\d+)[-.\x{ff0e}](\d+)\s+(.+)$`)
var workflowParagraphPattern = regexp.MustCompile(`(?s)<w:p\b[^>]*>.*?</w:p>`)
var workflowTablePattern = regexp.MustCompile(`(?s)<w:tbl\b[^>]*>.*?</w:tbl>`)
var workflowRunPattern = regexp.MustCompile(`(?s)<w:r\b[^>]*>.*?</w:r>`)

// workflowPythonVisualTimeout leaves sufficient time for the first PaddleOCR
// execution, which may initialize models before it starts processing pages.
// Deployments with a known capacity can override it with a Go duration such as
// "20m" through PYTHON_VISUAL_TIMEOUT.
func workflowPythonVisualTimeout() time.Duration {
	if configured := strings.TrimSpace(os.Getenv("PYTHON_VISUAL_TIMEOUT")); configured != "" {
		if timeout, err := time.ParseDuration(configured); err == nil && timeout > 0 {
			return timeout
		}
		log.Printf("[PYTHON_VISUAL] invalid PYTHON_VISUAL_TIMEOUT=%q; using %s", configured, defaultPythonVisualTimeout)
	}
	return defaultPythonVisualTimeout
}

// ensureWorkflowTemplateBaseline keeps visual-template state independent from
// DeepSeek. Existing compiled templates predate the Python baseline store, so
// a 404 means the source DOCX must be registered once before visual checking.
func ensureWorkflowTemplateBaseline(ctx context.Context, pythonURL string, record model.CompiledTemplate, profile *templateprofile.Profile) error {
	if strings.TrimSpace(pythonURL) == "" {
		return nil
	}
	templatePath := strings.TrimSpace(record.SourceFilePath)
	if templatePath == "" {
		return fmt.Errorf("selected template has no DOCX source")
	}
	client := NewPythonVisualClient(pythonURL)
	if _, err := client.TemplateBaseline(ctx, record.TemplateName); err == nil {
		return nil
	} else if !isPythonVisualNotFound(err) {
		return err
	}
	metadata := map[string]interface{}{
		"templateId": record.TemplateName,
		"school":     record.SchoolID,
		"version":    record.TemplateVersion,
		"filename":   filepath.Base(templatePath),
	}
	spec := workflowVisualSpec(record.TemplateName, profile)
	log.Printf("[PYTHON_VISUAL] template baseline missing; registering selected template=%s rules=%d", record.TemplateName, workflowVisualRuleCount(spec))
	return client.RegisterTemplate(ctx, templatePath, record.TemplateName, metadata, spec)
}

var workflowTextRunPattern = regexp.MustCompile(`(?s)<w:t\b[^>]*>.*?</w:t>`)
var workflowTextValuePattern = regexp.MustCompile(`(?s)<w:t\b[^>]*>(.*?)</w:t>`)
var workflowBookmarkStartIDPattern = regexp.MustCompile(`<w:bookmarkStart\b[^>]*\bw:id="(\d+)"`)
var workflowBookmarkStartPattern = regexp.MustCompile(`<w:bookmarkStart\b[^>]*\bw:name="([^"]+)"[^>]*/>`)
var workflowAlternateContentPattern = regexp.MustCompile(`(?s)<mc:AlternateContent\b.*?</mc:AlternateContent>`)
var workflowDrawingExtentPattern = regexp.MustCompile(`<(?:wp:extent|a:ext)\b[^>]*\bcx="(\d+)"[^>]*\bcy="(\d+)"[^>]*/>`)
var renderedCurrentBodyPagePattern = regexp.MustCompile(`\x{7b2c}\s*(\d+)\s*\x{9875}\s*\x{5171}`)
var renderedHeadingPattern = regexp.MustCompile(`^(\d+(?:\.\d+){0,2})\s*(\S.*)$`)
var renderedHeadingNumberPattern = regexp.MustCompile(`^\d+(?:\.\d+){0,2}$`)
var renderedTOCEntryWithPagePattern = regexp.MustCompile(`\s[1-9]\d*$`)
var formulaNumberLinePattern = regexp.MustCompile(`^\s*[\(（](\d+)[-.．](\d+)[\)）]\s*$`)
var workflowTemplateAnchorPattern = regexp.MustCompile(`\{\{[a-z][a-z0-9_]*\}\}`)
var workflowReviewMarkupPattern = regexp.MustCompile(`<w:(?:ins|del|moveFrom|moveTo|commentRangeStart|commentReference)\b`)

type WorkflowJobView struct {
	ID                 uuid.UUID `json:"id"`
	PaperID            uuid.UUID `json:"paper_id"`
	UserID             uuid.UUID `json:"user_id"`
	CompiledTemplateID uuid.UUID `json:"compiled_template_id"`
	Status             string    `json:"status"`
	Stage              string    `json:"stage"`
	DownloadPath       string    `json:"download_path"`
	DownloadURL        string    `json:"download_url,omitempty"`
	DownloadReady      bool      `json:"download_ready"`
}

type CompiledTemplateView struct {
	ID              uuid.UUID `json:"id"`
	SchoolID        string    `json:"school_id"`
	TemplateName    string    `json:"template_name"`
	TemplateVersion string    `json:"template_version"`
	SkeletonPath    string    `json:"skeleton_path"`
	Status          string    `json:"status"`
}

type CompileTemplateInput struct {
	SchoolID     string
	TemplateName string
	Version      string
	FilePath     string
}

type CreatePaperJobInput struct {
	UserID           uuid.UUID
	FormatTemplateID uuid.UUID
	Title            string
	FilePath         string
	FileName         string
	FileSize         int64
	FileType         string
}

type PaperWorkflowService interface {
	CompileTemplate(ctx context.Context, input CompileTemplateInput) (*CompiledTemplateView, error)
	CreatePaperJob(ctx context.Context, input CreatePaperJobInput) (*WorkflowJobView, error)
	RunJob(ctx context.Context, id string, userID uuid.UUID) (*WorkflowJobView, error)
	GetJob(id string) (*WorkflowJobView, error)
	GetJobForUser(id string, userID uuid.UUID) (*WorkflowJobView, error)
}

type paperWorkflowService struct {
	db         *gorm.DB
	outputRoot string
	jobGates   map[string]*workflowJobGate
	jobGatesMu sync.Mutex
}

type workflowJobGate struct {
	mu   sync.Mutex
	refs int
}

func NewPaperWorkflowService(db *gorm.DB) PaperWorkflowService {
	return NewPaperWorkflowServiceWithOutputRoot(db, defaultWorkflowOutputRoot)
}

func NewPaperWorkflowServiceWithOutputRoot(db *gorm.DB, outputRoot string) PaperWorkflowService {
	if strings.TrimSpace(outputRoot) == "" {
		outputRoot = defaultWorkflowOutputRoot
	}
	return &paperWorkflowService{db: db, outputRoot: outputRoot, jobGates: make(map[string]*workflowJobGate)}
}

func workflowVisualDiagnosticPath(outputPath, sourcePath string) string {
	if info, err := os.Stat(outputPath); err == nil && !info.IsDir() && info.Size() > 0 {
		return outputPath
	}
	return sourcePath
}

// lockJob serializes runs of one job because they share final.docx and its
// rollback backup. Different jobs remain concurrent.
func (s *paperWorkflowService) lockJob(id string) func() {
	s.jobGatesMu.Lock()
	gate := s.jobGates[id]
	if gate == nil {
		gate = &workflowJobGate{}
		s.jobGates[id] = gate
	}
	gate.refs++
	s.jobGatesMu.Unlock()

	gate.mu.Lock()
	return func() {
		gate.mu.Unlock()
		s.jobGatesMu.Lock()
		gate.refs--
		if gate.refs == 0 {
			delete(s.jobGates, id)
		}
		s.jobGatesMu.Unlock()
	}
}

func (s *paperWorkflowService) CompileTemplate(ctx context.Context, input CompileTemplateInput) (*CompiledTemplateView, error) {
	if err := s.validateReady(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureWorkflowTables(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.FilePath) == "" {
		return nil, ErrInvalidPaperUpload
	}
	if strings.TrimSpace(input.SchoolID) == "" {
		input.SchoolID = "single-template"
	}
	if strings.TrimSpace(input.TemplateName) == "" {
		input.TemplateName = filepath.Base(input.FilePath)
	}
	if strings.TrimSpace(input.Version) == "" {
		input.Version = "runtime"
	}

	compiled, err := templatecompile.NewCompiler().Compile(ctx, input.FilePath, templatecompile.CompileOptions{
		SchoolID:     input.SchoolID,
		TemplateName: input.TemplateName,
		Version:      input.Version,
		OutputDir:    filepath.Join(s.outputRoot, "_compiled_templates"),
	})
	if err != nil {
		return nil, err
	}
	manifestJSON, err := workflowJSON(compiled.Manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal compiled template manifest: %w", err)
	}
	blockCatalogJSON, err := workflowJSON(compiled.BlockCatalog)
	if err != nil {
		return nil, fmt.Errorf("marshal compiled block catalog: %w", err)
	}
	styleProfilesJSON, err := workflowJSON(compiled.StyleProfiles)
	if err != nil {
		return nil, fmt.Errorf("marshal compiled style profiles: %w", err)
	}
	mappingContractJSON, err := workflowJSON(compiled.MappingContract)
	if err != nil {
		return nil, fmt.Errorf("marshal mapping contract: %w", err)
	}
	verificationRulesJSON, err := workflowJSON(compiled.VerificationRules)
	if err != nil {
		return nil, fmt.Errorf("marshal verification rules: %w", err)
	}
	patchTargetsJSON, err := workflowJSON(compiled.PatchTargets)
	if err != nil {
		return nil, fmt.Errorf("marshal patch targets: %w", err)
	}

	record := model.CompiledTemplate{
		ID:                    uuid.New(),
		SchoolID:              compiled.Manifest.SchoolID,
		TemplateName:          compiled.Manifest.TemplateName,
		TemplateVersion:       compiled.Manifest.Version,
		SourceFilePath:        input.FilePath,
		SkeletonPath:          compiled.SkeletonPath,
		ManifestJSON:          manifestJSON,
		BlockCatalogJSON:      blockCatalogJSON,
		StyleProfilesJSON:     styleProfilesJSON,
		MappingContractJSON:   mappingContractJSON,
		VerificationRulesJSON: verificationRulesJSON,
		PatchTargetsJSON:      patchTargetsJSON,
		Status:                "compiled",
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return nil, err
	}
	if pythonURL := PythonVisualServiceURL(); pythonURL != "" {
		// Registering here gives Python the same template key used by RunJob.
		// The deterministic Go compiler remains authoritative if Python is unavailable.
		metadata := map[string]interface{}{
			"templateId": record.TemplateName,
			"school":     record.SchoolID,
			"version":    record.TemplateVersion,
			"filename":   filepath.Base(input.FilePath),
		}
		profile := buildWorkflowTemplateProfile(ctx, input.FilePath)
		if err := NewPythonVisualClient(pythonURL).RegisterTemplate(ctx, input.FilePath, record.TemplateName, metadata, workflowVisualSpec(record.TemplateName, profile)); err != nil {
			log.Printf("[PYTHON_VISUAL] v2 template registration failed template=%s err=%v", record.TemplateName, err)
		} else {
			log.Printf("[PYTHON_VISUAL] v2 template registered template=%s", record.TemplateName)
		}
	}
	return &CompiledTemplateView{
		ID:              record.ID,
		SchoolID:        record.SchoolID,
		TemplateName:    record.TemplateName,
		TemplateVersion: record.TemplateVersion,
		SkeletonPath:    record.SkeletonPath,
		Status:          record.Status,
	}, nil
}

func (s *paperWorkflowService) CreatePaperJob(ctx context.Context, input CreatePaperJobInput) (*WorkflowJobView, error) {
	if err := s.validateReady(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureWorkflowTables(ctx); err != nil {
		return nil, err
	}
	input.FileType = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(input.FileType)), ".")
	if input.UserID == uuid.Nil || strings.TrimSpace(input.FilePath) == "" || input.FileType != "docx" {
		return nil, ErrInvalidPaperUpload
	}
	if strings.TrimSpace(input.FileName) == "" {
		input.FileName = filepath.Base(input.FilePath)
	}
	if strings.TrimSpace(input.Title) == "" {
		input.Title = input.FileName
	}

	paperID := uuid.New()
	templateID := uuid.New()
	jobID := uuid.New()
	var selectedTemplateID *uuid.UUID
	templateName := "single-template-runtime"
	templateVersion := "runtime"
	schoolID := "single-template"
	compiledSource := input.FilePath
	var profile *templateprofile.Profile

	if input.FormatTemplateID != uuid.Nil {
		var formatTemplate model.FormatTemplate
		if err := s.db.WithContext(ctx).Preload("University").First(&formatTemplate, "id = ? AND is_active = ?", input.FormatTemplateID, true).Error; err != nil {
			return nil, err
		}
		selectedTemplateID = &formatTemplate.ID
		templateName = formatTemplate.Name
		templateVersion = formatTemplate.Version
		schoolID = formatTemplate.TemplateID
		templatePath := WorkflowTemplatePath(formatTemplate)
		if templatePath == "" {
			log.Printf("[WORKFLOW_TEMPLATE] template %s has no readable DOCX; rejecting paper job", formatTemplate.ID)
			return nil, ErrTemplateDOCXMissing
		}
		compiledSource = templatePath

		var extractErr error
		profile, extractErr = templateprofile.Extract(templatePath)
		if extractErr != nil {
			return nil, fmt.Errorf("extract selected template DOCX: %w", extractErr)
		}

	} else {
		// No selected template: create the job, but RunJob will stop at manual
		// review. Never synthesize a school profile or use hardcoded defaults.
		profile = nil
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		paper := model.Paper{
			ID:                    paperID,
			UserID:                input.UserID,
			Title:                 input.Title,
			FilePath:              input.FilePath,
			FileName:              input.FileName,
			FileSize:              input.FileSize,
			FileType:              input.FileType,
			ParsedInfo:            "{}",
			AutoDetectedTemplates: "[]",
			Status:                string(workflow.StatusUploaded),
			SelectedTemplateID:    selectedTemplateID,
		}
		if err := tx.Create(&paper).Error; err != nil {
			return err
		}

		compiled := model.CompiledTemplate{
			ID:                    templateID,
			SchoolID:              schoolID,
			TemplateName:          templateName,
			TemplateVersion:       templateVersion,
			SourceFilePath:        compiledSource,
			SkeletonPath:          compiledSource,
			ManifestJSON:          "{}",
			BlockCatalogJSON:      "[]",
			StyleProfilesJSON:     templateprofile.Marshal(profile),
			MappingContractJSON:   "{}",
			VerificationRulesJSON: "{}",
			PatchTargetsJSON:      "[]",
			Status:                "compiled",
		}
		if err := tx.Create(&compiled).Error; err != nil {
			return err
		}

		job := model.PaperWorkflowJob{
			ID:                 jobID,
			PaperID:            paperID,
			UserID:             input.UserID,
			CompiledTemplateID: templateID,
			Status:             string(workflow.StatusUploaded),
			Stage:              "queued",
			VerifyResultJSON:   "{}",
		}
		return tx.Create(&job).Error
	})
	if err != nil {
		return nil, err
	}

	return s.GetJobForUser(jobID.String(), input.UserID)
}

func (s *paperWorkflowService) RunJob(ctx context.Context, id string, userID uuid.UUID) (view *WorkflowJobView, retErr error) {
	outputPath := ""
	pythonVisualURL := PythonVisualServiceURL()
	var visual *PythonVisualReport
	var visualSHA string
	if pythonVisualURL != "" {
		log.Printf("[PYTHON_VISUAL] v2 workflow enabled url=%s job=%s", pythonVisualURL, id)
	}
	// ── 阶段 0：初始化诊断日志（可选，通过环境变量 PAPER_DIAG_LOG_PATH 控制） ──
	// 诊断日志用于记录工作流全过程的详细信息，便于调试和问题追溯
	if diagnosticPath := strings.TrimSpace(os.Getenv("PAPER_DIAG_LOG_PATH")); diagnosticPath != "" {
		if err := fileprocessor.InitDiagLog(diagnosticPath); err != nil {
			log.Printf("[DIAG] 初始化诊断日志失败（继续执行）: %v", err) // 初始化失败不阻塞主流程
		} else {
			defer fileprocessor.CloseDiagLog() // 确保函数退出时关闭日志文件
		}
	}

	// ── 阶段 0.5：前置校验 ──
	// 校验上下文有效性、服务实例可用性、数据库表是否存在
	if err := s.validateReady(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureWorkflowTables(ctx); err != nil {
		return nil, err
	}
	// 解析前端传入的任务 ID 为 UUID
	jobID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJobID, err)
	}
	unlock := s.lockJob(jobID.String())
	defer unlock()

	// ── 阶段 1：加载任务数据 ──
	// 从数据库加载工作流任务，并预加载关联的论文和编译模板记录
	var job model.PaperWorkflowJob
	if err := s.db.WithContext(ctx).Preload("Paper").Preload("CompiledTemplate").First(&job, "id = ? AND user_id = ?", jobID, userID).Error; err != nil {
		return nil, err
	}
	log.Printf("[WORKFLOW_FLOW] job loaded job=%s paper=%s template=%s input=%s", job.ID, job.PaperID, job.CompiledTemplateID, job.Paper.FilePath)
	// 论文文件路径为空 → 用户未上传文件，拒绝执行
	if strings.TrimSpace(job.Paper.FilePath) == "" {
		return nil, ErrInvalidPaperUpload
	}

	// ── 阶段 1.5：创建格式运行日志（format run log） ──
	// 每次 RunJob 调用都生成独立的诊断日志，记录格式化全过程的输入输出
	runCtx, runLogPath, runLogErr := fileprocessor.BeginFormatRunLog(
		ctx, job.Paper.FilePath, job.CompiledTemplate.SourceFilePath, job.ID.String(),
	)
	if runLogErr != nil {
		// 格式日志创建失败是硬错误，因为后续步骤依赖日志记录
		return nil, fmt.Errorf("create required format diagnostic log: %w", runLogErr)
	}
	ctx = runCtx   // 将日志注入的上下文替换原 ctx
	defer func() { // defer 确保日志在函数结束时正确关闭
		if recovered := recover(); recovered != nil { // 捕获 panic，记录到日志后重新抛出
			fileprocessor.FinishFormatRunLog(ctx, fmt.Errorf("panic: %v", recovered))
			panic(recovered)
		}
		fileprocessor.FinishFormatRunLog(ctx, retErr) // 正常/错误返回都写入日志
	}()
	log.Printf("[格式诊断日志] job=%s path=%s", job.ID, runLogPath)
	pythonVisualDiagnosticAttempted := false
	var visualProfile *templateprofile.Profile
	structuredComplianceLogged := false
	defer func() {
		if retErr != nil && !structuredComplianceLogged {
			log.Printf("[STRUCTURED_COMPLIANCE] skipped job=%s reason=workflow_failed_before_final_compliance err=%v", job.ID, retErr)
		}
	}()
	defer func() {
		if retErr == nil || pythonVisualDiagnosticAttempted || pythonVisualURL == "" {
			return
		}
		diagnosticPath := workflowVisualDiagnosticPath(outputPath, job.Paper.FilePath)
		log.Printf("[PYTHON_VISUAL] failure diagnostic request job=%s file=%s reason=%v", job.ID, diagnosticPath, retErr)
		visualCtx, cancel := context.WithTimeout(context.Background(), workflowPythonVisualTimeout())
		_, visualErr := NewPythonVisualClient(pythonVisualURL).CheckStudentPaperWithContext(
			visualCtx, diagnosticPath, job.CompiledTemplate.TemplateName, 1, job.ID.String()+"-failure",
			workflowVisualSpec(job.CompiledTemplate.TemplateName, visualProfile), nil,
		)
		cancel()
		if visualErr != nil {
			log.Printf("[PYTHON_VISUAL] failure diagnostic failed job=%s err=%v", job.ID, visualErr)
		} else {
			log.Printf("[PYTHON_VISUAL] failure diagnostic completed job=%s", job.ID)
		}
	}()
	fileprocessor.FormatLogSection(ctx, "真实 POST /api/v2/jobs/:job_id/run 工作流入口")
	fileprocessor.FormatLogPrintf(ctx, "任务编号：%s", job.ID)
	fileprocessor.FormatLogPrintf(ctx, "学生论文：%s", job.Paper.FilePath)
	fileprocessor.FormatLogPrintf(ctx, "学校模板：%s", job.CompiledTemplate.SourceFilePath)

	// ── 阶段 2：确定输出路径并解析模板 Profile ──
	outputPath, err = s.workflowOutputPath(job.ID)
	if err != nil {
		return nil, err
	}
	fileprocessor.FormatLogPrintf(ctx, "输出文件：%s", outputPath)
	// Always use the selected source DOCX; persisted profiles are diagnostic data.
	profile, err := templateprofile.Extract(job.CompiledTemplate.SourceFilePath)
	if err != nil {
		return nil, fmt.Errorf("extract selected template DOCX: %w", err)
	}
	fileprocessor.FormatLogPrintf(ctx, "模板规则直接读取所选 DOCX：%s", job.CompiledTemplate.SourceFilePath)
	visualProfile = profile
	fileprocessor.FormatLogProfile(ctx, profile, "第1步：学校模板 Profile")
	fileprocessor.DiagPrintf("========== [Node 1] Template Profile Parsed ==========\n")
	if profile != nil {
		if data, err := json.MarshalIndent(profile, "", "  "); err == nil {
			fileprocessor.DiagPrintf("%s\n", string(data))
		}
	}

	// ── 阶段 3：学生论文结构识别 ──
	// AST 提取：将学生论文解析为抽象语法树（段落类型、层级、样式等）

	ast, err := paperast.Extract(job.Paper.FilePath)

	if err != nil {
		return nil, err
	}
	// Role recognition intentionally precedes all compliance/format decisions.
	// A malformed heading must remain a heading so the later format plan can
	// repair it; do not infer a role from the student's current font or size.
	// P0: DeepSeek classifies semantic roles by stable paraId/NodeID. The model
	// returns candidates only; deterministic state/format rules remain the gate.
	var roleAssignments []roleclassify.Assignment
	var roleFormatPlan []templateapply.FormatPlanItem
	if client := newDeepSeekSemanticBlockClient(); client != nil {
		rolePDFEvidence := map[string][]evidence.Item{}
		if pythonVisualURL != "" && strings.TrimSpace(job.CompiledTemplate.TemplateName) != "" {
			visualClient := NewPythonVisualClient(pythonVisualURL)
			evidencePath, prepareErr := prepareRoleEvidenceDOCX(job.Paper.FilePath)
			if prepareErr != nil {
				log.Printf("[ROLE_PDF_EVIDENCE] skipped job=%s reason=prepare_copy err=%v", job.ID, prepareErr)
				fileprocessor.FormatLogPrintf(ctx, "Role PDF evidence skipped: prepare renderable copy: %v", prepareErr)
			} else {
				mapped, evidenceErr := visualClient.CollectStudentPDFEvidence(ctx, evidencePath, job.CompiledTemplate.TemplateName, 1, job.ID.String(), ast)
				if evidenceErr != nil {
					log.Printf("[ROLE_PDF_EVIDENCE] skipped job=%s reason=python_service err=%v", job.ID, evidenceErr)
					fileprocessor.FormatLogPrintf(ctx, "Role PDF evidence skipped: %v", evidenceErr)
				} else {
					rolePDFEvidence = mapped
					log.Printf("[ROLE_PDF_EVIDENCE] mapped job=%s nodes=%d items=%d", job.ID, len(rolePDFEvidence), evidenceItemCount(rolePDFEvidence))
					fileprocessor.FormatLogPrintf(ctx, "Role PDF evidence mapped nodes=%d", len(rolePDFEvidence))
				}
			}
		}
		candidates, classifyErr := roleclassify.ClassifyStructuredWithEvidence(ctx, client, ast.Nodes, rolePDFEvidence)
		if classifyErr != nil {
			// 非致命 LLM 失败（如 SSE 空响应/超时）已完成 per-node 降级：
			// 失败节点保留确定性角色，其余成功候选仍继续合并，而非整批丢弃。
			fileprocessor.FormatLogPrintf(ctx, "DeepSeek 段落角色分类部分失败 %v；保留失败节点确定性分类，继续合并成功候选", classifyErr)
		}
		ast.Nodes = roleclassify.Merge(ast.Nodes, candidates, 0.85)
		for _, candidate := range candidates {
			if candidate.Confidence < 0.85 || candidate.Role == "unknown" {
				fileprocessor.FormatLogPrintf(ctx, "低置信度角色：node=%s role=%s confidence=%.2f evidence=%s",
					candidate.NodeID, candidate.Role, candidate.Confidence, strings.Join(candidate.Evidence, "; "))
			}
		}
		if data, marshalErr := json.Marshal(candidates); marshalErr == nil {
			fileprocessor.FormatLogPrintf(ctx, "DeepSeek 角色分类结果（稳定 NodeID）：%s", string(data))
		}
	}
	ast.Nodes = roleclassify.EnforceDocumentTree(ast.Nodes)
	// Freeze exactly one semantic plan after deterministic/DeepSeek arbitration.
	// Formatting code consumes this plan and must not reclassify paragraphs.
	roleAssignments = roleclassify.Freeze(ast.Nodes)
	if data, marshalErr := json.Marshal(roleAssignments); marshalErr == nil {
		log.Printf("[STRUCTURED_ROLE_PLAN] job=%s nodes=%d decisions=%s", job.ID, len(roleAssignments), string(data))
		fileprocessor.FormatLogPrintf(ctx, "结构化角色计划：%s", string(data))
	}
	// OOXML 结构快照：捕获书签、域、公式、图片等受保护对象清单
	sourceStructure, err := repaircontract.CaptureStructure(job.Paper.FilePath)

	if err != nil {
		return nil, fmt.Errorf("capture source OOXML structure: %w", err)
	}
	fileprocessor.FormatLogSection(ctx, "第2步：学生论文结构识别")
	fileprocessor.FormatLogPrintf(ctx, "段落=%d；空段落=%d；表格=%d；标题=%d",
		ast.Stats.Paragraphs, ast.Stats.BlankParagraphs, ast.Stats.Tables, ast.Stats.Headings)
	for _, node := range ast.Nodes {
		if node.NodeType == "paragraph" {
			fileprocessor.FormatLogPrintf(ctx, "段落 #%04d -> %s（层级=%d，置信度=%.2f，样式=%s）| %q",
				node.Index+1, node.SemanticRole, node.LogicalLevel, node.Confidence,
				node.CurrentStyle, compactWorkflowLogText(node.Text, 160))
		}
	}

	// ── 阶段 4：构建模板规则与修复合同 ──
	// rules：从 Profile 推导出的格式规则集（样式映射、区段规则）
	rules := templatecontract.Build(profile)
	if err := ensureWorkflowTemplateBaseline(ctx, pythonVisualURL, job.CompiledTemplate, profile); err != nil {
		log.Printf("[PYTHON_VISUAL] template baseline unavailable template=%s err=%v", job.CompiledTemplate.TemplateName, err)
		fileprocessor.FormatLogPrintf(ctx, "Python 模板视觉基线不可用: %v", err)
	}
	// The LLM may only compile bounded, per-role proposals from typed template
	// evidence. Deterministic OOXML rules remain authoritative: conflicts are
	// recorded for review and never overwrite them.
	if client := newDeepSeekSemanticBlockClient(); client != nil {
		templatePath := strings.TrimSpace(job.CompiledTemplate.SourceFilePath)
		if templatePath == "" {
			fileprocessor.FormatLogPrintf(ctx, "Structured rule compiler skipped: selected template has no DOCX source")
		} else if templateAST, templateErr := paperast.Extract(templatePath); templateErr != nil {
			fileprocessor.FormatLogPrintf(ctx, "Structured rule compiler skipped: extract template AST failed: %v", templateErr)
		} else {
			var outcomes []templatecontract.CandidateOutcome
			pdfEvidence, pdfErr := NewPythonVisualClient(pythonVisualURL).TemplatePDFEvidence(ctx, job.CompiledTemplate.TemplateName, templateAST.Nodes)
			if pdfErr != nil {
				fileprocessor.FormatLogPrintf(ctx, "Structured rule compiler: template PDF evidence unavailable: %v", pdfErr)
			}
			rules, outcomes = templatecontract.CompileStructuredCandidates(ctx, client, job.CompiledTemplate.TemplateName, templateAST, rules, pdfEvidence)
			applied, conflicts, rejected := 0, 0, 0
			for _, outcome := range outcomes {
				if outcome.Applied {
					applied++
				}
				if strings.HasPrefix(outcome.Reason, "conflict:") {
					conflicts++
				}
				if strings.HasPrefix(outcome.Reason, "rejected:") {
					rejected++
				}
			}
			log.Printf("[STRUCTURED_RULE_COMPILER] job=%s candidates=%d applied=%d conflicts=%d rejected=%d", job.ID, len(outcomes), applied, conflicts, rejected)
			if data, marshalErr := json.Marshal(outcomes); marshalErr == nil {
				fileprocessor.FormatLogPrintf(ctx, "Structured rule candidates (bounded evidence): %s", string(data))
			}
		}
	}
	// Compile first: the writer and verifier consume the same completed rules.
	profile = templatecontract.ExecutionProfile(profile, rules)
	visualProfile = profile
	roleFormatPlan = templateapply.BuildRoleFormatPlan(profile, roleAssignments)
	for _, item := range roleFormatPlan {
		if !roleFormatPlanRequiresReview(item) {
			continue
		}
		fileprocessor.FormatLogPrintf(ctx, "FormatPlan review: node=%s role=%s confidence=%.2f apply=%t reason=%s evidence=%s",
			item.NodeID, item.Role, item.Confidence, item.Apply, item.Reason, strings.Join(item.Evidence, "; "))
	}
	if data, marshalErr := json.Marshal(roleFormatPlan); marshalErr == nil {
		fileprocessor.FormatLogPrintf(ctx, "冻结后的模板 Profile FormatPlan：%s", string(data))
	}
	// contract：修复合同，定义哪些段落需要修复、修复步骤是什么
	contract := repaircontract.Build(rules, ast)
	fileprocessor.FormatLogSection(ctx, "第3步：模板规则与修复合同")
	fileprocessor.FormatLogPrintf(ctx, "模板样式=%d；区段规则=%d；修复步骤=%d；受保护内容节点=%d",
		len(rules.Styles), len(rules.Sections), len(contract.Steps), len(contract.Targets))

	// ── 阶段 5：分支决策 ──
	// 判断是否可以走模板移植路径（分支 A）：
	//   1. templateTransplantEnabled：检查模板是否支持移植（有 CQRWST 规范化器或映射锚点）
	//   2. structureRequiresInPlaceRepair：检查学生论文是否含受保护对象（书签/域/公式/图片等）
	//      如果学生论文含受保护对象，即使模板支持移植也必须降级为就地修复，
	//      因为模板骨架重建会丢失书签、域代码、公式等 OOXML 精细结构
	// Strict mode has one output authority: the selected template skeleton.
	// Never silently fall back to in-place repair, because that is the path
	// where later format passes can overwrite a previously corrected layout.
	templateCanTransplant := templateTransplantEnabled(job.CompiledTemplate.SourceFilePath, profile)
	requiresInPlaceRepair := structureRequiresInPlaceRepair(sourceStructure)
	strictTransplantOnly := workflowStrictTemplateTransplantEnabled()
	transplantEnabled := templateCanTransplant && !requiresInPlaceRepair
	if strictTransplantOnly && !templateCanTransplant {
		fileprocessor.FormatLogPrintf(ctx, "严格模板移植不可用：模板缺少受控槽位；自动使用内容保全修复链路，不中断任务。")
	}
	if strictTransplantOnly && requiresInPlaceRepair {
		fileprocessor.FormatLogPrintf(ctx, "严格模板移植暂不承载图片、书签、超链接或域；自动使用内容保全修复链路，不中断任务。")
	}
	if !transplantEnabled {
		fileprocessor.FormatLogPrintf(ctx, "严格模板移植未启用或当前文档无法安全迁移；保留兼容就地修复路径。启用 PAPER_TEMPLATE_TRANSPLANT_ONLY=1 后将拒绝回退。")
	}
	// ── 阶段 6：执行模板移植（核心步骤）──
	// 根据 transplantEnabled 走分支 A（模板骨架重建）或分支 B（就地修复）
	// 产物：outputPath 指向的 final.docx
	fileprocessor.FormatLogPrintf(ctx, "模板移植决策：模板支持=%t；受保护结构需就地修复=%t；最终使用移植=%t；书签=%d；域=%d；公式=%d；图片/绘图=%d；批注引用=%d",
		templateCanTransplant, requiresInPlaceRepair, transplantEnabled,
		len(sourceStructure.Bookmarks), len(sourceStructure.Fields), sourceStructure.Formulas,
		sourceStructure.Drawings+sourceStructure.Pictures, sourceStructure.CommentReferences)
	profile, err = s.buildWorkflowOutput(ctx, job.Paper.FilePath, outputPath, job.CompiledTemplate, profile, transplantEnabled)
	if err != nil {
		log.Printf("[WORKFLOW_FLOW] Go format stage failed job=%s err=%v", job.ID, err)
		fileprocessor.FormatLogPrintf(ctx, "Go 格式修复阶段失败：%v", err)
		// Keep the visual pipeline observable even when the deterministic OOXML
		// repair gate rejects the generated output. This is diagnostic-only: the
		// job still returns the original Go repair error and is not downloadable.
		if pythonVisualURL != "" {
			diagnosticPath := workflowVisualDiagnosticPath(outputPath, job.Paper.FilePath)
			log.Printf("[PYTHON_VISUAL] fallback diagnostic request job=%s file=%s reason=Go format stage failed", job.ID, diagnosticPath)
			visualCtx, cancel := context.WithTimeout(ctx, workflowPythonVisualTimeout())
			_, visualErr := NewPythonVisualClient(pythonVisualURL).CheckStudentPaperWithContext(
				visualCtx, diagnosticPath, job.CompiledTemplate.TemplateName, 1, job.ID.String()+"-go-failure",
				workflowVisualSpec(job.CompiledTemplate.TemplateName, profile), nil,
			)
			cancel()
			if visualErr != nil {
				log.Printf("[PYTHON_VISUAL] fallback diagnostic failed job=%s err=%v", job.ID, visualErr)
			} else {
				log.Printf("[PYTHON_VISUAL] fallback diagnostic completed job=%s", job.ID)
			}
		} else {
			log.Printf("[PYTHON_VISUAL] fallback diagnostic skipped job=%s reason=PYTHON_SERVICE_URL is empty", job.ID)
		}
		return nil, err
	}
	// ── 阶段 7：持久化中间产物 ──
	// 将 Profile、Rules、AST、Contract 写入数据库，供前端查询展示
	if err := s.persistWorkflowContracts(ctx, job, profile, rules, ast, contract); err != nil {
		return nil, err
	}

	// ── 阶段 8：分支 B 专属后处理 ──
	// Profile 为 nil：无模板 Profile 可参照，标记为需人工审核并直接返回
	if profile == nil {
		result := verify.Result{
			Passed:           false,
			ComplianceStatus: "review_required",
			ComplianceReason: "no template profile is configured for this workflow; school-specific compliance cannot be proven",
			Warnings: []verify.Issue{{
				Kind:     "missing_template_profile",
				Severity: "warning",
				Message:  "upload or configure a school template before claiming format compliance",
				Target:   job.Paper.FilePath,
			}},
		}
		if err := workflow.NewStore(s.db).UpdateJobResult(ctx, job.ID, workflow.StatusManualReview, workflow.StageManualReview, outputPath, result); err != nil {
			return nil, err
		}
		fileprocessor.SetFormatRunLogResult(ctx, string(workflow.StatusManualReview), string(workflow.StageManualReview))
		return s.GetJobForUser(id, userID)
	}

	// ── 阶段 8.5：内容保全备份 ──
	// 如果修复合同涉及"可见内容重写"，在执行后续修复前先备份输出文件
	// 用于修复失败时回滚，确保学生原文内容不丢失
	contractBackup := outputPath + ".contract-backup"
	if !transplantEnabled && contract.Blocks("visible_content_rewrite") {
		if err := copyFile(outputPath, contractBackup); err != nil {
			return nil, err
		}
		defer os.Remove(contractBackup) // 函数正常结束时清理备份
	}

	// Page setup is independent from paragraph semantics. Paragraph properties
	// have exactly one writer below: the frozen role plan.
	if !transplantEnabled {
		pageChanges, applyErr := templateapply.ApplyTemplateProfilePageSetup(ctx, outputPath, profile)
		if applyErr != nil {
			return nil, fmt.Errorf("apply selected template page setup: %w", applyErr)
		}
		fileprocessor.FormatLogPrintf(ctx, "模板页面设置：实际修改=%d", pageChanges)
		headerChanges, applyErr := templateapply.ApplyTemplateProfileHeaderFormatting(ctx, outputPath, profile)
		if applyErr != nil {
			return nil, fmt.Errorf("apply selected template header formatting: %w", applyErr)
		}
		fileprocessor.FormatLogPrintf(ctx, "模板页眉格式：实际修改=%d", headerChanges)
	}

	// ── 阶段 10：OOXML 最终清理 ──
	// 归一化：移除多余的 XML 命名空间、空白节点等冗余部件
	// Apply the frozen semantic plan after the single profile baseline pass.
	// No later document-wide formatter runs after this point.
	if !transplantEnabled && len(roleAssignments) > 0 {
		applied, planErr := templateapply.ApplyRoleFormatPlan(ctx, outputPath, profile, roleAssignments)
		if planErr != nil {
			return nil, fmt.Errorf("apply stable-node role format plan: %w", planErr)
		}
		fileprocessor.FormatLogPrintf(ctx, "stable-node FormatPlan: candidates=%d applied=%d; unknown/low-confidence nodes preserved",
			len(roleAssignments), applied)
		pending, checkErr := templateapply.CheckRoleFormatPlan(ctx, outputPath, profile, roleAssignments)
		if checkErr != nil {
			return nil, fmt.Errorf("check stable-node role format plan: %w", checkErr)
		}
		if pending != 0 {
			return nil, fmt.Errorf("stable-node role format plan is not idempotent: %d paragraph(s) still differ", pending)
		}
		fileprocessor.FormatLogPrintf(ctx, "stable-node FormatPlan 幂等复检：通过；再次应用修改=0")
	}

	writtenAST, err := paperast.Extract(outputPath)
	if err != nil {
		return nil, fmt.Errorf("capture post-write rule observations: %w", err)
	}

	normalized, err := transplant.NormalizeFinalDOCX(outputPath)
	if err != nil {
		return nil, fmt.Errorf("normalize final OOXML: %w", err)
	}
	// 属性顺序修复：确保 OOXML 元素属性按规范顺序排列（兼容不同解析器的序列化差异）
	coverNormalized, err := templateapply.NormalizeCoverTitleFormat(ctx, outputPath)
	if err != nil {
		return nil, fmt.Errorf("normalize cover title format: %w", err)
	}
	reordered, err := ooxmlpkg.RepairPropertyOrder(outputPath)
	if err != nil {
		return nil, fmt.Errorf("repair final OOXML property order: %w", err)
	}
	fileprocessor.FormatLogPrintf(ctx, "最终 OOXML 清理：归一化部件=%d；封面规范化=%d；属性顺序修复=%d。", normalized, coverNormalized, reordered)

	// ── 阶段 11：确定性验证 ──
	if err := fileprocessor.RepairRunningHeaders(outputPath); err != nil {
		return nil, fmt.Errorf("resolve running-header styles: %w", err)
	}
	// 构建验证器：基于模板 Profile、规则集、AST、修复合同
	verifier := verify.NewVerifierWithTemplateProfileAndClosure(profile, rules, ast, contract)
	// The role plan is applied and re-read by stable NodeID below.  Its exact
	// per-node validation is authoritative; the legacy whole-document profile
	// heuristic otherwise reclassifies untouched cover fields and creates a
	// false template_profile_rule failure.
	if transplantEnabled || len(roleAssignments) > 0 {
		verifier.WithoutTemplateProfileCheck()
	}
	// 分支 A（模板移植）不需要 CQRWST 规则验证（因为走的是骨架重建，规则已经内置）
	if transplantEnabled {
		verifier.WithoutSchoolSpecificRules()
	}
	// LoopController 执行验证循环：渲染 → 检查 → 修复 → 再验证，直到通过或达到上限
	result, err := workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
	if err != nil {
		return nil, err
	}
	fileprocessor.FormatLogSection(ctx, "第5步：确定性验证")
	logWorkflowVerifyResult(ctx, "首次验证", result.VerifyResult)

	// ── 阶段 12：修复验证中发现的特定问题 ──
	// 12a：修复手动题注编号（如"图1-1"、"表2-3"等）
	if !transplantEnabled {
		if repaired, repairErr := repairManualCaptionFields(outputPath, result.VerifyResult); repairErr != nil {
			return nil, repairErr
		} else if repaired {
			// 修复后需要重新验证，确保修复没有引入新问题
			result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
			if err != nil {
				return nil, err
			}
		}
	}
	// 12b：修复手动公式编号（如"(1-1)"、"(2-3)"等）
	if repaired, repairErr := repairManualFormulaNumberFields(outputPath, result.VerifyResult); repairErr != nil {
		return nil, repairErr
	} else if repaired {
		result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
		if err != nil {
			return nil, err
		}
	}
	// 12c：修复手动交叉引用（如"见第X章"、"如表X所示"等）
	if repaired, repairErr := repairManualCrossReferenceFields(outputPath, result.VerifyResult); repairErr != nil {
		return nil, repairErr
	} else if repaired {
		result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
		if err != nil {
			return nil, err
		}
	}

	// ── 阶段 13：验证不通过时的兜底修复（仅分支 B） ──
	// 首次验证未通过 + 非移植路径 → 再次执行 CQRWST 完整修复
	// 这是二阶修复：第一次修复在阶段 9，这里是验证不通过后的补修

	// ── 阶段 14：内容保全验证 ──
	// 当修复合同标记了"visible_content_rewrite"时，校验修复是否意外删改学生原文内容
	if transplantEnabled {
		sourceParsed, parseErr := paperparse.NewParser().Parse(ctx, job.Paper.FilePath)
		if parseErr != nil {
			return nil, fmt.Errorf("parse source content for template transplant gate: %w", parseErr)
		}
		if missing := missingGeneratedSourceContent(ctx, sourceParsed, outputPath); len(missing) > 0 {
			return nil, fmt.Errorf("template transplant content gate failed: %s", missing[0])
		}
		fileprocessor.FormatLogPrintf(ctx, "模板移植内容门禁：通过；学生正文已迁移到模板骨架。")
	} else if contract.Blocks("visible_content_rewrite") {
		// 对输出文件重新做 AST 提取
		finalAST, extractErr := paperast.Extract(outputPath)
		if extractErr != nil {
			// 提取失败时尝试从备份恢复
			if restoreErr := copyFile(contractBackup, outputPath); restoreErr != nil {
				return nil, fmt.Errorf("extract final paper: %v; restore backup: %w", extractErr, restoreErr)
			}
			return nil, extractErr
		}
		// 对比原始 AST 与最终 AST，检查可见内容是否被删除或改写
		contentIssues := repaircontract.ValidateVisibleContentPreserved(ast, finalAST)
		if transplantEnabled {
			// 分支 A 额外检查：确保模板自身内容也未丢失
			templateAST, templateErr := paperast.Extract(job.CompiledTemplate.SourceFilePath)
			if templateErr != nil {
				return nil, fmt.Errorf("extract selected template content contract: %w", templateErr)
			}
			contentIssues = repaircontract.ValidateVisibleContentPreservedWithTemplate(ast, finalAST, templateAST)
		}
		if len(contentIssues) > 0 {
			// 内容不完整 → 从备份恢复输出文件，并报错
			if restoreErr := copyFile(contractBackup, outputPath); restoreErr != nil {
				return nil, fmt.Errorf("repair contract violation: %s; restore backup: %w", contentIssues[0].Message, restoreErr)
			}
			return nil, fmt.Errorf("repair contract violation: %s", contentIssues[0].Message)
		}
		fileprocessor.FormatLogPrintf(ctx, "内容保全检查：通过；输出可见内容未删除、重排或改写。")
	}

	// ── 阶段 15：OOXML 结构保全验证 ──
	// 对比修复前后 OOXML 结构快照，确保节、书签等受保护对象未减少
	var structureErr error
	if !transplantEnabled {
		finalStructure, captureErr := repaircontract.CaptureStructure(outputPath)
		structureErr = captureErr
		if structureErr == nil {
			if issues := repaircontract.ValidateStructurePreserved(sourceStructure, finalStructure); len(issues) > 0 {
				structureErr = fmt.Errorf("repair contract violation: %s", issues[0].Message)
			}
		}
	}
	if structureErr != nil {
		// 结构不完整 → 尝试从备份恢复
		if _, statErr := os.Stat(contractBackup); statErr == nil {
			if restoreErr := copyFile(contractBackup, outputPath); restoreErr != nil {
				return nil, fmt.Errorf("%v; restore backup: %w", structureErr, restoreErr)
			}
		}
		return nil, structureErr
	}
	fileprocessor.FormatLogPrintf(ctx, "OOXML 结构保全检查：通过；节和受保护对象均未减少。")
	if len(roleAssignments) > 0 {
		finalAST, astErr := paperast.Extract(outputPath)
		if astErr != nil {
			return nil, fmt.Errorf("extract final AST for role validation: %w", astErr)
		}
		roleIssues := roleclassify.Validate(finalAST.Nodes, roleAssignments)
		for _, issue := range roleIssues {
			// Missing paraId after a legacy OOXML serializer is an advisory
			// mapping issue, not proof that visible content or structure was lost.
			// The independent structure/content gates remain blocking.
			result.VerifyResult.Warnings = append(result.VerifyResult.Warnings, verify.Issue{
				Kind: "role_node_validation", Severity: "warning", Message: issue.Message, Target: issue.NodeID,
			})
		}
		fileprocessor.FormatLogPrintf(ctx, "稳定 NodeID 逐节点验证：计划=%d；错误=%d", len(roleAssignments), len(roleIssues))
	}

	if len(roleAssignments) > 0 {
		formatIssues, formatErr := templateapply.ValidateRoleFormatPlan(ctx, outputPath, profile, roleAssignments)
		if formatErr != nil {
			return nil, fmt.Errorf("validate final role format plan: %w", formatErr)
		}
		for _, issue := range formatIssues {
			result.VerifyResult.RepairableIssues = append(result.VerifyResult.RepairableIssues, verify.Issue{
				Kind: "role_format_validation", Severity: "error",
				Message: fmt.Sprintf("%s expected %s, actual %s", issue.Property, issue.Expected, issue.Actual), Target: issue.NodeID,
			})
		}
		if len(formatIssues) > 0 {
			result.Status = workflow.StatusManualReview
			result.VerifyResult.Passed = false
			result.VerifyResult.ComplianceStatus = "review_required"
			result.VerifyResult.ComplianceReason = "one or more stable-node template properties did not survive final serialization"
		}
		fileprocessor.FormatLogPrintf(ctx, "stable NodeID format validation: plan=%d property_errors=%d", len(roleAssignments), len(formatIssues))
		roleDiffDetails := buildWorkflowRoleFormatLogDetails(ast.Nodes, roleFormatPlan, formatIssues, roleAssignments, profile)
		fileprocessor.FormatLogRoleParaDiffs(ctx, "第5步：段落级格式差异明细（模板要求→修改后，供人工核查）", roleDiffDetails)
	}
	for _, item := range roleFormatPlan {
		if !roleFormatPlanRequiresReview(item) {
			continue
		}
		result.VerifyResult.Warnings = append(result.VerifyResult.Warnings, verify.Issue{
			Kind: "role_review_required", Severity: "warning",
			Message: strings.Trim(strings.Join([]string{item.Reason, strings.Join(item.Evidence, "; ")}, "; "), "; "), Target: item.NodeID,
		})
	}

	// Compliance remains entirely deterministic. A former end-of-job DeepSeek
	// review serialized every issue in one prompt, which violates the one
	// subject/typed-evidence boundary and could not alter this decision anyway.
	log.Printf("[DEEPSEEK_POLICY] aggregate compliance review disabled job=%s", job.ID)

	// ── 阶段 16：确定最终状态并更新数据库 ──
	stage := workflow.StageManualReview // 默认状态：需人工审核
	downloadPath := outputPath
	if result.Status == workflow.StatusVerifiedPass { // 验证全部通过 → 状态升级为已验证
		stage = workflow.StageVerified
	}
	logWorkflowVerifyResult(ctx, "OOXML阶段验证（尚非最终结论）", result.VerifyResult)
	if pythonVisualURL != "" {
		pythonVisualDiagnosticAttempted = true
		visualSpec := workflowVisualSpec(job.CompiledTemplate.TemplateName, profile)
		log.Printf("[PYTHON_VISUAL] v2 request job=%s template=%s rules=%d output=%s", job.ID, job.CompiledTemplate.TemplateName, workflowVisualRuleCount(visualSpec), outputPath)
		visualInputSHA, hashErr := workflowTemplateSHA(outputPath)
		if hashErr != nil {
			return nil, hashErr
		}
		visualCtx, cancel := context.WithTimeout(ctx, workflowPythonVisualTimeout())
		var visualErr error
		visual, visualErr = NewPythonVisualClient(pythonVisualURL).CheckStudentPaperWithContext(
			visualCtx, outputPath, job.CompiledTemplate.TemplateName, 1, job.ID.String()+"-workflow",
			visualSpec, nil,
		)
		cancel()
		if visualErr != nil {
			log.Printf("[PYTHON_VISUAL] v2 workflow failed job=%s err=%v", job.ID, visualErr)
			fileprocessor.FormatLogPrintf(ctx, "Python视觉复核失败：%v", visualErr)
		} else if visual != nil {
			visualSHA = visualInputSHA
			log.Printf("[PYTHON_VISUAL] v2 workflow completed job=%s verdict=%s violations=%d reviews=%d", job.ID, visual.VisualVerdict, len(visual.Violations), len(visual.ReviewItems))
			fileprocessor.FormatLogPrintf(ctx, "Python视觉复核：verdict=%s violations=%d reviews=%d", visual.VisualVerdict, len(visual.Violations), len(visual.ReviewItems))
			repairs := pythonVisualRepairs(visual)
			log.Printf("[PYTHON_VISUAL] v2 repair candidates job=%s violations=%d candidates=%d", job.ID, len(visual.Violations), len(repairs))
			if len(repairs) > 0 {
				applied, repairErr := fileprocessor.ApplyVisualRepairsToFile(outputPath, repairs)
				if repairErr != nil {
					log.Printf("[PYTHON_VISUAL] v2 repair failed job=%s err=%v", job.ID, repairErr)
					fileprocessor.FormatLogPrintf(ctx, "Python视觉反馈修复失败：%v", repairErr)
				} else if applied > 0 {
					log.Printf("[PYTHON_VISUAL] v2 repairs applied job=%s count=%d; rechecking", job.ID, applied)
					fileprocessor.FormatLogPrintf(ctx, "Python视觉反馈已应用：%d 处；开始二次确定性验证", applied)
					result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
					if err != nil {
						return nil, err
					}
					visualSHA = "" // repairs invalidate the previous rendered evidence
					postRepairSHA, hashErr := workflowTemplateSHA(outputPath)
					if hashErr != nil {
						return nil, hashErr
					}
					visualCtx2, cancel2 := context.WithTimeout(ctx, workflowPythonVisualTimeout())
					visual2, visualErr2 := NewPythonVisualClient(pythonVisualURL).CheckStudentPaperWithContext(
						visualCtx2, outputPath, job.CompiledTemplate.TemplateName, 1, job.ID.String()+"-workflow-postfix",
						workflowVisualSpec(job.CompiledTemplate.TemplateName, profile), nil,
					)
					cancel2()
					if visualErr2 != nil {
						log.Printf("[PYTHON_VISUAL] v2 post-fix verification failed job=%s err=%v", job.ID, visualErr2)
					} else if visual2 != nil {
						visual = visual2
						visualSHA = postRepairSHA
						log.Printf("[PYTHON_VISUAL] v2 post-fix completed job=%s verdict=%s violations=%d reviews=%d", job.ID, visual.VisualVerdict, len(visual.Violations), len(visual.ReviewItems))
						fileprocessor.FormatLogPrintf(ctx, "Python视觉二次复核：verdict=%s violations=%d reviews=%d", visual.VisualVerdict, len(visual.Violations), len(visual.ReviewItems))
					}
				}
			}
			if visual.VisualVerdict != "pass" {
				// A rendered document with remaining visual violations cannot be
				// advertised as verified. It remains downloadable for inspection.
				result.Status = workflow.StatusManualReview
				stage = workflow.StageManualReview
				log.Printf("[WORKFLOW_FLOW] visual gate requires manual review job=%s verdict=%s violations=%d", job.ID, visual.VisualVerdict, len(visual.Violations))
				result.VerifyResult.Warnings = append(result.VerifyResult.Warnings, verify.Issue{Kind: "python_visual_review", Severity: "warning", Message: fmt.Sprintf("Python视觉结果：%s，违规=%d，复核=%d", visual.VisualVerdict, len(visual.Violations), len(visual.ReviewItems)), Target: outputPath})
			}
		}
	}
	// 将最终状态、阶段、下载路径、验证结果写入数据库
	structuredComplianceLogged = true
	// OOXML compliance remains useful even if the optional visual service is
	// unavailable. Visual observations supplement, but never gate, deterministic
	// style decisions.
	if finalAST, astErr := paperast.Extract(outputPath); astErr != nil {
		result.VerifyResult.Warnings = append(result.VerifyResult.Warnings, verify.Issue{Kind: "structured_compliance_unavailable", Severity: "warning", Message: astErr.Error(), Target: outputPath})
		log.Printf("[STRUCTURED_COMPLIANCE] skipped job=%s reason=extract_final_ast err=%v", job.ID, astErr)
	} else {
		decisions := EvaluateStructuredCompliance(job.ID.String(), finalAST, rules, visual)
		appendStructuredComplianceWarnings(&result.VerifyResult, decisions)
		if structuredComplianceHasFix(decisions) {
			result.Status = workflow.StatusManualReview
			stage = workflow.StageManualReview
			log.Printf("[WORKFLOW_FLOW] structured compliance requires manual review job=%s", job.ID)
		}
		log.Printf("[STRUCTURED_COMPLIANCE] job=%s nodes=%d decisions=%d visual_evidence=%t", job.ID, len(finalAST.Nodes), len(decisions), visual != nil)
	}
	// CQIE 规范：摘要页不使用模板手动空段推页的残留。role plan 已给
	// 英文摘要另起页，此处删除摘要节内纯空段（分节符/目录节不受影响）。
	// 必须放在所有确定性校验（角色格式校验、结构化合规）之后，因为删除
	// 空段会前移后续段落索引，任何依赖 assignment.Index 的定位都会错位；
	// 放在这里则只影响最终展示层，且幂等（无空段时原样返回）。
	if !transplantEnabled && profileUsesCQIEHardRules(profile) {
		cleaned, cleanErr := templateapply.CleanAbstractSectionBlankParagraphsInFile(outputPath)
		if cleanErr != nil {
			return nil, fmt.Errorf("clean abstract section blank paragraphs: %w", cleanErr)
		}
		fileprocessor.FormatLogPrintf(ctx, "摘要节空段清理：删除 %d 段", cleaned)
		// These final display-layer edits are still document mutations. Re-run
		// the deterministic loop and stable-node gate so the file being offered
		// for download is the file that was actually verified.
		if cleaned > 0 {
			result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
			if err != nil {
				return nil, fmt.Errorf("revalidate final display-layer edits: %w", err)
			}
			if len(roleAssignments) > 0 {
				formatIssues, formatErr := templateapply.ValidateRoleFormatPlan(ctx, outputPath, profile, roleAssignments)
				if formatErr != nil {
					return nil, fmt.Errorf("revalidate final role format plan: %w", formatErr)
				}
				if len(formatIssues) > 0 {
					result.Status = workflow.StatusManualReview
					result.VerifyResult.Passed = false
					result.VerifyResult.ComplianceStatus = "review_required"
					result.VerifyResult.ComplianceReason = "final display-layer edits did not preserve the role format plan"
				}
			}
			if finalAST, astErr := paperast.Extract(outputPath); astErr != nil {
				return nil, fmt.Errorf("revalidate final structured compliance: %w", astErr)
			} else {
				decisions := EvaluateStructuredCompliance(job.ID.String(), finalAST, rules, visual)
				appendStructuredComplianceWarnings(&result.VerifyResult, decisions)
				if structuredComplianceHasFix(decisions) {
					result.Status = workflow.StatusManualReview
					result.VerifyResult.Passed = false
					result.VerifyResult.ComplianceStatus = "review_required"
					result.VerifyResult.ComplianceReason = "final display-layer edits still have structured compliance findings"
				}
			}
			fileprocessor.FormatLogPrintf(ctx, "最终展示层修改已重新全量校验：通过门禁=%t", result.VerifyResult.Passed)
		}
	}
	// No document mutations are allowed beyond this point. Bind all final
	// evidence to the exact bytes offered for download.
	finalSHA, hashErr := workflowTemplateSHA(outputPath)
	if hashErr != nil {
		return nil, hashErr
	}
	if pythonVisualURL != "" && visualSHA != finalSHA {
		visual = nil
		visualSHA = ""
		visualCtx, cancel := context.WithTimeout(ctx, workflowPythonVisualTimeout())
		fresh, visualErr := NewPythonVisualClient(pythonVisualURL).CheckStudentPaperWithContext(
			visualCtx, outputPath, job.CompiledTemplate.TemplateName, 1, job.ID.String()+"-final-audit",
			workflowVisualSpec(job.CompiledTemplate.TemplateName, profile), nil)
		cancel()
		if visualErr != nil {
			fileprocessor.FormatLogPrintf(ctx, "最终文件视觉验证未完成：%v", visualErr)
		} else if fresh != nil {
			visual, visualSHA = fresh, finalSHA
		}
	}
	// A renderer must not silently change the artifact it was asked to inspect.
	// If it does, the hash mismatch below leaves its observations untrusted.
	finalSHA, hashErr = workflowTemplateSHA(outputPath)
	if hashErr != nil {
		return nil, hashErr
	}
	finalAST, finalErr := paperast.Extract(outputPath)
	if finalErr != nil {
		return nil, fmt.Errorf("extract final rule audit: %w", finalErr)
	}
	audit := buildWorkflowRuleAudit(profile, rules, roleFormatPlan, ast, writtenAST, finalAST, finalSHA, visualSHA, visual)
	audit.Writer = "in_place_role_plan"
	if transplantEnabled {
		audit.Writer = "template_transplant"
	}
	if err := attachWorkflowRuleAudit(&result, audit); err != nil {
		return nil, err
	}
	stage = workflow.StageManualReview
	if result.Status == workflow.StatusVerifiedPass && result.VerifyResult.Passed {
		stage = workflow.StageVerified
	}
	fileprocessor.FormatLogPrintf(ctx, "最终规则审计：文件SHA=%s；规则记录=%d；视觉状态=%s；需复核=%t", finalSHA, len(audit.Entries), audit.VisualStatus, audit.NeedsReview)
	fileprocessor.FormatLogPrintf(ctx, "规则来源→执行→最终验证：%s", string(result.VerifyResult.RuleAudit))
	if err := workflow.NewStore(s.db).UpdateJobResult(ctx, job.ID, result.Status, stage, downloadPath, result.VerifyResult); err != nil {
		return nil, err
	}

	// ── 阶段 17：SHA256 完整性校验 ──
	// 对比源文件和输出文件的 SHA256，确认格式化确实修改了文件
	if sourceSHA, sourceErr := workflowTemplateSHA(job.Paper.FilePath); sourceErr == nil {
		if outputSHA, outputErr := workflowTemplateSHA(outputPath); outputErr == nil {
			fileprocessor.FormatLogPrintf(ctx, "源文件 SHA256：%s", sourceSHA)
			fileprocessor.FormatLogPrintf(ctx, "输出文件 SHA256：%s", outputSHA)
			fileprocessor.FormatLogPrintf(ctx, "输出是否仍是原稿逐字节副本：%t", sourceSHA == outputSHA)
		}
	}
	fileprocessor.FormatLogPrintf(ctx, "最终状态=%s；下载阶段=%s；下载路径=%s", result.Status, stage, downloadPath)
	fileprocessor.SetFormatRunLogResult(ctx, string(result.Status), string(stage))

	// ── 阶段 18：返回最终任务视图 ──
	// 重新查询数据库返回最新的任务状态（包含验证结果、下载路径等）
	return s.GetJobForUser(id, userID)
}

// buildWorkflowRoleFormatLogDetails 汇总稳定 NodeID 计划与逐属性校验残留差异，
// 生成供人工核查使用的段落级中文明细（位置 + 模板要求→修改后值 + 判定理由）。
// 对已应用模板规则的段落附带其命中规则的模板要求值（TemplateRule），使
// residual=0（应用后完全达标）时日志仍能展示"模板要求值"供人工核查。
func buildWorkflowRoleFormatLogDetails(
	nodes []paperast.Node,
	plan []templateapply.FormatPlanItem,
	issues []templateapply.RolePlanValidationIssue,
	assignments []roleclassify.Assignment,
	profile *templateprofile.Profile,
) []fileprocessor.RoleParaDetail {
	nodeByID := map[string]paperast.Node{}
	for _, node := range nodes {
		nodeByID[node.NodeID] = node
	}
	planByID := map[string]templateapply.FormatPlanItem{}
	for _, item := range plan {
		planByID[item.NodeID] = item
	}
	residualByID := map[string][]fileprocessor.RolePropDiff{}
	for _, issue := range issues {
		residualByID[issue.NodeID] = append(residualByID[issue.NodeID], fileprocessor.RolePropDiff{
			Property: issue.Property, Expected: issue.Expected, Actual: issue.Actual,
		})
	}
	details := make([]fileprocessor.RoleParaDetail, 0, len(assignments))
	for _, assignment := range assignments {
		detail := fileprocessor.RoleParaDetail{
			NodeID: assignment.NodeID, ParaIndex: assignment.Index, Role: assignment.Role,
		}
		if node, ok := nodeByID[assignment.NodeID]; ok {
			detail.Text = node.Text
		}
		if item, ok := planByID[assignment.NodeID]; ok {
			detail.Applied = item.Apply
			detail.RuleKey = item.RuleKey
			detail.Reason = item.Reason
			detail.ReviewRequired = item.Flow != nil && item.Flow.ReviewRequired
			detail.Residual = append([]fileprocessor.RolePropDiff(nil), residualByID[assignment.NodeID]...)
			if detail.Applied && item.RuleKey != "" && profile != nil {
				if rule, found := templateapply.RoleStyleRequirement(profile, item.Role, item.RuleKey); found {
					ruleCopy := rule
					detail.TemplateRule = &ruleCopy
				}
			}
		}
		details = append(details, detail)
	}
	return details
}

func roleFormatPlanRequiresReview(item templateapply.FormatPlanItem) bool {
	return !item.Apply || (item.Flow != nil && item.Flow.ReviewRequired) || (!item.Trusted && item.Confidence < 0.85)
}

func profileUsesCQIEHardRules(profile *templateprofile.Profile) bool {
	if profile == nil {
		return false
	}
	text := strings.Join([]string{
		profile.Source,
		profile.Header.Text,
		profile.HeaderFirst.Text,
		profile.HeaderEven.Text,
		profile.RulePack.HeaderPolicy,
		profile.RulePack.OddHeaderText,
		profile.RulePack.EvenHeaderText,
	}, " ")
	return strings.Contains(text, "重庆工程学院")
}

func pythonVisualRepairs(report *PythonVisualReport) []map[string]interface{} {
	if report == nil {
		return nil
	}
	repairs := make([]map[string]interface{}, 0)
	for _, issue := range report.Violations {
		repair, ok := issue["suggestedRepair"].(map[string]interface{})
		if !ok || repair["action"] != "set_paragraph_alignment" || repair["value"] == nil {
			continue
		}
		confidence, _ := issue["confidence"].(float64)
		nodeID, _ := issue["nodeId"].(string)
		if confidence < 0.85 || nodeID == "" {
			continue
		}
		repairs = append(repairs, map[string]interface{}{
			"node_id": nodeID, "action": repair["action"], "value": repair["value"],
		})
	}
	return repairs
}

func workflowVisualRuleCount(spec map[string]interface{}) int {
	if rules, ok := spec["rules"].([]interface{}); ok {
		return len(rules)
	}
	return 0
}

// persistWorkflowContracts 将工作流中间产物持久化到数据库。
// 数据流向：内存中的 AST 快照 → paper 表的 parsed_info 字段；
// Profile / Rules / Contract → compiled_template 表的 JSON 字段。
// 这些数据供后续查询（GetJobForUser）返回给前端展示。
func (s *paperWorkflowService) persistWorkflowContracts(ctx context.Context, job model.PaperWorkflowJob, profile *templateprofile.Profile, rules templatecontract.RuleSet, ast paperast.Snapshot, contract repaircontract.Contract) error {
	// 将论文的 AST 解析结果（段落结构、标题层级等）写入 paper 表
	if err := s.db.WithContext(ctx).Model(&model.Paper{}).
		Where("id = ?", job.PaperID).
		Updates(map[string]any{
			"parsed_info": paperast.Marshal(ast), // AST 快照序列化为 JSON
			"updated_at":  time.Now().UTC(),
		}).Error; err != nil {
		return err
	}
	// 将模板 Profile、校验规则、修复合同写入 compiled_template 表
	return s.db.WithContext(ctx).Model(&model.CompiledTemplate{}).
		Where("id = ?", job.CompiledTemplateID).
		Updates(map[string]any{
			"style_profiles_json":     templateprofile.Marshal(profile), // 样式画像 JSON
			"verification_rules_json": templatecontract.Marshal(rules),  // 验证规则 JSON
			"mapping_contract_json":   repaircontract.Marshal(contract), // 修复合同 JSON
			"updated_at":              time.Now().UTC(),
		}).Error
}

func repairRenderedPageFooterTotal(outputPath string, result verify.Result) (bool, error) {
	if !hasWorkflowIssue(result.RepairableIssues, "render_page_footer_total_mismatch") || result.RenderResult == nil {
		return false, nil
	}
	total := renderedBodyPageTotal(result.RenderResult.PageTexts)
	if total <= 0 {
		return false, nil
	}
	pkg, err := ooxmlpkg.Open(outputPath)
	if err != nil {
		return false, err
	}
	changed := false
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/footer") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		content, ok := pkg.Get(name)
		if !ok {
			continue
		}
		updated := string(content)
		if strings.Contains(updated, "NUMPAGES") || strings.Contains(updated, "SECTIONPAGES") {
			updated = totalPagesFieldBlockPattern.ReplaceAllString(updated, `<w:r><w:t>`+strconv.Itoa(total)+`</w:t></w:r>`)
		} else {
			updated = materializedTotalPagePattern.ReplaceAllString(updated, `${1}`+strconv.Itoa(total)+`${2}`)
		}
		if updated == string(content) {
			continue
		}
		pkg.Set(name, []byte(updated))
		changed = true
	}
	if !changed {
		return false, nil
	}
	return true, pkg.Write(outputPath)
}

func repairRenderedTOCPageNumbers(outputPath string, result verify.Result) (bool, error) {
	if result.RenderResult == nil || len(result.RenderResult.PageTexts) == 0 {
		return false, nil
	}
	pkg, err := ooxmlpkg.Open(outputPath)
	if err != nil {
		return false, err
	}
	content, ok := pkg.Get("word/document.xml")
	if !ok {
		return false, nil
	}
	headingKeys := tocCachedHeadingKeys(string(content))
	pageByHeading := renderedHeadingPages(result.RenderResult.PageTexts, headingKeys)
	if len(pageByHeading) == 0 {
		return false, nil
	}
	updated, changed := replaceTOCCachedPageNumbers(string(content), pageByHeading)
	if !changed {
		return false, nil
	}
	pkg.Set("word/document.xml", []byte(updated))
	return true, pkg.Write(outputPath)
}

func replaceTOCCachedPageNumbers(documentXML string, pageByHeading map[string]int) (string, bool) {
	inTOC := false
	changed := false
	updated := workflowParagraphPattern.ReplaceAllStringFunc(documentXML, func(paragraph string) string {
		if strings.Contains(paragraph, `TOC \o "1-3"`) || strings.Contains(paragraph, " TOC ") {
			inTOC = true
			return paragraph
		}
		if inTOC && strings.Contains(paragraph, `w:fldCharType="end"`) {
			inTOC = false
			return paragraph
		}
		if !inTOC || strings.Contains(paragraph, "<w:instrText") {
			return paragraph
		}
		text := tocCacheHeadingText(paragraph)
		key := canonicalRenderedHeadingKey(text)
		page := pageByHeading[key]
		if key == "" || page <= 0 || renderedTOCEntryWithPagePattern.MatchString(text) {
			return paragraph
		}
		changed = true
		return replaceFirstWorkflowText(paragraph, text+"    "+strconv.Itoa(page))
	})
	return updated, changed
}

func tocCacheHeadingText(paragraph string) string {
	text := strings.TrimSpace(workflowParagraphText(paragraph))
	if !strings.Contains(paragraph, "<w:tab") {
		return text
	}
	matches := workflowTextValuePattern.FindAllStringSubmatch(paragraph, -1)
	if len(matches) == 0 || len(matches[0]) < 2 {
		return text
	}
	return strings.TrimSpace(html.UnescapeString(matches[0][1]))
}

func tocCachedHeadingKeys(documentXML string) []string {
	inTOC := false
	keys := make([]string, 0)
	for _, paragraph := range workflowParagraphPattern.FindAllString(documentXML, -1) {
		if strings.Contains(paragraph, `TOC \o "1-3"`) || strings.Contains(paragraph, " TOC ") {
			inTOC = true
			continue
		}
		if inTOC && strings.Contains(paragraph, `w:fldCharType="end"`) {
			break
		}
		if inTOC {
			if key := canonicalRenderedHeadingKey(tocCacheHeadingText(paragraph)); key != "" {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func renderedHeadingPages(pageTexts []string, headingKeys []string) map[string]int {
	pages := map[string]int{}
	for _, pageText := range pageTexts {
		bodyPage := renderedCurrentBodyPage(pageText)
		if bodyPage <= 0 {
			continue
		}
		compactPage := strings.ReplaceAll(strings.Join(strings.Fields(pageText), ""), " ", "")
		for _, key := range headingKeys {
			if key != "" && pages[key] == 0 && strings.Contains(compactPage, key) {
				pages[key] = bodyPage
			}
		}
	}
	return pages
}

func renderedCurrentBodyPage(pageText string) int {
	match := renderedCurrentBodyPagePattern.FindStringSubmatch(pageText)
	if len(match) != 2 {
		return 0
	}
	page, _ := strconv.Atoi(match[1])
	return page
}

func canonicalRenderedHeadingKey(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	fields := strings.Fields(text)
	if len(fields) >= 3 && fields[0] == fields[1] && renderedHeadingNumberPattern.MatchString(fields[0]) {
		text = fields[0] + " " + strings.Join(fields[2:], " ")
	}
	match := renderedHeadingPattern.FindStringSubmatch(text)
	if len(match) != 3 {
		return ""
	}
	return strings.ReplaceAll(match[1]+" "+match[2], " ", "")
}

func replaceFirstWorkflowText(paragraph string, text string) string {
	if strings.Contains(paragraph, "<w:tab") {
		return replaceTOCTabbedWorkflowText(paragraph, text)
	}
	match := workflowTextValuePattern.FindStringSubmatchIndex(paragraph)
	if len(match) < 4 {
		return paragraph
	}
	return paragraph[:match[2]] + html.EscapeString(text) + paragraph[match[3]:]
}

func replaceTOCTabbedWorkflowText(paragraph string, text string) string {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return paragraph
	}
	page := fields[len(fields)-1]
	heading := strings.Join(fields[:len(fields)-1], " ")
	matches := workflowTextValuePattern.FindAllStringSubmatchIndex(paragraph, -1)
	if len(matches) < 2 {
		return paragraph
	}
	first := matches[0]
	last := matches[len(matches)-1]
	updated := paragraph[:last[2]] + html.EscapeString(page) + paragraph[last[3]:]
	first = workflowTextValuePattern.FindAllStringSubmatchIndex(updated, -1)[0]
	return updated[:first[2]] + html.EscapeString(heading) + updated[first[3]:]
}

func repairManualCaptionFields(outputPath string, result verify.Result) (bool, error) {
	if !hasWorkflowIssue(result.Warnings, "manual_caption_not_dynamic") {
		return false, nil
	}
	pkg, err := ooxmlpkg.Open(outputPath)
	if err != nil {
		return false, err
	}
	content, ok := pkg.Get("word/document.xml")
	if !ok {
		return false, nil
	}
	updated, changed := replaceManualCaptionFields(string(content))
	if !changed {
		return false, nil
	}
	pkg.Set("word/document.xml", []byte(updated))
	return true, pkg.Write(outputPath)
}

func replaceManualCaptionFields(documentXML string) (string, bool) {
	changed := false
	nextBookmarkID := nextWorkflowBookmarkID(documentXML)
	lastTableBookmark := ""
	updated := workflowParagraphPattern.ReplaceAllStringFunc(documentXML, func(paragraph string) string {
		if replaced, ok := replaceContinuedTableCaptionParagraph(paragraph, lastTableBookmark); ok {
			changed = true
			return replaced
		}
		replaced, ok := replaceManualCaptionParagraph(paragraph, nextBookmarkID)
		if ok {
			changed = true
			text := strings.TrimSpace(workflowParagraphText(paragraph))
			if match := manualCaptionLinePattern.FindStringSubmatch(text); len(match) == 5 && match[1] == "\u8868" {
				lastTableBookmark = workflowCaptionBookmarkName(match[1], match[2], match[3])
			}
			nextBookmarkID++
			return replaced
		}
		return paragraph
	})
	return updated, changed
}

func replaceContinuedTableCaptionParagraph(paragraph string, bookmark string) (string, bool) {
	if bookmark == "" || strings.Contains(paragraph, "<w:instrText") {
		return paragraph, false
	}
	textRun := workflowTextRunPattern.FindStringIndex(paragraph)
	if textRun == nil {
		return paragraph, false
	}
	firstText := workflowTextValue(paragraph[textRun[0]:textRun[1]])
	match := continuedTableCaptionLinePattern.FindStringSubmatch(strings.TrimSpace(firstText))
	if len(match) != 4 {
		return paragraph, false
	}
	runStart, runEnd, ok := workflowRunBoundsContaining(paragraph, textRun[0], textRun[1])
	if !ok {
		return paragraph, false
	}
	rPr := firstRunProperties(paragraph[runStart:runEnd])
	replacement := workflowTextRun("\u7eed", rPr) +
		workflowRefFieldRuns(bookmark, "\u8868"+match[1]+"-"+match[2], rPr) +
		workflowTextRun(" "+match[3], rPr)
	return paragraph[:runStart] + replacement + paragraph[runEnd:], true
}

func replaceManualCaptionParagraph(paragraph string, bookmarkID int) (string, bool) {
	if strings.Contains(paragraph, "<w:instrText") && strings.Contains(paragraph, "SEQ") {
		return paragraph, false
	}
	textRun := workflowTextRunPattern.FindStringIndex(paragraph)
	if textRun == nil {
		return paragraph, false
	}
	firstText := workflowTextValue(paragraph[textRun[0]:textRun[1]])
	match := manualCaptionLinePattern.FindStringSubmatch(strings.TrimSpace(firstText))
	if len(match) != 5 {
		return paragraph, false
	}
	runStart, runEnd, ok := workflowRunBoundsContaining(paragraph, textRun[0], textRun[1])
	if !ok {
		return paragraph, false
	}
	rPr := firstRunProperties(paragraph[runStart:runEnd])
	bookmarkName := workflowCaptionBookmarkName(match[1], match[2], match[3])
	replacement := fmt.Sprintf(`<w:bookmarkStart w:id="%d" w:name="%s"/>`, bookmarkID, bookmarkName) +
		captionFieldRuns(match[1], match[2], match[3], "", rPr) +
		fmt.Sprintf(`<w:bookmarkEnd w:id="%d"/>`, bookmarkID) +
		workflowTextRun(" "+match[4], rPr)
	replaced := paragraph[:runStart] + replacement + paragraph[runEnd:]
	return replaced, true
}

func workflowRunBoundsContaining(paragraph string, start int, end int) (int, int, bool) {
	for _, bounds := range workflowRunPattern.FindAllStringIndex(paragraph, -1) {
		if len(bounds) == 2 && bounds[0] <= start && bounds[1] >= end {
			return bounds[0], bounds[1], true
		}
	}
	return 0, 0, false
}

func workflowTextValue(textRun string) string {
	match := workflowTextValuePattern.FindStringSubmatch(textRun)
	if len(match) != 2 {
		return ""
	}
	return html.UnescapeString(match[1])
}

func workflowParagraphText(paragraph string) string {
	var builder strings.Builder
	for _, match := range workflowTextValuePattern.FindAllStringSubmatch(paragraph, -1) {
		if len(match) == 2 {
			builder.WriteString(html.UnescapeString(match[1]))
		}
	}
	return builder.String()
}

func firstRunProperties(run string) string {
	start := strings.Index(run, "<w:rPr>")
	end := strings.Index(run, "</w:rPr>")
	if start < 0 || end < start {
		return ""
	}
	return run[start : end+len("</w:rPr>")]
}

func captionFieldRuns(label string, chapter string, ordinal string, title string, rPr string) string {
	prefix := label
	instruction := " SEQ " + label + ` \* ARABIC `
	if chapter != "" {
		prefix += chapter + "-"
		instruction = " SEQ " + label + ` \* ARABIC \s 1 `
	}
	result := workflowTextRun(prefix, rPr) +
		workflowFieldCharRun("begin", rPr) +
		workflowInstrRun(instruction, rPr) +
		workflowFieldCharRun("separate", rPr) +
		workflowTextRun(ordinal, rPr) +
		workflowFieldCharRun("end", rPr)
	if title != "" {
		result += workflowTextRun(" "+title, rPr)
	}
	return result
}

func workflowTextRun(text string, rPr string) string {
	return `<w:r>` + rPr + `<w:t xml:space="preserve">` + html.EscapeString(text) + `</w:t></w:r>`
}

func workflowInstrRun(instruction string, rPr string) string {
	return `<w:r>` + rPr + `<w:instrText xml:space="preserve">` + html.EscapeString(instruction) + `</w:instrText></w:r>`
}

func workflowFieldCharRun(fieldType string, rPr string) string {
	return `<w:r>` + rPr + `<w:fldChar w:fldCharType="` + fieldType + `"/></w:r>`
}

func repairManualCrossReferenceFields(outputPath string, result verify.Result) (bool, error) {
	if !hasWorkflowIssue(result.Warnings, "manual_cross_reference") {
		return false, nil
	}
	pkg, err := ooxmlpkg.Open(outputPath)
	if err != nil {
		return false, err
	}
	content, ok := pkg.Get("word/document.xml")
	if !ok {
		return false, nil
	}
	updated, changed := replaceManualCrossReferenceFields(string(content))
	if !changed {
		return false, nil
	}
	pkg.Set("word/document.xml", []byte(updated))
	return true, pkg.Write(outputPath)
}

func repairManualFormulaNumberFields(outputPath string, result verify.Result) (bool, error) {
	if !hasWorkflowIssue(result.RepairableIssues, "manual_formula_number_not_dynamic") {
		return false, nil
	}
	pkg, err := ooxmlpkg.Open(outputPath)
	if err != nil {
		return false, err
	}
	content, ok := pkg.Get("word/document.xml")
	if !ok {
		return false, nil
	}
	updated, changed := replaceManualFormulaNumberFields(string(content))
	if !changed {
		return false, nil
	}
	pkg.Set("word/document.xml", []byte(updated))
	return true, pkg.Write(outputPath)
}

func replaceManualFormulaNumberFields(documentXML string) (string, bool) {
	changed := false
	updated := workflowTablePattern.ReplaceAllStringFunc(documentXML, func(table string) string {
		if (!strings.Contains(table, "<m:oMath") && !strings.Contains(table, "<m:oMathPara")) || strings.Contains(table, " SEQ 公式 ") {
			return table
		}
		return workflowRunPattern.ReplaceAllStringFunc(table, func(run string) string {
			match := formulaNumberLinePattern.FindStringSubmatch(workflowTextValue(run))
			if len(match) != 3 {
				return run
			}
			rPr := firstRunProperties(run)
			changed = true
			return workflowTextRun("("+match[1]+"-", rPr) +
				workflowFieldCharRun("begin", rPr) +
				workflowInstrRun(" SEQ 公式 \\* ARABIC \\s 1 ", rPr) +
				workflowFieldCharRun("separate", rPr) +
				workflowTextRun(match[2], rPr) +
				workflowFieldCharRun("end", rPr) +
				workflowTextRun(")", rPr)
		})
	})
	return updated, changed
}

func finalizeGeneratedPaperDOCX(ctx context.Context, outputPath string) error {
	if strings.TrimSpace(outputPath) == "" {
		return nil
	}
	if err := fileprocessor.RepairRunningHeaders(outputPath); err != nil {
		return err
	}
	verifier := verify.NewVerifier().WithoutSchoolSpecificRules()
	for _, repair := range []func(string, verify.Result) (bool, error){
		repairManualCaptionFields,
		repairManualFormulaNumberFields,
		repairManualCrossReferenceFields,
	} {
		result, err := verifier.Verify(ctx, outputPath)
		if err != nil {
			return err
		}
		if _, err := repair(outputPath, result); err != nil {
			return err
		}
	}
	return nil
}

func replaceManualCrossReferenceFields(documentXML string) (string, bool) {
	bookmarks := workflowCaptionBookmarks(documentXML)
	if len(bookmarks) == 0 {
		return documentXML, false
	}
	changed := false
	updated := workflowParagraphPattern.ReplaceAllStringFunc(documentXML, func(paragraph string) string {
		if paragraphHasWorkflowReferenceField(paragraph) {
			return paragraph
		}
		text := strings.TrimSpace(workflowParagraphText(paragraph))
		if manualCaptionLinePattern.MatchString(text) || strings.HasPrefix(text, "续表") {
			return paragraph
		}
		replaced := workflowRunPattern.ReplaceAllStringFunc(paragraph, func(run string) string {
			runText := workflowTextValue(run)
			if runText == "" {
				return run
			}
			rPr := firstRunProperties(run)
			converted, ok := replaceCaptionReferencesInText(runText, rPr, bookmarks)
			if !ok {
				return run
			}
			changed = true
			return converted
		})
		return replaced
	})
	return updated, changed
}

func workflowCaptionBookmarks(documentXML string) map[string]string {
	bookmarks := map[string]string{}
	for _, paragraph := range workflowParagraphPattern.FindAllString(documentXML, -1) {
		text := strings.TrimSpace(workflowParagraphText(paragraph))
		match := captionReferencePattern.FindStringSubmatch(text)
		if len(match) != 4 || !strings.HasPrefix(text, match[0]) {
			continue
		}
		nameMatch := workflowBookmarkStartPattern.FindStringSubmatch(paragraph)
		if len(nameMatch) != 2 {
			continue
		}
		bookmarks[workflowCaptionReferenceKey(match[1], match[2], match[3])] = nameMatch[1]
	}
	return bookmarks
}

func replaceCaptionReferencesInText(text string, rPr string, bookmarks map[string]string) (string, bool) {
	matches := captionReferencePattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return "", false
	}
	var builder strings.Builder
	position := 0
	changed := false
	for _, match := range matches {
		if len(match) < 8 {
			continue
		}
		label := text[match[2]:match[3]]
		chapter := text[match[4]:match[5]]
		ordinal := ""
		if match[6] >= 0 && match[7] >= 0 {
			ordinal = text[match[6]:match[7]]
		}
		key := workflowCaptionReferenceKey(label, chapter, ordinal)
		bookmark := bookmarks[key]
		if bookmark == "" {
			continue
		}
		builder.WriteString(workflowTextRun(text[position:match[0]], rPr))
		display := text[match[0]:match[1]]
		builder.WriteString(workflowRefFieldRuns(bookmark, display, rPr))
		position = match[1]
		changed = true
	}
	if !changed {
		return "", false
	}
	builder.WriteString(workflowTextRun(text[position:], rPr))
	return builder.String(), true
}

func workflowRefFieldRuns(bookmark string, display string, rPr string) string {
	return workflowFieldCharRun("begin", rPr) +
		workflowInstrRun(" REF "+bookmark+` \h `, rPr) +
		workflowFieldCharRun("separate", rPr) +
		workflowTextRun(display, rPr) +
		workflowFieldCharRun("end", rPr)
}

func paragraphHasWorkflowReferenceField(paragraph string) bool {
	return strings.Contains(paragraph, " REF ") || strings.Contains(paragraph, " PAGEREF ")
}

func nextWorkflowBookmarkID(documentXML string) int {
	next := 1
	for _, match := range workflowBookmarkStartIDPattern.FindAllStringSubmatch(documentXML, -1) {
		if len(match) != 2 {
			continue
		}
		id, err := strconv.Atoi(match[1])
		if err == nil && id >= next {
			next = id + 1
		}
	}
	return next
}

func workflowCaptionBookmarkName(label string, chapter string, ordinal string) string {
	prefix := "Template_Fig"
	if label == "表" {
		prefix = "Template_Tbl"
	}
	return "_" + prefix + "_" + chapter + "_" + ordinal
}

func workflowCaptionReferenceKey(label string, chapter string, ordinal string) string {
	if ordinal == "" {
		return label + chapter
	}
	return label + chapter + "-" + ordinal
}

func renderedBodyPageTotal(pageTexts []string) int {
	maxPage := 0
	for _, text := range pageTexts {
		match := renderedChineseTotalFooterPattern.FindStringSubmatch(strings.Join(strings.Fields(text), ""))
		if len(match) != 3 {
			continue
		}
		current, err := strconv.Atoi(match[1])
		if err == nil && current > maxPage {
			maxPage = current
		}
	}
	return maxPage
}

func hasWorkflowIssue(issues []verify.Issue, kind string) bool {
	for _, issue := range issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}

func (s *paperWorkflowService) GetJob(id string) (*WorkflowJobView, error) {
	if err := s.validateReady(context.Background()); err != nil {
		return nil, err
	}

	jobID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJobID, err)
	}

	return s.getJobView("id = ?", jobID)
}

func (s *paperWorkflowService) GetJobForUser(id string, userID uuid.UUID) (*WorkflowJobView, error) {
	if err := s.validateReady(context.Background()); err != nil {
		return nil, err
	}

	jobID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJobID, err)
	}

	return s.getJobView("id = ? AND user_id = ?", jobID, userID)
}

// validateReady 在执行工作流前做基础环境校验，确保上下文有效、服务实例可用。
func (s *paperWorkflowService) validateReady(ctx context.Context) error {
	// 上下文不能为空，后续数据库操作依赖有效 ctx 做超时控制与取消传播
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	// 如果 ctx 已被取消（如客户端断开），提前终止避免无效工作
	if err := ctx.Err(); err != nil {
		return err
	}
	// 服务实例或数据库连接为空时拒绝执行，避免 nil panic
	if s == nil || s.db == nil {
		return ErrServiceUnavailable
	}
	return nil
}

// ensureWorkflowTables 确保工作流所需的数据库表已存在。
// 先修复旧版模板表的约束，再检查核心表是否存在；不存在则自动建表。
func (s *paperWorkflowService) ensureWorkflowTables(ctx context.Context) error {
	// 修复旧版 format_template（原 V1 表）的数据库约束，兼容历史数据
	if err := database.RepairLegacyFormatTemplateConstraints(s.db.WithContext(ctx)); err != nil {
		return err
	}
	// 如果三张核心工作流表均已存在，无需重复建表，直接返回
	if s.db.Migrator().HasTable(&model.CompiledTemplate{}) &&
		s.db.Migrator().HasTable(&model.PaperWorkflowJob{}) &&
		s.db.Migrator().HasTable(&model.PaperWorkflowIssue{}) {
		return nil
	}
	// 自动创建缺失的工作流表：编译模板、工作流任务、格式问题记录
	return s.db.WithContext(ctx).AutoMigrate(
		&model.CompiledTemplate{},
		&model.PaperWorkflowJob{},
		&model.PaperWorkflowIssue{},
	)
}

// workflowOutputPath 根据任务 ID 构造输出文件路径：{outputRoot}/{jobID}/final.docx。
// 每个工作流任务有独立子目录，避免多任务输出冲突。
func (s *paperWorkflowService) workflowOutputPath(jobID uuid.UUID) (string, error) {
	// 清理并解析输出根目录为绝对路径
	root, err := filepath.Abs(filepath.Clean(s.outputRoot))
	if err != nil {
		return "", err
	}
	// 输出到以 jobID 命名的子目录，文件固定命名为 final.docx
	return filepath.Join(root, jobID.String(), "final.docx"), nil
}

// buildWorkflowOutput creates the structure-preserving base document. It does
// not write paragraph formatting; RunJob's frozen FormatPlan is the sole writer.
func (s *paperWorkflowService) buildWorkflowOutput(ctx context.Context, sourcePath string, outputPath string, record model.CompiledTemplate, profile *templateprofile.Profile, transplantEnabled bool) (*templateprofile.Profile, error) {
	// 提取模板文件路径，用于后续分支操作
	templatePath := strings.TrimSpace(record.SourceFilePath)
	// Profile 为空时无格式参照，直接复制原文作为输出
	if profile == nil {
		return nil, copyFile(sourcePath, outputPath)
	}
	// 模板路径为空 → 无法进行任何格式参照，报错
	if templatePath == "" {
		return profile, fmt.Errorf("selected template path is empty")
	}
	// 防御：模板路径等于学生论文路径 → 配置错误，报错防止丢失原文
	if templatePath == sourcePath {
		return profile, fmt.Errorf("selected template path incorrectly points to the student paper")
	}
	coverParsed, err := paperparse.NewParser().Parse(ctx, sourcePath)
	if err != nil {
		return profile, fmt.Errorf("parse student paper for template transplant: %w", err)
	}
	if transplantEnabled {
		compiled, compileErr := templatecompile.NewCompiler().Compile(ctx, templatePath, templatecompile.CompileOptions{
			SchoolID:     record.SchoolID,
			TemplateName: record.TemplateName,
			Version:      record.TemplateVersion,
			OutputDir:    filepath.Join(filepath.Dir(outputPath), "_compiled_template"),
		})
		if compileErr != nil {
			return profile, fmt.Errorf("compile selected template skeleton: %w", compileErr)
		}
		mapping, mapErr := blockmap.NewMapper().Map(compiled, coverParsed)
		if mapErr != nil {
			return profile, fmt.Errorf("map student content to template slots: %w", mapErr)
		}
		if len(mapping.AmbiguousBlocks) > 0 || len(mapping.UnmappedBlocks) > 0 {
			return profile, fmt.Errorf("template transplant requires review: ambiguous=%v unmapped=%v", mapping.AmbiguousBlocks, mapping.UnmappedBlocks)
		}
		if err := transplant.NewTransplanter().Generate(ctx, transplant.GenerateInput{
			CompiledTemplate: compiled,
			Mapping:          mapping,
			OutputPath:       outputPath,
			TemplateProfile:  profile,
		}); err != nil {
			return profile, fmt.Errorf("generate document from template skeleton: %w", err)
		}
		fileprocessor.FormatLogPrintf(ctx, "模板移植：使用模板骨架生成输出；绑定=%d，生成块=%d。", len(mapping.Bindings), len(mapping.GeneratedBlocks))
		return profile, nil
	}
	if err := copyFile(sourcePath, outputPath); err != nil {
		return profile, err
	}
	if err := fileprocessor.CopyTemplateHeaderFooter(templatePath, outputPath, coverParsed.CoverFields); err != nil {
		return profile, fmt.Errorf("copy template header/footer: %w", err)
	}
	fileprocessor.FormatLogPrintf(ctx, "selected DOCX copied; profile application is deferred to the single RunJob format stage")
	return profile, nil
}

func WorkflowTemplatePath(template model.FormatTemplate) string {
	// Only the DOCX uploaded/selected as the template is authoritative. The
	// legacy GoldenTemplatePath field is intentionally ignored.
	candidates := []string{template.FilePath}
	if template.ID != uuid.Nil {
		candidates = append(candidates, filepath.Join("uploads", "templates", template.ID.String(), "template.docx"))
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || !strings.EqualFold(filepath.Ext(candidate), ".docx") {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func workflowTemplateSHA(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read selected template for hash: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(content)), nil
}

// templateTransplantEnabled 判断是否可以使用「模板移植」分支（分支 A）。
// 分支 A：用模板骨架重建论文结构（Compile → Transplanter.Generate），保留模板的全部 OOXML 结构。
// 分支 B：就地修复（copyFile → ApplyTemplateProfileStylesAndPageSetup → CopyTemplateHeaderFooter），保留学生原文结构。
// 决策依据：
//   - 模板路径或 Profile 为空 → 禁用移植（无模板可参照）
//   - 模板含审阅标记（批注/修订） → 禁用移植（审阅状态会污染输出）
//   - 环境变量显式置 0/false/no/off → 强制禁用移植
//   - 模板使用了 CQRWST 规范化器或含映射锚点 → 启用移植（模板有结构化槽位）
func templateTransplantEnabled(templatePath string, profile *templateprofile.Profile) bool {
	// 模板路径为空或 Profile 未配置 → 无法进行模板移植，返回 false
	if strings.TrimSpace(templatePath) == "" || profile == nil {
		return false
	}
	// 模板包含批注/修订等审阅标记 → 不能用作移植骨架，返回 false
	if templateContainsReviewMarkup(templatePath) {
		return false
	}
	// 读取环境变量 PAPER_TEMPLATE_TRANSPLANT_ENABLED，显式禁用时返回 false
	switch strings.ToLower(strings.TrimSpace(os.Getenv(cqrwstTemplateTransplantEnabledEnv))) {
	case "0", "false", "no", "off":
		return false
	default:
		// 模板使用了 CQRWST 规范化器（如 SmartNormalizer）或有映射锚点 → 可以移植
		return transplant.UsesCQRWSTNormalizers(templatePath) || templateHasMappingAnchors(templatePath)
	}
}

func workflowStrictTemplateTransplantEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PAPER_TEMPLATE_TRANSPLANT_ONLY"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// templateContainsReviewMarkup 检测模板是否包含审阅标记（批注/修订痕迹）。
// 含审阅标记的模板不能用作移植骨架，因为 Word 的修订/批注在 OOXML 层面
// 会生成额外的 run/段落，移植时会污染输出文档。
func templateContainsReviewMarkup(templatePath string) bool {
	// 以 ZIP 方式打开 docx 文件，解析 OOXML 包
	pkg, err := ooxmlpkg.Open(templatePath)
	if err != nil {
		// 打不开文件视为有风险，保守返回 true 禁用移植
		return true
	}
	// 检查是否存在 word/comments.xml（批注文件），有则说明模板含批注
	if _, ok := pkg.Get("word/comments.xml"); ok {
		return true
	}
	// 遍历 word/ 下所有 XML 文件，检测修订标记
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		content, ok := pkg.Get(name)
		// 用正则匹配 w:ins（插入修订）、w:del（删除修订）等标记
		if ok && workflowReviewMarkupPattern.Match(content) {
			return true
		}
	}
	return false
}

// structureRequiresInPlaceRepair 判断学生论文是否包含模板移植无法保留的 OOXML 结构。
// 当学生论文含以下任一结构时，必须降级为「就地修复」分支（分支 B），
// 因为模板骨架重建（分支 A）会丢弃这些精细的 OOXML 元素。
// 每个 OR 条件代表一类无法通过模板移植保留的受保护对象：
func structureRequiresInPlaceRepair(snapshot repaircontract.StructureSnapshot) bool {
	return len(snapshot.Bookmarks) > 0 || // 书签起始标记：Word 文档内的跳转锚点，模板移植无法重建
		len(snapshot.BookmarkEnds) > 0 || // 书签结束标记：与书签起始配对，移植会丢失书签范围
		len(snapshot.Fields) > 0 || // 域代码：如自动页码 {PAGE}、目录 {TOC}、交叉引用等，移植无法保留
		snapshot.Formulas > 0 || // 公式（MathML/OMML）：数学公式对象，重建骨架会丢失公式
		snapshot.Drawings > 0 || // 绘图对象：Word 内嵌形状/图表，移植不可迁移
		snapshot.Pictures > 0 || // 图片：文档中的图片引用，模板重建可能丢失或错位
		snapshot.FootnoteReferences > 0 || // 脚注引用：正文中的脚注编号标记，移植会断连
		snapshot.EndnoteReferences > 0 || // 尾注引用：正文中的尾注编号标记，移植会断连
		snapshot.CommentReferences > 0 || // 批注引用：正文中的批注编号标记，移植会断连
		snapshot.CommentRangeStarts > 0 || // 批注范围起始：标记批注覆盖的文本起始位置
		snapshot.CommentRangeEnds > 0 || // 批注范围结束：标记批注覆盖的文本结束位置
		snapshot.FootnoteDefinitions > 0 || // 脚注定义：脚注的具体内容，移植无法迁移
		snapshot.EndnoteDefinitions > 0 || // 尾注定义：尾注的具体内容，移植无法迁移
		snapshot.CommentDefinitions > 0 || // 批注定义：批注的具体内容，移植无法迁移
		len(snapshot.Hyperlinks) > 0 || // 超链接：文档内的外部/内部链接，移植可能丢失
		len(snapshot.VerticalAlignments) > 0 || // 垂直对齐：单元格/文本的纵向对齐设置
		len(snapshot.MediaHashes) > 0 || // 媒体文件指纹：文档内嵌图片/视频的唯一标识
		len(snapshot.ReferencedImageHashes) > 0 || // 引用图片指纹：通过 rId 引用的图片资源
		len(snapshot.RelationshipTargets) > 0 || // 关系目标：OOXML .rels 文件中的外部资源引用
		len(snapshot.ProtectedPartHashes) > 0 // 受保护区域：文档保护范围内不可编辑的部分
}

// templateHasMappingAnchors 检测模板 document.xml 是否包含内容映射锚点标记。
// 锚点标记是模板中预设的占位符（如 {{title}}、{{abstract}} 等），
// 表示模板有明确的"内容槽位"，可以用移植的方式将学生内容填入对应位置。
// 有锚点的模板走分支 A（模板骨架重建）；无锚点的模板走分支 B（就地修复）。
func templateHasMappingAnchors(path string) bool {
	// 以 ZIP 方式打开 docx，获取 word/document.xml
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return false
	}
	documentXML, ok := pkg.Get("word/document.xml")
	// 用预编译正则匹配锚点标记模式（如 {{...}} 占位符）
	return ok && workflowTemplateAnchorPattern.Match(documentXML)
}

func preserveSourceDrawingGroups(sourcePath string, outputPath string, profile *templateprofile.Profile) error {
	source, err := ooxmlpkg.Open(sourcePath)
	if err != nil {
		return err
	}
	output, err := ooxmlpkg.Open(outputPath)
	if err != nil {
		return err
	}
	sourceXML, sourceOK := source.Get("word/document.xml")
	outputXML, outputOK := output.Get("word/document.xml")
	if !sourceOK || !outputOK {
		return nil
	}
	updated := string(outputXML)
	figureOrdinals := map[string]int{}
	maxWidth := templateUsableWidthEMU(profile)
	for _, bounds := range workflowAlternateContentPattern.FindAllStringIndex(string(sourceXML), -1) {
		block := normalizeWorkflowDrawingWidth(string(sourceXML)[bounds[0]:bounds[1]], maxWidth)
		if !strings.Contains(block, "<w:drawing") {
			continue
		}
		heading := precedingWorkflowHeading(string(sourceXML)[:bounds[0]])
		if heading == "" {
			continue
		}
		headingMatch := renderedHeadingPattern.FindStringSubmatch(heading)
		if len(headingMatch) != 3 {
			continue
		}
		chapter := strings.Split(headingMatch[1], ".")[0]
		figureOrdinals[chapter]++
		caption := fmt.Sprintf("\u56fe%s-%d %s", chapter, figureOrdinals[chapter], headingMatch[2])
		drawingText := compactWorkflowText(workflowParagraphText(block))
		updated = workflowParagraphPattern.ReplaceAllStringFunc(updated, func(paragraph string) string {
			text := compactWorkflowText(workflowParagraphText(paragraph))
			if drawingText != "" && text == drawingText {
				return ""
			}
			if compactWorkflowText(heading) != text {
				return paragraph
			}
			return paragraph +
				`<w:p><w:r>` + block + `</w:r></w:p>` +
				`<w:p>` + workflowTextRun(caption, "") + `</w:p>`
		})
	}
	if updated == string(outputXML) {
		return nil
	}
	output.Set("word/document.xml", []byte(updated))
	return output.Write(outputPath)
}

func normalizeWorkflowDrawingWidth(block string, maxWidth int) string {
	if maxWidth <= 0 {
		return block
	}
	extentIndex := 0
	return workflowDrawingExtentPattern.ReplaceAllStringFunc(block, func(extent string) string {
		extentIndex++
		if extentIndex > 3 {
			return extent
		}
		match := workflowDrawingExtentPattern.FindStringSubmatch(extent)
		if len(match) != 3 {
			return extent
		}
		width, widthErr := strconv.Atoi(match[1])
		height, heightErr := strconv.Atoi(match[2])
		if widthErr != nil || heightErr != nil || width <= maxWidth {
			return extent
		}
		scaledHeight := height * maxWidth / width
		extent = strings.Replace(extent, `cx="`+match[1]+`"`, fmt.Sprintf(`cx="%d"`, maxWidth), 1)
		return strings.Replace(extent, `cy="`+match[2]+`"`, fmt.Sprintf(`cy="%d"`, scaledHeight), 1)
	})
}

func templateUsableWidthEMU(profile *templateprofile.Profile) int {
	if profile == nil {
		return 0
	}
	width, widthErr := strconv.Atoi(profile.PageSetup.PageWidthTwips)
	left, leftErr := strconv.Atoi(profile.PageSetup.MarginLeftTwips)
	right, rightErr := strconv.Atoi(profile.PageSetup.MarginRightTwips)
	if widthErr != nil || leftErr != nil || rightErr != nil || width <= left+right {
		return 0
	}
	return (width - left - right) * 635
}

func precedingWorkflowHeading(prefix string) string {
	paragraphs := workflowParagraphPattern.FindAllString(prefix, -1)
	for index := len(paragraphs) - 1; index >= 0; index-- {
		text := strings.TrimSpace(workflowParagraphText(paragraphs[index]))
		if renderedHeadingPattern.MatchString(text) {
			return text
		}
	}
	return ""
}

func compactWorkflowText(text string) string {
	return strings.Join(strings.Fields(text), "")
}

func compactWorkflowLogText(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return text
}

func logWorkflowVerifyResult(ctx context.Context, label string, result verify.Result) {
	fileprocessor.FormatLogPrintf(ctx, "%s：通过=%t；合规状态=%s；原因=%s；可修复=%d；阻断=%d；警告=%d",
		label, result.Passed, result.ComplianceStatus, result.ComplianceReason,
		len(result.RepairableIssues), len(result.FatalIssues), len(result.Warnings))
	for _, issue := range result.FatalIssues {
		fileprocessor.FormatLogPrintf(ctx, "  [阻断] %s | %s | %s", issue.Kind, issue.Target, issue.Message)
	}
	for _, issue := range result.RepairableIssues {
		fileprocessor.FormatLogPrintf(ctx, "  [可修复] %s | %s | %s", issue.Kind, issue.Target, issue.Message)
	}
	for _, issue := range result.Warnings {
		fileprocessor.FormatLogPrintf(ctx, "  [警告] %s | %s | %s", issue.Kind, issue.Target, issue.Message)
	}
}

func cqrwstTemplateTransplantEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(cqrwstTemplateTransplantEnabledEnv))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return resolveCQRWSTTemplatePath() != ""
	}
}

func resolveCQRWSTTemplatePath() string {
	if configured := strings.TrimSpace(os.Getenv(cqrwstTemplatePathEnv)); configured != "" {
		return configured
	}
	if info, err := os.Stat(defaultCQRWSTTemplatePath); err == nil && !info.IsDir() {
		return defaultCQRWSTTemplatePath
	}
	return ""
}

func shouldRunCQRWSTPostFix() bool {
	return !cqrwstTemplateTransplantEnabled()
}

func buildWorkflowTemplateProfile(ctx context.Context, templatePath string) *templateprofile.Profile {
	// A template profile is deterministic OOXML evidence. Do not submit the
	// complete template profile to DeepSeek here: semantic calls are made later
	// with one bounded evidence packet per subject.
	profile, err := templateprofile.Build(ctx, templatePath, templateprofile.Options{})
	if err != nil {
		log.Printf("[WORKFLOW_TEMPLATE_PROFILE] build failed: %v", err)
		return nil
	}
	log.Printf("[WORKFLOW_TEMPLATE_PROFILE] built source=%s confidence=%.2f sections=%d styles=%d",
		profile.Source, profile.Confidence, len(profile.Sections), len(profile.Styles))
	logWorkflowTemplateProfile(filepath.Base(templatePath), templatePath, profile)
	return profile
}

func logWorkflowTemplateProfile(templateName, templatePath string, profile *templateprofile.Profile) {
	if !formatRulesDebugEnabled() || profile == nil {
		return
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		log.Printf("[WORKFLOW_TEMPLATE_RULES] marshal failed: %v", err)
		return
	}
	log.Printf("[WORKFLOW_TEMPLATE_RULES] template=%q path=%q final_rules=\n%s", templateName, templatePath, data)
}

func newDeepSeekSemanticBlockClient() templateapply.SemanticAIClient {
	creds := deepSeekCredentialsFromEnvOrFile()
	if !creds.Enabled || creds.Cookie == "" {
		return nil
	}
	return aiclassifier.NewDeepSeekWebClient(creds.Cookie, creds.Bearer)
}

func generatedOutputPreservesSourceContent(ctx context.Context, source *paperparse.ParsedPaper, outputPath string) bool {
	return len(missingGeneratedSourceContent(ctx, source, outputPath)) == 0
}

func missingGeneratedSourceContent(ctx context.Context, source *paperparse.ParsedPaper, outputPath string) []string {
	if source == nil || len(source.ContentBlocks) == 0 {
		return nil
	}

	generated, err := paperparse.NewParser().Parse(ctx, outputPath)
	if err != nil {
		return []string{"parse generated output: " + err.Error()}
	}

	generatedText := normalizeContentText(joinContentBlockText(generated.ContentBlocks))
	if generatedText == "" {
		return []string{"generated output has no body content"}
	}
	missing := make([]string, 0)
	for _, block := range source.ContentBlocks {
		if shouldSkipContentPreservationBlock(block) {
			continue
		}
		sourceText := normalizeContentText(block.Text)
		if sourceText == "" {
			continue
		}
		preserved := strings.Contains(generatedText, sourceText)
		if !preserved && isEnglishKeywordsText(sourceText) {
			preserved = strings.Contains(canonicalEnglishKeywords(generatedText), canonicalEnglishKeywords(sourceText))
		}
		if !preserved {
			missing = append(missing, sourceText)
			if len(missing) >= 10 {
				break
			}
		}
	}
	return missing
}

func isEnglishKeywordsText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(lower, "key words") || strings.HasPrefix(lower, "keywords")
}

func canonicalEnglishKeywords(text string) string {
	text = strings.ToLower(text)
	return strings.Map(func(r rune) rune {
		if r == ',' || r == ';' || r == '，' || r == '；' || r == '、' || r == ':' || r == '：' || r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, text)
}

func shouldSkipContentPreservationBlock(block paperparse.ContentBlock) bool {
	text := strings.TrimSpace(block.Text)
	if text == "" {
		return true
	}
	if block.Kind == "table" {
		return true
	}
	normalized := strings.Join(strings.Fields(text), " ")
	compact := strings.Join(strings.Fields(text), "")
	if compact == "目录" {
		return true
	}
	if regexp.MustCompile(`^(摘要|关键词|Abstract|Keywords?)[:：]?[IVXLCDM\d]+$`).MatchString(compact) {
		return true
	}
	if regexp.MustCompile(`^\d+(?:\.\d+)*\S*.*\s+\d+$`).MatchString(normalized) {
		return true
	}
	if regexp.MustCompile(`^(参考文献|致谢)\s*\d+$`).MatchString(compact) {
		return true
	}
	return false
}

func joinContentBlockText(blocks []paperparse.ContentBlock) string {
	texts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		texts = append(texts, block.Text)
	}
	return strings.Join(texts, "\n")
}

func normalizeContentText(text string) string {
	normalized := strings.Join(strings.Fields(text), " ")
	return normalizeHeadingNumberSpacing(normalized)
}

func normalizeHeadingNumberSpacing(text string) string {
	var out strings.Builder
	out.Grow(len(text) + 8)
	for i := 0; i < len(text); {
		if (i == 0 || isASCIIWhitespace(text[i-1])) && text[i] >= '1' && text[i] <= '9' {
			j := i + 1
			dots := 0
			lastDot := false
			for j < len(text) && ((text[j] >= '0' && text[j] <= '9') || text[j] == '.') {
				if text[j] == '.' {
					dots++
					lastDot = true
				} else {
					lastDot = false
				}
				j++
			}
			validHeadingNumber := (dots == 0 && j-i == 1) || (dots > 0 && dots <= 2 && !lastDot)
			if validHeadingNumber {
				out.WriteString(text[i:j])
				if j < len(text) && !isASCIIWhitespace(text[j]) && !(text[j] >= '0' && text[j] <= '9') {
					out.WriteByte(' ')
				}
				i = j
				continue
			}
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

func isASCIIWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func (s *paperWorkflowService) getJobView(query any, args ...any) (*WorkflowJobView, error) {
	var job model.PaperWorkflowJob
	conds := append([]any{query}, args...)
	if err := s.db.First(&job, conds...).Error; err != nil {
		return nil, err
	}

	view := &WorkflowJobView{
		ID:                 job.ID,
		PaperID:            job.PaperID,
		UserID:             job.UserID,
		CompiledTemplateID: job.CompiledTemplateID,
		Status:             job.Status,
		Stage:              job.Stage,
		DownloadPath:       job.DownloadPath,
	}
	view.DownloadReady = workflowDownloadReady(job)
	if strings.TrimSpace(view.DownloadPath) != "" {
		view.DownloadURL = workflowJobDownloadURL(view.ID)
	}
	return view, nil
}

// workflowDownloadReady reports whether a generated DOCX artifact exists.
// Manual review is an analysis state, not a file-access state.
func workflowDownloadReady(job model.PaperWorkflowJob) bool {
	if strings.TrimSpace(job.DownloadPath) == "" {
		return false
	}
	info, err := os.Stat(job.DownloadPath)
	return err == nil && !info.IsDir() && strings.EqualFold(filepath.Ext(job.DownloadPath), ".docx")
}

func workflowJobDownloadURL(id uuid.UUID) string {
	return "/api/v2/jobs/" + id.String() + "/download"
}

func workflowJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func copyFile(src string, dst string) error {
	if strings.TrimSpace(src) == "" || strings.TrimSpace(dst) == "" {
		return ErrInvalidPaperUpload
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	in, err := os.Open(filepath.Clean(src))
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(filepath.Clean(dst))
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// prepareRoleEvidenceDOCX deliberately returns the original upload. PDF
// coordinates are evidence about the student's current layout, so a "safe"
// normalization copy would still be the wrong rendered document.
func prepareRoleEvidenceDOCX(sourcePath string) (string, error) {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", fmt.Errorf("stat source DOCX: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("source DOCX is not a regular file")
	}
	return sourcePath, nil
}
