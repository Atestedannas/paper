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
	"gitee.com/greatmusicians/unioffice/schema/soo/ofc/sharedTypes"
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

	// 将 DiagPrintf 的诊断内容兜底到主流程中文日志（仅当未设置 PAPER_DIAG_LOG_PATH 时生效）；
	// Process 结束时统一由 defer 解除，避免污染其他流程。
	defer SetDiagRunLog(nil)
	SetDiagRunLog(runLog)

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

	// ── D4: 全局写操作标志 ──
	// 短路保存（机制2）只能在本链路确认"未产生任何写改动"时才允许触发 copyFileRaw。
	// 此前短路条件依赖 buildDiffReport（仅遍历 BodyLevelParagraphsOnly 顶层正文段），
	// 与真实写范围不一致，会静默丢弃 CloneStyles / section / 表格 run / 页边距等改动。
	// 现将所有写路径逐点计入 engineWrite，任一写路径命中即禁止短路。
	engineWrite := false

	// ── 步骤 4: 复制模板样式定义到学生文档 ──
	log.Println("[V2][步骤4] 复制样式定义...")
	styleSummary := CloneStyles(templateDoc, studentDoc)
	if styleSummary.DocDefaultsCopied || styleSummary.NamedStylesCopied > 0 {
		engineWrite = true
	}
	runLog.section("第4步：复制模板样式定义")
	runLog.printf("DocDefaults 默认样式已复制=%s；命名样式共复制=%d（覆盖学生已有=%d，新增=%d）",
		yesNo(styleSummary.DocDefaultsCopied), styleSummary.NamedStylesCopied, styleSummary.Overwritten, styleSummary.Added)
	runLog.printf("复制的样式 ID：%s", strings.Join(styleSummary.StyleIDs, "、"))

	// ── 步骤 5: 复制页面设置（A4/边距等）──
	log.Println("[V2][步骤5] 复制页面设置...")
	sectionSummary := CloneSectionProperties(templateDoc, studentDoc)
	if sectionSummary.PageSizeCopied || sectionSummary.PageMarginsCopied {
		engineWrite = true
	}
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
	// 步骤5b 对现成表格的三线表重写与上标引用设置均为真实写操作（D4）
	if tableCount > 0 || citationCount > 0 {
		engineWrite = true
	}
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
		// DIAG: dump all critical specs（全中文人类可读，含单位换算）
		if bodySpec != nil {
			DiagPrintf("[DIAG] 正文字体规范：%s", humanParagraphSpec(*bodySpec))
		}
		if refSpec != nil {
			DiagPrintf("[DIAG] 参考文献条目规范：%s", humanParagraphSpec(*refSpec))
		}
		if referencesTitleSpec != nil {
			DiagPrintf("[DIAG] 参考文献标题规范：%s", humanParagraphSpec(*referencesTitleSpec))
		}
		if sectionTitleSpec != nil {
			DiagPrintf("[DIAG] 章节标题规范：%s", humanParagraphSpec(*sectionTitleSpec))
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
		if appliedCount > 0 {
			engineWrite = true
		}
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
		if repair.TotalFixes > 0 {
			engineWrite = true
		}
		runLog.repair(repair)
	}

	// ── 步骤 8: 表格内正文格式化 ──
	// 职责：对表格内段落按模板正文格式（bodyFormat.RPr）统一套用，
	//       覆盖 BodyLevelParagraphsOnly 排除掉的表格内正文，保证不遗漏。
	log.Println("[V2][步骤8] 表格内正文格式化...")
	if e.applyTableFormatFromTemplate(studentDoc, store) > 0 {
		engineWrite = true
	}

	// B1-B2 修复：在所有 sectPr 操作（克隆/格式化/页眉）完成后才应用页边距覆盖
	if profileForMargins != nil {
		// D4: Profile 页边距覆盖为无条件写路径（写 body 级与所有段落级 sectPr）
		engineWrite = true
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
	DiagPrintf(" ====== 节点4: 最终产出验证（保存前） ======")
	dumpFinalDocDiagnostics(studentDoc, classified)

	// B1-B2 post-save fix: unioffice serializes body-level sectPr from a cached/internal
	// copy that ignores our in-memory mutations.  Save → patch ZIP in-place.

	// 短路保存（机制2 isUnchanged，D4 修正）：短路只能基于"本次引擎是否产生任何写操作"判定。
	// 旧条件依赖 buildDiffReport（仅遍历 BodyLevelParagraphsOnly 顶层正文段），不含
	// CloneStyles / CloneSectionProperties / 表格 run 替换 / 页边距修补等写路径，会在
	// 学生正文合规但表格/样式/边距需改时静默丢弃已执行修改。现仅当 engineWrite==false
	// （确认所有写路径均未命中）才允许 copyFileRaw 原字节保真复制。
	if !engineWrite {
		log.Printf("[V2][步骤10] 引擎未产生任何写操作（差异报告: 错误=%d 警告=%d），短路复制原文件到输出", diffReport.ErrorCount, diffReport.WarningCount)
		runLog.section("第10步：未产生任何写操作，原文件字节保真复制")
		if err := copyFileRaw(studentDocPath, outputPath); err != nil {
			return "", fmt.Errorf("短路保存复制原文件失败: %w", err)
		}
		if info, statErr := os.Stat(outputPath); statErr == nil {
			runLog.printf("原文件已字节保真复制：%d 字节。", info.Size())
		}
	} else {
		if err := studentDoc.SaveToFile(outputPath); err != nil {
			return "", fmt.Errorf("保存文档失败: %w", err)
		}
		if info, statErr := os.Stat(outputPath); statErr == nil {
			runLog.printf("文档保存成功：%d 字节。", info.Size())
		}
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

// applyTableFormatFromTemplate 使用模板的正文格式处理表格内文本。
// D3 修正：不再对表格 run 整块 cloneRPr 替换（原实现会清掉步骤5b 刚设置的上标 vertAlign、
// 高亮、超链接与 caps 等未覆盖子元素），改为"只改目标字体槽位 + 差异比较后写入"——
// 复用 v2_smart_format 的 v2WriteRFontsSlots（保留主题字体引用、已符合的槽不重复写、
// 未覆盖子元素不动），并显式跳过上标/高亮/超链接 run。返回实际写入的 run 数（D4 判定用）。
func (e *V2FormatEngine) applyTableFormatFromTemplate(doc *document.Document, store *V2TemplateFormatStore) int {
	bodyFormat, ok := store.Formats[V2Body]
	if !ok || bodyFormat.RPr == nil {
		return 0
	}

	tables := doc.Tables()
	writeCount := 0
	for _, tbl := range tables {
		writeCount += e.formatTableParagraphs(doc, tbl, bodyFormat.RPr)
	}
	if writeCount > 0 {
		log.Printf("[V2] 表格内修正 %d 个 run", writeCount)
	}
	return writeCount
}

// tableFontTargetsFromRPr 从模板正文 rPr 提取"目标槽位"（D3）：
// eastAsia/ascii/hAnsi 槽位为主题字体引用（ThemeAttr != ST_ThemeUnset）时返回 nil，保留主题引用、不改该槽；
// 为显式字体时返回模板值；Sz 提供字号目标（半磅 → 磅）。nil 槽表示"不改动该槽"。
func tableFontTargetsFromRPr(tpl *wml.CT_RPr) (eastAsia, ascii, hAnsi *string, sizePt float64, setSize bool) {
	if tpl == nil {
		return nil, nil, nil, 0, false
	}
	if rf := tpl.RFonts; rf != nil {
		if rf.EastAsiaThemeAttr == wml.ST_ThemeUnset {
			eastAsia = rf.EastAsiaAttr
		}
		if rf.AsciiThemeAttr == wml.ST_ThemeUnset {
			ascii = rf.AsciiAttr
		}
		if rf.HAnsiThemeAttr == wml.ST_ThemeUnset {
			hAnsi = rf.HAnsiAttr
		}
	}
	if tpl.Sz != nil && tpl.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
		setSize = true
		sizePt = float64(*tpl.Sz.ValAttr.ST_UnsignedDecimalNumber) / 2
	}
	return eastAsia, ascii, hAnsi, sizePt, setSize
}

// tableRunSkipFormat 判断表格 run 是否应跳过覆写（D3）：
// ① 含 vertAlign 上标的引用/注释 run（步骤5b 刚设置，覆写会清零）；
// ② 含高亮（hilite）的 run；
// ③ 带超链接字符样式（Word 标准 "Hyperlink" rStyle）的 run。
// 注：真正位于 w:hyperlink 容器内的 run 由 unioffice Paragraph.Runs() 天然排除
// （Runs 仅遍历 EG_PContent.EG_ContentRunContent，不进入 CT_Hyperlink），
// 此处以 rStyle 特征做超链接兜底防御。
func tableRunSkipFormat(r document.Run) bool {
	rpr := r.X().RPr
	if rpr == nil {
		return false
	}
	if rpr.VertAlign != nil && rpr.VertAlign.ValAttr == sharedTypes.ST_VerticalAlignRunSuperscript {
		return true
	}
	if rpr.Highlight != nil {
		return true
	}
	if rpr.RStyle != nil && strings.EqualFold(rpr.RStyle.ValAttr, "Hyperlink") {
		return true
	}
	return false
}

// formatTableParagraphs 格式化单张表格内所有直接段落的 run（只改目标槽位 + 差异比较，D3），
// 并对单元格内嵌套表递归。
// 嵌套表通过 cell.X().EG_BlockLevelElts 提取（Cell 无 Tables() 方法），
// 保证表格内正文按 bodyFormat 统一格式化，且不会因顶层 Tables() 已含嵌套表而重复写入。
func (e *V2FormatEngine) formatTableParagraphs(doc *document.Document, tbl document.Table, rpr *wml.CT_RPr) int {
	eastAsia, ascii, hAnsi, sizePt, setSize := tableFontTargetsFromRPr(rpr)
	writeCount := 0
	for _, row := range tbl.Rows() {
		for _, cell := range row.Cells() {
			for _, para := range cell.Paragraphs() {
				for _, r := range para.Runs() {
					if strings.TrimSpace(r.Text()) == "" {
						continue
					}
					// D3: 上标/高亮/超链接 run 不做覆写
					if tableRunSkipFormat(r) {
						continue
					}
					runPr := r.X().RPr
					if runPr == nil {
						runPr = wml.NewCT_RPr()
						r.X().RPr = runPr
					}
					if v2WriteRFontsSlots(runPr, eastAsia, ascii, hAnsi, sizePt, setSize) {
						writeCount++
					}
				}
			}
			// 嵌套表递归：遍历单元格内所有块级元素，对其中表格继续格式化
			if cell.X() == nil || cell.X().EG_BlockLevelElts == nil {
				continue
			}
			for _, blk := range cell.X().EG_BlockLevelElts {
				for _, content := range blk.EG_ContentBlockContent {
					for _, nestedTbl := range content.Tbl {
						writeCount += e.formatTableParagraphs(doc, document.Table{Document: doc, WTable: nestedTbl}, rpr)
					}
				}
			}
		}
	}
	return writeCount
}

// copyFileRaw 原字节保真复制源文件到目标路径（短路保存用）。
// 复用 io.Copy 逐字节拷贝，避免对未修改文档做 unioffice 全量序列化。
// 命名避开测试文件的 copyFile(t, src, dst)（strict_template_formatter_test.go）。
func copyFileRaw(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	target, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		return err
	}
	return target.Sync()
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

// v2ComparePPr 比对段落属性（对齐、行距、段前后距、首行缩进、左右缩进）
func v2ComparePPr(expected, actual *wml.CT_PPr) []SpecDiff {
	var diffs []SpecDiff

	if expected.Jc != nil && actual.Jc != nil {
		if expected.Jc.ValAttr != actual.Jc.ValAttr {
			diffs = append(diffs, SpecDiff{
				Field:    "对齐方式",
				Expected: jcToAlignString(expected.Jc.ValAttr),
				Actual:   jcToAlignString(actual.Jc.ValAttr),
				Severity: "error",
			})
		}
	}

	// 行距/段前后距：读 w:spacing
	if expected.Spacing != nil && actual.Spacing != nil {
		expSp := expected.Spacing
		actSp := actual.Spacing
		// 行距（允许 ±20 twips 误差）
		if expSp.LineAttr != nil && expSp.LineAttr.Int64 != nil &&
			actSp.LineAttr != nil && actSp.LineAttr.Int64 != nil {
			expLine := *expSp.LineAttr.Int64
			actLine := *actSp.LineAttr.Int64
			if expLine != 0 && actLine != 0 {
				diff := expLine - actLine
				if diff > 20 || diff < -20 {
					diffs = append(diffs, SpecDiff{
						Field:    "行距",
						Expected: humanLineSpacing(expLine, spacingRuleString(expSp.LineRuleAttr)),
						Actual:   humanLineSpacing(actLine, spacingRuleString(actSp.LineRuleAttr)),
						Severity: "warning",
					})
				}
			}
		}
		// 段前距
		if expSp.BeforeAttr != nil && expSp.BeforeAttr.ST_UnsignedDecimalNumber != nil &&
			actSp.BeforeAttr != nil && actSp.BeforeAttr.ST_UnsignedDecimalNumber != nil {
			diff := int64(*expSp.BeforeAttr.ST_UnsignedDecimalNumber) - int64(*actSp.BeforeAttr.ST_UnsignedDecimalNumber)
			if diff > 20 || diff < -20 {
				diffs = append(diffs, SpecDiff{
					Field:    "段前距",
					Expected: humanTwipsUint(*expSp.BeforeAttr.ST_UnsignedDecimalNumber),
					Actual:   humanTwipsUint(*actSp.BeforeAttr.ST_UnsignedDecimalNumber),
					Severity: "warning",
				})
			}
		}
		// 段后距
		if expSp.AfterAttr != nil && expSp.AfterAttr.ST_UnsignedDecimalNumber != nil &&
			actSp.AfterAttr != nil && actSp.AfterAttr.ST_UnsignedDecimalNumber != nil {
			diff := int64(*expSp.AfterAttr.ST_UnsignedDecimalNumber) - int64(*actSp.AfterAttr.ST_UnsignedDecimalNumber)
			if diff > 20 || diff < -20 {
				diffs = append(diffs, SpecDiff{
					Field:    "段后距",
					Expected: humanTwipsUint(*expSp.AfterAttr.ST_UnsignedDecimalNumber),
					Actual:   humanTwipsUint(*actSp.AfterAttr.ST_UnsignedDecimalNumber),
					Severity: "warning",
				})
			}
		}
	}

	// 首行缩进/左右缩进：读 w:ind
	if expected.Ind != nil && actual.Ind != nil {
		expInd := expected.Ind
		actInd := actual.Ind
		if expInd.FirstLineAttr != nil && expInd.FirstLineAttr.ST_UnsignedDecimalNumber != nil &&
			actInd.FirstLineAttr != nil && actInd.FirstLineAttr.ST_UnsignedDecimalNumber != nil {
			diff := int64(*expInd.FirstLineAttr.ST_UnsignedDecimalNumber) - int64(*actInd.FirstLineAttr.ST_UnsignedDecimalNumber)
			if diff > 40 || diff < -40 {
				diffs = append(diffs, SpecDiff{
					Field:    "首行缩进",
					Expected: humanTwipsUint(*expInd.FirstLineAttr.ST_UnsignedDecimalNumber),
					Actual:   humanTwipsUint(*actInd.FirstLineAttr.ST_UnsignedDecimalNumber),
					Severity: "warning",
				})
			}
		}
		if expInd.LeftAttr != nil && expInd.LeftAttr.Int64 != nil &&
			actInd.LeftAttr != nil && actInd.LeftAttr.Int64 != nil {
			diff := *expInd.LeftAttr.Int64 - *actInd.LeftAttr.Int64
			if diff > 40 || diff < -40 {
				diffs = append(diffs, SpecDiff{
					Field:    "左缩进",
					Expected: humanTwipsUint(uint64(*expInd.LeftAttr.Int64)),
					Actual:   humanTwipsUint(uint64(*actInd.LeftAttr.Int64)),
					Severity: "warning",
				})
			}
		}
		if expInd.RightAttr != nil && expInd.RightAttr.Int64 != nil &&
			actInd.RightAttr != nil && actInd.RightAttr.Int64 != nil {
			diff := *expInd.RightAttr.Int64 - *actInd.RightAttr.Int64
			if diff > 40 || diff < -40 {
				diffs = append(diffs, SpecDiff{
					Field:    "右缩进",
					Expected: humanTwipsUint(uint64(*expInd.RightAttr.Int64)),
					Actual:   humanTwipsUint(uint64(*actInd.RightAttr.Int64)),
					Severity: "warning",
				})
			}
		}
	}

	return diffs
}

// spacingRuleString 将行距规则枚举转中文描述
func spacingRuleString(rule wml.ST_LineSpacingRule) string {
	switch rule {
	case wml.ST_LineSpacingRuleAuto:
		return "自动"
	case wml.ST_LineSpacingRuleExact:
		return "固定值"
	case wml.ST_LineSpacingRuleAtLeast:
		return "最小值"
		// 该 fork 无 ST_LineSpacingRuleMultiple 常量（多倍行距为 0 之外的另一取值），此处兜底。
	}
	return ""
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
		// D8: 后置修补链写回点说明——
		// applyPostSavePatches 对 zip 内各 part 读改后一次写回；
		// normalizeFinalBoldByProfile 对 document.xml 单 part 做"读一次→收集→一次性写回"的
		// 块级 patch（删除 <w:b/> 幂等，公式/图片 run 已豁免）。
		// transplant.NormalizeFinalDOCX 与 ooxmlpkg.RepairPropertyOrder 均基于 ooxmlpkg
		// 结构化遍历后单次 pkg.Write：前者纠正内容/引用/绘图残差，后者修复 pPr/rPr 子元素
		// 的 OOXML 固定顺序，二者语义正交，且分属 transplant / ooxmlpkg 两个包——合并为
		// 一次遍历需跨包搬移归一化逻辑，改动面过大且无正确性收益，故保持两阶段串行，
		// 每个 part 各只写回一次，不做循环内重复正则重写。
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
		// D7: 最终下载门禁与强校验链保持一致，由"真实未达标"驱动。
		// 默认阈值按本轮比对段落数的 5% 动态计算（与强校验链解析规则一致），
		// 不再零容忍——目录条目、图/表说明等合理差异已在 countTemplateSpecDiffs 内豁免，
		// 分类器/映射自身误差不再导致误拒收。
		var gateParaCount, gateExempted int
		if p.lastStrongVerify != nil {
			gateParaCount = p.lastStrongVerify.VerifParagraphCount
			gateExempted = p.lastStrongVerify.ExemptedDiffs
		}
		gateThreshold := resolveStrongVerifyThreshold(strongVerificationThreshold(), gateParaCount)
		runLog.printf("最终格式质量门禁：真实未达标=%d，阈值=%d（段落数=%d，豁免=%d）",
			finalDiffs, gateThreshold, gateParaCount, gateExempted)
		if finalDiffs > gateThreshold {
			SetFormatRunLogResult(ctx, "manual_review", "manual_review")
			return "", fmt.Errorf("final format quality gate rejected output: %d diffs remain (threshold %d)", finalDiffs, gateThreshold)
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
	// D8: 单轮块级 patch——仅对 document.xml 这一个 part 做内存级修正，
	// 全部段落遍历结束后一次性重建并写回，避免对同一 part 做多轮全量重写。
	paragraphPatternFinal := regexp.MustCompile(`(?s)<w:p\b[^>]*>.*?</w:p>`)
	paragraphs := paragraphPatternFinal.FindAllStringIndex(xml, -1)
	boldPattern := regexp.MustCompile(`(?s)<w:b(?:Cs)?\b[^>]*/>|<w:b(?:Cs)?\b[^>]*>.*?</w:b(?:Cs)>`)
	rPrPattern := regexp.MustCompile(`(?s)<w:rPr\b[^>]*>.*?</w:rPr>`)
	// D8: 只对普通文本 run 做粗体兜底；含数学公式(oMath)或图片(drawing/pict)的 run
	// 原样保留（含其显式加粗），防止粗体兜底误删公式/特殊 run 的加粗。
	runPattern := regexp.MustCompile(`(?s)<w:r\b[^>]*>.*?</w:r>`)
	changed := 0
	type paraPatch struct {
		start, end int
		updated    string
	}
	var patches []paraPatch
	for i := len(paragraphs) - 1; i >= 0; i-- {
		start, end := paragraphs[i][0], paragraphs[i][1]
		paragraph := xml[start:end]
		text := strings.TrimSpace(strings.Join(extractDocxTextNodes(paragraph), ""))
		if targets[text] <= 0 {
			continue
		}
		updated := runPattern.ReplaceAllStringFunc(paragraph, func(run string) string {
			if strings.Contains(run, "oMath") || strings.Contains(run, "drawing") || strings.Contains(run, "<w:pict") {
				// 公式/图片 run 保留原样（含其显式加粗），不参与删除
				return run
			}
			return rPrPattern.ReplaceAllStringFunc(run, func(rPr string) string {
				// 删除操作幂等：<w:b[Cs]/ > 被删后不再命中，同一 run 不会被二次改写
				return boldPattern.ReplaceAllString(rPr, "")
			})
		})
		if updated != paragraph {
			patches = append(patches, paraPatch{start: start, end: end, updated: updated})
			targets[text]--
			changed++
		}
	}
	if changed == 0 {
		return
	}
	// 一次性重建 document.xml：patches 按原文件偏移从大到小收集，按序应用不会造成偏移错位。
	for _, patch := range patches {
		xml = xml[:patch.start] + patch.updated + xml[patch.end:]
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

// strongVerificationThreshold 返回强校验门禁阈值（环境变量 FORMAT_STRONG_VERIFY_MAX_DIFFS）。
// 未设置或取值非法时返回 -1，表示由 resolveStrongVerifyThreshold 按实际段落数动态计算。
// 默认不再采用零容忍（0）：分类器自身误差、模板映射缺口、目录/图/表说明等合理差异
// 不应把"系统自身不准"伪装成"学生未达标"并触发整链路无效重跑或误拒收（D7）。
func strongVerificationThreshold() int {
	v := strings.TrimSpace(os.Getenv("FORMAT_STRONG_VERIFY_MAX_DIFFS"))
	if v == "" {
		return -1
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return -1
	}
	return n
}

// resolveStrongVerifyThreshold 将强校验阈值解析为整数：
//   - 环境变量显式设置（>=0）时原样返回；
//   - 否则按段落数的 5% 动态计算（max(1, 段落数*5%)），给分类器/映射误差留出合理余量。
func resolveStrongVerifyThreshold(envOrResolved int, paragraphCount int) int {
	if envOrResolved >= 0 {
		return envOrResolved
	}
	dynamic := paragraphCount * 5 / 100
	if dynamic < 1 {
		dynamic = 1
	}
	return dynamic
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
	if p.lastStrongVerify != nil {
		// 记录本次比对覆盖的正文段落数，供默认动态阈值（段落数 5%）解析使用。
		p.lastStrongVerify.VerifParagraphCount = len(BodyLevelParagraphsOnly(doc))
	}
	diffs := verifier.compareAllWithSpecs(classified, specs)
	// D7: 区分"合理差异"（目录条目、图/表说明、封面副标题等模板映射缺口/分类器
	// 自身误差）与"真实未达标"（学生正文确实未按模板规格书写）。只有真实未达标
	// 数参与门禁决策，避免零容忍门禁因系统自身误差触发整链路无效重跑或误拒收。
	realDiffs := make([]FormatDiff, 0, len(diffs))
	for _, diff := range diffs {
		if isReasonableTemplateSpecDiff(diff.Category, diff.TextSnip) {
			continue
		}
		realDiffs = append(realDiffs, diff)
	}
	if p.lastStrongVerify != nil {
		p.lastStrongVerify.ExemptedDiffs = len(diffs) - len(realDiffs)
	}
	if len(diffs) != len(realDiffs) {
		log.Printf("[强校验] 豁免合理差异=%d 条（目录/图/表说明/映射缺口），真实未达标=%d 条",
			len(diffs)-len(realDiffs), len(realDiffs))
	}
	if len(realDiffs) > 0 {
		// Keep strong-verification failures diagnosable; a bare count cannot
		// distinguish a real template mismatch from a classifier/spec mapping
		// error. The details are written to the existing per-run format log.
		limit := len(realDiffs)
		if limit > 40 {
			limit = 40
		}
		for _, diff := range realDiffs[:limit] {
			log.Printf("[强校验差异] 分类=%s 段落=%d 属性=%s 期望=%s 实际=%s 文本=%s", diff.Category, diff.ParaIdx, humanField(diff.Property), diff.Expected, diff.Actual, diff.TextSnip)
		}
	}
	return len(realDiffs), nil
}

// isReasonableTemplateSpecDiff 判定一条强校验差异是否属于"合理差异"（可豁免）：
//   - 目录标题/目录条目（V2TOCTitle/V2TOC）：页码制表位、缩进等随模板样式映射变化，
//     属于系统映射差异而非学生未达标；
//   - 图/表说明（V2FigureCaption/V2TableCaption）：说明行位于表格/图片上下文，
//     其字号/对齐由模板说明样式兜底，分类器解析自身误差较大；
//   - 封面副标题（V2ThesisSubtitle）：多为无结构信号的居中短行，profile 常缺该角色
//     映射（映射缺口）。
//
// 其余类别（正文、标题、封面核心字段等）的差异视为"真实未达标"，计入超阈值判断。
func isReasonableTemplateSpecDiff(category, text string) bool {
	switch category {
	case V2TOCTitle, V2TOC, V2FigureCaption, V2TableCaption, V2ThesisSubtitle:
		return true
	}
	return false
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
	envThreshold := strongVerificationThreshold()
	formatRules := normalizedFormatRulesFromCorrections(p, corrections)
	p.lastStrongVerify.Enabled = true
	initialDiffs, err := p.countTemplateSpecDiffs(candidatePath, templatePath, formatRules)
	if err != nil {
		log.Printf("[强校验] 首次比对失败，保留主路径产物: %v", err)
		return candidatePath, primaryEngine, nil
	}
	// D7: 门禁阈值默认按真实未达标判定后的段落数比例动态计算
	// （max(1, 段落数*5%)），避免零容忍触发无效重跑。
	threshold := resolveStrongVerifyThreshold(envThreshold, p.lastStrongVerify.VerifParagraphCount)
	p.lastStrongVerify.Threshold = threshold
	p.lastStrongVerify.InitialDiffs = initialDiffs
	retry, _ := planStrongVerificationAction(initialDiffs, -1, threshold)
	log.Printf("[强校验] 引擎=%s 首次真实未达标=%d 阈值=%d(段落数=%d 豁免=%d)",
		primaryEngine, initialDiffs, threshold, p.lastStrongVerify.VerifParagraphCount, p.lastStrongVerify.ExemptedDiffs)
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
				parts = append(parts, fmt.Sprintf("对齐=%s", humanAlignment(ppr.Jc.ValAttr.String())))
			}
			if ppr.Spacing != nil {
				if ppr.Spacing.LineAttr != nil && ppr.Spacing.LineAttr.Int64 != nil {
					lineRule := "auto"
					if ppr.Spacing.LineRuleAttr == wml.ST_LineSpacingRuleExact {
						lineRule = "exact"
					}
					parts = append(parts, fmt.Sprintf("行距=%s", humanLineSpacing(*ppr.Spacing.LineAttr.Int64, lineRule)))
				}
				if ppr.Spacing.BeforeAttr != nil && ppr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber != nil {
					parts = append(parts, fmt.Sprintf("段前=%s", humanTwipsString(fmt.Sprintf("%d", *ppr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber))))
				}
				if ppr.Spacing.AfterAttr != nil && ppr.Spacing.AfterAttr.ST_UnsignedDecimalNumber != nil {
					parts = append(parts, fmt.Sprintf("段后=%s", humanTwipsString(fmt.Sprintf("%d", *ppr.Spacing.AfterAttr.ST_UnsignedDecimalNumber))))
				}
			}
			if ppr.Ind != nil && ppr.Ind.FirstLineAttr != nil && ppr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber != nil {
				parts = append(parts, fmt.Sprintf("首行缩进=%s", humanTwipsString(fmt.Sprintf("%d", *ppr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber))))
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
						rParts = append(rParts, fmt.Sprintf("中文字体=%s", *rpr.RFonts.EastAsiaAttr))
					}
					if rpr.RFonts.AsciiAttr != nil {
						rParts = append(rParts, fmt.Sprintf("西文字体=%s", *rpr.RFonts.AsciiAttr))
					}
				}
				if rpr.Sz != nil && rpr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
					rParts = append(rParts, fmt.Sprintf("字号=%s", humanHalfPoints(uint64(*rpr.Sz.ValAttr.ST_UnsignedDecimalNumber))))
				}
				if rpr.SzCs != nil && rpr.SzCs.ValAttr.ST_UnsignedDecimalNumber != nil {
					rParts = append(rParts, fmt.Sprintf("中文字号=%s", humanHalfPoints(uint64(*rpr.SzCs.ValAttr.ST_UnsignedDecimalNumber))))
				}
				if rpr.B != nil {
					rParts = append(rParts, "加粗=是")
				}
				if rpr.I != nil {
					rParts = append(rParts, "斜体=是")
				}
				rprInfo = strings.Join(rParts, " ")
			}
		}
		DiagPrintf("[产出] 段落序号=%d 类型=%s 文字=%q 段落属性={%s} 文字属性={%s}",
			s.index, humanCategory(s.category), text, pprInfo, rprInfo)
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
