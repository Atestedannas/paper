package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/core/blockmap"
	"github.com/paper-format-checker/backend/internal/core/cqrwst"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/paperparse"
	"github.com/paper-format-checker/backend/internal/core/renderverify"
	"github.com/paper-format-checker/backend/internal/core/repaircontract"
	"github.com/paper-format-checker/backend/internal/core/templatecompile"
	"github.com/paper-format-checker/backend/internal/core/templatecontract"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
	"github.com/paper-format-checker/backend/internal/core/transplant"
	"github.com/paper-format-checker/backend/internal/core/verify"
	"github.com/paper-format-checker/backend/internal/core/workflow"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/pkg/aiclassifier"
	"gorm.io/gorm"
)

var (
	ErrInvalidJobID        = errors.New("invalid job id")
	ErrInvalidPaperUpload  = errors.New("invalid paper upload")
	ErrTemplateDOCXMissing = errors.New("selected template has no readable DOCX file")
	ErrServiceUnavailable  = errors.New("paper workflow service unavailable")
)

const defaultWorkflowOutputRoot = "uploads/workflow_outputs"
const defaultCQRWSTTemplatePath = "uploads/template.docx"
const cqrwstTemplatePathEnv = "CQRWST_TEMPLATE_PATH"
const cqrwstTemplateTransplantEnabledEnv = "CQRWST_TEMPLATE_TRANSPLANT_ENABLED"

var renderedChineseTotalFooterPattern = regexp.MustCompile(`第(\d+)页共(\d+)页`)
var totalPagesFieldBlockPattern = regexp.MustCompile(`(?s)<w:r><w:fldChar\b[^>]*w:fldCharType="begin"[^>]*/></w:r>\s*<w:r><w:instrText\b[^>]*>\s*(?:NUMPAGES|SECTIONPAGES)\b.*?</w:instrText></w:r>\s*<w:r><w:fldChar\b[^>]*w:fldCharType="separate"[^>]*/></w:r>\s*<w:r><w:t\b[^>]*>.*?</w:t></w:r>\s*<w:r><w:fldChar\b[^>]*w:fldCharType="end"[^>]*/></w:r>`)
var materializedTotalPagePattern = regexp.MustCompile(`(?s)(共\s*</w:t></w:r>\s*<w:r><w:t\b[^>]*>)\d+(</w:t>)`)
var manualCaptionLinePattern = regexp.MustCompile(`^\s*([图表])\s*(\d+)(?:[-.．](\d+))?\s+(.+)$`)
var captionReferencePattern = regexp.MustCompile(`([图表])\s*(\d+)(?:[-.．](\d+))?`)
var continuedTableCaptionLinePattern = regexp.MustCompile(`^\s*\x{7eed}\x{8868}\s*(\d+)[-.\x{ff0e}](\d+)\s+(.+)$`)
var workflowParagraphPattern = regexp.MustCompile(`(?s)<w:p\b[^>]*>.*?</w:p>`)
var workflowTablePattern = regexp.MustCompile(`(?s)<w:tbl\b[^>]*>.*?</w:tbl>`)
var workflowRunPattern = regexp.MustCompile(`(?s)<w:r\b[^>]*>.*?</w:r>`)
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

type WorkflowJobView struct {
	ID                 uuid.UUID `json:"id"`
	PaperID            uuid.UUID `json:"paper_id"`
	UserID             uuid.UUID `json:"user_id"`
	CompiledTemplateID uuid.UUID `json:"compiled_template_id"`
	Status             string    `json:"status"`
	Stage              string    `json:"stage"`
	DownloadPath       string    `json:"download_path"`
	DownloadURL        string    `json:"download_url,omitempty"`
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
}

func NewPaperWorkflowService(db *gorm.DB) PaperWorkflowService {
	return NewPaperWorkflowServiceWithOutputRoot(db, defaultWorkflowOutputRoot)
}

func NewPaperWorkflowServiceWithOutputRoot(db *gorm.DB, outputRoot string) PaperWorkflowService {
	if strings.TrimSpace(outputRoot) == "" {
		outputRoot = defaultWorkflowOutputRoot
	}
	return &paperWorkflowService{db: db, outputRoot: outputRoot}
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
	templatePath := ""
	templateName := "single-template-runtime"
	templateVersion := "runtime"
	schoolID := "single-template"
	formatRules := ""
	if input.FormatTemplateID != uuid.Nil {
		var formatTemplate model.FormatTemplate
		if err := s.db.WithContext(ctx).Preload("University").First(&formatTemplate, "id = ? AND is_active = ?", input.FormatTemplateID, true).Error; err != nil {
			return nil, err
		}
		templatePath = WorkflowTemplatePath(formatTemplate)
		if templatePath == "" && formatTemplate.University != nil && strings.Contains(formatTemplate.University.Name, "重庆人文科技学院") {
			templatePath = resolveCQRWSTTemplatePath()
		}
		if templatePath == "" {
			return nil, ErrTemplateDOCXMissing
		}
		selectedTemplateID = &formatTemplate.ID
		templateName = formatTemplate.Name
		templateVersion = formatTemplate.Version
		schoolID = formatTemplate.TemplateID
		formatRules = formatTemplate.FormatRules
	} else if configured := resolveCQRWSTTemplatePath(); configured != "" {
		// Compatibility for older clients; new clients must submit template_id.
		templatePath = configured
	}

	var profile *templateprofile.Profile
	if templatePath != "" {
		profile = buildWorkflowTemplateProfile(ctx, templatePath)
		if profile == nil {
			return nil, fmt.Errorf("build selected template profile failed")
		}
		if err := templateprofile.ApplyFormatRules(profile, formatRules); err != nil {
			return nil, err
		}
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

		compiledSource := input.FilePath
		if templatePath != "" {
			compiledSource = templatePath
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

func (s *paperWorkflowService) RunJob(ctx context.Context, id string, userID uuid.UUID) (*WorkflowJobView, error) {
	if err := s.validateReady(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureWorkflowTables(ctx); err != nil {
		return nil, err
	}
	jobID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJobID, err)
	}

	var job model.PaperWorkflowJob
	if err := s.db.WithContext(ctx).Preload("Paper").Preload("CompiledTemplate").First(&job, "id = ? AND user_id = ?", jobID, userID).Error; err != nil {
		return nil, err
	}
	if strings.TrimSpace(job.Paper.FilePath) == "" {
		return nil, ErrInvalidPaperUpload
	}

	outputPath, err := s.workflowOutputPath(job.ID)
	if err != nil {
		return nil, err
	}
	profile, err := templateprofile.Parse(job.CompiledTemplate.StyleProfilesJSON)
	if err != nil {
		return nil, err
	}
	ast, err := paperast.Extract(job.Paper.FilePath)
	if err != nil {
		return nil, err
	}
	rules := templatecontract.Build(profile)
	contract := repaircontract.Build(rules, ast)
	profile, err = s.buildWorkflowOutput(ctx, job.Paper.FilePath, outputPath, job.CompiledTemplate, profile, &contract)
	if err != nil {
		return nil, err
	}
	if err := s.persistWorkflowContracts(ctx, job, profile, rules, ast, contract); err != nil {
		return nil, err
	}
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
		return s.GetJobForUser(id, userID)
	}
	contractBaseline, err := paperast.Extract(outputPath)
	if err != nil {
		return nil, err
	}
	contractBackup := outputPath + ".contract-backup"
	if contract.Blocks("visible_content_rewrite") {
		if err := copyFile(outputPath, contractBackup); err != nil {
			return nil, err
		}
		defer os.Remove(contractBackup)
	}

	transplantEnabled := templateTransplantEnabled(job.CompiledTemplate.SourceFilePath, profile)
	if !transplantEnabled {
		if _, err := cqrwst.FixDOCXWithTemplateProfileAndSemanticAI(ctx, outputPath, profile, newDeepSeekSemanticBlockClient()); err != nil {
			return nil, err
		}
	}

	verifier := verify.NewVerifierWithTemplateProfileAndClosure(profile, rules, ast, contract)
	if workflowNeedsTOCMaterialization(outputPath) {
		verifier.WithRenderGate(renderverify.Options{Enabled: true, Strict: false, CheckPageFooter: true, TextExtractor: workflowPDFTextExtractor()}, "")
	}
	if transplantEnabled {
		verifier.WithoutCQRWSTRules()
	}
	result, err := workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
	if err != nil {
		return nil, err
	}
	if repaired, repairErr := repairRenderedPageFooterTotal(outputPath, result.VerifyResult); repairErr != nil {
		return nil, repairErr
	} else if repaired {
		result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
		if err != nil {
			return nil, err
		}
	}
	if repaired, repairErr := repairManualCaptionFields(outputPath, result.VerifyResult); repairErr != nil {
		return nil, repairErr
	} else if repaired {
		result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
		if err != nil {
			return nil, err
		}
	}
	if repaired, repairErr := repairManualFormulaNumberFields(outputPath, result.VerifyResult); repairErr != nil {
		return nil, repairErr
	} else if repaired {
		result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
		if err != nil {
			return nil, err
		}
	}
	if repaired, repairErr := repairManualCrossReferenceFields(outputPath, result.VerifyResult); repairErr != nil {
		return nil, repairErr
	} else if repaired {
		result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
		if err != nil {
			return nil, err
		}
	}
	if repaired, repairErr := repairRenderedPageFooterTotal(outputPath, result.VerifyResult); repairErr != nil {
		return nil, repairErr
	} else if repaired {
		result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
		if err != nil {
			return nil, err
		}
	}
	if repaired, repairErr := repairRenderedTOCPageNumbers(outputPath, result.VerifyResult); repairErr != nil {
		return nil, repairErr
	} else if repaired {
		verifier.WithoutRenderGate()
		result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
		if err != nil {
			return nil, err
		}
	}
	if result.Status != workflow.StatusVerifiedPass && !transplantEnabled {
		if _, fixErr := cqrwst.FixDOCXWithTemplateProfileAndSemanticAI(ctx, outputPath, profile, newDeepSeekSemanticBlockClient()); fixErr == nil {
			result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
			if err != nil {
				return nil, err
			}
		}
	}
	if contract.Blocks("visible_content_rewrite") {
		finalAST, extractErr := paperast.Extract(outputPath)
		if extractErr != nil {
			if restoreErr := copyFile(contractBackup, outputPath); restoreErr != nil {
				return nil, fmt.Errorf("extract final paper: %v; restore backup: %w", extractErr, restoreErr)
			}
			return nil, extractErr
		}
		if issues := repaircontract.ValidateVisibleContentPreserved(contractBaseline, finalAST); len(issues) > 0 {
			if restoreErr := copyFile(contractBackup, outputPath); restoreErr != nil {
				return nil, fmt.Errorf("repair contract violation: %s; restore backup: %w", issues[0].Message, restoreErr)
			}
			return nil, fmt.Errorf("repair contract violation: %s", issues[0].Message)
		}
	}

	stage := workflow.StageManualReview
	downloadPath := outputPath
	if result.Status == workflow.StatusVerifiedPass {
		stage = workflow.StageVerified
	}
	if err := workflow.NewStore(s.db).UpdateJobResult(ctx, job.ID, result.Status, stage, downloadPath, result.VerifyResult); err != nil {
		return nil, err
	}

	return s.GetJobForUser(id, userID)
}

func workflowNeedsTOCMaterialization(outputPath string) bool {
	pkg, err := ooxmlpkg.Open(outputPath)
	if err != nil {
		return false
	}
	document, ok := pkg.Get("word/document.xml")
	return ok && strings.Contains(string(document), `TOC \o "1-3"`) && strings.Contains(string(document), `<w:t>0</w:t>`)
}

func workflowPDFTextExtractor() renderverify.TextExtractor {
	candidates := []string{strings.TrimSpace(os.Getenv("PDF_TEXT_PYTHON"))}
	for _, name := range []string{"python3", "python"} {
		if binary, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, binary)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".cache", "codex-runtimes", "codex-primary-runtime", "dependencies", "python", "python.exe"))
	}
	for _, binary := range candidates {
		if binary == "" {
			continue
		}
		if err := exec.Command(binary, "-c", "import pdfplumber").Run(); err == nil {
			return renderverify.PythonPDFTextExtractor{Binary: binary}
		}
	}
	return nil
}

func (s *paperWorkflowService) persistWorkflowContracts(ctx context.Context, job model.PaperWorkflowJob, profile *templateprofile.Profile, rules templatecontract.RuleSet, ast paperast.Snapshot, contract repaircontract.Contract) error {
	if err := s.db.WithContext(ctx).Model(&model.Paper{}).
		Where("id = ?", job.PaperID).
		Updates(map[string]any{
			"parsed_info": paperast.Marshal(ast),
			"updated_at":  time.Now().UTC(),
		}).Error; err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&model.CompiledTemplate{}).
		Where("id = ?", job.CompiledTemplateID).
		Updates(map[string]any{
			"style_profiles_json":     templateprofile.Marshal(profile),
			"verification_rules_json": templatecontract.Marshal(rules),
			"mapping_contract_json":   repaircontract.Marshal(contract),
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
	verifier := verify.NewVerifier().WithoutCQRWSTRules()
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
	prefix := "CQRWST_Fig"
	if label == "表" {
		prefix = "CQRWST_Tbl"
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

func (s *paperWorkflowService) validateReady(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrServiceUnavailable
	}
	return nil
}

func (s *paperWorkflowService) ensureWorkflowTables(ctx context.Context) error {
	if err := database.RepairLegacyFormatTemplateConstraints(s.db.WithContext(ctx)); err != nil {
		return err
	}
	if s.db.Migrator().HasTable(&model.CompiledTemplate{}) &&
		s.db.Migrator().HasTable(&model.PaperWorkflowJob{}) &&
		s.db.Migrator().HasTable(&model.PaperWorkflowIssue{}) {
		return nil
	}
	return s.db.WithContext(ctx).AutoMigrate(
		&model.CompiledTemplate{},
		&model.PaperWorkflowJob{},
		&model.PaperWorkflowIssue{},
	)
}

func (s *paperWorkflowService) workflowOutputPath(jobID uuid.UUID) (string, error) {
	root, err := filepath.Abs(filepath.Clean(s.outputRoot))
	if err != nil {
		return "", err
	}
	return filepath.Join(root, jobID.String(), "final.docx"), nil
}

func (s *paperWorkflowService) buildWorkflowOutput(ctx context.Context, sourcePath string, outputPath string, record model.CompiledTemplate, profile *templateprofile.Profile, contract *repaircontract.Contract) (*templateprofile.Profile, error) {
	templatePath := strings.TrimSpace(record.SourceFilePath)
	if templatePath == "" || templatePath == sourcePath || profile == nil {
		return nil, copyFile(sourcePath, outputPath)
	}
	if !templateTransplantEnabled(templatePath, profile) {
		if err := copyFile(sourcePath, outputPath); err != nil {
			return profile, err
		}
		return profile, nil
	}

	compiled, err := templatecompile.NewCompiler().Compile(ctx, templatePath, templatecompile.CompileOptions{
		SchoolID:     record.SchoolID,
		TemplateName: record.TemplateName,
		Version:      record.TemplateVersion,
		OutputDir:    filepath.Join(s.outputRoot, "_compiled_templates"),
	})
	if err != nil {
		return profile, copyFileWithTemplateFallbackNotice(sourcePath, outputPath, fmt.Errorf("compile selected template skeleton: %w", err))
	}

	parsed, err := paperparse.NewParser().Parse(ctx, sourcePath)
	if err != nil {
		return profile, copyFileWithTemplateFallbackNotice(sourcePath, outputPath, fmt.Errorf("parse source paper for template transplant: %w", err))
	}

	mapping, err := blockmap.NewMapper().Map(compiled, parsed)
	if err != nil {
		return profile, copyFileWithTemplateFallbackNotice(sourcePath, outputPath, fmt.Errorf("map source paper blocks to template skeleton: %w", err))
	}

	if err := transplant.NewTransplanter().Generate(ctx, transplant.GenerateInput{
		CompiledTemplate: compiled,
		Mapping:          mapping,
		OutputPath:       outputPath,
		RepairContract:   contract,
	}); err != nil {
		return profile, copyFileWithTemplateFallbackNotice(sourcePath, outputPath, fmt.Errorf("generate final paper from template skeleton: %w", err))
	}
	if err := preserveSourceDrawingGroups(sourcePath, outputPath); err != nil {
		return profile, copyFileWithTemplateFallbackNotice(sourcePath, outputPath, fmt.Errorf("preserve source drawings: %w", err))
	}
	if missing := missingGeneratedSourceContent(ctx, parsed, outputPath); len(missing) > 0 {
		return profile, copyFileWithTemplateFallbackNotice(sourcePath, outputPath, fmt.Errorf("generated template output lost source content: %s", strings.Join(missing, " | ")))
	}

	return profile, nil
}

func WorkflowTemplatePath(template model.FormatTemplate) string {
	for _, candidate := range []string{template.GoldenTemplatePath, template.FilePath} {
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

func templateTransplantEnabled(templatePath string, profile *templateprofile.Profile) bool {
	if strings.TrimSpace(templatePath) == "" || profile == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(cqrwstTemplateTransplantEnabledEnv))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func preserveSourceDrawingGroups(sourcePath string, outputPath string) error {
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
	for _, bounds := range workflowAlternateContentPattern.FindAllStringIndex(string(sourceXML), -1) {
		block := normalizeWorkflowDrawingWidth(string(sourceXML)[bounds[0]:bounds[1]])
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
				`<w:p><w:pPr><w:jc w:val="center"/><w:keepNext/></w:pPr><w:r>` + block + `</w:r></w:p>` +
				`<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:line="300" w:lineRule="auto"/></w:pPr>` +
				workflowTextRun(caption, `<w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体" w:hAnsi="宋体"/><w:sz w:val="21"/><w:szCs w:val="21"/></w:rPr>`) +
				`</w:p>`
		})
	}
	if updated == string(outputXML) {
		return nil
	}
	output.Set("word/document.xml", []byte(updated))
	return output.Write(outputPath)
}

func normalizeWorkflowDrawingWidth(block string) string {
	const maxWidth = 5800000
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
	profile, err := templateprofile.Build(ctx, templatePath, templateprofile.Options{
		AIEnabled: deepSeekTemplateProfileEnabled(),
		AIClient:  newDeepSeekTemplateProfileClient(),
	})
	if err != nil {
		log.Printf("[WORKFLOW_TEMPLATE_PROFILE] build failed: %v", err)
		return nil
	}
	log.Printf("[WORKFLOW_TEMPLATE_PROFILE] built source=%s confidence=%.2f sections=%d styles=%d",
		profile.Source, profile.Confidence, len(profile.Sections), len(profile.Styles))
	return profile
}

func deepSeekTemplateProfileEnabled() bool {
	creds := deepSeekCredentialsFromEnvOrFile()
	return creds.Enabled && creds.Cookie != ""
}

func newDeepSeekTemplateProfileClient() templateprofile.ChatClient {
	creds := deepSeekCredentialsFromEnvOrFile()
	if !creds.Enabled || creds.Cookie == "" {
		return nil
	}
	return aiclassifier.NewDeepSeekWebClient(creds.Cookie, creds.Bearer)
}

func newDeepSeekSemanticBlockClient() cqrwst.SemanticAIClient {
	creds := deepSeekCredentialsFromEnvOrFile()
	if !creds.Enabled || creds.Cookie == "" {
		return nil
	}
	return aiclassifier.NewDeepSeekWebClient(creds.Cookie, creds.Bearer)
}

func copyFileWithTemplateFallbackNotice(sourcePath string, outputPath string, cause error) error {
	rootCause := cause
	for errors.Unwrap(rootCause) != nil {
		rootCause = errors.Unwrap(rootCause)
	}
	log.Printf("component=workflow_template stage=fallback error=%q", rootCause.Error())
	return copyFile(sourcePath, outputPath)
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
	if view.Status == string(workflow.StatusVerifiedPass) {
		view.DownloadURL = workflowJobDownloadURL(view.ID)
	}
	return view, nil
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
