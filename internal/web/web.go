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

	// OpenAIAppsChallenge is the token OpenAI gives to verify the domain of
	// a ChatGPT app; /.well-known/openai-apps-challenge answers it. Empty: 404.
	OpenAIAppsChallenge string

	// Operators are the emails allowed into /operator (confirmed accounts only).
	Operators []string

	// Billing charges accounts for Renoxium plans.
	Billing Billing

	growth  growthCache // operator funnel figures (growth.go)
	resFees resFees     // card fees per app for /operator/resources (resources.go)

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
	s.mcp = mcpserver.Handler(mcpserver.New(p, mcpserver.Options{ConfirmPublish: s.publishLink, ConfirmRename: s.renameLink, ConfirmDelete: s.deleteLink}), s.verify, base+"/.well-known/oauth-protected-resource/mcp")

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
	s.registerResources()
	s.registerLanding()
	s.registerStaff()
	s.registerDomain()
	s.registerDeleteApp()
	s.registerRenameApp()
	s.registerAlerts()
	s.registerConnections()
	s.registerAbuse()
	s.registerGallery()
	s.registerBrand()
	s.registerStatic()
	s.registerSEO()
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
	if _, pattern := s.mux.Handler(r); pattern == "" {
		render(w, http.StatusNotFound, "notfound", nil)
		return
	}
	if r.Method == http.MethodGet && isPublicPage(r.URL.Path) {
		s.servePublic(w, r)
		return
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
		renderLanding(w, s.landingData(false))
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
	live, errs, customers := 0, 0, 0
	for _, c := range cards {
		if c.LiveVersion > 0 {
			live++
		}
		errs += c.Errors
		customers += c.Visitors
	}
	csrf := s.csrfToken(w, r)
	render(w, http.StatusOK, "dashboard", map[string]any{"User": u, "Apps": cards, "MCP": s.base + "/mcp", "CSRF": csrf, "Plan": plan,
		"Shared": sharedCards, "AppLimit": platform.AppsPerPlan[s.accounts.Plan(u.ID)], "Live": live, "Errors": errs, "Customers": customers,
		"Shell": s.ownerShell(u, "Your apps", "/", csrf)})
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
{{define "top"}}{{template "head" .}}<body>{{template "pubnav"}}<main>{{end}}
{{define "wide"}}{{template "head" .}}<body>{{template "pubnav"}}<main class="wide">{{end}}
{{define "auth"}}{{template "head" .}}<body class="auth">{{template "pubnav"}}<div class="authwrap"><main>{{end}}
{{define "authbottom"}}</main><aside class="authside" aria-label="About Renoxium"><h2>Orders, bookings and payments in one place.</h2>
<p class="ar" lang="ar" dir="rtl">الطلبات والحجوزات والمدفوعات في مكان واحد</p>
<p>Describe your business to ChatGPT or Claude. Renoxium builds the website, you check the preview, and nothing goes live until you publish.</p>
<ul><li>Card payments to your own Stripe account, or cash on delivery</li><li>English and Arabic, with AED prices and 5% VAT</li><li>Free to start: one site, no card needed</li></ul></aside></div></body></html>{{end}}
{{define "apptop"}}{{template "head" .Shell.Title}}<body class="app">{{template "nav"}}<div class="shell">{{template "sidenav" .Shell}}<main class="appmain">{{if .Shell.Demo}}{{template "demo_note"}}{{end}}{{with .Shell.Throttle}}<p class="note throttle" role="status" style="border-color:var(--sun);background:var(--sun-soft)"><strong>Limited by Renoxium.</strong> {{.}}</p>{{end}}{{end}}
{{define "appbottom"}}</main></div></body></html>{{end}}
{{define "sidenav"}}<aside class="sidenav" aria-label="{{.App}}">{{if .Demo}}<a class="back" href="/templates">← Templates</a>{{else}}<a class="back" href="/">← Your apps</a>{{end}}<div class="who"><strong>{{.App}}</strong>{{with .StatusText}}<span class="pill {{$.Status}}">{{.}}</span>{{end}}{{with .SiteURL}}<a class="site" href="{{.}}/">Open site</a>{{end}}</div>
<nav>{{template "navlist" .Main}}{{with .Data}}<h3>Data</h3>{{template "navlist" .}}{{end}}{{with .Business}}<h3>Business</h3>{{template "navlist" .}}{{end}}{{with .Settings}}<h3>Settings</h3>{{template "navlist" .}}{{end}}</nav></aside>{{end}}
{{define "tabs"}}<nav class="tabs" aria-label="Sections">{{range .}}<a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}}</a>{{end}}</nav>{{end}}
{{define "navlist"}}<ul>{{range .}}<li><a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}}</a></li>{{end}}</ul>{{end}}
{{define "pager"}}{{if .Total}}<nav class="pager" aria-label="Pages"><span class="muted">{{.From}}–{{.To}} of {{.Of}}</span>{{if gt .Pages 1}}<span class="pages">{{if .Prev}}<a href="{{.Prev}}" rel="prev" aria-label="Previous page">‹</a>{{end}}{{range .Links}}{{if .Ellip}}<span>…</span>{{else}}<a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}}</a>{{end}}{{end}}{{if .Next}}<a href="{{.Next}}" rel="next" aria-label="Next page">›</a>{{end}}</span>{{end}}</nav>{{end}}{{end}}
{{define "chart"}}<div class="chart">{{if .Bars}}<span class="top" aria-hidden="true">{{.Max}}</span><div class="plot" role="img" aria-label="{{.Title}}, total {{.Total}}">{{range .Bars}}<span class="b{{if .Today}} today{{end}}" tabindex="0"><i style="height:{{.Height}}%"></i><span class="tip">{{.Tip}}</span></span>{{end}}</div>
<div class="ticks" aria-hidden="true">{{range .Ticks}}<span>{{.}}</span>{{end}}</div>
<details class="astable"><summary>Show as a table</summary><table class="rx"><thead><tr><th>Day</th><th>Figures</th></tr></thead><tbody>{{range .Rows}}<tr><td>{{index . 0}}</td><td>{{index . 1}}</td></tr>{{end}}</tbody></table></details>
{{else}}<p class="empty">{{.Empty}}</p>{{end}}</div>{{end}}
{{define "nav"}}<header class="bar-top"><div class="in"><a class="logo" href="/" aria-label="Renoxium: your apps">{{template "brandlogo"}}</a><nav aria-label="Site"><a href="/support">Support</a></nav></div></header>{{end}}
{{define "pubnav"}}<header class="bar-top"><div class="in"><a class="logo" href="/" aria-label="Renoxium home">{{template "brandlogo"}}</a><nav aria-label="Main"><a href="/templates">Templates</a><a class="wide" href="/#pricing">Pricing</a><a class="wide" href="/connect">Connect</a><a href="/login">Sign in</a><a class="button sm" href="/signup">Create account</a></nav></div></header>{{end}}
{{define "head"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.}} · Renoxium</title>{{template "icons"}}{{template "fonts"}}<style>
:root{color-scheme:light dark;--series:#2b4a85;--brand:#13203a;--fg:#13203a;--muted:#4b5870;--bg:#eef1f5;--card:#fff;--line:#d6dce5;--accent:#13203a;--accent-ink:#fff;--accent-soft:#e6ebf3;--gold:#b8892e;--sun:#b8892e;--sun-soft:#f5ecd9;--live:#1e8a50;--bad:#b3261e;--radius:12px}
@media (prefers-color-scheme:dark){:root{--series:#8fb0ea;--brand:#e7ecf4;--fg:#e7ecf4;--muted:#a3afc4;--bg:#0b1424;--card:#121d33;--line:#263552;--accent:#e7ecf4;--accent-ink:#0b1424;--accent-soft:#1b2a47;--gold:#d2a955;--sun:#d2a955;--sun-soft:#3a2f17;--live:#5fd394;--bad:#ff8a80}}
*{box-sizing:border-box}html{-webkit-text-size-adjust:100%}
body{margin:0;font:16px/1.55 "Readex Pro",system-ui,sans-serif;color:var(--fg);background:var(--bg)}
a{color:var(--accent);text-decoration-color:var(--muted);text-underline-offset:3px}a:hover{text-decoration-color:var(--gold);text-decoration-thickness:2px}a:focus-visible,button:focus-visible,input:focus-visible,select:focus-visible,textarea:focus-visible,summary:focus-visible{outline:3px solid var(--accent);outline-offset:2px}
.bar-top{background:var(--card);border-bottom:1px solid var(--line)}.bar-top .in{max-width:1200px;margin:0 auto;padding:14px 20px;min-height:68px;display:flex;align-items:center;gap:8px 20px;flex-wrap:wrap}
.logo{display:inline-flex;align-items:center;gap:9px;font-weight:600;font-size:22px;color:var(--brand);text-decoration:none;letter-spacing:-.02em;margin-inline-end:auto}.logo .mark{height:30px;width:auto;flex:none}
.bar-top nav{display:flex;align-items:center;gap:4px 22px;flex-wrap:wrap;font-size:15px}.bar-top nav a{color:var(--muted);text-decoration:none}.bar-top nav a:hover{color:var(--fg)}
.bar-top nav a.button{color:var(--accent-ink);margin:0}.button.sm{min-height:40px;padding:8px 16px;font-size:15px}
@media(max-width:600px){.bar-top nav .wide{display:none}.bar-top nav{gap:4px 16px}}
.sidenav a.back{display:inline-block;font-size:14px;color:var(--muted);text-decoration:none;margin:0 0 14px}.sidenav a.back:hover{color:var(--fg)}
main{max-width:600px;margin:32px auto 64px;padding:0 20px}main.wide{max-width:1120px}body.auth{background:var(--card)}.authwrap{display:grid;min-height:calc(100vh - 69px)}
body.auth main{max-width:none;margin:0;padding:clamp(32px,9vh,104px) 24px 56px;display:flex;justify-content:center;align-items:flex-start}
body.auth main>.card{width:100%;max-width:400px;border:0;border-radius:0;padding:0;background:none}
body.auth h1{font-size:30px;line-height:1.15;margin-bottom:10px}body.auth main button{width:100%}body.auth main .row button{flex:1;width:auto}body.auth code{display:block;padding:10px 12px}
.authside{display:none}
@media(min-width:960px){.authwrap{grid-template-columns:minmax(0,1fr) minmax(0,1fr)}
.authside{display:flex;flex-direction:column;justify-content:center;position:relative;isolation:isolate;overflow:hidden;background:#101b33;color:#e7ecf4;padding:64px clamp(40px,6vw,104px);border-inline-start:1px solid var(--line)}
.authside::before{content:"";position:absolute;inset:0;z-index:-1;opacity:.3;background:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='72' height='72'%3E%3Cpath d='M18 18H54V54H18ZM36 10.5L61.5 36L36 61.5L10.5 36ZM36 0V10.5M36 61.5V72M0 36H10.5M61.5 36H72' fill='none' stroke='%23d2a955' stroke-width='1'/%3E%3C/svg%3E");-webkit-mask-image:radial-gradient(ellipse 80% 70% at 100% 0,#000 0,transparent 72%);mask-image:radial-gradient(ellipse 80% 70% at 100% 0,#000 0,transparent 72%)}
.authside h2{font-size:clamp(28px,2.7vw,40px);line-height:1.1;letter-spacing:-.03em;color:#fff;margin:0;max-width:13em}
.authside .ar{font-size:clamp(20px,1.8vw,26px);font-weight:600;line-height:1.5;color:#d2a955;margin:18px 0 0;padding-top:18px;border-top:1px solid rgb(210 169 85 / .5);max-width:20em;text-align:start}
.authside p{font-size:17px;line-height:1.6;color:#b9c4d8;max-width:30em;margin:24px 0 0}
.authside ul{list-style:none;padding:0;margin:28px 0 0;display:grid;gap:12px;max-width:30em}.authside li{padding-inline-start:22px;position:relative}
.authside li::before{content:"";position:absolute;inset-inline-start:0;top:.62em;width:8px;height:8px;border-radius:50%;background:#d2a955}}
.crumbs{font-size:14px;margin:0 0 14px;color:var(--muted)}.crumbs a{color:var(--muted)}
.card{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:24px}.card+.card{margin-top:16px}
h1{font-size:24px;line-height:1.25;margin:0 0 14px;font-weight:600;letter-spacing:-.02em}h2{font-size:18px;margin:0 0 10px;font-weight:600}
p{margin:0 0 12px}.muted{color:var(--muted);font-size:14px}ul{padding-inline-start:20px}
label{display:block;font-size:14px;font-weight:500;margin:14px 0 6px}
input,select,textarea{width:100%;padding:11px 12px;border:1px solid var(--line);border-radius:10px;background:var(--bg);color:var(--fg);font:inherit}
textarea{min-height:96px}textarea.json{font-family:ui-monospace,monospace;font-size:14px}
button,.button{display:inline-flex;align-items:center;justify-content:center;min-height:44px;margin-top:18px;padding:10px 18px;border:1px solid var(--accent);border-radius:8px;background:var(--accent);color:var(--accent-ink);font:inherit;font-weight:500;cursor:pointer;text-decoration:none;line-height:1.3}
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
.formcard{max-width:720px}.settings{max-width:720px}.settings .card{margin-bottom:16px}@media(min-width:760px){.formcard{padding:28px 32px}}
.tables{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:10px}
.tables a{display:block;background:var(--card);border:1px solid var(--line);border-radius:12px;padding:14px 16px;text-decoration:none;color:var(--fg)}
.tables a:hover{border-color:var(--accent)}.tables strong{display:block;font-weight:500}.tables span{font-size:14px;color:var(--muted);font-variant-numeric:tabular-nums}
.shell{max-width:1320px;margin:0 auto;padding:0 20px;display:grid;grid-template-columns:minmax(0,1fr);gap:24px}.sidenav{min-width:0}
@media(min-width:1000px){.shell{grid-template-columns:232px minmax(0,1fr);gap:32px}}
.appmain{min-width:0;max-width:none;margin:0;padding:24px 0 40px}body.app .bar-top .in{max-width:1320px}.grid2>.panel,.grid2>.panel+.panel{margin-top:0}.cur{text-transform:uppercase}
.sidenav{padding-top:24px}
@media(min-width:1000px){.sidenav{position:sticky;top:0;align-self:start;max-height:100vh;overflow:auto;padding-bottom:24px}}
.sidenav .who{margin:0 0 18px}.sidenav .who strong{display:block;font-size:17px;font-weight:600;overflow-wrap:anywhere}.sidenav .who .pill{margin-top:6px}
.sidenav .who a.site{display:inline-block;margin-top:8px;font-size:14px}
.sidenav h3{font-size:12px;font-weight:500;color:var(--muted);margin:18px 0 6px;letter-spacing:.02em}
.sidenav ul{list-style:none;margin:0;padding:0}.sidenav li a{display:block;padding:7px 10px;border-radius:8px;color:var(--fg);text-decoration:none;font-size:15px;border-inline-start:3px solid transparent}
.sidenav li a:hover{background:var(--accent-soft)}.sidenav li a[aria-current]{background:var(--card);border-inline-start-color:var(--gold);font-weight:500;box-shadow:0 1px 2px rgb(19 32 58 / .06)}
@media(max-width:999px){.sidenav{padding-top:16px}.sidenav .who{display:flex;flex-wrap:wrap;align-items:center;gap:6px 12px;margin:0 0 10px}.sidenav .who .pill{margin:0}.sidenav .who a.site{margin:0}
.sidenav nav{display:flex;gap:6px;overflow-x:auto;padding-bottom:6px;scrollbar-width:thin}.sidenav h3{display:none}.sidenav ul{display:contents}
.sidenav li a{white-space:nowrap;border:1px solid var(--line);border-radius:999px;padding:6px 12px;background:var(--card);font-size:14px}.sidenav li a[aria-current]{border-color:var(--fg);background:var(--fg);color:var(--bg)}
.appmain{padding-top:8px}.kpi{padding:14px}.kpi .v{font-size:22px}.pagehead h1{font-size:24px}}
.pagehead{display:flex;flex-wrap:wrap;gap:12px 20px;align-items:flex-end;justify-content:space-between;margin-bottom:16px}.pagehead h1{margin:0;font-size:30px;line-height:1.15}
.tabs{display:flex;gap:4px 24px;flex-wrap:wrap;border-bottom:1px solid var(--line);margin:-4px 0 18px}
.tabs a{padding:8px 0 10px;margin-bottom:-1px;font-size:15px;color:var(--muted);text-decoration:none;border-bottom:2px solid transparent}.tabs a:hover{color:var(--fg)}
.tabs a[aria-current]{color:var(--fg);font-weight:500;border-bottom-color:var(--gold)}
.fit{max-height:calc(100vh - 350px);min-height:240px;overflow:auto}.fit thead th{position:sticky;top:0;background:var(--card);z-index:1}
.pagehead .sub{color:var(--muted);font-size:16px;margin:6px 0 0;max-width:46em}.pagehead .acts{display:flex;gap:8px;flex-wrap:wrap}.pagehead .acts .button{margin:0}
.seg{display:inline-flex;border:1px solid var(--line);border-radius:10px;overflow:hidden;background:var(--card)}.seg a{padding:7px 12px;font-size:14px;color:var(--fg);text-decoration:none}.seg a+a{border-inline-start:1px solid var(--line)}
.seg a[aria-current]{background:var(--fg);color:var(--bg)}
.kpis{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:12px;margin-bottom:16px}
.kpi{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:16px 18px}.kpi .l{font-size:13px;color:var(--muted)}
.kpi .v{font-size:28px;font-weight:600;letter-spacing:-.02em;line-height:1.2;margin:4px 0 2px;font-variant-numeric:tabular-nums;overflow-wrap:anywhere}.kpi .n{font-size:13px;color:var(--muted)}
.delta{font-size:12.5px;font-weight:500;padding:1px 7px;border-radius:999px;background:var(--bg);margin-inline-start:6px;white-space:nowrap}.delta.up{color:var(--live)}.delta.down{color:var(--bad)}
.grid2{display:grid;gap:16px}@media(min-width:900px){.grid2{grid-template-columns:1fr 1fr}}
.panel{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:18px 20px;min-width:0}.panel+.panel,.grid2+.panel,.panel+.grid2,.grid2+.grid2{margin-top:16px}
.panel h2{font-size:16px;margin:0}.panel .ph{display:flex;justify-content:space-between;align-items:baseline;gap:12px;margin-bottom:14px;flex-wrap:wrap}.panel .ph .muted{font-size:13px}
.chart{position:relative}.chart .plot{display:flex;align-items:flex-end;gap:2px;height:168px;border-bottom:1px solid var(--line);background:linear-gradient(var(--line),var(--line)) top/100% 1px no-repeat}
.chart .b{flex:1;position:relative;height:100%;display:flex;align-items:flex-end;outline:none}
.chart .b i{display:block;width:100%;background:var(--series);border-radius:4px 4px 0 0;min-height:0}.chart .b.today i{background:var(--gold)}
.chart .b:hover i,.chart .b:focus i{filter:brightness(1.25)}
.chart .b .tip{display:none;position:absolute;bottom:calc(100% + 6px);left:50%;transform:translateX(-50%);background:var(--fg);color:var(--bg);font-size:12.5px;line-height:1.35;padding:6px 9px;border-radius:7px;white-space:nowrap;z-index:5;pointer-events:none}
.chart .b:hover .tip,.chart .b:focus .tip{display:block}.chart .b:first-child .tip{left:0;transform:none}.chart .b:last-child .tip{left:auto;right:0;transform:none}
.chart .top{position:absolute;top:-9px;inset-inline-end:0;font-size:12px;color:var(--muted);background:var(--card);padding-inline-start:6px}
.chart .ticks{display:flex;justify-content:space-between;font-size:12px;color:var(--muted);margin-top:6px}
.chart .empty{height:168px;display:flex;align-items:center;justify-content:center;text-align:center;color:var(--muted);font-size:14px;border:1px dashed var(--line);border-radius:10px;padding:16px}
details.astable{margin-top:10px;font-size:13px}details.astable summary{cursor:pointer;color:var(--muted)}details.astable table{margin-top:8px}
.bars{list-style:none;margin:0;padding:0;display:grid;gap:8px}.bars li{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:4px 12px;font-size:14px}
.bars .nm{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.bars .ct{font-variant-numeric:tabular-nums;color:var(--muted)}
.bars .tr{grid-column:1/-1;height:6px;border-radius:3px;background:var(--bg)}.bars .tr i{display:block;height:100%;border-radius:3px;background:var(--series)}
.toolbar{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin-bottom:12px}.toolbar form.search{display:flex;gap:6px;flex:1;min-width:220px;max-width:420px}.toolbar form.search input{margin:0}
.toolbar .button,.toolbar button{margin:0}.toolbar .sp{flex:1}
details.pick{position:relative}details.pick summary{list-style:none;cursor:pointer;padding:10px 14px;border:1px solid var(--line);border-radius:8px;background:var(--card);font-size:15px}details.pick summary::-webkit-details-marker{display:none}
details.pick .menu{position:absolute;z-index:20;inset-inline-end:0;top:calc(100% + 6px);background:var(--card);border:1px solid var(--line);border-radius:12px;padding:12px 14px;min-width:240px;max-height:60vh;overflow:auto;box-shadow:0 12px 32px -12px rgb(0 0 0 / .3)}
details.pick .menu label{display:flex;gap:8px;align-items:center;margin:6px 0;font-weight:400}details.pick .menu input{width:auto}details.pick .menu button{width:100%;margin-top:10px}
table.rx{border-collapse:separate;border-spacing:0;width:100%;font-size:14.5px}
table.rx th{position:sticky;top:0;background:var(--card);z-index:1;text-align:start;font-weight:500;font-size:13px;color:var(--muted);padding:10px 12px;border-bottom:1px solid var(--line);white-space:nowrap}
table.rx th a{color:inherit;text-decoration:none}table.rx th a:hover{color:var(--fg)}table.rx th .dir{color:var(--gold);margin-inline-start:4px}
table.rx td{padding:11px 12px;border-bottom:1px solid var(--line);vertical-align:middle;max-width:280px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
table.rx tbody tr:hover td{background:var(--accent-soft)}table.rx td.id a{font-weight:600;font-variant-numeric:tabular-nums}
table.rx td.num{font-variant-numeric:tabular-nums}table.rx img{width:40px;height:40px;object-fit:cover;border-radius:8px;display:block}
.tablebox{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);overflow:auto;max-height:72vh}
.tag{display:inline-block;font-size:12.5px;font-weight:500;padding:2px 9px;border-radius:999px;background:var(--bg);border:1px solid var(--line);white-space:nowrap}
.pager{display:flex;flex-wrap:wrap;gap:6px 14px;align-items:center;justify-content:space-between;margin-top:14px;font-size:14px}
.pager .pages{display:flex;gap:4px;flex-wrap:wrap}.pager .pages a,.pager .pages span{min-width:36px;height:36px;display:inline-flex;align-items:center;justify-content:center;border-radius:8px;border:1px solid var(--line);background:var(--card);color:var(--fg);text-decoration:none;padding:0 8px}
.pager .pages a[aria-current]{background:var(--fg);color:var(--bg);border-color:var(--fg)}.pager .pages span{border:0;background:none}
.pager form{display:flex;gap:6px;align-items:center}.pager select{width:auto;padding:6px 8px;margin:0}
@media(max-width:760px){.tablebox{background:none;border:0;max-height:none;overflow:visible}
table.rx,table.rx tbody{display:block}table.rx thead{display:none}
table.rx tr{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:2px 12px;background:var(--card);border:1px solid var(--line);border-radius:14px;padding:12px 14px;margin-bottom:10px}
table.rx tbody tr:hover td{background:none}
table.rx td{grid-column:1/-1;display:flex;justify-content:space-between;align-items:center;gap:12px;border:0;padding:4px 0;max-width:none;white-space:normal;text-align:end}
table.rx td::before{content:attr(data-label);color:var(--muted);font-size:13px;text-align:start;flex:none}
table.rx td.t{grid-column:1;grid-row:1;font-weight:600;font-size:16px;text-align:start;display:block}table.rx td.t::before,table.rx td.id::before{display:none}
table.rx td.id{grid-column:2;grid-row:1;justify-content:flex-end}table.rx td.act{justify-content:flex-end}table.rx td.act::before{display:none}}
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
<p class="muted" style="margin-top:16px"><a href="/abuse">Report an app</a></p></div>{{template "authbottom"}}{{end}}

{{define "abuse"}}{{template "top" "Report an app"}}<form class="card" method="post" action="/abuse"><h1>Report an app</h1>
<p class="muted">Tell us about an app on Renoxium that is used for phishing, scams, malware or anything illegal.</p>
{{with .Error}}<p class="error">{{.}}</p>{{end}}<input type="hidden" name="csrf" value="{{.CSRF}}">
<label for="address">App address</label><input id="address" name="address" required placeholder="https://name.renoxium.app" value="{{.Address}}">
<label for="details">What is wrong?</label><textarea id="details" name="details" required>{{.Details}}</textarea>
<label for="contact">Your email (optional, if you want an answer)</label><input id="contact" name="contact" type="email" value="{{.Contact}}">
<button>Send report</button></form>{{template "bottom"}}{{end}}

{{define "dashboard"}}{{template "owntop" .}}{{template "owncss"}}<style>
.dash{display:grid;gap:20px}@media(min-width:1100px){.dash{grid-template-columns:minmax(0,1fr) 320px;align-items:start}}
.apps{display:grid;gap:16px}@media(min-width:640px){.apps{grid-template-columns:1fr 1fr}}
.appcard{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:20px;display:flex;flex-direction:column;gap:14px;transition:border-color .15s,box-shadow .15s}
.appcard:hover{border-color:color-mix(in srgb,var(--fg) 35%,var(--line));box-shadow:0 10px 28px -20px rgb(19 32 58 / .5)}
.appcard h2{font-size:18px;margin:0}.appcard h2 a{color:var(--fg);text-decoration:none}.appcard h2 a:hover{color:var(--accent)}
.appcard .top{display:flex;justify-content:space-between;gap:8px;align-items:flex-start;flex-wrap:wrap}.appcard .host{font-size:13.5px;color:var(--muted);overflow-wrap:anywhere;margin:2px 0 0}
.figs{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;background:var(--bg);border-radius:10px;padding:12px 14px;margin:0}
.figs div{min-width:0}.figs dt{font-size:12.5px;color:var(--muted)}.figs dd{margin:0;font-weight:600;font-size:16px;font-variant-numeric:tabular-nums;overflow-wrap:anywhere}.figs dd.bad{color:var(--bad)}
.appcard .acts{display:flex;gap:6px 16px;flex-wrap:wrap;align-items:center;font-size:14.5px;margin-top:auto}.appcard .acts .button{margin:0;padding:8px 14px}
.draft{font-size:14px;color:var(--muted);margin:0;background:var(--bg);border-radius:10px;padding:12px 14px}
.empty{background:var(--card);border:1px dashed var(--line);border-radius:var(--radius);padding:28px}.empty h2{font-size:20px}
.say{background:var(--accent-soft);border-radius:12px;padding:10px 14px;font-size:15px;margin:8px 0 0}
.steps{list-style:none;counter-reset:s;padding:0;margin:0 0 12px;display:grid;gap:12px;font-size:14.5px}.steps li{counter-increment:s;display:grid;grid-template-columns:26px 1fr;gap:10px;align-items:start}
.steps li::before{content:counter(s);width:26px;height:26px;border-radius:50%;background:var(--accent-soft);display:inline-flex;align-items:center;justify-content:center;font-size:13px;font-weight:600}
.copy{display:flex;gap:6px;margin-top:8px}.copy input{font-size:13px;font-family:ui-monospace,monospace;padding:8px 10px;margin:0}.copy button{margin:0;padding:8px 12px;font-size:13px}
.side{display:grid;gap:16px}.side .panel+.panel{margin-top:0}
</style>
<div class="pagehead"><div><h1>Your apps</h1><p class="sub"><a href="/billing">{{.Plan}} plan</a> · {{len .Apps}} of {{.AppLimit}} app{{if ne .AppLimit 1}}s{{end}} used</p></div>
<div class="acts"><a class="button secondary" href="/templates">Browse templates</a><a class="button" href="/connect">New app</a></div></div>
{{if not .User.Verified}}<form class="flash" method="post" action="/verify/resend" style="justify-content:space-between;align-items:center;flex-wrap:wrap"><span><strong>Confirm your email.</strong> You need it to publish apps and to reset your password if you forget it.</span><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="secondary" style="margin:0">Send the link again</button></form>{{end}}
{{if .Apps}}<div class="kpis"><div class="kpi"><div class="l">Apps</div><div class="v">{{len .Apps}}</div><div class="n">of {{.AppLimit}} on your plan</div></div>
<div class="kpi"><div class="l">Live</div><div class="v">{{.Live}}</div><div class="n">published sites</div></div>
<div class="kpi"><div class="l">Customers</div><div class="v">{{.Customers}}</div><div class="n">accounts across your apps</div></div>
<div class="kpi"><div class="l">Recent errors</div><div class="v"{{if .Errors}} style="color:var(--bad)"{{end}}>{{.Errors}}</div><div class="n">on live sites</div></div></div>{{end}}
<div class="dash"><div>
{{if .Apps}}<div class="apps">{{range .Apps}}{{template "app_card" .}}{{end}}</div>
{{else}}<div class="empty"><h2>No apps yet</h2><p>Connect Renoxium to ChatGPT or Claude, then ask for what you need. Your assistant starts from a finished template and shows you a preview before anything goes live.</p>
<p class="say">"Build me an online store for my perfume shop in Dubai."</p><p class="say">"Make a booking site for my salon with three stylists."</p>
<p style="margin-top:16px"><a href="/templates">See the templates you can start from</a></p></div>{{end}}
{{if .Shared}}<h2 style="margin:28px 0 12px;font-size:18px">Shared with you</h2><div class="apps">{{range .Shared}}{{template "app_card" .}}{{end}}</div>{{end}}
</div><aside class="side"><section class="panel"><div class="ph"><h2>Build or change an app</h2></div>
<ol class="steps"><li><div>Add Renoxium to your AI assistant once, with this address:<div class="copy"><input id="mcp" readonly value="{{.MCP}}" aria-label="Connector address" onfocus="this.select()"><button type="button" class="secondary" onclick="var i=document.getElementById('mcp');i.select();navigator.clipboard&&navigator.clipboard.writeText(i.value);this.textContent='Copied'">Copy</button></div></div></li>
<li><div>Ask for a new app or a change.</div></li><li><div>Check the preview, then publish.</div></li></ol>
<p class="muted"><a href="/connect">Step-by-step for ChatGPT and Claude</a></p></section>
<section class="panel"><div class="ph"><h2>Start from a template</h2></div><p class="muted">Online store, appointments, home services, tours and rentals, quotes, property listings. Try each one live.</p><a href="/templates">Browse templates →</a></section></aside></div>{{template "appbottom"}}{{end}}

{{define "app_card"}}<article class="appcard"><div class="top"><div><h2><a href="/apps/{{.App}}">{{.App}}</a></h2>{{if .LiveVersion}}<p class="host">{{.Host}}</p>{{end}}</div>
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
<button>Create account</button><p class="muted" style="margin-top:16px">Already have an account? <a href="/login?next={{.Next}}">Sign in</a></p></form>{{template "authbottom"}}{{end}}

{{define "login"}}{{template "auth" "Sign in"}}<form class="card" method="post" action="/login"><h1>Sign in to Renoxium</h1>
{{with .Error}}<p class="error">{{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="next" value="{{.Next}}">
<label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required value="{{.Email}}">
<label for="password">Password</label><input id="password" name="password" type="password" autocomplete="current-password" required>
<button>Sign in</button><p class="muted" style="margin-top:16px"><a href="/forgot">Forgot your password?</a></p><p class="muted">New here? <a href="/signup?next={{.Next}}">Create an account</a></p></form>{{template "authbottom"}}{{end}}

{{define "consent"}}{{template "auth" "Allow access"}}<form class="card" method="post" action="/oauth/authorize"><h1>Allow {{.Client}}?</h1>
<p><strong>{{.Client}}</strong> wants to create, change and publish your Renoxium apps, and read their logs.</p>
{{with .Warning}}<p class="error"><strong>Warning:</strong> {{.}}</p>{{end}}
<p>After you allow, you return to <code>{{.RedirectHost}}</code>. Only allow if you just connected Renoxium in your agent and you recognise this address.</p>
<p class="muted">Signed in as {{.User.Email}} · <a href="{{.Switch}}">use another account</a></p>
<input type="hidden" name="csrf" value="{{.CSRF}}">
{{range $k, $v := .Params}}<input type="hidden" name="{{$k}}" value="{{index $v 0}}">{{end}}
<div class="row"><button name="decision" value="allow">Allow</button><button class="secondary" name="decision" value="deny">Cancel</button></div></form>{{template "authbottom"}}{{end}}

{{define "demo_note"}}<p class="note" style="border:1px dashed var(--accent);background:var(--accent-soft)">
This is the owner's admin panel of a template demo, shown read-only. In your own app you add, change and delete here, on your phone or computer. The data is public and deleted every hour.</p>{{end}}

{{define "admin_app"}}{{template "apptop" .}}
<div class="pagehead"><div><h1>Overview</h1><p class="sub">{{if .Shell.Demo}}What the owner of this template sees every day.{{else if not .Shell.IsOwner}}You have staff access to this app.{{else}}How {{.Info.App}} is doing, in UAE time.{{end}}</p></div>
<div class="acts"><nav class="seg" aria-label="Period">{{range .Periods}}<a href="{{.Href}}"{{if .Current}} aria-current="true"{{end}}>{{.Label}}</a>{{end}}</nav>
{{if and .Shell.IsOwner (gt .Info.PreviewVersion .Info.LiveVersion)}}<a class="button" href="/apps/{{.Info.App}}/publish?version={{.Info.PreviewVersion}}">Publish version {{.Info.PreviewVersion}}</a>{{end}}
{{if not .Shell.Demo}}<a class="button secondary" href="/apps/{{.Info.App}}/preview">Open preview</a>{{end}}</div></div>
<div class="kpis">{{range .KPIs}}<div class="kpi"><div class="l">{{.Label}}</div><div class="v">{{.Value}}</div><div class="n">{{if .Change}}<span class="delta{{if .Up}} up{{end}}{{if .Down}} down{{end}}" title="against the {{$.Days}} days before">{{.Change}}</span> {{end}}{{.Note}}</div></div>{{end}}</div>
<section class="panel"><div class="ph"><h2>{{.VisitorsChart.Title}}</h2><span class="muted">last {{.Days}} days · gold is today</span></div>{{template "chart" .VisitorsChart}}</section>
<div class="grid2"><section class="panel"><div class="ph"><h2>Most viewed pages</h2><span class="muted">page views</span></div>{{if .Pages}}<ul class="bars">{{range .Pages}}<li><span class="nm">{{.Name}}</span><span class="ct">{{.Count}}</span><span class="tr"><i style="width:{{.Width}}%"></i></span></li>{{end}}</ul>{{else}}<p class="muted">No visits yet.</p>{{end}}</section>
<section class="panel"><div class="ph"><h2>Where visitors come from</h2><span class="muted">page views</span></div>{{if .Sources}}<ul class="bars">{{range .Sources}}<li><span class="nm">{{.Name}}</span><span class="ct">{{.Count}}</span><span class="tr"><i style="width:{{.Width}}%"></i></span></li>{{end}}</ul><p class="muted" style="margin:12px 0 0">Add <code>?utm_source=instagram</code> to links you share to see each channel here.</p>{{else}}<p class="muted">No visits yet.</p>{{end}}</section></div>
<section class="panel"><div class="ph"><h2>{{.SalesChart.Title}}</h2><span class="muted">total {{.SalesChart.Total}}</span></div>{{template "chart" .SalesChart}}</section>
{{if .Shell.IsOwner}}<section class="panel"><div class="ph"><h2>Latest payments</h2><a href="/apps/{{.Info.App}}/payments">All payments</a></div>{{if .Recent}}<div class="tablebox" style="border:0;max-height:none"><table class="rx"><thead><tr><th>Order</th><th>Status</th><th>Amount</th><th>Customer</th><th>Date</th></tr></thead><tbody>
{{range .Recent}}<tr><td class="t" data-label="Order">{{.Reference}}</td><td data-label="Status"><span class="tag">{{.Status}}</span></td><td class="num" data-label="Amount"><span>{{printf "%.2f" .Amount}} <span class="cur">{{.Currency}}</span></span></td><td data-label="Customer">{{.CustomerEmail}}</td><td data-label="Date">{{.CreatedAt}}</td></tr>{{end}}</tbody></table></div>{{else}}<p class="muted" style="margin:0">No card payments yet. <a href="/apps/{{.Info.App}}/payments">Connect Stripe</a> to take cards; cash orders are in your tables.</p>{{end}}</section>{{end}}
<section class="panel"><div class="ph"><h2>Your data</h2><span class="muted">tap a table to search, sort and edit</span></div><div class="tables">{{range .Tiles}}<a href="{{.Href}}"><strong>{{.Label}}</strong><span>{{.Rows}}{{with .Week}} · <b style="color:var(--live);font-weight:500">{{.}}</b>{{end}}</span></a>{{end}}</div></section>
{{if .Problems}}<section class="panel"><div class="ph"><h2>Recent errors</h2><span class="muted">ask your assistant to "check the Renoxium logs and fix the errors"</span></div><ul class="errs" style="list-style:none;padding:0;margin:0">{{range .Problems}}<li style="padding:8px 0;border-top:1px solid var(--line);font-size:14px;overflow-wrap:anywhere"><span class="muted">{{.Time}}</span><br>{{.Message}}</li>{{end}}</ul></section>{{end}}
<div class="grid2">{{if and .Shell.IsOwner .Jobs}}<section class="panel"><div class="ph"><h2>Scheduled jobs</h2></div><table class="rx"><thead><tr><th>Job</th><th>Runs</th><th>Last run</th></tr></thead><tbody>
{{range .Jobs}}<tr><td class="t" data-label="Job">{{.Name}}</td><td data-label="Runs">{{.Schedule}}</td><td data-label="Last run">{{if .Status}}{{.LastRun.UTC.Format "2 Jan 15:04"}} UTC · {{if eq .Status "ok"}}<span class="ok">ok</span>{{else}}<span class="error">{{.Message}}</span>{{end}}{{else}}not yet{{end}}</td></tr>{{end}}</tbody></table></section>{{end}}
<section class="panel"><div class="ph"><h2>Addresses</h2></div><dl class="addr" style="margin:0">{{with .Shell.SiteURL}}<dt class="muted" style="font-size:13px">Your site</dt><dd style="margin:0 0 10px;overflow-wrap:anywhere"><a href="{{.}}/">{{.}}</a></dd>{{end}}
{{with .MCPURL}}<dt class="muted" style="font-size:13px">For customers' AI assistants</dt><dd style="margin:0"><input readonly value="{{.}}" aria-label="Address for AI assistants" onfocus="this.select()" style="font:13px ui-monospace,monospace;padding:8px 10px;margin-top:4px"></dd>{{end}}</dl>
{{if .Shell.Demo}}<p style="margin:14px 0 0"><a class="button" href="/signup" style="margin:0">Create your account</a></p>{{end}}</section></div>
{{template "appbottom"}}{{end}}

{{define "publish"}}{{template "apptop" .}}<form class="card formcard" method="post" action="/apps/{{.Info.App}}/publish"><h1>{{if .Rollback}}Roll back{{else}}Publish{{end}} version {{.Version}}?</h1>
<p>Your agent asked to make version {{.Version}} of <strong>{{.Info.App}}</strong> live at <a href="{{.Info.LiveURL}}">{{.Info.LiveURL}}</a>.</p>
<p class="muted">Live now: {{if .Info.LiveVersion}}version {{.Info.LiveVersion}}{{else}}nothing yet{{end}}. <a href="/apps/{{.Info.App}}/preview">Check the preview</a> first if you have not.</p>
<p class="muted">Live data is backed up before the change.</p>
<input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="version" value="{{.Version}}">
<button>{{if .Rollback}}Roll back{{else}}Publish{{end}}</button></form>{{template "appbottom"}}{{end}}

{{define "admin_list"}}{{template "apptop" .}}
<div class="pagehead"><div><h1>{{.Label}}</h1><p class="sub">{{.Pager.Of}} {{if eq .Total 1}}row{{else}}rows{{end}}{{if .Query}} matching "{{.Query}}"{{end}}</p></div>
<div class="acts">{{if .HasCalendar}}<nav class="seg" aria-label="View"><a href="/apps/{{.App}}/t/{{.Table}}" aria-current="true">List</a><a href="/apps/{{.App}}/t/{{.Table}}/calendar">Calendar</a></nav>{{end}}
{{if not .Demo}}<a class="button" href="/apps/{{.App}}/t/{{.Table}}/new">Add</a>{{end}}</div></div>
{{with .Notice}}<p class="error">{{.}}</p>{{end}}
<div class="toolbar"><form class="search" method="get" role="search">{{range $k, $v := .Keep}}{{if ne $k "q"}}<input type="hidden" name="{{$k}}" value="{{index $v 0}}">{{end}}{{end}}{{with .ColsParam}}<input type="hidden" name="cols" value="{{.}}">{{end}}
<input name="q" value="{{.Query}}" placeholder="Search {{.Label}}" aria-label="Search"><button class="secondary">Search</button></form><span class="sp"></span>
<details class="pick"><summary>Columns</summary><form class="menu" method="get">{{range $k, $v := .Keep}}<input type="hidden" name="{{$k}}" value="{{index $v 0}}">{{end}}
<p class="muted" style="margin:0 0 4px">Show these columns</p>{{range .Choices}}<label><input type="checkbox" name="c" value="{{.Name}}"{{if .Shown}} checked{{end}}> {{.Label}}</label>{{end}}<button>Apply</button></form></details></div>
{{if .Rows}}<div class="tablebox"><table class="rx"><thead><tr>{{range .Columns}}<th scope="col"{{with .SortDir}} aria-sort="{{if eq . "asc"}}ascending{{else}}descending{{end}}"{{end}}><a href="{{.SortHref}}">{{.Label}}{{with .SortDir}}<span class="dir" aria-hidden="true">{{if eq . "asc"}}↑{{else}}↓{{end}}</span>{{end}}</a></th>{{end}}</tr></thead><tbody>
{{range .Rows}}{{$id := .ID}}<tr><td class="id" data-label="#"><a href="/apps/{{$.App}}/t/{{$.Table}}/{{$id}}" aria-label="Open row {{$id}}">#{{$id}}</a></td>{{range .Cells}}<td{{if .Title}} class="t"{{end}} data-label="{{.Label}}">{{if .Image}}<img src="{{.Image}}" alt="" loading="lazy">{{else if .Pill}}<span class="tag">{{.Pill}}</span>{{else if .Title}}<a href="/apps/{{$.App}}/t/{{$.Table}}/{{$id}}" style="color:inherit;text-decoration:none">{{if .Text}}{{.Text}}{{else}}—{{end}}</a>{{else}}{{.Text}}{{end}}</td>{{end}}</tr>{{end}}
</tbody></table></div>
<div class="pager">{{template "pager" .Pager}}<form method="get">{{range $k, $v := .Keep}}{{if ne $k "size"}}<input type="hidden" name="{{$k}}" value="{{index $v 0}}">{{end}}{{end}}{{with .ColsParam}}<input type="hidden" name="cols" value="{{.}}">{{end}}
<label for="size" class="muted" style="margin:0;font-weight:400">Rows per page</label><select id="size" name="size" onchange="this.form.submit()">{{range .Sizes}}<option{{if eq . $.Size}} selected{{end}}>{{.}}</option>{{end}}</select><noscript><button class="secondary">Go</button></noscript></form></div>
{{else}}<div class="panel"><p style="margin:0">{{if .Query}}Nothing matches "{{.Query}}". <a href="?">Clear the search</a>{{else}}No rows yet.{{if not .Demo}} <a href="/apps/{{.App}}/t/{{.Table}}/new">Add the first one</a>.{{end}}{{end}}</p></div>{{end}}
{{template "appbottom"}}{{end}}

{{define "operator"}}{{template "owntop" .}}{{template "owncss"}}<style>
.optable td{white-space:nowrap}.optable form.inline input{width:150px}.optable .em{max-width:280px;overflow:hidden;text-overflow:ellipsis}
.alerts{margin:0 0 16px}.alerts li{margin:4px 0}
</style>
<div class="pagehead"><div><h1>Platform</h1><p class="sub">Accounts, apps and abuse reports across Renoxium.</p></div></div>{{template "tabs" .Tabs}}
{{if eq .Tab "overview"}}{{with .Health}}{{range .Problems}}<p class="flash bad error">{{.}}</p>{{end}}{{end}}
<div class="kpis">
<div class="kpi"><div class="l">Accounts</div><div class="v">{{.Accounts}}</div><div class="n">Free {{index .PerPlan "free"}} · Pro {{index .PerPlan "pro"}} · Business {{index .PerPlan "business"}}</div></div>
<div class="kpi"><div class="l">Subscription revenue</div><div class="v">AED {{.MRR}}</div><div class="n">per month · paying Pro {{index .Paying "pro"}}, Business {{index .Paying "business"}}</div></div>
<div class="kpi"><div class="l">Apps</div><div class="v">{{.Stats.Apps}}</div><div class="n">{{.Stats.LiveApps}} live · {{.Stats.SuspendedApps}} suspended · {{.Stats.StripeReady}} take cards</div></div>
<div class="kpi"><div class="l">WhatsApp codes</div><div class="v">{{.Stats.WhatsAppCodes}}</div><div class="n">this month, all accounts</div></div>
{{with .Health}}<div class="kpi"><div class="l">Server</div><div class="v">{{.DiskFreeGB}} GB</div><div class="n">free of {{.DiskTotalGB}} GB · backup {{.BackupAge}} · off-site {{.OffsiteAge}}</div></div>{{end}}
</div>
{{with .Health}}{{with .Alerts}}<details class="panel alerts"><summary>Recent alerts</summary><ul>{{range .}}<li>{{.}}</li>{{end}}</ul></details>{{end}}{{end}}
{{if .Reports}}<section class="panel"><div class="ph"><h2>Abuse reports <span class="muted">({{.OpenReports}} open)</span></h2></div>
{{range .Reports}}<div class="report{{if .Resolved}} done{{end}}"><p><strong>{{.Address}}</strong> <span class="muted">{{.CreatedAt}}{{with .Contact}} · {{.}}{{end}}</span></p><p>{{.Details}}</p>
{{if not .Resolved}}<form method="post" action="/operator/abuse/{{.ID}}/resolve"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="secondary" style="margin-top:0">Mark handled</button></form>{{end}}</div>{{end}}</section>{{end}}{{if not .Reports}}<p class="muted">No abuse reports.</p>{{end}}
<p class="muted" style="margin-top:14px">Card fees and subscription payments are in the Stripe Dashboard.</p>
{{else if eq .Tab "accounts"}}<form method="get" class="toolbar"><div class="search" style="display:flex;gap:6px;flex:1 1 300px;min-width:260px;max-width:460px"><input name="q" value="{{.Q}}" placeholder="{{.SearchHint}}" aria-label="Search" style="margin:0"><button class="secondary" style="margin:0">Search</button></div>{{if .Q}}<a href="{{.Here}}">Clear</a>{{end}}</form>
<section class="panel"><div class="ph"><h2>Accounts</h2><span class="muted">Setting a plan here grants it without payment; Stripe subscriptions are managed in Stripe.</span></div><div class="scroll fit"><table class="optable"><thead><tr><th>Email</th><th>Plan</th><th>Joined</th><th>Give plan</th></tr></thead><tbody>
{{range .Users}}<tr><td class="em"><a href="/operator/apps?q={{.Email}}">{{.Email}}</a>{{if not .Verified}} <span class="muted">unconfirmed</span>{{end}}</td><td><span class="tag">{{.Plan}}{{with .Status}} <span class="muted">{{.}}</span>{{end}}</span></td><td class="muted">{{.CreatedAt}}</td>
<td><form method="post" action="/operator/plan" class="inline"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="user" value="{{.ID}}"><input type="hidden" name="q" value="{{$.Q}}">
<select name="plan" aria-label="Plan"><option value="free">free</option><option value="pro">pro</option><option value="business">business</option></select><button class="secondary">Set</button></form></td></tr>{{end}}
</tbody></table></div></section>

{{else}}<form method="get" class="toolbar"><div class="search" style="display:flex;gap:6px;flex:1 1 300px;min-width:260px;max-width:460px"><input name="q" value="{{.Q}}" placeholder="{{.SearchHint}}" aria-label="Search" style="margin:0"><button class="secondary" style="margin:0">Search</button></div>{{if .Q}}<a href="{{.Here}}">Clear</a>{{end}}</form>
<section class="panel"><div class="ph"><h2>Apps</h2></div><div class="scroll fit"><table class="optable"><thead><tr><th>App</th><th>Owner</th><th>Live</th><th>Stripe</th><th></th></tr></thead><tbody>
{{range .Apps}}<tr><td><a href="{{.LiveURL}}" target="_blank" rel="noopener">{{.Name}}</a>{{with .Suspended}} <span class="error">suspended: {{.}}</span>{{end}}</td><td class="em">{{.OwnerEmail}}</td>
<td>{{if .LiveVersion}}<span class="pill live">v{{.LiveVersion}}</span>{{else}}<span class="muted">not published</span>{{end}}</td><td>{{if .ChargesEnabled}}<span class="ok">ready</span>{{else if .StripeAccount}}setting up{{else}}<span class="muted">none</span>{{end}}</td>
<td><form method="post" action="/operator/suspend" class="inline"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="app" value="{{.Name}}"><input type="hidden" name="q" value="{{$.Q}}">
{{if .Suspended}}<button class="secondary" name="action" value="unsuspend">Bring back</button>{{else}}<input name="reason" placeholder="Reason" aria-label="Reason"><button class="danger" name="action" value="suspend" style="margin:0">Suspend</button>{{end}}</form></td></tr>{{end}}
</tbody></table></div></section>
{{end}}{{template "appbottom"}}{{end}}

{{define "operator_growth"}}{{template "owntop" .}}{{template "owncss"}}{{with .Growth}}{{template "growth" .}}{{else}}<div class="card"><h1>Growth</h1><p class="error">The growth figures could not be read; see the server log.</p></div>{{end}}{{template "appbottom"}}{{end}}

{{define "account"}}{{template "owntop" .}}{{template "owncss"}}
<div class="pagehead"><div><h1>Account settings</h1><p class="sub">Sign-in, your data and your account.</p></div></div>
<div class="settings-grid">
<section class="panel"><div class="ph"><h2>Profile</h2></div><dl class="kv"><dt>Email</dt><dd>{{.User.Email}} {{if .User.Verified}}<span class="pill live">confirmed</span>{{else}}<span class="pill wait">not confirmed</span>{{end}}</dd>
<dt>Plan</dt><dd>{{.PlanName}} · <a href="/billing">Plan and billing</a></dd><dt>Apps</dt><dd>{{len .Apps}}</dd></dl></section>
<form class="panel" method="post" action="/account/password"><div class="ph"><h2>Change password</h2></div>
{{if .Changed}}<p class="flash ok">Password changed. Other devices and connected agents were signed out.</p>{{end}}{{with .PasswordError}}<p class="flash bad error">{{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}"><label for="current">Current password</label><input id="current" name="current" type="password" autocomplete="current-password" required>
<label for="password">New password (at least 10 characters)</label><input id="password" name="password" type="password" autocomplete="new-password" minlength="10" required>
<button>Change password</button><p class="fine">Connected agents (ChatGPT, Claude) will need to connect again.</p></form>
<section class="panel"><div class="ph"><h2>Export your data</h2></div><p class="muted">A zip file with your account details and, for each app, its definition, its live data and the list of its files.</p>
<a class="button secondary" href="/account/export" style="margin-top:4px">Download export</a></section>
<form class="panel danger-zone" method="post" action="/account/delete"><div class="ph"><h2>Delete account</h2></div>
<p>This deletes your account and {{len .Apps}} app(s) with all their data, for good. Export first if you want a copy.{{if ne .Billing.Plan "free"}} Your paid plan is cancelled at once, without a refund for the rest of the period.{{end}}</p>{{with .DeleteError}}<p class="flash bad error">{{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}"><label for="confirm">Type your email address to confirm</label><input id="confirm" name="confirm" autocomplete="off" required>
<label for="dpassword">Password</label><input id="dpassword" name="password" type="password" autocomplete="current-password" required>
<button class="danger">Delete my account</button></form></div>{{template "appbottom"}}{{end}}

{{define "billing"}}{{template "owntop" .}}{{template "owncss"}}
<div class="pagehead"><div><h1>Plan and billing</h1><p class="sub">Your Renoxium plan, what you have used this month, and payments.</p></div></div>
{{with .Notice}}<p class="flash" role="status">{{.}}</p>{{end}}
<div class="grid2"><section class="panel"><div class="hero"><div><p class="muted" style="margin:0">Current plan: <strong>{{.PlanName}}</strong></p>
<p class="name">{{if .Granted}}Included{{else if .Paying}}AED {{index .Prices .Billing.Plan}}{{else}}AED 0{{end}} <span class="per">{{if .Granted}}at no charge{{else}}/ month{{end}}</span></p>
{{if .Granted}}<p><span class="pill plain">granted</span> <span class="muted">Given by Renoxium, no payment needed.</span></p>
{{else if .Paying}}<p><span class="pill {{if eq .Billing.Status "active" "trialing"}}live{{else}}wait{{end}}">{{.Billing.Status}}{{with .PeriodEnd}} · {{if $.Billing.CancelAtPeriodEnd}}ends{{else}}renews{{end}} {{.}}{{end}}</span></p>
<p class="muted">No Renoxium fee on your shops' card payments, and no Renoxium badge on your shops.</p>
{{else}}<p class="muted">On the Free plan Renoxium takes {{.Fee}}% of card payments in your shops and shows a small "Made with Renoxium" badge.</p>{{end}}</div>
{{if .CanManage}}<div><div class="acts"><form method="post" action="/billing/portal"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Manage billing</button></form></div>
<p class="fine" style="margin-top:8px">Change plan, update your card, download invoices or cancel.</p></div>{{end}}</div></section>

<section class="panel"><div class="ph"><h2>Usage</h2><span class="muted">resets on the 1st of each month</span></div><div class="meters">
{{range .Meters}}<div class="meter{{if .Warn}} warn{{end}}"><div class="top"><span>{{.Label}}</span><span><b>{{.Used}}{{.Unit}}</b> of {{.Quota}}{{.Unit}}</span></div><div class="tr" role="progressbar" aria-label="{{.Label}}" aria-valuenow="{{.Pct}}" aria-valuemin="0" aria-valuemax="100"><i style="width:{{.Pct}}%"></i></div></div>{{end}}
</div></section>

</div><section class="panel" style="margin-top:16px"><div class="ph"><h2>Plans</h2><span class="muted">Prices in AED, billed monthly by Stripe. Cancel any time.</span></div>
{{if not .Ready}}<p class="flash">Paid plans are not available yet.</p>{{end}}
<div class="tiers" style="margin-top:18px">{{range .Plans}}<div class="tier{{if .Current}} current{{else if .Popular}} popular{{end}}">{{if .Current}}<span class="badge">Your plan</span>{{else if .Popular}}<span class="badge">Most popular</span>{{end}}
<h3>{{.Name}}</h3><span class="tag">{{.Tagline}}</span><p class="price">AED {{.Price}} <small>/ month</small></p>
<ul>{{range .Features}}<li>{{.}}</li>{{end}}</ul>
{{if .Current}}<span class="is">Current plan</span>
{{else if eq .ID "free"}}{{if $.CanManage}}<form method="post" action="/billing/portal"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="secondary">Cancel in billing</button></form>{{else}}<span class="is">Always free</span>{{end}}
{{else if not $.Ready}}<span class="is">Not available yet</span>
{{else if $.CanManage}}<form method="post" action="/billing/portal"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="secondary">Switch to {{.Name}}</button></form>
{{else}}<form method="post" action="/billing/checkout"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="plan" value="{{.ID}}"><button{{if not .Popular}} class="secondary"{{end}}>Choose {{.Name}}</button></form>{{end}}
</div>{{end}}</div>
<p class="fine">Payment is handled by Stripe. Cancel any time.</p></section>{{template "appbottom"}}{{end}}

{{define "admin_connections"}}{{template "apptop" .}}<div class="settings">
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
</div></div>{{template "appbottom"}}{{end}}

{{define "admin_alerts"}}{{template "apptop" .}}<div class="settings">
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
</div></div>{{template "appbottom"}}{{end}}

{{define "admin_domain"}}{{template "apptop" .}}<div class="settings">
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
</div></div>{{template "appbottom"}}{{end}}

{{define "admin_delete"}}{{template "apptop" .}}<div class="settings">
<div class="card"><h1>Export this app first</h1><p class="muted">A zip file with the app's definition, its live data and the list of its files. Keep it before you delete anything.</p>
<a class="button secondary" href="/apps/{{.App}}/export">Download export</a></div>
<form class="card danger-zone" method="post" action="/apps/{{.App}}/delete" style="margin-top:16px"><h1>Delete app for good</h1>
<div class="warnbox" role="alert"><p><strong>This cannot be undone.</strong> Renoxium cannot bring this app back, not even on request.</p>
<p>Deleting <strong>{{.App}}</strong> removes at once:</p><ul>
<li>the website{{with .Info.LiveURL}} at <strong>{{.}}</strong>{{end}}; your customers will get "not found"</li>
{{range .Losses}}<li><strong>{{.Count}}</strong> {{.Label}}</li>{{end}}
<li>every version, uploaded file, staff member, notification and custom domain</li></ul>
<p>Customers who signed in lose their accounts and order history. The address stays reserved for your account for 90 days. Your Stripe account and the money in it are not changed.</p></div>
{{with .Error}}<p class="error">{{.}}</p>{{end}}
{{if .Running}}<p class="error">{{.Running}} customer subscription(s) are still running. Cancel them in <a href="/apps/{{.App}}/payments">Payments</a> first, so nobody keeps being charged.</p>
{{else}}<input type="hidden" name="csrf" value="{{.CSRF}}"><label for="confirm">To confirm, type the app's name: <strong>{{.App}}</strong></label>
<input id="confirm" name="confirm" autocomplete="off" autocapitalize="off" spellcheck="false" required pattern="{{.App}}" data-name="{{.App}}">
<label for="dpassword">Your Renoxium password</label><input id="dpassword" name="password" type="password" autocomplete="current-password" required>
<button class="danger" id="delete-btn" disabled>Delete {{.App}} for good</button>
<script>(function(){var i=document.getElementById("confirm"),b=document.getElementById("delete-btn");function f(){b.disabled=i.value.trim()!==i.dataset.name}i.addEventListener("input",f);f()})()</script>{{end}}</form>
<style>.warnbox{border:1px solid var(--danger,#b42318);background:color-mix(in srgb,var(--danger,#b42318) 8%,transparent);border-radius:12px;padding:12px 16px;margin:0 0 16px}
.warnbox ul{margin:6px 0 10px;padding-left:20px}.warnbox p{margin:6px 0}button.danger[disabled]{opacity:.45;cursor:not-allowed}</style>
</div>{{template "appbottom"}}{{end}}

{{define "admin_rename"}}{{template "apptop" .}}<div class="settings">
<form class="card" method="post" action="/apps/{{.App}}/rename"><h1>Rename app</h1>
<p>The app's name is its address: <strong>{{.Info.LiveURL}}</strong>. A new name gives it a new address.</p>
<input type="hidden" name="csrf" value="{{.CSRF}}">
<label for="to">New name</label><input id="to" name="to" value="{{.To}}" autocomplete="off" autocapitalize="off" spellcheck="false" required pattern="[a-z][a-z0-9-]{1,38}[a-z0-9]">
<p class="help muted">3-40 lowercase letters, digits and hyphens. The address becomes <strong>{{if .To}}{{.To}}{{else}}new-name{{end}}.{{.Domain}}</strong>.</p>
<div class="note"><p><strong>What changes</strong></p><ul>
<li>The old addresses send visitors to the same page on the new ones for {{.RedirectDays}} days, and nobody else can take the old name in that time.</li>
<li>Signed-in customers sign in again once.</li>
<li>Data, versions, files, staff, payments and your own domain stay as they are.</li>
<li>You can rename an app once every 30 days.</li></ul></div>
{{with .Error}}<p class="error">{{.}}</p>{{end}}
<label for="rpassword">Your Renoxium password</label><input id="rpassword" name="password" type="password" autocomplete="current-password" required>
<button>Rename {{.App}}{{if .Ready}} to {{.To}}{{end}}</button></form>
</div>{{template "appbottom"}}{{end}}

{{define "admin_staff"}}{{template "apptop" .}}<div class="settings">
<div class="card"><h1>Staff</h1><p class="muted">Staff can edit your data (products, orders, discount codes), upload photos and see visitors. Only you can publish, connect Stripe, delete visitors and manage staff.</p>
{{with .Error}}<p class="error">{{.}}</p>{{end}}
{{if .Staff}}<ul class="plain">{{range .Staff}}<li><span>{{.}}</span><form method="post" action="/apps/{{$.App}}/staff/remove" class="inline"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="email" value="{{.}}"><button class="secondary">Remove</button></form></li>{{end}}</ul>
{{else}}<p>No staff yet.</p>{{end}}
<p class="muted">{{len .Staff}} of {{.Limit}} on your plan{{if eq .Limit 0}}. Staff need the <a href="/billing">Pro or Business plan</a>{{end}}.</p>
{{if lt (len .Staff) .Limit}}<form method="post" action="/apps/{{.App}}/staff"><input type="hidden" name="csrf" value="{{.CSRF}}"><label for="email">Email address</label>
<input id="email" name="email" type="email" required><button>Add staff member</button></form>
<p class="muted">They get an email. They sign in with a Renoxium account for this address and confirm the address first.</p>{{end}}</div></div>{{template "appbottom"}}{{end}}

{{define "admin_payments"}}{{template "apptop" .}}<div class="pagehead"><div><h1>Payments</h1><p class="sub">Card payments through your Stripe account.</p></div></div><div class="card">
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
</div><div class="pagehead" style="margin:28px 0 12px"><h2 style="margin:0;font-size:20px">All payments <span class="muted" style="font-weight:400">({{.Total}})</span></h2><nav class="seg" aria-label="Status">{{range .Filters}}<a href="{{.Href}}"{{if .Current}} aria-current="true"{{end}}>{{.Label}}</a>{{end}}</nav></div>
{{if .Payments}}<div class="tablebox"><table class="rx"><thead><tr><th>Order</th><th>Status</th><th>Amount</th><th>Customer</th><th>Started</th><th></th></tr></thead><tbody>
{{range .Payments}}<tr><td class="t" data-label="Order">{{.Reference}}</td><td data-label="Status"><span class="tag">{{if eq .Status "authorized"}}card hold{{with .HoldExpiresAt}}, take by {{.}} UTC{{end}}{{else}}{{.Status}}{{end}}</span></td>
<td class="num" data-label="Amount"><span>{{printf "%.2f" .Amount}} <span class="cur">{{.Currency}}</span></span>{{if gt .Refunded 0.0}}<br><span class="muted">{{printf "%.2f" .Refunded}} refunded</span>{{end}}</td><td data-label="Customer">{{.CustomerEmail}}</td><td data-label="Started">{{.CreatedAt}}</td>
<td class="act">{{if gt .Refundable 0.0}}<form method="post" action="/apps/{{$.App}}/payments/{{.ID}}/refund" class="inline" onsubmit="return confirm('Refund this payment to the card?')"><input type="hidden" name="csrf" value="{{$.CSRF}}">
<input name="amount" inputmode="decimal" placeholder="{{printf "%.2f" .Refundable}}" aria-label="Amount to refund (empty = {{printf "%.2f" .Refundable}})" style="width:7em;margin:0"> <button class="secondary" style="margin:0;padding:4px 10px">Refund</button></form>
{{else if eq .Status "authorized"}}<form method="post" action="/apps/{{$.App}}/payments/{{.ID}}/capture" class="inline"><input type="hidden" name="csrf" value="{{$.CSRF}}">
<input name="amount" inputmode="decimal" placeholder="{{printf "%.2f" .Authorized}}" aria-label="Amount to take (empty = all {{printf "%.2f" .Authorized}})" style="width:7em;margin:0"> <button style="margin:0;padding:4px 10px">Capture</button></form>
<form method="post" action="/apps/{{$.App}}/payments/{{.ID}}/release" class="inline" onsubmit="return confirm('Release the hold? Nothing is charged.')"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="secondary" style="margin:0;padding:4px 10px">Release</button></form>{{end}}</td></tr>{{end}}
</tbody></table></div>{{template "pager" .Pager}}<p class="muted" style="margin-top:12px">Refund: leave the amount empty to refund everything that is left, or type part of it. On the Free plan Renoxium's fee is refunded with it. Card holds: capture all or part (e.g. a damage charge) before the date shown, or release them; most cards drop a hold after about 7 days.</p>
{{else}}<div class="panel"><p class="muted" style="margin:0">{{if .Filter}}No payments with this status.{{else}}No payments yet.{{end}}</p></div>{{end}}
{{if .Subscriptions}}<h2 style="margin:28px 0 12px;font-size:20px">Subscriptions</h2><div class="tablebox"><table class="rx"><thead><tr><th>Reference</th><th>Status</th><th>Amount</th><th>Next payment</th><th>Customer</th><th></th></tr></thead><tbody>
{{range .Subscriptions}}<tr><td class="t" data-label="Reference">{{.Reference}}</td><td data-label="Status">{{.Status}}{{if .CancelAtPeriodEnd}}{{if ne .Status "canceled"}}, ends at period end{{end}}{{end}}</td>
<td data-label="Amount"><span>{{printf "%.2f" .Amount}} <span class="cur">{{.Currency}}</span></span> every {{if gt .IntervalCount 1}}{{.IntervalCount}} {{.Interval}}s{{else}}{{.Interval}}{{end}}<br><span class="muted">{{.PaymentsMade}} paid</span></td>
<td data-label="Next payment">{{if and (ne .Status "canceled") (not .CancelAtPeriodEnd)}}{{.CurrentPeriodEnd}}{{else}}—{{end}}</td><td data-label="Customer">{{.CustomerEmail}}</td>
<td class="act">{{if ne .Status "canceled"}}<form method="post" action="/apps/{{$.App}}/subscriptions/{{.ID}}/cancel" class="inline" onsubmit="return confirm('Cancel this subscription?')"><input type="hidden" name="csrf" value="{{$.CSRF}}">
<select name="when" aria-label="When" style="margin:0;width:auto"><option value="period_end">At period end</option><option value="now">Now</option></select> <button class="danger" style="margin:0;padding:4px 10px">Cancel</button></form>{{end}}</td></tr>{{end}}
</tbody></table></div><p class="muted" style="margin-top:12px">Cancelling never refunds a payment already made; refund it above if needed.</p>{{end}}{{template "appbottom"}}{{end}}

{{define "admin_visitors"}}{{template "apptop" .}}
<div class="pagehead"><div><h1>Customer accounts</h1><p class="sub">People who signed up on your site. Passwords are never shown to anyone.</p></div></div>
<div class="toolbar"><form class="search" method="get" role="search"><input name="q" value="{{.Query}}" placeholder="Search email, number or name" aria-label="Search"><button class="secondary">Search</button></form></div>
{{if .Visitors}}<div class="tablebox"><table class="rx"><thead><tr><th>Name</th><th>#</th><th>Email</th><th>WhatsApp</th><th>Confirmed</th><th>Files</th><th>Joined</th><th></th></tr></thead><tbody>
{{range .Visitors}}<tr><td class="t" data-label="Name">{{if .Name}}{{.Name}}{{else}}—{{end}}</td><td class="id" data-label="#">#{{.ID}}</td><td data-label="Email">{{.Email}}</td><td data-label="WhatsApp">{{.Phone}}</td><td data-label="Confirmed"><span class="tag">{{if .Verified}}Yes{{else}}No{{end}}</span></td><td class="num" data-label="Files">{{.Files}}</td><td data-label="Joined">{{.CreatedAt}}</td>
<td class="act"><form method="post" action="/apps/{{$.App}}/visitors/{{.ID}}/delete" onsubmit="return confirm('Delete this account and the files this person uploaded?')"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="danger" style="margin:0;padding:5px 12px">Delete</button></form></td></tr>{{end}}
</tbody></table></div>{{template "pager" .Pager}}
{{else}}<div class="panel"><p style="margin:0">{{if .Query}}Nobody matches "{{.Query}}".{{else}}No customer accounts yet. They appear when people sign up on your site.{{end}}</p></div>{{end}}
{{template "appbottom"}}{{end}}

{{define "admin_form"}}{{template "apptop" .}}<p class="crumbs"><a href="/apps/{{.App}}/t/{{.Table}}">{{.Label}}</a>{{with .Back}} / <a href="{{.}}">Back to calendar</a>{{end}}</p>
<form class="card formcard" method="post"{{if .HasImage}} enctype="multipart/form-data"{{end}}><h1>{{if .ID}}{{.Label}} #{{.ID}}{{else}}New {{.Label}}{{end}}</h1>{{if .Demo}}<fieldset disabled style="border:0;padding:0;margin:0">{{end}}
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
{{template "appbottom"}}{{end}}

{{define "templates_app"}}{{template "owntop" .}}{{template "owncss"}}<style>
.tcards{display:grid;gap:12px}@media(min-width:680px){.tcards{grid-template-columns:1fr 1fr}}@media(min-width:1240px){.tcards{grid-template-columns:repeat(3,1fr)}}
.tcard{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:14px;display:grid;grid-template-columns:76px minmax(0,1fr);gap:14px;align-items:start;scroll-margin-top:16px}
.tcard:target{border-color:var(--gold)}
.tcard img{width:76px;height:auto;aspect-ratio:600/900;object-fit:cover;object-position:top;border-radius:8px;border:1px solid var(--line)}
.tcard h3{font-size:16px;margin:0 0 2px}.tcard .job{font-size:12.5px;color:var(--muted);margin:0 0 6px}.tcard p{margin:0 0 6px;font-size:14px;line-height:1.45;color:var(--muted)}
.tcard .tlinks{display:flex;flex-wrap:wrap;gap:2px 14px;font-size:14px;align-items:center}
.tcard .tlinks button{margin:0;min-height:0;padding:0;border:0;background:none;color:var(--fg);font-size:14px;font-weight:400;text-decoration:underline;text-decoration-color:var(--muted);text-underline-offset:3px}
</style>
<div class="pagehead"><div><h1>Templates</h1><p class="sub">Finished websites you can start from. Try a demo like a customer, open its owner's admin panel, then copy the request and paste it to your assistant.</p></div>
<div class="acts"><a class="button secondary" href="/connect">How to connect</a></div></div>
<div class="tcards">{{range .Groups}}{{$job := .Job}}{{range .Cards}}<article class="tcard" id="{{.Name}}">
{{if .Screen.File}}<img src="{{.Screen.Src}}" width="{{.Screen.W}}" height="{{.Screen.H}}" alt="{{.Screen.Alt}}" loading="lazy">{{else}}<span></span>{{end}}
<div><h3>{{.Title}}</h3><p class="job">{{$job}}</p><p>{{.Summary}}</p>
<div class="tlinks"><a href="{{.DemoURL}}/">Try the demo</a><a href="{{.AdminURL}}">Owner's admin panel</a><button type="button" data-prompt="{{.Prompt}}">Copy the request</button></div></div></article>{{end}}{{end}}</div>
<p class="muted" style="margin-top:14px">Something in between? Start from the closest template and tell your assistant what to change.</p>
<script>document.addEventListener("click",function(e){var b=e.target.closest("[data-prompt]");if(!b)return;var t=b.getAttribute("data-prompt");if(navigator.clipboard)navigator.clipboard.writeText(t);b.textContent="Copied: paste it to your assistant"})</script>
{{template "appbottom"}}{{end}}

{{define "connect_body"}}<div class="card connect">{{range .Blocks}}{{if eq .Kind "h1"}}<h1>{{.Text}}</h1>
{{else if eq .Kind "h2"}}<h2>{{.Text}}</h2>{{else if eq .Kind "ul"}}<ul>{{range .Items}}<li>{{.}}</li>{{end}}</ul>
{{else if eq .Kind "ol"}}<ol>{{range .Items}}<li>{{.}}</li>{{end}}</ol>{{else}}<p>{{.Text}}</p>{{end}}{{end}}</div>{{end}}
{{define "connect"}}{{if .Shell}}{{template "owntop" .}}{{template "owncss"}}<style>.connect>h1:first-child{display:none}@media(min-width:1100px){.connect{column-count:2;column-gap:44px}.connect h2{break-after:avoid;margin-top:20px}.connect p,.connect ul,.connect ol{break-inside:avoid}.connect>:nth-child(2){margin-top:0}}.connect{font-size:15px}</style>
<div class="pagehead"><div><h1>Connect and help</h1><p class="sub">Add Renoxium to ChatGPT or Claude once, then ask for a new app or a change.</p></div><div class="acts"><a class="button secondary" href="/support">Support</a></div></div>{{template "connect_body" .}}{{template "appbottom"}}{{else}}{{template "wide" "Connect"}}{{template "connect_body" .}}{{template "bottom"}}{{end}}{{end}}

{{define "forgot"}}{{template "auth" "Forgot password"}}<form class="card" method="post" action="/forgot"><h1>Forgot your password?</h1>
<p class="muted">Enter your email and we will send you a link to choose a new password.</p>
<input type="hidden" name="csrf" value="{{.CSRF}}"><label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required>
<button>Send the link</button></form>{{template "authbottom"}}{{end}}

{{define "reset"}}{{template "auth" "New password"}}<form class="card" method="post" action="/reset"><h1>Choose a new password</h1>
{{with .Email}}<p class="muted">For {{.}}</p>{{end}}{{with .Error}}<p class="error">{{.}}</p>{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="token" value="{{.Token}}">
<label for="password">New password (at least 10 characters)</label><input id="password" name="password" type="password" autocomplete="new-password" minlength="10" required>
<button>Save and sign in</button><p class="muted" style="margin-top:16px">You will be signed out on every other device, and agents will need to connect again.</p></form>{{template "authbottom"}}{{end}}

{{define "notice"}}{{template "top" .Title}}<div class="card"><h1>{{.Title}}</h1><p>{{.Message}}</p>{{with .Link}}<p><a href="{{.}}">Continue</a></p>{{end}}</div>{{template "bottom"}}{{end}}

{{define "notfound"}}{{template "top" "Page not found"}}<div class="card"><h1>This page does not exist</h1><p>The address may be mistyped, or the page was moved.</p><p><a class="button" href="/">Go to Renoxium</a></p></div>{{template "bottom"}}{{end}}

{{define "error"}}{{template "top" "Error"}}<div class="card"><h1>Something is wrong</h1><p>{{.Message}}</p></div>{{template "bottom"}}{{end}}
`))

func render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	pages.ExecuteTemplate(w, name, data)
}
