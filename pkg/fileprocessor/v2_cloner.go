package fileprocessor

import (
	"encoding/xml"
	"log"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

// V2TemplateFormat 从模板提取的完整XML格式（零损耗）
type V2TemplateFormat struct {
	PPr     *wml.CT_PPr // 段落属性完整XML节点
	RPr     *wml.CT_RPr // 运行属性完整XML节点（主文本格式）
	LabelRPr *wml.CT_RPr // 标签格式（如"摘要：""关键词："的格式，可为nil）
}

// V2TemplateFormatStore 模板格式库：type -> 完整XML格式
type V2TemplateFormatStore struct {
	Formats map[string]*V2TemplateFormat
}

// ── XML 深拷贝工具 ──

func clonePPr(src *wml.CT_PPr) *wml.CT_PPr {
	if src == nil {
		return nil
	}
	data, err := xml.Marshal(src)
	if err != nil {
		return nil
	}
	dst := &wml.CT_PPr{}
	if xml.Unmarshal(data, dst) != nil {
		return nil
	}
	return dst
}

func cloneRPr(src *wml.CT_RPr) *wml.CT_RPr {
	if src == nil {
		return nil
	}
	data, err := xml.Marshal(src)
	if err != nil {
		return nil
	}
	dst := &wml.CT_RPr{}
	if xml.Unmarshal(data, dst) != nil {
		return nil
	}
	return dst
}

// ── 模板格式提取 ──

// ExtractTemplateFormats 从模板文档提取每种段落类型的完整XML格式
func ExtractTemplateFormats(templateDoc *document.Document, proc *EnhancedProcessor) *V2TemplateFormatStore {
	store := &V2TemplateFormatStore{
		Formats: make(map[string]*V2TemplateFormat),
	}

	classifier := NewV2DeterministicClassifier(proc)
	classified := classifier.Classify(templateDoc.Paragraphs())

	// 按类型收集，取每种类型的第一个有格式的段落作为模板
	for _, cp := range classified {
		if cp.Text == "" {
			continue
		}
		if _, exists := store.Formats[cp.Type]; exists {
			continue // 每种类型只取第一个样本
		}

		pPr := cp.Para.X().PPr
		if pPr == nil {
			continue
		}

		format := &V2TemplateFormat{
			PPr: clonePPr(pPr),
		}

		// 提取运行属性（取第一个有文本的run）
		runs := cp.Para.Runs()
		if len(runs) > 0 {
			for _, r := range runs {
				if strings.TrimSpace(r.Text()) != "" && r.X().RPr != nil {
					format.RPr = cloneRPr(r.X().RPr)
					break
				}
			}
			if format.RPr == nil && runs[0].X().RPr != nil {
				format.RPr = cloneRPr(runs[0].X().RPr)
			}
		}

		// 对于有标签+内容混合格式的段落（摘要/关键词），提取标签格式
		if isLabeledType(cp.Type) && len(runs) >= 2 {
			format.LabelRPr = cloneRPr(runs[0].X().RPr)
			for i := 1; i < len(runs); i++ {
				if strings.TrimSpace(runs[i].Text()) != "" && runs[i].X().RPr != nil {
					format.RPr = cloneRPr(runs[i].X().RPr)
					break
				}
			}
		}

		store.Formats[cp.Type] = format
		log.Printf("[V2模板提取] %s: pPr=%v rPr=%v labelRPr=%v",
			cp.Type, format.PPr != nil, format.RPr != nil, format.LabelRPr != nil)
	}

	log.Printf("[V2模板提取] 共提取 %d 种格式", len(store.Formats))
	return store
}

func isLabeledType(t string) bool {
	return t == V2Abstract || t == V2Keywords || t == V2EnAbstract || t == V2EnKeywords
}

// ── 格式应用（XML 节点整体替换）──

// V2FormatCloner 格式克隆器：将模板XML节点完整替换到学生文档段落
type V2FormatCloner struct {
	store *V2TemplateFormatStore
}

func NewV2FormatCloner(store *V2TemplateFormatStore) *V2FormatCloner {
	return &V2FormatCloner{store: store}
}

// ApplyAll 对所有已分类段落应用模板格式
func (c *V2FormatCloner) ApplyAll(classified []V2ClassifiedPara) int {
	totalFixed := 0

	for i := range classified {
		if classified[i].Text == "" {
			continue
		}

		format, ok := c.store.Formats[classified[i].Type]
		if !ok {
			// 尝试回退映射
			fallbackType := getFallbackType(classified[i].Type)
			if fallbackType != "" {
				format, ok = c.store.Formats[fallbackType]
			}
		}
		if !ok || format == nil {
			continue
		}

		if c.applyFormat(classified[i].Para, classified[i].Type, format) {
			totalFixed++
		}
	}

	return totalFixed
}

// applyFormat 将模板格式应用到单个段落
func (c *V2FormatCloner) applyFormat(para document.Paragraph, paraType string, format *V2TemplateFormat) bool {
	changed := false

	// 封面/题目/目录段落：由SmartFormatter专门处理
	if paraType == V2Cover || paraType == V2ThesisTitle || paraType == V2ThesisSubtitle ||
		paraType == V2TOCTitle || paraType == V2TOC {
		return false
	}

	// 1. 克隆段落属性（pPr）
	if format.PPr != nil {
		newPPr := clonePPr(format.PPr)
		if newPPr != nil {
			oldPPr := para.X().PPr
			if oldPPr != nil {
				if oldPPr.SectPr != nil {
					newPPr.SectPr = oldPPr.SectPr
				}
				if oldPPr.NumPr != nil && newPPr.NumPr == nil {
					newPPr.NumPr = oldPPr.NumPr
				}
			}
			para.X().PPr = newPPr
			changed = true
		}
	}

	// 2. 克隆运行属性（rPr）
	if format.RPr != nil || format.LabelRPr != nil {
		runs := para.Runs()
		if isLabeledType(paraType) && format.LabelRPr != nil {
			changed = c.applyLabeledFormat(runs, format) || changed
		} else if format.RPr != nil {
			for _, r := range runs {
				if strings.TrimSpace(r.Text()) == "" {
					continue
				}
				newRPr := cloneRPr(format.RPr)
				if newRPr != nil {
					r.X().RPr = newRPr
					changed = true
				}
			}
		}
	}

	return changed
}

// applyLabeledFormat 处理"标签：内容"混合格式段落
// 例如"摘要：XXXX"中，"摘要："用黑体加粗，"XXXX"用宋体
func (c *V2FormatCloner) applyLabeledFormat(runs []document.Run, format *V2TemplateFormat) bool {
	if len(runs) == 0 {
		return false
	}

	changed := false
	labelApplied := false

	for _, r := range runs {
		text := r.Text()
		if strings.TrimSpace(text) == "" {
			continue
		}

		// 第一个含"："或":"的 run 及之前都是标签部分
		if !labelApplied && (strings.Contains(text, "：") || strings.Contains(text, ":")) {
			if format.LabelRPr != nil {
				r.X().RPr = cloneRPr(format.LabelRPr)
				changed = true
			}
			labelApplied = true
			continue
		}

		if !labelApplied {
			if format.LabelRPr != nil {
				r.X().RPr = cloneRPr(format.LabelRPr)
				changed = true
			}
		} else {
			if format.RPr != nil {
				r.X().RPr = cloneRPr(format.RPr)
				changed = true
			}
		}
	}

	return changed
}

// applyPPrSelective 选择性应用段落属性（用于封面等特殊段落）
func applyPPrSelective(paraX *wml.CT_P, newPPr *wml.CT_PPr) {
	if paraX.PPr == nil {
		paraX.PPr = newPPr
		return
	}
	old := paraX.PPr
	if newPPr.Jc != nil {
		old.Jc = newPPr.Jc
	}
	if newPPr.Spacing != nil {
		old.Spacing = newPPr.Spacing
	}
	if newPPr.Ind != nil {
		old.Ind = newPPr.Ind
	}
}

// getFallbackType 回退类型映射（当模板缺少某种类型时尝试用相近类型替代）
func getFallbackType(t string) string {
	fallbacks := map[string]string{
		V2Heading4:             V2Heading3,
		V2FigureCaption:        V2Body,
		V2TableCaption:         V2Body,
		V2Acknowledgements:     V2Body,
		V2Appendix:             V2Body,
		V2Notes:                V2References,
		V2AcknowledgementsTitle: V2Heading1,
		V2AppendixTitle:        V2Heading1,
		V2NotesTitle:           V2Heading1,
		V2ThesisSubtitle:       V2ThesisTitle,
	}
	return fallbacks[t]
}

// ── Section 级别格式克隆 ──

// CloneSectionProperties 从模板复制页面设置、页眉页脚等section级属性
func CloneSectionProperties(templateDoc, studentDoc *document.Document) {
	tBody := templateDoc.X().Body
	sBody := studentDoc.X().Body

	if tBody == nil || sBody == nil {
		return
	}

	// 复制文档级 sectPr（最后一个节的属性）
	if tBody.SectPr != nil {
		data, err := xml.Marshal(tBody.SectPr)
		if err == nil {
			newSectPr := &wml.CT_SectPr{}
			if xml.Unmarshal(data, newSectPr) == nil {
				// 保留学生文档的页眉页脚引用（关系ID在不同文档间不通用）
				if sBody.SectPr != nil {
					newSectPr.EG_HdrFtrReferences = sBody.SectPr.EG_HdrFtrReferences
				}
				// 复制页面尺寸和边距
				if sBody.SectPr == nil {
					sBody.SectPr = &wml.CT_SectPr{}
				}
				if newSectPr.PgSz != nil {
					sBody.SectPr.PgSz = newSectPr.PgSz
				}
				if newSectPr.PgMar != nil {
					sBody.SectPr.PgMar = newSectPr.PgMar
				}
				log.Printf("[V2] 已从模板复制页面尺寸和边距")
			}
		}
	}
}

// CloneStyles 从模板复制样式定义到学生文档
func CloneStyles(templateDoc, studentDoc *document.Document) {
	tStyles := templateDoc.Styles
	sStyles := studentDoc.Styles

	if tStyles.X() == nil || sStyles.X() == nil {
		return
	}

	// 复制默认段落属性
	if tStyles.X().DocDefaults != nil {
		data, _ := xml.Marshal(tStyles.X().DocDefaults)
		dst := &wml.CT_DocDefaults{}
		if xml.Unmarshal(data, dst) == nil {
			sStyles.X().DocDefaults = dst
			log.Printf("[V2] 已复制 DocDefaults 样式")
		}
	}

	// 复制命名样式（Heading 1, Normal, etc.）
	if len(tStyles.X().Style) > 0 {
		styleMap := make(map[string]bool)
		for _, s := range sStyles.X().Style {
			if s.StyleIdAttr != nil {
				styleMap[*s.StyleIdAttr] = true
			}
		}

		copied := 0
		for _, tStyle := range tStyles.X().Style {
			if tStyle.StyleIdAttr == nil {
				continue
			}
			data, err := xml.Marshal(tStyle)
			if err != nil {
				continue
			}
			newStyle := &wml.CT_Style{}
			if xml.Unmarshal(data, newStyle) != nil {
				continue
			}

			if styleMap[*tStyle.StyleIdAttr] {
				// 覆盖已有样式
				for j, s := range sStyles.X().Style {
					if s.StyleIdAttr != nil && *s.StyleIdAttr == *tStyle.StyleIdAttr {
						sStyles.X().Style[j] = newStyle
						break
					}
				}
			} else {
				sStyles.X().Style = append(sStyles.X().Style, newStyle)
			}
			copied++
		}
		log.Printf("[V2] 已复制/覆盖 %d 个命名样式", copied)
	}
}
