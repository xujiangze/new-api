package service

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

const (
	VerificationScopePromotionStarGrant   = "admin.promotion_star.grant"
	VerificationScopePromotionStarRate    = "admin.promotion_star.rate"
	VerificationScopePromotionStarSuspend = "admin.promotion_star.suspend"
	VerificationScopePromotionStarAdjust  = "admin.promotion_star.adjust"
	VerificationScopePromotionStarReview  = "admin.promotion_star.review"
)

// Promotion Star proofs name the managed account and every mutable input. The
// effective time and operator are always assigned by the server, never the client.
type PromotionStarRateContext struct {
	UserID  int    `json:"user_id"`
	RateBPS int    `json:"rate_bps"`
	Reason  string `json:"reason"`
}

type PromotionStarSuspendContext struct {
	UserID int    `json:"user_id"`
	Reason string `json:"reason"`
}

type PromotionStarAdjustmentContext struct {
	UserID          int    `json:"user_id"`
	CommissionQuota string `json:"commission_quota"`
	Reason          string `json:"reason"`
	Evidence        string `json:"evidence"`
	IdempotencyKey  string `json:"idempotency_key"`
}

type PromotionStarReviewContext struct {
	EventKey            string `json:"event_key"`
	State               string `json:"state"`
	Reason              string `json:"reason"`
	ExpectedUpdatedAtMS int64  `json:"expected_updated_at_ms"`
	ExpectedQuota       string `json:"expected_quota"`
	ExpectedSettledAtMS int64  `json:"expected_settled_at_ms"`
}

func isPromotionStarVerificationScope(scope string) bool {
	switch scope {
	case VerificationScopePromotionStarGrant, VerificationScopePromotionStarRate,
		VerificationScopePromotionStarSuspend, VerificationScopePromotionStarAdjust, VerificationScopePromotionStarReview:
		return true
	default:
		return false
	}
}

func promotionStarVerificationContext(scope string, fields map[string]json.RawMessage) (any, error) {
	if scope == VerificationScopePromotionStarReview {
		var context PromotionStarReviewContext
		if len(fields) != 6 || common.GetJsonType(fields["event_key"]) != "string" ||
			common.GetJsonType(fields["state"]) != "string" || common.GetJsonType(fields["reason"]) != "string" ||
			common.GetJsonType(fields["expected_updated_at_ms"]) != "number" || common.GetJsonType(fields["expected_settled_at_ms"]) != "number" ||
			common.GetJsonType(fields["expected_quota"]) != "string" ||
			common.Unmarshal(fields["event_key"], &context.EventKey) != nil ||
			common.Unmarshal(fields["state"], &context.State) != nil ||
			common.Unmarshal(fields["reason"], &context.Reason) != nil ||
			common.Unmarshal(fields["expected_updated_at_ms"], &context.ExpectedUpdatedAtMS) != nil ||
			common.Unmarshal(fields["expected_settled_at_ms"], &context.ExpectedSettledAtMS) != nil ||
			common.Unmarshal(fields["expected_quota"], &context.ExpectedQuota) != nil ||
			context.ExpectedUpdatedAtMS <= 0 || context.ExpectedSettledAtMS < 0 || context.ExpectedQuota == "" || len(context.ExpectedQuota) > 32 ||
			strings.TrimSpace(context.EventKey) == "" || len(context.EventKey) > 128 ||
			(context.State != "pending" && context.State != "ignored") ||
			strings.TrimSpace(context.Reason) == "" || utf8.RuneCountInString(context.Reason) > 500 {
			return nil, ErrVerificationContextInvalid
		}
		return context, nil
	}
	required := []string{"user_id", "reason"}
	switch scope {
	case VerificationScopePromotionStarGrant, VerificationScopePromotionStarRate:
		required = append(required, "rate_bps")
	case VerificationScopePromotionStarAdjust:
		required = append(required, "commission_quota", "evidence", "idempotency_key")
	case VerificationScopePromotionStarSuspend:
	default:
		return nil, ErrProofScope
	}
	if len(fields) != len(required) {
		return nil, ErrVerificationContextInvalid
	}
	for _, key := range required {
		expectedType := "string"
		if key == "user_id" || key == "rate_bps" {
			expectedType = "number"
		}
		if common.GetJsonType(fields[key]) != expectedType {
			return nil, ErrVerificationContextInvalid
		}
	}
	var userID int
	var reason string
	if common.Unmarshal(fields["user_id"], &userID) != nil || userID <= 0 ||
		common.Unmarshal(fields["reason"], &reason) != nil || strings.TrimSpace(reason) == "" || utf8.RuneCountInString(reason) > 500 {
		return nil, ErrVerificationContextInvalid
	}
	switch scope {
	case VerificationScopePromotionStarGrant, VerificationScopePromotionStarRate:
		var rateBPS int
		if common.Unmarshal(fields["rate_bps"], &rateBPS) != nil || rateBPS < 0 || rateBPS > 10000 {
			return nil, ErrVerificationContextInvalid
		}
		return PromotionStarRateContext{UserID: userID, RateBPS: rateBPS, Reason: reason}, nil
	case VerificationScopePromotionStarSuspend:
		return PromotionStarSuspendContext{UserID: userID, Reason: reason}, nil
	default:
		context := PromotionStarAdjustmentContext{UserID: userID, Reason: reason}
		if common.Unmarshal(fields["commission_quota"], &context.CommissionQuota) != nil ||
			common.Unmarshal(fields["evidence"], &context.Evidence) != nil ||
			common.Unmarshal(fields["idempotency_key"], &context.IdempotencyKey) != nil ||
			strings.TrimSpace(context.CommissionQuota) == "" || len(context.CommissionQuota) > 32 ||
			strings.TrimSpace(context.Evidence) == "" || utf8.RuneCountInString(context.Evidence) > 2000 ||
			strings.TrimSpace(context.IdempotencyKey) == "" || len(context.IdempotencyKey) > 128 {
			return nil, ErrVerificationContextInvalid
		}
		whole, fraction, fractional := strings.Cut(strings.TrimPrefix(context.CommissionQuota, "-"), ".")
		if whole == "" || fractional && (fraction == "" || len(fraction) > 4) {
			return nil, ErrVerificationContextInvalid
		}
		for _, digit := range whole + fraction {
			if digit < '0' || digit > '9' {
				return nil, ErrVerificationContextInvalid
			}
		}
		amount, err := decimal.NewFromString(context.CommissionQuota)
		if err != nil || amount.IsZero() || amount.Abs().GreaterThan(decimal.NewFromInt(common.MaxWalletQuota)) {
			return nil, ErrVerificationContextInvalid
		}
		return context, nil
	}
}
