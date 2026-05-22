package fileprocessor

import (
	"log"
	"regexp"
	"strings"
	"unicode"

	"gitee.com/greatmusicians/unioffice/document"
)

// V2Zone 论文结构区段
type V2Zone int

const (
	ZoneCover V2Zone = iota
	ZoneAbstract
	ZoneEnAbstract
	ZoneTOC
	ZoneBody
	ZoneReferences
	ZoneAcknowledgements
	ZoneAppendix
)

// V2ParaType 段落类型标签
const (
	V2Cover                  = "cover"
	V2OriginalityDeclaration = "originality_declaration"
	V2ThesisTitle            = "thesis_title"
	V2ThesisSubtitle         = "thesis_subtitle"
	V2AbstractTitle          = "abstract_title"
	V2Abstract               = "abstract"
	V2Keywords               = "keywords"
	V2EnAbstractTitle        = "en_abstract_title"
	V2EnAbstract             = "en_abstract"
	V2EnKeywords             = "en_keywords"
	V2TOCTitle               = "table_of_contents_title"
	V2TOC                    = "table_of_contents"
	V2Heading1               = "heading_1"
	V2Heading2               = "heading_2"
	V2Heading3               = "heading_3"
	V2Heading4               = "heading_4"
	V2Body                   = "body"
	V2ReferencesTitle        = "references_title"
	V2References             = "references"
	V2AcknowledgementsTitle  = "acknowledgements_title"
	V2Acknowledgements       = "acknowledgements_content"
	V2AppendixTitle          = "appendix_title"
	V2Appendix               = "appendix_content"
	V2NotesTitle             = "notes_title"
	V2Notes                  = "notes_content"
	V2FigureCaption          = "figure_caption"
	V2TableCaption           = "table_caption"
)

var (
	reHeading1Num    = regexp.MustCompile(`^(\d+)\s`)
	reHeading1NumCN  = regexp.MustCompile(`^([1-9])\p{Han}`)
	reHeading1Ch     = regexp.MustCompile(`^第[一二三四五六七八九十百]+章`)
	reHeading2       = regexp.MustCompile(`^(\d+)[.．](\d+)\s*[^.．\d]`)
	reHeading3       = regexp.MustCompile(`^(\d+)[.．](\d+)[.．](\d+)`)
	reHeading4       = regexp.MustCompile(`^(\d+)[.．](\d+)[.．](\d+)[.．](\d+)`)
	reRefItem        = regexp.MustCompile(`^\[?\d+\]`)
	reTOCDots        = regexp.MustCompile(`[．\.…]{2,}`)
	reFigureCaption  = regexp.MustCompile(`^图\s*\d+`)
	reTableCaption   = regexp.MustCompile(`^表\s*\d+`)
)

// V2ClassifiedPara 分类结果
type V2ClassifiedPara struct {
	Para     document.Paragraph
	Text     string
	Type     string
	ParaIdx  int
}

// V2DeterministicClassifier 确定性段落分类器
// 使用状态机 + 关键词 + 正则实现100%确定性分类
type V2DeterministicClassifier struct {
	processor *EnhancedProcessor
}

func NewV2DeterministicClassifier(proc *EnhancedProcessor) *V2DeterministicClassifier {
	return &V2DeterministicClassifier{processor: proc}
}

// Classify 对文档所有段落进行确定性分类
func (c *V2DeterministicClassifier) Classify(paragraphs []document.Paragraph) []V2ClassifiedPara {
	var results []V2ClassifiedPara

	for i, para := range paragraphs {
		text := strings.TrimSpace(c.processor.extractParagraphText(para))
		results = append(results, V2ClassifiedPara{
			Para:    para,
			Text:    text,
			ParaIdx: i,
		})
	}

	c.assignTypes(results)
	return results
}

// ClassifyToMap 返回 map[type][]Paragraph 形式（兼容旧接口）
func (c *V2DeterministicClassifier) ClassifyToMap(paragraphs []document.Paragraph) map[string][]document.Paragraph {
	classified := c.Classify(paragraphs)
	result := make(map[string][]document.Paragraph)
	for _, cp := range classified {
		if cp.Text == "" {
			continue
		}
		result[cp.Type] = append(result[cp.Type], cp.Para)
	}
	return result
}

func (c *V2DeterministicClassifier) assignTypes(paras []V2ClassifiedPara) {
	zone := ZoneCover
	inOriginality := false

	for i := range paras {
		text := paras[i].Text
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			paras[i].Type = V2Body
			continue
		}

		normalized := normalizeSpaces(trimmed)

		// ── 关键词触发区段转换 ──
		switch {
		case inOriginality:
			if isAbstractTitleKW(normalized) || isEnAbstractTitleKW(normalized) ||
				isTOCTitleKW(normalized) || isHeading1(normalized) {
				inOriginality = false
			} else {
				paras[i].Type = V2OriginalityDeclaration
				continue
			}

		case isOriginalityKW(normalized):
			inOriginality = true
			paras[i].Type = V2OriginalityDeclaration
			continue
		}

		// ── 区段标题检测（触发状态转移）──
		if isAbstractTitleKW(normalized) {
			zone = ZoneAbstract
			paras[i].Type = V2AbstractTitle
			continue
		}
		// "摘要：内容..." 格式（标签和正文在同一段落）
		if zone == ZoneCover && isAbstractStartKW(normalized) {
			zone = ZoneAbstract
			paras[i].Type = V2Abstract
			continue
		}
		if isKeywordsKW(normalized) && (zone == ZoneAbstract || zone == ZoneCover) {
			zone = ZoneAbstract
			paras[i].Type = V2Keywords
			continue
		}
		if isEnAbstractTitleKW(normalized) {
			zone = ZoneEnAbstract
			paras[i].Type = V2EnAbstractTitle
			continue
		}
		if isEnAbstractStartKW(normalized) && (zone == ZoneAbstract || zone == ZoneCover) {
			zone = ZoneEnAbstract
			paras[i].Type = V2EnAbstract
			continue
		}
		if isEnKeywordsKW(normalized) && (zone == ZoneEnAbstract || zone == ZoneAbstract) {
			zone = ZoneEnAbstract
			paras[i].Type = V2EnKeywords
			continue
		}
		if isTOCTitleKW(normalized) {
			zone = ZoneTOC
			paras[i].Type = V2TOCTitle
			continue
		}
		if isReferencesTitleKW(normalized) && zone == ZoneBody {
			zone = ZoneReferences
			paras[i].Type = V2ReferencesTitle
			continue
		}
		if isAcknowledgementsTitleKW(normalized) {
			zone = ZoneAcknowledgements
			paras[i].Type = V2AcknowledgementsTitle
			continue
		}
		if isAppendixTitleKW(normalized) {
			zone = ZoneAppendix
			paras[i].Type = V2AppendixTitle
			continue
		}
		if isNotesTitleKW(normalized) {
			zone = ZoneReferences // notes are reference-like
			paras[i].Type = V2NotesTitle
			continue
		}

		// ── 区段内部分类 ──
		switch zone {
		case ZoneCover:
			if isHeading1(normalized) {
				zone = ZoneBody
				paras[i].Type = V2Heading1
			} else if isThesisMainTitle(normalized) && !isCoverLabel(normalized) {
				paras[i].Type = V2ThesisTitle
			} else if isThesisSubtitle(normalized) {
				paras[i].Type = V2ThesisSubtitle
			} else {
				paras[i].Type = V2Cover
			}

		case ZoneAbstract:
			paras[i].Type = V2Abstract

		case ZoneEnAbstract:
			paras[i].Type = V2EnAbstract

		case ZoneTOC:
			if v2HasTOCStyle(paras[i].Para) || reTOCDots.MatchString(normalized) || v2IsTOCEntry(normalized) {
				paras[i].Type = V2TOC
			} else if isHeading1(normalized) {
				zone = ZoneBody
				paras[i].Type = V2Heading1
			} else if isReferencesTitleKW(normalized) {
				zone = ZoneReferences
				paras[i].Type = V2ReferencesTitle
			} else {
				paras[i].Type = V2TOC
			}

		case ZoneBody:
			paras[i].Type = classifyBodyParagraph(normalized)

		case ZoneReferences:
			if reRefItem.MatchString(trimmed) || isReferenceContinuation(trimmed) {
				paras[i].Type = V2References
			} else if isAcknowledgementsTitleKW(normalized) {
				zone = ZoneAcknowledgements
				paras[i].Type = V2AcknowledgementsTitle
			} else {
				paras[i].Type = V2References
			}

		case ZoneAcknowledgements:
			if isAppendixTitleKW(normalized) {
				zone = ZoneAppendix
				paras[i].Type = V2AppendixTitle
			} else {
				paras[i].Type = V2Acknowledgements
			}

		case ZoneAppendix:
			paras[i].Type = V2Appendix
		}
	}

	// 统计日志
	dist := make(map[string]int)
	for _, p := range paras {
		if p.Text != "" {
			dist[p.Type]++
		}
	}
	log.Printf("[V2分类器] 确定性分类完成: %v", dist)

	// #region agent log
	if dist["acknowledgements_content"] > 10 || dist["en_abstract"] > 0 || dist["en_keywords"] > 0 {
		ackSamples := []map[string]interface{}{}
		ackTitleIdx := -1
		for idx, p := range paras {
			if p.Type == V2AcknowledgementsTitle {
				ackTitleIdx = idx
			}
			if ackTitleIdx >= 0 && idx > ackTitleIdx && idx <= ackTitleIdx+5 && p.Text != "" {
				txt := p.Text
				if len(txt) > 60 {
					txt = txt[:60]
				}
				ackSamples = append(ackSamples, map[string]interface{}{
					"idx": idx, "type": p.Type, "text": txt,
				})
			}
		}
		enSamples := []map[string]interface{}{}
		for idx, p := range paras {
			if p.Type == V2EnAbstract || p.Type == V2EnKeywords {
				txt := p.Text
				if len(txt) > 80 {
					txt = txt[:80]
				}
				enSamples = append(enSamples, map[string]interface{}{
					"idx": idx, "type": p.Type, "text": txt,
				})
			}
		}
		h1Samples := []map[string]interface{}{}
		for idx, p := range paras {
			if p.Type == V2Heading1 {
				txt := p.Text
				if len(txt) > 60 {
					txt = txt[:60]
				}
				h1Samples = append(h1Samples, map[string]interface{}{"idx": idx, "type": p.Type, "text": txt})
			}
		}
		debugLog("v2_classifier.go:postClassify", "H3_ZONE_ANALYSIS", map[string]interface{}{
			"hypothesisId":     "H3",
			"distribution":     dist,
			"ackAfterTitle":    ackSamples,
			"enAbstractKW":     enSamples,
			"heading1_all":     h1Samples,
		})
	}
	// #endregion

	// #region agent log
	first20 := []map[string]string{}
	for _, p := range paras {
		if p.Text == "" {
			continue
		}
		t := []rune(p.Text)
		if len(t) > 40 {
			t = t[:40]
		}
		first20 = append(first20, map[string]string{"type": p.Type, "text": string(t)})
		if len(first20) >= 30 {
			break
		}
	}
	debugLog("v2_classifier.go:assignTypes", "H2_CLASSIFICATION_RESULT", map[string]interface{}{
		"hypothesisId": "H2",
		"distribution": dist,
		"first30":      first20,
	})
	// #endregion
}

// ── 关键词匹配函数 ──

func normalizeSpaces(s string) string {
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) || r == '\u3000' {
			if !lastSpace {
				b.WriteRune(' ')
				lastSpace = true
			}
		} else {
			b.WriteRune(r)
			lastSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

func isAbstractTitleKW(s string) bool {
	s = strings.ToLower(s)
	return s == "摘要" || s == "摘 要" || s == "中文摘要" || s == "论文摘要"
}

// isAbstractStartKW 匹配"摘要：内容..."格式（标签+正文在同一段落）
func isAbstractStartKW(s string) bool {
	return strings.HasPrefix(s, "摘要：") || strings.HasPrefix(s, "摘要:") ||
		strings.HasPrefix(s, "摘 要：") || strings.HasPrefix(s, "摘 要:")
}

func isEnAbstractTitleKW(s string) bool {
	lower := strings.ToLower(s)
	return lower == "abstract" || lower == "英文摘要"
}

func isEnAbstractStartKW(s string) bool {
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "abstract:") || strings.HasPrefix(lower, "abstract：") ||
		strings.HasPrefix(lower, "abstract :")
}

func isKeywordsKW(s string) bool {
	return strings.HasPrefix(s, "关键词") || strings.HasPrefix(s, "关键字") ||
		strings.HasPrefix(s, "关 键 词") || strings.HasPrefix(s, "关 键 字")
}

func isEnKeywordsKW(s string) bool {
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "keywords") || strings.HasPrefix(lower, "key words")
}

func isTOCTitleKW(s string) bool {
	return s == "目录" || s == "目 录"
}

// normalizeReferencesTitleStripColon 去掉尾部全角/半角冒号，识别「参考文献：」等仅标题行。
func normalizeReferencesTitleStripColon(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "：")
	s = strings.TrimSuffix(s, ":")
	return strings.TrimSpace(s)
}

func isReferencesTitleKW(s string) bool {
	n := normalizeReferencesTitleStripColon(s)
	return n == "参考文献" || n == "参 考 文 献"
}

func isAcknowledgementsTitleKW(s string) bool {
	return s == "致谢" || s == "致 谢" || s == "鸣谢" || s == "致谢语"
}

func isAppendixTitleKW(s string) bool {
	return strings.HasPrefix(s, "附录") || s == "附 录"
}

func isNotesTitleKW(s string) bool {
	return s == "注释" || s == "注 释"
}

func isOriginalityKW(s string) bool {
	return strings.Contains(s, "原创性声明") || strings.Contains(s, "独创性声明") ||
		strings.Contains(s, "学术诚信") || strings.Contains(s, "原创性申明")
}

func isCoverLabel(s string) bool {
	labels := []string{"本科毕业论文", "本科毕业设计", "学院", "专业", "班级",
		"学号", "姓名", "指导教师", "题目", "年", "月"}
	for _, l := range labels {
		if strings.Contains(s, l) {
			return true
		}
	}
	runes := []rune(s)
	return len(runes) < 4
}

func isHeading1(s string) bool {
	if reHeading1Ch.MatchString(s) {
		return true
	}
	if reHeading1Num.MatchString(s) || reHeading1NumCN.MatchString(s) {
		first := s[0]
		if first >= '1' && first <= '9' && !reHeading2.MatchString(s) && !reHeading3.MatchString(s) {
			return true
		}
	}
	if strings.HasPrefix(s, "绪论") || strings.HasPrefix(s, "引言") || strings.HasPrefix(s, "结论") {
		return true
	}
	return false
}

func v2HasTOCStyle(para document.Paragraph) bool {
	if para.X().PPr == nil || para.X().PPr.PStyle == nil {
		return false
	}
	style := strings.ToLower(para.X().PPr.PStyle.ValAttr)
	return strings.HasPrefix(style, "toc") || strings.Contains(style, "contents")
}

func v2IsTOCEntry(s string) bool {
	if strings.Contains(s, "．") || strings.Contains(s, "…") {
		return true
	}
	if strings.Contains(s, "\t") {
		runes := []rune(strings.TrimSpace(s))
		if len(runes) > 0 {
			lastR := runes[len(runes)-1]
			if lastR >= '0' && lastR <= '9' {
				return true
			}
		}
	}
	return false
}

func isReferenceContinuation(s string) bool {
	if len(s) < 5 {
		return false
	}
	return !isHeading1(s) && !isAcknowledgementsTitleKW(s) && !isAppendixTitleKW(s)
}

func classifyBodyParagraph(s string) string {
	if reHeading4.MatchString(s) {
		return V2Heading4
	}
	if reHeading3.MatchString(s) {
		return V2Heading3
	}
	if reHeading2.MatchString(s) {
		return V2Heading2
	}
	if isHeading1(s) {
		return V2Heading1
	}
	if reFigureCaption.MatchString(s) {
		return V2FigureCaption
	}
	if reTableCaption.MatchString(s) {
		return V2TableCaption
	}
	if isReferencesTitleKW(s) {
		return V2ReferencesTitle
	}
	return V2Body
}
