package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

type openXMLValidationResult struct {
	OK     bool                     `json:"ok"`
	Errors []openXMLValidationError `json:"errors"`
}

type openXMLValidationError struct {
	Description string `json:"description"`
	Path        string `json:"path"`
	ID          string `json:"id"`
}

func addOpenXMLSchemaIssues(ctx context.Context, docxPath string, result *Result) {
	binary := strings.TrimSpace(os.Getenv("OPENXML_VALIDATOR_BIN"))
	required := envBoolDefault("OPENXML_VALIDATOR_REQUIRED", false)
	if binary == "" {
		if resolved, err := exec.LookPath("openxml-validator"); err == nil {
			binary = resolved
		}
	}
	if binary == "" {
		if required {
			appendFatalIssueOnce(result, "openxml_schema_validator_missing", "OpenXmlValidator is required but unavailable", docxPath)
		}
		return
	}

	validation, output, commandErr, parseErr := runOpenXMLValidator(ctx, binary, docxPath)
	if parseErr != nil {
		appendFatalIssueOnce(result, "openxml_schema_validator_failed", fmt.Sprintf("OpenXmlValidator failed: %v; output: %.500s", commandErr, output), docxPath)
		return
	}
	if !validation.OK && len(validation.Errors) > 0 {
		if repaired, err := ooxmlpkg.RepairPropertyOrder(docxPath); err != nil {
			appendFatalIssueOnce(result, "openxml_schema_repair_failed", fmt.Sprintf("OOXML property-order repair failed: %v", err), docxPath)
			return
		} else if repaired > 0 {
			validation, output, commandErr, parseErr = runOpenXMLValidator(ctx, binary, docxPath)
			if parseErr != nil {
				appendFatalIssueOnce(result, "openxml_schema_validator_failed", fmt.Sprintf("OpenXmlValidator failed after repair: %v; output: %.500s", commandErr, output), docxPath)
				return
			}
		}
	}
	if validation.OK && commandErr == nil {
		return
	}
	if len(validation.Errors) == 0 {
		appendFatalIssueOnce(result, "openxml_schema_validator_failed", fmt.Sprintf("OpenXmlValidator failed: %v", commandErr), docxPath)
		return
	}
	for index, schemaError := range validation.Errors {
		if index == 100 {
			break
		}
		target := strings.TrimSpace(schemaError.Path)
		if target == "" {
			target = docxPath
		}
		result.FatalIssues = append(result.FatalIssues, Issue{
			Kind:     "openxml_schema",
			Severity: "fatal",
			Message:  strings.TrimSpace(schemaError.Description),
			Target:   target,
		})
	}
}

func runOpenXMLValidator(ctx context.Context, binary, docxPath string) (openXMLValidationResult, []byte, error, error) {
	output, commandErr := exec.CommandContext(ctx, binary, docxPath).CombinedOutput()
	var validation openXMLValidationResult
	parseErr := json.Unmarshal(output, &validation)
	return validation, output, commandErr, parseErr
}
