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
