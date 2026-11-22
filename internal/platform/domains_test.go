package platform_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/cloudflare"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// fakeCloudflare keeps custom hostnames; active decides what Get reports.
type fakeCloudflare struct {
	mu      sync.Mutex
	active  bool
	created int
	deleted []string
}

func (f *fakeCloudflare) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer cf-token" || !strings.HasPrefix(r.URL.Path, "/zones/zone1/custom_hostnames") {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`))
		return
	}
	host := "www.oud.ae"
	if r.Method == http.MethodPost {
		var body struct{ Hostname string }
		json.NewDecoder(r.Body).Decode(&body)
		host = body.Hostname
		f.created++
	}
	if r.Method == http.MethodDelete {
		f.deleted = append(f.deleted, strings.TrimPrefix(r.URL.Path, "/zones/zone1/custom_hostnames/"))
		w.Write([]byte(`{"success":true,"result":{"id":"ch1"}}`))
		return
	}
	status, ssl := "pending", "pending_validation"
	if f.active {
		status, ssl = "active", "active"
	}
	json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{
		"id": "ch1", "hostname": host, "status": status,
		"verification_errors":    []string{"custom hostname does not CNAME to this zone."},
		"ownership_verification": map[string]string{"type": "txt", "name": "_cf-custom-hostname." + host, "value": "abc"},
		"ssl":                    map[string]any{"status": ssl, "validation_records": []map[string]string{{"txt_name": "_acme-challenge." + host, "txt_value": "xyz"}}},
	}})
}

func TestCustomDomainLifecycle(t *testing.T) {
	cf := &fakeCloudflare{}
	srv := httptest.NewServer(cf)
	defer srv.Close()
	plans := map[string]string{"usr_free": "free", "usr_pro": "pro", "usr_other": "pro"}
	dns := map[string][]string{} // the TXT records "in DNS"
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", SiteURL: "https://renoxium.com",
		OwnerPlan:  func(id string) string { return plans[id] },
		LookupTXT:  func(_ context.Context, name string) ([]string, error) { return dns[name], nil },
		Cloudflare: &cloudflare.Client{Token: "cf-token", ZoneID: "zone1", BaseURL: srv.URL}, CustomDomainTarget: "customers.renoxium.test"},
		sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	page := &appdef.App{Templates: map[string]string{"t": "<!doctype html><html><head></head><body><h1>Oud shop</h1></body></html>"},
		Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}}
	for _, app := range []string{"free-shop", "oud"} {
		p.CreateApp("usr_"+map[string]string{"free-shop": "free", "oud": "pro"}[app], app)
		p.ApplyChange(ctx, app, page)
		p.Publish(app, 0)
	}

	// A static site on the free plan keeps its Renoxium address; a shop or a
	// work app may have its own domain.
	if _, err := p.SetCustomDomain(ctx, "free-shop", "www.free.ae"); err == nil || !strings.Contains(err.Error(), "shop or a work app") {
		t.Errorf("free static site: err = %v", err)
	}
	platform.ReadyShop(t, p, "free-shop")
	if d, err := p.SetCustomDomain(ctx, "free-shop", "www.free.ae"); err != nil || d.Host != "www.free.ae" {
		t.Errorf("free shop: %v %v", d, err)
	}
	if err := p.RemoveCustomDomain(ctx, "free-shop"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"not a domain", "shop.renoxium.test", "renoxium.com"} {
		if _, err := p.SetCustomDomain(ctx, "oud", bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}

	// Adding a domain first asks for proof that it belongs to the account:
	// nothing reaches Cloudflare before the proof record is in DNS.
	d, err := p.SetCustomDomain(ctx, "oud", " https://WWW.Oud.ae/ ")
	if err != nil {
		t.Fatal(err)
	}
	if d.Host != "www.oud.ae" || d.Status != "verify" || d.Verify.Name != "_renoxium.www.oud.ae" || !strings.HasPrefix(d.Verify.Value, "renoxium-verify=") || cf.created != 0 {
		t.Fatalf("unproven domain = %+v, %d created at Cloudflare", d, cf.created)
	}
	// Another account claims the same domain, as someone would to take over
	// a domain whose DNS already points here. Its proof record is different,
	// so the first owner's record does not help it, and its claim does not
	// keep the real owner out.
	p.CreateApp("usr_other", "squatter")
	p.ApplyChange(ctx, "squatter", page)
	sq, err := p.SetCustomDomain(ctx, "squatter", "www.oud.ae")
	if err != nil || sq.Status != "verify" || sq.Verify.Value == d.Verify.Value {
		t.Fatalf("second claim = %+v, %v", sq, err)
	}
	dns[d.Verify.Name] = []string{"unrelated", d.Verify.Value}
	if sq, _ = p.RefreshCustomDomain(ctx, "squatter"); sq.Status != "verify" || cf.created != 0 {
		t.Fatalf("another account's proof was accepted: %+v", sq)
	}
	if d, err = p.SetCustomDomain(ctx, "oud", "www.oud.ae"); err != nil {
		t.Fatal(err)
	}
	if sq, _ := p.CustomDomain("squatter"); sq != nil {
		t.Fatalf("the unproven claim was kept: %+v", sq)
	}
	if d.Host != "www.oud.ae" || d.Status != "pending" || d.CNAME != "customers.renoxium.test" || d.Ownership.Value != "abc" || len(d.SSLRecords) != 1 || len(d.Problems) == 0 {
		t.Errorf("pending domain = %+v", d)
	}
	if _, err := p.SetCustomDomain(ctx, "oud", "shop.oud.ae"); err == nil {
		t.Error("a second domain was accepted")
	}
	if got := do(t, p, "GET", "www.oud.ae", "/", nil); got.status != -1 {
		t.Errorf("pending domain served: %d", got.status)
	}
	if u := p.URL("oud", platform.Live); u != "https://oud.renoxium.test" {
		t.Errorf("URL while pending = %s", u)
	}

	cf.mu.Lock()
	cf.active = true
	cf.mu.Unlock()
	if d, err = p.RefreshCustomDomain(ctx, "oud"); err != nil || d.Status != "active" {
		t.Fatalf("refresh = %+v, %v", d, err)
	}
	if got := do(t, p, "GET", "www.oud.ae", "/", nil); got.status != 200 || !strings.Contains(got.body, "Oud shop") {
		t.Errorf("active domain: %d %s", got.status, got.body)
	}
	if u := p.URL("oud", platform.Live); u != "https://www.oud.ae" {
		t.Errorf("URL when active = %s", u)
	}

	if err := p.RemoveCustomDomain(ctx, "oud"); err != nil {
		t.Fatal(err)
	}
	if len(cf.deleted) != 1 || cf.deleted[0] != "ch1" {
		t.Errorf("cloudflare deletes = %v", cf.deleted)
	}
	if got := do(t, p, "GET", "www.oud.ae", "/", nil); got.status != -1 {
		t.Errorf("removed domain still served: %d", got.status)
	}
	if u := p.URL("oud", platform.Live); u != "https://oud.renoxium.test" {
		t.Errorf("URL after removal = %s", u)
	}
	if d, _ := p.CustomDomain("oud"); d != nil {
		t.Errorf("domain after removal = %+v", d)
	}
}
