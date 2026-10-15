package paycell

import "context"

// Provision service paths, relative to Config.ProvisionURL.
const (
	svcProvision         = "provision/"
	svcProvisionAll      = "provisionAll/"
	svcProvisionMarket   = "provisionForMarketPlace/"
	svcInquire           = "inquire/"
	svcInquireAll        = "inquireAll/"
	svcReverse           = "reverse/"
	svcRefund            = "refund/"
	svcRefundAll         = "refundAll/"
	svcGetCards          = "getCards/"
	svcRegisterCard      = "registerCard/"
	svcUpdateCard        = "updateCard/"
	svcDeleteCard        = "deleteCard/"
	svcThreeDSession     = "getThreeDSession/"
	svcThreeDResult      = "getThreeDSessionResult/"
	svcProvisionHistory  = "getProvisionHistory/"
	svcTermsOfService    = "getTermsOfServiceContent/"
	svcCardBin           = "getCardBinInformation/"
	svcPaymentMethods    = "getPaymentMethods/"
	svcPaymentMethodList = "getPaymentMethodList/"
	svcOpenMobilePayment = "openMobilePayment/"
	svcSendOTP           = "sendOTP/"
	svcValidateOTP       = "validateOTP/"
	svcReconciliation    = "summaryReconciliation/"
)

func (c *Client) provisionCall(ctx context.Context, svc string, in request, out response) error {
	return c.post(ctx, c.cfg.ProvisionURL+svc, in, out)
}

// Provision takes a card payment. Defaults: SALE, TRY, generated reference.
//
// Hang on to ReferenceNumber. Inquire, Reverse and Refund all need it.
func (c *Client) Provision(ctx context.Context, req *ProvisionRequest) (*ProvisionResponse, error) {
	c.defaultProvision(req)
	res := new(ProvisionResponse)
	err := c.provisionCall(ctx, svcProvision, req, res)
	if res.ReferenceNumber == "" {
		res.ReferenceNumber = req.ReferenceNumber
	}
	return res, err
}

func (c *Client) defaultProvision(req *ProvisionRequest) {
	if req.PaymentType == "" {
		req.PaymentType = PaymentTypeSale
	}
	if req.Currency == "" {
		req.Currency = CurrencyTRY
	}
	if req.ReferenceNumber == "" {
		req.ReferenceNumber = c.NewReferenceNumber()
	}
}

// PreAuth holds the amount without taking it. PostAuth takes it later.
func (c *Client) PreAuth(ctx context.Context, req *ProvisionRequest) (*ProvisionResponse, error) {
	req.PaymentType = PaymentTypePreAuth
	return c.Provision(ctx, req)
}

// PostAuth captures a PreAuth. OriginalReferenceNumber is the pre-auth's
// reference, ReferenceNumber a fresh one.
func (c *Client) PostAuth(ctx context.Context, req *ProvisionRequest) (*ProvisionResponse, error) {
	req.PaymentType = PaymentTypePostAuth
	return c.Provision(ctx, req)
}

// ProvisionAll is Provision for the other methods too: DCB, Paycell Kart,
// e-money. PaymentMethodType defaults to CREDIT_CARD.
func (c *Client) ProvisionAll(ctx context.Context, req *ProvisionAllRequest) (*ProvisionResponse, error) {
	c.defaultProvision(&req.ProvisionRequest)
	if req.PaymentMethodType == "" {
		req.PaymentMethodType = MethodCreditCard
	}
	res := new(ProvisionResponse)
	err := c.provisionCall(ctx, svcProvisionAll, req, res)
	if res.ReferenceNumber == "" {
		res.ReferenceNumber = req.ReferenceNumber
	}
	return res, err
}

// ProvisionForMarketPlace splits one payment between sub-merchants.
//
// Untested against a live merchant. The field names come from third-party
// code, so check them against your integration pack.
func (c *Client) ProvisionForMarketPlace(ctx context.Context, req *ProvisionForMarketPlaceRequest) (*ProvisionResponse, error) {
	c.defaultProvision(&req.ProvisionRequest)
	res := new(ProvisionResponse)
	err := c.provisionCall(ctx, svcProvisionMarket, req, res)
	if res.ReferenceNumber == "" {
		res.ReferenceNumber = req.ReferenceNumber
	}
	return res, err
}

// Inquire reports on the payment at OriginalReferenceNumber.
//
// Call it after any timeout before retrying. A payment can be through even
// when the reply never showed up.
func (c *Client) Inquire(ctx context.Context, req *InquireRequest) (*InquireResponse, error) {
	if req.ReferenceNumber == "" {
		req.ReferenceNumber = c.NewReferenceNumber()
	}
	res := new(InquireResponse)
	return res, c.provisionCall(ctx, svcInquire, req, res)
}

// InquireAll is Inquire for the non-card methods.
func (c *Client) InquireAll(ctx context.Context, req *InquireRequest) (*InquireResponse, error) {
	if req.ReferenceNumber == "" {
		req.ReferenceNumber = c.NewReferenceNumber()
	}
	if req.PaymentMethodType == "" {
		req.PaymentMethodType = MethodCreditCard
	}
	res := new(InquireResponse)
	return res, c.provisionCall(ctx, svcInquireAll, req, res)
}

// Reverse voids a same-day payment, always in full. ReferenceNumber is a new
// one for the reversal, OriginalReferenceNumber is the payment being voided.
//
// 2029 means it already settled, so refund instead. 2027 means a refund got
// there first.
func (c *Client) Reverse(ctx context.Context, req *ReverseRequest) (*CancelResponse, error) {
	if req.ReferenceNumber == "" {
		req.ReferenceNumber = c.NewReferenceNumber()
	}
	res := new(CancelResponse)
	return res, c.provisionCall(ctx, svcReverse, req, res)
}

// Refund gives money back from a settled payment.
//
// Amount is mandatory. There's no "refund everything" shortcut, so send the
// original amount for a full refund.
//
// 2008 no amount. 2015 the refunds would total more than was paid. 2027 you
// already refunded this one.
func (c *Client) Refund(ctx context.Context, req *RefundRequest) (*CancelResponse, error) {
	if req.ReferenceNumber == "" {
		req.ReferenceNumber = c.NewReferenceNumber()
	}
	res := new(CancelResponse)
	return res, c.provisionCall(ctx, svcRefund, req, res)
}

// RefundAll is Refund for the non-card methods. Amount still mandatory.
func (c *Client) RefundAll(ctx context.Context, req *RefundRequest) (*CancelResponse, error) {
	if req.ReferenceNumber == "" {
		req.ReferenceNumber = c.NewReferenceNumber()
	}
	if req.PaymentMethodType == "" {
		req.PaymentMethodType = MethodCreditCard
	}
	res := new(CancelResponse)
	return res, c.provisionCall(ctx, svcRefundAll, req, res)
}

// GetThreeDSession opens a 3D session. Defaults: MERCHANT, AUTH, TRY.
func (c *Client) GetThreeDSession(ctx context.Context, req *ThreeDSessionRequest) (*ThreeDSessionResponse, error) {
	if req.Target == "" {
		req.Target = TargetMerchant
	}
	if req.TransactionType == "" {
		req.TransactionType = TransactionTypeAuth
	}
	if req.Currency == "" {
		req.Currency = CurrencyTRY
	}
	res := new(ThreeDSessionResponse)
	return res, c.provisionCall(ctx, svcThreeDSession, req, res)
}

// GetThreeDSessionResult asks how the verification went.
//
// A nil error only means the query worked. Check Verified before you charge
// anyone.
func (c *Client) GetThreeDSessionResult(ctx context.Context, req *ThreeDSessionResultRequest) (*ThreeDSessionResultResponse, error) {
	res := new(ThreeDSessionResultResponse)
	return res, c.provisionCall(ctx, svcThreeDResult, req, res)
}

// GetCards lists the subscriber's stored cards. Usually OTP gated, see
// IsOTPRequired.
//
// Deprecated: Paycell is shutting getCards down and does not allow it in
// production. List cards with GetPaymentMethodList after an OTP.
func (c *Client) GetCards(ctx context.Context, req *GetCardsRequest) (*GetCardsResponse, error) {
	if req == nil {
		req = new(GetCardsRequest)
	}
	res := new(GetCardsResponse)
	return res, c.provisionCall(ctx, svcGetCards, req, res)
}

// RegisterCard puts a card token in the wallet so later payments can just
// use CardID. EulaID falls back to Config.EulaID. See RegisterCardRequest for
// the 3D session it needs.
//
// A card that is in the wallet already comes back as
// CodeCardAlreadyRegistered, with its CardID filled in all the same.
func (c *Client) RegisterCard(ctx context.Context, req *RegisterCardRequest) (*CardResponse, error) {
	if req.EulaID == "" {
		req.EulaID = c.cfg.EulaID
	}
	res := new(CardResponse)
	return res, c.provisionCall(ctx, svcRegisterCard, req, res)
}

// UpdateCard renames a card or makes it the default.
func (c *Client) UpdateCard(ctx context.Context, req *UpdateCardRequest) (*CardResponse, error) {
	if req.EulaID == "" {
		req.EulaID = c.cfg.EulaID
	}
	res := new(CardResponse)
	return res, c.provisionCall(ctx, svcUpdateCard, req, res)
}

// DeleteCard drops a stored card.
func (c *Client) DeleteCard(ctx context.Context, req *DeleteCardRequest) (*CardResponse, error) {
	res := new(CardResponse)
	return res, c.provisionCall(ctx, svcDeleteCard, req, res)
}

// GetTermsOfServiceContent fetches the wallet terms plus the id you send
// back as EulaID.
func (c *Client) GetTermsOfServiceContent(ctx context.Context, req *TermsOfServiceRequest) (*TermsOfServiceResponse, error) {
	if req == nil {
		req = new(TermsOfServiceRequest)
	}
	res := new(TermsOfServiceResponse)
	return res, c.provisionCall(ctx, svcTermsOfService, req, res)
}

// GetCardBinInformation looks up the issuer behind a card's first digits.
// That decides installment options and which acquirer gets the payment.
func (c *Client) GetCardBinInformation(ctx context.Context, req *CardBinRequest) (*CardBinResponse, error) {
	res := new(CardBinResponse)
	return res, c.provisionCall(ctx, svcCardBin, req, res)
}

// GetPaymentMethods lists what the subscriber can pay with. Stored cards,
// and mobile payment with its remaining limit when that's open.
//
// This is the one Paycell's own plugins use. For the card list itself their
// docs point at GetPaymentMethodList; this one is still where the mobile
// payment standing comes from.
func (c *Client) GetPaymentMethods(ctx context.Context, req *PaymentMethodsRequest) (*PaymentMethodsResponse, error) {
	if req == nil {
		req = new(PaymentMethodsRequest)
	}
	res := new(PaymentMethodsResponse)
	return res, c.provisionCall(ctx, svcPaymentMethods, req, res)
}

// GetPaymentMethodList lists the subscriber's stored cards, the way Paycell's
// docs do it. It sits behind an SMS OTP: SendOTP and ValidateOTP with
// CardListOTPParams, then this call with the same ReferenceNumber. Without a
// validated reference it answers 3110, see IsOTPRequired.
//
// ExtraParameters defaults to CardListOTPParams, which the docs ask for here
// too.
//
// Paycell wants a fresh OTP before every listing and does not allow keeping
// the cards on your side, masked or not, so it goes OTP, list, pay each time.
// The sandbox happily reuses a validated reference for days; that is not
// permission.
func (c *Client) GetPaymentMethodList(ctx context.Context, req *PaymentMethodListRequest) (*PaymentMethodsResponse, error) {
	if req == nil {
		req = new(PaymentMethodListRequest)
	}
	if req.ExtraParameters == nil {
		req.ExtraParameters = CardListOTPParams()
	}
	res := new(PaymentMethodsResponse)
	return res, c.provisionCall(ctx, svcPaymentMethodList, req, res)
}

// OpenMobilePayment switches on direct carrier billing once the subscriber
// has accepted the terms. EulaID falls back to Config.EulaID.
//
// Needs a reference number (2007 without one) and an OTP.
func (c *Client) OpenMobilePayment(ctx context.Context, req *OpenMobilePaymentRequest) (*OpenMobilePaymentResponse, error) {
	if req == nil {
		req = new(OpenMobilePaymentRequest)
	}
	if req.EulaID == "" {
		req.EulaID = c.cfg.EulaID
	}
	if req.ReferenceNumber == "" {
		req.ReferenceNumber = c.NewReferenceNumber()
	}
	res := new(OpenMobilePaymentResponse)
	return res, c.provisionCall(ctx, svcOpenMobilePayment, req, res)
}

// SendOTP texts the subscriber a one time password.
func (c *Client) SendOTP(ctx context.Context, req *SendOTPRequest) (*SendOTPResponse, error) {
	if req.Currency == "" && req.Amount != 0 {
		req.Currency = CurrencyTRY
	}
	if req.ReferenceNumber == "" {
		req.ReferenceNumber = c.NewReferenceNumber()
	}
	res := new(SendOTPResponse)
	return res, c.provisionCall(ctx, svcSendOTP, req, res)
}

// ValidateOTP checks the typed code against the token SendOTP handed back.
// Wrong code comes back 3112.
func (c *Client) ValidateOTP(ctx context.Context, req *ValidateOTPRequest) (*ValidateOTPResponse, error) {
	if req.Currency == "" && req.Amount != 0 {
		req.Currency = CurrencyTRY
	}
	res := new(ValidateOTPResponse)
	return res, c.provisionCall(ctx, svcValidateOTP, req, res)
}

// GetProvisionHistory lists a settlement day's transactions.
// ReconciliationDate is YYYYMMDD.
func (c *Client) GetProvisionHistory(ctx context.Context, req *ProvisionHistoryRequest) (*ProvisionHistoryResponse, error) {
	res := new(ProvisionHistoryResponse)
	return res, c.provisionCall(ctx, svcProvisionHistory, req, res)
}

// SummaryReconciliation sends your daily totals for Paycell to compare with
// its own. Every total has to be there, zeroes included, or you get 2018.
func (c *Client) SummaryReconciliation(ctx context.Context, req *ReconciliationRequest) (*ReconciliationResponse, error) {
	res := new(ReconciliationResponse)
	return res, c.provisionCall(ctx, svcReconciliation, req, res)
}
