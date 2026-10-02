# Architecture and current release boundary

Candidate only; no production approval or deployment. Native and PagePay adapters use pinned GoPay in-process. Routes use the existing session/admin middleware. No external gateway, subscription, automatic renewal, or multi-merchant routing was added.

User intent -> decimal quote/confirmation -> atomic TopUp + direct order -> signed checkout. Checkout is not payment proof. Notification verification -> normalized durable Inbox -> ACK. A worker uses the same evidence/settlement path for query observations. Order, TopUp, ledger, user credit, and event processing share one SQL transaction. Global transaction identity and ledger uniqueness enforce single credit; row locking uses lockForUpdate, and SQLite conflicts remain retryable.

The worker persists query schedule and leases; account revision bindings enforce immutable identity and a database query throttle. New-order enablement requires mounted configuration, an explicit per-user allowlist, compliance confirmation, and a durable administrator switch. History remains active when that switch closes. There are no production mock/refund routes.

Review-required events require investigation; automatic refunds are not implemented. Worker scans are bounded per batch but cumulative cache reconciliation currently scans all credited user IDs. Scale testing, metrics integration, certificate lifecycle testing, full E2E, and official integration remain release gates (see TEST_REPORT and KNOWN_LIMITATIONS).
