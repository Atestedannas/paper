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

func TestVerifierTreatsFieldShadingAsDisplayOnlyWarning(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>Clean final document with enough text and a table of contents field. </w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> TOC \o "1-3" \h \z \u </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p></w:body></w:document>`,
		"word/settings.xml": `<w:settings><w:updateFields w:val="true"/></w:settings>`,
	})

	result, err := NewVerifier().WithoutCQRWSTRules().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !result.Passed {
		t.Fatalf("standard field should not fail verification: %#v", result)
	}
	if len(result.FatalIssues) != 0 || len(result.RepairableIssues) != 0 {
		t.Fatalf("field shading display behavior should not be fatal or repairable: %#v", result)
	}
	if !hasVerifyIssueKind(result.Warnings, "field_shading_display_only") {
		t.Fatalf("Warnings = %#v, want field_shading_display_only", result.Warnings)
	}
}

func TestVerifierWarnsManualFigureTableCaptionsAreNotDynamic(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p><w:p><w:r><w:t>图1-1 系统架构图</w:t></w:r></w:p><w:p><w:r><w:t>表1-1 数据表</w:t></w:r></w:p></w:body></w:document>`,
	})

	result, err := NewVerifier().WithoutCQRWSTRules().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !result.Passed {
		t.Fatalf("manual caption warning should not fail verification: %#v", result)
	}
	if !hasVerifyIssueKind(result.Warnings, "manual_caption_not_dynamic") {
		t.Fatalf("Warnings = %#v, want manual_caption_not_dynamic", result.Warnings)
	}
}

func TestVerifierReportsCaptionNumberingProblems(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>` + "\u56fe1-1 \u7cfb\u7edf\u67b6\u6784\u56fe" + `</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>` + "\u56fe1-3 \u6a21\u5757\u56fe" + `</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>` + "\u56fe2 \u7ed3\u679c\u56fe" + `</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>` + "\u88681.1 \u6570\u636e\u8868" + `</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>` + "\u88682-1 \u7edf\u8ba1\u8868" + `</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})

	result, err := NewVerifier().WithoutCQRWSTRules().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("caption numbering problems should require review: %#v", result)
	}
	for _, kind := range []string{"caption_number_sequence", "caption_missing_chapter_number", "caption_numbering_mixed_format"} {
		if !hasVerifyIssueKind(result.RepairableIssues, kind) {
			t.Fatalf("RepairableIssues = %#v, want %s", result.RepairableIssues, kind)
		}
	}
}

func TestVerifierReportsLinkedHeaderFooterSections(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>Front matter clean text.</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rIdHeader1"/><w:footerReference w:type="default" r:id="rIdFooter1"/></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:r><w:t>Chapter one clean text.</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:sectPr></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:r><w:t>Chapter two clean text.</w:t></w:r></w:p>` +
			`<w:sectPr><w:headerReference w:type="default" r:id="rIdHeader1"/><w:footerReference w:type="default" r:id="rIdFooter1"/></w:sectPr>` +
			`</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdHeader1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>` +
			`<Relationship Id="rIdFooter1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>` +
			`</Relationships>`,
		"word/header1.xml": `<w:hdr><w:p><w:r><w:t>Shared header</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml": `<w:ftr><w:p><w:r><w:instrText> PAGE </w:instrText></w:r></w:p></w:ftr>`,
	})

	result, err := NewVerifier().WithoutCQRWSTRules().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("linked header/footer sections should require review: %#v", result)
	}
	for _, kind := range []string{"section_header_footer_inherited", "linked_header_footer_sections"} {
		if !hasVerifyIssueKind(result.RepairableIssues, kind) {
			t.Fatalf("RepairableIssues = %#v, want %s", result.RepairableIssues, kind)
		}
	}
}

func TestVerifierReportsFrontRomanBodyArabicPageNumberingProblems(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>Cover page text.</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:footerReference w:type="default" r:id="rIdFooterCover"/><w:pgNumType w:fmt="decimal" w:start="1"/></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:r><w:t>Abstract page text.</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:footerReference w:type="default" r:id="rIdFooterFront"/><w:pgNumType w:fmt="decimal" w:start="1"/></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:r><w:t>Chapter one body text.</w:t></w:r></w:p>` +
			`<w:sectPr><w:footerReference w:type="default" r:id="rIdFooterBody"/><w:pgNumType w:fmt="upperRoman" w:start="3"/></w:sectPr>` +
			`</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdFooterCover" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>` +
			`<Relationship Id="rIdFooterFront" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer2.xml"/>` +
			`<Relationship Id="rIdFooterBody" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer3.xml"/>` +
			`</Relationships>`,
		"word/footer1.xml": `<w:ftr><w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:instrText> PAGE </w:instrText></w:r></w:p></w:ftr>`,
		"word/footer2.xml": `<w:ftr><w:p><w:r><w:instrText> PAGE </w:instrText></w:r></w:p></w:ftr>`,
		"word/footer3.xml": `<w:ftr><w:p><w:pPr><w:jc w:val="right"/></w:pPr><w:r><w:t>3</w:t></w:r></w:p></w:ftr>`,
	})

	result, err := NewVerifier().WithoutCQRWSTRules().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("bad page numbering should require review: %#v", result)
	}
	for _, kind := range []string{"cover_page_number_present", "front_page_number_not_roman", "body_page_number_not_decimal_start", "footer_page_number_not_centered", "manual_page_number_not_dynamic"} {
		if !hasVerifyIssueKind(result.RepairableIssues, kind) {
			t.Fatalf("RepairableIssues = %#v, want %s", result.RepairableIssues, kind)
		}
	}
}

func TestVerifierAllowsSharedRunningHeaderAcrossSections(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rIdHeader1"/><w:footerReference w:type="default" r:id="rIdFooter1"/></w:sectPr></w:pPr></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rIdHeader1"/><w:footerReference w:type="default" r:id="rIdFooter2"/></w:sectPr></w:pPr></w:p>` +
			`</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdHeader1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>` +
			`<Relationship Id="rIdFooter1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>` +
			`<Relationship Id="rIdFooter2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer2.xml"/>` +
			`</Relationships>`,
		"word/header1.xml": `<w:hdr><w:p><w:r><w:t>Shared header</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml": `<w:ftr><w:p><w:r><w:instrText> PAGE </w:instrText></w:r></w:p></w:ftr>`,
		"word/footer2.xml": `<w:ftr><w:p><w:r><w:instrText> PAGE </w:instrText></w:r></w:p></w:ftr>`,
	})

	result, err := NewVerifier().WithoutCQRWSTRules().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if hasVerifyIssueKind(result.RepairableIssues, "linked_header_footer_sections") {
		t.Fatalf("shared running header should not require review: %#v", result.RepairableIssues)
	}
}

func TestVerifierReportsTableFormattingProblems(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p>` +
			`<w:tbl><w:tblPr><w:tblpPr w:tblpX="1"/><w:tblW w:w="0" w:type="auto"/><w:tblBorders><w:left w:val="single"/><w:right w:val="single"/><w:insideV w:val="single"/></w:tblBorders></w:tblPr>` +
			`<w:tr><w:tc><w:tcPr><w:vAlign w:val="top"/></w:tcPr><w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="Arial"/><w:sz w:val="24"/></w:rPr><w:t>Header</w:t></w:r></w:p></w:tc></w:tr>` +
			`<w:tr><w:tc><w:p><w:r><w:t>Body</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
			`</w:body></w:document>`,
	})

	result, err := NewVerifier().WithoutCQRWSTRules().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("bad table formatting should require review: %#v", result)
	}
	for _, kind := range []string{"table_three_line_format", "table_layout_not_centered", "table_repeating_header_missing", "table_cell_style_mismatch"} {
		if !hasVerifyIssueKind(result.RepairableIssues, kind) {
			t.Fatalf("RepairableIssues = %#v, want %s", result.RepairableIssues, kind)
		}
	}
}

func TestVerifierSkipsCoverLayoutTables(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p>` +
			`<w:tbl><w:tblPr><w:tblpPr w:tblpX="1"/><w:tblW w:w="0" w:type="auto"/></w:tblPr>` +
			`<w:tr><w:tc><w:p><w:r><w:t>题目</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>论文题目</w:t></w:r></w:p></w:tc></w:tr>` +
			`<w:tr><w:tc><w:p><w:r><w:t>学院</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>护理学院</w:t></w:r></w:p></w:tc></w:tr>` +
			`<w:tr><w:tc><w:p><w:r><w:t>指导教师</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>教师</w:t></w:r></w:p></w:tc></w:tr>` +
			`</w:tbl></w:body></w:document>`,
	})

	result, err := NewVerifier().WithoutCQRWSTRules().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	for _, kind := range []string{"table_three_line_format", "table_layout_not_centered", "table_repeating_header_missing", "table_cell_style_mismatch"} {
		if hasVerifyIssueKind(result.RepairableIssues, kind) {
			t.Fatalf("cover layout table should not raise %s: %#v", kind, result.RepairableIssues)
		}
	}
}

func TestVerifierAllowsCompactTableCellSize(t *testing.T) {
	table := `<w:tbl><w:tr><w:tc><w:tcPr><w:vAlign w:val="center"/></w:tcPr><w:p><w:pPr><w:spacing w:line="240"/><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Times New Roman" w:eastAsia="SimSun"/><w:sz w:val="16"/></w:rPr><w:t>P</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	if !tableCellsFollowStyle(table) {
		t.Fatalf("compact 8pt table text should be accepted")
	}
}

func TestVerifierReportsImageFormulaAndReferenceProblems(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:t>Clean final document with enough text.</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>` +
			`<w:p><w:r><w:drawing><wp:anchor><wp:extent cx="8000000" cy="2000000"/></wp:anchor></w:drawing></w:r></w:p>` +
			`<w:p><w:r><w:t>` + "\u56fe2 \u7cfb\u7edf\u67b6\u6784\u56fe" + `</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>` + "\u5982\u56fe2-1\u548c\u88683-1\u6240\u793a\uff0c\u5f0f(2-1)\u53ef\u5f97\u5230\u7ed3\u679c\u3002" + `</w:t></w:r></w:p>` +
			`<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="dxa"/><w:jc w:val="left"/><w:tblBorders><w:top w:val="nil"/><w:bottom w:val="nil"/></w:tblBorders></w:tblPr>` +
			`<w:tr><w:tc><w:p><m:oMath><m:r><m:t>E=mc2</m:t></m:r></m:oMath></w:p></w:tc><w:tc><w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:t>(2-1)</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
			`</w:body></w:document>`,
	})

	result, err := NewVerifier().WithoutCQRWSTRules().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("image/formula/reference problems should require review: %#v", result)
	}
	for _, kind := range []string{
		"floating_image_anchor",
		"image_width_over_text_area",
		"image_keep_with_caption_missing",
		"figure_caption_missing_chapter_number",
		"manual_formula_number_not_dynamic",
		"formula_layout_mismatch",
	} {
		if !hasVerifyIssueKind(result.RepairableIssues, kind) {
			t.Fatalf("RepairableIssues = %#v, want %s", result.RepairableIssues, kind)
		}
	}
	if !hasVerifyIssueKind(result.Warnings, "manual_cross_reference") {
		t.Fatalf("Warnings = %#v, want manual_cross_reference", result.Warnings)
	}
}

func TestVerifierReportsFieldsNotMarkedForUpdate(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> SEQ \u56fe \* ARABIC </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p></w:body></w:document>`,
		"word/settings.xml": `<w:settings></w:settings>`,
	})

	result, err := NewVerifier().WithoutCQRWSTRules().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("refreshable fields without updateFields should require review: %#v", result)
	}
	if !hasVerifyIssueKind(result.RepairableIssues, "fields_not_marked_for_update") {
		t.Fatalf("RepairableIssues = %#v, want fields_not_marked_for_update", result.RepairableIssues)
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

func TestVerifierSkipsCoverLogoCaptionCheck(t *testing.T) {
	docxPath := writeVerifyTestDocx(t, map[string]string{
		"word/document.xml": `<w:document><w:body>` +
			`<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1000000" cy="1000000"/></wp:inline></w:drawing></w:r></w:p>` +
			`<w:p><w:r><w:t>本科毕业论文/设计</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>正文</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})

	result, err := NewVerifier().WithoutCQRWSTRules().Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if hasVerifyIssueKind(result.RepairableIssues, "image_keep_with_caption_missing") {
		t.Fatalf("cover logo should not require a figure caption: %#v", result.RepairableIssues)
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
