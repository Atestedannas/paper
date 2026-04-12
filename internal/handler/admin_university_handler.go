package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// AdminUniversityHandler 高校管理处理器
type AdminUniversityHandler struct{}

// NewAdminUniversityHandler 创建高校管理处理器实例
func NewAdminUniversityHandler() *AdminUniversityHandler {
	return &AdminUniversityHandler{}
}

// convertToFriendlyFormat 将格式规则转换为友好展示格式（与 adminConvertFormatRulesToChineseFriendly 一致）
func (h *AdminUniversityHandler) convertToFriendlyFormat(formatRules map[string]interface{}) map[string]interface{} {
	return adminConvertFormatRulesToChineseFriendly(formatRules)
}

// 以下是管理员高校管理接口的占位实现
// GetUniversities 获取高校列表
func (h *AdminUniversityHandler) GetUniversities(c *gin.Context) {
	q := c.Query("q")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	if pageSize > 100 {
		pageSize = 100
	}

	var universities []model.University
	var total int64

	db := database.DB.Model(&model.University{})

	if q != "" {
		db = db.Where("name LIKE ? OR abbr LIKE ?", "%"+q+"%", "%"+q+"%")
	}

	db.Count(&total)

	offset := (page - 1) * pageSize
	result := db.Preload("Templates").Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&universities)
	if result.Error != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取高校列表失败", result.Error.Error())
		return
	}

	// 填充虚拟字段
	for i := range universities {
		if len(universities[i].Templates) > 0 {
			// 取最新的模板（假设最后一个或按Version排序，这里简单取第一个）
			// 实际应该找IsActive=true的
			var activeTemplate *model.FormatTemplate
			for j := range universities[i].Templates {
				if universities[i].Templates[j].IsActive {
					activeTemplate = &universities[i].Templates[j]
					break
				}
			}
			if activeTemplate == nil {
				activeTemplate = &universities[i].Templates[0]
			}

			universities[i].FormatRequirements = json.RawMessage(activeTemplate.FormatRules)
			universities[i].FilePath = activeTemplate.FilePath
			universities[i].Subject = activeTemplate.Subject
			universities[i].DocumentType = activeTemplate.DocumentType

			// 简单的URL构造，实际可能需要更复杂的逻辑
			if strings.HasSuffix(strings.ToLower(activeTemplate.FilePath), ".docx") {
				universities[i].DocxTemplateURL = "/" + activeTemplate.FilePath // 假设FilePath是相对uploads的路径，或者已经是完整路径
				// 如果FilePath是绝对路径或不带/uploads前缀，需调整
				if !strings.HasPrefix(activeTemplate.FilePath, "/") && !strings.HasPrefix(activeTemplate.FilePath, "http") {
					universities[i].DocxTemplateURL = "/" + strings.ReplaceAll(activeTemplate.FilePath, "\\", "/")
				}
			} else if strings.HasSuffix(strings.ToLower(activeTemplate.FilePath), ".pdf") {
				universities[i].PdfTemplateURL = "/" + strings.ReplaceAll(activeTemplate.FilePath, "\\", "/")
			}
		}
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"page":      page,
		"page_size": pageSize,
		"total":     total,
		"items":     universities,
	})
}

// GetUniversity 获取高校详情
func (h *AdminUniversityHandler) GetUniversity(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的高校ID", err.Error())
		return
	}

	var university model.University
	if err := database.DB.Preload("Templates").First(&university, "id = ?", id).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "高校不存在", err.Error())
		return
	}

	// 填充虚拟字段
	if len(university.Templates) > 0 {
		var activeTemplate *model.FormatTemplate
		for j := range university.Templates {
			if university.Templates[j].IsActive {
				activeTemplate = &university.Templates[j]
				break
			}
		}
		if activeTemplate == nil {
			activeTemplate = &university.Templates[0]
		}

		university.FormatRequirements = json.RawMessage(activeTemplate.FormatRules)
		university.FilePath = activeTemplate.FilePath
		university.Subject = activeTemplate.Subject
		university.DocumentType = activeTemplate.DocumentType

		path := "/" + strings.ReplaceAll(activeTemplate.FilePath, "\\", "/")
		if strings.HasSuffix(strings.ToLower(activeTemplate.FilePath), ".docx") {
			university.DocxTemplateURL = path
		} else if strings.HasSuffix(strings.ToLower(activeTemplate.FilePath), ".pdf") {
			university.PdfTemplateURL = path
		}
	}

	utils.SuccessResponse(c, "获取成功", university)
}

// CreateUniversity 创建高校
func (h *AdminUniversityHandler) CreateUniversity(c *gin.Context) {
	var requestData struct {
		Name        string          `json:"name" binding:"required"`
		Abbr        string          `json:"abbr"`
		Subject     string          `json:"subject" binding:"required"`
		Description string          `json:"description"`
		Color       string          `json:"color"`
		Tags        string          `json:"tags"`
		FormatRules json.RawMessage `json:"format_rules"` // 可选：格式规则
	}

	if err := c.ShouldBindJSON(&requestData); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 1. 创建高校记录
	university := model.University{
		Name:        requestData.Name,
		Abbr:        requestData.Abbr,
		Description: requestData.Description,
		Color:       requestData.Color,
		Tags:        requestData.Tags,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	tx := database.DB.Begin()

	if err := tx.Create(&university).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建高校失败", err.Error())
		return
	}

	// 2. 如果提供了格式规则，创建关联的格式模板
	if len(requestData.FormatRules) > 0 {
		template := model.FormatTemplate{
			TemplateID:   fmt.Sprintf("%s_default_%d", strings.ToLower(requestData.Abbr), time.Now().Unix()),
			Name:         fmt.Sprintf("%s默认格式标准", requestData.Name),
			UniversityID: &university.ID,
			DocumentType: "thesis", // 默认本科论文
			Subject:      requestData.Subject,
			Source:       "system",
			Version:      "1.0",
			IsPublic:     true,
			IsActive:     true,
			FormatRules:  string(requestData.FormatRules),
			Description:  fmt.Sprintf("%s默认格式标准", requestData.Name),
		}

		if err := tx.Create(&template).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(c, http.StatusInternalServerError, "创建格式模板失败", err.Error())
			return
		}
	}

	tx.Commit()

	utils.SuccessResponse(c, "创建成功", university)
}

// UpdateUniversity 更新高校
func (h *AdminUniversityHandler) UpdateUniversity(c *gin.Context) {
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

	// 获取请求参数
	var requestData map[string]interface{}
	if err := c.ShouldBindJSON(&requestData); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	// 准备更新字段
	updates := make(map[string]interface{})

	// 检查是否有需要更新的字段
	if name, ok := requestData["name"].(string); ok && name != "" {
		updates["name"] = name
	}
	if abbr, ok := requestData["abbr"].(string); ok && abbr != "" {
		updates["abbr"] = abbr
	}
	if description, ok := requestData["description"].(string); ok {
		updates["description"] = description
	}
	if color, ok := requestData["color"].(string); ok && color != "" {
		updates["color"] = color
	}
	if tags, ok := requestData["tags"].(string); ok && tags != "" {
		updates["tags"] = tags
	}

	// 设置更新时间
	updates["updated_at"] = time.Now()

	// 开启事务
	tx := database.DB.Begin()

	// 执行更新高校基本信息
	if len(updates) > 0 {
		if err := tx.Model(&university).Updates(updates).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(c, http.StatusInternalServerError, "更新高校失败", err.Error())
			return
		}
	}

	// 检查是否需要更新格式规则
	if formatRequirementsVal, ok := requestData["format_requirements"]; ok && formatRequirementsVal != nil {
		var formatRequirementsStr string

		// 处理不同类型的输入
		switch v := formatRequirementsVal.(type) {
		case string:
			formatRequirementsStr = v
		case map[string]interface{}:
			// 如果是JSON对象，序列化为字符串
			bytes, err := json.Marshal(v)
			if err != nil {
				utils.ErrorResponse(c, http.StatusBadRequest, "格式规则格式错误", err.Error())
				return
			}
			formatRequirementsStr = string(bytes)
		case []interface{}: // 可能是数组
			bytes, err := json.Marshal(v)
			if err != nil {
				utils.ErrorResponse(c, http.StatusBadRequest, "格式规则格式错误", err.Error())
				return
			}
			formatRequirementsStr = string(bytes)
		default:
			// 尝试作为通用JSON处理
			bytes, err := json.Marshal(v)
			if err == nil {
				formatRequirementsStr = string(bytes)
			}
		}

		if formatRequirementsStr != "" {
			// 查找关联的模板
			var templates []model.FormatTemplate
			if err := tx.Where("university_id = ?", id).Find(&templates).Error; err != nil {
				tx.Rollback()
				utils.ErrorResponse(c, http.StatusInternalServerError, "获取关联模板失败", err.Error())
				return
			}

			var activeTemplate *model.FormatTemplate
			// 找活跃的模板
			for i := range templates {
				if templates[i].IsActive {
					activeTemplate = &templates[i]
					break
				}
			}

			if activeTemplate != nil {
				// 更新现有模板
				if err := tx.Model(activeTemplate).Update("format_rules", formatRequirementsStr).Error; err != nil {
					tx.Rollback()
					utils.ErrorResponse(c, http.StatusInternalServerError, "更新格式规则失败", err.Error())
					return
				}
			} else {
				// 如果没有模板，创建一个新的
				newTemplate := model.FormatTemplate{
					TemplateID:   fmt.Sprintf("%s_default_%d", strings.ToLower(university.Abbr), time.Now().Unix()),
					Name:         fmt.Sprintf("%s默认格式标准", university.Name),
					UniversityID: &university.ID,
					DocumentType: "thesis",
					Subject:      "综合", // 默认
					Source:       "system",
					Version:      "1.0",
					IsPublic:     true,
					IsActive:     true,
					FormatRules:  formatRequirementsStr,
				}
				if err := tx.Create(&newTemplate).Error; err != nil {
					tx.Rollback()
					utils.ErrorResponse(c, http.StatusInternalServerError, "创建格式模板失败", err.Error())
					return
				}
			}
		}
	}

	tx.Commit()

	// 获取更新后的数据 (包含模板信息)
	var updatedUniversity model.University
	if err := database.DB.Preload("Templates").First(&updatedUniversity, "id = ?", id).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取更新后的数据失败", err.Error())
		return
	}

	// 填充虚拟字段
	if len(updatedUniversity.Templates) > 0 {
		var activeTemplate *model.FormatTemplate
		for j := range updatedUniversity.Templates {
			if updatedUniversity.Templates[j].IsActive {
				activeTemplate = &updatedUniversity.Templates[j]
				break
			}
		}
		if activeTemplate == nil {
			activeTemplate = &updatedUniversity.Templates[0]
		}
		updatedUniversity.FormatRequirements = json.RawMessage(activeTemplate.FormatRules)
		updatedUniversity.FilePath = activeTemplate.FilePath
	}

	utils.SuccessResponse(c, "更新成功", updatedUniversity)
}

// DeleteUniversity 删除高校
func (h *AdminUniversityHandler) DeleteUniversity(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的高校ID", err.Error())
		return
	}

	// 开启事务
	tx := database.DB.Begin()

	// 1. 删除关联的模板 (软删除或硬删除，这里假设硬删除或由GORM处理)
	// 如果FormatTemplate有DeletedAt，GORM会软删除。这里手动处理一下关联。
	if err := tx.Where("university_id = ?", id).Delete(&model.FormatTemplate{}).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除关联模板失败", err.Error())
		return
	}

	// 2. 删除高校
	if err := tx.Delete(&model.University{}, id).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除高校失败", err.Error())
		return
	}

	tx.Commit()

	utils.SuccessResponse(c, "删除成功", nil)
}

// BatchUpdateUniversities 批量更新高校
func (h *AdminUniversityHandler) BatchUpdateUniversities(c *gin.Context) {
	utils.ErrorResponse(c, http.StatusNotImplemented, "此功能尚未实现", "")
}

// ParseTemplate 解析上传的模板文件
func (h *AdminUniversityHandler) ParseTemplate(c *gin.Context) {
	// 1. 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请上传文件", err.Error())
		return
	}

	// 2. 检查文件类型
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".docx" {
		utils.ErrorResponse(c, http.StatusBadRequest, "不支持的文件类型", "仅支持 .docx 文件")
		return
	}

	// 3. 保存文件到临时目录
	tempDir := "temp/uploads"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建临时目录失败", err.Error())
		return
	}

	filename := fmt.Sprintf("%s_%s", uuid.New().String(), file.Filename)
	filePath := filepath.Join(tempDir, filename)

	if err := c.SaveUploadedFile(file, filePath); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "保存文件失败", err.Error())
		return
	}
	// 解析完成后删除临时文件
	defer os.Remove(filePath)

	// 4. 调用服务解析模板
	svc := service.NewTemplateParserService()
	standard, err := svc.ParseTemplateFromFile(filePath)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "解析模板失败", err.Error())
		return
	}

	// 5. 返回解析结果
	// 返回 university_info (如果有) 和 format_rules
	utils.SuccessResponse(c, "解析成功", gin.H{
		"university_name": standard.Name, // TemplateParser 可能会从页眉提取学校名称
		"format_rules":    standard,
	})
}
