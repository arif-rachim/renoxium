package platform_test

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

func TestPlansDecideFeeAndPublishing(t *testing.T) {
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
	connect := func(app string) {
		t.Helper()
		if _, err := p.StripeOnboardingURL(ctx, app, "o@x.ae", "https://r", "https://f"); err != nil {
			t.Fatal(err)
		}
		if pa, err := p.RefreshPaymentAccount(ctx, app); err != nil || !pa.ChargesEnabled {
			t.Fatal(pa, err)
		}
	}
	for owner, app := range map[string]string{"usr_free": "free-shop", "usr_pro": "pro-shop"} {
		if _, err := p.CreateApp(owner, app); err != nil {
			t.Fatal(err)
		}
		p.ApplyChange(ctx, app, shopWithCheckout())
	}

	// A free account's app with server code goes live only once the owner
	// chose how it is paid for; a shop may connect Stripe afterwards.
	if _, err := p.Publish("free-shop", 0); err == nil || !strings.Contains(err.Error(), "https://www.renoxium.test/apps/free-shop/plan") {
		t.Fatalf("publish without a plan: %v", err)
	}
	p.SetAppKind("free-shop", platform.KindShop)
	if _, err := p.Publish("free-shop", 0); err != nil {
		t.Fatalf("shop: %v", err)
	}
	connect("free-shop")
	// An account on a paid plan needs neither.
	if _, err := p.Publish("pro-shop", 0); err != nil {
		t.Fatalf("pro: %v", err)
	}
	connect("pro-shop")

	// A shop pays 2% of 500 AED = 10 AED (1000 fils); Pro pays nothing.
	do(t, p, "POST", "free-shop.renoxium.test", "/buy", url.Values{})
	if fee := fs.sessions[len(fs.sessions)-1].Get("payment_intent_data[application_fee_amount]"); fee != "1000" {
		t.Fatalf("shop fee = %q", fee)
	}
	do(t, p, "POST", "pro-shop.renoxium.test", "/buy", url.Values{})
	if fee := fs.sessions[len(fs.sessions)-1].Get("payment_intent_data[application_fee_amount]"); fee != "" {
		t.Fatalf("pro plan fee = %q", fee)
	}

	// A work app pays its subscription instead of the fee, and pauses when
	// the subscription ends.
	p.CreateApp("usr_free", "free-work")
	p.ApplyChange(ctx, "free-work", shopWithCheckout())
	p.SetAppKind("free-work", platform.KindWork)
	if _, err := p.Publish("free-work", 0); err == nil || !strings.Contains(err.Error(), "subscribe") {
		t.Fatalf("work app without a subscription: %v", err)
	}
	platform.PaidWorkApp(t, p, "free-work")
	if _, err := p.Publish("free-work", 0); err != nil {
		t.Fatal(err)
	}
	connect("free-work")
	do(t, p, "POST", "free-work.renoxium.test", "/buy", url.Values{})
	if fee := fs.sessions[len(fs.sessions)-1].Get("payment_intent_data[application_fee_amount]"); fee != "" {
		t.Fatalf("work app fee = %q", fee)
	}
	if got := p.StaffLimit("free-work"); got != platform.WorkAppUsers {
		t.Fatalf("work app staff = %d", got)
	}
	if !p.CustomDomainAllowed("free-work") {
		t.Fatal("work app may not have its own domain")
	}
	p.SetWorkSubscription("free-work", "sub_free-work", "canceled", time.Time{})
	if r := do(t, p, "GET", "free-work.renoxium.test", "/", nil); r.status != 402 || !strings.Contains(r.body, "paused") {
		t.Fatalf("lapsed work app: %d %s", r.status, r.body)
	}

	// The free plan allows ten apps.
	for i := 3; i <= platform.AppsPerPlan["free"]; i++ {
		if _, err := p.CreateApp("usr_free", fmt.Sprintf("free-%d", i)); err != nil {
			t.Fatalf("free app %d: %v", i, err)
		}
	}
	if _, err := p.CreateApp("usr_free", "free-too-many"); err == nil || !strings.Contains(err.Error(), "https://www.renoxium.test/billing") {
		t.Fatalf("app over the free limit: %v", err)
	}
}

// A static site needs no plan, and no app shows Renoxium branding.
func TestStaticSiteIsFreeAndUnbranded(t *testing.T) {
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		OwnerPlan: func(string) string { return "free" }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	page := &appdef.App{Templates: map[string]string{"t": "<!doctype html><html><head></head><body><h1>Shop</h1></body></html>"},
		Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}}
	if !page.Static() {
		t.Fatal("a page-only app is not static")
	}
	p.CreateApp("usr_free", "free-page")
	p.ApplyChange(context.Background(), "free-page", page)
	if _, err := p.Publish("free-page", 0); err != nil {
		t.Fatalf("static site: %v", err)
	}
	if live := do(t, p, "GET", "free-page.renoxium.test", "/", nil); !strings.Contains(live.body, "<h1>Shop</h1>") || strings.Contains(live.body, "Renoxium") {
		t.Fatalf("live page: %s", live.body)
	}
	if p.CustomDomainAllowed("free-page") {
		t.Fatal("a free static site may have its own domain")
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
	platform.ReadyShop(t, p, "club")
	if _, err := p.Publish("club", 0); err != nil {
		t.Fatal(err)
	}
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
	// A free account's app gets staff once it is a shop (2) or a paid work app (5).
	if err := p.AddStaff("free-shop", "a@x.ae", "usr_free"); err == nil || !strings.Contains(err.Error(), "shop or a work app") {
		t.Fatalf("free staff: %v", err)
	}
	platform.ReadyShop(t, p, "free-shop")
	if got := p.StaffLimit("free-shop"); got != platform.ShopStaff {
		t.Fatalf("shop staff limit = %d", got)
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
