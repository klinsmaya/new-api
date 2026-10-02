# Provenance

Implementation target/baseline: klinsmaya/new-api at 1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5.
User specification: Library libfile_a5530db581588191a7d3f75014937c2a, new-api-direct-pay-claude-plan(1).md, read all 753 lines; local IMPLEMENTATION_PLAN.md verified 52,855 bytes. Library version_id is null.
No reference fork files copied or merged. Reference SHAs in the specification remain explanatory references only.
SDK candidate: github.com/go-pay/gopay v1.5.123, source examined from official Go module download. go.mod requires Go 1.25.0 and x/crypto v0.53.0 (target previously v0.52.0). Pin and record the dependency diff; never use @latest or go get -u.
Official Go compiler: go1.26.1.linux-amd64.tar.gz SHA256 031f088e5d955bab8657ede27ad4e3bc5b7c1ba281f05f245bcc304f327c987a; source https://go.dev/dl/.
Reviewed SDK docs/source on 2026-10-02: alipay/client.go, alipay/sign.go, alipay/payment_api.go, wechat/v3/client.go and notification interfaces. AutoVerifySign for Alipay documents certificate-only support; ordinary public-key responses need explicit VerifySyncSign. PagePay return is not payment evidence.

Dependency review additions:
- GoPay v1.5.123 LICENSE is Apache-2.0 (read locally from the verified module); no reference-fork implementation copied.
- Direct imports: gopay v1.5.123 and go-pay/crypto v0.0.3. Transitives include errgroup v0.0.3, smap v0.0.2, util v0.0.4, xlog v0.0.3, xtime v0.0.2. See evidence/dependency-diff.patch and go.sum.
- Candidate SDK selected x/crypto 0.53.0, x/sync 0.21.0, x/sys 0.46.0, x/text 0.38.0. Vulnerability scanning identified GO-2026-5970; x/text is separately fixed at 0.39.0 in commit 23329aacd.
- Initial compiler scan (Go 1.26.1): 16 reachable reports across stdlib and x/text; evidence/dependency-vulnerabilities.log. This was not treated as acceptance.
- Official local verification compiler upgraded to Go 1.26.8 after checking go.dev release metadata; archive SHA256 d0f743b33e8d8945e6b1f432edd15785c70507121d6e2a723b21285eddf8b57b. govulncheck v1.1.4 then reported zero reachable vulnerabilities (one imported-package and seven required-module findings remain outside reported reachable calls). Scope was ./service/directpay/..., not the entire application.
- Repository Dockerfile still pins Go 1.26.1. No production/CI image was changed. Release must separately update/review the builder pin; local scan does not make the old builder safe.
