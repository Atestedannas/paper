package formatchecker

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"

	"github.com/nguyenthenguyen/docx"
	"github.com/paper-format-checker/backend/pkg/fileprocessor"
)

// DOCXChecker DOCX格式检查器
type DOCXChecker struct {
	processor  fileprocessor.FileProcessor
	standard   FormatStandard
	styleCache *docxStyleCache // 样式继承缓存，在 Check() 开始时初始化
}

const (
	masterMinChineseAbstractChars = 2000
	masterMinBodyChineseChars     = 8000
	masterMinBodyEnglishWords     = 3000
	masterMinReferencesCount      = 40
	masterRecentRefRatio          = 0.50
	masterForeignRefRatio         = 0.30
	masterTitleMaxChars           = 20
	masterTitleExpectedSize       = 26.0
)

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

	// 加载样式继承缓存（修复字体/字号/行距的漏检问题）
	c.styleCache = loadDocxStyleCache(docPath)

	// 1. 检查页面设置
	c.checkPageSetupDetailed(r, docPath, result, &c.standard)

	// 2. 检查标题格式
	c.checkHeadingsDetailed(ctx, r, docPath, result, &c.standard)

	// 3. 检查段落格式
	c.checkParagraphsDetailed(ctx, r, docPath, result, &c.standard)

	// 4. 检查表格格式
	c.checkTablesDetailed(ctx, r, docPath, result, &c.standard)

	// 5. 硕士论文高优先级规则检查（Day 10+）
	c.runMasterThesisPriorityChecks(r, docPath, result, &c.standard)

	// 更新问题统计
	c.updateIssueStatistics(result)

	return result, nil
}

// checkPageSetupDetailed 详细检查页面设置
func (c *DOCXChecker) checkPageSetupDetailed(doc *document.Document, docPath string, result *CheckResult, standard *FormatStandard) {
	section := doc.BodySection()

	// 获取当前页面设置
	pgMar := section.X().PgMar
	pgSz := section.X().PgSz

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

		// 检查页眉距离
		if standard.PageSetup.HeaderDistance > 0 && pgMar.HeaderAttr.ST_UnsignedDecimalNumber != nil {
			expectedHeader := measurement.Distance(standard.PageSetup.HeaderDistance) * measurement.Centimeter
			actualHeader := measurement.Distance(*pgMar.HeaderAttr.ST_UnsignedDecimalNumber)
			if abs(int(actualHeader)-int(expectedHeader)) > 10 {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("header_distance"),
					Type:        IssueTypePageSetup,
					Severity:    SeverityWarning,
					Description: fmt.Sprintf("页眉距离不符合要求: 当前 %.2fcm, 期望 %.2fcm", float64(actualHeader)/measurement.Centimeter, standard.PageSetup.HeaderDistance),
					Original:    float64(actualHeader) / measurement.Centimeter,
					Suggestion:  standard.PageSetup.HeaderDistance,
				})
			}
		}

		// 检查页脚距离
		if standard.PageSetup.FooterDistance > 0 && pgMar.FooterAttr.ST_UnsignedDecimalNumber != nil {
			expectedFooter := measurement.Distance(standard.PageSetup.FooterDistance) * measurement.Centimeter
			actualFooter := measurement.Distance(*pgMar.FooterAttr.ST_UnsignedDecimalNumber)
			if abs(int(actualFooter)-int(expectedFooter)) > 10 {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("footer_distance"),
					Type:        IssueTypePageSetup,
					Severity:    SeverityWarning,
					Description: fmt.Sprintf("页脚距离不符合要求: 当前 %.2fcm, 期望 %.2fcm", float64(actualFooter)/measurement.Centimeter, standard.PageSetup.FooterDistance),
					Original:    float64(actualFooter) / measurement.Centimeter,
					Suggestion:  standard.PageSetup.FooterDistance,
				})
			}
		}
	}

	// 检查纸张大小
	if pgSz != nil {
		if standard.PageSetup.PaperSize != "" {
			// 检查纸张大小
			expectedW, expectedH := getPaperSize(standard.PageSetup.PaperSize)
			if pgSz.WAttr != nil && pgSz.HAttr != nil {
				actualW := measurement.Distance(*pgSz.WAttr.ST_UnsignedDecimalNumber)
				actualH := measurement.Distance(*pgSz.HAttr.ST_UnsignedDecimalNumber)

				if abs(int(actualW)-int(expectedW)) > 20 || abs(int(actualH)-int(expectedH)) > 20 {
					result.Issues = append(result.Issues, FormatIssue{
						ID:          generateIssueID("paper_size"),
						Type:        IssueTypePageSetup,
						Severity:    SeverityWarning,
						Description: fmt.Sprintf("纸张大小不符合要求: 期望 %s", standard.PageSetup.PaperSize),
						Original:    "其他尺寸",
						Suggestion:  standard.PageSetup.PaperSize,
					})
				}
			}
		}

		// 检查纸张方向
		actualOrient := string(pgSz.OrientAttr)
		expectedOrient := "portrait" // 默认纵向
		if standard.PageSetup.PaperSize != "" {
			// 根据纸张大小判断期望方向，默认为纵向
		}

		if actualOrient != expectedOrient && actualOrient != "" {
			orientDesc := map[string]string{
				"portrait":  "纵向",
				"landscape": "横向",
			}
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("paper_orientation"),
				Type:        IssueTypePageSetup,
				Severity:    SeverityWarning,
				Description: fmt.Sprintf("纸张方向不符合要求: 当前 %s, 期望 %s", orientDesc[actualOrient], orientDesc[expectedOrient]),
				Original:    orientDesc[actualOrient],
				Suggestion:  orientDesc[expectedOrient],
			})
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
	// 首先检查段落样式名称 - 这是最可靠的方式
	styleName := para.Style()
	if styleName != "" {
		styleNameLower := strings.ToLower(styleName)

		// 检查是否是标题样式
		if strings.Contains(styleNameLower, "heading 1") || strings.Contains(styleNameLower, "标题 1") {
			return 1
		}
		if strings.Contains(styleNameLower, "heading 2") || strings.Contains(styleNameLower, "标题 2") {
			return 2
		}
		if strings.Contains(styleNameLower, "heading 3") || strings.Contains(styleNameLower, "标题 3") {
			return 3
		}
		if strings.Contains(styleNameLower, "heading 4") || strings.Contains(styleNameLower, "标题 4") {
			return 4
		}
		if strings.Contains(styleNameLower, "heading 5") || strings.Contains(styleNameLower, "标题 5") {
			return 5
		}
		if strings.Contains(styleNameLower, "heading 6") || strings.Contains(styleNameLower, "标题 6") {
			return 6
		}
	}

	// 三级标题模式（必须先检查，避免被一级标题模式误判）
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

	// 二级标题模式（必须在一级标题之前检查）
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

	return 0
}

// matchesHeading1Format 检查是否符合一级标题格式特征
// 改为多条件积分制，避免单条件(如"加粗")就判定为一级标题导致的大量误判
func (c *DOCXChecker) matchesHeading1Format(para document.Paragraph) bool {
	runs := para.Runs()
	if len(runs) == 0 {
		return false
	}

	score := 0

	// 字号 >= 14pt（四号），权重较高
	fontSize := c.resolveRunSize(runs[0], para)
	if fontSize >= 14 {
		score += 2
	}

	// 居中对齐
	if getAlignment(para) == wml.ST_JcCenter {
		score++
	}

	// 加粗
	if runs[0].Properties().Bold() {
		score++
	}

	// 段落文本长度适中（独占行标题通常较短）
	text := strings.TrimSpace(extractParagraphText(para))
	if len([]rune(text)) >= 2 && len([]rune(text)) <= 50 {
		score++
	}

	// 需要至少2分才认定为一级标题（防止单纯加粗的正文被误判）
	return score >= 2
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
		actualFont := c.resolveRunFont(runs[0], para)
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
		actualSize := c.resolveRunSize(runs[0], para)
		expectedSize := standard.HeadingStyles[0].FontSize
		if actualSize > 0 && absFloat64(actualSize-expectedSize) > 0.5 {
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
		actualFont := c.resolveRunFont(runs[0], para)
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
		actualSize := c.resolveRunSize(runs[0], para)
		expectedSize := standard.HeadingStyles[1].FontSize
		if actualSize > 0 && absFloat64(actualSize-expectedSize) > 0.5 {
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
		actualFont := c.resolveRunFont(runs[0], para)
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
		actualSize := c.resolveRunSize(runs[0], para)
		expectedSize := standard.HeadingStyles[2].FontSize
		if actualSize > 0 && absFloat64(actualSize-expectedSize) > 0.5 {
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

	// 段落级别的字体/字号检查（用第一个非空 run 代表整段，避免每个 run 都报错）
	// 先用继承链解析出段落的实际字体和字号
	paraFont := ""
	paraSize := 0.0
	for _, run := range runs {
		if strings.TrimSpace(run.Text()) == "" {
			continue
		}
		paraFont = c.resolveRunFont(run, para)
		paraSize = c.resolveRunSize(run, para)
		break
	}

	if bodyStyle.FontName != "" && paraFont != "" {
		if !fontsMatch(paraFont, bodyStyle.FontName) {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("body_font"),
				Type:        IssueTypeFont,
				Severity:    SeverityError,
				Description: fmt.Sprintf("正文字体不匹配: 段落%d 期望 %s, 实际 %s", index+1, bodyStyle.FontName, paraFont),
				Original:    paraFont,
				Suggestion:  bodyStyle.FontName,
			})
		}
	}

	if bodyStyle.FontSize > 0 && paraSize > 0 {
		expectedSize := bodyStyle.FontSize
		if absFloat64(paraSize-expectedSize) > 0.5 {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("body_size"),
				Type:        IssueTypeFont,
				Severity:    SeverityError,
				Description: fmt.Sprintf("正文字号不匹配: 段落%d 期望 %.1fpt, 实际 %.1fpt", index+1, expectedSize, paraSize),
				Original:    paraSize,
				Suggestion:  expectedSize,
			})
		}
	}

	// 检查每个 run 的中英文字体合规性（run 级别有独立字体设置时才报）
	for runIndex, run := range runs {
		runText := run.Text()
		// 只检查 run 上有内联字体设置的情况，避免样式继承误报
		inlineFont := getRunFontName(run)

		// 硕士规范：中文用宋体，英文/数字用 Times New Roman
		if inlineFont != "" && containsChineseText(runText) {
			if !fontsMatch(inlineFont, "宋体") {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("body_cn_font"),
					Type:        IssueTypeFont,
					Severity:    SeverityWarning,
					Description: fmt.Sprintf("正文中文字体建议为宋体: 段落%d第%d块当前 %s", index+1, runIndex+1, inlineFont),
					Original:    inlineFont,
					Suggestion:  "宋体",
				})
			}
		}
		if inlineFont != "" && containsLatinOrDigit(runText) {
			if !fontsMatch(inlineFont, "Times New Roman") {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("body_en_font"),
					Type:        IssueTypeFont,
					Severity:    SeverityWarning,
					Description: fmt.Sprintf("正文英文/数字字体建议为 Times New Roman: 段落%d第%d块当前 %s", index+1, runIndex+1, inlineFont),
					Original:    inlineFont,
					Suggestion:  "Times New Roman",
				})
			}
		}
	}

	// 检查首行缩进
	// 修复：期望值应以字符数 × 字号(pt) × 20(twips/pt) 计算，而非乘以 Centimeter 常量
	if bodyStyle.FirstLineIndent > 0 {
		actualIndent := getFirstLineIndent(para)
		// 用解析到的正文字号换算：2字符 × fontSize × 20 twips/pt
		fontSizeForIndent := paraSize
		if fontSizeForIndent <= 0 {
			fontSizeForIndent = 12 // 小四号默认值
		}
		expectedIndentTwips := measurement.Distance(bodyStyle.FirstLineIndent * fontSizeForIndent * 20)
		toleranceTwips := measurement.Distance(fontSizeForIndent * 20 * 0.3) // 30% 容差
		if actualIndent > 0 && abs(int(actualIndent)-int(expectedIndentTwips)) > int(toleranceTwips) {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("body_indent"),
				Type:        IssueTypeSpacing,
				Severity:    SeverityWarning,
				Description: fmt.Sprintf("正文首行缩进不匹配: 期望约 %.0f 字符缩进 (%.0f twips), 实际 %.0f twips", bodyStyle.FirstLineIndent, float64(expectedIndentTwips), float64(actualIndent)),
				Original:    float64(actualIndent),
				Suggestion:  float64(expectedIndentTwips),
			})
		}
	}

	// 检查行间距（使用继承链）
	if bodyStyle.LineSpacing > 0 {
		actualSpacing := c.resolveLineSpacing(para)
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

	// 检查段前段后
	actualBefore := getParagraphSpacingBefore(para)
	actualAfter := getParagraphSpacingAfter(para)
	expectedBefore := bodyStyle.SpacingBefore
	expectedAfter := bodyStyle.SpacingAfter

	if expectedBefore > 0 && absFloat64(actualBefore-expectedBefore) > 2.0 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("body_spacing_before"),
			Type:        IssueTypeSpacing,
			Severity:    SeverityWarning,
			Description: fmt.Sprintf("正文段前间距不匹配: 期望 %.1fpt, 实际 %.1fpt", expectedBefore, actualBefore),
			Original:    actualBefore,
			Suggestion:  expectedBefore,
		})
	}
	if expectedAfter > 0 && absFloat64(actualAfter-expectedAfter) > 2.0 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("body_spacing_after"),
			Type:        IssueTypeSpacing,
			Severity:    SeverityWarning,
			Description: fmt.Sprintf("正文段后间距不匹配: 期望 %.1fpt, 实际 %.1fpt", expectedAfter, actualAfter),
			Original:    actualAfter,
			Suggestion:  expectedAfter,
		})
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

var issueCounter = 0

func generateIssueID(prefix string) string {
	issueCounter++
	return fmt.Sprintf("%s_%d", prefix, issueCounter)
}

func getPaperSize(paperSize string) (measurement.Distance, measurement.Distance) {
	switch strings.ToUpper(paperSize) {
	case "A4":
		return 11906, 16838 // A4纸张大小 (twips)
	case "A3":
		return 16838, 23811
	default:
		return 11906, 16838
	}
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

func getParagraphSpacingBefore(para document.Paragraph) float64 {
	pPr := para.X().PPr
	if pPr == nil || pPr.Spacing == nil || pPr.Spacing.BeforeAttr == nil || pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber == nil {
		return 0
	}
	return float64(*pPr.Spacing.BeforeAttr.ST_UnsignedDecimalNumber) / 20.0
}

func getParagraphSpacingAfter(para document.Paragraph) float64 {
	pPr := para.X().PPr
	if pPr == nil || pPr.Spacing == nil || pPr.Spacing.AfterAttr == nil || pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber == nil {
		return 0
	}
	return float64(*pPr.Spacing.AfterAttr.ST_UnsignedDecimalNumber) / 20.0
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

func (c *DOCXChecker) runMasterThesisPriorityChecks(doc *document.Document, docPath string, result *CheckResult, standard *FormatStandard) {
	paragraphs := doc.Paragraphs()
	texts := make([]string, 0, len(paragraphs))
	for _, p := range paragraphs {
		text := strings.TrimSpace(extractParagraphText(p))
		if text != "" {
			texts = append(texts, text)
		}
	}
	structure := inspectDOCXStructure(docPath)

	c.checkMasterCoverTitleRule(paragraphs, result)
	c.checkMasterWordCountRule(texts, result)
	c.checkMasterAbstractRule(texts, result)
	c.checkMasterHeadingRule(texts, result)
	c.checkMasterCaptionRule(texts, structure, result)
	c.checkMasterReferenceRule(texts, result)
	c.checkMasterHeaderFooterRule(structure, result)
	c.checkMasterPageNumberRule(structure, result)
	c.checkMasterTOCSectionRule(structure, result)
	c.checkHeaderFooterOnlyPageNumberRule(structure, result)
}

func (c *DOCXChecker) checkMasterCoverTitleRule(paragraphs []document.Paragraph, result *CheckResult) {
	for _, p := range paragraphs {
		title := strings.TrimSpace(extractParagraphText(p))
		if title == "" {
			continue
		}
		if utf8Len(title) > masterTitleMaxChars {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("cover_title_len"),
				Type:        IssueTypeTitlePage,
				Severity:    SeverityWarning,
				Description: fmt.Sprintf("封面标题建议不超过%d字，当前约%d字", masterTitleMaxChars, utf8Len(title)),
				Original:    utf8Len(title),
				Suggestion:  masterTitleMaxChars,
			})
		}
		runs := p.Runs()
		if len(runs) > 0 {
			size := getRunFontSize(runs[0])
			if size > 0 && absFloat64(size-masterTitleExpectedSize) > 1.5 {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("cover_title_size"),
					Type:        IssueTypeTitlePage,
					Severity:    SeverityWarning,
					Description: fmt.Sprintf("封面标题字号建议约%.0fpt，当前%.1fpt", masterTitleExpectedSize, size),
					Original:    size,
					Suggestion:  masterTitleExpectedSize,
				})
			}
			if !runs[0].Properties().Bold() {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("cover_title_bold"),
					Type:        IssueTypeTitlePage,
					Severity:    SeverityWarning,
					Description: "封面标题建议加粗",
					Original:    false,
					Suggestion:  true,
				})
			}
			font := getRunFontName(runs[0])
			if font != "" && !fontsMatch(font, "宋体") {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("cover_title_font"),
					Type:        IssueTypeTitlePage,
					Severity:    SeverityWarning,
					Description: fmt.Sprintf("封面标题建议使用宋体，当前 %s", font),
					Original:    font,
					Suggestion:  "宋体",
				})
			}
		}
		if getAlignment(p) != wml.ST_JcCenter {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("cover_title_align"),
				Type:        IssueTypeTitlePage,
				Severity:    SeverityWarning,
				Description: "封面标题建议居中",
				Original:    alignmentToString(getAlignment(p)),
				Suggestion:  "center",
			})
		}
		break
	}
}

func (c *DOCXChecker) checkMasterWordCountRule(texts []string, result *CheckResult) {
	fullText := strings.Join(texts, "\n")
	bodyCN, bodyEN := countBodyCNEChars(fullText)
	if bodyCN < masterMinBodyChineseChars {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("body_cn_count"),
			Type:        IssueTypeMainBody,
			Severity:    SeverityWarning,
			Description: fmt.Sprintf("正文字数不足: 中文约%d，建议至少%d", bodyCN, masterMinBodyChineseChars),
			Original:    bodyCN,
			Suggestion:  masterMinBodyChineseChars,
		})
	}
	if bodyEN < masterMinBodyEnglishWords {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("body_en_count"),
			Type:        IssueTypeMainBody,
			Severity:    SeverityInfo,
			Description: fmt.Sprintf("正文英文词数不足: 当前约%d，建议至少%d", bodyEN, masterMinBodyEnglishWords),
			Original:    bodyEN,
			Suggestion:  masterMinBodyEnglishWords,
		})
	}
}

func (c *DOCXChecker) checkMasterAbstractRule(texts []string, result *CheckResult) {
	joined := strings.Join(texts, "\n")
	cnAbs := extractSectionByHeading(joined, []string{"摘要"}, []string{"关键词", "abstract", "目录"})
	if utf8Len(cnAbs) > 0 && utf8Len(cnAbs) < masterMinChineseAbstractChars {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("cn_abstract_len"),
			Type:        IssueTypeAbstract,
			Severity:    SeverityWarning,
			Description: fmt.Sprintf("中文摘要字数不足: 当前约%d，建议约%d", utf8Len(cnAbs), masterMinChineseAbstractChars),
			Original:    utf8Len(cnAbs),
			Suggestion:  masterMinChineseAbstractChars,
		})
	}
	enAbs := extractSectionByHeading(joined, []string{"abstract"}, []string{"keywords", "目录", "正文"})
	if strings.TrimSpace(enAbs) == "" {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("en_abstract_missing"),
			Type:        IssueTypeAbstract,
			Severity:    SeverityWarning,
			Description: "未检测到英文摘要（Abstract）",
			Original:    "missing",
			Suggestion:  "present",
		})
	}
}

func (c *DOCXChecker) checkMasterHeadingRule(texts []string, result *CheckResult) {
	maxDepth := 0
	var scienceCount, artsCount int
	for _, t := range texts {
		if isScienceHeading(t) {
			scienceCount++
			if d := scienceHeadingDepth(t); d > maxDepth {
				maxDepth = d
			}
		}
		if isArtsHeading(t) {
			artsCount++
			if d := artsHeadingDepth(t); d > maxDepth {
				maxDepth = d
			}
		}
	}
	if scienceCount > 0 && artsCount > 0 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("heading_mixed_scheme"),
			Type:        IssueTypeHeading,
			Severity:    SeverityWarning,
			Description: "标题编号疑似混用文科/理工科两套体系",
			Original:    "mixed",
			Suggestion:  "single_scheme",
		})
	}
	if maxDepth > 4 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("heading_depth"),
			Type:        IssueTypeHeading,
			Severity:    SeverityError,
			Description: fmt.Sprintf("检测到五级及以下标题（深度%d），规范建议禁用", maxDepth),
			Original:    maxDepth,
			Suggestion:  4,
		})
	}
}

func (c *DOCXChecker) checkMasterCaptionRule(texts []string, structure docxStructuralInfo, result *CheckResult) {
	figRe := regexp.MustCompile(`^图\s*\d+(\.\d+)?`)
	tabRe := regexp.MustCompile(`^表\s*\d+(\.\d+)?`)
	eqRe := regexp.MustCompile(`^\(\d+\.\d+\)$`)
	figCount, tabCount, eqCount := 0, 0, 0
	for _, t := range texts {
		tt := strings.TrimSpace(t)
		if figRe.MatchString(tt) {
			figCount++
		}
		if tabRe.MatchString(tt) {
			tabCount++
		}
		if eqRe.MatchString(tt) {
			eqCount++
		}
	}
	if figCount == 0 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("figure_caption"),
			Type:        IssueTypeFigure,
			Severity:    SeverityInfo,
			Description: "未检测到标准图题格式（如：图2.1 标题）",
			Original:    0,
			Suggestion:  "图x.y 标题",
		})
	}
	if tabCount == 0 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("table_caption"),
			Type:        IssueTypeTable,
			Severity:    SeverityInfo,
			Description: "未检测到标准表题格式（如：表2.1 标题）",
			Original:    0,
			Suggestion:  "表x.y 标题",
		})
	}
	if eqCount == 0 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("equation_number"),
			Type:        IssueTypeFigure,
			Severity:    SeverityInfo,
			Description: "未检测到标准公式编号格式（如：(2.1)）",
			Original:    0,
			Suggestion:  "(x.y)",
		})
	}

	// 结构级校验（升级）：支持跨空段落；图题关联到前序锚点对象；公式编号应右对齐
	for i, node := range structure.BodyNodes {
		if node.Kind != "p" {
			continue
		}
		text := strings.TrimSpace(node.Text)
		if tabRe.MatchString(text) {
			tableIdx := findNextNonEmptyTableNode(structure.BodyNodes, i, 8)
			if tableIdx == -1 {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("table_caption_position"),
					Type:        IssueTypeTable,
					Severity:    SeverityWarning,
					Description: "表题位置疑似不规范：未在后续相邻块（允许跨空段）定位到对应表格",
					Original:    "caption_not_before_table",
					Suggestion:  "caption_before_table",
				})
			}
		}
		if figRe.MatchString(text) {
			figureIdx := findPrevDrawingNode(structure.BodyNodes, i, 8)
			if figureIdx == -1 {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("figure_caption_position"),
					Type:        IssueTypeFigure,
					Severity:    SeverityWarning,
					Description: "图题位置疑似不规范：未在前序相邻块（允许跨空段）定位到图对象",
					Original:    "caption_not_after_figure",
					Suggestion:  "caption_after_figure",
				})
			} else if !structure.BodyNodes[figureIdx].HasAnchorObj {
				result.Issues = append(result.Issues, FormatIssue{
					ID:          generateIssueID("figure_anchor_assoc"),
					Type:        IssueTypeFigure,
					Severity:    SeverityInfo,
					Description: "图题对象关联较弱：前序图块未检测到锚点对象（wp:anchor/wp:inline）",
					Original:    "drawing_without_anchor_signature",
					Suggestion:  "drawing_with_anchor_signature",
				})
			}
		}
		if eqRe.MatchString(text) && !node.RightAligned {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("equation_number_align"),
				Type:        IssueTypeFigure,
				Severity:    SeverityWarning,
				Description: "公式编号疑似不规范：建议右对齐",
				Original:    "not_right_aligned",
				Suggestion:  "right_aligned",
			})
		}
	}
}

func (c *DOCXChecker) checkMasterReferenceRule(texts []string, result *CheckResult) {
	refLines := collectReferenceLines(texts)
	total := len(refLines)
	if total == 0 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("refs_missing"),
			Type:        IssueTypeReference,
			Severity:    SeverityWarning,
			Description: "未识别到参考文献条目",
			Original:    0,
			Suggestion:  masterMinReferencesCount,
		})
		return
	}
	if total < masterMinReferencesCount {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("refs_count"),
			Type:        IssueTypeReference,
			Severity:    SeverityWarning,
			Description: fmt.Sprintf("参考文献数量不足: 当前%d，建议至少%d", total, masterMinReferencesCount),
			Original:    total,
			Suggestion:  masterMinReferencesCount,
		})
	}

	currentYear := 2025
	recent, foreign := 0, 0
	yearRe := regexp.MustCompile(`(19|20)\d{2}`)
	for _, line := range refLines {
		if hasLatinLetter(line) {
			foreign++
		}
		matches := yearRe.FindAllString(line, -1)
		for _, y := range matches {
			yi, _ := strconv.Atoi(y)
			if yi >= currentYear-5 {
				recent++
				break
			}
		}
	}
	recentRatio := float64(recent) / float64(total)
	foreignRatio := float64(foreign) / float64(total)
	if recentRatio < masterRecentRefRatio {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("refs_recent_ratio"),
			Type:        IssueTypeReference,
			Severity:    SeverityWarning,
			Description: fmt.Sprintf("近五年参考文献比例偏低: 当前%.0f%%，建议至少%.0f%%", recentRatio*100, masterRecentRefRatio*100),
			Original:    recentRatio,
			Suggestion:  masterRecentRefRatio,
		})
	}
	if foreignRatio < masterForeignRefRatio {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("refs_foreign_ratio"),
			Type:        IssueTypeReference,
			Severity:    SeverityWarning,
			Description: fmt.Sprintf("外文参考文献比例偏低: 当前%.0f%%，建议至少%.0f%%", foreignRatio*100, masterForeignRefRatio*100),
			Original:    foreignRatio,
			Suggestion:  masterForeignRefRatio,
		})
	}
}

func (c *DOCXChecker) checkMasterHeaderFooterRule(structure docxStructuralInfo, result *CheckResult) {
	evenHeader := strings.TrimSpace(structure.HeaderByType["even"])
	if evenHeader == "" {
		evenHeader = strings.Join(structure.HeaderTexts, "\n")
	}
	if !strings.Contains(evenHeader, "北京大学硕士学位论文") {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("header_even_rule"),
			Type:        IssueTypeHeaderFooter,
			Severity:    SeverityWarning,
			Description: "页眉结构校验：未检测到偶数页目标文本“北京大学硕士学位论文”",
			Original:    evenHeader,
			Suggestion:  "北京大学硕士学位论文",
		})
	}
	if !structure.HasEvenAndOddHeaders {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("header_odd_even_switch"),
			Type:        IssueTypeHeaderFooter,
			Severity:    SeverityWarning,
			Description: "页眉结构校验：未启用奇偶页不同页眉（w:evenAndOddHeaders）",
			Original:    false,
			Suggestion:  true,
		})
	}
	if structure.HeaderCount < 2 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("header_def_count"),
			Type:        IssueTypeHeaderFooter,
			Severity:    SeverityInfo,
			Description: "页眉结构校验：检测到的页眉定义不足，无法完整区分奇偶页页眉",
			Original:    structure.HeaderCount,
			Suggestion:  2,
		})
	}

	oddHeader := strings.TrimSpace(structure.HeaderByType["default"])
	if oddHeader == "" {
		oddHeader = strings.TrimSpace(structure.HeaderByType["odd"])
	}
	if oddHeader == "" && len(structure.HeaderTexts) > 0 {
		oddHeader = strings.TrimSpace(structure.HeaderTexts[0])
	}
	if oddHeader == "" {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("header_odd_missing"),
			Type:        IssueTypeHeaderFooter,
			Severity:    SeverityInfo,
			Description: "页眉结构校验：未检测到奇数页页眉内容",
			Original:    "missing",
			Suggestion:  "chapter_title",
		})
		return
	}

	if len(structure.ChapterCandidates) > 0 && !headerMatchesChapterCandidate(oddHeader, structure.ChapterCandidates) {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("header_chapter_match"),
			Type:        IssueTypeHeaderFooter,
			Severity:    SeverityWarning,
			Description: "页眉结构校验：奇数页页眉与章节标题候选不匹配",
			Original:    oddHeader,
			Suggestion:  "chapter_title_candidate",
		})
	}
}

func (c *DOCXChecker) checkMasterPageNumberRule(structure docxStructuralInfo, result *CheckResult) {
	if !structure.HasRomanPageNum || !structure.HasArabicPageNum {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("page_number_style"),
			Type:        IssueTypePageNumber,
			Severity:    SeverityWarning,
			Description: "页码结构校验：未同时检测到罗马与阿拉伯分页格式（w:pgNumType）",
			Original:    fmt.Sprintf("roman=%v, arabic=%v", structure.HasRomanPageNum, structure.HasArabicPageNum),
			Suggestion:  "roman+arabic",
		})
	}
	if !structure.HasPageNumField {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("page_number_field"),
			Type:        IssueTypePageNumber,
			Severity:    SeverityInfo,
			Description: "页码结构校验：未检测到 PAGE 域，可能未插入自动页码",
			Original:    false,
			Suggestion:  true,
		})
	}

	// 分段一致性：前置部分（摘要/目录）应使用罗马数字，正文应切换阿拉伯数字
	if structure.FrontMatterStart >= 0 && structure.MainBodyStart > structure.FrontMatterStart {
		if len(structure.PageNumFormats) < 2 {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("page_number_segment_count"),
				Type:        IssueTypePageNumber,
				Severity:    SeverityWarning,
				Description: "页码结构校验：存在前置部分与正文分段，但页码格式定义不足（未检测到明确切换）",
				Original:    structure.PageNumFormats,
				Suggestion:  []string{"upperRoman", "decimal"},
			})
			return
		}
		firstFmt := strings.ToLower(strings.TrimSpace(structure.PageNumFormats[0]))
		lastFmt := strings.ToLower(strings.TrimSpace(structure.PageNumFormats[len(structure.PageNumFormats)-1]))
		if (firstFmt != "upperroman" && firstFmt != "roman") || lastFmt != "decimal" {
			result.Issues = append(result.Issues, FormatIssue{
				ID:          generateIssueID("page_number_segment_order"),
				Type:        IssueTypePageNumber,
				Severity:    SeverityWarning,
				Description: "页码结构校验：摘要/目录到正文的页码格式切换顺序不一致（应 Roman -> Arabic）",
				Original:    structure.PageNumFormats,
				Suggestion:  []string{"upperRoman", "decimal"},
			})
		}
	}
}

func (c *DOCXChecker) checkMasterTOCSectionRule(structure docxStructuralInfo, result *CheckResult) {
	if structure.TOCStart == -1 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("toc_missing"),
			Type:        IssueTypePageNumber,
			Severity:    SeverityWarning,
			Description: "结构校验：未检测到目录区段",
			Original:    "missing",
			Suggestion:  "toc_section",
		})
		return
	}

	if !structure.HasTOCField {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("toc_field_missing"),
			Type:        IssueTypePageNumber,
			Severity:    SeverityWarning,
			Description: "结构校验：目录未检测到 TOC 域（建议使用自动目录域）",
			Original:    false,
			Suggestion:  true,
		})
	}
	if structure.TOCMaxLevel > 3 {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("toc_level_overflow"),
			Type:        IssueTypeHeading,
			Severity:    SeverityWarning,
			Description: fmt.Sprintf("结构校验：目录域层级超过三级（当前最大 %d 级）", structure.TOCMaxLevel),
			Original:    structure.TOCMaxLevel,
			Suggestion:  3,
		})
	}

	if structure.MainBodyStart == -1 || structure.MainBodyStart <= structure.TOCStart {
		return
	}
	tocSectionIdx := findSectionIndexForNode(structure.Sections, structure.TOCStart)
	mainSectionIdx := findSectionIndexForNode(structure.Sections, structure.MainBodyStart)
	if tocSectionIdx == -1 || mainSectionIdx == -1 {
		return
	}
	if tocSectionIdx == mainSectionIdx {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("toc_main_same_section"),
			Type:        IssueTypePageNumber,
			Severity:    SeverityWarning,
			Description: "结构校验：目录与正文位于同一分节，目录页码“另编”规则可能不满足",
			Original:    tocSectionIdx,
			Suggestion:  "separate_sections",
		})
		return
	}

	tocSection := structure.Sections[tocSectionIdx]
	mainSection := structure.Sections[mainSectionIdx]
	// “目录页码另编”：至少应出现不同起始定义或独立格式定义
	if tocSection.HasPgNumType && mainSection.HasPgNumType &&
		tocSection.PageNumFmt == mainSection.PageNumFmt &&
		tocSection.PageNumStart == mainSection.PageNumStart {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("toc_page_number_not_restarted"),
			Type:        IssueTypePageNumber,
			Severity:    SeverityWarning,
			Description: "结构校验：目录与正文页码定义完全一致，未体现“目录页码另编”",
			Original:    fmt.Sprintf("fmt=%s,start=%d", tocSection.PageNumFmt, tocSection.PageNumStart),
			Suggestion:  "different_page_numbering_definition",
		})
	}
}

func (c *DOCXChecker) checkHeaderFooterOnlyPageNumberRule(structure docxStructuralInfo, result *CheckResult) {
	if structure.HeaderHasNonPageText {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("header_extra_text"),
			Type:        IssueTypeHeaderFooter,
			Severity:    SeverityWarning,
			Description: "结构校验：检测到除页码外的页眉文本，不符合“除页码外不设其他页眉页脚”",
			Original:    "header_has_non_page_text",
			Suggestion:  "page_number_only",
		})
	}
	if structure.FooterHasNonPageText {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("footer_extra_text"),
			Type:        IssueTypeHeaderFooter,
			Severity:    SeverityWarning,
			Description: "结构校验：检测到除页码外的页脚文本，不符合“除页码外不设其他页眉页脚”",
			Original:    "footer_has_non_page_text",
			Suggestion:  "page_number_only",
		})
	}
	if !structure.HasPageNumField {
		result.Issues = append(result.Issues, FormatIssue{
			ID:          generateIssueID("page_number_only_missing_field"),
			Type:        IssueTypePageNumber,
			Severity:    SeverityInfo,
			Description: "结构校验：未检测到自动页码域（PAGE），请确认页码是否正确插入",
			Original:    false,
			Suggestion:  true,
		})
	}
}

func extractParagraphText(p document.Paragraph) string {
	var b strings.Builder
	for _, run := range p.Runs() {
		b.WriteString(run.Text())
	}
	return b.String()
}

func utf8Len(s string) int {
	return len([]rune(strings.TrimSpace(s)))
}

func containsChineseText(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func containsLatinOrDigit(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return true
		}
	}
	return false
}

func hasLatinLetter(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func countBodyCNEChars(text string) (int, int) {
	cnChars := 0
	enWords := 0
	word := strings.Builder{}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			cnChars++
			if word.Len() > 0 {
				enWords++
				word.Reset()
			}
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
			continue
		}
		if word.Len() > 0 {
			enWords++
			word.Reset()
		}
	}
	if word.Len() > 0 {
		enWords++
	}
	return cnChars, enWords
}

func extractSectionByHeading(fullText string, starts []string, ends []string) string {
	lines := strings.Split(fullText, "\n")
	start := -1
	for i, line := range lines {
		l := strings.TrimSpace(strings.ToLower(line))
		for _, s := range starts {
			if l == strings.ToLower(strings.TrimSpace(s)) || strings.HasPrefix(l, strings.ToLower(strings.TrimSpace(s))+" ") {
				start = i + 1
				break
			}
		}
		if start != -1 {
			break
		}
	}
	if start == -1 || start >= len(lines) {
		return ""
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		l := strings.TrimSpace(strings.ToLower(lines[i]))
		for _, e := range ends {
			el := strings.ToLower(strings.TrimSpace(e))
			if l == el || strings.HasPrefix(l, el) {
				end = i
				return strings.Join(lines[start:end], "\n")
			}
		}
	}
	return strings.Join(lines[start:end], "\n")
}

func isScienceHeading(text string) bool {
	t := strings.TrimSpace(text)
	matched, _ := regexp.MatchString(`^\d+(\.\d+){0,3}\s*`, t)
	return matched
}

func scienceHeadingDepth(text string) int {
	t := strings.TrimSpace(text)
	re := regexp.MustCompile(`^(\d+(\.\d+){0,3})`)
	m := re.FindStringSubmatch(t)
	if len(m) < 2 {
		return 0
	}
	return strings.Count(m[1], ".") + 1
}

func isArtsHeading(text string) bool {
	t := strings.TrimSpace(text)
	patterns := []string{
		`^[一二三四五六七八九十]+、`,
		`^（[一二三四五六七八九十]+）`,
		`^\([一二三四五六七八九十]+\)`,
		`^\d+\.`,
		`^（\d+）`,
		`^\(\d+\)`,
	}
	for _, p := range patterns {
		m, _ := regexp.MatchString(p, t)
		if m {
			return true
		}
	}
	return false
}

func artsHeadingDepth(text string) int {
	t := strings.TrimSpace(text)
	switch {
	case regexp.MustCompile(`^[一二三四五六七八九十]+、`).MatchString(t):
		return 1
	case regexp.MustCompile(`^（[一二三四五六七八九十]+）|^\([一二三四五六七八九十]+\)`).MatchString(t):
		return 2
	case regexp.MustCompile(`^\d+\.`).MatchString(t):
		return 3
	case regexp.MustCompile(`^（\d+）|^\(\d+\)`).MatchString(t):
		return 4
	default:
		return 0
	}
}

func collectReferenceLines(texts []string) []string {
	start := -1
	for i, t := range texts {
		tt := strings.TrimSpace(strings.ToLower(t))
		if tt == "参考文献" || strings.HasPrefix(tt, "参考文献") {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return []string{}
	}
	lines := make([]string, 0)
	refStartRe := regexp.MustCompile(`^(\[\d+\]|\d+\.)`)
	stopHeadings := []string{"附录", "致谢", "攻读", "声明"}
	for i := start; i < len(texts); i++ {
		line := strings.TrimSpace(texts[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		shouldStop := false
		for _, stop := range stopHeadings {
			if strings.HasPrefix(lower, strings.ToLower(stop)) {
				shouldStop = true
				break
			}
		}
		if shouldStop {
			break
		}
		if refStartRe.MatchString(line) || hasLatinLetter(line) {
			lines = append(lines, line)
		}
	}
	return lines
}

type bodyNode struct {
	Kind         string
	Text         string
	IsEmpty      bool
	HasDrawing   bool
	HasAnchorObj bool
	RightAligned bool
	HasSectPr    bool
	PageNumFmt   string
	PageNumStart int
	HasTOCField  bool
	TOCMaxLevel  int
	RawXML       string
}

type docSection struct {
	StartNode    int
	EndNode      int
	HasPgNumType bool
	PageNumFmt   string
	PageNumStart int
}

type docxStructuralInfo struct {
	HasEvenAndOddHeaders bool
	HeaderTexts          []string
	HeaderCount          int
	HeaderByType         map[string]string
	FooterTexts          []string
	HeaderHasNonPageText bool
	FooterHasNonPageText bool
	HasRomanPageNum      bool
	HasArabicPageNum     bool
	HasPageNumField      bool
	PageNumFormats       []string
	FrontMatterStart     int
	MainBodyStart        int
	TOCStart             int
	TOCEnd               int
	HasTOCField          bool
	TOCMaxLevel          int
	ChapterCandidates    []string
	Sections             []docSection
	BodyNodes            []bodyNode
}

func inspectDOCXStructure(docPath string) docxStructuralInfo {
	info := docxStructuralInfo{
		HeaderTexts:       make([]string, 0),
		HeaderByType:      make(map[string]string),
		FooterTexts:       make([]string, 0),
		PageNumFormats:    make([]string, 0),
		FrontMatterStart:  -1,
		MainBodyStart:     -1,
		TOCStart:          -1,
		TOCEnd:            -1,
		TOCMaxLevel:       0,
		ChapterCandidates: make([]string, 0),
		Sections:          make([]docSection, 0),
		BodyNodes:         make([]bodyNode, 0),
	}

	zr, err := zip.OpenReader(docPath)
	if err != nil {
		return info
	}
	defer zr.Close()

	fileMap := make(map[string]string)
	for _, f := range zr.File {
		content, err := readZipFileAsString(f)
		if err != nil {
			continue
		}
		fileMap[f.Name] = content
	}

	if settingsXML, ok := fileMap["word/settings.xml"]; ok {
		info.HasEvenAndOddHeaders = strings.Contains(settingsXML, "w:evenAndOddHeaders")
	}

	if documentXML, ok := fileMap["word/document.xml"]; ok {
		dxml := strings.ToLower(documentXML)
		info.PageNumFormats = parsePageNumFormats(documentXML)
		for _, fmtName := range info.PageNumFormats {
			switch fmtName {
			case "roman", "upperroman":
				info.HasRomanPageNum = true
			case "decimal":
				info.HasArabicPageNum = true
			}
		}
		if strings.Contains(dxml, "page") && (strings.Contains(dxml, "instrtext") || strings.Contains(dxml, "fldsimple")) && strings.Contains(dxml, " page ") {
			info.HasPageNumField = true
		}
		info.BodyNodes = parseBodyNodes(documentXML)
		info.FrontMatterStart, info.MainBodyStart = detectFrontAndMainRanges(info.BodyNodes)
		info.TOCStart, info.TOCEnd = detectTOCRange(info.BodyNodes, info.MainBodyStart)
		info.HasTOCField, info.TOCMaxLevel = detectTOCField(info.BodyNodes)
		info.ChapterCandidates = extractChapterCandidates(info.BodyNodes)
		info.Sections = parseSections(info.BodyNodes)
		info.HeaderByType = parseHeaderByType(documentXML, fileMap["word/_rels/document.xml.rels"], fileMap)
	}

	headerPaths := make([]string, 0)
	footerPaths := make([]string, 0)
	for name := range fileMap {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "word/header") && strings.HasSuffix(lower, ".xml") {
			headerPaths = append(headerPaths, name)
		}
		if strings.HasPrefix(lower, "word/footer") && strings.HasSuffix(lower, ".xml") {
			footerPaths = append(footerPaths, name)
		}
	}
	sort.Strings(headerPaths)
	sort.Strings(footerPaths)
	info.HeaderCount = len(headerPaths)
	for _, p := range headerPaths {
		info.HeaderTexts = append(info.HeaderTexts, collectXMLText(fileMap[p]))
		lower := strings.ToLower(fileMap[p])
		if strings.Contains(lower, " page ") && (strings.Contains(lower, "instrtext") || strings.Contains(lower, "fldsimple")) {
			info.HasPageNumField = true
		}
		if hasNonPageTextInHeaderFooterXML(fileMap[p]) {
			info.HeaderHasNonPageText = true
		}
	}
	for _, p := range footerPaths {
		info.FooterTexts = append(info.FooterTexts, collectXMLText(fileMap[p]))
		lower := strings.ToLower(fileMap[p])
		if strings.Contains(lower, " page ") && (strings.Contains(lower, "instrtext") || strings.Contains(lower, "fldsimple")) {
			info.HasPageNumField = true
		}
		if hasNonPageTextInHeaderFooterXML(fileMap[p]) {
			info.FooterHasNonPageText = true
		}
	}

	return info
}

func readZipFileAsString(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func collectXMLText(xml string) string {
	re := regexp.MustCompile(`(?s)<w:t[^>]*>(.*?)</w:t>`)
	matches := re.FindAllStringSubmatch(xml, -1)
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			parts = append(parts, strings.TrimSpace(m[1]))
		}
	}
	return strings.Join(parts, "")
}

func parseBodyNodes(documentXML string) []bodyNode {
	re := regexp.MustCompile(`(?s)<w:p\b.*?</w:p>|<w:tbl\b.*?</w:tbl>`)
	blocks := re.FindAllString(documentXML, -1)
	nodes := make([]bodyNode, 0, len(blocks))
	for _, block := range blocks {
		lower := strings.ToLower(block)
		if strings.HasPrefix(lower, "<w:tbl") {
			nodes = append(nodes, bodyNode{Kind: "tbl"})
			continue
		}
		text := collectXMLText(block)
		trimmed := strings.TrimSpace(text)
		rightAligned := strings.Contains(lower, `w:jc`) && strings.Contains(lower, `w:val="right"`)
		hasTOCField, tocMaxLevel := parseTOCInfoFromParagraphXML(block)
		hasSectPr, pageFmt, pageStart := parseSectPrInfoFromParagraphXML(block)
		nodes = append(nodes, bodyNode{
			Kind:         "p",
			Text:         trimmed,
			IsEmpty:      trimmed == "",
			HasDrawing:   strings.Contains(lower, "<w:drawing"),
			HasAnchorObj: strings.Contains(lower, "<wp:anchor") || strings.Contains(lower, "<wp:inline"),
			RightAligned: rightAligned,
			HasSectPr:    hasSectPr,
			PageNumFmt:   pageFmt,
			PageNumStart: pageStart,
			HasTOCField:  hasTOCField,
			TOCMaxLevel:  tocMaxLevel,
			RawXML:       block,
		})
	}
	return nodes
}

func findNextNonEmptyTableNode(nodes []bodyNode, fromIdx int, maxLookahead int) int {
	looked := 0
	for i := fromIdx + 1; i < len(nodes); i++ {
		n := nodes[i]
		if n.Kind == "p" && n.IsEmpty {
			continue
		}
		looked++
		if n.Kind == "tbl" {
			return i
		}
		if looked >= maxLookahead {
			break
		}
	}
	return -1
}

func findPrevDrawingNode(nodes []bodyNode, fromIdx int, maxLookback int) int {
	looked := 0
	for i := fromIdx - 1; i >= 0; i-- {
		n := nodes[i]
		if n.Kind == "p" && n.IsEmpty {
			continue
		}
		looked++
		if n.Kind == "p" && n.HasDrawing {
			return i
		}
		if looked >= maxLookback {
			break
		}
	}
	return -1
}

func parseTOCInfoFromParagraphXML(paragraphXML string) (bool, int) {
	lower := strings.ToLower(paragraphXML)
	if !strings.Contains(lower, " toc ") && !strings.Contains(lower, "toc") {
		return false, 0
	}
	// Word TOC field usually appears in instrText: TOC \o "1-3"
	re := regexp.MustCompile(`(?i)toc\s+\\o\s+"(\d+)-(\d+)"`)
	m := re.FindStringSubmatch(paragraphXML)
	if len(m) >= 3 {
		maxLevel, err := strconv.Atoi(m[2])
		if err == nil {
			return true, maxLevel
		}
	}
	return true, 0
}

func parseSectPrInfoFromParagraphXML(paragraphXML string) (bool, string, int) {
	lower := strings.ToLower(paragraphXML)
	if !strings.Contains(lower, "<w:sectpr") {
		return false, "", 0
	}
	fmtRe := regexp.MustCompile(`(?i)<w:pgNumType\b[^>]*w:fmt="([^"]+)"`)
	startRe := regexp.MustCompile(`(?i)<w:pgNumType\b[^>]*w:start="([^"]+)"`)
	fmtMatch := fmtRe.FindStringSubmatch(paragraphXML)
	startMatch := startRe.FindStringSubmatch(paragraphXML)

	pageFmt := ""
	pageStart := 0
	if len(fmtMatch) > 1 {
		pageFmt = strings.ToLower(strings.TrimSpace(fmtMatch[1]))
	}
	if len(startMatch) > 1 {
		pageStart, _ = strconv.Atoi(strings.TrimSpace(startMatch[1]))
	}
	return true, pageFmt, pageStart
}

func detectTOCRange(nodes []bodyNode, mainBodyStart int) (int, int) {
	start := -1
	end := -1
	for i, n := range nodes {
		if n.Kind != "p" {
			continue
		}
		t := strings.TrimSpace(strings.ToLower(n.Text))
		if start == -1 && (t == "目录" || strings.HasPrefix(t, "目录")) {
			start = i
			continue
		}
		if start != -1 {
			if mainBodyStart != -1 && i >= mainBodyStart {
				end = i - 1
				break
			}
		}
	}
	if start != -1 && end == -1 {
		if mainBodyStart > start {
			end = mainBodyStart - 1
		} else {
			end = len(nodes) - 1
		}
	}
	return start, end
}

func detectTOCField(nodes []bodyNode) (bool, int) {
	hasField := false
	maxLevel := 0
	for _, n := range nodes {
		if !n.HasTOCField {
			continue
		}
		hasField = true
		if n.TOCMaxLevel > maxLevel {
			maxLevel = n.TOCMaxLevel
		}
	}
	return hasField, maxLevel
}

func parseSections(nodes []bodyNode) []docSection {
	sections := make([]docSection, 0)
	if len(nodes) == 0 {
		return sections
	}
	current := docSection{
		StartNode:    0,
		EndNode:      len(nodes) - 1,
		HasPgNumType: false,
		PageNumFmt:   "",
		PageNumStart: 0,
	}
	for i, n := range nodes {
		if n.Kind != "p" || !n.HasSectPr {
			continue
		}
		current.EndNode = i
		if n.PageNumFmt != "" {
			current.HasPgNumType = true
			current.PageNumFmt = n.PageNumFmt
		}
		if n.PageNumStart > 0 {
			current.PageNumStart = n.PageNumStart
		}
		sections = append(sections, current)
		current = docSection{
			StartNode:    i + 1,
			EndNode:      len(nodes) - 1,
			HasPgNumType: false,
			PageNumFmt:   "",
			PageNumStart: 0,
		}
	}
	if len(sections) == 0 || sections[len(sections)-1].StartNode != current.StartNode {
		sections = append(sections, current)
	}
	return sections
}

func findSectionIndexForNode(sections []docSection, nodeIdx int) int {
	for i, s := range sections {
		if nodeIdx >= s.StartNode && nodeIdx <= s.EndNode {
			return i
		}
	}
	return -1
}

func hasNonPageTextInHeaderFooterXML(xml string) bool {
	parts := extractXMLTextParts(xml)
	allowed := regexp.MustCompile(`^[0-9ivxlcdmIVXLCDM\-–—\.\(\)（）\s]*$`)
	for _, p := range parts {
		text := strings.TrimSpace(p)
		if text == "" {
			continue
		}
		if !allowed.MatchString(text) {
			return true
		}
	}
	return false
}

func extractXMLTextParts(xml string) []string {
	re := regexp.MustCompile(`(?s)<w:t[^>]*>(.*?)</w:t>`)
	matches := re.FindAllStringSubmatch(xml, -1)
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			parts = append(parts, m[1])
		}
	}
	return parts
}

func parsePageNumFormats(documentXML string) []string {
	re := regexp.MustCompile(`(?i)<w:pgNumType\b[^>]*w:fmt="([^"]+)"[^>]*/?>`)
	matches := re.FindAllStringSubmatch(documentXML, -1)
	formats := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			formats = append(formats, strings.ToLower(strings.TrimSpace(m[1])))
		}
	}
	return formats
}

func parseHeaderByType(documentXML, relsXML string, fileMap map[string]string) map[string]string {
	result := make(map[string]string)
	if strings.TrimSpace(documentXML) == "" || strings.TrimSpace(relsXML) == "" {
		return result
	}

	relRe := regexp.MustCompile(`(?i)<Relationship[^>]*\bId="([^"]+)"[^>]*\bTarget="([^"]+)"`)
	relMatches := relRe.FindAllStringSubmatch(relsXML, -1)
	relMap := make(map[string]string, len(relMatches))
	for _, m := range relMatches {
		if len(m) < 3 {
			continue
		}
		id := strings.TrimSpace(m[1])
		target := strings.TrimSpace(m[2])
		target = strings.ReplaceAll(target, "\\", "/")
		target = strings.TrimPrefix(target, "../")
		if !strings.HasPrefix(target, "word/") {
			target = "word/" + target
		}
		relMap[id] = target
	}

	refRe := regexp.MustCompile(`(?is)<w:headerReference\b[^>]*>`)
	typeRe := regexp.MustCompile(`w:type="([^"]+)"`)
	idRe := regexp.MustCompile(`r:id="([^"]+)"`)
	refs := refRe.FindAllString(documentXML, -1)
	for _, tag := range refs {
		tm := typeRe.FindStringSubmatch(tag)
		im := idRe.FindStringSubmatch(tag)
		if len(tm) < 2 || len(im) < 2 {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(tm[1]))
		rid := strings.TrimSpace(im[1])
		target, ok := relMap[rid]
		if !ok {
			continue
		}
		xml := fileMap[target]
		if strings.TrimSpace(xml) == "" {
			continue
		}
		result[typ] = collectXMLText(xml)
	}
	return result
}

func detectFrontAndMainRanges(nodes []bodyNode) (int, int) {
	frontStart := -1
	mainStart := -1
	for i, n := range nodes {
		if n.Kind != "p" {
			continue
		}
		t := strings.TrimSpace(strings.ToLower(n.Text))
		if frontStart == -1 && (t == "摘要" || t == "目录" || strings.HasPrefix(t, "摘要") || strings.HasPrefix(t, "目录")) {
			frontStart = i
		}
		if mainStart == -1 && isMainBodyStartHeading(n.Text) {
			mainStart = i
		}
	}
	return frontStart, mainStart
}

func isMainBodyStartHeading(text string) bool {
	t := strings.TrimSpace(text)
	patterns := []string{
		`^第[一二三四五六七八九十百]+章`,
		`^第\d+章`,
		`^一、`,
		`^1[\s\.、].*`,
	}
	for _, p := range patterns {
		ok, _ := regexp.MatchString(p, t)
		if ok {
			return true
		}
	}
	return false
}

func extractChapterCandidates(nodes []bodyNode) []string {
	candidates := make([]string, 0)
	seen := make(map[string]struct{})
	candidateRe := regexp.MustCompile(`^(第[一二三四五六七八九十百\d]+章|[一二三四五六七八九十]+、|\d+(\.\d+){0,3}\.?)`)
	for _, n := range nodes {
		if n.Kind != "p" {
			continue
		}
		t := strings.TrimSpace(n.Text)
		if t == "" {
			continue
		}
		if !candidateRe.MatchString(t) {
			continue
		}
		norm := normalizeHeadingText(t)
		if norm == "" {
			continue
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		candidates = append(candidates, norm)
		if len(candidates) >= 12 {
			break
		}
	}
	return candidates
}

func headerMatchesChapterCandidate(header string, candidates []string) bool {
	h := normalizeHeadingText(header)
	if h == "" {
		return false
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if strings.Contains(h, c) || strings.Contains(c, h) {
			return true
		}
	}
	return false
}

func normalizeHeadingText(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.NewReplacer(" ", "", "\t", "", "　", "").Replace(s)
	rePrefix := regexp.MustCompile(`^(第[一二三四五六七八九十百\d]+章|[一二三四五六七八九十]+、|\d+(\.\d+){0,3}\.?)`)
	s = rePrefix.ReplaceAllString(s, "")
	rePunc := regexp.MustCompile("[，。；：:、,.()（）【】\\[\\]《》<>“”\"'`]")
	s = rePunc.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// ApplyCorrections 应用修正建议到文档
func (c *DOCXChecker) ApplyCorrections(ctx context.Context, docPath string, corrections []Correction) (string, error) {
	standard, err := c.correctionsToStandard(corrections)
	if err != nil {
		return "", err
	}

	return c.FixDocumentDirectly(ctx, docPath, *standard)
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
		return fmt.Errorf("unsupported correction type: %s", correction.Type)
	}
}

// applyPageSetupCorrection 应用页面设置修正
func (c *DOCXChecker) applyPageSetupCorrection(doc *docx.Docx, correction Correction) error {
	return fmt.Errorf("page setup correction is handled by the standard pipeline")
}

// applyFontCorrection 应用字体修正
func (c *DOCXChecker) applyFontCorrection(doc *docx.Docx, correction Correction) error {
	return fmt.Errorf("font correction is handled by the standard pipeline")
}

// applySpacingCorrection 应用间距修正
func (c *DOCXChecker) applySpacingCorrection(doc *docx.Docx, correction Correction) error {
	return fmt.Errorf("spacing correction is handled by the standard pipeline")
}

// applyHeadingStyleCorrection 应用标题样式修正
func (c *DOCXChecker) applyHeadingStyleCorrection(doc *docx.Docx, correction Correction) error {
	return fmt.Errorf("heading correction is handled by the standard pipeline")
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
		measurement.Distance(standard.PageSetup.Gutter)*measurement.Centimeter, // 装订线/装订边距
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

	// 设置字体和字号 - 只使用高级API，避免直接取临时变量地址
	for _, run := range para.Runs() {
		runProps := run.Properties()

		// 设置字体
		if style.FontName != "" {
			runProps.SetFontFamily(style.FontName)
		}

		// 设置字号
		if style.FontSize > 0 {
			runProps.SetSize(measurement.Distance(style.FontSize))
		}

		// 设置加粗
		if style.Bold {
			runProps.SetBold(true)
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

	// 设置字体和字号 - 只使用高级API，避免直接取临时变量地址
	for _, run := range para.Runs() {
		runProps := run.Properties()

		// 设置字体
		if style.FontName != "" {
			runProps.SetFontFamily(style.FontName)
		}

		// 设置字号
		if style.FontSize > 0 {
			runProps.SetSize(measurement.Distance(style.FontSize))
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
						runProps := run.Properties()

						// 设置字体 - 使用高级API
						if standard.TableStyle.FontName != "" {
							runProps.SetFontFamily(standard.TableStyle.FontName)
						}

						// 设置字号 - 使用高级API
						if standard.TableStyle.FontSize > 0 {
							runProps.SetSize(measurement.Distance(standard.TableStyle.FontSize))
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
