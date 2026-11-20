package platform_test

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// A demo of the members app: sign-up, an email from a script and an owner
// notice, so every kind of demo message is covered.
func demoExample(t *testing.T) examples.Example {
	t.Helper()
	def := membersApp()
	def.Scripts["hello"] = `function handle(req) {
		email.send({to: req.form.to, subject: "Your booking", text: "See you soon. Details: https://example.ae/b/1"});
		notify.owner({subject: "New booking", text: "From " + req.form.to});
		db.exec("INSERT INTO notes (user_id, body) VALUES (0, ?)", req.form.to);
		return {redirect: "/"}
	}`
	def.Routes = append(def.Routes, appdef.Route{Method: "POST", Path: "/book", Script: "hello"})
	def.Seed = map[string][]map[string]any{"notes": {{"user_id": 0, "body": "seeded"}}}
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	return examples.Example{Name: "club", JSON: raw}
}

func newDemoPlatform(t *testing.T) (*platform.Platform, *outbox) {
	t.Helper()
	box := &outbox{}
	// Previews are private on this server: demos must be public anyway.
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", Mailer: box,
		SiteURL: "https://site.test", PreviewKey: []byte("0123456789abcdef0123456789abcdef")}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	box.wait = p.WaitBackground
	return p, box
}

func notes(t *testing.T, p *platform.Platform) int {
	t.Helper()
	_, total, err := p.AdminList(context.Background(), "demo-club", "notes", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func TestDemoIsPublicSharedAndReset(t *testing.T) {
	p, box := newDemoPlatform(t)
	ctx := context.Background()
	ex := demoExample(t)
	if err := p.EnsureDemos(ctx, []examples.Example{ex}); err != nil {
		t.Fatal(err)
	}
	if !p.IsDemo("demo-club") || p.DemoURL("club") != "https://demo-club.renoxium.test" {
		t.Fatalf("demo: %v %s", p.IsDemo("demo-club"), p.DemoURL("club"))
	}
	// Starting again with the same template keeps the demo version and only
	// renews its data.
	info, _ := p.Info("demo-club")
	if err := p.EnsureDemos(ctx, []examples.Example{ex}); err != nil {
		t.Fatal(err)
	}
	if again, _ := p.Info("demo-club"); again.PreviewVersion != info.PreviewVersion || again.LiveVersion != 0 {
		t.Fatalf("unchanged template re-applied: %+v then %+v", info, again)
	}

	// Anyone opens it, with no preview link, at the plain address, with the demo bar.
	v := newVisitor(t, p, "demo-club.renoxium.test")
	w := v.do("GET", "/", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Welcome, guest") || !strings.Contains(w.Body.String(), `id="rx-demo"`) ||
		!strings.Contains(w.Body.String(), "https://site.test/templates/club/admin") {
		t.Fatalf("demo home: %d %s", w.Code, w.Body.String())
	}
	if w := v.do("GET", "/", nil); w.Code != 200 {
		t.Fatalf("again: %d", w.Code)
	}

	// Visitors use it like customers; everything stays inside the platform.
	if w := v.do("POST", "/book", url.Values{"to": {"guest@example.ae"}}); w.Code != 303 {
		t.Fatalf("book: %d %s", w.Code, w.Body.String())
	}
	if w := v.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"email": {"guest@example.ae"}, "password": {"demo password 1"}}); w.Code != 303 {
		t.Fatalf("signup: %d %s", w.Code, w.Body.String())
	}
	if w := v.do("GET", "/account", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "guest@example.ae") {
		t.Fatalf("account: %d", w.Code)
	}
	if len(box.sent) != 0 {
		t.Fatalf("a demo sent %d real emails", len(box.sent))
	}
	if n := notes(t, p); n != 2 {
		t.Fatalf("notes after booking: %d", n)
	}

	// The reset puts the seed rows back and signs everyone out.
	if err := p.ResetDemos(ctx); err != nil {
		t.Fatal(err)
	}
	if n := notes(t, p); n != 1 {
		t.Fatalf("notes after reset: %d", n)
	}
	if w := v.do("GET", "/account", nil); w.Code != 303 {
		t.Fatalf("still signed in after the reset: %d", w.Code)
	}
	if w := v.do("GET", platform.DemoInboxPath, nil); strings.Contains(w.Body.String(), "Your booking") {
		t.Fatal("inbox survived the reset")
	}

	// A changed template is applied at the next start.
	def, _ := appdef.Parse(ex.JSON)
	def.Templates["home"] = strings.Replace(def.Templates["home"], "Welcome, guest", "Hello, guest", 1)
	ex.JSON, _ = json.Marshal(def)
	if err := p.EnsureDemos(ctx, []examples.Example{ex}); err != nil {
		t.Fatal(err)
	}
	if w := v.do("GET", "/", nil); !strings.Contains(w.Body.String(), "Hello, guest") {
		t.Fatalf("changed template not applied: %s", w.Body.String())
	}

	// Nobody owns a demo: it is no account's app and its name is reserved.
	if err := p.CheckOwner("usr_test", "demo-club"); err == nil {
		t.Fatal("an account owns the demo")
	}
	if _, err := p.CreateApp("usr_test", "demo-salon"); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("demo- name for an account: %v", err)
	}
	// A real app's preview is still private.
	p.CreateApp("usr_test", "club")
	p.ApplyChange(ctx, "club", membersApp())
	if w := newVisitor(t, p, "club--preview.renoxium.test").do("GET", "/", nil); w.Code != 403 {
		t.Fatalf("an ordinary preview without access: %d", w.Code)
	}
}

var (
	codeRE     = regexp.MustCompile(`sign-in code is (\d{6})`)
	htmlLinkRE = regexp.MustCompile(`https://[^\s"<]+token=[^\s"<&]+`)
)

func TestDemoInboxBelongsToOneBrowser(t *testing.T) {
	p, _ := newDemoPlatform(t)
	if err := p.EnsureDemos(context.Background(), []examples.Example{demoExample(t)}); err != nil {
		t.Fatal(err)
	}
	alice := newVisitor(t, p, "demo-club.renoxium.test")
	bob := newVisitor(t, p, "demo-club.renoxium.test")
	alice.do("GET", "/", nil)
	bob.do("GET", "/", nil)

	alice.do("POST", "/book", url.Values{"to": {"alice@example.ae"}})
	inbox := alice.do("GET", platform.DemoInboxPath, nil).Body.String()
	for _, want := range []string{"Your booking", "alice@example.ae", "New booking", "the business owner", `<a href="https://example.ae/b/1">`} {
		if !strings.Contains(inbox, want) {
			t.Fatalf("alice's inbox lacks %q:\n%s", want, inbox)
		}
	}
	if strings.Contains(bob.do("GET", platform.DemoInboxPath, nil).Body.String(), "alice@example.ae") {
		t.Fatal("bob sees alice's messages")
	}

	// Sign-up happens on the sign-in host; its confirmation link lands in
	// the same inbox, and opening it confirms the address.
	if w := alice.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"email": {"alice@example.ae"}, "password": {"demo password 1"}}); w.Code != 303 {
		t.Fatalf("signup: %d", w.Code)
	}
	inbox = alice.do("GET", platform.DemoInboxPath, nil).Body.String()
	link := htmlLinkRE.FindString(inbox)
	if !strings.Contains(inbox, "Confirm your email") || !strings.HasPrefix(link, "https://demo-club--id.renoxium.test/_rx/auth/verify?token=") {
		t.Fatalf("no confirmation link in the inbox:\n%s", inbox)
	}
	if w := alice.do("GET", link, nil); w.Code >= 400 {
		t.Fatalf("confirmation link %s: %d %s", link, w.Code, w.Body.String())
	}

	// A WhatsApp code shows on the page that asks for it, for that browser only.
	w := bob.submit("/_rx/auth/phone", "/_rx/auth/phone", url.Values{"phone": {"+971501234567"}})
	m := codeRE.FindStringSubmatch(w.Body.String())
	if w.Code != 200 || m == nil {
		t.Fatalf("code page: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(alice.do("GET", platform.DemoInboxPath, nil).Body.String(), m[1]) {
		t.Fatal("alice sees bob's sign-in code")
	}
	w = bob.do("POST", "/_rx/auth/phone/verify", url.Values{"csrf": {csrfField.FindStringSubmatch(w.Body.String())[1]}, "phone": {"+971501234567"}, "code": {m[1]}, "name": {"Bob"}})
	if w.Code != 303 {
		t.Fatalf("verify code: %d %s", w.Code, w.Body.String())
	}
	if w := bob.do("GET", "/account", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "+971501234567") {
		t.Fatalf("bob's account: %d %s", w.Code, w.Body.String())
	}
}

// Every gallery template comes up as a demo whose pages all render.
func TestGalleryTemplatesRunAsDemos(t *testing.T) {
	p, _ := newDemoPlatform(t)
	if err := p.EnsureDemos(context.Background(), examples.Gallery()); err != nil {
		t.Fatal(err)
	}
	for _, e := range examples.Gallery() {
		v := newVisitor(t, p, "demo-"+e.Name+".renoxium.test")
		if w := v.do("GET", "/", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `id="rx-demo"`) {
			t.Errorf("%s: %d", e.Name, w.Code)
		}
	}
}

// A link carrying someone else's inbox key does not make this browser's
// demo messages (sign-in links, codes, the email typed) land in their
// inbox, on the app host or on the sign-in host, and a forged hand-over
// does not move them either.
func TestDemoInboxKeyCannotBeForcedByALink(t *testing.T) {
	p, _ := newDemoPlatform(t)
	if err := p.EnsureDemos(context.Background(), []examples.Example{demoExample(t)}); err != nil {
		t.Fatal(err)
	}
	mallory := newVisitor(t, p, "demo-club.renoxium.test")
	mallory.do("GET", "/", nil)
	key := mallory.cookies["__Host-rx_demo"].Value

	victim := newVisitor(t, p, "demo-club.renoxium.test")
	victim.do("GET", "/?rx_dk="+key, nil)
	if c := victim.cookies["__Host-rx_demo"]; c == nil || c.Value == key {
		t.Fatal("the app host took the inbox key from the link")
	}
	victim.do("POST", "/book?rx_dk="+key, url.Values{"to": {"victim@real.ae"}})
	victim.doAt(victim.idHost(), "GET", "/_rx/auth/signup?rx_dk="+key, nil)
	if c := victim.jar(victim.idHost())["__Host-rx_demo"]; c != nil && c.Value == key {
		t.Fatal("the sign-in host took the inbox key from the link")
	}
	if w := victim.submit("/_rx/auth/signup?rx_dk="+key, "/_rx/auth/signup", url.Values{"email": {"victim@real.ae"}, "password": {"demo password 1"}}); w.Code != 303 {
		t.Fatalf("signup: %d", w.Code)
	}
	// A hand-over with a made-up code and Mallory's key moves nothing.
	victim.do("GET", "/_rx/auth/callback?code=made-up&rx_dk="+key, nil)
	if inbox := mallory.do("GET", platform.DemoInboxPath, nil).Body.String(); strings.Contains(inbox, "victim@real.ae") || strings.Contains(inbox, "Confirm your email") {
		t.Fatalf("the victim's messages reached the link's inbox:\n%s", inbox)
	}
	// The victim has them all: the booking and, after the hand-over, the
	// confirmation sent from the sign-in host.
	inbox := victim.do("GET", platform.DemoInboxPath, nil).Body.String()
	if !strings.Contains(inbox, "Your booking") || !strings.Contains(inbox, "Confirm your email") {
		t.Fatalf("the victim's own inbox:\n%s", inbox)
	}
}
