package verify

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/renderverify"
)

func TestVerifierRenderGateAddsRepairableIssue(t *testing.T) {
	docxPath := writeMinimalDocx(t)
	ast := paperast.Snapshot{
		Version: paperast.Version,
		Source:  "word/document.xml",
		Nodes: []paperast.Node{
			{NodeID: "n1", SourcePart: "word/document.xml", NodeType: "paragraph", Text: "护理学论文题目", SemanticRole: "body_paragraph"},
			{NodeID: "n2", SourcePart: "word/document.xml", NodeType: "paragraph", Text: "摘要：正文", SemanticRole: "abstract_cn"},
		},
	}
	verifier := (&Verifier{
		closure: &ClosureArtifacts{PaperAST: ast},
	}).WithRenderGate(renderverify.Options{
		Enabled:       true,
		Strict:        true,
		Renderer:      fakeRenderGateRenderer{},
		TextExtractor: fakeRenderGateExtractor{pages: []string{"护理学论文题目", "摘要：正文"}},
	}, "")

	result, err := verifier.Verify(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Passed = true, want false")
	}
	if result.RenderResult == nil {
		t.Fatal("RenderResult = nil, want render result")
	}
	if len(result.RepairableIssues) == 0 {
		t.Fatalf("RepairableIssues = none, want render same-page issue")
	}
}

type fakeRenderGateRenderer struct{}

func (fakeRenderGateRenderer) RenderPDF(context.Context, string, string) (renderverify.PDFArtifact, error) {
	return renderverify.PDFArtifact{Path: "paper.pdf"}, nil
}

type fakeRenderGateExtractor struct {
	pages []string
}

func (e fakeRenderGateExtractor) ExtractPageTexts(string) ([]string, error) {
	return e.pages, nil
}

func writeMinimalDocx(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "paper.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	addZipFile(t, writer, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`)
	addZipFile(t, writer, "word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>护理学论文题目</w:t></w:r></w:p><w:p><w:r><w:t>摘要：正文</w:t></w:r></w:p></w:body></w:document>`)
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return path
}

func addZipFile(t *testing.T, writer *zip.Writer, name string, content string) {
	t.Helper()
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatalf("create zip entry %s: %v", name, err)
	}
	if _, err := io.WriteString(entry, content); err != nil {
		t.Fatalf("write zip entry %s: %v", name, err)
	}
}
