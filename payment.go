package paycell

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"strings"
)

// CardTokenURL is the getCardTokenSecure endpoint.
func (c *Client) CardTokenURL() string { return c.cfg.PaymentURL + "getCardTokenSecure" }

// ThreeDSecureURL is where the customer's browser gets POSTed for 3D.
func (c *Client) ThreeDSecureURL() string { return c.cfg.PaymentURL + "threeDSecure" }

// BuildCardTokenRequest returns a signed getCardTokenSecure body for the
// browser to post.
//
// This keeps the card number out of your process entirely. Verify whatever
// comes back with VerifyCardTokenResponse and the transaction id from the
// header here.
func (c *Client) BuildCardTokenRequest(card Card) *CardTokenRequest {
	req := new(CardTokenRequest)
	req.Header.ApplicationName = c.cfg.ApplicationName
	req.Header.TransactionID = c.newID()
	req.Header.TransactionDateTime = TransactionDateTime(c.now())

	req.CreditCardNo = strings.ReplaceAll(card.Number, " ", "")
	req.ExpireDateMonth = card.ExpireMonth
	req.ExpireDateYear = card.ExpireYear
	req.CVCNo = card.CVC
	req.CCAuthor = card.Holder
	req.HashData = c.RequestHash(req.Header.TransactionID, req.Header.TransactionDateTime)
	return req
}

// GetCardTokenSecure swaps card data for a single use token, checking the
// reply signature on the way back.
//
// Convenient, but it does drag the card number through your process. Use
// BuildCardTokenRequest instead where you can.
func (c *Client) GetCardTokenSecure(ctx context.Context, card Card) (*CardTokenResponse, error) {
	req := c.BuildCardTokenRequest(card)
	res := new(CardTokenResponse)
	if err := c.postRaw(ctx, c.CardTokenURL(), req, res); err != nil {
		return res, err
	}
	if err := res.Header.Err(); err != nil {
		return res, err
	}
	if !c.cfg.SkipResponseHashCheck {
		if err := c.VerifyCardTokenResponse(req.Header.TransactionID, res); err != nil {
			return res, err
		}
	}
	return res, nil
}

// ThreeDFormParams describes the 3D Secure redirect.
type ThreeDFormParams struct {
	// ThreeDSessionID comes from GetThreeDSession.
	ThreeDSessionID string

	// CallbackURL is where Paycell returns the customer. Carry your own
	// reference number in it: the callback itself is not authenticated, so
	// treat it as a hint and confirm with GetThreeDSessionResult.
	CallbackURL string

	// IsPoint marks a payment that spends Paycell points.
	IsPoint bool

	// IsPost3DResult asks Paycell to POST the result to CallbackURL instead
	// of redirecting with query parameters.
	IsPost3DResult bool
}

// ThreeDForm is the form the customer's browser must submit to reach the 3D
// Secure page. Render it with HTML, or build your own form from the fields.
type ThreeDForm struct {
	URL    string
	Fields map[string]string
}

// ThreeDSecureForm builds the redirect form.
//
// The customer's browser has to make this POST. Relaying it from the server
// puts the bank's page in the wrong place and breaks the trip back.
func (c *Client) ThreeDSecureForm(p ThreeDFormParams) ThreeDForm {
	return ThreeDForm{
		URL: c.ThreeDSecureURL(),
		Fields: map[string]string{
			"threeDSessionId": p.ThreeDSessionID,
			"callbackurl":     p.CallbackURL,
			"isPoint":         boolStr(p.IsPoint),
			"isPost3DResult":  boolStr(p.IsPost3DResult),
		},
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

var threeDFormTmpl = template.Must(template.New("3d").Parse(
	`<!doctype html><html><head><meta charset="utf-8"><title>3D Secure</title></head>
<body onload="document.forms[0].submit()">
<form method="post" action="{{.URL}}">
{{range $k, $v := .Fields}}<input type="hidden" name="{{$k}}" value="{{$v}}">
{{end}}<noscript><button type="submit">Devam et</button></noscript>
</form>
</body></html>`))

// HTML renders a page that submits itself. Serve it to the customer.
func (f ThreeDForm) HTML() string {
	var b strings.Builder
	// The template is fixed and every value is escaped, so this cannot fail.
	_ = threeDFormTmpl.Execute(&b, f)
	return b.String()
}

// ------------------------------------------------------------------ flows

// PayParams is the short form of a card payment. Exactly one of Card,
// CardToken or CardID.
type PayParams struct {
	Card      Card   // raw card, tokenised first
	CardToken string // token from an earlier getCardTokenSecure
	CardID    string // card stored in the subscriber's Paycell wallet

	Amount           Amount
	Currency         string // defaults to TRY
	MSISDN           string // defaults to Config.DefaultMSISDN
	ReferenceNumber  string // generated when empty
	InstallmentCount int
	PaymentType      string // defaults to SALE
	AcquirerBankCode string
	PointAmount      Amount
	ExtraParameters  []KeyValue
}

// ErrNoCard means the payment named no card at all.
var ErrNoCard = errors.New("paycell: one of Card, CardToken or CardID is required")

func (c *Client) resolveToken(ctx context.Context, p *PayParams) error {
	if p.CardToken != "" || p.CardID != "" {
		return nil
	}
	if p.Card.Number == "" {
		return ErrNoCard
	}
	res, err := c.GetCardTokenSecure(ctx, p.Card)
	if err != nil {
		return fmt.Errorf("paycell: card token: %w", err)
	}
	p.CardToken = res.CardToken
	return nil
}

func (p PayParams) provision() *ProvisionRequest {
	req := &ProvisionRequest{
		CardID:           p.CardID,
		CardToken:        p.CardToken,
		ReferenceNumber:  p.ReferenceNumber,
		Amount:           p.Amount,
		PointAmount:      p.PointAmount,
		Currency:         p.Currency,
		InstallmentCount: p.InstallmentCount,
		PaymentType:      p.PaymentType,
		AcquirerBankCode: p.AcquirerBankCode,
		ExtraParameters:  p.ExtraParameters,
	}
	req.MSISDN = p.MSISDN
	return req
}

// Pay tokenises if it has to, then charges. The non-3D flow, in one call.
func (c *Client) Pay(ctx context.Context, p PayParams) (*ProvisionResponse, error) {
	if err := c.resolveToken(ctx, &p); err != nil {
		return nil, err
	}
	return c.Provision(ctx, p.provision())
}

// ThreeDStart is what has to survive the customer's trip to the bank. Store
// it under the reference number, Complete3D wants it back.
type ThreeDStart struct {
	ThreeDSessionID string
	CardToken       string
	CardID          string
	ReferenceNumber string
	Form            ThreeDForm
}

// Start3D opens a session and returns the form to send the customer off
// with. Nothing is charged yet. Complete3D does that when they come back.
func (c *Client) Start3D(ctx context.Context, p PayParams, callbackURL string) (*ThreeDStart, error) {
	if err := c.resolveToken(ctx, &p); err != nil {
		return nil, err
	}
	if p.ReferenceNumber == "" {
		p.ReferenceNumber = c.NewReferenceNumber()
	}

	// The reference number is deliberately not sent here: getThreeDSession
	// rejects it (2032). It goes on the provision in Complete3D.
	sess := &ThreeDSessionRequest{
		CardID:           p.CardID,
		CardToken:        p.CardToken,
		Amount:           p.Amount,
		PointAmount:      p.PointAmount,
		Currency:         p.Currency,
		InstallmentCount: p.InstallmentCount,
		TransactionType:  TransactionTypeAuth,
	}
	if p.PaymentType == PaymentTypePreAuth {
		sess.TransactionType = TransactionTypePreAuth
	}
	sess.MSISDN = p.MSISDN

	res, err := c.GetThreeDSession(ctx, sess)
	if err != nil {
		return nil, err
	}

	return &ThreeDStart{
		ThreeDSessionID: res.ThreeDSessionID,
		CardToken:       p.CardToken,
		CardID:          p.CardID,
		ReferenceNumber: p.ReferenceNumber,
		Form: c.ThreeDSecureForm(ThreeDFormParams{
			ThreeDSessionID: res.ThreeDSessionID,
			CallbackURL:     callbackURL,
			IsPoint:         p.PointAmount > 0,
		}),
	}, nil
}

// ErrNotVerified means the customer failed 3D verification.
type ErrNotVerified struct {
	Result      string
	Description string
	MdStatus    string
	MdError     string
}

func (e *ErrNotVerified) Error() string {
	d := e.Description
	if d == "" {
		d = e.MdError
	}
	return fmt.Sprintf("paycell: 3D verification failed: %s (threeDResult %s, mdStatus %s)", d, e.Result, e.MdStatus)
}

// Complete3D checks the result and charges only if the customer passed. Give
// it the ThreeDStart from Start3D.
//
// Don't take the callback's word for anything. It is unauthenticated. The
// session result is the only thing that knows whether verification happened.
func (c *Client) Complete3D(ctx context.Context, start *ThreeDStart, p PayParams) (*ProvisionResponse, error) {
	if start == nil || start.ThreeDSessionID == "" {
		return nil, errors.New("paycell: Complete3D needs the ThreeDStart from Start3D")
	}

	q := &ThreeDSessionResultRequest{ThreeDSessionID: start.ThreeDSessionID}
	q.MSISDN = p.MSISDN
	result, err := c.GetThreeDSessionResult(ctx, q)
	if err != nil {
		return nil, err
	}
	if !result.Verified() {
		return nil, &ErrNotVerified{
			Result:      result.ThreeDOperationResult.ThreeDResult,
			Description: result.ThreeDOperationResult.ThreeDResultDescription,
			MdStatus:    result.MdStatus,
			MdError:     result.MdErrorMessage,
		}
	}

	p.CardToken = start.CardToken
	p.CardID = start.CardID
	p.ReferenceNumber = start.ReferenceNumber
	req := p.provision()
	req.ThreeDSessionID = start.ThreeDSessionID
	return c.Provision(ctx, req)
}
