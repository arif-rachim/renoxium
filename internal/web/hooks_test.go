package web_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// A change in the admin panel runs on_admin_change; when that script fails
// the change stays and the next admin page says so once.
func TestAdminPanelRunsOnAdminChange(t *testing.T) {
	e := newEnv(t)
	alice := e.browser()
	e.signup(alice, "alice@shop.ae")
	e.publishDef(alice, "hooked", &appdef.App{
		Tables: []appdef.Table{
			{Name: "orders", Columns: []appdef.Column{{Name: "status", Type: "text"}}},
			{Name: "seen", Columns: []appdef.Column{{Name: "line", Type: "text"}}},
		},
		Lib: map[string]string{"util": "function line(b) { return b.action + ' ' + b.table + ' ' + b.id + ' by ' + b.by; }"},
		Scripts: map[string]string{
			"home": `function handle(req) { return {text: 'shop'} }`,
			"hook": `function handle(req) {
				if (req.body.table !== 'orders') return {text: 'skip'};
				db.exec('INSERT INTO seen (line) VALUES (?)', line(req.body));
				if (req.body.row && req.body.row.status === 'boom') throw new Error('hook broke');
				return {text: 'ok'};
			}`,
		},
		Routes:        []appdef.Route{{Method: "GET", Path: "/", Script: "home"}},
		OnAdminChange: "hook",
	})
	post := func(path string, form url.Values) *http.Response {
		t.Helper()
		form.Set("csrf", e.csrf(alice, "/"))
		r, err := alice.PostForm(e.base+path, form)
		if err != nil {
			t.Fatal(err)
		}
		read(t, r)
		return r
	}
	get := func(path string) string {
		t.Helper()
		r, err := alice.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		return read(t, r)
	}
	if r := post("/apps/hooked/t/orders/new", url.Values{"status": {"new"}}); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("add: %d", r.StatusCode)
	}
	rows, _, _ := e.p.AdminList(context.Background(), "hooked", "seen", "", 1)
	if len(rows) != 1 || rows[0]["line"] != "insert orders 1 by alice@shop.ae" {
		t.Fatalf("hook did not run: %v", rows)
	}
	if page := get("/apps/hooked/t/orders/1?saved=1"); !strings.Contains(page, "Saved.") || strings.Contains(page, "follow-up script") {
		t.Fatalf("page after a good change: %s", page)
	}

	r := post("/apps/hooked/t/orders/1", url.Values{"status": {"boom"}})
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("a failing hook failed the save: %d", r.StatusCode)
	}
	if row, _ := e.p.AdminGet(context.Background(), "hooked", "orders", 1); row["status"] != "boom" {
		t.Fatalf("change undone: %v", row)
	}
	if page := get(r.Header.Get("Location")); !strings.Contains(page, "Saved.") || !strings.Contains(page, "follow-up script (hook) failed") {
		t.Fatalf("no notice after a failing hook: %s", page)
	}
	if page := get("/apps/hooked/t/orders"); strings.Contains(page, "follow-up script") {
		t.Fatal("the notice is shown more than once")
	}
	if r := post("/apps/hooked/t/orders/1/delete", url.Values{}); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if rows, _, _ := e.p.AdminList(context.Background(), "hooked", "seen", "", 1); len(rows) != 3 || rows[0]["line"] != "delete orders 1 by alice@shop.ae" {
		t.Fatalf("hook after delete: %v", rows)
	}
}
