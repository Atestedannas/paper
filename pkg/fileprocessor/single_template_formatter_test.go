package fileprocessor

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gitee.com/greatmusicians/unioffice/document"
)

func TestFormatSingleTemplateDocumentUsesTemplateBaseFormatterAndReturnsOutputPath(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := writeTinyDocxFixture(t, tmpDir, "user.docx", map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>User</w:t></w:r></w:p></w:body></w:document>`,
	})
	templatePath := writeTinyDocxFixture(t, tmpDir, "template.docx", map[string]string{
		"[Content_Types].xml":      `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":        `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Template</w:t></w:r></w:p></w:body></w:document>`,
		"word/customXml/item1.xml": `<custom>template-only</custom>`,
	})
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	calls := 0
	restoreBase := swapSingleTemplateBaseFormatter(func(_ context.Context, cfg SingleTemplateFormatConfig) (string, error) {
		calls++
		return cfg.OutputPath, nil
	})
	defer restoreBase()

	restorePost := swapSingleTemplatePostProcessor(func(_ context.Context, _ SingleTemplateFormatConfig, localOutputPath string) (string, error) {
		return localOutputPath, nil
	})
	defer restorePost()

	got, err := FormatSingleTemplateDocument(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	})
	if err != nil {
		t.Fatalf("FormatSingleTemplateDocument() error = %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(outputPath) && filepath.ToSlash(got) != filepath.ToSlash(outputPath) {
		t.Fatalf("output path = %q, want %q", got, outputPath)
	}
	if calls != 1 {
		t.Fatalf("base formatter calls = %d, want 1", calls)
	}
}

func TestFormatSingleTemplateDocumentReturnsTemplateBaseFormatterError(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := writeTinyDocxFixture(t, tmpDir, "user.docx", map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>User</w:t></w:r></w:p></w:body></w:document>`,
	})
	templatePath := writeTinyDocxFixture(t, tmpDir, "template.docx", map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Template</w:t></w:r></w:p></w:body></w:document>`,
	})
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	restoreBase := swapSingleTemplateBaseFormatter(func(_ context.Context, _ SingleTemplateFormatConfig) (string, error) {
		return "", errors.New("template base failed")
	})
	defer restoreBase()

	restorePost := swapSingleTemplatePostProcessor(func(_ context.Context, _ SingleTemplateFormatConfig, localOutputPath string) (string, error) {
		return localOutputPath, nil
	})
	defer restorePost()

	_, err := FormatSingleTemplateDocument(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	})
	if err == nil {
		t.Fatalf("expected template base formatter error")
	}
}

func TestDefaultSingleTemplateBaseFormatterFallsBackToStrictFormatter(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := SingleTemplateFormatConfig{
		UserPaperPath: filepath.Join(tmpDir, "user.docx"),
		TemplatePath:  filepath.Join(tmpDir, "template.docx"),
		OutputPath:    filepath.Join(tmpDir, "formatted.docx"),
	}

	var calls []string
	restoreTemplateClone := swapSingleTemplateTemplateCloneFormatter(func(_ context.Context, _ SingleTemplateFormatConfig) (string, error) {
		calls = append(calls, "template-clone")
		return "", errors.New("missing template block")
	})
	defer restoreTemplateClone()

	restoreStrict := swapSingleTemplateStrictFallbackFormatter(func(_ context.Context, cfg SingleTemplateFormatConfig) (string, error) {
		calls = append(calls, "strict")
		return cfg.OutputPath, nil
	})
	defer restoreStrict()

	got, err := defaultSingleTemplateBaseFormatter(context.Background(), cfg)
	if err != nil {
		t.Fatalf("defaultSingleTemplateBaseFormatter() error = %v", err)
	}
	if got != cfg.OutputPath {
		t.Fatalf("defaultSingleTemplateBaseFormatter() = %q, want %q", got, cfg.OutputPath)
	}
	if want := []string{"template-clone", "strict"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("formatter calls = %#v, want %#v", calls, want)
	}
}

func TestFormatSingleTemplateDocumentDefaultRunnerDoesNotInjectTemplateInstructionText(t *testing.T) {
	tmpDir := t.TempDir()
	userPath := filepath.Join(tmpDir, "user.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	outputPath := filepath.Join(tmpDir, "formatted.docx")

	writeTestDocx(t, userPath, func(doc *document.Document) {
		doc.AddParagraph().AddRun().AddText("社区2型糖尿病患者疾病知识认知现状及影响因素分析")
		doc.AddParagraph().AddRun().AddText("摘要：用户自己的摘要内容")
		doc.AddParagraph().AddRun().AddText("正文第一段")
	})

	writeTestDocx(t, templatePath, func(doc *document.Document) {
		doc.AddParagraph().AddRun().AddText("封面格式不要调整，直接填写相关内容即可")
		doc.AddParagraph().AddRun().AddText("本科毕业论文/设计")
		doc.AddParagraph().AddRun().AddText("模板标题")
		doc.AddParagraph().AddRun().AddText("摘要")
	})

	got, err := FormatSingleTemplateDocument(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	})
	if err != nil {
		t.Fatalf("FormatSingleTemplateDocument() error = %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(outputPath) && filepath.ToSlash(got) != filepath.ToSlash(outputPath) {
		t.Fatalf("output path = %q, want %q", got, outputPath)
	}

	docXML := readDocxEntry(t, outputPath, "word/document.xml")
	if strings.Contains(docXML, "封面格式不要调整") {
		t.Fatalf("output should not include template instruction text: %s", docXML)
	}
	if strings.Contains(docXML, "模板标题") {
		t.Fatalf("output should not inject template-only title text: %s", docXML)
	}
	if !strings.Contains(docXML, "社区2型糖尿病患者疾病知识认知现状及影响因素分析") {
		t.Fatalf("output should preserve user title text: %s", docXML)
	}
	if !strings.Contains(docXML, "摘要：用户自己的摘要内容") {
		t.Fatalf("output should preserve user abstract text: %s", docXML)
	}
}

func swapSingleTemplateBaseFormatter(runner func(context.Context, SingleTemplateFormatConfig) (string, error)) func() {
	oldRunner := runSingleTemplateBaseFormatter
	runSingleTemplateBaseFormatter = runner
	return func() {
		runSingleTemplateBaseFormatter = oldRunner
	}
}

func swapSingleTemplateTemplateCloneFormatter(runner func(context.Context, SingleTemplateFormatConfig) (string, error)) func() {
	oldRunner := runSingleTemplateTemplateCloneFormatter
	runSingleTemplateTemplateCloneFormatter = runner
	return func() {
		runSingleTemplateTemplateCloneFormatter = oldRunner
	}
}

func swapSingleTemplateStrictFallbackFormatter(runner func(context.Context, SingleTemplateFormatConfig) (string, error)) func() {
	oldRunner := runSingleTemplateStrictFallbackFormatter
	runSingleTemplateStrictFallbackFormatter = runner
	return func() {
		runSingleTemplateStrictFallbackFormatter = oldRunner
	}
}

func swapSingleTemplatePostProcessor(runner func(context.Context, SingleTemplateFormatConfig, string) (string, error)) func() {
	oldRunner := runSingleTemplatePostProcessor
	runSingleTemplatePostProcessor = runner
	return func() {
		runSingleTemplatePostProcessor = oldRunner
	}
}
