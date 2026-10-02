# Upgrade checks

Deployment baseline remains UNKNOWN. Do not upgrade a live deployment merely to match the development candidate.

Run scripts/directpay/upstream-impact.sh with two local full commit SHAs. Review every YAML integration point, including unchanged files whose caller semantics changed. Keep GoPay and toolchain updates isolated; inspect go.mod/go.sum rather than pulling @latest.

In an isolated worktree/test DB: create legacy TopUps and direct orders, save exact amount/quota/account snapshots, expand migrations twice, process signed fixtures and competing notification/query observations, verify one ledger/credit, then exercise the compatible prior payment binary. Run the actual DB dialect/Redis combination and frontend E2E. Retain historical revisions and prohibit pure-upstream cache hydrators after the first credit.

Current evidence: original baseline full-schema creation, candidate pre-upgrade HTTP orders, current full-binary/image migration with price changes, two-instance signed notification races, SIGKILL recovery, and creation-disabled compatible public-key image rollback have executed successfully. See LOCAL_REHEARSAL.md and container-rehearsal.log for exact limits. The actual deployed baseline remains UNKNOWN; no production schema snapshot or rollout is claimed. Certificate-mode rollback must include the final certificate-rotation fix.

Executed integration-impact example: baseline 1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5 -> implementation commit 49c584c14, exit 0; evidence/candidate-impact.txt. This demonstrates the script on actual local revisions and inventories changed touchpoints. It is not an upstream version upgrade.
