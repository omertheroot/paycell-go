# paycell-go service reference

[Türkçe](DOCS.md) · **English**

Every service, its request, its response, and the rules the sandbox taught us.
For general use see [README_EN.md](README_EN.md).

- [Basics](#basics)
- [Card token](#card-token)
- [Payments](#payments)
- [Inquire, reverse, refund](#inquire-reverse-refund)
- [3D Secure](#3d-secure)
- [Stored cards](#stored-cards)
- [Mobile payment and OTP](#mobile-payment-and-otp)
- [BIN, terms, payment methods](#bin-terms-payment-methods)
- [Reconciliation](#reconciliation)
- [Web SDK](#web-sdk)
- [Response codes](#response-codes)

## Basics

### Client

`paycell.New` validates the configuration and returns a `*Client` that is safe
for concurrent use. `ApplicationName`, `ApplicationPassword` and `SecureCode`
are required.

| Field | What it does |
| --- | --- |
| `ApplicationName`, `ApplicationPassword`, `SecureCode` | build the signature, all three required |
| `MerchantCode` | merchant code, the default for calls that carry one |
| `TerminalCode` | Web SDK only |
| `EulaID` | default terms id for storing a card and opening mobile payment |
| `Environment` | `paycell.Test` or `paycell.Production` |
| `ClientIPAddress` | reported as the origin, `127.0.0.1` when empty. Paycell wants the customer's IP:port; `PayParams.ClientIP` sets it per payment |
| `DefaultMSISDN` | used by calls that do not carry a subscriber number |
| `ReferencePrefix` | the digits `NewReferenceNumber` puts in front |
| `SkipResponseHashCheck` | turns off card token signature checking, leave it off |
| `ProvisionURL`, `PaymentURL`, `CardTokenURL`, `CancelURL`, `SDKInitURL`, `SDKHomeURL` | endpoint overrides; `CardTokenURL` follows `PaymentURL` when only that is set |
| `HTTPClient`, `Timeout` | transport, 30s by default |

### Default endpoints

| | Test | Production |
| --- | --- | --- |
| Provision | `tpay-test.turkcell.com.tr/tpay/provision/services/restful/getCardToken/` | `tpay.turkcell.com.tr/...` |
| Card token | `omccstb.turkcell.com.tr/paymentmanagement/rest/getCardTokenSecure` | `epayment-gtm.turkcell.com.tr/...` |
| 3D page | `omccstb.turkcell.com.tr/paymentmanagement/rest/threeDSecure` | `epayment.turkcell.com.tr/...` |
| Cancel / status | `zone-test.turkcell.com.tr/tpay/zone/services/cancelrestful/cancelService/` | `zone-ist.paycell.com.tr/...` |
| Web SDK | `websdktest.turkcell.com.tr/api/session/init` | `paycellsdk.paycell.com.tr/...` |

### Request and response headers

Every provision request carries a `requestHeader`. Leave it zero and the
client fills it in. Set `TransactionID` yourself to make a retry idempotent.

```go
type RequestHeader struct {
    ApplicationName     string // applicationName
    ApplicationPwd      string // applicationPwd
    ClientIPAddress     string // clientIPAddress
    TransactionDateTime string // transactionDateTime, yyyyMMddHHmmssSSS
    TransactionID       string // transactionId, unique per applicationName, 20 digits
}
```

Responses carry a `responseHeader` with `transactionId`, `responseDateTime`,
`responseCode` and `responseDescription`. The call succeeded when
`responseCode` is `"0"`. `ResponseHeader.OK()` and `.Err()` read it.

### Amounts

`Amount` is an `int64` in minor units. 15,25 TL is `1525`. It marshals as a
string, and decodes from a string or a number, because Paycell sends both
depending on the service. A decimal is an error. `.TRY()` gives lira,
`.String()` gives kuruş.

### Flexible fields

`FlexString` covers fields Paycell sometimes quotes and sometimes does not.
`eulaId` arrives quoted from `getCards` and unquoted from
`getTermsOfServiceContent`; `binValue` is always unquoted.

### Errors

```go
res, err := c.Provision(ctx, req)   // res is filled in even on failure

var pErr *paycell.Error             // Code, Description, TransactionID
errors.As(err, &pErr)
paycell.HasCode(err, "2029")
paycell.IsAlreadySettled(err)       // 2029
paycell.IsOTPRequired(err)          // 3110
```

HTTP failures (`http 502: ...`) and decode failures come back as plain
errors. Anything not modelled stays in `res.Raw`.

### Signature

Two stages, uppercased at both:

```
securityData = base64(sha256(UPPER(applicationPwd + applicationName)))
hashData     = base64(sha256(UPPER(applicationName + transactionId +
               transactionDateTime + [responseCode] + [cardToken] +
               secureCode + securityData)))
```

`responseCode` and `cardToken` are empty in a request signature and present in
a response signature. Skip the uppercase step and the signature never matches.
Exported: `RequestHash`, `ResponseHash`, `VerifyCardTokenResponse`.

### Helpers

| | |
| --- | --- |
| `paycell.NewTransactionID()` | 20 digit unique transaction id |
| `paycell.TransactionDateTime(t)` | `yyyyMMddHHmmssSSS` |
| `c.NewReferenceNumber()` | prefix + random, 20 digits |
| `paycell.CardListOTPParams()` | `VALIDATION_TYPE=PM_LIST` |
| `c.Call(ctx, "service/", in, &out)` | for services this package does not model |

## Card token

`getCardTokenSecure`, at `Config.CardTokenURL`. Card data goes here and nowhere
else: the provision services never see it.

```go
res, err := c.GetCardTokenSecure(ctx, paycell.Card{
    Number: "5200190006338608", ExpireMonth: "01", ExpireYear: "30", CVC: "410",
    Holder: "NAME SURNAME", // ccAuthor, optional
})
```

| Request | Response |
| --- | --- |
| `header{applicationName, transactionId, transactionDateTime}` | `header{...}` |
| `creditCardNo`, `expireDateMonth`, `expireDateYear`, `cvcNo`, `ccAuthor` | `cardToken` |
| `hashData` | `hashData` |

The header carries no password, unlike the provision header.

A token is single use. Tokenise again if you want to store a card you have
just charged.

In the flow Paycell prefers, the browser makes this call, so the card number
never reaches your backend:

```go
req := c.BuildCardTokenRequest(card)   // signed body
// the browser POSTs it to c.CardTokenURL() and sends you the response
err := c.VerifyCardTokenResponse(req.Header.TransactionID, res)
```

`GetCardTokenSecure` verifies for you. `ErrHashMismatch` means the response
was not produced with your credentials.

## Payments

The payment service in Paycell's docs is `provisionAll`. `provision` still
answers but is no longer documented, so `Pay` and `Complete3D` use
`provisionAll`. The fields below are the same for both.

### provision

```go
res, err := c.Provision(ctx, &paycell.ProvisionRequest{
    CardToken:       token.CardToken, // or CardID
    Amount:          1525,
    ReferenceNumber: c.NewReferenceNumber(),
})
```

| Request field | Note |
| --- | --- |
| `cardId` / `cardToken` | one of them; `cardId` is a stored card, `cardToken` a freshly entered one |
| `referenceNumber` | 20 numeric digits, generated when empty |
| `originalReferenceNumber` | POSTAUTH only, the pre-auth's reference |
| `amount`, `pointAmount`, `currency` | `currency` defaults to `TRY` |
| `installmentCount` | 0 or 1 means a single charge |
| `paymentType` | `SALE`, `PREAUTH`, `POSTAUTH`; defaults to `SALE` |
| `acquirerBankCode`, `threeDSessionId`, `pin`, `extraParameters` | |

Response: `orderId`, `approvalCode`, `acquirerBankCode`, `issuerBankCode`,
`reconciliationDate`, plus `iyzPaymentId` and `iyzPaymentTransactionId` when
the payment was routed through iyzico. If the response omits
`referenceNumber`, the package copies the one from the request.

`PreAuth` and `PostAuth` are the same call with a different `paymentType`.

### provisionAll

Every method, cards included: card, mobile payment, Paycell Kart, e-money.
All `ProvisionRequest` fields apply, plus `paymentMethodType`: `CREDIT_CARD`
(default), `DCB`, `PCARD`, `EPARA`.

```go
c.ProvisionAll(ctx, &paycell.ProvisionAllRequest{
    PaymentMethodType: paycell.MethodDCB,
    ProvisionRequest:  paycell.ProvisionRequest{Amount: 1525, ReferenceNumber: ref},
})
```

### provisionForMarketPlace

Splits one payment between sub-merchants. The amounts in `marketPlaceList`
must add up to the payment. Each line takes `merchantCode`, `amount`,
`orderId` and `itemId`.

The sandbox could not exercise this service, so the field names come from
third-party code.

## Inquire, reverse, refund

### inquire / inquireAll

```go
res, err := c.Inquire(ctx, &paycell.InquireRequest{OriginalReferenceNumber: ref})
```

Call this after a timeout, before retrying anything. The payment may well have
gone through.

`provisionList` holds every attempt against that reference: `provisionType`
(`SALE`, `REVERSE`, `REFUND`, `PREAUTH`, `POSTAUTH`), `amount`,
`approvalCode`, `dateTime`, `reconciliationDate`, `responseCode`,
`responseDescription`. Failed attempts appear too.

`InquireAll` is the same call with a `paymentMethodType`.

### reverse

Voids a payment made the same day. `referenceNumber` is the reversal's own
reference, `originalReferenceNumber` is the payment.

`2029` means the payment has settled. Refund it in full instead:

```go
_, err := c.Reverse(ctx, &paycell.ReverseRequest{OriginalReferenceNumber: ref})
if paycell.IsAlreadySettled(err) {
    _, err = c.Refund(ctx, &paycell.RefundRequest{OriginalReferenceNumber: ref})
}
```

### refund / refundAll

`Amount` is **required**. Send it empty and the service answers `2008`. Send
the payment's amount to refund in full, a smaller one for a partial refund.

Refunds accumulate: a 5 TL payment takes two 2 TL refunds. Once one lands the
payment can no longer be reversed, which comes back as `2027`.

The Web SDK's `RefundPayment` differs: Paycell documents the amount as
optional there. That service was not reachable in the sandbox.

### Refund and reverse rules (measured in the sandbox)

| Behaviour | Result |
| --- | --- |
| Refund with no amount | `2008`, the amount is required |
| Refunds totalling more than the payment | `2015 Total refund amount is greater than original amount` |
| Partial refund | works, refunds accumulate |
| Partial reverse | **not a thing**, reverse voids the whole provision |
| Reverse after a refund | `2027` |
| Reverse's approval code | the same code as the payment |
| Refund's approval code | a new one |

A rejected refund is listed in `provisionList` too, carrying `responseCode`
`"0"` like the rest. What separates the lines that moved money is the approval
code, not the response code. `ProvisionItem.Applied()` reads it:

```go
var refunded paycell.Amount
for _, p := range inq.ProvisionList {
    if p.ProvisionType == "REFUND" && p.Applied() { // do not test ResponseCode alone
        refunded += p.Amount
    }
}
```

In the measurement above the refund lines totalled 600 kuruş; 300 had actually
applied.

`reverse` and `refund` share a response: `approvalCode`,
`reconciliationDate`, and `retryStatusCode` with
`retryStatusDescription` when Paycell could not reach the acquirer.

## 3D Secure

Three steps. The card becomes a token, the token opens a session, and the
customer's browser POSTs to the bank with that session. On the way back you
query the result and, if it passed, take the payment with a separate
provision.

### getThreeDSession

```go
sess, err := c.GetThreeDSession(ctx, &paycell.ThreeDSessionRequest{
    CardToken: token.CardToken, // or CardID
    Amount:    1525,
    SaveCard:  true, // store the card once verification passes
})
```

`target` defaults to `MERCHANT`, `transactionType` to `AUTH` (`PREAUTH` for a
pre-authorisation), `currency` to `TRY`.

**Do not send a `referenceNumber` here.** It comes back as `2032`. The
reference number belongs on the provision that follows.

### The redirect form

```go
form := c.ThreeDSecureForm(paycell.ThreeDFormParams{
    ThreeDSessionID: sess.ThreeDSessionID,
    CallbackURL:     "https://shop.example/callback?ref=" + ref,
    IsPoint:         false, // paying with Paycell points
    IsPost3DResult:  true,  // the result is POSTed to the callback
})
w.Write([]byte(form.HTML()))
```

`HTML()` renders a self-submitting page with every value escaped. Do not make
the POST from your server: it breaks the return trip.

The way back (measured on the test channel):

| `isPost3DResult` | What reaches the callback |
| --- | --- |
| `true` | `POST`, form body: `oid`, `mdStatus`, `mdErrorMsg`, `clientid`, `result`, `resultDescription`, `sessionId`, `currentStep`, `hash` |
| `false` | `GET`, Paycell adds nothing to the URL, only your own query arrives |

`Start3D` sends `true`. The body is still no proof of anything, ask
`GetThreeDSessionResult` for the outcome.

Paycell only sends customers back to addresses registered for your
application. With an unregistered callback the customer never reaches the
bank: they stop on Paycell's "3D Yönlendirme Hatası" page and the session is
lost (the result reads `90013 Session not found`). In the sandbox `localhost`
passes whatever the port and path, `127.0.0.1` does not. Have Paycell register
your production callback.

### getThreeDSessionResult

```go
result, err := c.GetThreeDSessionResult(ctx, &paycell.ThreeDSessionResultRequest{
    ThreeDSessionID: sess.ThreeDSessionID,
})
if !result.Verified() { /* do not charge */ }
```

`err == nil` only says the query worked. `Verified()` says the customer
passed, which is `threeDResult == "0"`. The response also carries
`currentStep`, `mdStatus`, `mdErrorMessage` and `threeDOperationResult`.

**Do not send a `referenceNumber` here either.** It comes back as `2041`.

### The short way

```go
start, err := c.Start3D(ctx, params, callbackURL) // token + session + form
res, err := c.Complete3D(ctx, start, params)      // result + provision
```

`Complete3D` returns `*ErrNotVerified` and provisions nothing when the
customer did not pass. Store `start` against the reference number: the
callback is unauthenticated, so it is your only record.

## Stored cards

### Listing

The listing Paycell documents is `GetPaymentMethodList`. It sits behind an SMS
OTP: `SendOTP` and `ValidateOTP` with `CardListOTPParams()` first, then the
list with the same `ReferenceNumber`. An unvalidated reference gets `3110`.

```go
methods, err := c.GetPaymentMethodList(ctx, &paycell.PaymentMethodListRequest{
    ReferenceNumber: ref, // the reference the OTP went out with
})
if paycell.IsOTPRequired(err) { // 3110
    // SendOTP + ValidateOTP with the same ref, then again
}
for _, card := range methods.Cards() { ... }
```

`ExtraParameters` defaults to `VALIDATION_TYPE=PM_LIST`, which the docs ask
for here as well. In the sandbox a validated reference still opened the list
five days later.

`GetPaymentMethods` and `GetCards` are the older listings and are not in the
docs; on the test channel they answered without an OTP. `GetPaymentMethods`
also reports the mobile payment standing, and `OTPToken` is for these two.
`Cards()` reads the list from `cardList` or `paymentMethods`, whichever came
back.

`StoredCard`: `cardId`, `cardBrand`, `cardType`, `maskedCardNo`, `alias`,
`activationDate`, `isDefault`, `isExpired`, `showEulaId`, `isThreeDValidated`,
`isOTPValidated`, `expiryMonth`, `expiryYear`, `issuerBankCode`,
`issuerBankDescription`.

### registerCard

```go
terms, _ := c.GetTermsOfServiceContent(ctx, nil)
c.RegisterCard(ctx, &paycell.RegisterCardRequest{
    CardToken:       token.CardToken,
    Alias:           "İş kartı",
    IsDefault:       true,
    EulaID:          terms.ID(),  // falls back to Config.EulaID
    ThreeDSessionID: "",          // set when the merchant requires 3D first
})
```

The response carries `cardId`. Later payments use that instead of card data.

### updateCard / deleteCard

`UpdateCard` renames a card or makes it the default; `cardId` is required.
`DeleteCard` needs only `cardId`.

## Mobile payment and OTP

### openMobilePayment

Turns on direct carrier billing for a subscriber.

```go
c.OpenMobilePayment(ctx, &paycell.OpenMobilePaymentRequest{
    EulaID: methods.MobilePayment.EulaID,
})
```

**`referenceNumber` is required** or you get `2007`; the package generates one
when it is empty. The call is OTP gated like the stored card list.

`MobilePaymentInfo`: `eulaId`, `eulaUrl`, `signedEulaId`, `statementDate`,
`limit`, `maxLimit`, `remainingLimit`, `isDcbOpen`, `isEulaExpired`.

### sendOTP / validateOTP

```go
otp, _ := c.SendOTP(ctx, &paycell.SendOTPRequest{Amount: 1525, ReferenceNumber: ref})
_, err := c.ValidateOTP(ctx, &paycell.ValidateOTPRequest{
    OTP: typedByCustomer, Token: otp.Token, ReferenceNumber: ref, Amount: 1525,
})
```

`SendOTP` returns `token`, `expireDate` and `remainingRetryCount`. A wrong
code comes back as `3112`.

For the OTP that unlocks the stored card list, send
`ExtraParameters: paycell.CardListOTPParams()`, which is
`VALIDATION_TYPE=PM_LIST`. Both calls need it and the same `ReferenceNumber`,
and so does the `GetPaymentMethodList` that follows. The sandbox OTP is
`111111`.

## BIN, terms, payment methods

### getCardBinInformation

```go
bin, err := c.GetCardBinInformation(ctx, &paycell.CardBinRequest{BinValue: "520019"})
if i := bin.First(); i != nil && i.IsCreditCard() {
    // installments can be offered
}
```

The response is a list (`cardBinInformations`); `First()` returns the record.
Fields: `binValue` (unquoted), `cardType` (free text such as `"Credit Card"`,
test it with `IsCreditCard()`), `cardBrand`, `cardOrganization`, `bankName`,
`bankCode`, `commercialType` (`BIREYSEL` / `TICARI`, see `IsCommercial()`),
`typeId`.

### getTermsOfServiceContent

Takes only a `requestHeader`. Returns `eulaId` (unquoted, read it with
`ID()`), `termsOfServiceHtmlContentTR` and `termsOfServiceHtmlContentEN`.

## Reconciliation

### getProvisionHistory

Returns the transactions for `reconciliationDate` (YYYYMMDD) as
`provisionList`.

### summaryReconciliation

Sends your daily totals to Paycell and reports the comparison in
`reconciliationResult`. Every total must be present, including the zeroes;
one missing field gives `2018`. The package always sends all of them.

Fields: `totalSaleAmount`, `totalReverseAmount`, `totalRefundAmount`,
`totalPreAuthAmount`, `totalPostAuthAmount`, `totalPreAuthReverseAmount`,
`totalPostAuthReverseAmount` and the matching `...Count` fields.

## Web SDK

Paycell's hosted checkout. Paycell collects the card, you query the outcome.
`TerminalCode` is required.

### init

```go
session, err := c.InitSession(ctx, paycell.SDKParams{
    Amount:                 1525,
    PaymentReferenceNumber: ref,
    HostAccount:            "customer@example.com",
    PaymentSecurity:        paycell.ThreeDSecure,
    ReturnURL:              "https://shop.example/done?ref=[paymentReferenceNumber]",
    PostResultURL:          "https://shop.example/paycell/result",
    InstallmentPlan: []paycell.Installment{
        {LineID: 1, PaymentMethodType: paycell.MethodCreditCard, CardBrand: "BONUS", Count: 3, Amount: 1525},
    },
})
// send the customer to session.TrackingURL
```

The `[paymentReferenceNumber]` placeholder in `ReturnURL` is substituted.
`Currency` defaults to `99`, `PaymentSecurity` to `THREED_SECURE`, `Language`
to `tr`, `TimeoutDuration` to 600000 ms.

Installment line fields: `lineId` (unique per line), `paymentMethodType`
(`CREDIT_CARD` / `DCB`), `cardBrand` (`BONUS`, `WORLD`, `AXESS`, `ISBANK`,
`FINANS`, `HALKBANK`, `TURKCELL`), `count`, `amount`.

The signature differs here: no uppercase step, and it is built on
`secureCode + terminalCode`. The installment plan enters the signature ordered
by `lineId`.

### queryPaymentStatus

```go
status, err := c.QueryPaymentStatus(ctx, ref)
if status.Paid() { ... }
```

`responseCode` says whether the query worked, `status` says what happened to
the payment:

| `status` | |
| --- | --- |
| `0` | paid (`StatusSuccess`) |
| `1` | reversed (`StatusReversed`) |
| `2` | refunded (`StatusRefunded`) |
| `3` | not found (`StatusNotFound`) |
| `4` | pending (`StatusPending`) |
| `5` | unknown response (`StatusUnknown`) |
| `6` | error or timeout (`StatusFailed`) |

Other fields: `statusExplanation`, `msisdn`, `amount`, `currency`,
`installmentCount`, `approvalCode`, `orderId`, `paymentSecurity`,
`paymentDate`, `acquirerbankCode`, `issuerBankCode`, `paymentMethod` and
`reconcilationDate`. Paycell spells that last one without the second `i`; the
package exposes it as `ReconciliationDate`.

### reversePayment / refundPayment / summaryReconciliation

They take `originalPaymentReferenceNumber` as the payment reference. The
response carries `reverseDetails` or `refundDetails` depending on the method.

## Response codes

Seen directly in the sandbox:

| Code | Meaning | What to do |
| --- | --- | --- |
| `0` | success | |
| `1002` | internal error | inquire before retrying blindly |
| `1024` | 3D authentication error | |
| `2007` | `referenceNumber` is empty | send the field |
| `2008` | `amount` is empty | refunds require an amount |
| `2013` | order not found | check the reference |
| `2015` | refunds exceed the payment | compute what is left with `Applied()` |
| `2018` | a reconciliation total is empty | send all of them |
| `2027` | payment already refunded | it cannot be reversed, refund the rest |
| `2029` | too late to reverse | refund in full |
| `2032` | invalid parameter | do not send `referenceNumber` to the 3D session |
| `2041` | provision entity not found | do not send `referenceNumber` to the 3D result |
| `3110` | OTP not validated | `SendOTP` + `ValidateOTP`, then `GetPaymentMethodList` with the same `ReferenceNumber` |
| `3112` | wrong OTP | check `remainingRetryCount` |
| `4003` | provision restricted for card holder | the card must belong to the subscriber |
| `4015` | card info is empty | |

Your integration pack has the full list. `pErr.Description` carries Paycell's
own wording.
