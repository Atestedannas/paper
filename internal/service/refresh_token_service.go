package service

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

var (
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrRefreshTokenExpired  = errors.New("refresh token expired")
	ErrRefreshTokenRevoked  = errors.New("refresh token revoked")
	ErrMaxRefreshExceeded   = errors.New("maximum refresh count exceeded")
)

type RefreshTokenService struct {
	db *gorm.DB
}

func NewRefreshTokenService(db *gorm.DB) *RefreshTokenService {
	return &RefreshTokenService{db: db}
}

func (s *RefreshTokenService) CreateRefreshToken(token string, userID uuid.UUID, expiresAt time.Time) (*model.RefreshToken, error) {
	refreshToken := &model.RefreshToken{
		Token:           token,
		UserID:          userID,
		RefreshCount:    0,
		ExpiresAt:       expiresAt,
		LastRefreshedAt: time.Now(),
		Revoked:         false,
	}

	if err := s.db.Create(refreshToken).Error; err != nil {
		return nil, err
	}

	return refreshToken, nil
}

func (s *RefreshTokenService) GetRefreshToken(token string) (*model.RefreshToken, error) {
	var refreshToken model.RefreshToken
	err := s.db.Where("token = ?", token).First(&refreshToken).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRefreshTokenNotFound
		}
		return nil, err
	}
	return &refreshToken, nil
}

func (s *RefreshTokenService) ValidateRefreshToken(token string, maxRefreshCount int) (*model.RefreshToken, error) {
	refreshToken, err := s.GetRefreshToken(token)
	if err != nil {
		return nil, err
	}

	if refreshToken.IsExpired() {
		return nil, ErrRefreshTokenExpired
	}

	if refreshToken.IsRevoked() {
		return nil, ErrRefreshTokenRevoked
	}

	if !refreshToken.CanRefresh(maxRefreshCount) {
		return nil, ErrMaxRefreshExceeded
	}

	return refreshToken, nil
}

func (s *RefreshTokenService) RefreshToken(oldToken string, newToken string, newExpiresAt time.Time, maxRefreshCount int) (*model.RefreshToken, error) {
	oldRefreshToken, err := s.ValidateRefreshToken(oldToken, maxRefreshCount)
	if err != nil {
		return nil, err
	}

	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	oldRefreshToken.Revoke("token refreshed")
	if err := tx.Save(oldRefreshToken).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	newRefreshToken := &model.RefreshToken{
		Token:           newToken,
		UserID:          oldRefreshToken.UserID,
		RefreshCount:    oldRefreshToken.RefreshCount + 1,
		ExpiresAt:       newExpiresAt,
		LastRefreshedAt: time.Now(),
		Revoked:         false,
	}

	if err := tx.Create(newRefreshToken).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	return newRefreshToken, nil
}

func (s *RefreshTokenService) RevokeRefreshToken(token string, reason string) error {
	refreshToken, err := s.GetRefreshToken(token)
	if err != nil {
		return err
	}

	refreshToken.Revoke(reason)
	return s.db.Save(refreshToken).Error
}

func (s *RefreshTokenService) RevokeUserRefreshTokens(userID uuid.UUID, reason string) error {
	now := time.Now()
	return s.db.Model(&model.RefreshToken{}).
		Where("user_id = ? AND revoked = false AND expires_at > ?", userID, now).
		Updates(map[string]interface{}{
			"revoked":        true,
			"revoked_at":     now,
			"revoked_reason": reason,
		}).Error
}

func (s *RefreshTokenService) CleanupExpiredTokens() error {
	return s.db.Where("expires_at <= ?", time.Now()).
		Delete(&model.RefreshToken{}).Error
}

func (s *RefreshTokenService) GetUserRefreshTokens(userID uuid.UUID) ([]model.RefreshToken, error) {
	var tokens []model.RefreshToken
	err := s.db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&tokens).Error
	return tokens, err
}

func (s *RefreshTokenService) GetActiveRefreshTokens(userID uuid.UUID) ([]model.RefreshToken, error) {
	var tokens []model.RefreshToken
	err := s.db.Where("user_id = ? AND revoked = false AND expires_at > ?", userID, time.Now()).
		Order("created_at DESC").
		Find(&tokens).Error
	return tokens, err
}

func (s *RefreshTokenService) StartCleanupTask(ctx context.Context, interval time.Duration) {
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
					log.Printf("component=refresh_token_cleanup error=%q", err)
				}
			}
		}
	}()
}
