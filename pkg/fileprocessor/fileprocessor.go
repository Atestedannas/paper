package fileprocessor

import "context"

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
type BasicFileProcessor struct{}

// NewBasicFileProcessor 创建基本文件处理器
func NewBasicFileProcessor() FileProcessor {
	return &BasicFileProcessor{}
}

// ExtractDocumentInfo 提取文档信息（简化实现）
func (p *BasicFileProcessor) ExtractDocumentInfo(filePath string) (FileInfo, error) {
	// 这里是简化实现，实际应该根据文件类型调用相应的解析器
	info := FileInfo{
		Format:       "pdf", // 简化处理
		Pages:        10,
		WordCount:    5000,
		CharCount:    25000,
		Title:        "示例论文",
		Author:       "作者",
		CreatedDate:  "2024-01-01",
		ModifiedDate: "2024-01-01",
	}

	return info, nil
}

// ExtractDocInfo 提取文档信息
func (p *BasicFileProcessor) ExtractDocInfo(ctx context.Context, docPath string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"title":  "",
		"author": "",
		"pages":  0,
	}, nil
}

// ExtractHeadings 提取标题
func (p *BasicFileProcessor) ExtractHeadings(ctx context.Context, docPath string) ([]map[string]interface{}, error) {
	return []map[string]interface{}{}, nil
}

// ExtractParagraphs 提取段落
func (p *BasicFileProcessor) ExtractParagraphs(ctx context.Context, docPath string) ([]map[string]interface{}, error) {
	return []map[string]interface{}{}, nil
}
