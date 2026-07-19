package templateprofile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

const Version = "template-profile-v1"

const templateProfileAIPromptTemplate = `你是“本科毕业论文 DOCX 模板格式规范解析专家”，任务是把 OOXML 本地解析结果转成可执行的论文格式画像 JSON。

你必须只输出严格 JSON，不要 markdown，不要解释。

## 目标
根据本地解析 JSON，提取并校准该模板的论文格式要求，用于后续 Go 程序自动修复学生论文格式。
你的输出必须尽量详细、稳定、可执行；不允许改写论文内容，不允许推断学生论文观点，只分析格式。

## 重点识别
1. 章节另起页：
   - body_start：正文起始，如“1 绪论/1 引言”是否另起页。
   - references_title：参考文献标题是否另起页。
   - acknowledgements_title：致谢标题是否另起页。
   - appendix_title：附录标题是否另起页（如果存在）。
2. 页眉页脚：
   - 页眉是否存在，文字内容、字体、字号、是否双线。
   - 页脚是否存在，是否包含 PAGE、NUMPAGES，页码格式是否“第×页 共×页”。
   - 如果本地解析没有起止页证据，不要编造。
3. 样式画像：
   - abstract_cn、keywords_cn、abstract_en、keywords_en。
   - heading_1、heading_2、heading_3、heading_4。
   - body_start/body、references_title/references、acknowledgements_title/acknowledgements。
   - 字体、字号、加粗、对齐、首行缩进、行距、段前段后。
4. 风险控制：
   - 本地解析中没有证据的字段省略。
   - 如果本地解析和常识冲突，以本地解析为准。
   - 如果只能部分确定，请保留已确定字段，并降低 confidence。

## 输出 JSON 结构
{
  "sections": {
    "body_start": {"page_break_before": true, "evidence": "current_paragraph/previous_paragraph/not_found"},
    "references_title": {"page_break_before": true, "evidence": "current_paragraph/previous_paragraph/not_found"},
    "acknowledgements_title": {"page_break_before": true, "evidence": "current_paragraph/previous_paragraph/not_found"}
  },
  "header": {
    "exists": true,
    "text": "页眉文本",
    "font_east_asia": "宋体",
    "font_size_half_pt": "18",
    "has_double_line": true
  },
  "footer": {
    "exists": true,
    "has_page_field": true,
    "has_num_pages": true,
    "text": "第页 共页"
  },
  "styles": {
    "heading_1": {"font_east_asia":"宋体","font_size_half_pt":"32","bold":true,"alignment":"left","line":"360","before_lines":"100","after_lines":"100"},
    "body": {"font_east_asia":"宋体","font_ascii":"Times New Roman","font_size_half_pt":"24","alignment":"both","first_line_chars":"200","line":"360"},
    "references": {"font_east_asia":"宋体","font_ascii":"Times New Roman","font_size_half_pt":"21","first_line_chars":"0","line":"360"}
  },
  "confidence": 0.88
}

## 本地解析 JSON
%s`

type ChatClient interface {
	ChatCompletion(prompt string) (string, error)
}

type Profile struct {
	Version     string                 `json:"version"`
	Source      string                 `json:"source"`
	TemplateSHA string                 `json:"template_sha"`
	Sections    map[string]SectionRule `json:"sections"`
	Styles      map[string]StyleRule   `json:"styles"`
	PageSetup   PageSetupRule          `json:"page_setup,omitempty"`
	RulePack    RulePack               `json:"rule_pack,omitempty"`
	Header      HeaderFooterRule       `json:"header"`
	Footer      HeaderFooterRule       `json:"footer"`
	AI          *AIProfile             `json:"ai,omitempty"`
	Confidence  float64                `json:"confidence"`
}

type SectionRule struct {
	Label           string `json:"label"`
	PageBreakBefore bool   `json:"page_break_before"`
	SectionBreak    bool   `json:"section_break"`
	DetectedFrom    string `json:"detected_from"`
}

type StyleRule struct {
	Label          string `json:"label"`
	FontEastAsia   string `json:"font_east_asia,omitempty"`
	FontASCII      string `json:"font_ascii,omitempty"`
	FontSizeHalfPt string `json:"font_size_half_pt,omitempty"`
	Bold           bool   `json:"bold,omitempty"`
	Alignment      string `json:"alignment,omitempty"`
	Line           string `json:"line,omitempty"`
	BeforeLines    string `json:"before_lines,omitempty"`
	AfterLines     string `json:"after_lines,omitempty"`
	FirstLineChars string `json:"first_line_chars,omitempty"`
}

type PageSetupRule struct {
	PageWidthTwips    string `json:"page_width_twips,omitempty"`
	PageHeightTwips   string `json:"page_height_twips,omitempty"`
	MarginTopTwips    string `json:"margin_top_twips,omitempty"`
	MarginRightTwips  string `json:"margin_right_twips,omitempty"`
	MarginBottomTwips string `json:"margin_bottom_twips,omitempty"`
	MarginLeftTwips   string `json:"margin_left_twips,omitempty"`
	HeaderMarginTwips string `json:"header_margin_twips,omitempty"`
	FooterMarginTwips string `json:"footer_margin_twips,omitempty"`
	Orientation       string `json:"orientation,omitempty"`
}

type RulePack struct {
	CitationStyle            string   `json:"citation_style,omitempty"`
	ReferenceStandard        string   `json:"reference_standard,omitempty"`
	FigureNumbering          string   `json:"figure_numbering,omitempty"`
	TableNumbering           string   `json:"table_numbering,omitempty"`
	FormulaNumbering         string   `json:"formula_numbering,omitempty"`
	TableStyle               string   `json:"table_style,omitempty"`
	NotesStyle               string   `json:"notes_style,omitempty"`
	RequiredSections         []string `json:"required_sections,omitempty"`
	RequiredFields           []string `json:"required_fields,omitempty"`
	TitleMaxCNChars          int      `json:"title_max_cn_chars,omitempty"`
	TitleMaxENWords          int      `json:"title_max_en_words,omitempty"`
	KeywordMin               int      `json:"keyword_min,omitempty"`
	KeywordMax               int      `json:"keyword_max,omitempty"`
	HeadingNumbering         string   `json:"heading_numbering,omitempty"`
	BodyMinChars             int      `json:"body_min_chars,omitempty"`
	ReferenceMinCount        int      `json:"reference_min_count,omitempty"`
	ReferenceForeignRatioMin float64  `json:"reference_foreign_ratio_min,omitempty"`
	HeaderPolicy             string   `json:"header_policy,omitempty"`
	OddHeaderText            string   `json:"odd_header_text,omitempty"`
	EvenHeaderText           string   `json:"even_header_text,omitempty"`
	HeaderLine               string   `json:"header_line,omitempty"`
	PageNumbering            string   `json:"page_numbering,omitempty"`
	FrontPageFormat          string   `json:"front_page_format,omitempty"`
	BodyPageFormat           string   `json:"body_page_format,omitempty"`
	BodyPageStart            int      `json:"body_page_start,omitempty"`
	BodyPageWrapper          string   `json:"body_page_wrapper,omitempty"`
	HeadingLevels            []string `json:"heading_levels,omitempty"`
	FigureCaptionPosition    string   `json:"figure_caption_position,omitempty"`
	TableCaptionPosition     string   `json:"table_caption_position,omitempty"`
	CaptionStyleKey          string   `json:"caption_style_key,omitempty"`
	ReferenceStyle           string   `json:"reference_style,omitempty"`
	BlindReview              bool     `json:"blind_review,omitempty"`
}

type HeaderFooterRule struct {
	Exists         bool   `json:"exists"`
	Text           string `json:"text,omitempty"`
	HasPageField   bool   `json:"has_page_field,omitempty"`
	HasNumPages    bool   `json:"has_num_pages,omitempty"`
	HasDoubleLine  bool   `json:"has_double_line,omitempty"`
	FontEastAsia   string `json:"font_east_asia,omitempty"`
	FontSizeHalfPt string `json:"font_size_half_pt,omitempty"`
}

type AIProfile struct {
	Enabled bool                   `json:"enabled"`
	RawJSON map[string]interface{} `json:"raw_json,omitempty"`
	RawText string                 `json:"raw_text,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

type Options struct {
	AIEnabled bool
	AIClient  ChatClient
}

type paragraph struct {
	Text string
	XML  string
}

var (
	paragraphPattern = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`)
	textPattern      = regexp.MustCompile(`(?s)<w:t\b[^>]*>(.*?)</w:t>`)
	fontPattern      = regexp.MustCompile(`<w:rFonts\b[^>]*/>`)
	sizePattern      = regexp.MustCompile(`<w:sz\b[^>]*/>`)
	spacingPattern   = regexp.MustCompile(`<w:spacing\b[^>]*/>`)
	indentPattern    = regexp.MustCompile(`<w:ind\b[^>]*/>`)
	jcPattern        = regexp.MustCompile(`<w:jc\b[^>]*/>`)
	sectPrPattern    = regexp.MustCompile(`(?s)<w:sectPr\b[^>]*>.*?</w:sectPr>|<w:sectPr\b[^>]*/>`)
	pgSzPattern      = regexp.MustCompile(`<w:pgSz\b[^>]*/>`)
	pgMarPattern     = regexp.MustCompile(`<w:pgMar\b[^>]*/>`)
	attrPattern      = regexp.MustCompile(`\s([A-Za-z0-9_:]+)="([^"]*)"`)
)

func Build(ctx context.Context, templatePath string, opts Options) (*Profile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	profile, err := Extract(templatePath)
	if err != nil {
		return nil, err
	}
	AttachRulePackSidecar(profile, templatePath)
	if opts.AIEnabled && opts.AIClient != nil {
		AttachAISummary(ctx, profile, opts.AIClient)
	}
	return profile, nil
}

func Extract(templatePath string) (*Profile, error) {
	content, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("read template docx: %w", err)
	}
	sum := sha256.Sum256(content)
	pkg, err := ooxmlpkg.Open(templatePath)
	if err != nil {
		return nil, fmt.Errorf("open template docx: %w", err)
	}
	documentXML, ok := pkg.Get("word/document.xml")
	if !ok {
		return nil, fmt.Errorf("word/document.xml missing")
	}

	profile := &Profile{
		Version:     Version,
		Source:      "local",
		TemplateSHA: hex.EncodeToString(sum[:]),
		Sections:    map[string]SectionRule{},
		Styles:      map[string]StyleRule{},
		PageSetup:   extractPageSetup(string(documentXML)),
		Header:      extractHeaderFooter(pkg, true),
		Footer:      extractHeaderFooter(pkg, false),
		Confidence:  0.76,
	}
	paras := collectParagraphs(string(documentXML))
	profile.RulePack = extractLocalRulePack(paras)
	for index, para := range paras {
		key := classifyParagraph(para.Text)
		if key == "" {
			continue
		}
		profile.Styles[key] = extractStyle(key, para.XML)
		if isSectionKey(key) {
			breakBefore, detectedFrom := detectPageBreakBefore(paras, index)
			profile.Sections[key] = SectionRule{
				Label:           key,
				PageBreakBefore: breakBefore,
				SectionBreak:    strings.Contains(para.XML, "<w:sectPr"),
				DetectedFrom:    detectedFrom,
			}
		}
	}
	return profile, nil
}

func extractLocalRulePack(paras []paragraph) RulePack {
	var rules RulePack
	for _, para := range paras {
		text := normalizeLabel(para.Text)
		if strings.Contains(text, "\u6587\u732e\u5f15\u7528") &&
			strings.Contains(text, "\u4e0a\u6807") &&
			strings.Contains(text, "[1]") {
			rules.CitationStyle = "superscript_bracket"
		}
		if strings.Contains(strings.ToUpper(text), "GB7714") || strings.Contains(strings.ToUpper(text), "GB/T7714") {
			rules.ReferenceStandard = "GB/T 7714"
		}
	}
	return rules
}

func AttachAISummary(ctx context.Context, profile *Profile, client ChatClient) {
	if profile == nil || client == nil {
		return
	}
	if err := ctx.Err(); err != nil {
		profile.AI = &AIProfile{Enabled: true, Error: err.Error()}
		return
	}
	localJSON, _ := json.Marshal(profile)
	prompt := fmt.Sprintf(templateProfileAIPromptTemplate, string(localJSON))
	response, err := client.ChatCompletion(prompt)
	trimmed := trimJSONResponse(response)
	ai := &AIProfile{Enabled: true, RawText: trimmed}
	if err != nil {
		ai.Error = err.Error()
		profile.AI = ai
		return
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		ai.Error = err.Error()
		profile.AI = ai
		return
	}
	ai.RawJSON = raw
	mergeAISummary(profile, raw)
	profile.AI = ai
	profile.Source = "local+deepseek"
	if profile.Confidence == 0 {
		profile.Confidence = 0.88
	}
}

func AttachRulePackSidecar(profile *Profile, templatePath string) {
	if profile == nil || strings.TrimSpace(templatePath) == "" {
		return
	}
	data, err := os.ReadFile(templatePath + ".rules.json")
	if err != nil {
		return
	}
	var raw struct {
		RulePack RulePack `json:"rule_pack"`
	}
	if err := json.Unmarshal(data, &raw); err == nil {
		profile.RulePack = mergeRulePack(profile.RulePack, raw.RulePack)
		return
	}
	var direct RulePack
	if err := json.Unmarshal(data, &direct); err == nil {
		profile.RulePack = mergeRulePack(profile.RulePack, direct)
	}
}

func mergeAISummary(profile *Profile, raw map[string]interface{}) {
	if profile == nil || raw == nil {
		return
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return
	}
	var summary struct {
		Sections map[string]struct {
			PageBreakBefore bool   `json:"page_break_before"`
			Evidence        string `json:"evidence"`
			DetectedFrom    string `json:"detected_from"`
		} `json:"sections"`
		Styles     map[string]StyleRule `json:"styles"`
		PageSetup  PageSetupRule        `json:"page_setup"`
		RulePack   RulePack             `json:"rule_pack"`
		Header     HeaderFooterRule     `json:"header"`
		Footer     HeaderFooterRule     `json:"footer"`
		Confidence float64              `json:"confidence"`
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		return
	}
	if profile.Sections == nil {
		profile.Sections = map[string]SectionRule{}
	}
	for key, section := range summary.Sections {
		if _, exists := profile.Sections[key]; exists {
			continue
		}
		profile.Sections[key] = SectionRule{Label: key, PageBreakBefore: section.PageBreakBefore, DetectedFrom: firstNonEmpty(section.Evidence, section.DetectedFrom)}
	}
	if profile.Styles == nil {
		profile.Styles = map[string]StyleRule{}
	}
	for key, style := range summary.Styles {
		style.Label = key
		local, exists := profile.Styles[key]
		profile.Styles[key] = mergeStyleRule(style, local)
		if exists {
			merged := profile.Styles[key]
			merged.Bold = local.Bold
			profile.Styles[key] = merged
		}
	}
	profile.PageSetup = mergePageSetupRule(summary.PageSetup, profile.PageSetup)
	profile.RulePack = mergeRulePack(summary.RulePack, profile.RulePack)
	if profile.Confidence == 0 && summary.Confidence > 0 {
		profile.Confidence = summary.Confidence
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func mergeStyleRule(base StyleRule, override StyleRule) StyleRule {
	if override.Label != "" {
		base.Label = override.Label
	}
	if override.FontEastAsia != "" {
		base.FontEastAsia = override.FontEastAsia
	}
	if override.FontASCII != "" {
		base.FontASCII = override.FontASCII
	}
	if override.FontSizeHalfPt != "" {
		base.FontSizeHalfPt = override.FontSizeHalfPt
	}
	if override.Bold {
		base.Bold = true
	}
	if override.Alignment != "" {
		base.Alignment = override.Alignment
	}
	if override.Line != "" {
		base.Line = override.Line
	}
	if override.BeforeLines != "" {
		base.BeforeLines = override.BeforeLines
	}
	if override.AfterLines != "" {
		base.AfterLines = override.AfterLines
	}
	if override.FirstLineChars != "" {
		base.FirstLineChars = override.FirstLineChars
	}
	return base
}

func mergePageSetupRule(base PageSetupRule, override PageSetupRule) PageSetupRule {
	if override.PageWidthTwips != "" {
		base.PageWidthTwips = override.PageWidthTwips
	}
	if override.PageHeightTwips != "" {
		base.PageHeightTwips = override.PageHeightTwips
	}
	if override.MarginTopTwips != "" {
		base.MarginTopTwips = override.MarginTopTwips
	}
	if override.MarginRightTwips != "" {
		base.MarginRightTwips = override.MarginRightTwips
	}
	if override.MarginBottomTwips != "" {
		base.MarginBottomTwips = override.MarginBottomTwips
	}
	if override.MarginLeftTwips != "" {
		base.MarginLeftTwips = override.MarginLeftTwips
	}
	if override.HeaderMarginTwips != "" {
		base.HeaderMarginTwips = override.HeaderMarginTwips
	}
	if override.FooterMarginTwips != "" {
		base.FooterMarginTwips = override.FooterMarginTwips
	}
	if override.Orientation != "" {
		base.Orientation = override.Orientation
	}
	return base
}

func mergeRulePack(base RulePack, override RulePack) RulePack {
	if override.CitationStyle != "" {
		base.CitationStyle = override.CitationStyle
	}
	if override.ReferenceStandard != "" {
		base.ReferenceStandard = override.ReferenceStandard
	}
	if override.FigureNumbering != "" {
		base.FigureNumbering = override.FigureNumbering
	}
	if override.TableNumbering != "" {
		base.TableNumbering = override.TableNumbering
	}
	if override.FormulaNumbering != "" {
		base.FormulaNumbering = override.FormulaNumbering
	}
	if override.TableStyle != "" {
		base.TableStyle = override.TableStyle
	}
	if override.NotesStyle != "" {
		base.NotesStyle = override.NotesStyle
	}
	if len(override.RequiredSections) > 0 {
		base.RequiredSections = append([]string(nil), override.RequiredSections...)
	}
	if len(override.RequiredFields) > 0 {
		base.RequiredFields = append([]string(nil), override.RequiredFields...)
	}
	if override.TitleMaxCNChars > 0 {
		base.TitleMaxCNChars = override.TitleMaxCNChars
	}
	if override.TitleMaxENWords > 0 {
		base.TitleMaxENWords = override.TitleMaxENWords
	}
	if override.KeywordMin > 0 {
		base.KeywordMin = override.KeywordMin
	}
	if override.KeywordMax > 0 {
		base.KeywordMax = override.KeywordMax
	}
	if override.HeadingNumbering != "" {
		base.HeadingNumbering = override.HeadingNumbering
	}
	if override.BodyMinChars > 0 {
		base.BodyMinChars = override.BodyMinChars
	}
	if override.ReferenceMinCount > 0 {
		base.ReferenceMinCount = override.ReferenceMinCount
	}
	if override.ReferenceForeignRatioMin > 0 {
		base.ReferenceForeignRatioMin = override.ReferenceForeignRatioMin
	}
	if override.HeaderPolicy != "" {
		base.HeaderPolicy = override.HeaderPolicy
	}
	if override.OddHeaderText != "" {
		base.OddHeaderText = override.OddHeaderText
	}
	if override.EvenHeaderText != "" {
		base.EvenHeaderText = override.EvenHeaderText
	}
	if override.HeaderLine != "" {
		base.HeaderLine = override.HeaderLine
	}
	if override.PageNumbering != "" {
		base.PageNumbering = override.PageNumbering
	}
	if override.FrontPageFormat != "" {
		base.FrontPageFormat = override.FrontPageFormat
	}
	if override.BodyPageFormat != "" {
		base.BodyPageFormat = override.BodyPageFormat
	}
	if override.BodyPageStart > 0 {
		base.BodyPageStart = override.BodyPageStart
	}
	if override.BodyPageWrapper != "" {
		base.BodyPageWrapper = override.BodyPageWrapper
	}
	if len(override.HeadingLevels) > 0 {
		base.HeadingLevels = append([]string(nil), override.HeadingLevels...)
	}
	if override.FigureCaptionPosition != "" {
		base.FigureCaptionPosition = override.FigureCaptionPosition
	}
	if override.TableCaptionPosition != "" {
		base.TableCaptionPosition = override.TableCaptionPosition
	}
	if override.CaptionStyleKey != "" {
		base.CaptionStyleKey = override.CaptionStyleKey
	}
	if override.ReferenceStyle != "" {
		base.ReferenceStyle = override.ReferenceStyle
	}
	if override.BlindReview {
		base.BlindReview = true
	}
	return base
}

func mergeHeaderFooterRule(base HeaderFooterRule, override HeaderFooterRule) HeaderFooterRule {
	if override.Exists {
		base.Exists = true
	}
	if override.Text != "" {
		base.Text = override.Text
	}
	if override.HasPageField {
		base.HasPageField = true
	}
	if override.HasNumPages {
		base.HasNumPages = true
	}
	if override.HasDoubleLine {
		base.HasDoubleLine = true
	}
	if override.FontEastAsia != "" {
		base.FontEastAsia = override.FontEastAsia
	}
	if override.FontSizeHalfPt != "" {
		base.FontSizeHalfPt = override.FontSizeHalfPt
	}
	return base
}

// ApplyFormatRules applies the administrator-edited database JSON as the final
// override on top of values measured from the DOCX template.
func ApplyFormatRules(profile *Profile, data string) error {
	if profile == nil || strings.TrimSpace(data) == "" {
		return nil
	}
	var rules map[string]interface{}
	if err := json.Unmarshal([]byte(data), &rules); err != nil {
		return fmt.Errorf("parse format rules: %w", err)
	}
	if profile.Styles == nil {
		profile.Styles = map[string]StyleRule{}
	}
	applyPageSetupOverrides(&profile.PageSetup, mapRule(rules["page_setup"]))
	applyStyleOverride(profile, "body_start", mapRule(rules["body"]))
	if headings := mapRule(rules["headings"]); headings != nil {
		for level := 1; level <= 4; level++ {
			applyStyleOverride(profile, fmt.Sprintf("heading_%d", level), mapRule(headings[fmt.Sprintf("level%d", level)]))
		}
	}
	for _, item := range []struct{ source, part, target string }{
		{"abstract", "content", "abstract_cn"},
		{"english_abstract", "content", "abstract_en"},
		{"references", "content", "references"},
		{"references", "label", "references_title"},
		{"acknowledgements", "label", "acknowledgements_title"},
		{"table_of_contents", "title", "toc_title"},
	} {
		applyStyleOverride(profile, item.target, mapRule(mapRule(rules[item.source])[item.part]))
	}
	applyHeaderFooterOverrides(profile, rules)
	if raw := mapRule(rules["rule_pack"]); raw != nil {
		encoded, _ := json.Marshal(raw)
		var override RulePack
		if json.Unmarshal(encoded, &override) == nil {
			profile.RulePack = mergeRulePack(profile.RulePack, override)
		}
	}
	return nil
}

func applyPageSetupOverrides(target *PageSetupRule, setup map[string]interface{}) {
	if target == nil || setup == nil {
		return
	}
	margins := mapRule(setup["margins"])
	for _, item := range []struct {
		values []interface{}
		target *string
	}{
		{[]interface{}{margins["top"], setup["margin_top"]}, &target.MarginTopTwips},
		{[]interface{}{margins["right"], setup["margin_right"]}, &target.MarginRightTwips},
		{[]interface{}{margins["bottom"], setup["margin_bottom"]}, &target.MarginBottomTwips},
		{[]interface{}{margins["left"], setup["margin_left"]}, &target.MarginLeftTwips},
		{[]interface{}{nestedRuleValue(setup["header"], "distance"), setup["header"]}, &target.HeaderMarginTwips},
		{[]interface{}{nestedRuleValue(setup["footer"], "distance"), setup["footer"]}, &target.FooterMarginTwips},
	} {
		for _, value := range item.values {
			if twips, ok := centimetersToTwips(value); ok {
				*item.target = twips
				break
			}
		}
	}
	if value, ok := setup["orientation"].(string); ok && value != "" {
		target.Orientation = value
	}
}

func applyStyleOverride(profile *Profile, key string, raw map[string]interface{}) {
	if raw == nil {
		return
	}
	style := profile.Styles[key]
	style.Label = key
	if value := stringRule(raw["font_name"]); value != "" {
		style.FontEastAsia = value
	}
	if value := stringRule(raw["font_name_latin"]); value != "" {
		style.FontASCII = value
	}
	if points, ok := fontPoints(raw); ok {
		style.FontSizeHalfPt = strconv.Itoa(int(points * 2))
	}
	if value, exists := raw["bold"].(bool); exists {
		style.Bold = value
	}
	if value := stringRule(raw["alignment"]); value != "" {
		if value == "justify" {
			value = "both"
		}
		style.Alignment = value
	}
	if multiple, ok := numberRule(raw["line_space"]); ok {
		style.Line = strconv.Itoa(int(multiple * 240))
	}
	if chars, ok := numberRule(raw["first_line_indent"]); ok {
		style.FirstLineChars = strconv.Itoa(int(chars * 100))
	}
	if lines, ok := numberRule(raw["paragraph_before"]); ok {
		style.BeforeLines = strconv.Itoa(int(lines * 100))
	}
	if lines, ok := numberRule(raw["paragraph_after"]); ok {
		style.AfterLines = strconv.Itoa(int(lines * 100))
	}
	profile.Styles[key] = style
}

func applyHeaderFooterOverrides(profile *Profile, rules map[string]interface{}) {
	if header := mapRule(rules["header"]); header != nil {
		profile.Header.Exists = true
		if value := stringRule(header["content"]); value != "" {
			profile.Header.Text = value
		}
		if value := stringRule(header["font_name"]); value != "" {
			profile.Header.FontEastAsia = value
		}
		if points, ok := fontPoints(header); ok {
			profile.Header.FontSizeHalfPt = strconv.Itoa(int(points * 2))
		}
	}
	if footer := mapRule(rules["page_number"]); footer != nil {
		profile.Footer.Exists = true
		profile.Footer.HasPageField = boolRule(footer["has_page_field"], profile.Footer.HasPageField)
		profile.Footer.HasNumPages = boolRule(footer["has_total_pages"], profile.Footer.HasNumPages)
		if value := firstNonEmpty(stringRule(footer["format"]), stringRule(footer["content"])); value != "" {
			profile.Footer.Text = value
		}
		if points, ok := fontPoints(footer); ok {
			profile.Footer.FontSizeHalfPt = strconv.Itoa(int(points * 2))
		}
	}
}

func mapRule(value interface{}) map[string]interface{} {
	result, _ := value.(map[string]interface{})
	return result
}

func nestedRuleValue(value interface{}, key string) interface{} {
	return mapRule(value)[key]
}

func stringRule(value interface{}) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func numberRule(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case string:
		match := regexp.MustCompile(`[-+]?\d*\.?\d+`).FindString(typed)
		if match == "" {
			return 0, false
		}
		number, err := strconv.ParseFloat(match, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func centimetersToTwips(value interface{}) (string, bool) {
	centimeters, ok := numberRule(value)
	if !ok {
		return "", false
	}
	return strconv.Itoa(int(centimeters*567 + 0.5)), true
}

func fontPoints(rule map[string]interface{}) (float64, bool) {
	if points, ok := numberRule(rule["font_size_pt"]); ok {
		return points, true
	}
	pointsByName := map[string]float64{"初号": 42, "小初": 36, "一号": 26, "小一": 24, "二号": 22, "小二": 18, "三号": 16, "小三": 15, "四号": 14, "小四": 12, "五号": 10.5, "小五": 9, "六号": 7.5}
	points, ok := pointsByName[stringRule(rule["font_size"])]
	return points, ok
}

func boolRule(value interface{}, fallback bool) bool {
	if result, ok := value.(bool); ok {
		return result
	}
	return fallback
}

func Marshal(profile *Profile) string {
	if profile == nil {
		return "{}"
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func Parse(data string) (*Profile, error) {
	data = strings.TrimSpace(data)
	if data == "" || data == "{}" || data == "[]" {
		return nil, nil
	}
	var profile Profile
	if err := json.Unmarshal([]byte(data), &profile); err != nil {
		return nil, err
	}
	if profile.Version != Version {
		return nil, fmt.Errorf("unsupported template profile version %q", profile.Version)
	}
	return &profile, nil
}

func extractPageSetup(documentXML string) PageSetupRule {
	section := sectPrPattern.FindString(documentXML)
	if section == "" {
		return PageSetupRule{}
	}
	rule := PageSetupRule{}
	if pgSz := pgSzPattern.FindString(section); pgSz != "" {
		attrs := attrs(pgSz)
		rule.PageWidthTwips = attrs["w:w"]
		rule.PageHeightTwips = attrs["w:h"]
		rule.Orientation = attrs["w:orient"]
	}
	if pgMar := pgMarPattern.FindString(section); pgMar != "" {
		attrs := attrs(pgMar)
		rule.MarginTopTwips = attrs["w:top"]
		rule.MarginRightTwips = attrs["w:right"]
		rule.MarginBottomTwips = attrs["w:bottom"]
		rule.MarginLeftTwips = attrs["w:left"]
		rule.HeaderMarginTwips = attrs["w:header"]
		rule.FooterMarginTwips = attrs["w:footer"]
	}
	return rule
}

func collectParagraphs(documentXML string) []paragraph {
	matches := paragraphPattern.FindAllString(documentXML, -1)
	paras := make([]paragraph, 0, len(matches))
	for _, raw := range matches {
		paras = append(paras, paragraph{Text: extractText(raw), XML: raw})
	}
	return paras
}

func extractText(raw string) string {
	var builder strings.Builder
	for _, match := range textPattern.FindAllStringSubmatch(raw, -1) {
		if len(match) > 1 {
			builder.WriteString(html.UnescapeString(match[1]))
		}
	}
	return strings.TrimSpace(builder.String())
}

func classifyParagraph(text string) string {
	normalized := normalizeLabel(text)
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case normalized == "目录":
		return "toc_title"
	case strings.HasPrefix(normalized, "摘要"):
		return "abstract_cn"
	case strings.HasPrefix(lower, "abstract"):
		return "abstract_en"
	case strings.HasPrefix(normalized, "关键词"):
		return "keywords_cn"
	case strings.HasPrefix(lower, "keywords") || strings.HasPrefix(lower, "key words"):
		return "keywords_en"
	case normalized == "参考文献":
		return "references_title"
	case strings.HasPrefix(normalized, "[") && strings.Contains(normalized, "]"):
		return "references"
	case normalized == "致谢":
		return "acknowledgements_title"
	case regexp.MustCompile(`^1\s+\S+`).MatchString(strings.TrimSpace(text)):
		return "body_start"
	case regexp.MustCompile(`^\d+\.\d+\.\d+\s+\S+`).MatchString(strings.TrimSpace(text)):
		return "heading_3"
	case regexp.MustCompile(`^\d+\.\d+\s+\S+`).MatchString(strings.TrimSpace(text)):
		return "heading_2"
	case regexp.MustCompile(`^\d+\s+\S+`).MatchString(strings.TrimSpace(text)):
		return "heading_1"
	default:
		return ""
	}
}

func isSectionKey(key string) bool {
	return key == "body_start" || key == "references_title" || key == "acknowledgements_title"
}

func detectPageBreakBefore(paras []paragraph, index int) (bool, string) {
	if index < 0 || index >= len(paras) {
		return false, ""
	}
	current := paras[index].XML
	if strings.Contains(current, "<w:pageBreakBefore") || strings.Contains(current, `<w:br w:type="page"`) {
		return true, "current_paragraph"
	}
	if index > 0 {
		previous := paras[index-1].XML
		if strings.Contains(previous, `<w:br w:type="page"`) || strings.Contains(previous, `<w:type w:val="nextPage"`) {
			return true, "previous_paragraph"
		}
	}
	return false, "not_found"
}

func extractStyle(label string, raw string) StyleRule {
	style := StyleRule{Label: label}
	if font := fontPattern.FindString(raw); font != "" {
		attrs := attrs(font)
		style.FontEastAsia = attrs["w:eastAsia"]
		style.FontASCII = attrs["w:ascii"]
	}
	if size := sizePattern.FindString(raw); size != "" {
		style.FontSizeHalfPt = attrs(size)["w:val"]
	}
	style.Bold = strings.Contains(raw, "<w:b") || strings.Contains(raw, "<w:bCs")
	if jc := jcPattern.FindString(raw); jc != "" {
		style.Alignment = attrs(jc)["w:val"]
	}
	if spacing := spacingPattern.FindString(raw); spacing != "" {
		attrs := attrs(spacing)
		style.Line = attrs["w:line"]
		style.BeforeLines = attrs["w:beforeLines"]
		style.AfterLines = attrs["w:afterLines"]
	}
	if ind := indentPattern.FindString(raw); ind != "" {
		style.FirstLineChars = attrs(ind)["w:firstLineChars"]
	}
	return style
}

func extractHeaderFooter(pkg *ooxmlpkg.DocxPackage, header bool) HeaderFooterRule {
	prefix := "word/footer"
	if header {
		prefix = "word/header"
	}
	best := HeaderFooterRule{}
	bestScore := -1
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".xml") {
			continue
		}
		content, ok := pkg.Get(name)
		if !ok {
			continue
		}
		raw := string(content)
		text := extractText(raw)
		rule := HeaderFooterRule{
			Exists:        true,
			Text:          text,
			HasPageField:  strings.Contains(raw, " PAGE "),
			HasNumPages:   strings.Contains(raw, " NUMPAGES ") || hasChineseTotalPageText(text),
			HasDoubleLine: strings.Contains(raw, `w:val="double"`),
		}
		if font := fontPattern.FindString(raw); font != "" {
			rule.FontEastAsia = attrs(font)["w:eastAsia"]
		}
		if size := sizePattern.FindString(raw); size != "" {
			rule.FontSizeHalfPt = attrs(size)["w:val"]
		}
		score := len([]rune(rule.Text))
		if rule.HasPageField {
			score += 100
		}
		if rule.HasNumPages {
			score += 200
		}
		if rule.HasDoubleLine {
			score += 100
		}
		if score > bestScore {
			best, bestScore = rule, score
		}
	}
	return best
}

func hasChineseTotalPageText(text string) bool {
	compact := strings.NewReplacer(" ", "", "\t", "", "\u00a0", "").Replace(text)
	return strings.Contains(compact, "第") && strings.Contains(compact, "共") && strings.Count(compact, "页") >= 2
}

func attrs(tag string) map[string]string {
	result := map[string]string{}
	for _, match := range attrPattern.FindAllStringSubmatch(tag, -1) {
		if len(match) == 3 {
			result[match[1]] = match[2]
		}
	}
	return result
}

func normalizeLabel(text string) string {
	replacer := strings.NewReplacer(" ", "", "\t", "", "\u00a0", "", "　", "")
	return replacer.Replace(strings.TrimSpace(text))
}

func trimJSONResponse(response string) string {
	s := strings.TrimSpace(response)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
