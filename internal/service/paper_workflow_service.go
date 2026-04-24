package service

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

var (
	ErrInvalidJobID       = errors.New("invalid job id")
	ErrServiceUnavailable = errors.New("paper workflow service unavailable")
)

type WorkflowJobView struct {
	ID           uuid.UUID `json:"id"`
	Status       string    `json:"status"`
	Stage        string    `json:"stage"`
	DownloadPath string    `json:"download_path"`
}

type PaperWorkflowService interface {
	GetJob(id string) (*WorkflowJobView, error)
}

type paperWorkflowService struct {
	db *gorm.DB
}

func NewPaperWorkflowService(db *gorm.DB) PaperWorkflowService {
	return &paperWorkflowService{db: db}
}

func (s *paperWorkflowService) GetJob(id string) (*WorkflowJobView, error) {
	if s == nil || s.db == nil {
		return nil, ErrServiceUnavailable
	}

	jobID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJobID, err)
	}

	var job model.PaperWorkflowJob
	if err := s.db.First(&job, "id = ?", jobID).Error; err != nil {
		return nil, err
	}

	return &WorkflowJobView{
		ID:           job.ID,
		Status:       job.Status,
		Stage:        job.Stage,
		DownloadPath: job.DownloadPath,
	}, nil
}
