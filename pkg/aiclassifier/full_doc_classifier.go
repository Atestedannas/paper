package aiclassifier

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// FullDocumentClassifier 全文档语义分类器
// 将整篇文档所有段落打包发给 DeepSeek，利用其对论文结构的整体语义理解
// 相比 AIArbitrator（只处理低置信度段落），全文分类可以：
//  1. 识别上下文关系（如TOC区间内的所有条目）
//  2. 纠正规则引擎的系统性偏差（如TOC条目被误判为heading_2）
//  3. 准确区分相似段落（body vs abstract 在ZoneTOC→ZoneBody过渡处）
type FullDocumentClassifier struct {
	client *DeepSeekWebClient
}

// NewFullDocumentClassifier 创建全文分类器
func NewFullDocumentClassifier(client *DeepSeekWebClient) *FullDocumentClassifier {
	return &FullDocumentClassifier{client: client}
}

// ClassifyAll 对整篇文档所有段落做全量语义分类
// features:    所有段落的特征向量（来自 ExtractFeatures）
// ruleResults: 规则引擎+状态机的初步分类（作为参考提供给 AI）
// 返回: 全量修正后的分类结果
func (c *FullDocumentClassifier) ClassifyAll(
	features []ParagraphFeature,
	ruleResults []ClassifyResult,
) ([]ClassifyResult, error) {
	if c.client == nil {
		return nil, fmt.Errorf("DeepSeek client not available")
	}

	total := len(features)
	log.Printf("[全文分类] 开始全量语义分类，共 %d 个段落", total)

	// 初始化为规则结果（失败时保留规则结果）
	results := make([]ClassifyResult, total)
	copy(results, ruleResults)

	// 分批处理（每批 ≤100 段，避免超过 token 限制）
	const batchSize = 100
	successBatches := 0

	for start := 0; start < total; start += batchSize {
		end := start + batchSize
		if end > total {
			end = total
		}

		batchResults, err := c.classifyBatch(
			start, end,
			features[start:end],
			ruleResults[start:end],
			total,
		)
		if err != nil {
			log.Printf("[全文分类] 批次[%d:%d]失败，保留规则结果: %v", start, end, err)
			continue
		}

		for i, r := range batchResults {
			if r.Label != "" {
				results[start+i] = r
			}
		}
		successBatches++
		log.Printf("[全文分类] 批次[%d:%d]完成（%d段）", start, end, end-start)
	}

	log.Printf("[全文分类] 完成，成功批次: %d/%d",
		successBatches, (total+batchSize-1)/batchSize)
	logClassificationStats("全文语义分类", results)

	return results, nil
}

// paraDesc 发送给 DeepSeek 的段落摘要（精简以节省 token）
type paraDesc struct {
	I int    `json:"i"` // 全局段落序号
	T string `json:"t"` // 文本前 30 字
	S int    `json:"s"` // 字号（半磅，如 24=12pt）
	B bool   `json:"b"` // 是否加粗
	A string `json:"a"` // 对齐方式
	P string `json:"p"` // 前一段分类
	R string `json:"r"` // 规则引擎分类结果（供参考）
}

// labelItem DeepSeek 返回的每段分类结果
type labelItem struct {
	I int    `json:"i"` // 段落序号
	L string `json:"l"` // 分类标签
}

// classifyBatch 对一批段落调用 DeepSeek
func (c *FullDocumentClassifier) classifyBatch(
	startIdx, endIdx int,
	features []ParagraphFeature,
	ruleResults []ClassifyResult,
	totalParas int,
) ([]ClassifyResult, error) {
	// 构建精简段落描述
	descs := make([]paraDesc, len(features))
	for i, f := range features {
		text := f.Text
		runes := []rune(text)
		if len(runes) > 30 {
			text = string(runes[:30])
		}
		descs[i] = paraDesc{
			I: startIdx + i,
			T: text,
			S: int(f.FontSizePt * 2),
			B: f.IsBold,
			A: f.Alignment,
			P: f.PrevType,
			R: ruleResults[i].Label,
		}
	}

	descJSON, _ := json.Marshal(descs)

	prompt := fmt.Sprintf(`你是中文本科毕业论文格式分析专家。
下面是论文中第%d~%d段（共%d段）的摘要，每条字段含义：i=段落序号,t=文本前30字,s=字号(半磅,24=12pt),b=加粗,a=对齐,p=前一段分类,r=规则引擎猜测。

论文结构固定顺序：封面 → 中文摘要 → 英文摘要 → 目录 → 正文章节 → 参考文献 → 致谢 → 附录

请将每个段落分类为以下标签之一：
- cover: 封面内容（校名/题目/学院专业/姓名/学号/日期等）
- abstract_title: 只有"摘要"两字的行（或"摘  要"带空格）
- abstract: 中文摘要的正文内容
- en_abstract_title: 只有"Abstract"的行
- en_abstract: 英文摘要正文
- keywords: "关键词："开头的行
- en_keywords: "Keywords:"开头的行
- table_of_contents_title: 目录标题（"目录"或"目  录"）
- table_of_contents: 目录条目（含"．．"或"......"填充符和页码的行）
- heading_1: 一级标题（如"1 绪论"、"第一章 绪论"）
- heading_2: 二级标题（如"1.1 研究背景"）
- heading_3: 三级标题（如"1.1.1 研究现状"）
- body: 正文段落（最常见类型）
- references_title: 只有"参考文献"的行
- references: 参考文献条目（如"[1] 作者..."）
- acknowledgements: 致谢内容
- body: 其他（默认为正文）

⚠️ 关键规则：
1. 含"．．"（全角点×2+）或"......"（多个点）且末尾有数字的行→一定是目录条目(table_of_contents)，绝不是heading
2. 以"[数字]"开头的行→references条目
3. 目录区间：从"目录"标题到第一个正文一级标题之间的所有条目均为table_of_contents

段落数据（JSON）：
%s

仅返回JSON数组，格式：[{"i":0,"l":"body"},...]，绝对不要包含任何解释文字。`,
		startIdx, endIdx-1, totalParas, string(descJSON))

	response, err := c.client.ChatCompletion(prompt)
	if err != nil {
		return nil, fmt.Errorf("DeepSeek调用失败: %w", err)
	}

	// 提取JSON（去掉可能的markdown代码块包装）
	jsonStr := extractJSONArray(response)

	var labelResults []labelItem
	if err := json.Unmarshal([]byte(jsonStr), &labelResults); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %w\n原始响应(前300字): %s",
			err, truncateStr(response, 300))
	}

	// 将AI结果填充回本批次的结果切片
	results := make([]ClassifyResult, len(features))
	for j := range results {
		results[j] = ruleResults[j] // 默认保留规则结果
	}
	applied := 0
	for _, lr := range labelResults {
		localIdx := lr.I - startIdx
		if localIdx >= 0 && localIdx < len(results) && isValidParagraphLabel(lr.L) {
			results[localIdx] = ClassifyResult{
				Label:      lr.L,
				Confidence: 0.95,
				Source:     "deepseek_full",
			}
			applied++
		}
	}
	log.Printf("[全文分类] 批次[%d:%d]: AI返回%d条，成功应用%d条",
		startIdx, endIdx, len(labelResults), applied)

	return results, nil
}

// extractJSONArray 从可能含 markdown 的字符串中提取 JSON 数组
func extractJSONArray(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

// isValidParagraphLabel 验证 AI 返回的标签是否合法
func isValidParagraphLabel(label string) bool {
	switch label {
	case TypeCover, TypeAbstract, TypeAbstractTitle,
		TypeEnAbstract, TypeEnAbstractTitle,
		TypeKeywords, TypeEnKeywords,
		TypeTOCTitle, TypeTOC,
		TypeHeading1, TypeHeading2, TypeHeading3,
		TypeBody, TypeReferencesTitle, TypeReferences,
		"acknowledgements", TypeOriginalityDeclaration, TypeTitle:
		return true
	}
	return false
}

// truncateStr 截断字符串，防止日志过长
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}
