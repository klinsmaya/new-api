package provider

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	dp "github.com/QuantumNous/new-api/types/directpay"
	"github.com/go-pay/gopay/pkg/xhttp"
	wx "github.com/go-pay/gopay/wechat/v3"
)

type verificationTransport struct {
	adapter *Adapter
	next    http.RoundTripper
}

func (t verificationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(r)
	if err != nil || response == nil {
		return response, err
	}
	// Reject unknown serials before GoPay can automatically download certificates.
	// The SDK still performs its own verification after this trust-boundary check.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	_ = response.Body.Close()
	if err != nil || len(body) > 1<<20 || t.adapter.verifyWechat(response.Header, body) != nil {
		return nil, dp.ErrEvidence
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}
func (a *Adapter) setWechatTransport(transport http.RoundTripper) {
	a.wx.SetHttpClient(xhttp.NewClient().SetTransport(verificationTransport{adapter: a, next: transport}))
}
func (a *Adapter) verifyWechat(h http.Header, body []byte) error {
	if a.cfg.VerificationMode == "certificate" {
		if _, err := a.selectedCertificate(h.Get("Wechatpay-Serial")); err != nil {
			return dp.ErrEvidence
		}
	}
	stamp, err := strconv.ParseInt(h.Get("Wechatpay-Timestamp"), 10, 64)
	if err != nil || stamp < time.Now().Unix()-300 || stamp > time.Now().Unix()+300 || a.verifiers[h.Get("Wechatpay-Serial")] == nil || h.Get("Wechatpay-Nonce") == "" || strings.HasPrefix(h.Get("Wechatpay-Signature"), "WECHATPAY/SIGNTEST/") {
		return dp.ErrEvidence
	}
	if err := wx.V3VerifySignByPK(h.Get("Wechatpay-Timestamp"), h.Get("Wechatpay-Nonce"), string(body), h.Get("Wechatpay-Signature"), a.verifiers[h.Get("Wechatpay-Serial")]); err != nil {
		return dp.ErrEvidence
	}
	return nil
}
