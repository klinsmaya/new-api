# Expanded local acceptance — 2026-10-02

This record supersedes earlier NOT_RUN entries only for the exact scenarios below. It does not clear production acceptance.

## Full program / image / browser

`node scripts/directpay/full-app-rehearsal.mjs` and `DIRECTPAY_CONTAINER=1 node scripts/directpay/full-app-rehearsal.mjs` both exited 0. The latter uses three locally built Linux amd64 fixture images, two concurrent application containers, a dedicated Redis container and a disposable SQLite database. These are full application binaries with the embedded production frontend, not handler-only servers. Images contain the binary, host libc/loader and zoneinfo; they are NOT distribution/release images. Identities and binary hashes: evidence/rehearsal-image-identities.txt.

- Original baseline 1a4166d8 initializes its actual full schema; direct-pay tables are verified absent.
- Candidate 796f44088 migrates and creates three orders over authenticated HTTP.
- Current payment implementation 0062e0834 migrates/restarts, while current price is changed to 99. Two full servers accept 40 duplicate cryptographically signed callbacks concurrently; exactly one ledger credits the original snapshot, never current pricing.
- After a second order's durable ACK, one server is SIGKILLed; the other settles exactly once.
- A development rollback binary built from 1d40b4abc runs against the upgraded DB with create disabled. A third late paid order settles using its old snapshot. No tables are dropped or restored.
- Chromium / Playwright-core 1.57.0 exercises real refresh-cookie login and wallet UI, restore/reload, committed-create response abort and same-key resume, blocked popup, expiry, and login loss. No API success is fabricated in the browser. Payment popup HTTPS requests are blocked.
- Redis is actually stopped. The inherited global limiter returns 500, never a false ACK; after restart, the signed callback is retried. After durable ACK, Redis is stopped again before credit; the DB settles while Redis is down. On restart a previously persisted 700-quota reservation remains deducted and cumulative credit is applied once.

All account/signing materials are generated local fixtures. Go HTTP(S) gateway requests use a refused loopback proxy; browser HTTPS requests are aborted. Docker uses a dedicated bridge with loopback-only published ports, not host networking. The initial internal-network attempt could not expose services to the host test client; the final isolated bridge plus gateway proxy avoids altering host network/security settings. No official sandbox or live gateway is contacted.

Local 40-callback latency is in the log. It is not a sustained-load or production p99 SLO result. SQLite full-program concurrency complements real MySQL/PostgreSQL model/controller tests; full-container MySQL/PostgreSQL rolling upgrades were not claimed.

## Reproduction

Prerequisites: Linux amd64, official Go 1.26.8, Node 24, installed locked frontend tools, Chromium, Playwright-core 1.57.0 (set DIRECTPAY_PLAYWRIGHT to its index.mjs), Docker, Python3. Build web/dist with the repository's frontend build first. Import the supplied local Git bundle if intermediate candidate commits are not available. Run `scripts/directpay/prepare-rehearsal.sh`; its operations were also executed individually for the recorded image builds.

Create a dedicated fixture Redis named `newapi-directpay-test-redis`, published at **127.0.0.1:16379**, and a Docker bridge named `directpay-rehearsal`; connect only fixture containers to it. Redis image used: public.ecr.aws/docker/library/redis:7-alpine (7.4.11). The runner flushes **only database 7 of that named test Redis**, never arbitrary configured Redis. It creates private fixtures beneath .directpay-tools, generates fresh keys/passwords without printing them, and stops its application containers in finally. Remove the fixture Redis/network/images afterward. Do not point this harness at shared or production services.

`DIRECTPAY_CONTAINER=1 node scripts/directpay/full-app-rehearsal.mjs` exercises image replacement. The fixture older rollback covers the tested Alipay public-key configuration; it predates the final certificate rotation fix and is not an approved certificate-mode rollback target. Certificate-mode rollbacks must include the final rotation fix and pass the certificate protocol suite. Production migration remains UNKNOWN and unauthorized.

## Other exact commands / exits

- Go 1.26.8 `go test ./service/directpay/... -count=1 -v`: 0; public-key/certificate SDK query, signed close (including WeChat 204), Native creation, tampering, unknown serial, expiry/not-yet-valid, old-expired/new-valid query+notify, old-key rejection and cross-revision rejection.
- Dedicated PostgreSQL/MySQL DSNs, `go test ./model ./controller -p 1 -run DirectPay -count=1 -v`: each 0; includes absolute wallet capacity and rejection above the limit.
- Real Redis fault-hook test: 0; server executes Lua, client reports lost response, retry does not double credit; out-of-order cumulative total; legacy hydrator lacks watermark and is refused.
- Focused `go test ... -race`: 0; evidence/expanded-race.log.
- Full affected regression `go test ./model ./controller ./service/directpay/... -p 1 -count=1`: 0. An earlier run failed an existing account-deletion concurrency test; isolated repetition 5 times and final full run passed. Both failure and follow-up are retained, not hidden.
- Frontend typecheck, scoped lint, build: each 0. Vitest payment + theme/cache suite: 11/11, exit 0.
- Official Dockerfile `docker build --target builder2`: exit 1, Docker Hub anonymous pull 429. Builder version and multiarch digest are verified and pinned; complete distribution-image build remains externally blocked by registry access, not marked passing.

## Safety finding that blocks release

`TestDirectPayBaselineBatchLossCounterexample` deliberately reproduces a failure: with batch persistence enabled, reserve 4000 against DB 5000, delete Redis before batch flush, then another 4000 is accepted. **A passing characterization test means the unsafe behavior was reproduced, not fixed.** No DB balance was manually adjusted before cache loss. This inherited consumption-path issue requires a separate durable-reservation / batch recovery fix and full relay billing validation. Production new-payment creation is now hard-blocked both in CanCreate and the admin enable endpoint, even if configuration switches request enablement. Notifications and historical settlement stay active. Sandbox tests must use isolated data and no real funds.

2026-10-03 continuation: the approved synchronous-wallet change replaces the historical `TestDirectPayBaselineBatchLossCounterexample` with `TestDirectPayBatchLossReservationSafety`; the second spend is now rejected. Current engine/failure/concurrency evidence is in TEST_REPORT.md under the approved continuation. Earlier full-image upgrade/rollback results remain scoped to their original commits and do not demonstrate deployment of this wallet fix. The new full-node drain/no-old-batch rollback requirements are in OPERATIONS_AND_ROLLBACK.md. Production creation is still hard-blocked.
