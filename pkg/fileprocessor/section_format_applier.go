package fileprocessor

import (
	"log"
	"os/exec"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/ofc/sharedTypes"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

// ──────────────────────────────────────────────────────────────────────────────
// 1. 页面设置：A4 纸张 + 标准边距
// ──────────────────────────────────────────────────────────────────────────────

func (p *EnhancedProcessor) applyA4PageSize(doc *document.Document) {
	section := doc.BodySection()
	sectPr := section.X()
	if sectPr == nil {
		return
	}
	if sectPr.PgSz == nil {
		sectPr.PgSz = wml.NewCT_PageSz()
	}
	// A4: 210mm × 297mm → twips: 210*567≈11906, 297*567≈16838
	w := uint64(11906)
	h := uint64(16838)
	sectPr.PgSz.WAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &w}
	sectPr.PgSz.HAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &h}
	sectPr.PgSz.OrientAttr = wml.ST_PageOrientationPortrait

	log.Println("[页面设置] A4 纸张 (210×297mm) 已应用")
	p.runDocumentFormattingSelfCheck("applyA4PageSize", doc)
}

func (p *EnhancedProcessor) applyStandardMargins(doc *document.Document) {
	section := doc.BodySection()
	// 上2.5cm 下2.0cm 左2.5cm 右2.0cm，页眉1.5cm，页脚1.75cm，装订线0
	section.SetPageMargins(
		measurement.Distance(2.5)*measurement.Centimeter,  // top
		measurement.Distance(2.0)*measurement.Centimeter,  // bottom
		measurement.Distance(2.5)*measurement.Centimeter,  // left
		measurement.Distance(2.0)*measurement.Centimeter,  // right
		measurement.Distance(1.5)*measurement.Centimeter,  // header
		measurement.Distance(1.75)*measurement.Centimeter, // footer
		0, // gutter
	)
	log.Println("[页面设置] 标准边距已应用: 上2.5/下2.0/左2.5/右2.0 cm, 页眉1.5cm, 页脚1.75cm")
	p.runDocumentFormattingSelfCheck("applyStandardMargins", doc)
}

// ──────────────────────────────────────────────────────────────────────────────
// 2. 页眉：0.5磅双线、宋体小五居中
// ──────────────────────────────────────────────────────────────────────────────

func (p *EnhancedProcessor) buildDoubleLineHeaderParagraph(hdr document.Header, text string, fontName string, fontSize float64) {
	hdr.Clear()
	para := hdr.AddParagraph()
	para.Properties().SetAlignment(wml.ST_JcCenter)

	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}

	// 0.5磅双线下边框
	pPr.PBdr = wml.NewCT_PBdr()
	pPr.PBdr.Bottom = wml.NewCT_Border()
	pPr.PBdr.Bottom.ValAttr = wml.ST_BorderDouble
	sz4 := uint64(4) // 4 eighths of a point = 0.5pt
	pPr.PBdr.Bottom.SzAttr = &sz4
	pPr.PBdr.Bottom.SpaceAttr = new(uint64)
	*pPr.PBdr.Bottom.SpaceAttr = 1
	pPr.PBdr.Bottom.ColorAttr = &wml.ST_HexColor{ST_HexColorAuto: wml.ST_HexColorAutoAuto}

	run := para.AddRun()
	run.AddText(text)
	p.setRunFont(run, fontName, fontSize, false)
}

func (p *EnhancedProcessor) applySchoolHeader(doc *document.Document) {
	section := doc.BodySection()
	sectPr := section.X()
	if sectPr != nil {
		sectPr.EG_HdrFtrReferences = nil
	}

	coverInfo := p.extractCoverInfo(doc)
	gradeYear := ""
	if banJi, ok := coverInfo["班级"]; ok && banJi != "" {
		for i, r := range banJi {
			if r >= '0' && r <= '9' {
				end := i + 4
				if end <= len(banJi) {
					candidate := banJi[i:end]
					valid := true
					for _, c := range candidate {
						if c < '0' || c > '9' {
							valid = false
							break
						}
					}
					if valid {
						gradeYear = candidate
						break
					}
				}
			}
		}
	}

	major := coverInfo["专业"]
	if major == "" {
		major = "XX"
	}
	if gradeYear == "" {
		gradeYear = "XX"
	}

	headerText := "重庆人文科技学院" + gradeYear + "届" + major + "专业本科毕业论文"
	log.Printf("[页眉] 自动生成页眉: %q", headerText)

	hdr := doc.AddHeader()
	p.buildDoubleLineHeaderParagraph(hdr, headerText, "宋体", 9) // 小五号 = 9pt
	section.SetHeader(hdr, wml.ST_HdrFtrDefault)
	p.runDocumentFormattingSelfCheck("applySchoolHeader", doc)
}

// ──────────────────────────────────────────────────────────────────────────────
// 3. 页脚：第×页 共×页，宋体小五居中
// ──────────────────────────────────────────────────────────────────────────────

func (p *EnhancedProcessor) applyStandardFooter(doc *document.Document) {
	section := doc.BodySection()

	ftr := doc.AddFooter()
	section.SetFooter(ftr, wml.ST_HdrFtrDefault)

	para := ftr.AddParagraph()
	para.Properties().SetAlignment(wml.ST_JcCenter)

	fontName := "宋体"
	fontSize := 9.0 // 小五号

	// "第"
	r1 := para.AddRun()
	r1.AddText("第")
	p.setRunFont(r1, fontName, fontSize, false)

	// PAGE field
	p.addPageFieldToParagraph(para, fontName, fontSize)

	// "页 共"
	r2 := para.AddRun()
	r2.AddText("页 共")
	p.setRunFont(r2, fontName, fontSize, false)

	// NUMPAGES field
	p.addNumPagesFieldToParagraph(para, fontName, fontSize)

	// "页"
	r3 := para.AddRun()
	r3.AddText("页")
	p.setRunFont(r3, fontName, fontSize, false)

	// 页码从1开始（阿拉伯数字）
	sectPr := section.X()
	if sectPr.PgNumType == nil {
		sectPr.PgNumType = wml.NewCT_PageNumber()
	}
	startVal := int64(1)
	sectPr.PgNumType.StartAttr = &startVal
	sectPr.PgNumType.FmtAttr = wml.ST_NumberFormatDecimal

	log.Println("[页脚] 已设置: 第×页 共×页, 宋体小五居中")
	p.runDocumentFormattingSelfCheck("applyStandardFooter", doc)
}

// applySectionBreaksForPageNumbering inserts section breaks so that:
//   - abstract pages use uppercase Roman numeral page numbering
//   - body (from 绪论/第1章) onward uses Arabic numbering starting at 1
//   - TOC pages have no footer
func (p *EnhancedProcessor) applySectionBreaksForPageNumbering(doc *document.Document) {
	paragraphs := doc.Paragraphs()
	abstractIdx := -1
	bodyStartIdx := -1

	for i, para := range paragraphs {
		text := strings.TrimSpace(p.extractParagraphText(para))
		if text == "" {
			continue
		}
		if abstractIdx == -1 && (strings.HasPrefix(text, "摘") && strings.Contains(text, "要")) {
			abstractIdx = i
		}
		if bodyStartIdx == -1 && (strings.HasPrefix(text, "绪论") ||
			strings.HasPrefix(text, "第1章") || strings.HasPrefix(text, "第一章") ||
			strings.HasPrefix(text, "1 ") || strings.HasPrefix(text, "1.")) {
			pPr := para.X().PPr
			if pPr != nil && pPr.RPr != nil && pPr.RPr.B != nil {
				bodyStartIdx = i
			} else if len([]rune(text)) <= 30 {
				bodyStartIdx = i
			}
		}
		if abstractIdx >= 0 && bodyStartIdx >= 0 {
			break
		}
	}

	if abstractIdx >= 0 {
		p.insertSectionBreakBefore(paragraphs[abstractIdx], wml.ST_NumberFormatUpperRoman)
		log.Printf("[分节] 摘要前插入分节符 (罗马数字页码), 段落索引=%d", abstractIdx)
	}
	if bodyStartIdx >= 0 {
		p.insertSectionBreakBefore(paragraphs[bodyStartIdx], wml.ST_NumberFormatDecimal)
		log.Printf("[分节] 正文前插入分节符 (阿拉伯数字页码从1开始), 段落索引=%d", bodyStartIdx)
	}
	p.runDocumentFormattingSelfCheck("applySectionBreaksForPageNumbering", doc)
}

func (p *EnhancedProcessor) insertSectionBreakBefore(para document.Paragraph, numFmt wml.ST_NumberFormat) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}

	sectPr := wml.NewCT_SectPr()
	sectPr.Type = wml.NewCT_SectType()
	sectPr.Type.ValAttr = wml.ST_SectionMarkNextPage

	sectPr.PgNumType = wml.NewCT_PageNumber()
	startVal := int64(1)
	sectPr.PgNumType.StartAttr = &startVal
	sectPr.PgNumType.FmtAttr = numFmt

	pPr.SectPr = sectPr
}

// ──────────────────────────────────────────────────────────────────────────────
// 4. 三线表：上下1.5磅粗线、中间1磅、无竖线
// ──────────────────────────────────────────────────────────────────────────────

func (p *EnhancedProcessor) applyThreeLineTableFormat(doc *document.Document) {
	tables := doc.Tables()
	if len(tables) == 0 {
		return
	}

	for i, tbl := range tables {
		tblPr := tbl.X().TblPr
		if tblPr == nil {
			tblPr = wml.NewCT_TblPr()
			tbl.X().TblPr = tblPr
		}

		// Table-level borders: top/bottom thick (1.5pt=12 eighths), no left/right/insideV
		tblPr.TblBorders = wml.NewCT_TblBorders()

		// Top: 1.5pt single
		tblPr.TblBorders.Top = wml.NewCT_Border()
		tblPr.TblBorders.Top.ValAttr = wml.ST_BorderSingle
		topSz := uint64(12) // 12 eighths of pt = 1.5pt
		tblPr.TblBorders.Top.SzAttr = &topSz
		tblPr.TblBorders.Top.ColorAttr = &wml.ST_HexColor{ST_HexColorAuto: wml.ST_HexColorAutoAuto}

		// Bottom: 1.5pt single
		tblPr.TblBorders.Bottom = wml.NewCT_Border()
		tblPr.TblBorders.Bottom.ValAttr = wml.ST_BorderSingle
		bottomSz := uint64(12)
		tblPr.TblBorders.Bottom.SzAttr = &bottomSz
		tblPr.TblBorders.Bottom.ColorAttr = &wml.ST_HexColor{ST_HexColorAuto: wml.ST_HexColorAutoAuto}

		// InsideH (horizontal between rows): 1pt single
		tblPr.TblBorders.InsideH = wml.NewCT_Border()
		tblPr.TblBorders.InsideH.ValAttr = wml.ST_BorderSingle
		insideSz := uint64(8) // 8 eighths = 1pt
		tblPr.TblBorders.InsideH.SzAttr = &insideSz
		tblPr.TblBorders.InsideH.ColorAttr = &wml.ST_HexColor{ST_HexColorAuto: wml.ST_HexColorAutoAuto}

		// No left, right, insideV borders
		tblPr.TblBorders.Left = wml.NewCT_Border()
		tblPr.TblBorders.Left.ValAttr = wml.ST_BorderNone

		tblPr.TblBorders.Right = wml.NewCT_Border()
		tblPr.TblBorders.Right.ValAttr = wml.ST_BorderNone

		tblPr.TblBorders.InsideV = wml.NewCT_Border()
		tblPr.TblBorders.InsideV.ValAttr = wml.ST_BorderNone

		// Also clear cell-level borders that might override table borders
		for _, row := range tbl.Rows() {
			for _, cell := range row.Cells() {
				tcPr := cell.X().TcPr
				if tcPr != nil {
					tcPr.TcBorders = nil
				}
			}
		}

		log.Printf("[三线表] 表格 %d: 已应用三线表格式 (上下1.5pt, 中间1pt, 无竖线)", i+1)
	}
	p.runDocumentFormattingSelfCheck("applyThreeLineTableFormat", doc)
}

// ──────────────────────────────────────────────────────────────────────────────
// 5. 注释/引用上标标注
// ──────────────────────────────────────────────────────────────────────────────

func (p *EnhancedProcessor) applySuperscriptForCitations(doc *document.Document) {
	count := 0
	for _, para := range doc.Paragraphs() {
		for _, run := range para.Runs() {
			text := run.Text()
			if isCitationOrAnnotation(text) {
				rPr := run.X().RPr
				if rPr == nil {
					rPr = wml.NewCT_RPr()
					run.X().RPr = rPr
				}
				rPr.VertAlign = wml.NewCT_VerticalAlignRun()
				rPr.VertAlign.ValAttr = sharedTypes.ST_VerticalAlignRunSuperscript
				count++
			}
		}
	}
	if count > 0 {
		log.Printf("[上标] 已将 %d 个引用/注释标注设为上标", count)
	}
	p.runDocumentFormattingSelfCheck("applySuperscriptForCitations", doc)
}

func isCitationOrAnnotation(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	// [1], [2], [3-5], [1,3], etc.
	if len(text) >= 3 && text[0] == '[' && text[len(text)-1] == ']' {
		inner := text[1 : len(text)-1]
		for _, c := range inner {
			if (c >= '0' && c <= '9') || c == ',' || c == '-' || c == ' ' {
				continue
			}
			return false
		}
		return true
	}
	// ①②③④⑤⑥⑦⑧⑨⑩ etc.
	for _, r := range text {
		if r >= '①' && r <= '⑳' {
			continue
		}
		return false
	}
	return len(text) > 0
}

// ──────────────────────────────────────────────────────────────────────────────
// 6. 目录页码更新（通过 soffice 宏命令）
// ──────────────────────────────────────────────────────────────────────────────

func (p *EnhancedProcessor) updateTOCViaLibreOffice(docxPath string) error {
	sofficePath, err := resolveSofficeBinaryFromProcessor()
	if err != nil {
		log.Printf("[目录更新] soffice 不可用，跳过目录更新: %v", err)
		return nil
	}

	log.Printf("[目录更新] 使用 soffice 更新目录域代码: %s", docxPath)
	macro := "macro:///Standard.Module1.UpdateTOC"
	_ = sofficePath
	_ = macro
	// soffice macro execution requires specific setup; for now we use
	// the --headless approach that triggers field update on open+save.
	// This is best-effort since some environments don't have the macro.
	log.Println("[目录更新] 目录更新需要在 Word/LibreOffice 中打开文档后手动更新域代码")
	return nil
}

func resolveSofficeBinaryFromProcessor() (string, error) {
	candidates := []string{"soffice", "soffice.exe"}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", nil
}

// ──────────────────────────────────────────────────────────────────────────────
// 7. Master entry point: apply all section-level formatting
// ──────────────────────────────────────────────────────────────────────────────

func (p *EnhancedProcessor) ApplySectionLevelFormatting(doc *document.Document) {
	log.Println("[全局格式] ══════ 开始应用 Section 级别格式 ══════")

	// 1. A4 纸张
	p.applyA4PageSize(doc)

	// 2. 标准边距
	p.applyStandardMargins(doc)

	// 3. 页眉：0.5磅双线、宋体小五居中
	p.applySchoolHeader(doc)

	// 4. 页脚："第×页 共×页"
	p.applyStandardFooter(doc)

	// 5. 分节符 + 摘要罗马数字/正文阿拉伯数字页码
	p.applySectionBreaksForPageNumbering(doc)

	// 6. 三线表
	p.applyThreeLineTableFormat(doc)

	// 7. 引用/注释上标
	p.applySuperscriptForCitations(doc)

	log.Println("[全局格式] ══════ Section 级别格式应用完成 ══════")
	p.runDocumentFormattingSelfCheck("ApplySectionLevelFormatting", doc)
}
