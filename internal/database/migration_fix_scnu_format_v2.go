package database

import (
	"encoding/json"
	"log"

	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// Migration20260313FixSCNUFormatV2 将四川师范大学格式规则更新为 EnhancedProcessor 期待的统一结构
type Migration20260313FixSCNUFormatV2 struct{}

func (m *Migration20260313FixSCNUFormatV2) Name() string {
	return "20260313_fix_scnu_format_template_v3"
}

func (m *Migration20260313FixSCNUFormatV2) Up(tx *gorm.DB) error {
	log.Println("开始创建/更新四川师范大学格式模板（v2 统一结构）...")

	// 查找学校（不存在时先创建）
	var university model.University
	if err := tx.Where("name = ?", "四川师范大学").First(&university).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			university = model.University{
				Name:        "四川师范大学",
				Abbr:        "SCNU",
				Description: "四川师范大学",
				Tags:        `["本科院校","师范类"]`,
				Color:       "#e6162d",
			}
			if createErr := tx.Create(&university).Error; createErr != nil {
				log.Printf("创建四川师范大学失败: %v", createErr)
				return nil
			}
			log.Printf("已创建学校：四川师范大学，ID=%d", university.ID)
		} else {
			log.Printf("查询四川师范大学失败，跳过: %v", err)
			return nil
		}
	}

	formatRules := m.buildFormatRules()

	// 先尝试更新已有模板
	result := tx.Model(&model.FormatTemplate{}).
		Where("university_id = ? AND source = 'system'", university.ID).
		Updates(map[string]interface{}{
			"format_rules": formatRules,
			"version":      "2.0",
			"description":  "四川师范大学本科毕业论文格式规范（v2，含完整英文摘要规则）",
		})
	if result.Error != nil {
		log.Printf("更新四川师范大学模板失败: %v", result.Error)
		return nil
	}

	if result.RowsAffected > 0 {
		log.Printf("已更新 %d 个四川师范大学格式模板", result.RowsAffected)
		return nil
	}

	// 无现有模板时用原始 SQL 创建（绕过 GORM template_id UUID 问题）
	log.Println("未找到现有模板，使用原始 SQL 创建...")
	insertErr := tx.Exec(`
		INSERT INTO format_templates
			(id, template_id, name, university_id, document_type, subject,
			 file_path, source, version, is_public, is_active, format_rules,
			 parse_confidence, usage_count, success_rate, description, created_at, updated_at)
		VALUES
			(gen_random_uuid(), gen_random_uuid(), $1, $2, '本科论文', '综合',
			 '', 'system', '2.0', true, true, $3,
			 0.0, 0, 0.0, $4, NOW(), NOW())
		ON CONFLICT DO NOTHING
	`,
		"四川师范大学本科毕业论文格式规范",
		university.ID,
		formatRules,
		"四川师范大学本科毕业论文格式规范（v2，含完整英文摘要规则）",
	).Error
	if insertErr != nil {
		log.Printf("创建四川师范大学格式模板失败（非致命）: %v", insertErr)
	} else {
		log.Println("已成功创建四川师范大学格式模板（v2）")
	}
	return nil
}

func (m *Migration20260313FixSCNUFormatV2) Down(tx *gorm.DB) error {
	return nil
}

// buildFormatRules 生成统一结构的格式规则 JSON
// 结构与 EnhancedProcessor.applyPreciseFormatting 期待的路径完全对齐：
//
//	page_setup / title / abstract.label+content / keywords.label+content
//	english_abstract.label+content+keywords / body / headings.level1~3
//	references.label+content
func (m *Migration20260313FixSCNUFormatV2) buildFormatRules() string {
	rules := map[string]interface{}{
		"name":        "四川师范大学本科毕业论文格式规范",
		"version":     "2.0",
		"description": "四川师范大学本科毕业论文格式规范（v2）",

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
		"title": map[string]interface{}{
			"font_name": "黑体",
			"font_size": "三号", // 16pt
			"bold":      true,
			"alignment": "center",
		},

		// ── 中文摘要页 ───────────────────────────────────────────────
		// abstract.label  → "摘要" / "内容摘要" 标题行
		// abstract.content → 摘要正文段落
		"abstract": map[string]interface{}{
			"label": map[string]interface{}{
				"font_name": "黑体",
				"font_size": "四号", // 14pt
				"bold":      true,
				"alignment": "center",
			},
			"content": map[string]interface{}{
				"font_name":         "宋体",
				"font_size":         "小四", // 12pt
				"alignment":         "justify",
				"first_line_indent": "2",
				"line_space":        "1.5",
			},
		},

		// ── 中文关键词 ───────────────────────────────────────────────
		"keywords": map[string]interface{}{
			"label": map[string]interface{}{
				"font_name": "黑体",
				"font_size": "四号", // 14pt
				"bold":      true,
			},
			"content": map[string]interface{}{
				"font_name": "宋体",
				"font_size": "小四", // 12pt
			},
		},

		// ── 英文摘要页（四川师范大学规范）───────────────────────────
		// 英文题目：三号加粗，Arial，居中，上下各空一行
		// "Abstract" 标签：四号黑体，居中
		// 摘要内容：小四，Arial
		// "Key words" 标签：四号
		// 关键词内容：小四，Arial
		"english_abstract": map[string]interface{}{
			"label": map[string]interface{}{
				"font_name":    "Arial",
				"font_name_cn": "黑体",
				"font_size":    "四号", // 14pt，黑体加粗
				"bold":         true,
				"alignment":    "center",
			},
			"content": map[string]interface{}{
				"font_name": "Arial",
				"font_size": "小四", // 12pt
				"alignment": "justify",
			},
			"keywords": map[string]interface{}{
				"font_name": "Arial",
				"font_size": "四号", // 14pt
			},
		},

		// ── 正文 ─────────────────────────────────────────────────────
		"body": map[string]interface{}{
			"font_name":         "宋体",
			"font_name_latin":   "Times New Roman", // 正文英文/数字用 Times New Roman
			"font_size":         "小四",             // 12pt
			"alignment":         "justify",
			"line_space":        "1.5",
			"first_line_indent": "2",
		},

		// ── 各级标题 ─────────────────────────────────────────────────
		"headings": map[string]interface{}{
			"level1": map[string]interface{}{
				"font_name": "黑体",
				"font_size": "四号", // 14pt
				"bold":      false,
				"alignment": "left",
			},
			"level2": map[string]interface{}{
				"font_name": "宋体",
				"font_size": "小四", // 12pt
				"bold":      false,
				"alignment": "left",
			},
			"level3": map[string]interface{}{
				"font_name": "宋体",
				"font_size": "小四", // 12pt
				"bold":      false,
				"alignment": "left",
			},
		},

		// ── 参考文献 ─────────────────────────────────────────────────
		"references": map[string]interface{}{
			"label": map[string]interface{}{
				"font_name": "黑体",
				"font_size": "四号", // 14pt
				"bold":      true,
				"alignment": "center",
			},
			"content": map[string]interface{}{
				"font_name": "宋体",
				"font_size": "小四", // 12pt
			},
		},
	}

	b, err := json.Marshal(rules)
	if err != nil {
		log.Printf("生成四川师范大学v2格式规则JSON失败: %v", err)
		return "{}"
	}
	return string(b)
}
