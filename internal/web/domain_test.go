package web_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

func TestDomainPage(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "lina@shop.ae")
	e.publishDef(owner, "lina-shop", &appdef.App{Templates: map[string]string{"t": "shop"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}})
	r, _ := owner.Get(e.base + "/apps/lina-shop")
	if page := read(t, r); !strings.Contains(page, "/apps/lina-shop/domain") {
		t.Fatal("overview has no domain link")
	}
	r, _ = owner.Get(e.base + "/apps/lina-shop/domain")
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "not available on this Renoxium server") {
		t.Fatalf("domain page: %d %s", r.StatusCode, page)
	}
	other := e.browser()
	e.signup(other, "x@other.ae")
	if r, _ := other.Get(e.base + "/apps/lina-shop/domain"); r.StatusCode != 404 {
		t.Fatalf("stranger: %d", r.StatusCode)
	}
}

func TestNotificationsPage(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "noor@shop.ae")
	e.publishDef(owner, "noor-shop", &appdef.App{Templates: map[string]string{"t": "shop"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}})
	r, _ := owner.Get(e.base + "/apps/noor-shop/notifications")
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "noor@shop.ae") || !strings.Contains(page, "not available on this Renoxium server") {
		t.Fatalf("notifications page: %d %s", r.StatusCode, page)
	}
}

func TestJobsOnAppPage(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "jana@shop.ae")
	e.publishDef(owner, "jana-shop", &appdef.App{Templates: map[string]string{"t": "shop"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
		Scripts: map[string]string{"r": `function handle(req) { return {text: 'ok'} }`},
		Jobs:    []appdef.Job{{Name: "nightly", Script: "r", DailyAt: "21:00"}}})
	r, _ := owner.Get(e.base + "/apps/jana-shop/status")
	if page := read(t, r); !strings.Contains(page, "Scheduled jobs") || !strings.Contains(page, "daily at 21:00 (Asia/Dubai)") {
		t.Fatalf("app page: %s", page)
	}
}

func TestConnectionsPage(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "rami@shop.ae")
	e.publishDef(owner, "rami-shop", &appdef.App{Templates: map[string]string{"t": "shop"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
		Hosts:   []appdef.Host{{Host: "api.courier.ae", Purpose: "Delivery prices"}},
		Secrets: []appdef.Secret{{Name: "COURIER_KEY", Host: "api.courier.ae", Header: "Authorization", Prefix: "Bearer", Help: "Courier portal, API page"}}})
	page := func() string {
		r, _ := owner.Get(e.base + "/apps/rami-shop/connections")
		return read(t, r)
	}
	if p := page(); !strings.Contains(p, "api.courier.ae") || !strings.Contains(p, "Delivery prices") || !strings.Contains(p, ">Allow</button>") ||
		!strings.Contains(p, "Courier portal, API page") {
		t.Fatalf("page: %s", p)
	}
	post := func(form url.Values) int {
		form.Set("csrf", e.csrf(owner, "/apps/rami-shop/connections"))
		r, _ := owner.PostForm(e.base+"/apps/rami-shop/connections", form)
		return r.StatusCode
	}
	if code := post(url.Values{"action": {"allow"}, "host": {"api.courier.ae"}}); code != 303 {
		t.Fatalf("allow: %d", code)
	}
	if code := post(url.Values{"action": {"secret"}, "name": {"COURIER_KEY"}, "value": {"sk_live_abcdefgh9876"}}); code != 303 {
		t.Fatalf("secret: %d", code)
	}
	if p := page(); !strings.Contains(p, "by rami@shop.ae") || !strings.Contains(p, "ends in 9876") || strings.Contains(p, "sk_live_abcdefgh9876") {
		t.Fatalf("after saving: %s", p)
	}
	// Test calls from the preview are off until the owner turns them on.
	if p := page(); !strings.Contains(p, "Turn on for 2 hours") || e.p.PreviewFetchOn("rami-shop") {
		t.Fatalf("test calls before turning on: %s", p)
	}
	if code := post(url.Values{"action": {"preview-on"}}); code != 303 || !e.p.PreviewFetchOn("rami-shop") {
		t.Fatalf("turn on: %d", code)
	}
	if p := page(); !strings.Contains(p, "On until") {
		t.Fatalf("after turning on: %s", p)
	}
	if code := post(url.Values{"action": {"preview-off"}}); code != 303 || e.p.PreviewFetchOn("rami-shop") {
		t.Fatalf("turn off: %d", code)
	}
	other := e.browser()
	e.signup(other, "x@other.ae")
	if r, _ := other.Get(e.base + "/apps/rami-shop/connections"); r.StatusCode != 404 {
		t.Fatalf("stranger: %d", r.StatusCode)
	}
}
