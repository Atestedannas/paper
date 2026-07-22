package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MemberLevel 会员等级模型
type MemberLevel struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	LevelName    string    `gorm:"size:50;uniqueIndex;not null" json:"level_name"` // 等级名称
	Price        float64   `gorm:"type:decimal(10,2);not null" json:"price"`       // 价格
	DurationDays int       `gorm:"not null" json:"duration_days"`                  // 有效期天数
	MaxChecks    int       `gorm:"not null" json:"max_checks"`                     // 最大检查次数
	MaxFileSize  int64     `gorm:"not null" json:"max_file_size"`                  // 最大文件大小（字节）
	Features     string    `gorm:"type:jsonb" json:"features"`                     // 会员特权（JSON格式）
	Description  string    `gorm:"type:text" json:"description"`                   // 等级描述
	SortOrder    int       `gorm:"default:0" json:"sort_order"`                    // 排序顺序
	IsActive     bool      `gorm:"default:true" json:"is_active"`                  // 是否激活
	CreatedAt    time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt    time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联
	Members []Member `gorm:"foreignKey:MemberLevelID" json:"members,omitempty"`
}

// Member 会员信息模型
type Member struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	UserID        uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"user_id"`
	MemberLevelID uuid.UUID `gorm:"type:uuid;index;not null" json:"member_level_id"`
	StartDate     time.Time `gorm:"not null" json:"start_date"`           // 开始日期
	EndDate       time.Time `gorm:"not null" json:"end_date"`             // 结束日期
	Status        string    `gorm:"size:20;default:active" json:"status"` // active, expired, cancelled
	TotalChecks   int       `gorm:"default:0" json:"total_checks"`        // 已使用检查次数
	CreatedAt     time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt     time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联
	User        User        `gorm:"foreignKey:UserID" json:"user,omitempty"`
	MemberLevel MemberLevel `gorm:"foreignKey:MemberLevelID" json:"member_level,omitempty"`
}

// Order 订单模型
type Order struct {
	ID            uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	UserID        uuid.UUID  `gorm:"type:uuid;index;not null" json:"user_id"`
	MemberLevelID uuid.UUID  `gorm:"type:uuid;index" json:"member_level_id"`          // 可以为空，用于论文检查订单
	OrderNo       string     `gorm:"size:50;uniqueIndex;not null" json:"order_no"`    // 订单号
	TotalAmount   float64    `gorm:"type:decimal(10,2);not null" json:"total_amount"` // 订单金额
	PaymentMethod string     `gorm:"size:20;not null" json:"payment_method"`          // wechat, alipay
	PaymentStatus string     `gorm:"size:20;default:pending;check:payment_status IN ('pending','paid','cancelled','expired','failed','refunded')" json:"payment_status"`
	OrderStatus   string     `gorm:"size:20;default:created;check:order_status IN ('created','completed','cancelled','failed','refunded')" json:"order_status"`
	ServiceType   string     `gorm:"size:32;index" json:"service_type,omitempty"`
	PaperID       *uuid.UUID `gorm:"type:uuid;index" json:"paper_id,omitempty"`
	UsedAt        *time.Time `json:"used_at,omitempty"`
	ExpiredAt     time.Time  `gorm:"not null;index" json:"expired_at"`
	CreatedAt     time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联
	User          User           `gorm:"foreignKey:UserID" json:"user,omitempty"`
	MemberLevel   *MemberLevel   `gorm:"foreignKey:MemberLevelID" json:"member_level,omitempty"` // 使用指针允许为空
	PaymentRecord *PaymentRecord `gorm:"foreignKey:OrderID" json:"payment_record,omitempty"`
}

// PaymentRecord 支付记录模型
type PaymentRecord struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	OrderID       uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"order_id"`
	TransactionID string    `gorm:"size:100;uniqueIndex" json:"transaction_id"` // 支付平台交易ID
	PaymentAmount float64   `gorm:"type:decimal(10,2);not null" json:"payment_amount"`
	PaymentMethod string    `gorm:"size:20;not null" json:"payment_method"` // wechat, alipay
	PaymentStatus string    `gorm:"size:20;not null;check:payment_status IN ('pending','success','failed','refunded')" json:"payment_status"`
	PaymentTime   time.Time `json:"payment_time"`                 // 支付时间
	ExtraData     string    `gorm:"type:jsonb" json:"extra_data"` // 额外数据（JSON格式）
	CreatedAt     time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt     time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联
	Order Order `gorm:"foreignKey:OrderID" json:"order,omitempty"`
}

// BeforeCreate GORM hook：确保 jsonb 字段不为空字符串
func (m *MemberLevel) BeforeCreate(tx *gorm.DB) error {
	if m.Features == "" {
		m.Features = "[]"
	}
	return nil
}

func (p *PaymentRecord) BeforeCreate(tx *gorm.DB) error {
	if p.ExtraData == "" {
		p.ExtraData = "{}"
	}
	return nil
}

func (o *Order) BeforeCreate(tx *gorm.DB) error {
	if o.ExpiredAt.IsZero() {
		o.ExpiredAt = time.Now().Add(30 * time.Minute)
	}
	return nil
}

// PaymentResourceLink 支付资源关联表
type PaymentResourceLink struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	UserID       uuid.UUID `gorm:"type:uuid;index;not null" json:"user_id"` // 用户ID
	PaymentID    uuid.UUID `gorm:"type:uuid;index;not null" json:"payment_id"`
	ResourceType string    `gorm:"size:50;not null" json:"resource_type"` // paper, report, etc.
	ResourceID   uuid.UUID `gorm:"type:uuid;index;not null" json:"resource_id"`
	ServiceType  string    `gorm:"size:50;not null" json:"service_type"` // format_check, format_fix, etc.
	CreatedAt    time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`

	// 关联
	User    User          `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Payment PaymentRecord `gorm:"foreignKey:PaymentID" json:"payment,omitempty"`
}
