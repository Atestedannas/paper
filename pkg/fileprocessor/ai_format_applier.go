package fileprocessor

import (
	"fmt"
	"log"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

// AIFormatApplier 基于ParagraphFormatSpec直接写入OOXML的格式应用器
// 完全绕过JSON规则解析链，消除多层转换带来的误差
type AIFormatApplier struct {
	processor *EnhancedProcessor
}

// NewAIFormatApplier 创建格式应用器
func NewAIFormatApplier(proc *EnhancedProcessor) *AIFormatApplier {
	return &AIFormatApplier{processor: proc}
}

// Apply 将模板格式规范应用到所有已分类的段落
//
// classified:      classifyParagraphs 返回的分类结果
// specs:           LoadFromFile 返回的模板格式规范
// skipCategories:  要跳过的段落类别（不修改格式），如 "cover"
//
// 返回实际修正的段落数量
func (a *AIFormatApplier) Apply(
	classified map[string][]document.Paragraph,
	specs map[string]ParagraphFormatSpec,
	skipCategories map[string]bool,
) int {
	total := 0
	for category, paras := range classified {
		if skipCategories[category] {
			log.Printf("[AI应用] 跳过 %s (%d段)", category, len(paras))
			continue
		}
		spec, ok := specs[category]
		if !ok || spec.IsEmpty() {
			// 没有该类型的模板格式，静默跳过（不污染日志）
			continue
		}
		count := 0
		for _, para := range paras {
			text := strings.TrimSpace(a.processor.extractParagraphText(para))
			if text == "" {
				continue
			}
		applySpec := spec
		// 摘要/英文摘要：不加粗，字号最大12pt
		if isAbstractCategory(category) {
			applySpec.Bold = false
			if applySpec.FontSizeHalfPt == 0 || applySpec.FontSizeHalfPt > 24 {
				applySpec.FontSizeHalfPt = 24 // 小四=12pt
			}
		}
		// 安全兜底：Named Style集成后应少触发，但保留防污染保护
		switch category {
		case "references":
			// Named Style未提供字体/字号时才兜底（防止空spec）
			if applySpec.FontEastAsia == "" || applySpec.FontEastAsia == "黑体" || applySpec.FontEastAsia == "仿宋" {
				applySpec.FontEastAsia = "宋体"
				applySpec.FontAscii = "SimSun"
			}
			if applySpec.FontSizeHalfPt == 0 || applySpec.FontSizeHalfPt > 28 {
				applySpec.FontSizeHalfPt = 24 // 宋体小四=12pt
			}
			applySpec.Bold = false
		case "body":
			// Named Style后font应是正确的宋体；黑体说明Named Style也未生效，兜底
			if applySpec.FontEastAsia == "" || applySpec.FontEastAsia == "黑体" || applySpec.FontEastAsia == "SimHei" {
				applySpec.FontEastAsia = "宋体"
				applySpec.FontAscii = "SimSun"
			}
			if applySpec.FontSizeHalfPt == 0 || applySpec.FontSizeHalfPt > 28 {
				applySpec.FontSizeHalfPt = 24
			}
			applySpec.Bold = false
		}
			// #region agent log H2
			if count == 0 { // 每类只记第一段
				actualSpec := extractParaFormatSpec(para)
				preview := []rune(text); if len(preview) > 25 { preview = preview[:25] }
				dbgLog("H2", "ai_format_applier.go:Apply",
					"applying spec to para",
					fmt.Sprintf(`{"category":%q,"text":%q,"specFont":%q,"specSizeHalfPt":%d,"specBold":%v,"actualFont":%q,"actualSizeHalfPt":%d,"actualBold":%v}`,
						category, string(preview),
						applySpec.FontEastAsia, applySpec.FontSizeHalfPt, applySpec.Bold,
						actualSpec.FontEastAsia, actualSpec.FontSizeHalfPt, actualSpec.Bold))
			}
			// #endregion agent log H2
			a.ApplySpecToPara(para, applySpec)
			count++
		}
		if count > 0 {
			log.Printf("[AI应用] %s: 修正 %d 段 (font=%q size=%.1fpt bold=%v align=%s)",
				category, count, spec.FontEastAsia, spec.FontSizePt(),
				spec.Bold, jcToAlignString(spec.Alignment))
			total += count
		}
	}
	return total
}

// ApplySpecToPara 将格式规范直接写入段落的OOXML，无JSON解析
// 按照Word格式优先级链从低到高逐层覆写：段落级→段落RPr→Run级
func (a *AIFormatApplier) ApplySpecToPara(para document.Paragraph, spec ParagraphFormatSpec) {
	// 清除 Word 样式引用，避免样式定义覆盖直接设置的格式
	if pPr := para.X().PPr; pPr != nil {
		pPr.PStyle = nil
	}
	for _, run := range para.Runs() {
		if rPr := run.X().RPr; rPr != nil {
			rPr.RStyle = nil
		}
	}

	paraProps := para.Properties()

	// 1. 对齐方式
	if spec.AlignmentSet {
		paraProps.SetAlignment(spec.Alignment)
	}

	// 2. 行距（直接操作XML，高层API行距设置有已知问题）
	if spec.LineSpacingVal > 0 {
		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}
		if pPr.Spacing == nil {
			pPr.Spacing = wml.NewCT_Spacing()
		}
		lv := spec.LineSpacingVal
		if pPr.Spacing.LineAttr == nil {
			pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
		}
		pPr.Spacing.LineAttr.Int64 = &lv
		pPr.Spacing.LineRuleAttr = spec.LineSpacingRule
	}

	// 3. 段前段后间距
	if spec.SpaceBefore > 0 {
		paraProps.Spacing().SetBefore(measurement.Distance(spec.SpaceBefore) * measurement.Twips)
	}
	if spec.SpaceAfter > 0 {
		paraProps.Spacing().SetAfter(measurement.Distance(spec.SpaceAfter) * measurement.Twips)
	}

	// 4. 首行缩进（twips → measurement.Distance）
	if spec.FirstLineIndent > 0 {
		paraProps.SetFirstLineIndent(measurement.Distance(spec.FirstLineIndent) * measurement.Twips)
	}

	// 4b. 左右缩进（twips）
	if spec.IndentLeft > 0 || spec.IndentRight > 0 {
		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}
		if pPr.Ind == nil {
			pPr.Ind = wml.NewCT_Ind()
		}
		if spec.IndentLeft > 0 {
			left := int64(spec.IndentLeft)
			pPr.Ind.LeftAttr = &wml.ST_SignedTwipsMeasure{Int64: &left}
		}
		if spec.IndentRight > 0 {
			right := int64(spec.IndentRight)
			pPr.Ind.RightAttr = &wml.ST_SignedTwipsMeasure{Int64: &right}
		}
	}

	// 5. 分页符
	if spec.PageBreak {
		a.processor.setPageBreakBefore(para)
	}

	// 5b. 与下段同页 / 段内不分页
	{
		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}
		if spec.KeepWithNext {
			pPr.KeepNext = wml.NewCT_OnOff()
		} else {
			pPr.KeepNext = nil
		}
		if spec.KeepLines {
			pPr.KeepLines = wml.NewCT_OnOff()
		} else {
			pPr.KeepLines = nil
		}
	}

	// 6. 段落级默认Run属性（pPr/rPr）：字体/字号/加粗
	{
		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}
		if pPr.RPr == nil {
			pPr.RPr = wml.NewCT_ParaRPr()
		}
		a.applyFontToParaRPr(pPr.RPr, spec)
	}

	// 7. Run级格式（最高优先级）
	for _, run := range para.Runs() {
		a.applySpecToRun(run, spec)
	}
}

// applyFontToParaRPr 将字体信息写入段落级默认Run属性（pPr/rPr）
func (a *AIFormatApplier) applyFontToParaRPr(rPr *wml.CT_ParaRPr, spec ParagraphFormatSpec) {
	if spec.FontEastAsia != "" {
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		// EastAsia 用中文名，Ascii/HAnsi/Cs 用英文名，避免 WPS 显示 "黑体;SimHei"
		eastAsiaPtr := a.processor.getCachedFontName(spec.FontEastAsia)
		englishName := getEnglishFontName(spec.FontEastAsia)
		asciiPtr := a.processor.getCachedFontName(englishName)
		rPr.RFonts.EastAsiaAttr = eastAsiaPtr
		rPr.RFonts.AsciiAttr = asciiPtr
		rPr.RFonts.HAnsiAttr = asciiPtr
		rPr.RFonts.CsAttr = asciiPtr
	}
	if spec.FontAscii != "" && spec.FontEastAsia == "" {
		// 纯ASCII字体（如英文参考文献条目）
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		asciiPtr := a.processor.getCachedFontName(spec.FontAscii)
		rPr.RFonts.AsciiAttr = asciiPtr
		rPr.RFonts.HAnsiAttr = asciiPtr
	}
	if spec.FontSizeHalfPt > 0 {
		halfPt := spec.FontSizeHalfPt
		rPr.Sz = wml.NewCT_HpsMeasure()
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	}
	csHalfPt := spec.FontSizeCSHalfPt
	if csHalfPt == 0 {
		csHalfPt = spec.FontSizeHalfPt
	}
	if csHalfPt > 0 {
		rPr.SzCs = wml.NewCT_HpsMeasure()
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &csHalfPt
	}
	if spec.Bold {
		rPr.B = wml.NewCT_OnOff()
	} else {
		rPr.B = nil
	}
	if spec.Underline {
		rPr.U = wml.NewCT_Underline()
		rPr.U.ValAttr = wml.ST_UnderlineSingle
	} else {
		rPr.U = nil
	}
}

// applySpecToRun 将格式规范应用到单个Run（Run级优先级最高）
func (a *AIFormatApplier) applySpecToRun(run document.Run, spec ParagraphFormatSpec) {
	rPr := run.X().RPr
	if rPr == nil {
		rPr = wml.NewCT_RPr()
		run.X().RPr = rPr
	}

	// 字体
	if spec.FontEastAsia != "" {
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		eastAsiaPtr := a.processor.getCachedFontName(spec.FontEastAsia)
		englishName := getEnglishFontName(spec.FontEastAsia)
		asciiPtr := a.processor.getCachedFontName(englishName)
		rPr.RFonts.EastAsiaAttr = eastAsiaPtr
		rPr.RFonts.AsciiAttr = asciiPtr
		rPr.RFonts.HAnsiAttr = asciiPtr
		rPr.RFonts.CsAttr = asciiPtr
	}
	if spec.FontAscii != "" && spec.FontEastAsia == "" {
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		asciiPtr := a.processor.getCachedFontName(spec.FontAscii)
		rPr.RFonts.AsciiAttr = asciiPtr
		rPr.RFonts.HAnsiAttr = asciiPtr
	}

	// 字号（半磅单位，直接写w:sz，避免单位转换问题）
	if spec.FontSizeHalfPt > 0 {
		halfPt := spec.FontSizeHalfPt
		if rPr.Sz == nil {
			rPr.Sz = wml.NewCT_HpsMeasure()
		}
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	}
	csHalfPt := spec.FontSizeCSHalfPt
	if csHalfPt == 0 {
		csHalfPt = spec.FontSizeHalfPt
	}
	if csHalfPt > 0 {
		if rPr.SzCs == nil {
			rPr.SzCs = wml.NewCT_HpsMeasure()
		}
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &csHalfPt
	}

	// 加粗
	if spec.Bold {
		rPr.B = wml.NewCT_OnOff()
		rPr.BCs = wml.NewCT_OnOff()
	} else {
		rPr.B = nil
		rPr.BCs = nil
	}

	// 斜体
	if spec.Italic {
		rPr.I = wml.NewCT_OnOff()
		rPr.ICs = wml.NewCT_OnOff()
	} else {
		rPr.I = nil
		rPr.ICs = nil
	}

	// 下划线
	if spec.Underline {
		rPr.U = wml.NewCT_Underline()
		rPr.U.ValAttr = wml.ST_UnderlineSingle
	} else {
		rPr.U = nil
	}
}

func isAbstractCategory(category string) bool {
	return category == "abstract" || category == "english_abstract" || category == "en_abstract"
}

// ApplyFontOnlyToTableCellPara 表格单元格专用：只修改字体/字号/加粗，
// 不清除 pStyle，不改段落级属性（行距/缩进/对齐），防止表格列宽重排换行
func (a *AIFormatApplier) ApplyFontOnlyToTableCellPara(para document.Paragraph, spec ParagraphFormatSpec) {
	// 不清除 pPr.PStyle（表格单元格样式需保留）
	// 不清除 rPr.RStyle
	// 不修改段落级属性（行距/对齐/缩进/分页符）

	// 只在 Run 级别修改字体/字号/加粗
	for _, run := range para.Runs() {
		a.applySpecToRun(run, spec)
	}

	// 更新段落级 rPr（pPr/rPr）中的字体，影响 run 继承
	if pPr := para.X().PPr; pPr != nil {
		if pPr.RPr == nil {
			pPr.RPr = wml.NewCT_ParaRPr()
		}
		a.applyFontToParaRPr(pPr.RPr, spec)
	}
}
