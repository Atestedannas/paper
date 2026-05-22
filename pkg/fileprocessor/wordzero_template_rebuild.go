package fileprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	wzdocument "github.com/nineya/wordZero/pkg/document"
	wzstyle "github.com/nineya/wordZero/pkg/style"
	"github.com/paper-format-checker/backend/pkg/docconvert"
	"github.com/paper-format-checker/backend/pkg/templatefiller"
)

// TemplateDrivenRebuildEnabled returns true unless explicitly disabled.
func TemplateDrivenRebuildEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("USE_TEMPLATE_DRIVEN_REBUILD")))
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

type wordZeroTemplateProfile struct {
	AllStyles       int
	ParagraphStyles int
	CharacterStyles int
	HeadingStyles   int
	HasPageSettings bool
}

func ensurePreparedTemplate(templatePath string) (string, error) {
	if strings.TrimSpace(templatePath) == "" {
		return "", fmt.Errorf("empty template path")
	}
	if strings.HasSuffix(strings.ToLower(templatePath), "_prepared.docx") {
		return templatePath, nil
	}

	preparedPath := strings.TrimSuffix(templatePath, filepath.Ext(templatePath)) + "_prepared.docx"
	if err := templatefiller.PrepareRealTemplate(templatePath, preparedPath); err != nil {
		return "", fmt.Errorf("prepare template: %w", err)
	}
	return preparedPath, nil
}

func inspectTemplateWithWordZero(templatePath string) (*wordZeroTemplateProfile, error) {
	doc, err := wzdocument.Open(templatePath)
	if err != nil {
		return nil, fmt.Errorf("wordzero open template: %w", err)
	}

	styleManager := doc.GetStyleManager()
	if styleManager == nil {
		return nil, fmt.Errorf("wordzero template style manager is nil")
	}

	quickAPI := wzstyle.NewQuickStyleAPI(styleManager)
	profile := &wordZeroTemplateProfile{
		AllStyles:       len(quickAPI.GetAllStylesInfo()),
		ParagraphStyles: len(quickAPI.GetParagraphStylesInfo()),
		CharacterStyles: len(quickAPI.GetCharacterStylesInfo()),
		HeadingStyles:   len(quickAPI.GetHeadingStylesInfo()),
		HasPageSettings: doc.GetPageSettings() != nil,
	}
	return profile, nil
}

func templateDrivenOutputPath(docPath string) string {
	dir := filepath.Dir(docPath)
	base := strings.TrimSuffix(filepath.Base(docPath), filepath.Ext(docPath))
	outDir := filepath.Join(dir, "corrected")
	ext := strings.ToLower(filepath.Ext(docPath))
	if ext != ".docx" {
		ext = ".docx"
	}
	return filepath.Join(outDir, base+"_styled"+ext)
}

func TemplateDrivenRebuildUNOEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("TEMPLATE_REBUILD_UNO")))
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func (p *EnhancedProcessor) runTemplateDrivenUNO(
	ctx context.Context,
	outputPath string,
	templatePath string,
) error {
	if !TemplateDrivenRebuildUNOEnabled() {
		return nil
	}
	cfg := DefaultStyleFormatterConfig()
	cfg.UnoEnabled = true
	if cfg.UnoScriptPath == "" {
		return nil
	}
	if mode := strings.TrimSpace(os.Getenv("TEMPLATE_REBUILD_UNO_MODE")); mode != "" {
		cfg.UnoMode = mode
	}
	if cfg.UnoMode == "" {
		cfg.UnoMode = "apply_styles"
	}

	classificationPath := ""
	tmpFile := (*os.File)(nil)
	if cfg.UnoMode == "apply_styles" {
		cls, err := p.classifyDocumentForStyleFormatter(outputPath)
		if err != nil {
			return fmt.Errorf("uno re-classify output: %w", err)
		}
		clsJSON, err := json.Marshal(cls)
		if err != nil {
			return fmt.Errorf("uno marshal classification: %w", err)
		}
		tmpFile, err = os.CreateTemp("", fmt.Sprintf("template_rebuild_uno_%d_*.json", time.Now().UnixMilli()))
		if err != nil {
			return fmt.Errorf("uno temp classification: %w", err)
		}
		if _, err := tmpFile.Write(clsJSON); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			return fmt.Errorf("uno write classification: %w", err)
		}
		classificationPath = tmpFile.Name()
		tmpFile.Close()
		defer os.Remove(classificationPath)
	}

	return p.runUnoFormatter(ctx, outputPath, templatePath, classificationPath, cfg)
}

// RunTemplateDrivenRebuild rebuilds the paper from the school template instead of
// repeatedly patching the student draft in place. WordZero is used to inspect the
// uploaded template's style system before the OOXML filler renders the final docx.
func (p *EnhancedProcessor) RunTemplateDrivenRebuild(
	ctx context.Context,
	docPath string,
	templatePath string,
	corrections []map[string]interface{},
) (string, error) {
	if strings.TrimSpace(templatePath) == "" {
		return "", fmt.Errorf("template-driven rebuild: empty template path")
	}

	preparedTemplatePath, err := ensurePreparedTemplate(templatePath)
	if err != nil {
		return "", fmt.Errorf("template-driven rebuild prepare template: %w", err)
	}

	profile, err := inspectTemplateWithWordZero(preparedTemplatePath)
	if err != nil {
		return "", err
	}
	log.Printf("[TemplateRebuild] template inspected by WordZero: all=%d paragraph=%d character=%d heading=%d pageSettings=%v",
		profile.AllStyles, profile.ParagraphStyles, profile.CharacterStyles, profile.HeadingStyles, profile.HasPageSettings)
	if filepath.Clean(preparedTemplatePath) != filepath.Clean(templatePath) {
		log.Printf("[TemplateRebuild] using prepared template: %s", preparedTemplatePath)
	}

	studentPath := docPath
	if strings.EqualFold(filepath.Ext(docPath), ".doc") {
		converted, convErr := docconvert.ConvertDocToDocx(docPath, false)
		if convErr != nil {
			return "", fmt.Errorf("template-driven rebuild: convert .doc to .docx: %w", convErr)
		}
		studentPath = converted
		log.Printf("[TemplateRebuild] student converted to docx: %s", studentPath)
	}

	classification, err := p.classifyDocumentForStyleFormatter(studentPath)
	if err != nil {
		return "", fmt.Errorf("template-driven rebuild classification: %w", err)
	}
	log.Printf("[TemplateRebuild] classified %d body-level paragraphs", len(classification.Paragraphs))

	tf := templatefiller.NewTemplateFiller()
	if p.smartClassifier != nil {
		tf.DeepSeekClient = p.smartClassifier.GetDeepSeekClient()
	}
	outputDir := filepath.Join(filepath.Dir(docPath), "corrected")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("template-driven rebuild mkdir: %w", err)
	}

	tempOutput, err := tf.Fill(ctx, studentPath, preparedTemplatePath, toTemplateFillClassification(classification), outputDir)
	if err != nil {
		return "", fmt.Errorf("template-driven rebuild fill: %w", err)
	}

	finalOutput := templateDrivenOutputPath(docPath)
	if filepath.Clean(tempOutput) != filepath.Clean(finalOutput) {
		_ = os.Remove(finalOutput)
		if err := os.Rename(tempOutput, finalOutput); err != nil {
			return "", fmt.Errorf("template-driven rebuild finalize output: %w", err)
		}
	} else {
		finalOutput = tempOutput
	}

	if ShellFillPostValidateEnabled() {
		ok100, reportPath, vErr := p.RunShellPostValidate(ctx, finalOutput, preparedTemplatePath, corrections)
		if vErr != nil {
			return "", fmt.Errorf("template-driven rebuild post-validate: %w", vErr)
		}
		log.Printf("[TemplateRebuild] post-validate compliance_100=%v report=%s", ok100, reportPath)
		if !ok100 && ShellFillFallbackOnValidateFail() {
			return "", fmt.Errorf("template-driven rebuild post-validate compliance_100=false")
		}
	}
	if unoErr := p.runTemplateDrivenUNO(ctx, finalOutput, preparedTemplatePath); unoErr != nil {
		log.Printf("[TemplateRebuild] UNO post-process skipped/failed: %v", unoErr)
	}

	return finalOutput, nil
}
