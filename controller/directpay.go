package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	direct "github.com/QuantumNous/new-api/service/directpay"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	dp "github.com/QuantumNous/new-api/types/directpay"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type directPayIntent struct {
	Amount int64  `json:"amount"`
	Method string `json:"method"`
	Quote  string `json:"quote"`
}

func DirectPayMethods(c *gin.Context) {
	methods := []gin.H{}
	if direct.Active.CanCreate(c.GetInt("id")) && operation_setting.IsPaymentComplianceConfirmed() {
		for provider, key := range direct.Active.Current {
			a := direct.Active.Accounts[key]
			method := dp.Page
			if provider == dp.Wechat {
				method = dp.Native
			}
			methods = append(methods, gin.H{"method": method, "environment": a.Environment})
		}
	}
	c.JSON(200, gin.H{"success": true, "data": methods})
}
func directPayQuote(userID int, amount int64) (int64, int, string, int64, error) {
	if amount < getMinTopup() || amount <= 0 || amount > getMaxTopUpAmount() {
		return 0, 0, "", 0, fmt.Errorf("invalid recharge quantity")
	}
	quota, err := validateTopUpQuota(amount)
	if err != nil {
		return 0, 0, "", 0, err
	}
	if err = model.ValidateTopUpQuotaCapacity(userID, quota); err != nil {
		return 0, 0, "", 0, err
	}
	group, err := model.GetUserGroup(userID, true)
	if err != nil {
		return 0, 0, "", 0, err
	}
	price := operation_setting.Price
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	discount := 1.0
	if d, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok && d > 0 {
		discount = d
	}
	for _, v := range []float64{price, ratio, discount, common.QuotaPerUnit} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			return 0, 0, "", 0, fmt.Errorf("invalid pricing configuration")
		}
	}
	units := decimal.NewFromInt(amount)
	stored := amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		units = units.Div(decimal.NewFromFloat(common.QuotaPerUnit))
		stored = units.IntPart()
	}
	money := units.Mul(decimal.NewFromFloat(price)).Mul(decimal.NewFromFloat(ratio)).Mul(decimal.NewFromFloat(discount)).Mul(decimal.NewFromInt(100)).Round(0)
	if money.LessThan(decimal.NewFromInt(1)) || money.GreaterThan(decimal.NewFromInt(direct.Active.Config.MaxMoneyMinor)) {
		return 0, 0, "", 0, fmt.Errorf("payment amount outside configured limits")
	}
	snapshot := fmt.Sprintf("v1;amount=%d;display=%s;price=%s;group=%s;ratio=%s;discount=%s;quota_unit=%s;round=half_away;minor=%s;quota=%d", amount, operation_setting.GetQuotaDisplayType(), decimal.NewFromFloat(price), group, decimal.NewFromFloat(ratio), decimal.NewFromFloat(discount), decimal.NewFromFloat(common.QuotaPerUnit), money, quota)
	return money.IntPart(), quota, snapshot, stored, nil
}
func DirectPayQuote(c *gin.Context) {
	var req directPayIntent
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false})
		return
	}
	minor, quota, snapshot, _, err := directPayQuote(c.GetInt("id"), req.Amount)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"money_minor": minor, "currency": "CNY", "quota_to_credit": quota, "quote": model.DirectPayHash(snapshot)}})
}
func CreateDirectPay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	userID := c.GetInt("id")
	if !direct.Active.CanCreate(userID) {
		c.JSON(403, gin.H{"success": false, "message": "Direct payment creation is disabled"})
		return
	}
	var req directPayIntent
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false})
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if len(key) < 16 || len(key) > 128 {
		c.JSON(400, gin.H{"success": false, "message": "Idempotency-Key required"})
		return
	}
	provider := dp.Alipay
	if req.Method == dp.Native {
		provider = dp.Wechat
	} else if req.Method != dp.Page {
		c.JSON(400, gin.H{"success": false})
		return
	}
	accountKey := direct.Active.Current[provider]
	a, ok := direct.Active.Accounts[accountKey]
	if !ok {
		c.JSON(400, gin.H{"success": false})
		return
	}
	digest := model.DirectPayHash(fmt.Sprintf("%d/%s/%s", req.Amount, req.Method, req.Quote))
	var existing model.DirectPayOrder
	if model.DB.Where("user_id = ? AND request_key = ?", userID, model.DirectPayHash(key)).First(&existing).Error == nil {
		if existing.RequestDigest != digest {
			c.JSON(409, gin.H{"success": false, "message": "Idempotency conflict"})
			return
		}
		c.JSON(200, gin.H{"success": true, "data": existing})
		return
	}
	minor, quota, pricing, stored, err := directPayQuote(userID, req.Amount)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "message": err.Error()})
		return
	}
	if req.Quote != model.DirectPayHash(pricing) {
		c.JSON(409, gin.H{"success": false, "message": "Price changed; request and confirm a new quote", "code": "price_changed"})
		return
	}
	var random [14]byte
	if _, err = rand.Read(random[:]); err != nil {
		c.Status(503)
		return
	}
	prefix := "L"
	if a.Environment == "sandbox" {
		prefix = "S"
	}
	order := model.DirectPayOrder{Snapshot: dp.Snapshot{OrderNo: prefix + hex.EncodeToString(random[:]), Provider: provider, Method: req.Method, Environment: a.Environment, Account: a.Account, Revision: a.Revision, AppID: a.AppID, MerchantID: a.MerchantID, MoneyMinor: minor, Currency: "CNY", Quota: quota, ExpiresAt: time.Now().Unix() + 900}, UserID: userID, RequestKey: model.DirectPayHash(key), RequestDigest: digest, Pricing: pricing}
	created, err := model.CreateDirectPayOrder(&order, stored)
	if err != nil {
		c.JSON(409, gin.H{"success": false, "message": "Order persistence failed or idempotency conflict"})
		return
	}
	if created {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		checkout, createErr := direct.Active.Providers[accountKey].Create(ctx, order.Snapshot)
		cancel()
		state := "pending"
		reason := ""
		if createErr != nil {
			state = "unknown"
			reason = "create_unknown"
		}
		if err = model.DB.Model(&model.DirectPayOrder{}).Where("id = ? AND payment_state = ?", order.ID, "creating").Updates(map[string]any{"checkout_kind": checkout.Kind, "checkout_value": checkout.Value, "payment_state": state, "last_error": reason}).Error; err != nil {
			c.JSON(503, gin.H{"success": false, "message": "Order stored; retry with the same key"})
			return
		}
		_ = model.DB.First(&order, order.ID).Error
	}
	c.JSON(200, gin.H{"success": true, "data": order})
}
func GetDirectPay(c *gin.Context) {
	var order model.DirectPayOrder
	if model.DB.Where("merchant_order_no = ? AND user_id = ?", c.Param("order"), c.GetInt("id")).First(&order).Error != nil {
		c.Status(404)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": order})
}
func RefreshDirectPay(c *gin.Context) {
	updates := map[string]any{"next_query_at": time.Now().Unix()}
	if strings.HasSuffix(c.FullPath(), "/close") {
		updates["close_requested"] = true
	}
	result := model.DB.Model(&model.DirectPayOrder{}).Where("merchant_order_no = ? AND user_id = ? AND settlement_state <> ?", c.Param("order"), c.GetInt("id"), "credited").Updates(updates)
	if result.Error != nil {
		c.Status(503)
		return
	}
	c.JSON(200, gin.H{"success": true})
}
func DirectPayNotify(c *gin.Context) {
	p := direct.Active.Providers[c.Param("account")]
	if p == nil {
		c.Status(404)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 65536))
	if err != nil {
		c.Status(413)
		return
	}
	e, err := p.Notify(c.Request.Header, body)
	if err != nil {
		c.Status(400)
		return
	}
	if err = model.ReceiveDirectPayEvent(e); err != nil {
		c.Status(503)
		return
	}
	if e.Provider == dp.Alipay {
		c.String(200, "success")
	} else {
		c.Status(204)
	}
}
func AdminDirectPay(c *gin.Context) {
	var orders []model.DirectPayOrder
	var events []model.DirectPayEvent
	if model.DB.Order("id desc").Limit(100).Find(&orders).Error != nil || model.DB.Order("id desc").Limit(100).Find(&events).Error != nil {
		c.Status(503)
		return
	}
	accounts := []gin.H{}
	for key, a := range direct.Active.Accounts {
		accounts = append(accounts, gin.H{"account_ref": key, "provider": a.Provider, "environment": a.Environment, "app_id": a.AppID, "merchant_id": a.MerchantID, "verification_mode": a.VerificationMode, "credentials_configured": true, "notify_url": a.NotifyURL})
	}
	for i := range orders {
		orders[i].CheckoutValue = ""
	}
	var enabled model.Option
	_ = model.DB.Where(&model.Option{Key: "directpay.create_enabled"}).First(&enabled).Error
	c.JSON(200, gin.H{"success": true, "data": gin.H{"accounts": accounts, "orders": orders, "events": events, "create_enabled": enabled.Value == "true", "startup_allows_create": direct.Active.Config.CreateEnabled, "refund_enabled": false}})
}
func AdminDirectPaySwitch(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.Status(400)
		return
	}
	if req.Enabled && !requirePaymentCompliance(c) {
		return
	}
	if err := model.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&model.Option{Key: "directpay.create_enabled", Value: fmt.Sprint(req.Enabled)}).Error; err != nil {
		c.Status(503)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("directpay create_enabled=%t", req.Enabled))
	c.JSON(200, gin.H{"success": true})
}

// AdminRefreshDirectPay requests authenticated reconciliation, never an amount
// or a manual credit. Previously verified capacity failures may be retried.
func AdminRefreshDirectPay(c *gin.Context) {
	var order model.DirectPayOrder
	if model.DB.Where("merchant_order_no = ?", c.Param("order")).First(&order).Error != nil {
		c.Status(404)
		return
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&order).Update("next_query_at", time.Now().Unix()).Error; err != nil {
			return err
		}
		return tx.Model(&model.DirectPayEvent{}).Where("order_no = ? AND status = ? AND reason = ?", order.OrderNo, "quarantined", "wallet_unavailable").Updates(map[string]any{"status": "pending", "next_attempt_at": 0}).Error
	}); err != nil {
		c.Status(503)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, "directpay verified reconciliation requested")
	c.JSON(200, gin.H{"success": true})
}
