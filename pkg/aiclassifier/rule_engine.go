package aiclassifier

import (
	"fmt"
	"strings"
)

// ClassifyResult 分类结果
type ClassifyResult struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"` // 0.0 ~ 1.0
	Source     string  `json:"source"`     // "rule" / "local_model" / "ai"
	Level      int     `json:"level"`      // 标题级别，0=非标题
}

// RuleEngine 基于规则的段落分类引擎
// 将原有 intelligentClassifyParagraphWithLevel 的逻辑重构为带置信度的版本
type RuleEngine struct{}

// NewRuleEngine 创建规则引擎
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{}
}

// Classify 使用规则对段落特征进行分类，返回结果和置信度
func (r *RuleEngine) Classify(f *ParagraphFeature) ClassifyResult {
	text := strings.TrimSpace(f.Text)
	runeLen := f.RuneLength

	// 空文本或过短
	if runeLen == 0 {
		return ClassifyResult{Label: TypeBody, Confidence: 0.5, Source: "rule", Level: 0}
	}

	// ────────── 目录条目（最先检查，因为目录条目可能包含标题样式的文本） ──────────
	if f.HasTOCIndicator {
		normalized := text
		if runeLen < 30 {
			normalized = strings.ReplaceAll(text, " ", "")
		}
		normalizedLower := strings.ToLower(normalized)
		if strings.Contains(normalizedLower, "目录") {
			return ClassifyResult{Label: TypeTOCTitle, Confidence: 0.98, Source: "rule", Level: 0}
		}
		return ClassifyResult{Label: TypeTOC, Confidence: 0.95, Source: "rule", Level: 0}
	}

	// ────────── 封面识别 ──────────
	if f.HasCoverKeywords {
		return ClassifyResult{Label: TypeCover, Confidence: 0.95, Source: "rule", Level: 0}
	}
	// 短文本含"大学"/"学院" + 毕业相关词
	if runeLen < 25 && f.HasChinese {
		if containsAny(text, "大学", "学院", "学校") && containsAny(text, "毕业", "学士", "硕士", "博士") {
			return ClassifyResult{Label: TypeCover, Confidence: 0.92, Source: "rule", Level: 0}
		}
	}

	// ────────── 原创性声明识别 ──────────
	if f.HasOriginalityKW {
		return ClassifyResult{Label: TypeOriginalityDeclaration, Confidence: 0.96, Source: "rule", Level: 0}
	}

	// ────────── 目录标题（不含 tab 的独立"目录"段落） ──────────
	normalized := text
	if runeLen < 30 {
		normalized = strings.ReplaceAll(text, " ", "")
	}
	normalizedLower := strings.ToLower(normalized)

	if strings.Contains(normalizedLower, "目录") && len([]rune(normalized)) < 10 {
		return ClassifyResult{Label: TypeTOCTitle, Confidence: 0.96, Source: "rule", Level: 0}
	}

	// ────────── 摘要识别 ──────────
	if f.HasAbstractKW {
		if f.HasChinese && strings.Contains(normalizedLower, "摘要") {
			if len([]rune(normalized)) < 20 {
				return ClassifyResult{Label: TypeAbstractTitle, Confidence: 0.97, Source: "rule", Level: 0}
			}
			return ClassifyResult{Label: TypeAbstract, Confidence: 0.90, Source: "rule", Level: 0}
		}
		textLower := strings.ToLower(text)
		if !f.HasChinese && strings.Contains(textLower, "abstract") {
			if len(text) < 30 {
				return ClassifyResult{Label: TypeEnAbstractTitle, Confidence: 0.96, Source: "rule", Level: 0}
			}
			return ClassifyResult{Label: TypeEnAbstract, Confidence: 0.85, Source: "rule", Level: 0}
		}
	}

	// ────────── 关键词识别 ──────────
	if f.HasKeywordsKW {
		if f.HasChinese {
			return ClassifyResult{Label: TypeKeywords, Confidence: 0.96, Source: "rule", Level: 0}
		}
		return ClassifyResult{Label: TypeEnKeywords, Confidence: 0.95, Source: "rule", Level: 0}
	}

	// ────────── 各级标题（语义+结构+字号三重验证） ──────────
	level := DetectHeadingLevel(text)
	if level > 0 {
		conf := 0.90

		// 字体大小信号：中文论文标题字号远大于正文
		// heading_1 典型字号 16pt(三号), heading_2 15pt(小三), heading_3 14pt(四号)
		// 正文 12pt(小四)
		fontSize := f.FontSizePt
		switch level {
		case 1:
			if fontSize >= 15.5 {
				conf += 0.05 // 16pt → 强烈支持
			} else if fontSize >= 14.0 {
				conf += 0.02
			} else if fontSize > 0 && fontSize < 13.0 {
				conf -= 0.35 // 12pt 正文大小 → 很可能是误判
			}
		case 2:
			if fontSize >= 14.5 && fontSize <= 16.0 {
				conf += 0.05
			} else if fontSize > 0 && fontSize < 13.0 {
				conf -= 0.30
			}
		case 3:
			if fontSize >= 13.5 && fontSize <= 15.0 {
				conf += 0.03
			} else if fontSize > 0 && fontSize < 12.5 {
				conf -= 0.20
			}
		}

		// 加粗信号：标题通常加粗
		if f.IsBold {
			conf += 0.04
		} else if level == 1 {
			conf -= 0.05 // 一级标题不加粗很可疑
		}

		// 短文本 + 无句号 → 更可能是标题
		if runeLen < 50 {
			conf += 0.02
		}
		if f.EndsWithPeriod {
			conf -= 0.20 // 以句号结尾 → 几乎不是标题
		}
		// 太长的文本不太可能是标题
		if runeLen > 80 {
			conf -= 0.25
		}

		if conf > 1.0 {
			conf = 1.0
		}
		if conf < 0.5 {
			conf = 0.5
		}

		// 置信度 < 0.65 时降级为正文（交给 AI 仲裁）
		if conf < 0.65 {
			return ClassifyResult{Label: TypeBody, Confidence: 1.0 - conf, Source: "rule", Level: 0}
		}

		return ClassifyResult{
			Label:      fmt.Sprintf("heading_%d", level),
			Confidence: conf,
			Source:     "rule",
			Level:      level,
		}
	}

	// ────────── 论文标题识别 ──────────
	if r.isTitleParagraph(text, f) {
		return ClassifyResult{Label: TypeTitle, Confidence: 0.80, Source: "rule", Level: 0}
	}

	// ────────── 参考文献识别 ──────────
	if f.HasReferencesKW {
		if runeLen < 20 {
			return ClassifyResult{Label: TypeReferencesTitle, Confidence: 0.95, Source: "rule", Level: 0}
		}
		return ClassifyResult{Label: TypeReferences, Confidence: 0.88, Source: "rule", Level: 0}
	}
	if r.isReferenceItem(text) {
		return ClassifyResult{Label: TypeReferences, Confidence: 0.92, Source: "rule", Level: 0}
	}

	// ────────── 默认：正文（置信度取决于文本特征） ──────────
	bodyConf := 0.70
	if runeLen > 30 && f.EndsWithPeriod {
		bodyConf = 0.85
	}
	if runeLen > 100 {
		bodyConf = 0.90
	}
	return ClassifyResult{Label: TypeBody, Confidence: bodyConf, Source: "rule", Level: 0}
}

// isTitleParagraph 论文标题判断
func (r *RuleEngine) isTitleParagraph(text string, f *ParagraphFeature) bool {
	runeLen := f.RuneLength
	if runeLen < 5 || runeLen > 60 {
		return false
	}
	if f.EndsWithPeriod {
		return false
	}
	titleKWs := []string{"毕业设计", "毕业论文", "学士学位论文", "硕士学位论文", "博士学位论文"}
	for _, kw := range titleKWs {
		if strings.Contains(text, kw) {
			return true
		}
	}
	if runeLen < 30 {
		researchKWs := []string{"的研究", "的设计", "的分析", "的应用", "的构建", "系统设计", "系统分析", "系统开发", "系统实现"}
		for _, kw := range researchKWs {
			if strings.Contains(text, kw) {
				return true
			}
		}
	}
	return false
}

// isReferenceItem 参考文献条目判断
func (r *RuleEngine) isReferenceItem(text string) bool {
	text = strings.TrimSpace(text)
	if len(text) < 30 {
		return false
	}
	return reRefItem.MatchString(text) || reRefItemDot.MatchString(text)
}

// containsAny 检查 text 是否包含 any 中的任一子串
func containsAny(text string, substrs ...string) bool {
	for _, s := range substrs {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}
