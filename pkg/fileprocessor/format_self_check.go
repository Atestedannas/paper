package fileprocessor

import (
	"log"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
)

type FormattingSelfCheckResult struct {
	FunctionName string
	Scope        string
	TargetCount  int
	CheckedCount int
	FixesApplied int
}

func (p *EnhancedProcessor) executeFormattingSelfCheck(result FormattingSelfCheckResult) FormattingSelfCheckResult {
	log.Printf("[格式自检] %s scope=%s targets=%d checked=%d fixes=%d",
		result.FunctionName, result.Scope, result.TargetCount, result.CheckedCount, result.FixesApplied)
	if p != nil && p.formatSelfCheckHook != nil {
		p.formatSelfCheckHook(result)
	}
	return result
}

func (p *EnhancedProcessor) runParagraphFormattingSelfCheck(functionName string, paragraphs []document.Paragraph, rules map[string]interface{}) FormattingSelfCheckResult {
	result := FormattingSelfCheckResult{
		FunctionName: functionName,
		Scope:        "paragraphs",
		TargetCount:  len(paragraphs),
	}
	if p == nil {
		return result
	}
	for _, para := range paragraphs {
		if strings.TrimSpace(p.extractParagraphText(para)) == "" {
			continue
		}
		result.CheckedCount++
		result.FixesApplied += p.verifyParagraphFormat(para, rules, functionName)
	}
	return p.executeFormattingSelfCheck(result)
}

func (p *EnhancedProcessor) runSingleParagraphFormattingSelfCheck(functionName string, para document.Paragraph, rules map[string]interface{}) FormattingSelfCheckResult {
	return p.runParagraphFormattingSelfCheck(functionName, []document.Paragraph{para}, rules)
}

func (p *EnhancedProcessor) runDocumentFormattingSelfCheck(functionName string, doc *document.Document) FormattingSelfCheckResult {
	result := FormattingSelfCheckResult{
		FunctionName: functionName,
		Scope:        "document",
	}
	if doc != nil {
		result.TargetCount = len(doc.Paragraphs()) + len(doc.Tables())
		result.CheckedCount = result.TargetCount
	}
	return p.executeFormattingSelfCheck(result)
}
