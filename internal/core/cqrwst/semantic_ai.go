package cqrwst

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

type SemanticAIClient interface {
	ChatCompletion(prompt string) (string, error)
}

type semanticAIDecision struct {
	Index       int     `json:"index"`
	Kind        string  `json:"kind"`
	CaptionName string  `json:"caption_name,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	Reason      string  `json:"reason,omitempty"`
}

type semanticAIResponse struct {
	Decisions []semanticAIDecision `json:"decisions"`
}

type semanticAICandidate struct {
	Index          int    `json:"index"`
	LocalKind      string `json:"local_kind"`
	Chapter        string `json:"chapter"`
	TextPreview    string `json:"text_preview,omitempty"`
	PreviousText   string `json:"previous_text,omitempty"`
	NextText       string `json:"next_text,omitempty"`
	Rows           int    `json:"rows,omitempty"`
	Cells          int    `json:"cells,omitempty"`
	HasShape       bool   `json:"has_shape,omitempty"`
	HasPrevCaption bool   `json:"has_prev_caption,omitempty"`
	HasNextCaption bool   `json:"has_next_caption,omitempty"`
}

const semanticAIConfidenceThreshold = 0.85

func FixDOCXWithTemplateProfileAndSemanticAI(ctx context.Context, path string, profile *templateprofile.Profile, client SemanticAIClient) (Result, error) {
	result, err := FixDOCXWithTemplateProfile(ctx, path, profile)
	if err != nil {
		return result, err
	}
	if client == nil {
		return result, nil
	}
	count, err := applySemanticAIRepairs(path, client)
	if err != nil || count == 0 {
		return result, nil
	}
	result.FixCount += count
	result.Issues = append(result.Issues, Issue{
		RuleID:   "cqrwst-semantic-ai-block-decision",
		Kind:     "repairable_semantic",
		Severity: "error",
		Message:  "DeepSeek semantic block decisions adjusted figure/table layout and captions",
		Target:   documentTarget,
	})
	result.Passed = len(result.Issues) == 0
	return result, nil
}

func applySemanticAIRepairs(path string, client SemanticAIClient) (int, error) {
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, nil
	}
	documentXML := string(content)
	blocks := buildSemanticBlocks(documentXML)
	candidates := semanticAICandidates(blocks)
	if len(candidates) == 0 {
		return 0, nil
	}
	decisions, err := requestSemanticAIDecisions(client, candidates)
	if err != nil || len(decisions) == 0 {
		return 0, err
	}
	next, count := applySemanticAIDecisionsToDocumentXML(documentXML, decisions)
	if count == 0 {
		return 0, nil
	}
	pkg.Set(documentTarget, []byte(next))
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

func semanticAICandidates(blocks []semanticBlock) []semanticAICandidate {
	candidates := []semanticAICandidate{}
	for _, block := range blocks {
		if !block.InBody {
			continue
		}
		if block.Kind != semanticBlockDataTable && block.Kind != semanticBlockLayoutTable && block.Kind != semanticBlockFigure {
			continue
		}
		candidates = append(candidates, semanticAICandidate{
			Index:          block.Index,
			LocalKind:      string(block.Kind),
			Chapter:        block.Chapter,
			TextPreview:    truncateSemanticText(block.Text, 80),
			PreviousText:   nearbySemanticText(blocks, block.Index, -1),
			NextText:       nearbySemanticText(blocks, block.Index, 1),
			Rows:           block.Rows,
			Cells:          block.Cells,
			HasShape:       block.HasFigureShape,
			HasPrevCaption: previousSemanticBlockIsCaption(blocks, block.Index, "table"),
			HasNextCaption: nextSemanticBlockIsCaption(blocks, block.Index, "figure"),
		})
	}
	return candidates
}

func requestSemanticAIDecisions(client SemanticAIClient, candidates []semanticAICandidate) ([]semanticAIDecision, error) {
	payload, err := json.MarshalIndent(map[string]interface{}{"blocks": candidates}, "", "  ")
	if err != nil {
		return nil, err
	}
	prompt := semanticAIPrompt(string(payload))
	response, err := client.ChatCompletion(prompt)
	clean := trimSemanticAIResponse(response)
	if err != nil {
		return nil, err
	}
	var parsed semanticAIResponse
	if err := json.Unmarshal([]byte(clean), &parsed); err != nil {
		return nil, fmt.Errorf("parse semantic AI response: %w", err)
	}
	return normalizeSemanticAIDecisions(parsed.Decisions), nil
}

func semanticAIPrompt(payload string) string {
	return `你是“本科论文 DOCX 排版修复系统”的语义安全分类器，不是写作助手。
只返回严格 JSON；不要 Markdown、不要解释、不要自然语言前后缀。

目标：
你只判断候选 block 的结构语义，帮助程序决定是否补/删“图表题注”。
你不能改论文正文内容，不能改标题编号，不能改摘要/关键词/参考文献文本，不能决定分页。

允许的 kind：
- "layout_table": 封面表格、信息采集表、排版用网格表、签名栏、目录/页眉页脚布局表。不能加“表X.X”题注。
- "data_table": 正文中的真实学术数据表、调查结果表、统计结果表、变量/样本信息表。通常需要“表X.X 表名”。
- "figure": 正文中的流程图、技术路线图、图片、模型图、示意图。通常需要“图X.X 图名”。

硬性规则：
1. 只输出需要程序采取动作的 block；不确定就不要输出该 block。
2. confidence 必须 >= 0.85；低于 0.85 的判断不要输出。
3. local_kind 只是程序初判，可纠正，但不能凭空想象。
4. 封面/基本信息表含“题目、学院、专业、班级、学号、姓名、指导教师、日期”等字段时，一律判为 layout_table。
5. 只有呈现研究数据、统计结果、样本资料、变量定义、量表条目、实验/问卷结果的表，才判为 data_table。
6. 复杂形状/箭头/流程节点/路线图优先判为 figure；caption_name 从 nearby heading 或已有上下文抽短名，例如“技术路线图”。
7. caption_name 只能是短名词短语，不要带“表1.1/图1.1”，不要编造研究结论，不要超过 20 个汉字。
8. 如果 has_prev_caption 或 has_next_caption 已经为 true，通常不要输出，除非它明显是程序生成的泛名“表格/图示”且你能给出更准确 caption_name。
9. 如果 blocks 里没有安全可改项，返回 {"decisions":[]}。

输出 schema：
{"decisions":[{"index":12,"kind":"data_table","caption_name":"样本基本情况表","confidence":0.92,"reason":"正文研究数据表"}]}

Blocks JSON:
` + payload
}

func trimSemanticAIResponse(response string) string {
	s := strings.TrimSpace(response)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func normalizeSemanticAIDecisions(decisions []semanticAIDecision) []semanticAIDecision {
	result := make([]semanticAIDecision, 0, len(decisions))
	for _, decision := range decisions {
		decision.Kind = strings.TrimSpace(strings.ToLower(decision.Kind))
		if decision.Confidence < semanticAIConfidenceThreshold {
			continue
		}
		switch decision.Kind {
		case "layout_table", "data_table", "figure":
			decision.CaptionName = strings.TrimSpace(decision.CaptionName)
			result = append(result, decision)
		}
	}
	return result
}

func applySemanticAIDecisionsToDocumentXML(documentXML string, decisions []semanticAIDecision) (string, int) {
	matches := documentBodyChildPattern.FindAllStringIndex(documentXML, -1)
	if len(matches) == 0 {
		return documentXML, 0
	}
	decisionByIndex := map[int]semanticAIDecision{}
	for _, decision := range decisions {
		decisionByIndex[decision.Index] = decision
	}
	blocks := buildSemanticBlocks(documentXML)
	removeParagraph := map[int]bool{}
	replaceParagraph := map[int]string{}
	insertBefore := map[int]string{}
	insertAfter := map[int]string{}
	tableCounters := map[string]int{}
	figureCounters := map[string]int{}

	for _, block := range blocks {
		if block.Kind == semanticBlockTableCaption {
			if chapter := block.Chapter; chapter != "" {
				tableCounters[chapter]++
			}
		}
		if block.Kind == semanticBlockFigureCaption {
			if chapter := block.Chapter; chapter != "" {
				figureCounters[chapter]++
			}
		}
	}

	for _, block := range blocks {
		decision, ok := decisionByIndex[block.Index]
		if !ok {
			continue
		}
		switch decision.Kind {
		case "layout_table":
			if prev := previousNonBlankSemanticParagraph(blocks, block.Index); prev >= 0 && isGeneratedGenericCaption(semanticBlockByIndex(blocks, prev).Text) {
				removeParagraph[prev] = true
			}
		case "data_table":
			if previousSemanticBlockIsCaption(blocks, block.Index, "table") {
				if prev := previousNonBlankSemanticParagraph(blocks, block.Index); prev >= 0 && decision.CaptionName != "" {
					prevBlock := semanticBlockByIndex(blocks, prev)
					if isGeneratedGenericCaption(prevBlock.Text) {
						replaceParagraph[prev] = replaceParagraphVisibleText(prevBlock.XML, numberedCaption("\u8868", block.Chapter, captionNumberFromText(prevBlock.Text), decision.CaptionName))
					}
				}
				continue
			}
			tableCounters[block.Chapter]++
			insertBefore[block.Index] = buildParagraphXML(numberedCaption("\u8868", block.Chapter, tableCounters[block.Chapter], semanticCaptionName(decision.CaptionName, "\u8868\u683c")), captionStyle())
		case "figure":
			if next := nextNonBlankSemanticParagraph(blocks, block.Index); next >= 0 {
				nextBlock := semanticBlockByIndex(blocks, next)
				if isFigureCaption(nextBlock.Text) && decision.CaptionName != "" && isGeneratedGenericCaption(nextBlock.Text) {
					replaceParagraph[next] = replaceParagraphVisibleText(nextBlock.XML, numberedCaption("\u56fe", block.Chapter, captionNumberFromText(nextBlock.Text), decision.CaptionName))
					continue
				}
				if isFigureCaption(nextBlock.Text) {
					continue
				}
			}
			figureCounters[block.Chapter]++
			insertAfter[block.Index] = buildParagraphXML(numberedCaption("\u56fe", block.Chapter, figureCounters[block.Chapter], semanticCaptionName(decision.CaptionName, "\u56fe\u793a")), captionStyle())
		}
	}

	var builder strings.Builder
	last := 0
	count := 0
	for index, match := range matches {
		builder.WriteString(documentXML[last:match[0]])
		if value := insertBefore[index]; value != "" {
			builder.WriteString(value)
			count++
		}
		if !removeParagraph[index] {
			if value := replaceParagraph[index]; value != "" {
				builder.WriteString(value)
				count++
			} else {
				builder.WriteString(documentXML[match[0]:match[1]])
			}
		} else {
			count++
		}
		if value := insertAfter[index]; value != "" {
			builder.WriteString(value)
			count++
		}
		last = match[1]
	}
	builder.WriteString(documentXML[last:])
	return builder.String(), count
}

func semanticCaptionName(name string, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fallback
	}
	return name
}

func captionNumberFromText(text string) int {
	match := regexp.MustCompile(`^[\p{Han}]\s*\d+(?:[.-](\d+))?`).FindStringSubmatch(strings.TrimSpace(text))
	if len(match) == 2 && strings.TrimSpace(match[1]) != "" {
		var number int
		if _, err := fmt.Sscanf(match[1], "%d", &number); err == nil && number > 0 {
			return number
		}
	}
	return 1
}

func previousSemanticBlockIsCaption(blocks []semanticBlock, index int, kind string) bool {
	prev := previousNonBlankSemanticParagraph(blocks, index)
	if prev < 0 {
		return false
	}
	text := semanticBlockByIndex(blocks, prev).Text
	if kind == "table" {
		return isTableCaption(text)
	}
	return isFigureCaption(text)
}

func nextSemanticBlockIsCaption(blocks []semanticBlock, index int, kind string) bool {
	next := nextNonBlankSemanticParagraph(blocks, index)
	if next < 0 {
		return false
	}
	text := semanticBlockByIndex(blocks, next).Text
	if kind == "table" {
		return isTableCaption(text)
	}
	return isFigureCaption(text)
}

func previousNonBlankSemanticParagraph(blocks []semanticBlock, index int) int {
	for i := index - 1; i >= 0; i-- {
		block := semanticBlockByIndex(blocks, i)
		if !block.IsParagraph || strings.TrimSpace(block.Text) == "" {
			continue
		}
		return i
	}
	return -1
}

func nextNonBlankSemanticParagraph(blocks []semanticBlock, index int) int {
	for i := index + 1; i < len(blocks); i++ {
		block := semanticBlockByIndex(blocks, i)
		if !block.IsParagraph || strings.TrimSpace(block.Text) == "" {
			continue
		}
		return i
	}
	return -1
}

func nearbySemanticText(blocks []semanticBlock, index int, direction int) string {
	next := -1
	if direction < 0 {
		next = previousNonBlankSemanticParagraph(blocks, index)
	} else {
		next = nextNonBlankSemanticParagraph(blocks, index)
	}
	if next < 0 {
		return ""
	}
	return truncateSemanticText(semanticBlockByIndex(blocks, next).Text, 80)
}

func truncateSemanticText(text string, max int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}
