# Test report — development candidate, 2026-10-02

Baseline: klinsmaya/new-api 1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5. Branch feat/direct-pay. Initial tree clean. Production deployment UNKNOWN; no production action.

## Initial candidate evidence (superseded where expanded below)

| Check | Command (repository root unless noted) | Exit/result | Evidence |
|---|---|---|---|
| Original affected backend baseline | scoped Go 1.26.1 `go test ./model ./controller ./service` | 0, PASS_UNIT | evidence/baseline-go.log |
| Original frontend typecheck | `web/node_modules/.bin/tsgo -b web/tsconfig.json` | 0 | BASELINE.md |
| SQLite two-process money tests | `go test ./model -run DirectPay -count=1 -v` | 0, PASS_UNIT | evidence/p2-sqlite.log |
| MySQL + Redis | isolated loopback fixture env, `go test ./model ./controller -p 1 -run DirectPay -count=1 -v` | 0, PASS_UNIT | evidence/final-mysql.log |
| PostgreSQL + Redis | equivalent isolated PostgreSQL fixture, same command | 0, PASS_UNIT | evidence/final-postgres.log |
| Real SDK/local cryptography | `go test ./service/directpay/... -v` | 0, PASS_CONTRACT / PASS_UNIT | evidence/p3-contract.log |
| Backend regression | `go test ./model ./controller ./service/directpay/...` | 0, PASS_UNIT | evidence/final-go.log |
| Patched compiler regression | Go 1.26.8 `go test -p 2 ./model ./controller ./service/directpay/...` | 0, PASS_UNIT | evidence/final-go-1.26.8.log |
| Race checks | `go test ./model ./service/directpay/... -run 'DirectPay\|SDKContract\|MoneyBoundaries' -race -count=1` | 0 | evidence/final-race.log |
| Frontend user state regression | web: `node node_modules/vitest/vitest.mjs run src/features/wallet/direct-pay/__tests__/payment.test.tsx` | 0; 3 cases | evidence/p4-ui-tests.log |
| Frontend typecheck/lint | web: `node_modules/.bin/tsgo -b`; scoped oxlint | 0 / 0 | evidence/p4-typecheck.log, p4-lint.log |
| Frontend build | web: `node node_modules/@rsbuild/core/bin/rsbuild.js build` | 0 | evidence/p4-build.log |
| Root binary build | `go build -o .directpay-tools/new-api .` | 0 | evidence/final-build.log |
| Payment dependency scan | Go 1.26.8, `go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./service/directpay/...` | 0, no reachable findings; scope limits below | evidence/dependency-vulnerabilities-remediated.log |

Toolchains: original Go 1.26.1, patched local Go 1.26.8, Node 24.19.0, existing locked web/node_modules. Bun is not installed; Node executed the installed repository tools directly, without changing web/bun.lock. Databases: PostgreSQL 16.15, MySQL 8.0.46, Redis 7.4.11; SQLite driver modernc.org/sqlite v1.40.1. No minimum-version claim is inferred from this matrix.

Coverage includes request-key replay/conflict, same transaction on another order, two independent SQL processes each replaying an event 100 times, bad amount/currency/app/merchant/provider/environment/revision, rollback after ledger insertion, changed-current-pricing vs original quota, repeat migration, in-flight cache reservation preservation, duplicate watermark replay and hydration, database-failed stop switch, other-user order reads, malformed decimal, real SDK signed queries and tamper rejection, WeChat AES-GCM/SIGNTEST/unknown key and controlled public-key rotation, expired uncreated PagePay + late payment with creation disabled. Local fixtures have no merchant credentials and no network payment transport.

## Review

Independent read-only first review found: Inbox starvation; missing revision scheduling starvation; unquoted MySQL key; ignored DB error in stop switch; quote-change recovery; PagePay nonexistent-order lifecycle. These were fixed. Independent second review found no new severe funds/security issue; tightened the suggested stale-batch lease CAS afterward. Latest incremental review is recorded in REVIEW.md when available. Static review is not runtime acceptance.

## Initial candidate gaps (historical; see continuation below)

- Official Alipay sandbox: BLOCKED_EXTERNAL.
- WeChat live Native and Alipay live smoke: BLOCKED_EXTERNAL.
- Certificate-mode lifecycle/expiry/rotation matrix: NOT_RUN (public-key tests are insufficient).
- Browser E2E including popup/login-loss/network-abort UX: NOT_RUN; component tests are not E2E.
- Full previous-release schema snapshot migration, compatible-container rolling upgrade and actual image rollback drill: NOT_RUN. Existing snapshot test is a local simulation only.
- Two running full application instances with Redis network/crash fault injection, capacity at absolute wallet boundary, ACK p99/load and monitor alert integration: NOT_RUN.
- Refund P7: NOT_IMPLEMENTED, disabled. T23/T24 are not marked passing.
- Repository Docker builder remains Go 1.26.1 with reachable vulnerability findings. Separate reviewed builder upgrade required before release. The patched local scan does not clear all application/dependency scope.

No push, PR, merge, deployment, production migration, real payment or refund occurred. G1/G2 remain NOT ACCEPTED.

Final source verification: scripts/directpay/test.sh passed with Go 1.26.8 (evidence/final-local-entrypoint.log); root Go 1.26.8 build passed (evidence/final-build-1.26.8.log). Source-only pattern scan found no PEM private-key block or AWS access-key pattern. This is a limited scan, not a blanket secret-detection certification. git diff --check passed. The candidate-impact script executed successfully from the fixed baseline to candidate commit 49c584c14; evidence/candidate-impact.txt is an implementation integration diff, not proof of an upstream upgrade.

Phase accounting: P0 completed; P1/P2 core implemented with the recorded database tests; P3/P4 candidate implementation and public-key/component/build checks completed, but their full acceptance matrices are not complete. P5 official integration is externally blocked; P6 tooling/runbooks and snapshot simulation delivered, actual compatible-image upgrade/rollback not executed. These incomplete local gates are outstanding work, not attributed to missing merchant credentials. P7 remains separate and unimplemented.

Final patched-toolchain database rerun: Go 1.26.8 `go test -p 1 ./model ./controller -run DirectPay -count=1` passed on MySQL and PostgreSQL, both exit 0 (evidence/final-mysql-1.26.8.log and final-postgres-1.26.8.log). MySQL rerun used real Redis; PostgreSQL rerun used the deterministic Redis fixture, with earlier real-Redis evidence retained. Latest UI regression rerun also passed 3/3, exit 0.


## Continuation acceptance and current gates

The follow-up request completed the formerly unrun local certificate SDK, real browser, two-full-instance concurrency, original-schema migration, actual fixture-image replacement/compatible rollback, Redis stop/restart, unknown-result replay and absolute-capacity tests. Exact commands, exit codes, artifacts, scope limits and reproduction are in LOCAL_REHEARSAL.md. The current limitations in KNOWN_LIMITATIONS.md supersede the initial gap list above. Source implementation is 0062e0834 plus the additional Native protocol test; Go builder patch is e77106090 and cache/production gate is 1d40b4abc.

Important new outcome: a **confirmed inherited batch/cache-loss overspend** is reproduced by a characterization test. Production creation is hard-blocked in both runtime and admin API; historical settlement remains active. This is a release blocker, not a passing wallet-safety result. The independent reviewer identified missing-watermark replay and expired-primary rotation defects; both were fixed, tested and reviewed again. No new severe direct-pay funds/key defect was found in the final read-only review.

Final executed evidence: expanded-postgres.log / expanded-mysql.log (each exit 0), certificate-contract.log (exit 0), expanded-race.log (exit 0), final-safety-regression.log (exit 0), expanded-typecheck.log / expanded-lint.log (0), expanded-ui.log (11/11, 0), browser-build.log (0), full-app-rehearsal.log / container-rehearsal.log (0). Fixture image builds are 0. Official Dockerfile builder target is exit 1 due anonymous registry 429; see official-dockerfile-build.log.

P0 implementation baseline, P1-P4 candidate code and expanded local checks, P6 local upgrade/rollback tooling and executed rehearsals have deliverables. P5 official integration remains BLOCKED_EXTERNAL. G1/G2 remain NOT ACCEPTED because the inherited wallet safety finding, operational/load gates, official provider acceptance and distribution-image build are not cleared. This does not claim full production P0-P6 acceptance.
