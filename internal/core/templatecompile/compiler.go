package templatecompile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

	skeletonPath := filepath.Join(opts.OutputDir, "skeleton.docx")
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

	return &CompiledTemplatePackage{
		Manifest: TemplateManifest{
			SchoolID:        opts.SchoolID,
			TemplateName:    opts.TemplateName,
			Version:         opts.Version,
			DocxHash:        hex.EncodeToString(hash.Sum(nil)),
			CompilerVersion: compilerVersion,
			CompiledAt:      time.Now().UTC(),
		},
		BlockCatalog: map[string]TemplateBlock{
			"cover_title": {
				Kind:        "cover_title",
				Description: "Cover title placeholder",
			},
			"abstract_cn_body": {
				Kind:        "abstract_cn_body",
				Description: "Chinese abstract body placeholder",
			},
			"heading_1": {
				Kind:        "heading_1",
				Description: "Level 1 heading style",
			},
		},
		SkeletonPath:   skeletonPath,
		SkeletonSource: templatePath,
		PatchTargets: []string{
			"word/document.xml",
			"word/_rels/document.xml.rels",
			"word/settings.xml",
		},
		StyleProfiles: []StyleProfile{
			{Name: "default"},
		},
		MappingContract: MappingContract{
			Bindings: map[string]string{
				"cover_title":      "word/document.xml",
				"abstract_cn_body": "word/document.xml",
				"heading_1":        "word/document.xml",
			},
		},
		VerificationRules: []VerificationRule{
			{Name: "skeleton_docx_exists"},
		},
	}, nil
}
