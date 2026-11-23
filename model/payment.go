package model

import (
	"context"
	"errors"
	"math"

	"github.com/infinmalum/a1o-platform/common/helper"
	"github.com/infinmalum/a1o-platform/common/logger"
	"gorm.io/gorm"
)

type PaymentOrder struct {
	ID          string  `json:"id" gorm:"primaryKey;size:36"`
	UserID      int     `json:"-" gorm:"index"`
	PackageName string  `json:"package_name" gorm:"size:100"`
	Provider    string  `json:"provider" gorm:"size:20;uniqueIndex:idx_payment_provider"`
	ProviderID  *string `json:"-" gorm:"size:128;uniqueIndex:idx_payment_provider"`
	CheckoutURL string  `json:"checkout_url,omitempty" gorm:"type:text"`
	Amount      int64   `json:"amount"`
	Currency    string  `json:"currency" gorm:"size:3"`
	PayCurrency string  `json:"pay_currency,omitempty" gorm:"size:32"`
	Quota       int64   `json:"quota"`
	CreatedAt   int64   `json:"created_at" gorm:"autoCreateTime"`
	State       string  `json:"state" gorm:"size:16;default:pending"`
	CreditedAt  int64   `json:"credited_at"`
}

func CreditPaymentOrder(ctx context.Context, order PaymentOrder, providerID string) error {
	if order.ID == "" || order.Quota <= 0 || providerID == "" {
		return errors.New("invalid payment order")
	}
	var credited bool
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&PaymentOrder{}).
			Where("id = ? AND provider = ? AND provider_id = ? AND credited_at = 0", order.ID, order.Provider, providerID).
			Updates(map[string]any{"credited_at": helper.GetTimestamp(), "state": "credited", "checkout_url": ""})
		if update.Error != nil || update.RowsAffected == 0 {
			return update.Error
		}
		update = tx.Model(&User{}).Where("id = ? AND quota <= ?", order.UserID, math.MaxInt64-order.Quota).
			UpdateColumn("quota", gorm.Expr("quota + ?", order.Quota))
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errors.New("payment recipient unavailable")
		}
		credited = true
		return nil
	})
	if err == nil && credited {
		ctx = context.WithoutCancel(ctx)
		if err := CacheRefreshUserQuota(ctx, order.UserID); err != nil {
			logger.Error(ctx, "refresh paid quota: "+err.Error())
		}
		RecordTopupLog(ctx, order.UserID, "Online top-up "+order.ID, int(order.Quota))
	}
	return err
}
