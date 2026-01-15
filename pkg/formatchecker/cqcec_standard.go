package formatchecker

// CQCECStandard 返回重庆工程学院格式标准配置
func CQCECStandard() FormatStandard {
	return FormatStandard{
		Name:        "重庆工程学院本科毕业论文格式标准",
		Description: "重庆工程学院本科毕业设计（论文）格式规范",
		PageSetup: PageSetup{
			PaperSize:      "A4",
			MarginTop:      2.5,
			MarginBottom:   2.5,
			MarginLeft:     2.5,
			MarginRight:    2.5,
			HeaderDistance: 1.6, // 页眉高度1.6cm
			FooterDistance: 2.1, // 页脚高度2.1cm
		},
		HeadingStyles: []HeadingStyle{
			// 一级标题（章）- 三号黑体，居中
			{
				Level:         1,
				Name:          "一级标题",
				FontName:      "黑体",
				FontSize:      16, // 三号
				Bold:          true,
				Alignment:     "center",
				SpacingBefore: 24,
				SpacingAfter:  18,
				LineSpacing:   20,
			},
			// 二级标题（节）- 小三号黑体，居左
			{
				Level:         2,
				Name:          "二级标题",
				FontName:      "黑体",
				FontSize:      15, // 小三号
				Bold:          true,
				Alignment:     "left",
				SpacingBefore: 20,
				SpacingAfter:  16,
				LineSpacing:   20,
			},
			// 三级标题（条）- 四号黑体，居左，右缩进两字
			{
				Level:         3,
				Name:          "三级标题",
				FontName:      "黑体",
				FontSize:      14, // 四号
				Bold:          true,
				Alignment:     "left",
				SpacingBefore: 18,
				SpacingAfter:  14,
				LineSpacing:   20,
				IndentRight:   2, // 右缩进两字
			},
			// 四级标题 - 小四号宋体，右缩进两字（理工类：①）
			{
				Level:         4,
				Name:          "四级标题",
				FontName:      "宋体",
				FontSize:      12, // 小四号
				Bold:          false,
				Alignment:     "left",
				SpacingBefore: 16,
				SpacingAfter:  12,
				LineSpacing:   20,
				IndentRight:   2, // 右缩进两字
			},
			// 五级标题 - 小四号宋体，右缩进两字（理工类：1））
			{
				Level:         5,
				Name:          "五级标题",
				FontName:      "宋体",
				FontSize:      12, // 小四号
				Bold:          false,
				Alignment:     "left",
				SpacingBefore: 14,
				SpacingAfter:  10,
				LineSpacing:   20,
				IndentRight:   2, // 右缩进两字
			},
			// 六级标题 - 小四号宋体，右缩进两字（理工类：a．）
			{
				Level:         6,
				Name:          "六级标题",
				FontName:      "宋体",
				FontSize:      12, // 小四号
				Bold:          false,
				Alignment:     "left",
				SpacingBefore: 12,
				SpacingAfter:  8,
				LineSpacing:   20,
				IndentRight:   2, // 右缩进两字
			},
		},
		ParagraphStyles: []ParagraphStyle{
			// 正文段落
			{
				Name:            "正文",
				FontName:        "宋体",
				FontSize:        12,
				Alignment:       "justify",
				LineSpacing:     20,
				FirstLineIndent: 2,
				SpacingBefore:   0,
				SpacingAfter:    0,
			},
			// 摘要段落
			{
				Name:            "摘要正文",
				FontName:        "宋体",
				FontSize:        12,
				Alignment:       "justify",
				LineSpacing:     20,
				FirstLineIndent: 2,
				SpacingBefore:   0,
				SpacingAfter:    0,
			},
		},
		TableStyle: TableStyle{
			CaptionPrefix:   "表格",
			FontName:        "宋体",
			FontSize:        10.5,
			CaptionPosition: "top",
			BorderStyle:     "all_borders",
		},
		FigureStyle: FigureStyle{
			CaptionPrefix:   "图",
			FontName:        "宋体",
			FontSize:        10.5,
			CaptionPosition: "bottom",
		},
		ReferenceStyle: ReferenceStyle{
			Style:        "GB/T 7714",
			FontName:     "宋体",
			FontSize:     10.5,
			LineSpacing:  20,
			IndentFormat: "hanging_indent",
		},
		AbstractStyles: []AbstractStyle{
			// 中文摘要
			{
				Type:           "chinese",
				Heading:        "摘要",
				FontName:       "宋体",
				FontSize:       14,
				Bold:           true,
				Alignment:      "center",
				LineSpacing:    20,
				KeywordsPrefix: "关键词：",
			},
			// 英文摘要
			{
				Type:           "english",
				Heading:        "Abstract",
				FontName:       "Times New Roman",
				FontSize:       14,
				Bold:           true,
				Alignment:      "center",
				LineSpacing:    20,
				KeywordsPrefix: "Keywords: ",
			},
		},
	}
}
