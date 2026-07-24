package service

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/paper-format-checker/backend/pkg/aiclassifier"
)

const aiFormatPrompt = `你是一个学术论文格式规范解析专家。请从以下格式规范文本中提取所有格式要求，输出为严格的JSON格式。

## 输出JSON结构要求

请严格按照以下结构输出（所有字段都是可选的，只提取文本中明确提到的格式要求，没有提到的字段不要输出）：

{
  "page_setup": {
    "paper_size": "A4",
    "orientation": "portrait 或 landscape",
    "margins": {
      "top": "如 2.54cm 或 25mm",
      "bottom": "如 2.54cm",
      "left": "如 3.17cm",
      "right": "如 3.17cm"
    },
    "header": { "distance": "如 1.5cm", "content": "页眉内容", "font_name": "页眉字体", "font_size": "页眉字号" },
    "footer": { "distance": "如 1.75cm", "content": "页脚内容", "font_name": "页脚字体", "font_size": "页脚字号" },
    "page_number": { "position": "bottom_center", "format": "如 -1-", "font_name": "字体", "font_size": "字号" }
  },
  "title": {
    "font_name": "字体名称，如 黑体",
    "font_size": "字号，如 三号、小二号",
    "bold": true,
    "alignment": "center / left / right / justify",
    "line_space": "行距，如 single / 1.5 / double / 固定值20磅",
    "paragraph_space": { "before": "段前间距", "after": "段后间距" }
  },
  "author": {
    "font_name": "字体",
    "font_size": "字号",
    "bold": false,
    "alignment": "center",
    "line_space": "行距",
    "paragraph_space": { "before": "段前间距", "after": "段后间距" }
  },
  "abstract": {
    "label": {
      "text": "摘要标签文本，如 摘要：或 摘  要",
      "font_name": "字体",
      "font_size": "字号",
      "bold": true,
      "alignment": "center 或 left",
      "line_space": "行距，如 1.5 或 22磅"
    },
    "content": {
      "font_name": "字体",
      "font_size": "字号",
      "alignment": "justify",
      "line_space": "行距",
      "first_line_indent": "首行缩进，如 2字符",
      "paragraph_space": { "before": "段前间距", "after": "段后间距" }
    }
  },
  "keywords": {
    "label": {
      "text": "关键词标签，如 关键词：",
      "font_name": "字体",
      "font_size": "字号",
      "bold": true,
      "alignment": "left",
      "line_space": "行距"
    },
    "content": {
      "font_name": "字体",
      "font_size": "字号",
      "bold": false,
      "alignment": "left 或 justify",
      "line_space": "行距",
      "paragraph_space": { "before": "与摘要正文的间距，如 1行 或 12磅", "after": "段后间距" },
      "separator": "分隔符，如 ；或 ;",
      "count": "关键词数量要求，如 3-5个",
      "no_end_punctuation": true
    }
  },
  "english_title": {
    "font_name": "如 Times New Roman",
    "font_size": "字号",
    "bold": true,
    "alignment": "center",
    "line_space": "行距",
    "paragraph_space": { "before": "段前间距", "after": "段后间距" }
  },
  "english_abstract": {
    "label": {
      "text": "Abstract",
      "font_name": "字体",
      "font_size": "字号",
      "bold": true,
      "alignment": "center 或 left",
      "line_space": "行距"
    },
    "content": {
      "font_name": "字体",
      "font_size": "字号",
      "alignment": "justify",
      "line_space": "行距",
      "first_line_indent": "首行缩进",
      "paragraph_space": { "before": "段前间距", "after": "段后间距" }
    }
  },
  "english_keywords": {
    "label": {
      "text": "Key Words 或 Keywords",
      "font_name": "字体",
      "font_size": "字号",
      "bold": true,
      "alignment": "left"
    },
    "content": {
      "font_name": "字体",
      "font_size": "字号",
      "bold": false,
      "paragraph_space": { "before": "与摘要正文的间距，如 1行 或 12磅", "after": "段后间距" },
      "separator": "分隔符，如 ; 或 ,",
      "count": "关键词数量要求",
      "no_end_punctuation": true
    }
  },
  "body": {
    "font_name": "正文字体",
    "font_size": "正文字号",
    "line_space": "行距",
    "alignment": "justify",
    "first_line_indent": "首行缩进",
    "paragraph_space": { "before": "段前间距", "after": "段后间距" }
  },
  "headings": {
    "level1": {
      "font_name": "字体", "font_size": "字号", "bold": true,
      "alignment": "center 或 left", "numbering": "编号格式如 第一章 / 1 / 一、",
      "line_space": "行距",
      "paragraph_space": { "before": "段前", "after": "段后" }
    },
    "level2": {
      "font_name": "字体", "font_size": "字号", "bold": true,
      "alignment": "left", "numbering": "编号格式如 1.1 / （一）",
      "line_space": "行距",
      "paragraph_space": { "before": "段前", "after": "段后" }
    },
    "level3": {
      "font_name": "字体", "font_size": "字号", "bold": true,
      "alignment": "left", "numbering": "编号格式如 1.1.1 / (1)",
      "line_space": "行距",
      "paragraph_space": { "before": "段前", "after": "段后" }
    }
  },
  "references": {
    "title": {
      "text": "参考文献",
      "font_name": "字体", "font_size": "字号", "bold": true,
      "alignment": "center", "line_space": "行距",
      "paragraph_space": { "before": "段前", "after": "段后" }
    },
    "content": {
      "font_name": "字体", "font_size": "字号",
      "alignment": "justify 或 left",
      "numbering": "编号格式如 [1]",
      "line_space": "行距",
      "hanging_indent": "悬挂缩进",
      "paragraph_space": { "before": "段前", "after": "段后" }
    }
  },
  "table_of_contents": {
    "title": { "text": "目录", "font_name": "字体", "font_size": "字号", "bold": true, "alignment": "center", "line_space": "行距" },
    "content": { "font_name": "字体", "font_size": "字号", "alignment": "left 或 justify", "line_space": "行距", "max_level": 3 }
  },
  "figures": {
    "caption": {
      "font_name": "字体", "font_size": "字号", "bold": false,
      "alignment": "center",
      "numbering": "编号格式如 图1-1 / 图1.1 / Figure 1",
      "position": "below 或 above"
    }
  },
  "tables": {
    "caption": {
      "font_name": "字体", "font_size": "字号", "bold": false,
      "alignment": "center",
      "numbering": "编号格式如 表1-1 / 表1.1 / Table 1",
      "position": "above 或 below"
    },
    "content": {
      "font_name": "字体", "font_size": "字号",
      "alignment": "center",
      "line_space": "行距"
    }
  },
  "footnotes": {
    "font_name": "字体", "font_size": "字号",
    "line_space": "行距",
    "numbering": "编号格式如 ①②③ 或 [1][2][3]"
  },
  "cover": {
    "description": "封面格式说明"
  },
  "acknowledgements": {
    "title": {
      "text": "致谢",
      "font_name": "标题字体，如 黑体",
      "font_size": "标题字号，如 小三",
      "bold": true,
      "alignment": "center 或 left",
      "line_space": "标题行距，如 1.5",
      "paragraph_space": { "before": "段前间距", "after": "段后间距，如 1行" }
    },
    "content": {
      "font_name": "正文字体，如 宋体",
      "font_size": "正文字号，如 小四",
      "alignment": "justify",
      "line_space": "正文行距，如 22磅 或 固定值22磅",
      "first_line_indent": "首行缩进，如 2字符",
      "paragraph_space": { "before": "段前，如 0行", "after": "段后，如 0行" }
    }
  },
  "appendix": {
    "title": { "text": "附录", "font_name": "字体", "font_size": "字号", "bold": true, "alignment": "center 或 left", "line_space": "行距" },
    "content": { "font_name": "字体", "font_size": "字号", "alignment": "justify", "line_space": "行距", "first_line_indent": "首行缩进" }
  }
}

## 重要规则

1. **只输出JSON**，不要输出任何其他文字、解释或markdown标记
2. **只提取文本中明确提到的格式要求**，没有明确提到的字段不要猜测，直接省略
3. 字号使用中文表示（如"三号"、"小四号"），不要转换为磅值
4. 行距保持原始描述（如"1.5倍行距"写为"1.5"，"单倍行距"写为"single"，"固定值20磅"写为"20磅"）
5. 如果文本中有"格式同xxx"或"参照xxx"，请将对应的格式复制过来
6. 布尔值用 true/false，不要用字符串

## 格式规范文本

补充结构规则：headings 可输出 level4、level5；table_of_contents 可输出 level1 至 level4；figures 可输出 image；tables 可输出 note；公式使用顶层 formula。
同时出现中文字体和西文字体时，中文字体写入 font_name，西文字体写入 font_name_latin；颜色写入 color。仅在原文明确给出时输出，不得猜测。

%s`

// FormatAIPromptKind 传给 ParseFormatWithAI 的第二个参数，选择使用哪套 DeepSeek 提示词。
const (
	// FormatAIPromptKindFormatRules 从《格式说明/规范》类文本提取排版规则 JSON（对应 aiFormatPrompt）。
	FormatAIPromptKindFormatRules = "format_rules"
	// FormatAIPromptKindPaperSections 将学生论文全文按结构抽取为有序 segments 数组（对应 aiPaperStructurePrompt）。
	FormatAIPromptKindPaperSections = "paper_sections"
)

// PaperSectionKey* 与 aiPaperStructurePrompt 输出 JSON 的键一致，供业务侧读取。
const (
	PaperSectionKeyCover            = "cover"
	PaperSectionKeyAbstractCN       = "abstract_cn"
	PaperSectionKeyKeywordsCN       = "keywords_cn"
	PaperSectionKeyAbstractEN       = "abstract_en"
	PaperSectionKeyKeywordsEN       = "keywords_en"
	PaperSectionKeyTOC              = "toc"
	PaperSectionKeyHeading1         = "heading_1"
	PaperSectionKeyHeading2         = "heading_2"
	PaperSectionKeyHeading3         = "heading_3"
	PaperSectionKeyHeading4         = "heading_4"
	PaperSectionKeyBody             = "body"
	PaperSectionKeyReferences       = "references"
	PaperSectionKeyAcknowledgements = "acknowledgements"
)

// PaperSectionSegment 按论文阅读顺序排列的一条片段；连续多条可同属一个 key（如多段 body）。
type PaperSectionSegment struct {
	Key   string `json:"key"`
	Text  string `json:"text"`
	Index int    `json:"i"` // 对应输入段号 [n]；模型未填时为 0（勿用 omitempty，否则 i=0 被吃掉）
}

// PaperSegmentFormatHint 与 PaperSectionSegment 按下标一一对应，供写段前设置 TextFormat/Spacing。
type PaperSegmentFormatHint struct {
	I                   int     `json:"i"`
	Key                 string  `json:"key"`
	FontFamily          string  `json:"font_family"`
	FontSizePt          int     `json:"font_size_pt"`
	Bold                bool    `json:"bold"`
	FirstLineIndentPt   int     `json:"first_line_indent_pt"`
	LineSpacingMultiple float64 `json:"line_spacing_multiple"`
	BeforeParaPt        int     `json:"before_para_pt"`
	AfterParaPt         int     `json:"after_para_pt"`
}

var paperSectionKeyOrder = []string{
	PaperSectionKeyCover,
	PaperSectionKeyAbstractCN,
	PaperSectionKeyKeywordsCN,
	PaperSectionKeyAbstractEN,
	PaperSectionKeyKeywordsEN,
	PaperSectionKeyTOC,
	PaperSectionKeyHeading1,
	PaperSectionKeyHeading2,
	PaperSectionKeyHeading3,
	PaperSectionKeyHeading4,
	PaperSectionKeyBody,
	PaperSectionKeyReferences,
	PaperSectionKeyAcknowledgements,
}

// legacyPaperSectionsMapToSegments 将旧版 13 键对象转为有序片段（仅键顺序，无正文内交错），供兼容旧模型输出。
func legacyPaperSectionsMapToSegments(m map[string]interface{}) []PaperSectionSegment {
	if m == nil {
		return nil
	}
	out := make([]PaperSectionSegment, 0, 16)
	for _, k := range paperSectionKeyOrder {
		s := paperSectionValueToString(m[k])
		if s != "" {
			out = append(out, PaperSectionSegment{Key: k, Text: s})
		}
	}
	return out
}

func paperSectionValueToString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// JSON 数字默认解为 float64
		if t == float64(int64(t)) {
			return fmt.Sprintf("%.0f", t)
		}
		return fmt.Sprint(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(t)
	}
}

func paperSectionIndexFromInterface(v interface{}) int {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	default:
		return 0
	}
}

// parseJSONRootForPaperSections 解析论文结构 AI 返回：根可为 {"segments":[…]} 或裸数组 [...]（调用前已去掉 markdown 围栏）。
func parseJSONRootForPaperSections(trimmed string, tag string) (map[string]interface{}, error) {
	s := strings.TrimSpace(trimmed)
	var root interface{}
	if err := json.Unmarshal([]byte(s), &root); err != nil {
		s2 := fixJSONObjectBody(s, tag)
		if err2 := json.Unmarshal([]byte(s2), &root); err2 != nil {
			return nil, fmt.Errorf("论文结构 AI JSON 解析失败: %w", err)
		}
	}
	switch v := root.(type) {
	case map[string]interface{}:
		return v, nil
	case []interface{}:
		return map[string]interface{}{"segments": v}, nil
	default:
		return nil, fmt.Errorf("论文结构 AI 返回根类型无效")
	}
}

func segmentsFromJSONArray(raw interface{}) ([]PaperSectionSegment, error) {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("segments 不是数组")
	}
	out := make([]PaperSectionSegment, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		key := strings.TrimSpace(paperSectionValueToString(m["key"]))
		text := paperSectionValueToString(m["text"])
		if key == "" && strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, PaperSectionSegment{
			Key:   key,
			Text:  text,
			Index: paperSectionIndexFromInterface(m["i"]),
		})
	}
	return out, nil
}

func mapHasAnyPaperSectionKey(m map[string]interface{}) bool {
	for _, k := range paperSectionKeyOrder {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

// normalizePaperSegmentsFromAIRoot 从 AI 顶层对象得到有序片段；支持新版 segments 数组或旧版 13 键对象。
func normalizePaperSegmentsFromAIRoot(m map[string]interface{}) ([]PaperSectionSegment, error) {
	if m == nil {
		return []PaperSectionSegment{}, nil
	}
	if raw, ok := m["segments"]; ok {
		if raw == nil {
			return []PaperSectionSegment{}, nil
		}
		return segmentsFromJSONArray(raw)
	}
	if mapHasAnyPaperSectionKey(m) {
		return legacyPaperSectionsMapToSegments(m), nil
	}
	return []PaperSectionSegment{}, nil
}

const aiPaperStructurePrompt = `你是中文学位论文结构分类专家。输入是一份**学生论文（或从中抽取的纯文本）**，已按段编号为 [0]、[1]、[2]…。

## 输出格式（必须严格遵守）

只输出**一个 JSON 对象**，不要 markdown，不要解释。结构固定为：

{"segments":[{"key":"…","text":"…","i":0}, …]}

- **segments**：数组，**数组元素顺序 = 论文从头到尾的阅读顺序**。
- **每一输入段 [n] 恰好对应数组中的一项**（一一对应）：先处理 [0] 输出第 1 个对象，再 [1] 输出第 2 个……**禁止**调换数组元素顺序，**禁止**把后段内容合并到前段前面。
- **key**：该段所属类别，只能是下列英文之一：
  cover | abstract_cn | keywords_cn | abstract_en | keywords_en | toc | heading_1 | heading_2 | heading_3 | heading_4 | body | references | acknowledgements
- **text**：该段**原文**（来自输入，去掉行首的 [n] 前缀即可），不要翻译、不要概括、不要编造。
- **i**：整数，等于该段在输入中的编号 n（与 [n] 一致），便于核对。

每个带编号 [n] 的输入段均输出 **恰好一项**；text 为该段去掉 [n] 前缀后的原文（trim 空白即可）。**禁止**合并多个 [n] 为一条 segment、**禁止**遗漏某个 [n]。

## 各类别含义（归类规则）

1. **cover**  
   封面页全部文字：学校/论文类型、中文题目、日期等。不含摘要与关键词。

2. **abstract_cn**  
   「摘要」标签行及中文摘要正文（到中文关键词行之前）。若摘要跨多段，**每段仍单独一项**，key 均为 abstract_cn。

3. **keywords_cn**  
   「关键词：」等行及中文关键词列表。

4. **abstract_en**  
   "Abstract" 标题行及英文摘要正文（到英文关键词之前）。

5. **keywords_en**  
   "Key words:" / "Keywords:" 及英文关键词。

6. **toc**  
   「目录」及**目录页上所有行**（含页码、点线）。**目录中的标题行只归 toc**，不要另用 heading_*。

7. **heading_1 / heading_2 / heading_3 / heading_4**  
   仅**正文区**（目录结束至参考文献前）的对应级别**标题行本身**。正文跟随段落用 **body**。

8. **body**  
   正文段落、表格说明、图表标题等（非参考文献条目、非致谢、非上述标题行）。

9. **references**  
   「参考文献」标题及文献条目。

10. **acknowledgements**  
    「致谢」及致谢正文。

## 重要规则

- **顺序错误视为严重错误**：segments 数组顺序必须与 [0],[1],[2]… 输入顺序一致。
- 不要为省事把多段合并成一条（除非输入里本为一段）；**默认一段输入 → segments 中一项**。
- 若输入是格式说明而非成稿：尽量归入 body，仍保持 [n] 顺序输出。

## 已编号输入文本

%s`

const aiTemplateSegmentFormatsPrompt = `你是学位论文排版分析专家。输入分为两部分：

A）**模板/范例论文**全文，已按段编号为 [0]、[1]、[2]…（反映模板排版层次，请对照其中字体、字号、行距、段前后距、缩进）。

B）**学生稿分段计划** JSON 数组：每一项含 "i"（学生稿分段序号，从 0 递增）、"key"（结构键，如 abstract_cn、body）、"preview"（该段正文前缀，便于对照，勿修改）。

你的任务：根据模板中与该结构位置**相对应的版式习惯**，为分段计划中**每一项**输出一条可直接用于 Word 段落的格式建议（字体、字号磅、是否加粗、首行缩进磅、行距倍数、段前/段后磅）。

## 输出（仅一个 JSON 对象，不要 markdown）

{
  "segment_formats": [
    {
      "i": 0,
      "key": "cover",
      "font_family": "黑体",
      "font_size_pt": 16,
      "bold": true,
      "first_line_indent_pt": 0,
      "line_spacing_multiple": 1.0,
      "before_para_pt": 0,
      "after_para_pt": 12
    }
  ]
}

## 硬性规则

- **segment_formats 数组长度必须等于学生稿分段计划的项数**，且**按下标一一对应**：segment_formats[k] 对应计划中的第 k 项（k 从 0 起）。
- 每一项的 "i"、"key" 必须与计划中第 k 项的 i、key **完全一致**（不得错位、不得合并、不得遗漏）。
- 字号用整数磅（font_size_pt）；行距用倍数（line_spacing_multiple，如 1.5）；缩进与段前后单位均为磅（pt）。
- 无法从模板确定时：正文 body 类用宋体 12pt、1.5 倍行距、首行缩进 28pt、段后 24pt；heading_* 常用黑体 15pt 加粗、首行缩进 0、单倍行距；cover/toc/references 首行缩进多为 0。

## 模板文本（A）

%s

## 学生稿分段计划（B）

%s`

// numberParagraphsForAI 将全文切成段并加 [0] [1] 前缀。maxChunkRunes<=0 时不截断单段（仅受上游全文截断限制）。
func numberParagraphsForAI(text string, maxChunkRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	chunks := splitIntoParagraphChunks(text)
	var b strings.Builder
	idx := 0
	for _, c := range chunks {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if maxChunkRunes > 0 {
			r := []rune(c)
			if len(r) > maxChunkRunes {
				c = string(r[:maxChunkRunes]) + "…"
			}
		}
		fmt.Fprintf(&b, "[%d] %s\n", idx, c)
		idx++
	}
	return strings.TrimSpace(b.String())
}

func splitIntoParagraphChunks(text string) []string {
	raw := strings.Split(text, "\n\n")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) > 1 {
		return out
	}
	lines := strings.Split(text, "\n")
	out = out[:0]
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return []string{strings.TrimSpace(text)}
	}
	return out
}

type segmentPlanItem struct {
	I       int    `json:"i"`
	Key     string `json:"key"`
	Preview string `json:"preview"`
}

func buildSegmentPlanJSON(segments []PaperSectionSegment) (string, error) {
	plan := make([]segmentPlanItem, 0, len(segments))
	for k, seg := range segments {
		prev := seg.Text
		r := []rune(prev)
		if len(r) > 120 {
			prev = string(r[:120]) + "…"
		}
		plan = append(plan, segmentPlanItem{I: k, Key: seg.Key, Preview: prev})
	}
	b, err := json.Marshal(plan)
	return string(b), err
}

// DefaultPaperSegmentFormatHint 无模板或 AI 失败时的按 key 兜底格式。
func DefaultPaperSegmentFormatHint(seq int, seg PaperSectionSegment) PaperSegmentFormatHint {
	k := seg.Key
	h := PaperSegmentFormatHint{
		I: seq, Key: k, FontFamily: "宋体", FontSizePt: 12, Bold: false,
		FirstLineIndentPt: 28, LineSpacingMultiple: 1.5, BeforeParaPt: 0, AfterParaPt: 24,
	}
	if strings.HasPrefix(k, "heading_") {
		h.FontFamily = "黑体"
		h.FontSizePt = 15
		h.Bold = true
		h.FirstLineIndentPt = 0
		h.LineSpacingMultiple = 1.0
		h.AfterParaPt = 12
	}
	if k == "cover" || k == "toc" || k == "keywords_cn" || k == "keywords_en" {
		h.FirstLineIndentPt = 0
	}
	return h
}

func finalizePaperSegmentFormatHint(h PaperSegmentFormatHint, seq int, seg PaperSectionSegment) PaperSegmentFormatHint {
	d := DefaultPaperSegmentFormatHint(seq, seg)
	h.I = seq
	h.Key = seg.Key
	if strings.TrimSpace(h.FontFamily) == "" {
		h.FontFamily = d.FontFamily
	}
	if h.FontSizePt <= 0 {
		h.FontSizePt = d.FontSizePt
	}
	if h.LineSpacingMultiple <= 0 {
		h.LineSpacingMultiple = d.LineSpacingMultiple
	}
	if h.AfterParaPt < 0 {
		h.AfterParaPt = d.AfterParaPt
	}
	if h.BeforeParaPt < 0 {
		h.BeforeParaPt = d.BeforeParaPt
	}
	return h
}

// alignSegmentFormatsWithSegments 将模型返回的 segment_formats 按下标与学生稿 segments 对齐；缺项或 key 不符用默认。
func alignSegmentFormatsWithSegments(segments []PaperSectionSegment, hints []PaperSegmentFormatHint) []PaperSegmentFormatHint {
	out := make([]PaperSegmentFormatHint, len(segments))
	for k, seg := range segments {
		out[k] = DefaultPaperSegmentFormatHint(k, seg)
	}
	if len(hints) == 0 {
		return out
	}
	n := len(hints)
	if n > len(segments) {
		n = len(segments)
	}
	for k := 0; k < n; k++ {
		h := hints[k]
		if strings.TrimSpace(h.Key) != "" && h.Key != segments[k].Key {
			log.Printf("[模板格式对齐] 下标 %d key 不一致: 模型 %q 学生稿 %q，保留默认", k, h.Key, segments[k].Key)
			continue
		}
		out[k] = finalizePaperSegmentFormatHint(h, k, segments[k])
	}
	return out
}

type segmentFormatsRoot struct {
	SegmentFormats []PaperSegmentFormatHint `json:"segment_formats"`
}

func parseSegmentFormatsFromAIResponse(sanitized string) ([]PaperSegmentFormatHint, error) {
	sanitized = strings.TrimSpace(sanitized)
	var root segmentFormatsRoot
	if err := json.Unmarshal([]byte(sanitized), &root); err != nil {
		fixed := fixJSONObjectBody(sanitized, "[模板格式AI]")
		if err2 := json.Unmarshal([]byte(fixed), &root); err2 != nil {
			return nil, fmt.Errorf("segment_formats JSON: %w", err)
		}
	}
	return root.SegmentFormats, nil
}

// ParseTemplateSegmentFormatsAI 根据模板论文全文 + 学生稿 segments 顺序，请求 DeepSeek 返回与 segments 一一对齐的版式（供写段）。
// templateText 为空时仅返回按 key 的默认对齐结果，不调用模型。
func (s *FormatParserService) ParseTemplateSegmentFormatsAI(templateText string, segments []PaperSectionSegment) ([]PaperSegmentFormatHint, string, error) {
	if len(segments) == 0 {
		return nil, "", nil
	}
	if s.aiClient == nil {
		return alignSegmentFormatsWithSegments(segments, nil), "", nil
	}
	templateText = strings.TrimSpace(templateText)
	if templateText == "" {
		out := alignSegmentFormatsWithSegments(segments, nil)
		return out, "", nil
	}
	templateText = s.CleanTextForAI(templateText)
	const maxR = 9000
	if len([]rune(templateText)) > maxR {
		templateText = string([]rune(templateText)[:maxR])
	}
	numbered := numberParagraphsForAI(templateText, 0)
	if len([]rune(numbered)) > 12000 {
		numbered = string([]rune(numbered)[:12000])
	}
	planJSON, err := buildSegmentPlanJSON(segments)
	if err != nil {
		return nil, "", err
	}
	prompt := fmt.Sprintf(aiTemplateSegmentFormatsPrompt, numbered, planJSON)
	logTag := "[模板分段格式AI]"
	log.Printf("%s 发送给 DeepSeek（计划项数=%d）", logTag, len(segments))
	response, err := s.aiClient.ChatCompletion(prompt)
	sanitized := trimDeepSeekResponseBody(response)
	if err != nil {
		log.Printf("%s DeepSeek 失败，使用默认对齐: %v", logTag, err)
		return alignSegmentFormatsWithSegments(segments, nil), sanitized, fmt.Errorf("DeepSeek 调用失败: %w", err)
	}
	rawHints, perr := parseSegmentFormatsFromAIResponse(sanitized)
	if perr != nil {
		log.Printf("%s 解析失败，使用默认对齐: %v", logTag, perr)
		return alignSegmentFormatsWithSegments(segments, nil), sanitized, fmt.Errorf("解析 segment_formats: %w", perr)
	}
	if len(rawHints) != len(segments) {
		log.Printf("%s 警告: segment_formats 条数 %d != segments %d，按下标截断/补缺", logTag, len(rawHints), len(segments))
	}
	aligned := alignSegmentFormatsWithSegments(segments, rawHints)
	log.Printf("%s 对齐完成 %d 条", logTag, len(aligned))
	return aligned, sanitized, nil
}

// trimDeepSeekResponseBody 去掉模型可能包裹的 markdown 代码围栏，供解析与调试回传。
func trimDeepSeekResponseBody(response string) string {
	s := strings.TrimSpace(response)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// ParseFormatWithAI 调用 DeepSeek：kind 为 FormatAIPromptKindFormatRules 时解析格式规范；为 FormatAIPromptKindPaperSections 时返回 map 仅含键 "segments"。
// 第二返回值 aiRawBody 为去掉围栏后的模型原文（成功/失败均尽量返回，便于调试）；在调用失败时也可能含截断的 SSE 片段。
// kind 传空字符串时等同于 FormatAIPromptKindFormatRules。
func (s *FormatParserService) ParseFormatWithAI(text string, kind string) (data map[string]interface{}, aiRawBody string, err error) {
	if s.aiClient == nil {
		return nil, "", fmt.Errorf("AI client not available")
	}

	if kind == "" {
		kind = FormatAIPromptKindFormatRules
	}

	text = s.CleanTextForAI(text)
	runeLen := len([]rune(text))

	var (
		prompt   string
		logTag   string
		maxRunes int
		minRunes int
	)

	switch kind {
	case FormatAIPromptKindFormatRules:
		maxRunes = 6000
		minRunes = 50
		logTag = "[格式解析AI]"
		if runeLen > maxRunes {
			text = string([]rune(text)[:maxRunes])
			runeLen = maxRunes
		}
		if runeLen < minRunes {
			return nil, "", fmt.Errorf("过滤乱码后文本过短 (%d 字符), 跳过 AI 解析", runeLen)
		}
		prompt = fmt.Sprintf(aiFormatPrompt, text)

	case FormatAIPromptKindPaperSections:
		maxRunes = 10000
		minRunes = 30
		logTag = "[论文结构AI]"
		if runeLen > maxRunes {
			text = string([]rune(text)[:maxRunes])
			runeLen = maxRunes
		}
		if runeLen < minRunes {
			return nil, "", fmt.Errorf("过滤乱码后文本过短 (%d 字符), 跳过论文结构 AI 解析", runeLen)
		}
		numbered := numberParagraphsForAI(text, 0)
		nr := len([]rune(numbered))
		const maxNumberedRunes = 12000
		if nr > maxNumberedRunes {
			numbered = string([]rune(numbered)[:maxNumberedRunes])
		}
		if strings.TrimSpace(numbered) == "" {
			return nil, "", fmt.Errorf("分段编号后无有效段落，跳过论文结构 AI 解析")
		}
		prompt = fmt.Sprintf(aiPaperStructurePrompt, numbered)

	default:
		return nil, "", fmt.Errorf("unknown ParseFormatWithAI kind %q: use %q or %q",
			kind, FormatAIPromptKindFormatRules, FormatAIPromptKindPaperSections)
	}

	log.Printf("%s 发送给 DeepSeek（kind=%s，约 %d 字符）", logTag, kind, len([]rune(prompt)))
	start := time.Now()

	response, err := s.aiClient.ChatCompletion(prompt)
	sanitized := trimDeepSeekResponseBody(response)

	elapsed := time.Since(start)
	log.Printf("%s DeepSeek 返回 (%v), 响应长度: %d", logTag, elapsed, len(response))

	if err != nil {
		return nil, sanitized, fmt.Errorf("DeepSeek 调用失败: %w", err)
	}

	if kind == FormatAIPromptKindPaperSections {
		rootMap, err := parseJSONRootForPaperSections(sanitized, logTag)
		if err != nil {
			log.Printf("%s JSON 解析失败: %v\n  response: %s", logTag, err, truncateStr(sanitized, 300))
			return nil, sanitized, fmt.Errorf("AI 返回的 JSON 解析失败: %w", err)
		}
		segs, err := normalizePaperSegmentsFromAIRoot(rootMap)
		if err != nil {
			log.Printf("%s segments 归一化失败: %v", logTag, err)
			return nil, sanitized, fmt.Errorf("论文结构 segments 解析失败: %w", err)
		}
		log.Printf("%s 论文章节 segments 条数: %d", logTag, len(segs))
		return map[string]interface{}{
			"segments": segs,
		}, sanitized, nil
	}

	fixed := fixJSONObjectBody(sanitized, logTag)

	var aiRules map[string]interface{}
	if err := json.Unmarshal([]byte(fixed), &aiRules); err != nil {
		log.Printf("%s JSON 解析失败: %v\n  response: %s", logTag, err, truncateStr(fixed, 300))
		return nil, sanitized, fmt.Errorf("AI 返回的 JSON 解析失败: %w", err)
	}

	log.Printf("%s 成功解析 %d 个顶层规则: %v", logTag, len(aiRules), mapKeysTop(aiRules))

	return aiRules, sanitized, nil
}

func mapKeysTop(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// MergeFormatRules 合并 AI 规则和正则规则（AI 优先，正则兜底）
func MergeFormatRules(aiRules, regexRules map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})

	// 先放入所有正则规则作为基础
	for k, v := range regexRules {
		merged[k] = v
	}

	// AI 规则覆盖（深度合并）
	for k, aiVal := range aiRules {
		regexVal, exists := merged[k]
		if !exists {
			merged[k] = aiVal
			continue
		}

		aiMap, aiIsMap := aiVal.(map[string]interface{})
		regexMap, regexIsMap := regexVal.(map[string]interface{})

		if aiIsMap && regexIsMap {
			merged[k] = deepMerge(aiMap, regexMap)
		} else {
			// AI 非空值直接覆盖
			if aiVal != nil && aiVal != "" {
				merged[k] = aiVal
			}
		}
	}

	return merged
}

// deepMerge 深度合并两个 map（src 覆盖 base）
func deepMerge(src, base map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for k, v := range base {
		result[k] = v
	}

	for k, srcVal := range src {
		baseVal, exists := result[k]
		if !exists {
			result[k] = srcVal
			continue
		}

		srcMap, srcIsMap := srcVal.(map[string]interface{})
		baseMap, baseIsMap := baseVal.(map[string]interface{})

		if srcIsMap && baseIsMap {
			result[k] = deepMerge(srcMap, baseMap)
		} else if srcVal != nil && srcVal != "" {
			result[k] = srcVal
		}
	}

	return result
}

func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}

// fixJSONObjectBody 修复 AI 返回的 JSON，处理外层 {} 缺失的情况
func fixJSONObjectBody(response string, tag string) string {
	trimmed := strings.TrimSpace(response)

	// 正常情况：已经是 {...} 包裹的 JSON
	if len(trimmed) > 1 && trimmed[0] == '{' {
		jsonEnd := strings.LastIndex(trimmed, "}")
		if jsonEnd > 0 {
			return trimmed[:jsonEnd+1]
		}
		return trimmed
	}

	// 异常情况：内容以 "key_name": 开头 → 缺少外层 {}，需要包裹
	if len(trimmed) > 2 && trimmed[0] == '"' {
		jsonEnd := strings.LastIndex(trimmed, "}")
		if jsonEnd > 0 {
			wrapped := "{" + trimmed[:jsonEnd+1] + "}"
			log.Printf("%s 自动包裹缺失的外层 {}: %s", tag, truncateStr(wrapped, 100))
			return wrapped
		}
	}

	// 无法识别格式，返回原文
	return response
}

// ParseFormatFromTextSmart 智能解析：AI 优先 + 正则兜底
func (s *FormatParserService) ParseFormatFromTextSmart(text string) (string, error) {
	// Step 1: 正则解析（始终执行，作为兜底）
	regexJSON, err := s.ParseFormatFromText(text)
	if err != nil {
		return "", fmt.Errorf("正则解析失败: %w", err)
	}

	var regexRules map[string]interface{}
	if err := json.Unmarshal([]byte(regexJSON), &regexRules); err != nil {
		return regexJSON, nil
	}

	// Step 2: AI 解析（如果可用）
	if s.aiClient == nil {
		log.Println("[格式解析] AI 未启用，仅使用正则结果")
		return regexJSON, nil
	}

	aiRules, _, aiErr := s.ParseFormatWithAI(text, FormatAIPromptKindFormatRules)
	if aiErr != nil {
		log.Printf("[格式解析] AI 解析失败，回退到正则结果: %v", aiErr)
		return regexJSON, nil
	}

	// Step 3: 合并（AI 优先）
	merged := MergeFormatRules(aiRules, regexRules)

	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		log.Printf("[格式解析] 合并结果序列化失败: %v", err)
		return regexJSON, nil
	}

	log.Printf("[格式解析] AI+正则 合并完成，最终 %d 个顶层规则", len(merged))
	return string(mergedJSON), nil
}

// InitAIClient 初始化 AI 客户端（在获取配置后调用）
func (s *FormatParserService) InitAIClient(cookie, bearer string, enabled bool) {
	if enabled && cookie != "" {
		s.aiClient = aiclassifier.NewDeepSeekWebClient(cookie, bearer)
		log.Println("[格式解析] AI 增强解析已启用")
	} else {
		log.Println("[格式解析] AI 未配置，使用纯正则解析")
	}
}

const aiUniversityPrompt = `从以下论文格式规范文本中提取高校信息。只输出JSON，不要其他文字。

输出格式：
{"university_name": "高校全称", "document_type": "本科论文/硕士论文/博士论文/课程论文"}

规则：
1. university_name 必须是高校全称（如"重庆工程学院"，不是"重工"）
2. 如果文本中找不到任何高校名称，university_name 设为空字符串
3. document_type 根据文本中"本科/学士/硕士/博士/课程"等关键词判断，默认"本科论文"
4. 只输出JSON，不要 markdown 标记

文本：
%s`

// isLikelyGarbageCJK 检测一行是否是 .doc 二进制产生的伪CJK乱码
// .doc 二进制中 ASCII 字节对被误读为 CJK 字符，特征是含有大量生僻字 + ASCII混合
func isLikelyGarbageCJK(line string) bool {
	runes := []rune(line)
	if len(runes) < 3 {
		return false
	}

	commonCJK := 0   // 常用汉字
	rareCJK := 0     // 罕见/生僻字（乱码特征）
	punctuation := 0 // 中文标点
	alphaNum := 0    // 字母数字

	for _, r := range runes {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF:
			// 常用汉字范围：检查是否在常用3500字区域
			// 生僻字特征：0x6000-0x9FFF 中有大量罕见字
			// 用一个简单启发式：连续的罕见CJK码点通常是乱码
			if r >= 0x5000 && r <= 0x9000 {
				commonCJK++
			} else {
				rareCJK++
			}
		case r == '，' || r == '。' || r == '、' || r == '：' || r == '；' ||
			r == '\u201c' || r == '\u201d' || r == '（' || r == '）' || r == '《' || r == '》':
			punctuation++
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			alphaNum++
		}
	}

	total := len(runes)
	// 特征1：罕见CJK字符超过50%的行几乎必定是乱码
	if rareCJK*2 > total && punctuation == 0 {
		return true
	}
	// 特征2：没有任何中文标点但有大量CJK字符 + 长度超过30
	if total > 30 && punctuation == 0 && (commonCJK+rareCJK) > total/2 && alphaNum > total/4 {
		return true
	}
	// 特征3：含有典型的.doc乱码模式（连续的罕见CJK字符对）
	if total > 10 && rareCJK > 5 && commonCJK < rareCJK/2 {
		return true
	}
	return false
}

// cleanTextForAI 过滤文本中的乱码行，只保留有意义的中文/数字行
func (s *FormatParserService) CleanTextForAI(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 跳过明显的乱码行
		if isLikelyGarbageCJK(line) {
			continue
		}

		runes := []rune(line)
		total := len(runes)

		// 跳过过短的无意义行
		if total < 2 {
			continue
		}

		// 跳过全是不可打印/控制字符的行
		printable := 0
		for _, r := range runes {
			if r >= 0x20 {
				printable++
			}
		}
		if printable*2 < total {
			continue
		}

		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// ExtractUniversityInfoWithAI 使用 AI 从文本中提取高校信息
func (s *FormatParserService) ExtractUniversityInfoWithAI(text string) map[string]string {
	info := make(map[string]string)

	if s.aiClient == nil {
		return info
	}

	// 先过滤乱码行，再取前 2000 字符
	text = s.CleanTextForAI(text)
	runes := []rune(text)
	if len(runes) > 2000 {
		runes = runes[:2000]
		text = string(runes)
	}

	log.Printf("[高校识别AI] 过滤后文本预览 (%d 字符): %s", len([]rune(text)), func() string {
		r := []rune(text)
		if len(r) > 200 {
			return string(r[:200])
		}
		return text
	}())

	prompt := fmt.Sprintf(aiUniversityPrompt, text)

	log.Println("[高校识别AI] 调用 DeepSeek 提取高校信息...")
	start := time.Now()

	response, err := s.aiClient.ChatCompletion(prompt)
	if err != nil {
		log.Printf("[高校识别AI] DeepSeek 调用失败: %v", err)
		return info
	}

	elapsed := time.Since(start)
	log.Printf("[高校识别AI] DeepSeek 返回 (%v): %s", elapsed, truncateStr(response, 200))

	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	response = fixJSONObjectBody(response, "[高校识别AI]")

	var result struct {
		UniversityName string `json:"university_name"`
		DocumentType   string `json:"document_type"`
	}
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		log.Printf("[高校识别AI] JSON 解析失败: %v\n  response: %s", err, truncateStr(response, 200))
		return info
	}

	if result.UniversityName != "" {
		info["name"] = strings.TrimSpace(result.UniversityName)
		log.Printf("[高校识别AI] 识别到高校: %s", info["name"])
	}
	if result.DocumentType != "" {
		info["document_type"] = result.DocumentType
	}

	return info
}
