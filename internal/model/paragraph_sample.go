package model

import (
	"time"

	"github.com/google/uuid"
)

// ParagraphSample 段落分类样本（教学数据库）
// 存储 AI 生成的标签、规则引擎标签、用户修正标签，用于训练本地决策树模型
type ParagraphSample struct {
	ID uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`

	// ── 段落特征（用于本地模型训练）──
	TextLength    int     `gorm:"not null" json:"text_length"`
	RuneLength    int     `gorm:"not null" json:"rune_length"`
	FontSizePt    float64 `json:"font_size_pt"`
	IsBold        bool    `json:"is_bold"`
	Alignment     string  `gorm:"size:20" json:"alignment"`
	PositionRatio float64 `json:"position_ratio"` // 段落在文档中的位置 0.0~1.0
	HasChinese    bool    `json:"has_chinese"`
	ChineseRatio  float64 `json:"chinese_ratio"`

	// 关键词命中特征
	HasNumberPrefix  bool `json:"has_number_prefix"`  // 以数字开头
	HasChapterMark   bool `json:"has_chapter_mark"`   // 含"第X章"等
	HasAbstractKW    bool `json:"has_abstract_kw"`    // 含"摘要"
	HasKeywordsKW    bool `json:"has_keywords_kw"`    // 含"关键词"
	HasReferencesKW  bool `json:"has_references_kw"`  // 含"参考文献"
	HasTOCIndicator  bool `json:"has_toc_indicator"`  // 含目录特征（tab+页码/引导符）
	HasCoverKeywords bool `json:"has_cover_keywords"` // 含封面关键词
	HasOriginalityKW bool `json:"has_originality_kw"` // 含原创性声明关键词

	// 上下文特征
	PrevType string `gorm:"size:50" json:"prev_type"` // 前一段的分类
	NextType string `gorm:"size:50" json:"next_type"` // 后一段的分类

	// ── 分类标签 ──
	RuleLabel       string  `gorm:"size:50" json:"rule_label"`       // 规则引擎给出的标签
	RuleConfidence  float64 `json:"rule_confidence"`                 // 规则引擎的置信度
	AILabel         string  `gorm:"size:50" json:"ai_label"`         // AI 仲裁给出的标签
	AIConfidence    float64 `json:"ai_confidence"`                   // AI 置信度
	UserLabel       string  `gorm:"size:50" json:"user_label"`       // 用户修正的标签（最高权重）
	FinalLabel      string  `gorm:"size:50;index" json:"final_label"` // 最终使用的标签
	LabelSource     string  `gorm:"size:20" json:"label_source"`     // "rule" / "ai" / "user" / "local_model"
	LocalModelLabel string  `gorm:"size:50" json:"local_model_label"` // 本地模型预测的标签

	// ── 元数据 ──
	TextSnippet string `gorm:"size:200" json:"text_snippet"` // 文本片段（用于人工审查）
	DocumentID  string `gorm:"size:100;index" json:"document_id"` // 关联的文档标识
	ParaIndex   int    `json:"para_index"`                   // 段落在文档中的序号

	// 训练权重：user > ai > rule
	Weight float64 `gorm:"default:1.0" json:"weight"`

	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// ClassifierModelState 分类器模型状态（记录本地模型的训练进度）
type ClassifierModelState struct {
	ID            uint      `gorm:"primary_key;autoIncrement" json:"id"`
	ModelVersion  int       `gorm:"not null;default:0" json:"model_version"`
	SampleCount   int       `gorm:"not null;default:0" json:"sample_count"`
	Accuracy      float64   `json:"accuracy"`
	TrainedAt     time.Time `json:"trained_at"`
	ModelDataJSON string    `gorm:"type:text" json:"model_data_json"` // 序列化的决策树 JSON
	Phase         string    `gorm:"size:20;default:'cold_start'" json:"phase"` // cold_start / apprentice / independent
	AICallCount   int       `gorm:"default:0" json:"ai_call_count"`
	CreatedAt     time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt     time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}
