package fileprocessor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type LocalWordPostProcessor struct {
	ApplyPageSetup        func(studentPath, templatePath, outputPath string) error
	CopyHeaderFooter      func(templatePath, outputPath string) error
	RestoreCoverTables    func(templatePath, outputPath string) error
	SanitizeOutputPackage func(outputPath string) error
}

func NewLocalWordPostProcessor() *LocalWordPostProcessor {
	return &LocalWordPostProcessor{
		ApplyPageSetup:        nil,
		CopyHeaderFooter:      copyTemplateHeaderFooterPackage,
		RestoreCoverTables:    restoreStrictCoverTablesFromTemplate,
		SanitizeOutputPackage: postProcessStrictOutput,
	}
}

func (p *LocalWordPostProcessor) Finalize(ctx context.Context, cfg SingleTemplateFormatConfig, localOutputPath string) (string, error) {
	if p == nil {
		return localOutputPath, nil
	}
	if strings.TrimSpace(cfg.TemplatePath) == "" || strings.TrimSpace(localOutputPath) == "" {
		return localOutputPath, nil
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	tempOutputPath := localOutputPath + ".pagesetup.docx"
	_ = os.Remove(tempOutputPath)

	if p.ApplyPageSetup != nil {
		if err := p.ApplyPageSetup(localOutputPath, cfg.TemplatePath, tempOutputPath); err != nil {
			log.Printf("[single-template] skip local page-setup copy for %s: %v", localOutputPath, err)
		} else {
			if err := moveOrCopySingleTemplateOutput(tempOutputPath, localOutputPath); err != nil {
				return "", fmt.Errorf("persist page setup output: %w", err)
			}
		}
	}

	if p.RestoreCoverTables != nil {
		if err := p.RestoreCoverTables(cfg.TemplatePath, localOutputPath); err != nil {
			return "", fmt.Errorf("restore cover tables: %w", err)
		}
	}

	if p.CopyHeaderFooter != nil {
		if err := p.CopyHeaderFooter(cfg.TemplatePath, localOutputPath); err != nil {
			return "", fmt.Errorf("copy header/footer package: %w", err)
		}
	}

	if p.SanitizeOutputPackage != nil {
		if err := p.SanitizeOutputPackage(localOutputPath); err != nil {
			return "", fmt.Errorf("sanitize output package: %w", err)
		}
	}

	return filepath.ToSlash(localOutputPath), nil
}
