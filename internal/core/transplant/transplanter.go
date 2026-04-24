package transplant

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/blockmap"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/templatecompile"
)

const defaultPatchTarget = "word/document.xml"

type GenerateInput struct {
	CompiledTemplate *templatecompile.CompiledTemplatePackage
	Mapping          *blockmap.MappingResult
	OutputPath       string
}

type Transplanter struct{}

func NewTransplanter() *Transplanter {
	return &Transplanter{}
}

func (t *Transplanter) Generate(ctx context.Context, input GenerateInput) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateInput(input); err != nil {
		return err
	}

	pkg, err := ooxmlpkg.Open(input.CompiledTemplate.SkeletonPath)
	if err != nil {
		return fmt.Errorf("open skeleton docx %q: %w", input.CompiledTemplate.SkeletonPath, err)
	}

	replacements := buildReplacements(input.Mapping.Bindings)
	for _, target := range patchTargets(input.CompiledTemplate.PatchTargets) {
		content, ok := pkg.Get(target)
		if !ok {
			return fmt.Errorf("patch target %q not found in skeleton docx", target)
		}
		pkg.Set(target, []byte(applyReplacements(string(content), replacements)))
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := pkg.Write(input.OutputPath); err != nil {
		return fmt.Errorf("write generated docx %q: %w", input.OutputPath, err)
	}

	return nil
}

func validateInput(input GenerateInput) error {
	if input.CompiledTemplate == nil {
		return fmt.Errorf("compiled template is nil")
	}
	if strings.TrimSpace(input.CompiledTemplate.SkeletonPath) == "" {
		return fmt.Errorf("compiled template skeleton path is empty")
	}
	if input.Mapping == nil {
		return fmt.Errorf("mapping is nil")
	}
	if strings.TrimSpace(input.OutputPath) == "" {
		return fmt.Errorf("output path is empty")
	}
	for _, target := range input.CompiledTemplate.PatchTargets {
		if strings.TrimSpace(target) == "" {
			return fmt.Errorf("patch target is empty")
		}
	}
	return nil
}

func patchTargets(targets []string) []string {
	if len(targets) == 0 {
		return []string{defaultPatchTarget}
	}
	return targets
}

func buildReplacements(bindings []blockmap.Binding) map[string]string {
	grouped := make(map[string][]string)
	for _, binding := range bindings {
		grouped[binding.BlockID] = append(grouped[binding.BlockID], html.EscapeString(binding.Payload))
	}

	replacements := make(map[string]string, len(grouped))
	for blockID, payloads := range grouped {
		replacements["{{"+blockID+"}}"] = strings.Join(payloads, "\n")
	}
	return replacements
}

func applyReplacements(content string, replacements map[string]string) string {
	for placeholder, payload := range replacements {
		content = strings.ReplaceAll(content, placeholder, payload)
	}
	return content
}
