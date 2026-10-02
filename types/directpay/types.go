// Package directpay defines the payment contract without SDK or ORM dependencies.
package directpay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const (
	Wechat = "wechat_direct"
	Alipay = "alipay_direct"
	Native = "wechat_native"
	Page   = "alipay_page"
)

var ErrEvidence = errors.New("payment evidence mismatch")
var ErrUnknown = errors.New("provider result unknown; reconcile original order")
var ErrConflict = errors.New("idempotency key conflicts with existing purchase")
var decimalMoney = regexp.MustCompile(`^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$`)

func ParseCNY(s string) (int64, error) {
	if !decimalMoney.MatchString(s) {
		return 0, errors.New("invalid CNY amount")
	}
	whole, frac, _ := strings.Cut(s, ".")
	n, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, err
	}
	frac += strings.Repeat("0", 2-len(frac))
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil || n*100+f <= 0 {
		return 0, errors.New("invalid CNY amount")
	}
	return n*100 + f, nil
}
func FormatCNY(n int64) string { return fmt.Sprintf("%d.%02d", n/100, n%100) }

type Snapshot struct {
	OrderNo     string `json:"order_no"`
	Provider    string `json:"provider"`
	Method      string `json:"method"`
	Environment string `json:"environment"`
	Account     string `json:"account"`
	Revision    string `json:"revision"`
	AppID       string `json:"app_id"`
	MerchantID  string `json:"merchant_id"`
	MoneyMinor  int64  `json:"money_minor"`
	Currency    string `json:"currency"`
	Quota       int    `json:"quota_to_credit"`
	ExpiresAt   int64  `json:"expires_at"`
}
type Checkout struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Evidence is produced only by an authenticated provider adapter, never by browser input.
// QueryBoundIdentity identifies absent identity fields bound by the authenticated query.
type Evidence struct {
	Snapshot
	TransactionID      string `json:"transaction_id"`
	State              string `json:"state"`
	PaidAt             int64  `json:"paid_at"`
	Source             string `json:"source"`
	EventID            string `json:"event_id"`
	QueryBoundIdentity bool   `json:"query_bound_identity"`
}

func (e Evidence) Match(o Snapshot) error {
	if e.Provider != o.Provider || e.Environment != o.Environment || e.Account != o.Account || e.Revision != o.Revision || e.OrderNo != o.OrderNo || e.AppID != o.AppID || e.MerchantID != o.MerchantID || e.MoneyMinor != o.MoneyMinor || e.Currency != "CNY" || o.Currency != "CNY" || e.State != "paid" || e.TransactionID == "" || len(e.TransactionID) > 128 || e.PaidAt <= 0 {
		return ErrEvidence
	}
	return nil
}

type Provider interface {
	Create(context.Context, Snapshot) (Checkout, error)
	Query(context.Context, Snapshot) (Evidence, error)
	Close(context.Context, Snapshot) error
	Notify(http.Header, []byte) (Evidence, error)
}
