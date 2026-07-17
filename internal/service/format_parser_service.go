package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// FormatParserService 格式解析服务
type formatAIClient interface {
	ChatCompletion(prompt string) (string, error)
}

type FormatParserService struct {
	aiClient   formatAIClient
	maxAICalls int
}

// NewFormatParserService 创建格式解析服务
func NewFormatParserService() *FormatParserService {
	return &FormatParserService{maxAICalls: 20}
}

// SetMaxAICalls limits DeepSeek calls made while parsing one document.
func (s *FormatParserService) SetMaxAICalls(max int) {
	if max > 0 {
		s.maxAICalls = max
	}
}

// ParseFormatFromText 从文本中解析格式规范
func (s *FormatParserService) ParseFormatFromText(text string) (string, error) {
	rules := make(map[string]interface{})

	// 解析各个部分的格式
	rules["title"] = s.extractTitleFormat(text)
	rules["author"] = s.extractAuthorFormat(text)
	rules["abstract"] = s.extractAbstractFormat(text)
	rules["keywords"] = s.extractKeywordsFormat(text)
	rules["body"] = s.extractBodyFormat(text)
	rules["headings"] = s.extractHeadingsFormat(text)
	rules["references"] = s.extractReferencesFormat(text)
	rules["page_setup"] = s.extractPageSetup(text)
	rules["english_title"] = s.extractEnglishTitleFormat(text)
	rules["english_abstract"] = s.extractEnglishAbstractFormat(text)

	// 额外解析封面格式
	rules["cover"] = s.extractCoverFormat(text)
	// 额外解析目录格式
	rules["table_of_contents"] = s.extractTableOfContentsFormat(text)
	// 额外解析致谢格式
	rules["acknowledgements"] = s.extractAcknowledgementsFormat(text)
	// 额外解析附录格式
	rules["appendix"] = s.extractAppendixFormat(text)

	// 转换为JSON
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return "", err
	}

	return string(rulesJSON), nil
}

// extractCoverFormat 提取封面格式
func (s *FormatParserService) extractCoverFormat(text string) map[string]interface{} {
	format := map[string]interface{}{
		"description": "按给定模板填写",
	}

	// 查找封面格式描述
	coverPattern := regexp.MustCompile(`封面[：:\s]+([^\n\r]+)`)
	if matches := coverPattern.FindStringSubmatch(text); len(matches) > 1 {
		coverDesc := matches[1]
		coverFormat := s.parseFormatDescription(coverDesc)

		// 合并格式
		for key, value := range coverFormat {
			format[key] = value
		}
	}

	return format
}

// extractTableOfContentsFormat 提取目录格式
func (s *FormatParserService) extractTableOfContentsFormat(text string) map[string]interface{} {
	format := map[string]interface{}{
		"title": map[string]interface{}{
			"text":      "目录",
			"font_name": "黑体",
			"font_size": "三号",
			"alignment": "center",
			"bold":      true,
		},
		"content": map[string]interface{}{
			"font_name":  "宋体",
			"font_size":  "小四号",
			"line_space": "1.5",
			"alignment":  "left",
			"max_level":  3, // 层次至三级标题
		},
	}

	// 查找目录格式描述
	tocPattern := regexp.MustCompile(`目录[：:\s]+([^\n\r]+)`)
	if matches := tocPattern.FindStringSubmatch(text); len(matches) > 1 {
		tocDesc := matches[1]

		// 分析目录格式描述
		if strings.Contains(tocDesc, "目录") && strings.Contains(tocDesc, "居中") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["alignment"] = "center"
			}
		}
		if strings.Contains(tocDesc, "黑体") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["font_name"] = "黑体"
			}
		}
		if strings.Contains(tocDesc, "三号") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["font_size"] = "三号"
			}
		}
		if strings.Contains(tocDesc, "宋体") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["font_name"] = "宋体"
			}
		}
		if strings.Contains(tocDesc, "小四") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["font_size"] = "小四号"
			}
		}
		if strings.Contains(tocDesc, "1.5倍行距") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["line_space"] = "1.5"
			}
		}
	}

	return format
}

// extractAcknowledgementsFormat 提取致谢格式
func (s *FormatParserService) extractAcknowledgementsFormat(text string) map[string]interface{} {
	format := map[string]interface{}{
		"title": map[string]interface{}{
			"text":      "致谢",
			"font_name": "宋体",
			"font_size": "小四号",
			"bold":      true,
		},
		"content": map[string]interface{}{
			"font_name": "宋体",
			"font_size": "五号",
		},
	}

	// 查找致谢格式描述
	thanksPattern := regexp.MustCompile(`致谢[：:\s]+([^\n\r]+)`)
	if matches := thanksPattern.FindStringSubmatch(text); len(matches) > 1 {
		thanksDesc := matches[1]

		// 分析致谢格式描述
		if strings.Contains(thanksDesc, "小四") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["font_size"] = "小四号"
			}
		}
		if strings.Contains(thanksDesc, "宋体") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["font_name"] = "宋体"
			}
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["font_name"] = "宋体"
			}
		}
		if strings.Contains(thanksDesc, "五号") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["font_size"] = "五号"
			}
		}
		if strings.Contains(thanksDesc, "加粗") || strings.Contains(thanksDesc, "粗体") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["bold"] = true
			}
		}
	}

	return format
}

// extractAppendixFormat 提取附录格式
func (s *FormatParserService) extractAppendixFormat(text string) map[string]interface{} {
	format := map[string]interface{}{
		"title": map[string]interface{}{
			"text":      "附录",
			"font_name": "黑体",
			"font_size": "小二号",
			"alignment": "left",
		},
		"content": map[string]interface{}{
			"font_name": "黑体",
			"font_size": "小三号",
			"alignment": "center",
		},
	}

	// 查找附录格式描述
	appendixPattern := regexp.MustCompile(`附录[：:\s]+([^\n\r]+)`)
	if matches := appendixPattern.FindStringSubmatch(text); len(matches) > 1 {
		appendixDesc := matches[1]

		// 分析附录格式描述
		if strings.Contains(appendixDesc, "黑体") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["font_name"] = "黑体"
			}
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["font_name"] = "黑体"
			}
		}
		if strings.Contains(appendixDesc, "小二号") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["font_size"] = "小二号"
			}
		}
		if strings.Contains(appendixDesc, "小三") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["font_size"] = "小三号"
			}
		}
		if strings.Contains(appendixDesc, "顶格") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["alignment"] = "left"
			}
		}
		if strings.Contains(appendixDesc, "居中") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["alignment"] = "center"
			}
		}
	}

	return format
}

// extractTitleFormat 提取标题格式
func (s *FormatParserService) extractTitleFormat(text string) map[string]interface{} {
	format := map[string]interface{}{
		"alignment":  "center",
		"font_name":  "黑体",
		"font_size":  "三号",
		"bold":       true,
		"line_space": "single",
	}

	// 查找标题格式描述
	titlePattern := regexp.MustCompile(`(?:中文题目|题目|标题|中文标题)[：:\s]+([^\n\r]+)`)
	if matches := titlePattern.FindStringSubmatch(text); len(matches) > 1 {
		formatDesc := matches[1]
		format = s.parseFormatDescription(formatDesc)
	}

	// 也查找中文标题格式描述
	chineseTitlePattern := regexp.MustCompile(`中文标题[：:\s]+([^\n\r]+)`)
	if matches := chineseTitlePattern.FindStringSubmatch(text); len(matches) > 1 {
		formatDesc := matches[1]
		titleFormat := s.parseFormatDescription(formatDesc)

		// 合并格式
		for key, value := range titleFormat {
			format[key] = value
		}
	}

	return format
}

// extractAuthorFormat 提取作者格式
func (s *FormatParserService) extractAuthorFormat(text string) map[string]interface{} {
	format := map[string]interface{}{
		"alignment": "center",
		"font_name": "宋体",
		"font_size": "小四号",
		"bold":      false,
	}

	// 查找作者格式描述
	authorPattern := regexp.MustCompile(`(?:中文姓名|姓名|作者)[：:\s]+([^\n\r]+)`)
	if matches := authorPattern.FindStringSubmatch(text); len(matches) > 1 {
		formatDesc := matches[1]
		format = s.parseFormatDescription(formatDesc)
	}

	// 也查找作者信息格式描述
	authorInfoPattern := regexp.MustCompile(`作者信息[：:\s]+([^\n\r]+)`)
	if matches := authorInfoPattern.FindStringSubmatch(text); len(matches) > 1 {
		formatDesc := matches[1]

		// 分析作者信息格式
		if strings.Contains(formatDesc, "姓名居中") {
			// 姓名居中部分
			namePattern := regexp.MustCompile(`姓名居中([\s\S]+?)；`)
			if nameMatches := namePattern.FindStringSubmatch(formatDesc); len(nameMatches) > 1 {
				nameFormat := s.parseFormatDescription(nameMatches[1])
				for key, value := range nameFormat {
					format[key] = value
				}
				format["alignment"] = "center"
			}

			// 学院信息部分
			collegePattern := regexp.MustCompile(`学院信息([\s\S]+)`)
			if collegeMatches := collegePattern.FindStringSubmatch(formatDesc); len(collegeMatches) > 1 {
				collegeFormat := s.parseFormatDescription(collegeMatches[1])
				// 为学院信息创建单独的格式结构
				format["college_info"] = collegeFormat
			}
		}
	}

	return format
}

// extractAbstractFormat 提取摘要格式
func (s *FormatParserService) extractAbstractFormat(text string) map[string]interface{} {
	format := map[string]interface{}{
		"label": map[string]interface{}{
			"text":      "摘要：",
			"font_name": "仿宋",
			"font_size": "五号",
			"bold":      true,
		},
		"content": map[string]interface{}{
			"font_name":  "仿宋",
			"font_size":  "五号",
			"alignment":  "justify",
			"line_space": "1.5",
		},
	}

	// 查找摘要格式描述
	abstractPattern := regexp.MustCompile(`(?:中文摘要|摘要)[：:\s]+([^\n\r]+)`)
	if matches := abstractPattern.FindStringSubmatch(text); len(matches) > 1 {
		formatDesc := matches[1]
		contentFormat := s.parseFormatDescription(formatDesc)
		format["content"] = contentFormat
	}

	// 也查找中文摘要格式描述
	chineseAbstractPattern := regexp.MustCompile(`中文摘要[：:\s]+([^\n\r]+)`)
	if matches := chineseAbstractPattern.FindStringSubmatch(text); len(matches) > 1 {
		formatDesc := matches[1]

		// 分析摘要格式
		if strings.Contains(formatDesc, "摘要：") && strings.Contains(formatDesc, "仿宋") {
			// 提取摘要标签格式
			labelPattern := regexp.MustCompile(`摘要：([\s\S]+?)；`)
			if labelMatches := labelPattern.FindStringSubmatch(formatDesc); len(labelMatches) > 1 {
				labelFormat := s.parseFormatDescription(labelMatches[1])
				labelMap, ok := format["label"].(map[string]interface{})
				if ok {
					for key, value := range labelFormat {
						labelMap[key] = value
					}
				}
			}

			// 提取关键词格式
			keywordsPattern := regexp.MustCompile(`关键词([\s\S]+)`)
			if keywordsMatches := keywordsPattern.FindStringSubmatch(formatDesc); len(keywordsMatches) > 1 {
				keywordsFormat := s.parseFormatDescription(keywordsMatches[1])
				// 添加关键词分隔符信息
				if strings.Contains(keywordsMatches[1], "分号") || strings.Contains(keywordsMatches[1], "；") {
					keywordsFormat["separator"] = "；"
					keywordsFormat["no_end_punctuation"] = true
				}
				format["keywords"] = keywordsFormat
			}
		}
	}

	return format
}

// extractKeywordsFormat 提取关键词格式
func (s *FormatParserService) extractKeywordsFormat(text string) map[string]interface{} {
	format := map[string]interface{}{
		"label": map[string]interface{}{
			"text":      "关键词：",
			"font_name": "宋体",
			"font_size": "五号",
			"bold":      true,
		},
		"content": map[string]interface{}{
			"font_name":          "宋体",
			"font_size":          "五号",
			"separator":          "；",
			"no_end_punctuation": true,
		},
	}

	// 查找关键词格式描述
	keywordsPattern := regexp.MustCompile(`(?:中文关键词|关键词)[：:]\s*([^)）]+)`)
	if matches := keywordsPattern.FindStringSubmatch(text); len(matches) > 1 {
		formatDesc := matches[1]
		contentFormat := s.parseFormatDescription(formatDesc)
		format["content"] = contentFormat

		// 检查是否提到分隔符
		if strings.Contains(text, "；") || strings.Contains(text, "分号") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["separator"] = "；"
			}
		}

		// 检查是否提到最后一个词不打标点
		if strings.Contains(text, "最后一个词不打标点") || strings.Contains(text, "最后不打标点") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["no_end_punctuation"] = true
			}
		}
	}

	return format
}

// extractBodyFormat 提取正文格式
func (s *FormatParserService) extractBodyFormat(text string) map[string]interface{} {
	format := map[string]interface{}{
		"font_name":         "宋体",
		"font_size":         "小四号",
		"line_space":        "1.5",
		"alignment":         "justify",
		"first_line_indent": "2字符",
		"paragraph_space": map[string]interface{}{
			"before": "0",
			"after":  "0",
		},
	}

	// 查找正文格式描述
	bodyPattern := regexp.MustCompile(`(?:正文)[：:\s]+([^\n\r]+)`)
	if matches := bodyPattern.FindStringSubmatch(text); len(matches) > 1 {
		formatDesc := matches[1]
		format = s.parseFormatDescription(formatDesc)
	}

	// 检查是否提到标题加粗
	if strings.Contains(text, "标题加粗") {
		format["bold_for_headings"] = true
	}

	return format
}

// extractHeadingsFormat 提取标题层级格式
func (s *FormatParserService) extractHeadingsFormat(text string) map[string]interface{} {
	// 默认标题格式
	headingsFormat := map[string]interface{}{
		"level1": map[string]interface{}{
			"font_name":  "黑体",
			"font_size":  "三号",
			"bold":       true,
			"alignment":  "left",
			"numbering":  "1",
			"line_space": "single",
		},
		"level2": map[string]interface{}{
			"font_name":  "黑体",
			"font_size":  "四号",
			"bold":       true,
			"alignment":  "left",
			"numbering":  "1.1",
			"line_space": "single",
		},
		"level3": map[string]interface{}{
			"font_name":  "黑体",
			"font_size":  "小四号",
			"bold":       true,
			"alignment":  "left",
			"numbering":  "1.1.1",
			"line_space": "single",
		},
	}

	// 查找章节标题格式描述
	chapterPattern := regexp.MustCompile(`章节标题[：:\s]+([^\n\r]+)`)
	if matches := chapterPattern.FindStringSubmatch(text); len(matches) > 1 {
		chapterDesc := matches[1]
		// 解析章节标题格式描述
		chapterFormat := s.parseFormatDescription(chapterDesc)

		// 应用到一级标题
		for key, value := range chapterFormat {
			if _, exists := headingsFormat["level1"].(map[string]interface{})[key]; exists {
				headingsFormat["level1"].(map[string]interface{})[key] = value
			} else {
				// 如果level1中没有该字段，添加新字段
				level1Map := headingsFormat["level1"].(map[string]interface{})
				level1Map[key] = value
			}
		}
	}

	// 查找更具体的章节标题格式描述，如"第1章、第2章、第3章等，居中、黑体四号"
	chapterDetailPattern := regexp.MustCompile(`第[一二三四五六七八九十]+章[^，,]+[，,]\s*([^\n\r]+)`)
	if matches := chapterDetailPattern.FindStringSubmatch(text); len(matches) > 1 {
		chapterDesc := matches[1]
		// 解析章节标题格式描述
		chapterFormat := s.parseFormatDescription(chapterDesc)

		// 应用到一级标题
		for key, value := range chapterFormat {
			if _, exists := headingsFormat["level1"].(map[string]interface{})[key]; exists {
				headingsFormat["level1"].(map[string]interface{})[key] = value
			} else {
				// 如果level1中没有该字段，添加新字段
				level1Map := headingsFormat["level1"].(map[string]interface{})
				level1Map[key] = value
			}
		}
	}

	// 查找更广泛的章节格式描述，支持"第1章、第2章、第3章等，居中、黑体四号"模式
	multipleChapterPattern := regexp.MustCompile(`第[一二三四五六七八九十\d]+章[、，]第[一二三四五六七八九十\d]+章[、，]第[一二三四五六七八九十\d]+章[^，,]*[，,]\s*([^\n\r]+)`)
	if matches := multipleChapterPattern.FindStringSubmatch(text); len(matches) > 1 {
		chapterDesc := matches[1]
		// 解析章节标题格式描述
		chapterFormat := s.parseFormatDescription(chapterDesc)

		// 应用到一级标题
		for key, value := range chapterFormat {
			if _, exists := headingsFormat["level1"].(map[string]interface{})[key]; exists {
				headingsFormat["level1"].(map[string]interface{})[key] = value
			} else {
				// 如果level1中没有该字段，添加新字段
				level1Map := headingsFormat["level1"].(map[string]interface{})
				level1Map[key] = value
			}
		}
	}

	// 查找其他标题格式描述
	// 例如：一级标题、二级标题、三级标题等
	level1Pattern := regexp.MustCompile(`(一级标题|第[一二三四五六七八九十]+章)[：:\s]+([^\n\r]+)`)
	if matches := level1Pattern.FindStringSubmatch(text); len(matches) > 2 {
		level1Desc := matches[2]
		level1Format := s.parseFormatDescription(level1Desc)

		level1Map := headingsFormat["level1"].(map[string]interface{})
		for key, value := range level1Format {
			level1Map[key] = value
		}
	}

	level2Pattern := regexp.MustCompile(`(二级标题|第[一二三四五六七八九十]+章[一二三四五六七八九十]+节)[：:\s]+([^\n\r]+)`)
	if matches := level2Pattern.FindStringSubmatch(text); len(matches) > 2 {
		level2Desc := matches[2]
		level2Format := s.parseFormatDescription(level2Desc)

		level2Map := headingsFormat["level2"].(map[string]interface{})
		for key, value := range level2Format {
			level2Map[key] = value
		}
	}

	level3Pattern := regexp.MustCompile(`(三级标题|第[一二三四五六七八九十]+章[一二三四五六七八九十]+节[一二三四五六七八九十]+条)[：:\s]+([^\n\r]+)`)
	if matches := level3Pattern.FindStringSubmatch(text); len(matches) > 2 {
		level3Desc := matches[2]
		level3Format := s.parseFormatDescription(level3Desc)

		level3Map := headingsFormat["level3"].(map[string]interface{})
		for key, value := range level3Format {
			level3Map[key] = value
		}
	}

	// 查找章节标题格式的更详细描述，例如"章节标题：第1章、第2章、第3章等，居中、黑体四号"
	detailedChapterPattern := regexp.MustCompile(`章节标题[：:]\s*第\d+章[^，,，]*[，,]\s*([^\n\r]+)`)
	if matches := detailedChapterPattern.FindStringSubmatch(text); len(matches) > 1 {
		chapterDesc := matches[1]
		chapterFormat := s.parseFormatDescription(chapterDesc)

		// 应用到一级标题
		for key, value := range chapterFormat {
			if _, exists := headingsFormat["level1"].(map[string]interface{})[key]; exists {
				headingsFormat["level1"].(map[string]interface{})[key] = value
			} else {
				// 如果level1中没有该字段，添加新字段
				level1Map := headingsFormat["level1"].(map[string]interface{})
				level1Map[key] = value
			}
		}
	}

	return headingsFormat
}

// extractReferencesFormat 提取参考文献格式
func (s *FormatParserService) extractReferencesFormat(text string) map[string]interface{} {
	format := map[string]interface{}{
		"title": map[string]interface{}{
			"text":      "参考文献",
			"font_name": "黑体",
			"font_size": "三号",
			"bold":      true,
			"alignment": "center",
		},
		"content": map[string]interface{}{
			"font_name":  "宋体",
			"font_size":  "五号",
			"alignment":  "justify",
			"numbering":  "[1]",
			"line_space": "single",
		},
	}

	// 查找参考文献格式描述 - 支持多种格式
	referencesPattern := regexp.MustCompile(`参考文献[：:\s]+([\s\S]*?)(?:\n|$)`)
	if matches := referencesPattern.FindStringSubmatch(text); len(matches) > 1 {
		formatDesc := matches[1]

		// 分析参考文献格式
		if strings.Contains(formatDesc, "小四") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["font_size"] = "小四号"
			}
		}
		if strings.Contains(formatDesc, "宋体") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["font_name"] = "宋体"
			}
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["font_name"] = "宋体"
			}
		}
		if strings.Contains(formatDesc, "五号") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["font_size"] = "五号"
			}
		}
		if strings.Contains(formatDesc, "单倍行距") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["line_space"] = "single"
			}
		}
		if strings.Contains(formatDesc, "顶格") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["alignment"] = "left" // 顶格就是左对齐
			}
		}
		if strings.Contains(formatDesc, "连续编号") {
			if content, ok := format["content"].(map[string]interface{}); ok {
				content["numbering"] = "[1]" // 连续编号
			}
		}
		if strings.Contains(formatDesc, "加粗") || strings.Contains(formatDesc, "粗体") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["bold"] = true
			}
		}
		if strings.Contains(formatDesc, "居中") {
			if title, ok := format["title"].(map[string]interface{}); ok {
				title["alignment"] = "center"
			}
		}
	}

	// 更精确的参考文献格式解析
	// 查找"参考文献"标题格式
	refTitlePattern := regexp.MustCompile(`"参考文献"([^，,]+)[，,]([\s\S]*?)`)
	if matches := refTitlePattern.FindStringSubmatch(text); len(matches) > 2 {
		titleDesc := matches[1] + "," + matches[2]
		titleFormat := s.parseFormatDescription(titleDesc)

		// 应用到标题格式
		if title, ok := format["title"].(map[string]interface{}); ok {
			for key, value := range titleFormat {
				title[key] = value
			}
		}
	}

	// 查找参考文献内容格式
	refContentPattern := regexp.MustCompile(`内容([^，,，]+[，,][^\n\r]+)`)
	if matches := refContentPattern.FindStringSubmatch(text); len(matches) > 1 {
		contentDesc := matches[1]
		contentFormat := s.parseFormatDescription(contentDesc)

		// 应用到内容格式
		if content, ok := format["content"].(map[string]interface{}); ok {
			for key, value := range contentFormat {
				content[key] = value
			}
		}
	}

	// 查找更具体的参考文献格式描述
	// 例如：参考文献 小四宋体加粗；内容宋体五号，单倍行距，顶格连续编号
	specificPattern := regexp.MustCompile(`参考文献\s+([^\n\r]+)`)
	if matches := specificPattern.FindStringSubmatch(text); len(matches) > 1 {
		specificDesc := matches[1]

		// 分割描述，处理标题和内容部分
		if strings.Contains(specificDesc, "；") || strings.Contains(specificDesc, ";") {
			parts := strings.Split(specificDesc, "；")
			if len(parts) >= 2 {
				// 第一部分是标题格式
				titleFormat := s.parseFormatDescription(parts[0])
				if title, ok := format["title"].(map[string]interface{}); ok {
					for key, value := range titleFormat {
						title[key] = value
					}
				}

				// 第二部分是内容格式
				contentFormat := s.parseFormatDescription(parts[1])
				if content, ok := format["content"].(map[string]interface{}); ok {
					for key, value := range contentFormat {
						content[key] = value
					}
				}
			}
		} else {
			// 如果只有一部分，可能是标题格式
			titleFormat := s.parseFormatDescription(specificDesc)
			if title, ok := format["title"].(map[string]interface{}); ok {
				for key, value := range titleFormat {
					title[key] = value
				}
			}
		}

	}

	return format
}

// extractPageSetup 提取页面设置
func (s *FormatParserService) extractPageSetup(text string) map[string]interface{} {
	return map[string]interface{}{
		"paper_size":  "A4",
		"orientation": "portrait",
		"margins": map[string]interface{}{
			"top":    "2.54cm",
			"bottom": "2.54cm",
			"left":   "3.17cm",
			"right":  "3.17cm",
		},
		"header": map[string]interface{}{
			"distance": "1.5cm",
		},
		"footer": map[string]interface{}{
			"distance": "1.75cm",
		},
	}
}

// extractEnglishTitleFormat 提取英文标题格式
func (s *FormatParserService) extractEnglishTitleFormat(text string) map[string]interface{} {
	format := map[string]interface{}{
		"alignment":  "center",
		"font_name":  "Times New Roman",
		"font_size":  "四号",
		"bold":       true,
		"line_space": "single",
	}

	// 查找英文标题格式描述
	englishTitlePattern := regexp.MustCompile(`(?:英文题目)[：:]\s*([^)）]+)`)
	if matches := englishTitlePattern.FindStringSubmatch(text); len(matches) > 1 {
		formatDesc := matches[1]
		format = s.parseFormatDescription(formatDesc)
	}

	return format
}

// extractEnglishAbstractFormat 提取英文摘要格式
func (s *FormatParserService) extractEnglishAbstractFormat(text string) map[string]interface{} {
	return map[string]interface{}{
		"label": map[string]interface{}{
			"text":      "Abstract:",
			"font_name": "Times New Roman",
			"font_size": "五号",
			"bold":      true,
		},
		"content": map[string]interface{}{
			"font_name":  "Times New Roman",
			"font_size":  "五号",
			"alignment":  "justify",
			"line_space": "1.5",
		},
	}
}

// parseFormatDescription 解析格式描述字符串
func (s *FormatParserService) parseFormatDescription(desc string) map[string]interface{} {
	format := make(map[string]interface{})

	// 解析对齐方式
	if strings.Contains(desc, "居中") {
		format["alignment"] = "center"
	} else if strings.Contains(desc, "居左") || strings.Contains(desc, "左对齐") {
		format["alignment"] = "left"
	} else if strings.Contains(desc, "居右") || strings.Contains(desc, "右对齐") {
		format["alignment"] = "right"
	} else if strings.Contains(desc, "两端对齐") {
		format["alignment"] = "justify"
	}

	// 解析字体
	fontPattern := regexp.MustCompile(`(黑体|宋体|仿宋|楷体|Times New Roman)`)
	if matches := fontPattern.FindStringSubmatch(desc); len(matches) > 0 {
		format["font_name"] = matches[1]
	}

	// 解析字号
	sizePattern := regexp.MustCompile(`(初号|小初|一号|小一|二号|小二|三号|小三|四号|小四|五号|小五|六号|小六)`)
	if matches := sizePattern.FindStringSubmatch(desc); len(matches) > 0 {
		format["font_size"] = matches[1]
	}

	// 解析加粗
	if strings.Contains(desc, "加粗") || strings.Contains(desc, "粗体") {
		format["bold"] = true
	}

	// 解析斜体
	if strings.Contains(desc, "斜体") {
		format["italic"] = true
	}

	// 解析行距
	lineSpacePattern := regexp.MustCompile(`(单倍行距|1\.5倍行距|2倍行距|固定值\s*(\d+)磅)`)
	if matches := lineSpacePattern.FindStringSubmatch(desc); len(matches) > 0 {
		if strings.Contains(matches[1], "单倍") {
			format["line_space"] = "single"
		} else if strings.Contains(matches[1], "1.5") {
			format["line_space"] = "1.5"
		} else if strings.Contains(matches[1], "2倍") {
			format["line_space"] = "double"
		} else if len(matches) > 2 {
			format["line_space"] = fmt.Sprintf("%s磅", matches[2])
		}
	}

	return format
}

// DocumentStructure 文档结构信息
type DocumentStructure struct {
	Sections     []Section             // 文档章节
	FormatRules  map[string]FormatRule // 格式规则
	Examples     []FormatExample       // 格式示例
	SpecialRules []SpecialRule         // 特殊规则
	Metadata     DocumentMetadata      // 元数据
}

// Section 文档章节
type Section struct {
	Type     string   // 章节类型：title, abstract, body, references等
	Level    int      // 层级
	Content  string   // 内容
	Keywords []string // 关键词
}

// FormatRule 格式规则
type FormatRule struct {
	Target     string            // 应用目标
	Properties map[string]string // 格式属性
	Priority   int               // 优先级（用于冲突解决）
	Source     string            // 来源位置
	Confidence float64           // 置信度
}

// FormatExample 格式示例
type FormatExample struct {
	Type        string            // 示例类型
	Description string            // 描述
	Properties  map[string]string // 提取的属性
}

// SpecialRule 特殊规则
type SpecialRule struct {
	Type        string // 规则类型
	Description string // 描述
	Value       string // 值
}

// DocumentMetadata 文档元数据
type DocumentMetadata struct {
	UniversityName string // 高校名称
	DocumentType   string // 文档类型
	Version        string // 版本
	Year           string // 年份
}

// ExtractUniversityInfo 从文本中提取高校信息（改进版 - 规则引擎方式）
func (s *FormatParserService) ExtractUniversityInfo(text string) map[string]string {
	info := make(map[string]string)

	// 第一步：结构化识别
	structure := s.identifyDocumentStructure(text)

	// 第二步：信息分类与分层
	classifiedInfo := s.classifyAndLayerInformation(structure)

	// 第三步：模式匹配与提取
	extractedRules := s.extractFormatPatterns(classifiedInfo)

	// 第四步：冲突解决与整合
	_ = s.resolveConflictsAndMerge(extractedRules) // 用于未来扩展

	// 提取高校名称（多种模式）
	info["name"] = s.extractUniversityName(text, structure)

	// 提取文档类型（语义理解）
	info["document_type"] = s.extractDocumentType(text, structure)

	// 提取版本信息
	if version := s.extractVersion(text); version != "" {
		info["version"] = version
	}

	// 提取年份信息
	if year := s.extractYear(text); year != "" {
		info["year"] = year
	}
	// 添加置信度信息
	info["confidence"] = fmt.Sprintf("%.2f", s.calculateConfidence(info))

	return info
}

// identifyDocumentStructure 识别文档组织结构
func (s *FormatParserService) identifyDocumentStructure(text string) DocumentStructure {
	structure := DocumentStructure{
		Sections:     []Section{},
		FormatRules:  make(map[string]FormatRule),
		Examples:     []FormatExample{},
		SpecialRules: []SpecialRule{},
		Metadata:     DocumentMetadata{},
	}

	lines := strings.Split(text, "\n")
	currentSection := ""

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 识别标题（通常包含数字编号或特定关键词）
		if s.isHeading(line) {
			section := Section{
				Type:     s.identifySectionType(line),
				Level:    s.identifyHeadingLevel(line),
				Content:  line,
				Keywords: s.extractKeywords(line),
			}
			structure.Sections = append(structure.Sections, section)
			currentSection = section.Type
		}

		// 识别格式说明（包含格式关键词）
		if s.isFormatDescription(line) {
			rule := s.parseFormatRule(line, currentSection, i)
			if rule.Target != "" {
				structure.FormatRules[fmt.Sprintf("%s_%d", rule.Target, i)] = rule
			}
		}

		// 识别格式示例（通常有明显的排版特征）
		if s.isFormatExample(line) {
			example := s.parseFormatExample(line, currentSection)
			structure.Examples = append(structure.Examples, example)
		}

		// 识别特殊规则（如"参考文献不少于15篇"）
		if s.isSpecialRule(line) {
			rule := s.parseSpecialRule(line)
			structure.SpecialRules = append(structure.SpecialRules, rule)
		}
	}

	return structure
}

// classifyAndLayerInformation 信息分类与分层
func (s *FormatParserService) classifyAndLayerInformation(structure DocumentStructure) map[string]interface{} {
	classified := make(map[string]interface{})

	// 结构层：论文组成部分
	structureLayer := make(map[string][]string)
	for _, section := range structure.Sections {
		if _, exists := structureLayer[section.Type]; !exists {
			structureLayer[section.Type] = []string{}
		}
		structureLayer[section.Type] = append(structureLayer[section.Type], section.Content)
	}
	classified["structure"] = structureLayer

	// 格式层：具体格式要求
	formatLayer := make(map[string]map[string]string)
	for key, rule := range structure.FormatRules {
		formatLayer[key] = rule.Properties
	}
	classified["format"] = formatLayer

	// 规则层：特殊规则
	ruleLayer := make([]map[string]string, 0)
	for _, rule := range structure.SpecialRules {
		ruleLayer = append(ruleLayer, map[string]string{
			"type":        rule.Type,
			"description": rule.Description,
			"value":       rule.Value,
		})
	}
	classified["rules"] = ruleLayer

	// 示例层：格式示例
	exampleLayer := make([]map[string]interface{}, 0)
	for _, example := range structure.Examples {
		exampleLayer = append(exampleLayer, map[string]interface{}{
			"type":        example.Type,
			"description": example.Description,
			"properties":  example.Properties,
		})
	}
	classified["examples"] = exampleLayer

	return classified
}

// extractFormatPatterns 模式匹配与提取
func (s *FormatParserService) extractFormatPatterns(classifiedInfo map[string]interface{}) []FormatRule {
	rules := []FormatRule{}

	// 从格式层提取
	if formatLayer, ok := classifiedInfo["format"].(map[string]map[string]string); ok {
		for target, properties := range formatLayer {
			rule := FormatRule{
				Target:     target,
				Properties: properties,
				Priority:   s.calculatePriority(target, properties),
				Confidence: s.calculateRuleConfidence(properties),
			}
			rules = append(rules, rule)
		}
	}

	// 从示例层提取
	if exampleLayer, ok := classifiedInfo["examples"].([]map[string]interface{}); ok {
		for _, example := range exampleLayer {
			if props, ok := example["properties"].(map[string]string); ok {
				rule := FormatRule{
					Target:     example["type"].(string),
					Properties: props,
					Priority:   2, // 示例的优先级较低
					Source:     "example",
					Confidence: 0.7,
				}
				rules = append(rules, rule)
			}
		}
	}

	return rules
}

// resolveConflictsAndMerge 冲突解决与整合
func (s *FormatParserService) resolveConflictsAndMerge(rules []FormatRule) map[string]FormatRule {
	merged := make(map[string]FormatRule)

	// 按目标分组
	grouped := make(map[string][]FormatRule)
	for _, rule := range rules {
		grouped[rule.Target] = append(grouped[rule.Target], rule)
	}

	// 对每个目标解决冲突
	for target, targetRules := range grouped {
		if len(targetRules) == 1 {
			merged[target] = targetRules[0]
			continue
		}

		// 多个规则时，按优先级和置信度选择
		bestRule := targetRules[0]
		for _, rule := range targetRules[1:] {
			// 优先级高的优先
			if rule.Priority > bestRule.Priority {
				bestRule = rule
			} else if rule.Priority == bestRule.Priority {
				// 优先级相同时，置信度高的优先
				if rule.Confidence > bestRule.Confidence {
					bestRule = rule
				}
			}
		}

		// 合并属性（保留所有不冲突的属性）
		mergedProps := make(map[string]string)
		for _, rule := range targetRules {
			for key, value := range rule.Properties {
				if _, exists := mergedProps[key]; !exists {
					mergedProps[key] = value
				}
			}
		}
		bestRule.Properties = mergedProps
		merged[target] = bestRule
	}

	return merged
}

// extractUniversityName 提取高校名称（多种模式）
func (s *FormatParserService) extractUniversityName(text string, structure DocumentStructure) string {
	// 模式1：直接匹配高校名称
	patterns := []string{
		`([^，,。\s]{2,20}(?:大学|学院|研究院|研究所))`,
		`([^，,。\s]+(?:理工|师范|医科|财经|政法|农业|工业|科技|交通)大学)`,
		`([^，,。\s]+(?:职业|技术)学院)`,
	}

	// 清理特殊字符的函数
	cleanName := func(name string) string {
		if name == "" {
			return name
		}
		// 移除XML标签和其他特殊字符
		xmlPatterns := []string{
			`[0-9]+"/></w:rPr><w:t>`,  // 匹配数字加XML标签
			`</?w:[a-zA-Z]+>`,         // 匹配所有<w:xxx>标签
			`[<>"/=]`,                 // 匹配其他XML特殊字符
			`\s+`,                     // 多个空格
			`^[\s\p{C}]+|[\s\p{C}]+$`, // 开头和结尾的控制字符和空白
		}

		for _, pattern := range xmlPatterns {
			re := regexp.MustCompile(pattern)
			name = re.ReplaceAllString(name, "")
		}

		// 额外的清理：去掉常见的标点符号
		punctuation := []string{"！", "？", "。", "，", ",", "!", "?", ";", ":", "、", "“", "”", "《", "》"}
		for _, p := range punctuation {
			name = strings.Trim(name, p)
		}

		// 最后再去除首尾空白
		return strings.TrimSpace(name)
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			name := matches[1]
			name = cleanName(name)
			// 验证名称合理性
			if s.isValidUniversityName(name) {
				return name
			}
		}
	}

	// 模式2：从文档标题提取
	for _, section := range structure.Sections {
		if section.Type == "title" || section.Level == 1 {
			for _, pattern := range patterns {
				re := regexp.MustCompile(pattern)
				if matches := re.FindStringSubmatch(section.Content); len(matches) > 1 {
					name := matches[1]
					name = cleanName(name)
					return name
				}
			}
		}
	}

	// 模式3：从元数据提取
	if structure.Metadata.UniversityName != "" {
		name := structure.Metadata.UniversityName
		name = cleanName(name)
		return name
	}

	return ""
}

// extractDocumentType 提取文档类型（语义理解）
func (s *FormatParserService) extractDocumentType(text string, structure DocumentStructure) string {
	// 关键词权重表
	typeKeywords := map[string]map[string]int{
		"本科论文": {
			"本科": 10, "学士": 8, "毕业论文": 5, "毕业设计": 5,
			"undergraduate": 8, "bachelor": 8,
		},
		"硕士论文": {
			"硕士": 10, "研究生": 7, "master": 8,
		},
		"博士论文": {
			"博士": 10, "phd": 8, "doctor": 8, "doctoral": 8,
		},
		"课程论文": {
			"课程": 8, "作业": 5, "报告": 3,
		},
	}

	// 计算每种类型的得分
	scores := make(map[string]int)
	textLower := strings.ToLower(text)

	for docType, keywords := range typeKeywords {
		score := 0
		for keyword, weight := range keywords {
			count := strings.Count(textLower, strings.ToLower(keyword))
			score += count * weight
		}
		scores[docType] = score
	}

	// 找出得分最高的类型
	maxScore := 0
	bestType := ""
	for docType, score := range scores {
		if score > maxScore {
			maxScore = score
			bestType = docType
		}
	}

	// 如果得分太低，返回默认值
	if maxScore < 5 {
		return "本科论文" // 默认
	}

	return bestType
}

// extractVersion 提取版本信息
func (s *FormatParserService) extractVersion(text string) string {
	patterns := []string{
		`版本[：:]\s*([0-9.]+)`,
		`V([0-9.]+)`,
		`v([0-9.]+)`,
		`第([一二三四五六七八九十]+)版`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			return matches[1]
		}
	}

	return ""
}

// extractYear 提取年份信息
func (s *FormatParserService) extractYear(text string) string {
	// 匹配 2020-2030 年份
	re := regexp.MustCompile(`(20[2-3][0-9])年?`)
	if matches := re.FindStringSubmatch(text); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// 辅助方法

func (s *FormatParserService) isHeading(line string) bool {
	// 判断是否为标题
	headingPatterns := []string{
		`^[一二三四五六七八九十]+[、.]`, // 中文数字编号
		`^[0-9]+[、.]`, // 阿拉伯数字编号
		`^第[一二三四五六七八九十]+[章节]`,    // 第X章
		`^[（(][一二三四五六七八九十]+[)）]`, // (一)
	}

	for _, pattern := range headingPatterns {
		if matched, _ := regexp.MatchString(pattern, line); matched {
			return true
		}
	}

	return false
}

func (s *FormatParserService) identifySectionType(line string) string {
	keywords := map[string][]string{
		"title":      {"标题", "题目", "论文题目"},
		"abstract":   {"摘要", "内容摘要"},
		"keywords":   {"关键词", "关键字"},
		"body":       {"正文", "主体", "内容"},
		"references": {"参考文献", "引用", "文献"},
		"appendix":   {"附录"},
		"thanks":     {"致谢", "谢辞"},
	}

	lineLower := strings.ToLower(line)
	for sectionType, kws := range keywords {
		for _, kw := range kws {
			if strings.Contains(lineLower, strings.ToLower(kw)) {
				return sectionType
			}
		}
	}

	return "unknown"
}

func (s *FormatParserService) identifyHeadingLevel(line string) int {
	// 根据编号判断层级
	if matched, _ := regexp.MatchString(`^[一二三四五六七八九十]+[、.]`, line); matched {
		return 1
	}
	if matched, _ := regexp.MatchString(`^[0-9]+[、.]`, line); matched {
		return 1
	}
	if matched, _ := regexp.MatchString(`^[0-9]+\.[0-9]+`, line); matched {
		return 2
	}
	if matched, _ := regexp.MatchString(`^[0-9]+\.[0-9]+\.[0-9]+`, line); matched {
		return 3
	}
	return 0
}

func (s *FormatParserService) extractKeywords(line string) []string {
	// 提取行中的关键词
	keywords := []string{}
	formatKeywords := []string{
		"字体", "字号", "行距", "居中", "左对齐", "右对齐", "加粗", "斜体",
		"宋体", "黑体", "仿宋", "楷体", "Times New Roman",
		"一号", "二号", "三号", "四号", "五号", "小四", "小五",
	}

	for _, kw := range formatKeywords {
		if strings.Contains(line, kw) {
			keywords = append(keywords, kw)
		}
	}

	return keywords
}

func (s *FormatParserService) isFormatDescription(line string) bool {
	// 判断是否为格式描述
	formatKeywords := []string{
		"字体", "字号", "行距", "居中", "对齐", "加粗", "斜体",
		"磅", "倍", "厘米", "mm",
	}

	count := 0
	for _, kw := range formatKeywords {
		if strings.Contains(line, kw) {
			count++
		}
	}

	return count >= 2 // 至少包含2个格式关键词
}

func (s *FormatParserService) parseFormatRule(line string, section string, lineNum int) FormatRule {
	rule := FormatRule{
		Target:     section,
		Properties: make(map[string]string),
		Priority:   3, // 默认优先级
		Source:     fmt.Sprintf("line_%d", lineNum),
		Confidence: 0.8,
	}

	// 提取格式属性
	properties := s.parseFormatDescription(line)
	for key, value := range properties {
		if strValue, ok := value.(string); ok {
			rule.Properties[key] = strValue
		}
	}

	return rule
}

func (s *FormatParserService) isFormatExample(line string) bool {
	// 判断是否为格式示例（通常包含示例标记）
	exampleMarkers := []string{
		"示例", "例如", "如下", "格式如下", "样式",
	}

	for _, marker := range exampleMarkers {
		if strings.Contains(line, marker) {
			return true
		}
	}

	return false
}

func (s *FormatParserService) parseFormatExample(line string, section string) FormatExample {
	return FormatExample{
		Type:        section,
		Description: line,
		Properties:  s.extractPropertiesFromExample(line),
	}
}

func (s *FormatParserService) extractPropertiesFromExample(line string) map[string]string {
	props := make(map[string]string)

	// 从示例中提取属性（简化版）
	if strings.Contains(line, "居中") {
		props["alignment"] = "center"
	}
	if strings.Contains(line, "加粗") {
		props["bold"] = "true"
	}

	return props
}

func (s *FormatParserService) isSpecialRule(line string) bool {
	// 判断是否为特殊规则
	rulePatterns := []string{
		`不少于\d+`,
		`不超过\d+`,
		`至少\d+`,
		`最多\d+`,
		`必须`,
		`应当`,
		`不得`,
	}

	for _, pattern := range rulePatterns {
		if matched, _ := regexp.MatchString(pattern, line); matched {
			return true
		}
	}

	return false
}

func (s *FormatParserService) parseSpecialRule(line string) SpecialRule {
	rule := SpecialRule{
		Description: line,
	}

	// 识别规则类型
	if strings.Contains(line, "参考文献") {
		rule.Type = "references_count"
		// 提取数量
		re := regexp.MustCompile(`(\d+)`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			rule.Value = matches[1]
		}
	}

	return rule
}

func (s *FormatParserService) calculatePriority(target string, properties map[string]string) int {
	// 根据属性完整性计算优先级
	priority := 1
	if len(properties) > 3 {
		priority = 3
	} else if len(properties) > 1 {
		priority = 2
	}
	return priority
}

func (s *FormatParserService) calculateRuleConfidence(properties map[string]string) float64 {
	// 根据属性数量和质量计算置信度
	confidence := 0.5
	if len(properties) > 0 {
		confidence = 0.5 + float64(len(properties))*0.1
	}
	if confidence > 1.0 {
		confidence = 1.0
	}
	return confidence
}

func (s *FormatParserService) calculateConfidence(info map[string]string) float64 {
	// 计算整体置信度
	confidence := 0.0
	if info["name"] != "" {
		confidence += 0.5
	}
	if info["document_type"] != "" {
		confidence += 0.3
	}
	if info["version"] != "" {
		confidence += 0.1
	}
	if info["year"] != "" {
		confidence += 0.1
	}
	return confidence
}

func (s *FormatParserService) isValidUniversityName(name string) bool {
	// 验证高校名称的合理性
	if len(name) < 4 || len(name) > 30 {
		return false
	}

	// 排除一些明显不是高校名称的词
	invalidWords := []string{"格式", "要求", "规范", "说明", "文档"}
	for _, word := range invalidWords {
		if strings.Contains(name, word) {
			return false
		}
	}

	return true
}
