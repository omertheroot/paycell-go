// The 3D Secure flow. Needs a browser, because the customer's own browser is
// what has to POST to the bank.
//
//	go run ./examples/3d-secure     then open http://localhost:8080
//
// That splits the payment in two around the redirect. Sandbox 3D password is
// 123456.
package main

import (
	"errors"
	"flag"
	"fmt"
	"html"
	"log"
	"net/http"
	"sync"

	paycell "github.com/omertheroot/paycell-go"
	"github.com/omertheroot/paycell-go/examples/internal/sandbox"
)

const amount = paycell.Amount(100)

func main() {
	log.SetFlags(0)
	addr := flag.String("addr", "localhost:8080", "listen address")
	flag.Parse()

	c := sandbox.Client()
	base := "http://" + *addr

	// Nothing in the callback can be trusted, so what we started has to be
	// remembered here. A real service would put this in a database.
	var mu sync.Mutex
	pending := map[string]*paycell.ThreeDStart{}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		page(w, fmt.Sprintf(`<h1>3D Secure</h1>
			<p>Card %s, %.2f TL. The sandbox 3D password is 123456.</p>
			<form method="post" action="/pay"><button>Pay</button></form>`,
			sandbox.TestCard.Number, amount.TRY()))
	})

	// Before the redirect. Open a session, keep it, serve the form.
	http.HandleFunc("/pay", func(w http.ResponseWriter, r *http.Request) {
		ref := c.NewReferenceNumber()
		params := paycell.PayParams{Card: sandbox.TestCard, Amount: amount, ReferenceNumber: ref}

		start, err := c.Start3D(r.Context(), params, base+"/callback?ref="+ref)
		if err != nil {
			page(w, "could not start: "+html.EscapeString(err.Error()))
			return
		}
		mu.Lock()
		pending[ref] = start
		mu.Unlock()

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, start.Form.HTML()) // submits itself
	})

	// After the redirect. Confirm the verification, then charge.
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		ref := r.FormValue("ref")

		mu.Lock()
		start := pending[ref]
		delete(pending, ref)
		mu.Unlock()
		if start == nil {
			page(w, "unknown payment "+html.EscapeString(ref))
			return
		}

		res, err := c.Complete3D(r.Context(), start, paycell.PayParams{Amount: amount})

		var notVerified *paycell.ErrNotVerified
		switch {
		case errors.As(err, &notVerified):
			page(w, "not verified: "+html.EscapeString(notVerified.Error()))
		case err != nil:
			page(w, "payment failed: "+html.EscapeString(err.Error()))
		default:
			page(w, fmt.Sprintf(`<h1>Paid</h1><p>order %s, approval %s</p>
				<form method="post" action="/reverse?ref=%s"><button>Reverse it</button></form>`,
				html.EscapeString(res.OrderID), html.EscapeString(res.ApprovalCode), html.EscapeString(ref)))
		}
	})

	http.HandleFunc("/reverse", func(w http.ResponseWriter, r *http.Request) {
		ref := r.URL.Query().Get("ref")
		rev, err := c.Reverse(r.Context(), &paycell.ReverseRequest{OriginalReferenceNumber: ref})
		if err != nil {
			page(w, "reverse failed: "+html.EscapeString(err.Error()))
			return
		}
		page(w, "reversed, approval "+html.EscapeString(rev.ApprovalCode))
	})

	log.Printf("open %s", base)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func page(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8">
<body style="font:16px/1.6 system-ui;max-width:40rem;margin:4rem auto;padding:0 1rem">
%s<p><a href="/">start again</a></p></body>`, body)
}
