package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/core/blockmap"
	"github.com/paper-format-checker/backend/internal/core/cqrwst"
	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/paperparse"
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
	ErrInvalidJobID       = errors.New("invalid job id")
	ErrInvalidPaperUpload = errors.New("invalid paper upload")
	ErrServiceUnavailable = errors.New("paper workflow service unavailable")
)

const defaultWorkflowOutputRoot = "uploads/workflow_outputs"
const defaultCQRWSTTemplatePath = "uploads/template.docx"
const cqrwstTemplatePathEnv = "CQRWST_TEMPLATE_PATH"
const cqrwstTemplateTransplantEnabledEnv = "CQRWST_TEMPLATE_TRANSPLANT_ENABLED"

type WorkflowJobView struct {
	ID                 uuid.UUID `json:"id"`
	PaperID            uuid.UUID `json:"paper_id"`
	UserID             uuid.UUID `json:"user_id"`
	CompiledTemplateID uuid.UUID `json:"compiled_template_id"`
	Status             string    `json:"status"`
	Stage              string    `json:"stage"`
	DownloadPath       string    `json:"download_path"`
}

type CreatePaperJobInput struct {
	UserID   uuid.UUID
	Title    string
	FilePath string
	FileName string
	FileSize int64
	FileType string
}

type PaperWorkflowService interface {
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
		}
		if err := tx.Create(&paper).Error; err != nil {
			return err
		}

		compiled := model.CompiledTemplate{
			ID:                    templateID,
			SchoolID:              "single-template",
			TemplateName:          "single-template-runtime",
			TemplateVersion:       "runtime",
			SourceFilePath:        input.FilePath,
			SkeletonPath:          input.FilePath,
			ManifestJSON:          "{}",
			BlockCatalogJSON:      "[]",
			StyleProfilesJSON:     "{}",
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
	if err := s.db.WithContext(ctx).Preload("Paper").First(&job, "id = ? AND user_id = ?", jobID, userID).Error; err != nil {
		return nil, err
	}
	if strings.TrimSpace(job.Paper.FilePath) == "" {
		return nil, ErrInvalidPaperUpload
	}

	outputPath, err := s.workflowOutputPath(job.ID)
	if err != nil {
		return nil, err
	}
	profile, err := s.buildWorkflowOutput(ctx, job.Paper.FilePath, outputPath)
	if err != nil {
		return nil, err
	}
	ast, err := paperast.Extract(job.Paper.FilePath)
	if err != nil {
		return nil, err
	}
	rules := templatecontract.Build(profile)
	contract := repaircontract.Build(rules, ast)
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

	if shouldRunCQRWSTPostFix() {
		if _, err := cqrwst.FixDOCXWithTemplateProfileAndSemanticAI(ctx, outputPath, profile, newDeepSeekSemanticBlockClient()); err != nil {
			return nil, err
		}
	}

	verifier := verify.NewVerifierWithTemplateProfileAndClosure(profile, rules, ast, contract)
	if cqrwstTemplateTransplantEnabled() {
		verifier.WithoutCQRWSTRules()
	}
	result, err := workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
	if err != nil {
		return nil, err
	}
	if result.Status != workflow.StatusVerifiedPass && shouldRunCQRWSTPostFix() {
		if _, fixErr := cqrwst.FixDOCXWithTemplateProfileAndSemanticAI(ctx, outputPath, profile, newDeepSeekSemanticBlockClient()); fixErr == nil {
			result, err = workflow.NewLoopController(nil, nil, verifier).Run(ctx, workflow.RunInput{OutputPath: outputPath})
			if err != nil {
				return nil, err
			}
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

func (s *paperWorkflowService) buildWorkflowOutput(ctx context.Context, sourcePath string, outputPath string) (*templateprofile.Profile, error) {
	templatePath := resolveCQRWSTTemplatePath()
	if templatePath == "" {
		return nil, copyFile(sourcePath, outputPath)
	}
	profile := buildCQRWSTTemplateProfile(ctx, templatePath)
	if !cqrwstTemplateTransplantEnabled() {
		if err := copyFile(sourcePath, outputPath); err != nil {
			return profile, err
		}
		return profile, nil
	}

	compiled, err := templatecompile.NewCompiler().Compile(ctx, templatePath, templatecompile.CompileOptions{
		SchoolID:     "cqrwst",
		TemplateName: "cqrwst-single-template",
		Version:      "runtime",
		OutputDir:    filepath.Join(s.outputRoot, "_compiled_templates"),
	})
	if err != nil {
		return profile, copyFileWithTemplateFallbackNotice(sourcePath, outputPath, fmt.Errorf("compile CQRWST template skeleton: %w", err))
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
	}); err != nil {
		return profile, copyFileWithTemplateFallbackNotice(sourcePath, outputPath, fmt.Errorf("generate final paper from template skeleton: %w", err))
	}
	if missing := missingGeneratedSourceContent(ctx, parsed, outputPath); len(missing) > 0 {
		return profile, copyFileWithTemplateFallbackNotice(sourcePath, outputPath, fmt.Errorf("generated template output lost source content: %s", strings.Join(missing, " | ")))
	}

	return profile, nil
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

func buildCQRWSTTemplateProfile(ctx context.Context, templatePath string) *templateprofile.Profile {
	profile, err := templateprofile.Build(ctx, templatePath, templateprofile.Options{
		AIEnabled: deepSeekTemplateProfileEnabled(),
		AIClient:  newDeepSeekTemplateProfileClient(),
	})
	if err != nil {
		log.Printf("[CQRWST_TEMPLATE_PROFILE] build failed: %v", err)
		return nil
	}
	log.Printf("[CQRWST_TEMPLATE_PROFILE] built source=%s confidence=%.2f sections=%d styles=%d",
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
	fmt.Printf("[CQRWST_TEMPLATE] fallback to copy-and-fix route: %v\n", cause)
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
		if !strings.Contains(generatedText, sourceText) {
			missing = append(missing, sourceText)
			if len(missing) >= 10 {
				break
			}
		}
	}
	return missing
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
	return strings.Join(strings.Fields(text), " ")
}

func (s *paperWorkflowService) getJobView(query any, args ...any) (*WorkflowJobView, error) {
	var job model.PaperWorkflowJob
	conds := append([]any{query}, args...)
	if err := s.db.First(&job, conds...).Error; err != nil {
		return nil, err
	}

	return &WorkflowJobView{
		ID:                 job.ID,
		PaperID:            job.PaperID,
		UserID:             job.UserID,
		CompiledTemplateID: job.CompiledTemplateID,
		Status:             job.Status,
		Stage:              job.Stage,
		DownloadPath:       job.DownloadPath,
	}, nil
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
