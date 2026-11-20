package platform_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

func TestMain(m *testing.M) {
	sandbox.ServeWorkerIfRequested()
	os.Exit(m.Run())
}

func newPlatform(t *testing.T) *platform.Platform {
	t.Helper()
	return newPlatformWith(t, sandbox.NewGoja())
}

func newPlatformWith(t *testing.T, sb sandbox.Sandbox) *platform.Platform {
	t.Helper()
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https"}, sb)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func catalog(t *testing.T) *appdef.App {
	t.Helper()
	data, err := os.ReadFile("../../examples/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	def, problems := appdef.Parse(data)
	if len(problems) > 0 {
		t.Fatalf("example is invalid: %v", problems)
	}
	return def
}

type response struct {
	status int
	body   string
	header http.Header
}

func do(t *testing.T, p *platform.Platform, method, host, path string, form url.Values) response {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, "http://"+host+path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	if !p.ServeApp(w, r) {
		return response{status: -1}
	}
	return response{status: w.Code, body: w.Body.String(), header: w.Header()}
}

const (
	liveHost    = "al-noor.renoxium.test"
	previewHost = "al-noor--preview.renoxium.test"
)

func TestPreviewPublishAndOrder(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	if _, err := p.CreateApp("usr_test", "al-noor"); err != nil {
		t.Fatal(err)
	}

	res, err := p.ApplyChange(ctx, "al-noor", catalog(t))
	if err != nil || !res.OK {
		t.Fatalf("apply: %v %+v", err, res)
	}
	for _, c := range res.Checks {
		if c.Status != 200 {
			t.Errorf("check %s: %d %s", c.Route, c.Status, c.Error)
		}
	}

	// Preview serves the new version; live is untouched until publish.
	if r := do(t, p, "GET", previewHost, "/", nil); r.status != 200 || !strings.Contains(r.body, "Royal Oud") {
		t.Fatalf("preview home: %d %s", r.status, r.body)
	}
	if r := do(t, p, "GET", liveHost, "/", nil); r.status != 404 {
		t.Fatalf("live before publish: %d", r.status)
	}

	pub, err := p.Publish("al-noor", 0)
	if err != nil || pub.LiveVersion != 1 || pub.LiveURL != "https://al-noor.renoxium.test" {
		t.Fatalf("publish: %v %+v", err, pub)
	}

	home := do(t, p, "GET", liveHost, "/", nil)
	if home.status != 200 || !strings.Contains(home.body, "AED 250.00") || !strings.Contains(home.body, "/_rx/log") {
		t.Fatalf("live home: %d %s", home.status, home.body)
	}
	if r := do(t, p, "GET", liveHost, "/products/1", nil); r.status != 200 || !strings.Contains(r.body, "Smoky oud") {
		t.Fatalf("product page: %d", r.status)
	}
	if r := do(t, p, "GET", liveHost, "/products/999", nil); r.status != 404 {
		t.Fatalf("missing product: %d", r.status)
	}

	// Visitor text is escaped by the template.
	order := do(t, p, "POST", liveHost, "/orders", url.Values{"product_id": {"1"}, "customer_name": {"<script>alert(1)</script>"}, "phone": {"+971500000000"}})
	if order.status != 200 || strings.Contains(order.body, "<script>alert(1)") || !strings.Contains(order.body, "&lt;script&gt;") {
		t.Fatalf("order: %d %s", order.status, order.body)
	}

	var products []map[string]any
	api := do(t, p, "GET", liveHost, "/api/products", nil)
	if err := json.Unmarshal([]byte(api.body), &products); err != nil || len(products) != 3 {
		t.Fatalf("api: %s", api.body)
	}

	// A new preview starts from a copy of live data, so the live order is visible there,
	// and orders placed on preview never reach live.
	if _, err := p.ApplyChange(ctx, "al-noor", catalog(t)); err != nil {
		t.Fatal(err)
	}
	do(t, p, "POST", previewHost, "/orders", url.Values{"product_id": {"2"}, "customer_name": {"Test"}, "phone": {"1"}})
	logs, _ := p.Logs("al-noor", applog.Filter{Stage: platform.Live})
	found := false
	for _, e := range logs {
		if strings.Contains(e.Message, "console: new order 1") {
			found = true
		}
		if strings.Contains(e.Message, "new order 2") {
			t.Fatal("preview order reached the live log")
		}
	}
	if !found {
		t.Fatal("script console output missing from live log")
	}
}

func TestScriptErrorsAndBrowserErrorsReachTheLog(t *testing.T) {
	p := newPlatform(t)
	p.CreateApp("usr_test", "broken-shop")
	def := &appdef.App{
		Templates: map[string]string{"page": "<!doctype html><html><head></head><body>hi</body></html>"},
		Scripts:   map[string]string{"bad": "function handle(req) {\n  return db.query('SELECT * FROM missing_table');\n}"},
		Routes: []appdef.Route{
			{Method: "GET", Path: "/", Template: "page"},
			{Method: "GET", Path: "/bad", Script: "bad"},
		},
	}
	res, err := p.ApplyChange(context.Background(), "broken-shop", def)
	if err != nil || !res.OK {
		t.Fatalf("%v %+v", err, res)
	}
	var badCheck platform.Check
	for _, c := range res.Checks {
		if c.Route == "GET /bad" {
			badCheck = c
		}
	}
	if badCheck.Status != 500 || !strings.Contains(badCheck.Error, "no such table: missing_table") || !strings.Contains(badCheck.Error, "script:2") {
		t.Fatalf("check did not report the script error: %+v", badCheck)
	}

	host := "broken-shop--preview.renoxium.test"
	page := do(t, p, "GET", host, "/", nil)
	rid := page.header.Get("X-Request-Id")
	beacon := httptest.NewRequest("POST", "http://"+host+"/_rx/log",
		strings.NewReader(`{"rid":"`+rid+`","level":"error","message":"TypeError: x is undefined at app.js:3","page":"/"}`))
	w := httptest.NewRecorder()
	p.ServeApp(w, beacon)
	if w.Code != 204 {
		t.Fatalf("beacon: %d", w.Code)
	}

	events, err := p.Logs("broken-shop", applog.Filter{RequestID: rid})
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{}
	for _, e := range events {
		sources[e.Source] = true
	}
	if !sources["server"] || !sources["browser"] {
		t.Fatalf("request %s should link server and browser events: %+v", rid, events)
	}
}

func TestTenantsAreIsolated(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "shop-a")
	p.CreateApp("usr_test", "shop-b")
	if res, _ := p.ApplyChange(ctx, "shop-a", catalog(t)); !res.OK {
		t.Fatal(res.Problems)
	}
	p.Publish("shop-a", 0)

	// shop-b tries every way it has to reach shop-a's data or the filesystem.
	thief := &appdef.App{
		Scripts: map[string]string{"steal": `function handle(req) {
			const out = {};
			for (const q of [
				"ATTACH DATABASE '../shop-a/live.db' AS a",
				"SELECT * FROM a.products",
				"PRAGMA database_list",
				"SELECT * FROM products",
			]) {
				try { out[q] = db.query(q); } catch (e) { out[q] = "denied"; }
			}
			out.require = typeof require;
			return {json: out};
		}`},
		Routes: []appdef.Route{{Method: "GET", Path: "/", Script: "steal"}},
	}
	if res, _ := p.ApplyChange(ctx, "shop-b", thief); !res.OK {
		t.Fatal(res.Problems)
	}
	r := do(t, p, "GET", "shop-b--preview.renoxium.test", "/", nil)
	var out map[string]any
	if err := json.Unmarshal([]byte(r.body), &out); err != nil {
		t.Fatalf("%d %s", r.status, r.body)
	}
	for q, v := range out {
		if q != "require" && v != "denied" {
			t.Errorf("%s was not denied: %v", q, v)
		}
	}
	if strings.Contains(r.body, "Royal Oud") {
		t.Fatal("shop-b read shop-a's products")
	}

	// A runaway script in one tenant does not stop another tenant.
	loop := &appdef.App{
		Scripts: map[string]string{"spin": "function handle(req) { while (true) {} }"},
		Routes:  []appdef.Route{{Method: "GET", Path: "/", Script: "spin"}},
	}
	res, _ := p.ApplyChange(ctx, "shop-b", loop)
	if !res.OK || res.Checks[0].Status != 500 || !strings.Contains(res.Checks[0].Error, "time limit") {
		t.Fatalf("runaway script check: %+v", res)
	}
	if r := do(t, p, "GET", "shop-a.renoxium.test", "/", nil); r.status != 200 {
		t.Fatalf("shop-a affected: %d", r.status)
	}
}

func TestRollback(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "rollback-shop")
	v1 := &appdef.App{Templates: map[string]string{"t": "one"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}}
	v2 := &appdef.App{Templates: map[string]string{"t": "two"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}}
	p.ApplyChange(ctx, "rollback-shop", v1)
	p.Publish("rollback-shop", 0)
	p.ApplyChange(ctx, "rollback-shop", v2)
	p.Publish("rollback-shop", 0)
	if r := do(t, p, "GET", "rollback-shop.renoxium.test", "/", nil); r.body != "two" {
		t.Fatalf("got %q", r.body)
	}
	if _, err := p.Publish("rollback-shop", 1); err != nil {
		t.Fatal(err)
	}
	if r := do(t, p, "GET", "rollback-shop.renoxium.test", "/", nil); r.body != "one" {
		t.Fatalf("after rollback got %q", r.body)
	}
}

func TestRejectedDefinitionIsExplained(t *testing.T) {
	p := newPlatform(t)
	p.CreateApp("usr_test", "typo-shop")
	def := &appdef.App{
		Scripts: map[string]string{"s": "function handle(req) { return {text: 'x' }"},
		Routes:  []appdef.Route{{Method: "GET", Path: "/", Script: "s"}},
	}
	res, err := p.ApplyChange(context.Background(), "typo-shop", def)
	if err != nil || res.OK || len(res.Problems) == 0 || !strings.Contains(res.Problems[0], `script "s"`) {
		t.Fatalf("%v %+v", err, res)
	}
	if info, _ := p.Info("typo-shop"); info.PreviewVersion != 0 {
		t.Fatal("a rejected definition created a version")
	}
}

func TestUnknownHostIsNotAnApp(t *testing.T) {
	p := newPlatform(t)
	if r := do(t, p, "GET", "nobody.renoxium.test", "/", nil); r.status != -1 {
		t.Fatalf("unknown host served: %d", r.status)
	}
}

// With worker processes, a tenant script that eats memory is stopped and
// other tenants keep working.
func TestMemoryHungryTenantIsContained(t *testing.T) {
	pool, err := sandbox.NewPool(sandbox.PoolConfig{Workers: 2, Timeout: 3 * time.Second, HeapLimit: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	p := newPlatformWith(t, pool)
	ctx := context.Background()
	p.CreateApp("usr_test", "good-shop")
	p.CreateApp("usr_test", "greedy-shop")
	if res, err := p.ApplyChange(ctx, "good-shop", catalog(t)); err != nil || !res.OK {
		t.Fatalf("%v %+v", err, res)
	}
	p.Publish("good-shop", 0)

	greedy := &appdef.App{
		Scripts: map[string]string{"eat": "function handle(req) { const a = []; for (let i = 0; ; i++) a.push({i: i, s: 'abcdefghij' + i}); }"},
		Routes:  []appdef.Route{{Method: "GET", Path: "/", Script: "eat"}},
	}
	res, err := p.ApplyChange(ctx, "greedy-shop", greedy)
	if err != nil || !res.OK || res.Checks[0].Status != 500 || !strings.Contains(res.Checks[0].Error, "memory limit exceeded") {
		t.Fatalf("%v %+v", err, res)
	}
	// The good shop, including script-rendered pages and orders, still works.
	if r := do(t, p, "GET", "good-shop.renoxium.test", "/", nil); r.status != 200 || !strings.Contains(r.body, "Royal Oud") {
		t.Fatalf("good shop home: %d", r.status)
	}
	order := do(t, p, "POST", "good-shop.renoxium.test", "/orders", url.Values{"product_id": {"1"}, "customer_name": {"Mariam"}, "phone": {"0501112222"}})
	if order.status != 200 || !strings.Contains(order.body, "order #1") {
		t.Fatalf("good shop order: %d %s", order.status, order.body)
	}
}

func pngFile(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 3, 3))
	img.Pix[0] = shade
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// Files are stored once on disk but each app serves only its own.
func TestFilesAreSharedOnDiskAndServedPerApp(t *testing.T) {
	p := newPlatform(t)
	p.CreateApp("usr_test", "shop-a")
	p.CreateApp("usr_test", "shop-b")
	logo := pngFile(t, 10)

	a, err := p.AddFile("shop-a", bytes.NewReader(logo))
	if err != nil || a.Reused || !strings.HasPrefix(a.URL, "/_rx/files/") || !strings.HasSuffix(a.URL, ".png") {
		t.Fatalf("add: %+v %v", a, err)
	}
	again, _ := p.AddFile("shop-a", bytes.NewReader(logo))
	if again.URL != a.URL || !again.Reused {
		t.Fatalf("same file again: %+v", again)
	}

	// Served on the app's live and preview hosts, before any version exists.
	for _, host := range []string{"shop-a.renoxium.test", "shop-a--preview.renoxium.test"} {
		r := do(t, p, "GET", host, a.URL, nil)
		if r.status != 200 || r.body != string(logo) || r.header.Get("Content-Type") != "image/png" ||
			!strings.Contains(r.header.Get("Cache-Control"), "immutable") || r.header.Get("Content-Security-Policy") == "" {
			t.Fatalf("%s: %d %v", host, r.status, r.header)
		}
	}
	// Not served by another app, even though the bytes are on disk.
	if r := do(t, p, "GET", "shop-b.renoxium.test", a.URL, nil); r.status != 404 {
		t.Fatalf("shop-b served shop-a's file: %d", r.status)
	}
	// Once shop-b uploads the same content it reuses the stored copy.
	b, _ := p.AddFile("shop-b", bytes.NewReader(logo))
	if b.URL != a.URL || !b.Reused {
		t.Fatalf("shop-b upload: %+v", b)
	}
	if r := do(t, p, "GET", "shop-b.renoxium.test", a.URL, nil); r.status != 200 {
		t.Fatalf("shop-b own file: %d", r.status)
	}
	// Wrong extension, unknown hash, and conditional requests.
	if r := do(t, p, "GET", "shop-a.renoxium.test", strings.TrimSuffix(a.URL, ".png")+".jpg", nil); r.status != 404 {
		t.Fatalf("wrong extension: %d", r.status)
	}
	if r := do(t, p, "GET", "shop-a.renoxium.test", "/_rx/files/"+strings.Repeat("0", 64)+".png", nil); r.status != 404 {
		t.Fatalf("unknown hash: %d", r.status)
	}
	req := httptest.NewRequest("GET", "http://shop-a.renoxium.test"+a.URL, nil)
	req.Header.Set("If-None-Match", `"`+strings.TrimSuffix(strings.TrimPrefix(a.URL, "/_rx/files/"), ".png")+`"`)
	w := httptest.NewRecorder()
	p.ServeApp(w, req)
	if w.Code != 304 {
		t.Fatalf("conditional request: %d", w.Code)
	}
	list, _ := p.Files("shop-a")
	if len(list) != 1 || list[0].URL != a.URL {
		t.Fatalf("files = %+v", list)
	}
	if _, err := p.AddFile("shop-a", strings.NewReader("<svg onload=alert(1)>")); err == nil {
		t.Fatal("non-image accepted")
	}
}

// Every bundled example applies cleanly and every checked route renders.
func TestExamplesApply(t *testing.T) {
	p := newPlatform(t)
	for _, ex := range examples.All() {
		def, problems := appdef.Parse(ex.JSON)
		if len(problems) > 0 {
			t.Fatalf("%s: %v", ex.Name, problems)
		}
		app := "example-" + ex.Name
		p.CreateApp("usr_test", app)
		res, err := p.ApplyChange(context.Background(), app, def)
		if err != nil || !res.OK || len(res.Checks) == 0 {
			t.Fatalf("%s: %v %+v", ex.Name, err, res)
		}
		for _, c := range res.Checks {
			// 303 is a route that asks visitors to sign in; a parameter that is
			// not a row id (a crew's secret link) may answer 404, with a note.
			if c.Status >= 400 && !(c.Status == 404 && c.Note != "") {
				t.Errorf("%s %s: %d %s", ex.Name, c.Route, c.Status, c.Error)
			}
		}
	}
}

func TestRepeatedFormFields(t *testing.T) {
	p := newPlatform(t)
	p.CreateApp("usr_test", "form-shop")
	def := &appdef.App{
		Scripts: map[string]string{"s": "function handle(req) { return {json: {first: req.form.size, all: req.formAll.size}} }"},
		Routes:  []appdef.Route{{Method: "POST", Path: "/", Script: "s"}},
	}
	p.ApplyChange(context.Background(), "form-shop", def)
	r := do(t, p, "POST", "form-shop--preview.renoxium.test", "/", url.Values{"size": {"S", "M", "L"}})
	if r.body != `{"all":["S","M","L"],"first":"S"}` {
		t.Fatalf("got %s", r.body)
	}
}

func TestDefaultFavicon(t *testing.T) {
	p := newPlatform(t)
	if _, err := p.CreateApp("usr_test", "al-noor"); err != nil {
		t.Fatal(err)
	}
	if res, err := p.ApplyChange(context.Background(), "al-noor", catalog(t)); err != nil || !res.OK {
		t.Fatalf("apply: %v %+v", err, res)
	}
	r := do(t, p, "GET", previewHost, "/favicon.ico", nil)
	if r.status != 200 || r.header.Get("Content-Type") != "image/png" || !strings.HasPrefix(r.body, "\x89PNG") {
		t.Fatalf("favicon: %d %s", r.status, r.header.Get("Content-Type"))
	}
	logs, _ := p.Logs("al-noor", applog.Filter{Level: "warn"})
	for _, l := range logs {
		if strings.Contains(l.Message, "favicon") {
			t.Fatalf("favicon still logged: %s", l.Message)
		}
	}
}
