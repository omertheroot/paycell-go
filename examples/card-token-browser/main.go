// Tokenising in the browser so the card number never reaches your server.
//
//	go run ./examples/card-token-browser    then open http://localhost:8081
//
// The flow Paycell would rather you used. Server signs a request, browser
// posts the card straight to Paycell, only the token comes back to us. A
// number your process never sees can't leak out of it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	paycell "github.com/omertheroot/paycell-go"
	"github.com/omertheroot/paycell-go/examples/internal/sandbox"
)

func main() {
	log.SetFlags(0)
	addr := flag.String("addr", "localhost:8081", "listen address")
	flag.Parse()

	c := sandbox.Client()

	// Browser asks for a signature. The hash and the transaction id go out,
	// the secure code and the password stay put.
	http.HandleFunc("/sign", func(w http.ResponseWriter, r *http.Request) {
		txID := paycell.NewTransactionID()
		dt := paycell.TransactionDateTime(time.Now())

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"url":                 c.CardTokenURL(),
			"applicationName":     c.Config().ApplicationName,
			"transactionId":       txID,
			"transactionDateTime": dt,
			"hashData":            c.RequestHash(txID, dt),
		})
	})

	// Back comes whatever Paycell answered. Check the signature before
	// believing a word of it, or a client can just make up a token.
	http.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TransactionID string                    `json:"transactionId"`
			Response      paycell.CardTokenResponse `json:"response"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		if err := c.VerifyCardTokenResponse(body.TransactionID, &body.Response); err != nil {
			log.Printf("rejected a token: %v", err)
			http.Error(w, "signature mismatch", http.StatusBadRequest)
			return
		}

		log.Printf("verified token %s", body.Response.CardToken)
		// From here it's the same as basic-payment.
		fmt.Fprintf(w, "verified token %s", body.Response.CardToken)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, form)
	})

	log.Printf("open http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

const form = `<!doctype html><meta charset="utf-8">
<body style="font:16px/1.6 system-ui;max-width:32rem;margin:4rem auto;padding:0 1rem">
<h1>Card token</h1>
<p>The card goes straight from this page to Paycell. Watch the network tab:
the number never touches the Go server.</p>
<input id="no" value="5200190006338608" size="24"> number<br>
<input id="mm" value="01" size="3"> month
<input id="yy" value="30" size="3"> year
<input id="cvc" value="410" size="4"> cvc<br><br>
<button onclick="go()">Tokenise</button>
<pre id="out"></pre>
<script>
async function go() {
  const out = document.getElementById('out')
  const sig = await (await fetch('/sign')).json()

  // Straight to Paycell, not through our own server.
  const res = await (await fetch(sig.url, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({
      header: {
        applicationName: sig.applicationName,
        transactionId: sig.transactionId,
        transactionDateTime: sig.transactionDateTime,
      },
      creditCardNo: document.getElementById('no').value,
      expireDateMonth: document.getElementById('mm').value,
      expireDateYear: document.getElementById('yy').value,
      cvcNo: document.getElementById('cvc').value,
      hashData: sig.hashData,
    }),
  })).json()
  out.textContent = JSON.stringify(res, null, 2)

  // Only the token and its signature go back to us.
  const verdict = await fetch('/token', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({transactionId: sig.transactionId, response: res}),
  })
  out.textContent += '\n\nserver: ' + await verdict.text()
}
</script>
</body>`
