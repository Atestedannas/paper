package fileprocessor

import (
	"log"
	"math"
	"regexp"
	"strings"
	"unicode"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
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
	V2Empty                  = "empty" // 空行：从分类分布统计与模板采样中剔除
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
	fe        FeatureExtractor // 第3层接缝：可注入 ML 特征抽取器（CRF/LightGBM）替换规则打分
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
			// 第2层：空行单独标 V2Empty，从分布统计与采样中剔除，避免污染正文判定
			paras[i].Type = V2Empty
			continue
		}

		normalized := normalizeSpaces(trimmed)

		// ── 第1层：结构信号直读（pStyle 命名样式 / outlineLvl / numPr）优先于关键词启发式 ──
		// 命中标题类样式信号的段落直接定类，不再走“关键词一票切区”。
		if st := structuralSignalType(paras[i].Para); st != "" {
			paras[i].Type = st
			if st == V2Heading1 {
				zone = ZoneBody
			} else if zone == ZoneCover {
				zone = ZoneBody
			}
			continue
		}

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
			// 第2层：参考文献/附录区增加出口条件——遇标题样式、大纲级别、居中加粗的
			// 短标题段即退出参考文献区，回到正文判定，不再“无脑吞正文”。
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
				paras[i].Type = V2NotesTitle
				continue
			}
			if c.refsExitSignal(paras[i].Para, normalized) || isHeading1(normalized) {
				zone = ZoneBody
				paras[i].Type = c.classifyBodyParagraphWithSignals(paras, i, normalized)
				continue
			}
			if reRefItem.MatchString(trimmed) || isReferenceContinuation(trimmed) {
				paras[i].Type = V2References
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

	// 第4层：结构合法性后处理 — 强制标题层级合法，低置信度标题段回退正文
	postProcessHeadingHierarchy(paras)

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
	// 第2层：多维特征加权打分（格式特征 + 内容特征 + 位置特征），
	// 取代“关键词一票切区”，并去掉“>80字放弃格式回判”的字数上限。
	semantic := classifyBodyParagraph(text)
	if semantic != V2Body || c.processor == nil || c.processor.templateProfile == nil {
		return semantic
	}

	// 走统一的特征抽取管线（第3层接缝，可被 ML 特征抽取器替换）
	fv := c.extractFeature(paras, index, text, semantic)

	// 内容反证：数字开头 + 含逗号句号的整句（如“3.5 万名受访者……”）是强正文信号
	if fv.IsNumericSentence {
		return V2Body
	}

	actual := extractParaFormatSpec(paras[index].Para)
	bestType, bestScore := semantic, c.combinedSignalScore(semantic, fv, actual, templateStyle(c.processor.templateProfile, semantic))
	for _, candidate := range []string{V2Heading1, V2Heading2, V2Heading3, V2Heading4} {
		score := c.combinedSignalScore(candidate, fv, actual, templateStyle(c.processor.templateProfile, candidate))
		if score > bestScore {
			bestType, bestScore = candidate, score
		}
	}
	if bestType != semantic && bestScore >= 0.62 {
		return bestType
	}
	return semantic
}

// ── 第3层接缝：特征抽取接口与数据管线 ────────────────────────────────
// CRF/LightGBM 等机器学习判定器暂未实现，但标识符/管道已预留：
// 只要实现 FeatureExtractor 并注入 classifier 即可替换规则打分。

// FeatureVector 段落的归一化特征向量（格式/内容/位置三维）
type FeatureVector struct {
	Semantic string // 基础规则语义判定结果（classifyBodyParagraph）
	// ── 格式特征 ──
	FontSizePt float64 // 主导字号（磅）
	IsBold     bool
	IsCenter   bool // 居中对齐
	HasIndent  bool // 存在缩进/首行缩进
	// ── 内容特征 ──
	RuneLen           int  // 文本长度（rune）
	NumericLevel      int  // 数字编号层级：0=无编号，1~4=1~4级编号
	HasSentencePunct  bool // 含句子结束标点 。！？；!?;
	IsNumericSentence bool // 数字开头且含逗号句号（正文反证）
	HasRefKW          bool // 命中参考文献相关关键词
	// ── 位置特征 ──
	PrevType       string // 前一个非空段类型
	ParagraphIndex int
	Confidence     float64 // 综合置信度（规则打分结果）
}

// FeatureExtractor 特征抽取接口 —— 第3层（CRF/LightGBM）可挂接的接缝
type FeatureExtractor interface {
	Extract(c *V2DeterministicClassifier, paras []V2ClassifiedPara, index int, text string) FeatureVector
}

// ruleFeatureExtractor 默认规则特征抽取器（可被 ML 实现替换）
type ruleFeatureExtractor struct{}

// V2DeterministicClassifier 增加可注入的特征抽取器字段（第3层接缝）
// (字段已在结构体定义处增加 fe FeatureExtractor)

// extractFeature 派出默认/注入的特征抽取器
func (c *V2DeterministicClassifier) extractFeature(paras []V2ClassifiedPara, index int, text string, semantic string) FeatureVector {
	fe := c.fe
	if fe == nil {
		fe = ruleFeatureExtractor{}
	}
	return fe.Extract(c, paras, index, text)
}

// Extract 规则版特征抽取：从格式/内容/位置三维归一化
func (ruleFeatureExtractor) Extract(c *V2DeterministicClassifier, paras []V2ClassifiedPara, index int, text string) FeatureVector {
	fv := FeatureVector{Semantic: classifyBodyParagraph(text), ParagraphIndex: index}
	runes := []rune(text)
	fv.RuneLen = len(runes)
	fv.HasSentencePunct = strings.ContainsAny(text, "。！？；!?;")

	// 内容：数字编号层级 + 正文反证
	trimmed := strings.TrimSpace(text)
	fv.NumericLevel = numericLevelOf(trimmed)
	fv.IsNumericSentence = isNumericSentence(text, fv.NumericLevel >= 1)
	fv.HasRefKW = isReferencesTitleKW(trimmed) || strings.Contains(trimmed, "参考文献")

	// 格式：主导 run 格式 + 段落级格式（首行缩进/行距等）
	if c != nil && c.processor != nil {
		spec := extractParaFormatSpec(paras[index].Para)
		fv.FontSizePt = spec.FontSizePt()
		fv.IsBold = spec.Bold
		fv.IsCenter = spec.AlignmentSet && spec.Alignment == wml.ST_JcCenter
		fv.HasIndent = spec.FirstLineIndent > 0 || spec.IndentLeft > 0 || spec.IndentRight > 0
	}

	// 位置：前一个非空段类型
	fv.PrevType = previousType(paras, index)
	return fv
}

// numericLevelOf 由文本头部判定数字编号层级（0=无）
func numericLevelOf(trimmed string) int {
	switch {
	case reHeading4.MatchString(trimmed):
		return 4
	case reHeading3.MatchString(trimmed):
		return 3
	case reHeading2.MatchString(trimmed):
		if !reRefItem.MatchString(trimmed) {
			return 2
		}
		return 0
	case reHeading1Ch.MatchString(trimmed), reHeading1List.MatchString(trimmed):
		return 1
	case reHeading1Num.MatchString(trimmed) || reHeading1NumCN.MatchString(trimmed):
		first := trimmed[0]
		if first >= '1' && first <= '9' && !reHeading2.MatchString(trimmed) {
			return 1
		}
		return 0
	}
	if m := reHeading1Compact.FindStringSubmatch(trimmed); len(m) == 3 {
		return 1
	}
	return 0
}

// isNumericSentence 数字开头 + 含逗号/句号 → 强正文反证（如“3.5 万名受访者……”）
func isNumericSentence(text string, numericPrefix bool) bool {
	if !numericPrefix {
		return false
	}
	trimmed := strings.TrimSpace(text)
	if strings.ContainsAny(trimmed, "。！？；!?;") {
		return true
	}
	// “3.5 万名受访者超过，……” 或 “2.5 亿人次，……”
	return len([]rune(trimmed)) > 12 && strings.ContainsAny(trimmed, "，,")
}

// combinedSignalScore 多维加权综合评分：语义 0.25 + 内容 0.30 + 格式 0.30 + 位置 0.15
func (c *V2DeterministicClassifier) combinedSignalScore(candidate string, fv FeatureVector, actual ParagraphFormatSpec, expected templateprofile.StyleRule) float64 {
	semanticScore := 0.0
	if candidate == fv.Semantic {
		semanticScore = 1
	} else if fv.Semantic == V2Body && strings.HasPrefix(candidate, "heading_") {
		semanticScore = 0.15
	}
	match := matchFormatScore(actual, expected)
	sem := 0.25*semanticScore + 0.30*contentSignalScore(candidate, fv) + 0.30*match + 0.15*positionSignalScore(fv.PrevType, candidate)
	// 强模板格式辅助：无编号、加粗/居中的短文段若与目标标题模板格式几乎完全吻合
	// （match ≥ 0.9），格式信号足以支撑其成为小节标题——补足 0.10 使其越过 0.62 的
	// 切换阈值；并列同级标题的位置分只有 0.5，补足后仍停在阈值之下，判正文行为不受影响。
	if strings.HasPrefix(candidate, "heading_") && match >= 0.9 && fv.NumericLevel == 0 &&
		(fv.IsBold || fv.IsCenter) && fv.RuneLen > 0 && fv.RuneLen <= 20 {
		sem += 0.10
	}
	if sem >= 0 {
		fv.Confidence = sem
	}
	return sem
}

// contentSignalScore 内容特征：编号正则优先级、段长、标点、参考文献关键词
func contentSignalScore(candidate string, fv FeatureVector) float64 {
	if candidate == V2Body {
		base := 0.55
		if fv.HasSentencePunct {
			base += 0.2
		}
		if fv.RuneLen > 40 {
			base += 0.1
		}
		if fv.HasRefKW {
			base += 0.15
		}
		if base > 1 {
			return 1
		}
		return base
	}
	// 标题候选
	level := headingLevelOf(candidate)
	if level >= 1 && fv.NumericLevel == level {
		return 0.8 // 编号层级与候选标题完全匹配
	}
	if fv.NumericLevel >= 1 && !fv.IsNumericSentence {
		return 0.2 // 有编号但层级不符
	}
	if fv.IsCenter || fv.IsBold {
		return 0.3 // 无编号但居中/加粗，可能是短标题
	}
	return 0.05
}

// positionSignalScore 位置特征：前文类型、连续同级标题是否合理
// （body 不再恒为 1；连续同级标题给 0.5，避免 0.2 权重把并列小节拉低）
func positionSignalScore(previous, candidate string) float64 {
	if previous == "" {
		return 0.6
	}
	if candidate == V2Body {
		if previous == V2Body {
			return 0.8
		}
		return 0.6
	}
	cur := headingLevelOf(candidate)
	prev := headingLevelOf(previous)
	if cur >= 1 && prev == cur {
		return 0.5 // 同级标题连续（并列小节）
	}
	if cur >= 1 && prev > 0 && prev < cur {
		return 0.75 // 从浅层标题进入深层小节（1→2、2→3）
	}
	if cur >= 1 && prev > 0 && prev > cur {
		return 0.25 // 突然跳回更浅层级（2→1），低分，需格式内容支撑
	}
	return 0.6
}

// headingLevelOf 返回段落类型的标题层级（0=非标题）
func headingLevelOf(ptype string) int {
	switch ptype {
	case V2Heading1:
		return 1
	case V2Heading2:
		return 2
	case V2Heading3:
		return 3
	case V2Heading4:
		return 4
	}
	return 0
}

// structuralSignalType 第1层：结构信号直读。
// 优先读取段落的结构化信号：pStyle 命名样式、outlineLvl 大纲级别、多级编号样式，
// 命中即直接定类，绕过“关键词一票切区”的启发式。
func structuralSignalType(para document.Paragraph) string {
	if para.X().PPr == nil {
		return ""
	}
	var style string
	if para.X().PPr.PStyle != nil {
		style = strings.ToLower(strings.TrimSpace(para.X().PPr.PStyle.ValAttr))
	}
	switch {
	case isHeadingStyleName(style, "heading1", "heading 1", "h1", "标题1", "一级标题"):
		return V2Heading1
	case isHeadingStyleName(style, "heading2", "heading 2", "h2", "标题2", "二级标题"):
		return V2Heading2
	case isHeadingStyleName(style, "heading3", "heading 3", "h3", "标题3", "三级标题"):
		return V2Heading3
	case isHeadingStyleName(style, "heading4", "heading 4", "h4", "标题4", "四级标题"):
		return V2Heading4
	}
	// 大纲级别直读（outlineLvl: 0=1级标题，以此类推）
	if para.X().PPr.OutlineLvl != nil {
		lvl := int(para.X().PPr.OutlineLvl.ValAttr) + 1
		switch {
		case lvl == 1:
			return V2Heading1
		case lvl == 2:
			return V2Heading2
		case lvl == 3:
			return V2Heading3
		case lvl >= 4:
			return V2Heading4
		}
	}
	// 多级编号（numPr）且文本满足编号标题形态 → 按编号层级定类
	if para.X().PPr.NumPr != nil {
		text := normalizeSpaces(strings.TrimSpace(extractParaPlainText(para)))
		if lvl := numericLevelOf(text); lvl >= 1 {
			switch lvl {
			case 1:
				return V2Heading1
			case 2:
				return V2Heading2
			case 3:
				return V2Heading3
			}
			return V2Heading4
		}
	}
	return ""
}

// isHeadingStyleName 判定样式名是否命中某标题样式族
func isHeadingStyleName(style string, names ...string) bool {
	if style == "" {
		return false
	}
	for _, n := range names {
		if style == n {
			return true
		}
	}
	if strings.HasPrefix(style, "heading") {
		return true
	}
	return false
}

// extractParaPlainText 返回段落的纯文本（仅供结构信号省略用，性能敏感路径请用 processor）
func extractParaPlainText(para document.Paragraph) string {
	var b strings.Builder
	for _, run := range para.Runs() {
		b.WriteString(run.Text())
	}
	return b.String()
}

// refsExitSignal 参考文献/附录区出口条件：
// 遇标题样式、大纲级别、或“居中加粗的短标题段”即退出参考文献区。
func (c *V2DeterministicClassifier) refsExitSignal(para document.Paragraph, text string) bool {
	if para.X().PPr == nil {
		return false
	}
	if para.X().PPr.PStyle != nil {
		style := strings.ToLower(strings.TrimSpace(para.X().PPr.PStyle.ValAttr))
		if strings.HasPrefix(style, "heading") || strings.Contains(style, "标题") {
			return true
		}
	}
	if para.X().PPr.OutlineLvl != nil {
		return true
	}
	spec := extractParaFormatSpec(para)
	if spec.Bold && spec.Alignment == wml.ST_JcCenter && len([]rune(text)) <= 30 {
		return true
	}
	return false
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
		weight += 0.25
		delta := math.Abs(float64(spec.FontSizeHalfPt) - float64(actual.FontSizeHalfPt))
		if delta <= 1 {
			score += 0.25
		} else if delta <= 2 {
			score += 0.15
		}
	}
	// 第2层：中文（Complex Script）字号比对——控制中文字体的精准字号（w:szCs）
	if spec.FontSizeCSHalfPt > 0 && actual.FontSizeCSHalfPt > 0 {
		weight += 0.15
		delta := math.Abs(float64(spec.FontSizeCSHalfPt) - float64(actual.FontSizeCSHalfPt))
		if delta <= 1 {
			score += 0.15
		} else if delta <= 2 {
			score += 0.08
		}
	}
	if expected.BoldSet {
		weight += 0.15
		if actual.Bold == expected.Bold {
			score += 0.15
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

// ── 第4层：结构合法性后处理 ──────────────────────────────────────────
// 在确定性分类完成后执行：强制标题层级合法（不跳级），并把“数字编号正则
// 误判的数据/公式形态行”回退为正文，避免错误标题被格式化层强制写格式放大。

// reHeadingNumStrip 剥离行首的数字/中文序数编号（“1.2.3 ”、“1、”、“第3 ”等），
// 用于判断编号后的正文主体是否真为标题语。
var reHeadingNumStrip = regexp.MustCompile(`^\s{0,4}[0-9一二三四五六七八九十百]+[．.、]\s*`)

// typeOfHeadingLevel 由标题层级反查段落类型标签
func typeOfHeadingLevel(lv int) string {
	switch lv {
	case 1:
		return V2Heading1
	case 2:
		return V2Heading2
	case 3:
		return V2Heading3
	case 4:
		return V2Heading4
	}
	return V2Body
}

// isDataShapedLine 判断剥去编号前缀后的行主体是否为“数据/公式”形态：
// 纯数字或主体几乎无汉字（数字、单位、符号），这类段落高度疑似被编号正则
// 误判成标题的数据行（如 “3.14”、”5.0MHz”、”2020 ”）。
func isDataShapedLine(text string) bool {
	content := strings.TrimSpace(reHeadingNumStrip.ReplaceAllString(strings.TrimSpace(text), ""))
	// 纯编号（如“1.2.3 ”、“3、”）——剥净后无内容，直接判为数据/占位行
	if content == "" {
		return true
	}
	han := 0
	lenRunes := 0
	for _, r := range content {
		if r == '。' || r == '，' || r == '；' {
			return false // 含句读，更像叙述句而非数据行
		}
		lenRunes++
		if unicode.Is(unicode.Han, r) {
			han++
		}
	}
	if lenRunes == 0 {
		return true
	}
	// 主体极短且汉字占比过低（<1/3）——如 “5.0MHz”、“2020” → 数据行
	return lenRunes <= 16 && float64(han)/float64(lenRunes) < 0.34
}

// postProcessHeadingHierarchy 第4层：强制标题层级合法。
// 规则：
//  1. 数据/公式形态且未命中第1层结构信号（pStyle/outlineLvl/numPr）的编号行 → 回退正文；
//  2. 标题层级不得跳级：当前标题层级大于上一标题层级+1 时，强制降级为上一级+1；
//     此后同章节子级标题一并按修正后的层级纪律约束。
func postProcessHeadingHierarchy(paras []V2ClassifiedPara) {
	lastLevel := 0 // 最近一次出现的标题层级，0 表示尚未出现
	for i := range paras {
		lv := headingLevelOf(paras[i].Type)
		if lv == 0 {
			continue
		}

		// 1) 数据/公式形态回退：命中第1层结构信号（样式/大纲/编号）便信任结构，不做数据回退
		if structuralSignalType(paras[i].Para) == "" && isDataShapedLine(paras[i].Text) {
			paras[i].Type = V2Body
			lastLevel = 0 // 该误判标题不参与后续层级纪律
			continue
		}

		// 2) 层级纪律：禁止跳级（如 1 → 3），跳级则降级为上一级 + 1
		if lastLevel > 0 && lv > lastLevel+1 {
			paras[i].Type = typeOfHeadingLevel(lastLevel + 1)
		}
		lastLevel = headingLevelOf(paras[i].Type)
	}
}
