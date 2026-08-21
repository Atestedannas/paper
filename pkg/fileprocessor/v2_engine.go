package fileprocessor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
	"golang.org/x/sync/singleflight"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/templateapply"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
	"github.com/paper-format-checker/backend/internal/core/transplant"
)

// v2ApplyDedupe 合并对同一文档 + 相同 corrections 的并发 ApplyCorrectionsV2（避免双请求产生两套审计日志与重复 CPU）。
var v2ApplyDedupe singleflight.Group

func normalizeLegacyFormatterOutput(path string) error {
	_, err := transplant.NormalizeFinalDOCX(path)
	return err
}

func logOutputParagraphCount(stage, path string) {
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		log.Printf("[内容保全] 阶段=%s 无法读取文档: %v", stage, err)
		return
	}
	documentXML, ok := pkg.Get("word/document.xml")
	if !ok {
		log.Printf("[内容保全] 阶段=%s 缺少 word/document.xml", stage)
		return
	}
	log.Printf("[内容保全] 阶段=%s 段落数=%d", stage, countDocxParagraphs(string(documentXML)))
	emptyAttributes := map[string]bool{}
	for _, match := range regexp.MustCompile(`\b([A-Za-z_:][A-Za-z0-9_.:-]*)=""`).FindAllSubmatch(documentXML, -1) {
		emptyAttributes[string(match[1])] = true
	}
	if len(emptyAttributes) > 0 {
		log.Printf("[OOXML属性] 阶段=%s 空值属性=%v", stage, emptyAttributes)
	}
	logGoldenStageFormatDrift(stage, path)
}

func logGoldenStageFormatDrift(stage, candidatePath string) {
	goldenPath := strings.TrimSpace(os.Getenv("PAPER_GOLDEN_DOCX"))
	if goldenPath == "" {
		return
	}
	golden, err := document.Open(goldenPath)
	if err != nil {
		log.Printf("[黄金回归] 阶段=%s 无法读取黄金稿: %v", stage, err)
		return
	}
	defer golden.Close()
	candidate, err := document.Open(candidatePath)
	if err != nil {
		log.Printf("[黄金回归] 阶段=%s 无法读取候选稿: %v", stage, err)
		return
	}
	defer candidate.Close()
	goldenParagraphs, candidateParagraphs := golden.Paragraphs(), candidate.Paragraphs()
	if len(goldenParagraphs) != len(candidateParagraphs) {
		log.Printf("[黄金回归] 阶段=%s 段落数=%d/%d", stage, len(goldenParagraphs), len(candidateParagraphs))
		return
	}
	diffs := 0
	for index := range goldenParagraphs {
		if extractParaFormatSpec(goldenParagraphs[index]) != extractParaFormatSpec(candidateParagraphs[index]) {
			diffs++
		}
	}
	log.Printf("[黄金回归] 阶段=%s 直接格式差异段=%d", stage, diffs)
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
	runLog := formatLogFromContext(ctx)
	if e.processor != nil {
		e.processor.templatePath = e.templatePath
		e.processor.formatLocks = NewFormatLockManager()
	}
	log.Println("========== V2 格式修正引擎 开始 ==========")
	log.Printf("[V2] 模板: %s", e.templatePath)
	log.Printf("[V2] 学生论文: %s", studentDocPath)

	if diagnosticPath := strings.TrimSpace(os.Getenv("PAPER_DIAG_LOG_PATH")); diagnosticPath != "" {
		if err := InitDiagLog(diagnosticPath); err != nil {
			log.Printf("[V2] unable to initialize optional diagnostic log: %v", err)
		} else {
			defer CloseDiagLog()
		}
	}

	// #region agent log
	debugLog("v2_engine.go:Process", "H1_V2_ENGINE_STARTED", map[string]interface{}{
		"templatePath":   e.templatePath,
		"studentDocPath": studentDocPath,
	})
	// #endregion

	// ── 步骤 1: 打开模板文档 ──
	log.Println("[V2][步骤1] 打开模板文档...")
	runLog.section("第1步：打开学校模板并提取格式规范")
	runLog.printf("Extract(templatePath) 输入：%s", e.templatePath)
	templateDoc, err := document.Open(e.templatePath)
	if err != nil {
		runLog.printf("打开模板失败：%v", err)
		return "", fmt.Errorf("无法打开模板文档: %w", err)
	}
	defer templateDoc.Close()
	log.Printf("[V2][步骤1] 模板段落数: %d", len(templateDoc.Paragraphs()))
	runLog.printf("模板打开成功：段落=%d，表格=%d", len(templateDoc.Paragraphs()), len(templateDoc.Tables()))
	if e.processor != nil {
		profile, profileErr := templateprofile.Extract(e.templatePath)
		if profileErr != nil {
			log.Printf("[V2][步骤1] 模板分区格式提取失败，分类回退到语义规则: %v", profileErr)
			runLog.printf("Profile 提取失败：%v", profileErr)
		} else {
			e.processor.templateProfile = profile
			runLog.profile(profile, "第1步结果：学校模板 Profile（可读值）")
		}
	}

	// ── 步骤 2: 从模板提取完整XML格式 ──
	log.Println("[V2][步骤2] 提取模板格式（XML完整节点）...")
	store := ExtractTemplateFormats(templateDoc, e.processor)
	if len(store.Formats) == 0 {
		runLog.printf("模板采样失败：未找到任何具有格式的段落。")
		return "", fmt.Errorf("模板格式提取失败：未找到任何格式定义")
	}
	log.Printf("[V2][步骤2] 提取了 %d 种格式类型", len(store.Formats))
	runLog.templateSamples(store)

	// ── 步骤 3: 打开学生文档 ──
	log.Println("[V2][步骤3] 打开学生文档...")
	studentDoc, err := document.Open(studentDocPath)
	if err != nil {
		return "", fmt.Errorf("无法打开学生文档: %w", err)
	}
	defer studentDoc.Close()
	log.Printf("[V2][步骤3] 学生文档段落数: %d", len(studentDoc.Paragraphs()))
	runLog.section("第3步：打开学生论文")
	runLog.printf("学生论文打开成功：段落=%d，表格=%d", len(studentDoc.Paragraphs()), len(studentDoc.Tables()))

	// ── 步骤 4: 复制模板样式定义到学生文档 ──
	log.Println("[V2][步骤4] 复制样式定义...")
	styleSummary := CloneStyles(templateDoc, studentDoc)
	runLog.section("第4步：复制模板样式定义")
	runLog.printf("DocDefaults 默认样式已复制=%s；命名样式共复制=%d（覆盖学生已有=%d，新增=%d）",
		yesNo(styleSummary.DocDefaultsCopied), styleSummary.NamedStylesCopied, styleSummary.Overwritten, styleSummary.Added)
	runLog.printf("复制的样式 ID：%s", strings.Join(styleSummary.StyleIDs, "、"))

	// ── 步骤 5: 复制页面设置（A4/边距等）──
	log.Println("[V2][步骤5] 复制页面设置...")
	sectionSummary := CloneSectionProperties(templateDoc, studentDoc)
	runLog.section("第5步：复制页面设置")
	runLog.printf("纸张大小已复制=%s；页边距已复制=%s；保留学生原页眉页脚引用=%d 个",
		yesNo(sectionSummary.PageSizeCopied), yesNo(sectionSummary.PageMarginsCopied),
		sectionSummary.PreservedHeaderFooterReferences)
	if e.processor != nil && e.processor.templateProfile != nil {
		runLog.printf("复制目标的页面精确值：%s", humanPageSetup(e.processor.templateProfile.PageSetup))
	}

	// ── 步骤 5b: 应用 Section 级别高级格式（页眉/页脚/三线表/上标等）──
	log.Println("[V2][步骤5b] 应用高级Section格式...")
	runLog.section("第5b步：应用节级别高级格式")
	if err := e.processor.ApplyTemplateSectionLevelFormatting(studentDoc); err != nil {
		log.Printf("[V2][步骤5b] 警告: Section 格式部分失败: %v", err)
		runLog.printf("结果：部分失败；%v", err)
	} else {
		runLog.printf("结果：成功。")
	}
	tableCount, sectionBreakCount, citationCount := sectionLevelStats(studentDoc)
	runLog.printf("实际处理结果：三线表重写=%d 个；当前段落级分节符=%d 个；识别并设为上标的引用/注释 run=%d 个。",
		tableCount, sectionBreakCount, citationCount)
	runLog.printf("页眉页脚策略：此步骤不生成硬编码页眉页脚，最终从学校模板部件复制。")

	// ── 步骤 6: 确定性段落分类 ──
	log.Println("[V2][步骤6] 确定性段落分类...")
	classifier := NewV2DeterministicClassifier(e.processor)
	classified := classifier.Classify(BodyLevelParagraphsOnly(studentDoc))
	runLog.classifications(classified)

	// ── 步骤 7: XML格式克隆（非封面、非特殊段落）──
	// 职责：复制模板段落的完整 XML 节点（含 pPr + rPr），处理非格式属性
	//       （如大纲级别、列表编号、修订标记等 cloneRPr/clonePPr 无法覆盖的深层结构）。
	// 注意：步骤 7b 会在克隆基础上对标题/正文等做专项格式覆写——这是有意设计：
	//       ① 克隆保证"不低于模板"的基准；② 智能格式化按学校规范做精确调整。
	//       两步骤职责不重叠：克隆管结构完整性，智能格式化管语义精确性。
	log.Println("[V2][步骤7] XML格式克隆...")
	fixCount := 0
	log.Println("[V2][步骤7] 已停用旧全局段落克隆，使用分区规则映射")

	// ── 步骤 7b: 智能格式化（题目/摘要/标题/页眉等特殊段落）──
	// 职责：对标题、摘要、正文等特殊段落按学校规范做精确格式调整。
	//       内置跳过逻辑：若段落当前格式已与目标一致，则跳过写入，避免
	//       对步骤 7 已正确克隆的段落做无意义覆盖。
	// 🔒 LOCKED: 标题格式全部从模板提取 — headingSpecs 从 FormatRuleEngine 传入，不硬编码
	log.Println("[V2][步骤7b] 智能格式化...")
	var headingSpecs map[string]ParagraphFormatSpec
	var bodySpec, refSpec *ParagraphFormatSpec
	var coverTitleSpec, abstractTitleSpec, abstractContentSpec, keywordsSpec *ParagraphFormatSpec
	var coverFieldSpec *ParagraphFormatSpec
	var enAbstractTitleSpec, enAbstractContentSpec, enKeywordsSpec *ParagraphFormatSpec
	var tocTitleSpec, tocEntrySpec, referencesTitleSpec, sectionTitleSpec *ParagraphFormatSpec
	var notesSpec, captionSpec, headerSpec *ParagraphFormatSpec
	var profileForMargins *templateprofile.Profile
	var templateHeaderText string
	var resolvedSpecs map[string]ParagraphFormatSpec
	if ruleEngine, ruleErr := NewFormatRuleEngine(e.processor, e.templatePath, nil); ruleErr == nil {
		resolvedSpecs = templateProfileBackedSpecs(ruleEngine.Rules(), ruleEngine.Profile)
		runLog.resolvedRules(resolvedSpecs)
		// 保存 Profile 引用，稍后在格式化完成后应用页边距
		// （避免被后续 sectPr 操作覆盖 —— B1-B2 修复）
		if ruleEngine.Profile != nil {
			profileForMargins = ruleEngine.Profile
		}
		// The OOXML profile is the source of truth.  GetRule also contains
		// legacy compiled/instruction rules, which can describe a different
		// sample from the same template (notably body bold/alignment and
		// complex-script size).  Use the profile-backed map for every role so
		// one extraction cannot be silently overwritten by the fallback loader.
		specFor := func(category string) (ParagraphFormatSpec, bool) {
			spec, ok := resolvedSpecs[category]
			return spec, ok
		}
		headingSpecs = make(map[string]ParagraphFormatSpec)
		for _, category := range []string{
			V2Heading1, V2Heading2, V2Heading3, V2Heading4,
			V2AcknowledgementsTitle, V2Acknowledgements,
			V2AppendixTitle, V2Appendix, V2NotesTitle, V2Notes,
		} {
			if spec, ok := specFor(category); ok {
				headingSpecs[category] = spec
			}
		}
		// 🔒 LOCKED: 正文段落 — bodySpec 从 FormatRuleEngine 取值（模板 > 硬编码兜底）
		if bs, ok := specFor("body"); ok {
			bodySpec = &bs
		}
		// 🔒 LOCKED: 参考文献条目 — refSpec 从 FormatRuleEngine 取值，行距不硬编码
		if rs, ok := specFor("references"); ok {
			refSpec = &rs
		}
		if spec, ok := specFor("cover_title"); ok {
			coverTitleSpec = &spec
		}
		if spec, ok := specFor("cover"); ok {
			coverFieldSpec = &spec
		}
		if spec, ok := specFor("abstract_title"); ok {
			abstractTitleSpec = &spec
		}
		if spec, ok := specFor("abstract"); ok {
			abstractContentSpec = &spec
		}
		if spec, ok := specFor("keywords"); ok {
			keywordsSpec = &spec
		}
		if spec, ok := specFor("en_abstract_title"); ok {
			enAbstractTitleSpec = &spec
		}
		if spec, ok := specFor("en_abstract"); ok {
			enAbstractContentSpec = &spec
		}
		if spec, ok := specFor("en_keywords"); ok {
			enKeywordsSpec = &spec
		}
		if spec, ok := specFor("toc_title"); ok {
			tocTitleSpec = &spec
		}
		if spec, ok := specFor("toc_entry"); ok {
			tocEntrySpec = &spec
		}
		if spec, ok := specFor("references_title"); ok {
			referencesTitleSpec = &spec
		}
		if spec, ok := specFor("section_title"); ok {
			sectionTitleSpec = &spec
		}
		if spec, ok := specFor("notes"); ok {
			notesSpec = &spec
		}
		if spec, ok := specFor("caption"); ok {
			captionSpec = &spec
		}
		if spec, ok := specFor("header"); ok {
			headerSpec = &spec
		}
		templateHeaderText = ""
		if ruleEngine.Profile != nil {
			templateHeaderText = ruleEngine.Profile.Header.Text
			e.processor.templateHeaderText = templateHeaderText
		}
		// DIAG: dump all critical specs
		if bodySpec != nil {
			DiagPrintf("[DIAG] bodySpec: fontEA=%s sz=%d line=%d rule=%d fl=%d", bodySpec.FontEastAsia, bodySpec.FontSizeHalfPt, bodySpec.LineSpacingVal, bodySpec.LineSpacingRule, bodySpec.FirstLineIndent)
		}
		if refSpec != nil {
			DiagPrintf("[DIAG] refSpec: fontEA=%s sz=%d line=%d rule=%d fl=%d", refSpec.FontEastAsia, refSpec.FontSizeHalfPt, refSpec.LineSpacingVal, refSpec.LineSpacingRule, refSpec.FirstLineIndent)
		}
		if referencesTitleSpec != nil {
			DiagPrintf("[DIAG] referencesTitleSpec: fontEA=%s sz=%d line=%d rule=%d", referencesTitleSpec.FontEastAsia, referencesTitleSpec.FontSizeHalfPt, referencesTitleSpec.LineSpacingVal, referencesTitleSpec.LineSpacingRule)
		}
		if sectionTitleSpec != nil {
			DiagPrintf("[DIAG] sectionTitleSpec: fontEA=%s sz=%d line=%d rule=%d", sectionTitleSpec.FontEastAsia, sectionTitleSpec.FontSizeHalfPt, sectionTitleSpec.LineSpacingVal, sectionTitleSpec.LineSpacingRule)
		}
	}
	smartFmt := NewV2SmartFormatter(e.processor, headingSpecs, bodySpec, refSpec,
		coverTitleSpec, coverFieldSpec, abstractTitleSpec, abstractContentSpec, keywordsSpec,
		enAbstractTitleSpec, enAbstractContentSpec, enKeywordsSpec,
		tocTitleSpec, tocEntrySpec,
		referencesTitleSpec, sectionTitleSpec, notesSpec, captionSpec, headerSpec,
		templateHeaderText)
	smartClassified := classified
	if len(resolvedSpecs) > 0 {
		paragraphsByType := v2ParagraphMap(classified)
		verifyAndLockParagraphTypes(e.processor, e.processor.formatLocks, paragraphsByType, resolvedSpecs)
		appliedCount := NewAIFormatApplier(e.processor).Apply(
			paragraphsByType,
			resolvedSpecs,
			lockedCategoryMap(e.processor.formatLocks, paragraphsByType),
		)
		runLog.printf("规则写入结果：实际处理 %d 个非空段落；已符合规则并锁定的类别会跳过重复写入。", appliedCount)
		verifyAndLockParagraphTypes(e.processor, e.processor.formatLocks, paragraphsByType, resolvedSpecs)
		smartClassified = unlockedV2Paragraphs(e.processor.formatLocks, classified)
	}
	// The unified rule applier above is the paragraph-format writer. The legacy
	// smart formatter contains hard-coded fallbacks and must not overwrite
	// template-derived specs.
	_ = smartFmt
	_ = smartClassified
	if len(resolvedSpecs) > 0 {
		repair := NewRepairAgent(e.processor, 3, e.processor.repairDiagnosticClient()).
			WithLocks(e.processor.formatLocks).
			RunClassified(v2ParagraphMap(classified), resolvedSpecs)
		fixCount += repair.TotalFixes
		runLog.repair(repair)
	}

	// B1-B2 修复：在所有 sectPr 操作（克隆/格式化/页眉）完成后才应用页边距覆盖
	if profileForMargins != nil {
		ApplyProfilePageMargins(studentDoc, profileForMargins)
		// 诊断：写入内存诊断文件
		diagLines := []string{}
		if s := studentDoc.X().Body.SectPr; s != nil && s.PgMar != nil {
			top := int64(0)
			if s.PgMar.TopAttr.Int64 != nil {
				top = *s.PgMar.TopAttr.Int64
			}
			left := uint64(0)
			if s.PgMar.LeftAttr.ST_UnsignedDecimalNumber != nil {
				left = *s.PgMar.LeftAttr.ST_UnsignedDecimalNumber
			}
			diagLines = append(diagLines, fmt.Sprintf("body PgMar top=%d left=%d", top, left))
		}
		diagLines = append(diagLines, fmt.Sprintf("profile TopTwips=%s LeftTwips=%s",
			profileForMargins.PageSetup.MarginTopTwips, profileForMargins.PageSetup.MarginLeftTwips))
		for i, p := range studentDoc.Paragraphs() {
			ppr := p.Properties().X()
			if ppr != nil && ppr.SectPr != nil && ppr.SectPr.PgMar != nil {
				top := int64(0)
				if ppr.SectPr.PgMar.TopAttr.Int64 != nil {
					top = *ppr.SectPr.PgMar.TopAttr.Int64
				}
				left := uint64(0)
				if ppr.SectPr.PgMar.LeftAttr.ST_UnsignedDecimalNumber != nil {
					left = *ppr.SectPr.PgMar.LeftAttr.ST_UnsignedDecimalNumber
				}
				diagLines = append(diagLines, fmt.Sprintf("para[%d] PgMar top=%d left=%d", i, top, left))
			}
		}
		if diagPath := strings.TrimSpace(os.Getenv("PAPER_V2_MARGIN_DIAG_PATH")); diagPath != "" {
			_ = os.WriteFile(diagPath, []byte(strings.Join(diagLines, "\n")), 0o644)
		}
	}

	// Preserve tables unless a dedicated table rule exists. Body paragraph
	// formatting must not overwrite cover grids, headers, or data tables.

	// ── 步骤 9: 生成差异报告 ──
	log.Println("[V2][步骤9] 生成差异报告...")
	diffReport := e.buildDiffReport(classified, store)
	e.processor.lastDiffReport = diffReport
	log.Printf("[V2][步骤9] 差异报告: 扫描%d段, %d错误, %d警告",
		diffReport.TotalParas, diffReport.ErrorCount, diffReport.WarningCount)
	runLog.diffReport(diffReport)

	// ── 步骤 10: 保存输出文件 ──
	outputPath := e.generateOutputPath(studentDocPath)
	log.Printf("[V2][步骤10] 保存到: %s", outputPath)
	runLog.section("第10步：保存文件并复制页眉页脚")
	runLog.printf("输出文件：%s", outputPath)

	// 节点4：最终产出验证 — 保存前检查关键段落类型的实际 run/paragraph 属性
	DiagPrintf(" ====== 节点4: 最终产出验证 (保存前) ======")
	dumpFinalDocDiagnostics(studentDoc, classified)

	// B1-B2 post-save fix: unioffice serializes body-level sectPr from a cached/internal
	// copy that ignores our in-memory mutations.  Save → patch ZIP in-place.

	if err := studentDoc.SaveToFile(outputPath); err != nil {
		return "", fmt.Errorf("保存文档失败: %w", err)
	}
	if info, statErr := os.Stat(outputPath); statErr == nil {
		runLog.printf("文档保存成功：%d 字节。", info.Size())
	}
	coverInfo := e.processor.extractCoverInfo(studentDoc)
	coverKeys := make([]string, 0, len(coverInfo))
	for key := range coverInfo {
		coverKeys = append(coverKeys, key)
	}
	sort.Strings(coverKeys)
	for _, key := range coverKeys {
		runLog.printf("页眉占位符材料：%s=%q", key, compactText(coverInfo[key], 120))
	}
	if err := copyAndMaterializeTemplateHeaderFooter(e.templatePath, outputPath, coverInfo); err != nil {
		return "", fmt.Errorf("复制模板页眉页脚失败: %w", err)
	}
	if headerCount, footerCount, ok := verifyCopiedHeaderFooterStructure(e.templatePath, outputPath); ok {
		e.processor.formatLocks.Lock("header", headerCount)
		e.processor.formatLocks.Lock("footer", footerCount)
		runLog.printf("页眉页脚复制并验证成功：页眉部件=%d，页脚部件=%d。", headerCount, footerCount)
	} else {
		runLog.printf("页眉页脚结构复检未能确认通过。")
	}
	if profileForMargins != nil {
		// B-S5 fix: unioffice body-level sectPr pgMar serialization is
		// non-deterministic (sometimes 1417 instead of 1418).  Post-save
		// ZIP patch ensures all body-level pgMar values match the profile.
		data, err := os.ReadFile(outputPath)
		if err != nil {
			log.Printf("[B1B2] failed to read saved file: %v", err)
		} else {
			patched, err := patchBodySectPrMarginsFromBytes(data, &profileForMargins.PageSetup)
			if err != nil {
				log.Printf("[B1B2] post-save ZIP patch warning: %v", err)
				runLog.printf("页边距兜底修补失败：%v", err)
			} else {
				changed := !bytes.Equal(data, patched)
				if err := os.WriteFile(outputPath, patched, 0644); err != nil {
					log.Printf("[B1B2] post-save ZIP patch write error: %v", err)
					runLog.printf("页边距兜底结果写入失败：%v", err)
				} else {
					runLog.section("最后一步：页边距兜底修补")
					runLog.printf("目标页面值：%s", humanPageSetup(profileForMargins.PageSetup))
					runLog.printf("是否发现保存后的边距偏差并改写 OOXML ZIP：%s", yesNo(changed))
				}
			}
		}
	}
	// Save/section-margin patches can reintroduce empty numeric OOXML
	// attributes (notably w:gutter=""). Normalize after the last package write
	// so the V2 engine itself always returns a reopenable DOCX.
	if _, err := transplant.NormalizeFinalDOCX(outputPath); err != nil {
		return "", fmt.Errorf("normalize V2 output: %w", err)
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
// 可选 StyleFormatter (python-docx) → V2FormatEngine (unioffice) 回退链
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

func (p *EnhancedProcessor) applyCorrectionsV2Once(ctx context.Context, docPath string, corrections []map[string]interface{}) (result string, retErr error) {
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
	runLog, runLogErr := newFormatRunLog(docPath, templatePath)
	if runLogErr != nil {
		log.Printf("[格式诊断日志] 创建 logs/geshi 日志失败（不阻断论文处理）: %v", runLogErr)
	} else {
		ctx = withFormatRunLog(ctx, runLog)
		defer func() { runLog.finish(retErr) }()
		runLog.section("流程入口")
		runLog.printf("用户修正规则数量：%d", len(corrections))
	}
	var sourceContent []string
	if fileExt == ".docx" {
		var snapshotErr error
		sourceContent, snapshotErr = captureDOCXContent(p, docPath)
		if snapshotErr != nil {
			runLog.printf("输入论文内容快照失败：%v", snapshotErr)
			return "", fmt.Errorf("capture source content: %w", snapshotErr)
		}
		runLog.printf("输入论文内容快照：共 %d 个段落，供最终内容保全检查使用。", len(sourceContent))
	}
	logPaperFormatAudit("GoV2", "apply_start", map[string]interface{}{
		"doc_path":      docPath,
		"template_path": templatePath,
		"shell_enabled": false,
	})
	finalizeOutput := func(primaryEngine, outPath string) (string, error) {
		runLog.section("最终闭环处理")
		runLog.printf("主格式引擎：%s；主引擎产物：%s", primaryEngine, outPath)
		logOutputParagraphCount("primary-engine", outPath)
		plannedPath, err := p.applyUnifiedRulePlanToFile(outPath, templatePath, corrections, runLog)
		if err != nil {
			return "", fmt.Errorf("apply unified format plan: %w", err)
		}
		runLog.printf("统一规则计划：成功；产物=%s", plannedPath)
		logOutputParagraphCount("unified-rule-plan", plannedPath)
		// Apply region-specific OOXML rules before strong verification. The V2
		// body-level classifier cannot reliably reach cover-table dates and
		// captions, so verifying before these passes creates avoidable fallback
		// rebuilds and leaves those paragraphs in the student's old format.
		preProfile, profileErr := templateprofile.Extract(templatePath)
		if profileErr != nil {
			return "", fmt.Errorf("extract template profile before strong verification: %w", profileErr)
		}
		if _, err := templateapply.ApplyTemplateProfileCoverStylesAndPageSetup(ctx, plannedPath, preProfile); err != nil {
			return "", fmt.Errorf("apply pre-verification cover profile: %w", err)
		}
		if _, err := templateapply.ApplyTemplateProfileCaptionStyles(ctx, plannedPath, preProfile); err != nil {
			return "", fmt.Errorf("apply pre-verification caption profile: %w", err)
		}
		if _, err := templateapply.ApplyTemplateProfileBodyListStyles(ctx, plannedPath, preProfile); err != nil {
			return "", fmt.Errorf("apply pre-verification body-list profile: %w", err)
		}
		if _, err := templateapply.ApplyTemplateProfileTOCStyles(ctx, plannedPath, preProfile); err != nil {
			return "", fmt.Errorf("apply pre-verification TOC profile: %w", err)
		}
		finalPath, finalEngine, err := p.enforceStrongFormatConsistency(ctx, docPath, plannedPath, templatePath, corrections, primaryEngine)
		if err != nil {
			return "", err
		}
		runLog.printf("强一致性复检：成功；最终采用引擎=%s；产物=%s", finalEngine, finalPath)
		logOutputParagraphCount("strong-consistency", finalPath)
		profile, err := templateprofile.Extract(templatePath)
		if err != nil {
			return "", fmt.Errorf("extract template profile for final validation: %w", err)
		}
		if applied, err := templateapply.ApplyTemplateProfileCoverStylesAndPageSetup(ctx, finalPath, profile); err != nil {
			return "", fmt.Errorf("apply final template profile styles and page setup: %w", err)
		} else {
			runLog.printf("最终模板 Profile 样式与页面设置重写：成功；修改=%d 处", applied)
		}
		runLog.printf("最终页面设置重写：成功；%s", humanPageSetup(profile.PageSetup))
		logOutputParagraphCount("template-profile", finalPath)
		p.applyPostSavePatches(finalPath, templatePath)
		p.normalizeFinalBoldByProfile(finalPath, templatePath)
		runLog.printf("保存后 XML 补丁：已执行（页眉规范化、页码域规范化、页边距兜底）。")
		logOutputParagraphCount("post-save-patches", finalPath)
		if _, err := transplant.NormalizeFinalDOCX(finalPath); err != nil {
			return "", fmt.Errorf("normalize final docx: %w", err)
		}
		runLog.printf("OOXML 归一化：成功。")
		logOutputParagraphCount("normalize-final", finalPath)
		if _, err := ooxmlpkg.RepairPropertyOrder(finalPath); err != nil {
			return "", fmt.Errorf("repair final OOXML property order: %w", err)
		}
		runLog.printf("OOXML 段落属性/文字属性元素顺序修复：成功。")
		logOutputParagraphCount("property-order", finalPath)
		// Reapply the narrow cover-only profile pass after all final OOXML
		// normalization so a writer cannot reintroduce stale cover run props.
		if applied, err := templateapply.ApplyTemplateProfileCoverStylesAndPageSetup(ctx, finalPath, profile); err != nil {
			return "", fmt.Errorf("reapply final cover profile: %w", err)
		} else {
			runLog.printf("final cover profile verification: applied=%d", applied)
		}
		if applied, err := templateapply.ApplyTemplateProfileCaptionStyles(ctx, finalPath, profile); err != nil {
			return "", fmt.Errorf("reapply final caption profile: %w", err)
		} else {
			runLog.printf("final caption profile verification: applied=%d", applied)
		}
		if applied, err := templateapply.ApplyTemplateProfileBodyListStyles(ctx, finalPath, profile); err != nil {
			return "", fmt.Errorf("reapply final body-list profile: %w", err)
		} else {
			runLog.printf("final body-list profile verification: applied=%d", applied)
		}
		if applied, err := templateapply.ApplyTemplateProfileTOCStyles(ctx, finalPath, profile); err != nil {
			return "", fmt.Errorf("reapply final TOC profile: %w", err)
		} else {
			runLog.printf("final TOC profile verification: applied=%d", applied)
		}
		// The final OOXML normalizer may recreate section properties and
		// relationship IDs.  Copy the template's header/footer package after
		// that last writer, so the file delivered to the user cannot retain the
		// student's empty header parts or stale references.  Re-materialize
		// placeholders from the source cover when available.
		coverInfo := map[string]string{}
		if sourceDoc, openErr := document.Open(docPath); openErr == nil {
			coverInfo = p.extractCoverInfo(sourceDoc)
			sourceDoc.Close()
		}
		if err := copyAndMaterializeTemplateHeaderFooter(templatePath, finalPath, coverInfo); err != nil {
			return "", fmt.Errorf("final template header/footer copy: %w", err)
		}
		runLog.printf("最终页眉页脚包复制：已在最后写入阶段完成")
		if sourceContent != nil {
			if err := verifyDOCXContentPreserved(p, sourceContent, finalPath); err != nil {
				SetFormatRunLogResult(ctx, "manual_review", "manual_review")
				return "", fmt.Errorf("content preservation check failed: %w", err)
			}
			runLog.printf("内容保全检查：成功；段落数量=%d，逐段文字未改变。", len(sourceContent))
		}
		// The final profile/OOXML/header-footer passes above are the artifact
		// returned to callers. Validate that exact artifact before marking the
		// operation successful; earlier strong verification is only a repair hint.
		// Do not let database/user JSON redefine the acceptance criteria. The
		// selected DOCX template is the authoritative standard for the final
		// download gate; JSON rules may guide repair but cannot mask a mismatch.
		finalDiffs, verifyErr := p.countTemplateSpecDiffs(finalPath, templatePath, nil)
		if verifyErr != nil {
			SetFormatRunLogResult(ctx, "manual_review", "manual_review")
			return "", fmt.Errorf("final format quality gate failed: %w", verifyErr)
		}
		// The final download gate is stricter than the optional repair retry
		// threshold: a returned document must have zero profile differences.
		runLog.printf("最终格式质量门禁：差异=%d，允许阈值=0", finalDiffs)
		if finalDiffs != 0 {
			SetFormatRunLogResult(ctx, "manual_review", "manual_review")
			return "", fmt.Errorf("final format quality gate rejected output: %d diffs remain", finalDiffs)
		}
		SetFormatRunLogResult(ctx, "verified_pass", "verified")
		runLog.printf("最终格式质量门禁：通过；仅允许下载最终已验证文件")
		logPaperFormatAudit("GoV2", "path_chosen", map[string]interface{}{
			"engine":      finalEngine,
			"output_path": finalPath,
		})
		if info, statErr := os.Stat(finalPath); statErr == nil {
			runLog.printf("最终文件：%s（%d 字节）", finalPath, info.Size())
		}
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
		runLog.printf("V2FormatEngine 失败：%v；转入旧版回退流程。", err)
		return p.ApplyCorrections(ctx, docPath, corrections)
	}

	finalPath, finErr := finalizeOutput("V2FormatEngine", outputPath)
	if finErr != nil {
		return "", finErr
	}
	return finalPath, nil
}

// normalizeFinalBoldByProfile repairs stale run-level bold properties after
// the final DOCX writer has serialized the document. Targets come only from
// the template profile and deterministic classifier.
func (p *EnhancedProcessor) normalizeFinalBoldByProfile(path, templatePath string) {
	profile, err := templateprofile.Extract(templatePath)
	if err != nil || profile == nil {
		return
	}
	ruleEngine, err := NewFormatRuleEngine(p, templatePath, nil)
	if err != nil {
		return
	}
	specs := templateProfileBackedSpecs(ruleEngine.Rules(), profile)
	doc, err := document.Open(path)
	if err != nil {
		return
	}
	classified := NewV2DeterministicClassifier(p).Classify(BodyLevelParagraphsOnly(doc))
	targets := map[string]int{}
	for _, item := range classified {
		// A cover paragraph containing the document-type marker is the cover
		// heading, not ordinary cover metadata. Do not let the generic cover
		// normal-weight cleanup strip its template bold setting.
		if item.Type == V2Cover || item.Type == V2ThesisTitle || isCoverTitleText(item.Text) {
			continue
		}
		spec, ok := specs[item.Type]
		if !ok || !spec.BoldSet || spec.Bold {
			continue
		}
		if text := strings.TrimSpace(item.Text); text != "" {
			targets[text]++
		}
	}
	doc.Close()
	if len(targets) == 0 {
		return
	}
	entries, err := readDocxEntries(path)
	if err != nil {
		return
	}
	xml := string(entries["word/document.xml"])
	paragraphPatternFinal := regexp.MustCompile(`(?s)<w:p\b[^>]*>.*?</w:p>`)
	paragraphs := paragraphPatternFinal.FindAllStringIndex(xml, -1)
	boldPattern := regexp.MustCompile(`(?s)<w:b(?:Cs)?\b[^>]*/>|<w:b(?:Cs)?\b[^>]*>.*?</w:b(?:Cs)>`)
	rPrPattern := regexp.MustCompile(`(?s)<w:rPr\b[^>]*>.*?</w:rPr>`)
	changed := 0
	for i := len(paragraphs) - 1; i >= 0; i-- {
		start, end := paragraphs[i][0], paragraphs[i][1]
		paragraph := xml[start:end]
		text := strings.TrimSpace(strings.Join(extractDocxTextNodes(paragraph), ""))
		if targets[text] <= 0 {
			continue
		}
		updated := rPrPattern.ReplaceAllStringFunc(paragraph, func(rPr string) string {
			return boldPattern.ReplaceAllString(rPr, "")
		})
		if updated != paragraph {
			xml = xml[:start] + updated + xml[end:]
			targets[text]--
			changed++
		}
	}
	if changed == 0 {
		return
	}
	entries["word/document.xml"] = []byte(xml)
	if err := writeDocxEntries(path, entries); err != nil {
		log.Printf("[最终粗体兜底] 写入失败: %v", err)
	} else {
		log.Printf("[最终粗体兜底] 按模板 Profile 清除 %d 个段落粗体", changed)
	}
}

func isCoverTitleText(text string) bool {
	text = strings.TrimSpace(text)
	if strings.Contains(text, "原创性") || strings.Contains(text, "作者") || len([]rune(text)) > 32 {
		return false
	}
	return strings.Contains(text, "毕业论文") || strings.Contains(text, "毕业设计") ||
		strings.Contains(text, "学士学位") || strings.Contains(text, "硕士学位") || strings.Contains(text, "博士学位")
}

func captureDOCXContent(processor *EnhancedProcessor, path string) ([]string, error) {
	doc, err := document.Open(path)
	if err != nil {
		return nil, err
	}
	defer doc.Close()
	paragraphs := doc.Paragraphs()
	content := make([]string, len(paragraphs))
	for index, paragraph := range paragraphs {
		content[index] = processor.extractParagraphText(paragraph)
	}
	return content, nil
}

func verifyDOCXContentPreserved(processor *EnhancedProcessor, source []string, outputPath string) error {
	output, err := captureDOCXContent(processor, outputPath)
	if err != nil {
		return err
	}
	if len(output) != len(source) {
		return fmt.Errorf("paragraph count changed from %d to %d", len(source), len(output))
	}
	for index := range source {
		if normalizePreservedContentText(source[index]) != normalizePreservedContentText(output[index]) {
			return fmt.Errorf("paragraph %d text changed", index)
		}
	}
	return nil
}

func normalizePreservedContentText(text string) string {
	text = strings.ReplaceAll(text, "\u3000", "")
	return strings.Join(strings.Fields(text), "")
}

func (p *EnhancedProcessor) applyUnifiedRulePlanToFile(path, templatePath string, corrections []map[string]interface{}, runLog *formatRunLog) (string, error) {
	rules := normalizedFormatRulesFromCorrections(p, corrections)
	engine, err := NewFormatRuleEngine(p, templatePath, rules)
	if err != nil {
		return "", err
	}
	specs := templateProfileBackedSpecs(engine.Rules(), engine.Profile)
	runLog.resolvedRules(specs)
	doc, err := document.Open(path)
	if err != nil {
		return "", err
	}
	locks, err := p.applyTemplateFormatting(doc, rules, specs)
	if err != nil {
		doc.Close()
		return "", err
	}
	repair := NewRepairAgent(p, 3, p.repairDiagnosticClient()).WithLocks(locks).Run(doc, specs)
	runLog.printf("最终统一规则计划的复检与修复：")
	runLog.repair(repair)
	if applied := applyVisualRepairs(doc, corrections); applied > 0 {
		runLog.printf("Python视觉反馈应用：%d处高置信度修复", applied)
	}
	if repair.NeedsManualReview {
		log.Printf("[修复代理] 规划执行后三轮仍有 %d 处差异，标记为需人工复核", repair.FinalDiffs)
	}
	tempPath := fmt.Sprintf("%s.rule-plan-%d.docx", strings.TrimSuffix(path, filepath.Ext(path)), time.Now().UnixNano())
	if err := doc.SaveToFile(tempPath); err != nil {
		doc.Close()
		return "", err
	}
	doc.Close()
	if _, err := transplant.NormalizeFinalDOCX(tempPath); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("normalize unified rule plan output: %w", err)
	}
	return promoteStrongVerificationRetry(tempPath, path), nil
}

// applyVisualRepairs applies only explicit, targetable repairs returned by the
// Python renderer. The node id is generated from the Go paragraph index, so a
// missing or malformed target is ignored instead of widening the edit scope.
func applyVisualRepairs(doc *document.Document, corrections []map[string]interface{}) int {
	return applyVisualRepairsWithIndexes(doc, corrections, nil)
}

func applyVisualRepairsWithIndexes(doc *document.Document, corrections []map[string]interface{}, nodeIndexes map[string]int) int {
	if doc == nil {
		return 0
	}
	paragraphs := BodyLevelParagraphsOnly(doc)
	paragraphByIndex := make(map[int]document.Paragraph, len(paragraphs))
	for i, para := range paragraphs {
		paragraphByIndex[i] = para
	}
	applied := 0
	for _, correction := range corrections {
		raw, ok := correction["visual_repairs"].([]map[string]interface{})
		if !ok {
			continue
		}
		for _, repair := range raw {
			if repair["action"] != "set_paragraph_alignment" || repair["value"] == nil {
				continue
			}
			nodeID, _ := repair["node_id"].(string)
			index, found := nodeIndexes[nodeID]
			if !found && strings.HasPrefix(nodeID, "para-") {
				if parsed, err := strconv.Atoi(strings.TrimPrefix(nodeID, "para-")); err == nil {
					index, found = parsed, true
				}
			}
			if !found {
				continue
			}
			alignment, _ := repair["value"].(string)
			var value wml.ST_Jc
			switch alignment {
			case "center":
				value = wml.ST_JcCenter
			case "left":
				value = wml.ST_JcLeft
			case "right":
				value = wml.ST_JcRight
			case "justify", "both":
				value = wml.ST_JcBoth
			default:
				continue
			}
			para, ok := paragraphByIndex[index]
			if !ok {
				continue
			}
			para.Properties().SetAlignment(value)
			applied++
		}
	}
	return applied
}

// ApplyVisualRepairsToFile applies only explicit high-confidence repairs from
// the Python visual service and writes the result back to the same DOCX.
// Keeping this narrow prevents OCR feedback from triggering a second global
// formatting pass.
func ApplyVisualRepairsToFile(path string, repairs []map[string]interface{}) (int, error) {
	if strings.TrimSpace(path) == "" || len(repairs) == 0 {
		return 0, nil
	}
	doc, err := document.Open(path)
	if err != nil {
		return 0, err
	}
	ast, err := paperast.Extract(path)
	if err != nil {
		doc.Close()
		return 0, fmt.Errorf("extract visual repair node IDs: %w", err)
	}
	nodeIndexes := make(map[string]int)
	for _, node := range ast.Nodes {
		if node.SourcePart == "word/document.xml" && node.NodeType == "paragraph" {
			nodeIndexes[node.NodeID] = node.Index
		}
	}
	applied := applyVisualRepairsWithIndexes(doc, []map[string]interface{}{{"visual_repairs": repairs}}, nodeIndexes)
	if applied == 0 {
		doc.Close()
		return 0, nil
	}
	tmp := fmt.Sprintf("%s.python-visual-%d.docx", strings.TrimSuffix(path, filepath.Ext(path)), time.Now().UnixNano())
	if err := doc.SaveToFile(tmp); err != nil {
		doc.Close()
		return 0, err
	}
	doc.Close()
	if _, err := transplant.NormalizeFinalDOCX(tmp); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	if err := os.Remove(path); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return applied, nil
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

func (p *EnhancedProcessor) countTemplateSpecDiffs(docPath, templatePath string, rules map[string]interface{}) (int, error) {
	var specs map[string]ParagraphFormatSpec
	if rules == nil {
		// Final verification must compare against the OOXML profile itself. A
		// legacy/named-style fallback can turn an explicitly exact line rule
		// into an auto rule and report false residuals after the profile pass.
		profile, err := templateprofile.Extract(templatePath)
		if err != nil {
			return 0, err
		}
		specs = templateProfileBackedSpecs(nil, profile)
	} else {
		engine, err := NewFormatRuleEngine(p, templatePath, rules)
		if err != nil {
			return 0, err
		}
		specs = templateProfileBackedSpecs(engine.Rules(), engine.Profile)
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
	classified := NewV2DeterministicClassifier(p).ClassifyToMap(BodyLevelParagraphsOnly(doc))
	diffs := verifier.compareAllWithSpecs(classified, specs)
	if len(diffs) > 0 {
		// Keep strong-verification failures diagnosable; a bare count cannot
		// distinguish a real template mismatch from a classifier/spec mapping
		// error. The details are written to the existing per-run format log.
		limit := len(diffs)
		if limit > 40 {
			limit = 40
		}
		for _, diff := range diffs[:limit] {
			log.Printf("[强校验差异] category=%s para=%d property=%s expected=%s actual=%s text=%s", diff.Category, diff.ParaIdx, diff.Property, diff.Expected, diff.Actual, diff.TextSnip)
		}
	}
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
	formatRules := normalizedFormatRulesFromCorrections(p, corrections)
	p.lastStrongVerify.Enabled = true
	p.lastStrongVerify.Threshold = threshold
	initialDiffs, err := p.countTemplateSpecDiffs(candidatePath, templatePath, formatRules)
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

	retryPath := fmt.Sprintf("%s.strong-retry-%d.docx", strings.TrimSuffix(candidatePath, filepath.Ext(candidatePath)), time.Now().UnixNano())
	if copyErr := copyStrongVerificationCandidate(candidatePath, retryPath); copyErr != nil {
		log.Printf("[强校验] 无法创建事务性重试副本，进入V2回退: %v", copyErr)
		return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, initialDiffs, threshold)
	}
	doc, openErr := document.Open(retryPath)
	if openErr != nil {
		_ = os.Remove(retryPath)
		log.Printf("[强校验] 重试前打开失败，进入V2回退: %v", openErr)
		return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, initialDiffs, threshold)
	}
	ruleEngine, loadErr := NewFormatRuleEngine(p, templatePath, formatRules)
	var specs map[string]ParagraphFormatSpec
	if loadErr == nil && ruleEngine != nil {
		// The initial V2 pass is profile-backed; the strong retry must use the
		// same OOXML-derived expectations. Falling back to the legacy loader here
		// reintroduced instruction-text rules and caused needless rebuilds.
		specs = templateProfileBackedSpecs(ruleEngine.Rules(), ruleEngine.Profile)
	}
	if loadErr != nil || len(specs) == 0 {
		doc.Close()
		_ = os.Remove(retryPath)
		log.Printf("[强校验] 无法加载模板规范，进入V2回退: %v", loadErr)
		return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, initialDiffs, threshold)
	}

	repair := NewRepairAgent(p, 3, p.repairDiagnosticClient()).Run(doc, specs)
	fixes := repair.TotalFixes
	p.lastStrongVerify.RepairRounds = repair.Rounds
	p.lastStrongVerify.NeedsManualReview = repair.NeedsManualReview
	if fixes > 0 {
		if saveErr := doc.SaveToFile(retryPath); saveErr != nil {
			doc.Close()
			_ = os.Remove(retryPath)
			log.Printf("[强校验] 重试保存失败，进入V2回退: %v", saveErr)
			return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, initialDiffs, threshold)
		}
	}
	doc.Close()
	if _, normalizeErr := transplant.NormalizeFinalDOCX(retryPath); normalizeErr != nil {
		_ = os.Remove(retryPath)
		log.Printf("[强校验] 重试产物规范化失败，进入 V2 回退: %v", normalizeErr)
		return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, initialDiffs, threshold)
	}

	retryDiffs, recountErr := p.countTemplateSpecDiffs(retryPath, templatePath, formatRules)
	if recountErr != nil {
		_ = os.Remove(retryPath)
		log.Printf("[强校验] 重试后复核失败，进入V2回退: %v", recountErr)
		return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, initialDiffs, threshold)
	}
	p.lastStrongVerify.RetryDiffs = retryDiffs
	_, fallback := planStrongVerificationAction(initialDiffs, retryDiffs, threshold)
	log.Printf("[强校验] 重试修正=%d 重试后差异=%d 阈值=%d", fixes, retryDiffs, threshold)
	if retryDiffs > initialDiffs {
		_ = os.Remove(retryPath)
		log.Printf("[强校验] 检测到回归：差异 %d -> %d，丢弃重试副本", initialDiffs, retryDiffs)
		return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, initialDiffs, threshold)
	}
	if !fallback {
		finalEngine := primaryEngine + "+StrongVerifyRetry"
		p.lastStrongVerify.FinalDiffs = retryDiffs
		p.lastStrongVerify.Passed = retryDiffs <= threshold
		p.lastStrongVerify.FinalEngine = finalEngine
		finalPath := promoteStrongVerificationRetry(retryPath, candidatePath)
		return finalPath, finalEngine, nil
	}
	_ = os.Remove(retryPath)
	return p.fallbackToV2Engine(ctx, sourceDocPath, templatePath, corrections, retryDiffs, threshold)
}

func copyStrongVerificationCandidate(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func promoteStrongVerificationRetry(retryPath, candidatePath string) string {
	if err := os.Remove(candidatePath); err != nil {
		return retryPath
	}
	if err := os.Rename(retryPath, candidatePath); err != nil {
		return retryPath
	}
	return candidatePath
}

func (p *EnhancedProcessor) fallbackToV2Engine(
	ctx context.Context,
	sourceDocPath string,
	templatePath string,
	corrections []map[string]interface{},
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
	finalDiffs, diffErr := p.countTemplateSpecDiffs(outPath, templatePath, normalizedFormatRulesFromCorrections(p, corrections))
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

func normalizedFormatRulesFromCorrections(p *EnhancedProcessor, corrections []map[string]interface{}) map[string]interface{} {
	for _, correction := range corrections {
		if rules, ok := correction["format_rules"].(map[string]interface{}); ok {
			return p.normalizeFormatRules(rules)
		}
	}
	return nil
}

func templateProfileBackedSpecs(specs map[string]ParagraphFormatSpec, profile *templateprofile.Profile) map[string]ParagraphFormatSpec {
	if profile == nil {
		return map[string]ParagraphFormatSpec{}
	}
	styleKeys := map[string][]string{
		V2Cover:                 {"cover"},
		V2ThesisTitle:           {"cover_title"},
		V2AbstractTitle:         {"abstract_cn"},
		V2Abstract:              {"abstract_body"},
		V2Keywords:              {"keywords_cn_body", "keywords_cn"},
		V2EnAbstractTitle:       {"abstract_en"},
		V2EnAbstract:            {"abstract_en_body"},
		V2EnKeywords:            {"keywords_en_body", "keywords_en"},
		V2Heading1:              {"heading_1", "body_start"},
		V2Heading2:              {"heading_2"},
		V2Heading3:              {"heading_3"},
		V2Heading4:              {"heading_4"},
		V2Body:                  {"body_text", "body"},
		V2ReferencesTitle:       {"references_title"},
		V2References:            {"references"},
		V2AcknowledgementsTitle: {"acknowledgements_title"},
		V2Acknowledgements:      {"acknowledgements_content", "acknowledgements"},
		V2AppendixTitle:         {"appendix_title"},
		V2Appendix:              {"appendix_content", "appendix"},
		V2NotesTitle:            {"notes_title"},
		V2Notes:                 {"notes"},
		V2TOCTitle:              {"toc_title"},
		V2TOC:                   {"toc_entry"},
		V2FigureCaption:         {"figure_caption", "caption"},
		V2TableCaption:          {"table_caption", "caption"},
	}
	result := make(map[string]ParagraphFormatSpec)
	for category, candidates := range styleKeys {
		found := false
		for _, key := range candidates {
			if style, ok := profile.Styles[key]; ok {
				if spec, usable := styleRuleToFormatSpec(style); usable {
					// A sampled template style with no w:b is effective normal
					// weight. Clear stale student bold formatting for every role;
					// an absent rule has SampleCount==0 and remains unspecified.
					if !style.BoldSet && style.SampleCount > 0 {
						spec.BoldSet = true
					}
					result[category] = spec
					found = true
				}
				break
			}
		}
		if !found {
			if fallback, ok := specs[category]; ok && !fallback.IsEmpty() {
				if !fallback.Bold && fallback.SampleCount > 0 {
					fallback.BoldSet = true
				}
				result[category] = fallback
			}
		}
	}
	for _, category := range []string{"header", "footer"} {
		if spec, ok := specs[category]; ok {
			result[category] = spec
		}
	}
	// Cover metadata is heterogeneous: the title and date are not governed by
	// the generic cover-field sample. Keep the date rule available to the
	// verifier and the final cover-only pass without inventing fixed values.
	if style, ok := profile.Styles["cover_date"]; ok {
		if spec, usable := styleRuleToFormatSpec(style); usable {
			result["cover_date"] = spec
		}
	}
	return result
}

// resolveTemplatePath searches for a valid golden template path from multiple sources.
// 优先级：① corrections.template_path（单次请求显式指定）
// ② SetTemplatePath / DB golden_template_path ③ 自动扫描 golden_templates 与 templates
func (p *EnhancedProcessor) resolveTemplatePath(corrections []map[string]interface{}) string {
	var templatePath string

	if templatePath = getStringFromCorrectionsList(corrections, "template_path"); templatePath != "" {
		if _, err := os.Stat(templatePath); os.IsNotExist(err) {
			log.Printf("[V2入口] corrections.template_path 不存在: %s", templatePath)
			templatePath = ""
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

	// Production is DOCX-only; a pre-converted sibling may be used for legacy records.
	if strings.ToLower(filepath.Ext(templatePath)) == ".doc" {
		docxPath := strings.TrimSuffix(templatePath, filepath.Ext(templatePath)) + ".docx"
		if _, err := os.Stat(docxPath); os.IsNotExist(err) {
			log.Printf("[V2入口] 不支持旧版 .doc 模板，请预先转换为 DOCX: %s", templatePath)
			return ""
		}
		templatePath = docxPath
	}

	return templatePath
}

// dumpFinalDocDiagnostics 保存前验证关键段落类型的实际 run/paragraph 属性
func dumpFinalDocDiagnostics(doc *document.Document, classified []V2ClassifiedPara) {
	// 收集每个类型的代表性段落（每种类型最多取 3 个）
	type sample struct {
		category string
		index    int
		para     document.Paragraph
	}
	sampled := map[string]int{}
	var samples []sample
	for _, cp := range classified {
		if cp.Para.WParagraph == nil {
			continue
		}
		if sampled[cp.Type] >= 3 {
			continue
		}
		sampled[cp.Type]++
		samples = append(samples, sample{category: cp.Type, index: cp.ParaIdx, para: cp.Para})
	}

	for _, s := range samples {
		var textBuilder strings.Builder
		for _, r := range s.para.Runs() {
			textBuilder.WriteString(r.Text())
		}
		text := strings.TrimSpace(textBuilder.String())
		if len([]rune(text)) > 30 {
			text = string([]rune(text)[:30]) + "..."
		}
		var pprInfo, rprInfo string
		if ppr := s.para.X().PPr; ppr != nil {
			parts := []string{}
			if ppr.Jc != nil {
				parts = append(parts, fmt.Sprintf("Align=%s", ppr.Jc.ValAttr.String()))
			}
			if ppr.Spacing != nil {
				if ppr.Spacing.LineAttr != nil && ppr.Spacing.LineAttr.Int64 != nil {
					lineRule := "auto"
					if ppr.Spacing.LineRuleAttr == wml.ST_LineSpacingRuleExact {
						lineRule = "exact"
					}
					parts = append(parts, fmt.Sprintf("Line=%d(%s)", *ppr.Spacing.LineAttr.Int64, lineRule))
				}
				if ppr.Spacing.BeforeAttr != nil && ppr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber != nil {
					parts = append(parts, fmt.Sprintf("Before=%d", *ppr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber))
				}
				if ppr.Spacing.AfterAttr != nil && ppr.Spacing.AfterAttr.ST_UnsignedDecimalNumber != nil {
					parts = append(parts, fmt.Sprintf("After=%d", *ppr.Spacing.AfterAttr.ST_UnsignedDecimalNumber))
				}
			}
			if ppr.Ind != nil && ppr.Ind.FirstLineAttr != nil && ppr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber != nil {
				parts = append(parts, fmt.Sprintf("FirstLine=%d", *ppr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber))
			}
			pprInfo = strings.Join(parts, " ")
		}
		runs := s.para.Runs()
		if len(runs) > 0 {
			r := runs[0]
			if rpr := r.X().RPr; rpr != nil {
				rParts := []string{}
				if rpr.RFonts != nil {
					if rpr.RFonts.EastAsiaAttr != nil {
						rParts = append(rParts, fmt.Sprintf("East=%s", *rpr.RFonts.EastAsiaAttr))
					}
					if rpr.RFonts.AsciiAttr != nil {
						rParts = append(rParts, fmt.Sprintf("Ascii=%s", *rpr.RFonts.AsciiAttr))
					}
				}
				if rpr.Sz != nil && rpr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
					rParts = append(rParts, fmt.Sprintf("sz=%.1fpt", float64(*rpr.Sz.ValAttr.ST_UnsignedDecimalNumber)/2.0))
				}
				if rpr.SzCs != nil && rpr.SzCs.ValAttr.ST_UnsignedDecimalNumber != nil {
					rParts = append(rParts, fmt.Sprintf("cs=%.1fpt", float64(*rpr.SzCs.ValAttr.ST_UnsignedDecimalNumber)/2.0))
				}
				if rpr.B != nil {
					rParts = append(rParts, "Bold=true")
				}
				if rpr.I != nil {
					rParts = append(rParts, "Italic=true")
				}
				rprInfo = strings.Join(rParts, " ")
			}
		}
		DiagPrintf(" [产出] idx=%d type=%s text=%q ppr={%s} rpr={%s}",
			s.index, s.category, text, pprInfo, rprInfo)
	}
}

// applyPostSavePatches applies XML-level patches to the final output that
// are difficult to express through the formatting engine alone. These run
// after all engines (StyleFormatter, V2FormatEngine, ShellInPlace) produce
// their final file, ensuring consistent results regardless of engine choice.
func (p *EnhancedProcessor) applyPostSavePatches(outputPath, templatePath string) {
	log.Println("[PostSave] applying XML patches...")

	data, err := os.ReadFile(outputPath)
	if err != nil {
		log.Printf("[PostSave] read error: %v", err)
		return
	}

	// Remove duplicate document-type suffixes without changing template styles.
	patched, err := patchHeaderNormalization(data)
	if err != nil {
		log.Printf("[PostSave] HeaderFix error: %v", err)
	} else if !bytes.Equal(patched, data) {
		data = patched
		log.Println("[PostSave] Duplicate header suffixes normalized")
	}

	// Strip legacy hardcoded "-" around PAGE fields.
	patched, err = patchFooterPageNumber(data)
	if err != nil {
		log.Printf("[PostSave] FooterFix error: %v", err)
	} else if !bytes.Equal(patched, data) {
		data = patched
		log.Println("[PostSave] Footers patched (removed hardcoded dashes around PAGE)")
	}

	if templatePath != "" {
		if profile, profileErr := templateprofile.Extract(templatePath); profileErr != nil {
			log.Printf("[PostSave] PageMargins profile error: %v", profileErr)
		} else if patched, patchErr := patchBodySectPrMarginsFromBytes(data, &profile.PageSetup); patchErr != nil {
			log.Printf("[PostSave] PageMargins patch error: %v", patchErr)
		} else {
			data = patched
		}
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		log.Printf("[PostSave] write error: %v", err)
	}
	log.Println("[PostSave] done")
}
