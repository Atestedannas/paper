package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/utils"
)

type UniversityHandler struct{}

func NewUniversityHandler() *UniversityHandler {
	return &UniversityHandler{}
}

// GetUniversities 搜索高校
func (h *UniversityHandler) GetUniversities(c *gin.Context) {
	q := c.Query("q")
	tag := c.Query("tag")
	// subject := c.Query("subject") // 暂时未使用，待模型调整后启用
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if pageSize > 100 {
		pageSize = 100
	}

	var universities []model.University
	var total int64

	db := database.DB.Model(&model.University{})

	if q != "" {
		db = db.Where("name LIKE ? OR abbr LIKE ?", "%"+q+"%", "%"+q+"%")
	}

	if tag != "" {
		// PostgreSQL JSONB 查询
		db = db.Where("tags @> ?", fmt.Sprintf(`[{"text": "%s"}]`, tag))
	}

	db.Count(&total)

	offset := (page - 1) * pageSize
	result := db.Offset(offset).Limit(pageSize).Find(&universities)
	if result.Error != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取高校列表失败", result.Error.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"page":      page,
		"page_size": pageSize,
		"total":     total,
		"items":     universities,
	})
}

// GetTags 获取高校标签集合
func (h *UniversityHandler) GetTags(c *gin.Context) {
	var universities []model.University
	if err := database.DB.Find(&universities).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取标签失败", err.Error())
		return
	}

	// 收集所有标签
	tagSet := make(map[string]bool)
	for _, u := range universities {
		// 解析JSONB标签
		if u.Tags != "" {
			var tags []map[string]interface{}
			if err := json.Unmarshal([]byte(u.Tags), &tags); err == nil {
				for _, tag := range tags {
					if text, ok := tag["text"].(string); ok {
						tagSet[text] = true
					}
				}
			}
		}
	}

	// 转换为数组
	var tags []string
	for tagText := range tagSet {
		tags = append(tags, tagText)
	}

	utils.SuccessResponse(c, "获取成功", tags)
}

// GetUniversityDetail 获取高校详情
func (h *UniversityHandler) GetUniversityDetail(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的高校ID", err.Error())
		return
	}

	var university model.University
	if err := database.DB.First(&university, "id = ?", id).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "高校不存在", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", university)
}

// DownloadTemplate 获取高校格式模板信息
func (h *UniversityHandler) DownloadTemplate(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的高校ID", err.Error())
		return
	}

	var university model.University
	if err := database.DB.First(&university, "id = ?", id).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "高校不存在", err.Error())
		return
	}

	// 查找该高校的默认模板（通常是本科论文）
	var template model.FormatTemplate
	// 优先查找本科论文模板
	if err := database.DB.Where("university_id = ? AND is_active = ? AND document_type = ?", id, true, "本科论文").First(&template).Error; err != nil {
		// 如果没找到，查找任意一个激活的模板
		if err := database.DB.Where("university_id = ? AND is_active = ?", id, true).First(&template).Error; err != nil {
			utils.ErrorResponse(c, http.StatusNotFound, "该高校暂无格式模板", err.Error())
			return
		}
	}

	// 返回高校格式要求信息，前端可以直接展示
	utils.SuccessResponse(c, "获取成功", gin.H{
		"university_id":                  university.ID,
		"university_name":                university.Name,
		"format_requirements":            template.FormatRules,
		"format_requirements_translated": h.getTranslatedFormatRequirements(template.FormatRules),
		"document_type":                  template.DocumentType,
		"subject":                        template.Subject,
	})
}

// getTranslatedFormatRequirements 获取格式要求的中文翻译版本
func (h *UniversityHandler) getTranslatedFormatRequirements(formatRequirements string) interface{} {
	if formatRequirements == "" {
		return nil
	}

	var formatRules map[string]interface{}
	if err := json.Unmarshal([]byte(formatRequirements), &formatRules); err != nil {
		return nil
	}

	// 重用管理员处理器中的转换函数
	adminHandler := NewAdminUniversityHandler()
	return adminHandler.convertToFriendlyFormat(formatRules)
}

// generateDOCTemplate 生成DOCX模板文件
func (h *UniversityHandler) generateDOCTemplate(university model.University) (string, error) {
	// 创建上传目录
	uploadDir := filepath.Join("uploads", "university_templates")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return "", err
	}

	// 生成模板文件名

	// 由于不能使用unioffice库，我们创建一个空的或基本的文件
	// 实际的DOCX格式很复杂，需要专门的库来生成
	// 这里返回一个错误信息，提示需要安装相应的库
	return "", fmt.Errorf("需要安装unioffice库来生成DOCX模板，请运行: go get github.com/unidoc/unioffice")
}

// sanitizeFilename 清理文件名
func sanitizeFilename(filename string) string {
	// 移除或替换不允许的字符
	re := regexp.MustCompile(`[<>:"/\\|?*]`)
	sanitized := re.ReplaceAllString(filename, "_")

	// 限制长度
	if len(sanitized) > 100 {
		sanitized = sanitized[:100]
	}

	return sanitized
}

// getFileSize 获取文件大小
func getFileSize(filename string) int64 {
	info, err := os.Stat(filename)
	if err != nil {
		return 0
	}
	return info.Size()
}

// DeleteUniversity 删除高校（管理员）
func (h *UniversityHandler) DeleteUniversity(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的高校ID", err.Error())
		return
	}

	var university model.University
	if err := database.DB.First(&university, "id = ?", id).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "高校不存在", err.Error())
		return
	}

	if err := database.DB.Delete(&university).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除高校失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}
