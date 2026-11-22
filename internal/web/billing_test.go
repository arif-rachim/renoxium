package web_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
}

func (f *fakeBilling) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
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
				data = append(data, map[string]any{"id": "sub_1", "customer": "cus_1", "status": f.status, "metadata": f.metadata,
					"items": map[string]any{"data": []any{map[string]any{"price": map[string]any{"id": f.price}, "current_period_end": 1900000000}}}})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
		case "/v1/billing_portal/sessions":
			w.Write([]byte(`{"url":"https://billing.stripe.com/p/session/x"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBilling(t *testing.T) {
	e := newEnv(t)
	fb := &fakeBilling{}
	e.site.Billing = web.Billing{Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: fb.server(t).URL},
		PricePro: "price_pro", PriceBusiness: "price_business", WebhookSecret: "whsec_billing"}
	c := e.browser()
	e.signup(c, "vera@shop.ae")
	page := func() string {
		r, _ := c.Get(e.base + "/billing")
		return read(t, r)
	}
	if p := page(); !strings.Contains(p, "Current plan: <strong>Pay per app</strong>") || !strings.Contains(p, "shops pay 2%") || !strings.Contains(p, "AED 99 a month") ||
		strings.Contains(p, "Choose Pro") {
		t.Fatalf("free page: %s", p)
	}
	if r, _ := c.Get(e.base + "/"); !strings.Contains(read(t, r), `<a href="/billing">Free plan</a>`) {
		t.Fatal("dashboard plan")
	}

	// Choosing Pro creates the customer once and sends the owner to Stripe Checkout.
	r, _ := c.PostForm(e.base+"/billing/checkout", url.Values{"csrf": {e.csrf(c, "/billing")}, "plan": {"pro"}})
	if r.StatusCode != 303 || r.Header.Get("Location") != "https://checkout.stripe.com/c/pay/cs_sub" {
		t.Fatalf("checkout: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	co := fb.checkouts[0]
	if co.Get("mode") != "subscription" || co.Get("customer") != "cus_1" || co.Get("line_items[0][price]") != "price_pro" ||
		co.Get("success_url") != e.base+"/billing?done=1" {
		t.Fatalf("checkout session: %v", co)
	}
	if r, _ := c.PostForm(e.base+"/billing/checkout", url.Values{"plan": {"pro"}}); r.StatusCode != 403 {
		t.Fatalf("checkout without csrf: %d", r.StatusCode)
	}

	// Stripe confirms the subscription by webhook.
	fb.status, fb.price = "active", "price_pro"
	webhook := func(secret, typ, account string) int {
		payload, _ := json.Marshal(map[string]any{"id": "evt_1", "type": typ, "account": account, "data": map[string]any{"object": map[string]any{"customer": "cus_1"}}})
		req, _ := http.NewRequest("POST", e.base+"/stripe/billing-webhook", strings.NewReader(string(payload)))
		req.Header.Set("Stripe-Signature", stripe.Sign(payload, secret, time.Now()))
		resp, _ := http.DefaultClient.Do(req)
		return resp.StatusCode
	}
	if code := webhook("whsec_wrong", "customer.subscription.created", ""); code != 400 {
		t.Fatalf("bad signature: %d", code)
	}
	if code := webhook("whsec_billing", "customer.subscription.created", ""); code != 204 {
		t.Fatalf("webhook: %d", code)
	}
	if p := page(); !strings.Contains(p, "Current plan: <strong>Pro</strong>") || !strings.Contains(p, "Manage billing") || !strings.Contains(p, "renews") {
		t.Fatalf("pro page: %s", p)
	}
	// Already paying: choosing a plan again opens the portal instead of a second subscription.
	r, _ = c.PostForm(e.base+"/billing/checkout", url.Values{"csrf": {e.csrf(c, "/billing")}, "plan": {"business"}})
	if r.Header.Get("Location") != "https://billing.stripe.com/p/session/x" || len(fb.checkouts) != 1 {
		t.Fatalf("second checkout: %s", r.Header.Get("Location"))
	}
	r, _ = c.PostForm(e.base+"/billing/portal", url.Values{"csrf": {e.csrf(c, "/billing")}})
	if r.Header.Get("Location") != "https://billing.stripe.com/p/session/x" {
		t.Fatal("portal")
	}

	// A connected account's event on this endpoint is ignored.
	fb.status = "canceled"
	webhook("whsec_billing", "customer.subscription.deleted", "acct_someone")
	if p := page(); !strings.Contains(p, "<strong>Pro</strong>") {
		t.Fatal("connected-account event changed the plan")
	}
	// Cancellation returns the account to Free.
	webhook("whsec_billing", "customer.subscription.deleted", "")
	if p := page(); !strings.Contains(p, "<strong>Pay per app</strong>") {
		t.Fatalf("after cancel: %s", p)
	}
	if fb.customers != 1 {
		t.Fatalf("created %d customers", fb.customers)
	}
}

// An operator's grant gives a plan without Stripe and survives syncs until
// a paid plan replaces it.
func TestGrantedPlan(t *testing.T) {
	e := newEnv(t)
	fb := &fakeBilling{}
	e.site.Billing = web.Billing{Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: fb.server(t).URL},
		PricePro: "price_pro", PriceBusiness: "price_business", WebhookSecret: "whsec_billing"}
	c := e.browser()
	e.signup(c, "wade@shop.ae")
	acc := e.accounts()
	u, _ := acc.UserByEmail("wade@shop.ae")
	if err := acc.GrantPlan(u.ID, "business"); err != nil {
		t.Fatal(err)
	}
	r, _ := c.Get(e.base + "/billing?done=1")
	if p := read(t, r); !strings.Contains(p, "<strong>Business</strong>") {
		t.Fatalf("granted plan: %s", p)
	}
	// No Stripe subscription to manage: the page says so instead of a
	// Manage billing button that only reloads the page.
	r, _ = c.Get(e.base + "/billing")
	p := read(t, r)
	if strings.Contains(p, "Manage billing") || !strings.Contains(p, "granted") {
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
	r, _ = c.PostForm(e.base+"/billing/checkout", url.Values{"csrf": {e.csrf(c, "/billing")}, "plan": {"pro"}})
	if r.Header.Get("Location") != "https://checkout.stripe.com/c/pay/cs_sub" {
		t.Fatalf("checkout on a granted plan: %s", r.Header.Get("Location"))
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
	e.site.Billing = web.Billing{Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: fb.server(t).URL},
		PricePro: "price_pro", PriceBusiness: "price_business", WebhookSecret: "whsec_billing"}
	c := e.browser()
	e.signup(c, "tala@shop.ae")
	token := e.csrf(c, "/billing")
	if r, _ := c.PostForm(e.base+"/billing/checkout", url.Values{"csrf": {token}, "plan": {"pro"}}); r.StatusCode != 303 {
		t.Fatalf("checkout: %d", r.StatusCode)
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
	if fb.lists == 0 || fb.lists > 30 {
		t.Fatalf("60 page loads made %d calls to Stripe", fb.lists)
	}
}
