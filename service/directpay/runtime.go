package directpay

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/url"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/directpay/provider"
	dp "github.com/QuantumNous/new-api/types/directpay"
)

type Account struct {
	Config  provider.Config `json:"config"`
	Current bool            `json:"current"`
}
type Configuration struct {
	DeploymentTier string    `json:"deployment_tier"`
	BaseURL        string    `json:"base_url"`
	CreateEnabled  bool      `json:"create_enabled"`
	MaxMoneyMinor  int64     `json:"max_money_minor"`
	AllowedUsers   []int     `json:"allowed_users"`
	Accounts       []Account `json:"accounts"`
}
type Runtime struct {
	Config    Configuration
	Providers map[string]dp.Provider
	Accounts  map[string]provider.Config
	Current   map[string]string
}

var Active = &Runtime{Providers: map[string]dp.Provider{}, Accounts: map[string]provider.Config{}, Current: map[string]string{}}
var once sync.Once
var safeRef = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

func Key(account, revision string) string { return account + "." + revision }

// Initialize is fail-closed and never logs the contents of the mounted secret.
func Initialize() error {
	path := os.Getenv("DIRECTPAY_CONFIG_FILE")
	if path == "" {
		var count int64
		if err := model.DB.Model(&model.DirectPayOrder{}).Where("settlement_state <> ?", "credited").Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("historical direct payment accounts required")
		}
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return errors.New("direct payment configuration unavailable")
	}
	if len(b) > 1<<20 {
		return errors.New("direct payment configuration too large")
	}
	var c Configuration
	if common.Unmarshal(b, &c) != nil {
		return errors.New("invalid direct payment configuration")
	}
	if c.DeploymentTier != "local" && c.DeploymentTier != "ci" && c.DeploymentTier != "staging" && c.DeploymentTier != "production" {
		return errors.New("invalid deployment tier")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("direct payment HTTPS origin required")
	}
	if c.MaxMoneyMinor <= 0 || c.MaxMoneyMinor > 100000000 {
		return errors.New("invalid direct payment maximum")
	}
	r := &Runtime{Config: c, Providers: map[string]dp.Provider{}, Accounts: map[string]provider.Config{}, Current: map[string]string{}}
	identities := map[string]string{}
	for _, entry := range c.Accounts {
		a := entry.Config
		if !safeRef.MatchString(a.Account) || !safeRef.MatchString(a.Revision) {
			return errors.New("invalid account reference")
		}
		if c.DeploymentTier == "production" && a.Environment != "live" {
			return errors.New("production accepts live accounts only")
		}
		if a.Environment == "live" && (c.DeploymentTier == "local" || c.DeploymentTier == "ci") {
			return errors.New("local and CI reject live accounts")
		}
		identity := a.Account + "/" + a.AppID + "/" + a.MerchantID + "/" + a.Environment
		if old := identities[a.Provider]; old != "" && old != identity {
			return errors.New("multiple merchants/environments are not supported")
		}
		identities[a.Provider] = identity
		key := Key(a.Account, a.Revision)
		if r.Providers[key] != nil {
			return errors.New("duplicate account revision")
		}
		a.NotifyURL = c.BaseURL + "/api/direct-pay/notify/" + key
		a.ReturnURL = c.BaseURL + "/wallet"
		p, err := provider.New(a)
		if err != nil {
			return err
		}
		r.Providers[key] = p
		r.Accounts[key] = a
		if entry.Current {
			if r.Current[a.Provider] != "" {
				return errors.New("duplicate current provider")
			}
			r.Current[a.Provider] = key
		}
	}
	var outstanding []model.DirectPayOrder
	if err := model.DB.Where("settlement_state <> ?", "credited").Find(&outstanding).Error; err != nil {
		return err
	}
	for _, order := range outstanding {
		a, ok := r.Accounts[Key(order.Account, order.Revision)]
		if !ok || a.AppID != order.AppID || a.MerchantID != order.MerchantID || a.Environment != order.Environment || a.Provider != order.Provider {
			return errors.New("historical account revision missing or changed")
		}
	}
	for key, a := range r.Accounts {
		fingerprint := model.DirectPayHash(fmt.Sprintf("%s/%s/%s/%s/%s/%s/%s", a.Provider, a.Environment, a.AppID, a.MerchantID, a.VerificationMode, a.PublicKeyID, a.PublicKey+a.Serial+model.DirectPayHash(a.PrivateKey)+model.DirectPayHash(a.APIv3Key)))
		binding := model.DirectPayAccountBinding{Reference: key, Fingerprint: fingerprint}
		if err := model.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&binding).Error; err != nil {
			return err
		}
		var existing model.DirectPayAccountBinding
		if err := model.DB.Where("reference = ?", key).First(&existing).Error; err != nil {
			return err
		}
		if existing.Fingerprint != fingerprint {
			return errors.New("account revision is immutable; add a new revision")
		}
	}
	Active = r
	return nil
}
func (r *Runtime) CanCreate(userID int) bool {
	if !r.Config.CreateEnabled {
		return false
	}
	var option model.Option
	if err := model.DB.Where(&model.Option{Key: "directpay.create_enabled"}).First(&option).Error; err != nil || option.Value != "true" {
		return false
	}
	if len(r.Config.AllowedUsers) == 0 {
		return false
	}
	for _, id := range r.Config.AllowedUsers {
		if id == userID {
			return true
		}
	}
	return false
}
func Start() {
	once.Do(func() {
		go func() {
			for {
				Tick(context.Background())
				time.Sleep(5 * time.Second)
			}
		}()
	})
}

// Tick uses durable work and per-order CAS leases, never browser polling for gateway traffic.
func Tick(ctx context.Context) {
	var events []model.DirectPayEvent
	if model.DB.Where("status = ? AND next_attempt_at <= ?", "pending", time.Now().Unix()).Order("id").Limit(100).Find(&events).Error == nil {
		for _, e := range events {
			if err := model.SettleDirectPayEvent(e.ID); err != nil {
				common.SysLog("directpay inbox retry required")
				_ = model.DB.Model(&model.DirectPayEvent{}).Where("id = ?", e.ID).Update("next_attempt_at", time.Now().Unix()+60).Error
			}
		}
	}
	var orders []model.DirectPayOrder
	now := time.Now().Unix()
	if model.DB.Where("settlement_state <> ? AND next_query_at <= ? AND lease_until < ?", "credited", now, now).Order("next_query_at").Limit(10).Find(&orders).Error == nil {
		for _, o := range orders {
			p := Active.Providers[Key(o.Account, o.Revision)]
			if p == nil {
				_ = model.DB.Model(&o).Updates(map[string]any{"next_query_at": now + 300, "last_error": "account_unavailable"}).Error
				continue
			}
			slot := model.DB.Model(&model.DirectPayAccountBinding{}).Where("reference = ? AND next_request_at < ?", Key(o.Account, o.Revision), now).Update("next_request_at", now+2)
			if slot.Error != nil || slot.RowsAffected != 1 {
				continue
			}
			claimed := model.DB.Model(&model.DirectPayOrder{}).Where("id = ? AND lease_until < ? AND next_query_at <= ? AND settlement_state <> ?", o.ID, now, now, "credited").Update("lease_until", now+90)
			if claimed.Error != nil || claimed.RowsAffected != 1 {
				continue
			}
			queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			e, err := p.Query(queryCtx, o.Snapshot)
			cancel()
			updates := map[string]any{"next_query_at": now + int64(min(1800, 30*(1+o.Attempts))) + int64(o.ID%17), "attempts": o.Attempts + 1, "lease_until": 0}
			if err != nil {
				updates["last_error"] = "query_unknown"
			} else {
				switch e.State {
				case "paid":
					if model.ReceiveDirectPayEvent(e) != nil {
						updates["last_error"] = "inbox_unavailable"
					}
				case "pending":
					if o.CloseRequested || now >= o.ExpiresAt {
						closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
						err = p.Close(closeCtx, o.Snapshot)
						cancel()
						updates["last_error"] = "close_confirmation_pending"
						updates["next_query_at"] = now + 30
						if err != nil {
							updates["last_error"] = "close_unknown"
						}
					}
				case "not_found":
					if o.Method == dp.Page && now >= o.ExpiresAt {
						updates["payment_state"] = "closed"
						updates["last_error"] = "expired_checkout_not_created"
					}
				case "closed":
					updates["payment_state"] = "closed"
				}
			}
			if state, ok := updates["payment_state"]; ok {
				updates["payment_state"] = gorm.Expr("CASE WHEN payment_state = ? THEN payment_state ELSE ? END", "paid", state)
			}
			// A concurrent verified credit must not be overwritten by stale NOTPAY/close observations.
			if err := model.DB.Model(&model.DirectPayOrder{}).Where("id = ? AND settlement_state <> ?", o.ID, "credited").Updates(updates).Error; err != nil {
				common.SysLog("directpay query persistence failed")
			}
		}
	}
	// Periodic cumulative replay also heals a crash after DB commit, an unknown
	// Redis result, or delayed hydration from a pre-credit snapshot.
	var users []int
	if model.DB.Model(&model.DirectPayLedger{}).Distinct("user_id").Pluck("user_id", &users).Error == nil {
		for _, id := range users {
			if model.SyncDirectPayCredit(id) != nil {
				common.SysLog("directpay cache reconciliation required")
			}
		}
	}
}
