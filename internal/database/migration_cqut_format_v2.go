package database

import (
	"encoding/json"
	"log"

	"gorm.io/gorm"
)

// Migration20260315FixCQUTFormatV2 强制更新重庆理工大学所有格式模板的规则
// 解决：DB 中已有模板的 abstract/english_abstract/keywords 等规则数据不正确
type Migration20260315FixCQUTFormatV2 struct{}

func (m *Migration20260315FixCQUTFormatV2) Name() string {
	return "20260315_fix_cqut_format_v2"
}

func (m *Migration20260315FixCQUTFormatV2) Up(tx *gorm.DB) error {
	log.Println("[CQUT-V2] 开始强制更新重庆理工大学所有格式模板...")

	formatRules := buildCQUTFormatRulesV2()

	// 直接更新该大学所有模板（不限 source）
	result := tx.Exec(`
		UPDATE format_templates 
		SET format_rules = ?
		WHERE university_id IN (
			SELECT id FROM universities WHERE name = ?
		)
	`, formatRules, "重庆理工大学")

	if result.Error != nil {
		log.Printf("[CQUT-V2] ❌ 更新失败: %v", result.Error)
		return result.Error
	}

	log.Printf("[CQUT-V2] ✅ 已更新 %d 个模板", result.RowsAffected)

	// 同时更新重庆工程学院（名字类似，可能也有问题）
	result2 := tx.Exec(`
		UPDATE format_templates 
		SET format_rules = ?
		WHERE university_id IN (
			SELECT id FROM universities WHERE name = ?
		)
	`, formatRules, "重庆工程学院")

	if result2.Error == nil && result2.RowsAffected > 0 {
		log.Printf("[CQUT-V2] ✅ 同时更新重庆工程学院 %d 个模板", result2.RowsAffected)
	}

	return nil
}

func (m *Migration20260315FixCQUTFormatV2) Down(tx *gorm.DB) error {
	return nil
}

func buildCQUTFormatRulesV2() string {
	rules := map[string]interface{}{
		"name":        "重庆理工大学本科毕业论文格式规范",
		"version":     "2.0",
		"description": "重庆理工大学本科毕业论文格式要求（V2修正版）",

		"page_setup": map[string]interface{}{
			"paper_size":    "A4",
			"margin_top":    2.54,
			"margin_bottom": 2.54,
			"margin_left":   3.17,
			"margin_right":  3.17,
			"orientation":   "portrait",
		},

		"title": map[string]interface{}{
			"font_name":    "黑体",
			"font_size":    "三号",
			"font_size_pt": float64(16),
			"bold":         true,
			"alignment":    "center",
		},

		"abstract": map[string]interface{}{
			"label": map[string]interface{}{
				"text":         "摘 要",
				"font_name":    "黑体",
				"font_size":    "三号",
				"font_size_pt": float64(16),
				"bold":         true,
				"alignment":    "center",
			},
			"content": map[string]interface{}{
				"font_name":         "宋体",
				"font_size":         "小四",
				"font_size_pt":      float64(12),
				"alignment":         "justify",
				"first_line_indent": "2字符",
				"line_space":        1.5,
			},
		},

		"keywords": map[string]interface{}{
			"label": map[string]interface{}{
				"font_name":    "宋体",
				"font_size":    "小四",
				"font_size_pt": float64(12),
				"bold":         true,
			},
			"content": map[string]interface{}{
				"font_name":    "宋体",
				"font_size":    "小四",
				"font_size_pt": float64(12),
				"bold":         false,
			},
		},

		"english_abstract": map[string]interface{}{
			"label": map[string]interface{}{
				"text":         "Abstract",
				"font_name":    "Times New Roman",
				"font_size":    "三号",
				"font_size_pt": float64(16),
				"bold":         true,
				"alignment":    "center",
			},
			"content": map[string]interface{}{
				"font_name":    "Times New Roman",
				"font_size":    "小四",
				"font_size_pt": float64(12),
				"alignment":    "justify",
				"line_space":   1.5,
			},
			"keywords": map[string]interface{}{
				"label": map[string]interface{}{
					"text":      "Key words",
					"font_name": "Times New Roman",
					"font_size": "小四",
					"bold":      true,
				},
				"content": map[string]interface{}{
					"font_name": "Times New Roman",
					"font_size": "小四",
					"bold":      false,
				},
			},
		},

		"table_of_contents": map[string]interface{}{
			"title": map[string]interface{}{
				"text":         "目 录",
				"font_name":    "黑体",
				"font_size":    "三号",
				"font_size_pt": float64(16),
				"bold":         true,
				"alignment":    "center",
			},
			"level1": map[string]interface{}{
				"font_name":    "黑体",
				"font_size":    "小四",
				"font_size_pt": float64(12),
				"alignment":    "left",
			},
			"level2": map[string]interface{}{
				"font_name":         "宋体",
				"font_size":         "小四",
				"font_size_pt":      float64(12),
				"alignment":         "left",
				"first_line_indent": "2字符",
			},
			"level3": map[string]interface{}{
				"font_name":         "宋体",
				"font_size":         "小四",
				"font_size_pt":      float64(12),
				"alignment":         "left",
				"first_line_indent": "4字符",
			},
		},

		"body": map[string]interface{}{
			"font_name":         "宋体",
			"font_name_latin":   "Times New Roman",
			"font_size":         "小四",
			"font_size_pt":      float64(12),
			"alignment":         "justify",
			"line_space":        1.5,
			"first_line_indent": "2字符",
			"paragraph_space": map[string]interface{}{
				"before": "0",
				"after":  "0",
			},
		},

		"headings": map[string]interface{}{
			"level1": map[string]interface{}{
				"font_name":    "黑体",
				"font_size":    "三号",
				"font_size_pt": float64(16),
				"bold":         true,
				"alignment":    "center",
			},
			"level2": map[string]interface{}{
				"font_name":    "黑体",
				"font_size":    "小三",
				"font_size_pt": float64(15),
				"bold":         true,
				"alignment":    "left",
			},
			"level3": map[string]interface{}{
				"font_name":    "黑体",
				"font_size":    "四号",
				"font_size_pt": float64(14),
				"bold":         true,
				"alignment":    "left",
			},
		},

		"references": map[string]interface{}{
			"label": map[string]interface{}{
				"text":         "参考文献",
				"font_name":    "黑体",
				"font_size":    "三号",
				"font_size_pt": float64(16),
				"bold":         true,
				"alignment":    "center",
			},
			"content": map[string]interface{}{
				"font_name":       "宋体",
				"font_name_latin": "Times New Roman",
				"font_size":       "小四",
				"font_size_pt":    float64(12),
				"alignment":       "left",
			},
		},

		"acknowledgements": map[string]interface{}{
			"label": map[string]interface{}{
				"font_name": "黑体",
				"font_size": "三号",
				"bold":      true,
				"alignment": "center",
			},
			"content": map[string]interface{}{
				"font_name":         "宋体",
				"font_size":         "小四",
				"alignment":         "justify",
				"first_line_indent": "2字符",
			},
		},

		"cover": map[string]interface{}{},

		"english_title": map[string]interface{}{
			"font_name":    "Times New Roman",
			"font_size":    "三号",
			"font_size_pt": float64(16),
			"bold":         true,
			"alignment":    "center",
		},

		"author": map[string]interface{}{},
		"appendix": map[string]interface{}{},
	}

	b, err := json.Marshal(rules)
	if err != nil {
		log.Printf("[CQUT-V2] JSON 编码失败: %v", err)
		return "{}"
	}
	return string(b)
}
