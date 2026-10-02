# Upgrade checks

Deployment baseline remains UNKNOWN. Do not upgrade a live deployment merely to match the development candidate.

Run scripts/directpay/upstream-impact.sh with two local full commit SHAs. Review every YAML integration point, including unchanged files whose caller semantics changed. Keep GoPay and toolchain updates isolated; inspect go.mod/go.sum rather than pulling @latest.

In an isolated worktree/test DB: create legacy TopUps and direct orders, save exact amount/quota/account snapshots, expand migrations twice, process signed fixtures and competing notification/query observations, verify one ledger/credit, then exercise the compatible prior payment binary. Run the actual DB dialect/Redis combination and frontend E2E. Retain historical revisions and prohibit pure-upstream cache hydrators after the first credit.

Current evidence: tests rerun migrations and change current quota conversion after creating an order, then settle the immutable original quota once. This is a local migration/snapshot simulation, not an actual two-image rolling upgrade. Full released-schema snapshot and compatible-container rollback rehearsal remain NOT_RUN and block production acceptance.

Executed integration-impact example: baseline 1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5 -> implementation commit 49c584c14, exit 0; evidence/candidate-impact.txt. This demonstrates the script on actual local revisions and inventories changed touchpoints. It is not an upstream version upgrade.
