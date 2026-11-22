package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const PromotionStarReviewRealtime = "Realtime wallet debits require reconciliation"

func promotionStarKeyHash(key string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
}

// promotionStarQuota rejects exponents and oversized coefficients before decimal
// parsing. Wallet facts are integral quota; commission adjustments have at most
// four places, exactly matching basis-point precision without rounding.
func promotionStarQuota(raw string, adjustment bool) (decimal.Decimal, error) {
	if raw == "" || len(raw) > 32 {
		return decimal.Zero, ErrPromotionStarInvalid
	}
	unsigned := raw
	if adjustment {
		unsigned = strings.TrimPrefix(unsigned, "-")
	}
	whole, fraction, hasFraction := strings.Cut(unsigned, ".")
	if whole == "" || (hasFraction && (!adjustment || fraction == "" || len(fraction) > 4)) {
		return decimal.Zero, ErrPromotionStarInvalid
	}
	for _, char := range whole + fraction {
		if char < '0' || char > '9' {
			return decimal.Zero, ErrPromotionStarInvalid
		}
	}
	amount, err := decimal.NewFromString(raw)
	if err != nil || amount.Abs().GreaterThan(decimal.NewFromInt(common.MaxWalletQuota)) || (!adjustment && amount.IsNegative()) {
		return decimal.Zero, ErrPromotionStarInvalid
	}
	return amount, nil
}

func GetPromotionStarEvent(eventKey string) (*PromotionStarEvent, error) {
	var event PromotionStarEvent
	if err := DB.Where("key_hash = ?", promotionStarKeyHash(eventKey)).First(&event).Error; err != nil {
		return nil, err
	}
	if event.EventKey != eventKey {
		return nil, ErrPromotionStarConflict
	}
	return &event, nil
}

func GetPromotionStarEventByID(id int64) (*PromotionStarEvent, error) {
	var event PromotionStarEvent
	if err := DB.First(&event, id).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

// RecordPromotionStarEvent captures the immutable direct inviter before funding.
// A supplied primary transaction joins the host debit. nil starts a model-only
// transaction and never retries or performs a wallet operation.
func RecordPromotionStarEvent(tx *gorm.DB, input PromotionStarEventInput) (*PromotionStarEvent, error) {
	if input.EventKey == "" || len(input.EventKey) > 128 || strings.TrimSpace(input.EventKey) != input.EventKey ||
		input.UserID <= 0 || len(input.InternalEvidence) > 8000 ||
		(input.Kind != PromotionStarKindConsumption && input.Kind != PromotionStarKindRefund) ||
		(input.Source != PromotionStarSourceWalletUsage && input.Source != PromotionStarSourceSubscriptionBalance) ||
		(input.State != PromotionStarEventProvisional && input.State != PromotionStarEventPending && input.State != PromotionStarEventReview && input.State != PromotionStarEventIgnored) ||
		(input.Kind == PromotionStarKindConsumption && input.OriginalEventKey != "") ||
		(input.Kind == PromotionStarKindRefund && (input.OriginalEventKey == "" || input.OriginalEventKey == input.EventKey || len(input.OriginalEventKey) > 128)) {
		return nil, ErrPromotionStarInvalid
	}
	amount, err := promotionStarQuota(input.Quota, false)
	if err != nil || input.SettledAtMS < 0 || (input.State == PromotionStarEventPending && input.SettledAtMS == 0) {
		return nil, ErrPromotionStarInvalid
	}
	input.Quota = amount.String()
	var result *PromotionStarEvent
	err = promotionStarTransaction(tx, func(tx *gorm.DB) error {
		result = nil
		var existing PromotionStarEvent
		err := lockForUpdate(tx).Where("key_hash = ?", promotionStarKeyHash(input.EventKey)).First(&existing).Error
		if err == nil {
			if err := promotionStarMatchEvent(existing, input); err != nil {
				return err
			}
			result = &existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var inviterID int
		var originalID int64
		if input.Kind == PromotionStarKindConsumption {
			var user User
			if err := tx.Select("id", "inviter_id").First(&user, input.UserID).Error; err != nil {
				return err
			}
			if user.InviterId <= 0 || user.InviterId == user.Id {
				return nil
			}
			inviterID = user.InviterId
		} else {
			var original PromotionStarEvent
			if err := tx.Where("key_hash = ?", promotionStarKeyHash(input.OriginalEventKey)).First(&original).Error; err != nil {
				return err
			}
			if original.EventKey != input.OriginalEventKey || original.Kind != PromotionStarKindConsumption || original.UserID != input.UserID || original.Source != input.Source {
				return ErrPromotionStarInvalid
			}
			inviterID, originalID = original.PromoterUserID, original.ID
		}
		now := time.Now().UnixMilli()
		event := PromotionStarEvent{
			KeyHash: promotionStarKeyHash(input.EventKey), EventKey: input.EventKey,
			Kind: input.Kind, Source: input.Source, UserID: input.UserID, PromoterUserID: inviterID,
			OriginalEventID: originalID, OriginalEventKey: input.OriginalEventKey,
			State: PromotionStarEventProvisional, Quota: input.Quota, CommissionQuota: "0",
			RefundedQuota: "0", ReversedCommissionQuota: "0", ReservationQuota: "0", SettledAtMS: input.SettledAtMS,
			InternalEvidence: input.InternalEvidence, CreatedAtMS: now, UpdatedAtMS: now,
		}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected == 0 {
			if err := lockForUpdate(tx).Where("key_hash = ?", event.KeyHash).First(&existing).Error; err != nil {
				return err
			}
			if err := promotionStarMatchEvent(existing, input); err != nil {
				return err
			}
			result = &existing
			return nil
		}
		if input.State != PromotionStarEventProvisional {
			if err := promotionStarFinalize(tx, &event, PromotionStarFinalizeInput{Quota: input.Quota, SettledAtMS: input.SettledAtMS, State: input.State, InternalEvidence: input.InternalEvidence}); err != nil {
				return err
			}
		}
		result = &event
		return nil
	})
	return result, err
}

func promotionStarMatchEvent(existing PromotionStarEvent, input PromotionStarEventInput) error {
	if existing.EventKey != input.EventKey || existing.Kind != input.Kind || existing.Source != input.Source ||
		existing.UserID != input.UserID || existing.OriginalEventKey != input.OriginalEventKey {
		return ErrPromotionStarConflict
	}
	// Retry of an intent may observe its later finalized version. It cannot
	// reset the settlement amount or timestamp back to the provisional values.
	if input.State == PromotionStarEventProvisional {
		return nil
	}
	if existing.Quota != input.Quota || existing.SettledAtMS != input.SettledAtMS ||
		(existing.FinalizedState != input.State && existing.State != input.State) {
		return ErrPromotionStarConflict
	}
	return nil
}

func FinalizePromotionStarEvent(tx *gorm.DB, eventKey string, input PromotionStarFinalizeInput) (*PromotionStarEvent, error) {
	var result PromotionStarEvent
	err := promotionStarTransaction(tx, func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("key_hash = ?", promotionStarKeyHash(eventKey)).First(&result).Error; err != nil {
			return err
		}
		if result.EventKey != eventKey {
			return ErrPromotionStarConflict
		}
		return promotionStarFinalize(tx, &result, input)
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func UpdatePromotionStarEventState(tx *gorm.DB, eventKey, state, evidence string) error {
	return promotionStarTransaction(tx, func(tx *gorm.DB) error {
		var event PromotionStarEvent
		if err := lockForUpdate(tx).Where("key_hash = ?", promotionStarKeyHash(eventKey)).First(&event).Error; err != nil {
			return err
		}
		if event.EventKey != eventKey {
			return ErrPromotionStarConflict
		}
		return promotionStarFinalize(tx, &event, PromotionStarFinalizeInput{Quota: event.Quota, SettledAtMS: event.SettledAtMS, State: state, InternalEvidence: evidence})
	})
}

// promotionStarFinalize resolves final eligibility exactly once. Processing later
// only applies this snapshot, even after suspension, rule changes or user deletion.
func promotionStarFinalize(tx *gorm.DB, event *PromotionStarEvent, input PromotionStarFinalizeInput) error {
	amount, err := promotionStarQuota(input.Quota, false)
	if err != nil || input.SettledAtMS < 0 || len(input.InternalEvidence) > 8000 ||
		(input.State != PromotionStarEventPending && input.State != PromotionStarEventReview && input.State != PromotionStarEventIgnored) ||
		(input.State == PromotionStarEventPending && input.SettledAtMS == 0) {
		return ErrPromotionStarInvalid
	}
	if event.State != PromotionStarEventProvisional && event.State != PromotionStarEventReview {
		if event.Quota == amount.String() && event.SettledAtMS == input.SettledAtMS && event.FinalizedState == input.State {
			return nil
		}
		return ErrPromotionStarConflict
	}
	settlementCaptured := event.FinalizedState == PromotionStarEventReview && event.SettledAtMS > 0
	if settlementCaptured && (event.Quota != amount.String() || event.SettledAtMS != input.SettledAtMS) {
		return ErrPromotionStarConflict
	}
	if !settlementCaptured && event.Kind == PromotionStarKindConsumption && input.SettledAtMS > 0 &&
		(input.State == PromotionStarEventPending || input.State == PromotionStarEventReview) {
		// Capture eligibility at the actual settlement boundary, including when
		// funding needs review. Later reconciliation must not consult live user
		// status or replace this rule. A zero RuleID is an ineligible snapshot;
		// a valid zero-percent rate still retains its nonzero RuleID.
		var promoterCount int64
		if err := tx.Model(&User{}).Where("id = ? AND status = ?", event.PromoterUserID, common.UserStatusEnabled).Count(&promoterCount).Error; err != nil {
			return err
		}
		var profile PromotionStarProfile
		err := lockForUpdate(tx).Where("user_id = ?", event.PromoterUserID).First(&profile).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var rule PromotionStarRule
		if err == nil {
			err = tx.Where("user_id = ? AND effective_at_ms <= ?", event.PromoterUserID, input.SettledAtMS).Order("effective_at_ms DESC, id DESC").First(&rule).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		event.RuleID, event.RateBPS = 0, 0
		if err == nil && rule.Status == PromotionStarStatusActive && promoterCount > 0 {
			event.RuleID, event.RateBPS = rule.ID, rule.RateBPS
		}
	}
	event.Quota, event.SettledAtMS, event.State = amount.String(), input.SettledAtMS, input.State
	event.InternalEvidence, event.UpdatedAtMS = input.InternalEvidence, time.Now().UnixMilli()
	event.CommissionQuota, event.ReviewReason = "0", ""
	event.FinalizedState = input.State
	if input.State == PromotionStarEventReview {
		// Reservation/crash review has no final settlement yet. Once a real
		// settlement is captured, this marker freezes both facts and eligibility.
		if input.SettledAtMS == 0 {
			event.FinalizedState = ""
		}
		event.ReviewReason = "Funding requires reconciliation"
		if input.InternalEvidence == PromotionStarReviewRealtime {
			event.ReviewReason = PromotionStarReviewRealtime
		}
	}
	if input.State == PromotionStarEventPending {
		if event.Kind == PromotionStarKindRefund {
			var original PromotionStarEvent
			if err := lockForUpdate(tx).First(&original, event.OriginalEventID).Error; err != nil {
				return err
			}
			if original.State != PromotionStarEventPending && original.State != PromotionStarEventProcessed && original.State != PromotionStarEventIgnored {
				return ErrPromotionStarOriginalUnsettled
			}
			if input.SettledAtMS < original.SettledAtMS {
				return ErrPromotionStarInvalid
			}
			basis, err := decimal.NewFromString(original.Quota)
			if err != nil {
				return err
			}
			refunded, err := decimal.NewFromString(original.RefundedQuota)
			if err != nil {
				return err
			}
			refunded = refunded.Add(amount)
			if refunded.GreaterThan(basis) {
				return ErrPromotionStarRefundExceeded
			}
			event.RuleID, event.RateBPS = original.RuleID, original.RateBPS
			reversal := amount.Mul(decimal.NewFromInt(int64(original.RateBPS))).Shift(-4)
			priorReversed, err := decimal.NewFromString(original.ReversedCommissionQuota)
			if err != nil {
				return err
			}
			if refunded.Equal(basis) {
				commission, err := decimal.NewFromString(original.CommissionQuota)
				if err != nil {
					return err
				}
				reversal = commission.Sub(priorReversed)
			}
			event.CommissionQuota = reversal.Neg().String()
			if original.State == PromotionStarEventIgnored || amount.IsZero() {
				event.State = PromotionStarEventIgnored
				event.CommissionQuota = "0"
			}
			if err := tx.Model(&PromotionStarEvent{}).Where("id = ?", original.ID).Updates(map[string]any{
				"refunded_quota": refunded.String(), "reversed_commission_quota": priorReversed.Add(reversal).String(),
			}).Error; err != nil {
				return err
			}
		} else {
			if event.RuleID == 0 || amount.IsZero() {
				event.State = PromotionStarEventIgnored
			} else {
				event.CommissionQuota = amount.Mul(decimal.NewFromInt(int64(event.RateBPS))).Shift(-4).String()
			}
		}
	}
	return tx.Model(&PromotionStarEvent{}).Where("id = ?", event.ID).Updates(map[string]any{
		"quota": event.Quota, "settled_at_ms": event.SettledAtMS, "state": event.State,
		"finalized_state": event.FinalizedState, "commission_quota": event.CommissionQuota,
		"rule_id": event.RuleID, "rate_bps": event.RateBPS, "review_reason": event.ReviewReason,
		"internal_evidence": event.InternalEvidence, "updated_at_ms": event.UpdatedAtMS,
	}).Error
}
