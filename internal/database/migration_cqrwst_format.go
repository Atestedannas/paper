package database

import (
	"encoding/json"
	"log"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// Migration20260327CreateCQRWSTFormat 创建重庆人文科技学院论文格式模板
type Migration20260327CreateCQRWSTFormat struct{}

func (m *Migration20260327CreateCQRWSTFormat) Name() string {
	return "20260327_create_cqrwst_format_template"
}

func (m *Migration20260327CreateCQRWSTFormat) Up(tx *gorm.DB) error {
	log.Println("开始创建重庆人文科技学院格式模板...")

	var university model.University
	err := tx.Where("name = ?", "重庆人文科技学院").First(&university).Error
	if err != nil {
		university = model.University{
			Name:        "重庆人文科技学院",
			Abbr:        "CQRWST",
			Description: "重庆人文科技学院本科毕业论文格式规范",
			Tags:        `["人文","科技","重庆","本科"]`,
		}
		if err := tx.Create(&university).Error; err != nil {
			return err
		}
		log.Printf("已创建高校记录：重庆人文科技学院 (ID: %d)", university.ID)
	} else {
		log.Printf("高校记录已存在：重庆人文科技学院 (ID: %d)", university.ID)
	}

	formatRules := m.buildFormatRules()

	var existing model.FormatTemplate
	err = tx.Where("university_id = ? AND document_type = ? AND source = 'system'",
		university.ID, "本科论文").First(&existing).Error

	if err == nil {
		result := tx.Model(&existing).Updates(map[string]interface{}{
			"format_rules": formatRules,
			"version":      "1.0",
			"description":  "重庆人文科技学院本科毕业论文格式规范",
		})
		if result.Error != nil {
			return result.Error
		}
		log.Printf("已更新重庆人文科技学院格式模板 (ID: %s)", existing.ID)
		return nil
	}

	template := model.FormatTemplate{
		ID:           uuid.New(),
		TemplateID:   uuid.New().String(),
		Name:         "重庆人文科技学院本科论文格式标准",
		UniversityID: &university.ID,
		DocumentType: "本科论文",
		Subject:      "综合",
		Source:       "system",
		Version:      "1.0",
		IsPublic:     true,
		IsActive:     true,
		FormatRules:  formatRules,
		Description:  "重庆人文科技学院本科毕业论文格式规范",
	}
	if err := tx.Create(&template).Error; err != nil {
		return err
	}
	log.Printf("已创建重庆人文科技学院格式模板 (ID: %s)", template.ID)
	return nil
}

func (m *Migration20260327CreateCQRWSTFormat) Down(tx *gorm.DB) error {
	var university model.University
	if err := tx.Where("name = ?", "重庆人文科技学院").First(&university).Error; err != nil {
		return nil
	}
	return tx.Where("university_id = ? AND source = 'system'", university.ID).
		Delete(&model.FormatTemplate{}).Error
}

func (m *Migration20260327CreateCQRWSTFormat) buildFormatRules() string {
	return buildCQRWSTFormatRulesJSON()
}

// buildCQRWSTFormatRulesJSON 构建重庆人文科技学院完整格式规则（V3 基于模板文件分析校正版）
func buildCQRWSTFormatRulesJSON() string {
	rules := map[string]interface{}{
		"name":        "重庆人文科技学院本科毕业论文格式规范",
		"version":     "5.0",
		"description": "重庆人文科技学院本科毕业论文（设计）格式要求（V5 修复段落分类+加粗清除）",

		"page_setup": map[string]interface{}{
			"paper_size":    "A4",
			"margin_top":    2.5,
			"margin_bottom": 2.0,
			"margin_left":   2.5,
			"margin_right":  2.0,
			"header":        1.5,
			"footer":        1.75,
			"orientation":   "portrait",
		},

		"header": map[string]interface{}{
			"content":      "重庆人文科技学院本科毕业论文（设计）",
			"font_name":    "宋体",
			"font_size":    "小五",
			"font_size_pt": float64(9),
			"alignment":    "center",
			"border_style": "0.5磅双线",
			"start_from":   "摘要页",
			"end_at":       "最后一页",
		},

		"page_number": map[string]interface{}{
			"position":      "bottom_center",
			"font_name":     "宋体",
			"font_size":     "小五",
			"font_size_pt":  float64(9),
			"format":        "第×页 共×页",
			"start_from":    "正文页",
			"abstract_page": "roman",
			"toc_page":      "none",
		},

		// FIX #4: 论文正标题（从模板文件确认：黑体三号加粗居中）
		"title": map[string]interface{}{
			"font_name":        "黑体",
			"font_size":        "三号",
			"font_size_pt":     float64(16),
			"bold":             true,
			"alignment":        "center",
			"paragraph_before": "1行",
			"subtitle": map[string]interface{}{
				"font_name":       "黑体",
				"font_size":       "小三",
				"font_size_pt":    float64(15),
				"bold":            true,
				"alignment":       "center",
				"paragraph_after": "2行",
				"line_space":      "single",
			},
		},

		"headings": map[string]interface{}{
			"level1": map[string]interface{}{
				"example":          "1",
				"font_name":        "宋体",
				"font_size":        "三号",
				"font_size_pt":     float64(16),
				"bold":             true,
				"alignment":        "left",
				"line_space":       1.5,
				"paragraph_before": "1行",
				"paragraph_after":  "1行",
			},
			"level2": map[string]interface{}{
				"example":      "1.1",
				"font_name":    "宋体",
				"font_size":    "小三",
				"font_size_pt": float64(15),
				"bold":         true,
				"alignment":    "left",
				"line_space":   1.5,
			},
			"level3": map[string]interface{}{
				"example":      "1.1.1",
				"font_name":    "宋体",
				"font_size":    "四号",
				"font_size_pt": float64(14),
				"bold":         true,
				"alignment":    "left",
				"line_space":   1.5,
			},
			"level4": map[string]interface{}{
				"example":      "1.1.1.1",
				"font_name":    "宋体",
				"font_size":    "四号",
				"font_size_pt": float64(14),
				"bold":         false,
				"alignment":    "left",
				"line_space":   1.5,
			},
		},

		"abstract": map[string]interface{}{
			"label": map[string]interface{}{
				"text":              "摘要：",
				"font_name":         "黑体",
				"font_size":         "小三",
				"font_size_pt":      float64(15),
				"bold":              true,
				"first_line_indent": "2字符",
			},
			"content": map[string]interface{}{
				"font_name":         "宋体",
				"font_size":         "小四",
				"font_size_pt":      float64(12),
				"bold":              false,
				"alignment":         "justify",
				"first_line_indent": "2字符",
				"line_space":        1.5,
				"paragraph_after":   "2行",
				"word_count":        "300-500字",
			},
		},

		"keywords": map[string]interface{}{
			"label": map[string]interface{}{
				"text":              "关键词：",
				"font_name":         "黑体",
				"font_size":         "小三",
				"font_size_pt":      float64(15),
				"bold":              true,
				"first_line_indent": "2字符",
				"line_space":        1.5,
				"paragraph_after":   "2行",
			},
			"content": map[string]interface{}{
				"font_name":    "宋体",
				"font_size":    "小四",
				"font_size_pt": float64(12),
				"bold":         false,
				"separator":    "；",
				"count":        "3-5个",
			},
		},

		"english_abstract": map[string]interface{}{
			"label": map[string]interface{}{
				"text":              "Abstract：",
				"font_name":         "Times New Roman",
				"font_size":         "小三",
				"font_size_pt":      float64(15),
				"bold":              true,
				"first_line_indent": "2字符",
			},
			"content": map[string]interface{}{
				"font_name":       "Times New Roman",
				"font_size":       "小四",
				"font_size_pt":    float64(12),
				"bold":            false,
				"alignment":       "justify",
				"line_space":      1.5,
				"paragraph_after": "2行",
			},
			"keywords": map[string]interface{}{
				"label": map[string]interface{}{
					"text":              "Key words：",
					"font_name":         "Times New Roman",
					"font_size":         "小三",
					"font_size_pt":      float64(15),
					"bold":              true,
					"first_line_indent": "2字符",
					"line_space":        1.5,
				},
				"content": map[string]interface{}{
					"font_name":       "Times New Roman",
					"font_size":       "小四",
					"font_size_pt":    float64(12),
					"bold":            false,
					"separator":       ",  ",
					"capitalize_each": true,
				},
			},
		},

		"body": map[string]interface{}{
			"font_name":         "宋体",
			"font_name_latin":   "Times New Roman",
			"font_size":         "小四",
			"font_size_pt":      float64(12),
			"bold":              false,
			"alignment":         "justify",
			"line_space":        1.5,
			"first_line_indent": "2字符",
			"note": map[string]interface{}{
				"style":        "上标带圈数字",
				"example":      "①②③",
				"font_name":    "宋体",
				"font_size":    "小四",
				"font_size_pt": float64(12),
			},
			"citation": map[string]interface{}{
				"style":        "上标方括号",
				"example":      "[1][2]",
				"font_name":    "宋体",
				"font_size":    "小四",
				"font_size_pt": float64(12),
			},
			"case_text": map[string]interface{}{
				"font_name":    "楷体",
				"font_size":    "小四",
				"font_size_pt": float64(12),
				"description":  "案例/访谈原文",
			},
		},

		"figure": map[string]interface{}{
			"numbering":        "分章编号",
			"example":          "图1.1",
			"caption_position": "bottom",
			"caption": map[string]interface{}{
				"font_name":    "宋体",
				"font_size":    "五号",
				"font_size_pt": float64(10.5),
				"alignment":    "center",
				"format":       "图序+空1格+图名",
			},
			"inner_text": map[string]interface{}{
				"font_name":    "宋体",
				"font_size":    "五号",
				"font_size_pt": float64(10.5),
			},
			"spacing": "与前后正文空1行",
		},

		"table": map[string]interface{}{
			"numbering":        "分章编号",
			"example":          "表2.1",
			"caption_position": "top",
			"caption": map[string]interface{}{
				"font_name":    "宋体",
				"font_size":    "五号",
				"font_size_pt": float64(10.5),
				"alignment":    "center",
				"format":       "表序+空1格+表名",
			},
			"inner_text": map[string]interface{}{
				"font_name":    "宋体",
				"font_size":    "五号",
				"font_size_pt": float64(10.5),
			},
			"style":         "三线表",
			"border_top":    "1.5磅",
			"border_bottom": "1.5磅",
			"border_middle": "1磅",
			"cross_page":    "加续表，表头前标注续字",
		},

		"formula": map[string]interface{}{
			"editor":           "公式编辑器（英文状态）",
			"alignment":        "center",
			"number_alignment": "right",
			"numbering":        "分章编号",
			"example":          "(4-2)",
		},

		// FIX #3: 参考文献标签补全（从模板文件确认：另页起，黑体小三加粗居中，段后2行）
		"references": map[string]interface{}{
			"label": map[string]interface{}{
				"text":            "参考文献",
				"font_name":       "黑体",
				"font_size":       "小三",
				"font_size_pt":    float64(15),
				"bold":            true,
				"alignment":       "center",
				"paragraph_after": "2行",
				"new_page":        true,
			},
			"content": map[string]interface{}{
				"font_name":       "宋体",
				"font_name_latin": "Times New Roman",
				"font_size":       "五号",
				"font_size_pt":    float64(10.5),
				"bold":            false,
				"alignment":       "left",
				"line_space":      1.5,
				"indent":          "顶格",
				"standard":        "GB/T 7714-2015",
				"count":           "10-15篇",
				"author_rule":     "3人以上列前3人，后加',等'或',et al.'",
			},
		},

		// FIX #5: 注释（从模板文件确认：另页起，黑体小三加粗，字间空6格，居中，段后2行）
		"notes": map[string]interface{}{
			"label": map[string]interface{}{
				"text":            "注  释",
				"font_name":       "黑体",
				"font_size":       "小三",
				"font_size_pt":    float64(15),
				"bold":            true,
				"alignment":       "center",
				"char_spacing":    "6格",
				"paragraph_after": "2行",
				"new_page":        true,
			},
			"content": map[string]interface{}{
				"font_name":    "宋体",
				"font_size":    "五号",
				"font_size_pt": float64(10.5),
				"bold":         false,
				"line_space":   1.5,
				"indent":       "顶格",
				"marker_style": "带圈数字",
				"marker_space": "1字符",
			},
		},

		// FIX #6: 附录补全内容格式（从模板文件确认：宋体五号1.5倍行距首行缩进2字符）
		"appendix": map[string]interface{}{
			"label": map[string]interface{}{
				"font_name":       "黑体",
				"font_size":       "小三",
				"font_size_pt":    float64(15),
				"bold":            true,
				"alignment":       "center",
				"paragraph_after": "2行",
				"new_page":        true,
				"format":          "附录序码+空2格+附录题目",
			},
			"content": map[string]interface{}{
				"font_name":         "宋体",
				"font_size":         "五号",
				"font_size_pt":      float64(10.5),
				"bold":              false,
				"line_space":        1.5,
				"first_line_indent": "2字符",
			},
			"example":        "附录A 附录题目",
			"page_break":     true,
			"figure_prefix":  "图A1",
			"table_prefix":   "表B2",
			"formula_prefix": "式(B3)",
		},

		"table_of_contents": map[string]interface{}{
			"title": map[string]interface{}{
				"text":            "目  录",
				"font_name":       "黑体",
				"font_size":       "小三",
				"font_size_pt":    float64(15),
				"bold":            true,
				"alignment":       "center",
				"paragraph_after": "1行",
			},
			"level1": map[string]interface{}{
				"font_name":    "黑体",
				"font_size":    "小四",
				"font_size_pt": float64(12),
				"bold":         true,
				"alignment":    "left",
				"line_space":   1.5,
			},
			"level2": map[string]interface{}{
				"font_name":         "宋体",
				"font_size":         "小四",
				"font_size_pt":      float64(12),
				"bold":              false,
				"alignment":         "left",
				"first_line_indent": "2字符",
				"line_space":        1.5,
			},
			"level3": map[string]interface{}{
				"font_name":         "宋体",
				"font_size":         "小四",
				"font_size_pt":      float64(12),
				"bold":              false,
				"alignment":         "left",
				"first_line_indent": "4字符",
				"line_space":        1.5,
			},
		},

		"cover": map[string]interface{}{
			"separate_page": true,
			"no_adjustment": true,
		},

		// FIX #1 & #2: 致谢（内容字号从小四改为五号，补全标签格式）
		"acknowledgements": map[string]interface{}{
			"label": map[string]interface{}{
				"text":            "致  谢",
				"font_name":       "黑体",
				"font_size":       "小三",
				"font_size_pt":    float64(15),
				"bold":            true,
				"alignment":       "center",
				"char_spacing":    "6格",
				"paragraph_after": "2行",
				"new_page":        true,
			},
			"content": map[string]interface{}{
				"font_name":         "宋体",
				"font_size":         "五号",
				"font_size_pt":      float64(10.5),
				"bold":              false,
				"alignment":         "justify",
				"first_line_indent": "2字符",
				"line_space":        1.5,
			},
		},

		// 打印规范
		"printing": map[string]interface{}{
			"cover":   "单独一页打印",
			"duplex":  "摘要、目录、正文、注释、参考文献、致谢、附录页均分开双面打印",
			"color":   "封面及彩色图片页需要彩打",
		},
	}

	b, err := json.Marshal(rules)
	if err != nil {
		log.Printf("生成重庆人文科技学院格式规则JSON失败: %v", err)
		return "{}"
	}
	return string(b)
}
