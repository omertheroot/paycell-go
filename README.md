# paycell-go

**Türkçe** · [English](README_EN.md)

Turkcell'in ödeme platformu [Paycell](https://paycell.com.tr) için Go
istemcisi. Standart kütüphane dışında bağımlılığı yok.

Bütün servisler yazıldı ve tamamı Paycell sandbox'ına karşı test edildi:
1 TL'lik bir ödeme, sorgusu ve iptali, 3D oturumu, OTP, BIN sorgusu ve
mutabakat canlı test ortamında baştan sona çalışıyor.

| Servis ailesi | Ne yapar |
| --- | --- |
| Provizyon servisleri (`tpay`) | kart ödemesi, saklı kart, 3D oturumu, mobil ödeme (DCB), OTP, pazaryeri, mutabakat |
| Payment management | `getCardTokenSecure` ve 3D Secure yönlendirme formu |
| Web SDK | Paycell'in barındırdığı ödeme sayfası: oturum açma, statü sorgulama, iptal, iade |

```
go get github.com/omertheroot/paycell-go
```

Ayrıntılı servis dokümantasyonu: [DOCS.md](DOCS.md).

## Örnekler

Her biri tek bir akışı gösteriyor ve Paycell'in yayımladığı test bilgileriyle
kurulum gerektirmeden çalışıyor.

| Örnek | Ne gösteriyor |
| --- | --- |
| [`basic-payment`](examples/basic-payment) | en kısa yol: token, ödeme, sorgu, iptal |
| [`3d-secure`](examples/3d-secure) | tarayıcıdaki yönlendirme akışı, localhost'ta |
| [`card-token-browser`](examples/card-token-browser) | kart numarası sunucunuza hiç uğramadan token alma |
| [`stored-cards`](examples/stored-cards) | cüzdana kart kaydetme, OTP'li listeleme, id ile ödeme |
| [`refund`](examples/refund) | iptal mi iade mi, kısmi iade, gerçekten işleyeni sayma |
| [`mobile-payment`](examples/mobile-payment) | mobil ödeme (DCB), OTP ve `provisionAll` |
| [`web-sdk`](examples/web-sdk) | Paycell'in barındırdığı ödeme sayfası ve statü sorgusu |

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

`3d-secure` ve `card-token-browser` birer web sunucusu açar, tarayıcıdan
girilir. `web-sdk` yayımlanan bilgilerde olmayan bir `TerminalCode` ister.
Sandbox 3D şifresi `123456`.

## Kurulum

Bilgileri Paycell entegrasyon sürecinde veriyor. Provizyon, iptal ve statü
servislerini çağıracak her sunucunun IP'sinin Paycell tarafında
tanımlanması gerekiyor.

```go
c, err := paycell.New(paycell.Config{
    ApplicationName:     "PAYCELLTEST",
    ApplicationPassword: "...",
    SecureCode:          "...",
    MerchantCode:        "9998",
    TerminalCode:        "12345",       // yalnızca Web SDK için
    DefaultMSISDN:       "905381234567",
    ReferencePrefix:     "123",         // Paycell'in size verdiği ilk haneler
    Environment:         paycell.Test,  // ya da paycell.Production
})
```

Bütün adresler değiştirilebilir (`ProvisionURL`, `PaymentURL`, `CardTokenURL`,
`CancelURL`, `SDKInitURL`, `SDKHomeURL`). Bazı üye işyerlerine farklı sunucular
tanımlanıyor. Prod'da kart token'ı `epayment-gtm.turkcell.com.tr`, 3D sayfası
`epayment.turkcell.com.tr` üzerinde; ikisi de varsayılan.

## Ödeme almak

```go
res, err := c.Pay(ctx, paycell.PayParams{
    Card:            paycell.Card{Number: "5200190006338608", ExpireMonth: "01", ExpireYear: "30", CVC: "410"},
    Amount:          1525, // 15,25 TL, tutarlar her zaman kuruş cinsinden
    ReferenceNumber: c.NewReferenceNumber(),
})
```

`Pay` kartı token'a çevirip provizyonu tek çağrıda alır. Referans numarasını
siparişle birlikte saklayın. Sonraki her `Inquire`, `Reverse` ve `Refund`
çağrısı bu numarayı ister.

Ön otorizasyon aynı çağrının farklı ödeme tipiyle yapılan hâli: önce
`c.PreAuth`, sonra ön otorizasyonun referansını `OriginalReferenceNumber`
alanına koyarak `c.PostAuth`.

## 3D Secure

Bankaya POST'u müşterinin tarayıcısı atmak zorunda, bu yüzden akış
yönlendirmenin iki yanına bölünüyor.

```go
// yönlendirmeden önce
start, err := c.Start3D(ctx, params, "https://magaza.example/paycell/callback")
save(start)                        // start.ReferenceNumber ile saklayın
w.Write([]byte(start.Form.HTML())) // kendi kendini gönderen form

// müşteri döndükten sonra
res, err := c.Complete3D(ctx, load(r), params)
```

`Complete3D` önce oturum sonucunu sorgular, ödemeyi yalnızca müşteri
doğrulamayı geçtiyse alır. Geçemediyse `*ErrNotVerified` döner ve provizyon
istenmez. Callback'in kendisi doğrulanmış bir kaynak değil, hiçbir şeyin
kanıtı sayılmamalı.

Müşteri callback'e `POST` ile döner. Paycell yalnızca uygulamanıza tanımlı
adreslere geri gönderiyor; callback adresinizi Paycell'e tanımlatın, yoksa
müşteri "3D Yönlendirme Hatası" sayfasında kalır. Sandbox'ta `localhost`
geçiyor.

## Kart saklama

Paycell kartı yalnızca müşterinin 3D'den geçtiği bir oturumla saklıyor.
Kartın ne zaman saklanmasını istediğinize göre iki yol var:

```go
terms, _ := c.GetTermsOfServiceContent(ctx, nil)

// 3D'li ödemeden hemen sonra, ödemenin token'ı ve oturumuyla
c.Complete3D(ctx, start, params)
c.RegisterCard(ctx, &paycell.RegisterCardRequest{
    CardToken:       start.CardToken,
    ThreeDSessionID: start.ThreeDSessionID,
    Alias:           "İş kartı",
    EulaID:          terms.ID(),
})

// ya da ödeme olmadan: 1 kuruşluk oturum, 3D, provizyon yok
sess, _ := c.GetThreeDSession(ctx, &paycell.ThreeDSessionRequest{CardToken: token.CardToken, Amount: 1})
// ... müşteri 3D'den geçer, GetThreeDSessionResult ile doğrulayın ...
c.RegisterCard(ctx, &paycell.RegisterCardRequest{
    CardToken: token.CardToken, ThreeDSessionID: sess.ThreeDSessionID, EulaID: terms.ID(),
})
```

Kart zaten cüzdandaysa `3005` döner ve yanıtta `cardId` yine dolu gelir.

Saklı kartları listelemek SMS OTP'ye bağlı. Paycell'in belgelediği yol
`GetPaymentMethodList`: OTP hangi referans numarasıyla gönderilip
doğrulandıysa liste o numarayla açılır.

```go
ref := c.NewReferenceNumber()
otp, _ := c.SendOTP(ctx, &paycell.SendOTPRequest{
    ReferenceNumber: ref,
    ExtraParameters: paycell.CardListOTPParams(), // VALIDATION_TYPE=PM_LIST
})
c.ValidateOTP(ctx, &paycell.ValidateOTPRequest{
    OTP: musteriningirdigiKod, Token: otp.Token, ReferenceNumber: ref,
    ExtraParameters: paycell.CardListOTPParams(),
})
methods, err := c.GetPaymentMethodList(ctx, &paycell.PaymentMethodListRequest{ReferenceNumber: ref})

for _, card := range methods.Cards() {
    c.Pay(ctx, paycell.PayParams{CardID: card.CardID, Amount: 1525})
}
```

Doğrulanmış referans OTP'nin kendisinden çok daha uzun yaşıyor: sandbox beş
gün önce doğrulanmış bir referansla listeyi hâlâ veriyordu. Referans
düştüğünde `3110` gelir, o zaman yeni bir OTP isteyin.

`GetPaymentMethods` ve `GetCards` eski listeler, Paycell'in güncel
dokümanında yoklar. Test kanalında OTP istemeden cevap verdiler, ama kart
listesi için `GetPaymentMethodList` kullanın: Paycell'in kabul testleri onu
soruyor. Mobil ödeme limiti `GetPaymentMethods`'ta. Saklı kartları
`UpdateCard` ve `DeleteCard` ile yönetirsiniz.

## Hatalar

Her çağrı hem yanıtı hem hatayı döner. `*paycell.Error`, Paycell'in sıfırdan
farklı bir `responseCode` verdiği anlamına gelir. Yanıt yapısı yine dolu
gelir, yani kodu görüp ona göre dallanabilirsiniz.

```go
var pErr *paycell.Error
if errors.As(err, &pErr) {
    log.Print(pErr.Code, pErr.Description)
}

// adı konulmaya değer iki kod
paycell.IsAlreadySettled(err) // 2029: iptal için geç kalındı, tamamını iade edin
paycell.IsOTPRequired(err)    // 3110: önce OTP doğrulaması gerekiyor
```

Bu paketin modellemediği alanlar `res.Raw` içinde durur.
`c.Call(ctx, "birServis/", in, &out)` ile herhangi bir provizyon servisine
istediğiniz gövdeyi gönderebilirsiniz.

## İmzalar

İki ayrı algoritma var ve birbirinin yerine geçmiyorlar.

Provizyon ve kart token'ı, her iki aşamada da büyük harfe çevrilir:

```
securityData = base64(sha256(UPPER(applicationPwd + applicationName)))
hashData     = base64(sha256(UPPER(applicationName + transactionId +
               transactionDateTime + [responseCode] + [cardToken] +
               secureCode + securityData)))
```

Web SDK oturum açma, büyük harfe çevrilmez, terminal koduna dayanır:

```
securityData = base64(sha256(secureCode + terminalCode))
hashData     = base64(sha256(paymentReferenceNumber + terminalCode + amount +
               currency + paymentSecurity + hostAccount +
               installmentPlan… + securityData))
```

İkisi de paket içinde hallediliyor ve ikisi de testlerde sabit değerlere
bağlandı. `RequestHash` ile `VerifyCardTokenResponse` dışarı açık, çünkü
Paycell'in tercih ettiği akışta kart numarası sizin sunucunuza hiç uğramaz:

```go
req := c.BuildCardTokenRequest(card) // imzalı gövde, istemciye verilebilir
// tarayıcı bunu c.CardTokenURL() adresine POST eder ve yanıtı size gönderir
err := c.VerifyCardTokenResponse(req.Header.TransactionID, res)
```

`GetCardTokenSecure` bu doğrulamayı kendiliğinden yapar. Yapmazsanız istemcinin
uydurduğu bir kart token'ını hiçbir şey engellemez.

## Kurallar

- **Tutarlar kuruş cinsinden.** 15,25 TL için `1525`. `Amount` alanı Paycell'in
  beklediği gibi string olarak gider, gelirken hem string hem sayı kabul eder,
  çünkü servise göre ikisi de geliyor. Ondalıklı bir değer hata verir, çünkü
  yuvarlamak yanlış tutarı kaydeder.
- **Başarı `responseCode == "0"` demek.** 3D oturum sonucunda `Verified()`,
  Web SDK statüsünde `Paid()` bakılır.
- **Aynı gün iptal, sonrası iade.** `2029` dönen bir iptalin tamamı iade
  edilmeli.
- **Referans numarası 20 haneli ve sayısal**, ilk haneleri uygulamanıza
  Paycell tarafından verilir. `c.NewReferenceNumber()` üretir; sipariş
  numaranız sığıyorsa kendi numaranızı verin.
- **Para birimi provizyon servislerinde `"TRY"`, Web SDK'da `"99"`.** İkisi de
  varsayılan olarak dolduruluyor.
- Timeout aldığınızda tekrar denemeden önce `Inquire` ya da
  `QueryPaymentStatus` çağırın. Sizin tarafınızdaki timeout Paycell tarafında
  ne olduğunu söylemez.

## Servisler

Provizyon servisleri, `Config.ProvisionURL` üzerinde:

`Provision` · `PreAuth` · `PostAuth` · `ProvisionAll` · `ProvisionForMarketPlace` ·
`Inquire` · `InquireAll` · `Reverse` · `Refund` · `RefundAll` ·
`GetThreeDSession` · `GetThreeDSessionResult` ·
`GetCards` · `RegisterCard` · `UpdateCard` · `DeleteCard` ·
`GetTermsOfServiceContent` · `GetCardBinInformation` · `GetPaymentMethods` ·
`GetPaymentMethodList` · `OpenMobilePayment` · `SendOTP` · `ValidateOTP` ·
`GetProvisionHistory` · `SummaryReconciliation`

Payment management, `Config.PaymentURL` ve `Config.CardTokenURL` üzerinde:

`GetCardTokenSecure` · `BuildCardTokenRequest` · `ThreeDSecureForm`

Web SDK, `Config.SDKInitURL` ve `Config.CancelURL` üzerinde:

`InitSession` · `QueryPaymentStatus` · `ReversePayment` · `RefundPayment` ·
`SDKSummaryReconciliation`

Kolaylık çağrıları: `Pay` · `Start3D` · `Complete3D` · `Call`

Her birinin çalışan örneği `example_test.go` içinde, uçtan uca akışlar
[`examples/`](examples) altında. Alan alan referans için
[DOCS.md](DOCS.md).

## Test

Birim testleri `httptest` kullanır, ne bilgi ne ağ ister:

```
go test ./...
```

Entegrasyon testleri gerçek sandbox'a gider:

```
go test -tags integration -v
```

Varsayılan olarak Paycell'in yayımladığı test bilgilerini (`PAYCELLTEST`,
üye işyeri `9998`, eulaId `17`) kullanır; `PAYCELL_APP_NAME`,
`PAYCELL_APP_PASSWORD`, `PAYCELL_SECURE_CODE`, `PAYCELL_MERCHANT_CODE`,
`PAYCELL_EULA_ID`, `PAYCELL_PREFIX` ve `PAYCELL_MSISDN` ile değiştirilir.
Çözümleme hatası testi düşürür, çünkü bir yapının yanlış olduğu anlamına
gelir; Paycell'in döndüğü hata kodu yalnızca loglanır, çünkü sandbox'ın
beklenmedik bir durumda olduğunu gösterir. Ödemeler 1 ile 5 TL arası ve
hepsi iptal ya da iade ediliyor; kaydedilen test kartı sonunda siliniyor.

Test kartı: Paycell'in MOCK BANK kartı `1000000000000016`, `12/26`, CVC
`000`. Sandbox'ta gerçekten çekim yapan tek kart bu; banka test kartları 3D'den
geçiyor ama provizyonda `4000` alıyor. 3D sayfasının kodu `123456`, Paycell
OTP'si `111111`.

3D'li testler tarayıcı açmaz: sandbox'ın 3D sayfası bir simülasyon, kodu
JavaScript ile kontrol edip `action=success` gönderiyor, testler de aynısını
yapıyor. Callback olarak `localhost` kullanılıyor, sandbox'ta geçen tanımlı
adres o.

Ortak `PAYCELLTEST` hesabında `getPaymentMethodList` `1002` dönüyor ve
`520019` için BIN kaydı yok; test bunları loglayıp geçiyor. Kendi test
kanalınızla bütün testler sonuna kadar koşar.

### Sandbox'ın değiştirdikleri

Hiçbir açık dokümanda geçmeyen üç istek kuralı, canlı isteklerde alan alan
eleyerek bulundu:

- `getThreeDSession` gönderilen `referenceNumber` alanını `2032` ile,
  `getThreeDSessionResult` ise `2041` ile reddediyor. Bu alan arkadan gelen
  provizyona ait ve paket onu orada tutuyor.
- `openMobilePayment` `referenceNumber` istiyor (`2007`) ve saklı kart listesi
  gibi OTP'ye bağlı.
- `eulaId`, `getCards` yanıtında tırnaklı, `getTermsOfServiceContent`
  yanıtında tırnaksız geliyor; `binValue` her zaman tırnaksız. Bu tür alanlar
  ikisini de kabul eden `FlexString` tipinde.

Bir üye işyeri test kanalında ayrıca görülenler:

- Kart yalnızca 3D'den geçmiş bir oturumla saklanıyor (`3427`, ortak
  sandbox'ta `4003`). `saveCard` hiçbir şey saklamıyor. Abone başına sınır
  iki kart (`3002`).
- `getPaymentMethodList` OTP'nin gönderildiği `referenceNumber` ile açılıyor,
  doğrulanmış referans günlerce geçerli kalıyor.
- Paycell tanımsız bir callback adresine dönmüyor: müşteri "3D Yönlendirme
  Hatası" sayfasında kalıyor, oturum `90013` ile düşüyor.
- `isPost3DResult` açıkken sonuç callback'e form olarak `POST` ediliyor,
  kapalıyken dönüş `GET` ve adrese hiçbir şey eklenmiyor.
- `getPrepaidCommission` ve `queryOrderVpf` dokümanda var ama iki sandbox'ta
  da `1002` döndü; paket bu yüzden henüz ikisini modellemiyor.

PREAUTH ve POSTAUTH ortak sandbox üye işyerinde `4015` ve `1024` dönüyor, oysa
aynı kart ve alanlarla SALE çalışıyor. Bu, istek yapısından çok üye işyeri
tanımına benziyor; kendi test üye işyerinizde deneyin. Web SDK, yayımlanan
bilgilerde olmayan bir `TerminalCode` istediği için `InitSession` yalnızca
birim testleriyle kontrol edildi.

## Kaynaklar

Paycell SDK entegrasyon dokümanı, Paycell'in resmî WooCommerce, PrestaShop ve
OpenCart eklentileri, `omnipay-paycell` ve `omnipay-paycell-sdk` PHP
kütüphaneleri ve mevcut iki Go uygulaması okunarak yazıldı, sonra sandbox'a
karşı düzeltildi.

Sandbox'ta çalıştırılamayan iki servis `ProvisionForMarketPlace` ve
`GetProvisionHistory`. Alan adları üçüncü parti kodlardan geliyor, kendi
entegrasyon dokümanınızla karşılaştırın. Modellenmemiş alanlar her hâlükârda
`res.Raw` içinde duruyor.

## Lisans

MIT
