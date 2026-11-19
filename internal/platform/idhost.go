package platform

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Visitors sign in on a host of the platform's own, not on the app's:
// <app>--id.<apps domain> (and <app>--preview--id for the preview). That
// host serves only the platform's sign-in pages: no template and no script
// of the app runs there, so the app's owner (or whoever changed the app)
// cannot read what a visitor types, and a password manager fills a
// visitor's password only there. The app's own pages cannot look into it:
// it is another origin, and it refuses to be framed.
//
// The two hosts hand over like this:
//
//  1. The app host's /_rx/auth/<page> sets a random state cookie on the app
//     host and redirects to the same path on the id host, with the state.
//  2. The id host remembers the state in a cookie of its own and shows the
//     page. After a successful sign-in it stores a one-time code bound to
//     the state and redirects to the app host's /_rx/auth/callback.
//  3. The callback accepts the code only from the browser that holds the
//     matching state cookie, starts the session on the app host and goes
//     to the page asked for. A code sent to someone else's browser is
//     useless there, so nobody can be signed in to an account they did
//     not ask for.
const (
	idLabelSuffix   = "--id"
	authStateCookie = "rx_auth_state" // on the app host
	authFlowCookie  = "rx_auth_flow"  // on the id host
	authFlowTTL     = 30 * time.Minute
	authCodeTTL     = time.Minute
)

func (p *Platform) idHost(app, stageName string) string {
	label := app
	if stageName == Preview && !p.IsDemo(app) {
		label += "--preview"
	}
	return label + idLabelSuffix + "." + p.cfg.AppsDomain
}

// IDURL is the address of the sign-in host of an app stage.
func (p *Platform) IDURL(app, stageName string) string {
	return p.cfg.PublicScheme + "://" + p.idHost(app, stageName) + p.cfg.PublicPort
}

// resolveID finds the app stage whose sign-in host a request is for.
func (p *Platform) resolveID(hostport string) (app, stageName string, ok bool) {
	host := strings.ToLower(hostport)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	label, found := strings.CutSuffix(host, "."+p.cfg.AppsDomain)
	if !found {
		return "", "", false
	}
	base, found := strings.CutSuffix(label, idLabelSuffix)
	if !found {
		return "", "", false
	}
	return p.Resolve(base + "." + p.cfg.AppsDomain)
}

// serveAuthApp handles /_rx/auth/* on the app's own host: the callback,
// sign-out, and for everything else the start of a sign-in on the id host.
func (p *Platform) serveAuthApp(w http.ResponseWriter, r *http.Request, st *stage) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	page := strings.TrimPrefix(r.URL.Path, AuthPrefix)
	switch {
	case page == "callback" && r.Method == http.MethodGet:
		p.authCallback(w, r, st)
	case page == "logout" && r.Method == http.MethodGet:
		p.authPage(w, r, st, http.StatusOK, "logout", authData{})
	case page == "logout" && r.Method == http.MethodPost:
		// No token needed: ServeApp already refuses POSTs from other origins.
		if c, err := r.Cookie(p.cookieName(visitorSessionCookie)); err == nil {
			st.admin.ExecContext(r.Context(), `DELETE FROM rx_sessions WHERE hash = ?`, sha(c.Value))
		}
		p.cookie(w, p.cookieName(visitorSessionCookie), "", -time.Second)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	default:
		state := ""
		if c, err := r.Cookie(p.cookieName(authStateCookie)); err == nil && len(c.Value) >= 32 {
			state = c.Value
		} else {
			state = secretToken()
		}
		p.cookie(w, p.cookieName(authStateCookie), state, authFlowTTL)
		q := r.URL.Query()
		q.Set("state", state)
		if st.name == Preview && len(p.cfg.PreviewKey) > 0 {
			q.Set("pv", p.previewToken(st.app, time.Now().Add(previewLinkTTL))) // the preview's sign-in host is private too
		}
		q.Del(demoKeyParam) // inbox keys never travel to the sign-in host (see withDemoKey)
		// A POST (a form from an old page) becomes a GET of the page: what
		// was typed on the app's host is not passed on.
		http.Redirect(w, r, p.IDURL(st.app, st.name)+AuthPrefix+page+"?"+q.Encode(), http.StatusSeeOther)
	}
}

// authCallback finishes a sign-in on the app host.
func (p *Platform) authCallback(w http.ResponseWriter, r *http.Request, st *stage) {
	next := localPath(r.URL.Query().Get("next"))
	retry := AuthPrefix + "login?next=" + url.QueryEscape(next)
	c, err := r.Cookie(p.cookieName(authStateCookie))
	if err != nil || len(c.Value) < 32 {
		http.Redirect(w, r, retry, http.StatusSeeOther)
		return
	}
	var userID int64
	err = st.admin.QueryRowContext(r.Context(), `DELETE FROM rx_auth_codes WHERE hash = ? AND state_hash = ? AND expires_at > ? RETURNING user_id`,
		sha(r.URL.Query().Get("code")), sha(c.Value), time.Now().Unix()).Scan(&userID)
	if err != nil {
		http.Redirect(w, r, retry, http.StatusSeeOther)
		return
	}
	p.cookie(w, p.cookieName(authStateCookie), "", -time.Second)
	if st.name == Preview && p.IsDemo(st.app) {
		p.moveDemoMessages(r, st.app)
	}
	if err := p.startVisitorSession(r.Context(), w, st, userID); err != nil {
		http.Error(w, "could not sign you in", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// serveID handles a request to an app stage's sign-in host.
func (p *Platform) serveID(w http.ResponseWriter, r *http.Request, st *stage) {
	if !strings.HasPrefix(r.URL.Path, AuthPrefix) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, p.URL(st.app, st.name)+"/", http.StatusSeeOther)
			return
		}
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// No scripts at all on these pages. Forms post here, and a finished
	// sign-in redirects to the app's host, which form-action must name too
	// (browsers apply it to the redirect after a form).
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' "+
		p.URL(st.app, st.name))
	if stt := r.URL.Query().Get("state"); len(stt) >= 32 && len(stt) <= 128 {
		p.cookie(w, p.cookieName(authFlowCookie), stt, authFlowTTL)
	}
	p.serveAuth(w, r, st)
}

// finishAuth ends a successful sign-in on the id host: it hands the visitor
// over to the app host, or, when this browser did not start from the app
// (a link from an email, an expired flow), sends it there to start.
func (p *Platform) finishAuth(w http.ResponseWriter, r *http.Request, st *stage, userID int64, next string) {
	next = localPath(next)
	app := p.URL(st.app, st.name)
	state := r.URL.Query().Get("state")
	if c, err := r.Cookie(p.cookieName(authFlowCookie)); err == nil && len(c.Value) >= 32 {
		state = c.Value
	}
	if len(state) < 32 {
		http.Redirect(w, r, app+AuthPrefix+"login?next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	code := secretToken()
	if _, err := st.admin.ExecContext(r.Context(), `INSERT INTO rx_auth_codes (hash, user_id, state_hash, expires_at) VALUES (?, ?, ?, ?)`,
		sha(code), userID, sha(state), time.Now().Add(authCodeTTL).Unix()); err != nil {
		http.Error(w, "could not sign you in", http.StatusInternalServerError)
		return
	}
	p.cookie(w, p.cookieName(authFlowCookie), "", -time.Second)
	back := app + AuthPrefix + "callback?code=" + url.QueryEscape(code) + "&next=" + url.QueryEscape(next)
	if key, _ := r.Context().Value(demoKey{}).(string); key != "" && st.name == Preview && p.IsDemo(st.app) {
		back += "&" + demoKeyParam + "=" + key // used only if the code is accepted (authCallback)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// cleanAuthCodes deletes hand-over codes nobody collected.
func cleanAuthCodes(ctx context.Context, st *stage) {
	st.admin.ExecContext(ctx, `DELETE FROM rx_auth_codes WHERE expires_at < ?`, time.Now().Unix())
}
