package fileprocessor

import (
	"log"
	"regexp"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/ofc/sharedTypes"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

// V2SmartFormatter 智能格式化器：处理需要特殊逻辑的段落（题目、摘要、页眉等）
type V2SmartFormatter struct {
	processor *EnhancedProcessor
}

func NewV2SmartFormatter(proc *EnhancedProcessor) *V2SmartFormatter {
	return &V2SmartFormatter{processor: proc}
}

// ApplySmartFormatting 在XML克隆之后，对特殊段落做精准格式化
func (f *V2SmartFormatter) ApplySmartFormatting(doc *document.Document, classified []V2ClassifiedPara) {
	// #region agent log
	coverCount, absCount, h1Count := 0, 0, 0
	for _, c := range classified {
		switch c.Type {
		case V2Cover:
			coverCount++
		case V2Abstract, V2AbstractTitle:
			absCount++
		case V2Heading1:
			h1Count++
		}
	}
	debugLog("v2_smart_format.go:ApplySmartFormatting", "H3_SMART_FMT_ENTRY", map[string]interface{}{
		"hypothesisId": "H3",
		"totalParas":   len(classified),
		"coverCount":   coverCount,
		"abstractCount": absCount,
		"heading1Count": h1Count,
	})
	// #endregion

	f.formatThesisTitle(classified)
	f.formatAbstract(classified)
	f.formatHeading1(classified)
	f.formatTOC(classified)
	f.formatSmartHeader(doc, classified)
	log.Println("[V2智能格式] 特殊段落格式化完成")
}

// ── 1. 题目格式化：正标题 + 副标题 ──

func (f *V2SmartFormatter) formatThesisTitle(classified []V2ClassifiedPara) {
	for i := range classified {
		text := classified[i].Text
		if text == "" {
			continue
		}
		if classified[i].Type == V2ThesisTitle {
			f.applyTitleFormat(classified[i].Para, true)
			log.Printf("[V2智能格式] 正标题: %s", truncStr(text, 30))
		} else if classified[i].Type == V2ThesisSubtitle {
			f.applyTitleFormat(classified[i].Para, false)
			log.Printf("[V2智能格式] 副标题: %s", truncStr(text, 30))
		}
	}
}

func isThesisMainTitle(text string) bool {
	if strings.Contains(text, "本科毕业论文") || strings.Contains(text, "本科毕业设计") {
		return false
	}
	if strings.Contains(text, "学院") || strings.Contains(text, "专业") ||
		strings.Contains(text, "学号") || strings.Contains(text, "姓名") ||
		strings.Contains(text, "指导教师") || strings.Contains(text, "班级") {
		return false
	}
	if strings.Contains(text, "年") && strings.Contains(text, "月") && len([]rune(text)) < 15 {
		return false
	}
	// 题目通常是封面中独立的一行，字数 > 4 且 < 50
	runes := []rune(text)
	return len(runes) >= 4 && len(runes) <= 50
}

func isThesisSubtitle(text string) bool {
	return strings.HasPrefix(text, "——") || strings.HasPrefix(text, "—") ||
		strings.HasPrefix(text, "--")
}

// applyTitleFormat 应用题目格式
// isMain: true=正标题(黑体三号加粗居中段前1行), false=副标题(黑体小三加粗居中段后2行)
func (f *V2SmartFormatter) applyTitleFormat(para document.Paragraph, isMain bool) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}

	// 居中
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcCenter

	// 行距：单倍行距
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(240) // 单倍行距 = 240 twips
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto

	if isMain {
		// 段前 1 行 = 1 line = ~312 twips (based on 三号=16pt)
		before := uint64(312)
		pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber = &before
	} else {
		// 段后 2 行 = ~624 twips
		after := uint64(624)
		pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after
	}

	// Run属性：黑体，加粗
	var fontSize float64
	if isMain {
		fontSize = 16.0 // 三号 = 16pt
	} else {
		fontSize = 15.0 // 小三号 = 15pt
	}
	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, "黑体", fontSize, true)
	}
}

// ── 2. 摘要格式化 ──

func (f *V2SmartFormatter) formatAbstract(classified []V2ClassifiedPara) {
	for i := range classified {
		switch classified[i].Type {
		case V2AbstractTitle:
			f.applyAbstractTitleFormat(classified[i].Para)
		case V2Abstract:
			f.applyAbstractContentFormat(classified[i].Para)
		case V2Keywords:
			f.applyKeywordsFormat(classified[i].Para)
		case V2EnAbstractTitle:
			f.applyEnAbstractTitleFormat(classified[i].Para)
		case V2EnAbstract:
			f.applyEnAbstractContentFormat(classified[i].Para)
		case V2EnKeywords:
			f.applyEnKeywordsFormat(classified[i].Para)
		}
	}
}

// applyAbstractTitleFormat "摘要"二字：黑体小三号加粗居中，段前1行
func (f *V2SmartFormatter) applyAbstractTitleFormat(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcCenter

	pPr.Spacing = wml.NewCT_Spacing()
	before := uint64(312)
	pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber = &before

	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, "黑体", 15, true) // 小三号=15pt, 加粗
	}
	log.Printf("[V2智能格式] 摘要标题已格式化")
}

// applyAbstractContentFormat 摘要内容：宋体小四号，1.5倍行距，首行缩进2字符，段后2行
func (f *V2SmartFormatter) applyAbstractContentFormat(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcBoth // 两端对齐

	// 1.5倍行距
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360) // 1.5倍行距
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto

	// 段后2行
	after := uint64(624)
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	// 首行缩进2字符 = 480 twips (12pt * 2 * 20)
	pPr.Ind = wml.NewCT_Ind()
	firstLine := uint64(480)
	pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &firstLine

	// 智能处理 run 格式
	text := strings.TrimSpace(f.processor.extractParagraphText(para))
	runs := para.Runs()
	if strings.HasPrefix(text, "摘要") || strings.HasPrefix(text, "摘 要") {
		labelApplied := false
		for _, r := range runs {
			rText := r.Text()
			if !labelApplied && (strings.Contains(rText, "摘要") || strings.Contains(rText, "：") || strings.Contains(rText, ":")) {
				v2SetRunFont(f.processor, r, "黑体", 15, true)
				if strings.Contains(rText, "：") || strings.Contains(rText, ":") {
					labelApplied = true
				}
			} else {
				v2SetBodyRunFont(f.processor, r, 12, false)
				labelApplied = true
			}
		}
	} else {
		for _, r := range runs {
			v2SetBodyRunFont(f.processor, r, 12, false)
		}
	}
}

// applyKeywordsFormat 关键词格式：首行缩进2字符，1.5倍行距，段后2行
func (f *V2SmartFormatter) applyKeywordsFormat(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcBoth

	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto
	after := uint64(624) // 段后2行
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	pPr.Ind = wml.NewCT_Ind()
	firstLine := uint64(480)
	pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &firstLine

	runs := para.Runs()
	labelApplied := false
	for _, r := range runs {
		rText := r.Text()
		if !labelApplied && (strings.Contains(rText, "关键词") || strings.Contains(rText, "关键字")) {
			v2SetRunFont(f.processor, r, "黑体", 15, true)
			if strings.Contains(rText, "：") || strings.Contains(rText, ":") {
				labelApplied = true
			}
		} else {
			v2SetBodyRunFont(f.processor, r, 12, false)
			labelApplied = true
		}
	}
}

func (f *V2SmartFormatter) applyEnAbstractTitleFormat(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcCenter

	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, "Times New Roman", 15, true)
	}
}

func (f *V2SmartFormatter) applyEnAbstractContentFormat(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcBoth
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto
	after := uint64(624)
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	pPr.Ind = wml.NewCT_Ind()
	firstLine := uint64(480)
	pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &firstLine

	text := strings.TrimSpace(f.processor.extractParagraphText(para))
	lower := strings.ToLower(text)
	runs := para.Runs()
	if strings.HasPrefix(lower, "abstract") {
		labelApplied := false
		for _, r := range runs {
			rText := r.Text()
			if !labelApplied && (strings.Contains(strings.ToLower(rText), "abstract") || strings.Contains(rText, ":") || strings.Contains(rText, "：")) {
				v2SetRunFont(f.processor, r, "Times New Roman", 15, true)
				if strings.Contains(rText, ":") || strings.Contains(rText, "：") {
					labelApplied = true
				}
			} else {
				v2SetRunFont(f.processor, r, "Times New Roman", 12, false)
				labelApplied = true
			}
		}
	} else {
		for _, r := range runs {
			v2SetRunFont(f.processor, r, "Times New Roman", 12, false)
		}
	}
}

func (f *V2SmartFormatter) applyEnKeywordsFormat(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcBoth
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto
	after := uint64(624) // 段后2行
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after
	pPr.Ind = wml.NewCT_Ind()
	firstLine := uint64(480)
	pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &firstLine

	runs := para.Runs()
	labelApplied := false
	for _, r := range runs {
		rText := strings.ToLower(r.Text())
		if !labelApplied && (strings.Contains(rText, "keyword") || strings.Contains(rText, "key word")) {
			v2SetRunFont(f.processor, r, "Times New Roman", 15, true)
			if strings.Contains(rText, ":") || strings.Contains(rText, "：") {
				labelApplied = true
			}
		} else {
			v2SetRunFont(f.processor, r, "Times New Roman", 12, false)
			labelApplied = true
		}
	}
}

// ── 3. 一级标题格式（绪论等）：三号宋体加粗，1.5倍行距，段前1行段后1行 ──

func (f *V2SmartFormatter) formatHeading1(classified []V2ClassifiedPara) {
	for i := range classified {
		if classified[i].Type != V2Heading1 || classified[i].Text == "" {
			continue
		}
		para := classified[i].Para

		// #region agent log
		hasPBB := false
		hasStyleRef := ""
		if para.X().PPr != nil && para.X().PPr.PageBreakBefore != nil {
			hasPBB = true
		}
		if para.X().PPr != nil && para.X().PPr.PStyle != nil {
			hasStyleRef = para.X().PPr.PStyle.ValAttr
		}
		debugLog("v2_smart_format.go:formatHeading1", "H5_HEADING1_BEFORE", map[string]interface{}{
			"hypothesisId":    "H5",
			"text":            truncStr(classified[i].Text, 30),
			"hasPageBreakBefore": hasPBB,
			"styleRef":        hasStyleRef,
		})
		// #endregion

		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}

		// 顶格、三号、宋体、加粗、1.5倍行距、段前1行段后1行
		pPr.Jc = nil // 顶格（左对齐，不设置=默认左）

		pPr.Spacing = wml.NewCT_Spacing()
		lineVal := int64(360) // 1.5倍行距
		pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
		pPr.Spacing.LineAttr.Int64 = &lineVal
		pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto
		before := uint64(312) // 段前1行
		after := uint64(312)  // 段后1行
		pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber = &before
		pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
		pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

		// 去掉多余缩进
		pPr.Ind = nil

		// 不要分页符（避免绪论前空太多）
		pPr.PageBreakBefore = nil

		for _, r := range para.Runs() {
			v2SetRunFont(f.processor, r, "宋体", 16, true) // 三号=16pt, 加粗
		}
	}
}

// ── 4. 页眉智能化：从封面提取 届/专业 ──

var reGradeYear = regexp.MustCompile(`(\d{4})级`)

func (f *V2SmartFormatter) formatSmartHeader(doc *document.Document, classified []V2ClassifiedPara) {
	coverInfo := f.processor.extractCoverInfo(doc)

	// 从班级提取入学年份 → +4 = 毕业届
	gradeYear := ""
	if banJi, ok := coverInfo["班级"]; ok && banJi != "" {
		if m := reGradeYear.FindStringSubmatch(banJi); len(m) >= 2 {
			if year, err := strconv.Atoi(m[1]); err == nil {
				gradeYear = strconv.Itoa(year + 4)
			}
		}
	}
	if gradeYear == "" {
		gradeYear = "XXX"
	}

	major := coverInfo["专业"]
	if major == "" {
		major = "XXX"
	}

	// 判断是论文还是设计：检查封面文字
	// "本科毕业论文/设计" 同时包含两者，默认"论文"；只有纯"设计"才改
	docType := "本科毕业论文"
	for _, cp := range classified {
		if cp.Type == V2Cover {
			t := cp.Text
			if strings.Contains(t, "毕业设计") && !strings.Contains(t, "论文") {
				docType = "本科毕业设计"
				break
			}
		}
	}

	headerText := "重庆人文科技学院" + gradeYear + "届" + major + "专业" + docType
	log.Printf("[V2智能页眉] %q (班级=%q, 专业=%q)", headerText, coverInfo["班级"], major)

	// #region agent log
	debugLog("v2_smart_format.go:formatSmartHeader", "H4_HEADER_GENERATION", map[string]interface{}{
		"hypothesisId": "H4",
		"headerText":   headerText,
		"coverInfo":    coverInfo,
		"gradeYear":    gradeYear,
		"major":        major,
		"docType":      docType,
	})
	// #endregion

	// 清除原有页眉并设置新页眉
	section := doc.BodySection()
	sectPr := section.X()
	if sectPr != nil {
		sectPr.EG_HdrFtrReferences = nil
	}
	hdr := doc.AddHeader()
	f.processor.buildDoubleLineHeaderParagraph(hdr, headerText, "宋体", 9)
	section.SetHeader(hdr, wml.ST_HdrFtrDefault)
}

// ── 5. 目录格式化 ──

func (f *V2SmartFormatter) formatTOC(classified []V2ClassifiedPara) {
	for i := range classified {
		switch classified[i].Type {
		case V2TOCTitle:
			f.formatTOCTitle(classified[i].Para)
		case V2TOC:
			f.formatTOCEntry(classified[i].Para)
		}
	}
}

// formatTOCTitle "目录"二字：黑体，三号，居中，两字间空6个空格，段后2行
func (f *V2SmartFormatter) formatTOCTitle(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcCenter
	pPr.Ind = nil

	pPr.Spacing = wml.NewCT_Spacing()
	after := uint64(624) // 段后2行
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	// 修正文本为 "目      录"（两字间空6个空格）
	runs := para.Runs()
	fullText := ""
	for _, r := range runs {
		fullText += r.Text()
	}
	normalized := strings.ReplaceAll(strings.TrimSpace(fullText), " ", "")
	normalized = strings.ReplaceAll(normalized, "\u3000", "")
	if normalized == "目录" {
		for i, r := range runs {
			if i == 0 {
				r.ClearContent()
				r.AddText("目      录")
			} else {
				r.ClearContent()
			}
		}
	}

	for _, r := range runs {
		v2SetRunFont(f.processor, r, "黑体", 16, false) // 三号=16pt，不加粗
	}
	log.Printf("[V2智能格式] 目录标题已格式化")
}

// formatTOCEntry 目录内容：宋体五号，1.5倍行距，两端对齐
func (f *V2SmartFormatter) formatTOCEntry(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcBoth

	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360) // 1.5倍行距
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto

	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, "宋体", 10.5, false) // 五号=10.5pt
	}
}

// ── 6. 正文段落格式（全部段落类型）──

// ApplyBodyFormats 对全文所有段落应用标准格式
func (f *V2SmartFormatter) ApplyBodyFormats(classified []V2ClassifiedPara) {
	for i := range classified {
		switch classified[i].Type {
		case V2Heading2:
			f.formatHeading2(classified[i].Para)
		case V2Heading3:
			f.formatHeading3(classified[i].Para)
		case V2Heading4:
			f.formatHeading4(classified[i].Para)
		case V2Body:
			f.formatBodyPara(classified[i].Para)
		case V2ReferencesTitle:
			f.formatReferencesTitle(classified[i].Para)
		case V2References:
			f.formatReferenceItem(classified[i].Para)
		case V2TOCTitle:
			f.formatTOCTitle(classified[i].Para)
		case V2TOC:
			f.formatTOCEntry(classified[i].Para)
		case V2FigureCaption:
			f.formatCaption(classified[i].Para)
		case V2TableCaption:
			f.formatCaption(classified[i].Para)
		case V2AcknowledgementsTitle:
			f.formatSectionTitle(classified[i].Para)
		case V2Acknowledgements:
			f.formatBodyPara(classified[i].Para)
		case V2AppendixTitle:
			f.formatSectionTitle(classified[i].Para)
		case V2Appendix:
			f.formatBodyPara(classified[i].Para)
		case V2NotesTitle:
			f.formatSectionTitle(classified[i].Para)
		case V2Notes:
			f.formatNotesContent(classified[i].Para)
		}
	}
}

func (f *V2SmartFormatter) formatHeading2(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	// 模板克隆阶段可能保留学生稿上的 w:numPr；与正文里手打的「1.1 …」并存会导致编号重复显示，二级标题规范为数字前缀写在 runs 中，此处去掉列表编号。
	pPr.NumPr = nil
	pPr.Ind = nil
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto

	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, "宋体", 15, true) // 小三号=15pt, 加粗
	}
}

func (f *V2SmartFormatter) formatHeading3(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Ind = nil
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto

	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, "宋体", 14, true) // 四号=14pt, 加粗
	}
}

// formatHeading4 四级标题：四号宋体，不加粗，1.5倍行距
func (f *V2SmartFormatter) formatHeading4(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Ind = nil
	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto

	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, "宋体", 14, false) // 四号=14pt, 不加粗
	}
}

func (f *V2SmartFormatter) formatBodyPara(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcBoth

	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto

	pPr.Ind = wml.NewCT_Ind()
	fl := uint64(480)
	pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &fl

	for _, r := range para.Runs() {
		v2SetBodyRunFont(f.processor, r, 12, false) // 宋体小四+TNR for digits
	}
}

// formatCaption 图题/表题：宋体五号，居中
func (f *V2SmartFormatter) formatCaption(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcCenter
	pPr.Ind = nil

	for _, r := range para.Runs() {
		v2SetBodyRunFont(f.processor, r, 10.5, false) // 五号=10.5pt
	}
}

// formatSectionTitle 致谢/附录/注释标题：同一级标题格式
func (f *V2SmartFormatter) formatSectionTitle(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = nil
	pPr.Ind = nil
	pPr.PageBreakBefore = nil

	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto
	before := uint64(312)
	after := uint64(312)
	pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber = &before
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, "宋体", 16, true) // 三号=16pt, 加粗
	}
}

// formatNotesContent 注释内容：宋体五号，顶格，1.5倍行距
func (f *V2SmartFormatter) formatNotesContent(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcBoth
	pPr.Ind = nil

	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto

	for _, r := range para.Runs() {
		v2SetBodyRunFont(f.processor, r, 10.5, false) // 五号=10.5pt
	}
}

func (f *V2SmartFormatter) formatReferencesTitle(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcCenter
	pPr.Ind = nil

	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto
	before := uint64(312)
	after := uint64(312)
	pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber = &before
	pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber = &after

	for _, r := range para.Runs() {
		v2SetRunFont(f.processor, r, "宋体", 16, true) // 三号=16pt, 加粗
	}
}

// formatReferenceItem 参考文献条目：宋体(外文TNR)五号，1.5倍行距，悬挂缩进 2 字符（与 paper_format.json references.content「首行顶格 + 换行对齐」的常见版式）
func (f *V2SmartFormatter) formatReferenceItem(para document.Paragraph) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}
	pPr.Jc = wml.NewCT_Jc()
	pPr.Jc.ValAttr = wml.ST_JcBoth

	// 五号 10.5pt：2 全角字符宽 ≈ 420 twips；左缩进与悬挂量一致实现悬挂缩进，避免整块 Ind 置 nil 造成缩进退化
	refPt := 10.5
	hangTwips := uint64(refPt * 2 * 20)
	leftTwips := int64(hangTwips)
	pPr.Ind = wml.NewCT_Ind()
	pPr.Ind.FirstLineAttr = nil
	pPr.Ind.LeftAttr = &wml.ST_SignedTwipsMeasure{Int64: &leftTwips}
	pPr.Ind.HangingAttr = &sharedTypes.ST_TwipsMeasure{}
	pPr.Ind.HangingAttr.ST_UnsignedDecimalNumber = &hangTwips

	pPr.Spacing = wml.NewCT_Spacing()
	lineVal := int64(360)
	pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
	pPr.Spacing.LineAttr.Int64 = &lineVal
	pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto

	for _, r := range para.Runs() {
		v2SetBodyRunFont(f.processor, r, 10.5, false) // 宋体+TNR 五号
	}
}

// truncStr 截断字符串
func truncStr(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n]) + "..."
	}
	return s
}

// v2SetBodyRunFont 正文专用：中文宋体 + 数字/字母 Times New Roman
func v2SetBodyRunFont(proc *EnhancedProcessor, run document.Run, sizePt float64, bold bool) {
	rPr := run.X().RPr
	if rPr == nil {
		rPr = wml.NewCT_RPr()
		run.X().RPr = rPr
	}
	if rPr.RFonts == nil {
		rPr.RFonts = wml.NewCT_Fonts()
	}
	eastAsiaFont := proc.getCachedFontName("宋体")
	rPr.RFonts.EastAsiaAttr = eastAsiaFont
	tnrFont := proc.getCachedFontName("Times New Roman")
	rPr.RFonts.AsciiAttr = tnrFont
	rPr.RFonts.HAnsiAttr = tnrFont

	halfPt := uint64(sizePt * 2)
	rPr.Sz = wml.NewCT_HpsMeasure()
	rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	rPr.SzCs = wml.NewCT_HpsMeasure()
	rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt

	if bold {
		rPr.B = wml.NewCT_OnOff()
		rPr.BCs = wml.NewCT_OnOff()
	} else {
		rPr.B = nil
		rPr.BCs = nil
	}
}

// v2SetRunFont 设置 run 字体/字号/加粗（支持取消加粗）
func v2SetRunFont(proc *EnhancedProcessor, run document.Run, fontName string, sizePt float64, bold bool) {
	rPr := run.X().RPr
	if rPr == nil {
		rPr = wml.NewCT_RPr()
		run.X().RPr = rPr
	}

	if rPr.RFonts == nil {
		rPr.RFonts = wml.NewCT_Fonts()
	}
	fn := proc.getCachedFontName(fontName)
	rPr.RFonts.EastAsiaAttr = fn
	asciiFont := fontName
	if fontName == "宋体" {
		asciiFont = "SimSun"
	} else if fontName == "黑体" {
		asciiFont = "SimHei"
	}
	af := proc.getCachedFontName(asciiFont)
	rPr.RFonts.AsciiAttr = af
	rPr.RFonts.HAnsiAttr = af

	halfPt := uint64(sizePt * 2)
	rPr.Sz = wml.NewCT_HpsMeasure()
	rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPt
	rPr.SzCs = wml.NewCT_HpsMeasure()
	rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &halfPt

	if bold {
		rPr.B = wml.NewCT_OnOff()
		rPr.BCs = wml.NewCT_OnOff()
	} else {
		rPr.B = nil
		rPr.BCs = nil
	}

	_ = measurement.Distance(0) // keep import
}
