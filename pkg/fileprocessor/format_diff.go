package fileprocessor

import "fmt"

// SpecDiff 单个属性的差异记录
type SpecDiff struct {
	Field    string `json:"field"`    // 属性名
	Expected string `json:"expected"` // 模板要求值
	Actual   string `json:"actual"`   // 文档实际值
	Severity string `json:"severity"` // "error" | "warning"
}

// ParaDiff 单段落的完整差异记录
type ParaDiff struct {
	ParaIndex int        `json:"para_index"`
	Category  string     `json:"category"`
	Text      string     `json:"text"` // 段落前30字预览
	Diffs     []SpecDiff `json:"diffs"`
}

// DocDiffReport 整篇文档的差异报告
type DocDiffReport struct {
	TotalParas   int        `json:"total_paras"`
	ErrorCount   int        `json:"error_count"`
	WarningCount int        `json:"warning_count"`
	ParaDiffs    []ParaDiff `json:"para_diffs"`
	// 闭环验收（docvalidate.validate_document），与样式 diff 互补
	ValidationOK           *bool    `json:"validation_ok,omitempty"`
	ValidationErrorCount   int      `json:"validation_error_count,omitempty"`
	ValidationWarningCount int      `json:"validation_warning_count,omitempty"`
	ValidationTemplateGaps []string `json:"validation_template_gaps,omitempty"`
	// 黄金模板标题段落格式与样式定义对齐（template_parity）
	TemplateParityOK                *bool   `json:"template_parity_ok,omitempty"`
	TemplateParityRemainingMismatch int     `json:"template_parity_remaining_mismatches,omitempty"`
	Compliance100                   *bool   `json:"compliance_100,omitempty"`
	ComplianceScore                 float64 `json:"compliance_score,omitempty"`
}

// DiffSpec 对比两个 ParagraphFormatSpec，返回差异列表
// expected = 模板标准格式，actual = 用户文档当前格式
func DiffSpec(expected, actual ParagraphFormatSpec) []SpecDiff {
	var diffs []SpecDiff

	// 中文字体
	if expected.FontEastAsia != "" && actual.FontEastAsia != expected.FontEastAsia {
		diffs = append(diffs, SpecDiff{"font_east_asia", expected.FontEastAsia, actual.FontEastAsia, "error"})
	}
	// 西文字体
	if expected.FontAscii != "" && actual.FontAscii != expected.FontAscii {
		diffs = append(diffs, SpecDiff{"font_ascii", expected.FontAscii, actual.FontAscii, "warning"})
	}

	// 字号（允许 ±1 halfPt 误差）
	if expected.FontSizeHalfPt > 0 {
		diff := int64(expected.FontSizeHalfPt) - int64(actual.FontSizeHalfPt)
		if diff > 1 || diff < -1 {
			diffs = append(diffs, SpecDiff{
				"font_size",
				humanHalfPoints(expected.FontSizeHalfPt),
				humanHalfPoints(actual.FontSizeHalfPt),
				"error",
			})
		}
	}

	// 加粗
	if expected.Bold != actual.Bold {
		exp := "不加粗"
		if expected.Bold {
			exp = "加粗"
		}
		act := "不加粗"
		if actual.Bold {
			act = "加粗"
		}
		diffs = append(diffs, SpecDiff{"bold", exp, act, "error"})
	}

	// 对齐方式
	if expected.AlignmentSet && actual.Alignment != expected.Alignment {
		diffs = append(diffs, SpecDiff{
			"alignment",
			jcToAlignString(expected.Alignment),
			jcToAlignString(actual.Alignment),
			"error",
		})
	}

	// 行距（允许 ±20 twips 误差）
	if expected.LineSpacingVal > 0 {
		diff := expected.LineSpacingVal - actual.LineSpacingVal
		if diff > 20 || diff < -20 {
			diffs = append(diffs, SpecDiff{
				"line_spacing",
				humanLineSpacing(expected.LineSpacingVal, string(expected.LineSpacingRule)),
				humanLineSpacing(actual.LineSpacingVal, string(actual.LineSpacingRule)),
				"warning",
			})
		}
	}

	// 段前距（允许 ±20 twips 误差）
	if expected.SpaceBefore > 0 {
		diff := int64(expected.SpaceBefore) - int64(actual.SpaceBefore)
		if diff > 20 || diff < -20 {
			diffs = append(diffs, SpecDiff{
				"space_before",
				humanTwipsUint(expected.SpaceBefore),
				humanTwipsUint(actual.SpaceBefore),
				"warning",
			})
		}
	}

	// 段后距（允许 ±20 twips 误差）
	if expected.SpaceAfter > 0 {
		diff := int64(expected.SpaceAfter) - int64(actual.SpaceAfter)
		if diff > 20 || diff < -20 {
			diffs = append(diffs, SpecDiff{
				"space_after",
				humanTwipsUint(expected.SpaceAfter),
				humanTwipsUint(actual.SpaceAfter),
				"warning",
			})
		}
	}

	// 首行缩进（允许 ±40 twips 误差）
	if expected.FirstLineIndent > 0 {
		diff := int64(expected.FirstLineIndent) - int64(actual.FirstLineIndent)
		if diff > 40 || diff < -40 {
			diffs = append(diffs, SpecDiff{
				"first_line_indent",
				humanTwipsUint(expected.FirstLineIndent),
				humanTwipsUint(actual.FirstLineIndent),
				"warning",
			})
		}
	}

	return diffs
}

func DiffSpecExact(expected, actual ParagraphFormatSpec) []SpecDiff {
	var diffs []SpecDiff
	add := func(field string, expectedValue, actualValue interface{}) {
		if fmt.Sprint(expectedValue) != fmt.Sprint(actualValue) {
			diffs = append(diffs, SpecDiff{
				Field: field, Expected: fmt.Sprint(expectedValue), Actual: fmt.Sprint(actualValue), Severity: "error",
			})
		}
	}
	if expected.FontEastAsia != "" {
		add("font_east_asia", expected.FontEastAsia, actual.FontEastAsia)
	}
	if expected.FontAscii != "" {
		add("font_ascii", expected.FontAscii, actual.FontAscii)
	}
	if expected.FontSizeHalfPt > 0 {
		add("font_size_half_pt", expected.FontSizeHalfPt, actual.FontSizeHalfPt)
	}
	if expected.FontSizeCSHalfPt > 0 {
		add("font_size_cs_half_pt", expected.FontSizeCSHalfPt, actual.FontSizeCSHalfPt)
	}
	// Bold is tri-state in OOXML: absent means "inherit/unspecified", while
	// w:b and w:b w:val="false" are explicit true/false. Do not report an
	// error (or trigger a repair) when the template did not define it.
	if expected.BoldSet || expected.Bold {
		add("bold", expected.Bold, actual.Bold)
	}
	add("italic", expected.Italic, actual.Italic)
	add("underline", expected.Underline, actual.Underline)
	if expected.AlignmentSet {
		add("alignment", expected.Alignment, actual.Alignment)
	}
	if expected.LineSpacingVal > 0 {
		add("line_spacing", expected.LineSpacingVal, actual.LineSpacingVal)
		add("line_spacing_rule", expected.LineSpacingRule, actual.LineSpacingRule)
	}
	if expected.SpaceBefore > 0 {
		add("space_before", expected.SpaceBefore, actual.SpaceBefore)
	}
	if expected.SpaceAfter > 0 {
		add("space_after", expected.SpaceAfter, actual.SpaceAfter)
	}
	if expected.FirstLineIndent > 0 {
		add("first_line_indent", expected.FirstLineIndent, actual.FirstLineIndent)
	}
	if expected.IndentLeft > 0 {
		add("indent_left", expected.IndentLeft, actual.IndentLeft)
	}
	if expected.IndentRight > 0 {
		add("indent_right", expected.IndentRight, actual.IndentRight)
	}
	if expected.ColorHex != "" {
		add("color", expected.ColorHex, actual.ColorHex)
	}
	if expected.OutlineLevel > 0 {
		add("outline_level", expected.OutlineLevel, actual.OutlineLevel)
	}
	add("page_break", expected.PageBreak, actual.PageBreak)
	add("keep_with_next", expected.KeepWithNext, actual.KeepWithNext)
	add("keep_lines", expected.KeepLines, actual.KeepLines)
	return diffs
}
