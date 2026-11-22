package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// InitializePromotionStar verifies the primary schema on both migration and
// non-migration nodes. It does not suppress schema errors or migrate on replicas.
func InitializePromotionStar(db *gorm.DB) error {
	if db == nil || db != DB {
		return fmt.Errorf("%w: primary database is required", ErrPromotionStarInvalid)
	}
	promotionStarReadyDB.Store(nil)
	for _, table := range []any{&PromotionStarProfile{}, &PromotionStarRule{}, &PromotionStarEvent{}, &PromotionStarLedgerEntry{}, &PromotionStarDaily{}} {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("promotion star schema is not initialized: %T", table)
		}
	}
	promotionStarReadyDB.Store(db)
	return nil
}

// PromotionStarReady is false for unrelated test databases that have not opted
// into this feature. Direct model calls always report real database failures.
func PromotionStarReady() bool {
	return DB != nil && promotionStarReadyDB.Load() == DB
}

// promotionStarTransaction retries the complete accounting transaction, never
// the host wallet operation. Supplied transactions are owned by their caller.
func promotionStarTransaction(tx *gorm.DB, fn func(*gorm.DB) error) error {
	if DB == nil {
		return fmt.Errorf("promotion star primary database is not initialized")
	}
	db := DB
	if tx != nil {
		if tx.Config.ConnPool != DB.Config.ConnPool {
			return fmt.Errorf("%w: promotion star requires the primary database", ErrPromotionStarInvalid)
		}
		// Host callbacks may carry an INSERT's table, clauses and bound values.
		// Keep its transaction/context, not its statement: MySQL cannot prepare
		// SAVEPOINT with those stale values, and model queries must start clean.
		tx = tx.Session(&gorm.Session{NewDB: true})
		if _, inTransaction := tx.Statement.ConnPool.(gorm.TxCommitter); inTransaction {
			// A savepoint makes a rejected capture atomic even if the host chooses
			// to continue its transaction after inspecting the returned error.
			return tx.Transaction(fn)
		}
		db = tx
	}
	var err error
	for attempt := range 5 {
		err = db.Transaction(fn)
		if err == nil {
			return nil
		}
		message := strings.ToLower(err.Error())
		if !strings.Contains(message, "database is locked") && !strings.Contains(message, "database table is locked") &&
			!strings.Contains(message, "sqlite_busy") && !strings.Contains(message, "deadlock") &&
			!strings.Contains(message, "serialization failure") && !strings.Contains(message, "could not serialize") &&
			!strings.Contains(message, "lock wait timeout") {
			return err
		}
		if attempt < 4 {
			time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
		}
	}
	return err
}

func SetPromotionStarProfile(input PromotionStarProfileInput) (*PromotionStarProfile, error) {
	return SetPromotionStarProfileWithTx(nil, input)
}

// SetPromotionStarProfileWithTx is the accounting seam. Authorization and
// session revocation belong to the authenticated host transaction, not here.
func SetPromotionStarProfileWithTx(tx *gorm.DB, input PromotionStarProfileInput) (*PromotionStarProfile, error) {
	if input.UserID <= 0 || input.OperatorID <= 0 || input.EffectiveAtMS <= 0 ||
		input.RateBPS < 0 || input.RateBPS > 10000 ||
		(input.Status != PromotionStarStatusActive && input.Status != PromotionStarStatusSuspended) ||
		strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 2000 {
		return nil, ErrPromotionStarInvalid
	}
	var result PromotionStarProfile
	err := promotionStarTransaction(tx, func(tx *gorm.DB) error {
		// The existing user row also serializes creation of the first profile.
		var user User
		if err := lockForUpdate(tx).Select("id").First(&user, input.UserID).Error; err != nil {
			return err
		}
		var profile PromotionStarProfile
		err := lockForUpdate(tx).Where("user_id = ?", input.UserID).First(&profile).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		fresh := errors.Is(err, gorm.ErrRecordNotFound)
		if !fresh && input.EffectiveAtMS < profile.UpdatedAtMS {
			return fmt.Errorf("%w: rule history cannot be backdated", ErrPromotionStarInvalid)
		}
		now := time.Now().UnixMilli()
		rule := PromotionStarRule{UserID: input.UserID, Status: input.Status, RateBPS: input.RateBPS, EffectiveAtMS: input.EffectiveAtMS, OperatorID: input.OperatorID, Reason: input.Reason, CreatedAtMS: now}
		if err := tx.Create(&rule).Error; err != nil {
			return err
		}
		if fresh {
			profile = PromotionStarProfile{UserID: input.UserID, CreatedAtMS: input.EffectiveAtMS}
		}
		profile.Status, profile.RateBPS = input.Status, input.RateBPS
		profile.CurrentRuleID, profile.UpdatedAtMS = rule.ID, input.EffectiveAtMS
		if input.Status == PromotionStarStatusActive && profile.FirstActivatedAtMS == 0 {
			profile.FirstActivatedAtMS = input.EffectiveAtMS
		}
		if err := tx.Save(&profile).Error; err != nil {
			return err
		}
		result = profile
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func IsPromotionStarEnabled(userID int) (bool, error) {
	if userID <= 0 {
		return false, nil
	}
	var count int64
	if err := DB.Model(&PromotionStarProfile{}).Where("user_id = ? AND status = ?", userID, PromotionStarStatusActive).Count(&count).Error; err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}
	if err := DB.Model(&User{}).Where("id = ? AND status = ?", userID, common.UserStatusEnabled).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func GetPromotionStarProfile(userID int) (*PromotionStarProfile, error) {
	var profile PromotionStarProfile
	if err := DB.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		return nil, err
	}
	return &profile, nil
}

func ListPromotionStarProfiles(q PromotionStarQuery) ([]PromotionStarProfile, int64, error) {
	if q.Offset < 0 || q.Limit < 0 || q.Limit > 200 {
		return nil, 0, ErrPromotionStarInvalid
	}
	var total int64
	if err := DB.Model(&PromotionStarProfile{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	profiles := make([]PromotionStarProfile, 0)
	limit := q.Limit
	if limit == 0 {
		limit = 20
	}
	err := DB.Order("user_id DESC").Offset(q.Offset).Limit(limit).Find(&profiles).Error
	return profiles, total, err
}

func ListPromotionStarRules(userID int) ([]PromotionStarRule, error) {
	rules := make([]PromotionStarRule, 0)
	err := DB.Where("user_id = ?", userID).Order("effective_at_ms ASC, id ASC").Find(&rules).Error
	return rules, err
}

func (*PromotionStarRule) BeforeUpdate(*gorm.DB) error        { return ErrPromotionStarLedgerImmutable }
func (*PromotionStarRule) BeforeDelete(*gorm.DB) error        { return ErrPromotionStarLedgerImmutable }
func (*PromotionStarLedgerEntry) BeforeUpdate(*gorm.DB) error { return ErrPromotionStarLedgerImmutable }
func (*PromotionStarLedgerEntry) BeforeDelete(*gorm.DB) error { return ErrPromotionStarLedgerImmutable }
