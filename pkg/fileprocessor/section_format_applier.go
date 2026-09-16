package fileprocessor

import (
	"fmt"
	"log"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/ofc/sharedTypes"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

// 🔒 LOCKED: 默认页边距常量（非模板路径兜底，模板路径由 applyPageSetup 从 rules 覆盖）
const (
	defaultMarginTop    = 2.5  // cm
	defaultMarginBottom = 2.5  // cm
	defaultMarginLeft   = 2.5  // cm
	defaultMarginRight  = 2.5  // cm
	defaultHeaderDist   = 1.5  // cm
	defaultFooterDist   = 1.75 // cm
)

// defaultHeaderFooterFont 从 defaultParagraphFormatSpecs 获取页眉/页脚兜底字体参数。
// 🔒 LOCKED: 非模板路径兜底；模板路径由 applyHeaderFooter 从 templateprofile 覆盖。
func defaultHeaderFooterFont() (fontName string, fontSizePt float64) {
	spec := defaultParagraphFormatSpecs()["header"]
	fontName = spec.FontEastAsia
	if spec.FontSizeHalfPt > 0 {
		fontSizePt = float64(spec.FontSizeHalfPt) / 2.0
	}
	if fontName == "" {
		fontName = "宋体"
	}
	if fontSizePt <= 0 {
		fontSizePt = 9.0
	}
	return
}

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
	// 🔒 LOCKED: 页边距取自包级常量 defaultMargin*（非模板路径兜底）
	section.SetPageMargins(
		measurement.Distance(defaultMarginTop)*measurement.Centimeter,
		measurement.Distance(defaultMarginBottom)*measurement.Centimeter,
		measurement.Distance(defaultMarginLeft)*measurement.Centimeter,
		measurement.Distance(defaultMarginRight)*measurement.Centimeter,
		measurement.Distance(defaultHeaderDist)*measurement.Centimeter,
		measurement.Distance(defaultFooterDist)*measurement.Centimeter,
		0, // gutter
	)
	log.Printf("[页面设置] 标准边距已应用: 上%.1f/下%.1f/左%.1f/右%.1f cm, 页眉%.1fcm, 页脚%.1fcm",
		defaultMarginTop, defaultMarginBottom, defaultMarginLeft, defaultMarginRight,
		defaultHeaderDist, defaultFooterDist)
	p.runDocumentFormattingSelfCheck("applyStandardMargins", doc)
}

// ──────────────────────────────────────────────────────────────────────────────
// 2. 页眉：0.5磅双线、宋体小五居中
// ──────────────────────────────────────────────────────────────────────────────

func (p *EnhancedProcessor) buildDoubleLineHeaderParagraph(hdr document.Header, text string, fontName string, fontSize float64) {
	p.buildDoubleLineHeaderParagraphEx(hdr, text, fontName, fontSize, false)
}

func (p *EnhancedProcessor) buildDoubleLineHeaderParagraphEx(hdr document.Header, text string, fontName string, fontSize float64, underline bool) {
	hdr.Clear()
	para := hdr.AddParagraph()
	leftText, rightText, split := strings.Cut(text, "\t")
	if split {
		para.Properties().SetAlignment(wml.ST_JcLeft)
		para.Properties().AddTabStop(headerRightTabPosition(hdr), wml.ST_TabJcRight, wml.ST_TabTlcNone)
	} else {
		para.Properties().SetAlignment(wml.ST_JcCenter)
	}

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

	addTextRun := func(value string) {
		run := para.AddRun()
		run.AddText(value)
		p.setRunFont(run, fontName, fontSize, false)
		if underline {
			p.setRunUnderline(run)
		}
	}
	addTextRun(leftText)
	if split {
		tabRun := para.AddRun()
		tabRun.AddTab()
		p.setRunFont(tabRun, fontName, fontSize, false)
		addTextRun(strings.TrimSpace(rightText))
	}
}

func headerRightTabPosition(hdr document.Header) measurement.Distance {
	const defaultTextWidthTwips uint64 = 9070
	if hdr.Document == nil {
		return measurement.Distance(defaultTextWidthTwips) * measurement.Twips
	}
	sectPr := hdr.Document.BodySection().X()
	if sectPr == nil || sectPr.PgSz == nil || sectPr.PgSz.WAttr == nil ||
		sectPr.PgSz.WAttr.ST_UnsignedDecimalNumber == nil || sectPr.PgMar == nil ||
		sectPr.PgMar.LeftAttr.ST_UnsignedDecimalNumber == nil ||
		sectPr.PgMar.RightAttr.ST_UnsignedDecimalNumber == nil {
		return measurement.Distance(defaultTextWidthTwips) * measurement.Twips
	}
	width := *sectPr.PgSz.WAttr.ST_UnsignedDecimalNumber
	left := *sectPr.PgMar.LeftAttr.ST_UnsignedDecimalNumber
	right := *sectPr.PgMar.RightAttr.ST_UnsignedDecimalNumber
	if width <= left+right {
		return measurement.Distance(defaultTextWidthTwips) * measurement.Twips
	}
	return measurement.Distance(width-left-right) * measurement.Twips
}

// applySchoolHeader 旧路径硬编码页眉（双线下划线、宋体 9pt）。
// Deprecated: 仅非模板流程使用（includeDefaultHeaderFooter=true）。
// 模板路径的页眉由 enhanced_processor.go 中的 applyHeaderFooter 从 templateprofile 规则驱动。
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

	college := templateprofile.ExtractCollegeName(p.templateHeaderText, "重庆人文科技学院")
	headerText := college + gradeYear + "届" + major + "专业本科毕业论文"
	log.Printf("[页眉] 自动生成页眉: %q", headerText)

	hdr := doc.AddHeader()
	// 🔒 LOCKED: 页眉字体取自 defaultParagraphFormatSpecs["header"]（非模板路径兜底）
	fontName, fontSize := defaultHeaderFooterFont()
	p.buildDoubleLineHeaderParagraph(hdr, headerText, fontName, fontSize)
	section.SetHeader(hdr, wml.ST_HdrFtrDefault)
	p.runDocumentFormattingSelfCheck("applySchoolHeader", doc)
}

// ──────────────────────────────────────────────────────────────────────────────
// 3. 页脚：第×页 共×页，宋体小五居中
// ──────────────────────────────────────────────────────────────────────────────

// applyStandardFooter 旧路径硬编码页脚（"第×页 共×页"、宋体 9pt）。
// Deprecated: 仅非模板流程使用（includeDefaultHeaderFooter=true）。
// 模板路径的页脚由 enhanced_processor.go 中的 applyHeaderFooter 从 templateprofile 规则驱动。
func (p *EnhancedProcessor) applyStandardFooter(doc *document.Document) {
	section := doc.BodySection()

	ftr := doc.AddFooter()
	section.SetFooter(ftr, wml.ST_HdrFtrDefault)

	para := ftr.AddParagraph()
	para.Properties().SetAlignment(wml.ST_JcCenter)

	// 🔒 LOCKED: 页脚字体取自 defaultParagraphFormatSpecs["header"]（非模板路径兜底）
	fontName, fontSize := defaultHeaderFooterFont()

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

	log.Printf("[页脚] 已设置: 第×页 共×页, %s %.0fpt 居中", fontName, fontSize)
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
		// The source may already have a section break immediately before the
		// first body heading. Avoid creating an empty adjacent section, which
		// changes header/footer inheritance and page numbering.
		hasPreviousBreak := bodyStartIdx > 0 && paragraphs[bodyStartIdx-1].X().PPr != nil &&
			paragraphs[bodyStartIdx-1].X().PPr.SectPr != nil
		if hasPreviousBreak {
			log.Printf("[分节] 正文前已有分节符，跳过重复插入（段落索引=%d）", bodyStartIdx)
		} else {
			p.insertSectionBreakBefore(paragraphs[bodyStartIdx], wml.ST_NumberFormatDecimal)
			log.Printf("[分节] 正文前插入分节符 (阿拉伯数字页码从1开始), 段落索引=%d", bodyStartIdx)
		}
		// 注意：此处不提前 return——正文前已有分节符只是跳过正文分节插入，
		// 仍须继续执行下文 A6（参考文献/致谢分页、文末空段收敛）。
	}

	// ── A6: 参考文献 / 致谢 章节另起一页 ──
	// 第一遍循环在定位到摘要与正文页后即提前 break，参考文献/致谢标题在其后，
	// 需第二遍扫描定位它们的标题段，并分别在其前插入不重置页码的 nextPage
	// 分节符（正文分节之外的最小改动），保证这两个章节与前面内容强制分页。
	// 标题采用"精确匹配优先，宽松回退兜底"：优先整行独立成章的 "参考文献" /
	// "致谢"；回退匹配时排除目录条目（如 "参考文献12" 这类紧凑带页码文本）。
	refsIdx, ackIdx := -1, -1
	exactRefs, exactAck := false, false
	for i, para := range paragraphs {
		text := strings.TrimSpace(p.extractParagraphText(para))
		if text == "" {
			continue
		}
		// 标题以居中"致      谢"、加"致  谢"等多空格变体书写时，精确比较
		// 会漏检（extractParagraphText 保留字内空白）。剔除所有空白后再比较，
		// 同时规避目录条目（"致谢13"/"致      谢13" 压平后带页码，不会命中）；
		// 宽松回退分支保持不变。
		compact := strings.Join(strings.Fields(text), "")
		if compact == "参考文献" && !exactRefs {
			refsIdx = i
			exactRefs = true
		}
		if compact == "致谢" && !exactAck {
			ackIdx = i
			exactAck = true
		}
	}
	if !exactRefs {
		for i, para := range paragraphs {
			text := strings.TrimSpace(p.extractParagraphText(para))
			if text == "" || refsIdx >= 0 {
				continue
			}
			// 宽松回退：以"参考文献"开头且字数受限、且不含半角数字/目录引导点，
			// 规避目录条目（"参考文献12"、"参考文献………23"）误伤。
			if strings.HasPrefix(text, "参考文献") && len([]rune(text)) <= 6 &&
				!strings.ContainsAny(text, "0123456789．.…") {
				refsIdx = i
			}
		}
	}
	if !exactAck {
		for i, para := range paragraphs {
			text := strings.TrimSpace(p.extractParagraphText(para))
			if text == "" || ackIdx >= 0 {
				continue
			}
			if strings.Contains(text, "致谢") && len([]rune(text)) <= 6 &&
				!strings.ContainsAny(text, "0123456789．.…") {
				ackIdx = i
			}
		}
	}
	if refsIdx >= 0 && !hasSectionBreakBefore(paragraphs, refsIdx) {
		p.insertSectionBreakInheritPageNumber(paragraphs[refsIdx])
		log.Printf("[A6][分节] 参考文献前插入分节符 (另起一页, 页码续接), 段落索引=%d", refsIdx)
	}
	if ackIdx >= 0 && !hasSectionBreakBefore(paragraphs, ackIdx) {
		p.insertSectionBreakInheritPageNumber(paragraphs[ackIdx])
		log.Printf("[A6][分节] 致谢前插入分节符 (另起一页, 页码续接), 段落索引=%d", ackIdx)
		p.normalizeAcknowledgementBody(doc, ackIdx)
	} else if ackIdx >= 0 {
		p.normalizeAcknowledgementBody(doc, ackIdx)
	}
	// A6: 收敛文末多余空段（不生成/移除尾部空段，保留正文处 final 断点不被破坏）
	p.removeTrailingEmptyParagraphs(doc)
	p.runDocumentFormattingSelfCheck("applySectionBreaksForPageNumbering", doc)
}

// hasSectionBreakBefore 报告 idx 段之前是否已存在段落级分节符，
// 用于避免重复插入产生空节（A6 references/ack 分节的重复插入保护）。
func hasSectionBreakBefore(paragraphs []document.Paragraph, idx int) bool {
	if idx <= 0 {
		return false
	}
	prev := paragraphs[idx-1].X().PPr
	return prev != nil && prev.SectPr != nil
}

// insertSectionBreakInheritPageNumber 在段落前插入 nextPage 分节符，且
// 不携带 PgNumType（页码延续上一节），用于参考文献/致谢"另起一页"的最小改动。
func (p *EnhancedProcessor) insertSectionBreakInheritPageNumber(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	sectPr := wml.NewCT_SectPr()
	sectPr.Type = wml.NewCT_SectType()
	sectPr.Type.ValAttr = wml.ST_SectionMarkNextPage
	pPr.SectPr = sectPr
}

// normalizeAcknowledgementBody 将"致谢"标题段之后的正文段落统一为
// 两端对齐 + 宋体小四（24 半磅），符合模板致谢正文规范。
//
// 与 removeTrailingEmptyParagraphs 同理：不能依赖 doc.Paragraphs() 的乱序
// 列表遍历——表内段落会拼在 body 顶层段落之后，若按 ackIdx 直接向后扫会把
// 正文表格中的说明段也强制改写。这里以 Body.EG_BlockLevelElts 物理顺序限定
// 范围：从致谢标题段向后，遇分节边界或下一结构标题（参考文献/附录）即停。
func (p *EnhancedProcessor) normalizeAcknowledgementBody(doc *document.Document, ackIdx int) {
	paras := doc.Paragraphs()
	if ackIdx < 0 || ackIdx >= len(paras) {
		return
	}
	ackElem := paras[ackIdx].X()
	body := doc.X().Body
	if body == nil {
		return
	}
	elemToPara := make(map[*wml.CT_P]document.Paragraph, len(paras))
	for _, pa := range paras {
		elemToPara[pa.X()] = pa
	}
	var order []*wml.CT_P
	for _, ebl := range body.EG_BlockLevelElts {
		for _, cb := range ebl.EG_ContentBlockContent {
			order = append(order, cb.P...)
		}
	}
	start := -1
	for i, ct := range order {
		if ct == ackElem {
			start = i
			break
		}
	}
	if start < 0 {
		return
	}
	changed := 0
	for i := start + 1; i < len(order); i++ {
		para, ok := elemToPara[order[i]]
		if !ok {
			continue
		}
		if order[i].PPr != nil && order[i].PPr.SectPr != nil {
			break // 遇分节边界，致谢节结束
		}
		text := strings.TrimSpace(p.extractParagraphText(para))
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "参考文献") || strings.HasPrefix(text, "附录") ||
			(strings.Contains(text, "致谢") && len([]rune(text)) <= 20) {
			break // 下一结构标题，停止，避免越界改写其他章节
		}
		if p.applyAcknowledgementBodyFormat(para) {
			changed++
		}
	}
	if changed > 0 {
		log.Printf("[A6] 致谢正文两端对齐宋体小四: %d 段", changed)
	}
}

// applyAcknowledgementBodyFormat 对单段致谢正文强制 jc=both 与 宋体 sz=24(小四)，
// 返回是否发生写入（供调用方判断是否确有变更）。
func (p *EnhancedProcessor) applyAcknowledgementBodyFormat(para document.Paragraph) bool {
	changed := false
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	if pPr.Jc == nil {
		pPr.Jc = wml.NewCT_Jc()
	}
	if pPr.Jc.ValAttr != wml.ST_JcBoth {
		pPr.Jc.ValAttr = wml.ST_JcBoth
		changed = true
	}
	eastAsiaPtr := p.getCachedFontName("宋体")
	for _, r := range para.Runs() {
		rPr := r.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			r.X().RPr = rPr
		}
		if rPr.RFonts == nil {
			rPr.RFonts = wml.NewCT_Fonts()
		}
		if rPr.RFonts.EastAsiaAttr == nil || *rPr.RFonts.EastAsiaAttr != "宋体" {
			rPr.RFonts.EastAsiaAttr = eastAsiaPtr
			changed = true
		}
		if rPr.Sz == nil {
			rPr.Sz = wml.NewCT_HpsMeasure()
		}
		if (rPr.Sz.ValAttr.ST_UnsignedDecimalNumber == nil && rPr.Sz.ValAttr.ST_PositiveUniversalMeasure == nil) ||
			*rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != 24 {
			sz := uint64(24)
			rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &sz
			changed = true
		}
	}
	// 空 run 无文字时也保证至少 pPr 已写
	return changed
}

// removeTrailingEmptyParagraphs 从文档末尾向前移除连续空段（无可见文字，
// 且非分节边界段落），收敛 A6 中"文末 3 个空段"问题。
//
// 注意：document.Document.Paragraphs() 返回顺序是先 body 顶层段、再按
// 表格/行/单元格拼接的表内段，并非 document.xml 的真实文档顺序——若直接
// 取其"末尾"，会误删统计表/致谢前段。因此这里以 Body.EG_BlockLevelElts
// 的物理顺序收集 body 顶层段落，从真正的文末向前收敛。
func (p *EnhancedProcessor) removeTrailingEmptyParagraphs(doc *document.Document) {
	body := doc.X().Body
	if body == nil {
		return
	}
	paras := doc.Paragraphs()
	elemToPara := make(map[*wml.CT_P]document.Paragraph, len(paras))
	for _, pa := range paras {
		elemToPara[pa.X()] = pa
	}
	// 按物理顺序收集 body 顶层段落底层元素（w:tbl 之后的内容才是文末范围）
	var order []*wml.CT_P
	for _, ebl := range body.EG_BlockLevelElts {
		for _, cb := range ebl.EG_ContentBlockContent {
			order = append(order, cb.P...)
		}
	}
	removed := 0
	for i := len(order) - 1; i >= 0; i-- {
		ct := order[i]
		if ct.PPr != nil && ct.PPr.SectPr != nil {
			break // 分节边界段落需保留（body sectPr）
		}
		para, ok := elemToPara[ct]
		if !ok {
			break
		}
		if strings.TrimSpace(p.extractParagraphText(para)) != "" {
			break
		}
		doc.RemoveParagraph(para)
		removed++
	}
	if removed > 0 {
		log.Printf("[A6] 移除文末空段 %d 个", removed)
	}
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

// shdFillIsCustom 判断单元格底纹是否为"非默认"（非 auto/无/白色），
// 用于识别学生手工设置的表头/单元格底色（D16 保护判定）。
func shdFillIsCustom(shd *wml.CT_Shd) bool {
	if shd == nil || shd.FillAttr == nil {
		return false
	}
	if shd.FillAttr.ST_HexColorAuto == wml.ST_HexColorAutoAuto || string(shd.FillAttr.ST_HexColorAuto) != "" {
		return false // 显式 auto（默认无底纹）或其他自动值
	}
	if shd.FillAttr.ST_HexColorRGB != nil {
		fill := strings.ToUpper(strings.TrimSpace(*shd.FillAttr.ST_HexColorRGB))
		if fill == "" || fill == "FFFFFF" {
			return false // 无值或白色，视为默认
		}
		return true
	}
	return false
}

// hasManualTableLayoutFeatures 检测目标表格是否存在"学生手工排版特征"。
// 命中任意一项即认为该表格含手工排版，三线表重写会跳过它（D16，默认保守）：
//  1. 表格级已自定义边框（自绘边框结构）；
//  2. 合并单元格（GridSpan / 水平 / 垂直合并）；
//  3. 单元格级自定义边框；
//  4. 单元格自定义底纹（表头底色等）。
func hasManualTableLayoutFeatures(tbl document.Table) bool {
	tblPr := tbl.X().TblPr
	if tblPr != nil && tblPr.TblBorders != nil {
		return true // 已有表格级自定义边框
	}
	for _, row := range tbl.Rows() {
		for _, cell := range row.Cells() {
			tcPr := cell.X().TcPr
			if tcPr == nil {
				continue
			}
			if tcPr.GridSpan != nil || tcPr.HMerge != nil || tcPr.VMerge != nil {
				return true // 合并单元格
			}
			if tcPr.TcBorders != nil {
				return true // 单元格级自定义边框
			}
			if shdFillIsCustom(tcPr.Shd) {
				return true // 自定义底纹（如表头底色）
			}
		}
	}
	return false
}

func (p *EnhancedProcessor) applyThreeLineTableFormat(doc *document.Document) {
	tables := doc.Tables()
	if len(tables) == 0 {
		return
	}

	for i, tbl := range tables {
		if hasManualTableLayoutFeatures(tbl) {
			// D16：检测到学生手工排版特征（自定义边框/合并单元格/底纹），
			// 不强制执行三线表重写，避免覆盖有意的手工排版（默认保守）。
			log.Printf("[三线表] 表格 %d: 检测到手工排版特征，跳过三线表重写（保留学生原排版）", i+1)
			continue
		}

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

// pageUsableWidthTwips 版心可用宽：pgSz w=11906，pgMar left/right=1418，
// 11906 - 1418×2 = 9070 twips（与 final.docx 版心一致）。
const pageUsableWidthTwips = 9070

// gridColWidth 读取 gridCol 声明宽度（twips），缺失时返回 0。
func gridColWidth(c *wml.CT_TblGridCol) uint64 {
	if c == nil || c.WAttr == nil || c.WAttr.ST_UnsignedDecimalNumber == nil {
		return 0
	}
	return *c.WAttr.ST_UnsignedDecimalNumber
}

// setGridColWidth 覆写 gridCol 宽度（twips）。
func setGridColWidth(c *wml.CT_TblGridCol, w uint64) {
	if c == nil {
		return
	}
	if c.WAttr == nil {
		c.WAttr = &sharedTypes.ST_TwipsMeasure{}
	}
	c.WAttr.ST_UnsignedDecimalNumber = &w
}

// wmlUnsignedFromMeasurementOrPercent 从 TblW.WAttr 中提取数值（dxa 裸数值），
// 百分比型（type=pct）返回 nil，避免把百分比误当 twips。
func wmlUnsignedFromMeasurementOrPercent(m *wml.ST_MeasurementOrPercent) *uint64 {
	if m == nil || m.ST_DecimalNumberOrPercent == nil {
		return nil
	}
	if dnp := m.ST_DecimalNumberOrPercent; dnp.ST_UnqualifiedPercentage != nil {
		v := uint64(*dnp.ST_UnqualifiedPercentage)
		return &v
	}
	return nil
}

// normalizeOverflowTableColumns 修复三线表数据溢出（A5）。
//
// 病症：学生三线表 tblW 声明合理表宽（研究因素 5750 / 回归 5000 dxa），
// 但 tblGrid/gridCol 合计分别为 10757 / 9354，远超标宽且超出/逼近版心 9070；
// tblLayout=fixed 按 gridCol 渲染导致表格右缘溢出页面边界。
//
// 修复：对 fixed 布局三线表，以 tblW 声明值为目标（非法/缺省时回退版心 9070），
// 将 gridCol 各列等比缩放并让合计恰好等于目标；同时把 tblW 规范为
// w:type="dxa"、值回写为缩放后合计，保证 Word 渲染与 gridCol 自洽。
// 仅动列宽与表宽，绝不触碰表格线（applyThreeLineTableFormat 的三线表边框保留）。
// 非 fixed 布局（自动布局）由 Word 按内容分配宽度，default 跳过不做干预。
func (p *EnhancedProcessor) normalizeOverflowTableColumns(doc *document.Document) {
	changed := 0
	for i, tbl := range doc.Tables() {
		tblPr := tbl.X().TblPr
		if tblPr == nil || tbl.X().TblGrid == nil {
			continue
		}
		cols := tbl.X().TblGrid.GridCol
		if len(cols) == 0 {
			continue
		}
		// 仅处理 fixed 布局（auto/fml 布局由 Word 自行分配，不动）
		if tblPr.TblLayout == nil || tblPr.TblLayout.TypeAttr != wml.ST_TblLayoutTypeFixed {
			continue
		}
		// 目标表宽：优先取 tblW 声明值；非法（0 / 超版心）时回退版心 9070
		target := uint64(0)
		if tblPr.TblW != nil {
			if uv := wmlUnsignedFromMeasurementOrPercent(tblPr.TblW.WAttr); uv != nil {
				target = *uv
			}
		}
		if target == 0 || target > pageUsableWidthTwips {
			target = pageUsableWidthTwips
		}
		// 当前 gridCol 合计
		var sum uint64
		for _, c := range cols {
			sum += gridColWidth(c)
		}
		if sum == 0 || sum <= target {
			continue // 未溢出：不缩放，保持学生原列宽
		}
		// 等比缩放（四舍五入），末列回填余量保证合计恰好=target
		var acc uint64
		for ci, c := range cols {
			w := gridColWidth(c)
			var newW uint64
			if ci == len(cols)-1 {
				newW = target - acc
			} else {
				newW = uint64(float64(w)*float64(target)/float64(sum) + 0.5)
				if acc+newW > target {
					newW = target - acc
				}
			}
			setGridColWidth(c, newW)
			acc += newW
		}
		// tblW 规范为 dxa、值=缩放后合计（与原声明值一致）
		if tblPr.TblW == nil {
			tblPr.TblW = wml.NewCT_TblWidth()
		}
		tblPr.TblW.TypeAttr = wml.ST_TblWidthDxa
		tblPr.TblW.WAttr = &wml.ST_MeasurementOrPercent{
			ST_DecimalNumberOrPercent: &wml.ST_DecimalNumberOrPercent{
				ST_UnqualifiedPercentage: func() *int64 { v := int64(target); return &v }(),
			},
		}
		log.Printf("[三线表] 表格 %d: 列宽溢出修复 gridCol 合计 %d→%d twips (表宽 %d)", i+1, sum, acc, target)
		changed++
	}
	if changed > 0 {
		log.Printf("[三线表] 共修复 %d 张 fixed 布局溢出三线表列宽", changed)
	}
	p.runDocumentFormattingSelfCheck("normalizeOverflowTableColumns", doc)
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
// 7. Master entry point: apply all section-level formatting
// ──────────────────────────────────────────────────────────────────────────────

func (p *EnhancedProcessor) ApplySectionLevelFormatting(doc *document.Document) error {
	return p.applySectionLevelFormatting(doc, true)
}

// ApplyTemplateSectionLevelFormatting 保留页面、分节、表格等全局处理，
// 但页眉页脚只允许后续从模板 Rule Engine 写入，避免生成硬编码孤立部件。
func (p *EnhancedProcessor) ApplyTemplateSectionLevelFormatting(doc *document.Document) error {
	return p.applySectionLevelFormatting(doc, false)
}

func (p *EnhancedProcessor) applySectionLevelFormatting(doc *document.Document, includeDefaultHeaderFooter bool) error {
	log.Println("[全局格式] ══════ 开始应用 Section 级别格式 ══════")

	// 前置验证：文档是否存在 valid body section
	section := doc.BodySection()
	if section.WSection == nil {
		return fmt.Errorf("applySectionLevelFormatting: 文档缺少有效的 body section")
	}

	var errs []string

	// 1. 非模板兜底路径才强制 A4；模板路径的纸张尺寸由模板 Profile 写入。
	if strings.TrimSpace(p.templatePath) == "" {
		p.applyA4PageSize(doc)
		sectPr := section.X()
		if sectPr == nil || sectPr.PgSz == nil ||
			sectPr.PgSz.WAttr == nil || sectPr.PgSz.HAttr == nil {
			errs = append(errs, "A4纸张设置后验证失败：PgSz 缺失")
		} else if *sectPr.PgSz.WAttr.ST_UnsignedDecimalNumber != 11906 ||
			*sectPr.PgSz.HAttr.ST_UnsignedDecimalNumber != 16838 {
			errs = append(errs, "A4纸张设置后验证失败：尺寸不匹配 (预期 11906×16838 twips)")
		}
	}

	if includeDefaultHeaderFooter {
		// 2. 标准边距（仅非模板路径：模板路径的边距由 applyPageSetup 统一处理，避免 sectPr 双重写入）
		p.applyStandardMargins(doc)

		// 3. 页眉：0.5磅双线、宋体小五居中
		p.applySchoolHeader(doc)

		// 4. 页脚："第×页 共×页"
		p.applyStandardFooter(doc)
	}

	// 5. 分节符 + 摘要罗马数字/正文阿拉伯数字页码 + A6 参考文献/致谢分页
	p.applySectionBreaksForPageNumbering(doc)

	// 5b. A1: 封面"题目"表格 schema 修复（行高统一/垂直居中/去垫高）
	// 放在三线表之前，避免三线表判断先改写封面表；仅首个表格具备
	// 题目/课题名称标签时才生效，其余文档不受影响。
	p.applyCoverTitleSchemaFix(doc)

	// 6. 三线表
	p.applyThreeLineTableFormat(doc)

	// 6b. A5: fixed 布局三线表列宽归约（gridCol 溢出版心 → 按表宽等比缩放）
	p.normalizeOverflowTableColumns(doc)

	// 7. 引用/注释上标
	p.applySuperscriptForCitations(doc)

	log.Println("[全局格式] ══════ Section 级别格式应用完成 ══════")
	p.runDocumentFormattingSelfCheck("ApplySectionLevelFormatting", doc)

	if len(errs) > 0 {
		return fmt.Errorf("applySectionLevelFormatting: %d 个步骤验证失败: %s", len(errs), strings.Join(errs, "; "))
	}
	return nil
}
