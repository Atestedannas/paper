package service

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/core/workflow"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPaperWorkflowServiceGetJobReturnsView(t *testing.T) {
	db := openPaperWorkflowServiceTestDB(t)
	jobID := uuid.New()
	userID := uuid.New()
	job := model.PaperWorkflowJob{
		ID:                 jobID,
		PaperID:            uuid.New(),
		UserID:             userID,
		CompiledTemplateID: uuid.New(),
		Status:             string(workflow.StatusVerifiedPass),
		Stage:              workflow.StageVerified,
		DownloadPath:       "out/final.docx",
		VerifyResultJSON:   "{}",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	view, err := NewPaperWorkflowService(db).GetJob(jobID.String())
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}

	if view.ID != jobID {
		t.Fatalf("ID = %s, want %s", view.ID, jobID)
	}
	if view.UserID != userID {
		t.Fatalf("UserID = %s, want %s", view.UserID, userID)
	}
	if view.Status != string(workflow.StatusVerifiedPass) {
		t.Fatalf("Status = %s, want %s", view.Status, workflow.StatusVerifiedPass)
	}
	if view.Stage != workflow.StageVerified {
		t.Fatalf("Stage = %s, want %s", view.Stage, workflow.StageVerified)
	}
	if view.DownloadPath != "out/final.docx" {
		t.Fatalf("DownloadPath = %s, want out/final.docx", view.DownloadPath)
	}
}

func TestPaperWorkflowServiceGetJobForUserReturnsOwnerJob(t *testing.T) {
	db := openPaperWorkflowServiceTestDB(t)
	jobID := uuid.New()
	userID := uuid.New()
	job := model.PaperWorkflowJob{
		ID:                 jobID,
		PaperID:            uuid.New(),
		UserID:             userID,
		CompiledTemplateID: uuid.New(),
		Status:             string(workflow.StatusVerifiedPass),
		Stage:              workflow.StageVerified,
		DownloadPath:       "out/final.docx",
		VerifyResultJSON:   "{}",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	view, err := NewPaperWorkflowService(db).GetJobForUser(jobID.String(), userID)
	if err != nil {
		t.Fatalf("GetJobForUser() error = %v", err)
	}

	if view.ID != jobID {
		t.Fatalf("ID = %s, want %s", view.ID, jobID)
	}
	if view.UserID != userID {
		t.Fatalf("UserID = %s, want %s", view.UserID, userID)
	}
}

func TestPaperWorkflowServiceGetJobForUserReturnsNotFoundForNonOwner(t *testing.T) {
	db := openPaperWorkflowServiceTestDB(t)
	jobID := uuid.New()
	job := model.PaperWorkflowJob{
		ID:                 jobID,
		PaperID:            uuid.New(),
		UserID:             uuid.New(),
		CompiledTemplateID: uuid.New(),
		Status:             string(workflow.StatusVerifiedPass),
		Stage:              workflow.StageVerified,
		DownloadPath:       "out/final.docx",
		VerifyResultJSON:   "{}",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	_, err := NewPaperWorkflowService(db).GetJobForUser(jobID.String(), uuid.New())
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("GetJobForUser() error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestPaperWorkflowServiceGetJobRejectsInvalidUUID(t *testing.T) {
	_, err := NewPaperWorkflowService(openPaperWorkflowServiceTestDB(t)).GetJob("not-a-uuid")
	if !errors.Is(err, ErrInvalidJobID) {
		t.Fatalf("GetJob() error = %v, want ErrInvalidJobID", err)
	}
}

func TestPaperWorkflowServiceGetJobReturnsNotFound(t *testing.T) {
	_, err := NewPaperWorkflowService(openPaperWorkflowServiceTestDB(t)).GetJob(uuid.New().String())
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("GetJob() error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func openPaperWorkflowServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE paper_workflow_jobs (
			id text PRIMARY KEY,
			paper_id text NOT NULL,
			user_id text NOT NULL,
			compiled_template_id text NOT NULL,
			status text NOT NULL,
			stage text NOT NULL,
			download_path text,
			verify_result_json text NOT NULL,
			created_at datetime,
			updated_at datetime
		)
	`).Error; err != nil {
		t.Fatalf("create jobs table: %v", err)
	}

	return db
}
