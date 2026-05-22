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
	ValidationOK          *bool    `json:"validation_ok,omitempty"`
	ValidationErrorCount  int      `json:"validation_error_count,omitempty"`
	ValidationWarningCount int     `json:"validation_warning_count,omitempty"`
	ValidationTemplateGaps []string `json:"validation_template_gaps,omitempty"`
	// 黄金模板标题段落格式与样式定义对齐（template_parity）
	TemplateParityOK                *bool `json:"template_parity_ok,omitempty"`
	TemplateParityRemainingMismatch int   `json:"template_parity_remaining_mismatches,omitempty"`
	Compliance100                   *bool `json:"compliance_100,omitempty"`
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
				fmt.Sprintf("%.1fpt", float64(expected.FontSizeHalfPt)/2),
				fmt.Sprintf("%.1fpt", float64(actual.FontSizeHalfPt)/2),
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
				fmt.Sprintf("%d twips", expected.LineSpacingVal),
				fmt.Sprintf("%d twips", actual.LineSpacingVal),
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
				fmt.Sprintf("%d twips", expected.SpaceBefore),
				fmt.Sprintf("%d twips", actual.SpaceBefore),
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
				fmt.Sprintf("%d twips", expected.SpaceAfter),
				fmt.Sprintf("%d twips", actual.SpaceAfter),
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
				fmt.Sprintf("%d twips", expected.FirstLineIndent),
				fmt.Sprintf("%d twips", actual.FirstLineIndent),
				"warning",
			})
		}
	}

	return diffs
}
