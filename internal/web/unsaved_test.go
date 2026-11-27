package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Business data changed only in the preview's test data (change_preview_row)
// is reported by get_app, and publish makes no confirm link until it is
// saved in the definition or dropped on purpose. Saving the definition
// rebuilds the preview, which clears the report.
func TestUnsavedPreviewChangesBlockPublish(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "sara@salon.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	if _, errText := call(t, s, "create_app", map[string]any{"app": "sara-salon", "from_example": "booking-uae"}); errText != "" {
		t.Fatal(errText)
	}
	// A test booking is test data: nothing to report.
	if out, _ := call(t, s, "get_app_outline", map[string]any{"app": "sara-salon"}); out["unsaved_preview_changes"] != nil {
		t.Fatalf("fresh app: %v", out["unsaved_preview_changes"])
	}
	if _, errText := call(t, s, "change_preview_row", map[string]any{"app": "sara-salon", "action": "update", "table": "services", "id": 1,
		"values": map[string]any{"name": "Keratin blow-dry"}}); errText != "" {
		t.Fatal(errText)
	}
	app, _ := call(t, s, "get_app", map[string]any{"app": "sara-salon"})
	if got := fmt.Sprint(app["unsaved_preview_changes"]); !strings.Contains(got, "services") || !strings.Contains(got, "name") || app["unsaved_note"] == nil {
		t.Fatalf("get_app: %v %v", app["unsaved_preview_changes"], app["unsaved_note"])
	}
	pub, errText := call(t, s, "publish", map[string]any{"app": "sara-salon"})
	if errText != "" || pub["confirm_url"] != nil || pub["unsaved_preview_changes"] == nil || !strings.Contains(fmt.Sprint(pub["note"]), "discard_preview_changes") {
		t.Fatalf("publish with unsaved rows: %v %s", pub, errText)
	}
	// The owner's confirm page warns too.
	resp, _ := c.Get(e.base + "/apps/sara-salon/publish?version=1")
	if page := read(t, resp); !strings.Contains(page, "Not in this version") || !strings.Contains(page, "services #1") {
		t.Fatalf("publish page: %s", page)
	}
	// Saving in the definition clears it.
	version := int(app["version"].(float64))
	if out, errText := call(t, s, "edit_app", map[string]any{"app": "sara-salon", "base_version": version, "edits": []any{
		map[string]any{"target": "templates.css_extra", "old_text": "", "new_text": "<style>:root{--r:1}</style>"}}}); errText != "" || out["ok"] != true {
		t.Fatalf("edit: %v %s", out, errText)
	}
	if pub, _ := call(t, s, "publish", map[string]any{"app": "sara-salon"}); pub["confirm_url"] == nil {
		t.Fatalf("publish after saving: %v", pub)
	}
	// Dropping them on purpose works too.
	call(t, s, "change_preview_row", map[string]any{"app": "sara-salon", "action": "delete", "table": "staff", "id": 1})
	if pub, _ := call(t, s, "publish", map[string]any{"app": "sara-salon"}); pub["confirm_url"] != nil {
		t.Fatalf("publish after a deleted staff row: %v", pub)
	}
	if pub, _ := call(t, s, "publish", map[string]any{"app": "sara-salon", "discard_preview_changes": true}); pub["confirm_url"] == nil {
		t.Fatalf("publish discarding: %v", pub)
	}
}

// The results that start building repeat the photo rule, which models
// follow better there than in the server instructions.
func TestBuildResultsSayNoWebImages(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "lina@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"get_example", map[string]any{}},
		{"get_example", map[string]any{"name": "tasks"}},
		{"create_app", map[string]any{"app": "lina-shop", "from_example": "store-uae"}},
		{"create_app", map[string]any{"app": "lina-empty"}},
	} {
		out, errText := call(t, s, tc.tool, tc.args)
		if errText != "" || !strings.Contains(fmt.Sprint(out["note"]), "do not search the web for images") {
			t.Errorf("%s %v: note %v %s", tc.tool, tc.args, out["note"], errText)
		}
	}
}

// After publishing, the admin panel shows live data again: neither the
// publish page nor the app's pages keep the "Preview data" banner.
func TestPublishLeavesPreviewData(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "rana@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	call(t, s, "create_app", map[string]any{"app": "rana-shop", "from_example": "store-uae"})
	c.Get(e.base + "/apps/rana-shop/data-stage?stage=preview&back=/apps/rana-shop")
	if resp, _ := c.Get(e.base + "/apps/rana-shop"); !strings.Contains(read(t, resp), "Preview data.") {
		t.Fatal("the switch did not show preview data")
	}
	pub, _ := call(t, s, "publish", map[string]any{"app": "rana-shop"})
	if resp, _ := c.Get(fmt.Sprint(pub["confirm_url"])); strings.Contains(read(t, resp), "Preview data.") {
		t.Fatal("the publish page shows the preview data banner")
	}
	e.confirmPublish(c, pub)
	if resp, _ := c.Get(e.base + "/apps/rana-shop"); strings.Contains(read(t, resp), "Preview data.") {
		t.Fatal("after publishing, the app still shows preview data")
	}
}

// The assistant's test order stays in the preview's data and made the
// owner's own test show "Order booked for tomorrow". submit_form says
// which rows a test added, the admin panel's preview data tags them, and
// clear_test_rows removes them and the test visitor, but not the owner's
// rows or the starting data.
func TestTestRowsAreMarkedAndCleared(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "omar@dist.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	call(t, s, "create_app", map[string]any{"app": "omar-dist"})
	def := map[string]any{
		"tables":    []any{map[string]any{"name": "orders", "columns": []any{map[string]any{"name": "shop", "type": "text"}}}},
		"seed":      map[string]any{"orders": []any{map[string]any{"shop": "Starter row"}}},
		"templates": map[string]any{"home": "<p>{{len .orders}} orders</p>"},
		"scripts": map[string]any{
			"home":  `function handle(req) { return {template: "home", data: {orders: db.query("SELECT id FROM orders")}} }`,
			"order": `function handle(req) { db.exec("INSERT INTO orders (shop) VALUES (?)", req.form.shop); return {redirect: "/"} }`,
		},
		"routes": []any{map[string]any{"method": "GET", "path": "/", "script": "home"}, map[string]any{"method": "POST", "path": "/order", "script": "order"}},
	}
	if out, errText := call(t, s, "apply_change", map[string]any{"app": "omar-dist", "definition": def}); errText != "" || out["ok"] != true {
		t.Fatalf("apply: %v %s", out, errText)
	}
	v, _ := call(t, s, "create_test_visitor", map[string]any{"app": "omar-dist", "name": "Test Salesman"})
	out, errText := call(t, s, "submit_form", map[string]any{"app": "omar-dist", "path": "/order", "form": map[string]any{"shop": "Al Noor Grocery"}, "as_visitor": v["email"]})
	if errText != "" || fmt.Sprint(out["test_rows"]) != "[orders#2]" || !strings.Contains(fmt.Sprint(out["test_rows_note"]), "clear_test_rows") {
		t.Fatalf("submit_form: %v %s", out, errText)
	}
	// The owner's own order on the preview data is not a test row.
	c.Get(e.base + "/apps/omar-dist/data-stage?stage=preview&back=/apps/omar-dist/t/orders")
	if r, err := c.PostForm(e.base+"/apps/omar-dist/t/orders/new", url.Values{"csrf": {e.csrf(c, "/apps/omar-dist/t/orders/new")}, "shop": {"Owner's shop"}}); err != nil || r.StatusCode != http.StatusSeeOther {
		t.Fatalf("owner's row: %v %v", err, r)
	}
	resp, _ := c.Get(e.base + "/apps/omar-dist/t/orders")
	if page := read(t, resp); strings.Count(page, ">test</span>") != 1 {
		t.Fatalf("admin list should tag one test row: %s", page)
	}
	cleared, errText := call(t, s, "clear_test_rows", map[string]any{"app": "omar-dist"})
	if errText != "" || fmt.Sprint(cleared["removed"]) != "map[orders:1 rx_users:1]" {
		t.Fatalf("clear: %v %s", cleared, errText)
	}
	home, _ := call(t, s, "view_page", map[string]any{"app": "omar-dist", "path": "/"})
	if !strings.Contains(fmt.Sprint(home["body"]), "2 orders") {
		t.Fatalf("after clearing, the starting row and the owner's row stay: %v", home["body"])
	}
}

// view_page returns 8000 characters unless asked for more, so a long page
// does not fill a free plan chat; truncated says when it was cut.
func TestViewPageIsShortByDefault(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "hala@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	call(t, s, "create_app", map[string]any{"app": "hala-shop", "from_example": "store-uae"})
	page, _ := call(t, s, "view_page", map[string]any{"app": "hala-shop", "path": "/"})
	if body, _ := page["body"].(string); len(body) > 8000 || page["truncated"] != true {
		t.Fatalf("default: %d characters, truncated %v", len(body), page["truncated"])
	}
	more, _ := call(t, s, "view_page", map[string]any{"app": "hala-shop", "path": "/", "max_chars": 15000})
	if body, _ := more["body"].(string); len(body) <= 8000 || len(body) > 15000 {
		t.Fatalf("max_chars 15000: %d", len(body))
	}
}
