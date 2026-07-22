package database

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

func UpdateFormatTemplateWithAudit(template *model.FormatTemplate, updates map[string]interface{}, changedBy *uuid.UUID) error {
	if template == nil || template.ID == uuid.Nil {
		return fmt.Errorf("format template is required")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if nextRules, ok := updates["format_rules"].(string); ok && nextRules != "" && nextRules != template.FormatRules {
			revision := model.FormatTemplateRuleRevision{
				ID:               uuid.New(),
				FormatTemplateID: template.ID,
				FormatRules:      template.FormatRules,
				TemplateVersion:  template.Version,
				ChangedBy:        changedBy,
			}
			if err := tx.Create(&revision).Error; err != nil {
				return fmt.Errorf("record format rule revision: %w", err)
			}
		}
		return tx.Model(template).Updates(updates).Error
	})
}
