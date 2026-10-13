# paycell-go

[Türkçe](README.md) · **English**

Go client for [Paycell](https://paycell.com.tr), Turkcell's payment platform.
Standard library only.

Every service is implemented and the whole thing is tested against Paycell's
sandbox: a 1 TL payment, its inquiry and its reversal, a 3D session, OTP, BIN
lookup and reconciliation all round-trip against the live test environment.

| Family | What it does |
| --- | --- |
| Provision services (`tpay`) | card payments, stored cards, 3D sessions, mobile payment (DCB), OTP, marketplace, reconciliation |
| Payment management | `getCardTokenSecure`, the 3D Secure redirect form |
| Web SDK | hosted checkout: session init, status query, reverse, refund |

```
go get github.com/omertheroot/paycell-go
```

Full service reference: [DOCS_EN.md](DOCS_EN.md).

## Examples

Each one shows a single flow and runs with no setup, on the credentials
Paycell publishes for integration work.

| Example | What it shows |
| --- | --- |
| [`basic-payment`](examples/basic-payment) | the short path: token, pay, inquire, reverse |
| [`3d-secure`](examples/3d-secure) | the browser redirect flow, on localhost |
| [`card-token-browser`](examples/card-token-browser) | tokenising the card without the number reaching your server |
| [`stored-cards`](examples/stored-cards) | storing a card, the OTP gated listing, paying by id |
| [`refund`](examples/refund) | reverse or refund, partial refunds, counting what applied |
| [`mobile-payment`](examples/mobile-payment) | direct carrier billing, OTP and `provisionAll` |
| [`web-sdk`](examples/web-sdk) | Paycell's hosted checkout and the status poll |

```
go run ./examples/basic-payment
```

```
1. pay 1 TL
   order 000000000000000390387970, approval 929205, reference 66627136356766211670
2. inquire
   SALE      100 kuruş  applied=true  Success
3. reverse
   approval 929205, reconciliation 20260911
```

`3d-secure` and `card-token-browser` each start a web server you open in a
browser. `web-sdk` needs a `TerminalCode`, which is not part of the published
credentials. The sandbox 3D password is `123456`.

## Setup

Paycell issues the credentials during onboarding and allow-lists the IP of
every server that calls the provision, cancel and status services.

```go
c, err := paycell.New(paycell.Config{
    ApplicationName:     "PAYCELLTEST",
    ApplicationPassword: "...",
    SecureCode:          "...",
    MerchantCode:        "9998",
    TerminalCode:        "12345",       // Web SDK only
    DefaultMSISDN:       "905381234567",
    ReferencePrefix:     "123",         // the first digits Paycell assigned you
    Environment:         paycell.Test,  // or paycell.Production
})
```

Every endpoint can be overridden (`ProvisionURL`, `PaymentURL`, `CardTokenURL`,
`CancelURL`, `SDKInitURL`, `SDKHomeURL`). Merchants are sometimes given
different hosts. In production the card token sits on
`epayment-gtm.turkcell.com.tr` and the 3D page on `epayment.turkcell.com.tr`;
both are the defaults.

## Paying

```go
res, err := c.Pay(ctx, paycell.PayParams{
    Card:            paycell.Card{Number: "5200190006338608", ExpireMonth: "01", ExpireYear: "30", CVC: "410"},
    Amount:          1525, // 15,25 TL, always minor units
    ReferenceNumber: c.NewReferenceNumber(),
})
```

`Pay` tokenises the card and provisions in one go. Store the reference number
with the order. Every later `Inquire`, `Reverse` and `Refund` needs it.

Pre-authorisation is the same call with a different payment type: `c.PreAuth`,
then `c.PostAuth` with the pre-auth's reference in
`OriginalReferenceNumber`.

## 3D Secure

The customer's browser has to make the POST to the bank, so the flow splits in
two around the redirect.

```go
// before
start, err := c.Start3D(ctx, params, "https://shop.example/paycell/callback")
save(start)                        // keyed by start.ReferenceNumber
w.Write([]byte(start.Form.HTML())) // self-submitting form

// after
res, err := c.Complete3D(ctx, load(r), params)
```

`Complete3D` queries the session result and charges only if the customer
passed. Otherwise it returns `*ErrNotVerified` and nothing is provisioned. The
callback itself is unauthenticated, so never take it as proof of anything.

## Saving cards

Three ways in, depending on when you want the card stored:

```go
// as its own step, from a fresh token
terms, _ := c.GetTermsOfServiceContent(ctx, nil)
token, _ := c.GetCardTokenSecure(ctx, card)
c.RegisterCard(ctx, &paycell.RegisterCardRequest{
    CardToken: token.CardToken,
    Alias:     "İş kartı",
    IsDefault: true,
    EulaID:    terms.ID(),
})

// or during a 3D payment, stored once verification passes
c.GetThreeDSession(ctx, &paycell.ThreeDSessionRequest{CardToken: ..., SaveCard: true})
```

A card token is single use. Tokenise again if you have just charged the card.

Listing stored cards sits behind an SMS OTP. The documented way is
`GetPaymentMethodList`: the list opens with whatever reference number the OTP
was sent and validated with.

```go
ref := c.NewReferenceNumber()
otp, _ := c.SendOTP(ctx, &paycell.SendOTPRequest{
    ReferenceNumber: ref,
    ExtraParameters: paycell.CardListOTPParams(), // VALIDATION_TYPE=PM_LIST
})
c.ValidateOTP(ctx, &paycell.ValidateOTPRequest{
    OTP: typedByCustomer, Token: otp.Token, ReferenceNumber: ref,
    ExtraParameters: paycell.CardListOTPParams(),
})
methods, err := c.GetPaymentMethodList(ctx, &paycell.PaymentMethodListRequest{ReferenceNumber: ref})

for _, card := range methods.Cards() {
    c.Pay(ctx, paycell.PayParams{CardID: card.CardID, Amount: 1525})
}
```

A validated reference lives far longer than the OTP: the sandbox still listed
cards for a reference validated five days earlier. Once it lapses you get
`3110`, and that is the time for a fresh OTP.

`GetPaymentMethods` and `GetCards` are the older listings and are gone from
Paycell's current docs. On the test channel they answered without any OTP,
but use `GetPaymentMethodList` for the card list: it is the one Paycell's
acceptance tests ask about. The mobile payment limit is in
`GetPaymentMethods`. Manage the stored set with `UpdateCard` and
`DeleteCard`.

## Errors

Every call returns its response and an error. `*paycell.Error` means Paycell
answered with a non-zero `responseCode`, and the response struct is still
filled in, so the code is there to branch on.

```go
var pErr *paycell.Error
if errors.As(err, &pErr) {
    log.Print(pErr.Code, pErr.Description)
}

// the two codes worth naming
paycell.IsAlreadySettled(err) // 2029: too late to reverse, refund in full
paycell.IsOTPRequired(err)    // 3110: confirm an OTP first
```

Anything Paycell returns that this package does not model stays in `res.Raw`,
and `c.Call(ctx, "someService/", in, &out)` posts an arbitrary body to any
provision service.

## Signatures

Two algorithms, and they are not interchangeable.

Provision and card token, uppercased at both stages:

```
securityData = base64(sha256(UPPER(applicationPwd + applicationName)))
hashData     = base64(sha256(UPPER(applicationName + transactionId +
               transactionDateTime + [responseCode] + [cardToken] +
               secureCode + securityData)))
```

Web SDK session init, not uppercased, keyed on the terminal:

```
securityData = base64(sha256(secureCode + terminalCode))
hashData     = base64(sha256(paymentReferenceNumber + terminalCode + amount +
               currency + paymentSecurity + hostAccount +
               installmentPlan… + securityData))
```

Both are handled for you, and both are pinned to golden vectors in the tests.
`RequestHash` and `VerifyCardTokenResponse` are exported for the flow Paycell
prefers, where the card number never touches your backend:

```go
req := c.BuildCardTokenRequest(card) // signed body, safe to hand to the client
// the browser POSTs it to c.CardTokenURL() and sends you the response
err := c.VerifyCardTokenResponse(req.Header.TransactionID, res)
```

`GetCardTokenSecure` verifies by default. Without that check, nothing stops a
client from inventing a card token.

## Conventions

- **Amounts are minor units.** 15,25 TL is `1525`. `Amount` marshals as a
  string, which is what Paycell wants, and decodes from a string or a number,
  which is what Paycell sends depending on the service. A decimal is an error
  rather than a guess, because rounding one books the wrong sum.
- **Success is `responseCode == "0"`.** For a 3D session result check
  `Verified()`, for a Web SDK status check `Paid()`.
- **Reverse on the day, refund after.** A reverse answering `2029` has to be
  refunded in full.
- **Reference numbers are 20 numeric digits**, the first few assigned to your
  application. `c.NewReferenceNumber()` builds one. Pass your own when your
  order id fits.
- **Currency is `"TRY"` on the provision services and `"99"` on the Web SDK.**
  Both are defaulted.
- After a timeout, call `Inquire` or `QueryPaymentStatus` before retrying. A
  timeout on your side says nothing about Paycell's.

## Service reference

Provision services, on `Config.ProvisionURL`:

`Provision` · `PreAuth` · `PostAuth` · `ProvisionAll` · `ProvisionForMarketPlace` ·
`Inquire` · `InquireAll` · `Reverse` · `Refund` · `RefundAll` ·
`GetThreeDSession` · `GetThreeDSessionResult` ·
`GetCards` · `RegisterCard` · `UpdateCard` · `DeleteCard` ·
`GetTermsOfServiceContent` · `GetCardBinInformation` · `GetPaymentMethods` ·
`GetPaymentMethodList` · `OpenMobilePayment` · `SendOTP` · `ValidateOTP` ·
`GetProvisionHistory` · `SummaryReconciliation`

Payment management, on `Config.PaymentURL` and `Config.CardTokenURL`:

`GetCardTokenSecure` · `BuildCardTokenRequest` · `ThreeDSecureForm`

Web SDK, on `Config.SDKInitURL` and `Config.CancelURL`:

`InitSession` · `QueryPaymentStatus` · `ReversePayment` · `RefundPayment` ·
`SDKSummaryReconciliation`

Convenience: `Pay` · `Start3D` · `Complete3D` · `Call`

`example_test.go` has a worked flow for each, and [`examples/`](examples)
has the end-to-end programs. Field-by-field reference in
[DOCS_EN.md](DOCS_EN.md).

## Testing

Unit tests use `httptest`. No credentials, no network:

```
go test ./...
```

Integration tests hit the real sandbox:

```
go test -tags integration -v
```

They default to the sandbox credentials Paycell publishes (`PAYCELLTEST`,
merchant `9998`, eulaId `17`) and read overrides from `PAYCELL_APP_NAME`,
`PAYCELL_APP_PASSWORD`, `PAYCELL_SECURE_CODE`, `PAYCELL_MERCHANT_CODE`,
`PAYCELL_EULA_ID`, `PAYCELL_PREFIX` and `PAYCELL_MSISDN`. A decode failure
fails the test because it means a struct is wrong; a Paycell response code is
logged, because it means the sandbox is in an unexpected state. Payments are
1 TL and every one is reversed.

Test card: Denizbank `5200190006338608`, `01/30`, CVC `410`, 3D password
`123456`.

### What the sandbox changed

Three request rules that no public documentation mentions, found by bisecting
live requests:

- `getThreeDSession` rejects a `referenceNumber` with `2032`, and
  `getThreeDSessionResult` rejects it with `2041`. It belongs on the provision
  that follows, and this package keeps it there.
- `openMobilePayment` requires a `referenceNumber` (`2007`) and is OTP gated
  like the card list.
- `eulaId` arrives quoted from `getCards` and unquoted from
  `getTermsOfServiceContent`; `binValue` is always unquoted. Fields like these
  are typed `FlexString`, which takes either.

PREAUTH and POSTAUTH answer `4015` and `1024` on the shared sandbox merchant
while SALE with the same card and fields succeeds, which points at merchant
configuration rather than request shape. Check them against your own test
merchant. The Web SDK needs a `TerminalCode` that is not in the published
credentials, so `InitSession` is unit-tested only.

## References

Built from Paycell's SDK integration documentation, Paycell's official
WooCommerce, PrestaShop and OpenCart gateway plugins, the `omnipay-paycell` and
`omnipay-paycell-sdk` PHP libraries, and two existing Go implementations, then
corrected against the sandbox.

`ProvisionForMarketPlace` and `GetProvisionHistory` are the two services the
sandbox could not exercise. Their field names come from third-party code, so
check them against your integration pack. Unmodelled fields survive in
`res.Raw` either way.

## Licence

MIT
