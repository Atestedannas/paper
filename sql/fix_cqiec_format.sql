-- 修复重庆工程学院格式模板的JSON结构
-- 问题：JSON键名与解析函数不匹配，导致使用默认值（间距400磅）
-- 修复：将键名改为解析函数期望的格式

-- 更新格式规则JSON
UPDATE format_templates 
SET format_rules = '{
  "name": "重庆工程学院本科毕业论文格式标准",
  "version": "2.0",
  "description": "重庆工程学院本科毕业设计（论文）格式规范（2024版）",
  "page_setup": {
    "paper_size": "A4",
    "margin_top": 2.5,
    "margin_bottom": 2.5,
    "margin_left": 2.5,
    "margin_right": 2.5,
    "header_distance": 1.6,
    "footer_distance": 2.1
  },
  "headings": {
    "level1": {
      "name": "一级标题（章）",
      "font_name": "黑体",
      "font_size": 16,
      "bold": true,
      "alignment": "center",
      "line_space": "fixed",
      "spacing_before": 24,
      "spacing_after": 18,
      "line_space_value": 20
    },
    "level2": {
      "name": "二级标题（节）",
      "font_name": "黑体",
      "font_size": 15,
      "bold": true,
      "alignment": "left",
      "spacing_before": 20,
      "spacing_after": 16,
      "line_space": "fixed",
      "line_space_value": 20
    },
    "level3": {
      "name": "三级标题（条）",
      "font_name": "黑体",
      "font_size": 14,
      "bold": true,
      "alignment": "left",
      "spacing_before": 18,
      "spacing_after": 14,
      "line_space": "fixed",
      "line_space_value": 20,
      "indent_right": 2
    }
  },
  "body": {
    "font_name": "宋体",
    "font_size": 12,
    "alignment": "justify",
    "line_space": "fixed",
    "line_space_value": 20,
    "first_line_indent": 2
  },
  "table": {
    "caption": {
      "prefix": "表",
      "font_name": "宋体",
      "font_size": 10.5
    },
    "caption_position": "top"
  },
  "figure": {
    "caption": {
      "prefix": "图",
      "font_name": "宋体",
      "font_size": 10.5
    },
    "caption_position": "bottom"
  },
  "reference": {
    "standard": "GB/T 7714",
    "font_name": "宋体",
    "font_size": 10.5,
    "line_space": "fixed",
    "line_space_value": 20
  },
  "abstract": {
    "chinese": {
      "heading": "摘要",
      "font_name": "宋体",
      "font_size": 14,
      "bold": true,
      "alignment": "center",
      "line_space": "fixed",
      "line_space_value": 20,
      "keywords_prefix": "关键词："
    },
    "english": {
      "heading": "Abstract",
      "font_name": "Times New Roman",
      "font_size": 14,
      "bold": true,
      "alignment": "center",
      "line_space": "fixed",
      "line_space_value": 20,
      "keywords_prefix": "Keywords: "
    }
  }
}'
WHERE name LIKE '%重庆工程%' AND document_type = '本科论文';

-- 验证更新结果
SELECT 
    id,
    name,
    version,
    LEFT(format_rules::text, 300) as rules_preview,
    updated_at
FROM format_templates 
WHERE name LIKE '%重庆工程%'
ORDER BY updated_at DESC
LIMIT 1;
