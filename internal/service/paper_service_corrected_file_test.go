package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestResolveCorrectedPaperFileReturnsStoredExistingPath(t *testing.T) {
	db := openPaperServiceTestDB(t)
	previousDB := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previousDB })

	dir := t.TempDir()
	userID := uuid.New()
	paperID := uuid.New()
	correctedPath := filepath.Join(dir, "paper_v2_corrected.docx")
	if err := os.WriteFile(correctedPath, []byte("corrected"), 0644); err != nil {
		t.Fatalf("write corrected file: %v", err)
	}

	paper := model.Paper{
		ID:                paperID,
		UserID:            userID,
		Title:             "paper",
		FilePath:          filepath.Join(dir, "paper.docx"),
		FileName:          "paper.docx",
		FileSize:          1,
		FileType:          "docx",
		Status:            "corrected",
		CorrectedFilePath: correctedPath,
	}
	if err := db.Create(&paper).Error; err != nil {
		t.Fatalf("create paper: %v", err)
	}

	got, err := (PaperService{}).ResolveCorrectedPaperFile(userID, paperID)
	if err != nil {
		t.Fatalf("ResolveCorrectedPaperFile returned error: %v", err)
	}
	if got != correctedPath {
		t.Fatalf("path = %q, want %q", got, correctedPath)
	}
}

func TestResolveCorrectedPaperFileFindsV2CorrectedPath(t *testing.T) {
	db := openPaperServiceTestDB(t)
	previousDB := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previousDB })

	dir := t.TempDir()
	userID := uuid.New()
	paperID := uuid.New()
	originalPath := filepath.Join(dir, "paper.docx")
	correctedDir := filepath.Join(dir, "corrected")
	if err := os.MkdirAll(correctedDir, 0755); err != nil {
		t.Fatalf("mkdir corrected: %v", err)
	}
	correctedPath := filepath.Join(correctedDir, "paper_v2_corrected.docx")
	if err := os.WriteFile(correctedPath, []byte("corrected"), 0644); err != nil {
		t.Fatalf("write corrected file: %v", err)
	}

	paper := model.Paper{
		ID:       paperID,
		UserID:   userID,
		Title:    "paper",
		FilePath: originalPath,
		FileName: "paper.docx",
		FileSize: 1,
		FileType: "docx",
		Status:   "corrected",
	}
	if err := db.Create(&paper).Error; err != nil {
		t.Fatalf("create paper: %v", err)
	}

	got, err := (PaperService{}).ResolveCorrectedPaperFile(userID, paperID)
	if err != nil {
		t.Fatalf("ResolveCorrectedPaperFile returned error: %v", err)
	}
	if got != correctedPath {
		t.Fatalf("path = %q, want %q", got, correctedPath)
	}

	var saved model.Paper
	if err := db.First(&saved, "id = ?", paperID).Error; err != nil {
		t.Fatalf("reload paper: %v", err)
	}
	if saved.CorrectedFilePath != correctedPath {
		t.Fatalf("saved corrected path = %q, want %q", saved.CorrectedFilePath, correctedPath)
	}
}

func openPaperServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE papers (
			id text PRIMARY KEY,
			user_id text NOT NULL,
			title text NOT NULL,
			description text,
			file_path text NOT NULL,
			file_name text NOT NULL,
			file_size integer NOT NULL,
			file_type text NOT NULL,
			selected_template_id text,
			status text,
			corrected_file_path text,
			parsed_info text,
			auto_detected_templates text,
			deleted_at datetime,
			created_at datetime,
			updated_at datetime
		)
	`).Error; err != nil {
		t.Fatalf("create papers table: %v", err)
	}
	return db
}
