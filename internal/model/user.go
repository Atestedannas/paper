package model

import (
	"time"

	"github.com/google/uuid"
)

// User 用户模型
type User struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Username     string    `gorm:"size:50;uniqueIndex;not null" json:"username"`
	Email        string    `gorm:"size:100;uniqueIndex;not null" json:"email"`
	PasswordHash string    `gorm:"size:100;not null" json:"-"` // 密码哈希，不返回给前端
	FullName     string    `gorm:"size:100" json:"full_name"`
	Avatar       string    `gorm:"size:10000" json:"avatar"`
	Status       string    `gorm:"size:20;default:active" json:"status"` // active, inactive, deleted
	Role         string    `gorm:"size:20;default:user" json:"role"`     // user, admin

	// 新用户试用额度
	FreeChecks int `gorm:"default:2" json:"free_checks"` // 剩余免费检查次数

	// 第三方登录相关字段
	WechatOpenID *string `gorm:"size:100;uniqueIndex" json:"wechat_open_id,omitempty"`
	AlipayOpenID *string `gorm:"size:100;uniqueIndex" json:"alipay_open_id,omitempty"`

	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联
	Papers       []Paper       `gorm:"foreignKey:UserID" json:"papers,omitempty"`
	Member       *Member       `gorm:"foreignKey:UserID" json:"member,omitempty"`
	CheckResults []CheckResult `gorm:"foreignKey:UserID" json:"check_results,omitempty"`
	Roles        []Role        `gorm:"many2many:user_roles;" json:"roles,omitempty"`
}
