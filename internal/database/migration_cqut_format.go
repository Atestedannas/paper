package database

import (
	"encoding/json"
	"log"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// Migration20260315CreateCQUTFormat 创建重庆理工大学论文格式模板
type Migration20260315CreateCQUTFormat struct{}

func (m *Migration20260315CreateCQUTFormat) Name() string {
	return "20260315_create_cqut_format_template"
}

func (m *Migration20260315CreateCQUTFormat) Up(tx *gorm.DB) error {
	log.Println("开始创建重庆理工大学格式模板...")

	// 查找或创建高校记录
	var university model.University
	err := tx.Where("name = ?", "重庆理工大学").First(&university).Error
	if err != nil {
		university = model.University{
			Name:        "重庆理工大学",
			Abbr:        "CQUT",
			Description: "重庆理工大学本科毕业论文格式规范",
			Tags:        `["理工", "重庆", "本科"]`,
		}
		if err := tx.Create(&university).Error; err != nil {
			return err
		}
		log.Printf("已创建高校记录：重庆理工大学 (ID: %d)", university.ID)
	} else {
		log.Printf("高校记录已存在：重庆理工大学 (ID: %d)", university.ID)
	}

	formatRules := m.buildFormatRules()

	// 查找是否已存在本科论文模板
	var existing model.FormatTemplate
	err = tx.Where("university_id = ? AND document_type = ? AND source = 'system'",
		university.ID, "本科论文").First(&existing).Error

	if err == nil {
		// 已存在，更新
		result := tx.Model(&existing).Updates(map[string]interface{}{
			"format_rules": formatRules,
			"version":      "1.0",
			"description":  "重庆理工大学本科毕业论文格式规范",
		})
		if result.Error != nil {
			return result.Error
		}
		log.Printf("已更新重庆理工大学格式模板 (ID: %s)", existing.ID)
		return nil
	}

	// 不存在，创建
	template := model.FormatTemplate{
		ID:           uuid.New(),
		TemplateID:   uuid.New().String(),
		Name:         "重庆理工大学本科论文格式标准",
		UniversityID: &university.ID,
		DocumentType: "本科论文",
		Subject:      "综合",
		Source:       "system",
		Version:      "1.0",
		IsPublic:     true,
		IsActive:     true,
		FormatRules:  formatRules,
		Description:  "重庆理工大学本科毕业论文格式规范",
	}
	if err := tx.Create(&template).Error; err != nil {
		return err
	}
	log.Printf("已创建重庆理工大学格式模板 (ID: %s)", template.ID)
	return nil
}

func (m *Migration20260315CreateCQUTFormat) Down(tx *gorm.DB) error {
	var university model.University
	if err := tx.Where("name = ?", "重庆理工大学").First(&university).Error; err != nil {
		return nil
	}
	return tx.Where("university_id = ? AND source = 'system'", university.ID).
		Delete(&model.FormatTemplate{}).Error
}

// buildFormatRules 生成重庆理工大学格式规则 JSON
// 严格对齐 EnhancedProcessor.applyPreciseFormatting 期待的字段路径
func (m *Migration20260315CreateCQUTFormat) buildFormatRules() string {
	rules := map[string]interface{}{
		"name":        "重庆理工大学本科毕业论文格式规范",
		"version":     "1.0",
		"description": "重庆理工大学本科毕业论文格式要求",

		// ── 页面设置 ─────────────────────────────────────────────────
		"page_setup": map[string]interface{}{
			"paper_size":    "A4",
			"margin_top":    2.54,
			"margin_bottom": 2.54,
			"margin_left":   3.17,
			"margin_right":  3.17,
			"orientation":   "portrait",
		},

		// ── 论文主标题（中文）────────────────────────────────────────
		// 摘要页的中文题目：三号黑体居中，上下各空一行
		"title": map[string]interface{}{
			"font_name":   "黑体",
			"font_size":   "三号", // 16pt
			"font_size_pt": 16,
			"bold":        true,
			"alignment":   "center",
			"space_before": 1,
			"space_after":  1,
		},

		// ── 中文摘要 ─────────────────────────────────────────────────
		// label  → "摘 要" 标题行：三号黑体居中，与内容间空一行
		// content → 摘要正文：小四号宋体，首行缩进两字
		"abstract": map[string]interface{}{
			"label": map[string]interface{}{
				"text":        "摘 要",
				"font_name":   "黑体",
				"font_size":   "三号", // 16pt
				"font_size_pt": 16,
				"bold":        true,
				"alignment":   "center",
				"space_after":  1,
			},
			"content": map[string]interface{}{
				"font_name":         "宋体",
				"font_size":         "小四", // 12pt
				"font_size_pt":       12,
				"alignment":         "justify",
				"first_line_indent": "2",
				"line_space":        "single",
			},
		},

		// ── 中文关键词 ───────────────────────────────────────────────
		// label  → "关键词" 三字：小四号宋体加粗
		// content → 具体关键词：小四号宋体
		"keywords": map[string]interface{}{
			"label": map[string]interface{}{
				"font_name":   "宋体",
				"font_size":   "小四", // 12pt
				"font_size_pt": 12,
				"bold":        true,
			},
			"content": map[string]interface{}{
				"font_name":   "宋体",
				"font_size":   "小四", // 12pt
				"font_size_pt": 12,
				"bold":        false,
				"separator":   " ",
				"min_count":   3,
				"max_count":   5,
			},
		},

		// ── 英文摘要（Abstract）──────────────────────────────────────
		// label  → "Abstract"：三号Times New Roman，居中，与内容间空一行
		// content → 英文摘要正文：小四号Times New Roman
		// keywords → "Key words" 标签加粗，内容正常
		"english_abstract": map[string]interface{}{
			"label": map[string]interface{}{
				"text":        "Abstract",
				"font_name":   "Times New Roman",
				"font_size":   "三号", // 16pt
				"font_size_pt": 16,
				"bold":        true,
				"alignment":   "center",
				"space_after":  1,
			},
			"content": map[string]interface{}{
				"font_name":   "Times New Roman",
				"font_size":   "小四", // 12pt
				"font_size_pt": 12,
				"alignment":   "justify",
				"line_space":  "single",
			},
			"keywords": map[string]interface{}{
				"label": map[string]interface{}{
					"text":      "Key words",
					"font_name": "Times New Roman",
					"font_size": "小四", // 12pt
					"bold":      true,
				},
				"content": map[string]interface{}{
					"font_name": "Times New Roman",
					"font_size": "小四", // 12pt
					"bold":      false,
				},
			},
		},

		// ── 目录 ──────────────────────────────────────────────────────
		// 标题 "目 录"：三号黑体居中
		// 一级条目（如"摘 要"、"1 绪论"）：小四号黑体左对齐
		// 二级条目（如"2.1 ..."）：小四号宋体，缩进
		// 最多列至三级标题
		"table_of_contents": map[string]interface{}{
			"title": map[string]interface{}{
				"text":        "目 录",
				"font_name":   "黑体",
				"font_size":   "三号", // 16pt
				"font_size_pt": 16,
				"bold":        true,
				"alignment":   "center",
			},
			"level1": map[string]interface{}{
				"font_name":   "黑体",
				"font_size":   "小四", // 12pt
				"font_size_pt": 12,
				"bold":        false,
				"alignment":   "left",
				"indent":      0,
			},
			"level2": map[string]interface{}{
				"font_name":   "宋体",
				"font_size":   "小四", // 12pt
				"font_size_pt": 12,
				"bold":        false,
				"alignment":   "left",
				"indent":      2,
			},
			"level3": map[string]interface{}{
				"font_name":   "宋体",
				"font_size":   "小四", // 12pt
				"font_size_pt": 12,
				"bold":        false,
				"alignment":   "left",
				"indent":      4,
			},
			"max_level": 3,
		},

		// ── 正文 ─────────────────────────────────────────────────────
		// 正文段落：小四号宋体，首行缩进两字
		// 引用：行内右上角方括号数字 [1]
		"body": map[string]interface{}{
			"font_name":         "宋体",
			"font_name_latin":   "Times New Roman",
			"font_size":         "小四", // 12pt
			"font_size_pt":       12,
			"alignment":         "justify",
			"line_space":        "single",
			"first_line_indent": "2",
			"citation_style":    "superscript_bracket",
		},

		// ── 各级正文标题 ─────────────────────────────────────────────
		// 一级标题（如"2 住房市场分析"）：三号黑体居中
		// 二级标题（如"2.1 住房市场概述"）：小三号黑体
		// 三级标题（如"2.1.1 住房概念"）：四号黑体
		"headings": map[string]interface{}{
			"level1": map[string]interface{}{
				"font_name":   "黑体",
				"font_size":   "三号", // 16pt
				"font_size_pt": 16,
				"bold":        true,
				"alignment":   "center",
			},
			"level2": map[string]interface{}{
				"font_name":   "黑体",
				"font_size":   "小三", // 15pt
				"font_size_pt": 15,
				"bold":        true,
				"alignment":   "left",
			},
			"level3": map[string]interface{}{
				"font_name":   "黑体",
				"font_size":   "四号", // 14pt
				"font_size_pt": 14,
				"bold":        true,
				"alignment":   "left",
			},
		},

		// ── 参考文献 ─────────────────────────────────────────────────
		// 标题 "参考文献"：三号黑体居中，与内容间空一行
		// 正文（中文）：小四号宋体，顶左书写
		// 正文（英文）：小四号Times New Roman，顶左书写
		// 著录格式：GB/T 7714-2015
		"references": map[string]interface{}{
			"label": map[string]interface{}{
				"text":        "参考文献",
				"font_name":   "黑体",
				"font_size":   "三号", // 16pt
				"font_size_pt": 16,
				"bold":        true,
				"alignment":   "center",
				"space_after":  1,
			},
			"content": map[string]interface{}{
				"font_name":       "宋体",
				"font_name_latin": "Times New Roman",
				"font_size":       "小四", // 12pt
				"font_size_pt":     12,
				"alignment":       "left",
				"indent":          0,
				"standard":        "GB/T 7714-2015",
			},
			// 各文献类型著录格式示例
			"formats": map[string]interface{}{
				"journal":    "[序号] 作者(前三名). 题名[J]. 刊名, 年, 卷(期): 起止页.",
				"book":       "[序号] 作者(前三名). 题名[M]. 出版地: 出版社, 年.",
				"conference": "[序号] 作者(前三名). 题名[C]. 出版地: 出版社, 年: 起止页.",
				"thesis":     "[序号] 作者. 题名[D]. 保存地: 保存单位, 年.",
				"electronic": "[序号] 作者(前三名). 题名[类别/载体]. (修改日期)[引用日期]. 获取路径.",
			},
		},
	}

	b, err := json.Marshal(rules)
	if err != nil {
		log.Printf("生成重庆理工大学格式规则JSON失败: %v", err)
		return "{}"
	}
	return string(b)
}
