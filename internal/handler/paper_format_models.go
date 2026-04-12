package handler

import (
	"mime/multipart"

	"github.com/paper-format-checker/backend/pkg/formatchecker"
)

// ParsedFormatRequirements 解析后的格式要求
type ParsedFormatRequirements struct {
	Institution       string            `json:"institution"`        // 学校名称
	DocumentType      string            `json:"document_type"`      // 文档类型
	BasicRequirements []string          `json:"basic_requirements"` // 基本要求
	PageSetup         PageSetup         `json:"page_setup"`         // 页面设置
	FontSettings      FontSettings      `json:"font_settings"`      // 字体设置
	Structure         DocumentStructure `json:"structure"`          // 文档结构
	CitationRules     CitationRules     `json:"citation_rules"`     // 引用规则
	AppendixRules     AppendixRules     `json:"appendix_rules"`     // 附录规则
}

// PageSetup 页面设置
type PageSetup struct {
	PaperSize    string       `json:"paper_size"`    // 纸张大小
	Orientation  string       `json:"orientation"`   // 页面方向
	Margins      Margins      `json:"margins"`       // 页边距
	HeaderFooter HeaderFooter `json:"header_footer"` // 页眉页脚
	PrintingSide string       `json:"printing_side"` // 打印面
}

// Margins 页边距
type Margins struct {
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
}

// HeaderFooter 页眉页脚
type HeaderFooter struct {
	HeaderHeight float64 `json:"header_height"` // 页眉高度
	FooterHeight float64 `json:"footer_height"` // 页脚高度
	HeaderLeft   string  `json:"header_left"`   // 页眉左侧内容
	HeaderRight  string  `json:"header_right"`  // 页眉右侧内容
	HeaderCenter string  `json:"header_center"` // 页眉居中内容
}

// FontSettings 字体设置
type FontSettings struct {
	MainFont      MainFont      `json:"main_font"`      // 正文字体
	TitleFont     TitleFont     `json:"title_font"`     // 标题字体
	AbstractFont  AbstractFont  `json:"abstract_font"`  // 摘要字体
	DirectoryFont DirectoryFont `json:"directory_font"` // 目录字体
	TableFont     TableFont     `json:"table_font"`     // 表格字体
	FigureFont    FigureFont    `json:"figure_font"`    // 图片字体
}

// MainFont 正文字体
type MainFont struct {
	FontName    string  `json:"font_name"`
	FontSize    float64 `json:"font_size"`
	LineSpacing float64 `json:"line_spacing"`
}

// TitleFont 标题字体
type TitleFont struct {
	ChapterTitle    ChapterTitle    `json:"chapter_title"`    // 章标题
	SectionTitle    SectionTitle    `json:"section_title"`    // 节标题
	SubsectionTitle SubsectionTitle `json:"subsection_title"` // 小节标题
}

// ChapterTitle 章标题
type ChapterTitle struct {
	FontName  string  `json:"font_name"`
	FontSize  float64 `json:"font_size"`
	Alignment string  `json:"alignment"`
}

// SectionTitle 节标题
type SectionTitle struct {
	FontName  string  `json:"font_name"`
	FontSize  float64 `json:"font_size"`
	Alignment string  `json:"alignment"`
}

// SubsectionTitle 小节标题
type SubsectionTitle struct {
	FontName  string  `json:"font_name"`
	FontSize  float64 `json:"font_size"`
	Alignment string  `json:"alignment"`
}

// AbstractFont 摘要字体
type AbstractFont struct {
	FontName string  `json:"font_name"`
	FontSize float64 `json:"font_size"`
}

// DirectoryFont 目录字体
type DirectoryFont struct {
	FontName string  `json:"font_name"`
	FontSize float64 `json:"font_size"`
}

// TableFont 表格字体
type TableFont struct {
	FontName string  `json:"font_name"`
	FontSize float64 `json:"font_size"`
}

// FigureFont 图片字体
type FigureFont struct {
	FontName string  `json:"font_name"`
	FontSize float64 `json:"font_size"`
}

// DocumentStructure 文档结构
type DocumentStructure struct {
	FrontMatter FrontMatter `json:"front_matter"` // 前置部分
	MainBody    MainBody    `json:"main_body"`    // 主体部分
	BackMatter  BackMatter  `json:"back_matter"`  // 后置部分
}

// FrontMatter 前置部分
type FrontMatter struct {
	CoverPage          bool `json:"cover_page"`          // 封面
	CopyrightStatement bool `json:"copyright_statement"` // 版权声明
	Abstract           bool `json:"abstract"`            // 摘要
	TableOfContents    bool `json:"table_of_contents"`   // 目录
	ListOfFigures      bool `json:"list_of_figures"`     // 插图清单
	ListOfTables       bool `json:"list_of_tables"`      // 表格清单
}

// MainBody 主体部分
type MainBody struct {
	Introduction bool `json:"introduction"` // 引言
	MainContent  bool `json:"main_content"` // 正文
	Conclusion   bool `json:"conclusion"`   // 结论
}

// BackMatter 后置部分
type BackMatter struct {
	References       bool `json:"references"`       // 参考文献
	Acknowledgements bool `json:"acknowledgements"` // 致谢
	Appendices       bool `json:"appendices"`       // 附录
}

// CitationRules 引用规则
type CitationRules struct {
	ReferenceFormat string   `json:"reference_format"` // 参考文献格式
	ReferenceTypes  []string `json:"reference_types"`  // 参考文献类型
}

// AppendixRules 附录规则
type AppendixRules struct {
	AppendixFormat string   `json:"appendix_format"` // 附录格式
	AttachmentList []string `json:"attachment_list"` // 附件列表
}

// CQCECFormatRequest 重庆工程学院格式处理请求结构体
type CQCECFormatRequest struct {
	File *multipart.FileHeader `form:"file" binding:"required"`
}

// CQCECFormatResponse 重庆工程学院格式处理响应结构体
type CQCECFormatResponse struct {
	Success       bool                        `json:"success"`
	Message       string                      `json:"message"`
	Issues        []formatchecker.FormatIssue `json:"issues,omitempty"`
	Corrections   []formatchecker.Correction  `json:"corrections,omitempty"`
	CorrectedFile string                      `json:"corrected_file,omitempty"`
}

// ApplySelectedDiffsRequest 请求体
type ApplySelectedDiffsRequest struct {
	// 用户选择"接受修改"的段落索引列表；空列表表示全部接受
	AcceptedParaIndices []int `json:"accepted_para_indices"`
}
