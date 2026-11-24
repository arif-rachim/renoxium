package stripe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type captured struct {
	method, path, account, user string
	form                        url.Values
}

func fake(t *testing.T, status int, answer string, got *captured) *Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		if r.Method == http.MethodGet {
			form = r.URL.Query()
		}
		user, _, _ := r.BasicAuth()
		*got = captured{r.Method, r.URL.Path, r.Header.Get("Stripe-Account"), user, form}
		w.WriteHeader(status)
		w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return &Client{Key: "sk_test_x", BaseURL: srv.URL}
}

func TestCreateAccountIsStandardEquivalent(t *testing.T) {
	var path, auth, version string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth, version = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Stripe-Version")
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"id":"acct_1","contact_email":"owner@shop.ae"}`))
	}))
	t.Cleanup(srv.Close)
	c := &Client{Key: "sk_test_x", BaseURL: srv.URL}
	a, err := c.CreateAccount(context.Background(), "owner@shop.ae", "AE")
	if err != nil || a.ID != "acct_1" {
		t.Fatal(a, err)
	}
	// Accounts v2: Stripe refuses POST /v1/accounts for new Connect integrations.
	if path != "/v2/core/accounts" || auth != "Bearer sk_test_x" || version != APIVersionV2 {
		t.Fatalf("%s %s %s", path, auth, version)
	}
	raw, _ := json.Marshal(body)
	for _, want := range []string{`"dashboard":"full"`, `"fees_collector":"stripe"`, `"losses_collector":"stripe"`, `"contact_email":"owner@shop.ae"`,
		`"identity":{"country":"AE"}`, `"merchant":{"capabilities":{"card_payments":{"requested":true}}}`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("body %s lacks %s", raw, want)
		}
	}
}

func TestCreateAccountError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"parameter_invalid","message":"bad country"}}`))
	}))
	t.Cleanup(srv.Close)
	_, err := (&Client{Key: "k", BaseURL: srv.URL}).CreateAccount(context.Background(), "", "XX")
	if err == nil || !strings.Contains(err.Error(), "bad country") {
		t.Fatal(err)
	}
}

func TestCheckoutIsADirectCharge(t *testing.T) {
	var got captured
	c := fake(t, 200, `{"id":"cs_1","url":"https://checkout.stripe.com/c/pay/cs_1"}`, &got)
	s, err := c.CreateCheckout(context.Background(), "acct_m", CheckoutParams{
		Currency: "aed", SuccessURL: "https://s/ok", CancelURL: "https://s/cancel", Reference: "order-9",
		Items:    []LineItem{{Name: "Royal Oud", Amount: 25000, Quantity: 2}},
		Metadata: map[string]string{"rx_app": "al-noor"},
	})
	if err != nil || s.URL == "" {
		t.Fatal(err)
	}
	if got.account != "acct_m" || got.path != "/v1/checkout/sessions" || got.form.Get("line_items[0][price_data][unit_amount]") != "25000" ||
		got.form.Get("line_items[0][quantity]") != "2" || got.form.Get("metadata[rx_app]") != "al-noor" || got.form.Get("client_reference_id") != "order-9" ||
		got.form.Get("payment_intent_data[application_fee_amount]") != "" {
		t.Fatalf("%+v", got)
	}
}

func TestErrorsAreReadable(t *testing.T) {
	var got captured
	c := fake(t, 400, `{"error":{"type":"invalid_request_error","code":"parameter_invalid_integer","message":"Invalid integer: abc","param":"line_items[0][price_data][unit_amount]"}}`, &got)
	_, err := c.GetAccount(context.Background(), "acct_1")
	if err == nil || !strings.Contains(err.Error(), "parameter_invalid_integer") || !strings.Contains(err.Error(), "unit_amount") {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyWebhook(t *testing.T) {
	payload := []byte(`{"id":"evt_1","type":"checkout.session.completed","account":"acct_m","data":{"object":{"id":"cs_1"}}}`)
	now := time.Now()
	header := Sign(payload, "whsec_x", now)
	e, err := VerifyWebhook(payload, header, "whsec_x", now)
	if err != nil || e.Account != "acct_m" || e.Type != "checkout.session.completed" {
		t.Fatal(e, err)
	}
	if _, err := VerifyWebhook(payload, header, "whsec_other", now); err == nil {
		t.Fatal("wrong secret accepted")
	}
	if _, err := VerifyWebhook(append(payload, ' '), header, "whsec_x", now); err == nil {
		t.Fatal("changed payload accepted")
	}
	if _, err := VerifyWebhook(payload, header, "whsec_x", now.Add(10*time.Minute)); err == nil {
		t.Fatal("old signature accepted")
	}
	if _, err := VerifyWebhook(payload, "", "whsec_x", now); err == nil {
		t.Fatal("missing header accepted")
	}
}

func TestApplicationFeeOnlyWhenSet(t *testing.T) {
	var got captured
	c := fake(t, 200, `{"id":"cs_1","url":"u"}`, &got)
	c.CreateCheckout(context.Background(), "acct_m", CheckoutParams{Currency: "aed", Items: []LineItem{{Name: "x", Amount: 100000, Quantity: 1}}, ApplicationFee: 2000})
	if got.form.Get("payment_intent_data[application_fee_amount]") != "2000" {
		t.Fatalf("%v", got.form)
	}
}

func TestSubscriptionCheckout(t *testing.T) {
	var got captured
	c := fake(t, 200, `{"id":"cs_2","url":"https://checkout.stripe.com/c/pay/cs_2"}`, &got)
	u, err := c.SubscriptionCheckout(context.Background(), "cus_1", "price_pro", "https://s/ok", "https://s/no", map[string]string{"rx_user": "usr_1"})
	if err != nil || u == "" || got.account != "" || got.form.Get("mode") != "subscription" || got.form.Get("customer") != "cus_1" ||
		got.form.Get("line_items[0][price]") != "price_pro" || got.form.Get("subscription_data[metadata][rx_user]") != "usr_1" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestSubscriptionHelpers(t *testing.T) {
	var got captured
	c := fake(t, 200, `{"data":[{"id":"sub_1","customer":"cus_1","status":"active","items":{"data":[{"price":{"id":"price_pro"},"current_period_end":1900000000}]}}]}`, &got)
	subs, err := c.Subscriptions(context.Background(), "cus_1")
	if err != nil || len(subs) != 1 || subs[0].PriceID() != "price_pro" || subs[0].PeriodEnd().Unix() != 1900000000 || got.form.Get("status") != "all" {
		t.Fatalf("%+v %v %v", subs, err, got.form)
	}
}

func TestSetupCheckout(t *testing.T) {
	var got captured
	c := fake(t, 200, `{"id":"cs_3","url":"https://checkout.stripe.com/c/pay/cs_3"}`, &got)
	u, err := c.SetupCheckout(context.Background(), "cus_1", "https://s/ok", "https://s/no", map[string]string{"rx_user": "usr_1"})
	if err != nil || u != "https://checkout.stripe.com/c/pay/cs_3" || got.account != "" || got.path != "/v1/checkout/sessions" ||
		got.form.Get("mode") != "setup" || got.form.Get("customer") != "cus_1" || got.form.Get("allowed_payment_method_types[0]") != "card" ||
		got.form.Get("success_url") != "https://s/ok" || got.form.Get("setup_intent_data[metadata][rx_user]") != "usr_1" ||
		got.form.Get("line_items[0][price]") != "" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestCardPaymentMethods(t *testing.T) {
	var got captured
	c := fake(t, 200, `{"data":[{"id":"pm_1","type":"card","customer":"cus_1","card":{"brand":"visa","last4":"4242","exp_month":12,"exp_year":2030}}]}`, &got)
	pms, err := c.CardPaymentMethods(context.Background(), "cus_1")
	if err != nil || len(pms) != 1 || pms[0].Card.Last4 != "4242" || pms[0].Card.ExpMonth != 12 ||
		!pms[0].Expires().Equal(time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v %v", pms, err)
	}
	if got.method != "GET" || got.path != "/v1/customers/cus_1/payment_methods" || got.form.Get("type") != "card" || got.account != "" {
		t.Fatalf("%+v", got)
	}
	if (PaymentMethod{}).Expires() != (time.Time{}) {
		t.Fatal("unknown expiry")
	}
}
