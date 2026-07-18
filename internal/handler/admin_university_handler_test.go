package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"gorm.io/gorm"
)

func TestAdminUniversityTemplatePreviewAndPermanentDelete(t *testing.T) {
	oldDB := database.DB
	t.Cleanup(func() { database.DB = oldDB })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE universities (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE format_templates (id TEXT PRIMARY KEY, university_id INTEGER, file_path TEXT, golden_template_path TEXT, is_active BOOLEAN, created_at DATETIME)`,
		`CREATE TABLE papers (id TEXT PRIMARY KEY, selected_template_id TEXT, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE check_results (id TEXT PRIMARY KEY, format_template_id TEXT)`,
		`CREATE TABLE format_corrections (id TEXT PRIMARY KEY, check_result_id TEXT)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	database.DB = db

	templateID := uuid.New()
	checkID := uuid.New()
	templatePath := filepath.Join(t.TempDir(), "template.docx")
	if err := os.WriteFile(templatePath, []byte("docx-preview"), 0600); err != nil {
		t.Fatalf("write template: %v", err)
	}
	db.Exec(`INSERT INTO universities (id, name) VALUES (1, 'Test University')`)
	db.Exec(`INSERT INTO format_templates (id, university_id, file_path, is_active, created_at) VALUES (?, 1, ?, 1, CURRENT_TIMESTAMP)`, templateID, templatePath)
	db.Exec(`INSERT INTO papers (id, selected_template_id) VALUES (?, ?)`, uuid.New(), templateID)
	db.Exec(`INSERT INTO check_results (id, format_template_id) VALUES (?, ?)`, checkID, templateID)
	db.Exec(`INSERT INTO format_corrections (id, check_result_id) VALUES (?, ?)`, uuid.New(), checkID)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAdminUniversityHandler()
	router.GET("/universities/:id/template-file", handler.DownloadTemplateFile)
	router.DELETE("/universities/:id", handler.DeleteUniversity)

	preview := httptest.NewRecorder()
	router.ServeHTTP(preview, httptest.NewRequest(http.MethodGet, "/universities/1/template-file?type=docx", nil))
	if preview.Code != http.StatusOK || preview.Body.String() != "docx-preview" {
		t.Fatalf("preview status=%d body=%q", preview.Code, preview.Body.String())
	}

	deleted := httptest.NewRecorder()
	router.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/universities/1", nil))
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	for _, table := range []string{"universities", "format_templates", "check_results", "format_corrections"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	var selectedTemplateID *string
	if err := db.Raw(`SELECT selected_template_id FROM papers LIMIT 1`).Scan(&selectedTemplateID).Error; err != nil || selectedTemplateID != nil {
		t.Fatalf("paper selected_template_id=%v err=%v", selectedTemplateID, err)
	}
	if _, err := os.Stat(templatePath); !os.IsNotExist(err) {
		t.Fatalf("template file still exists: %v", err)
	}
}
