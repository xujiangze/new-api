package controller

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type promotionStarTotals struct {
	WalletConsumption string `json:"wallet_consumption"`
	WalletRefund      string `json:"wallet_refund"`
	WalletNet         string `json:"wallet_net"`
	Commission        string `json:"commission"`
	Adjustment        string `json:"adjustment"`
	CommissionNet     string `json:"commission_net"`
}

type promotionStarPage struct {
	Items    any   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"p"`
	PageSize int   `json:"page_size"`
}

// Promotion Star remains a session-only capability, independent of numeric user
// roles. Even an administrator must have an active profile to use the self view.
func promotionStarActor(c *gin.Context, admin bool) (service.AuthIdentity, bool) {
	identity, ok := middleware.GetSessionAuthIdentity(c)
	_, pat := model.ParseAccessTokenSessionID(identity.SessionID)
	if !ok || pat || c.GetBool("use_access_token") {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("a dashboard login session is required"), gin.H{"code": "AUTH_SESSION_REQUIRED"})
		return service.AuthIdentity{}, false
	}
	if admin && c.GetInt("role") < common.RoleAdminUser {
		promotionStarError(c, model.ErrPromotionStarForbidden)
		return service.AuthIdentity{}, false
	}
	return identity, true
}

func promotionStarTarget(c *gin.Context) (int, *model.PromotionStarProfile, bool) {
	admin := c.Param("user_id") != ""
	identity, ok := promotionStarActor(c, admin)
	if !ok {
		return 0, nil, false
	}
	userID := identity.UserID
	if admin {
		var err error
		userID, err = strconv.Atoi(c.Param("user_id"))
		if err != nil || userID <= 0 {
			promotionStarError(c, model.ErrPromotionStarInvalid)
			return 0, nil, false
		}
	}
	profile, err := model.GetPromotionStarProfile(userID)
	if !admin && (errors.Is(err, gorm.ErrRecordNotFound) || err == nil && profile.Status != model.PromotionStarStatusActive) {
		promotionStarError(c, model.ErrPromotionStarForbidden)
		return 0, nil, false
	}
	if err != nil {
		promotionStarError(c, err)
		return 0, nil, false
	}
	return userID, profile, true
}

// The view accepts calendar dates only. Ownership, inviter identity, raw event
// IDs and request-log selectors cannot be supplied through query parameters.
func promotionStarQuery(c *gin.Context, dated bool) (model.PromotionStarQuery, *promotionStarPage, bool) {
	query := model.PromotionStarQuery{Limit: 20}
	page := &promotionStarPage{Page: 1, PageSize: 20}
	var err error
	params, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return query, nil, false
	}
	for key, values := range params {
		if len(values) != 1 || values[0] == "" || key != "p" && key != "page_size" && !(dated && (key == "start_date" || key == "end_date")) {
			promotionStarError(c, model.ErrPromotionStarInvalid)
			return query, nil, false
		}
	}
	if raw := params.Get("p"); raw != "" {
		page.Page, err = strconv.Atoi(raw)
		if err != nil || page.Page < 1 || page.Page > 1000000 {
			promotionStarError(c, model.ErrPromotionStarInvalid)
			return query, nil, false
		}
	}
	if raw := params.Get("page_size"); raw != "" {
		page.PageSize, err = strconv.Atoi(raw)
		if err != nil || page.PageSize < 1 || page.PageSize > 100 {
			promotionStarError(c, model.ErrPromotionStarInvalid)
			return query, nil, false
		}
	}
	query.Offset, query.Limit = (page.Page-1)*page.PageSize, page.PageSize
	if !dated {
		return query, page, true
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		promotionStarError(c, err)
		return query, nil, false
	}
	now := time.Now().In(location)
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	if raw := params.Get("end_date"); raw != "" {
		end, err = time.ParseInLocation(time.DateOnly, raw, location)
		if err != nil {
			promotionStarError(c, model.ErrPromotionStarInvalid)
			return query, nil, false
		}
	}
	start := end.AddDate(0, 0, -29)
	if raw := params.Get("start_date"); raw != "" {
		start, err = time.ParseInLocation(time.DateOnly, raw, location)
		if err != nil {
			promotionStarError(c, model.ErrPromotionStarInvalid)
			return query, nil, false
		}
	}
	end = end.AddDate(0, 0, 1)
	if !start.Before(end) || end.After(start.AddDate(0, 0, 366)) || start.UnixMilli() <= 0 {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return query, nil, false
	}
	query.FromMS, query.ToMS = start.UnixMilli(), end.UnixMilli()
	return query, page, true
}

func promotionStarQuotaTotals(consumption, refund, commission, reversal, adjustment, net string) (promotionStarTotals, error) {
	amounts := make([]decimal.Decimal, 6)
	for i, raw := range []string{consumption, refund, commission, reversal, adjustment, net} {
		amount, err := decimal.NewFromString(raw)
		if err != nil {
			return promotionStarTotals{}, err
		}
		amounts[i] = amount
	}
	return promotionStarTotals{
		WalletConsumption: amounts[0].String(), WalletRefund: amounts[1].String(),
		WalletNet:  amounts[0].Sub(amounts[1]).String(),
		Commission: amounts[2].Sub(amounts[3]).String(), Adjustment: amounts[4].String(), CommissionNet: amounts[5].String(),
	}, nil
}

func GetPromotionStarOverview(c *gin.Context) {
	userID, profile, ok := promotionStarTarget(c)
	if !ok {
		return
	}
	query, _, ok := promotionStarQuery(c, true)
	if !ok {
		return
	}
	summary, err := model.GetPromotionStarSummary(userID, query)
	if err != nil {
		promotionStarError(c, err)
		return
	}
	totals, err := promotionStarQuotaTotals(summary.ConsumptionQuota, summary.RefundQuota, summary.CommissionQuota, summary.ReversalQuota, summary.AdjustmentQuota, summary.NetCommissionQuota)
	if err != nil {
		promotionStarError(c, err)
		return
	}
	rows, err := model.ListPromotionStarDaily(userID, query)
	if err != nil {
		promotionStarError(c, err)
		return
	}
	type dailyView struct {
		Date string `json:"date"`
		promotionStarTotals
	}
	daily := make([]dailyView, 0, len(rows))
	for _, row := range rows {
		amounts, err := promotionStarQuotaTotals(row.ConsumptionQuota, row.RefundQuota, row.CommissionQuota, row.ReversalQuota, row.AdjustmentQuota, row.NetCommissionQuota)
		if err != nil {
			promotionStarError(c, err)
			return
		}
		daily = append(daily, dailyView{Date: row.Day, promotionStarTotals: amounts})
	}
	processing, err := model.GetPromotionStarProcessingStatus(userID)
	if err != nil {
		promotionStarError(c, err)
		return
	}
	var updatedThrough *int64
	if processing.LastProcessedMS > 0 {
		updatedThrough = &processing.LastProcessedMS
	}
	common.ApiSuccess(c, gin.H{
		"profile": profile, "totals": totals, "daily": daily,
		"direct_invitee_count": summary.DirectInviteeCount,
		"updated_through":      updatedThrough, "pending_review": processing.PendingCount > 0 || processing.ReviewCount > 0 || processing.ProvisionalCount > 0,
	})
}

func ListPromotionStarInvitees(c *gin.Context) {
	userID, _, ok := promotionStarTarget(c)
	if !ok {
		return
	}
	query, page, ok := promotionStarQuery(c, true)
	if !ok {
		return
	}
	invitees, total, err := model.ListPromotionStarInvitees(userID, query)
	if err != nil {
		promotionStarError(c, err)
		return
	}
	type inviteeView struct {
		UserID      int    `json:"user_id"`
		DisplayName string `json:"display_name"`
		promotionStarTotals
	}
	items := make([]inviteeView, 0, len(invitees))
	for _, invitee := range invitees {
		totals, err := promotionStarQuotaTotals(invitee.ConsumptionQuota, invitee.RefundQuota, invitee.CommissionQuota, invitee.ReversalQuota, "0", invitee.NetCommissionQuota)
		if err != nil {
			promotionStarError(c, err)
			return
		}
		items = append(items, inviteeView{UserID: invitee.UserID, DisplayName: invitee.DisplayName, promotionStarTotals: totals})
	}
	page.Items, page.Total = items, total
	common.ApiSuccess(c, page)
}

func ListPromotionStarLedger(c *gin.Context) {
	userID, _, ok := promotionStarTarget(c)
	if !ok {
		return
	}
	query, page, ok := promotionStarQuery(c, true)
	if !ok {
		return
	}
	items, total, err := model.ListPromotionStarLedger(userID, query)
	if err != nil {
		promotionStarError(c, err)
		return
	}
	page.Items, page.Total = items, total
	common.ApiSuccess(c, page)
}

func ListPromotionStarReviewEvents(c *gin.Context) {
	if _, ok := promotionStarActor(c, true); !ok {
		return
	}
	query, page, ok := promotionStarQuery(c, false)
	if !ok {
		return
	}
	items, total, err := model.ListPromotionStarEvents(model.PromotionStarEventQuery{UnresolvedOnly: true, Offset: query.Offset, Limit: query.Limit})
	if err != nil {
		promotionStarError(c, err)
		return
	}
	type reviewView struct {
		model.PromotionStarAdminEvent
		CanConfirm bool `json:"can_confirm"`
	}
	rows := make([]reviewView, 0, len(items))
	for _, event := range items {
		rows = append(rows, reviewView{PromotionStarAdminEvent: event, CanConfirm: model.PromotionStarReviewCanConfirm(event.State, event.SettledAtMS, event.ReviewReason)})
	}
	page.Items, page.Total = rows, total
	common.ApiSuccess(c, page)
}

func ResolvePromotionStarReviewEvent(c *gin.Context) {
	if _, ok := promotionStarActor(c, true); !ok {
		return
	}
	eventID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || eventID <= 0 {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return
	}
	event, err := model.GetPromotionStarEventByID(eventID)
	if err != nil {
		promotionStarError(c, err)
		return
	}
	payload, ok := promotionStarMutationContext(c, service.VerificationScopePromotionStarReview, "event_key", event.EventKey)
	if !ok {
		return
	}
	var input service.PromotionStarReviewContext
	if err := common.Unmarshal(payload, &input); err != nil {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return
	}
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePromotionStarReview, Context: payload})
	if authorization == nil {
		return
	}
	if err := model.ResolvePromotionStarReviewForAdmin(authorization, model.PromotionStarReviewInput{
		EventID: eventID, EventKey: event.EventKey, State: input.State, Reason: input.Reason,
		ExpectedUpdatedAtMS: input.ExpectedUpdatedAtMS, ExpectedQuota: input.ExpectedQuota, ExpectedSettledAtMS: input.ExpectedSettledAtMS,
	}); err != nil {
		promotionStarError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func ListPromotionStarProfiles(c *gin.Context) {
	if _, ok := promotionStarActor(c, true); !ok {
		return
	}
	query, page, ok := promotionStarQuery(c, false)
	if !ok {
		return
	}
	items, total, err := model.ListPromotionStarProfiles(query)
	if err != nil {
		promotionStarError(c, err)
		return
	}
	page.Items, page.Total = items, total
	common.ApiSuccess(c, page)
}

func GetPromotionStarProfile(c *gin.Context) {
	_, profile, ok := promotionStarTarget(c)
	if !ok {
		return
	}
	if _, _, ok := promotionStarQuery(c, false); !ok {
		return
	}
	common.ApiSuccess(c, profile)
}

func ListPromotionStarRules(c *gin.Context) {
	userID, _, ok := promotionStarTarget(c)
	if !ok {
		return
	}
	query, page, ok := promotionStarQuery(c, false)
	if !ok {
		return
	}
	rules, err := model.ListPromotionStarRules(userID)
	if err != nil {
		promotionStarError(c, err)
		return
	}
	page.Total = int64(len(rules))
	page.Items = rules[min(query.Offset, len(rules)):min(query.Offset+query.Limit, len(rules))]
	common.ApiSuccess(c, page)
}

func GrantPromotionStar(c *gin.Context) {
	changePromotionStarProfile(c, "grant", service.VerificationScopePromotionStarGrant)
}

func ChangePromotionStarRate(c *gin.Context) {
	changePromotionStarProfile(c, "rate", service.VerificationScopePromotionStarRate)
}

func SuspendPromotionStar(c *gin.Context) {
	changePromotionStarProfile(c, "suspend", service.VerificationScopePromotionStarSuspend)
}

func changePromotionStarProfile(c *gin.Context, action, scope string) {
	if _, ok := promotionStarActor(c, true); !ok {
		return
	}
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userID <= 0 {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return
	}
	payload, ok := promotionStarMutationContext(c, scope, "user_id", userID)
	if !ok {
		return
	}
	var input service.PromotionStarRateContext
	if err := common.Unmarshal(payload, &input); err != nil {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return
	}
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: scope, Context: payload})
	if authorization == nil {
		return
	}
	profile, err := model.SetPromotionStarProfileForAdmin(authorization, action, model.PromotionStarProfileInput{
		UserID: userID, RateBPS: input.RateBPS, Reason: input.Reason,
	})
	if err != nil {
		promotionStarError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func AdjustPromotionStarCommission(c *gin.Context) {
	if _, ok := promotionStarActor(c, true); !ok {
		return
	}
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userID <= 0 {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return
	}
	payload, ok := promotionStarMutationContext(c, service.VerificationScopePromotionStarAdjust, "user_id", userID)
	if !ok {
		return
	}
	var input service.PromotionStarAdjustmentContext
	if err := common.Unmarshal(payload, &input); err != nil {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return
	}
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePromotionStarAdjust, Context: payload})
	if authorization == nil {
		return
	}
	_, err = model.AdjustPromotionStarCommissionForAdmin(authorization, model.PromotionStarAdjustmentInput{
		UserID: userID, EventKey: fmt.Sprintf("adjustment:%x", common.Sha256Raw([]byte(strconv.Itoa(userID)+":"+input.IdempotencyKey))),
		CommissionQuota: input.CommissionQuota, PublicReason: input.Reason, InternalEvidence: input.Evidence,
	})
	if err != nil {
		promotionStarError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// A strict JSON body is also the exact proof context. Extra fields, caller-owned
// actor IDs, backdated timestamps and duplicate identity inputs are rejected.
func promotionStarMutationContext(c *gin.Context, scope, targetKey string, target any) ([]byte, bool) {
	contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || contentType != "application/json" || c.Request.URL.RawQuery != "" {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return nil, false
	}
	var fields map[string]common.RawMessage
	if err := common.DecodeJsonBody(http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024), &fields); err != nil || fields == nil {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return nil, false
	}
	if _, exists := fields[targetKey]; exists {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return nil, false
	}
	fields[targetKey], err = common.Marshal(target)
	if err != nil {
		promotionStarError(c, err)
		return nil, false
	}
	payload, err := common.Marshal(fields)
	if err != nil {
		promotionStarError(c, err)
		return nil, false
	}
	if _, err := service.BindVerificationOperation(service.VerificationOperation{Scope: scope, Context: payload}); err != nil {
		promotionStarError(c, model.ErrPromotionStarInvalid)
		return nil, false
	}
	return payload, true
}

func promotionStarError(c *gin.Context, err error) {
	status, code, message := http.StatusInternalServerError, "PROMOTION_STAR_INTERNAL_ERROR", "Please try again later."
	switch {
	case errors.Is(err, model.ErrPromotionStarForbidden):
		status, code, message = http.StatusForbidden, "PROMOTION_STAR_FORBIDDEN", "Unauthorized, insufficient privileges"
	case errors.Is(err, model.ErrPromotionStarInvalid):
		status, code, message = http.StatusBadRequest, "PROMOTION_STAR_INVALID", "Invalid parameters"
	case errors.Is(err, model.ErrPromotionStarConflict):
		status, code, message = http.StatusConflict, "PROMOTION_STAR_CONFLICT", "The action details are invalid."
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code, message = http.StatusNotFound, "PROMOTION_STAR_NOT_FOUND", "The action details are invalid."
	case errors.Is(err, model.ErrUserSessionInactive), errors.Is(err, model.ErrUserSessionInvalid),
		errors.Is(err, model.ErrAuthFlowInvalid), errors.Is(err, model.ErrAuthFlowExpired), errors.Is(err, model.ErrAuthFlowConsumed):
		writeSecurityOperationError(c, err)
		return
	default:
		// SQL errors and outbox evidence never cross the console API boundary.
		_ = c.Error(err)
	}
	common.ApiErrorStatus(c, status, common.NewMessage(message), gin.H{"code": code})
}
