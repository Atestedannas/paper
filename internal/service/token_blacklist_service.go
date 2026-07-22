package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TokenBlacklistService struct {
	db *gorm.DB
}

func NewTokenBlacklistService(db *gorm.DB) *TokenBlacklistService {
	return &TokenBlacklistService{db: db}
}

func (s *TokenBlacklistService) AddToken(token string, tokenType model.TokenType, userID uuid.UUID, expiresAt time.Time, reason string) error {
	blacklist := &model.TokenBlacklist{
		Token:     tokenHash(token),
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
		Where("token IN ? AND expires_at > ?", []string{tokenHash(token), token}, time.Now()).
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
	return s.db.Where("token IN ?", []string{tokenHash(token), token}).Delete(&model.TokenBlacklist{}).Error
}

func (s *TokenBlacklistService) RevokeUserAccessTokens(userID uuid.UUID, expiresAt time.Time, reason string) error {
	now := time.Now()
	entry := model.TokenBlacklist{
		Token: tokenHash("user:" + userID.String()), TokenType: model.TokenTypeAccessAll,
		UserID: userID, ExpiresAt: expiresAt, RevokedAt: now, Reason: reason,
	}
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "token"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"expires_at": expiresAt, "revoked_at": now, "reason": reason, "updated_at": now,
		}),
	}).Create(&entry).Error
}

func (s *TokenBlacklistService) AreUserAccessTokensRevoked(userID uuid.UUID, issuedAt time.Time) bool {
	var entry model.TokenBlacklist
	err := s.db.Where("token = ? AND token_type = ? AND expires_at > ?", tokenHash("user:"+userID.String()), model.TokenTypeAccessAll, time.Now()).
		First(&entry).Error
	return err == nil && !issuedAt.After(entry.RevokedAt)
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *TokenBlacklistService) StartCleanupTask(ctx context.Context, interval time.Duration) {
	if ctx == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.CleanupExpiredTokens(); err != nil {
					log.Printf("component=token_blacklist_cleanup error=%q", err)
				}
			}
		}
	}()
}
