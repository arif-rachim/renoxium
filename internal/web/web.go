// Package web serves the Renoxium site: sign-up and sign-in, a small
// dashboard, the OAuth 2.1 authorization server agents use to act for a user,
// and the builder MCP endpoint.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/email"
	"github.com/arif-rachim/renoxium/internal/mcpserver"
	"github.com/arif-rachim/renoxium/internal/platform"
)

const (
	scope         = "apps"
	sessionCookie = "rx_session"
	csrfCookie    = "rx_csrf"
)

// Site is the http.Handler for the Renoxium main host.
type Site struct {
	base     string // e.g. https://renoxium.com, no trailing slash
	host     string // host name of base, without port
	secure   bool
	accounts *accounts.Store
	platform *platform.Platform
	mcp      http.Handler
	mux      *http.ServeMux
	limiter  *limiter

	// Mailer sends account emails. New sets it to email.Log; set a real
	// sender before serving.
	Mailer email.Sender
	// mailLimiter caps emails per address or user.
	mailLimiter *limiter

	// TrustedIPHeader names the header that carries the visitor's address
	// when the site runs behind a local tunnel, e.g. "CF-Connecting-IP".
	TrustedIPHeader string
}

// New builds the site. base is the public origin of the site.
func New(base string, acc *accounts.Store, p *platform.Platform) *Site {
	base = strings.TrimRight(base, "/")
	host := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	s := &Site{base: base, host: strings.ToLower(host), secure: strings.HasPrefix(base, "https://"), accounts: acc, platform: p,
		mux: http.NewServeMux(), limiter: newLimiter(10, 15*time.Minute), Mailer: email.Log{}, mailLimiter: newLimiter(3, time.Hour)}
	s.mcp = mcpserver.Handler(mcpserver.New(p), s.verify, base+"/.well-known/oauth-protected-resource/mcp")

	s.mux.HandleFunc("GET /{$}", s.home)
	s.mux.HandleFunc("GET /signup", s.signupForm)
	s.mux.HandleFunc("POST /signup", s.signup)
	s.mux.HandleFunc("GET /login", s.loginForm)
	s.mux.HandleFunc("POST /login", s.login)
	s.mux.HandleFunc("POST /logout", s.logout)
	s.mux.HandleFunc("GET /oauth/authorize", s.authorize)
	s.mux.HandleFunc("POST /oauth/authorize", s.approve)
	s.mux.HandleFunc("POST /oauth/token", cors(s.token))
	s.mux.HandleFunc("POST /oauth/register", cors(s.register))
	s.mux.HandleFunc("GET /.well-known/oauth-authorization-server", cors(s.serverMetadata))
	prm := auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource: base + "/mcp", AuthorizationServers: []string{base}, ScopesSupported: []string{scope},
		BearerMethodsSupported: []string{"header"}, ResourceName: "Renoxium",
	})
	s.mux.Handle("GET /.well-known/oauth-protected-resource", prm)
	s.mux.Handle("GET /.well-known/oauth-protected-resource/mcp", prm)
	s.mux.HandleFunc("/mcp", s.mcpEndpoint)
	s.mux.HandleFunc("GET /connect", s.connectPage)
	s.mux.HandleFunc("GET /llms.txt", s.llmsTxt)
	s.registerAdmin()
	s.registerEmailFlows()
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok\n") })
	return s
}

func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The site, with its sign-in forms and session cookies, answers only on
	// its own host, never on an app domain such as unknown.renoxium.app.
	host := strings.ToLower(r.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != s.host && r.URL.Path != "/healthz" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method == http.MethodOptions {
		// CORS preflight. Everything reachable cross-origin uses bearer tokens or
		// no credentials at all, never cookies.
		cors(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/mcp") {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	}
	s.mux.ServeHTTP(w, r)
}

// cors allows browser-based agents to call token, registration and metadata
// endpoints. None of them use cookies, so any origin is safe.
func cors(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Protocol-Version")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id")
		h(w, r)
	}
}

// verify checks an MCP access token.
func (s *Site) verify(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	user, expires, err := s.accounts.AccessToken(token)
	if err != nil {
		return nil, auth.ErrInvalidToken
	}
	return &auth.TokenInfo{UserID: user, Expiration: expires, Scopes: []string{scope}}, nil
}

// --- sessions and CSRF ---

func (s *Site) currentUser(r *http.Request) (accounts.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return accounts.User{}, false
	}
	u, err := s.accounts.SessionUser(c.Value)
	return u, err == nil
}

func (s *Site) setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: s.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge.Seconds())})
}

// csrfToken returns the double-submit token for this browser, setting it if needed.
func (s *Site) csrfToken(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookie); err == nil && len(c.Value) >= 32 {
		return c.Value
	}
	var b [24]byte
	rand.Read(b[:])
	v := base64.RawURLEncoding.EncodeToString(b[:])
	s.setCookie(w, csrfCookie, v, 24*time.Hour)
	return v
}

func checkCSRF(r *http.Request) bool {
	c, err := r.Cookie(csrfCookie)
	return err == nil && len(c.Value) >= 32 && subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.PostFormValue("csrf"))) == 1
}

// safeNext only allows returning to the authorize endpoint or the admin
// panel on this site.
func safeNext(next string) string {
	if strings.ContainsAny(next, "\\\r\n") || strings.Contains(next, "//") {
		return "/"
	}
	if strings.HasPrefix(next, "/oauth/authorize?") || strings.HasPrefix(next, "/apps/") {
		return next
	}
	return "/"
}

// clientIP is the visitor's address. Behind a local tunnel or proxy (such
// as cloudflared) every connection comes from loopback, so the address is
// then taken from TrustedIPHeader. The header is ignored on connections from
// anywhere else, where a visitor could set it to anything.
func (s *Site) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if s.TrustedIPHeader != "" {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			if fwd := strings.TrimSpace(r.Header.Get(s.TrustedIPHeader)); net.ParseIP(fwd) != nil {
				return fwd
			}
		}
	}
	return host
}

// --- pages ---

func (s *Site) home(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		render(w, http.StatusOK, "welcome", map[string]any{"MCP": s.base + "/mcp"})
		return
	}
	apps, err := s.platform.Apps(u.ID)
	if err != nil {
		http.Error(w, "could not load your apps", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "dashboard", map[string]any{"User": u, "Apps": apps, "MCP": s.base + "/mcp", "CSRF": s.csrfToken(w, r)})
}

func (s *Site) signupForm(w http.ResponseWriter, r *http.Request) {
	render(w, http.StatusOK, "signup", map[string]any{"CSRF": s.csrfToken(w, r), "Next": safeNext(r.URL.Query().Get("next"))})
}

func (s *Site) signup(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.PostFormValue("next"))
	if !checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if !s.limiter.allow("signup:" + s.clientIP(r)) {
		http.Error(w, "too many attempts; try again later", http.StatusTooManyRequests)
		return
	}
	u, err := s.accounts.CreateUser(r.PostFormValue("email"), r.PostFormValue("password"))
	if err != nil {
		render(w, http.StatusBadRequest, "signup", map[string]any{"CSRF": s.csrfToken(w, r), "Next": next, "Error": err.Error(), "Email": r.PostFormValue("email")})
		return
	}
	s.sendVerification(r.Context(), u)
	s.startSession(w, r, u, next)
}

func (s *Site) loginForm(w http.ResponseWriter, r *http.Request) {
	render(w, http.StatusOK, "login", map[string]any{"CSRF": s.csrfToken(w, r), "Next": safeNext(r.URL.Query().Get("next"))})
}

func (s *Site) login(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.PostFormValue("next"))
	if !checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	email, _ := accounts.NormalizeEmail(r.PostFormValue("email"))
	if !s.limiter.allow("login:"+s.clientIP(r)) || !s.limiter.allow("login:"+email) {
		http.Error(w, "too many attempts; try again in 15 minutes", http.StatusTooManyRequests)
		return
	}
	u, err := s.accounts.Authenticate(r.PostFormValue("email"), r.PostFormValue("password"))
	if err != nil {
		render(w, http.StatusUnauthorized, "login", map[string]any{"CSRF": s.csrfToken(w, r), "Next": next, "Error": "Wrong email or password.", "Email": r.PostFormValue("email")})
		return
	}
	s.startSession(w, r, u, next)
}

func (s *Site) startSession(w http.ResponseWriter, r *http.Request, u accounts.User, next string) {
	secret, err := s.accounts.CreateSession(u.ID)
	if err != nil {
		http.Error(w, "could not sign you in", http.StatusInternalServerError)
		return
	}
	s.setCookie(w, sessionCookie, secret, accounts.SessionTTL)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Site) logout(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.accounts.DeleteSession(c.Value)
	}
	s.setCookie(w, sessionCookie, "", -time.Second)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- OAuth 2.1 authorization server ---

type authRequest struct {
	client      accounts.Client
	redirectURI string
	state       string
	challenge   string
}

// parseAuthorize validates an authorization request. If the client or
// redirect URI is bad it returns fatal: the user must not be redirected.
func (s *Site) parseAuthorize(v url.Values) (req authRequest, oauthErr string, fatal error) {
	client, err := s.accounts.Client(v.Get("client_id"))
	if err != nil {
		return req, "", errors.New("This agent is not registered with Renoxium. Remove the Renoxium connector in your agent and add it again.")
	}
	if !client.AllowsRedirect(v.Get("redirect_uri")) {
		return req, "", errors.New("The agent asked to return to an address it did not register. Remove the Renoxium connector in your agent and add it again.")
	}
	req = authRequest{client: client, redirectURI: v.Get("redirect_uri"), state: v.Get("state"), challenge: v.Get("code_challenge")}
	switch {
	case v.Get("response_type") != "code":
		return req, "unsupported_response_type", nil
	case v.Get("code_challenge_method") != "S256" || len(req.challenge) != 43:
		return req, "invalid_request", nil // PKCE with S256 is required
	case v.Get("resource") != "" && v.Get("resource") != s.base+"/mcp":
		return req, "invalid_target", nil
	}
	for _, sc := range strings.Fields(v.Get("scope")) {
		if sc != scope {
			return req, "invalid_scope", nil
		}
	}
	return req, "", nil
}

func (s *Site) redirectError(w http.ResponseWriter, r *http.Request, req authRequest, code string) {
	q := url.Values{"error": {code}, "iss": {s.base}}
	if req.state != "" {
		q.Set("state", req.state)
	}
	http.Redirect(w, r, withQuery(req.redirectURI, q), http.StatusFound)
}

func withQuery(uri string, q url.Values) string {
	sep := "?"
	if strings.Contains(uri, "?") {
		sep = "&"
	}
	return uri + sep + q.Encode()
}

func (s *Site) authorize(w http.ResponseWriter, r *http.Request) {
	req, oauthErr, fatal := s.parseAuthorize(r.URL.Query())
	if fatal != nil {
		render(w, http.StatusBadRequest, "error", map[string]any{"Message": fatal.Error()})
		return
	}
	if oauthErr != "" {
		s.redirectError(w, r, req, oauthErr)
		return
	}
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}
	redirect, _ := url.Parse(req.redirectURI)
	render(w, http.StatusOK, "consent", map[string]any{
		"User": u, "Client": req.client.Name, "RedirectHost": redirect.Host, "CSRF": s.csrfToken(w, r),
		"Params": authorizeParams(r.URL.Query()), "Switch": "/login?next=" + url.QueryEscape(r.URL.RequestURI()),
	})
}

// authorizeParams keeps only the OAuth parameters, so a crafted link cannot
// smuggle fields such as decision or csrf into the consent form.
func authorizeParams(q url.Values) url.Values {
	out := url.Values{}
	for _, k := range []string{"response_type", "client_id", "redirect_uri", "state", "scope", "code_challenge", "code_challenge_method", "resource"} {
		if v := q.Get(k); v != "" {
			out.Set(k, v)
		}
	}
	return out
}

func (s *Site) approve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req, oauthErr, fatal := s.parseAuthorize(r.PostForm)
	if fatal != nil {
		render(w, http.StatusBadRequest, "error", map[string]any{"Message": fatal.Error()})
		return
	}
	if !checkCSRF(r) {
		http.Error(w, "the form expired; go back to your agent and connect again", http.StatusForbidden)
		return
	}
	if oauthErr != "" {
		s.redirectError(w, r, req, oauthErr)
		return
	}
	u, ok := s.currentUser(r)
	if !ok {
		http.Error(w, "you were signed out; go back to your agent and connect again", http.StatusUnauthorized)
		return
	}
	if r.PostFormValue("decision") != "allow" {
		s.redirectError(w, r, req, "access_denied")
		return
	}
	code, err := s.accounts.CreateCode(req.client.ID, u.ID, req.redirectURI, req.challenge, scope)
	if err != nil {
		http.Error(w, "could not complete sign-in", http.StatusInternalServerError)
		return
	}
	q := url.Values{"code": {code}, "iss": {s.base}}
	if req.state != "" {
		q.Set("state", req.state)
	}
	http.Redirect(w, r, withQuery(req.redirectURI, q), http.StatusFound)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func (s *Site) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "form body expected")
		return
	}
	f := r.PostForm
	if f.Get("resource") != "" && f.Get("resource") != s.base+"/mcp" {
		oauthError(w, http.StatusBadRequest, "invalid_target", "unknown resource")
		return
	}
	var t accounts.Tokens
	var err error
	switch f.Get("grant_type") {
	case "authorization_code":
		t, err = s.accounts.ExchangeCode(f.Get("code"), f.Get("client_id"), f.Get("redirect_uri"), f.Get("code_verifier"))
	case "refresh_token":
		t, err = s.accounts.Refresh(f.Get("refresh_token"), f.Get("client_id"))
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "use authorization_code or refresh_token")
		return
	}
	if errors.Is(err, accounts.ErrInvalidGrant) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the code or token is invalid, expired or already used")
		return
	}
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "try again")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Site) register(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow("register:" + s.clientIP(r)) {
		oauthError(w, http.StatusTooManyRequests, "invalid_client_metadata", "too many registrations")
		return
	}
	var in oauthex.ClientRegistrationMetadata
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "JSON body expected")
		return
	}
	if in.TokenEndpointAuthMethod != "" && in.TokenEndpointAuthMethod != "none" {
		in.TokenEndpointAuthMethod = "none" // only public PKCE clients; tell the client so
	}
	c, err := s.accounts.RegisterClient(in.ClientName, in.RedirectURIs)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id": c.ID, "client_id_issued_at": time.Now().Unix(), "client_name": c.Name,
		"redirect_uris": c.RedirectURIs, "token_endpoint_auth_method": "none",
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "scope": scope,
	})
}

func (s *Site) serverMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         s.base,
		"authorization_endpoint":                         s.base + "/oauth/authorize",
		"token_endpoint":                                 s.base + "/oauth/token",
		"registration_endpoint":                          s.base + "/oauth/register",
		"scopes_supported":                               []string{scope},
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"code_challenge_methods_supported":               []string{"S256"},
		"authorization_response_iss_parameter_supported": true,
	})
}

// --- rate limiting ---

type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.hits) > 100000 {
		l.hits = map[string][]time.Time{}
	}
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

// --- templates ---

var pages = template.Must(template.New("").Parse(`
{{define "top"}}{{template "head" .}}<main>{{end}}
{{define "wide"}}{{template "head" .}}<main class="wide">{{end}}
{{define "head"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.}} · Renoxium</title><style>
:root{color-scheme:light dark;--fg:#16181d;--muted:#5d6370;--bg:#fafafa;--card:#fff;--line:#e3e5ea;--accent:#2f5bea}
@media (prefers-color-scheme:dark){:root{--fg:#eceef2;--muted:#a1a7b3;--bg:#121418;--card:#1b1e24;--line:#2c3039;--accent:#7f9cff}}
*{box-sizing:border-box}body{margin:0;font:16px/1.5 system-ui,sans-serif;color:var(--fg);background:var(--bg)}
main{max-width:440px;margin:48px auto;padding:0 16px}main.wide{max-width:960px;margin-top:24px}
.crumbs{font-size:14px;margin-bottom:12px}.scroll{overflow-x:auto}table{border-collapse:collapse;width:100%;font-size:14px}
th,td{text-align:left;padding:8px;border-bottom:1px solid var(--line);white-space:nowrap}td a{font-weight:600}
textarea{width:100%;min-height:96px;padding:10px 12px;border:1px solid var(--line);border-radius:8px;background:var(--bg);color:var(--fg);font:inherit}
textarea.json{font-family:ui-monospace,monospace;font-size:14px}.check{display:flex;gap:8px;align-items:center;margin-top:12px}.check input{width:auto}
.ok{color:#2e7d32}.field-error{color:#c62828;font-size:14px;margin-top:4px}.bar{display:flex;gap:8px;flex-wrap:wrap;align-items:center;justify-content:space-between;margin-bottom:12px}
.bar form{display:flex;gap:8px}.bar input{width:220px}.bar button,.bar .button{margin-top:0}.button{display:inline-block;padding:10px 16px;border-radius:8px;background:var(--accent);color:#fff;text-decoration:none}
button.danger{background:transparent;color:#c62828;border:1px solid #c62828}.pager{display:flex;gap:12px;margin-top:12px;font-size:14px}
.connect h2{font-size:18px;margin:24px 0 8px}.connect li{margin:4px 0;overflow-wrap:anywhere}.connect p{overflow-wrap:anywhere}.thumb{display:block;max-width:160px;max-height:160px;border-radius:8px;border:1px solid var(--line);margin:4px 0}.card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:24px}
h1{font-size:22px;margin:0 0 16px}p{margin:0 0 12px}.muted{color:var(--muted);font-size:14px}label{display:block;font-size:14px;margin:12px 0 4px}
input{width:100%;padding:10px 12px;border:1px solid var(--line);border-radius:8px;background:var(--bg);color:var(--fg);font:inherit}
button{margin-top:16px;padding:10px 16px;border:0;border-radius:8px;background:var(--accent);color:#fff;font:inherit;cursor:pointer}
button.secondary{background:transparent;color:var(--fg);border:1px solid var(--line)}.row{display:flex;gap:8px}.error{color:#c62828}
code{font-size:14px;background:var(--bg);border:1px solid var(--line);border-radius:6px;padding:2px 6px;word-break:break-all}
ul{padding-left:20px}a{color:var(--accent)}</style></head><body>{{end}}
{{define "bottom"}}</main></body></html>{{end}}

{{define "welcome"}}{{template "top" "Welcome"}}<div class="card"><h1>Renoxium</h1>
<p>Build and run your online store by chatting with ChatGPT or Claude.</p>
<p class="muted">Add this connector address to your agent:</p><p><code>{{.MCP}}</code></p>
<p><a href="/connect">How to connect ChatGPT or Claude</a></p>
<div class="row"><a href="/signup">Create an account</a> · <a href="/login">Sign in</a></div></div>{{template "bottom"}}{{end}}

{{define "dashboard"}}{{template "top" "Your apps"}}<div class="card"><h1>Your apps</h1>
<p class="muted">Signed in as {{.User.Email}}</p>
{{if not .User.Verified}}<form method="post" action="/verify/resend"><p class="muted">Confirm your email so you can reset your password if you forget it. <input type="hidden" name="csrf" value="{{.CSRF}}"><button class="secondary" style="margin-top:4px">Send the link again</button></p></form>{{end}}
{{if .Apps}}<ul>{{range .Apps}}<li><a href="/apps/{{.App}}">{{.App}}</a> <span class="muted">live v{{.LiveVersion}}, preview v{{.PreviewVersion}}</span></li>{{end}}</ul>
{{else}}<p>No apps yet. Ask your agent: "Build me an online store for my products."</p>{{end}}
<p class="muted">Connector address for your agent:</p><p><code>{{.MCP}}</code> · <a href="/connect">how to connect</a></p>
<form method="post" action="/logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="secondary">Sign out</button></form></div>{{template "bottom"}}{{end}}

{{define "signup"}}{{template "top" "Create account"}}<form class="card" method="post" action="/signup"><h1>Create your account</h1>
{{with .Error}}<p class="error">{{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="next" value="{{.Next}}">
<label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required value="{{.Email}}">
<label for="password">Password (at least 10 characters)</label><input id="password" name="password" type="password" autocomplete="new-password" minlength="10" required>
<button>Create account</button><p class="muted" style="margin-top:16px">Already have an account? <a href="/login?next={{.Next}}">Sign in</a></p></form>{{template "bottom"}}{{end}}

{{define "login"}}{{template "top" "Sign in"}}<form class="card" method="post" action="/login"><h1>Sign in to Renoxium</h1>
{{with .Error}}<p class="error">{{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="next" value="{{.Next}}">
<label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required value="{{.Email}}">
<label for="password">Password</label><input id="password" name="password" type="password" autocomplete="current-password" required>
<button>Sign in</button><p class="muted" style="margin-top:16px"><a href="/forgot">Forgot your password?</a></p><p class="muted">New here? <a href="/signup?next={{.Next}}">Create an account</a></p></form>{{template "bottom"}}{{end}}

{{define "consent"}}{{template "top" "Allow access"}}<form class="card" method="post" action="/oauth/authorize"><h1>Allow {{.Client}}?</h1>
<p><strong>{{.Client}}</strong> wants to create, change and publish your Renoxium apps, and read their logs.</p>
<p class="muted">After you allow, you return to <strong>{{.RedirectHost}}</strong>. Only allow if you just connected Renoxium in your agent.</p>
<p class="muted">Signed in as {{.User.Email}} · <a href="{{.Switch}}">use another account</a></p>
<input type="hidden" name="csrf" value="{{.CSRF}}">
{{range $k, $v := .Params}}<input type="hidden" name="{{$k}}" value="{{index $v 0}}">{{end}}
<div class="row"><button name="decision" value="allow">Allow</button><button class="secondary" name="decision" value="deny">Cancel</button></div></form>{{template "bottom"}}{{end}}

{{define "admin_app"}}{{template "wide" .Info.App}}<p class="crumbs"><a href="/">Your apps</a></p><div class="card"><h1>{{.Info.App}}</h1>
<p><a href="{{.Info.LiveURL}}">{{.Info.LiveURL}}</a> <span class="muted">live version {{.Info.LiveVersion}}</span></p>
<p class="muted">Preview (version {{.Info.PreviewVersion}}): <a href="{{.Info.PreviewURL}}">{{.Info.PreviewURL}}</a></p>
{{if .Tables}}<h1 style="margin-top:24px">Data</h1><ul>{{range .Tables}}<li><a href="/apps/{{$.Info.App}}/t/{{.Name}}">{{.Label}}</a> <span class="muted">{{.Rows}} rows</span></li>{{end}}</ul>
{{else}}<p>{{if .Info.LiveVersion}}This app has no data tables.{{else}}Publish the app from your agent to manage its data here.{{end}}</p>{{end}}
{{if .Problems}}<h1 style="margin-top:24px">Recent errors</h1><p class="muted">Ask your agent to "check the Renoxium logs and fix the errors".</p><ul>{{range .Problems}}<li><span class="muted">{{.Time}}</span> {{.Message}}</li>{{end}}</ul>{{end}}
</div>{{template "bottom"}}{{end}}

{{define "admin_list"}}{{template "wide" .Label}}<p class="crumbs"><a href="/">Your apps</a> / <a href="/apps/{{.App}}">{{.App}}</a></p><div class="card">
<div class="bar"><h1 style="margin:0">{{.Label}} <span class="muted">({{.Total}})</span></h1>
<form method="get"><input name="q" value="{{.Query}}" placeholder="Search" aria-label="Search"><button class="secondary">Search</button></form>
<a class="button" href="/apps/{{.App}}/t/{{.Table}}/new">Add</a></div>
{{if .Cells}}<div class="scroll"><table><thead><tr>{{range .Columns}}<th>{{.Label}}</th>{{end}}</tr></thead><tbody>
{{range $i, $row := .Cells}}<tr>{{range $j, $v := $row}}<td>{{if eq $j 0}}<a href="/apps/{{$.App}}/t/{{$.Table}}/{{index $.IDs $i}}">{{$v}}</a>{{else}}{{$v}}{{end}}</td>{{end}}</tr>{{end}}
</tbody></table></div>
{{if gt .Pages 1}}<div class="pager">{{if gt .Page 1}}<a href="?q={{.Query}}&page={{.Prev}}">Newer</a>{{end}}<span class="muted">Page {{.Page}} of {{.Pages}}</span>{{if lt .Page .Pages}}<a href="?q={{.Query}}&page={{.Next}}">Older</a>{{end}}</div>{{end}}
{{else}}<p>{{if .Query}}Nothing matches "{{.Query}}".{{else}}No rows yet.{{end}}</p>{{end}}</div>{{template "bottom"}}{{end}}

{{define "admin_form"}}{{template "top" .Label}}<p class="crumbs"><a href="/apps/{{.App}}">{{.App}}</a> / <a href="/apps/{{.App}}/t/{{.Table}}">{{.Label}}</a></p>
<form class="card" method="post"{{if .HasImage}} enctype="multipart/form-data"{{end}}><h1>{{if .ID}}{{.Label}} #{{.ID}}{{else}}New {{.Label}}{{end}}</h1>
{{if .Saved}}<p class="ok">Saved.</p>{{end}}{{if .HasErrors}}<p class="error">Check the highlighted fields.{{with .Error}} {{.}}{{end}}</p>{{end}}
{{with .Created}}<p class="muted">Created {{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}">
{{range .Fields}}{{if eq .Input "checkbox"}}<label class="check"><input type="checkbox" name="{{.Name}}" value="1"{{if .Checked}} checked{{end}}> {{.Label}}</label>
{{else}}<label for="f-{{.Name}}">{{.Label}}{{if .Required}} *{{end}}</label>
{{if eq .Input "image"}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">
{{if .Value}}<img class="thumb" src="{{$.LiveURL}}{{.Value}}" alt=""><label class="check"><input type="checkbox" name="{{.Name}}__remove" value="1"> Remove image</label>{{end}}
<input id="f-{{.Name}}" name="{{.Name}}__upload" type="file" accept="image/jpeg,image/png,image/gif,image/webp,image/avif">
{{else if eq .Input "textarea"}}<textarea id="f-{{.Name}}" name="{{.Name}}"{{if eq .Type "json"}} class="json"{{end}}{{if .Required}} required{{end}}>{{.Value}}</textarea>
{{else}}<input id="f-{{.Name}}" name="{{.Name}}" type="{{.Input}}" value="{{.Value}}"{{if eq .Type "real"}} step="any"{{end}}{{if eq .Type "integer"}} step="1"{{end}}{{if .Required}} required{{end}}>{{end}}{{end}}
{{with .Error}}<p class="field-error">{{.}}</p>{{end}}{{end}}
<button>Save</button></form>
{{if .ID}}<form method="post" action="/apps/{{.App}}/t/{{.Table}}/{{.ID}}/delete" style="margin-top:16px"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="danger">Delete {{.Label}} #{{.ID}}</button></form>{{end}}
{{template "bottom"}}{{end}}

{{define "connect"}}{{template "wide" "Connect"}}<div class="card connect">{{range .Blocks}}{{if eq .Kind "h1"}}<h1>{{.Text}}</h1>
{{else if eq .Kind "h2"}}<h2>{{.Text}}</h2>{{else if eq .Kind "ul"}}<ul>{{range .Items}}<li>{{.}}</li>{{end}}</ul>
{{else if eq .Kind "ol"}}<ol>{{range .Items}}<li>{{.}}</li>{{end}}</ol>{{else}}<p>{{.Text}}</p>{{end}}{{end}}</div>{{template "bottom"}}{{end}}

{{define "forgot"}}{{template "top" "Forgot password"}}<form class="card" method="post" action="/forgot"><h1>Forgot your password?</h1>
<p class="muted">Enter your email and we will send you a link to choose a new password.</p>
<input type="hidden" name="csrf" value="{{.CSRF}}"><label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required>
<button>Send the link</button></form>{{template "bottom"}}{{end}}

{{define "reset"}}{{template "top" "New password"}}<form class="card" method="post" action="/reset"><h1>Choose a new password</h1>
{{with .Email}}<p class="muted">For {{.}}</p>{{end}}{{with .Error}}<p class="error">{{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="token" value="{{.Token}}">
<label for="password">New password (at least 10 characters)</label><input id="password" name="password" type="password" autocomplete="new-password" minlength="10" required>
<button>Save and sign in</button><p class="muted" style="margin-top:16px">You will be signed out on every other device, and agents will need to connect again.</p></form>{{template "bottom"}}{{end}}

{{define "notice"}}{{template "top" .Title}}<div class="card"><h1>{{.Title}}</h1><p>{{.Message}}</p>{{with .Link}}<p><a href="{{.}}">Continue</a></p>{{end}}</div>{{template "bottom"}}{{end}}

{{define "error"}}{{template "top" "Error"}}<div class="card"><h1>Something is wrong</h1><p>{{.Message}}</p></div>{{template "bottom"}}{{end}}
`))

func render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	pages.ExecuteTemplate(w, name, data)
}
