package provider

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	dp "github.com/QuantumNous/new-api/types/directpay"
	"github.com/go-pay/gopay/pkg/xhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type protocolTransport func(*http.Request) (*http.Response, error)

func (f protocolTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, provider string) (*Adapter, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	c := Config{Provider: provider, Environment: "sandbox", Account: "fixture", Revision: "v1", AppID: "app", MerchantID: "merchant", VerificationMode: "public_key", PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})), NotifyURL: "https://example.test/notify", ReturnURL: "https://example.test/return"}
	if provider == dp.Wechat {
		c.Environment = "live"
		c.PublicKeyID = "PUB_KEY_ID_FIXTURE"
		c.Serial = "merchantserial"
		c.APIv3Key = "01234567890123456789012345678901"
	}
	a, err := New(c)
	require.NoError(t, err)
	return a, key
}
func signFixture(t *testing.T, key *rsa.PrivateKey, data string) string {
	t.Helper()
	hash := sha256.Sum256([]byte(data))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(sig)
}
func TestAlipaySDKContract(t *testing.T) {
	a, key := fixture(t, dp.Alipay)
	o := a.identity()
	o.OrderNo = "order1"
	o.MoneyMinor = 100
	o.ExpiresAt = time.Now().Unix() + 600
	o.Method = dp.Page
	checkout, err := a.Create(t.Context(), o)
	require.NoError(t, err)
	assert.Equal(t, "redirect", checkout.Kind)
	u, err := url.Parse(checkout.Value)
	require.NoError(t, err)
	assert.Equal(t, "openapi-sandbox.dl.alipaydev.com", u.Host)
	assert.Equal(t, "alipay.trade.page.pay", u.Query().Get("method"))
	assert.NotEmpty(t, u.Query().Get("sign"))
	payload := `{"code":"10000","out_trade_no":"order1","trade_no":"trade1","trade_status":"TRADE_SUCCESS","total_amount":"1.00","send_pay_date":"2026-10-02 12:00:00"}`
	tampered := false
	a.ali.SetHttpClient(xhttp.NewClient().SetTransport(protocolTransport(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "openapi-sandbox.dl.alipaydev.com", r.URL.Host)
		require.NoError(t, r.ParseForm())
		assert.NotEmpty(t, r.Form.Get("sign"))
		sig := signFixture(t, key, payload)
		body := payload
		if tampered {
			body = strings.Replace(body, "1.00", "9.00", 1)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"alipay_trade_query_response":` + body + `,"sign":"` + sig + `"}`)), Request: r}, nil
	})))
	e, err := a.Query(context.Background(), o)
	require.NoError(t, err)
	assert.EqualValues(t, 100, e.MoneyMinor)
	assert.True(t, e.QueryBoundIdentity)
	tampered = true
	_, err = a.Query(t.Context(), o)
	require.Error(t, err)
	tampered = false
	payload = `{"code":"40004","sub_code":"ACQ.TRADE_NOT_EXIST"}`
	e, err = a.Query(t.Context(), o)
	require.NoError(t, err)
	assert.Equal(t, "not_found", e.State)

	form := url.Values{"app_id": {"app"}, "seller_id": {"merchant"}, "out_trade_no": {"order1"}, "trade_no": {"trade1"}, "trade_status": {"TRADE_SUCCESS"}, "total_amount": {"1.00"}, "gmt_payment": {"2026-10-02 12:00:00"}, "notify_id": {"event1"}}
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+form.Get(k))
	}
	form.Set("sign", signFixture(t, key, strings.Join(pairs, "&")))
	form.Set("sign_type", "RSA2")
	_, err = a.Notify(nil, []byte(form.Encode()))
	require.NoError(t, err)
	_, err = a.Notify(nil, []byte(form.Encode()+"&total_amount=1.00"))
	require.Error(t, err)
	form.Set("total_amount", "2.00")
	_, err = a.Notify(nil, []byte(form.Encode()))
	require.Error(t, err)
}
func TestWechatSDKContract(t *testing.T) {
	a, key := fixture(t, dp.Wechat)
	payment := map[string]any{"appid": "app", "mchid": "merchant", "out_trade_no": "order1", "transaction_id": "trade1", "trade_type": "NATIVE", "trade_state": "SUCCESS", "success_time": "2026-10-02T12:00:00+08:00", "amount": map[string]any{"total": 100, "payer_total": 50, "currency": "CNY"}}
	plain, err := common.Marshal(payment)
	require.NoError(t, err)
	block, err := aes.NewCipher([]byte(a.cfg.APIv3Key))
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	encrypted := gcm.Seal(nil, []byte("nonce1234567"), plain, []byte("transaction"))
	body, err := common.Marshal(map[string]any{"id": "event1", "event_type": "TRANSACTION.SUCCESS", "resource": map[string]any{"algorithm": "AEAD_AES_256_GCM", "nonce": "nonce1234567", "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
	require.NoError(t, err)
	h := make(http.Header)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	h.Set("Wechatpay-Timestamp", stamp)
	h.Set("Wechatpay-Nonce", "headernonce")
	h.Set("Wechatpay-Serial", a.cfg.PublicKeyID)
	h.Set("Wechatpay-Signature", signFixture(t, key, stamp+"\nheadernonce\n"+string(body)+"\n"))
	e, err := a.Notify(h, body)
	require.NoError(t, err)
	assert.EqualValues(t, 100, e.MoneyMinor)
	_, err = a.Notify(h, append(body, ' '))
	require.Error(t, err)
	h.Set("Wechatpay-Signature", "WECHATPAY/SIGNTEST/fixture")
	_, err = a.Notify(h, body)
	require.Error(t, err)
	h.Set("Wechatpay-Serial", "unknown")
	_, err = a.Notify(h, body)
	require.Error(t, err)
	// A controlled trust-window update accepts the next platform key while
	// preserving the original order/account binding; unknown keys still fail.
	nextKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	nextDER, err := x509.MarshalPKIXPublicKey(&nextKey.PublicKey)
	require.NoError(t, err)
	cfg := a.cfg
	cfg.TrustedVerificationKeys = map[string]string{"PUB_KEY_ID_NEXT": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: nextDER}))}
	rotated, err := New(cfg)
	require.NoError(t, err)
	h.Set("Wechatpay-Serial", "PUB_KEY_ID_NEXT")
	h.Set("Wechatpay-Signature", signFixture(t, nextKey, stamp+"\nheadernonce\n"+string(body)+"\n"))
	_, err = rotated.Notify(h, body)
	require.NoError(t, err)
	_, err = a.Notify(h, body)
	require.Error(t, err)
	// Real SDK request and response verification through a deterministic local transport.
	o := a.identity()
	o.OrderNo = "order1"
	o.MoneyMinor = 100
	o.ExpiresAt = time.Now().Unix() + 600
	tampered := false
	a.wx.SetHttpClient(xhttp.NewClient().SetTransport(protocolTransport(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "api.mch.weixin.qq.com", r.URL.Host)
		assert.NotEmpty(t, r.Header.Get("Authorization"))
		payload := string(plain)
		responseHeader := make(http.Header)
		responseHeader.Set("Wechatpay-Timestamp", stamp)
		responseHeader.Set("Wechatpay-Nonce", "response")
		responseHeader.Set("Wechatpay-Serial", a.cfg.PublicKeyID)
		responseHeader.Set("Wechatpay-Signature", signFixture(t, key, stamp+"\nresponse\n"+payload+"\n"))
		if tampered {
			payload += " "
		}
		return &http.Response{StatusCode: 200, Header: responseHeader, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
	})))
	e, err = a.Query(t.Context(), o)
	require.NoError(t, err)
	assert.EqualValues(t, 100, e.MoneyMinor)
	tampered = true
	_, err = a.Query(t.Context(), o)
	require.Error(t, err)
}
func TestMoneyBoundaries(t *testing.T) {
	for _, s := range []string{"0", "-1", "1.001", "1e2", "NaN", " 1.00", "999999999999999999999"} {
		_, err := dp.ParseCNY(s)
		assert.Error(t, err, s)
	}
	for s, want := range map[string]int64{"0.01": 1, "1": 100, "1.2": 120, "1234.56": 123456} {
		got, err := dp.ParseCNY(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}
