package database

import (
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// Migration 迁移接口
type Migration interface {
	Up(tx *gorm.DB) error
	Down(tx *gorm.DB) error
	Name() string
}

// MigrationRecord 迁移记录
type MigrationRecord struct {
	ID        uint      `gorm:"primaryKey"`
	Name      string    `gorm:"size:255;uniqueIndex;not null"`
	AppliedAt time.Time `gorm:"default:CURRENT_TIMESTAMP"`
}

// RunMigrations 运行所有迁移
func RunMigrations() error {
	// 确保迁移记录表存在
	if err := DB.AutoMigrate(&MigrationRecord{}); err != nil {
		return fmt.Errorf("failed to create migration_records table: %w", err)
	}

	// 定义所有迁移
	migrations := []Migration{
		&Migration20250129FixCheckResultsTemplateID{},
		&Migration20250129AddUniversityID{},
		&Migration20250129CreatePaymentResourceLinks{},
		&Migration20250129AddPaymentConfig{},
		&Migration20250129FixFKCheckResultsTemplate{},
		&Migration20250129CleanupAndFixFK{},
		&Migration20250129DropBadConstraint{},
		&Migration20250129AddTemplateIDToFormatTemplates{},
		&Migration20250124UpdateCQIECFormat{},
	}

	// 按顺序执行迁移
	for _, migration := range migrations {
		if err := runMigration(migration); err != nil {
			return fmt.Errorf("migration %s failed: %w", migration.Name(), err)
		}
	}

	log.Println("All migrations completed successfully")
	return nil
}

// runMigration 运行单个迁移
func runMigration(migration Migration) error {
	name := migration.Name()

	// 检查迁移是否已执行
	var record MigrationRecord
	result := DB.Where("name = ?", name).First(&record)
	if result.Error == nil {
		log.Printf("Migration %s already applied, skipping", name)
		return nil
	}

	log.Printf("Running migration: %s", name)
	startTime := time.Now()

	// 在事务中执行迁移
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 执行迁移
		if err := migration.Up(tx); err != nil {
			log.Printf("Migration %s failed: %v", name, err)
			log.Printf("Rolling back migration %s", name)
			return err
		}

		// 记录迁移
		record := MigrationRecord{
			Name:      name,
			AppliedAt: time.Now(),
		}
		if err := tx.Create(&record).Error; err != nil {
			log.Printf("Failed to record migration %s: %v", name, err)
			log.Printf("Rolling back migration %s", name)
			return fmt.Errorf("failed to record migration: %w", err)
		}

		duration := time.Since(startTime)
		log.Printf("Migration %s completed successfully in %v", name, duration)
		return nil
	})

	if err != nil {
		log.Printf("Migration %s rolled back due to error: %v", name, err)
	}

	return err
}

// Migration20250129FixCheckResultsTemplateID 修复 check_results 表的 template_id 字段
type Migration20250129FixCheckResultsTemplateID struct{}

func (m *Migration20250129FixCheckResultsTemplateID) Name() string {
	return "20250129_fix_check_results_template_id"
}

func (m *Migration20250129FixCheckResultsTemplateID) Up(tx *gorm.DB) error {
	// 检查 template_id 列是否存在
	hasColumn := tx.Migrator().HasColumn(&model.CheckResult{}, "template_id")

	if !hasColumn {
		log.Println("Adding template_id column to check_results table as nullable first")
		// 先添加为可空列
		if err := tx.Exec(`
			ALTER TABLE check_results 
			ADD COLUMN template_id UUID
		`).Error; err != nil {
			return err
		}
	}

	// 检查是否有 NULL 值
	var nullCount int64
	tx.Model(&model.CheckResult{}).Where("template_id IS NULL").Count(&nullCount)

	if nullCount > 0 {
		log.Printf("Found %d check_results with NULL template_id, setting to a default value", nullCount)
		// 为 NULL 值设置一个默认的 UUID（可以是第一个模板的 ID）
		var defaultTemplateID uuid.UUID
		err := tx.Raw("SELECT id FROM format_templates LIMIT 1").Scan(&defaultTemplateID).Error
		if err != nil || defaultTemplateID == uuid.Nil {
			// 如果没有模板，创建一个默认的
			log.Println("No templates found, creating a default template")
			defaultTemplateID = uuid.New()
		}

		// 更新所有 NULL 值
		if err := tx.Exec(`
			UPDATE check_results 
			SET template_id = ? 
			WHERE template_id IS NULL
		`, defaultTemplateID).Error; err != nil {
			return err
		}
	}

	// 现在可以安全地设置为 NOT NULL
	log.Println("Setting template_id column to NOT NULL")
	return tx.Exec(`
		ALTER TABLE check_results 
		ALTER COLUMN template_id SET NOT NULL
	`).Error
}

func (m *Migration20250129FixCheckResultsTemplateID) Down(tx *gorm.DB) error {
	return tx.Exec(`
		ALTER TABLE check_results 
		ALTER COLUMN template_id DROP NOT NULL
	`).Error
}

// Migration20250129AddUniversityID 添加 university_id 字段到 format_templates 表
type Migration20250129AddUniversityID struct{}

func (m *Migration20250129AddUniversityID) Name() string {
	return "20250129_add_university_id_to_format_templates"
}

func (m *Migration20250129AddUniversityID) Up(tx *gorm.DB) error {
	// 检查字段是否已存在
	if tx.Migrator().HasColumn(&model.FormatTemplate{}, "university_id") {
		log.Println("Column university_id already exists in format_templates, skipping")
		return nil
	}

	// 添加字段
	log.Println("Adding university_id column to format_templates table")
	return tx.Exec(`
		ALTER TABLE format_templates 
		ADD COLUMN university_id BIGINT REFERENCES universities(id) ON DELETE SET NULL
	`).Error
}

func (m *Migration20250129AddUniversityID) Down(tx *gorm.DB) error {
	return tx.Migrator().DropColumn(&model.FormatTemplate{}, "university_id")
}

// PaymentResourceLink 支付资源关联表
type PaymentResourceLink struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	PaymentID    uuid.UUID `gorm:"type:uuid;index;not null"`
	ResourceType string    `gorm:"size:50;not null"` // paper, report, etc.
	ResourceID   uuid.UUID `gorm:"type:uuid;index;not null"`
	ServiceType  string    `gorm:"size:50;not null"` // format_check, format_fix, etc.
	CreatedAt    time.Time `gorm:"default:CURRENT_TIMESTAMP"`
}

// Migration20250129CreatePaymentResourceLinks 创建 payment_resource_links 表
type Migration20250129CreatePaymentResourceLinks struct{}

func (m *Migration20250129CreatePaymentResourceLinks) Name() string {
	return "20250129_create_payment_resource_links"
}

func (m *Migration20250129CreatePaymentResourceLinks) Up(tx *gorm.DB) error {
	// 检查表是否已存在
	if tx.Migrator().HasTable(&PaymentResourceLink{}) {
		log.Println("Table payment_resource_links already exists, skipping")
		return nil
	}

	log.Println("Creating payment_resource_links table")

	// 创建表
	if err := tx.AutoMigrate(&PaymentResourceLink{}); err != nil {
		return err
	}

	// 创建索引
	if err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_payment_resource_links_payment_id 
		ON payment_resource_links(payment_id)
	`).Error; err != nil {
		return err
	}

	if err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_payment_resource_links_resource 
		ON payment_resource_links(resource_id, service_type)
	`).Error; err != nil {
		return err
	}

	if err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_payment_resource_links_composite 
		ON payment_resource_links(payment_id, resource_id, service_type)
	`).Error; err != nil {
		return err
	}

	return nil
}

func (m *Migration20250129CreatePaymentResourceLinks) Down(tx *gorm.DB) error {
	return tx.Migrator().DropTable(&PaymentResourceLink{})
}

// Migration20250129AddPaymentConfig 添加默认支付配置
type Migration20250129AddPaymentConfig struct{}

func (m *Migration20250129AddPaymentConfig) Name() string {
	return "20250129_add_payment_config"
}

func (m *Migration20250129AddPaymentConfig) Up(tx *gorm.DB) error {
	// 检查配置是否已存在
	var count int64
	tx.Model(&model.SystemSetting{}).Where("key = ?", "payment_config").Count(&count)
	if count > 0 {
		log.Println("Payment config already exists, skipping")
		return nil
	}

	log.Println("Adding default payment configuration")

	// 插入默认支付配置
	setting := model.SystemSetting{
		Key:         "payment_config",
		Value:       `{"is_check_free": true, "format_check": 10.0, "format_fix": 15.0}`,
		Description: "Global payment strategy configuration",
		IsSecret:    false,
	}

	return tx.Create(&setting).Error
}

func (m *Migration20250129AddPaymentConfig) Down(tx *gorm.DB) error {
	return tx.Where("key = ?", "payment_config").Delete(&model.SystemSetting{}).Error
}
