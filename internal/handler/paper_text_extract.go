package handler

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"github.com/EndFirstCorp/doc2txt"
	"github.com/nguyenthenguyen/docx"
	"rsc.io/pdf"
)

// detectFileType 通过魔数检测文件真实类型，返回对应扩展名
func (h *PaperHandler) detectFileType(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]byte, 8)
	n, err := f.Read(buf)
	if err != nil || n < 4 {
		return "", fmt.Errorf("文件过短")
	}

	// ZIP (DOCX/XLSX/PPTX)
	if buf[0] == 0x50 && buf[1] == 0x4B && buf[2] == 0x03 && buf[3] == 0x04 {
		return ".docx", nil
	}
	// OLE2 Compound (DOC/XLS/PPT)
	if buf[0] == 0xD0 && buf[1] == 0xCF && buf[2] == 0x11 && buf[3] == 0xE0 {
		return ".doc", nil
	}
	// PDF
	if buf[0] == 0x25 && buf[1] == 0x50 && buf[2] == 0x44 && buf[3] == 0x46 {
		return ".pdf", nil
	}
	// RTF
	if n >= 5 && string(buf[:5]) == `{\rtf` {
		return ".rtf", nil
	}
	// HTML (简单检测)
	preview := strings.ToLower(strings.TrimSpace(string(buf[:n])))
	if strings.HasPrefix(preview, "<!doc") || strings.HasPrefix(preview, "<html") {
		return ".html", nil
	}
	return "", nil
}

// extractTextFromTXT 从TXT文件提取文本
func (h *PaperHandler) extractTextFromTXT(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// extractTextFromDOCXRobust 从DOCX文件提取文本（unioffice 优先）
func (h *PaperHandler) extractTextFromDOCXRobust(filePath string) (string, error) {
	// 方案1: unioffice 库 — 正确解析 OOXML 段落结构，无乱码
	if text := h.tryUniofficeExtract(filePath); text != "" {
		return text, nil
	}
	// 方案2: nguyenthenguyen/docx 库
	if doc, err := docx.ReadDocxFile(filePath); err == nil {
		defer doc.Close()
		content := h.cleanDocxContent(doc.Editable().GetContent())
		if content != "" {
			return content, nil
		}
	}
	// 方案3: ZIP 直接解析 document.xml
	return h.extractTextFromDOCXSimple(filePath)
}

// extractTextFromDOCX 从DOCX文件提取文本（保持兼容）
func (h *PaperHandler) extractTextFromDOCX(filePath string) (string, error) {
	return h.extractTextFromDOCXRobust(filePath)
}

// tryUniofficeExtract 使用 unioffice 从 .docx 提取干净的段落文本
func (h *PaperHandler) tryUniofficeExtract(filePath string) string {
	doc, err := document.Open(filePath)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	for _, para := range doc.Paragraphs() {
		var line strings.Builder
		for _, run := range para.Runs() {
			line.WriteString(run.Text())
		}
		text := strings.TrimSpace(line.String())
		if text != "" {
			sb.WriteString(text)
			sb.WriteByte('\n')
		}
	}

	result := strings.TrimSpace(sb.String())
	if len([]rune(result)) < 10 {
		return ""
	}
	log.Printf("[DOCX提取] unioffice 成功: %d 字符", len([]rune(result)))
	return result
}

// extractTextFromDOC 从旧版DOC（OLE2 Compound）文件提取文本
func (h *PaperHandler) extractTextFromDOC(filePath string) (string, error) {
	// 方案 1：先尝试作为 DOCX (ZIP) 处理（有些 .doc 实际上是 DOCX）
	if text, err := h.extractTextFromDOCXRobust(filePath); err == nil && strings.TrimSpace(text) != "" {
		log.Println("[DOC提取] 方案1成功: 文件实际是 DOCX 格式")
		return text, nil
	}

	// 方案 2：PowerShell COM 自动化 — 调用 WPS/Word 提取文本（Windows，最可靠）
	if text := h.tryComDocToText(filePath); text != "" {
		log.Printf("[DOC提取] 方案2成功: COM (WPS/Word), %d 字符", len([]rune(text)))
		return text, nil
	}

	// 方案 3：使用 doc2txt 库解析 OLE2 .doc
	if text := h.tryDoc2txt(filePath); text != "" {
		log.Printf("[DOC提取] 方案3成功: doc2txt, %d 字符", len([]rune(text)))
		return text, nil
	}

	// 方案 4：LibreOffice soffice 命令行转换
	if text := h.trySofficeConvert(filePath); text != "" {
		log.Printf("[DOC提取] 方案4成功: soffice, %d 字符", len([]rune(text)))
		return text, nil
	}

	// 方案 5：扫描 OLE2 二进制中的 UTF-16LE 中文文本（宽松模式）
	if text := h.tryBinaryUTF16Extract(filePath); text != "" {
		log.Printf("[DOC提取] 方案5成功: 二进制UTF-16LE, %d 字符", len([]rune(text)))
		return text, nil
	}

	return "", fmt.Errorf("DOC 文件文本提取失败，建议将文件另存为 DOCX 格式后重新上传")
}

// tryDoc2txt 尝试用 doc2txt 库提取 .doc 文本
func (h *PaperHandler) tryDoc2txt(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	reader, err := doc2txt.ParseDoc(f)
	if err != nil {
		log.Printf("[DOC提取] doc2txt 解析失败: %v", err)
		return ""
	}

	const maxBytes = 2 * 1024 * 1024
	textBytes, err := io.ReadAll(io.LimitReader(reader, maxBytes))
	if err != nil {
		log.Printf("[DOC提取] doc2txt 读取失败: %v", err)
		return ""
	}

	text := h.cleanExtractedText(string(textBytes))
	chineseCount := countChinese(text)
	log.Printf("[DOC提取] doc2txt 结果: %d 字符, 中文 %d 个", len([]rune(text)), chineseCount)

	if chineseCount < 30 {
		log.Println("[DOC提取] doc2txt 结果质量不佳，尝试其他方案")
		return ""
	}
	return text
}

// tryComDocToText 使用 PowerShell COM 自动化 (WPS / Word) 提取 .doc 纯文本
func (h *PaperHandler) tryComDocToText(filePath string) string {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return ""
	}
	absPath = strings.ReplaceAll(absPath, `/`, `\`)
	escapedPath := strings.ReplaceAll(absPath, `'`, `''`)

	// PowerShell 脚本：尝试多个 COM ProgID，通过 stdout 输出文本
	psScript := fmt.Sprintf(`
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$ErrorActionPreference = 'Stop'
$path = '%s'
$app = $null
$ids = @('Word.Application','KWps.Application','wps.Application','KWPS.Application')
foreach ($id in $ids) {
  try { $app = New-Object -ComObject $id; break } catch {}
}
if (-not $app) { exit 1 }
$app.Visible = $false
$app.DisplayAlerts = 0
try {
  $doc = $app.Documents.Open($path)
  [Console]::Out.Write($doc.Content.Text)
  $doc.Close([ref]$false)
} finally {
  $app.Quit()
}
`, escapedPath)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[DOC提取] COM 失败: %v (output: %.200s)", err, string(output))
		return ""
	}

	text := strings.TrimSpace(string(output))
	chineseCount := countChinese(text)
	log.Printf("[DOC提取] COM (WPS/Word) 结果: %d 字符, 中文 %d 个", len([]rune(text)), chineseCount)

	if chineseCount < 10 {
		return ""
	}
	return text
}

// trySofficeConvert 尝试使用 LibreOffice soffice 将 .doc 转为 .txt
func (h *PaperHandler) trySofficeConvert(filePath string) string {
	sofficePaths := []string{
		"soffice",
		`C:\Program Files\LibreOffice\program\soffice.exe`,
		`C:\Program Files (x86)\LibreOffice\program\soffice.exe`,
		"/usr/bin/soffice",
		"/usr/local/bin/soffice",
	}

	var sofficeBin string
	for _, p := range sofficePaths {
		if _, err := exec.LookPath(p); err == nil {
			sofficeBin = p
			break
		}
	}
	if sofficeBin == "" {
		log.Println("[DOC提取] LibreOffice 未找到，跳过")
		return ""
	}

	absPath, _ := filepath.Abs(filePath)
	outDir := filepath.Dir(absPath)
	baseName := strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
	txtPath := filepath.Join(outDir, baseName+".txt")
	defer os.Remove(txtPath)

	cmd := exec.Command(sofficeBin,
		"--headless", "--convert-to", "txt:Text (encoded):UTF8",
		"--outdir", outDir, absPath,
	)
	cmd.Env = append(os.Environ(), "HOME="+os.TempDir())

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[DOC提取] soffice 转换失败: %v, output: %s", err, string(output))
		return ""
	}

	textBytes, err := os.ReadFile(txtPath)
	if err != nil {
		return ""
	}

	text := strings.TrimSpace(string(textBytes))
	chineseCount := countChinese(text)
	log.Printf("[DOC提取] soffice 成功: %d 字符, 中文 %d 个", len([]rune(text)), chineseCount)
	if chineseCount < 10 {
		return ""
	}
	return text
}

// tryBinaryUTF16Extract 从 OLE2 二进制中扫描 UTF-16LE 编码的中文文本
// 这是最后的保底方案：虽然会有部分乱码，但能保证提取到关键的中文内容（如高校名称）
func (h *PaperHandler) tryBinaryUTF16Extract(filePath string) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	// 跳过 OLE2 文件头元数据
	startOffset := 0
	if len(data) > 0x900 {
		startOffset = 0x900
	}

	var sb strings.Builder
	const maxOutput = 500 * 1024

	i := startOffset
	for i < len(data)-1 {
		lo, hi := data[i], data[i+1]
		cp := uint16(lo) | uint16(hi)<<8

		switch {
		case cp >= 0x4E00 && cp <= 0x9FFF:
			sb.WriteRune(rune(cp))
			i += 2
		case cp >= 0x3000 && cp <= 0x303F:
			sb.WriteRune(rune(cp))
			i += 2
		case cp >= 0xFF00 && cp <= 0xFFEF:
			sb.WriteRune(rune(cp))
			i += 2
		case lo >= 0x20 && lo < 0x7F:
			sb.WriteByte(lo)
			i++
		case lo == 0x0D || lo == 0x0A:
			sb.WriteByte('\n')
			i++
		default:
			i++
		}

		if sb.Len() > maxOutput {
			break
		}
	}

	text := h.cleanExtractedText(sb.String())
	chineseCount := countChinese(text)
	log.Printf("[DOC提取] 二进制UTF-16LE: %d 字符, 中文 %d 个", len([]rune(text)), chineseCount)

	if chineseCount < 5 {
		return ""
	}
	return text
}

func countChinese(text string) int {
	count := 0
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			count++
		}
	}
	return count
}

// extractTextFromRTF 从RTF文件提取纯文本
func (h *PaperHandler) extractTextFromRTF(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	text := string(data)
	// 移除RTF控制字和组
	re := regexp.MustCompile(`\\\*[^\\{}]+|\\[a-z]+[-\d]*\s?|\{|\}`)
	text = re.ReplaceAllString(text, " ")
	// 处理 \uN 转义（Unicode字符）
	uniRe := regexp.MustCompile(`\\u(\d+)\??`)
	text = uniRe.ReplaceAllStringFunc(text, func(m string) string {
		matches := uniRe.FindStringSubmatch(m)
		if len(matches) < 2 {
			return ""
		}
		if n, err := strconv.Atoi(matches[1]); err == nil {
			if n < 0 {
				n += 65536
			}
			return string(rune(n))
		}
		return ""
	})
	return h.cleanExtractedText(text), nil
}

// extractTextFromHTML 从HTML文件提取纯文本
func (h *PaperHandler) extractTextFromHTML(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	text := string(data)
	// 移除 <script> 和 <style> 块
	text = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`).ReplaceAllString(text, " ")
	text = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`).ReplaceAllString(text, " ")
	// 移除所有HTML标签
	text = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, " ")
	// HTML实体解码（常见）
	text = strings.NewReplacer(
		"&nbsp;", " ", "&lt;", "<", "&gt;", ">",
		"&amp;", "&", "&quot;", `"`, "&#39;", "'",
	).Replace(text)
	return h.cleanExtractedText(text), nil
}

// extractTextFallback 兜底：尝试将文件作为文本读取
func (h *PaperHandler) extractTextFallback(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	// 仅保留可打印字符和中文
	var sb strings.Builder
	for _, r := range string(data) {
		if r >= 0x20 && r < 0x7F || r >= 0x4E00 && r <= 0x9FFF || r == '\n' || r == '\r' {
			sb.WriteRune(r)
		}
	}
	text := h.cleanExtractedText(sb.String())
	if len(text) < 10 {
		return "", fmt.Errorf("文件内容无法识别")
	}
	return text, nil
}

// cleanDocxContent 清理DOCX内容（从原始XML中提取纯文本）
func (h *PaperHandler) cleanDocxContent(content string) string {
	// GetContent() 返回的是原始 XML，需要先提取 <w:t> 标签中的文本
	if strings.Contains(content, "<w:") {
		var sb strings.Builder
		textRe := regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>`)
		// 用段落标签作为换行分隔
		paraContent := regexp.MustCompile(`</w:p>`).ReplaceAllString(content, "</w:p>\n")

		for _, line := range strings.Split(paraContent, "\n") {
			matches := textRe.FindAllStringSubmatch(line, -1)
			if len(matches) == 0 {
				continue
			}
			var lineText strings.Builder
			for _, m := range matches {
				if len(m) > 1 {
					lineText.WriteString(m[1])
				}
			}
			text := strings.TrimSpace(lineText.String())
			if text != "" {
				sb.WriteString(text)
				sb.WriteString("\n")
			}
		}
		content = sb.String()
	}

	// XML 实体解码
	content = strings.ReplaceAll(content, "&lt;", "<")
	content = strings.ReplaceAll(content, "&gt;", ">")
	content = strings.ReplaceAll(content, "&amp;", "&")
	content = strings.ReplaceAll(content, "&quot;", "\"")
	content = strings.ReplaceAll(content, "&#39;", "'")

	// 移除残留的 XML 标签
	content = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(content, "")
	// 合并多个连续空行为一个
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")
	content = strings.TrimSpace(content)
	return content
}

// extractTextFromDOCXSimple DOCX文本提取的简化实现（改进版）
func (h *PaperHandler) extractTextFromDOCXSimple(filePath string) (string, error) {
	// DOCX 是一个 ZIP 文件，包含 word/document.xml
	// 这里提供一个改进的实现

	// 打开 ZIP 文件
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("无法打开DOCX文件: %v", err)
	}
	defer r.Close()

	// 查找 word/document.xml
	var documentXML *zip.File
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			documentXML = f
			break
		}
	}

	if documentXML == nil {
		return "", fmt.Errorf("DOCX文件格式错误：找不到document.xml")
	}

	// 读取 XML 内容
	rc, err := documentXML.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	content, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	// 解析 XML 内容
	text := string(content)

	// 提取所有文本内容，不仅仅是<w:t>标签
	// 使用更全面的正则表达式来提取文本
	textRe := regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>|<w:instrText[^>]*>([^<]*)</w:instrText>|<w:delText[^>]*>([^<]*)</w:delText>`)
	matches := textRe.FindAllStringSubmatch(text, -1)

	var extractedText strings.Builder

	for _, match := range matches {
		// 检查不同的捕获组
		for i := 1; i < len(match); i++ {
			if match[i] != "" {
				textContent := match[i]
				// 解码可能的XML实体
				textContent = strings.ReplaceAll(textContent, "&lt;", "<")
				textContent = strings.ReplaceAll(textContent, "&gt;", ">")
				textContent = strings.ReplaceAll(textContent, "&amp;", "&")
				textContent = strings.ReplaceAll(textContent, "&quot;", "\"")
				textContent = strings.ReplaceAll(textContent, "&#39;", "'")
				extractedText.WriteString(textContent)
				break
			}
		}
	}

	// 另一种方法：使用更简单的正则表达式移除所有XML标签
	cleanText := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, " ")
	// 清理多个空格
	cleanText = regexp.MustCompile(`\s+`).ReplaceAllString(cleanText, " ")
	// 如果通过标签提取的内容为空，使用清理标签的方法
	if strings.TrimSpace(extractedText.String()) == "" {
		return cleanText, nil
	}
	// 否则，使用标签提取的内容
	text = extractedText.String()

	result := extractedText.String()

	// 清理结果
	result = h.cleanExtractedText(result)

	if result == "" {
		return "", fmt.Errorf("无法从DOCX文件中提取文本内容")
	}

	return result, nil
}

// cleanExtractedText 清理提取的文本
func (h *PaperHandler) cleanExtractedText(text string) string {
	// 移除多余的空格
	text = regexp.MustCompile(`[ \t]+`).ReplaceAllString(text, " ")
	// 移除多余的换行
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	// 移除首尾空白
	text = strings.TrimSpace(text)
	return text
}

// extractTextFromPDF 从PDF文件提取文本（改进版）
func (h *PaperHandler) extractTextFromPDF(filePath string) (string, error) {
	// 打开PDF文件
	r, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("无法打开PDF文件: %v", err)
	}

	var textBuilder strings.Builder
	totalPages := r.NumPage()

	// 限制只读取前20页（格式规范通常在前几页）
	maxPages := min(20, totalPages)

	for pageNum := 1; pageNum <= maxPages; pageNum++ {
		page := r.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		// 提取页面文本内容
		content := page.Content()

		// 遍历文本对象
		for _, text := range content.Text {
			// 过滤空白文本
			if strings.TrimSpace(text.S) != "" {
				textBuilder.WriteString(text.S)
				textBuilder.WriteString(" ")
			}
		}
		textBuilder.WriteString("\n")
	}

	result := textBuilder.String()

	// 清理文本
	result = h.cleanPDFContent(result)

	if result == "" {
		return "", fmt.Errorf("无法从PDF文件中提取文本内容")
	}

	return result, nil
}

// cleanPDFContent 清理PDF提取的内容
func (h *PaperHandler) cleanPDFContent(content string) string {
	// 移除多余的空白字符
	content = regexp.MustCompile(`[ \t]+`).ReplaceAllString(content, " ")
	// 移除多余的换行
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")
	// 移除首尾空白
	content = strings.TrimSpace(content)
	return content
}

// extractTextFromPDFSimple PDF文本提取的简化实现（备用）
func (h *PaperHandler) extractTextFromPDFSimple(filePath string) (string, error) {
	// 打开PDF文件
	r, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("无法打开PDF文件: %v", err)
	}

	var textBuilder strings.Builder
	totalPages := r.NumPage()

	// 限制只读取前10页（避免处理时间过长）
	maxPages := min(10, totalPages)

	for pageNum := 1; pageNum <= maxPages; pageNum++ {
		page := r.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		// 提取页面文本内容
		// rsc.io/pdf 库的文本提取比较基础
		content := page.Content()

		// 遍历文本对象
		for _, text := range content.Text {
			textBuilder.WriteString(text.S)
			textBuilder.WriteString(" ")
		}
		textBuilder.WriteString("\n")
	}

	result := textBuilder.String()
	if result == "" {
		return "", fmt.Errorf("无法从PDF文件中提取文本内容")
	}

	return result, nil
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
