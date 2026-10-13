// Package sandbox is the boilerplate every example would otherwise repeat:
// a client pointed at the Paycell sandbox and a card that works there.
//
// Defaults are the credentials Paycell publishes for integration work, so the
// examples run with nothing set up. Environment variables override them.
package sandbox

import (
	"html"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	paycell "github.com/omertheroot/paycell-go"
)

// TestCard is Paycell's mock bank card, the only one the sandbox actually
// charges. 3D password is 123456. It expires 12/26.
var TestCard = paycell.Card{
	Number:      "1000000000000016",
	ExpireMonth: "12",
	ExpireYear:  "26",
	CVC:         "000",
}

// OTP is the code the sandbox accepts for Paycell's own OTPs.
const OTP = "111111"

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

// Pass3D takes a session through the sandbox's simulated bank page the way
// the customer's browser would. Sandbox only: in production it is the
// customer who does this, see examples/3d-secure.
//
// The page checks the code in JavaScript and then posts action=success, so
// there is nothing to type.
func Pass3D(c *paycell.Client, sessionID string) {
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	post := func(u string, form url.Values) string {
		resp, err := hc.PostForm(u, form)
		if err != nil {
			log.Fatalf("3D page: %v", err)
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

	// localhost is the callback host the sandbox has registered.
	post(c.ThreeDSecureURL(), url.Values{
		"threeDSessionId": {sessionID},
		"callbackurl":     {"http://localhost/callback"},
		"isPost3DResult":  {"true"},
	})
	result := post(strings.TrimSuffix(c.ThreeDSecureURL(), "threeDSecure")+"mockBankResult", url.Values{
		"threeDSessionId": {sessionID},
		"action":          {"success"},
	})
	post(between(result, `action="`), url.Values{"mdStatus": {between(result, `name="mdStatus" value="`)}})
}
