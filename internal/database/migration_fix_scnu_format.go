package database

import (
	"encoding/json"
	"log"

	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// Migration20260312FixSCNUFormat 修正/创建四川师范大学格式模板
type Migration20260312FixSCNUFormat struct{}

func (m *Migration20260312FixSCNUFormat) Name() string {
	return "20260312_fix_scnu_format_template"
}

func (m *Migration20260312FixSCNUFormat) Up(tx *gorm.DB) error {
	log.Println("开始创建/更新四川师范大学格式模板...")

	// 查找四川师范大学
	var university model.University
	if err := tx.Where("name = ?", "四川师范大学").First(&university).Error; err != nil {
		// 学校不存在则先创建
		if err == gorm.ErrRecordNotFound {
			university = model.University{
				Name:        "四川师范大学",
				Abbr:        "SCNU",
				Description: "四川师范大学",
				Tags:        `["本科院校","师范类"]`,
				Color:       "#e6162d",
			}
			if err := tx.Create(&university).Error; err != nil {
				return err
			}
			log.Printf("已创建学校：四川师范大学，ID=%d", university.ID)
		} else {
			return err
		}
	}

	formatRules := m.buildFormatRules()

	var existing model.FormatTemplate
	err := tx.Where("university_id = ? AND document_type = ? AND source = ?",
		university.ID, "本科论文", "system").First(&existing).Error

	if err == gorm.ErrRecordNotFound {
		tpl := model.FormatTemplate{
			TemplateID:   "scnu_bachelor_thesis_std",
			Name:         "四川师范大学本科毕业论文格式规范",
			UniversityID: &university.ID,
			DocumentType: "本科论文",
			Subject:      "综合",
			Source:       "system",
			Version:      "1.0",
			IsPublic:     true,
			IsActive:     true,
			FormatRules:  formatRules,
			Description:  "四川师范大学本科毕业论文格式规范（含页边距、字体、字号、标题等）",
		}
		if err := tx.Create(&tpl).Error; err != nil {
			return err
		}
		log.Printf("已创建四川师范大学格式模板，ID=%s", tpl.ID)
	} else if err != nil {
		return err
	} else {
		if err := tx.Model(&existing).Updates(map[string]interface{}{
			"name":         "四川师范大学本科毕业论文格式规范",
			"version":      "1.0",
			"format_rules": formatRules,
			"description":  "四川师范大学本科毕业论文格式规范（含页边距、字体、字号、标题等）",
		}).Error; err != nil {
			return err
		}
		log.Printf("已更新四川师范大学格式模板，ID=%s", existing.ID)
	}

	log.Println("四川师范大学格式模板处理完成")
	return nil
}

func (m *Migration20260312FixSCNUFormat) Down(tx *gorm.DB) error {
	return nil
}

func (m *Migration20260312FixSCNUFormat) buildFormatRules() string {
	rules := map[string]interface{}{
		"name":        "四川师范大学本科毕业论文格式规范",
		"version":     "1.0",
		"description": "四川师范大学本科毕业论文格式规范，使用Word默认页边距",

		// ── 页面设置 ────────────────────────────────────
		"page_setup": map[string]interface{}{
			"paper_size":    "A4",
			"paper_width":   210.0, // mm
			"paper_height":  297.0, // mm
			"margin_top":    2.54,  // cm（Word 默认值）
			"margin_bottom": 2.54,
			"margin_left":   3.17, // cm（Word 默认值，注意不是3.18）
			"margin_right":  3.17,
			"layout":        "single_column", // 通栏
			"orientation":   "portrait",       // 纵向
			"binding":       "left",           // 左侧装订
		},

		// ── 页码 ──────────────────────────────────────
		"page_number": map[string]interface{}{
			"style":         "continuous", // 全文连续编号
			"single_side":   "bottom_right",
			"duplex_odd":    "bottom_right",
			"duplex_even":   "bottom_left",
		},

		// ── 封面 ──────────────────────────────────────
		"cover": map[string]interface{}{
			"header": map[string]interface{}{
				"font_name": "楷体",
				"font_size": float64(22), // 2号
				"bold":      true,
				"alignment": "center",
				"note":      "上下各空两行",
			},
			"title": map[string]interface{}{
				"font_name": "黑体",
				"font_size": float64(22), // 2号
				"alignment": "center",
			},
			"subtitle": map[string]interface{}{
				"font_name": "黑体",
				"font_size": float64(18), // 小2号
				"alignment": "center",
				"prefix":    "——",
			},
			"student_info_label": map[string]interface{}{
				"font_name": "楷体",
				"font_size": float64(15), // 小3号
				"bold":      true,
			},
			"student_info_value": map[string]interface{}{
				"font_name": "宋体",
				"font_size": float64(15), // 小3号
				"bold":      true,
			},
		},

		// ── 摘要页 ────────────────────────────────────
		"abstract": map[string]interface{}{
			"chinese": map[string]interface{}{
				"title": map[string]interface{}{
					"font_name": "黑体",
					"font_size": float64(16), // 3号
					"alignment": "center",
					"note":      "上下各空一行",
				},
				"heading": map[string]interface{}{
					"text":      "内容摘要",
					"font_name": "黑体",
					"font_size": float64(14), // 4号
				},
				"body": map[string]interface{}{
					"font_name":         "宋体",
					"font_size":         float64(12), // 小4号
					"first_line_indent": float64(2),
					"word_count_min":    300,
					"word_count_max":    500,
				},
				"keywords_label": map[string]interface{}{
					"text":      "关键词：",
					"font_name": "黑体",
					"font_size": float64(14), // 4号
				},
				"keywords_value": map[string]interface{}{
					"font_name":   "黑体",
					"font_size":   float64(12), // 小4号
					"count_min":   3,
					"count_max":   5,
					"separator":   " ", // 词间空一格
				},
			},
		},

		// ── 目录 ──────────────────────────────────────
		"toc": map[string]interface{}{
			"heading": map[string]interface{}{
				"text":      "目录",
				"font_name": "黑体",
				"font_size": float64(16), // 3号
				"alignment": "center",
			},
			"body": map[string]interface{}{
				"font_name": "仿宋",
				"font_size": float64(12), // 小4号
			},
		},

		// ── 正文标题 ──────────────────────────────────
		"headings": map[string]interface{}{
			"level1": map[string]interface{}{
				"name":             "一级标题",
				"example":          "1",
				"font_name":        "黑体",
				"font_size":        float64(14), // 4号
				"bold":             false,
				"alignment":        "left",
				"exclusive_line":   true,
				"no_punctuation":   true, // 独占行，不加标点
				"line_space":       "single",
				"line_space_value": float64(1.0),
			},
			"level2": map[string]interface{}{
				"name":             "二级标题",
				"example":          "1.1",
				"font_name":        "宋体",
				"font_size":        float64(12), // 小4号
				"bold":             false,
				"alignment":        "left",
				"exclusive_line":   true,
				"line_space":       "single",
				"line_space_value": float64(1.0),
			},
		},

		// ── 正文 ──────────────────────────────────────
		"body": map[string]interface{}{
			"font_name":         "宋体",
			"font_size":         float64(12), // 小4号
			"alignment":         "justify",
			"line_space":        "single",   // 单倍行距
			"line_space_value":  float64(1.0),
			"first_line_indent": float64(2), // 每段空两格
		},

		// ── 脚注 ──────────────────────────────────────
		"footnote": map[string]interface{}{
			"font_name":     "宋体",
			"font_size":     float64(10.5), // 5号
			"numbering":     "per_page",   // 以页为单位排序
		},

		// ── 参考文献 ──────────────────────────────────
		"reference": map[string]interface{}{
			"heading": map[string]interface{}{
				"text":      "参考文献",
				"font_name": "黑体",
				"font_size": float64(14), // 4号
			},
			"body": map[string]interface{}{
				"font_name": "宋体",
				"font_size": float64(12), // 小4号
			},
		},

		// ── 表格 ──────────────────────────────────────
		"table": map[string]interface{}{
			"caption_position": "top",
			"caption": map[string]interface{}{
				"font_name":          "黑体",
				"font_size":          float64(10.5), // 5号
				"number_alignment":   "left",
				"title_alignment":    "center",
				"unit_alignment":     "right",
			},
			"body": map[string]interface{}{
				"font_size":   float64(10.5), // 5号
				"open_sides":  true,          // 左右不封口
			},
		},

		// ── 图 ────────────────────────────────────────
		"figure": map[string]interface{}{
			"caption_position": "bottom",
			"caption": map[string]interface{}{
				"font_name":  "黑体",
				"font_size":  float64(10.5), // 5号
				"alignment":  "center",
			},
		},

		// ── 公式 ──────────────────────────────────────
		"formula": map[string]interface{}{
			"alignment":        "center",
			"number_format":    "(1)",
			"number_alignment": "right",
		},
	}

	b, err := json.Marshal(rules)
	if err != nil {
		log.Printf("生成四川师范大学格式规则JSON失败: %v", err)
		return "{}"
	}
	return string(b)
}
