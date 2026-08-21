package database

import "gorm.io/gorm"

// Migration20260821RequireCQIEOriginalityDeclaration makes the written CQIE
// originality-declaration requirement visible to the deterministic workflow.
// A missing declaration requires review; the formatter must not fabricate a
// student signature or declaration content.
type Migration20260821RequireCQIEOriginalityDeclaration struct{}

func (m *Migration20260821RequireCQIEOriginalityDeclaration) Name() string {
	return "20260821_require_cqie_originality_declaration"
}

func (m *Migration20260821RequireCQIEOriginalityDeclaration) Up(tx *gorm.DB) error {
	return tx.Exec(`
		UPDATE format_templates
		SET format_rules = jsonb_set(
			format_rules,
			'{rule_pack}',
			COALESCE(format_rules->'rule_pack', '{}'::jsonb) || jsonb_build_object(
				'required_sections',
				COALESCE(format_rules #> '{rule_pack,required_sections}', '[]'::jsonb)
					|| CASE
						WHEN COALESCE(format_rules #> '{rule_pack,required_sections}', '[]'::jsonb) @> '["originality_declaration"]'::jsonb
						THEN '[]'::jsonb
						ELSE '["originality_declaration"]'::jsonb
					END
			),
			true
		), updated_at = NOW()
		WHERE name = '重庆工程学院本科论文格式标准'
	`).Error
}

func (m *Migration20260821RequireCQIEOriginalityDeclaration) Down(tx *gorm.DB) error {
	return nil
}
