package fileprocessor

import (
	"context"
)

// FileProcessor 文件处理器接口
type FileProcessor interface {
	// ExtractDocumentInfo 提取文档信息
	ExtractDocumentInfo(filePath string) (FileInfo, error)

	// ExtractDocInfo 提取文档信息（带上下文）
	ExtractDocInfo(ctx context.Context, docPath string) (map[string]interface{}, error)

	// ExtractHeadings 提取标题
	ExtractHeadings(ctx context.Context, docPath string) ([]map[string]interface{}, error)

	// ExtractParagraphs 提取段落
	ExtractParagraphs(ctx context.Context, docPath string) ([]map[string]interface{}, error)

	// ApplyCorrections 应用修正到文档
	ApplyCorrections(ctx context.Context, docPath string, corrections []map[string]interface{}) (string, error)
}

// FileInfo 文档基本信息
type FileInfo struct {
	Format       string `json:"format"`        // 文件格式 (docx, pdf)
	Pages        int    `json:"pages"`         // 页数
	WordCount    int    `json:"word_count"`    // 字数
	CharCount    int    `json:"char_count"`    // 字符数
	Title        string `json:"title"`         // 标题
	Author       string `json:"author"`        // 作者
	CreatedDate  string `json:"created_date"`  // 创建日期
	ModifiedDate string `json:"modified_date"` // 修改日期
}

// BasicFileProcessor 基本文件处理器实现
type BasicFileProcessor struct {
	processor FileProcessor // 使用内部处理器实现
}

// NewBasicFileProcessor 创建基本文件处理器
func NewBasicFileProcessor() FileProcessor {
	// 优先使用四阶段处理器
	return &BasicFileProcessor{
		processor: NewFourStageProcessor(),
	}
}

// ExtractDocumentInfo 提取文档信息（简化实现）
func (p *BasicFileProcessor) ExtractDocumentInfo(filePath string) (FileInfo, error) {
	// 委托给内部处理器实现
	return p.processor.ExtractDocumentInfo(filePath)
}

// ExtractDocInfo 提取文档信息
func (p *BasicFileProcessor) ExtractDocInfo(ctx context.Context, docPath string) (map[string]interface{}, error) {
	// 委托给内部处理器实现
	return p.processor.ExtractDocInfo(ctx, docPath)
}

// ExtractHeadings 提取标题
func (p *BasicFileProcessor) ExtractHeadings(ctx context.Context, docPath string) ([]map[string]interface{}, error) {
	// 委托给内部处理器实现
	return p.processor.ExtractHeadings(ctx, docPath)
}

// ExtractParagraphs 提取段落
func (p *BasicFileProcessor) ExtractParagraphs(ctx context.Context, docPath string) ([]map[string]interface{}, error) {
	// 委托给内部处理器实现
	return p.processor.ExtractParagraphs(ctx, docPath)
}

// ApplyCorrections 应用修正到文档
func (p *BasicFileProcessor) ApplyCorrections(ctx context.Context, docPath string, corrections []map[string]interface{}) (string, error) {
	// 委托给内部处理器实现
	return p.processor.ApplyCorrections(ctx, docPath, corrections)
}
