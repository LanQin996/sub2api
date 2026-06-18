package service

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type RedeemCode struct {
	ID                  int64
	Code                string
	Type                string
	Value               float64
	Status              string
	MaxRedemptions      int
	RedeemedCount       int
	PerUserLimit        bool
	RandomAmountEnabled bool
	RandomMinValue      float64
	RandomMaxValue      float64
	UsedBy              *int64
	CreatedBy           *int64
	UsedAt              *time.Time
	Notes               string
	CreatedAt           time.Time
	ExpiresAt           *time.Time

	GroupID      *int64
	ValidityDays int

	User  *User
	Group *Group
}

type RedeemCodeUsage struct {
	ID           int64
	RedeemCodeID int64
	UserID       int64
	Value        float64
	CreatedAt    time.Time

	RedeemCode *RedeemCode
	User       *User
}

func (r *RedeemCode) IsUsed() bool {
	return r.Status == StatusUsed
}

func (r *RedeemCode) IsExpired() bool {
	return r.IsExpiredAt(time.Now())
}

func (r *RedeemCode) IsExpiredAt(now time.Time) bool {
	if r == nil {
		return false
	}
	if r.Status == StatusExpired {
		return true
	}
	return r.Status == StatusUnused && r.ExpiresAt != nil && !r.ExpiresAt.After(now)
}

func (r *RedeemCode) CanUse() bool {
	return r.Status == StatusUnused && !r.IsExpired() && r.HasRemainingRedemptions()
}

func (r *RedeemCode) HasRemainingRedemptions() bool {
	if r == nil {
		return false
	}
	maxRedemptions := r.MaxRedemptions
	if maxRedemptions <= 0 {
		maxRedemptions = 1
	}
	return r.RedeemedCount < maxRedemptions
}

func (r *RedeemCode) IsMultiUse() bool {
	return r != nil && r.MaxRedemptions > 1
}

func GenerateRedeemCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
