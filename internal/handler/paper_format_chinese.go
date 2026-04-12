package handler

// friendlyParsedRequirementsToChineseMap 将解析后的格式要求转换为汉字键值对结构
func friendlyParsedRequirementsToChineseMap(parsedFormat *ParsedFormatRequirements) map[string]interface{} {
	out := make(map[string]interface{})

	out["学校名称"] = parsedFormat.Institution
	out["文档类型"] = parsedFormat.DocumentType

	if len(parsedFormat.BasicRequirements) > 0 {
		out["基本要求"] = parsedFormat.BasicRequirements
	}

	out["页面设置"] = chinesePageSetupMap(parsedFormat.PageSetup)
	out["字体设置"] = chineseFontSettingsMap(parsedFormat.FontSettings)
	out["文档结构"] = chineseDocumentStructureMap(parsedFormat.Structure)
	out["引用规则"] = chineseCitationRulesMap(parsedFormat.CitationRules)
	out["附录规则"] = chineseAppendixRulesMap(parsedFormat.AppendixRules)

	return out
}

func chinesePageSetupMap(ps PageSetup) map[string]interface{} {
	m := make(map[string]interface{})
	m["纸张大小"] = ps.PaperSize
	m["页面方向"] = ps.Orientation
	m["页边距"] = map[string]interface{}{
		"上边距": ps.Margins.Top,
		"下边距": ps.Margins.Bottom,
		"左边距": ps.Margins.Left,
		"右边距": ps.Margins.Right,
	}
	m["页眉页脚"] = map[string]interface{}{
		"页眉高度":   ps.HeaderFooter.HeaderHeight,
		"页脚高度":   ps.HeaderFooter.FooterHeight,
		"页眉左侧内容": ps.HeaderFooter.HeaderLeft,
		"页眉右侧内容": ps.HeaderFooter.HeaderRight,
		"页眉居中内容": ps.HeaderFooter.HeaderCenter,
	}
	m["打印方式"] = ps.PrintingSide
	return m
}

func chineseFontSettingsMap(fs FontSettings) map[string]interface{} {
	out := make(map[string]interface{})

	out["正文字体"] = map[string]interface{}{
		"字体名称": fs.MainFont.FontName,
		"字体大小": fs.MainFont.FontSize,
		"行间距":  fs.MainFont.LineSpacing,
	}

	tf := fs.TitleFont
	out["标题字体"] = map[string]interface{}{
		"章标题":  chineseTitleStyleMap(tf.ChapterTitle.FontName, tf.ChapterTitle.FontSize, tf.ChapterTitle.Alignment),
		"节标题":  chineseTitleStyleMap(tf.SectionTitle.FontName, tf.SectionTitle.FontSize, tf.SectionTitle.Alignment),
		"小节标题": chineseTitleStyleMap(tf.SubsectionTitle.FontName, tf.SubsectionTitle.FontSize, tf.SubsectionTitle.Alignment),
	}

	out["摘要字体"] = chineseTwoFieldFontMap(fs.AbstractFont.FontName, fs.AbstractFont.FontSize)
	out["目录字体"] = chineseTwoFieldFontMap(fs.DirectoryFont.FontName, fs.DirectoryFont.FontSize)
	out["表格字体"] = chineseTwoFieldFontMap(fs.TableFont.FontName, fs.TableFont.FontSize)
	out["图片字体"] = chineseTwoFieldFontMap(fs.FigureFont.FontName, fs.FigureFont.FontSize)

	return out
}

func chineseTitleStyleMap(fontName string, fontSize float64, alignment string) map[string]interface{} {
	return map[string]interface{}{
		"字体名称": fontName,
		"字体大小": fontSize,
		"对齐方式": alignment,
	}
}

func chineseTwoFieldFontMap(fontName string, fontSize float64) map[string]interface{} {
	return map[string]interface{}{
		"字体名称": fontName,
		"字体大小": fontSize,
	}
}

func chineseDocumentStructureMap(s DocumentStructure) map[string]interface{} {
	return map[string]interface{}{
		"前置部分": map[string]interface{}{
			"封面":   s.FrontMatter.CoverPage,
			"版权声明": s.FrontMatter.CopyrightStatement,
			"摘要":   s.FrontMatter.Abstract,
			"目录":   s.FrontMatter.TableOfContents,
			"插图清单": s.FrontMatter.ListOfFigures,
			"表格清单": s.FrontMatter.ListOfTables,
		},
		"主体部分": map[string]interface{}{
			"引言": s.MainBody.Introduction,
			"正文": s.MainBody.MainContent,
			"结论": s.MainBody.Conclusion,
		},
		"后置部分": map[string]interface{}{
			"参考文献": s.BackMatter.References,
			"致谢":   s.BackMatter.Acknowledgements,
			"附录":   s.BackMatter.Appendices,
		},
	}
}

func chineseCitationRulesMap(c CitationRules) map[string]interface{} {
	m := map[string]interface{}{
		"参考文献格式": c.ReferenceFormat,
	}
	if len(c.ReferenceTypes) > 0 {
		m["参考文献类型"] = c.ReferenceTypes
	}
	return m
}

func chineseAppendixRulesMap(a AppendixRules) map[string]interface{} {
	m := map[string]interface{}{
		"附录格式": a.AppendixFormat,
	}
	if len(a.AttachmentList) > 0 {
		m["附件列表"] = a.AttachmentList
	}
	return m
}
