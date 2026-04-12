package handler

import (
	"regexp"
	"strconv"
	"strings"
)

// isSpecialFormat 检查是否是特殊格式
func (h *PaperHandler) isSpecialFormat(text string) bool {
	// 检查是否包含特定的关键字
	keywords := []string{"中文摘要", "英文摘要", "关键词", "目录", "主体部分", "标题序号与格式"}
	matchCount := 0
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			matchCount++
		}
	}
	// 如果匹配到超过一半的关键字，则认为是特殊格式
	return matchCount >= len(keywords)/2
}

// parseSpecialFormat 解析特殊格式
func (h *PaperHandler) parseSpecialFormat(text string, format *ParsedFormatRequirements) {
	// 解析摘要部分
	h.parseAbstractSection(text, format)

	// 解析目录部分
	h.parseDirectorySection(text, format)

	// 解析主体部分
	h.parseMainBodySection(text, format)

	// 解析标题格式
	h.parseTitleFormat(text, format)

	// 解析字体要求
	h.parseFontRequirements(text, format)
}

// parseAbstractSection 解析摘要部分
func (h *PaperHandler) parseAbstractSection(text string, format *ParsedFormatRequirements) {
	// 查找中文摘要部分
	cnAbstractRegex := regexp.MustCompile(`中文摘要[：:]([^。]+)`)

	cnMatches := cnAbstractRegex.FindStringSubmatch(text)

	if len(cnMatches) > 1 {
		// 提取摘要要求

		abstractReq := cnMatches[1]
		if strings.Contains(abstractReq, "300汉字") {
			format.BasicRequirements = append(format.BasicRequirements, "中文摘要约300汉字")
		}
		if strings.Contains(abstractReq, "第三人称") {
			format.BasicRequirements = append(format.BasicRequirements, "中文摘要以第三人称陈述")
		}
		if strings.Contains(abstractReq, "目的") && strings.Contains(abstractReq, "方法") &&
			strings.Contains(abstractReq, "结果") && strings.Contains(abstractReq, "结论") {
			format.BasicRequirements = append(format.BasicRequirements, "中文摘要包含目的、方法、结果、结论")
		}
		if strings.Contains(abstractReq, "重点在结果和结论") {
			format.BasicRequirements = append(format.BasicRequirements, "中文摘要重点在结果和结论")
		}
	}

	// 查找英文摘要部分
	if strings.Contains(text, "英文摘要") {
		format.BasicRequirements = append(format.BasicRequirements, "包含英文摘要")
	}

	// 查找关键词部分
	keywordRegex := regexp.MustCompile(`关键词[：:]([^。]+)`)
	keywordMatches := keywordRegex.FindStringSubmatch(text)
	if len(keywordMatches) > 1 {
		keywordsReq := keywordMatches[1]
		if strings.Contains(keywordsReq, "3-5个") {
			format.BasicRequirements = append(format.BasicRequirements, "关键词3-5个")
		}
		if strings.Contains(keywordsReq, "左对齐") {
			format.BasicRequirements = append(format.BasicRequirements, "关键词左对齐")
		}
		if strings.Contains(keywordsReq, "中英文关键词间用分号分隔") {
			format.BasicRequirements = append(format.BasicRequirements, "中英文关键词间用分号分隔")
		}
	}

}

// parseDirectorySection 解析目录部分
func (h *PaperHandler) parseDirectorySection(text string, format *ParsedFormatRequirements) {
	if strings.Contains(text, "目录") && strings.Contains(text, "另起一页") {
		format.Structure.FrontMatter.TableOfContents = true
		format.BasicRequirements = append(format.BasicRequirements, "目录另起一页，排在摘要之后")
	}

	if strings.Contains(text, "包含章、节、条、附录的序号、名称和页码") {
		format.BasicRequirements = append(format.BasicRequirements, "目录包含章、节、条、附录的序号、名称和页码")
	}

	if strings.Contains(text, "目录层次为2-4级") {
		format.BasicRequirements = append(format.BasicRequirements, "目录层次为2-4级")
	}

	if strings.Contains(text, "下级依次右缩进两个字符") {
		format.BasicRequirements = append(format.BasicRequirements, "下级目录依次右缩进两个字符")
	}

	if strings.Contains(text, "小四号宋体") {
		format.FontSettings.DirectoryFont.FontName = "宋体"
		format.FontSettings.DirectoryFont.FontSize = 12.0 // 小四号
	}
}

// parseMainBodySection 解析主体部分
func (h *PaperHandler) parseMainBodySection(text string, format *ParsedFormatRequirements) {
	if strings.Contains(text, "主体部分") && strings.Contains(text, "必须从右页") {
		format.BasicRequirements = append(format.BasicRequirements, "主体部分必须从右页（奇数页）开始")
	}

	if strings.Contains(text, "一级标题（章）之间应换页") {
		format.BasicRequirements = append(format.BasicRequirements, "一级标题（章）之间应换页")
	}

	// 标记主体部分内容结构
	format.Structure.MainBody.Introduction = true
	format.Structure.MainBody.MainContent = true
	format.Structure.MainBody.Conclusion = true
}

// parseTitleFormat 解析标题格式
func (h *PaperHandler) parseTitleFormat(text string, format *ParsedFormatRequirements) {
	if strings.Contains(text, "标题序号与格式") {
		format.BasicRequirements = append(format.BasicRequirements, "遵循标题序号与格式要求")
	}

	// 理工类格式
	if strings.Contains(text, "理工类") {
		format.BasicRequirements = append(format.BasicRequirements, "使用理工类标题序号格式: 1 → 1.1 → 1.1.1 → ① → 1） → a．")
	}

	// 文科类格式
	if strings.Contains(text, "文科类") {
		format.BasicRequirements = append(format.BasicRequirements, "使用文科类标题序号格式: 一 → (一） → 1. → (1) → 第一")
	}
}

// parseFontRequirements 解析字体要求
func (h *PaperHandler) parseFontRequirements(text string, format *ParsedFormatRequirements) {
	// 一级标题（章）
	if strings.Contains(text, "一级标题（章）") && strings.Contains(text, "三号黑体") && strings.Contains(text, "居中") {
		format.FontSettings.TitleFont.ChapterTitle.FontName = "黑体"
		format.FontSettings.TitleFont.ChapterTitle.FontSize = 16.0 // 三号
		format.FontSettings.TitleFont.ChapterTitle.Alignment = "center"
	}

	// 二级标题（节）
	if strings.Contains(text, "二级标题（节）") && strings.Contains(text, "小三号黑体") && strings.Contains(text, "居左") {
		format.FontSettings.TitleFont.SectionTitle.FontName = "黑体"
		format.FontSettings.TitleFont.SectionTitle.FontSize = 15.0 // 小三号
		format.FontSettings.TitleFont.SectionTitle.Alignment = "left"
	}

	// 三级标题（条）
	if strings.Contains(text, "三级标题（条）") && strings.Contains(text, "四号黑体") && strings.Contains(text, "居左") && strings.Contains(text, "右缩进两字") {
		format.FontSettings.TitleFont.SubsectionTitle.FontName = "黑体"
		format.FontSettings.TitleFont.SubsectionTitle.FontSize = 14.0 // 四号
		format.FontSettings.TitleFont.SubsectionTitle.Alignment = "left"
	}

	// 更下级标题
	if strings.Contains(text, "更下级标题") && strings.Contains(text, "与正文同大小宋体") && strings.Contains(text, "右缩进两字") {
		format.BasicRequirements = append(format.BasicRequirements, "更下级标题与正文同大小宋体，右缩进两字")
	}
}

// cleanText 清理文本，去除多余空格和换行符
func (h *PaperHandler) cleanText(text string) string {
	// 替换多个连续的空白字符为单个空格
	re := regexp.MustCompile(`\s+`)
	cleaned := re.ReplaceAllString(text, " ")
	return strings.TrimSpace(cleaned)
}

// extractInstitution 提取学校名称
func (h *PaperHandler) extractInstitution(text string, format *ParsedFormatRequirements) {
	// 使用更智能的方式匹配学校名称，结合分词结果
	institutionRegex := regexp.MustCompile(`([\x{4e00}-\x{9fa5}]+大学|[^\s]+学院)`)
	matches := institutionRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		format.Institution = matches[1]
	} else {
		// 如果正则表达式没有匹配到，尝试从分词结果中查找
		institutionKeywords := []string{"大学", "学院"}
		words := strings.Split(text, " ")
		for i, word := range words {
			for _, keyword := range institutionKeywords {
				if strings.Contains(word, keyword) && i > 0 {
					// 找到可能的学校名称
					format.Institution = words[i-1] + word
					return
				}
			}
		}
	}
}

// extractBasicRequirements 提取基本要求
func (h *PaperHandler) extractBasicRequirements(text string, format *ParsedFormatRequirements) {
	// 查找基本要求部分
	basicReqRegex := regexp.MustCompile(`一、基本要求\\s*([\\s\\S]*?)\\s*二、`)
	matches := basicReqRegex.FindStringSubmatch(text)

	if len(matches) > 1 {
		// 分割成要点
		points := strings.Split(matches[1], "\n")
		for _, point := range points {
			trimmed := strings.TrimSpace(point)
			if trimmed != "" && !strings.HasPrefix(trimmed, "一、") {
				format.BasicRequirements = append(format.BasicRequirements, trimmed)
			}
		}
	} else {
		// 如果正则表达式没有匹配到，尝试从分词结果中查找基本要求
		words := strings.Split(text, " ")
		startIndex := -1
		endIndex := -1
		for i, word := range words {
			if strings.Contains(word, "基本要求") {
				startIndex = i
			} else if startIndex != -1 && strings.Contains(word, "要求") && i > startIndex {
				endIndex = i
				break
			}
		}

		//fmt.Println(111)
		//fmt.Println(words)
		//fmt.Println(222)

		if startIndex != -1 && endIndex != -1 {
			// 提取基本要求内容
			for i := startIndex; i < endIndex; i++ {
				if len(words[i]) > 3 { // 过滤掉太短的词语
					format.BasicRequirements = append(format.BasicRequirements, words[i])
				}
			}
		}
	}
}

// extractPageSetup 提取页面设置
func (h *PaperHandler) extractPageSetup(text string, format *ParsedFormatRequirements) {
	// 设置默认值

	format.PageSetup.Orientation = "portrait"

	// 提取纸张尺寸
	pattern := `([A-Za-z0-9]+)\(([0-9.]+)[×x]([0-9.]+)([a-z]+)\)`
	re := regexp.MustCompile(pattern)

	if re.MatchString(text) {
		matches := re.FindStringSubmatch(text)
		if len(matches) >= 5 {
			paperType := matches[1] // A4
			width := matches[2]     // 21
			height := matches[3]    // 29.7
			unit := matches[4]      // cm
			format.PageSetup.PaperSize = paperType + width + height + unit
		}
	} else {
		format.PageSetup.PaperSize = "A4"

	}

	// 提取页边距
	result := make(map[string]float64)

	// 先找到页边距设置的整个段落
	marginSectionPattern := `页边距设置[：:]\s*([^。\n]+(?:。|$))`
	marginSectionRe := regexp.MustCompile(marginSectionPattern)
	marginSectionMatch := marginSectionRe.FindStringSubmatch(text)

	if marginSectionMatch == nil || len(marginSectionMatch) < 2 {
		// 如果没有找到页边距设置，使用默认值
		format.PageSetup.Margins.Top = 2.5
		format.PageSetup.Margins.Right = 2.5
		format.PageSetup.Margins.Left = 2.5
		format.PageSetup.Margins.Bottom = 2.5
	} else {
		// 如果找到了页边距设置，解析具体的值
		marginText := marginSectionMatch[1]

		// 处理"均为"的情况
		if strings.Contains(marginText, "均为") {
			commonPattern := `均为\s*(\d+(?:\.\d+)?)\s*(?:厘米|cm)`
			commonRe := regexp.MustCompile(commonPattern)
			commonMatch := commonRe.FindStringSubmatch(marginText)

			if commonMatch != nil && len(commonMatch) >= 2 {
				value, _ := strconv.ParseFloat(commonMatch[1], 64)

				// 设置四个方向的值
				result["上"] = value
				result["下"] = value
				result["左"] = value
				result["右"] = value

				format.PageSetup.Margins.Top = result["上"]
				format.PageSetup.Margins.Right = result["下"]
				format.PageSetup.Margins.Left = result["左"]
				format.PageSetup.Margins.Bottom = result["右"]
			}
		}

		// 分别提取各个方向
		directionPattern := `([上下左右])[^，、]*?(\d+(?:\.\d+)?)\s*(?:厘米|cm)`
		directionRe := regexp.MustCompile(directionPattern)
		directionMatches := directionRe.FindAllStringSubmatch(marginText, -1)

		for _, match := range directionMatches {
			if len(match) >= 3 {
				direction := match[1]
				value, _ := strconv.ParseFloat(match[2], 64)

				result[direction] = value
			}
		}

		// 验证是否四个方向都有值
		marginRegex := regexp.MustCompile(`([上下左右])[^，。]*?(\d+(?:\.\d+)?)\s*(?:厘米|cm)`)
		marginMatches := marginRegex.FindAllStringSubmatch(text, -1)
		for _, match := range marginMatches {
			if len(match) > 2 {
				marginValue, err := strconv.ParseFloat(match[2], 64)
				if err == nil {
					switch match[1] {
					case "上":
						format.PageSetup.Margins.Top = marginValue
					case "下":
						format.PageSetup.Margins.Bottom = marginValue
					case "左":
						format.PageSetup.Margins.Left = marginValue
					case "右":
						format.PageSetup.Margins.Right = marginValue
					}
				}
			}
		}

		// 如果没有匹配到具体的页边距，设置默认值
		if format.PageSetup.Margins.Top == 0 && format.PageSetup.Margins.Bottom == 0 &&
			format.PageSetup.Margins.Left == 0 && format.PageSetup.Margins.Right == 0 {
			format.PageSetup.Margins.Top = 2.5
			format.PageSetup.Margins.Bottom = 2.5
			format.PageSetup.Margins.Left = 2.5
			format.PageSetup.Margins.Right = 2.5
		}
	}

	// 提取页眉页脚高度
	headerRegex := regexp.MustCompile(`页眉[：:][^，。]*?(\d+(?:\.\d+)?)\s*(?:厘米|cm)`)
	headerMatch := headerRegex.FindStringSubmatch(text)

	if len(headerMatch) > 1 {
		headerHeight, err := strconv.ParseFloat(headerMatch[1], 64)
		if err == nil {
			format.PageSetup.HeaderFooter.HeaderHeight = headerHeight
		}
	}

	footerRegex := regexp.MustCompile(`页脚[：:](\d+(?:\.\d+)?)\s*(?:厘米|cm)`)
	footerMatch := footerRegex.FindStringSubmatch(text)
	if len(footerMatch) > 1 {
		footerHeight, err := strconv.ParseFloat(footerMatch[1], 64)
		if err == nil {
			format.PageSetup.HeaderFooter.FooterHeight = footerHeight
		}
	}

	// 提取页眉内容和格式
	if strings.Contains(text, "单面印制") {
		// 单面印制的页眉设置
		if strings.Contains(text, "左对齐") && strings.Contains(text, "重庆工程学院本科生毕业设计（论文）") {
			format.PageSetup.HeaderFooter.HeaderLeft = "重庆工程学院本科生毕业设计（论文）"
		}
		if strings.Contains(text, "右对齐") && strings.Contains(text, "各章章名") {
			format.PageSetup.HeaderFooter.HeaderRight = "各章章名"
		}
	}

	if strings.Contains(text, "双面印制") || strings.Contains(text, "双面打印") {
		// 双面印制的页眉设置
		if strings.Contains(text, "左页居中") && strings.Contains(text, "重庆工程学院本科生毕业设计（论文）") {
			format.PageSetup.HeaderFooter.HeaderCenter = "重庆工程学院本科生毕业设计（论文）"
		}
		if strings.Contains(text, "右页居中") && strings.Contains(text, "各章章名") {
			// 这里可以根据需要设置其他属性来表示右页居中章名
		}
	}

	// 提取页眉字体
	if strings.Contains(text, "页眉字号为5号宋体") {
		format.FontSettings.TitleFont.ChapterTitle.FontName = "宋体"
		format.FontSettings.TitleFont.ChapterTitle.FontSize = 10.5 // 5号字
	}

	// 提取打印方式规则
	if strings.Contains(text, "50页以上") && strings.Contains(text, "双面打印") {
		format.BasicRequirements = append(format.BasicRequirements, "总页数50页以上必须双面打印")
		format.PageSetup.PrintingSide = "double"
	}

	if strings.Contains(text, "50页以下") && strings.Contains(text, "单面打印") {
		format.BasicRequirements = append(format.BasicRequirements, "总页数50页以下单面打印即可")
		if format.PageSetup.PrintingSide == "" {
			format.PageSetup.PrintingSide = "single"
		}
	}

	// 提取页码编排规则
	if strings.Contains(text, "主体部分") && strings.Contains(text, "引言或绪论") {
		format.BasicRequirements = append(format.BasicRequirements, "主体部分从引言或绪论开始用阿拉伯数字连续编页")
	}

	if strings.Contains(text, "主体之前部分") && strings.Contains(text, "中文摘要") && strings.Contains(text, "英文摘要") && strings.Contains(text, "目录") {
		format.BasicRequirements = append(format.BasicRequirements, "主体之前部分（中文摘要、英文摘要、目录）用罗马数字单独编页")
	}

	// 如果没有明确指定打印方式，默认设置为双面打印
	if format.PageSetup.PrintingSide == "" {
		format.PageSetup.PrintingSide = "double"
	}
}

// extractFontSettings 提取字体设置
func (h *PaperHandler) extractFontSettings(text string, format *ParsedFormatRequirements) {
	// 查找字体与间距部分
	fontRegex := regexp.MustCompile(`\(2\)\\s*字体与间距[\\s\\S]*?(?:字体为|用)([^，。]*)`)
	matches := fontRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		fontText := matches[1]

		// 提取正文字体
		if strings.Contains(fontText, "宋体") {
			format.FontSettings.MainFont.FontName = "宋体"
		}

		// 提取字号
		fontSizeRegex := regexp.MustCompile(`(?:(小四号|四号|小三号|三号|小二号|二号|小一号|一号)|(\\d+(?:\\.\\d+)?)\\s*(?:号|pt|磅))`)
		fontSizeMatch := fontSizeRegex.FindStringSubmatch(fontText)
		if len(fontSizeMatch) > 1 {
			if fontSizeMatch[1] != "" {
				// 中文字号转换
				switch fontSizeMatch[1] {
				case "小四号":
					format.FontSettings.MainFont.FontSize = 12.0
				case "四号":
					format.FontSettings.MainFont.FontSize = 14.0
				case "小三号":
					format.FontSettings.MainFont.FontSize = 15.0
				case "三号":
					format.FontSettings.MainFont.FontSize = 16.0
				case "小二号":
					format.FontSettings.MainFont.FontSize = 18.0
				case "二号":
					format.FontSettings.MainFont.FontSize = 22.0
				case "小一号":
					format.FontSettings.MainFont.FontSize = 24.0
				case "一号":
					format.FontSettings.MainFont.FontSize = 26.0
				}
			} else if fontSizeMatch[2] != "" {
				// 数字字号
				fontSize, err := strconv.ParseFloat(fontSizeMatch[2], 64)
				if err == nil {
					format.FontSettings.MainFont.FontSize = fontSize
				}
			}
		}

		// 提取行间距
		lineSpaceRegex := regexp.MustCompile(`(?:行间距|行距)[：:](\\d+(?:\\.\\d+)?)\\s*(?:磅|pt)`)
		lineSpaceMatch := lineSpaceRegex.FindStringSubmatch(fontText)
		if len(lineSpaceMatch) > 1 {
			lineSpace, err := strconv.ParseFloat(lineSpaceMatch[1], 64)
			if err == nil {
				format.FontSettings.MainFont.LineSpacing = lineSpace
			}
		}
	}
}

// extractDocumentStructure 提取文档结构要求
func (h *PaperHandler) extractDocumentStructure(text string, format *ParsedFormatRequirements) {
	// 查找前置部分要求
	frontMatterRegex := regexp.MustCompile(`\(1\)\\s*前置部分\\s*([\\s\\S]*?)\\s*\(?2\)?`)
	frontMatches := frontMatterRegex.FindStringSubmatch(text)
	if len(frontMatches) > 1 {
		frontText := frontMatches[1]

		if strings.Contains(frontText, "封面") {
			format.Structure.FrontMatter.CoverPage = true
		}
		if strings.Contains(frontText, "原创性声明") || strings.Contains(frontText, "版权声明") {
			format.Structure.FrontMatter.CopyrightStatement = true
		}
		if strings.Contains(frontText, "摘要") {
			format.Structure.FrontMatter.Abstract = true
		}
		if strings.Contains(frontText, "目次页") || strings.Contains(frontText, "目录") {
			format.Structure.FrontMatter.TableOfContents = true
		}
		if strings.Contains(frontText, "插图") && strings.Contains(frontText, "清单") {
			format.Structure.FrontMatter.ListOfFigures = true
		}
		if strings.Contains(frontText, "表格") && strings.Contains(frontText, "清单") {
			format.Structure.FrontMatter.ListOfTables = true
		}
	}

	// 查找主体部分要求
	mainBodyRegex := regexp.MustCompile(`\(2\)\\s*主体部分\\s*([\\s\\S]*?)\\s*\(?3\)?`)
	mainMatches := mainBodyRegex.FindStringSubmatch(text)
	if len(mainMatches) > 1 {
		mainText := mainMatches[1]

		if strings.Contains(mainText, "引言") || strings.Contains(mainText, "绪论") {
			format.Structure.MainBody.Introduction = true
		}
		if strings.Contains(mainText, "正文") {
			format.Structure.MainBody.MainContent = true
		}
		if strings.Contains(mainText, "结论") {
			format.Structure.MainBody.Conclusion = true
		}
	}

	// 查找后置部分要求
	// 修复正则表达式，使其正确匹配参考文献部分
	backMatterRegex := regexp.MustCompile(`(?:\(3\)|5\))\\s*参考文献`)
	backMatches := backMatterRegex.FindStringSubmatch(text)
	if len(backMatches) > 0 {
		format.Structure.BackMatter.References = true
	}

	if strings.Contains(text, "致谢") {
		format.Structure.BackMatter.Acknowledgements = true
	}

	if strings.Contains(text, "附录") {
		format.Structure.BackMatter.Appendices = true
	}
}

// extractCitationRules 提取引用规则
func (h *PaperHandler) extractCitationRules(text string, format *ParsedFormatRequirements) {
	// 查找参考文献部分
	citationRegex := regexp.MustCompile(`\(3\)\\s*参考文献\\s*([\\s\\S]*?)\\s*\(?4\)?`)
	matches := citationRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		citationText := matches[1]

		// 提取参考文献格式
		formatRegex := regexp.MustCompile(`(?:格式为|格式：|格式[：:])([^。]*)`)
		formatMatches := formatRegex.FindStringSubmatch(citationText)
		if len(formatMatches) > 1 {
			format.CitationRules.ReferenceFormat = strings.TrimSpace(formatMatches[1])
		}

		// 提取常见的文献类型
		refTypes := []string{"专著", "期刊", "论文集", "学位论文", "报告", "标准", "专利", "报纸", "电子公告"}
		for _, refType := range refTypes {
			if strings.Contains(citationText, refType) {
				// 简化处理，实际应该提取完整的标识符
				format.CitationRules.ReferenceTypes = append(format.CitationRules.ReferenceTypes, refType)
			}
		}
	}
}

// extractAppendixRules 提取附录规则
func (h *PaperHandler) extractAppendixRules(text string, format *ParsedFormatRequirements) {
	// 查找附录部分
	appendixRegex := regexp.MustCompile(`\(4\)\\s*附录\\s*([\\s\\S]*?)\\s*$`)
	matches := appendixRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		appendixText := matches[1]

		// 提取附录格式要求
		formatRegex := regexp.MustCompile(`(?:格式为|格式：|格式[：:])([^。]*)`)
		formatMatches := formatRegex.FindStringSubmatch(appendixText)
		if len(formatMatches) > 1 {
			format.AppendixRules.AppendixFormat = strings.TrimSpace(formatMatches[1])
		}

		// 提取附件列表
		attachmentRegex := regexp.MustCompile(`(?:包括|包含)([^。]*)`)
		attachmentMatches := attachmentRegex.FindStringSubmatch(appendixText)
		if len(attachmentMatches) > 1 {
			attachments := strings.Split(attachmentMatches[1], "、")
			for _, attachment := range attachments {
				trimmed := strings.TrimSpace(attachment)
				if trimmed != "" {
					format.AppendixRules.AttachmentList = append(format.AppendixRules.AttachmentList, trimmed)
				}
			}
		}
	}
}

// ParseDetailedFormatText 公共方法用于解析详细的格式文本内容并提取格式要求
func (h *PaperHandler) ParseDetailedFormatText(formatText string) *ParsedFormatRequirements {
	return h.parseDetailedFormatText(formatText)
}

// parseDetailedFormatText 解析详细的格式文本内容并提取格式要求
func (h *PaperHandler) parseDetailedFormatText(formatText string) *ParsedFormatRequirements {
	// 初始化默认格式要求
	format := &ParsedFormatRequirements{
		Institution:       "重庆工程学院",     //学校名称
		DocumentType:      "本科毕业设计（论文）", // 文档类型
		BasicRequirements: []string{},   // 存储各种基本格式要求的字符串数组
		PageSetup: PageSetup{ // 页面设置
			PaperSize:   "A4",       // 纸张大小
			Orientation: "portrait", // 页面方向（纵向）
			Margins: Margins{ // 页边距设置
				Top:    2.5, // 上边距2.5厘米
				Bottom: 2.5, // 下边距2.5厘米
				Left:   2.5, // 左边距2.5厘米
				Right:  2.5, // 右边距2.5厘米
			},

			HeaderFooter: HeaderFooter{ // 页眉页脚设置
				HeaderHeight: 1.6,                 // 页眉高度1.6厘米
				FooterHeight: 2.1,                 // 页脚高度2.1厘米
				HeaderLeft:   "重庆工程学院本科生毕业设计（论文）", // 页眉左侧内容
				HeaderRight:  "",                  // 页眉右侧内容
				HeaderCenter: "",                  // 页眉居中内容
			},
			PrintingSide: "single", // 打印面（单面打印）
		},
		FontSettings: FontSettings{ // 字体设置
			MainFont: MainFont{ // 正文字体
				FontName:    "宋体", // 字体名称
				FontSize:    12.0, // 字体大小（小四号）
				LineSpacing: 20.0, // 20磅  // 行间距20磅
			},
			TitleFont: TitleFont{ // 标题字体设置
				ChapterTitle: ChapterTitle{ // 章标题
					FontName:  "黑体",     // 黑体
					FontSize:  16.0,     // 三号
					Alignment: "center", // 居中对齐
				},
				SectionTitle: SectionTitle{ // 节标题
					FontName:  "黑体",   // 黑体
					FontSize:  15.0,   // 小三号
					Alignment: "left", // 左对齐
				},
				SubsectionTitle: SubsectionTitle{ // 小节标题
					FontName:  "黑体",   // 黑体
					FontSize:  14.0,   // 四号
					Alignment: "left", // 左对齐
				},
			},
			AbstractFont: AbstractFont{ // 摘要字体
				FontName: "宋体", // 字体名称
				FontSize: 12.0, // 小四号
			},
			DirectoryFont: DirectoryFont{ // 目录字体
				FontName: "宋体", // 字体名称
				FontSize: 12.0, // 小四号
			},
			TableFont: TableFont{ // 表格字体
				FontName: "宋体", // 字体名称
				FontSize: 10.5, // 五号
			},
			FigureFont: FigureFont{ // 图片说明字体
				FontName: "宋体", // 字体名称
				FontSize: 10.5, // 五号
			},
		},
		Structure: DocumentStructure{ // 文档结构
			FrontMatter: FrontMatter{ // 前置部分
				CoverPage:          true, // 需要封面
				CopyrightStatement: true, // 需要版权声明
				Abstract:           true, // 需要摘要
				TableOfContents:    true, // 需要目录
				ListOfFigures:      true, // 需要插图清单
				ListOfTables:       true, // 需要表格清单
			},
			MainBody: MainBody{ // 主体部分
				Introduction: true, // 需要引言
				MainContent:  true, // 需要正文
				Conclusion:   true, // 需要结论
			},
			BackMatter: BackMatter{ // 后置部分
				References:       true, // 需要参考文献
				Acknowledgements: true, // 需要致谢
				Appendices:       true, // 需要附录
			},
		},
		CitationRules: CitationRules{ // 引用规则
			ReferenceFormat: "[序号] 作者.题名[文献类型标识].出版地:出版者,出版年.",                                                                  // 参考文献格式
			ReferenceTypes:  []string{"专著(M)", "期刊(J)", "论文集(C)", "学位论文(D)", "报告(R)", "标准(S)", "专利(P)", "报纸(N)", "电子公告(EB/OL)"}, // 支持的文献类型
		},
		AppendixRules: AppendixRules{ // 附录规则
			AppendixFormat: "附录+字母编号",                                // 附录格式
			AttachmentList: []string{"任务书", "开题报告", "相关图纸", "光盘等资料"}, // 需要提交的附件列表
		},
	}

	// 检查是否是您提供的特定格式
	//if h.isSpecialFormat(formatText) {
	//	// 使用特殊格式解析函数
	//	h.parseSpecialFormat(formatText, format)
	//} else {
	// 使用更智能的文本处理方式来提取格式要求
	// 清理文本，去除多余空格和换行符

	cleanText := h.cleanText(formatText)

	// 从清理后的文本中提取学校名称
	h.extractInstitution(cleanText, format)

	// 提取基本要求
	h.extractBasicRequirements(cleanText, format)

	// 提取页面设置
	h.extractPageSetup(cleanText, format)

	// 提取字体设置
	h.extractFontSettings(cleanText, format)

	// 提取文档结构要求
	h.extractDocumentStructure(cleanText, format)

	// 提取引用规则
	h.extractCitationRules(cleanText, format)

	// 提取附录规则
	h.extractAppendixRules(cleanText, format)

	return format
}
