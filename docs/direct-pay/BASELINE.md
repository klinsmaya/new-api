# Direct payment baseline — 2026-10-02

- Authorized target: https://github.com/klinsmaya/new-api.git (origin fetch/push).
- Initial branch: work; clean tree. Implementation branch: feat/direct-pay.
- Full candidate HEAD: 1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5.
- Deployment commit/image/database/Redis/multi-instance topology: UNKNOWN. No production access or deployment performed.
- No upstream remote configured; module identity remains github.com/QuantumNous/new-api. No remote rewritten.
- Read root AGENTS.md, CLAUDE.md, .agents/rules/billing.md, web/AGENTS.md and shadcn-ui SKILL.md. No separate CONTRIBUTING file found.
- Go module: 1.25.1; Docker builder pins Go 1.26.1; web Docker builder pins Bun 1.4.0. React 19, TypeScript, Rsbuild, web/src (no alternate theme copied).
- Host /usr/bin/go is not the Go compiler (go version exits 1). Official Go 1.26.1 downloaded to ignored .directpay-tools; caches scoped there. No host configuration changed.
- Node v24.19.0; Bun absent on PATH. Existing web/node_modules usable. `web/node_modules/.bin/tsgo -b web/tsconfig.json`: exit 0.
- First Go test attempt failed before compilation because default cache location is read-only; rerun uses scoped GOPATH/GOCACHE. See evidence/baseline-go.log.
- Docker daemon 28.4.0 available, initially no images. Only isolated disposable test databases may be created.

## Integration points observed

- router/api-router.go: session-authenticated self routes and admin routes; controller/payment_compliance.go and operation_setting.IsPaymentComplianceConfirmed preserve compliance gating for new orders.
- model/topup.go: RechargeEpay already uses transaction/row lock, creditTopUpQuota enforces wallet ceiling through conditional UPDATE. New payment must preserve this, not inherit conclusions from another commit.
- controller/topup.go: getPayMoney uses decimal intermediate then float; token display rounds stored TopUp.Amount down to whole units. New path must snapshot decimal pricing and final quota.
- model/user_cache.go: postcommit syncCreditUserQuotaCache is a non-idempotent delta and only logs failure. It cannot be an at-least-once cache outbox operation.
- model/quota_reserve.go: Redis pre-reservation, optional batched DB persistence, and database fallback. Never overwrite live cached quota with a DB balance.
- model/user_auth_cache.go: hydration preserves an existing Quota hash field. Any direct-pay replay watermark must be initialized atomically with a fresh quota snapshot.
- model/topup.go ManualCompleteTopUp: currently generic admin completion recalculates quota; direct orders require rejection/routing to provider verification.
- model/main.go AutoMigrate: SQLite/MySQL/PostgreSQL primary DB; separate log DB. Expansion only; no production migration run.

Official integration: BLOCKED_EXTERNAL until separately provisioned sandbox/live accounts, approved products, trusted HTTPS callbacks, and operator payment are available.
