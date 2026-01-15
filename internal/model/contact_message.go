package model

import (
	"time"

	"github.com/google/uuid"
)

// ContactMessage 联系消息模型
type ContactMessage struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Name      string    `gorm:"size:100;not null" json:"name"`
	Email     string    `gorm:"size:100;not null" json:"email"`
	Phone     string    `gorm:"size:20" json:"phone"`
	Subject   string    `gorm:"size:50;not null" json:"subject"`
	Message   string    `gorm:"type:text;not null" json:"message"`
	Status    string    `gorm:"size:20;default:pending" json:"status"` // pending, processed, replied
	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}
