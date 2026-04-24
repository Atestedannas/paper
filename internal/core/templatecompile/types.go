package templatecompile

import (
	"fmt"
	"time"
)

type CompileOptions struct {
	SchoolID     string
	TemplateName string
	Version      string
	OutputDir    string
}

type CompiledTemplatePackage struct {
	Manifest          TemplateManifest
	BlockCatalog      map[string]TemplateBlock
	SkeletonPath      string
	SkeletonSource    string
	PatchTargets      []string
	StyleProfiles     []StyleProfile
	MappingContract   MappingContract
	VerificationRules []VerificationRule
}

type TemplateManifest struct {
	SchoolID        string
	TemplateName    string
	Version         string
	DocxHash        string
	CompilerVersion string
	CompiledAt      time.Time
}

type TemplateBlock struct {
	Kind        string
	Description string
}

type StyleProfile struct {
	Name string
}

type MappingContract struct {
	Bindings map[string]string
}

type VerificationRule struct {
	Name string
}

func (p *CompiledTemplatePackage) MustBlock(kind string) (TemplateBlock, error) {
	if p == nil {
		return TemplateBlock{}, fmt.Errorf("compiled template package is nil")
	}

	block, ok := p.BlockCatalog[kind]
	if !ok {
		return TemplateBlock{}, fmt.Errorf("template block %q not found", kind)
	}

	return block, nil
}
