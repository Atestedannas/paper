package fileprocessor

import "gitee.com/greatmusicians/unioffice/document"

// FormatLockManager 只锁定“类型 + 当前段落数”均已验证通过的结果。
// 分类数量变化时锁自动失效，避免新误分类段落被错误跳过。
type FormatLockManager struct {
	verifiedCounts map[string]int
}

func NewFormatLockManager() *FormatLockManager {
	return &FormatLockManager{verifiedCounts: map[string]int{}}
}

func (m *FormatLockManager) Lock(paragraphType string, paragraphCount int) {
	if paragraphCount > 0 {
		m.verifiedCounts[paragraphType] = paragraphCount
	}
}

func (m *FormatLockManager) IsLocked(paragraphType string, paragraphCount int) bool {
	count, ok := m.verifiedCounts[paragraphType]
	return ok && count == paragraphCount
}

func (m *FormatLockManager) Unlock(paragraphType string) {
	delete(m.verifiedCounts, paragraphType)
}

func verifyAndLockParagraphTypes(
	processor *EnhancedProcessor,
	locks *FormatLockManager,
	classified map[string][]document.Paragraph,
	specs map[string]ParagraphFormatSpec,
) {
	if processor == nil || locks == nil {
		return
	}
	diffs := NewFormatVerifier(processor, nil).compareAllWithSpecs(classified, specs)
	failed := make(map[string]bool)
	for _, diff := range diffs {
		failed[diff.Category] = true
		locks.Unlock(diff.Category)
	}
	for category := range specs {
		if paragraphs := classified[category]; len(paragraphs) > 0 && !failed[category] && !categoryHasRunFormatMismatch(paragraphs, specs[category]) {
			locks.Lock(category, len(paragraphs))
		}
	}
}

func categoryHasRunFormatMismatch(paragraphs []document.Paragraph, spec ParagraphFormatSpec) bool {
	for _, paragraph := range paragraphs {
		if paragraphHasRunFormatMismatch(paragraph, spec) {
			return true
		}
	}
	return false
}

func lockedCategoryMap(locks *FormatLockManager, classified map[string][]document.Paragraph) map[string]bool {
	result := make(map[string]bool)
	if locks == nil {
		return result
	}
	for category, paragraphs := range classified {
		result[category] = locks.IsLocked(category, len(paragraphs))
	}
	return result
}

func v2ParagraphMap(classified []V2ClassifiedPara) map[string][]document.Paragraph {
	result := make(map[string][]document.Paragraph)
	for _, paragraph := range classified {
		if paragraph.Text != "" {
			result[paragraph.Type] = append(result[paragraph.Type], paragraph.Para)
		}
	}
	return result
}

func unlockedV2Paragraphs(locks *FormatLockManager, classified []V2ClassifiedPara) []V2ClassifiedPara {
	if locks == nil {
		return classified
	}
	counts := make(map[string]int)
	for _, paragraph := range classified {
		if paragraph.Text != "" {
			counts[paragraph.Type]++
		}
	}
	result := make([]V2ClassifiedPara, 0, len(classified))
	for _, paragraph := range classified {
		if !locks.IsLocked(paragraph.Type, counts[paragraph.Type]) {
			result = append(result, paragraph)
		}
	}
	return result
}
