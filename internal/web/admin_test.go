package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// publishShop builds a published shop owned by the browser's user.
func (e *env) publishShop(c *http.Client, app string) {
	e.t.Helper()
	tables := []appdef.Table{
		{Name: "products", Columns: []appdef.Column{
			{Name: "name", Type: "text", Required: true, Unique: true},
			{Name: "price_aed", Type: "real", Required: true},
			{Name: "stock", Type: "integer"},
			{Name: "active", Type: "boolean"},
			{Name: "available_from", Type: "datetime"},
			{Name: "attributes", Type: "json"},
			{Name: "description", Type: "text"},
		}},
		{Name: "orders", Columns: []appdef.Column{{Name: "customer_name", Type: "text"}}},
	}
	def := &appdef.App{
		Tables:    tables,
		Seed:      map[string][]map[string]any{"products": {{"name": "Royal Oud", "price_aed": 250, "stock": 4, "active": 1}}, "orders": {{"customer_name": "Mariam"}}},
		Templates: map[string]string{"t": "shop"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
	}
	e.publishDef(c, app, def)
}

func (e *env) publishDef(c *http.Client, app string, def *appdef.App) {
	e.t.Helper()
	raw, _ := json.Marshal(def)
	var defMap map[string]any
	json.Unmarshal(raw, &defMap)
	sess := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	for _, step := range []struct {
		tool string
		args map[string]any
	}{
		{"create_app", map[string]any{"app": app}},
		{"apply_change", map[string]any{"app": app, "definition": defMap}},
		{"publish", map[string]any{"app": app}},
	} {
		out, errText := call(e.t, sess, step.tool, step.args)
		if errText != "" {
			e.t.Fatalf("%s: %s", step.tool, errText)
		}
		if step.tool == "publish" {
			e.confirmPublish(c, out)
		}
	}
}

func logFilter() applog.Filter { return applog.Filter{Stage: platform.Live, Limit: 200} }

func anyMessage(events []applog.Event, text string) bool {
	for _, ev := range events {
		if strings.Contains(ev.Message, text) {
			return true
		}
	}
	return false
}

func TestAdminPanel(t *testing.T) {
	e := newEnv(t)
	alice := e.browser()
	e.signup(alice, "alice@shop.ae")
	e.publishShop(alice, "al-noor")

	get := func(c *http.Client, path string) (int, string) {
		t.Helper()
		r, err := c.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode, read(t, r)
	}
	post := func(c *http.Client, path string, form url.Values) (*http.Response, string) {
		t.Helper()
		form.Set("csrf", e.csrf(c, "/"))
		r, err := c.PostForm(e.base+path, form)
		if err != nil {
			t.Fatal(err)
		}
		return r, read(t, r)
	}

	// Overview and lists.
	if code, page := get(alice, "/apps/al-noor"); code != 200 || !strings.Contains(page, "Products") || !strings.Contains(page, "1 row<") {
		t.Fatalf("overview: %d %s", code, page)
	}
	if code, page := get(alice, "/apps/al-noor/t/orders"); code != 200 || !strings.Contains(page, "Mariam") {
		t.Fatalf("orders: %d", code)
	}

	// Add a product with every column type.
	r, _ := post(alice, "/apps/al-noor/t/products/new", url.Values{
		"name": {`<script>alert(1)</script> Musk`}, "price_aed": {"95.5"}, "stock": {"12"}, "active": {"1"},
		"available_from": {"2026-12-01T09:30"}, "attributes": {`{"size":"30ml"}`}, "description": {"Soft\nmusk"},
	})
	loc := r.Header.Get("Location")
	if r.StatusCode != http.StatusSeeOther || loc != "/apps/al-noor/t/products/2?saved=1" {
		t.Fatalf("add: %d %s", r.StatusCode, loc)
	}
	code, page := get(alice, loc)
	if code != 200 || !strings.Contains(page, "Saved.") || !strings.Contains(page, `value="2026-12-01T09:30"`) ||
		!strings.Contains(page, `checked`) || !strings.Contains(page, "&lt;script&gt;") || strings.Contains(page, "<script>alert") {
		t.Fatalf("edit page: %d %s", code, page)
	}
	row, err := e.p.AdminGet(context.Background(), "al-noor", "products", 2)
	if err != nil || row["price_aed"] != 95.5 || row["stock"] != int64(12) || row["active"] != int64(1) || row["available_from"] != "2026-12-01 09:30:00" {
		t.Fatalf("stored row: %v %v", row, err)
	}

	// Validation errors keep the typed values and explain the problem.
	r, page = post(alice, "/apps/al-noor/t/products/2", url.Values{"name": {"Royal Oud"}, "price_aed": {"cheap"}, "attributes": {"{"}})
	if r.StatusCode != 422 || !strings.Contains(page, "enter a number") || !strings.Contains(page, "enter valid JSON") {
		t.Fatalf("invalid edit: %d %s", r.StatusCode, page)
	}
	r, page = post(alice, "/apps/al-noor/t/products/2", url.Values{"name": {"Royal Oud"}, "price_aed": {"1"}})
	if r.StatusCode != 422 || !strings.Contains(page, "another row already has this value") {
		t.Fatalf("duplicate name: %d %s", r.StatusCode, page)
	}

	// Unchecking a boolean stores 0.
	r, _ = post(alice, "/apps/al-noor/t/products/2", url.Values{"name": {"White Musk"}, "price_aed": {"90"}})
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit: %d", r.StatusCode)
	}
	if row, _ := e.p.AdminGet(context.Background(), "al-noor", "products", 2); row["active"] != int64(0) || row["name"] != "White Musk" {
		t.Fatalf("after edit: %v", row)
	}

	// Search.
	if _, page := get(alice, "/apps/al-noor/t/products?q=white"); !strings.Contains(page, "White Musk") || strings.Contains(page, "Royal Oud") {
		t.Fatalf("search: %s", page)
	}

	// The live shop sees the change, and the edit is in the app log.
	logs, _ := e.p.Logs("al-noor", logFilter())
	if !anyMessage(logs, "alice@shop.ae changed products #2") {
		t.Fatalf("admin edit not logged: %v", logs)
	}

	// Delete needs the CSRF token.
	r2, _ := alice.PostForm(e.base+"/apps/al-noor/t/products/2/delete", url.Values{})
	if r2.StatusCode != 403 {
		t.Fatalf("delete without csrf: %d", r2.StatusCode)
	}
	r, _ = post(alice, "/apps/al-noor/t/products/2/delete", url.Values{})
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if code, _ := get(alice, "/apps/al-noor/t/products/2"); code != 404 {
		t.Fatalf("deleted row: %d", code)
	}

	// Another user cannot see or change the app.
	bob := e.browser()
	e.signup(bob, "bob@shop.ae")
	for _, path := range []string{"/apps/al-noor", "/apps/al-noor/t/orders", "/apps/al-noor/t/products/1"} {
		if code, page := get(bob, path); code != 404 || strings.Contains(page, "Mariam") || strings.Contains(page, "Royal Oud") {
			t.Errorf("bob GET %s: %d", path, code)
		}
	}
	r, _ = post(bob, "/apps/al-noor/t/products/1", url.Values{"name": {"hacked"}, "price_aed": {"1"}})
	if r.StatusCode != 404 {
		t.Fatalf("bob edit: %d", r.StatusCode)
	}
	if row, _ := e.p.AdminGet(context.Background(), "al-noor", "products", 1); row["name"] != "Royal Oud" {
		t.Fatal("bob changed alice's product")
	}

	// Signed out: sign in, then land back on the page.
	carol := e.browser()
	r, _ = carol.Get(e.base + "/apps/al-noor/t/orders")
	if loc := r.Header.Get("Location"); r.StatusCode != 303 || loc != "/login?next=%2Fapps%2Fal-noor%2Ft%2Forders" {
		t.Fatalf("signed out: %d %s", r.StatusCode, loc)
	}
	token := e.csrf(carol, "/login")
	r, _ = carol.PostForm(e.base+"/login", url.Values{"csrf": {token}, "email": {"alice@shop.ae"}, "password": {"correct horse battery"}, "next": {"/apps/al-noor/t/orders"}})
	if loc := r.Header.Get("Location"); loc != "/apps/al-noor/t/orders" {
		t.Fatalf("login next: %s", loc)
	}
	r, _ = carol.PostForm(e.base+"/login", url.Values{"csrf": {token}, "email": {"alice@shop.ae"}, "password": {"correct horse battery"}, "next": {"//evil.example/x"}})
	if loc := r.Header.Get("Location"); loc != "/" {
		t.Fatalf("open redirect: %s", loc)
	}
}

// The overview offers to publish a preview that is newer than live.
func TestOverviewOffersPublish(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "uma@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	call(t, s, "create_app", map[string]any{"app": "uma-shop"})
	call(t, s, "apply_change", map[string]any{"app": "uma-shop", "definition": map[string]any{
		"templates": map[string]any{"t": "x"}, "routes": []any{map[string]any{"method": "GET", "path": "/", "template": "t"}}}})
	r, _ := c.Get(e.base + "/apps/uma-shop")
	page := read(t, r)
	if !strings.Contains(page, `href="/apps/uma-shop/publish?version=1"`) || !strings.Contains(page, "Publish version 1") {
		t.Fatalf("no publish button: %s", page)
	}
	if !strings.Contains(page, `href="/apps/uma-shop/payments"`) {
		t.Fatal("payments must be reachable before the first publish")
	}
	if r, _ := c.Get(e.base + "/apps/uma-shop/payments"); r.StatusCode != 200 {
		t.Fatalf("payments page before publish: %d", r.StatusCode)
	}
	r, _ = c.Get(e.base + "/apps/uma-shop/publish?version=1")
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "Publish version 1?") {
		t.Fatalf("confirm page: %d", r.StatusCode)
	}
}
