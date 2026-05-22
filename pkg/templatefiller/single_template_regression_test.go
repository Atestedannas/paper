package templatefiller

import (
	"strings"
	"testing"
)

func TestPrepareDocumentXMLUsesSingleTemplateTargetFormatting(t *testing.T) {
	xmlIn := minimalRealTemplateXML()

	out, err := prepareDocumentXML([]byte(xmlIn))
	if err != nil {
		t.Fatalf("prepareDocumentXML() error = %v", err)
	}
	content := string(out)

	innerTitlePara := paragraphContaining(content, "{{INNER_TITLE}}")
	if !strings.Contains(innerTitlePara, `<w:rFonts w:ascii="黑体" w:eastAsia="黑体" w:hAnsi="黑体"/>`) {
		t.Fatalf("inner title paragraph font mismatch: %s", innerTitlePara)
	}
	if !strings.Contains(innerTitlePara, `<w:sz w:val="36"/>`) {
		t.Fatalf("inner title should use 小二号 (36 half-points), got: %s", innerTitlePara)
	}

	abstractTitlePara := paragraphContaining(content, "{{ABSTRACT_TITLE}}")
	if !strings.Contains(abstractTitlePara, `<w:sz w:val="32"/>`) {
		t.Fatalf("abstract title should use 三号 (32 half-points), got: %s", abstractTitlePara)
	}
	if !strings.Contains(abstractTitlePara, `w:before="120"`) || !strings.Contains(abstractTitlePara, `w:after="120"`) {
		t.Fatalf("abstract title should have 0.5-line spacing before and after, got: %s", abstractTitlePara)
	}

	enAbstractTitlePara := paragraphContaining(content, "{{EN_ABSTRACT_TITLE}}")
	if !strings.Contains(enAbstractTitlePara, `<w:rFonts w:ascii="Times New Roman" w:eastAsia="Times New Roman" w:hAnsi="Times New Roman"/>`) {
		t.Fatalf("english abstract title font mismatch: %s", enAbstractTitlePara)
	}
	if !strings.Contains(enAbstractTitlePara, `<w:sz w:val="32"/>`) {
		t.Fatalf("english abstract title should use 三号 (32 half-points), got: %s", enAbstractTitlePara)
	}
}

func TestWriteTOCPlaceholderUsesSingleRequiredTitle(t *testing.T) {
	var b strings.Builder
	writeTOCPlaceholder(&b)
	content := b.String()

	if !strings.Contains(content, `目  录`) {
		t.Fatalf("TOC title should be normalized to '目  录', got: %s", content)
	}
	if strings.Contains(content, `目　　　录`) {
		t.Fatalf("TOC title should not contain the legacy wide spacing variant: %s", content)
	}
	if !strings.Contains(content, `<w:rFonts w:ascii="黑体" w:eastAsia="黑体" w:hAnsi="黑体"/>`) {
		t.Fatalf("TOC title should use 黑体, got: %s", content)
	}
}

func TestBuildKeywordRunsNormalizesEnglishKeywords(t *testing.T) {
	runs, plain, ok := buildKeywordRuns("Key words: diabetes, self-management、quality of life", false)
	if !ok {
		t.Fatal("buildKeywordRuns() should recognize english keywords")
	}

	if plain != "Keywords: diabetes; self-management; quality of life" {
		t.Fatalf("plain = %q, want normalized english keywords", plain)
	}
	if len(runs) != 2 {
		t.Fatalf("runs len = %d, want 2", len(runs))
	}
	if runs[0].Text != "Keywords:" {
		t.Fatalf("label = %q, want %q", runs[0].Text, "Keywords:")
	}
	if runs[1].Text != " diabetes; self-management; quality of life" {
		t.Fatalf("content = %q, want normalized keyword payload", runs[1].Text)
	}
}

func TestNormalizeCoverDateRemovesInnerSpaces(t *testing.T) {
	got := normalizeCoverDate("2026年 3月")
	if got != "2026年3月" {
		t.Fatalf("normalizeCoverDate() = %q, want %q", got, "2026年3月")
	}
}

func minimalRealTemplateXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>
<w:p><w:r><w:t>本科毕业论文</w:t></w:r></w:p>
<w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr>
<w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr>
<w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr>
<w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr>
</w:body>
</w:document>`
}

func paragraphContaining(content, marker string) string {
	idx := strings.Index(content, marker)
	if idx < 0 {
		return ""
	}
	start := strings.LastIndex(content[:idx], "<w:p")
	end := strings.Index(content[idx:], "</w:p>")
	if start < 0 || end < 0 {
		return ""
	}
	end += idx + len("</w:p>")
	return content[start:end]
}
