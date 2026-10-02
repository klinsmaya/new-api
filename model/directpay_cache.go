package model

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/common"
)

// SyncDirectPayCredit is replayable: the cumulative credit watermark belongs to
// the same hash generation as Quota. Hydration sets both from one DB snapshot;
// existing hashes preserve both. No live quota is replaced with a DB balance.
// A delayed/unknown-result call is safe to retry, even after cache expiration.
func SyncDirectPayCredit(userID int) error {
	if !common.RedisEnabled {
		return nil
	}
	var user User
	if err := DB.Select("id", "direct_pay_credit_total").First(&user, userID).Error; err != nil {
		return err
	}
	const script = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[1])
 or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then return 0 end
local previous = tonumber(redis.call('HGET', KEYS[1], 'DirectPayCreditTotal') or '0')
local incoming = tonumber(ARGV[2])
if previous >= incoming then return 1 end
local delta = incoming - previous
local quota = tonumber(redis.call('HGET', KEYS[1], 'Quota'))
if quota == nil or quota + delta > tonumber(ARGV[3]) then return -1 end
redis.call('HINCRBY', KEYS[1], 'Quota', string.format('%.0f', delta))
redis.call('HSET', KEYS[1], 'DirectPayCreditTotal', ARGV[2])
return 1`
	result, err := common.RDB.Eval(context.Background(), script, []string{getUserCacheKey(userID)}, userID, user.DirectPayCreditTotal, common.MaxWalletQuota).Int()
	if err != nil {
		return err
	}
	if result < 0 {
		return errors.New("direct payment cache quota requires review")
	}
	return nil
}
