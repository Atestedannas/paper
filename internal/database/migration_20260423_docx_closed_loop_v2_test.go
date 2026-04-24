package database

import (
	"fmt"
	"testing"
	"time"

	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigration20260423CreateDocxClosedLoopV2Tables(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testDB, cleanup := setupDocxClosedLoopV2TestDB(t, time.Now().UnixNano())
	defer cleanup()

	migration := &Migration20260423CreateDocxClosedLoopV2Tables{}
	if err := runTestMigration(testDB, migration); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	if !testDB.Migrator().HasTable("compiled_templates") {
		t.Fatalf("compiled_templates table was not created")
	}
	if !testDB.Migrator().HasTable("paper_workflow_jobs") {
		t.Fatalf("paper_workflow_jobs table was not created")
	}
	if !testDB.Migrator().HasTable("paper_workflow_issues") {
		t.Fatalf("paper_workflow_issues table was not created")
	}

	assertForeignKeyExists(t, testDB, "paper_workflow_jobs", "paper_id", "papers")
	assertForeignKeyExists(t, testDB, "paper_workflow_jobs", "user_id", "users")
	assertForeignKeyExists(t, testDB, "paper_workflow_jobs", "compiled_template_id", "compiled_templates")
	assertForeignKeyExists(t, testDB, "paper_workflow_issues", "job_id", "paper_workflow_jobs")
	assertForeignKeyDeleteRule(t, testDB, "paper_workflow_jobs", "paper_id", "CASCADE")
	assertForeignKeyDeleteRule(t, testDB, "paper_workflow_jobs", "user_id", "CASCADE")
	assertForeignKeyDeleteRule(t, testDB, "paper_workflow_issues", "job_id", "CASCADE")

	if !testDB.Migrator().HasIndex(&model.PaperWorkflowJob{}, "idx_paper_workflow_jobs_status_stage") {
		t.Fatalf("idx_paper_workflow_jobs_status_stage index was not created")
	}

	if err := runTestMigration(testDB, migration); err != nil {
		t.Fatalf("migration was not idempotent: %v", err)
	}
}

func setupDocxClosedLoopV2TestDB(t *testing.T, seed int64) (*gorm.DB, func()) {
	t.Helper()

	dbName := fmt.Sprintf("test_docx_closed_loop_v2_%d", seed)
	testConfig := getTestConfig()

	adminDSN := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=postgres sslmode=%s",
		testConfig.Database.Host,
		testConfig.Database.Port,
		testConfig.Database.User,
		testConfig.Database.Password,
		testConfig.Database.SSLMode,
	)
	adminDB, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to connect to admin database: %v", err)
	}

	adminSQLDB, err := adminDB.DB()
	if err != nil {
		t.Fatalf("failed to open admin sql db: %v", err)
	}

	if _, err := adminSQLDB.Exec(fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}

	testDSN := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		testConfig.Database.Host,
		testConfig.Database.Port,
		testConfig.Database.User,
		testConfig.Database.Password,
		dbName,
		testConfig.Database.SSLMode,
	)
	testDB, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	if err := testDB.AutoMigrate(&MigrationRecord{}); err != nil {
		t.Fatalf("failed to create migration records table: %v", err)
	}
	if err := testDB.Exec(`CREATE TABLE users (id UUID PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}
	if err := testDB.Exec(`CREATE TABLE papers (id UUID PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("failed to create papers table: %v", err)
	}

	cleanup := func() {
		sqlDB, err := testDB.DB()
		if err == nil {
			_ = sqlDB.Close()
		}

		_, _ = adminSQLDB.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		_ = adminSQLDB.Close()
	}

	return testDB, cleanup
}

func assertForeignKeyExists(t *testing.T, db *gorm.DB, tableName, columnName, referencedTable string) {
	t.Helper()

	var count int64
	err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name
		 AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage ccu
		  ON tc.constraint_name = ccu.constraint_name
		 AND tc.table_schema = ccu.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_schema = current_schema()
		  AND tc.table_name = ?
		  AND kcu.column_name = ?
		  AND ccu.table_name = ?
	`, tableName, columnName, referencedTable).Scan(&count).Error
	if err != nil {
		t.Fatalf("failed to inspect foreign keys for %s.%s: %v", tableName, columnName, err)
	}
	if count == 0 {
		t.Fatalf("foreign key for %s.%s -> %s was not created", tableName, columnName, referencedTable)
	}
}

func assertForeignKeyDeleteRule(t *testing.T, db *gorm.DB, tableName, columnName, expectedRule string) {
	t.Helper()

	var deleteRule string
	err := db.Raw(`
		SELECT rc.delete_rule
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name
		 AND tc.table_schema = kcu.table_schema
		JOIN information_schema.referential_constraints rc
		  ON tc.constraint_name = rc.constraint_name
		 AND tc.constraint_schema = rc.constraint_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_schema = current_schema()
		  AND tc.table_name = ?
		  AND kcu.column_name = ?
		LIMIT 1
	`, tableName, columnName).Scan(&deleteRule).Error
	if err != nil {
		t.Fatalf("failed to inspect delete rule for %s.%s: %v", tableName, columnName, err)
	}
	if deleteRule != expectedRule {
		t.Fatalf("unexpected delete rule for %s.%s: got %s want %s", tableName, columnName, deleteRule, expectedRule)
	}
}
