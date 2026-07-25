package fileprocessor

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
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

	if sampled, sampledErr := NewTemplateFormatLoader(processor).LoadSampledFromFile(templatePath); sampledErr == nil {
		engine.compiled = sampled
	}
	if named, namedErr := NewTemplateStyleExtractor().ExtractFromTemplate(templatePath); namedErr == nil {
		engine.namedStyles = named
	}
	// 🔒 LOCKED: 标题格式全部从模板提取 — templateprofile.Extract() 优先于硬编码
	profile, profileErr := templateprofile.Extract(templatePath)
	if profileErr == nil {
		if header, ok := headerFooterFormatSpec(profile.Header); ok {
			engine.compiled["header"] = header
		}
		if footer, ok := headerFooterFormatSpec(profile.Footer); ok {
			engine.compiled["footer"] = footer
		}
		// 🔒 LOCKED: 模板 Styles → compiled heading specs (hardcode fallback only when template missing)
		for _, level := range []string{"heading_1", "heading_2", "heading_3"} {
			if style, ok := profile.Styles[level]; ok && style.FontEastAsia != "" {
				if spec, ok := styleRuleToFormatSpec(style); ok {
					if existing, exists := engine.compiled[level]; exists {
						engine.compiled[level] = mergeFormatSpec(existing, spec)
					} else {
						engine.compiled[level] = spec
					}
				}
			}
		}
		// 🔒 LOCKED: 正文段落 — 模板 Styles["body"] 注入 compiled，优先于硬编码
		if bodyStyle, ok := profile.Styles["body"]; ok && bodyStyle.FontEastAsia != "" {
			if bodySpec, ok := styleRuleToFormatSpec(bodyStyle); ok {
				if existing, exists := engine.compiled["body"]; exists {
					engine.compiled["body"] = mergeFormatSpec(existing, bodySpec)
				} else {
					engine.compiled["body"] = bodySpec
				}
			}
		}
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
	if paragraphType == "header" || paragraphType == "footer" {
		merged := map[string]interface{}{}
		if direct, ok := e.userOverrides[paragraphType].(map[string]interface{}); ok {
			for key, value := range direct {
				merged[key] = value
			}
		}
		if pageSetup, ok := e.userOverrides["page_setup"].(map[string]interface{}); ok {
			if rule, ok := pageSetup[paragraphType].(map[string]interface{}); ok {
				for key, value := range rule {
					merged[key] = value
				}
			}
			if paragraphType == "footer" {
				if rule, ok := pageSetup["page_number"].(map[string]interface{}); ok {
					for key, value := range rule {
						merged[key] = value
					}
				}
			}
		}
		if paragraphType == "footer" {
			if rule, ok := e.userOverrides["page_number"].(map[string]interface{}); ok {
				for key, value := range rule {
					merged[key] = value
				}
			}
		}
		if len(merged) > 0 {
			return merged
		}
	}
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

func headerFooterFormatSpec(rule templateprofile.HeaderFooterRule) (ParagraphFormatSpec, bool) {
	if !rule.Exists {
		return ParagraphFormatSpec{}, false
	}
	spec := ParagraphFormatSpec{
		FontEastAsia: rule.FontEastAsia,
		Underline:    rule.HasUnderline,
		SampleCount:  1,
	}
	if rule.FontAscii != "" {
		spec.FontAscii = rule.FontAscii
	}
	if value, err := strconv.ParseUint(strings.TrimSpace(rule.FontSizeHalfPt), 10, 64); err == nil {
		spec.FontSizeHalfPt = value
		spec.FontSizeCSHalfPt = value
	}
	return spec, !spec.IsEmpty()
}

// 🔒 LOCKED: 标题格式全部从模板提取 — styleRuleToFormatSpec 将 templateprofile.StyleRule → ParagraphFormatSpec
func styleRuleToFormatSpec(style templateprofile.StyleRule) (ParagraphFormatSpec, bool) {
	spec := ParagraphFormatSpec{
		FontEastAsia: style.FontEastAsia,
		FontAscii:    style.FontASCII,
		Bold:         style.Bold,
		SampleCount:  1,
	}
	if v, err := strconv.ParseUint(style.FontSizeHalfPt, 10, 64); err == nil {
		spec.FontSizeHalfPt = v
		spec.FontSizeCSHalfPt = v
	} else {
		return spec, false
	}
	if alignment, valid := parseAlignment(style.Alignment); valid {
		spec.AlignmentSet = true
		spec.Alignment = alignment
	}
	if v, err := strconv.ParseInt(style.Line, 10, 64); err == nil && v > 0 {
		spec.LineSpacingVal = v
		spec.LineSpacingRule = wml.ST_LineSpacingRuleAuto
	}
	if v, err := strconv.ParseUint(style.BeforeTwips, 10, 64); err == nil && v > 0 {
		spec.SpaceBefore = v
	}
	if v, err := strconv.ParseUint(style.AfterTwips, 10, 64); err == nil && v > 0 {
		spec.SpaceAfter = v
	}
	return spec, !spec.IsEmpty()
}

// defaultParagraphFormatSpecs — 硬编码兜底，仅在模板未提供时使用。
// 🔒 LOCKED: heading_1/2/3 硬编码仅做 fallback；模板有值时被 templateprofile.Extract() → compiled 覆盖。
func defaultParagraphFormatSpecs() map[string]ParagraphFormatSpec {
	return map[string]ParagraphFormatSpec{
		// 🔒 LOCKED: cover_title - 封面主标题（"本科毕业论文/设计"）
		"cover_title": {
			FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 36,
			Bold: true, AlignmentSet: true, Alignment: wml.ST_JcCenter,
			SampleCount: 3,
		},
		// 🔒 LOCKED: cover - 封面字段段落（学院/专业/班级/学号/姓名/指导教师/日期等）
		"cover": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 24,
			AlignmentSet: true, Alignment: wml.ST_JcCenter,
			LineSpacingVal: 400, LineSpacingRule: wml.ST_LineSpacingRuleExact,
			SampleCount: 3,
		},
		"title": {
			FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 36,
			Bold: true, AlignmentSet: true, Alignment: wml.ST_JcCenter,
			SampleCount: 3,
		},
		// 🔒 LOCKED: 正文段落 fallback — 模板有值时被 templateprofile.Extract() → compiled 覆盖
		"body": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 24,
			AlignmentSet: true, Alignment: wml.ST_JcBoth, LineSpacingVal: 400,
			LineSpacingRule: wml.ST_LineSpacingRuleExact, FirstLineIndent: 480, SampleCount: 3,
		},
		// 🔒 LOCKED: 标题 fallback — 仅在 templateprofile.Extract() 未提取到标题格式时使用
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
		// 🔒 LOCKED: 四级标题 — 四号宋体，1.5倍行距(仅兜底)
		"heading_4": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 28,
			LineSpacingVal: 360, LineSpacingRule: wml.ST_LineSpacingRuleAuto,
			SampleCount: 3,
		},
		// 🔒 LOCKED: 参考文献标题 — 三号黑体居中(模板规范，仅兜底)
		"references_title": {
			FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 32,
			Bold: true, AlignmentSet: true, Alignment: wml.ST_JcCenter,
			SampleCount: 3,
		},
		// 🔒 LOCKED: 参考文献条目 — 五号宋体顶格(模板规范，仅兜底)
		"references": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 21,
			LineSpacingVal: 400, LineSpacingRule: wml.ST_LineSpacingRuleExact,
			SampleCount: 3,
		},
		// 🔒 LOCKED: 致谢标题 — 三号黑体居中(模板规范，仅兜底)
		"acknowledgements_title": {
			FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 32,
			Bold: true, AlignmentSet: true, Alignment: wml.ST_JcCenter,
			SampleCount: 3,
		},
		// 🔒 LOCKED: 致谢内容 — 小四宋体，1.5倍行距(模板规范，仅兜底)
		"acknowledgements": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 24,
			AlignmentSet: true, Alignment: wml.ST_JcBoth,
			LineSpacingVal: 400, LineSpacingRule: wml.ST_LineSpacingRuleExact,
			FirstLineIndent: 480, SampleCount: 3,
		},
		// 🔒 LOCKED: 摘要标题 — 四号黑体加粗居中(仅兜底)
		"abstract_title": {
			FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 30,
			Bold: true, AlignmentSet: true, Alignment: wml.ST_JcCenter,
			SpaceBefore: 312, SampleCount: 3,
		},
		// 🔒 LOCKED: 摘要内容 — 小四宋体，1.5倍行距，首行缩进(仅兜底)
		"abstract": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 24,
			AlignmentSet: true, Alignment: wml.ST_JcBoth,
			LineSpacingVal: 360, LineSpacingRule: wml.ST_LineSpacingRuleAuto,
			SpaceAfter: 624, FirstLineIndent: 480, SampleCount: 3,
		},
		// 🔒 LOCKED: 关键词 — 同摘要内容格式(仅兜底)
		"keywords": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 24,
			AlignmentSet: true, Alignment: wml.ST_JcBoth,
			LineSpacingVal: 360, LineSpacingRule: wml.ST_LineSpacingRuleAuto,
			SpaceAfter: 624, FirstLineIndent: 480, SampleCount: 3,
		},
		// 🔒 LOCKED: 英文摘要标题 — 四号 TNR 加粗居中(仅兜底)
		"en_abstract_title": {
			FontEastAsia: "Times New Roman", FontAscii: "Times New Roman", FontSizeHalfPt: 30,
			Bold: true, AlignmentSet: true, Alignment: wml.ST_JcCenter,
			SampleCount: 3,
		},
		// 🔒 LOCKED: 英文摘要内容 — 小四 TNR，1.5倍行距(仅兜底)
		"en_abstract": {
			FontEastAsia: "Times New Roman", FontAscii: "Times New Roman", FontSizeHalfPt: 24,
			AlignmentSet: true, Alignment: wml.ST_JcBoth,
			LineSpacingVal: 360, LineSpacingRule: wml.ST_LineSpacingRuleAuto,
			SpaceAfter: 624, FirstLineIndent: 480, SampleCount: 3,
		},
		// 🔒 LOCKED: 英文关键词 — 同英文摘要内容格式(仅兜底)
		"en_keywords": {
			FontEastAsia: "Times New Roman", FontAscii: "Times New Roman", FontSizeHalfPt: 24,
			AlignmentSet: true, Alignment: wml.ST_JcBoth,
			LineSpacingVal: 360, LineSpacingRule: wml.ST_LineSpacingRuleAuto,
			SpaceAfter: 624, FirstLineIndent: 480, SampleCount: 3,
		},
		// 🔒 LOCKED: 目录标题 — 三号黑体居中，段后1行(仅兜底)
		"toc_title": {
			FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 32,
			AlignmentSet: true, Alignment: wml.ST_JcCenter,
			SpaceAfter: 624, SampleCount: 3,
		},
		// 🔒 LOCKED: 目录条目 — 五号宋体，1.5倍行距(仅兜底)
		"toc_entry": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 21,
			AlignmentSet: true, Alignment: wml.ST_JcBoth,
			LineSpacingVal: 360, LineSpacingRule: wml.ST_LineSpacingRuleAuto,
			SampleCount: 3,
		},
		// 🔒 LOCKED: 章节标题（致谢/附录/注释）— 三号黑体加粗居中(仅兜底)
		"section_title": {
			FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 32,
			Bold: true, AlignmentSet: true, Alignment: wml.ST_JcCenter,
			SpaceBefore: 312, SpaceAfter: 312, SampleCount: 3,
		},
		// 🔒 LOCKED: 注释/致谢内容 — 五号宋体(仅兜底)
		"notes": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 21,
			AlignmentSet: true, Alignment: wml.ST_JcBoth,
			LineSpacingVal: 360, LineSpacingRule: wml.ST_LineSpacingRuleAuto,
			SampleCount: 3,
		},
		// 🔒 LOCKED: 图题/表题 — 五号宋体居中(仅兜底)
		"caption": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 21,
			AlignmentSet: true, Alignment: wml.ST_JcCenter,
			SampleCount: 3,
		},
		// 🔒 LOCKED: 页眉 — 小五宋体(仅兜底)
		"header": {
			FontEastAsia: "宋体", FontAscii: "Times New Roman", FontSizeHalfPt: 18,
			SampleCount: 3,
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
