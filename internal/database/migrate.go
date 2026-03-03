package database

import (
	"log"
	"time"

	"github.com/paper-format-checker/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// AutoMigrate 自动迁移数据库表结构
func AutoMigrate() {
	log.Println("开始数据库迁移...")

	// 按依赖顺序迁移表
	err := DB.AutoMigrate(
		// 基础表
		&model.University{},
		&model.SystemSetting{},
		&model.User{},
		&model.MemberLevel{},
		&model.Role{},
		&model.Permission{},

		// 依赖表
		&model.Member{},
		&model.Paper{},
		&model.CheckResult{},
		&model.FormatCorrection{},
		&model.Order{},
		&model.PaymentRecord{},

		// 多对多关联表
		&model.UserRole{},
		&model.RolePermission{},
	)

	if err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	log.Println("数据库迁移完成")

	// 插入初始数据已经被移到 PerformMigration 中调用
	// insertInitialData()
}

// insertInitialData 插入初始数据
func insertInitialData() {
	log.Println("插入初始数据...")

	// 插入默认高校
	insertDefaultUniversities()

	// 插入默认会员等级
	insertDefaultMemberLevels()

	// 插入默认系统设置
	insertDefaultSystemSettings()

	// 插入超级管理员
	insertSuperAdmin()
	// 重置指定管理员密码
	resetSpecificAdminPassword()

	log.Println("初始数据插入完成")

	// 初始化RBAC数据
	InitRBACData()
}

// resetSpecificAdminPassword 重置指定管理员账号密码
func resetSpecificAdminPassword() {
	targetEmail := "2673078804@qq.com"
	newPassword := "123456"

	var user model.User
	if err := DB.Where("email = ?", targetEmail).First(&user).Error; err == nil {
		// 生成新密码哈希
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
		if err != nil {
			log.Printf("生成密码哈希失败: %v", err)
			return
		}

		// 更新密码
		if err := DB.Model(&user).Update("password_hash", string(hashedPassword)).Error; err != nil {
			log.Printf("重置管理员密码失败: %v", err)
		} else {
			log.Printf("管理员密码已重置 - 账号: %s, 新密码: %s", targetEmail, newPassword)
		}
	} else {
		log.Printf("未找到目标管理员账号: %s", targetEmail)
	}
}

// insertSuperAdmin 插入超级管理员
func insertSuperAdmin() {
	username := "admin"
	email := "admin@example.com"
	password := "Admin@123456" // 默认强密码

	var count int64
	DB.Model(&model.User{}).Where("username = ? OR email = ?", username, email).Count(&count)
	if count == 0 {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 12)
		if err != nil {
			log.Printf("生成管理员密码失败: %v", err)
			return
		}

		admin := model.User{
			Username:     username,
			Email:        email,
			PasswordHash: string(hashedPassword),
			Role:         "admin",
			Status:       "active",
			FreeChecks:   9999,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		if err := DB.Create(&admin).Error; err != nil {
			log.Printf("插入超级管理员失败: %v", err)
		} else {
			log.Printf("超级管理员已创建 - 账号: %s / %s, 密码: %s", username, email, password)
		}
	} else {
		// 确保现有管理员权限正确
		DB.Model(&model.User{}).Where("username = ?", username).Updates(map[string]interface{}{
			"role": "admin",
		})
		log.Println("超级管理员账号已存在 (已确保权限为 admin)")
	}
}

// insertDefaultUniversities 插入默认高校
func insertDefaultUniversities() {
	universities := []model.University{
		{
			Name:        "重庆工程学院",
			Abbr:        "CQIE",
			Description: "重庆工程学院",
			Tags:        `["本科院校", "工程类"]`,
			Color:       "#1890ff",
		},
		{
			Name:        "清华大学",
			Abbr:        "THU",
			Description: "清华大学",
			Tags:        `["985", "211", "双一流"]`,
			Color:       "#722ed1",
		},
		{
			Name:        "北京大学",
			Abbr:        "PKU",
			Description: "北京大学",
			Tags:        `["985", "211", "双一流"]`,
			Color:       "#f5222d",
		},
	}

	for _, university := range universities {
		var count int64
		DB.Model(&model.University{}).Where("name = ?", university.Name).Count(&count)
		if count == 0 {
			DB.Create(&university)
			log.Printf("插入高校: %s", university.Name)
		}
	}
}

// insertDefaultMemberLevels 插入默认会员等级
func insertDefaultMemberLevels() {
	memberLevels := []model.MemberLevel{
		{
			LevelName:    "免费用户",
			Price:        0.00,
			DurationDays: 365,
			MaxChecks:    5,
			MaxFileSize:  5 * 1024 * 1024, // 5MB
			Features:     `["基本格式检查", "每日5次检查限制", "文件大小限制5MB"]`,
			Description:  "免费基础服务",
			SortOrder:    1,
			IsActive:     true,
		},
		{
			LevelName:    "高级会员",
			Price:        29.99,
			DurationDays: 30,
			MaxChecks:    100,
			MaxFileSize:  20 * 1024 * 1024, // 20MB
			Features:     `["高级格式检查", "每日100次检查限制", "文件大小限制20MB", "优先处理"]`,
			Description:  "高级会员服务",
			SortOrder:    2,
			IsActive:     true,
		},
		{
			LevelName:    "专业会员",
			Price:        99.99,
			DurationDays: 365,
			MaxChecks:    1000,
			MaxFileSize:  50 * 1024 * 1024, // 50MB
			Features:     `["专业格式检查", "每日1000次检查限制", "文件大小限制50MB", "优先处理", "专属支持"]`,
			Description:  "专业会员服务",
			SortOrder:    3,
			IsActive:     true,
		},
	}

	for _, level := range memberLevels {
		var count int64
		DB.Model(&model.MemberLevel{}).Where("level_name = ?", level.LevelName).Count(&count)
		if count == 0 {
			DB.Create(&level)
			log.Printf("插入会员等级: %s", level.LevelName)
		}
	}
}

// insertDefaultSystemSettings 插入默认系统设置
func insertDefaultSystemSettings() {
	settings := []model.SystemSetting{
		{
			Key:         "site_name",
			Value:       "论文格式检查网站",
			Description: "网站名称",
			IsSecret:    false,
		},
		{
			Key:         "site_description",
			Value:       "专业的论文格式检查和修正服务",
			Description: "网站描述",
			IsSecret:    false,
		},
		{
			Key:         "max_file_size",
			Value:       "52428800", // 50MB
			Description: "最大文件上传大小（字节）",
			IsSecret:    false,
		},
		{
			Key:         "allowed_file_types",
			Value:       "pdf,docx",
			Description: "允许的文件类型",
			IsSecret:    false,
		},
		{
			Key:         "free_check_limit",
			Value:       "5",
			Description: "免费用户检查次数限制",
			IsSecret:    false,
		},
		{
			Key:         "wechat_app_id",
			Value:       "",
			Description: "微信小程序AppID",
			IsSecret:    true,
		},
		{
			Key:         "wechat_app_secret",
			Value:       "",
			Description: "微信小程序AppSecret",
			IsSecret:    true,
		},
		{
			Key:         "alipay_app_id",
			Value:       "",
			Description: "支付宝AppID",
			IsSecret:    true,
		},
		{
			Key:         "alipay_private_key",
			Value:       "",
			Description: "支付宝私钥",
			IsSecret:    true,
		},
	}

	for _, setting := range settings {
		var count int64
		DB.Model(&model.SystemSetting{}).Where("key = ?", setting.Key).Count(&count)
		if count == 0 {
			DB.Create(&setting)
			log.Printf("插入系统设置: %s", setting.Key)
		}
	}
}
