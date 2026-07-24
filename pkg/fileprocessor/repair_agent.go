package fileprocessor

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
)

type RepairResult struct {
	Rounds            int
	TotalFixes        int
	InitialDiffs      int
	FinalDiffs        int
	NeedsManualReview bool
	Regressed         bool
	Diagnostics       []string
}

type RepairDiagnosticClient interface {
	ChatCompletion(prompt string) (string, error)
}

type RepairAgent struct {
	processor       *EnhancedProcessor
	maxRounds       int
	locks           *FormatLockManager
	diagnosticAgent *DiagnosticRepairAgent
}

func NewRepairAgent(processor *EnhancedProcessor, maxRounds int, clients ...RepairDiagnosticClient) *RepairAgent {
	if maxRounds < 1 {
		maxRounds = 1
	}
	var client RepairDiagnosticClient
	if len(clients) > 0 {
		client = clients[0]
	}
	return &RepairAgent{
		processor:       processor,
		maxRounds:       maxRounds,
		locks:           NewFormatLockManager(),
		diagnosticAgent: &DiagnosticRepairAgent{client: client},
	}
}

func (a *RepairAgent) WithLocks(locks *FormatLockManager) *RepairAgent {
	if locks != nil {
		a.locks = locks
	}
	return a
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
		a.lockVerifiedTypes(classified, specs, diffs)
		diffs = a.unlockedDiffs(classified, diffs)
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
			result.Diagnostics = a.diagnosticAgent.Diagnose(round, diffs)
			return result
		}

		result.Rounds = round
		fixes := verifier.autoFixDiffsWithSpecs(classified, diffs, specs, applier)
		result.TotalFixes += fixes
		previousDiffs = len(diffs)
		if fixes == 0 {
			result.NeedsManualReview = true
			result.Diagnostics = a.diagnosticAgent.Diagnose(round, diffs)
			return result
		}
		log.Printf("[修复代理] 第%d轮：差异=%d，修复=%d", round, len(diffs), fixes)
	}

	classified := a.processor.classifyParagraphs(doc.Paragraphs())
	result.FinalDiffs = len(verifier.compareAllWithSpecs(classified, specs))
	result.NeedsManualReview = result.FinalDiffs > 0
	result.Regressed = result.InitialDiffs >= 0 && result.FinalDiffs > result.InitialDiffs
	if result.NeedsManualReview {
		result.Diagnostics = a.diagnosticAgent.Diagnose(a.maxRounds, verifier.compareAllWithSpecs(classified, specs))
	}
	return result
}

func (a *RepairAgent) lockVerifiedTypes(classified map[string][]document.Paragraph, specs map[string]ParagraphFormatSpec, diffs []FormatDiff) {
	failed := map[string]bool{}
	for _, diff := range diffs {
		failed[diff.Category] = true
		a.locks.Unlock(diff.Category)
	}
	for paragraphType := range specs {
		if paragraphs := classified[paragraphType]; len(paragraphs) > 0 && !failed[paragraphType] {
			a.locks.Lock(paragraphType, len(paragraphs))
		}
	}
}

func (a *RepairAgent) unlockedDiffs(classified map[string][]document.Paragraph, diffs []FormatDiff) []FormatDiff {
	result := diffs[:0]
	for _, diff := range diffs {
		if !a.locks.IsLocked(diff.Category, len(classified[diff.Category])) {
			result = append(result, diff)
		}
	}
	return result
}

type DiagnosticRepairAgent struct {
	client RepairDiagnosticClient
}

// Diagnose 只分析失败原因并返回审计文本，不接触 document 或 OOXML。
func (a *DiagnosticRepairAgent) Diagnose(round int, diffs []FormatDiff) []string {
	if a == nil || a.client == nil || len(diffs) == 0 {
		return nil
	}
	payload, err := json.Marshal(diffs)
	if err != nil {
		return nil
	}
	response, err := a.client.ChatCompletion(fmt.Sprintf(`你是DOCX格式修复诊断器。第%d轮确定性修复后仍有以下差异：
%s
只分析根因和下一步应由确定性Go执行器修复的属性，不输出OOXML，不修改文本内容。用不超过5条短句回答。`, round, payload))
	if err != nil {
		log.Printf("[诊断修复代理] AI诊断失败: %v", err)
		return nil
	}
	response = strings.TrimSpace(response)
	if response == "" {
		return nil
	}
	log.Printf("[诊断修复代理] %s", response)
	return []string{response}
}

func (p *EnhancedProcessor) repairDiagnosticClient() RepairDiagnosticClient {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("FORMAT_REPAIR_AI_DIAGNOSTICS")), "true") &&
		strings.TrimSpace(os.Getenv("FORMAT_REPAIR_AI_DIAGNOSTICS")) != "1" {
		return nil
	}
	if p.smartClassifier == nil {
		return nil
	}
	return p.smartClassifier.GetDeepSeekClient()
}
