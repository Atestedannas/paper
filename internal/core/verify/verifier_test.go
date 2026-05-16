package verify

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/cqrwst"
	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/repaircontract"
	"github.com/paper-format-checker/backend/internal/core/templatecontract"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

func TestVerifierReturnsFatalIssueWhenDocxCannotOpen(t *testing.T) {
	result, err := NewVerifier().Verify(context.Background(), filepath.Join(t.TempDir(), "missing.docx"))
	if err != nil {
		t.Fatalf("Verify() error = %v, want result with fatal issue", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false")
	}
	if len(result.FatalIssues) != 1 {
		t.Fatalf("FatalIssues len = %d, want 1", len(result.FatalIssues))
	}
	if result.FatalIssues[0].Kind != "docx_open" {
		t.Fatalf("fatal kind = %q, want docx_open", result.FatalIssues[0].Kind)
	}
	if result.ComplianceStatus != "rejected" {
		t.Fatalf("ComplianceStatus = %s, want rejected", result.ComplianceStatus)
	}
}

func TestVerifierReturnsFatalIssueWhenDocumentXMLMissing(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{"word/header1.xml": `<w:hdr/>`})

	result, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false")
	}
	if len(result.FatalIssues) != 1 {
		t.Fatalf("FatalIssues len = %d, want 1", len(result.FatalIssues))
	}
	if result.FatalIssues[0].Target != "word/document.xml" {
		t.Fatalf("fatal target = %q, want word/document.xml", result.FatalIssues[0].Target)
	}
}

func TestVerifierReturnsRepairableIssueForPlaceholders(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>{{title}}</w:body></w:document>`,
	})

	result, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false")
	}
	if !hasVerifyIssueKind(result.RepairableIssues, "placeholder") {
		t.Fatalf("RepairableIssues = %#v, want placeholder", result.RepairableIssues)
	}
	if result.ComplianceStatus != "review_required" {
		t.Fatalf("ComplianceStatus = %s, want review_required", result.ComplianceStatus)
	}
}

func TestVerifierRejectsRendererIncompatibleStartAlignment(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:pPr><w:jc w:val="start"/></w:pPr><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p></w:body></w:document>`,
	})

	result, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false")
	}
	if !hasVerifyIssueKind(result.FatalIssues, "renderer_incompatible_ooxml") {
		t.Fatalf("FatalIssues = %#v, want renderer_incompatible_ooxml", result.FatalIssues)
	}
}

func TestVerifierRequiresFinalDeliveryWithoutComments(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p></w:body></w:document>`,
		"word/comments.xml": `<w:comments><w:comment w:id="0"><w:p><w:r><w:t>review note</w:t></w:r></w:p></w:comment></w:comments>`,
	})

	result, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false")
	}
	if !hasVerifyIssueKind(result.RepairableIssues, "comments_not_finalized") {
		t.Fatalf("RepairableIssues = %#v, want comments_not_finalized", result.RepairableIssues)
	}
}

func TestVerifierReportsCQRWSTRepairableIssues(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>1.1研究背景</w:t></w:r></w:p></w:body></w:document>`,
	})

	result, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false")
	}
	if !hasVerifyIssueKind(result.RepairableIssues, "cqrwst_rule") {
		t.Fatalf("RepairableIssues = %#v, want cqrwst_rule", result.RepairableIssues)
	}
}

func TestVerifierDoesNotPassShortDocumentWithMissingCQRWSTStructure(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{"word/document.xml": `  <w/>  `})

	result, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false when CQRWST structure is missing")
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings len = %d, want 1", len(result.Warnings))
	}
	if !hasVerifyIssueKind(result.RepairableIssues, "cqrwst_rule") {
		t.Fatalf("RepairableIssues = %#v, want cqrwst_rule", result.RepairableIssues)
	}
}

func TestVerifierPassesCleanDocument(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p></w:body></w:document>`,
	})
	if _, err := cqrwst.FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	result, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !result.Passed {
		t.Fatalf("Verify() Passed = false, result = %#v", result)
	}
	if result.ComplianceStatus != "format_compliant" {
		t.Fatalf("ComplianceStatus = %s, want format_compliant", result.ComplianceStatus)
	}
	if len(result.FatalIssues) != 0 || len(result.RepairableIssues) != 0 || len(result.Warnings) != 0 {
		t.Fatalf("Verify() result has unexpected issues: %#v", result)
	}
}

func TestVerifierWithTemplateProfileUsesProfileStyles(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>1 Introduction</w:t></w:r></w:p><w:p><w:r><w:t>Body paragraph content long enough.</w:t></w:r></w:p></w:body></w:document>`,
	})
	profile := &templateprofile.Profile{
		Version: templateprofile.Version,
		Styles: map[string]templateprofile.StyleRule{
			"body": {
				FontEastAsia:   "Courier New",
				FontASCII:      "Times New Roman",
				FontSizeHalfPt: "26",
				Alignment:      "both",
				Line:           "420",
				FirstLineChars: "200",
			},
		},
	}
	if _, err := cqrwst.FixDOCXWithTemplateProfile(context.Background(), docxPath, profile); err != nil {
		t.Fatalf("FixDOCXWithTemplateProfile() error = %v", err)
	}

	hardcodedResult, err := NewVerifier().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if hardcodedResult.Passed {
		t.Fatal("NewVerifier() Passed = true, want false because hardcoded style differs from profile")
	}

	profileResult, err := NewVerifierWithTemplateProfile(profile).Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() with profile error = %v", err)
	}
	if !profileResult.Passed {
		t.Fatalf("NewVerifierWithTemplateProfile() Passed = false, result = %#v", profileResult)
	}
}

func TestVerifierRejectsComplianceWhenClosureArtifactsAreInvalid(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p></w:body></w:document>`,
	})
	if _, err := cqrwst.FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	result, err := NewVerifierWithTemplateProfileAndClosure(nil, templatecontract.RuleSet{Version: templatecontract.Version}, paperast.Snapshot{Version: paperast.Version}, repaircontract.Contract{Version: repaircontract.Version}).Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false when closure artifacts are incomplete")
	}
	if result.ComplianceStatus != "rejected" {
		t.Fatalf("ComplianceStatus = %s, want rejected", result.ComplianceStatus)
	}
	if !hasVerifyIssueKind(result.FatalIssues, "closure_paper_ast") || !hasVerifyIssueKind(result.FatalIssues, "closure_repair_contract") {
		t.Fatalf("FatalIssues = %#v, want closure artifact issues", result.FatalIssues)
	}
}

func TestVerifierPassesWhenClosureArtifactsAreValid(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p></w:body></w:document>`,
	})
	if _, err := cqrwst.FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}
	ast := paperast.ExtractDocumentXML(`<w:document><w:body><w:p><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p></w:body></w:document>`)
	rules := templatecontract.Build(nil)
	contract := repaircontract.Build(rules, ast)

	result, err := NewVerifierWithTemplateProfileAndClosure(nil, rules, ast, contract).Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !result.Passed || result.ComplianceStatus != "format_compliant" {
		t.Fatalf("Verify() result = %#v, want compliant pass", result)
	}
}

func hasVerifyIssueKind(issues []Issue, kind string) bool {
	for _, issue := range issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}

func TestVerifierReturnsContextCanceled(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{"word/document.xml": `<w:document/>`})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewVerifier().Verify(ctx, docxPath)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify() error = %v, want context.Canceled", err)
	}
}

func writeVerifyTestDocx(t *testing.T, entries map[string]string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test docx: %v", err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	defer zw.Close()

	baseEntries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
	}
	for name, content := range entries {
		baseEntries[name] = content
	}
	for name, content := range baseEntries {
		writer, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}

	return path
}
