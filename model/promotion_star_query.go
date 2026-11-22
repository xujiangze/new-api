package model

import (
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type PromotionStarDailySummary struct {
	Day string `json:"day"`
	PromotionStarSummary
}

type PromotionStarProcessingStatus struct {
	PendingCount     int64 `json:"pending_count"`
	ProvisionalCount int64 `json:"provisional_count"`
	ReviewCount      int64 `json:"review_count"`
	LastProcessedMS  int64 `json:"last_processed_ms"`
}

func promotionStarDailyQuery(userID int, q PromotionStarQuery) (*gorm.DB, error) {
	if userID <= 0 || q.FromMS < 0 || q.ToMS < 0 || (q.ToMS > 0 && q.FromMS >= q.ToMS) || q.Offset < 0 || q.Limit < 0 || q.Limit > 200 {
		return nil, ErrPromotionStarInvalid
	}
	db := DB.Model(&PromotionStarDaily{}).Where("user_id = ?", userID)
	for _, bound := range []struct {
		ms       int64
		operator string
	}{{q.FromMS, ">="}, {q.ToMS, "<"}} {
		if bound.ms == 0 {
			continue
		}
		date := time.UnixMilli(bound.ms).In(promotionStarLocation)
		if date.Hour() != 0 || date.Minute() != 0 || date.Second() != 0 || date.Nanosecond() != 0 {
			return nil, fmt.Errorf("%w: range must use Asia/Shanghai day boundaries", ErrPromotionStarInvalid)
		}
		db = db.Where("day "+bound.operator+" ?", date.Format("2006-01-02"))
	}
	return db, nil
}

type promotionStarTotals struct {
	consumption decimal.Decimal
	refund      decimal.Decimal
	commission  decimal.Decimal
	reversal    decimal.Decimal
	adjustment  decimal.Decimal
}

func (totals *promotionStarTotals) add(row PromotionStarDaily) error {
	basis, err := decimal.NewFromString(row.BasisQuota)
	if err != nil {
		return err
	}
	commission, err := decimal.NewFromString(row.CommissionQuota)
	if err != nil {
		return err
	}
	switch row.Kind {
	case PromotionStarKindConsumption:
		totals.consumption = totals.consumption.Add(basis)
		totals.commission = totals.commission.Add(commission)
	case PromotionStarKindRefund:
		totals.refund = totals.refund.Sub(basis)
		totals.reversal = totals.reversal.Sub(commission)
	case PromotionStarKindAdjustment:
		totals.adjustment = totals.adjustment.Add(commission)
	default:
		return fmt.Errorf("unknown promotion star aggregate kind %q", row.Kind)
	}
	return nil
}

func (totals promotionStarTotals) summary(userID int) PromotionStarSummary {
	return PromotionStarSummary{
		UserID: userID, ConsumptionQuota: totals.consumption.String(), RefundQuota: totals.refund.String(),
		CommissionQuota: totals.commission.String(), ReversalQuota: totals.reversal.String(),
		AdjustmentQuota: totals.adjustment.String(), NetCommissionQuota: totals.commission.Sub(totals.reversal).Add(totals.adjustment).String(),
	}
}

func GetPromotionStarSummary(userID int, q PromotionStarQuery) (*PromotionStarSummary, error) {
	query, err := promotionStarDailyQuery(userID, q)
	if err != nil {
		return nil, err
	}
	var rows []PromotionStarDaily
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	var totals promotionStarTotals
	for _, row := range rows {
		if err := totals.add(row); err != nil {
			return nil, err
		}
	}
	summary := totals.summary(userID)
	if err := DB.Model(&User{}).Where("inviter_id = ? AND id <> ?", userID, userID).Count(&summary.DirectInviteeCount).Error; err != nil {
		return nil, err
	}
	return &summary, nil
}

func ListPromotionStarLedger(userID int, q PromotionStarQuery) ([]PromotionStarSummaryEntry, int64, error) {
	query, err := promotionStarDailyQuery(userID, q)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit == 0 {
		limit = 20
	}
	rows := make([]PromotionStarSummaryEntry, 0)
	err = query.Select("id", "day", "user_id", "invitee_user_id", "source", "kind", "rule_id", "rate_bps", "basis_quota", "commission_quota", "public_reason").Order("day DESC, id DESC").Limit(limit).Offset(q.Offset).Scan(&rows).Error
	return rows, total, err
}

func ListPromotionStarInvitees(userID int, q PromotionStarQuery) ([]PromotionStarInvitee, int64, error) {
	query, err := promotionStarDailyQuery(userID, q)
	if err != nil {
		return nil, 0, err
	}
	users := DB.Model(&User{}).Where("inviter_id = ? AND id <> ?", userID, userID)
	var total int64
	if err := users.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit == 0 {
		limit = 20
	}
	var invitees []User
	if err := users.Select("id", "username", "display_name").Order("id DESC").Offset(q.Offset).Limit(limit).Find(&invitees).Error; err != nil {
		return nil, 0, err
	}
	result := make([]PromotionStarInvitee, 0, len(invitees))
	if len(invitees) == 0 {
		return result, total, nil
	}
	ids := make([]int, 0, len(invitees))
	for _, user := range invitees {
		ids = append(ids, user.Id)
	}
	var rows []PromotionStarDaily
	if err := query.Where("invitee_user_id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	totals := make(map[int]*promotionStarTotals, len(ids))
	for _, row := range rows {
		if totals[row.InviteeUserID] == nil {
			totals[row.InviteeUserID] = &promotionStarTotals{}
		}
		if err := totals[row.InviteeUserID].add(row); err != nil {
			return nil, 0, err
		}
	}
	for _, user := range invitees {
		amounts := totals[user.Id]
		if amounts == nil {
			amounts = &promotionStarTotals{}
		}
		summary := amounts.summary(userID)
		result = append(result, PromotionStarInvitee{UserID: user.Id, Username: user.Username, DisplayName: user.DisplayName,
			ConsumptionQuota: summary.ConsumptionQuota, RefundQuota: summary.RefundQuota, CommissionQuota: summary.CommissionQuota,
			ReversalQuota: summary.ReversalQuota, NetCommissionQuota: summary.NetCommissionQuota})
	}
	return result, total, nil
}

func ListPromotionStarDaily(userID int, q PromotionStarQuery) ([]PromotionStarDailySummary, error) {
	query, err := promotionStarDailyQuery(userID, q)
	if err != nil {
		return nil, err
	}
	var rows []PromotionStarDaily
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	byDay := make(map[string]*promotionStarTotals)
	for _, row := range rows {
		if byDay[row.Day] == nil {
			byDay[row.Day] = &promotionStarTotals{}
		}
		if err := byDay[row.Day].add(row); err != nil {
			return nil, err
		}
	}
	result := make([]PromotionStarDailySummary, 0, len(byDay))
	for day, totals := range byDay {
		result = append(result, PromotionStarDailySummary{Day: day, PromotionStarSummary: totals.summary(userID)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Day < result[j].Day })
	return result, nil
}

func GetPromotionStarProcessingStatus(userID int) (*PromotionStarProcessingStatus, error) {
	if userID <= 0 {
		return nil, ErrPromotionStarInvalid
	}
	status := &PromotionStarProcessingStatus{}
	for state, count := range map[string]*int64{
		PromotionStarEventPending: &status.PendingCount, PromotionStarEventProvisional: &status.ProvisionalCount, PromotionStarEventReview: &status.ReviewCount,
	} {
		if err := DB.Model(&PromotionStarEvent{}).Where("promoter_user_id = ? AND state = ?", userID, state).Count(count).Error; err != nil {
			return nil, err
		}
	}
	var latest []PromotionStarEvent
	if err := DB.Select("processed_at_ms").Where("promoter_user_id = ? AND state = ?", userID, PromotionStarEventProcessed).Order("processed_at_ms DESC").Limit(1).Find(&latest).Error; err != nil {
		return nil, err
	}
	if len(latest) > 0 {
		status.LastProcessedMS = latest[0].ProcessedAtMS
	}
	return status, nil
}

func ListPromotionStarEvents(q PromotionStarEventQuery) ([]PromotionStarAdminEvent, int64, error) {
	if q.Offset < 0 || q.Limit < 0 || q.Limit > 200 || q.UserID < 0 || q.InviteeUserID < 0 {
		return nil, 0, ErrPromotionStarInvalid
	}
	query := DB.Model(&PromotionStarEvent{})
	if q.UnresolvedOnly {
		query = query.Where("state IN ?", []string{PromotionStarEventProvisional, PromotionStarEventReview})
	}
	if q.State != "" {
		query = query.Where("state = ?", q.State)
	}
	if q.Source != "" {
		query = query.Where("source = ?", q.Source)
	}
	if q.UserID > 0 {
		query = query.Where("promoter_user_id = ?", q.UserID)
	}
	if q.InviteeUserID > 0 {
		query = query.Where("user_id = ?", q.InviteeUserID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit == 0 {
		limit = 20
	}
	rows := make([]PromotionStarAdminEvent, 0)
	err := query.Select("id, event_key, original_event_key, kind, source, state, promoter_user_id AS user_id, user_id AS invitee_user_id, quota, settled_at_ms, created_at_ms, updated_at_ms, rule_id, rate_bps, review_reason").Order("id DESC").Offset(q.Offset).Limit(limit).Scan(&rows).Error
	return rows, total, err
}
