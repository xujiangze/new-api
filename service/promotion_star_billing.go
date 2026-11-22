package service

import (
	"errors"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"gorm.io/gorm"
)

func promotionRelayEventKey(info *relaycommon.RelayInfo) string {
	if info.TaskRelayInfo != nil && info.PublicTaskID != "" {
		return "task:" + info.PublicTaskID
	}
	if info.RequestId == "" {
		info.RequestId = common.NewRequestId()
	}
	return "request:" + info.RequestId
}

// Capture before funding so a crash cannot silently turn an uncertain debit
// into a confirmed commission. Processing these records never moves funds.
func beginPromotionWalletConsumption(userID int, key string) error {
	if !model.PromotionStarReady() {
		return nil
	}
	_, err := model.RecordPromotionStarEvent(nil, model.PromotionStarEventInput{
		EventKey: key,
		UserID:   userID,
		Kind:     model.PromotionStarKindConsumption,
		Source:   model.PromotionStarSourceWalletUsage,
		Quota:    "0",
		State:    model.PromotionStarEventProvisional,
	})
	return err
}

func finishPromotionWalletConsumption(key string, quota int, fundingApplied bool) {
	if !model.PromotionStarReady() {
		return
	}
	event, err := model.GetPromotionStarEvent(key)
	if errors.Is(err, gorm.ErrRecordNotFound) || event == nil && err == nil {
		return
	}
	if err != nil {
		common.SysError(common.LogText("promotion consumption lookup failed: %s", err.Error()))
		return
	}
	if event.State == model.PromotionStarEventPending || event.State == model.PromotionStarEventProcessed || event.State == model.PromotionStarEventIgnored {
		return
	}
	state, evidence := model.PromotionStarEventPending, ""
	if !fundingApplied {
		state, evidence = model.PromotionStarEventReview, "Wallet settlement was not confirmed"
	} else if common.BatchUpdateEnabled {
		state, evidence = model.PromotionStarEventReview, "Wallet batch updates require reconciliation"
	}
	if quota == 0 && fundingApplied && !common.BatchUpdateEnabled {
		state = model.PromotionStarEventIgnored
	}
	settledAtMS := time.Now().UnixMilli()
	if event.State == model.PromotionStarEventReview && event.SettledAtMS > 0 {
		settledAtMS = event.SettledAtMS
	}
	finalization := model.PromotionStarFinalizeInput{
		Quota: strconv.Itoa(max(quota, 0)), SettledAtMS: settledAtMS,
		State: state, InternalEvidence: evidence,
	}
	_, err = model.FinalizePromotionStarEvent(nil, key, finalization)
	if errors.Is(err, model.ErrPromotionStarOriginalUnsettled) && event.Kind == model.PromotionStarKindRefund {
		finalization.State = model.PromotionStarEventReview
		finalization.InternalEvidence = "Original consumption requires reconciliation before its refund"
		_, err = model.FinalizePromotionStarEvent(nil, key, finalization)
	}
	if err != nil {
		common.SysError(common.LogText("promotion consumption finalization failed: %s", err.Error()))
	}
}

func recordPromotionRelaySettlement(info *relaycommon.RelayInfo, quota int, fundingApplied bool) {
	if !model.PromotionStarReady() || info == nil || info.IsChannelTest || info.BillingSource == BillingSourceSubscription {
		return
	}
	key := promotionRelayEventKey(info)
	if info.RelayFormat == types.RelayFormatOpenAIRealtime {
		// The host separately debits response events and its closing summary.
		// Keep one unresolved fact rather than paying commission on both paths.
		if _, err := model.FinalizePromotionStarEvent(nil, key, model.PromotionStarFinalizeInput{
			Quota: strconv.Itoa(max(quota, 0)), SettledAtMS: time.Now().UnixMilli(),
			State: model.PromotionStarEventReview, InternalEvidence: model.PromotionStarReviewRealtime,
		}); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysError(common.LogText("promotion realtime reconciliation recording failed: %s", err.Error()))
		}
		return
	}
	if info.TaskRelayInfo != nil && info.PublicTaskID != "" {
		if err := model.RecordPromotionStarReservation(nil, key, strconv.Itoa(max(quota, 0)), fundingApplied && !common.BatchUpdateEnabled); err != nil {
			common.SysError(common.LogText("promotion task reservation recording failed: %s", err.Error()))
		}
		return
	}
	finishPromotionWalletConsumption(key, quota, fundingApplied)
}

// RecordPromotionStarTaskCompletion is called only at a terminal task boundary.
// A task row alone is not proof that its wallet reservation was committed.
func RecordPromotionStarTaskCompletion(task *model.Task) {
	if task == nil || !model.PromotionStarReady() || task.PrivateData.BillingSource == BillingSourceSubscription {
		return
	}
	if task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure {
		return
	}
	key := "task:" + task.TaskID
	event, err := model.GetPromotionStarEvent(key)
	if errors.Is(err, gorm.ErrRecordNotFound) || event == nil && err == nil {
		return
	}
	if err != nil {
		common.SysError(common.LogText("promotion task lookup failed: %s", err.Error()))
		return
	}
	if event.State != model.PromotionStarEventProvisional && (event.State != model.PromotionStarEventReview || event.SettledAtMS > 0) {
		return
	}
	finishPromotionWalletConsumption(key, task.Quota, event.ReservationConfirmed && event.State == model.PromotionStarEventProvisional)
}

func beginPromotionWalletRefund(key string, userID int, quota int) (string, error) {
	if !model.PromotionStarReady() || quota <= 0 {
		return "", nil
	}
	original, err := model.GetPromotionStarEvent(key)
	if errors.Is(err, gorm.ErrRecordNotFound) || original == nil && err == nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if original.State != model.PromotionStarEventPending && original.State != model.PromotionStarEventProcessed && original.State != model.PromotionStarEventReview {
		return "", nil
	}
	refundKey := key + ":refund"
	_, err = model.RecordPromotionStarEvent(nil, model.PromotionStarEventInput{
		EventKey:         refundKey,
		Kind:             model.PromotionStarKindRefund,
		Source:           model.PromotionStarSourceWalletUsage,
		UserID:           userID,
		Quota:            strconv.Itoa(quota),
		OriginalEventKey: key,
		State:            model.PromotionStarEventProvisional,
	})
	return refundKey, err
}

func finishPromotionWalletRefund(originalKey, refundKey string, quota int, fundingApplied bool) {
	if refundKey != "" {
		finishPromotionWalletConsumption(refundKey, quota, fundingApplied)
		return
	}
	if fundingApplied {
		finishPromotionWalletConsumption(originalKey, 0, true)
	} else {
		finishPromotionWalletConsumption(originalKey, quota, false)
	}
}

// Legacy Midjourney has its own charging path, but shares the promotion ledger.
func RecordPromotionStarMidjourneyCompletion(task *model.Midjourney) {
	if task == nil || task.Status != "SUCCESS" || !model.PromotionStarReady() {
		return
	}
	key := "midjourney:" + strconv.Itoa(task.Id)
	event, err := model.GetPromotionStarEvent(key)
	if errors.Is(err, gorm.ErrRecordNotFound) || event == nil && err == nil {
		return
	}
	if err != nil {
		common.SysError(common.LogText("promotion Midjourney lookup failed: %s", err.Error()))
		return
	}
	if event.State == model.PromotionStarEventProvisional || event.State == model.PromotionStarEventReview && event.SettledAtMS == 0 {
		finishPromotionWalletConsumption(key, task.Quota, event.ReservationConfirmed && event.State == model.PromotionStarEventProvisional)
	}
}
