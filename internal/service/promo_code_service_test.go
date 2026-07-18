package service

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

func TestPromoCodeRedeemHonorsLimitAndExpiry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE users (id text primary key)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.PromoCode{}, &model.PromoCodeGrant{}); err != nil {
		t.Fatal(err)
	}
	svc := NewPromoCodeService(db)
	adminID, userID := uuid.New(), uuid.New()

	_, plain, err := svc.Generate(context.Background(), adminID, GeneratePromoCodesInput{
		CampaignName: "新用户体验", Quantity: 1, ServiceType: "check_and_fix", MaxUses: 1, PerUserLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, remaining, err := svc.Redeem(context.Background(), userID, plain[0], "check_and_fix")
	if err != nil || remaining != 0 {
		t.Fatalf("redeem = %v, remaining = %d", err, remaining)
	}
	if _, _, err := svc.Redeem(context.Background(), uuid.New(), plain[0], "check_and_fix"); err == nil {
		t.Fatal("exhausted code was accepted")
	}
	if err := svc.BindGrant(context.Background(), grant.ID, userID, uuid.New()); err != nil {
		t.Fatal(err)
	}

	expired := time.Now().Add(-time.Minute)
	_, expiredCodes, err := svc.Generate(context.Background(), adminID, GeneratePromoCodesInput{
		CampaignName: "expired", Quantity: 1, ServiceType: "format_check", MaxUses: 1, PerUserLimit: 1, ExpiresAt: &expired,
	})
	if err == nil || len(expiredCodes) != 0 {
		t.Fatal("expired generation should fail")
	}
}
