package fileprocessor

import (
	"log"

	"gitee.com/greatmusicians/unioffice/document"
)

type RepairResult struct {
	Rounds            int
	TotalFixes        int
	InitialDiffs      int
	FinalDiffs        int
	NeedsManualReview bool
	Regressed         bool
}

type RepairAgent struct {
	processor *EnhancedProcessor
	maxRounds int
}

func NewRepairAgent(processor *EnhancedProcessor, maxRounds int) *RepairAgent {
	if maxRounds < 1 {
		maxRounds = 1
	}
	return &RepairAgent{processor: processor, maxRounds: maxRounds}
}

// Run 执行最多三轮确定性的“验证 → 修复 → 回归检查”。
// AI 只负责上游分类，不参与字号、字体等格式值决策。
func (a *RepairAgent) Run(doc *document.Document, specs map[string]ParagraphFormatSpec) RepairResult {
	verifier := NewFormatVerifier(a.processor, nil)
	applier := NewAIFormatApplier(a.processor)
	result := RepairResult{}
	previousDiffs := -1

	for round := 1; round <= a.maxRounds; round++ {
		classified := a.processor.classifyParagraphs(doc.Paragraphs())
		diffs := verifier.compareAllWithSpecs(classified, specs)
		if round == 1 {
			result.InitialDiffs = len(diffs)
		}
		result.FinalDiffs = len(diffs)
		if len(diffs) == 0 {
			return result
		}
		if previousDiffs >= 0 && len(diffs) >= previousDiffs {
			result.NeedsManualReview = true
			result.Regressed = len(diffs) > previousDiffs
			return result
		}

		result.Rounds = round
		fixes := verifier.autoFixDiffsWithSpecs(classified, diffs, specs, applier)
		result.TotalFixes += fixes
		previousDiffs = len(diffs)
		if fixes == 0 {
			result.NeedsManualReview = true
			return result
		}
		log.Printf("[修复代理] 第%d轮：差异=%d，修复=%d", round, len(diffs), fixes)
	}

	classified := a.processor.classifyParagraphs(doc.Paragraphs())
	result.FinalDiffs = len(verifier.compareAllWithSpecs(classified, specs))
	result.NeedsManualReview = result.FinalDiffs > 0
	result.Regressed = result.InitialDiffs >= 0 && result.FinalDiffs > result.InitialDiffs
	return result
}
