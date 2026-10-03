> Current wallet status (2026-10-03): the approved synchronous wallet fix is implemented. Earlier batch-defect/pending-approval statements below describe previous stages; the final continuation section supersedes them. Production gates remain closed.

# Release blockers and limits

This is a development candidate, NOT production ready. See LOCAL_REHEARSAL.md for expanded executed evidence.

- **Production creation is hard-blocked in code.** Fault injection reproduced inherited batch-persistence overspend after Redis cache loss: 8000 reservations admitted against 5000. Cumulative payment watermarks do not repair the entire consumption path. A durable reservation/recovery solution and relay billing validation are required before removing the gate.
- Official Alipay sandbox and WeChat/Alipay live smoke: BLOCKED_EXTERNAL (merchant credentials/product authorization/callback infrastructure/operator action absent). No real payment/refund occurred.
- Distribution Dockerfile build: BLOCKED_EXTERNAL_REGISTRY, anonymous Docker Hub 429. Go builder was patched and digest-pinned to 1.26.8 in a separate commit. Local compiler, fixture images, and all stated program tests pass; the distribution image itself is not certified.
- Certificate public-key/query/close/Native protocols, rotation, expiry and tampering now have generated-certificate real-SDK tests. Real provider-issued certificate provisioning and official acceptance remain external gates. These fixtures are neither Alipay official sandbox acceptance nor a WeChat sandbox.
- Full-browser Alipay wallet recovery and full-program SQLite two-instance/image rollback/Redis outage checks now pass. Sustained production-scale latency/account-rate tests and monitoring/alert integration are still unvalidated. No claim is made for a full-container MySQL/PostgreSQL rolling-upgrade matrix; their local accounting/controller suites pass.
- The tested older compatible rollback image uses public-key configuration and predates the final certificate rotation fix. Certificate-mode rollback needs the final verifier fix. Actual deployed revision is UNKNOWN; production backup/schema/rollout rehearsal has not been authorized or performed.
- Missing Redis payment watermarks now reject replay rather than infer zero. Pure-upstream hydrators remain unsupported; no live quota is replaced with a DB snapshot. Lifetime direct credit is bounded by MaxWalletQuota; reconciliation currently scans all credited user IDs.
- Review-required events have authenticated reconciliation actions, but a complete operational exception workflow needs further product integration. Generic admin completion is rejected for direct orders.
- P7 refunds are NOT_IMPLEMENTED and disabled. No subscriptions, auto-renewal, H5/WAP, multi-merchant routing or third-party gateway scope was added.
- No push, PR, merge, deployment, production migration/configuration or real funds operation was performed.

## Wallet diagnosis continuation

The recovered-stale-Redis synchronous reservation defect is fixed with a conditional SQL debit and cache invalidation. The Redis + batch-update cache-loss overspend remains reproducible, including across a correct direct-pay credit. See WALLET_FIX_BOUNDARY.md for exact configurations, evidence and the pending whole-site wallet persistence scope decision. Production creation stays hard-disabled.

## 2026-10-03 wallet continuation

The user subsequently approved synchronous wallet persistence. The inherited batch cache-loss counterexample is now fixed for a homogeneous candidate fleet and covered by three-engine tests; the earlier statements that it remains unimplemented are historical. Mixed legacy batch writers, unverified old queue drains, SQL ambiguous commit outcomes and non-idempotent usage refunds remain outside this fix. The old payment-only rollback rehearsal cannot validate wallet rollback. Production creation remains hard-disabled pending all remaining official, operational/load and distribution gates. See updated WALLET_FIX_BOUNDARY.md and OPERATIONS_AND_ROLLBACK.md.
