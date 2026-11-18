package web_test

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/stripe"
	"github.com/arif-rachim/renoxium/internal/web"
)

func TestAccountPage(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "xena@shop.ae")
	other := e.browser()
	token := e.csrf(other, "/login")
	other.PostForm(e.base+"/login", url.Values{"csrf": {token}, "email": {"xena@shop.ae"}, "password": {"correct horse battery"}})
	agent := e.connect(c, e.registerClient()).AccessToken
	e.publishDef(c, "xena-shop", &appdef.App{
		Tables:    []appdef.Table{{Name: "products", Columns: []appdef.Column{{Name: "name", Type: "text"}}}},
		Seed:      map[string][]map[string]any{"products": {{"name": "Oud"}}},
		Templates: map[string]string{"t": "shop"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}})
	agent = e.connect(c, e.registerClient()).AccessToken

	post := func(path string, form url.Values) (*http.Response, string) {
		form.Set("csrf", e.csrf(c, "/account"))
		r, _ := c.PostForm(e.base+path, form)
		return r, read(t, r)
	}

	// Change password: the current one is checked; other sessions and agents are signed out.
	if r, page := post("/account/password", url.Values{"current": {"wrong one!!"}, "password": {"a brand new password"}}); r.StatusCode != 401 || !strings.Contains(page, "not right") {
		t.Fatalf("wrong current: %d", r.StatusCode)
	}
	r, _ := post("/account/password", url.Values{"current": {"correct horse battery"}, "password": {"a brand new password"}})
	if r.StatusCode != 303 || r.Header.Get("Location") != "/account?changed=1" {
		t.Fatalf("change: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	if r, _ := c.Get(e.base + "/account?changed=1"); !strings.Contains(read(t, r), "Password changed") {
		t.Fatal("this browser was signed out")
	}
	if r, _ := other.Get(e.base + "/"); strings.Contains(read(t, r), "xena@shop.ae") {
		t.Fatal("other session survived")
	}
	req, _ := http.NewRequest("POST", e.base+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+agent)
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 401 {
		t.Fatalf("agent token survived: %d", r.StatusCode)
	}

	// Export: a zip with the account, the app definition and its live database.
	r, _ = c.Get(e.base + "/account/export")
	raw, _ := io.ReadAll(r.Body)
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || r.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("export: %v %s", err, r.Header.Get("Content-Type"))
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"account.json", "xena-shop/app.json", "xena-shop/definition-v1.json", "xena-shop/live.db", "xena-shop/files.json"} {
		if !names[want] {
			t.Errorf("export lacks %s (has %v)", want, names)
		}
	}
	for _, f := range zr.File {
		if f.Name == "xena-shop/live.db" {
			rc, _ := f.Open()
			head := make([]byte, 16)
			io.ReadFull(rc, head)
			if string(head) != "SQLite format 3\x00" {
				t.Fatal("live.db is not a SQLite database")
			}
		}
	}

	// Delete: a running paid plan is cancelled at Stripe first; nothing is
	// deleted when that fails or the confirmation is wrong.
	fb := &fakeBilling{status: "active", price: "price_pro"}
	e.site.Billing = web.Billing{Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: fb.server(t).URL},
		PricePro: "price_pro", PriceBusiness: "price_business", WebhookSecret: "whsec_billing"}
	u, _ := e.acc.UserByEmail("xena@shop.ae")
	e.acc.SetCustomer(u.ID, "cus_1")
	e.acc.SetSubscription(u.ID, accounts.Billing{Plan: "pro", Status: "active", Subscription: "sub_1"})
	if r, _ := c.Get(e.base + "/account"); !strings.Contains(read(t, r), "Your paid plan is cancelled at once") {
		t.Fatal("the delete form does not warn about the paid plan")
	}
	fb.failCancel = true
	if r, page := post("/account/delete", url.Values{"confirm": {"xena@shop.ae"}, "password": {"a brand new password"}}); r.StatusCode != 502 || !strings.Contains(page, "nothing was deleted") {
		t.Fatalf("delete when Stripe fails: %d", r.StatusCode)
	}
	if _, err := e.acc.UserByEmail("xena@shop.ae"); err != nil {
		t.Fatal("the account was deleted although the plan was not cancelled")
	}
	fb.failCancel = false
	if r, _ := post("/account/delete", url.Values{"confirm": {"someone@else.ae"}, "password": {"a brand new password"}}); r.StatusCode != 400 {
		t.Fatalf("wrong confirmation: %d", r.StatusCode)
	}
	if r, _ := post("/account/delete", url.Values{"confirm": {"xena@shop.ae"}, "password": {"wrong password!"}}); r.StatusCode != 401 {
		t.Fatalf("wrong password: %d", r.StatusCode)
	}
	if len(fb.cancels) != 0 {
		t.Fatalf("cancelled before the confirmation was right: %v", fb.cancels)
	}
	if r, page := post("/account/delete", url.Values{"confirm": {"XENA@shop.ae"}, "password": {"a brand new password"}}); r.StatusCode != 200 || !strings.Contains(page, "1 app(s) were deleted") {
		t.Fatalf("delete: %d %s", r.StatusCode, page)
	}
	if len(fb.cancels) != 1 || fb.cancels[0] != "sub_1" {
		t.Fatalf("subscription not cancelled: %v", fb.cancels)
	}
	if e.p.ServeApp(httptest.NewRecorder(), httptest.NewRequest("GET", "http://xena-shop.renoxium.test/", nil)) {
		t.Fatal("the app still serves")
	}
	if _, err := e.acc.UserByEmail("xena@shop.ae"); err == nil {
		t.Fatal("the account still exists")
	}
	// The name is held: its address is in customers' bookmarks and old
	// emails, so another account cannot take it over right away.
	n := e.browser()
	e.signup(n, "yuri@shop.ae")
	if s := e.mcpSession(e.connect(n, e.registerClient()).AccessToken); true {
		if _, errText := call(t, s, "create_app", map[string]any{"app": "xena-shop"}); !strings.Contains(errText, "already taken") {
			t.Fatalf("a deleted app's name went to another account: %q", errText)
		}
	}
}
