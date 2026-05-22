package templateprofile

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeChatClient struct {
	response string
	prompt   string
}

func (f *fakeChatClient) ChatCompletion(prompt string) (string, error) {
	f.prompt = prompt
	return f.response, nil
}

func TestExtractDetectsTemplateSectionPageBreaksHeaderFooterAndStyles(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeTemplateProfileDocx(t, templatePath)

	profile, err := Extract(templatePath)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	if profile.Version != Version {
		t.Fatalf("Version = %s, want %s", profile.Version, Version)
	}
	for _, key := range []string{"body_start", "references_title", "acknowledgements_title"} {
		rule := profile.Sections[key]
		if !rule.PageBreakBefore {
			t.Fatalf("%s PageBreakBefore = false, profile=%#v", key, profile.Sections)
		}
	}
	if !profile.Header.Exists || !profile.Header.HasDoubleLine || !strings.Contains(profile.Header.Text, "重庆人文科技学院") {
		t.Fatalf("header not extracted correctly: %#v", profile.Header)
	}
	if !profile.Footer.Exists || !profile.Footer.HasPageField || !profile.Footer.HasNumPages {
		t.Fatalf("footer not extracted correctly: %#v", profile.Footer)
	}
	refStyle := profile.Styles["references_title"]
	if refStyle.FontEastAsia != "宋体" || refStyle.FontSizeHalfPt != "28" || !refStyle.Bold {
		t.Fatalf("references title style not extracted: %#v", refStyle)
	}
}

func TestBuildAttachesDeepSeekSummary(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeTemplateProfileDocx(t, templatePath)
	client := &fakeChatClient{response: `{"sections":{"references_title":{"page_break_before":true,"evidence":"ai_confirmed"}},"styles":{"body":{"font_east_asia":"楷体","font_ascii":"Times New Roman","font_size_half_pt":"26","line":"420","first_line_chars":"200"},"references":{"font_east_asia":"宋体","font_size_half_pt":"21","first_line_chars":"0"}},"header":{"exists":true,"has_double_line":true},"confidence":0.91}`}

	profile, err := Build(context.Background(), templatePath, Options{AIEnabled: true, AIClient: client})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if profile.Source != "local+deepseek" {
		t.Fatalf("Source = %s, want local+deepseek", profile.Source)
	}
	if profile.AI == nil || profile.AI.Error != "" || profile.AI.RawJSON == nil {
		t.Fatalf("AI summary not attached: %#v", profile.AI)
	}
	if profile.Styles["body"].FontEastAsia != "楷体" || profile.Styles["body"].Line != "420" {
		t.Fatalf("AI styles should merge into profile styles: %#v", profile.Styles["body"])
	}
	if !profile.Sections["references_title"].PageBreakBefore || profile.Sections["references_title"].DetectedFrom != "ai_confirmed" {
		t.Fatalf("AI sections should merge into profile sections: %#v", profile.Sections["references_title"])
	}
	if profile.Confidence != 0.91 {
		t.Fatalf("Confidence = %v, want 0.91", profile.Confidence)
	}
	for _, want := range []string{
		"本科毕业论文 DOCX 模板格式规范解析专家",
		"章节另起页",
		"references_title",
		"acknowledgements_title",
		"页眉页脚",
		"样式画像",
		"本地解析 JSON",
	} {
		if !strings.Contains(client.prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, client.prompt)
		}
	}
}

func TestParseRoundTrip(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	writeTemplateProfileDocx(t, templatePath)
	profile, err := Extract(templatePath)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	parsed, err := Parse(Marshal(profile))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.TemplateSHA != profile.TemplateSHA {
		t.Fatalf("TemplateSHA = %s, want %s", parsed.TemplateSHA, profile.TemplateSHA)
	}
}

func writeTemplateProfileDocx(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/><Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/></Types>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:pPr><w:sectPr><w:type w:val="nextPage"/></w:sectPr></w:pPr><w:r><w:t>封面</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:rPr><w:rFonts w:eastAsia="黑体" w:ascii="Times New Roman"/><w:sz w:val="30"/><w:b/></w:rPr></w:pPr><w:r><w:t>摘要：</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:sectPr><w:type w:val="nextPage"/></w:sectPr></w:pPr><w:r><w:t>目录</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pageBreakBefore/><w:spacing w:beforeLines="100" w:afterLines="100" w:line="360"/><w:rPr><w:rFonts w:eastAsia="宋体" w:ascii="Times New Roman"/><w:sz w:val="32"/><w:b/></w:rPr></w:pPr><w:r><w:t>1 绪论</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:spacing w:line="360"/><w:ind w:firstLineChars="200"/><w:rPr><w:rFonts w:eastAsia="宋体" w:ascii="Times New Roman"/><w:sz w:val="24"/></w:rPr></w:pPr><w:r><w:t>正文。</w:t></w:r></w:p>` +
			`<w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
			`<w:p><w:pPr><w:jc w:val="center"/><w:rPr><w:rFonts w:eastAsia="宋体" w:ascii="Times New Roman"/><w:sz w:val="28"/><w:b/></w:rPr></w:pPr><w:r><w:t>参考文献</w:t></w:r></w:p>` +
			`<w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
			`<w:p><w:pPr><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="24"/></w:rPr></w:pPr><w:r><w:t>致      谢</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
		"word/header1.xml": `<?xml version="1.0" encoding="UTF-8"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:pBdr><w:bottom w:val="double"/></w:pBdr></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="18"/></w:rPr><w:t>重庆人文科技学院2026届护理学专业本科毕业论文</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml": `<?xml version="1.0" encoding="UTF-8"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>第</w:t></w:r><w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:t>页 共</w:t></w:r><w:r><w:instrText> NUMPAGES </w:instrText></w:r><w:r><w:t>页</w:t></w:r></w:p></w:ftr>`,
	}
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write entry %s: %v", name, err)
		}
	}
}
