package ooxmlpatch

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

const defaultTarget = "word/document.xml"

type Patch struct {
	Target  string
	Find    string
	Replace string
}

type Writer struct {
	patches        []Patch
	allowedTargets map[string]struct{}
}

func NewWriter(patches []Patch) *Writer {
	return NewWriterWithTargets(patches, []string{defaultTarget})
}

func NewWriterWithTargets(patches []Patch, allowedTargets []string) *Writer {
	if len(allowedTargets) == 0 {
		allowedTargets = []string{defaultTarget}
	}

	allowed := make(map[string]struct{}, len(allowedTargets))
	for _, target := range allowedTargets {
		target = normalizeTarget(target)
		allowed[target] = struct{}{}
	}

	return &Writer{
		patches:        append([]Patch(nil), patches...),
		allowedTargets: allowed,
	}
}

func (w *Writer) Apply(ctx context.Context, docxPath string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if w == nil {
		return fmt.Errorf("patch writer is nil")
	}
	if strings.TrimSpace(docxPath) == "" {
		return fmt.Errorf("docx path is empty")
	}

	patchesByTarget := make(map[string][]Patch)
	for _, patch := range w.patches {
		target := normalizeTarget(patch.Target)
		if _, ok := w.allowedTargets[target]; !ok {
			return fmt.Errorf("patch target %q is not allowed", target)
		}
		if patch.Find == "" {
			return fmt.Errorf("patch find text is empty for target %q", target)
		}
		patch.Target = target
		patchesByTarget[target] = append(patchesByTarget[target], patch)
	}

	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		return fmt.Errorf("open docx %q: %w", docxPath, err)
	}

	for target, patches := range patchesByTarget {
		content, ok := pkg.Get(target)
		if !ok {
			return fmt.Errorf("patch target %q not found in docx", target)
		}

		pkg.Set(target, []byte(applyPatches(string(content), patches)))
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := pkg.Write(docxPath); err != nil {
		return fmt.Errorf("write patched docx %q: %w", docxPath, err)
	}

	return nil
}

func applyPatches(content string, patches []Patch) string {
	var builder strings.Builder
	for offset := 0; offset < len(content); {
		matched := false
		for _, patch := range patches {
			if strings.HasPrefix(content[offset:], patch.Find) {
				builder.WriteString(html.EscapeString(patch.Replace))
				offset += len(patch.Find)
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		builder.WriteByte(content[offset])
		offset++
	}
	return builder.String()
}

func normalizeTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return defaultTarget
	}
	return target
}
