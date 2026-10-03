# Independent read-only review

Reviewer: separately delegated read-only agent, instructed to read AGENTS/CLAUDE/billing first, perform no edits and not run shared-workspace tests. First reviewed HEAD 126bdfcf249a4c450f9a329015d5aecad68a4bcc plus candidate working changes.

First findings and corrections:
1. Stop switch inherited UpdateOption's ignored DB errors -> explicit checked upsert; injected failure returns 503.
2. Unknown signed order occupied Inbox head forever -> quarantine; retryable failures get next_attempt_at.
3. Missing historical provider revisions blocked first query batch -> startup validation, immutable bindings, missing-account retry scheduling.
4. MySQL reserved key in raw predicate -> GORM struct conditions; real MySQL controller test.
5. Quote-changed response left persistent purchase stuck -> explicit price_changed code and safe re-quote recovery.
6. Unvisited PagePay has no provider trade -> SDK SelfV2 retains exact signed error JSON; verified not_found only closes after checkout expiry; late paid event still settles.

Second review confirmed those corrections and reported no newly identified severe funds/security defect. A suggested extra lease predicate was applied (schedule due and not credited at CAS time). It explicitly did not certify runtime tests or production readiness.

Third incremental review requested for lease tightening, controlled WeChat verifier-key map and authenticated administrator reconciliation. See final handoff for that result. Pending tests/operational gates remain in TEST_REPORT; static review does not waive them.

Third incremental review completed: no severe funds/secret exposure issue found in the added CAS guard, Serial-selected verifier map, credential fingerprint or admin reconciliation path. Reviewer confirmed trusted_verification_keys is deliberately outside immutable fingerprint as an operator-controlled trust-window update; removal/revocation must remove that key as well. Reviewer did not execute tests. Public-key rotation contract tests subsequently passed locally.


## Follow-up independent read-only review

Reviewer identified and confirmed two further P1 issues: (1) old-version hydration with no cumulative watermark could double credit cache; (2) one expired primary certificate rejected valid rotated anchors and could block historical startup/settlement. Both were repaired and covered by explicit counterexamples. Final review found no new severe direct-pay funds/secret defect. A lower-priority certificate-ID case mismatch was resolved by strictly rejecting noncanonical configured IDs. Production runtime and admin enable endpoint both deny activation. Reviewer did not run tests; actual executed evidence is separately recorded in LOCAL_REHEARSAL.md.

## Bounded synchronous wallet correction review — 2026-10-02

Independent agent `review_directpay` read the final uncommitted quota guard and tests without writing files. It found no new severe funds issue in this narrow change; verified unchanged batch branch, conditional synchronous SQL admission, distinct insufficient/deleted-user outcomes and exactly-one-success two-process assertions. It explicitly excludes mixed batch nodes and the inherited batch-cache-loss defect. Reviewer did not execute tests; the primary agent's database, real Redis, regression and race runs are recorded separately in TEST_REPORT.md. Whole-site synchronous wallet persistence remains pending scope confirmation. Production creation remains hard-disabled.

## Approved wallet persistence independent review — 2026-10-03

Non-author reviewer `review_directpay` re-read AGENTS and billing rules, inspected the three-entrypoint diff and full consumption/refund caller chain. First pass found a MySQL zero-increase changed-rows regression in exact preconsume/actual settlement. Fixed by an explicit zero no-op after validation; final three-engine tests include it. Final independent read-only review found no remaining severe issue introduced by this change. It confirmed unchanged token/stat batching, SQL guarded pre-admission, preserved debt/refund caps and the no-mixed-fleet/drain/rollback/load requirements. The reviewer did not execute tests. Its historical-doc ambiguity note was addressed by marking the previous scope decision historical and adding current status at the top. SQL unknown commit and non-idempotent usage refund limitations remain explicit.
