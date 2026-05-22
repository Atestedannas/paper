package model

import (
	"time"

	"github.com/google/uuid"
)

type CmsPost struct {
	ID        uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	UserID    uuid.UUID  `gorm:"type:uuid;not null;index" json:"user_id"`
	Username  string     `gorm:"size:100;not null" json:"username"`
	Avatar    string     `gorm:"size:500" json:"avatar"`
	Title     string     `gorm:"size:200;not null" json:"title"`
	Content   string     `gorm:"type:text;not null" json:"content"`
	Status    string     `gorm:"size:20;default:active;index" json:"status"` // active, closed, hidden
	ViewCount int        `gorm:"default:0" json:"view_count"`
	CreatedAt time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
	Replies   []CmsReply `gorm:"foreignKey:PostID" json:"replies,omitempty"`
}

type CmsReply struct {
	ID              uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	PostID          uuid.UUID  `gorm:"type:uuid;not null;index" json:"post_id"`
	UserID          uuid.UUID  `gorm:"type:uuid;not null;index" json:"user_id"`
	Username        string     `gorm:"size:100;not null" json:"username"`
	Avatar          string     `gorm:"size:500" json:"avatar"`
	Content         string     `gorm:"type:text;not null" json:"content"`
	IsAdmin         bool       `gorm:"default:false" json:"is_admin"`
	ReplyToID       *uuid.UUID `gorm:"type:uuid;index" json:"reply_to_id"`
	ReplyToUsername  string     `gorm:"size:100" json:"reply_to_username"`
	CreatedAt       time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}
