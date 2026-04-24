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
	BlockCatalog      []TemplateBlock
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
	BlockID        string
	Kind           string
	SlotType       string
	OrderIndex     int
	ParentBlockID  string
	StyleProfileID string
	Anchor         Anchor
	SourceRegion   SourceRegion
	Capacity       Capacity
	Required       bool
	Accepts        []string
	PatchPolicy    PatchPolicy
	VerifyPolicy   VerifyPolicy
}

type Anchor struct {
	Path  string
	Match string
}

type SourceRegion struct {
	Path  string
	Start string
	End   string
}

type Capacity struct {
	Min int
	Max int
}

type PatchPolicy struct {
	Target string
	Mode   string
}

type VerifyPolicy struct {
	RuleID   string
	Severity string
}

type StyleProfile struct {
	StyleProfileID string
	Name           string
	BasedOn        string
	Properties     map[string]string
}

type MappingContract struct {
	ContractID    string
	BlockBindings map[string]string
	PatchTarget   string
}

type VerificationRule struct {
	RuleID    string
	Target    string
	Assertion string
	Severity  string
}

func (p *CompiledTemplatePackage) MustBlock(kind string) (TemplateBlock, error) {
	if p == nil {
		return TemplateBlock{}, fmt.Errorf("compiled template package is nil")
	}

	for _, block := range p.BlockCatalog {
		if block.Kind == kind {
			return block, nil
		}
	}

	return TemplateBlock{}, fmt.Errorf("template block %q not found", kind)
}
