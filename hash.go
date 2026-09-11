package paycell

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// hash is Paycell's digest: uppercase, SHA-256, base64.
//
// The uppercase is where everyone comes unstuck. It applies to the second
// stage too, so the base64 security data gets uppercased before being hashed
// again. Looks wrong, is correct.
func hash(s string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(s)))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// securityData = hash(applicationPwd + applicationName)
func (c *Client) securityData() string {
	return hash(c.cfg.ApplicationPassword + c.cfg.ApplicationName)
}

//	hashData = hash(applicationName + transactionId + dateTime +
//	                responseCode + cardToken + secureCode + securityData)
//
// responseCode and cardToken are blank on the request side, filled in on the
// response side.
func (c *Client) hashData(transactionID, dateTime, responseCode, cardToken string) string {
	return hash(c.cfg.ApplicationName + transactionID + dateTime +
		responseCode + cardToken + c.cfg.SecureCode + c.securityData())
}

// RequestHash signs a getCardTokenSecure request.
//
// Paycell would rather the browser called getCardTokenSecure itself so the
// card number never reaches your backend. Sign here, pass the hash out with
// the same transaction id and timestamp.
func (c *Client) RequestHash(transactionID, transactionDateTime string) string {
	return c.hashData(transactionID, transactionDateTime, "", "")
}

// ResponseHash recomputes a getCardTokenSecure response signature.
func (c *Client) ResponseHash(transactionID, responseDateTime, responseCode, cardToken string) string {
	return c.hashData(transactionID, responseDateTime, responseCode, cardToken)
}

// ErrHashMismatch means the response signature doesn't match our credentials.
var ErrHashMismatch = errors.New("paycell: response hash mismatch")

// VerifyCardTokenResponse checks a getCardTokenSecure response against the
// transaction id that was sent.
//
// Always call it on anything that came back through a browser. Skip it and
// there is nothing stopping a client making up a card token.
func (c *Client) VerifyCardTokenResponse(transactionID string, res *CardTokenResponse) error {
	if res == nil {
		return ErrHashMismatch
	}
	want := c.ResponseHash(transactionID, res.Header.ResponseDateTime, res.Header.ResponseCode, res.CardToken)
	if subtle.ConstantTimeCompare([]byte(want), []byte(res.HashData)) != 1 {
		return ErrHashMismatch
	}
	return nil
}

// sdkHash is the Web SDK digest. Not the same as the provision one: no
// uppercase, and the security data comes from secureCode + terminalCode
// rather than the application credentials.
func sdkHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// sdkHashData signs a Web SDK session init.
//
//	securityData = sdkHash(secureCode + terminalCode)
//	hashData     = sdkHash(paymentReferenceNumber + terminalCode + amount +
//	                       currency + paymentSecurity + hostAccount +
//	                       installmentPlan... + securityData)
//
// Each plan line adds lineId, paymentMethodType, cardBrand, count and amount,
// in lineId order.
func (c *Client) sdkHashData(p SDKPayment, hostAccount string) string {
	securityData := sdkHash(c.cfg.SecureCode + c.cfg.TerminalCode)

	var b strings.Builder
	b.WriteString(p.PaymentReferenceNumber)
	b.WriteString(c.cfg.TerminalCode)
	b.WriteString(p.Amount.String())
	b.WriteString(p.Currency)
	b.WriteString(p.PaymentSecurity)
	b.WriteString(hostAccount)

	plan := append([]Installment(nil), p.InstallmentPlan...)
	sort.SliceStable(plan, func(i, j int) bool { return plan[i].LineID < plan[j].LineID })
	for _, line := range plan {
		b.WriteString(strconv.Itoa(line.LineID))
		b.WriteString(line.PaymentMethodType)
		b.WriteString(line.CardBrand)
		b.WriteString(strconv.Itoa(line.Count))
		b.WriteString(line.Amount.String())
	}

	b.WriteString(securityData)
	return sdkHash(b.String())
}
