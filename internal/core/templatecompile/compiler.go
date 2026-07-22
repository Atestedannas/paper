package templatecompile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

const compilerVersion = "templatecompile-style-profile-v2"

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
	profile, err := templateprofile.Extract(templatePath)
	if err != nil {
		return nil, fmt.Errorf("extract template styles: %w", err)
	}
	if err := validateProfileNumbers(profile); err != nil {
		return nil, err
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
	blockCatalog := compileBlockCatalog(profile)
	bindings := make(map[string]string, len(blockCatalog))
	for _, block := range blockCatalog {
		bindings[block.BlockID] = block.Anchor.Match
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
		BlockCatalog:   blockCatalog,
		SkeletonPath:   skeletonPath,
		SkeletonSource: templatePath,
		PatchTargets: []string{
			"word/document.xml",
			"word/_rels/document.xml.rels",
			"word/settings.xml",
		},
		StyleProfiles: compileStyleProfiles(profile),
		MappingContract: MappingContract{
			ContractID:    "mapping-default",
			PatchTarget:   "word/document.xml",
			BlockBindings: bindings,
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

func compileBlockCatalog(profile *templateprofile.Profile) []TemplateBlock {
	type blockSpec struct {
		id, slot, style string
		required        bool
	}
	defaults := []blockSpec{
		{"cover_title", "text", "style-cover-title", true},
		{"abstract_cn_body", "rich_text", "style-body-cn", true},
		{"keywords_cn", "text", "style-body-cn", false},
		{"heading_1", "repeatable", "style-heading-1", false},
		{"body", "repeatable", "style-body-cn", false},
		{"content_blocks", "repeatable", "style-body-cn", false},
		{"references", "repeatable", "style-reference", false},
		{"acknowledgement", "rich_text", "style-body-cn", false},
	}
	seen := make(map[string]bool, len(defaults))
	blocks := make([]TemplateBlock, 0, len(defaults)+len(profile.Styles))
	for _, spec := range defaults {
		seen[spec.id] = true
		blocks = append(blocks, newBlock(spec.id, spec.id, spec.slot, len(blocks), "", spec.style, "{{"+spec.id+"}}", spec.required))
	}
	keys := make([]string, 0, len(profile.Styles))
	for key := range profile.Styles {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		slot := "rich_text"
		if strings.HasPrefix(key, "heading_") || key == "body_start" || strings.Contains(key, "references") {
			slot = "repeatable"
		}
		blocks = append(blocks, newBlock(key, key, slot, len(blocks), "", styleProfileID(key), "{{"+key+"}}", false))
	}
	return blocks
}

func styleProfileID(key string) string {
	switch key {
	case "title", "cover_title":
		return "style-cover-title"
	case "body", "body_start":
		return "style-body-cn"
	case "heading_1":
		return "style-heading-1"
	case "references":
		return "style-reference"
	default:
		return "style-" + strings.ReplaceAll(key, "_", "-")
	}
}

func compileStyleProfiles(profile *templateprofile.Profile) []StyleProfile {
	profiles := make([]StyleProfile, 0, len(profile.Styles))
	keys := make([]string, 0, len(profile.Styles))
	for key := range profile.Styles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	for _, key := range keys {
		style := profile.Styles[key]
		id, basedOn := styleProfileID(key), "Normal"
		switch key {
		case "title", "cover_title":
			basedOn = "Title"
		case "heading_1":
			basedOn = "Heading1"
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		profiles = append(profiles, StyleProfile{
			StyleProfileID: id,
			Name:           key,
			BasedOn:        basedOn,
			Properties: StyleProperties{
				EastAsiaFont:          style.FontEastAsia,
				ASCIIFont:             style.FontASCII,
				FontHint:              style.FontHint,
				FontSizeHalfPoints:    atoi(style.FontSizeHalfPt),
				ComplexSizeHalfPoints: atoi(style.ComplexSizeHalfPt),
				Bold:                  style.Bold,
				BoldSet:               style.BoldSet,
				Italic:                style.Italic,
				ItalicSet:             style.ItalicSet,
				Alignment:             style.Alignment,
				LineTwips:             atoi(style.Line),
				LineRule:              style.LineRule,
				BeforeTwips:           atoi(style.BeforeTwips),
				AfterTwips:            atoi(style.AfterTwips),
				FirstLineChars:        atoi(style.FirstLineChars),
				FirstLineTwips:        atoi(style.FirstLineTwips),
				OutlineLevel:          atoi(style.OutlineLevel),
				OutlineLevelSet:       strings.TrimSpace(style.OutlineLevel) != "",
			},
		})
	}
	return profiles
}

func atoi(value string) int {
	number, _ := strconv.Atoi(strings.TrimSpace(value))
	return number
}

func validateProfileNumbers(profile *templateprofile.Profile) error {
	if profile == nil {
		return fmt.Errorf("template profile is nil")
	}
	for name, style := range profile.Styles {
		values := map[string]string{
			"font_size_half_pt": style.FontSizeHalfPt, "complex_size_half_pt": style.ComplexSizeHalfPt,
			"line": style.Line, "before_twips": style.BeforeTwips, "after_twips": style.AfterTwips,
			"first_line_chars": style.FirstLineChars, "first_line_twips": style.FirstLineTwips,
			"outline_level": style.OutlineLevel,
		}
		for field, value := range values {
			if strings.TrimSpace(value) == "" {
				continue
			}
			if _, err := strconv.Atoi(strings.TrimSpace(value)); err != nil {
				return fmt.Errorf("invalid numeric style property %s.%s=%q", name, field, value)
			}
		}
	}
	return nil
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
