package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

func TestOperatorPanel(t *testing.T) {
	e := newEnv(t)
	e.site.Operators = []string{"ops@renoxium.ae"}
	merchant := e.browser()
	e.signup(merchant, "zara@shop.ae")
	e.publishDef(merchant, "zara-shop", &appdef.App{Templates: map[string]string{"t": "shop"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}})

	ops := e.browser()
	e.signup(ops, "ops@renoxium.ae")
	if r, _ := ops.Get(e.base + "/operator"); r.StatusCode != 404 {
		t.Fatalf("unconfirmed operator: %d", r.StatusCode)
	}
	link := linkRE.FindString(e.mail.last(t, "ops@renoxium.ae", "verify_email").Text)
	http.Get(link)
	if r, _ := merchant.Get(e.base + "/operator"); r.StatusCode != 404 {
		t.Fatalf("merchant: %d", r.StatusCode)
	}

	// An abuse report lands in the panel.
	reporter := e.browser()
	reporter.PostForm(e.base+"/abuse", url.Values{"csrf": {e.csrf(reporter, "/abuse")}, "address": {"zara-shop.renoxium.app"}, "details": {"Sells fake perfume"}})

	r, _ := ops.Get(e.base + "/operator")
	page := read(t, r)
	for _, want := range []string{"Accounts", ">2<", "zara@shop.ae", "zara-shop", "Sells fake perfume", "(1 open)", "AED 0"} {
		if !strings.Contains(page, want) {
			t.Errorf("operator page lacks %q", want)
		}
	}
	csrf := e.csrf(ops, "/operator")
	post := func(path string, form url.Values) int {
		form.Set("csrf", csrf)
		r, _ := ops.PostForm(e.base+path, form)
		read(t, r)
		return r.StatusCode
	}
	// Grant a plan.
	u, _ := e.acc.UserByEmail("zara@shop.ae")
	if code := post("/operator/plan", url.Values{"user": {u.ID}, "plan": {"business"}}); code != 303 {
		t.Fatalf("grant: %d", code)
	}
	if e.acc.Plan(u.ID) != "business" {
		t.Fatal("plan not granted")
	}
	// Suspend and bring back.
	serve := func() int {
		w := httptest.NewRecorder()
		e.p.ServeApp(w, httptest.NewRequest("GET", "http://zara-shop.renoxium.test/", nil))
		return w.Code
	}
	if serve() != 200 {
		t.Fatal("app not live")
	}
	post("/operator/suspend", url.Values{"app": {"zara-shop"}, "action": {"suspend"}, "reason": {"fake goods"}})
	if code := serve(); code == 200 {
		t.Fatal("suspended app still serves")
	}
	if r, _ := ops.Get(e.base + "/operator?q=zara"); !strings.Contains(read(t, r), "suspended: fake goods") {
		t.Fatal("suspension not shown")
	}
	post("/operator/suspend", url.Values{"app": {"zara-shop"}, "action": {"unsuspend"}})
	if serve() != 200 {
		t.Fatal("app not back")
	}
	// Resolve the report; forms without CSRF are refused.
	if code := post("/operator/abuse/1/resolve", url.Values{}); code != 303 {
		t.Fatalf("resolve: %d", code)
	}
	if r, _ := ops.PostForm(e.base+"/operator/plan", url.Values{"user": {u.ID}, "plan": {"free"}}); r.StatusCode != 403 {
		t.Fatalf("plan without csrf: %d", r.StatusCode)
	}
	if r, _ := ops.Get(e.base + "/operator"); !strings.Contains(read(t, r), "(0 open)") {
		t.Fatal("report not resolved")
	}
}
