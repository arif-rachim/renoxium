package web_test

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

func TestDeleteApp(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "dina@shop.ae")
	def := &appdef.App{
		Tables:    []appdef.Table{{Name: "products", Columns: []appdef.Column{{Name: "name", Type: "text"}}}},
		Seed:      map[string][]map[string]any{"products": {{"name": "Oud"}}},
		Templates: map[string]string{"t": "dina's shop"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}}
	e.publishDef(c, "dina-shop", def)
	e.publishDef(c, "dina-cafe", def)
	live := func(app string) int {
		req, _ := http.NewRequest("GET", e.base+"/", nil)
		req.Host = app + ".renoxium.test"
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		read(t, r)
		return r.StatusCode
	}
	if live("dina-shop") != 200 {
		t.Fatal("the app is not live")
	}

	// The page is in the app's settings, for the owner only.
	r, _ := c.Get(e.base + "/apps/dina-shop/delete")
	page := read(t, r)
	if r.StatusCode != 200 || !strings.Contains(page, "cannot be undone") || !strings.Contains(page, "<strong>1</strong> Products") ||
		!strings.Contains(page, `data-name="dina-shop"`) || !strings.Contains(page, `href="/apps/dina-shop/export"`) {
		t.Fatalf("delete page: %d %s", r.StatusCode, page)
	}
	if r, _ := c.Get(e.base + "/apps/dina-shop/staff"); !strings.Contains(read(t, r), `href="/apps/dina-shop/delete"`) {
		t.Fatal("no Delete app in the settings navigation")
	}
	stranger := e.browser()
	e.signup(stranger, "sam@other.ae")
	if r, _ := stranger.Get(e.base + "/apps/dina-shop/delete"); r.StatusCode != 404 {
		t.Fatalf("someone else's app: %d", r.StatusCode)
	}

	// Export of one app.
	r, _ = c.Get(e.base + "/apps/dina-shop/export")
	raw, _ := io.ReadAll(r.Body)
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["dina-shop/live.db"] || names["dina-cafe/live.db"] {
		t.Fatalf("export has %v", names)
	}

	post := func(app string, form url.Values) (*http.Response, string) {
		form.Set("csrf", e.csrf(c, "/account"))
		r, _ := c.PostForm(e.base+"/apps/"+app+"/delete", form)
		return r, read(t, r)
	}
	if r, _ := post("dina-shop", url.Values{"confirm": {"dina-cafe"}, "password": {"correct horse battery"}}); r.StatusCode != 400 {
		t.Fatalf("wrong name: %d", r.StatusCode)
	}
	if r, _ := post("dina-shop", url.Values{"confirm": {"dina-shop"}, "password": {"wrong password!"}}); r.StatusCode != 401 {
		t.Fatalf("wrong password: %d", r.StatusCode)
	}

	// A customer's running subscription blocks the delete until it is cancelled.
	db, err := sql.Open("sqlite", "file:"+filepath.Join(e.dir, "apps", "dina-shop", "live.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO rx_subscriptions (id, reference, status, amount, currency, items, interval, success_path, cancel_path)
		VALUES ('rxs_1', 'member-1', 'active', 9900, 'aed', '[]', 'month', '/', '/')`); err != nil {
		t.Fatal(err)
	}
	if r, _ := c.Get(e.base + "/apps/dina-shop/delete"); !strings.Contains(read(t, r), "1 customer subscription(s) are still running") {
		t.Fatal("running subscription not shown")
	}
	if r, _ := post("dina-shop", url.Values{"confirm": {"dina-shop"}, "password": {"correct horse battery"}}); r.StatusCode != 409 {
		t.Fatalf("delete with a running subscription: %d", r.StatusCode)
	}
	db.Exec(`UPDATE rx_subscriptions SET cancel_at_period_end = 1`)

	r, page = post("dina-shop", url.Values{"confirm": {"dina-shop"}, "password": {"correct horse battery"}})
	if r.StatusCode != 200 || !strings.Contains(page, "App deleted") {
		t.Fatalf("delete: %d %s", r.StatusCode, page)
	}
	if live("dina-shop") == 200 {
		t.Fatal("the deleted app still answers")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "apps", "dina-shop")); !os.IsNotExist(err) {
		t.Fatalf("data left on disk: %v", err)
	}
	r, _ = c.Get(e.base + "/")
	if home := read(t, r); strings.Contains(home, "dina-shop") || !strings.Contains(home, "dina-cafe") {
		t.Fatal("dashboard does not show the right apps")
	}
	if live("dina-cafe") != 200 {
		t.Fatal("the other app was touched")
	}
	if r, _ := c.Get(e.base + "/apps/dina-shop/delete"); r.StatusCode != 404 {
		t.Fatalf("deleted app's page: %d", r.StatusCode)
	}
}
