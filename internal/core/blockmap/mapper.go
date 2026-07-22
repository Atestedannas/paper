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
		CoverFields:    copyCoverFields(paper.CoverFields),
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
				binding := Binding{
					BlockID:   block.BlockID,
					BlockKind: block.Kind,
					Payload:   payload,
				}
				populatePayloadMetadata(&binding, paper)
				result.Bindings = append(result.Bindings, binding)
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

		binding := Binding{
			BlockID:   block.BlockID,
			BlockKind: block.Kind,
			Payload:   payloads[0],
		}
		populatePayloadMetadata(&binding, paper)
		result.Bindings = append(result.Bindings, binding)
	}
	appendBackMatterFallbackBindings(result, paper)

	return result, nil
}

func populatePayloadMetadata(binding *Binding, paper *paperparse.ParsedPaper) {
	if binding == nil || paper == nil || binding.BlockKind != "content_blocks" {
		return
	}
	for index, block := range paper.ContentBlocks {
		if (block.XML != "" && strings.TrimSpace(block.XML) == strings.TrimSpace(binding.Payload)) ||
			(block.XML == "" && strings.TrimSpace(block.Text) == strings.TrimSpace(binding.Payload)) {
			binding.PayloadKind = block.Kind
			binding.PayloadXML = strings.TrimSpace(block.XML)
			binding.Level = block.Level
			binding.SourceIndex = index
			return
		}
	}
}

func appendBackMatterFallbackBindings(result *MappingResult, paper *paperparse.ParsedPaper) {
	if result == nil || paper == nil {
		return
	}
	references, acknowledgements := splitBackMatterPayloads(paper.References, paper.Acknowledgements)
	if !hasBinding(result.Bindings, "references") && !hasBindingKind(result.Bindings, "references") {
		if payload := strings.TrimSpace(strings.Join(references, "\n")); payload != "" {
			result.Bindings = append(result.Bindings, Binding{BlockID: "references", BlockKind: "back_matter", Payload: payload})
		}
	}
	if !hasBinding(result.Bindings, "acknowledgement") && !hasBindingKind(result.Bindings, "acknowledgement") {
		if payload := strings.TrimSpace(strings.Join(acknowledgements, "\n")); payload != "" {
			result.Bindings = append(result.Bindings, Binding{BlockID: "acknowledgement", BlockKind: "back_matter", Payload: payload})
		}
	}
}

func splitBackMatterPayloads(references []string, acknowledgements []string) ([]string, []string) {
	cleanRefs := make([]string, 0, len(references))
	cleanThanks := append([]string(nil), acknowledgements...)
	inThanks := false
	for _, payload := range references {
		text := strings.TrimSpace(payload)
		if text == "" {
			continue
		}
		if isAcknowledgementHeading(text) {
			inThanks = true
			if content := strings.TrimSpace(strings.TrimPrefix(normalizeFragment(text), "致谢")); content != "" {
				cleanThanks = append(cleanThanks, content)
			}
			continue
		}
		if inThanks {
			cleanThanks = append(cleanThanks, text)
			continue
		}
		cleanRefs = append(cleanRefs, text)
	}
	return cleanRefs, cleanThanks
}

func isAcknowledgementHeading(text string) bool {
	return strings.HasPrefix(normalizeFragment(text), "致谢")
}

func hasBinding(bindings []Binding, blockID string) bool {
	for _, binding := range bindings {
		if binding.BlockID == blockID {
			return true
		}
	}
	return false
}

func hasBindingKind(bindings []Binding, kind string) bool {
	for _, binding := range bindings {
		if binding.BlockKind == kind {
			return true
		}
	}
	return false
}

func copyCoverFields(fields map[string]string) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	copied := make(map[string]string, len(fields))
	for key, value := range fields {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		copied[key] = value
	}
	return copied
}

func isEmptyPaper(paper *paperparse.ParsedPaper) bool {
	return len(paper.CoverFields) == 0 &&
		len(paper.AbstractCN) == 0 &&
		len(paper.KeywordsCN) == 0 &&
		len(paper.Headings) == 0 &&
		len(paper.Body) == 0 &&
		len(paper.References) == 0 &&
		len(paper.Acknowledgements) == 0 &&
		len(paper.ContentBlocks) == 0 &&
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
			if payload := coverTitlePayload(paper.CoverFields); payload != "" {
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
		case "body":
			for _, paragraph := range paper.Body {
				if payload := strings.TrimSpace(paragraph); payload != "" {
					payloads = append(payloads, payload)
				}
			}
		case "content_blocks":
			for _, block := range paper.ContentBlocks {
				if isReferenceHeading(block.Text) {
					break
				}
				if isBackMatterBlock(block) || isCoverFieldFragment(block.Text, paper.CoverFields) {
					continue
				}
				if block.Kind == "table" && strings.TrimSpace(block.XML) != "" {
					payloads = append(payloads, strings.TrimSpace(block.XML))
					continue
				}
				if payload := strings.TrimSpace(block.Text); payload != "" {
					payloads = append(payloads, payload)
				}
			}
		case "references":
			references, _ := splitBackMatterPayloads(paper.References, paper.Acknowledgements)
			if len(references) == 0 {
				references = referencePayloadsFromContent(paper.ContentBlocks)
			}
			if payload := strings.TrimSpace(strings.Join(references, "\n")); payload != "" {
				payloads = append(payloads, payload)
			}
		case "acknowledgement":
			_, acknowledgements := splitBackMatterPayloads(paper.References, paper.Acknowledgements)
			if payload := strings.TrimSpace(strings.Join(acknowledgements, "\n")); payload != "" {
				payloads = append(payloads, payload)
			}
		}
	}
	return payloads
}

func isBackMatterBlock(block paperparse.ContentBlock) bool {
	kind := strings.TrimSpace(block.Kind)
	if kind == "references" || kind == "acknowledgement" {
		return true
	}
	if looksLikeReferenceText(block.Text) {
		return true
	}
	if kind == "section_label" {
		text := strings.ToLower(strings.TrimSpace(block.Text))
		return strings.Contains(text, "reference") ||
			strings.Contains(text, "acknowledgement") ||
			strings.Contains(text, "\u53c2\u8003\u6587\u732e") ||
			strings.Contains(text, "\u81f4\u8c22")
	}
	return false
}

func looksLikeReferenceText(text string) bool {
	text = strings.TrimSpace(text)
	if isReferenceHeading(text) || strings.Contains(text, "\u81f4\u8c22") {
		return true
	}
	if strings.HasPrefix(text, "[") {
		end := strings.Index(text, "]")
		return end > 1 && end <= 4
	}
	return false
}

func isReferenceHeading(text string) bool {
	normalized := normalizeFragment(text)
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "references" {
		return true
	}
	if !strings.HasPrefix(normalized, "\u53c2\u8003\u6587\u732e") {
		return false
	}
	suffix := strings.TrimPrefix(normalized, "\u53c2\u8003\u6587\u732e")
	suffix = strings.Trim(suffix, ":：")
	return suffix == ""
}

func referencePayloadsFromContent(blocks []paperparse.ContentBlock) []string {
	var references []string
	inReferences := false
	for _, block := range blocks {
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		if isReferenceHeading(text) {
			inReferences = true
			continue
		}
		if !inReferences {
			continue
		}
		if strings.Contains(text, "\u81f4\u8c22") {
			break
		}
		references = append(references, text)
	}
	return references
}

func isCoverFieldFragment(text string, fields map[string]string) bool {
	text = normalizeFragment(text)
	if text == "" {
		return true
	}
	for _, value := range []string{
		"\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587/\u8bbe\u8ba1",
		"\u672c\u79d1\u6bd5\u4e1a\u8bba\u6587",
		"\u9898\u76ee", "\u5b66\u9662", "\u4e13\u4e1a", "\u73ed\u7ea7",
		"\u5b66\u53f7", "\u59d3\u540d", "\u6307\u5bfc\u6559\u5e08", "\u5b8c\u6210\u65e5\u671f",
	} {
		if text == normalizeFragment(value) {
			return true
		}
	}
	for key, value := range fields {
		key = normalizeFragment(key)
		value = normalizeFragment(value)
		if text == key || text == value {
			return true
		}
		if len(text) >= 4 && len(value) >= 4 && strings.Contains(value, text) {
			return true
		}
	}
	if isCoverDateFragment(text) {
		return true
	}
	return false
}

func normalizeFragment(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), "")
}

func isCoverDateFragment(text string) bool {
	return strings.Contains(text, "\u5e74") && strings.Contains(text, "\u6708") && len([]rune(text)) <= 12
}
func coverTitlePayload(fields map[string]string) string {
	for _, key := range []string{
		"cover_title",
		"\u9898\u76ee",
		"\u8bba\u6587\u9898\u76ee",
		"\u6bd5\u4e1a\u8bba\u6587\u9898\u76ee",
		"\u6807\u9898",
	} {
		if payload := strings.TrimSpace(fields[key]); payload != "" {
			return payload
		}
	}
	return ""
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
	case "cover_title", "abstract_cn_body", "keywords_cn", "heading_1", "body", "content_blocks", "references", "acknowledgement":
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
