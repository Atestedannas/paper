package database

import (
	"log"

	"gorm.io/gorm"
)

// Migration20260315CreateAIClassifierTables 创建智能分类器相关表
// paragraph_samples 和 classifier_model_states
type Migration20260315CreateAIClassifierTables struct{}

func (m *Migration20260315CreateAIClassifierTables) Name() string {
	return "20260315_create_ai_classifier_tables"
}

func (m *Migration20260315CreateAIClassifierTables) Up(tx *gorm.DB) error {
	log.Println("创建智能分类器相关表（paragraph_samples / classifier_model_states）...")

	// ── paragraph_samples ──────────────────────────────────────────
	if err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS paragraph_samples (
			id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			text_length         INTEGER NOT NULL DEFAULT 0,
			rune_length         INTEGER NOT NULL DEFAULT 0,
			font_size_pt        DOUBLE PRECISION DEFAULT 0,
			is_bold             BOOLEAN DEFAULT FALSE,
			alignment           VARCHAR(20) DEFAULT '',
			position_ratio      DOUBLE PRECISION DEFAULT 0,
			has_chinese         BOOLEAN DEFAULT FALSE,
			chinese_ratio       DOUBLE PRECISION DEFAULT 0,
			has_number_prefix   BOOLEAN DEFAULT FALSE,
			has_chapter_mark    BOOLEAN DEFAULT FALSE,
			has_abstract_kw     BOOLEAN DEFAULT FALSE,
			has_keywords_kw     BOOLEAN DEFAULT FALSE,
			has_references_kw   BOOLEAN DEFAULT FALSE,
			has_toc_indicator   BOOLEAN DEFAULT FALSE,
			has_cover_keywords  BOOLEAN DEFAULT FALSE,
			prev_type           VARCHAR(50) DEFAULT '',
			next_type           VARCHAR(50) DEFAULT '',
			rule_label          VARCHAR(50) DEFAULT '',
			rule_confidence     DOUBLE PRECISION DEFAULT 0,
			ai_label            VARCHAR(50) DEFAULT '',
			ai_confidence       DOUBLE PRECISION DEFAULT 0,
			user_label          VARCHAR(50) DEFAULT '',
			final_label         VARCHAR(50) DEFAULT '',
			label_source        VARCHAR(20) DEFAULT '',
			local_model_label   VARCHAR(50) DEFAULT '',
			text_snippet        VARCHAR(200) DEFAULT '',
			document_id         VARCHAR(100) DEFAULT '',
			para_index          INTEGER DEFAULT 0,
			weight              DOUBLE PRECISION DEFAULT 1.0,
			created_at          TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at          TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)
	`).Error; err != nil {
		return err
	}

	// 索引
	_ = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_paragraph_samples_final_label ON paragraph_samples(final_label)`).Error
	_ = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_paragraph_samples_document_id ON paragraph_samples(document_id)`).Error

	// ── classifier_model_states ────────────────────────────────────
	if err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS classifier_model_states (
			id              SERIAL PRIMARY KEY,
			model_version   INTEGER NOT NULL DEFAULT 0,
			sample_count    INTEGER NOT NULL DEFAULT 0,
			accuracy        DOUBLE PRECISION DEFAULT 0,
			trained_at      TIMESTAMPTZ,
			model_data_json TEXT DEFAULT '',
			phase           VARCHAR(20) DEFAULT 'cold_start',
			ai_call_count   INTEGER DEFAULT 0,
			created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)
	`).Error; err != nil {
		return err
	}

	log.Println("智能分类器相关表创建完成")
	return nil
}

func (m *Migration20260315CreateAIClassifierTables) Down(tx *gorm.DB) error {
	_ = tx.Exec(`DROP TABLE IF EXISTS paragraph_samples`).Error
	_ = tx.Exec(`DROP TABLE IF EXISTS classifier_model_states`).Error
	return nil
}
