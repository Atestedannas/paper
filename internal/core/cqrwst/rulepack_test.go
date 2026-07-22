package cqrwst

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

func TestFixDOCXNormalizesDeterministicCQRWSTTextRules(t *testing.T) {
	t.Setenv("CQRWST_ALLOW_CONTENT_NORMALIZATION", "true")
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>2026年 3 月</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1.1研究背景</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1.3 国内外研究现状：</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1.3.1 国外研究现状</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1.3.2 国内研究现状</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>5 结论/总结</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>2/Z</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Wald2</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>[2] 郑晓萌.多学科团队模式[D].石河子大学,2016.</w:t></w:r></w:p>`,
	)

	result, err := FixDOCX(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}
	if result.FixCount < 9 {
		t.Fatalf("FixCount = %d, want at least 9 text fixes", result.FixCount)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	for _, want := range []string{
		"2026年3月",
		"1.1 研究背景",
		"1.4 国内外研究现状",
		"1.4.1 国外研究现状",
		"1.4.2 国内研究现状",
		"5 结论",
		"χ²/Z",
		"Wald χ²",
		"[D].石河子: 石河子大学,2016.",
	} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("document XML missing %q: %s", want, documentXML)
		}
	}
}

func TestFixDOCXNormalizesSplitRunVisibleTextRules(t *testing.T) {
	t.Setenv("CQRWST_ALLOW_CONTENT_NORMALIZATION", "true")
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>1.3</w:t></w:r><w:r><w:t>国内外研究现状：</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1.3.1</w:t></w:r><w:r><w:t>国内现状</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1.3.2</w:t></w:r><w:r><w:t>国外研究现状：</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>3</w:t></w:r><w:r><w:t>研究结果</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Key words:</w:t></w:r><w:r><w:t> Community diabetes</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>2026年 </w:t></w:r><w:r><w:t>3  月</w:t></w:r></w:p>`,
	)

	result, err := FixDOCX(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}
	if result.FixCount == 0 {
		t.Fatal("FixCount = 0, want split-run text fixes")
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	for _, want := range []string{
		"1.4 国内外研究现状",
		"1.4.2 国内研究现状",
		"1.4.1 国外研究现状",
		"3 研究结果",
		"Key words:",
		"Community diabetes",
		"2026年3月",
	} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("document XML missing %q: %s", want, documentXML)
		}
	}
}

func TestFixDOCXStylesFrontMatterWithoutChangingContent(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>摘要：目的  探讨社区二型糖尿病患者疾病知识认知水平。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>关键词：社区二型糖尿病；认知水平；影响因素</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Abstract: Objective To explore community diabetes.</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Key words: Community type 2 diabetes; Cognitive level</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>正文内容。</w:t></w:r></w:p>`,
	)

	result, err := FixDOCX(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}
	if result.FixCount == 0 {
		t.Fatal("FixCount = 0, want front-matter style fixes")
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if strings.Contains(documentXML, "摘  要") {
		t.Fatalf("FixDOCX should not rewrite inline abstract content into a separate title: %s", documentXML)
	}
	assertParagraphHas(t, documentXML, "摘要：", []string{
		`w:eastAsia="黑体"`,
		`w:sz w:val="24"`,
		`<w:b/>`,
		`目的  探讨`,
	})
	assertParagraphHas(t, documentXML, "目的  探讨", []string{
		`w:eastAsia="宋体"`,
		`w:sz w:val="24"`,
		`w:firstLineChars="200"`,
	})
	assertParagraphHas(t, documentXML, "关键词：", []string{
		`w:eastAsia="黑体"`,
		`<w:b/>`,
		`社区二型糖尿病`,
	})
	if strings.Contains(documentXML, "<w:t>Abstract</w:t>") {
		t.Fatalf("FixDOCX should not rewrite inline English abstract into a separate title: %s", documentXML)
	}
	assertParagraphHas(t, documentXML, "Abstract:", []string{
		`w:ascii="Times New Roman"`,
		`w:sz w:val="24"`,
		`<w:b/>`,
		`Objective To explore`,
	})
	assertParagraphHas(t, documentXML, "Objective To explore", []string{
		`w:ascii="Times New Roman"`,
		`w:sz w:val="24"`,
		`w:jc w:val="both"`,
	})
	assertParagraphHas(t, documentXML, "Key words:", []string{
		`w:ascii="Times New Roman"`,
		`<w:b/>`,
		`Community type 2 diabetes`,
	})

	checkResult, err := CheckDOCX(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("CheckDOCX() error = %v", err)
	}
	for _, issue := range checkResult.Issues {
		if issue.RuleID == "cqrwst-frontmatter-structure" {
			t.Fatalf("front-matter structural fix should be idempotent, issue=%#v", issue)
		}
	}
}

func TestFixDOCXDoesNotApplyBodyStyleBeforeMainMatter(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="黑体"/><w:sz w:val="36"/></w:rPr><w:t>本科毕业论文/设计</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>题目</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘要：摘要正文。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>目录</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论1</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>参考文献12</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>正文内容。</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	coverParagraph := paragraphContaining(documentXML, "本科毕业论文/设计")
	for _, forbidden := range []string{`w:firstLineChars="200"`, `w:jc w:val="both"`, `w:sz w:val="24"`} {
		if strings.Contains(coverParagraph, forbidden) {
			t.Fatalf("cover paragraph should not be restyled as body, found %s in %s", forbidden, coverParagraph)
		}
	}
	tocReferenceParagraph := paragraphContaining(documentXML, "参考文献12")
	if strings.Contains(tocReferenceParagraph, `w:firstLineChars="200"`) || strings.Contains(tocReferenceParagraph, `w:jc w:val="both"`) {
		t.Fatalf("toc entry should not start body styling before real body: %s", tocReferenceParagraph)
	}
}

func TestCheckDOCXReportsRepairableCQRWSTIssues(t *testing.T) {
	t.Setenv("CQRWST_ALLOW_CONTENT_NORMALIZATION", "true")
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>1.1研究背景</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>5 结论/总结</w:t></w:r></w:p>`,
	)

	result, err := CheckDOCX(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("CheckDOCX() error = %v", err)
	}
	if result.Passed {
		t.Fatal("CheckDOCX() Passed = true, want false")
	}
	if !hasIssueKind(result.Issues, "repairable_text") {
		t.Fatalf("Issues = %#v, want repairable_text", result.Issues)
	}
}

func TestFixDOCXAppliesCQRWSTParagraphAndRunStyles(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>摘要：</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘要内容。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>关键词：</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>护理；研究；管理</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Abstract：</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>English abstract body.</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Key words：</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Nursing,  Research,  Management</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1.1 研究背景</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1.1.1 研究概述</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>正文内容。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>[1] 张三.题名[J].期刊,2024,1(1):1-2.</w:t></w:r></w:p>`,
	)

	result, err := FixDOCX(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}
	if result.FixCount == 0 {
		t.Fatal("FixCount = 0, want style fixes")
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	assertParagraphHas(t, documentXML, "摘要：", []string{
		`w:eastAsia="黑体"`,
		`w:sz w:val="30"`,
		`<w:b/>`,
		`w:firstLineChars="200"`,
		`w:line="360"`,
		`w:afterLines="200"`,
	})
	assertParagraphHas(t, documentXML, "Abstract：", []string{
		`w:ascii="Times New Roman"`,
		`w:sz w:val="30"`,
		`<w:b/>`,
		`w:firstLineChars="200"`,
	})
	assertParagraphHas(t, documentXML, "1 绪论", []string{
		`w:eastAsia="宋体"`,
		`w:sz w:val="32"`,
		`<w:b/>`,
		`w:before="240"`,
		`w:after="240"`,
		`w:jc w:val="center"`,
	})
	assertParagraphHas(t, documentXML, "1.1 研究背景", []string{
		`w:eastAsia="宋体"`,
		`w:sz w:val="30"`,
		`<w:b/>`,
		`w:jc w:val="left"`,
	})
	assertParagraphHas(t, documentXML, "正文内容。", []string{
		`w:eastAsia="宋体"`,
		`w:ascii="Times New Roman"`,
		`w:sz w:val="24"`,
		`w:firstLineChars="200"`,
		`w:jc w:val="both"`,
	})
	assertParagraphHas(t, documentXML, "[1] 张三", []string{
		`w:eastAsia="宋体"`,
		`w:ascii="Times New Roman"`,
		`w:sz w:val="21"`,
		`w:firstLineChars="0"`,
	})
}

func TestCheckDOCXReportsMissingCQRWSTStyleIssues(t *testing.T) {
	docxPath := writeCQRWSTDocx(t, `<w:p><w:r><w:t>摘要：</w:t></w:r></w:p>`)

	result, err := CheckDOCX(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("CheckDOCX() error = %v", err)
	}
	if result.Passed {
		t.Fatal("CheckDOCX() Passed = true, want false")
	}
	if !hasIssueKind(result.Issues, "repairable_style") {
		t.Fatalf("Issues = %#v, want repairable_style", result.Issues)
	}
}

func TestFixDOCXAppliesCQRWSTSectionHeaderAndFooter(t *testing.T) {
	docxPath := writeCQRWSTDocx(t, `<w:p><w:r><w:t>正文内容。</w:t></w:r></w:p>`)

	result, err := FixDOCX(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}
	if result.FixCount == 0 {
		t.Fatal("FixCount = 0, want section/header/footer fixes")
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	for _, want := range []string{
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`,
		`<w:headerReference w:type="default" r:id="rIdCQRWSTHeader1"/>`,
		`<w:footerReference w:type="default" r:id="rIdCQRWSTFooter1"/>`,
		`<w:pgSz w:w="11906" w:h="16838"/>`,
		`<w:pgMar w:top="1418" w:right="1134" w:bottom="1134" w:left="1418" w:header="851" w:footer="992" w:gutter="0"/>`,
	} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("document XML missing %q: %s", want, documentXML)
		}
	}

	headerXML := readCQRWSTEntry(t, docxPath, "word/header1.xml")
	for _, want := range []string{"重庆人文科技学院", "本科毕业论文", `w:val="center"`, `w:sz w:val="18"`} {
		if !strings.Contains(headerXML, want) {
			t.Fatalf("header XML missing %q: %s", want, headerXML)
		}
	}

	footerXML := readCQRWSTEntry(t, docxPath, "word/footer1.xml")
	for _, want := range []string{"第", "页", "共", "NUMPAGES", "PAGE", `w:val="center"`, `w:sz w:val="18"`} {
		if !strings.Contains(footerXML, want) {
			t.Fatalf("footer XML missing %q: %s", want, footerXML)
		}
	}

	relsXML := readCQRWSTEntry(t, docxPath, "word/_rels/document.xml.rels")
	for _, want := range []string{"rIdCQRWSTHeader1", "header1.xml", "rIdCQRWSTFooter1", "footer1.xml"} {
		if !strings.Contains(relsXML, want) {
			t.Fatalf("rels XML missing %q: %s", want, relsXML)
		}
	}

	contentTypesXML := readCQRWSTEntry(t, docxPath, "[Content_Types].xml")
	for _, want := range []string{"/word/header1.xml", "/word/footer1.xml"} {
		if !strings.Contains(contentTypesXML, want) {
			t.Fatalf("content types XML missing %q: %s", want, contentTypesXML)
		}
	}
}

func TestFixDOCXPreservesExistingDocumentRelationships(t *testing.T) {
	docxPath := writeCQRWSTDocxWithEntries(t,
		`<w:p><w:r><w:drawing><a:blip r:embed="rId2"/></w:drawing><w:t>图片说明</w:t></w:r></w:p>`,
		map[string]string{
			"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/></Relationships>`,
			"word/media/image1.png":        "fake-image",
		},
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	relsXML := readCQRWSTEntry(t, docxPath, "word/_rels/document.xml.rels")
	for _, want := range []string{"rId2", "media/image1.png", "rIdCQRWSTHeader1", "rIdCQRWSTFooter1"} {
		if !strings.Contains(relsXML, want) {
			t.Fatalf("document relationships missing %q: %s", want, relsXML)
		}
	}
}

func TestFixDOCXBuildsHeaderTextFromCoverFields(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>本科毕业论文/设计</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>专业</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>护理学</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>班级</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>2022级护理学5班</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘  要</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘要正文。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>正文内容。</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	headerXML := readCQRWSTEntry(t, docxPath, "word/header1.xml")
	want := "重庆人文科技学院2026届护理学专业本科毕业论文"
	if !strings.Contains(headerXML, want) {
		t.Fatalf("header XML missing extracted header %q: %s", want, headerXML)
	}
	if strings.Contains(headerXML, "XXX") {
		t.Fatalf("header XML should not keep placeholders after extraction: %s", headerXML)
	}
}

func TestCQRWSTHeaderTextRemovesExtractionWhitespace(t *testing.T) {
	headerXML := cqrwstHeaderXML(" 重庆人文科技学院 2026 届 护理学 专业本科毕业论文 ")
	want := "重庆人文科技学院2026届护理学专业本科毕业论文"
	if !strings.Contains(headerXML, want) {
		t.Fatalf("header XML missing compact header %q: %s", want, headerXML)
	}
	forbidden := []string{"学院 2026", "2026 届", "护理学 专业"}
	for _, text := range forbidden {
		if strings.Contains(headerXML, text) {
			t.Fatalf("header XML should not contain whitespace fragment %q: %s", text, headerXML)
		}
	}
}

func TestFixDOCXUsesVisibleDoubleHeaderDivider(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>专业</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>护理学</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>班级</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>2022级护理学5班</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘  要</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘要正文。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	headerXML := readCQRWSTEntry(t, docxPath, "word/header1.xml")
	for _, want := range []string{
		`<w:pBdr><w:bottom w:val="double"`,
		`w:sz="4"`,
		`w:space="0"`,
		`w:color="000000"`,
	} {
		if !strings.Contains(headerXML, want) {
			t.Fatalf("header divider missing %q: %s", want, headerXML)
		}
	}
}

func TestFixDOCXOverwritesStaleHeaderPartsWithDoubleDivider(t *testing.T) {
	docxPath := writeCQRWSTDocxWithEntries(t,
		`<w:p><w:r><w:t>摘要：摘要正文。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`,
		map[string]string{
			"word/header2.xml": `<w:hdr><w:p><w:r><w:t>old stale header</w:t></w:r></w:p></w:hdr>`,
		},
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	header2XML := readCQRWSTEntry(t, docxPath, "word/header2.xml")
	for _, want := range []string{`w:val="double"`, `w:sz="4"`, `w:space="0"`, `w:color="000000"`} {
		if !strings.Contains(header2XML, want) {
			t.Fatalf("stale header part missing double divider %q: %s", want, header2XML)
		}
	}
	if strings.Contains(header2XML, "old stale header") {
		t.Fatalf("stale header content was not replaced: %s", header2XML)
	}
}

func TestFixDOCXPreservesCoverTableAlignmentAndSectionBoundary(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:tbl><w:tblPr><w:tblW w:w="7819" w:type="dxa"/><w:jc w:val="start"/><w:tblLayout w:type="fixed"/></w:tblPr><w:tr><w:tc><w:p><w:r><w:t>题目</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`+
			`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rIdOldHeader"/><w:footerReference w:type="default" r:id="rIdOldFooter"/><w:type w:val="nextPage"/></w:sectPr></w:pPr><w:r><w:t>2026年3月</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘要：摘要正文。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if !strings.Contains(documentXML, `<w:jc w:val="start"/>`) {
		t.Fatalf("cover table alignment should preserve w:val=start for WPS layout: %s", documentXML)
	}
	coverBoundary := paragraphContaining(documentXML, "2026年3月")
	if !strings.Contains(coverBoundary, "<w:sectPr") || !strings.Contains(coverBoundary, `<w:type w:val="nextPage"/>`) {
		t.Fatalf("cover section boundary should be preserved: %s", coverBoundary)
	}
	if strings.Contains(coverBoundary, "rIdOldHeader") || strings.Contains(coverBoundary, "rIdOldFooter") {
		t.Fatalf("stale header/footer refs should be removed without deleting section boundary: %s", coverBoundary)
	}
}

func TestFixDOCXDoesNotSwallowParagraphClosersWhenParagraphHasSectionProperties(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:pPr><w:sectPr><w:type w:val="nextPage"/></w:sectPr></w:pPr><w:r><w:t>Cover</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Body</w:t></w:r></w:p>`+
			`<w:sectPr><w:pgSz w:w="1" w:h="2"/></w:sectPr>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if err := xml.Unmarshal([]byte(documentXML), new(any)); err != nil {
		t.Fatalf("document.xml should remain well-formed, error=%v xml=%s", err, documentXML)
	}
	if strings.Count(documentXML, "</w:pPr>") != 1 || strings.Count(documentXML, "</w:p>") != 2 {
		t.Fatalf("paragraph closing tags were swallowed: %s", documentXML)
	}
}

func TestFixDOCXDoesNotCorruptSelfClosingParagraphRunProperties(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:pPr><w:rPr/></w:pPr><w:r><w:rPr><w:kern w:val="2"/></w:rPr><w:t>文本框内容</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if err := xml.Unmarshal([]byte(documentXML), new(any)); err != nil {
		t.Fatalf("document.xml should remain well-formed, error=%v xml=%s", err, documentXML)
	}
}

func TestFixDOCXRepairsMalformedParagraphRunProperties(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:pPr><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="24"/><w:rPr/></w:pPr><w:r><w:t>文本框内容</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if err := xml.Unmarshal([]byte(documentXML), new(any)); err != nil {
		t.Fatalf("document.xml should be repaired to well-formed XML, error=%v xml=%s", err, documentXML)
	}
	if strings.Contains(documentXML, `<w:rPr/></w:pPr>`) {
		t.Fatalf("malformed paragraph run properties were not repaired: %s", documentXML)
	}
}

func TestCheckDOCXReportsMissingCQRWSTSectionIssues(t *testing.T) {
	docxPath := writeCQRWSTDocx(t, `<w:p><w:r><w:t>正文内容。</w:t></w:r></w:p>`)

	result, err := CheckDOCX(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("CheckDOCX() error = %v", err)
	}
	if result.Passed {
		t.Fatal("CheckDOCX() Passed = true, want false")
	}
	if !hasIssueKind(result.Issues, "repairable_section") {
		t.Fatalf("Issues = %#v, want repairable_section", result.Issues)
	}
}

func TestFixDOCXAppliesCQRWSTMultiSectionPageNumbering(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>封面信息</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>目录</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘要：</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘要内容。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>正文内容。</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	coverParagraph := paragraphContaining(documentXML, "封面信息")
	if !strings.Contains(coverParagraph, `<w:sectPr><w:type w:val="nextPage"/></w:sectPr>`) {
		t.Fatalf("cover paragraph missing next-page section break: %s", coverParagraph)
	}
	for _, forbidden := range []string{"w:pgMar", "w:headerReference", "w:footerReference"} {
		if strings.Contains(coverParagraph, forbidden) {
			t.Fatalf("cover paragraph should preserve cover layout and not contain %s: %s", forbidden, coverParagraph)
		}
	}

	tocParagraph := paragraphContaining(documentXML, "目录")
	if !strings.Contains(tocParagraph, `<w:type w:val="nextPage"/>`) {
		t.Fatalf("toc paragraph missing next-page section break: %s", tocParagraph)
	}
	if strings.Contains(tocParagraph, "w:footerReference") {
		t.Fatalf("toc paragraph should not contain footer reference: %s", tocParagraph)
	}

	abstractParagraph := paragraphContaining(documentXML, "摘要内容。")
	for _, want := range []string{
		`<w:headerReference w:type="default" r:id="rIdCQRWSTHeader1"/>`,
		`<w:footerReference w:type="default" r:id="rIdCQRWSTFooter1"/>`,
		`<w:pgNumType w:start="1" w:fmt="upperRoman"/>`,
	} {
		if !strings.Contains(abstractParagraph, want) {
			t.Fatalf("abstract section paragraph missing %q: %s", want, abstractParagraph)
		}
	}

	if !strings.Contains(documentXML, `<w:pgNumType w:start="1" w:fmt="decimal"/>`) {
		t.Fatalf("document missing decimal body page numbering: %s", documentXML)
	}
}

func TestFixDOCXPrunesBlankParagraphsBeforeSectionStarts(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>封面信息</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘要：摘要正文。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>Key words: Nursing</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t></w:t></w:r></w:p>`+
			`<w:p><w:r><w:t></w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>目      录</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论1</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t></w:t></w:r></w:p>`+
			`<w:p><w:r><w:t></w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>正文内容。</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if strings.Count(documentXML, `<w:t></w:t>`) != 0 {
		t.Fatalf("blank paragraphs before section starts should be pruned: %s", documentXML)
	}
	keywordsParagraph := paragraphContaining(documentXML, "Key words:")
	if !strings.Contains(keywordsParagraph, `<w:sectPr>`) {
		t.Fatalf("abstract section break should move to last non-empty abstract paragraph: %s", keywordsParagraph)
	}
	tocEntryParagraph := paragraphContaining(documentXML, "1 绪论1")
	if !strings.Contains(tocEntryParagraph, `<w:sectPr>`) {
		t.Fatalf("toc section break should move to last non-empty toc paragraph: %s", tocEntryParagraph)
	}
}

func TestFixDOCXStartsReferencesAndAcknowledgementsOnNewPages(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>正文内容。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t></w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>参考文献</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>[1] 张三.题名[J].期刊,2024,1(1):1-2.</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t></w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>致      谢</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>感谢内容。</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	referenceTitleParagraph := paragraphContaining(documentXML, "参考文献")
	if !strings.Contains(referenceTitleParagraph, `<w:br w:type="page"/>`) {
		t.Fatalf("references title should start on a new page: %s", referenceTitleParagraph)
	}
	ackParagraph := paragraphContaining(documentXML, "致")
	if !strings.Contains(ackParagraph, `<w:br w:type="page"/>`) {
		t.Fatalf("acknowledgements title should start on a new page: %s", ackParagraph)
	}
	if strings.Contains(documentXML, `<w:t></w:t>`) {
		t.Fatalf("blank paragraphs before forced new-page titles should be pruned: %s", documentXML)
	}
}

func TestFixDOCXDecodesDoubleEscapedVisibleTextEntities(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>Abstract: patients&amp;#39; cognition (P&amp;lt;0.05)</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if strings.Contains(documentXML, `&amp;#39;`) || strings.Contains(documentXML, `&amp;lt;`) {
		t.Fatalf("double-escaped visible entities should be decoded: %s", documentXML)
	}
	if !strings.Contains(documentXML, `patients&#39; cognition`) || !strings.Contains(documentXML, `P&lt;0.05`) {
		t.Fatalf("decoded entities should display as apostrophe and less-than: %s", documentXML)
	}
}

func TestFixDOCXRemovesStaleHeaderFooterSectionsBeforeRebuilding(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rIdOldHeader"/><w:footerReference w:type="default" r:id="rIdOldFooter"/><w:type w:val="nextPage"/></w:sectPr></w:pPr><w:r><w:t>2026年3月</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>论文题目</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘  要</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘要正文。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>目      录</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论1</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>正文内容。</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if strings.Contains(documentXML, "rIdOldHeader") || strings.Contains(documentXML, "rIdOldFooter") {
		t.Fatalf("stale header/footer section references were not removed: %s", documentXML)
	}
	coverEndParagraph := paragraphContaining(documentXML, "论文题目")
	if strings.Contains(coverEndParagraph, `<w:type w:val="nextPage"/>`) {
		t.Fatalf("thesis title should stay on the same page as abstract, not end with a next-page section: %s", coverEndParagraph)
	}
	abstractEndParagraph := paragraphContaining(documentXML, "摘要正文。")
	for _, want := range []string{`r:id="rIdCQRWSTHeader1"`, `r:id="rIdCQRWSTFooter1"`, `w:fmt="upperRoman"`} {
		if !strings.Contains(abstractEndParagraph, want) {
			t.Fatalf("abstract section missing %q: %s", want, abstractEndParagraph)
		}
	}
	tocEndParagraph := paragraphContaining(documentXML, "1 绪论1")
	if !strings.Contains(tocEndParagraph, `r:id="rIdCQRWSTHeader1"`) {
		t.Fatalf("toc section should keep header: %s", tocEndParagraph)
	}
	if strings.Contains(tocEndParagraph, `footerReference`) || strings.Contains(tocEndParagraph, `pgNumType`) {
		t.Fatalf("toc section should not have footer/page number: %s", tocEndParagraph)
	}
}

func TestFixDOCXKeepsThesisTitleAndAbstractOnSamePage(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>本科毕业论文/设计</w:t></w:r></w:p>`+
			`<w:p><w:pPr><w:sectPr><w:type w:val="nextPage"/></w:sectPr></w:pPr><w:r><w:t>2026年3月</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>社区2型糖尿病患者疾病知识认知现状及影响因素分析</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>摘要：摘要正文。</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>关键词：护理；糖尿病</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>正文内容。</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	titleParagraph := paragraphContaining(documentXML, "认知现状及影响因素分析")
	if strings.Contains(titleParagraph, `<w:type w:val="nextPage"/>`) {
		t.Fatalf("thesis title should not contain next-page section break before abstract: %s", titleParagraph)
	}
	keywordsParagraph := paragraphContaining(documentXML, "关键词：")
	for _, want := range []string{`r:id="rIdCQRWSTHeader1"`, `r:id="rIdCQRWSTFooter1"`, `w:fmt="upperRoman"`} {
		if !strings.Contains(keywordsParagraph, want) {
			t.Fatalf("front matter section should still carry abstract page numbering %q: %s", want, keywordsParagraph)
		}
	}
}

func TestFixDOCXStylesNestedTextBoxParagraphsUntilStable(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>1 绪论</w:t></w:r></w:p>`+
			`<w:p><w:pPr><w:spacing w:line="360" w:lineRule="auto"/><w:ind w:firstLineChars="200"/><w:jc w:val="both"/></w:pPr>`+
			`<w:r><w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:eastAsia="宋体"/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr>`+
			`<mc:AlternateContent><mc:Choice><w:drawing><w:txbxContent>`+
			`<w:p><w:pPr><w:spacing w:line="360" w:lineRule="auto"/><w:ind w:firstLineChars="200"/><w:jc w:val="both"/></w:pPr><w:r><w:rPr><w:kern w:val="2"/></w:rPr><w:t>设计调查问卷，确定研究工具</w:t></w:r></w:p>`+
			`</w:txbxContent></w:drawing></mc:Choice></mc:AlternateContent></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	result, err := CheckDOCX(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("CheckDOCX() error = %v", err)
	}
	for _, issue := range result.Issues {
		if issue.RuleID == "cqrwst-body-style" {
			t.Fatalf("nested text-box paragraph style should be stable after FixDOCX, issue=%#v", issue)
		}
	}
}

func TestCleanupOldDebugTracesRemovesExpiredDirectories(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	recentDir := filepath.Join(root, "recent")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(recentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	cleanupOldDebugTraces(root, time.Now().Add(-7*24*time.Hour))
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("expired trace still exists: %v", err)
	}
	if _, err := os.Stat(recentDir); err != nil {
		t.Fatalf("recent trace removed: %v", err)
	}
}

func TestFixDOCXWritesCQRWSTDebugTraceWhenEnabled(t *testing.T) {
	debugDir := t.TempDir()
	t.Setenv("CQRWST_DEBUG", "1")
	t.Setenv("CQRWST_DEBUG_DIR", debugDir)

	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:t>1.1鐮旂┒鑳屾櫙</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>鎽樿锛?/w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>1 缁</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	entries, err := os.ReadDir(debugDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", debugDir, err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("debugDir entries = %#v, want one trace directory", entries)
	}
	traceDir := filepath.Join(debugDir, entries[0].Name())
	for _, name := range []string{
		"00_fix_input_document.xml",
		"01_after_text_rules.xml",
		"02_after_style_rules.xml",
		"03_after_section_rules.xml",
		"04_fix_issues.txt",
		"05_fix_paragraphs.txt",
	} {
		if _, err := os.Stat(filepath.Join(traceDir, name)); err != nil {
			t.Fatalf("debug trace missing %s: %v", name, err)
		}
	}
	report, err := os.ReadFile(filepath.Join(traceDir, "05_fix_paragraphs.txt"))
	if err != nil {
		t.Fatalf("read paragraph report: %v", err)
	}
	if !strings.Contains(string(report), "paragraphs=") || !strings.Contains(string(report), "style=") {
		t.Fatalf("paragraph report missing useful probes: %s", string(report))
	}
}

func TestFixDOCXReturnsContextCanceled(t *testing.T) {
	docxPath := writeCQRWSTDocx(t, `<w:p><w:r><w:t>1.1研究背景</w:t></w:r></w:p>`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FixDOCX(ctx, docxPath)
	if err != context.Canceled {
		t.Fatalf("FixDOCX() error = %v, want context.Canceled", err)
	}
}

func writeCQRWSTDocx(t *testing.T, bodyXML string) string {
	t.Helper()
	return writeCQRWSTDocxWithEntries(t, bodyXML, nil)
}

func writeCQRWSTDocxWithEntries(t *testing.T, bodyXML string, extraEntries map[string]string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "cqrwst.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test docx: %v", err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	defer zw.Close()

	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/document.xml":   `<w:document><w:body>` + bodyXML + `</w:body></w:document>`,
	}
	for name, content := range extraEntries {
		entries[name] = content
	}
	for name, content := range entries {
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

func readCQRWSTDocumentXML(t *testing.T, docxPath string) string {
	t.Helper()
	return readCQRWSTEntry(t, docxPath, documentTarget)
}

func readCQRWSTEntry(t *testing.T, docxPath string, entryName string) string {
	t.Helper()

	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		t.Fatalf("open docx: %v", err)
	}
	content, ok := pkg.Get(entryName)
	if !ok {
		t.Fatalf("%s missing", entryName)
	}
	return string(content)
}

func assertParagraphHas(t *testing.T, documentXML string, text string, wants []string) {
	t.Helper()

	paragraph := paragraphContaining(documentXML, text)
	if paragraph == "" {
		t.Fatalf("paragraph containing %q not found in %s", text, documentXML)
	}
	for _, want := range wants {
		if !strings.Contains(paragraph, want) {
			t.Fatalf("paragraph %q missing %q: %s", text, want, paragraph)
		}
	}
}

func paragraphContaining(documentXML string, text string) string {
	for _, paragraph := range paragraphPattern.FindAllString(documentXML, -1) {
		if strings.Contains(paragraph, text) {
			return paragraph
		}
	}
	return ""
}

func hasIssueKind(issues []Issue, kind string) bool {
	for _, issue := range issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}

func TestFixDOCXAddsMissingTableAndFigureCaptions(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		"<w:p><w:r><w:t>1 \u7eea\u8bba</w:t></w:r></w:p>"+
			`<w:tbl><w:tblPr/><w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`+
			"<w:p><w:r><w:t>2.1.6 \u6280\u672f\u8def\u7ebf\u56fe</w:t></w:r></w:p>"+
			`<w:p><w:r><w:drawing/></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if !strings.Contains(documentXML, "\u88681.1 \u8868\u683c") {
		t.Fatalf("missing generated table caption: %s", documentXML)
	}
	if !strings.Contains(documentXML, "\u56fe1.1 \u6280\u672f\u8def\u7ebf\u56fe") {
		t.Fatalf("missing generated figure caption from heading context: %s", documentXML)
	}
	assertParagraphHas(t, documentXML, "\u88681.1", []string{`w:jc w:val="center"`, `w:sz w:val="21"`})
	assertParagraphHas(t, documentXML, "\u56fe1.1", []string{`w:jc w:val="center"`, `w:sz w:val="21"`})
}

func TestBodyStartSupportsChineseNumbering(t *testing.T) {
	for _, text := range []string{"第一章 绪论", "第1章 绪论", "一、绪论"} {
		if !isBodyStartParagraph(text) {
			t.Fatalf("isBodyStartParagraph(%q) = false", text)
		}
	}
}

func TestMergedBodyLayoutTableIsNotDataTable(t *testing.T) {
	block := semanticBlock{IsTable: true, InBody: true, Rows: 2, Cells: 4, HasMergedCells: true, AverageCellLen: 5}
	if shouldTreatAsDataTable(block) {
		t.Fatal("merged short-cell layout table classified as data table")
	}
}

func TestBodyTableLabelsDoNotForceCoverLayoutClassification(t *testing.T) {
	block := semanticBlock{IsTable: true, InBody: true, Text: "专业选择 学生偏好", Rows: 10, Cells: 20, AverageCellLen: 18}
	if !shouldTreatAsDataTable(block) {
		t.Fatal("body data table was classified as cover layout table")
	}
}

func TestFixDOCXNormalizesUnnumberedTableCaptionWithoutDuplicating(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		"<w:p><w:r><w:t>1 \u7eea\u8bba</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>\u8868 \u8bbf\u8c08\u5bf9\u8c61\u57fa\u672c\u4fe1\u606f\u8868</w:t></w:r></w:p>"+
			`<w:tbl><w:tblPr/><w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if got := strings.Count(documentXML, "\u88681.1"); got != 1 {
		t.Fatalf("table caption should be normalized once, got %d occurrences: %s", got, documentXML)
	}
	if !strings.Contains(documentXML, "\u88681.1 \u8bbf\u8c08\u5bf9\u8c61\u57fa\u672c\u4fe1\u606f\u8868") {
		t.Fatalf("missing normalized table caption: %s", documentXML)
	}
}

func TestFixDOCXDoesNotCaptionCoverLayoutBeforeBody(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		`<w:p><w:r><w:drawing/></w:r></w:p>`+
			"<w:p><w:r><w:t>\\u56fe1.1 \\u56fe\\u793a</w:t></w:r></w:p>"+
			`<w:tbl><w:tblPr/><w:tr><w:tc><w:p><w:r><w:t>cover field</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`+
			"<w:p><w:r><w:t>\\u88681.1 \\u8868\\u683c</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>1 \u7eea\u8bba</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>\u6b63\u6587\u5185\u5bb9\u3002</w:t></w:r></w:p>",
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	if strings.Contains(documentXML, "\u56fe1.1 \u56fe\u793a") || strings.Contains(documentXML, "\u88681.1 \u8868\u683c") {
		t.Fatalf("cover layout drawing/table should not receive generated captions: %s", documentXML)
	}
}

func TestFixDOCXStylesReferencesTitleAsSizeFourCentered(t *testing.T) {
	docxPath := writeCQRWSTDocx(t,
		"<w:p><w:r><w:t>1 \u7eea\u8bba</w:t></w:r></w:p>"+
			"<w:p><w:r><w:t>\u53c2\u8003\u6587\u732e</w:t></w:r></w:p>"+
			`<w:p><w:r><w:t>[1] Ref item.</w:t></w:r></w:p>`,
	)

	if _, err := FixDOCX(context.Background(), docxPath); err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}

	documentXML := readCQRWSTDocumentXML(t, docxPath)
	assertParagraphHas(t, documentXML, "\u53c2\u8003\u6587\u732e", []string{`w:sz w:val="28"`, `<w:b/>`, `w:jc w:val="center"`})
	assertParagraphHas(t, documentXML, "[1] Ref item.", []string{`w:sz w:val="21"`, `w:firstLineChars="0"`})
}
