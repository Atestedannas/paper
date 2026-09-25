package fileprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gitee.com/greatmusicians/unioffice/document"

	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

type formatRunLog struct {
	logger       *log.Logger
	file         *os.File
	path         string
	started      time.Time
	finalStatus  string
	finalStage   string
	finishedOnce bool
}

type formatRunLogContextKey struct{}

func newFormatRunLog(studentPath, templatePath string) (*formatRunLog, error) {
	return createFormatRunLog(studentPath, templatePath, "")
}

func createFormatRunLog(studentPath, templatePath, runID string) (*formatRunLog, error) {
	dir, err := formatLogDirectory()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	base := sanitizeFormatLogName(strings.TrimSuffix(filepath.Base(studentPath), filepath.Ext(studentPath)))
	if base == "" {
		base = "paper"
	}
	if runID != "" {
		base += "_" + sanitizeFormatLogName(runID)
	}
	started := time.Now()
	path := filepath.Join(dir, fmt.Sprintf("%s_%s.log", started.Format("20060102-150405.000000000"), base))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	l := &formatRunLog{logger: log.New(f, "", 0), file: f, path: path, started: started}
	l.logger.Println("论文格式修改流程诊断日志")
	l.logger.Printf("开始时间：%s", started.Format("2006-01-02 15:04:05.000"))
	if runID != "" {
		l.logger.Printf("任务编号：%s", runID)
	}
	l.logger.Printf("学生论文：%s", studentPath)
	l.logger.Printf("学校模板：%s", templatePath)
	l.logger.Println("单位说明：本日志中的格式值均已换算为中文常用单位——字号用磅表示，行距用倍数或磅表示，缩进用字符数或磅表示，页边距与页眉页脚距用毫米表示，便于直接阅读核对。")
	return l, nil
}

func formatLogDirectory() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("PAPER_FORMAT_LOG_DIR")); configured != "" {
		return filepath.Abs(configured)
	}
	if configured := strings.TrimSpace(os.Getenv("LOG_PATH")); configured != "" {
		logRoot, err := filepath.Abs(configured)
		if err != nil {
			return "", err
		}
		return filepath.Join(logRoot, "geshi"), nil
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := workingDirectory
	for {
		if info, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil && !info.IsDir() {
			return filepath.Join(dir, "logs", "geshi"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join(workingDirectory, "logs", "geshi"), nil
		}
		dir = parent
	}
}

func sanitizeFormatLogName(value string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, value)
}

// BeginFormatRunLog creates one job-scoped readable log for the real workflow.
func BeginFormatRunLog(ctx context.Context, studentPath, templatePath, runID string) (context.Context, string, error) {
	l, err := createFormatRunLog(studentPath, templatePath, runID)
	if err != nil {
		return ctx, "", err
	}
	return withFormatRunLog(ctx, l), l.path, nil
}

func FinishFormatRunLog(ctx context.Context, err error) {
	if l := formatLogFromContext(ctx); l != nil {
		l.finish(err)
	}
}

func FormatLogSection(ctx context.Context, title string) {
	if l := formatLogFromContext(ctx); l != nil {
		l.section(title)
	}
}

func FormatLogPrintf(ctx context.Context, format string, args ...interface{}) {
	if l := formatLogFromContext(ctx); l != nil {
		l.printf(format, args...)
	}
}

func FormatLogProfile(ctx context.Context, profile *templateprofile.Profile, heading string) {
	if l := formatLogFromContext(ctx); l != nil {
		l.profile(profile, heading)
	}
}

// SetFormatRunLogResult records the quality-gate result separately from
// operational errors so a completed manual-review job is never labelled as a pass.
func SetFormatRunLogResult(ctx context.Context, status, stage string) {
	if l := formatLogFromContext(ctx); l != nil {
		l.finalStatus = strings.TrimSpace(status)
		l.finalStage = strings.TrimSpace(stage)
	}
}

func withFormatRunLog(ctx context.Context, l *formatRunLog) context.Context {
	if l == nil {
		return ctx
	}
	return context.WithValue(ctx, formatRunLogContextKey{}, l)
}

func formatLogFromContext(ctx context.Context) *formatRunLog {
	if ctx == nil {
		return nil
	}
	l, _ := ctx.Value(formatRunLogContextKey{}).(*formatRunLog)
	return l
}

func (l *formatRunLog) section(title string) {
	if l != nil {
		l.logger.Printf("\n========== %s ==========", title)
	}
}

func (l *formatRunLog) printf(format string, args ...interface{}) {
	if l != nil {
		l.logger.Printf(format, args...)
	}
}

// println 写入一整行文本（不附带格式参数）。供 DiagPrintf 兜底落日志使用。
func (l *formatRunLog) println(s string) {
	if l != nil {
		l.logger.Print(s)
	}
}

func (l *formatRunLog) finish(err error) {
	if l == nil || l.finishedOnce {
		return
	}
	l.finishedOnce = true
	l.section("流程结束")
	if err != nil {
		l.printf("结果：失败")
		l.printf("错误：%v", err)
	} else if l.finalStatus != "" {
		l.printf("流程执行：完成")
		l.printf("质量门禁状态：%s", l.finalStatus)
		if l.finalStage != "" {
			l.printf("下载阶段：%s", l.finalStage)
		}
	} else {
		l.printf("流程执行：完成；未记录最终质量门禁状态")
	}
	l.printf("总耗时：%s", time.Since(l.started).Round(time.Millisecond))
	l.printf("日志文件：%s", l.path)
	_ = l.file.Close()
}

func (l *formatRunLog) profile(profile *templateprofile.Profile, heading string) {
	if l == nil {
		return
	}
	l.section(heading)
	if profile == nil {
		l.printf("未生成 Profile。")
		return
	}
	l.printf("来源文件：%s", profile.Source)
	l.printf("模板摘要 SHA：%s", profile.TemplateSHA)
	l.printf("Profile 版本：%s；提取置信度：%.1f%%", profile.Version, profile.Confidence*100)
	l.printf("页面：%s", humanPageSetup(profile.PageSetup))
	if body, ok := profile.Styles["body"]; ok && body.BoldEvidence != "" {
		l.printf("正文加粗规则依据：%s；排除模板说明段落：%d", body.BoldEvidence, len(profile.ExcludedSamples))
	}

	keys := sortedStyleKeys(profile.SectionFormats)
	l.printf("分区格式数量：%d", len(keys))
	for _, key := range keys {
		l.printf("- %s（%s）：%s", humanCategory(key), key, humanStyleRule(profile.SectionFormats[key]))
	}

	styleKeys := sortedStyleKeys(profile.Styles)
	l.printf("命名样式规则数量：%d", len(styleKeys))
	for _, key := range styleKeys {
		l.printf("- %s（%s）：%s", humanCategory(key), key, humanStyleRule(profile.Styles[key]))
	}

	sectionKeys := make([]string, 0, len(profile.Sections))
	for key := range profile.Sections {
		sectionKeys = append(sectionKeys, key)
	}
	sort.Strings(sectionKeys)
	l.printf("区段规则数量：%d", len(sectionKeys))
	for _, key := range sectionKeys {
		r := profile.Sections[key]
		l.printf("- %s（%s）：段前分页=%s，分节=%s，识别依据=%s",
			humanCategory(key), key, yesNo(r.PageBreakBefore), yesNo(r.SectionBreak), valueOrMissing(r.DetectedFrom))
	}

	l.printf("普通页眉：%s", humanHeaderFooter(profile.Header))
	l.printf("首页页眉：%s", humanHeaderFooter(profile.HeaderFirst))
	l.printf("偶数页页眉：%s", humanHeaderFooter(profile.HeaderEven))
	l.printf("普通页脚：%s", humanHeaderFooter(profile.Footer))
	l.printf("首页页脚：%s", humanHeaderFooter(profile.FooterFirst))
	l.printf("偶数页页脚：%s", humanHeaderFooter(profile.FooterEven))
	l.printf("其他规则：引用=%s，参考文献标准=%s，表格=%s，标题编号=%s，页码=%s，必需区段=%s",
		valueOrMissing(profile.RulePack.CitationStyle),
		valueOrMissing(profile.RulePack.ReferenceStandard),
		valueOrMissing(profile.RulePack.TableStyle),
		valueOrMissing(profile.RulePack.HeadingNumbering),
		valueOrMissing(profile.RulePack.PageNumbering),
		strings.Join(profile.RulePack.RequiredSections, "、"))

	// 完整 Profile 原始值（含 FontSizeHalfPt/BeforeTwips 等机器字段）只写入 agent 专用 NDJSON 日志
	// （debugLog → agent_trace），主流程 human 日志保持全中文可读，仅保留一句指引。
	l.printf("完整 Profile 原始值已写入 agent 专用诊断日志（NDJSON），本日志不重复输出机器格式；上方为全部人类可读的中文摘要。")
	if raw, err := json.MarshalIndent(profile, "", "  "); err == nil {
		debugLog("format_run_log.go:profile", "PROFILE_RAW_JSON", map[string]interface{}{
			"source":      profile.Source,
			"profileJSON": string(raw),
		})
	}
}

func (l *formatRunLog) templateSamples(store *V2TemplateFormatStore) {
	if l == nil {
		return
	}
	l.section("第2步：从模板中采样提取格式模板")
	keys := make([]string, 0, len(store.Formats))
	for key := range store.Formats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	l.printf("共提取 %d 类格式；每类采用第一个具有段落属性的模板段落。", len(keys))
	for _, key := range keys {
		sample := store.Formats[key]
		l.printf("- %s（%s），模板段落 #%d，样本文字=%q",
			humanCategory(key), key, sample.SampleParaIndex+1, compactText(sample.SampleText, 120))
		l.printf("  段落/文字格式：%s", humanParagraphSpec(sample.SampleSpec))
		l.printf("  XML 节点：段落属性=%s，正文文字属性=%s，标签文字属性=%s",
			yesNo(sample.PPr != nil), yesNo(sample.RPr != nil), yesNo(sample.LabelRPr != nil))
	}
}

func (l *formatRunLog) classifications(classified []V2ClassifiedPara) {
	if l == nil {
		return
	}
	l.section("第6步：确定性段落分类")
	counts := map[string]int{}
	for _, cp := range classified {
		counts[cp.Type]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		l.printf("- %s（%s）：%d 段", humanCategory(key), key, counts[key])
	}
	l.printf("以下逐段列出分类结果，便于发现“目录被当正文”等误判：")
	for _, cp := range classified {
		l.printf("  段落 #%04d -> %s（%s） | %q",
			cp.ParaIdx+1, humanCategory(cp.Type), cp.Type, compactText(cp.Text, 160))
	}
}

func (l *formatRunLog) resolvedRules(specs map[string]ParagraphFormatSpec) {
	if l == nil {
		return
	}
	l.section("第7b步：智能格式化采用的最终规则")
	keys := make([]string, 0, len(specs))
	for key := range specs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	l.printf("最终解析出 %d 类段落规则（模板 Profile/采样/用户修正仲裁后的实际写入值）：", len(keys))
	for _, key := range keys {
		l.printf("- %s（%s）：%s", humanCategory(key), key, humanParagraphSpec(specs[key]))
	}
}

func (l *formatRunLog) repair(result RepairResult) {
	if l == nil {
		return
	}
	l.printf("修复代理：执行轮数=%d，初始差异=%d，自动修复=%d，剩余差异=%d，需要人工复核=%s，发生回退=%s",
		result.Rounds, result.InitialDiffs, result.TotalFixes, result.FinalDiffs,
		yesNo(result.NeedsManualReview), yesNo(result.Regressed))
	for _, item := range result.Diagnostics {
		l.printf("  诊断：%s", item)
	}
}

func (l *formatRunLog) diffReport(report *DocDiffReport) {
	if l == nil {
		return
	}
	l.section("第9步：格式差异报告")
	if report == nil {
		l.printf("未生成差异报告。")
		return
	}
	l.printf("扫描段落=%d，错误=%d，警告=%d，有差异的段落=%d",
		report.TotalParas, report.ErrorCount, report.WarningCount, len(report.ParaDiffs))
	for _, para := range report.ParaDiffs {
		l.printf("- 段落 #%d，%s（%s），文字=%q",
			para.ParaIndex+1, humanCategory(para.Category), para.Category, compactText(para.Text, 120))
		for _, diff := range para.Diffs {
			l.printf("  [%s] %s：模板要求=%s；修改后=%s",
				humanSeverity(diff.Severity), humanField(diff.Field), diff.Expected, diff.Actual)
		}
	}
}

func sectionLevelStats(doc *document.Document) (tables, sectionBreaks, citationRuns int) {
	tables = len(doc.Tables())
	for _, para := range doc.Paragraphs() {
		if pPr := para.X().PPr; pPr != nil && pPr.SectPr != nil {
			sectionBreaks++
		}
		for _, run := range para.Runs() {
			if isCitationOrAnnotation(run.Text()) {
				citationRuns++
			}
		}
	}
	return
}

func humanParagraphSpec(s ParagraphFormatSpec) string {
	parts := []string{
		"中文字体=" + valueOrMissing(s.FontEastAsia),
		"西文字体=" + valueOrMissing(s.FontAscii),
		"字号=" + humanHalfPoints(s.FontSizeHalfPt),
		"加粗=" + yesNo(s.Bold),
		"斜体=" + yesNo(s.Italic),
		"下划线=" + yesNo(s.Underline),
	}
	if s.AlignmentSet {
		parts = append(parts, "对齐="+humanAlignment(jcToAlignString(s.Alignment)))
	} else {
		parts = append(parts, "对齐=未直接指定")
	}
	parts = append(parts,
		"行距="+humanLineSpacing(s.LineSpacingVal, fmt.Sprint(s.LineSpacingRule)),
		"段前="+humanTwipsUint(s.SpaceBefore),
		"段后="+humanTwipsUint(s.SpaceAfter),
		"首行缩进="+humanTwipsUint(s.FirstLineIndent),
		"左缩进="+humanTwipsUint(s.IndentLeft),
		"右缩进="+humanTwipsUint(s.IndentRight),
		"大纲级别="+outlineValue(s.OutlineLevel),
		"段前分页="+yesNo(s.PageBreak),
		"与下段同页="+yesNo(s.KeepWithNext),
		"段内不分页="+yesNo(s.KeepLines),
		"样本数="+strconv.Itoa(s.SampleCount),
	)
	return strings.Join(parts, "；")
}

func humanStyleRule(s templateprofile.StyleRule) string {
	halfPt, _ := strconv.ParseUint(s.FontSizeHalfPt, 10, 64)
	bold := "模板未直接指定"
	if s.BoldSet {
		bold = yesNo(s.Bold)
	}
	italic := "模板未直接指定"
	if s.ItalicSet {
		italic = yesNo(s.Italic)
	}
	return strings.Join([]string{
		"中文字体=" + valueOrMissing(s.FontEastAsia),
		"西文字体=" + valueOrMissing(s.FontASCII),
		"字号=" + humanHalfPoints(halfPt),
		"加粗=" + bold,
		"斜体=" + italic,
		"对齐=" + humanAlignment(s.Alignment),
		"行距=" + humanLineSpacingString(s.Line, s.LineRule),
		"段前=" + humanTwipsString(s.BeforeTwips),
		"段后=" + humanTwipsString(s.AfterTwips),
		"首行缩进字符=" + humanHundredthChars(s.FirstLineChars),
		"首行缩进=" + humanTwipsString(s.FirstLineTwips),
		"大纲级别=" + valueOrMissing(s.OutlineLevel),
	}, "；")
}

func humanPageSetup(p templateprofile.PageSetupRule) string {
	return strings.Join([]string{
		"纸张宽=" + humanTwipsStringMM(p.PageWidthTwips),
		"纸张高=" + humanTwipsStringMM(p.PageHeightTwips),
		"方向=" + humanOrientation(p.Orientation),
		"上边距=" + humanTwipsStringMM(p.MarginTopTwips),
		"右边距=" + humanTwipsStringMM(p.MarginRightTwips),
		"下边距=" + humanTwipsStringMM(p.MarginBottomTwips),
		"左边距=" + humanTwipsStringMM(p.MarginLeftTwips),
		"页眉距边界=" + humanTwipsStringMM(p.HeaderMarginTwips),
		"页脚距边界=" + humanTwipsStringMM(p.FooterMarginTwips),
	}, "；")
}

func humanHeaderFooter(h templateprofile.HeaderFooterRule) string {
	if !h.Exists {
		return "不存在"
	}
	halfPt, _ := strconv.ParseUint(h.FontSizeHalfPt, 10, 64)
	return fmt.Sprintf("存在；文字=%q；中文字体=%s；西文字体=%s；字号=%s；PAGE页码域=%s；总页数域=%s；双线=%s；下划线=%s",
		compactText(h.Text, 160), valueOrMissing(h.FontEastAsia), valueOrMissing(h.FontAscii),
		humanHalfPoints(halfPt), yesNo(h.HasPageField), yesNo(h.HasNumPages),
		yesNo(h.HasDoubleLine), yesNo(h.HasUnderline))
}

// humanLineRuleName 将 OOXML 行距规则枚举值映射为中文可读名称。
func humanLineRuleName(rule string) string {
	switch strings.ToLower(rule) {
	case "auto":
		return "自动"
	case "exact":
		return "固定值"
	case "atleast", "at_least":
		return "最小值"
	case "multiple":
		return "多倍行距"
	default:
		return valueOrMissing(rule)
	}
}

func humanLineSpacing(value int64, rule string) string {
	if value == 0 {
		return "未直接指定"
	}
	return humanLineSpacingString(strconv.FormatInt(value, 10), rule)
}

func humanLineSpacingString(value, rule string) string {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n == 0 {
		return "未直接指定"
	}
	switch strings.ToLower(rule) {
	case "auto":
		return fmt.Sprintf("%.2f 倍", float64(n)/240)
	case "exact":
		return fmt.Sprintf("%.2f 磅（固定值）", float64(n)/20)
	case "atleast", "at_least":
		return fmt.Sprintf("至少 %.2f 磅", float64(n)/20)
	default:
		return fmt.Sprintf("%.2f 磅（%s）", float64(n)/20, humanLineRuleName(rule))
	}
}

func humanHalfPoints(n uint64) string {
	if n == 0 {
		return "未直接指定"
	}
	name := map[uint64]string{84: "初号", 72: "小初", 52: "一号", 48: "小一", 44: "二号", 36: "小二", 32: "三号", 30: "小三", 28: "四号", 24: "小四", 21: "五号", 18: "小五"}[n]
	if name != "" {
		return fmt.Sprintf("%s（%.1f 磅）", name, float64(n)/2)
	}
	return fmt.Sprintf("%.1f 磅", float64(n)/2)
}

func humanTwipsString(value string) string {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || value == "" {
		return "未直接指定"
	}
	return fmt.Sprintf("%.2f 磅", float64(n)/20)
}

func humanTwipsStringMM(value string) string {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || value == "" {
		return "未直接指定"
	}
	return fmt.Sprintf("%.2f 毫米", float64(n)*25.4/1440)
}

func humanTwipsUint(n uint64) string {
	if n == 0 {
		return "未直接指定"
	}
	return fmt.Sprintf("%.2f 磅", float64(n)/20)
}

func humanTwipsInt(n int64) string {
	if n <= 0 {
		return "未直接指定"
	}
	return fmt.Sprintf("%.2f 磅", float64(n)/20)
}

func humanHundredthChars(value string) string {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || value == "" {
		return "未直接指定"
	}
	return fmt.Sprintf("%.2f 字符", float64(n)/100)
}

func sortedStyleKeys[T ~map[string]templateprofile.StyleRule](items T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compactText(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return value
}

func valueOrMissing(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未直接指定"
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func humanAlignment(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "left", "start":
		return "左对齐"
	case "right", "end":
		return "右对齐"
	case "center":
		return "居中"
	case "both", "justify":
		return "两端对齐"
	case "distribute":
		return "分散对齐"
	case "":
		return "未直接指定"
	default:
		return value
	}
}

func humanOrientation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "landscape":
		return "横向"
	case "portrait":
		return "纵向"
	case "":
		return "未直接指定（Word 默认纵向）"
	default:
		return value
	}
}

func outlineValue(value int) string {
	if value <= 0 {
		return "正文/未直接指定"
	}
	return strconv.Itoa(value)
}

func humanSeverity(value string) string {
	if strings.EqualFold(value, "error") {
		return "错误"
	}
	if strings.EqualFold(value, "warning") {
		return "警告"
	}
	return value
}

func humanField(value string) string {
	fields := map[string]string{
		"font_east_asia":       "中文字体",
		"font_ascii":           "西文字体",
		"font_size":            "字号",
		"font_size_half_pt":    "字号原值",
		"bold":                 "加粗",
		"italic":               "斜体",
		"underline":            "下划线",
		"alignment":            "对齐方式",
		"line_spacing":         "行距",
		"line_spacing_rule":    "行距规则",
		"space_before":         "段前距",
		"space_after":          "段后距",
		"first_line_indent":    "首行缩进",
		"indent_left":          "左缩进",
		"indent_right":         "右缩进",
		"font_size_cs_half_pt": "中文字号原值",
		"outline_level":        "大纲级别",
		"page_break":           "段前分页",
		"keep_with_next":       "与下段同页",
		"keep_lines":           "段内不分页",
		"color":                "字体颜色",
		"font_size_cs":         "中文字号",
	}
	if translated := fields[value]; translated != "" {
		return translated
	}
	return value
}

func humanCategory(value string) string {
	categories := map[string]string{
		V2Cover: "封面信息", V2OriginalityDeclaration: "原创性声明",
		V2ThesisTitle: "论文题目", V2ThesisSubtitle: "论文副标题",
		V2AbstractTitle: "中文摘要标题", V2Abstract: "中文摘要正文", V2Keywords: "中文关键词",
		V2EnAbstractTitle: "英文摘要标题", V2EnAbstract: "英文摘要正文", V2EnKeywords: "英文关键词",
		V2TOCTitle: "目录标题", V2TOC: "目录条目",
		V2Heading1: "一级标题", V2Heading2: "二级标题", V2Heading3: "三级标题", V2Heading4: "四级标题",
		V2Body: "正文", V2ReferencesTitle: "参考文献标题", V2References: "参考文献条目",
		V2AcknowledgementsTitle: "致谢标题", V2Acknowledgements: "致谢正文",
		V2AppendixTitle: "附录标题", V2Appendix: "附录正文",
		V2NotesTitle: "注释标题", V2Notes: "注释正文",
		V2FigureCaption: "图题", V2TableCaption: "表题",
		V2Empty:       "空行",
		"cover_title": "封面论文题目", "cover_field": "封面字段",
		"chapter_title": "一级标题", "section_title": "二级标题",
		"subsection_title": "三级标题", "body_text": "正文",
		"abstract_body": "摘要正文", "reference_item": "参考文献条目",
	}
	if translated := categories[value]; translated != "" {
		return translated
	}
	return value
}
