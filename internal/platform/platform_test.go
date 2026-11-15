package platform_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/mcpserver"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

func newPlatform(t *testing.T) *platform.Platform {
	t.Helper()
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
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
	if _, err := p.CreateApp("al-noor"); err != nil {
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
	p.CreateApp("broken-shop")
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
	p.CreateApp("shop-a")
	p.CreateApp("shop-b")
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
	p.CreateApp("rollback-shop")
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
	p.CreateApp("typo-shop")
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

// An agent builds and publishes the catalog through the builder MCP tools.
func TestBuildThroughMCP(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	server := mcpserver.New(p)
	ct, st := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if got := strings.Join(names, ","); got != "apply_change,create_app,get_app,get_logs,publish" {
		t.Fatalf("tools = %s", got)
	}

	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("%s failed: %v", name, res.Content[0].(*mcp.TextContent).Text)
		}
		out, _ := res.StructuredContent.(map[string]any)
		return out
	}

	call("create_app", map[string]any{"app": "al-noor"})
	raw, _ := os.ReadFile("../../examples/catalog.json")
	var def map[string]any
	json.Unmarshal(raw, &def)
	applied := call("apply_change", map[string]any{"app": "al-noor", "definition": def})
	if applied["ok"] != true || applied["preview_url"] != "https://al-noor--preview.renoxium.test" {
		t.Fatalf("apply_change = %v", applied)
	}
	call("publish", map[string]any{"app": "al-noor"})
	if r := do(t, p, "GET", liveHost, "/", nil); r.status != 200 || !strings.Contains(r.body, "White Musk") {
		t.Fatalf("live after MCP publish: %d", r.status)
	}
	got := call("get_app", map[string]any{"app": "al-noor"})
	if got["live_version"] != float64(1) || got["definition"] == nil {
		t.Fatalf("get_app = %v", got)
	}
	logs := call("get_logs", map[string]any{"app": "al-noor", "stage": "live"})
	if events, _ := logs["events"].([]any); len(events) == 0 {
		t.Fatalf("get_logs returned no events: %v", logs)
	}
}
