package handler

import (
	"strconv"
	"strings"
)

// friendlyConvertFormatRulesForDisplay 将格式规则转换为友好展示格式（供前端展示）
func friendlyConvertFormatRulesForDisplay(formatRules map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range formatRules {
		switch key {
		case "body":
			if m, ok := value.(map[string]interface{}); ok {
				result[key] = friendlyConvertBodyFormat(m)
			}
		case "title":
			if m, ok := value.(map[string]interface{}); ok {
				result[key] = friendlyConvertTitleLikeFormat(m)
			}
		case "author":
			if m, ok := value.(map[string]interface{}); ok {
				result[key] = friendlyConvertTitleLikeFormat(m)
			}
		case "abstract":
			if m, ok := value.(map[string]interface{}); ok {
				result[key] = friendlyConvertAbstractOrKeywordsFormat(m)
			}
		case "headings":
			if m, ok := value.(map[string]interface{}); ok {
				result[key] = friendlyConvertHeadingsFormat(m)
			}
		case "keywords":
			if m, ok := value.(map[string]interface{}); ok {
				result[key] = friendlyConvertAbstractOrKeywordsFormat(m)
			}
		case "page_setup":
			if m, ok := value.(map[string]interface{}); ok {
				result[key] = friendlyConvertPageSetupFormat(m)
			}
		case "references":
			if m, ok := value.(map[string]interface{}); ok {
				result[key] = friendlyConvertReferencesFormat(m)
			}
		default:
			result[key] = value
		}
	}

	return result
}

// friendlyAnnotateString 若为 string，则追加「（中文说明）」
func friendlyAnnotateString(v interface{}, explain func(string) string) interface{} {
	if s, ok := v.(string); ok {
		return s + "（" + explain(s) + "）"
	}
	return v
}

func friendlyAnnotateMap(in map[string]interface{}, rules map[string]func(string) string) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		if fn, ok := rules[k]; ok {
			out[k] = friendlyAnnotateString(v, fn)
		} else {
			out[k] = v
		}
	}
	return out
}

// friendlyRenameTopLevelKeys 替换顶层键名；未出现在 renames 中的键保持不变。
func friendlyRenameTopLevelKeys(m map[string]interface{}, renames map[string]string) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if nk, ok := renames[k]; ok {
			out[nk] = v
		} else {
			out[k] = v
		}
	}
	return out
}

var friendlyBodyAnnotators = map[string]func(string) string{
	"font_size":         friendlyFontSizeToChinese,
	"alignment":         friendlyAlignmentToChinese,
	"line_space":        friendlyLineSpaceToChinese,
	"first_line_indent": friendlyIndentToChinese,
}

// friendlyBodyAnnotatorsForAdmin 在正文块上额外注解 font_name（用户端 friendly 正文仍仅用 friendlyBodyAnnotators）
var friendlyBodyAnnotatorsForAdmin = map[string]func(string) string{
	"font_size":         friendlyFontSizeToChinese,
	"font_name":         friendlyFontNameToChinese,
	"alignment":         friendlyAlignmentToChinese,
	"line_space":        friendlyLineSpaceToChinese,
	"first_line_indent": friendlyIndentToChinese,
}

var friendlyTitleLikeAnnotators = map[string]func(string) string{
	"font_size": friendlyFontSizeToChinese,
	"alignment": friendlyAlignmentToChinese,
	"font_name": friendlyFontNameToChinese,
}

var friendlyLabelAnnotators = map[string]func(string) string{
	"font_size": friendlyFontSizeToChinese,
	"font_name": friendlyFontNameToChinese,
}

// friendlyAbstractCNContentAnnotators 中文摘要/关键词「内容」块（仅字号、字体加说明）
var friendlyAbstractCNContentAnnotators = map[string]func(string) string{
	"font_size": friendlyFontSizeToChinese,
	"font_name": friendlyFontNameToChinese,
}

var friendlyKeywordsContentAnnotators = friendlyAbstractCNContentAnnotators

// friendlySimpleTypographyAnnotators 目录/致谢/附录等内容区三项排版
var friendlySimpleTypographyAnnotators = map[string]func(string) string{
	"font_name": friendlyFontNameToChinese,
	"font_size": friendlyFontSizeToChinese,
	"alignment": friendlyAlignmentToChinese,
}

var friendlyContentAnnotators = map[string]func(string) string{
	"font_size": friendlyFontSizeToChinese,
	"font_name": friendlyFontNameToChinese,
	"alignment": friendlyAlignmentToChinese,
}

// friendlyEnglishAbstractContentAnnotators 英文摘要「内容」块（含行距）
var friendlyEnglishAbstractContentAnnotators = map[string]func(string) string{
	"font_size":  friendlyFontSizeToChinese,
	"font_name":  friendlyFontNameToChinese,
	"alignment":  friendlyAlignmentToChinese,
	"line_space": friendlyLineSpaceToChinese,
}

// friendlyReferencesContentAnnotators 参考文献正文样式字段注解
var friendlyReferencesContentAnnotators = map[string]func(string) string{
	"font_size":  friendlyFontSizeToChinese,
	"font_name":  friendlyFontNameToChinese,
	"alignment":  friendlyAlignmentToChinese,
	"line_space": friendlyLineSpaceToChinese,
}

func friendlyConvertBodyFormat(bodyMap map[string]interface{}) map[string]interface{} {
	return friendlyAnnotateMap(bodyMap, friendlyBodyAnnotators)
}

func friendlyConvertTitleLikeFormat(m map[string]interface{}) map[string]interface{} {
	return friendlyAnnotateMap(m, friendlyTitleLikeAnnotators)
}

func friendlyConvertAbstractOrKeywordsFormat(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch k {
		case "label":
			if mm, ok := v.(map[string]interface{}); ok {
				out[k] = friendlyAnnotateMap(mm, friendlyLabelAnnotators)
			} else {
				out[k] = v
			}
		case "content":
			if mm, ok := v.(map[string]interface{}); ok {
				out[k] = friendlyAnnotateMap(mm, friendlyContentAnnotators)
			} else {
				out[k] = v
			}
		default:
			out[k] = v
		}
	}
	return out
}

func friendlyConvertHeadingsFormat(headingsMap map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(headingsMap))
	for k, v := range headingsMap {
		if mm, ok := v.(map[string]interface{}); ok {
			out[k] = friendlyConvertTitleLikeFormat(mm)
		} else {
			out[k] = v
		}
	}
	return out
}

func friendlyConvertPageSetupFormat(pageSetupMap map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(pageSetupMap))
	for k, v := range pageSetupMap {
		switch k {
		case "margins", "header", "footer":
			if mm, ok := v.(map[string]interface{}); ok {
				out[k] = friendlyShallowCopyMap(mm)
			} else {
				out[k] = v
			}
		default:
			out[k] = v
		}
	}
	return out
}

func friendlyShallowCopyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

func friendlyConvertReferencesFormat(referencesMap map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(referencesMap))
	for k, v := range referencesMap {
		if k == "title" {
			if mm, ok := v.(map[string]interface{}); ok {
				out[k] = friendlyConvertTitleLikeFormat(mm)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func friendlyFontSizeToChinese(fontSize string) string {
	switch fontSize {
	case "小四号", "小四":
		return "12磅"
	case "四号":
		return "14磅"
	case "三号":
		return "16磅"
	case "小三号", "小三":
		return "15磅"
	case "二号":
		return "22磅"
	case "小二号", "小二":
		return "18磅"
	case "一号":
		return "26磅"
	case "小一号", "小一":
		return "24磅"
	case "五号":
		return "10.5磅"
	case "小五号", "小五":
		return "9磅"
	case "六号":
		return "7.5磅"
	case "小六":
		return "6.5磅"
	case "七号":
		return "5.5磅"
	case "八号":
		return "5磅"
	default:
		return fontSize
	}
}

func friendlyAlignmentToChinese(alignment string) string {
	switch alignment {
	case "left":
		return "左对齐"
	case "center":
		return "居中"
	case "right":
		return "右对齐"
	case "justify":
		return "两端对齐"
	default:
		return alignment
	}
}

func friendlyLineSpaceToChinese(lineSpace string) string {
	switch lineSpace {
	case "1.0", "single":
		return "单倍行距"
	case "1.5", "1.5倍":
		return "1.5倍行距"
	case "2.0", "double", "2":
		return "双倍行距"
	case "multiple":
		return "多倍行距"
	case "fixed":
		return "固定值"
	default:
		if strings.HasPrefix(lineSpace, "fixed_") {
			ptStr := strings.TrimPrefix(lineSpace, "fixed_")
			ptStr = strings.TrimSuffix(ptStr, "_pt")
			if _, err := strconv.ParseFloat(ptStr, 64); err == nil {
				return ptStr + "磅固定值"
			}
		}
		if _, err := strconv.ParseFloat(lineSpace, 64); err == nil {
			return lineSpace + "倍行距"
		}
		return lineSpace
	}
}

func friendlyIndentToChinese(indent string) string {
	switch indent {
	case "2字符":
		return "2个字符宽度"
	case "1字符":
		return "1个字符宽度"
	case "0字符":
		return "无缩进"
	default:
		return indent
	}
}

func friendlyFontNameToChinese(fontName string) string {
	switch fontName {
	case "宋体":
		return "SimSun"
	case "黑体":
		return "SimHei"
	case "仿宋":
		return "FangSong"
	case "楷体":
		return "KaiTi"
	case "Times New Roman":
		return "西文字体"
	default:
		return fontName
	}
}
