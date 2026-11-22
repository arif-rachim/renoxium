package web_test

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
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

// A paid work app includes 5 staff; each one more adds a seat to its
// subscription, up to 10, and removing one takes it off again.
func TestWorkAppSeats(t *testing.T) {
	e := newEnvPlans(t, true, func(c *platform.Config) { c.WorkSeats = true })
	fb := &fakeBilling{}
	e.site.Billing = web.Billing{Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: fb.server(t).URL},
		PriceWorkApp: "price_work", PriceWorkSeat: "price_seat", WebhookSecret: "whsec_billing"}
	c := e.browser()
	e.signup(c, "huda@office.ae")
	post := func(path string, form url.Values) (int, string) {
		t.Helper()
		form.Set("csrf", e.csrf(c, path))
		r, err := c.PostForm(e.base+path, form)
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode, read(t, r)
	}
	page := func(path string) string {
		t.Helper()
		r, _ := c.Get(e.base + path)
		return read(t, r)
	}
	sess := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	if _, errText := call(t, sess, "create_app", map[string]any{"app": "huda-office"}); errText != "" {
		t.Fatal(errText)
	}
	post("/apps/huda-office/plan", url.Values{"action": {"subscribe"}})
	fb.status, fb.price, fb.metadata = "active", "price_work", map[string]string{"rx_app": "huda-office"}
	page("/apps/huda-office/plan?done=1")
	if got := e.p.StaffLimit("huda-office"); got != platform.WorkAppMaxUsers {
		t.Fatalf("staff limit %d", got)
	}
	if p := page("/apps/huda-office/staff"); !strings.Contains(p, "5 staff members are included") || !strings.Contains(p, "AED 25 a month, up to 10") {
		t.Fatalf("staff page: %s", p)
	}
	add := func(i int) (int, string) {
		return post("/apps/huda-office/staff", url.Values{"email": {"s" + strconv.Itoa(i) + "@office.ae"}})
	}
	for i := 1; i <= 5; i++ {
		if code, body := add(i); code != 303 {
			t.Fatalf("staff %d: %d %s", i, code, body)
		}
	}
	if len(fb.seatCalls) != 0 {
		t.Fatalf("included staff changed the subscription: %v", fb.seatCalls)
	}

	// The 6th adds a seat item, the 7th raises it.
	add(6)
	add(7)
	if fb.seatPrice != "price_seat" || fb.seats != 2 || len(fb.seatCalls) != 2 || fb.seatCalls[0] != "POST /v1/subscription_items 1" ||
		fb.seatCalls[1] != "POST /v1/subscription_items/si_seat 2" {
		t.Fatalf("seats: %d %v", fb.seats, fb.seatCalls)
	}
	if p := page("/apps/huda-office/plan"); !strings.Contains(p, "AED 149 a month: AED 99 with 5 staff, plus 2 more at AED 25") {
		t.Fatalf("plan page: %s", p)
	}
	if p := page("/apps/huda-office/staff"); !strings.Contains(p, "Adding a staff member adds AED 25 a month") {
		t.Fatalf("staff page past 5: %s", p)
	}

	// Removing one lowers it; down to 5 removes the item.
	remove := func(addr string) {
		t.Helper()
		r, err := c.PostForm(e.base+"/apps/huda-office/staff/remove", url.Values{"csrf": {e.csrf(c, "/apps/huda-office/staff")}, "email": {addr}})
		if err != nil || r.StatusCode != 303 {
			t.Fatalf("remove %s: %v %v", addr, err, r)
		}
	}
	remove("s7@office.ae")
	if fb.seats != 1 {
		t.Fatalf("after removing one: %d", fb.seats)
	}
	remove("s6@office.ae")
	if fb.seats != 0 || fb.seatCalls[len(fb.seatCalls)-1] != "DELETE /v1/subscription_items/si_seat " {
		t.Fatalf("back to 5: %d %v", fb.seats, fb.seatCalls)
	}

	// When Stripe refuses, the staff member is not added.
	fb.failSeats = true
	if code, body := add(6); code != 503 || !strings.Contains(body, "Stripe could not add this staff member") {
		t.Fatalf("Stripe down: %d", code)
	}
	if staff, _ := e.p.Staff("huda-office"); len(staff) != 5 {
		t.Fatalf("staff after a failed seat: %d", len(staff))
	}
	fb.failSeats = false

	// At most 10.
	for i := 6; i <= 10; i++ {
		if code, body := add(i); code != 303 {
			t.Fatalf("staff %d: %d %s", i, code, body)
		}
	}
	if fb.seats != 5 {
		t.Fatalf("seats at 10 staff: %d", fb.seats)
	}
	if code, body := add(11); code != 400 || !strings.Contains(body, "at most 10 staff members") {
		t.Fatalf("11th staff: %d", code)
	}
}
