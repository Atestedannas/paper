package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	stdlog "log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/renderverify"
	"github.com/paper-format-checker/backend/internal/core/repaircontract"
	"github.com/paper-format-checker/backend/internal/core/templateapply"
	"github.com/paper-format-checker/backend/internal/core/templatecontract"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
	"github.com/paper-format-checker/backend/internal/core/verify"
	"github.com/paper-format-checker/backend/internal/core/workflow"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPaperWorkflowServiceCreatePaperJobPersistsPaperAndJob(t *testing.T) {
	db := openPaperWorkflowServiceTestDB(t)
	userID := uuid.New()
	inputPath := filepath.Join(t.TempDir(), "paper.docx")
	writeMinimalWorkflowDocx(t, inputPath, "hello v2 workflow")

	view, err := NewPaperWorkflowService(db).CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:   userID,
		Title:    "paper.docx",
		FilePath: inputPath,
		FileName: "paper.docx",
		FileSize: 123,
		FileType: "docx",
	})
	if err != nil {
		t.Fatalf("CreatePaperJob() error = %v", err)
	}

	if view.ID == uuid.Nil {
		t.Fatal("job ID is nil")
	}
	if view.PaperID == uuid.Nil {
		t.Fatal("paper ID is nil")
	}
	if view.UserID != userID {
		t.Fatalf("UserID = %s, want %s", view.UserID, userID)
	}
	if view.Status != string(workflow.StatusUploaded) {
		t.Fatalf("Status = %s, want %s", view.Status, workflow.StatusUploaded)
	}
	if view.Stage != "queued" {
		t.Fatalf("Stage = %s, want queued", view.Stage)
	}

	var paper model.Paper
	if err := db.First(&paper, "id = ?", view.PaperID).Error; err != nil {
		t.Fatalf("load paper: %v", err)
	}
	if paper.FilePath != inputPath {
		t.Fatalf("FilePath = %s, want %s", paper.FilePath, inputPath)
	}
}

func TestFormatRulesDebugEnabled(t *testing.T) {
	t.Setenv("PAPER_DEBUG_FORMAT_RULES", "yes")
	if !formatRulesDebugEnabled() {
		t.Fatal("PAPER_DEBUG_FORMAT_RULES=yes should enable template rule logging")
	}
	t.Setenv("PAPER_DEBUG_FORMAT_RULES", "0")
	if formatRulesDebugEnabled() {
		t.Fatal("PAPER_DEBUG_FORMAT_RULES=0 should disable template rule logging")
	}
}

func TestWorkflowVisualSpecAddsTemplateBoundaryRules(t *testing.T) {
	profile := &templateprofile.Profile{PageSetup: templateprofile.PageSetupRule{
		MarginTopTwips: "1417", MarginRightTwips: "1417", MarginBottomTwips: "1417", MarginLeftTwips: "1417",
	}}
	rules, ok := workflowVisualSpec("template", profile)["rules"].([]interface{})
	if !ok || len(rules) != 7 { // five text roles, plus table and image.
		t.Fatalf("visual rules = %#v", rules)
	}
	for _, raw := range rules {
		rule := raw.(map[string]interface{})
		if rule["property"] != "content_overflow" && rule["property"] != "table_image_boundary" {
			t.Fatalf("unexpected visual property: %#v", rule)
		}
		margins := rule["expected"].(map[string]interface{})["marginsMm"].(map[string]interface{})
		if got := margins["leftMm"].(float64); got < 24.9 || got > 25.1 {
			t.Fatalf("left margin = %v mm", got)
		}
	}
}

func TestWorkflowVisualSpecAddsConservativeHeaderFooterBounds(t *testing.T) {
	profile := &templateprofile.Profile{
		PageSetup: templateprofile.PageSetupRule{PageHeightTwips: "16838", HeaderMarginTwips: "907", FooterMarginTwips: "1191"},
		Header:    templateprofile.HeaderFooterRule{Exists: true},
		Footer:    templateprofile.HeaderFooterRule{Exists: true},
	}
	rules := workflowVisualSpec("template", profile)["rules"].([]interface{})
	if len(rules) != 2 {
		t.Fatalf("header/footer rules = %#v", rules)
	}
	for _, raw := range rules {
		rule := raw.(map[string]interface{})
		if rule["property"] != "header_footer_position" {
			t.Fatalf("unexpected visual rule: %#v", rule)
		}
		expected := rule["expected"].(map[string]interface{})
		switch rule["targetRole"] {
		case "header":
			if got := expected["maxBottomMm"].(float64); got < 21 || got > 23 {
				t.Fatalf("header bound = %v", got)
			}
		case "footer":
			if got := expected["minTopMm"].(float64); got < 269 || got > 271 {
				t.Fatalf("footer bound = %v", got)
			}
		default:
			t.Fatalf("unknown target role: %#v", rule)
		}
	}
}

func TestWorkflowVisualSpecAddsCaptionPositionRules(t *testing.T) {
	profile := &templateprofile.Profile{RulePack: templateprofile.RulePack{
		TableCaptionPosition: "above", FigureCaptionPosition: "below",
	}}
	rules := workflowVisualSpec("template", profile)["rules"].([]interface{})
	if len(rules) != 2 {
		t.Fatalf("caption rules = %#v", rules)
	}
	for _, raw := range rules {
		rule := raw.(map[string]interface{})
		if rule["property"] != "caption_position" {
			t.Fatalf("unexpected rule: %#v", rule)
		}
		expected := rule["expected"].(map[string]interface{})
		if expected["position"] == "above" && expected["relatedRole"] != "table" {
			t.Fatalf("table caption relation = %#v", expected)
		}
		if expected["position"] == "below" && expected["relatedRole"] != "image" {
			t.Fatalf("figure caption relation = %#v", expected)
		}
	}
}

func TestWorkflowVisualSpecFromRealTemplate(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("PAPER_TEMPLATE_DOCX"))
	if path == "" {
		t.Skip("set PAPER_TEMPLATE_DOCX to verify a real template profile")
	}
	profile, err := templateprofile.Extract(path)
	if err != nil {
		t.Fatalf("extract template profile: %v", err)
	}
	rules, ok := workflowVisualSpec("real-template", profile)["rules"].([]interface{})
	if !ok || len(rules) == 0 {
		t.Fatalf("real template produced no visual rules: page=%#v header=%#v footer=%#v", profile.PageSetup, profile.Header, profile.Footer)
	}
	t.Logf("real template visual rules=%d", len(rules))
}

func TestRoleFormatPlanRequiresReview(t *testing.T) {
	if roleFormatPlanRequiresReview(templateapply.FormatPlanItem{Apply: true, Trusted: true, Confidence: 0.55}) {
		t.Fatal("trusted deterministic role must not require review solely for low confidence")
	}
	if !roleFormatPlanRequiresReview(templateapply.FormatPlanItem{Apply: true, Confidence: 0.55}) {
		t.Fatal("untrusted low-confidence role must require review")
	}
	if !roleFormatPlanRequiresReview(templateapply.FormatPlanItem{Apply: false, Trusted: true, Confidence: 1}) {
		t.Fatal("non-applicable role must require review")
	}
}

func TestLogWorkflowTemplateProfile(t *testing.T) {
	t.Setenv("PAPER_DEBUG_FORMAT_RULES", "1")
	var output bytes.Buffer
	previous := stdlog.Writer()
	stdlog.SetOutput(&output)
	defer stdlog.SetOutput(previous)

	logWorkflowTemplateProfile("重庆工程学院本科论文格式标准", "template.docx", &templateprofile.Profile{
		Version: templateprofile.Version,
		Styles: map[string]templateprofile.StyleRule{
			"body": {FontEastAsia: "宋体", FontSizeHalfPt: "24"},
		},
	})

	for _, want := range []string{"[WORKFLOW_TEMPLATE_RULES]", "重庆工程学院本科论文格式标准", `"body"`, `"宋体"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("template profile log missing %q: %s", want, output.String())
		}
	}
}

func TestPaperWorkflowServiceUsesSelectedFormatTemplateFromCreationThroughOutput(t *testing.T) {
	db := openPaperWorkflowServiceTestDB(t)
	if err := db.Exec(`CREATE TABLE format_templates (
		id text PRIMARY KEY, template_id text, name text, university_id integer,
		document_type text, subject text, file_path text, source text, version text,
		is_public integer, is_active integer, format_rules text,
		parsed_from_paper_id text, parse_confidence real, usage_count integer,
		success_rate real, golden_template_path text, description text,
		created_at datetime, updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create format_templates table: %v", err)
	}

	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeWorkflowTemplateDocxWithReferenceBreak(t, templatePath)
	templateID := uuid.New()
	formatRules := `{"headings":{"level1":{"font_name":"AdminFont","font_size_pt":18}}}`
	if err := db.Exec(`INSERT INTO format_templates
		(id, template_id, name, file_path, golden_template_path, version, is_active, is_public, format_rules)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, templateID, "school-template", "Admin template", templatePath, templatePath, "1.0", true, true, formatRules).Error; err != nil {
		t.Fatalf("insert format template: %v", err)
	}

	inputPath := filepath.Join(t.TempDir(), "paper.docx")
	writeMinimalWorkflowDocx(t, inputPath, "1 Introduction")
	t.Setenv("CQRWST_TEMPLATE_TRANSPLANT_ENABLED", "false")
	t.Setenv("DEEPSEEK_ENABLED", "false")
	userID := uuid.New()
	outputRoot := t.TempDir()
	svc := NewPaperWorkflowServiceWithOutputRoot(db, outputRoot)
	created, err := svc.CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:           userID,
		FormatTemplateID: templateID,
		Title:            "paper.docx",
		FilePath:         inputPath,
		FileName:         "paper.docx",
		FileSize:         123,
		FileType:         "docx",
	})
	if err != nil {
		t.Fatalf("CreatePaperJob() error = %v", err)
	}

	var paper model.Paper
	if err := db.First(&paper, "id = ?", created.PaperID).Error; err != nil {
		t.Fatalf("load paper: %v", err)
	}
	if paper.SelectedTemplateID == nil || *paper.SelectedTemplateID != templateID {
		t.Fatalf("SelectedTemplateID = %v, want %s", paper.SelectedTemplateID, templateID)
	}
	var compiled model.CompiledTemplate
	if err := db.First(&compiled, "id = ?", created.CompiledTemplateID).Error; err != nil {
		t.Fatalf("load compiled template: %v", err)
	}
	if compiled.SourceFilePath != templatePath {
		t.Fatalf("SourceFilePath = %q, want %q", compiled.SourceFilePath, templatePath)
	}
	var profile templateprofile.Profile
	if err := json.Unmarshal([]byte(compiled.StyleProfilesJSON), &profile); err != nil {
		t.Fatalf("decode profile snapshot: %v", err)
	}
	if profile.Source != "local" || profile.AI != nil {
		t.Fatalf("workflow template profile must be deterministic OOXML only: source=%q ai=%#v", profile.Source, profile.AI)
	}
	if profile.Styles["heading_1"].FontEastAsia != "AdminFont" || profile.Styles["heading_1"].FontSizeHalfPt != "36" {
		t.Fatalf("administrator override missing from profile: %#v", profile.Styles["heading_1"])
	}

	// The first creation persists a full Profile JSON cache. A second creation
	// must still bind the school DOCX as the skeleton instead of the student file.
	createdFromCache, err := svc.CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:           userID,
		FormatTemplateID: templateID,
		Title:            "paper.docx",
		FilePath:         inputPath,
		FileName:         "paper.docx",
		FileSize:         123,
		FileType:         "docx",
	})
	if err != nil {
		t.Fatalf("CreatePaperJob() with cached Profile error = %v", err)
	}
	var compiledFromCache model.CompiledTemplate
	if err := db.First(&compiledFromCache, "id = ?", createdFromCache.CompiledTemplateID).Error; err != nil {
		t.Fatalf("load cached compiled template: %v", err)
	}
	if compiledFromCache.SourceFilePath != templatePath {
		t.Fatalf("cached Profile changed skeleton to %q, want %q", compiledFromCache.SourceFilePath, templatePath)
	}

	view, err := svc.RunJob(context.Background(), created.ID.String(), userID)
	if err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	outputPath := view.DownloadPath
	if outputPath == "" {
		outputPath = filepath.Join(outputRoot, created.ID.String(), "final.docx")
	}
	if documentXML := readWorkflowDocumentXML(t, outputPath); !strings.Contains(documentXML, `w:eastAsia="AdminFont"`) || !strings.Contains(documentXML, `w:sz w:val="36"`) {
		t.Fatalf("output did not use the persisted profile: %s", documentXML)
	}
}

func TestPaperWorkflowServiceRealSelectedTemplateFixture(t *testing.T) {
	templatePath := strings.TrimSpace(os.Getenv("PAPER_REAL_TEMPLATE_DOCX"))
	inputPath := strings.TrimSpace(os.Getenv("PAPER_REAL_INPUT_DOCX"))
	if templatePath == "" || inputPath == "" {
		t.Skip("set PAPER_REAL_TEMPLATE_DOCX and PAPER_REAL_INPUT_DOCX")
	}
	profile, err := templateprofile.Extract(templatePath)
	if err != nil {
		t.Fatalf("extract real template: %v", err)
	}
	db := openPaperWorkflowServiceTestDB(t)
	if err := db.Exec(`CREATE TABLE format_templates (
		id text PRIMARY KEY, template_id text, name text, university_id integer,
		document_type text, subject text, file_path text, source text, version text,
		is_public integer, is_active integer, format_rules text,
		parsed_from_paper_id text, parse_confidence real, usage_count integer,
		success_rate real, golden_template_path text, description text,
		created_at datetime, updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create format_templates: %v", err)
	}
	templateID := uuid.New()
	if err := db.Create(&model.FormatTemplate{
		ID: templateID, TemplateID: "real-selected-template", Name: filepath.Base(templatePath),
		FilePath: templatePath, GoldenTemplatePath: templatePath, Version: "1",
		IsActive: true, IsPublic: true, FormatRules: templateprofile.Marshal(profile),
	}).Error; err != nil {
		t.Fatalf("insert real template: %v", err)
	}
	outputRoot := strings.TrimSpace(os.Getenv("PAPER_REAL_OUTPUT_ROOT"))
	if outputRoot == "" {
		outputRoot = t.TempDir()
	}
	t.Setenv("CQRWST_TEMPLATE_TRANSPLANT_ENABLED", "true")
	t.Setenv("DEEPSEEK_ENABLED", "false")
	userID := uuid.New()
	svc := NewPaperWorkflowServiceWithOutputRoot(db, outputRoot)
	inputInfo, err := os.Stat(inputPath)
	if err != nil {
		t.Fatalf("stat real input: %v", err)
	}
	created, err := svc.CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID: userID, FormatTemplateID: templateID, Title: filepath.Base(inputPath),
		FilePath: inputPath, FileName: filepath.Base(inputPath), FileSize: inputInfo.Size(), FileType: "docx",
	})
	if err != nil {
		t.Fatalf("create real paper job: %v", err)
	}
	var compiled model.CompiledTemplate
	if err := db.First(&compiled, "id = ?", created.CompiledTemplateID).Error; err != nil {
		t.Fatalf("load real compiled template: %v", err)
	}
	if compiled.SourceFilePath != templatePath {
		t.Fatalf("real selected skeleton = %q, want %q", compiled.SourceFilePath, templatePath)
	}
	view, err := svc.RunJob(context.Background(), created.ID.String(), userID)
	if err != nil {
		t.Fatalf("run real paper job: %v", err)
	}
	if view.Status != string(workflow.StatusVerifiedPass) || view.Stage != workflow.StageVerified {
		t.Fatalf("real workflow status/stage = %s/%s, want %s/%s", view.Status, view.Stage, workflow.StatusVerifiedPass, workflow.StageVerified)
	}
	outputBytes, err := os.ReadFile(view.DownloadPath)
	if err != nil {
		t.Fatalf("read real output: %v", err)
	}
	inputBytes, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("read real input: %v", err)
	}
	if bytes.Equal(inputBytes, outputBytes) {
		t.Fatal("real output is still a byte-identical copy of the student paper")
	}
	t.Logf("real workflow output: %s", view.DownloadPath)
}

func TestPaperWorkflowServiceRejectsSelectedTemplateWithoutDOCX(t *testing.T) {
	db := openPaperWorkflowServiceTestDB(t)
	if err := db.Exec(`CREATE TABLE format_templates (
		id text PRIMARY KEY, template_id text, name text, university_id integer,
		document_type text, subject text, file_path text, source text, version text,
		is_public integer, is_active integer, format_rules text,
		parsed_from_paper_id text, parse_confidence real, usage_count integer,
		success_rate real, golden_template_path text, description text,
		created_at datetime, updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create format_templates table: %v", err)
	}

	templateID := uuid.New()
	formatRules := `{"headings":{"level1":{"font_name":"AdminFont","font_size_pt":18}}}`
	if err := db.Exec(`INSERT INTO format_templates
		(id, template_id, name, version, is_active, is_public, format_rules)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, templateID, "rules-only-template", "Rules only", "1.0", true, true, formatRules).Error; err != nil {
		t.Fatalf("insert format template: %v", err)
	}

	inputPath := filepath.Join(t.TempDir(), "paper.docx")
	writeMinimalWorkflowDocx(t, inputPath, "1 Introduction")
	t.Setenv("DEEPSEEK_ENABLED", "false")
	_, err := NewPaperWorkflowService(db).CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:           uuid.New(),
		FormatTemplateID: templateID,
		FilePath:         inputPath,
		FileName:         "paper.docx",
		FileType:         "docx",
	})
	if !errors.Is(err, ErrTemplateDOCXMissing) {
		t.Fatalf("CreatePaperJob() error = %v, want ErrTemplateDOCXMissing", err)
	}
}

func TestPaperWorkflowServiceRunJobWithoutTemplateRequiresManualReview(t *testing.T) {
	db := openPaperWorkflowServiceTestDB(t)
	outputRoot := t.TempDir()
	inputPath := filepath.Join(t.TempDir(), "paper.docx")
	writeMinimalWorkflowDocx(t, inputPath, "hello v2 workflow")
	userID := uuid.New()

	svc := NewPaperWorkflowServiceWithOutputRoot(db, outputRoot)
	created, err := svc.CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:   userID,
		Title:    "paper.docx",
		FilePath: inputPath,
		FileName: "paper.docx",
		FileSize: 123,
		FileType: "docx",
	})
	if err != nil {
		t.Fatalf("CreatePaperJob() error = %v", err)
	}

	view, err := svc.RunJob(context.Background(), created.ID.String(), userID)
	if err == nil || !strings.Contains(err.Error(), "selected template path incorrectly points to the student paper") {
		t.Fatalf("RunJob() error = %v, want independent-template rejection (view=%#v)", err, view)
	}
}

func TestPaperWorkflowServiceRunJobWithoutTemplateDoesNotApplyCQRWSTFixes(t *testing.T) {
	t.Setenv("CQRWST_ALLOW_CONTENT_NORMALIZATION", "true")
	db := openPaperWorkflowServiceTestDB(t)
	outputRoot := t.TempDir()
	inputPath := filepath.Join(t.TempDir(), "paper.docx")
	writeMinimalWorkflowDocx(t, inputPath, "1.1研究背景")
	userID := uuid.New()

	svc := NewPaperWorkflowServiceWithOutputRoot(db, outputRoot)
	created, err := svc.CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:   userID,
		Title:    "paper.docx",
		FilePath: inputPath,
		FileName: "paper.docx",
		FileSize: 123,
		FileType: "docx",
	})
	if err != nil {
		t.Fatalf("CreatePaperJob() error = %v", err)
	}

	view, err := svc.RunJob(context.Background(), created.ID.String(), userID)
	if err != nil {
		if strings.Contains(err.Error(), "selected template path incorrectly points to the student paper") {
			return
		}
		t.Fatalf("RunJob() error = %v", err)
	}
	if view.Status != string(workflow.StatusManualReview) {
		t.Fatalf("Status = %s, want %s", view.Status, workflow.StatusManualReview)
	}

	outputPath := filepath.Join(outputRoot, created.ID.String(), "final.docx")
	documentXML := readWorkflowDocumentXML(t, outputPath)
	if strings.Contains(documentXML, "1.1 研究背景") {
		t.Fatalf("no-template workflow should not apply school-specific CQRWST text fixes: %s", documentXML)
	}
}

func TestShouldRunCQRWSTPostFixIsDisabledForTemplateTransplant(t *testing.T) {
	t.Setenv("CQRWST_TEMPLATE_TRANSPLANT_ENABLED", "true")
	if shouldRunCQRWSTPostFix() {
		t.Fatal("CQRWST post-fix should be disabled when template transplant is enabled")
	}
}

func TestStructureRequiresInPlaceRepairForGenericProtectedOOXML(t *testing.T) {
	if !structureRequiresInPlaceRepair(repaircontract.StructureSnapshot{
		ProtectedPartHashes: map[string]int{"part": 1},
	}) {
		t.Fatal("a protected package part must disable skeleton reconstruction")
	}
	if !structureRequiresInPlaceRepair(repaircontract.StructureSnapshot{
		RelationshipTargets: map[string]int{"relationship": 1},
	}) {
		t.Fatal("a protected relationship target must disable skeleton reconstruction")
	}
}

func TestTemplatePathEnablesTransplantByDefault(t *testing.T) {
	t.Setenv("CQRWST_TEMPLATE_PATH", filepath.Join(t.TempDir(), "template.docx"))
	t.Setenv("CQRWST_TEMPLATE_TRANSPLANT_ENABLED", "")

	if !cqrwstTemplateTransplantEnabled() {
		t.Fatal("template transplant should default on when CQRWST_TEMPLATE_PATH is configured")
	}
}

func TestDefaultTemplatePathEnablesTransplantByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)
	t.Setenv("CQRWST_TEMPLATE_PATH", "")
	t.Setenv("CQRWST_TEMPLATE_TRANSPLANT_ENABLED", "")
	if err := os.MkdirAll("uploads", 0755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(defaultCQRWSTTemplatePath, []byte("template"), 0644); err != nil {
		t.Fatalf("write default template: %v", err)
	}

	if got := resolveCQRWSTTemplatePath(); got != defaultCQRWSTTemplatePath {
		t.Fatalf("resolveCQRWSTTemplatePath() = %q, want %q", got, defaultCQRWSTTemplatePath)
	}
	if !cqrwstTemplateTransplantEnabled() {
		t.Fatal("template transplant should default on when uploads/template.docx exists")
	}
}

func TestTemplateTransplantCanBeExplicitlyDisabledWithDefaultTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)
	t.Setenv("CQRWST_TEMPLATE_PATH", "")
	t.Setenv("CQRWST_TEMPLATE_TRANSPLANT_ENABLED", "false")
	if err := os.MkdirAll("uploads", 0755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(defaultCQRWSTTemplatePath, []byte("template"), 0644); err != nil {
		t.Fatalf("write default template: %v", err)
	}

	if cqrwstTemplateTransplantEnabled() {
		t.Fatal("template transplant should respect explicit disable")
	}
}

func TestWorkflowJSONReturnsMarshalError(t *testing.T) {
	if _, err := workflowJSON(make(chan int)); err == nil {
		t.Fatal("workflowJSON() error = nil, want unsupported type error")
	}
}

func TestPaperWorkflowServiceRunJobUsesDefaultTemplateForRealFixture(t *testing.T) {
	t.Skip("legacy default-template route removed; use selected-template regression")
	repoRoot := workflowServiceRepoRoot(t)
	sourcePath := filepath.Join(repoRoot, "uploads", "user.docx")
	templatePath := filepath.Join(repoRoot, defaultCQRWSTTemplatePath)
	if _, err := os.Stat(sourcePath); err != nil {
		t.Skipf("real user fixture missing: %v", err)
	}
	if _, err := os.Stat(templatePath); err != nil {
		t.Skipf("real template fixture missing: %v", err)
	}

	db := openPaperWorkflowServiceTestDB(t)
	outputRoot := t.TempDir()
	t.Chdir(repoRoot)
	t.Setenv("CQRWST_TEMPLATE_PATH", "")
	t.Setenv("CQRWST_TEMPLATE_TRANSPLANT_ENABLED", "")
	t.Setenv("CQRWST_ALLOW_CONTENT_NORMALIZATION", "true")
	t.Setenv("DEEPSEEK_ENABLED", "false")
	userID := uuid.New()

	svc := NewPaperWorkflowServiceWithOutputRoot(db, outputRoot)
	created, err := svc.CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:   userID,
		Title:    "user.docx",
		FilePath: sourcePath,
		FileName: "user.docx",
		FileSize: 123,
		FileType: "docx",
	})
	if err != nil {
		t.Fatalf("CreatePaperJob() error = %v", err)
	}
	view, err := svc.RunJob(context.Background(), created.ID.String(), userID)
	if err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	job := loadWorkflowJob(t, db, view.ID)
	var verifyResult verify.Result
	if err := json.Unmarshal([]byte(job.VerifyResultJSON), &verifyResult); err != nil {
		t.Fatalf("VerifyResultJSON is invalid: %v", err)
	}
	if view.Status != string(workflow.StatusVerifiedPass) {
		t.Fatalf("Status = %s, want %s, verify=%s", view.Status, workflow.StatusVerifiedPass, job.VerifyResultJSON)
	}
	if renderverify.DefaultEnabled() {
		if verifyResult.RenderResult == nil || !verifyResult.RenderResult.Enabled || !verifyResult.RenderResult.Passed || verifyResult.RenderResult.PageCount == 0 {
			t.Fatalf("render result should pass for real CQRWST fixture: %#v", verifyResult.RenderResult)
		}
		if len(verifyResult.RenderResult.Issues) != 0 || hasWorkflowIssue(verifyResult.RepairableIssues, "render_page_footer_total_mismatch") {
			t.Fatalf("render result should not leave footer/page issues: render=%#v repairable=%#v", verifyResult.RenderResult.Issues, verifyResult.RepairableIssues)
		}
		renderedPageTexts := workflowRenderedPageTexts(t, verifyResult.RenderResult)
		assertWorkflowRenderedTOCHasPageNumbers(t, renderedPageTexts)
		assertWorkflowRenderedHeadingsAreNotDoubleNumbered(t, renderedPageTexts)
		assertWorkflowRenderedHeadingNumbersHaveSpacing(t, renderedPageTexts)
		assertWorkflowRenderedTextHasNoTemplateResidue(t, renderedPageTexts)
	}
	for _, kind := range []string{"manual_caption_not_dynamic", "manual_cross_reference"} {
		if hasWorkflowIssue(verifyResult.FatalIssues, kind) || hasWorkflowIssue(verifyResult.RepairableIssues, kind) {
			t.Fatalf("advisory issue %s must not block a schema- and structure-valid download: fatal=%#v repairable=%#v", kind, verifyResult.FatalIssues, verifyResult.RepairableIssues)
		}
	}

	outputPath := filepath.Join(outputRoot, created.ID.String(), "final.docx")
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(outputBytes) == string(sourceBytes) {
		t.Fatal("workflow output is identical to uploaded source; template transplant did not run")
	}

	documentXML := readWorkflowDocumentXML(t, outputPath)
	for _, want := range []string{"<w:pict", "<w:tblpPr"} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("generated output should preserve template structure %q: %s", want, documentXML)
		}
	}
	documentText := workflowDocumentText(documentXML)
	for _, want := range []string{"护理学院", "护理学", "2022级护理学5班", "20220152192", "冉怡琴", "杨严政", "认知现状及影响因素分析"} {
		if !strings.Contains(documentText, want) {
			t.Fatalf("generated output should preserve visible cover field %q: %s", want, documentText)
		}
	}
	for _, forbidden := range []string{
		"202X",
		"20XX",
		"XXXX",
		"\u5c01\u9762\u683c\u5f0f\u4e0d\u8981\u8c03\u6574",
		"\u9009\u9898\u9898\u76ee\u4e00\u822c\u4e0d\u8d85\u8fc7",
		"\u6b63\u6587\u683c\u5f0f\u8303\u4f8b",
		"\u9644\u5f55A",
		"\u9644\u5f55B",
		"\u65e0\u9644\u5f55\u5185\u5bb9\u53ef\u5220\u9664",
	} {
		if strings.Contains(documentXML, forbidden) {
			t.Fatalf("generated output should remove cover placeholder/instruction %q: %s", forbidden, documentXML)
		}
	}
	abstractIndex := strings.Index(documentText, "摘要：目的")
	frontTitleIndex := strings.Index(documentText, "社区2型糖尿病患者疾病知识")
	if frontTitleIndex < 0 || abstractIndex < 0 || frontTitleIndex > abstractIndex {
		t.Fatalf("generated output should keep the front-matter title before the abstract: %s", documentXML)
	}
	if len(workflowParagraphs(documentXML)) < 25 {
		t.Fatal("generated output should retain the source cover and front matter")
	}
}

func TestRepairRenderedPageFooterTotalMaterializesBodyTotal(t *testing.T) {
	tmpDir := t.TempDir()
	docxPath := filepath.Join(tmpDir, "paper.docx")
	writeWorkflowDocxPackage(t, docxPath, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/footer3.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>第 </w:t></w:r>` +
			`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE \* MERGEFORMAT </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>18</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>` +
			`<w:r><w:t> 页 共 </w:t></w:r>` +
			`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> NUMPAGES \* MERGEFORMAT </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>24</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>` +
			`<w:r><w:t> 页</w:t></w:r></w:p></w:ftr>`,
	})

	repaired, err := repairRenderedPageFooterTotal(docxPath, verify.Result{
		RepairableIssues: []verify.Issue{{Kind: "render_page_footer_total_mismatch"}},
		RenderResult:     &renderverify.Result{PageTexts: []string{"正文 第1页 共24页", "致谢 第18页 共24页"}},
	})
	if err != nil {
		t.Fatalf("repairRenderedPageFooterTotal() error = %v", err)
	}
	if !repaired {
		t.Fatal("repairRenderedPageFooterTotal() repaired = false")
	}
	footerXML := readWorkflowDocxEntry(t, docxPath, "word/footer3.xml")
	if strings.Contains(footerXML, "NUMPAGES") || !strings.Contains(footerXML, ">18<") {
		t.Fatalf("footer should materialize body total 18 and remove NUMPAGES: %s", footerXML)
	}
}

func TestRepairRenderedPageFooterTotalUpdatesMaterializedBodyTotal(t *testing.T) {
	tmpDir := t.TempDir()
	docxPath := filepath.Join(tmpDir, "paper.docx")
	writeWorkflowDocxPackage(t, docxPath, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/footer3.xml":    `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>第 </w:t></w:r><w:r><w:t>1</w:t></w:r><w:r><w:t> 页 共 </w:t></w:r><w:r><w:t>18</w:t></w:r><w:r><w:t> 页</w:t></w:r></w:p></w:ftr>`,
	})

	repaired, err := repairRenderedPageFooterTotal(docxPath, verify.Result{
		RepairableIssues: []verify.Issue{{Kind: "render_page_footer_total_mismatch"}},
		RenderResult:     &renderverify.Result{PageTexts: []string{"正文 第1页 共18页", "致谢 第19页 共18页"}},
	})
	if err != nil {
		t.Fatalf("repairRenderedPageFooterTotal() error = %v", err)
	}
	if !repaired {
		t.Fatal("repairRenderedPageFooterTotal() repaired = false")
	}
	footerXML := readWorkflowDocxEntry(t, docxPath, "word/footer3.xml")
	if !strings.Contains(footerXML, ">19<") || strings.Contains(footerXML, ">18<") {
		t.Fatalf("footer should update materialized body total to 19: %s", footerXML)
	}
}

func TestRepairRenderedTOCPageNumbersMaterializesCachedEntries(t *testing.T) {
	tmpDir := t.TempDir()
	docxPath := filepath.Join(tmpDir, "paper.docx")
	writeWorkflowDocxPackage(t, docxPath, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> TOC \o "1-3" \h \z \u </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r></w:p>` +
			`<w:p><w:r><w:t>1 ` + "\u7eea\u8bba" + `</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>0</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>1.1 ` + "\u7814\u7a76\u80cc\u666f" + `</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>0</w:t></w:r></w:p>` +
			`<w:p><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>` +
			`</w:body></w:document>`,
	})

	repaired, err := repairRenderedTOCPageNumbers(docxPath, verify.Result{
		RenderResult: &renderverify.Result{PageTexts: []string{
			"\u76ee\u5f55",
			"1 1 \u7eea\u8bba\n1.1 1.1 \u7814\u7a76\u80cc\u666f\n\u7b2c1\u9875 \u517119\u9875",
		}},
	})
	if err != nil {
		t.Fatalf("repairRenderedTOCPageNumbers() error = %v", err)
	}
	if !repaired {
		t.Fatal("repairRenderedTOCPageNumbers() repaired = false")
	}
	documentXML := readWorkflowDocxEntry(t, docxPath, "word/document.xml")
	for _, want := range []string{"1 \u7eea\u8bba", "1.1 \u7814\u7a76\u80cc\u666f", "<w:tab/>", ">1<"} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("TOC cache missing %q: %s", want, documentXML)
		}
	}
}

func TestRepairManualCaptionFieldsConvertsTableCaptionToSEQ(t *testing.T) {
	tmpDir := t.TempDir()
	docxPath := filepath.Join(tmpDir, "paper.docx")
	writeWorkflowDocxPackage(t, docxPath, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:rPr><w:b/></w:rPr><w:t>表3-1 社区2型糖尿病患者基本特征分布</w:t></w:r></w:p></w:body></w:document>`,
	})

	repaired, err := repairManualCaptionFields(docxPath, verify.Result{
		Warnings: []verify.Issue{{Kind: "manual_caption_not_dynamic"}},
	})
	if err != nil {
		t.Fatalf("repairManualCaptionFields() error = %v", err)
	}
	if !repaired {
		t.Fatal("repairManualCaptionFields() repaired = false")
	}
	documentXML := readWorkflowDocxEntry(t, docxPath, "word/document.xml")
	for _, want := range []string{"SEQ 表", `\s 1`, ">表3-<", ">1<", "社区2型糖尿病患者基本特征分布", "<w:b/>"} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("document XML missing %q: %s", want, documentXML)
		}
	}
	if strings.Contains(documentXML, "<w:rPr><w:bookmarkStart") {
		t.Fatalf("bookmark should not be written inside run properties: %s", documentXML)
	}
}

func TestReplaceManualCaptionFieldsLinksContinuedCaptionToPreviousTable(t *testing.T) {
	documentXML := `<w:document><w:body>` +
		`<w:p><w:r><w:t>` + "\u88684-2 \u56de\u5f52\u5206\u6790" + `</w:t></w:r></w:p>` +
		`<w:tbl/>` +
		`<w:p><w:r><w:t>` + "\u7eed\u88684-2 \u56de\u5f52\u5206\u6790" + `</w:t></w:r></w:p>` +
		`</w:body></w:document>`

	updated, changed := replaceManualCaptionFields(documentXML)
	if !changed {
		t.Fatal("replaceManualCaptionFields() changed = false")
	}
	for _, want := range []string{
		`SEQ ` + "\u8868",
		`w:name="_Template_Tbl_4_2"`,
		`REF _Template_Tbl_4_2`,
		`>` + "\u7eed" + `<`,
		`>` + "\u88684-2" + `<`,
		`<w:bookmarkEnd w:id="1"/><w:r><w:t xml:space="preserve"> ` + "\u56de\u5f52\u5206\u6790",
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated XML missing %q: %s", want, updated)
		}
	}
}

func TestRepairManualCrossReferenceFieldsConvertsTableReferenceToREF(t *testing.T) {
	tmpDir := t.TempDir()
	docxPath := filepath.Join(tmpDir, "paper.docx")
	writeWorkflowDocxPackage(t, docxPath, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:bookmarkStart w:id="1" w:name="_CQRWST_Tbl_3_2"/><w:r><w:t>表3-2 疾病认知水平整体现状</w:t></w:r><w:bookmarkEnd w:id="1"/></w:p>` +
			`<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>患者整体认知水平较低。见表3-2</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})

	repaired, err := repairManualCrossReferenceFields(docxPath, verify.Result{
		Warnings: []verify.Issue{{Kind: "manual_cross_reference"}},
	})
	if err != nil {
		t.Fatalf("repairManualCrossReferenceFields() error = %v", err)
	}
	if !repaired {
		t.Fatal("repairManualCrossReferenceFields() repaired = false")
	}
	documentXML := readWorkflowDocxEntry(t, docxPath, "word/document.xml")
	for _, want := range []string{"REF _CQRWST_Tbl_3_2", ">表3-2<", "患者整体认知水平较低。见", "<w:b/>"} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("document XML missing %q: %s", want, documentXML)
		}
	}
}

func TestReplaceManualFormulaNumberFields(t *testing.T) {
	documentXML := `<w:document><w:body><w:tbl><w:tr>` +
		`<w:tc><w:p><m:oMath><m:r><m:t>E=mc2</m:t></m:r></m:oMath></w:p></w:tc>` +
		`<w:tc><w:p><w:pPr><w:jc w:val="right"/></w:pPr><w:r><w:rPr><w:b/></w:rPr><w:t>(2-1)</w:t></w:r></w:p></w:tc>` +
		`</w:tr></w:tbl></w:body></w:document>`

	updated, changed := replaceManualFormulaNumberFields(documentXML)
	if !changed {
		t.Fatal("replaceManualFormulaNumberFields() changed = false")
	}
	for _, want := range []string{" SEQ 公式 ", `\s 1`, `w:fldCharType="begin"`, "(2-"} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated formula missing %q: %s", want, updated)
		}
	}
}

func TestPaperWorkflowServiceRunJobUsesConfiguredTemplateSkeleton(t *testing.T) {
	t.Skip("legacy skeleton transplant route removed; production uses in-place selected-template rules")
	db := openPaperWorkflowServiceTestDB(t)
	outputRoot := t.TempDir()
	inputPath := filepath.Join(t.TempDir(), "paper.docx")
	writeWorkflowDocxParagraphs(t, inputPath, []string{
		"1 Introduction",
		"Body from parsed source",
	})
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeWorkflowTemplateDocx(t, templatePath)
	t.Setenv("CQRWST_TEMPLATE_PATH", templatePath)
	t.Setenv("CQRWST_TEMPLATE_TRANSPLANT_ENABLED", "true")
	t.Setenv("DEEPSEEK_ENABLED", "false")
	userID := uuid.New()

	svc := NewPaperWorkflowServiceWithOutputRoot(db, outputRoot)
	created, err := svc.CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:   userID,
		Title:    "paper.docx",
		FilePath: inputPath,
		FileName: "paper.docx",
		FileSize: 123,
		FileType: "docx",
	})
	if err != nil {
		t.Fatalf("CreatePaperJob() error = %v", err)
	}

	view, err := svc.RunJob(context.Background(), created.ID.String(), userID)
	if err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	if view.Status != string(workflow.StatusVerifiedPass) {
		t.Fatalf("Status = %s, want %s", view.Status, workflow.StatusVerifiedPass)
	}

	outputPath := view.DownloadPath
	if outputPath == "" {
		outputPath = filepath.Join(outputRoot, created.ID.String(), "final.docx")
	}
	documentXML := readWorkflowDocumentXML(t, outputPath)
	for _, want := range []string{"TEMPLATE-SKELETON-MARKER", "Introduction", "Body from parsed source"} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("download docx missing %q: %s", want, documentXML)
		}
	}
	if strings.Contains(documentXML, "{{content_blocks}}") {
		t.Fatalf("download docx still contains template placeholders: %s", documentXML)
	}
}

func TestPaperWorkflowServiceRunJobIgnoresTemplateReviewMarkup(t *testing.T) {
	t.Skip("legacy skeleton transplant route removed; template review markup is never transplanted")
	db := openPaperWorkflowServiceTestDB(t)
	outputRoot := t.TempDir()
	inputPath := filepath.Join(t.TempDir(), "paper.docx")
	writeWorkflowDocxParagraphs(t, inputPath, []string{
		"1 Introduction",
		"Body from parsed source",
	})
	templatePath := filepath.Join(t.TempDir(), "template-review.docx")
	writeWorkflowDocxPackage(t, templatePath, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`<Override PartName="/word/comments.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"/>` +
			`</Types>`,
		"_rels/.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdComments" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="comments.xml"/>` +
			`</Relationships>`,
		"word/settings.xml": `<w:settings></w:settings>`,
		"word/comments.xml": `<w:comments><w:comment w:id="0"><w:p><w:r><w:t>review note</w:t></w:r></w:p></w:comment></w:comments>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:commentRangeStart w:id="0"/><w:r><w:t>TEMPLATE-SKELETON-MARKER</w:t></w:r>` +
			`<w:ins><w:r><w:t>accepted review text</w:t></w:r></w:ins>` +
			`<w:del><w:r><w:delText>deleted review text</w:delText></w:r></w:del>` +
			`<w:r><w:commentReference w:id="0"/></w:r><w:commentRangeEnd w:id="0"/></w:p>` +
			`<w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})
	t.Setenv("CQRWST_TEMPLATE_PATH", templatePath)
	t.Setenv("CQRWST_TEMPLATE_TRANSPLANT_ENABLED", "true")
	t.Setenv("DEEPSEEK_ENABLED", "false")
	userID := uuid.New()

	svc := NewPaperWorkflowServiceWithOutputRoot(db, outputRoot)
	created, err := svc.CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:   userID,
		Title:    "paper.docx",
		FilePath: inputPath,
		FileName: "paper.docx",
		FileSize: 123,
		FileType: "docx",
	})
	if err != nil {
		t.Fatalf("CreatePaperJob() error = %v", err)
	}

	view, err := svc.RunJob(context.Background(), created.ID.String(), userID)
	if err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	if view.Status != string(workflow.StatusVerifiedPass) {
		t.Fatalf("Status = %s, want %s", view.Status, workflow.StatusVerifiedPass)
	}
	documentXML := readWorkflowDocumentXML(t, view.DownloadPath)
	relsXML := readWorkflowDocxEntry(t, view.DownloadPath, "word/_rels/document.xml.rels")
	contentTypesXML := readWorkflowDocxEntry(t, view.DownloadPath, "[Content_Types].xml")
	pkg, err := ooxmlpkg.Open(view.DownloadPath)
	if err != nil {
		t.Fatalf("open output docx: %v", err)
	}
	if _, ok := pkg.Get("word/comments.xml"); ok {
		t.Fatal("output should remove comments inherited from the template")
	}
	for _, forbidden := range []string{
		"commentRange", "commentReference", "<w:ins", "<w:del", "deleted review text",
		"accepted review text", "review note", "TEMPLATE-SKELETON-MARKER", "comments",
	} {
		if strings.Contains(documentXML+relsXML+contentTypesXML, forbidden) {
			t.Fatalf("output still contains review markup %q:\n%s", forbidden, documentXML)
		}
	}
	for _, want := range []string{"Introduction", "Body from parsed source"} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("output missing %q after review finalization: %s", want, documentXML)
		}
	}
}

func TestPaperWorkflowServiceRunJobAcceptsSourceChangesAndPreservesComments(t *testing.T) {
	t.Skip("legacy skeleton transplant route removed; structure preservation is covered by the selected-template regression")
	db := openPaperWorkflowServiceTestDB(t)
	outputRoot := t.TempDir()
	inputPath := filepath.Join(t.TempDir(), "paper-source-review.docx")
	writeWorkflowDocxPackage(t, inputPath, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`<Override PartName="/word/comments.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"/>` +
			`</Types>`,
		"_rels/.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdComments" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="comments.xml"/>` +
			`</Relationships>`,
		"word/comments.xml": `<w:comments><w:comment w:id="0"><w:p><w:r><w:t>source review note</w:t></w:r></w:p></w:comment></w:comments>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:commentRangeStart w:id="0"/><w:r><w:t>1 Introduction</w:t></w:r><w:r><w:commentReference w:id="0"/></w:r><w:commentRangeEnd w:id="0"/></w:p>` +
			`<w:p><w:r><w:t>Body before </w:t></w:r>` +
			`<w:ins><w:r><w:t>accepted source text</w:t></w:r></w:ins>` +
			`<w:del><w:r><w:t>deleted source normal text</w:t></w:r><w:r><w:delText>deleted source tracked text</w:delText></w:r></w:del>` +
			`<w:moveFrom><w:r><w:t>moved away source text</w:t></w:r></w:moveFrom>` +
			`<w:r><w:t> after.</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeWorkflowTemplateDocx(t, templatePath)
	t.Setenv("CQRWST_TEMPLATE_PATH", templatePath)
	t.Setenv("CQRWST_TEMPLATE_TRANSPLANT_ENABLED", "true")
	t.Setenv("DEEPSEEK_ENABLED", "false")
	userID := uuid.New()

	svc := NewPaperWorkflowServiceWithOutputRoot(db, outputRoot)
	created, err := svc.CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:   userID,
		Title:    "paper.docx",
		FilePath: inputPath,
		FileName: "paper.docx",
		FileSize: 123,
		FileType: "docx",
	})
	if err != nil {
		t.Fatalf("CreatePaperJob() error = %v", err)
	}

	view, err := svc.RunJob(context.Background(), created.ID.String(), userID)
	if err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	if view.Status != string(workflow.StatusVerifiedPass) {
		t.Fatalf("Status = %s, want %s", view.Status, workflow.StatusVerifiedPass)
	}
	documentXML := readWorkflowDocumentXML(t, view.DownloadPath)
	text := workflowDocumentText(documentXML)
	for _, want := range []string{"1 Introduction", "Body before accepted source text after."} {
		if !strings.Contains(text, want) {
			t.Fatalf("output text missing %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"<w:ins", "<w:del", "<w:moveFrom", "deleted source", "moved away source"} {
		if strings.Contains(documentXML, forbidden) || strings.Contains(text, forbidden) {
			t.Fatalf("output still contains source review markup/text %q:\n%s", forbidden, documentXML)
		}
	}
	for _, required := range []string{"commentRangeStart", "commentRangeEnd", "commentReference"} {
		if !strings.Contains(documentXML, required) {
			t.Fatalf("output lost source comment anchor %q:\n%s", required, documentXML)
		}
	}
	if comments := readWorkflowDocxEntry(t, view.DownloadPath, "word/comments.xml"); !strings.Contains(comments, "source review note") {
		t.Fatalf("output lost source comment content: %s", comments)
	}
}

func TestPaperWorkflowServiceRunJobPersistsTemplateProfile(t *testing.T) {
	t.Skip("legacy environment-template route removed; selected format-template persistence is covered above")
	db := openPaperWorkflowServiceTestDB(t)
	outputRoot := t.TempDir()
	inputPath := filepath.Join(t.TempDir(), "paper.docx")
	writeWorkflowDocxParagraphs(t, inputPath, []string{
		"1 绪论",
		"正文内容。",
		"参考文献",
		"[1] 张三.题名[J].期刊,2024,1(1):1-2.",
	})
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeWorkflowTemplateDocxWithReferenceBreak(t, templatePath)
	if err := os.WriteFile(templatePath+".rules.json", []byte(`{"rule_pack":{"reference_standard":"GB/T 7714-2005","citation_style":"superscript_bracket"}}`), 0644); err != nil {
		t.Fatalf("write template rule sidecar: %v", err)
	}
	t.Setenv("CQRWST_TEMPLATE_PATH", templatePath)
	t.Setenv("DEEPSEEK_ENABLED", "false")
	userID := uuid.New()

	svc := NewPaperWorkflowServiceWithOutputRoot(db, outputRoot)
	created, err := svc.CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:   userID,
		Title:    "paper.docx",
		FilePath: inputPath,
		FileName: "paper.docx",
		FileSize: 123,
		FileType: "docx",
	})
	if err != nil {
		t.Fatalf("CreatePaperJob() error = %v", err)
	}

	view, err := svc.RunJob(context.Background(), created.ID.String(), userID)
	if err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}

	var compiled model.CompiledTemplate
	if err := db.First(&compiled, "id = ?", view.CompiledTemplateID).Error; err != nil {
		t.Fatalf("load compiled template: %v", err)
	}
	var profile templateprofile.Profile
	if err := json.Unmarshal([]byte(compiled.StyleProfilesJSON), &profile); err != nil {
		t.Fatalf("style_profiles_json should contain template profile JSON: %v\n%s", err, compiled.StyleProfilesJSON)
	}
	if profile.Version != templateprofile.Version {
		t.Fatalf("profile version = %s, want %s", profile.Version, templateprofile.Version)
	}
	if !profile.Sections["references_title"].PageBreakBefore {
		t.Fatalf("profile should persist references page-break rule: %#v", profile.Sections)
	}
	var rules templatecontract.RuleSet
	if err := json.Unmarshal([]byte(compiled.VerificationRulesJSON), &rules); err != nil {
		t.Fatalf("verification_rules_json should contain template rule JSON: %v\n%s", err, compiled.VerificationRulesJSON)
	}
	if rules.Version != templatecontract.Version || rules.Verification.StrictFailurePolicy != "reject_compliance_on_any_error" {
		t.Fatalf("template rule JSON not persisted correctly: %#v", rules)
	}
	if rules.PageSetup.PageWidthTwips == "" || rules.PageSetup.MarginTopTwips == "" {
		t.Fatalf("template rule JSON should persist page setup: %#v", rules.PageSetup)
	}
	if rules.RulePack.ReferenceStandard != "GB/T 7714-2005" {
		t.Fatalf("template rule JSON should persist configurable rule pack: %#v", rules.RulePack)
	}
	var contract repaircontract.Contract
	if err := json.Unmarshal([]byte(compiled.MappingContractJSON), &contract); err != nil {
		t.Fatalf("mapping_contract_json should contain repair contract JSON: %v\n%s", err, compiled.MappingContractJSON)
	}
	if contract.Version != repaircontract.Version ||
		!workflowTestHasContractStep(contract, "validate_go_template_rules") ||
		!workflowTestHasContractStep(contract, "validate_openxml_schema") ||
		!workflowTestHasContractStep(contract, "validate_content_preservation") {
		t.Fatalf("repair contract not persisted correctly: %#v", contract)
	}
	var paper model.Paper
	if err := db.First(&paper, "id = ?", view.PaperID).Error; err != nil {
		t.Fatalf("load paper: %v", err)
	}
	var ast paperast.Snapshot
	if err := json.Unmarshal([]byte(paper.ParsedInfo), &ast); err != nil {
		t.Fatalf("parsed_info should contain paper AST: %v\n%s", err, paper.ParsedInfo)
	}
	if ast.Version != paperast.Version || ast.Stats.Paragraphs == 0 {
		t.Fatalf("paper AST not persisted correctly: %#v", ast)
	}
	var verifyResult verify.Result
	if err := json.Unmarshal([]byte(loadWorkflowJob(t, db, view.ID).VerifyResultJSON), &verifyResult); err != nil {
		t.Fatalf("verify_result_json invalid: %v", err)
	}
	if verifyResult.ComplianceStatus == "" {
		t.Fatalf("verify result should include compliance status: %#v", verifyResult)
	}
	outputPath := view.DownloadPath
	if outputPath == "" {
		outputPath = filepath.Join(outputRoot, created.ID.String(), "final.docx")
	}
	documentXML := readWorkflowDocumentXML(t, outputPath)
	referenceParagraph := paragraphContainingWorkflow(documentXML, "参考文献")
	if referenceParagraph == "" || !strings.Contains(documentXML, `<w:br w:type="page"/>`) || !strings.Contains(documentXML, "[1] 张三.题名[J].期刊,2024,1(1):1-2.") {
		t.Fatalf("output should keep template reference section and insert reference payload: %s", documentXML)
	}
	if strings.Index(documentXML, "[1] 张三.题名[J].期刊,2024,1(1):1-2.") < strings.Index(documentXML, "参考文献") {
		t.Fatalf("reference payload should appear after template reference heading: %s", documentXML)
	}
}

func TestPaperWorkflowServiceRunJobFallsBackWhenTemplateHasNoContentSlot(t *testing.T) {
	t.Skip("legacy skeleton fallback route removed; only in-place selected-template formatting remains")
	db := openPaperWorkflowServiceTestDB(t)
	outputRoot := t.TempDir()
	inputPath := filepath.Join(t.TempDir(), "paper.docx")
	writeWorkflowDocxParagraphs(t, inputPath, []string{
		"1 Introduction",
		"Important source paragraph that must not disappear",
	})
	templatePath := filepath.Join(t.TempDir(), "template-without-content-slot.docx")
	writeWorkflowDocxParagraphs(t, templatePath, []string{
		"TEMPLATE-ONLY-COVER",
	})
	t.Setenv("CQRWST_TEMPLATE_PATH", templatePath)
	t.Setenv("DEEPSEEK_ENABLED", "false")
	userID := uuid.New()

	svc := NewPaperWorkflowServiceWithOutputRoot(db, outputRoot)
	created, err := svc.CreatePaperJob(context.Background(), CreatePaperJobInput{
		UserID:   userID,
		Title:    "paper.docx",
		FilePath: inputPath,
		FileName: "paper.docx",
		FileSize: 123,
		FileType: "docx",
	})
	if err != nil {
		t.Fatalf("CreatePaperJob() error = %v", err)
	}

	view, err := svc.RunJob(context.Background(), created.ID.String(), userID)
	if err != nil {
		t.Fatalf("RunJob() should safely use in-place formatting: %v", err)
	}
	documentXML := readWorkflowDocumentXML(t, view.DownloadPath)
	for _, want := range []string{"1 Introduction", "Important source paragraph that must not disappear"} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("safe fallback lost source content %q: %s", want, documentXML)
		}
	}
	if strings.Contains(documentXML, "TEMPLATE-ONLY-COVER") {
		t.Fatalf("template without a verified content slot was used as a skeleton: %s", documentXML)
	}
}

func TestNormalizeContentTextAllowsHeadingSpacingRepairs(t *testing.T) {
	source := normalizeContentText("1.2\u7814\u7a76\u76ee\u7684")
	generated := normalizeContentText("1.2 \u7814\u7a76\u76ee\u7684")
	if source != generated {
		t.Fatalf("heading spacing repair should not look like content loss: source=%q generated=%q", source, generated)
	}
	if got := normalizeContentText("190\u4f8b\u60a3\u8005"); got != "190\u4f8b\u60a3\u8005" {
		t.Fatalf("numeric-leading body text should not be rewritten as a heading: %q", got)
	}
}

func workflowTestHasContractStep(contract repaircontract.Contract, stepID string) bool {
	for _, step := range contract.Steps {
		if step.ID == stepID {
			return true
		}
	}
	return false
}

func TestPaperWorkflowServiceGetJobReturnsView(t *testing.T) {
	db := openPaperWorkflowServiceTestDB(t)
	jobID := uuid.New()
	userID := uuid.New()
	job := model.PaperWorkflowJob{
		ID:                 jobID,
		PaperID:            uuid.New(),
		UserID:             userID,
		CompiledTemplateID: uuid.New(),
		Status:             string(workflow.StatusVerifiedPass),
		Stage:              workflow.StageVerified,
		DownloadPath:       "out/final.docx",
		VerifyResultJSON:   "{}",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	view, err := NewPaperWorkflowService(db).GetJob(jobID.String())
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}

	if view.ID != jobID {
		t.Fatalf("ID = %s, want %s", view.ID, jobID)
	}
	if view.UserID != userID {
		t.Fatalf("UserID = %s, want %s", view.UserID, userID)
	}
	if view.Status != string(workflow.StatusVerifiedPass) {
		t.Fatalf("Status = %s, want %s", view.Status, workflow.StatusVerifiedPass)
	}
	if view.Stage != workflow.StageVerified {
		t.Fatalf("Stage = %s, want %s", view.Stage, workflow.StageVerified)
	}
	if view.DownloadPath != "out/final.docx" {
		t.Fatalf("DownloadPath = %s, want out/final.docx", view.DownloadPath)
	}
	if view.DownloadURL != "/api/v2/jobs/"+jobID.String()+"/download" {
		t.Fatalf("DownloadURL = %s, want v2 job download URL", view.DownloadURL)
	}
}

func TestPaperWorkflowServiceCompileTemplatePersistsRecord(t *testing.T) {
	db := openPaperWorkflowServiceTestDB(t)
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeWorkflowDocxPackage(t, templatePath, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p></w:body></w:document>`,
	})

	view, err := NewPaperWorkflowServiceWithOutputRoot(db, t.TempDir()).CompileTemplate(context.Background(), CompileTemplateInput{
		SchoolID:     "school",
		TemplateName: "template",
		Version:      "v1",
		FilePath:     templatePath,
	})
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}
	if view.ID == uuid.Nil || view.Status != "compiled" {
		t.Fatalf("compiled template view = %+v, want persisted compiled template", view)
	}
	var record model.CompiledTemplate
	if err := db.First(&record, "id = ?", view.ID).Error; err != nil {
		t.Fatalf("compiled template not persisted: %v", err)
	}
	if record.SchoolID != "school" || record.TemplateName != "template" || record.TemplateVersion != "v1" {
		t.Fatalf("compiled template record = %+v, want input metadata", record)
	}
	if _, err := os.Stat(record.SkeletonPath); err != nil {
		t.Fatalf("compiled skeleton missing: %v", err)
	}
}

func TestPaperWorkflowServiceGetJobForUserReturnsOwnerJob(t *testing.T) {
	db := openPaperWorkflowServiceTestDB(t)
	jobID := uuid.New()
	userID := uuid.New()
	job := model.PaperWorkflowJob{
		ID:                 jobID,
		PaperID:            uuid.New(),
		UserID:             userID,
		CompiledTemplateID: uuid.New(),
		Status:             string(workflow.StatusVerifiedPass),
		Stage:              workflow.StageVerified,
		DownloadPath:       "out/final.docx",
		VerifyResultJSON:   "{}",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	view, err := NewPaperWorkflowService(db).GetJobForUser(jobID.String(), userID)
	if err != nil {
		t.Fatalf("GetJobForUser() error = %v", err)
	}

	if view.ID != jobID {
		t.Fatalf("ID = %s, want %s", view.ID, jobID)
	}
	if view.UserID != userID {
		t.Fatalf("UserID = %s, want %s", view.UserID, userID)
	}
}

func TestPaperWorkflowServiceGetJobForUserReturnsNotFoundForNonOwner(t *testing.T) {
	db := openPaperWorkflowServiceTestDB(t)
	jobID := uuid.New()
	job := model.PaperWorkflowJob{
		ID:                 jobID,
		PaperID:            uuid.New(),
		UserID:             uuid.New(),
		CompiledTemplateID: uuid.New(),
		Status:             string(workflow.StatusVerifiedPass),
		Stage:              workflow.StageVerified,
		DownloadPath:       "out/final.docx",
		VerifyResultJSON:   "{}",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	_, err := NewPaperWorkflowService(db).GetJobForUser(jobID.String(), uuid.New())
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("GetJobForUser() error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestPaperWorkflowServiceGetJobRejectsInvalidUUID(t *testing.T) {
	_, err := NewPaperWorkflowService(openPaperWorkflowServiceTestDB(t)).GetJob("not-a-uuid")
	if !errors.Is(err, ErrInvalidJobID) {
		t.Fatalf("GetJob() error = %v, want ErrInvalidJobID", err)
	}
}

func TestPaperWorkflowServiceGetJobReturnsNotFound(t *testing.T) {
	_, err := NewPaperWorkflowService(openPaperWorkflowServiceTestDB(t)).GetJob(uuid.New().String())
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("GetJob() error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func openPaperWorkflowServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE papers (
			id text PRIMARY KEY,
			user_id text NOT NULL,
			title text NOT NULL,
			description text,
			file_path text NOT NULL,
			file_name text NOT NULL,
			file_size integer NOT NULL,
			file_type text NOT NULL,
			selected_template_id text,
			corrected_file_path text,
			parsed_info text,
			auto_detected_templates text,
			status text,
			deleted_at datetime,
			created_at datetime,
			updated_at datetime
		)
	`).Error; err != nil {
		t.Fatalf("create papers table: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE compiled_templates (
			id text PRIMARY KEY,
			school_id text NOT NULL,
			template_name text NOT NULL,
			template_version text NOT NULL,
			source_file_path text NOT NULL,
			skeleton_path text NOT NULL,
			manifest_json text NOT NULL,
			block_catalog_json text NOT NULL,
			style_profiles_json text NOT NULL,
			mapping_contract_json text NOT NULL,
			verification_rules_json text NOT NULL,
			patch_targets_json text NOT NULL,
			status text NOT NULL,
			created_at datetime,
			updated_at datetime
		)
	`).Error; err != nil {
		t.Fatalf("create compiled templates table: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE paper_workflow_jobs (
			id text PRIMARY KEY,
			paper_id text NOT NULL,
			user_id text NOT NULL,
			compiled_template_id text NOT NULL,
			status text NOT NULL,
			stage text NOT NULL,
			download_path text,
			verify_result_json text NOT NULL,
			created_at datetime,
			updated_at datetime
		)
	`).Error; err != nil {
		t.Fatalf("create jobs table: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE paper_workflow_issues (
			id text PRIMARY KEY,
			job_id text NOT NULL,
			kind text NOT NULL,
			severity text NOT NULL,
			block_id text,
			message text NOT NULL,
			detail_json text NOT NULL,
			created_at datetime
		)
	`).Error; err != nil {
		t.Fatalf("create issues table: %v", err)
	}

	return db
}

func workflowServiceRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}

func loadWorkflowJob(t *testing.T, db *gorm.DB, id uuid.UUID) model.PaperWorkflowJob {
	t.Helper()
	var job model.PaperWorkflowJob
	if err := db.First(&job, "id = ?", id).Error; err != nil {
		t.Fatalf("load workflow job: %v", err)
	}
	return job
}

func writeMinimalWorkflowDocx(t *testing.T, path string, body string) {
	t.Helper()
	writeWorkflowDocxParagraphs(t, path, []string{body})
}

func writeWorkflowDocxPackage(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create docx dir: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	defer file.Close()
	zipWriter := zip.NewWriter(file)
	defer zipWriter.Close()
	for name, content := range entries {
		writer, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
}

func writeWorkflowDocxParagraphs(t *testing.T, path string, paragraphs []string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	var body strings.Builder
	for _, paragraph := range paragraphs {
		body.WriteString(`<w:p><w:r><w:t>`)
		body.WriteString(paragraph)
		body.WriteString(`</w:t></w:r></w:p>`)
	}

	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"word/document.xml":   `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() + `</w:body></w:document>`,
	}
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
}

func writeWorkflowTemplateDocx(t *testing.T, path string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create template docx: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	entries := map[string]string{
		"[Content_Types].xml":          `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":                  `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/settings.xml":            `<w:settings></w:settings>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>TEMPLATE-SKELETON-MARKER</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	}
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
}

func writeWorkflowTemplateDocxWithReferenceBreak(t *testing.T, path string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create template docx: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	entries := map[string]string{
		"[Content_Types].xml":          `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/><Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/></Types>`,
		"_rels/.rels":                  `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/settings.xml":            `<w:settings></w:settings>`,
		"word/header1.xml":             `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:pBdr><w:bottom w:val="double"/></w:pBdr></w:pPr><w:r><w:t>重庆人文科技学院2026届护理学专业本科毕业论文</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml":             `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>第</w:t></w:r><w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:t>页 共</w:t></w:r><w:r><w:instrText> NUMPAGES </w:instrText></w:r><w:r><w:t>页</w:t></w:r></w:p></w:ftr>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>TEMPLATE-SKELETON-MARKER</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>{{content_blocks}}</w:t></w:r></w:p>` +
			`<w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
			`<w:p><w:pPr><w:rPr><w:rFonts w:eastAsia="宋体" w:ascii="Times New Roman"/><w:sz w:val="28"/><w:b/></w:rPr></w:pPr><w:r><w:t>参考文献</w:t></w:r></w:p>` +
			`<w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
			`<w:p><w:r><w:t>致      谢</w:t></w:r></w:p>` +
			`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1701" w:right="1417" w:bottom="1417" w:left="1701" w:header="907" w:footer="851"/></w:sectPr>` +
			`</w:body></w:document>`,
	}
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
}

func readWorkflowDocumentXML(t *testing.T, docxPath string) string {
	t.Helper()
	return readWorkflowDocxEntry(t, docxPath, "word/document.xml")
}

func readWorkflowDocxEntry(t *testing.T, docxPath string, name string) string {
	t.Helper()

	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		t.Fatalf("open docx: %v", err)
	}
	content, ok := pkg.Get(name)
	if !ok {
		t.Fatalf("%s missing", name)
	}
	return string(content)
}

func paragraphContainingWorkflow(documentXML string, text string) string {
	for _, paragraph := range regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`).FindAllString(documentXML, -1) {
		if strings.Contains(paragraph, text) {
			return paragraph
		}
	}
	return ""
}

func workflowParagraphMatchingText(documentXML string, pattern *regexp.Regexp) string {
	for _, paragraph := range regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`).FindAllString(documentXML, -1) {
		if pattern.MatchString(workflowDocumentText(paragraph)) {
			return paragraph
		}
	}
	return ""
}

func workflowParagraphsMatchingText(documentXML string, pattern *regexp.Regexp) []string {
	var matches []string
	for _, paragraph := range regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`).FindAllString(documentXML, -1) {
		if pattern.MatchString(workflowDocumentText(paragraph)) {
			matches = append(matches, paragraph)
		}
	}
	return matches
}

func workflowParagraphMatchingTextAndXML(documentXML string, pattern *regexp.Regexp, xmlFragment string) string {
	for _, paragraph := range regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`).FindAllString(documentXML, -1) {
		if strings.Contains(paragraph, xmlFragment) && pattern.MatchString(workflowDocumentText(paragraph)) {
			return paragraph
		}
	}
	return ""
}

func workflowTablesContaining(documentXML string, fragment string) []string {
	var tables []string
	for _, table := range regexp.MustCompile(`(?s)<w:tbl(?:\s[^>]*)?>.*?</w:tbl>`).FindAllString(documentXML, -1) {
		if strings.Contains(table, fragment) {
			tables = append(tables, table)
		}
	}
	return tables
}

func workflowParagraphs(documentXML string) []string {
	return regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`).FindAllString(documentXML, -1)
}

type workflowParagraphStyle struct {
	Font           string
	Size           string
	Bold           bool
	Center         bool
	Line           string
	FirstLineChars string
	After          string
}

func assertWorkflowParagraphHasStyle(t *testing.T, paragraph string, style workflowParagraphStyle) {
	t.Helper()
	if style.Font != "" && !strings.Contains(paragraph, `"`+style.Font+`"`) {
		t.Fatalf("paragraph missing font %q: %s", style.Font, paragraph)
	}
	if style.Size != "" && !strings.Contains(paragraph, `<w:sz w:val="`+style.Size+`"`) {
		t.Fatalf("paragraph missing size %s: %s", style.Size, paragraph)
	}
	if style.Bold && !strings.Contains(paragraph, `<w:b`) {
		t.Fatalf("paragraph missing bold: %s", paragraph)
	}
	if style.Center && !strings.Contains(paragraph, `<w:jc w:val="center"`) {
		t.Fatalf("paragraph missing center alignment: %s", paragraph)
	}
	if style.Line != "" && !strings.Contains(paragraph, `w:line="`+style.Line+`"`) {
		t.Fatalf("paragraph missing line spacing %s: %s", style.Line, paragraph)
	}
	if style.FirstLineChars != "" && !strings.Contains(paragraph, `w:firstLineChars="`+style.FirstLineChars+`"`) {
		t.Fatalf("paragraph missing first-line indent %s chars: %s", style.FirstLineChars, paragraph)
	}
	if style.After != "" && !strings.Contains(paragraph, `w:after="`+style.After+`"`) {
		t.Fatalf("paragraph missing after spacing %s twips: %s", style.After, paragraph)
	}
}

func workflowRenderedPageTexts(t *testing.T, result *renderverify.Result) []string {
	t.Helper()
	if result == nil || strings.TrimSpace(result.PDFPath) == "" {
		t.Fatal("render result missing PDF path")
	}
	if python := strings.TrimSpace(os.Getenv("PDF_TEXT_PYTHON")); python != "" {
		pages, err := (renderverify.PythonPDFTextExtractor{Binary: python}).ExtractPageTexts(result.PDFPath)
		if err != nil {
			t.Fatalf("extract rendered PDF text with Python: %v", err)
		}
		return pages
	}
	pages, err := (renderverify.RscPDFTextExtractor{}).ExtractPageTexts(result.PDFPath)
	if err != nil {
		t.Fatalf("extract rendered PDF text: %v", err)
	}
	return pages
}

func assertWorkflowRenderedTOCHasPageNumbers(t *testing.T, pageTexts []string) {
	t.Helper()
	for _, pageText := range pageTexts {
		if !strings.Contains(pageText, "\u76ee") || !strings.Contains(pageText, "\u5f55") {
			continue
		}
		for _, pattern := range []*regexp.Regexp{
			regexp.MustCompile(`1\s+` + "\u7eea\u8bba" + `.*\d+`),
			regexp.MustCompile(`1\.1\s+` + "\u7814\u7a76\u80cc\u666f" + `.*\d+`),
			regexp.MustCompile(`5\s+` + "\u7ed3\u8bba/\u603b\u7ed3" + `.*\d+`),
		} {
			if !pattern.MatchString(pageText) {
				t.Fatalf("rendered TOC page missing numbered entry %s:\n%s", pattern, pageText)
			}
		}
		return
	}
	t.Fatalf("rendered PDF missing TOC page: %#v", pageTexts)
}

func assertWorkflowRenderedHeadingsAreNotDoubleNumbered(t *testing.T, pageTexts []string) {
	t.Helper()
	allText := strings.Join(pageTexts, "\n")
	for _, pattern := range []*regexp.Regexp{
		regexp.MustCompile(`(^|\n)1\s+1\s+` + "\u7eea\u8bba"),
		regexp.MustCompile(`(^|\n)1\.1\s+1\.1\s+`),
		regexp.MustCompile(`(^|\n)2\.1\.1\s+2\.1\.1\s+`),
	} {
		if pattern.MatchString(allText) {
			t.Fatalf("rendered headings should not contain duplicated numbering %s:\n%s", pattern, allText)
		}
	}
}

func assertWorkflowRenderedHeadingNumbersHaveSpacing(t *testing.T, pageTexts []string) {
	t.Helper()
	allText := strings.Join(pageTexts, "\n")
	for _, pattern := range []*regexp.Regexp{
		regexp.MustCompile(`(^|\n)1\.2` + "\u7814\u7a76\u76ee\u7684"),
		regexp.MustCompile(`(^|\n)3` + "\u7814\u7a76\u7ed3\u679c"),
		regexp.MustCompile(`(^|\n)5\.1` + "\u5355\u56e0\u7d20"),
	} {
		if pattern.MatchString(allText) {
			t.Fatalf("rendered headings should have a space after numbering %s:\n%s", pattern, allText)
		}
	}
}

func assertWorkflowRenderedTextHasNoTemplateResidue(t *testing.T, pageTexts []string) {
	t.Helper()
	allText := strings.Join(pageTexts, "\n")
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile("\u6458\\s*\u8981"),
		regexp.MustCompile("\u76ee\\s*\u5f55"),
		regexp.MustCompile(`1\s+` + "\u7eea\u8bba"),
		regexp.MustCompile("\u53c2\u8003\u6587\u732e"),
	} {
		if !want.MatchString(allText) {
			t.Fatalf("rendered PDF text missing required thesis landmark %q:\n%s", want, allText)
		}
	}
	romanFooterLine := regexp.MustCompile(`(?m)^(?:I|II|III|IV|V|VI|VII|VIII|IX|X)$`)
	for index, pageText := range pageTexts {
		if got := len(romanFooterLine.FindAllString(pageText, -1)); got > 1 {
			t.Fatalf("rendered PDF page %d has duplicate Roman front-matter page numbers:\n%s", index+1, pageText)
		}
	}
	for _, forbidden := range []string{
		"202X",
		"20XX",
		"XXXX",
		"\u5c01\u9762\u683c\u5f0f\u4e0d\u8981\u8c03\u6574",
		"\u9009\u9898\u9898\u76ee\u4e00\u822c\u4e0d\u8d85\u8fc7",
		"\u6b63\u6587\u683c\u5f0f\u8303\u4f8b",
		"\u65e0\u9644\u5f55\u5185\u5bb9\u53ef\u5220\u9664",
	} {
		if strings.Contains(allText, forbidden) {
			t.Fatalf("rendered PDF text should not expose template residue %q:\n%s", forbidden, allText)
		}
	}
}

func workflowDocumentText(documentXML string) string {
	var builder strings.Builder
	for _, match := range regexp.MustCompile(`(?s)<w:t(?:\s[^>]*)?>(.*?)</w:t>`).FindAllStringSubmatch(documentXML, -1) {
		if len(match) == 2 {
			builder.WriteString(match[1])
		}
	}
	return builder.String()
}
