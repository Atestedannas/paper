package aiclassifier

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// AIArbitrator 使用 DeepSeek AI 进行段落分类仲裁
type AIArbitrator struct {
	client  *DeepSeekWebClient
	enabled bool
}

// NewAIArbitrator 创建 AI 仲裁器
func NewAIArbitrator(cookie, bearer string, enabled bool) *AIArbitrator {
	var client *DeepSeekWebClient
	if enabled && cookie != "" {
		client = NewDeepSeekWebClient(cookie, bearer)
	}
	return &AIArbitrator{
		client:  client,
		enabled: enabled && client != nil,
	}
}

// IsEnabled 是否启用
func (a *AIArbitrator) IsEnabled() bool {
	return a.enabled
}

// ClassifyBatch 批量分类段落（一次 AI 调用处理多个段落，降低成本）
func (a *AIArbitrator) ClassifyBatch(paragraphs []ParagraphFeature, ruleResults []ClassifyResult) ([]ClassifyResult, error) {
	if !a.enabled || a.client == nil {
		return nil, fmt.Errorf("AI arbitrator is disabled")
	}

	// 构建 prompt：只发送规则引擎低置信度的段落
	type paraItem struct {
		Index   int    `json:"index"`
		Text    string `json:"text"`
		RuleRes string `json:"rule_result"`
	}
	var items []paraItem
	indexMap := make(map[int]int) // prompt index -> original index

	for i, r := range ruleResults {
		if r.Confidence < 0.90 {
			snippet := paragraphs[i].Text
			if len([]rune(snippet)) > 100 {
				snippet = string([]rune(snippet)[:100]) + "..."
			}
			indexMap[len(items)] = i
			items = append(items, paraItem{
				Index:   i,
				Text:    snippet,
				RuleRes: r.Label,
			})
		}
	}

	if len(items) == 0 {
		return ruleResults, nil
	}

	// 限制单次发送数量
	if len(items) > 30 {
		items = items[:30]
	}

	itemsJSON, _ := json.Marshal(items)

	prompt := fmt.Sprintf(`你是一个学术论文格式分析专家。请对以下段落进行分类。

可用的分类标签:
- title: 论文标题
- cover: 封面内容
- originality_declaration: 原创性声明（含声明正文、签名区域）
- abstract_title: 中文摘要标题
- abstract: 中文摘要正文
- en_abstract_title: 英文摘要标题
- en_abstract: 英文摘要正文
- keywords: 中文关键词
- en_keywords: 英文关键词
- heading_1: 一级标题
- heading_2: 二级标题
- heading_3: 三级标题
- body: 正文
- references_title: 参考文献标题
- references: 参考文献条目
- table_of_contents_title: 目录标题
- table_of_contents: 目录条目

请对每个段落返回JSON数组，格式: [{"index": 0, "label": "body", "confidence": 0.95}]
只返回JSON，不要其他文字。

段落列表:
%s`, string(itemsJSON))

	log.Printf("\n[AI仲裁] 发送 %d 个低置信度段落给 DeepSeek\n"+
		"  总段落数: %d\n"+
		"  需仲裁数: %d",
		len(items), len(paragraphs), len(items))
	response, err := a.client.ChatCompletion(prompt)
	if err != nil {
		log.Printf("[AI仲裁] DeepSeek 调用失败: %v", err)
		return nil, err
	}

	// 解析 AI 回复
	aiResults := parseAIResponse(response)

	// 合并结果
	results := make([]ClassifyResult, len(ruleResults))
	copy(results, ruleResults)

	for promptIdx, aiRes := range aiResults {
		if origIdx, ok := indexMap[promptIdx]; ok && origIdx < len(results) {
			results[origIdx] = ClassifyResult{
				Label:      aiRes.Label,
				Confidence: aiRes.Confidence,
				Source:     "ai",
				Level:      detectLevelFromLabel(aiRes.Label),
			}
		}
	}

	log.Printf("[AI仲裁] 完成\n"+
		"  AI返回结果数: %d\n"+
		"  已更新分类数: %d",
		len(aiResults), len(aiResults))
	return results, nil
}

type aiResultItem struct {
	Index      int     `json:"index"`
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
}

func parseAIResponse(response string) []aiResultItem {
	response = strings.TrimSpace(response)
	// 去除可能的 markdown 代码块标记
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var items []aiResultItem
	if err := json.Unmarshal([]byte(response), &items); err != nil {
		log.Printf("[AI仲裁] JSON 解析失败: %v, response=%s", err, truncate(response, 200))
		return nil
	}

	// 验证标签合法性
	validLabels := map[string]bool{
		TypeTitle: true, TypeCover: true, TypeOriginalityDeclaration: true,
		TypeAbstractTitle: true, TypeAbstract: true,
		TypeEnAbstractTitle: true, TypeEnAbstract: true,
		TypeKeywords: true, TypeEnKeywords: true,
		TypeHeading1: true, TypeHeading2: true, TypeHeading3: true,
		TypeBody: true,
		TypeReferencesTitle: true, TypeReferences: true,
		TypeTOCTitle: true, TypeTOC: true,
	}

	var valid []aiResultItem
	for _, item := range items {
		if validLabels[item.Label] {
			if item.Confidence <= 0 || item.Confidence > 1 {
				item.Confidence = 0.85
			}
			valid = append(valid, item)
		}
	}
	return valid
}

func detectLevelFromLabel(label string) int {
	switch label {
	case TypeHeading1:
		return 1
	case TypeHeading2:
		return 2
	case TypeHeading3:
		return 3
	default:
		return 0
	}
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}
