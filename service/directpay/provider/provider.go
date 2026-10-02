// Package provider is the only production package importing GoPay.
package provider

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	dp "github.com/QuantumNous/new-api/types/directpay"
	"github.com/go-pay/crypto/xpem"
	"github.com/go-pay/crypto/xrsa"
	"github.com/go-pay/gopay"
	"github.com/go-pay/gopay/alipay"
	wx "github.com/go-pay/gopay/wechat/v3"
)

type Config struct {
	TrustedVerificationKeys map[string]string `json:"trusted_verification_keys"`
	Provider                string            `json:"provider"`
	Environment             string            `json:"environment"`
	Account                 string            `json:"account"`
	Revision                string            `json:"revision"`
	AppID                   string            `json:"app_id"`
	MerchantID              string            `json:"merchant_id"`
	VerificationMode        string            `json:"verification_mode"`
	Serial                  string            `json:"serial"`
	PublicKeyID             string            `json:"public_key_id"`
	// These values are loaded exclusively from an operator-mounted Secret, never returned to HTTP callers.
	PrivateKey      string `json:"private_key"`
	APIv3Key        string `json:"api_v3_key"`
	PublicKey       string `json:"public_key"`
	AppCertificate  string `json:"app_certificate"`
	RootCertificate string `json:"root_certificate"`
	NotifyURL       string `json:"-"`
	ReturnURL       string `json:"-"`
}
type Adapter struct {
	cfg       Config
	ali       *alipay.Client
	wx        *wx.ClientV3
	verifier  *rsa.PublicKey
	verifiers map[string]*rsa.PublicKey
}

func New(c Config) (*Adapter, error) {
	if c.Account == "" || c.Revision == "" || c.AppID == "" || c.MerchantID == "" {
		return nil, errors.New("incomplete account identity")
	}
	for _, s := range []string{c.NotifyURL, c.ReturnURL} {
		u, err := url.Parse(s)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return nil, errors.New("trusted HTTPS URL required")
		}
	}
	a := &Adapter{cfg: c}
	if err := a.validateCertificates(true); err != nil {
		return nil, err
	}
	var err error
	if c.Provider == dp.Alipay {
		if c.Environment != "sandbox" && c.Environment != "live" {
			return nil, errors.New("invalid Alipay environment")
		}
		if block, _ := pem.Decode([]byte(c.PrivateKey)); block != nil {
			c.PrivateKey = base64.StdEncoding.EncodeToString(block.Bytes)
		}
		if c.VerificationMode == "public_key" {
			if block, _ := pem.Decode([]byte(c.PublicKey)); block != nil {
				c.PublicKey = base64.StdEncoding.EncodeToString(block.Bytes)
			}
		}
		a.cfg = c
		a.ali, err = alipay.NewClient(c.AppID, c.PrivateKey, c.Environment == "live")
		if err != nil {
			return nil, errors.New("invalid Alipay signing key")
		}
		a.ali.SetSignType(alipay.RSA2).SetCharset(alipay.UTF8).SetLocation(alipay.LocationShanghai).SetNotifyUrl(c.NotifyURL).SetReturnUrl(c.ReturnURL)
		switch c.VerificationMode {
		case "public_key":
			a.verifier, err = xpem.DecodePublicKey([]byte(xrsa.FormatAlipayPublicKey(c.PublicKey)))
		case "certificate":
			err = a.ali.SetCertSnByContent([]byte(c.AppCertificate), []byte(c.RootCertificate), []byte(c.PublicKey))
			if err == nil {
				a.ali.AutoVerifySign([]byte(c.PublicKey))
			}
		default:
			return nil, errors.New("invalid Alipay verification mode")
		}
	} else if c.Provider == dp.Wechat {
		if c.Environment != "live" || len(c.APIv3Key) != 32 {
			return nil, errors.New("invalid WeChat environment or APIv3 key")
		}
		if c.VerificationMode == "public_key" {
			if !strings.HasPrefix(c.PublicKeyID, "PUB_KEY_ID_") {
				return nil, errors.New("WeChat public key ID prefix required")
			}
		} else if c.VerificationMode != "certificate" || strings.HasPrefix(c.PublicKeyID, "PUB_KEY_ID_") {
			return nil, errors.New("invalid WeChat verification mode")
		}
		a.wx, err = wx.NewClientV3(c.MerchantID, c.Serial, c.APIv3Key, c.PrivateKey)
		if err != nil {
			return nil, errors.New("invalid WeChat signing material")
		}
		err = a.wx.AutoVerifySignByPublicKey([]byte(c.PublicKey), c.PublicKeyID)
		if err == nil {
			a.verifier, err = xpem.DecodePublicKey([]byte(c.PublicKey))
		}
	} else {
		return nil, errors.New("unsupported provider")
	}
	if err != nil {
		return nil, errors.New("invalid verification material")
	}
	if a.wx != nil {
		a.verifiers = map[string]*rsa.PublicKey{c.PublicKeyID: a.verifier}
		for id, content := range c.TrustedVerificationKeys {
			if id == "" || (c.VerificationMode == "public_key" && !strings.HasPrefix(id, "PUB_KEY_ID_")) {
				return nil, errors.New("invalid trusted verification key ID")
			}
			key, err := xpem.DecodePublicKey([]byte(content))
			if err != nil || key == nil {
				return nil, errors.New("invalid trusted verification key")
			}
			if id == c.PublicKeyID {
				return nil, errors.New("duplicate primary verification key")
			}
			a.verifiers[id] = key
			a.wx.SnCertMap.Store(id, key)
		}
	}
	return a, nil
}

// Certificates are explicitly provisioned trust anchors. Never trust a certificate
// supplied by a callback or automatically fetch an unknown serial. Recheck dates
// on each operation because a long-running process can outlive its certificates.
func (a *Adapter) validateCertificates(includeSigner bool) error {
	if a.cfg.VerificationMode != "certificate" {
		return nil
	}
	materials := map[string]string{a.cfg.PublicKeyID: a.cfg.PublicKey}
	if a.cfg.Provider == dp.Alipay {
		materials = map[string]string{"platform": a.cfg.PublicKey}
		if includeSigner {
			materials["app"] = a.cfg.AppCertificate
			materials["root"] = a.cfg.RootCertificate
		}
	} else {
		for id, content := range a.cfg.TrustedVerificationKeys {
			materials[id] = content
		}
	}
	for id, content := range materials {
		remaining := []byte(content)
		count := 0
		for len(strings.TrimSpace(string(remaining))) > 0 {
			block, rest := pem.Decode(remaining)
			if block == nil || block.Type != "CERTIFICATE" {
				return errors.New("invalid configured certificate")
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil || time.Now().Before(cert.NotBefore) || !time.Now().Before(cert.NotAfter) {
				return errors.New("configured certificate outside validity window")
			}
			if a.cfg.Provider == dp.Wechat && (id == "" || !strings.EqualFold(cert.SerialNumber.Text(16), id)) {
				return errors.New("certificate serial mismatch")
			}
			if _, ok := cert.PublicKey.(*rsa.PublicKey); !ok && id != "root" {
				return errors.New("RSA certificate required")
			}
			count++
			remaining = rest
		}
		if count == 0 {
			return errors.New("configured certificate missing")
		}
	}
	return nil
}
func (a *Adapter) bound(o dp.Snapshot) bool {
	return o.Provider == a.cfg.Provider && o.Environment == a.cfg.Environment && o.Account == a.cfg.Account && o.Revision == a.cfg.Revision && o.AppID == a.cfg.AppID && o.MerchantID == a.cfg.MerchantID
}
func (a *Adapter) identity() dp.Snapshot {
	return dp.Snapshot{Provider: a.cfg.Provider, Environment: a.cfg.Environment, Account: a.cfg.Account, Revision: a.cfg.Revision, AppID: a.cfg.AppID, MerchantID: a.cfg.MerchantID, Currency: "CNY"}
}
func (a *Adapter) Create(ctx context.Context, o dp.Snapshot) (dp.Checkout, error) {
	if a.validateCertificates(true) != nil || !a.bound(o) || o.MoneyMinor <= 0 || o.Currency != "CNY" {
		return dp.Checkout{}, dp.ErrEvidence
	}
	if a.ali != nil {
		bm := gopay.BodyMap{"out_trade_no": o.OrderNo, "total_amount": dp.FormatCNY(o.MoneyMinor), "subject": "Wallet recharge", "product_code": "FAST_INSTANT_TRADE_PAY", "time_expire": time.Unix(o.ExpiresAt, 0).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")}
		value, err := a.ali.TradePagePay(ctx, bm)
		if err != nil {
			return dp.Checkout{}, dp.ErrUnknown
		}
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.User != nil {
			return dp.Checkout{}, dp.ErrUnknown
		}
		expected := "openapi.alipay.com"
		if a.cfg.Environment == "sandbox" {
			expected = "openapi-sandbox.dl.alipaydev.com"
		}
		if u.Host != expected {
			return dp.Checkout{}, dp.ErrUnknown
		}
		return dp.Checkout{Kind: "redirect", Value: value}, nil
	}
	r, err := a.wx.V3TransactionNative(ctx, gopay.BodyMap{"appid": o.AppID, "mchid": o.MerchantID, "description": "Wallet recharge", "out_trade_no": o.OrderNo, "notify_url": a.cfg.NotifyURL, "time_expire": time.Unix(o.ExpiresAt, 0).UTC().Format(time.RFC3339), "amount": gopay.BodyMap{"total": o.MoneyMinor, "currency": "CNY"}})
	if err != nil || r == nil || r.Code != 0 || r.Response == nil || !strings.HasPrefix(r.Response.CodeUrl, "weixin://wxpay/") {
		return dp.Checkout{}, dp.ErrUnknown
	}
	return dp.Checkout{Kind: "qr", Value: r.Response.CodeUrl}, nil
}
func (a *Adapter) verifyAli(data, sign string) error {
	var ok bool
	var err error
	if data == "" || sign == "" {
		return dp.ErrEvidence
	}
	if a.cfg.VerificationMode == "certificate" {
		ok, err = alipay.VerifySyncSignWithCert([]byte(a.cfg.PublicKey), data, sign)
	} else {
		ok, err = alipay.VerifySyncSign(a.cfg.PublicKey, data, sign)
	}
	if err != nil || !ok {
		return dp.ErrEvidence
	}
	return nil
}
func (a *Adapter) Query(ctx context.Context, o dp.Snapshot) (dp.Evidence, error) {
	if a.validateCertificates(true) != nil || !a.bound(o) {
		return dp.Evidence{}, dp.ErrEvidence
	}
	e := dp.Evidence{Snapshot: a.identity(), Source: "query"}
	e.OrderNo = o.OrderNo
	if a.ali != nil {
		// SelfV2 retains exact signed JSON even on business errors; TradeQuery
		// returns before extracting SignData for TRADE_NOT_EXIST in this SDK.
		var envelope struct {
			Response json.RawMessage `json:"alipay_trade_query_response"`
			Sign     string          `json:"sign"`
			CertSN   string          `json:"alipay_cert_sn"`
		}
		err := a.ali.PostAliPayAPISelfV2(ctx, gopay.BodyMap{"biz_content": gopay.BodyMap{"out_trade_no": o.OrderNo}}, "alipay.trade.query", &envelope)
		if err != nil || len(envelope.Response) == 0 {
			return e, dp.ErrUnknown
		}
		if a.cfg.VerificationMode == "certificate" && envelope.CertSN != a.ali.AliPayPublicCertSN {
			return e, dp.ErrEvidence
		}
		if err = a.verifyAli(string(envelope.Response), envelope.Sign); err != nil {
			return e, err
		}
		var p alipay.TradeQuery
		if common.Unmarshal(envelope.Response, &p) != nil {
			return e, dp.ErrEvidence
		}
		if p.Code != "10000" {
			if p.SubCode == "ACQ.TRADE_NOT_EXIST" {
				e.State = "not_found"
				e.QueryBoundIdentity = true
				return e, nil
			}
			return e, dp.ErrUnknown
		}
		if p.OutTradeNo != o.OrderNo || (p.TransCurrency != "" && p.TransCurrency != "CNY") {
			return e, dp.ErrEvidence
		}
		e.QueryBoundIdentity = true
		e.TransactionID = p.TradeNo
		switch p.TradeStatus {
		case "TRADE_SUCCESS", "TRADE_FINISHED":
			e.State = "paid"
		case "WAIT_BUYER_PAY":
			e.State = "pending"
		case "TRADE_CLOSED":
			e.State = "closed"
		default:
			return e, dp.ErrEvidence
		}
		if e.State == "paid" {
			e.MoneyMinor, err = dp.ParseCNY(p.TotalAmount)
			if err != nil {
				return e, err
			}
			paid, err := time.ParseInLocation("2006-01-02 15:04:05", p.SendPayDate, time.FixedZone("CST", 8*3600))
			if err != nil {
				return e, dp.ErrEvidence
			}
			e.PaidAt = paid.Unix()
		}
		return e, nil
	}
	r, err := a.wx.V3TransactionQueryOrder(ctx, wx.OutTradeNo, o.OrderNo)
	if err != nil || r == nil || r.Code != 0 || r.Response == nil {
		return e, dp.ErrUnknown
	}
	p := r.Response
	if p.Appid != o.AppID || p.Mchid != o.MerchantID || p.OutTradeNo != o.OrderNo {
		return e, dp.ErrEvidence
	}
	e.TransactionID = p.TransactionId
	switch p.TradeState {
	case "SUCCESS":
		e.State = "paid"
	case "NOTPAY", "USERPAYING":
		e.State = "pending"
	case "CLOSED":
		e.State = "closed"
	default:
		return e, dp.ErrEvidence
	}
	if e.State == "paid" {
		if p.Amount == nil || p.TradeType != "NATIVE" {
			return e, dp.ErrEvidence
		}
		e.MoneyMinor = int64(p.Amount.Total)
		e.Currency = p.Amount.Currency
		paid, err := time.Parse(time.RFC3339, p.SuccessTime)
		if err != nil {
			return e, dp.ErrEvidence
		}
		e.PaidAt = paid.Unix()
	}
	return e, nil
}
func (a *Adapter) Close(ctx context.Context, o dp.Snapshot) error {
	if a.validateCertificates(true) != nil || !a.bound(o) {
		return dp.ErrEvidence
	}
	if a.ali != nil {
		r, err := a.ali.TradeClose(ctx, gopay.BodyMap{"out_trade_no": o.OrderNo})
		if err != nil || r == nil || r.Response == nil {
			return dp.ErrUnknown
		}
		if r.Response.OutTradeNo != o.OrderNo {
			return dp.ErrEvidence
		}
		return a.verifyAli(r.SignData, r.Sign)
	}
	r, err := a.wx.V3TransactionCloseOrder(ctx, o.OrderNo)
	if err != nil || r == nil || r.Code != 0 {
		return dp.ErrUnknown
	}
	return nil
}
func (a *Adapter) Notify(h http.Header, body []byte) (dp.Evidence, error) {
	e := dp.Evidence{Snapshot: a.identity(), Source: "notify"}
	if a.validateCertificates(false) != nil || len(body) == 0 || len(body) > 65536 {
		return e, dp.ErrEvidence
	}
	if a.ali != nil {
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return e, dp.ErrEvidence
		}
		bm := gopay.BodyMap{}
		for k, v := range form {
			if len(v) != 1 {
				return e, dp.ErrEvidence
			}
			bm[k] = v[0]
		}
		if form.Get("sign_type") != "RSA2" {
			return e, dp.ErrEvidence
		}
		var ok bool
		if a.cfg.VerificationMode == "certificate" {
			ok, err = alipay.VerifySignWithCert([]byte(a.cfg.PublicKey), bm)
		} else {
			ok, err = alipay.VerifySign(a.cfg.PublicKey, bm)
		}
		if err != nil || !ok || form.Get("app_id") != a.cfg.AppID || form.Get("seller_id") != a.cfg.MerchantID {
			return e, dp.ErrEvidence
		}
		status := form.Get("trade_status")
		if status != "TRADE_SUCCESS" && status != "TRADE_FINISHED" {
			return e, dp.ErrEvidence
		}
		e.MoneyMinor, err = dp.ParseCNY(form.Get("total_amount"))
		if err != nil {
			return e, dp.ErrEvidence
		}
		e.OrderNo = form.Get("out_trade_no")
		e.TransactionID = form.Get("trade_no")
		e.EventID = form.Get("notify_id")
		e.State = "paid"
		paid, err := time.ParseInLocation("2006-01-02 15:04:05", form.Get("gmt_payment"), time.FixedZone("CST", 8*3600))
		if err != nil {
			return e, dp.ErrEvidence
		}
		e.PaidAt = paid.Unix()
	} else {
		stamp, err := strconv.ParseInt(h.Get("Wechatpay-Timestamp"), 10, 64)
		if err != nil || stamp < time.Now().Unix()-300 || stamp > time.Now().Unix()+300 || a.verifiers[h.Get("Wechatpay-Serial")] == nil || h.Get("Wechatpay-Nonce") == "" || strings.HasPrefix(h.Get("Wechatpay-Signature"), "WECHATPAY/SIGNTEST/") {
			return e, dp.ErrEvidence
		}
		if err := wx.V3VerifySignByPK(h.Get("Wechatpay-Timestamp"), h.Get("Wechatpay-Nonce"), string(body), h.Get("Wechatpay-Signature"), a.verifiers[h.Get("Wechatpay-Serial")]); err != nil {
			return e, dp.ErrEvidence
		}
		var envelope struct {
			ID        string      `json:"id"`
			EventType string      `json:"event_type"`
			Resource  wx.Resource `json:"resource"`
		}
		if common.Unmarshal(body, &envelope) != nil || envelope.EventType != "TRANSACTION.SUCCESS" || envelope.Resource.Algorithm != "AEAD_AES_256_GCM" {
			return e, dp.ErrEvidence
		}
		p, err := wx.V3DecryptPayNotifyCipherText(envelope.Resource.Ciphertext, envelope.Resource.Nonce, envelope.Resource.AssociatedData, a.cfg.APIv3Key)
		if err != nil || p == nil || p.Amount == nil || p.Appid != a.cfg.AppID || p.Mchid != a.cfg.MerchantID || p.TradeState != "SUCCESS" || p.TradeType != "NATIVE" {
			return e, dp.ErrEvidence
		}
		e.OrderNo = p.OutTradeNo
		e.TransactionID = p.TransactionId
		e.MoneyMinor = int64(p.Amount.Total)
		e.Currency = p.Amount.Currency
		e.EventID = envelope.ID
		e.State = "paid"
		paid, err := time.Parse(time.RFC3339, p.SuccessTime)
		if err != nil {
			return e, dp.ErrEvidence
		}
		e.PaidAt = paid.Unix()
	}
	if len(e.OrderNo) == 0 || len(e.OrderNo) > 32 || e.TransactionID == "" || len(e.TransactionID) > 128 || e.Currency != "CNY" || e.MoneyMinor <= 0 {
		return e, dp.ErrEvidence
	}
	return e, nil
}
