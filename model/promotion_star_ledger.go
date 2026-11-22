package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var promotionStarLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

// ProcessPromotionStarEvents is safe to call from multiple workers. Each event,
// ledger append and daily update commits as one primary-database transaction.
func ProcessPromotionStarEvents(limit int) error {
	if limit <= 0 || limit > 10000 {
		return ErrPromotionStarInvalid
	}
	var ids []int64
	if err := DB.Model(&PromotionStarEvent{}).Where("state = ?", PromotionStarEventPending).Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := promotionStarTransaction(nil, func(tx *gorm.DB) error {
			var event PromotionStarEvent
			if err := lockForUpdate(tx).First(&event, id).Error; err != nil {
				return err
			}
			if event.State != PromotionStarEventPending {
				return nil
			}
			basis, err := decimal.NewFromString(event.Quota)
			if err != nil {
				return err
			}
			if event.Kind == PromotionStarKindRefund {
				basis = basis.Neg()
			}
			entry := PromotionStarLedgerEntry{
				EntryKeyHash: promotionStarKeyHash("event:" + event.EventKey), EventID: &event.ID, EventKey: event.EventKey,
				UserID: event.PromoterUserID, InviteeUserID: event.UserID,
				Day:    time.UnixMilli(event.SettledAtMS).In(promotionStarLocation).Format("2006-01-02"),
				Source: event.Source, Kind: event.Kind, RuleID: event.RuleID, RateBPS: event.RateBPS,
				BasisQuota: basis.String(), CommissionQuota: event.CommissionQuota,
				OccurredAtMS: event.SettledAtMS, CreatedAtMS: time.Now().UnixMilli(),
			}
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
			if err := promotionStarAccumulateDaily(tx, &entry); err != nil {
				return err
			}
			return tx.Model(&PromotionStarEvent{}).Where("id = ?", event.ID).Updates(map[string]any{
				"state": PromotionStarEventProcessed, "processed_at_ms": time.Now().UnixMilli(),
			}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

// promotionStarAccumulateDaily adds exact decimal strings while holding the
// bucket row lock; SQL SUM or SQL text arithmetic would lose precision in SQLite.
func promotionStarAccumulateDaily(tx *gorm.DB, entry *PromotionStarLedgerEntry) error {
	bucketKey := fmt.Sprintf("%d|%d|%s|%s|%s|%d|%s", entry.UserID, entry.InviteeUserID, entry.Day, entry.Source, entry.Kind, entry.RuleID, entry.PublicReason)
	daily := PromotionStarDaily{
		BucketHash: promotionStarKeyHash(bucketKey), UserID: entry.UserID, InviteeUserID: entry.InviteeUserID,
		Day: entry.Day, Source: entry.Source, Kind: entry.Kind, RuleID: entry.RuleID, RateBPS: entry.RateBPS,
		BasisQuota: "0", CommissionQuota: "0", PublicReason: entry.PublicReason,
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&daily).Error; err != nil {
		return err
	}
	if err := lockForUpdate(tx).Where("bucket_hash = ?", daily.BucketHash).First(&daily).Error; err != nil {
		return err
	}
	basis, err := decimal.NewFromString(daily.BasisQuota)
	if err != nil {
		return err
	}
	commission, err := decimal.NewFromString(daily.CommissionQuota)
	if err != nil {
		return err
	}
	deltaBasis, err := decimal.NewFromString(entry.BasisQuota)
	if err != nil {
		return err
	}
	deltaCommission, err := decimal.NewFromString(entry.CommissionQuota)
	if err != nil {
		return err
	}
	return tx.Model(&PromotionStarDaily{}).Where("id = ?", daily.ID).Updates(map[string]any{
		"basis_quota": basis.Add(deltaBasis).String(), "commission_quota": commission.Add(deltaCommission).String(),
	}).Error
}

func AdjustPromotionStarCommission(input PromotionStarAdjustmentInput) (*PromotionStarLedgerEntry, error) {
	return AdjustPromotionStarCommissionWithTx(nil, input)
}

func AdjustPromotionStarCommissionWithTx(tx *gorm.DB, input PromotionStarAdjustmentInput) (*PromotionStarLedgerEntry, error) {
	amount, err := promotionStarQuota(input.CommissionQuota, true)
	if err != nil || amount.IsZero() || input.UserID <= 0 || input.OperatorID <= 0 || input.OccurredAtMS <= 0 ||
		input.EventKey == "" || len(input.EventKey) > 128 || strings.TrimSpace(input.EventKey) != input.EventKey ||
		strings.TrimSpace(input.PublicReason) == "" || len(input.PublicReason) > 2000 ||
		strings.TrimSpace(input.InternalEvidence) == "" || len(input.InternalEvidence) > 8000 {
		return nil, ErrPromotionStarInvalid
	}
	var result PromotionStarLedgerEntry
	err = promotionStarTransaction(tx, func(tx *gorm.DB) error {
		// A profile lock serializes adjustment retries and creation, including on
		// databases whose default isolation would retain an earlier read snapshot.
		var profile PromotionStarProfile
		if err := lockForUpdate(tx).Where("user_id = ?", input.UserID).First(&profile).Error; err != nil {
			return err
		}
		keyHash := promotionStarKeyHash("adjustment:" + input.EventKey)
		var existing PromotionStarLedgerEntry
		err := lockForUpdate(tx).Where("entry_key_hash = ?", keyHash).First(&existing).Error
		if err == nil {
			if existing.EventKey != input.EventKey || existing.UserID != input.UserID || existing.CommissionQuota != amount.String() ||
				existing.PublicReason != input.PublicReason || existing.InternalEvidence != input.InternalEvidence || existing.OperatorID != input.OperatorID {
				return ErrPromotionStarConflict
			}
			// The first occurrence time wins when an HTTP retry supplies a new now.
			result = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if profile.FirstActivatedAtMS == 0 || input.OccurredAtMS < profile.FirstActivatedAtMS {
			return fmt.Errorf("%w: adjustment predates activation", ErrPromotionStarInvalid)
		}
		entry := PromotionStarLedgerEntry{
			EntryKeyHash: keyHash, EventKey: input.EventKey, UserID: input.UserID,
			Day:    time.UnixMilli(input.OccurredAtMS).In(promotionStarLocation).Format("2006-01-02"),
			Source: PromotionStarSourceAdjustment, Kind: PromotionStarKindAdjustment,
			BasisQuota: "0", CommissionQuota: amount.String(), PublicReason: input.PublicReason,
			InternalEvidence: input.InternalEvidence, OperatorID: input.OperatorID,
			OccurredAtMS: input.OccurredAtMS, CreatedAtMS: time.Now().UnixMilli(),
		}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		if err := promotionStarAccumulateDaily(tx, &entry); err != nil {
			return err
		}
		result = entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
