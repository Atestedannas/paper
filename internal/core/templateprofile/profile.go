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
		Header:      extractHeaderFooter(pkg, true),
		Footer:      extractHeaderFooter(pkg, false),
		Confidence:  0.76,
	}
	paras := collectParagraphs(string(documentXML))
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
		existing := profile.Sections[key]
		existing.Label = key
		existing.PageBreakBefore = section.PageBreakBefore
		if section.Evidence != "" {
			existing.DetectedFrom = section.Evidence
		}
		if section.DetectedFrom != "" {
			existing.DetectedFrom = section.DetectedFrom
		}
		profile.Sections[key] = existing
	}
	if profile.Styles == nil {
		profile.Styles = map[string]StyleRule{}
	}
	for key, style := range summary.Styles {
		style.Label = key
		profile.Styles[key] = mergeStyleRule(profile.Styles[key], style)
	}
	profile.Header = mergeHeaderFooterRule(profile.Header, summary.Header)
	profile.Footer = mergeHeaderFooterRule(profile.Footer, summary.Footer)
	if summary.Confidence > 0 {
		profile.Confidence = summary.Confidence
	}
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
	for index := 1; index <= 5; index++ {
		name := fmt.Sprintf("%s%d.xml", prefix, index)
		content, ok := pkg.Get(name)
		if !ok {
			continue
		}
		raw := string(content)
		rule := HeaderFooterRule{
			Exists:        true,
			Text:          extractText(raw),
			HasPageField:  strings.Contains(raw, " PAGE "),
			HasNumPages:   strings.Contains(raw, " NUMPAGES "),
			HasDoubleLine: strings.Contains(raw, `w:val="double"`),
		}
		if font := fontPattern.FindString(raw); font != "" {
			rule.FontEastAsia = attrs(font)["w:eastAsia"]
		}
		if size := sizePattern.FindString(raw); size != "" {
			rule.FontSizeHalfPt = attrs(size)["w:val"]
		}
		return rule
	}
	return HeaderFooterRule{}
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
