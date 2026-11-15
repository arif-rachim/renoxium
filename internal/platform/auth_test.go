package platform_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/email"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

type outbox struct {
	mu   sync.Mutex
	sent []email.Message
}

func (o *outbox) Send(_ context.Context, m email.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, m)
	return nil
}

func (o *outbox) last(t *testing.T, tag string) email.Message {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := len(o.sent) - 1; i >= 0; i-- {
		if o.sent[i].Tag == tag {
			return o.sent[i]
		}
	}
	t.Fatalf("no %s email", tag)
	return email.Message{}
}

// visitorBrowser keeps cookies per host, like a browser.
type visitorBrowser struct {
	t       *testing.T
	p       *platform.Platform
	host    string
	cookies map[string]*http.Cookie
}

func newVisitor(t *testing.T, p *platform.Platform, host string) *visitorBrowser {
	return &visitorBrowser{t: t, p: p, host: host, cookies: map[string]*http.Cookie{}}
}

func (b *visitorBrowser) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	b.t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	r := httptest.NewRequest(method, "http://"+b.host+path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range b.cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	if !b.p.ServeApp(w, r) {
		b.t.Fatalf("%s is not an app host", b.host)
	}
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return w
}

var csrfField = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

// form loads a page and posts its form with the given fields.
func (b *visitorBrowser) submit(page, action string, fields url.Values) *httptest.ResponseRecorder {
	b.t.Helper()
	w := b.do("GET", page, nil)
	m := csrfField.FindStringSubmatch(w.Body.String())
	if m == nil {
		b.t.Fatalf("no form on %s: %d %s", page, w.Code, w.Body.String())
	}
	fields.Set("csrf", m[1])
	return b.do("POST", action, fields)
}

func membersApp() *appdef.App {
	return &appdef.App{
		Tables: []appdef.Table{{Name: "notes", Columns: []appdef.Column{{Name: "user_id", Type: "integer"}, {Name: "body", Type: "text"}}}},
		Templates: map[string]string{
			"home":       `<!doctype html><html><head></head><body>{{if .user}}Hi {{.user.name}}{{else}}Welcome, guest{{end}}</body></html>`,
			"auth_login": `<!doctype html><html><head></head><body><h1>Members sign in</h1>{{with .error}}<p>{{.}}</p>{{end}}<form method="post" action="/_rx/auth/login"><input type="hidden" name="csrf" value="{{.csrf}}"><input type="hidden" name="next" value="{{.next}}"><input name="email"><input name="password"></form></body></html>`,
		},
		Scripts: map[string]string{
			"account": `function handle(req) { return {json: req.user} }`,
			"add":     `function handle(req) { db.exec("INSERT INTO notes (user_id, body) VALUES (?, ?)", req.user.id, req.form.body); return {json: db.query("SELECT body FROM notes WHERE user_id = ?", req.user.id)} }`,
			"steal":   `function handle(req) { try { return {json: db.query("SELECT email, password_hash FROM rx_users")} } catch (e) { return {text: "denied"} } }`,
		},
		Routes: []appdef.Route{
			{Method: "GET", Path: "/", Template: "home"},
			{Method: "GET", Path: "/account", Script: "account", Auth: "user"},
			{Method: "POST", Path: "/notes", Script: "add", Auth: "user"},
			{Method: "GET", Path: "/steal", Script: "steal"},
		},
	}
}

func newAuthPlatform(t *testing.T) (*platform.Platform, *outbox) {
	t.Helper()
	box := &outbox{}
	owners := func(id string) (string, bool) { return "owner@club.ae", id == "usr_test" }
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", Mailer: box, OwnerEmail: owners}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, box
}

var tokenLink = regexp.MustCompile(`https://[^\s]+token=\S+`)

func TestVisitorAccounts(t *testing.T) {
	p, box := newAuthPlatform(t)
	p.CreateApp("usr_test", "club")
	if res, err := p.ApplyChange(context.Background(), "club", membersApp()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	p.Publish("club", 0)
	host := "club.renoxium.test"
	v := newVisitor(t, p, host)

	// Signed out: protected GET redirects to sign in, protected POST is refused.
	if w := v.do("GET", "/account", nil); w.Code != 303 || w.Header().Get("Location") != "/_rx/auth/login?next=%2Faccount" {
		t.Fatalf("signed out GET: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := v.do("POST", "/notes", url.Values{"body": {"x"}}); w.Code != 401 {
		t.Fatalf("signed out POST: %d", w.Code)
	}
	if w := v.do("GET", "/", nil); !strings.Contains(w.Body.String(), "Welcome, guest") {
		t.Fatal("home for a guest")
	}
	// A form without the CSRF token is refused.
	if w := v.do("POST", "/_rx/auth/signup", url.Values{"email": {"a@b.ae"}, "password": {"long enough pw"}}); w.Code != 403 {
		t.Fatalf("signup without csrf: %d", w.Code)
	}

	// Sign up, landing back where they were going.
	w := v.submit("/_rx/auth/signup?next=/account", "/_rx/auth/signup",
		url.Values{"name": {"Salma"}, "email": {"Salma@Mail.ae"}, "password": {"salma's password"}, "next": {"/account"}})
	if w.Code != 303 || w.Header().Get("Location") != "/account" {
		t.Fatalf("signup: %d %s", w.Code, w.Body.String())
	}
	sc := v.cookies["rx_session"]
	if sc == nil || !sc.HttpOnly || !sc.Secure || sc.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie: %+v", sc)
	}
	if w := v.do("GET", "/account", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"email":"salma@mail.ae"`) || !strings.Contains(w.Body.String(), `"verified":false`) {
		t.Fatalf("account: %d %s", w.Code, w.Body.String())
	}
	if w := v.do("GET", "/", nil); !strings.Contains(w.Body.String(), "Hi Salma") {
		t.Fatal("template route does not see the visitor")
	}
	if w := v.do("POST", "/notes", url.Values{"body": {"first"}}); w.Code != 200 || !strings.Contains(w.Body.String(), "first") {
		t.Fatalf("notes: %d %s", w.Code, w.Body.String())
	}

	// Confirmation email from the live app, with the app's sender name.
	msg := box.last(t, "visitor_verify")
	if msg.To != "salma@mail.ae" || msg.FromName != "club via Renoxium" || msg.ReplyTo != "owner@club.ae" {
		t.Fatalf("verify email: %+v", msg)
	}
	link := tokenLink.FindString(msg.Text)
	if !strings.HasPrefix(link, "https://club.renoxium.test/_rx/auth/verify?token=") {
		t.Fatalf("link: %s", link)
	}
	if w := v.do("GET", strings.TrimPrefix(link, "https://"+host), nil); w.Code != 200 || !strings.Contains(w.Body.String(), "confirmed") {
		t.Fatalf("verify: %d", w.Code)
	}
	if w := v.do("GET", "/account", nil); !strings.Contains(w.Body.String(), `"verified":true`) {
		t.Fatal("not verified")
	}

	// Scripts cannot read accounts.
	if w := v.do("GET", "/steal", nil); w.Body.String() != "denied" {
		t.Fatalf("steal: %s", w.Body.String())
	}

	// Sign out, then sign in again; wrong password fails.
	if w := v.do("POST", "/_rx/auth/logout", nil); w.Code != 303 {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := v.do("GET", "/account", nil); w.Code != 303 {
		t.Fatal("still signed in after logout")
	}
	if w := v.submit("/_rx/auth/login", "/_rx/auth/login", url.Values{"email": {"salma@mail.ae"}, "password": {"wrong password"}}); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	// The app's own auth_login template is used.
	if w := v.do("GET", "/_rx/auth/login", nil); !strings.Contains(w.Body.String(), "Members sign in") {
		t.Fatal("custom login page not used")
	}
	if w := v.submit("/_rx/auth/login", "/_rx/auth/login", url.Values{"email": {"salma@mail.ae"}, "password": {"salma's password"}, "next": {"//evil.example/x"}}); w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatalf("login with foreign next: %d %s", w.Code, w.Header().Get("Location"))
	}

	// A session of one app means nothing on another app.
	p.CreateApp("usr_test", "other-club")
	p.ApplyChange(context.Background(), "other-club", membersApp())
	p.Publish("other-club", 0)
	o := newVisitor(t, p, "other-club.renoxium.test")
	o.cookies["rx_session"] = v.cookies["rx_session"]
	if w := o.do("GET", "/account", nil); w.Code != 303 {
		t.Fatal("session worked on another app")
	}

	// Password reset signs out other sessions.
	other := newVisitor(t, p, host)
	other.cookies["rx_session"] = v.cookies["rx_session"]
	r := newVisitor(t, p, host)
	if w := r.submit("/_rx/auth/forgot", "/_rx/auth/forgot", url.Values{"email": {"salma@mail.ae"}}); w.Code != 200 || !strings.Contains(w.Body.String(), "If an account exists") {
		t.Fatalf("forgot: %d", w.Code)
	}
	resetLink := strings.TrimPrefix(tokenLink.FindString(box.last(t, "visitor_reset").Text), "https://"+host)
	token := strings.SplitN(resetLink, "token=", 2)[1]
	tok, _ := url.QueryUnescape(token)
	if w := r.submit(resetLink, "/_rx/auth/reset", url.Values{"token": {tok}, "password": {"a new salma password"}}); w.Code != 303 {
		t.Fatalf("reset: %d %s", w.Code, w.Body.String())
	}
	if w := other.do("GET", "/account", nil); w.Code != 303 {
		t.Fatal("old session survived the reset")
	}
	if w := r.do("GET", "/account", nil); w.Code != 200 {
		t.Fatal("not signed in after reset")
	}
	if w := newVisitor(t, p, host).submit("/_rx/auth/login", "/_rx/auth/login", url.Values{"email": {"salma@mail.ae"}, "password": {"salma's password"}}); w.Code != 401 {
		t.Fatal("old password still works")
	}
}

func TestPreviewEmailsGoToTheLog(t *testing.T) {
	p, box := newAuthPlatform(t)
	p.CreateApp("usr_test", "club")
	p.ApplyChange(context.Background(), "club", membersApp())
	v := newVisitor(t, p, "club--preview.renoxium.test")
	if w := v.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"email": {"test@club.ae"}, "password": {"preview password"}}); w.Code != 303 {
		t.Fatalf("signup: %d", w.Code)
	}
	if len(box.sent) != 0 {
		t.Fatalf("preview sent %d emails", len(box.sent))
	}
	logs, _ := p.Logs("club", applog.Filter{Source: "email"})
	if len(logs) != 1 || !strings.Contains(logs[0].Message, "not sent from preview") || !strings.Contains(logs[0].Message, "https://club--preview.renoxium.test/_rx/auth/verify?token=") {
		t.Fatalf("logs: %+v", logs)
	}

	// test_request can sign up and then act as the visitor.
	res, err := p.TestRequest(context.Background(), "club", platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup",
		Form: map[string]string{"name": "Agent", "email": "agent@club.ae", "password": "agent password"}})
	if err != nil || res.Status != 303 {
		t.Fatalf("test signup: %v %+v", err, res)
	}
	res, err = p.TestRequest(context.Background(), "club", platform.TestRequestInput{Path: "/account", UserEmail: "agent@club.ae"})
	if err != nil || res.Status != 200 || !strings.Contains(res.Body, "agent@club.ae") {
		t.Fatalf("as visitor: %v %+v", err, res)
	}
	if _, err := p.TestRequest(context.Background(), "club", platform.TestRequestInput{Path: "/account", UserEmail: "ghost@club.ae"}); err == nil {
		t.Fatal("unknown visitor accepted")
	}
}

type codeBox struct {
	mu    sync.Mutex
	codes map[string]string
}

func (c *codeBox) SendCode(_ context.Context, phone, code string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.codes[phone] = code
	return nil
}

func TestWhatsAppSignIn(t *testing.T) {
	codes := &codeBox{codes: map[string]string{}}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", WhatsApp: codes}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.CreateApp("usr_test", "club")
	def := membersApp()
	delete(def.Templates, "auth_login") // use the platform's sign-in page
	p.ApplyChange(context.Background(), "club", def)
	p.Publish("club", 0)
	host := "club.renoxium.test"
	v := newVisitor(t, p, host)

	if w := v.do("GET", "/_rx/auth/login", nil); !strings.Contains(w.Body.String(), "/_rx/auth/phone") {
		t.Fatal("sign-in page does not offer WhatsApp")
	}
	if w := v.submit("/_rx/auth/phone", "/_rx/auth/phone", url.Values{"phone": {"not a number"}}); w.Code != 400 {
		t.Fatalf("bad number: %d", w.Code)
	}
	w := v.submit("/_rx/auth/phone?next=/account", "/_rx/auth/phone", url.Values{"phone": {"050 123 4567"}, "next": {"/account"}})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "971501234567") {
		t.Fatalf("send: %d %s", w.Code, w.Body.String())
	}
	code := codes.codes["+971501234567"]
	if len(code) != 6 {
		t.Fatalf("code = %q", code)
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	verify := func(c string) *httptest.ResponseRecorder {
		return v.submit("/_rx/auth/phone", "/_rx/auth/phone/verify", url.Values{"phone": {"+971501234567"}, "code": {c}, "name": {"Huda"}, "next": {"/account"}})
	}
	if w := verify(wrong); w.Code != 401 {
		t.Fatalf("wrong code: %d", w.Code)
	}
	if w := verify(code); w.Code != 303 || w.Header().Get("Location") != "/account" {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}
	if w := v.do("GET", "/account", nil); !strings.Contains(w.Body.String(), `"phone":"+971501234567"`) || !strings.Contains(w.Body.String(), `"name":"Huda"`) || !strings.Contains(w.Body.String(), `"verified":true`) {
		t.Fatalf("account: %s", w.Body.String())
	}
	// A code works once.
	if w := verify(code); w.Code != 400 {
		t.Fatalf("code reused: %d", w.Code)
	}
	// Signing in again with the same number reuses the account.
	v.do("POST", "/_rx/auth/logout", nil)
	v.submit("/_rx/auth/phone", "/_rx/auth/phone", url.Values{"phone": {"+971501234567"}})
	verify(codes.codes["+971501234567"])
	if w := v.do("GET", "/account", nil); !strings.Contains(w.Body.String(), `"id":1`) {
		t.Fatalf("second sign-in made a new account: %s", w.Body.String())
	}
	list, total, _ := p.Visitors(context.Background(), "club", "", 1)
	if total != 1 || list[0].Phone != "+971501234567" || list[0].Email != "" {
		t.Fatalf("visitors: %+v", list)
	}

	// Five wrong guesses burn the code.
	v.submit("/_rx/auth/phone", "/_rx/auth/phone", url.Values{"phone": {"+971509999999"}})
	good := codes.codes["+971509999999"]
	bad := "000000"
	if good == bad {
		bad = "111111"
	}
	for i := 0; i < 5; i++ {
		v.submit("/_rx/auth/phone", "/_rx/auth/phone/verify", url.Values{"phone": {"+971509999999"}, "code": {bad}})
	}
	if w := v.submit("/_rx/auth/phone", "/_rx/auth/phone/verify", url.Values{"phone": {"+971509999999"}, "code": {good}}); w.Code != 400 {
		t.Fatalf("code still worked after 5 wrong guesses: %d", w.Code)
	}

	// At most three codes per number per 15 minutes (each costs a message).
	last := 0
	for i := 0; i < 4; i++ {
		last = v.submit("/_rx/auth/phone", "/_rx/auth/phone", url.Values{"phone": {"+971508888888"}}).Code
	}
	if last != 429 {
		t.Fatalf("fourth code: %d", last)
	}
}

func TestWhatsAppOffOnLiveWithoutConfig(t *testing.T) {
	p := newPlatform(t)
	p.CreateApp("usr_test", "club")
	p.ApplyChange(context.Background(), "club", membersApp())
	p.Publish("club", 0)
	live := newVisitor(t, p, "club.renoxium.test")
	if w := live.do("GET", "/_rx/auth/phone", nil); w.Code != 404 {
		t.Fatalf("live without WhatsApp: %d", w.Code)
	}
	// Preview always offers it, with the code in the log and no message sent.
	res, err := p.TestRequest(context.Background(), "club", platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/phone", Form: map[string]string{"phone": "+971501112222"}})
	if err != nil || res.Status != 200 {
		t.Fatalf("preview send: %v %+v", err, res)
	}
	logs, _ := p.Logs("club", applog.Filter{Source: "whatsapp"})
	m := regexp.MustCompile(`sign-in code (\d{6}) for \+971501112222`).FindStringSubmatch(logs[0].Message)
	if m == nil {
		t.Fatalf("logs: %+v", logs)
	}
	res, _ = p.TestRequest(context.Background(), "club", platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/phone/verify",
		Form: map[string]string{"phone": "+971501112222", "code": m[1], "name": "Tester"}})
	if res.Status != 303 {
		t.Fatalf("preview verify: %+v", res)
	}
	res, _ = p.TestRequest(context.Background(), "club", platform.TestRequestInput{Path: "/account", UserEmail: "+971501112222"})
	if res.Status != 200 || !strings.Contains(res.Body, `"name":"Tester"`) {
		t.Fatalf("as WhatsApp visitor: %+v", res)
	}
}
