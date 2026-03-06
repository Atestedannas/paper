package service

import (
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

type TokenBlacklistService struct {
	db *gorm.DB
}

func NewTokenBlacklistService(db *gorm.DB) *TokenBlacklistService {
	return &TokenBlacklistService{db: db}
}

func (s *TokenBlacklistService) AddToken(token string, tokenType model.TokenType, userID uuid.UUID, expiresAt time.Time, reason string) error {
	blacklist := &model.TokenBlacklist{
		Token:     token,
		TokenType: tokenType,
		UserID:    userID,
		ExpiresAt: expiresAt,
		Reason:    reason,
	}
	return s.db.Create(blacklist).Error
}

func (s *TokenBlacklistService) IsTokenBlacklisted(token string) bool {
	var count int64
	err := s.db.Model(&model.TokenBlacklist{}).
		Where("token = ? AND expires_at > ?", token, time.Now()).
		Count(&count).Error
	return err == nil && count > 0
}

func (s *TokenBlacklistService) RevokeUserTokens(userID uuid.UUID, reason string) error {
	return s.db.Model(&model.TokenBlacklist{}).
		Where("user_id = ? AND expires_at > ?", userID, time.Now()).
		Update("reason", reason).Error
}

func (s *TokenBlacklistService) RevokeAllTokensByType(tokenType model.TokenType, reason string) error {
	return s.db.Model(&model.TokenBlacklist{}).
		Where("token_type = ? AND expires_at > ?", tokenType, time.Now()).
		Update("reason", reason).Error
}

func (s *TokenBlacklistService) CleanupExpiredTokens() error {
	return s.db.Where("expires_at <= ?", time.Now()).
		Delete(&model.TokenBlacklist{}).Error
}

func (s *TokenBlacklistService) GetBlacklistedTokensByUser(userID uuid.UUID) ([]model.TokenBlacklist, error) {
	var tokens []model.TokenBlacklist
	err := s.db.Where("user_id = ? AND expires_at > ?", userID, time.Now()).
		Find(&tokens).Error
	return tokens, err
}

func (s *TokenBlacklistService) GetBlacklistedTokensByType(tokenType model.TokenType) ([]model.TokenBlacklist, error) {
	var tokens []model.TokenBlacklist
	err := s.db.Where("token_type = ? AND expires_at > ?", tokenType, time.Now()).
		Find(&tokens).Error
	return tokens, err
}

func (s *TokenBlacklistService) RemoveToken(token string) error {
	return s.db.Where("token = ?", token).Delete(&model.TokenBlacklist{}).Error
}

func (s *TokenBlacklistService) StartCleanupTask(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			s.CleanupExpiredTokens()
		}
	}()
}
