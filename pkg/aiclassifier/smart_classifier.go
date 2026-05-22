package aiclassifier

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// scDbgLog 写NDJSON调试日志（仅Debug模式）
func scDbgLog(hypothesisID, location, message string, jsonData string) {
	f, err := os.OpenFile("debug-c190b3.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, `{"sessionId":"c190b3","hypothesisId":%q,"location":%q,"message":%q,"data":%s,"timestamp":%d}`+"\n",
		hypothesisID, location, message, jsonData, time.Now().UnixMilli())
}

const (
	HighConfidenceThreshold = 0.92 // 规则引擎高置信度阈值：直出
	ModelConfidenceThreshold = 0.80 // 本地模型中置信度阈值
	RetrainDefaultThreshold  = 200  // 默认重训练样本阈值

	PhaseColdStart   = "cold_start"   // 冷启动：依赖规则+AI
	PhaseApprentice  = "apprentice"   // 学徒期：本地模型+AI辅助
	PhaseIndependent = "independent"  // 独立期：主要依赖本地模型
)

// SmartClassifier 智能分类器 — 三级路由
//
//	┌──────────────┐   ┌──────────────┐   ┌──────────────┐
//	│  规则引擎      │   │  本地模型      │   │  AI 仲裁      │
//	│  高置信→直出   │   │  中置信→接管   │   │  低置信→请教   │
//	└──────┬───────┘   └──────┬───────┘   └──────┬───────┘
//	       └──────────────────┼──────────────────┘
//	                          ↓
//	                    教学数据库 → 自动重训练
type SmartClassifier struct {
	ruleEngine         *RuleEngine
	localModel         *LocalClassifier
	aiArbitrator       *AIArbitrator
	fullDocClassifier  *FullDocumentClassifier // 阶段3：全文档语义分类器
	db                 *gorm.DB
	mu                 sync.RWMutex

	// 配置
	retrainThreshold int
	maxAICallsPerDoc int
	aiCallCount      int // 当前文档的 AI 调用次数

	// 状态
	phase            string
	samplesSinceLastTrain int
}

// NewSmartClassifier 创建智能分类器
func NewSmartClassifier(db *gorm.DB, cookie, bearer string, aiEnabled bool, retrainThreshold, maxAICallsPerDoc int) *SmartClassifier {
	if retrainThreshold <= 0 {
		retrainThreshold = RetrainDefaultThreshold
	}
	if maxAICallsPerDoc <= 0 {
		maxAICallsPerDoc = 20
	}

	arbitrator := NewAIArbitrator(cookie, bearer, aiEnabled)

	sc := &SmartClassifier{
		ruleEngine:       NewRuleEngine(),
		localModel:       NewLocalClassifier(),
		aiArbitrator:     arbitrator,
		db:               db,
		retrainThreshold: retrainThreshold,
		maxAICallsPerDoc: maxAICallsPerDoc,
		phase:            PhaseColdStart,
	}

	// 阶段3：若 AI 可用，同时初始化全文档分类器（使用相同的 DeepSeek 客户端）
	if arbitrator.client != nil {
		sc.fullDocClassifier = NewFullDocumentClassifier(arbitrator.client)
		log.Printf("[SmartClassifier] 全文档语义分类器已启用")
	}

	// 尝试从数据库加载已训练的模型
	sc.loadModelFromDB()

	return sc
}

// ClassifyDocument 对整个文档的段落进行分类
// 这是外部调用的主入口
func (sc *SmartClassifier) ClassifyDocument(features []ParagraphFeature, documentID string) []ClassifyResult {
	sc.mu.Lock()
	sc.aiCallCount = 0
	sc.mu.Unlock()

	results := make([]ClassifyResult, len(features))

	// ── Step 1: 规则引擎全量扫描 ──
	for i := range features {
		results[i] = sc.ruleEngine.Classify(&features[i])
	}
	logClassificationStats("规则引擎", results)

	// ── Step 2: 本地模型接管中等置信度的 ──
	sc.mu.RLock()
	modelReady := sc.localModel.IsReady()
	sc.mu.RUnlock()

	if modelReady {
		for i := range results {
			if results[i].Confidence < HighConfidenceThreshold {
				featureVec := features[i].ToFloat64Slice()
				modelResult := sc.localModel.Predict(featureVec)
				if modelResult.Label != "" && modelResult.Confidence >= ModelConfidenceThreshold {
					results[i] = modelResult
				}
			}
		}
		logClassificationStats("规则+本地模型", results)
	}

	// ── Step 3: AI 仲裁低置信度段落 ──
	// #region agent log H3
	{
		lowConf := 0
		for _, r := range results { if r.Confidence < 0.90 { lowConf++ } }
		scDbgLog("H3", "smart_classifier.go:ClassifyDocument",
			"before AI arbitration check",
			fmt.Sprintf(`{"runId":"post-fix","aiEnabled":%v,"shouldCallAI":%v,"totalParas":%d,"lowConfParas":%d}`,
				sc.aiArbitrator.IsEnabled(), sc.shouldCallAI(results), len(results), lowConf))
	}
	// #endregion agent log H3
	if sc.aiArbitrator.IsEnabled() && sc.shouldCallAI(results) {
		aiResults, err := sc.aiArbitrator.ClassifyBatch(features, results)
		if err != nil {
			log.Printf("[智能分类] AI 仲裁失败: %v, 使用规则+模型结果", err)
		} else {
			sc.mu.Lock()
			sc.aiCallCount++
			sc.mu.Unlock()
			results = aiResults
			logClassificationStats("规则+模型+AI", results)
		}
	}

	// ── Step 3.5: 全文档语义分类（替代局部AI仲裁，更准确） ──
	// 仅在文档段落数足够、且全文分类器可用时启动
	if sc.fullDocClassifier != nil && len(features) >= 20 {
		log.Printf("[全文分类] 启动全文语义分类（%d段）", len(features))
		fullResults, err := sc.fullDocClassifier.ClassifyAll(features, results)
		if err != nil {
			log.Printf("[全文分类] 失败，保留规则+AI结果: %v", err)
		} else {
			results = fullResults
		}
	}

	// ── Step 4: 状态机上下文修正（在全文分类之后再做一次兜底校正） ──
	results = sc.applyContextRules(features, results)

	// #region agent log H4
	{
		typeCounts := make(map[string]int)
		for _, r := range results { typeCounts[r.Label]++ }
		body := typeCounts["body"]; h1 := typeCounts["heading_1"]; h2 := typeCounts["heading_2"]
		abst := typeCounts["abstract"]; cover := typeCounts["cover"]; refs := typeCounts["references"]
		scDbgLog("H4", "smart_classifier.go:ClassifyDocument",
			"final classification distribution",
			fmt.Sprintf(`{"runId":"post-fix2","totalParas":%d,"body":%d,"heading_1":%d,"heading_2":%d,"abstract":%d,"cover":%d,"references":%d}`,
				len(results), body, h1, h2, abst, cover, refs))
	}
	// #endregion agent log H4

	// ── Step 5: 异步保存样本到教学数据库 ──
	go sc.saveSamples(features, results, documentID)

	return results
}

// shouldCallAI 判断是否需要调用 AI
func (sc *SmartClassifier) shouldCallAI(results []ClassifyResult) bool {
	sc.mu.RLock()
	calls := sc.aiCallCount
	phase := sc.phase
	sc.mu.RUnlock()

	if calls >= sc.maxAICallsPerDoc {
		return false
	}
	if phase == PhaseIndependent {
		// 独立阶段，只在置信度极低时才调用 AI
		lowCount := 0
		for _, r := range results {
			if r.Confidence < 0.6 {
				lowCount++
			}
		}
		return lowCount > 5
	}

	// 冷启动或学徒阶段，低置信度段落较多时调用
	lowCount := 0
	for _, r := range results {
		if r.Confidence < HighConfidenceThreshold {
			lowCount++
		}
	}
	return lowCount > 3
}

// applyContextRules 使用有序状态机对分类结果进行上下文修正
func (sc *SmartClassifier) applyContextRules(features []ParagraphFeature, results []ClassifyResult) []ClassifyResult {
	sm := NewThesisStateMachine()
	for i := range results {
		text := features[i].Text
		original := results[i].Label
		corrected := sm.Reclassify(original, text)
		if corrected != original {
			log.Printf("[状态机] para#%d %q: %s → %s", i, truncateText(text, 20), original, corrected)
			results[i].Label = corrected
			results[i].Source = "state_machine"
		}
	}
	return results
}

func truncateText(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n]) + "..."
	}
	return s
}

// saveSamples 保存分类样本到数据库
func (sc *SmartClassifier) saveSamples(features []ParagraphFeature, results []ClassifyResult, documentID string) {
	if sc.db == nil {
		return
	}
	if !sc.db.Migrator().HasTable(&model.ParagraphSample{}) {
		log.Printf("[智能分类] paragraph_samples 表不存在，跳过保存样本")
		return
	}

	var samples []model.ParagraphSample
	for i, f := range features {
		r := results[i]

		snippet := f.Text
		if len([]rune(snippet)) > 190 {
			snippet = string([]rune(snippet)[:190]) + "..."
		}

		weight := 1.0
		switch r.Source {
		case "ai":
			weight = 3.0
		case "rule":
			if r.Confidence >= HighConfidenceThreshold {
				weight = 2.0
			}
		}

		sample := model.ParagraphSample{
			TextLength:       f.TextLength,
			RuneLength:       f.RuneLength,
			FontSizePt:       f.FontSizePt,
			IsBold:           f.IsBold,
			Alignment:        f.Alignment,
			PositionRatio:    f.PositionRatio,
			HasChinese:       f.HasChinese,
			ChineseRatio:     f.ChineseRatio,
			HasNumberPrefix:  f.HasNumberPrefix,
			HasChapterMark:   f.HasChapterMark,
			HasAbstractKW:    f.HasAbstractKW,
			HasKeywordsKW:    f.HasKeywordsKW,
			HasReferencesKW:  f.HasReferencesKW,
			HasTOCIndicator:  f.HasTOCIndicator,
			HasCoverKeywords: f.HasCoverKeywords,
			HasOriginalityKW: f.HasOriginalityKW,
			FinalLabel:       r.Label,
			LabelSource:      r.Source,
			RuleLabel:        "",
			RuleConfidence:   0,
			AILabel:          "",
			TextSnippet:      snippet,
			DocumentID:       documentID,
			ParaIndex:        i,
			Weight:           weight,
		}

		switch r.Source {
		case "rule":
			sample.RuleLabel = r.Label
			sample.RuleConfidence = r.Confidence
		case "ai":
			sample.AILabel = r.Label
			sample.AIConfidence = r.Confidence
		case "local_model":
			sample.LocalModelLabel = r.Label
		}

		// 上下文
		if i > 0 {
			sample.PrevType = results[i-1].Label
		}
		if i < len(results)-1 {
			sample.NextType = results[i+1].Label
		}

		samples = append(samples, sample)
	}

	if len(samples) > 0 {
		if err := sc.db.CreateInBatches(samples, 100).Error; err != nil {
			log.Printf("[智能分类] 保存样本失败: %v", err)
		} else {
			log.Printf("[智能分类] 保存 %d 个样本到教学数据库", len(samples))
			sc.checkAndRetrain()
		}
	}
}

// checkAndRetrain 检查是否需要重训练
func (sc *SmartClassifier) checkAndRetrain() {
	if sc.db == nil {
		return
	}
	if !sc.db.Migrator().HasTable(&model.ParagraphSample{}) {
		return
	}

	var count int64
	sc.db.Model(&model.ParagraphSample{}).Count(&count)

	// 获取上次训练的样本数
	var state model.ClassifierModelState
	sc.db.Order("id DESC").First(&state)

	newSamples := int(count) - state.SampleCount
	if newSamples < sc.retrainThreshold {
		return
	}

	log.Printf("[智能分类] 新增 %d 个样本（阈值=%d），触发重训练...", newSamples, sc.retrainThreshold)
	go sc.retrain(int(count))
}

// retrain 重训练本地模型
func (sc *SmartClassifier) retrain(totalSamples int) {
	if sc.db == nil {
		return
	}
	if !sc.db.Migrator().HasTable(&model.ParagraphSample{}) {
		return
	}

	var dbSamples []model.ParagraphSample
	// 取最近的样本（限制数量避免内存溢出）
	sc.db.Order("created_at DESC").Limit(10000).Find(&dbSamples)

	if len(dbSamples) < 50 {
		log.Printf("[重训练] 样本不足 (%d)，跳过", len(dbSamples))
		return
	}

	trainData := make([]trainingSample, 0, len(dbSamples))
	for _, s := range dbSamples {
		label := s.FinalLabel
		if s.UserLabel != "" {
			label = s.UserLabel
		}
		if label == "" {
			continue
		}

		features := []float64{
			float64(s.RuneLength),
			s.FontSizePt,
			boolToFloat(s.IsBold),
			s.PositionRatio,
			s.ChineseRatio,
			boolToFloat(s.HasNumberPrefix),
			boolToFloat(s.HasChapterMark),
			boolToFloat(s.HasAbstractKW),
			boolToFloat(s.HasKeywordsKW),
			boolToFloat(s.HasReferencesKW),
			boolToFloat(s.HasTOCIndicator),
			boolToFloat(s.HasCoverKeywords),
			boolToFloat(s.HasOriginalityKW),
			0, // starts_with_digit_dot (not stored, but needed for consistency)
			0, // ends_with_period
			0, // has_tab
		}
		trainData = append(trainData, trainingSample{
			Features: features,
			Label:    label,
			Weight:   s.Weight,
		})
	}

	sc.mu.Lock()
	newModel := NewLocalClassifier()
	newModel.Train(trainData, 12)
	sc.localModel = newModel
	sc.mu.Unlock()

	// 保存模型状态到数据库
	modelJSON, _ := newModel.Serialize()
	phase := PhaseColdStart
	if totalSamples >= 500 {
		phase = PhaseApprentice
	}
	if totalSamples >= 2000 {
		phase = PhaseIndependent
	}

	state := model.ClassifierModelState{
		ModelVersion:  newModel.Version,
		SampleCount:   totalSamples,
		TrainedAt:     time.Now(),
		ModelDataJSON: modelJSON,
		Phase:         phase,
	}
	sc.db.Create(&state)

	sc.mu.Lock()
	sc.phase = phase
	sc.mu.Unlock()

	log.Printf("[重训练] 完成，阶段=%s, 样本数=%d, 模型版本=%d", phase, totalSamples, newModel.Version)
}

// loadModelFromDB 从数据库加载最新模型
func (sc *SmartClassifier) loadModelFromDB() {
	if sc.db == nil {
		return
	}

	// 确保表存在
	if !sc.db.Migrator().HasTable(&model.ClassifierModelState{}) {
		return
	}

	var state model.ClassifierModelState
	if err := sc.db.Order("id DESC").First(&state).Error; err != nil {
		log.Printf("[智能分类] 无已保存模型，使用冷启动模式")
		return
	}

	if state.ModelDataJSON != "" {
		if err := sc.localModel.Deserialize(state.ModelDataJSON); err != nil {
			log.Printf("[智能分类] 模型反序列化失败: %v", err)
			return
		}
		sc.phase = state.Phase
		log.Printf("[智能分类] 加载模型成功, 版本=%d, 阶段=%s, 样本数=%d",
			state.ModelVersion, state.Phase, state.SampleCount)
	}
}

// GetPhase 获取当前阶段
func (sc *SmartClassifier) GetPhase() string {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.phase
}

// GetDeepSeekClient 获取 DeepSeek Web 客户端（供格式验证器使用）
func (sc *SmartClassifier) GetDeepSeekClient() *DeepSeekWebClient {
	if sc.aiArbitrator != nil && sc.aiArbitrator.client != nil {
		return sc.aiArbitrator.client
	}
	return nil
}

// GetStats 获取分类器统计信息
func (sc *SmartClassifier) GetStats() map[string]interface{} {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	stats := map[string]interface{}{
		"phase":       sc.phase,
		"model_ready": sc.localModel.IsReady(),
		"ai_enabled":  sc.aiArbitrator.IsEnabled(),
	}
	if sc.localModel.IsReady() {
		stats["model_version"] = sc.localModel.Version
	}
	if sc.db != nil && sc.db.Migrator().HasTable(&model.ParagraphSample{}) {
		var count int64
		sc.db.Model(&model.ParagraphSample{}).Count(&count)
		stats["total_samples"] = count
	}
	return stats
}

func boolToFloat(v bool) float64 {
	if v {
		return 1.0
	}
	return 0.0
}

func logClassificationStats(stage string, results []ClassifyResult) {
	dist := make(map[string]int)
	lowConf := 0
	for _, r := range results {
		dist[r.Label]++
		if r.Confidence < HighConfidenceThreshold {
			lowConf++
		}
	}
	log.Printf("[智能分类][%s] 总计 %d 段, 低置信度 %d 段, 分布: %v", stage, len(results), lowConf, dist)
}
