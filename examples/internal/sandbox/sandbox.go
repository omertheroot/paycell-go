// Package sandbox is the boilerplate every example would otherwise repeat:
// a client pointed at the Paycell sandbox and a card that works there.
//
// Defaults are the credentials Paycell publishes for integration work, so the
// examples run with nothing set up. Environment variables override them.
package sandbox

import (
	"context"
	"log"
	"os"
	"time"

	paycell "github.com/omertheroot/paycell-go"
)

// TestCard works in the sandbox. 3D password is 123456.
var TestCard = paycell.Card{
	Number:      "5200190006338608",
	ExpireMonth: "01",
	ExpireYear:  "30",
	CVC:         "410",
}

// Client builds a sandbox client, or dies saying why.
func Client() *paycell.Client {
	c, err := paycell.New(paycell.Config{
		ApplicationName:     env("PAYCELL_APP_NAME", "PAYCELLTEST"),
		ApplicationPassword: env("PAYCELL_APP_PASSWORD", "PaycellTestPassword"),
		SecureCode:          env("PAYCELL_SECURE_CODE", "PAYCELL12345"),
		MerchantCode:        env("PAYCELL_MERCHANT_CODE", "9998"),
		TerminalCode:        os.Getenv("PAYCELL_TERMINAL_CODE"), // Web SDK only
		EulaID:              env("PAYCELL_EULA_ID", "17"),
		ReferencePrefix:     env("PAYCELL_PREFIX", "666"),
		DefaultMSISDN:       env("PAYCELL_MSISDN", "905380521479"),
		Environment:         paycell.Test,
		Timeout:             30 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	return c
}

// Step numbers the output so the examples read as a trace.
var step int

func Step(what string) {
	step++
	log.Printf("\n%d. %s", step, what)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// PayParams returns something the shared test merchant will actually accept.
//
// In your own code this is just Card: TestCard and you can ignore all of
// this. The shared sandbox merchant won't settle a card that isn't already on
// file for the subscriber (4003), so we fall back to a stored one.
func PayParams(ctx context.Context, c *paycell.Client, amount paycell.Amount, ref string) paycell.PayParams {
	params := paycell.PayParams{Card: TestCard, Amount: amount, ReferenceNumber: ref}

	token, err := c.GetCardTokenSecure(ctx, TestCard)
	if err != nil {
		log.Fatalf("could not tokenise the test card: %v", err)
	}
	params.Card = paycell.Card{}
	params.CardToken = token.CardToken

	// No dry-run endpoint, so find out by looking at what it holds for this
	// subscriber.
	cards, err := c.GetCards(ctx, nil)
	if err != nil || len(cards.Cards()) == 0 {
		return params
	}
	if !restrictsNewCards(ctx, c) {
		return params
	}

	log.Printf("   (this merchant only settles cards on file, using %s)", cards.Cards()[0].MaskedCardNo)
	return paycell.PayParams{CardID: cards.Cards()[0].CardID, Amount: amount, ReferenceNumber: ref}
}

// restrictsNewCards tries a 1 kuruş provision and watches for 4003.
func restrictsNewCards(ctx context.Context, c *paycell.Client) bool {
	token, err := c.GetCardTokenSecure(ctx, TestCard)
	if err != nil {
		return false
	}
	_, err = c.Provision(ctx, &paycell.ProvisionRequest{
		CardToken: token.CardToken, Amount: 1, ReferenceNumber: c.NewReferenceNumber(),
	})
	return paycell.HasCode(err, "4003")
}
