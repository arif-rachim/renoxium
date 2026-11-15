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
	query := map[string]string{}
	for k, v := range r.URL.Query() {
		query[k] = v[0]
	}
	if route.Template != "" {
		return p.render(st, http.StatusOK, route.Template, map[string]any{"params": params, "query": query}, rid)
	}

	req := sandbox.Request{Method: r.Method, Path: r.URL.Path, Params: params, Query: query, Form: map[string]string{}, FormAll: map[string][]string{}}
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
		if !strings.HasPrefix(resp.Redirect, "/") || strings.HasPrefix(resp.Redirect, "//") || strings.HasPrefix(resp.Redirect, "/\\") {
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
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return failure(http.StatusInternalServerError, "template "+name+": "+err.Error())
	}
	return result{status: status, contentType: "text/html; charset=utf-8", body: injectTelemetry(buf.Bytes(), rid)}
}

func injectTelemetry(page []byte, rid string) []byte {
	lower := bytes.ToLower(page)
	at := bytes.Index(lower, []byte("<head>"))
	if at < 0 {
		return page // a fragment, not a full document
	}
	at += len("<head>")
	snippet := strings.Replace(telemetryJS, "__RID__", template.JSEscapeString(rid), 1)
	out := make([]byte, 0, len(page)+len(snippet))
	out = append(out, page[:at]...)
	out = append(out, snippet...)
	return append(out, page[at:]...)
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
	if len(in.RID) > 32 {
		in.RID = in.RID[:32]
	}
	if len(in.Page) > 200 {
		in.Page = in.Page[:200]
	}
	p.event(st, "browser", in.Level, in.RID, "page "+in.Page+": "+in.Message)
	w.WriteHeader(http.StatusNoContent)
}

// window is a fixed one-minute rate limit counter.
type window struct {
	start time.Time
	count int
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

// acquireSlot waits up to 3 seconds for one of the app's script slots.
func (p *Platform) acquireSlot(ctx context.Context, app string) (release func(), ok bool) {
	p.mu.Lock()
	slots := p.slots[app]
	if slots == nil {
		slots = make(chan struct{}, p.cfg.ScriptsPerApp)
		p.slots[app] = slots
	}
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	case <-ctx.Done():
		return nil, false
	}
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
}

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
	return h.st.db.Query(ctx, q, args)
}

func (h *scriptHost) Exec(ctx context.Context, q string, args []any) (int64, int64, error) {
	if err := h.count(); err != nil {
		return 0, 0, err
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
