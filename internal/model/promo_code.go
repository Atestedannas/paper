package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	PromoGrantAvailable = "available"
	PromoGrantConsumed  = "consumed"
)

// PromoCode is a promotional access code. Only a hash and a display mask are stored.
type PromoCode struct {
	ID           uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	CampaignName string     `gorm:"size:100;not null" json:"campaign_name"`
	CodeHash     string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	CodeMask     string     `gorm:"size:32;not null" json:"code_mask"`
	ServiceType  string     `gorm:"size:32;index;not null" json:"service_type"`
	MaxUses      int        `gorm:"not null" json:"max_uses"`
	UsedCount    int        `gorm:"not null;default:0" json:"used_count"`
	PerUserLimit int        `gorm:"not null;default:1" json:"per_user_limit"`
	ExpiresAt    *time.Time `gorm:"index" json:"expires_at,omitempty"`
	IsActive     bool       `gorm:"not null;default:true;index" json:"is_active"`
	CreatedBy    uuid.UUID  `gorm:"type:uuid;index;not null" json:"created_by"`
	CreatedAt    time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// PromoCodeGrant is one claimed use. It remains reusable until a paper is created.
type PromoCodeGrant struct {
	ID          uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	PromoCodeID uuid.UUID  `gorm:"type:uuid;index;not null" json:"promo_code_id"`
	UserID      uuid.UUID  `gorm:"type:uuid;index;not null" json:"user_id"`
	ServiceType string     `gorm:"size:32;index;not null" json:"service_type"`
	Status      string     `gorm:"size:16;index;not null" json:"status"`
	PaperID     *uuid.UUID `gorm:"type:uuid;index" json:"paper_id,omitempty"`
	ExpiresAt   *time.Time `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt   time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	ConsumedAt  *time.Time `json:"consumed_at,omitempty"`

	PromoCode PromoCode `gorm:"foreignKey:PromoCodeID" json:"promo_code,omitempty"`
	User      User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
}
