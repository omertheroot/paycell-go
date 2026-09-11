// Reverse or refund, and how to count what actually went back.
//
//	go run ./examples/refund
//
// Before day end a reverse voids the provision outright and the customer may
// never see the charge at all. After it, money has moved and a refund sends
// it back the other way. Which one you get isn't your decision: try the
// reverse and read the code.
package main

import (
	"context"
	"log"

	paycell "github.com/omertheroot/paycell-go"
	"github.com/omertheroot/paycell-go/examples/internal/sandbox"
)

func main() {
	log.SetFlags(0)
	c := sandbox.Client()
	ctx := context.Background()

	sandbox.Step("pay 5 TL")
	ref := c.NewReferenceNumber()
	if _, err := c.Pay(ctx, sandbox.PayParams(ctx, c, 500, ref)); err != nil {
		log.Fatal(err)
	}
	log.Printf("   reference %s", ref)

	sandbox.Step("refund 2 TL of it")
	// Required. Leave it out and you get 2008, not a full refund.
	if _, err := c.Refund(ctx, &paycell.RefundRequest{
		OriginalReferenceNumber: ref, Amount: 200,
	}); err != nil {
		log.Fatal(err)
	}
	log.Print("   done")

	sandbox.Step("try to refund 4 TL more")
	// They accumulate, and the total is capped at what was paid.
	_, err := c.Refund(ctx, &paycell.RefundRequest{OriginalReferenceNumber: ref, Amount: 400})
	log.Printf("   %v", err)
	if paycell.HasCode(err, "2015") {
		log.Print("   2015: the total would exceed what was paid")
	}

	sandbox.Step("try to reverse a refunded payment")
	_, err = c.Reverse(ctx, &paycell.ReverseRequest{OriginalReferenceNumber: ref})
	log.Printf("   %v", err)

	sandbox.Step("count what actually came back")
	inq, err := c.Inquire(ctx, &paycell.InquireRequest{OriginalReferenceNumber: ref})
	if err != nil {
		log.Fatal(err)
	}
	var naive, applied paycell.Amount
	for _, p := range inq.ProvisionList {
		log.Printf("   %-8s %4d kuruş  approval=%-7q applied=%v",
			p.ProvisionType, p.Amount, p.ApprovalCode, p.Applied())
		if p.ProvisionType == "REFUND" {
			naive += p.Amount
			if p.Applied() {
				applied += p.Amount
			}
		}
	}
	// The rejected one is sitting in that list with responseCode "0" like
	// everything else. Only the approval code knows the difference.
	log.Printf("\n   refund lines add up to %d kuruş", naive)
	log.Printf("   actually refunded:     %d kuruş", applied)
	log.Printf("   still refundable:      %d kuruş", 500-applied)
}
