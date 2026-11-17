package web_test

import (
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
