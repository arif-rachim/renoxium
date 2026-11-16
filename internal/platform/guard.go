package platform

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxPage caps one rendered page. Templates run in the platform process, so
// a template that includes itself many times over must not be able to
// exhaust its memory.
const maxPage = 4 << 20

var errPageTooLarge = fmt.Errorf("the page is larger than %d MB", maxPage>>20)

// limitedWriter fails once more than max bytes are written, which stops
// template execution.
type limitedWriter struct {
	buf []byte
	max int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if len(l.buf)+len(p) > l.max {
		return 0, errPageTooLarge
	}
	l.buf = append(l.buf, p...)
	return len(p), nil
}

// CrossSite reports whether a state-changing request came from a page on
// another origin. Every app shares the renoxium.app site, so SameSite
// cookies alone do not stop one app's pages from posting to another app.
func CrossSite(r *http.Request) bool {
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" {
		return s != "same-origin" && s != "none"
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err != nil || !strings.EqualFold(u.Host, r.Host)
	}
	return false
}

// cookieName adds the __Host- prefix on HTTPS. Browsers then refuse the
// cookie unless this exact host set it, so another app on the same parent
// domain cannot plant a session or CSRF cookie for this one.
func (p *Platform) cookieName(name string) string {
	if p.cfg.PublicScheme == "https" {
		return "__Host-" + name
	}
	return name
}

// RateKey is the part of an address used for rate limits: the whole IPv4
// address, or the /64 network of an IPv6 address, since one IPv6
// subscriber usually controls a whole /64.
func RateKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() != nil {
		return ip
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// requestsPerMinute caps app requests from one address, so one client
// cannot keep an app's script slots busy for everyone else.
const requestsPerMinute = 300

// --- request IDs for browser telemetry ---

const ridTTL = 30 * time.Minute

type ridEntry struct {
	app     string
	expires time.Time
	n       int
}

// issueRID records a request ID handed to a page, so /_rx/log only accepts
// reports that belong to a page this app really served.
func (p *Platform) issueRID(app, rid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if len(p.rids) > 100000 {
		for k, e := range p.rids {
			if now.After(e.expires) {
				delete(p.rids, k)
			}
		}
		if len(p.rids) > 100000 {
			p.rids = map[string]*ridEntry{}
		}
	}
	p.rids[rid] = &ridEntry{app: app, expires: now.Add(ridTTL)}
}

// useRID reports whether a telemetry report may be stored for rid.
func (p *Platform) useRID(app, rid string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.rids[rid]
	if e == nil || e.app != app || time.Now().After(e.expires) {
		return false
	}
	e.n++
	return e.n <= 30
}

// --- private previews ---

const (
	previewLinkTTL   = 10 * time.Minute
	previewCookieTTL = 12 * time.Hour
	previewPath      = "/_rx/preview"
)

func (p *Platform) previewToken(app string, expires time.Time) string {
	exp := strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, p.cfg.PreviewKey)
	mac.Write([]byte("preview\x00" + app + "\x00" + exp))
	return exp + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (p *Platform) validPreviewToken(app, token string) bool {
	exp, _, ok := strings.Cut(token, ".")
	n, err := strconv.ParseInt(exp, 10, 64)
	if !ok || err != nil || time.Now().Unix() > n {
		return false
	}
	return hmac.Equal([]byte(token), []byte(p.previewToken(app, time.Unix(n, 0))))
}

// PreviewLink is a short-lived link that opens an app's private preview
// in the browser. Only hand it to the app's owner.
func (p *Platform) PreviewLink(app string) string {
	if len(p.cfg.PreviewKey) == 0 {
		return p.URL(app, Preview)
	}
	return p.URL(app, Preview) + previewPath + "?t=" + url.QueryEscape(p.previewToken(app, time.Now().Add(previewLinkTTL)))
}

func (p *Platform) previewCookie(app string) *http.Cookie {
	return &http.Cookie{Name: p.cookieName("rx_preview"), Value: p.previewToken(app, time.Now().Add(previewCookieTTL)), Path: "/",
		HttpOnly: true, Secure: p.cfg.PublicScheme == "https", SameSite: http.SameSiteLaxMode, MaxAge: int(previewCookieTTL.Seconds())}
}

// guardPreview lets only the owner's browser (holding a preview cookie)
// see a preview: it runs unfinished code on a copy of live data, visitor
// accounts included. It has answered the request when it returns false.
func (p *Platform) guardPreview(w http.ResponseWriter, r *http.Request, app string) bool {
	if len(p.cfg.PreviewKey) == 0 {
		return true
	}
	if r.URL.Path == previewPath {
		if !p.validPreviewToken(app, r.URL.Query().Get("t")) {
			p.previewLocked(w, app, "This preview link has expired.")
			return false
		}
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.SetCookie(w, p.previewCookie(app))
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return false
	}
	if c, err := r.Cookie(p.cookieName("rx_preview")); err == nil && p.validPreviewToken(app, c.Value) {
		return true
	}
	p.previewLocked(w, app, "This preview is private.")
	return false
}

func (p *Platform) previewLocked(w http.ResponseWriter, app, why string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.WriteHeader(http.StatusForbidden)
	link := p.cfg.SiteURL + "/apps/" + app + "/preview"
	fmt.Fprintf(w, `<!doctype html><title>Private preview</title><meta name="viewport" content="width=device-width, initial-scale=1">`+
		`<p>%s Only the app's owner can open it: <a href="%s">open the preview from your Renoxium account</a>.</p>`,
		why, link)
}

// ownerVerified reports whether an app's owner has confirmed their email.
// Without an OwnerEmail lookup (tests, development) every owner counts as
// verified.
func (p *Platform) ownerVerified(app string) error {
	if p.cfg.OwnerEmail == nil {
		return nil
	}
	var owner string
	if err := p.db.QueryRow(`SELECT owner FROM apps WHERE name = ?`, app).Scan(&owner); err != nil {
		return err
	}
	if _, ok := p.cfg.OwnerEmail(owner); !ok {
		return errors.New("confirm your Renoxium email address before publishing: open the link we emailed you, or send it again from your dashboard at " + p.cfg.SiteURL)
	}
	return nil
}

// Suspend takes an app offline on every host, for example after an abuse
// report. Its data is kept. Running servers notice within suspendedTTL, so
// it also works from a separate process such as "renoxium suspend".
func (p *Platform) Suspend(app, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "suspended"
	}
	res, err := p.db.Exec(`UPDATE apps SET suspended = ? WHERE name = ?`, reason, app)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("app %q: %w", app, ErrNotFound)
	}
	p.mu.Lock()
	p.suspendedAt = time.Time{}
	p.mu.Unlock()
	return nil
}

// Unsuspend brings a suspended app back.
func (p *Platform) Unsuspend(app string) error {
	if _, err := p.db.Exec(`UPDATE apps SET suspended = '' WHERE name = ?`, app); err != nil {
		return err
	}
	p.mu.Lock()
	p.suspendedAt = time.Time{}
	p.mu.Unlock()
	return nil
}

const suspendedTTL = 30 * time.Second

func (p *Platform) isSuspended(app string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.suspendedAt) > suspendedTTL {
		set := map[string]bool{}
		if rows, err := p.db.Query(`SELECT name FROM apps WHERE suspended != ''`); err == nil {
			for rows.Next() {
				var n string
				if rows.Scan(&n) == nil {
					set[n] = true
				}
			}
			rows.Close()
			p.suspended, p.suspendedAt = set, time.Now()
		}
	}
	return p.suspended[app]
}

func (p *Platform) suspendedPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnavailableForLegalReasons)
	fmt.Fprint(w, `<!doctype html><title>App suspended</title><meta name="viewport" content="width=device-width, initial-scale=1">`+
		`<p>This app has been suspended by Renoxium.</p>`)
}

// AbuseLink is where anyone can report an app.
func (p *Platform) AbuseLink() string { return p.cfg.SiteURL + "/abuse" }
