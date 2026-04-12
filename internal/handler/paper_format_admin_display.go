package handler

// 管理员展示用：英文字段名 -> 中文键（与 friendlyAnnotateMap 组合使用）
var (
	adminBodyKeyRename = map[string]string{
		"font_size":         "字号",
		"font_name":         "字体",
		"alignment":         "对齐方式",
		"line_space":        "行距",
		"first_line_indent": "首行缩进",
		"bold":              "是否加粗",
	}
	adminParagraphSpaceKeyRename = map[string]string{
		"after":  "段后间距",
		"before": "段前间距",
	}
	adminTitleLikeKeyRename = map[string]string{
		"font_size": "字号",
		"font_name": "字体",
		"alignment": "对齐方式",
		"bold":      "是否加粗",
	}
	adminHeadingInnerKeyRename = map[string]string{
		"font_size": "字号",
		"font_name": "字体",
		"alignment": "对齐方式",
		"bold":      "是否加粗",
		"numbering": "编号",
	}
	adminLabelKeyRename = map[string]string{
		"font_size": "字号",
		"font_name": "字体",
		"bold":      "是否加粗",
		"text":      "文本",
	}
	adminAbstractCNContentKeyRename = map[string]string{
		"font_size": "字号",
		"font_name": "字体",
		"bold":      "是否加粗",
		"numbering": "编号",
		"separator": "分隔符",
	}
	adminKeywordsContentKeyRename = map[string]string{
		"font_size": "字号",
		"font_name": "字体",
	}
	adminEnglishAbstractContentKeyRename = map[string]string{
		"font_size":  "字号",
		"font_name":  "字体",
		"alignment":  "对齐方式",
		"line_space": "行距",
		"bold":       "是否加粗",
		"numbering":  "编号",
	}
	adminReferencesContentKeyRename = map[string]string{
		"font_size":  "字号",
		"font_name":  "字体",
		"alignment":  "对齐方式",
		"line_space": "行距",
		"numbering":  "编号格式",
		"bold":       "是否加粗",
	}
	adminSectionTitleKeyRename = map[string]string{
		"text":      "文本",
		"font_name": "字体",
		"font_size": "字号",
		"alignment": "对齐方式",
		"bold":      "是否加粗",
	}
	adminSimpleTypographyKeyRename = map[string]string{
		"font_name": "字体",
		"font_size": "字号",
		"alignment": "对齐方式",
	}
)

// adminConvertFormatRulesToChineseFriendly 将英文键格式规则转为管理员界面用的中文键 + 带说明的展示值。
// 行为与原 AdminUniversityHandler.convertToFriendlyFormat 一致。
func adminConvertFormatRulesToChineseFriendly(formatRules map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range formatRules {
		switch key {
		case "body":
			if m, ok := value.(map[string]interface{}); ok {
				result["正文"] = adminRemapBodyBlock(m)
			}
		case "title":
			if m, ok := value.(map[string]interface{}); ok {
				result["标题"] = adminRemapTitleLikeBlock(m)
			}
		case "author":
			if m, ok := value.(map[string]interface{}); ok {
				result["作者"] = adminRemapTitleLikeBlock(m)
			}
		case "abstract":
			if m, ok := value.(map[string]interface{}); ok {
				result["摘要"] = adminRemapAbstractOrEnglishAbstractBlock(m, false)
			}
		case "headings":
			if m, ok := value.(map[string]interface{}); ok {
				adminMergeHeadingsIntoResult(result, m)
			}
		case "keywords":
			if m, ok := value.(map[string]interface{}); ok {
				result["关键词"] = adminRemapKeywordsBlock(m)
			}
		case "page_setup":
			if m, ok := value.(map[string]interface{}); ok {
				result["页面设置"] = adminRemapPageSetupBlock(m)
			}
		case "references":
			if m, ok := value.(map[string]interface{}); ok {
				result["参考文献"] = adminRemapReferencesBlock(m)
			}
		default:
			adminRemapTopLevelSpecial(result, key, value)
		}
	}

	return result
}

func adminRemapBodyBlock(bodyMap map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	rest := make(map[string]interface{}, len(bodyMap))
	for k, v := range bodyMap {
		if k == "paragraph_space" {
			if spaceMap, ok := v.(map[string]interface{}); ok {
				out["段落间距"] = friendlyRenameTopLevelKeys(spaceMap, adminParagraphSpaceKeyRename)
			} else {
				out[k] = v
			}
			continue
		}
		rest[k] = v
	}
	annotated := friendlyAnnotateMap(rest, friendlyBodyAnnotatorsForAdmin)
	for nk, nv := range friendlyRenameTopLevelKeys(annotated, adminBodyKeyRename) {
		out[nk] = nv
	}
	return out
}

func adminRemapTitleLikeBlock(m map[string]interface{}) map[string]interface{} {
	return friendlyRenameTopLevelKeys(friendlyConvertTitleLikeFormat(m), adminTitleLikeKeyRename)
}

func adminRemapLabelBlock(labelMap map[string]interface{}) map[string]interface{} {
	return friendlyRenameTopLevelKeys(friendlyAnnotateMap(labelMap, friendlyLabelAnnotators), adminLabelKeyRename)
}

func adminRemapAbstractContentBlock(contentMap map[string]interface{}) map[string]interface{} {
	return friendlyRenameTopLevelKeys(friendlyAnnotateMap(contentMap, friendlyAbstractCNContentAnnotators), adminAbstractCNContentKeyRename)
}

func adminRemapAbstractContentWithLineSpaceBlock(contentMap map[string]interface{}) map[string]interface{} {
	return friendlyRenameTopLevelKeys(friendlyAnnotateMap(contentMap, friendlyEnglishAbstractContentAnnotators), adminEnglishAbstractContentKeyRename)
}

func adminRemapKeywordsContentBlock(contentMap map[string]interface{}) map[string]interface{} {
	return friendlyRenameTopLevelKeys(friendlyAnnotateMap(contentMap, friendlyKeywordsContentAnnotators), adminKeywordsContentKeyRename)
}

func adminRemapAbstractOrEnglishAbstractBlock(abstractMap map[string]interface{}, englishAbstract bool) map[string]interface{} {
	out := make(map[string]interface{})
	for abstractKey, abstractValue := range abstractMap {
		switch abstractKey {
		case "label":
			if labelMap, ok := abstractValue.(map[string]interface{}); ok {
				out["标签"] = adminRemapLabelBlock(labelMap)
			}
		case "content":
			if contentMap, ok := abstractValue.(map[string]interface{}); ok {
				if englishAbstract {
					out["内容"] = adminRemapAbstractContentWithLineSpaceBlock(contentMap)
				} else {
					out["内容"] = adminRemapAbstractContentBlock(contentMap)
				}
			}
		default:
			out[abstractKey] = abstractValue
		}
	}
	return out
}

func adminRemapKeywordsBlock(keywordsMap map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	for keywordKey, keywordValue := range keywordsMap {
		switch keywordKey {
		case "label":
			if labelMap, ok := keywordValue.(map[string]interface{}); ok {
				out["标签"] = adminRemapLabelBlock(labelMap)
			}
		case "content":
			if contentMap, ok := keywordValue.(map[string]interface{}); ok {
				out["内容"] = adminRemapKeywordsContentBlock(contentMap)
			}
		default:
			out[keywordKey] = keywordValue
		}
	}
	return out
}

var adminHeadingLevelChinese = map[string]string{
	"level1": "一级标题",
	"level2": "二级标题",
	"level3": "三级标题",
	"level4": "四级标题",
	"level5": "五级标题",
	"level6": "六级标题",
}

func adminMergeHeadingsIntoResult(result map[string]interface{}, headingsMap map[string]interface{}) {
	for headingLevel, headingValue := range headingsMap {
		headingMap, ok := headingValue.(map[string]interface{})
		if !ok {
			continue
		}
		chineseLevel := adminHeadingLevelChinese[headingLevel]
		if chineseLevel == "" {
			chineseLevel = headingLevel
		}
		result[chineseLevel] = adminRemapHeadingInnerBlock(headingMap)
	}
}

func adminRemapHeadingInnerBlock(headingMap map[string]interface{}) map[string]interface{} {
	return friendlyRenameTopLevelKeys(friendlyConvertTitleLikeFormat(headingMap), adminHeadingInnerKeyRename)
}

func adminRemapPageSetupBlock(pageSetupMap map[string]interface{}) map[string]interface{} {
	pageResult := make(map[string]interface{})
	for pageKey, pageValue := range pageSetupMap {
		switch pageKey {
		case "margins":
			if marginsMap, ok := pageValue.(map[string]interface{}); ok {
				marginsResult := make(map[string]interface{})
				for marginKey, marginValue := range marginsMap {
					switch marginKey {
					case "top":
						marginsResult["上边距"] = marginValue
					case "bottom":
						marginsResult["下边距"] = marginValue
					case "left":
						marginsResult["左边距"] = marginValue
					case "right":
						marginsResult["右边距"] = marginValue
					default:
						marginsResult[marginKey] = marginValue
					}
				}
				pageResult["页边距"] = marginsResult
			}
		case "header":
			if headerMap, ok := pageValue.(map[string]interface{}); ok {
				headerResult := make(map[string]interface{})
				for headerKey, headerValue := range headerMap {
					if headerKey == "distance" {
						headerResult["距离"] = headerValue
					} else {
						headerResult[headerKey] = headerValue
					}
				}
				pageResult["页眉"] = headerResult
			}
		case "footer":
			if footerMap, ok := pageValue.(map[string]interface{}); ok {
				footerResult := make(map[string]interface{})
				for footerKey, footerValue := range footerMap {
					if footerKey == "distance" {
						footerResult["距离"] = footerValue
					} else {
						footerResult[footerKey] = footerValue
					}
				}
				pageResult["页脚"] = footerResult
			}
		case "orientation":
			if orientation, ok := pageValue.(string); ok {
				switch orientation {
				case "portrait":
					pageResult["方向"] = "纵向"
				case "landscape":
					pageResult["方向"] = "横向"
				default:
					pageResult["方向"] = pageValue
				}
			} else {
				pageResult["方向"] = pageValue
			}
		case "paper_size":
			if paperSize, ok := pageValue.(string); ok {
				switch paperSize {
				case "A4":
					pageResult["纸张大小"] = "A4"
				default:
					pageResult["纸张大小"] = paperSize
				}
			} else {
				pageResult["纸张大小"] = pageValue
			}
		default:
			pageResult[pageKey] = pageValue
		}
	}
	return pageResult
}

func adminRemapReferencesContentBlock(contentMap map[string]interface{}) map[string]interface{} {
	return friendlyRenameTopLevelKeys(friendlyAnnotateMap(contentMap, friendlyReferencesContentAnnotators), adminReferencesContentKeyRename)
}

func adminRemapReferencesBlock(referencesMap map[string]interface{}) map[string]interface{} {
	referencesResult := make(map[string]interface{})
	for refKey, refValue := range referencesMap {
		switch refKey {
		case "title":
			if titleMap, ok := refValue.(map[string]interface{}); ok {
				referencesResult["标题"] = adminRemapTitleLikeBlock(titleMap)
			}
		case "content":
			if contentMap, ok := refValue.(map[string]interface{}); ok {
				referencesResult["内容"] = adminRemapReferencesContentBlock(contentMap)
			}
		default:
			referencesResult[refKey] = refValue
		}
	}
	return referencesResult
}

func adminRemapSectionTitleWithTextBlock(titleMap map[string]interface{}) map[string]interface{} {
	return friendlyRenameTopLevelKeys(friendlyAnnotateMap(titleMap, friendlyTitleLikeAnnotators), adminSectionTitleKeyRename)
}

func adminRemapSimpleTypographyContentBlock(contentMap map[string]interface{}) map[string]interface{} {
	return friendlyRenameTopLevelKeys(friendlyAnnotateMap(contentMap, friendlySimpleTypographyAnnotators), adminSimpleTypographyKeyRename)
}

func adminRemapTOCorAckOrAppendixBlock(sectionMap map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	for k, v := range sectionMap {
		switch k {
		case "title":
			if titleMap, ok := v.(map[string]interface{}); ok {
				out["标题"] = adminRemapSectionTitleWithTextBlock(titleMap)
			}
		case "content":
			if contentMap, ok := v.(map[string]interface{}); ok {
				out["内容"] = adminRemapSimpleTypographyContentBlock(contentMap)
			}
		default:
			out[k] = v
		}
	}
	return out
}

func adminRemapTopLevelSpecial(result map[string]interface{}, key string, value interface{}) {
	switch key {
	case "english_title":
		if titleMap, ok := value.(map[string]interface{}); ok {
			result["英文标题"] = adminRemapTitleLikeBlock(titleMap)
		}
	case "english_abstract":
		if abstractMap, ok := value.(map[string]interface{}); ok {
			result["英文摘要"] = adminRemapAbstractOrEnglishAbstractBlock(abstractMap, true)
		}
	case "cover":
		if coverMap, ok := value.(map[string]interface{}); ok {
			coverResult := make(map[string]interface{})
			for coverKey, coverValue := range coverMap {
				if coverKey == "description" {
					coverResult["描述"] = coverValue
				} else {
					coverResult[coverKey] = coverValue
				}
			}
			result["封面"] = coverResult
		}
	case "table_of_contents":
		if tocMap, ok := value.(map[string]interface{}); ok {
			result["目录"] = adminRemapTOCorAckOrAppendixBlock(tocMap)
		}
	case "acknowledgements":
		if ackMap, ok := value.(map[string]interface{}); ok {
			result["致谢"] = adminRemapTOCorAckOrAppendixBlock(ackMap)
		}
	case "appendix":
		if appMap, ok := value.(map[string]interface{}); ok {
			result["附录"] = adminRemapTOCorAckOrAppendixBlock(appMap)
		}
	default:
		result[key] = value
	}
}
