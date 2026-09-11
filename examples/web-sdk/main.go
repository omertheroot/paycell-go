// Hosted checkout. Paycell collects the card, you poll for the outcome.
//
//	PAYCELL_TERMINAL_CODE=... go run ./examples/web-sdk
//
// Nothing about the card comes near you here, not even a token. Open a
// session, send the customer off, watch the status.
//
// The terminal code isn't in the published sandbox credentials, so this one
// wants your own test merchant.
package main

import (
	"context"
	"log"
	"time"

	paycell "github.com/omertheroot/paycell-go"
	"github.com/omertheroot/paycell-go/examples/internal/sandbox"
)

func main() {
	log.SetFlags(0)
	c := sandbox.Client()
	ctx := context.Background()

	if c.Config().TerminalCode == "" {
		log.Fatal("set PAYCELL_TERMINAL_CODE: the Web SDK needs a terminal code")
	}

	sandbox.Step("open a checkout session")
	ref := c.NewReferenceNumber()
	session, err := c.InitSession(ctx, paycell.SDKParams{
		Amount:                 1525, // 15,25 TL
		PaymentReferenceNumber: ref,
		HostAccount:            "customer@example.com", // your own id for the payer
		PaymentSecurity:        paycell.ThreeDSecure,
		ReturnURL:              "https://shop.example/done?ref=[paymentReferenceNumber]",
		PostResultURL:          "https://shop.example/paycell/result",

		// Want a different total on one card brand? Add a line with
		// Count 1 for it.
		InstallmentPlan: []paycell.Installment{
			{LineID: 1, PaymentMethodType: paycell.MethodCreditCard, CardBrand: "BONUS", Count: 3, Amount: 1525},
			{LineID: 2, PaymentMethodType: paycell.MethodCreditCard, CardBrand: "WORLD", Count: 6, Amount: 1600},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("   tracking id %s", session.TrackingID)
	log.Printf("   send the customer to %s", session.TrackingURL)

	sandbox.Step("wait for it to settle")
	// A real service would also take the POST to PostResultURL and stop
	// polling the moment it lands.
	for i := 0; i < 30; i++ {
		status, err := c.QueryPaymentStatus(ctx, ref)
		if err != nil {
			log.Printf("   query failed: %v", err)
		} else if status.Status != paycell.StatusPending {
			log.Printf("   status %s: %s", status.Status, status.StatusExplanation)
			if status.Paid() {
				log.Printf("   %d kuruş, %d instalments, order %s, %s",
					status.Amount, status.InstallmentCount, status.OrderID,
					status.PaymentMethod.PaymentMethodNumber)
			}
			return
		}
		time.Sleep(2 * time.Second)
	}
	log.Print("   still pending after a minute, giving up on the poll")
}
