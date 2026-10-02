package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	dp "github.com/QuantumNous/new-api/types/directpay"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func directPayDB(t *testing.T, child bool) string {
	t.Helper()
	old := DB
	oldTypes := common.MainDatabaseType()
	dsn := os.Getenv("DIRECTPAY_TEST_DSN")
	if os.Getenv("DIRECTPAY_TEST_DB") != "" {
		require.True(t, strings.Contains(dsn, "127.0.0.1") && strings.Contains(dsn, "directpay"), "dedicated loopback test DB required")
	}
	kind := os.Getenv("DIRECTPAY_TEST_DB")
	var driver gorm.Dialector
	switch kind {
	case "mysql":
		driver = mysql.Open(dsn)
		common.SetDatabaseTypes(common.DatabaseTypeMySQL, common.DatabaseTypeSQLite)
	case "postgres":
		driver = postgres.Open(dsn)
		common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypeSQLite)
	default:
		if dsn == "" {
			dsn = filepath.Join(t.TempDir(), "directpay.db")
		}
		driver = sqlite.Open(dsn)
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	}
	var err error
	DB, err = gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	handle, err := DB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = handle.Close(); DB = old; common.SetDatabaseTypes(oldTypes, common.DatabaseTypeSQLite) })
	if !child {
		require.NoError(t, DB.Migrator().DropTable(&DirectPayEvent{}, &DirectPayLedger{}, &DirectPayOrder{}, &TopUp{}, &User{}))
		for range 2 {
			require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &DirectPayOrder{}, &DirectPayEvent{}, &DirectPayLedger{}))
		}
	}
	return dsn
}
func directPayPurchase(t *testing.T, user User, suffix string) (DirectPayOrder, dp.Evidence) {
	t.Helper()
	o := DirectPayOrder{Snapshot: dp.Snapshot{OrderNo: "test" + suffix, Provider: dp.Alipay, Method: dp.Page, Environment: "sandbox", Account: "test", Revision: "r1", AppID: "app", MerchantID: "seller", MoneyMinor: 100, Currency: "CNY", Quota: 1000, ExpiresAt: time.Now().Unix() + 600}, UserID: user.Id, RequestKey: DirectPayHash(suffix), RequestDigest: DirectPayHash("purchase"), Pricing: "original pricing"}
	created, err := CreateDirectPayOrder(&o, 1)
	require.NoError(t, err)
	require.True(t, created)
	e := dp.Evidence{Snapshot: o.Snapshot, State: "paid", Source: "notify", PaidAt: time.Now().Unix(), TransactionID: "transaction" + suffix}
	return o, e
}
func TestDirectPayAccounting(t *testing.T) {
	if os.Getenv("DIRECTPAY_TEST_CHILD") == "1" {
		directPayDB(t, true)
		id, err := strconv.Atoi(os.Getenv("DIRECTPAY_TEST_EVENT"))
		require.NoError(t, err)
		for range 100 {
			_ = SettleDirectPayEvent(uint(id))
		}
		return
	}
	dsn := directPayDB(t, false)
	// An authenticated notification for an unknown order is isolated, so it
	// cannot occupy the head of the Inbox forever.
	require.NoError(t, ReceiveDirectPayEvent(dp.Evidence{Snapshot: dp.Snapshot{OrderNo: "missing"}, State: "paid"}))
	var orphan DirectPayEvent
	require.NoError(t, DB.Where("order_no = ?", "missing").First(&orphan).Error)
	require.NoError(t, SettleDirectPayEvent(orphan.ID))
	require.NoError(t, DB.First(&orphan, orphan.ID).Error)
	assert.Equal(t, "quarantined", orphan.Status)
	require.NoError(t, DB.Delete(&orphan).Error)
	user := createReserveTestUser(t, 5000)
	o, e := directPayPurchase(t, user, "first")
	replay := o
	created, err := CreateDirectPayOrder(&replay, 1)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, o.ID, replay.ID)
	replay.RequestDigest = "changed"
	_, err = CreateDirectPayOrder(&replay, 1)
	require.ErrorIs(t, err, dp.ErrConflict)
	require.NoError(t, ReceiveDirectPayEvent(e))
	require.NoError(t, ReceiveDirectPayEvent(e))
	var event DirectPayEvent
	require.NoError(t, DB.First(&event).Error)
	// Two independent processes, each with its own SQL connection pool.
	binary, err := os.Executable()
	require.NoError(t, err)
	commands := make([]*exec.Cmd, 2)
	for i := range commands {
		cmd := exec.Command(binary, "-test.run=^TestDirectPayAccounting$", "-test.count=1")
		cmd.Env = append(os.Environ(), "DIRECTPAY_TEST_CHILD=1", "DIRECTPAY_TEST_DSN="+dsn, "DIRECTPAY_TEST_EVENT="+strconv.Itoa(int(event.ID)))
		commands[i] = cmd
		require.NoError(t, cmd.Start())
	}
	for _, cmd := range commands {
		require.NoError(t, cmd.Wait())
	}
	require.NoError(t, SettleDirectPayEvent(event.ID))
	assert.Equal(t, 6000, getUserQuotaFromDB(t, user.Id))
	var count int64
	require.NoError(t, DB.Model(&DirectPayLedger{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.Error(t, ManualCompleteTopUp(o.OrderNo, "test"))
	// A duplicate provider transaction cannot fund another order.
	other, otherEvidence := directPayPurchase(t, user, "second")
	otherEvidence.TransactionID = e.TransactionID
	require.NoError(t, ReceiveDirectPayEvent(otherEvidence))
	var second DirectPayEvent
	require.NoError(t, DB.Where("order_no = ?", other.OrderNo).First(&second).Error)
	require.NoError(t, SettleDirectPayEvent(second.ID))
	require.NoError(t, DB.First(&second, second.ID).Error)
	assert.Equal(t, "quarantined", second.Status)
	assert.Equal(t, 6000, getUserQuotaFromDB(t, user.Id))
	// Every evidence field participates in authorization of a credit.
	mutations := []func(*dp.Evidence){func(e *dp.Evidence) { e.MoneyMinor++ }, func(e *dp.Evidence) { e.Currency = "USD" }, func(e *dp.Evidence) { e.AppID = "other" }, func(e *dp.Evidence) { e.MerchantID = "other" }, func(e *dp.Evidence) { e.Environment = "live" }, func(e *dp.Evidence) { e.Provider = dp.Wechat }, func(e *dp.Evidence) { e.Revision = "r2" }}
	for i, mutate := range mutations {
		order, proof := directPayPurchase(t, user, fmt.Sprintf("mismatch%d", i))
		mutate(&proof)
		require.NoError(t, ReceiveDirectPayEvent(proof))
		var ev DirectPayEvent
		require.NoError(t, DB.Where("order_no = ?", order.OrderNo).First(&ev).Error)
		require.NoError(t, SettleDirectPayEvent(ev.ID))
		require.NoError(t, DB.First(&ev, ev.ID).Error)
		assert.Equal(t, "quarantined", ev.Status)
	}
	assert.Equal(t, 6000, getUserQuotaFromDB(t, user.Id))
	// Failure between ledger insertion and wallet update rolls everything back.
	failed, proof := directPayPurchase(t, user, "rollback")
	require.NoError(t, ReceiveDirectPayEvent(proof))
	var ev DirectPayEvent
	require.NoError(t, DB.Where("order_no = ?", failed.OrderNo).First(&ev).Error)
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("directpay_fail_wallet", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("injected wallet failure"))
		}
	}))
	require.Error(t, SettleDirectPayEvent(ev.ID))
	require.NoError(t, DB.Callback().Update().Remove("directpay_fail_wallet"))
	assert.Equal(t, 6000, getUserQuotaFromDB(t, user.Id))
	require.NoError(t, DB.Model(&DirectPayLedger{}).Where("order_id = ?", failed.ID).Count(&count).Error)
	assert.Zero(t, count)
	// Simulated upgrade/restart: rerun migration, change current pricing, then
	// settle the pre-upgrade order from the immutable quota snapshot.
	previous := common.QuotaPerUnit
	common.QuotaPerUnit = 999999
	t.Cleanup(func() { common.QuotaPerUnit = previous })
	require.NoError(t, DB.AutoMigrate(&DirectPayOrder{}, &DirectPayEvent{}, &DirectPayLedger{}))
	require.NoError(t, SettleDirectPayEvent(ev.ID))
	assert.Equal(t, 7000, getUserQuotaFromDB(t, user.Id))
	require.NoError(t, SettleDirectPayEvent(ev.ID))
	assert.Equal(t, 7000, getUserQuotaFromDB(t, user.Id))
}
func TestDirectPayCacheRecovery(t *testing.T) {
	directPayDB(t, false)
	addr := os.Getenv("DIRECTPAY_TEST_REDIS")
	if addr == "" {
		useUserCacheMiniRedis(t)
	} else {
		old, enabled := common.RDB, common.RedisEnabled
		common.RDB = redis.NewClient(&redis.Options{Addr: addr})
		common.RedisEnabled = true
		require.NoError(t, common.RDB.Ping(t.Context()).Err())
		t.Cleanup(func() { _ = common.RDB.Close(); common.RDB = old; common.RedisEnabled = enabled })
	}
	user := createReserveTestUser(t, 5000)
	require.NoError(t, common.RDB.Del(t.Context(), getUserCacheKey(user.Id), getUserAuthFenceKey(user.Id), getUserAuthVersionKey(user.Id)).Err())
	require.NoError(t, populateUserCache(user))
	// Reserve remains in cache while DB settlement commits, then replay twice.
	result, err := cacheTryReserveUserQuota(user.Id, 700)
	require.NoError(t, err)
	assert.Equal(t, cacheQuotaOK, result)
	_, e := directPayPurchase(t, user, "cache")
	require.NoError(t, ReceiveDirectPayEvent(e))
	var ev DirectPayEvent
	require.NoError(t, DB.First(&ev).Error)
	require.NoError(t, SettleDirectPayEvent(ev.ID))
	require.NoError(t, SyncDirectPayCredit(user.Id))
	require.NoError(t, SyncDirectPayCredit(user.Id))
	q, err := common.RDB.HGet(t.Context(), getUserCacheKey(user.Id), "Quota").Int()
	require.NoError(t, err)
	assert.Equal(t, 5300, q)
	// Cache rebuild includes the committed credit; a delayed replay adds zero.
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", 5300).Error)
	require.NoError(t, common.RDB.Del(t.Context(), getUserCacheKey(user.Id)).Err())
	var fresh User
	require.NoError(t, DB.First(&fresh, user.Id).Error)
	require.NoError(t, populateUserCache(fresh))
	require.NoError(t, SyncDirectPayCredit(user.Id))
	q, err = common.RDB.HGet(t.Context(), getUserCacheKey(user.Id), "Quota").Int()
	require.NoError(t, err)
	assert.Equal(t, 5300, q)
}

type lostDirectPayReply struct{ fired bool }

func (h *lostDirectPayReply) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (h *lostDirectPayReply) AfterProcess(ctx context.Context, cmd redis.Cmder) error {
	if !h.fired && cmd.Name() == "eval" && cmd.Err() == nil {
		h.fired = true
		return errors.New("injected reply lost after Redis committed Lua")
	}
	return nil
}
func (h *lostDirectPayReply) BeforeProcessPipeline(ctx context.Context, cmds []redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (h *lostDirectPayReply) AfterProcessPipeline(ctx context.Context, cmds []redis.Cmder) error {
	return nil
}
func TestDirectPayRedisFaultWindows(t *testing.T) {
	addr := os.Getenv("DIRECTPAY_TEST_REDIS")
	if addr == "" {
		t.Skip("requires isolated real Redis")
	}
	directPayDB(t, false)
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1})
	common.RedisEnabled = true
	t.Cleanup(func() { _ = common.RDB.Close(); common.RDB = old; common.RedisEnabled = enabled })
	user := createReserveTestUser(t, 5000)
	key := getUserCacheKey(user.Id)
	require.NoError(t, common.RDB.Del(t.Context(), key, getUserAuthFenceKey(user.Id), getUserAuthVersionKey(user.Id)).Err())
	require.NoError(t, populateUserCache(user))
	_, err := cacheTryReserveUserQuota(user.Id, 700)
	require.NoError(t, err)
	_, proof := directPayPurchase(t, user, "fault")
	require.NoError(t, ReceiveDirectPayEvent(proof))
	var ev DirectPayEvent
	require.NoError(t, DB.First(&ev).Error)
	require.NoError(t, SettleDirectPayEvent(ev.ID))
	// Actual Redis executes Lua; only the client-visible successful response is lost.
	hook := &lostDirectPayReply{}
	common.RDB.AddHook(hook)
	require.ErrorContains(t, SyncDirectPayCredit(user.Id), "reply lost")
	require.NoError(t, SyncDirectPayCredit(user.Id))
	quota, err := common.RDB.HGet(t.Context(), key, "Quota").Int()
	require.NoError(t, err)
	assert.Equal(t, 5300, quota)
	total, err := common.RDB.HGet(t.Context(), key, "DirectPayCreditTotal").Int64()
	require.NoError(t, err)
	assert.EqualValues(t, 1000, total)
	// A previous worker's stale cumulative snapshot cannot move the watermark back.
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("direct_pay_credit_total", 500).Error)
	require.NoError(t, SyncDirectPayCredit(user.Id))
	quota, err = common.RDB.HGet(t.Context(), key, "Quota").Int()
	require.NoError(t, err)
	assert.Equal(t, 5300, quota)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("direct_pay_credit_total", 1000).Error)
	// Old-version hydration after expiry contains committed credit but no watermark.
	require.NoError(t, common.RDB.Del(t.Context(), key).Err())
	require.NoError(t, common.RDB.HSet(t.Context(), key, "Id", user.Id, "Quota", 6000, "CacheSchema", 2).Err())
	require.Error(t, SyncDirectPayCredit(user.Id))
	quota, err = common.RDB.HGet(t.Context(), key, "Quota").Int()
	require.NoError(t, err)
	assert.Equal(t, 6000, quota)
	// Delayed compatible hydration preserves its matching (old balance, old total)
	// pair; credit replay catches it up exactly once without overwriting the hash.
	require.NoError(t, common.RDB.Del(t.Context(), key).Err())
	require.NoError(t, populateUserCache(user))
	require.NoError(t, SyncDirectPayCredit(user.Id))
	require.NoError(t, populateUserCache(user))
	quota, err = common.RDB.HGet(t.Context(), key, "Quota").Int()
	require.NoError(t, err)
	assert.Equal(t, 6000, quota)
	t.Log("real Redis: unknown-result retry, out-of-order total, legacy hydration rejection, delayed hydration PASS")
}

// This is a release-blocking characterization, not a claim of wallet safety.
// It intentionally does not flush or manually adjust DB before Redis loss.
func TestDirectPayBaselineBatchLossCounterexample(t *testing.T) {
	directPayDB(t, false)
	useUserCacheMiniRedis(t)
	resetBatchUpdateTestState(t)
	old := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() { common.BatchUpdateEnabled = old })
	user := createReserveTestUser(t, 5000)
	require.NoError(t, populateUserCache(user))
	ok, err := TryReserveUserQuota(user.Id, 4000)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 5000, getUserQuotaFromDB(t, user.Id))
	require.NoError(t, common.RDB.Del(t.Context(), getUserCacheKey(user.Id)).Err())
	ok, err = TryReserveUserQuota(user.Id, 4000)
	require.NoError(t, err)
	// Two admitted reservations exceed the committed wallet. This baseline risk
	// must block production activation; a cumulative credit watermark cannot fix it.
	assert.True(t, ok, "if baseline is repaired, replace this characterization with safety assertions and review release gate")
	t.Log("CONFIRMED_UNSAFE_BASELINE: cache loss before batch flush admitted 8000 against 5000")
}
