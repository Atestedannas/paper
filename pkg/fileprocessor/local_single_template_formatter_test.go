package fileprocessor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFormatSingleTemplateDocumentRunsLocalPostProcessorAfterBaseFormatter(t *testing.T) {
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
	cfg := SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	}

	baseCalls := 0
	postCalls := 0
	restoreBase := swapSingleTemplateBaseFormatter(func(_ context.Context, got SingleTemplateFormatConfig) (string, error) {
		baseCalls++
		if got != cfg {
			t.Fatalf("base formatter config = %+v, want %+v", got, cfg)
		}
		if err := os.WriteFile(got.OutputPath, []byte("base-output"), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		return got.OutputPath, nil
	})
	defer restoreBase()

	restorePost := swapSingleTemplatePostProcessor(func(_ context.Context, got SingleTemplateFormatConfig, localOutputPath string) (string, error) {
		postCalls++
		if got != cfg {
			t.Fatalf("post processor config = %+v, want %+v", got, cfg)
		}
		if localOutputPath != outputPath {
			t.Fatalf("post processor localOutputPath = %q, want %q", localOutputPath, outputPath)
		}
		return localOutputPath, nil
	})
	defer restorePost()

	got, err := FormatSingleTemplateDocument(context.Background(), cfg)
	if err != nil {
		t.Fatalf("FormatSingleTemplateDocument() error = %v", err)
	}
	if got != outputPath {
		t.Fatalf("FormatSingleTemplateDocument() = %q, want %q", got, outputPath)
	}
	if baseCalls != 1 {
		t.Fatalf("base formatter calls = %d, want 1", baseCalls)
	}
	if postCalls != 1 {
		t.Fatalf("post processor calls = %d, want 1", postCalls)
	}
}

func TestFormatSingleTemplateDocumentReturnsLocalPostProcessorError(t *testing.T) {
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

	restoreBase := swapSingleTemplateBaseFormatter(func(_ context.Context, got SingleTemplateFormatConfig) (string, error) {
		if err := os.WriteFile(got.OutputPath, []byte("base-output"), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		return got.OutputPath, nil
	})
	defer restoreBase()

	restorePost := swapSingleTemplatePostProcessor(func(_ context.Context, _ SingleTemplateFormatConfig, _ string) (string, error) {
		return "", errors.New("local post-process failed")
	})
	defer restorePost()

	_, err := FormatSingleTemplateDocument(context.Background(), SingleTemplateFormatConfig{
		UserPaperPath: userPath,
		TemplatePath:  templatePath,
		OutputPath:    outputPath,
	})
	if err == nil {
		t.Fatalf("expected post processor error")
	}
}

func TestLocalWordPostProcessorFinalizeRunsLocalStepsInOrder(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "formatted.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	if err := os.WriteFile(outputPath, []byte("formatted"), 0644); err != nil {
		t.Fatalf("WriteFile(output) error = %v", err)
	}
	if err := os.WriteFile(templatePath, []byte("template"), 0644); err != nil {
		t.Fatalf("WriteFile(template) error = %v", err)
	}

	var operations []string
	postProcessor := &LocalWordPostProcessor{
		ApplyPageSetup: func(studentPath, gotTemplatePath, tempOutputPath string) error {
			operations = append(operations, "page-setup")
			if studentPath != outputPath {
				t.Fatalf("ApplyPageSetup studentPath = %q, want %q", studentPath, outputPath)
			}
			if gotTemplatePath != templatePath {
				t.Fatalf("ApplyPageSetup templatePath = %q, want %q", gotTemplatePath, templatePath)
			}
			return os.WriteFile(tempOutputPath, []byte("page-setup"), 0644)
		},
		RestoreCoverTables: func(gotTemplatePath, gotOutputPath string) error {
			operations = append(operations, "restore-cover")
			if gotTemplatePath != templatePath || gotOutputPath != outputPath {
				t.Fatalf("RestoreCoverTables args = (%q, %q)", gotTemplatePath, gotOutputPath)
			}
			return nil
		},
		CopyHeaderFooter: func(gotTemplatePath, gotOutputPath string) error {
			operations = append(operations, "copy-header-footer")
			if gotTemplatePath != templatePath || gotOutputPath != outputPath {
				t.Fatalf("CopyHeaderFooter args = (%q, %q)", gotTemplatePath, gotOutputPath)
			}
			return nil
		},
		SanitizeOutputPackage: func(gotOutputPath string) error {
			operations = append(operations, "sanitize")
			if gotOutputPath != outputPath {
				t.Fatalf("SanitizeOutputPackage outputPath = %q, want %q", gotOutputPath, outputPath)
			}
			return nil
		},
	}

	got, err := postProcessor.Finalize(context.Background(), SingleTemplateFormatConfig{
		TemplatePath: templatePath,
		OutputPath:   outputPath,
	}, outputPath)
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if got != filepath.ToSlash(outputPath) {
		t.Fatalf("Finalize() = %q, want %q", got, filepath.ToSlash(outputPath))
	}

	if want := []string{"page-setup", "restore-cover", "copy-header-footer", "sanitize"}; !reflect.DeepEqual(operations, want) {
		t.Fatalf("operations = %#v, want %#v", operations, want)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(output) error = %v", err)
	}
	if string(data) != "page-setup" {
		t.Fatalf("output contents = %q, want page-setup result", string(data))
	}
}

func TestLocalWordPostProcessorFinalizeContinuesWhenPageSetupFails(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "formatted.docx")
	templatePath := filepath.Join(tmpDir, "template.docx")
	if err := os.WriteFile(outputPath, []byte("formatted"), 0644); err != nil {
		t.Fatalf("WriteFile(output) error = %v", err)
	}
	if err := os.WriteFile(templatePath, []byte("template"), 0644); err != nil {
		t.Fatalf("WriteFile(template) error = %v", err)
	}

	var operations []string
	postProcessor := &LocalWordPostProcessor{
		ApplyPageSetup: func(studentPath, gotTemplatePath, tempOutputPath string) error {
			operations = append(operations, "page-setup")
			if studentPath != outputPath || gotTemplatePath != templatePath {
				t.Fatalf("ApplyPageSetup args = (%q, %q)", studentPath, gotTemplatePath)
			}
			return errors.New("unsupported body xml")
		},
		RestoreCoverTables: func(gotTemplatePath, gotOutputPath string) error {
			operations = append(operations, "restore-cover")
			if gotTemplatePath != templatePath || gotOutputPath != outputPath {
				t.Fatalf("RestoreCoverTables args = (%q, %q)", gotTemplatePath, gotOutputPath)
			}
			return nil
		},
		CopyHeaderFooter: func(gotTemplatePath, gotOutputPath string) error {
			operations = append(operations, "copy-header-footer")
			if gotTemplatePath != templatePath || gotOutputPath != outputPath {
				t.Fatalf("CopyHeaderFooter args = (%q, %q)", gotTemplatePath, gotOutputPath)
			}
			return nil
		},
		SanitizeOutputPackage: func(gotOutputPath string) error {
			operations = append(operations, "sanitize")
			if gotOutputPath != outputPath {
				t.Fatalf("SanitizeOutputPackage outputPath = %q, want %q", gotOutputPath, outputPath)
			}
			return nil
		},
	}

	got, err := postProcessor.Finalize(context.Background(), SingleTemplateFormatConfig{
		TemplatePath: templatePath,
		OutputPath:   outputPath,
	}, outputPath)
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if got != filepath.ToSlash(outputPath) {
		t.Fatalf("Finalize() = %q, want %q", got, filepath.ToSlash(outputPath))
	}

	if want := []string{"page-setup", "restore-cover", "copy-header-footer", "sanitize"}; !reflect.DeepEqual(operations, want) {
		t.Fatalf("operations = %#v, want %#v", operations, want)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(output) error = %v", err)
	}
	if string(data) != "formatted" {
		t.Fatalf("output contents = %q, want original output to remain", string(data))
	}
}
