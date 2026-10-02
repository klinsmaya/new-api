# Operations and rollback

First release: validate schema in disposable DB -> deploy compatible code with new orders disabled -> mount isolated configuration -> validate callbacks/worker -> allowlist -> operator-authorized external tests -> review acceptance evidence. Never publish an unverified binary or assume the development HEAD is deployed.

Rollback: reliably persist directpay.create_enabled=false and verify it; retain callbacks, Inbox, original account revisions and query/cache reconciliation. A write failure returns 503, not success. Retain all additive tables and users.direct_pay_credit_total. Never restore an older full DB snapshot to undo code, never DROP payment tables, and never share a Redis namespace with a pure-upstream hydrator after direct credits exist. On first release use stop-new-orders and forward repair because no compatible older payment binary exists.

Inspect pending/quarantined direct_pay_events, review_required direct_pay_orders, earliest next_query_at and unknown/paid-uncredited age. Fixed warning categories signal Inbox retry, query persistence or cache reconciliation. Metrics/alert integration and ACK p99 load measurement remain NOT_RUN.

Evidence mismatch: preserve event, compare immutable identity/amount with authenticated provider query and official merchant records. Never edit quota or bypass ledger uniqueness. Wallet capacity/deleted user: preserve payment proof and investigate; no automatic refund is issued. Missing revision: restore the exact authorized historical configuration, never repoint old orders to another merchant. Operator refunds require a separately approved process and quota disposition; P7 is not implemented.

Daily statement comparison is a manual operator procedure using merchant transaction IDs vs ledger/order snapshots; polling is not bank reconciliation. No production SQL commands are supplied. Actual compatible-image rollback rehearsal remains NOT_RUN.
