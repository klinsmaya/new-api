# Release blockers and limits

This is a development candidate, NOT production ready.

- Official Alipay sandbox and WeChat/Alipay live smoke: BLOCKED_EXTERNAL (credentials/product authorization/callback infrastructure/operator action absent).
- Certificate-mode constructors exist; certificate expiry, platform-key rotation and full cross-revision trust-window tests are NOT_RUN. Public-key contract tests do not establish certificate readiness.
- Full browser E2E, ACK p99, sustained account-rate/load testing, two live application instances with Redis faults, full release-schema upgrade and compatible-image rollback rehearsal: NOT_RUN.
- Cache watermark requires every sharing application to be compatible before first direct credit. Mixed pure-upstream cache hydration is unsupported.
- Cumulative lifetime direct credit counter is bounded by MaxWalletQuota; capacity exhaustion goes to review. Worker cache reconciliation currently scans all credited user IDs.
- Review-required events are visible, but a complete operational workflow for resolving wallet-capacity exceptions needs further product integration. Generic admin manual completion is intentionally rejected for direct orders.
- Minimal first UI supports Native on another device and desktop PagePay; no H5/WAP/precreate, subscriptions, auto-renewal, multi-merchant routing or refund implementation.
- P7 is NOT_IMPLEMENTED and remains disabled; no refund endpoints or unsafe quota reservation placeholder exists.
- No production database, configuration, deployment, push, PR or merge has been performed.

- Local patched-toolchain scan is clean for reachable payment paths, but the unchanged Dockerfile Go 1.26.1 builder has known findings. A separately reviewed builder update is a release blocker.
- Public-key trust-window rotation now has local positive/negative coverage; this does not clear the certificate-mode lifecycle gate.
