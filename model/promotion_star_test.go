package model

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func promotionStarTestDB(t *testing.T, dialect string) {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousType := common.MainDatabaseType()
	previousPath := common.SQLitePath
	dsn := "local"
	switch dialect {
	case "sqlite":
		common.SQLitePath = filepath.Join(t.TempDir(), "promotion.db")
	case "mysql":
		dsn = os.Getenv("TEST_MYSQL_DSN")
	case "postgres":
		dsn = os.Getenv("TEST_POSTGRES_DSN")
	}
	if dsn == "" {
		t.Skip("test database DSN is not configured")
	}
	t.Setenv("PROMOTION_TEST_DSN", dsn)
	db, dbType, err := chooseDB("PROMOTION_TEST_DSN", false)
	require.NoError(t, err)
	// Isolate the feature matrix from other model fixtures and existing data.
	db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "promotion_test_"}
	DB, LOG_DB = db, nil
	common.SetMainDatabaseType(dbType)
	initCol()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		for _, table := range []any{&PromotionStarDaily{}, &PromotionStarLedgerEntry{}, &PromotionStarEvent{}, &PromotionStarRule{}, &PromotionStarProfile{}, &User{}} {
			_ = db.Migrator().DropTable(table)
		}
		_ = sqlDB.Close()
		DB, LOG_DB = previousDB, previousLogDB
		common.SetMainDatabaseType(previousType)
		common.SQLitePath = previousPath
		initCol()
	})
	require.NoError(t, db.AutoMigrate(&User{}, &PromotionStarProfile{}, &PromotionStarRule{}, &PromotionStarEvent{}, &PromotionStarLedgerEntry{}, &PromotionStarDaily{}))
}

func promotionStarTestUsers(t *testing.T) (User, User) {
	t.Helper()
	promoter := User{Username: "promoter", DisplayName: "Promotion Star", Password: "unused", AffCode: "promoter-code", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&promoter).Error)
	invitee := User{Username: "invitee", DisplayName: "Direct Invitee", Password: "unused", AffCode: "invitee-code", InviterId: promoter.Id, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&invitee).Error)
	return promoter, invitee
}

func TestPromotionStarFinalSettlementAndIndependentAggregates(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			promotionStarTestDB(t, dialect)
			promoter, invitee := promotionStarTestUsers(t)
			// Shanghai midnight is 16:00 UTC on the preceding day.
			at := time.Date(2026, time.November, 20, 16, 0, 0, 0, time.UTC).UnixMilli()
			input := PromotionStarEventInput{EventKey: "req:first", Kind: PromotionStarKindConsumption, Source: PromotionStarSourceWalletUsage, UserID: invitee.Id, Quota: "0", State: PromotionStarEventProvisional}
			intent, err := RecordPromotionStarEvent(nil, input)
			require.NoError(t, err)
			require.NotNil(t, intent, "capture direct relationship even before a promotion profile exists")
			assert.Equal(t, promoter.Id, intent.PromoterUserID)
			_, err = SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 1250, EffectiveAtMS: at, OperatorID: 99, Reason: "Activation"})
			require.NoError(t, err)
			finalize := PromotionStarFinalizeInput{Quota: "1001", SettledAtMS: at, State: PromotionStarEventPending}
			settled, err := FinalizePromotionStarEvent(nil, input.EventKey, finalize)
			require.NoError(t, err)
			assert.Equal(t, "125.125", settled.CommissionQuota)
			_, err = SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 9000, EffectiveAtMS: at + 1, OperatorID: 99, Reason: "New rate"})
			require.NoError(t, err)
			require.NoError(t, ProcessPromotionStarEvents(20))
			require.NoError(t, ProcessPromotionStarEvents(20), "worker retry is idempotent")
			summary, err := GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			assert.Equal(t, "1001", summary.ConsumptionQuota)
			assert.Equal(t, "125.125", summary.NetCommissionQuota, "worker never uses the current rule")
			assert.EqualValues(t, 1, summary.DirectInviteeCount)
			entries, total, err := ListPromotionStarLedger(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.EqualValues(t, 1, total)
			assert.Equal(t, "2026-11-21", entries[0].Day)
			assert.Equal(t, 1250, entries[0].RateBPS)
			assert.Equal(t, "125.125", entries[0].CommissionQuota)
			payload, err := common.Marshal(entries)
			require.NoError(t, err)
			for _, forbidden := range []string{"req:first", "event_key", "request_id", "internal_evidence", "operator_id", "token", "channel", "ip"} {
				assert.NotContains(t, string(payload), forbidden)
			}
			rows, total, err := ListPromotionStarInvitees(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			require.Len(t, rows, 1)
			assert.EqualValues(t, 1, total)
			assert.Equal(t, "125.125", rows[0].NetCommissionQuota)
			assert.Equal(t, invitee.Username, rows[0].Username)

			replayed, err := RecordPromotionStarEvent(nil, input)
			require.NoError(t, err)
			assert.Equal(t, intent.ID, replayed.ID, "intent retries return immutable existing settlement")
			_, err = FinalizePromotionStarEvent(nil, input.EventKey, finalize)
			require.NoError(t, err)
			finalize.Quota = "1002"
			_, err = FinalizePromotionStarEvent(nil, input.EventKey, finalize)
			assert.ErrorIs(t, err, ErrPromotionStarConflict)

			rollback := assert.AnError
			input.EventKey = "req:rollback"
			err = DB.Transaction(func(tx *gorm.DB) error {
				_, err := RecordPromotionStarEvent(tx, input)
				require.NoError(t, err)
				return rollback
			})
			assert.ErrorIs(t, err, rollback)
			_, err = GetPromotionStarEvent(input.EventKey)
			assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
		})
	}
}

func TestPromotionStarAdjustmentsAreSignedAuditedAndIdempotent(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			promotionStarTestDB(t, dialect)
			promoter, _ := promotionStarTestUsers(t)
			at := time.Date(2026, time.November, 20, 16, 0, 0, 0, time.UTC).UnixMilli()
			_, err := SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 0, EffectiveAtMS: at, OperatorID: 99, Reason: "Activation"})
			require.NoError(t, err)
			input := PromotionStarAdjustmentInput{EventKey: "adjust:1", UserID: promoter.Id, CommissionQuota: "1.2345", OccurredAtMS: at, PublicReason: "Corrected settlement discrepancy", InternalEvidence: "ticket-123", OperatorID: 99}
			entry, err := AdjustPromotionStarCommission(input)
			require.NoError(t, err)
			assert.Equal(t, "1.2345", entry.CommissionQuota)
			assert.Equal(t, "0", entry.BasisQuota)
			assert.Equal(t, "ticket-123", entry.InternalEvidence)
			assert.Equal(t, 99, entry.OperatorID)
			input.OccurredAtMS++
			repeated, err := AdjustPromotionStarCommission(input)
			require.NoError(t, err)
			assert.Equal(t, entry.ID, repeated.ID)
			assert.Equal(t, entry.OccurredAtMS, repeated.OccurredAtMS, "retry preserves the first server occurrence time")
			input.CommissionQuota = "1.2346"
			_, err = AdjustPromotionStarCommission(input)
			assert.ErrorIs(t, err, ErrPromotionStarConflict)

			input.EventKey, input.CommissionQuota = "adjust:2", "-0.2345"
			_, err = AdjustPromotionStarCommission(input)
			require.NoError(t, err)
			summary, err := GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			assert.Equal(t, "1", summary.AdjustmentQuota)
			assert.Equal(t, "1", summary.NetCommissionQuota)
			assert.Equal(t, "0", summary.ConsumptionQuota)
			rows, total, err := ListPromotionStarLedger(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			require.Len(t, rows, 1, "same day and public reason aggregate without exposing individual evidence")
			assert.EqualValues(t, 1, total)
			assert.Equal(t, input.PublicReason, rows[0].PublicReason)
			payload, err := common.Marshal(rows)
			require.NoError(t, err)
			assert.NotContains(t, string(payload), "ticket-123")
			assert.NotContains(t, string(payload), "operator_id")

			input.EventKey, input.OccurredAtMS = "adjust:before", at-1
			_, err = AdjustPromotionStarCommission(input)
			assert.ErrorIs(t, err, ErrPromotionStarInvalid, "adjustments cannot manufacture pre-activation history")
			input.OccurredAtMS, input.PublicReason = at, ""
			_, err = AdjustPromotionStarCommission(input)
			assert.ErrorIs(t, err, ErrPromotionStarInvalid)
			input.PublicReason, input.InternalEvidence = "Reason", ""
			_, err = AdjustPromotionStarCommission(input)
			assert.ErrorIs(t, err, ErrPromotionStarInvalid)
			assert.ErrorIs(t, DB.Model(entry).Update("commission_quota", "9").Error, ErrPromotionStarLedgerImmutable)
			assert.ErrorIs(t, DB.Delete(entry).Error, ErrPromotionStarLedgerImmutable)
		})
	}
}

func TestPromotionStarSuspensionAndDirectEligibility(t *testing.T) {
	promotionStarTestDB(t, "sqlite")
	promoter, invitee := promotionStarTestUsers(t)
	at := time.Date(2026, time.November, 20, 16, 0, 0, 0, time.UTC).UnixMilli()
	enabled, err := IsPromotionStarEnabled(promoter.Id)
	require.NoError(t, err)
	assert.False(t, enabled)
	_, err = SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 0, EffectiveAtMS: at, OperatorID: 99, Reason: "Activation"})
	require.NoError(t, err)
	enabled, err = IsPromotionStarEnabled(promoter.Id)
	require.NoError(t, err)
	assert.True(t, enabled)
	_, err = SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusSuspended, RateBPS: 0, EffectiveAtMS: at + 2, OperatorID: 99, Reason: "Suspension"})
	require.NoError(t, err)
	enabled, err = IsPromotionStarEnabled(promoter.Id)
	require.NoError(t, err)
	assert.False(t, enabled, "suspension immediately removes promotion access")
	for _, tc := range []struct {
		key   string
		at    int64
		state string
	}{
		{"before", at - 1, PromotionStarEventIgnored},
		{"active-zero", at + 1, PromotionStarEventPending},
		{"suspended", at + 2, PromotionStarEventIgnored},
	} {
		event, err := RecordPromotionStarEvent(nil, PromotionStarEventInput{EventKey: tc.key, Kind: PromotionStarKindConsumption, Source: PromotionStarSourceWalletUsage, UserID: invitee.Id, Quota: "100", SettledAtMS: tc.at, State: PromotionStarEventPending})
		require.NoError(t, err)
		assert.Equal(t, tc.state, event.State)
	}
	_, err = SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 10000, EffectiveAtMS: at + 3, OperatorID: 99, Reason: "Resume"})
	require.NoError(t, err)
	grandchild := User{Username: "grandchild", Password: "unused", AffCode: "grandchild-code", InviterId: invitee.Id}
	require.NoError(t, DB.Create(&grandchild).Error)
	event, err := RecordPromotionStarEvent(nil, PromotionStarEventInput{EventKey: "grandchild", Kind: PromotionStarKindConsumption, Source: PromotionStarSourceWalletUsage, UserID: grandchild.Id, Quota: "100", SettledAtMS: at + 4, State: PromotionStarEventPending})
	require.NoError(t, err)
	assert.Equal(t, PromotionStarEventIgnored, event.State, "no multi-level attribution")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", promoter.Id).Update("status", common.UserStatusDisabled).Error)
	enabled, err = IsPromotionStarEnabled(promoter.Id)
	require.NoError(t, err)
	assert.False(t, enabled, "disabled promoter cannot access independent promotion data")
	event, err = RecordPromotionStarEvent(nil, PromotionStarEventInput{EventKey: "banned", Kind: PromotionStarKindConsumption, Source: PromotionStarSourceWalletUsage, UserID: invitee.Id, Quota: "100", SettledAtMS: at + 4, State: PromotionStarEventPending})
	require.NoError(t, err)
	assert.Equal(t, PromotionStarEventIgnored, event.State)
	require.NoError(t, ProcessPromotionStarEvents(20))
	summary, err := GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
	require.NoError(t, err)
	assert.Equal(t, "100", summary.ConsumptionQuota, "zero rate still accounts for eligible basis")
	assert.Equal(t, "0", summary.NetCommissionQuota)
}

func TestPromotionStarRefundsExactlyReverseHistoricalCommission(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			promotionStarTestDB(t, dialect)
			promoter, invitee := promotionStarTestUsers(t)
			at := time.Date(2026, time.November, 20, 16, 0, 0, 0, time.UTC).UnixMilli()
			_, err := SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 9999, EffectiveAtMS: at, OperatorID: 99, Reason: "Activation"})
			require.NoError(t, err)
			original := PromotionStarEventInput{EventKey: "purchase:max", Kind: PromotionStarKindConsumption, Source: PromotionStarSourceSubscriptionBalance, UserID: invitee.Id, Quota: "9007199254740991", SettledAtMS: at, State: PromotionStarEventPending}
			event, err := RecordPromotionStarEvent(nil, original)
			require.NoError(t, err)
			assert.Equal(t, "9006298534815516.9009", event.CommissionQuota, "MaxWallet basis never multiplies in int64 or float64")
			require.NoError(t, ProcessPromotionStarEvents(20))
			_, err = SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusSuspended, RateBPS: 1, EffectiveAtMS: at + 1, OperatorID: 99, Reason: "Suspension"})
			require.NoError(t, err)
			refund := PromotionStarEventInput{EventKey: "refund:partial", Kind: PromotionStarKindRefund, Source: original.Source, UserID: invitee.Id, Quota: "1", SettledAtMS: at + 2, State: PromotionStarEventPending, OriginalEventKey: original.EventKey}
			reversal, err := RecordPromotionStarEvent(nil, refund)
			require.NoError(t, err)
			assert.Equal(t, 9999, reversal.RateBPS)
			assert.Equal(t, "-0.9999", reversal.CommissionQuota)
			repeated, err := RecordPromotionStarEvent(nil, refund)
			require.NoError(t, err)
			assert.Equal(t, reversal.ID, repeated.ID)
			refund.EventKey, refund.Quota = "refund:too-much", "9007199254740991"
			_, err = RecordPromotionStarEvent(nil, refund)
			assert.ErrorIs(t, err, ErrPromotionStarRefundExceeded)
			_, err = GetPromotionStarEvent(refund.EventKey)
			assert.ErrorIs(t, err, gorm.ErrRecordNotFound, "rejected refunds cannot leave a partial outbox row")
			// A deleted/disabled promoter does not cancel reversal of past commission.
			require.NoError(t, DB.Delete(&promoter).Error)
			refund.EventKey, refund.Quota = "refund:rest", "9007199254740990"
			_, err = RecordPromotionStarEvent(nil, refund)
			require.NoError(t, err)
			require.NoError(t, ProcessPromotionStarEvents(20))
			summary, err := GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			assert.Equal(t, "9007199254740991", summary.ConsumptionQuota)
			assert.Equal(t, "9007199254740991", summary.RefundQuota)
			assert.Equal(t, "9006298534815516.9009", summary.CommissionQuota)
			assert.Equal(t, summary.CommissionQuota, summary.ReversalQuota)
			assert.Equal(t, "0", summary.NetCommissionQuota)
			stored, err := GetPromotionStarEvent(original.EventKey)
			require.NoError(t, err)
			assert.Equal(t, "9007199254740991", stored.RefundedQuota)
			assert.Equal(t, stored.CommissionQuota, stored.ReversedCommissionQuota)
			_, err = RecordPromotionStarEvent(nil, refund)
			require.NoError(t, err)
			refund.EventKey, refund.Quota = "refund:after-full", "1"
			_, err = RecordPromotionStarEvent(nil, refund)
			assert.ErrorIs(t, err, ErrPromotionStarRefundExceeded)
		})
	}
}

func TestPromotionStarReviewAndExactAggregateLimits(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			promotionStarTestDB(t, dialect)
			promoter, invitee := promotionStarTestUsers(t)
			at := time.Date(2026, time.November, 20, 16, 0, 0, 0, time.UTC).UnixMilli()
			_, err := SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 10000, EffectiveAtMS: at, OperatorID: 99, Reason: "Activation"})
			require.NoError(t, err)
			input := PromotionStarEventInput{EventKey: "needs-review", Kind: PromotionStarKindConsumption, Source: PromotionStarSourceWalletUsage, UserID: invitee.Id, Quota: "0", State: PromotionStarEventProvisional}
			_, err = RecordPromotionStarEvent(nil, input)
			require.NoError(t, err)
			_, err = FinalizePromotionStarEvent(nil, input.EventKey, PromotionStarFinalizeInput{State: PromotionStarEventReview, Quota: "9007199254740991", SettledAtMS: at + 1, InternalEvidence: "Batch funding not durably confirmed"})
			require.NoError(t, err)
			require.NoError(t, ProcessPromotionStarEvents(20))
			summary, err := GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			assert.Equal(t, "0", summary.NetCommissionQuota, "review is never an optimistic commission")
			status, err := GetPromotionStarProcessingStatus(promoter.Id)
			require.NoError(t, err)
			assert.EqualValues(t, 1, status.ReviewCount)
			adminRows, total, err := ListPromotionStarEvents(PromotionStarEventQuery{State: PromotionStarEventReview})
			require.NoError(t, err)
			require.Len(t, adminRows, 1)
			assert.EqualValues(t, 1, total)
			assert.Equal(t, "Funding requires reconciliation", adminRows[0].ReviewReason)
			_, err = FinalizePromotionStarEvent(nil, input.EventKey, PromotionStarFinalizeInput{State: PromotionStarEventReview, Quota: "9007199254740991", SettledAtMS: at + 1, InternalEvidence: PromotionStarReviewRealtime})
			require.NoError(t, err)
			adminRows, _, err = ListPromotionStarEvents(PromotionStarEventQuery{State: PromotionStarEventReview})
			require.NoError(t, err)
			require.Len(t, adminRows, 1)
			assert.Equal(t, PromotionStarReviewRealtime, adminRows[0].ReviewReason)
			_, err = FinalizePromotionStarEvent(nil, input.EventKey, PromotionStarFinalizeInput{State: PromotionStarEventReview, Quota: "9007199254740991", SettledAtMS: at + 1, InternalEvidence: "private operator evidence"})
			require.NoError(t, err)
			adminRows, _, err = ListPromotionStarEvents(PromotionStarEventQuery{State: PromotionStarEventReview})
			require.NoError(t, err)
			assert.Equal(t, "Funding requires reconciliation", adminRows[0].ReviewReason, "arbitrary internal evidence never becomes a review label")
			_, err = FinalizePromotionStarEvent(nil, input.EventKey, PromotionStarFinalizeInput{State: PromotionStarEventPending, Quota: "9007199254740991", SettledAtMS: at + 1, InternalEvidence: "Funding verified by administrator"})
			require.NoError(t, err)
			input.State, input.Quota, input.SettledAtMS, input.EventKey = PromotionStarEventPending, "9007199254740991", at+2, "second-max"
			_, err = RecordPromotionStarEvent(nil, input)
			require.NoError(t, err)
			require.NoError(t, ProcessPromotionStarEvents(20))
			summary, err = GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			assert.Equal(t, "18014398509481982", summary.ConsumptionQuota, "aggregate may exceed individual wallet bounds")
			assert.Equal(t, "18014398509481982", summary.NetCommissionQuota)
			input.EventKey, input.State, input.Quota, input.SettledAtMS = "unfinished", PromotionStarEventProvisional, "0", 0
			_, err = RecordPromotionStarEvent(nil, input)
			require.NoError(t, err)
			unresolved, unresolvedTotal, err := ListPromotionStarEvents(PromotionStarEventQuery{UnresolvedOnly: true})
			require.NoError(t, err)
			require.Len(t, unresolved, 1)
			assert.EqualValues(t, 1, unresolvedTotal)
			assert.Equal(t, "unfinished", unresolved[0].EventKey)
			input.State, input.SettledAtMS = PromotionStarEventPending, at+2
			daily, err := ListPromotionStarDaily(promoter.Id, PromotionStarQuery{FromMS: at, ToMS: at + 24*60*60*1000})
			require.NoError(t, err)
			require.Len(t, daily, 1)
			assert.Equal(t, "18014398509481982", daily[0].NetCommissionQuota)
			_, err = GetPromotionStarSummary(promoter.Id, PromotionStarQuery{FromMS: at + 1})
			assert.ErrorIs(t, err, ErrPromotionStarInvalid, "cannot pretend partial-day filters are exact for daily aggregates")
			for _, invalid := range []string{"-1", "9007199254740992", "1e900000000", "NaN", "0.1", ""} {
				input.EventKey, input.Quota = "invalid:"+invalid, invalid
				_, err = RecordPromotionStarEvent(nil, input)
				assert.ErrorIs(t, err, ErrPromotionStarInvalid)
			}

			_, err = SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 1250, EffectiveAtMS: at + 3, OperatorID: 99, Reason: "Historical review rate"})
			require.NoError(t, err)
			review := PromotionStarEventInput{EventKey: "historical-review", Kind: PromotionStarKindConsumption, Source: PromotionStarSourceWalletUsage, UserID: invitee.Id, Quota: "1001", SettledAtMS: at + 4, State: PromotionStarEventReview}
			_, err = RecordPromotionStarEvent(nil, review)
			require.NoError(t, err)
			require.NoError(t, DB.Model(&User{}).Where("id = ?", promoter.Id).Update("status", common.UserStatusDisabled).Error)
			review.EventKey, review.SettledAtMS = "ineligible-review", at+5
			_, err = RecordPromotionStarEvent(nil, review)
			require.NoError(t, err)
			require.NoError(t, DB.Model(&User{}).Where("id = ?", promoter.Id).Update("status", common.UserStatusEnabled).Error)
			_, err = SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 9000, EffectiveAtMS: at + 6, OperatorID: 99, Reason: "Later rate"})
			require.NoError(t, err)
			ineligible, err := FinalizePromotionStarEvent(nil, review.EventKey, PromotionStarFinalizeInput{Quota: "1001", SettledAtMS: at + 5, State: PromotionStarEventPending})
			require.NoError(t, err)
			assert.Equal(t, PromotionStarEventIgnored, ineligible.State, "later reactivation cannot qualify a previously ineligible settlement")
			assert.Equal(t, "0", ineligible.CommissionQuota)
			_, err = SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusSuspended, RateBPS: 9000, EffectiveAtMS: at + 7, OperatorID: 99, Reason: "Later suspension"})
			require.NoError(t, err)
			require.NoError(t, DB.Model(&User{}).Where("id = ?", promoter.Id).Update("status", common.UserStatusDisabled).Error)
			require.NoError(t, DB.Delete(&promoter).Error)
			for _, changed := range []PromotionStarFinalizeInput{
				{Quota: "1002", SettledAtMS: at + 4, State: PromotionStarEventPending},
				{Quota: "1001", SettledAtMS: at + 8, State: PromotionStarEventPending},
			} {
				_, err = FinalizePromotionStarEvent(nil, "historical-review", changed)
				assert.ErrorIs(t, err, ErrPromotionStarConflict, "reconciliation cannot replace snapshotted settlement facts")
			}
			confirmed, err := FinalizePromotionStarEvent(nil, "historical-review", PromotionStarFinalizeInput{Quota: "1001", SettledAtMS: at + 4, State: PromotionStarEventPending})
			require.NoError(t, err)
			assert.Equal(t, PromotionStarEventPending, confirmed.State)
			assert.Equal(t, 1250, confirmed.RateBPS, "historical eligibility survives later rate, suspension, ban and deletion")
			assert.Equal(t, "125.125", confirmed.CommissionQuota)
			require.NoError(t, ProcessPromotionStarEvents(20))
			summary, err = GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			assert.Equal(t, "18014398509482107.125", summary.NetCommissionQuota)
		})
	}
}

func TestPromotionStarConcurrentWorkersAndRefundBound(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			promotionStarTestDB(t, dialect)
			sqlDB, err := DB.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(2)
			promoter, invitee := promotionStarTestUsers(t)
			at := time.Date(2026, time.November, 20, 16, 0, 0, 0, time.UTC).UnixMilli()
			_, err = SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 1000, EffectiveAtMS: at, OperatorID: 99, Reason: "Activation"})
			require.NoError(t, err)
			original := PromotionStarEventInput{EventKey: "req:concurrent", Kind: PromotionStarKindConsumption, Source: PromotionStarSourceWalletUsage, UserID: invitee.Id, Quota: "100", SettledAtMS: at, State: PromotionStarEventPending}
			start, results := make(chan struct{}), make(chan error, 2)
			for range 2 {
				go func() {
					<-start
					_, err := RecordPromotionStarEvent(nil, original)
					results <- err
				}()
			}
			close(start)
			for range 2 {
				require.NoError(t, <-results, "concurrent duplicate capture has one durable event")
			}
			start = make(chan struct{})
			for range 2 {
				go func() { <-start; results <- ProcessPromotionStarEvents(20) }()
			}
			close(start)
			for range 2 {
				require.NoError(t, <-results)
			}
			summary, err := GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			assert.Equal(t, "10", summary.NetCommissionQuota)
			start = make(chan struct{})
			for _, key := range []string{"refund:left", "refund:right"} {
				go func() {
					<-start
					_, err := RecordPromotionStarEvent(nil, PromotionStarEventInput{EventKey: key, Kind: PromotionStarKindRefund, Source: original.Source, UserID: invitee.Id, Quota: "60", SettledAtMS: at + 1, State: PromotionStarEventPending, OriginalEventKey: original.EventKey})
					results <- err
				}()
			}
			close(start)
			var succeeded, rejected int
			for range 2 {
				err := <-results
				if err == nil {
					succeeded++
				} else {
					require.ErrorIs(t, err, ErrPromotionStarRefundExceeded)
					rejected++
				}
			}
			assert.Equal(t, 1, succeeded)
			assert.Equal(t, 1, rejected)
			require.NoError(t, ProcessPromotionStarEvents(20))
			summary, err = GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			assert.Equal(t, "60", summary.RefundQuota)
			assert.Equal(t, "4", summary.NetCommissionQuota)
		})
	}
}

func TestPromotionStarSchemaReadinessAndPrimaryDatabaseBoundary(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			promotionStarTestDB(t, dialect)
			assert.False(t, PromotionStarReady())
			require.NoError(t, InitializePromotionStar(DB))
			assert.True(t, PromotionStarReady())
			promoter, invitee := promotionStarTestUsers(t)
			at := time.Date(2026, time.November, 20, 16, 0, 0, 0, time.UTC).UnixMilli()
			_, err := SetPromotionStarProfile(PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 1234, EffectiveAtMS: at, OperatorID: 99, Reason: "Activation"})
			require.NoError(t, err)
			input := PromotionStarEventInput{EventKey: "Req:CASE", Kind: PromotionStarKindConsumption, Source: PromotionStarSourceWalletUsage, UserID: invitee.Id, Quota: "1001", SettledAtMS: at, State: PromotionStarEventPending}
			_, err = RecordPromotionStarEvent(nil, input)
			require.NoError(t, err)
			input.EventKey = "req:case"
			_, err = RecordPromotionStarEvent(nil, input)
			require.NoError(t, err, "MySQL case-insensitive collation must not merge stable business IDs")
			require.NoError(t, ProcessPromotionStarEvents(20))
			for range 2 {
				require.NoError(t, DB.AutoMigrate(&PromotionStarProfile{}, &PromotionStarRule{}, &PromotionStarEvent{}, &PromotionStarLedgerEntry{}, &PromotionStarDaily{}))
				require.NoError(t, InitializePromotionStar(DB))
			}
			summary, err := GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			assert.Equal(t, "2002", summary.ConsumptionQuota)
			assert.Equal(t, "247.0468", summary.NetCommissionQuota)
			_, err = RecordPromotionStarEvent(nil, input)
			require.NoError(t, err)
			require.NoError(t, ProcessPromotionStarEvents(20))
			afterRestart, err := GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
			require.NoError(t, err)
			assert.Equal(t, summary, afterRestart, "restart preserves aggregate and uniqueness guarantees")

			separate, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "log.db")), newGormConfig(true))
			require.NoError(t, err)
			logSQL, err := separate.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = logSQL.Close() })
			LOG_DB = separate
			err = separate.Transaction(func(tx *gorm.DB) error {
				_, err := RecordPromotionStarEvent(tx, input)
				return err
			})
			assert.ErrorIs(t, err, ErrPromotionStarInvalid, "outbox cannot be captured in the separate log database")
			require.NoError(t, DB.Migrator().DropTable(&PromotionStarDaily{}))
			assert.Error(t, InitializePromotionStar(DB))
			assert.False(t, PromotionStarReady())
			_, err = GetPromotionStarSummary(promoter.Id, PromotionStarQuery{})
			assert.Error(t, err, "direct model calls must surface missing table errors")
		})
	}
}

func TestPromotionStarProfileLifecycle(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			promotionStarTestDB(t, dialect)
			promoter, _ := promotionStarTestUsers(t)
			at := time.Date(2026, time.November, 20, 0, 0, 0, 0, time.UTC).UnixMilli()
			input := PromotionStarProfileInput{UserID: promoter.Id, Status: PromotionStarStatusActive, RateBPS: 1250, EffectiveAtMS: at, OperatorID: 99, Reason: "Approved participation"}
			profile, err := SetPromotionStarProfile(input)
			require.NoError(t, err)
			assert.Equal(t, 1250, profile.RateBPS)
			assert.Equal(t, at, profile.FirstActivatedAtMS)

			input.RateBPS, input.EffectiveAtMS = 0, at+1
			_, err = SetPromotionStarProfile(input)
			require.NoError(t, err, "zero is an explicitly supported commission rate")
			input.Status, input.EffectiveAtMS = PromotionStarStatusSuspended, at+2
			_, err = SetPromotionStarProfile(input)
			require.NoError(t, err)
			current, err := GetPromotionStarProfile(promoter.Id)
			require.NoError(t, err)
			assert.Equal(t, PromotionStarStatusSuspended, current.Status)
			assert.Equal(t, at, current.FirstActivatedAtMS)
			rules, err := ListPromotionStarRules(promoter.Id)
			require.NoError(t, err)
			require.Len(t, rules, 3)
			assert.Equal(t, []int{1250, 0, 0}, []int{rules[0].RateBPS, rules[1].RateBPS, rules[2].RateBPS})
			assert.Equal(t, []string{"active", "active", "suspended"}, []string{rules[0].Status, rules[1].Status, rules[2].Status})

			for _, invalidRate := range []int{-1, 10001} {
				input.RateBPS = invalidRate
				_, err = SetPromotionStarProfile(input)
				assert.ErrorIs(t, err, ErrPromotionStarInvalid)
			}
			input.RateBPS, input.EffectiveAtMS = 1000, at-1
			_, err = SetPromotionStarProfile(input)
			assert.ErrorIs(t, err, ErrPromotionStarInvalid, "cannot rewrite prior eligibility")

			input.EffectiveAtMS = at + 3
			rollback := assert.AnError
			err = DB.Transaction(func(tx *gorm.DB) error {
				_, err := SetPromotionStarProfileWithTx(tx, input)
				require.NoError(t, err)
				return rollback
			})
			assert.ErrorIs(t, err, rollback)
			rules, err = ListPromotionStarRules(promoter.Id)
			require.NoError(t, err)
			assert.Len(t, rules, 3, "host transaction rollback rolls back history too")
		})
	}
}
