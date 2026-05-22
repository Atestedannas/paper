package templatefiller

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DeepSeekClient is the interface for making DeepSeek API calls.
type DeepSeekClient interface {
	ChatCompletion(prompt string) (string, error)
}

// DeepSeekRefiner uses DeepSeek AI to refine paragraph classification by
// analyzing the full document structure and identifying precise section boundaries.
type DeepSeekRefiner struct {
	client DeepSeekClient
}

func NewDeepSeekRefiner(client DeepSeekClient) *DeepSeekRefiner {
	return &DeepSeekRefiner{client: client}
}

// refinerWaitTimeout is how long RefineClassification waits for ChatCompletion.
// Long papers over browser DeepSeek SSE often exceed 30s; default 120s.
// Set DEEPSEEK_REFINER_TIMEOUT_SEC (5–600); invalid or empty → 120.
func refinerWaitTimeout() time.Duration {
	const defaultSec = 120
	s := strings.TrimSpace(os.Getenv("DEEPSEEK_REFINER_TIMEOUT_SEC"))
	if s == "" {
		return defaultSec * time.Second
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 5 {
		return defaultSec * time.Second
	}
	if n > 600 {
		n = 600
	}
	return time.Duration(n) * time.Second
}

// SectionBoundary represents the start and end paragraph indices for a document section.
type SectionBoundary struct {
	Section    string `json:"section"`
	StartIndex int    `json:"start_index"`
	EndIndex   int    `json:"end_index"`
}

// RefineClassification takes initial classification results and uses DeepSeek
// to produce more accurate section assignments by analyzing the full document context.
// Waits up to refinerWaitTimeout() (default 120s, env DEEPSEEK_REFINER_TIMEOUT_SEC).
func (r *DeepSeekRefiner) RefineClassification(cls ClassificationResult) (ClassificationResult, error) {
	if r.client == nil {
		return cls, nil
	}

	wait := refinerWaitTimeout()

	compactDoc := buildCompactDocument(cls)
	if compactDoc == "" {
		return cls, nil
	}

	prompt := buildRefinementPrompt(compactDoc)

	log.Printf("[DeepSeekRefiner] sending %d paragraphs for refinement (timeout %v)", len(cls.Paragraphs), wait)

	type result struct {
		response string
		err      error
	}

	ch := make(chan result, 1)
	var once sync.Once

	go func() {
		resp, err := r.client.ChatCompletion(prompt)
		once.Do(func() {
			ch <- result{resp, err}
		})
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			log.Printf("[DeepSeekRefiner] DeepSeek call failed: %v, using original classification", res.err)
			return cls, nil
		}

		boundaries := parseRefinementResponse(res.response)
		if len(boundaries) == 0 {
			log.Printf("[DeepSeekRefiner] no valid boundaries parsed, using original classification")
			return cls, nil
		}

		refined := applyBoundaries(cls, boundaries)
		log.Printf("[DeepSeekRefiner] refined %d paragraphs using %d section boundaries", len(refined.Paragraphs), len(boundaries))
		return refined, nil

	case <-time.After(wait):
		log.Printf("[DeepSeekRefiner] timeout after %v, using original classification", wait)
		return cls, nil
	}
}

func buildCompactDocument(cls ClassificationResult) string {
	if len(cls.Paragraphs) == 0 {
		return ""
	}

	// Only send key paragraphs to reduce prompt size and DeepSeek response time.
	// Include: first/last 5 of each section, all headings, all section boundaries.
	important := selectImportantParagraphs(cls)

	var b strings.Builder
	for _, p := range important {
		text := strings.TrimSpace(p.Text)
		runes := []rune(text)
		if len(runes) > 50 {
			text = string(runes[:50]) + "..."
		}
		b.WriteString(fmt.Sprintf("[%d] %s | %s\n", p.Index, p.Type, text))
	}
	return b.String()
}

func selectImportantParagraphs(cls ClassificationResult) []ClassificationParagraph {
	if len(cls.Paragraphs) <= 120 {
		return cls.Paragraphs
	}

	included := make(map[int]bool)

	// Always include headings and title-type paragraphs
	for _, p := range cls.Paragraphs {
		switch p.Type {
		case "heading_1", "heading_2", "heading_3",
			"abstract_title", "en_abstract_title",
			"references_title", "acknowledgements_title", "appendix_title",
			"table_of_contents_title", "cover", "cover_title",
			"keywords", "en_keywords":
			included[p.Index] = true
		}
	}

	// Include section boundaries (first and last 3 paragraphs of each section type)
	sectionFirst := make(map[string][]int)
	sectionLast := make(map[string][]int)
	for _, p := range cls.Paragraphs {
		if len(sectionFirst[p.Type]) < 3 {
			sectionFirst[p.Type] = append(sectionFirst[p.Type], p.Index)
		}
		sectionLast[p.Type] = append(sectionLast[p.Type], p.Index)
		if len(sectionLast[p.Type]) > 3 {
			sectionLast[p.Type] = sectionLast[p.Type][len(sectionLast[p.Type])-3:]
		}
	}
	for _, indices := range sectionFirst {
		for _, idx := range indices {
			included[idx] = true
		}
	}
	for _, indices := range sectionLast {
		for _, idx := range indices {
			included[idx] = true
		}
	}

	var result []ClassificationParagraph
	for _, p := range cls.Paragraphs {
		if included[p.Index] {
			result = append(result, p)
		}
	}
	return result
}

func buildRefinementPrompt(compactDoc string) string {
	return fmt.Sprintf(`你是一个学术论文结构分析专家。以下是一篇论文的段落列表，每行格式为：[段落序号] 当前分类 | 文本摘要

请分析整篇文档结构，识别每个章节的精确边界。返回JSON数组，格式：
[
  {"section": "cover", "start_index": 0, "end_index": 5},
  {"section": "abstract_title", "start_index": 6, "end_index": 6},
  {"section": "abstract", "start_index": 7, "end_index": 10},
  ...
]

可用的section值：
- cover: 封面（含标题、学院、专业、学号、姓名、指导教师、日期等）
- abstract_title: 中文摘要标题（仅"摘要"二字）
- abstract: 中文摘要正文
- keywords: 中文关键词（含"关键词："标签）
- en_abstract_title: 英文摘要标题（仅"Abstract"）
- en_abstract: 英文摘要正文
- en_keywords: 英文关键词（含"Key words:"标签）
- table_of_contents: 目录
- heading_1: 一级标题（如"1 绪论"、"2 xxx"）
- heading_2: 二级标题（如"2.1 xxx"）
- heading_3: 三级标题（如"2.1.1 xxx"）
- body: 正文段落
- references_title: 参考文献标题
- references: 参考文献条目
- acknowledgements_title: 致谢标题
- acknowledgements: 致谢正文
- appendix_title: 附录标题
- appendix: 附录内容

重要规则：
1. 每个段落必须且只能属于一个section
2. heading_1/heading_2/heading_3 应单独标注，不合并入body
3. start_index和end_index使用段落的[序号]
4. 只返回JSON，不要其他文字

段落列表：
%s`, compactDoc)
}

func parseRefinementResponse(response string) []SectionBoundary {
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var boundaries []SectionBoundary
	if err := json.Unmarshal([]byte(response), &boundaries); err != nil {
		log.Printf("[DeepSeekRefiner] JSON parse failed: %v, response=%s",
			err, truncateStr(response, 300))
		return nil
	}

	validSections := map[string]bool{
		"cover": true, "abstract_title": true, "abstract": true,
		"keywords": true, "en_abstract_title": true, "en_abstract": true,
		"en_keywords": true, "table_of_contents": true,
		"heading_1": true, "heading_2": true, "heading_3": true,
		"body": true, "references_title": true, "references": true,
		"acknowledgements_title": true, "acknowledgements": true,
		"appendix_title": true, "appendix": true,
	}

	var valid []SectionBoundary
	for _, b := range boundaries {
		if !validSections[b.Section] {
			continue
		}
		if b.StartIndex < 0 || b.EndIndex < b.StartIndex {
			continue
		}
		valid = append(valid, b)
	}
	return valid
}

func applyBoundaries(cls ClassificationResult, boundaries []SectionBoundary) ClassificationResult {
	indexToParaIdx := make(map[int]int) // paragraph original index -> position in cls.Paragraphs
	for i, p := range cls.Paragraphs {
		indexToParaIdx[p.Index] = i
	}

	// Build a map from original paragraph index to new section label
	refinedLabels := make(map[int]string)
	for _, b := range boundaries {
		for idx := b.StartIndex; idx <= b.EndIndex; idx++ {
			refinedLabels[idx] = b.Section
		}
	}

	result := ClassificationResult{
		Paragraphs: make([]ClassificationParagraph, len(cls.Paragraphs)),
	}
	copy(result.Paragraphs, cls.Paragraphs)

	updated := 0
	for i, p := range result.Paragraphs {
		if newLabel, ok := refinedLabels[p.Index]; ok {
			if newLabel != p.Type {
				result.Paragraphs[i].Type = newLabel
				updated++
			}
		}
	}

	log.Printf("[DeepSeekRefiner] updated %d/%d paragraph labels", updated, len(result.Paragraphs))
	return result
}

func truncateStr(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n]) + "..."
	}
	return s
}
