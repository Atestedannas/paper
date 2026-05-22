package templatefiller

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareRealTemplateRegeneratesInvalidSiblingPreparedTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	rawTemplatePath := filepath.Join(tmpDir, "template.docx")
	invalidPreparedPath := filepath.Join(tmpDir, "template_prepared.docx")
	outputPath := filepath.Join(tmpDir, "out.docx")

	writeTemplateDocxFixture(t, rawTemplatePath, realTemplateDocumentXML(), map[string]string{
		"word/header1.xml":               `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>重庆人文科技学院</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml":               `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>第 1 页</w:t></w:r></w:p></w:ftr>`,
		"word/_rels/document.xml.rels":   `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/><Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/></Relationships>`,
	})
	writeTemplateDocxFixture(t, invalidPreparedPath, invalidPreparedDocumentXML(), nil)

	if err := PrepareRealTemplate(rawTemplatePath, outputPath); err != nil {
		t.Fatalf("PrepareRealTemplate() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Count(docXML, "<w:tbl") < 2 {
		t.Fatalf("prepared template should preserve cover tables, got document.xml = %s", docXML)
	}
	for _, marker := range []string{"{{COVER_TITLE}}", "{{COVER_COLLEGE}}", "{{COVER_DATE}}", "{{BODY}}"} {
		if !strings.Contains(docXML, marker) {
			t.Fatalf("prepared template missing marker %s", marker)
		}
	}
	if !docxEntryExists(t, outputPath, "word/header1.xml") {
		t.Fatal("prepared template should preserve header part from raw template")
	}
	if !docxEntryExists(t, outputPath, "word/footer1.xml") {
		t.Fatal("prepared template should preserve footer part from raw template")
	}
}

func TestPrepareRealTemplateRemovesInstructionShapesAndKeepsTOCField(t *testing.T) {
	tmpDir := t.TempDir()
	rawTemplatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "prepared.docx")

	writeTemplateDocxFixture(t, rawTemplatePath, realTemplateDocumentXML(), map[string]string{
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/><Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/></Relationships>`,
	})

	if err := PrepareRealTemplate(rawTemplatePath, outputPath); err != nil {
		t.Fatalf("PrepareRealTemplate() error = %v", err)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Contains(docXML, "<w:pict") {
		t.Fatalf("prepared template should strip instructional pict shapes, got %s", docXML)
	}
	if strings.Contains(docXML, `w:color w:val="FF0000"`) {
		t.Fatalf("prepared template should not keep instructional red sample text, got %s", docXML)
	}
	if !strings.Contains(docXML, `TOC \o "1-3" \h \z \u`) {
		t.Fatalf("prepared template should preserve a TOC field, got %s", docXML)
	}
	if !strings.Contains(docXML, "{{REFERENCES_CONTENT}}") {
		t.Fatalf("prepared template should still contain downstream placeholders, got %s", docXML)
	}
}

func writeTemplateDocxFixture(t *testing.T, path string, documentXML string, extraEntries map[string]string) {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rPkg" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": documentXML,
	}
	for name, content := range extraEntries {
		entries[name] = content
	}

	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func readDocxEntry(t *testing.T, path, name string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read docx: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", name, err)
		}
		defer rc.Close()
		content, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read entry %s: %v", name, err)
		}
		return string(content)
	}
	t.Fatalf("missing zip entry %s", name)
	return ""
}

func docxEntryExists(t *testing.T, path, name string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read docx: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	for _, file := range reader.File {
		if file.Name == name {
			return true
		}
	}
	return false
}

func realTemplateDocumentXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:v="urn:schemas-microsoft-com:vml" xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r>
        <w:pict><v:shape id="shape1"><v:textbox><w:txbxContent><w:p><w:r><w:t>封面格式不要调整</w:t></w:r></w:p></w:txbxContent></v:textbox></v:shape></w:pict>
      </w:r>
      <w:r><w:t>本科毕业论文/设计</w:t></w:r>
    </w:p>
    <w:tbl>
      <w:tr>
        <w:tc><w:p><w:r><w:t>题目</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>XXXXXXXXXXXXXXXXXXXXXXXX</w:t></w:r></w:p></w:tc>
      </w:tr>
      <w:tr>
        <w:tc><w:p/></w:tc>
        <w:tc><w:p><w:r><w:t>——以社区护理为例</w:t></w:r></w:p></w:tc>
      </w:tr>
    </w:tbl>
    <w:tbl>
      <w:tr><w:tc><w:p><w:r><w:t>学院</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:rPr><w:color w:val="FF0000"/></w:rPr><w:t>护理学院</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>专业</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:rPr><w:color w:val="FF0000"/></w:rPr><w:t>护理学</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>班级</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>2022级护理学5班</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>学号</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>20220152192</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>姓名</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>冉怡琴</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>指导教师</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>杨严政</w:t></w:r></w:p></w:tc></w:tr>
    </w:tbl>
    <w:p><w:r><w:t xml:space="preserve"> 2026年 3 月</w:t></w:r></w:p>
    <w:p><w:pPr><w:sectPr><w:headerReference r:id="rId3" w:type="default"/><w:footerReference r:id="rId4" w:type="default"/><w:pgSz w:w="11906" w:h="16838"/></w:sectPr></w:pPr></w:p>
    <w:p><w:r><w:t>摘要：</w:t></w:r></w:p>
    <w:p><w:r><w:t>Abstract:</w:t></w:r></w:p>
    <w:p><w:r><w:t>目  录</w:t></w:r></w:p>
    <w:p>
      <w:r><w:fldChar w:fldCharType="begin"/></w:r>
      <w:r><w:instrText xml:space="preserve"> TOC \o "1-3" \h \z \u </w:instrText></w:r>
      <w:r><w:fldChar w:fldCharType="separate"/></w:r>
      <w:r><w:t>目录将在打开文档后自动更新</w:t></w:r>
      <w:r><w:fldChar w:fldCharType="end"/></w:r>
    </w:p>
    <w:p><w:r><w:t>参考文献</w:t></w:r></w:p>
    <w:p><w:r><w:t>致谢</w:t></w:r></w:p>
    <w:sectPr><w:headerReference r:id="rId3" w:type="default"/><w:footerReference r:id="rId4" w:type="default"/><w:pgSz w:w="11906" w:h="16838"/></w:sectPr>
  </w:body>
</w:document>`
}

func invalidPreparedDocumentXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>{{BODY}}</w:t></w:r></w:p>
  </w:body>
</w:document>`
}
