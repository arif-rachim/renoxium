package web_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/stripe"
	"github.com/arif-rachim/renoxium/internal/web"
)

type fakeBilling struct {
	mu         sync.Mutex
	customers  int
	status     string // status of the one subscription, "" for none
	price      string
	metadata   map[string]string // of the one subscription
	checkouts  []url.Values
	cancels    []string // subscriptions cancelled with DELETE
	failCancel bool
	lists      int // GET /v1/subscriptions calls
	// The seat item of the one subscription (work app staff beyond the
	// included ones), and the subscription item calls that changed it.
	seatPrice string
	seats     int64
	seatCalls []string
	failSeats bool
	cards     []map[string]any // card payment methods of cus_1
	cardLists int              // GET /v1/customers/cus_1/payment_methods calls
}

// card is a card payment method as Stripe returns it.
func card(last4 string, month, year int) map[string]any {
	return map[string]any{"id": "pm_" + last4, "type": "card", "customer": "cus_1",
		"card": map[string]any{"brand": "visa", "last4": last4, "exp_month": month, "exp_year": year}}
}

// subJSON is the one subscription as Stripe returns it.
func (f *fakeBilling) subJSON() map[string]any {
	items := []any{map[string]any{"id": "si_base", "price": map[string]any{"id": f.price}, "quantity": 1, "current_period_end": 1900000000}}
	if f.seats > 0 {
		items = append(items, map[string]any{"id": "si_seat", "price": map[string]any{"id": f.seatPrice}, "quantity": f.seats, "current_period_end": 1900000000})
	}
	return map[string]any{"id": "sub_1", "customer": "cus_1", "status": f.status, "metadata": f.metadata, "items": map[string]any{"data": items}}
}

func (f *fakeBilling) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		if strings.HasPrefix(r.URL.Path, "/v1/subscription_items") {
			if f.failSeats {
				w.WriteHeader(500)
				w.Write([]byte(`{"error":{"type":"api_error","message":"try later"}}`))
				return
			}
			f.seatCalls = append(f.seatCalls, r.Method+" "+r.URL.Path+" "+form.Get("quantity"))
			switch {
			case r.Method == http.MethodDelete:
				f.seats = 0
			case r.URL.Path == "/v1/subscription_items":
				f.seatPrice = form.Get("price")
				fallthrough
			default:
				f.seats, _ = strconv.ParseInt(form.Get("quantity"), 10, 64)
			}
			w.Write([]byte(`{"id":"si_seat"}`))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/sub_1" {
			json.NewEncoder(w).Encode(f.subJSON())
			return
		}
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/subscriptions/") {
			if f.failCancel {
				w.WriteHeader(500)
				w.Write([]byte(`{"error":{"type":"api_error","message":"try later"}}`))
				return
			}
			id := strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/")
			f.cancels = append(f.cancels, id)
			f.status = "canceled"
			w.Write([]byte(`{"id":"` + id + `","status":"canceled"}`))
			return
		}
		switch r.URL.Path {
		case "/v1/customers":
			f.customers++
			w.Write([]byte(`{"id":"cus_1"}`))
		case "/v1/checkout/sessions":
			f.checkouts = append(f.checkouts, form)
			w.Write([]byte(`{"id":"cs_sub","url":"https://checkout.stripe.com/c/pay/cs_sub"}`))
		case "/v1/subscriptions":
			f.lists++
			data := []any{}
			if f.status != "" {
				data = append(data, f.subJSON())
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
		case "/v1/customers/cus_1/payment_methods":
			f.cardLists++
			cards := f.cards
			if cards == nil {
				cards = []map[string]any{}
			}
			json.NewEncoder(w).Encode(map[string]any{"data": cards})
		case "/v1/billing_portal/sessions":
			w.Write([]byte(`{"url":"https://billing.stripe.com/p/session/x"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// billingWebhook sends a signed event to the platform billing webhook.
func billingWebhook(t *testing.T, base, secret, typ, account string, object, previous map[string]any) int {
	t.Helper()
	data := map[string]any{"object": object}
	if previous != nil {
		data["previous_attributes"] = previous
	}
	payload, _ := json.Marshal(map[string]any{"id": "evt_1", "type": typ, "account": account, "data": data})
	req, _ := http.NewRequest("POST", base+"/stripe/billing-webhook", strings.NewReader(string(payload)))
	req.Header.Set("Stripe-Signature", stripe.Sign(payload, secret, time.Now()))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// Saving a card (Stripe Checkout in setup mode) makes the account a member;
// removing it, or its expiry, makes it free again. The webhook's setup and
// payment method events, and the return from Stripe, read the cards.
func TestBilling(t *testing.T) {
	e := newEnv(t)
	fb := &fakeBilling{}
	e.site.Billing = web.Billing{Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: fb.server(t).URL}, WebhookSecret: "whsec_billing"}
	c := e.browser()
	e.signup(c, "vera@shop.ae")
	page := func(path string) string {
		r, _ := c.Get(e.base + path)
		return read(t, r)
	}
	if p := page("/billing"); !strings.Contains(p, "Your account: <strong>Free</strong>") || !strings.Contains(p, "A free account may have 3 apps") ||
		!strings.Contains(p, "saving a card charges nothing") || !strings.Contains(p, `action="/billing/card"`) || !strings.Contains(p, "shops pay 2%") ||
		!strings.Contains(p, "AED 99 a month") || strings.Contains(p, "Pro plan") || strings.Contains(p, "Business plan") || strings.Contains(p, "10 apps") {
		t.Fatalf("free page: %s", p)
	}
	if p := page("/"); !strings.Contains(p, `<a href="/billing">Free plan</a>`) || !strings.Contains(p, "of 3 apps used") {
		t.Fatal("dashboard plan")
	}

	// Save a card: the customer is created once and the owner goes to
	// Stripe Checkout in setup mode, which charges nothing.
	r, _ := c.PostForm(e.base+"/billing/card", url.Values{"csrf": {e.csrf(c, "/billing")}})
	if r.StatusCode != 303 || r.Header.Get("Location") != "https://checkout.stripe.com/c/pay/cs_sub" {
		t.Fatalf("card checkout: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	co := fb.checkouts[0]
	if co.Get("mode") != "setup" || co.Get("customer") != "cus_1" || co.Get("allowed_payment_method_types[0]") != "card" || co.Get("line_items[0][price]") != "" ||
		co.Get("success_url") != e.base+"/billing?done=1" || co.Get("cancel_url") != e.base+"/billing" {
		t.Fatalf("checkout session: %v", co)
	}
	if r, _ := c.PostForm(e.base+"/billing/card", url.Values{}); r.StatusCode != 403 {
		t.Fatalf("card checkout without csrf: %d", r.StatusCode)
	}

	// Stripe confirms by webhook.
	fb.cards = []map[string]any{card("4242", 12, 2030)}
	if code := billingWebhook(t, e.base, "whsec_wrong", "setup_intent.succeeded", "", map[string]any{"customer": "cus_1"}, nil); code != 400 {
		t.Fatalf("bad signature: %d", code)
	}
	if code := billingWebhook(t, e.base, "whsec_billing", "setup_intent.succeeded", "", map[string]any{"object": "setup_intent", "customer": "cus_1"}, nil); code != 204 {
		t.Fatalf("webhook: %d", code)
	}
	if p := page("/billing"); !strings.Contains(p, "Your account: <strong>Member</strong>") || !strings.Contains(p, "Visa ending 4242, expires 12/2030") ||
		!strings.Contains(p, "Manage billing") || strings.Contains(p, `action="/billing/card"`) || !strings.Contains(p, "shops pay 2%") {
		t.Fatalf("member page: %s", p)
	}
	if p := page("/"); !strings.Contains(p, `<a href="/billing">Member plan</a>`) || !strings.Contains(p, "of 50 apps used") {
		t.Fatal("member dashboard")
	}
	r, _ = c.PostForm(e.base+"/billing/portal", url.Values{"csrf": {e.csrf(c, "/billing")}})
	if r.Header.Get("Location") != "https://billing.stripe.com/p/session/x" {
		t.Fatal("portal")
	}

	// A connected account's event on this endpoint is ignored, and so are
	// events that change nothing here.
	fb.cards = nil
	lists := fb.cardLists
	billingWebhook(t, e.base, "whsec_billing", "payment_method.detached", "acct_someone", map[string]any{"customer": nil}, map[string]any{"customer": "cus_1"})
	billingWebhook(t, e.base, "whsec_billing", "invoice.paid", "", map[string]any{"customer": "cus_1"}, nil)
	if fb.cardLists != lists || !strings.Contains(page("/billing"), "<strong>Member</strong>") {
		t.Fatal("an ignored event changed the plan")
	}
	// Removed in the portal: the detached payment method names its
	// customer only in the previous attributes.
	if code := billingWebhook(t, e.base, "whsec_billing", "payment_method.detached", "", map[string]any{"object": "payment_method", "customer": nil},
		map[string]any{"customer": "cus_1"}); code != 204 {
		t.Fatalf("detached: %d", code)
	}
	if p := page("/billing"); !strings.Contains(p, "<strong>Free</strong>") || !strings.Contains(p, `action="/billing/card"`) {
		t.Fatalf("after removal: %s", p)
	}
	// Saved again, from the checkout session; back from Stripe the page
	// reads it at once.
	fb.cards = []map[string]any{card("1881", 3, 2031)}
	billingWebhook(t, e.base, "whsec_billing", "checkout.session.completed", "", map[string]any{"object": "checkout.session", "mode": "setup", "customer": "cus_1"}, nil)
	if p := page("/billing"); !strings.Contains(p, "<strong>Member</strong>") || !strings.Contains(p, "ending 1881") {
		t.Fatalf("after checkout: %s", p)
	}
	// Only an expired card left: free, and the page says so.
	fb.cards = []map[string]any{card("0005", 1, 2020)}
	if code := billingWebhook(t, e.base, "whsec_billing", "customer.updated", "", map[string]any{"object": "customer", "id": "cus_1"}, nil); code != 204 {
		t.Fatalf("customer.updated: %d", code)
	}
	if p := page("/billing"); !strings.Contains(p, "<strong>Free</strong>") || !strings.Contains(p, "card expired") {
		t.Fatalf("expired card: %s", p)
	}
	fb.cards = []map[string]any{card("0005", 1, 2020), card("4444", 8, 2032)}
	if p := page("/billing?done=1"); !strings.Contains(p, "<strong>Member</strong>") || !strings.Contains(p, "ending 4444") {
		t.Fatalf("back from Stripe: %s", p)
	}
	if code := billingWebhook(t, e.base, "whsec_billing", "payment_method.attached", "", map[string]any{"object": "payment_method", "customer": "cus_other"}, nil); code != 204 {
		t.Fatalf("unknown customer: %d", code)
	}
	if fb.customers != 1 {
		t.Fatalf("created %d customers", fb.customers)
	}
}

// An operator's grant makes a member by agreement without Stripe, and a
// card sync does not end it.
func TestGrantedPlan(t *testing.T) {
	e := newEnv(t)
	fb := &fakeBilling{}
	e.site.Billing = web.Billing{Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: fb.server(t).URL}, WebhookSecret: "whsec_billing"}
	c := e.browser()
	e.signup(c, "wade@shop.ae")
	acc := e.accounts()
	u, _ := acc.UserByEmail("wade@shop.ae")
	acc.SetCustomer(u.ID, "cus_1")
	if err := acc.GrantPlan(u.ID, "member"); err != nil {
		t.Fatal(err)
	}
	r, _ := c.Get(e.base + "/billing?done=1")
	if p := read(t, r); !strings.Contains(p, "<strong>Member by agreement</strong>") {
		t.Fatalf("granted plan: %s", p)
	}
	// Nothing at Stripe to manage: the page says so instead of a Manage
	// billing button that only reloads the page.
	r, _ = c.Get(e.base + "/billing")
	p := read(t, r)
	if strings.Contains(p, "Manage billing") || strings.Contains(p, `action="/billing/card"`) || !strings.Contains(p, "granted") {
		t.Fatalf("granted billing page: %s", p)
	}
	r, _ = c.PostForm(e.base+"/billing/portal", url.Values{"csrf": {e.csrf(c, "/billing")}})
	if r.StatusCode != 303 || r.Header.Get("Location") != "/billing?notice=granted" {
		t.Fatalf("portal on a granted plan: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	r, _ = c.Get(e.base + "/billing?notice=granted")
	if p := read(t, r); !strings.Contains(p, "no card, invoice or subscription to manage") {
		t.Fatal("granted notice not shown")
	}
	if err := acc.GrantPlan(u.ID, "gold"); err == nil {
		t.Fatal("unknown plan accepted")
	}
}

// Pages that call Stripe with the platform's key do so a limited number of
// times per account, so one account cannot use up Stripe's rate limit for
// everyone's payments.
func TestBillingPageCallsStripeSparingly(t *testing.T) {
	e := newEnv(t)
	fb := &fakeBilling{}
	e.site.Billing = web.Billing{Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: fb.server(t).URL}, WebhookSecret: "whsec_billing"}
	c := e.browser()
	e.signup(c, "tala@shop.ae")
	token := e.csrf(c, "/billing")
	if r, _ := c.PostForm(e.base+"/billing/card", url.Values{"csrf": {token}}); r.StatusCode != 303 {
		t.Fatalf("card checkout: %d", r.StatusCode)
	}
	for i := 0; i < 60; i++ {
		r, err := c.Get(e.base + "/billing?done=1")
		if err != nil || r.StatusCode != 200 {
			t.Fatalf("billing page %d: %v", i, err)
		}
		read(t, r)
	}
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if fb.cardLists == 0 || fb.cardLists > 30 {
		t.Fatalf("60 page loads made %d calls to Stripe", fb.cardLists)
	}
}
