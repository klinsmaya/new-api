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
	if a.wx != nil {
		a.setWechatTransport(http.DefaultTransport.(*http.Transport).Clone())
	}
	return a, nil
}

// Configured certificate bytes are pinned trust anchors, never callback input.
func parseCertificate(content string) (*x509.Certificate, error) {
	block, rest := pem.Decode([]byte(content))
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("invalid configured certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid configured certificate")
	}
	if _, ok := cert.PublicKey.(*rsa.PublicKey); !ok {
		return nil, errors.New("RSA certificate required")
	}
	return cert, nil
}
func certificateActive(cert *x509.Certificate) bool {
	now := time.Now()
	return !now.Before(cert.NotBefore) && now.Before(cert.NotAfter)
}
func (a *Adapter) certificateMaterials() (map[string]string, error) {
	primaryID := a.cfg.PublicKeyID
	if a.cfg.Provider == dp.Alipay {
		var err error
		primaryID, err = alipay.GetCertSN([]byte(a.cfg.PublicKey))
		if err != nil {
			return nil, dp.ErrEvidence
		}
	}
	materials := map[string]string{primaryID: a.cfg.PublicKey}
	for id, content := range a.cfg.TrustedVerificationKeys {
		if id == primaryID {
			return nil, dp.ErrEvidence
		}
		materials[id] = content
	}
	for id, content := range materials {
		cert, err := parseCertificate(content)
		if err != nil {
			return nil, err
		}
		expected := strings.ToUpper(cert.SerialNumber.Text(16))
		if a.cfg.Provider == dp.Alipay {
			expected, err = alipay.GetCertSN([]byte(content))
			if err != nil {
				return nil, dp.ErrEvidence
			}
		}
		if id == "" || id != expected {
			return nil, errors.New("certificate serial mismatch")
		}
	}
	return materials, nil
}
func (a *Adapter) selectedCertificate(id string) (string, error) {
	materials, err := a.certificateMaterials()
	if err != nil {
		return "", err
	}
	content, ok := materials[id]
	if !ok {
		return "", dp.ErrEvidence
	}
	cert, err := parseCertificate(content)
	if err != nil || !certificateActive(cert) {
		return "", dp.ErrEvidence
	}
	return content, nil
}

// Retained expired anchors cannot verify anything, but must not prevent a
// historical revision from using a separately provisioned active next anchor.
func (a *Adapter) validateCertificates(includeSigner bool) error {
	if a.cfg.VerificationMode != "certificate" {
		return nil
	}
	materials, err := a.certificateMaterials()
	if err != nil {
		return err
	}
	active := false
	for _, content := range materials {
		cert, _ := parseCertificate(content)
		if certificateActive(cert) {
			active = true
		}
	}
	if !active {
		return errors.New("no active configured verification certificate")
	}
	if includeSigner && a.cfg.Provider == dp.Alipay {
		cert, err := parseCertificate(a.cfg.AppCertificate)
		if err != nil || !certificateActive(cert) {
			return errors.New("application certificate outside validity window")
		}
		// Root bundles may retain old roots; require at least one currently valid
		// certificate and reject malformed bundles without echoing their content.
		remaining := []byte(a.cfg.RootCertificate)
		activeRoot := false
		for len(strings.TrimSpace(string(remaining))) > 0 {
			block, rest := pem.Decode(remaining)
			if block == nil || block.Type != "CERTIFICATE" {
				return dp.ErrEvidence
			}
			root, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return dp.ErrEvidence
			}
			if certificateActive(root) {
				activeRoot = true
			}
			remaining = rest
		}
		if !activeRoot {
			return errors.New("no active configured root certificate")
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
func (a *Adapter) verifyAli(data, sign, serial string) error {
	var ok bool
	var err error
	if data == "" || sign == "" {
		return dp.ErrEvidence
	}
	if a.cfg.VerificationMode == "certificate" {
		content, certErr := a.selectedCertificate(serial)
		if certErr != nil {
			return dp.ErrEvidence
		}
		ok, err = alipay.VerifySyncSignWithCert([]byte(content), data, sign)
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
		if err = a.verifyAli(string(envelope.Response), envelope.Sign, envelope.CertSN); err != nil {
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
		var envelope struct {
			Response json.RawMessage `json:"alipay_trade_close_response"`
			Sign     string          `json:"sign"`
			CertSN   string          `json:"alipay_cert_sn"`
		}
		if err := a.ali.PostAliPayAPISelfV2(ctx, gopay.BodyMap{"biz_content": gopay.BodyMap{"out_trade_no": o.OrderNo}}, "alipay.trade.close", &envelope); err != nil {
			return dp.ErrUnknown
		}
		if err := a.verifyAli(string(envelope.Response), envelope.Sign, envelope.CertSN); err != nil {
			return err
		}
		var response struct {
			Code    string `json:"code"`
			OrderNo string `json:"out_trade_no"`
		}
		if common.Unmarshal(envelope.Response, &response) != nil || response.Code != "10000" || response.OrderNo != o.OrderNo {
			return dp.ErrEvidence
		}
		return nil
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
			materials, materialErr := a.certificateMaterials()
			if materialErr != nil {
				return e, dp.ErrEvidence
			}
			for id := range materials {
				content, certErr := a.selectedCertificate(id)
				if certErr != nil {
					continue
				}
				verified, verifyErr := alipay.VerifySignWithCert([]byte(content), bm)
				if verifyErr == nil && verified {
					ok = true
					break
				}
			}
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
		if a.verifyWechat(h, body) != nil {
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
