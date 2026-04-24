package database

import (
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database/migrations"
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
		&Migration20250306CreateRBACTables{},
		&Migration20250306IncreaseAvatarLength{},
		&Migration20250306AddDeletedAtToPapers{},
		// RBAC 增强版迁移（包括菜单和权限初始化）
		&migrations.MigrationRBACEnhanced{},
		// RBAC 双模型收敛（authority -> permission，非破坏式）
		&Migration20260307UnifyRBACModel{},
		// ACL 行级权限表
		&Migration20260311CreateACLTables{},
		// 修正重庆文理学院模板左右页边距 3.17 → 3.18
		&Migration20260312FixCQWLMargins{},
		// 创建/修正四川师范大学格式模板（含完整格式规范）
		&Migration20260312FixSCNUFormat{},
		// 将四川师范大学格式规则升级为 EnhancedProcessor 统一结构（v2）
		&Migration20260313FixSCNUFormatV2{},
		// 创建智能分类器相关表（paragraph_samples / classifier_model_states）
		&Migration20260315CreateAIClassifierTables{},
		// 创建重庆理工大学论文格式模板
		&Migration20260315CreateCQUTFormat{},
		// V2: 强制更新重庆理工大学所有模板（修复 abstract/keywords/english_abstract 规则数据）
		&Migration20260315FixCQUTFormatV2{},
		// 创建重庆人文科技学院本科论文格式模板
		&Migration20260327CreateCQRWSTFormat{},
		// V2: 根据完整格式规范强制更新重庆人文科技学院模板
		&Migration20260327FixCQRWSTFormatV2{},
		// V3: 基于模板文件 XML 分析校正（致谢字号、标签补全、标题/注释/附录内容）
		&Migration20260327FixCQRWSTFormatV3{},
		// V4: 修复页眉页脚不生效（处理器支持顶层 header/page_number + 总页数格式）
		&Migration20260327FixCQRWSTFormatV4{},
		// V5: 修复段落分类准确率（(n)长文本→body）+ 所有内容区域显式 bold:false
		&Migration20260327FixCQRWSTFormatV5{},
		// 添加 golden_template_path 字段 + 为重庆人文科技学院设置黄金模板路径
		&Migration20260327AddGoldenTemplatePath{},
		// 将 golden_template_path 更新为真实模板文件（cqrwst_real.docx，414KB，格式学习效果最佳）
		&Migration20260327UpdateGoldenTemplateToReal{},
		// 为 check_results 表增加 diff_report jsonb 字段
		&Migration20260327AddDiffReport{},
		&Migration20260423CreateDocxClosedLoopV2Tables{},
	}

	// 按顺序执行迁移（单个失败不阻止后续，仅记录 warning）
	var firstErr error
	for _, migration := range migrations {
		if err := runMigration(migration); err != nil {
			log.Printf("⚠️  Migration %s failed (non-fatal, continuing): %v", migration.Name(), err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if firstErr != nil {
		log.Printf("All migrations finished with at least one error: %v", firstErr)
	} else {
		log.Println("All migrations completed successfully")
	}
	return firstErr
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

// Migration20250306AddDeletedAtToPapers 为 papers 表添加 deleted_at 字段
type Migration20250306AddDeletedAtToPapers struct{}

func (m *Migration20250306AddDeletedAtToPapers) Name() string {
	return "20250306_add_deleted_at_to_papers"
}

func (m *Migration20250306AddDeletedAtToPapers) Up(tx *gorm.DB) error {
	// 检查列是否已存在
	var exists bool
	err := tx.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns 
			WHERE table_name = 'papers' AND column_name = 'deleted_at'
		)
	`).Scan(&exists).Error
	if err != nil {
		return fmt.Errorf("检查列是否存在失败：%w", err)
	}

	if exists {
		log.Println("Column deleted_at already exists in papers table, skipping")
		return nil
	}

	// 添加 deleted_at 列
	log.Println("Adding deleted_at column to papers table")
	err = tx.Exec(`
		ALTER TABLE papers 
		ADD COLUMN deleted_at TIMESTAMPTZ NULL
	`).Error
	if err != nil {
		return fmt.Errorf("添加 deleted_at 列失败：%w", err)
	}

	// 为 deleted_at 创建索引
	log.Println("Creating index on deleted_at column")
	err = tx.Exec(`
		CREATE INDEX idx_papers_deleted_at ON papers(deleted_at)
	`).Error
	if err != nil {
		return fmt.Errorf("创建 deleted_at 索引失败：%w", err)
	}

	log.Println("Successfully added deleted_at column to papers table")
	return nil
}

func (m *Migration20250306AddDeletedAtToPapers) Down(tx *gorm.DB) error {
	// 删除索引
	err := tx.Exec(`DROP INDEX IF EXISTS idx_papers_deleted_at`).Error
	if err != nil {
		return fmt.Errorf("删除索引失败：%w", err)
	}

	// 删除列
	err = tx.Exec(`ALTER TABLE papers DROP COLUMN IF EXISTS deleted_at`).Error
	if err != nil {
		return fmt.Errorf("删除列失败：%w", err)
	}

	log.Println("Successfully removed deleted_at column from papers table")
	return nil
}

// Migration20260311CreateACLTables 创建 ACL 行级权限表
type Migration20260311CreateACLTables struct{}

func (m *Migration20260311CreateACLTables) Name() string {
	return "20260311_create_acl_tables"
}

func (m *Migration20260311CreateACLTables) Up(tx *gorm.DB) error {
	// 创建 resource_acls 表
	if err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS resource_acls (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			resource_type VARCHAR(50) NOT NULL,
			resource_id UUID NOT NULL,
			owner_id UUID NOT NULL,
			creator_id UUID,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)
	`).Error; err != nil {
		return fmt.Errorf("创建 resource_acls 表失败: %w", err)
	}

	// 创建索引
	if err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_resource_type_id
		ON resource_acls(resource_type, resource_id)
	`).Error; err != nil {
		return fmt.Errorf("创建 resource_acls 索引失败: %w", err)
	}

	if err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_resource_acls_owner_id
		ON resource_acls(owner_id)
	`).Error; err != nil {
		return fmt.Errorf("创建 resource_acls owner_id 索引失败: %w", err)
	}

	// 创建 resource_acl_users 表
	if err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS resource_acl_users (
			acl_id UUID NOT NULL,
			user_id UUID NOT NULL,
			access_level VARCHAR(20) DEFAULT 'read',
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (acl_id, user_id),
			CONSTRAINT fk_acl_users_acl FOREIGN KEY (acl_id) REFERENCES resource_acls(id) ON DELETE CASCADE
		)
	`).Error; err != nil {
		return fmt.Errorf("创建 resource_acl_users 表失败: %w", err)
	}

	if err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_resource_acl_users_user_id
		ON resource_acl_users(user_id)
	`).Error; err != nil {
		return fmt.Errorf("创建 resource_acl_users user_id 索引失败: %w", err)
	}

	log.Println("ACL tables created successfully")
	return nil
}

func (m *Migration20260311CreateACLTables) Down(tx *gorm.DB) error {
	if err := tx.Exec(`DROP TABLE IF EXISTS resource_acl_users`).Error; err != nil {
		return err
	}
	return tx.Exec(`DROP TABLE IF EXISTS resource_acls`).Error
}
