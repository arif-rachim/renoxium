package platform_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

func TestPlansDecideFeeBadgeAndAppLimit(t *testing.T) {
	fs := &fakeStripe{charges: true}
	srv := fs.server(t)
	plans := map[string]string{"usr_free": "free", "usr_pro": "pro"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", SiteURL: "https://www.renoxium.test",
		Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: srv.URL}, OwnerPlan: func(id string) string { return plans[id] }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	for _, owner := range []string{"usr_free", "usr_pro"} {
		app := strings.TrimPrefix(owner, "usr_") + "-shop"
		if _, err := p.CreateApp(owner, app); err != nil {
			t.Fatal(err)
		}
		p.ApplyChange(ctx, app, shopWithCheckout())
		p.Publish(app, 0)
		if _, err := p.StripeOnboardingURL(ctx, app, "o@x.ae", "https://r", "https://f"); err != nil {
			t.Fatal(err)
		}
		if pa, err := p.RefreshPaymentAccount(ctx, app); err != nil || !pa.ChargesEnabled {
			t.Fatal(pa, err)
		}
	}

	// Free: 2% of 500 AED = 10 AED (1000 fils) to Renoxium, a badge, and one app only.
	do(t, p, "POST", "free-shop.renoxium.test", "/buy", url.Values{})
	if fee := fs.sessions[len(fs.sessions)-1].Get("payment_intent_data[application_fee_amount]"); fee != "1000" {
		t.Fatalf("free plan fee = %q", fee)
	}
	if _, err := p.CreateApp("usr_free", "free-second"); err == nil || !strings.Contains(err.Error(), "free plan allows 1") ||
		!strings.Contains(err.Error(), "https://www.renoxium.test/billing") {
		t.Fatalf("second free app: %v", err)
	}
	// Pro: no fee, no badge, more apps.
	do(t, p, "POST", "pro-shop.renoxium.test", "/buy", url.Values{})
	if fee := fs.sessions[len(fs.sessions)-1].Get("payment_intent_data[application_fee_amount]"); fee != "" {
		t.Fatalf("pro plan fee = %q", fee)
	}
	if _, err := p.CreateApp("usr_pro", "pro-second"); err != nil {
		t.Fatalf("second pro app: %v", err)
	}
}

func TestFreePlanBadgeOnLivePagesOnly(t *testing.T) {
	plans := map[string]string{"usr_free": "free", "usr_pro": "pro"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		OwnerPlan: func(id string) string { return plans[id] }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	page := &appdef.App{Templates: map[string]string{"t": "<!doctype html><html><head></head><body><h1>Shop</h1></body></html>"},
		Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}}
	for owner, want := range map[string]bool{"usr_free": true, "usr_pro": false} {
		app := strings.TrimPrefix(owner, "usr_") + "-page"
		p.CreateApp(owner, app)
		p.ApplyChange(context.Background(), app, page)
		p.Publish(app, 0)
		live := do(t, p, "GET", app+".renoxium.test", "/", nil)
		if got := strings.Contains(live.body, "Made with Renoxium"); got != want {
			t.Errorf("%s live badge = %v", owner, got)
		}
		if want && strings.Index(live.body, "Made with Renoxium") > strings.Index(live.body, "</body>") {
			t.Error("badge after </body>")
		}
		res, _ := p.TestRequest(context.Background(), app, platform.TestRequestInput{Path: "/"})
		if strings.Contains(res.Body, "Made with Renoxium") {
			t.Errorf("%s preview shows the badge", owner)
		}
	}
}
