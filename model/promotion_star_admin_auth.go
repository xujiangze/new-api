package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrPromotionStarForbidden = errors.New("promotion star action is not permitted")

// validatePromotionStarAdminAuthorizationWithTx rechecks the administrator and
// the consumed, session-bound proof under the same locks as the requested change.
// A bearer PAT cannot become a browser session by supplying a proof.
func validatePromotionStarAdminAuthorizationWithTx(tx *gorm.DB, authorization *AuthFlowAuthorization, scope string) error {
	if authorization == nil || authorization.ProofID <= 0 || authorization.Scope != scope || authorization.ContextHash == "" {
		return ErrAuthFlowInvalid
	}
	if _, pat := ParseAccessTokenSessionID(authorization.SessionID); pat {
		return ErrPromotionStarForbidden
	}
	if err := ValidateAuthSessionWithTx(tx, authorization.AuthSessionIdentity); err != nil {
		return err
	}
	var actor User
	if err := tx.Select("id", "role", "status").First(&actor, authorization.UserID).Error; err != nil {
		return err
	}
	if actor.Role < common.RoleAdminUser || actor.Status != common.UserStatusEnabled {
		return ErrPromotionStarForbidden
	}
	var proof AuthFlow
	if err := lockForUpdate(tx).First(&proof, authorization.ProofID).Error; err != nil {
		return err
	}
	if proof.Purpose != AuthFlowPurposeSecurityProof || proof.UserId != authorization.UserID ||
		proof.SessionId != authorization.SessionID || proof.ConsumedAt == nil || proof.Payload != "" {
		return ErrAuthFlowInvalid
	}
	if !proof.ExpiresAt.After(time.Now()) {
		return ErrAuthFlowExpired
	}
	// Mark application in this transaction as well as consumption in the proof
	// framework. An internal retry must not apply the same authorization twice.
	return tx.Model(&proof).Update("payload", "promotion_star_action_applied").Error
}

// SetPromotionStarProfileForAdmin applies a manually authorized lifecycle
// change. No caller-selected operator or effective timestamp is trusted.
func SetPromotionStarProfileForAdmin(authorization *AuthFlowAuthorization, action string, input PromotionStarProfileInput) (*PromotionStarProfile, error) {
	if action != "grant" && action != "rate" && action != "suspend" {
		return nil, ErrPromotionStarInvalid
	}
	var profile *PromotionStarProfile
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := validatePromotionStarAdminAuthorizationWithTx(tx, authorization, "admin.promotion_star."+action); err != nil {
			return err
		}
		if input.UserID <= 0 {
			return ErrPromotionStarInvalid
		}
		var target User
		if err := lockForUpdate(tx).Select("id", "status").First(&target, input.UserID).Error; err != nil {
			return err
		}
		var current PromotionStarProfile
		err := lockForUpdate(tx).Where("user_id = ?", input.UserID).First(&current).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		active := err == nil && current.Status == PromotionStarStatusActive
		if action == "grant" && active || action != "grant" && !active {
			return ErrPromotionStarConflict
		}
		if action != "suspend" && target.Status != common.UserStatusEnabled {
			return ErrPromotionStarForbidden
		}
		input.Status = PromotionStarStatusActive
		if action == "suspend" {
			input.Status, input.RateBPS = PromotionStarStatusSuspended, current.RateBPS
		}
		input.OperatorID, input.EffectiveAtMS = authorization.UserID, time.Now().UnixMilli()
		profile, err = SetPromotionStarProfileWithTx(tx, input)
		if err != nil {
			return err
		}
		if action != "rate" {
			_, err = IncrementUserAuthVersionWithTx(tx, input.UserID)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if action != "rate" {
		// Auth-version fencing invalidates every old access and refresh session,
		// including the current one when an administrator changes their own flag.
		if err := PublishUserAuthCache(input.UserID); err != nil {
			return nil, err
		}
	}
	return profile, nil
}

func AdjustPromotionStarCommissionForAdmin(authorization *AuthFlowAuthorization, input PromotionStarAdjustmentInput) (*PromotionStarLedgerEntry, error) {
	var entry *PromotionStarLedgerEntry
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := validatePromotionStarAdminAuthorizationWithTx(tx, authorization, "admin.promotion_star.adjust"); err != nil {
			return err
		}
		input.OperatorID, input.OccurredAtMS = authorization.UserID, time.Now().UnixMilli()
		var err error
		entry, err = AdjustPromotionStarCommissionWithTx(tx, input)
		return err
	})
	return entry, err
}

// PromotionStarReviewCanConfirm distinguishes a final amount awaiting funding
// reconciliation from an intent or a known unreliable realtime debit total.
func PromotionStarReviewCanConfirm(state string, settledAtMS int64, reviewReason string) bool {
	return state == PromotionStarEventReview && settledAtMS > 0 &&
		!strings.Contains(reviewReason, PromotionStarReviewRealtime)
}

type PromotionStarReviewInput struct {
	EventID             int64
	EventKey            string
	State               string
	Reason              string
	ExpectedUpdatedAtMS int64
	ExpectedQuota       string
	ExpectedSettledAtMS int64
}

// ResolvePromotionStarReviewForAdmin changes reconciliation state only; the
// recorded consumption and settlement instant are never editable through review.
func ResolvePromotionStarReviewForAdmin(authorization *AuthFlowAuthorization, input PromotionStarReviewInput) error {
	if input.State != PromotionStarEventPending && input.State != PromotionStarEventIgnored {
		return ErrPromotionStarInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := validatePromotionStarAdminAuthorizationWithTx(tx, authorization, "admin.promotion_star.review"); err != nil {
			return err
		}
		var event PromotionStarEvent
		if err := lockForUpdate(tx).First(&event, input.EventID).Error; err != nil {
			return err
		}
		if event.EventKey != input.EventKey || event.State != PromotionStarEventReview && event.State != PromotionStarEventProvisional ||
			event.UpdatedAtMS != input.ExpectedUpdatedAtMS || event.Quota != input.ExpectedQuota || event.SettledAtMS != input.ExpectedSettledAtMS {
			return ErrPromotionStarConflict
		}
		if input.State == PromotionStarEventPending && (!PromotionStarReviewCanConfirm(event.State, event.SettledAtMS, event.ReviewReason) ||
			strings.Contains(event.InternalEvidence, PromotionStarReviewRealtime)) {
			return ErrPromotionStarInvalid
		}
		evidence := event.InternalEvidence + fmt.Sprintf("\nAdministrator %d resolved review as %s: %s", authorization.UserID, input.State, input.Reason)
		_, err := FinalizePromotionStarEvent(tx, event.EventKey, PromotionStarFinalizeInput{
			Quota: event.Quota, SettledAtMS: event.SettledAtMS, State: input.State, InternalEvidence: evidence,
		})
		return err
	})
}
