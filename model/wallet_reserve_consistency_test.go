package model

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Optional external Redis is a dedicated loopback fixture; DB 9 is test-only.
func walletConsistencyRedis(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("WALLET_TEST_REDIS")
	if addr == "" {
		return useUserCacheMiniRedis(t).Addr()
	}
	require.True(t, strings.HasPrefix(addr, "127.0.0.1:"))
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: addr, DB: 9})
	common.RedisEnabled = true
	require.NoError(t, common.RDB.Ping(t.Context()).Err())
	require.NoError(t, common.RDB.Del(t.Context(), "user:1", "auth:user:fence:1", "auth:user:version:1").Err())
	t.Cleanup(func() { _ = common.RDB.Close(); common.RDB = old; common.RedisEnabled = enabled })
	return addr
}

func TestWalletReserveCacheLossConfigurationMatrix(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		redis, batch, credit bool
		admitted             bool
		final                int
	}{
		{"database_only_batch_flag", false, true, false, false, 1000},
		{"redis_synchronous", true, false, false, false, 1000},
		{"redis_batch_unflushed", true, true, false, false, 1000},
		{"redis_batch_with_direct_credit", true, true, true, false, 2000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directPayDB(t, false)
			resetBatchUpdateTestState(t)
			oldBatch, oldRedis := common.BatchUpdateEnabled, common.RedisEnabled
			t.Cleanup(func() { common.BatchUpdateEnabled = oldBatch; common.RedisEnabled = oldRedis })
			common.BatchUpdateEnabled = tc.batch
			common.RedisEnabled = tc.redis
			if tc.redis {
				walletConsistencyRedis(t)
			}
			user := createReserveTestUser(t, 5000)
			ok, err := TryReserveUserQuota(user.Id, 4000)
			require.NoError(t, err)
			require.True(t, ok)
			if tc.credit {
				_, proof := directPayPurchase(t, user, "batch-bridge")
				require.NoError(t, ReceiveDirectPayEvent(proof))
				var ev DirectPayEvent
				require.NoError(t, DB.Where("order_no = ?", proof.OrderNo).First(&ev).Error)
				require.NoError(t, SettleDirectPayEvent(ev.ID))
				require.NoError(t, SyncDirectPayCredit(user.Id))
				assert.Equal(t, 2000, getUserQuotaFromDB(t, user.Id))
			}
			if tc.redis {
				require.NoError(t, common.RDB.Del(t.Context(), getUserCacheKey(user.Id)).Err())
			}
			ok, err = TryReserveUserQuota(user.Id, 4000)
			require.NoError(t, err)
			assert.Equal(t, tc.admitted, ok)
			batchUpdate()
			assert.Equal(t, tc.final, getUserQuotaFromDB(t, user.Id))
			t.Logf("redis=%t batch=%t direct_credit=%t second_admitted=%t final_db=%d", tc.redis, tc.batch, tc.credit, ok, getUserQuotaFromDB(t, user.Id))
		})
	}
}

func TestWalletSynchronousReserveRejectsRecoveredStaleCache(t *testing.T) {
	directPayDB(t, false)
	walletConsistencyRedis(t)
	resetBatchUpdateTestState(t)
	old := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() { common.BatchUpdateEnabled = old })
	user := createReserveTestUser(t, 5000)
	require.NoError(t, populateUserCache(user))
	// Preserve the real cached value while the client temporarily cannot reach it.
	connected := common.RDB
	unavailable := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 20 * time.Millisecond, MaxRetries: -1})
	common.RDB = unavailable
	ok, err := TryReserveUserQuota(user.Id, 4000)
	common.RDB = connected
	require.NoError(t, unavailable.Close())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 1000, getUserQuotaFromDB(t, user.Id))
	cached, err := common.RDB.HGet(t.Context(), getUserCacheKey(user.Id), "Quota").Int()
	require.NoError(t, err)
	assert.Equal(t, 5000, cached)
	ok, err = TryReserveUserQuota(user.Id, 4000)
	require.NoError(t, err)
	assert.False(t, ok, "stale cache must not authorize a second synchronous spend")
	assert.Equal(t, 1000, getUserQuotaFromDB(t, user.Id))
}

func TestWalletSynchronousReserveTwoProcesses(t *testing.T) {
	if os.Getenv("WALLET_GUARD_CHILD") == "1" {
		directPayDB(t, true)
		common.BatchUpdateEnabled = true
		common.RedisEnabled = true
		dbIndex, err := strconv.Atoi(os.Getenv("WALLET_GUARD_REDIS_DB"))
		require.NoError(t, err)
		common.RDB = redis.NewClient(&redis.Options{Addr: os.Getenv("WALLET_GUARD_REDIS"), DB: dbIndex})
		defer common.RDB.Close()
		id, err := strconv.Atoi(os.Getenv("WALLET_GUARD_USER"))
		require.NoError(t, err)
		admitted, err := TryReserveUserQuota(id, 4000)
		require.NoError(t, err)
		fmt.Printf("WALLET_GUARD_ADMITTED=%t\n", admitted)
		return
	}
	dsn := directPayDB(t, false)
	redisAddr := walletConsistencyRedis(t)
	resetBatchUpdateTestState(t)
	old := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() { common.BatchUpdateEnabled = old })
	user := createReserveTestUser(t, 5000)
	require.NoError(t, populateUserCache(user))
	// Both processes can pass the deliberately stale Redis balance. Only the SQL
	// conditional update is allowed to authorize a successful reservation.
	require.NoError(t, common.RDB.HSet(t.Context(), getUserCacheKey(user.Id), "Quota", 12000).Err())
	binary, err := os.Executable()
	require.NoError(t, err)
	commands := make([]*exec.Cmd, 2)
	outputs := make([]bytes.Buffer, 2)
	for i := range commands {
		cmd := exec.Command(binary, "-test.run=^TestWalletSynchronousReserveTwoProcesses$", "-test.count=1")
		cmd.Env = append(os.Environ(), "WALLET_GUARD_CHILD=1", "DIRECTPAY_TEST_DSN="+dsn, "WALLET_GUARD_REDIS="+redisAddr, "WALLET_GUARD_REDIS_DB="+strconv.Itoa(common.RDB.Options().DB), "WALLET_GUARD_USER="+strconv.Itoa(user.Id))
		cmd.Stdout = &outputs[i]
		cmd.Stderr = &outputs[i]
		commands[i] = cmd
		require.NoError(t, cmd.Start())
	}
	admitted, rejected := 0, 0
	for i, cmd := range commands {
		require.NoError(t, cmd.Wait(), outputs[i].String())
		if strings.Contains(outputs[i].String(), "WALLET_GUARD_ADMITTED=true") {
			admitted++
		}
		if strings.Contains(outputs[i].String(), "WALLET_GUARD_ADMITTED=false") {
			rejected++
		}
	}
	assert.Equal(t, 1, admitted)
	assert.Equal(t, 1, rejected)
	assert.Equal(t, 1000, getUserQuotaFromDB(t, user.Id))
	recovered, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 1000, recovered.Quota)
}

// Successful return must survive loss of all process-local batch state. Actual
// usage may create debt; a rejected cap/failing SQL must not create a refund.
func TestWalletSynchronousSettlementAndFailure(t *testing.T) {
	directPayDB(t, false)
	walletConsistencyRedis(t)
	resetBatchUpdateTestState(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, IncreaseUserQuota(user.Id, 0, false), "zero settlement is a no-op, including MySQL changed-rows mode")
	require.NoError(t, DecreaseUserQuota(user.Id, 150, false))
	assert.Equal(t, -50, getUserQuotaFromDB(t, user.Id))
	require.NoError(t, IncreaseUserQuota(user.Id, 70, false))
	assert.Equal(t, 20, getUserQuotaFromDB(t, user.Id))
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 20, cached.Quota)
	batchUpdate()
	assert.Equal(t, 20, getUserQuotaFromDB(t, user.Id), "flush must not apply wallet mutations twice")

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", common.MaxWalletQuota).Error)
	require.NoError(t, invalidateUserCache(user.Id))
	require.ErrorIs(t, IncreaseUserQuota(user.Id, 1, false), ErrWalletQuotaLimitExceeded)
	assert.Equal(t, common.MaxWalletQuota, getUserQuotaFromDB(t, user.Id))
	cached, err = GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, common.MaxWalletQuota, cached.Quota)

	fault := fmt.Errorf("fixture: wallet SQL unavailable")
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("wallet_fixture_failure", func(tx *gorm.DB) { tx.AddError(fault) }))
	t.Cleanup(func() { DB.Callback().Update().Remove("wallet_fixture_failure") })
	require.ErrorIs(t, IncreaseUserQuota(user.Id, 1, false), fault)
	require.ErrorIs(t, DecreaseUserQuota(user.Id, 10, false), fault)
	ok, err := TryReserveUserQuota(user.Id, 10)
	require.ErrorIs(t, err, fault)
	assert.False(t, ok)
	assert.Equal(t, common.MaxWalletQuota, getUserQuotaFromDB(t, user.Id))
	cached, err = GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, common.MaxWalletQuota, cached.Quota, "failed SQL must not change cache balance")
}
