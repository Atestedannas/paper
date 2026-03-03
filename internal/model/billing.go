package model

import (
	"time"

	"github.com/google/uuid"
)

// PricingModel 计费模式
type PricingModel string

const (
	PricingModelFree  PricingModel = "free"  // 免费
	PricingModelCount PricingModel = "count" // 按次计费
	PricingModelMonth PricingModel = "month" // 按月订阅
	PricingModelYear  PricingModel = "year"  // 按年订阅
)

// ServicePricing 服务计费配置模型
type ServicePricing struct {
	ID           uuid.UUID    `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	ServiceType  string       `gorm:"size:50;uniqueIndex;not null" json:"service_type"` // 服务类型: paper_check, paper_download, paper_fix 等
	ServiceName  string       `gorm:"size:100" json:"service_name"`                     // 服务显示名称
	PricingModel PricingModel `gorm:"size:20;not null" json:"pricing_model"`            // 计费模式: free, count, month, year
	IsEnabled    bool         `gorm:"default:true" json:"is_enabled"`                   // 是否启用该服务
	IsFree       bool         `gorm:"default:false" json:"is_free"`                     // 是否免费（可独立于 pricing_model 设置）
	Price        float64      `gorm:"default:0" json:"price"`                           // 价格（分/次 或 元/月）
	Currency     string       `gorm:"size:10;default:CNY" json:"currency"`              // 货币: CNY, USD
	FreeCount    int          `gorm:"default:0" json:"free_count"`                      // 免费次数（每月）
	Description  string       `gorm:"size:500" json:"description"`                      // 服务描述
	SortOrder    int          `gorm:"default:0" json:"sort_order"`                      // 排序顺序
	CreatedAt    time.Time    `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt    time.Time    `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// PricingPlan 计费套餐模型
type PricingPlan struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	PlanName    string    `gorm:"size:50;not null" json:"plan_name"`   // 套餐名称: basic, pro, enterprise
	PlanType    string    `gorm:"size:20;not null" json:"plan_type"`   // 套餐类型: monthly, yearly,永久
	ServiceType string    `gorm:"size:50;index" json:"service_type"`   // 关联服务类型
	Price       float64   `gorm:"not null" json:"price"`               // 套餐价格
	Currency    string    `gorm:"size:10;default:CNY" json:"currency"` // 货币
	PeriodDays  int       `gorm:"default:30" json:"period_days"`       // 周期天数
	CheckCount  int       `gorm:"default:0" json:"check_count"`        // 检查次数限制（0表示无限）
	Description string    `gorm:"size:500" json:"description"`         // 套餐描述
	Features    string    `gorm:"type:text" json:"features"`           // 功能特性（JSON数组）
	IsActive    bool      `gorm:"default:true" json:"is_active"`       // 是否启用
	SortOrder   int       `gorm:"default:0" json:"sort_order"`         // 排序顺序
	CreatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}
