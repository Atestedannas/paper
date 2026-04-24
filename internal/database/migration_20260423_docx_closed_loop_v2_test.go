package database

import (
	"fmt"
	"testing"
	"time"

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
