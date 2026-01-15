package formatchecker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/paper-format-checker/backend/pkg/fileprocessor"
	"rsc.io/pdf"
)

// PDFChecker PDF格式检查器
type PDFChecker struct {
	processor fileprocessor.FileProcessor
	standard  FormatStandard
}

// NewPDFChecker 创建PDF检查器实例
func NewPDFChecker(processor fileprocessor.FileProcessor, standard FormatStandard) *PDFChecker {
	return &PDFChecker{
		processor: processor,
		standard:  standard,
	}
}

// Check 检查PDF文档格式
func (c *PDFChecker) Check(ctx context.Context, docPath string) (*CheckResult, error) {
	// 检查文件类型
	fileExt := strings.ToLower(filepath.Ext(docPath))
	if fileExt != ".pdf" {
		return nil, fmt.Errorf("invalid file type, expected .pdf, got %s", fileExt)
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
	c.checkPageSetup(ctx, docPath, result)

	// 检查标题格式
	c.checkHeadings(ctx, docPath, result)

	// 检查段落格式
	c.checkParagraphs(ctx, docPath, result)

	// 检查表格格式
	c.checkTables(ctx, docPath, result)

	// 检查图表格式
	c.checkFigures(ctx, docPath, result)

	// 更新问题统计
	c.updateIssueStatistics(result)

	return result, nil
}

// GenerateCorrections 生成修正建议
func (c *PDFChecker) GenerateCorrections(ctx context.Context, result *CheckResult) ([]Correction, error) {
	corrections := make([]Correction, 0)

	// 为每个问题生成修正建议
	for _, issue := range result.Issues {
		correction := c.generateCorrectionForIssue(issue)
		if correction != nil {
			corrections = append(corrections, *correction)
		}
	}

	return corrections, nil
}

// ApplyCorrections 应用修正建议到文档
func (c *PDFChecker) ApplyCorrections(ctx context.Context, docPath string, corrections []Correction) (string, error) {
	// 简化的PDF修正实现
	// 实际实现需要更复杂的逻辑
	return "", nil
}

// FixDocumentDirectly 直接修复文档，不生成中间检查结果
func (c *PDFChecker) FixDocumentDirectly(ctx context.Context, docPath string, standard FormatStandard) (string, error) {
	// PDF文档直接修复功能暂未实现
	// PDF格式的修改需要更复杂的PDF处理库支持
	return "", fmt.Errorf("direct document fixing is not supported for PDF files")
}

// generateCorrectionForIssue 为单个问题生成修正建议
func (c *PDFChecker) generateCorrectionForIssue(issue FormatIssue) *Correction {
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
func (c *PDFChecker) generatePageSetupCorrection(issue FormatIssue) *Correction {
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
func (c *PDFChecker) generateHeaderFooterCorrection(issue FormatIssue) *Correction {
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
func (c *PDFChecker) generateFontCorrection(issue FormatIssue) *Correction {
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
func (c *PDFChecker) generateSpacingCorrection(issue FormatIssue) *Correction {
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
func (c *PDFChecker) generateHeadingCorrection(issue FormatIssue) *Correction {
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
func (c *PDFChecker) generateTableCorrection(issue FormatIssue) *Correction {
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
func (c *PDFChecker) generateFigureCorrection(issue FormatIssue) *Correction {
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

// checkPageSetup 检查页面设置
func (c *PDFChecker) checkPageSetup(ctx context.Context, docPath string, result *CheckResult) {
	// 打开PDF文件
	file, err := os.Open(docPath)
	if err != nil {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          "page-setup-file-error",
			Type:        IssueTypePageSetup,
			Severity:    SeverityError,
			Description: fmt.Sprintf("无法打开PDF文件进行页面设置检查: %v", err),
			Original:    nil,
			Suggestion:  nil,
		})
		return
	}
	defer file.Close()

	// 获取文件信息
	fileInfo, err := file.Stat()
	if err != nil {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          "page-setup-file-info-error",
			Type:        IssueTypePageSetup,
			Severity:    SeverityError,
			Description: fmt.Sprintf("无法获取PDF文件信息: %v", err),
			Original:    nil,
			Suggestion:  nil,
		})
		return
	}

	// 创建PDF阅读器
	reader, err := pdf.NewReader(file, fileInfo.Size())
	if err != nil {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          "page-setup-pdf-reader-error",
			Type:        IssueTypePageSetup,
			Severity:    SeverityError,
			Description: fmt.Sprintf("无法创建PDF阅读器: %v", err),
			Original:    nil,
			Suggestion:  nil,
		})
		return
	}

	// 检查纸张大小是否为A4 (21 × 29.7 cm)
	// 由于PDF格式的复杂性，我们无法直接获取纸张大小
	// 这里我们只能添加一个信息性问题，说明需要手动检查
	result.Issues = append(result.Issues, FormatIssue{
		ID:          "page-setup-paper-size",
		Type:        IssueTypePageSetup,
		Severity:    SeverityInfo,
		Description: "无法自动检查纸张大小，请手动确认是否为A4标准（21 × 29.7 cm）",
		Original:    nil,
		Suggestion:  map[string]interface{}{"paper_size": "A4", "width": 21.0, "height": 29.7, "unit": "cm"},
	})

	// 检查页边距是否为2.5cm
	// 同样，由于PDF格式的复杂性，我们无法直接获取页边距
	result.Issues = append(result.Issues, FormatIssue{
		ID:          "page-setup-margins",
		Type:        IssueTypePageSetup,
		Severity:    SeverityInfo,
		Description: "无法自动检查页边距，请手动确认上、下、左、右页边距均为2.5cm",
		Original:    nil,
		Suggestion:  map[string]interface{}{"margin_top": 2.5, "margin_bottom": 2.5, "margin_left": 2.5, "margin_right": 2.5, "unit": "cm"},
	})

	// 检查页眉设置
	// 页眉高度1.6cm，从摘要页开始到论文最后一页
	result.Issues = append(result.Issues, FormatIssue{
		ID:          "page-setup-header",
		Type:        IssueTypeHeaderFooter,
		Severity:    SeverityInfo,
		Description: "请确认页眉设置：页眉高度1.6cm，从摘要页开始到论文最后一页",
		Original:    nil,
		Suggestion:  map[string]interface{}{"header_height": 1.6, "header_start_page": "摘要页", "header_end_page": "论文最后一页"},
	})

	// 检查页脚设置
	// 页脚高度2.1cm
	result.Issues = append(result.Issues, FormatIssue{
		ID:          "page-setup-footer",
		Type:        IssueTypeHeaderFooter,
		Severity:    SeverityInfo,
		Description: "请确认页脚设置：页脚高度2.1cm",
		Original:    nil,
		Suggestion:  map[string]interface{}{"footer_height": 2.1, "unit": "cm"},
	})

	// 检查打印方式
	// 总页数50页以上必须双面打印，50页以下单面打印即可
	// 获取PDF页数
	pageCount := reader.NumPage()

	if pageCount > 50 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          "page-setup-printing-double",
			Type:        IssueTypePageSetup,
			Severity:    SeverityWarning,
			Description: fmt.Sprintf("文档%d页，超过50页，应使用双面打印", pageCount),
			Original:    map[string]interface{}{"page_count": pageCount, "printing_side": "unknown"},
			Suggestion:  map[string]interface{}{"printing_side": "double"},
		})
	} else {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          "page-setup-printing-single",
			Type:        IssueTypePageSetup,
			Severity:    SeverityInfo,
			Description: fmt.Sprintf("文档%d页，少于50页，可使用单面打印", pageCount),
			Original:    map[string]interface{}{"page_count": pageCount, "printing_side": "unknown"},
			Suggestion:  map[string]interface{}{"printing_side": "single"},
		})
	}
}

// checkHeadings 检查标题格式
func (c *PDFChecker) checkHeadings(ctx context.Context, docPath string, result *CheckResult) {
	// 提取标题信息
	headings, err := c.processor.ExtractHeadings(ctx, docPath)
	if err != nil {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          "headings-extraction-error",
			Type:        IssueTypeHeading,
			Severity:    SeverityError,
			Description: fmt.Sprintf("无法提取标题信息: %v", err),
			Original:    nil,
			Suggestion:  nil,
		})
		return
	}

	// 检查每个标题是否符合格式标准
	for _, h := range headings {
		heading := h // 使用 map 访问

		// 获取标题级别
		level, _ := heading["level"].(int)

		// 根据标题级别获取对应的格式标准
		var standardStyle *HeadingStyle
		for _, style := range c.standard.HeadingStyles {
			if style.Level == level {
				standardStyle = &style
				break
			}
		}

		// 如果没有找到对应级别的标准，跳过检查
		if standardStyle == nil {
			continue
		}

		// 获取样式信息
		style, _ := heading["style"].(map[string]interface{})
		fontName, _ := style["font_name"].(string)
		fontSize, _ := style["font_size"].(float64)
		page, _ := heading["page"].(int)
		position, _ := heading["position"].(int)

		// 检查字体名称
		if fontName != "" && standardStyle.FontName != "" && fontName != standardStyle.FontName {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          fmt.Sprintf("heading-font-name-%d", position),
				Type:        IssueTypeHeading,
				Severity:    SeverityWarning,
				Page:        page,
				Position:    position,
				Description: fmt.Sprintf("第%d级标题字体应为%s，实际为%s", level, standardStyle.FontName, fontName),
				Original:    map[string]interface{}{"font_name": fontName},
				Suggestion:  map[string]interface{}{"font_name": standardStyle.FontName},
			})
		}

		// 检查字体大小
		if fontSize > 0 && standardStyle.FontSize > 0 && fontSize != standardStyle.FontSize {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          fmt.Sprintf("heading-font-size-%d", position),
				Type:        IssueTypeHeading,
				Severity:    SeverityWarning,
				Page:        page,
				Position:    position,
				Description: fmt.Sprintf("第%d级标题字体大小应为%.1f磅，实际为%.1f磅", level, standardStyle.FontSize, fontSize),
				Original:    map[string]interface{}{"font_size": fontSize},
				Suggestion:  map[string]interface{}{"font_size": standardStyle.FontSize},
			})
		}

		// 检查是否加粗
		bold, _ := style["bold"].(bool)
		if standardStyle.Bold && !bold {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          fmt.Sprintf("heading-bold-%d", position),
				Type:        IssueTypeHeading,
				Severity:    SeverityWarning,
				Page:        page,
				Position:    position,
				Description: fmt.Sprintf("第%d级标题应加粗", level),
				Original:    map[string]interface{}{"bold": false},
				Suggestion:  map[string]interface{}{"bold": true},
			})
		}

		// 检查对齐方式
		alignment, _ := style["alignment"].(string)
		if alignment != "" && standardStyle.Alignment != "" && alignment != standardStyle.Alignment {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          fmt.Sprintf("heading-alignment-%d", position),
				Type:        IssueTypeHeading,
				Severity:    SeverityWarning,
				Page:        page,
				Position:    position,
				Description: fmt.Sprintf("第%d级标题应对齐方式为%s，实际为%s", level, standardStyle.Alignment, alignment),
				Original:    map[string]interface{}{"alignment": alignment},
				Suggestion:  map[string]interface{}{"alignment": standardStyle.Alignment},
			})
		}
	}
}

// checkParagraphs 检查段落格式
func (c *PDFChecker) checkParagraphs(ctx context.Context, docPath string, result *CheckResult) {
	// 提取段落信息
	paragraphs, err := c.processor.ExtractParagraphs(ctx, docPath)
	if err != nil {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          "paragraphs-extraction-error",
			Type:        IssueTypeParagraph,
			Severity:    SeverityError,
			Description: fmt.Sprintf("无法提取段落信息: %v", err),
			Original:    nil,
			Suggestion:  nil,
		})
		return
	}

	// 获取正文段落样式标准
	var bodyStyle *ParagraphStyle
	for _, style := range c.standard.ParagraphStyles {
		if style.Name == "正文" {
			bodyStyle = &style
			break
		}
	}

	// 如果没有找到正文样式标准，使用第一个段落样式
	if bodyStyle == nil && len(c.standard.ParagraphStyles) > 0 {
		bodyStyle = &c.standard.ParagraphStyles[0]
	}

	// 如果仍然没有段落样式标准，跳过检查
	if bodyStyle == nil {
		return
	}

	// 检查每个段落是否符合格式标准
	for _, p := range paragraphs {
		paragraph := p // 使用 map 访问

		// 获取样式信息
		style, _ := paragraph["style"].(map[string]interface{})
		fontName, _ := style["font_name"].(string)
		fontSize, _ := style["font_size"].(float64)
		alignment, _ := style["alignment"].(string)
		page, _ := paragraph["page"].(int)
		position, _ := paragraph["position"].(int)

		// 检查字体名称
		if fontName != "" && bodyStyle.FontName != "" && fontName != bodyStyle.FontName {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          fmt.Sprintf("paragraph-font-name-%d", position),
				Type:        IssueTypeParagraph,
				Severity:    SeverityWarning,
				Page:        page,
				Position:    position,
				Description: fmt.Sprintf("段落字体应为%s，实际为%s", bodyStyle.FontName, fontName),
				Original:    map[string]interface{}{"font_name": fontName},
				Suggestion:  map[string]interface{}{"font_name": bodyStyle.FontName},
			})
		}

		// 检查字体大小
		if fontSize > 0 && bodyStyle.FontSize > 0 && fontSize != bodyStyle.FontSize {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          fmt.Sprintf("paragraph-font-size-%d", position),
				Type:        IssueTypeParagraph,
				Severity:    SeverityWarning,
				Page:        page,
				Position:    position,
				Description: fmt.Sprintf("段落字体大小应为%.1f磅，实际为%.1f磅", bodyStyle.FontSize, fontSize),
				Original:    map[string]interface{}{"font_size": fontSize},
				Suggestion:  map[string]interface{}{"font_size": bodyStyle.FontSize},
			})
		}

		// 检查对齐方式
		if alignment != "" && bodyStyle.Alignment != "" && alignment != bodyStyle.Alignment {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          fmt.Sprintf("paragraph-alignment-%d", position),
				Type:        IssueTypeParagraph,
				Severity:    SeverityWarning,
				Page:        page,
				Position:    position,
				Description: fmt.Sprintf("段落对齐方式应为%s，实际为%s", bodyStyle.Alignment, alignment),
				Original:    map[string]interface{}{"alignment": alignment},
				Suggestion:  map[string]interface{}{"alignment": bodyStyle.Alignment},
			})
		}

		// 检查行间距
		lineSpacing, _ := style["line_spacing"].(float64)
		if lineSpacing > 0 && bodyStyle.LineSpacing > 0 && lineSpacing != bodyStyle.LineSpacing {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          fmt.Sprintf("paragraph-line-spacing-%d", position),
				Type:        IssueTypeParagraph,
				Severity:    SeverityWarning,
				Page:        page,
				Position:    position,
				Description: fmt.Sprintf("段落行间距应为%.1f磅，实际为%.1f磅", bodyStyle.LineSpacing, lineSpacing),
				Original:    map[string]interface{}{"line_spacing": lineSpacing},
				Suggestion:  map[string]interface{}{"line_spacing": bodyStyle.LineSpacing},
			})
		}

		// 检查首行缩进
		firstLineIndent, _ := style["first_line_indent"].(float64)
		if firstLineIndent > 0 && bodyStyle.FirstLineIndent > 0 && firstLineIndent != bodyStyle.FirstLineIndent {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          fmt.Sprintf("paragraph-first-line-indent-%d", position),
				Type:        IssueTypeParagraph,
				Severity:    SeverityWarning,
				Page:        page,
				Position:    position,
				Description: fmt.Sprintf("段落首行缩进应为%.1f字符，实际为%.1f字符", bodyStyle.FirstLineIndent, firstLineIndent),
				Original:    map[string]interface{}{"first_line_indent": firstLineIndent},
				Suggestion:  map[string]interface{}{"first_line_indent": bodyStyle.FirstLineIndent},
			})
		}
	}
}

// checkTables 检查表格格式
func (c *PDFChecker) checkTables(ctx context.Context, docPath string, result *CheckResult) {
	// 简化的表格格式检查实现
	// 实际实现需要更复杂的逻辑
}

// checkFigures 检查图表格式
func (c *PDFChecker) checkFigures(ctx context.Context, docPath string, result *CheckResult) {
	// 简化的图表格式检查实现
	// 实际实现需要更复杂的逻辑
}

// updateIssueStatistics 更新问题统计信息
func (c *PDFChecker) updateIssueStatistics(result *CheckResult) {
	result.TotalIssues = len(result.Issues)
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
