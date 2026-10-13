// The Paycell wallet. Store a card once, charge it by id after that.
//
//	go run ./examples/stored-cards
//
// Storing needs a 3D session the customer has passed, and the listing sits
// behind a one time password tied to a reference number.
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
	// Nothing is being bought, so a 1 kuruş session and no provision.
	// Nothing gets charged.
	token, err := c.GetCardTokenSecure(ctx, sandbox.TestCard)
	if err != nil {
		log.Fatal(err)
	}
	sess, err := c.GetThreeDSession(ctx, &paycell.ThreeDSessionRequest{CardToken: token.CardToken, Amount: 1})
	if err != nil {
		log.Fatal(err)
	}
	sandbox.Pass3D(c, sess.ThreeDSessionID)

	result, err := c.GetThreeDSessionResult(ctx, &paycell.ThreeDSessionResultRequest{ThreeDSessionID: sess.ThreeDSessionID})
	if err != nil || !result.Verified() {
		log.Fatalf("   3D not passed, nothing to store: %v", err)
	}
	reg, err := c.RegisterCard(ctx, &paycell.RegisterCardRequest{
		CardToken:       token.CardToken,
		ThreeDSessionID: sess.ThreeDSessionID,
		Alias:           "İş kartı",
		EulaID:          terms.ID(),
	})
	ours := err == nil
	switch {
	case paycell.HasCode(err, paycell.CodeCardAlreadyRegistered):
		log.Printf("   already in the wallet as %s", reg.CardID)
	case err != nil:
		log.Fatalf("   could not store it: %v", err)
	default:
		log.Printf("   stored as %s", reg.CardID)
	}

	sandbox.Step("list the stored cards")
	methods, err := listWithOTP(ctx, c)
	switch {
	case paycell.HasCode(err, "1002"):
		log.Print("   the shared PAYCELLTEST merchant has no getPaymentMethodList (1002), a test channel does")
	case err != nil:
		log.Fatal(err)
	default:
		for _, card := range methods.Cards() {
			log.Printf("   %s  %-22s %-8s default=%v expired=%v",
				card.MaskedCardNo, card.Alias, card.CardBrand, card.IsDefault, card.IsExpired)
		}
	}

	sandbox.Step("pay with the stored card, by id")
	ref := c.NewReferenceNumber()
	res, err := c.Pay(ctx, paycell.PayParams{CardID: reg.CardID, Amount: 100, ReferenceNumber: ref})
	if err != nil {
		log.Fatalf("   %v", err)
	}
	log.Printf("   order %s, approval %s", res.OrderID, res.ApprovalCode)
	if _, err := c.Reverse(ctx, &paycell.ReverseRequest{OriginalReferenceNumber: ref, Amount: 100}); err != nil {
		log.Printf("   reverse failed: %v", err)
	}

	sandbox.Step("rename it, then delete it")
	if !ours {
		log.Print("   not ours, leaving it")
		return
	}
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
}

// listWithOTP walks the OTP that gates the card list. The reference number is
// what ties the three calls together, and VALIDATION_TYPE=PM_LIST says this
// OTP opens the list rather than authorising a payment.
func listWithOTP(ctx context.Context, c *paycell.Client) (*paycell.PaymentMethodsResponse, error) {
	ref := c.NewReferenceNumber()
	otp, err := c.SendOTP(ctx, &paycell.SendOTPRequest{
		ReferenceNumber: ref,
		ExtraParameters: paycell.CardListOTPParams(),
	})
	if err != nil {
		return nil, err
	}
	log.Printf("   OTP sent, token %s", otp.Token)

	code := ask("   enter the code the subscriber received (sandbox: " + sandbox.OTP + "): ")
	if _, err := c.ValidateOTP(ctx, &paycell.ValidateOTPRequest{
		OTP:             code,
		Token:           otp.Token,
		ReferenceNumber: ref,
		ExtraParameters: paycell.CardListOTPParams(),
	}); err != nil {
		return nil, err
	}

	return c.GetPaymentMethodList(ctx, &paycell.PaymentMethodListRequest{ReferenceNumber: ref})
}

func ask(prompt string) string {
	log.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}
