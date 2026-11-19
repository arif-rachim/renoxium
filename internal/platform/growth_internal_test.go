package platform

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

func growthPlatform(t *testing.T) *Platform {
	t.Helper()
	p, err := New(Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func example(t *testing.T, name string) *appdef.App {
	t.Helper()
	ex, ok := examples.Get(name)
	if !ok {
		t.Fatalf("no example %s", name)
	}
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	return def
}

func events(t *testing.T, p *Platform) map[string]map[string]FunnelEvent {
	t.Helper()
	ev, err := p.FunnelEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

var noticeApp = &appdef.App{
	Tables:    []appdef.Table{{Name: "requests", Columns: []appdef.Column{{Name: "item", Type: "text"}}}},
	Scripts:   map[string]string{"order": `function handle(req) { notify.owner({subject: 'New order', text: 'x'}); return {text: 'ok'} }`},
	Templates: map[string]string{"t": "shop"},
	Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}, {Method: "POST", Path: "/order", Script: "order"}},
}

func TestFunnelEventsAreFirstOnly(t *testing.T) {
	p := growthPlatform(t)
	ctx := context.Background()
	if _, err := p.CreateApp("usr_a", "first-shop"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // times have one-second resolution
	p.CreateApp("usr_a", "second-shop")
	ev := events(t, p)["usr_a"]
	if ev[FunnelApp].App != "first-shop" {
		t.Fatalf("first app = %+v", ev[FunnelApp])
	}
	if _, ok := ev[FunnelPublish]; ok {
		t.Fatal("publish before publishing")
	}

	// A copy of the booking example is recognised when it goes live.
	if res, err := p.ApplyChange(ctx, "first-shop", example(t, "booking-uae")); err != nil || !res.OK {
		t.Fatalf("apply: %v %v", err, res.Problems)
	}
	if _, err := p.Publish("first-shop", 0); err != nil {
		t.Fatal(err)
	}
	p.ApplyChange(ctx, "second-shop", noticeApp)
	p.Publish("second-shop", 0)
	ev = events(t, p)["usr_a"]
	if e := ev[FunnelPublish]; e.App != "first-shop" || e.Detail != "booking-uae" || e.At.IsZero() {
		t.Fatalf("publish = %+v", e)
	}

	// Owner notices count only from live.
	p.TestRequest(ctx, "second-shop", TestRequestInput{Method: "POST", Path: "/order"})
	if _, ok := events(t, p)["usr_a"][FunnelActivity]; ok {
		t.Fatal("a preview notice counted as customer activity")
	}
	w := httptest.NewRecorder()
	p.ServeApp(w, httptest.NewRequest("POST", "http://second-shop.renoxium.test/order", nil))
	if w.Code != 200 {
		t.Fatalf("order: %d %s", w.Code, w.Body)
	}
	e := events(t, p)["usr_a"][FunnelActivity]
	if e.App != "second-shop" || e.Detail != "notice" {
		t.Fatalf("activity = %+v", e)
	}
	// A second notice (also after a restart's empty memory) keeps the first.
	p.funnelSeen.Clear()
	p.ServeApp(httptest.NewRecorder(), httptest.NewRequest("POST", "http://second-shop.renoxium.test/order", nil))
	if got := events(t, p)["usr_a"][FunnelActivity]; !got.At.Equal(e.At) {
		t.Fatalf("activity moved: %v -> %v", e.At, got.At)
	}
}

func TestFunnelLeavesOutTheDemoOwner(t *testing.T) {
	p := growthPlatform(t)
	p.funnel(DemoOwner, FunnelApp, "demo-store-uae", "")
	p.funnel("", FunnelApp, "orphan", "")
	ctx := context.Background()
	ex, _ := examples.Get("tasks")
	if err := p.EnsureDemos(ctx, []examples.Example{ex}); err != nil {
		t.Fatal(err)
	}
	if err := p.BackfillFunnel(ctx); err != nil {
		t.Fatal(err)
	}
	if ev := events(t, p); len(ev) != 0 {
		t.Fatalf("events = %+v", ev)
	}
}

func TestBackfillFunnel(t *testing.T) {
	p := growthPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_old", "old-shop")
	p.CreateApp("usr_old", "older-shop")
	p.ApplyChange(ctx, "old-shop", example(t, "store-uae"))
	p.Publish("old-shop", 0)
	// A database from before funnel events: only apps, versions and payments.
	p.db.Exec(`DELETE FROM funnel_events`)
	p.db.Exec(`UPDATE apps SET created_at = '2026-01-05T10:00:00Z' WHERE name = 'older-shop'`)
	p.db.Exec(`UPDATE apps SET created_at = '2026-01-09T10:00:00Z' WHERE name = 'old-shop'`)
	p.db.Exec(`UPDATE versions SET created_at = '2026-01-10T08:00:00Z' WHERE app = 'old-shop'`)
	p.db.Exec(`INSERT INTO payment_accounts (app, stripe_account, charges_enabled, updated_at) VALUES ('old-shop', 'acct_1', 1, ?)`, now())
	st, err := p.loadStage("old-shop", Live)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO rx_payments (id, reference, status, amount, currency, items, success_path, cancel_path, created_at, updated_at, application_fee)
			VALUES ('pay_1', 'o1', 'paid', 10000, 'aed', '[]', '/', '/', '2026-02-01 09:00:00', '2026-02-01 09:01:00', 200)`,
		`INSERT INTO rx_payments (id, reference, status, amount, currency, items, success_path, cancel_path, created_at, updated_at, application_fee)
			VALUES ('pay_2', 'o2', 'pending', 5000, 'aed', '[]', '/', '/', '2026-01-20 09:00:00', NULL, 100)`,
		`INSERT INTO rx_payments (id, reference, status, amount, currency, items, success_path, cancel_path, updated_at, application_fee)
			VALUES ('pay_3', 'o3', 'paid', 25000, 'aed', '[]', '/', '/', datetime('now'), 500)`,
	} {
		if _, err := st.admin.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ { // repeating it changes nothing
		if err := p.BackfillFunnel(ctx); err != nil {
			t.Fatal(err)
		}
	}
	ev := events(t, p)["usr_old"]
	if e := ev[FunnelApp]; e.App != "older-shop" || e.At.Format(time.RFC3339) != "2026-01-05T10:00:00Z" {
		t.Errorf("app = %+v", e)
	}
	if e := ev[FunnelPublish]; e.App != "old-shop" || e.Detail != "store-uae" || e.At.Format(time.RFC3339) != "2026-01-10T08:00:00Z" {
		t.Errorf("publish = %+v", e)
	}
	if e := ev[FunnelActivity]; e.Detail != "card" || e.At.Format(time.RFC3339) != "2026-02-01T09:01:00Z" {
		t.Errorf("activity = %+v", e)
	}
	// A recorded event that is later than one found in old data moves back.
	p.db.Exec(`UPDATE funnel_events SET at = '2026-03-01T00:00:00Z' WHERE kind = 'app'`)
	p.BackfillFunnel(ctx)
	if e := events(t, p)["usr_old"][FunnelApp]; e.At.Format(time.RFC3339) != "2026-01-05T10:00:00Z" {
		t.Errorf("app after backfill = %+v", e)
	}

	// Card fees of the last 30 days: only the recent paid payment.
	fees, err := p.CardFees(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil || fees["aed"] != 500 || len(fees) != 1 {
		t.Fatalf("fees = %v %v", fees, err)
	}
}

func TestOperatorSettings(t *testing.T) {
	p := growthPlatform(t)
	if p.GrossMargin() != DefaultGrossMarginPct {
		t.Fatal("default margin")
	}
	if p.SetGrossMargin(0) == nil || p.SetGrossMargin(101) == nil {
		t.Fatal("bad margin accepted")
	}
	p.SetGrossMargin(65)
	if p.GrossMargin() != 65 {
		t.Fatal("margin not stored")
	}
	for _, bad := range []string{"2026-13", "26-01", "2026-1", "x"} {
		if p.SetMarketingSpend(bad, 10) == nil {
			t.Errorf("month %q accepted", bad)
		}
	}
	if p.SetMarketingSpend("2026-10", -1) == nil {
		t.Error("negative spend accepted")
	}
	p.SetMarketingSpend("2026-10", 1500)
	p.SetMarketingSpend("2026-11", 800)
	p.SetMarketingSpend("2026-11", 900)
	spend, err := p.MarketingSpend()
	if err != nil || len(spend) != 2 || spend["2026-10"] != 1500 || spend["2026-11"] != 900 {
		t.Fatalf("spend = %v %v", spend, err)
	}
}

func TestGuessTemplate(t *testing.T) {
	for _, name := range []string{"store-uae", "booking-uae", "home-service-uae", "listings-uae", "quote-portal-uae", "reservation-uae"} {
		def := example(t, name)
		// Agents add a table or two of their own.
		def.Tables = append(def.Tables, appdef.Table{Name: "reviews"})
		if got := guessTemplate(def); got != name {
			t.Errorf("%s recognised as %q", name, got)
		}
	}
	if got := guessTemplate(noticeApp); got != "" {
		t.Errorf("a custom app recognised as %q", got)
	}
	data, _ := os.ReadFile("../../examples/catalog.json")
	def, _ := appdef.Parse(data)
	if guessTemplate(def) != "catalog" {
		t.Error("catalog")
	}
}
