package fileprocessor

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/ofc/sharedTypes"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

// BodyTextSpec 正文格式规格 — 单一数据源，所有正文格式化必须使用此结构体。
// 对标 ParagraphFormatSpec，但语义更聚焦于"正文"这一特定格式类型。
type BodyTextSpec struct {
	FontEastAsia   string  // 中文字体，如 "宋体"
	FontAscii      string  // 西文字体，如 "Times New Roman"
	FontSizePt     float64 // 字号（磅），如 12.0 = 小四
	LineSpacingPt  float64 // 行距（磅），如 20.0 = 固定 20 磅
	FirstLineChars float64 // 首行缩进字符数，如 2.0
	Alignment      string  // 对齐方式: "justify", "left", "center", "right"
}

// DefaultBodyTextSpec 返回本科毕业论文标准正文格式规格。
func DefaultBodyTextSpec() BodyTextSpec {
	return BodyTextSpec{
		FontEastAsia:   "宋体",
		FontAscii:      "Times New Roman",
		FontSizePt:     12.0,
		LineSpacingPt:  20.0,
		FirstLineChars: 2.0,
		Alignment:      "justify",
	}
}

// FormatBodyParagraph 格式化单个正文段落。
// 这是正文格式化的唯一底层入口，所有调用方必须通过此函数。
func FormatBodyParagraph(para document.Paragraph, spec BodyTextSpec) {
	// ── 段落属性 ──
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}

	// 对齐方式
	switch spec.Alignment {
	case "center":
		pPr.Jc = wml.NewCT_Jc()
		pPr.Jc.ValAttr = wml.ST_JcCenter
	case "left":
		pPr.Jc = wml.NewCT_Jc()
		pPr.Jc.ValAttr = wml.ST_JcLeft
	case "right":
		pPr.Jc = wml.NewCT_Jc()
		pPr.Jc.ValAttr = wml.ST_JcRight
	case "justify", "":
		pPr.Jc = wml.NewCT_Jc()
		pPr.Jc.ValAttr = wml.ST_JcBoth
	}

	// 行距
	if spec.LineSpacingPt > 0 {
		if pPr.Spacing == nil {
			pPr.Spacing = wml.NewCT_Spacing()
		}
		lineVal := int64(spec.LineSpacingPt * 20) // 磅 → twips
		pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
		pPr.Spacing.LineAttr.Int64 = &lineVal
		pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleExact
	}

	// 首行缩进（字符数 × 字号pt × 20 = twips）
	if spec.FirstLineChars > 0 {
		if pPr.Ind == nil {
			pPr.Ind = wml.NewCT_Ind()
		}
		fl := uint64(spec.FirstLineChars * spec.FontSizePt * 20)
		pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &fl
	}

	// ── Run 属性 ──
	halfPt := uint64(spec.FontSizePt * 2)
	for _, run := range para.Runs() {
		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}

		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		if spec.FontEastAsia != "" {
			eastAsiaVal := spec.FontEastAsia
			rPr.RFonts.EastAsiaAttr = &eastAsiaVal
		}
		if spec.FontAscii != "" {
			asciiVal := spec.FontAscii
			rPr.RFonts.AsciiAttr = &asciiVal
			rPr.RFonts.HAnsiAttr = &asciiVal
		}

		if halfPt > 0 {
			rPr.Sz = wml.NewCT_HpsMeasure()
			rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
			rPr.SzCs = wml.NewCT_HpsMeasure()
			rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt
		}

		// 正文不加粗
		rPr.B = nil
	}
}

// FormatBodyParagraphs 批量格式化正文段落。
func FormatBodyParagraphs(paragraphs []document.Paragraph, spec BodyTextSpec) {
	for i := range paragraphs {
		FormatBodyParagraph(paragraphs[i], spec)
	}
}

// ExtractBodyTextSpec 从格式规则 map 中提取正文格式规格。
// 这是从旧版 rules map 到新 BodyTextSpec 的桥接函数。
func ExtractBodyTextSpec(rules map[string]interface{}) (BodyTextSpec, error) {
	spec := DefaultBodyTextSpec()

	bodyRules, ok := rules["body"].(map[string]interface{})
	if !ok {
		return spec, fmt.Errorf("未找到 body 规则，可用键: %v", getMapKeys(rules))
	}

	if v, ok := bodyRules["font_name"].(string); ok && v != "" {
		spec.FontEastAsia = v
	}
	if v, ok := bodyRules["font_name_east"].(string); ok && v != "" {
		spec.FontEastAsia = v
	}
	if v, ok := bodyRules["font_name_ascii"].(string); ok && v != "" {
		spec.FontAscii = v
	}

	if v, ok := bodyRules["font_size"].(string); ok && v != "" {
		spec.FontSizePt = parseFontSizeStatic(v)
	} else if v, ok := bodyRules["font_size_pt"].(float64); ok && v > 0 {
		spec.FontSizePt = v
	}

	if v, ok := bodyRules["line_space"].(float64); ok && v > 0 {
		spec.LineSpacingPt = v
	} else if v, ok := bodyRules["line_space"].(string); ok && v != "" {
		spec.LineSpacingPt = parseLineSpacingStatic(v)
	}

	if v, ok := bodyRules["first_line_indent"].(string); ok && v != "" {
		spec.FirstLineChars = parseFirstLineIndentStatic(v)
	} else if v, ok := bodyRules["first_line_indent"].(float64); ok && v > 0 {
		spec.FirstLineChars = v
	}

	if v, ok := bodyRules["alignment"].(string); ok && v != "" {
		spec.Alignment = v
	}

	return spec, nil
}

// BodyTextSpecFromParagraphFormatSpec 从 ParagraphFormatSpec 转换。
// ParagraphFormatSpec 使用 OOXML 原生单位（half-pt, twips），
// BodyTextSpec 使用人类可读单位（pt, 字符数）。
func BodyTextSpecFromParagraphFormatSpec(ps *ParagraphFormatSpec) BodyTextSpec {
	if ps == nil {
		return DefaultBodyTextSpec()
	}
	spec := BodyTextSpec{
		FontEastAsia:   ps.FontEastAsia,
		FontAscii:      ps.FontAscii,
		FontSizePt:     ps.FontSizePt(),
		Alignment:      "justify",
		FirstLineChars: 0,
	}
	if ps.LineSpacingVal > 0 {
		spec.LineSpacingPt = float64(ps.LineSpacingVal) / 20.0
	}
	if ps.FirstLineIndent > 0 {
		spec.FirstLineChars = float64(ps.FirstLineIndent) / (spec.FontSizePt * 20)
	}
	if ps.AlignmentSet {
		switch ps.Alignment {
		case wml.ST_JcCenter:
			spec.Alignment = "center"
		case wml.ST_JcLeft:
			spec.Alignment = "left"
		case wml.ST_JcRight:
			spec.Alignment = "right"
		case wml.ST_JcBoth:
			spec.Alignment = "justify"
		}
	}
	return spec
}

// FontSizeString 返回中文字号描述，如 "小四"。
func (s BodyTextSpec) FontSizeString() string {
	switch {
	case s.FontSizePt >= 15.5:
		return "小三"
	case s.FontSizePt >= 13.5:
		return "四号"
	case s.FontSizePt >= 11.5:
		return "小四"
	case s.FontSizePt >= 10:
		return "五号"
	default:
		return fmt.Sprintf("%.1fpt", s.FontSizePt)
	}
}

// String 返回人类可读的规格描述。
func (s BodyTextSpec) String() string {
	return fmt.Sprintf("正文格式: 字体=%s/%s, 字号=%s(%.1fpt), 行距=%.1fpt, 首行缩进=%.0f字符, 对齐=%s",
		s.FontEastAsia, s.FontAscii, s.FontSizeString(), s.FontSizePt,
		s.LineSpacingPt, s.FirstLineChars, s.Alignment)
}

// ── 静态辅助函数（不依赖 EnhancedProcessor） ──

var chineseFontSizeMap = map[string]float64{
	"初号": 42, "初": 42,
	"小初号": 36, "小初": 36,
	"一号": 26, "一": 26,
	"小一号": 24, "小一": 24,
	"二号": 22, "二": 22,
	"小二号": 18, "小二": 18,
	"三号": 16, "三": 16,
	"小三号": 15, "小三": 15,
	"四号": 14, "四": 14,
	"小四号": 12, "小四": 12,
	"五号": 10.5, "五": 10.5,
	"小五号": 9, "小五": 9,
	"六号": 7.5, "六": 7.5,
	"小六号": 6.5, "小六": 6.5,
	"七号": 5.5, "七": 5.5,
	"八号": 5, "八": 5,
}

func parseFontSizeStatic(size string) float64 {
	size = strings.TrimSpace(size)
	for _, suffix := range []string{"号", "pt", "磅"} {
		size = strings.TrimSuffix(size, suffix)
	}
	size = strings.TrimSpace(size)

	if val, ok := chineseFontSizeMap[size]; ok {
		return val
	}
	if val, err := strconv.ParseFloat(size, 64); err == nil {
		return val
	}
	log.Printf("[BodyTextSpec] 无法识别的字号: %q", size)
	return 0
}

func parseLineSpacingStatic(lineSpace string) float64 {
	lineSpace = strings.TrimSpace(lineSpace)
	if strings.HasPrefix(lineSpace, "fixed_") {
		lineSpace = strings.TrimPrefix(lineSpace, "fixed_")
	}
	for _, suffix := range []string{"磅", "pt"} {
		if strings.HasSuffix(lineSpace, suffix) {
			lineSpace = strings.TrimSuffix(lineSpace, suffix)
			break
		}
	}
	lineSpace = strings.TrimSpace(lineSpace)
	if val, err := strconv.ParseFloat(lineSpace, 64); err == nil {
		return val
	}
	return 0
}

func parseFirstLineIndentStatic(indent string) float64 {
	indent = strings.TrimSpace(indent)
	if strings.HasSuffix(indent, "字符") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(indent, "字符"), 64); err == nil {
			return val
		}
	}
	if val, err := strconv.ParseFloat(indent, 64); err == nil {
		return val
	}
	return 0
}
