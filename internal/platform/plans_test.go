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
	plans := map[string]string{"usr_free": "free", "usr_member": "member", "usr_granted": "granted"}
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
	for owner, app := range map[string]string{"usr_free": "free-shop", "usr_member": "member-shop", "usr_granted": "granted-shop"} {
		if _, err := p.CreateApp(owner, app); err != nil {
			t.Fatal(err)
		}
		p.ApplyChange(ctx, app, shopWithCheckout())
	}

	// A free or member account's app with server code goes live only once
	// the owner chose how it is paid for; a shop may connect Stripe afterwards.
	for _, app := range []string{"free-shop", "member-shop"} {
		if _, err := p.Publish(app, 0); err == nil || !strings.Contains(err.Error(), "https://www.renoxium.test/apps/"+app+"/plan") {
			t.Fatalf("%s: publish without a plan: %v", app, err)
		}
		if pl, _ := p.AppPlan(app); pl.Included {
			t.Fatalf("%s is included", app)
		}
		p.SetAppKind(app, platform.KindShop)
		if _, err := p.Publish(app, 0); err != nil {
			t.Fatalf("%s: shop: %v", app, err)
		}
	}
	if _, q := p.WhatsAppUsage("usr_free"); q != platform.WhatsAppCodesPerPlan["free"] {
		t.Fatalf("shop without Stripe: quota %d", q)
	}
	if _, q := p.WhatsAppUsage("usr_member"); q != 2000 {
		t.Fatalf("member: quota %d", q)
	}
	connect("free-shop")
	platform.ForgetPlan("usr_free")
	// Once Stripe takes its payments, the shop's account gets the member limits.
	if _, q := p.WhatsAppUsage("usr_free"); q != platform.WhatsAppCodesPerPlan["member"] {
		t.Fatalf("shop with Stripe: quota %d", q)
	}
	// A granted account needs neither.
	if pl, _ := p.AppPlan("granted-shop"); !pl.Included || !pl.Ready {
		t.Fatalf("granted plan %+v", pl)
	}
	if _, err := p.Publish("granted-shop", 0); err != nil {
		t.Fatalf("granted: %v", err)
	}
	connect("member-shop")
	connect("granted-shop")

	// A shop pays 2% of 500 AED = 10 AED (1000 fils), on a member's account
	// too; a granted account pays nothing.
	for _, app := range []string{"free-shop", "member-shop"} {
		do(t, p, "POST", app+".renoxium.test", "/buy", url.Values{})
		if fee := fs.sessions[len(fs.sessions)-1].Get("payment_intent_data[application_fee_amount]"); fee != "1000" {
			t.Fatalf("%s fee = %q", app, fee)
		}
	}
	do(t, p, "POST", "granted-shop.renoxium.test", "/buy", url.Values{})
	if fee := fs.sessions[len(fs.sessions)-1].Get("payment_intent_data[application_fee_amount]"); fee != "" {
		t.Fatalf("granted fee = %q", fee)
	}

	// A work app pays its subscription instead of the fee, on a member's
	// account too, and pauses when the subscription ends.
	for _, owner := range []string{"usr_free", "usr_member"} {
		app := strings.TrimPrefix(owner, "usr_") + "-work"
		p.CreateApp(owner, app)
		p.ApplyChange(ctx, app, shopWithCheckout())
		p.SetAppKind(app, platform.KindWork)
		if _, err := p.Publish(app, 0); err == nil || !strings.Contains(err.Error(), "subscribe") {
			t.Fatalf("%s without a subscription: %v", app, err)
		}
		platform.PaidWorkApp(t, p, app)
		if _, err := p.Publish(app, 0); err != nil {
			t.Fatal(err)
		}
		connect(app)
		do(t, p, "POST", app+".renoxium.test", "/buy", url.Values{})
		if fee := fs.sessions[len(fs.sessions)-1].Get("payment_intent_data[application_fee_amount]"); fee != "" {
			t.Fatalf("%s fee = %q", app, fee)
		}
		if got := p.StaffLimit(app); got != platform.WorkAppUsers {
			t.Fatalf("%s staff = %d", app, got)
		}
		if !p.CustomDomainAllowed(app) {
			t.Fatalf("%s may not have its own domain", app)
		}
		p.SetWorkSubscription(app, "sub_"+app, "canceled", time.Time{})
		if r := do(t, p, "GET", app+".renoxium.test", "/", nil); r.status != 402 || !strings.Contains(r.body, "paused") {
			t.Fatalf("lapsed %s: %d %s", app, r.status, r.body)
		}
	}
	// A granted account's work app needs no subscription.
	p.CreateApp("usr_granted", "granted-work")
	p.ApplyChange(ctx, "granted-work", shopWithCheckout())
	p.SetAppKind("granted-work", platform.KindWork)
	if _, err := p.Publish("granted-work", 0); err != nil {
		t.Fatalf("granted work app: %v", err)
	}
	if r := do(t, p, "GET", "granted-work.renoxium.test", "/", nil); r.status == 402 {
		t.Fatal("granted work app paused")
	}
}

// A free account may have 3 apps, a member 50. Apps beyond the limit keep
// running when the card goes; only new ones are refused.
func TestAppLimits(t *testing.T) {
	plans := map[string]string{"usr_a": "free"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", SiteURL: "https://www.renoxium.test",
		OwnerPlan: func(id string) string { return plans[id] }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if platform.AppsPerPlan["free"] != 3 || platform.AppsPerPlan["member"] != 50 || platform.AppsPerPlan["granted"] != 50 {
		t.Fatalf("limits %v", platform.AppsPerPlan)
	}
	for i := 1; i <= 3; i++ {
		if _, err := p.CreateApp("usr_a", fmt.Sprintf("app-%d", i)); err != nil {
			t.Fatalf("free app %d: %v", i, err)
		}
	}
	freeMsg := "A free account may have 3 apps. Save a card at https://www.renoxium.test/billing to create up to 50 (saving a card charges nothing), " +
		"or delete an app you no longer need at https://www.renoxium.test/apps/<app>/delete."
	if _, err := p.CreateApp("usr_a", "app-4"); err == nil || err.Error() != freeMsg {
		t.Fatalf("app over the free limit: %v", err)
	}
	// A card saved: up to 50.
	plans["usr_a"] = "member"
	platform.ForgetPlan("usr_a")
	for i := 4; i <= 50; i++ {
		if _, err := p.CreateApp("usr_a", fmt.Sprintf("app-%d", i)); err != nil {
			t.Fatalf("member app %d: %v", i, err)
		}
	}
	memberMsg := "this account has the maximum of 50 apps; delete one you no longer need, or contact Renoxium at https://www.renoxium.test/support"
	if _, err := p.CreateApp("usr_a", "app-51"); err == nil || err.Error() != memberMsg {
		t.Fatalf("app over the member limit: %v", err)
	}
	// The card is removed: all 50 apps stay, a new one is refused.
	p.ApplyChange(context.Background(), "app-12", &appdef.App{Templates: map[string]string{"t": "<h1>Twelve</h1>"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}})
	if _, err := p.Publish("app-12", 0); err != nil {
		t.Fatal(err)
	}
	plans["usr_a"] = "free"
	platform.ForgetPlan("usr_a")
	if apps, _ := p.Apps("usr_a"); len(apps) != 50 {
		t.Fatalf("%d apps after the card went", len(apps))
	}
	if r := do(t, p, "GET", "app-12.renoxium.test", "/", nil); r.status != 200 || !strings.Contains(r.body, "Twelve") {
		t.Fatalf("app over the free limit stopped: %d", r.status)
	}
	if _, err := p.CreateApp("usr_a", "app-51"); err == nil || err.Error() != freeMsg {
		t.Fatalf("new app after the card went: %v", err)
	}
	// Without plans nothing but the hard cap limits.
	open, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	for i := 1; i <= 4; i++ {
		if _, err := open.CreateApp("usr_b", fmt.Sprintf("open-%d", i)); err != nil {
			t.Fatalf("no plans, app %d: %v", i, err)
		}
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
	p.SetAppKind("club", platform.KindShop) // a shop without Stripe keeps the free limits
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
	plans := map[string]string{"usr_free": "free", "usr_member": "member", "usr_granted": "granted"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", SiteURL: "https://www.renoxium.test",
		OwnerPlan: func(id string) string { return plans[id] }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.CreateApp("usr_free", "free-shop")
	// A free account's app gets staff once it is a shop (2) or a paid work app (5).
	if err := p.AddStaff("free-shop", "a@x.ae", "usr_free"); err == nil || !strings.Contains(err.Error(), "shop or a work app") {
		t.Fatalf("free staff: %v", err)
	}
	platform.ReadyShop(t, p, "free-shop")
	if got := p.StaffLimit("free-shop"); got != platform.ShopStaff {
		t.Fatalf("shop staff limit = %d", got)
	}
	for _, e := range []string{"a@x.ae", "b@x.ae"} {
		if err := p.AddStaff("free-shop", e, "usr_free"); err != nil {
			t.Fatal(err)
		}
	}
	// A full shop is pointed to its Plan page: a work app has more staff.
	if err := p.AddStaff("free-shop", "c@x.ae", "usr_free"); err == nil || !strings.Contains(err.Error(), "https://www.renoxium.test/apps/free-shop/plan") {
		t.Fatalf("third shop staff: %v", err)
	}
	// A member's apps get staff the same way; only a grant gives 10 to any app.
	p.CreateApp("usr_member", "member-shop")
	if err := p.AddStaff("member-shop", "a@x.ae", "usr_member"); err == nil || !strings.Contains(err.Error(), "shop or a work app") {
		t.Fatalf("member staff: %v", err)
	}
	p.CreateApp("usr_granted", "granted-app")
	if got := p.StaffLimit("granted-app"); got != platform.GrantedStaff {
		t.Fatalf("granted staff limit = %d", got)
	}
	for i := 0; i < platform.GrantedStaff; i++ {
		if err := p.AddStaff("granted-app", fmt.Sprintf("s%d@x.ae", i), "usr_granted"); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.AddStaff("granted-app", "more@x.ae", "usr_granted"); err == nil || !strings.Contains(err.Error(), "may have 10 staff members") ||
		!strings.Contains(err.Error(), "https://www.renoxium.test/support") {
		t.Fatalf("eleventh granted staff: %v", err)
	}
	if err := p.AddStaff("granted-app", "S1@X.ae", "usr_granted"); err != nil || !p.IsStaff("granted-app", "s1@x.ae") {
		t.Fatalf("existing staff again: %v", err)
	}
	if err := p.AddStaff("granted-app", "not an email", "usr_granted"); err == nil {
		t.Fatal("bad email accepted")
	}
}

func TestStorageQuotaPerAccount(t *testing.T) {
	plans := map[string]string{"usr_free": "free"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", SiteURL: "https://www.renoxium.test",
		OwnerPlan: func(id string) string { return plans[id] }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	old := platform.StorageMBPerPlan["free"]
	platform.StorageMBPerPlan["free"] = 0 // every new byte is over quota
	defer func() { platform.StorageMBPerPlan["free"] = old }()
	p.CreateApp("usr_free", "shop-a")
	if _, err := p.AddFile("shop-a", bytes.NewReader(pngFile(t, 1))); err == nil || !strings.Contains(err.Error(), "storage is full") ||
		!strings.Contains(err.Error(), "https://www.renoxium.test/apps/shop-a/plan") {
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
