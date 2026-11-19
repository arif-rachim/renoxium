package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
)

func TestOperatorResources(t *testing.T) {
	e := newEnv(t)
	e.site.Operators = []string{"ops@renoxium.ae"}
	merchant := e.browser()
	e.signup(merchant, "zara@shop.ae")
	http.Get(linkRE.FindString(e.mail.last(t, "zara@shop.ae", "verify_email").Text))
	e.publishDef(merchant, "zara-shop", &appdef.App{
		Scripts:   map[string]string{"busy": `function handle(req) { const end = Date.now() + 30; while (Date.now() < end) {} return {text: "x".repeat(2000)} }`},
		Templates: map[string]string{"t": "shop"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}, {Method: "GET", Path: "/busy/:n", Script: "busy"}}})
	for range 5 {
		w := httptest.NewRecorder()
		e.p.ServeApp(w, httptest.NewRequest("GET", "http://zara-shop.renoxium.test/busy/1", nil))
		if w.Code != 200 {
			t.Fatalf("busy: %d", w.Code)
		}
	}
	ops := e.browser()
	e.signup(ops, "ops@renoxium.ae")
	http.Get(linkRE.FindString(e.mail.last(t, "ops@renoxium.ae", "verify_email").Text))

	// Only the operator: everyone else gets 404, forms too.
	csrfMerchant := e.csrf(merchant, "/")
	for _, c := range []*http.Client{merchant, e.browser()} {
		for _, path := range []string{"/operator/resources", "/operator/resources/zara-shop"} {
			if r, _ := c.Get(e.base + path); r.StatusCode != 404 {
				t.Fatalf("%s for a non-operator: %d", path, r.StatusCode)
			}
		}
		r, _ := c.PostForm(e.base+"/operator/resources/zara-shop/action", url.Values{"csrf": {csrfMerchant}, "action": {"suspend"}})
		if r.StatusCode != 404 {
			t.Fatalf("action for a non-operator: %d", r.StatusCode)
		}
	}

	r, _ := ops.Get(e.base + "/operator/resources")
	page := read(t, r)
	for _, want := range []string{"Resources", "Last 24 hours", "Workers busy now", "Slot waits", "Memory kills", "Timeouts", "zara-shop", "zara@shop.ae",
		"What the flags mean", "Noisy neighbour", "Unprofitable heavy", "Error-prone", "Free plan over fair use", "Unit prices and fair use", `name="cpu_hour"`,
		"/operator/resources/zara-shop?window=24h"} {
		if !strings.Contains(page, want) {
			t.Errorf("resources page lacks %q", want)
		}
	}
	for _, q := range []string{"?window=7d&sort=cost", "?window=30d&sort=margin&all=1", "?sort=waits"} {
		if r, _ := ops.Get(e.base + "/operator/resources" + q); r.StatusCode != 200 || !strings.Contains(read(t, r), "zara-shop") {
			t.Errorf("%s: %d", q, r.StatusCode)
		}
	}
	if r, _ := ops.Get(e.base + "/operator"); !strings.Contains(read(t, r), `href="/operator/resources"`) {
		t.Error("no link from /operator")
	}

	r, _ = ops.Get(e.base + "/operator/resources/zara-shop?window=7d")
	page = read(t, r)
	for _, want := range []string{"zara-shop", "Script time per hour", "Requests per hour", "GET /busy/:n", "Email the owner", "Throttle for a while", "Suspend",
		"uses a lot of the shared server", "/billing", "Operator audit"} {
		if !strings.Contains(page, want) {
			t.Errorf("app page lacks %q", want)
		}
	}
	if r, _ := ops.Get(e.base + "/operator/resources/no-such-app"); r.StatusCode != 404 {
		t.Errorf("unknown app: %d", r.StatusCode)
	}

	csrf := e.csrf(ops, "/operator/resources/zara-shop")
	post := func(path string, form url.Values, withCSRF bool) (int, string) {
		if withCSRF {
			form.Set("csrf", csrf)
		}
		r, _ := ops.PostForm(e.base+path, form)
		return r.StatusCode, read(t, r)
	}
	// CSRF is required for every action and for the prices.
	if code, _ := post("/operator/resources/zara-shop/action", url.Values{"action": {"throttle"}, "slots": {"1"}, "hours": {"1"}}, false); code != 403 {
		t.Fatalf("throttle without csrf: %d", code)
	}
	if code, _ := post("/operator/resources/prices", url.Values{"cpu_hour": {"1"}}, false); code != 403 {
		t.Fatalf("prices without csrf: %d", code)
	}

	// Prices: saved, validated.
	if code, _ := post("/operator/resources/prices", url.Values{"cpu_hour": {"0.2"}, "email": {"0.003"}}, true); code != 303 {
		t.Fatalf("prices: %d", code)
	}
	if code, _ := post("/operator/resources/prices", url.Values{"cpu_hour": {"-3"}}, true); code != 400 {
		t.Fatalf("negative price: %d", code)
	}
	if r, _ := ops.Get(e.base + "/operator/resources"); !strings.Contains(read(t, r), `name="cpu_hour" inputmode="decimal" value="0.2"`) {
		t.Error("saved price not shown")
	}

	// Throttle: the owner sees why in the admin panel; logged and audited.
	if code, body := post("/operator/resources/zara-shop/action", url.Values{"action": {"throttle"}, "slots": {"1"}, "per_minute": {"30"}, "hours": {"24"},
		"reason": {"busy loops"}}, true); code != 303 {
		t.Fatalf("throttle: %d %s", code, body)
	}
	if _, ok := e.p.ThrottleOf("zara-shop"); !ok {
		t.Fatal("not throttled")
	}
	if r, _ := merchant.Get(e.base + "/apps/zara-shop"); !strings.Contains(read(t, r), "Limited by Renoxium.") {
		t.Error("the owner is not told about the throttle")
	}
	if code, _ := post("/operator/resources/zara-shop/action", url.Values{"action": {"throttle"}, "slots": {"0"}, "hours": {"24"}}, true); code != 400 {
		t.Errorf("a throttle that limits nothing: %d", code)
	}
	// Email the owner.
	if code, _ := post("/operator/resources/zara-shop/action", url.Values{"action": {"notify"}, "subject": {"Your app uses a lot"}, "text": {"Please make it lighter."}}, true); code != 303 {
		t.Fatalf("notify: %d", code)
	}
	if code, _ := post("/operator/resources/zara-shop/action", url.Values{"action": {"notify"}, "subject": {""}, "text": {"x"}}, true); code != 400 {
		t.Errorf("notify without subject: %d", code)
	}
	// End the throttle; suspend and bring back.
	post("/operator/resources/zara-shop/action", url.Values{"action": {"unthrottle"}}, true)
	if _, ok := e.p.ThrottleOf("zara-shop"); ok {
		t.Fatal("still throttled")
	}
	if r, _ := merchant.Get(e.base + "/apps/zara-shop"); strings.Contains(read(t, r), "Limited by Renoxium.") {
		t.Error("throttle notice after the end")
	}
	post("/operator/resources/zara-shop/action", url.Values{"action": {"suspend"}, "reason": {"too heavy"}}, true)
	w := httptest.NewRecorder()
	e.p.ServeApp(w, httptest.NewRequest("GET", "http://zara-shop.renoxium.test/", nil))
	if w.Code == 200 {
		t.Fatal("suspended app still serves")
	}
	post("/operator/resources/zara-shop/action", url.Values{"action": {"unsuspend"}}, true)

	audit, _ := e.p.AuditLog("zara-shop", 10)
	var actions []string
	for _, a := range audit {
		actions = append(actions, a.Action)
		if a.Operator != "ops@renoxium.ae" {
			t.Errorf("audit operator %q", a.Operator)
		}
	}
	if got := strings.Join(actions, ","); got != "unsuspend,suspend,unthrottle,notify,throttle" {
		t.Errorf("audit %s", got)
	}
	logs, _ := e.p.Logs("zara-shop", applog.Filter{Source: "operator"})
	if len(logs) != 5 || !strings.Contains(logs[len(logs)-1].Message, "throttled until") {
		t.Errorf("app log: %+v", logs)
	}
	if r, _ := ops.Get(e.base + "/operator/resources/zara-shop"); !strings.Contains(read(t, r), "emailed the owner: Your app uses a lot") {
		t.Error("audit not shown on the app page")
	}
}
