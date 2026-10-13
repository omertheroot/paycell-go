# paycell-go servis dokümantasyonu

**Türkçe** · [English](DOCS_EN.md)

Her servisin isteği, yanıtı ve sandbox'ta öğrenilen kuralları.
Genel kullanım için [README.md](README.md).

- [Temeller](#temeller)
- [Kart token'ı](#kart-tokenı)
- [Ödeme](#ödeme)
- [Sorgu, iptal, iade](#sorgu-iptal-iade)
- [3D Secure](#3d-secure)
- [Saklı kartlar](#saklı-kartlar)
- [Mobil ödeme ve OTP](#mobil-ödeme-ve-otp)
- [BIN, sözleşme, ödeme yöntemleri](#bin-sözleşme-ödeme-yöntemleri)
- [Mutabakat](#mutabakat)
- [Web SDK](#web-sdk)
- [Hata kodları](#hata-kodları)

## Temeller

### İstemci

`paycell.New` yapılandırmayı doğrular ve eşzamanlı kullanıma uygun bir
`*Client` döner. `ApplicationName`, `ApplicationPassword` ve `SecureCode`
zorunlu.

| Alan | Ne işe yarar |
| --- | --- |
| `ApplicationName`, `ApplicationPassword`, `SecureCode` | imzayı kurar, üçü de zorunlu |
| `MerchantCode` | üye işyeri kodu, dolduran çağrılarda varsayılan |
| `TerminalCode` | yalnızca Web SDK |
| `EulaID` | kart saklama ve mobil ödeme açmada varsayılan sözleşme |
| `Environment` | `paycell.Test` ya da `paycell.Production` |
| `ClientIPAddress` | isteğe yazılan IP, boşsa `127.0.0.1`. Paycell müşterinin IP:port'unu istiyor, `PayParams.ClientIP` istek başına verir |
| `DefaultMSISDN` | çağrılarda numara verilmezse kullanılır |
| `ReferencePrefix` | `NewReferenceNumber`'ın başa koyduğu haneler |
| `SkipResponseHashCheck` | kart token yanıtının imza kontrolünü kapatır, açık bırakın |
| `ProvisionURL`, `PaymentURL`, `CardTokenURL`, `CancelURL`, `SDKInitURL`, `SDKHomeURL` | adres değişikliği; yalnız `PaymentURL` verilirse `CardTokenURL` onu izler |
| `HTTPClient`, `Timeout` | taşıma katmanı, varsayılan 30 sn |

### Varsayılan adresler

| | Test | Prod |
| --- | --- | --- |
| Provizyon | `tpay-test.turkcell.com.tr/tpay/provision/services/restful/getCardToken/` | `tpay.turkcell.com.tr/...` |
| Kart token'ı | `omccstb.turkcell.com.tr/paymentmanagement/rest/getCardTokenSecure` | `epayment-gtm.turkcell.com.tr/...` |
| 3D sayfası | `omccstb.turkcell.com.tr/paymentmanagement/rest/threeDSecure` | `epayment.turkcell.com.tr/...` |
| İptal / statü | `zone-test.turkcell.com.tr/tpay/zone/services/cancelrestful/cancelService/` | `zone-ist.paycell.com.tr/...` |
| Web SDK | `websdktest.turkcell.com.tr/api/session/init` | `paycellsdk.paycell.com.tr/...` |

### İstek ve yanıt başlığı

Her provizyon isteğinde `requestHeader` var. Boş bırakırsanız istemci
doldurur. `TransactionID`'yi kendiniz verirseniz aynı isteği tekrarlamak
güvenli olur.

```go
type RequestHeader struct {
    ApplicationName     string // applicationName
    ApplicationPwd      string // applicationPwd
    ClientIPAddress     string // clientIPAddress
    TransactionDateTime string // transactionDateTime, yyyyMMddHHmmssSSS
    TransactionID       string // transactionId, applicationName bazında tekil, 20 hane
}
```

Yanıtta `responseHeader` döner: `transactionId`, `responseDateTime`,
`responseCode`, `responseDescription`. `responseCode` `"0"` ise iş başarılı.
`ResponseHeader.OK()` ve `.Err()` bunu okur.

### Tutarlar

`Amount` kuruş cinsinden `int64`. 15,25 TL için `1525`. İstekte string olarak
gider; yanıtta hem string hem sayı kabul edilir, çünkü servise göre ikisi de
geliyor. Ondalıklı değer hata verir. `.TRY()` lira karşılığını, `.String()`
kuruş karşılığını verir.

### Esnek alanlar

`FlexString`, Paycell'in kâh tırnaklı kâh tırnaksız gönderdiği alanlar için.
`eulaId` `getCards`'ta tırnaklı, `getTermsOfServiceContent`'te tırnaksız
geliyor; `binValue` her zaman tırnaksız.

### Hatalar

```go
res, err := c.Provision(ctx, req)   // res hata durumunda da dolu gelir

var pErr *paycell.Error             // Code, Description, TransactionID
errors.As(err, &pErr)
paycell.HasCode(err, "2029")
paycell.IsAlreadySettled(err)       // 2029
paycell.IsOTPRequired(err)          // 3110
```

HTTP seviyesindeki hatalar (`http 502: ...`) ve çözümleme hataları düz
`error` olarak döner. Modellenmemiş her alan `res.Raw` içinde durur.

### İmza

İki aşamalı, iki aşamada da büyük harfe çevrilir:

```
securityData = base64(sha256(UPPER(applicationPwd + applicationName)))
hashData     = base64(sha256(UPPER(applicationName + transactionId +
               transactionDateTime + [responseCode] + [cardToken] +
               secureCode + securityData)))
```

`responseCode` ve `cardToken` istek imzasında boş, yanıt imzasında dolu.
Büyük harfe çevirme atlanırsa imza hiçbir zaman tutmaz. Dışarı açık olanlar:
`RequestHash`, `ResponseHash`, `VerifyCardTokenResponse`.

### Yardımcılar

| | |
| --- | --- |
| `paycell.NewTransactionID()` | 20 haneli tekil işlem numarası |
| `paycell.TransactionDateTime(t)` | `yyyyMMddHHmmssSSS` |
| `c.NewReferenceNumber()` | önek + rastgele, 20 hane |
| `paycell.CardListOTPParams()` | `VALIDATION_TYPE=PM_LIST` |
| `c.Call(ctx, "servis/", in, &out)` | modellenmemiş servisler için |

## Kart token'ı

`getCardTokenSecure`, `Config.CardTokenURL` adresinde. Kart verisi yalnızca bu
servise gider, provizyon servislerine hiç gitmez.

```go
res, err := c.GetCardTokenSecure(ctx, paycell.Card{
    Number: "5200190006338608", ExpireMonth: "01", ExpireYear: "30", CVC: "410",
    Holder: "AD SOYAD", // ccAuthor, isteğe bağlı
})
```

| İstek | Yanıt |
| --- | --- |
| `header{applicationName, transactionId, transactionDateTime}` | `header{...}` |
| `creditCardNo`, `expireDateMonth`, `expireDateYear`, `cvcNo`, `ccAuthor` | `cardToken` |
| `hashData` | `hashData` |

Başlıkta parola yok, provizyon başlığından farklı.

3D'li bir ödemenin token'ı ve oturumu, provizyondan sonra kartı saklamak için
de geçerli; yeni token gerekmez.

Paycell'in tercih ettiği akışta bu çağrıyı tarayıcı yapar, böylece kart
numarası sizin sunucunuza uğramaz:

```go
req := c.BuildCardTokenRequest(card)   // imzalı gövde
// tarayıcı bunu c.CardTokenURL() adresine POST eder, yanıtı size gönderir
err := c.VerifyCardTokenResponse(req.Header.TransactionID, res)
```

`GetCardTokenSecure` imzayı kendi doğrular. `ErrHashMismatch` dönerse yanıt
sizin bilgilerinizle üretilmemiş demektir.

## Ödeme

Paycell'in dokümanında ödeme servisi `provisionAll`. `provision` hâlâ cevap
veriyor ama dokümanda geçmiyor; `Pay` ve `Complete3D` bu yüzden
`provisionAll` kullanır. Aşağıdaki alanlar ikisinde de aynı.

### provision

```go
res, err := c.Provision(ctx, &paycell.ProvisionRequest{
    CardToken:       token.CardToken, // ya da CardID
    Amount:          1525,
    ReferenceNumber: c.NewReferenceNumber(),
})
```

| İstek alanı | Not |
| --- | --- |
| `cardId` / `cardToken` | biri; `cardId` saklı kart, `cardToken` yeni girilen kart |
| `referenceNumber` | 20 hane sayısal, boşsa üretilir |
| `originalReferenceNumber` | yalnızca POSTAUTH'ta, ön otorizasyonun referansı |
| `amount`, `pointAmount`, `currency` | `currency` boşsa `TRY` |
| `installmentCount` | 0 ya da 1 tek çekim |
| `paymentType` | `SALE`, `PREAUTH`, `POSTAUTH`; boşsa `SALE` |
| `acquirerBankCode`, `threeDSessionId`, `pin`, `extraParameters` | |

Yanıt: `orderId`, `approvalCode`, `acquirerBankCode`, `issuerBankCode`,
`reconciliationDate`, iyzico üzerinden geçtiyse `iyzPaymentId` ve
`iyzPaymentTransactionId`. `referenceNumber` yanıtta boş gelirse paket
isteğinkini kopyalar.

`PreAuth` ve `PostAuth` aynı çağrının `paymentType` değiştirilmiş hâli.

### provisionAll

Kartla ödeme de dahil bütün yöntemler için: kart, mobil ödeme, Paycell Kart,
e-para. `ProvisionRequest`'in bütün alanları geçerli, üstüne
`paymentMethodType`: `CREDIT_CARD` (varsayılan), `DCB`, `PCARD`, `EPARA`.

```go
c.ProvisionAll(ctx, &paycell.ProvisionAllRequest{
    PaymentMethodType: paycell.MethodDCB,
    ProvisionRequest:  paycell.ProvisionRequest{Amount: 1525, ReferenceNumber: ref},
})
```

### provisionForMarketPlace

Tek ödemeyi alt üye işyerlerine böler. `marketPlaceList` içindeki tutarların
toplamı ödeme tutarına eşit olmalı. Her satır: `merchantCode`, `amount`,
`orderId`, `itemId`.

Bu servis sandbox'ta çalıştırılamadı, alan adları üçüncü parti kodlardan
alındı.

## Sorgu, iptal, iade

### inquire / inquireAll

```go
res, err := c.Inquire(ctx, &paycell.InquireRequest{OriginalReferenceNumber: ref})
```

Timeout aldığınızda tekrar denemeden önce bunu çağırın. Ödeme geçmiş olabilir.

Yanıttaki `provisionList` o referansa yapılmış her denemeyi verir:
`provisionType` (`SALE`, `REVERSE`, `REFUND`, `PREAUTH`, `POSTAUTH`),
`amount`, `approvalCode`, `dateTime`, `reconciliationDate`, `responseCode`,
`responseDescription`. Başarısız denemeler de listede görünür.

`InquireAll` aynı çağrının `paymentMethodType` alanlı hâli.

### reverse

Aynı gün yapılan ödemeyi iptal eder. `referenceNumber` iptal işleminin kendi
referansı, `originalReferenceNumber` iptal edilen ödeme.

`2029` dönerse ödeme gün sonuna girmiş, tamamını iade edin:

```go
_, err := c.Reverse(ctx, &paycell.ReverseRequest{OriginalReferenceNumber: ref})
if paycell.IsAlreadySettled(err) {
    _, err = c.Refund(ctx, &paycell.RefundRequest{OriginalReferenceNumber: ref})
}
```

### refund / refundAll

`Amount` **zorunlu**. Boş gönderirseniz servis `2008` döner. Tamamını iade
etmek için ödemenin tutarını, kısmi iade için daha küçük bir tutarı gönderin.

İadeler birikir: 5 TL'lik ödemeye iki kez 2 TL iade geçebilirsiniz. Bir iade
işledikten sonra ödeme artık iptal edilemez, `2027` döner.

Web SDK tarafındaki `RefundPayment` farklı: Paycell dokümanı orada tutarı
opsiyonel gösteriyor. O servis sandbox'ta denenemedi.

### İade ve iptal kuralları (sandbox'ta ölçüldü)

| Davranış | Sonuç |
| --- | --- |
| Tutarsız iade | `2008`, tutar zorunlu |
| Toplam iade > ödeme tutarı | `2015 Total refund amount is greater than original amount` |
| Kısmi iade | çalışıyor, iadeler birikir |
| Kısmi iptal | **yok**, iptal provizyonun tamamını geri alır |
| İadeden sonra iptal | `2027` |
| İptalin onay kodu | ödemenin onay koduyla aynı |
| İadenin onay kodu | yeni kod |

Reddedilen bir iade de `provisionList` içinde görünür ve `responseCode` alanı
`"0"` gelir. Gerçekten para hareket etmiş satırı ayıran şey onay kodudur,
yanıt kodu değil. `ProvisionItem.Applied()` bunu okur:

```go
var iadeEdilen paycell.Amount
for _, p := range inq.ProvisionList {
    if p.ProvisionType == "REFUND" && p.Applied() { // sadece ResponseCode'a bakmayın
        iadeEdilen += p.Amount
    }
}
```

Ölçümde 600 kuruşluk iade satırı vardı, gerçekte işleyen 300 kuruştu.

`reverse` ve `refund` aynı yanıtı döner: `approvalCode`,
`reconciliationDate`, bankaya ulaşılamadıysa `retryStatusCode` ve
`retryStatusDescription`.

## 3D Secure

Üç adım. Kart token'a çevrilir, token ile oturum açılır, müşterinin tarayıcısı
o oturumla bankaya POST eder. Dönüşte sonuç sorgulanır ve geçildiyse ödeme
ayrı bir provizyonla alınır.

### getThreeDSession

```go
sess, err := c.GetThreeDSession(ctx, &paycell.ThreeDSessionRequest{
    CardToken: token.CardToken, // ya da CardID
    Amount:    1525,
})
```

`target` boşsa `MERCHANT`, `transactionType` boşsa `AUTH` (ön otorizasyon için
`PREAUTH`), `currency` boşsa `TRY`. `amount` zorunlu, boşsa `2008`; yalnızca
kart saklamak için açılan oturumda 1 kuruş yeter.

**Bu servise `referenceNumber` göndermeyin.** Gönderilirse `2032` döner.
Referans numarası arkadan gelen provizyona ait.

### Yönlendirme formu

```go
form := c.ThreeDSecureForm(paycell.ThreeDFormParams{
    ThreeDSessionID: sess.ThreeDSessionID,
    CallbackURL:     "https://magaza.example/callback?ref=" + ref,
    IsPoint:         false, // puanla ödeme
    IsPost3DResult:  true,  // sonuç callback'e POST edilir
})
w.Write([]byte(form.HTML()))
```

`HTML()` kendi kendini gönderen bir sayfa üretir, bütün değerler kaçırılmış
hâlde. POST'u sunucudan atmayın, dönüş yolu bozulur.

Dönüş (test kanalında ölçüldü):

| `isPost3DResult` | Callback'e gelen |
| --- | --- |
| `true` | `POST`, form gövdesi: `oid`, `mdStatus`, `mdErrorMsg`, `clientid`, `result`, `resultDescription`, `sessionId`, `currentStep`, `hash` |
| `false` | `GET`, Paycell adrese hiçbir şey eklemez, yalnızca sizin query'niz gelir |

`Start3D` `true` gönderir. Gövdedeki alanlar yine de kanıt değil, sonucu
`GetThreeDSessionResult` ile sorun.

Paycell müşteriyi yalnızca uygulamanız için tanımlanmış adreslere geri
gönderir. Tanımsız bir callback adresinde müşteri bankaya hiç gitmez, Paycell'in
"3D Yönlendirme Hatası" sayfasında kalır ve oturum düşer (sonuç
`90013 Session not found`). Sandbox'ta `localhost` port ve yol fark etmeksizin
geçiyor, `127.0.0.1` geçmiyor. Canlı callback adresinizi Paycell'e
tanımlatın.

### getThreeDSessionResult

```go
result, err := c.GetThreeDSessionResult(ctx, &paycell.ThreeDSessionResultRequest{
    ThreeDSessionID: sess.ThreeDSessionID,
})
if !result.Verified() { /* ödeme almayın */ }
```

`err == nil` yalnızca sorgunun çalıştığını söyler. Doğrulamanın geçtiğini
`Verified()` söyler, yani `threeDResult == "0"`. Yanıtta ayrıca `currentStep`,
`mdStatus`, `mdErrorMessage` ve `threeDOperationResult` var.

**Bu servise de `referenceNumber` göndermeyin.** Gönderilirse `2041` döner.

### Kolay yol

```go
start, err := c.Start3D(ctx, params, callbackURL) // token + oturum + form
res, err := c.Complete3D(ctx, start, params)      // sonuç + provizyon
```

`Complete3D` doğrulama geçilmediyse `*ErrNotVerified` döner ve provizyon
istemez. `start` yapısını referans numarasıyla saklayın: callback
doğrulanmamış bir kaynak, tek kaydınız o.

## Saklı kartlar

### Listeleme

Paycell'in belgelediği listeleme `GetPaymentMethodList`. SMS OTP'ye bağlı:
önce `CardListOTPParams()` ile `SendOTP` ve `ValidateOTP`, sonra liste aynı
`ReferenceNumber` ile. Doğrulanmamış referans `3110` alır.

```go
methods, err := c.GetPaymentMethodList(ctx, &paycell.PaymentMethodListRequest{
    ReferenceNumber: ref, // OTP'nin gönderildiği referans
})
if paycell.IsOTPRequired(err) { // 3110
    // aynı ref ile SendOTP + ValidateOTP, sonra tekrar
}
for _, card := range methods.Cards() { ... }
```

`ExtraParameters` boşsa `VALIDATION_TYPE=PM_LIST` gider, doküman bunu burada
da istiyor. Sandbox'ta doğrulanmış bir referans beş gün sonra da listeyi
açıyordu.

`GetPaymentMethods` ve `GetCards` eski listeler, dokümanda yoklar; test
kanalında OTP istemeden cevap verdiler. `GetPaymentMethods` mobil ödeme
durumunu da döner, `OTPToken` alanı bu ikisi için. `Cards()` kart listesini
`cardList`'ten ya da `paymentMethods`'tan, hangisi doluysa oradan alır.

`StoredCard`: `cardId`, `cardBrand`, `cardType`, `maskedCardNo`, `alias`,
`activationDate`, `isDefault`, `isExpired`, `showEulaId`, `isThreeDValidated`,
`isOTPValidated`, `expiryMonth`, `expiryYear`, `issuerBankCode`,
`issuerBankDescription`.

### registerCard

Paycell kartı yalnızca müşterinin 3D'den geçtiği bir oturumla saklar,
`threeDSessionId` dokümanda zorunlu. Oturumsuz deneme test kanalında `3427`,
herkese açık sandbox'ta `4003` ile reddedildi.

```go
terms, _ := c.GetTermsOfServiceContent(ctx, nil)
c.RegisterCard(ctx, &paycell.RegisterCardRequest{
    CardToken:       token.CardToken,
    ThreeDSessionID: sess.ThreeDSessionID, // doğrulaması geçilmiş oturum
    Alias:           "İş kartı",
    IsDefault:       true,
    EulaID:          terms.ID(), // boşsa Config.EulaID
})
```

İki yol var, ikisi de test kanalında denendi:

- **Ödemeyle birlikte:** 3D'li ödemeden sonra aynı `cardToken` ve
  `threeDSessionId` ile `RegisterCard`. `Start3D`'nin döndüğü yapıda ikisi de
  var.
- **Ödemesiz:** 1 kuruşluk oturum aç, 3D'den geçir, provizyon istemeden
  `RegisterCard`. Para çekilmez.

Yanıtta `cardId` döner. Bir sonraki ödemede kart verisi yerine bu id yeter.
Kart zaten cüzdandaysa `3005` (`CodeCardAlreadyRegistered`) döner, yanıtta
`cardId` yine dolu gelir. Abone kart sınırındaysa `3002`; test kanalında sınır
iki karttı.

`ThreeDSessionRequest`'te `saveCard` diye bir alan yok. Dokümanda geçmiyor,
gönderildiğinde de hiçbir kart saklanmadı.

### updateCard / deleteCard

`UpdateCard` takma adı değiştirir ya da kartı varsayılan yapar; `cardId`
zorunlu. `DeleteCard` yalnızca `cardId` ister.

## Mobil ödeme ve OTP

### openMobilePayment

Aboneye DCB'yi açar.

```go
c.OpenMobilePayment(ctx, &paycell.OpenMobilePaymentRequest{
    EulaID: methods.MobilePayment.EulaID,
})
```

**`referenceNumber` zorunlu**, yoksa `2007` döner; paket boşsa üretir. Çağrı
saklı kart listesi gibi OTP'ye bağlı.

`MobilePaymentInfo`: `eulaId`, `eulaUrl`, `signedEulaId`, `statementDate`,
`limit`, `maxLimit`, `remainingLimit`, `isDcbOpen`, `isEulaExpired`.

### sendOTP / validateOTP

```go
otp, _ := c.SendOTP(ctx, &paycell.SendOTPRequest{Amount: 1525, ReferenceNumber: ref})
_, err := c.ValidateOTP(ctx, &paycell.ValidateOTPRequest{
    OTP: kullanicininGirdigi, Token: otp.Token, ReferenceNumber: ref, Amount: 1525,
})
```

`SendOTP` yanıtı: `token`, `expireDate`, `remainingRetryCount`. Yanlış kod
`3112` ile döner.

Saklı kart listesini açmak için istenen OTP'de
`ExtraParameters: paycell.CardListOTPParams()` gönderin, yani
`VALIDATION_TYPE=PM_LIST`. Aynı parametre ve aynı `ReferenceNumber` iki
çağrıda da, ardından gelen `GetPaymentMethodList`'te de olmalı. Sandbox'ta OTP
kodu `111111`.

## BIN, sözleşme, ödeme yöntemleri

### getCardBinInformation

```go
bin, err := c.GetCardBinInformation(ctx, &paycell.CardBinRequest{BinValue: "520019"})
if i := bin.First(); i != nil && i.IsCreditCard() {
    // taksit önerilebilir
}
```

Yanıt liste döner (`cardBinInformations`), `First()` ilkini verir.
Alanlar: `binValue` (tırnaksız gelir), `cardType` (`"Credit Card"` gibi serbest
metin, `IsCreditCard()` ile bakın), `cardBrand`, `cardOrganization`,
`bankName`, `bankCode`, `commercialType` (`BIREYSEL` / `TICARI`,
`IsCommercial()`), `typeId`.

### getTermsOfServiceContent

Yalnızca `requestHeader` ister. Yanıt: `eulaId` (tırnaksız gelir, `ID()` ile
okuyun), `termsOfServiceHtmlContentTR`, `termsOfServiceHtmlContentEN`.

## Mutabakat

### getProvisionHistory

`reconciliationDate` (YYYYMMDD) gününün işlemlerini `provisionList` olarak
döner.

### summaryReconciliation

Günün toplamlarını Paycell'e bildirir, karşılaştırma sonucu
`reconciliationResult` alanında gelir. Bütün toplam alanları gönderilmeli,
sıfır olanlar dahil; biri eksikse `2018` döner. Paket hepsini her zaman
gönderir.

Alanlar: `totalSaleAmount`, `totalReverseAmount`, `totalRefundAmount`,
`totalPreAuthAmount`, `totalPostAuthAmount`, `totalPreAuthReverseAmount`,
`totalPostAuthReverseAmount` ve bunların `...Count` karşılıkları.

## Web SDK

Paycell'in barındırdığı ödeme sayfası. Kartı Paycell toplar, siz sonucu
sorgularsınız. `TerminalCode` zorunlu.

### init

```go
session, err := c.InitSession(ctx, paycell.SDKParams{
    Amount:                 1525,
    PaymentReferenceNumber: ref,
    HostAccount:            "musteri@example.com",
    PaymentSecurity:        paycell.ThreeDSecure,
    ReturnURL:              "https://magaza.example/done?ref=[paymentReferenceNumber]",
    PostResultURL:          "https://magaza.example/paycell/result",
    InstallmentPlan: []paycell.Installment{
        {LineID: 1, PaymentMethodType: paycell.MethodCreditCard, CardBrand: "BONUS", Count: 3, Amount: 1525},
    },
})
// müşteriyi session.TrackingURL adresine gönderin
```

`ReturnURL` içindeki `[paymentReferenceNumber]` yer tutucusu doldurulur.
`Currency` boşsa `99`, `PaymentSecurity` boşsa `THREED_SECURE`, `Language`
boşsa `tr`, `TimeoutDuration` boşsa 600000 ms.

Taksit satırı alanları: `lineId` (her satırda tekil), `paymentMethodType`
(`CREDIT_CARD` / `DCB`), `cardBrand` (`BONUS`, `WORLD`, `AXESS`, `ISBANK`,
`FINANS`, `HALKBANK`, `TURKCELL`), `count`, `amount`.

İmza burada farklı: büyük harfe çevrilmez ve `secureCode + terminalCode`
üzerine kurulur. Taksit planı `lineId` sırasına göre imzaya girer.

### queryPaymentStatus

```go
status, err := c.QueryPaymentStatus(ctx, ref)
if status.Paid() { ... }
```

`responseCode` sorgunun çalışıp çalışmadığını, `status` ödemenin durumunu
söyler:

| `status` | |
| --- | --- |
| `0` | başarılı (`StatusSuccess`) |
| `1` | iptal edilmiş (`StatusReversed`) |
| `2` | iade edilmiş (`StatusRefunded`) |
| `3` | işlem bulunamadı (`StatusNotFound`) |
| `4` | beklemede (`StatusPending`) |
| `5` | bilinmeyen yanıt (`StatusUnknown`) |
| `6` | hata ya da timeout (`StatusFailed`) |

Yanıttaki diğer alanlar: `statusExplanation`, `msisdn`, `amount`, `currency`,
`installmentCount`, `approvalCode`, `orderId`, `paymentSecurity`,
`paymentDate`, `acquirerbankCode`, `issuerBankCode`, `paymentMethod` ve
`reconcilationDate`. Son alanın adı Paycell tarafında böyle yazılıyor, paket
onu `ReconciliationDate` olarak açıyor.

### reversePayment / refundPayment / summaryReconciliation

Ödeme referansı olarak `originalPaymentReferenceNumber` alırlar. Yanıtta
yönteme göre `reverseDetails` ya da `refundDetails` gelir.

## Hata kodları

Sandbox'ta doğrudan görülenler:

| Kod | Anlamı | Ne yapmalı |
| --- | --- | --- |
| `0` | başarılı | |
| `1002` | iç hata | isteği sorgulayın, körlemesine tekrarlamayın |
| `1024` | 3D doğrulamasında hata | |
| `2007` | `referenceNumber` boş | alanı gönderin |
| `2008` | `amount` boş | iadede tutar zorunlu |
| `2013` | işlem bulunamadı | referansı kontrol edin |
| `2015` | toplam iade ödemeyi aşıyor | kalan tutarı `Applied()` ile hesaplayın |
| `2018` | mutabakat toplamlarından biri boş | hepsini gönderin |
| `2027` | ödeme zaten iade edilmiş | iptal edilemez, kalanı iade edin |
| `2029` | iptal için geç kalındı | tamamını iade edin |
| `2032` | geçersiz parametre | 3D oturumuna `referenceNumber` göndermeyin |
| `2041` | provizyon kaydı bulunamadı | 3D sonucuna `referenceNumber` göndermeyin |
| `3110` | OTP doğrulanamadı | `SendOTP` + `ValidateOTP`, sonra aynı `ReferenceNumber` ile `GetPaymentMethodList` |
| `3002` | abone kart sınırında | önce bir kart silin; test kanalında sınır iki kart |
| `3005` | kart zaten kayıtlı | yanıttaki `cardId`'yi kullanın |
| `3112` | OTP hatalı | `remainingRetryCount` alanına bakın |
| `4003` | kart sahibi için provizyon kısıtlı | kartın aboneye tanımlı olması gerekiyor |
| `4015` | kart bilgisi boş | |

Tam liste entegrasyon dokümanınızda. `pErr.Description` Paycell'in kendi
açıklamasını taşır.
