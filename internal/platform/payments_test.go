package platform_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

// shopWithCheckout sells one product and marks orders paid from on_payment.
func shopWithCheckout() *appdef.App {
	return &appdef.App{
		Tables: []appdef.Table{{Name: "orders", Columns: []appdef.Column{{Name: "status", Type: "text"}, {Name: "total", Type: "real"}}}},
		Scripts: map[string]string{
			"buy": `function handle(req) {
				const o = db.exec("INSERT INTO orders (status, total) VALUES ('awaiting payment', 500)");
				const pay = payments.checkout({items: [{name: "Royal Oud", price: 250, quantity: 2}], reference: "order-" + o.lastInsertId,
					success_path: "/thanks?order=" + o.lastInsertId, cancel_path: "/"});
				return {redirect: pay.url};
			}`,
			"paid": `function handle(req) {
				const id = Number(req.body.reference.split("-")[1]);
				db.exec("UPDATE orders SET status = ? WHERE id = ?", req.body.event, id);
				console.log("on_payment", req.body.event, req.body.amount, req.body.currency);
				return {text: "ok"};
			}`,
			"thanks": `function handle(req) { return {json: {order: db.query("SELECT status FROM orders WHERE id = ?", Number(req.query.order))[0], payment: payments.get("order-" + req.query.order)}} }`,
			"bad":    `function handle(req) { return {json: payments.checkout({items: [{name: "x", price: 0.001}], reference: "r"})} }`,
		},
		OnPayment: "paid",
		Routes: []appdef.Route{
			{Method: "POST", Path: "/buy", Script: "buy"},
			{Method: "GET", Path: "/thanks", Script: "thanks"},
			{Method: "GET", Path: "/bad", Script: "bad"},
		},
	}
}

func TestPreviewCheckoutIsSimulated(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "oud")
	if res, err := p.ApplyChange(ctx, "oud", shopWithCheckout()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	buy, err := p.TestRequest(ctx, "oud", platform.TestRequestInput{Method: "POST", Path: "/buy"})
	if err != nil || buy.Status != 303 || !strings.HasPrefix(buy.Location, "/_rx/pay/sim_") {
		t.Fatalf("buy: %v %+v", err, buy)
	}
	// The agent is told the payment was simulated and that live card
	// payments need the owner to connect Stripe first.
	if !strings.HasPrefix(buy.Payment, "simulated") || !strings.Contains(buy.Payment, "/apps/oud/payments") && !strings.Contains(buy.Payment, "not available") {
		t.Fatalf("payment label: %q", buy.Payment)
	}
	if st := p.PaymentStatus("oud"); st.CanCharge || st.StripeConnected || st.Note == "" {
		t.Fatalf("payment status: %+v", st)
	}
	page, _ := p.TestRequest(ctx, "oud", platform.TestRequestInput{Path: buy.Location})
	if page.Status != 200 || !strings.Contains(page.Body, "500.00 AED") || !strings.Contains(page.Body, "simulated") {
		t.Fatalf("pay page: %+v", page)
	}
	paid, _ := p.TestRequest(ctx, "oud", platform.TestRequestInput{Method: "POST", Path: buy.Location, Form: map[string]string{"decision": "pay"}})
	if paid.Status != 303 || paid.Location != "/thanks?order=1" {
		t.Fatalf("pay: %+v", paid)
	}
	thanks, _ := p.TestRequest(ctx, "oud", platform.TestRequestInput{Path: paid.Location})
	if !strings.Contains(thanks.Body, `"status":"paid"`) || !strings.Contains(thanks.Body, `"amount":500`) || !strings.Contains(thanks.Body, `"reference":"order-1"`) {
		t.Fatalf("thanks: %s", thanks.Body)
	}
	// Paying again changes nothing and does not run on_payment twice.
	p.TestRequest(ctx, "oud", platform.TestRequestInput{Method: "POST", Path: buy.Location, Form: map[string]string{"decision": "pay"}})
	logs, _ := p.Logs("oud", logFilterAll())
	if n := countMessages(logs, "console: on_payment paid 500 aed"); n != 1 {
		t.Fatalf("on_payment ran %d times", n)
	}
	bad, _ := p.TestRequest(ctx, "oud", platform.TestRequestInput{Path: "/bad"})
	if bad.Status != 500 || !strings.Contains(logText(bad), "at most 2 decimals") {
		t.Fatalf("bad price: %+v", bad)
	}
}

type fakeStripe struct {
	mu       sync.Mutex
	charges  bool
	sessions []url.Values
	accounts []map[string]any
	headers  []string
	// calls are money calls (refunds, payment intents, subscriptions).
	calls []fakeCall
	// intents answer GET /v1/payment_intents/{id}.
	intents map[string]map[string]any
}

type fakeCall struct {
	method, path, account string
	form                  url.Values
}

// callsTo returns the money calls whose path starts with prefix.
func (f *fakeStripe) callsTo(method, prefix string) []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeCall
	for _, c := range f.calls {
		if c.method == method && strings.HasPrefix(c.path, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// periodEnd is the fake's current_period_end for subscriptions.
const periodEnd = 1900000000

func (f *fakeStripe) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		if r.Method == "GET" {
			form = r.URL.Query()
		}
		path := r.URL.Path
		if strings.HasPrefix(path, "/v1/refunds") || strings.HasPrefix(path, "/v1/payment_intents") || strings.HasPrefix(path, "/v1/subscriptions") ||
			strings.HasPrefix(path, "/v1/invoice_payments") {
			f.calls = append(f.calls, fakeCall{r.Method, path, r.Header.Get("Stripe-Account"), form})
		}
		sub := func(id, status string, cancelAtEnd bool) map[string]any {
			return map[string]any{"id": id, "status": status, "cancel_at_period_end": cancelAtEnd,
				"items": map[string]any{"data": []map[string]any{{"current_period_end": periodEnd}}}}
		}
		switch {
		case r.Method == "POST" && path == "/v1/refunds":
			amount, _ := strconv.ParseInt(form.Get("amount"), 10, 64)
			json.NewEncoder(w).Encode(map[string]any{"id": "re_" + strconv.Itoa(len(f.calls)), "status": "succeeded", "amount": amount})
		case r.Method == "GET" && strings.HasPrefix(path, "/v1/payment_intents/"):
			pi, ok := f.intents[strings.TrimPrefix(path, "/v1/payment_intents/")]
			if !ok {
				w.WriteHeader(404)
				w.Write([]byte(`{"error":{"message":"No such payment_intent"}}`))
				return
			}
			json.NewEncoder(w).Encode(pi)
		case r.Method == "POST" && strings.HasSuffix(path, "/capture"):
			amount, _ := strconv.ParseInt(form.Get("amount_to_capture"), 10, 64)
			json.NewEncoder(w).Encode(map[string]any{"id": strings.Split(path, "/")[3], "status": "succeeded", "amount_received": amount})
		case r.Method == "POST" && strings.HasPrefix(path, "/v1/payment_intents/") && strings.HasSuffix(path, "/cancel"):
			json.NewEncoder(w).Encode(map[string]any{"id": strings.Split(path, "/")[3], "status": "canceled"})
		case strings.HasPrefix(path, "/v1/subscriptions/"):
			id := strings.TrimPrefix(path, "/v1/subscriptions/")
			switch r.Method {
			case "DELETE":
				json.NewEncoder(w).Encode(sub(id, "canceled", false))
			case "POST":
				json.NewEncoder(w).Encode(sub(id, "active", form.Get("cancel_at_period_end") == "true"))
			default:
				json.NewEncoder(w).Encode(sub(id, "active", false))
			}
		case r.Method == "GET" && path == "/v1/invoice_payments":
			json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"status": "paid",
				"payment": map[string]any{"type": "payment_intent", "payment_intent": "pi_for_" + form.Get("invoice")}}}})
		case r.URL.Path == "/v1/checkout/sessions" && form.Get("mode") == "subscription":
			f.sessions = append(f.sessions, form)
			f.headers = append(f.headers, r.Header.Get("Stripe-Account"))
			w.Write([]byte(`{"id":"cs_sub_1","url":"https://checkout.stripe.com/c/pay/cs_sub_1"}`))
		case r.Method == "POST" && r.URL.Path == "/v2/core/accounts":
			var acct map[string]any
			json.Unmarshal(raw, &acct)
			f.accounts = append(f.accounts, acct)
			id := "acct_merchant"
			if n := len(f.accounts); n > 1 {
				id += strconv.Itoa(n)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": id, "charges_enabled": false})
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/accounts/acct_merchant"):
			json.NewEncoder(w).Encode(map[string]any{"id": strings.TrimPrefix(r.URL.Path, "/v1/accounts/"), "charges_enabled": f.charges, "details_submitted": f.charges})
		case r.URL.Path == "/v1/account_links":
			w.Write([]byte(`{"url":"https://connect.stripe.com/setup/s/xyz"}`))
		case r.URL.Path == "/v1/checkout/sessions":
			f.sessions = append(f.sessions, form)
			f.headers = append(f.headers, r.Header.Get("Stripe-Account"))
			id := "cs_test_" + strconv.Itoa(len(f.sessions)) // cs_test_1 first, as Stripe ids differ per session
			w.Write([]byte(`{"id":"` + id + `","url":"https://checkout.stripe.com/c/pay/` + id + `"}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"error":{"message":"unknown"}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLiveCheckoutThroughStripe(t *testing.T) {
	fs := &fakeStripe{}
	srv := fs.server(t)
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: srv.URL}}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	p.CreateApp("usr_test", "oud")
	p.ApplyChange(ctx, "oud", shopWithCheckout())
	p.Publish("oud", 0)
	host := "oud.renoxium.test"

	// Before the owner connects Stripe, checkout explains what is missing.
	if r := do(t, p, "POST", host, "/buy", url.Values{}); r.status != 500 {
		t.Fatalf("buy before connect: %d", r.status)
	}
	logs, _ := p.Logs("oud", logFilterAll())
	if countMessages(logs, "has not finished connecting Stripe") == 0 {
		t.Fatal("missing explanation in the log")
	}

	link, err := p.StripeOnboardingURL(ctx, "oud", "owner@oud.ae", "https://site/return", "https://site/refresh")
	if err != nil || link != "https://connect.stripe.com/setup/s/xyz" {
		t.Fatal(link, err)
	}
	acct, _ := json.Marshal(fs.accounts[0])
	for _, want := range []string{`"dashboard":"full"`, `"fees_collector":"stripe"`, `"losses_collector":"stripe"`, `"identity":{"country":"AE"}`} {
		if !strings.Contains(string(acct), want) {
			t.Fatalf("account created with %s", acct)
		}
	}
	// A second visit reuses the account.
	p.StripeOnboardingURL(ctx, "oud", "owner@oud.ae", "https://site/return", "https://site/refresh")
	if len(fs.accounts) != 1 {
		t.Fatal("created a second Stripe account")
	}
	fs.charges = true
	if pa, err := p.RefreshPaymentAccount(ctx, "oud"); err != nil || !pa.ChargesEnabled {
		t.Fatal(pa, err)
	}

	r := do(t, p, "POST", host, "/buy", url.Values{})
	if r.status != 303 || r.header.Get("Location") != "https://checkout.stripe.com/c/pay/cs_test_1" {
		t.Fatalf("buy: %d %s", r.status, r.header.Get("Location"))
	}
	s := fs.sessions[0]
	if fs.headers[0] != "acct_merchant" || s.Get("line_items[0][price_data][unit_amount]") != "25000" || s.Get("line_items[0][quantity]") != "2" ||
		s.Get("line_items[0][price_data][currency]") != "aed" || s.Get("success_url") != "https://oud.renoxium.test/thanks?order=2" ||
		s.Get("metadata[rx_app]") != "oud" || s.Get("client_reference_id") != "order-2" {
		t.Fatalf("session: %v on %s", s, fs.headers[0])
	}

	event := func(typ, account, paymentStatus string) stripe.Event {
		obj, _ := json.Marshal(map[string]any{"id": "cs_test_1", "payment_status": paymentStatus, "metadata": map[string]string{"rx_app": "oud"}, "payment_intent": "pi_1",
			"customer_details": map[string]string{"email": "buyer@mail.ae", "name": "Buyer"}})
		e := stripe.Event{ID: "evt", Type: typ, Account: account}
		e.Data.Object = obj
		return e
	}
	status := func() string {
		r := do(t, p, "GET", host, "/thanks?order=2", nil) // order 1 was left by the failed first attempt
		var out struct{ Order struct{ Status string } }
		json.Unmarshal([]byte(r.body), &out)
		return out.Order.Status
	}
	// An event from another Stripe account is ignored.
	p.HandleStripeEvent(ctx, event("checkout.session.completed", "acct_someone_else", "paid"))
	if status() != "awaiting payment" {
		t.Fatal("event from another account was applied")
	}
	// Completed but unpaid (delayed methods) waits for the async event.
	p.HandleStripeEvent(ctx, event("checkout.session.completed", "acct_merchant", "unpaid"))
	if status() != "awaiting payment" {
		t.Fatal("unpaid session marked paid")
	}
	p.HandleStripeEvent(ctx, event("checkout.session.async_payment_succeeded", "acct_merchant", "paid"))
	p.HandleStripeEvent(ctx, event("checkout.session.completed", "acct_merchant", "paid")) // retried delivery
	if status() != "paid" {
		t.Fatalf("status = %q", status())
	}
	logs, _ = p.Logs("oud", logFilterAll())
	if n := countMessages(logs, "console: on_payment paid 500 aed"); n != 1 {
		t.Fatalf("on_payment ran %d times", n)
	}
	// A later expiry cannot undo a payment.
	p.HandleStripeEvent(ctx, event("checkout.session.expired", "acct_merchant", "unpaid"))
	if status() != "paid" {
		t.Fatal("expiry undid the payment")
	}
	list, _ := p.Payments(ctx, "oud", 10)
	if len(list) != 1 || list[0].CustomerEmail != "buyer@mail.ae" || list[0].Amount != 500 {
		t.Fatalf("payments: %+v", list)
	}
	cur, sales, err := p.Sales(ctx, "oud")
	if err != nil || cur != "AED" || len(sales) != 3 || sales[0].Count != 1 || sales[0].Amount != 500 || sales[2].Amount != 500 {
		t.Fatalf("sales: %v %s %+v", err, cur, sales)
	}
	// Refunds made in the merchant's Stripe Dashboard reach the order.
	refund := func(account string, amount int64, full bool) {
		obj, _ := json.Marshal(map[string]any{"id": "ch_1", "payment_intent": "pi_1", "amount": 50000, "amount_refunded": amount, "refunded": full})
		e := stripe.Event{ID: "evt_r", Type: "charge.refunded", Account: account}
		e.Data.Object = obj
		if err := p.HandleStripeEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	refund("acct_someone_else", 50000, true)
	if status() != "paid" {
		t.Fatal("refund from another account applied")
	}
	refund("acct_merchant", 10000, false)
	if status() != "partially_refunded" {
		t.Fatalf("after a partial refund: %q", status())
	}
	refund("acct_merchant", 10000, false) // repeated delivery
	refund("acct_merchant", 50000, true)
	if status() != "refunded" {
		t.Fatalf("after the full refund: %q", status())
	}
	logs, _ = p.Logs("oud", logFilterAll())
	if n := countMessages(logs, "on_payment partially_refunded"); n != 1 {
		t.Fatalf("partial refund hook ran %d times", n)
	}
	if list, _ := p.Payments(ctx, "oud", 1); list[0].Status != "refunded" || list[0].Refunded != 500 {
		t.Fatalf("payment after refund: %+v", list[0])
	}

	// Live never serves the simulated checkout.
	if r := do(t, p, "GET", host, "/_rx/pay/cs_test_1", nil); r.status != 404 {
		t.Fatalf("simulated checkout on live: %d", r.status)
	}
}

func logFilterAll() applog.Filter { return applog.Filter{Limit: 200} }

func countMessages(events []applog.Event, text string) int {
	n := 0
	for _, e := range events {
		if strings.Contains(e.Message, text) {
			n++
		}
	}
	return n
}

func logText(r platform.TestResponse) string {
	var b strings.Builder
	for _, e := range r.Logs {
		b.WriteString(e.Message + "\n")
	}
	return b.String()
}

func TestScriptsRedirectOnlyToTheirOwnCheckout(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "hop")
	def := &appdef.App{
		Templates: map[string]string{"t": "<!doctype html><html><head></head><body>ok</body></html>"},
		Scripts: map[string]string{
			"stripe":  `function handle(req){ return {redirect: "https://checkout.stripe.com.evil.example/c/pay/cs_forged"} }`,
			"away":    `function handle(req){ return {redirect: "https://evil.example/"} }`,
			"slashes": `function handle(req){ return {redirect: "//evil.example/"} }`,
			"own":     `function handle(req){ return {redirect: "/thanks"} }`,
			"pay":     `function handle(req){ return {redirect: payments.checkout({items: [{name: "x", price: 5}], reference: "r1", success_path: "/", cancel_path: "/"}).url} }`,
		},
		Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}, {Method: "POST", Path: "/stripe", Script: "stripe"},
			{Method: "POST", Path: "/away", Script: "away"}, {Method: "POST", Path: "/slashes", Script: "slashes"},
			{Method: "POST", Path: "/own", Script: "own"}, {Method: "POST", Path: "/pay", Script: "pay"}},
	}
	if res, err := p.ApplyChange(ctx, "hop", def); err != nil || !res.OK {
		t.Fatalf("apply: %v %+v", err, res)
	}
	host := "hop--preview.renoxium.test"
	for path, want := range map[string]int{"/stripe": 500, "/away": 500, "/slashes": 500, "/own": 303} {
		if r := do(t, p, "POST", host, path, url.Values{}); r.status != want {
			t.Errorf("%s: %d, want %d", path, r.status, want)
		}
	}
	// A checkout the script just created is allowed (on preview it is the simulated one).
	if r := do(t, p, "POST", host, "/pay", url.Values{}); r.status != 303 || !strings.HasPrefix(r.header.Get("Location"), platform.PayPrefix) {
		t.Fatalf("pay: %d %s", r.status, r.header.Get("Location"))
	}
}
