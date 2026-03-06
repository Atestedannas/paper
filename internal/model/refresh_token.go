package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type RefreshToken struct {
	ID              uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	Token           string     `gorm:"type:varchar(1000);uniqueIndex;not null" json:"token"`
	UserID          uuid.UUID  `gorm:"type:uuid;not null;index" json:"user_id"`
	RefreshCount    int        `gorm:"not null;default:0" json:"refresh_count"`
	ExpiresAt       time.Time  `gorm:"not null;index" json:"expires_at"`
	LastRefreshedAt time.Time  `gorm:"not null" json:"last_refreshed_at"`
	Revoked         bool       `gorm:"not null;default:false;index" json:"revoked"`
	RevokedAt       *time.Time `json:"revoked_at"`
	RevokedReason   string     `gorm:"type:varchar(255)" json:"revoked_reason"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (RefreshToken) TableName() string {
	return "refresh_tokens"
}

func (rt *RefreshToken) BeforeCreate(tx *gorm.DB) error {
	if rt.ID == uuid.Nil {
		rt.ID = uuid.New()
	}
	if rt.LastRefreshedAt.IsZero() {
		rt.LastRefreshedAt = time.Now()
	}
	return nil
}

func (rt *RefreshToken) IsExpired() bool {
	return time.Now().After(rt.ExpiresAt)
}

func (rt *RefreshToken) IsRevoked() bool {
	return rt.Revoked
}

func (rt *RefreshToken) CanRefresh(maxRefreshCount int) bool {
	return !rt.IsExpired() && !rt.IsRevoked() && rt.RefreshCount < maxRefreshCount
}

func (rt *RefreshToken) Revoke(reason string) {
	now := time.Now()
	rt.Revoked = true
	rt.RevokedAt = &now
	rt.RevokedReason = reason
}
