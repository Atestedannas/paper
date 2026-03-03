package database

import (
	"encoding/json"
	"log"

	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

type Migration20250124UpdateCQIECFormat struct{}

func (m *Migration20250124UpdateCQIECFormat) Name() string {
	return "20250124_update_cqiec_format_requirements"
}

func (m *Migration20250124UpdateCQIECFormat) Up(tx *gorm.DB) error {
	log.Println("开始更新重庆工程学院格式要求...")

	var university model.University
	if err := tx.Where("name = ?", "重庆工程学院").First(&university).Error; err != nil {
		return gorm.ErrRecordNotFound
	}

	cqiecUniversityID := university.ID

	var existingTemplate model.FormatTemplate
	err := tx.Where("university_id = ? AND document_type = ? AND source = ?", cqiecUniversityID, "本科论文", "system").First(&existingTemplate).Error

	formatRulesJSON := m.generateFormatRules()

	if err == gorm.ErrRecordNotFound {
		log.Println("未找到现有模板，创建新模板...")
		newTemplate := model.FormatTemplate{
			TemplateID:   "cqiec_bachelor_thesis_2024",
			Name:         "重庆工程学院本科毕业论文格式标准（2024版）",
			UniversityID: &cqiecUniversityID,
			DocumentType: "本科论文",
			Subject:      "综合",
			Source:       "system",
			Version:      "2.0",
			IsPublic:     true,
			IsActive:     true,
			FormatRules:  formatRulesJSON,
			Description:  "重庆工程学院本科毕业设计（论文）格式规范（2024版）",
		}
		if err := tx.Create(&newTemplate).Error; err != nil {
			return err
		}
		log.Printf("已创建新模板，ID: %s", newTemplate.ID)
	} else if err != nil {
		return err
	} else {
		log.Println("找到现有模板，更新格式要求...")
		if err := tx.Model(&existingTemplate).Updates(map[string]interface{}{
			"name":         "重庆工程学院本科毕业论文格式标准（2024版）",
			"version":      "2.0",
			"format_rules": formatRulesJSON,
			"description":  "重庆工程学院本科毕业设计（论文）格式规范（2024版）",
		}).Error; err != nil {
			return err
		}
		log.Printf("已更新模板，ID: %s", existingTemplate.ID)
	}

	log.Println("重庆工程学院格式要求更新完成")
	return nil
}

func (m *Migration20250124UpdateCQIECFormat) Down(tx *gorm.DB) error {
	log.Println("回滚重庆工程学院格式要求更新...")
	return nil
}

func (m *Migration20250124UpdateCQIECFormat) generateFormatRules() string {
	rules := map[string]interface{}{
		"name":        "重庆工程学院本科毕业论文格式标准",
		"version":     "2.0",
		"description": "重庆工程学院本科毕业设计（论文）格式规范（2024版）",
		"page_setup": map[string]interface{}{
			"paper_size":      "A4",
			"margin_top":      2.5,
			"margin_bottom":   2.5,
			"margin_left":     2.5,
			"margin_right":    2.5,
			"header_distance": 1.6,
			"footer_distance": 2.1,
		},
		"headings": map[string]interface{}{
			"level1": map[string]interface{}{
				"name":             "一级标题（章）",
				"font_name":        "黑体",
				"font_size":        float64(16),
				"bold":             true,
				"alignment":        "center",
				"line_space":       "fixed",
				"spacing_before":   float64(24),
				"spacing_after":    float64(18),
				"line_space_value": float64(20),
			},
			"level2": map[string]interface{}{
				"name":             "二级标题（节）",
				"font_name":        "黑体",
				"font_size":        float64(15),
				"bold":             true,
				"alignment":        "left",
				"spacing_before":   float64(20),
				"spacing_after":    float64(16),
				"line_space":       "fixed",
				"line_space_value": float64(20),
			},
			"level3": map[string]interface{}{
				"name":             "三级标题（条）",
				"font_name":        "黑体",
				"font_size":        float64(14),
				"bold":             true,
				"alignment":        "left",
				"spacing_before":   float64(18),
				"spacing_after":    float64(14),
				"line_space":       "fixed",
				"line_space_value": float64(20),
				"indent_right":     float64(2),
			},
		},
		"body": map[string]interface{}{
			"font_name":         "宋体",
			"font_size":         float64(12),
			"alignment":         "justify",
			"line_space":        "fixed",
			"line_space_value":  float64(20),
			"first_line_indent": float64(2),
		},
		"table": map[string]interface{}{
			"caption": map[string]interface{}{
				"prefix":    "表",
				"font_name": "宋体",
				"font_size": float64(10.5),
			},
			"caption_position": "top",
		},
		"figure": map[string]interface{}{
			"caption": map[string]interface{}{
				"prefix":    "图",
				"font_name": "宋体",
				"font_size": float64(10.5),
			},
			"caption_position": "bottom",
		},
		"reference": map[string]interface{}{
			"standard":         "GB/T 7714",
			"font_name":        "宋体",
			"font_size":        float64(10.5),
			"line_space":       "fixed",
			"line_space_value": float64(20),
		},
		"abstract": map[string]interface{}{
			"chinese": map[string]interface{}{
				"heading":          "摘要",
				"font_name":        "宋体",
				"font_size":        float64(14),
				"bold":             true,
				"alignment":        "center",
				"line_space":       "fixed",
				"line_space_value": float64(20),
				"keywords_prefix":  "关键词：",
			},
			"english": map[string]interface{}{
				"heading":          "Abstract",
				"font_name":        "Times New Roman",
				"font_size":        float64(14),
				"bold":             true,
				"alignment":        "center",
				"line_space":       "fixed",
				"line_space_value": float64(20),
				"keywords_prefix":  "Keywords: ",
			},
		},
	}

	jsonBytes, err := json.Marshal(rules)
	if err != nil {
		log.Printf("生成格式规则JSON失败: %v", err)
		return "{}"
	}
	return string(jsonBytes)
}
