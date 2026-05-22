package database

import (
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"
)

type legacyFormatTemplateConstraint struct {
	ConstraintName string
}

// RepairLegacyFormatTemplateConstraints removes historical, wrong-direction
// foreign keys from format_templates. The valid direction is check_results ->
// format_templates; format_templates must never depend on check_results.
func RepairLegacyFormatTemplateConstraints(db *gorm.DB) error {
	if db == nil || db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return nil
	}
	if !db.Migrator().HasTable("format_templates") {
		return nil
	}

	if err := db.Exec(`ALTER TABLE format_templates DROP CONSTRAINT IF EXISTS fk_check_results_template`).Error; err != nil {
		return fmt.Errorf("drop legacy fk_check_results_template: %w", err)
	}
	if err := db.Exec(`ALTER TABLE format_templates DROP CONSTRAINT IF EXISTS fk_format_templates_check_results`).Error; err != nil {
		return fmt.Errorf("drop legacy fk_format_templates_check_results: %w", err)
	}

	var constraints []legacyFormatTemplateConstraint
	if err := db.Raw(`
		SELECT c.conname AS constraint_name
		FROM pg_constraint c
		JOIN pg_class child ON child.oid = c.conrelid
		JOIN pg_namespace child_schema ON child_schema.oid = child.relnamespace
		LEFT JOIN pg_class parent ON parent.oid = c.confrelid
		WHERE child_schema.nspname = current_schema()
		  AND child.relname = 'format_templates'
		  AND c.contype = 'f'
		  AND (
		       parent.relname = 'check_results'
		    OR c.conname IN ('fk_check_results_template', 'fk_format_templates_check_results')
		  )
	`).Scan(&constraints).Error; err != nil {
		return fmt.Errorf("scan legacy format_templates constraints: %w", err)
	}

	for _, constraint := range constraints {
		name := strings.TrimSpace(constraint.ConstraintName)
		if name == "" {
			continue
		}
		if err := db.Exec(fmt.Sprintf(
			`ALTER TABLE format_templates DROP CONSTRAINT IF EXISTS %s`,
			quotePostgresIdentifier(name),
		)).Error; err != nil {
			return fmt.Errorf("drop legacy format_templates constraint %s: %w", name, err)
		}
		log.Printf("Dropped legacy format_templates constraint: %s", name)
	}

	if err := db.Exec(`ALTER TABLE format_templates DROP COLUMN IF EXISTS check_result_id`).Error; err != nil {
		return fmt.Errorf("drop legacy format_templates.check_result_id: %w", err)
	}

	return nil
}

func quotePostgresIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
