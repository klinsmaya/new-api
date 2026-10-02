# Official integration — BLOCKED_EXTERNAL

No merchant/sandbox credentials were supplied or read. No live payment or refund was made. Local RSA/AES-GCM fixtures are PASS_CONTRACT, never WeChat sandbox.

Alipay L2: operator provisions a separate official sandbox application/product, seller/buyer and client per current console guidance; mounts sandbox keys, configures a stable HTTPS callback and return URL, enables an allowlisted disposable user. Operator pays in the official client. Record order ID, provider transaction, expected minor amount/quota, durable event, exactly one ledger and displayed credited state. Repeat duplicate notify, loss of return, callback outage followed by query recovery, unused/expired checkout and wrong-key rejection. Store only redacted evidence. Until performed: BLOCKED_EXTERNAL.

WeChat L3: Native live merchant product authorization, bound appid, signer, verifier and APIv3 key are required in isolated staging. An operator scans/pays a separately approved small test product. No real payment is automated. Verify callback, independent query, Inbox/ledger/wallet and consistency before declaring PASS_LIVE_SMOKE. WeChat API v3 mock is not an official sandbox.

External callback provisioning, official account setup, live money and production deployment are outside this authorization. Never disable signature/TLS verification to unblock them.
