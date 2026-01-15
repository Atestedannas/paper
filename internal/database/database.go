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
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Database.Host,
		config.Database.Port,
		config.Database.User,
		config.Database.Password,
		config.Database.Name,
		config.Database.SSLMode,
	)

	// 设置日志级别
	logLevel := logger.Info
	if config.Server.Env == "production" {
		logLevel = logger.Error
	}

	// 连接数据库
	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// 先运行自定义迁移脚本（在 AutoMigrate 之前）
	err = RunMigrations()
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// 然后自动迁移数据库表
	err = migrateDatabase()
	if err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	log.Println("Database connection established successfully")
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
	)
}
