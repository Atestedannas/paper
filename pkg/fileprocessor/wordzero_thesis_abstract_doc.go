package fileprocessor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nineya/wordZero/pkg/document"
)

// 使用 wordzero 和规则和设计对应的规则来实现新的符合格式的 .docx
//
// 规则摘要：
// - 「摘要：」黑体、小三号(≈15pt)、加粗、首行缩进 2 字符(≈28pt)、段后约 2 行(≈24pt)
// - 正文 宋体、小四(12pt)、1.5 倍行距、首行缩进 2 字符、段后约 2 行
// - 页边距：常见本科论文 A4 上下右 2.54cm、左 3.17cm（单位：毫米，与 WordZero PageSettings 一致）

// DefaultThesisAbstractBody 未传入正文时的占位说明（可替换为真实摘要）。
const DefaultThesisAbstractBody = "这里是摘要内容，应说明论文（设计）的目的、主要内容、研究方法、得出的成果和结论等。必须重点突出，文字精练。中文摘要以300—500字为宜。"

// BuildWordZeroThesisAbstractDocx 新建一篇仅含中文摘要版式示例的 docx（WordZero）。
func BuildWordZeroThesisAbstractDocx(outputPath string, abstractBody string) error {
	outputPath = filepath.Clean(outputPath)
	if outputPath == "" || !strings.EqualFold(filepath.Ext(outputPath), ".docx") {
		return fmt.Errorf("wordzero abstract doc: outputPath 须为非空 .docx 路径")
	}
	body := strings.TrimSpace(abstractBody)
	if body == "" {
		body = DefaultThesisAbstractBody
	}

	// 1. 创建新文档
	doc := document.New()

	// 2. 设置页面边距（毫米）：上/下/右 25.4mm≈2.54cm，左 31.7mm≈3.17cm
	if err := doc.SetPageMargins(25.4, 25.4, 25.4, 31.7); err != nil {
		return fmt.Errorf("wordzero SetPageMargins: %w", err)
	}
	if err := doc.SetPageSize(document.PageSizeA4); err != nil {
		return fmt.Errorf("wordzero SetPageSize: %w", err)
	}

	// 3. 「摘要：」标题
	titleFormat := &document.TextFormat{
		FontFamily: "黑体",
		FontSize:   15,   // 小三号 ≈ 15pt
		Bold:       true, // 加粗
	}
	titlePara := doc.AddFormattedParagraph("摘要：", titleFormat)
	titlePara.SetSpacing(&document.SpacingConfig{
		FirstLineIndent: 28, // 首行缩进 2 字符 ≈ 28pt（按小四 14pt 估算）
		AfterPara:       24, // 段后约 2 行
	})

	// 4. 摘要正文
	contentFormat := &document.TextFormat{
		FontFamily: "宋体",
		FontSize:   12, // 小四 = 12pt
	}
	contentPara := doc.AddFormattedParagraph(body, contentFormat)
	contentPara.SetSpacing(&document.SpacingConfig{
		FirstLineIndent: 28,  // 首行缩进 2 字符
		LineSpacing:     1.5, // 1.5 倍行距
		AfterPara:       24,  // 段后约 2 行
	})

	outDir := filepath.Dir(outputPath)
	if outDir != "" && outDir != "." {
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return fmt.Errorf("wordzero abstract doc mkdir: %w", err)
		}
	}
	if err := doc.Save(outputPath); err != nil {
		return fmt.Errorf("wordzero Save: %w", err)
	}
	return nil
}
