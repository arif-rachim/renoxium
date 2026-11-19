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
	"github.com/arif-rachim/renoxium/internal/monitor"
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
	// apiLimiter caps, per account, pages that call Stripe or Cloudflare with
	// the platform's keys, so one account cannot use up their rate limits.
	apiLimiter *limiter
	// takenLimiter counts "this email already has an account" answers per address.
	takenLimiter *limiter
	bg           sync.WaitGroup // emails sent after the request was answered
	// accountLimiter caps sign-in attempts for one account from everywhere.
	accountLimiter *limiter

	// Monitor, when set, adds server health to /healthz and the operator panel.
	Monitor interface {
		Healthy() error
		Status() monitor.Status
	}
	// Mailer sends account emails. New sets it to email.Log; set a real
	// sender before serving.
	Mailer email.Sender
	// mailLimiter caps emails per address or user.
	mailLimiter *limiter

	// AbuseEmail receives abuse reports from /abuse. Empty only logs them.
	AbuseEmail string

	// Legal fills the company details on /privacy and /terms.
	Legal Legal

	// Operators are the emails allowed into /operator (confirmed accounts only).
	Operators []string

	// Billing charges accounts for Renoxium plans.
	Billing Billing

	// StripeWebhookSecret verifies events posted to /stripe/webhook.
	StripeWebhookSecret string

	// Version is the commit the server was built from, served at /version
	// so a deploy can be checked from outside.
	Version string

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
		mux: http.NewServeMux(), limiter: newLimiter(10, 15*time.Minute), accountLimiter: newLimiter(100, time.Hour), takenLimiter: newLimiter(5, time.Hour), apiLimiter: newLimiter(30, 10*time.Minute), Mailer: email.Log{}, mailLimiter: newLimiter(3, time.Hour)}
	s.mcp = mcpserver.Handler(mcpserver.New(p, mcpserver.Options{ConfirmPublish: s.publishLink}), s.verify, base+"/.well-known/oauth-protected-resource/mcp")

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
	s.registerPayments()
	s.registerBilling()
	s.registerAccount()
	s.registerOperator()
	s.registerLanding()
	s.registerStaff()
	s.registerDomain()
	s.registerAlerts()
	s.registerConnections()
	s.registerAbuse()
	s.registerGallery()
	s.registerBrand()
	s.registerStatic()
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if s.Monitor != nil {
			if err := s.Monitor.Healthy(); err != nil {
				// The reason (such as how full the disk is) is for the operator:
				// the monitor emails it and the operator panel shows it.
				http.Error(w, "unhealthy", http.StatusServiceUnavailable)
				return
			}
		}
		io.WriteString(w, "ok\n")
	})
	// The running commit tells an attacker which fixes are deployed, so only
	// the operator sees it: signed in, or asking on the server itself.
	s.mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		if u, ok := s.currentUser(r); !s.onServer(r) && !(ok && u.Verified && s.isOperator(u.Email)) {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, s.Version+"\n")
	})
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
	if host != s.host && r.URL.Path != "/healthz" && r.URL.Path != "/version" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if s.secure {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	// Forms that rely on the session cookie must come from this site's own
	// pages. Bearer-token and anonymous endpoints are meant for other origins.
	if r.Method == http.MethodPost && platform.CrossSite(r) &&
		r.URL.Path != "/oauth/token" && r.URL.Path != "/oauth/register" && r.URL.Path != "/mcp" {
		http.Error(w, "this form must be sent from the Renoxium site", http.StatusForbidden)
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
	c, err := r.Cookie(s.cookieName(sessionCookie))
	if err != nil {
		return accounts.User{}, false
	}
	u, err := s.accounts.SessionUser(c.Value)
	return u, err == nil
}

// cookieName adds the __Host- prefix on HTTPS, so no other host (such as
// a subdomain) can set or overwrite the cookie.
func (s *Site) cookieName(name string) string {
	if s.secure {
		return "__Host-" + name
	}
	return name
}

func (s *Site) setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(name), Value: value, Path: "/", HttpOnly: true, Secure: s.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge.Seconds())})
}

// csrfToken returns the double-submit token for this browser, setting it if needed.
func (s *Site) csrfToken(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(s.cookieName(csrfCookie)); err == nil && len(c.Value) >= 32 {
		return c.Value
	}
	var b [24]byte
	rand.Read(b[:])
	v := base64.RawURLEncoding.EncodeToString(b[:])
	s.setCookie(w, csrfCookie, v, 24*time.Hour)
	return v
}

func (s *Site) checkCSRF(r *http.Request) bool {
	c, err := r.Cookie(s.cookieName(csrfCookie))
	return err == nil && len(c.Value) >= 32 && subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.PostFormValue("csrf"))) == 1
}

// safeNext only allows returning to the authorize endpoint or a page of
// the signed-in site (admin panel, account, billing, operator).
func safeNext(next string) string {
	if strings.ContainsAny(next, "\\\r\n") || strings.Contains(next, "//") {
		return "/"
	}
	if strings.HasPrefix(next, "/oauth/authorize?") || strings.HasPrefix(next, "/apps/") {
		return next
	}
	for _, page := range []string{"/account", "/billing", "/operator"} {
		if next == page || strings.HasPrefix(next, page+"?") {
			return next
		}
	}
	return "/"
}

// onServer reports whether a request was made on the server itself rather
// than through the tunnel, which always adds the visitor's address.
func (s *Site) onServer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && (s.TrustedIPHeader == "" || r.Header.Get(s.TrustedIPHeader) == "")
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
				return platform.RateKey(fwd)
			}
		}
	}
	return platform.RateKey(host)
}

// --- pages ---

func (s *Site) home(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		renderLanding(w, s.landingData())
		return
	}
	apps, err := s.platform.Apps(u.ID)
	if err != nil {
		http.Error(w, "could not load your apps", http.StatusInternalServerError)
		return
	}
	plan := map[string]string{accounts.PlanPro: "Pro", accounts.PlanBusiness: "Business"}[s.accounts.Plan(u.ID)]
	if plan == "" {
		plan = "Free"
	}
	var shared []platform.AppInfo
	if u.Verified {
		shared, _ = s.platform.StaffApps(u.Email)
	}
	var cards, sharedCards []appCard
	for _, a := range apps {
		cards = append(cards, s.appCard(r.Context(), a))
	}
	for _, a := range shared {
		sharedCards = append(sharedCards, s.appCard(r.Context(), a))
	}
	render(w, http.StatusOK, "dashboard", map[string]any{"User": u, "Apps": cards, "MCP": s.base + "/mcp", "CSRF": s.csrfToken(w, r), "Plan": plan,
		"Shared": sharedCards, "AppLimit": platform.AppsPerPlan[s.accounts.Plan(u.ID)]})
}

func (s *Site) signupForm(w http.ResponseWriter, r *http.Request) {
	render(w, http.StatusOK, "signup", map[string]any{"CSRF": s.csrfToken(w, r), "Next": safeNext(r.URL.Query().Get("next"))})
}

func (s *Site) signup(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.PostFormValue("next"))
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if !s.limiter.allow("signup:" + s.clientIP(r)) {
		http.Error(w, "too many attempts; try again later", http.StatusTooManyRequests)
		return
	}
	// See platform.signup: a few "this email already has an account"
	// answers stop sign-up from that address for an hour.
	dupKey := "signup-taken:" + s.clientIP(r)
	if s.takenLimiter.count(dupKey) >= s.takenLimiter.max {
		http.Error(w, "too many attempts; try again in an hour", http.StatusTooManyRequests)
		return
	}
	u, err := s.accounts.CreateUser(r.PostFormValue("email"), r.PostFormValue("password"))
	if errors.Is(err, accounts.ErrEmailTaken) {
		s.takenLimiter.allow(dupKey)
	}
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
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	email, _ := accounts.NormalizeEmail(r.PostFormValue("email"))
	// Per address, and per account from that address. The per-account limit
	// over all addresses is much higher, so a stranger guessing someone's
	// password cannot lock them out, but a wide guessing attack still stops.
	ip := s.clientIP(r)
	if !s.limiter.allow("login:"+ip) || !s.limiter.allow("login:"+email+":"+ip) || !s.accountLimiter.allow("login:"+email) {
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
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if c, err := r.Cookie(s.cookieName(sessionCookie)); err == nil {
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
		"Warning": impersonation(req.client.Name, redirect.Hostname()),
		"Params":  authorizeParams(r.URL.Query()), "Switch": "/login?next=" + url.QueryEscape(r.URL.RequestURI()),
	})
}

// knownAgents maps words in well-known agent names to the domains their
// redirects really use. Anyone can register a client with any name.
var knownAgents = map[string][]string{
	"claude":  {"claude.ai", "claude.com", "anthropic.com"},
	"chatgpt": {"chatgpt.com", "openai.com"},
	"openai":  {"chatgpt.com", "openai.com"},
}

// impersonation returns a warning when a client's name claims a well-known
// agent but its redirect goes to a host that agent does not use.
func impersonation(name, host string) string {
	host = strings.ToLower(host)
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return ""
	}
	lower := strings.ToLower(name)
	for word, domains := range knownAgents {
		if !strings.Contains(lower, word) {
			continue
		}
		for _, d := range domains {
			if host == d || strings.HasSuffix(host, "."+d) {
				return ""
			}
		}
		return "This agent calls itself \"" + name + "\", but it returns to " + host +
			", which does not belong to it. It may be someone pretending. Choose Cancel unless you are sure."
	}
	return ""
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
	if !s.checkCSRF(r) {
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

// count is how many uses of key fall in the current window.
func (l *limiter) count(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, t := range l.hits[key] {
		if time.Since(t) < l.window {
			n++
		}
	}
	return n
}

// WaitBackground waits for emails still being sent for requests that were
// already answered. Tests use it.
func (s *Site) WaitBackground() { s.bg.Wait() }

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.hits) > 100000 {
		// Drop only keys with no recent hits; clearing everything would
		// reset the limits of whoever filled the map.
		for k, hits := range l.hits {
			if len(hits) == 0 || now.Sub(hits[len(hits)-1]) >= l.window {
				delete(l.hits, k)
			}
		}
		if len(l.hits) > 200000 {
			return false
		}
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

var pages = template.Must(template.New("").Parse(brandDefines + `
{{define "top"}}{{template "head" .}}<body>{{template "nav"}}<main>{{end}}
{{define "wide"}}{{template "head" .}}<body>{{template "nav"}}<main class="wide">{{end}}
{{define "auth"}}{{template "head" .}}<body class="auth"><header class="bar-top"><div class="in"><a class="logo" href="/" aria-label="Renoxium home">{{template "brandlogo"}}</a><nav aria-label="Main"><a href="/templates">Templates</a><a href="/connect">Connect</a></nav></div></header><main>{{end}}
{{define "nav"}}<header class="bar-top"><div class="in"><a class="logo" href="/" aria-label="Renoxium home">{{template "brandlogo"}}</a><nav aria-label="Main"><a href="/">Your apps</a><a href="/templates">Templates</a><a href="/billing">Plan</a><a href="/account">Account</a><a href="/connect">Help</a></nav></div></header>{{end}}
{{define "head"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.}} · Renoxium</title>{{template "icons"}}{{template "fonts"}}<style>
:root{color-scheme:light dark;--brand:#13203a;--fg:#13203a;--muted:#4b5870;--bg:#eef1f5;--card:#fff;--line:#d6dce5;--accent:#13203a;--accent-ink:#fff;--accent-soft:#e6ebf3;--gold:#b8892e;--sun:#b8892e;--sun-soft:#f5ecd9;--live:#1e8a50;--bad:#b3261e;--radius:14px}
@media (prefers-color-scheme:dark){:root{--brand:#e7ecf4;--fg:#e7ecf4;--muted:#a3afc4;--bg:#0b1424;--card:#121d33;--line:#263552;--accent:#e7ecf4;--accent-ink:#0b1424;--accent-soft:#1b2a47;--gold:#d2a955;--sun:#d2a955;--sun-soft:#3a2f17;--live:#5fd394;--bad:#ff8a80}}
*{box-sizing:border-box}html{-webkit-text-size-adjust:100%}
body{margin:0;font:16px/1.55 "Readex Pro",system-ui,sans-serif;color:var(--fg);background:var(--bg)}
a{color:var(--accent);text-decoration-color:var(--muted);text-underline-offset:3px}a:hover{text-decoration-color:var(--gold);text-decoration-thickness:2px}a:focus-visible,button:focus-visible,input:focus-visible,select:focus-visible,textarea:focus-visible,summary:focus-visible{outline:3px solid var(--accent);outline-offset:2px}
.bar-top{background:var(--card);border-bottom:1px solid var(--line)}.bar-top .in{max-width:1120px;margin:0 auto;padding:12px 20px;display:flex;align-items:center;gap:8px 24px;flex-wrap:wrap}
.logo{display:inline-flex;align-items:center;gap:8px;font-weight:600;font-size:20px;color:var(--brand);text-decoration:none;letter-spacing:-.01em;margin-inline-end:auto}.logo .mark{height:26px;width:auto;flex:none}
.bar-top nav{display:flex;gap:4px 18px;flex-wrap:wrap;font-size:15px}.bar-top nav a{color:var(--fg);text-decoration:none;padding:4px 0}.bar-top nav a:hover{color:var(--accent)}
main{max-width:600px;margin:32px auto 64px;padding:0 20px}main.wide{max-width:1120px}body.auth main{max-width:440px;margin-top:56px}
.crumbs{font-size:14px;margin:0 0 14px;color:var(--muted)}.crumbs a{color:var(--muted)}
.card{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:24px}.card+.card{margin-top:16px}
h1{font-size:24px;line-height:1.25;margin:0 0 14px;font-weight:600;letter-spacing:-.01em}h2{font-size:18px;margin:0 0 10px;font-weight:600}
p{margin:0 0 12px}.muted{color:var(--muted);font-size:14px}ul{padding-inline-start:20px}
label{display:block;font-size:14px;font-weight:500;margin:14px 0 6px}
input,select,textarea{width:100%;padding:11px 12px;border:1px solid var(--line);border-radius:10px;background:var(--bg);color:var(--fg);font:inherit}
textarea{min-height:96px}textarea.json{font-family:ui-monospace,monospace;font-size:14px}
button,.button{display:inline-block;margin-top:18px;padding:11px 18px;border:1px solid var(--accent);border-radius:8px;background:var(--accent);color:var(--accent-ink);font:inherit;font-weight:500;cursor:pointer;text-decoration:none;line-height:1.3}
button.secondary,.button.secondary{background:transparent;color:var(--fg);border-color:var(--line)}button.danger{background:transparent;color:var(--bad);border-color:var(--bad)}
.scroll{overflow-x:auto;margin:0 -4px}table{border-collapse:collapse;width:100%;font-size:14px}
th{text-align:start;font-weight:500;color:var(--muted);font-size:13px;padding:8px 10px;border-bottom:1px solid var(--line);white-space:nowrap}
td{text-align:start;padding:10px;border-bottom:1px solid var(--line);white-space:nowrap}tbody tr:hover{background:var(--accent-soft)}td a{font-weight:600}
.help{margin:4px 0 0;font-size:13px}.check{display:flex;gap:8px;align-items:center;margin-top:12px;font-weight:400}.check input{width:auto}
.ok{color:var(--live)}.error{color:var(--bad)}.field-error{color:var(--bad);font-size:14px;margin-top:4px}
.note{border:1px solid var(--line);border-radius:10px;padding:12px 14px;margin:12px 0;background:var(--card)}.note ul{margin:0 0 8px;padding-inline-start:20px}
.bar{display:flex;gap:10px;flex-wrap:wrap;align-items:center;justify-content:space-between;margin-bottom:14px}
.bar form{display:flex;gap:8px}.bar input{width:220px}.bar button,.bar .button{margin-top:0}
.pager{display:flex;gap:14px;margin-top:14px;font-size:14px;align-items:center}
.connect h2{font-size:18px;margin:28px 0 8px}.connect li{margin:4px 0;overflow-wrap:anywhere}.connect p{overflow-wrap:anywhere}
.plans{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px;margin:12px 0}.plan{border:1px solid var(--line);border-radius:12px;padding:16px}
.plan h2{margin:0 0 4px;font-size:18px}.plan .price{font-size:20px;font-weight:600;margin:0}.plan ul{padding-inline-start:18px;font-size:14px}
.tiles{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:12px;margin:8px 0 12px}.tile{border:1px solid var(--line);border-radius:12px;padding:14px;background:var(--card)}
.tile .big{font-size:22px;font-weight:600;margin:2px 0;letter-spacing:-.01em;font-variant-numeric:tabular-nums}
form.inline{display:flex;gap:6px;align-items:center}form.inline button{margin:0;padding:6px 10px}form.inline select,form.inline input{width:auto;padding:6px 8px}
.report{border-top:1px solid var(--line);padding-top:8px;margin-top:8px}.report.done{opacity:.6}
ul.plain{list-style:none;padding:0}ul.plain li{display:flex;justify-content:space-between;align-items:center;gap:8px;padding:8px 0;border-bottom:1px solid var(--line)}
.thumb{display:block;max-width:160px;max-height:160px;border-radius:8px;border:1px solid var(--line);margin:4px 0}
.row{display:flex;gap:8px;flex-wrap:wrap}
code{font-size:13.5px;background:var(--bg);border:1px solid var(--line);border-radius:6px;padding:2px 6px;word-break:break-all}
.pill{display:inline-flex;align-items:center;gap:6px;font-size:13px;font-weight:500;padding:3px 10px;border-radius:999px;background:var(--bg);border:1px solid var(--line);color:var(--fg);white-space:nowrap}
.pill::before{content:"";width:8px;height:8px;border-radius:50%;background:var(--muted)}.pill.live::before{background:var(--live)}.pill.wait{background:var(--sun-soft);border-color:transparent}.pill.wait::before{background:var(--sun)}
.pill.plain::before{display:none}
.tables{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:10px}
.tables a{display:block;background:var(--card);border:1px solid var(--line);border-radius:12px;padding:14px 16px;text-decoration:none;color:var(--fg)}
.tables a:hover{border-color:var(--accent)}.tables strong{display:block;font-weight:500}.tables span{font-size:14px;color:var(--muted);font-variant-numeric:tabular-nums}
@media (max-width:640px){main{margin-top:20px}.card{padding:18px}.bar input{width:100%}.bar form{flex:1}}
@media (prefers-reduced-motion:reduce){*{transition:none!important;animation:none!important}}
</style></head>{{end}}
{{define "fonts"}}<link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin><link href="https://fonts.googleapis.com/css2?family=Readex+Pro:wght@300;400;500;600;700&display=swap" rel="stylesheet">{{end}}
{{define "bottom"}}</main></body></html>{{end}}

{{define "welcome"}}{{template "auth" "Welcome"}}<div class="card"><h1>Renoxium</h1>
<p>Build and run your online store by chatting with ChatGPT or Claude.</p>
<p class="muted">Add this connector address to your agent:</p><p><code>{{.MCP}}</code></p>
<p><a href="/connect">How to connect ChatGPT or Claude</a></p>
<div class="row"><a href="/signup">Create an account</a> · <a href="/login">Sign in</a></div>
<p class="muted" style="margin-top:16px"><a href="/abuse">Report an app</a></p></div>{{template "bottom"}}{{end}}

{{define "abuse"}}{{template "top" "Report an app"}}<form class="card" method="post" action="/abuse"><h1>Report an app</h1>
<p class="muted">Tell us about an app on Renoxium that is used for phishing, scams, malware or anything illegal.</p>
{{with .Error}}<p class="error">{{.}}</p>{{end}}<input type="hidden" name="csrf" value="{{.CSRF}}">
<label for="address">App address</label><input id="address" name="address" required placeholder="https://name.renoxium.app" value="{{.Address}}">
<label for="details">What is wrong?</label><textarea id="details" name="details" required>{{.Details}}</textarea>
<label for="contact">Your email (optional, if you want an answer)</label><input id="contact" name="contact" type="email" value="{{.Contact}}">
<button>Send report</button></form>{{template "bottom"}}{{end}}

{{define "dashboard"}}{{template "wide" "Your apps"}}<style>
.dash{display:grid;gap:24px}@media(min-width:900px){.dash{grid-template-columns:minmax(0,1fr) 320px;align-items:start}}
.head{display:flex;flex-wrap:wrap;gap:8px 16px;align-items:baseline;justify-content:space-between;margin-bottom:16px}.head h1{margin:0}
.apps{display:grid;gap:16px}@media(min-width:640px){.apps{grid-template-columns:1fr 1fr}}
.app{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:20px;display:flex;flex-direction:column;gap:12px}
.app h2{font-size:20px;margin:0}.app h2 a{color:var(--fg);text-decoration:none}.app h2 a:hover{color:var(--accent)}
.app .top{display:flex;justify-content:space-between;gap:8px;align-items:flex-start;flex-wrap:wrap}.app .host{font-size:14px;color:var(--muted);overflow-wrap:anywhere;margin:2px 0 0}
.figs{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;border-block:1px solid var(--line);padding:12px 0;margin:0}
.figs div{min-width:0}.figs dt{font-size:12.5px;color:var(--muted)}.figs dd{margin:0;font-weight:600;font-size:16px;font-variant-numeric:tabular-nums;overflow-wrap:anywhere}.figs dd.bad{color:var(--bad)}
.app .acts{display:flex;gap:6px 16px;flex-wrap:wrap;align-items:center;font-size:15px}.app .acts .button{margin:0;padding:8px 14px}
.draft{font-size:14px;color:var(--muted);margin:0}
.empty{background:var(--card);border:1px dashed var(--line);border-radius:var(--radius);padding:24px}.empty h2{font-size:20px}
.side .card{padding:20px}.side h2{font-size:16px}.side ol{padding-inline-start:20px;margin:0 0 12px;font-size:15px}.side li{margin:4px 0}
.side input{font-size:13.5px;font-family:ui-monospace,monospace;background:var(--bg)}
.say{background:var(--accent-soft);border-radius:12px;padding:10px 12px;font-size:15px;margin:8px 0 0}
</style>
<div class="head"><h1>Your apps</h1><p class="muted" style="margin:0">{{.User.Email}} · <a href="/billing">{{.Plan}} plan</a>, {{len .Apps}} of {{.AppLimit}} app{{if ne .AppLimit 1}}s{{end}} · <a href="/account">Account</a></p></div>
{{if not .User.Verified}}<form class="note" method="post" action="/verify/resend" style="display:flex;gap:12px;align-items:center;justify-content:space-between;flex-wrap:wrap"><span>Confirm your email to publish apps and to reset your password if you forget it.</span><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="secondary" style="margin:0">Send the link again</button></form>{{end}}
<div class="dash"><div>
{{if .Apps}}<div class="apps">{{range .Apps}}{{template "app_card" .}}{{end}}</div>
{{else}}<div class="empty"><h2>No apps yet</h2><p>Connect Renoxium to ChatGPT or Claude, then ask for what you need. Your assistant starts from a finished template and shows you a preview before anything goes live.</p>
<p class="say">"Build me an online store for my perfume shop in Dubai."</p><p class="say">"Make a booking site for my salon with three stylists."</p>
<p style="margin-top:16px"><a href="/templates">See the templates you can start from</a></p></div>{{end}}
{{if .Shared}}<h2 style="margin:28px 0 12px">Shared with you</h2><div class="apps">{{range .Shared}}{{template "app_card" .}}{{end}}</div>{{end}}
</div><aside class="side"><div class="card"><h2>Build or change an app</h2>
<ol><li>Add Renoxium to your AI assistant once, with this address:</li></ol>
<input readonly value="{{.MCP}}" aria-label="Connector address" onfocus="this.select()">
<ol start="2"><li>Ask for a new app or a change.</li><li>Check the preview, then publish.</li></ol>
<p class="muted" style="margin:0"><a href="/connect">Step-by-step for ChatGPT and Claude</a></p></div>
<div class="card"><h2>Start from a template</h2><p class="muted">Online store, appointments, home services, tours and rentals, quotes, property listings. Try each one live.</p><a href="/templates">Browse templates</a></div>
<form method="post" action="/logout" style="margin-top:12px"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="secondary" style="margin:0;width:100%">Sign out</button></form></aside></div>{{template "bottom"}}{{end}}

{{define "app_card"}}<article class="app"><div class="top"><div><h2><a href="/apps/{{.App}}">{{.App}}</a></h2>{{if .LiveVersion}}<p class="host">{{.Host}}</p>{{end}}</div>
<span class="pill {{.Status}}">{{.StatusText}}</span></div>
{{if .LiveVersion}}<dl class="figs"><div><dt>Sales, 30 days</dt><dd>{{if .SalesCount}}{{.Sales}}{{else}}None yet{{end}}</dd></div><div><dt>Customers</dt><dd>{{.Visitors}}</dd></div>
<div><dt>Recent errors</dt><dd{{if .Errors}} class="bad"{{end}}>{{.Errors}}</dd></div></dl>
{{else}}<p class="draft">Preview version {{.PreviewVersion}} is ready to check. Publish it when you are happy and it goes live at {{.Host}}.</p>{{end}}
<div class="acts"><a class="button" href="/apps/{{.App}}">Manage</a>{{if .LiveVersion}}<a href="{{.LiveURL}}/">Open site</a>{{end}}<a href="/apps/{{.App}}/preview">Preview</a></div></article>{{end}}

{{define "signup"}}{{template "auth" "Create account"}}<form class="card" method="post" action="/signup"><h1>Create your account</h1>
{{with .Error}}<p class="error">{{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="next" value="{{.Next}}">
<label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required value="{{.Email}}">
<label for="password">Password (at least 10 characters)</label><input id="password" name="password" type="password" autocomplete="new-password" minlength="10" required>
<button>Create account</button><p class="muted" style="margin-top:16px">Already have an account? <a href="/login?next={{.Next}}">Sign in</a></p></form>{{template "bottom"}}{{end}}

{{define "login"}}{{template "auth" "Sign in"}}<form class="card" method="post" action="/login"><h1>Sign in to Renoxium</h1>
{{with .Error}}<p class="error">{{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="next" value="{{.Next}}">
<label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required value="{{.Email}}">
<label for="password">Password</label><input id="password" name="password" type="password" autocomplete="current-password" required>
<button>Sign in</button><p class="muted" style="margin-top:16px"><a href="/forgot">Forgot your password?</a></p><p class="muted">New here? <a href="/signup?next={{.Next}}">Create an account</a></p></form>{{template "bottom"}}{{end}}

{{define "consent"}}{{template "auth" "Allow access"}}<form class="card" method="post" action="/oauth/authorize"><h1>Allow {{.Client}}?</h1>
<p><strong>{{.Client}}</strong> wants to create, change and publish your Renoxium apps, and read their logs.</p>
{{with .Warning}}<p class="error"><strong>Warning:</strong> {{.}}</p>{{end}}
<p>After you allow, you return to <code>{{.RedirectHost}}</code>. Only allow if you just connected Renoxium in your agent and you recognise this address.</p>
<p class="muted">Signed in as {{.User.Email}} · <a href="{{.Switch}}">use another account</a></p>
<input type="hidden" name="csrf" value="{{.CSRF}}">
{{range $k, $v := .Params}}<input type="hidden" name="{{$k}}" value="{{index $v 0}}">{{end}}
<div class="row"><button name="decision" value="allow">Allow</button><button class="secondary" name="decision" value="deny">Cancel</button></div></form>{{template "bottom"}}{{end}}

{{define "demo_note"}}<p class="note" style="border:1px dashed var(--accent);background:var(--accent-soft)">
This is the owner's admin panel of a template demo, shown read-only. In your own app you add, change and delete here, on your phone or computer. The data is public and deleted every hour.</p>{{end}}

{{define "demo_admin_app"}}{{template "wide" .App}}<p class="crumbs"><a href="/templates">Templates</a></p>
<div style="display:flex;flex-wrap:wrap;gap:12px 20px;align-items:center;justify-content:space-between;margin-bottom:16px"><div><h1 style="margin:0 0 4px;font-size:28px">{{.App}}</h1><span class="pill plain">Owner's admin panel, read-only</span></div>
<a class="button secondary" style="margin:0" href="{{.DemoURL}}/">Open the demo site</a></div>{{template "demo_note"}}
<h2 style="margin:20px 0 10px">Your data</h2>{{if .Tables}}<div class="tables">{{range .Tables}}<a href="/apps/{{$.App}}/t/{{.Name}}"><strong>{{.Label}}</strong><span>{{.Rows}} {{if eq .Rows 1}}row{{else}}rows{{end}}</span></a>{{end}}</div>{{end}}
<div class="card" style="margin-top:24px"><h2>In your own app you also get</h2><p class="muted">Card sales and refunds, staff access, scheduled reports by email, new orders on WhatsApp, your own domain and your customers' accounts.</p>
<div class="row"><a class="button" style="margin-top:4px" href="/signup">Create your account</a><a class="button secondary" style="margin-top:4px" href="/connect">Connect ChatGPT or Claude</a></div></div>{{template "bottom"}}{{end}}

{{define "admin_app"}}{{template "wide" .Info.App}}<style>
.ahead{display:flex;flex-wrap:wrap;gap:12px 20px;align-items:center;justify-content:space-between;margin-bottom:20px}.ahead h1{margin:0 0 4px;font-size:28px}
.ahead .acts{display:flex;gap:8px;flex-wrap:wrap;align-items:center}.ahead .acts .button{margin:0}
.agrid{display:grid;gap:20px}@media(min-width:900px){.agrid{grid-template-columns:minmax(0,1fr) 320px;align-items:start}}
.sec{margin-bottom:24px}.sec>h2{margin:0 0 10px}
.side .card{padding:18px 20px}.side h2{font-size:16px;margin-bottom:8px}
.addr{margin:0}.addr dt{font-size:13px;color:var(--muted);margin-top:10px}.addr dt:first-child{margin-top:0}.addr dd{margin:0;overflow-wrap:anywhere;font-size:15px}
.links{list-style:none;padding:0;margin:0}.links li{border-top:1px solid var(--line)}.links li:first-child{border-top:0}
.links a{display:flex;justify-content:space-between;gap:12px;padding:10px 0;text-decoration:none;color:var(--fg)}.links a:hover strong{color:var(--accent)}
.links strong{font-weight:500}.links span{font-size:13.5px;color:var(--muted);text-align:end}
.errs{list-style:none;padding:0;margin:0}.errs li{padding:10px 0;border-top:1px solid var(--line);font-size:14px;overflow-wrap:anywhere}.errs li:first-child{border-top:0}
</style>
<p class="crumbs"><a href="/">Your apps</a></p>
<div class="ahead"><div><h1>{{.Info.App}}</h1><span class="pill {{.Status}}">{{.StatusText}}</span></div>
<div class="acts">{{if .Info.LiveVersion}}<a class="button secondary" href="{{.Info.LiveURL}}/">Open site</a>{{end}}<a class="button secondary" href="/apps/{{.Info.App}}/preview">Open preview</a>
{{if and .IsOwner (gt .Info.PreviewVersion .Info.LiveVersion)}}<a class="button" href="/apps/{{.Info.App}}/publish?version={{.Info.PreviewVersion}}">Publish version {{.Info.PreviewVersion}}</a>{{end}}</div></div>
<div class="agrid"><div>
{{if .Sales}}<section class="sec"><h2>Card sales</h2><div class="tiles">{{range .Sales}}<div class="tile"><div class="muted">{{.Label}}</div>
<div class="big">{{$.Currency}} {{printf "%.2f" .Amount}}</div><div class="muted">{{.Count}} payment{{if ne .Count 1}}s{{end}}</div></div>{{end}}</div>
<p class="muted">Card payments only. Cash-on-delivery orders are in your orders table.</p></section>{{end}}
<section class="sec"><h2>Your data</h2>{{if .Tables}}<div class="tables">{{range .Tables}}<a href="/apps/{{$.Info.App}}/t/{{.Name}}"><strong>{{.Label}}</strong><span>{{.Rows}} {{if eq .Rows 1}}row{{else}}rows{{end}}</span></a>{{end}}</div>
{{else}}<div class="card"><p style="margin:0">{{if .Info.LiveVersion}}This app has no data tables.{{else}}Publish the app to manage its products, orders and bookings here. Until then, check it in the preview.{{end}}</p></div>{{end}}</section>
{{if .Problems}}<section class="sec"><h2>Recent errors</h2><div class="card"><p class="muted">Ask your assistant to "check the Renoxium logs and fix the errors".</p><ul class="errs">{{range .Problems}}<li><span class="muted">{{.Time}}</span><br>{{.Message}}</li>{{end}}</ul></div></section>{{end}}
{{if .IsOwner}}{{with .Jobs}}<section class="sec"><h2>Scheduled jobs</h2><div class="card"><div class="scroll"><table><thead><tr><th>Job</th><th>Runs</th><th>Last run</th><th>Next run</th></tr></thead><tbody>
{{range .}}<tr><td>{{.Name}}</td><td>{{.Schedule}}</td><td>{{if .Status}}{{.LastRun.UTC.Format "2 Jan 15:04"}} UTC · {{if eq .Status "ok"}}<span class="ok">ok</span>{{else}}<span class="error">{{.Message}}</span>{{end}}{{else}}not yet{{end}}</td><td>{{if not .NextRun.IsZero}}{{.NextRun.UTC.Format "2 Jan 15:04"}} UTC{{end}}</td></tr>{{end}}
</tbody></table></div></div></section>{{end}}{{end}}
</div><aside class="side">
<div class="card"><h2>Addresses</h2><dl class="addr">{{if .Info.LiveVersion}}<dt>Your site</dt><dd><a href="{{.Info.LiveURL}}/">{{.Info.LiveURL}}</a></dd>{{end}}
<dt>Preview, version {{.Info.PreviewVersion}}</dt><dd><a href="/apps/{{.Info.App}}/preview">Open the private preview</a></dd>
{{with .MCPURL}}<dt>For customers' AI assistants</dt><dd><input readonly value="{{.}}" aria-label="Address for AI assistants" onfocus="this.select()" style="font:13px ui-monospace,monospace;padding:8px 10px;margin-top:4px"></dd>{{end}}</dl></div>
{{if .IsOwner}}<div class="card"><h2>Settings</h2><ul class="links">
<li><a href="/apps/{{.Info.App}}/payments"><strong>Payments</strong><span>Stripe, refunds</span></a></li>
<li><a href="/apps/{{.Info.App}}/notifications"><strong>Notifications</strong><span>orders on WhatsApp</span></a></li>
<li><a href="/apps/{{.Info.App}}/staff"><strong>Staff</strong><span>people who help you</span></a></li>
<li><a href="/apps/{{.Info.App}}/domain"><strong>Domain</strong><span>www.yourshop.com</span></a></li>
<li><a href="/apps/{{.Info.App}}/connections"><strong>Connections</strong><span>other sites, API keys</span></a></li>
{{if .Info.LiveVersion}}<li><a href="/apps/{{.Info.App}}/visitors"><strong>Customer accounts</strong><span>{{.Visitors}} signed up</span></a></li>{{end}}
</ul></div>{{else}}<div class="card"><p class="muted" style="margin:0">You have staff access to this app.</p>{{if .Info.LiveVersion}}<ul class="links" style="margin-top:8px"><li><a href="/apps/{{.Info.App}}/visitors"><strong>Customer accounts</strong><span>{{.Visitors}} signed up</span></a></li></ul>{{end}}</div>{{end}}
</aside></div>{{template "bottom"}}{{end}}

{{define "publish"}}{{template "top" "Publish"}}<p class="crumbs"><a href="/apps/{{.Info.App}}">{{.Info.App}}</a></p>
<form class="card" method="post" action="/apps/{{.Info.App}}/publish"><h1>{{if .Rollback}}Roll back{{else}}Publish{{end}} version {{.Version}}?</h1>
<p>Your agent asked to make version {{.Version}} of <strong>{{.Info.App}}</strong> live at <a href="{{.Info.LiveURL}}">{{.Info.LiveURL}}</a>.</p>
<p class="muted">Live now: {{if .Info.LiveVersion}}version {{.Info.LiveVersion}}{{else}}nothing yet{{end}}. <a href="/apps/{{.Info.App}}/preview">Check the preview</a> first if you have not.</p>
<p class="muted">Live data is backed up before the change.</p>
<input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="version" value="{{.Version}}">
<button>{{if .Rollback}}Roll back{{else}}Publish{{end}}</button></form>{{template "bottom"}}{{end}}

{{define "admin_list"}}{{template "wide" .Label}}<p class="crumbs">{{if .Demo}}<a href="/templates">Templates</a>{{else}}<a href="/">Your apps</a>{{end}} / <a href="/apps/{{.App}}">{{.App}}</a></p>{{if .Demo}}{{template "demo_note"}}{{end}}<div class="card">
{{if .HasCalendar}}<p style="display:flex;gap:16px"><strong>List</strong><a href="/apps/{{.App}}/t/{{.Table}}/calendar">Calendar</a></p>{{end}}
<div class="bar"><h1 style="margin:0">{{.Label}} <span class="muted">({{.Total}})</span></h1>
<form method="get"><input name="q" value="{{.Query}}" placeholder="Search" aria-label="Search"><button class="secondary">Search</button></form>
{{if not .Demo}}<a class="button" href="/apps/{{.App}}/t/{{.Table}}/new">Add</a>{{end}}</div>
{{with .Notice}}<p class="error">{{.}}</p>{{end}}{{if .Cells}}<div class="scroll"><table><thead><tr>{{range .Columns}}<th>{{.Label}}</th>{{end}}</tr></thead><tbody>
{{range $i, $row := .Cells}}<tr>{{range $j, $v := $row}}<td>{{if eq $j 0}}<a href="/apps/{{$.App}}/t/{{$.Table}}/{{index $.IDs $i}}">{{$v}}</a>{{else}}{{$v}}{{end}}</td>{{end}}</tr>{{end}}
</tbody></table></div>
{{if gt .Pages 1}}<div class="pager">{{if gt .Page 1}}<a href="?q={{.Query}}&page={{.Prev}}">Newer</a>{{end}}<span class="muted">Page {{.Page}} of {{.Pages}}</span>{{if lt .Page .Pages}}<a href="?q={{.Query}}&page={{.Next}}">Older</a>{{end}}</div>{{end}}
{{else}}<p>{{if .Query}}Nothing matches "{{.Query}}".{{else}}No rows yet.{{end}}</p>{{end}}</div>{{template "bottom"}}{{end}}

{{define "operator"}}{{template "wide" "Operator"}}<p class="crumbs"><a href="/">Your apps</a></p>
<div class="card"><h1>Renoxium operator</h1>{{with .Health}}{{range .Problems}}<p class="error">{{.}}</p>{{end}}{{end}}<div class="tiles">
<div class="tile"><div class="muted">Accounts</div><div class="big">{{.Accounts}}</div><div class="muted">Free {{index .PerPlan "free"}} · Pro {{index .PerPlan "pro"}} · Business {{index .PerPlan "business"}}</div></div>
<div class="tile"><div class="muted">Subscription revenue</div><div class="big">AED {{.MRR}}</div><div class="muted">per month · paying Pro {{index .Paying "pro"}}, Business {{index .Paying "business"}}</div></div>
<div class="tile"><div class="muted">Apps</div><div class="big">{{.Stats.Apps}}</div><div class="muted">{{.Stats.LiveApps}} live · {{.Stats.SuspendedApps}} suspended · {{.Stats.StripeReady}} take cards</div></div>
<div class="tile"><div class="muted">WhatsApp codes</div><div class="big">{{.Stats.WhatsAppCodes}}</div><div class="muted">this month, all accounts</div></div>
{{with .Health}}<div class="tile"><div class="muted">Server</div><div class="big">{{.DiskFreeGB}} GB free</div><div class="muted">of {{.DiskTotalGB}} GB · backup {{.BackupAge}} · off-site {{.OffsiteAge}}</div></div>{{end}}
</div>{{with .Health}}{{with .Alerts}}<details><summary>Recent alerts</summary><ul>{{range .}}<li>{{.}}</li>{{end}}</ul></details>{{end}}{{end}}<p class="muted">Card fees collected and subscription payments are in the Stripe Dashboard (Connect, Collected fees; Billing, Subscriptions).</p>
<form method="get" class="bar"><input name="q" value="{{.Q}}" placeholder="Email or app name" aria-label="Search"><button class="secondary">Search</button></form></div>

{{if .Reports}}<div class="card" style="margin-top:16px"><h1>Abuse reports <span class="muted">({{.OpenReports}} open)</span></h1>
{{range .Reports}}<div class="report{{if .Resolved}} done{{end}}"><p><strong>{{.Address}}</strong> <span class="muted">{{.CreatedAt}}{{with .Contact}} · {{.}}{{end}}</span></p><p>{{.Details}}</p>
{{if not .Resolved}}<form method="post" action="/operator/abuse/{{.ID}}/resolve"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="secondary">Mark handled</button></form>{{end}}</div>{{end}}</div>{{end}}

<div class="card" style="margin-top:16px"><h1>Accounts</h1><div class="scroll"><table><thead><tr><th>Email</th><th>Plan</th><th>Joined</th><th>Give plan</th></tr></thead><tbody>
{{range .Users}}<tr><td><a href="/operator?q={{.Email}}">{{.Email}}</a>{{if not .Verified}} <span class="muted">unconfirmed</span>{{end}}</td><td>{{.Plan}}{{with .Status}} <span class="muted">{{.}}</span>{{end}}</td><td>{{.CreatedAt}}</td>
<td><form method="post" action="/operator/plan" class="inline"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="user" value="{{.ID}}"><input type="hidden" name="q" value="{{$.Q}}">
<select name="plan" aria-label="Plan"><option value="free">free</option><option value="pro">pro</option><option value="business">business</option></select><button class="secondary">Set</button></form></td></tr>{{end}}
</tbody></table></div><p class="muted">Setting a plan here grants it without payment; Stripe subscriptions are managed in Stripe.</p></div>

<div class="card" style="margin-top:16px"><h1>Apps</h1><div class="scroll"><table><thead><tr><th>App</th><th>Owner</th><th>Live</th><th>Stripe</th><th></th></tr></thead><tbody>
{{range .Apps}}<tr><td><a href="{{.LiveURL}}" target="_blank" rel="noopener">{{.Name}}</a>{{with .Suspended}} <span class="error">suspended: {{.}}</span>{{end}}</td><td>{{.OwnerEmail}}</td>
<td>{{if .LiveVersion}}v{{.LiveVersion}}{{else}}<span class="muted">not published</span>{{end}}</td><td>{{if .ChargesEnabled}}ready{{else if .StripeAccount}}setting up{{else}}<span class="muted">none</span>{{end}}</td>
<td><form method="post" action="/operator/suspend" class="inline"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="app" value="{{.Name}}"><input type="hidden" name="q" value="{{$.Q}}">
{{if .Suspended}}<button class="secondary" name="action" value="unsuspend">Bring back</button>{{else}}<input name="reason" placeholder="Reason" aria-label="Reason" style="width:140px"><button class="danger" name="action" value="suspend" style="margin:0">Suspend</button>{{end}}</form></td></tr>{{end}}
</tbody></table></div></div>{{template "bottom"}}{{end}}

{{define "account"}}{{template "top" "Account"}}<p class="crumbs"><a href="/">Your apps</a></p>
<div class="card"><h1>Account</h1><p>{{.User.Email}} {{if .User.Verified}}<span class="ok">confirmed</span>{{else}}<span class="muted">not confirmed</span>{{end}}</p>
<p class="muted"><a href="/billing">Plan and billing</a></p></div>
<form class="card" method="post" action="/account/password" style="margin-top:16px"><h1>Change password</h1>
{{if .Changed}}<p class="ok">Password changed. Other devices and connected agents were signed out.</p>{{end}}{{with .PasswordError}}<p class="error">{{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}"><label for="current">Current password</label><input id="current" name="current" type="password" autocomplete="current-password" required>
<label for="password">New password (at least 10 characters)</label><input id="password" name="password" type="password" autocomplete="new-password" minlength="10" required>
<button>Change password</button><p class="muted" style="margin-top:12px">Connected agents (ChatGPT, Claude) will need to connect again.</p></form>
<div class="card" style="margin-top:16px"><h1>Export your data</h1><p class="muted">A zip file with your account details and, for each app, its definition, its live data and the list of its files.</p>
<a class="button" href="/account/export">Download export</a></div>
<form class="card" method="post" action="/account/delete" style="margin-top:16px"><h1>Delete account</h1>
<p>This deletes your account and {{len .Apps}} app(s) with all their data, for good. Export first if you want a copy.{{if ne .Billing.Plan "free"}} Your paid plan is cancelled at once, without a refund for the rest of the period.{{end}}</p>{{with .DeleteError}}<p class="error">{{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}"><label for="confirm">Type your email address to confirm</label><input id="confirm" name="confirm" autocomplete="off" required>
<label for="dpassword">Password</label><input id="dpassword" name="password" type="password" autocomplete="current-password" required>
<button class="danger">Delete my account</button></form>{{template "bottom"}}{{end}}

{{define "billing"}}{{template "top" "Plan and billing"}}<p class="crumbs"><a href="/">Your apps</a></p><div class="card"><h1>Plan and billing</h1>
<p>Current plan: <strong>{{if eq .Billing.Plan "pro"}}Pro{{else if eq .Billing.Plan "business"}}Business{{else}}Free{{end}}</strong>
{{if .Paying}}<span class="muted">· {{.Billing.Status}}{{with .PeriodEnd}} · {{if $.Billing.CancelAtPeriodEnd}}ends{{else}}renews{{end}} {{.}}{{end}}</span>{{end}}</p>
<p class="muted">Apps: {{.Apps}} of {{.Limit}} · WhatsApp sign-in codes this month: {{.WAUsed}} of {{.WAQuota}} · Image storage: {{.StorageMB}} MB of {{.StorageQuotaMB}} MB · External API calls this month: {{.FetchUsed}} of {{.FetchQuota}}</p>
{{if not .Ready}}<p>Paid plans are not available yet.</p>
{{else if .Paying}}<p>No Renoxium fee on your shops' card payments, and no Renoxium badge on your shops.</p>
<form method="post" action="/billing/portal"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Manage billing</button></form>
<p class="muted">Change plan, update your card, download invoices or cancel.</p>
{{else}}<p class="muted">On the Free plan Renoxium takes {{.Fee}}% of card payments in your shops and shows a small "Made with Renoxium" badge.</p>
<div class="plans"><form class="plan" method="post" action="/billing/checkout"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="plan" value="pro">
<h2>Pro</h2><p class="price">AED 99 <span class="muted">/ month</span></p><ul><li>3 apps</li><li>500 WhatsApp sign-in codes a month</li><li>2 GB of images</li><li>20,000 external API calls a month</li><li>Your own domain</li><li>2 staff per app</li><li>No fee on card payments</li><li>No Renoxium badge</li></ul><button>Choose Pro</button></form>
<form class="plan" method="post" action="/billing/checkout"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="plan" value="business">
<h2>Business</h2><p class="price">AED 299 <span class="muted">/ month</span></p><ul><li>10 apps</li><li>2,000 WhatsApp sign-in codes a month</li><li>10 GB of images</li><li>100,000 external API calls a month</li><li>Your own domain</li><li>10 staff per app</li><li>No fee on card payments</li><li>No Renoxium badge</li></ul><button>Choose Business</button></form></div>
<p class="muted">Payment is handled by Stripe. Cancel any time.</p>{{end}}</div>{{template "bottom"}}{{end}}

{{define "admin_connections"}}{{template "top" "Connections"}}<p class="crumbs"><a href="/apps/{{.App}}">{{.App}}</a></p>
<div class="card"><h1>Connections</h1>{{with .Error}}<p class="error">{{.}}</p>{{end}}
<p>Your app can call other services, such as a courier or SMS service, only with your permission. Allow a site only if you trust it:
your app can send it data, including your customers' details.</p>
{{if not .Hosts}}<p class="muted">This app does not use other sites.</p>{{else}}
<div class="scroll"><table><thead><tr><th>Site</th><th>Used for</th><th></th></tr></thead><tbody>
{{range .Hosts}}<tr><td><strong>{{.Host}}</strong></td><td>{{.Purpose}}</td><td>{{if .Allowed}}<span class="ok">allowed</span> <span class="muted">by {{.AllowedBy}}</span>
<form method="post" class="inline"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="action" value="revoke"><input type="hidden" name="host" value="{{.Host}}"><button class="danger">Stop</button></form>
{{else}}<form method="post" class="inline"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="action" value="allow"><input type="hidden" name="host" value="{{.Host}}"><button>Allow</button></form>{{end}}</td></tr>{{end}}
</tbody></table></div>{{end}}
{{if .Secrets}}<h2 style="margin-top:20px">API keys</h2><p class="muted">Keys are stored encrypted. Your app's scripts and the AI agent never see them: Renoxium adds a key only to requests to its own site.</p>
{{range .Secrets}}<div class="card" style="margin:10px 0"><p><strong>{{.Name}}</strong> <span class="muted">for {{.Host}}</span>{{if .Set}}{{if .Mismatch}} · <span class="error">entered for {{.SavedHost}}; enter it again</span>{{else}} · <span class="ok">saved</span>{{with .Hint}} <span class="muted">(ends in {{.}})</span>{{end}}{{end}}{{end}}</p>
{{with .Help}}<p class="muted">{{.}}</p>{{end}}
<form method="post"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="action" value="secret"><input type="hidden" name="name" value="{{.Name}}">
<label for="v-{{.Name}}">{{if .Set}}Replace the key{{else}}Key{{end}}</label><input id="v-{{.Name}}" name="value" type="password" autocomplete="off" required{{if eq .Prefix "Basic"}} placeholder="user:password"{{end}}><button>Save</button></form>
{{if .Set}}<form method="post" style="margin-top:8px"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="action" value="delete-secret"><input type="hidden" name="name" value="{{.Name}}"><button class="danger">Remove key</button></form>{{end}}</div>{{end}}{{end}}
{{if .Hosts}}<h2 style="margin-top:20px">Test calls from the preview</h2>
<p class="muted">The preview holds a copy of your live data, and your AI agent can change the preview without asking you. So the preview only calls these sites while you turn test calls on. Turn it on when you and your agent are testing a connection.</p>
{{if .PreviewUntil}}<p><span class="ok">On until {{.PreviewUntil}} UTC.</span></p>
<form method="post" class="inline"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="action" value="preview-off"><button class="danger">Turn off now</button></form>
{{else}}<form method="post" class="inline"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="action" value="preview-on"><button>Turn on for 2 hours</button></form>{{end}}{{end}}
<p class="muted" style="margin-top:16px">External calls from your apps this month, preview included: {{.Used}} of {{.Quota}}.</p>
</div>{{template "bottom"}}{{end}}

{{define "admin_alerts"}}{{template "top" "Notifications"}}<p class="crumbs"><a href="/apps/{{.App}}">{{.App}}</a></p>
<div class="card"><h1>Notifications</h1>{{with .Error}}<p class="error">{{.}}</p>{{end}}
<p>Notices from your app, such as new orders, are emailed to <strong>{{.Email}}</strong>.</p>
{{if not .Available}}<p class="muted">WhatsApp notices are not available on this Renoxium server yet.</p>
{{else if not .Phone}}<p>Get them on WhatsApp too. We send a code to confirm the number.</p>
<form method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="action" value="send">
<label for="phone">WhatsApp number</label><input id="phone" name="phone" type="tel" placeholder="+971 50 123 4567" required autocomplete="tel"><button>Send code</button></form>
{{else if not .Phone.Confirmed}}<p>We sent a code to <strong>{{.Phone.Phone}}</strong> on WhatsApp.</p>
<form method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="action" value="confirm">
<label for="code">Code</label><input id="code" name="code" inputmode="numeric" autocomplete="one-time-code" required><button>Confirm</button></form>
<form method="post" style="margin-top:12px"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="action" value="remove"><button>Use another number</button></form>
{{else}}<p>WhatsApp notices go to <strong>{{.Phone.Phone}}</strong> <span class="ok">on</span></p>
<form method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="action" value="remove"><button class="danger">Turn off WhatsApp notices</button></form>{{end}}
{{if .Available}}<p class="muted">Each WhatsApp notice counts as one of your plan's WhatsApp messages: {{.Used}} of {{.Quota}} used this month.</p>{{end}}
</div>{{template "bottom"}}{{end}}

{{define "admin_domain"}}{{template "top" "Domain"}}<p class="crumbs"><a href="/apps/{{.App}}">{{.App}}</a></p>
<div class="card"><h1>Your own domain</h1>{{with .Error}}<p class="error">{{.}}</p>{{end}}
{{if not .Available}}<p>Custom domains are not available on this Renoxium server yet.</p>
{{else if not .Allowed}}<p>Custom domains are included in the <a href="/billing">Pro and Business plans</a>.</p>
{{else if not .Domain}}<p>Serve this app on a domain you own, such as <strong>www.yourshop.com</strong>. A subdomain like www works with every domain provider.</p>
<form method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="action" value="add">
<label for="host">Domain</label><input id="host" name="host" placeholder="www.yourshop.com" required autocomplete="off"><button>Add domain</button></form>
{{else}}{{with .Domain}}<p><strong>{{.Host}}</strong> · {{if eq .Status "active"}}<span class="ok">active</span>{{else if eq .Status "verify"}}prove it is yours{{else}}waiting for DNS{{end}}</p>
{{if eq .Status "verify"}}<p>First show that this domain is yours. At the company where you bought it, add this DNS record, then choose Check now. Changes can take from minutes to a few hours.</p>
<div class="scroll"><table><thead><tr><th>Type</th><th>Name</th><th>Value</th></tr></thead><tbody>
<tr><td>TXT</td><td>{{.Verify.Name}}</td><td><code>{{.Verify.Value}}</code></td></tr></tbody></table></div>
<p class="muted">This record belongs to your Renoxium account. Keep it for as long as you use the domain here.</p>
<form method="post" style="display:inline"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="action" value="check"><button>Check now</button></form>
{{else if eq .Status "active"}}<p>Your app is live at <a href="https://{{.Host}}">https://{{.Host}}</a>, and links in emails and payments use it.</p>
{{else}}<p>At the company where you bought the domain, add these DNS records. Changes can take from minutes to a few hours.</p>
<div class="scroll"><table><thead><tr><th>Type</th><th>Name</th><th>Value</th></tr></thead><tbody>
<tr><td>CNAME</td><td>{{.Host}}</td><td><code>{{.CNAME}}</code></td></tr>
{{if .Ownership.Name}}<tr><td>TXT</td><td>{{.Ownership.Name}}</td><td><code>{{.Ownership.Value}}</code></td></tr>{{end}}
{{range .SSLRecords}}<tr><td>TXT</td><td>{{.Name}}</td><td><code>{{.Value}}</code></td></tr>{{end}}
</tbody></table></div>{{with .Problems}}<p class="muted">Not ready yet: {{range $i, $p := .}}{{if $i}}; {{end}}{{$p}}{{end}}</p>{{end}}
<form method="post" style="display:inline"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="action" value="check"><button>Check now</button></form>{{end}}
<form method="post" style="margin-top:16px"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="action" value="remove"><button class="danger">Remove domain</button></form>{{end}}{{end}}
</div>{{template "bottom"}}{{end}}

{{define "admin_staff"}}{{template "top" "Staff"}}<p class="crumbs"><a href="/apps/{{.App}}">{{.App}}</a></p>
<div class="card"><h1>Staff</h1><p class="muted">Staff can edit your data (products, orders, discount codes), upload photos and see visitors. Only you can publish, connect Stripe, delete visitors and manage staff.</p>
{{with .Error}}<p class="error">{{.}}</p>{{end}}
{{if .Staff}}<ul class="plain">{{range .Staff}}<li><span>{{.}}</span><form method="post" action="/apps/{{$.App}}/staff/remove" class="inline"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="email" value="{{.}}"><button class="secondary">Remove</button></form></li>{{end}}</ul>
{{else}}<p>No staff yet.</p>{{end}}
<p class="muted">{{len .Staff}} of {{.Limit}} on your plan{{if eq .Limit 0}}. Staff need the <a href="/billing">Pro or Business plan</a>{{end}}.</p>
{{if lt (len .Staff) .Limit}}<form method="post" action="/apps/{{.App}}/staff"><input type="hidden" name="csrf" value="{{.CSRF}}"><label for="email">Email address</label>
<input id="email" name="email" type="email" required><button>Add staff member</button></form>
<p class="muted">They get an email. They sign in with a Renoxium account for this address and confirm the address first.</p>{{end}}</div>{{template "bottom"}}{{end}}

{{define "admin_payments"}}{{template "wide" "Payments"}}<p class="crumbs"><a href="/">Your apps</a> / <a href="/apps/{{.App}}">{{.App}}</a></p><div class="card">
<h1>Payments</h1>
{{if not .Account.StripeConfigured}}<p>Card payments are not available on this Renoxium server yet.</p>
{{else if not .Account.StripeAccount}}<p>Connect a Stripe account to take card payments. Shoppers pay you directly; Stripe's fees are charged to your Stripe account{{if .Fee}}, and on the Free plan Renoxium takes {{.Fee}}% of each card payment (<a href="/billing">no fee on Pro and Business</a>){{else}}, and Renoxium takes no fee{{end}}.</p>
<div class="note"><p><strong>Before you start, have these ready.</strong> Stripe checks every business by law, and its form takes 15–30 minutes. In the UAE it usually asks for:</p>
<ul><li>the trade licence and the memorandum of association (for a company)</li><li>the passport and Emirates ID or visa of the owner and of each person owning 25% or more</li>
<li>the business address and phone number</li><li>a UAE bank account in the business's name (IBAN), for payouts</li></ul>
<p class="muted">Stripe may show a CAPTCHA and ask for more. You can stop and continue later from this page; payments open once Stripe has checked your details, often within minutes.</p></div>
<form method="post" action="/apps/{{.App}}/payments/connect"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Connect Stripe</button></form>
{{else if .Account.ChargesEnabled}}<p class="ok">Stripe is connected and ready to take payments.</p><p class="muted">Account {{.Account.StripeAccount}}. Refund payments and take card holds below; manage payouts and disputes in your Stripe Dashboard.</p>
{{else}}<p>Your Stripe account is connected but not ready to take payments yet{{if .Account.DetailsSubmitted}}: Stripe is reviewing your details{{end}}.</p>
<form method="post" action="/apps/{{.App}}/payments/connect"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Continue Stripe setup</button></form>{{end}}
{{with .Notice}}<p class="ok" role="status">{{.}}</p>{{end}}{{with .Error}}<p class="error" role="alert">{{.}}</p>{{end}}
<h1 style="margin-top:24px">Recent payments</h1>
{{if .Payments}}<div class="scroll"><table><thead><tr><th>Order</th><th>Status</th><th>Amount</th><th>Customer</th><th>Started</th><th></th></tr></thead><tbody>
{{range .Payments}}<tr><td>{{.Reference}}</td><td>{{if eq .Status "authorized"}}card hold{{with .HoldExpiresAt}}, take by {{.}} UTC{{end}}{{else}}{{.Status}}{{end}}</td>
<td>{{printf "%.2f" .Amount}} {{.Currency}}{{if gt .Refunded 0.0}}<br><span class="muted">{{printf "%.2f" .Refunded}} refunded</span>{{end}}</td><td>{{.CustomerEmail}}</td><td>{{.CreatedAt}}</td>
<td>{{if gt .Refundable 0.0}}<form method="post" action="/apps/{{$.App}}/payments/{{.ID}}/refund" class="inline" onsubmit="return confirm('Refund this payment to the card?')"><input type="hidden" name="csrf" value="{{$.CSRF}}">
<input name="amount" inputmode="decimal" placeholder="{{printf "%.2f" .Refundable}}" aria-label="Amount to refund (empty = {{printf "%.2f" .Refundable}})" style="width:7em;margin:0"> <button class="secondary" style="margin:0;padding:4px 10px">Refund</button></form>
{{else if eq .Status "authorized"}}<form method="post" action="/apps/{{$.App}}/payments/{{.ID}}/capture" class="inline"><input type="hidden" name="csrf" value="{{$.CSRF}}">
<input name="amount" inputmode="decimal" placeholder="{{printf "%.2f" .Authorized}}" aria-label="Amount to take (empty = all {{printf "%.2f" .Authorized}})" style="width:7em;margin:0"> <button style="margin:0;padding:4px 10px">Capture</button></form>
<form method="post" action="/apps/{{$.App}}/payments/{{.ID}}/release" class="inline" onsubmit="return confirm('Release the hold? Nothing is charged.')"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="secondary" style="margin:0;padding:4px 10px">Release</button></form>{{end}}</td></tr>{{end}}
</tbody></table></div><p class="muted">Refund: leave the amount empty to refund everything that is left, or type part of it. On the Free plan Renoxium's fee is refunded with it. Card holds: capture all or part (e.g. a damage charge) before the date shown, or release them; most cards drop a hold after about 7 days.</p>
{{else}}<p class="muted">No payments yet.</p>{{end}}
{{if .Subscriptions}}<h1 style="margin-top:24px">Subscriptions</h1><div class="scroll"><table><thead><tr><th>Reference</th><th>Status</th><th>Amount</th><th>Next payment</th><th>Customer</th><th></th></tr></thead><tbody>
{{range .Subscriptions}}<tr><td>{{.Reference}}</td><td>{{.Status}}{{if .CancelAtPeriodEnd}}{{if ne .Status "canceled"}}, ends at period end{{end}}{{end}}</td>
<td>{{printf "%.2f" .Amount}} {{.Currency}} every {{if gt .IntervalCount 1}}{{.IntervalCount}} {{.Interval}}s{{else}}{{.Interval}}{{end}}<br><span class="muted">{{.PaymentsMade}} paid</span></td>
<td>{{if and (ne .Status "canceled") (not .CancelAtPeriodEnd)}}{{.CurrentPeriodEnd}}{{else}}—{{end}}</td><td>{{.CustomerEmail}}</td>
<td>{{if ne .Status "canceled"}}<form method="post" action="/apps/{{$.App}}/subscriptions/{{.ID}}/cancel" class="inline" onsubmit="return confirm('Cancel this subscription?')"><input type="hidden" name="csrf" value="{{$.CSRF}}">
<select name="when" aria-label="When" style="margin:0;width:auto"><option value="period_end">At period end</option><option value="now">Now</option></select> <button class="danger" style="margin:0;padding:4px 10px">Cancel</button></form>{{end}}</td></tr>{{end}}
</tbody></table></div><p class="muted">Cancelling never refunds a payment already made; refund it above if needed.</p>{{end}}</div>{{template "bottom"}}{{end}}

{{define "admin_visitors"}}{{template "wide" "Visitors"}}<p class="crumbs"><a href="/">Your apps</a> / <a href="/apps/{{.App}}">{{.App}}</a></p><div class="card">
<div class="bar"><h1 style="margin:0">Visitor accounts <span class="muted">({{.Total}})</span></h1>
<form method="get"><input name="q" value="{{.Query}}" placeholder="Search email, number or name" aria-label="Search"><button class="secondary">Search</button></form></div>
<p class="muted">People who created an account on your app. Passwords are never shown to anyone. Deleting an account also deletes the files that person uploaded.</p>
{{if .Visitors}}<div class="scroll"><table><thead><tr><th>#</th><th>Email</th><th>WhatsApp</th><th>Name</th><th>Confirmed</th><th>Files</th><th>Joined</th><th></th></tr></thead><tbody>
{{range .Visitors}}<tr><td>{{.ID}}</td><td>{{.Email}}</td><td>{{.Phone}}</td><td>{{.Name}}</td><td>{{if .Verified}}Yes{{else}}No{{end}}</td><td>{{.Files}}</td><td>{{.CreatedAt}}</td>
<td><form method="post" action="/apps/{{$.App}}/visitors/{{.ID}}/delete"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="danger" style="margin:0;padding:4px 10px">Delete</button></form></td></tr>{{end}}
</tbody></table></div>
{{if gt .Pages 1}}<div class="pager">{{if gt .Page 1}}<a href="?q={{.Query}}&page={{.Prev}}">Newer</a>{{end}}<span class="muted">Page {{.Page}} of {{.Pages}}</span>{{if lt .Page .Pages}}<a href="?q={{.Query}}&page={{.Next}}">Older</a>{{end}}</div>{{end}}
{{else}}<p>{{if .Query}}Nobody matches "{{.Query}}".{{else}}No visitor accounts yet.{{end}}</p>{{end}}</div>{{template "bottom"}}{{end}}

{{define "admin_form"}}{{template "top" .Label}}<p class="crumbs"><a href="/apps/{{.App}}">{{.App}}</a> / <a href="/apps/{{.App}}/t/{{.Table}}">{{.Label}}</a>{{with .Back}} / <a href="{{.}}">Back to calendar</a>{{end}}</p>{{if .Demo}}{{template "demo_note"}}{{end}}
<form class="card" method="post"{{if .HasImage}} enctype="multipart/form-data"{{end}}><h1>{{if .ID}}{{.Label}} #{{.ID}}{{else}}New {{.Label}}{{end}}</h1>{{if .Demo}}<fieldset disabled style="border:0;padding:0;margin:0">{{end}}
{{if .Saved}}<p class="ok">Saved.</p>{{end}}{{with .Notice}}<p class="error">{{.}}</p>{{end}}{{if .HasErrors}}<p class="error">Check the highlighted fields.{{with .Error}} {{.}}{{end}}</p>{{end}}
{{with .Created}}<p class="muted">Created {{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}">{{with .Back}}<input type="hidden" name="back" value="{{.}}">{{end}}
{{range .Fields}}{{if eq .Input "checkbox"}}<label class="check"><input type="checkbox" name="{{.Name}}" value="1"{{if .Checked}} checked{{end}}> {{.Label}}</label>
{{else}}<label for="f-{{.Name}}">{{.Label}}{{if .Required}} *{{end}}</label>
{{if eq .Input "image"}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">
{{if .Value}}<img class="thumb" src="{{$.LiveURL}}{{.Value}}?w=320" alt=""><label class="check"><input type="checkbox" name="{{.Name}}__remove" value="1"> Remove image</label>{{end}}
<input id="f-{{.Name}}" name="{{.Name}}__upload" type="file" accept="image/jpeg,image/png,image/gif,image/webp,image/avif">
{{else if eq .Input "file"}}{{if .OwnerOnly}}<p id="f-{{.Name}}" class="muted">{{if .FileName}}A document is attached. Only the app's owner can open it.{{else if .Value}}The file was deleted.{{else}}No file.{{end}}</p>
{{else}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{if .Value}}{{if .FileName}}<p id="f-{{.Name}}"><a href="/apps/{{$.App}}/files/{{.Value}}" target="_blank" rel="noopener noreferrer">{{.FileName}}</a></p>
<label class="check"><input type="checkbox" name="{{.Name}}__delete" value="1"> Delete this file</label>{{else}}<p id="f-{{.Name}}" class="muted">The file was deleted.</p>{{end}}{{else}}<p id="f-{{.Name}}" class="muted">No file. Visitors upload files on the app.</p>{{end}}{{end}}
{{else if eq .Input "select"}}<select id="f-{{.Name}}" name="{{.Name}}"{{if .Required}} required{{end}}>{{if not .Required}}<option value=""></option>{{end}}{{$v := .Value}}{{range .Options}}<option{{if eq . $v}} selected{{end}}>{{.}}</option>{{end}}</select>
{{else if eq .Input "textarea"}}<textarea id="f-{{.Name}}" name="{{.Name}}"{{if eq .Type "json"}} class="json"{{end}}{{if .Required}} required{{end}}>{{.Value}}</textarea>
{{else}}<input id="f-{{.Name}}" name="{{.Name}}" type="{{.Input}}" value="{{.Value}}"{{if eq .Type "real"}} step="any"{{end}}{{if eq .Type "integer"}} step="1"{{end}}{{if .Required}} required{{end}}>{{end}}{{end}}
{{with .Help}}<p class="muted help">{{.}}</p>{{end}}{{with .Error}}<p class="field-error">{{.}}</p>{{end}}{{end}}
{{if .Demo}}</fieldset>{{else}}<button>Save</button>{{end}}</form>
{{if and .ID (not .Demo)}}<form method="post" action="/apps/{{.App}}/t/{{.Table}}/{{.ID}}/delete" style="margin-top:16px"><input type="hidden" name="csrf" value="{{.CSRF}}">{{with .Back}}<input type="hidden" name="back" value="{{.}}">{{end}}<button class="danger">Delete {{.Label}} #{{.ID}}</button></form>{{end}}
{{template "bottom"}}{{end}}

{{define "connect"}}{{template "wide" "Connect"}}<div class="card connect">{{range .Blocks}}{{if eq .Kind "h1"}}<h1>{{.Text}}</h1>
{{else if eq .Kind "h2"}}<h2>{{.Text}}</h2>{{else if eq .Kind "ul"}}<ul>{{range .Items}}<li>{{.}}</li>{{end}}</ul>
{{else if eq .Kind "ol"}}<ol>{{range .Items}}<li>{{.}}</li>{{end}}</ol>{{else}}<p>{{.Text}}</p>{{end}}{{end}}</div>{{template "bottom"}}{{end}}

{{define "forgot"}}{{template "auth" "Forgot password"}}<form class="card" method="post" action="/forgot"><h1>Forgot your password?</h1>
<p class="muted">Enter your email and we will send you a link to choose a new password.</p>
<input type="hidden" name="csrf" value="{{.CSRF}}"><label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required>
<button>Send the link</button></form>{{template "bottom"}}{{end}}

{{define "reset"}}{{template "auth" "New password"}}<form class="card" method="post" action="/reset"><h1>Choose a new password</h1>
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
