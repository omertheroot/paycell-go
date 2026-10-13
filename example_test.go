package paycell_test

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	paycell "github.com/omertheroot/paycell-go"
)

func client() *paycell.Client {
	c, err := paycell.New(paycell.Config{
		ApplicationName:     "PAYCELLTEST",
		ApplicationPassword: "PaycellTestPassword",
		SecureCode:          "0000000000000000",
		MerchantCode:        "9998",
		DefaultMSISDN:       "905380521479",
		ReferencePrefix:     "100",
		Environment:         paycell.Test,
	})
	if err != nil {
		log.Fatal(err)
	}
	return c
}

// A plain card payment. Tokenised and charged in one call.
func Example_payment() {
	c := client()
	ctx := context.Background()

	res, err := c.Pay(ctx, paycell.PayParams{
		Card: paycell.Card{
			Number:      "5200190006338608",
			ExpireMonth: "01",
			ExpireYear:  "30",
			CVC:         "410",
		},
		Amount:          1525, // 15,25 TL
		ReferenceNumber: c.NewReferenceNumber(),
	})

	var pErr *paycell.Error
	switch {
	case errors.As(err, &pErr):
		log.Printf("declined: %s (%s)", pErr.Description, pErr.Code)
	case err != nil:
		log.Printf("call failed: %v", err)
	default:
		log.Printf("paid, order %s approval %s", res.OrderID, res.ApprovalCode)
	}
}

// A 3D payment, in the two halves the trip to the bank splits it into.
func Example_threeDSecure() {
	c := client()

	params := paycell.PayParams{
		Card:   paycell.Card{Number: "5200190006338608", ExpireMonth: "01", ExpireYear: "30", CVC: "410"},
		Amount: 1525,
	}

	// 1. Before the redirect. Open a session, serve the form.
	http.HandleFunc("/pay", func(w http.ResponseWriter, r *http.Request) {
		start, err := c.Start3D(r.Context(), params, "https://shop.example/paycell/callback")
		if err != nil {
			http.Error(w, "payment could not be started", http.StatusBadGateway)
			return
		}
		// Keep this against the order. The callback proves nothing.
		saveSession(start)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(start.Form.HTML()))
	})

	// 2. After the redirect. Confirm, then charge.
	http.HandleFunc("/paycell/callback", func(w http.ResponseWriter, r *http.Request) {
		start := loadSession(r)

		res, err := c.Complete3D(r.Context(), start, params)
		var notVerified *paycell.ErrNotVerified
		switch {
		case errors.As(err, &notVerified):
			http.Error(w, "3D verification failed", http.StatusPaymentRequired)
		case err != nil:
			http.Error(w, "payment failed", http.StatusBadGateway)
		default:
			log.Printf("paid, order %s", res.OrderID)
		}
	})
}

// Storing a card in the wallet, then listing what's in there. The listing
// sits behind an OTP.
func Example_storedCard() {
	c := client()
	ctx := context.Background()

	// 1. Store a card. Paycell only stores one the customer has passed 3D
	//    with, so open a session for it. With nothing to buy, 1 kuruş does
	//    and nothing gets charged.
	terms, err := c.GetTermsOfServiceContent(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	token, err := c.GetCardTokenSecure(ctx, paycell.Card{
		Number: "5200190006338608", ExpireMonth: "01", ExpireYear: "30", CVC: "410",
	})
	if err != nil {
		log.Fatal(err)
	}
	session, err := c.GetThreeDSession(ctx, &paycell.ThreeDSessionRequest{CardToken: token.CardToken, Amount: 1})
	if err != nil {
		log.Fatal(err)
	}

	// ... the customer goes through c.ThreeDSecureForm and comes back ...

	result, err := c.GetThreeDSessionResult(ctx, &paycell.ThreeDSessionResultRequest{ThreeDSessionID: session.ThreeDSessionID})
	if err != nil || !result.Verified() {
		log.Fatal("3D not passed, nothing to store")
	}
	_, err = c.RegisterCard(ctx, &paycell.RegisterCardRequest{
		CardToken:       token.CardToken,
		ThreeDSessionID: session.ThreeDSessionID,
		Alias:           "İş kartı",
		IsDefault:       true,
		EulaID:          terms.ID(),
	})
	if err != nil && !paycell.HasCode(err, paycell.CodeCardAlreadyRegistered) {
		log.Fatal(err)
	}

	// 2. List them. The list opens with the reference an OTP was validated
	//    with, so send one first.
	ref := c.NewReferenceNumber()
	otp, err := c.SendOTP(ctx, &paycell.SendOTPRequest{
		ReferenceNumber: ref,
		ExtraParameters: paycell.CardListOTPParams(),
	})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := c.ValidateOTP(ctx, &paycell.ValidateOTPRequest{
		OTP:             "111111", // what the subscriber typed
		Token:           otp.Token,
		ReferenceNumber: ref,
		ExtraParameters: paycell.CardListOTPParams(),
	}); err != nil {
		log.Fatal(err)
	}
	methods, err := c.GetPaymentMethodList(ctx, &paycell.PaymentMethodListRequest{ReferenceNumber: ref})
	if err != nil {
		log.Fatal(err)
	}

	// 3. Pay with one by id. No card data, no token.
	for _, card := range methods.Cards() {
		log.Printf("%s %s", card.MaskedCardNo, card.Alias)
		if card.IsDefault && !card.IsExpired {
			if _, err := c.Pay(ctx, paycell.PayParams{CardID: card.CardID, Amount: 1525}); err != nil {
				log.Print(err)
			}
		}
	}
}

// Storing the card the customer has just paid with. The token and 3D session
// from the payment are enough, no second verification.
func Example_saveCardAfterPaying() {
	c := client()
	ctx := context.Background()

	params := paycell.PayParams{
		Card:   paycell.Card{Number: "5200190006338608", ExpireMonth: "01", ExpireYear: "30", CVC: "410"},
		Amount: 1525,
	}
	start, err := c.Start3D(ctx, params, "https://shop.example/paycell/callback")
	if err != nil {
		log.Fatal(err)
	}

	// ... the customer passes 3D and comes back ...

	if _, err := c.Complete3D(ctx, start, params); err != nil {
		log.Fatal(err)
	}
	card, err := c.RegisterCard(ctx, &paycell.RegisterCardRequest{
		CardToken:       start.CardToken,
		ThreeDSessionID: start.ThreeDSessionID,
		Alias:           "İş kartı",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("stored as %s", card.CardID)
}

// Mobile payment. Goes on the phone bill, after an OTP.
func Example_mobilePayment() {
	c := client()
	ctx := context.Background()

	methods, err := c.GetPaymentMethods(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	if methods.MobilePayment == nil || !methods.MobilePayment.IsDcbOpen {
		if _, err := c.OpenMobilePayment(ctx, &paycell.OpenMobilePaymentRequest{
			EulaID: methods.MobilePayment.EulaID,
		}); err != nil {
			log.Fatal(err)
		}
	}

	ref := c.NewReferenceNumber()
	otp, err := c.SendOTP(ctx, &paycell.SendOTPRequest{Amount: 1525, ReferenceNumber: ref})
	if err != nil {
		log.Fatal(err)
	}

	if _, err := c.ValidateOTP(ctx, &paycell.ValidateOTPRequest{
		OTP:             "123456", // what the customer typed
		Token:           otp.Token,
		Amount:          1525,
		ReferenceNumber: ref,
	}); err != nil {
		log.Fatal(err)
	}

	res, err := c.ProvisionAll(ctx, &paycell.ProvisionAllRequest{
		PaymentMethodType: paycell.MethodDCB,
		ProvisionRequest: paycell.ProvisionRequest{
			Amount:          1525,
			ReferenceNumber: ref,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Print(res.OrderID)
}

// Hosted checkout. Paycell collects the card, you poll for the outcome.
func Example_webSDK() {
	c, err := paycell.New(paycell.Config{
		ApplicationName:     "PAYCELLTEST",
		ApplicationPassword: "PaycellTestPassword",
		SecureCode:          "0000000000000000",
		MerchantCode:        "9998",
		TerminalCode:        "12345",
		Environment:         paycell.Test,
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	ref := c.NewReferenceNumber()
	session, err := c.InitSession(ctx, paycell.SDKParams{
		Amount:                 1525,
		PaymentReferenceNumber: ref,
		HostAccount:            "customer@example.com",
		PaymentSecurity:        paycell.ThreeDSecure,
		ReturnURL:              "https://shop.example/done?ref=[paymentReferenceNumber]",
		PostResultURL:          "https://shop.example/paycell/result",
		InstallmentPlan: []paycell.Installment{
			{LineID: 1, PaymentMethodType: paycell.MethodCreditCard, CardBrand: "BONUS", Count: 3, Amount: 1525},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("send the customer to %s", session.TrackingURL)

	for i := 0; i < 60; i++ {
		status, err := c.QueryPaymentStatus(ctx, ref)
		if err != nil {
			log.Print(err)
		} else if status.Status != paycell.StatusPending {
			log.Printf("status %s: %s", status.Status, status.StatusExplanation)
			break
		}
		time.Sleep(2 * time.Second)
	}
}

// Reverse on the day, refund after.
func Example_refund() {
	c := client()
	ctx := context.Background()
	const paid = "10000000000000000001"

	_, err := c.Reverse(ctx, &paycell.ReverseRequest{OriginalReferenceNumber: paid})

	var pErr *paycell.Error
	if errors.As(err, &pErr) && pErr.Code == "2029" {
		// Settled already, so refund is the only way back.
		_, err = c.Refund(ctx, &paycell.RefundRequest{OriginalReferenceNumber: paid})
	}
	if err != nil {
		log.Fatal(err)
	}
}

func saveSession(*paycell.ThreeDStart)               {}
func loadSession(*http.Request) *paycell.ThreeDStart { return nil }
