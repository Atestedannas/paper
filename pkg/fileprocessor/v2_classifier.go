package fileprocessor

import (
	"log"
	"math"
	"regexp"
	"strings"
	"unicode"

	"gitee.com/greatmusicians/unioffice/document"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
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
	reHeading1Num     = regexp.MustCompile(`^(\d+)\s`)
	reHeading1Compact = regexp.MustCompile(`^(\d+)、\s*([^\d\s].+)$`)
	reHeading2Compact = regexp.MustCompile(`^(\d+)[.．]\s*([^\d\s].+)$`)
	reHeading1NumCN   = regexp.MustCompile(`^([1-9])\p{Han}`)
	reHeading1Ch      = regexp.MustCompile(`^第(?:[一二三四五六七八九十百]+|\d+)章`)
	reHeading1List    = regexp.MustCompile(`^[一二三四五六七八九十百]+[、.．]`)
	reHeading2        = regexp.MustCompile(`^(\d+)[.．](\d+)\s*[^.．\d]`)
	reHeading3        = regexp.MustCompile(`^(\d+)[.．](\d+)[.．](\d+)`)
	reHeading4        = regexp.MustCompile(`^(\d+)[.．](\d+)[.．](\d+)[.．](\d+)`)
	reRefItem         = regexp.MustCompile(`^\[?\d+\]`)
	reTOCDots         = regexp.MustCompile(`[．\.…]{2,}`)
	reFigureCaption   = regexp.MustCompile(`^图\s*\d+`)
	reTableCaption    = regexp.MustCompile(`^表\s*\d+`)
)

// V2ClassifiedPara 分类结果
type V2ClassifiedPara struct {
	Para    document.Paragraph
	Text    string
	Type    string
	ParaIdx int
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
			paras[i].Type = c.classifyBodyParagraphWithSignals(paras, i, normalized)

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

	// 后处理：封面段落重分类 — 在 zone=Cover 期间未正确识别为 cover 的标签段落重新标记
	reclassifyCoverLabels(paras)

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
			"hypothesisId":  "H3",
			"distribution":  dist,
			"ackAfterTitle": ackSamples,
			"enAbstractKW":  enSamples,
			"heading1_all":  h1Samples,
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

// reclassifyCoverLabels 将封面区内被误分为 body 的封面标签段落（题目/学院/专业等）重新标记为 V2Cover。
// 从文档开始扫描到第一个"摘要"或 heading_1，在此范围内的 body 段落若匹配封面标签则重分类。
func reclassifyCoverLabels(paras []V2ClassifiedPara) {
	// 找到封面区域的结束位置：第一个摘要标题、英文摘要、TOC 标题或 heading_1
	endIdx := len(paras)
	for i, p := range paras {
		normalized := normalizeSpaces(strings.TrimSpace(p.Text))
		if isAbstractTitleKW(normalized) || isEnAbstractTitleKW(normalized) ||
			isTOCTitleKW(normalized) || isHeading1(normalized) ||
			isAbstractStartKW(normalized) || isEnAbstractStartKW(normalized) {
			endIdx = i
			break
		}
	}

	// 封面标签关键词集合 — 精确匹配（整个文本就是该标签）或包含该标签
	coverLabels := []string{"题目", "学院", "专业", "班级", "学号", "姓名", "指导教师", "日期", "年级"}

	for i := 0; i < endIdx; i++ {
		if paras[i].Type != V2Body {
			continue
		}
		normalized := normalizeSpaces(strings.TrimSpace(paras[i].Text))
		// 已经是其他特殊类型（thesis_title / subtitle / heading）的跳过
		if paras[i].Type == V2ThesisTitle || paras[i].Type == V2ThesisSubtitle ||
			paras[i].Type == V2Heading1 || paras[i].Type == V2Cover {
			continue
		}
		for _, label := range coverLabels {
			if strings.Contains(normalized, label) && len([]rune(normalized)) <= 10 {
				paras[i].Type = V2Cover
				break
			}
		}
	}
}

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
	if reHeading1Ch.MatchString(s) || reHeading1List.MatchString(s) {
		return true
	}
	if reHeading1Num.MatchString(s) || reHeading1NumCN.MatchString(s) {
		first := s[0]
		if first >= '1' && first <= '9' && !reHeading2.MatchString(s) && !reHeading3.MatchString(s) {
			return true
		}
	}
	if match := reHeading1Compact.FindStringSubmatch(strings.TrimSpace(s)); len(match) == 3 && likelyShortHeadingTitle(match[2]) {
		return true
	}
	if strings.HasPrefix(s, "绪论") || strings.HasPrefix(s, "引言") || strings.HasPrefix(s, "结论") {
		return true
	}
	return false
}

func likelyShortHeadingTitle(title string) bool {
	title = strings.TrimSpace(title)
	return title != "" && len([]rune(title)) <= 32 && !strings.ContainsAny(title, "。！？；;，,")
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
	if isContinuationTableCaption(s) {
		return V2TableCaption
	}
	if looksLikeNarrativeSentence(s) {
		return V2Body
	}
	if reHeading4.MatchString(s) {
		return V2Heading4
	}
	if reHeading3.MatchString(s) {
		return V2Heading3
	}
	if reHeading2.MatchString(s) {
		return V2Heading2
	}
	if match := reHeading2Compact.FindStringSubmatch(strings.TrimSpace(s)); len(match) == 3 && likelyShortHeadingTitle(match[2]) {
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

func isContinuationTableCaption(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(normalized, "\u7eed\u8868") ||
		(strings.HasPrefix(normalized, "table ") && strings.Contains(normalized, "continued"))
}

func looksLikeNarrativeSentence(text string) bool {
	trimmed := strings.TrimSpace(text)
	if strings.ContainsAny(trimmed, "。！？；!?;") {
		return true
	}
	return len([]rune(trimmed)) > 25 && strings.ContainsAny(trimmed, "，,")
}

func (c *V2DeterministicClassifier) classifyBodyParagraphWithSignals(paras []V2ClassifiedPara, index int, text string) string {
	semantic := classifyBodyParagraph(text)
	if semantic != V2Body || c.processor == nil || c.processor.templateProfile == nil || len([]rune(text)) > 80 {
		return semantic
	}

	actual := extractParaFormatSpec(paras[index].Para)
	bestType, bestScore := semantic, c.combinedSignalScore(semantic, semantic, actual, templateStyle(c.processor.templateProfile, semantic), previousType(paras, index))
	for _, candidate := range []string{V2Heading1, V2Heading2, V2Heading3, V2Heading4} {
		score := c.combinedSignalScore(candidate, semantic, actual, templateStyle(c.processor.templateProfile, candidate), previousType(paras, index))
		if score > bestScore {
			bestType, bestScore = candidate, score
		}
	}
	if bestType != semantic && bestScore >= 0.62 {
		return bestType
	}
	return semantic
}

func (c *V2DeterministicClassifier) combinedSignalScore(candidate, semantic string, actual ParagraphFormatSpec, expected templateprofile.StyleRule, previous string) float64 {
	semanticScore := 0.0
	if candidate == semantic {
		semanticScore = 1
	} else if semantic == V2Body && strings.HasPrefix(candidate, "heading_") {
		semanticScore = 0.15
	}
	return 0.4*semanticScore + 0.4*matchFormatScore(actual, expected) + 0.2*sequenceScore(previous, candidate)
}

func templateStyle(profile *templateprofile.Profile, paraType string) templateprofile.StyleRule {
	if profile == nil {
		return templateprofile.StyleRule{}
	}
	sectionKey := map[string]string{
		V2Heading1: "chapter_title",
		V2Heading2: "section_title",
		V2Heading3: "subsection_title",
		V2Body:     "body_text",
	}[paraType]
	if style, ok := profile.SectionFormats[sectionKey]; ok {
		return style
	}
	styleKey := map[string]string{
		V2Heading1: "heading_1",
		V2Heading2: "heading_2",
		V2Heading3: "heading_3",
		V2Heading4: "heading_4",
		V2Body:     "body",
	}[paraType]
	return profile.Styles[styleKey]
}

func matchFormatScore(actual ParagraphFormatSpec, expected templateprofile.StyleRule) float64 {
	spec, ok := styleRuleToFormatSpec(expected)
	if !ok {
		return 0
	}
	score, weight := 0.0, 0.0
	if spec.FontSizeHalfPt > 0 && actual.FontSizeHalfPt > 0 {
		weight += 0.35
		delta := math.Abs(float64(spec.FontSizeHalfPt) - float64(actual.FontSizeHalfPt))
		if delta <= 1 {
			score += 0.35
		} else if delta <= 2 {
			score += 0.2
		}
	}
	if expected.BoldSet {
		weight += 0.2
		if actual.Bold == expected.Bold {
			score += 0.2
		}
	}
	if spec.AlignmentSet && actual.AlignmentSet {
		weight += 0.15
		if actual.Alignment == spec.Alignment {
			score += 0.15
		}
	}
	if spec.LineSpacingVal > 0 && actual.LineSpacingVal > 0 {
		weight += 0.1
		if math.Abs(float64(spec.LineSpacingVal-actual.LineSpacingVal)) <= 40 {
			score += 0.1
		}
	}
	if spec.FirstLineIndent > 0 || actual.FirstLineIndent > 0 {
		weight += 0.1
		if math.Abs(float64(spec.FirstLineIndent)-float64(actual.FirstLineIndent)) <= 80 {
			score += 0.1
		}
	}
	if expected.FontEastAsia != "" && actual.FontEastAsia != "" {
		weight += 0.1
		if strings.EqualFold(expected.FontEastAsia, actual.FontEastAsia) {
			score += 0.1
		}
	}
	if weight == 0 {
		return 0
	}
	return score / weight
}

func previousType(paras []V2ClassifiedPara, index int) string {
	for index--; index >= 0; index-- {
		if paras[index].Text != "" {
			return paras[index].Type
		}
	}
	return ""
}

func sequenceScore(previous, candidate string) float64 {
	if candidate == V2Body {
		return 1
	}
	if previous == candidate {
		return 0
	}
	if candidate == V2Heading1 && (previous == V2Heading2 || previous == V2Heading3 || previous == V2Heading4) {
		return 0
	}
	return 1
}
