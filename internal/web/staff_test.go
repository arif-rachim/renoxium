package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

func TestStaffAccess(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "omar@shop.ae")
	e.publishDef(owner, "omar-shop", &appdef.App{
		Tables:    []appdef.Table{{Name: "products", Columns: []appdef.Column{{Name: "name", Type: "text"}}}},
		Seed:      map[string][]map[string]any{"products": {{"name": "Oud"}}},
		Templates: map[string]string{"t": "shop"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}})
	add := func(addr string) (int, string) {
		r, _ := owner.PostForm(e.base+"/apps/omar-shop/staff", url.Values{"csrf": {e.csrf(owner, "/apps/omar-shop/staff")}, "email": {addr}})
		return r.StatusCode, read(t, r)
	}

	if code, _ := add("sara@shop.ae"); code != 303 {
		t.Fatalf("add staff: %d", code)
	}
	if msg := e.mail.last(t, "sara@shop.ae", "staff_invite"); !strings.Contains(msg.Text, "/apps/omar-shop") || msg.ReplyTo != "omar@shop.ae" {
		t.Fatalf("invite: %+v", msg)
	}

	// The invited person signs up; until the email is confirmed there is no access.
	staff := e.browser()
	e.signup(staff, "sara@shop.ae")
	if r, _ := staff.Get(e.base + "/apps/omar-shop"); r.StatusCode != 404 {
		t.Fatalf("unconfirmed staff: %d", r.StatusCode)
	}
	http.Get(linkRE.FindString(e.mail.last(t, "sara@shop.ae", "verify_email").Text))

	get := func(path string) (int, string) {
		r, _ := staff.Get(e.base + path)
		return r.StatusCode, read(t, r)
	}
	if code, page := get("/"); code != 200 || !strings.Contains(page, "Shared with you") || !strings.Contains(page, "omar-shop") {
		t.Fatal("dashboard does not show the shared app")
	}
	code, page := get("/apps/omar-shop")
	if code != 200 || !strings.Contains(page, "staff access") || strings.Contains(page, "/apps/omar-shop/payments") || strings.Contains(page, "Publish version") {
		t.Fatalf("staff overview: %d", code)
	}
	if code, _ := get("/apps/omar-shop/t/products"); code != 200 {
		t.Fatalf("staff data: %d", code)
	}
	r, _ := staff.PostForm(e.base+"/apps/omar-shop/t/products/1", url.Values{"csrf": {e.csrf(staff, "/")}, "name": {"Royal Oud"}})
	if r.StatusCode != 303 {
		t.Fatalf("staff edit: %d", r.StatusCode)
	}
	logs, _ := e.p.Logs("omar-shop", logFilter())
	if !anyMessage(logs, "sara@shop.ae changed products #1") {
		t.Fatal("staff change not logged with their email")
	}
	// Owner-only pages stay closed.
	for _, path := range []string{"/apps/omar-shop/payments", "/apps/omar-shop/staff", "/apps/omar-shop/publish?version=1"} {
		if code, _ := get(path); code != 404 {
			t.Errorf("staff reached %s: %d", path, code)
		}
	}
	// Removing the staff member ends access.
	owner.PostForm(e.base+"/apps/omar-shop/staff/remove", url.Values{"csrf": {e.csrf(owner, "/apps/omar-shop/staff")}, "email": {"sara@shop.ae"}})
	if code, _ := get("/apps/omar-shop/t/products"); code != 404 {
		t.Fatalf("removed staff: %d", code)
	}
}
