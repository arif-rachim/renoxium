package web_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/arif-rachim/renoxium/internal/monitor"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/web"
)

const redirectURI = "http://localhost:9999/callback"

type env struct {
	acc    *accounts.Store
	site   *web.Site
	mail   *mailbox
	t      *testing.T
	server *httptest.Server
	base   string
	p      *platform.Platform
	dir    string
}

func newEnv(t *testing.T) *env { return newEnvPlans(t, false) }

// newEnvPlans is newEnv where, with plans, the platform reads each owner's
// plan from the accounts store, as the server does.
func newEnvPlans(t *testing.T, plans bool, opts ...func(*platform.Config)) *env {
	t.Helper()
	dir := t.TempDir()
	acc, err := accounts.Open(filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := platform.Config{DataDir: dir, AppsDomain: "renoxium.test", PublicScheme: "https"}
	if plans {
		cfg.OwnerPlan = acc.Plan
		cfg.OwnerActive = func(id string) { acc.NoteActivity(id) }
		cfg.OwnerDeactivated = acc.IsDeactivated
	}
	for _, o := range opts {
		o(&cfg)
	}
	p, err := platform.New(cfg, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	var site http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.ServeApp(w, r) {
			site.ServeHTTP(w, r)
		}
	}))
	ws := web.New(srv.URL, acc, p)
	mb := &mailbox{}
	ws.Mailer = mb
	mb.wait = ws.WaitBackground
	ws.StripeWebhookSecret = "whsec_test"
	ws.AbuseEmail = "abuse@renoxium.test"
	site = ws
	t.Cleanup(func() { srv.Close(); p.Close(); acc.Close() })
	return &env{t: t, server: srv, base: srv.URL, p: p, mail: mb, site: ws, acc: acc, dir: dir}
}

// browser is a cookie-keeping client that does not follow redirects.
func (e *env) browser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func read(t *testing.T, r *http.Response) string {
	t.Helper()
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

var csrfRE = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func (e *env) csrf(c *http.Client, path string) string {
	e.t.Helper()
	r, err := c.Get(e.base + path)
	if err != nil {
		e.t.Fatal(err)
	}
	m := csrfRE.FindStringSubmatch(read(e.t, r))
	if m == nil {
		e.t.Fatalf("no csrf token on %s", path)
	}
	return m[1]
}

func (e *env) signup(c *http.Client, email string) {
	e.t.Helper()
	token := e.csrf(c, "/signup")
	r, err := c.PostForm(e.base+"/signup", url.Values{"csrf": {token}, "email": {email}, "password": {"correct horse battery"}})
	if err != nil {
		e.t.Fatal(err)
	}
	if r.StatusCode != http.StatusSeeOther {
		e.t.Fatalf("signup: %d %s", r.StatusCode, read(e.t, r))
	}
}

func (e *env) registerClient() string {
	e.t.Helper()
	r, err := http.Post(e.base+"/oauth/register", "application/json",
		strings.NewReader(`{"client_name":"Test Agent","redirect_uris":["`+redirectURI+`"],"token_endpoint_auth_method":"none"}`))
	if err != nil {
		e.t.Fatal(err)
	}
	var out map[string]any
	json.Unmarshal([]byte(read(e.t, r)), &out)
	id, _ := out["client_id"].(string)
	if r.StatusCode != http.StatusCreated || id == "" {
		e.t.Fatalf("register: %d %v", r.StatusCode, out)
	}
	return id
}

func pkce() (verifier, challenge string) {
	verifier = "verifier-" + strings.Repeat("x", 50)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func (e *env) authorizeURL(clientID, challenge string) string {
	return "/oauth/authorize?" + url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"st4te"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "scope": {"apps"}, "resource": {e.base + "/mcp"},
	}.Encode()
}

// connect runs the whole OAuth flow for a signed-in browser and returns tokens.
func (e *env) connect(c *http.Client, clientID string) accounts.Tokens {
	e.t.Helper()
	code, verifier := e.approve(c, clientID)
	t, status := e.exchange(clientID, code, verifier)
	if status != 200 || t.AccessToken == "" || t.RefreshToken == "" {
		e.t.Fatalf("token: %d", status)
	}
	return t
}

func (e *env) exchange(clientID, code, verifier string) (accounts.Tokens, int) {
	e.t.Helper()
	r, _ := http.PostForm(e.base+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {clientID}, "redirect_uri": {redirectURI}, "code_verifier": {verifier}, "resource": {e.base + "/mcp"}})
	var t accounts.Tokens
	json.Unmarshal([]byte(read(e.t, r)), &t)
	return t, r.StatusCode
}

// approve signs the consent screen and returns the authorization code.
func (e *env) approve(c *http.Client, clientID string) (code, verifier string) {
	e.t.Helper()
	verifier, challenge := pkce()
	authz := e.authorizeURL(clientID, challenge)
	r, _ := c.Get(e.base + authz)
	page := read(e.t, r)
	if r.StatusCode != 200 || !strings.Contains(page, "Connect Test Agent to your Renoxium account") || !strings.Contains(page, "localhost:9999") {
		e.t.Fatalf("consent page: %d %s", r.StatusCode, page)
	}
	form := url.Values{"decision": {"allow"}, "csrf": {csrfRE.FindStringSubmatch(page)[1]}}
	u, _ := url.Parse(authz)
	for k, v := range u.Query() {
		form[k] = v
	}
	r, _ = c.PostForm(e.base+"/oauth/authorize", form)
	loc, _ := url.Parse(r.Header.Get("Location"))
	code = loc.Query().Get("code")
	if r.StatusCode != http.StatusFound || !strings.HasPrefix(loc.String(), redirectURI) || code == "" ||
		loc.Query().Get("state") != "st4te" || loc.Query().Get("iss") != e.base {
		e.t.Fatalf("approve: %d %s", r.StatusCode, loc)
	}
	return code, verifier
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func (e *env) mcpSession(token string) *mcp.ClientSession {
	e.t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	s, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: e.base + "/mcp", HTTPClient: &http.Client{Transport: bearer{token}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { s.Close() })
	return s
}

func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) (map[string]any, string) {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		return nil, res.Content[0].(*mcp.TextContent).Text
	}
	out, _ := res.StructuredContent.(map[string]any)
	return out, ""
}

// confirmPublish opens a publish tool's confirm_url as the owner and confirms.
func (e *env) confirmPublish(c *http.Client, pub map[string]any) {
	e.t.Helper()
	link, _ := pub["confirm_url"].(string)
	if !strings.HasPrefix(link, e.base+"/apps/") {
		e.t.Fatalf("confirm_url: %v", pub)
	}
	path := strings.TrimPrefix(link, e.base)
	token := e.csrf(c, path)
	r, err := c.PostForm(e.base+strings.Split(path, "?")[0], url.Values{"csrf": {token}, "version": {fmt.Sprint(pub["version"])}})
	if err != nil {
		e.t.Fatal(err)
	}
	if body := read(e.t, r); r.StatusCode != 200 || !strings.Contains(body, "is live") {
		e.t.Fatalf("confirm publish: %d %s", r.StatusCode, body)
	}
}

func TestMCPRequiresOAuthAndPointsToMetadata(t *testing.T) {
	e := newEnv(t)
	r, _ := http.Post(e.base+"/mcp", "application/json", strings.NewReader(`{}`))
	if r.StatusCode != 401 || !strings.Contains(r.Header.Get("WWW-Authenticate"), e.base+"/.well-known/oauth-protected-resource/mcp") {
		t.Fatalf("unauthenticated MCP: %d %q", r.StatusCode, r.Header.Get("WWW-Authenticate"))
	}
	r, _ = http.Get(e.base + "/.well-known/oauth-protected-resource/mcp")
	var prm map[string]any
	json.Unmarshal([]byte(read(t, r)), &prm)
	if prm["resource"] != e.base+"/mcp" || prm["authorization_servers"].([]any)[0] != e.base {
		t.Fatalf("resource metadata: %v", prm)
	}
	r, _ = http.Get(e.base + "/.well-known/oauth-authorization-server")
	var as map[string]any
	json.Unmarshal([]byte(read(t, r)), &as)
	if as["issuer"] != e.base || as["registration_endpoint"] != e.base+"/oauth/register" || as["code_challenge_methods_supported"].([]any)[0] != "S256" {
		t.Fatalf("server metadata: %v", as)
	}
	req, _ := http.NewRequest("POST", e.base+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer rxa_made_up")
	r, _ = http.DefaultClient.Do(req)
	if r.StatusCode != 401 {
		t.Fatalf("made-up token: %d", r.StatusCode)
	}
}

// An agent connects with OAuth, builds the catalog and publishes it, and a
// second user cannot see or change it.
func TestAgentBuildsAppForSignedInUser(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient()

	alice := e.browser()
	e.signup(alice, "alice@example.com")
	tokens := e.connect(alice, clientID)
	s := e.mcpSession(tokens.AccessToken)

	tools, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if got := strings.Join(names, ","); got != "apply_change,change_preview_row,create_app,create_test_visitor,delete_app,edit_app,get_app,get_app_outline,get_app_parts,get_example,get_logs,list_apps,patch_app,publish,rename_app,screenshot_preview,submit_form,try_scheduled_jobs,upload_as_visitor,upload_file,view_live_page,view_page" {
		t.Fatalf("tools = %s", got)
	}

	if _, errText := call(t, s, "create_app", map[string]any{"app": "al-noor"}); errText != "" {
		t.Fatal(errText)
	}
	raw, _ := os.ReadFile("../../examples/catalog.json")
	var def map[string]any
	json.Unmarshal(raw, &def)
	applied, errText := call(t, s, "apply_change", map[string]any{"app": "al-noor", "definition": def})
	if errText != "" || applied["ok"] != true {
		t.Fatalf("apply_change: %v %s", applied, errText)
	}
	pub, errText := call(t, s, "publish", map[string]any{"app": "al-noor"})
	if errText != "" || pub["needs_confirmation"] != true {
		t.Fatalf("publish: %v %s", pub, errText)
	}
	// Nothing is live until the owner confirms in the browser.
	req := httptest.NewRequest("GET", "http://al-noor.renoxium.test/", nil)
	w := httptest.NewRecorder()
	if e.p.ServeApp(w, req); w.Code != 404 {
		t.Fatalf("live before confirmation: %d", w.Code)
	}
	// Another signed-in user cannot confirm it.
	mallory := e.browser()
	e.signup(mallory, "mallory@example.com")
	if r, _ := mallory.Get(pub["confirm_url"].(string)); r.StatusCode != 404 {
		t.Fatalf("someone else's confirm link: %d", r.StatusCode)
	}
	e.confirmPublish(alice, pub)
	req = httptest.NewRequest("GET", "http://al-noor.renoxium.test/", nil)
	w = httptest.NewRecorder()
	if !e.p.ServeApp(w, req) || w.Code != 200 || !strings.Contains(w.Body.String(), "White Musk") {
		t.Fatalf("live app: %d", w.Code)
	}
	list, _ := call(t, s, "list_apps", map[string]any{})
	if apps, _ := list["apps"].([]any); len(apps) != 1 {
		t.Fatalf("list_apps = %v", list)
	}

	// The dashboard lists the app for its owner.
	r, _ := alice.Get(e.base + "/")
	if page := read(t, r); !strings.Contains(page, "al-noor") || !strings.Contains(page, "alice@example.com") {
		t.Fatalf("dashboard: %s", page)
	}

	// Bob is a different user: Alice's app looks like it does not exist.
	bob := e.browser()
	e.signup(bob, "bob@example.com")
	bs := e.mcpSession(e.connect(bob, clientID).AccessToken)
	for _, tool := range []string{"get_app", "apply_change", "publish", "get_logs"} {
		args := map[string]any{"app": "al-noor"}
		if tool == "apply_change" {
			args["definition"] = def
		}
		if _, errText := call(t, bs, tool, args); !strings.Contains(errText, "not found") {
			t.Errorf("bob %s on alice's app: %q", tool, errText)
		}
	}
	if list, _ := call(t, bs, "list_apps", map[string]any{}); len(list["apps"].([]any)) != 0 {
		t.Fatalf("bob sees apps: %v", list)
	}
	if _, errText := call(t, bs, "create_app", map[string]any{"app": "al-noor"}); !strings.Contains(errText, "taken") {
		t.Fatalf("bob took alice's name: %q", errText)
	}
}

func TestRefreshTokenRotates(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient()
	c := e.browser()
	e.signup(c, "carol@example.com")
	first := e.connect(c, clientID)

	refresh := func(token string) (*http.Response, accounts.Tokens) {
		r, _ := http.PostForm(e.base+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token}, "client_id": {clientID}})
		var t2 accounts.Tokens
		json.Unmarshal([]byte(read(t, r)), &t2)
		return r, t2
	}
	r, second := refresh(first.RefreshToken)
	if r.StatusCode != 200 || second.AccessToken == "" || second.RefreshToken == first.RefreshToken {
		t.Fatalf("refresh: %d", r.StatusCode)
	}
	if _, errText := call(t, e.mcpSession(second.AccessToken), "list_apps", map[string]any{}); errText != "" {
		t.Fatal(errText)
	}
	// Presenting a rotated-away refresh token means it was copied: it fails,
	// and every token of that grant is revoked.
	if r, _ := refresh(first.RefreshToken); r.StatusCode != 400 {
		t.Fatal("old refresh token still works")
	}
	if r, _ := refresh(second.RefreshToken); r.StatusCode != 400 {
		t.Fatal("refresh token of a replayed grant still works")
	}
	req, _ := http.NewRequest("POST", e.base+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer "+second.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 401 {
		t.Fatalf("access token of a replayed grant still works: %v %v", err, r)
	}
}

func TestAuthorizeRejectsBadRequests(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient()
	c := e.browser()
	e.signup(c, "dan@example.com")
	_, challenge := pkce()

	// Unregistered redirect URI: show an error, never redirect.
	bad := strings.Replace(e.authorizeURL(clientID, challenge), url.QueryEscape(redirectURI), url.QueryEscape("https://evil.example/cb"), 1)
	r, _ := c.Get(e.base + bad)
	if r.StatusCode != 400 || r.Header.Get("Location") != "" {
		t.Fatalf("bad redirect: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	// Missing PKCE: error goes back to the registered redirect URI.
	noPKCE := strings.Replace(e.authorizeURL(clientID, challenge), "code_challenge_method=S256", "code_challenge_method=plain", 1)
	r, _ = c.Get(e.base + noPKCE)
	if loc := r.Header.Get("Location"); !strings.HasPrefix(loc, redirectURI) || !strings.Contains(loc, "error=invalid_request") {
		t.Fatalf("plain PKCE: %d %s", r.StatusCode, loc)
	}
	// A crafted link cannot pre-approve the consent form.
	r, _ = c.Get(e.base + e.authorizeURL(clientID, challenge) + "&decision=allow&csrf=x")
	if page := read(t, r); strings.Count(page, `name="decision"`) != 2 || strings.Contains(page, `name="csrf" value="x"`) {
		t.Fatal("query parameters leaked into hidden form fields")
	}
	// Approving without the CSRF token is refused.
	form := url.Values{"decision": {"allow"}}
	u, _ := url.Parse(e.authorizeURL(clientID, challenge))
	for k, v := range u.Query() {
		form[k] = v
	}
	r, _ = c.PostForm(e.base+"/oauth/authorize", form)
	if r.StatusCode != 403 {
		t.Fatalf("approve without csrf: %d", r.StatusCode)
	}
	// Signed out: authorize sends the user to sign in and back.
	r, _ = e.browser().Get(e.base + e.authorizeURL(clientID, challenge))
	if loc := r.Header.Get("Location"); r.StatusCode != 303 || !strings.HasPrefix(loc, "/login?next=%2Foauth%2Fauthorize") {
		t.Fatalf("signed out: %d %s", r.StatusCode, loc)
	}
	// Wrong PKCE verifier.
	code, _ := e.approve(c, clientID)
	if _, status := e.exchange(clientID, code, strings.Repeat("y", 60)); status != 400 {
		t.Fatalf("wrong verifier: %d", status)
	}
}

func TestRegisterRejectsUnsafeRedirects(t *testing.T) {
	e := newEnv(t)
	for _, uri := range []string{"http://evil.example/cb", "javascript:alert(1)", "https://x.example/cb#frag"} {
		r, _ := http.Post(e.base+"/oauth/register", "application/json", strings.NewReader(`{"redirect_uris":["`+uri+`"]}`))
		if r.StatusCode != 400 {
			t.Errorf("%s registered: %d", uri, r.StatusCode)
		}
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "erin@example.com")
	c = e.browser()
	token := e.csrf(c, "/login")
	last := 0
	for i := 0; i < 12; i++ {
		r, _ := c.PostForm(e.base+"/login", url.Values{"csrf": {token}, "email": {"erin@example.com"}, "password": {"wrong password!"}})
		last = r.StatusCode
		read(t, r)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after 12 wrong passwords: %d", last)
	}
}

// Redeeming a code twice fails and revokes the tokens the code issued,
// because a replayed code may have been stolen.
func TestReplayedCodeRevokesTokens(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient()
	c := e.browser()
	e.signup(c, "fay@example.com")
	code, verifier := e.approve(c, clientID)
	tokens, status := e.exchange(clientID, code, verifier)
	if status != 200 {
		t.Fatalf("first exchange: %d", status)
	}
	if _, status := e.exchange(clientID, code, verifier); status != 400 {
		t.Fatalf("second exchange: %d", status)
	}
	req, _ := http.NewRequest("POST", e.base+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	r, _ := http.DefaultClient.Do(req)
	if r.StatusCode != 401 {
		t.Fatalf("token from a replayed code still works: %d", r.StatusCode)
	}
}

// Behind cloudflared every request comes from loopback. Rate limits must
// follow the visitor address in the trusted header, or one attacker locks
// everyone out.
func TestRateLimitFollowsTunnelHeader(t *testing.T) {
	dir := t.TempDir()
	p, _ := platform.New(platform.Config{DataDir: dir, AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	acc, _ := accounts.Open(filepath.Join(dir, "accounts.db"))
	defer p.Close()
	defer acc.Close()
	site := web.New("https://renoxium.test", acc, p)
	site.TrustedIPHeader = "CF-Connecting-IP"

	register := func(remote, header string) int {
		r := httptest.NewRequest("POST", "https://renoxium.test/oauth/register", strings.NewReader(`{"redirect_uris":["https://agent.example/cb"]}`))
		r.RemoteAddr = remote
		if header != "" {
			r.Header.Set("CF-Connecting-IP", header)
		}
		w := httptest.NewRecorder()
		site.ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < 10; i++ {
		register("127.0.0.1:5000", "203.0.113.7")
	}
	if code := register("127.0.0.1:5000", "203.0.113.7"); code != 429 {
		t.Fatalf("attacker not limited: %d", code)
	}
	if code := register("127.0.0.1:5000", "198.51.100.9"); code != 201 {
		t.Fatalf("other visitor blocked by the attacker's limit: %d", code)
	}
	// From a non-loopback address the header is ignored.
	for i := 0; i < 10; i++ {
		register("192.0.2.1:5000", "10.0.0."+string(rune('0'+i)))
	}
	if code := register("192.0.2.1:5000", "10.0.0.99"); code != 429 {
		t.Fatalf("spoofed header bypassed the limit: %d", code)
	}
}

// Hosts other than the site's own, such as an app domain with no app, must
// not show the site or its sign-in forms.
func TestSiteAnswersOnlyOnItsOwnHost(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/", "/login", "/oauth/authorize", "/.well-known/oauth-authorization-server"} {
		r := httptest.NewRequest("GET", "http://nobody.renoxium.test"+path, nil)
		w := httptest.NewRecorder()
		e.server.Config.Handler.ServeHTTP(w, r)
		if w.Code != 404 || strings.Contains(w.Body.String(), "Sign in") {
			t.Errorf("%s on an unknown host: %d", path, w.Code)
		}
	}
	r, _ := http.Get(e.base + "/login")
	if r.StatusCode != 200 {
		t.Fatalf("site host: %d", r.StatusCode)
	}
	// The local health check works whatever host it is called with.
	hr := httptest.NewRequest("GET", "http://127.0.0.1:8080/healthz", nil)
	hw := httptest.NewRecorder()
	e.server.Config.Handler.ServeHTTP(hw, hr)
	if hw.Code != 200 {
		t.Fatalf("healthz on 127.0.0.1: %d", hw.Code)
	}
}

// A browser or an assistant's fetch tool opening /mcp gets instructions,
// while MCP clients still get the bare OAuth challenge.
func TestMCPEndpointExplainsHowToConnect(t *testing.T) {
	e := newEnv(t)
	r, _ := http.Get(e.base + "/mcp")
	body := read(t, r)
	if r.StatusCode != 401 || !strings.Contains(r.Header.Get("WWW-Authenticate"), "resource_metadata=") ||
		!strings.Contains(body, e.base+"/connect") || !strings.Contains(body, "you cannot add this connector yourself") ||
		!strings.Contains(body, "MCP Server URL: "+e.base+"/mcp") {
		t.Fatalf("GET /mcp: %d %s", r.StatusCode, body)
	}
	// An MCP client posting without a token still gets the SDK's challenge.
	r, _ = http.Post(e.base+"/mcp", "application/json", strings.NewReader(`{}`))
	if r.StatusCode != 401 || !strings.Contains(r.Header.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("POST /mcp: %d", r.StatusCode)
	}
	for _, path := range []string{"/connect", "/llms.txt"} {
		r, _ = http.Get(e.base + path)
		if body := read(t, r); r.StatusCode != 200 || !strings.Contains(body, e.base+"/mcp") || !strings.Contains(body, "Developer mode") {
			t.Fatalf("%s: %d", path, r.StatusCode)
		}
	}
}

// tunnel sends every request to the test server's loopback address while the
// Host header keeps the public name, as cloudflared does in production.
type tunnel struct {
	addr, token string
}

func (t tunnel) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Host = r.URL.Host
	r.URL.Scheme, r.URL.Host = "http", t.addr
	r.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}

// Behind Cloudflare Tunnel the site listens on 127.0.0.1 and requests arrive
// with Host www.renoxium.test. The MCP endpoint must serve them: the go-sdk's
// localhost DNS-rebinding check would answer 403 "invalid Host header".
func TestMCPWorksBehindTunnel(t *testing.T) {
	dir := t.TempDir()
	p, err := platform.New(platform.Config{DataDir: dir, AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	acc, err := accounts.Open(filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	const base = "https://www.renoxium.test"
	srv := httptest.NewServer(web.New(base, acc, p))
	t.Cleanup(func() { srv.Close(); p.Close(); acc.Close() })

	u, err := acc.CreateUser("owner@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	c, err := acc.RegisterClient("Claude", []string{redirectURI})
	if err != nil {
		t.Fatal(err)
	}
	verifier, challenge := pkce()
	code, err := acc.CreateCode(c.ID, u.ID, redirectURI, challenge, "apps")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := acc.ExchangeCode(code, c.ID, redirectURI, verifier)
	if err != nil {
		t.Fatal(err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "claude", Version: "1"}, nil)
	s, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: base + "/mcp", HTTPClient: &http.Client{Transport: tunnel{addr: srv.Listener.Addr().String(), token: tok.AccessToken}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("connect through the tunnel: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	tools, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools through the tunnel: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("no tools listed")
	}
}

func TestConsentWarnsAboutImpersonatingAgents(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "dina@example.com")
	consent := func(name, redirect string) string {
		r, _ := http.Post(e.base+"/oauth/register", "application/json",
			strings.NewReader(`{"client_name":"`+name+`","redirect_uris":["`+redirect+`"]}`))
		var out map[string]any
		json.Unmarshal([]byte(read(t, r)), &out)
		_, challenge := pkce()
		page, err := c.Get(e.base + "/oauth/authorize?" + url.Values{"response_type": {"code"}, "client_id": {out["client_id"].(string)},
			"redirect_uri": {redirect}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}.Encode())
		if err != nil {
			t.Fatal(err)
		}
		return read(t, page)
	}
	if body := consent("Claude", "https://evil.example/callback"); !strings.Contains(body, "Warning:") || !strings.Contains(body, "evil.example") {
		t.Fatalf("no warning for a fake Claude: %s", body)
	}
	if body := consent("Claude", "https://claude.ai/api/mcp/auth_callback"); strings.Contains(body, "Warning:") {
		t.Fatal("warning for the real Claude")
	}
	if body := consent("My own agent", "https://agent.example/cb"); strings.Contains(body, "Warning:") {
		t.Fatal("warning for an agent that claims no known name")
	}
}

func TestSiteRefusesCrossSiteForms(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	token := e.csrf(c, "/signup")
	req, _ := http.NewRequest("POST", e.base+"/signup", strings.NewReader(url.Values{"csrf": {token}, "email": {"x@example.com"}, "password": {"correct horse battery"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	if r, err := c.Do(req); err != nil || r.StatusCode != 403 {
		t.Fatalf("cross-site signup: %v %v", err, r)
	}
	// Token and registration endpoints are for other origins by design.
	req, _ = http.NewRequest("POST", e.base+"/oauth/token", strings.NewReader("grant_type=refresh_token&refresh_token=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://claude.ai")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 400 {
		t.Fatalf("cross-origin token request: %v %v", err, r)
	}
}

func TestAbuseReportsReachTheOperator(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	token := e.csrf(c, "/abuse")
	r, _ := c.PostForm(e.base+"/abuse", url.Values{"csrf": {token}, "address": {"https://fake-bank.renoxium.app"},
		"details": {"asks for card numbers"}, "contact": {"reporter@example.com"}})
	if body := read(t, r); r.StatusCode != 200 || !strings.Contains(body, "received your report") {
		t.Fatalf("report: %d %s", r.StatusCode, body)
	}
	m := e.mail.last(t, "abuse@renoxium.test", "abuse_report")
	if !strings.Contains(m.Text, "asks for card numbers") || m.ReplyTo != "reporter@example.com" {
		t.Fatalf("report email: %+v", m)
	}
	if r, _ := c.PostForm(e.base+"/abuse", url.Values{"csrf": {token}, "address": {""}, "details": {""}}); r.StatusCode != 400 {
		t.Fatalf("empty report: %d", r.StatusCode)
	}
}

func (e *env) accounts() *accounts.Store { return e.acc }

func TestLoginReturnsToPage(t *testing.T) {
	e := newEnv(t)
	e.signup(e.browser(), "nadia@shop.ae")
	for next, want := range map[string]string{
		"/account": "/account", "/billing": "/billing", "/operator": "/operator", "/billing?done=1": "/billing?done=1",
		"/apps/x/payments": "/apps/x/payments", "//evil.example/account": "/", "https://evil.example/": "/",
		"/accounting": "/", "/account/../x": "/",
	} {
		c := e.browser()
		token := e.csrf(c, "/login?next="+url.QueryEscape(next))
		r, _ := c.PostForm(e.base+"/login", url.Values{"csrf": {token}, "email": {"nadia@shop.ae"}, "password": {"correct horse battery"}, "next": {next}})
		if loc := r.Header.Get("Location"); r.StatusCode != 303 || loc != want {
			t.Errorf("next %q: %d %q, want %q", next, r.StatusCode, loc, want)
		}
	}
}

// The running commit and the reason the server is unhealthy tell an
// attacker what to try, so visitors coming through the tunnel see neither.
func TestVersionAndHealthDetailsAreNotPublic(t *testing.T) {
	dir := t.TempDir()
	p, _ := platform.New(platform.Config{DataDir: dir, AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	acc, _ := accounts.Open(filepath.Join(dir, "accounts.db"))
	defer p.Close()
	defer acc.Close()
	site := web.New("https://renoxium.test", acc, p)
	site.TrustedIPHeader = "CF-Connecting-IP"
	site.Version = "abc1234"
	site.Monitor = sickMonitor{}
	get := func(path, remote, visitor string) (int, string) {
		r := httptest.NewRequest("GET", "https://renoxium.test"+path, nil)
		r.RemoteAddr = remote
		if visitor != "" {
			r.Header.Set("CF-Connecting-IP", visitor)
		}
		w := httptest.NewRecorder()
		site.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	if code, body := get("/version", "127.0.0.1:4000", "203.0.113.7"); code != 404 || strings.Contains(body, "abc1234") {
		t.Fatalf("version through the tunnel: %d %s", code, body)
	}
	if code, _ := get("/version", "203.0.113.7:4000", ""); code != 404 {
		t.Fatalf("version from another machine: %d", code)
	}
	if code, body := get("/version", "127.0.0.1:4000", ""); code != 200 || !strings.Contains(body, "abc1234") {
		t.Fatalf("version on the server itself: %d %s", code, body)
	}
	if code, body := get("/healthz", "127.0.0.1:4000", "203.0.113.7"); code != 503 || strings.Contains(body, "disk") {
		t.Fatalf("healthz: %d %s", code, body)
	}
}

type sickMonitor struct{}

func (sickMonitor) Healthy() error         { return errors.New("disk almost full: 1.2% free") }
func (sickMonitor) Status() monitor.Status { return monitor.Status{} }

// Sign-up on the site has to say when an address is taken; after a few
// such answers it stops for that visitor address, whatever they try, so
// the form is no way to test a list of addresses.
func TestSignupDoesNotListAccounts(t *testing.T) {
	e := newEnv(t)
	e.signup(e.browser(), "known@example.com")
	try := func(addr string) int {
		c := e.browser()
		token := e.csrf(c, "/signup")
		r, err := c.PostForm(e.base+"/signup", url.Values{"csrf": {token}, "email": {addr}, "password": {"correct horse battery"}})
		if err != nil {
			t.Fatal(err)
		}
		read(t, r)
		return r.StatusCode
	}
	for i := 0; i < 5; i++ {
		if c := try("known@example.com"); c != 400 {
			t.Fatalf("taken address, try %d: %d", i, c)
		}
	}
	if c := try("known@example.com"); c != 429 {
		t.Fatalf("sixth try with a taken address: %d", c)
	}
	if c := try("free@example.com"); c != 429 {
		t.Fatalf("a free address after the limit told it apart from a taken one: %d", c)
	}
}
