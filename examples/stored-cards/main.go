// The Paycell wallet. Store a card once, charge it by id after that.
//
//	go run ./examples/stored-cards
//
// The listing is OTP gated, so the first call comes back 3110 and you have to
// walk the subscriber through a one time password before it opens up.
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

	sandbox.Step("fetch the wallet terms")
	terms, err := c.GetTermsOfServiceContent(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("   eulaId %s, %d bytes of Turkish terms", terms.ID(), len(terms.TermsOfServiceHTMLContentTR))

	sandbox.Step("store a card")
	// Single use, so this needs a fresh one even if the same card was just
	// charged.
	token, err := c.GetCardTokenSecure(ctx, sandbox.TestCard)
	if err != nil {
		log.Fatal(err)
	}
	reg, err := c.RegisterCard(ctx, &paycell.RegisterCardRequest{
		CardToken: token.CardToken,
		Alias:     "İş kartı",
		IsDefault: true,
		EulaID:    terms.ID(),
	})
	if err != nil {
		// 4003 means the sandbox merchant won't store this card for this
		// subscriber. The listing below still works.
		log.Printf("   could not store it: %v", err)
	} else {
		log.Printf("   stored as %s", reg.CardID)
	}

	sandbox.Step("list the stored cards")
	methods, err := c.GetPaymentMethods(ctx, nil)
	if paycell.IsOTPRequired(err) {
		methods, err = unlockWithOTP(ctx, c)
	}
	if err != nil {
		log.Fatal(err)
	}

	cards := methods.Cards()
	for _, card := range cards {
		log.Printf("   %s  %-22s %-8s default=%v expired=%v",
			card.MaskedCardNo, card.Alias, card.CardBrand, card.IsDefault, card.IsExpired)
	}
	if m := methods.MobilePayment; m != nil {
		log.Printf("   mobile payment: open=%v remaining=%d kuruş", m.IsDcbOpen, m.RemainingLimit)
	}
	if len(cards) == 0 {
		return
	}

	sandbox.Step("pay with a stored card, by id")
	ref := c.NewReferenceNumber()
	res, err := c.Pay(ctx, paycell.PayParams{CardID: cards[0].CardID, Amount: 100, ReferenceNumber: ref})
	if err != nil {
		log.Fatalf("   %v", err)
	}
	log.Printf("   order %s, approval %s", res.OrderID, res.ApprovalCode)
	if _, err := c.Reverse(ctx, &paycell.ReverseRequest{OriginalReferenceNumber: ref}); err != nil {
		log.Printf("   reverse failed: %v", err)
	}

	sandbox.Step("rename it, then delete the one we stored")
	if reg != nil && reg.CardID != "" {
		if _, err := c.UpdateCard(ctx, &paycell.UpdateCardRequest{
			CardID: reg.CardID, Alias: "Yeni ad", EulaID: terms.ID(),
		}); err != nil {
			log.Printf("   rename failed: %v", err)
		}
		if _, err := c.DeleteCard(ctx, &paycell.DeleteCardRequest{CardID: reg.CardID}); err != nil {
			log.Printf("   delete failed: %v", err)
		} else {
			log.Print("   deleted")
		}
	} else {
		log.Print("   nothing of ours to clean up")
	}
}

// unlockWithOTP walks the OTP that gates the card list. Both calls carry
// VALIDATION_TYPE=PM_LIST, which is what says this OTP is for opening the
// list rather than authorising a payment.
func unlockWithOTP(ctx context.Context, c *paycell.Client) (*paycell.PaymentMethodsResponse, error) {
	log.Print("   the list is OTP gated (3110)")

	ref := c.NewReferenceNumber()
	otp, err := c.SendOTP(ctx, &paycell.SendOTPRequest{
		ReferenceNumber: ref,
		ExtraParameters: paycell.CardListOTPParams(),
	})
	if err != nil {
		return nil, err
	}
	log.Printf("   OTP sent, token %s", otp.Token)

	code := ask("   enter the code the subscriber received: ")
	if _, err := c.ValidateOTP(ctx, &paycell.ValidateOTPRequest{
		OTP:             code,
		Token:           otp.Token,
		ReferenceNumber: ref,
		ExtraParameters: paycell.CardListOTPParams(),
	}); err != nil {
		return nil, err
	}

	return c.GetPaymentMethods(ctx, &paycell.PaymentMethodsRequest{
		ReferenceNumber: ref,
		OTPToken:        otp.Token,
	})
}

func ask(prompt string) string {
	log.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}
