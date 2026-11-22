package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// RecordPromotionStarReservation keeps an asynchronous task's applied funding
// separate from its final consumption. An uncertain reservation requires review.
func RecordPromotionStarReservation(tx *gorm.DB, eventKey, quota string, durable bool) error {
	amount, err := promotionStarQuota(quota, false)
	if err != nil {
		return err
	}
	return promotionStarTransaction(tx, func(db *gorm.DB) error {
		var event PromotionStarEvent
		if err := lockForUpdate(db).Where("key_hash = ?", promotionStarKeyHash(eventKey)).First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if event.EventKey != eventKey {
			return ErrPromotionStarConflict
		}
		if event.State != PromotionStarEventProvisional {
			return nil
		}
		updates := map[string]any{
			"reservation_confirmed": durable,
			"reservation_quota":     amount.String(),
			"updated_at_ms":         time.Now().UnixMilli(),
		}
		if !durable {
			updates["state"] = PromotionStarEventReview
			updates["review_reason"] = "Wallet reservation requires reconciliation"
		}
		return db.Model(&PromotionStarEvent{}).Where("id = ?", event.ID).Updates(updates).Error
	})
}
