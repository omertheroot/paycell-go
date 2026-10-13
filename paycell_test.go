package paycell

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testClient pins the credentials, clock and transaction ids so requests come
// out the same every run.
func testClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := New(Config{
		ApplicationName:     "PAYCELLTEST",
		ApplicationPassword: "PaycellTestPassword",
		SecureCode:          "0000000000000000",
		MerchantCode:        "9998",
		TerminalCode:        "12345",
		EulaID:              "17",
		DefaultMSISDN:       "905380521479",
		ReferencePrefix:     "100",
		ProvisionURL:        srv.URL + "/provision/services/restful/getCardToken/",
		PaymentURL:          srv.URL + "/paymentmanagement/rest/",
		CancelURL:           srv.URL + "/cancelrestful/cancelService/",
		SDKInitURL:          srv.URL + "/api/session/init",
		SDKHomeURL:          srv.URL + "/home/",
		HTTPClient:          srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC) }
	c.newID = func() string { return "12345678901234567890" }
	return c
}

// jsonServer keeps the last request body and answers with body.
func jsonServer(t *testing.T, body string) (*httptest.Server, *map[string]any, *string) {
	t.Helper()
	got := map[string]any{}
	path := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("server: bad request json: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &got, &path
}

// The hash is where Paycell integrations usually come apart, so these vectors
// are pinned. Credentials below are made up, not the sandbox ones.
func TestHashVectors(t *testing.T) {
	c := testClient(t, httptest.NewServer(http.NotFoundHandler()))

	if got, want := c.securityData(), "d+RurbEgrO1HElWOfDW6I5/nCDmnUWDs4yYRjjDuDN0="; got != want {
		t.Errorf("securityData = %q, want %q", got, want)
	}
	if got, want := c.RequestHash("12345678901234567890", "20240101120000000"),
		"rNk2qw+L/Epzkxqe7J+LhRuBXA34Gv9VMt9PgqMKzkE="; got != want {
		t.Errorf("RequestHash = %q, want %q", got, want)
	}
	if got, want := c.ResponseHash("12345678901234567890", "20240101120001000", "0", "tok-123"),
		"ry5Nvyt5G9WuPz+gFt1KPkY0gCrEHrEUp6TeNnlGRkk="; got != want {
		t.Errorf("ResponseHash = %q, want %q", got, want)
	}
	// Lower case input must hash the same: the algorithm uppercases first.
	if hash("abc") != hash("ABC") {
		t.Error("hash is case sensitive, it must uppercase its input")
	}
}

// Different algorithm on the SDK side. No uppercase, security data out of
// secureCode+terminalCode, plan ordered by lineId.
func TestSDKHash(t *testing.T) {
	c := testClient(t, httptest.NewServer(http.NotFoundHandler()))

	p := SDKPayment{
		Amount:                 1000,
		Currency:               SDKCurrencyTRY,
		PaymentReferenceNumber: "10000000000000000001",
		PaymentSecurity:        ThreeDSecure,
		InstallmentPlan: []Installment{
			{LineID: 2, PaymentMethodType: MethodCreditCard, CardBrand: "BONUS", Count: 3, Amount: 1000},
			{LineID: 1, PaymentMethodType: MethodDCB, CardBrand: "TURKCELL", Count: 1, Amount: 1000},
		},
	}
	if got, want := c.sdkHashData(p, "a@b.com"), "Fa3kjC9dawOCL9ZptFJ0H4Px/yU3fma6gWp+G8RzwHU="; got != want {
		t.Errorf("sdkHashData = %q, want %q", got, want)
	}
	if sdkHash("abc") == sdkHash("ABC") {
		t.Error("the SDK hash must not uppercase its input")
	}
	// Ordering the plan must not depend on the caller's slice order.
	if p.InstallmentPlan[0].LineID != 2 {
		t.Error("sdkHashData reordered the caller's slice")
	}
}

func TestAmountJSON(t *testing.T) {
	b, err := json.Marshal(Amount(1525))
	if err != nil || string(b) != `"1525"` {
		t.Fatalf("marshal = %s, %v; want \"1525\"", b, err)
	}
	for _, tc := range []struct {
		in   string
		want Amount
	}{
		{`"1525"`, 1525},
		{`1525`, 1525},
		{`""`, 0},
		{`null`, 0},
	} {
		var a Amount
		if err := json.Unmarshal([]byte(tc.in), &a); err != nil {
			t.Errorf("unmarshal %s: %v", tc.in, err)
			continue
		}
		if a != tc.want {
			t.Errorf("unmarshal %s = %d, want %d", tc.in, a, tc.want)
		}
	}
	// Anything that isn't whole minor units has to fail loudly. Rounding
	// books the wrong amount.
	for _, bad := range []string{`"abc"`, `"15.25"`, `15.25`} {
		var a Amount
		if err := json.Unmarshal([]byte(bad), &a); err == nil {
			t.Errorf("unmarshal of %s must fail, got %d", bad, a)
		}
	}
	if got := Amount(1525).TRY(); got != 15.25 {
		t.Errorf("TRY() = %v, want 15.25", got)
	}
}

func TestTransactionDateTime(t *testing.T) {
	got := TransactionDateTime(time.Date(2024, 3, 9, 8, 40, 56, 197*int(time.Millisecond), time.UTC))
	if got != "20240309084056197" {
		t.Errorf("TransactionDateTime = %q, want 20240309084056197", got)
	}
	if len(got) != 17 {
		t.Errorf("length = %d, want 17", len(got))
	}
}

func TestNewReferenceNumber(t *testing.T) {
	c := testClient(t, httptest.NewServer(http.NotFoundHandler()))
	ref := c.NewReferenceNumber()
	if len(ref) != 20 {
		t.Errorf("reference number %q is %d digits, want 20", ref, len(ref))
	}
	if !strings.HasPrefix(ref, "100") {
		t.Errorf("reference number %q does not start with the application prefix", ref)
	}
	if ref == c.NewReferenceNumber() {
		t.Error("reference numbers repeat")
	}
}

func TestProvisionRequestShape(t *testing.T) {
	srv, got, path := jsonServer(t, `{
		"responseHeader":{"transactionId":"12345678901234567890","responseDateTime":"20240101120001000","responseCode":"0","responseDescription":"Success"},
		"orderId":"551952788718101710473129","approvalCode":"646174","acquirerBankCode":"111","issuerBankCode":"111","reconciliationDate":"20240101"}`)
	c := testClient(t, srv)

	res, err := c.Provision(context.Background(), &ProvisionRequest{
		CardToken:       "tok-123",
		Amount:          2351,
		ReferenceNumber: "10000000000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OrderID != "551952788718101710473129" || res.ApprovalCode != "646174" {
		t.Errorf("response not decoded: %+v", res)
	}
	if res.ReferenceNumber != "10000000000000000001" {
		t.Errorf("reference number lost: %q", res.ReferenceNumber)
	}
	if *path != "/provision/services/restful/getCardToken/provision/" {
		t.Errorf("posted to %q", *path)
	}

	// Defaults the caller did not set.
	if (*got)["paymentType"] != "SALE" || (*got)["currency"] != "TRY" {
		t.Errorf("missing defaults: %v", *got)
	}
	if (*got)["merchantCode"] != "9998" || (*got)["msisdn"] != "905380521479" {
		t.Errorf("merchant defaults not applied: %v", *got)
	}
	// Amounts go out as strings.
	if (*got)["amount"] != "2351" {
		t.Errorf("amount = %#v, want the string \"2351\"", (*got)["amount"])
	}
	h, _ := (*got)["requestHeader"].(map[string]any)
	if h["applicationName"] != "PAYCELLTEST" || h["applicationPwd"] != "PaycellTestPassword" {
		t.Errorf("credentials missing from header: %v", h)
	}
	if h["transactionDateTime"] != "20240101120000000" || h["transactionId"] != "12345678901234567890" {
		t.Errorf("header stamp = %v", h)
	}
	if h["clientIPAddress"] != "127.0.0.1" {
		t.Errorf("client ip = %v", h["clientIPAddress"])
	}
}

func TestProvisionFailureIsAnError(t *testing.T) {
	srv, _, _ := jsonServer(t, `{"responseHeader":{"responseCode":"2029","responseDescription":"Islem bulunamadi","transactionId":"t1"}}`)
	c := testClient(t, srv)

	res, err := c.Provision(context.Background(), &ProvisionRequest{CardToken: "t", Amount: 100})
	var pErr *Error
	if !errors.As(err, &pErr) {
		t.Fatalf("err = %v, want *paycell.Error", err)
	}
	if pErr.Code != "2029" || pErr.Description != "Islem bulunamadi" {
		t.Errorf("error = %+v", pErr)
	}
	// The response is still filled in, so callers can act on the code.
	if res.ResponseHeader.ResponseCode != "2029" {
		t.Error("response discarded on failure")
	}
	if len(res.Raw) == 0 {
		t.Error("raw body not kept")
	}
}

func TestGetCardTokenSecureVerifiesHash(t *testing.T) {
	// The golden response hash for transaction 12345678901234567890 at
	// 20240101120001000, code 0, token tok-123.
	const good = `{"header":{"transactionId":"12345678901234567890","responseDateTime":"20240101120001000","responseCode":"0","responseDescription":"Success"},` +
		`"cardToken":"tok-123","hashData":"ry5Nvyt5G9WuPz+gFt1KPkY0gCrEHrEUp6TeNnlGRkk="}`

	srv, got, path := jsonServer(t, good)
	c := testClient(t, srv)

	res, err := c.GetCardTokenSecure(context.Background(), Card{
		Number: "5200 1900 0633 8608", ExpireMonth: "01", ExpireYear: "30", CVC: "410",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.CardToken != "tok-123" {
		t.Errorf("card token = %q", res.CardToken)
	}
	if *path != "/paymentmanagement/rest/getCardTokenSecure" {
		t.Errorf("posted to %q", *path)
	}
	if (*got)["creditCardNo"] != "5200190006338608" {
		t.Errorf("card number not normalised: %v", (*got)["creditCardNo"])
	}
	if (*got)["hashData"] != "rNk2qw+L/Epzkxqe7J+LhRuBXA34Gv9VMt9PgqMKzkE=" {
		t.Errorf("request hash = %v", (*got)["hashData"])
	}
	// The card token header must not leak the application password.
	h, _ := (*got)["header"].(map[string]any)
	if _, leaked := h["applicationPwd"]; leaked {
		t.Error("application password sent to the card token endpoint")
	}

	// Tampered token, so the signature no longer lines up.
	bad := strings.Replace(good, `"cardToken":"tok-123"`, `"cardToken":"attacker-token"`, 1)
	srv2, _, _ := jsonServer(t, bad)
	c2 := testClient(t, srv2)
	if _, err := c2.GetCardTokenSecure(context.Background(), Card{Number: "5200190006338608"}); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("err = %v, want ErrHashMismatch", err)
	}
}

func TestThreeDForm(t *testing.T) {
	c := testClient(t, httptest.NewServer(http.NotFoundHandler()))
	f := c.ThreeDSecureForm(ThreeDFormParams{
		ThreeDSessionID: "sess-1",
		CallbackURL:     `https://shop.example/cb?ref=1&x="y"`,
	})
	html := f.HTML()
	if !strings.Contains(html, `action="`+c.ThreeDSecureURL()+`"`) {
		t.Errorf("form action missing:\n%s", html)
	}
	if !strings.Contains(html, `name="threeDSessionId" value="sess-1"`) {
		t.Errorf("session id missing:\n%s", html)
	}
	if strings.Contains(html, `value="https://shop.example/cb?ref=1&x="y""`) {
		t.Error("callback url was not escaped, the form is injectable")
	}
	if !strings.Contains(html, "isPoint") || !strings.Contains(html, "isPost3DResult") {
		t.Errorf("flags missing:\n%s", html)
	}
}

// Nobody who failed verification gets charged.
func TestComplete3DRefusesUnverified(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"responseHeader":{"responseCode":"0","responseDescription":"Success"},
			"mdStatus":"0","mdErrorMessage":"Not authenticated",
			"threeDOperationResult":{"threeDResult":"1","threeDResultDescription":"Dogrulama basarisiz"}}`))
	}))
	defer srv.Close()
	c := testClient(t, srv)

	start := &ThreeDStart{ThreeDSessionID: "sess-1", CardToken: "tok-1", ReferenceNumber: "10000000000000000001"}
	_, err := c.Complete3D(context.Background(), start, PayParams{Amount: 100})

	var nv *ErrNotVerified
	if !errors.As(err, &nv) {
		t.Fatalf("err = %v, want *ErrNotVerified", err)
	}
	for _, p := range paths {
		if strings.Contains(p, "getCardToken/provision") {
			t.Fatal("payment was provisioned despite failed 3D verification")
		}
	}
}

// provisionAll is the payment service in Paycell's docs, so both flows use it.
func TestPayGoesThroughProvisionAll(t *testing.T) {
	srv, got, path := jsonServer(t, `{"responseHeader":{"responseCode":"0"},"orderId":"o1"}`)
	c := testClient(t, srv)

	if _, err := c.Pay(context.Background(), PayParams{CardID: "card-1", Amount: 100}); err != nil {
		t.Fatal(err)
	}
	if *path != "/provision/services/restful/getCardToken/provisionAll/" {
		t.Errorf("posted to %q", *path)
	}
	if (*got)["paymentMethodType"] != MethodCreditCard || (*got)["cardId"] != "card-1" {
		t.Errorf("request = %v", *got)
	}
	// Paycell wants one card reference, the other left out entirely.
	if _, ok := (*got)["cardToken"]; ok {
		t.Errorf("cardToken sent alongside cardId: %v", *got)
	}
}

func TestPayNeedsACard(t *testing.T) {
	c := testClient(t, httptest.NewServer(http.NotFoundHandler()))
	if _, err := c.Pay(context.Background(), PayParams{Amount: 100}); !errors.Is(err, ErrNoCard) {
		t.Fatalf("err = %v, want ErrNoCard", err)
	}
}

func TestInitSessionBuildsTrackingURL(t *testing.T) {
	srv, got, path := jsonServer(t, `{"trackingId":"trk-1"}`)
	c := testClient(t, srv)

	res, err := c.InitSession(context.Background(), SDKParams{
		Amount:                 1000,
		PaymentReferenceNumber: "10000000000000000001",
		HostAccount:            "a@b.com",
		ReturnURL:              "https://shop.example/done?ref=[paymentReferenceNumber]",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.TrackingURL != srv.URL+"/home/trk-1" {
		t.Errorf("tracking url = %q", res.TrackingURL)
	}
	if *path != "/api/session/init" {
		t.Errorf("posted to %q", *path)
	}
	if (*got)["returnUrl"] != "https://shop.example/done?ref=10000000000000000001" {
		t.Errorf("returnUrl placeholder not substituted: %v", (*got)["returnUrl"])
	}
	if (*got)["hashData"] == "" || (*got)["hashData"] == nil {
		t.Error("session init sent without a hash")
	}
	payment, _ := (*got)["payment"].(map[string]any)
	if payment["currency"] != SDKCurrencyTRY || payment["paymentSecurity"] != ThreeDSecure {
		t.Errorf("payment defaults = %v", payment)
	}
	if (*got)["timeoutDuration"] != float64(DefaultTimeout) {
		t.Errorf("timeout = %v", (*got)["timeoutDuration"])
	}
	hdr, _ := (*got)["requestHeader"].(map[string]any)
	merchant, _ := hdr["merchant"].(map[string]any)
	if merchant["terminalCode"] != "12345" {
		t.Errorf("terminal code missing: %v", hdr)
	}
}

func TestInitSessionWithoutTrackingIDFails(t *testing.T) {
	srv, _, _ := jsonServer(t, `{"responseHeader":{"responseCode":"5001","responseDescription":"Invalid parameter"}}`)
	c := testClient(t, srv)
	if _, err := c.InitSession(context.Background(), SDKParams{Amount: 100}); err == nil {
		t.Fatal("a session without a tracking id must be an error")
	}
}

func TestQueryPaymentStatus(t *testing.T) {
	srv, got, path := jsonServer(t, `{
		"responseHeader":{"responseCode":"0","responseDescription":"Success"},
		"status":"0","statusExplanation":"Islem basarili","amount":10000,"currency":"99",
		"installmentCount":1,"paymentSecurity":"NON_THREED_SECURE","reconcilationDate":"20180718",
		"paymentReferenceNumber":"3fe058d9306e90ac",
		"paymentMethod":{"paymentMethodId":"886311","paymentMethodNumber":"435508******4358","paymentMethodType":"CREDITCARD"}}`)
	c := testClient(t, srv)

	res, err := c.QueryPaymentStatus(context.Background(), "3fe058d9306e90ac")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Paid() {
		t.Error("status 0 must read as paid")
	}
	if res.Amount != 10000 {
		t.Errorf("amount = %d, want 10000 (a numeric amount in the response)", res.Amount)
	}
	if res.ReconciliationDate != "20180718" {
		t.Errorf("reconcilationDate not mapped: %q", res.ReconciliationDate)
	}
	if res.PaymentMethod.PaymentMethodNumber != "435508******4358" {
		t.Errorf("payment method = %+v", res.PaymentMethod)
	}
	if *path != "/cancelrestful/cancelService/queryPaymentStatus/" {
		t.Errorf("posted to %q", *path)
	}
	if (*got)["originalPaymentReferenceNumber"] != "3fe058d9306e90ac" {
		t.Errorf("reference not sent: %v", *got)
	}
}

func TestHTTPErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>gateway down</html>"))
	}))
	defer srv.Close()
	c := testClient(t, srv)

	_, err := c.Provision(context.Background(), &ProvisionRequest{CardToken: "t", Amount: 1})
	if err == nil || !strings.Contains(err.Error(), "http 502") {
		t.Fatalf("err = %v, want an http 502 error", err)
	}
}

func TestNewValidatesCredentials(t *testing.T) {
	if _, err := New(Config{ApplicationPassword: "p", SecureCode: "s"}); err == nil {
		t.Error("a missing application name must be rejected")
	}
	c, err := New(Config{ApplicationName: "n", ApplicationPassword: "p", SecureCode: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Config().ProvisionURL; got != testProvisionURL {
		t.Errorf("default provision url = %q", got)
	}
	prod, _ := New(Config{ApplicationName: "n", ApplicationPassword: "p", SecureCode: "s", Environment: Production})
	if got := prod.Config().ProvisionURL; got != prodProvisionURL {
		t.Errorf("production provision url = %q", got)
	}
	// A base URL given without its trailing slash must still join correctly.
	custom, _ := New(Config{ApplicationName: "n", ApplicationPassword: "p", SecureCode: "s", ProvisionURL: "https://x.example/getCardToken"})
	if got := custom.Config().ProvisionURL; got != "https://x.example/getCardToken/" {
		t.Errorf("custom provision url = %q", got)
	}
}

// Nested embedding still has to pick up the header and merchant code without
// losing its own fields.
func TestProvisionAllShape(t *testing.T) {
	srv, got, path := jsonServer(t, `{"responseHeader":{"responseCode":"0"},"orderId":"o1"}`)
	c := testClient(t, srv)

	if _, err := c.ProvisionAll(context.Background(), &ProvisionAllRequest{
		PaymentMethodType: MethodDCB,
		ProvisionRequest:  ProvisionRequest{Amount: 1525, ReferenceNumber: "10000000000000000001"},
	}); err != nil {
		t.Fatal(err)
	}
	if *path != "/provision/services/restful/getCardToken/provisionAll/" {
		t.Errorf("posted to %q", *path)
	}
	if (*got)["paymentMethodType"] != "DCB" {
		t.Errorf("payment method = %v", (*got)["paymentMethodType"])
	}
	if (*got)["merchantCode"] != "9998" || (*got)["paymentType"] != "SALE" {
		t.Errorf("defaults not applied through the embedded request: %v", *got)
	}
	h, _ := (*got)["requestHeader"].(map[string]any)
	if h["applicationName"] != "PAYCELLTEST" {
		t.Errorf("header not filled through the embedded request: %v", h)
	}
}

// Merchant code yes, subscriber number no.
func TestQueryPaymentStatusSendsNoMSISDN(t *testing.T) {
	srv, got, _ := jsonServer(t, `{"responseHeader":{"responseCode":"0"},"status":"0"}`)
	c := testClient(t, srv)

	if _, err := c.QueryPaymentStatus(context.Background(), "ref-1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*got)["msisdn"]; ok {
		t.Errorf("msisdn sent to queryPaymentStatus: %v", *got)
	}
	if (*got)["merchantCode"] != "9998" {
		t.Errorf("merchant code missing: %v", *got)
	}
}

// First listing gets 3110, the same call with the token goes through.
func TestStoredCardListingIsOTPGated(t *testing.T) {
	var bodies []map[string]any
	reply := `{"responseHeader":{"responseCode":"3110","responseDescription":"OTP dogrulamasi gerekli"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		bodies = append(bodies, got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	c := testClient(t, srv)
	ctx := context.Background()

	_, err := c.GetPaymentMethods(ctx, nil)
	if !IsOTPRequired(err) {
		t.Fatalf("err = %v, want an OTP-required error", err)
	}

	reply = `{"responseHeader":{"responseCode":"0"},"paymentMethods":[
		{"cardId":"c1","maskedCardNo":"435508******4358","alias":"İş kartı","isDefault":true}]}`
	res, err := c.GetPaymentMethods(ctx, &PaymentMethodsRequest{OTPToken: "otp-token-1"})
	if err != nil {
		t.Fatal(err)
	}
	if bodies[1]["otpToken"] != "otp-token-1" {
		t.Errorf("otp token not sent: %v", bodies[1])
	}
	// The list arrives under paymentMethods here, not cardList.
	cards := res.Cards()
	if len(cards) != 1 || cards[0].CardID != "c1" || cards[0].Alias != "İş kartı" {
		t.Fatalf("cards = %+v", cards)
	}
}

// The documented listing: same reference as the OTP, PM_LIST without asking.
func TestPaymentMethodList(t *testing.T) {
	srv, got, path := jsonServer(t, `{"responseHeader":{"responseCode":"0"},"eulaId":"19","cardList":[
		{"cardId":"c1","maskedCardNo":"100000******0016","alias":"Test","isDefault":true,"cardType":"Credit"}],
		"mobilePayment":null,"wallet":{"detailBalances":null,"totalBalances":null}}`)
	c := testClient(t, srv)

	res, err := c.GetPaymentMethodList(context.Background(), &PaymentMethodListRequest{ReferenceNumber: "10000000000000000009"})
	if err != nil {
		t.Fatal(err)
	}
	if *path != "/provision/services/restful/getCardToken/getPaymentMethodList/" {
		t.Errorf("posted to %q", *path)
	}
	if (*got)["referenceNumber"] != "10000000000000000009" || (*got)["msisdn"] != "905380521479" {
		t.Errorf("request = %v", *got)
	}
	params, _ := (*got)["extraParameters"].([]any)
	if len(params) != 1 || params[0].(map[string]any)["value"] != ValidationTypePMList {
		t.Errorf("extraParameters = %v, want PM_LIST", (*got)["extraParameters"])
	}
	if cards := res.Cards(); len(cards) != 1 || cards[0].CardID != "c1" || res.EulaID != "19" {
		t.Errorf("response = %+v", res)
	}

	srv2, _, _ := jsonServer(t, `{"responseHeader":{"responseCode":"3110","responseDescription":"OTP Dogrulanamadi"}}`)
	if _, err := testClient(t, srv2).GetPaymentMethodList(context.Background(), nil); !IsOTPRequired(err) {
		t.Errorf("err = %v, want an OTP-required error", err)
	}
}

// The card-list OTP has to be flagged as one.
func TestCardListOTPParams(t *testing.T) {
	srv, got, path := jsonServer(t, `{"responseHeader":{"responseCode":"0"},"token":"otp-token-1","remainingRetryCount":3}`)
	c := testClient(t, srv)

	res, err := c.SendOTP(context.Background(), &SendOTPRequest{ExtraParameters: CardListOTPParams()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "otp-token-1" || res.RemainingRetryCount != 3 {
		t.Errorf("otp response = %+v", res)
	}
	if *path != "/provision/services/restful/getCardToken/sendOTP/" {
		t.Errorf("posted to %q", *path)
	}
	params, _ := (*got)["extraParameters"].([]any)
	if len(params) != 1 {
		t.Fatalf("extraParameters = %v", (*got)["extraParameters"])
	}
	kv, _ := params[0].(map[string]any)
	if kv["key"] != KeyValidationType || kv["value"] != ValidationTypePMList {
		t.Errorf("validation type = %v", kv)
	}
}

func TestRegisterCard(t *testing.T) {
	srv, got, path := jsonServer(t, `{"responseHeader":{"responseCode":"0"},"cardId":"c1"}`)
	c := testClient(t, srv)

	res, err := c.RegisterCard(context.Background(), &RegisterCardRequest{
		CardToken:       "tok-123",
		Alias:           "İş kartı",
		IsDefault:       true,
		EulaID:          "eula-1",
		ThreeDSessionID: "sess-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.CardID != "c1" {
		t.Errorf("card id = %q", res.CardID)
	}
	if *path != "/provision/services/restful/getCardToken/registerCard/" {
		t.Errorf("posted to %q", *path)
	}
	for k, want := range map[string]any{
		"cardToken": "tok-123", "eulaId": "eula-1", "threeDSessionId": "sess-1",
		"isDefault": true, "merchantCode": "9998", "msisdn": "905380521479",
	} {
		if (*got)[k] != want {
			t.Errorf("%s = %#v, want %#v", k, (*got)[k], want)
		}
	}
}

// Asking for the card to be stored happens on the 3D session.
func TestThreeDSessionCanSaveCard(t *testing.T) {
	srv, got, _ := jsonServer(t, `{"responseHeader":{"responseCode":"0"},"threeDSessionId":"sess-1"}`)
	c := testClient(t, srv)

	if _, err := c.GetThreeDSession(context.Background(), &ThreeDSessionRequest{
		CardToken: "tok-123", Amount: 1525, SaveCard: true,
	}); err != nil {
		t.Fatal(err)
	}
	if (*got)["saveCard"] != true {
		t.Errorf("saveCard not sent: %v", *got)
	}
	if (*got)["target"] != TargetMerchant || (*got)["transactionType"] != TransactionTypeAuth {
		t.Errorf("3D session defaults = %v", *got)
	}
}

func TestTermsOfServiceID(t *testing.T) {
	srv, _, _ := jsonServer(t, `{"responseHeader":{"responseCode":"0"},"eulaId":19,
		"termsOfServiceHtmlContentTR":"<html>TR</html>","termsOfServiceHtmlContentEN":"<html>EN</html>"}`)
	c := testClient(t, srv)

	res, err := c.GetTermsOfServiceContent(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ID() != "19" {
		t.Errorf("ID() = %q, want 19 (the service sends it unquoted)", res.ID())
	}
	if res.TermsOfServiceHTMLContentTR != "<html>TR</html>" {
		t.Errorf("TR terms = %q", res.TermsOfServiceHTMLContentTR)
	}
}

// Some scalars are quoted on one service and bare on the next. eulaId is
// quoted from getCards, bare from getTermsOfServiceContent. binValue is
// always bare.
func TestFlexString(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`"19"`, "19"},
		{`19`, "19"},
		{`520019`, "520019"},
		{`null`, ""},
		{`""`, ""},
	} {
		var f FlexString
		if err := json.Unmarshal([]byte(tc.in), &f); err != nil {
			t.Errorf("unmarshal %s: %v", tc.in, err)
			continue
		}
		if string(f) != tc.want {
			t.Errorf("unmarshal %s = %q, want %q", tc.in, f, tc.want)
		}
	}
	// It must survive a round trip as a string.
	b, err := json.Marshal(FlexString("19"))
	if err != nil || string(b) != `"19"` {
		t.Errorf("marshal = %s, %v", b, err)
	}
}

// BIN lookups answer with a list, and cardType is free text.
func TestCardBinInformation(t *testing.T) {
	srv, got, _ := jsonServer(t, `{"responseHeader":{"responseCode":"0"},"cardBinInformations":[
		{"bankCode":"134","bankName":"DENIZBANK","binValue":520019,"cardBrand":"BONUS",
		 "cardOrganization":"MASTERCARD","cardType":"Credit Card","commercialType":"BIREYSEL","typeId":1}]}`)
	c := testClient(t, srv)

	res, err := c.GetCardBinInformation(context.Background(), &CardBinRequest{BinValue: "520019"})
	if err != nil {
		t.Fatal(err)
	}
	info := res.First()
	if info == nil {
		t.Fatal("no bin record decoded")
	}
	if info.CardBrand != "BONUS" || info.BankName != "DENIZBANK" || info.BankCode != "134" {
		t.Errorf("bin info = %+v", info)
	}
	// binValue arrives unquoted.
	if info.BinValue != "520019" {
		t.Errorf("bin value = %q, want 520019", info.BinValue)
	}
	if info.IsCommercial() {
		t.Error("BIREYSEL must not read as commercial")
	}
	if !info.IsCreditCard() {
		t.Error(`"Credit Card" must read as a credit card`)
	}
	if (&CardBinInfo{CardType: "Debit Card"}).IsCreditCard() {
		t.Error("a debit card must not read as a credit card")
	}
	if (*got)["binValue"] != "520019" {
		t.Errorf("bin not sent: %v", *got)
	}
	if res := (&CardBinResponse{}).First(); res != nil {
		t.Error("an empty bin list must yield nil")
	}
}

func TestIsAlreadySettled(t *testing.T) {
	srv, _, _ := jsonServer(t, `{"responseHeader":{"responseCode":"2029","responseDescription":"Islem gunsonu olmus"}}`)
	c := testClient(t, srv)

	_, err := c.Reverse(context.Background(), &ReverseRequest{OriginalReferenceNumber: "10000000000000000001"})
	if !IsAlreadySettled(err) {
		t.Fatalf("err = %v, want an already-settled error", err)
	}
	if IsOTPRequired(err) {
		t.Error("2029 must not read as an OTP gate")
	}
	if HasCode(errors.New("boom"), "2029") {
		t.Error("a plain error must not match a response code")
	}
}

// 2007 without a reference number, so one gets generated.
func TestOpenMobilePaymentSendsReferenceNumber(t *testing.T) {
	srv, got, path := jsonServer(t, `{"responseHeader":{"responseCode":"0"}}`)
	c := testClient(t, srv)

	if _, err := c.OpenMobilePayment(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if *path != "/provision/services/restful/getCardToken/openMobilePayment/" {
		t.Errorf("posted to %q", *path)
	}
	ref, _ := (*got)["referenceNumber"].(string)
	if len(ref) != 20 {
		t.Errorf("referenceNumber = %q, want 20 digits", ref)
	}
	if (*got)["eulaID"] != "17" {
		t.Errorf("eulaID = %v, want the configured 17", (*got)["eulaID"])
	}
}

// Both 3D calls reject a referenceNumber, 2032 and 2041. It goes on the
// provision instead.
func TestThreeDCallsOmitReferenceNumber(t *testing.T) {
	var bodies []map[string]any
	var paths []string
	replies := []string{
		`{"responseHeader":{"responseCode":"0"},"threeDSessionId":"sess-1"}`,
		`{"responseHeader":{"responseCode":"0"},"threeDOperationResult":{"threeDResult":"0"}}`,
		`{"responseHeader":{"responseCode":"0"},"orderId":"o1"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		bodies = append(bodies, got)
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(replies[min(len(bodies)-1, len(replies)-1)]))
	}))
	defer srv.Close()
	c := testClient(t, srv)
	ctx := context.Background()

	params := PayParams{CardToken: "tok-1", Amount: 100}
	start, err := c.Start3D(ctx, params, "https://shop.example/cb")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bodies[0]["referenceNumber"]; ok {
		t.Errorf("getThreeDSession must not carry a reference number: %v", bodies[0])
	}
	if start.ReferenceNumber == "" {
		t.Error("the reference number must still be tracked for the provision")
	}

	if _, err := c.Complete3D(ctx, start, params); err != nil {
		t.Fatal(err)
	}
	if _, ok := bodies[1]["referenceNumber"]; ok {
		t.Errorf("getThreeDSessionResult must not carry a reference number: %v", bodies[1])
	}
	// The provision does carry it, along with the session id.
	if bodies[2]["referenceNumber"] != start.ReferenceNumber {
		t.Errorf("provision reference = %v, want %q", bodies[2]["referenceNumber"], start.ReferenceNumber)
	}
	if bodies[2]["threeDSessionId"] != "sess-1" {
		t.Errorf("provision session id = %v", bodies[2]["threeDSessionId"])
	}
	if !strings.HasSuffix(paths[2], "/provisionAll/") {
		t.Errorf("3D payment posted to %q", paths[2])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
