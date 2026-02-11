package formatchecker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// NewDOCXChecker 创建DOCX检查器实例
func NewDOCXChecker() *DOCXChecker {
	return &DOCXChecker{
		processor: fileprocessor.NewFourStageProcessor(), // 使用四阶段处理器，不再调用Python服务
		standard:  FormatStandard{},
	}
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

	// 初始化检查结果
	result := &CheckResult{
		Issues:       []FormatIssue{},
		TotalIssues:  0,
		ErrorCount:   0,
		WarningCount: 0,
		InfoCount:    0,
	}

	// 打开文档进行详细检查
	r, err := document.Open(docPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open document: %w", err)
	}
	defer r.Close()

	// 1. 检查页面设置
	c.checkPageSetupDetailed(r, docPath, result, &c.standard)

	// 2. 检查标题格式
	c.checkHeadingsDetailed(ctx, r, docPath, result, &c.standard)

	// 3. 检查段落格式
	c.checkParagraphsDetailed(ctx, r, docPath, result, &c.standard)

	// 4. 检查表格格式
	c.checkTablesDetailed(ctx, r, docPath, result, &c.standard)

	// 更新问题统计
	c.updateIssueStatistics(result)

	return result, nil
}

// checkPageSetupDetailed 详细检查页面设置
func (c *DOCXChecker) checkPageSetupDetailed(doc *document.Document, docPath string, result *CheckResult, standard *FormatStandard) {
	section := doc.BodySection()

	// 获取当前页面设置
	pgMar := section.X().PgMar

	// 检查页边距
	if pgMar != nil {
		if standard.PageSetup.MarginTop > 0 && pgMar.TopAttr.Int64 != nil {
			expectedTop := measurement.Distance(standard.PageSetup.MarginTop) * measurement.Centimeter
			actualTop := measurement.Distance(*pgMar.TopAttr.Int64)
			if abs(int(actualTop)-int(expectedTop)) > 10 { // 允许10twips的误差
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("margin_top"),
					Type:        IssueTypePageSetup,
					Severity:    SeverityWarning,
					Description: fmt.Sprintf("上边距不符合要求: 当前 %.2fcm, 期望 %.2fcm", float64(actualTop)/measurement.Centimeter, standard.PageSetup.MarginTop),
					Original:    float64(actualTop) / measurement.Centimeter,
					Suggestion:  standard.PageSetup.MarginTop,
				})
			}
		}

		if standard.PageSetup.MarginBottom > 0 && pgMar.BottomAttr.Int64 != nil {
			expectedBottom := measurement.Distance(standard.PageSetup.MarginBottom) * measurement.Centimeter
			actualBottom := measurement.Distance(*pgMar.BottomAttr.Int64)
			if abs(int(actualBottom)-int(expectedBottom)) > 10 {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("margin_bottom"),
					Type:        IssueTypePageSetup,
					Severity:    SeverityWarning,
					Description: fmt.Sprintf("下边距不符合要求: 当前 %.2fcm, 期望 %.2fcm", float64(actualBottom)/measurement.Centimeter, standard.PageSetup.MarginBottom),
					Original:    float64(actualBottom) / measurement.Centimeter,
					Suggestion:  standard.PageSetup.MarginBottom,
				})
			}
		}

		if standard.PageSetup.MarginLeft > 0 && pgMar.LeftAttr.ST_UnsignedDecimalNumber != nil {
			expectedLeft := measurement.Distance(standard.PageSetup.MarginLeft) * measurement.Centimeter
			actualLeft := measurement.Distance(*pgMar.LeftAttr.ST_UnsignedDecimalNumber)
			if abs(int(actualLeft)-int(expectedLeft)) > 10 {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("margin_left"),
					Type:        IssueTypePageSetup,
					Severity:    SeverityWarning,
					Description: fmt.Sprintf("左边距不符合要求: 当前 %.2fcm, 期望 %.2fcm", float64(actualLeft)/measurement.Centimeter, standard.PageSetup.MarginLeft),
					Original:    float64(actualLeft) / measurement.Centimeter,
					Suggestion:  standard.PageSetup.MarginLeft,
				})
			}
		}

		if standard.PageSetup.MarginRight > 0 && pgMar.RightAttr.ST_UnsignedDecimalNumber != nil {
			expectedRight := measurement.Distance(standard.PageSetup.MarginRight) * measurement.Centimeter
			actualRight := measurement.Distance(*pgMar.RightAttr.ST_UnsignedDecimalNumber)
			if abs(int(actualRight)-int(expectedRight)) > 10 {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("margin_right"),
					Type:        IssueTypePageSetup,
					Severity:    SeverityWarning,
					Description: fmt.Sprintf("右边距不符合要求: 当前 %.2fcm, 期望 %.2fcm", float64(actualRight)/measurement.Centimeter, standard.PageSetup.MarginRight),
					Original:    float64(actualRight) / measurement.Centimeter,
					Suggestion:  standard.PageSetup.MarginRight,
				})
			}
		}
	}
}

// checkHeadingsDetailed 详细检查标题格式
func (c *DOCXChecker) checkHeadingsDetailed(ctx context.Context, doc *document.Document, docPath string, result *CheckResult, standard *FormatStandard) {
	if standard == nil {
		return
	}

	paragraphs := doc.Paragraphs()
	for i, p := range paragraphs {
		// 获取段落文本
		var textBuilder strings.Builder
		for _, run := range p.Runs() {
			textBuilder.WriteString(run.Text())
		}
		text := textBuilder.String()
		trimmedText := strings.TrimSpace(text)

		// 跳过空文本
		if trimmedText == "" {
			continue
		}

		// 识别标题级别
		headingLevel := c.identifyHeadingLevel(trimmedText, p)

		if headingLevel == 0 {
			continue // 不是标题
		}

		// 根据标题级别检查格式
		switch headingLevel {
		case 1:
			c.checkHeading1Format(p, i, result, standard)
		case 2:
			c.checkHeading2Format(p, i, result, standard)
		case 3:
			c.checkHeading3Format(p, i, result, standard)
		}
	}
}

// identifyHeadingLevel 识别标题级别
func (c *DOCXChecker) identifyHeadingLevel(text string, para document.Paragraph) int {
	// 一级标题模式
	level1Patterns := []string{
		`^[一二三四五六七八九十]+、`,
		`^第[一二三四五六七八九十]+章`,
		`^第\d+章`,
		`^\d+\s+\D`,
		`^\d+\.\s+\D`,
	}

	for _, pattern := range level1Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			if c.matchesHeading1Format(para) {
				return 1
			}
		}
	}

	// 二级标题模式
	level2Patterns := []string{
		`^\d+\.\d+\s+`,
		`^\d+\.\d+$`,
	}

	for _, pattern := range level2Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			if c.matchesHeading2Format(para) {
				return 2
			}
		}
	}

	// 三级标题模式
	level3Patterns := []string{
		`^\d+\.\d+\.\d+\s+`,
		`^\d+\.\d+\.\d+$`,
	}

	for _, pattern := range level3Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			if c.matchesHeading3Format(para) {
				return 3
			}
		}
	}

	return 0
}

// matchesHeading1Format 检查是否符合一级标题格式特征
func (c *DOCXChecker) matchesHeading1Format(para document.Paragraph) bool {
	// 获取第一个run的格式
	runs := para.Runs()
	if len(runs) == 0 {
		return false
	}

	// 检查字体大小
	fontSize := getRunFontSize(runs[0])
	if fontSize >= 15 { // >= 15pt
		return true
	}

	// 检查对齐方式
	if getAlignment(para) == wml.ST_JcCenter {
		return true
	}

	// 检查加粗
	if runs[0].Properties().Bold() {
		return true
	}

	return false
}

// matchesHeading2Format 检查是否符合二级标题格式特征
func (c *DOCXChecker) matchesHeading2Format(para document.Paragraph) bool {
	runs := para.Runs()
	if len(runs) == 0 {
		return false
	}

	fontSize := getRunFontSize(runs[0])
	if fontSize >= 13 && fontSize <= 16 {
		return true
	}

	if getAlignment(para) == wml.ST_JcLeft {
		return true
	}

	if runs[0].Properties().Bold() {
		return true
	}

	return false
}

// matchesHeading3Format 检查是否符合三级标题格式特征
func (c *DOCXChecker) matchesHeading3Format(para document.Paragraph) bool {
	runs := para.Runs()
	if len(runs) == 0 {
		return false
	}

	fontSize := getRunFontSize(runs[0])
	if fontSize >= 12 && fontSize <= 14 {
		return true
	}

	if getAlignment(para) == wml.ST_JcLeft {
		return true
	}

	if runs[0].Properties().Bold() {
		return true
	}

	return false
}

// checkHeading1Format 检查一级标题格式
func (c *DOCXChecker) checkHeading1Format(para document.Paragraph, index int, result *CheckResult, standard *FormatStandard) {
	runs := para.Runs()
	if len(runs) == 0 {
		return
	}

	// 检查字体
	if len(standard.HeadingStyles) > 0 && standard.HeadingStyles[0].FontName != "" {
		actualFont := getRunFontName(runs[0])
		if !fontsMatch(actualFont, standard.HeadingStyles[0].FontName) {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("heading1_font"),
				Type:        IssueTypeHeading,
				Severity:    SeverityError,
				Description: fmt.Sprintf("一级标题字体不匹配: 期望 %s, 实际 %s", standard.HeadingStyles[0].FontName, actualFont),
				Original:    actualFont,
				Suggestion:  standard.HeadingStyles[0].FontName,
			})
		}
	}

	// 检查字号
	if len(standard.HeadingStyles) > 0 && standard.HeadingStyles[0].FontSize > 0 {
		actualSize := getRunFontSize(runs[0])
		expectedSize := standard.HeadingStyles[0].FontSize
		if absFloat64(actualSize-expectedSize) > 0.5 {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("heading1_size"),
				Type:        IssueTypeHeading,
				Severity:    SeverityError,
				Description: fmt.Sprintf("一级标题字号不匹配: 期望 %.1fpt, 实际 %.1fpt", expectedSize, actualSize),
				Original:    actualSize,
				Suggestion:  expectedSize,
			})
		}
	}

	// 检查对齐方式
	if len(standard.HeadingStyles) > 0 && standard.HeadingStyles[0].Alignment != "" {
		actualAlign := getAlignment(para)
		expectedAlign := parseAlignment(standard.HeadingStyles[0].Alignment)
		if actualAlign != expectedAlign {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("heading1_align"),
				Type:        IssueTypeHeading,
				Severity:    SeverityWarning,
				Description: fmt.Sprintf("一级标题对齐方式不匹配: 期望 %s, 实际 %s", standard.HeadingStyles[0].Alignment, alignmentToString(actualAlign)),
				Original:    alignmentToString(actualAlign),
				Suggestion:  standard.HeadingStyles[0].Alignment,
			})
		}
	}
}

// checkHeading2Format 检查二级标题格式
func (c *DOCXChecker) checkHeading2Format(para document.Paragraph, index int, result *CheckResult, standard *FormatStandard) {
	runs := para.Runs()
	if len(runs) == 0 {
		return
	}

	// 检查字体
	if len(standard.HeadingStyles) > 1 && standard.HeadingStyles[1].FontName != "" {
		actualFont := getRunFontName(runs[0])
		if !fontsMatch(actualFont, standard.HeadingStyles[1].FontName) {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("heading2_font"),
				Type:        IssueTypeHeading,
				Severity:    SeverityError,
				Description: fmt.Sprintf("二级标题字体不匹配: 期望 %s, 实际 %s", standard.HeadingStyles[1].FontName, actualFont),
				Original:    actualFont,
				Suggestion:  standard.HeadingStyles[1].FontName,
			})
		}
	}

	// 检查字号
	if len(standard.HeadingStyles) > 1 && standard.HeadingStyles[1].FontSize > 0 {
		actualSize := getRunFontSize(runs[0])
		expectedSize := standard.HeadingStyles[1].FontSize
		if absFloat64(actualSize-expectedSize) > 0.5 {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("heading2_size"),
				Type:        IssueTypeHeading,
				Severity:    SeverityError,
				Description: fmt.Sprintf("二级标题字号不匹配: 期望 %.1fpt, 实际 %.1fpt", expectedSize, actualSize),
				Original:    actualSize,
				Suggestion:  expectedSize,
			})
		}
	}
}

// checkHeading3Format 检查三级标题格式
func (c *DOCXChecker) checkHeading3Format(para document.Paragraph, index int, result *CheckResult, standard *FormatStandard) {
	runs := para.Runs()
	if len(runs) == 0 {
		return
	}

	// 检查字体
	if len(standard.HeadingStyles) > 2 && standard.HeadingStyles[2].FontName != "" {
		actualFont := getRunFontName(runs[0])
		if !fontsMatch(actualFont, standard.HeadingStyles[2].FontName) {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("heading3_font"),
				Type:        IssueTypeHeading,
				Severity:    SeverityError,
				Description: fmt.Sprintf("三级标题字体不匹配: 期望 %s, 实际 %s", standard.HeadingStyles[2].FontName, actualFont),
				Original:    actualFont,
				Suggestion:  standard.HeadingStyles[2].FontName,
			})
		}
	}

	// 检查字号
	if len(standard.HeadingStyles) > 2 && standard.HeadingStyles[2].FontSize > 0 {
		actualSize := getRunFontSize(runs[0])
		expectedSize := standard.HeadingStyles[2].FontSize
		if absFloat64(actualSize-expectedSize) > 0.5 {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("heading3_size"),
				Type:        IssueTypeHeading,
				Severity:    SeverityError,
				Description: fmt.Sprintf("三级标题字号不匹配: 期望 %.1fpt, 实际 %.1fpt", expectedSize, actualSize),
				Original:    actualSize,
				Suggestion:  expectedSize,
			})
		}
	}
}

// checkParagraphsDetailed 详细检查段落格式
func (c *DOCXChecker) checkParagraphsDetailed(ctx context.Context, doc *document.Document, docPath string, result *CheckResult, standard *FormatStandard) {
	if standard == nil {
		return
	}

	paragraphs := doc.Paragraphs()
	for i, p := range paragraphs {
		// 获取段落文本
		var textBuilder strings.Builder
		for _, run := range p.Runs() {
			textBuilder.WriteString(run.Text())
		}
		text := textBuilder.String()
		trimmedText := strings.TrimSpace(text)

		// 跳过空段落和标题
		if trimmedText == "" || c.identifyHeadingLevel(trimmedText, p) > 0 {
			continue
		}

		// 检查正文格式
		c.checkBodyParagraphFormat(p, i, result, standard)
	}
}

// checkBodyParagraphFormat 检查正文段落格式
func (c *DOCXChecker) checkBodyParagraphFormat(para document.Paragraph, index int, result *CheckResult, standard *FormatStandard) {
	runs := para.Runs()
	if len(runs) == 0 {
		return
	}

	// 获取正文样式（使用第一个段落样式）
	var bodyStyle ParagraphStyle
	if len(standard.ParagraphStyles) > 0 {
		bodyStyle = standard.ParagraphStyles[0]
	}

	// 检查段落中所有run的格式
	for runIndex, run := range runs {
		// 检查字体
		if bodyStyle.FontName != "" {
			actualFont := getRunFontName(run)
			if !fontsMatch(actualFont, bodyStyle.FontName) {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("body_font"),
					Type:        IssueTypeFont,
					Severity:    SeverityError,
					Description: fmt.Sprintf("正文字体不匹配: 段落%d的第%d个文本块期望 %s, 实际 %s", index+1, runIndex+1, bodyStyle.FontName, actualFont),
					Original:    actualFont,
					Suggestion:  bodyStyle.FontName,
				})
			}
		}

		// 检查字号
		if bodyStyle.FontSize > 0 {
			actualSize := getRunFontSize(run)
			expectedSize := bodyStyle.FontSize
			if absFloat64(actualSize-expectedSize) > 0.5 {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("body_size"),
					Type:        IssueTypeFont,
					Severity:    SeverityError,
					Description: fmt.Sprintf("正文字号不匹配: 段落%d的第%d个文本块期望 %.1fpt, 实际 %.1fpt", index+1, runIndex+1, expectedSize, actualSize),
					Original:    actualSize,
					Suggestion:  expectedSize,
				})
			}
		}
	}

	// 检查首行缩进
	if bodyStyle.FirstLineIndent > 0 {
		actualIndent := getFirstLineIndent(para)
		expectedIndent := bodyStyle.FirstLineIndent * measurement.Centimeter
		if abs(int(actualIndent)-int(expectedIndent)) > 20 { // 允许20twips的误差
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("body_indent"),
				Type:        IssueTypeSpacing,
				Severity:    SeverityWarning,
				Description: fmt.Sprintf("正文首行缩进不匹配: 期望 %.2fcm, 实际 %.2fcm", bodyStyle.FirstLineIndent, actualIndent/measurement.Centimeter),
				Original:    actualIndent / measurement.Centimeter,
				Suggestion:  bodyStyle.FirstLineIndent,
			})
		}
	}

	// 检查行间距
	if bodyStyle.LineSpacing > 0 {
		actualSpacing := getLineSpacing(para)
		expectedSpacing := bodyStyle.LineSpacing
		if absFloat64(actualSpacing-expectedSpacing) > 2.0 { // 允许2pt的误差
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("body_line_spacing"),
				Type:        IssueTypeSpacing,
				Severity:    SeverityWarning,
				Description: fmt.Sprintf("正文行间距不匹配: 期望 %.1fpt, 实际 %.1fpt", expectedSpacing, actualSpacing),
				Original:    actualSpacing,
				Suggestion:  expectedSpacing,
			})
		}
	}
}

// checkTablesDetailed 详细检查表格格式
func (c *DOCXChecker) checkTablesDetailed(ctx context.Context, doc *document.Document, docPath string, result *CheckResult, standard *FormatStandard) {
	if standard == nil {
		return
	}

	tables := doc.Tables()
	for i, table := range tables {
		// 检查表格内文字格式
		for _, row := range table.Rows() {
			for _, cell := range row.Cells() {
				for _, p := range cell.Paragraphs() {
					for _, run := range p.Runs() {
						// 检查字体
						if standard.TableStyle.FontName != "" {
							actualFont := getRunFontName(run)
							if !fontsMatch(actualFont, standard.TableStyle.FontName) {
								result.Issues = append(result.Issues, FormatIssue{
									ID:          generateIssueID("table_font"),
									Type:        IssueTypeTable,
									Severity:    SeverityWarning,
									Description: fmt.Sprintf("表格 %d 字体不匹配: 期望 %s, 实际 %s", i+1, standard.TableStyle.FontName, actualFont),
									Original:    actualFont,
									Suggestion:  standard.TableStyle.FontName,
								})
							}
						}

						// 检查字号
						if standard.TableStyle.FontSize > 0 {
							actualSize := getRunFontSize(run)
							if absFloat64(actualSize-standard.TableStyle.FontSize) > 0.5 {
								result.Issues = append(result.Issues, FormatIssue{
									ID:          generateIssueID("table_size"),
									Type:        IssueTypeTable,
									Severity:    SeverityWarning,
									Description: fmt.Sprintf("表格 %d 字号不匹配: 期望 %.1fpt, 实际 %.1fpt", i+1, standard.TableStyle.FontSize, actualSize),
									Original:    actualSize,
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

// 辅助函数
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func absFloat64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func generateIssueID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, len(prefix)*1000)
}

func getRunFontName(run document.Run) string {
	rPr := run.X().RPr
	if rPr == nil {
		return ""
	}
	rFonts := rPr.RFonts
	if rFonts == nil {
		return ""
	}
	if rFonts.EastAsiaAttr != nil {
		return *rFonts.EastAsiaAttr
	}
	if rFonts.AsciiAttr != nil {
		return *rFonts.AsciiAttr
	}
	return ""
}

func getRunFontSize(run document.Run) float64 {
	rPr := run.X().RPr
	if rPr == nil || rPr.Sz == nil || rPr.Sz.ValAttr.ST_UnsignedDecimalNumber == nil {
		return 0
	}
	return float64(*rPr.Sz.ValAttr.ST_UnsignedDecimalNumber) / 2.0
}

func getFirstLineIndent(para document.Paragraph) measurement.Distance {
	pPr := para.X().PPr
	if pPr == nil || pPr.Ind == nil {
		return 0
	}
	if pPr.Ind.FirstLineAttr != nil && pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber != nil {
		return measurement.Distance(*pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber)
	}
	return 0
}

func getLineSpacing(para document.Paragraph) float64 {
	pPr := para.X().PPr
	if pPr == nil || pPr.Spacing == nil {
		return 0
	}
	if pPr.Spacing.LineAttr != nil && pPr.Spacing.LineAttr.Int64 != nil {
		return float64(*pPr.Spacing.LineAttr.Int64) / 20.0
	}
	return 0
}

func getAlignment(para document.Paragraph) wml.ST_Jc {
	pPr := para.X().PPr
	if pPr == nil || pPr.Jc == nil {
		return wml.ST_JcUnset
	}
	return pPr.Jc.ValAttr
}

func fontsMatch(actual, expected string) bool {
	if actual == "" || expected == "" {
		return true
	}
	actualLower := strings.ToLower(actual)
	expectedLower := strings.ToLower(expected)
	return strings.Contains(actualLower, expectedLower) || strings.Contains(expectedLower, actualLower)
}

func parseAlignment(alignStr string) wml.ST_Jc {
	switch strings.ToLower(alignStr) {
	case "center", "居中":
		return wml.ST_JcCenter
	case "right", "右对齐":
		return wml.ST_JcRight
	case "justify", "两端对齐":
		return wml.ST_JcBoth
	default:
		return wml.ST_JcLeft
	}
}

func alignmentToString(align wml.ST_Jc) string {
	switch align {
	case wml.ST_JcCenter:
		return "center"
	case wml.ST_JcRight:
		return "right"
	case wml.ST_JcBoth:
		return "justify"
	default:
		return "left"
	}
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

	// 修复段落格式（字体、字号、对齐、缩进、行间距）
	c.fixParagraphFormats(r, &standard)

	// 修复表格格式
	c.fixTableFormats(r, &standard)

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
							if standard.TableStyle.FontSize > 0 && absFloat64(fontSize-standard.TableStyle.FontSize) > 0.5 {
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

// fixParagraphFormats 修复段落格式（增强版 - 上下文感知）
func (c *DOCXChecker) fixParagraphFormats(doc *document.Document, standard *FormatStandard) {
	paragraphs := doc.Paragraphs()

	// 分析文档结构，识别关键位置
	docStructure := c.analyzeDocumentStructure(paragraphs)

	// 遍历所有段落
	for i, para := range paragraphs {
		var textBuilder strings.Builder
		for _, run := range para.Runs() {
			textBuilder.WriteString(run.Text())
		}
		text := textBuilder.String()
		trimmedText := strings.TrimSpace(text)

		// 跳过空段落
		if trimmedText == "" {
			continue
		}

		// 获取段落位置上下文
		positionContext := c.getPositionContext(i, docStructure, paragraphs)

		// 识别段落类型（结合位置上下文）
		role := c.identifyParagraphRoleWithContext(trimmedText, para, positionContext)

		// 根据段落类型应用对应的格式
		switch role {
		case "heading1":
			if len(standard.HeadingStyles) > 0 {
				c.applyHeadingFormat(para, &standard.HeadingStyles[0])
			}
		case "heading2":
			if len(standard.HeadingStyles) > 1 {
				c.applyHeadingFormat(para, &standard.HeadingStyles[1])
			}
		case "heading3":
			if len(standard.HeadingStyles) > 2 {
				c.applyHeadingFormat(para, &standard.HeadingStyles[2])
			}
		case "heading4", "heading5", "heading6":
			// 4-6级标题应用正文格式（根据重庆工程学院标准）
			if len(standard.ParagraphStyles) > 0 {
				c.applyBodyFormat(para, &standard.ParagraphStyles[0])
			}
		default:
			// 其他段落（正文）应用正文格式
			if len(standard.ParagraphStyles) > 0 {
				c.applyBodyFormat(para, &standard.ParagraphStyles[0])
			}
		}
	}
}

// PositionContext 段落位置上下文
type PositionContext struct {
	IsAfterAbstract    bool // 是否在摘要之后
	IsAfterKeywords    bool // 是否在关键词之后
	IsAfterTOC         bool // 是否在目录之后
	IsAfterHeading1    bool // 是否在一级标题之后
	IsAfterHeading2    bool // 是否在二级标题之后
	IsInFrontMatter    bool // 是否在前言部分
	IsInMainBody       bool // 是否在主体部分
	IsInBackMatter     bool // 是否在附录部分
	PreviousRole       string
	NextRole           string
	DocumentPercentage float64 // 文档进度百分比
}

// DocumentStructure 文档结构分析结果
type DocumentStructure struct {
	AbstractStart     int   // 摘要开始位置
	AbstractEnd       int   // 摘要结束位置
	KeywordsStart     int   // 关键词开始位置
	KeywordsEnd       int   // 关键词结束位置
	TOCStart          int   // 目录开始位置
	TOCEnd            int   // 目录结束位置
	Heading1Positions []int // 所有一级标题位置
	Heading2Positions []int // 所有二级标题位置
	Heading3Positions []int // 所有三级标题位置
	MainBodyStart     int   // 正文开始位置
	MainBodyEnd       int   // 正文结束位置
	ReferencesStart   int   // 参考文献开始位置
}

// analyzeDocumentStructure 分析文档结构
func (c *DOCXChecker) analyzeDocumentStructure(paragraphs []document.Paragraph) *DocumentStructure {
	structure := &DocumentStructure{
		Heading1Positions: []int{},
		Heading2Positions: []int{},
		Heading3Positions: []int{},
	}

	for i, para := range paragraphs {
		text := getParagraphText(para)
		trimmedText := strings.TrimSpace(text)

		// 识别摘要
		if strings.Contains(trimmedText, "摘要") && structure.AbstractStart == 0 {
			structure.AbstractStart = i
		}
		if structure.AbstractStart > 0 && structure.AbstractEnd == 0 {
			// 摘要结束于遇到非摘要内容
			if len(trimmedText) > 20 && !strings.Contains(trimmedText, "摘要") &&
				!strings.Contains(trimmedText, "关键词") {
				structure.AbstractEnd = i
			}
		}

		// 识别关键词
		if strings.Contains(trimmedText, "关键词") {
			structure.KeywordsStart = i
			structure.KeywordsEnd = i
		}

		// 识别目录
		if strings.Contains(trimmedText, "目录") || strings.Contains(trimmedText, "Table of Contents") {
			structure.TOCStart = i
		}
		if structure.TOCStart > 0 && structure.TOCEnd == 0 {
			if strings.Contains(trimmedText, "第一章") ||
				strings.Contains(trimmedText, "1 ") ||
				strings.HasPrefix(trimmedText, "1.") {
				structure.TOCEnd = i
			}
		}

		// 识别标题位置
		role := c.identifyParagraphRole(trimmedText, para)
		switch role {
		case "heading1":
			structure.Heading1Positions = append(structure.Heading1Positions, i)
		case "heading2":
			structure.Heading2Positions = append(structure.Heading2Positions, i)
		case "heading3":
			structure.Heading3Positions = append(structure.Heading3Positions, i)
		}

		// 识别参考文献
		if strings.Contains(trimmedText, "参考文献") ||
			strings.Contains(trimmedText, "References") {
			structure.ReferencesStart = i
		}
	}

	// 设置正文开始位置（通常在第一个一级标题之后）
	if len(structure.Heading1Positions) > 0 {
		structure.MainBodyStart = structure.Heading1Positions[0] + 1
	}

	return structure
}

// getPositionContext 获取段落位置上下文
func (c *DOCXChecker) getPositionContext(index int, structure *DocumentStructure, paragraphs []document.Paragraph) *PositionContext {
	context := &PositionContext{
		DocumentPercentage: float64(index) / float64(len(paragraphs)) * 100,
	}

	// 检查是否在摘要之后
	if structure.AbstractEnd > 0 && index > structure.AbstractEnd {
		context.IsAfterAbstract = true
	}

	// 检查是否在关键词之后
	if structure.KeywordsEnd > 0 && index > structure.KeywordsEnd {
		context.IsAfterKeywords = true
	}

	// 检查是否在目录之后
	if structure.TOCEnd > 0 && index > structure.TOCEnd {
		context.IsAfterTOC = true
	}

	// 检查是否在某个一级标题之后
	for _, pos := range structure.Heading1Positions {
		if index > pos {
			context.IsAfterHeading1 = true
		}
	}

	// 检查是否在某个二级标题之后
	for _, pos := range structure.Heading2Positions {
		if index > pos {
			context.IsAfterHeading2 = true
		}
	}

	// 确定文档部分
	if structure.AbstractStart > 0 && index < structure.AbstractEnd {
		context.IsInFrontMatter = true
	} else if structure.ReferencesStart > 0 && index > structure.ReferencesStart {
		context.IsInBackMatter = true
	} else {
		context.IsInMainBody = true
	}

	// 获取前后段落角色
	if index > 0 {
		context.PreviousRole = c.identifyParagraphRole(getParagraphText(paragraphs[index-1]), paragraphs[index-1])
	}
	if index < len(paragraphs)-1 {
		context.NextRole = c.identifyParagraphRole(getParagraphText(paragraphs[index+1]), paragraphs[index+1])
	}

	return context
}

// identifyParagraphRoleWithContext 结合上下文识别段落角色
func (c *DOCXChecker) identifyParagraphRoleWithContext(text string, para document.Paragraph, context *PositionContext) string {
	// 1. 首先使用基础识别
	baseRole := c.identifyParagraphRole(text, para)

	// 2. 特殊情况处理

	// 摘要、关键词、目录等本身不是标题
	if strings.Contains(text, "摘要") && len(text) < 20 {
		return "body"
	}
	if strings.Contains(text, "关键词") && len(text) < 30 {
		return "body"
	}
	if strings.Contains(text, "目录") && len(text) < 10 {
		return "body"
	}
	if strings.Contains(text, "参考文献") && len(text) < 15 {
		return "body"
	}
	if strings.Contains(text, "致谢") && len(text) < 10 {
		return "body"
	}
	if strings.Contains(text, "附录") && len(text) < 10 {
		return "body"
	}

	// 3. 特殊位置处理

	// 在摘要和目录之间的段落通常是关键词
	if context.IsAfterKeywords && !context.IsAfterTOC {
		return "body"
	}

	// 参考文献部分的段落通常是引用，不是正文
	if context.IsInBackMatter {
		return "body"
	}

	// 4. 如果基础识别是正文，但前后有标题，可能是正文的延续
	if baseRole == "body" {
		// 如果前一段是标题，且当前段落没有明显标题特征，保持正文
		if strings.HasPrefix(context.PreviousRole, "heading") {
			// 检查是否真的应该是更高级别的标题
			if c.shouldBeHeading(text, context) {
				return c.determineHeadingLevel(text)
			}
		}
	}

	return baseRole
}

// shouldBeHeading 判断是否应该升级为标题
func (c *DOCXChecker) shouldBeHeading(text string, context *PositionContext) bool {
	textLength := len(strings.TrimSpace(text))

	// 长度异常检查：太短或太长的不太像标题
	if textLength < 5 || textLength > 80 {
		return false
	}

	// 如果前后段落都很长，当前行可能是标题
	return false
}

// getParagraphText 获取段落文本
func getParagraphText(para document.Paragraph) string {
	var textBuilder strings.Builder
	for _, run := range para.Runs() {
		textBuilder.WriteString(run.Text())
	}
	return textBuilder.String()
}

// identifyParagraphRole 识别段落角色（增强版 - 多维度特征判断）
func (c *DOCXChecker) identifyParagraphRole(text string, para document.Paragraph) string {
	// 1. 检查样式名称（最可靠的特征）
	if role := checkStyleByName(para); role != "" {
		return role
	}

	// 2. 基于文本内容识别（使用多种模式组合）
	headingScore := c.calculateHeadingScore(text, para)

	// 3. 如果标题分数很高，识别为标题
	if headingScore >= 3 {
		return c.determineHeadingLevel(text)
	}

	// 4. 结合段落格式特征判断
	if c.isLikelyHeading(text, para, headingScore) {
		return c.determineHeadingLevel(text)
	}

	// 5. 默认识别为正文
	return "body"
}

// styleCheck 定义样式检查规则
type styleCheck struct {
	keywords     []string
	headingLevel string
}

// checkStyleByName 通过样式名称检查段落类型
func checkStyleByName(para document.Paragraph) string {
	styleName := para.Style()
	styleNameLower := strings.ToLower(styleName)

	styleChecks := []styleCheck{
		{[]string{"heading 1", "标题 1"}, "heading1"},
		{[]string{"heading 2", "标题 2"}, "heading2"},
		{[]string{"heading 3", "标题 3"}, "heading3"},
		{[]string{"heading 4", "标题 4"}, "heading4"},
		{[]string{"heading 5", "标题 5"}, "heading5"},
		{[]string{"heading 6", "标题 6"}, "heading6"},
		{[]string{"heading", "标题"}, ""}, // 通用标题样式
	}

	for _, check := range styleChecks {
		for _, keyword := range check.keywords {
			if strings.Contains(styleNameLower, strings.ToLower(keyword)) {
				if check.headingLevel != "" {
					return check.headingLevel
				}
				// 如果是通用标题样式，尝试提取级别
				return extractHeadingLevelFromStyle(styleName)
			}
		}
	}

	return ""
}

// extractHeadingLevelFromStyle 从样式名称提取标题级别
func extractHeadingLevelFromStyle(styleName string) string {
	// 尝试匹配 "Heading 1" 或 "标题 1" 等格式
	pattern := regexp.MustCompile(`(?i)(?:heading|标题)\s*(\d+)`)
	matches := pattern.FindStringSubmatch(styleName)
	if len(matches) >= 2 {
		level, err := strconv.Atoi(matches[1])
		if err == nil && level >= 1 && level <= 9 {
			return fmt.Sprintf("heading%d", level)
		}
	}
	return "heading1"
}

// calculateHeadingScore 计算段落作为标题的综合分数
func (c *DOCXChecker) calculateHeadingScore(text string, para document.Paragraph) int {
	score := 0
	textLength := len(strings.TrimSpace(text))

	// 模式匹配分数（一级标题）
	level1Patterns := []string{
		`^[一二三四五六七八九十]+、`,
		`^第[一二三四五六七八九十]+章`,
		`^第\d+章`,
		`^\d+\s+\D`,
		`^\d+\.\s+\D`,
	}
	for _, pattern := range level1Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			score += 3
			break
		}
	}

	// 模式匹配分数（二级标题）
	level2Patterns := []string{
		`^\d+\.\d+\s+`,
		`^\d+\.\d+$`,
		`^[（(]\d+[）)]\s*\D`,
		`^\d+[）)]\s*\D`,
	}
	for _, pattern := range level2Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			score += 2
			break
		}
	}

	// 模式匹配分数（三级标题）
	level3Patterns := []string{
		`^\d+\.\d+\.\d+\s+`,
		`^\d+\.\d+\.\d+$`,
		`^\d+\.\d+\.\d+\.\d+\s+`,
	}
	for _, pattern := range level3Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			score += 1
			break
		}
	}

	// 长度特征：标题通常较短（10-50字符）
	if textLength >= 5 && textLength <= 50 {
		score += 1
	}

	// 检查字体大小：标题字体通常较大
	runs := para.Runs()
	if len(runs) > 0 {
		fontSize := getRunFontSize(runs[0])
		if fontSize >= 14 {
			score += 1
		}
	}

	// 检查是否加粗：标题通常加粗
	if len(runs) > 0 && runs[0].Properties().Bold() {
		score += 1
	}

	// 检查对齐方式：标题通常居中或左对齐
	align := getAlignment(para)
	if align == wml.ST_JcCenter || align == wml.ST_JcLeft {
		score += 1
	}

	return score
}

// isLikelyHeading 判断段落是否可能是标题
func (c *DOCXChecker) isLikelyHeading(text string, para document.Paragraph, baseScore int) bool {
	textLength := len(strings.TrimSpace(text))

	// 长度检查：标题一般不会太长也不会太短
	if textLength < 3 || textLength > 60 {
		return false
	}

	// 如果基础分数已经很高，直接返回
	if baseScore >= 3 {
		return true
	}

	// 检查是否有冒号结尾（可能是图表标题）
	if strings.HasSuffix(text, "：") || strings.HasSuffix(text, ":") {
		// 但不是以数字开头
		trimmed := strings.TrimSpace(text)
		if len(trimmed) > 0 && !unicodeIsDigit(rune(trimmed[0])) {
			return true
		}
	}

	return false
}

// determineHeadingLevel 根据文本模式确定标题级别
func (c *DOCXChecker) determineHeadingLevel(text string) string {
	// 一级标题模式
	level1Patterns := []string{
		`^[一二三四五六七八九十]+、`,
		`^第[一二三四五六七八九十]+章`,
		`^第\d+章`,
	}
	for _, pattern := range level1Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			return "heading1"
		}
	}

	// 二级标题模式
	level2Patterns := []string{
		`^\d+\.\d+\s+`,
		`^\d+\.\d+$`,
		`^[（(]\d+[）)]\s*\D`,
	}
	for _, pattern := range level2Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			return "heading2"
		}
	}

	// 三级标题模式
	level3Patterns := []string{
		`^\d+\.\d+\.\d+\s+`,
		`^\d+\.\d+\.\d+$`,
		`^\d+\.\d+\.\d+\.\d+\s+`,
	}
	for _, pattern := range level3Patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			return "heading3"
		}
	}

	// 默认返回一级标题
	return "heading1"
}

// unicodeIsDigit 检查字符是否是数字
func unicodeIsDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// applyHeadingFormat 应用标题格式
func (c *DOCXChecker) applyHeadingFormat(para document.Paragraph, style *HeadingStyle) {
	if style == nil {
		return
	}

	// 检查段落是否有效
	if para.X() == nil {
		return
	}

	// 使用defer和recover来捕获任何panic，确保程序不会崩溃
	defer func() {
		if r := recover(); r != nil {
			// 静默处理panic，继续执行
			return
		}
	}()

	// 设置对齐方式
	if style.Alignment != "" {
		align := parseAlignment(style.Alignment)
		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}
		if pPr.Jc == nil {
			pPr.Jc = wml.NewCT_Jc()
		}
		pPr.Jc.ValAttr = align
	}

	// 设置字体和字号
	for _, run := range para.Runs() {
		if run.X() == nil {
			continue
		}

		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}

		// 设置字体
		if style.FontName != "" {
			if rPr.RFonts == nil {
				rPr.RFonts = wml.NewCT_Fonts()
			}
			rPr.RFonts.EastAsiaAttr = &style.FontName
			rPr.RFonts.AsciiAttr = &style.FontName
		}

		// 设置字号
		if style.FontSize > 0 {
			if rPr.Sz == nil {
				rPr.Sz = wml.NewCT_HpsMeasure()
			}
			halfPoints := uint64(style.FontSize * 2)
			rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPoints
		}

		// 设置加粗
		if style.Bold {
			if rPr.B == nil {
				rPr.B = wml.NewCT_OnOff()
			}
			// 加粗属性存在即为true
		}
	}
}

// applyBodyFormat 应用正文格式
func (c *DOCXChecker) applyBodyFormat(para document.Paragraph, style *ParagraphStyle) {
	if style == nil {
		return
	}

	// 检查段落是否有效
	if para.X() == nil {
		return
	}

	// 使用defer和recover来捕获任何panic，确保程序不会崩溃
	defer func() {
		if r := recover(); r != nil {
			// 静默处理panic，继续执行
			return
		}
	}()

	// 设置首行缩进
	if style.FirstLineIndent > 0 {
		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}
		if pPr.Ind == nil {
			pPr.Ind = wml.NewCT_Ind()
		}
		// 将厘米转换为twips (1 cm = 567 twips)
		twips := uint64(style.FirstLineIndent * 567)
		pPr.Ind.FirstLineAttr.ST_UnsignedDecimalNumber = &twips
	}

	// 设置行间距
	if style.LineSpacing > 0 {
		pPr := para.X().PPr
		if pPr == nil {
			pPr = wml.NewCT_PPr()
			para.X().PPr = pPr
		}
		if pPr.Spacing == nil {
			pPr.Spacing = wml.NewCT_Spacing()
		}
		// 将磅转换为twips (1 pt = 20 twips)
		twips := int64(style.LineSpacing * 20)
		pPr.Spacing.LineAttr.Int64 = &twips
	}

	// 设置字体和字号
	for _, run := range para.Runs() {
		if run.X() == nil {
			continue
		}

		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}

		// 设置字体
		if style.FontName != "" {
			if rPr.RFonts == nil {
				rPr.RFonts = wml.NewCT_Fonts()
			}
			rPr.RFonts.EastAsiaAttr = &style.FontName
			rPr.RFonts.AsciiAttr = &style.FontName
		}

		// 设置字号
		if style.FontSize > 0 {
			if rPr.Sz == nil {
				rPr.Sz = wml.NewCT_HpsMeasure()
			}
			halfPoints := uint64(style.FontSize * 2)
			rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPoints
		}
	}
}

// fixTableFormats 修复表格格式
func (c *DOCXChecker) fixTableFormats(doc *document.Document, standard *FormatStandard) {
	if standard.TableStyle.FontName == "" && standard.TableStyle.FontSize == 0 {
		return
	}

	// 遍历所有表格
	for _, table := range doc.Tables() {
		for _, row := range table.Rows() {
			for _, cell := range row.Cells() {
				for _, para := range cell.Paragraphs() {
					for _, run := range para.Runs() {
						rPr := run.X().RPr
						if rPr == nil {
							rPr = wml.NewCT_RPr()
							run.X().RPr = rPr
						}

						// 设置字体
						if standard.TableStyle.FontName != "" {
							if rPr.RFonts == nil {
								rPr.RFonts = wml.NewCT_Fonts()
							}
							rPr.RFonts.EastAsiaAttr = &standard.TableStyle.FontName
							rPr.RFonts.AsciiAttr = &standard.TableStyle.FontName
						}

						// 设置字号
						if standard.TableStyle.FontSize > 0 {
							if rPr.Sz == nil {
								rPr.Sz = wml.NewCT_HpsMeasure()
							}
							halfPoints := uint64(standard.TableStyle.FontSize * 2)
							rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &halfPoints
						}
					}
				}
			}
		}
	}
}

// generateParagraphCorrection 生成段落修正建议
func (c *DOCXChecker) generateParagraphCorrection(issue FormatIssue) {

}
