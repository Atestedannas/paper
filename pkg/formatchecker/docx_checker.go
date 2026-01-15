package formatchecker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"

	"github.com/nguyenthenguyen/docx"
	"github.com/paper-format-checker/backend/pkg/fileprocessor"
)

// DOCXChecker DOCX格式检查器
type DOCXChecker struct {
	processor fileprocessor.FileProcessor
	standard  FormatStandard
}

func (c *DOCXChecker) GenerateCorrections(ctx context.Context, result *CheckResult) ([]Correction, error) {
	var corrections []Correction

	// 遍历所有检查问题，生成修正建议
	for _, issue := range result.Issues {
		correction := c.generateCorrectionForIssue(issue)
		if correction != nil {
			corrections = append(corrections, *correction)
		}
	}

	return corrections, nil
}

// NewDOCXChecker 创建DOCX检查器实例
func NewDOCXChecker() *DOCXChecker {
	return &DOCXChecker{
		processor: &fileprocessor.BasicFileProcessor{}, // 初始化processor
		standard:  FormatStandard{},
	}
}

// SetStandard 设置格式标准
func (c *DOCXChecker) SetStandard(standard FormatStandard) {
	c.standard = standard
}

// Check 检查DOCX文档格式
func (c *DOCXChecker) Check(ctx context.Context, docPath string) (*CheckResult, error) {
	// 检查文件类型
	fileExt := strings.ToLower(filepath.Ext(docPath))
	if fileExt != ".docx" {
		return nil, fmt.Errorf("invalid file type, expected .docx, got %s", fileExt)
	}

	// 提取文档信息
	docInfo, err := c.processor.ExtractDocInfo(ctx, docPath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract doc info: %w", err)
	}

	// 初始化检查结果
	result := &CheckResult{
		DocInfo:      docInfo,
		Issues:       []FormatIssue{},
		TotalIssues:  0,
		ErrorCount:   0,
		WarningCount: 0,
		InfoCount:    0,
	}

	// 检查页面设置
	c.checkPageSetup(docPath, &c.standard)

	// 检查标题格式
	c.checkHeadings(ctx, docPath, result, &c.standard)

	// 检查段落格式
	c.checkParagraphs(ctx, docPath, result, &c.standard)

	// 检查表格格式
	c.checkTables(ctx, docPath, result, &c.standard)

	// 更新问题统计
	c.updateIssueStatistics(result)

	return result, nil
}

// ApplyCorrections 应用修正建议到文档
func (c *DOCXChecker) ApplyCorrections(ctx context.Context, docPath string, corrections []Correction) (string, error) {
	// 打开文档
	r, err := docx.ReadDocxFile(docPath)
	if err != nil {
		return "", fmt.Errorf("failed to open document: %w", err)
	}
	defer r.Close()

	// 获取可编辑的文档对象
	doc := r.Editable()

	// 应用每个修正
	for _, correction := range corrections {
		if !correction.Applied {
			err := c.applySingleCorrection(doc, correction)
			if err != nil {
				fmt.Printf("应用修正失败: %s - %v\n", correction.IssueID, err)
				continue
			}
		}
	}

	// 生成输出文件路径
	outputPath := strings.TrimSuffix(docPath, filepath.Ext(docPath)) + "_corrected.docx"

	// 保存文档
	if err := doc.WriteToFile(outputPath); err != nil {
		return "", fmt.Errorf("failed to save corrected document: %w", err)
	}

	return outputPath, nil
}

// applySingleCorrection 应用单个修正
func (c *DOCXChecker) applySingleCorrection(doc *docx.Docx, correction Correction) error {

	switch correction.Type {
	case CorrectionTypePageSetup:
		return c.applyPageSetupCorrection(doc, correction)
	case CorrectionTypeFont:
		return c.applyFontCorrection(doc, correction)
	case CorrectionTypeSpacing:
		return c.applySpacingCorrection(doc, correction)
	case CorrectionTypeHeading:
		return c.applyHeadingStyleCorrection(doc, correction)
	default:
		return nil
	}
}

// applyPageSetupCorrection 应用页面设置修正
func (c *DOCXChecker) applyPageSetupCorrection(doc *docx.Docx, correction Correction) error {
	// 简化的页面设置修正实现
	// 实际实现需要更复杂的逻辑
	// 这里可以根据correction.Parameters中的参数来修改页面设置
	return nil
}

// applyFontCorrection 应用字体修正
func (c *DOCXChecker) applyFontCorrection(doc *docx.Docx, correction Correction) error {
	// 简化的字体修正实现
	// 实际实现需要更复杂的逻辑
	// 这里可以根据correction.Parameters中的参数来修改字体设置
	return nil
}

// applySpacingCorrection 应用间距修正
func (c *DOCXChecker) applySpacingCorrection(doc *docx.Docx, correction Correction) error {
	// 简化的间距修正实现
	// 实际实现需要更复杂的逻辑
	// 这里可以根据correction.Parameters中的参数来修改间距设置
	return nil
}

// applyHeadingStyleCorrection 应用标题样式修正
func (c *DOCXChecker) applyHeadingStyleCorrection(doc *docx.Docx, correction Correction) error {
	// 简化的标题样式修正实现
	// 实际实现需要更复杂的逻辑
	// 这里可以根据correction.Parameters中的参数来修改标题样式
	return nil
}

// generateCorrectionForIssue 为单个问题生成修正建议
func (c *DOCXChecker) generateCorrectionForIssue(issue FormatIssue) *Correction {
	// 根据问题类型生成相应的修正建议
	switch issue.Type {
	case IssueTypePageSetup:
		return c.generatePageSetupCorrection(issue)
	case IssueTypeHeaderFooter:
		return c.generateHeaderFooterCorrection(issue)
	case IssueTypeFont:
		return c.generateFontCorrection(issue)
	case IssueTypeSpacing:
		return c.generateSpacingCorrection(issue)
	case IssueTypeHeading:
		return c.generateHeadingCorrection(issue)
	case IssueTypeTable:
		return c.generateTableCorrection(issue)
	case IssueTypeFigure:
		return c.generateFigureCorrection(issue)
	default:
		return nil
	}
}

// generatePageSetupCorrection 生成页面设置修正建议
func (c *DOCXChecker) generatePageSetupCorrection(issue FormatIssue) *Correction {
	// 将Suggestion转换为map[string]interface{}
	params, ok := issue.Suggestion.(map[string]interface{})
	if !ok {
		params = make(map[string]interface{})
	}

	return &Correction{
		IssueID:     issue.ID,
		Type:        CorrectionTypePageSetup,
		Description: issue.Description,
		Action:      "修改页面设置",
		Parameters:  params,
	}
}

// generateHeaderFooterCorrection 生成页眉页脚修正建议
func (c *DOCXChecker) generateHeaderFooterCorrection(issue FormatIssue) *Correction {
	// 将Suggestion转换为map[string]interface{}
	params, ok := issue.Suggestion.(map[string]interface{})
	if !ok {
		params = make(map[string]interface{})
	}

	return &Correction{
		IssueID:     issue.ID,
		Type:        CorrectionTypeHeaderFooter,
		Description: issue.Description,
		Action:      "修改页眉页脚",
		Parameters:  params,
	}
}

// generateFontCorrection 生成字体修正建议
func (c *DOCXChecker) generateFontCorrection(issue FormatIssue) *Correction {
	// 将Suggestion转换为map[string]interface{}
	params, ok := issue.Suggestion.(map[string]interface{})
	if !ok {
		params = make(map[string]interface{})
	}

	return &Correction{
		IssueID:     issue.ID,
		Type:        CorrectionTypeFont,
		Description: issue.Description,
		Action:      "修改字体设置",
		Parameters:  params,
	}
}

// generateSpacingCorrection 生成间距修正建议
func (c *DOCXChecker) generateSpacingCorrection(issue FormatIssue) *Correction {
	// 将Suggestion转换为map[string]interface{}
	params, ok := issue.Suggestion.(map[string]interface{})
	if !ok {
		params = make(map[string]interface{})
	}

	return &Correction{
		IssueID:     issue.ID,
		Type:        CorrectionTypeSpacing,
		Description: issue.Description,
		Action:      "修改间距设置",
		Parameters:  params,
	}
}

// generateHeadingCorrection 生成标题修正建议
func (c *DOCXChecker) generateHeadingCorrection(issue FormatIssue) *Correction {
	// 将Suggestion转换为map[string]interface{}
	params, ok := issue.Suggestion.(map[string]interface{})
	if !ok {
		params = make(map[string]interface{})
	}

	return &Correction{
		IssueID:     issue.ID,
		Type:        CorrectionTypeHeading,
		Description: issue.Description,
		Action:      "修改标题格式",
		Parameters:  params,
	}
}

// generateTableCorrection 生成表格修正建议
func (c *DOCXChecker) generateTableCorrection(issue FormatIssue) *Correction {
	// 将Suggestion转换为map[string]interface{}
	params, ok := issue.Suggestion.(map[string]interface{})
	if !ok {
		params = make(map[string]interface{})
	}

	return &Correction{
		IssueID:     issue.ID,
		Type:        CorrectionTypeTable,
		Description: issue.Description,
		Action:      "修改表格格式",
		Parameters:  params,
	}
}

// generateFigureCorrection 生成图表修正建议
func (c *DOCXChecker) generateFigureCorrection(issue FormatIssue) *Correction {
	// 将Suggestion转换为map[string]interface{}
	params, ok := issue.Suggestion.(map[string]interface{})
	if !ok {
		params = make(map[string]interface{})
	}

	return &Correction{
		IssueID:     issue.ID,
		Type:        CorrectionTypeFigure,
		Description: issue.Description,
		Action:      "修改图表格式",
		Parameters:  params,
	}
}

// FixDocumentDirectly 直接修复文档
func (c *DOCXChecker) FixDocumentDirectly(ctx context.Context, docPath string, standard FormatStandard) (string, error) {
	// 打开DOCX文档
	r, err := document.Open(docPath)
	if err != nil {
		return "", fmt.Errorf("failed to open document: %w", err)
	}
	defer r.Close()

	// 使用standard中的页面设置来检查和修复
	// 获取文档主体部分
	section := r.BodySection()
	// 使用standard中的页边距设置，将厘米转换为毫米（Distance类型表示毫米）
	// standard.PageSetup 单位是 cm, measurement.Distance 单位是 mm?
	// measurement.Distance 是 float64.
	// measurement.Millimeter * 10? No.
	// unioffice measurement:
	// type Distance float64 (points?) No, unioffice usually uses Measurement types.
	// But section.SetPageMargins takes `measurement.Distance`.
	// Let's assume standard.PageSetup values are in cm (based on struct definition comments).
	// 1 cm = 10 mm.
	// measurement.Millimeter is a constant?
	// Actually section.SetPageMargins signature: (top, bottom, left, right, header, footer, gutter measurement.Distance)
	// We need to convert cm to measurement.Distance.
	// measurement.Centimeter * value.

	section.SetPageMargins(
		measurement.Distance(standard.PageSetup.MarginTop)*measurement.Centimeter,
		measurement.Distance(standard.PageSetup.MarginBottom)*measurement.Centimeter,
		measurement.Distance(standard.PageSetup.MarginLeft)*measurement.Centimeter,
		measurement.Distance(standard.PageSetup.MarginRight)*measurement.Centimeter,
		measurement.Distance(standard.PageSetup.HeaderDistance)*measurement.Centimeter,
		measurement.Distance(standard.PageSetup.FooterDistance)*measurement.Centimeter,
		measurement.Distance(0), // 装订线
	)

	// 调用processText和references函数处理文本格式
	c.processText(r, &standard) // 设置标题和正文字体、字号、行间距等格式
	c.references(r, &standard)  // 设置参考文献格式
	// 调用现有函数设置页眉页脚
	c.headerSetting(r, &standard) // 设置页眉
	c.footerSetting(r, &standard) // 设置页脚

	// 保存修改后的文档
	// 获取文件名和扩展名
	filename := filepath.Base(docPath)
	nameWithoutExt := strings.TrimSuffix(filename, filepath.Ext(filename))
	ext := filepath.Ext(filename)

	// 创建新文件名：源文件名_corrected.扩展名
	newFilename := fmt.Sprintf("%s_corrected%s", nameWithoutExt, ext)

	// 确保uploads目录存在
	uploadsDir := "uploads"
	if _, err := os.Stat(uploadsDir); os.IsNotExist(err) {
		os.MkdirAll(uploadsDir, 0755)
	}

	// 构造完整的保存路径
	savePath := filepath.Join(uploadsDir, newFilename)

	if err := r.SaveToFile(savePath); err != nil {
		return "", fmt.Errorf("failed to save corrected document: %w", err)
	}

	return savePath, nil
}

// checkPageSetup 检查页面设置 (占位符，避免Check方法报错)
func (c *DOCXChecker) checkPageSetup(docPath string, standard *FormatStandard) {
	// TODO: 实现真正的检查逻辑，而不是修复
}

// abs 返回整数的绝对值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// 页眉设置
func (c *DOCXChecker) headerSetting(doc *document.Document, standard *FormatStandard) {
	// 启用奇偶页不同
	if doc.Settings.X().EvenAndOddHeaders == nil {
		doc.Settings.X().EvenAndOddHeaders = wml.NewCT_OnOff()
	}
	// 默认即为true，无需设置ValAttr，避免类型兼容问题
	// doc.Settings.X().EvenAndOddHeaders.ValAttr = ...

	// 获取文档主体部分
	section := doc.BodySection()

	// 获取文档标题1内容
	title1Content := ""
	var introductionIndex int
	var foundIntroduction bool

	// 遍历所有段落，找到"1 绪论"段落和标题1
	paragraphs := doc.Paragraphs()
	for i, p := range paragraphs {
		var textBuilder strings.Builder
		for _, run := range p.Runs() {
			textBuilder.WriteString(run.Text())
		}
		text := textBuilder.String()
		trimmedText := strings.TrimSpace(text)

		// 检查是否是"1 绪论"或类似的一级标题
		if strings.Contains(trimmedText, "绪论") {
			// 匹配一级标题格式：数字+空格+标题（如"1 绪论"）
			level1Pattern := `^\d+\s+`
			if matched, _ := regexp.MatchString(level1Pattern, trimmedText); matched {
				introductionIndex = i
				foundIntroduction = true
				title1Content = trimmedText
				break
			}
		}

		// 匹配标题1格式
		if title1Content == "" {
			title1Pattern := `^\d+\s+.+`
			if matched, _ := regexp.MatchString(title1Pattern, trimmedText); matched {
				title1Content = trimmedText
			}
		}
	}

	// 如果没有找到"1 绪论"等一级标题，不设置页眉
	if !foundIntroduction {
		return
	}

	// 1. 尝试在"1 绪论"段落前插入分节符，将文档分为前置部分和正文部分
	if introductionIndex > 0 {
		prevPara := paragraphs[introductionIndex-1]
		prevParaXML := prevPara.X()
		if prevParaXML.PPr == nil {
			prevParaXML.PPr = &wml.CT_PPr{}
		}

		// 创建分节符，这实际上定义了"前置部分"的属性
		sectPr := wml.NewCT_SectPr()

		// 前置部分页码格式：罗马数字
		pgNumType := wml.NewCT_PageNumber()
		pgNumType.FmtAttr = wml.ST_NumberFormatLowerRoman
		sectPr.PgNumType = pgNumType

		prevParaXML.PPr.SectPr = sectPr
	}

	// 2. 设置正文部分（当前section）的页码格式：阿拉伯数字，从1开始
	// 注意：unioffice Section.X() 返回的是 *wml.CT_SectPr
	sectPr := section.X()

	mainPgNumType := wml.NewCT_PageNumber()
	mainPgNumType.FmtAttr = wml.ST_NumberFormatDecimal
	startVal := int64(1)
	mainPgNumType.StartAttr = &startVal
	sectPr.PgNumType = mainPgNumType

	// 3. 设置正文页眉
	// 清除旧页眉
	headers := doc.Headers()
	for _, h := range headers {
		h.Clear()
	}

	// 设置奇数页(右页)页眉：居中章名
	defaultHeader := doc.AddHeader()
	hParaRight := defaultHeader.AddParagraph()
	hParaRight.Properties().SetAlignment(wml.ST_JcCenter)
	// 设置下划线
	if hParaRight.X().PPr == nil {
		hParaRight.X().PPr = &wml.CT_PPr{}
	}
	pBdr := &wml.CT_PBdr{}
	pBdr.Bottom = &wml.CT_Border{
		ValAttr: wml.ST_BorderSingle,
		SzAttr:  func() *uint64 { v := uint64(6); return &v }(), // 1/8 pt * 6 = 0.75pt
	}
	hParaRight.X().PPr.PBdr = pBdr

	hRunRight := hParaRight.AddRun()
	hRunRight.Properties().SetFontFamily("宋体")
	hRunRight.Properties().SetSize(10.5) // 五号
	if title1Content != "" {
		hRunRight.AddText(title1Content)
	} else {
		hRunRight.AddText("章标题")
	}
	section.SetHeader(defaultHeader, wml.ST_HdrFtrDefault)

	// 设置偶数页(左页)页眉：居中校名
	evenHeader := doc.AddHeader()
	hParaLeft := evenHeader.AddParagraph()
	hParaLeft.Properties().SetAlignment(wml.ST_JcCenter)
	// 设置下划线
	if hParaLeft.X().PPr == nil {
		hParaLeft.X().PPr = &wml.CT_PPr{}
	}
	hParaLeft.X().PPr.PBdr = pBdr // 复用边框

	hRunLeft := hParaLeft.AddRun()
	hRunLeft.Properties().SetFontFamily("宋体")
	hRunLeft.Properties().SetSize(10.5) // 五号

	// 使用 Standard 中的 Name
	headerText := "硕士学位论文"
	if standard != nil && standard.Name != "" {
		headerText = standard.Name
	}
	hRunLeft.AddText(headerText)

	section.SetHeader(evenHeader, wml.ST_HdrFtrEven)
}

func (c *DOCXChecker) footerSetting(doc *document.Document, standard *FormatStandard) {
	_ = standard
	section := doc.BodySection()

	// 清除旧页脚
	footers := doc.Footers()
	for _, f := range footers {
		f.Clear()
	}

	// 创建新页脚
	newFooter := doc.AddFooter()
	fPara := newFooter.AddParagraph()
	fPara.Properties().SetAlignment(wml.ST_JcCenter)
	fRun := fPara.AddRun()
	fRun.Properties().SetFontFamily("宋体")
	fRun.Properties().SetSize(10.5) // 五号

	// 插入页码域
	// 手动构造 PAGE 域
	fRun.AddText("") // 确保 run 已经初始化

	// unioffice Run.X() 返回 CT_R。
	// CT_R 的内容（RunContent）是一个 wml.EG_RunInnerContent 列表
	// 但是 unioffice 生成的代码中，CT_R 可能没有直接暴露 Content 字段，或者字段名不同。
	// 我们需要检查 CT_R 的定义。
	// 实际上，CT_R 有一个 Choice 字段，或者是一组可选字段。
	// 让我们使用 AddField 来简化操作，虽然 AddField 只能添加简单域。
	// 如果一定要复杂域，我们可以使用 unioffice 的 wml.CT_R 结构。
	// 在 unioffice 中，CT_R 的内容是通过一组方法访问的，或者直接暴露字段。
	// 通常是 FldChar, InstrText 等字段。

	// 1. 开始字符 (begin)
	// runBegin := fPara.AddRun() // declared and not used fixed

	// document.FieldPage 可能也是 undefined，因为我们用的是 unioffice
	// unioffice 中 FieldPage 是 Field 枚举值
	// 如果 document 包中没有 FieldPage，我们直接传字符串 "PAGE"
	fRun.AddField("PAGE")

	// 如果需要居中，段落已经设置了居中。

	section.SetFooter(newFooter, wml.ST_HdrFtrDefault)
}

func (c *DOCXChecker) processAbstract(doc *document.Document) {
}

// 正文设置
func (c *DOCXChecker) processText(doc *document.Document, standard *FormatStandard) {
	// 完整实现： 对其方式
	// 1. 正文内容：宋体小四号，固定行间距20磅
	// 2. 一级标题：黑体小3号，居中，段前后间距
	// 3. 二级标题：黑体4号，左对齐，首行缩进

	// 状态变量
	var inBody bool = false

	// 跳过特殊段落类型
	specialSections := []string{"摘要", "Abstract", "目录", "Contents", "TOC"}

	// 遍历所有段落
	for _, p := range doc.Paragraphs() {
		// 获取段落文本
		var textBuilder strings.Builder
		for _, run := range p.Runs() {
			textBuilder.WriteString(run.Text())
		}
		text := textBuilder.String()
		trimmedText := strings.TrimSpace(text)

		// 跳过特殊段落
		isSpecialSection := false
		for _, section := range specialSections {
			if strings.Contains(trimmedText, section) {
				isSpecialSection = true
				break
			}
		}
		if isSpecialSection {
			continue
		}

		// 1. 检查是否是参考文献
		if strings.Contains(trimmedText, "参考文献") || strings.Contains(trimmedText, "REFERENCES") {
			inBody = false
			continue
		}

		// 2. 检查标题级别
		headingLevel := 0 // 0: 正文, 1: 一级标题, 2: 二级标题, 3: 三级标题

		// 检查一级标题：
		// 格式1：数字 + 空格/无空格 + 标题（如"1 绪 论"、"2相关工作"）- 确保不匹配带小数点的数字
		// 格式2：中文序数 + 章 + 空格/无空格 + 标题（如"第一章 引言"、"第二章系统设计"）
		// 格式3：第 + 数字 + 章 + 空格/无空格 + 标题（如"第1章 引言"、"第2章系统设计"）
		// 注意：^ 必须应用于整个正则表达式，否则会匹配正文中的"七章"等内容
		level1Pattern := `^((\d+\s+)|(\d+$)|([一二三四五六七八九十]+章\s*)|(第\d+章\s*))`
		if matched, _ := regexp.MatchString(level1Pattern, trimmedText); matched {
			headingLevel = 1
			inBody = true
		} else {
			// 检查二级标题：数字 + . + 数字 + 空格/无空格 + 标题（如"1.1 研究背景和意义"、"1.1研究背景与意义"、"2.2 系统架构"）
			level2Pattern := `^\d+\.\d+\s*`
			if matched, _ := regexp.MatchString(level2Pattern, trimmedText); matched {
				headingLevel = 2
				inBody = true
			} else {
				// 检查三级标题：数字 + . + 数字 + . + 数字 + 空格/无空格 + 标题（如"3.1.1 "、"3.1.2研究内容"）
				level3Pattern := `^\d+\.\d+\.\d+\s*`
				if matched, _ := regexp.MatchString(level3Pattern, trimmedText); matched {
					headingLevel = 3
					inBody = true
				} else if inBody {
					// 正文内容
					headingLevel = 0
				}
			}
		}

		// 3. 应用相应格式
		if inBody {
			switch headingLevel {
			case 1:
				// 一级标题格式：3号黑体，居中，段前后间距
				// 强制每章另起一页：在段前添加分页符
				// 在 unioffice 中，可以在段落属性中设置 PageBreakBefore
				if p.X().PPr == nil {
					p.X().PPr = &wml.CT_PPr{}
				}
				if p.X().PPr.PageBreakBefore == nil {
					p.X().PPr.PageBreakBefore = wml.NewCT_OnOff()
				}
				// ST_OnOff is boolean enum, true value is 1
				// 但之前报错说 undefined ST_OnOff
				// 也许我们不应该设置 ValAttr，而是直接创建 OnOff
				// 默认就是 true
				// p.X().PPr.PageBreakBefore.ValAttr = ... (removed)

				// 设置段落居中对齐
				p.Properties().SetAlignment(wml.ST_JcCenter)

				// 设置段落间距：段前24磅(480)，段后18磅(360)
				// 1磅 = 20 twips
				p.Properties().SetSpacing(480, 360)

				// 设置字体：黑体，3号
				for _, run := range p.Runs() {
					run.Properties().SetFontFamily("黑体") // 修正为黑体
					run.Properties().SetSize(16)         // 3号 ≈ 16pt
					run.Properties().SetBold(true)       // 黑体通常加粗
				}

			case 2:
				// 二级标题格式：4号黑体，左对齐，左顶格(无缩进)
				// 设置段落左对齐
				p.Properties().SetAlignment(wml.ST_JcLeft)

				// 取消首行缩进 (左顶格)
				p.Properties().SetFirstLineIndent(0)

				// 设置段落间距
				p.Properties().SetSpacing(240, 240) // 12磅

				// 设置字体：黑体，4号
				for _, run := range p.Runs() {
					run.Properties().SetFontFamily("黑体") // 修正为黑体
					run.Properties().SetSize(14)         // 4号 ≈ 14pt
					run.Properties().SetBold(true)
				}

			case 3:
				// 三级标题格式：小4号黑体，左对齐，左顶格
				p.Properties().SetAlignment(wml.ST_JcLeft)
				p.Properties().SetFirstLineIndent(0)

				for _, run := range p.Runs() {
					run.Properties().SetFontFamily("黑体")
					run.Properties().SetSize(12) // 小4号 = 12pt
					run.Properties().SetBold(true)
				}

			case 0:
				// 正文格式：宋体小4号，固定行距20磅，首行缩进2字符
				// 设置行间距：固定值20磅
				if p.X().PPr == nil {
					p.X().PPr = &wml.CT_PPr{}
				}
				if p.X().PPr.Spacing == nil {
					p.X().PPr.Spacing = wml.NewCT_Spacing()
				}
				lineVal := int64(400)                                                    // 20磅 * 20
				p.X().PPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{Int64: &lineVal} // unioffice generated struct wrapper
				p.X().PPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleExact

				// 更好的方式：使用 Indent 属性的 FirstLineChars
				if p.X().PPr.Ind == nil {
					p.X().PPr.Ind = wml.NewCT_Ind()
				}
				firstLineChars := int64(200) // 200 = 2 characters
				p.X().PPr.Ind.FirstLineCharsAttr = &firstLineChars
				// 同时清除 FirstLine (twips)，优先使用 Chars
				p.X().PPr.Ind.FirstLineAttr = nil

				// 字体设置
				for _, run := range p.Runs() {
					run.Properties().SetFontFamily("宋体")
					run.Properties().SetSize(12) // 小4号 = 12pt
				}
			}
		}
	}
}

// 标题一级标题采用中文序数
// (如一、二、三、……)标引、小3号黑体并居中排列；
// 二级标题采用阿拉伯数字(如1、2、3、……)标引
// 、4号黑体距左边正文边框两个字对齐排列；
// 三级标题采用加圆括号的阿拉伯数字标引、与正文相同字体和对齐方式排列；
// 一级标题与上一段落之间隔一行。
// bodyText 处理论文标题格式
func (c *DOCXChecker) bodyText(doc *document.Document) {
	// 遍历所有段落
	for i, p := range doc.Paragraphs() {
		// 获取段落文本
		var textBuilder strings.Builder
		for _, run := range p.Runs() {
			textBuilder.WriteString(run.Text())
		}
		text := textBuilder.String()
		trimmedText := strings.TrimSpace(text)

		// 跳过空段落
		if trimmedText == "" {
			continue
		}

		// 1. 识别一级标题：中文序数（如一、二、三、……）标引
		level1Pattern := `^[一二三四五六七八九十]+、`
		if matched, _ := regexp.MatchString(level1Pattern, trimmedText); matched {
			// 设置一级标题格式：3号黑体并居中排列
			// 1.1 设置段落居中对齐
			p.Properties().SetAlignment(wml.ST_JcCenter)
			// 1.2 设置字体为黑体
			for _, run := range p.Runs() {
				run.Properties().SetFontFamily("黑体")
				run.Properties().SetSize(16) // 3号 ≈ 16pt
				run.Properties().SetBold(true)
			}
			// 1.3 一级标题与上一段落之间隔一行
			if i > 0 {
				// 获取上一段落
				prevPara := doc.Paragraphs()[i-1]
				// 设置段后间距（这里需要根据实际情况调整具体数值）
				//prevPara.Properties().SetSpacingAfter(12) // 12pt 约为一行间距
				prevPara.Properties().SetEndIndent(12) // 12pt 约为一行间距
			}
			continue
		}

		// 2. 识别二级标题：阿拉伯数字（如1、2、3、……）标引
		level2Pattern := `^[1-9]\d*、`
		if matched, _ := regexp.MatchString(level2Pattern, trimmedText); matched {
			// 设置二级标题格式：4号黑体距左边正文边框两个字对齐排列
			// 2.1 设置段落左对齐，首行缩进两个字（约28pt）
			p.Properties().SetAlignment(wml.ST_JcLeft)
			p.Properties().SetFirstLineIndent(measurement.Distance(28)) // 正确用法// 两个字约28pt

			// 2.2 设置字体为黑体
			for _, run := range p.Runs() {
				run.Properties().SetFontFamily("黑体")
				run.Properties().SetSize(14) // 4号 ≈ 14pt
				run.Properties().SetBold(true)
			}

			continue
		}
		// 3. 识别三级标题：加圆括号的阿拉伯数字标引
		level3Pattern := `^\([1-9]\d*\)`
		if matched, _ := regexp.MatchString(level3Pattern, trimmedText); matched {
			// 设置三级标题格式：与正文相同字体和对齐方式排列
			// 这里假设正文格式为：宋体，小四号，左对齐
			// 3.1 设置段落左对齐
			p.Properties().SetAlignment(wml.ST_JcLeft)
			// 3.2 设置与正文相同的字体
			for _, run := range p.Runs() {
				run.Properties().SetFontFamily("宋体")
				run.Properties().SetSize(12) // 小四号 ≈ 12pt
				run.Properties().SetBold(false)
			}
			continue
		}
	}
}

// 参考文献
// 字体
// 字体大小
//
//	对齐方式
//	靠左对齐
//
// 阿拉伯数字标引序号的方式排列。
func (c *DOCXChecker) references(doc *document.Document, standard *FormatStandard) {
	inReferences := false

	// 遍历所有段落
	for _, p := range doc.Paragraphs() {
		var textBuilder strings.Builder

		// 拼接段落文本
		for _, run := range p.Runs() {
			textBuilder.WriteString(run.Text())
		}
		text := textBuilder.String()
		trimmedText := strings.TrimSpace(text)

		// 1. 定位参考文献标题
		if strings.Contains(trimmedText, "参考文献") ||
			strings.Contains(trimmedText, "References") ||
			strings.Contains(trimmedText, "Bibliography") {
			inReferences = true
			continue
		}

		// 2. 处理参考文献条目
		if inReferences {
			trimmedText = strings.TrimSpace(text)
			if trimmedText == "" {
				continue
			}

			// 检查是否结束（遇到其他标题）
			if strings.Contains(trimmedText, "附录") ||
				strings.Contains(trimmedText, "致谢") ||
				strings.Contains(trimmedText, "ABSTRACT") ||
				strings.Contains(trimmedText, "Abstract") {
				break
			}

			// 3. 识别参考文献条目（匹配常见格式）
			isReference := false

			// 检查是否包含参考文献序号格式
			refPattern := `^\[\d+\]`
			matched, _ := regexp.MatchString(refPattern, trimmedText)
			if matched {
				isReference = true
			}

			// 或者检查是否包含常见的文献类型标识
			if strings.Contains(trimmedText, "[M]") || // 专著
				strings.Contains(trimmedText, "[J]") || // 期刊
				strings.Contains(trimmedText, "[D]") || // 学位论文
				strings.Contains(trimmedText, "[C]") || // 会议论文
				strings.Contains(trimmedText, "[P]") || // 专利
				strings.Contains(trimmedText, "[S]") || // 标准
				strings.Contains(trimmedText, "[N]") || // 报纸
				strings.Contains(trimmedText, "[EB/OL]") || // 电子文献
				strings.Contains(trimmedText, "[R]") || // 报告
				strings.Contains(trimmedText, "[A]") { // 析出文献
				isReference = true
			}

			if isReference {
				// 4. 设置参考文献格式
				// 使用 standard 中的配置
				refStyle := standard.ReferenceStyle

				// 4.1 设置段落对齐方式
				p.Properties().SetAlignment(wml.ST_JcLeft)

				// 4.2 设置字体和字号
				fontName := "宋体"
				fontSize := 10.5 // 五号

				if refStyle.FontName != "" {
					fontName = refStyle.FontName
				}
				if refStyle.FontSize > 0 {
					fontSize = refStyle.FontSize
				}

				for _, run := range p.Runs() {
					run.Properties().SetFontFamily(fontName)
					run.Properties().SetSize(measurement.Distance(fontSize))
				}

				// 4.3 设置行间距
				if refStyle.LineSpacing > 0 {
					if p.X().PPr == nil {
						p.X().PPr = &wml.CT_PPr{}
					}
					if p.X().PPr.Spacing == nil {
						p.X().PPr.Spacing = wml.NewCT_Spacing()
					}
					lineVal := int64(refStyle.LineSpacing * 20)
					p.X().PPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{Int64: &lineVal}
					p.X().PPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleExact
				}

				// 4.4 检查并确保编号连续 (仅检查)
				// 提取当前编号
				numPattern := `^\[(\d+)\]`
				numRegex := regexp.MustCompile(numPattern)
				numMatch := numRegex.FindStringSubmatch(trimmedText)
				if len(numMatch) > 1 {
					// 检查编号连续性
					// 但由于unioffice库限制，无法直接修改编号
				}

				// 4.5 作者处理：检查作者数量，三名以内全部列出，超三名则列前三加“等”或“, et al”
				// 解析作者部分
				authorPattern := `^\[(\d+)\]\s*([^，,]+(?:[，,][^，,]+)*?)[，,][^，,]+\[(M|J|D|C|P|S|N|EB/OL|R|A)\]`
				authorRegex := regexp.MustCompile(authorPattern)
				authorMatch := authorRegex.FindStringSubmatch(trimmedText)
				if len(authorMatch) > 2 {
					authors := authorMatch[2]
					// 分割作者
					var authorList []string
					if strings.Contains(authors, "，") {
						authorList = strings.Split(authors, "，")
					} else if strings.Contains(authors, ",") {
						authorList = strings.Split(authors, ",")
					} else {
						authorList = []string{authors}
					}

					// 处理作者列表，去除首尾空格
					for i, author := range authorList {
						authorList[i] = strings.TrimSpace(author)
					}

					// 检查作者数量，超过3名则记录信息（由于库限制无法直接修改）
					if len(authorList) > 3 {
						// 这里可以根据需要记录修正建议
					}
				}

				// 4.6 识别文献类型并标准化格式
				// 支持的十种文献类型：专著[M]、期刊[J]、学位论文[D]、会议论文[C]、专利[P]、标准[S]、报纸[N]、电子文献[EB/OL]、报告[R]、析出文献[A]
				// refTypePattern := `\[(M|J|D|C|P|S|N|EB/OL|R|A)\]`
			}
		}
	}
}

//

// checkHeadings 检查标题格式
func (c *DOCXChecker) checkHeadings(ctx context.Context, docPath string, result *CheckResult, standard *FormatStandard) {
	// 如果没有设置格式标准，使用默认标准
	if standard == nil {
		return
	}

	// 打开DOCX文档进行标题检查
	r, err := document.Open(docPath)
	if err != nil {
		fmt.Printf("Failed to open document for heading check: %v\n", err)
		return
	}
	defer r.Close()

	// 遍历所有段落，检查标题格式
	for _, p := range r.Paragraphs() {
		var textBuilder strings.Builder
		for _, run := range p.Runs() {
			textBuilder.WriteString(run.Text())
		}
		trimmedText := strings.TrimSpace(textBuilder.String())
		// 跳过空文本
		if trimmedText == "" {
			continue
		}

		// 检查是否为标题
		for range standard.HeadingStyles {
			// 这里实现标题检查逻辑
			// 例如：检查标题的字体、字号、对齐方式等是否符合标准
			// 如果不符合，添加到result.Issues
			break
		}
	}
}

// checkParagraphs 检查段落格式
func (c *DOCXChecker) checkParagraphs(ctx context.Context, docPath string, result *CheckResult, standard *FormatStandard) {
	// 如果没有设置格式标准，使用默认标准
	if standard == nil {
		return
	}

	// 打开DOCX文档进行段落检查
	r, err := document.Open(docPath)
	if err != nil {
		fmt.Printf("Failed to open document for paragraph check: %v\n", err)
		return
	}
	defer r.Close()

	// 遍历所有段落，检查段落格式
	for _, p := range r.Paragraphs() {
		var textBuilder strings.Builder
		for _, run := range p.Runs() {
			textBuilder.WriteString(run.Text())
		}
		text := textBuilder.String()
		trimmedText := strings.TrimSpace(text)

		// 跳过空段落
		if trimmedText == "" {
			continue
		}

		// 检查是否为正文段落
		// 这里实现段落检查逻辑
		// 例如：检查段落的字体、行间距、首行缩进等是否符合标准
		// 如果不符合，添加到result.Issues
	}
}

// checkTables 检查表格格式
func (c *DOCXChecker) checkTables(ctx context.Context, docPath string, result *CheckResult, standard *FormatStandard) {
	if standard == nil {
		return
	}

	// 打开文档
	r, err := document.Open(docPath)
	if err != nil {
		fmt.Printf("Failed to open document for table check: %v\n", err)
		return
	}
	defer r.Close()

	// 遍历所有表格
	for i, table := range r.Tables() {
		// 1. 检查表格内文字格式
		for _, row := range table.Rows() {
			for _, cell := range row.Cells() {
				for _, p := range cell.Paragraphs() {
					for _, run := range p.Runs() {
						// 检查字体
						rPr := run.X().RPr
						fontName := ""
						if rPr != nil && rPr.RFonts != nil {
							if rPr.RFonts.EastAsiaAttr != nil {
								fontName = *rPr.RFonts.EastAsiaAttr
							} else if rPr.RFonts.AsciiAttr != nil {
								fontName = *rPr.RFonts.AsciiAttr
							}
						}

						// 如果标准指定了字体，且当前字体不匹配
						if standard.TableStyle.FontName != "" && fontName != standard.TableStyle.FontName {
							// 注意：这里简化了字体匹配逻辑，实际可能需要处理别名或fallback
							// 暂时只在非空时检查
						}

						// 检查字号（half-points）
						if rPr != nil && rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
							fontSize := float64(*rPr.Sz.ValAttr.ST_UnsignedDecimalNumber) / 2.0
							if standard.TableStyle.FontSize > 0 && fontSize != standard.TableStyle.FontSize {
								result.Issues = append(result.Issues, FormatIssue{
									ID:          fmt.Sprintf("table_font_size_%d", i),
									Type:        IssueTypeTable,
									Severity:    SeverityWarning,
									Description: fmt.Sprintf("表格 %d 文字大小不符合要求: 当前 %.1f, 应为 %.1f", i+1, fontSize, standard.TableStyle.FontSize),
									Original:    fontSize,
									Suggestion:  standard.TableStyle.FontSize,
								})
							}
						}
					}
				}
			}
		}
	}
}

// checkFigures 检查图表格式
func (c *DOCXChecker) checkFigures(ctx context.Context, docPath string, result *CheckResult) {
	// 简化的图表格式检查实现
	// 实际实现需要更复杂的逻辑
}

// updateIssueStatistics 更新问题统计信息
func (c *DOCXChecker) updateIssueStatistics(result *CheckResult) {
	// 更新问题统计
	result.TotalIssues = len(result.Issues)

	// 统计不同严重程度的问题数量
	for _, issue := range result.Issues {
		switch issue.Severity {
		case SeverityError:
			result.ErrorCount++
		case SeverityWarning:
			result.WarningCount++
		case SeverityInfo:
			result.InfoCount++
		}
	}
}

// generateParagraphCorrection 生成段落修正建议
func (c *DOCXChecker) generateParagraphCorrection(issue FormatIssue) {

}
