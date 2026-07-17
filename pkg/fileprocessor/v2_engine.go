package fileprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
	"golang.org/x/sync/singleflight"

	"github.com/paper-format-checker/backend/internal/core/transplant"
	"github.com/paper-format-checker/backend/pkg/docconvert"
	"github.com/paper-format-checker/backend/pkg/formatengine"
)

// v2ApplyDedupe 合并对同一文档 + 相同 corrections 的并发 ApplyCorrectionsV2（避免双请求产生两套审计日志与重复 CPU）。
var v2ApplyDedupe singleflight.Group

func normalizeLegacyFormatterOutput(path string) error {
	_, err := transplant.NormalizeFinalDOCX(path)
	return err
}

func v2ApplyDedupeKey(docPath string, corrections []map[string]interface{}) string {
	abs := docPath
	if a, err := filepath.Abs(docPath); err == nil {
		abs = a
	}
	b, err := json.Marshal(corrections)
	if err != nil {
		b = []byte(`"marshal_error"`)
	}
	return abs + "\x00" + string(b)
}

// V2FormatEngine 第二代格式修正引擎
// 核心原理：模板XML节点克隆，确定性段落分类，零AI调用
type V2FormatEngine struct {
	processor    *EnhancedProcessor
	templatePath string
}

func NewV2FormatEngine(proc *EnhancedProcessor, templatePath string) *V2FormatEngine {
	return &V2FormatEngine{
		processor:    proc,
		templatePath: templatePath,
	}
}

// Process 执行完整的格式修正流程
// 返回输出文件路径
func (e *V2FormatEngine) Process(ctx context.Context, studentDocPath string) (string, error) {
	startTime := time.Now()
	log.Println("========== V2 格式修正引擎 开始 ==========")
	log.Printf("[V2] 模板: %s", e.templatePath)
	log.Printf("[V2] 学生论文: %s", studentDocPath)

	// #region agent log
	debugLog("v2_engine.go:Process", "H1_V2_ENGINE_STARTED", map[string]interface{}{
		"templatePath":   e.templatePath,
		"studentDocPath": studentDocPath,
	})
	// #endregion

	// ── 步骤 1: 打开模板文档 ──
	log.Println("[V2][步骤1] 打开模板文档...")
	templateDoc, err := document.Open(e.templatePath)
	if err != nil {
		return "", fmt.Errorf("无法打开模板文档: %w", err)
	}
	defer templateDoc.Close()
	log.Printf("[V2][步骤1] 模板段落数: %d", len(templateDoc.Paragraphs()))

	// ── 步骤 2: 从模板提取完整XML格式 ──
	log.Println("[V2][步骤2] 提取模板格式（XML完整节点）...")
	store := ExtractTemplateFormats(templateDoc, e.processor)
	if len(store.Formats) == 0 {
		return "", fmt.Errorf("模板格式提取失败：未找到任何格式定义")
	}
	log.Printf("[V2][步骤2] 提取了 %d 种格式类型", len(store.Formats))

	// ── 步骤 3: 打开学生文档 ──
	log.Println("[V2][步骤3] 打开学生文档...")
	studentDoc, err := document.Open(studentDocPath)
	if err != nil {
		return "", fmt.Errorf("无法打开学生文档: %w", err)
	}
	defer studentDoc.Close()
	log.Printf("[V2][步骤3] 学生文档段落数: %d", len(studentDoc.Paragraphs()))

	// ── 步骤 4: 复制模板样式定义到学生文档 ──
	log.Println("[V2][步骤4] 复制样式定义...")
	CloneStyles(templateDoc, studentDoc)

	// ── 步骤 5: 复制页面设置（A4/边距等）──
	log.Println("[V2][步骤5] 复制页面设置...")
	CloneSectionProperties(templateDoc, studentDoc)

	// ── 步骤 5b: 应用 Section 级别高级格式（页眉/页脚/三线表/上标等）──
	log.Println("[V2][步骤5b] 应用高级Section格式...")
	e.processor.ApplySectionLevelFormatting(studentDoc)

	// ── 步骤 6: 确定性段落分类 ──
	log.Println("[V2][步骤6] 确定性段落分类...")
	classifier := NewV2DeterministicClassifier(e.processor)
	classified := classifier.Classify(studentDoc.Paragraphs())

	// ── 步骤 7: XML格式克隆（非封面、非特殊段落）──
	log.Println("[V2][步骤7] XML格式克隆...")
	cloner := NewV2FormatCloner(store)
	fixCount := cloner.ApplyAll(classified)
	log.Printf("[V2][步骤7] 修正了 %d 个段落", fixCount)

	// ── 步骤 7b: 智能格式化（题目/摘要/标题/页眉等特殊段落）──
	log.Println("[V2][步骤7b] 智能格式化...")
	smartFmt := NewV2SmartFormatter(e.processor)
	smartFmt.ApplySmartFormatting(studentDoc, classified)
	smartFmt.ApplyBodyFormats(classified)

	// ── 步骤 8: 表格格式处理 ──
	log.Println("[V2][步骤8] 处理表格格式...")
	e.applyTableFormatFromTemplate(studentDoc, store)

	// ── 步骤 9: 生成差异报告 ──
	log.Println("[V2][步骤9] 生成差异报告...")
	diffReport := e.buildDiffReport(classified, store)
	e.processor.lastDiffReport = diffReport
	log.Printf("[V2][步骤9] 差异报告: 扫描%d段, %d错误, %d警告",
		diffReport.TotalParas, diffReport.ErrorCount, diffReport.WarningCount)

	// ── 步骤 10: 保存输出文件 ──
	outputPath := e.generateOutputPath(studentDocPath)
	log.Printf("[V2][步骤10] 保存到: %s", outputPath)
	if err := studentDoc.SaveToFile(outputPath); err != nil {
		return "", fmt.Errorf("保存文档失败: %w", err)
	}

	elapsed := time.Since(startTime)
	log.Printf("========== V2 格式修正引擎 完成 (耗时 %v, 修正 %d 段) ==========", elapsed, fixCount)
	return outputPath, nil
}

// applyTableFormatFromTemplate 使用模板的正文格式处理表格内文本
func (e *V2FormatEngine) applyTableFormatFromTemplate(doc *document.Document, store *V2TemplateFormatStore) {
	bodyFormat, ok := store.Formats[V2Body]
	if !ok || bodyFormat.RPr == nil {
		return
	}

	tables := doc.Tables()
	fixCount := 0
	for _, tbl := range tables {
		for _, row := range tbl.Rows() {
			for _, cell := range row.Cells() {
				for _, para := range cell.Paragraphs() {
					for _, r := range para.Runs() {
						if strings.TrimSpace(r.Text()) == "" {
							continue
						}
						r.X().RPr = cloneRPr(bodyFormat.RPr)
						fixCount++
					}
				}
			}
		}
	}
	if fixCount > 0 {
		log.Printf("[V2] 表格内修正 %d 个 run", fixCount)
	}
}

// buildDiffReport 构建格式差异报告
func (e *V2FormatEngine) buildDiffReport(classified []V2ClassifiedPara, store *V2TemplateFormatStore) *DocDiffReport {
	report := &DocDiffReport{}

	for _, cp := range classified {
		if cp.Text == "" {
			continue
		}
		report.TotalParas++

		format, ok := store.Formats[cp.Type]
		if !ok {
			continue
		}

		// 比对段落属性
		var diffs []SpecDiff
		currentPPr := cp.Para.X().PPr
		if format.PPr != nil && currentPPr != nil {
			diffs = append(diffs, v2ComparePPr(format.PPr, currentPPr)...)
		}

		// 比对运行属性
		runs := cp.Para.Runs()
		if format.RPr != nil && len(runs) > 0 {
			for _, r := range runs {
				if strings.TrimSpace(r.Text()) == "" {
					continue
				}
				if r.X().RPr != nil {
					diffs = append(diffs, v2CompareRPr(format.RPr, r.X().RPr)...)
				}
				break
			}
		}

		if len(diffs) > 0 {
			preview := []rune(cp.Text)
			if len(preview) > 30 {
				preview = preview[:30]
			}
			report.ParaDiffs = append(report.ParaDiffs, ParaDiff{
				ParaIndex: cp.ParaIdx,
				Category:  cp.Type,
				Text:      string(preview) + "...",
				Diffs:     diffs,
			})
			for _, d := range diffs {
				if d.Severity == "error" {
					report.ErrorCount++
				} else {
					report.WarningCount++
				}
			}
		}
	}
	return report
}

// v2ComparePPr 比对段落属性
func v2ComparePPr(expected, actual *wml.CT_PPr) []SpecDiff {
	var diffs []SpecDiff

	if expected.Jc != nil && actual.Jc != nil {
		if expected.Jc.ValAttr != actual.Jc.ValAttr {
			diffs = append(diffs, SpecDiff{
				Field:    "对齐方式",
				Expected: expected.Jc.ValAttr.String(),
				Actual:   actual.Jc.ValAttr.String(),
				Severity: "error",
			})
		}
	}
	return diffs
}

// v2CompareRPr 比对运行属性
func v2CompareRPr(expected, actual *wml.CT_RPr) []SpecDiff {
	var diffs []SpecDiff

	if expected.Sz != nil && actual.Sz != nil {
		if expected.Sz.ValAttr.ST_UnsignedDecimalNumber != nil &&
			actual.Sz.ValAttr.ST_UnsignedDecimalNumber != nil &&
			*expected.Sz.ValAttr.ST_UnsignedDecimalNumber != *actual.Sz.ValAttr.ST_UnsignedDecimalNumber {
			diffs = append(diffs, SpecDiff{
				Field:    "字号",
				Expected: fmt.Sprintf("%.1fpt", float64(*expected.Sz.ValAttr.ST_UnsignedDecimalNumber)/2),
				Actual:   fmt.Sprintf("%.1fpt", float64(*actual.Sz.ValAttr.ST_UnsignedDecimalNumber)/2),
				Severity: "error",
			})
		}
	}

	expectedBold := expected.B != nil
	actualBold := actual.B != nil
	if expectedBold != actualBold {
		diffs = append(diffs, SpecDiff{
			Field:    "加粗",
			Expected: fmt.Sprintf("%v", expectedBold),
			Actual:   fmt.Sprintf("%v", actualBold),
			Severity: "warning",
		})
	}

	return diffs
}

func (e *V2FormatEngine) generateOutputPath(inputPath string) string {
	dir := filepath.Dir(inputPath)
	ext := filepath.Ext(inputPath)
	base := strings.TrimSuffix(filepath.Base(inputPath), ext)

	outputDir := filepath.Join(dir, "corrected")
	os.MkdirAll(outputDir, 0755)

	return filepath.Join(outputDir, base+"_v2_corrected"+ext)
}

// isPipelineTemplateArtifact 排除落在 uploads 目录下的中间产物，避免误当「学校官方模板」。
func isPipelineTemplateArtifact(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(base, "~$") {
		return true
	}
	for _, suf := range []string{
		"_styled.docx", "_styled.doc",
		"_v2_corrected.docx", "_v2_corrected.doc",
		"_parity.docx",
	} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	return false
}

// findTemplateFile 按目录/通配优先级搜索；同一通配内取体积最大者（避开极小占位文件）。
// 历史上曾在全部匹配里取全局最大，会把 uploads/templates 下的 *_styled.docx 误选为模板。
func findTemplateFile() string {
	patterns := []string{
		"uploads/golden_templates/*_real.docx",
		"uploads/golden_templates/*_prepared.docx",
		"uploads/golden_templates/*.docx",
		"uploads/golden_templates/*.doc",
		"uploads/templates/*.docx",
		"uploads/templates/*.doc",
	}

	const minBytes = int64(10000)

	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		var bestPath string
		var bestSize int64
		for _, m := range matches {
			if isPipelineTemplateArtifact(m) {
				continue
			}
			info, err := os.Stat(m)
			if err != nil || info.Size() <= minBytes {
				continue
			}
			if info.Size() > bestSize {
				bestSize = info.Size()
				bestPath = m
			}
		}
		if bestPath != "" {
			log.Printf("[V2模板搜索] 选中模板: %s (%.0fKB)", bestPath, float64(bestSize)/1024)
			return bestPath
		}
	}
	return ""
}

const cqHRNormativeMarker = "附件5"

// findCQHRNormativeAttachment5Template 在 uploads/templates 下查找文件名含「附件5」的规范稿（重庆人文毕业论文格式模板）。
// 优先 .docx，其次 .doc；同扩展名取体积最大者。与 isPipelineTemplateArtifact 配合排除 *_styled 等中间文件。
func findCQHRNormativeAttachment5Template() string {
	const dir = "uploads/templates"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var docxCand, docCand []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.Contains(name, cqHRNormativeMarker) {
			continue
		}
		full := filepath.Join(dir, name)
		if isPipelineTemplateArtifact(full) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".docx":
			docxCand = append(docxCand, full)
		case ".doc":
			docCand = append(docCand, full)
		}
	}
	pickLargest := func(paths []string) string {
		const minBytes = int64(10000)
		var best string
		var bestSize int64
		for _, path := range paths {
			st, err := os.Stat(path)
			if err != nil || st.Size() <= minBytes {
				continue
			}
			if st.Size() > bestSize {
				bestSize = st.Size()
				best = path
			}
		}
		return best
	}
	if p := pickLargest(docxCand); p != "" {
		log.Printf("[V2模板搜索] 重庆人文「附件5」规范模板(docx): %s", p)
		return p
	}
	if p := pickLargest(docCand); p != "" {
		log.Printf("[V2模板搜索] 重庆人文「附件5」规范模板(doc): %s", p)
		return p
	}
	return ""
}

// logPaperFormatAudit 与 Python [PAPER_FORMAT_AUDIT] 同前缀；控制台 + 默认写入 logs/paper_format_audit.log。
func logPaperFormatAudit(phase, action string, fields map[string]interface{}) {
	line, err := formatAuditJSONLine(phase, action, fields)
	if err != nil {
		log.Printf("[PAPER_FORMAT_AUDIT] {\"phase\":%q,\"action\":%q,\"marshal_error\":true}", phase, action)
		return
	}
	log.Printf("[PAPER_FORMAT_AUDIT] %s", line)
	writeFormatAuditLogFile(line)
}

// ── 集成到 EnhancedProcessor ──

// ApplyCorrectionsV2 V2版格式修正入口
// 可选 formatengine.UseWordZero → StyleFormatter (python-docx) → V2FormatEngine (unioffice) 回退链
func (p *EnhancedProcessor) ApplyCorrectionsV2(ctx context.Context, docPath string, corrections []map[string]interface{}) (string, error) {
	v := strings.TrimSpace(os.Getenv("PAPER_V2_APPLY_DEDUPE"))
	if v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "false") {
		return p.applyCorrectionsV2Once(ctx, docPath, corrections)
	}

	key := v2ApplyDedupeKey(docPath, corrections)
	res, err, shared := v2ApplyDedupe.Do(key, func() (interface{}, error) {
		return p.applyCorrectionsV2Once(ctx, docPath, corrections)
	})
	if err != nil {
		return "", err
	}
	if shared {
		log.Printf("[V2入口] 与进行中的 ApplyCorrectionsV2 合并（同文档同参数），避免重复执行: %s", filepath.Base(docPath))
	}
	return res.(string), nil
}

func (p *EnhancedProcessor) applyCorrectionsV2Once(ctx context.Context, docPath string, corrections []map[string]interface{}) (string, error) {
	log.Println("================= V2 格式修正流程 开始 =================")
	log.Printf("[V2入口] 文件: %s", docPath)

	fileExt := strings.ToLower(filepath.Ext(docPath))
	if fileExt != ".docx" && fileExt != ".doc" {
		return p.handleUnsupportedFormat(docPath)
	}

	// 查找模板路径（验证路径存在后才使用）

	templatePath := p.resolveTemplatePath(corrections)

	if templatePath == "" {
		log.Println("[V2入口] 未找到模板，回退到旧版方案")
		return p.ApplyCorrections(ctx, docPath, corrections)
	}

	log.Printf("[V2入口] 使用模板: %s", templatePath)
	logPaperFormatAudit("GoV2", "apply_start", map[string]interface{}{
		"doc_path":      docPath,
		"template_path": templatePath,
		"shell_enabled": false,
	})
	finalizeOutput := func(primaryEngine, outPath string) (string, error) {
		finalPath, finalEngine, err := p.enforceStrongFormatConsistency(ctx, docPath, outPath, templatePath, corrections, primaryEngine)
		if err != nil {
			return "", err
		}
		if _, err := transplant.NormalizeFinalDOCX(finalPath); err != nil {
			return "", fmt.Errorf("normalize final docx: %w", err)
		}
		logPaperFormatAudit("GoV2", "path_chosen", map[string]interface{}{
			"engine":      finalEngine,
			"output_path": finalPath,
		})
		return finalPath, nil
	}

	//TemplateShellFillEnabled  false
	// ── 可选：模板壳就地换字（USE_TEMPLATE_SHELL_FILL=1），保留模板 w:pPr / 节与页眉页脚 ──
	if TemplateShellFillEnabled() {

		log.Println("[V2入口] USE_TEMPLATE_SHELL_FILL 已启用，尝试 ShellInPlace 填充...")
		if outPath, err := p.RunTemplateShellInPlaceFill(ctx, docPath, templatePath, corrections); err == nil {
			acceptShell := true
			if ShellFillPostValidateEnabled() {
				ok100, repPath, vErr := p.RunShellPostValidate(ctx, outPath, templatePath, corrections)
				if vErr != nil {
					log.Printf("[V2入口] Shell 后校验异常: %v（报告: %s）", vErr, repPath)
					if ShellFillFallbackOnValidateFail() {
						acceptShell = false
					}
				} else if !ok100 {
					log.Printf("[V2入口] Shell 产物未过验收 compliance_100=false（报告: %s）", repPath)
					if ShellFillFallbackOnValidateFail() {
						acceptShell = false
					}
				} else {
					log.Printf("[V2入口] Shell 后验收通过 compliance_100=true（报告: %s）", repPath)
				}
			}
			if acceptShell {
				finalPath, finErr := finalizeOutput("ShellInPlace", outPath)
				if finErr != nil {
					return "", finErr
				}
				log.Println("================= V2 格式修正流程 完成 (ShellInPlace) =================")
				return finalPath, nil
			}
			logPaperFormatAudit("GoV2", "shell_discarded", map[string]interface{}{
				"reason": "post_validate_failed_or_error_with_fallback",
			})
			log.Println("[V2入口] Shell 结果被丢弃，改用 StyleFormatter（后校验未通过或脚本失败且已启用回退）")
		} else {
			log.Printf("[V2入口] ShellInPlace 失败: %v，继续 StyleFormatter / 回退", err)
		}
	}

	// ── 新主路径：模板驱动重建（WordZero 读取模板样式 + TemplateFiller 重建正文）──
	if TemplateDrivenRebuildEnabled() {
		log.Println("[V2入口] USE_TEMPLATE_DRIVEN_REBUILD=true，尝试模板驱动重建...")
		outPath, rebuildErr := p.RunTemplateDrivenRebuild(ctx, docPath, templatePath, corrections)
		if rebuildErr == nil {
			finalPath, finErr := finalizeOutput("TemplateDrivenRebuild", outPath)
			if finErr != nil {
				return "", finErr
			}
			log.Println("================= V2 格式修正流程 完成 (TemplateDrivenRebuild) =================")
			return finalPath, nil
		}
		log.Printf("[V2入口] 模板驱动重建失败: %v，继续尝试 WordZero / StyleFormatter / V2FormatEngine", rebuildErr)
	}

	// ── 编译期开关：pkg/formatengine.UseWordZero（见该包注释）──
	if formatengine.UseWordZero {
		log.Println("[V2入口] formatengine.UseWordZero=true，尝试 WordZero...")
		outPath, wzErr := p.RunWordZeroFormatter(ctx, docPath, templatePath)
		if wzErr == nil {
			finalPath, finErr := finalizeOutput("WordZero", outPath)
			if finErr != nil {
				return "", finErr
			}
			log.Println("================= V2 格式修正流程 完成 (WordZero) =================")
			return finalPath, nil
		}
		log.Printf("[V2入口] WordZero 失败: %v，回退 StyleFormatter / V2FormatEngine", wzErr)
	}

	// ── 优先尝试 StyleFormatter (python-docx 引擎) ──
	sfConfig := DefaultStyleFormatterConfig()

	if sfConfig.Enabled && sfConfig.ScriptPath != "" {

		log.Println("[V2入口] 尝试 StyleFormatter (python-docx 样式引用引擎)...")
		outputPath, err := p.RunStyleFormatter(ctx, docPath, templatePath, sfConfig, corrections)
		if err == nil {
			finalPath, finErr := finalizeOutput("StyleFormatter", outputPath)
			if finErr != nil {
				return "", finErr
			}
			log.Println("================= V2 格式修正流程 完成 (StyleFormatter) =================")
			return finalPath, nil
		}
		log.Printf("[V2入口] StyleFormatter 失败: %v, 回退到 V2FormatEngine", err)
	}

	// ── 回退: V2FormatEngine (unioffice XML 克隆) ──
	log.Println("[V2入口] 使用 V2FormatEngine (unioffice) 回退方案...")
	engine := NewV2FormatEngine(p, templatePath)
	outputPath, err := engine.Process(ctx, docPath)
	if err != nil {
		log.Printf("[V2入口] V2FormatEngine 也失败: %v, 回退到旧版", err)
		return p.ApplyCorrections(ctx, docPath, corrections)
	}

	finalPath, finErr := finalizeOutput("V2FormatEngine", outputPath)
	if finErr != nil {
		return "", finErr
	}
	return finalPath, nil
}

func strongVerificationEnabled() bool {
	v := strings.TrimSpace(os.Getenv("FORMAT_STRONG_VERIFY_ENABLED"))
	if v == "" {
		return true
	}
	return !(v == "0" || strings.EqualFold(v, "false") || strings.EqualFold(v, "off"))
}

func strongVerificationThreshold() int {
	v := strings.TrimSpace(os.Getenv("FORMAT_STRONG_VERIFY_MAX_DIFFS"))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func strongVerificationStrictMode() bool {
	v := strings.TrimSpace(os.Getenv("FORMAT_STRONG_VERIFY_STRICT"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func planStrongVerificationAction(initialDiffs, retryDiffs, threshold int) (shouldRetry bool, shouldFallback bool) {
	if initialDiffs <= threshold {
		return false, false
	}
	if retryDiffs < 0 {
		return true, false
	}
	return true, retryDiffs > threshold
}

func (p *EnhancedProcessor) countTemplateSpecDiffs(docPath, templatePath string) (int, error) {
	loader := NewTemplateFormatLoader(p)
	specs, err := loader.LoadFromFile(templatePath)
	if err != nil {
		return 0, err
	}
	if len(specs) == 0 {
		return 0, fmt.Errorf("no template specs loaded")
	}
	doc, err := document.Open(docPath)
	if err != nil {
		return 0, err
	}
	defer doc.Close()
	verifier := NewFormatVerifier(p, nil)
	classified := p.classifyParagraphs(doc.Paragraphs())
	diffs := verifier.compareAllWithSpecs(classified, specs)
	return len(diffs), nil
}

func (p *EnhancedProcessor) enforceStrongFormatConsistency(
	ctx context.Context,
	sourceDocPath string,
	candidatePath string,
	templatePath string,
	corrections []map[string]interface{},
	primaryEngine string,
) (string, string, error) {
	p.lastStrongVerify = &StrongVerifyResult{
		Enabled:      strongVerificationEnabled(),
		Threshold:    strongVerificationThreshold(),
		InitialDiffs: -1,
		RetryDiffs:   -1,
		FinalDiffs:   -1,
		Retried:      false,
		FallbackUsed: false,
		Passed:       false,
		FinalEngine:  primaryEngine,
	}
	if !strongVerificationEnabled() {
		return candidatePath, primaryEngine, nil
	}
	threshold := strongVerificationThreshold()
	p.lastStrongVerify.Enabled = true
	p.lastStrongVerify.Threshold = threshold
	initialDiffs, err := p.countTemplateSpecDiffs(candidatePath, templatePath)
	if err != nil {
		log.Printf("[强校验] 首次比对失败，保留主路径产物: %v", err)
		return candidatePath, primaryEngine, nil
	}
	p.lastStrongVerify.InitialDiffs = initialDiffs
	retry, _ := planStrongVerificationAction(initialDiffs, -1, threshold)
	log.Printf("[强校验] 引擎=%s 首次差异=%d 阈值=%d", primaryEngine, initialDiffs, threshold)
	if !retry {
		p.lastStrongVerify.FinalDiffs = initialDiffs
		p.lastStrongVerify.Passed = initialDiffs <= threshold
		p.lastStrongVerify.FinalEngine = primaryEngine
		return candidatePath, primaryEngine, nil
	}
	p.lastStrongVerify.Retried = true

	doc, openErr := document.Open(candidatePath)
	if openErr != nil {
		log.Printf("[强校验] 重试前打开失败，进入V2回退: %v", openErr)
		return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, initialDiffs, threshold)
	}
	defer doc.Close()
	loader := NewTemplateFormatLoader(p)
	specs, loadErr := loader.LoadFromFile(templatePath)
	if loadErr != nil || len(specs) == 0 {
		log.Printf("[强校验] 无法加载模板规范，进入V2回退: %v", loadErr)
		return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, initialDiffs, threshold)
	}

	verifier := NewFormatVerifier(p, nil)
	fixes := verifier.VerifyAndFixWithSpecs(doc, specs)
	if fixes > 0 {
		if saveErr := doc.SaveToFile(candidatePath); saveErr != nil {
			log.Printf("[强校验] 重试保存失败，进入V2回退: %v", saveErr)
			return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, initialDiffs, threshold)
		}
	}

	retryDiffs, recountErr := p.countTemplateSpecDiffs(candidatePath, templatePath)
	if recountErr != nil {
		log.Printf("[强校验] 重试后复核失败，进入V2回退: %v", recountErr)
		return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, initialDiffs, threshold)
	}
	p.lastStrongVerify.RetryDiffs = retryDiffs
	_, fallback := planStrongVerificationAction(initialDiffs, retryDiffs, threshold)
	log.Printf("[强校验] 重试修正=%d 重试后差异=%d 阈值=%d", fixes, retryDiffs, threshold)
	if !fallback {
		finalEngine := primaryEngine + "+StrongVerifyRetry"
		p.lastStrongVerify.FinalDiffs = retryDiffs
		p.lastStrongVerify.Passed = retryDiffs <= threshold
		p.lastStrongVerify.FinalEngine = finalEngine
		return candidatePath, finalEngine, nil
	}
	return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, retryDiffs, threshold)
}

func (p *EnhancedProcessor) fallbackToV2Engine(
	ctx context.Context,
	sourceDocPath string,
	templatePath string,
	_ []map[string]interface{},
	currentDiffs int,
	threshold int,
) (string, string, error) {
	log.Printf("[强校验] 差异仍超阈值(%d>%d)，回退 V2FormatEngine 重建", currentDiffs, threshold)
	if p.lastStrongVerify != nil {
		p.lastStrongVerify.FallbackUsed = true
		p.lastStrongVerify.FinalEngine = "V2FormatEngineFallback"
	}
	engine := NewV2FormatEngine(p, templatePath)
	outPath, err := engine.Process(ctx, sourceDocPath)
	if err != nil {
		return "", "", fmt.Errorf("strong verify fallback failed: %w", err)
	}
	finalDiffs, diffErr := p.countTemplateSpecDiffs(outPath, templatePath)
	if diffErr == nil {
		if p.lastStrongVerify != nil {
			p.lastStrongVerify.FinalDiffs = finalDiffs
			p.lastStrongVerify.Passed = finalDiffs <= threshold
		}
		if strongVerificationStrictMode() && finalDiffs > threshold {
			return "", "", fmt.Errorf("strong verify strict mode: final diffs %d exceed threshold %d", finalDiffs, threshold)
		}
	} else {
		log.Printf("[强校验] 回退后复核失败: %v", diffErr)
		if strongVerificationStrictMode() {
			return "", "", fmt.Errorf("strong verify strict mode: failed to verify fallback output: %w", diffErr)
		}
	}
	return outPath, "V2FormatEngineFallback", nil
}

// resolveTemplatePath searches for a valid golden template path from multiple sources.
// 优先级：① corrections.template_path（单次请求显式指定）② 重庆人文 + uploads/templates 下「附件5」规范稿
// ③ SetTemplatePath / DB golden_template_path ④ 自动扫描 golden_templates 与 templates
func (p *EnhancedProcessor) resolveTemplatePath(corrections []map[string]interface{}) string {
	var templatePath string

	if templatePath = getStringFromCorrectionsList(corrections, "template_path"); templatePath != "" {
		if _, err := os.Stat(templatePath); os.IsNotExist(err) {
			log.Printf("[V2入口] corrections.template_path 不存在: %s", templatePath)
			templatePath = ""
		}
	}
	if templatePath == "" && os.Getenv("DISABLE_CQHR_ATTACHMENT5_PRIORITY") == "" {
		if getSchoolIDFromCorrectionsList(corrections) == "cq-hr-university" {
			if att := findCQHRNormativeAttachment5Template(); att != "" {
				templatePath = att
			}
		}
	}
	if templatePath == "" {
		templatePath = p.templatePath
		if templatePath != "" {
			if _, err := os.Stat(templatePath); os.IsNotExist(err) {
				log.Printf("[V2入口] 预设模板路径不存在: %s, 重新搜索", templatePath)
				templatePath = ""
			}
		}
	}
	if templatePath == "" {
		templatePath = findTemplateFile()
	}

	if templatePath == "" {
		return ""
	}

	// Convert .doc template to .docx if needed
	if strings.ToLower(filepath.Ext(templatePath)) == ".doc" {
		docxPath := strings.TrimSuffix(templatePath, filepath.Ext(templatePath)) + ".docx"
		if _, err := os.Stat(docxPath); os.IsNotExist(err) {
			log.Printf("[V2入口] 转换模板 .doc → .docx: %s", templatePath)
			converted, convErr := docconvert.ConvertDocToDocx(templatePath, false)
			if convErr != nil {
				log.Printf("[V2入口] 模板转换失败: %v", convErr)
				return ""
			}
			templatePath = converted
		} else {
			templatePath = docxPath
		}
	}

	return templatePath
}
