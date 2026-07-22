package database

import (
	"log"
	"time"

	"github.com/paper-format-checker/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// AutoMigrate 鑷姩杩佺Щ鏁版嵁搴撹〃缁撴瀯
func AutoMigrate() {
	log.Println("寮€濮嬫暟鎹簱杩佺Щ...")

	// 鎸変緷璧栭『搴忚縼绉昏〃
	err := DB.AutoMigrate(
		// 鍩虹琛?
		&model.University{},
		&model.FormatTemplate{},
		&model.FormatTemplateRuleRevision{},
		&model.SystemSetting{},
		&model.User{},
		&model.MemberLevel{},
		&model.Role{},
		&model.Permission{},

		// 渚濊禆琛?
		&model.Member{},
		&model.Paper{},
		&model.CheckResult{},
		&model.FormatCorrection{},
		&model.Order{},
		&model.PaymentRecord{},
		&model.PromoCode{},
		&model.PromoCodeGrant{},

		// 澶氬澶氬叧鑱旇〃
		&model.UserRole{},
		&model.UserPermission{},
		&model.RolePermission{},
	)

	if err != nil {
		log.Fatalf("鏁版嵁搴撹縼绉诲け璐? %v", err)
	}

	log.Println("数据库迁移完成")

	// 鎻掑叆鍒濆鏁版嵁宸茬粡琚Щ鍒?PerformMigration 涓皟鐢?
	// insertInitialData()
}

// insertInitialData 鎻掑叆鍒濆鏁版嵁
func insertInitialData() {
	log.Println("鎻掑叆鍒濆鏁版嵁...")

	// 鎻掑叆榛樿楂樻牎
	insertDefaultUniversities()

	// 鎻掑叆榛樿浼氬憳绛夌骇
	insertDefaultMemberLevels()

	// 鎻掑叆榛樿绯荤粺璁剧疆
	insertDefaultSystemSettings()

	// 鎻掑叆瓒呯骇绠＄悊鍛?
	// 閲嶇疆鎸囧畾绠＄悊鍛樺瘑鐮?

	log.Println("鍒濆鏁版嵁鎻掑叆瀹屾垚")

	// 鍒濆鍖朢BAC鏁版嵁
	InitRBACData()
}

// resetSpecificAdminPassword 閲嶇疆鎸囧畾绠＄悊鍛樿处鍙峰瘑鐮?
func resetSpecificAdminPassword() {
	targetEmail := ""
	newPassword := ""

	var user model.User
	if err := DB.Where("email = ?", targetEmail).First(&user).Error; err == nil {
		// 鐢熸垚鏂板瘑鐮佸搱甯?
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
		if err != nil {
			log.Printf("鐢熸垚瀵嗙爜鍝堝笇澶辫触: %v", err)
			return
		}

		// 鏇存柊瀵嗙爜
		if err := DB.Model(&user).Update("password_hash", string(hashedPassword)).Error; err != nil {
			log.Printf("閲嶇疆绠＄悊鍛樺瘑鐮佸け璐? %v", err)
		} else {
			log.Printf("绠＄悊鍛樺瘑鐮佸凡閲嶇疆 - 璐﹀彿: %s, 鏂板瘑鐮? %s", targetEmail, newPassword)
		}
	} else {
		log.Printf("鏈壘鍒扮洰鏍囩鐞嗗憳璐﹀彿: %s", targetEmail)
	}
}

// insertSuperAdmin 鎻掑叆瓒呯骇绠＄悊鍛?
func insertSuperAdmin() {
	username := ""
	email := ""
	password := ""

	var count int64
	DB.Model(&model.User{}).Where("username = ? OR email = ?", username, email).Count(&count)
	if count == 0 {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 12)
		if err != nil {
			log.Printf("鐢熸垚绠＄悊鍛樺瘑鐮佸け璐? %v", err)
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
			log.Printf("鎻掑叆瓒呯骇绠＄悊鍛樺け璐? %v", err)
		} else {
			log.Printf("瓒呯骇绠＄悊鍛樺凡鍒涘缓 - 璐﹀彿: %s / %s, 瀵嗙爜: %s", username, email, password)
		}
	} else {
		// 纭繚鐜版湁绠＄悊鍛樻潈闄愭纭?
		DB.Model(&model.User{}).Where("username = ?", username).Updates(map[string]interface{}{
			"role": "admin",
		})
		log.Println("瓒呯骇绠＄悊鍛樿处鍙峰凡瀛樺湪 (宸茬‘淇濇潈闄愪负 admin)")
	}
}

// insertDefaultUniversities 鎻掑叆榛樿楂樻牎
func insertDefaultUniversities() {
	universities := []model.University{
		{
			Name:        "閲嶅簡宸ョ▼瀛﹂櫌",
			Abbr:        "CQIE",
			Description: "閲嶅簡宸ョ▼瀛﹂櫌",
			Tags:        `["鏈闄㈡牎", "宸ョ▼绫?]`,
			Color:       "#1890ff",
		},
		{
			Name:        "閲嶅簡宸ュ晢澶у",
			Abbr:        "CTBU",
			Description: "閲嶅簡宸ュ晢澶у",
			Tags:        `["鏈闄㈡牎", "璐㈢粡绫?]`,
			Color:       "#52c41a",
		},
		{
			Name:        "娓呭崕澶у",
			Abbr:        "THU",
			Description: "娓呭崕澶у",
			Tags:        `["985", "211", "鍙屼竴娴?]`,
			Color:       "#722ed1",
		},
		{
			Name:        "鍖椾含澶у",
			Abbr:        "PKU",
			Description: "鍖椾含澶у",
			Tags:        `["985", "211", "鍙屼竴娴?]`,
			Color:       "#f5222d",
		},
	}

	for _, university := range universities {
		var count int64
		DB.Model(&model.University{}).Where("name = ?", university.Name).Count(&count)
		if count == 0 {
			DB.Create(&university)
			log.Printf("鎻掑叆楂樻牎: %s", university.Name)
		}
	}
}

// insertDefaultMemberLevels 鎻掑叆榛樿浼氬憳绛夌骇
func insertDefaultMemberLevels() {
	memberLevels := []model.MemberLevel{
		{
			LevelName:    "鍏嶈垂鐢ㄦ埛",
			Price:        0.00,
			DurationDays: 365,
			MaxChecks:    5,
			MaxFileSize:  5 * 1024 * 1024, // 5MB
			Features:     `["鍩烘湰鏍煎紡妫€鏌?, "姣忔棩5娆℃鏌ラ檺鍒?, "鏂囦欢澶у皬闄愬埗5MB"]`,
			Description:  "鍏嶈垂鍩虹鏈嶅姟",
			SortOrder:    1,
			IsActive:     true,
		},
		{
			LevelName:    "楂樼骇浼氬憳",
			Price:        29.99,
			DurationDays: 30,
			MaxChecks:    100,
			MaxFileSize:  20 * 1024 * 1024, // 20MB
			Features:     `["楂樼骇鏍煎紡妫€鏌?, "姣忔棩100娆℃鏌ラ檺鍒?, "鏂囦欢澶у皬闄愬埗20MB", "浼樺厛澶勭悊"]`,
			Description:  "楂樼骇浼氬憳鏈嶅姟",
			SortOrder:    2,
			IsActive:     true,
		},
		{
			LevelName:    "涓撲笟浼氬憳",
			Price:        99.99,
			DurationDays: 365,
			MaxChecks:    1000,
			MaxFileSize:  50 * 1024 * 1024, // 50MB
			Features:     `["涓撲笟鏍煎紡妫€鏌?, "姣忔棩1000娆℃鏌ラ檺鍒?, "鏂囦欢澶у皬闄愬埗50MB", "浼樺厛澶勭悊", "涓撳睘鏀寔"]`,
			Description:  "涓撲笟浼氬憳鏈嶅姟",
			SortOrder:    3,
			IsActive:     true,
		},
	}

	for _, level := range memberLevels {
		var count int64
		DB.Model(&model.MemberLevel{}).Where("level_name = ?", level.LevelName).Count(&count)
		if count == 0 {
			DB.Create(&level)
			log.Printf("鎻掑叆浼氬憳绛夌骇: %s", level.LevelName)
		}
	}
}

// insertDefaultSystemSettings 鎻掑叆榛樿绯荤粺璁剧疆
func insertDefaultSystemSettings() {
	settings := []model.SystemSetting{
		{
			Key:         "site_name",
			Value:       "论文格式检查网站",
			Description: "缃戠珯鍚嶇О",
			IsSecret:    false,
		},
		{
			Key:         "site_description",
			Value:       "涓撲笟鐨勮鏂囨牸寮忔鏌ュ拰淇鏈嶅姟",
			Description: "缃戠珯鎻忚堪",
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
			Description: "寰俊灏忕▼搴廇ppID",
			IsSecret:    true,
		},
		{
			Key:         "alipay_app_id",
			Value:       "",
			Description: "鏀粯瀹滱ppID",
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
			log.Printf("鎻掑叆绯荤粺璁剧疆: %s", setting.Key)
		}
	}
}
