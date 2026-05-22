package fileprocessor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/greatmusicians/unioffice/document"
	"github.com/paper-format-checker/backend/pkg/templatefiller"
)

// StyleFormatterConfig remains only as a compatibility shim after removing the
// python-docx based formatter path.
type StyleFormatterConfig struct {
	Enabled         bool
	ScriptPath      string
	UnoEnabled      bool
	UnoScriptPath   string
	UnoMode         string
	SchoolSpecPath  string
	TimeoutSec      int
	MaxRepairRounds int
}

func DefaultStyleFormatterConfig() StyleFormatterConfig {
	return StyleFormatterConfig{}
}

func TemplateShellFillEnabled() bool {
	return false
}

func ShellFillPostValidateEnabled() bool {
	return false
}

func ShellFillFallbackOnValidateFail() bool {
	return false
}

func (p *EnhancedProcessor) RunStyleFormatter(
	_ context.Context,
	_ string,
	_ string,
	_ StyleFormatterConfig,
	_ []map[string]interface{},
) (string, error) {
	return "", fmt.Errorf("python-docx style formatter has been removed")
}

func (p *EnhancedProcessor) RunTemplateShellInPlaceFill(
	_ context.Context,
	_ string,
	_ string,
	_ []map[string]interface{},
) (string, error) {
	return "", fmt.Errorf("python template shell fill has been removed")
}

func (p *EnhancedProcessor) RunShellPostValidate(
	_ context.Context,
	_ string,
	_ string,
	_ []map[string]interface{},
) (bool, string, error) {
	return false, "", fmt.Errorf("python shell post-validate has been removed")
}

func (p *EnhancedProcessor) runUnoFormatter(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ StyleFormatterConfig,
) error {
	return fmt.Errorf("python UNO formatter has been removed")
}

func executableDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exePath)
}

func (p *EnhancedProcessor) classifyDocumentForStyleFormatter(docPath string) (templatefiller.ClassificationResult, error) {
	doc, err := document.Open(docPath)
	if err != nil {
		return templatefiller.ClassificationResult{}, err
	}
	defer doc.Close()

	classifier := NewV2DeterministicClassifier(p)
	classified := classifier.Classify(BodyLevelParagraphsOnly(doc))

	result := templatefiller.ClassificationResult{
		Paragraphs: make([]templatefiller.ClassificationParagraph, 0, len(classified)),
	}
	for _, para := range classified {
		result.Paragraphs = append(result.Paragraphs, templatefiller.ClassificationParagraph{
			Index: para.ParaIdx,
			Type:  para.Type,
			Text:  para.Text,
		})
	}
	return result, nil
}

func toTemplateFillClassification(classification templatefiller.ClassificationResult) templatefiller.ClassificationResult {
	return classification
}
