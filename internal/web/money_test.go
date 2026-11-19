package web_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/stripe"
	"github.com/arif-rachim/renoxium/internal/web"
)

// moneyStripe is a fake Stripe for the admin panel's money actions.
type moneyStripe struct {
	mu       sync.Mutex
	sessions int
	lastForm url.Values // the newest checkout session's parameters
	calls    []string   // refunds, captures, releases and subscription changes
}

func (f *moneyStripe) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *moneyStripe) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		p := r.URL.Path
		switch {
		case p == "/v2/core/accounts":
			w.Write([]byte(`{"id":"acct_m"}`))
		case strings.HasPrefix(p, "/v1/accounts/"):
			w.Write([]byte(`{"id":"acct_m","charges_enabled":true,"details_submitted":true}`))
		case p == "/v1/account_links":
			w.Write([]byte(`{"url":"https://connect.stripe.com/setup/s/x"}`))
		case p == "/v1/checkout/sessions":
			f.sessions++
			f.lastForm = form
			id := "cs_" + strconv.Itoa(f.sessions)
			w.Write([]byte(`{"id":"` + id + `","url":"https://checkout.stripe.com/c/pay/` + id + `"}`))
		case p == "/v1/refunds":
			f.calls = append(f.calls, "refund "+form.Get("payment_intent")+" "+form.Get("amount"))
			w.Write([]byte(`{"id":"re_1","status":"succeeded"}`))
		case strings.HasPrefix(p, "/v1/payment_intents/") && r.Method == "GET":
			w.Write([]byte(`{"id":"` + strings.TrimPrefix(p, "/v1/payment_intents/") + `","status":"requires_capture","amount_capturable":50000}`))
		case strings.HasSuffix(p, "/capture"):
			f.calls = append(f.calls, "capture "+p+" "+form.Get("amount_to_capture"))
			w.Write([]byte(`{"id":"pi","status":"succeeded"}`))
		case strings.HasSuffix(p, "/cancel"):
			f.calls = append(f.calls, "release "+p)
			w.Write([]byte(`{"id":"pi","status":"canceled"}`))
		case strings.HasPrefix(p, "/v1/subscriptions/"):
			f.calls = append(f.calls, "subscription "+r.Method+" "+p)
			json.NewEncoder(w).Encode(map[string]any{"id": strings.TrimPrefix(p, "/v1/subscriptions/"), "status": "active",
				"items": map[string]any{"data": []map[string]any{{"current_period_end": 1900000000}}}})
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"error":{"message":"unknown"}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newMoneyEnv is newEnv with Stripe payments set up against the fake.
func newMoneyEnv(t *testing.T, fs *moneyStripe) *env {
	t.Helper()
	dir := t.TempDir()
	p, err := platform.New(platform.Config{DataDir: dir, AppsDomain: "renoxium.test", PublicScheme: "https",
		Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: fs.server(t).URL}}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	acc, err := accounts.Open(filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	var site http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.ServeApp(w, r) {
			site.ServeHTTP(w, r)
		}
	}))
	ws := web.New(srv.URL, acc, p)
	mb := &mailbox{}
	ws.Mailer = mb
	ws.StripeWebhookSecret = "whsec_test"
	site = ws
	t.Cleanup(func() { srv.Close(); p.Close(); acc.Close() })
	return &env{t: t, server: srv, base: srv.URL, p: p, mail: mb, site: ws, acc: acc}
}

func (e *env) appPost(host, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://"+host+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	e.p.ServeApp(w, r)
	return w
}

func TestAdminMoneyActions(t *testing.T) {
	fs := &moneyStripe{}
	e := newMoneyEnv(t, fs)
	ctx := context.Background()
	owner := e.browser()
	e.signup(owner, "rana@shop.ae")
	e.publishDef(owner, "rana-cars", &appdef.App{
		Scripts: map[string]string{
			"buy": `function handle(req) { return {redirect: payments.checkout({items: [{name: "Rental", price: 500}], reference: "booking-" + req.form.n,
				hold: req.form.hold === "1"}).url} }`,
			"sub": `function handle(req) { return {redirect: payments.subscribe({items: [{name: "Monthly bookkeeping", price: 300}], interval: "month",
				reference: "client-1"}).url} }`,
			"paid": `function handle(req) { console.log("on_payment", req.body.event, req.body.reference); return {text: "ok"} }`,
		},
		OnPayment: "paid",
		Templates: map[string]string{"t": "x"},
		Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}, {Method: "POST", Path: "/buy", Script: "buy"},
			{Method: "POST", Path: "/sub", Script: "sub"}},
	})
	e.p.StripeOnboardingURL(ctx, "rana-cars", "rana@shop.ae", "https://r", "https://f")
	e.p.RefreshPaymentAccount(ctx, "rana-cars")
	host := "rana-cars.renoxium.test"
	event := func(id, typ string, obj map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(obj)
		ev := stripe.Event{ID: id, Type: typ, Account: "acct_m"}
		ev.Data.Object = raw
		if err := e.p.HandleStripeEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	paid := func(session, pi, status string) {
		event("evt_"+session, "checkout.session.completed", map[string]any{"id": session, "payment_status": status, "payment_intent": pi,
			"metadata": map[string]string{"rx_app": "rana-cars"}, "customer_details": map[string]string{"email": "buyer@mail.ae"}})
	}
	if w := e.appPost(host, "/buy", url.Values{"n": {"1"}}); w.Code != 303 {
		t.Fatalf("buy: %d %s", w.Code, w.Body.String())
	}
	paid("cs_1", "pi_1", "paid")

	page := func(c *http.Client) (int, string) {
		r, _ := c.Get(e.base + "/apps/rana-cars/payments")
		return r.StatusCode, read(t, r)
	}
	if code, body := page(owner); code != 200 || !strings.Contains(body, `action="/apps/rana-cars/payments/cs_1/refund"`) || !strings.Contains(body, "buyer@mail.ae") {
		t.Fatalf("payments page: %d %s", code, body)
	}
	refund := func(c *http.Client, csrf, amount string) (int, string) {
		r, err := c.PostForm(e.base+"/apps/rana-cars/payments/cs_1/refund", url.Values{"csrf": {csrf}, "amount": {amount}})
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode, read(t, r)
	}
	token := e.csrf(owner, "/apps/rana-cars/payments")

	// CSRF is required.
	if code, _ := refund(owner, "forged-token-forged-token-forged-token", "10"); code != 403 {
		t.Fatalf("without csrf: %d", code)
	}
	// Staff can edit data but cannot move money.
	owner.PostForm(e.base+"/apps/rana-cars/staff", url.Values{"csrf": {e.csrf(owner, "/apps/rana-cars/staff")}, "email": {"sara@shop.ae"}})
	staff := e.browser()
	e.signup(staff, "sara@shop.ae")
	http.Get(linkRE.FindString(e.mail.last(t, "sara@shop.ae", "verify_email").Text))
	if code, _ := refund(staff, e.csrf(staff, "/"), "10"); code != 404 {
		t.Fatalf("staff refund: %d", code)
	}
	// Another account's owner cannot either.
	other := e.browser()
	e.signup(other, "sami@shop.ae")
	if code, _ := refund(other, e.csrf(other, "/"), "10"); code != 404 {
		t.Fatalf("other owner refund: %d", code)
	}
	if fs.count("refund") != 0 {
		t.Fatal("a refused request reached Stripe")
	}
	// Amounts are checked on the server.
	if code, body := refund(owner, token, "600"); code != 400 || !strings.Contains(body, "at most 500.00 AED") {
		t.Fatalf("over-refund: %d %s", code, body)
	}
	if code, body := refund(owner, token, "ten"); code != 400 || !strings.Contains(body, "as a number") {
		t.Fatalf("not a number: %d %s", code, body)
	}
	if code, body := refund(owner, token, "-5"); code != 400 || !strings.Contains(body, "positive amount") {
		t.Fatalf("negative: %d %s", code, body)
	}
	code, body := refund(owner, token, "100")
	if code != 200 || !strings.Contains(body, "100.00 AED of the payment for booking-1 is refunded in all") || fs.count("refund pi_1 10000") != 1 {
		t.Fatalf("refund: %d %s %v", code, body, fs.calls)
	}
	if code, _ := refund(owner, token, ""); code != 200 || fs.count("refund pi_1 40000") != 1 {
		t.Fatalf("refund the rest: %d %v", code, fs.calls)
	}
	if code, _ := refund(owner, token, ""); code != 400 || fs.count("refund") != 2 {
		t.Fatal("refunded a fully refunded payment")
	}
	logs, _ := e.p.Logs("rana-cars", logFilter())
	if !anyMessage(logs, "rana@shop.ae refunded AED 100.00 of the payment for booking-1") || !anyMessage(logs, "rana@shop.ae refunded the payment for booking-1 in the admin panel") {
		t.Fatal("refund not logged with the owner's email")
	}
	if !anyMessage(logs, "console: on_payment partially_refunded booking-1") || !anyMessage(logs, "console: on_payment refunded booking-1") {
		t.Fatal("on_payment did not run")
	}

	// Card holds: capture part, release another.
	e.appPost(host, "/buy", url.Values{"n": {"2"}, "hold": {"1"}})
	paid("cs_2", "pi_2", "unpaid")
	e.appPost(host, "/buy", url.Values{"n": {"3"}, "hold": {"1"}})
	paid("cs_3", "pi_3", "unpaid")
	if _, body := page(owner); !strings.Contains(body, `action="/apps/rana-cars/payments/cs_2/capture"`) || !strings.Contains(body, `action="/apps/rana-cars/payments/cs_3/release"`) {
		t.Fatalf("holds not offered: %s", body)
	}
	r, _ := owner.PostForm(e.base+"/apps/rana-cars/payments/cs_2/capture", url.Values{"csrf": {token}, "amount": {"75.5"}})
	if body := read(t, r); r.StatusCode != 200 || !strings.Contains(body, "Captured 75.50 AED for booking-2") || fs.count("capture /v1/payment_intents/pi_2/capture 7550") != 1 {
		t.Fatalf("capture: %d %s", r.StatusCode, body)
	}
	r, _ = owner.PostForm(e.base+"/apps/rana-cars/payments/cs_3/release", url.Values{"csrf": {token}})
	if body := read(t, r); r.StatusCode != 200 || !strings.Contains(body, "released") || fs.count("release /v1/payment_intents/pi_3/cancel") != 1 {
		t.Fatalf("release: %d %s", r.StatusCode, body)
	}
	if r, _ := staff.PostForm(e.base+"/apps/rana-cars/payments/cs_2/capture", url.Values{"csrf": {e.csrf(staff, "/")}}); r.StatusCode != 404 {
		t.Fatalf("staff capture: %d", r.StatusCode)
	}

	// Subscriptions: listed with the next payment date, cancelled by the owner.
	e.appPost(host, "/sub", url.Values{})
	list, _ := e.p.Subscriptions(ctx, "rana-cars", 10)
	if len(list) != 0 {
		t.Fatal("a subscription still at checkout is listed")
	}
	meta := map[string]string{"rx_app": "rana-cars", "rx_reference": "client-1",
		"rx_subscription": fs.lastForm.Get("subscription_data[metadata][rx_subscription]")}
	event("evt_cs_sub", "checkout.session.completed", map[string]any{"id": "cs_4", "mode": "subscription", "subscription": "sub_1", "metadata": meta,
		"customer_details": map[string]string{"email": "client@mail.ae"}})
	event("evt_in_1", "invoice.paid", map[string]any{"id": "in_1", "billing_reason": "subscription_create", "amount_paid": 30000, "currency": "aed",
		"parent": map[string]any{"subscription_details": map[string]any{"subscription": "sub_1", "metadata": meta}}})
	_, body = page(owner)
	subID := meta["rx_subscription"]
	if !strings.Contains(body, "Subscriptions") || !strings.Contains(body, "300.00 <span class=\"cur\">aed</span></span> every month") || !strings.Contains(body, "2030-03-17 17:46:40") ||
		!strings.Contains(body, `action="/apps/rana-cars/subscriptions/`+subID+`/cancel"`) {
		t.Fatalf("subscriptions not listed: %s", body)
	}
	cancel := func(c *http.Client, csrf, when string) (int, string) {
		r, _ := c.PostForm(e.base+"/apps/rana-cars/subscriptions/"+subID+"/cancel", url.Values{"csrf": {csrf}, "when": {when}})
		return r.StatusCode, read(t, r)
	}
	if code, _ := cancel(staff, e.csrf(staff, "/"), "now"); code != 404 {
		t.Fatalf("staff cancel: %d", code)
	}
	if code, _ := cancel(owner, "forged-token-forged-token-forged-token", "now"); code != 403 {
		t.Fatalf("cancel without csrf: %d", code)
	}
	if code, body := cancel(owner, token, "period_end"); code != 200 || !strings.Contains(body, "ends on 2030-03-17 17:46:40 UTC") ||
		fs.count("subscription POST /v1/subscriptions/sub_1") != 1 {
		t.Fatalf("cancel at period end: %d %s", code, body)
	}
	if code, body := cancel(owner, token, "now"); code != 200 || !strings.Contains(body, "is cancelled") || fs.count("subscription DELETE /v1/subscriptions/sub_1") != 1 {
		t.Fatalf("cancel now: %d %s", code, body)
	}
	logs, _ = e.p.Logs("rana-cars", logFilter())
	if !anyMessage(logs, "rana@shop.ae cancelled the subscription for client-1 in the admin panel (now)") || !anyMessage(logs, "console: on_payment subscription_cancelled client-1") {
		t.Fatal("cancellation not logged or on_payment not run")
	}
	if list, _ := e.p.Subscriptions(ctx, "rana-cars", 10); len(list) != 1 || list[0].Status != "canceled" {
		t.Fatalf("after cancel: %+v", list)
	}
}
