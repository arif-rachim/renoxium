package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/arif-rachim/renoxium/internal/sandbox"
)

const maxBody = 1 << 20

// ServeApp serves a request for an app stage. It returns false if the host
// does not belong to any app.
func (p *Platform) ServeApp(w http.ResponseWriter, r *http.Request) bool {
	app, stageName, ok := p.Resolve(r.Host)
	if !ok {
		return false
	}
	if p.cfg.PublicScheme == "https" {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	// /mcp is an API for AI assistants: it never acts as a signed-in visitor,
	// so requests from other sites are harmless there.
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.URL.Path != MCPPath && CrossSite(r) {
		http.Error(w, "this form must be sent from the app's own pages", http.StatusForbidden)
		return true
	}
	if p.isSuspended(app) {
		p.suspendedPage(w)
		return true
	}
	if stageName == Preview && !p.guardPreview(w, r, app) {
		return true
	}
	if strings.HasPrefix(r.URL.Path, FilesPrefix) {
		p.serveFile(w, r, app)
		return true
	}
	st, err := p.loadStage(app, stageName)
	if err != nil {
		http.Error(w, "this app is temporarily unavailable", http.StatusServiceUnavailable)
		return true
	}
	if st == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "<!doctype html><title>Not published</title><p>This app has not been published yet.</p>")
		return true
	}
	if r.URL.Path == "/_rx/log" {
		p.browserLog(w, r, st)
		return true
	}
	if strings.HasPrefix(r.URL.Path, AuthPrefix) {
		p.serveAuth(w, r, st)
		return true
	}
	if strings.HasPrefix(r.URL.Path, PayPrefix) {
		p.servePay(w, r, st)
		return true
	}
	if strings.HasPrefix(r.URL.Path, LivePrefix) {
		p.serveLive(w, r, st)
		return true
	}
	if servesMCP(st.def, r.URL.Path) {
		if !p.allow("req:"+app+":"+p.clientIP(r), requestsPerMinute, time.Minute) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many requests; try again in a minute", http.StatusTooManyRequests)
			return true
		}
		p.serveMCP(w, r, st)
		return true
	}
	if r.URL.Path == "/favicon.ico" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		if route, _ := st.def.Match(http.MethodGet, r.URL.Path); route == nil {
			// Browsers ask for it on every first visit; an app without its
			// own icon gets a plain one instead of a 404 in its log.
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Write(defaultFavicon)
			return true
		}
	}
	if !p.allow("req:"+app+":"+p.clientIP(r), requestsPerMinute, time.Minute) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many requests; try again in a minute", http.StatusTooManyRequests)
		return true
	}
	rid := newRequestID()
	out := p.handle(st, r, rid)
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("X-Request-Id", rid)
	if stageName == Preview {
		h.Set("X-Robots-Tag", "noindex")
	}
	if out.location != "" {
		h.Set("Location", out.location)
	}
	if out.contentType != "" {
		h.Set("Content-Type", out.contentType)
	}
	w.WriteHeader(out.status)
	w.Write(out.body)
	return true
}

type result struct {
	status      int
	contentType string
	location    string
	body        []byte
	errMsg      string // set when the app failed; also written to the log
}

// handle runs one request against a stage and logs it.
func (p *Platform) handle(st *stage, r *http.Request, rid string) result {
	start := time.Now()
	out := p.dispatch(st, r, rid)
	level := "info"
	if out.status >= 500 {
		level = "error"
	} else if out.status >= 400 {
		level = "warn"
	}
	msg := fmt.Sprintf("%s %s -> %d in %s", r.Method, r.URL.Path, out.status, time.Since(start).Round(time.Microsecond))
	if out.errMsg != "" {
		msg += ": " + out.errMsg
	}
	p.event(st, "server", level, rid, msg)
	return out
}

func (p *Platform) dispatch(st *stage, r *http.Request, rid string) result {
	route, params := st.def.Match(r.Method, r.URL.Path)
	if route == nil {
		return result{status: http.StatusNotFound, contentType: "text/plain; charset=utf-8", body: []byte("not found")}
	}
	user := p.visitor(st, r)
	if route.Auth == "user" && user == nil {
		if r.Method == http.MethodGet {
			return result{status: http.StatusSeeOther, location: AuthPrefix + "login?next=" + url.QueryEscape(r.URL.RequestURI())}
		}
		return result{status: http.StatusUnauthorized, contentType: "text/plain; charset=utf-8", body: []byte("sign in required")}
	}
	query := map[string]string{}
	for k, v := range r.URL.Query() {
		query[k] = v[0]
	}
	if route.Template != "" {
		return p.render(st, http.StatusOK, route.Template, map[string]any{"params": params, "query": query, "user": user.Map()}, rid)
	}

	req := sandbox.Request{User: user, Origin: p.URL(st.app, st.name), Method: r.Method, Path: r.URL.Path, Params: params, Query: query, Form: map[string]string{}, FormAll: map[string][]string{}}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(nil, r.Body, maxBody)
		ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if ct == "application/json" {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				return failure(http.StatusRequestEntityTooLarge, "request body too large")
			}
			if len(data) > 0 && json.Unmarshal(data, &req.Body) != nil {
				return failure(http.StatusBadRequest, "request body is not valid JSON")
			}
		} else if err := r.ParseForm(); err == nil {
			for k, v := range r.PostForm {
				req.Form[k] = v[0]
				req.FormAll[k] = v
			}
		}
	}

	release, ok := p.acquireSlot(r.Context(), st.app)
	if !ok {
		return failure(http.StatusServiceUnavailable, "too many requests running at once for this app; the request waited 3s and gave up")
	}
	defer release()
	host := &scriptHost{st: st, p: p, rid: rid}
	resp, err := p.sb.Run(r.Context(), st.def.Scripts[route.Script], req, host)
	if err != nil {
		var se *sandbox.ScriptError
		if errors.As(err, &se) {
			return failure(http.StatusInternalServerError, "script "+route.Script+": "+se.Message)
		}
		return failure(http.StatusInternalServerError, "platform error: "+err.Error())
	}
	status := resp.Status
	if status < 200 || status > 599 {
		status = http.StatusOK
	}
	switch {
	case resp.Template != "":
		return p.render(st, status, resp.Template, resp.Data, rid)
	case resp.HasJSON:
		body, err := json.Marshal(resp.JSON)
		if err != nil {
			return failure(http.StatusInternalServerError, "script "+route.Script+": json value cannot be encoded: "+err.Error())
		}
		return result{status: status, contentType: "application/json", body: body}
	case resp.Redirect != "":
		if !host.mayRedirect(resp.Redirect) {
			return failure(http.StatusInternalServerError, "script "+route.Script+": redirect must be a path starting with / on this app")
		}
		if resp.Status < 300 || resp.Status > 399 {
			status = http.StatusSeeOther
		}
		return result{status: status, location: resp.Redirect}
	default:
		return result{status: status, contentType: "text/plain; charset=utf-8", body: []byte(resp.Text)}
	}
}

func failure(status int, msg string) result {
	return result{status: status, contentType: "text/plain; charset=utf-8",
		body: []byte("Something went wrong. The error has been logged."), errMsg: msg}
}

// render executes a template. Full HTML documents get the browser telemetry
// snippet so client-side errors reach the app log with this request's ID.
func (p *Platform) render(st *stage, status int, name string, data any, rid string) result {
	t := st.tmpl.Lookup(name)
	if t == nil {
		return failure(http.StatusInternalServerError, fmt.Sprintf("template %q is not defined", name))
	}
	buf := &limitedWriter{max: maxPage}
	if err := t.Execute(buf, data); err != nil {
		return failure(http.StatusInternalServerError, "template "+name+": "+err.Error())
	}
	page, injected := injectTelemetry(buf.buf, rid)
	if injected {
		p.issueRID(st.app, rid)
		if st.name == Live && p.cfg.OwnerPlan != nil && p.planOf(p.ownerOf(st.app)) == planFree {
			page = injectBadge(page)
		}
	}
	return result{status: status, contentType: "text/html; charset=utf-8", body: page}
}

// injectBadge adds the free-plan badge before </body>, or at the end.
func injectBadge(page []byte) []byte {
	at := bytes.LastIndex(bytes.ToLower(page), []byte("</body>"))
	if at < 0 {
		return append(page, badge...)
	}
	out := make([]byte, 0, len(page)+len(badge))
	out = append(out, page[:at]...)
	out = append(out, badge...)
	return append(out, page[at:]...)
}

func injectTelemetry(page []byte, rid string) ([]byte, bool) {
	lower := bytes.ToLower(page)
	at := bytes.Index(lower, []byte("<head>"))
	if at < 0 {
		return page, false // a fragment, not a full document
	}
	at += len("<head>")
	snippet := strings.Replace(telemetryJS, "__RID__", template.JSEscapeString(rid), 1)
	out := make([]byte, 0, len(page)+len(snippet))
	out = append(out, page[:at]...)
	out = append(out, snippet...)
	return append(out, page[at:]...), true
}

// telemetryJS reports browser errors to /_rx/log. It is small on purpose.
const telemetryJS = `<script>(function(){var r="__RID__",n=0;function s(l,m){if(n++>20)return;try{navigator.sendBeacon("/_rx/log",JSON.stringify({rid:r,level:l,message:String(m).slice(0,2000),page:location.pathname}))}catch(e){}}
addEventListener("error",function(e){if(e.target&&e.target!==window&&(e.target.src||e.target.href)){s("error","failed to load "+(e.target.src||e.target.href));return}s("error",(e.message||"error")+(e.filename?" at "+e.filename+":"+e.lineno+":"+e.colno:"")+(e.error&&e.error.stack?"\n"+e.error.stack:""))},true);
addEventListener("unhandledrejection",function(e){var x=e.reason;s("error","unhandled promise rejection: "+(x&&x.stack||x))});
var ce=console.error;console.error=function(){s("error",Array.prototype.map.call(arguments,String).join(" "));return ce.apply(console,arguments)};
var cw=console.warn;console.warn=function(){s("warn",Array.prototype.map.call(arguments,String).join(" "));return cw.apply(console,arguments)};
if(window.fetch){var f=window.fetch;window.fetch=function(){var u=String(arguments[0]&&arguments[0].url||arguments[0]);return f.apply(this,arguments).then(function(res){if(res.status>=500)s("warn","fetch "+u+" -> "+res.status);return res},function(err){s("error","fetch "+u+" failed: "+err);throw err})}}})();</script>`

// browserLog accepts telemetry beacons. The endpoint is public, so input is
// capped, rate limited per app and stored as data only.
func (p *Platform) browserLog(w http.ResponseWriter, r *http.Request, st *stage) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !p.allowIntake(st.app) {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	var in struct {
		RID     string `json:"rid"`
		Level   string `json:"level"`
		Message string `json:"message"`
		Page    string `json:"page"`
	}
	data, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 8<<10))
	if err != nil || json.Unmarshal(data, &in) != nil || in.Message == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if in.Level != "warn" {
		in.Level = "error"
	}
	// Only pages this app served may report, so the log, which the
	// building agent reads, does not take text from anyone on the internet.
	if !p.useRID(st.app, in.RID) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(in.Page) > 200 {
		in.Page = in.Page[:200]
	}
	p.event(st, "browser", in.Level, in.RID, "page "+in.Page+" (untrusted browser report): "+in.Message)
	w.WriteHeader(http.StatusNoContent)
}

// window is a fixed one-minute rate limit counter.
type window struct {
	start  time.Time
	period time.Duration
	count  int
}

const intakePerMinute = 120

func (p *Platform) allowIntake(app string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	w := p.intake[app]
	if w == nil || time.Since(w.start) > time.Minute {
		w = &window{start: time.Now()}
		p.intake[app] = w
	}
	w.count++
	return w.count <= intakePerMinute
}

// acquireSlot waits up to 3 seconds for a script slot of the app's owner
// and one of the app itself. The owner cap stops one account from filling
// every worker by spreading slow scripts over many apps.
func (p *Platform) acquireSlot(ctx context.Context, app string) (release func(), ok bool) {
	ownerSlots := p.slotsFor("owner:"+p.ownerOf(app), p.cfg.ScriptsPerOwner)
	appSlots := p.slotsFor("app:"+app, p.cfg.ScriptsPerApp)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	select {
	case ownerSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, false
	}
	select {
	case appSlots <- struct{}{}:
		return func() { <-appSlots; <-ownerSlots }, true
	case <-ctx.Done():
		<-ownerSlots
		return nil, false
	}
}

func (p *Platform) slotsFor(key string, n int) chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	slots := p.slots[key]
	if slots == nil {
		slots = make(chan struct{}, n)
		p.slots[key] = slots
	}
	return slots
}

// ownerOf returns an app's owner. Owners never change, so it is cached.
func (p *Platform) ownerOf(app string) string {
	if o, ok := p.owners.Load(app); ok {
		return o.(string)
	}
	var owner string
	if p.db.QueryRow(`SELECT owner FROM apps WHERE name = ?`, app).Scan(&owner) != nil {
		return ""
	}
	p.owners.Store(app, owner)
	return owner
}

func newRequestID() string {
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// scriptHost binds the sandbox host API to one stage and request.
type scriptHost struct {
	st  *stage
	p   *Platform
	rid string
	mu  sync.Mutex
	n   int
	// checkouts are the payment pages payments.checkout created during this
	// request; with Stripe Checkout itself, the only outside addresses a
	// script may redirect to.
	checkouts []string
	// readOnly is set for MCP tools marked read_only: assistants call those
	// without asking the person first, so the platform makes sure they
	// cannot change data, send messages or call other services.
	readOnly bool
	// http.fetch calls and time spent waiting on them in this run.
	fetches   int
	fetchWait time.Duration
}

// mayRedirect reports whether a script may send the visitor to target.
func (h *scriptHost) mayRedirect(target string) bool {
	if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") && !strings.HasPrefix(target, "/\\") {
		return true
	}
	if strings.HasPrefix(target, stripeCheckoutPrefix) {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.checkouts {
		if target == c {
			return true
		}
	}
	return false
}

var errReadOnlyTool = errors.New("this tool is marked read_only, so it cannot change data, send messages or call other services; remove read_only from the tool if it must")

const maxDBCalls = 200

func (h *scriptHost) count() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n++; h.n > maxDBCalls {
		return fmt.Errorf("more than %d database calls in one request", maxDBCalls)
	}
	return nil
}

func (h *scriptHost) Query(ctx context.Context, q string, args []any) ([]map[string]any, error) {
	if err := h.count(); err != nil {
		return nil, err
	}
	if h.readOnly {
		return h.st.db.QueryReadOnly(ctx, q, args)
	}
	return h.st.db.Query(ctx, q, args)
}

func (h *scriptHost) Exec(ctx context.Context, q string, args []any) (int64, int64, error) {
	if err := h.count(); err != nil {
		return 0, 0, err
	}
	if h.readOnly {
		return 0, 0, errReadOnlyTool
	}
	return h.st.db.Exec(ctx, q, args)
}

func (h *scriptHost) Log(level, message string) {
	h.mu.Lock()
	n := h.n
	h.n++
	h.mu.Unlock()
	if n < maxDBCalls*2 {
		h.p.event(h.st, "server", level, h.rid, "console: "+message)
	}
}

// Call implements sandbox.Host: the platform functions scripts may use.
func (h *scriptHost) Call(ctx context.Context, fn string, arg any) (any, error) {
	if err := h.count(); err != nil {
		return nil, err
	}
	if h.readOnly && fn != "payments.get" && fn != "realtime.token" {
		return nil, errReadOnlyTool
	}
	switch fn {
	case "payments.checkout":
		out, err := h.p.checkout(ctx, h.st, arg)
		if m, ok := out.(map[string]any); ok && err == nil {
			if u, ok := m["url"].(string); ok && !strings.HasPrefix(u, "/") {
				h.mu.Lock()
				h.checkouts = append(h.checkouts, u)
				h.mu.Unlock()
			}
		}
		return out, err
	case "payments.get":
		return h.p.paymentStatus(ctx, h.st, arg)
	case "email.send":
		return h.p.emailSend(ctx, h.st, arg)
	case "notify.owner":
		return h.p.notifyOwner(ctx, h.st, arg)
	case "http.fetch":
		return h.httpFetch(ctx, arg)
	case "realtime.publish":
		return h.p.realtimePublish(h.st, arg)
	case "realtime.token":
		return h.p.realtimeToken(h.st, arg)
	}
	return nil, fmt.Errorf("unknown platform function %s", fn)
}
