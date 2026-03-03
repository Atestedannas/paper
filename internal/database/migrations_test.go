package database

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Feature: payment-and-template-fix, Property 1: 数据库迁移幂等性
func TestMigrationIdempotency(t *testing.T) {
	// 跳过如果没有测试数据库
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	properties := gopter.NewProperties(nil)

	properties.Property("Migration can be run multiple times safely",
		prop.ForAll(
			func(seed int64) bool {
				// 1. 创建独立的测试数据库连接
				testDB, cleanup := setupTestDB(t, seed)
				defer cleanup()

				// 2. 第一次运行迁移
				err1 := runTestMigration(testDB, &Migration20250129AddUniversityID{})
				if err1 != nil {
					t.Logf("First migration failed: %v", err1)
					return false
				}
				state1 := getTableStructure(testDB, "format_templates")

				// 3. 第二次运行迁移
				err2 := runTestMigration(testDB, &Migration20250129AddUniversityID{})
				if err2 != nil {
					t.Logf("Second migration failed: %v", err2)
					return false
				}
				state2 := getTableStructure(testDB, "format_templates")

				// 4. 第三次运行迁移
				err3 := runTestMigration(testDB, &Migration20250129AddUniversityID{})
				if err3 != nil {
					t.Logf("Third migration failed: %v", err3)
					return false
				}
				state3 := getTableStructure(testDB, "format_templates")

				// 验证：所有运行都成功，且表结构相同
				structuresMatch := compareTableStructures(state1, state2) &&
					compareTableStructures(state2, state3)
				hasColumn := hasColumnInStructure(state1, "university_id")

				if !structuresMatch {
					t.Logf("Table structures don't match after multiple migrations")
				}
				if !hasColumn {
					t.Logf("university_id column not found after migration")
				}

				return structuresMatch && hasColumn
			},
			gen.Int64(),
		),
	)

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// setupTestDB 创建测试数据库连接
func setupTestDB(t *testing.T, seed int64) (*gorm.DB, func()) {
	// 使用唯一的数据库名称
	dbName := fmt.Sprintf("test_migration_%d_%d", time.Now().Unix(), seed)

	// 连接到默认数据库以创建测试数据库
	adminDSN := "host=localhost port=5432 user=postgres password=postgres dbname=postgres sslmode=disable"
	adminDB, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to connect to admin database: %v", err)
	}

	// 创建测试数据库
	sqlDB, _ := adminDB.DB()
	_, err = sqlDB.Exec(fmt.Sprintf("CREATE DATABASE %s", dbName))
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// 连接到测试数据库
	testDSN := fmt.Sprintf("host=localhost port=5432 user=postgres password=postgres dbname=%s sslmode=disable", dbName)
	testDB, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// 创建必要的表
	err = testDB.AutoMigrate(
		&model.University{},
		&model.FormatTemplate{},
		&MigrationRecord{},
	)
	if err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}

	// 返回清理函数
	cleanup := func() {
		sqlDB, _ := testDB.DB()
		sqlDB.Close()

		adminSQLDB, _ := adminDB.DB()
		_, _ = adminSQLDB.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		adminSQLDB.Close()
	}

	return testDB, cleanup
}

// runTestMigration 运行测试迁移
func runTestMigration(db *gorm.DB, migration Migration) error {
	name := migration.Name()

	// 检查迁移是否已执行
	var record MigrationRecord
	result := db.Where("name = ?", name).First(&record)
	if result.Error == nil {
		// 已执行，直接运行 Up 方法测试幂等性
		return migration.Up(db)
	}

	// 在事务中执行迁移
	return db.Transaction(func(tx *gorm.DB) error {
		if err := migration.Up(tx); err != nil {
			return err
		}

		record := MigrationRecord{
			Name:      name,
			AppliedAt: time.Now(),
		}
		return tx.Create(&record).Error
	})
}

// TableColumn 表列信息
type TableColumn struct {
	ColumnName string
	DataType   string
	IsNullable string
}

// getTableStructure 获取表结构
func getTableStructure(db *gorm.DB, tableName string) []TableColumn {
	var columns []TableColumn
	db.Raw(`
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_name = ?
		ORDER BY ordinal_position
	`, tableName).Scan(&columns)
	return columns
}

// compareTableStructures 比较表结构
func compareTableStructures(s1, s2 []TableColumn) bool {
	if len(s1) != len(s2) {
		return false
	}

	for i := range s1 {
		if s1[i].ColumnName != s2[i].ColumnName ||
			s1[i].DataType != s2[i].DataType ||
			s1[i].IsNullable != s2[i].IsNullable {
			return false
		}
	}

	return true
}

// hasColumnInStructure 检查列是否存在
func hasColumnInStructure(structure []TableColumn, columnName string) bool {
	for _, col := range structure {
		if col.ColumnName == columnName {
			return true
		}
	}
	return false
}

// Feature: payment-and-template-fix, Property 14: 迁移事务回滚完整性
func TestMigrationTransactionRollback(t *testing.T) {
	// 跳过如果没有测试数据库
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	properties := gopter.NewProperties(nil)

	properties.Property("Migration rollback restores original state",
		prop.ForAll(
			func(seed int64) bool {
				// 1. 创建测试数据库
				testDB, cleanup := setupTestDB(t, seed)
				defer cleanup()

				// 2. 获取初始状态
				initialState := getTableStructure(testDB, "format_templates")

				// 3. 尝试运行失败的迁移（在事务中）
				err := testDB.Transaction(func(tx *gorm.DB) error {
					// 先添加一个列
					if err := tx.Exec(`
						ALTER TABLE format_templates 
						ADD COLUMN test_column VARCHAR(50)
					`).Error; err != nil {
						return err
					}

					// 然后故意失败
					return fmt.Errorf("simulated migration failure")
				})

				// 4. 验证事务已回滚
				if err == nil {
					t.Logf("Expected migration to fail, but it succeeded")
					return false
				}

				// 5. 获取回滚后的状态
				finalState := getTableStructure(testDB, "format_templates")

				// 6. 验证状态相同
				statesMatch := compareTableStructures(initialState, finalState)
				hasTestColumn := hasColumnInStructure(finalState, "test_column")

				if !statesMatch {
					t.Logf("Table structure changed after rollback")
				}
				if hasTestColumn {
					t.Logf("test_column still exists after rollback")
				}

				return statesMatch && !hasTestColumn
			},
			gen.Int64(),
		),
	)

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// FailingMigration 用于测试的失败迁移
type FailingMigration struct{}

func (m *FailingMigration) Name() string {
	return "test_failing_migration"
}

func (m *FailingMigration) Up(tx *gorm.DB) error {
	return fmt.Errorf("simulated failure")
}

func (m *FailingMigration) Down(tx *gorm.DB) error {
	return nil
}

// TestPaymentResourceLinksCreation 测试 payment_resource_links 表创建
func TestPaymentResourceLinksCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// 创建测试数据库
	testDB, cleanup := setupTestDB(t, time.Now().Unix())
	defer cleanup()

	// 创建必要的依赖表
	err := testDB.AutoMigrate(
		&model.Order{},
		&model.PaymentRecord{},
	)
	if err != nil {
		t.Fatalf("Failed to create dependency tables: %v", err)
	}

	// 运行迁移
	migration := &Migration20250129CreatePaymentResourceLinks{}
	err = runTestMigration(testDB, migration)
	if err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// 验证表已创建
	if !testDB.Migrator().HasTable(&PaymentResourceLink{}) {
		t.Error("payment_resource_links table was not created")
	}

	// 验证可以插入数据
	link := PaymentResourceLink{
		ID:           uuid.New(),
		PaymentID:    uuid.New(),
		ResourceType: "paper",
		ResourceID:   uuid.New(),
		ServiceType:  "format_fix",
	}

	err = testDB.Create(&link).Error
	if err != nil {
		t.Errorf("Failed to insert test data: %v", err)
	}

	// 验证可以查询数据
	var retrieved PaymentResourceLink
	err = testDB.First(&retrieved, "id = ?", link.ID).Error
	if err != nil {
		t.Errorf("Failed to retrieve test data: %v", err)
	}

	if retrieved.ServiceType != link.ServiceType {
		t.Errorf("Retrieved data doesn't match: expected %s, got %s", link.ServiceType, retrieved.ServiceType)
	}
}

// TestPaymentConfigCreation 测试支付配置创建
func TestPaymentConfigCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// 创建测试数据库
	testDB, cleanup := setupTestDB(t, time.Now().Unix())
	defer cleanup()

	// 创建 system_settings 表
	err := testDB.AutoMigrate(&model.SystemSetting{})
	if err != nil {
		t.Fatalf("Failed to create system_settings table: %v", err)
	}

	// 运行迁移
	migration := &Migration20250129AddPaymentConfig{}
	err = runTestMigration(testDB, migration)
	if err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// 验证配置已创建
	var setting model.SystemSetting
	err = testDB.Where("key = ?", "payment_config").First(&setting).Error
	if err != nil {
		t.Fatalf("Failed to retrieve payment config: %v", err)
	}

	if setting.Value == "" {
		t.Error("Payment config value is empty")
	}

	if setting.Description != "Global payment strategy configuration" {
		t.Errorf("Unexpected description: %s", setting.Description)
	}

	// 验证幂等性 - 再次运行不应该报错
	err = runTestMigration(testDB, migration)
	if err != nil {
		t.Errorf("Second migration run failed: %v", err)
	}

	// 验证只有一条记录
	var count int64
	testDB.Model(&model.SystemSetting{}).Where("key = ?", "payment_config").Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 payment config record, got %d", count)
	}
}

// TestMigrationLogging 测试迁移日志记录
func TestMigrationLogging(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// 创建测试数据库
	testDB, cleanup := setupTestDB(t, time.Now().Unix())
	defer cleanup()

	// 运行迁移
	migration := &Migration20250129AddUniversityID{}
	err := runTestMigration(testDB, migration)
	if err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// 验证迁移记录已创建
	var record MigrationRecord
	err = testDB.Where("name = ?", migration.Name()).First(&record).Error
	if err != nil {
		t.Errorf("Migration record not found: %v", err)
	}

	if record.Name != migration.Name() {
		t.Errorf("Expected migration name %s, got %s", migration.Name(), record.Name)
	}

	if record.AppliedAt.IsZero() {
		t.Error("AppliedAt timestamp is zero")
	}
}

// 辅助函数：初始化测试配置
func getTestConfig() *config.Config {
	return &config.Config{
		Database: config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			User:     "postgres",
			Password: "postgres",
			Name:     "test_db",
			SSLMode:  "disable",
		},
	}
}
