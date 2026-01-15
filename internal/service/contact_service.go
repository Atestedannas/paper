package service

import (
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
)

// ContactService 联系服务接口
type ContactService interface {
	CreateContactMessage(message *model.ContactMessage) error
	GetContactMessages() ([]model.ContactMessage, error)
	GetContactMessageByID(id uuid.UUID) (*model.ContactMessage, error)
	UpdateContactMessage(message *model.ContactMessage) error
	DeleteContactMessage(id uuid.UUID) error
}

// contactService 联系服务实现
type contactService struct{}

// NewContactService 创建联系服务实例
func NewContactService() ContactService {
	return &contactService{}
}

// CreateContactMessage 创建联系消息
func (s *contactService) CreateContactMessage(message *model.ContactMessage) error {
	return database.DB.Create(message).Error
}

// GetContactMessages 获取所有联系消息
func (s *contactService) GetContactMessages() ([]model.ContactMessage, error) {
	var messages []model.ContactMessage
	err := database.DB.Find(&messages).Error
	return messages, err
}

// GetContactMessageByID 根据ID获取联系消息
func (s *contactService) GetContactMessageByID(id uuid.UUID) (*model.ContactMessage, error) {
	var message model.ContactMessage
	err := database.DB.First(&message, "id = ?", id).Error
	return &message, err
}

// UpdateContactMessage 更新联系消息
func (s *contactService) UpdateContactMessage(message *model.ContactMessage) error {
	return database.DB.Save(message).Error
}

// DeleteContactMessage 删除联系消息
func (s *contactService) DeleteContactMessage(id uuid.UUID) error {
	return database.DB.Delete(&model.ContactMessage{}, id).Error
}
