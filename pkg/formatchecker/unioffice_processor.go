package formatchecker

import (
	"context"
	"fmt"
)

// ProcessingOptions 处理选项
type ProcessingOptions struct {
	UseTemplate     bool
	TemplateName    string
	EnableNLP       bool
	EnableAutoFix   bool
	QualityLevel    string
	PreserveContent bool
}

// ProcessingResult 处理结果
type ProcessingResult struct {
	Success         bool
	OutputPath      string
	ProcessingTime  int64
	QualityScore    float64
	IssuesFixed     int
	IssuesRemaining int
	DetailedReport  map[string]interface{}
	Recommendations []string
}

// UniOfficeProcessor 基于UniOffice的高精度格式处理器
type UniOfficeProcessor struct {
	standard *FormatStandard
}

// NewUniOfficeProcessor 创建UniOffice处理器
func NewUniOfficeProcessor(standard *FormatStandard) *UniOfficeProcessor {
	return &UniOfficeProcessor{
		standard: standard,
	}
}

// ProcessDocument 处理文档（主入口）
// Deprecated: 已废弃。真实主链路为 pkg/fileprocessor 的 V2 格式修正引擎（v2_engine.go
// applyCorrectionsV2Once），本方法仅保留以兼容旧调用方：内部接线 RuleEngine.FixDocument
// 做真实写入（setter 已激活），不再返回写死的成功桩。
func (p *UniOfficeProcessor) ProcessDocument(ctx context.Context, inputPath string, options ProcessingOptions) (*ProcessingResult, error) {
	engine := NewRuleEngine(p.standard)
	engine.SetDebug(false)
	fixResult, err := engine.FixDocument(ctx, inputPath)
	if err != nil {
		return &ProcessingResult{
			Success:         false,
			OutputPath:      inputPath,
			DetailedReport:  make(map[string]interface{}),
			Recommendations: make([]string, 0),
		}, fmt.Errorf("ProcessDocument: %w", err)
	}
	return &ProcessingResult{
		Success:         fixResult.Success,
		OutputPath:      fixResult.OutputPath,
		ProcessingTime:  int64(fixResult.ProcessingTime.Milliseconds()),
		QualityScore:    fixResult.QualityScore,
		IssuesFixed:     len(fixResult.AppliedFixes),
		IssuesRemaining: len(fixResult.FailedFixes),
		DetailedReport: map[string]interface{}{
			"fix_result": fixResult,
		},
		Recommendations: make([]string, 0),
	}, nil
}
