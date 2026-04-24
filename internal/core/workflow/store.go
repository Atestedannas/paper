package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/core/verify"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) UpdateJobResult(ctx context.Context, jobID uuid.UUID, status Status, stage string, downloadPath string, verifyResult verify.Result) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("workflow store db is nil")
	}

	payload, err := json.Marshal(verifyResult)
	if err != nil {
		return fmt.Errorf("marshal verify result: %w", err)
	}

	result := s.db.WithContext(ctx).
		Model(&model.PaperWorkflowJob{}).
		Where("id = ?", jobID).
		Updates(map[string]any{
			"status":             string(status),
			"stage":              stage,
			"download_path":      downloadPath,
			"verify_result_json": string(payload),
			"updated_at":         time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}

	return nil
}
