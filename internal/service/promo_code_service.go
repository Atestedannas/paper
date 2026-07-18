package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrPromoCodeInvalid  = errors.New("卡密无效、已过期或次数已用完")
	ErrPromoCodeLimit    = errors.New("该用户已达到此卡密的使用次数限制")
	ErrPromoGrantInvalid = errors.New("体验授权无效或已使用")
)

type GeneratePromoCodesInput struct {
	CampaignName string
	Quantity     int
	ServiceType  string
	MaxUses      int
	PerUserLimit int
	ExpiresAt    *time.Time
}

type PromoCodeService struct{ db *gorm.DB }

func NewPromoCodeService(db *gorm.DB) *PromoCodeService { return &PromoCodeService{db: db} }

func (s *PromoCodeService) Generate(ctx context.Context, adminID uuid.UUID, input GeneratePromoCodesInput) ([]model.PromoCode, []string, error) {
	input.CampaignName = strings.TrimSpace(input.CampaignName)
	if input.CampaignName == "" || input.Quantity < 1 || input.Quantity > 500 || input.MaxUses < 1 || input.PerUserLimit < 1 || !validPromoService(input.ServiceType) {
		return nil, nil, errors.New("卡密生成参数不正确")
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		return nil, nil, errors.New("过期时间必须晚于当前时间")
	}

	codes := make([]model.PromoCode, 0, input.Quantity)
	plain := make([]string, 0, input.Quantity)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := 0; i < input.Quantity; i++ {
			value, err := generatePromoCode()
			if err != nil {
				return err
			}
			code := model.PromoCode{
				ID: uuid.New(), CampaignName: input.CampaignName, CodeHash: promoCodeHash(value),
				CodeMask: maskPromoCode(value), ServiceType: input.ServiceType, MaxUses: input.MaxUses,
				PerUserLimit: input.PerUserLimit, ExpiresAt: input.ExpiresAt, IsActive: true, CreatedBy: adminID,
			}
			if err := tx.Create(&code).Error; err != nil {
				return err
			}
			codes = append(codes, code)
			plain = append(plain, value)
		}
		return nil
	})
	return codes, plain, err
}

func (s *PromoCodeService) Redeem(ctx context.Context, userID uuid.UUID, rawCode, serviceType string) (*model.PromoCodeGrant, int, error) {
	if userID == uuid.Nil || !validPromoService(serviceType) {
		return nil, 0, ErrPromoCodeInvalid
	}
	var grant model.PromoCodeGrant
	remaining := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var code model.PromoCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code_hash = ?", promoCodeHash(rawCode)).First(&code).Error; err != nil {
			return ErrPromoCodeInvalid
		}
		if !code.IsActive || code.UsedCount >= code.MaxUses || (code.ExpiresAt != nil && !code.ExpiresAt.After(time.Now())) || code.ServiceType != serviceType {
			return ErrPromoCodeInvalid
		}

		// Repeated clicks return the existing unconsumed grant without spending another use.
		if err := tx.Where("promo_code_id = ? AND user_id = ? AND service_type = ? AND status = ?", code.ID, userID, serviceType, model.PromoGrantAvailable).
			Order("created_at DESC").First(&grant).Error; err == nil {
			remaining = code.MaxUses - code.UsedCount
			return nil
		}

		var userUses int64
		if err := tx.Model(&model.PromoCodeGrant{}).Where("promo_code_id = ? AND user_id = ?", code.ID, userID).Count(&userUses).Error; err != nil {
			return err
		}
		if userUses >= int64(code.PerUserLimit) {
			return ErrPromoCodeLimit
		}

		grant = model.PromoCodeGrant{
			ID: uuid.New(), PromoCodeID: code.ID, UserID: userID, ServiceType: serviceType,
			Status: model.PromoGrantAvailable, ExpiresAt: code.ExpiresAt,
		}
		if err := tx.Create(&grant).Error; err != nil {
			return err
		}
		if err := tx.Model(&code).UpdateColumn("used_count", gorm.Expr("used_count + 1")).Error; err != nil {
			return err
		}
		remaining = code.MaxUses - code.UsedCount - 1
		return nil
	})
	return &grant, remaining, err
}

func (s *PromoCodeService) ValidateGrant(ctx context.Context, grantID, userID uuid.UUID, serviceType string) (*model.PromoCodeGrant, error) {
	var grant model.PromoCodeGrant
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ? AND service_type = ? AND status = ?", grantID, userID, serviceType, model.PromoGrantAvailable).First(&grant).Error
	if err != nil || (grant.ExpiresAt != nil && !grant.ExpiresAt.After(time.Now())) {
		return nil, ErrPromoGrantInvalid
	}
	return &grant, nil
}

func (s *PromoCodeService) BindGrant(ctx context.Context, grantID, userID, paperID uuid.UUID) error {
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&model.PromoCodeGrant{}).
		Where("id = ? AND user_id = ? AND status = ?", grantID, userID, model.PromoGrantAvailable).
		Updates(map[string]interface{}{"paper_id": paperID, "status": model.PromoGrantConsumed, "consumed_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrPromoGrantInvalid
	}
	return nil
}

func (s *PromoCodeService) List(ctx context.Context, page, pageSize int, query string) ([]model.PromoCode, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	db := s.db.WithContext(ctx).Model(&model.PromoCode{})
	if query = strings.TrimSpace(query); query != "" {
		db = db.Where("campaign_name ILIKE ? OR code_mask ILIKE ?", "%"+query+"%", "%"+query+"%")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var codes []model.PromoCode
	err := db.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&codes).Error
	return codes, total, err
}

func (s *PromoCodeService) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	return s.db.WithContext(ctx).Model(&model.PromoCode{}).Where("id = ?", id).Update("is_active", active).Error
}

func (s *PromoCodeService) ListGrants(ctx context.Context, codeID uuid.UUID) ([]model.PromoCodeGrant, error) {
	var grants []model.PromoCodeGrant
	err := s.db.WithContext(ctx).Preload("User").Where("promo_code_id = ?", codeID).Order("created_at DESC").Find(&grants).Error
	return grants, err
}

func validPromoService(value string) bool {
	return value == "check_and_fix"
}

func promoCodeHash(value string) string {
	normalized := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(value)))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func generatePromoCode() (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	parts := make([]string, 4)
	for part := range parts {
		chunk := make([]byte, 4)
		for i := range chunk {
			chunk[i] = alphabet[int(b[part*4+i])%len(alphabet)]
		}
		parts[part] = string(chunk)
	}
	return "LIYI-" + strings.Join(parts, "-"), nil
}

func maskPromoCode(value string) string {
	if len(value) < 9 {
		return value
	}
	return fmt.Sprintf("%s-****-%s", value[:4], value[len(value)-4:])
}
