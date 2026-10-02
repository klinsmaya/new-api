# Wallet reservation fault diagnosis and bounded fix

2026-10-02. Applies to this candidate and its inherited quota implementation. No production access or configuration change was used.

## Reproduction

Start with quota 5000; reserve 4000; delete the user Redis hash before the next reserve of 4000. The tests use isolated SQLite/PostgreSQL/MySQL databases and deterministic or real loopback Redis.

| Redis | Batch update | Direct credit between reserves | Second reservation | Final DB after batch flush |
|---|---|---|---|---|
| disabled | enabled | none | rejected | 1000 |
| enabled | disabled | none | rejected | 1000 |
| enabled | enabled | none | admitted (defect) | -3000 |
| enabled | enabled | +1000, settled and cache synchronized | admitted (defect) | -2000 |

The affected configuration is Redis enabled plus `BATCH_UPDATE_ENABLED=true`, while a wallet delta remains in the process-local queue (default flush interval five seconds). Cache eviction/restart can rehydrate the unreserved DB balance. Queue loss on process failure is a related unresolved durability problem. Direct-pay credits settle correctly and preserve cached reservations while that cache survives; their watermark cannot make the legacy consumption queue durable. This is an inherited defect, not evidence that the new credit bridge is safe under all consumption configurations.

A separate synchronous defect also reproduced: Redis unavailable causes the first reservation to use SQL; Redis recovery exposes its old 5000 balance, allowing another 4000 through the old unconditional SQL write. Before the fix the diagnostic fails with final quota -3000 (`wallet-diagnostic-before.log`, exit 1).

## Implemented minimal correction

Only the synchronous persistence path of `TryReserveUserQuota` now uses an atomic SQL `quota >= requested` condition. A stale-cache admission rejected by SQL compensates the attempted cache delta, invalidates the stale hash, and reports ordinary insufficient balance. Deleted users retain their previous not-found error behavior. No unconditional settlement/debt or refund logic was changed. Batch, token and statistics paths are unchanged.

Two independent processes are given inflated Redis balance 12000 against SQL balance 5000. Exactly one 4000 reservation must succeed, the other must be rejected, and DB/cache recover to 1000. This tests caller outcomes, not only the final SQL value. Real-Redis outage/recovery and the configuration matrix are included. SQL-commit-unknown handling and durable usage settlement remain outside this narrow correction.

## Remaining decision: batch mode

The narrow correction does **not** repair enabled batch consumption or mixed fleets containing batch writers. Production new direct-pay orders remain hard-disabled; historical notifications and settlement remain active.

The smallest complete persistence change identified touches three user-wallet entrypoints: `persistUserQuotaDelta`, `IncreaseUserQuota`, `DecreaseUserQuota`. They would synchronously persist user wallet changes even when token/statistics batching remains enabled. Pre-reservation must retain its conditional balance guard; actual usage settlement must preserve existing permitted debt and refund/cap behavior. It requires a full-node drain and successful old-queue flush, prohibits mixed old batch writers, and adds SQL write load and SQL availability dependency to consumption/refunds. These are whole-site wallet semantics, so this larger step is pending parent/user scope confirmation under the instruction to avoid unrelated wallet reconstruction.

Recommended option: authorize this narrowly bounded user-wallet persistence change, followed by failure/concurrency/relay billing tests and independent review; keep the production gate until all remaining acceptance gates pass. Alternative: durable reservation journal and queue recovery, a materially larger design requiring its own scope. Until either is approved and verified, retain the release block. Merely disabling batch in a production environment is not performed or claimed here.

## Verification

Go 1.26.8; tests and command exit codes are appended to TEST_REPORT.md. Fixture credentials are disposable local test values. No official gateway or real money was used.
