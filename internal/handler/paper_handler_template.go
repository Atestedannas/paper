package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
	"github.com/paper-format-checker/backend/pkg/formatchecker"
)

func (h *PaperHandler) saveUploadedFile(c *gin.Context, file *multipart.FileHeader, paperID string) (string, error) {
	// 创建上传目录
	uploadDir := filepath.Join(h.config.File.UploadPath, paperID)
	if err := utils.CreateDirIfNotExists(uploadDir); err != nil {
		return "", err
	}

	// 保存文件
	filePath := filepath.Join(uploadDir, file.Filename)
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		return "", err
	}

	return filePath, nil
}

// applyCorrectionsAndGenerateFile keeps old callers from pretending a copy is a fix.
func (h *PaperHandler) applyCorrectionsAndGenerateFile(originalPath string, issues []formatchecker.FormatIssue, corrections []formatchecker.Correction) (string, error) {
	return "", fmt.Errorf("legacy correction file generation is unsupported; use PaperService.FixPaperFormat or DOCXChecker.ApplyCorrections")
}

// UploadTemplate 上传高校论文格式模板
// 支持格式：.docx .doc .pdf .txt .md .rtf .html .htm
// 表单字段 parse_mode: "auto"（默认）/ "sample"（格式范例）/ "description"（格式说明）
func (h *PaperHandler) UploadTemplate(c *gin.Context) {
	var formatText string

	// 优先从表单文本字段获取（纯文本模式）
	formatText = c.PostForm("format_text")
	if strings.TrimSpace(formatText) != "" {
		// 直接使用文本，跳过文件处理
		h.processTemplateText(c, formatText)
		return
	}

	// ── 文件上传模式 ──────────────────────────────────────────────
	file, err := c.FormFile("file")
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请提供格式文本或上传文件", "")
		return
	}

	parseMode := strings.TrimSpace(c.PostForm("parse_mode"))
	if parseMode == "" {
		parseMode = "auto"
	}

	// 支持的扩展名
	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowedExts := map[string]bool{
		".txt": true, ".md": true,
		".doc": true, ".docx": true,
		".pdf":  true,
		".rtf":  true,
		".html": true, ".htm": true,
	}
	if !allowedExts[ext] {
		utils.ErrorResponse(c, http.StatusBadRequest,
			fmt.Sprintf("不支持的文件格式 %s，支持：TXT、MD、DOC、DOCX、PDF、RTF、HTML", ext), "")
		return
	}

	// 创建上传目录
	uploadDir := filepath.Join("uploads", "templates")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建上传目录失败", err.Error())
		return
	}

	// 保存文件（保留原始扩展名，但用 UUID 命名避免冲突）
	tempFileName := fmt.Sprintf("%s_%d%s", uuid.New().String(), time.Now().Unix(), ext)
	tempFilePath := filepath.Join(uploadDir, tempFileName)
	if err := c.SaveUploadedFile(file, tempFilePath); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "保存文件失败", err.Error())
		return
	}

	// 通过文件魔数检测真实格式，不只依赖扩展名
	realType, detectErr := h.detectFileType(tempFilePath)
	if detectErr == nil && realType != "" && realType != ext {
		ext = realType
	}

	// 根据真实格式提取文本
	var extractErr error
	switch ext {
	case ".txt", ".md":
		formatText, extractErr = h.extractTextFromTXT(tempFilePath)
	case ".docx":
		formatText, extractErr = h.extractTextFromDOCXRobust(tempFilePath)
	case ".doc":
		formatText, extractErr = h.extractTextFromDOC(tempFilePath)
	case ".pdf":
		formatText, extractErr = h.extractTextFromPDF(tempFilePath)
	case ".rtf":
		formatText, extractErr = h.extractTextFromRTF(tempFilePath)
	case ".html", ".htm":
		formatText, extractErr = h.extractTextFromHTML(tempFilePath)
	default:
		extractErr = fmt.Errorf("不支持的文件格式: %s", ext)
	}

	// 提取失败时尝试纯文本兜底
	if extractErr != nil || strings.TrimSpace(formatText) == "" {
		fallback, fallbackErr := h.extractTextFallback(tempFilePath)
		if fallbackErr == nil && strings.TrimSpace(fallback) != "" {
			formatText = fallback
			extractErr = nil
		} else if extractErr != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError,
				"提取文件内容失败", fmt.Sprintf("格式: %s, 错误: %v", ext, extractErr))
			return
		}
	}

	if strings.TrimSpace(formatText) == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "文件内容为空，无法解析格式规范", "")
		return
	}

	// ── 格式范例模式检测 ──────────────────────────────────────────
	isSampleMode := false
	switch parseMode {
	case "sample":
		isSampleMode = true
	case "description":
		isSampleMode = false
	default:
		if (ext == ".docx" || ext == ".doc") && formatchecker.IsSampleDocument(formatText) {
			isSampleMode = true
			log.Printf("[上传模板] 自动检测：文件为格式范例（缺少格式描述关键词）")
		}
	}

	if isSampleMode && (ext == ".docx" || ext == ".doc") {
		h.processTemplateSample(c, tempFilePath, ext, formatText)
		return
	}

	h.processTemplateText(c, formatText)
}

// processTemplateSample 处理格式范例文档：直接从DOCX解析格式属性，
// 与AI/正则解析结果合并后保存。
func (h *PaperHandler) processTemplateSample(c *gin.Context, filePath, ext, extractedText string) {
	docxPath := filePath

	// .doc 需要先转为 .docx
	if ext == ".doc" {
		converted := h.trySofficeConvertToDocx(filePath)
		if converted == "" {
			log.Printf("[格式范例] .doc转.docx失败，回退到文本解析模式")
			h.processTemplateText(c, extractedText)
			return
		}
		docxPath = converted
		defer os.Remove(converted)
	}

	parser := formatchecker.NewTemplateParser()
	docxRules, err := parser.ParseTemplateToFormatRules(docxPath)
	if err != nil {
		log.Printf("[格式范例] DOCX格式解析失败: %v，回退到文本解析模式", err)
		h.processTemplateText(c, extractedText)
		return
	}

	log.Printf("[格式范例] DOCX直接解析成功，提取到 %d 个顶层规则", len(docxRules))

	universityName := c.PostForm("university_name")
	documentType := c.PostForm("document_type")
	subject := c.PostForm("subject")
	description := c.PostForm("description")

	// 从 DOCX 解析结果中提取大学名称
	if universityName == "" {
		if uniName, ok := docxRules["_university_name"].(string); ok && uniName != "" {
			universityName = uniName
		}
	}
	delete(docxRules, "_university_name")

	// 仍然尝试从文本中提取大学名称
	if universityName == "" {
		universityInfo := h.formatParserService.ExtractUniversityInfo(extractedText)
		if name, ok := universityInfo["name"]; ok {
			universityName = name
		}
		if docType, ok := universityInfo["document_type"]; ok && documentType == "" {
			documentType = docType
		}
	}
	if universityName == "" {
		aiInfo := h.formatParserService.ExtractUniversityInfoWithAI(extractedText)
		if name, ok := aiInfo["name"]; ok && name != "" {
			universityName = name
		}
		if docType, ok := aiInfo["document_type"]; ok && docType != "" && documentType == "" {
			documentType = docType
		}
	}

	if universityName == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "高校名称不能为空，请通过 university_name 参数提供", "")
		return
	}
	if documentType == "" {
		documentType = "本科论文"
	}
	if subject == "" {
		subject = "综合"
	}

	// 也尝试文本解析（正则+AI），将可测量属性用DOCX解析结果覆盖
	textRulesJSON, textErr := h.formatParserService.ParseFormatFromTextSmart(extractedText)

	if textErr == nil && strings.TrimSpace(textRulesJSON) != "" {
		var textRules map[string]interface{}
		if json.Unmarshal([]byte(textRulesJSON), &textRules) == nil {
			merged := service.MergeFormatRules(docxRules, textRules)
			docxRules = merged
			log.Printf("[格式范例] DOCX规则与文本规则合并完成")
		}
	}

	formatRulesJSON, err := json.Marshal(docxRules)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "序列化格式规则失败", err.Error())
		return
	}

	var university model.University
	if res := database.DB.Where("name = ?", universityName).First(&university); res.Error != nil {
		university = model.University{
			Name:        universityName,
			Abbr:        universityName,
			Description: description,
			Tags:        "[]",
		}
		if err := database.DB.Create(&university).Error; err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "创建高校记录失败", err.Error())
			return
		}
	} else if description != "" {
		database.DB.Model(&university).Update("description", description)
	}

	var existingTemplate model.FormatTemplate
	err = database.DB.Where("university_id = ? AND document_type = ? AND subject = ?",
		university.ID, documentType, subject).First(&existingTemplate).Error
	if err == nil {
		existingTemplate.FormatRules = string(formatRulesJSON)
		existingTemplate.FilePath = docxPath
		existingTemplate.GoldenTemplatePath = docxPath
		existingTemplate.UpdatedAt = time.Now()
		if description != "" {
			existingTemplate.Description = description
		}
		if err := database.DB.Save(&existingTemplate).Error; err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "更新格式模板失败", err.Error())
			return
		}
		utils.Success(c, gin.H{"message": "格式模板已更新（格式范例模式）", "id": existingTemplate.ID, "parse_mode": "sample"})
		return
	}

	newTemplate := model.FormatTemplate{
		TemplateID:         uuid.New().String(),
		Name:               fmt.Sprintf("%s%s格式标准", universityName, documentType),
		UniversityID:       &university.ID,
		DocumentType:       documentType,
		Subject:            subject,
		FilePath:           docxPath,
		Source:             "sample_upload",
		IsActive:           true,
		IsPublic:           true,
		FormatRules:        string(formatRulesJSON),
		GoldenTemplatePath: docxPath,
		Description:        description,
	}
	if err := database.DB.Create(&newTemplate).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建格式模板失败", err.Error())
		return
	}

	utils.Created(c, gin.H{"message": "格式模板已创建（格式范例模式）", "id": newTemplate.ID, "parse_mode": "sample"})
}

// trySofficeConvertToDocx converts a .doc file to .docx using soffice,
// returning the path to the generated .docx or empty string on failure.
func (h *PaperHandler) trySofficeConvertToDocx(filePath string) string {
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
		return ""
	}

	absPath, _ := filepath.Abs(filePath)
	outDir := filepath.Dir(absPath)
	baseName := strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))

	cmd := exec.Command(sofficeBin,
		"--headless", "--convert-to", "docx",
		"--outdir", outDir, absPath,
	)
	if err := cmd.Run(); err != nil {
		log.Printf("[格式范例] soffice转换失败: %v", err)
		return ""
	}

	docxPath := filepath.Join(outDir, baseName+".docx")
	if _, err := os.Stat(docxPath); err != nil {
		return ""
	}
	return docxPath
}

// processTemplateText 用提取的文本解析并保存格式模板（公共逻辑）
func (h *PaperHandler) processTemplateText(c *gin.Context, formatText string) {
	if strings.TrimSpace(formatText) == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "格式文本不能为空", "")
		return
	}

	// 安全上限：防止正则/AI处理超时（200K chars → 正则 <1s）
	const maxTemplateRunes = 200000
	runes := []rune(formatText)
	if len(runes) > maxTemplateRunes {
		log.Printf("[上传模板] 文本过长 (%d 字符)，截断到 %d 字符", len(runes), maxTemplateRunes)
		formatText = string(runes[:maxTemplateRunes])
		runes = runes[:maxTemplateRunes]
	}

	universityName := c.PostForm("university_name")
	documentType := c.PostForm("document_type")
	subject := c.PostForm("subject")
	description := c.PostForm("description")

	preview := string(runes)
	if len(runes) > 500 {
		preview = string(runes[:500])
	}
	log.Printf("[上传模板] 提取文本预览 (%d 字符):\n%s", len(runes), preview)

	if universityName == "" {
		universityInfo := h.formatParserService.ExtractUniversityInfo(formatText)
		log.Printf("[上传模板] 正则提取结果: %v", universityInfo)
		if name, ok := universityInfo["name"]; ok {
			universityName = name
		}
		if docType, ok := universityInfo["document_type"]; ok && documentType == "" {
			documentType = docType
		}
	}

	if universityName == "" {
		log.Println("[上传模板] 正则未识别到高校名称，尝试 AI 提取...")
		aiInfo := h.formatParserService.ExtractUniversityInfoWithAI(formatText)
		if name, ok := aiInfo["name"]; ok && name != "" {
			universityName = name
			log.Printf("[上传模板] AI 识别到高校: %s", universityName)
		}
		if docType, ok := aiInfo["document_type"]; ok && docType != "" && documentType == "" {
			documentType = docType
		}
	}

	if universityName == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "高校名称不能为空，请通过 university_name 参数提供", "")
		return
	}
	if documentType == "" {
		documentType = "本科论文"
	}
	if subject == "" {
		subject = "综合"
	}

	formatRules, err := h.formatParserService.ParseFormatFromTextSmart(formatText)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "解析格式失败", err.Error())
		return
	}

	formatRulesJSON, err := json.Marshal(formatRules)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "序列化格式规则失败", err.Error())
		return
	}

	var university model.University
	if res := database.DB.Where("name = ?", universityName).First(&university); res.Error != nil {
		university = model.University{
			Name:        universityName,
			Abbr:        universityName,
			Description: description,
			Tags:        "[]",
		}
		if err := database.DB.Create(&university).Error; err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "创建高校记录失败", err.Error())
			return
		}
	} else if description != "" {
		database.DB.Model(&university).Update("description", description)
	}

	var existingTemplate model.FormatTemplate
	err = database.DB.Where("university_id = ? AND document_type = ? AND subject = ?",
		university.ID, documentType, subject).First(&existingTemplate).Error
	if err == nil {
		existingTemplate.FormatRules = string(formatRulesJSON)
		existingTemplate.UpdatedAt = time.Now()
		if description != "" {
			existingTemplate.Description = description
		}
		if err := database.DB.Save(&existingTemplate).Error; err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "更新格式模板失败", err.Error())
			return
		}
		utils.Success(c, gin.H{"message": "格式模板已更新", "id": existingTemplate.ID})
		return
	}

	newTemplate := model.FormatTemplate{
		TemplateID:   uuid.New().String(),
		Name:         fmt.Sprintf("%s%s格式标准", universityName, documentType),
		UniversityID: &university.ID,
		DocumentType: documentType,
		Subject:      subject,
		Source:       "university_upload",
		IsActive:     true,
		IsPublic:     true,
		FormatRules:  string(formatRulesJSON),
		Description:  description,
	}
	if err := database.DB.Create(&newTemplate).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建格式模板失败", err.Error())
		return
	}

	utils.Created(c, gin.H{"message": "格式模板已创建", "id": newTemplate.ID})
}
