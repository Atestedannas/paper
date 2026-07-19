package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/renderverify"
	"github.com/paper-format-checker/backend/internal/core/repaircontract"
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
	if profile.Styles["heading_1"].FontEastAsia != "AdminFont" || profile.Styles["heading_1"].FontSizeHalfPt != "36" {
		t.Fatalf("administrator override missing from profile: %#v", profile.Styles["heading_1"])
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
	if err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}

	if view.Status != string(workflow.StatusManualReview) {
		t.Fatalf("Status = %s, want %s", view.Status, workflow.StatusManualReview)
	}
	if view.Stage != workflow.StageManualReview {
		t.Fatalf("Stage = %s, want %s", view.Stage, workflow.StageManualReview)
	}
	outputPath := filepath.Join(outputRoot, created.ID.String(), "final.docx")
	if view.DownloadPath != outputPath {
		t.Fatalf("DownloadPath = %s, want manual-review draft path %s", view.DownloadPath, outputPath)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected copied workflow output for manual review: %v", err)
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

func TestPaperWorkflowServiceRunJobUsesDefaultTemplateForRealFixture(t *testing.T) {
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
		if hasWorkflowIssue(verifyResult.Warnings, kind) {
			t.Fatalf("VerifyResultJSON warnings = %#v, did not want %s", verifyResult.Warnings, kind)
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
	if !strings.Contains(documentXML, "SEQ 表") {
		t.Fatalf("generated output should convert manual table captions to SEQ fields: %s", documentXML)
	}
	if !strings.Contains(documentXML, "REF _CQRWST_Tbl") {
		t.Fatalf("generated output should convert manual table references to REF fields: %s", documentXML)
	}
	for _, want := range []string{"<w:pict", "<w:tblpPr", "护理学院", "护理学", "2022级护理学5班", "20220152192", "冉怡琴", "杨严政", "认知现状及影响因素分析"} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("generated output should preserve template cover and field %q: %s", want, documentXML)
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
	if !regexp.MustCompile(`20\d{2}年\d{1,2}月`).MatchString(documentXML) {
		t.Fatalf("generated output should include cover date year/month: %s", documentXML)
	}
	documentText := workflowDocumentText(documentXML)
	coverDate := regexp.MustCompile(`20\d{2}年\d{1,2}月`).FindString(documentText)
	if coverDate == "" {
		t.Fatalf("generated output should include a visible cover date in document text: %s", documentText)
	}
	dateIndex := strings.Index(documentText, coverDate)
	abstractIndex := strings.Index(documentText, "摘要：目的")
	frontTitleIndex := -1
	if dateIndex >= 0 {
		relativeIndex := strings.Index(documentText[dateIndex+len(coverDate):], "社区2型糖尿病患者疾病知识")
		if relativeIndex >= 0 {
			frontTitleIndex = dateIndex + len(coverDate) + relativeIndex
		}
	}
	if dateIndex < 0 || frontTitleIndex < 0 || abstractIndex < 0 || frontTitleIndex > abstractIndex {
		t.Fatalf("generated output should keep front-matter title between cover date and abstract: %s", documentXML)
	}
	paragraphs := workflowParagraphs(documentXML)
	if len(paragraphs) < 25 {
		t.Fatalf("generated output should contain cover and front matter paragraphs, got %d", len(paragraphs))
	}
	assertWorkflowParagraphHasStyle(t, paragraphs[3], workflowParagraphStyle{Font: "\u5b8b\u4f53", Size: "44", Bold: true, Center: true})
	assertWorkflowParagraphHasStyle(t, paragraphs[5], workflowParagraphStyle{Font: "\u5b8b\u4f53", Size: "44", Bold: true, Center: true})
	assertWorkflowParagraphHasStyle(t, paragraphs[21], workflowParagraphStyle{Font: "\u9ed1\u4f53", Size: "30", Bold: true, Line: "360", FirstLineChars: "200", AfterLines: "200"})
	assertWorkflowParagraphHasStyle(t, paragraphs[21], workflowParagraphStyle{Font: "\u5b8b\u4f53", Size: "24", Line: "360", FirstLineChars: "200", AfterLines: "200"})
	assertWorkflowParagraphHasStyle(t, paragraphs[22], workflowParagraphStyle{Font: "\u9ed1\u4f53", Size: "30", Bold: true, Line: "360", FirstLineChars: "200", AfterLines: "200"})
	assertWorkflowParagraphHasStyle(t, paragraphs[22], workflowParagraphStyle{Font: "\u5b8b\u4f53", Size: "24", Line: "360", FirstLineChars: "200", AfterLines: "200"})
	assertWorkflowParagraphHasStyle(t, paragraphs[23], workflowParagraphStyle{Font: "Times New Roman", Size: "30", Bold: true, Line: "360", FirstLineChars: "200", AfterLines: "200"})
	assertWorkflowParagraphHasStyle(t, paragraphs[23], workflowParagraphStyle{Font: "Times New Roman", Size: "24", Line: "360", FirstLineChars: "200", AfterLines: "200"})
	cnKeywordsText := workflowDocumentText(paragraphs[22])
	cnKeywordsPrefix := "\u5173\u952e\u8bcd\uff1a"
	if !strings.HasPrefix(cnKeywordsText, cnKeywordsPrefix) {
		t.Fatalf("generated output should keep Chinese keyword label: %s", cnKeywordsText)
	}
	cnKeywordBody := strings.TrimSpace(strings.TrimPrefix(cnKeywordsText, cnKeywordsPrefix))
	if strings.Contains(cnKeywordBody, ";") {
		t.Fatalf("Chinese keywords should use full-width semicolon separators: %s", cnKeywordsText)
	}
	cnKeywords := strings.Split(cnKeywordBody, "\uff1b")
	if len(cnKeywords) < 3 || len(cnKeywords) > 5 {
		t.Fatalf("Chinese keywords should contain 3-5 entries, got %d: %s", len(cnKeywords), cnKeywordsText)
	}
	for _, keyword := range cnKeywords {
		if strings.TrimSpace(keyword) == "" {
			t.Fatalf("Chinese keywords should not contain empty entries: %s", cnKeywordsText)
		}
	}
	enKeywords := paragraphContainingWorkflow(documentXML, "Key words:")
	if enKeywords == "" {
		t.Fatalf("generated output missing English keywords paragraph: %s", documentXML)
	}
	assertWorkflowParagraphHasStyle(t, enKeywords, workflowParagraphStyle{Font: "Times New Roman", Size: "30", Bold: true, Line: "360", FirstLineChars: "200", AfterLines: "200"})
	assertWorkflowParagraphHasStyle(t, enKeywords, workflowParagraphStyle{Font: "Times New Roman", Size: "24", Line: "360", FirstLineChars: "200", AfterLines: "200"})
	enKeywordsText := workflowDocumentText(enKeywords)
	enKeywordBody := strings.TrimSpace(strings.TrimPrefix(enKeywordsText, "Key words:"))
	if strings.Contains(enKeywordBody, ";") || strings.Contains(enKeywordBody, ", ") && !strings.Contains(enKeywordBody, ",  ") {
		t.Fatalf("English keywords should use an English comma followed by two spaces: %s", enKeywordsText)
	}
	enKeywordParts := strings.Split(enKeywordBody, ",  ")
	if len(enKeywordParts) < 3 || len(enKeywordParts) > 5 {
		t.Fatalf("English keywords should contain 3-5 entries, got %d: %s", len(enKeywordParts), enKeywordsText)
	}
	for _, keyword := range enKeywordParts {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			t.Fatalf("English keywords should not contain empty entries: %s", enKeywordsText)
		}
		if keyword[:1] != strings.ToUpper(keyword[:1]) {
			t.Fatalf("English keyword should start with a capital letter: %s", enKeywordsText)
		}
	}
	tocTitle := paragraphContainingWorkflow(documentXML, "\u76ee      \u5f55")
	if tocTitle == "" {
		t.Fatalf("generated output missing table-of-contents title: %s", documentXML)
	}
	for _, want := range []string{`<w:jc w:val="center"/>`, `w:eastAsia="` + "\u9ed1\u4f53" + `"`, `<w:sz w:val="32"/>`, `w:line="360"`, `w:afterLines="200"`} {
		if !strings.Contains(tocTitle, want) {
			t.Fatalf("table-of-contents title missing %s: %s", want, tocTitle)
		}
	}
	if got := strings.Count(documentXML, `TOC \o "1-3" \h \z \u`); got != 1 {
		t.Fatalf("generated output should contain exactly one dynamic TOC field, got %d: %s", got, documentXML)
	}
	assertWorkflowTOCCacheHasRealPageNumbers(t, documentXML)
	settingsXML := readWorkflowDocxEntry(t, outputPath, "word/settings.xml")
	if !strings.Contains(settingsXML, `<w:updateFields w:val="true"/>`) {
		t.Fatalf("generated output should ask Word/LibreOffice to refresh fields on open: %s", settingsXML)
	}
	contentTypesXML := readWorkflowDocxEntry(t, outputPath, "[Content_Types].xml")
	relsXML := readWorkflowDocxEntry(t, outputPath, "word/_rels/document.xml.rels")
	for _, want := range []string{
		`PartName="/word/footnotes.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml"`,
		`PartName="/word/endnotes.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.endnotes+xml"`,
		`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes" Target="footnotes.xml"`,
		`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/endnotes" Target="endnotes.xml"`,
	} {
		if !strings.Contains(contentTypesXML+relsXML, want) {
			t.Fatalf("generated output missing note package plumbing %q:\ncontent-types=%s\nrels=%s", want, contentTypesXML, relsXML)
		}
	}
	for name, root := range map[string]string{"word/footnotes.xml": "footnote", "word/endnotes.xml": "endnote"} {
		noteXML := readWorkflowDocxEntry(t, outputPath, name)
		for _, want := range []string{`<w:` + root + ` w:type="separator"`, `<w:` + root + ` w:type="continuationSeparator"`} {
			if !strings.Contains(noteXML, want) {
				t.Fatalf("%s missing %s: %s", name, want, noteXML)
			}
		}
	}
	for _, want := range []string{
		`<Default Extension="png" ContentType="image/png"/>`,
		`<Default Extension="wmf" ContentType="image/x-wmf"/>`,
		`<Default Extension="bin" ContentType="application/vnd.openxmlformats-officedocument.oleObject"/>`,
	} {
		if !strings.Contains(contentTypesXML, want) {
			t.Fatalf("generated output missing media content type %s: %s", want, contentTypesXML)
		}
	}
	mediaTargets := regexp.MustCompile(`Type="http://schemas\.openxmlformats\.org/officeDocument/2006/relationships/(?:image|oleObject)" Target="([^"]+)"`).FindAllStringSubmatch(relsXML, -1)
	if len(mediaTargets) < 11 {
		t.Fatalf("generated output should preserve image and embedded object relationships, got %d: %s", len(mediaTargets), relsXML)
	}
	outputPkg, err := ooxmlpkg.Open(outputPath)
	if err != nil {
		t.Fatalf("open generated output package: %v", err)
	}
	for _, match := range mediaTargets {
		if _, ok := outputPkg.Get("word/" + match[1]); !ok {
			t.Fatalf("generated output relationship target missing: %s", match[1])
		}
	}
	tocEntry := workflowParagraphMatchingText(documentXML, regexp.MustCompile(`^\s*1\s+\S+`))
	if tocEntry == "" || strings.Contains(tocEntry, `<w:pStyle w:val="Heading1"/>`) {
		t.Fatalf("generated output missing compact generated TOC entry before body heading: %s", tocEntry)
	}
	for _, want := range []string{`w:eastAsia="` + "\u5b8b\u4f53" + `"`, `<w:sz w:val="20"/>`, `w:line="240"`} {
		if !strings.Contains(tocEntry, want) {
			t.Fatalf("table-of-contents entry missing %s: %s", want, tocEntry)
		}
	}
	chapterHeading := workflowParagraphMatchingTextAndXML(documentXML, regexp.MustCompile(`^\s*1\s+\S+`), `<w:pStyle w:val="Heading1"/>`)
	if chapterHeading == "" {
		t.Fatalf("generated output missing first-level numbered body heading: %s", documentXML)
	}
	for _, want := range []string{`<w:pStyle w:val="Heading1"/>`, `<w:outlineLvl w:val="0"/>`, `<w:sz w:val="32"/>`, `<w:b/><w:bCs/>`, `<w:jc w:val="left"/>`, `w:beforeLines="100"`, `w:afterLines="100"`} {
		if !strings.Contains(chapterHeading, want) {
			t.Fatalf("first-level numbered body heading missing %q: %s", want, chapterHeading)
		}
	}
	secondLevelHeading := workflowParagraphMatchingTextAndXML(documentXML, regexp.MustCompile(`^\s*1\.1\s*\S+`), `<w:pStyle w:val="Heading2"/>`)
	if secondLevelHeading == "" {
		t.Fatalf("generated output missing second-level numbered body heading: %s", documentXML)
	}
	for _, want := range []string{`<w:pStyle w:val="Heading2"/>`, `<w:outlineLvl w:val="1"/>`, `<w:sz w:val="30"/>`, `<w:b/><w:bCs/>`, `w:line="360"`} {
		if !strings.Contains(secondLevelHeading, want) {
			t.Fatalf("second-level numbered body heading missing %q: %s", want, secondLevelHeading)
		}
	}
	thirdLevelHeading := workflowParagraphMatchingTextAndXML(documentXML, regexp.MustCompile(`^\s*2\.1\.1\s*\S+`), `<w:pStyle w:val="Heading3"/>`)
	if thirdLevelHeading == "" {
		t.Fatalf("generated output missing third-level numbered body heading: %s", documentXML)
	}
	for _, want := range []string{`<w:pStyle w:val="Heading3"/>`, `<w:outlineLvl w:val="2"/>`, `<w:sz w:val="28"/>`, `<w:b/><w:bCs/>`, `w:line="360"`} {
		if !strings.Contains(thirdLevelHeading, want) {
			t.Fatalf("third-level numbered body heading missing %q: %s", want, thirdLevelHeading)
		}
	}
	bodyParagraph := paragraphContainingWorkflow(documentXML, "\u7cd6\u5c3f\u75c5\u662f\u4e00\u7ec4")
	if bodyParagraph == "" {
		t.Fatalf("generated output missing representative body paragraph: %s", documentXML)
	}
	assertWorkflowParagraphHasStyle(t, bodyParagraph, workflowParagraphStyle{Font: "\u5b8b\u4f53", Size: "24", Line: "360", FirstLineChars: "200"})
	if strings.Contains(bodyParagraph, `<w:pStyle w:val="Heading`) || strings.Contains(bodyParagraph, `<w:b`) {
		t.Fatalf("body paragraph should remain normal non-bold text, got: %s", bodyParagraph)
	}
	if got := regexp.MustCompile(`(?s)<w:r\b[^>]*>.*?<w:vertAlign w:val="superscript"/>.*?<w:t>\[\d+(?:-\d+)?\]</w:t>.*?</w:r>`).FindAllString(documentXML, -1); len(got) < 5 {
		t.Fatalf("generated output should keep body citations as superscript bracket references, got %d", len(got))
	}
	contentTables := workflowTablesContaining(documentXML, `<w:tblHeader/>`)
	if len(contentTables) == 0 {
		t.Fatalf("generated output should contain normalized body tables: %s", documentXML)
	}
	for _, table := range contentTables {
		for _, want := range []string{
			`<w:jc w:val="center"/>`,
			`<w:tblLayout w:type="fixed"/>`,
			`<w:top w:val="single" w:sz="12" w:space="0" w:color="000000"/>`,
			`<w:bottom w:val="single" w:sz="12" w:space="0" w:color="000000"/>`,
			`<w:insideH w:val="single" w:sz="4" w:space="0" w:color="000000"/>`,
			`<w:left w:val="nil"/>`,
			`<w:right w:val="nil"/>`,
			`<w:insideV w:val="nil"/>`,
			`<w:tblGrid>`,
		} {
			if !strings.Contains(table, want) {
				t.Fatalf("normalized body table missing %q: %s", want, table)
			}
		}
		if strings.Contains(table, "cantSplit") {
			t.Fatalf("normalized body table should allow row splitting across pages: %s", table)
		}
	}
	continuedTableCaptions := workflowParagraphsMatchingText(documentXML, regexp.MustCompile(`^\s*`+"\u7eed\u8868"+`\d+[-.．]\d+\s+`))
	if len(continuedTableCaptions) < 3 {
		t.Fatalf("generated output should preserve continued-table captions, got %d", len(continuedTableCaptions))
	}
	for _, caption := range continuedTableCaptions {
		assertWorkflowParagraphHasStyle(t, caption, workflowParagraphStyle{Font: "\u5b8b\u4f53", Size: "21", Center: true, Line: "300"})
		if !strings.Contains(caption, "<w:keepNext/>") || strings.Contains(caption, "firstLine") {
			t.Fatalf("continued-table caption should stay centered with following table and no body indent: %s", caption)
		}
	}
	if !strings.Contains(documentXML, `<w:footerReference w:type="default" r:id="rId11"`) || !strings.Contains(documentXML, `<w:pgNumType w:start="1"`) {
		t.Fatalf("generated output should preserve main-body footer starting at page 1: %s", documentXML)
	}
	if !strings.Contains(documentXML, `<w:headerReference w:type="default" r:id="rId8"`) {
		t.Fatalf("generated output should preserve the template running header: %s", documentXML)
	}
	sectPrs := regexp.MustCompile(`(?s)<w:sectPr\b[^>]*/>|<w:sectPr\b[^>]*>.*?</w:sectPr>`).FindAllString(documentXML, -1)
	if len(sectPrs) < 3 {
		t.Fatalf("generated output should keep cover/front/body sections: %s", documentXML)
	}
	for _, sectPr := range sectPrs {
		if !strings.Contains(sectPr, `<w:pgSz w:w="11906" w:h="16838"/>`) {
			t.Fatalf("generated output section should use A4 page size: %s", sectPr)
		}
	}
	if !strings.Contains(sectPrs[0], `<w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134"`) {
		t.Fatalf("generated output should preserve cover page margins: %s", sectPrs[0])
	}
	if strings.Contains(sectPrs[0], "headerReference") || strings.Contains(sectPrs[0], "footerReference") {
		t.Fatalf("generated output cover section should not inherit running header/footer: %s", sectPrs[0])
	}
	if !strings.Contains(sectPrs[1], `<w:pgNumType w:fmt="upperRoman" w:start="1"/>`) {
		t.Fatalf("generated output should preserve front-matter Roman page numbering: %s", sectPrs[1])
	}
	if !strings.Contains(sectPrs[1], `<w:footerReference w:type="default" r:id="rId9"`) || !regexp.MustCompile(`Id="rId9"[^>]+/footer"[^>]+Target="footer1\.xml"`).MatchString(relsXML) {
		t.Fatalf("generated output should route front-matter pages to footer1: sect=%s rels=%s", sectPrs[1], relsXML)
	}
	if !strings.Contains(sectPrs[2], `<w:pgMar w:top="1418" w:right="1134" w:bottom="1134" w:left="1418"`) || !strings.Contains(sectPrs[2], `<w:pgNumType w:start="1"/>`) {
		t.Fatalf("generated output should preserve body margins and restart page numbering: %s", sectPrs[2])
	}
	if !strings.Contains(sectPrs[2], `<w:footerReference w:type="default" r:id="rId11"`) || !regexp.MustCompile(`Id="rId11"[^>]+/footer"[^>]+Target="footer3\.xml"`).MatchString(relsXML) {
		t.Fatalf("generated output should route body pages to footer3: sect=%s rels=%s", sectPrs[2], relsXML)
	}
	headerXML := readWorkflowDocxEntry(t, outputPath, "word/header1.xml")
	for _, want := range []string{"重庆人文科技学院2026届护理学专业本科毕业论文", `<w:jc w:val="center"/>`, `<w:bottom w:val="double"`} {
		if !strings.Contains(headerXML, want) {
			t.Fatalf("generated output header missing %s: %s", want, headerXML)
		}
	}
	frontFooterXML := readWorkflowDocxEntry(t, outputPath, "word/footer1.xml")
	for _, want := range []string{`<w:jc w:val="center"/>`, ` PAGE `} {
		if !strings.Contains(frontFooterXML, want) {
			t.Fatalf("generated output front-matter footer missing %s: %s", want, frontFooterXML)
		}
	}
	for _, forbidden := range []string{"NUMPAGES", "SECTIONPAGES", "第", "共"} {
		if strings.Contains(frontFooterXML, forbidden) {
			t.Fatalf("front-matter footer should be a standalone Roman PAGE footer, found %q: %s", forbidden, frontFooterXML)
		}
	}
	if strings.Contains(frontFooterXML, "<w:pict") {
		t.Fatalf("front-matter footer should not use VML text boxes because they drift in LibreOffice rendering: %s", frontFooterXML)
	}
	footerXML := readWorkflowDocxEntry(t, outputPath, "word/footer3.xml")
	hasDynamicTotal := strings.Contains(footerXML, "SECTIONPAGES") && !strings.Contains(footerXML, "NUMPAGES") && !strings.Contains(footerXML, "12页")
	hasMaterializedTotal := !strings.Contains(footerXML, "SECTIONPAGES") && !strings.Contains(footerXML, "NUMPAGES") && regexp.MustCompile(`>\d+<`).MatchString(footerXML) && !strings.Contains(footerXML, ">12<")
	if !hasDynamicTotal && !hasMaterializedTotal {
		t.Fatalf("generated output should use a dynamic or render-corrected total page count in the main footer: %s", footerXML)
	}
	referencesTitle := paragraphContainingWorkflow(documentXML, "参考文献")
	if referencesTitle == "" {
		t.Fatal("generated output missing references title")
	}
	for _, want := range []string{`<w:pStyle w:val="Heading1"/>`, `<w:outlineLvl w:val="0"/>`, `<w:jc w:val="center"/>`, `w:eastAsia="黑体"`, `<w:b/>`, `<w:sz w:val="30"/>`} {
		if !strings.Contains(referencesTitle, want) {
			t.Fatalf("references title missing %s: %s", want, referencesTitle)
		}
	}
	firstReference := workflowParagraphMatchingText(documentXML, regexp.MustCompile(`^\[1\]`))
	if firstReference == "" {
		t.Fatal("generated output missing first reference entry")
	}
	if strings.Contains(firstReference, `w:vertAlign w:val="superscript"`) {
		t.Fatalf("reference list marker should not be superscripted: %s", firstReference)
	}
	referenceParagraphs := workflowParagraphsMatchingText(documentXML, regexp.MustCompile(`^\[\d+\]`))
	if len(referenceParagraphs) < 10 {
		t.Fatalf("generated output should preserve at least 10 reference entries, got %d", len(referenceParagraphs))
	}
	for index, paragraph := range referenceParagraphs {
		want := "[" + strconv.Itoa(index+1) + "]"
		if !strings.HasPrefix(workflowDocumentText(paragraph), want) {
			t.Fatalf("reference entries should be continuously numbered, want %s at index %d: %s", want, index, paragraph)
		}
	}
	for _, want := range []string{`w:eastAsia="宋体"`, `w:ascii="Times New Roman"`, `<w:sz w:val="21"/>`, `w:line="288"`} {
		if !strings.Contains(firstReference, want) {
			t.Fatalf("first reference entry missing %s: %s", want, firstReference)
		}
	}
	thanksTitle := paragraphContainingWorkflow(documentXML, "致      谢")
	if thanksTitle == "" {
		t.Fatal("generated output missing acknowledgement title")
	}
	for _, want := range []string{`<w:jc w:val="center"/>`, `w:eastAsia="黑体"`, `<w:b/>`, `<w:sz w:val="30"/>`} {
		if !strings.Contains(thanksTitle, want) {
			t.Fatalf("acknowledgement title missing %s: %s", want, thanksTitle)
		}
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
		`w:name="_CQRWST_Tbl_4_2"`,
		`REF _CQRWST_Tbl_4_2`,
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

func TestPaperWorkflowServiceRunJobFinalizesTemplateReviewMarkup(t *testing.T) {
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
		t.Fatal("output should remove comments.xml")
	}
	for _, forbidden := range []string{"commentRange", "commentReference", "<w:ins", "<w:del", "deleted review text", "comments"} {
		if strings.Contains(documentXML+relsXML+contentTypesXML, forbidden) {
			t.Fatalf("output still contains review markup %q:\n%s", forbidden, documentXML)
		}
	}
	for _, want := range []string{"accepted review text", "Introduction", "Body from parsed source"} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("output missing %q after review finalization: %s", want, documentXML)
		}
	}
}

func TestPaperWorkflowServiceRunJobFinalizesSourceReviewMarkup(t *testing.T) {
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
	for _, forbidden := range []string{"commentRange", "commentReference", "<w:ins", "<w:del", "<w:moveFrom", "deleted source", "moved away source", "source review note"} {
		if strings.Contains(documentXML, forbidden) || strings.Contains(text, forbidden) {
			t.Fatalf("output still contains source review markup/text %q:\n%s", forbidden, documentXML)
		}
	}
}

func TestPaperWorkflowServiceRunJobPersistsTemplateProfile(t *testing.T) {
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
	if contract.Version != repaircontract.Version || !workflowTestHasContractStep(contract, "verify_before_download") || !workflowTestHasContractStep(contract, "render_and_regression_gate") {
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

func TestPaperWorkflowServiceRunJobFallsBackWhenTemplateDropsSourceContent(t *testing.T) {
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
		t.Fatalf("RunJob() error = %v", err)
	}
	if view.Status != string(workflow.StatusVerifiedPass) {
		t.Fatalf("Status = %s, want %s", view.Status, workflow.StatusVerifiedPass)
	}

	documentXML := readWorkflowDocumentXML(t, view.DownloadPath)
	if !strings.Contains(documentXML, "Important source paragraph that must not disappear") {
		t.Fatalf("fallback output lost source content: %s", documentXML)
	}
	if strings.Contains(documentXML, "TEMPLATE-ONLY-COVER") {
		t.Fatalf("template-only output should have been rejected to prevent data loss: %s", documentXML)
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
	AfterLines     string
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
	if style.AfterLines != "" && !strings.Contains(paragraph, `w:afterLines="`+style.AfterLines+`"`) {
		t.Fatalf("paragraph missing after spacing %s lines: %s", style.AfterLines, paragraph)
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

func assertWorkflowTOCCacheHasRealPageNumbers(t *testing.T, documentXML string) {
	start := strings.Index(documentXML, `TOC \o "1-3" \h \z \u`)
	if start < 0 {
		t.Fatal("document XML missing TOC field")
	}
	endOffset := strings.Index(documentXML[start:], `w:fldCharType="end"`)
	if endOffset < 0 {
		t.Fatal("document XML missing TOC field end")
	}
	cache := documentXML[start : start+endOffset]
	pageRuns := regexp.MustCompile(`(?s)<w:tab/></w:r>\s*<w:r>.*?<w:t>([1-9]\d*)</w:t>`).FindAllStringSubmatch(cache, -1)
	if len(pageRuns) == 0 || strings.Contains(cache, `<w:t>0</w:t>`) {
		t.Fatalf("TOC cache should contain materialized non-zero page numbers: %s", cache)
	}
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
