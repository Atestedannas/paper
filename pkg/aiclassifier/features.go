package aiclassifier

import (
	"regexp"
	"strings"
	"unicode"
)

// ParagraphType 段落类型常量
const (
	TypeTitle                    = "title"
	TypeCover                    = "cover"
	TypeOriginalityDeclaration   = "originality_declaration"
	TypeAbstractTitle            = "abstract_title"
	TypeAbstract                 = "abstract"
	TypeEnAbstractTitle          = "en_abstract_title"
	TypeEnAbstract               = "en_abstract"
	TypeKeywords                 = "keywords"
	TypeEnKeywords               = "en_keywords"
	TypeHeading1                 = "heading_1"
	TypeHeading2                 = "heading_2"
	TypeHeading3                 = "heading_3"
	TypeBody                     = "body"
	TypeReferencesTitle          = "references_title"
	TypeReferences               = "references"
	TypeTOCTitle                 = "table_of_contents_title"
	TypeTOC                      = "table_of_contents"
)

// ParagraphFeature 段落特征向量（用于分类和模型训练）
type ParagraphFeature struct {
	// 文本特征
	TextLength    int     `json:"text_length"`
	RuneLength    int     `json:"rune_length"`
	HasChinese    bool    `json:"has_chinese"`
	ChineseRatio  float64 `json:"chinese_ratio"`

	// 格式特征（从 DOCX Run 属性提取）
	FontSizePt float64 `json:"font_size_pt"`
	IsBold     bool    `json:"is_bold"`
	Alignment  string  `json:"alignment"` // left / center / right / justify

	// 位置特征
	ParaIndex     int     `json:"para_index"`
	PositionRatio float64 `json:"position_ratio"` // 0.0 ~ 1.0

	// 关键词命中
	HasNumberPrefix  bool `json:"has_number_prefix"`
	HasChapterMark   bool `json:"has_chapter_mark"`
	HasAbstractKW    bool `json:"has_abstract_kw"`
	HasKeywordsKW    bool `json:"has_keywords_kw"`
	HasReferencesKW  bool `json:"has_references_kw"`
	HasTOCIndicator  bool `json:"has_toc_indicator"`
	HasCoverKeywords   bool `json:"has_cover_keywords"`
	HasOriginalityKW   bool `json:"has_originality_kw"`

	// 结构特征
	StartsWithDigitDot bool `json:"starts_with_digit_dot"` // 1.1 or 1.1.1
	EndsWithPeriod     bool `json:"ends_with_period"`
	HasTab             bool `json:"has_tab"`

	// 上下文（分类后回填）
	PrevType string `json:"prev_type"`
	NextType string `json:"next_type"`

	// 原始文本片段
	Text string `json:"text"`
}

// ToFloat64Slice 将特征转为数值向量（用于决策树）
func (f *ParagraphFeature) ToFloat64Slice() []float64 {
	b := func(v bool) float64 {
		if v {
			return 1.0
		}
		return 0.0
	}
	return []float64{
		float64(f.RuneLength),
		f.FontSizePt,
		b(f.IsBold),
		f.PositionRatio,
		f.ChineseRatio,
		b(f.HasNumberPrefix),
		b(f.HasChapterMark),
		b(f.HasAbstractKW),
		b(f.HasKeywordsKW),
		b(f.HasReferencesKW),
		b(f.HasTOCIndicator),
		b(f.HasCoverKeywords),
		b(f.HasOriginalityKW),
		b(f.StartsWithDigitDot),
		b(f.EndsWithPeriod),
		b(f.HasTab),
	}
}

// FeatureNames 对应 ToFloat64Slice 的特征名
var FeatureNames = []string{
	"rune_length", "font_size_pt", "is_bold", "position_ratio", "chinese_ratio",
	"has_number_prefix", "has_chapter_mark", "has_abstract_kw",
	"has_keywords_kw", "has_references_kw", "has_toc_indicator",
	"has_cover_keywords", "has_originality_kw", "starts_with_digit_dot", "ends_with_period", "has_tab",
}

// 预编译正则
var (
	reLevel1ChNum   = regexp.MustCompile(`^[一二三四五六七八九十]+[、.]`)
	reLevel1Digit   = regexp.MustCompile(`^[0-9]+[、.]`)
	reLevel1Chapter = regexp.MustCompile(`^第[一二三四五六七八九十]+[章节]`)
	reLevel1Space   = regexp.MustCompile(`^[0-9]+\s+[^\d]`)
	reLevel2        = regexp.MustCompile(`^[0-9]+\.[0-9]`)
	reLevel2ChParen = regexp.MustCompile(`^[（(][一二三四五六七八九十]+[)）]`)
	reLevel3        = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+`)
	reLevel3Paren   = regexp.MustCompile(`^[（(][0-9]+[)）]`)
	reRefItem       = regexp.MustCompile(`^\[[0-9]+\]`)
	reRefItemDot    = regexp.MustCompile(`^[0-9]+\.\s`)
)

// ExtractFeatures 从文本和格式信息中提取特征
func ExtractFeatures(text string, paraIndex, totalParas int, fontSizePt float64, isBold bool, alignment string) ParagraphFeature {
	trimmed := strings.TrimSpace(text)
	runes := []rune(trimmed)
	runeLen := len(runes)

	chineseCount := 0
	for _, r := range runes {
		if r >= '\u4e00' && r <= '\u9fff' {
			chineseCount++
		}
	}
	chineseRatio := 0.0
	if runeLen > 0 {
		chineseRatio = float64(chineseCount) / float64(runeLen)
	}

	positionRatio := 0.0
	if totalParas > 0 {
		positionRatio = float64(paraIndex) / float64(totalParas)
	}

	// 空格归一化文本（用于关键词匹配）
	normalized := trimmed
	if runeLen < 30 {
		normalized = strings.ReplaceAll(trimmed, " ", "")
	}
	normalizedLower := strings.ToLower(normalized)
	textLower := strings.ToLower(trimmed)

	f := ParagraphFeature{
		TextLength:    len(trimmed),
		RuneLength:    runeLen,
		HasChinese:    chineseCount > 0,
		ChineseRatio:  chineseRatio,
		FontSizePt:    fontSizePt,
		IsBold:        isBold,
		Alignment:     alignment,
		ParaIndex:     paraIndex,
		PositionRatio: positionRatio,
		Text:          trimmed,
	}

	// 关键词命中
	f.HasAbstractKW = strings.Contains(normalizedLower, "摘要") || (!f.HasChinese && strings.Contains(textLower, "abstract"))
	f.HasKeywordsKW = strings.Contains(normalizedLower, "关键词") || strings.Contains(normalizedLower, "关键字") ||
		(!f.HasChinese && (strings.Contains(textLower, "keywords") || strings.Contains(textLower, "key words")))
	f.HasReferencesKW = strings.Contains(textLower, "参考文献") || strings.Contains(textLower, "references")

	// 目录指标：直接调用 isTOCLike，统一处理全角点/半角点/Tab等所有格式
	f.HasTab = strings.Contains(trimmed, "\t")
	f.HasTOCIndicator = isTOCLike(trimmed)

	// 封面关键词
	coverKWs := []string{"毕业设计", "毕业论文", "学士学位论文", "硕士学位论文", "博士学位论文"}
	for _, kw := range coverKWs {
		if strings.Contains(trimmed, kw) {
			f.HasCoverKeywords = true
			break
		}
	}

	// 原创性声明关键词
	originalityKWs := []string{"原创性声明", "版权声明", "学位论文原创性声明", "原创性申明", "学术诚信声明", "诚信声明", "信誉声明", "信誉保证"}
	for _, kw := range originalityKWs {
		if strings.Contains(normalizedLower, kw) {
			f.HasOriginalityKW = true
			break
		}
	}

	// 章节号码特征
	f.HasChapterMark = reLevel1Chapter.MatchString(trimmed) || reLevel1ChNum.MatchString(trimmed)
	f.HasNumberPrefix = reLevel1Digit.MatchString(trimmed) || reLevel1Space.MatchString(trimmed) ||
		reLevel2.MatchString(trimmed) || reLevel3.MatchString(trimmed)
	f.StartsWithDigitDot = reLevel2.MatchString(trimmed) || reLevel3.MatchString(trimmed)

	// 结尾标点
	if runeLen > 0 {
		last := runes[runeLen-1]
		f.EndsWithPeriod = last == '。' || last == '.'
	}

	return f
}

// isTOCLike 判断是否为目录条目
// 支持中文全角点"．"（U+FF0E）和Tab+页码两种目录格式
func isTOCLike(text string) bool {
	if strings.Contains(text, "\t") {
		parts := strings.Split(text, "\t")
		lastPart := strings.TrimSpace(parts[len(parts)-1])
		for _, r := range lastPart {
			if !unicode.IsDigit(r) {
				return false
			}
		}
		return len(lastPart) > 0
	}
	// 全角点"．"（U+FF0E）是中文目录的标准填充符，≥2个即为目录条目
	fullWidthDots := strings.Count(text, "．")
	asciiDots := strings.Count(text, ".")
	ellipsis := strings.Count(text, "…")
	middleDots := strings.Count(text, "·")
	return fullWidthDots >= 2 || asciiDots > 5 || ellipsis > 2 || middleDots > 5
}

// DetectHeadingLevel 从文本检测标题级别 (0=非标题, 1/2/3=对应级别)
func DetectHeadingLevel(text string) int {
	text = strings.TrimSpace(text)
	if reLevel3.MatchString(text) {
		return 3
	}
	if reLevel3Paren.MatchString(text) {
		if isParenNumberedHeading(text) {
			return 3
		}
		return 0
	}
	if reLevel2.MatchString(text) || reLevel2ChParen.MatchString(text) {
		return 2
	}
	if reLevel1ChNum.MatchString(text) || reLevel1Digit.MatchString(text) ||
		reLevel1Chapter.MatchString(text) || reLevel1Space.MatchString(text) {
		return 1
	}
	return 0
}

// isParenNumberedHeading 判断 (n)/（n）开头的段落是否为标题（而非正文枚举项）。
// 短文本（去除编号后 ≤20 个字符）视为标题，长文本视为正文条目。
func isParenNumberedHeading(text string) bool {
	loc := reLevel3Paren.FindStringIndex(text)
	if loc == nil {
		return false
	}
	rest := []rune(strings.TrimSpace(text[loc[1]:]))
	return len(rest) <= 20
}
