# Wallet reservation fault diagnosis and bounded fix

Updated 2026-10-03. Current status: the approved three-entrypoint synchronous wallet fix is implemented and the inherited batch/cache-loss defect is covered by regression tests. Production creation remains disabled; mixed fleets, drain/rollback and load gates still apply. See the authorized continuation below. Earlier sections record the original diagnosis and scope decision. No production access or configuration change was used.

## Historical reproduction before the approved fix

Start with quota 5000; reserve 4000; delete the user Redis hash before the next reserve of 4000. The tests use isolated SQLite/PostgreSQL/MySQL databases and deterministic or real loopback Redis.

| Redis | Batch update | Direct credit between reserves | Second reservation | Final DB after batch flush |
|---|---|---|---|---|
| disabled | enabled | none | rejected | 1000 |
| enabled | disabled | none | rejected | 1000 |
| enabled | enabled | none | admitted (defect) | -3000 |
| enabled | enabled | +1000, settled and cache synchronized | admitted (defect) | -2000 |

The affected configuration is Redis enabled plus `BATCH_UPDATE_ENABLED=true`, while a wallet delta remains in the process-local queue (default flush interval five seconds). Cache eviction/restart can rehydrate the unreserved DB balance. Queue loss on process failure is a related unresolved durability problem. Direct-pay credits settle correctly and preserve cached reservations while that cache survives; their watermark cannot make the legacy consumption queue durable. This is an inherited defect, not evidence that the new credit bridge is safe under all consumption configurations.

A separate synchronous defect also reproduced: Redis unavailable causes the first reservation to use SQL; Redis recovery exposes its old 5000 balance, allowing another 4000 through the old unconditional SQL write. Before the fix the diagnostic fails with final quota -3000 (`wallet-diagnostic-before.log`, exit 1).

## Historical first-stage minimal correction

Only the synchronous persistence path of `TryReserveUserQuota` now uses an atomic SQL `quota >= requested` condition. A stale-cache admission rejected by SQL compensates the attempted cache delta, invalidates the stale hash, and reports ordinary insufficient balance. Deleted users retain their previous not-found error behavior. No unconditional settlement/debt or refund logic was changed. Batch, token and statistics paths are unchanged.

Two independent processes are given inflated Redis balance 12000 against SQL balance 5000. Exactly one 4000 reservation must succeed, the other must be rejected, and DB/cache recover to 1000. This tests caller outcomes, not only the final SQL value. Real-Redis outage/recovery and the configuration matrix are included. SQL-commit-unknown handling and durable usage settlement remain outside this narrow correction.

## Historical scope decision (subsequently approved)

The narrow correction does **not** repair enabled batch consumption or mixed fleets containing batch writers. Production new direct-pay orders remain hard-disabled; historical notifications and settlement remain active.

The smallest complete persistence change identified touches three user-wallet entrypoints: `persistUserQuotaDelta`, `IncreaseUserQuota`, `DecreaseUserQuota`. They would synchronously persist user wallet changes even when token/statistics batching remains enabled. Pre-reservation must retain its conditional balance guard; actual usage settlement must preserve existing permitted debt and refund/cap behavior. It requires a full-node drain and successful old-queue flush, prohibits mixed old batch writers, and adds SQL write load and SQL availability dependency to consumption/refunds. These are whole-site wallet semantics, so this larger step is pending parent/user scope confirmation under the instruction to avoid unrelated wallet reconstruction.

Recommended option: authorize this narrowly bounded user-wallet persistence change, followed by failure/concurrency/relay billing tests and independent review; keep the production gate until all remaining acceptance gates pass. Alternative: durable reservation journal and queue recovery, a materially larger design requiring its own scope. Until either is approved and verified, retain the release block. Merely disabling batch in a production environment is not performed or claimed here.

## Verification

Go 1.26.8; tests and command exit codes are appended to TEST_REPORT.md. Fixture credentials are disposable local test values. No official gateway or real money was used.

## Authorized continuation — 2026-10-03 (supersedes pending decision above)

The user approved the three-entrypoint change. Wallet reservation, IncreaseUserQuota and DecreaseUserQuota now commit SQL synchronously regardless of `BATCH_UPDATE_ENABLED`/legacy `db` argument. Token quota, used-quota/request statistics and channel statistics remain batched. Refund caps and unconditional actual-usage debt are preserved. A zero increase is an explicit no-op to preserve MySQL changed-row behavior for exact preconsume/actual-use settlement.

Post-commit increases/decreases invalidate the cache synchronously instead of applying delayed deltas to a potentially rehydrated hash. Failure to invalidate is logged, not returned as a failed financial write (repeating non-idempotent credits would be unsafe). Successful pre-reservation still requires a SQL balance guard, so stale high Redis values cannot authorize overspend on a homogeneous new fleet. Stale low hints can temporarily reject otherwise affordable requests; existing SQL-unknown-commit, non-idempotent usage refunds and token batch durability are not solved by this work.

The original 5000 / 4000 / cache-loss / 4000 test now rejects the second reservation, final DB 1000 with batching on or off. With an intervening direct credit of 1000, final DB is 2000 and the second reservation is rejected. Two independent processes with deliberately inflated Redis balance return exactly one success and one rejection. Tests also cover debt/refund persistence before flush, cap enforcement, zero increase and injected SQL failure without cache mutation. No production gate was removed.

Call-site inspection: WalletFunding.PreConsume/Settle/Refund, BillingSession extra reserve/rollback, postConsumeQuotaWithResult, taskAdjustFunding and Midjourney refund propagate or log wallet errors as before; checkin already requested synchronous DB writes. Signup invitation rewards already ignored Increase errors; this inherited behavior was not expanded or repaired. Exact production references are recorded in evidence/wallet-sync-callers.txt. Subscription paths are unchanged. This inspection and service regression do not claim provider live acceptance.
