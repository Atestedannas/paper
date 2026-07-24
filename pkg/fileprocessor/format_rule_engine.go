package fileprocessor

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

// FormatRuleEngine 是段落格式的唯一查询入口。
// 优先级：用户覆盖 > 模板段落聚合 > Named Style > 通用兜底。
type FormatRuleEngine struct {
	processor     *EnhancedProcessor
	userOverrides map[string]interface{}
	compiled      map[string]ParagraphFormatSpec
	namedStyles   map[string]ParagraphFormatSpec
	defaults      map[string]ParagraphFormatSpec
}

func NewFormatRuleEngine(processor *EnhancedProcessor, templatePath string, userOverrides map[string]interface{}) (*FormatRuleEngine, error) {
	engine := &FormatRuleEngine{
		processor:     processor,
		userOverrides: userOverrides,
		compiled:      map[string]ParagraphFormatSpec{},
		namedStyles:   map[string]ParagraphFormatSpec{},
		defaults:      defaultParagraphFormatSpecs(),
	}
	if templatePath == "" {
		return engine, nil
	}

	engine.compiled, _ = NewTemplateFormatLoader(processor).LoadSampledFromFile(templatePath)
	if named, namedErr := NewTemplateStyleExtractor().ExtractFromTemplate(templatePath); namedErr == nil {
		engine.namedStyles = named
	}
	if len(engine.compiled) == 0 && len(engine.namedStyles) == 0 {
		return nil, fmt.Errorf("template contains no usable paragraph rules")
	}
	return engine, nil
}

func (e *FormatRuleEngine) GetRule(paragraphType string) (ParagraphFormatSpec, bool) {
	spec, found := e.defaults[paragraphType]
	if named, ok := e.namedStyles[paragraphType]; ok {
		spec = mergeFormatSpec(spec, named)
		found = true
	}
	if compiled, ok := e.compiled[paragraphType]; ok {
		spec = mergeFormatSpec(spec, compiled)
		found = true
	}
	if override := e.overrideFor(paragraphType); override != nil {
		spec = e.applyUserOverride(spec, override)
		found = true
	}
	return spec, found && !spec.IsEmpty()
}

func (e *FormatRuleEngine) Rules() map[string]ParagraphFormatSpec {
	keys := map[string]bool{}
	for key := range e.defaults {
		keys[key] = true
	}
	for key := range e.namedStyles {
		keys[key] = true
	}
	for key := range e.compiled {
		keys[key] = true
	}
	for key := range e.userOverrides {
		keys[key] = true
	}
	for level := 1; level <= 5; level++ {
		key := fmt.Sprintf("heading_%d", level)
		if e.overrideFor(key) != nil {
			keys[key] = true
		}
	}
	for _, key := range []string{
		"abstract_title", "abstract", "english_abstract_title", "english_abstract",
		"keywords_label", "references_title", "references",
		"acknowledgements_title", "acknowledgements", "appendix_title", "appendix",
	} {
		if e.overrideFor(key) != nil {
			keys[key] = true
		}
	}
	result := make(map[string]ParagraphFormatSpec, len(keys))
	for key := range keys {
		if spec, ok := e.GetRule(key); ok {
			result[key] = spec
		}
	}
	return result
}

func (e *FormatRuleEngine) overrideFor(paragraphType string) map[string]interface{} {
	if direct, ok := e.userOverrides[paragraphType].(map[string]interface{}); ok {
		return direct
	}
	if strings.HasPrefix(paragraphType, "heading_") {
		level := strings.TrimPrefix(paragraphType, "heading_")
		if headings, ok := e.userOverrides["headings"].(map[string]interface{}); ok {
			if rule, ok := headings["level"+level].(map[string]interface{}); ok {
				return rule
			}
		}
	}
	nested := map[string][2]string{
		"abstract_title":         {"abstract", "title"},
		"abstract":               {"abstract", "content"},
		"english_abstract_title": {"english_abstract", "title"},
		"english_abstract":       {"english_abstract", "content"},
		"keywords_label":         {"keywords", "label"},
		"references_title":       {"references", "title"},
		"references":             {"references", "content"},
		"acknowledgements_title": {"acknowledgements", "title"},
		"acknowledgements":       {"acknowledgements", "content"},
		"appendix_title":         {"appendix", "title"},
		"appendix":               {"appendix", "content"},
	}
	path, ok := nested[paragraphType]
	if !ok {
		return nil
	}
	parent, ok := e.userOverrides[path[0]].(map[string]interface{})
	if !ok {
		return nil
	}
	rule, _ := parent[path[1]].(map[string]interface{})
	return rule
}

func (e *FormatRuleEngine) applyUserOverride(spec ParagraphFormatSpec, rule map[string]interface{}) ParagraphFormatSpec {
	if value, ok := stringValue(rule, "font_east_asia", "font_name", "font_chinese"); ok {
		spec.FontEastAsia = value
	}
	if value, ok := stringValue(rule, "font_ascii", "font_english"); ok {
		spec.FontAscii = value
	}
	if size := e.processor.resolveActualFontSizePt(rule); size > 0 {
		spec.FontSizeHalfPt = uint64(math.Round(size * 2))
		spec.FontSizeCSHalfPt = spec.FontSizeHalfPt
	}
	if value, ok := boolValue(rule["bold"]); ok {
		spec.Bold = value
	}
	if value, ok := boolValue(rule["italic"]); ok {
		spec.Italic = value
	}
	if value, ok := boolValue(rule["underline"]); ok {
		spec.Underline = value
	}
	if value, ok := stringValue(rule, "alignment"); ok {
		if alignment, valid := parseAlignment(value); valid {
			spec.AlignmentSet = true
			spec.Alignment = alignment
		}
	}
	if value, ok := numberValue(rule["paragraph_before_twips"]); ok && value >= 0 {
		spec.SpaceBefore = uint64(math.Round(value))
	}
	if value, ok := numberValue(rule["paragraph_after_twips"]); ok && value >= 0 {
		spec.SpaceAfter = uint64(math.Round(value))
	}
	if value, ok := numberValue(rule["first_line_indent_twips"]); ok && value >= 0 {
		spec.FirstLineIndent = uint64(math.Round(value))
	} else if value, ok := numberValue(rule["first_line_indent"]); ok && value >= 0 {
		spec.FirstLineIndent = uint64(math.Round(value * 240))
	}
	if value, ok := numberValue(rule["line_space"]); ok && value > 0 {
		spec.LineSpacingVal = int64(math.Round(value * 240))
		spec.LineSpacingRule = wml.ST_LineSpacingRuleAuto
	}
	if value, ok := boolValue(rule["page_break"]); ok {
		spec.PageBreak = value
	}
	return spec
}

func defaultParagraphFormatSpecs() map[string]ParagraphFormatSpec {
	return map[string]ParagraphFormatSpec{
		"body": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 24,
			AlignmentSet: true, Alignment: wml.ST_JcBoth, LineSpacingVal: 360,
			LineSpacingRule: wml.ST_LineSpacingRuleAuto, FirstLineIndent: 480, SampleCount: 3,
		},
		"heading_1": {
			FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 32,
			Bold: true, AlignmentSet: true, Alignment: wml.ST_JcCenter, SampleCount: 3,
		},
		"heading_2": {
			FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 30,
			Bold: true, AlignmentSet: true, Alignment: wml.ST_JcLeft, SampleCount: 3,
		},
		"heading_3": {
			FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 28,
			Bold: true, AlignmentSet: true, Alignment: wml.ST_JcLeft, SampleCount: 3,
		},
	}
}

func stringValue(values map[string]interface{}, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

func boolValue(value interface{}) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return false, false
	}
}

func numberValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func parseAlignment(value string) (wml.ST_Jc, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "left", "左对齐":
		return wml.ST_JcLeft, true
	case "center", "居中":
		return wml.ST_JcCenter, true
	case "right", "右对齐":
		return wml.ST_JcRight, true
	case "justify", "both", "两端对齐":
		return wml.ST_JcBoth, true
	default:
		return wml.ST_JcLeft, false
	}
}
