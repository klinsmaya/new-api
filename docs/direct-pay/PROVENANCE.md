# Provenance

Implementation target/baseline: klinsmaya/new-api at 1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5.
User specification: Library libfile_a5530db581588191a7d3f75014937c2a, new-api-direct-pay-claude-plan(1).md, read all 753 lines; local IMPLEMENTATION_PLAN.md verified 52,855 bytes. Library version_id is null.
No reference fork files copied or merged. Reference SHAs in the specification remain explanatory references only.
SDK candidate: github.com/go-pay/gopay v1.5.123, source examined from official Go module download. go.mod requires Go 1.25.0 and x/crypto v0.53.0 (target previously v0.52.0). Pin and record the dependency diff; never use @latest or go get -u.
Official Go compiler: go1.26.1.linux-amd64.tar.gz SHA256 031f088e5d955bab8657ede27ad4e3bc5b7c1ba281f05f245bcc304f327c987a; source https://go.dev/dl/.
Reviewed SDK docs/source on 2026-10-02: alipay/client.go, alipay/sign.go, alipay/payment_api.go, wechat/v3/client.go and notification interfaces. AutoVerifySign for Alipay documents certificate-only support; ordinary public-key responses need explicit VerifySyncSign. PagePay return is not payment evidence.
