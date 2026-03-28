package database

import (
	"fmt"
	"log"

	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB 全局数据库连接
var DB *gorm.DB

// InitDatabase 初始化数据库连接
func InitDatabase(config *config.Config) error {
	// 构建数据库连接字符串
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s client_encoding=UTF8",
		config.Database.Host,
		config.Database.Port,
		config.Database.User,
		config.Database.Password,
		config.Database.Name,
		config.Database.SSLMode,
	)

	logLevel := logger.Info
	if config.Server.Env == "production" {
		logLevel = logger.Error
	}

	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// 先运行自定义迁移脚本（在 AutoMigrate 之前）
	// err = RunMigrations()
	// if err != nil {
	// 	return fmt.Errorf("failed to run migrations: %w", err)
	// }

	// 然后自动迁移数据库表
	// err = migrateDatabase()
	// if err != nil {
	// 	return fmt.Errorf("failed to migrate database: %w", err)
	// }

	log.Println("Database connection established successfully")
	return nil
}

// PerformMigration 执行数据库迁移和初始化
func PerformMigration() error {
	log.Println("开始执行数据库迁移和初始化...")

	// 1. 先运行自定义迁移脚本
	if err := RunMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// 2. 自动迁移数据库表（GORM AutoMigrate）
	if err := migrateDatabase(); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	// 3. 插入初始数据
	insertInitialData()

	// 4. 最终兜底检查：确保 format_templates 表有 template_id 列
	// 这是一个强制性的修复，因为有时候 AutoMigrate 或 Versioned Migration 可能会失败或被跳过
	if DB.Migrator().HasTable("format_templates") {
		// ... (省略之前的日志代码) ...

		log.Println("CRITICAL FIX: Attempting to force add template_id column using raw SQL...")
		// 使用 IF NOT EXISTS (PostgreSQL 9.6+)
		if err := DB.Exec(`ALTER TABLE format_templates ADD COLUMN IF NOT EXISTS template_id VARCHAR(100)`).Error; err != nil {
			log.Printf("Failed to add template_id column: %v", err)
		} else {
			log.Println("SUCCESS: template_id column check/add completed.")
			// 尝试填充默认值
			DB.Exec(`UPDATE format_templates SET template_id = gen_random_uuid()::text WHERE template_id IS NULL`)
		}

		// 5. 强制删除顽固的错误外键约束
		// 即使 AutoMigrate 可能会尝试重建它，我们在这里再次删除它
		log.Println("CRITICAL FIX: Checking for and removing bad constraint 'fk_check_results_template' on 'format_templates'...")
		var count int64
		DB.Raw(`
			SELECT count(*)
			FROM pg_constraint c
			JOIN pg_class t ON c.conrelid = t.oid
			WHERE t.relname = 'format_templates' AND c.conname = 'fk_check_results_template'
		`).Scan(&count)

		if count > 0 {
			log.Println("Found bad constraint, FORCE DROPPING...")
			if err := DB.Exec(`ALTER TABLE format_templates DROP CONSTRAINT fk_check_results_template`).Error; err != nil {
				log.Printf("Failed to drop bad constraint: %v", err)
			} else {
				log.Println("Successfully dropped bad constraint.")
			}
		}
	}

	// 6. 确保智能分类器相关表存在（独立迁移，避免被前面的错误阻断）
	if err := DB.AutoMigrate(&model.ParagraphSample{}, &model.ClassifierModelState{}); err != nil {
		log.Printf("WARNING: 智能分类器表迁移失败: %v", err)
	}

	// 7. 确保 paragraph_samples 新增字段存在
	DB.Exec(`ALTER TABLE paragraph_samples ADD COLUMN IF NOT EXISTS has_originality_kw boolean DEFAULT false`)

	// 8. 确保 CMS 帖子相关表存在
	if err := DB.AutoMigrate(&model.CmsPost{}, &model.CmsReply{}); err != nil {
		log.Printf("WARNING: CMS 帖子表迁移失败: %v", err)
	}

	log.Println("数据库迁移和初始化完成")
	return nil
}

// migrateDatabase 迁移数据库表结构（重构后的模型）
func migrateDatabase() error {
	// 自动迁移所有模型
	return DB.AutoMigrate(
		// 核心模型
		&model.User{},
		&model.University{},
		&model.FormatTemplate{},
		&model.Paper{},
		&model.CheckResult{},
		&model.FormatCorrection{},

		// 会员和支付模型
		&model.MemberLevel{},
		&model.Member{},
		&model.Order{},
		&model.PaymentRecord{},

		// 系统设置
		&model.SystemSetting{},

		// RBAC 权限管理模型
		&model.Role{},
		&model.Permission{},
		&model.UserRole{},
		&model.UserPermission{},
		&model.RolePermission{},

		// 数据权限模型
		&model.DataPermission{},
		&model.DataPermissionRule{},

		// ACL 行级权限模型
		&model.ResourceACL{},
		&model.ACLUser{},

		// 智能分类器（自进化段落分类系统）
		&model.ParagraphSample{},
		&model.ClassifierModelState{},

		// CMS 帖子系统
		&model.CmsPost{},
		&model.CmsReply{},
	)
}
