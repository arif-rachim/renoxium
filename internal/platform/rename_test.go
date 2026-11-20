package platform_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

// A renamed app keeps its data, versions, files, staff and live site under
// the new name; the old addresses redirect to the same path; the old name
// stays out of others' reach; Stripe's old metadata still finds the app.
func TestRenameApp(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	if _, res, err := p.CreateFromExample(ctx, "usr_a", "little-reader", "catalog"); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if _, err := p.Publish("little-reader", 0); err != nil {
		t.Fatal(err)
	}
	f, err := p.AddFile("little-reader", bytes.NewReader(pngFile(t, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.AddStaff("little-reader", "sam@shop.ae", "usr_a"); err != nil {
		t.Fatal(err)
	}
	before := do(t, p, "POST", "little-reader.renoxium.test", "/orders", url.Values{"product_id": {"1"}, "customer_name": {"Ali"}, "phone": {"0501234567"}})
	if before.status != 200 {
		t.Fatalf("order before: %d %s", before.status, before.body)
	}

	// Names that may not be used.
	p.CreateApp("usr_b", "taken-name")
	for name, want := range map[string]string{"little-reader": "already the app's name", "Bad Name": "not allowed", "taken-name": "already taken", "demo-shop": "reserved"} {
		if err := p.CheckNewName("little-reader", name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", name, err)
		}
	}

	info, err := p.RenameApp(ctx, "little-reader", "noor-books")
	if err != nil || info.App != "noor-books" || info.LiveVersion != 1 || !strings.Contains(info.LiveURL, "noor-books.renoxium.test") {
		t.Fatalf("rename: %v %+v", err, info)
	}
	if _, err := p.Info("little-reader"); err == nil {
		t.Fatal("old name still an app")
	}
	if err := p.CheckOwner("usr_a", "noor-books"); err != nil {
		t.Fatal("owner lost", err)
	}
	// Data, versions, files and staff came along.
	if def, n, err := p.Definition("noor-books", 0); err != nil || n != 1 || def == nil {
		t.Fatalf("versions: %v %d", err, n)
	}
	live := do(t, p, "GET", "noor-books.renoxium.test", "/", nil)
	if live.status != 200 {
		t.Fatalf("new address: %d", live.status)
	}
	if r := do(t, p, "GET", "noor-books.renoxium.test", f.URL, nil); r.status != 200 {
		t.Fatalf("file on new address: %d", r.status)
	}
	if staff, _ := p.Staff("noor-books"); len(staff) != 1 {
		t.Fatalf("staff: %v", staff)
	}
	if _, rows, _ := p.AdminQuery(ctx, "noor-books", "orders", platform.ListQuery{PageSize: 25}); rows != 1 {
		t.Fatalf("orders on live after rename: %d", rows)
	}

	// Old addresses: a permanent redirect to the same path, for live, preview and sign-in.
	for host, want := range map[string]string{
		"little-reader.renoxium.test":              "https://noor-books.renoxium.test/products/2?x=1",
		"little-reader--preview.renoxium.test":     "https://noor-books--preview.renoxium.test/products/2?x=1",
		"little-reader--id.renoxium.test":          "https://noor-books--id.renoxium.test/products/2?x=1",
		"little-reader--preview--id.renoxium.test": "https://noor-books--preview--id.renoxium.test/products/2?x=1",
	} {
		r := do(t, p, "GET", host, "/products/2?x=1", nil)
		if r.status != 301 || r.header.Get("Location") != want {
			t.Errorf("%s: %d %s", host, r.status, r.header.Get("Location"))
		}
	}

	// Others cannot take the old name while it redirects; its owner can, which ends the redirect.
	if _, err := p.CreateApp("usr_b", "little-reader"); err == nil {
		t.Fatal("someone else took the old name")
	}
	// Stripe metadata written before the rename still finds the app.
	if got := platform.CurrentNameForTest(p, "little-reader"); got != "noor-books" {
		t.Fatalf("old name resolves to %q", got)
	}
	// Not again within 30 days.
	if err := p.CheckNewName("noor-books", "noor-books-two"); err == nil || !strings.Contains(err.Error(), "renamed recently") {
		t.Fatalf("second rename: %v", err)
	}
	if _, err := p.CreateApp("usr_a", "little-reader"); err != nil {
		t.Fatalf("owner taking the old name back: %v", err)
	}
	if r := do(t, p, "GET", "little-reader.renoxium.test", "/", nil); r.status == 301 {
		t.Fatal("the old name still redirects after its owner took it back")
	}
}

// A payment started before a rename is completed by Stripe's webhook,
// which still carries the old name in its metadata.
func TestRenameKeepsStripePayments(t *testing.T) {
	fs := &fakeStripe{charges: true}
	srv := fs.server(t)
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: srv.URL}}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	p.CreateApp("usr_test", "oud")
	p.ApplyChange(ctx, "oud", shopWithCheckout())
	p.Publish("oud", 0)
	p.StripeOnboardingURL(ctx, "oud", "owner@oud.ae", "https://site/return", "https://site/refresh")
	if pa, err := p.RefreshPaymentAccount(ctx, "oud"); err != nil || !pa.ChargesEnabled {
		t.Fatal(pa, err)
	}
	if r := do(t, p, "POST", "oud.renoxium.test", "/buy", url.Values{}); r.status != 303 {
		t.Fatalf("buy: %d", r.status)
	}
	if _, err := p.RenameApp(ctx, "oud", "oud-perfumes"); err != nil {
		t.Fatal(err)
	}
	if pa, err := p.PaymentAccount("oud-perfumes"); err != nil || pa.StripeAccount != "acct_merchant" {
		t.Fatalf("Stripe account after rename: %+v %v", pa, err)
	}
	obj, _ := json.Marshal(map[string]any{"id": "cs_test_1", "payment_status": "paid", "metadata": map[string]string{"rx_app": "oud"}, "payment_intent": "pi_1",
		"customer_details": map[string]string{"email": "buyer@mail.ae", "name": "Buyer"}})
	e := stripe.Event{ID: "evt", Type: "checkout.session.completed", Account: "acct_merchant"}
	e.Data.Object = obj
	if err := p.HandleStripeEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	list, _ := p.Payments(ctx, "oud-perfumes", 10)
	if len(list) != 1 || list[0].Status != "paid" {
		t.Fatalf("payments after rename: %+v", list)
	}
}
