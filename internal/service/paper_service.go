package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/pkg/fileprocessor"
	"github.com/paper-format-checker/backend/pkg/formatchecker"
	"gorm.io/gorm"
)

// PaperService 璁烘枃鏈嶅姟
type PaperService struct {
	config *config.Config
}

// NewPaperService 鍒涘缓璁烘枃鏈嶅姟
func NewPaperService(config *config.Config) PaperService {
	return PaperService{
		config: config,
	}
}

// CheckPaperFormat 妫€鏌ヨ鏂囨牸寮?
func (s PaperService) CheckPaperFormat(userID, paperID, templateID uuid.UUID) (*model.CheckResult, error) {
	// 1. 鑾峰彇璁烘枃淇℃伅

	paper, err := s.GetPaperByID(userID, paperID)
	if err != nil {
		return nil, fmt.Errorf("failed to get paper: %v", err)
	}

	// 2. 纭畾浣跨敤鐨勬ā鏉縄D
	if templateID == uuid.Nil {
		if paper.SelectedTemplateID != nil {
			templateID = *paper.SelectedTemplateID
		} else {
			// 濡傛灉娌℃湁鎸囧畾妯℃澘锛岃繑鍥為敊璇垨浣跨敤榛樿閫昏緫
			return nil, fmt.Errorf("no template selected for paper")
		}
	}

	// 3. 鑾峰彇鏍煎紡妯℃澘
	var template model.FormatTemplate
	if err := database.DB.Where("id = ?", templateID).First(&template).Error; err != nil {
		return nil, fmt.Errorf("failed to get format template: %v", err)
	}

	// 4. 瑙ｆ瀽鏍煎紡瑙勫垯
	var rulesMap map[string]interface{}
	// 灏濊瘯鐩存帴瑙ｆ瀽
	if err := json.Unmarshal([]byte(template.FormatRules), &rulesMap); err != nil {
		// 濡傛灉澶辫触锛屽皾璇曞厛瑙ｆ瀽涓哄瓧绗︿覆锛堝鐞嗗弻閲嶅簭鍒楀寲鐨勬儏鍐碉級
		var jsonString string
		if err2 := json.Unmarshal([]byte(template.FormatRules), &jsonString); err2 == nil {
			// 濡傛灉瑙ｆ瀽涓哄瓧绗︿覆鎴愬姛锛屽啀灏濊瘯瑙ｆ瀽璇ュ瓧绗︿覆鐨勫唴瀹?
			if err3 := json.Unmarshal([]byte(jsonString), &rulesMap); err3 != nil {
				return nil, fmt.Errorf("failed to unmarshal format rules (double encoded): %v", err3)
			}
		} else {
			return nil, fmt.Errorf("failed to unmarshal format rules: %v", err)
		}
	}

	// 5. 鍒涘缓妫€鏌ュ櫒
	standard := formatchecker.ParseRequirementsToStandard(rulesMap)

	processor := fileprocessor.NewBasicFileProcessor() // 浣跨敤鍩烘湰澶勭悊鍣ㄨ繘琛屾鏌?
	factory := formatchecker.NewCheckerFactory()

	checker, err := factory.CreateChecker(paper.FileType, processor, standard)
	if err != nil {
		return nil, fmt.Errorf("failed to create checker: %v", err)
	}

	// 6. 鎵ц妫€鏌?
	ctx := context.Background()
	checkResult, err := checker.Check(ctx, paper.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to check paper format: %v", err)
	}

	// 7. 淇濆瓨妫€鏌ョ粨鏋?
	issuesJSON, _ := json.Marshal(checkResult.Issues)

	resultUserID := userID
	if resultUserID == uuid.Nil {
		resultUserID = paper.UserID
	}

	result := &model.CheckResult{
		ID:               uuid.New(),
		PaperID:          paperID,
		UserID:           resultUserID,
		TemplateID:       templateID,
		FormatTemplateID: templateID, // 鍚屾椂璧嬪€间互婊¤冻鏁版嵁搴撶害鏉?
		Status:           "completed",
		TotalIssues:      checkResult.TotalIssues,
		ErrorCount:       checkResult.ErrorCount,
		WarningCount:     checkResult.WarningCount,
		InfoCount:        checkResult.InfoCount,
		Issues:           string(issuesJSON),
		Differences:      "[]", // 鍒濆鍖栦负绌?JSON 鏁扮粍锛岄伩鍏?PostgreSQL 鎶ラ敊
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := database.DB.Create(result).Error; err != nil {
		return nil, fmt.Errorf("failed to save check result: %v", err)
	}

	return result, nil
}

// FixPaperFormatByParsedRequirements 鏍规嵁瑙ｆ瀽鐨勮姹備慨澶嶈鏂囨牸寮?
func (s PaperService) FixPaperFormatByParsedRequirements(userID, paperID uuid.UUID, requirements map[string]interface{}) (interface{}, error) {
	// 鑾峰彇璁烘枃淇℃伅

	paper, err := s.GetPaperByID(userID, paperID)
	if err != nil {
		return nil, fmt.Errorf("failed to get paper: %v", err)
	}

	// 鍒涘缓妫€鏌ュ櫒閰嶇疆
	standard := formatchecker.ParseRequirementsToStandard(requirements)
	processor := fileprocessor.NewBasicFileProcessor() // 浣跨敤鍩烘湰澶勭悊鍣ㄨ繘琛屾鏌?
	factory := formatchecker.NewCheckerFactory()
	checker, err := factory.CreateChecker(paper.FileType, processor, standard)

	if err != nil {
		return nil, fmt.Errorf("failed to create checker: %v", err)
	}

	// 淇鏂囨。
	ctx := context.Background()
	fixedPath, err := checker.FixDocumentDirectly(ctx, paper.FilePath, standard)
	if err != nil {
		return nil, fmt.Errorf("failed to fix document: %v", err)
	}
	if fixedPath == "" {
		return nil, fmt.Errorf("failed to fix document: empty corrected file path")
	}

	// 鏇存柊璁烘枃璁板綍
	paper.CorrectedFilePath = fixedPath
	paper.Status = "corrected"
	if err := database.DB.Save(paper).Error; err != nil {
		return nil, fmt.Errorf("failed to update paper record: %v", err)
	}

	return map[string]interface{}{
		"corrected_file_path": fixedPath,
		"download_url":        fmt.Sprintf("/api/v1/papers/%s/corrected-file", paper.ID.String()),
	}, nil
}

// FixPaperFormat 淇璁烘枃鏍煎紡
func (s PaperService) FixPaperFormat(userID, paperID, checkResultID uuid.UUID) (interface{}, error) {
	// 1. 鑾峰彇璁烘枃鍜屾鏌ョ粨鏋?
	paper, err := s.GetPaperByID(userID, paperID)
	if err != nil {
		return nil, fmt.Errorf("failed to get paper: %v", err)
	}

	var checkResult model.CheckResult
	if err := database.DB.Where("id = ?", checkResultID).First(&checkResult).Error; err != nil {
		return nil, fmt.Errorf("failed to get check result: %v", err)
	}

	// 2. 鑾峰彇鏍煎紡妯℃澘
	var template model.FormatTemplate
	if err := database.DB.Where("id = ?", checkResult.TemplateID).First(&template).Error; err != nil {
		return nil, fmt.Errorf("failed to get format template: %v", err)
	}

	// 3. 鍑嗗妫€鏌ュ櫒
	var rulesMap map[string]interface{}
	// 灏濊瘯鐩存帴瑙ｆ瀽
	if err := json.Unmarshal([]byte(template.FormatRules), &rulesMap); err != nil {
		// 濡傛灉澶辫触锛屽皾璇曞厛瑙ｆ瀽涓哄瓧绗︿覆锛堝鐞嗗弻閲嶅簭鍒楀寲鐨勬儏鍐碉級
		var jsonString string
		if err2 := json.Unmarshal([]byte(template.FormatRules), &jsonString); err2 == nil {
			// 濡傛灉瑙ｆ瀽涓哄瓧绗︿覆鎴愬姛锛屽啀灏濊瘯瑙ｆ瀽璇ュ瓧绗︿覆鐨勫唴瀹?
			if err3 := json.Unmarshal([]byte(jsonString), &rulesMap); err3 != nil {
				return nil, fmt.Errorf("failed to unmarshal format rules (double encoded): %v", err3)
			}
		} else {
			return nil, fmt.Errorf("failed to unmarshal format rules: %v", err)
		}
	}

	processor := fileprocessor.NewEnhancedProcessor()

	// 4. 淇鏂囨。
	ctx := context.Background()
	var fixedPath string
	var fixErr error

	// 浣跨敤澧炲己澶勭悊鍣ㄨ繘琛岀簿纭牸寮忎慨姝?
	fixedPath, fixErr = processor.ApplyCorrections(ctx, paper.FilePath, []map[string]interface{}{
		{"format_rules": rulesMap},
	})
	if fixErr != nil {
		return nil, fmt.Errorf("failed to fix document: %v", fixErr)
	}
	if fixedPath == "" {
		return nil, fmt.Errorf("failed to fix document: empty corrected file path")
	}

	// 5. 鏇存柊璁烘枃璁板綍
	paper.CorrectedFilePath = fixedPath
	paper.Status = "corrected"
	if err := database.DB.Save(paper).Error; err != nil {
		return nil, fmt.Errorf("failed to update paper record: %v", err)
	}

	return map[string]interface{}{
		"corrected_file_path": fixedPath,
		"download_url":        fmt.Sprintf("/api/v1/papers/%s/corrected-file", paper.ID.String()),
	}, nil
}

// UploadPaper 涓婁紶璁烘枃
func (s PaperService) UploadPaper(userID uuid.UUID, title, description string, formatStandardID uuid.UUID, file *multipart.FileHeader, fileType string, c *gin.Context) (*model.Paper, error) {
	// 澶勭悊鍖垮悕鐢ㄦ埛鐨勬儏鍐?
	// 淇濆瓨鏂囦欢鍒版湰鍦?
	fileName := fmt.Sprintf("%s.%s", uuid.New().String(), fileType)
	filePath := filepath.Join("uploads", "papers", fileName)

	// 纭繚鐩綍瀛樺湪
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	// 淇濆瓨鏂囦欢
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		return nil, fmt.Errorf("failed to save file: %w", err)
	}

	var selectedTemplateID *uuid.UUID
	if formatStandardID != uuid.Nil {
		selectedTemplateID = &formatStandardID
	}

	// 鍒涘缓璁烘枃璁板綍
	paper := &model.Paper{
		ID:                    uuid.New(),
		UserID:                userID,
		Title:                 title,
		Description:           description,
		FilePath:              filePath,
		FileName:              file.Filename,
		FileSize:              file.Size,
		FileType:              fileType,
		SelectedTemplateID:    selectedTemplateID,
		Status:                "uploaded",
		ParsedInfo:            "{}", // 鍒濆鍖栦负绌篔SON瀵硅薄
		AutoDetectedTemplates: "[]", // 鍒濆鍖栦负绌篔SON鏁扮粍
	}

	// 淇濆瓨鍒版暟鎹簱
	if err := database.DB.Create(paper).Error; err != nil {
		return nil, fmt.Errorf("failed to save paper to database: %w", err)
	}

	// 濡傛灉鎻愪緵浜嗘牸寮忔爣鍑咺D锛屽垯绔嬪嵆搴旂敤鏍煎紡淇
	if formatStandardID != uuid.Nil {
		// 鑾峰彇鏍煎紡妯℃澘
		var template model.FormatTemplate
		if err := database.DB.Where("id = ?", formatStandardID).First(&template).Error; err != nil {
		} else {
			// 瑙ｆ瀽鏍煎紡瑙勫垯
			var rulesMap map[string]interface{}
			if err := json.Unmarshal([]byte(template.FormatRules), &rulesMap); err != nil {
				// 灏濊瘯鍏堣В鏋愪负瀛楃涓诧紙澶勭悊鍙岄噸搴忓垪鍖栫殑鎯呭喌锛?
				var jsonString string
				if err2 := json.Unmarshal([]byte(template.FormatRules), &jsonString); err2 == nil {
					if err3 := json.Unmarshal([]byte(jsonString), &rulesMap); err3 != nil {
					} else {
					}
				} else {
				}
			} else {

				// 濡傛灉鏄洿鎺ョ粨鏋勶紝浣跨敤 ParseRequirementsToStandard 鍑芥暟
				//standard := formatchecker.ParseRequirementsToStandard(rulesMap) // 浣跨敤鏍囧噯瑙ｆ瀽锛屼絾鐩存帴浣跨敤rulesMap
				processor := fileprocessor.NewEnhancedProcessor()

				ctx := context.Background()
				// 浣跨敤澧炲己澶勭悊鍣ㄧ洿鎺ュ簲鐢ㄦ牸寮忚鍒?
				correctedPath, err := processor.ApplyCorrections(ctx, filePath, []map[string]interface{}{
					{"format_rules": rulesMap},
				})
				if err != nil {
				} else if correctedPath != "" {
					// 鏇存柊璁烘枃璁板綍锛屼娇鐢ㄤ慨姝ｅ悗鐨勬枃浠?
					paper.FilePath = correctedPath
					paper.Status = "corrected"
					database.DB.Save(paper)
				}
			}
		}
	}

	return paper, nil
}

// GetPapersByUserID 鑾峰彇鐢ㄦ埛鐨勮鏂囧垪琛?
func (s PaperService) GetPapersByUserID(userID uuid.UUID, page, pageSize int) ([]model.Paper, int64, error) {
	var papers []model.Paper
	var total int64

	offset := (page - 1) * pageSize
	database.DB.Where("user_id = ?", userID).Count(&total)
	database.DB.Where("user_id = ?", userID).Offset(offset).Limit(pageSize).Find(&papers)

	return papers, total, nil
}

// GetPaperByID 鏍规嵁ID鑾峰彇璁烘枃
func (s PaperService) GetPaperByID(userID, paperID uuid.UUID) (*model.Paper, error) {
	var paper model.Paper
	query := database.DB.Where("id = ?", paperID)
	if userID != uuid.Nil {
		query = query.Where("user_id = ?", userID)
	}
	err := query.First(&paper).Error
	return &paper, err
}

// DeletePaper 鍒犻櫎璁烘枃
func (s PaperService) DeletePaper(userID, paperID uuid.UUID) error {
	query := database.DB.Where("id = ?", paperID)
	if userID != uuid.Nil {
		query = query.Where("user_id = ?", userID)
	}

	result := query.Delete(&model.Paper{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GetPaperCheckResults 鑾峰彇璁烘枃鐨勬鏌ョ粨鏋滃垪琛?
func (s PaperService) GetPaperCheckResults(userID, paperID uuid.UUID) ([]model.CheckResult, error) {
	var results []model.CheckResult
	err := database.DB.Where("paper_id = ? AND user_id = ?", paperID, userID).Find(&results).Error
	return results, err
}

// GetAllPapers 鑾峰彇鎵€鏈夎鏂囷紙绠＄悊鍛樼敤锛?
func (s PaperService) GetAllPapers(page, pageSize int) ([]model.Paper, int64, error) {
	var papers []model.Paper
	var total int64

	offset := (page - 1) * pageSize
	database.DB.Model(&model.Paper{}).Count(&total)
	database.DB.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&papers)

	return papers, total, nil
}

// ExportCorrectedPaper 瀵煎嚭淇鍚庣殑璁烘枃
func (s PaperService) ExportCorrectedPaper(userID, paperID uuid.UUID) (string, error) {
	paper, err := s.GetPaperByID(userID, paperID)
	if err != nil {
		return "", fmt.Errorf("failed to get paper: %v", err)
	}

	if paper.CorrectedFilePath != "" {
		if _, err := os.Stat(filepath.Clean(paper.CorrectedFilePath)); err == nil {
			return paper.CorrectedFilePath, nil
		}
	}

	ext := filepath.Ext(paper.FilePath)
	baseNoExt := strings.TrimSuffix(filepath.Base(paper.FilePath), ext)
	dir := filepath.Dir(paper.FilePath)

	standardPath := filepath.Join(dir, baseNoExt+"_corrected"+ext)
	if _, err := os.Stat(filepath.Clean(standardPath)); err == nil {
		paper.CorrectedFilePath = standardPath
		_ = database.DB.Save(paper).Error
		return standardPath, nil
	}

	pattern := filepath.Join(dir, baseNoExt+"_corrected_*"+ext)
	matches, _ := filepath.Glob(pattern)
	if len(matches) > 0 {
		latestPath := ""
		var latestTime time.Time
		for _, m := range matches {
			fi, err := os.Stat(filepath.Clean(m))
			if err != nil {
				continue
			}
			if latestPath == "" || fi.ModTime().After(latestTime) {
				latestPath = m
				latestTime = fi.ModTime()
			}
		}
		if latestPath != "" {
			paper.CorrectedFilePath = latestPath
			_ = database.DB.Save(paper).Error
			return latestPath, nil
		}
	}

	if paper.SelectedTemplateID != nil {
		var template model.FormatTemplate
		if err := database.DB.Where("id = ?", *paper.SelectedTemplateID).First(&template).Error; err == nil {
			var rulesMap map[string]interface{}
			if err := json.Unmarshal([]byte(template.FormatRules), &rulesMap); err != nil {
				var jsonString string
				if err2 := json.Unmarshal([]byte(template.FormatRules), &jsonString); err2 == nil {
					_ = json.Unmarshal([]byte(jsonString), &rulesMap)
				}
			}
			if rulesMap != nil {
				// 使用增强处理器进行格式修正
				log.Println("========================================")
				log.Println("🔧 使用增强处理器进行格式修正")
				log.Println("========================================")

				fp := fileprocessor.NewEnhancedProcessor()
				newFilePath, err := fp.ApplyCorrections(context.Background(), paper.FilePath, []map[string]interface{}{
					{"format_rules": rulesMap},
				})
				if err == nil && newFilePath != "" {
					paper.CorrectedFilePath = newFilePath
					paper.Status = "corrected"
					_ = database.DB.Save(paper).Error
					log.Printf("鉁?Python鏈嶅姟鏍煎紡淇鎴愬姛: %s", newFilePath)
					return newFilePath, nil
				} else {
					log.Printf("鉂?Python鏈嶅姟鏍煎紡淇澶辫触: %v", err)
					log.Println("鎻愮ず: 璇风‘淇漃ython鏈嶅姟宸插惎鍔?(python backend/python_service/src/server.py)")
					return "", fmt.Errorf("鏍煎紡淇澶辫触: %w", err)
				}
			}
		}
	}

	return "", fmt.Errorf("corrected file not found for paper %s", paperID)
}

// ComparePaperFormats 瀵规瘮璁烘枃鏍煎紡
func (s PaperService) ComparePaperFormats(userID, paperID, checkResultID uuid.UUID) (interface{}, error) {
	// TODO: 瀹炵幇鏍煎紡瀵规瘮閫昏緫
	return nil, nil
}

// ExportCheckReport 瀵煎嚭妫€鏌ユ姤鍛?
func (s PaperService) ExportCheckReport(userID, checkResultID uuid.UUID) (string, error) {
	// TODO: 瀹炵幇瀵煎嚭鎶ュ憡閫昏緫
	return "", nil
}
