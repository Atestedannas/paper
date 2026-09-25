package templateprofile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"os"
	pathpkg "path"
	"regexp"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpatch"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

// Version identifies serialized diagnostic profiles. Workflow execution reads
// the selected DOCX directly and does not use these snapshots as a cache.
const Version = "template-profile-v8"

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
    "font_east_asia": "[请根据实际模板填写，示例仅供参考]",
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
    "heading_1": {"font_east_asia":"[根据本地解析填入]","font_size_half_pt":"[解析值]","bold":true,"alignment":"left","line":"360"},
    "body": {"font_east_asia":"[根据本地解析填入]","font_ascii":"[根据本地解析填入]","font_size_half_pt":"[解析值]","alignment":"both","first_line_chars":"200","line":"360"},
    "references": {"font_east_asia":"[根据本地解析填入]","font_size_half_pt":"[解析值]","first_line_chars":"0","line":"360"}
  },
  "confidence": 0.88
}

## 本地解析 JSON
%s`

type ChatClient interface {
	ChatCompletion(prompt string) (string, error)
}

type Profile struct {
	ExcludedSamples []ExcludedStyleSample `json:"excluded_style_samples,omitempty"`
	Conflicts       []RuleConflict        `json:"rule_conflicts,omitempty"`
	// Blank paragraphs are read from the source DOCX for each run, never cached.
	HeadingBlankBefore *StyleRule             `json:"-"`
	HeadingBlankAfter  *StyleRule             `json:"-"`
	Version            string                 `json:"version"`
	Source             string                 `json:"source"`
	TemplateSHA        string                 `json:"template_sha"`
	Sections           map[string]SectionRule `json:"sections"`
	Styles             map[string]StyleRule   `json:"styles"`
	SectionFormats     SectionFormatMap       `json:"section_formats,omitempty"`
	PageSetup          PageSetupRule          `json:"page_setup,omitempty"`
	RulePack           RulePack               `json:"rule_pack,omitempty"`
	Header             HeaderFooterRule       `json:"header"`
	Footer             HeaderFooterRule       `json:"footer"`
	HeaderFirst        HeaderFooterRule       `json:"header_first,omitempty"`
	HeaderEven         HeaderFooterRule       `json:"header_even,omitempty"`
	FooterFirst        HeaderFooterRule       `json:"footer_first,omitempty"`
	FooterEven         HeaderFooterRule       `json:"footer_even,omitempty"`
	AI                 *AIProfile             `json:"ai,omitempty"`
	Numbering          *NumberingProfile      `json:"numbering,omitempty"` // OOXML numbering.xml 精确提取
	Confidence         float64                `json:"confidence"`
	Scope              *TemplateScope         `json:"template_scope,omitempty"`
	Requirements       []string               `json:"requirement_evidence,omitempty"`
}

// TemplateScope prevents requirements text and the wrong embedded example
// from contributing style evidence.
type TemplateScope struct {
	RequirementsRange  [2]int `json:"requirements_range"`
	ScienceSampleRange [2]int `json:"science_sample_range"`
	ArtsSampleRange    [2]int `json:"arts_sample_range"`
	SelectedSample     string `json:"selected_sample"`
}

const (
	SampleScience = "science"
	SampleArts    = "arts"
)

// SectionFormatMap exposes the semantic template regions used by classifiers.
// Values still use StyleRule so extraction and application share one schema.
type SectionFormatMap map[string]StyleRule

type SectionRule struct {
	Label                 string `json:"label"`
	PageBreakBefore       bool   `json:"page_break_before"`
	SectionBreak          bool   `json:"section_break"`
	SectionBreakType      string `json:"section_break_type,omitempty"`
	BlankParagraphsBefore int    `json:"blank_paragraphs_before,omitempty"`
	DetectedFrom          string `json:"detected_from"`
}

type StyleRule struct {
	SelectionEvidence string                      `json:"selection_evidence,omitempty" audit:"-"`
	ReviewRequired    bool                        `json:"review_required,omitempty" audit:"-"`
	PropertyEvidence  map[string]PropertyEvidence `json:"property_evidence,omitempty" audit:"-"`
	RunStyles         []ResolvedRun               `json:"run_styles,omitempty" audit:"-"`
	Label             string                      `json:"label"`
	FontEastAsia      string                      `json:"font_east_asia,omitempty"`
	FontASCII         string                      `json:"font_ascii,omitempty"`
	FontHAnsi         string                      `json:"font_hansi,omitempty"`
	FontCS            string                      `json:"font_cs,omitempty"`
	FontASCIITheme    string                      `json:"font_ascii_theme,omitempty"`
	FontHAnsiTheme    string                      `json:"font_hansi_theme,omitempty"`
	FontEastAsiaTheme string                      `json:"font_east_asia_theme,omitempty"`
	FontCSTheme       string                      `json:"font_cs_theme,omitempty"`
	FontHint          string                      `json:"font_hint,omitempty"`
	FontSizeHalfPt    string                      `json:"font_size_half_pt,omitempty"`
	ComplexSizeHalfPt string                      `json:"complex_size_half_pt,omitempty"`
	Bold              bool                        `json:"bold,omitempty"`
	BoldSet           bool                        `json:"bold_set,omitempty"`
	BoldEvidence      string                      `json:"bold_evidence,omitempty" audit:"-"`
	Italic            bool                        `json:"italic,omitempty"`
	ItalicSet         bool                        `json:"italic_set,omitempty"`
	ItalicEvidence    string                      `json:"italic_evidence,omitempty" audit:"-"`
	KeepNext          bool                        `json:"keep_next,omitempty"`
	KeepNextSet       bool                        `json:"keep_next_set,omitempty"`
	KeepLines         bool                        `json:"keep_lines,omitempty"`
	KeepLinesSet      bool                        `json:"keep_lines_set,omitempty"`
	WidowControl      bool                        `json:"widow_control,omitempty"`
	WidowControlSet   bool                        `json:"widow_control_set,omitempty"`
	Alignment         string                      `json:"alignment,omitempty"`
	Line              string                      `json:"line,omitempty"`
	LineRule          string                      `json:"line_rule,omitempty"`
	BeforeTwips       string                      `json:"before_twips,omitempty"`
	AfterTwips        string                      `json:"after_twips,omitempty"`
	BeforeLines       string                      `json:"before_lines,omitempty"`
	AfterLines        string                      `json:"after_lines,omitempty"`
	FirstLineChars    string                      `json:"first_line_chars,omitempty"`
	FirstLineTwips    string                      `json:"first_line_twips,omitempty"`
	OutlineLevel      string                      `json:"outline_level,omitempty"`
	SampleCount       int                         `json:"sample_count,omitempty"`
	Confidence        float64                     `json:"confidence,omitempty"`
	Sources           []StyleSource               `json:"sources,omitempty"`
	InheritanceChain  []string                    `json:"inheritance_chain,omitempty"`
}

type StyleSource struct {
	PropertyEvidence map[string]PropertyEvidence `json:"property_evidence,omitempty"`
	EffectiveBold    *bool                       `json:"effective_bold,omitempty"`
	BoldEvidence     string                      `json:"bold_evidence,omitempty" audit:"-"`
	Part             string                      `json:"part"`
	ParagraphIndex   int                         `json:"paragraph_index"`
	Text             string                      `json:"text,omitempty"`
	ParagraphStyleID string                      `json:"paragraph_style_id,omitempty"`
	RunStyleID       string                      `json:"run_style_id,omitempty"`
	InheritanceChain []string                    `json:"inheritance_chain,omitempty"`
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
	HasUnderline   bool   `json:"has_underline,omitempty"`
	FontEastAsia   string `json:"font_east_asia,omitempty"`
	FontAscii      string `json:"font_ascii,omitempty"`
	FontSizeHalfPt string `json:"font_size_half_pt,omitempty"`
}

type AIProfile struct {
	Enabled bool                   `json:"enabled"`
	RawJSON map[string]interface{} `json:"raw_json,omitempty"`
	RawText string                 `json:"raw_text,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// ─────────────────────────────────────────────────────
// Numbering.xml 精确提取
// ─────────────────────────────────────────────────────

// NumberingLevel mirrors a single <w:lvl> element from word/numbering.xml.
// Extracted directly from OOXML — no AI inference, no guessing.
type NumberingLevel struct {
	Level      int       `json:"level"`       // 0-based ilvl
	NumFmt     string    `json:"num_fmt"`     // decimal / chineseCounting / upperLetter / ...
	LvlText    string    `json:"lvl_text"`    // e.g. “%1”, “%1.%2”, “第%1章”
	Start      int       `json:"start"`       // start value (default 1)
	PStyle     string    `json:"p_style"`     // linked paragraph style (Heading1, Heading2, …)
	IsLgl      bool      `json:"is_lgl"`      // <w:isLgl/> flag
	LvlRestart int       `json:"lvl_restart"` // <w:lvlRestart w:val="..."/>  (-1 = never restart)
	Style      StyleRule `json:"style,omitempty"`
}

// NumberingProfile holds every abstractNum definition found in numbering.xml.
type NumberingProfile struct {
	AbstractNums map[int][]NumberingLevel `json:"abstract_nums"` // abstractNumId → levels
	NumToAbs     map[int]int              `json:"num_to_abs"`    // numId → abstractNumId
}

type numberingReference struct {
	NumID, Level       int
	NumIDSet, LevelSet bool
}

type styleDefinitionSet struct {
	Raw                   map[string]string
	DocDefaultsXML        string
	DefaultParagraphStyle string
	Resolved              map[string]StyleRule
	Local                 map[string]StyleRule
	BasedOn               map[string]string
	Numbering             map[string]numberingReference
	NameToID              map[string]string
	DocDefaults           StyleRule
}

func (definitions styleDefinitionSet) effectiveNumberingReference(styleID string) numberingReference {
	chain, seen := []string{}, map[string]bool{}
	for styleID != "" && len(chain) < 64 && !seen[styleID] {
		seen[styleID] = true
		chain = append(chain, styleID)
		styleID = definitions.BasedOn[styleID]
	}
	reference := numberingReference{}
	for index := len(chain) - 1; index >= 0; index-- {
		reference = mergeNumberingReference(reference, definitions.Numbering[chain[index]])
	}
	return reference
}

// HeadingNumberingLookup returns the heading levels extracted from numbering.xml,
// keyed by style name (e.g. "Heading1" → {Level, NumFmt, LvlText, Start}).
// Only levels that have a non-empty PStyle are included.
func (np *NumberingProfile) HeadingNumberingLookup() map[string]NumberingLevel {
	lookup := make(map[string]NumberingLevel)
	for _, levels := range np.AbstractNums {
		for _, lvl := range levels {
			if lvl.PStyle != "" {
				// Normalize to title-case for consistent lookup
				key := strings.ToLower(lvl.PStyle)
				if _, exists := lookup[key]; !exists {
					lookup[key] = lvl
				}
			}
		}
	}
	return lookup
}

func (np *NumberingProfile) effectiveLevel(reference numberingReference, paragraphStyleID string) (NumberingLevel, bool) {
	if np == nil {
		return NumberingLevel{}, false
	}
	level := 0
	if reference.LevelSet {
		level = reference.Level
	}
	if reference.NumIDSet {
		abstractID, ok := np.NumToAbs[reference.NumID]
		if !ok {
			return NumberingLevel{}, false
		}
		for _, candidate := range np.AbstractNums[abstractID] {
			if candidate.Level == level {
				return candidate, true
			}
		}
		return NumberingLevel{}, false
	}
	if paragraphStyleID == "" {
		return NumberingLevel{}, false
	}
	bestAbstractID, found := 0, false
	best := NumberingLevel{}
	for abstractID, levels := range np.AbstractNums {
		for _, candidate := range levels {
			if !strings.EqualFold(candidate.PStyle, paragraphStyleID) || reference.LevelSet && candidate.Level != level {
				continue
			}
			if !found || abstractID < bestAbstractID {
				bestAbstractID, best, found = abstractID, candidate, true
			}
		}
	}
	return best, found
}

func extractNumberingReference(raw string) numberingReference {
	properties := numberingPropertiesPattern.FindString(raw)
	if properties == "" {
		return numberingReference{}
	}
	reference := numberingReference{}
	if match := numberingIDPattern.FindStringSubmatch(properties); len(match) == 2 {
		reference.NumID, _ = strconv.Atoi(match[1])
		reference.NumIDSet = true
	}
	if match := numberingLevelIDPattern.FindStringSubmatch(properties); len(match) == 2 {
		reference.Level, _ = strconv.Atoi(match[1])
		reference.LevelSet = true
	}
	return reference
}

func mergeNumberingReference(base, override numberingReference) numberingReference {
	if override.NumIDSet {
		base.NumID, base.NumIDSet = override.NumID, true
	}
	if override.LevelSet {
		base.Level, base.LevelSet = override.Level, true
	}
	return base
}

// BuildHeadingPatterns converts numbering level definitions into compiled regexps.
// Returns a map from profile key ("heading_1", "heading_2", …) to regexp.
// The patterns precisely match what the template's numbering.xml defines,
// replacing the hardcoded "^\d+\.\d+" guesses.
func (np *NumberingProfile) BuildHeadingPatterns() map[string]*regexp.Regexp {
	lookup := np.HeadingNumberingLookup()
	patterns := make(map[string]*regexp.Regexp)

	// style→profile mapping: "heading1"→"heading_1", etc.
	styleToProfile := map[string]string{
		"heading1": "heading_1",
		"heading2": "heading_2",
		"heading3": "heading_3",
		"heading4": "heading_4",
		"heading5": "heading_4",
	}

	for styleName, lvl := range lookup {
		profileKey, ok := styleToProfile[styleName]
		if !ok {
			// Try numeric suffix extraction
			for s, p := range styleToProfile {
				if strings.HasSuffix(styleName, s[len(s)-1:]) && len(styleName) > len(s)-1 {
					profileKey = p
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}

		pattern := lvlTextToPattern(lvl.LvlText, lvl.NumFmt)
		if pattern != "" {
			patterns[profileKey] = regexp.MustCompile(pattern)
		}
	}
	return patterns
}

// lvlTextToPattern converts a numbering lvlText + numFmt into a Go regexp.
//
// Examples:
//
//	LvlText       NumFmt          → Pattern
//	%1            decimal         → ^\d+[.\s]?\S
//	%1.%2         decimal         → ^\d+\.\d+[.\s]?\S
//	第%1章        chineseCounting  → ^第[一二三四五六七八九十百零〇两0-9]+章\s*\S
//	%1.%2.%3      decimal         → ^\d+\.\d+\.\d+[.\s]?\S
func lvlTextToPattern(lvlText, numFmt string) string {
	if lvlText == "" {
		return ""
	}

	// Map numFmt to a character class for the placeholder replacement
	numFmtToCharClass := map[string]string{
		"decimal":                 `\d+`,
		"chineseCounting":         `[一二三四五六七八九十百零〇两0-9]+`,
		"chineseCountingThousand": `[一二三四五六七八九十百零〇两0-9]+`,
		"upperLetter":             `[A-Z]`,
		"lowerLetter":             `[a-z]`,
		"upperRoman":              `[IVXLCDM]+`,
		"lowerRoman":              `[ivxlcdm]+`,
		"japaneseCounting":        `[一二三四五六七八九十百零〇]+`,
	}

	result := regexp.QuoteMeta(lvlText)

	// If lvlText is just "%1" with no surrounding text, add trailing separator flexibility
	hasSurrounding := len(lvlText) > 2

	// Replace %1, %2, %3, ... with appropriate character classes
	for i := 1; i <= 9; i++ {
		placeholder := regexp.QuoteMeta(fmt.Sprintf("%%%d", i))
		replacement, ok := numFmtToCharClass[numFmt]
		if !ok {
			replacement = `\S+`
		}
		result = strings.ReplaceAll(result, placeholder, replacement)
	}

	// Anchor at start, add trailing content requirement
	if hasSurrounding {
		// Pattern like "第%1章" → anchored, text follows naturally
		result = `^` + result + `\s*\S*`
	} else {
		// Pattern like "%1" or "%1.%2" → allow optional dot/space after number
		result = `^` + result + `[.\s]?\S`
	}

	return result
}

type Options struct {
	AIEnabled bool
	AIClient  ChatClient
}

type paragraph struct {
	InTable             bool
	BlankLineAnnotation bool
	Text                string
	XML                 string
}

var (
	paragraphPattern             = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`)
	documentBodyNodePattern      = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>|<w:tbl(?:\s[^>]*)?>.*?</w:tbl>`)
	textBoxContentPattern        = regexp.MustCompile(`(?s)<w:txbxContent(?:\s[^>]*)?>.*?</w:txbxContent>`)
	textPattern                  = regexp.MustCompile(`(?s)<w:t\b[^>]*>(.*?)</w:t>`)
	fontPattern                  = regexp.MustCompile(`<w:rFonts\b[^>]*/>`)
	sizePattern                  = regexp.MustCompile(`<w:sz\b[^>]*/>`)
	sizeCsPattern                = regexp.MustCompile(`<w:szCs\b[^>]*/>`)
	spacingPattern               = regexp.MustCompile(`<w:spacing\b[^>]*/>`)
	keepNextPattern              = regexp.MustCompile(`<w:keepNext\b[^>]*/>`)
	keepLinesPattern             = regexp.MustCompile(`<w:keepLines\b[^>]*/>`)
	widowControlPattern          = regexp.MustCompile(`<w:widowControl\b[^>]*/>`)
	runPropertiesPattern         = regexp.MustCompile(`(?s)<w:rPr\b[^>]*>.*?</w:rPr>|<w:rPr\b[^>]*/>`)
	runElementPattern            = regexp.MustCompile(`(?s)<w:r(?:\s[^>]*)?>.*?</w:r>`)
	paragraphRunPropsPattern     = regexp.MustCompile(`(?s)<w:pPr\b[^>]*>.*?<w:rPr\b[^>]*>.*?</w:rPr>.*?</w:pPr>`)
	indentPattern                = regexp.MustCompile(`<w:ind\b[^>]*/>`)
	outlinePattern               = regexp.MustCompile(`<w:outlineLvl\b[^>]*/>`)
	styleElementPattern          = regexp.MustCompile(`(?s)<w:style\b[^>]*?(?:/>|>.*?</w:style>)`)
	styleIDPattern               = regexp.MustCompile(`<w:style\b[^>]*\bw:styleId="([^"]+)"`)
	styleTypePattern             = regexp.MustCompile(`<w:style\b[^>]*\bw:type="([^"]+)"`)
	styleNamePattern             = regexp.MustCompile(`<w:name\b[^>]*\bw:val="([^"]+)"`)
	basedOnPattern               = regexp.MustCompile(`<w:basedOn\b[^>]*\bw:val="([^"]+)"`)
	docDefaultsPattern           = regexp.MustCompile(`(?s)<w:docDefaults\b[^>]*>(.*?)</w:docDefaults>`)
	paragraphStyleIDPattern      = regexp.MustCompile(`<w:pStyle\b[^>]*\bw:val="([^"]+)"`)
	runStyleIDPattern            = regexp.MustCompile(`<w:rStyle\b[^>]*\bw:val="([^"]+)"`)
	numberingPropertiesPattern   = regexp.MustCompile(`(?s)<w:numPr\b[^>]*>.*?</w:numPr>|<w:numPr\b[^>]*/>`)
	numberingIDPattern           = regexp.MustCompile(`<w:numId\b[^>]*\bw:val="(\d+)"`)
	numberingLevelIDPattern      = regexp.MustCompile(`<w:ilvl\b[^>]*\bw:val="(\d+)"`)
	jcPattern                    = regexp.MustCompile(`<w:jc\b[^>]*/>`)
	sectPrPattern                = regexp.MustCompile(`(?s)<w:sectPr\b[^>]*>.*?</w:sectPr>|<w:sectPr\b[^>]*/>`)
	pgSzPattern                  = regexp.MustCompile(`<w:pgSz\b[^>]*/>`)
	pgMarPattern                 = regexp.MustCompile(`<w:pgMar\b[^>]*/>`)
	attrPattern                  = regexp.MustCompile(`\s([A-Za-z0-9_:]+)="([^"]*)"`)
	headerFooterReferencePattern = regexp.MustCompile(`<w:(?:header|footer)Reference\b[^>]*/>`)
	relationshipPattern          = regexp.MustCompile(`<Relationship\b[^>]*/>`)
	collegeNamePattern           = regexp.MustCompile(`[\p{Han}A-Za-z0-9·-]+(?:大学|学院)`)
	bodyStartArabicPattern       = regexp.MustCompile(`^1\s+[^\d.]\S*`)
	heading1ArabicPattern        = regexp.MustCompile(`^\d+\s+\S+`)
	heading2ArabicPattern        = regexp.MustCompile(`^\d+\.\d+\s+\S+`)
	heading3ArabicPattern        = regexp.MustCompile(`^\d+\.\d+\.\d+\s+\S+`)
	heading1ChinesePattern       = regexp.MustCompile(`^第[一二三四五六七八九十百零〇两0-9]+章\s*\S*`)
	bodyStartChinesePattern      = regexp.MustCompile(`^第[一1]章\s*\S*`)
	heading1ChineseListPattern   = regexp.MustCompile(`^[一二三四五六七八九十]+[、．.]\s*\S+`)
	bodyStartChineseListPattern  = regexp.MustCompile(`^一[、．.]\s*\S+`)
	captionNumberPattern         = regexp.MustCompile(`^(?:\x{8868}|\x{56fe})\s*\d+(?:[.\-]\d+)*`)
	captionWithSpacePattern      = regexp.MustCompile(`^(?:\x{8868}|\x{56fe})\s*\d+(?:[.\-]\d+)*\s+\S+`)
)

type themeFontFamily struct {
	Latin, EastAsia, Complex string
	Scripts                  map[string]string
}

type themeFontResolver struct {
	Major, Minor                  themeFontFamily
	EastAsiaScript, ComplexScript string
}

func (resolver themeFontResolver) resolve(reference, slot string) string {
	reference = strings.ToLower(strings.TrimSpace(reference))
	var family themeFontFamily
	switch {
	case strings.HasPrefix(reference, "major"):
		family = resolver.Major
	case strings.HasPrefix(reference, "minor"):
		family = resolver.Minor
	default:
		return ""
	}
	switch {
	case strings.HasSuffix(reference, "ascii"), strings.HasSuffix(reference, "hansi"):
		slot = "ascii"
	case strings.HasSuffix(reference, "eastasia"):
		slot = "eastAsia"
	case strings.HasSuffix(reference, "bidi"):
		slot = "cs"
	}
	switch slot {
	case "ascii", "hAnsi":
		return family.Latin
	case "eastAsia":
		if family.EastAsia != "" {
			return family.EastAsia
		}
		return family.Scripts[resolver.EastAsiaScript]
	case "cs":
		if family.Complex != "" {
			return family.Complex
		}
		return family.Scripts[resolver.ComplexScript]
	default:
		return ""
	}
}

func ExtractCollegeName(headerText, fallback string) string {
	best := ""
	for _, match := range collegeNamePattern.FindAllString(headerText, -1) {
		if len([]rune(match)) > len([]rune(best)) {
			best = match
		}
	}
	if best != "" {
		return best
	}
	return strings.TrimSpace(fallback)
}

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

func extractThemeFontResolver(pkg *ooxmlpkg.DocxPackage, stylesXML string) themeFontResolver {
	return extractThemeFontResolverFromXML(extractThemePart(pkg).XML, stylesXML)
}

func extractThemeFontResolverFromXML(themeXML, stylesXML string) themeFontResolver {
	resolver := themeFontResolver{
		Major: themeFontFamily{Scripts: map[string]string{}},
		Minor: themeFontFamily{Scripts: map[string]string{}},
	}
	if themeXML == "" {
		return resolver
	}
	decoder := xml.NewDecoder(strings.NewReader(themeXML))
	current := ""
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return resolver
		}
		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "majorFont", "minorFont":
				current = element.Name.Local
			case "latin", "ea", "cs", "font":
				if current == "" {
					continue
				}
				family := &resolver.Major
				if current == "minorFont" {
					family = &resolver.Minor
				}
				typeface, script := "", ""
				for _, attribute := range element.Attr {
					switch attribute.Name.Local {
					case "typeface":
						typeface = attribute.Value
					case "script":
						script = attribute.Value
					}
				}
				switch element.Name.Local {
				case "latin":
					family.Latin = typeface
				case "ea":
					family.EastAsia = typeface
				case "cs":
					family.Complex = typeface
				case "font":
					if script != "" && typeface != "" {
						family.Scripts[script] = typeface
					}
				}
			}
		case xml.EndElement:
			if element.Name.Local == "majorFont" || element.Name.Local == "minorFont" {
				current = ""
			}
		}
	}
	for _, tag := range regexp.MustCompile(`<w:lang\b[^>]*/>`).FindAllString(stylesXML, -1) {
		values := attrs(tag)
		if resolver.EastAsiaScript == "" {
			resolver.EastAsiaScript = languageThemeScript(values["w:eastAsia"])
		}
		if resolver.ComplexScript == "" {
			resolver.ComplexScript = languageThemeScript(values["w:bidi"])
		}
		if resolver.EastAsiaScript != "" && resolver.ComplexScript != "" {
			break
		}
	}
	return resolver
}

func languageThemeScript(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	switch {
	case strings.HasPrefix(language, "zh-hant"),
		strings.HasPrefix(language, "zh-tw"),
		strings.HasPrefix(language, "zh-hk"),
		strings.HasPrefix(language, "zh-mo"):
		return "Hant"
	case strings.HasPrefix(language, "zh"):
		return "Hans"
	case strings.HasPrefix(language, "ja"):
		return "Jpan"
	case strings.HasPrefix(language, "ko"):
		return "Hang"
	case strings.HasPrefix(language, "ar"):
		return "Arab"
	case strings.HasPrefix(language, "he"), strings.HasPrefix(language, "iw"):
		return "Hebr"
	case strings.HasPrefix(language, "th"):
		return "Thai"
	default:
		return ""
	}
}

func materializeThemeFonts(raw string, resolver themeFontResolver) string {
	return fontPattern.ReplaceAllStringFunc(raw, func(tag string) string {
		values := attrs(tag)
		for _, slot := range []struct {
			fontAttribute, themeAttribute, resolverSlot string
		}{
			{"w:ascii", "w:asciiTheme", "ascii"},
			{"w:hAnsi", "w:hAnsiTheme", "hAnsi"},
			{"w:eastAsia", "w:eastAsiaTheme", "eastAsia"},
			{"w:cs", "w:cstheme", "cs"},
		} {
			if font := resolver.resolve(values[slot.themeAttribute], slot.resolverSlot); font != "" {
				tag = setXMLAttribute(tag, slot.fontAttribute, font)
			}
		}
		return tag
	})
}

func setXMLAttribute(tag, name, value string) string {
	needle := " " + name + `="`
	if start := strings.Index(tag, needle); start >= 0 {
		valueStart := start + len(needle)
		if valueEnd := strings.Index(tag[valueStart:], `"`); valueEnd >= 0 {
			return tag[:valueStart] + html.EscapeString(value) + tag[valueStart+valueEnd:]
		}
	}
	if end := strings.LastIndex(tag, "/>"); end >= 0 {
		return tag[:end] + needle + html.EscapeString(value) + `"` + tag[end:]
	}
	return tag
}

// parseNumberingXML reads word/numbering.xml from the DOCX package and extracts
// all abstractNum definitions into a NumberingProfile.
// Returns nil, nil if numbering.xml is absent (template has no numbering definitions).
func parseNumberingXML(pkg *ooxmlpkg.DocxPackage) (*NumberingProfile, error) {
	return parseNumberingXMLWithTheme(pkg, themeFontResolver{})
}

func parseNumberingXMLWithTheme(pkg *ooxmlpkg.DocxPackage, themeFonts themeFontResolver) (*NumberingProfile, error) {
	return parseNumberingPartWithTheme(extractNumberingPart(pkg), themeFonts), nil
}

func parseNumberingPartWithTheme(part templatePart, themeFonts themeFontResolver) *NumberingProfile {
	if part.Name == "" {
		return nil
	}
	return ParseNumberingFromRawXML(materializeThemeFonts(part.XML, themeFonts))
}

// ParseNumberingFromRawXML extracts numbering profile from raw numbering.xml content.
// Exported for use by TemplateParser and other packages that read DOCX differently.
func ParseNumberingFromRawXML(xmlStr string) *NumberingProfile {

	np := &NumberingProfile{
		AbstractNums: make(map[int][]NumberingLevel),
		NumToAbs:     make(map[int]int),
	}

	// Extract abstractNum blocks: <w:abstractNum w:abstractNumId="N"> ... </w:abstractNum>
	absNumPattern := regexp.MustCompile(`<w:abstractNum\s+[^>]*?w:abstractNumId="(\d+)"[^>]*>(.*?)</w:abstractNum>`)
	for _, absMatch := range absNumPattern.FindAllStringSubmatch(xmlStr, -1) {
		absID, _ := strconv.Atoi(absMatch[1])
		absBody := absMatch[2]

		var levels []NumberingLevel
		// Extract level blocks: <w:lvl w:ilvl="N"> ... </w:lvl>
		lvlPattern := regexp.MustCompile(`<w:lvl\s+[^>]*?w:ilvl="(\d+)"[^>]*>(.*?)</w:lvl>`)
		for _, lvlMatch := range lvlPattern.FindAllStringSubmatch(absBody, -1) {
			ilvl, _ := strconv.Atoi(lvlMatch[1])
			lvlBody := lvlMatch[2]

			level := NumberingLevel{
				Level:      ilvl,
				Start:      1,
				LvlRestart: -1, // default: never restart
			}

			if m := regexp.MustCompile(`<w:numFmt\s+[^>]*?w:val="([^"]*)"`).FindStringSubmatch(lvlBody); m != nil {
				level.NumFmt = m[1]
			}
			if m := regexp.MustCompile(`<w:lvlText\s+[^>]*?w:val="([^"]*)"`).FindStringSubmatch(lvlBody); m != nil {
				level.LvlText = m[1]
			}
			if m := regexp.MustCompile(`<w:start\s+[^>]*?w:val="(\d+)"`).FindStringSubmatch(lvlBody); m != nil {
				level.Start, _ = strconv.Atoi(m[1])
			}
			if m := regexp.MustCompile(`<w:pStyle\s+[^>]*?w:val="([^"]*)"`).FindStringSubmatch(lvlBody); m != nil {
				level.PStyle = m[1]
			}
			if strings.Contains(lvlBody, "<w:isLgl") {
				level.IsLgl = true
			}
			if m := regexp.MustCompile(`<w:lvlRestart\s+[^>]*?w:val="(\d+)"`).FindStringSubmatch(lvlBody); m != nil {
				level.LvlRestart, _ = strconv.Atoi(m[1])
			}
			level.Style = extractNumberingLevelStyle(absID, ilvl, lvlBody)

			levels = append(levels, level)
		}
		np.AbstractNums[absID] = levels
	}

	// Extract num mappings: <w:num w:numId="N"> <w:abstractNumId w:val="M"/> </w:num>
	numPattern := regexp.MustCompile(`<w:num\s+[^>]*?w:numId="(\d+)"[^>]*>.*?<w:abstractNumId\s+[^>]*?w:val="(\d+)"`)
	for _, numMatch := range numPattern.FindAllStringSubmatch(xmlStr, -1) {
		numID, _ := strconv.Atoi(numMatch[1])
		absID, _ := strconv.Atoi(numMatch[2])
		np.NumToAbs[numID] = absID
	}

	if len(np.AbstractNums) == 0 {
		return nil
	}
	return np
}

func extractNumberingLevelStyle(abstractID, level int, raw string) StyleRule {
	label := fmt.Sprintf("numbering:%d:%d", abstractID, level)
	style := extractStyle(label, raw)
	if runProperties := runPropertiesPattern.FindString(raw); runProperties != "" {
		if hasBoldDeclaration(runProperties) {
			style.Bold, style.BoldSet = enabledBold(runProperties), true
		}
		if hasItalicDeclaration(runProperties) {
			style.Italic, style.ItalicSet = enabledProperty(runProperties, "i"), true
		}
	}
	style.InheritanceChain = []string{label}
	return style
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
	parts, err := extractTemplateParts(pkg)
	if err != nil {
		return nil, err
	}
	documentXML := parts.Document.XML
	stylesText := parts.Styles.XML
	themeFonts := extractThemeFontResolverFromXML(parts.Theme.XML, stylesText)
	documentXML = materializeThemeFonts(documentXML, themeFonts)
	stylesText = materializeThemeFonts(stylesText, themeFonts)

	profile := &Profile{
		Version:     Version,
		Source:      "local",
		TemplateSHA: hex.EncodeToString(sum[:]),
		Sections:    map[string]SectionRule{},
		Styles:      map[string]StyleRule{},
		PageSetup:   extractPageSetup(documentXML),
		Header:      extractHeaderFooterParts(parts.Headers, themeFonts),
		Footer:      extractHeaderFooterParts(parts.Footers, themeFonts),
		Confidence:  0.76,
	}
	paras := collectParagraphs(documentXML)
	var scope TemplateScope
	allParas := paras
	scope, paras = SelectTemplateScope(paras)
	if scope.SelectedSample != "" {
		profile.Scope = &scope
		for index := scope.RequirementsRange[0]; index < scope.RequirementsRange[1] && index < len(allParas); index++ {
			if text := strings.TrimSpace(allParas[index].Text); text != "" {
				profile.Requirements = append(profile.Requirements, summarizeSourceText(text))
			}
		}
	}
	if numbering := parseNumberingPartWithTheme(parts.Numbering, themeFonts); numbering != nil {
		profile.Numbering = numbering
	}
	styleSet := emptyStyleDefinitionSet()
	styleDefinitions := styleSet.Resolved
	tocStyleKeys := map[string]string{}
	if stylesText != "" {
		styleSet = parseStyleDefinitions(stylesText)
		styleDefinitions = styleSet.Resolved
		tocStyleKeys = buildTOCStyleKeys(styleSet.NameToID)
	}
	profile.RulePack = extractLocalRulePack(paras)
	extractHeaderFooterVariantsFromParts(profile, parts, documentXML, themeFonts)

	// Build numbering-derived heading patterns (OOXML precise, no AI guessing)
	numberingPatterns := map[string]*regexp.Regexp{}
	if profile.Numbering != nil {
		numberingPatterns = profile.Numbering.BuildHeadingPatterns()
	}

	instructionParagraphs, bodyRequirements := templateInstructionParagraphs(paras)
	styleSamples := map[string][]StyleRule{}
	bodyStarted := false
	inTOC := false
	coverTitlePending := false
	coverDateSamples := []StyleRule{}
	sampleRegionStarted := false
	abstractBodyKey := ""
	coverRegionStart := -1
	coverRegionEnd := -1
	for i, candidate := range paras {
		candidateText := strings.TrimSpace(candidate.Text)
		if coverRegionStart < 0 && (candidateText == "毕业设计（论文）" || candidateText == "毕业论文/设计" || candidateText == "毕业设计/论文") {
			coverRegionStart = i
		}
		if coverRegionStart >= 0 && coverRegionEnd < 0 && strings.Contains(candidateText, "原创性声明") {
			coverRegionEnd = i
		}
		if normalizeLabel(candidate.Text) == "摘要" || strings.EqualFold(strings.TrimSpace(candidate.Text), "abstract") {
			break
		}
	}
	for index, para := range paras {
		// A requirements section can contain genuine caption examples. Keep
		// those in their own role; prose instructions never enter body samples.
		if instructionParagraphs[index] && classifyCaptionParagraph(para.Text, para.XML, styleDefinitions) == "" {
			profile.ExcludedSamples = append(profile.ExcludedSamples, ExcludedStyleSample{ParagraphIndex: index + 1, Text: para.Text, Reason: "template_instruction_section"})
			profile.Requirements = append(profile.Requirements, para.Text)
			continue
		}
		normalized := normalizeLabel(para.Text)
		lower := strings.ToLower(strings.TrimSpace(para.Text))
		if !sampleRegionStarted && (normalized == "摘要" || lower == "abstract") {
			sampleRegionStarted = true
			coverSamples := styleSamples["cover_title"]
			coverFieldSamples := styleSamples["cover"]
			styleSamples = map[string][]StyleRule{}
			if len(coverSamples) > 0 {
				styleSamples["cover_title"] = coverSamples
			}
			if len(coverFieldSamples) > 0 {
				styleSamples["cover"] = coverFieldSamples
			}
			if len(coverDateSamples) > 0 {
				styleSamples["cover_date"] = coverDateSamples
			}
			profile.Sections = map[string]SectionRule{}
			bodyStarted, inTOC, coverTitlePending = false, false, false
		}
		key := classifyParagraphNumberingAware(para.Text, numberingPatterns)
		if !sampleRegionStarted && index == coverRegionStart {
			// The first paragraph of the detected cover block is the template's
			// large cover banner (for example "本科毕业论文（设计）", usually
			// 36pt). It must not be merged into cover_title, or the thesis
			// title rule inherits the banner's tiny-print size. Keep it on a
			// dedicated key that no role rule consumes.
			key = "cover_heading"
		}
		// A numbered heading must never fall through to the body sample pool.
		// This is important for templates that format headings as Normal and omit
		// w:outlineLvl; their direct formatting is still the heading evidence.
		if key == "" {
			if level := numberedHeadingLevel(strings.TrimSpace(para.Text)); level > 0 {
				key = fmt.Sprintf("heading_%d", level)
			}
		}
		if key == "" {
			key = classifyCaptionParagraph(para.Text, para.XML, styleDefinitions)
		}
		if (key == "body_start" || strings.HasPrefix(key, "heading_")) &&
			!hasSemanticOutlineLevel(para.XML) && isBodyStyleCandidate(para) &&
			numberedHeadingLevel(strings.TrimSpace(para.Text)) == 0 {
			key = ""
		}
		if abstractBodyKey != "" && key == "" && isBodyStyleCandidate(para) {
			key = abstractBodyKey
		}
		if normalized == "题目" {
			coverTitlePending = true
		} else if coverTitlePending && key == "" && len([]rune(strings.TrimSpace(para.Text))) >= 4 {
			key = "cover_title"
			coverTitlePending = false
		}
		if key == "toc_title" {
			inTOC = true
		} else if inTOC {
			if tocKey, ok := tocStyleKey(para.XML, tocStyleKeys); ok {
				key = tocKey
			} else if hasTOCLeader(para.Text) {
				key = "toc_entry"
			} else if key == "body_start" || strings.HasPrefix(key, "heading_") {
				inTOC = false
			}
		}
		if key == "body_start" {
			bodyStarted = true
		} else if key == "references_title" || key == "acknowledgements_title" {
			bodyStarted = false
		} else if key == "" && bodyStarted && isBodyStyleCandidate(para) {
			key = "body"
		}
		if strings.HasPrefix(key, "heading_") && hasTOCLeader(para.Text) {
			continue
		}
		// Cover metadata paragraphs (college, major, class, date, etc.) usually
		// have no semantic label that the classifier can recognize.  Preserve
		// their direct OOXML formatting as a separate consensus rule before the
		// first abstract instead of letting the later body fallback consume it.
		if !sampleRegionStarted && index > coverRegionStart && (coverRegionEnd < 0 || index < coverRegionEnd) && key == "" && strings.TrimSpace(para.Text) != "" {
			coverKey := "cover"
			if isTemplateCoverDateText(para.Text) {
				coverKey = "cover_date"
			}
			coverStyle := extractEffectiveParagraphStyle(coverKey, para.XML, styleSet, profile.Numbering)
			coverStyle = extractRepresentativeRunStyleWithDefinitions(coverKey, para.XML, coverStyle, styleDefinitions)
			coverStyle = recordStyleSource(coverStyle, index, para)
			if coverStyle.FontSizeHalfPt != "" || coverStyle.FontEastAsia != "" || coverStyle.FontASCII != "" {
				if coverKey == "cover_date" {
					coverDateSamples = append(coverDateSamples, coverStyle)
				} else {
					styleSamples["cover"] = append(styleSamples["cover"], coverStyle)
				}
			}
		}
		if key == "" {
			continue
		}
		style := extractEffectiveParagraphStyle(key, para.XML, styleSet, profile.Numbering)
		style = extractRepresentativeRunStyleWithDefinitions(key, para.XML, style, styleDefinitions)
		style = extractLeadingLabelRunStyleWithDefinitions(key, para.XML, style, styleDefinitions)
		if key == "body_start" || key == "heading_1" {
			if profile.HeadingBlankBefore == nil {
				profile.HeadingBlankBefore = headingBlankStyle(paras, index-1, styleSet, profile.Numbering)
			}
			if profile.HeadingBlankAfter == nil {
				profile.HeadingBlankAfter = headingBlankStyle(paras, index+1, styleSet, profile.Numbering)
			}
		}
		style = resolveEffectiveFormatting(style, formattingInputForRole(key, para.XML), styleSet, profile.Numbering)
		style.Bold, style.BoldSet, style.BoldEvidence = resolveParagraphBold(formattingInputForRole(key, para.XML), styleSet)
		style.Italic, style.ItalicSet, style.ItalicEvidence = resolveParagraphEmphasis(formattingInputForRole(key, para.XML), styleSet, true)
		style = recordStyleSource(style, index, para)
		styleSamples[key] = append(styleSamples[key], style)
		if key == "abstract_cn" {
			abstractBodyKey = "abstract_body"
			if content, ok := extractTrailingContentRunStyleWithDefinitions("abstract_body", para.XML, styleDefinitions); ok {
				base := extractEffectiveParagraphStyle(content.Label, runElementPattern.ReplaceAllString(para.XML, ""), styleSet, profile.Numbering)
				content = mergeStyleRule(base, content)
				styleSamples["abstract_body"] = append(styleSamples["abstract_body"], recordStyleSource(content, index, para))
			}
		} else if key == "abstract_en" {
			abstractBodyKey = "abstract_en_body"
			if content, ok := extractTrailingContentRunStyleWithDefinitions("abstract_en_body", para.XML, styleDefinitions); ok {
				base := extractEffectiveParagraphStyle(content.Label, runElementPattern.ReplaceAllString(para.XML, ""), styleSet, profile.Numbering)
				content = mergeStyleRule(base, content)
				styleSamples["abstract_en_body"] = append(styleSamples["abstract_en_body"], recordStyleSource(content, index, para))
			}
		} else if key == "keywords_cn" {
			if content, ok := extractTrailingContentRunStyleWithDefinitions("keywords_cn_body", para.XML, styleDefinitions); ok {
				base := extractEffectiveParagraphStyle(content.Label, runElementPattern.ReplaceAllString(para.XML, ""), styleSet, profile.Numbering)
				content = mergeStyleRule(base, content)
				styleSamples["keywords_cn_body"] = append(styleSamples["keywords_cn_body"], recordStyleSource(content, index, para))
			}
			abstractBodyKey = ""
		} else if key == "keywords_en" {
			if content, ok := extractTrailingContentRunStyleWithDefinitions("keywords_en_body", para.XML, styleDefinitions); ok {
				base := extractEffectiveParagraphStyle(content.Label, runElementPattern.ReplaceAllString(para.XML, ""), styleSet, profile.Numbering)
				content = mergeStyleRule(base, content)
				styleSamples["keywords_en_body"] = append(styleSamples["keywords_en_body"], recordStyleSource(content, index, para))
			}
			abstractBodyKey = ""
		} else if key == "toc_title" {
			abstractBodyKey = ""
		}
		if key == "body_start" {
			headingStyle := style
			headingStyle.Label = "heading_1"
			styleSamples["heading_1"] = append(styleSamples["heading_1"], headingStyle)
		}
		if isSectionKey(key) {
			breakBefore, detectedFrom := detectPageBreakBefore(paras, index)
			sectionBreak, sectionType := detectSectionBreak(paras, index)
			profile.Sections[key] = SectionRule{
				Label:                 key,
				PageBreakBefore:       breakBefore,
				SectionBreak:          sectionBreak,
				SectionBreakType:      sectionType,
				BlankParagraphsBefore: blankParagraphsBefore(paras, index),
				DetectedFrom:          detectedFrom,
			}
		}
		// A template may contain several complete example papers. Mixing later
		// examples into the first one produces a synthetic style that exists in
		// none of them. The first abstract-to-acknowledgements sequence is the
		// nearest complete, internally consistent formatting example.
		if sampleRegionStarted && key == "acknowledgements_title" {
			break
		}
	}
	for key, samples := range styleSamples {
		if key == "body" {
			// A template often formats numbered headings as Normal and omits
			// w:outlineLvl.  The semantic pass can therefore leave a heading in
			// the generic body pool.  Never let that heading determine the body
			// consensus; doing so makes every student paragraph inherit the
			// heading's font/size. Keep the original pool only when filtering
			// would discard every sample, so incomplete templates still work.
			filtered := samples[:0]
			for _, sample := range samples {
				isNumberedHeading := false
				for _, source := range sample.Sources {
					if numberedHeadingLevel(strings.TrimSpace(source.Text)) > 0 {
						isNumberedHeading = true
						break
					}
				}
				if !isNumberedHeading {
					filtered = append(filtered, sample)
				}
			}
			if len(filtered) > 0 {
				samples = filtered
			}
		}
		if key == "abstract_body" {
			// The abstract heading and its body can share one paragraph in
			// OOXML. Do not let the heading's larger complex-script size become
			// the body rule; retain only actual content samples.
			filtered := samples[:0]
			for _, sample := range samples {
				isLabel := false
				for _, source := range sample.Sources {
					n := strings.TrimSpace(normalizeLabel(source.Text))
					if n == "摘要" || strings.EqualFold(n, "abstract") {
						isLabel = true
						break
					}
				}
				if !isLabel {
					filtered = append(filtered, sample)
				}
			}
			if len(filtered) > 0 {
				samples = filtered
			}
		}
		profile.Styles[key] = aggregateStyleRules(key, samples)
		if key == "body" && strings.HasPrefix(profile.Styles[key].BoldEvidence, "conflicting_body_samples:") {
			profile.Conflicts = append(profile.Conflicts, RuleConflict{Role: "body", Property: "bold", Evidence: profile.Styles[key].BoldEvidence, Sample: "both bold and nonbold uniform body samples"})
		}
	}

	applyBodyRequirements(profile, bodyRequirements)
	profile.SectionFormats = buildSectionFormatMap(profile.Styles)
	if len(profile.Conflicts) > 0 {
		return profile, fmt.Errorf("模板正文规则存在冲突，需确认后再排版：%s", profile.Conflicts[0].Evidence)
	}
	return profile, nil
}

// ResolveDocumentEffectiveStyles returns the same inherited styles used by
// template extraction, keyed by the stable body-node index used by paperast.
func ResolveDocumentEffectiveStyles(docxPath string) (map[int]StyleRule, error) {
	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		return nil, err
	}
	parts, err := extractTemplateParts(pkg)
	if err != nil {
		return nil, err
	}
	stylesXML := parts.Styles.XML
	themeFonts := extractThemeFontResolverFromXML(parts.Theme.XML, stylesXML)
	stylesXML = materializeThemeFonts(stylesXML, themeFonts)
	documentXML := materializeThemeFonts(parts.Document.XML, themeFonts)
	definitions := parseStyleDefinitions(stylesXML)
	numbering := parseNumberingPartWithTheme(parts.Numbering, themeFonts)
	result := map[int]StyleRule{}
	for index, raw := range documentBodyNodePattern.FindAllString(documentXML, -1) {
		if !strings.HasPrefix(raw, "<w:p") {
			continue
		}
		style := extractEffectiveParagraphStyle("student", raw, definitions, numbering)
		style = extractRepresentativeRunStyleWithDefinitions("body", raw, style, definitions.Resolved)
		style = resolveEffectiveFormatting(style, inlineLabelContent(raw), definitions, numbering)
		style.Bold, style.BoldSet, style.BoldEvidence = resolveParagraphBold(inlineLabelContent(raw), definitions)
		style.Italic, style.ItalicSet, style.ItalicEvidence = resolveParagraphEmphasis(inlineLabelContent(raw), definitions, true)
		style.Label = ""
		result[index] = style
	}
	return result, nil
}

// isTemplateCoverDateText identifies a cover date without prescribing any
// particular year/month format. It is only used to keep date samples out of
// the generic cover-field consensus rule.
func isTemplateCoverDateText(text string) bool {
	return strings.Contains(text, "年") && strings.Contains(text, "月")
}

func buildSectionFormatMap(styles map[string]StyleRule) SectionFormatMap {
	result := SectionFormatMap{}
	for section, keys := range map[string][]string{
		"cover_title":      {"cover_title"},
		"chapter_title":    {"heading_1", "body_start"},
		"section_title":    {"heading_2"},
		"subsection_title": {"heading_3"},
		"body_text":        {"body"},
		"abstract_body":    {"abstract_body", "abstract_cn"},
		"reference_item":   {"references"},
		"toc_entry":        {"toc_entry", "toc_entry_1"},
		"table_caption":    {"table_caption"},
		"figure_caption":   {"figure_caption"},
	} {
		for _, key := range keys {
			if style, ok := styles[key]; ok {
				result[section] = style
				break
			}
		}
	}
	return result
}

func extractLocalRulePack(paras []paragraph) RulePack {
	var rules RulePack
	for _, para := range paras {
		text := normalizeLabel(para.Text)
		if strings.Contains(text, "\u539f\u521b\u6027\u58f0\u660e") || strings.Contains(text, "\u539f\u521b\u6027\u7533\u660e") || strings.Contains(text, "\u5b66\u672f\u8bda\u4fe1\u58f0\u660e") {
			rules.RequiredSections = appendUniqueRuleSection(rules.RequiredSections, "originality_declaration")
		}
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

func appendUniqueRuleSection(sections []string, section string) []string {
	for _, existing := range sections {
		if existing == section {
			return sections
		}
	}
	return append(sections, section)
}

func AttachAISummary(ctx context.Context, profile *Profile, client ChatClient) {
	if profile == nil || client == nil {
		return
	}
	// A whole Profile is not a per-object typed evidence packet. Structured
	// compilation in templatecontract is the only permitted model boundary.
	profile.AI = &AIProfile{Enabled: false, Error: "legacy unstructured AI profile summary disabled; use structured evidence compiler"}
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
	aiWins := summary.Confidence > 0 && (profile.Confidence == 0 || summary.Confidence >= profile.Confidence)
	for key, section := range summary.Sections {
		merged, exists := profile.Sections[key]
		if merged.Label == "" {
			merged.Label = key
		}
		if !exists || aiWins {
			merged.PageBreakBefore = section.PageBreakBefore
			if detectedFrom := firstNonEmpty(section.Evidence, section.DetectedFrom); detectedFrom != "" {
				merged.DetectedFrom = detectedFrom
			}
		}
		profile.Sections[key] = merged
	}
	if profile.Styles == nil {
		profile.Styles = map[string]StyleRule{}
	}
	for key, style := range summary.Styles {
		style.Label = key
		local, exists := profile.Styles[key]
		merged := mergeStyleRule(style, local)
		if aiWins {
			merged = mergeStyleRule(local, style)
		}
		if aiWins && jsonStyleFieldPresent(data, key, "bold") {
			merged.Bold = style.Bold
			merged.BoldSet = true
		} else if exists {
			merged.Bold = local.Bold
			merged.BoldSet = local.BoldSet
		}
		if aiWins && jsonStyleFieldPresent(data, key, "italic") {
			merged.Italic = style.Italic
			merged.ItalicSet = true
		} else if exists {
			merged.Italic = local.Italic
			merged.ItalicSet = local.ItalicSet
		}
		if aiWins && !jsonStyleFieldPresent(data, key, "italic") && belongsToBodyFamily(key) {
			merged.Italic = false
			merged.ItalicSet = true
		}
		profile.Styles[key] = merged
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

func belongsToBodyFamily(key string) bool {
	switch key {
	case "body", "abstract_cn", "abstract_en", "body_cn", "body_en":
		return true
	}
	return false
}

func recordStyleSource(style StyleRule, paragraphIndex int, para paragraph) StyleRule {
	source := StyleSource{
		PropertyEvidence: style.PropertyEvidence,
		BoldEvidence:     style.BoldEvidence,
		Part:             "word/document.xml",
		ParagraphIndex:   paragraphIndex + 1,
		Text:             summarizeSourceText(para.Text),
		InheritanceChain: append([]string(nil), style.InheritanceChain...),
	}
	if style.BoldSet && style.BoldEvidence != "" {
		value := style.Bold
		source.EffectiveBold = &value
	}
	if match := paragraphStyleIDPattern.FindStringSubmatch(para.XML); len(match) == 2 {
		source.ParagraphStyleID = match[1]
	}
	if match := runStyleIDPattern.FindStringSubmatch(para.XML); len(match) == 2 {
		source.RunStyleID = match[1]
	}
	style.SampleCount = 1
	style.Confidence = 1
	style.Sources = []StyleSource{source}
	return style
}

func summarizeSourceText(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 80 {
		runes = runes[:80]
	}
	return string(runes)
}

func mergeStyleRule(base StyleRule, override StyleRule) StyleRule {
	if override.Label != "" {
		base.Label = override.Label
	}
	mergeFontSlot(&base.FontEastAsia, &base.FontEastAsiaTheme, override.FontEastAsia, override.FontEastAsiaTheme)
	mergeFontSlot(&base.FontASCII, &base.FontASCIITheme, override.FontASCII, override.FontASCIITheme)
	mergeFontSlot(&base.FontHAnsi, &base.FontHAnsiTheme, override.FontHAnsi, override.FontHAnsiTheme)
	mergeFontSlot(&base.FontCS, &base.FontCSTheme, override.FontCS, override.FontCSTheme)
	if override.FontSizeHalfPt != "" {
		base.FontSizeHalfPt = override.FontSizeHalfPt
	}
	if override.BoldSet {
		base.BoldSet = true
		base.Bold = override.Bold
	}
	if override.ItalicSet {
		base.ItalicSet = true
		base.Italic = override.Italic
	}
	if override.KeepNextSet {
		base.KeepNextSet, base.KeepNext = true, override.KeepNext
	}
	if override.KeepLinesSet {
		base.KeepLinesSet, base.KeepLines = true, override.KeepLines
	}
	if override.WidowControlSet {
		base.WidowControlSet, base.WidowControl = true, override.WidowControl
	}
	if override.Alignment != "" {
		base.Alignment = override.Alignment
	}
	if override.Line != "" {
		base.Line = override.Line
	}
	if override.LineRule != "" {
		base.LineRule = override.LineRule
	}
	if override.BeforeTwips != "" {
		base.BeforeTwips = override.BeforeTwips
	}
	if override.AfterTwips != "" {
		base.AfterTwips = override.AfterTwips
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
	if override.SampleCount > 0 {
		base.SampleCount = override.SampleCount
		base.Confidence = override.Confidence
		base.Sources = append([]StyleSource(nil), override.Sources...)
		base.InheritanceChain = append([]string(nil), override.InheritanceChain...)
	}
	return base
}

func aggregateStyleRules(label string, samples []StyleRule) StyleRule {
	if strings.HasPrefix(label, "heading_") {
		withSize := make([]StyleRule, 0, len(samples))
		for _, sample := range samples {
			if sample.FontSizeHalfPt != "" || sample.PropertyEvidence["FontSizeHalfPt"].Source == "invalid_or_missing_style" || sample.PropertyEvidence["FontSizeHalfPt"].State == "mixed" {
				withSize = append(withSize, sample)
			}
		}
		if len(withSize) > 0 {
			samples = withSize
		}
	}
	style := StyleRule{Label: label}
	style.FontEastAsia = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FontEastAsia })
	style.FontASCII = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FontASCII })
	style.FontHAnsi = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FontHAnsi })
	style.FontCS = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FontCS })
	style.FontASCIITheme = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FontASCIITheme })
	style.FontHAnsiTheme = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FontHAnsiTheme })
	style.FontEastAsiaTheme = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FontEastAsiaTheme })
	style.FontCSTheme = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FontCSTheme })
	style.FontHint = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FontHint })
	style.FontSizeHalfPt = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FontSizeHalfPt })
	style.ComplexSizeHalfPt = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.ComplexSizeHalfPt })
	style.Alignment = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.Alignment })
	// BugFix: Filter outlier line values (>1200 in auto mode) before aggregating.
	// This prevents extreme values like 4800 (20x spacing) from winning the mode.
	lineSamples := make([]StyleRule, 0, len(samples))
	lineRuleSamples := make([]StyleRule, 0, len(samples))
	for _, sample := range samples {
		useForLine := true
		if (sample.LineRule == "auto" || sample.LineRule == "") && sample.Line != "" {
			if lineVal, err := strconv.Atoi(sample.Line); err == nil && lineVal > 1200 {
				useForLine = false
			}
		}
		if useForLine {
			lineSamples = append(lineSamples, sample)
		}
		lineRuleSamples = append(lineRuleSamples, sample)
	}
	style.Line = mostCommonStyleValue(lineSamples, func(sample StyleRule) string { return sample.Line })
	style.LineRule = mostCommonStyleValue(lineRuleSamples, func(sample StyleRule) string { return sample.LineRule })
	style.BeforeTwips = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.BeforeTwips })
	style.AfterTwips = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.AfterTwips })
	style.BeforeLines = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.BeforeLines })
	style.AfterLines = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.AfterLines })
	style.FirstLineChars = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FirstLineChars })
	style.FirstLineTwips = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.FirstLineTwips })
	style.OutlineLevel = mostCommonStyleValue(samples, func(sample StyleRule) string { return sample.OutlineLevel })
	boldCount := 0
	boldSamples := 0
	for _, sample := range samples {
		if style.FontSizeHalfPt != "" && sample.FontSizeHalfPt != style.FontSizeHalfPt {
			continue
		}
		if !sample.BoldSet {
			continue
		}
		boldSamples++
		if sample.Bold {
			boldCount++
		}
	}
	style.BoldSet = boldSamples > 0
	style.Bold = boldSamples > 0 && boldCount*5 > boldSamples*3
	if label == "body" {
		style.BoldEvidence = fmt.Sprintf("resolved_samples=%d bold=%d nonbold=%d excluded=%d", boldSamples, boldCount, boldSamples-boldCount, len(samples)-boldSamples)
		for _, sample := range samples {
			if !sample.BoldSet && sample.BoldEvidence != "" && sample.BoldEvidence != "mixed_run_emphasis" && sample.BoldEvidence != "no_visible_text" {
				style.Bold, style.BoldSet = false, false
				style.BoldEvidence = "unresolved_body_sample: " + sample.BoldEvidence
				break
			}
		}

		// Disagreement between uniform body examples is a conflict, not a vote
		// authorizing a document-wide change. Mixed runs already abstain above.
		if boldCount > 0 && boldCount < boldSamples {
			style.Bold, style.BoldSet = false, false
			style.BoldEvidence = "conflicting_body_samples: " + style.BoldEvidence
		}
	}

	italicCount, italicSamples := 0, 0
	for _, sample := range samples {
		if sample.ItalicSet {
			italicSamples++
			if sample.Italic {
				italicCount++
			}
		}
	}
	style.ItalicSet = italicSamples > 0
	style.Italic = italicSamples > 0 && italicCount*5 > italicSamples*3
	if label == "body" && italicCount > 0 && italicCount < italicSamples {
		style.Italic, style.ItalicSet = false, false
		style.ItalicEvidence = "conflicting_body_samples"
		style.ReviewRequired = true
	}
	style.KeepNext, style.KeepNextSet = majorityOnOff(samples, func(sample StyleRule) (bool, bool) { return sample.KeepNext, sample.KeepNextSet })
	style.KeepLines, style.KeepLinesSet = majorityOnOff(samples, func(sample StyleRule) (bool, bool) { return sample.KeepLines, sample.KeepLinesSet })
	style.WidowControl, style.WidowControlSet = majorityOnOff(samples, func(sample StyleRule) (bool, bool) { return sample.WidowControl, sample.WidowControlSet })
	style.SampleCount = len(samples)
	for _, sample := range samples {
		style.Sources = append(style.Sources, sample.Sources...)
		style.InheritanceChain = appendUniqueStrings(style.InheritanceChain, sample.InheritanceChain...)
	}
	retainObservedCombination(&style, samples)
	style.Confidence = styleConsensusConfidence(samples, style)
	return style
}

func styleConsensusConfidence(samples []StyleRule, consensus StyleRule) float64 {
	matches, compared := 0, 0
	for _, sample := range samples {
		for _, pair := range [][2]string{
			{sample.FontEastAsia, consensus.FontEastAsia},
			{sample.FontASCII, consensus.FontASCII},
			{sample.FontHAnsi, consensus.FontHAnsi},
			{sample.FontCS, consensus.FontCS},
			{sample.FontSizeHalfPt, consensus.FontSizeHalfPt},
			{sample.Alignment, consensus.Alignment},
			{sample.Line, consensus.Line},
			{sample.FirstLineChars, consensus.FirstLineChars},
		} {
			if pair[0] == "" || pair[1] == "" {
				continue
			}
			compared++
			if pair[0] == pair[1] {
				matches++
			}
		}
		if sample.BoldSet && consensus.BoldSet {
			compared++
			if sample.Bold == consensus.Bold {
				matches++
			}
		}
		if sample.ItalicSet && consensus.ItalicSet {
			compared++
			if sample.Italic == consensus.Italic {
				matches++
			}
		}
	}
	if compared == 0 {
		return 0
	}
	return float64(matches) / float64(compared)
}

func mostCommonStyleValue(samples []StyleRule, value func(StyleRule) string) string {
	counts := map[string]int{}
	best := ""
	for _, sample := range samples {
		current := value(sample)
		if current == "" {
			continue
		}
		counts[current]++
		if counts[current] > counts[best] {
			best = current
		}
	}
	return best
}

func majorityOnOff(samples []StyleRule, value func(StyleRule) (bool, bool)) (bool, bool) {
	enabled, declared := 0, 0
	for _, sample := range samples {
		current, set := value(sample)
		if !set {
			continue
		}
		declared++
		if current {
			enabled++
		}
	}
	if declared == 0 {
		return false, false
	}
	return enabled*2 >= declared, true
}

func jsonStyleFieldPresent(data []byte, styleKey string, field string) bool {
	var raw struct {
		Styles map[string]map[string]json.RawMessage `json:"styles"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	if raw.Styles == nil || raw.Styles[styleKey] == nil {
		return false
	}
	_, ok := raw.Styles[styleKey][field]
	return ok
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
		{"abstract", "content", "abstract_body"},
		{"english_abstract", "content", "abstract_en_body"},
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
	profile.SectionFormats = buildSectionFormatMap(profile.Styles)
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
		style.FontEastAsia, style.FontEastAsiaTheme = value, ""
	}
	if value := stringRule(raw["font_name_latin"]); value != "" {
		style.FontASCII, style.FontASCIITheme = value, ""
		style.FontHAnsi, style.FontHAnsiTheme = value, ""
	}
	if points, ok := fontPoints(raw); ok {
		style.FontSizeHalfPt = strconv.Itoa(int(points * 2))
	}
	if value, exists := raw["bold"].(bool); exists {
		style.Bold = value
		style.BoldSet = true
	}
	if value, exists := raw["italic"].(bool); exists {
		style.Italic = value
		style.ItalicSet = true
	}
	if value := stringRule(raw["alignment"]); value != "" {
		if value == "justify" {
			value = "both"
		}
		style.Alignment = value
	}
	if strings.EqualFold(stringRule(raw["line_space"]), "fixed") {
		if points, ok := numberRule(raw["line_space_value"]); ok {
			style.Line = strconv.Itoa(int(points * 20))
			style.LineRule = "exact"
		}
	} else if multiple, ok := numberRule(raw["line_space"]); ok {
		style.Line = strconv.Itoa(int(multiple * 240))
		if value := stringRule(raw["line_rule"]); value != "" {
			style.LineRule = value
		}
	}
	if chars, ok := numberRule(raw["first_line_indent"]); ok {
		if strings.EqualFold(stringRule(raw["first_line_indent_unit"]), "cm") {
			chars /= 0.37
		}
		if chars >= 0 && chars <= 10 {
			style.FirstLineChars = strconv.Itoa(int(chars * 100))
		}
	}
	if twips, ok := numberRule(raw["paragraph_before_twips"]); ok && twips >= 0 {
		style.BeforeTwips = strconv.Itoa(int(twips))
	}
	if twips, ok := numberRule(raw["paragraph_after_twips"]); ok && twips >= 0 {
		style.AfterTwips = strconv.Itoa(int(twips))
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
		if value := stringRule(header["font_ascii"]); value != "" {
			profile.Header.FontAscii = value
		}
		if points, ok := fontPoints(header); ok {
			profile.Header.FontSizeHalfPt = strconv.Itoa(int(points * 2))
		}
		profile.Header.HasUnderline = boolRule(header["has_underline"], profile.Header.HasUnderline)
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
	sections := sectPrPattern.FindAllString(documentXML, -1)
	if len(sections) == 0 {
		return PageSetupRule{}
	}
	rule := PageSetupRule{}
	for index := len(sections) - 1; index >= 0; index-- {
		section := sections[index]
		if rule.PageWidthTwips == "" {
			if pgSz := pgSzPattern.FindString(section); pgSz != "" {
				values := attrs(pgSz)
				rule.PageWidthTwips = values["w:w"]
				rule.PageHeightTwips = values["w:h"]
				rule.Orientation = values["w:orient"]
			}
		}
		if rule.MarginTopTwips == "" {
			if pgMar := pgMarPattern.FindString(section); pgMar != "" {
				values := attrs(pgMar)
				rule.MarginTopTwips = values["w:top"]
				rule.MarginRightTwips = values["w:right"]
				rule.MarginBottomTwips = values["w:bottom"]
				rule.MarginLeftTwips = values["w:left"]
				rule.HeaderMarginTwips = values["w:header"]
				rule.FooterMarginTwips = values["w:footer"]
			}
		}
		if rule.PageWidthTwips != "" && rule.MarginTopTwips != "" {
			break
		}
	}
	return rule
}

func collectParagraphs(documentXML string) []paragraph {
	tableDepth := 0
	decoder := xml.NewDecoder(strings.NewReader(documentXML))
	paras := make([]paragraph, 0, 64)
	depth, textBoxDepth := 0, 0
	paragraphStart, paragraphDepth := -1, -1
	for {
		start := int(decoder.InputOffset())
		token, err := decoder.Token()
		if err == io.EOF {
			return paras
		}
		if err != nil {
			break
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			if typed.Name.Local == "tbl" {
				tableDepth++
			}
			if typed.Name.Local == "txbxContent" {
				textBoxDepth++
			}
			if typed.Name.Local == "p" && paragraphStart < 0 && textBoxDepth == 0 {
				paragraphStart, paragraphDepth = start, depth
			}
		case xml.EndElement:
			if typed.Name.Local == "tbl" {
				tableDepth--
			}
			if typed.Name.Local == "txbxContent" {
				// Text boxes often contain template annotations. Preserve their XML
				// in the document, but never treat them as paragraph style samples.
				textBoxDepth--
				depth--
				continue
			}
			if typed.Name.Local == "p" && paragraphStart >= 0 && depth == paragraphDepth {
				raw := documentXML[paragraphStart:int(decoder.InputOffset())]
				blankLineAnnotation := hasBlankLineAnnotation(raw)
				raw = textBoxContentPattern.ReplaceAllString(raw, "")
				paras = append(paras, paragraph{Text: extractText(raw), XML: raw, BlankLineAnnotation: blankLineAnnotation, InTable: tableDepth > 0})
				paragraphStart, paragraphDepth = -1, -1
			}
			depth--
		}
	}

	// Keep the legacy fallback for malformed fixtures; valid DOCX XML always
	// follows the structural path above.
	matches := paragraphPattern.FindAllString(documentXML, -1)
	paras = make([]paragraph, 0, len(matches))
	for _, raw := range matches {
		blankLineAnnotation := hasBlankLineAnnotation(raw)
		raw = textBoxContentPattern.ReplaceAllString(raw, "")
		paras = append(paras, paragraph{Text: extractText(raw), XML: raw, BlankLineAnnotation: blankLineAnnotation})
	}
	return paras
}

// SelectTemplateScope selects the science sample when a template embeds both
// variants. Chinese numbered headings (1, 1.1, 1.1.1) are the default thesis
// shape supported by the formatter; the arts sample is retained as metadata
// but never mixed into the selected evidence.
func SelectTemplateScope(paras []paragraph) (TemplateScope, []paragraph) {
	scope := TemplateScope{SelectedSample: SampleScience}
	science, arts := -1, -1
	for i, para := range paras {
		text := strings.TrimSpace(para.Text)
		if science < 0 && strings.Contains(text, "7-1") && strings.Contains(text, "理工类") {
			science = i
		}
		if arts < 0 && strings.Contains(text, "7-2") && strings.Contains(text, "文科类") {
			arts = i
		}
	}
	if science < 0 && arts < 0 {
		return TemplateScope{}, paras
	}
	if science < 0 {
		science = 0
	}
	if arts < 0 || arts <= science {
		arts = len(paras)
	}
	scope.RequirementsRange = [2]int{0, science}
	// The anchor labels the sample; it is not itself a style example.
	scope.ScienceSampleRange = [2]int{science + 1, arts}
	if arts < len(paras) {
		scope.ArtsSampleRange = [2]int{arts, len(paras)}
	}
	if science+1 >= arts {
		return scope, paras
	}
	return scope, paras[science+1 : arts]
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

func buildTOCStyleKeys(nameToID map[string]string) map[string]string {
	result := map[string]string{}
	for name, id := range nameToID {
		normalized := strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(name))
		if !strings.HasPrefix(normalized, "toc") {
			continue
		}
		level, err := strconv.Atoi(strings.TrimPrefix(normalized, "toc"))
		if err != nil || level < 1 || level > 9 {
			continue
		}
		key := "toc_entry"
		if level > 1 {
			key = fmt.Sprintf("toc_entry_%d", level)
		}
		result[id] = key
	}
	return result
}

func tocStyleKey(paragraphXML string, keys map[string]string) (string, bool) {
	match := paragraphStyleIDPattern.FindStringSubmatch(paragraphXML)
	if len(match) != 2 {
		return "", false
	}
	key, ok := keys[match[1]]
	return key, ok
}

func classifyCaptionParagraph(text, paragraphXML string, definitions map[string]StyleRule) string {
	trimmed := strings.TrimSpace(text)
	match := captionNumberPattern.FindStringIndex(trimmed)
	if match == nil || match[1] >= len(trimmed) {
		return ""
	}
	if !captionWithSpacePattern.MatchString(trimmed) {
		style := extractStyleWithDefinitions("caption", paragraphXML, definitions)
		if !strings.EqualFold(style.Alignment, "center") {
			return ""
		}
	}
	if strings.HasPrefix(trimmed, "\u8868") {
		return "table_caption"
	}
	return "figure_caption"
}

func hasSemanticOutlineLevel(paragraphXML string) bool {
	tag := outlinePattern.FindString(paragraphXML)
	if tag == "" {
		return false
	}
	value, err := strconv.Atoi(attrs(tag)["w:val"])
	return err == nil && value >= 0 && value <= 8
}

// classifyParagraphNumberingAware first tries numbering-derived patterns (precise
// OOXML extraction), then falls back to the original classifyParagraph logic.
// When numberingPatterns is empty, it behaves identically to classifyParagraph.
func classifyParagraphNumberingAware(text string, numberingPatterns map[string]*regexp.Regexp) string {
	trimmed := strings.TrimSpace(text)
	if level := numberedHeadingLevel(trimmed); level > 0 {
		if level == 1 && IsBodyStartParagraph(trimmed) {
			return "body_start"
		}
		return fmt.Sprintf("heading_%d", level)
	}
	// Phase 1: Try numbering-derived heading patterns (most precise)
	if len(numberingPatterns) > 0 {
		for _, profileKey := range []string{"heading_3", "heading_2", "heading_1"} {
			pat, ok := numberingPatterns[profileKey]
			if !ok {
				continue
			}
			if pat.MatchString(trimmed) {
				return profileKey
			}
		}
		if pat, ok := numberingPatterns["heading_4"]; ok {
			if pat.MatchString(trimmed) {
				return "heading_4"
			}
		}
	}

	// Phase 2: Fall back to original rule-based classifier
	return classifyParagraph(text)
}

// numberedHeadingLevel recognizes compact Arabic headings such as
// "1.1研究目的" as well as spaced forms such as "1.1 研究目的". It rejects
// date-like prose so a sentence beginning with "2026 年" is not a heading.
func numberedHeadingLevel(text string) int {
	match := regexp.MustCompile(`^(\d+(?:\.\d+){0,3})\s*(.*)$`).FindStringSubmatch(strings.TrimSpace(text))
	if len(match) != 3 || strings.TrimSpace(match[2]) == "" {
		return 0
	}
	parts := strings.Split(match[1], ".")
	level := len(parts)
	if level == 1 {
		value, err := strconv.Atoi(parts[0])
		if err != nil || value > 99 {
			return 0
		}
	}
	title := strings.TrimSpace(match[2])
	first := []rune(title)
	if len(first) == 0 {
		return 0
	}
	// Numeric/scientific expressions such as "8.7×10-2" can match the
	// numbering prefix regexp, but they are paragraph content, not headings.
	// Do not let them contaminate heading samples.
	if first[0] >= '0' && first[0] <= '9' || strings.ContainsRune("×+-=/", first[0]) {
		return 0
	}
	if len(first) > 80 || strings.ContainsAny(title, "。！？；;，,") ||
		strings.HasPrefix(title, "年") || strings.HasPrefix(title, "月") ||
		strings.HasPrefix(title, "日") || strings.HasPrefix(title, "天") {
		return 0
	}
	return level
}

func classifyParagraph(text string) string {
	normalized := normalizeLabel(text)
	// Templates frequently put the sample heading in a shape with helper
	// placeholders such as “空一行空一行致  谢”. Ignore those placeholders for
	// semantic classification while retaining the original XML for style
	// extraction.
	acknowledgementText := strings.ReplaceAll(normalized, "空一行", "")
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
	case acknowledgementText == "致谢":
		return "acknowledgements_title"
	case IsBodyStartParagraph(text):
		return "body_start"
	case heading3ArabicPattern.MatchString(strings.TrimSpace(text)):
		return "heading_3"
	case heading2ArabicPattern.MatchString(strings.TrimSpace(text)):
		return "heading_2"
	case heading1ArabicPattern.MatchString(strings.TrimSpace(text)) || heading1ChinesePattern.MatchString(normalized) || heading1ChineseListPattern.MatchString(normalized):
		return "heading_1"
	default:
		return ""
	}
}

func IsBodyStartParagraph(text string) bool {
	trimmed := strings.TrimSpace(text)
	normalized := normalizeLabel(text)
	return bodyStartArabicPattern.MatchString(trimmed) || bodyStartChinesePattern.MatchString(normalized) || bodyStartChineseListPattern.MatchString(normalized)
}

func isSectionKey(key string) bool {
	return key == "body_start" || key == "references_title" || key == "acknowledgements_title"
}

func detectPageBreakBefore(paras []paragraph, index int) (bool, string) {
	if index < 0 || index >= len(paras) {
		return false, ""
	}
	current := paras[index].XML
	if ooxmlpatch.ParagraphPageBreakBefore(current) || strings.Contains(current, `<w:br w:type="page"`) {
		return true, "current_paragraph"
	}
	for previousIndex, checked := index-1, 0; previousIndex >= 0 && checked < 5; previousIndex, checked = previousIndex-1, checked+1 {
		previousPara := paras[previousIndex]
		previous := previousPara.XML
		sectionType := sectionBreakType(previous)
		if strings.Contains(previous, `<w:br w:type="page"`) || sectionType == "nextPage" || sectionType == "oddPage" || sectionType == "evenPage" {
			return true, "previous_paragraph"
		}
		if strings.TrimSpace(previousPara.Text) != "" {
			break
		}
	}
	return false, "not_found"
}

func blankParagraphsBefore(paras []paragraph, index int) int {
	count := 0
	for i := index - 1; i >= 0 && strings.TrimSpace(paras[i].Text) == ""; i-- {
		count++
	}
	return count
}

func sectionBreakType(raw string) string {
	section := sectPrPattern.FindString(raw)
	if section == "" {
		return ""
	}
	typeElement := regexp.MustCompile(`<w:type\b[^>]*/>`).FindString(section)
	if value := attrs(typeElement)["w:val"]; value != "" {
		return value
	}
	return "nextPage"
}

func detectSectionBreak(paras []paragraph, index int) (bool, string) {
	if index < 0 || index >= len(paras) {
		return false, ""
	}
	if value := sectionBreakType(paras[index].XML); value != "" {
		return true, value
	}
	for i, checked := index-1, 0; i >= 0 && checked < 5; i, checked = i-1, checked+1 {
		if value := sectionBreakType(paras[i].XML); value != "" {
			return true, value
		}
		if strings.TrimSpace(paras[i].Text) != "" {
			break
		}
	}
	return false, ""
}

func extractStyle(label string, raw string) StyleRule {
	style := StyleRule{Label: label}
	style.KeepNext, style.KeepNextSet = parseOnOffElement(keepNextPattern.FindString(raw))
	style.KeepLines, style.KeepLinesSet = parseOnOffElement(keepLinesPattern.FindString(raw))
	style.WidowControl, style.WidowControlSet = parseOnOffElement(widowControlPattern.FindString(raw))
	trimmed := strings.TrimSpace(raw)
	singleRun := strings.HasPrefix(trimmed, "<w:r>") || strings.HasPrefix(trimmed, "<w:r ")
	if font := fontPattern.FindString(raw); font != "" {
		attrs := attrs(font)
		style.FontEastAsia = attrs["w:eastAsia"]
		style.FontASCII = attrs["w:ascii"]
		style.FontHAnsi = attrs["w:hAnsi"]
		style.FontCS = attrs["w:cs"]
		style.FontASCIITheme = attrs["w:asciiTheme"]
		style.FontHAnsiTheme = attrs["w:hAnsiTheme"]
		style.FontEastAsiaTheme = attrs["w:eastAsiaTheme"]
		style.FontCSTheme = attrs["w:cstheme"]
		style.FontHint = attrs["w:hint"]
	}
	if size := sizePattern.FindString(raw); size != "" {
		style.FontSizeHalfPt = attrs(size)["w:val"]
	} else if size := sizeCsPattern.FindString(raw); size != "" {
		style.FontSizeHalfPt = attrs(size)["w:val"]
	}
	if size := sizeCsPattern.FindString(raw); size != "" {
		style.ComplexSizeHalfPt = attrs(size)["w:val"]
	}
	boldScope := paragraphRunPropsPattern.FindString(raw)
	if boldScope == "" && (strings.Contains(raw, "<w:style") || singleRun) {
		boldScope = runPropertiesPattern.FindString(raw)
	}
	if hasBoldDeclaration(boldScope) {
		style.BoldSet = true
		style.Bold = enabledBold(boldScope)
	} else {
		totalRuns, declaredRuns, enabled := 0, 0, 0
		for _, run := range runElementPattern.FindAllString(raw, -1) {
			if strings.TrimSpace(extractText(run)) == "" {
				continue
			}
			totalRuns++
			runProperties := runPropertiesPattern.FindString(run)
			if hasBoldDeclaration(runProperties) {
				declaredRuns++
			}
			if enabledBold(runProperties) {
				enabled++
			}
		}
		style.BoldSet = declaredRuns > 0
		style.Bold = declaredRuns > 0 && enabled*5 > totalRuns*3
	}
	italicScope := paragraphRunPropsPattern.FindString(raw)
	if italicScope == "" && (strings.Contains(raw, "<w:style") || singleRun) {
		italicScope = runPropertiesPattern.FindString(raw)
	}
	if hasItalicDeclaration(italicScope) {
		style.ItalicSet = true
		style.Italic = enabledProperty(italicScope, "i")
	}
	if jc := jcPattern.FindString(raw); jc != "" {
		style.Alignment = attrs(jc)["w:val"]
	}
	if spacing := spacingPattern.FindString(raw); spacing != "" {
		attrs := attrs(spacing)
		style.Line = attrs["w:line"]
		style.LineRule = attrs["w:lineRule"]
		// BugFix: Validate line value for auto mode to reject outliers (e.g., 4800 = 20x line spacing).
		// Normal auto range is 240~600 (1.0x~2.5x). Exact/atLeast modes are not validated.
		if (style.LineRule == "auto" || style.LineRule == "") && style.Line != "" {
			if lineVal, err := strconv.Atoi(style.Line); err == nil {
				if lineVal > 1200 || lineVal < 60 {
					style.Line = ""
				}
			}
		}
		style.BeforeTwips = attrs["w:before"]
		style.AfterTwips = attrs["w:after"]
		style.BeforeLines = attrs["w:beforeLines"]
		style.AfterLines = attrs["w:afterLines"]
	}
	if ind := indentPattern.FindString(raw); ind != "" {
		attributes := attrs(ind)
		style.FirstLineChars = attributes["w:firstLineChars"]
		if value, err := strconv.Atoi(style.FirstLineChars); err == nil && (value < 0 || value > 1000) {
			style.FirstLineChars = ""
		}
		style.FirstLineTwips = attributes["w:firstLine"]
	}
	if outline := outlinePattern.FindString(raw); outline != "" {
		style.OutlineLevel = attrs(outline)["w:val"]
	}
	return style
}

func parseOnOffElement(element string) (bool, bool) {
	if element == "" {
		return false, false
	}
	value := strings.ToLower(strings.TrimSpace(attrs(element)["w:val"]))
	switch value {
	case "0", "false", "off", "no":
		return false, true
	default:
		return true, true
	}
}

func extractLeadingLabelRunStyle(label, paragraphXML string, base StyleRule) StyleRule {
	return extractLeadingLabelRunStyleWithDefinitions(label, paragraphXML, base, nil)
}

func extractLeadingLabelRunStyleWithDefinitions(label, paragraphXML string, base StyleRule, definitions map[string]StyleRule) StyleRule {
	switch label {
	case "abstract_cn", "keywords_cn", "abstract_en", "keywords_en":
	default:
		return base
	}
	for _, run := range runElementPattern.FindAllString(paragraphXML, -1) {
		text := strings.TrimSpace(extractText(run))
		normalized := normalizeLabel(text)
		lower := strings.ToLower(text)
		matches := label == "abstract_cn" && strings.HasPrefix(normalized, "摘要") ||
			label == "keywords_cn" && strings.HasPrefix(normalized, "关键词") ||
			label == "abstract_en" && strings.HasPrefix(lower, "abstract") ||
			label == "keywords_en" && (strings.HasPrefix(lower, "keywords") || strings.HasPrefix(lower, "key words"))
		if matches {
			return mergeExtractedStyle(base, extractEffectiveRunStyle(label, run, definitions), run)
		}
	}
	return base
}

func extractRepresentativeRunStyle(label, paragraphXML string, base StyleRule) StyleRule {
	return extractRepresentativeRunStyleWithDefinitions(label, paragraphXML, base, nil)
}

func extractRepresentativeRunStyleWithDefinitions(label, paragraphXML string, base StyleRule, definitions map[string]StyleRule) StyleRule {
	switch {
	case strings.HasPrefix(label, "heading_"),
		label == "body_start",
		label == "body",
		label == "abstract_body",
		label == "references",
		strings.HasPrefix(label, "toc_entry"),
		label == "table_caption",
		label == "figure_caption":
	default:
		return base
	}
	bestRun, bestLength := "", 0
	for _, run := range runElementPattern.FindAllString(paragraphXML, -1) {
		length := len([]rune(strings.TrimSpace(extractText(run))))
		if length > bestLength {
			bestRun, bestLength = run, length
		}
	}
	if bestRun == "" {
		return base
	}
	return mergeExtractedStyle(base, extractEffectiveRunStyle(label, bestRun, definitions), bestRun)
}

func extractTrailingContentRunStyle(label, paragraphXML string) (StyleRule, bool) {
	return extractTrailingContentRunStyleWithDefinitions(label, paragraphXML, nil)
}

func extractTrailingContentRunStyleWithDefinitions(label, paragraphXML string, definitions map[string]StyleRule) (StyleRule, bool) {
	seenLabel := false
	for _, run := range runElementPattern.FindAllString(paragraphXML, -1) {
		text := strings.TrimSpace(extractText(run))
		if text == "" {
			continue
		}
		if !seenLabel {
			seenLabel = true
			continue
		}
		if strings.Trim(text, "：:；;，,。 ") == "" {
			continue
		}
		return extractEffectiveRunStyle(label, run, definitions), true
	}
	return StyleRule{}, false
}

func extractEffectiveRunStyle(label, runXML string, definitions map[string]StyleRule) StyleRule {
	direct := extractStyle(label, runXML)
	match := runStyleIDPattern.FindStringSubmatch(runXML)
	if len(match) != 2 {
		return direct
	}
	base, ok := definitions[match[1]]
	if !ok {
		return direct
	}
	base.Label = label
	return mergeExtractedStyle(base, direct, runXML)
}

func extractStyleWithDefinitions(label, paragraphXML string, definitions map[string]StyleRule) StyleRule {
	direct := extractStyle(label, paragraphXML)
	match := paragraphStyleIDPattern.FindStringSubmatch(paragraphXML)
	if len(match) != 2 {
		return direct
	}
	base, ok := definitions[match[1]]
	if !ok {
		return direct
	}
	base.Label = label
	return mergeExtractedStyle(base, direct, paragraphXML)
}

func extractEffectiveParagraphStyle(label, paragraphXML string, definitions styleDefinitionSet, numbering *NumberingProfile) StyleRule {
	direct := extractStyle(label, paragraphXML)
	paragraphStyleID := ""
	if match := paragraphStyleIDPattern.FindStringSubmatch(paragraphXML); len(match) == 2 {
		paragraphStyleID = match[1]
	}

	base := definitions.DocDefaults
	reference := numberingReference{}
	if paragraphStyleID != "" {
		if parentID := definitions.BasedOn[paragraphStyleID]; parentID != "" {
			base = definitions.Resolved[parentID]
		}
		reference = definitions.effectiveNumberingReference(paragraphStyleID)
	}
	reference = mergeNumberingReference(reference, extractNumberingReference(paragraphXML))
	if level, ok := numbering.effectiveLevel(reference, paragraphStyleID); ok {
		base = mergeExtractedStyle(base, level.Style, paragraphXML)
	}
	if local, ok := definitions.Local[paragraphStyleID]; ok {
		base = mergeExtractedStyle(base, local, paragraphXML)
	}
	base.Label = label
	return mergeExtractedStyle(base, direct, paragraphXML)
}

func extractStyleDefinitions(stylesXML string) (map[string]StyleRule, map[string]string) {
	definitions := parseStyleDefinitions(stylesXML)
	return definitions.Resolved, definitions.NameToID
}

func emptyStyleDefinitionSet() styleDefinitionSet {
	return styleDefinitionSet{
		Resolved:  map[string]StyleRule{},
		Local:     map[string]StyleRule{},
		BasedOn:   map[string]string{},
		Numbering: map[string]numberingReference{},
		NameToID:  map[string]string{},
	}
}

func parseStyleDefinitions(stylesXML string) styleDefinitionSet {
	definitions := emptyStyleDefinitionSet()
	definitions.Raw = map[string]string{}
	// Parse docDefaults as the ultimate style inheritance base.
	if ddMatch := docDefaultsPattern.FindStringSubmatch(stylesXML); len(ddMatch) == 2 {
		definitions.DocDefaultsXML = ddMatch[1]
		definitions.DocDefaults = extractStyle("docDefaults", ddMatch[1])
	}

	rawByID := map[string]string{}
	typeByID := map[string]string{}
	for _, element := range styleElementPattern.FindAllString(stylesXML, -1) {
		if match := styleIDPattern.FindStringSubmatch(element); len(match) == 2 {
			id := match[1]
			rawByID[id] = element
			definitions.Raw[id] = element
			if a := attrs(element[:strings.Index(element, ">")+1]); a["w:type"] == "paragraph" && (a["w:default"] == "1" || a["w:default"] == "true") {
				definitions.DefaultParagraphStyle = id
			}
			local := extractStyle(id, element)
			local.InheritanceChain = []string{id}
			definitions.Local[id] = local
			definitions.Numbering[id] = extractNumberingReference(element)
			if basedOn := basedOnPattern.FindStringSubmatch(element); len(basedOn) == 2 {
				definitions.BasedOn[id] = basedOn[1]
			}
			if typeMatch := styleTypePattern.FindStringSubmatch(element); len(typeMatch) == 2 {
				typeByID[id] = typeMatch[1]
			}
			if nmMatch := styleNamePattern.FindStringSubmatch(element); len(nmMatch) == 2 {
				definitions.NameToID[normalizeLabel(nmMatch[1])] = id
			}
		}
	}

	// Resolve basedOn first; docDefaults is applied below as the final layer.
	var resolve func(string, map[string]bool, int) StyleRule
	resolve = func(id string, seen map[string]bool, depth int) StyleRule {
		if style, ok := definitions.Resolved[id]; ok {
			return style
		}
		if depth > 64 || seen[id] {
			return StyleRule{}
		}
		seen[id] = true
		style := StyleRule{}
		if parentID := definitions.BasedOn[id]; parentID != "" {
			style = resolve(parentID, seen, depth+1)
		}
		style = mergeExtractedStyle(style, definitions.Local[id], rawByID[id])

		// Character styles are layered over the paragraph's effective style later;
		// filling their gaps here would wrongly erase paragraph-level values.
		if typeByID[id] != "character" {
			style = mergeExtractedStyle(definitions.DocDefaults, style, rawByID[id])
			if style.FontSizeHalfPt == "" && definitions.DocDefaults.FontSizeHalfPt != "" {
				style.FontSizeHalfPt = definitions.DocDefaults.FontSizeHalfPt
			}
		}

		definitions.Resolved[id] = style
		return style
	}
	for id := range rawByID {
		resolve(id, map[string]bool{}, 0)
	}
	return definitions
}

func mergeExtractedStyle(base, override StyleRule, _ string) StyleRule {
	base.Label = override.Label
	mergeFontSlot(&base.FontEastAsia, &base.FontEastAsiaTheme, override.FontEastAsia, override.FontEastAsiaTheme)
	mergeFontSlot(&base.FontASCII, &base.FontASCIITheme, override.FontASCII, override.FontASCIITheme)
	mergeFontSlot(&base.FontHAnsi, &base.FontHAnsiTheme, override.FontHAnsi, override.FontHAnsiTheme)
	mergeFontSlot(&base.FontCS, &base.FontCSTheme, override.FontCS, override.FontCSTheme)
	if override.FontHint != "" {
		base.FontHint = override.FontHint
	}
	if override.FontSizeHalfPt != "" {
		base.FontSizeHalfPt = override.FontSizeHalfPt
	}
	if override.ComplexSizeHalfPt != "" {
		base.ComplexSizeHalfPt = override.ComplexSizeHalfPt
	}
	if override.BoldSet {
		base.Bold = override.Bold
		base.BoldSet = true
	}
	if override.ItalicSet {
		base.Italic = override.Italic
		base.ItalicSet = true
	}
	if override.Alignment != "" {
		base.Alignment = override.Alignment
	}
	if override.Line != "" {
		base.Line = override.Line
	}
	if override.LineRule != "" {
		base.LineRule = override.LineRule
	}
	if override.BeforeTwips != "" {
		base.BeforeTwips = override.BeforeTwips
	}
	if override.AfterTwips != "" {
		base.AfterTwips = override.AfterTwips
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
	if override.FirstLineTwips != "" {
		base.FirstLineTwips = override.FirstLineTwips
	}
	if override.OutlineLevel != "" {
		base.OutlineLevel = override.OutlineLevel
	}
	base.InheritanceChain = appendUniqueStrings(base.InheritanceChain, override.InheritanceChain...)
	return base
}

func mergeFontSlot(baseFont, baseTheme *string, overrideFont, overrideTheme string) {
	if overrideTheme != "" {
		*baseFont, *baseTheme = overrideFont, overrideTheme
		return
	}
	if overrideFont != "" {
		*baseFont, *baseTheme = overrideFont, ""
	}
}

func appendUniqueStrings(base []string, values ...string) []string {
	for _, value := range values {
		if value == "" {
			continue
		}
		found := false
		for _, existing := range base {
			if existing == value {
				found = true
				break
			}
		}
		if !found {
			base = append(base, value)
		}
	}
	return base
}

func enabledBold(raw string) bool {
	p, ok := readBoldProperties(raw)
	return ok && p.b != nil && *p.b
}

func hasBoldDeclaration(raw string) bool {
	p, ok := readBoldProperties(raw)
	return ok && p.b != nil
}

func hasItalicDeclaration(raw string) bool {
	// Must match <w:i/> or <w:i w:...> or <w:i> (self-closing, with attributes, or with content).
	// Cannot use strings.Contains("<w:i") alone because <w:ind> (indentation) etc. also start with <w:i.
	return strings.Contains(raw, "<w:i/>") ||
		strings.Contains(raw, "<w:i ") ||
		strings.Contains(raw, "<w:i>")
}

func enabledProperty(raw string, property string) bool {
	// Find <w:property not just substring — to avoid <w:ind> matching <w:i>, etc.
	// Match only if followed by />, /, >, space, or =
	re := regexp.MustCompile(`<w:` + regexp.QuoteMeta(property) + `(?:/>|/|>|[ =])`)
	loc := re.FindStringIndex(raw)
	if loc == nil {
		return false
	}
	index := loc[0]
	end := strings.Index(raw[index:], ">")
	if end < 0 {
		return false
	}
	value := raw[index : index+end+1]
	return !strings.Contains(value, `w:val="0"`) && !strings.Contains(value, `w:val="false"`)
}

func isBodyStyleCandidate(para paragraph) bool {
	if para.InTable {
		return false
	}
	text := strings.TrimSpace(para.Text)
	if len([]rune(text)) < 15 || strings.Contains(para.XML, "<w:pict") || strings.Contains(para.XML, "<w:drawing") {
		return false
	}
	if strings.Contains(para.XML, `<w:jc w:val="center"`) || strings.Contains(para.XML, `<w:jc w:val="right"`) {
		return false
	}
	return strings.ContainsAny(text, "。！？.!?；;：:")
}

func hasTOCLeader(text string) bool {
	return strings.Count(text, "．")+strings.Count(text, "…") >= 3
}

func extractHeaderFooter(pkg *ooxmlpkg.DocxPackage, header bool, themeFonts themeFontResolver) HeaderFooterRule {
	if header {
		return extractHeaderFooterParts(extractHeaderParts(pkg), themeFonts)
	}
	return extractHeaderFooterParts(extractFooterParts(pkg), themeFonts)
}

func extractHeaderFooterParts(parts []templatePart, themeFonts themeFontResolver) HeaderFooterRule {
	best := HeaderFooterRule{}
	bestScore := -1
	for _, part := range parts {
		rule := extractHeaderFooterRule(materializeThemeFonts(part.XML, themeFonts))
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

func extractHeaderFooterVariants(profile *Profile, pkg *ooxmlpkg.DocxPackage, documentXML string, themeFonts themeFontResolver) {
	if pkg == nil {
		return
	}
	parts := templateParts{
		DocumentRelationships: readOptionalPart(pkg, "word/_rels/document.xml.rels"),
		Headers:               extractHeaderParts(pkg),
		Footers:               extractFooterParts(pkg),
	}
	extractHeaderFooterVariantsFromParts(profile, parts, documentXML, themeFonts)
}

func extractHeaderFooterVariantsFromParts(profile *Profile, parts templateParts, documentXML string, themeFonts themeFontResolver) {
	if profile == nil || parts.DocumentRelationships.Name == "" {
		return
	}
	targets := map[string]string{}
	for _, relationship := range relationshipPattern.FindAllString(parts.DocumentRelationships.XML, -1) {
		values := attrs(relationship)
		if values["Id"] != "" && values["Target"] != "" {
			targets[values["Id"]] = pathpkg.Clean(pathpkg.Join("word", values["Target"]))
		}
	}
	contents := map[string]string{}
	for _, part := range append(append([]templatePart{}, parts.Headers...), parts.Footers...) {
		contents[part.Name] = part.XML
	}

	for _, reference := range headerFooterReferencePattern.FindAllString(documentXML, -1) {
		values := attrs(reference)
		content, ok := contents[targets[values["r:id"]]]
		if !ok {
			continue
		}
		rule := extractHeaderFooterRule(materializeThemeFonts(content, themeFonts))
		kind := values["w:type"]
		switch {
		case strings.HasPrefix(reference, "<w:header") && kind == "first":
			profile.HeaderFirst = rule
		case strings.HasPrefix(reference, "<w:header") && kind == "even":
			profile.HeaderEven = rule
		case strings.HasPrefix(reference, "<w:header"):
			profile.Header = rule
		case kind == "first":
			profile.FooterFirst = rule
		case kind == "even":
			profile.FooterEven = rule
		default:
			profile.Footer = rule
		}
	}
	if profile.HeaderEven.Exists && (strings.TrimSpace(profile.HeaderEven.Text) != "" || profile.HeaderEven.HasPageField) {
		profile.RulePack.HeaderPolicy = "odd_even"
		profile.RulePack.OddHeaderText = profile.Header.Text
		profile.RulePack.EvenHeaderText = profile.HeaderEven.Text
	}
}

func extractHeaderFooterRule(raw string) HeaderFooterRule {
	text := extractText(raw)
	rule := HeaderFooterRule{
		Exists:        true,
		Text:          text,
		HasPageField:  strings.Contains(raw, " PAGE "),
		HasNumPages:   strings.Contains(raw, " NUMPAGES ") || hasChineseTotalPageText(text),
		HasDoubleLine: strings.Contains(raw, `w:val="double"`),
		HasUnderline:  strings.Contains(raw, "<w:u ") || strings.Contains(raw, "<w:u/>"),
	}
	if font := fontPattern.FindString(raw); font != "" {
		fontAttrs := attrs(font)
		rule.FontEastAsia = fontAttrs["w:eastAsia"]
		rule.FontAscii = fontAttrs["w:ascii"]
	}
	if size := sizePattern.FindString(raw); size != "" {
		rule.FontSizeHalfPt = attrs(size)["w:val"]
	}

	return rule
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

// isChineseFont returns true if the font name is a commonly used Chinese font
// where the ASCII slot is often incorrectly set to the same Chinese font name
// instead of a Latin font like Times New Roman.
func isChineseFont(name string) bool {
	chineseFonts := []string{
		"宋体", "黑体", "楷体", "仿宋", "微软雅黑",
		"华文宋体", "华文黑体", "华文楷体", "华文仿宋",
		"方正书宋", "方正黑体", "方正楷体", "方正仿宋",
		"思源宋体", "思源黑体", "标宋", "报宋",
	}
	for _, f := range chineseFonts {
		if name == f {
			return true
		}
	}
	return false
}
