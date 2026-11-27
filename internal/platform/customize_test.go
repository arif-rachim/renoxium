package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/examples"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// One call makes a store with the business's own settings and products.
// Mistakes in the data are reported before an app is made.
func TestCreateCustomized(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	if _, _, err := p.CreateCustomized(ctx, "usr_test", "bad-shop", "store-uae", platform.Customize{
		Settings: map[string]any{"whatsap": "+971501234567"}}); err == nil || !strings.Contains(err.Error(), "whatsapp") {
		t.Fatalf("unknown settings column: %v", err)
	}
	if _, _, err := p.CreateCustomized(ctx, "usr_test", "bad-shop", "store-uae", platform.Customize{
		Rows: map[string][]map[string]any{"items": {{"name": "x"}}}}); err == nil || !strings.Contains(err.Error(), "products") {
		t.Fatalf("unknown table: %v", err)
	}
	if apps, _ := p.Apps("usr_test"); len(apps) != 0 {
		t.Fatalf("a refused call made an app: %v", apps)
	}

	_, res, err := p.CreateCustomized(ctx, "usr_test", "oud-house", "store-uae", platform.Customize{
		Business: "Oud House",
		Settings: map[string]any{"whatsapp": "+971501234567", "delivery_fee": 20},
		Rows: map[string][]map[string]any{"products": {
			{"name": "Royal Oud", "price": 250, "active": 1, "stock": 5},
			{"name": "Amber Musk", "price": 120, "active": 1, "stock": 9},
		}},
	})
	if err != nil || !res.OK {
		t.Fatalf("create: %v %+v", err, res)
	}
	home, err := p.TestRequest(ctx, "oud-house", platform.TestRequestInput{Path: "/"})
	if err != nil || home.Status != 200 {
		t.Fatalf("home: %v %d", err, home.Status)
	}
	for _, want := range []string{"Oud House", "Royal Oud", "Amber Musk"} {
		if !strings.Contains(home.Body, want) {
			t.Errorf("home page lacks %q", want)
		}
	}
}

// A page whose browser script calls another site passes every check, but
// visitors see nothing: the save says so, and names the way that works.
func TestBrowserFetchWarning(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "gold")
	def := catalog(t)
	def.Templates["rates"] = `<!doctype html><p id="g"></p><script>fetch("https://api.goldprice.example/v1/aed").then(r => r.json()).then(d => g.textContent = d.price)</script>
<script>fetch("/api/products")</script>`
	def.Routes = append(def.Routes, appdef.Route{Method: "GET", Path: "/rates", Template: "rates"})
	res, err := p.ApplyChange(ctx, "gold", def)
	if err != nil || !res.OK {
		t.Fatal(err, res)
	}
	got := strings.Join(res.Warnings, " ")
	if len(res.Warnings) != 1 || !strings.Contains(got, "api.goldprice.example") || !strings.Contains(got, "http.fetch") ||
		!strings.Contains(got, "not in the app's hosts") || !strings.Contains(got, "may fail") || strings.Contains(got, "see nothing") {
		t.Fatalf("warnings: %q", res.Warnings)
	}
	// Declared, the host is not called undeclared; the warning stays (it is still unchecked).
	def.Hosts = []appdef.Host{{Host: "api.goldprice.example", Purpose: "gold price"}}
	if res, _ := p.ApplyChange(ctx, "gold", def); len(res.Warnings) != 1 || strings.Contains(res.Warnings[0], "not in the app's hosts") {
		t.Fatalf("declared host: %q", res.Warnings)
	}
	// The finished examples call no other site from the browser.
	for i, ex := range []string{"store-uae", "booking-uae", "home-service-uae", "reservation-uae", "quote-portal-uae", "listings-uae", "clinic-queue-uae"} {
		_, res, err := p.CreateFromExample(ctx, "usr_test", "ex-"+string(rune('a'+i))+"-app", ex)
		if err != nil || len(res.Warnings) > 0 {
			t.Errorf("%s: %v %q", ex, err, res.Warnings)
		}
	}
}

// A section written into a template no page renders is reported:
// souq-al-oud's gold price went into store-uae's home_boutique, which the
// home page includes only with the boutique theme on. Templates the change
// did not touch, and edits to rendered ones, are not reported.
func TestUnusedEditedTemplateWarning(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	_, res, err := p.CreateCustomized(ctx, "usr_test", "oud", "store-uae", platform.Customize{Settings: map[string]any{"font": "modern"}})
	if err != nil || len(res.Warnings) > 0 {
		t.Fatalf("create: %v %q", err, res.Warnings)
	}
	def, _, _ := p.Definition("oud", 0)
	def.Templates["home_boutique"] += "<section>Gold today</section>"
	def.Templates["home"] += "<!-- edited -->"
	res, err = p.ApplyChange(ctx, "oud", def)
	if err != nil || !res.OK || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "templates.home_boutique") ||
		!strings.Contains(res.Warnings[0], "none of the checked pages rendered it") {
		t.Fatalf("edit of home_boutique: %v %q", err, res.Warnings)
	}
	def.Templates["gold_box"] = "<section>Gold</section>"
	res, _ = p.ApplyChange(ctx, "oud", def)
	if got := strings.Join(res.Warnings, " "); !strings.Contains(got, "templates.gold_box") || !strings.Contains(got, "no page renders it") ||
		strings.Contains(got, "home_boutique") {
		t.Fatalf("new unused template: %q", res.Warnings)
	}
}

// An app made for a business from any example keeps none of the example
// business's legal name, address or contacts: not in its definition, not on
// its home page. What else is still the example's is reported.
func TestCreateForBusinessLeavesNoExampleIdentity(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	identity := regexp.MustCompile(`^(legal_name|address|whatsapp|phone|email|licence|bank_details|office_reg_value)(_ar)?$`)
	n := 0
	for _, e := range examples.All() {
		def, _ := appdef.Parse(e.JSON)
		if def == nil || def.Brand == nil || len(def.Seed[def.Brand.Table]) == 0 {
			continue
		}
		var values []string
		for k, v := range def.Seed[def.Brand.Table][0] {
			if s, ok := v.(string); ok && len(s) > 5 && identity.MatchString(k) {
				values = append(values, s)
			}
		}
		if len(values) == 0 {
			t.Fatalf("%s: no identity values to look for", e.Name)
		}
		n++
		app := fmt.Sprintf("biz-%d-app", n)
		_, res, left, err := p.CreateForBusiness(ctx, "usr_test", app, e.Name, platform.Customize{Business: "Test Biz", ForBusiness: true})
		if err != nil || !res.OK {
			t.Fatalf("%s: %v %+v", e.Name, err, res.Problems)
		}
		for _, c := range res.Checks {
			if c.Error != "" || c.Status >= 500 {
				t.Errorf("%s: %s fails without the example's contacts: %d %s", e.Name, c.Route, c.Status, c.Error)
			}
		}
		got, _, _ := p.Definition(app, 0)
		raw, _ := json.Marshal(got.Seed) // column help may show a made-up example of the format
		home, _ := p.TestRequest(ctx, app, platform.TestRequestInput{Path: "/"})
		for _, v := range values {
			if strings.Contains(string(raw), v) || strings.Contains(home.Body, v) {
				t.Errorf("%s: still has the example's %q", e.Name, v)
			}
		}
		if len(left.Cleared) == 0 || len(left.Kept) == 0 {
			t.Errorf("%s: leftovers %+v", e.Name, left)
		}
		if !strings.Contains(home.Body, "Test Biz") {
			t.Errorf("%s: home page lacks the business name", e.Name)
		}
	}
	if n < 7 {
		t.Fatalf("only %d examples have a settings row", n)
	}
}

// Empty placeholder rows in the starting data are reported (salma's four
// blank, inactive services); the examples have none.
func TestEmptySeedRowsWarning(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "blank")
	def := catalog(t)
	def.Seed["products"] = append(def.Seed["products"], map[string]any{"name": "", "price_aed": 0}, map[string]any{"name": " ", "price_aed": 0})
	res, err := p.ApplyChange(ctx, "blank", def)
	if err != nil || !res.OK || !strings.Contains(strings.Join(res.Warnings, " "), "seed.products: row") {
		t.Fatalf("warnings: %v %q %q", err, res.Warnings, res.Problems)
	}
	for _, e := range examples.All() {
		d, _ := appdef.Parse(e.JSON)
		if w := emptySeedWarningsOf(d); len(w) > 0 {
			t.Errorf("%s: %q", e.Name, w)
		}
	}
}

// emptySeedWarningsOf is ApplyChange's empty-row warning for a definition.
func emptySeedWarningsOf(d *appdef.App) []string { return platform.EmptySeedWarnings(d) }

// Whole numbers from rows are int64: {{printf "%.2f" .n}} printed
// %!f(int64=34) on barakah-distribution's stock plan. printf now takes any
// number for a number verb, and a mismatch that still reaches a page is
// reported by the checks.
func TestPrintfTakesAnyNumber(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "stock")
	def := catalog(t)
	def.Scripts["plan"] = `function handle(req) { return {template: "plan", data: {n: 34, f: 2.5, s: "7", word: "abc"}} }`
	def.Templates["plan"] = `<p>{{printf "%.2f" .n}}|{{printf "%d" .f}}|{{printf "%.1f" .s}}|{{printf "%d%%" .n}}</p>`
	def.Templates["oops"] = `<p>{{printf "%d" .word}}</p>`
	def.Scripts["oops"] = `function handle(req) { return {template: "oops", data: {word: "abc"}} }`
	def.Routes = append(def.Routes, appdef.Route{Method: "GET", Path: "/plan", Script: "plan"}, appdef.Route{Method: "GET", Path: "/oops", Script: "oops"})
	res, err := p.ApplyChange(ctx, "stock", def)
	if err != nil || !res.OK {
		t.Fatal(err, res.Problems)
	}
	page, _ := p.TestRequest(ctx, "stock", platform.TestRequestInput{Path: "/plan"})
	if !strings.Contains(page.Body, "<p>34.00|3|7.0|34%</p>") {
		t.Fatalf("plan: %s", page.Body)
	}
	var found bool
	for _, c := range res.Checks {
		if c.Route == "GET /oops" && strings.Contains(strings.Join(c.Warnings, " "), "a number did not format") {
			found = true
		}
		if c.Route == "GET /plan" && len(c.Warnings) > 0 {
			t.Errorf("plan warnings: %q", c.Warnings)
		}
	}
	if !found {
		t.Fatalf("checks: %+v", res.Checks)
	}
}

// "Save order" got a bare 400 and the page showed nothing; nobody could
// tell why. On the preview, the log line of a 4xx or 5xx the app's script
// gave says the route, the script and what it answered, and submit_form
// returns the same reason.
func TestFailedAnswersSayWhy(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "orders")
	def := catalog(t)
	def.Scripts["order"] = `function handle(req) { if (!req.form.qty) return {status: 400, json: {error: "qty is required"}}; return {status: 422, text: ""} }`
	def.Routes = append(def.Routes, appdef.Route{Method: "POST", Path: "/my-route/order", Script: "order"})
	if res, err := p.ApplyChange(ctx, "orders", def); err != nil || !res.OK {
		t.Fatal(err, res.Problems)
	}
	r, err := p.TestRequest(ctx, "orders", platform.TestRequestInput{Method: "POST", Path: "/my-route/order", Form: map[string]string{"shop": "1"}})
	if err != nil || r.Status != 400 || !strings.Contains(r.Reason, "route POST /my-route/order, script order") || !strings.Contains(r.Reason, "qty is required") {
		t.Fatalf("400: %v %+v", err, r.Reason)
	}
	r, _ = p.TestRequest(ctx, "orders", platform.TestRequestInput{Method: "POST", Path: "/my-route/order", Form: map[string]string{"qty": "2"}})
	if r.Status != 422 || !strings.Contains(r.Reason, "empty body") {
		t.Fatalf("422: %+v", r.Reason)
	}
	events, _ := p.Logs("orders", applog.Filter{Stage: platform.Preview})
	var logged bool
	for _, e := range events {
		logged = logged || strings.Contains(e.Message, "POST /my-route/order -> 400") && strings.Contains(e.Message, "qty is required")
	}
	if !logged {
		t.Fatal("get_logs does not say why the order failed")
	}
}

// Logs are in UTC, and an assistant reported 20:01 UTC as UAE time. Every
// event also carries the time in the app's time zone, with its offset.
func TestLogsHaveLocalTime(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "clock")
	if res, err := p.ApplyChange(ctx, "clock", catalog(t)); err != nil || !res.OK {
		t.Fatal(err, res.Problems)
	}
	r, _ := p.TestRequest(ctx, "clock", platform.TestRequestInput{Path: "/"})
	if len(r.Logs) == 0 || !strings.HasSuffix(r.Logs[0].LocalTime, "+04:00 Asia/Dubai") || !strings.HasSuffix(r.Logs[0].Time, "Z") {
		t.Fatalf("test request log: %+v", r.Logs)
	}
	events, _ := p.Logs("clock", applog.Filter{})
	utc, _ := time.Parse(time.RFC3339Nano, events[0].Time)
	if want := utc.In(time.FixedZone("", 4*3600)).Format("2006-01-02 15:04:05") + " +04:00 Asia/Dubai"; events[0].LocalTime != want {
		t.Fatalf("local_time %q, want %q", events[0].LocalTime, want)
	}
}
