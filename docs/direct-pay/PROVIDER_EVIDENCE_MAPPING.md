# Evidence mapping

| Observation | Amount | Identity | Success |
|---|---|---|---|
| WeChat notify | decrypted amount.total + currency (never payer_total) | appid, mchid, out_trade_no, transaction_id; route account/revision/environment | SUCCESS + NATIVE; signed raw body then AES-GCM |
| WeChat query | amount.total + currency | response appid/mchid/order; authenticated bound client | SUCCESS; GoPay synchronous signature check |
| Alipay notify | strict decimal total_amount to CNY minor units | app_id, seller_id, out_trade_no, trade_no; bound account/revision/environment | TRADE_SUCCESS / TRADE_FINISHED; RSA2 only |
| Alipay query | strict total_amount; reject non-CNY trans_currency when returned | out_trade_no and authenticated application/account context; QueryBoundIdentity=true, not falsely presented as returned seller fields | TRADE_SUCCESS / TRADE_FINISHED |

Alipay query uses GoPay PostAliPayAPISelfV2 to retain the exact signed JSON, including signed business errors, then explicit VerifySyncSign/VerifySyncSignWithCert. The higher-level TradeQuery returns before SignData extraction for errors in v1.5.123. An unsigned TRADE_NOT_EXIST is never authoritative. Signed not_found only closes an unvisited PagePay checkout after its absolute expiry; late valid successful evidence still settles the original snapshot.

PagePay signed URL and return_url never credit. POST form duplicate parameters are rejected; query parameters are not merged. WeChat unknown serial, stale signature timestamp, SIGNTEST, modified body, missing amount, bad AES-GCM or identity all fail closed. Callback body maximum is 64 KiB. SDK debug remains off; provider errors are mapped to fixed categories instead of returned/logged raw payloads.

Pinned public-key and certificate constructors exist. Local contract evidence currently covers public-key mode only; certificate validity/rotation matrices remain NOT_RUN and block claiming complete certificate-mode acceptance.
