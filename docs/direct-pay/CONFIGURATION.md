# Configuration (no credentials in this repository)

DIRECTPAY_CONFIG_FILE points to an operator-mounted, read-only JSON Secret. The path is set by the deployment operator, never through HTTP/UI. Leave it unset for a fresh installation with no direct-pay history. Existing outstanding history requires its account revisions at startup.

Shape (values shown are placeholders, not a runnable merchant account):

```json
{
  "deployment_tier": "staging",
  "base_url": "https://PAYMENT-STAGING.EXAMPLE",
  "create_enabled": false,
  "max_money_minor": 10000,
  "allowed_users": [],
  "accounts": [{
    "current": true,
    "config": {
      "provider": "alipay_direct",
      "environment": "sandbox",
      "account": "alipay-primary",
      "revision": "r1",
      "app_id": "OPERATOR_SUPPLIED",
      "merchant_id": "SELLER_ID",
      "verification_mode": "public_key",
      "private_key": "SECRET_MOUNT_ONLY",
      "public_key": "TRUSTED_ALIPAY_PUBLIC_KEY"
    }
  }]
}
```

WeChat: provider=wechat_direct, environment=live, merchant_id=mchid, app_id=bound appid, serial=merchant certificate serial, api_v3_key=32-byte secret, public_key_id=unaltered PUB_KEY_ID_… in public-key mode, public_key=trusted verifier. Certificate mode uses a certificate/serial instead. Alipay certificate mode additionally uses app_certificate and root_certificate with public_key containing the platform certificate. Do not paste credentials into reports or UI. TLS is never disabled.

production accepts live account configuration only but new-order creation is currently hard-blocked pending wallet safety repair; local/ci reject live; no mock provider is registered. Sandbox/live require separate database, Redis namespace and domain. Current implementation supports one merchant/environment per provider with immutable revision identities, not merchant routing. Rotation requires adding a revision and retaining needed historical revisions. See limitations on verification-key rotation before use.

All three new-order gates default closed: mounted create_enabled, nonempty explicit allowed_users, and DB directpay.create_enabled. UI can toggle only the DB gate. Existing compliance confirmation is also required. Notifications, Inbox and query compensation do not consult the new-order gate. Refund capability is absent/disabled.

WeChat controlled platform public-key rotation: add trusted_verification_keys as an ID -> trusted PEM mapping on each historical account revision that must receive the new key. Unknown IDs remain rejected; primary key cannot be duplicated in the map. This is an operator-controlled trust window, not automatic untrusted certificate download. Public-key and generated-certificate expiry/rotation are locally tested. Certificate-mode trusted_verification_keys also applies to Alipay (canonical GetCertSN IDs) and WeChat (uppercase certificate serials). Malformed/noncanonical IDs are rejected. Expired anchors cannot verify signatures, but do not prevent a retained revision from using a provisioned active next anchor. Signer/application/root validity is checked separately. Unknown WeChat response serials are rejected before the SDK can download a certificate. These local tests do not establish official merchant acceptance. The same trusted keys are loaded into the SDK response verifier map. Emergency revocation must remove compromised trust immediately and halt affected automatic processing; do not preserve a compromised key simply to avoid operational work.
