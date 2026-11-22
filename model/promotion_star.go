package model

import (
	"errors"
	"sync/atomic"

	"gorm.io/gorm"
)

const (
	PromotionStarStatusActive    = "active"
	PromotionStarStatusSuspended = "suspended"

	PromotionStarKindConsumption = "consumption"
	PromotionStarKindRefund      = "refund"
	PromotionStarKindAdjustment  = "adjustment"

	PromotionStarSourceWalletUsage         = "wallet_usage"
	PromotionStarSourceSubscriptionBalance = "subscription_balance"
	PromotionStarSourceAdjustment          = "manual_adjustment"

	PromotionStarEventProvisional = "provisional"
	PromotionStarEventPending     = "pending"
	PromotionStarEventReview      = "review"
	PromotionStarEventIgnored     = "ignored"
	PromotionStarEventProcessed   = "processed"
)

var (
	ErrPromotionStarInvalid           = errors.New("invalid promotion star input")
	ErrPromotionStarConflict          = errors.New("promotion star idempotency conflict")
	ErrPromotionStarRefundExceeded    = errors.New("promotion star refund exceeds original consumption")
	ErrPromotionStarOriginalUnsettled = errors.New("original promotion star consumption is not finalized")
	ErrPromotionStarLedgerImmutable   = errors.New("promotion star ledger is append only")
	promotionStarReadyDB              atomic.Pointer[gorm.DB]
)

// PromotionStarProfile is independent of user roles, wallets and request logs.
// Rules, including suspension, are retained separately rather than overwritten.
type PromotionStarProfile struct {
	UserID             int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Status             string `json:"status" gorm:"type:varchar(16);not null"`
	RateBPS            int    `json:"rate_bps" gorm:"not null"`
	CurrentRuleID      int64  `json:"current_rule_id" gorm:"not null"`
	FirstActivatedAtMS int64  `json:"first_activated_at_ms" gorm:"not null"`
	CreatedAtMS        int64  `json:"created_at_ms" gorm:"not null"`
	UpdatedAtMS        int64  `json:"updated_at_ms" gorm:"not null"`
}

type PromotionStarRule struct {
	ID            int64  `json:"id" gorm:"primaryKey"`
	UserID        int    `json:"user_id" gorm:"not null;index:,composite:timeline,priority:1"`
	Status        string `json:"status" gorm:"type:varchar(16);not null"`
	RateBPS       int    `json:"rate_bps" gorm:"not null"`
	EffectiveAtMS int64  `json:"effective_at_ms" gorm:"not null;index:,composite:timeline,priority:2"`
	OperatorID    int    `json:"operator_id" gorm:"not null"`
	Reason        string `json:"reason" gorm:"type:text;not null"`
	CreatedAtMS   int64  `json:"created_at_ms" gorm:"not null"`
}

type PromotionStarProfileInput struct {
	UserID        int
	Status        string
	RateBPS       int
	EffectiveAtMS int64
	OperatorID    int
	Reason        string
}

// PromotionStarEvent is the primary-database outbox, not a request-log hook.
// Quota strings use TEXT so SQLite cannot coerce exact arithmetic to floats.
// An inviter is captured before funding; eligibility is resolved on finalization.
type PromotionStarEvent struct {
	ID                      int64  `json:"-" gorm:"primaryKey"`
	KeyHash                 string `json:"-" gorm:"type:char(64);not null;uniqueIndex"`
	EventKey                string `json:"-" gorm:"type:varchar(128);not null"`
	Kind                    string `json:"-" gorm:"type:varchar(16);not null"`
	Source                  string `json:"-" gorm:"type:varchar(32);not null"`
	UserID                  int    `json:"-" gorm:"not null;index"`
	PromoterUserID          int    `json:"-" gorm:"not null;index"`
	OriginalEventID         int64  `json:"-" gorm:"not null;index"`
	OriginalEventKey        string `json:"-" gorm:"type:varchar(128);not null"`
	State                   string `json:"-" gorm:"type:varchar(16);not null;index:,composite:work,priority:1"`
	FinalizedState          string `json:"-" gorm:"type:varchar(16);not null"`
	ReservationConfirmed    bool   `json:"-" gorm:"not null"`
	ReservationQuota        string `json:"-" gorm:"type:text;not null"`
	Quota                   string `json:"-" gorm:"type:text;not null"`
	CommissionQuota         string `json:"-" gorm:"type:text;not null"`
	RefundedQuota           string `json:"-" gorm:"type:text;not null"`
	ReversedCommissionQuota string `json:"-" gorm:"type:text;not null"`
	SettledAtMS             int64  `json:"-" gorm:"not null"`
	RuleID                  int64  `json:"-" gorm:"not null"`
	RateBPS                 int    `json:"-" gorm:"not null"`
	InternalEvidence        string `json:"-" gorm:"type:text;not null"`
	ReviewReason            string `json:"-" gorm:"type:text;not null"`
	CreatedAtMS             int64  `json:"-" gorm:"not null"`
	UpdatedAtMS             int64  `json:"-" gorm:"not null"`
	ProcessedAtMS           int64  `json:"-" gorm:"not null"`
}

type PromotionStarEventInput struct {
	EventKey         string
	Kind             string
	Source           string
	UserID           int
	Quota            string
	SettledAtMS      int64
	OriginalEventKey string
	State            string
	InternalEvidence string
}

type PromotionStarFinalizeInput struct {
	Quota            string
	SettledAtMS      int64
	State            string
	InternalEvidence string
}

// PromotionStarLedgerEntry is append-only accounting evidence. It is never
// returned by promoter endpoints, which read independent daily aggregates.
type PromotionStarLedgerEntry struct {
	ID               int64  `json:"-" gorm:"primaryKey"`
	EntryKeyHash     string `json:"-" gorm:"type:char(64);not null;uniqueIndex"`
	EventID          *int64 `json:"-" gorm:"uniqueIndex"`
	EventKey         string `json:"-" gorm:"type:varchar(128);not null"`
	UserID           int    `json:"-" gorm:"not null;index"`
	InviteeUserID    int    `json:"-" gorm:"not null"`
	Day              string `json:"-" gorm:"type:varchar(10);not null"`
	Source           string `json:"-" gorm:"type:varchar(32);not null"`
	Kind             string `json:"-" gorm:"type:varchar(16);not null"`
	RuleID           int64  `json:"-" gorm:"not null"`
	RateBPS          int    `json:"-" gorm:"not null"`
	BasisQuota       string `json:"-" gorm:"type:text;not null"`
	CommissionQuota  string `json:"-" gorm:"type:text;not null"`
	PublicReason     string `json:"-" gorm:"type:text;not null"`
	InternalEvidence string `json:"-" gorm:"type:text;not null"`
	OperatorID       int    `json:"-" gorm:"not null"`
	OccurredAtMS     int64  `json:"-" gorm:"not null"`
	CreatedAtMS      int64  `json:"-" gorm:"not null"`
}

type PromotionStarDaily struct {
	ID              int64  `gorm:"primaryKey"`
	BucketHash      string `gorm:"type:char(64);not null;uniqueIndex"`
	UserID          int    `gorm:"not null;index:,composite:period,priority:1"`
	InviteeUserID   int    `gorm:"not null;index"`
	Day             string `gorm:"type:varchar(10);not null;index:,composite:period,priority:2"`
	Source          string `gorm:"type:varchar(32);not null"`
	Kind            string `gorm:"type:varchar(16);not null"`
	RuleID          int64  `gorm:"not null"`
	RateBPS         int    `gorm:"not null"`
	BasisQuota      string `gorm:"type:text;not null"`
	CommissionQuota string `gorm:"type:text;not null"`
	PublicReason    string `gorm:"type:text;not null"`
}

type PromotionStarAdjustmentInput struct {
	EventKey         string
	UserID           int
	CommissionQuota  string
	OccurredAtMS     int64
	PublicReason     string
	InternalEvidence string
	OperatorID       int
}

// Ranges are [FromMS, ToMS), aligned to Asia/Shanghai calendar-day boundaries.
// Zero bounds are open. Amounts are quota units, never currency or wallet funds.
type PromotionStarQuery struct {
	FromMS int64
	ToMS   int64
	Offset int
	Limit  int
}

type PromotionStarSummary struct {
	UserID             int    `json:"user_id"`
	DirectInviteeCount int64  `json:"direct_invitee_count"`
	ConsumptionQuota   string `json:"consumption_quota"`
	RefundQuota        string `json:"refund_quota"`
	CommissionQuota    string `json:"commission_quota"`
	ReversalQuota      string `json:"reversal_quota"`
	AdjustmentQuota    string `json:"adjustment_quota"`
	NetCommissionQuota string `json:"net_commission_quota"`
}

type PromotionStarInvitee struct {
	UserID             int    `json:"user_id"`
	Username           string `json:"username"`
	DisplayName        string `json:"display_name"`
	ConsumptionQuota   string `json:"consumption_quota"`
	RefundQuota        string `json:"refund_quota"`
	CommissionQuota    string `json:"commission_quota"`
	ReversalQuota      string `json:"reversal_quota"`
	NetCommissionQuota string `json:"net_commission_quota"`
}

type PromotionStarSummaryEntry struct {
	ID              int64  `json:"id"`
	Day             string `json:"day"`
	UserID          int    `json:"user_id"`
	InviteeUserID   int    `json:"invitee_user_id"`
	Source          string `json:"source"`
	Kind            string `json:"kind"`
	RuleID          int64  `json:"rule_id"`
	RateBPS         int    `json:"rate_bps"`
	BasisQuota      string `json:"basis_quota"`
	CommissionQuota string `json:"commission_quota"`
	PublicReason    string `json:"public_reason"`
}

type PromotionStarEventQuery struct {
	UnresolvedOnly bool
	State          string
	UserID         int
	InviteeUserID  int
	Source         string
	Offset         int
	Limit          int
}

type PromotionStarAdminEvent struct {
	ID               int64  `json:"id"`
	EventKey         string `json:"event_key"`
	OriginalEventKey string `json:"original_event_key"`
	Kind             string `json:"kind"`
	Source           string `json:"source"`
	State            string `json:"state"`
	UserID           int    `json:"user_id"`
	InviteeUserID    int    `json:"invitee_user_id"`
	Quota            string `json:"quota"`
	SettledAtMS      int64  `json:"settled_at_ms"`
	CreatedAtMS      int64  `json:"created_at_ms"`
	UpdatedAtMS      int64  `json:"updated_at_ms"`
	RuleID           int64  `json:"rule_id"`
	RateBPS          int    `json:"rate_bps"`
	ReviewReason     string `json:"review_reason"`
}
