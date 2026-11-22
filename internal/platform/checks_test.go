package platform_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// Checks after a change render parameter routes from seed rows and
// signed-in routes as a visitor, and report template errors there and
// missing template keys.
func TestChecksRenderParamAndSignedInRoutes(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "checks")
	def := &appdef.App{
		Tables: []appdef.Table{
			{Name: "services", Columns: []appdef.Column{{Name: "name", Type: "text"}, {Name: "slug", Type: "text"}}},
			{Name: "orders", Columns: []appdef.Column{{Name: "service_id", Type: "integer"}, {Name: "user_id", Type: "integer"}}},
		},
		Seed: map[string][]map[string]any{"services": {{"name": "Cleaning", "slug": "cleaning"}}},
		Templates: map[string]string{
			"page":    `<!doctype html><html><head><title>x</title></head><body>{{.params.id}}</body></html>`,
			"service": `<!doctype html><html><head></head><body><h1>{{.s.name}}</h1><p>{{.s.price}}</p></body></html>`,
			"broken":  "<!doctype html><html><head></head><body>\n{{.params.id}}\n{{index .user.email 5}}</body></html>",
			"mine":    `<!doctype html><html><head></head><body>{{.user.email}}</body></html>`,
		},
		Scripts: map[string]string{
			"service": `function handle(req){ const s = db.query('SELECT * FROM services WHERE id = ?', Number(req.params.id))[0]; if (!s) return {status: 404, text: 'no'}; return {template: 'service', data: {s}} }`,
			"byslug":  `function handle(req){ const s = db.query('SELECT * FROM services WHERE slug = ?', req.params.slug)[0]; if (!s) return {status: 404, text: 'no'}; return {template: 'service', data: {s}} }`,
		},
		Routes: []appdef.Route{
			{Method: "GET", Path: "/", Template: "page"},
			{Method: "GET", Path: "/s/:id", Script: "service"},
			{Method: "GET", Path: "/services/by/:slug", Script: "byslug"},
			{Method: "GET", Path: "/orders/:id", Template: "broken", Auth: "user"},
			{Method: "GET", Path: "/me", Template: "mine", Auth: "user"},
			{Method: "GET", Path: "/zones/:id", Template: "page"},
		},
	}
	res, err := p.ApplyChange(ctx, "checks", def)
	if err != nil || !res.OK {
		t.Fatal(err, res)
	}
	byRoute := map[string]int{}
	for i, c := range res.Checks {
		byRoute[c.Route] = i
	}
	get := func(route string) (c struct {
		Path, As, Error, Note string
		Status                int
		Warnings              []string
	}) {
		t.Helper()
		i, ok := byRoute["GET "+route]
		if !ok {
			t.Fatalf("no check for %s: %+v", route, res.Checks)
		}
		x := res.Checks[i]
		c.Path, c.As, c.Error, c.Note, c.Status, c.Warnings = x.Path, x.As, x.Error, x.Note, x.Status, x.Warnings
		return c
	}
	if c := get("/s/:id"); c.Path != "/s/1" || c.Status != 200 || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], `service:1:67: {{.s.price}} prints nothing`) {
		t.Fatalf("/s/:id: %+v", c)
	}
	if c := get("/services/by/:slug"); c.Path != "/services/by/cleaning" || c.Status != 200 {
		t.Fatalf("slug: %+v", c)
	}
	if c := get("/me"); c.Status != 200 || c.As != "a signed-in check visitor" {
		t.Fatalf("/me: %+v", c)
	}
	// No order rows: the route is reported, not rendered.
	if c := get("/orders/:id"); c.Status != 0 || !strings.Contains(c.Note, "no row") {
		t.Fatalf("/orders/:id without rows: %+v", c)
	}
	if c := get("/zones/:id"); c.Status != 0 || c.Note == "" {
		t.Fatalf("no table: %+v", c)
	}

	// With an order, the signed-in route renders as its owner, and a
	// template error deep in it names the template and line.
	def.Seed["orders"] = []map[string]any{{"service_id": 1, "user_id": 0}}
	def.Templates["broken"] = "<!doctype html><html><head></head><body>\n{{.params.id}}\n{{index .user.nope 5}}</body></html>"
	p.CreateApp("usr_test", "checks-two")
	res, _ = p.ApplyChange(ctx, "checks-two", def)
	for _, c := range res.Checks {
		if c.Route == "GET /orders/:id" {
			if c.Path != "/orders/1" || c.Status != 500 || !strings.Contains(c.Error, "broken:3") || c.As == "" {
				t.Fatalf("broken signed-in page: %+v", c)
			}
			return
		}
	}
	t.Fatal("no check for /orders/:id")
}

// Every example renders all its GET routes without errors, including the
// pages with parameters and the signed-in ones.
func TestExamplesPassBroaderChecks(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	for _, name := range []string{"home-service-uae", "store-uae", "booking-uae", "reservation-uae", "quote-portal-uae", "listings-uae", "clinic-queue-uae", "catalog", "tasks", "delivery-tracking"} {
		app := "ex-" + strings.ReplaceAll(name, "-uae", "")
		_, res, err := p.CreateFromExample(ctx, "usr_test", app, name)
		if err != nil || !res.OK {
			t.Fatalf("%s: %v %+v", name, err, res)
		}
		for _, c := range res.Checks {
			if c.Error != "" || c.Status >= 500 || len(c.Warnings) > 0 {
				t.Errorf("%s %s (%s): %d %s %v", name, c.Route, c.Path, c.Status, c.Error, c.Warnings)
			}
		}
	}
}
