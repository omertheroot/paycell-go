package paycell

import (
	"context"
	"errors"
	"strings"
)

// Web SDK service paths, relative to Config.CancelURL.
const (
	svcSDKQuery          = "queryPaymentStatus/"
	svcSDKReverse        = "reversePayment/"
	svcSDKRefund         = "refundPayment/"
	svcSDKReconciliation = "summaryReconciliation/"
)

// Payment status from QueryPaymentStatus.
const (
	StatusSuccess  = "0"
	StatusReversed = "1" // cancelled on the day of the payment
	StatusRefunded = "2"
	StatusNotFound = "3"
	StatusPending  = "4"
	StatusUnknown  = "5"
	StatusFailed   = "6" // error or timeout
)

// DefaultTimeout is how long a checkout session lives, in milliseconds.
const DefaultTimeout = 600000

const defaultLanguage = "tr"

// SDKMerchant identifies you to the Web SDK.
type SDKMerchant struct {
	MerchantCode string   `json:"merchantCode,omitempty"`
	TerminalCode string   `json:"terminalCode,omitempty"`
	Logos        []string `json:"logos"`
}

// SDKTransactionInfo is the Web SDK's transaction stamp.
type SDKTransactionInfo struct {
	TransactionDateTime string `json:"transactionDateTime,omitempty"`
	TransactionID       string `json:"transactionId,omitempty"`
}

// SDKRequestHeader is the nested header the session service wants. Different
// shape from the provision header, and it spells applicationPassword out in
// full rather than applicationPwd.
type SDKRequestHeader struct {
	Merchant            SDKMerchant        `json:"merchant"`
	TransactionInfo     SDKTransactionInfo `json:"transactionInfo"`
	ApplicationName     string             `json:"applicationName,omitempty"`
	ApplicationPassword string             `json:"applicationPassword,omitempty"`
}

// Installment is one line of a Web SDK plan: what a card brand pays over how
// many instalments. Add a line with Count 1 to charge a different total on a
// particular instrument.
type Installment struct {
	LineID            int    `json:"lineId"`
	PaymentMethodType string `json:"paymentMethodType,omitempty"` // CREDIT_CARD or DCB
	CardBrand         string `json:"cardBrand,omitempty"`         // BONUS, WORLD, AXESS, ISBANK, FINANS, HALKBANK, TURKCELL
	Count             int    `json:"count"`
	Amount            Amount `json:"amount,omitempty"`
}

// SDKPayment is the payment block of a session.
type SDKPayment struct {
	Amount                 Amount        `json:"amount,omitempty"`
	Currency               string        `json:"currency,omitempty"` // 99 for TRY
	PaymentReferenceNumber string        `json:"paymentReferenceNumber,omitempty"`
	PaymentSecurity        string        `json:"paymentSecurity,omitempty"` // THREED_SECURE or NON_THREED_SECURE
	InstallmentPlan        []Installment `json:"installmentPlan"`
}

// SDKSessionRequest opens a checkout session.
type SDKSessionRequest struct {
	RequestHeader SDKRequestHeader `json:"requestHeader"`
	HashData      string           `json:"hashData,omitempty"`

	// HostAccount is whatever identifies the payer in your system. An email
	// address does. It goes into the signature.
	HostAccount string `json:"hostAccount,omitempty"`

	Language string     `json:"language,omitempty"` // tr or en
	Payment  SDKPayment `json:"payment"`

	// ReturnURL is where the customer lands afterwards. A
	// [paymentReferenceNumber] placeholder gets substituted.
	ReturnURL string `json:"returnUrl,omitempty"`

	// PostResultURL gets a server-to-server POST with the outcome.
	PostResultURL string `json:"postResultUrl,omitempty"`

	// TimeoutDuration is how long the customer gets, in milliseconds.
	TimeoutDuration int `json:"timeoutDuration,omitempty"`
}

// SDKSessionResponse carries the checkout's tracking id.
type SDKSessionResponse struct {
	commonResponse

	TrackingID string `json:"trackingId,omitempty"`

	// TrackingURL is built here, not returned by Paycell. Send the customer
	// to it, in an iframe or a new tab.
	TrackingURL string `json:"-"`
}

// SDKParams is the short form of InitSession.
type SDKParams struct {
	Amount                 Amount
	Currency               string // defaults to 99 (TRY)
	PaymentReferenceNumber string // generated when empty
	PaymentSecurity        string // defaults to THREED_SECURE
	InstallmentPlan        []Installment
	HostAccount            string
	Language               string // defaults to tr
	ReturnURL              string
	PostResultURL          string
	TimeoutDuration        int // milliseconds, defaults to 600000
}

// InitSession opens a checkout session and gives back the tracking id plus
// the URL to send the customer to.
//
// After that either poll QueryPaymentStatus on the same payment reference or
// sit and wait for the POST to PostResultURL.
func (c *Client) InitSession(ctx context.Context, p SDKParams) (*SDKSessionResponse, error) {
	if c.cfg.TerminalCode == "" {
		return nil, errors.New("paycell: TerminalCode is required for the Web SDK")
	}
	if p.PaymentReferenceNumber == "" {
		p.PaymentReferenceNumber = c.NewReferenceNumber()
	}
	if p.Currency == "" {
		p.Currency = SDKCurrencyTRY
	}
	if p.PaymentSecurity == "" {
		p.PaymentSecurity = ThreeDSecure
	}
	if p.Language == "" {
		p.Language = defaultLanguage
	}
	if p.TimeoutDuration <= 0 {
		p.TimeoutDuration = DefaultTimeout
	}

	payment := SDKPayment{
		Amount:                 p.Amount,
		Currency:               p.Currency,
		PaymentReferenceNumber: p.PaymentReferenceNumber,
		PaymentSecurity:        p.PaymentSecurity,
		InstallmentPlan:        p.InstallmentPlan,
	}
	if payment.InstallmentPlan == nil {
		payment.InstallmentPlan = []Installment{}
	}

	req := &SDKSessionRequest{
		RequestHeader: SDKRequestHeader{
			Merchant: SDKMerchant{
				MerchantCode: c.cfg.MerchantCode,
				TerminalCode: c.cfg.TerminalCode,
			},
			TransactionInfo: SDKTransactionInfo{
				TransactionDateTime: TransactionDateTime(c.now()),
				TransactionID:       c.newID(),
			},
			ApplicationName:     c.cfg.ApplicationName,
			ApplicationPassword: c.cfg.ApplicationPassword,
		},
		HashData:        c.sdkHashData(payment, p.HostAccount),
		HostAccount:     p.HostAccount,
		Language:        p.Language,
		Payment:         payment,
		ReturnURL:       strings.ReplaceAll(p.ReturnURL, "[paymentReferenceNumber]", p.PaymentReferenceNumber),
		PostResultURL:   p.PostResultURL,
		TimeoutDuration: p.TimeoutDuration,
	}

	res := new(SDKSessionResponse)
	err := c.postRaw(ctx, c.cfg.SDKInitURL, req, res)
	if res.TrackingID != "" {
		res.TrackingURL = c.cfg.SDKHomeURL + res.TrackingID
	}
	if err != nil {
		return res, err
	}
	// On success this one answers with no responseHeader at all, so a
	// missing tracking id is the only thing we can go on.
	if res.TrackingID == "" {
		if err := res.ResponseHeader.Err(); err != nil {
			return res, err
		}
		return res, errors.New("paycell: session init returned no tracking id: " + snippet(res.Raw))
	}
	return res, nil
}

// SDKQueryRequest asks after a Web SDK payment.
type SDKQueryRequest struct {
	merchantOnlyBase

	OriginalPaymentReferenceNumber string `json:"originalPaymentReferenceNumber,omitempty"`
}

// SDKPaymentMethod is what the payment was made with. For DCB and e-money
// both the id and the number are the phone number.
type SDKPaymentMethod struct {
	PaymentMethodID     string `json:"paymentMethodId,omitempty"`
	PaymentMethodNumber string `json:"paymentMethodNumber,omitempty"`
	PaymentMethodType   string `json:"paymentMethodType,omitempty"`
}

// SDKQueryResponse is where a Web SDK payment stands.
//
// ResponseHeader.ResponseCode is about the query. Status is about the
// payment. Don't confuse them.
type SDKQueryResponse struct {
	commonResponse

	Status            string           `json:"status,omitempty"`
	StatusExplanation string           `json:"statusExplanation,omitempty"`
	MSISDN            string           `json:"msisdn,omitempty"`
	Amount            Amount           `json:"amount,omitempty"`
	Currency          string           `json:"currency,omitempty"`
	InstallmentCount  int              `json:"installmentCount,omitempty"`
	ApprovalCode      string           `json:"approvalCode,omitempty"`
	OrderID           string           `json:"orderId,omitempty"`
	PaymentSecurity   string           `json:"paymentSecurity,omitempty"`
	PaymentDate       string           `json:"paymentDate,omitempty"`
	AcquirerBankCode  string           `json:"acquirerbankCode,omitempty"`
	IssuerBankCode    string           `json:"issuerBankCode,omitempty"`
	PaymentMethod     SDKPaymentMethod `json:"paymentMethod,omitempty"`

	PaymentReferenceNumber string `json:"paymentReferenceNumber,omitempty"`

	// Paycell drops the second i in the wire name here. Not a typo of ours.
	ReconciliationDate string `json:"reconcilationDate,omitempty"`
}

// Paid reports a completed payment.
func (r *SDKQueryResponse) Paid() bool { return r.Status == StatusSuccess }

// QueryPaymentStatus reports where a Web SDK payment stands.
//
// Poll it while the customer is on the page. Call it once more before you
// write a payment off as failed, because a timeout on your side says nothing
// at all about Paycell's.
func (c *Client) QueryPaymentStatus(ctx context.Context, paymentReferenceNumber string) (*SDKQueryResponse, error) {
	req := &SDKQueryRequest{OriginalPaymentReferenceNumber: paymentReferenceNumber}
	res := new(SDKQueryResponse)
	return res, c.post(ctx, c.cfg.CancelURL+svcSDKQuery, req, res)
}

// SDKReverseRequest voids a same-day Web SDK payment.
type SDKReverseRequest struct {
	merchantBase

	OriginalPaymentReferenceNumber string     `json:"originalPaymentReferenceNumber,omitempty"`
	OriginalReferenceNumber        string     `json:"originalReferenceNumber,omitempty"`
	ReferenceNumber                string     `json:"referenceNumber,omitempty"`
	ExtraParameters                []KeyValue `json:"extraParameters,omitempty"`
}

// SDKRefundRequest gives money back from a Web SDK payment.
//
// Paycell's docs call Amount optional here, unlike the provision Refund which
// demands it. Couldn't reach this one in the sandbox to check, so if a full
// refund comes back empty-handed, send the amount.
type SDKRefundRequest struct {
	merchantBase

	OriginalPaymentReferenceNumber string     `json:"originalPaymentReferenceNumber,omitempty"`
	OriginalReferenceNumber        string     `json:"originalReferenceNumber,omitempty"`
	ReferenceNumber                string     `json:"referenceNumber,omitempty"`
	Amount                         Amount     `json:"amount,omitempty"`
	ExtraParameters                []KeyValue `json:"extraParameters,omitempty"`
}

// SDKCancelResponse answers ReversePayment and RefundPayment.
type SDKCancelResponse struct {
	commonResponse

	ReconciliationDate string `json:"reconciliationDate,omitempty"`
	ApprovalCode       string `json:"approvalCode,omitempty"`
	PaymentMethodType  string `json:"paymentMethodType,omitempty"`

	ReverseDetails *SDKCancelDetails `json:"reverseDetails,omitempty"`
	RefundDetails  *SDKCancelDetails `json:"refundDetails,omitempty"`
}

// SDKCancelDetails is the per-method breakdown.
type SDKCancelDetails struct {
	ApprovalCode       string `json:"approvalCode,omitempty"`
	ReconciliationDate string `json:"reconciliationDate,omitempty"`
	PaymentMethodType  string `json:"paymentMethodType,omitempty"`
	Amount             Amount `json:"amount,omitempty"`
}

// ReversePayment voids a Web SDK payment on the day it happened. 2029 means
// you left it too late, refund the full amount instead.
func (c *Client) ReversePayment(ctx context.Context, req *SDKReverseRequest) (*SDKCancelResponse, error) {
	if req.ReferenceNumber == "" {
		req.ReferenceNumber = c.NewReferenceNumber()
	}
	res := new(SDKCancelResponse)
	return res, c.post(ctx, c.cfg.CancelURL+svcSDKReverse, req, res)
}

// RefundPayment gives money back from a settled Web SDK payment.
func (c *Client) RefundPayment(ctx context.Context, req *SDKRefundRequest) (*SDKCancelResponse, error) {
	if req.ReferenceNumber == "" {
		req.ReferenceNumber = c.NewReferenceNumber()
	}
	res := new(SDKCancelResponse)
	return res, c.post(ctx, c.cfg.CancelURL+svcSDKRefund, req, res)
}

// SDKSummaryReconciliation sends your daily Web SDK totals for comparison.
func (c *Client) SDKSummaryReconciliation(ctx context.Context, req *ReconciliationRequest) (*ReconciliationResponse, error) {
	res := new(ReconciliationResponse)
	return res, c.post(ctx, c.cfg.CancelURL+svcSDKReconciliation, req, res)
}
