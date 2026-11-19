package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
)

// Template demos: every example in the public gallery runs as an app of
// the platform's own, demo-<example>, so business owners can try it before
// they sign up. A demo is an ordinary app with three differences:
//
//   - It is owned by DemoOwner, which is no account: nobody can sign in as
//     it, change it with the builder tools or publish it.
//   - It only has a preview stage, served without the preview lock at the
//     address a live app would have (demo-<example>.<apps domain>). Preview
//     already keeps everything inside the platform: card payments are
//     simulated, emails and WhatsApp codes are not sent, http.fetch makes no
//     real calls and scheduled jobs do not run.
//   - What preview would only write to the log (emails, sign-in links and
//     codes, owner notices) goes to a demo inbox that only the browser whose
//     request caused it can read, so visitors can sign in and see the
//     emails a business would send.
//
// Everyone shares one demo database, so ResetDemos puts it back to the
// template's seed rows every hour.
const (
	DemoOwner     = "rx_demo"
	DemoPrefix    = "demo-"
	DemoInboxPath = "/_rx/demo/inbox"
	demoCookie    = "rx_demo"
	demoKeyParam  = "rx_dk" // hands the inbox key to the sign-in host
	// Each browser keeps its newest demoInboxSize messages; at most
	// demoInboxBoxes browsers per demo get an inbox until the next reset.
	demoInboxSize  = 20
	demoInboxBoxes = 5000
)

// DemoApp is the app name of an example's demo.
func DemoApp(example string) string { return DemoPrefix + example }

// IsDemo reports whether app is a template demo.
func (p *Platform) IsDemo(app string) bool {
	return strings.HasPrefix(app, DemoPrefix) && p.ownerOf(app) == DemoOwner
}

// DemoURL is the public address of an example's demo.
func (p *Platform) DemoURL(example string) string { return p.URL(DemoApp(example), Preview) }

// EnsureDemos creates a demo for each example that has none and applies
// the example's current definition to demos that run an older one, which
// also resets their data. Unchanged demos are left alone. It is called at
// startup, so a new or changed template is in the gallery after a deploy.
func (p *Platform) EnsureDemos(ctx context.Context, list []examples.Example) error {
	var errs []error
	for _, ex := range list {
		if err := p.ensureDemo(ctx, ex); err != nil {
			errs = append(errs, fmt.Errorf("demo of %s: %w", ex.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (p *Platform) ensureDemo(ctx context.Context, ex examples.Example) error {
	name := DemoApp(ex.Name)
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	var owner string
	err := p.db.QueryRow(`SELECT owner FROM apps WHERE name = ?`, name).Scan(&owner)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := p.createDemo(name); err != nil {
			return err
		}
	case err != nil:
		return err
	case owner != DemoOwner:
		return fmt.Errorf("the app name %s belongs to an account", name)
	}
	p.owners.Store(name, DemoOwner)
	info, err := p.Info(name)
	if err != nil {
		return err
	}
	if info.PreviewVersion > 0 {
		if cur, _, err := p.Definition(name, info.PreviewVersion); err == nil {
			a, errA := json.Marshal(cur)
			b, errB := json.Marshal(def)
			if errA == nil && errB == nil && bytes.Equal(a, b) {
				return nil
			}
		}
	}
	res, err := p.ApplyChange(ctx, name, def)
	if err != nil {
		return err
	}
	if !res.OK {
		return errors.New(strings.Join(res.Problems, "; "))
	}
	p.demoBox.clear(name)
	return nil
}

// createDemo registers a demo app. Unlike CreateApp it takes a reserved
// name, counts against no account and has a single host, which serves the
// preview stage.
func (p *Platform) createDemo(name string) error {
	if !appNameRE.MatchString(name) || strings.Contains(name, "--") {
		return fmt.Errorf("%q is not a valid app name", name)
	}
	if err := os.MkdirAll(p.appDir(name), 0o755); err != nil {
		return err
	}
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO apps (name, created_at, owner) VALUES (?, ?, ?)`, name, now(), DemoOwner); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO domains (host, app, stage) VALUES (?, ?, ?)`, name+"."+p.cfg.AppsDomain, name, Preview); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	p.hosts.Delete(name + "." + p.cfg.AppsDomain)
	return nil
}

// ResetDemos puts every demo's data back to its template's seed rows:
// visitors' bookings, orders, accounts, sessions and uploads are removed,
// and so are the demo inboxes.
func (p *Platform) ResetDemos(ctx context.Context) error {
	rows, err := p.db.QueryContext(ctx, `SELECT name FROM apps WHERE owner = ? ORDER BY name`, DemoOwner)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		names = append(names, n)
	}
	rows.Close()
	var errs []error
	for _, name := range names {
		if err := p.resetDemo(name); err != nil {
			errs = append(errs, fmt.Errorf("reset %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func (p *Platform) resetDemo(name string) error {
	info, err := p.Info(name)
	if err != nil || info.PreviewVersion == 0 {
		return err
	}
	def, _, err := p.Definition(name, info.PreviewVersion)
	if err != nil {
		return err
	}
	p.change.Lock()
	defer p.change.Unlock()
	if _, problem, err := p.rebuildPreview(name, def); err != nil {
		return err
	} else if problem != "" {
		return errors.New(problem)
	}
	p.demoBox.clear(name)
	p.forgetLive(name)
	return nil
}

// --- demo inbox ---

type demoKey struct{}

// withDemoKey gives a request to a demo the browser's inbox key, from a
// cookie set on first visit. The sign-in host is another origin with its
// own cookies, so the app host passes its key along when it sends the
// browser there: sign-in codes and links then show up in the same inbox.
// The key only reads demo messages, which are public data anyway.
func (p *Platform) withDemoKey(w http.ResponseWriter, r *http.Request) *http.Request {
	name := p.cookieName(demoCookie)
	key := ""
	if v := r.URL.Query().Get(demoKeyParam); validDemoKey(v) {
		key = v
	} else if c, err := r.Cookie(name); err == nil && validDemoKey(c.Value) {
		key = c.Value
	}
	if c, err := r.Cookie(name); err != nil || c.Value != key {
		if key == "" {
			b := make([]byte, 16)
			rand.Read(b)
			key = hex.EncodeToString(b)
		}
		http.SetCookie(w, &http.Cookie{Name: name, Value: key, Path: "/", HttpOnly: true,
			Secure: p.cfg.PublicScheme == "https", SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
	}
	return r.WithContext(context.WithValue(r.Context(), demoKey{}, key))
}

func validDemoKey(v string) bool {
	if len(v) != 32 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

// DemoMessage is an email or WhatsApp message a demo would have sent.
type DemoMessage struct {
	At      time.Time
	Kind    string // "email" or "whatsapp"
	To      string
	Subject string
	Text    string
}

type demoInbox struct {
	mu    sync.Mutex
	boxes map[string]map[string][]DemoMessage // app -> browser key -> newest last
}

func (b *demoInbox) add(app, key string, m DemoMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.boxes == nil {
		b.boxes = map[string]map[string][]DemoMessage{}
	}
	app_ := b.boxes[app]
	if app_ == nil {
		app_ = map[string][]DemoMessage{}
		b.boxes[app] = app_
	}
	list, ok := app_[key]
	if !ok && len(app_) >= demoInboxBoxes {
		return
	}
	list = append(list, m)
	if len(list) > demoInboxSize {
		list = list[len(list)-demoInboxSize:]
	}
	app_[key] = list
}

func (b *demoInbox) list(app, key string) []DemoMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	src := b.boxes[app][key]
	out := make([]DemoMessage, len(src))
	for i, m := range src {
		out[len(src)-1-i] = m // newest first
	}
	return out
}

func (b *demoInbox) clear(app string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.boxes, app)
}

// demoDeliver puts a message preview did not send into the inbox of the
// browser whose request caused it. Messages caused by no browser (jobs,
// background work) are only logged, as on any preview.
func (p *Platform) demoDeliver(ctx context.Context, st *stage, m DemoMessage) {
	if st.name != Preview || !p.IsDemo(st.app) {
		return
	}
	key, _ := ctx.Value(demoKey{}).(string)
	if key == "" {
		return
	}
	m.At = time.Now()
	p.demoBox.add(st.app, key, m)
}

// DemoInbox returns the messages for one browser of a demo, newest first.
// Tests use it; visitors read DemoInboxPath.
func (p *Platform) DemoInbox(app, key string) []DemoMessage { return p.demoBox.list(app, key) }

func (p *Platform) demoMessages(r *http.Request, app string) []DemoMessage {
	key, _ := r.Context().Value(demoKey{}).(string)
	if key == "" {
		return nil
	}
	return p.demoBox.list(app, key)
}

var linkRE = regexp.MustCompile(`https?://[^\s<>"]+`)

// linkify escapes text and turns addresses into links, so a visitor can
// follow a confirmation or password link straight from the demo inbox.
func linkify(text string) template.HTML {
	var b strings.Builder
	last := 0
	for _, m := range linkRE.FindAllStringIndex(text, -1) {
		b.WriteString(template.HTMLEscapeString(text[last:m[0]]))
		u := template.HTMLEscapeString(text[m[0]:m[1]])
		b.WriteString(`<a href="` + u + `">` + u + `</a>`)
		last = m[1]
	}
	b.WriteString(template.HTMLEscapeString(text[last:]))
	return template.HTML(b.String())
}

type demoMessageView struct {
	DemoMessage
	Body template.HTML
	When string
}

func demoViews(list []DemoMessage) []demoMessageView {
	out := make([]demoMessageView, 0, len(list))
	for _, m := range list {
		out = append(out, demoMessageView{DemoMessage: m, Body: linkify(m.Text), When: humanAgo(time.Since(m.At))})
	}
	return out
}

func humanAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
}

var demoPages = template.Must(template.New("").Parse(`
{{define "messages"}}{{range .}}<article class="rx-demo-msg"><p class="rx-demo-meta">{{if eq .Kind "whatsapp"}}WhatsApp{{else}}Email{{end}} to <strong>{{.To}}</strong> · {{.When}}</p>
<h2>{{.Subject}}</h2><pre>{{.Body}}</pre></article>{{end}}{{end}}
{{define "inbox"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="robots" content="noindex">
<title>Demo inbox</title><style>:root{color-scheme:light dark}body{font:16px/1.5 system-ui,sans-serif;max-width:720px;margin:32px auto;padding:0 16px}
.rx-demo-msg{border:1px solid #8884;border-radius:10px;padding:12px 16px;margin:12px 0}.rx-demo-msg h2{font-size:17px;margin:4px 0}.rx-demo-meta{color:#777;font-size:14px;margin:0}
pre{white-space:pre-wrap;word-break:break-word;font:inherit;margin:8px 0 0}.muted{color:#777}</style></head><body>
<p><a href="/">&larr; Back to the demo</a></p><h1>Demo inbox</h1>
<p class="muted">This demo sends nothing for real. The emails and WhatsApp messages it would have sent because of what you did in this browser appear here: sign-in codes, confirmation links, booking and order emails, and the notices the business owner would get. Everything is deleted every hour.</p>
{{if .Messages}}{{template "messages" .Messages}}{{else}}<p>Nothing yet. Book, order or sign in on the demo, then come back.</p>{{end}}
<p><a href="` + DemoInboxPath + `">Refresh</a></p></body></html>{{end}}
{{define "box"}}<section style="border:2px dashed #2f5bea;border-radius:10px;padding:8px 16px;margin:24px 0"><p style="margin:8px 0"><strong>Demo inbox</strong> — nothing is really sent. Messages for this browser:</p>
<style>.rx-demo-msg{border-top:1px solid #8884;padding:8px 0}.rx-demo-msg h2{font-size:16px;margin:4px 0}.rx-demo-meta{color:#777;font-size:13px;margin:0}.rx-demo-msg pre{white-space:pre-wrap;word-break:break-word;font:inherit;font-size:14px;margin:4px 0 0}</style>
{{template "messages" .}}</section>{{end}}
{{define "banner"}}<div id="rx-demo" style="display:flex;flex-wrap:wrap;gap:2px 14px;align-items:center;justify-content:center;padding:6px 12px;background:#1b1f3b;color:#fff;font:13px/1.4 system-ui,sans-serif;text-align:center" dir="ltr" lang="en">
<span style="display:inline-flex;align-items:center;gap:6px">{{.Mark}}<strong>Template demo</strong></span><span>Public data, deleted every hour. Don't enter real personal details.</span>
<span><a href="` + DemoInboxPath + `" style="color:#9db4ff">Demo inbox</a> · <a href="{{.Admin}}" style="color:#9db4ff">Owner's admin panel</a> · <a href="{{.Gallery}}" style="color:#9db4ff">All templates</a></span></div>{{end}}
`))

// serveDemoInbox shows one browser's demo messages, on the app host or the
// sign-in host. The page is the platform's own: nothing of the app runs here.
func (p *Platform) serveDemoInbox(w http.ResponseWriter, r *http.Request, app string) {
	var buf bytes.Buffer
	if err := demoPages.ExecuteTemplate(&buf, "inbox", map[string]any{"Messages": demoViews(p.demoMessages(r, app))}); err != nil {
		http.Error(w, "page unavailable", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Write(buf.Bytes())
}

// demoBox renders the inbox inside the platform's sign-in pages of a demo.
func (p *Platform) demoInboxBox(r *http.Request, app string) []byte {
	list := p.demoMessages(r, app)
	if len(list) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if demoPages.ExecuteTemplate(&buf, "box", demoViews(list)) != nil {
		return nil
	}
	return buf.Bytes()
}

// injectDemoBanner adds the demo bar at the top of the page.
func (p *Platform) injectDemoBanner(app string, page []byte) []byte {
	var buf bytes.Buffer
	example := strings.TrimPrefix(app, DemoPrefix)
	if demoPages.ExecuteTemplate(&buf, "banner", map[string]any{
		"Gallery": p.cfg.SiteURL + "/templates",
		"Admin":   p.cfg.SiteURL + "/templates/" + example + "/admin",
		"Mark":    template.HTML(brandMark("15px", "#fff")),
	}) != nil {
		return page
	}
	lower := bytes.ToLower(page)
	at := bytes.Index(lower, []byte("<body"))
	if at < 0 {
		return insertBeforeBody(page, buf.Bytes())
	}
	end := bytes.IndexByte(lower[at:], '>')
	if end < 0 {
		return page
	}
	at += end + 1
	out := make([]byte, 0, len(page)+buf.Len())
	out = append(out, page[:at]...)
	out = append(out, buf.Bytes()...)
	return append(out, page[at:]...)
}

func insertBeforeBody(page, snippet []byte) []byte {
	at := bytes.LastIndex(bytes.ToLower(page), []byte("</body>"))
	if at < 0 {
		return append(page, snippet...)
	}
	out := make([]byte, 0, len(page)+len(snippet))
	out = append(out, page[:at]...)
	out = append(out, snippet...)
	return append(out, page[at:]...)
}
