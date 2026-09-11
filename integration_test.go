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
// Provision services are IP allow-listed, so from an unregistered address
// these just fail.
//
// No real money involved. Sandbox, 1 TL, and everything gets reversed.
package paycell

import (
	"context"
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

var sandboxTestCard = Card{Number: "5200190006338608", ExpireMonth: "01", ExpireYear: "30", CVC: "410"}

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
	if err == nil {
		info := bin.First()
		if info == nil {
			t.Fatal("no bin record for a known range")
		}
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

// The whole money path. Pay 1 TL on a stored card, find it in the inquiry,
// reverse it.
func TestSandboxPayInquireReverse(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()

	cards, err := c.GetCards(ctx, nil)
	checkDecode(t, "getCards", err)
	if err != nil {
		t.Skipf("cannot list stored cards: %v", err)
	}
	if len(cards.Cards()) == 0 {
		t.Skip("the sandbox subscriber has no stored card to pay with")
	}
	card := cards.Cards()[0]

	ref := c.NewReferenceNumber()
	prov, err := c.Provision(ctx, &ProvisionRequest{CardID: card.CardID, Amount: 100, ReferenceNumber: ref})
	checkDecode(t, "provision", err)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if prov.OrderID == "" || prov.ApprovalCode == "" {
		t.Errorf("provision succeeded without an order id or approval code: %+v", prov)
	}
	t.Logf("paid 1 TL, order %s approval %s ref %s", prov.OrderID, prov.ApprovalCode, ref)

	inq, err := c.Inquire(ctx, &InquireRequest{OriginalReferenceNumber: ref})
	checkDecode(t, "inquire", err)
	if err != nil {
		t.Errorf("inquire: %v", err)
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
	rev, err := c.Reverse(ctx, &ReverseRequest{OriginalReferenceNumber: ref})
	checkDecode(t, "reverse", err)
	if err != nil {
		t.Fatalf("reverse (the sandbox payment is still standing): %v", err)
	}
	t.Logf("reversed, approval %s reconciliation %s", rev.ApprovalCode, rev.ReconciliationDate)
}

// A 3D session, as far as you can get without a browser.
func TestSandboxThreeDSession(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()

	cards, err := c.GetCards(ctx, nil)
	if err != nil || len(cards.Cards()) == 0 {
		t.Skipf("no stored card to open a session with: %v", err)
	}

	sess, err := c.GetThreeDSession(ctx, &ThreeDSessionRequest{
		CardID: cards.Cards()[0].CardID, Amount: 100,
	})
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
	// Nobody went to the bank page, so this had better not read as verified.
	if result.Verified() {
		t.Error("an untouched session must not report as verified")
	}
	t.Logf("session %s step %q verified=%v", sess.ThreeDSessionID, result.CurrentStep, result.Verified())
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

	// No way to read the code the sandbox sent, so test the rejection
	// instead.
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

	cards, err := c.GetCards(ctx, nil)
	if err != nil || len(cards.Cards()) == 0 {
		t.Skipf("no stored card to pay with: %v", err)
	}

	ref := c.NewReferenceNumber()
	if _, err := c.Provision(ctx, &ProvisionRequest{
		CardID: cards.Cards()[0].CardID, Amount: 500, ReferenceNumber: ref,
	}); err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Leave the amount out and it's rejected, not treated as "all of it".
	if _, err := c.Refund(ctx, &RefundRequest{OriginalReferenceNumber: ref}); !HasCode(err, "2008") {
		t.Errorf("a refund without an amount should answer 2008, got %v", err)
	}

	part, err := c.Refund(ctx, &RefundRequest{OriginalReferenceNumber: ref, Amount: 200})
	checkDecode(t, "refund", err)
	if err != nil {
		t.Fatalf("partial refund: %v", err)
	}
	t.Logf("refunded 2 of 5 TL, approval %s", part.ApprovalCode)

	// They stack up against the same payment.
	if _, err := c.Refund(ctx, &RefundRequest{OriginalReferenceNumber: ref, Amount: 200}); err != nil {
		t.Errorf("second partial refund: %v", err)
	}

	// Refunded means no longer reversible.
	if _, err := c.Reverse(ctx, &ReverseRequest{OriginalReferenceNumber: ref}); !HasCode(err, "2027") {
		t.Errorf("reversing a refunded payment should answer 2027, got %v", err)
	}

	inq, err := c.Inquire(ctx, &InquireRequest{OriginalReferenceNumber: ref})
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
}

// Refunds are capped at what was paid, and a rejected one still turns up in
// the inquiry wearing responseCode "0". The approval code is the only thing
// separating the real ones.
func TestSandboxOverRefund(t *testing.T) {
	c := sandbox(t)
	ctx := context.Background()

	cards, err := c.GetCards(ctx, nil)
	if err != nil || len(cards.Cards()) == 0 {
		t.Skipf("no stored card to pay with: %v", err)
	}

	ref := c.NewReferenceNumber()
	if _, err := c.Provision(ctx, &ProvisionRequest{
		CardID: cards.Cards()[0].CardID, Amount: 500, ReferenceNumber: ref,
	}); err != nil {
		t.Fatalf("provision: %v", err)
	}

	if _, err := c.Refund(ctx, &RefundRequest{OriginalReferenceNumber: ref, Amount: 300}); err != nil {
		t.Fatalf("first refund: %v", err)
	}
	// 300 + 300 against a 500 payment.
	if _, err := c.Refund(ctx, &RefundRequest{OriginalReferenceNumber: ref, Amount: 300}); !HasCode(err, "2015") {
		t.Errorf("refunding past the original amount should answer 2015, got %v", err)
	}

	inq, err := c.Inquire(ctx, &InquireRequest{OriginalReferenceNumber: ref})
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
}
