package templatecompile

import (
	"fmt"
	"time"
)

type CompileOptions struct {
	SchoolID     string `json:"school_id"`
	TemplateName string `json:"template_name"`
	Version      string `json:"version"`
	OutputDir    string `json:"output_dir"`
}

type CompiledTemplatePackage struct {
	Manifest          TemplateManifest   `json:"manifest"`
	BlockCatalog      []TemplateBlock    `json:"block_catalog"`
	SkeletonPath      string             `json:"skeleton_path"`
	SkeletonSource    string             `json:"skeleton_source"`
	PatchTargets      []string           `json:"patch_targets"`
	StyleProfiles     []StyleProfile     `json:"style_profiles"`
	MappingContract   MappingContract    `json:"mapping_contract"`
	VerificationRules []VerificationRule `json:"verification_rules"`
}

type TemplateManifest struct {
	SchoolID        string    `json:"school_id"`
	TemplateName    string    `json:"template_name"`
	Version         string    `json:"version"`
	DocxHash        string    `json:"docx_hash"`
	CompilerVersion string    `json:"compiler_version"`
	CompiledAt      time.Time `json:"compiled_at"`
}

type TemplateBlock struct {
	BlockID        string       `json:"block_id"`
	Kind           string       `json:"kind"`
	SlotType       string       `json:"slot_type"`
	OrderIndex     int          `json:"order_index"`
	ParentBlockID  string       `json:"parent_block_id"`
	StyleProfileID string       `json:"style_profile_id"`
	Anchor         Anchor       `json:"anchor"`
	SourceRegion   SourceRegion `json:"source_region"`
	Capacity       Capacity     `json:"capacity"`
	Required       bool         `json:"required"`
	Accepts        []string     `json:"accepts"`
	PatchPolicy    PatchPolicy  `json:"patch_policy"`
	VerifyPolicy   VerifyPolicy `json:"verify_policy"`
}

type Anchor struct {
	Path  string `json:"path"`
	Match string `json:"match"`
}

type SourceRegion struct {
	Path  string `json:"path"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type Capacity struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type PatchPolicy struct {
	Target string `json:"target"`
	Mode   string `json:"mode"`
}

type VerifyPolicy struct {
	RuleID   string `json:"rule_id"`
	Severity string `json:"severity"`
}

type StyleProfile struct {
	StyleProfileID string            `json:"style_profile_id"`
	Name           string            `json:"name"`
	BasedOn        string            `json:"based_on"`
	Properties     map[string]string `json:"properties"`
}

type MappingContract struct {
	ContractID    string            `json:"contract_id"`
	BlockBindings map[string]string `json:"block_bindings"`
	PatchTarget   string            `json:"patch_target"`
}

type VerificationRule struct {
	RuleID    string `json:"rule_id"`
	Target    string `json:"target"`
	Assertion string `json:"assertion"`
	Severity  string `json:"severity"`
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
