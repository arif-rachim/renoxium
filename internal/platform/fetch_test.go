package platform_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// fakeAPI answers for every host; the transport sends every host to it.
func fakeAPI(t *testing.T) (*httptest.Server, http.RoundTripper) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rates":
			if r.Header.Get("Authorization") != "Bearer sk_courier_123456" {
				w.WriteHeader(401)
				w.Write([]byte(`{"error":"no key"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"price":25,"currency":"AED","emirate":"` + r.URL.Query().Get("emirate") + `"}`))
		case "/echo":
			w.Write([]byte("you sent " + r.Header.Get("Authorization") + " and " + r.URL.Query().Get("api_key")))
		case "/slow":
			time.Sleep(2500 * time.Millisecond)
			w.Write([]byte("finally"))
		case "/moved":
			http.Redirect(w, r, "https://evil.example/steal", http.StatusFound)
		default:
			w.Write([]byte(r.Method + " " + r.Host + r.URL.Path))
		}
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}}
	return srv, tr
}

func fetchApp(secretHost string) *appdef.App {
	return &appdef.App{
		Scripts: map[string]string{"call": `function handle(req) {
  const a = req.body || {};
  try { return {json: http.fetch(a)}; } catch (e) { return {json: {error: String(e)}}; }
}`, "many": `function handle(req) {
  const out = [];
  for (let i = 0; i < 7; i++) { try { http.fetch({url: 'https://api.courier.ae/x'}); out.push('ok'); } catch (e) { out.push(String(e)); } }
  return {json: out};
}`},
		Templates: map[string]string{"t": "x"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}, {Method: "POST", Path: "/call", Script: "call"}, {Method: "POST", Path: "/many", Script: "many"}},
		Hosts:     []appdef.Host{{Host: "api.courier.ae", Purpose: "Delivery prices"}, {Host: "sms.example.com", Purpose: "Order texts"}},
		Secrets: []appdef.Secret{{Name: "COURIER_KEY", Host: secretHost, Header: "Authorization", Prefix: "Bearer", Help: "Courier portal"},
			{Name: "SMS_KEY", Host: "sms.example.com", Query: "api_key"}},
	}
}

func TestHTTPFetch(t *testing.T) {
	_, tr := fakeAPI(t)
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", SiteURL: "https://renoxium.test",
		FetchTransport: tr}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	p.CreateApp("usr_test", "shop")
	res, err := p.ApplyChange(ctx, "shop", fetchApp("api.courier.ae"))
	if err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if got := strings.Join(res.OwnerActions, "; "); !strings.Contains(got, "allow api.courier.ae at https://renoxium.test/apps/shop/connections") ||
		!strings.Contains(got, "enter COURIER_KEY") {
		t.Fatalf("owner actions: %s", got)
	}
	call := func(args map[string]any) map[string]any {
		t.Helper()
		r, err := p.TestRequest(ctx, "shop", platform.TestRequestInput{Method: "POST", Path: "/call", JSON: args})
		if err != nil || r.Status != 200 {
			t.Fatalf("call: %v %+v", err, r)
		}
		var out map[string]any
		json.Unmarshal([]byte(r.Body), &out)
		return out
	}
	errOf := func(args map[string]any) string { s, _ := call(args)["error"].(string); return s }

	if e := errOf(map[string]any{"url": "https://evil.example/x"}); !strings.Contains(e, "evil.example is not in the app's hosts") {
		t.Fatalf("unlisted host: %s", e)
	}
	for _, u := range []string{"http://api.courier.ae/x", "https://api.courier.ae:8443/x", "https://user:pw@api.courier.ae/x"} {
		if e := errOf(map[string]any{"url": u}); !strings.Contains(e, "url must be https://host/path on port 443") {
			t.Fatalf("%s: %s", u, e)
		}
	}
	if e := errOf(map[string]any{"url": "https://api.courier.ae/x"}); !strings.Contains(e, "the owner has not allowed api.courier.ae yet") {
		t.Fatalf("not allowed: %s", e)
	}
	if err := p.AllowHost("shop", "evil.example", "o@x"); err == nil {
		t.Fatal("allowed a host the app does not list")
	}
	p.AllowHost("shop", "api.courier.ae", "owner@shop.ae")
	p.AllowHost("shop", "sms.example.com", "owner@shop.ae")
	// Preview holds a copy of live data and the agent can change it unasked,
	// so it calls out only while the owner has turned test calls on.
	if e := errOf(map[string]any{"url": "https://api.courier.ae/hello"}); !strings.Contains(e, "until the owner turns on test calls") {
		t.Fatalf("preview call without test calls on: %s", e)
	}
	if err := p.SetPreviewFetch("shop", true); err != nil || !p.PreviewFetchOn("shop") {
		t.Fatal("test calls not turned on", err)
	}
	if out := call(map[string]any{"url": "https://api.courier.ae/hello", "method": "post", "body": map[string]any{"a": 1}}); out["body"] != "POST api.courier.ae/hello" || out["status"] != 200.0 {
		t.Fatalf("plain call: %v", out)
	}

	// Keys: entered by the owner, added by the platform, never shown to the script.
	if e := errOf(map[string]any{"url": "https://api.courier.ae/rates", "secret": "COURIER_KEY"}); !strings.Contains(e, "has not entered COURIER_KEY yet") {
		t.Fatalf("no key yet: %s", e)
	}
	if err := p.SetSecret("shop", "COURIER_KEY", "sk_courier_123456"); err != nil {
		t.Fatal(err)
	}
	out := call(map[string]any{"url": "https://api.courier.ae/rates?emirate=Dubai", "secret": "COURIER_KEY"})
	if j, _ := out["json"].(map[string]any); j["price"] != 25.0 || j["emirate"] != "Dubai" || out["ok"] != true {
		t.Fatalf("rates: %v", out)
	}
	if out := call(map[string]any{"url": "https://api.courier.ae/echo", "secret": "COURIER_KEY"}); strings.Contains(out["body"].(string), "sk_courier") {
		t.Fatalf("key echoed to the script: %v", out["body"])
	}
	if e := errOf(map[string]any{"url": "https://sms.example.com/echo", "secret": "COURIER_KEY"}); !strings.Contains(e, "only sent to api.courier.ae") {
		t.Fatalf("key to another host: %s", e)
	}
	p.SetSecret("shop", "SMS_KEY", "sms-secret-777")
	if out := call(map[string]any{"url": "https://sms.example.com/echo", "secret": "SMS_KEY"}); out["body"] != "you sent  and [secret]" {
		t.Fatalf("query key: %v", out["body"])
	}
	_, secrets, _ := p.Connections("shop")
	if secrets[0].Hint != "3456" || !secrets[0].Set {
		t.Fatalf("secret state: %+v", secrets[0])
	}

	// Redirects come back to the script, so a key never follows them.
	if out := call(map[string]any{"url": "https://api.courier.ae/moved"}); out["status"] != 302.0 {
		t.Fatalf("redirect: %v", out)
	}
	// Waiting on the network does not count towards the 2-second script limit.
	if out := call(map[string]any{"url": "https://api.courier.ae/slow", "timeout_ms": 4000}); out["body"] != "finally" {
		t.Fatalf("slow call: %v", out)
	}
	if e := errOf(map[string]any{"url": "https://api.courier.ae/slow", "timeout_ms": 500}); !strings.Contains(e, "did not answer within 500ms") {
		t.Fatalf("timeout: %s", e)
	}
	r, _ := p.TestRequest(ctx, "shop", platform.TestRequestInput{Method: "POST", Path: "/many"})
	if !strings.HasPrefix(r.Body, `["ok","ok","ok","ok","ok","`) || !strings.Contains(r.Body, "http.fetch: at most 5 calls per request or job") {
		t.Fatalf("per-run limit: %s", r.Body)
	}

	// The agent moves the key to another host: it is not sent until the owner enters it again.
	if res, _ := p.ApplyChange(ctx, "shop", fetchApp("sms.example.com")); !res.OK {
		t.Fatal(res)
	}
	if e := errOf(map[string]any{"url": "https://sms.example.com/x", "secret": "COURIER_KEY"}); !strings.Contains(e, "was entered for api.courier.ae") {
		t.Fatalf("moved key: %s", e)
	}
	logs, _ := p.Logs("shop", logFilterAll())
	for _, e := range logs {
		if strings.Contains(e.Message, "sk_courier_123456") || strings.Contains(e.Message, "sms-secret-777") {
			t.Fatalf("key in the log: %s", e.Message)
		}
	}
	if !anyLog(logs, "fetch GET https://api.courier.ae/rates -> 200") {
		t.Fatal("call not logged")
	}
}

func TestFetchValidation(t *testing.T) {
	def := fetchApp("api.courier.ae")
	def.Hosts = append(def.Hosts, appdef.Host{Host: "10.0.0.1", Purpose: "x"}, appdef.Host{Host: "nopurpose.com"})
	def.Secrets = append(def.Secrets, appdef.Secret{Name: "lower", Host: "api.courier.ae", Header: "X"}, appdef.Secret{Name: "B", Host: "other.com", Header: "X"},
		appdef.Secret{Name: "C", Host: "api.courier.ae"})
	raw, _ := jsonMarshal(def)
	_, problems := appdef.Parse(raw)
	text := strings.Join(problems, "\n")
	for _, want := range []string{`host "10.0.0.1"`, `host "nopurpose.com": purpose`, `secret "lower"`, `secret "B": host "other.com"`, `secret "C": set either`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}

// A Free account's app may not call a payment service: on Free, card
// payments go through payments.checkout, which carries Renoxium's fee.
func TestFreePlanCannotCallPaymentServices(t *testing.T) {
	plans := map[string]string{"usr_free": "free", "usr_pro": "pro"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", SiteURL: "https://renoxium.test",
		OwnerPlan: func(id string) string { return plans[id] }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	def := fetchApp("api.courier.ae")
	def.Hosts = append(def.Hosts, appdef.Host{Host: "api-v2.ziina.com", Purpose: "Card payments"})
	for owner, app := range map[string]string{"usr_free": "free-shop", "usr_pro": "pro-shop"} {
		p.CreateApp(owner, app)
		if res, err := p.ApplyChange(ctx, app, def); err != nil || !res.OK {
			t.Fatal(err, res)
		}
	}
	if err := p.AllowHost("free-shop", "api-v2.ziina.com", "o@x"); err == nil || !strings.Contains(err.Error(), "is a payment service") ||
		!strings.Contains(err.Error(), "https://renoxium.test/billing") {
		t.Fatalf("free account allowed a payment host: %v", err)
	}
	if err := p.AllowHost("free-shop", "api.courier.ae", "o@x"); err != nil {
		t.Fatalf("free account, other host: %v", err)
	}
	if err := p.AllowHost("pro-shop", "api-v2.ziina.com", "o@x"); err != nil {
		t.Fatalf("paid account: %v", err)
	}
	r, err := p.TestRequest(ctx, "free-shop", platform.TestRequestInput{Method: "POST", Path: "/call", JSON: map[string]any{"url": "https://api-v2.ziina.com/api/payment_intent"}})
	if err != nil || !strings.Contains(r.Body, "is a payment service") {
		t.Fatalf("free account called a payment host: %v %+v", err, r)
	}
	for host, want := range map[string]bool{"ziina.com": true, "API.Stripe.com": true, "notziina.com": false, "api.courier.ae": false} {
		if platform.PaymentHost(host) != want {
			t.Fatalf("PaymentHost(%q) != %v", host, want)
		}
	}
}
