//go:build integration

// Integration tests against the Paycell sandbox.
//
//	go test -tags integration -v
//
// Defaults are the credentials Paycell publishes for integration work,
// application PAYCELLTEST on merchant 9998. Point them at your own test
// merchant with:
//
//	PAYCELL_APP_NAME PAYCELL_APP_PASSWORD PAYCELL_SECURE_CODE
//	PAYCELL_MERCHANT_CODE PAYCELL_EULA_ID PAYCELL_PREFIX PAYCELL_MSISDN
//
// Paycell allow-lists server addresses in production. Both the shared
// sandbox and a merchant test channel answered from an unregistered one.
//
// No real money involved. Sandbox amounts, and everything gets reversed,
// refunded or deleted again.
package paycell

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func sandbox(t *testing.T) *Client {
	t.Helper()
	c, err := New(Config{
		ApplicationName:     env("PAYCELL_APP_NAME", "PAYCELLTEST"),
		ApplicationPassword: env("PAYCELL_APP_PASSWORD", "PaycellTestPassword"),
		SecureCode:          env("PAYCELL_SECURE_CODE", "PAYCELL12345"),
		MerchantCode:        env("PAYCELL_MERCHANT_CODE", "9998"),
		EulaID:              env("PAYCELL_EULA_ID", "17"),
		ReferencePrefix:     env("PAYCELL_PREFIX", "666"),
		DefaultMSISDN:       env("PAYCELL_MSISDN", "905380521479"),
		Environment:         Test,
		Timeout:             30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A decode error means a struct here is wrong. A Paycell response code means
// the sandbox isn't in the state the test wanted. Only the first is our
// problem.
func checkDecode(t *testing.T, label string, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), "decode") {
		t.Errorf("%s: response does not match the struct: %v", label, err)
	}
}

// Paycell's mock bank card, the only one the sandbox actually charges. The
// bank test cards pass 3D and then get 4000 on the provision. It expires
// 12/26, so look for a new one on Paycell's test card page after that.
var sandboxTestCard = Card{Number: "1000000000000016", ExpireMonth: "12", ExpireYear: "26", CVC: "000"}

// The OTP the sandbox accepts. Its 3D page wants 123456, a different code.
const sandboxOTP = "111111"

// Paycell only redirects to registered callback hosts. localhost is one in
// the sandbox, whatever the port and path.
const sandboxCallback = "http://localhost/paycell-go-test"

// pay charges the test card without 3D and hands back the reference.
func pay(t *testing.T, c *Client, amount Amount) string {
	t.Helper()
	ref := c.NewReferenceNumber()
	res, err := c.Pay(context.Background(), PayParams{Card: sandboxTestCard, Amount: amount, ReferenceNumber: ref})
	checkDecode(t, "provisionAll", err)
	if err != nil {
		t.Fatalf("pay: %v", err)
	}
	if res.OrderID == "" || res.ApprovalCode == "" {
		t.Errorf("paid without an order id or approval code: %+v", res)
	}
	return ref
}

// passSandbox3D does what the customer's browser would on the sandbox's
// simulated bank page. That page checks the code in JavaScript and then just
// posts action=success, so nothing has to be typed.
func passSandbox3D(t *testing.T, c *Client, sessionID string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	post := func(u string, form url.Values) string {
		t.Helper()
		resp, err := hc.PostForm(u, form)
		if err != nil {
			t.Fatalf("3D page: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	between := func(s, start string) string {
		_, s, _ = strings.Cut(s, start)
		s, _, _ = strings.Cut(s, `"`)
		return html.UnescapeString(s)
	}

	page := post(c.ThreeDSecureURL(), url.Values{
		"threeDSessionId": {sessionID},
		"callbackurl":     {sandboxCallback},
		"isPost3DResult":  {"true"},
	})
	if !strings.Contains(page, "mockBankResult") {
		t.Fatalf("no simulated bank page, Paycell said: %s", snippet([]byte(page)))
	}
	result := post(strings.TrimSuffix(c.ThreeDSecureURL(), "threeDSecure")+"mockBankResult", url.Values{
		"threeDSessionId": {sessionID},
		"action":          {"success"},
	})
	post(between(result, `action="`), url.Values{"mdStatus": {between(result, `name="mdStatus" value="`)}})
}

// The signature is the likeliest thing to have wrong, and the sandbox is the
// only place that can settle it.
func TestSandboxCardToken(t *testing.T) {
	c := sandbox(t)
	res, err := c.GetCardTokenSecure(context.Background(), sandboxTestCard)
	if err != nil {
		t.Fatalf("getCardTokenSecure: %v", err)
	}
	if res.CardToken == "" {
		t.Fatal("no card token returned")
	}
	t.Logf("card token %s, response hash verified", res.CardToken)
}

func TestSandboxLookups(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()

	terms, err := c.GetTermsOfServiceContent(ctx, nil)
	checkDecode(t, "getTermsOfServiceContent", err)
	if err == nil {
		if terms.ID() == "" {
			t.Error("no eula id returned")
		}
		if terms.TermsOfServiceHTMLContentTR == "" {
			t.Error("no Turkish terms returned")
		}
		t.Logf("eulaId=%s trTerms=%dB", terms.ID(), len(terms.TermsOfServiceHTMLContentTR))
	}

	bin, err := c.GetCardBinInformation(ctx, &CardBinRequest{BinValue: "520019"})
	checkDecode(t, "getCardBinInformation", err)
	if info := bin.First(); err == nil && info == nil {
		// The shared sandbox no longer knows this range, a test channel does.
		t.Log("no bin record for 520019")
	} else if err == nil {
		if !info.IsCreditCard() {
			t.Errorf("520019 should be a credit card, got %q", info.CardType)
		}
		t.Logf("bin %s %s %s commercial=%v", info.BinValue, info.BankName, info.CardBrand, info.IsCommercial())
	}

	methods, err := c.GetPaymentMethods(ctx, nil)
	checkDecode(t, "getPaymentMethods", err)
	if IsOTPRequired(err) {
		t.Log("stored cards are OTP gated for this subscriber")
	}
	if err == nil {
		t.Logf("payment methods: %d stored cards", len(methods.Cards()))
	}
}

// The whole money path. Pay 1 TL, find it in the inquiry, reverse it.
func TestSandboxPayInquireReverse(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()

	ref := pay(t, c, 100)

	inq, err := c.InquireAll(ctx, &InquireRequest{OriginalReferenceNumber: ref})
	checkDecode(t, "inquireAll", err)
	if err != nil {
		t.Errorf("inquireAll: %v", err)
	} else {
		if len(inq.ProvisionList) == 0 {
			t.Error("the payment does not appear in its own inquiry")
		}
		for _, p := range inq.ProvisionList {
			t.Logf("  %s %d kuruş %s", p.ProvisionType, p.Amount, p.ResponseDescription)
			if p.ProvisionType == PaymentTypeSale && p.Amount != 100 {
				t.Errorf("inquiry reports %d kuruş, want 100", p.Amount)
			}
		}
	}

	// Give the money back whatever happened above.
	rev, err := c.Reverse(ctx, &ReverseRequest{OriginalReferenceNumber: ref, Amount: 100})
	checkDecode(t, "reverse", err)
	if err != nil {
		t.Fatalf("reverse (the sandbox payment is still standing): %v", err)
	}
	t.Logf("reversed, approval %s reconciliation %s", rev.ApprovalCode, rev.ReconciliationDate)
}

// A session nobody took to the bank must not read as verified.
func TestSandboxThreeDSession(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()

	token, err := c.GetCardTokenSecure(ctx, sandboxTestCard)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := c.GetThreeDSession(ctx, &ThreeDSessionRequest{CardToken: token.CardToken, Amount: 100})
	checkDecode(t, "getThreeDSession", err)
	if err != nil {
		t.Fatalf("getThreeDSession: %v", err)
	}
	if sess.ThreeDSessionID == "" {
		t.Fatal("no session id returned")
	}

	result, err := c.GetThreeDSessionResult(ctx, &ThreeDSessionResultRequest{
		ThreeDSessionID: sess.ThreeDSessionID,
	})
	checkDecode(t, "getThreeDSessionResult", err)
	if err != nil {
		t.Fatalf("getThreeDSessionResult: %v", err)
	}
	if result.Verified() {
		t.Error("an untouched session must not report as verified")
	}
	t.Logf("session %s step %q verified=%v", sess.ThreeDSessionID, result.CurrentStep, result.Verified())
}

// The 3D flow end to end with a freshly entered card.
func TestSandbox3DPayment(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()

	params := PayParams{Card: sandboxTestCard, Amount: 100}
	start, err := c.Start3D(ctx, params, sandboxCallback)
	if err != nil {
		t.Fatalf("start3D: %v", err)
	}
	passSandbox3D(t, c, start.ThreeDSessionID)

	res, err := c.Complete3D(ctx, start, params)
	checkDecode(t, "complete3D", err)
	if err != nil {
		t.Fatalf("complete3D: %v", err)
	}
	t.Logf("paid with 3D, order %s approval %s", res.OrderID, res.ApprovalCode)

	if _, err := c.Reverse(ctx, &ReverseRequest{OriginalReferenceNumber: start.ReferenceNumber, Amount: 100}); err != nil {
		t.Fatalf("reverse (the sandbox payment is still standing): %v", err)
	}
}

// What Paycell asks to see before go-live: store a card, list it behind the
// OTP, pay with it with and without 3D, delete it.
func TestSandboxStoredCard(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()

	terms, err := c.GetTermsOfServiceContent(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Storing needs a session that passed 3D. Nothing is bought, so 1 kuruş
	// and no provision.
	token, err := c.GetCardTokenSecure(ctx, sandboxTestCard)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := c.GetThreeDSession(ctx, &ThreeDSessionRequest{CardToken: token.CardToken, Amount: 1})
	if err != nil {
		t.Fatalf("getThreeDSession: %v", err)
	}
	passSandbox3D(t, c, sess.ThreeDSessionID)

	reg, err := c.RegisterCard(ctx, &RegisterCardRequest{
		CardToken:       token.CardToken,
		ThreeDSessionID: sess.ThreeDSessionID,
		Alias:           "paycell-go test",
		EulaID:          terms.ID(),
	})
	checkDecode(t, "registerCard", err)
	switch {
	case HasCode(err, "3002"):
		t.Skipf("the subscriber is at the card limit: %v", err)
	case HasCode(err, CodeCardAlreadyRegistered):
		t.Logf("already stored as %s, leaving it there", reg.CardID)
	case err != nil:
		t.Fatalf("registerCard: %v", err)
	default:
		t.Cleanup(func() {
			if _, err := c.DeleteCard(context.Background(), &DeleteCardRequest{CardID: reg.CardID}); err != nil {
				t.Errorf("deleteCard (the test card is still stored): %v", err)
			}
		})
	}
	cardID := reg.CardID
	t.Logf("stored as %s", cardID)

	// The list opens with the reference the OTP was validated with.
	ref := c.NewReferenceNumber()
	otp, err := c.SendOTP(ctx, &SendOTPRequest{ReferenceNumber: ref, ExtraParameters: CardListOTPParams()})
	if err != nil {
		t.Fatalf("sendOTP: %v", err)
	}
	if _, err := c.ValidateOTP(ctx, &ValidateOTPRequest{
		OTP: sandboxOTP, Token: otp.Token, ReferenceNumber: ref, ExtraParameters: CardListOTPParams(),
	}); err != nil {
		t.Fatalf("validateOTP: %v", err)
	}
	list, err := c.GetPaymentMethodList(ctx, &PaymentMethodListRequest{ReferenceNumber: ref})
	checkDecode(t, "getPaymentMethodList", err)
	switch {
	case HasCode(err, "1002"):
		// The shared PAYCELLTEST merchant answers this way, a test channel does not.
		t.Logf("getPaymentMethodList is not open for this merchant: %v", err)
	case err != nil:
		t.Errorf("getPaymentMethodList: %v", err)
	default:
		found := false
		for _, card := range list.Cards() {
			found = found || card.CardID == cardID
		}
		if !found {
			t.Errorf("stored card %s missing from the list: %+v", cardID, list.Cards())
		}
	}

	// By id, no 3D.
	ref = c.NewReferenceNumber()
	if _, err := c.Pay(ctx, PayParams{CardID: cardID, Amount: 100, ReferenceNumber: ref}); err != nil {
		t.Fatalf("pay by card id: %v", err)
	}
	if _, err := c.Reverse(ctx, &ReverseRequest{OriginalReferenceNumber: ref, Amount: 100}); err != nil {
		t.Errorf("reverse: %v", err)
	}

	// By id, with 3D.
	params := PayParams{CardID: cardID, Amount: 100}
	start, err := c.Start3D(ctx, params, sandboxCallback)
	if err != nil {
		t.Fatalf("start3D by card id: %v", err)
	}
	passSandbox3D(t, c, start.ThreeDSessionID)
	if _, err := c.Complete3D(ctx, start, params); err != nil {
		t.Fatalf("3D payment by card id: %v", err)
	}
	if _, err := c.Reverse(ctx, &ReverseRequest{OriginalReferenceNumber: start.ReferenceNumber, Amount: 100}); err != nil {
		t.Errorf("reverse: %v", err)
	}
}

func TestSandboxOTP(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()

	ref := c.NewReferenceNumber()
	otp, err := c.SendOTP(ctx, &SendOTPRequest{Amount: 100, ReferenceNumber: ref})
	checkDecode(t, "sendOTP", err)
	if err != nil {
		t.Fatalf("sendOTP: %v", err)
	}
	if otp.Token == "" {
		t.Fatal("no OTP token returned")
	}

	_, err = c.ValidateOTP(ctx, &ValidateOTPRequest{
		OTP: "000000", Token: otp.Token, ReferenceNumber: ref, Amount: 100,
	})
	if err == nil {
		t.Error("a wrong OTP was accepted")
	}
	checkDecode(t, "validateOTP", err)
	t.Logf("otp token %s, wrong code rejected with %v", otp.Token, err)
}

func TestSandboxReconciliation(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()
	today := time.Now().Format("20060102")

	hist, err := c.GetProvisionHistory(ctx, &ProvisionHistoryRequest{ReconciliationDate: today})
	checkDecode(t, "getProvisionHistory", err)
	if err != nil {
		t.Errorf("getProvisionHistory: %v", err)
	} else {
		t.Logf("provision history for %s: %d entries", today, len(hist.ProvisionList))
	}

	// The totals are wrong on purpose. This is about the shape being
	// accepted, not about the books balancing.
	rec, err := c.SummaryReconciliation(ctx, &ReconciliationRequest{ReconciliationDate: today})
	checkDecode(t, "summaryReconciliation", err)
	if err != nil {
		t.Errorf("summaryReconciliation: %v", err)
	} else {
		t.Logf("reconciliation for %s: %q", today, rec.ReconciliationResult)
	}
}

// Refunds need an amount, 2008 without one. And once one lands, no more
// reversing.
func TestSandboxRefund(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()

	ref := pay(t, c, 500)

	// Leave the amount out and it's rejected, not treated as "all of it".
	if _, err := c.RefundAll(ctx, &RefundRequest{OriginalReferenceNumber: ref}); !HasCode(err, "2008") {
		t.Errorf("a refund without an amount should answer 2008, got %v", err)
	}

	part, err := c.RefundAll(ctx, &RefundRequest{OriginalReferenceNumber: ref, Amount: 200})
	checkDecode(t, "refundAll", err)
	if err != nil {
		t.Fatalf("partial refund: %v", err)
	}
	t.Logf("refunded 2 of 5 TL, approval %s", part.ApprovalCode)

	// They stack up against the same payment.
	if _, err := c.RefundAll(ctx, &RefundRequest{OriginalReferenceNumber: ref, Amount: 200}); err != nil {
		t.Errorf("second partial refund: %v", err)
	}

	// Refunded means no longer reversible.
	if _, err := c.Reverse(ctx, &ReverseRequest{OriginalReferenceNumber: ref}); !HasCode(err, "2027") {
		t.Errorf("reversing a refunded payment should answer 2027, got %v", err)
	}

	inq, err := c.InquireAll(ctx, &InquireRequest{OriginalReferenceNumber: ref})
	if err != nil {
		t.Fatal(err)
	}
	var refunded Amount
	for _, p := range inq.ProvisionList {
		if p.ProvisionType == "REFUND" && p.Applied() {
			refunded += p.Amount
		}
	}
	if refunded != 400 {
		t.Errorf("inquiry reports %d kuruş refunded, want 400", refunded)
	}
	t.Logf("inquiry shows %d kuruş refunded across %d entries", refunded, len(inq.ProvisionList))

	// The last 1 TL, so nothing is left standing.
	if _, err := c.RefundAll(ctx, &RefundRequest{OriginalReferenceNumber: ref, Amount: 100}); err != nil {
		t.Errorf("refunding the rest: %v", err)
	}
}

// Refunds are capped at what was paid, and a rejected one still turns up in
// the inquiry wearing responseCode "0". The approval code is the only thing
// separating the real ones.
func TestSandboxOverRefund(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()

	ref := pay(t, c, 500)

	if _, err := c.RefundAll(ctx, &RefundRequest{OriginalReferenceNumber: ref, Amount: 300}); err != nil {
		t.Fatalf("first refund: %v", err)
	}
	// 300 + 300 against a 500 payment.
	if _, err := c.RefundAll(ctx, &RefundRequest{OriginalReferenceNumber: ref, Amount: 300}); !HasCode(err, "2015") {
		t.Errorf("refunding past the original amount should answer 2015, got %v", err)
	}

	inq, err := c.InquireAll(ctx, &InquireRequest{OriginalReferenceNumber: ref})
	if err != nil {
		t.Fatal(err)
	}
	var applied, naive Amount
	for _, p := range inq.ProvisionList {
		if p.ProvisionType != "REFUND" {
			continue
		}
		naive += p.Amount
		if p.Applied() {
			applied += p.Amount
		}
	}
	if applied != 300 {
		t.Errorf("Applied() sums to %d kuruş, want 300", applied)
	}
	if naive == applied {
		t.Error("the rejected refund did not appear in the inquiry, so this test proves nothing")
	}
	t.Logf("refund lines total %d kuruş, of which %d actually applied", naive, applied)

	if _, err := c.RefundAll(ctx, &RefundRequest{OriginalReferenceNumber: ref, Amount: 200}); err != nil {
		t.Errorf("refunding the rest: %v", err)
	}
}
