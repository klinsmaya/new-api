package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	dp "github.com/QuantumNous/new-api/types/directpay"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DirectPayOrder struct {
	ID          uint `json:"id"`
	dp.Snapshot `gorm:"embedded"`
	// Explicit indexed order identity; Snapshot.OrderNo is ignored by GORM via embedded override below.
	MerchantOrderNo string  `json:"-" gorm:"size:32;uniqueIndex"`
	TopUpID         int     `json:"-" gorm:"uniqueIndex"`
	UserID          int     `json:"user_id" gorm:"uniqueIndex:idx_dp_request,priority:1;index"`
	RequestKey      string  `json:"-" gorm:"size:64;uniqueIndex:idx_dp_request,priority:2"`
	RequestDigest   string  `json:"-" gorm:"size:64"`
	Pricing         string  `json:"-" gorm:"type:text"`
	PaymentState    string  `json:"payment_state" gorm:"size:32"`
	SettlementState string  `json:"settlement_state" gorm:"size:32"`
	TransactionKey  *string `json:"-" gorm:"size:64;uniqueIndex"`
	TransactionID   string  `json:"transaction_id,omitempty" gorm:"size:128"`
	CheckoutKind    string  `json:"checkout_kind" gorm:"size:16"`
	CheckoutValue   string  `json:"checkout_value,omitempty" gorm:"type:text"`
	CreditedQuota   int     `json:"credited_quota"`
	CreatedAt       int64   `json:"created_at"`
	PaidAt          int64   `json:"paid_at"`
	SettledAt       int64   `json:"settled_at"`
	NextQueryAt     int64   `json:"-" gorm:"index"`
	Attempts        int     `json:"-"`
	LeaseUntil      int64   `json:"-"`
	CloseRequested  bool    `json:"close_requested"`
	LastError       string  `json:"last_error,omitempty" gorm:"size:64"`
}
type DirectPayEvent struct {
	ID        uint   `json:"id"`
	EventKey  string `json:"event_key" gorm:"size:64;uniqueIndex"`
	OrderNo   string `json:"order_no" gorm:"size:32;index"`
	Evidence  string `json:"-" gorm:"type:text"`
	Status    string `json:"status" gorm:"size:24;index"`
	Reason    string `json:"reason,omitempty" gorm:"size:64"`
	CreatedAt int64  `json:"created_at"`
}
type DirectPayLedger struct {
	ID            uint   `json:"id"`
	OrderID       uint   `json:"order_id" gorm:"uniqueIndex"`
	UserID        int    `json:"user_id" gorm:"index"`
	QuotaDelta    int    `json:"quota_delta"`
	CashMinor     int64  `json:"cash_minor"`
	TransactionID string `json:"transaction_id" gorm:"size:128"`
	CreatedAt     int64  `json:"created_at"`
}

func DirectPayHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func CreateDirectPayOrder(o *DirectPayOrder, topUpAmount int64) (bool, error) {
	if o.MoneyMinor <= 0 || o.Quota <= 0 || o.Quota > common.MaxWalletQuota || o.Currency != "CNY" || o.UserID <= 0 {
		return false, ErrInvalidTopUpQuota
	}
	var existing DirectPayOrder
	err := DB.Where("user_id = ? AND request_key = ?", o.UserID, o.RequestKey).First(&existing).Error
	if err == nil {
		if existing.RequestDigest != o.RequestDigest {
			return false, dp.ErrConflict
		}
		*o = existing
		return false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		top := TopUp{UserId: o.UserID, Amount: topUpAmount, Money: float64(o.MoneyMinor) / 100, TradeNo: o.OrderNo, PaymentMethod: o.Method, PaymentProvider: o.Provider, CreateTime: time.Now().Unix(), Status: common.TopUpStatusPending}
		if err := tx.Create(&top).Error; err != nil {
			return err
		}
		o.TopUpID = top.Id
		o.MerchantOrderNo = o.OrderNo
		o.PaymentState = "creating"
		o.SettlementState = "uncredited"
		o.CreatedAt = top.CreateTime
		o.NextQueryAt = top.CreateTime + 15
		return tx.Create(o).Error
	})
	if err != nil { // A concurrent request may have won the unique constraint.
		if readErr := DB.Where("user_id = ? AND request_key = ?", o.UserID, o.RequestKey).First(&existing).Error; readErr == nil {
			if existing.RequestDigest != o.RequestDigest {
				return false, dp.ErrConflict
			}
			*o = existing
			return false, nil
		}
	}
	return err == nil, err
}

// ReceiveDirectPayEvent is called after provider verification. ACK only after it commits.
func ReceiveDirectPayEvent(e dp.Evidence) error {
	b, err := common.Marshal(e)
	if err != nil {
		return err
	}
	event := DirectPayEvent{EventKey: DirectPayHash(e.Provider + "\x00" + e.Environment + "\x00" + e.Account + "\x00" + e.Revision + "\x00" + string(b)), OrderNo: e.OrderNo, Evidence: string(b), Status: "pending", CreatedAt: time.Now().Unix()}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
}

// SettleDirectPayEvent contains no network operations. Every credit and state write
// shares one transaction; failures leave the durable Inbox entry retryable.
func SettleDirectPayEvent(id uint) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var initial DirectPayEvent
		if err := tx.First(&initial, id).Error; err != nil {
			return err
		}
		var o DirectPayOrder
		if err := lockForUpdate(tx).Where("merchant_order_no = ?", initial.OrderNo).First(&o).Error; err != nil {
			return err
		}
		var event DirectPayEvent
		if err := lockForUpdate(tx).First(&event, id).Error; err != nil {
			return err
		}
		if event.Status != "pending" {
			return nil
		}
		var e dp.Evidence
		if err := common.UnmarshalJsonStr(event.Evidence, &e); err != nil {
			return err
		}
		quarantine := func(reason string) error {
			if o.SettlementState != "credited" {
				if err := tx.Model(&o).Updates(map[string]any{"settlement_state": "review_required", "last_error": reason}).Error; err != nil {
					return err
				}
			}
			return tx.Model(&event).Updates(map[string]any{"status": "quarantined", "reason": reason}).Error
		}
		if err := e.Match(o.Snapshot); err != nil {
			return quarantine("evidence_mismatch")
		}
		if o.TransactionID != "" && o.TransactionID != e.TransactionID {
			return quarantine("transaction_conflict")
		}
		if o.SettlementState == "credited" {
			var ledger DirectPayLedger
			if err := tx.Where("order_id = ?", o.ID).First(&ledger).Error; err != nil {
				return err
			}
			if ledger.TransactionID != e.TransactionID || ledger.QuotaDelta != o.Quota {
				return quarantine("ledger_conflict")
			}
			return tx.Model(&event).Update("status", "processed").Error
		}
		key := DirectPayHash(o.Provider + "\x00" + o.Environment + "\x00" + o.Account + "\x00" + e.TransactionID)
		var count int64
		if err := tx.Model(&DirectPayOrder{}).Where("transaction_key = ? AND id <> ?", key, o.ID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return quarantine("transaction_reused")
		}
		var top TopUp
		if err := lockForUpdate(tx).First(&top, o.TopUpID).Error; err != nil {
			return err
		}
		if top.UserId != o.UserID || top.PaymentProvider != o.Provider || top.TradeNo != o.OrderNo || top.Status != common.TopUpStatusPending {
			return quarantine("topup_conflict")
		}
		var user User
		if err := lockForUpdate(tx).First(&user, o.UserID).Error; err != nil {
			return quarantine("user_unavailable")
		}
		if user.Status != common.UserStatusEnabled || user.Quota > common.MaxWalletQuota-o.Quota || user.DirectPayCreditTotal > int64(common.MaxWalletQuota-o.Quota) {
			if err := tx.Model(&o).Updates(map[string]any{"payment_state": "paid", "paid_at": e.PaidAt}).Error; err != nil {
				return err
			}
			return quarantine("wallet_unavailable")
		}
		ledger := DirectPayLedger{OrderID: o.ID, UserID: o.UserID, QuotaDelta: o.Quota, CashMinor: o.MoneyMinor, TransactionID: e.TransactionID, CreatedAt: time.Now().Unix()}
		if err := tx.Create(&ledger).Error; err != nil {
			return err
		}
		if err := creditTopUpQuota(tx, o.UserID, o.Quota, map[string]any{"direct_pay_credit_total": gorm.Expr("COALESCE(direct_pay_credit_total, 0) + ?", o.Quota)}); err != nil {
			return err
		}
		if err := tx.Model(&top).Updates(map[string]any{"status": common.TopUpStatusSuccess, "complete_time": ledger.CreatedAt}).Error; err != nil {
			return err
		}
		if err := tx.Model(&o).Updates(map[string]any{"payment_state": "paid", "settlement_state": "credited", "transaction_key": key, "transaction_id": e.TransactionID, "paid_at": e.PaidAt, "settled_at": ledger.CreatedAt, "credited_quota": o.Quota, "last_error": ""}).Error; err != nil {
			return err
		}
		return tx.Model(&event).Update("status", "processed").Error
	})
}
