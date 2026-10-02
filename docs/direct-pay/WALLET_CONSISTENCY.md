# Wallet consistency

Database credit atomically increments users.quota and users.direct_pay_credit_total. This cumulative counter is bounded by MaxWalletQuota to preserve exact Lua/JavaScript arithmetic; reaching the lifetime counter limit requires review, not wraparound. It is not a bank settlement ledger.

A fresh Redis user hash hydrates Quota and DirectPayCreditTotal together from the same User snapshot. Existing hashes retain both, preserving consumption reservations. Reconciliation applies only (DB cumulative credit - hash cumulative credit) inside a single Lua invocation, updating quota and watermark atomically. Duplicate/unknown-result Redis calls can be replayed; after rebuild the watermark already reflects included credits. No direct payment path deletes a live hash, overwrites its quota, or blindly retries a naked INCR.

Crash after DB commit: ledger remains durable; periodic worker reconciles all credited users. Crash/timeout during Lua: retry computes zero or the missing delta. Missing cache: no partial hash is created, normal hydration supplies both fields. A delayed pre-credit hydration is repaired by periodic reconciliation. A Redis outage leaves DB authoritative and emits a fixed warning; no claim of SQL+Redis atomic commit is made.

Tests cover reservation preservation, duplicate replay and rebuild across SQLite/MySQL/PostgreSQL; PostgreSQL uses real Redis 7. Separate tests exercise two SQL processes. Full two-live-application Redis failover, batch-flush crash and all cache eviction/reservation races remain NOT_RUN; existing upstream fallback/batch-consumption windows are not claimed solved.

IMPORTANT upgrade fence: all writers/hydrators sharing this Redis namespace must contain this patch before enabling the first direct payment. A pure-upstream process could hydrate Quota without the watermark and permit double credit on subsequent replay. Such mixed deployment/rollback is unsupported. Stop new orders before rollout; use only compatible binaries for history and cache hydration. Do not revert to an old snapshot or delete payment tables.
