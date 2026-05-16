package templatecompile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const compilerVersion = "templatecompile-skeleton-v1"

type Compiler struct{}

func NewCompiler() *Compiler {
	return &Compiler{}
}

func (c *Compiler) Compile(ctx context.Context, templatePath string, opts CompileOptions) (*CompiledTemplatePackage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if templatePath == "" {
		return nil, fmt.Errorf("template path is required")
	}
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("output dir is required")
	}

	source, err := os.Open(templatePath)
	if err != nil {
		return nil, fmt.Errorf("open template docx: %w", err)
	}
	defer source.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, source); err != nil {
		return nil, fmt.Errorf("hash template docx: %w", err)
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind template docx: %w", err)
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	packageDir, err := os.MkdirTemp(opts.OutputDir, packageDirPrefix(opts, hash.Sum(nil))+"-*")
	if err != nil {
		return nil, fmt.Errorf("create compiled package dir: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(packageDir)
		}
	}()

	skeletonPath := filepath.Join(packageDir, "skeleton.docx")
	skeleton, err := os.Create(skeletonPath)
	if err != nil {
		return nil, fmt.Errorf("create skeleton docx: %w", err)
	}
	if _, err := io.Copy(skeleton, source); err != nil {
		skeleton.Close()
		return nil, fmt.Errorf("copy skeleton docx: %w", err)
	}
	if err := skeleton.Close(); err != nil {
		return nil, fmt.Errorf("close skeleton docx: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	success = true
	return &CompiledTemplatePackage{
		Manifest: TemplateManifest{
			SchoolID:        opts.SchoolID,
			TemplateName:    opts.TemplateName,
			Version:         opts.Version,
			DocxHash:        hex.EncodeToString(hash.Sum(nil)),
			CompilerVersion: compilerVersion,
			CompiledAt:      time.Now().UTC(),
		},
		BlockCatalog: []TemplateBlock{
			newBlock("cover_title", "cover_title", "text", 0, "", "style-cover-title", "{{cover_title}}", true),
			newBlock("abstract_cn_body", "abstract_cn_body", "rich_text", 1, "", "style-body-cn", "{{abstract_cn_body}}", true),
			newBlock("keywords_cn", "keywords_cn", "text", 2, "", "style-body-cn", "{{keywords_cn}}", false),
			newBlock("heading_1", "heading_1", "repeatable", 3, "", "style-heading-1", "{{heading_1}}", false),
			newBlock("body", "body", "repeatable", 4, "", "style-body-cn", "{{body}}", false),
			newBlock("content_blocks", "content_blocks", "repeatable", 5, "", "style-body-cn", "{{content_blocks}}", false),
			newBlock("references", "references", "repeatable", 6, "", "style-reference", "{{references}}", false),
			newBlock("acknowledgement", "acknowledgement", "rich_text", 7, "", "style-body-cn", "{{acknowledgement}}", false),
		},
		SkeletonPath:   skeletonPath,
		SkeletonSource: templatePath,
		PatchTargets: []string{
			"word/document.xml",
			"word/_rels/document.xml.rels",
			"word/settings.xml",
		},
		StyleProfiles: []StyleProfile{
			{
				StyleProfileID: "style-cover-title",
				Name:           "Cover Title",
				BasedOn:        "Title",
				Properties: map[string]string{
					"alignment": "center",
					"bold":      "true",
				},
			},
			{
				StyleProfileID: "style-body-cn",
				Name:           "Chinese Body",
				BasedOn:        "Normal",
				Properties: map[string]string{
					"language": "zh-CN",
					"spacing":  "single",
				},
			},
			{
				StyleProfileID: "style-heading-1",
				Name:           "Heading 1",
				BasedOn:        "Heading1",
				Properties: map[string]string{
					"outlineLevel": "1",
				},
			},
		},
		MappingContract: MappingContract{
			ContractID:  "mapping-default",
			PatchTarget: "word/document.xml",
			BlockBindings: map[string]string{
				"cover_title":      "{{cover_title}}",
				"abstract_cn_body": "{{abstract_cn_body}}",
				"keywords_cn":      "{{keywords_cn}}",
				"heading_1":        "{{heading_1}}",
				"body":             "{{body}}",
				"content_blocks":   "{{content_blocks}}",
				"references":       "{{references}}",
				"acknowledgement":  "{{acknowledgement}}",
			},
		},
		VerificationRules: []VerificationRule{
			{
				RuleID:    "verify-skeleton-docx-exists",
				Target:    "skeleton.docx",
				Assertion: "file_exists",
				Severity:  "error",
			},
			{
				RuleID:    "verify-document-patchable",
				Target:    "word/document.xml",
				Assertion: "zip_entry_exists",
				Severity:  "error",
			},
		},
	}, nil
}

func newBlock(blockID, kind, slotType string, orderIndex int, parentBlockID, styleProfileID, anchorMatch string, required bool) TemplateBlock {
	return TemplateBlock{
		BlockID:        blockID,
		Kind:           kind,
		SlotType:       slotType,
		OrderIndex:     orderIndex,
		ParentBlockID:  parentBlockID,
		StyleProfileID: styleProfileID,
		Anchor: Anchor{
			Path:  "word/document.xml",
			Match: anchorMatch,
		},
		SourceRegion: SourceRegion{
			Path:  "word/document.xml",
			Start: anchorMatch,
			End:   anchorMatch,
		},
		Capacity: Capacity{
			Min: 0,
			Max: 1,
		},
		Required: required,
		Accepts: []string{
			slotType,
		},
		PatchPolicy: PatchPolicy{
			Target: "word/document.xml",
			Mode:   "replace_anchor",
		},
		VerifyPolicy: VerifyPolicy{
			RuleID:   "verify-document-patchable",
			Severity: "error",
		},
	}
}

func packageDirPrefix(opts CompileOptions, hash []byte) string {
	return strings.Join([]string{
		safeSegment(opts.SchoolID),
		safeSegment(opts.Version),
		hex.EncodeToString(hash)[:12],
	}, "-")
}

func safeSegment(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "unknown"
	}

	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('-')
	}
	return builder.String()
}
