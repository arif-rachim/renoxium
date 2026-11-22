package platform_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"github.com/arif-rachim/renoxium/internal/stripe"
	"image"
	"image/png"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/licence"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// exportOf exports app from p as the console's download does.
func exportOf(t *testing.T, p *platform.Platform, app string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := p.ExportApp(context.Background(), app, zw); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

// An app exported from a paid account runs on another server: same pages,
// data and file addresses, with card payments on the server's own Stripe key.
func TestImportAppOnOwnServer(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(nil)
	platform.LicenceKeys = []ed25519.PublicKey{pub}
	t.Cleanup(func() { platform.LicenceKeys = nil })
	hosted, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		LicenceKey: key, OwnerPlan: func(owner string) string {
			if owner == "usr_free" {
				return "free"
			}
			return "pro"
		}, OwnerEmail: func(string) (string, bool) { return "owner@shop.ae", true }},
		sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hosted.Close)
	ctx := context.Background()
	if _, res, err := hosted.CreateFromExample(ctx, "usr_owner", "noor-shop", "store-uae"); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	var img bytes.Buffer
	png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 60, 40)))
	photo, err := hosted.AddFile("noor-shop", bytes.NewReader(img.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hosted.Publish("noor-shop", 0); err != nil {
		t.Fatal(err)
	}
	if r, err := hosted.TestRequest(ctx, "noor-shop", platform.TestRequestInput{Path: "/", Stage: "live"}); err != nil || r.Status != 200 {
		t.Fatalf("hosted live home: %v %+v", err, r.Status)
	}
	export := exportOf(t, hosted, "noor-shop")

	own, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "noorperfumes.ae", PublicScheme: "https", SelfHosted: true},
		sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(own.Close)
	app, lic, err := own.ImportApp(ctx, "usr_local", export, "")
	if err != nil || app != "noor-shop" || lic.Plan != "pro" || lic.Email != "owner@shop.ae" {
		t.Fatalf("import: %q %+v %v", app, lic, err)
	}
	if err := own.UseOwnDomain(app, "www.noorperfumes.ae"); err != nil {
		t.Fatal(err)
	}
	get := func(host, path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "https://"+host+path, nil)
		if !own.ServeApp(w, r) {
			t.Fatalf("%s%s is not served", host, path)
		}
		return w
	}
	for _, host := range []string{"noor-shop.noorperfumes.ae", "www.noorperfumes.ae"} {
		if w := get(host, "/"); w.Code != 200 || !strings.Contains(w.Body.String(), "Al Noor Perfumes") {
			t.Errorf("%s: %d", host, w.Code)
		}
	}
	if w := get("www.noorperfumes.ae", photo.URL); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), img.Bytes()) {
		t.Errorf("photo at its old address: %d", w.Code)
	}
	if info, _ := own.Info(app); info.LiveURL != "https://www.noorperfumes.ae" {
		t.Errorf("main address %s", info.LiveURL)
	}
	if pa, err := own.PaymentAccount(app); err != nil || pa.StripeAccount != "direct" || !pa.ChargesEnabled {
		t.Errorf("payments: %+v %v", pa, err)
	}

	// Importing again, or into a server that already has the app, is refused.
	if _, _, err := own.ImportApp(ctx, "usr_local", export, ""); err == nil {
		t.Error("the same app was imported twice")
	}

	// A free account's export carries no licence, and a changed licence is refused.
	if _, res, err := hosted.CreateFromExample(ctx, "usr_free", "free-shop", "store-uae"); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if _, err := hosted.Publish("free-shop", 0); err != nil {
		t.Fatal(err)
	}
	free := exportOf(t, hosted, "free-shop")
	for _, f := range free.File {
		if strings.HasSuffix(f.Name, "/licence.json") {
			t.Fatal("a free account's export has a licence")
		}
	}
	other, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "example.ae", PublicScheme: "https", SelfHosted: true}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	if _, _, err := other.ImportApp(ctx, "usr_local", free, ""); !errors.Is(err, licence.ErrUnsigned) {
		t.Errorf("free export: %v", err)
	}
	platform.LicenceKeys = nil // Renoxium's own keys: the test key is not one of them
	if _, _, err := other.ImportApp(ctx, "usr_local", export, ""); !errors.Is(err, licence.ErrBadSig) {
		t.Errorf("licence signed by another key: %v", err)
	}
}

// On the owner's own server a checkout charges the Stripe key's own account:
// no Stripe-Account header, no platform fee (even on the free plan's
// terms), and events of that account, which carry no account, are applied.
func TestSelfHostedCheckoutChargesOwnAccount(t *testing.T) {
	fs := &fakeStripe{}
	srv := fs.server(t)
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "oud.ae", PublicScheme: "https", SelfHosted: true,
		Stripe: &stripe.Client{Key: "sk_test_owner", BaseURL: srv.URL}, OwnerPlan: func(string) string { return "free" }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	p.CreateApp("usr_local", "oud")
	p.ApplyChange(ctx, "oud", shopWithCheckout())
	p.Publish("oud", 0)
	platform.UseDirectPayments(t, p, "oud")
	host := "oud.oud.ae"

	r := do(t, p, "POST", host, "/buy", url.Values{})
	if r.status != 303 || len(fs.sessions) != 1 {
		t.Fatalf("buy: %d, %d sessions", r.status, len(fs.sessions))
	}
	if fs.headers[0] != "" || fs.sessions[0].Get("payment_intent_data[application_fee_amount]") != "" {
		t.Fatalf("checkout on %q with fee %q", fs.headers[0], fs.sessions[0].Get("payment_intent_data[application_fee_amount]"))
	}
	obj, _ := json.Marshal(map[string]any{"id": "cs_test_1", "payment_status": "paid", "metadata": map[string]string{"rx_app": "oud"}, "payment_intent": "pi_1",
		"customer_details": map[string]string{"email": "buyer@mail.ae"}})
	e := stripe.Event{ID: "evt_1", Type: "checkout.session.completed"} // no account: the key's own
	e.Data.Object = obj
	if err := p.HandleStripeEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	if list, _ := p.Payments(ctx, "oud", 10); len(list) != 1 || list[0].Status != "paid" {
		t.Fatalf("payments: %+v", list)
	}
}
