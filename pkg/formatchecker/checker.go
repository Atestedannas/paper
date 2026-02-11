package formatchecker

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/pkg/fileprocessor"
)

// FormatChecker 格式检查器接口
type FormatChecker interface {
	// Check 检查文档格式，返回检查结果
	Check(ctx context.Context, docPath string) (*CheckResult, error)

	// GenerateCorrections 根据检查结果生成修正建议
	GenerateCorrections(ctx context.Context, result *CheckResult) ([]Correction, error)

	// ApplyCorrections 应用修正建议到文档
	ApplyCorrections(ctx context.Context, docPath string, corrections []Correction) (string, error)

	// FixDocumentDirectly 直接修复文档，不生成中间检查结果
	FixDocumentDirectly(ctx context.Context, docPath string, standard FormatStandard) (string, error)
}

// CheckResult 格式检查结果
type CheckResult struct {
	DocumentPath string                 `json:"document_path"` // 文档路径
	DocInfo      map[string]interface{} `json:"doc_info"`      // 文档基本信息
	Issues       []FormatIssue          `json:"issues"`        // 格式问题列表
	TotalIssues  int                    `json:"total_issues"`  // 问题总数
	ErrorCount   int                    `json:"error_count"`   // 错误数量
	WarningCount int                    `json:"warning_count"` // 警告数量
	InfoCount    int                    `json:"info_count"`    // 信息数量
}

// FormatIssue 格式问题
type FormatIssue struct {
	ID          string                 `json:"id"`                // 问题唯一标识
	Type        IssueType              `json:"type"`              // 问题类型
	Severity    SeverityLevel          `json:"severity"`          // 严重程度
	Page        int                    `json:"page"`              // 页码
	Position    int                    `json:"position"`          // 在页面中的位置
	Description string                 `json:"description"`       // 问题描述
	Original    interface{}            `json:"original"`          // 原始内容或格式
	Suggestion  interface{}            `json:"suggestion"`        // 修正建议
	Details     map[string]interface{} `json:"details,omitempty"` // 详细信息
}

// IssueType 格式问题类型
type IssueType string

const (
	IssueTypeHeading      IssueType = "heading"       // 标题格式
	IssueTypeParagraph    IssueType = "paragraph"     // 段落格式
	IssueTypeCitation     IssueType = "citation"      // 引用格式
	IssueTypeReference    IssueType = "reference"     // 参考文献格式
	IssueTypeTable        IssueType = "table"         // 表格格式
	IssueTypeFigure       IssueType = "figure"        // 图表格式
	IssueTypePageNumber   IssueType = "page_number"   // 页码格式
	IssueTypeAbstract     IssueType = "abstract"      // 摘要格式
	IssueTypeKeywords     IssueType = "keywords"      // 关键词格式
	IssueTypeTitlePage    IssueType = "title_page"    // 标题页格式
	IssueTypePageSetup    IssueType = "page_setup"    // 页面设置
	IssueTypeFontSpacing  IssueType = "font_spacing"  // 字体间距
	IssueTypeMainBody     IssueType = "main_body"     // 正文格式
	IssueTypeFrontPart    IssueType = "front_part"    // 前置部分格式
	IssueTypeHeaderFooter IssueType = "header_footer" // 页眉页脚
	IssueTypeFont         IssueType = "font"          // 字体
	IssueTypeSpacing      IssueType = "spacing"       // 间距
)

// SeverityLevel 问题严重程度
type SeverityLevel string

const (
	SeverityError   SeverityLevel = "error"   // 错误
	SeverityWarning SeverityLevel = "warning" // 警告
	SeverityInfo    SeverityLevel = "info"    // 信息
)

// Correction 修正建议
type Correction struct {
	ID          string                 `json:"id"`          // 修正唯一标识
	IssueID     string                 `json:"issue_id"`    // 对应的问题ID
	Type        CorrectionType         `json:"type"`        // 修正类型
	Description string                 `json:"description"` // 修正描述
	Original    map[string]interface{} `json:"original"`    // 原始格式
	Corrected   map[string]interface{} `json:"corrected"`   // 修正后格式
	Applied     bool                   `json:"applied"`     // 是否已应用
	Location    CorrectionLocation     `json:"location"`    // 修正位置
	Action      string                 `json:"action"`      // 修正操作
	Parameters  map[string]interface{} `json:"parameters"`  // 修正参数
}

// CorrectionLocation 修正位置
type CorrectionLocation struct {
	Page        int    `json:"page"`         // 页码
	StartPos    int    `json:"start_pos"`    // 起始位置
	EndPos      int    `json:"end_pos"`      // 结束位置
	ParagraphID string `json:"paragraph_id"` // 段落ID（如果适用）
	ElementID   string `json:"element_id"`   // 元素ID（如果适用）
}

// FormatStandard 格式标准配置
type FormatStandard struct {
	Name            string           `json:"name"`             // 标准名称
	Description     string           `json:"description"`      // 标准描述
	PageSetup       PageSetup        `json:"page_setup"`       // 页面设置
	HeadingStyles   []HeadingStyle   `json:"heading_styles"`   // 标题样式
	ParagraphStyles []ParagraphStyle `json:"paragraph_styles"` // 段落样式
	TableStyle      TableStyle       `json:"table_style"`      // 表格样式
	FigureStyle     FigureStyle      `json:"figure_style"`     // 图表样式
	ReferenceStyle  ReferenceStyle   `json:"reference_style"`  // 参考文献样式
	AbstractStyles  []AbstractStyle  `json:"abstract_styles"`  // 摘要样式
}

// PageSetup 页面设置
type PageSetup struct {
	PaperSize      string  `json:"paper_size"`      // 纸张大小 (A4)
	MarginTop      float64 `json:"margin_top"`      // 上边距 (cm)
	MarginBottom   float64 `json:"margin_bottom"`   // 下边距 (cm)
	MarginLeft     float64 `json:"margin_left"`     // 左边距 (cm)
	MarginRight    float64 `json:"margin_right"`    // 右边距 (cm)
	HeaderDistance float64 `json:"header_distance"` // 页眉距离 (cm)
	FooterDistance float64 `json:"footer_distance"` // 页脚距离 (cm)
}

// HeadingStyle 标题样式配置
type HeadingStyle struct {
	Level         int     `json:"level"`          // 标题级别
	Name          string  `json:"name"`           // 样式名称
	FontName      string  `json:"font_name"`      // 字体名称
	FontSize      float64 `json:"font_size"`      // 字体大小（磅）
	Bold          bool    `json:"bold"`           // 是否粗体
	Alignment     string  `json:"alignment"`      // 对齐方式 (left, center, right, justify)
	SpacingBefore float64 `json:"spacing_before"` // 段前间距（磅）
	SpacingAfter  float64 `json:"spacing_after"`  // 段后间距（磅）
	LineSpacing   float64 `json:"line_spacing"`   // 行间距（磅）
	IndentRight   int     `json:"indent_right"`   // 右缩进字符数
}

// ParagraphStyle 段落样式配置
type ParagraphStyle struct {
	Name            string  `json:"name"`              // 样式名称
	FontName        string  `json:"font_name"`         // 字体名称
	FontSize        float64 `json:"font_size"`         // 字体大小（磅）
	Alignment       string  `json:"alignment"`         // 对齐方式
	LineSpacing     float64 `json:"line_spacing"`      // 行间距（磅）
	FirstLineIndent float64 `json:"first_line_indent"` // 首行缩进（字符）
	SpacingBefore   float64 `json:"spacing_before"`    // 段前间距（磅）
	SpacingAfter    float64 `json:"spacing_after"`     // 段后间距（磅）
}

// TableStyle 表格样式配置
type TableStyle struct {
	CaptionPrefix   string  `json:"caption_prefix"`   // 表格标题前缀 (表格 X-)
	FontName        string  `json:"font_name"`        // 字体名称
	FontSize        float64 `json:"font_size"`        // 字体大小
	CaptionPosition string  `json:"caption_position"` // 标题位置 (top, bottom)
	BorderStyle     string  `json:"border_style"`     // 边框样式
}

// FigureStyle 图表样式配置
type FigureStyle struct {
	CaptionPrefix   string  `json:"caption_prefix"`   // 图表标题前缀 (图 X-)
	FontName        string  `json:"font_name"`        // 字体名称
	FontSize        float64 `json:"font_size"`        // 字体大小
	CaptionPosition string  `json:"caption_position"` // 标题位置 (top, bottom)
}

// ReferenceStyle 参考文献样式配置
type ReferenceStyle struct {
	Style        string  `json:"style"`         // 参考文献样式 (GB/T 7714)
	FontName     string  `json:"font_name"`     // 字体名称
	FontSize     float64 `json:"font_size"`     // 字体大小
	LineSpacing  float64 `json:"line_spacing"`  // 行间距
	IndentFormat string  `json:"indent_format"` // 缩进格式
}

// AbstractStyle 摘要样式配置
type AbstractStyle struct {
	Type           string  `json:"type"`            // 摘要类型 (chinese, english)
	Heading        string  `json:"heading"`         // 摘要标题
	FontName       string  `json:"font_name"`       // 字体名称
	FontSize       float64 `json:"font_size"`       // 字体大小
	Bold           bool    `json:"bold"`            // 是否粗体
	Alignment      string  `json:"alignment"`       // 对齐方式
	LineSpacing    float64 `json:"line_spacing"`    // 行间距
	KeywordsPrefix string  `json:"keywords_prefix"` // 关键词前缀
}

// CheckerFactory 格式检查器工厂
type CheckerFactory struct{}

// NewCheckerFactory 创建新的检查器工厂实例
func NewCheckerFactory() *CheckerFactory {
	return &CheckerFactory{}
}

// CreateChecker 根据文档类型创建相应的格式检查器
func (f *CheckerFactory) CreateChecker(docType string, processor fileprocessor.FileProcessor, standard FormatStandard) (FormatChecker, error) {
	switch docType {
	case "docx":
		// 使用Go实现的DOCX检查器
		checker := NewDOCXChecker()
		checker.SetStandard(standard)
		return checker, nil
	case "pdf":
		// PDF检查器已实现
		return NewPDFChecker(processor, standard), nil
	default:
		return nil, fmt.Errorf("unsupported document type: %s", docType)
	}
}

// ParseRequirementsToStandard 将解析的格式要求转换为FormatStandard结构
func ParseRequirementsToStandard(parsedRequirements map[string]interface{}) FormatStandard {

	standard := FormatStandard{
		Name:        "解析格式标准",
		Description: "从用户上传的格式要求解析生成的格式标准",
	}

	// 检查是否有顶层页面设置（中文键）
	if _, ok := parsedRequirements["页面设置"].(map[string]interface{}); ok {
		// 如果直接有页面设置，说明是直接格式要求（中文键）
		standard.PageSetup = parsePageSetup(parsedRequirements)
		standard.HeadingStyles = parseHeadingStyles(parsedRequirements)
		standard.ParagraphStyles = parseParagraphStyles(parsedRequirements)
		standard.TableStyle = parseTableStyle(parsedRequirements)
		standard.FigureStyle = parseFigureStyle(parsedRequirements)
		standard.ReferenceStyle = parseReferenceStyle(parsedRequirements)
		standard.AbstractStyles = parseAbstractStyles(parsedRequirements)
	} else if formatReqs, ok := parsedRequirements["format_requirements"].(map[string]interface{}); ok {
		// 否则使用嵌套的format_requirements结构
		standard.PageSetup = parsePageSetup(formatReqs)
		standard.HeadingStyles = parseHeadingStyles(formatReqs)
		standard.ParagraphStyles = parseParagraphStyles(formatReqs)
		standard.TableStyle = parseTableStyle(formatReqs)
		standard.FigureStyle = parseFigureStyle(formatReqs)
		standard.ReferenceStyle = parseReferenceStyle(formatReqs)
		standard.AbstractStyles = parseAbstractStyles(formatReqs)
	} else if basicRequirements, ok := parsedRequirements["基本要求"].(map[string]interface{}); ok {
		// 尝试从基本要求中提取格式信息（中文键）
		standard.PageSetup = parsePageSetup(basicRequirements)
		standard.HeadingStyles = parseHeadingStyles(basicRequirements)
		standard.ParagraphStyles = parseParagraphStyles(basicRequirements)
	} else if _, ok := parsedRequirements["page_setup"].(map[string]interface{}); ok {
		// 检查是否有顶层页面设置（英文键）
		standard.PageSetup = parsePageSetupEnglish(parsedRequirements)
		standard.HeadingStyles = parseHeadingStylesEnglish(parsedRequirements)
		standard.ParagraphStyles = parseParagraphStylesEnglish(parsedRequirements)
		standard.ReferenceStyle = parseReferenceStyleEnglish(parsedRequirements)
		standard.TableStyle = parseTableStyleEnglish(parsedRequirements)
		standard.FigureStyle = parseFigureStyleEnglish(parsedRequirements)
	} else {
		// 新增：处理直接结构，即您数据库中存储的格式
		// 直接从顶层解析各种样式
		standard.PageSetup = parsePageSetupFromDirectStructure(parsedRequirements)
		standard.HeadingStyles = parseHeadingStylesFromDirectStructure(parsedRequirements)
		standard.ParagraphStyles = parseParagraphStylesFromDirectStructure(parsedRequirements)
		standard.TableStyle = parseTableStyleFromDirectStructure(parsedRequirements)
		standard.FigureStyle = parseFigureStyleFromDirectStructure(parsedRequirements)
		standard.ReferenceStyle = parseReferenceStyleFromDirectStructure(parsedRequirements)
		standard.AbstractStyles = parseAbstractStylesFromDirectStructure(parsedRequirements)
	}

	// 优先使用直接在parsedRequirements中的格式要求
	// 检查是否有顶层页面设置（中文键）
	if _, ok := parsedRequirements["页面设置"].(map[string]interface{}); ok {
		// 如果直接有页面设置，说明是直接格式要求（中文键）
		standard.PageSetup = parsePageSetup(parsedRequirements)
		standard.HeadingStyles = parseHeadingStyles(parsedRequirements)
		standard.ParagraphStyles = parseParagraphStyles(parsedRequirements)
		standard.TableStyle = parseTableStyle(parsedRequirements)
		standard.FigureStyle = parseFigureStyle(parsedRequirements)
		standard.ReferenceStyle = parseReferenceStyle(parsedRequirements)
		standard.AbstractStyles = parseAbstractStyles(parsedRequirements)
	} else if formatReqs, ok := parsedRequirements["format_requirements"].(map[string]interface{}); ok {
		// 否则使用嵌套的format_requirements结构
		standard.PageSetup = parsePageSetup(formatReqs)
		standard.HeadingStyles = parseHeadingStyles(formatReqs)
		standard.ParagraphStyles = parseParagraphStyles(formatReqs)
		standard.TableStyle = parseTableStyle(formatReqs)
		standard.FigureStyle = parseFigureStyle(formatReqs)
		standard.ReferenceStyle = parseReferenceStyle(formatReqs)
		standard.AbstractStyles = parseAbstractStyles(formatReqs)
	} else if basicRequirements, ok := parsedRequirements["基本要求"].(map[string]interface{}); ok {
		// 尝试从基本要求中提取格式信息（中文键）
		standard.PageSetup = parsePageSetup(basicRequirements)
		standard.HeadingStyles = parseHeadingStyles(basicRequirements)
		standard.ParagraphStyles = parseParagraphStyles(basicRequirements)
	} else if _, ok := parsedRequirements["page_setup"].(map[string]interface{}); ok {
		// 检查是否有顶层页面设置（英文键）
		standard.PageSetup = parsePageSetupEnglish(parsedRequirements)
		standard.HeadingStyles = parseHeadingStylesEnglish(parsedRequirements)
		standard.ParagraphStyles = parseParagraphStylesEnglish(parsedRequirements)
		standard.ReferenceStyle = parseReferenceStyleEnglish(parsedRequirements)
		standard.TableStyle = parseTableStyleEnglish(parsedRequirements)
		standard.FigureStyle = parseFigureStyleEnglish(parsedRequirements)
	}

	// 设置标准名称和描述
	if name, ok := parsedRequirements["name"].(string); ok {
		standard.Name = name
	} else if name, ok := parsedRequirements["学校名称"].(string); ok {
		standard.Name = fmt.Sprintf("%s格式标准", name)
	}

	if desc, ok := parsedRequirements["description"].(string); ok {
		standard.Description = desc
	} else if docType, ok := parsedRequirements["文档类型"].(string); ok {
		standard.Description = fmt.Sprintf("%s%s", standard.Name, docType)
	} else {
		standard.Description = fmt.Sprintf("%s - 从用户上传的格式要求解析生成", standard.Name)
	}

	return standard
}

// parsePageSetup 解析页面设置
func parsePageSetup(settings map[string]interface{}) PageSetup {
	result := PageSetup{
		PaperSize:      "A4",
		MarginTop:      2.5,
		MarginBottom:   2.5,
		MarginLeft:     2.5,
		MarginRight:    2.5,
		HeaderDistance: 1.6,
		FooterDistance: 2.1,
	}

	// 检查页面设置嵌套结构
	pageSetupMap, _ := settings["页面设置"].(map[string]interface{})

	// 解析纸张大小
	if paperSize, ok := settings["纸张大小"].(string); ok {
		result.PaperSize = paperSize
	} else if paperSize, ok := pageSetupMap["纸张大小"].(string); ok {
		result.PaperSize = paperSize
	}

	// 解析页边距
	var marginsMap map[string]interface{}
	if margins, ok := settings["页边距"].(map[string]interface{}); ok {
		marginsMap = margins
	} else if pageSetupMap != nil {
		marginsMap, _ = pageSetupMap["页边距"].(map[string]interface{})
	}

	if marginsMap != nil {
		// 处理"均为"的情况
		if commonMargin, ok := marginsMap["均为"].(float64); ok {
			result.MarginTop = commonMargin
			result.MarginBottom = commonMargin
			result.MarginLeft = commonMargin
			result.MarginRight = commonMargin
		} else {
			// 单独处理各边距
			if top, ok := marginsMap["上边距"].(float64); ok {
				result.MarginTop = top
			} else if top, ok := marginsMap["上"].(float64); ok {
				result.MarginTop = top
			}

			if bottom, ok := marginsMap["下边距"].(float64); ok {
				result.MarginBottom = bottom
			} else if bottom, ok := marginsMap["下"].(float64); ok {
				result.MarginBottom = bottom
			}

			if left, ok := marginsMap["左边距"].(float64); ok {
				result.MarginLeft = left
			} else if left, ok := marginsMap["左"].(float64); ok {
				result.MarginLeft = left
			}

			if right, ok := marginsMap["右边距"].(float64); ok {
				result.MarginRight = right
			} else if right, ok := marginsMap["右"].(float64); ok {
				result.MarginRight = right
			}
		}
	}

	// 解析页眉页脚设置
	var headerFooterMap map[string]interface{}
	if headerFooter, ok := settings["页眉页脚"].(map[string]interface{}); ok {
		headerFooterMap = headerFooter
	} else if pageSetupMap != nil {
		headerFooterMap, _ = pageSetupMap["页眉页脚"].(map[string]interface{})
	}

	if headerFooterMap != nil {
		if header, ok := headerFooterMap["页眉高度"].(float64); ok {
			result.HeaderDistance = header
		}
		if footer, ok := headerFooterMap["页脚高度"].(float64); ok {
			result.FooterDistance = footer
		}
	}

	return result
}

// parseHeadingStyles 解析标题样式
func parseHeadingStyles(settings map[string]interface{}) []HeadingStyle {
	var styles []HeadingStyle

	// 支持多种标题字体结构
	var titleFonts map[string]interface{}

	// 检查直接的标题字体设置
	if fonts, ok := settings["标题字体"].(map[string]interface{}); ok {
		titleFonts = fonts
	} else if fonts, ok := settings["字体设置"].(map[string]interface{}); ok {
		// 检查字体设置下的标题字体
		titleFonts, _ = fonts["标题字体"].(map[string]interface{})
	} else if fonts, ok := settings["字体"].(map[string]interface{}); ok {
		// 检查字体下的标题字体
		titleFonts, _ = fonts["标题"].(map[string]interface{})
	}

	if titleFonts != nil {
		// 支持的标题类型映射
		headingTypes := map[string]int{
			"章标题":  1,
			"一级标题": 1,
			"节标题":  2,
			"二级标题": 2,
			"小节标题": 3,
			"三级标题": 3,
			"四级标题": 4,
			"五级标题": 5,
		}

		// 遍历所有标题类型
		for headingName, level := range headingTypes {
			if heading, ok := titleFonts[headingName].(map[string]interface{}); ok {
				styles = append(styles, HeadingStyle{
					Level:         level,
					Name:          headingName,
					FontName:      getString(heading, "字体名称", "宋体"),
					FontSize:      getFloat64(heading, "字体大小", 16-float64(level-1)),
					Bold:          getBool(heading, "粗体", true),
					Alignment:     getString(heading, "对齐方式", getDefaultHeadingAlignment(level)),
					LineSpacing:   getFloat64(heading, "行间距", 20),
					SpacingBefore: getFloat64(heading, "段前间距", 18-float64(level-1)*2),
					SpacingAfter:  getFloat64(heading, "段后间距", 12-float64(level-1)*2),
					IndentRight:   int(getFloat64(heading, "右缩进", 0)),
				})
			}
		}
	}

	// 如果没有标题样式，使用默认样式
	if len(styles) == 0 {
		// 添加默认标题样式
		styles = append(styles, HeadingStyle{
			Level:       1,
			Name:        "一级标题",
			FontName:    "宋体",
			FontSize:    16,
			Bold:        true,
			Alignment:   "center",
			LineSpacing: 20,
		})
		styles = append(styles, HeadingStyle{
			Level:       2,
			Name:        "二级标题",
			FontName:    "宋体",
			FontSize:    15,
			Bold:        true,
			Alignment:   "left",
			LineSpacing: 20,
		})
		styles = append(styles, HeadingStyle{
			Level:       3,
			Name:        "三级标题",
			FontName:    "宋体",
			FontSize:    14,
			Bold:        true,
			Alignment:   "left",
			LineSpacing: 20,
		})
	}

	return styles
}

// getDefaultHeadingAlignment 获取默认标题对齐方式
func getDefaultHeadingAlignment(level int) string {
	// 一级标题默认居中，其他默认左对齐
	if level == 1 {
		return "center"
	}
	return "left"
}

// parseParagraphStyles 解析段落样式
func parseParagraphStyles(settings map[string]interface{}) []ParagraphStyle {
	var styles []ParagraphStyle

	// 支持多种段落字体结构
	var bodyFont map[string]interface{}

	// 检查直接的正文字体设置
	if font, ok := settings["正文字体"].(map[string]interface{}); ok {
		bodyFont = font
	} else if fonts, ok := settings["字体设置"].(map[string]interface{}); ok {
		// 检查字体设置下的正文字体
		bodyFont, _ = fonts["正文字体"].(map[string]interface{})
	} else if fonts, ok := settings["字体"].(map[string]interface{}); ok {
		// 检查字体下的正文字体
		bodyFont, _ = fonts["正文"].(map[string]interface{})
	}

	// 添加正文字体样式
	if bodyFont != nil {
		styles = append(styles, ParagraphStyle{
			Name:            "正文",
			FontName:        getString(bodyFont, "字体名称", "宋体"),
			FontSize:        getFloat64(bodyFont, "字体大小", 12),
			Alignment:       getString(bodyFont, "对齐方式", "justify"),
			LineSpacing:     getFloat64(bodyFont, "行间距", 20),
			FirstLineIndent: getFloat64(bodyFont, "首行缩进", 2),
			SpacingBefore:   getFloat64(bodyFont, "段前间距", 0),
			SpacingAfter:    getFloat64(bodyFont, "段后间距", 0),
		})
	} else {
		// 添加默认正文字体样式
		styles = append(styles, ParagraphStyle{
			Name:            "正文",
			FontName:        "宋体",
			FontSize:        12,
			Alignment:       "justify",
			LineSpacing:     20,
			FirstLineIndent: 2,
			SpacingBefore:   0,
			SpacingAfter:    0,
		})
	}

	// 支持摘要、关键词等特殊段落样式
	specialParagraphs := map[string]string{
		"摘要":   "摘要",
		"关键词":  "关键词",
		"目录":   "目录",
		"参考文献": "参考文献",
		"致谢":   "致谢",
		"附录":   "附录",
	}

	for paraName, styleName := range specialParagraphs {
		if paraFont, ok := settings[paraName].(map[string]interface{}); ok {
			styles = append(styles, ParagraphStyle{
				Name:            styleName,
				FontName:        getString(paraFont, "字体名称", "宋体"),
				FontSize:        getFloat64(paraFont, "字体大小", 12),
				Alignment:       getString(paraFont, "对齐方式", "center"),
				LineSpacing:     getFloat64(paraFont, "行间距", 20),
				FirstLineIndent: getFloat64(paraFont, "首行缩进", 0),
				SpacingBefore:   getFloat64(paraFont, "段前间距", 12),
				SpacingAfter:    getFloat64(paraFont, "段后间距", 12),
			})
		}
	}

	return styles
}

// parseTableStyle 解析表格样式
func parseTableStyle(settings map[string]interface{}) TableStyle {
	result := TableStyle{
		CaptionPrefix:   "表格",
		FontName:        "宋体",
		FontSize:        10.5,
		CaptionPosition: "top",
		BorderStyle:     "all_borders",
	}

	// 支持多种表格样式结构
	var tableFont map[string]interface{}

	// 检查直接的表格字体设置
	if font, ok := settings["表格字体"].(map[string]interface{}); ok {
		tableFont = font
	} else if fonts, ok := settings["字体设置"].(map[string]interface{}); ok {
		// 检查字体设置下的表格字体
		tableFont, _ = fonts["表格字体"].(map[string]interface{})
	} else if tableSettings, ok := settings["表格设置"].(map[string]interface{}); ok {
		// 检查表格设置下的字体
		tableFont, _ = tableSettings["字体"].(map[string]interface{})
	}

	if tableFont != nil {
		// 设置表格字体属性
		result.FontName = getString(tableFont, "字体名称", "宋体")
		result.FontSize = getFloat64(tableFont, "字体大小", 10.5)
	}

	// 检查表格设置
	if tableSettings, ok := settings["表格设置"].(map[string]interface{}); ok {
		result.CaptionPrefix = getString(tableSettings, "标题前缀", "表格")
		result.CaptionPosition = getString(tableSettings, "标题位置", "top")
		result.BorderStyle = getString(tableSettings, "边框样式", "all_borders")
	}

	return result
}

// parseFigureStyle 解析图表样式
func parseFigureStyle(settings map[string]interface{}) FigureStyle {
	result := FigureStyle{
		CaptionPrefix:   "图",
		FontName:        "宋体",
		FontSize:        10.5,
		CaptionPosition: "bottom",
	}

	// 支持多种图表样式结构
	var figureFont map[string]interface{}

	// 检查直接的图片字体设置
	if font, ok := settings["图片字体"].(map[string]interface{}); ok {
		figureFont = font
	} else if fonts, ok := settings["字体设置"].(map[string]interface{}); ok {
		// 检查字体设置下的图片字体
		figureFont, _ = fonts["图片字体"].(map[string]interface{})
	} else if figureSettings, ok := settings["图表设置"].(map[string]interface{}); ok {
		// 检查图表设置下的字体
		figureFont, _ = figureSettings["字体"].(map[string]interface{})
	}

	if figureFont != nil {
		// 设置图表字体属性
		result.FontName = getString(figureFont, "字体名称", "宋体")
		result.FontSize = getFloat64(figureFont, "字体大小", 10.5)
	}

	// 检查图表设置
	if figureSettings, ok := settings["图表设置"].(map[string]interface{}); ok {
		result.CaptionPrefix = getString(figureSettings, "标题前缀", "图")
		result.CaptionPosition = getString(figureSettings, "标题位置", "bottom")
	}

	return result
}

// parseReferenceStyle 解析参考文献样式
func parseReferenceStyle(settings map[string]interface{}) ReferenceStyle {
	result := ReferenceStyle{
		Style:        "GB/T 7714",
		FontName:     "宋体",
		FontSize:     10.5,
		LineSpacing:  20,
		IndentFormat: "hanging_indent",
	}

	// 支持多种参考文献样式结构
	var referenceSettings map[string]interface{}

	// 检查直接的参考文献设置
	if ref, ok := settings["参考文献"].(map[string]interface{}); ok {
		referenceSettings = ref
	} else if fonts, ok := settings["字体设置"].(map[string]interface{}); ok {
		// 检查字体设置下的参考文献字体
		if refFont, ok := fonts["参考文献字体"].(map[string]interface{}); ok {
			result.FontName = getString(refFont, "字体名称", "宋体")
			result.FontSize = getFloat64(refFont, "字体大小", 10.5)
		}
	}

	if referenceSettings != nil {
		// 设置参考文献样式
		result.Style = getString(referenceSettings, "格式", "GB/T 7714")
		result.IndentFormat = getString(referenceSettings, "缩进格式", "hanging_indent")
		result.LineSpacing = getFloat64(referenceSettings, "行间距", 20)

		// 检查参考文献字体设置
		if font, ok := referenceSettings["字体"].(map[string]interface{}); ok {
			result.FontName = getString(font, "字体名称", "宋体")
			result.FontSize = getFloat64(font, "字体大小", 10.5)
		}
	}

	return result
}

// parseAbstractStyles 解析摘要样式
func parseAbstractStyles(settings map[string]interface{}) []AbstractStyle {
	var styles []AbstractStyle

	// 支持多种摘要样式结构
	var abstractSettings map[string]interface{}

	// 检查直接的摘要设置
	if abs, ok := settings["摘要"].(map[string]interface{}); ok {
		abstractSettings = abs
	} else if abs, ok := settings["摘要设置"].(map[string]interface{}); ok {
		abstractSettings = abs
	} else if fonts, ok := settings["字体设置"].(map[string]interface{}); ok {
		// 检查字体设置下的摘要字体
		if abstractFont, ok := fonts["摘要字体"].(map[string]interface{}); ok {
			// 添加中文摘要样式
			styles = append(styles, AbstractStyle{
				Type:           "chinese",
				Heading:        "摘要",
				FontName:       getString(abstractFont, "字体名称", "宋体"),
				FontSize:       getFloat64(abstractFont, "字体大小", 12),
				Bold:           getBool(abstractFont, "粗体", true),
				Alignment:      getString(abstractFont, "对齐方式", "center"),
				LineSpacing:    getFloat64(abstractFont, "行间距", 20),
				KeywordsPrefix: "关键词：",
			})

			// 添加英文摘要样式
			styles = append(styles, AbstractStyle{
				Type:           "english",
				Heading:        "ABSTRACT",
				FontName:       getString(abstractFont, "字体名称", "Times New Roman"),
				FontSize:       getFloat64(abstractFont, "字体大小", 12),
				Bold:           getBool(abstractFont, "粗体", true),
				Alignment:      getString(abstractFont, "对齐方式", "center"),
				LineSpacing:    getFloat64(abstractFont, "行间距", 20),
				KeywordsPrefix: "Keywords:",
			})
		}
	}

	if abstractSettings != nil {
		// 支持中文和英文摘要
		abstractTypes := map[string]string{
			"chinese": "摘要",
			"english": "ABSTRACT",
		}

		for abstractType, heading := range abstractTypes {
			if absType, ok := abstractSettings[abstractType].(map[string]interface{}); ok {
				styles = append(styles, AbstractStyle{
					Type:           abstractType,
					Heading:        getString(absType, "标题", heading),
					FontName:       getString(absType, "字体名称", getDefaultAbstractFont(abstractType)),
					FontSize:       getFloat64(absType, "字体大小", 12),
					Bold:           getBool(absType, "粗体", true),
					Alignment:      getString(absType, "对齐方式", "center"),
					LineSpacing:    getFloat64(absType, "行间距", 20),
					KeywordsPrefix: getString(absType, "关键词前缀", getDefaultKeywordsPrefix(abstractType)),
				})
			}
		}
	}

	// 如果没有摘要样式，添加默认样式
	if len(styles) == 0 {
		// 添加默认中文摘要样式
		styles = append(styles, AbstractStyle{
			Type:           "chinese",
			Heading:        "摘要",
			FontName:       "宋体",
			FontSize:       12,
			Bold:           true,
			Alignment:      "center",
			LineSpacing:    20,
			KeywordsPrefix: "关键词：",
		})

		// 添加默认英文摘要样式
		styles = append(styles, AbstractStyle{
			Type:           "english",
			Heading:        "ABSTRACT",
			FontName:       "Times New Roman",
			FontSize:       12,
			Bold:           true,
			Alignment:      "center",
			LineSpacing:    20,
			KeywordsPrefix: "Keywords:",
		})
	}

	return styles
}

// getDefaultAbstractFont 获取默认摘要字体
func getDefaultAbstractFont(abstractType string) string {
	if abstractType == "english" {
		return "Times New Roman"
	}
	return "宋体"
}

// getDefaultKeywordsPrefix 获取默认关键词前缀
func getDefaultKeywordsPrefix(abstractType string) string {
	if abstractType == "english" {
		return "Keywords:"
	}
	return "关键词："
}

// 辅助函数
func getString(m map[string]interface{}, key string, defaultValue string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultValue
}

func getFloat64(m map[string]interface{}, key string, defaultValue float64) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	// 尝试转换字符串数值
	if v, ok := m[key].(string); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

func getBool(m map[string]interface{}, key string, defaultValue bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return defaultValue
}

// parsePageSetupFromDirectStructure 从直接结构解析页面设置
func parsePageSetupFromDirectStructure(settings map[string]interface{}) PageSetup {
	result := PageSetup{
		PaperSize:      "A4",
		MarginTop:      2.5,
		MarginBottom:   2.5,
		MarginLeft:     2.5,
		MarginRight:    2.5,
		HeaderDistance: 1.6,
		FooterDistance: 2.1,
	}

	if pageSetup, ok := settings["page_setup"].(map[string]interface{}); ok {
		// 解析纸张大小
		if paperSize, ok := pageSetup["paper_size"].(string); ok {
			result.PaperSize = paperSize
		}

		// 解析页边距
		if margins, ok := pageSetup["margins"].(map[string]interface{}); ok {
			if top, ok := margins["top"].(float64); ok {
				result.MarginTop = top
			}
			if bottom, ok := margins["bottom"].(float64); ok {
				result.MarginBottom = bottom
			}
			if left, ok := margins["left"].(float64); ok {
				result.MarginLeft = left
			}
			if right, ok := margins["right"].(float64); ok {
				result.MarginRight = right
			}
		}

		// 解析页眉页脚
		if header, ok := pageSetup["header"].(map[string]interface{}); ok {
			if distance, ok := header["distance"].(float64); ok {
				result.HeaderDistance = distance
			}
		}
		if footer, ok := pageSetup["footer"].(map[string]interface{}); ok {
			if distance, ok := footer["distance"].(float64); ok {
				result.FooterDistance = distance
			}
		}

		// 解析方向和纸张大小
		if orientation, ok := pageSetup["orientation"].(string); ok {
			result.PaperSize = orientation
		}
	}

	return result
}

// parseHeadingStylesFromDirectStructure 从直接结构解析标题样式
func parseHeadingStylesFromDirectStructure(settings map[string]interface{}) []HeadingStyle {
	var styles []HeadingStyle

	if headings, ok := settings["headings"].(map[string]interface{}); ok {
		// 解析各级标题
		for levelName, levelData := range headings {
			if levelMap, ok := levelData.(map[string]interface{}); ok {
				// 确定标题级别
				level := 0
				switch levelName {
				case "level1":
					level = 1
				case "level2":
					level = 2
				case "level3":
					level = 3
				default:
					continue
				}

				// 提取样式属性
				style := HeadingStyle{
					Level:     level,
					Name:      fmt.Sprintf("%s级标题", getChineseNumber(level)),
					FontName:  getString(levelMap, "font_name", "黑体"),
					Bold:      getBool(levelMap, "bold", true),
					Alignment: getString(levelMap, "alignment", "left"),
				}

				// 解析字体大小
				if fontSize, ok := levelMap["font_size"].(string); ok {
					style.FontSize = parseFontSize(fontSize)
				} else if fontSize, ok := levelMap["font_size"].(float64); ok {
					style.FontSize = fontSize
				}

				// 解析行间距
				if lineSpace, ok := levelMap["line_space"].(string); ok {
					if lineSpace == "single" {
						style.LineSpacing = 12 // 单倍行距约等于12磅
					}
				}

				// 解析编号格式
				if numbering, ok := levelMap["numbering"].(string); ok {
					style.Name = numbering
				}

				styles = append(styles, style)
			}
		}
	}

	// 如果没有解析到任何标题样式，添加默认样式
	if len(styles) == 0 {
		styles = append(styles, HeadingStyle{
			Level:     1,
			Name:      "一级标题",
			FontName:  "黑体",
			FontSize:  16, // 三号
			Bold:      true,
			Alignment: "center",
		})
		styles = append(styles, HeadingStyle{
			Level:     2,
			Name:      "二级标题",
			FontName:  "黑体",
			FontSize:  14, // 四号
			Bold:      true,
			Alignment: "left",
		})
		styles = append(styles, HeadingStyle{
			Level:     3,
			Name:      "三级标题",
			FontName:  "黑体",
			FontSize:  12, // 小四号
			Bold:      true,
			Alignment: "left",
		})
	}

	return styles
}

// parseParagraphStylesFromDirectStructure 从直接结构解析段落样式
func parseParagraphStylesFromDirectStructure(settings map[string]interface{}) []ParagraphStyle {
	var styles []ParagraphStyle

	if body, ok := settings["body"].(map[string]interface{}); ok {
		style := ParagraphStyle{
			Name:      "正文",
			FontName:  getString(body, "font_name", "宋体"),
			Alignment: getString(body, "alignment", "justify"),
		}

		// 解析字体大小
		if fontSize, ok := body["font_size"].(string); ok {
			style.FontSize = parseFontSize(fontSize)
		} else if fontSize, ok := body["font_size"].(float64); ok {
			style.FontSize = fontSize
		}

		// 解析行间距
		if lineSpace, ok := body["line_space"].(string); ok {
			if lineSpace == "1.5" {
				style.LineSpacing = 24 // 1.5倍行距约等于24磅
			}
		} else if lineSpace, ok := body["line_space"].(float64); ok {
			style.LineSpacing = lineSpace * 16 // 假设基于1倍行距16磅计算
		}

		// 解析首行缩进
		if indent, ok := body["first_line_indent"].(string); ok {
			if indent == "2字符" {
				style.FirstLineIndent = 2
			}
		} else if indent, ok := body["first_line_indent"].(float64); ok {
			style.FirstLineIndent = indent
		}

		styles = append(styles, style)
	}

	// 如果没有解析到正文样式，添加默认样式
	if len(styles) == 0 {
		styles = append(styles, ParagraphStyle{
			Name:            "正文",
			FontName:        "宋体",
			FontSize:        12, // 小四号
			Alignment:       "justify",
			LineSpacing:     24, // 1.5倍行距
			FirstLineIndent: 2,  // 2字符
		})
	}

	return styles
}

// parseTableStyleFromDirectStructure 从直接结构解析表格样式
func parseTableStyleFromDirectStructure(settings map[string]interface{}) TableStyle {
	result := TableStyle{
		CaptionPrefix:   "表格",
		FontName:        "宋体",
		FontSize:        10.5,
		CaptionPosition: "top",
		BorderStyle:     "all_borders",
	}

	if table, ok := settings["table"].(map[string]interface{}); ok {
		// 解析表格相关设置
		if caption, ok := table["caption"].(map[string]interface{}); ok {
			result.CaptionPrefix = getString(caption, "prefix", "表格")
			result.FontName = getString(caption, "font_name", "宋体")
			if fontSize, ok := caption["font_size"].(float64); ok {
				result.FontSize = fontSize
			}
		}
	}

	return result
}

// parseFigureStyleFromDirectStructure 从直接结构解析图表样式
func parseFigureStyleFromDirectStructure(settings map[string]interface{}) FigureStyle {
	result := FigureStyle{
		CaptionPrefix:   "图",
		FontName:        "宋体",
		FontSize:        10.5,
		CaptionPosition: "bottom",
	}

	if figure, ok := settings["figure"].(map[string]interface{}); ok {
		// 解析图表相关设置
		if caption, ok := figure["caption"].(map[string]interface{}); ok {
			result.CaptionPrefix = getString(caption, "prefix", "图")
			result.FontName = getString(caption, "font_name", "宋体")
			if fontSize, ok := caption["font_size"].(float64); ok {
				result.FontSize = fontSize
			}
		}
	}

	return result
}

// parseReferenceStyleFromDirectStructure 从直接结构解析参考文献样式
func parseReferenceStyleFromDirectStructure(settings map[string]interface{}) ReferenceStyle {
	result := ReferenceStyle{
		Style:        "GB/T 7714",
		FontName:     "宋体",
		FontSize:     10.5,
		LineSpacing:  20,
		IndentFormat: "hanging_indent",
	}

	if references, ok := settings["references"].(map[string]interface{}); ok {
		// 解析参考文献相关设置
		if content, ok := references["content"].(map[string]interface{}); ok {
			result.FontName = getString(content, "font_name", "宋体")
			if fontSize, ok := content["font_size"].(float64); ok {
				result.FontSize = fontSize
			}
			if lineSpace, ok := content["line_space"].(string); ok {
				if lineSpace == "single" {
					result.LineSpacing = 16
				}
			}
		}
	}

	return result
}

// parseAbstractStylesFromDirectStructure 从直接结构解析摘要样式
func parseAbstractStylesFromDirectStructure(settings map[string]interface{}) []AbstractStyle {
	var styles []AbstractStyle

	// 解析中文摘要
	if abstract, ok := settings["abstract"].(map[string]interface{}); ok {
		if content, ok := abstract["content"].(map[string]interface{}); ok {
			styles = append(styles, AbstractStyle{
				Type:        "chinese",
				Heading:     getString(abstract, "label", "摘要：")[:len(getString(abstract, "label", "摘要："))-3], // 去掉冒号
				FontName:    getString(content, "font_name", "宋体"),
				Bold:        getBool(content, "bold", false),
				Alignment:   getString(content, "alignment", "center"),
				LineSpacing: 20,
			})
		}
	}

	// 解析英文摘要
	if engAbstract, ok := settings["english_abstract"].(map[string]interface{}); ok {
		if content, ok := engAbstract["content"].(map[string]interface{}); ok {
			styles = append(styles, AbstractStyle{
				Type:        "english",
				Heading:     "Abstract",
				FontName:    getString(content, "font_name", "Times New Roman"),
				FontSize:    12,
				Bold:        getBool(content, "bold", false),
				Alignment:   getString(content, "alignment", "left"),
				LineSpacing: 20,
			})
		}
	}

	// 如果没有解析到任何摘要样式，添加默认样式
	if len(styles) == 0 {
		styles = append(styles, AbstractStyle{
			Type:           "chinese",
			Heading:        "摘要",
			FontName:       "宋体",
			FontSize:       12,
			Bold:           true,
			Alignment:      "center",
			LineSpacing:    20,
			KeywordsPrefix: "关键词：",
		})
		styles = append(styles, AbstractStyle{
			Type:           "english",
			Heading:        "Abstract",
			FontName:       "Times New Roman",
			FontSize:       12,
			Bold:           true,
			Alignment:      "center",
			LineSpacing:    20,
			KeywordsPrefix: "Keywords:",
		})
	}

	return styles
}

// 辅助函数：将数字转换为中文
func getChineseNumber(num int) string {
	switch num {
	case 1:
		return "一"
	case 2:
		return "二"
	case 3:
		return "三"
	case 4:
		return "四"
	case 5:
		return "五"
	default:
		return fmt.Sprintf("%d", num)
	}
}

// 辅助函数：解析字体大小字符串
func parseFontSize(sizeStr string) float64 {
	switch sizeStr {
	case "小四号":
		return 12
	case "四号":
		return 14
	case "小三号":
		return 15
	case "三号":
		return 16
	case "小二号":
		return 18
	case "二号":
		return 22
	case "小一号":
		return 24
	case "一号":
		return 26
	case "小初号":
		return 36
	case "初号":
		return 42
	case "五号":
		return 10.5
	case "六号":
		return 7.5
	case "七号":
		return 5.5
	case "八号":
		return 5
	default:
		return 12 // 默认小四号
	}
}

// parsePageSetupEnglish 解析英文键名的页面设置
func parsePageSetupEnglish(settings map[string]interface{}) PageSetup {
	result := PageSetup{
		PaperSize:      "A4",
		MarginTop:      2.5,
		MarginBottom:   2.5,
		MarginLeft:     2.5,
		MarginRight:    2.5,
		HeaderDistance: 1.6,
		FooterDistance: 2.1,
	}

	if pageSetup, ok := settings["pageSetup"].(map[string]interface{}); ok {
		result.PaperSize = getString(pageSetup, "paper_size", "A4")
		result.MarginTop = getFloat64(pageSetup, "margin_top", 2.5)
		result.MarginBottom = getFloat64(pageSetup, "margin_bottom", 2.5)
		result.MarginLeft = getFloat64(pageSetup, "margin_left", 2.5)
		result.MarginRight = getFloat64(pageSetup, "margin_right", 2.5)
		result.HeaderDistance = getFloat64(pageSetup, "header_distance", 1.6)
		result.FooterDistance = getFloat64(pageSetup, "footer_distance", 2.1)
	} else if pageSetup, ok := settings["page_setup"].(map[string]interface{}); ok {
		result.PaperSize = getString(pageSetup, "paper_size", "A4")
		result.MarginTop = getFloat64(pageSetup, "margin_top", 2.5)
		result.MarginBottom = getFloat64(pageSetup, "margin_bottom", 2.5)
		result.MarginLeft = getFloat64(pageSetup, "margin_left", 2.5)
		result.MarginRight = getFloat64(pageSetup, "margin_right", 2.5)
		result.HeaderDistance = getFloat64(pageSetup, "header_distance", 1.6)
		result.FooterDistance = getFloat64(pageSetup, "footer_distance", 2.1)
	}

	return result
}

// parseHeadingStylesEnglish 解析英文键名的标题样式
func parseHeadingStylesEnglish(settings map[string]interface{}) []HeadingStyle {
	var styles []HeadingStyle

	var headingStyles []interface{}
	if hs, ok := settings["heading_styles"].([]interface{}); ok {
		headingStyles = hs
	} else if hs, ok := settings["headingStyles"].([]interface{}); ok {
		headingStyles = hs
	}

	if headingStyles != nil {
		for _, s := range headingStyles {
			if style, ok := s.(map[string]interface{}); ok {
				styles = append(styles, HeadingStyle{
					Level:         int(getFloat64(style, "level", 1)),
					Name:          getString(style, "name", ""),
					FontName:      getString(style, "font_name", "宋体"),
					FontSize:      getFloat64(style, "font_size", 16),
					Bold:          getBool(style, "bold", true),
					Alignment:     getString(style, "alignment", "left"),
					SpacingBefore: getFloat64(style, "spacing_before", 12),
					SpacingAfter:  getFloat64(style, "spacing_after", 12),
					LineSpacing:   getFloat64(style, "line_spacing", 20),
					IndentRight:   int(getFloat64(style, "indent_right", 0)),
				})
			}
		}
	}

	return styles
}

// parseParagraphStylesEnglish 解析英文键名的段落样式
func parseParagraphStylesEnglish(settings map[string]interface{}) []ParagraphStyle {
	var styles []ParagraphStyle

	var paraStyles []interface{}
	if ps, ok := settings["paragraph_styles"].([]interface{}); ok {
		paraStyles = ps
	} else if ps, ok := settings["paragraphStyles"].([]interface{}); ok {
		paraStyles = ps
	}

	if paraStyles != nil {
		for _, s := range paraStyles {
			if style, ok := s.(map[string]interface{}); ok {
				styles = append(styles, ParagraphStyle{
					Name:            getString(style, "name", "正文"),
					FontName:        getString(style, "font_name", "宋体"),
					FontSize:        getFloat64(style, "font_size", 12),
					Alignment:       getString(style, "alignment", "justify"),
					LineSpacing:     getFloat64(style, "line_spacing", 20),
					FirstLineIndent: getFloat64(style, "first_line_indent", 2),
					SpacingBefore:   getFloat64(style, "spacing_before", 0),
					SpacingAfter:    getFloat64(style, "spacing_after", 0),
				})
			}
		}
	}

	return styles
}

// parseReferenceStyleEnglish 解析英文键名的参考文献样式
func parseReferenceStyleEnglish(settings map[string]interface{}) ReferenceStyle {
	result := ReferenceStyle{
		Style:        "GB/T 7714",
		FontName:     "宋体",
		FontSize:     10.5,
		LineSpacing:  20,
		IndentFormat: "hanging_indent",
	}

	var refStyle map[string]interface{}
	if rs, ok := settings["reference_style"].(map[string]interface{}); ok {
		refStyle = rs
	} else if rs, ok := settings["referenceStyle"].(map[string]interface{}); ok {
		refStyle = rs
	}

	if refStyle != nil {
		result.Style = getString(refStyle, "style", "GB/T 7714")
		result.FontName = getString(refStyle, "font_name", "宋体")
		result.FontSize = getFloat64(refStyle, "font_size", 10.5)
		result.LineSpacing = getFloat64(refStyle, "line_spacing", 20)
		result.IndentFormat = getString(refStyle, "indent_format", "hanging_indent")
	}

	return result
}

// parseTableStyleEnglish 解析英文键名的表格样式
func parseTableStyleEnglish(settings map[string]interface{}) TableStyle {
	result := TableStyle{
		CaptionPrefix:   "Table",
		FontName:        "宋体",
		FontSize:        10.5,
		CaptionPosition: "top",
		BorderStyle:     "single",
	}

	var tableStyle map[string]interface{}
	if ts, ok := settings["table_style"].(map[string]interface{}); ok {
		tableStyle = ts
	} else if ts, ok := settings["tableStyle"].(map[string]interface{}); ok {
		tableStyle = ts
	}

	if tableStyle != nil {
		result.CaptionPrefix = getString(tableStyle, "caption_prefix", "Table")
		result.FontName = getString(tableStyle, "font_name", "宋体")
		result.FontSize = getFloat64(tableStyle, "font_size", 10.5)
		result.CaptionPosition = getString(tableStyle, "caption_position", "top")
		result.BorderStyle = getString(tableStyle, "border_style", "single")
	}

	return result
}

// parseFigureStyleEnglish 解析英文键名的图表样式
func parseFigureStyleEnglish(settings map[string]interface{}) FigureStyle {
	result := FigureStyle{
		CaptionPrefix:   "Figure",
		FontName:        "宋体",
		FontSize:        10.5,
		CaptionPosition: "bottom",
	}

	var figureStyle map[string]interface{}
	if fs, ok := settings["figure_style"].(map[string]interface{}); ok {
		figureStyle = fs
	} else if fs, ok := settings["figureStyle"].(map[string]interface{}); ok {
		figureStyle = fs
	}

	if figureStyle != nil {
		result.CaptionPrefix = getString(figureStyle, "caption_prefix", "Figure")
		result.FontName = getString(figureStyle, "font_name", "宋体")
		result.FontSize = getFloat64(figureStyle, "font_size", 10.5)
		result.CaptionPosition = getString(figureStyle, "caption_position", "bottom")
	}

	return result
}

type CorrectionType string

const (
	CorrectionTypePageSetup    CorrectionType = "page_setup"    // 页面设置修正
	CorrectionTypeHeaderFooter CorrectionType = "header_footer" // 页眉页脚修正
	CorrectionTypeFont         CorrectionType = "font"          // 字体修正
	CorrectionTypeSpacing      CorrectionType = "spacing"       // 间距修正
	CorrectionTypeHeading      CorrectionType = "heading"       // 标题修正
	CorrectionTypeTable        CorrectionType = "table"         // 表格修正
	CorrectionTypeFigure       CorrectionType = "figure"        // 图表修正
)

// ParseFormatText 智能解析格式要求文本
// 将用户粘贴的格式说明文本解析为结构化的格式要求
func ParseFormatText(text string) map[string]interface{} {
	result := make(map[string]interface{})

	// 1. 解析页面设置
	result["页面设置"] = parsePageSetupFromText(text)

	// 2. 解析正文字体
	result["body"] = parseBodyFormatFromText(text)

	// 3. 解析标题字体
	result["headings"] = parseHeadingsFromText(text)

	// 4. 解析摘要格式
	result["abstract"] = parseAbstractFromText(text)

	// 5. 解析关键词格式
	result["keywords"] = parseKeywordsFromText(text)

	// 6. 解析参考文献格式
	result["references"] = parseReferencesFromText(text)

	return result
}

// parsePageSetupFromText 从文本中解析页面设置
func parsePageSetupFromText(text string) map[string]interface{} {
	setup := make(map[string]interface{})

	// 解析页边距
	marginPatterns := []struct {
		pattern string
		key     string
	}{
		{`上[、，,\s]+下[、，,\s]+左[、，,\s]+右[均为]*(\d+\.?\d*)\s*厘米`, "margin_all"},
		{`上[、，,\s]+(\d+\.?\d*)\s*厘米`, "margin_top"},
		{`下[、，,\s]+(\d+\.?\d*)\s*厘米`, "margin_bottom"},
		{`左[、，,\s]+(\d+\.?\d*)\s*厘米`, "margin_left"},
		{`右[、，,\s]+(\d+\.?\d*)\s*厘米`, "margin_right"},
	}

	for _, p := range marginPatterns {
		re := regexp.MustCompile(p.pattern)
		matches := re.FindStringSubmatch(text)
		if len(matches) > 1 {
			val, _ := strconv.ParseFloat(matches[1], 64)
			if p.key == "margin_all" {
				setup["margin_top"] = val
				setup["margin_bottom"] = val
				setup["margin_left"] = val
				setup["margin_right"] = val
			} else {
				setup[p.key] = val
			}
		}
	}

	// 如果没有找到明确的边距，尝试通用模式
	if len(setup) == 0 {
		// 匹配 "2.5厘米" 这种模式
		marginRegex := regexp.MustCompile(`(\d+\.?\d*)\s*厘米`)
		if marginRegex.MatchString(text) {
			// 默认所有边距为2.5cm（如果文本中提到的话）
			if strings.Contains(text, "2.5") {
				setup["margin_top"] = 2.5
				setup["margin_bottom"] = 2.5
				setup["margin_left"] = 2.5
				setup["margin_right"] = 2.5
			}
		}
	}

	// 解析页眉页脚距离
	headerRegex := regexp.MustCompile(`页眉[：:\s]+(\d+\.?\d*)\s*厘米`)
	if match := headerRegex.FindStringSubmatch(text); len(match) > 1 {
		if val, _ := strconv.ParseFloat(match[1], 64); val > 0 {
			setup["header_distance"] = val
		}
	}

	footerRegex := regexp.MustCompile(`页脚[：:\s]+(\d+\.?\d*)\s*厘米`)
	if match := footerRegex.FindStringSubmatch(text); len(match) > 1 {
		if val, _ := strconv.ParseFloat(match[1], 64); val > 0 {
			setup["footer_distance"] = val
		}
	}

	return setup
}

// parseBodyFormatFromText 从文本中解析正文字体格式
func parseBodyFormatFromText(text string) map[string]interface{} {
	format := make(map[string]interface{})

	// 解析正文字体
	bodyFontPatterns := []string{
		`正文[字样]*[：:\s]+([^，。,；\n]+?)[\s\n]`,
		`正文[为用是]+([^，。,；\n]+?)字体`,
	}

	fontName := ""
	for _, pattern := range bodyFontPatterns {
		re := regexp.MustCompile(pattern)
		match := re.FindStringSubmatch(text)
		if len(match) > 1 {
			fontText := strings.TrimSpace(match[1])
			// 提取字体名称
			fonts := []string{"宋体", "黑体", "仿宋", "楷体", "Times New Roman", "Arial"}
			for _, f := range fonts {
				if strings.Contains(fontText, f) {
					fontName = f
					break
				}
			}
			break
		}
	}

	if fontName == "" {
		// 尝试从整段文本中查找
		if strings.Contains(text, "小四号宋体") || strings.Contains(text, "小四宋体") {
			fontName = "宋体"
		}
	}
	format["font_name"] = fontName

	// 解析字号
	size := 12.0 // 默认小四号
	if strings.Contains(text, "小四号") || strings.Contains(text, "小四") {
		size = 12.0
	} else if strings.Contains(text, "五号") {
		size = 10.5
	} else if strings.Contains(text, "四号") {
		size = 14.0
	} else if strings.Contains(text, "三号") {
		size = 16.0
	}
	format["font_size"] = fmt.Sprintf("%.1f", size)

	// 解析行间距
	lineSpace := "1.5"
	if strings.Contains(text, "固定值20磅") || strings.Contains(text, "固定值 20 磅") {
		lineSpace = "fixed_20_pt"
	} else if strings.Contains(text, "单倍行距") {
		lineSpace = "single"
	} else if strings.Contains(text, "1.5倍行距") {
		lineSpace = "1.5"
	} else if strings.Contains(text, "2倍行距") || strings.Contains(text, "双倍行距") {
		lineSpace = "2"
	}
	format["line_space"] = lineSpace

	// 解析对齐方式
	alignment := "justify" // 默认两端对齐
	if strings.Contains(text, "居中") && !strings.Contains(text, "居中对齐") {
		alignment = "center"
	} else if strings.Contains(text, "左对齐") {
		alignment = "left"
	} else if strings.Contains(text, "右对齐") {
		alignment = "right"
	}
	format["alignment"] = alignment

	// 解析首行缩进
	indent := "2字符"
	if strings.Contains(text, "首行缩进") || strings.Contains(text, "首行缩") {
		if strings.Contains(text, "2字符") || strings.Contains(text, "2字") {
			indent = "2字符"
		}
	}
	format["first_line_indent"] = indent

	return format
}

// parseHeadingsFromText 从文本中解析标题格式
func parseHeadingsFromText(text string) map[string]interface{} {
	headings := make(map[string]interface{})

	// 一级标题
	level1 := make(map[string]interface{})
	if strings.Contains(text, "一级标题") || strings.Contains(text, "章标题") {
		// 解析一级标题字体
		if strings.Contains(text, "一级标题") && strings.Contains(text, "黑体") {
			level1["font_name"] = "黑体"
		}
		if strings.Contains(text, "一级标题") && strings.Contains(text, "三号") {
			level1["font_size"] = "三号"
		}
		if strings.Contains(text, "一级标题") && strings.Contains(text, "居中") {
			level1["alignment"] = "center"
		}
		// 行间距
		if strings.Contains(text, "一级标题") && strings.Contains(text, "固定值20磅") {
			level1["line_space"] = "fixed_20_pt"
		}
	}
	// 检查是否有一级标题（章）的完整描述
	if strings.Contains(text, "一级标题（章）") || strings.Contains(text, "章）：") {
		level1["font_name"] = "黑体"
		level1["font_size"] = "三号"
		level1["alignment"] = "center"
		level1["line_space"] = "fixed_20_pt"
		level1["bold"] = true
	}
	headings["level1"] = level1

	// 二级标题
	level2 := make(map[string]interface{})
	if strings.Contains(text, "二级标题（节）") || strings.Contains(text, "节）：") {
		level2["font_name"] = "黑体"
		level2["font_size"] = "小三号"
		level2["alignment"] = "left"
		level2["line_space"] = "fixed_20_pt"
		level2["bold"] = true
	}
	headings["level2"] = level2

	// 三级标题
	level3 := make(map[string]interface{})
	if strings.Contains(text, "三级标题（条）") || strings.Contains(text, "条）：") {
		level3["font_name"] = "黑体"
		level3["font_size"] = "四号"
		level3["alignment"] = "left"
		level3["line_space"] = "fixed_20_pt"
		level3["bold"] = true
	}
	headings["level3"] = level3

	return headings
}

// parseAbstractFromText 从文本中解析摘要格式
func parseAbstractFromText(text string) map[string]interface{} {
	abstract := make(map[string]interface{})
	content := make(map[string]interface{})

	// 摘要通常使用与正文相同的格式，但字号可能不同
	content["font_name"] = "宋体"
	content["font_size"] = "小四号"
	content["alignment"] = "justify"

	abstract["content"] = content
	abstract["title"] = map[string]interface{}{
		"font_name": "黑体",
		"font_size": "三号",
		"alignment": "center",
	}

	return abstract
}

// parseKeywordsFromText 从文本中解析关键词格式
func parseKeywordsFromText(text string) map[string]interface{} {
	keywords := make(map[string]interface{})
	content := make(map[string]interface{})

	content["font_name"] = "宋体"
	content["font_size"] = "小四号"
	content["alignment"] = "left" // 关键词左对齐

	keywords["content"] = content
	keywords["separator"] = "分号"

	return keywords
}

// parseReferencesFromText 从文本中解析参考文献格式
func parseReferencesFromText(text string) map[string]interface{} {
	refs := make(map[string]interface{})
	content := make(map[string]interface{})

	// 参考文献通常使用五号宋体
	content["font_name"] = "宋体"
	content["font_size"] = "五号"
	content["alignment"] = "left"

	// 行距
	if strings.Contains(text, "参考文献") && strings.Contains(text, "行距") {
		if strings.Contains(text, "固定值") {
			content["line_space"] = "fixed_20_pt"
		}
	}

	// 悬挂缩进
	if strings.Contains(text, "参考文献") && strings.Contains(text, "悬挂") {
		content["first_line_indent"] = "2字符"
	}

	refs["content"] = content
	return refs
}
