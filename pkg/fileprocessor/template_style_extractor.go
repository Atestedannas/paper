package fileprocessor

import (
	"log"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

// TemplateStyleExtractor 从模板 styles.xml 中的 Named Style 定义直接提取格式规范
// 与 TemplateFormatLoader（段落内容采样）相比，优势：
//  1. 无采样噪声：样式定义本身就是权威的格式来源
//  2. 处理继承：通过 basedOn 链完整展开父样式属性
//  3. 覆盖范围广：Normal/Heading1/2/3 覆盖论文绝大多数段落类型
type TemplateStyleExtractor struct{}

// NewTemplateStyleExtractor 创建样式提取器
func NewTemplateStyleExtractor() *TemplateStyleExtractor {
	return &TemplateStyleExtractor{}
}

// ExtractFromTemplate 从模板中提取各类段落的格式规范
// 返回 category → ParagraphFormatSpec 映射
func (e *TemplateStyleExtractor) ExtractFromTemplate(templatePath string) (map[string]ParagraphFormatSpec, error) {
	doc, err := document.Open(templatePath)
	if err != nil {
		return nil, err
	}
	defer doc.Close()

	// 构建样式ID和名称两套索引（大小写不敏感）
	byID := make(map[string]*wml.CT_Style)
	byName := make(map[string]*wml.CT_Style)

	for _, s := range doc.Styles.Styles() {
		ctStyle := s.X()
		if ctStyle == nil {
			continue
		}
		id := strings.ToLower(s.StyleID())
		if id != "" {
			byID[id] = ctStyle
		}
		name := strings.ToLower(s.Name())
		if name != "" {
			byName[name] = ctStyle
		}
	}
	log.Printf("[样式提取] 模板共有 %d 个命名样式", len(byID))

	// 样式名称→论文区段类型映射（中英文兼容）
	mappings := []struct {
		names    []string
		category string
	}{
		{[]string{"normal", "正文", "default", "body text", "bodytext"}, "body"},
		{[]string{"heading 1", "标题 1", "标题1", "heading1"}, "heading_1"},
		{[]string{"heading 2", "标题 2", "标题2", "heading2"}, "heading_2"},
		{[]string{"heading 3", "标题 3", "标题3", "heading3"}, "heading_3"},
	}

	specs := make(map[string]ParagraphFormatSpec)
	for _, m := range mappings {
		var found *wml.CT_Style
		for _, name := range m.names {
			key := strings.ToLower(name)
			if s, ok := byName[key]; ok {
				found = s
				break
			}
			if s, ok := byID[key]; ok {
				found = s
				break
			}
		}
		if found == nil {
			log.Printf("[样式提取] 未找到 %s 对应样式", m.category)
			continue
		}

		spec := e.extractWithInheritance(found, byID, byName, 0)
		if !spec.IsEmpty() {
			specs[m.category] = spec
			log.Printf("[样式提取] ✓ %s: font=%q ascii=%q size=%.1fpt bold=%v lineSpacing=%d firstLine=%d",
				m.category, spec.FontEastAsia, spec.FontAscii,
				spec.FontSizePt(), spec.Bold, spec.LineSpacingVal, spec.FirstLineIndent)
		} else {
			log.Printf("[样式提取] ✗ %s: 提取结果为空（样式可能未定义字体/字号）", m.category)
		}
	}

	return specs, nil
}

// extractWithInheritance 递归展开样式继承链，提取完整格式规范
// depth 防止循环继承（最深5层）
func (e *TemplateStyleExtractor) extractWithInheritance(
	style *wml.CT_Style,
	byID map[string]*wml.CT_Style,
	byName map[string]*wml.CT_Style,
	depth int,
) ParagraphFormatSpec {
	if depth > 5 || style == nil {
		return ParagraphFormatSpec{}
	}

	spec := ParagraphFormatSpec{}

	// 先递归获取父样式属性（basedOn）
	if style.BasedOn != nil && style.BasedOn.ValAttr != "" {
		parentKey := strings.ToLower(style.BasedOn.ValAttr)
		if parentStyle, ok := byID[parentKey]; ok {
			spec = e.extractWithInheritance(parentStyle, byID, byName, depth+1)
		} else if parentStyle, ok := byName[parentKey]; ok {
			spec = e.extractWithInheritance(parentStyle, byID, byName, depth+1)
		}
	}

	// 用本层 Run 属性覆盖父样式（子优先）
	if style.RPr != nil {
		rPr := style.RPr
		if rPr.RFonts != nil {
			if rPr.RFonts.EastAsiaAttr != nil && *rPr.RFonts.EastAsiaAttr != "" {
				spec.FontEastAsia = *rPr.RFonts.EastAsiaAttr
			}
			if rPr.RFonts.AsciiAttr != nil && *rPr.RFonts.AsciiAttr != "" {
				spec.FontAscii = *rPr.RFonts.AsciiAttr
			}
		}
		if rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
			spec.FontSizeHalfPt = *rPr.Sz.ValAttr.ST_UnsignedDecimalNumber
		}
		if rPr.SzCs != nil && rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber != nil {
			spec.FontSizeCSHalfPt = *rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber
		}
		spec.Bold = rPr.B != nil
	}

	// 用本层段落属性覆盖
	if style.PPr != nil {
		pPr := style.PPr
		if pPr.Jc != nil {
			spec.AlignmentSet = true
			spec.Alignment = pPr.Jc.ValAttr
		}
		if pPr.Spacing != nil {
			sp := pPr.Spacing
			if sp.LineAttr != nil && sp.LineAttr.Int64 != nil {
				spec.LineSpacingVal = *sp.LineAttr.Int64
				spec.LineSpacingRule = sp.LineRuleAttr
			}
			if sp.BeforeAttr != nil && sp.BeforeAttr.ST_UnsignedDecimalNumber != nil {
				spec.SpaceBefore = *sp.BeforeAttr.ST_UnsignedDecimalNumber
			}
			if sp.AfterAttr != nil && sp.AfterAttr.ST_UnsignedDecimalNumber != nil {
				spec.SpaceAfter = *sp.AfterAttr.ST_UnsignedDecimalNumber
			}
		}
		if pPr.Ind != nil {
			ind := pPr.Ind
			// FirstLine 缩进：用 ST_UnsignedDecimalNumber（与 CT_PPr 相同的访问路径）
			if ind.FirstLineAttr != nil && ind.FirstLineAttr.ST_UnsignedDecimalNumber != nil {
				spec.FirstLineIndent = *ind.FirstLineAttr.ST_UnsignedDecimalNumber
			}
			// 左缩进：LeftAttr 是 ST_SignedTwipsMeasure
			if ind.LeftAttr != nil && ind.LeftAttr.Int64 != nil && *ind.LeftAttr.Int64 > 0 {
				spec.IndentLeft = uint64(*ind.LeftAttr.Int64)
			}
		}
		if pPr.KeepNext != nil {
			spec.KeepWithNext = true
		}
		if pPr.KeepLines != nil {
			spec.KeepLines = true
		}
		if pPr.PageBreakBefore != nil {
			spec.PageBreak = true
		}
	}

	return spec
}
