package directpay

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	dp "github.com/QuantumNous/new-api/types/directpay"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type reconciliationFixture struct{ state string }

func (p *reconciliationFixture) Create(context.Context, dp.Snapshot) (dp.Checkout, error) {
	return dp.Checkout{}, dp.ErrUnknown
}
func (p *reconciliationFixture) Close(context.Context, dp.Snapshot) error { return nil }
func (p *reconciliationFixture) Notify(http.Header, []byte) (dp.Evidence, error) {
	return dp.Evidence{}, dp.ErrEvidence
}
func (p *reconciliationFixture) Query(_ context.Context, o dp.Snapshot) (dp.Evidence, error) {
	return dp.Evidence{Snapshot: o, State: p.state, TransactionID: "verified-fixture", Source: "query", PaidAt: time.Now().Unix()}, nil
}
func TestHistoricalReconciliationWithCreationDisabled(t *testing.T) {
	oldDB, oldRuntime := model.DB, Active
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { model.DB = oldDB; Active = oldRuntime; common.RedisEnabled = oldRedis })
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "history.db")), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.DirectPayOrder{}, &model.DirectPayEvent{}, &model.DirectPayLedger{}, &model.DirectPayAccountBinding{}))
	user := model.User{Username: "history", Quota: 10, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	order := model.DirectPayOrder{Snapshot: dp.Snapshot{OrderNo: "historical", Provider: dp.Alipay, Method: dp.Page, Environment: "sandbox", Account: "fixture", Revision: "v1", AppID: "app", MerchantID: "merchant", MoneyMinor: 100, Currency: "CNY", Quota: 1000, ExpiresAt: time.Now().Unix() - 1}, UserID: user.Id, RequestKey: "key", RequestDigest: "digest"}
	_, err = model.CreateDirectPayOrder(&order, 1)
	require.NoError(t, err)
	t.Setenv("DIRECTPAY_CONFIG_FILE", "")
	require.ErrorContains(t, Initialize(), "historical")
	fixture := &reconciliationFixture{state: "not_found"}
	Active = &Runtime{Providers: map[string]dp.Provider{"fixture.v1": fixture}}
	binding := model.DirectPayAccountBinding{Reference: "fixture.v1"}
	require.NoError(t, db.Create(&binding).Error)
	require.NoError(t, db.Model(&order).Updates(map[string]any{"next_query_at": 0, "payment_state": "pending"}).Error)
	Tick(t.Context())
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.Equal(t, "closed", order.PaymentState)
	// A late authenticated payment after local close remains payable, including
	// while the new-order switch is off and after compatible worker replacement.
	fixture.state = "paid"
	require.NoError(t, db.Model(&order).Updates(map[string]any{"next_query_at": 0, "lease_until": 0}).Error)
	require.NoError(t, db.Model(&binding).Update("next_request_at", 0).Error)
	Tick(t.Context())
	Tick(t.Context())
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.Equal(t, "credited", order.SettlementState)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 1010, user.Quota)
	Tick(t.Context())
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 1010, user.Quota)
}

func TestProductionCreationSafetyGate(t *testing.T) {
	r := &Runtime{Config: Configuration{DeploymentTier: "production", CreateEnabled: true, AllowedUsers: []int{1}}}
	// Must reject before touching any database/config switch: known baseline
	// cache-loss counterexample prevents production activation in this candidate.
	assert.False(t, r.CanCreate(1))
}
