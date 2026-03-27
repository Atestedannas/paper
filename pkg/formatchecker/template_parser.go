package formatchecker

import (
	"fmt"
	"math"
	"regexp"
	"strings"

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
	rPr := run.Properties().X()
	if rPr != nil && rPr.RFonts != nil {
		if rPr.RFonts.EastAsiaAttr != nil && *rPr.RFonts.EastAsiaAttr != "" {
			return *rPr.RFonts.EastAsiaAttr
		}
		if rPr.RFonts.HAnsiAttr != nil && *rPr.RFonts.HAnsiAttr != "" {
			return *rPr.RFonts.HAnsiAttr
		}
		if rPr.RFonts.AsciiAttr != nil && *rPr.RFonts.AsciiAttr != "" {
			return *rPr.RFonts.AsciiAttr
		}
	}
	if sc != nil {
		styleID := getParagraphStyleID(para)
		if styleID != "" {
			props := sc.resolve(styleID)
			if props.EastAsiaFont != "" {
				return props.EastAsiaFont
			}
			if props.AsciiFont != "" {
				return props.AsciiFont
			}
		}
		if sc.defaults.EastAsiaFont != "" {
			return sc.defaults.EastAsiaFont
		}
	}
	return ""
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

	if info := classified["title"]; len(info) > 0 {
		rules["title"] = p.paraInfoToRuleMap(info[0])
	}
	if info := classified["abstract"]; len(info) > 0 {
		rules["abstract"] = p.paraInfoToRuleMap(info[0])
	}
	if info := classified["abstract_title"]; len(info) > 0 {
		rules["abstract_title"] = p.paraInfoToRuleMap(info[0])
	}
	if info := classified["body"]; len(info) > 0 {
		rules["body"] = p.paraInfoToRuleMap(info[0])
	}
	if info := classified["references"]; len(info) > 0 {
		rules["references"] = p.paraInfoToRuleMap(info[0])
	}
	if info := classified["reference_title"]; len(info) > 0 {
		rules["reference_title"] = p.paraInfoToRuleMap(info[0])
	}

	headingsMap := make(map[string]interface{})
	for _, h := range classified["heading1"] {
		headingsMap["level1"] = p.paraInfoToRuleMap(h)
		break
	}
	for _, h := range classified["heading2"] {
		headingsMap["level2"] = p.paraInfoToRuleMap(h)
		break
	}
	for _, h := range classified["heading3"] {
		headingsMap["level3"] = p.paraInfoToRuleMap(h)
		break
	}
	if len(headingsMap) > 0 {
		rules["headings"] = headingsMap
	}

	uniName := p.extractUniversityNameFromDoc(doc)
	if uniName != "" {
		rules["_university_name"] = uniName
	}

	return rules, nil
}

// paraInfo holds extracted formatting for a classified paragraph.
type paraInfo struct {
	FontName  string
	FontSize  float64
	Bold      bool
	Alignment string
	LineSpace string
	Indent    float64
}

func (p *TemplateParser) paraInfoToRuleMap(info paraInfo) map[string]interface{} {
	m := make(map[string]interface{})
	if info.FontName != "" {
		m["font_name"] = info.FontName
	}
	if info.FontSize > 0 {
		m["font_size"] = fontPointsToChineseName(info.FontSize)
	}
	if info.Bold {
		m["bold"] = true
	}
	if info.Alignment != "" {
		m["alignment"] = info.Alignment
	}
	if info.LineSpace != "" {
		m["line_space"] = info.LineSpace
	}
	if info.Indent > 0 {
		m["first_line_indent"] = fmt.Sprintf("%.0f字符", info.Indent)
	}
	return m
}

func (p *TemplateParser) extractParaInfo(para document.Paragraph, sc *docxStyleCache) paraInfo {
	info := paraInfo{
		Alignment: extractAlignment(para),
	}

	runs := para.Runs()
	if len(runs) > 0 {
		info.FontName = resolveRunFontForTemplate(runs[0], para, sc)
		info.FontSize = resolveRunSizeForTemplate(runs[0], para, sc)
		info.Bold = resolveRunBold(runs[0], para, sc)
	}

	info.LineSpace = extractLineSpacingDisplay(para)

	pPr := para.X().PPr
	if pPr != nil && pPr.Ind != nil {
		if pPr.Ind.FirstLineCharsAttr != nil {
			info.Indent = float64(*pPr.Ind.FirstLineCharsAttr) / 100.0
		} else if pPr.Ind.FirstLineAttr != nil && pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber != nil {
			info.Indent = float64(*pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber) / 240.0
		}
	}

	return info
}

// classifyParagraphs scans all paragraphs and classifies them by category.
func (p *TemplateParser) classifyParagraphs(paras []document.Paragraph, sc *docxStyleCache) map[string][]paraInfo {
	result := make(map[string][]paraInfo)
	foundCategories := make(map[string]bool)

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
		if category == "" || category == "unknown" {
			continue
		}

		if foundCategories[category] && category != "body" {
			continue
		}
		foundCategories[category] = true

		info := p.extractParaInfo(para, sc)
		result[category] = append(result[category], info)
	}

	return result
}

func (p *TemplateParser) classifyParagraphCategory(styleName, text string, index, total int) string {
	sn := strings.ToLower(styleName)
	textLower := strings.ToLower(text)

	if sn == "title" || sn == "论文标题" {
		return "title"
	}
	if index == 0 && len(text) > 2 && len(text) < 80 {
		return "title"
	}

	if strings.Contains(textLower, "摘要") || strings.Contains(textLower, "abstract") {
		if len(text) < 10 {
			return "abstract_title"
		}
		return "abstract"
	}

	if strings.Contains(textLower, "参考文献") || strings.Contains(textLower, "references") {
		if len(text) < 15 {
			return "reference_title"
		}
	}
	if matched, _ := regexp.MatchString(`^\[\d+\]`, text); matched {
		return "references"
	}

	level := classifyHeadingLevel(styleName, text)
	if level == 1 {
		return "heading1"
	}
	if level == 2 {
		return "heading2"
	}
	if level == 3 {
		return "heading3"
	}

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
