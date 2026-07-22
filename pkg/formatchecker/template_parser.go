package formatchecker

import (
	"archive/zip"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/templateprofile"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

// TemplateParser 模板解析器
type TemplateParser struct{}

// NewTemplateParser 创建模板解析器
func NewTemplateParser() *TemplateParser {
	return &TemplateParser{}
}

// ── 字号映射表 ────────────────────────────────────────────────────────────

var pointsToChineseSizeName = map[float64]string{
	42:   "初号",
	36:   "小初",
	26:   "一号",
	24:   "小一号",
	22:   "二号",
	18:   "小二号",
	16:   "三号",
	15:   "小三号",
	14:   "四号",
	12:   "小四",
	10.5: "五号",
	9:    "小五号",
	7.5:  "六号",
	6.5:  "小六号",
	5.5:  "七号",
	5:    "八号",
}

func fontPointsToChineseName(pt float64) string {
	for size, name := range pointsToChineseSizeName {
		if math.Abs(pt-size) < 0.3 {
			return name
		}
	}
	return fmt.Sprintf("%.1fpt", pt)
}

// ── ParseTemplate (原有方法，改进) ────────────────────────────────────────

// ParseTemplate 从DOCX模板文件中解析格式标准
func (p *TemplateParser) ParseTemplate(templatePath string) (*FormatStandard, error) {
	doc, err := document.Open(templatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open template: %w", err)
	}
	defer doc.Close()

	styleCache := loadDocxStyleCache(templatePath)

	standard := &FormatStandard{
		Name:        "从模板导入的标准",
		Description: "根据上传的DOCX模板解析生成的格式标准",
	}

	p.parsePageSetup(doc, standard)
	p.parseHeadingStylesImproved(doc, standard, styleCache)
	p.parseParagraphStylesImproved(doc, standard, styleCache)
	p.parseReferenceStyles(doc, standard)
	p.parseHeaderContent(doc, standard)

	return standard, nil
}

// ── 页面设置 ──────────────────────────────────────────────────────────────

func (p *TemplateParser) parsePageSetup(doc *document.Document, standard *FormatStandard) {
	section := doc.BodySection()

	pgMar := section.X().PgMar
	if pgMar != nil {
		signedTwipsToCm := func(twips int64) float64 {
			return float64(twips) / 567.0
		}

		if pgMar.TopAttr.Int64 != nil {
			standard.PageSetup.MarginTop = signedTwipsToCm(*pgMar.TopAttr.Int64)
		}
		if pgMar.BottomAttr.Int64 != nil {
			standard.PageSetup.MarginBottom = signedTwipsToCm(*pgMar.BottomAttr.Int64)
		}
		if pgMar.LeftAttr.ST_UnsignedDecimalNumber != nil {
			standard.PageSetup.MarginLeft = uint64TwipsToCm(*pgMar.LeftAttr.ST_UnsignedDecimalNumber)
		}
		if pgMar.RightAttr.ST_UnsignedDecimalNumber != nil {
			standard.PageSetup.MarginRight = uint64TwipsToCm(*pgMar.RightAttr.ST_UnsignedDecimalNumber)
		}
		if pgMar.HeaderAttr.ST_UnsignedDecimalNumber != nil {
			standard.PageSetup.HeaderDistance = uint64TwipsToCm(*pgMar.HeaderAttr.ST_UnsignedDecimalNumber)
		}
		if pgMar.FooterAttr.ST_UnsignedDecimalNumber != nil {
			standard.PageSetup.FooterDistance = uint64TwipsToCm(*pgMar.FooterAttr.ST_UnsignedDecimalNumber)
		}
	}

	pgSz := section.X().PgSz
	if pgSz != nil {
		w := int64(0)
		if pgSz.WAttr != nil && pgSz.WAttr.ST_UnsignedDecimalNumber != nil {
			w = int64(*pgSz.WAttr.ST_UnsignedDecimalNumber)
		}
		if w > 11000 && w < 13000 {
			standard.PageSetup.PaperSize = "A4"
		} else {
			standard.PageSetup.PaperSize = "Custom"
		}
	}
}

func uint64TwipsToCm(twips uint64) float64 {
	return float64(twips) / 567.0
}

// ── 改进的样式提取辅助函数 ─────────────────────────────────────────────

// resolveRunFontForTemplate extracts font name from a run, falling back to
// style cache. Checks EastAsia → HAnsi → Ascii.
func resolveRunFontForTemplate(run document.Run, para document.Paragraph, sc *docxStyleCache) string {
	eastAsia, latin := resolveRunFontsForTemplate(run, para, sc)
	if eastAsia != "" {
		return eastAsia
	}
	return latin
}

func resolveRunFontsForTemplate(run document.Run, para document.Paragraph, sc *docxStyleCache) (string, string) {
	var eastAsia, latin string
	rPr := run.Properties().X()
	if rPr != nil && rPr.RFonts != nil {
		if rPr.RFonts.EastAsiaAttr != nil && *rPr.RFonts.EastAsiaAttr != "" {
			eastAsia = *rPr.RFonts.EastAsiaAttr
		}
		if rPr.RFonts.HAnsiAttr != nil && *rPr.RFonts.HAnsiAttr != "" {
			latin = *rPr.RFonts.HAnsiAttr
		}
		if latin == "" && rPr.RFonts.AsciiAttr != nil && *rPr.RFonts.AsciiAttr != "" {
			latin = *rPr.RFonts.AsciiAttr
		}
	}
	if sc != nil {
		styleID := getParagraphStyleID(para)
		if styleID != "" {
			props := sc.resolve(styleID)
			if eastAsia == "" {
				eastAsia = props.EastAsiaFont
			}
			if latin == "" {
				latin = props.AsciiFont
			}
		}
		if eastAsia == "" {
			eastAsia = sc.defaults.EastAsiaFont
		}
		if latin == "" {
			latin = sc.defaults.AsciiFont
		}
	}
	return eastAsia, latin
}

// resolveRunSizeForTemplate extracts font size in points from a run,
// falling back to paragraph rPr and then style cache.
func resolveRunSizeForTemplate(run document.Run, para document.Paragraph, sc *docxStyleCache) float64 {
	rPr := run.Properties().X()
	if rPr != nil && rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
		return float64(*rPr.Sz.ValAttr.ST_UnsignedDecimalNumber) / 2.0
	}
	if pPr := para.X().PPr; pPr != nil && pPr.RPr != nil && pPr.RPr.Sz != nil && pPr.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
		return float64(*pPr.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber) / 2.0
	}
	if sc != nil {
		styleID := getParagraphStyleID(para)
		if styleID != "" {
			props := sc.resolve(styleID)
			if props.FontSizePt > 0 {
				return props.FontSizePt
			}
		}
		if sc.defaults.FontSizePt > 0 {
			return sc.defaults.FontSizePt
		}
	}
	return 0
}

func resolveRunBold(run document.Run, para document.Paragraph, sc *docxStyleCache) bool {
	rPr := run.Properties().X()
	if rPr != nil && rPr.B != nil {
		return true
	}
	if sc != nil {
		styleID := getParagraphStyleID(para)
		if styleID != "" {
			props := sc.resolve(styleID)
			if props.Bold {
				return true
			}
		}
	}
	return false
}

// jcToString converts a wml.ST_Jc enum to human-readable alignment string.
func jcToString(jc wml.ST_Jc) string {
	switch jc {
	case wml.ST_JcCenter:
		return "center"
	case wml.ST_JcRight:
		return "right"
	case wml.ST_JcBoth:
		return "justify"
	case wml.ST_JcLeft:
		return "left"
	default:
		return "left"
	}
}

// extractAlignment returns the alignment string for a paragraph.
func extractAlignment(para document.Paragraph) string {
	pPr := para.X().PPr
	if pPr != nil && pPr.Jc != nil {
		return jcToString(pPr.Jc.ValAttr)
	}
	return "left"
}

// extractLineSpacing returns the line spacing value.
// For "auto" line rule: 240=single(1.0), 360=1.5x, 480=double(2.0).
//
//	Returns the multiplier (e.g. 1.5).
//
// For "exact" or "atLeast": value is in twips, returns points (twips/20).
func extractLineSpacing(para document.Paragraph) (float64, string) {
	pPr := para.X().PPr
	if pPr == nil || pPr.Spacing == nil {
		return 0, ""
	}
	sp := pPr.Spacing
	if sp.LineAttr == nil || sp.LineAttr.Int64 == nil {
		return 0, ""
	}
	lineVal := *sp.LineAttr.Int64
	lineRule := sp.LineRuleAttr

	switch lineRule {
	case wml.ST_LineSpacingRuleExact, wml.ST_LineSpacingRuleAtLeast:
		return float64(lineVal) / 20.0, "fixed"
	default:
		return float64(lineVal) / 240.0, "auto"
	}
}

// extractLineSpacingDisplay returns a human-readable line spacing string.
func extractLineSpacingDisplay(para document.Paragraph) string {
	val, mode := extractLineSpacing(para)
	if val == 0 {
		return ""
	}
	if mode == "auto" {
		if math.Abs(val-1.0) < 0.05 {
			return "单倍行距"
		} else if math.Abs(val-1.5) < 0.05 {
			return "1.5倍行距"
		} else if math.Abs(val-2.0) < 0.05 {
			return "2倍行距"
		}
		return fmt.Sprintf("%.1f倍行距", val)
	}
	return fmt.Sprintf("固定值%.1f磅", val)
}

func extractLineRule(para document.Paragraph) string {
	pPr := para.X().PPr
	if pPr == nil || pPr.Spacing == nil || pPr.Spacing.LineAttr == nil {
		return ""
	}
	switch pPr.Spacing.LineRuleAttr {
	case wml.ST_LineSpacingRuleExact:
		return "exact"
	case wml.ST_LineSpacingRuleAtLeast:
		return "atLeast"
	default:
		return "auto"
	}
}

// ── 改进的标题样式提取 ──────────────────────────────────────────────────

func (p *TemplateParser) parseHeadingStylesImproved(doc *document.Document, standard *FormatStandard, sc *docxStyleCache) {
	standard.HeadingStyles = make([]HeadingStyle, 0)
	foundLevels := make(map[int]bool)

	for _, para := range doc.Paragraphs() {
		styleName := para.Style()
		text := ""
		for _, run := range para.Runs() {
			text += run.Text()
		}
		text = strings.TrimSpace(text)

		level := classifyHeadingLevel(styleName, text)
		if level <= 0 || foundLevels[level] {
			continue
		}

		style := HeadingStyle{
			Level:     level,
			Name:      fmt.Sprintf("%d级标题", level),
			Alignment: extractAlignment(para),
		}

		runs := para.Runs()
		if len(runs) > 0 {
			style.FontName = resolveRunFontForTemplate(runs[0], para, sc)
			style.FontSize = resolveRunSizeForTemplate(runs[0], para, sc)
			style.Bold = resolveRunBold(runs[0], para, sc)
		}

		if pPr := para.X().PPr; pPr != nil && pPr.Spacing != nil {
			if pPr.Spacing.BeforeAttr != nil && pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber != nil {
				style.SpacingBefore = float64(*pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber) / 20.0
			}
			if pPr.Spacing.AfterAttr != nil && pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber != nil {
				style.SpacingAfter = float64(*pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber) / 20.0
			}
		}

		lineVal, lineMode := extractLineSpacing(para)
		if lineVal > 0 {
			if lineMode == "fixed" {
				style.LineSpacing = lineVal
			} else {
				style.LineSpacing = lineVal * 12.0
			}
		}

		standard.HeadingStyles = append(standard.HeadingStyles, style)
		foundLevels[level] = true
	}
}

// classifyHeadingLevel returns 1/2/3 for heading paragraphs, 0 otherwise.
func classifyHeadingLevel(styleName, text string) int {
	sn := strings.ToLower(strings.TrimSpace(styleName))
	for level := 1; level <= 5; level++ {
		if sn == fmt.Sprintf("heading%d", level) || sn == fmt.Sprintf("heading %d", level) ||
			sn == fmt.Sprintf("\u6807\u9898 %d", level) || sn == strconv.Itoa(level) {
			return level
		}
	}
	trimmed := strings.TrimSpace(text)
	if matched, _ := regexp.MatchString("^\u7b2c[\u4e00\u4e8c\u4e09\u56db\u4e94\u516d\u4e03\u516b\u4e5d\u5341\u767e\u96f6\u3007\\d]+\u7ae0(?:\\s|$)", trimmed); matched {
		return 1
	}
	for level := 5; level >= 2; level-- {
		pattern := fmt.Sprintf(`^\d+(?:[.\uff0e]\d+){%d}\s+`, level-1)
		if matched, _ := regexp.MatchString(pattern, trimmed); matched {
			return level
		}
	}

	if sn == "heading1" || sn == "heading 1" || sn == "标题 1" || sn == "1" {
		return 1
	}
	if sn == "heading2" || sn == "heading 2" || sn == "标题 2" || sn == "2" {
		return 2
	}
	if sn == "heading3" || sn == "heading 3" || sn == "标题 3" || sn == "3" {
		return 3
	}

	if matched, _ := regexp.MatchString(`^(第[一二三四五六七八九十0-9]+章|1\s+绪论)`, text); matched {
		return 1
	}
	if matched, _ := regexp.MatchString(`^\d+\.\d+\.\d+\s+`, text); matched {
		return 3
	}
	if matched, _ := regexp.MatchString(`^\d+\.\d+\s+`, text); matched {
		return 2
	}
	return 0
}

// ── 改进的正文样式提取 ──────────────────────────────────────────────────

func (p *TemplateParser) parseParagraphStylesImproved(doc *document.Document, standard *FormatStandard, sc *docxStyleCache) {
	for _, para := range doc.Paragraphs() {
		styleName := para.Style()
		text := ""
		for _, run := range para.Runs() {
			text += run.Text()
		}

		isBody := false
		if styleName == "Normal" || styleName == "正文" {
			isBody = true
		} else if len(text) > 50 && !isHeading(text) {
			isBody = true
		}

		if !isBody {
			continue
		}

		style := ParagraphStyle{
			Name: "正文",
		}

		runs := para.Runs()
		if len(runs) > 0 {
			style.FontName = resolveRunFontForTemplate(runs[0], para, sc)
			style.FontSize = resolveRunSizeForTemplate(runs[0], para, sc)
		}

		pPr := para.Properties()
		if pPr.X().Ind != nil {
			if pPr.X().Ind.FirstLineCharsAttr != nil {
				style.FirstLineIndent = float64(*pPr.X().Ind.FirstLineCharsAttr) / 100.0
			} else if pPr.X().Ind.FirstLineAttr != nil && pPr.X().Ind.FirstLineAttr.ST_UnsignedDecimalNumber != nil {
				style.FirstLineIndent = float64(*pPr.X().Ind.FirstLineAttr.ST_UnsignedDecimalNumber) / 240.0
			}
		}

		lineVal, lineMode := extractLineSpacing(para)
		if lineVal > 0 {
			if lineMode == "fixed" {
				style.LineSpacing = lineVal
			} else {
				style.LineSpacing = lineVal * 12.0
			}
		}

		standard.ParagraphStyles = append(standard.ParagraphStyles, style)
		return
	}
}

func isHeading(text string) bool {
	matched, _ := regexp.MatchString(`^(第.+章|1\s+绪论|\d+\.\d+)`, text)
	return matched
}

// ── 参考文献样式 ──────────────────────────────────────────────────────────

func (p *TemplateParser) parseReferenceStyles(doc *document.Document, standard *FormatStandard) {
	for _, para := range doc.Paragraphs() {
		text := ""
		for _, run := range para.Runs() {
			text += run.Text()
		}
		text = strings.TrimSpace(text)

		if strings.HasPrefix(text, "[1]") || strings.HasPrefix(text, "[M]") {
			style := ReferenceStyle{
				Style: "GB/T 7714",
			}
			runs := para.Runs()
			if len(runs) > 0 {
				rPr := runs[0].Properties()
				if rPr.X().RFonts != nil {
					if rPr.X().RFonts.EastAsiaAttr != nil {
						style.FontName = *rPr.X().RFonts.EastAsiaAttr
					}
				}
				if rPr.X().Sz != nil && rPr.X().Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
					style.FontSize = float64(*rPr.X().Sz.ValAttr.ST_UnsignedDecimalNumber) / 2.0
				}
			}
			standard.ReferenceStyle = style
			return
		}
	}
}

// ── 页眉提取 ─────────────────────────────────────────────────────────────

func (p *TemplateParser) parseHeaderContent(doc *document.Document, standard *FormatStandard) {
	for _, header := range doc.Headers() {
		for _, hp := range header.Paragraphs() {
			text := ""
			for _, run := range hp.Runs() {
				text += run.Text()
			}
			text = strings.TrimSpace(text)
			if strings.Contains(text, "大学") || strings.Contains(text, "学院") {
				standard.Name = text
				return
			}
		}
	}
}

// ══════════════════════════════════════════════════════════════════════════
// ParseTemplateToFormatRules — returns map[string]interface{} compatible
// with the JSON structure used by the AI/regex parser and stored in DB.
// ══════════════════════════════════════════════════════════════════════════

func (p *TemplateParser) ParseTemplateToFormatRules(templatePath string) (map[string]interface{}, error) {
	doc, err := document.Open(templatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open template: %w", err)
	}
	defer doc.Close()

	sc := loadDocxStyleCache(templatePath)

	rules := make(map[string]interface{})

	rules["page_setup"] = p.extractPageSetupRules(doc)

	allParas := doc.Paragraphs()
	classified := p.classifyParagraphs(allParas, sc)

	// 标题
	if info := classified["title"]; len(info) > 0 {
		titleRules := p.paraInfoToRuleMap(info[0])
		if sub := classified["subtitle"]; len(sub) > 0 {
			titleRules["subtitle"] = p.paraInfoToRuleMap(sub[0])
		}
		rules["title"] = titleRules
	}

	// 各级标题
	headingsMap := make(map[string]interface{})
	for level := 1; level <= 5; level++ {
		key := fmt.Sprintf("heading%d", level)
		if h := classified[key]; len(h) > 0 {
			headingsMap[fmt.Sprintf("level%d", level)] = p.paraInfoToRuleMap(h[0])
		}
	}
	if len(headingsMap) > 0 {
		rules["headings"] = headingsMap
	}

	// 摘要（标签+内容 run 级别提取）
	if info := classified["abstract_title"]; len(info) > 0 {
		abstractRules := map[string]interface{}{
			"label": p.paraInfoToRuleMap(info[0]),
		}
		if content := classified["abstract"]; len(content) > 0 {
			abstractRules["content"] = p.paraInfoToRuleMap(content[0])
		}
		rules["abstract"] = abstractRules
	} else if info := classified["abstract"]; len(info) > 0 {
		rules["abstract"] = map[string]interface{}{
			"content": p.paraInfoToRuleMap(info[0]),
		}
	}

	// 英文摘要
	if info := classified["en_abstract_title"]; len(info) > 0 {
		eaRules := map[string]interface{}{
			"label": p.paraInfoToRuleMap(info[0]),
		}
		if content := classified["en_abstract"]; len(content) > 0 {
			eaRules["content"] = p.paraInfoToRuleMap(content[0])
		}
		rules["english_abstract"] = eaRules
	}

	// 关键词
	if info := classified["keywords"]; len(info) > 0 {
		keywordRules := map[string]interface{}{"label": p.paraInfoToRuleMap(info[0])}
		if content := classified["keywords_content"]; len(content) > 0 {
			keywordRules["content"] = p.paraInfoToRuleMap(content[0])
		}
		rules["keywords"] = keywordRules
	}
	if info := classified["en_keywords"]; len(info) > 0 {
		keywordRules := map[string]interface{}{"label": p.paraInfoToRuleMap(info[0])}
		if content := classified["en_keywords_content"]; len(content) > 0 {
			keywordRules["content"] = p.paraInfoToRuleMap(content[0])
		}
		rules["english_keywords"] = keywordRules
	}

	// 正文
	if info := classified["body"]; len(info) > 0 {
		rules["body"] = p.paraInfoToRuleMap(info[0])
	}

	// 参考文献
	refRules := map[string]interface{}{}
	if info := classified["reference_title"]; len(info) > 0 {
		refRules["title"] = p.paraInfoToRuleMap(info[0])
	}
	if info := classified["references"]; len(info) > 0 {
		refRules["content"] = p.paraInfoToRuleMap(info[0])
	}
	if len(refRules) > 0 {
		rules["references"] = refRules
	}

	// 致谢
	if info := classified["acknowledgements_title"]; len(info) > 0 {
		ackRules := map[string]interface{}{"title": p.paraInfoToRuleMap(info[0])}
		if content := classified["acknowledgements"]; len(content) > 0 {
			ackRules["content"] = p.paraInfoToRuleMap(content[0])
		}
		rules["acknowledgements"] = ackRules
	}

	// 附录
	if info := classified["appendix_title"]; len(info) > 0 {
		appendixRules := map[string]interface{}{"title": p.paraInfoToRuleMap(info[0])}
		if content := classified["appendix"]; len(content) > 0 {
			appendixRules["content"] = p.paraInfoToRuleMap(content[0])
		}
		rules["appendix"] = appendixRules
	}

	// 注释
	if info := classified["notes_title"]; len(info) > 0 {
		rules["notes"] = map[string]interface{}{
			"label": p.paraInfoToRuleMap(info[0]),
		}
	}

	// 目录
	tocRules := map[string]interface{}{}
	if info := classified["toc_title"]; len(info) > 0 {
		tocRules["title"] = p.paraInfoToRuleMap(info[0])
	}
	for level := 1; level <= 4; level++ {
		if info := classified[fmt.Sprintf("toc%d", level)]; len(info) > 0 {
			tocRules[fmt.Sprintf("level%d", level)] = p.paraInfoToRuleMap(info[0])
		}
	}
	if info := classified["table_of_contents"]; len(info) > 0 {
		tocRules["content"] = p.paraInfoToRuleMap(info[0])
	} else if level1, ok := tocRules["level1"]; ok {
		tocRules["content"] = level1
	}
	if len(tocRules) > 0 {
		rules["table_of_contents"] = tocRules
	}

	// 图表标题
	if info := classified["figure_caption"]; len(info) > 0 {
		figureRules := map[string]interface{}{
			"caption": p.paraInfoToRuleMap(info[0]),
		}
		rules["figure"] = figureRules
		rules["figures"] = figureRules
	}
	if info := classified["table_caption"]; len(info) > 0 {
		tableRules := map[string]interface{}{
			"caption": p.paraInfoToRuleMap(info[0]),
		}
		if note := classified["table_note"]; len(note) > 0 {
			tableRules["note"] = p.paraInfoToRuleMap(note[0])
		}
		rules["table"] = tableRules
		rules["tables"] = tableRules
	}
	if note := classified["table_note"]; len(note) > 0 {
		tableRules, _ := rules["table"].(map[string]interface{})
		if tableRules == nil {
			tableRules = map[string]interface{}{}
		}
		tableRules["note"] = p.paraInfoToRuleMap(note[0])
		rules["table"] = tableRules
		rules["tables"] = tableRules
	}

	// 页眉页脚内容提取
	p.extractHeaderFooterRules(doc, sc, rules)
	if profile, profileErr := templateprofile.Extract(templatePath); profileErr == nil {
		if profile.Header.Exists && profile.Header.Text != "" {
			headerRules, _ := rules["header"].(map[string]interface{})
			if headerRules == nil {
				headerRules = map[string]interface{}{}
			}
			headerRules["content"] = profile.Header.Text
			rules["header"] = headerRules
		}
		if profile.Footer.Exists {
			pageRules, _ := rules["page_number"].(map[string]interface{})
			if pageRules == nil {
				pageRules = map[string]interface{}{}
			}
			pageRules["content"] = profile.Footer.Text
			pageRules["has_page_field"] = profile.Footer.HasPageField
			pageRules["has_total_pages"] = profile.Footer.HasNumPages
			if profile.Footer.HasPageField && profile.Footer.HasNumPages && hasTemplateTotalPageText(profile.Footer.Text) {
				pageRules["content"] = "第×页 共×页"
				pageRules["format"] = "第×页 共×页"
			}
			rules["page_number"] = pageRules
		}
	}

	p.extractPackageStructuralRules(templatePath, sc, rules)

	uniName := p.extractUniversityNameFromDoc(doc)
	if uniName != "" {
		rules["_university_name"] = uniName
	}

	return rules, nil
}

// paraInfo holds extracted formatting for a classified paragraph.
type paraInfo struct {
	FontName      string
	LatinFont     string
	FontSize      float64
	Bold          bool
	HasTextStyle  bool
	Alignment     string
	LineSpace     string
	LineRule      string
	Indent        float64
	HangingIndent string
	SpaceBeforePt float64
	SpaceAfterPt  float64
	Color         string
}

func (p *TemplateParser) paraInfoToRuleMap(info paraInfo) map[string]interface{} {
	m := make(map[string]interface{})
	if info.FontName != "" {
		m["font_name"] = info.FontName
	}
	if info.LatinFont != "" {
		m["font_name_latin"] = info.LatinFont
	}
	if info.FontSize > 0 {
		m["font_size"] = fontPointsToChineseName(info.FontSize)
		m["font_size_pt"] = info.FontSize
	}
	if info.HasTextStyle {
		m["bold"] = info.Bold
	}
	if info.Alignment != "" {
		m["alignment"] = info.Alignment
	}
	if info.LineSpace != "" {
		m["line_space"] = info.LineSpace
	}
	if info.LineRule != "" {
		m["line_rule"] = info.LineRule
	}
	if info.Indent > 0 {
		m["first_line_indent"] = fmt.Sprintf("%.0f字符", info.Indent)
	}
	if info.HangingIndent != "" {
		m["hanging_indent"] = info.HangingIndent
	}
	if info.SpaceBeforePt > 0 || info.SpaceAfterPt > 0 {
		m["paragraph_space"] = map[string]interface{}{
			"before": fmt.Sprintf("%.1fpt", info.SpaceBeforePt),
			"after":  fmt.Sprintf("%.1fpt", info.SpaceAfterPt),
		}
	}
	if info.Color != "" {
		m["color"] = "#" + strings.TrimPrefix(info.Color, "#")
	}
	return m
}

func (p *TemplateParser) extractParaInfo(para document.Paragraph, sc *docxStyleCache) paraInfo {
	info := paraInfo{
		Alignment: extractAlignment(para),
	}

	runs := para.Runs()
	if len(runs) > 0 {
		info.FontName, info.LatinFont = resolveRunFontsForTemplate(runs[0], para, sc)
		info.FontSize = resolveRunSizeForTemplate(runs[0], para, sc)
		info.Bold = resolveRunBold(runs[0], para, sc)
		info.HasTextStyle = true
		if rPr := runs[0].X().RPr; rPr != nil && rPr.Color != nil && rPr.Color.ValAttr.ST_HexColorRGB != nil {
			info.Color = *rPr.Color.ValAttr.ST_HexColorRGB
		}
	}

	info.LineSpace = extractLineSpacingDisplay(para)
	info.LineRule = extractLineRule(para)

	pPr := para.X().PPr
	if pPr != nil && pPr.Ind != nil {
		if pPr.Ind.FirstLineCharsAttr != nil {
			info.Indent = float64(*pPr.Ind.FirstLineCharsAttr) / 100.0
		} else if pPr.Ind.FirstLineAttr != nil && pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber != nil {
			info.Indent = float64(*pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber) / 240.0
		}
		if pPr.Ind.HangingCharsAttr != nil {
			info.HangingIndent = fmt.Sprintf("%.0f\u5b57\u7b26", float64(*pPr.Ind.HangingCharsAttr)/100.0)
		} else if pPr.Ind.HangingAttr != nil && pPr.Ind.HangingAttr.ST_UnsignedDecimalNumber != nil {
			info.HangingIndent = fmt.Sprintf("%.1fpt", float64(*pPr.Ind.HangingAttr.ST_UnsignedDecimalNumber)/20.0)
		}
	}
	if pPr != nil && pPr.Spacing != nil {
		if pPr.Spacing.BeforeAttr != nil && pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber != nil {
			info.SpaceBeforePt = float64(*pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber) / 20.0
		}
		if pPr.Spacing.AfterAttr != nil && pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber != nil {
			info.SpaceAfterPt = float64(*pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber) / 20.0
		}
	}

	return info
}

func (p *TemplateParser) extractParaInfoFromRun(para document.Paragraph, run document.Run, sc *docxStyleCache) paraInfo {
	info := p.extractParaInfo(para, sc)
	info.FontName, info.LatinFont = resolveRunFontsForTemplate(run, para, sc)
	info.FontSize = resolveRunSizeForTemplate(run, para, sc)
	info.Bold = resolveRunBold(run, para, sc)
	info.HasTextStyle = true
	info.Color = ""
	if rPr := run.X().RPr; rPr != nil && rPr.Color != nil && rPr.Color.ValAttr.ST_HexColorRGB != nil {
		info.Color = *rPr.Color.ValAttr.ST_HexColorRGB
	}
	return info
}

// classifyParagraphs scans all paragraphs and classifies them by category.
func (p *TemplateParser) classifyParagraphs(paras []document.Paragraph, sc *docxStyleCache) map[string][]paraInfo {
	result := make(map[string][]paraInfo)
	foundCategories := make(map[string]bool)

	repeatableCategories := map[string]bool{
		"body": true, "references": true, "table_of_contents": true,
		"en_abstract": true, "abstract": true, "acknowledgements": true,
		"appendix": true, "toc1": true, "toc2": true, "toc3": true,
		"toc4": true, "table_note": true,
	}
	activeSection := ""

	for i, para := range paras {
		styleName := strings.TrimSpace(para.Style())
		text := ""
		for _, run := range para.Runs() {
			text += run.Text()
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		category := p.classifyParagraphCategory(styleName, text, i, len(paras))
		switch category {
		case "abstract_title":
			activeSection = "abstract"
		case "en_abstract_title":
			activeSection = "en_abstract"
		case "reference_title":
			activeSection = "references"
		case "acknowledgements_title":
			activeSection = "acknowledgements"
		case "appendix_title":
			activeSection = "appendix"
		case "toc_title":
			activeSection = "table_of_contents"
		case "keywords", "en_keywords":
			activeSection = ""
		case "heading1", "heading2", "heading3", "heading4", "heading5":
			activeSection = ""
		case "body", "unknown", "":
			if activeSection != "" {
				category = activeSection
			}
		}
		if category == "" || category == "unknown" {
			continue
		}

		if foundCategories[category] && !repeatableCategories[category] {
			continue
		}
		foundCategories[category] = true

		info := p.extractParaInfo(para, sc)
		result[category] = append(result[category], info)
		if category == "keywords" || category == "en_keywords" {
			contentCategory := category + "_content"
			runs := para.Runs()
			if len(runs) > 1 {
				result[contentCategory] = append(result[contentCategory], p.extractParaInfoFromRun(para, runs[len(runs)-1], sc))
			} else {
				result[contentCategory] = append(result[contentCategory], info)
			}
		}
	}

	return result
}

func (p *TemplateParser) classifyParagraphCategory(styleName, text string, index, total int) string {
	sn := strings.ToLower(styleName)
	textLower := strings.ToLower(text)
	normalized := normalizeChineseTextForParser(text)
	for level := 1; level <= 5; level++ {
		if sn == fmt.Sprintf("heading %d", level) || sn == fmt.Sprintf("heading%d", level) ||
			sn == fmt.Sprintf("\u6807\u9898 %d", level) {
			return fmt.Sprintf("heading%d", level)
		}
	}
	for level := 1; level <= 4; level++ {
		if sn == fmt.Sprintf("toc %d", level) || sn == fmt.Sprintf("toc%d", level) ||
			sn == fmt.Sprintf("\u76ee\u5f55 %d", level) {
			return fmt.Sprintf("toc%d", level)
		}
	}
	compact := strings.NewReplacer(" ", "", "\t", "", "\u3000", "").Replace(strings.TrimSpace(text))
	if compact == "\u6458\u8981" || compact == "\u6458\u8981\uff1a" || compact == "\u6458\u8981:" {
		return "abstract_title"
	}
	if strings.HasPrefix(compact, "\u5173\u952e\u8bcd") || strings.HasPrefix(compact, "\u5173\u952e\u5b57") {
		return "keywords"
	}
	if compact == "\u76ee\u5f55" {
		return "toc_title"
	}
	if compact == "\u53c2\u8003\u6587\u732e" {
		return "reference_title"
	}
	if compact == "\u81f4\u8c22" {
		return "acknowledgements_title"
	}
	if strings.HasPrefix(compact, "\u9644\u5f55") && len([]rune(compact)) < 20 {
		return "appendix_title"
	}
	if matched, _ := regexp.MatchString("^(?:\u56fe|Figure)\\s*\\d+(?:[.\\-\uff0d]\\d+)+", strings.TrimSpace(text)); matched {
		return "figure_caption"
	}
	if matched, _ := regexp.MatchString("^(?:\u8868|Table)\\s*\\d+(?:[.\\-\uff0d]\\d+)+", strings.TrimSpace(text)); matched {
		return "table_caption"
	}
	if matched, _ := regexp.MatchString("^(?:\u6ce8|Note)\\s*[:\uff1a]", strings.TrimSpace(text)); matched {
		return "table_note"
	}

	// Word 样式名称优先（100% 可靠信号）
	if sn == "title" || sn == "论文标题" {
		return "title"
	}
	if sn == "heading 1" || sn == "标题 1" || sn == "heading1" {
		return "heading1"
	}
	if sn == "heading 2" || sn == "标题 2" || sn == "heading2" {
		return "heading2"
	}
	if sn == "heading 3" || sn == "标题 3" || sn == "heading3" {
		return "heading3"
	}
	if sn == "heading 4" || sn == "标题 4" || sn == "heading4" {
		return "heading4"
	}
	if sn == "toc 1" || sn == "toc 2" || sn == "toc 3" || strings.HasPrefix(sn, "目录") {
		return "table_of_contents"
	}

	// 首段通常是标题
	if index == 0 && len(text) > 2 && len(text) < 80 {
		return "title"
	}

	// 副标题（以——开头）
	trimmed := strings.TrimSpace(text)
	if (strings.HasPrefix(trimmed, "——") || strings.HasPrefix(trimmed, "—")) && len([]rune(trimmed)) < 50 {
		return "subtitle"
	}

	// 目录标题
	if (normalized == "目录" || normalized == "目  录") && len([]rune(text)) < 10 {
		return "toc_title"
	}

	// 摘要
	if strings.Contains(normalized, "摘要") {
		if len([]rune(normalized)) < 20 {
			return "abstract_title"
		}
		return "abstract"
	}

	// 英文摘要
	hasChinese := false
	for _, r := range text {
		if r >= 0x4e00 && r <= 0x9fff {
			hasChinese = true
			break
		}
	}
	if strings.Contains(textLower, "abstract") && !hasChinese {
		if len(text) < 30 {
			return "en_abstract_title"
		}
		return "en_abstract"
	}

	// 关键词
	if strings.Contains(normalized, "关键词") || strings.Contains(normalized, "关键字") {
		return "keywords"
	}
	if (strings.Contains(textLower, "keywords") || strings.Contains(textLower, "key words")) && !hasChinese {
		return "en_keywords"
	}

	// 致谢
	noSpaceNorm := strings.ReplaceAll(normalized, " ", "")
	noSpaceNorm = strings.ReplaceAll(noSpaceNorm, "\u3000", "")
	if (noSpaceNorm == "致谢" || strings.Contains(normalized, "致谢")) && len([]rune(normalized)) < 10 {
		return "acknowledgements_title"
	}

	// 参考文献
	if strings.Contains(textLower, "参考文献") || strings.Contains(textLower, "references") {
		if len(text) < 20 {
			return "reference_title"
		}
	}
	if matched, _ := regexp.MatchString(`^\[\d+\]`, text); matched {
		return "references"
	}

	// 注释
	if (noSpaceNorm == "注释" || noSpaceNorm == "注释：" || noSpaceNorm == "注释:") && len([]rune(normalized)) < 15 {
		return "notes_title"
	}

	// 附录
	if strings.Contains(normalized, "附录") && len([]rune(normalized)) < 20 {
		return "appendix_title"
	}

	// 图标题
	if matched, _ := regexp.MatchString(`^图\s*[\d]+[.\-][\d]+`, trimmed); matched {
		return "figure_caption"
	}
	// 表标题
	if matched, _ := regexp.MatchString(`^表\s*[\d]+[.\-][\d]+`, trimmed); matched {
		return "table_caption"
	}

	// 标题级别
	if matched, _ := regexp.MatchString("^(?:\u56fe|Figure)\\s*\\d+(?:[.\\-\uff0d]\\d+)+", strings.TrimSpace(text)); matched {
		return "figure_caption"
	}
	if matched, _ := regexp.MatchString("^(?:\u8868|Table)\\s*\\d+(?:[.\\-\uff0d]\\d+)+", strings.TrimSpace(text)); matched {
		return "table_caption"
	}
	if matched, _ := regexp.MatchString("^(?:\u6ce8|Note)\\s*[:\uff1a]", strings.TrimSpace(text)); matched {
		return "table_note"
	}

	level := classifyHeadingLevel(styleName, text)
	if level >= 1 && level <= 5 {
		return fmt.Sprintf("heading%d", level)
	}

	// 正文
	if sn == "normal" || sn == "正文" || sn == "" {
		if len(text) > 30 {
			return "body"
		}
	}
	if len(text) > 50 && !isHeading(text) {
		return "body"
	}

	return "unknown"
}

func normalizeChineseTextForParser(text string) string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) < 30 {
		text = strings.ReplaceAll(text, " ", "")
		text = strings.ReplaceAll(text, "\u3000", "")
	}
	return text
}

// extractPageSetupRules builds the page_setup section of the rules map.
func (p *TemplateParser) extractPageSetupRules(doc *document.Document) map[string]interface{} {
	setup := make(map[string]interface{})
	section := doc.BodySection()

	pgSz := section.X().PgSz
	if pgSz != nil {
		w := uint64(0)
		if pgSz.WAttr != nil && pgSz.WAttr.ST_UnsignedDecimalNumber != nil {
			w = *pgSz.WAttr.ST_UnsignedDecimalNumber
		}
		if w > 11000 && w < 13000 {
			setup["paper_size"] = "A4"
		}
	}

	pgMar := section.X().PgMar
	if pgMar != nil {
		margins := make(map[string]interface{})
		if pgMar.TopAttr.Int64 != nil {
			margins["top"] = fmt.Sprintf("%.2fcm", float64(*pgMar.TopAttr.Int64)/567.0)
		}
		if pgMar.BottomAttr.Int64 != nil {
			margins["bottom"] = fmt.Sprintf("%.2fcm", float64(*pgMar.BottomAttr.Int64)/567.0)
		}
		if pgMar.LeftAttr.ST_UnsignedDecimalNumber != nil {
			margins["left"] = fmt.Sprintf("%.2fcm", float64(*pgMar.LeftAttr.ST_UnsignedDecimalNumber)/567.0)
		}
		if pgMar.RightAttr.ST_UnsignedDecimalNumber != nil {
			margins["right"] = fmt.Sprintf("%.2fcm", float64(*pgMar.RightAttr.ST_UnsignedDecimalNumber)/567.0)
		}
		if len(margins) > 0 {
			setup["margins"] = margins
		}

		header := make(map[string]interface{})
		if pgMar.HeaderAttr.ST_UnsignedDecimalNumber != nil {
			header["distance"] = fmt.Sprintf("%.2fcm", float64(*pgMar.HeaderAttr.ST_UnsignedDecimalNumber)/567.0)
		}
		if len(header) > 0 {
			setup["header"] = header
		}

		footer := make(map[string]interface{})
		if pgMar.FooterAttr.ST_UnsignedDecimalNumber != nil {
			footer["distance"] = fmt.Sprintf("%.2fcm", float64(*pgMar.FooterAttr.ST_UnsignedDecimalNumber)/567.0)
		}
		if len(footer) > 0 {
			setup["footer"] = footer
		}
	}

	return setup
}

func (p *TemplateParser) extractPackageStructuralRules(templatePath string, sc *docxStyleCache, rules map[string]interface{}) {
	zr, err := zip.OpenReader(templatePath)
	if err != nil {
		return
	}
	defer zr.Close()

	documentXML := ""
	for _, file := range zr.File {
		if strings.EqualFold(file.Name, "word/document.xml") {
			documentXML, _ = readZipFileAsString(file)
			break
		}
	}
	if documentXML == "" {
		return
	}

	pageRules, _ := rules["page_number"].(map[string]interface{})
	if pageRules == nil {
		pageRules = map[string]interface{}{}
	}
	for _, format := range parsePageNumFormats(documentXML) {
		switch strings.ToLower(format) {
		case "lowerroman", "upperroman":
			pageRules["front_format"] = format
		case "decimal":
			pageRules["body_format"] = format
		}
	}
	if len(pageRules) > 0 {
		rules["page_number"] = pageRules
	}

	paragraphPattern := regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`)
	for _, paragraphXML := range paragraphPattern.FindAllString(documentXML, -1) {
		if _, exists := rules["formula"]; !exists && (strings.Contains(paragraphXML, "<m:oMath") || strings.Contains(paragraphXML, "<m:oMathPara")) {
			rules["formula"] = paragraphXMLToRuleMap(paragraphXML, sc)
		}
		if strings.Contains(paragraphXML, "<w:drawing") {
			figureRules, _ := rules["figure"].(map[string]interface{})
			if figureRules == nil {
				figureRules = map[string]interface{}{}
			}
			if _, exists := figureRules["image"]; !exists {
				imageRule := map[string]interface{}{}
				if alignment := xmlAttributeValue(paragraphXML, "w:jc", "w:val"); alignment != "" {
					imageRule["alignment"] = alignment
				}
				figureRules["image"] = imageRule
				rules["figure"] = figureRules
				rules["figures"] = figureRules
			}
		}
	}
}

func paragraphXMLToRuleMap(paragraphXML string, sc *docxStyleCache) map[string]interface{} {
	rule := map[string]interface{}{}
	if font := xmlAttributeValue(paragraphXML, "w:rFonts", "w:eastAsia"); font != "" {
		rule["font_name"] = font
	}
	latin := xmlAttributeValue(paragraphXML, "w:rFonts", "w:ascii")
	if latin == "" {
		latin = xmlAttributeValue(paragraphXML, "w:rFonts", "w:hAnsi")
	}
	if latin != "" {
		rule["font_name_latin"] = latin
	}
	if sc != nil {
		if styleID := xmlAttributeValue(paragraphXML, "w:pStyle", "w:val"); styleID != "" {
			style := sc.resolve(styleID)
			if _, ok := rule["font_name"]; !ok && style.EastAsiaFont != "" {
				rule["font_name"] = style.EastAsiaFont
			}
			if _, ok := rule["font_name_latin"]; !ok && style.AsciiFont != "" {
				rule["font_name_latin"] = style.AsciiFont
			}
			if _, ok := rule["font_size_pt"]; !ok && style.FontSizePt > 0 {
				rule["font_size"] = fontPointsToChineseName(style.FontSizePt)
				rule["font_size_pt"] = style.FontSizePt
			}
		}
	}
	if sizeText := xmlAttributeValue(paragraphXML, "w:sz", "w:val"); sizeText != "" {
		if halfPoints, err := strconv.ParseFloat(sizeText, 64); err == nil {
			points := halfPoints / 2
			rule["font_size"] = fontPointsToChineseName(points)
			rule["font_size_pt"] = points
		}
	}
	if alignment := xmlAttributeValue(paragraphXML, "w:jc", "w:val"); alignment != "" {
		rule["alignment"] = alignment
	}
	if color := xmlAttributeValue(paragraphXML, "w:color", "w:val"); color != "" && !strings.EqualFold(color, "auto") {
		rule["color"] = "#" + strings.TrimPrefix(color, "#")
	}
	if regexp.MustCompile(`<w:b(?:\s|/|>)`).MatchString(paragraphXML) {
		rule["bold"] = true
	}
	spacing := map[string]interface{}{}
	if before := xmlAttributeValue(paragraphXML, "w:spacing", "w:before"); before != "" {
		if twips, err := strconv.ParseFloat(before, 64); err == nil {
			spacing["before"] = fmt.Sprintf("%.1fpt", twips/20)
		}
	}
	if after := xmlAttributeValue(paragraphXML, "w:spacing", "w:after"); after != "" {
		if twips, err := strconv.ParseFloat(after, 64); err == nil {
			spacing["after"] = fmt.Sprintf("%.1fpt", twips/20)
		}
	}
	if len(spacing) > 0 {
		rule["paragraph_space"] = spacing
	}
	if line := xmlAttributeValue(paragraphXML, "w:spacing", "w:line"); line != "" {
		if value, err := strconv.ParseFloat(line, 64); err == nil {
			lineRule := strings.ToLower(xmlAttributeValue(paragraphXML, "w:spacing", "w:lineRule"))
			if lineRule == "exact" || lineRule == "atleast" {
				rule["line_space"] = fmt.Sprintf("fixed_%.1f_pt", value/20)
				rule["line_rule"] = lineRule
			} else {
				rule["line_space"] = fmt.Sprintf("%.1f", value/240)
				rule["line_rule"] = "auto"
			}
		}
	}
	return rule
}

func xmlAttributeValue(xmlText, element, attribute string) string {
	elementPattern := regexp.QuoteMeta(element)
	attributePattern := regexp.QuoteMeta(attribute)
	match := regexp.MustCompile(`<` + elementPattern + `\b[^>]*\b` + attributePattern + `="([^"]+)"`).FindStringSubmatch(xmlText)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

// extractHeaderFooterRules extracts header/footer text and formatting from the document.
func (p *TemplateParser) extractHeaderFooterRules(doc *document.Document, sc *docxStyleCache, rules map[string]interface{}) {
	bestHeaderScore := -1
	for _, header := range doc.Headers() {
		for _, hp := range header.Paragraphs() {
			text := ""
			for _, run := range hp.Runs() {
				text += run.Text()
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			headerRules := map[string]interface{}{
				"content": text,
			}
			info := p.extractParaInfo(hp, sc)
			if info.FontName != "" {
				headerRules["font_name"] = info.FontName
			}
			if info.FontSize > 0 {
				headerRules["font_size"] = fontPointsToChineseName(info.FontSize)
			}
			if info.Alignment != "" {
				headerRules["alignment"] = info.Alignment
			}
			if pPr := hp.X().PPr; pPr != nil && pPr.PBdr != nil && pPr.PBdr.Bottom != nil {
				border := pPr.PBdr.Bottom
				if border.SzAttr != nil {
					headerRules["border_bottom"] = fmt.Sprintf("%.2fpt", float64(*border.SzAttr)/8.0)
				} else {
					headerRules["border_bottom"] = true
				}
			}
			score := len([]rune(text))
			if strings.Contains(text, "大学") || strings.Contains(text, "学院") {
				score += 100
			}
			if score > bestHeaderScore {
				rules["header"] = headerRules
				bestHeaderScore = score
			}
		}
	}

	bestFooterScore := -1
	for _, footer := range doc.Footers() {
		for _, fp := range footer.Paragraphs() {
			text := ""
			for _, run := range fp.Runs() {
				text += run.Text()
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			footerRules := map[string]interface{}{
				"content": text,
			}
			info := p.extractParaInfo(fp, sc)
			if info.FontName != "" {
				footerRules["font_name"] = info.FontName
			}
			if info.FontSize > 0 {
				footerRules["font_size"] = fontPointsToChineseName(info.FontSize)
			}
			if info.Alignment != "" {
				footerRules["alignment"] = info.Alignment
				footerRules["position"] = "bottom_" + info.Alignment
			}
			score := len([]rune(text))
			if hasTemplateTotalPageText(text) {
				footerRules["content"] = "第×页 共×页"
				footerRules["format"] = "第×页 共×页"
				footerRules["has_page_field"] = true
				footerRules["has_total_pages"] = true
				score += 200
			}
			if score > bestFooterScore {
				rules["page_number"] = footerRules
				bestFooterScore = score
			}
		}
	}
}

func hasTemplateTotalPageText(text string) bool {
	compact := strings.NewReplacer(" ", "", "\t", "", "\u00a0", "").Replace(text)
	return strings.Contains(compact, "第") && strings.Contains(compact, "共") && strings.Count(compact, "页") >= 2
}

// extractUniversityNameFromDoc tries to find a university name from headers and first-page text.
func (p *TemplateParser) extractUniversityNameFromDoc(doc *document.Document) string {
	for _, header := range doc.Headers() {
		for _, hp := range header.Paragraphs() {
			text := ""
			for _, run := range hp.Runs() {
				text += run.Text()
			}
			text = strings.TrimSpace(text)
			if strings.Contains(text, "大学") || strings.Contains(text, "学院") {
				return extractUniversityFromText(text)
			}
		}
	}

	for i, para := range doc.Paragraphs() {
		if i > 10 {
			break
		}
		text := ""
		for _, run := range para.Runs() {
			text += run.Text()
		}
		text = strings.TrimSpace(text)
		if strings.Contains(text, "大学") || strings.Contains(text, "学院") {
			return extractUniversityFromText(text)
		}
	}

	return ""
}

var reUniversityName = regexp.MustCompile(`([\p{Han}]{2,10}(?:大学|学院))`)

func extractUniversityFromText(text string) string {
	if m := reUniversityName.FindString(text); m != "" {
		return m
	}
	return text
}

// ── IsSampleDocument 检测是否为格式范例文档 ─────────────────────────────

// IsSampleDocument checks whether a DOCX file is a "formatted sample"
// (actual paper with formatting) rather than a "format description"
// (text document describing formatting rules).
//
// Heuristic: if the extracted text does NOT contain many format-description
// keywords (字体, 字号, 行距, 居中, 对齐, 加粗 appearing at least 5 times total),
// it's likely a formatted sample.
func IsSampleDocument(text string) bool {
	keywords := []string{"字体", "字号", "行距", "居中", "对齐", "加粗", "磅", "页边距", "格式要求", "宋体", "黑体", "楷体"}
	totalHits := 0
	for _, kw := range keywords {
		totalHits += strings.Count(text, kw)
	}
	return totalHits < 5
}
