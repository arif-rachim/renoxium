package platform_test

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

func TestPlansDecideFeeAndAppLimit(t *testing.T) {
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

	// Free: 2% of 500 AED = 10 AED (1000 fils) to Renoxium, and ten apps.
	do(t, p, "POST", "free-shop.renoxium.test", "/buy", url.Values{})
	if fee := fs.sessions[len(fs.sessions)-1].Get("payment_intent_data[application_fee_amount]"); fee != "1000" {
		t.Fatalf("free plan fee = %q", fee)
	}
	for i := 2; i <= platform.AppsPerPlan["free"]; i++ {
		if _, err := p.CreateApp("usr_free", fmt.Sprintf("free-%d", i)); err != nil {
			t.Fatalf("free app %d: %v", i, err)
		}
	}
	if _, err := p.CreateApp("usr_free", "free-too-many"); err == nil || !strings.Contains(err.Error(), "Professional plan allows 10 apps") ||
		!strings.Contains(err.Error(), "https://www.renoxium.test/billing") {
		t.Fatalf("app over the Professional limit: %v", err)
	}
	// Pro: no fee, more apps.
	do(t, p, "POST", "pro-shop.renoxium.test", "/buy", url.Values{})
	if fee := fs.sessions[len(fs.sessions)-1].Get("payment_intent_data[application_fee_amount]"); fee != "" {
		t.Fatalf("pro plan fee = %q", fee)
	}
	if _, err := p.CreateApp("usr_pro", "pro-second"); err != nil {
		t.Fatalf("second pro app: %v", err)
	}
}

// No plan adds Renoxium branding to an owner's pages.
func TestNoBadgeOnAnyPlan(t *testing.T) {
	plans := map[string]string{"usr_free": "free", "usr_pro": "pro"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		OwnerPlan: func(id string) string { return plans[id] }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	page := &appdef.App{Templates: map[string]string{"t": "<!doctype html><html><head></head><body><h1>Shop</h1></body></html>"},
		Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}}
	for _, owner := range []string{"usr_free", "usr_pro"} {
		app := strings.TrimPrefix(owner, "usr_") + "-page"
		p.CreateApp(owner, app)
		p.ApplyChange(context.Background(), app, page)
		p.Publish(app, 0)
		if live := do(t, p, "GET", app+".renoxium.test", "/", nil); !strings.Contains(live.body, "<h1>Shop</h1>") || strings.Contains(live.body, "Renoxium") {
			t.Errorf("%s live page: %s", owner, live.body)
		}
	}
}

func TestWhatsAppQuotaPerPlan(t *testing.T) {
	codes := &codeBox{codes: map[string]string{}}
	box := &outbox{}
	plans := map[string]string{"usr_free": "free"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", WhatsApp: codes, Mailer: box,
		OwnerPlan: func(id string) string { return plans[id] }, OwnerEmail: func(string) (string, bool) { return "owner@club.ae", true }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	def := membersApp()
	delete(def.Templates, "auth_login")
	p.CreateApp("usr_free", "club")
	p.ApplyChange(context.Background(), "club", def)
	p.Publish("club", 0)
	// This test is about the monthly quota alone: lift the daily share of
	// new numbers and the part kept for known ones (see TestWhatsAppNewNumbers).
	platform.WhatsAppNewPerDayShare, platform.WhatsAppKnownReserveShare = 1, 0
	defer func() { platform.WhatsAppNewPerDayShare, platform.WhatsAppKnownReserveShare = 4, 5 }()
	quota := platform.WhatsAppCodesPerPlan["free"]
	for i := 0; i < quota; i++ {
		v := newVisitor(t, p, "club.renoxium.test")
		v.ip = "198.51.100." + strconv.Itoa(i+1)
		phone := "+97150" + strings.Repeat("0", 6-len(strconv.Itoa(i))) + strconv.Itoa(i) + "1"
		if w := v.submit("/_rx/auth/phone", "/_rx/auth/phone", url.Values{"phone": {phone}}); w.Code != 200 {
			t.Fatalf("code %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if used, q := p.WhatsAppUsage("usr_free"); used != quota || q != quota {
		t.Fatalf("usage %d of %d", used, q)
	}
	v := newVisitor(t, p, "club.renoxium.test")
	if w := v.do("GET", "/_rx/auth/phone", nil); w.Code != 404 || !strings.Contains(w.Body.String(), "sign in with your email") {
		t.Fatalf("past the quota: %d", w.Code)
	}
	if w := v.do("GET", "/_rx/auth/login", nil); strings.Contains(w.Body.String(), "/_rx/auth/phone") {
		t.Fatal("sign-in page still offers WhatsApp")
	}
	if box.last(t, "owner_notice").Subject != "[club] WhatsApp sign-in paused until next month" {
		t.Fatal("owner not told")
	}
	// Preview is never limited: its codes go to the log, not to WhatsApp.
	if res, _ := p.TestRequest(context.Background(), "club", platform.TestRequestInput{Path: "/_rx/auth/phone"}); res.Status != 200 {
		t.Fatalf("preview: %d", res.Status)
	}
}

func TestStaffLimitPerPlan(t *testing.T) {
	plans := map[string]string{"usr_free": "free", "usr_pro": "pro"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", SiteURL: "https://www.renoxium.test",
		OwnerPlan: func(id string) string { return plans[id] }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.CreateApp("usr_free", "free-shop")
	p.CreateApp("usr_pro", "pro-shop")
	if err := p.AddStaff("free-shop", "a@x.ae", "usr_free"); err != nil || !p.IsStaff("free-shop", "a@x.ae") {
		t.Fatalf("staff on the Professional plan: %v", err)
	}
	for _, e := range []string{"a@x.ae", "b@x.ae"} {
		if err := p.AddStaff("pro-shop", e, "usr_pro"); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.AddStaff("pro-shop", "c@x.ae", "usr_pro"); err == nil || !strings.Contains(err.Error(), "allows 2") {
		t.Fatalf("third pro staff: %v", err)
	}
	if err := p.AddStaff("pro-shop", "A@X.ae", "usr_pro"); err != nil || !p.IsStaff("pro-shop", "a@x.ae") {
		t.Fatalf("existing staff again: %v", err)
	}
	if err := p.AddStaff("pro-shop", "not an email", "usr_pro"); err == nil {
		t.Fatal("bad email accepted")
	}
}

func TestStorageQuotaPerAccount(t *testing.T) {
	plans := map[string]string{"usr_free": "free"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		OwnerPlan: func(id string) string { return plans[id] }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	old := platform.StorageMBPerPlan["free"]
	platform.StorageMBPerPlan["free"] = 0 // every new byte is over quota
	defer func() { platform.StorageMBPerPlan["free"] = old }()
	p.CreateApp("usr_free", "shop-a")
	if _, err := p.AddFile("shop-a", bytes.NewReader(pngFile(t, 1))); err == nil || !strings.Contains(err.Error(), "storage is full") {
		t.Fatalf("over quota: %v", err)
	}
	platform.StorageMBPerPlan["free"] = 1
	if _, err := p.AddFile("shop-a", bytes.NewReader(pngFile(t, 1))); err != nil {
		t.Fatal(err)
	}
	used, quota := p.StorageUsage("usr_free")
	if used == 0 || quota != 1<<20 {
		t.Errorf("usage = %d of %d", used, quota)
	}
}
