package paycell

import (
	"encoding/json"
	"strings"
)

// ---------------------------------------------------------------- card token

// Card is raw card data. Goes to getCardTokenSecure and nowhere else.
type Card struct {
	Number      string // creditCardNo, digits only
	ExpireMonth string // MM
	ExpireYear  string // YY or YYYY, as issued
	CVC         string
	Holder      string // ccAuthor, optional
}

// CardTokenRequest is the getCardTokenSecure body. Flatter header than the
// provision one, and no password in it.
type CardTokenRequest struct {
	Header struct {
		ApplicationName     string `json:"applicationName,omitempty"`
		TransactionID       string `json:"transactionId,omitempty"`
		TransactionDateTime string `json:"transactionDateTime,omitempty"`
	} `json:"header"`

	CreditCardNo    string `json:"creditCardNo"`
	ExpireDateMonth string `json:"expireDateMonth"`
	ExpireDateYear  string `json:"expireDateYear"`
	CVCNo           string `json:"cvcNo"`
	CCAuthor        string `json:"ccAuthor,omitempty"`
	HashData        string `json:"hashData"`
}

// CardTokenResponse carries the single-use token.
type CardTokenResponse struct {
	Header    ResponseHeader `json:"header"`
	CardToken string         `json:"cardToken,omitempty"`
	HashData  string         `json:"hashData,omitempty"`

	Raw json.RawMessage `json:"-"`
}

func (r *CardTokenResponse) head() ResponseHeader { return r.Header }
func (r *CardTokenResponse) setRaw(b []byte)      { r.Raw = b }

// ----------------------------------------------------------------- provision

// ProvisionRequest authorises or captures a payment.
//
// One card reference, not both. CardToken for a card just entered, CardID for
// one in the wallet. POSTAUTH also wants OriginalReferenceNumber set to the
// pre-auth's reference.
type ProvisionRequest struct {
	merchantBase

	CardID                  string     `json:"cardId,omitempty"`
	CardToken               string     `json:"cardToken,omitempty"`
	ReferenceNumber         string     `json:"referenceNumber,omitempty"`
	OriginalReferenceNumber string     `json:"originalReferenceNumber,omitempty"`
	Amount                  Amount     `json:"amount,omitempty"`
	PointAmount             Amount     `json:"pointAmount,omitempty"`
	Currency                string     `json:"currency,omitempty"`
	InstallmentCount        int        `json:"installmentCount,omitempty"`
	PaymentType             string     `json:"paymentType,omitempty"`
	AcquirerBankCode        string     `json:"acquirerBankCode,omitempty"`
	ThreeDSessionID         string     `json:"threeDSessionId,omitempty"`
	PIN                     string     `json:"pin,omitempty"`
	ExtraParameters         []KeyValue `json:"extraParameters,omitempty"`
}

// ProvisionResponse comes back from provision, provisionAll and
// provisionForMarketPlace alike.
type ProvisionResponse struct {
	commonResponse

	OrderID            string `json:"orderId,omitempty"`
	AcquirerBankCode   string `json:"acquirerBankCode,omitempty"`
	IssuerBankCode     string `json:"issuerBankCode,omitempty"`
	ApprovalCode       string `json:"approvalCode,omitempty"`
	ReconciliationDate string `json:"reconciliationDate,omitempty"`
	ReferenceNumber    string `json:"referenceNumber,omitempty"`

	// Only set when the payment went through iyzico.
	IyzPaymentID            string `json:"iyzPaymentId,omitempty"`
	IyzPaymentTransactionID string `json:"iyzPaymentTransactionId,omitempty"`
}

// ProvisionAllRequest is provision plus the non-card methods: DCB, Paycell
// Kart, e-money.
type ProvisionAllRequest struct {
	ProvisionRequest

	PaymentMethodType string `json:"paymentMethodType,omitempty"`
}

// ProvisionForMarketPlaceRequest splits a payment between sub-merchants.
type ProvisionForMarketPlaceRequest struct {
	ProvisionRequest

	MarketPlaceList []MarketPlaceItem `json:"marketPlaceList,omitempty"`
}

// MarketPlaceItem is one sub-merchant's share. They have to add up to the
// payment.
type MarketPlaceItem struct {
	MerchantCode string `json:"merchantCode,omitempty"`
	Amount       Amount `json:"amount,omitempty"`
	OrderID      string `json:"orderId,omitempty"`
	ItemID       string `json:"itemId,omitempty"`
}

// ------------------------------------------------------- inquire / reverse / refund

// InquireRequest asks after an earlier payment.
type InquireRequest struct {
	merchantBase

	ReferenceNumber         string `json:"referenceNumber,omitempty"`
	OriginalReferenceNumber string `json:"originalReferenceNumber,omitempty"`
	PaymentMethodType       string `json:"paymentMethodType,omitempty"` // inquireAll only
}

// InquireResponse lists every attempt against the reference number.
type InquireResponse struct {
	commonResponse

	OrderID            string          `json:"orderId,omitempty"`
	AcquirerBankCode   string          `json:"acquirerBankCode,omitempty"`
	IssuerBankCode     string          `json:"issuerBankCode,omitempty"`
	Status             string          `json:"status,omitempty"`
	Amount             Amount          `json:"amount,omitempty"`
	ApprovalCode       string          `json:"approvalCode,omitempty"`
	ReconciliationDate string          `json:"reconciliationDate,omitempty"`
	ProvisionList      []ProvisionItem `json:"provisionList,omitempty"`
}

// ProvisionItem is one line of an inquire or history reply.
//
// Careful here. Rejected attempts show up in this list too, carrying
// responseCode "0" like everything else. Applied is what tells them apart:
// only the ones that moved money got an approval code.
type ProvisionItem struct {
	ProvisionType       string `json:"provisionType,omitempty"` // SALE, REVERSE, REFUND, PREAUTH, POSTAUTH
	TransactionID       string `json:"transactionId,omitempty"`
	Amount              Amount `json:"amount,omitempty"`
	ApprovalCode        string `json:"approvalCode,omitempty"`
	DateTime            string `json:"dateTime,omitempty"`
	ReconciliationDate  string `json:"reconciliationDate,omitempty"`
	ReferenceNumber     string `json:"referenceNumber,omitempty"`
	OrderID             string `json:"orderId,omitempty"`
	ResponseCode        string `json:"responseCode,omitempty"`
	ResponseDescription string `json:"responseDescription,omitempty"`
	PaymentMethodType   string `json:"paymentMethodType,omitempty"`
}

// Applied reports whether this entry actually happened.
//
// Add up every REFUND line and you will overstate what went back, because the
// rejected attempts sit in the list wearing the same responseCode as the
// successful ones. What they don't have is an approval code, a dateTime or a
// reconciliation date.
func (p ProvisionItem) Applied() bool {
	return p.ApprovalCode != "" && p.ResponseCode == SuccessCode
}

// ReverseRequest voids a payment, same day only. After that it's Refund's
// job, and a 2029 is Paycell telling you so.
//
// No partial reverse exists. Send an amount and the service ignores it and
// voids the lot, which is why there's no Amount field here. Use Refund for
// part of the money.
type ReverseRequest struct {
	merchantBase

	ReferenceNumber         string     `json:"referenceNumber,omitempty"`
	OriginalReferenceNumber string     `json:"originalReferenceNumber,omitempty"`
	PaymentType             string     `json:"paymentType,omitempty"`
	AcquirerBankCode        string     `json:"acquirerBankCode,omitempty"`
	ThreeDSessionID         string     `json:"threeDSessionId,omitempty"`
	PaymentMethodType       string     `json:"paymentMethodType,omitempty"`
	ExtraParameters         []KeyValue `json:"extraParameters,omitempty"`
}

// RefundRequest gives money back from a settled payment.
//
// Amount is required, 2008 without it. Full refund means sending the original
// amount, there's no shortcut for it.
type RefundRequest struct {
	merchantBase

	ReferenceNumber         string     `json:"referenceNumber,omitempty"`
	OriginalReferenceNumber string     `json:"originalReferenceNumber,omitempty"`
	Amount                  Amount     `json:"amount,omitempty"`
	PaymentMethodType       string     `json:"paymentMethodType,omitempty"`
	ExtraParameters         []KeyValue `json:"extraParameters,omitempty"`
}

// CancelResponse comes back from both reverse and refund. RetryStatusCode
// shows up when Paycell couldn't reach the acquirer and plans to try again.
type CancelResponse struct {
	commonResponse

	OrderID                string `json:"orderId,omitempty"`
	ApprovalCode           string `json:"approvalCode,omitempty"`
	ReconciliationDate     string `json:"reconciliationDate,omitempty"`
	RetryStatusCode        string `json:"retryStatusCode,omitempty"`
	RetryStatusDescription string `json:"retryStatusDescription,omitempty"`
}

// ---------------------------------------------------------------------- 3D

// ThreeDSessionRequest opens a 3D session for a card.
type ThreeDSessionRequest struct {
	merchantBase

	CardID           string `json:"cardId,omitempty"`
	CardToken        string `json:"cardToken,omitempty"`
	Amount           Amount `json:"amount,omitempty"`
	PointAmount      Amount `json:"pointAmount,omitempty"`
	Currency         string `json:"currency,omitempty"`
	InstallmentCount int    `json:"installmentCount,omitempty"`
	Target           string `json:"target,omitempty"`          // MERCHANT
	TransactionType  string `json:"transactionType,omitempty"` // AUTH or PREAUTH
	SaveCard         bool   `json:"saveCard,omitempty"`
}

// No ReferenceNumber field on purpose: this service rejects one with 2032.
// It belongs on the provision that follows.

// ThreeDSessionResponse carries the id the customer gets redirected with.
type ThreeDSessionResponse struct {
	commonResponse

	ThreeDSessionID string `json:"threeDSessionId,omitempty"`
}

// ThreeDSessionResultRequest asks how the verification went.
type ThreeDSessionResultRequest struct {
	merchantBase

	ThreeDSessionID string `json:"threeDSessionId,omitempty"`
	TransactionType string `json:"transactionType,omitempty"`
}

// Same story here, 2041 instead of 2032.

// ThreeDSessionResultResponse reports the outcome. A clean responseHeader
// only means the query worked, so check Verified.
type ThreeDSessionResultResponse struct {
	commonResponse

	CurrentStep    string `json:"currentStep,omitempty"`
	MdStatus       string `json:"mdStatus,omitempty"`
	MdErrorMessage string `json:"mdErrorMessage,omitempty"`

	ThreeDOperationResult struct {
		ThreeDResult            string `json:"threeDResult,omitempty"`
		ThreeDResultDescription string `json:"threeDResultDescription,omitempty"`
	} `json:"threeDOperationResult,omitempty"`
}

// Verified reports whether the customer passed, meaning threeDResult "0".
// Nothing gets charged before this is true.
func (r *ThreeDSessionResultResponse) Verified() bool {
	return r.ThreeDOperationResult.ThreeDResult == SuccessCode
}

// ------------------------------------------------------------------- cards

// StoredCard is a card sitting in a subscriber's Paycell wallet.
type StoredCard struct {
	CardID            string `json:"cardId,omitempty"`
	CardBrand         string `json:"cardBrand,omitempty"`
	CardType          string `json:"cardType,omitempty"`
	MaskedCardNo      string `json:"maskedCardNo,omitempty"`
	Alias             string `json:"alias,omitempty"`
	ActivationDate    string `json:"activationDate,omitempty"`
	IsDefault         bool   `json:"isDefault,omitempty"`
	IsExpired         bool   `json:"isExpired,omitempty"`
	ShowEulaID        bool   `json:"showEulaId,omitempty"`
	IsThreeDValidated bool   `json:"isThreeDValidated,omitempty"`
	IsOTPValidated    bool   `json:"isOTPValidated,omitempty"`
	ExpiryMonth       string `json:"expiryMonth,omitempty"`
	ExpiryYear        string `json:"expiryYear,omitempty"`

	IssuerBankCode        string `json:"issuerBankCode,omitempty"`
	IssuerBankDescription string `json:"issuerBankDescription,omitempty"`
}

// GetCardsRequest lists a subscriber's stored cards.
//
// OTP gated. Without a valid OTPToken you get CodeOTPRequired back, see
// IsOTPRequired.
type GetCardsRequest struct {
	merchantBase

	ReferenceNumber string `json:"referenceNumber,omitempty"`
	OTPToken        string `json:"otpToken,omitempty"`
}

// GetCardsResponse is the stored card list.
type GetCardsResponse struct {
	commonResponse

	CardList       []StoredCard `json:"cardList,omitempty"`
	PaymentMethods []StoredCard `json:"paymentMethods,omitempty"`
	EulaID         FlexString   `json:"eulaId,omitempty"`
}

// Cards reads the list out of whichever field this deployment filled in.
// Both cardList and paymentMethods turn up in the wild.
func (r *GetCardsResponse) Cards() []StoredCard {
	if len(r.CardList) > 0 {
		return r.CardList
	}
	return r.PaymentMethods
}

// RegisterCardRequest puts a card in the wallet. CardToken comes from
// getCardTokenSecure, EulaID from GetTermsOfServiceContent. ThreeDSessionID
// only if the merchant insists on verification before storing.
type RegisterCardRequest struct {
	merchantBase

	CardToken       string `json:"cardToken,omitempty"`
	Alias           string `json:"alias,omitempty"`
	IsDefault       bool   `json:"isDefault,omitempty"`
	EulaID          string `json:"eulaId,omitempty"`
	ThreeDSessionID string `json:"threeDSessionId,omitempty"`
}

// UpdateCardRequest renames a card or makes it the default.
type UpdateCardRequest struct {
	merchantBase

	CardID          string `json:"cardId,omitempty"`
	CardToken       string `json:"cardToken,omitempty"`
	Alias           string `json:"alias,omitempty"`
	IsDefault       bool   `json:"isDefault,omitempty"`
	EulaID          string `json:"eulaId,omitempty"`
	ThreeDSessionID string `json:"threeDSessionId,omitempty"`
}

// DeleteCardRequest drops a stored card.
type DeleteCardRequest struct {
	merchantBase

	CardID string `json:"cardId,omitempty"`
}

// CardResponse comes back from registerCard, updateCard and deleteCard.
type CardResponse struct {
	commonResponse

	CardID string `json:"cardId,omitempty"`
}

// TermsOfServiceRequest fetches the current terms.
type TermsOfServiceRequest struct {
	base
}

// TermsOfServiceResponse carries the terms and the id you send back as
// EulaID. Use ID to read it, the service leaves it unquoted.
type TermsOfServiceResponse struct {
	commonResponse

	EulaID FlexString `json:"eulaId,omitempty"`

	TermsOfServiceHTMLContentTR string `json:"termsOfServiceHtmlContentTR,omitempty"`
	TermsOfServiceHTMLContentEN string `json:"termsOfServiceHtmlContentEN,omitempty"`
}

// ID is the terms id, for EulaID.
func (r *TermsOfServiceResponse) ID() string { return string(r.EulaID) }

// CardBinRequest looks up an issuer from a card's first digits.
type CardBinRequest struct {
	merchantBase

	BinValue string `json:"binValue,omitempty"`
}

// CardBinInfo describes one card range. CardType is free text out of the
// issuer ("Credit Card", "Debit Card"), so go through IsCreditCard.
type CardBinInfo struct {
	BinValue         FlexString `json:"binValue,omitempty"`
	CardType         string     `json:"cardType,omitempty"`
	CardBrand        string     `json:"cardBrand,omitempty"`
	CardOrganization string     `json:"cardOrganization,omitempty"`
	BankName         string     `json:"bankName,omitempty"`
	BankCode         string     `json:"bankCode,omitempty"`
	CommercialType   string     `json:"commercialType,omitempty"` // BIREYSEL or TICARI
	TypeID           int        `json:"typeId,omitempty"`
}

// IsCreditCard decides whether you can offer installments.
func (i CardBinInfo) IsCreditCard() bool {
	return strings.EqualFold(strings.TrimSpace(i.CardType), "Credit Card")
}

// IsCommercial reports a commercial card.
func (i CardBinInfo) IsCommercial() bool {
	return strings.EqualFold(strings.TrimSpace(i.CommercialType), "TICARI")
}

// CardBinResponse describes the card behind a BIN. The service answers with
// a list even for one BIN, so use First.
type CardBinResponse struct {
	commonResponse

	CardBinInformations []CardBinInfo `json:"cardBinInformations,omitempty"`
}

// First returns the record, or nil for an unknown range.
func (r *CardBinResponse) First() *CardBinInfo {
	if len(r.CardBinInformations) == 0 {
		return nil
	}
	return &r.CardBinInformations[0]
}

// ---------------------------------------------------- payment methods / DCB

// PaymentMethodsRequest lists what the subscriber can pay with. Paycell's
// own plugins use this one to show stored cards.
//
// OTP gated, same as GetCardsRequest.
type PaymentMethodsRequest struct {
	merchantBase

	ReferenceNumber string `json:"referenceNumber,omitempty"`
	OTPToken        string `json:"otpToken,omitempty"`
}

// PaymentMethodListRequest asks for the stored cards. ReferenceNumber is the
// one the card-list OTP went out and got validated with.
type PaymentMethodListRequest struct {
	merchantBase

	ReferenceNumber string     `json:"referenceNumber,omitempty"`
	ExtraParameters []KeyValue `json:"extraParameters,omitempty"`
}

// MobilePaymentInfo is where a subscriber stands on direct carrier billing.
type MobilePaymentInfo struct {
	EulaID         string `json:"eulaId,omitempty"`
	EulaURL        string `json:"eulaUrl,omitempty"`
	SignedEulaID   string `json:"signedEulaId,omitempty"`
	StatementDate  string `json:"statementDate,omitempty"`
	Limit          Amount `json:"limit,omitempty"`
	MaxLimit       Amount `json:"maxLimit,omitempty"`
	RemainingLimit Amount `json:"remainingLimit,omitempty"`
	IsDcbOpen      bool   `json:"isDcbOpen,omitempty"`
	IsEulaExpired  bool   `json:"isEulaExpired,omitempty"`
}

// PaymentMethodsResponse holds the cards and the mobile payment state.
type PaymentMethodsResponse struct {
	commonResponse

	EulaID         FlexString         `json:"eulaId,omitempty"`
	CardList       []StoredCard       `json:"cardList,omitempty"`
	PaymentMethods []StoredCard       `json:"paymentMethods,omitempty"`
	MobilePayment  *MobilePaymentInfo `json:"mobilePayment,omitempty"`
}

// Cards reads the list out of whichever field this deployment filled in.
// Both cardList and paymentMethods turn up in the wild.
func (r *PaymentMethodsResponse) Cards() []StoredCard {
	if len(r.CardList) > 0 {
		return r.CardList
	}
	return r.PaymentMethods
}

// OpenMobilePaymentRequest switches on direct carrier billing for a
// subscriber who has accepted the terms at EulaID.
//
// ReferenceNumber is required, 2007 without it. OTP gated as well.
type OpenMobilePaymentRequest struct {
	merchantBase

	EulaID          string `json:"eulaID,omitempty"`
	ReferenceNumber string `json:"referenceNumber,omitempty"`
	OTPToken        string `json:"otpToken,omitempty"`
}

// OpenMobilePaymentResponse is what openMobilePayment answers.
type OpenMobilePaymentResponse struct {
	commonResponse
}

// --------------------------------------------------------------------- OTP

// SendOTPRequest texts a one time password to the subscriber.
type SendOTPRequest struct {
	merchantBase

	Amount          Amount     `json:"amount,omitempty"`
	Currency        string     `json:"currency,omitempty"`
	ReferenceNumber string     `json:"referenceNumber,omitempty"`
	ExtraParameters []KeyValue `json:"extraParameters,omitempty"`
}

// SendOTPResponse carries the token ValidateOTP has to echo back.
type SendOTPResponse struct {
	commonResponse

	Token               string `json:"token,omitempty"`
	ExpireDate          string `json:"expireDate,omitempty"`
	RemainingRetryCount int    `json:"remainingRetryCount,omitempty"`
}

// ValidateOTPRequest checks what the subscriber typed.
type ValidateOTPRequest struct {
	merchantBase

	OTP             string     `json:"otp,omitempty"`
	Token           string     `json:"token,omitempty"`
	Amount          Amount     `json:"amount,omitempty"`
	Currency        string     `json:"currency,omitempty"`
	ReferenceNumber string     `json:"referenceNumber,omitempty"`
	ExtraParameters []KeyValue `json:"extraParameters,omitempty"`
}

// ValidateOTPResponse is what validateOTP answers.
type ValidateOTPResponse struct {
	commonResponse

	RemainingRetryCount int `json:"remainingRetryCount,omitempty"`
}

// ---------------------------------------------------------- reconciliation

// ProvisionHistoryRequest asks for a day's transactions.
type ProvisionHistoryRequest struct {
	merchantBase

	ReconciliationDate string `json:"reconciliationDate,omitempty"` // YYYYMMDD
	ReferenceNumber    string `json:"referenceNumber,omitempty"`
}

// ProvisionHistoryResponse is that day's list.
type ProvisionHistoryResponse struct {
	commonResponse

	ProvisionList []ProvisionItem `json:"provisionList,omitempty"`
}

// ReconciliationRequest states your own totals for a day so Paycell can
// compare. Every field has to be sent, zeroes included, or you get 2018.
// That's why none of them are omitempty.
type ReconciliationRequest struct {
	merchantBase

	ReconciliationDate string `json:"reconciliationDate,omitempty"` // YYYYMMDD

	TotalSaleAmount     Amount `json:"totalSaleAmount"`
	TotalReverseAmount  Amount `json:"totalReverseAmount"`
	TotalRefundAmount   Amount `json:"totalRefundAmount"`
	TotalPreAuthAmount  Amount `json:"totalPreAuthAmount"`
	TotalPostAuthAmount Amount `json:"totalPostAuthAmount"`

	TotalPreAuthReverseAmount  Amount `json:"totalPreAuthReverseAmount"`
	TotalPostAuthReverseAmount Amount `json:"totalPostAuthReverseAmount"`

	TotalSaleCount     int `json:"totalSaleCount"`
	TotalReverseCount  int `json:"totalReverseCount"`
	TotalRefundCount   int `json:"totalRefundCount"`
	TotalPreAuthCount  int `json:"totalPreAuthCount"`
	TotalPostAuthCount int `json:"totalPostAuthCount"`

	TotalPreAuthReverseCount  int `json:"totalPreAuthReverseCount"`
	TotalPostAuthReverseCount int `json:"totalPostAuthReverseCount"`
}

// ReconciliationResponse says whether the two sides agree.
type ReconciliationResponse struct {
	commonResponse

	ReconciliationResult string `json:"reconciliationResult,omitempty"`
	ReconciliationDate   string `json:"reconciliationDate,omitempty"`
}
