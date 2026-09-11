// Package paycell is a client for Paycell, Turkcell's payment platform.
//
// Three families of service:
//
//   - Provision (tpay): card payments, stored cards, 3D sessions, mobile
//     payment (DCB), OTP, marketplace, reconciliation.
//   - Payment management: getCardTokenSecure, the 3D redirect form.
//   - Web SDK: hosted checkout, status query, reverse, refund.
//
// Amounts are minor units throughout. 15,25 TL is 1525.
//
// Calls hand back the response and an error together. Only responseCode "0"
// counts as success; anything else arrives as *Error with the response struct
// still populated, so you never lose the description.
//
//	c, _ := paycell.New(paycell.Config{
//	    ApplicationName:     "PAYCELLTEST",
//	    ApplicationPassword: "...",
//	    SecureCode:          "...",
//	    MerchantCode:        "9998",
//	    Environment:         paycell.Test,
//	})
//	res, err := c.Pay(ctx, paycell.PayParams{
//	    Card:   paycell.Card{Number: "5200190006338608", ExpireMonth: "01", ExpireYear: "30", CVC: "410"},
//	    Amount: 100,
//	    MSISDN: "905380521479",
//	})
package paycell

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SuccessCode is the responseCode Paycell returns for a successful call.
const SuccessCode = "0"

// Codes that come up often enough to name.
const (
	// CodeOTPRequired: the subscriber has to confirm an OTP first. The
	// stored card list sits behind this.
	CodeOTPRequired = "3110"

	// CodeAlreadySettled: too late to reverse, refund instead.
	CodeAlreadySettled = "2029"
)

// Keys and values Paycell reads out of extraParameters.
const (
	KeyValidationType = "VALIDATION_TYPE"

	// ValidationTypePMList marks an OTP as unlocking the card list rather
	// than authorising a payment.
	ValidationTypePMList = "PM_LIST"
)

// IsOTPRequired reports whether Paycell wants an OTP before it will answer.
//
// The card list and OpenMobilePayment both do this. Make the call, and if it
// comes back true: SendOTP, ValidateOTP, call again with the OTPToken.
func IsOTPRequired(err error) bool { return HasCode(err, CodeOTPRequired) }

// IsAlreadySettled reports a reversal refused because the payment settled.
// Refund the full amount instead.
func IsAlreadySettled(err error) bool { return HasCode(err, CodeAlreadySettled) }

// HasCode reports whether err is a Paycell response carrying code.
func HasCode(err error, code string) bool {
	var pErr *Error
	return errors.As(err, &pErr) && pErr.Code == code
}

// CardListOTPParams marks an OTP as the one that unlocks the card list.
// Paycell's own plugins send it on both sendOTP and validateOTP.
func CardListOTPParams() []KeyValue {
	return []KeyValue{{Key: KeyValidationType, Value: ValidationTypePMList}}
}

// Payment types accepted by the provision services.
const (
	PaymentTypeSale     = "SALE"
	PaymentTypePreAuth  = "PREAUTH"
	PaymentTypePostAuth = "POSTAUTH"
)

// Payment method types accepted by the *All services and returned in queries.
const (
	MethodCreditCard    = "CREDIT_CARD"
	MethodDCB           = "DCB"   // mobile payment / direct carrier billing
	MethodPaycellCard   = "PCARD" // Paycell Kart
	MethodEPara         = "EPARA"
	MethodPaycellWallet = "PAYCELL_WALLET"
)

// Currency codes. The provision services take an ISO code, the Web SDK
// takes Paycell's numeric code (99 for TRY).
const (
	CurrencyTRY    = "TRY"
	SDKCurrencyTRY = "99"
)

// Payment security options used by the Web SDK and returned by status queries.
const (
	ThreeDSecure    = "THREED_SECURE"
	NonThreeDSecure = "NON_THREED_SECURE"
)

// 3D session targets and transaction types.
const (
	TargetMerchant = "MERCHANT"
	TargetPaycell  = "PAYCELL"

	TransactionTypeAuth    = "AUTH"
	TransactionTypePreAuth = "PREAUTH"
)

// Environment selects the Paycell environment a Client talks to.
type Environment int

const (
	Test Environment = iota
	Production
)

// Default endpoints per environment. Merchants are occasionally given
// different hosts (epayment-gtm, a dedicated provision host); override them
// through Config.
const (
	testProvisionURL = "https://tpay-test.turkcell.com.tr/tpay/provision/services/restful/getCardToken/"
	prodProvisionURL = "https://tpay.turkcell.com.tr/tpay/provision/services/restful/getCardToken/"

	testPaymentURL = "https://omccstb.turkcell.com.tr/paymentmanagement/rest/"
	prodPaymentURL = "https://epayment.turkcell.com.tr/paymentmanagement/rest/"

	testCancelURL = "https://zone-test.turkcell.com.tr/tpay/zone/services/cancelrestful/cancelService/"
	prodCancelURL = "https://zone-ist.paycell.com.tr/tpay/zone/services/cancelrestful/cancelService/"

	testSDKInitURL = "https://websdktest.turkcell.com.tr/api/session/init"
	prodSDKInitURL = "https://paycellsdk.paycell.com.tr/api/session/init"

	testSDKHomeURL = "https://websdktest.turkcell.com.tr/home/"
	prodSDKHomeURL = "https://paycellsdk.paycell.com.tr/home/"
)

// maxResponseBytes caps how much of a response body is read, so a misrouted
// request that lands on an HTML page cannot exhaust memory.
const maxResponseBytes = 4 << 20

// Config holds credentials and endpoints. Paycell hands the credentials over
// during onboarding, TerminalCode only if you're using the Web SDK.
type Config struct {
	ApplicationName     string
	ApplicationPassword string
	SecureCode          string
	MerchantCode        string

	// TerminalCode is required by the Web SDK only.
	TerminalCode string

	// EulaID is the terms id for storing a card or opening mobile payment.
	// GetTermsOfServiceContent has the current one.
	EulaID string

	// Environment picks the default endpoints. Ignored for any URL set below.
	Environment Environment

	// ClientIPAddress goes to Paycell as the origin. Defaults to 127.0.0.1.
	ClientIPAddress string

	// DefaultMSISDN fills in for calls that don't carry one. Card-only
	// merchants get assigned a fixed number for this. Country code first:
	// 905380521479.
	DefaultMSISDN string

	// ReferencePrefix is the prefix Paycell assigned you for the first
	// digits of every reference number. NewReferenceNumber uses it.
	ReferencePrefix string

	// SkipResponseHashCheck turns off getCardTokenSecure signature
	// verification. Don't.
	SkipResponseHashCheck bool

	// Endpoint overrides. Empty values fall back to Environment defaults.
	ProvisionURL string // .../provision/services/restful/getCardToken/
	PaymentURL   string // .../paymentmanagement/rest/
	CancelURL    string // .../cancelrestful/cancelService/
	SDKInitURL   string // .../api/session/init
	SDKHomeURL   string // .../home/ , the tracking id is appended

	// HTTPClient does every call. Defaults to one built from Timeout.
	HTTPClient *http.Client

	// Timeout applies to the default HTTPClient only. Defaults to 30s.
	Timeout time.Duration
}

// Client talks to Paycell. Safe for concurrent use.
type Client struct {
	cfg  Config
	http *http.Client

	// now and newID are swapped in tests to make requests deterministic.
	now   func() time.Time
	newID func() string
}

// New checks cfg over and returns a Client.
func New(cfg Config) (*Client, error) {
	switch {
	case cfg.ApplicationName == "":
		return nil, errors.New("paycell: ApplicationName is required")
	case cfg.ApplicationPassword == "":
		return nil, errors.New("paycell: ApplicationPassword is required")
	case cfg.SecureCode == "":
		return nil, errors.New("paycell: SecureCode is required")
	}

	prod := cfg.Environment == Production
	cfg.ProvisionURL = slashed(pick(cfg.ProvisionURL, prod, prodProvisionURL, testProvisionURL))
	cfg.PaymentURL = slashed(pick(cfg.PaymentURL, prod, prodPaymentURL, testPaymentURL))
	cfg.CancelURL = slashed(pick(cfg.CancelURL, prod, prodCancelURL, testCancelURL))
	cfg.SDKInitURL = pick(cfg.SDKInitURL, prod, prodSDKInitURL, testSDKInitURL)
	cfg.SDKHomeURL = slashed(pick(cfg.SDKHomeURL, prod, prodSDKHomeURL, testSDKHomeURL))

	if cfg.ClientIPAddress == "" {
		cfg.ClientIPAddress = "127.0.0.1"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{cfg: cfg, http: hc, now: time.Now, newID: NewTransactionID}, nil
}

// Config returns a copy of the client configuration.
func (c *Client) Config() Config { return c.cfg }

func pick(override string, prod bool, prodURL, testURL string) string {
	if override != "" {
		return override
	}
	if prod {
		return prodURL
	}
	return testURL
}

func slashed(u string) string {
	if u == "" || strings.HasSuffix(u, "/") {
		return u
	}
	return u + "/"
}

// Error is a non-zero responseCode from Paycell.
type Error struct {
	Code          string
	Description   string
	TransactionID string
}

func (e *Error) Error() string {
	if e.Description == "" {
		return "paycell: response code " + e.Code
	}
	return fmt.Sprintf("paycell: %s (code %s)", e.Description, e.Code)
}

// RequestHeader goes on every provision request. Leave it zero and the
// Client fills it in. Set TransactionID yourself if you want a retry to be
// idempotent.
type RequestHeader struct {
	ApplicationName     string `json:"applicationName,omitempty"`
	ApplicationPwd      string `json:"applicationPwd,omitempty"`
	ClientIPAddress     string `json:"clientIPAddress,omitempty"`
	TransactionDateTime string `json:"transactionDateTime,omitempty"`
	TransactionID       string `json:"transactionId,omitempty"`
}

// ResponseHeader comes back from every service.
type ResponseHeader struct {
	TransactionID       string `json:"transactionId,omitempty"`
	ResponseDateTime    string `json:"responseDateTime,omitempty"`
	ResponseCode        string `json:"responseCode,omitempty"`
	ResponseDescription string `json:"responseDescription,omitempty"`
}

// OK reports success.
func (h ResponseHeader) OK() bool { return h.ResponseCode == SuccessCode }

// Err is nil on success, *Error otherwise.
func (h ResponseHeader) Err() error {
	if h.OK() {
		return nil
	}
	return &Error{Code: h.ResponseCode, Description: h.ResponseDescription, TransactionID: h.TransactionID}
}

// KeyValue is one extraParameters entry. Paycell carries per-service flags
// in these.
type KeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Amount is money in minor units. Goes out as a string because that's what
// Paycell wants, comes in as either a string or a number because Paycell
// can't make its mind up per service.
type Amount int64

// TRY converts to lira.
func (a Amount) TRY() float64 { return float64(a) / 100 }

func (a Amount) String() string { return strconv.FormatInt(int64(a), 10) }

func (a Amount) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatInt(int64(a), 10) + `"`), nil
}

func (a *Amount) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*a = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		// Paycell amounts are whole minor units. A decimal means the unit is
		// not what this type assumes, and quietly rounding it would book the
		// wrong sum, so it is an error rather than a guess.
		return fmt.Errorf("paycell: bad amount %q: %w", s, err)
	}
	*a = Amount(n)
	return nil
}

// FlexString is for the fields Paycell quotes on one service and not on the
// next. eulaId is quoted from getCards, bare from getTermsOfServiceContent.
// binValue is always bare. Marshals as a string.
type FlexString string

func (f FlexString) String() string { return string(f) }

func (f *FlexString) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*f = ""
		return nil
	}
	if len(s) > 1 && s[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*f = FlexString(str)
		return nil
	}
	*f = FlexString(s)
	return nil
}

// base is the header every provision service wants.
type base struct {
	RequestHeader RequestHeader `json:"requestHeader"`
}

func (b *base) prepare(c *Client) { c.fillHeader(&b.RequestHeader) }

// merchantBase adds the merchant code and subscriber number most of them
// want on top.
type merchantBase struct {
	base
	MerchantCode string `json:"merchantCode,omitempty"`
	MSISDN       string `json:"msisdn,omitempty"`
}

func (b *merchantBase) prepare(c *Client) {
	b.base.prepare(c)
	if b.MerchantCode == "" {
		b.MerchantCode = c.cfg.MerchantCode
	}
	if b.MSISDN == "" {
		b.MSISDN = c.cfg.DefaultMSISDN
	}
}

// merchantOnlyBase is the same without the subscriber number.
type merchantOnlyBase struct {
	base
	MerchantCode string `json:"merchantCode,omitempty"`
}

func (b *merchantOnlyBase) prepare(c *Client) {
	b.base.prepare(c)
	if b.MerchantCode == "" {
		b.MerchantCode = c.cfg.MerchantCode
	}
}

// commonResponse is the shared response header plus the untouched body.
type commonResponse struct {
	ResponseHeader  ResponseHeader  `json:"responseHeader"`
	ExtraParameters []KeyValue      `json:"extraParameters,omitempty"`
	Raw             json.RawMessage `json:"-"`
}

func (r *commonResponse) head() ResponseHeader { return r.ResponseHeader }
func (r *commonResponse) setRaw(b []byte)      { r.Raw = b }

type request interface{ prepare(*Client) }

type response interface {
	head() ResponseHeader
	setRaw([]byte)
}

func (c *Client) fillHeader(h *RequestHeader) {
	if h.ApplicationName == "" {
		h.ApplicationName = c.cfg.ApplicationName
	}
	if h.ApplicationPwd == "" {
		h.ApplicationPwd = c.cfg.ApplicationPassword
	}
	if h.ClientIPAddress == "" {
		h.ClientIPAddress = c.cfg.ClientIPAddress
	}
	if h.TransactionDateTime == "" {
		h.TransactionDateTime = TransactionDateTime(c.now())
	}
	if h.TransactionID == "" {
		h.TransactionID = c.newID()
	}
}

// post sends JSON, decodes into out, and turns a non-zero responseCode into
// an error. out gets populated either way.
func (c *Client) post(ctx context.Context, url string, in request, out response) error {
	in.prepare(c)
	if err := c.postRaw(ctx, url, in, out); err != nil {
		return err
	}
	return out.head().Err()
}

func (c *Client) postRaw(ctx context.Context, url string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("paycell: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("paycell: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("paycell: %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("paycell: %s: read body: %w", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("paycell: %s: http %d: %s", url, resp.StatusCode, snippet(raw))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("paycell: %s: decode %s: %w", url, snippet(raw), err)
	}
	if r, ok := out.(response); ok {
		r.setRaw(raw)
	}
	return nil
}

// Call posts whatever you give it to a provision service and decodes the
// reply into out. The escape hatch for services this package doesn't model,
// and for fields Paycell adds after a release. service is the path segment,
// "provision/" and so on.
//
// It fills in nothing and judges nothing. Build the body, read the code.
func (c *Client) Call(ctx context.Context, service string, in, out any) error {
	return c.postRaw(ctx, c.cfg.ProvisionURL+strings.TrimPrefix(service, "/"), in, out)
}

func snippet(b []byte) string {
	const n = 256
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// TransactionDateTime formats t as yyyyMMddHHmmssSSS.
func TransactionDateTime(t time.Time) string {
	return t.Format("20060102150405") + fmt.Sprintf("%03d", t.Nanosecond()/int(time.Millisecond))
}

// NewTransactionID returns a 20 digit id. Has to be unique per
// applicationName, hence crypto/rand rather than the clock.
func NewTransactionID() string { return randomDigits(20) }

// NewReferenceNumber returns 20 digits behind Config.ReferencePrefix.
//
// This is what identifies the payment in every later call, so file it with
// the order. Pass your own if the order id is numeric and fits in 20.
func (c *Client) NewReferenceNumber() string {
	p := c.cfg.ReferencePrefix
	if len(p) >= 20 {
		return p[:20]
	}
	return p + randomDigits(20-len(p))
}

func randomDigits(n int) string {
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			// crypto/rand does not fail in practice; if it ever does, a
			// duplicate id is better than a panic in a payment path.
			d = big.NewInt(int64(time.Now().UnixNano() % 10))
		}
		b.WriteByte(byte('0' + d.Int64()))
	}
	return b.String()
}
