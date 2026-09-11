// Mobile payment. Goes on the phone bill instead of a card.
//
//	go run ./examples/mobile-payment
//
// Direct carrier billing has to be open for the subscriber before any of this
// works, and opening it means terms plus an OTP. After that it's provisionAll
// with a DCB method.
package main

import (
	"bufio"
	"context"
	"log"
	"os"
	"strings"

	paycell "github.com/omertheroot/paycell-go"
	"github.com/omertheroot/paycell-go/examples/internal/sandbox"
)

func main() {
	log.SetFlags(0)
	c := sandbox.Client()
	ctx := context.Background()

	sandbox.Step("where does this subscriber stand")
	methods, err := c.GetPaymentMethods(ctx, nil)
	if err != nil && !paycell.IsOTPRequired(err) {
		log.Fatal(err)
	}

	dcb := methods.MobilePayment
	switch {
	case dcb == nil:
		log.Print("   mobile payment is not offered to this subscriber")
		return
	case dcb.IsDcbOpen:
		log.Printf("   already open, %d of %d kuruş left this period", dcb.RemainingLimit, dcb.Limit)
	default:
		log.Printf("   closed, terms at %s", dcb.EulaURL)
		openIt(ctx, c, dcb)
	}

	ref := c.NewReferenceNumber()

	sandbox.Step("confirm the payment with an OTP")
	otp, err := c.SendOTP(ctx, &paycell.SendOTPRequest{Amount: 100, ReferenceNumber: ref})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("   sent, token %s, %d tries left", otp.Token, otp.RemainingRetryCount)

	if _, err := c.ValidateOTP(ctx, &paycell.ValidateOTPRequest{
		OTP:             ask("   code: "),
		Token:           otp.Token,
		Amount:          100,
		ReferenceNumber: ref,
	}); err != nil {
		log.Fatalf("   %v", err) // 3112 is a wrong code
	}

	sandbox.Step("charge it to the bill")
	res, err := c.ProvisionAll(ctx, &paycell.ProvisionAllRequest{
		PaymentMethodType: paycell.MethodDCB,
		ProvisionRequest:  paycell.ProvisionRequest{Amount: 100, ReferenceNumber: ref},
	})
	if err != nil {
		log.Fatalf("   %v", err)
	}
	log.Printf("   order %s", res.OrderID)

	sandbox.Step("give it back")
	// Mobile payments go back through the *All variants.
	if _, err := c.RefundAll(ctx, &paycell.RefundRequest{
		OriginalReferenceNumber: ref,
		Amount:                  100,
		PaymentMethodType:       paycell.MethodDCB,
	}); err != nil {
		log.Printf("   %v", err)
	}
}

func openIt(ctx context.Context, c *paycell.Client, dcb *paycell.MobilePaymentInfo) {
	// Needs a reference number, 2007 without one, and an OTP on top.
	_, err := c.OpenMobilePayment(ctx, &paycell.OpenMobilePaymentRequest{EulaID: dcb.EulaID})
	if paycell.IsOTPRequired(err) {
		log.Print("   opening it needs an OTP the subscriber has to confirm")
		return
	}
	if err != nil {
		log.Fatalf("   could not open it: %v", err)
	}
	log.Print("   opened")
}

func ask(prompt string) string {
	log.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}
