package provider

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	dp "github.com/QuantumNous/new-api/types/directpay"
	"github.com/go-pay/gopay/alipay"
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
	a.setWechatTransport(protocolTransport(func(r *http.Request) (*http.Response, error) {
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
	}))
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

// Local certificates exercise the real GoPay certificate path; they are not
// provider-issued certificates and confer no official sandbox acceptance.
func fixtureCertificate(t *testing.T, key *rsa.PrivateKey, serial int64, start, end time.Time) string {
	t.Helper()
	cert := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "local-contract"}, NotBefore: start, NotAfter: end, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, IsCA: true, BasicConstraintsValid: true, SignatureAlgorithm: x509.SHA256WithRSA}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
func TestCertificateSDKContract(t *testing.T) {
	for _, provider := range []string{dp.Alipay, dp.Wechat} {
		t.Run(provider, func(t *testing.T) {
			original, key := fixture(t, provider)
			cfg := original.cfg
			cfg.VerificationMode = "certificate"
			cfg.PublicKeyID = "A1"
			cert := fixtureCertificate(t, key, 161, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
			cfg.PublicKey, cfg.AppCertificate, cfg.RootCertificate = cert, cert, cert
			a, err := New(cfg)
			require.NoError(t, err)
			o := a.identity()
			o.OrderNo = "cert-order"
			o.MoneyMinor = 100
			o.ExpiresAt = time.Now().Unix() + 600
			tamper, wrongSerial := false, false
			transport := protocolTransport(func(r *http.Request) (*http.Response, error) {
				hdr := make(http.Header)
				payload := `{"code":"10000","out_trade_no":"cert-order","trade_no":"cert-trade","trade_status":"TRADE_SUCCESS","total_amount":"1.00","send_pay_date":"2026-10-02 12:00:00"}`
				var body string
				if provider == dp.Alipay {
					sn := a.ali.AliPayPublicCertSN
					if wrongSerial {
						sn = "unknown"
					}
					sig := signFixture(t, key, payload)
					if tamper {
						payload = strings.Replace(payload, "1.00", "9.00", 1)
					}
					body = `{"alipay_trade_query_response":` + payload + `,"sign":"` + sig + `","alipay_cert_sn":"` + sn + `"}`
				} else {
					payload = `{"appid":"app","mchid":"merchant","out_trade_no":"cert-order","transaction_id":"cert-trade","trade_type":"NATIVE","trade_state":"SUCCESS","success_time":"2026-10-02T12:00:00+08:00","amount":{"total":100,"currency":"CNY"}}`
					stamp := strconv.FormatInt(time.Now().Unix(), 10)
					hdr.Set("Wechatpay-Timestamp", stamp)
					hdr.Set("Wechatpay-Nonce", "cert")
					hdr.Set("Wechatpay-Serial", "A1")
					if wrongSerial {
						hdr.Set("Wechatpay-Serial", "BAD")
					}
					hdr.Set("Wechatpay-Signature", signFixture(t, key, stamp+"\ncert\n"+payload+"\n"))
					body = payload
					if tamper {
						body += " "
					}
				}
				return &http.Response{StatusCode: 200, Header: hdr, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			if provider == dp.Alipay {
				a.ali.SetHttpClient(xhttp.NewClient().SetTransport(transport))
			} else {
				a.setWechatTransport(transport)
			}
			proof, err := a.Query(t.Context(), o)
			require.NoError(t, err)
			assert.EqualValues(t, 100, proof.MoneyMinor)
			tamper = true
			_, err = a.Query(t.Context(), o)
			require.Error(t, err)
			tamper = false
			wrongSerial = true
			_, err = a.Query(t.Context(), o)
			require.Error(t, err)
			for _, window := range [][2]time.Time{{time.Now().Add(-2 * time.Hour), time.Now().Add(-time.Hour)}, {time.Now().Add(time.Hour), time.Now().Add(2 * time.Hour)}} {
				invalid := cfg
				invalid.PublicKey = fixtureCertificate(t, key, 161, window[0], window[1])
				_, err = New(invalid)
				require.Error(t, err)
				// Expiry after construction must also fail before contacting a gateway.
				a.cfg.PublicKey = invalid.PublicKey
				_, err = a.Query(t.Context(), o)
				require.Error(t, err)
			}
			if provider == dp.Wechat {
				invalid := cfg
				invalid.PublicKeyID = "A2"
				_, err = New(invalid)
				require.Error(t, err)
				cfg.TrustedVerificationKeys = map[string]string{"A2": fixtureCertificate(t, key, 162, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))}
				_, err = New(cfg)
				require.NoError(t, err)
				cfg.TrustedVerificationKeys["A2"] = cert
				_, err = New(cfg)
				require.Error(t, err)
			}
		})
	}
}

func TestCertificateExpiredPrimaryRotation(t *testing.T) {
	for _, kind := range []string{dp.Alipay, dp.Wechat} {
		t.Run(kind, func(t *testing.T) {
			original, oldKey := fixture(t, kind)
			cfg := original.cfg
			cfg.VerificationMode = "certificate"
			cfg.PublicKeyID = "A1"
			cfg.PublicKey = fixtureCertificate(t, oldKey, 161, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
			cfg.AppCertificate = fixtureCertificate(t, oldKey, 163, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
			cfg.RootCertificate = cfg.AppCertificate
			nextKey, err := rsa.GenerateKey(rand.Reader, 2048)
			require.NoError(t, err)
			nextCert := fixtureCertificate(t, nextKey, 177, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
			nextID := "B1"
			if kind == dp.Alipay {
				nextID, err = alipay.GetCertSN([]byte(nextCert))
				require.NoError(t, err)
			}
			cfg.TrustedVerificationKeys = map[string]string{nextID: nextCert}
			a, err := New(cfg)
			require.NoError(t, err)
			o := a.identity()
			o.OrderNo = "rotation"
			o.MoneyMinor = 100
			o.ExpiresAt = time.Now().Unix() + 600
			calls := 0
			badSerial := false
			transport := protocolTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				header := make(http.Header)
				payload := `{"code":"10000","out_trade_no":"rotation","trade_no":"tx","trade_status":"TRADE_SUCCESS","total_amount":"1.00","send_pay_date":"2026-10-02 12:00:00"}`
				serial := nextID
				if badSerial {
					serial = "UNKNOWN"
				}
				body := ""
				if kind == dp.Alipay {
					body = `{"alipay_trade_query_response":` + payload + `,"alipay_cert_sn":"` + serial + `","sign":"` + signFixture(t, nextKey, payload) + `"}`
				} else {
					payload = `{"appid":"app","mchid":"merchant","out_trade_no":"rotation","transaction_id":"tx","trade_type":"NATIVE","trade_state":"SUCCESS","success_time":"2026-10-02T12:00:00+08:00","amount":{"total":100,"currency":"CNY"}}`
					stamp := strconv.FormatInt(time.Now().Unix(), 10)
					header.Set("Wechatpay-Timestamp", stamp)
					header.Set("Wechatpay-Nonce", "rotation")
					header.Set("Wechatpay-Serial", serial)
					header.Set("Wechatpay-Signature", signFixture(t, nextKey, stamp+"\nrotation\n"+payload+"\n"))
					body = payload
				}
				return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			if kind == dp.Alipay {
				a.ali.SetHttpClient(xhttp.NewClient().SetTransport(transport))
			} else {
				a.setWechatTransport(transport)
			}
			_, err = a.Query(t.Context(), o)
			require.NoError(t, err)
			badSerial = true
			_, err = a.Query(t.Context(), o)
			require.Error(t, err)
			assert.Equal(t, 2, calls, "unknown serial must not cause SDK certificate download")
			// Keep original account/revision evidence while selecting the next verifier.
			if kind == dp.Alipay {
				form := url.Values{"app_id": {"app"}, "seller_id": {"merchant"}, "out_trade_no": {"rotation"}, "trade_no": {"tx"}, "trade_status": {"TRADE_SUCCESS"}, "total_amount": {"1.00"}, "gmt_payment": {"2026-10-02 12:00:00"}}
				keys := make([]string, 0, len(form))
				for k := range form {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				pairs := []string{}
				for _, k := range keys {
					pairs = append(pairs, k+"="+form.Get(k))
				}
				canonical := strings.Join(pairs, "&")
				form.Set("sign_type", "RSA2")
				form.Set("sign", signFixture(t, nextKey, canonical))
				proof, err := a.Notify(nil, []byte(form.Encode()))
				require.NoError(t, err)
				assert.Equal(t, o.Revision, proof.Revision)
				form.Set("sign", signFixture(t, oldKey, canonical))
				_, err = a.Notify(nil, []byte(form.Encode()))
				require.Error(t, err)
			} else {
				plain := []byte(`{"appid":"app","mchid":"merchant","out_trade_no":"rotation","transaction_id":"tx","trade_type":"NATIVE","trade_state":"SUCCESS","success_time":"2026-10-02T12:00:00+08:00","amount":{"total":100,"currency":"CNY"}}`)
				block, err := aes.NewCipher([]byte(cfg.APIv3Key))
				require.NoError(t, err)
				gcm, err := cipher.NewGCM(block)
				require.NoError(t, err)
				encrypted := gcm.Seal(nil, []byte("nonce1234567"), plain, []byte("transaction"))
				body, err := common.Marshal(map[string]any{"id": "rotation", "event_type": "TRANSACTION.SUCCESS", "resource": map[string]any{"algorithm": "AEAD_AES_256_GCM", "nonce": "nonce1234567", "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
				require.NoError(t, err)
				stamp := strconv.FormatInt(time.Now().Unix(), 10)
				header := make(http.Header)
				header.Set("Wechatpay-Timestamp", stamp)
				header.Set("Wechatpay-Nonce", "rotation")
				header.Set("Wechatpay-Serial", nextID)
				header.Set("Wechatpay-Signature", signFixture(t, nextKey, stamp+"\nrotation\n"+string(body)+"\n"))
				proof, err := a.Notify(header, body)
				require.NoError(t, err)
				assert.Equal(t, o.Revision, proof.Revision)
				header.Set("Wechatpay-Serial", "A1")
				header.Set("Wechatpay-Signature", signFixture(t, oldKey, stamp+"\nrotation\n"+string(body)+"\n"))
				_, err = a.Notify(header, body)
				require.Error(t, err)
			}
			nextRevision := o
			nextRevision.Revision = "v2"
			_, err = a.Query(t.Context(), nextRevision)
			require.Error(t, err)
		})
	}
}

func TestSignedCloseCertificateContract(t *testing.T) {
	for _, kind := range []string{dp.Alipay, dp.Wechat} {
		t.Run(kind, func(t *testing.T) {
			a, key := fixture(t, kind)
			cfg := a.cfg
			cfg.VerificationMode = "certificate"
			cfg.PublicKeyID = "A1"
			cert := fixtureCertificate(t, key, 161, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
			cfg.PublicKey = cert
			cfg.AppCertificate = cert
			cfg.RootCertificate = cert
			a, err := New(cfg)
			require.NoError(t, err)
			o := a.identity()
			o.OrderNo = "close-order"
			tamper := false
			transport := protocolTransport(func(r *http.Request) (*http.Response, error) {
				header := make(http.Header)
				status := 200
				body := ""
				if kind == dp.Alipay {
					payload := `{"code":"10000","out_trade_no":"close-order"}`
					sig := signFixture(t, key, payload)
					if tamper {
						payload = strings.Replace(payload, "close-order", "other-order", 1)
					}
					body = `{"alipay_trade_close_response":` + payload + `,"alipay_cert_sn":"` + a.ali.AliPayPublicCertSN + `","sign":"` + sig + `"}`
				} else {
					status = 204
					stamp := strconv.FormatInt(time.Now().Unix(), 10)
					header.Set("Wechatpay-Timestamp", stamp)
					header.Set("Wechatpay-Nonce", "close")
					header.Set("Wechatpay-Serial", "A1")
					header.Set("Wechatpay-Signature", signFixture(t, key, stamp+"\nclose\n\n"))
					if tamper {
						header.Set("Wechatpay-Nonce", "wrong")
					}
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			if kind == dp.Alipay {
				a.ali.SetHttpClient(xhttp.NewClient().SetTransport(transport))
			} else {
				a.setWechatTransport(transport)
			}
			require.NoError(t, a.Close(t.Context(), o))
			tamper = true
			require.Error(t, a.Close(t.Context(), o))
		})
	}
}

func TestWechatNativeCreateSDKContract(t *testing.T) {
	a, key := fixture(t, dp.Wechat)
	o := a.identity()
	o.OrderNo = "native-order"
	o.MoneyMinor = 123
	o.Method = dp.Native
	o.ExpiresAt = time.Now().Unix() + 600
	codeURL := "weixin://wxpay/local-fixture"
	tamper := false
	a.setWechatTransport(protocolTransport(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "/v3/pay/transactions/native", r.URL.Path)
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var request struct {
			AppID      string `json:"appid"`
			MerchantID string `json:"mchid"`
			Amount     struct {
				Total    int    `json:"total"`
				Currency string `json:"currency"`
			} `json:"amount"`
		}
		require.NoError(t, common.Unmarshal(raw, &request))
		assert.Equal(t, "app", request.AppID)
		assert.Equal(t, "merchant", request.MerchantID)
		assert.Equal(t, 123, request.Amount.Total)
		assert.Equal(t, "CNY", request.Amount.Currency)
		body, err := common.Marshal(map[string]string{"code_url": codeURL})
		require.NoError(t, err)
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		h := make(http.Header)
		h.Set("Wechatpay-Timestamp", stamp)
		h.Set("Wechatpay-Nonce", "native")
		h.Set("Wechatpay-Serial", a.cfg.PublicKeyID)
		h.Set("Wechatpay-Signature", signFixture(t, key, stamp+"\nnative\n"+string(body)+"\n"))
		if tamper {
			body = append(body, ' ')
		}
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	}))
	checkout, err := a.Create(t.Context(), o)
	require.NoError(t, err)
	assert.Equal(t, "qr", checkout.Kind)
	assert.Equal(t, codeURL, checkout.Value)
	tamper = true
	_, err = a.Create(t.Context(), o)
	require.Error(t, err)
	tamper = false
	codeURL = "https://untrusted.test"
	_, err = a.Create(t.Context(), o)
	require.Error(t, err)
}
