package platform_test

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/email"

	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// Visitors sign in on <app>--id, a host where nothing of the app runs, and
// are handed over to the app's host with a one-time code that only the
// browser that started the sign-in can use.
func TestSignInHandOver(t *testing.T) {
	p, box := newAuthPlatform(t)
	p.CreateApp("usr_test", "club")
	p.ApplyChange(context.Background(), "club", membersApp())
	p.Publish("club", 0)
	host, id := "club.renoxium.test", "club--id.renoxium.test"

	// The sign-in host serves the sign-in pages and nothing else of the app.
	guest := newVisitor(t, p, host)
	for path, want := range map[string]int{"/account": 404, "/steal": 404, "/_rx/files/x.png": 404, "/_rx/log": 404, "/mcp": 404} {
		if w := guest.doAt(id, "GET", path, nil); w.Code != want {
			t.Errorf("%s on the sign-in host: %d", path, w.Code)
		}
	}
	if w := guest.doAt(id, "GET", "/", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Welcome, guest") {
		t.Errorf("the sign-in host's front page does not lead to the app: %d", w.Code)
	}

	// Mallory signs up and stops at the hand-over link instead of following it.
	mallory := newVisitor(t, p, host)
	page := mallory.do("GET", "/_rx/auth/signup?next=/account", nil)
	form := url.Values{"csrf": {csrfField.FindStringSubmatch(page.Body.String())[1]}, "email": {"mallory@evil.ae"}, "password": {"mallory's password"}, "next": {"/account"}}
	r := httptest.NewRequest("POST", "http://"+id+"/_rx/auth/signup", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range mallory.jar(id) {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	p.ServeApp(rec, r)
	link, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || rec.Code != 303 || link.Host != host || link.Path != "/_rx/auth/callback" || link.Query().Get("code") == "" {
		t.Fatalf("hand-over: %d %s", rec.Code, rec.Header().Get("Location"))
	}

	// Sent to someone else, the link does not sign them in to Mallory's
	// account: their browser did not start this sign-in.
	victim := newVisitor(t, p, host)
	if w := victim.do("GET", link.RequestURI(), nil); victim.cookies["__Host-rx_session"] != nil || w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/_rx/auth/login") {
		t.Fatalf("a hand-over link worked in another browser: %d", w.Code)
	}
	// Nor does a made-up code.
	if victim.do("GET", "/_rx/auth/callback?code=made-up&next=/account", nil); victim.cookies["__Host-rx_session"] != nil {
		t.Fatal("a made-up code signed someone in")
	}
	// In Mallory's own browser it works, once.
	state := mallory.cookies["__Host-rx_auth_state"]
	if w := mallory.do("GET", link.RequestURI(), nil); w.Code != 303 || w.Header().Get("Location") != "/account" || mallory.cookies["__Host-rx_session"] == nil {
		t.Fatalf("the hand-over in the browser that started it: %d", w.Code)
	}
	again := newVisitor(t, p, host)
	again.cookies["__Host-rx_auth_state"] = state
	if again.do("GET", link.RequestURI(), nil); again.cookies["__Host-rx_session"] != nil {
		t.Fatal("a hand-over code worked twice")
	}

	// A reset link opened from an email, in a browser that did not ask for
	// it from the app, ends at the sign-in page, not in a session.
	asker := newVisitor(t, p, host)
	asker.submit("/_rx/auth/forgot", "/_rx/auth/forgot", url.Values{"email": {"mallory@evil.ae"}})
	resetLink := tokenLink.FindString(box.last(t, "visitor_reset").Text)
	if !strings.HasPrefix(resetLink, "https://"+id+"/_rx/auth/reset?token=") {
		t.Fatalf("reset link: %s", resetLink)
	}
	tok, _ := url.QueryUnescape(strings.SplitN(resetLink, "token=", 2)[1])
	opener := newVisitor(t, p, host)
	if w := opener.submit(resetLink, "/_rx/auth/reset", url.Values{"token": {tok}, "password": {"a brand new password"}}); w.Code != 200 ||
		!strings.Contains(w.Body.String(), "<h1>Sign in</h1>") || opener.cookies["__Host-rx_session"] != nil {
		t.Fatalf("after a reset from an emailed link: %d", w.Code)
	}
	if w := opener.submit("/_rx/auth/login", "/_rx/auth/login", url.Values{"email": {"mallory@evil.ae"}, "password": {"a brand new password"}}); w.Code != 303 {
		t.Fatalf("sign-in with the new password: %d", w.Code)
	}
}

// The preview's sign-in host is as private as the preview: it holds a copy
// of the live visitor accounts.
func TestPreviewSignInHostIsPrivate(t *testing.T) {
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		SiteURL: "https://site.test", PreviewKey: []byte("0123456789abcdef0123456789abcdef")}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.CreateApp("usr_test", "club")
	p.ApplyChange(context.Background(), "club", membersApp())
	host, id := "club--preview.renoxium.test", "club--preview--id.renoxium.test"

	stranger := newVisitor(t, p, host)
	if w := stranger.doAt(id, "GET", "/_rx/auth/login", nil); w.Code != 403 {
		t.Fatalf("the preview's sign-in host without access: %d", w.Code)
	}
	if w := stranger.doAt(id, "POST", "/_rx/auth/login", url.Values{"email": {"a@b.ae"}, "password": {"whatever it is"}}); w.Code != 403 {
		t.Fatalf("a sign-in attempt on the preview without access: %d", w.Code)
	}
	// The owner opened the preview; signing up there works across both hosts.
	owner := newVisitor(t, p, host)
	link, _ := url.Parse(p.PreviewLink("club"))
	owner.do("GET", link.RequestURI(), nil)
	w := owner.submit("/_rx/auth/signup?next=/account", "/_rx/auth/signup", url.Values{"email": {"test@club.ae"}, "password": {"preview password"}, "next": {"/account"}})
	if w.Code != 303 || w.Header().Get("Location") != "/account" {
		t.Fatalf("sign-up on the preview: %d %s", w.Code, w.Body.String())
	}
	if w := owner.do("GET", "/account", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "test@club.ae") {
		t.Fatalf("account on the preview: %d", w.Code)
	}
}

// slowBox is a mail service that takes its time, like a real one.
type slowBox struct{ outbox }

func (s *slowBox) Send(ctx context.Context, m email.Message) error {
	time.Sleep(400 * time.Millisecond)
	return s.outbox.Send(ctx, m)
}

// The sign-in pages must not tell which email addresses have an account:
// "forgot password" answers the same and as fast either way, and sign-up,
// which has to say when an address is taken, stops for an address that
// got that answer a few times.
func TestSignInPagesDoNotListAccounts(t *testing.T) {
	box := &slowBox{}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", Mailer: box}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.CreateApp("usr_test", "club")
	p.ApplyChange(context.Background(), "club", membersApp())
	p.Publish("club", 0)
	host := "club.renoxium.test"
	member := newVisitor(t, p, host)
	member.ip = "203.0.113.50"
	member.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"email": {"member@club.ae"}, "password": {"member's password"}})

	forgot := func(addr string) (time.Duration, string) {
		v := newVisitor(t, p, host)
		v.ip = "203.0.113.51"
		page := v.do("GET", "/_rx/auth/forgot", nil)
		form := url.Values{"csrf": {csrfField.FindStringSubmatch(page.Body.String())[1]}, "email": {addr}}
		start := time.Now()
		w := v.do("POST", "/_rx/auth/forgot", form)
		return time.Since(start), strings.ReplaceAll(w.Body.String(), addr, "ADDRESS")
	}
	knownTook, knownPage := forgot("member@club.ae")
	unknownTook, unknownPage := forgot("nobody@club.ae")
	strip := func(page string) string { return csrfField.ReplaceAllString(page, "") }
	if strip(knownPage) != strip(unknownPage) {
		t.Fatal("the answer differs for an address with an account")
	}
	if knownTook > 200*time.Millisecond || unknownTook > 200*time.Millisecond {
		t.Fatalf("the answer waited for the mail service: %s with an account, %s without", knownTook, unknownTook)
	}
	p.WaitBackground()
	if m := box.last(t, "visitor_reset"); m.To != "member@club.ae" {
		t.Fatalf("reset email: %+v", m)
	}

	// Sign-up from one address: five "already exists" answers, then nothing.
	prober := func(addr string) int {
		v := newVisitor(t, p, host)
		v.ip = "203.0.113.52"
		return v.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"email": {addr}, "password": {"some long password"}}).Code
	}
	for i := 0; i < 5; i++ {
		if c := prober("member@club.ae"); c != 409 {
			t.Fatalf("taken address, try %d: %d", i, c)
		}
	}
	if c := prober("member@club.ae"); c != 429 {
		t.Fatalf("sixth try with a taken address: %d", c)
	}
	if c := prober("brand-new@club.ae"); c != 429 {
		t.Fatalf("a free address after the limit told it apart from a taken one: %d", c)
	}
	// Someone else is not affected.
	other := newVisitor(t, p, host)
	other.ip = "203.0.113.53"
	if w := other.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"email": {"brand-new@club.ae"}, "password": {"some long password"}}); w.Code != 303 {
		t.Fatalf("sign-up from another address: %d", w.Code)
	}
}
