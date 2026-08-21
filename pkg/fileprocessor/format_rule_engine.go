package fileprocessor

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
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
	Profile       *templateprofile.Profile
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
	if processor != nil {
		processor.templateHeaderText = ""
		processor.templateProfile = nil
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
		engine.Profile = profile
		if processor != nil {
			processor.templateHeaderText = profile.Header.Text
			processor.templateProfile = profile
		}
		// 节点1：模板解析 — 打印 Profile 中所有格式信息
		DiagPrintf("====== 节点1: 模板解析 (templateprofile.Extract) =====")
		DiagPrintf("template_path=%s", templatePath)
		DiagPrintf("--- 页面设置 ---")
		DiagPrintf("PageWidthTwips=%s PageHeightTwips=%s", profile.PageSetup.PageWidthTwips, profile.PageSetup.PageHeightTwips)
		DiagPrintf("MarginTop=%s MarginRight=%s MarginBottom=%s MarginLeft=%s",
			profile.PageSetup.MarginTopTwips, profile.PageSetup.MarginRightTwips,
			profile.PageSetup.MarginBottomTwips, profile.PageSetup.MarginLeftTwips)
		DiagPrintf("HeaderMargin=%s FooterMargin=%s Orientation=%s",
			profile.PageSetup.HeaderMarginTwips, profile.PageSetup.FooterMarginTwips, profile.PageSetup.Orientation)
		DiagPrintf("--- 页眉 ---")
		DiagPrintf("Exists=%v FontEastAsia=%s FontAscii=%s FontSizeHalfPt=%s HasDoubleLine=%v HasUnderline=%v Text=%q",
			profile.Header.Exists, profile.Header.FontEastAsia, profile.Header.FontAscii,
			profile.Header.FontSizeHalfPt, profile.Header.HasDoubleLine, profile.Header.HasUnderline, profile.Header.Text)
		DiagPrintf("--- 页脚 ---")
		DiagPrintf("Exists=%v FontEastAsia=%s FontAscii=%s FontSizeHalfPt=%s HasPageField=%v HasNumPages=%v Text=%q",
			profile.Footer.Exists, profile.Footer.FontEastAsia, profile.Footer.FontAscii,
			profile.Footer.FontSizeHalfPt, profile.Footer.HasPageField, profile.Footer.HasNumPages, profile.Footer.Text)
		DiagPrintf("--- 样式画像 (Styles) ---")
		for styleKey, style := range profile.Styles {
			DiagPrintf("Styles[%s]: font_east_asia=%s font_ascii=%s font_size_half_pt=%s bold=%v alignment=%s line=%s line_rule=%s before_twips=%s after_twips=%s first_line_chars=%s first_line_twips=%s",
				styleKey, style.FontEastAsia, style.FontASCII, style.FontSizeHalfPt,
				style.Bold, style.Alignment, style.Line, style.LineRule,
				style.BeforeTwips, style.AfterTwips, style.FirstLineChars, style.FirstLineTwips)
		}
		if header, ok := headerFooterFormatSpec(profile.Header); ok {
			engine.compiled["header"] = header
		}
		if footer, ok := headerFooterFormatSpec(profile.Footer); ok {
			engine.compiled["footer"] = footer
		}
		// 将模板画像中的全部语义样式注入统一规则引擎。
		// 硬编码默认值只在模板没有对应属性时兜底，不能反向覆盖模板。
		for key, style := range profile.Styles {
			if spec, ok := styleRuleToFormatSpec(style); ok {
				engine.compiled[key] = spec
			}
		}
	}
	if templateDoc, openErr := document.Open(templatePath); openErr == nil {
		strictSpecs := extractStrictTemplateSpecs(templateDoc, processor)
		for key, spec := range strictSpecs {
			if _, exists := engine.compiled[key]; !exists {
				engine.compiled[key] = spec
			}
		}
		for source, targets := range map[string][]string{
			V2ThesisTitle:      {"cover_title"},
			V2TOCTitle:         {"toc_title"},
			V2TOC:              {"toc_entry"},
			V2Acknowledgements: {"acknowledgements", "notes"},
			V2FigureCaption:    {"caption"},
			V2TableCaption:     {"caption"},
		} {
			if spec, ok := strictSpecs[source]; ok {
				for _, target := range targets {
					if _, exists := engine.compiled[target]; !exists {
						engine.compiled[target] = spec
					}
				}
			}
		}
		for key, spec := range extractInstructionTemplateSpecs(templateDoc, processor) {
			if _, exists := engine.compiled[key]; !exists {
				engine.compiled[key] = spec
			}
		}
		templateDoc.Close()
	}
	if len(engine.compiled) == 0 && len(engine.namedStyles) == 0 {
		return nil, fmt.Errorf("template contains no usable paragraph rules")
	}

	// 节点2：规则编译 — 打印编译后的所有 ParagraphFormatSpec
	DiagPrintf("====== 节点2: 规则编译 (NewFormatRuleEngine) =====")
	DiagPrintf("compiled count=%d namedStyles count=%d defaults count=%d",
		len(engine.compiled), len(engine.namedStyles), len(engine.defaults))
	DiagPrintf("--- compiled (模板采样 + templateprofile 注入) ---")
	for key, spec := range engine.compiled {
		DiagPrintf("compiled[%s]: %s", key, formatSpecCompact(spec))
	}
	DiagPrintf("--- namedStyles (模板 Named Style 提取) ---")
	for key, spec := range engine.namedStyles {
		DiagPrintf("namedStyles[%s]: %s", key, formatSpecCompact(spec))
	}
	DiagPrintf("--- defaults (硬编码兜底) ---")
	for key, spec := range engine.defaults {
		DiagPrintf("defaults[%s]: %s", key, formatSpecCompact(spec))
	}
	engine.dumpAllRules()
	return engine, nil
}

func overlayTemplateFormatSpec(base, explicit ParagraphFormatSpec) ParagraphFormatSpec {
	if explicit.FontEastAsia != "" {
		base.FontEastAsia = explicit.FontEastAsia
	}
	if explicit.FontAscii != "" {
		base.FontAscii = explicit.FontAscii
	}
	if explicit.FontSizeHalfPt > 0 {
		base.FontSizeHalfPt = explicit.FontSizeHalfPt
	}
	if explicit.FontSizeCSHalfPt > 0 {
		base.FontSizeCSHalfPt = explicit.FontSizeCSHalfPt
	}
	if !explicit.IsEmpty() {
		base.Bold = explicit.Bold
		base.Italic = explicit.Italic
	}
	if explicit.AlignmentSet {
		base.AlignmentSet = true
		base.Alignment = explicit.Alignment
	}
	if explicit.LineSpacingVal > 0 {
		base.LineSpacingVal = explicit.LineSpacingVal
		base.LineSpacingRule = explicit.LineSpacingRule
	}
	if explicit.SpaceBefore > 0 {
		base.SpaceBefore = explicit.SpaceBefore
	}
	if explicit.SpaceAfter > 0 {
		base.SpaceAfter = explicit.SpaceAfter
	}
	if explicit.FirstLineIndent > 0 {
		base.FirstLineIndent = explicit.FirstLineIndent
	}
	if explicit.IndentLeft > 0 {
		base.IndentLeft = explicit.IndentLeft
	}
	if explicit.IndentRight > 0 {
		base.IndentRight = explicit.IndentRight
	}
	if explicit.ColorHex != "" {
		base.ColorHex = explicit.ColorHex
	}
	if explicit.OutlineLevel > 0 {
		base.OutlineLevel = explicit.OutlineLevel
	}
	base.PageBreak = base.PageBreak || explicit.PageBreak
	base.KeepWithNext = base.KeepWithNext || explicit.KeepWithNext
	base.KeepLines = base.KeepLines || explicit.KeepLines
	base.Underline = base.Underline || explicit.Underline
	return base
}

func (e *FormatRuleEngine) GetRule(paragraphType string) (ParagraphFormatSpec, bool) {
	spec, found := ParagraphFormatSpec{}, false
	if compiled, ok := e.compiled[paragraphType]; ok {
		spec = compiled
		found = true
	} else if named, ok := e.namedStyles[paragraphType]; ok {
		spec = named
		found = true
	}
	if !found {
		if fallback, ok := e.defaults[paragraphType]; ok {
			spec = fallback
			found = true
		}
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
	eastAsia, ascii := style.FontEastAsia, style.FontASCII
	if ascii != "" && (isChineseFont(ascii) || containsChineseChar(ascii)) {
		if eastAsia == "" {
			eastAsia = ascii
		}
		// A Chinese family stored in w:ascii is not reliable evidence for the
		// Western font slot. Keep it as East Asian evidence only.
		ascii = ""
	}
	spec := ParagraphFormatSpec{
		FontEastAsia: eastAsia,
		FontAscii:    ascii,
		Bold:         style.Bold,
		BoldSet:      style.BoldSet,
		Italic:       style.Italic,
		SampleCount:  1,
	}
	if v, err := strconv.ParseUint(style.FontSizeHalfPt, 10, 64); err == nil {
		spec.FontSizeHalfPt = v
	} else {
		return spec, false
	}
	if v, err := strconv.ParseUint(style.ComplexSizeHalfPt, 10, 64); err == nil {
		spec.FontSizeCSHalfPt = v
	}
	if alignment, valid := parseAlignment(style.Alignment); valid {
		spec.AlignmentSet = true
		spec.Alignment = alignment
	}
	if v, err := strconv.ParseInt(style.Line, 10, 64); err == nil && v > 0 {
		spec.LineSpacingVal = v
		switch strings.ToLower(strings.TrimSpace(style.LineRule)) {
		case "exact":
			spec.LineSpacingRule = wml.ST_LineSpacingRuleExact
		case "atleast", "at_least", "at-least":
			spec.LineSpacingRule = wml.ST_LineSpacingRuleAtLeast
		default:
			spec.LineSpacingRule = wml.ST_LineSpacingRuleAuto
		}
	}
	if v, err := strconv.ParseUint(style.BeforeTwips, 10, 64); err == nil {
		spec.SpaceBefore = v
	}
	if v, err := strconv.ParseUint(style.AfterTwips, 10, 64); err == nil {
		spec.SpaceAfter = v
	}
	// Centered and right-aligned samples often retain irrelevant indentation
	// metadata. Applying that metadata to titles moves them off-center.
	if !spec.AlignmentSet || (spec.Alignment != wml.ST_JcCenter && spec.Alignment != wml.ST_JcRight) {
		if v, err := strconv.ParseUint(style.FirstLineTwips, 10, 64); err == nil {
			// More than four glyph widths is not a plausible first-line indent;
			// it is usually stale positioning metadata from a textbox/title.
			if spec.FontSizeHalfPt == 0 || v <= spec.FontSizeHalfPt*40 {
				spec.FirstLineIndent = v
			}
		} else if v, err := strconv.ParseUint(style.FirstLineChars, 10, 64); err == nil {
			spec.FirstLineIndent = v
		}
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

// formatSpecCompact 紧凑格式化 ParagraphFormatSpec 用于诊断日志
func formatSpecCompact(spec ParagraphFormatSpec) string {
	parts := []string{}
	if spec.FontEastAsia != "" {
		parts = append(parts, fmt.Sprintf("East=%s", spec.FontEastAsia))
	}
	if spec.FontAscii != "" {
		parts = append(parts, fmt.Sprintf("Ascii=%s", spec.FontAscii))
	}
	if spec.FontSizeHalfPt > 0 {
		parts = append(parts, fmt.Sprintf("sz=%.1fpt", spec.FontSizePt()))
	}
	if spec.FontSizeCSHalfPt > 0 {
		parts = append(parts, fmt.Sprintf("cs=%.1fpt", float64(spec.FontSizeCSHalfPt)/2.0))
	}
	if spec.Bold {
		parts = append(parts, "Bold=true")
	}
	if spec.Italic {
		parts = append(parts, "Italic=true")
	}
	if spec.AlignmentSet {
		parts = append(parts, fmt.Sprintf("Align=%s", spec.Alignment.String()))
	}
	if spec.LineSpacingVal > 0 {
		rule := "auto"
		if spec.LineSpacingRule == wml.ST_LineSpacingRuleExact {
			rule = "exact"
		}
		parts = append(parts, fmt.Sprintf("Line=%d(%s)", spec.LineSpacingVal, rule))
	}
	if spec.SpaceBefore > 0 {
		parts = append(parts, fmt.Sprintf("Before=%d", spec.SpaceBefore))
	}
	if spec.SpaceAfter > 0 {
		parts = append(parts, fmt.Sprintf("After=%d", spec.SpaceAfter))
	}
	if spec.FirstLineIndent > 0 {
		parts = append(parts, fmt.Sprintf("FirstLine=%d", spec.FirstLineIndent))
	}
	if spec.OutlineLevel > 0 {
		parts = append(parts, fmt.Sprintf("Level=%d", spec.OutlineLevel))
	}
	if spec.Underline {
		parts = append(parts, "Underline=true")
	}
	if spec.PageBreak {
		parts = append(parts, "PageBreak=true")
	}
	return strings.Join(parts, " ")
}

// dumpAllRules 打印 GetRule 融合后的最终规则（四级优先级合并结果）
func (e *FormatRuleEngine) dumpAllRules() {
	DiagPrintf("--- 最终融合规则 (核心类型 defaults→compiled→namedStyles→overrides) ---")
	for key, spec := range e.Rules() {
		DiagPrintf("Rule[%s]: %s", key, formatSpecCompact(spec))
	}
}
