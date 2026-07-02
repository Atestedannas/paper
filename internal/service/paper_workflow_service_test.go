package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
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
	if _, err := svc.RunJob(context.Background(), created.ID.String(), userID); err != nil {
		t.Fatalf("RunJob() error = %v", err)
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
	for _, want := range []string{"<w:pict", "<w:tblpPr", "护理学院", "护理学", "2022级护理学5班", "20220152192", "冉怡琴", "杨严政", "认知现状及影响因素分析"} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("generated output should preserve template cover and field %q: %s", want, documentXML)
		}
	}
	for _, forbidden := range []string{"202X", "20XX", "XXXX", "封面格式不要调整", "选题题目一般不超过"} {
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
	if !strings.Contains(documentXML, `<w:footerReference w:type="default" r:id="rId11"`) || !strings.Contains(documentXML, `<w:pgNumType w:start="1"`) {
		t.Fatalf("generated output should preserve main-body footer starting at page 1: %s", documentXML)
	}
	if !strings.Contains(documentXML, `<w:headerReference w:type="default" r:id="rId8"`) {
		t.Fatalf("generated output should preserve the template running header: %s", documentXML)
	}
	headerXML := readWorkflowDocxEntry(t, outputPath, "word/header1.xml")
	for _, want := range []string{"重庆人文科技学院2026届护理学专业本科毕业论文", `<w:jc w:val="center"/>`, `<w:bottom w:val="double"`} {
		if !strings.Contains(headerXML, want) {
			t.Fatalf("generated output header missing %s: %s", want, headerXML)
		}
	}
	footerXML := readWorkflowDocxEntry(t, outputPath, "word/footer3.xml")
	hasDynamicTotal := strings.Contains(footerXML, "NUMPAGES") && !strings.Contains(footerXML, "12页")
	hasMaterializedTotal := !strings.Contains(footerXML, "NUMPAGES") && strings.Contains(footerXML, ">18<")
	if !hasDynamicTotal && !hasMaterializedTotal {
		t.Fatalf("generated output should use a dynamic or render-corrected total page count in the main footer: %s", footerXML)
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

func workflowDocumentText(documentXML string) string {
	var builder strings.Builder
	for _, match := range regexp.MustCompile(`(?s)<w:t(?:\s[^>]*)?>(.*?)</w:t>`).FindAllStringSubmatch(documentXML, -1) {
		if len(match) == 2 {
			builder.WriteString(match[1])
		}
	}
	return builder.String()
}
