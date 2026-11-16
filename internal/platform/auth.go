package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/email"
	"github.com/arif-rachim/renoxium/internal/password"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// AuthPrefix is where every app serves its visitor sign-in pages.
const AuthPrefix = "/_rx/auth/"

const (
	visitorSessionCookie = "rx_session"
	visitorCSRFCookie    = "rx_csrf"
	visitorSessionTTL    = 30 * 24 * time.Hour
	visitorVerifyTTL     = 7 * 24 * time.Hour
	visitorResetTTL      = time.Hour
)

func secretToken() string {
	var b [32]byte
	rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// visitor returns the signed-in visitor of a request, or nil.
func (p *Platform) visitor(st *stage, r *http.Request) *sandbox.Visitor {
	c, err := r.Cookie(p.cookieName(visitorSessionCookie))
	if err != nil || c.Value == "" {
		return nil
	}
	var v sandbox.Visitor
	var verified sql.NullString
	err = st.admin.QueryRowContext(r.Context(), `SELECT u.id, coalesce(u.email, ''), coalesce(u.phone, ''), u.name, u.verified_at
		FROM rx_sessions s JOIN rx_users u ON u.id = s.user_id
		WHERE s.hash = ? AND s.expires_at > ?`, sha(c.Value), time.Now().Unix()).Scan(&v.ID, &v.Email, &v.Phone, &v.Name, &verified)
	if err != nil {
		return nil
	}
	v.Verified = verified.Valid
	return &v
}

func (p *Platform) cookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: p.cfg.PublicScheme == "https",
		SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge.Seconds())})
}

func (p *Platform) startVisitorSession(ctx context.Context, w http.ResponseWriter, st *stage, userID int64) error {
	token := secretToken()
	_, err := st.admin.ExecContext(ctx, `INSERT INTO rx_sessions (hash, user_id, expires_at) VALUES (?, ?, ?)`,
		sha(token), userID, time.Now().Add(visitorSessionTTL).Unix())
	if err == nil {
		p.cookie(w, p.cookieName(visitorSessionCookie), token, visitorSessionTTL)
	}
	return err
}

func (p *Platform) csrf(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(p.cookieName(visitorCSRFCookie)); err == nil && len(c.Value) >= 32 {
		return c.Value
	}
	v := secretToken()
	p.cookie(w, p.cookieName(visitorCSRFCookie), v, 24*time.Hour)
	return v
}

func (p *Platform) validCSRF(r *http.Request) bool {
	c, err := r.Cookie(p.cookieName(visitorCSRFCookie))
	return err == nil && len(c.Value) >= 32 && subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.PostFormValue("csrf"))) == 1
}

// localPath keeps a return address on the app itself.
func localPath(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n") || strings.HasPrefix(next, AuthPrefix) {
		return "/"
	}
	return next
}

func normalizeEmail(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, err := mail.ParseAddress(s)
	return s, err == nil && a.Address == s && len(s) <= 254
}

// clientIP is the visitor's address; see web.Site.clientIP.
func (p *Platform) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if p.cfg.TrustedIPHeader != "" {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			if fwd := strings.TrimSpace(r.Header.Get(p.cfg.TrustedIPHeader)); net.ParseIP(fwd) != nil {
				return RateKey(fwd)
			}
		}
	}
	return RateKey(host)
}

// allow is a per-key rate limit shared by the visitor auth pages.
func (p *Platform) allow(key string, max int, period time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	w := p.limits[key]
	if w == nil || time.Since(w.start) > period {
		if len(p.limits) > 100000 {
			// Drop only finished windows; clearing everything would reset
			// the limits of whoever filled the map.
			for k, old := range p.limits {
				if time.Since(old.start) > old.period {
					delete(p.limits, k)
				}
			}
			if len(p.limits) > 200000 {
				return false
			}
		}
		w = &window{start: time.Now(), period: period}
		p.limits[key] = w
	}
	w.count++
	return w.count <= max
}

// serveAuth handles /_rx/auth/* on an app host.
func (p *Platform) serveAuth(w http.ResponseWriter, r *http.Request, st *stage) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	page := strings.TrimPrefix(r.URL.Path, AuthPrefix)
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		// Sign-out needs no token: ServeApp already refuses POSTs from other
		// origins, including other apps on the same parent domain.
		if page != "logout" && !p.validCSRF(r) {
			p.authPage(w, r, st, http.StatusForbidden, "message", authData{Title: "Please try again", Message: "The form expired. Go back, reload the page and try again."})
			return
		}
	}
	if page == "phone" || strings.HasPrefix(page, "phone/") {
		p.servePhone(w, r, st, page)
		return
	}
	switch {
	case page == "signup" && r.Method == http.MethodGet:
		p.authPage(w, r, st, http.StatusOK, "signup", authData{Next: localPath(r.URL.Query().Get("next"))})
	case page == "signup" && r.Method == http.MethodPost:
		p.signup(w, r, st)
	case page == "login" && r.Method == http.MethodGet:
		p.authPage(w, r, st, http.StatusOK, "login", authData{Next: localPath(r.URL.Query().Get("next"))})
	case page == "login" && r.Method == http.MethodPost:
		p.login(w, r, st)
	case page == "logout" && r.Method == http.MethodGet:
		p.authPage(w, r, st, http.StatusOK, "logout", authData{})
	case page == "logout" && r.Method == http.MethodPost:
		if c, err := r.Cookie(p.cookieName(visitorSessionCookie)); err == nil {
			st.admin.ExecContext(r.Context(), `DELETE FROM rx_sessions WHERE hash = ?`, sha(c.Value))
		}
		p.cookie(w, p.cookieName(visitorSessionCookie), "", -time.Second)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	case page == "forgot" && r.Method == http.MethodGet:
		p.authPage(w, r, st, http.StatusOK, "forgot", authData{})
	case page == "forgot" && r.Method == http.MethodPost:
		p.forgot(w, r, st)
	case page == "reset" && r.Method == http.MethodGet:
		token := r.URL.Query().Get("token")
		if _, err := p.tokenUser(r.Context(), st, token, "reset"); err != nil {
			p.authPage(w, r, st, http.StatusBadRequest, "message", authData{Title: "Link expired", Message: "This link has expired or was already used. Ask for a new one."})
			return
		}
		p.authPage(w, r, st, http.StatusOK, "reset", authData{Token: token})
	case page == "reset" && r.Method == http.MethodPost:
		p.reset(w, r, st)
	case page == "verify" && r.Method == http.MethodGet:
		p.verify(w, r, st)
	default:
		http.NotFound(w, r)
	}
}

func (p *Platform) signup(w http.ResponseWriter, r *http.Request, st *stage) {
	next := localPath(r.PostFormValue("next"))
	addr, ok := normalizeEmail(r.PostFormValue("email"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	data := authData{Next: next, Email: r.PostFormValue("email"), Name: name}
	if !p.allow("signup:"+st.app+":"+p.clientIP(r), 10, 15*time.Minute) {
		data.Error = "Too many attempts. Try again in a few minutes."
		p.authPage(w, r, st, http.StatusTooManyRequests, "signup", data)
		return
	}
	if !ok {
		data.Error = "Enter a valid email address."
		p.authPage(w, r, st, http.StatusBadRequest, "signup", data)
		return
	}
	pw := r.PostFormValue("password")
	if err := password.Validate(pw); err != nil {
		data.Error = "The password must be at least 10 characters."
		p.authPage(w, r, st, http.StatusBadRequest, "signup", data)
		return
	}
	if utf8.RuneCountInString(name) > 80 {
		name = string([]rune(name)[:80])
	}
	res, err := st.admin.ExecContext(r.Context(), `INSERT INTO rx_users (email, name, password_hash) VALUES (?, ?, ?)`, addr, name, password.Hash(pw))
	if err != nil {
		data.Error = "An account with this email already exists. Sign in instead."
		p.authPage(w, r, st, http.StatusConflict, "signup", data)
		return
	}
	id, _ := res.LastInsertId()
	p.event(st, "auth", "info", "", fmt.Sprintf("visitor #%d signed up", id))
	p.sendVerification(r.Context(), st, id, addr)
	if err := p.startVisitorSession(r.Context(), w, st, id); err != nil {
		http.Error(w, "could not sign you in", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (p *Platform) login(w http.ResponseWriter, r *http.Request, st *stage) {
	next := localPath(r.PostFormValue("next"))
	addr, _ := normalizeEmail(r.PostFormValue("email"))
	data := authData{Next: next, Email: r.PostFormValue("email")}
	// See web.Site.login: per address, per account from that address, and a
	// much higher per-account cap, so strangers cannot lock a visitor out.
	ip := p.clientIP(r)
	if !p.allow("login:"+st.app+":"+ip, 10, 15*time.Minute) || !p.allow("login:"+st.app+":"+addr+":"+ip, 10, 15*time.Minute) ||
		!p.allow("login-account:"+st.app+":"+addr, 100, time.Hour) {
		data.Error = "Too many attempts. Try again in 15 minutes."
		p.authPage(w, r, st, http.StatusTooManyRequests, "login", data)
		return
	}
	var id int64
	var hash string
	err := st.admin.QueryRowContext(r.Context(), `SELECT id, password_hash FROM rx_users WHERE email = ? AND password_hash IS NOT NULL`, addr).Scan(&id, &hash)
	if err != nil {
		password.CheckMissing(r.PostFormValue("password"))
	}
	if err != nil || !password.Check(hash, r.PostFormValue("password")) {
		data.Error = "Wrong email or password."
		p.authPage(w, r, st, http.StatusUnauthorized, "login", data)
		return
	}
	if err := p.startVisitorSession(r.Context(), w, st, id); err != nil {
		http.Error(w, "could not sign you in", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (p *Platform) createToken(ctx context.Context, st *stage, userID int64, purpose string, ttl time.Duration) (string, error) {
	token := secretToken()
	if _, err := st.admin.ExecContext(ctx, `DELETE FROM rx_email_tokens WHERE user_id = ? AND purpose = ?`, userID, purpose); err != nil {
		return "", err
	}
	_, err := st.admin.ExecContext(ctx, `INSERT INTO rx_email_tokens (hash, user_id, purpose, expires_at) VALUES (?, ?, ?, ?)`,
		sha(token), userID, purpose, time.Now().Add(ttl).Unix())
	return token, err
}

func (p *Platform) tokenUser(ctx context.Context, st *stage, token, purpose string) (int64, error) {
	var id int64
	err := st.admin.QueryRowContext(ctx, `SELECT user_id FROM rx_email_tokens WHERE hash = ? AND purpose = ? AND used = 0 AND expires_at > ?`,
		sha(token), purpose, time.Now().Unix()).Scan(&id)
	return id, err
}

// useToken marks a token used and returns its user, failing if another
// request used it first.
func (p *Platform) useToken(ctx context.Context, st *stage, token, purpose string) (int64, error) {
	id, err := p.tokenUser(ctx, st, token, purpose)
	if err != nil {
		return 0, err
	}
	res, err := st.admin.ExecContext(ctx, `UPDATE rx_email_tokens SET used = 1 WHERE hash = ? AND used = 0`, sha(token))
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, errors.New("token already used")
	}
	return id, nil
}

// sendEmail sends from live. On preview nothing leaves the platform: the
// message, links included, goes to the app log instead.
func (p *Platform) sendEmail(ctx context.Context, st *stage, m email.Message) {
	if st.name == Preview {
		p.event(st, "email", "info", "", fmt.Sprintf("not sent from preview: to %s, %q\n%s", m.To, m.Subject, m.Text))
		return
	}
	m.FromName = st.app + " via Renoxium"
	if p.cfg.OwnerEmail != nil {
		var owner string
		if p.db.QueryRowContext(ctx, `SELECT owner FROM apps WHERE name = ?`, st.app).Scan(&owner) == nil {
			if addr, ok := p.cfg.OwnerEmail(owner); ok {
				m.ReplyTo = addr
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if p.cfg.Mailer == nil {
		p.event(st, "email", "warn", "", "email not sent: no mail service configured: "+m.Subject)
		return
	}
	if !p.allow("mail-day:"+st.app, p.cfg.Daily.EmailsPerApp, 24*time.Hour) || !p.allow("mail-day", p.cfg.Daily.Emails, 24*time.Hour) {
		p.event(st, "email", "error", "", "email not sent: the daily email limit was reached: "+m.Subject)
		return
	}
	if err := p.cfg.Mailer.Send(ctx, m); err != nil {
		slog.Error("visitor email", "app", st.app, "err", err)
		p.event(st, "email", "error", "", "email to a visitor failed: "+err.Error())
	}
}

func (p *Platform) sendVerification(ctx context.Context, st *stage, id int64, addr string) {
	token, err := p.createToken(ctx, st, id, "verify", visitorVerifyTTL)
	if err != nil {
		return
	}
	link := p.URL(st.app, st.name) + AuthPrefix + "verify?token=" + url.QueryEscape(token)
	p.sendEmail(ctx, st, email.Message{To: addr, Tag: "visitor_verify", Subject: "Confirm your email",
		Text: "Confirm your email address by opening this link:\n\n" + link + "\n\nThe link works for 7 days. If you did not sign up, ignore this email.\n"})
}

func (p *Platform) forgot(w http.ResponseWriter, r *http.Request, st *stage) {
	addr, _ := normalizeEmail(r.PostFormValue("email"))
	if !p.allow("forgot:"+st.app+":"+p.clientIP(r), 10, 15*time.Minute) || !p.allow("forgot-mail:"+st.app+":"+addr, 3, time.Hour) {
		p.authPage(w, r, st, http.StatusTooManyRequests, "message", authData{Title: "Too many attempts", Message: "Try again later."})
		return
	}
	var id int64
	if err := st.admin.QueryRowContext(r.Context(), `SELECT id FROM rx_users WHERE email = ?`, addr).Scan(&id); err == nil {
		if token, err := p.createToken(r.Context(), st, id, "reset", visitorResetTTL); err == nil {
			link := p.URL(st.app, st.name) + AuthPrefix + "reset?token=" + url.QueryEscape(token)
			p.sendEmail(r.Context(), st, email.Message{To: addr, Tag: "visitor_reset", Subject: "Reset your password",
				Text: "Someone asked to reset your password.\n\nChoose a new password here:\n\n" + link +
					"\n\nThe link works for 1 hour and only once. If it was not you, ignore this email.\n"})
		}
	}
	p.authPage(w, r, st, http.StatusOK, "message", authData{Title: "Check your email",
		Message: "If an account exists for " + addr + ", we sent it a link to choose a new password."})
}

func (p *Platform) reset(w http.ResponseWriter, r *http.Request, st *stage) {
	token := r.PostFormValue("token")
	pw := r.PostFormValue("password")
	if err := password.Validate(pw); err != nil {
		p.authPage(w, r, st, http.StatusBadRequest, "reset", authData{Token: token, Error: "The password must be at least 10 characters."})
		return
	}
	id, err := p.useToken(r.Context(), st, token, "reset")
	if err != nil {
		p.authPage(w, r, st, http.StatusBadRequest, "message", authData{Title: "Link expired", Message: "This link has expired or was already used. Ask for a new one."})
		return
	}
	ctx := r.Context()
	if _, err := st.admin.ExecContext(ctx, `UPDATE rx_users SET password_hash = ?, verified_at = coalesce(verified_at, datetime('now')) WHERE id = ?`, password.Hash(pw), id); err != nil {
		http.Error(w, "could not save the password", http.StatusInternalServerError)
		return
	}
	st.admin.ExecContext(ctx, `DELETE FROM rx_sessions WHERE user_id = ?`, id)
	p.event(st, "auth", "info", "", fmt.Sprintf("visitor #%d reset their password", id))
	if err := p.startVisitorSession(ctx, w, st, id); err != nil {
		http.Error(w, "could not sign you in", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (p *Platform) verify(w http.ResponseWriter, r *http.Request, st *stage) {
	id, err := p.useToken(r.Context(), st, r.URL.Query().Get("token"), "verify")
	if err != nil {
		p.authPage(w, r, st, http.StatusBadRequest, "message", authData{Title: "Link expired", Message: "This confirmation link has expired or was already used."})
		return
	}
	st.admin.ExecContext(r.Context(), `UPDATE rx_users SET verified_at = coalesce(verified_at, datetime('now')) WHERE id = ?`, id)
	p.authPage(w, r, st, http.StatusOK, "message", authData{Title: "Email confirmed", Message: "Thank you. Your email address is confirmed.", Link: "/"})
}

// authData is what auth pages, and the app's own auth_* templates, receive.
type authData struct {
	CSRF, Next, Email, Name, Phone, Token, Error, Title, Message, Link string
	WhatsApp                                                           bool
}

func (d authData) templateData() map[string]any {
	return map[string]any{"csrf": d.CSRF, "next": d.Next, "email": d.Email, "name": d.Name, "phone": d.Phone, "token": d.Token,
		"error": d.Error, "title": d.Title, "message": d.Message, "link": d.Link, "whatsapp": d.WhatsApp}
}

// authPage renders the app's auth_<page> template if it defines one, else
// the platform's plain page.
func (p *Platform) authPage(w http.ResponseWriter, r *http.Request, st *stage, status int, page string, d authData) {
	d.CSRF = p.csrf(w, r)
	d.WhatsApp = p.whatsappAvailable(st)
	var buf bytes.Buffer
	if t := st.tmpl.Lookup("auth_" + page); t != nil {
		lw := &limitedWriter{max: maxPage}
		if err := t.Execute(lw, d.templateData()); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(status)
			w.Write(lw.buf)
			return
		} else {
			p.event(st, "server", "error", "", "template auth_"+page+": "+err.Error())
		}
	}
	if err := defaultAuthPages.ExecuteTemplate(&buf, page, d.templateData()); err != nil {
		http.Error(w, "page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

var defaultAuthPages = template.Must(template.New("").Parse(`
{{define "top"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.}}</title>
<style>:root{color-scheme:light dark}body{font:16px/1.5 system-ui,sans-serif;max-width:400px;margin:48px auto;padding:0 16px}
label{display:block;margin:12px 0 4px;font-size:14px}input{width:100%;box-sizing:border-box;padding:10px;border:1px solid #bbb;border-radius:8px;font:inherit}
button{margin-top:16px;padding:10px 16px;border:0;border-radius:8px;background:#2f5bea;color:#fff;font:inherit}.error{color:#c62828}.muted{color:#777;font-size:14px}</style></head><body>{{end}}
{{define "login"}}{{template "top" "Sign in"}}<h1>Sign in</h1>{{with .error}}<p class="error">{{.}}</p>{{end}}
<form method="post" action="/_rx/auth/login"><input type="hidden" name="csrf" value="{{.csrf}}"><input type="hidden" name="next" value="{{.next}}">
<label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required value="{{.email}}">
<label for="password">Password</label><input id="password" name="password" type="password" autocomplete="current-password" required>
<button>Sign in</button></form><p class="muted"><a href="/_rx/auth/forgot">Forgot your password?</a> · <a href="/_rx/auth/signup?next={{.next}}">Create an account</a></p>
{{if .whatsapp}}<p><a href="/_rx/auth/phone?next={{.next}}">Sign in with WhatsApp instead</a></p>{{end}}</body></html>{{end}}
{{define "phone"}}{{template "top" "Sign in with WhatsApp"}}<h1>Sign in with WhatsApp</h1>{{with .error}}<p class="error">{{.}}</p>{{end}}
<p class="muted">We send a 6-digit code to your WhatsApp. No password needed.</p>
<form method="post" action="/_rx/auth/phone"><input type="hidden" name="csrf" value="{{.csrf}}"><input type="hidden" name="next" value="{{.next}}">
<label for="phone">WhatsApp number</label><input id="phone" name="phone" type="tel" autocomplete="tel" inputmode="tel" placeholder="+971 50 123 4567" required value="{{.phone}}">
<button>Send code</button></form><p class="muted"><a href="/_rx/auth/login?next={{.next}}">Use email and password</a></p></body></html>{{end}}
{{define "phone_code"}}{{template "top" "Enter the code"}}<h1>Enter the code</h1>{{with .error}}<p class="error">{{.}}</p>{{end}}
<p class="muted">We sent a code to {{.phone}} on WhatsApp. It works for 10 minutes.</p>
<form method="post" action="/_rx/auth/phone/verify"><input type="hidden" name="csrf" value="{{.csrf}}"><input type="hidden" name="next" value="{{.next}}"><input type="hidden" name="phone" value="{{.phone}}">
<label for="code">Code</label><input id="code" name="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required>
<label for="name">Your name (first time only)</label><input id="name" name="name" autocomplete="name">
<button>Sign in</button></form><p class="muted"><a href="/_rx/auth/phone?next={{.next}}">Send a new code</a></p></body></html>{{end}}
{{define "signup"}}{{template "top" "Create an account"}}<h1>Create an account</h1>{{with .error}}<p class="error">{{.}}</p>{{end}}
<form method="post" action="/_rx/auth/signup"><input type="hidden" name="csrf" value="{{.csrf}}"><input type="hidden" name="next" value="{{.next}}">
<label for="name">Name</label><input id="name" name="name" autocomplete="name" value="{{.name}}">
<label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required value="{{.email}}">
<label for="password">Password (at least 10 characters)</label><input id="password" name="password" type="password" autocomplete="new-password" minlength="10" required>
<button>Create account</button></form><p class="muted">Already have an account? <a href="/_rx/auth/login?next={{.next}}">Sign in</a></p></body></html>{{end}}
{{define "forgot"}}{{template "top" "Forgot password"}}<h1>Forgot your password?</h1>
<form method="post" action="/_rx/auth/forgot"><input type="hidden" name="csrf" value="{{.csrf}}">
<label for="email">Email</label><input id="email" name="email" type="email" autocomplete="email" required><button>Send me a link</button></form></body></html>{{end}}
{{define "reset"}}{{template "top" "New password"}}<h1>Choose a new password</h1>{{with .error}}<p class="error">{{.}}</p>{{end}}
<form method="post" action="/_rx/auth/reset"><input type="hidden" name="csrf" value="{{.csrf}}"><input type="hidden" name="token" value="{{.token}}">
<label for="password">New password (at least 10 characters)</label><input id="password" name="password" type="password" autocomplete="new-password" minlength="10" required>
<button>Save and sign in</button></form></body></html>{{end}}
{{define "logout"}}{{template "top" "Sign out"}}<h1>Sign out?</h1>
<form method="post" action="/_rx/auth/logout"><button>Sign out</button></form></body></html>{{end}}
{{define "message"}}{{template "top" .title}}<h1>{{.title}}</h1><p>{{.message}}</p>{{with .link}}<p><a href="{{.}}">Continue</a></p>{{end}}</body></html>{{end}}
`))
