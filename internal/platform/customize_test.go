package platform_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
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
