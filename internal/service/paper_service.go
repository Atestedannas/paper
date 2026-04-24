package service

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/pkg/aiclassifier"
	"github.com/paper-format-checker/backend/pkg/docconvert"
	"github.com/paper-format-checker/backend/pkg/fileprocessor"
	"github.com/paper-format-checker/backend/pkg/formatchecker"
	"github.com/paper-format-checker/backend/pkg/templatefiller"
	"gorm.io/gorm"
)

// PaperService 璁烘枃鏈嶅姟
type PaperService struct {
	config *config.Config
}

const maxFormatRulesDebugBytes = 16384

var ErrLegacyWritePathDisabled = fmt.Errorf("legacy paper write path disabled")

// logFormatRulesDebug 在环境变量 PAPER_DEBUG_FORMAT_RULES=1 或 true 时，把模板 format_rules（JSON）打到日志。
// 用于 POST /upload 异步链路：检查 CheckPaperFormat、修正 FixPaperFormat、快速 QuickV2Fix 对照规则与引擎行为。
func logFormatRulesDebug(phase string, paperID, templateID uuid.UUID, rulesMap map[string]interface{}) {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PAPER_DEBUG_FORMAT_RULES")))
	if v != "1" && v != "true" && v != "yes" {
		return
	}
	if rulesMap == nil {
		log.Printf("[format_rules调试] phase=%s paper=%s template=%s rules=<nil>", phase, paperID, templateID)
		return
	}
	b, err := json.MarshalIndent(rulesMap, "", "  ")
	if err != nil {
		log.Printf("[format_rules调试] phase=%s paper=%s template=%s marshal错误: %v", phase, paperID, templateID, err)
		return
	}
	out := string(b)
	if len(out) > maxFormatRulesDebugBytes {
		out = out[:maxFormatRulesDebugBytes] + fmt.Sprintf("\n... [PAPER_DEBUG_FORMAT_RULES 截断, 总长 %d 字节]", len(b))
	}
	log.Printf("[format_rules调试] phase=%s paper=%s template=%s 顶层键=%v\n%s",
		phase, paperID, templateID, keysOfRulesMapForDebug(rulesMap), out)
}

func keysOfRulesMapForDebug(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// FixPaperFormatOptions 控制按问题粒度修复
type FixPaperFormatOptions struct {
	FixAll   bool
	IssueIDs []string
}

const maxIssueIDsPerFixRequest = 500

// NewPaperService 鍒涘缓璁烘枃鏈嶅姟
func NewPaperService(config *config.Config) PaperService {
	return PaperService{
		config: config,
	}
}

// createSmartProcessor 创建带智能分类器的增强处理器
func (s PaperService) createSmartProcessor() *fileprocessor.EnhancedProcessor {
	processor := fileprocessor.NewEnhancedProcessor()

	if s.config != nil && s.config.DeepSeek.Cookie != "" {
		sc := aiclassifier.NewSmartClassifier(
			database.DB,
			s.config.DeepSeek.Cookie,
			s.config.DeepSeek.Bearer,
			s.config.DeepSeek.Enabled,
			s.config.DeepSeek.RetrainThreshold,
			s.config.DeepSeek.MaxCallsPerDocument,
		)
		processor.SetSmartClassifier(sc)
		log.Printf("[PaperService] 智能分类器已注入，阶段=%s", sc.GetPhase())
	} else if database.DB != nil {
		sc := aiclassifier.NewSmartClassifier(database.DB, "", "", false, 200, 20)
		processor.SetSmartClassifier(sc)
		log.Println("[PaperService] 智能分类器已注入（无AI，仅规则+本地模型）")
	}

	return processor
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

	logFormatRulesDebug("CheckPaperFormat", paperID, templateID, rulesMap)

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

// QuickV2Fix 直接运行 V2 引擎修正格式（跳过 CheckPaperFormat，~200ms）
func (s PaperService) QuickV2Fix(paperFilePath string, universityID int64) (string, error) {
	return "", ErrLegacyWritePathDisabled

	start := time.Now()

	var template model.FormatTemplate
	if err := database.DB.Preload("University").Where("university_id = ? AND is_active = ?", universityID, true).
		First(&template).Error; err != nil {
		return "", fmt.Errorf("no active template for university %d: %v", universityID, err)
	}

	// QuickV2Fix 只把 school_id 等传给 ApplyCorrectionsV2，不传入下方 JSON；调试时仍可打印库里的 format_rules 便于对照
	var quickV2DbgRules map[string]interface{}
	if err := json.Unmarshal([]byte(template.FormatRules), &quickV2DbgRules); err != nil {
		var js string
		if err2 := json.Unmarshal([]byte(template.FormatRules), &js); err2 == nil {
			_ = json.Unmarshal([]byte(js), &quickV2DbgRules)
		}
	}
	logFormatRulesDebug("QuickV2Fix_upload_async", uuid.Nil, template.ID, quickV2DbgRules)

	processor := s.createSmartProcessor()
	if template.GoldenTemplatePath != "" {
		processor.SetTemplatePath(template.GoldenTemplatePath)
	}

	var corrections []map[string]interface{}
	if template.ID != uuid.Nil && template.University != nil {
		if sid := fileprocessor.SchoolIDFromUniversityName(template.University.Name, template.University.Abbr); sid != "" {
			corrections = []map[string]interface{}{{"school_id": sid}}
			log.Printf("[QuickV2Fix] school_id=%s (StyleFormatter school spec)", sid)
		}
	}

	ctx := context.Background()
	result, err := processor.ApplyCorrectionsV2(ctx, paperFilePath, corrections)
	log.Printf("[QuickV2Fix] 完成: result=%q, err=%v, 耗时=%v", result, err, time.Since(start))
	return result, err
}

// FixPaperFormatByParsedRequirements 鏍规嵁瑙ｆ瀽鐨勮姹備慨澶嶈鏂囨牸寮?
func (s PaperService) FixPaperFormatByParsedRequirements(userID, paperID uuid.UUID, requirements map[string]interface{}) (interface{}, error) {
	return nil, ErrLegacyWritePathDisabled

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
	return nil, ErrLegacyWritePathDisabled
}

// FixPaperFormatWithOptions 按 Issue 粒度修复论文格式
func (s PaperService) FixPaperFormatWithOptions(userID, paperID, checkResultID uuid.UUID, options FixPaperFormatOptions) (interface{}, error) {
	return nil, ErrLegacyWritePathDisabled

	// 校验 FixAll 与 IssueIDs 是否同时指定等非法组合
	if err := validateFixPaperFormatOptions(options); err != nil {
		return nil, err
	}

	// 按用户 ID 与论文 ID 加载论文记录（含 FilePath 等）
	paper, err := s.GetPaperByID(userID, paperID)
	if err != nil {
		return nil, fmt.Errorf("failed to get paper: %v", err)
	}

	// 校验该检查记录属于当前用户与当前论文
	checkResult, err := s.GetCheckResultForPaperUser(userID, paperID, checkResultID)
	if err != nil {
		return nil, err
	}
	// 仅已完成检查的结果才能驱动修正（Issues 已落库）
	if checkResult.Status != "completed" {
		return nil, fmt.Errorf("check result status is not completed")
	}

	// 从检查结果解析/生成可修复的 issue id 列表
	allIssueIDs, err := s.ensureFormatCorrectionsFromCheckResult(checkResult)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare corrections: %v", err)
	}
	// 没有任何可映射到修正逻辑的 issue 则无法继续
	if len(allIssueIDs) == 0 {
		return nil, fmt.Errorf("no fixable issues found for check result %s", checkResultID.String())
	}

	// 根据 options.FixAll / options.IssueIDs 得到实际要应用的 id 子集
	selectedIssueIDs, err := resolveSelectedIssueIDs(allIssueIDs, options)
	if err != nil {
		return nil, err
	}
	// 过滤后为空说明所选 id 均不在检查结果中
	if len(selectedIssueIDs) == 0 {
		return nil, fmt.Errorf("no issue selected to apply")
	}

	// 将本次选中的 issue 标记为已应用（便于幂等与审计）
	if err := s.markAppliedCorrections(checkResult.ID, selectedIssueIDs); err != nil {
		return nil, fmt.Errorf("failed to update correction selection: %v", err)
	}

	// 加载格式模板；Preload University 供高校名解析 school_id（样式规范目录）
	var template model.FormatTemplate
	if err := database.DB.Preload("University").Where("id = ?", checkResult.TemplateID).First(&template).Error; err != nil {
		return nil, fmt.Errorf("failed to get format template: %v", err)
	}

	// 将模板表中的 FormatRules（JSON 字符串）反序列化为 map，供 V2 引擎消费
	var rulesMap map[string]interface{}
	if err := json.Unmarshal([]byte(template.FormatRules), &rulesMap); err != nil {
		// 兼容历史「双重 JSON 字符串」存储：先解成 string 再解成对象
		var jsonString string
		if err2 := json.Unmarshal([]byte(template.FormatRules), &jsonString); err2 == nil {
			if err3 := json.Unmarshal([]byte(jsonString), &rulesMap); err3 != nil {
				return nil, fmt.Errorf("failed to unmarshal format rules (double encoded): %v", err3)
			}
		} else {
			return nil, fmt.Errorf("failed to unmarshal format rules: %v", err)
		}
	}

	// 环境变量 PAPER_DEBUG_FORMAT_RULES=1 时打印完整规则 JSON（截断）
	logFormatRulesDebug("FixPaperFormat", paper.ID, template.ID, rulesMap)

	// 修正过程无 HTTP 上下文，使用 Background
	ctx := context.Background()
	var fixedPath string // V2 返回的修正后 docx 路径
	var fixErr error     // ApplyCorrectionsV2 的错误

	// 记录模板 ID 与金模板路径（有金模板时引擎可走「对照范例」分支）
	log.Printf("[FixFormat] templateID=%s GoldenTemplatePath=%q", template.ID, template.GoldenTemplatePath)
	// 创建带 V2/分类器等能力的处理器实例
	processor := s.createSmartProcessor()
	if template.GoldenTemplatePath != "" {
		processor.SetTemplatePath(template.GoldenTemplatePath) // 设置金模板磁盘路径
	} else {
		log.Printf("[FixFormat] ⚠️  GoldenTemplatePath为空，将使用JSON规则方案")
	}
	// 传入 V2 的修正参数：规则全文 + 选中 issue + 是否全量修复语义
	correctionMap := map[string]interface{}{
		"format_rules":       rulesMap,                                     // 与检查阶段 ParseRequirementsToStandard 同源
		"selected_issue_ids": selectedIssueIDs,                             // 只处理这些 issue（与 fix_all 配合）
		"fix_all":            options.FixAll || len(options.IssueIDs) == 0, // FixAll 或未显式传 IssueIDs 时视为全量
	}
	if template.ID != uuid.Nil {
		if u := template.University; u != nil {
			// 由高校中文名/简称映射到 pkg 内学校标识，加载 *.spec.json 等
			if sid := fileprocessor.SchoolIDFromUniversityName(u.Name, u.Abbr); sid != "" {
				correctionMap["school_id"] = sid
				log.Printf("[FixFormat] school_id=%s (for StyleFormatter school spec)", sid)
			}
		}
	}
	// 对原稿路径执行 V2 修正，生成新文件路径
	fixedPath, fixErr = processor.ApplyCorrectionsV2(ctx, paper.FilePath, []map[string]interface{}{
		correctionMap,
	})
	if fixErr != nil {
		return nil, fmt.Errorf("failed to fix document: %v", fixErr)
	}
	if fixedPath == "" {
		return nil, fmt.Errorf("failed to fix document: empty corrected file path")
	}
	paper.CorrectedFilePath = fixedPath // 回写论文表：修正稿路径
	paper.Status = "corrected"          // 状态标记为已修正
	if err := database.DB.Save(paper).Error; err != nil {
		return nil, fmt.Errorf("failed to update paper record: %v", err)
	}

	// 若处理器生成了差异报告，序列化后挂到本次 CheckResult 上
	if report := processor.GetLastDiffReport(); report != nil {
		if b, err2 := json.Marshal(report); err2 == nil {
			database.DB.Model(&model.CheckResult{}).
				Where("id = ?", checkResult.ID).
				Update("diff_report", string(b))
			log.Printf("[差异报告] 已保存到数据库，错误: %d 警告: %d",
				report.ErrorCount, report.WarningCount)
		}
	}

	// 返回给 API 的摘要：路径、下载 URL、实际应用的 issue 列表与数量
	return map[string]interface{}{
		"corrected_file_path": fixedPath,
		"download_url":        fmt.Sprintf("/api/v1/papers/%s/corrected-file", paper.ID.String()),
		"applied_issue_ids":   selectedIssueIDs,
		"applied_issue_count": len(selectedIssueIDs),
		"fix_all":             options.FixAll || len(options.IssueIDs) == 0,
	}, nil
}

// tryTemplateFill uses the Go OOXML template filling engine for high-accuracy correction.
// Falls back to the legacy Python script if the Go engine fails.
func (s PaperService) tryTemplateFill(ctx context.Context, inputPath, goldenTemplatePath string, rulesMap map[string]interface{}) (string, error) {
	tf := templatefiller.NewTemplateFiller()

	// Inject DeepSeek client for precise refinement
	if s.config != nil && s.config.DeepSeek.Enabled && s.config.DeepSeek.Cookie != "" {
		dsClient := aiclassifier.NewDeepSeekWebClient(s.config.DeepSeek.Cookie, s.config.DeepSeek.Bearer)
		tf.DeepSeekClient = dsClient
		log.Printf("[FixFormat] DeepSeek精确替换已启用")
	}

	// Prefer the prepared real-template-based golden template for maximum accuracy
	preparedPath, prepErr := tf.EnsureGoldenTemplate("cqrwst")
	if prepErr == nil && preparedPath != "" {
		log.Printf("[FixFormat] using prepared golden template: %s", preparedPath)
		goldenTemplatePath = preparedPath
	} else if _, err := os.Stat(goldenTemplatePath); os.IsNotExist(err) {
		log.Printf("[FixFormat] golden template not found at %s and no prepared template: %v", goldenTemplatePath, prepErr)
		return "", fmt.Errorf("golden template not found: %s", goldenTemplatePath)
	}

	classification, err := s.classifyForTemplateFill(inputPath)
	if err != nil {
		return "", fmt.Errorf("classification failed: %w", err)
	}

	outputDir := filepath.Dir(inputPath)
	return tf.Fill(ctx, inputPath, goldenTemplatePath, classification, outputDir)
}

// classifyForTemplateFill runs the SmartClassifier and produces a classification
// result suitable for the OOXML template filler.
//
// Improvements over the legacy version:
//   - Uses paragraph index (position) for matching, not text equality
//   - Validates structural ordering (abstract before body, body before references)
//   - Includes all paragraphs (even empty) to preserve accurate indices
func (s PaperService) classifyForTemplateFill(docPath string) (templatefiller.ClassificationResult, error) {
	result := templatefiller.ClassificationResult{}

	processor := s.createSmartProcessor()
	doc, err := fileprocessor.OpenDocument(docPath)
	if err != nil {
		return result, fmt.Errorf("failed to open document: %w", err)
	}

	paras := doc.Paragraphs()
	classified := processor.ClassifyParagraphsExport(paras)

	// Build an index-based lookup: paragraph index -> label.
	// ClassifyParagraphsExport returns map[label][]Paragraph — we need to
	// invert this to map[paragraphPointer]label for O(1) lookup.
	type paraKey struct {
		text  string
		index int
	}
	paraLabels := make(map[int]string)

	// First pass: build a text→indices map for the source paragraphs
	paraTexts := make([]string, len(paras))
	for i, para := range paras {
		paraTexts[i] = processor.ExtractParagraphTextExport(para)
	}

	// For each classified label, find the matching paragraph indices
	for label, labelParas := range classified {
		for _, lp := range labelParas {
			lpText := processor.ExtractParagraphTextExport(lp)
			// Match by finding the paragraph in the original list at the same position
			for i, para := range paras {
				if &para == &lp {
					paraLabels[i] = label
					break
				}
			}
			// Fallback: match by text + position (first unmatched occurrence)
			if _, found := findByLabel(paraLabels, label); !found {
				for i, t := range paraTexts {
					if t == lpText && paraLabels[i] == "" {
						paraLabels[i] = label
						break
					}
				}
			}
		}
	}

	// Build result with all paragraphs, preserving indices
	for i, para := range paras {
		text := processor.ExtractParagraphTextExport(para)
		if strings.TrimSpace(text) == "" {
			continue
		}

		paraType := paraLabels[i]
		if paraType == "" {
			paraType = "body"
		}

		result.Paragraphs = append(result.Paragraphs, templatefiller.ClassificationParagraph{
			Index: i,
			Type:  paraType,
			Text:  text,
		})
	}

	// Structural validation: ensure correct ordering
	result = validateClassificationOrder(result)

	log.Printf("[ClassifyForTemplateFill] classified %d paragraphs from %s", len(result.Paragraphs), docPath)
	return result, nil
}

// findByLabel checks if any entry in the map has the given label value.
func findByLabel(m map[int]string, label string) (int, bool) {
	for k, v := range m {
		if v == label {
			return k, true
		}
	}
	return 0, false
}

// validateClassificationOrder ensures structural ordering is correct.
// For example, abstract_title must come before body, references_title must come after body.
func validateClassificationOrder(cls templatefiller.ClassificationResult) templatefiller.ClassificationResult {
	type sectionBound struct {
		firstIdx int
		lastIdx  int
	}
	sections := make(map[string]*sectionBound)

	for i, p := range cls.Paragraphs {
		sType := mapToSectionGroup(p.Type)
		if b, ok := sections[sType]; ok {
			if i < b.firstIdx {
				b.firstIdx = i
			}
			if i > b.lastIdx {
				b.lastIdx = i
			}
		} else {
			sections[sType] = &sectionBound{firstIdx: i, lastIdx: i}
		}
	}

	// Expected order: cover < abstract < body < references < acknowledgements
	expectedOrder := []string{"cover", "abstract", "body", "references", "acknowledgements"}
	prevEnd := -1
	for _, section := range expectedOrder {
		b, ok := sections[section]
		if !ok {
			continue
		}
		if b.firstIdx < prevEnd {
			log.Printf("[ClassifyValidation] warning: section '%s' starts at %d but previous section ended at %d — possible misclassification", section, b.firstIdx, prevEnd)
		}
		prevEnd = b.lastIdx
	}

	return cls
}

// mapToSectionGroup maps paragraph types to high-level section groups for validation.
func mapToSectionGroup(paraType string) string {
	switch paraType {
	case "cover", "cover_title", "cover_subtitle", "cover_college", "cover_major",
		"cover_grade", "cover_student_name", "cover_student_id", "cover_advisor", "cover_date":
		return "cover"
	case "abstract_title", "abstract", "keywords", "en_abstract_title", "en_abstract", "en_keywords":
		return "abstract"
	case "heading_1", "heading_2", "heading_3", "body":
		return "body"
	case "references_title", "references":
		return "references"
	case "acknowledgements_title", "acknowledgements":
		return "acknowledgements"
	default:
		return "other"
	}
}

func validateFixPaperFormatOptions(options FixPaperFormatOptions) error {
	if options.FixAll && len(options.IssueIDs) > 0 {
		return fmt.Errorf("fix_all and issue_ids cannot be provided at the same time")
	}
	if !options.FixAll && len(options.IssueIDs) == 0 {
		return fmt.Errorf("issue_ids is required when fix_all is false")
	}
	if len(options.IssueIDs) > maxIssueIDsPerFixRequest {
		return fmt.Errorf("too many issue_ids, max %d", maxIssueIDsPerFixRequest)
	}
	return nil
}

// GetCheckResultForPaperUser 验证检查结果属于指定论文与用户
func (s PaperService) GetCheckResultForPaperUser(userID, paperID, checkResultID uuid.UUID) (*model.CheckResult, error) {
	query := database.DB.Where("id = ? AND paper_id = ?", checkResultID, paperID)
	if userID != uuid.Nil {
		query = query.Where("user_id = ?", userID)
	}

	var checkResult model.CheckResult
	if err := query.First(&checkResult).Error; err != nil {
		return nil, fmt.Errorf("failed to get check result: %w", err)
	}
	return &checkResult, nil
}

func (s PaperService) ensureFormatCorrectionsFromCheckResult(checkResult *model.CheckResult) ([]string, error) {
	var existing []model.FormatCorrection
	if err := database.DB.Where("check_result_id = ?", checkResult.ID).Order("created_at ASC").Find(&existing).Error; err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		ids := make([]string, 0, len(existing))
		seen := make(map[string]struct{}, len(existing))
		for _, correction := range existing {
			issueID := strings.TrimSpace(correction.IssueID)
			if issueID == "" {
				continue
			}
			if _, ok := seen[issueID]; ok {
				continue
			}
			seen[issueID] = struct{}{}
			ids = append(ids, issueID)
		}
		return ids, nil
	}

	issues, err := parseIssuesFromRaw(checkResult.Issues)
	if err != nil {
		return nil, err
	}
	if len(issues) == 0 {
		return []string{}, nil
	}

	tx := database.DB.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}

	createdIssueIDs := make([]string, 0, len(issues))
	for _, issue := range issues {
		issueID := strings.TrimSpace(issue.ID)
		if issueID == "" {
			issueID = uuid.NewString()
		}

		originalJSON, _ := json.Marshal(issue.Original)
		correctedJSON, _ := json.Marshal(issue.Suggestion)
		locationJSON, _ := json.Marshal(map[string]interface{}{
			"page":     issue.Page,
			"position": issue.Position,
		})

		correction := &model.FormatCorrection{
			ID:               uuid.New(),
			CheckResultID:    checkResult.ID,
			IssueID:          issueID,
			CorrectionType:   string(issue.Type),
			OriginalContent:  string(originalJSON),
			CorrectedContent: string(correctedJSON),
			Location:         string(locationJSON),
			IsApplied:        false,
			Confidence:       1.0,
			Description:      issue.Description,
		}
		if err := tx.Create(correction).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
		createdIssueIDs = append(createdIssueIDs, issueID)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	return createdIssueIDs, nil
}

func parseIssuesFromRaw(raw string) ([]formatchecker.FormatIssue, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return []formatchecker.FormatIssue{}, nil
	}

	var issues []formatchecker.FormatIssue
	if err := json.Unmarshal([]byte(raw), &issues); err == nil {
		return issues, nil
	}

	var encoded string
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		return nil, fmt.Errorf("failed to parse check issues: %w", err)
	}
	if err := json.Unmarshal([]byte(encoded), &issues); err != nil {
		return nil, fmt.Errorf("failed to parse double-encoded check issues: %w", err)
	}
	return issues, nil
}

func resolveSelectedIssueIDs(allIssueIDs []string, options FixPaperFormatOptions) ([]string, error) {
	if options.FixAll || len(options.IssueIDs) == 0 {
		return allIssueIDs, nil
	}

	allSet := make(map[string]struct{}, len(allIssueIDs))
	for _, issueID := range allIssueIDs {
		allSet[issueID] = struct{}{}
	}

	selected := make([]string, 0, len(options.IssueIDs))
	seen := make(map[string]struct{}, len(options.IssueIDs))
	missing := make([]string, 0)
	for _, raw := range options.IssueIDs {
		issueID := strings.TrimSpace(raw)
		if issueID == "" {
			continue
		}
		if _, ok := seen[issueID]; ok {
			continue
		}
		seen[issueID] = struct{}{}
		if _, ok := allSet[issueID]; !ok {
			missing = append(missing, issueID)
			continue
		}
		selected = append(selected, issueID)
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("some issue_ids are not found in check result: %s", strings.Join(missing, ", "))
	}

	return selected, nil
}

func (s PaperService) markAppliedCorrections(checkResultID uuid.UUID, selectedIssueIDs []string) error {
	tx := database.DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	if err := tx.Model(&model.FormatCorrection{}).
		Where("check_result_id = ?", checkResultID).
		Update("is_applied", false).Error; err != nil {
		tx.Rollback()
		return err
	}

	if len(selectedIssueIDs) > 0 {
		if err := tx.Model(&model.FormatCorrection{}).
			Where("check_result_id = ? AND issue_id IN ?", checkResultID, selectedIssueIDs).
			Update("is_applied", true).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit().Error
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

	// .doc → .docx (LibreOffice soffice, or Microsoft Word COM on Windows)
	if fileType == "doc" {
		docxPath, err := docconvert.ConvertDocToDocx(filePath, true)
		if err != nil {
			os.Remove(filePath)
			return nil, fmt.Errorf("failed to convert .doc to .docx: %w", err)
		}
		filePath = docxPath
		fileType = "docx"
		log.Printf("[UploadPaper] Converted .doc to .docx: %s", docxPath)
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

	// Format correction is handled asynchronously by the HTTP handler goroutine.

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

	// 优先检查 V2 corrected 文件
	v2Path := filepath.Join(dir, "corrected", baseNoExt+"_v2_corrected"+ext)
	if _, err := os.Stat(filepath.Clean(v2Path)); err == nil {
		paper.CorrectedFilePath = v2Path
		_ = database.DB.Save(paper).Error
		return v2Path, nil
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
		if err := database.DB.Preload("University").Where("id = ?", *paper.SelectedTemplateID).First(&template).Error; err == nil {
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

				fp := s.createSmartProcessor()
				if template.GoldenTemplatePath != "" {
					fp.SetTemplatePath(template.GoldenTemplatePath)
				}
				retryCorrMap := map[string]interface{}{"format_rules": rulesMap}
				if template.ID != uuid.Nil {
					if u := template.University; u != nil {
						if sid := fileprocessor.SchoolIDFromUniversityName(u.Name, u.Abbr); sid != "" {
							retryCorrMap["school_id"] = sid
						}
					}
				}
				newFilePath, err := fp.ApplyCorrectionsV2(context.Background(), paper.FilePath, []map[string]interface{}{
					retryCorrMap,
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
	reportData, err := s.buildCheckReportData(userID, checkResultID)
	if err != nil {
		return "", err
	}
	return buildTextCheckReport(reportData, "report"), nil
}

// ResolveLatestCheckResultID 获取论文最新检查结果ID
func (s PaperService) ResolveLatestCheckResultID(userID, paperID uuid.UUID) (uuid.UUID, error) {
	query := database.DB.Model(&model.CheckResult{}).Where("paper_id = ? AND status = ?", paperID, "completed")
	if userID != uuid.Nil {
		query = query.Where("user_id = ?", userID)
	}
	var checkResult model.CheckResult
	if err := query.Order("created_at DESC").First(&checkResult).Error; err != nil {
		return uuid.Nil, fmt.Errorf("failed to get latest check result: %w", err)
	}
	return checkResult.ID, nil
}

// ExportCheckReportHTML 导出HTML格式检查报告
func (s PaperService) ExportCheckReportHTML(userID, checkResultID uuid.UUID, reportType string) (string, error) {
	reportType = normalizeReportType(reportType)
	reportData, err := s.buildCheckReportData(userID, checkResultID)
	if err != nil {
		return "", err
	}
	return buildHTMLCheckReport(reportData, reportType), nil
}

// ExportCheckReportJSON 导出JSON格式检查报告
func (s PaperService) ExportCheckReportJSON(userID, checkResultID uuid.UUID, reportType string) (string, error) {
	reportType = normalizeReportType(reportType)
	reportData, err := s.buildCheckReportData(userID, checkResultID)
	if err != nil {
		return "", err
	}

	payload := map[string]interface{}{
		"paper":               reportData.Paper,
		"check_result":        reportData.CheckResult,
		"template":            reportData.Template,
		"issues":              reportData.Issues,
		"differences":         reportData.Differences,
		"corrections":         reportData.Corrections,
		"report_type":         reportType,
		"generated_at":        time.Now(),
		"applied_issue_count": len(reportData.AppliedIssueIDs),
		"applied_issue_ids":   reportData.AppliedIssueIDs,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal report json: %w", err)
	}
	return string(b), nil
}

type checkReportData struct {
	Paper           model.Paper
	CheckResult     model.CheckResult
	Template        model.FormatTemplate
	Issues          []formatchecker.FormatIssue
	Differences     []FormatDifference
	Corrections     []model.FormatCorrection
	AppliedIssueIDs []string
}

func (s PaperService) buildCheckReportData(userID, checkResultID uuid.UUID) (*checkReportData, error) {
	checkQuery := database.DB.Where("id = ?", checkResultID)
	if userID != uuid.Nil {
		checkQuery = checkQuery.Where("user_id = ?", userID)
	}

	var checkResult model.CheckResult
	if err := checkQuery.First(&checkResult).Error; err != nil {
		return nil, fmt.Errorf("failed to get check result: %w", err)
	}
	if checkResult.Status != "completed" {
		return nil, fmt.Errorf("check result status is not completed")
	}

	paper, err := s.GetPaperByID(userID, checkResult.PaperID)
	if err != nil {
		return nil, fmt.Errorf("failed to get paper: %w", err)
	}

	var template model.FormatTemplate
	if err := database.DB.Where("id = ?", checkResult.TemplateID).First(&template).Error; err != nil {
		return nil, fmt.Errorf("failed to get format template: %w", err)
	}

	issues, err := parseIssuesFromRaw(checkResult.Issues)
	if err != nil {
		return nil, err
	}

	differences, err := parseDifferencesFromRaw(checkResult.Differences)
	if err != nil {
		return nil, err
	}

	var corrections []model.FormatCorrection
	if err := database.DB.Where("check_result_id = ?", checkResult.ID).Order("created_at ASC").Find(&corrections).Error; err != nil {
		return nil, fmt.Errorf("failed to get corrections: %w", err)
	}

	appliedIssueIDs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, correction := range corrections {
		if !correction.IsApplied {
			continue
		}
		issueID := strings.TrimSpace(correction.IssueID)
		if issueID == "" {
			continue
		}
		if _, ok := seen[issueID]; ok {
			continue
		}
		seen[issueID] = struct{}{}
		appliedIssueIDs = append(appliedIssueIDs, issueID)
	}

	return &checkReportData{
		Paper:           *paper,
		CheckResult:     checkResult,
		Template:        template,
		Issues:          issues,
		Differences:     differences,
		Corrections:     corrections,
		AppliedIssueIDs: appliedIssueIDs,
	}, nil
}

func parseDifferencesFromRaw(raw string) ([]FormatDifference, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return []FormatDifference{}, nil
	}

	var differences []FormatDifference
	if err := json.Unmarshal([]byte(raw), &differences); err == nil {
		return differences, nil
	}

	var encoded string
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		return nil, fmt.Errorf("failed to parse check differences: %w", err)
	}
	if err := json.Unmarshal([]byte(encoded), &differences); err != nil {
		return nil, fmt.Errorf("failed to parse double-encoded check differences: %w", err)
	}
	return differences, nil
}

func buildTextCheckReport(data *checkReportData, reportType string) string {
	var b strings.Builder
	now := time.Now().Format("2006-01-02 15:04:05")

	b.WriteString("论文格式检查报告\n")
	b.WriteString("====================\n")
	b.WriteString(fmt.Sprintf("生成时间：%s\n", now))
	b.WriteString(fmt.Sprintf("论文标题：%s\n", fallback(data.Paper.Title, data.Paper.FileName)))
	b.WriteString(fmt.Sprintf("模板名称：%s\n", fallback(data.Template.Name, data.Template.TemplateID)))
	b.WriteString(fmt.Sprintf("问题总数：%d（error=%d, warning=%d, info=%d）\n", data.CheckResult.TotalIssues, data.CheckResult.ErrorCount, data.CheckResult.WarningCount, data.CheckResult.InfoCount))
	b.WriteString(fmt.Sprintf("可追踪差异：%d，修正建议：%d，已应用修正：%d\n\n", len(data.Differences), len(data.Corrections), len(data.AppliedIssueIDs)))

	if reportType != "correction-report" {
		b.WriteString("详细问题列表\n")
		b.WriteString("--------------------\n")
		if len(data.Issues) == 0 {
			b.WriteString("无详细问题数据\n\n")
		} else {
			for i, issue := range data.Issues {
				b.WriteString(fmt.Sprintf("%d) [%s/%s] %s\n", i+1, issue.Type, issue.Severity, issue.Description))
				b.WriteString(fmt.Sprintf("   issue_id=%s, page=%d, position=%d\n", issue.ID, issue.Page, issue.Position))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("修正建议\n")
	b.WriteString("--------------------\n")
	if len(data.Corrections) == 0 {
		b.WriteString("无修正建议数据\n")
	} else {
		for i, correction := range data.Corrections {
			status := "待应用"
			if correction.IsApplied {
				status = "已应用"
			}
			b.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, status, fallback(correction.Description, correction.CorrectionType)))
			b.WriteString(fmt.Sprintf("   issue_id=%s, confidence=%.2f\n", correction.IssueID, correction.Confidence))
		}
	}

	return b.String()
}

func buildHTMLCheckReport(data *checkReportData, reportType string) string {
	now := time.Now().Format("2006-01-02 15:04:05")
	var b strings.Builder

	b.WriteString("<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	b.WriteString("<title>论文格式检查报告</title>")
	b.WriteString("<style>body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'PingFang SC','Microsoft YaHei',sans-serif;max-width:960px;margin:24px auto;padding:0 16px;line-height:1.6;color:#1f2937}h1,h2{margin:12px 0}table{width:100%;border-collapse:collapse;margin:12px 0}th,td{border:1px solid #e5e7eb;padding:8px;text-align:left;vertical-align:top}th{background:#f9fafb}code{background:#f3f4f6;padding:1px 4px;border-radius:4px}</style>")
	b.WriteString("</head><body>")
	b.WriteString("<h1>论文格式检查报告</h1>")
	b.WriteString(fmt.Sprintf("<p>生成时间：%s</p>", html.EscapeString(now)))
	b.WriteString(fmt.Sprintf("<p>论文标题：%s</p>", html.EscapeString(fallback(data.Paper.Title, data.Paper.FileName))))
	b.WriteString(fmt.Sprintf("<p>模板名称：%s</p>", html.EscapeString(fallback(data.Template.Name, data.Template.TemplateID))))
	b.WriteString(fmt.Sprintf("<p>问题总数：%d（error=%d, warning=%d, info=%d）</p>", data.CheckResult.TotalIssues, data.CheckResult.ErrorCount, data.CheckResult.WarningCount, data.CheckResult.InfoCount))
	b.WriteString(fmt.Sprintf("<p>可追踪差异：%d，修正建议：%d，已应用修正：%d</p>", len(data.Differences), len(data.Corrections), len(data.AppliedIssueIDs)))

	if reportType != "correction-report" {
		b.WriteString("<h2>详细问题列表</h2><table><thead><tr><th>#</th><th>严重级别</th><th>类型</th><th>描述</th><th>位置</th><th>Issue ID</th></tr></thead><tbody>")
		if len(data.Issues) == 0 {
			b.WriteString("<tr><td colspan=\"6\">无详细问题数据</td></tr>")
		} else {
			for i, issue := range data.Issues {
				b.WriteString("<tr>")
				b.WriteString(fmt.Sprintf("<td>%d</td>", i+1))
				b.WriteString(fmt.Sprintf("<td>%s</td>", html.EscapeString(string(issue.Severity))))
				b.WriteString(fmt.Sprintf("<td>%s</td>", html.EscapeString(string(issue.Type))))
				b.WriteString(fmt.Sprintf("<td>%s</td>", html.EscapeString(issue.Description)))
				b.WriteString(fmt.Sprintf("<td>page=%d, pos=%d</td>", issue.Page, issue.Position))
				b.WriteString(fmt.Sprintf("<td><code>%s</code></td>", html.EscapeString(issue.ID)))
				b.WriteString("</tr>")
			}
		}
		b.WriteString("</tbody></table>")
	}

	b.WriteString("<h2>修正建议</h2><table><thead><tr><th>#</th><th>状态</th><th>描述</th><th>Issue ID</th><th>置信度</th></tr></thead><tbody>")
	if len(data.Corrections) == 0 {
		b.WriteString("<tr><td colspan=\"5\">无修正建议数据</td></tr>")
	} else {
		for i, correction := range data.Corrections {
			status := "待应用"
			if correction.IsApplied {
				status = "已应用"
			}
			b.WriteString("<tr>")
			b.WriteString(fmt.Sprintf("<td>%d</td>", i+1))
			b.WriteString(fmt.Sprintf("<td>%s</td>", html.EscapeString(status)))
			b.WriteString(fmt.Sprintf("<td>%s</td>", html.EscapeString(fallback(correction.Description, correction.CorrectionType))))
			b.WriteString(fmt.Sprintf("<td><code>%s</code></td>", html.EscapeString(correction.IssueID)))
			b.WriteString(fmt.Sprintf("<td>%.2f</td>", correction.Confidence))
			b.WriteString("</tr>")
		}
	}
	b.WriteString("</tbody></table>")
	b.WriteString("</body></html>")

	return b.String()
}

func fallback(primary, secondary string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return secondary
}

func normalizeReportType(reportType string) string {
	reportType = strings.TrimSpace(strings.ToLower(reportType))
	if reportType == "correction-report" {
		return "correction-report"
	}
	return "report"
}
