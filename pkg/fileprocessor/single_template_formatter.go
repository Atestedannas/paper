package fileprocessor

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"path/filepath"

	wzdocument "github.com/nineya/wordZero/pkg/document"
)

// SingleTemplateFormatConfig defines the fixed input/output paths used by the
// dedicated single-template formatter CLI.
type SingleTemplateFormatConfig struct {
	UserPaperPath string
	TemplatePath  string
	OutputPath    string
}

var runSingleTemplateTemplateCloneFormatter = func(ctx context.Context, cfg SingleTemplateFormatConfig) (string, error) {
	return NewTemplateBaseFormatter().Format(ctx, cfg)
}

var runSingleTemplateStrictFallbackFormatter = func(ctx context.Context, cfg SingleTemplateFormatConfig) (string, error) {
	return NewStrictTemplateFormatter().Format(ctx, cfg)
}

var runSingleTemplateBaseFormatter = func(ctx context.Context, cfg SingleTemplateFormatConfig) (string, error) {
	return defaultSingleTemplateBaseFormatter(ctx, cfg)
}

var runSingleTemplatePostProcessor = func(ctx context.Context, cfg SingleTemplateFormatConfig, localOutputPath string) (string, error) {
	postProcessor := NewLocalWordPostProcessor()
	return postProcessor.Finalize(ctx, cfg, localOutputPath)
}

func defaultSingleTemplateBaseFormatter(ctx context.Context, cfg SingleTemplateFormatConfig) (string, error) {
	outputPath, err := runSingleTemplateTemplateCloneFormatter(ctx, cfg)
	if err == nil && docxXMLPartsAreValid(outputPath) == nil {
		return outputPath, nil
	}

	if err == nil {
		err = docxXMLPartsAreValid(outputPath)
	}
	log.Printf("[single-template] template clone formatter failed, falling back to strict formatter: %v", err)
	return runSingleTemplateStrictFallbackFormatter(ctx, cfg)
}

func docxXMLPartsAreValid(path string) error {
	pkg, err := openDocxPackage(path)
	if err != nil {
		return err
	}
	for name, content := range pkg.entries {
		if filepath.Ext(name) != ".xml" {
			continue
		}
		if err := xml.Unmarshal(content, new(any)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// FormatSingleTemplateDocument routes through the template-base formatter first,
// then applies local template-aware post-processing to the generated docx.
func FormatSingleTemplateDocument(ctx context.Context, config SingleTemplateFormatConfig) (string, error) {
	if err := validateSingleTemplateConfig(config); err != nil {
		return "", err
	}

	if err := probeSingleTemplateInputsWithWordZero(config.UserPaperPath, config.TemplatePath); err != nil {
		log.Printf("[single-template] wordZero preflight warning: %v", err)
	}

	outputPath, err := runSingleTemplateBaseFormatter(ctx, config)
	if err != nil {
		return "", fmt.Errorf("single-template formatting failed: %w", err)
	}

	finalOutputPath, err := runSingleTemplatePostProcessor(ctx, config, outputPath)
	if err != nil {
		return "", fmt.Errorf("single-template local post-process failed: %w", err)
	}
	return finalOutputPath, nil
}

func validateSingleTemplateConfig(config SingleTemplateFormatConfig) error {
	switch {
	case config.UserPaperPath == "":
		return fmt.Errorf("user paper path cannot be empty")
	case config.TemplatePath == "":
		return fmt.Errorf("template path cannot be empty")
	case config.OutputPath == "":
		return fmt.Errorf("output path cannot be empty")
	}

	for _, path := range []string{config.UserPaperPath, config.TemplatePath} {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("file not found or inaccessible %q: %w", path, err)
		}
		if info.IsDir() {
			return fmt.Errorf("path %q must be a file", path)
		}
	}

	if filepath.Clean(config.UserPaperPath) == filepath.Clean(config.OutputPath) {
		return fmt.Errorf("output path cannot be the same as input path")
	}
	if err := os.MkdirAll(filepath.Dir(config.OutputPath), 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	return nil
}

func probeSingleTemplateInputsWithWordZero(userPaperPath, templatePath string) error {
	userDoc, err := wzdocument.Open(userPaperPath)
	if err != nil {
		return fmt.Errorf("wordZero open user paper: %w", err)
	}
	if userDoc.GetPageSettings() == nil {
		return fmt.Errorf("wordZero could not read user paper page settings")
	}

	templateDoc, err := wzdocument.Open(templatePath)
	if err != nil {
		return fmt.Errorf("wordZero open template: %w", err)
	}
	if templateDoc.GetPageSettings() == nil {
		return fmt.Errorf("wordZero could not read template page settings")
	}
	return nil
}

func moveOrCopySingleTemplateOutput(srcPath, dstPath string) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	_ = os.Remove(dstPath)
	if err := os.Rename(srcPath, dstPath); err == nil {
		return nil
	}

	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read generated output: %w", err)
	}
	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		return fmt.Errorf("write final output: %w", err)
	}
	_ = os.Remove(srcPath)
	return nil
}
