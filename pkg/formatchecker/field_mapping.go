package formatchecker

import (
	"fmt"
	"regexp"
	"strings"
)

// FieldMapping 字段映射配置
type FieldMapping struct {
	EnglishKey  string // 英文键名
	ChineseKey  string // 中文键名（兼容旧数据）
	FieldType   string // 字段类型
	Required    bool   // 是否必填
	Default     string // 默认值
	Validation  string // 正则验证规则
	PatternDesc string // 规则描述
	Description string // 字段描述
	Children    map[string]*FieldMapping
}

// FormatRequirements 格式要求结构（后端定义）
type FormatRequirements struct {
	Cover            *CommonFormat     `json:"cover,omitempty"`
	TableOfContents  *CommonFormat     `json:"table_of_contents,omitempty"`
	Title            *CommonFormat     `json:"title,omitempty"`
	Author           *CommonFormat     `json:"author,omitempty"`
	Abstract         *AbstractFormat   `json:"abstract,omitempty"`
	EnglishTitle     *CommonFormat     `json:"english_title,omitempty"`
	EnglishAbstract  *AbstractFormat   `json:"english_abstract,omitempty"`
	Body             *CommonFormat     `json:"body"`
	Headings         *HeadingsFormat   `json:"headings"`
	Keywords         *KeywordsFormat   `json:"keywords,omitempty"`
	References       *ReferencesFormat `json:"references,omitempty"`
	Acknowledgements *CommonFormat     `json:"acknowledgements,omitempty"`
	Appendix         *AppendixFormat   `json:"appendix,omitempty"`
	PageSetup        *PageSetup        `json:"page_setup,omitempty"`
}

// CommonFormat 通用格式配置
type CommonFormat struct {
	FontName        string          `json:"font_name,omitempty"`
	FontSize        string          `json:"font_size,omitempty"`
	Alignment       string          `json:"alignment,omitempty"`
	Bold            bool            `json:"bold,omitempty"`
	LineSpace       string          `json:"line_space,omitempty"`
	ParagraphSpace  *ParagraphSpace `json:"paragraph_space,omitempty"`
	FirstLineIndent string          `json:"first_line_indent,omitempty"`
}

// AbstractFormat 摘要格式
type AbstractFormat struct {
	Label   *LabelFormat  `json:"label,omitempty"`
	Content *CommonFormat `json:"content,omitempty"`
}

// LabelFormat 标签格式
type LabelFormat struct {
	FontName string `json:"font_name,omitempty"`
	FontSize string `json:"font_size,omitempty"`
	Bold     bool   `json:"bold,omitempty"`
	Text     string `json:"text,omitempty"`
}

// HeadingsFormat 标题层级格式
type HeadingsFormat struct {
	Level1 *HeadingLevel `json:"level1,omitempty"`
	Level2 *HeadingLevel `json:"level2,omitempty"`
	Level3 *HeadingLevel `json:"level3,omitempty"`
	Level4 *HeadingLevel `json:"level4,omitempty"`
	Level5 *HeadingLevel `json:"level5,omitempty"`
	Level6 *HeadingLevel `json:"level6,omitempty"`
}

// HeadingLevel 单级标题格式
type HeadingLevel struct {
	FontName  string `json:"font_name,omitempty"`
	FontSize  string `json:"font_size,omitempty"`
	Bold      bool   `json:"bold,omitempty"`
	Alignment string `json:"alignment,omitempty"`
	LineSpace string `json:"line_space,omitempty"`
	Numbering string `json:"numbering,omitempty"`
}

// KeywordsFormat 关键词格式
type KeywordsFormat struct {
	Label   *LabelFormat     `json:"label,omitempty"`
	Content *KeywordsContent `json:"content,omitempty"`
}

// KeywordsContent 关键词内容格式
type KeywordsContent struct {
	FontName  string `json:"font_name,omitempty"`
	FontSize  string `json:"font_size,omitempty"`
	Alignment string `json:"alignment,omitempty"`
	Separator string `json:"separator,omitempty"`
	Bold      bool   `json:"bold,omitempty"`
}

// ReferencesFormat 参考文献格式
type ReferencesFormat struct {
	Title   *CommonFormat `json:"title,omitempty"`
	Content *CommonFormat `json:"content,omitempty"`
}

// AppendixFormat 附录格式
type AppendixFormat struct {
	Title   *CommonFormat `json:"title,omitempty"`
	Content *CommonFormat `json:"content,omitempty"`
}

// ParagraphSpace 段落间距
type ParagraphSpace struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// Margins 页边距
type Margins struct {
	Top    string `json:"top,omitempty"`
	Bottom string `json:"bottom,omitempty"`
	Left   string `json:"left,omitempty"`
	Right  string `json:"right,omitempty"`
}

// HeaderFooter 页眉/页脚
type HeaderFooter struct {
	Distance string `json:"distance,omitempty"`
}

// ValidationError 验证错误
type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	Value   any    `json:"value,omitempty"`
}

// 完整字段映射表
var fieldMappings = map[string]*FieldMapping{
	"cover": {
		EnglishKey:  "cover",
		ChineseKey:  "封面",
		FieldType:   "object",
		Required:    false,
		Description: "论文封面格式",
		Children: map[string]*FieldMapping{
			"font_name": {EnglishKey: "font_name", ChineseKey: "字体名称", FieldType: "string", Required: false, Validation: `^[\u4e00-\u9fa5A-Za-z\s]+$`, PatternDesc: "中英文或空格"},
			"font_size": {EnglishKey: "font_size", ChineseKey: "字号", FieldType: "string", Required: false, Validation: `^(初号|小初|一号|小一|二号|小二|三号|小三|四号|小四|五号|小五|六号|小六|七号|小八|[0-9]+pt)$`, PatternDesc: "中文字号或数字+pt"},
			"alignment": {EnglishKey: "alignment", ChineseKey: "对齐方式", FieldType: "string", Required: false, Validation: `^(left|center|right|justify)$`, PatternDesc: "left/center/right/justify"},
			"bold":      {EnglishKey: "bold", ChineseKey: "是否加粗", FieldType: "boolean", Required: false},
		},
	},
	"title": {
		EnglishKey:  "title",
		ChineseKey:  "论文标题",
		FieldType:   "object",
		Required:    false,
		Description: "论文主标题格式",
		Children: map[string]*FieldMapping{
			"font_name":  {EnglishKey: "font_name", ChineseKey: "字体名称", FieldType: "string", Required: true, Default: "黑体", Validation: `^[\u4e00-\u9fa5A-Za-z\s]+$`, PatternDesc: "中英文或空格"},
			"font_size":  {EnglishKey: "font_size", ChineseKey: "字号", FieldType: "string", Required: true, Default: "三号", Validation: `^(初号|小初|一号|小一|二号|小二|三号|小三|四号|小四|[0-9]+pt)$`, PatternDesc: "中文字号或数字+pt"},
			"bold":       {EnglishKey: "bold", ChineseKey: "是否加粗", FieldType: "boolean", Required: false, Default: "true"},
			"alignment":  {EnglishKey: "alignment", ChineseKey: "对齐方式", FieldType: "string", Required: false, Default: "center", Validation: `^(left|center|right|justify)$`, PatternDesc: "left/center/right/justify"},
			"line_space": {EnglishKey: "line_space", ChineseKey: "行间距", FieldType: "string", Required: false, Validation: `^(single|1|1.5|2|double|fixed_[0-9]+_pt|min)$`, PatternDesc: "single/1/1.5/2/double/fixed_N_pt/min"},
		},
	},
	"body": {
		EnglishKey:  "body",
		ChineseKey:  "正文",
		FieldType:   "object",
		Required:    true,
		Description: "论文正文格式",
		Children: map[string]*FieldMapping{
			"font_name":         {EnglishKey: "font_name", ChineseKey: "字体名称", FieldType: "string", Required: true, Default: "宋体", Validation: `^[\u4e00-\u9fa5A-Za-z\s]+$`, PatternDesc: "中英文或空格"},
			"font_size":         {EnglishKey: "font_size", ChineseKey: "字号", FieldType: "string", Required: true, Default: "小四", Validation: `^(初号|小初|一号|小一|二号|小二|三号|小三|四号|小四|五号|小五|六号|小六|七号|小八|[0-9]+pt)$`, PatternDesc: "中文字号或数字+pt"},
			"alignment":         {EnglishKey: "alignment", ChineseKey: "对齐方式", FieldType: "string", Required: false, Default: "justify", Validation: `^(left|center|right|justify)$`, PatternDesc: "left/center/right/justify"},
			"line_space":        {EnglishKey: "line_space", ChineseKey: "行间距", FieldType: "string", Required: false, Default: "fixed_20_pt", Validation: `^(single|1|1.5|2|double|fixed_[0-9]+_pt|min)$`, PatternDesc: "single/1/1.5/2/double/fixed_N_pt/min"},
			"first_line_indent": {EnglishKey: "first_line_indent", ChineseKey: "首行缩进", FieldType: "string", Required: false, Default: "2字符", Validation: `^[0-9]+(字符|cm|mm|in|pt)$`, PatternDesc: "数字+单位(字符/cm/mm/in/pt)"},
		},
	},
	"headings": {
		EnglishKey:  "headings",
		ChineseKey:  "标题层级",
		FieldType:   "object",
		Required:    true,
		Description: "各级标题格式",
		Children: map[string]*FieldMapping{
			"level1": {
				EnglishKey: "level1", ChineseKey: "一级标题", FieldType: "object", Required: true,
				Children: map[string]*FieldMapping{
					"font_name":  {EnglishKey: "font_name", ChineseKey: "字体名称", FieldType: "string", Required: true, Default: "黑体", Validation: `^[\u4e00-\u9fa5A-Za-z\s]+$`, PatternDesc: "中英文或空格"},
					"font_size":  {EnglishKey: "font_size", ChineseKey: "字号", FieldType: "string", Required: true, Default: "三号", Validation: `^(初号|小初|一号|小一|二号|小二|三号|小三|四号|小四|[0-9]+pt)$`, PatternDesc: "中文字号或数字+pt"},
					"bold":       {EnglishKey: "bold", ChineseKey: "是否加粗", FieldType: "boolean", Required: false},
					"alignment":  {EnglishKey: "alignment", ChineseKey: "对齐方式", FieldType: "string", Required: false, Default: "center", Validation: `^(left|center|right|justify)$`, PatternDesc: "left/center/right/justify"},
					"line_space": {EnglishKey: "line_space", ChineseKey: "行间距", FieldType: "string", Required: false, Validation: `^(single|1|1.5|2|double|fixed_[0-9]+_pt|min)$`, PatternDesc: "single/1/1.5/2/double/fixed_N_pt/min"},
					"numbering":  {EnglishKey: "numbering", ChineseKey: "编号格式", FieldType: "string", Required: false},
				},
			},
			"level2": {
				EnglishKey: "level2", ChineseKey: "二级标题", FieldType: "object", Required: true,
				Children: map[string]*FieldMapping{
					"font_name":  {EnglishKey: "font_name", ChineseKey: "字体名称", FieldType: "string", Required: true, Default: "黑体", Validation: `^[\u4e00-\u9fa5A-Za-z\s]+$`, PatternDesc: "中英文或空格"},
					"font_size":  {EnglishKey: "font_size", ChineseKey: "字号", FieldType: "string", Required: true, Default: "小三", Validation: `^(初号|小初|一号|小一|二号|小二|三号|小三|四号|小四|[0-9]+pt)$`, PatternDesc: "中文字号或数字+pt"},
					"bold":       {EnglishKey: "bold", ChineseKey: "是否加粗", FieldType: "boolean", Required: false},
					"alignment":  {EnglishKey: "alignment", ChineseKey: "对齐方式", FieldType: "string", Required: false, Default: "left", Validation: `^(left|center|right|justify)$`, PatternDesc: "left/center/right/justify"},
					"line_space": {EnglishKey: "line_space", ChineseKey: "行间距", FieldType: "string", Required: false, Validation: `^(single|1|1.5|2|double|fixed_[0-9]+_pt|min)$`, PatternDesc: "single/1/1.5/2/double/fixed_N_pt/min"},
					"numbering":  {EnglishKey: "numbering", ChineseKey: "编号格式", FieldType: "string", Required: false},
				},
			},
			"level3": {
				EnglishKey: "level3", ChineseKey: "三级标题", FieldType: "object", Required: false,
				Children: map[string]*FieldMapping{
					"font_name":  {EnglishKey: "font_name", ChineseKey: "字体名称", FieldType: "string", Required: true, Default: "黑体", Validation: `^[\u4e00-\u9fa5A-Za-z\s]+$`, PatternDesc: "中英文或空格"},
					"font_size":  {EnglishKey: "font_size", ChineseKey: "字号", FieldType: "string", Required: true, Default: "四号", Validation: `^(初号|小初|一号|小一|二号|小二|三号|小三|四号|小四|[0-9]+pt)$`, PatternDesc: "中文字号或数字+pt"},
					"bold":       {EnglishKey: "bold", ChineseKey: "是否加粗", FieldType: "boolean", Required: false},
					"alignment":  {EnglishKey: "alignment", ChineseKey: "对齐方式", FieldType: "string", Required: false, Default: "left", Validation: `^(left|center|right|justify)$`, PatternDesc: "left/center/right/justify"},
					"line_space": {EnglishKey: "line_space", ChineseKey: "行间距", FieldType: "string", Required: false, Validation: `^(single|1|1.5|2|double|fixed_[0-9]+_pt|min)$`, PatternDesc: "single/1/1.5/2/double/fixed_N_pt/min"},
					"numbering":  {EnglishKey: "numbering", ChineseKey: "编号格式", FieldType: "string", Required: false},
				},
			},
		},
	},
	"page_setup": {
		EnglishKey:  "page_setup",
		ChineseKey:  "页面设置",
		FieldType:   "object",
		Required:    false,
		Description: "页面布局设置",
		Children: map[string]*FieldMapping{
			"paper_size":  {EnglishKey: "paper_size", ChineseKey: "纸张大小", FieldType: "string", Required: false, Default: "A4", Validation: `^(A4|A3|Letter|Legal|B5)$`, PatternDesc: "A4/A3/Letter/Legal/B5"},
			"orientation": {EnglishKey: "orientation", ChineseKey: "页面方向", FieldType: "string", Required: false, Default: "portrait", Validation: `^(portrait|landscape)$`, PatternDesc: "portrait(纵向)/landscape(横向)"},
			"margins": {
				EnglishKey: "margins", ChineseKey: "页边距", FieldType: "object", Required: false,
				Children: map[string]*FieldMapping{
					"top":    {EnglishKey: "top", ChineseKey: "上边距", FieldType: "string", Required: false, Validation: `^[0-9]+(\.[0-9]+)?(cm|mm|in|pt)$`, PatternDesc: "数字+单位(cm/mm/in/pt)"},
					"bottom": {EnglishKey: "bottom", ChineseKey: "下边距", FieldType: "string", Required: false, Validation: `^[0-9]+(\.[0-9]+)?(cm|mm|in|pt)$`, PatternDesc: "数字+单位(cm/mm/in/pt)"},
					"left":   {EnglishKey: "left", ChineseKey: "左边距", FieldType: "string", Required: false, Validation: `^[0-9]+(\.[0-9]+)?(cm|mm|in|pt)$`, PatternDesc: "数字+单位(cm/mm/in/pt)"},
					"right":  {EnglishKey: "right", ChineseKey: "右边距", FieldType: "string", Required: false, Validation: `^[0-9]+(\.[0-9]+)?(cm|mm|in|pt)$`, PatternDesc: "数字+单位(cm/mm/in/pt)"},
				},
			},
			"header": {
				EnglishKey: "header", ChineseKey: "页眉", FieldType: "object", Required: false,
				Children: map[string]*FieldMapping{
					"distance": {EnglishKey: "distance", ChineseKey: "页眉距离", FieldType: "string", Required: false, Validation: `^[0-9]+(\.[0-9]+)?(cm|mm|in|pt)$`, PatternDesc: "数字+单位(cm/mm/in/pt)"},
				},
			},
			"footer": {
				EnglishKey: "footer", ChineseKey: "页脚", FieldType: "object", Required: false,
				Children: map[string]*FieldMapping{
					"distance": {EnglishKey: "distance", ChineseKey: "页脚距离", FieldType: "string", Required: false, Validation: `^[0-9]+(\.[0-9]+)?(cm|mm|in|pt)$`, PatternDesc: "数字+单位(cm/mm/in/pt)"},
				},
			},
		},
	},
}

// 反向映射表（中文 -> 英文）
var reverseMapping map[string]string

func init() {
	reverseMapping = make(map[string]string)
	for engKey, mapping := range fieldMappings {
		reverseMapping[mapping.ChineseKey] = engKey
		if mapping.Children != nil {
			for childKey, childMapping := range mapping.Children {
				reverseMapping[mapping.ChineseKey+childMapping.ChineseKey] = engKey + "." + childKey
				if childMapping.Children != nil {
					for subChildKey, subChildMapping := range childMapping.Children {
						reverseMapping[mapping.ChineseKey+childMapping.ChineseKey+subChildMapping.ChineseKey] =
							engKey + "." + childKey + "." + subChildKey
					}
				}
			}
		}
	}
}

// NormalizeKey 标准化字段键名
func NormalizeKey(key string) string {
	if mapping, ok := fieldMappings[key]; ok {
		return mapping.EnglishKey
	}
	if engKey, ok := reverseMapping[key]; ok {
		return engKey
	}
	return key
}

// GetChinese 获取中文键名
func GetChinese(key string) string {
	keys := strings.Split(key, ".")
	if mapping, ok := fieldMappings[keys[0]]; ok {
		if len(keys) > 1 && mapping.Children != nil {
			if childMapping, ok := mapping.Children[keys[1]]; ok {
				if len(keys) > 2 && childMapping.Children != nil {
					if subMapping, ok := childMapping.Children[keys[2]]; ok {
						return subMapping.ChineseKey
					}
				}
				return childMapping.ChineseKey
			}
		}
		return mapping.ChineseKey
	}
	return key
}

// ValidateField 验证单个字段
func ValidateField(key string, value any) *ValidationError {
	keys := strings.Split(key, ".")
	mapping := fieldMappings[keys[0]]

	if mapping == nil {
		return nil
	}

	if len(keys) > 1 && mapping.Children != nil {
		mapping = mapping.Children[keys[1]]
		if mapping == nil {
			return nil
		}
		if len(keys) > 2 && mapping.Children != nil {
			mapping = mapping.Children[keys[2]]
			if mapping == nil {
				return nil
			}
		}
	}

	if mapping == nil {
		return nil
	}

	// 必填验证
	if mapping.Required {
		if value == nil || value == "" {
			return &ValidationError{
				Path:    key,
				Message: fmt.Sprintf("%s 不能为空", mapping.ChineseKey),
				Value:   value,
			}
		}
	}

	// 跳过空值验证
	if value == nil || value == "" {
		return nil
	}

	// 正则验证
	if mapping.Validation != "" {
		re, err := regexp.Compile("^" + mapping.Validation + "$")
		if err == nil {
			if !re.MatchString(fmt.Sprintf("%v", value)) {
				return &ValidationError{
					Path:    key,
					Message: fmt.Sprintf("%s 格式不正确，应为: %s", mapping.ChineseKey, mapping.PatternDesc),
					Value:   value,
				}
			}
		}
	}

	return nil
}

// ValidateAll 验证整个格式配置
func ValidateAll(config map[string]interface{}) []ValidationError {
	var errors []ValidationError
	flattened := flattenMap("", config)

	for key, value := range flattened {
		if err := ValidateField(key, value); err != nil {
			errors = append(errors, *err)
		}
	}

	return errors
}

// flattenMap 扁平化嵌套 map
func flattenMap(prefix string, m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for k, v := range m {
		newKey := k
		if prefix != "" {
			newKey = prefix + "." + k
		}

		if nestedMap, ok := v.(map[string]interface{}); ok {
			nested := flattenMap(newKey, nestedMap)
			for nk, nv := range nested {
				result[nk] = nv
			}
		} else {
			result[newKey] = v
		}
	}

	return result
}

// CleanFormatRequirements 清洗格式要求数据
func CleanFormatRequirements(config map[string]interface{}) map[string]interface{} {
	cleaned := make(map[string]interface{})

	for key, value := range config {
		// 标准化键名
		normalizedKey := NormalizeKey(key)

		// 递归处理嵌套对象
		if nestedMap, ok := value.(map[string]interface{}); ok {
			cleaned[normalizedKey] = CleanFormatRequirements(nestedMap)
		} else if nestedMap, ok := value.(map[string]any); ok {
			cleaned[normalizedKey] = CleanFormatRequirements(nestedMap)
		} else if strVal, ok := value.(string); ok {
			// 修复单位拼写错误
			cleaned[normalizedKey] = fixUnitErrors(strVal)
		} else {
			cleaned[normalizedKey] = value
		}
	}

	return cleaned
}

// fixUnitErrors 修复常见的单位拼写错误
func fixUnitErrors(value string) string {
	// 修复 2.54c -> 2.54cm
	if value == "2.54c" || value == "2.54" {
		return "2.54cm"
	}
	// 修复以 c 结尾的单位
	if matched, _ := regexp.MatchString(`^\d+(\.\d+)?c$`, value); matched {
		return value + "m"
	}
	return value
}

// GetDefaultFormat 获取默认格式配置
func GetDefaultFormat() map[string]interface{} {
	return map[string]interface{}{
		"body": map[string]interface{}{
			"font_name":         "宋体",
			"font_size":         "小四",
			"alignment":         "justify",
			"line_space":        "fixed_20_pt",
			"first_line_indent": "2字符",
			"paragraph_space": map[string]string{
				"before": "0",
				"after":  "0",
			},
		},
		"headings": map[string]interface{}{
			"level1": map[string]interface{}{
				"font_name":  "黑体",
				"font_size":  "三号",
				"bold":       false,
				"alignment":  "center",
				"line_space": "fixed_20_pt",
				"numbering":  "1",
			},
			"level2": map[string]interface{}{
				"font_name":  "黑体",
				"font_size":  "小三",
				"bold":       false,
				"alignment":  "left",
				"line_space": "fixed_20_pt",
				"numbering":  "1.1",
			},
			"level3": map[string]interface{}{
				"font_name":  "黑体",
				"font_size":  "四号",
				"bold":       false,
				"alignment":  "left",
				"line_space": "fixed_20_pt",
				"numbering":  "1.1.1",
			},
		},
		"page_setup": map[string]interface{}{
			"paper_size":  "A4",
			"orientation": "portrait",
			"margins": map[string]string{
				"top":    "2.5cm",
				"bottom": "2.5cm",
				"left":   "2.5cm",
				"right":  "2.5cm",
			},
			"header": map[string]string{
				"distance": "1.5cm",
			},
			"footer": map[string]string{
				"distance": "1.75cm",
			},
		},
	}
}

// CompareFormats 比较两个格式配置
func CompareFormats(a, b map[string]interface{}) map[string]interface{} {
	flattenedA := flattenMap("", a)
	flattenedB := flattenMap("", b)

	changes := make(map[string]interface{})
	allKeys := make(map[string]bool)

	for k := range flattenedA {
		allKeys[k] = true
	}
	for k := range flattenedB {
		allKeys[k] = true
	}

	for key := range allKeys {
		valA := flattenedA[key]
		valB := flattenedB[key]

		if fmt.Sprintf("%v", valA) != fmt.Sprintf("%v", valB) {
			changes[key] = map[string]interface{}{
				"old": valA,
				"new": valB,
			}
		}
	}

	return changes
}
