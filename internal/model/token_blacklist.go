package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TokenType string

const (
	TokenTypeAccess  TokenType = "access"
	TokenTypeRefresh TokenType = "refresh"
)

type TokenBlacklist struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	Token     string    `gorm:"type:varchar(1000);uniqueIndex;not null" json:"token"`
	TokenType TokenType `gorm:"type:varchar(20);not null;index" json:"token_type"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
	RevokedAt time.Time `gorm:"not null" json:"revoked_at"`
	Reason    string    `gorm:"type:varchar(255)" json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (TokenBlacklist) TableName() string {
	return "token_blacklists"
}

func (tb *TokenBlacklist) BeforeCreate(tx *gorm.DB) error {
	if tb.ID == uuid.Nil {
		tb.ID = uuid.New()
	}
	if tb.RevokedAt.IsZero() {
		tb.RevokedAt = time.Now()
	}
	return nil
}

func (tb *TokenBlacklist) IsExpired() bool {
	return time.Now().After(tb.ExpiresAt)
}
