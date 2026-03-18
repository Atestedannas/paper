package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// AdminUniversityHandler 高校管理处理器
type AdminUniversityHandler struct{}

// NewAdminUniversityHandler 创建高校管理处理器实例
func NewAdminUniversityHandler() *AdminUniversityHandler {
	return &AdminUniversityHandler{}
}

// convertToFriendlyFormat 将格式规则转换为友好展示格式
func (h *AdminUniversityHandler) convertToFriendlyFormat(formatRules map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// 将所有格式要求转换为简单的汉字键值对
	for key, value := range formatRules {
		switch key {
		case "body":
			if bodyMap, ok := value.(map[string]interface{}); ok {
				bodyResult := make(map[string]interface{})
				for bodyKey, bodyValue := range bodyMap {
					switch bodyKey {
					case "font_size":
						if fontSize, ok := bodyValue.(string); ok {
							bodyResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
						} else {
							bodyResult["字号"] = bodyValue
						}
					case "font_name":
						if fontName, ok := bodyValue.(string); ok {
							bodyResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
						} else {
							bodyResult["字体"] = bodyValue
						}
					case "alignment":
						if alignment, ok := bodyValue.(string); ok {
							bodyResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
						} else {
							bodyResult["对齐方式"] = bodyValue
						}
					case "line_space":
						if lineSpace, ok := bodyValue.(string); ok {
							bodyResult["行距"] = lineSpace + "（" + h.lineSpaceToChinese(lineSpace) + "）"
						} else {
							bodyResult["行距"] = bodyValue
						}
					case "first_line_indent":
						if indent, ok := bodyValue.(string); ok {
							bodyResult["首行缩进"] = indent + "（" + h.indentToChinese(indent) + "）"
						} else {
							bodyResult["首行缩进"] = bodyValue
						}
					default:
						switch bodyKey {
						case "bold":
							bodyResult["是否加粗"] = bodyValue
						case "paragraph_space":
							if spaceMap, ok := bodyValue.(map[string]interface{}); ok {
								spaceResult := make(map[string]interface{})
								for spaceKey, spaceValue := range spaceMap {
									switch spaceKey {
									case "after":
										spaceResult["段后间距"] = spaceValue
									case "before":
										spaceResult["段前间距"] = spaceValue
									default:
										spaceResult[spaceKey] = spaceValue
									}
								}
								bodyResult["段落间距"] = spaceResult
							} else {
								bodyResult[bodyKey] = bodyValue
							}
						default:
							bodyResult[bodyKey] = bodyValue
						}
					}
				}
				result["正文"] = bodyResult
			}
		case "title":
			if titleMap, ok := value.(map[string]interface{}); ok {
				titleResult := make(map[string]interface{})
				for titleKey, titleValue := range titleMap {
					switch titleKey {
					case "font_size":
						if fontSize, ok := titleValue.(string); ok {
							titleResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
						} else {
							titleResult["字号"] = titleValue
						}
					case "font_name":
						if fontName, ok := titleValue.(string); ok {
							titleResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
						} else {
							titleResult["字体"] = titleValue
						}
					case "alignment":
						if alignment, ok := titleValue.(string); ok {
							titleResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
						} else {
							titleResult["对齐方式"] = titleValue
						}
					default:
						switch titleKey {
						case "bold":
							titleResult["是否加粗"] = titleValue
						default:
							titleResult[titleKey] = titleValue
						}
					}
				}
				result["标题"] = titleResult
			}
		case "author":
			if authorMap, ok := value.(map[string]interface{}); ok {
				authorResult := make(map[string]interface{})
				for authorKey, authorValue := range authorMap {
					switch authorKey {
					case "font_size":
						if fontSize, ok := authorValue.(string); ok {
							authorResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
						} else {
							authorResult["字号"] = authorValue
						}
					case "font_name":
						if fontName, ok := authorValue.(string); ok {
							authorResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
						} else {
							authorResult["字体"] = authorValue
						}
					case "alignment":
						if alignment, ok := authorValue.(string); ok {
							authorResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
						} else {
							authorResult["对齐方式"] = authorValue
						}
					default:
						switch authorKey {
						case "bold":
							authorResult["是否加粗"] = authorValue
						default:
							authorResult[authorKey] = authorValue
						}
					}
				}
				result["作者"] = authorResult
			}
		case "abstract":
			if abstractMap, ok := value.(map[string]interface{}); ok {
				abstractResult := make(map[string]interface{})
				for abstractKey, abstractValue := range abstractMap {
					switch abstractKey {
					case "label":
						if labelMap, ok := abstractValue.(map[string]interface{}); ok {
							labelResult := make(map[string]interface{})
							for labelKey, labelValue := range labelMap {
								switch labelKey {
								case "font_size":
									if fontSize, ok := labelValue.(string); ok {
										labelResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
									} else {
										labelResult["字号"] = labelValue
									}
								case "font_name":
									if fontName, ok := labelValue.(string); ok {
										labelResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
									} else {
										labelResult["字体"] = labelValue
									}
								default:
									switch labelKey {
									case "bold":
										labelResult["是否加粗"] = labelValue
									case "text":
										labelResult["文本"] = labelValue
									default:
										labelResult[labelKey] = labelValue
									}
								}
							}
							abstractResult["标签"] = labelResult
						}
					case "content":
						if contentMap, ok := abstractValue.(map[string]interface{}); ok {
							contentResult := make(map[string]interface{})
							for contentKey, contentValue := range contentMap {
								switch contentKey {
								case "font_size":
									if fontSize, ok := contentValue.(string); ok {
										contentResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
									} else {
										contentResult["字号"] = contentValue
									}
								case "font_name":
									if fontName, ok := contentValue.(string); ok {
										contentResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
									} else {
										contentResult["字体"] = contentValue
									}
								default:
									switch contentKey {
									case "bold":
										contentResult["是否加粗"] = contentValue
									case "numbering":
										contentResult["编号"] = contentValue
									case "separator":
										contentResult["分隔符"] = contentValue
									default:
										contentResult[contentKey] = contentValue
									}
								}
							}
							abstractResult["内容"] = contentResult
						}
					default:
						abstractResult[abstractKey] = abstractValue
					}
				}
				result["摘要"] = abstractResult
			}
		case "headings":
			if headingsMap, ok := value.(map[string]interface{}); ok {
				for headingLevel, headingValue := range headingsMap {
					if headingMap, ok := headingValue.(map[string]interface{}); ok {
						headingResult := make(map[string]interface{})
						for headingKey, headingValue := range headingMap {
							switch headingKey {
							case "font_size":
								if fontSize, ok := headingValue.(string); ok {
									headingResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
								} else {
									headingResult["字号"] = headingValue
								}
							case "font_name":
								if fontName, ok := headingValue.(string); ok {
									headingResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
								} else {
									headingResult["字体"] = headingValue
								}
							case "alignment":
								if alignment, ok := headingValue.(string); ok {
									headingResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
								} else {
									headingResult["对齐方式"] = headingValue
								}
							default:
								switch headingKey {
								case "bold":
									headingResult["是否加粗"] = headingValue
								case "numbering":
									headingResult["编号"] = headingValue
								default:
									headingResult[headingKey] = headingValue
								}
							}
						}
						// 翻译层级键名
						var chineseLevel string
						switch headingLevel {
						case "level1":
							chineseLevel = "一级标题"
						case "level2":
							chineseLevel = "二级标题"
						case "level3":
							chineseLevel = "三级标题"
						case "level4":
							chineseLevel = "四级标题"
						case "level5":
							chineseLevel = "五级标题"
						case "level6":
							chineseLevel = "六级标题"
						default:
							chineseLevel = headingLevel // 如果不是标准层级，保持原名
						}
						result[chineseLevel] = headingResult
					}
				}
			}
		case "keywords":
			if keywordsMap, ok := value.(map[string]interface{}); ok {
				keywordsResult := make(map[string]interface{})
				for keywordKey, keywordValue := range keywordsMap {
					switch keywordKey {
					case "label":
						if labelMap, ok := keywordValue.(map[string]interface{}); ok {
							labelResult := make(map[string]interface{})
							for labelKey, labelValue := range labelMap {
								switch labelKey {
								case "font_size":
									if fontSize, ok := labelValue.(string); ok {
										labelResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
									} else {
										labelResult["字号"] = labelValue
									}
								case "font_name":
									if fontName, ok := labelValue.(string); ok {
										labelResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
									} else {
										labelResult["字体"] = labelValue
									}
								default:
									switch labelKey {
									case "bold":
										labelResult["是否加粗"] = labelValue
									case "text":
										labelResult["文本"] = labelValue
									default:
										labelResult[labelKey] = labelValue
									}
								}
							}
							keywordsResult["标签"] = labelResult
						}
					case "content":
						if contentMap, ok := keywordValue.(map[string]interface{}); ok {
							contentResult := make(map[string]interface{})
							for contentKey, contentValue := range contentMap {
								switch contentKey {
								case "font_size":
									if fontSize, ok := contentValue.(string); ok {
										contentResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
									} else {
										contentResult["字号"] = contentValue
									}
								case "font_name":
									if fontName, ok := contentValue.(string); ok {
										contentResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
									} else {
										contentResult["字体"] = contentValue
									}
								default:
									contentResult[contentKey] = contentValue
								}
							}
							keywordsResult["内容"] = contentResult
						}
					default:
						keywordsResult[keywordKey] = keywordValue
					}
				}
				result["关键词"] = keywordsResult
			}
		case "page_setup":
			if pageSetupMap, ok := value.(map[string]interface{}); ok {
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
								switch headerKey {
								case "distance":
									headerResult["距离"] = headerValue
								default:
									headerResult[headerKey] = headerValue
								}
							}
							pageResult["页眉"] = headerResult
						}
					case "footer":
						if footerMap, ok := pageValue.(map[string]interface{}); ok {
							footerResult := make(map[string]interface{})
							for footerKey, footerValue := range footerMap {
								switch footerKey {
								case "distance":
									footerResult["距离"] = footerValue
								default:
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
					default:
						switch pageKey {
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
				}
				result["页面设置"] = pageResult
			}
		case "references":
			if referencesMap, ok := value.(map[string]interface{}); ok {
				referencesResult := make(map[string]interface{})
				for refKey, refValue := range referencesMap {
					switch refKey {
					case "title":
						if titleMap, ok := refValue.(map[string]interface{}); ok {
							titleResult := make(map[string]interface{})
							for titleKey, titleValue := range titleMap {
								switch titleKey {
								case "font_size":
									if fontSize, ok := titleValue.(string); ok {
										titleResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
									} else {
										titleResult["字号"] = titleValue
									}
								case "font_name":
									if fontName, ok := titleValue.(string); ok {
										titleResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
									} else {
										titleResult["字体"] = titleValue
									}
								case "alignment":
									if alignment, ok := titleValue.(string); ok {
										titleResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
									} else {
										titleResult["对齐方式"] = titleValue
									}
								default:
									switch titleKey {
									case "bold":
										titleResult["是否加粗"] = titleValue
									default:
										titleResult[titleKey] = titleValue
									}
								}
							}
							referencesResult["标题"] = titleResult
						}
					case "content":
						if contentMap, ok := refValue.(map[string]interface{}); ok {
							contentResult := make(map[string]interface{})
							for contentKey, contentValue := range contentMap {
								switch contentKey {
								case "font_size":
									if fontSize, ok := contentValue.(string); ok {
										contentResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
									} else {
										contentResult["字号"] = contentValue
									}
								case "font_name":
									if fontName, ok := contentValue.(string); ok {
										contentResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
									} else {
										contentResult["字体"] = contentValue
									}
								case "alignment":
									if alignment, ok := contentValue.(string); ok {
										contentResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
									} else {
										contentResult["对齐方式"] = contentValue
									}
								case "line_space":
									if lineSpace, ok := contentValue.(string); ok {
										contentResult["行距"] = lineSpace + "（" + h.lineSpaceToChinese(lineSpace) + "）"
									} else {
										contentResult["行距"] = contentValue
									}
								case "numbering":
									contentResult["编号格式"] = contentValue
								default:
									switch contentKey {
									case "bold":
										contentResult["是否加粗"] = contentValue
									default:
										contentResult[contentKey] = contentValue
									}
								}
							}
							referencesResult["内容"] = contentResult
						}
					default:
						referencesResult[refKey] = refValue
					}
				}
				result["参考文献"] = referencesResult
			}
		default:
			switch key {
			case "english_title":
				if titleMap, ok := value.(map[string]interface{}); ok {
					titleResult := make(map[string]interface{})
					for titleKey, titleValue := range titleMap {
						switch titleKey {
						case "font_size":
							if fontSize, ok := titleValue.(string); ok {
								titleResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
							} else {
								titleResult["字号"] = titleValue
							}
						case "font_name":
							if fontName, ok := titleValue.(string); ok {
								titleResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
							} else {
								titleResult["字体"] = titleValue
							}
						case "alignment":
							if alignment, ok := titleValue.(string); ok {
								titleResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
							} else {
								titleResult["对齐方式"] = titleValue
							}
						default:
							switch titleKey {
							case "bold":
								titleResult["是否加粗"] = titleValue
							default:
								titleResult[titleKey] = titleValue
							}
						}
					}
					result["英文标题"] = titleResult
				}
			case "english_abstract":
				if abstractMap, ok := value.(map[string]interface{}); ok {
					abstractResult := make(map[string]interface{})
					for abstractKey, abstractValue := range abstractMap {
						switch abstractKey {
						case "label":
							if labelMap, ok := abstractValue.(map[string]interface{}); ok {
								labelResult := make(map[string]interface{})
								for labelKey, labelValue := range labelMap {
									switch labelKey {
									case "font_size":
										if fontSize, ok := labelValue.(string); ok {
											labelResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
										} else {
											labelResult["字号"] = labelValue
										}
									case "font_name":
										if fontName, ok := labelValue.(string); ok {
											labelResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
										} else {
											labelResult["字体"] = labelValue
										}
									default:
										switch labelKey {
										case "bold":
											labelResult["是否加粗"] = labelValue
										case "text":
											labelResult["文本"] = labelValue
										default:
											labelResult[labelKey] = labelValue
										}
									}
								}
								abstractResult["标签"] = labelResult
							}
						case "content":
							if contentMap, ok := abstractValue.(map[string]interface{}); ok {
								contentResult := make(map[string]interface{})
								for contentKey, contentValue := range contentMap {
									switch contentKey {
									case "font_size":
										if fontSize, ok := contentValue.(string); ok {
											contentResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
										} else {
											contentResult["字号"] = contentValue
										}
									case "font_name":
										if fontName, ok := contentValue.(string); ok {
											contentResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
										} else {
											contentResult["字体"] = contentValue
										}
									case "alignment":
										if alignment, ok := contentValue.(string); ok {
											contentResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
										} else {
											contentResult["对齐方式"] = contentValue
										}
									case "line_space":
										if lineSpace, ok := contentValue.(string); ok {
											contentResult["行距"] = lineSpace + "（" + h.lineSpaceToChinese(lineSpace) + "）"
										} else {
											contentResult["行距"] = contentValue
										}
									default:
										switch contentKey {
										case "bold":
											contentResult["是否加粗"] = contentValue
										case "numbering":
											contentResult["编号"] = contentValue
										default:
											contentResult[contentKey] = contentValue
										}
									}
								}
								abstractResult["内容"] = contentResult
							}
						default:
							abstractResult[abstractKey] = abstractValue
						}
					}
					result["英文摘要"] = abstractResult
				}
			case "cover":
				if coverMap, ok := value.(map[string]interface{}); ok {
					coverResult := make(map[string]interface{})
					for coverKey, coverValue := range coverMap {
						switch coverKey {
						case "description":
							coverResult["描述"] = coverValue
						default:
							coverResult[coverKey] = coverValue
						}
					}
					result["封面"] = coverResult
				}
			case "table_of_contents":
				if tocMap, ok := value.(map[string]interface{}); ok {
					tocResult := make(map[string]interface{})
					for tocKey, tocValue := range tocMap {
						switch tocKey {
						case "title":
							if titleMap, ok := tocValue.(map[string]interface{}); ok {
								titleResult := make(map[string]interface{})
								for titleKey, titleValue := range titleMap {
									switch titleKey {
									case "text":
										titleResult["文本"] = titleValue
									case "font_name":
										if fontName, ok := titleValue.(string); ok {
											titleResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
										} else {
											titleResult["字体"] = titleValue
										}
									case "font_size":
										if fontSize, ok := titleValue.(string); ok {
											titleResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
										} else {
											titleResult["字号"] = titleValue
										}
									case "alignment":
										if alignment, ok := titleValue.(string); ok {
											titleResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
										} else {
											titleResult["对齐方式"] = titleValue
										}
									default:
										switch titleKey {
										case "bold":
											titleResult["是否加粗"] = titleValue
										default:
											titleResult[titleKey] = titleValue
										}
									}
								}
								tocResult["标题"] = titleResult
							}
						case "content":
							if contentMap, ok := tocValue.(map[string]interface{}); ok {
								contentResult := make(map[string]interface{})
								for contentKey, contentValue := range contentMap {
									switch contentKey {
									case "font_name":
										if fontName, ok := contentValue.(string); ok {
											contentResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
										} else {
											contentResult["字体"] = contentValue
										}
									case "font_size":
										if fontSize, ok := contentValue.(string); ok {
											contentResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
										} else {
											contentResult["字号"] = contentValue
										}
									case "alignment":
										if alignment, ok := contentValue.(string); ok {
											contentResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
										} else {
											contentResult["对齐方式"] = contentValue
										}
									default:
										contentResult[contentKey] = contentValue
									}
								}
								tocResult["内容"] = contentResult
							}
						default:
							tocResult[tocKey] = tocValue
						}
					}
					result["目录"] = tocResult
				}
			case "acknowledgements":
				if ackMap, ok := value.(map[string]interface{}); ok {
					ackResult := make(map[string]interface{})
					for ackKey, ackValue := range ackMap {
						switch ackKey {
						case "title":
							if titleMap, ok := ackValue.(map[string]interface{}); ok {
								titleResult := make(map[string]interface{})
								for titleKey, titleValue := range titleMap {
									switch titleKey {
									case "text":
										titleResult["文本"] = titleValue
									case "font_name":
										if fontName, ok := titleValue.(string); ok {
											titleResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
										} else {
											titleResult["字体"] = titleValue
										}
									case "font_size":
										if fontSize, ok := titleValue.(string); ok {
											titleResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
										} else {
											titleResult["字号"] = titleValue
										}
									case "alignment":
										if alignment, ok := titleValue.(string); ok {
											titleResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
										} else {
											titleResult["对齐方式"] = titleValue
										}
									default:
										switch titleKey {
										case "bold":
											titleResult["是否加粗"] = titleValue
										default:
											titleResult[titleKey] = titleValue
										}
									}
								}
								ackResult["标题"] = titleResult
							}
						case "content":
							if contentMap, ok := ackValue.(map[string]interface{}); ok {
								contentResult := make(map[string]interface{})
								for contentKey, contentValue := range contentMap {
									switch contentKey {
									case "font_name":
										if fontName, ok := contentValue.(string); ok {
											contentResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
										} else {
											contentResult["字体"] = contentValue
										}
									case "font_size":
										if fontSize, ok := contentValue.(string); ok {
											contentResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
										} else {
											contentResult["字号"] = contentValue
										}
									case "alignment":
										if alignment, ok := contentValue.(string); ok {
											contentResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
										} else {
											contentResult["对齐方式"] = contentValue
										}
									default:
										contentResult[contentKey] = contentValue
									}
								}
								ackResult["内容"] = contentResult
							}
						default:
							ackResult[ackKey] = ackValue
						}
					}
					result["致谢"] = ackResult
				}
			case "appendix":
				if appMap, ok := value.(map[string]interface{}); ok {
					appResult := make(map[string]interface{})
					for appKey, appValue := range appMap {
						switch appKey {
						case "title":
							if titleMap, ok := appValue.(map[string]interface{}); ok {
								titleResult := make(map[string]interface{})
								for titleKey, titleValue := range titleMap {
									switch titleKey {
									case "text":
										titleResult["文本"] = titleValue
									case "font_name":
										if fontName, ok := titleValue.(string); ok {
											titleResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
										} else {
											titleResult["字体"] = titleValue
										}
									case "font_size":
										if fontSize, ok := titleValue.(string); ok {
											titleResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
										} else {
											titleResult["字号"] = titleValue
										}
									case "alignment":
										if alignment, ok := titleValue.(string); ok {
											titleResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
										} else {
											titleResult["对齐方式"] = titleValue
										}
									default:
										switch titleKey {
										case "bold":
											titleResult["是否加粗"] = titleValue
										default:
											titleResult[titleKey] = titleValue
										}
									}
								}
								appResult["标题"] = titleResult
							}
						case "content":
							if contentMap, ok := appValue.(map[string]interface{}); ok {
								contentResult := make(map[string]interface{})
								for contentKey, contentValue := range contentMap {
									switch contentKey {
									case "font_name":
										if fontName, ok := contentValue.(string); ok {
											contentResult["字体"] = fontName + "（" + h.fontNameToChinese(fontName) + "）"
										} else {
											contentResult["字体"] = contentValue
										}
									case "font_size":
										if fontSize, ok := contentValue.(string); ok {
											contentResult["字号"] = fontSize + "（" + h.fontSizeToChinese(fontSize) + "）"
										} else {
											contentResult["字号"] = contentValue
										}
									case "alignment":
										if alignment, ok := contentValue.(string); ok {
											contentResult["对齐方式"] = alignment + "（" + h.alignmentToChinese(alignment) + "）"
										} else {
											contentResult["对齐方式"] = contentValue
										}
									default:
										contentResult[contentKey] = contentValue
									}
								}
								appResult["内容"] = contentResult
							}
						default:
							appResult[appKey] = appValue
						}
					}
					result["附录"] = appResult
				}
			default:
				result[key] = value
			}
		}
	}

	return result
}

// fontSizeToChinese 将字体大小转换为中文描述
func (h *AdminUniversityHandler) fontSizeToChinese(fontSize string) string {
	switch fontSize {
	case "小四号":
		return "小四号（12磅）"
	case "小四":
		return "小四（12磅）"
	case "四号":
		return "四号（14磅）"
	case "三号":
		return "三号（16磅）"
	case "小三号":
		return "小三号（15磅）"
	case "小三":
		return "小三（15磅）"
	case "二号":
		return "二号（22磅）"
	case "小二号":
		return "小二号（18磅）"
	case "小二":
		return "小二（18磅）"
	case "一号":
		return "一号（26磅）"
	case "小一号":
		return "小一号（24磅）"
	case "五号":
		return "五号（10.5磅）"
	case "小五号":
		return "小五号（9磅）"
	case "小五":
		return "小五（9磅）"
	case "六号":
		return "六号（7.5磅）"
	case "小六":
		return "小六（6.5磅）"
	case "七号":
		return "七号（5.5磅）"
	case "八号":
		return "八号（5磅）"
	default:
		return fontSize
	}
}

// fontNameToChinese 将字体名称转换为中文描述
func (h *AdminUniversityHandler) fontNameToChinese(fontName string) string {
	switch fontName {
	case "宋体":
		return "SimSun"
	case "黑体":
		return "SimHei"
	case "仿宋":
		return "FangSong"
	case "楷体":
		return "KaiTi"
	case "Times New Roman":
		return "Times New Roman"
	case "Arial":
		return "Arial"
	case "Verdana":
		return "Verdana"
	default:
		return fontName
	}
}

// alignmentToChinese 将对齐方式转换为中文描述
func (h *AdminUniversityHandler) alignmentToChinese(alignment string) string {
	switch alignment {
	case "left":
		return "left（左对齐）"
	case "center":
		return "center（居中）"
	case "right":
		return "right（右对齐）"
	case "justify":
		return "justify（两端对齐）"
	default:
		return alignment
	}
}

// lineSpaceToChinese 将行距转换为中文描述
func (h *AdminUniversityHandler) lineSpaceToChinese(lineSpace string) string {
	switch lineSpace {
	case "single":
		return "单倍行距"
	case "1.5", "1.5倍":
		return "1.5倍行距"
	case "double", "2":
		return "双倍行距"
	case "multiple":
		return "多倍行距"
	case "fixed":
		return "固定值"
	default:
		// 处理其他格式，如固定值格式 "fixed_20_pt"
		if strings.HasPrefix(lineSpace, "fixed_") {
			ptStr := strings.TrimPrefix(lineSpace, "fixed_")
			ptStr = strings.TrimSuffix(ptStr, "_pt")
			if _, err := strconv.ParseFloat(ptStr, 64); err == nil {
				return ptStr + "磅固定值"
			}
		}
		// 处理 "30" 这样的数值（可能是多倍行距倍数）
		if _, err := strconv.ParseFloat(lineSpace, 64); err == nil {
			return lineSpace + "倍行距"
		}
		return lineSpace
	}
}

// indentToChinese 将缩进转换为中文描述
func (h *AdminUniversityHandler) indentToChinese(indent string) string {
	switch indent {
	case "2字符":
		return "2字符（约14磅）"
	case "1字符":
		return "1字符（约7磅）"
	case "0字符":
		return "0字符（无缩进）"
	default:
		return indent
	}
}

// 以下是管理员高校管理接口的占位实现
// GetUniversities 获取高校列表
func (h *AdminUniversityHandler) GetUniversities(c *gin.Context) {
	q := c.Query("q")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	if pageSize > 100 {
		pageSize = 100
	}

	var universities []model.University
	var total int64

	db := database.DB.Model(&model.University{})

	if q != "" {
		db = db.Where("name LIKE ? OR abbr LIKE ?", "%"+q+"%", "%"+q+"%")
	}

	db.Count(&total)

	offset := (page - 1) * pageSize
	result := db.Preload("Templates").Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&universities)
	if result.Error != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取高校列表失败", result.Error.Error())
		return
	}

	// 填充虚拟字段
	for i := range universities {
		if len(universities[i].Templates) > 0 {
			// 取最新的模板（假设最后一个或按Version排序，这里简单取第一个）
			// 实际应该找IsActive=true的
			var activeTemplate *model.FormatTemplate
			for j := range universities[i].Templates {
				if universities[i].Templates[j].IsActive {
					activeTemplate = &universities[i].Templates[j]
					break
				}
			}
			if activeTemplate == nil {
				activeTemplate = &universities[i].Templates[0]
			}

			universities[i].FormatRequirements = json.RawMessage(activeTemplate.FormatRules)
			universities[i].FilePath = activeTemplate.FilePath
			universities[i].Subject = activeTemplate.Subject
			universities[i].DocumentType = activeTemplate.DocumentType

			// 简单的URL构造，实际可能需要更复杂的逻辑
			if strings.HasSuffix(strings.ToLower(activeTemplate.FilePath), ".docx") {
				universities[i].DocxTemplateURL = "/" + activeTemplate.FilePath // 假设FilePath是相对uploads的路径，或者已经是完整路径
				// 如果FilePath是绝对路径或不带/uploads前缀，需调整
				if !strings.HasPrefix(activeTemplate.FilePath, "/") && !strings.HasPrefix(activeTemplate.FilePath, "http") {
					universities[i].DocxTemplateURL = "/" + strings.ReplaceAll(activeTemplate.FilePath, "\\", "/")
				}
			} else if strings.HasSuffix(strings.ToLower(activeTemplate.FilePath), ".pdf") {
				universities[i].PdfTemplateURL = "/" + strings.ReplaceAll(activeTemplate.FilePath, "\\", "/")
			}
		}
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"page":      page,
		"page_size": pageSize,
		"total":     total,
		"items":     universities,
	})
}

// GetUniversity 获取高校详情
func (h *AdminUniversityHandler) GetUniversity(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的高校ID", err.Error())
		return
	}

	var university model.University
	if err := database.DB.Preload("Templates").First(&university, "id = ?", id).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "高校不存在", err.Error())
		return
	}

	// 填充虚拟字段
	if len(university.Templates) > 0 {
		var activeTemplate *model.FormatTemplate
		for j := range university.Templates {
			if university.Templates[j].IsActive {
				activeTemplate = &university.Templates[j]
				break
			}
		}
		if activeTemplate == nil {
			activeTemplate = &university.Templates[0]
		}

		university.FormatRequirements = json.RawMessage(activeTemplate.FormatRules)
		university.FilePath = activeTemplate.FilePath
		university.Subject = activeTemplate.Subject
		university.DocumentType = activeTemplate.DocumentType

		path := "/" + strings.ReplaceAll(activeTemplate.FilePath, "\\", "/")
		if strings.HasSuffix(strings.ToLower(activeTemplate.FilePath), ".docx") {
			university.DocxTemplateURL = path
		} else if strings.HasSuffix(strings.ToLower(activeTemplate.FilePath), ".pdf") {
			university.PdfTemplateURL = path
		}
	}

	utils.SuccessResponse(c, "获取成功", university)
}

// CreateUniversity 创建高校
func (h *AdminUniversityHandler) CreateUniversity(c *gin.Context) {
	var requestData struct {
		Name        string          `json:"name" binding:"required"`
		Abbr        string          `json:"abbr"`
		Subject     string          `json:"subject" binding:"required"`
		Description string          `json:"description"`
		Color       string          `json:"color"`
		Tags        string          `json:"tags"`
		FormatRules json.RawMessage `json:"format_rules"` // 可选：格式规则
	}

	if err := c.ShouldBindJSON(&requestData); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 1. 创建高校记录
	university := model.University{
		Name:        requestData.Name,
		Abbr:        requestData.Abbr,
		Description: requestData.Description,
		Color:       requestData.Color,
		Tags:        requestData.Tags,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	tx := database.DB.Begin()

	if err := tx.Create(&university).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建高校失败", err.Error())
		return
	}

	// 2. 如果提供了格式规则，创建关联的格式模板
	if len(requestData.FormatRules) > 0 {
		template := model.FormatTemplate{
			TemplateID:   fmt.Sprintf("%s_default_%d", strings.ToLower(requestData.Abbr), time.Now().Unix()),
			Name:         fmt.Sprintf("%s默认格式标准", requestData.Name),
			UniversityID: &university.ID,
			DocumentType: "thesis", // 默认本科论文
			Subject:      requestData.Subject,
			Source:       "system",
			Version:      "1.0",
			IsPublic:     true,
			IsActive:     true,
			FormatRules:  string(requestData.FormatRules),
			Description:  fmt.Sprintf("%s默认格式标准", requestData.Name),
		}

		if err := tx.Create(&template).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(c, http.StatusInternalServerError, "创建格式模板失败", err.Error())
			return
		}
	}

	tx.Commit()

	utils.SuccessResponse(c, "创建成功", university)
}

// UpdateUniversity 更新高校
func (h *AdminUniversityHandler) UpdateUniversity(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的高校ID", err.Error())
		return
	}

	var university model.University
	if err := database.DB.First(&university, "id = ?", id).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "高校不存在", err.Error())
		return
	}

	// 获取请求参数
	var requestData map[string]interface{}
	if err := c.ShouldBindJSON(&requestData); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 准备更新字段
	updates := make(map[string]interface{})

	// 检查是否有需要更新的字段
	if name, ok := requestData["name"].(string); ok && name != "" {
		updates["name"] = name
	}
	if abbr, ok := requestData["abbr"].(string); ok && abbr != "" {
		updates["abbr"] = abbr
	}
	if description, ok := requestData["description"].(string); ok {
		updates["description"] = description
	}
	if color, ok := requestData["color"].(string); ok && color != "" {
		updates["color"] = color
	}
	if tags, ok := requestData["tags"].(string); ok && tags != "" {
		updates["tags"] = tags
	}

	// 设置更新时间
	updates["updated_at"] = time.Now()

	// 开启事务
	tx := database.DB.Begin()

	// 执行更新高校基本信息
	if len(updates) > 0 {
		if err := tx.Model(&university).Updates(updates).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(c, http.StatusInternalServerError, "更新高校失败", err.Error())
			return
		}
	}

	// 检查是否需要更新格式规则
	if formatRequirementsVal, ok := requestData["format_requirements"]; ok && formatRequirementsVal != nil {
		var formatRequirementsStr string

		// 处理不同类型的输入
		switch v := formatRequirementsVal.(type) {
		case string:
			formatRequirementsStr = v
		case map[string]interface{}:
			// 如果是JSON对象，序列化为字符串
			bytes, err := json.Marshal(v)
			if err != nil {
				utils.ErrorResponse(c, http.StatusBadRequest, "格式规则格式错误", err.Error())
				return
			}
			formatRequirementsStr = string(bytes)
		case []interface{}: // 可能是数组
			bytes, err := json.Marshal(v)
			if err != nil {
				utils.ErrorResponse(c, http.StatusBadRequest, "格式规则格式错误", err.Error())
				return
			}
			formatRequirementsStr = string(bytes)
		default:
			// 尝试作为通用JSON处理
			bytes, err := json.Marshal(v)
			if err == nil {
				formatRequirementsStr = string(bytes)
			}
		}

		if formatRequirementsStr != "" {
			// 查找关联的模板
			var templates []model.FormatTemplate
			if err := tx.Where("university_id = ?", id).Find(&templates).Error; err != nil {
				tx.Rollback()
				utils.ErrorResponse(c, http.StatusInternalServerError, "获取关联模板失败", err.Error())
				return
			}

			var activeTemplate *model.FormatTemplate
			// 找活跃的模板
			for i := range templates {
				if templates[i].IsActive {
					activeTemplate = &templates[i]
					break
				}
			}

			if activeTemplate != nil {
				// 更新现有模板
				if err := tx.Model(activeTemplate).Update("format_rules", formatRequirementsStr).Error; err != nil {
					tx.Rollback()
					utils.ErrorResponse(c, http.StatusInternalServerError, "更新格式规则失败", err.Error())
					return
				}
			} else {
				// 如果没有模板，创建一个新的
				newTemplate := model.FormatTemplate{
					TemplateID:   fmt.Sprintf("%s_default_%d", strings.ToLower(university.Abbr), time.Now().Unix()),
					Name:         fmt.Sprintf("%s默认格式标准", university.Name),
					UniversityID: &university.ID,
					DocumentType: "thesis",
					Subject:      "综合", // 默认
					Source:       "system",
					Version:      "1.0",
					IsPublic:     true,
					IsActive:     true,
					FormatRules:  formatRequirementsStr,
				}
				if err := tx.Create(&newTemplate).Error; err != nil {
					tx.Rollback()
					utils.ErrorResponse(c, http.StatusInternalServerError, "创建格式模板失败", err.Error())
					return
				}
			}
		}
	}

	tx.Commit()

	// 获取更新后的数据 (包含模板信息)
	var updatedUniversity model.University
	if err := database.DB.Preload("Templates").First(&updatedUniversity, "id = ?", id).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取更新后的数据失败", err.Error())
		return
	}

	// 填充虚拟字段
	if len(updatedUniversity.Templates) > 0 {
		var activeTemplate *model.FormatTemplate
		for j := range updatedUniversity.Templates {
			if updatedUniversity.Templates[j].IsActive {
				activeTemplate = &updatedUniversity.Templates[j]
				break
			}
		}
		if activeTemplate == nil {
			activeTemplate = &updatedUniversity.Templates[0]
		}
		updatedUniversity.FormatRequirements = json.RawMessage(activeTemplate.FormatRules)
		updatedUniversity.FilePath = activeTemplate.FilePath
	}

	utils.SuccessResponse(c, "更新成功", updatedUniversity)
}

// DeleteUniversity 删除高校
func (h *AdminUniversityHandler) DeleteUniversity(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的高校ID", err.Error())
		return
	}

	// 开启事务
	tx := database.DB.Begin()

	// 1. 删除关联的模板 (软删除或硬删除，这里假设硬删除或由GORM处理)
	// 如果FormatTemplate有DeletedAt，GORM会软删除。这里手动处理一下关联。
	if err := tx.Where("university_id = ?", id).Delete(&model.FormatTemplate{}).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除关联模板失败", err.Error())
		return
	}

	// 2. 删除高校
	if err := tx.Delete(&model.University{}, id).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除高校失败", err.Error())
		return
	}

	tx.Commit()

	utils.SuccessResponse(c, "删除成功", nil)
}

// BatchUpdateUniversities 批量更新高校
func (h *AdminUniversityHandler) BatchUpdateUniversities(c *gin.Context) {
	utils.ErrorResponse(c, http.StatusNotImplemented, "此功能尚未实现", "")
}

// ParseTemplate 解析上传的模板文件
func (h *AdminUniversityHandler) ParseTemplate(c *gin.Context) {
	// 1. 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请上传文件", err.Error())
		return
	}

	// 2. 检查文件类型
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".docx" {
		utils.ErrorResponse(c, http.StatusBadRequest, "不支持的文件类型", "仅支持 .docx 文件")
		return
	}

	// 3. 保存文件到临时目录
	tempDir := "temp/uploads"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建临时目录失败", err.Error())
		return
	}

	filename := fmt.Sprintf("%s_%s", uuid.New().String(), file.Filename)
	filePath := filepath.Join(tempDir, filename)

	if err := c.SaveUploadedFile(file, filePath); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "保存文件失败", err.Error())
		return
	}
	// 解析完成后删除临时文件
	defer os.Remove(filePath)

	// 4. 调用服务解析模板
	svc := service.NewTemplateParserService()
	standard, err := svc.ParseTemplateFromFile(filePath)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "解析模板失败", err.Error())
		return
	}

	// 5. 返回解析结果
	// 返回 university_info (如果有) 和 format_rules
	utils.SuccessResponse(c, "解析成功", gin.H{
		"university_name": standard.Name, // TemplateParser 可能会从页眉提取学校名称
		"format_rules":    standard,
	})
}
