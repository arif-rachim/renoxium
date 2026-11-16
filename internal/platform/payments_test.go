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
	accounts []url.Values
	headers  []string
}

func (f *fakeStripe) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/accounts":
			f.accounts = append(f.accounts, form)
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
			w.Write([]byte(`{"id":"cs_test_1","url":"https://checkout.stripe.com/c/pay/cs_test_1"}`))
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
	acct := fs.accounts[0]
	if acct.Get("controller[stripe_dashboard][type]") != "full" || acct.Get("controller[fees][payer]") != "account" ||
		acct.Get("controller[losses][payments]") != "stripe" || acct.Get("country") != "AE" {
		t.Fatalf("account created with %v", acct)
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
		obj, _ := json.Marshal(map[string]any{"id": "cs_test_1", "payment_status": paymentStatus, "metadata": map[string]string{"rx_app": "oud"},
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
