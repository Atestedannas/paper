package fileprocessor

import (
	"context"
	"fmt"

	"gitee.com/greatmusicians/unioffice/document"
)

type TemplateBaseFormatter struct{}

func NewTemplateBaseFormatter() *TemplateBaseFormatter {
	return &TemplateBaseFormatter{}
}

func (f *TemplateBaseFormatter) Format(ctx context.Context, cfg SingleTemplateFormatConfig) (string, error) {
	_ = f
	_ = ctx

	userPkg, err := openDocxPackage(cfg.UserPaperPath)
	if err != nil {
		return "", fmt.Errorf("open user package: %w", err)
	}
	templatePkg, err := openDocxPackage(cfg.TemplatePath)
	if err != nil {
		return "", fmt.Errorf("open template package: %w", err)
	}
	outputPkg := templatePkg.Clone()
	if outputPkg == nil {
		return "", fmt.Errorf("clone template package")
	}

	templateBlocks, err := findTemplateBlocks(templatePkg)
	if err != nil {
		return "", fmt.Errorf("find template blocks: %w", err)
	}

	userBlocks, err := findTemplateBlocks(userPkg)
	if err != nil {
		return "", fmt.Errorf("find user blocks: %w", err)
	}

	payload, err := extractUserPayload(userPkg, userBlocks)
	if err != nil {
		return "", fmt.Errorf("extract user payload: %w", err)
	}
	if err := normalizeTemplatePayloadForTemplate(templatePkg, payload); err != nil {
		return "", fmt.Errorf("normalize template payload: %w", err)
	}
	if err := validateTemplatePayloadFits(templatePkg, userPkg, templateBlocks, userBlocks, payload); err != nil {
		return "", fmt.Errorf("validate template payload: %w", err)
	}

	templateDoc, err := document.Open(cfg.TemplatePath)
	if err != nil {
		return "", fmt.Errorf("open template for transplant rules: %w", err)
	}
	transplantRules := extractStrictTemplateBlockRules(templateDoc, NewEnhancedProcessor())
	templateDoc.Close()

	if err := transplantUserPayload(outputPkg, templateBlocks, payload, transplantRules); err != nil {
		return "", fmt.Errorf("transplant user payload: %w", err)
	}
	if err := normalizeTemplateCloneTypography(cfg.TemplatePath, outputPkg); err != nil {
		return "", fmt.Errorf("normalize template clone typography: %w", err)
	}
	if err := validateTransplantedTemplate(templatePkg, outputPkg, templateBlocks); err != nil {
		return "", fmt.Errorf("validate transplanted template: %w", err)
	}

	if err := outputPkg.WriteTo(cfg.OutputPath); err != nil {
		return "", fmt.Errorf("write template package: %w", err)
	}

	return cfg.OutputPath, nil
}
