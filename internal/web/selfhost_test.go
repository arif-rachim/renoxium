package web_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// On the owner's own server the site is only their app's admin panel: the
// rest of Renoxium (sign-up, templates, billing, operator pages, other apps)
// is not there, and the app's console pages that need Renoxium's service are
// not either.
func TestSingleAppSite(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "sara@shop.ae")
	def := &appdef.App{
		Tables:    []appdef.Table{{Name: "products", Columns: []appdef.Column{{Name: "name", Type: "text"}}}},
		Seed:      map[string][]map[string]any{"products": {{"name": "Oud"}}},
		Templates: map[string]string{"t": "x"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
	}
	publish(e, c, "sara-shop", def)
	e.site.SingleApp = "sara-shop"

	noRedirect := *c
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	status := func(path string) (int, string, http.Header) {
		t.Helper()
		r, err := noRedirect.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode, read(t, r), r.Header
	}
	if code, _, h := status("/"); code != http.StatusFound || h.Get("Location") != "/apps/sara-shop" || h.Get("X-Robots-Tag") != "noindex" {
		t.Errorf("home: %d %s %s", code, h.Get("Location"), h.Get("X-Robots-Tag"))
	}
	for _, path := range []string{"/apps/sara-shop", "/apps/sara-shop/t/products", "/apps/sara-shop/export", "/account", "/login"} {
		if code, _, _ := status(path); code != 200 && code != http.StatusSeeOther {
			t.Errorf("%s: %d", path, code)
		}
	}
	for _, path := range []string{"/signup", "/templates", "/billing", "/operator", "/connect", "/privacy", "/sitemap.xml", "/mcp",
		"/apps/other-shop", "/apps/sara-shop/rename", "/apps/sara-shop/domain", "/apps/sara-shop/delete", "/apps/sara-shop/payments/connect"} {
		if code, _, _ := status(path); code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, code)
		}
	}
	// The console's menu has the app and account settings only.
	_, page, _ := status("/apps/sara-shop")
	if !strings.Contains(page, `href="/apps/sara-shop"`) || strings.Contains(page, `href="/templates"`) || strings.Contains(page, `href="/billing"`) ||
		strings.Contains(page, `href="/support"`) || strings.Contains(page, `/rename"`) || strings.Contains(page, `/domain"`) {
		t.Error("the menu links to pages a self-hosted server does not have")
	}
	_, page, _ = status("/account")
	if strings.Contains(page, `href="/support"`) || strings.Contains(page, `href="/billing"`) {
		for _, l := range strings.Split(page, "href=")[1:] {
			t.Log(l[:min(60, len(l))])
		}
		t.Error("the account page links to pages a self-hosted server does not have")
	}
}
