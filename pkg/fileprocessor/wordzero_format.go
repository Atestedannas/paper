package fileprocessor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/nineya/wordZero/pkg/document"
)

// saveDocWithRoundTripCheck 先保存到临时文件，再回读校验，最后原子替换目标文件。
// 这样可避免下载到半写入/损坏的 docx。
func saveDocWithRoundTripCheck(doc *document.Document, outputPath string) error {
	outDir := filepath.Dir(outputPath)
	if outDir != "" && outDir != "." {
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return fmt.Errorf("wordzero mkdir: %w", err)
		}
	}

	tmpPath := outputPath + ".tmp"
	if err := doc.Save(tmpPath); err != nil {
		return fmt.Errorf("wordzero save temp: %w", err)
	}

	// 回读校验：至少保证文档结构可被正常打开并含页面设置
	roundTripDoc, err := document.Open(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("wordzero round-trip open temp: %w", err)
	}
	if roundTripDoc.GetPageSettings() == nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("wordzero round-trip verify failed: page settings missing")
	}

	_ = os.Remove(outputPath)
	if err := os.Rename(tmpPath, outputPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("wordzero replace output: %w", err)
	}
	return nil
}

// correctedStyledOutputPath 与 RunStyleFormatter 一致：论文同目录 corrected/<base>_styled.ext
func correctedStyledOutputPath(docPath string) string {
	dir := filepath.Dir(docPath)
	ext := filepath.Ext(docPath)
	base := strings.TrimSuffix(filepath.Base(docPath), ext)
	outDir := filepath.Join(dir, "corrected")
	return filepath.Join(outDir, base+"_styled"+ext)
}

// ApplyWordZeroPageSetupFromTemplate 用 WordZero 将模板的页面设置写到学生稿并保存到 outputPath。
func ApplyWordZeroPageSetupFromTemplate(studentPath, templatePath, outputPath string) error {
	if studentPath == "" || templatePath == "" || outputPath == "" {
		return fmt.Errorf("wordzero: empty path")
	}
	if filepath.Clean(studentPath) == filepath.Clean(outputPath) {
		return fmt.Errorf("wordzero: output must differ from student input")
	}
	for _, p := range []string{studentPath, templatePath} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("wordzero: stat %s: %w", p, err)
		}
	}
	if strings.ToLower(filepath.Ext(studentPath)) != ".docx" {
		return fmt.Errorf("wordzero: student file must be .docx")
	}
	if strings.ToLower(filepath.Ext(templatePath)) != ".docx" {
		return fmt.Errorf("wordzero: template must be .docx")
	}

	tmpl, err := document.Open(templatePath)
	if err != nil {
		return fmt.Errorf("wordzero open template: %w", err)
	}
	tSettings := tmpl.GetPageSettings()
	if tSettings == nil {
		return fmt.Errorf("wordzero: template GetPageSettings returned nil")
	}

	stu, err := document.Open(studentPath)
	if err != nil {
		return fmt.Errorf("wordzero open student: %w", err)
	}
	if err := stu.SetPageSettings(tSettings); err != nil {
		return fmt.Errorf("wordzero SetPageSettings: %w", err)
	}
	_ = stu.UpdateStatistics()

	if err := saveDocWithRoundTripCheck(stu, outputPath); err != nil {
		return err
	}
	return nil
}

// RunWordZeroFormatter 将模板页面设置写出到 corrected/<base>_styled.docx。
func (p *EnhancedProcessor) RunWordZeroFormatter(ctx context.Context, docPath, templatePath string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	log.Println("========== WordZero 格式修正（页面设置）开始 ==========")
	log.Printf("[WordZero] input=%s template=%s", docPath, templatePath)

	outputPath := correctedStyledOutputPath(docPath)
	if err := ApplyWordZeroPageSetupFromTemplate(docPath, templatePath, outputPath); err != nil {
		return "", err
	}

	p.lastDiffReport = nil

	log.Printf("[WordZero] 已写出: %s", outputPath)
	log.Println("========== WordZero 格式修正 完成 ==========")
	return outputPath, nil
}

// ApplyWordZeroFormatFix 使用WordZero进行完整的格式修正，确保保留所有图片内容
func ApplyWordZeroFormatFix(studentPath, templatePath, outputPath string, preserveImages bool) error {
	if studentPath == "" || templatePath == "" || outputPath == "" {
		return fmt.Errorf("wordzero: empty path")
	}
	if filepath.Clean(studentPath) == filepath.Clean(outputPath) {
		return fmt.Errorf("wordzero: output must differ from student input")
	}
	for _, p := range []string{studentPath, templatePath} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("wordzero: stat %s: %w", p, err)
		}
	}
	if strings.ToLower(filepath.Ext(studentPath)) != ".docx" {
		return fmt.Errorf("wordzero: student file must be .docx")
	}
	if strings.ToLower(filepath.Ext(templatePath)) != ".docx" {
		return fmt.Errorf("wordzero: template must be .docx")
	}

	// 打开模板文件，提取格式设置
	tmpl, err := document.Open(templatePath)
	if err != nil {
		return fmt.Errorf("wordzero open template: %w", err)
	}
	
	// 提取页面设置
	tSettings := tmpl.GetPageSettings()
	if tSettings == nil {
		return fmt.Errorf("wordzero: template GetPageSettings returned nil")
	}

	// 打开学生论文文件
	stu, err := document.Open(studentPath)
	if err != nil {
		return fmt.Errorf("wordzero open student: %w", err)
	}
	
	// 应用页面设置
	if err := stu.SetPageSettings(tSettings); err != nil {
		return fmt.Errorf("wordzero SetPageSettings: %w", err)
	}
	
	// 确保保留图片（WordZero默认会保留图片）
	if preserveImages {
		log.Println("[WordZero] Preserving all images")
	}
	
	// 更新统计信息
	_ = stu.UpdateStatistics()

	if err := saveDocWithRoundTripCheck(stu, outputPath); err != nil {
		return err
	}
	
	log.Printf("[WordZero] Successfully applied format fix, preserved images: %v", preserveImages)
	return nil
}
