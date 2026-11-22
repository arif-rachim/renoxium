package web_test

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/stripe"
	"github.com/arif-rachim/renoxium/internal/web"
)

// A free account's app with a database goes live once the owner makes it a
// shop (with Stripe) or a work app (with a subscription); deleting a work app
// cancels its subscription.
func TestAppPlanShopOrWorkApp(t *testing.T) {
	e := newEnvPlans(t, true)
	fb := &fakeBilling{}
	e.site.Billing = web.Billing{Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: fb.server(t).URL},
		PriceWorkApp: "price_work", WebhookSecret: "whsec_billing"}
	c := e.browser()
	e.signup(c, "wafa@clinic.ae")
	page := func(path string) string {
		t.Helper()
		r, err := c.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		return read(t, r)
	}
	post := func(path string, form url.Values) (int, string) {
		t.Helper()
		form.Set("csrf", e.csrf(c, path))
		r, err := c.PostForm(e.base+path, form)
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode, r.Header.Get("Location")
	}

	def := &appdef.App{
		Tables:    []appdef.Table{{Name: "patients", Columns: []appdef.Column{{Name: "name", Type: "text"}}}},
		Templates: map[string]string{"t": "queue"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
	}
	raw, _ := json.Marshal(def)
	var defMap map[string]any
	json.Unmarshal(raw, &defMap)
	sess := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	for _, step := range []struct {
		tool string
		args map[string]any
	}{{"create_app", map[string]any{"app": "queue"}}, {"apply_change", map[string]any{"app": "queue", "definition": defMap}}} {
		if _, errText := call(t, sess, step.tool, step.args); errText != "" {
			t.Fatalf("%s: %s", step.tool, errText)
		}
	}

	// Without a plan the publish page sends the owner to choose one.
	if p := page("/apps/queue/publish?version=1"); !strings.Contains(p, "Choose a plan") || strings.Contains(p, "<button>Publish</button>") {
		t.Fatalf("publish page without a plan: %s", p)
	}
	if p := page("/apps/queue/plan"); !strings.Contains(p, "Make it a shop") || !strings.Contains(p, "Make it a work app, AED 99 a month") {
		t.Fatalf("plan page: %s", p)
	}

	// A shop needs Stripe before it goes live.
	if code, _ := post("/apps/queue/plan", url.Values{"action": {"shop"}}); code != 303 {
		t.Fatalf("make shop: %d", code)
	}
	if p := page("/apps/queue/plan"); !strings.Contains(p, "Connect Stripe") {
		t.Fatalf("shop without Stripe: %s", p)
	}

	// A work app subscribes through Stripe Checkout, for this app.
	code, loc := post("/apps/queue/plan", url.Values{"action": {"subscribe"}})
	if code != 303 || loc != "https://checkout.stripe.com/c/pay/cs_sub" {
		t.Fatalf("subscribe: %d %s", code, loc)
	}
	co := fb.checkouts[0]
	if co.Get("mode") != "subscription" || co.Get("line_items[0][price]") != "price_work" || co.Get("subscription_data[metadata][rx_app]") != "queue" ||
		co.Get("success_url") != e.base+"/apps/queue/plan?done=1" {
		t.Fatalf("checkout: %v", co)
	}
	fb.status, fb.price, fb.metadata = "active", "price_work", map[string]string{"rx_app": "queue"}
	if p := page("/apps/queue/plan?done=1"); !strings.Contains(p, "Manage subscription") || !strings.Contains(p, "active") {
		t.Fatalf("paid work app: %s", p)
	}
	if b, _ := e.acc.Billing(mustUser(t, e, "wafa@clinic.ae")); b.Plan != "free" {
		t.Fatalf("a work app changed the account's plan: %s", b.Plan)
	}
	if p := page("/apps/queue/publish?version=1"); !strings.Contains(p, "<button>Publish</button>") {
		t.Fatalf("publish page for a paid work app: %s", p)
	}
	if code, _ := post("/apps/queue/publish?version=1", url.Values{"version": {"1"}}); code != 200 {
		t.Fatalf("publish: %d", code)
	}
	if info, _ := e.p.Info("queue"); info.LiveVersion != 1 {
		t.Fatalf("live version %d", info.LiveVersion)
	}

	// Deleting the app cancels its subscription.
	code, _ = post("/apps/queue/delete", url.Values{"confirm": {"queue"}, "password": {"correct horse battery"}})
	if code != 200 || len(fb.cancels) != 1 || fb.cancels[0] != "sub_1" {
		t.Fatalf("delete: %d, cancelled %v", code, fb.cancels)
	}
}

func mustUser(t *testing.T, e *env, email string) string {
	t.Helper()
	u, err := e.acc.UserByEmail(email)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}
