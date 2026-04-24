package workflow

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/core/verify"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestStoreUpdateJobResultPersistsStatusAndVerifyJSON(t *testing.T) {
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

	jobID := uuid.New()
	job := model.PaperWorkflowJob{
		ID:                 jobID,
		PaperID:            uuid.New(),
		UserID:             uuid.New(),
		CompiledTemplateID: uuid.New(),
		Status:             string(StatusUploaded),
		Stage:              "queued",
		VerifyResultJSON:   "{}",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	verifyResult := verify.Result{
		Passed: true,
		Warnings: []verify.Issue{{
			Kind:     "short_document",
			Severity: "warning",
			Message:  "document is short",
			Target:   "word/document.xml",
		}},
	}

	err = NewStore(db).UpdateJobResult(context.Background(), jobID, StatusVerifiedPass, "verified", "final.docx", verifyResult)
	if err != nil {
		t.Fatalf("UpdateJobResult() error = %v", err)
	}

	var got model.PaperWorkflowJob
	if err := db.First(&got, "id = ?", jobID).Error; err != nil {
		t.Fatalf("load job: %v", err)
	}
	if got.Status != string(StatusVerifiedPass) {
		t.Fatalf("Status = %s, want %s", got.Status, StatusVerifiedPass)
	}
	if got.Stage != "verified" {
		t.Fatalf("Stage = %s, want verified", got.Stage)
	}
	if got.DownloadPath != "final.docx" {
		t.Fatalf("DownloadPath = %s, want final.docx", got.DownloadPath)
	}

	var decoded verify.Result
	if err := json.Unmarshal([]byte(got.VerifyResultJSON), &decoded); err != nil {
		t.Fatalf("VerifyResultJSON is invalid: %v", err)
	}
	if !decoded.Passed || len(decoded.Warnings) != 1 {
		t.Fatalf("VerifyResultJSON decoded = %#v, want persisted verify result", decoded)
	}
}
