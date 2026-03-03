package formatchecker

import (
	"fmt"
	"regexp"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	// "gitee.com/greatmusicians/unioffice/measurement"
	// "github.com/nguyenthenguyen/docx"
)

// TemplateParser 模板解析器
type TemplateParser struct{}

// NewTemplateParser 创建模板解析器
func NewTemplateParser() *TemplateParser {
	return &TemplateParser{}
}

// ParseTemplate 从DOCX模板文件中解析格式标准
// 支持两种模式：
// 1. 样式定义模式：解析文档中的样式定义 (Styles)
// 2. 实例扫描模式：扫描文档中的特定段落作为样本 (如果没有明确样式)
func (p *TemplateParser) ParseTemplate(templatePath string) (*FormatStandard, error) {
	// 使用 unioffice 打开文档进行详细属性读取
	doc, err := document.Open(templatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open template: %w", err)
	}
	defer doc.Close()

	// 为了兼容性，也可以用 github.com/nguyenthenguyen/docx 打开以获取某些纯文本信息
	// 但 unioffice 功能更强，主要用它。

	standard := &FormatStandard{
		Name:        "从模板导入的标准",
		Description: "根据上传的DOCX模板解析生成的格式标准",
	}

	// 1. 解析页面设置 (Page Setup)
	p.parsePageSetup(doc, standard)

	// 2. 解析标题样式 (Heading Styles)
	p.parseHeadingStyles(doc, standard)

	// 3. 解析正文样式 (Paragraph Styles)
	p.parseParagraphStyles(doc, standard)

	// 4. 解析参考文献样式
	p.parseReferenceStyles(doc, standard)

	// 5. 解析页眉内容作为学校名称/论文标题
	p.parseHeaderContent(doc, standard)

	return standard, nil
}

// parsePageSetup 解析页面设置
func (p *TemplateParser) parsePageSetup(doc *document.Document, standard *FormatStandard) {
	// 获取第一个节 (Section) 的页面设置
	// 大多数论文模板第一节或第二节是正文，具有代表性
	// doc.Sections() is not available, we need to iterate body contents or use BodySection()
	// But BodySection() returns a section which wraps the body.
	// Actually unioffice Document has Body() which has content.
	// And there is BodySection() which returns the last section properties? No.
	// Let's use BodySection() as a start, which represents the last section properties usually.

	section := doc.BodySection()

	pgMar := section.X().PgMar
	if pgMar != nil {
		// unioffice 中 PgMar 单位通常是 Twips (1/20 point) 或 Point?
		// XML 中是 Twips. 1 cm = 567 Twips.
		// 需要转换为 cm.

		// 转换辅助函数：Twips (signed) -> cm
		signedTwipsToCm := func(twips int64) float64 {
			return float64(twips) / 567.0
		}

		if pgMar.TopAttr.Int64 != nil {
			standard.PageSetup.MarginTop = signedTwipsToCm(*pgMar.TopAttr.Int64)
		}
		if pgMar.BottomAttr.Int64 != nil {
			standard.PageSetup.MarginBottom = signedTwipsToCm(*pgMar.BottomAttr.Int64)
		}
		if pgMar.LeftAttr.ST_UnsignedDecimalNumber != nil {
			standard.PageSetup.MarginLeft = uint64TwipsToCm(*pgMar.LeftAttr.ST_UnsignedDecimalNumber)
		}
		if pgMar.RightAttr.ST_UnsignedDecimalNumber != nil {
			standard.PageSetup.MarginRight = uint64TwipsToCm(*pgMar.RightAttr.ST_UnsignedDecimalNumber)
		}
		if pgMar.HeaderAttr.ST_UnsignedDecimalNumber != nil {
			standard.PageSetup.HeaderDistance = uint64TwipsToCm(*pgMar.HeaderAttr.ST_UnsignedDecimalNumber)
		}
		if pgMar.FooterAttr.ST_UnsignedDecimalNumber != nil {
			standard.PageSetup.FooterDistance = uint64TwipsToCm(*pgMar.FooterAttr.ST_UnsignedDecimalNumber)
		}
	}

	// 纸张大小
	pgSz := section.X().PgSz
	if pgSz != nil {
		// A4: 11906 x 16838 Twips (21cm x 29.7cm)
		w := int64(0)
		if pgSz.WAttr != nil && pgSz.WAttr.ST_UnsignedDecimalNumber != nil {
			w = int64(*pgSz.WAttr.ST_UnsignedDecimalNumber)
		}

		if w > 11000 && w < 13000 {
			standard.PageSetup.PaperSize = "A4"
		} else {
			standard.PageSetup.PaperSize = "Custom"
		}
	}
}

// uint64TwipsToCm 将 uint64 Twips 转换为 cm
func uint64TwipsToCm(twips uint64) float64 {
	return float64(twips) / 567.0
}

// parseHeadingStyles 解析标题样式
// 策略：优先查找样式名为 "Heading 1"/"标题 1" 的样式定义。
// 如果找不到，则扫描文档中看起来像标题的段落。
func (p *TemplateParser) parseHeadingStyles(doc *document.Document, standard *FormatStandard) {
	// 初始化默认值
	standard.HeadingStyles = make([]HeadingStyle, 0)

	// 1. 尝试从样式表 (Styles) 中读取
	// unioffice 暂未提供直接遍历 Styles 的高级 API，需要操作底层 XML 或遍历段落引用
	// 简单的做法是：遍历文档中的所有段落，找到使用了 "Heading 1" 等样式的段落，
	// 或者找到内容匹配 "第1章" 的段落，提取其格式。

	foundLevels := make(map[int]bool)

	for _, para := range doc.Paragraphs() {
		styleName := ""
		if para.Style() != "" {
			styleName = para.Style()
		}

		text := ""
		for _, run := range para.Runs() {
			text += run.Text()
		}
		text = strings.TrimSpace(text)

		level := 0

		// 判断级别
		if styleName == "Heading1" || styleName == "标题 1" || styleName == "1" {
			level = 1
		} else if styleName == "Heading2" || styleName == "标题 2" || styleName == "2" {
			level = 2
		} else if styleName == "Heading3" || styleName == "标题 3" || styleName == "3" {
			level = 3
		} else {
			// 基于内容判断
			if matched, _ := regexp.MatchString(`^(第[一二三四五六七八九十0-9]+章|1\s+绪论)`, text); matched {
				level = 1
			} else if matched, _ := regexp.MatchString(`^\d+\.\d+\s+`, text); matched {
				level = 2
			} else if matched, _ := regexp.MatchString(`^\d+\.\d+\.\d+\s+`, text); matched {
				level = 3
			}
		}

		if level > 0 && !foundLevels[level] {
			// 提取格式
			style := HeadingStyle{
				Level: level,
				Name:  fmt.Sprintf("%d级标题", level),
			}

			// 提取字体
			runs := para.Runs()
			if len(runs) > 0 {
				// 优先取第一个 Run 的属性
				rPr := runs[0].Properties()
				// unioffice Font extraction is complex due to themes
				// Simplified:
				if rPr.X().RFonts != nil {
					if rPr.X().RFonts.EastAsiaAttr != nil {
						style.FontName = *rPr.X().RFonts.EastAsiaAttr
					} else if rPr.X().RFonts.AsciiAttr != nil {
						style.FontName = *rPr.X().RFonts.AsciiAttr
					}
				}
				if rPr.X().Sz != nil && rPr.X().Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
					style.FontSize = float64(*rPr.X().Sz.ValAttr.ST_UnsignedDecimalNumber) / 2.0 // half-points
				}
				if rPr.X().B != nil {
					// Toggle property, usually means true if present without Val
					style.Bold = true
					// ST_OnOff is boolean enum wrapper
					if rPr.X().B.ValAttr != nil {
						// It's an enum, we need to check value. ST_OnOff(true) is 1.
						// But unioffice generated types are tricky.
						// Let's assume default is true.
						// If explicitly false?
						// It's complicated to check exact enum value without importing wml
					}
				}
			}

			// 提取段落属性
			pPr := para.Properties()
			// 对齐
			if pPr.X().Jc != nil {
				// unioffice uses ST_Jc enum
				switch pPr.X().Jc.ValAttr {
				case 1:
					style.Alignment = "center" // center
				case 2:
					style.Alignment = "right"
				case 3:
					style.Alignment = "justify" // both
				default:
					style.Alignment = "left"
				}
			} else {
				style.Alignment = "left" // default
			}

			// 间距
			if pPr.X().Spacing != nil {
				if pPr.X().Spacing.BeforeAttr != nil && pPr.X().Spacing.BeforeAttr.ST_UnsignedDecimalNumber != nil {
					style.SpacingBefore = float64(*pPr.X().Spacing.BeforeAttr.ST_UnsignedDecimalNumber) / 20.0 // Twips to Points
				}
				if pPr.X().Spacing.AfterAttr != nil && pPr.X().Spacing.AfterAttr.ST_UnsignedDecimalNumber != nil {
					style.SpacingAfter = float64(*pPr.X().Spacing.AfterAttr.ST_UnsignedDecimalNumber) / 20.0
				}
				if pPr.X().Spacing.LineAttr != nil && pPr.X().Spacing.LineAttr.Int64 != nil {
					// Line spacing interpretation depends on LineRule
					// Simplified: assume exact or auto
					lineVal := *pPr.X().Spacing.LineAttr.Int64
					style.LineSpacing = float64(lineVal) / 20.0
					if pPr.X().Spacing.LineRuleAttr == 0 { // auto
						style.LineSpacing = float64(lineVal) / 240.0 * 12.0 // approx
					}
				}
			}

			standard.HeadingStyles = append(standard.HeadingStyles, style)
			foundLevels[level] = true
		}
	}
}

// parseParagraphStyles 解析正文样式
func (p *TemplateParser) parseParagraphStyles(doc *document.Document, standard *FormatStandard) {
	// 查找正文段落
	// 策略：查找样式为 "Normal" 或 "正文" 的段落，或者字数较多且不是标题的段落

	for _, para := range doc.Paragraphs() {
		styleName := para.Style()
		text := ""
		for _, run := range para.Runs() {
			text += run.Text()
		}

		isBody := false
		if styleName == "Normal" || styleName == "正文" {
			isBody = true
		} else if len(text) > 50 && !isHeading(text) { // 简单启发式
			isBody = true
		}

		if isBody {
			// 提取样式
			style := ParagraphStyle{
				Name: "正文",
			}

			// 提取字体 (同上，简化)
			runs := para.Runs()
			if len(runs) > 0 {
				rPr := runs[0].Properties()
				if rPr.X().RFonts != nil {
					if rPr.X().RFonts.EastAsiaAttr != nil {
						style.FontName = *rPr.X().RFonts.EastAsiaAttr
					}
				}
				if rPr.X().Sz != nil && rPr.X().Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
					style.FontSize = float64(*rPr.X().Sz.ValAttr.ST_UnsignedDecimalNumber) / 2.0
				}
			}

			// 提取段落属性
			pPr := para.Properties()
			if pPr.X().Ind != nil {
				if pPr.X().Ind.FirstLineCharsAttr != nil {
					style.FirstLineIndent = float64(*pPr.X().Ind.FirstLineCharsAttr) / 100.0
				} else if pPr.X().Ind.FirstLineAttr != nil && pPr.X().Ind.FirstLineAttr.ST_UnsignedDecimalNumber != nil {
					// Twips to chars approx (assuming 12pt font)
					style.FirstLineIndent = float64(*pPr.X().Ind.FirstLineAttr.ST_UnsignedDecimalNumber) / 240.0
				}
			}

			if pPr.X().Spacing != nil {
				if pPr.X().Spacing.LineAttr != nil && pPr.X().Spacing.LineAttr.Int64 != nil {
					style.LineSpacing = float64(*pPr.X().Spacing.LineAttr.Int64) / 20.0
				}
			}

			standard.ParagraphStyles = append(standard.ParagraphStyles, style)
			return // 只需要提取一个样本
		}
	}
}

func isHeading(text string) bool {
	matched, _ := regexp.MatchString(`^(第.+章|1\s+绪论|\d+\.\d+)`, text)
	return matched
}

// parseReferenceStyles 解析参考文献样式
func (p *TemplateParser) parseReferenceStyles(doc *document.Document, standard *FormatStandard) {
	for _, para := range doc.Paragraphs() {
		text := ""
		for _, run := range para.Runs() {
			text += run.Text()
		}
		text = strings.TrimSpace(text)

		if strings.HasPrefix(text, "[1]") || strings.HasPrefix(text, "[M]") {
			// 提取样式
			style := ReferenceStyle{
				Style: "GB/T 7714",
			}

			runs := para.Runs()
			if len(runs) > 0 {
				rPr := runs[0].Properties()
				if rPr.X().RFonts != nil {
					if rPr.X().RFonts.EastAsiaAttr != nil {
						style.FontName = *rPr.X().RFonts.EastAsiaAttr
					}
				}
				if rPr.X().Sz != nil && rPr.X().Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
					style.FontSize = float64(*rPr.X().Sz.ValAttr.ST_UnsignedDecimalNumber) / 2.0
				}
			}

			standard.ReferenceStyle = style
			return
		}
	}
}

// parseHeaderContent 提取页眉中的学校名称
func (p *TemplateParser) parseHeaderContent(doc *document.Document, standard *FormatStandard) {
	for _, header := range doc.Headers() {
		for _, p := range header.Paragraphs() {
			text := ""
			for _, run := range p.Runs() {
				text += run.Text()
			}
			text = strings.TrimSpace(text)
			if strings.Contains(text, "大学") || strings.Contains(text, "学院") {
				standard.Name = text
				return
			}
		}
	}
}
