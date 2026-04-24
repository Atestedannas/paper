package blockmap

import (
	"fmt"
	"sort"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/paperparse"
	"github.com/paper-format-checker/backend/internal/core/templatecompile"
)

func (m *Mapper) Map(template *templatecompile.CompiledTemplatePackage, paper *paperparse.ParsedPaper) (*MappingResult, error) {
	if template == nil {
		return nil, fmt.Errorf("blockmap: template is nil")
	}
	if len(template.BlockCatalog) == 0 {
		return nil, fmt.Errorf("blockmap: template block catalog is empty")
	}
	if paper == nil {
		return nil, fmt.Errorf("blockmap: paper is nil")
	}
	if isEmptyPaper(paper) {
		return nil, fmt.Errorf("blockmap: paper is empty")
	}

	result := &MappingResult{
		UnmappedBlocks: append([]string(nil), paper.Abnormal...),
	}

	blocks := append([]templatecompile.TemplateBlock(nil), template.BlockCatalog...)
	sort.SliceStable(blocks, func(i, j int) bool {
		return blocks[i].OrderIndex < blocks[j].OrderIndex
	})

	duplicateIDs := duplicateBlockIDs(blocks)
	for _, block := range blocks {
		if duplicateIDs[block.BlockID] {
			addUnique(&result.AmbiguousBlocks, block.BlockID)
		}

		payloads := payloadsForBlock(block, paper)
		if len(payloads) == 0 {
			continue
		}

		if block.SlotType == "repeatable" {
			for i, payload := range payloads {
				result.Bindings = append(result.Bindings, Binding{
					BlockID:   block.BlockID,
					BlockKind: block.Kind,
					Payload:   payload,
				})
				if i > 0 {
					result.GeneratedBlocks = append(result.GeneratedBlocks, block.BlockID)
				}
			}
			continue
		}

		if len(payloads) > 1 {
			addUnique(&result.AmbiguousBlocks, block.BlockID)
			continue
		}

		result.Bindings = append(result.Bindings, Binding{
			BlockID:   block.BlockID,
			BlockKind: block.Kind,
			Payload:   payloads[0],
		})
	}

	return result, nil
}

func isEmptyPaper(paper *paperparse.ParsedPaper) bool {
	return len(paper.CoverFields) == 0 &&
		len(paper.AbstractCN) == 0 &&
		len(paper.KeywordsCN) == 0 &&
		len(paper.Headings) == 0 &&
		len(paper.Body) == 0 &&
		len(paper.References) == 0 &&
		len(paper.Acknowledgements) == 0 &&
		len(paper.Abnormal) == 0
}

func duplicateBlockIDs(blocks []templatecompile.TemplateBlock) map[string]bool {
	counts := make(map[string]int)
	for _, block := range blocks {
		if block.BlockID == "" {
			continue
		}
		counts[block.BlockID]++
	}

	duplicates := make(map[string]bool)
	for blockID, count := range counts {
		if count > 1 {
			duplicates[blockID] = true
		}
	}
	return duplicates
}

func payloadsForBlock(block templatecompile.TemplateBlock, paper *paperparse.ParsedPaper) []string {
	keys := acceptedKeys(block)
	payloads := make([]string, 0, len(keys))
	for _, key := range keys {
		switch key {
		case "cover_title":
			if payload := strings.TrimSpace(paper.CoverFields["cover_title"]); payload != "" {
				payloads = append(payloads, payload)
			}
		case "abstract_cn_body":
			if payload := strings.TrimSpace(strings.Join(paper.AbstractCN, "\n")); payload != "" {
				payloads = append(payloads, payload)
			}
		case "keywords_cn":
			if payload := strings.TrimSpace(strings.Join(paper.KeywordsCN, "；")); payload != "" {
				payloads = append(payloads, payload)
			}
		case "heading_1":
			for _, heading := range paper.Headings {
				if heading.Level == 1 && strings.TrimSpace(heading.Text) != "" {
					payloads = append(payloads, strings.TrimSpace(heading.Text))
				}
			}
		case "references":
			if payload := strings.TrimSpace(strings.Join(paper.References, "\n")); payload != "" {
				payloads = append(payloads, payload)
			}
		case "acknowledgement":
			if payload := strings.TrimSpace(strings.Join(paper.Acknowledgements, "\n")); payload != "" {
				payloads = append(payloads, payload)
			}
		}
	}
	return payloads
}

func acceptedKeys(block templatecompile.TemplateBlock) []string {
	seen := make(map[string]bool)
	keys := make([]string, 0, len(block.Accepts)+1)
	addSupportedKey := func(key string) {
		if isSupportedKey(key) && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}

	addSupportedKey(block.Kind)
	for _, accept := range block.Accepts {
		addSupportedKey(accept)
	}
	return keys
}

func isSupportedKey(key string) bool {
	switch key {
	case "cover_title", "abstract_cn_body", "keywords_cn", "heading_1", "references", "acknowledgement":
		return true
	default:
		return false
	}
}

func addUnique(values *[]string, value string) {
	for _, existing := range *values {
		if existing == value {
			return
		}
	}
	*values = append(*values, value)
}
