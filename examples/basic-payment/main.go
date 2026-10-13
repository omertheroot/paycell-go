// Take a payment, look it up, give it back.
//
//	go run ./examples/basic-payment
//
// The whole non-3D flow. Card becomes a token, token becomes a payment, and
// the reference number is what ties the calls together.
package main

import (
	"context"
	"errors"
	"log"

	paycell "github.com/omertheroot/paycell-go"
	"github.com/omertheroot/paycell-go/examples/internal/sandbox"
)

func main() {
	log.SetFlags(0)
	c := sandbox.Client()
	ctx := context.Background()

	// File this with the order. Everything afterwards needs it.
	ref := c.NewReferenceNumber()

	sandbox.Step("pay 1 TL")
	res, err := c.Pay(ctx, paycell.PayParams{Card: sandbox.TestCard, Amount: 100, ReferenceNumber: ref})

	var pErr *paycell.Error
	switch {
	case errors.As(err, &pErr):
		log.Fatalf("   declined: %s (%s)", pErr.Description, pErr.Code)
	case err != nil:
		log.Fatalf("   call failed: %v", err)
	}
	log.Printf("   order %s, approval %s, reference %s", res.OrderID, res.ApprovalCode, ref)

	sandbox.Step("inquire")
	// Always worth doing after a timeout. The payment can be through even
	// when the reply got lost.
	inq, err := c.InquireAll(ctx, &paycell.InquireRequest{OriginalReferenceNumber: ref})
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range inq.ProvisionList {
		log.Printf("   %-8s %4d kuruş  applied=%v  %s", p.ProvisionType, p.Amount, p.Applied(), p.ResponseDescription)
	}

	sandbox.Step("reverse")
	rev, err := c.Reverse(ctx, &paycell.ReverseRequest{OriginalReferenceNumber: ref, Amount: 100})
	if paycell.IsAlreadySettled(err) {
		log.Print("   too late to reverse, refunding the full amount")
		rev, err = c.RefundAll(ctx, &paycell.RefundRequest{OriginalReferenceNumber: ref, Amount: 100})
	}
	if err != nil {
		log.Fatalf("   the payment is still standing: %v", err)
	}
	log.Printf("   approval %s, reconciliation %s", rev.ApprovalCode, rev.ReconciliationDate)
}
