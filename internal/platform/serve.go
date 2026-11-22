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
	"strconv"
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
	signIn := false // the request is for the stage's sign-in host
	if !ok {
		if app, stageName, ok = p.resolveID(r.Host); !ok {
			// An old address of a renamed app: the same path on the new one.
			if target, moved := p.renamedHost(r.Host); moved {
				http.Redirect(w, r, p.cfg.PublicScheme+"://"+target+p.cfg.PublicPort+r.URL.RequestURI(), http.StatusMovedPermanently)
				return true
			}
			return false
		}
		signIn = true
	}
	mw := &meterWriter{ResponseWriter: w}
	defer func() { p.meterRequest(app, stageName, mw.n) }()
	w = mw
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
	if stageName == Live && p.planLapsed(app) {
		p.lapsedPage(w)
		return true
	}
	demo := stageName == Preview && p.IsDemo(app)
	if stageName == Preview && !demo && !p.guardPreview(w, r, app) {
		return true
	}
	if demo {
		r = p.withDemoKey(w, r)
		if r.URL.Path == DemoInboxPath {
			p.serveDemoInbox(w, r, app)
			return true
		}
	}
	if strings.HasPrefix(r.URL.Path, FilesPrefix) && !signIn {
		p.serveFile(w, r, app)
		return true
	}
	st, err := p.loadStage(app, stageName)
	if err != nil {
		http.Error(w, "this app is temporarily unavailable", http.StatusServiceUnavailable)
		return true
	}
	if st == nil {
		writeSysPage(w, http.StatusNotFound, sysPageData{Title: "Not published", Heading: "Coming soon",
			Body: "<p>This app has not been published yet.</p>"})
		return true
	}
	if signIn {
		// Only the platform's sign-in pages: nothing of the app runs here.
		if !p.allow("req:"+app+":"+p.clientIP(r), requestsPerMinute, time.Minute) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many requests; try again in a minute", http.StatusTooManyRequests)
			return true
		}
		p.serveID(w, r, st)
		return true
	}
	if r.URL.Path == "/_rx/log" {
		p.browserLog(w, r, st)
		return true
	}
	if strings.HasPrefix(r.URL.Path, AuthPrefix) {
		p.serveAuthApp(w, r, st)
		return true
	}
	if r.URL.Path == UploadPath || r.URL.Path == UploadJSPath || strings.HasPrefix(r.URL.Path, PrivatePrefix) {
		p.servePrivateRoutes(w, r, st)
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
	if isSEOPath(r.URL.Path) && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		if route, _ := st.def.Match(http.MethodGet, r.URL.Path); route == nil {
			if !p.allow("req:"+app+":"+p.clientIP(r), requestsPerMinute, time.Minute) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "too many requests; try again in a minute", http.StatusTooManyRequests)
				return true
			}
			p.serveSEO(w, r, st)
			return true
		}
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
	out := browserPage(r, p.handle(st, r, rid), "/")
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
	// Search engines and link previews only ever see live, public pages.
	if stageName == Live && r.Method == http.MethodGet && out.status == http.StatusOK && strings.HasPrefix(out.contentType, "text/html") {
		if route, _ := st.def.Match(http.MethodGet, r.URL.Path); route != nil && route.Auth == "" {
			out.body = p.seoHead(st, r, out.body)
		}
	}
	h.Set("Content-Length", strconv.Itoa(len(out.body)))
	w.WriteHeader(out.status)
	w.Write(out.body)
	if stageName == Live || demo {
		p.trafficHit(app, r, out)
	}
	return true
}

type result struct {
	status      int
	contentType string
	location    string
	body        []byte
	errMsg      string   // set when the app failed; also written to the log
	warnings    []string // missing template keys, found only while checking a change (checks.go)
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
		data := map[string]any{"params": params, "query": query, "user": user.Map()}
		return checkMissingKeys(r.Context(), p.render(st, http.StatusOK, route.Template, data, rid), route.Template, data)
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

	host := &scriptHost{st: st, p: p, rid: rid, noMoney: user == nil}
	if isLiveTest(r.Context()) {
		host.readOnly, host.liveTest = true, true
	}
	defer host.runDeferred(r.Context()) // after release: on_payment needs a slot of its own
	release, err := p.acquireSlot(r.Context(), st)
	if err != nil {
		return failure(http.StatusServiceUnavailable, err.Error())
	}
	defer release()
	resp, err := p.runScript(r.Context(), st, route.Method+" "+route.Path, st.def.Scripts[route.Script], req, host)
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
	if status == http.StatusBadGateway || status == http.StatusGatewayTimeout {
		// Cloudflare, in front of every app, replaces a 502 or 504 from the
		// origin with its own error page, so the app's page would never
		// reach the visitor. 503 is passed through.
		status = http.StatusServiceUnavailable
	}
	switch {
	case resp.Template != "":
		return checkMissingKeys(r.Context(), p.render(st, status, resp.Template, resp.Data, rid), resp.Template, resp.Data)
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
		if st.name == Preview && p.IsDemo(st.app) {
			page = p.injectDemoBanner(st.app, page)
		}
	}
	return result{status: status, contentType: "text/html; charset=utf-8", body: page}
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
	// liveTest marks a test_request GET to live: read-only like a
	// read_only tool, so an agent's test never changes the live app.
	liveTest bool
	// http.fetch calls and time spent waiting on them in this run.
	fetches   int
	fetchWait time.Duration
	// on_payment runs started by this run's money calls (payments.refund,
	// capture, release, cancel_subscription), run once it has finished.
	hooks hookQueue
	// noMoney refuses payments.refund, capture, release and
	// cancel_subscription. It is set for runs anyone on the internet can
	// start without signing in: public MCP tools and requests without a
	// signed-in visitor. A script bug there (or a script written to) must
	// not let a stranger move a merchant's money. Signed-in visitors' own
	// requests, jobs, on_payment and on_admin_change keep them.
	noMoney bool
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

var errNoMoney = errors.New("payments.refund, capture, release and cancel_subscription only work in a request of a signed-in visitor " +
	`(a route with "auth": "user"), a job, on_payment or on_admin_change; not in public MCP tools or requests without a signed-in visitor`)

var errLiveTest = errors.New("view_live_page is read-only: it cannot change data, send messages or call other services; test changes on preview")

func (h *scriptHost) readOnlyErr() error {
	if h.liveTest {
		return errLiveTest
	}
	return errReadOnlyTool
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
		return 0, 0, h.readOnlyErr()
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
		return nil, h.readOnlyErr()
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
	case "payments.subscribe":
		out, err := h.p.subscribe(ctx, h.st, arg)
		if m, ok := out.(map[string]any); ok && err == nil {
			if u, ok := m["url"].(string); ok && !strings.HasPrefix(u, "/") {
				h.mu.Lock()
				h.checkouts = append(h.checkouts, u)
				h.mu.Unlock()
			}
		}
		return out, err
	case "payments.subscription":
		return h.p.subscriptionStatus(ctx, h.st, arg)
	case "payments.refund", "payments.capture", "payments.release", "payments.cancel_subscription":
		if h.noMoney {
			return nil, errNoMoney
		}
		ctx = context.WithValue(ctx, hookQueueKey{}, &h.hooks)
		switch fn {
		case "payments.refund":
			return h.p.scriptRefund(ctx, h.st, arg)
		case "payments.capture":
			return h.p.scriptCapture(ctx, h.st, arg)
		case "payments.release":
			return h.p.scriptRelease(ctx, h.st, arg)
		}
		return h.p.scriptCancelSubscription(ctx, h.st, arg)
	case "email.send":
		return h.p.emailSend(ctx, h.st, arg)
	case "notify.owner":
		return h.p.notifyOwner(ctx, h.st, arg)
	case "whatsapp.send":
		return h.p.whatsappSend(ctx, h.st, arg)
	case "http.fetch":
		return h.httpFetch(ctx, arg)
	case "realtime.publish":
		return h.p.realtimePublish(h.st, arg)
	case "realtime.token":
		return h.p.realtimeToken(h.st, arg)
	case "files.get", "files.delete", "files.url":
		return h.p.filesCall(ctx, h.st, fn, arg)
	}
	return nil, fmt.Errorf("unknown platform function %s", fn)
}
