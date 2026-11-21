package platform

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// Screenshots of an app's preview, for the building agent to see its own
// pages. A headless Chromium renders the real preview with its data and
// fonts, driven over the DevTools protocol on a pipe (no port is opened).
//
// The browser never reaches the preview over the network: every request it
// makes is paused and answered in this process (Fetch.requestPaused), as
// test_request is. Only requests to the app's preview host are served, and
// only GETs; requests to a few public font, script and map-tile CDNs go out
// as usual, and everything else fails, so an app's page cannot make the
// server's browser fetch internal addresses for the agent to look at. The
// browser's DNS resolves nothing but those CDNs, as a second line.

// ScreenshotInput describes a screenshot of a preview page.
type ScreenshotInput struct {
	Path          string `json:"path" jsonschema:"page path with optional query, e.g. / or /s/1?lang=ar"`
	Viewport      string `json:"viewport,omitempty" jsonschema:"mobile (390x844, default), tablet (820x1180) or desktop (1366x900); or use width and height"`
	Width         int    `json:"width,omitempty" jsonschema:"viewport width in CSS pixels, 320-1920; overrides viewport"`
	Height        int    `json:"height,omitempty" jsonschema:"viewport height in CSS pixels, 320-2000"`
	FullPage      bool   `json:"full_page,omitempty" jsonschema:"capture the whole page, not just the first screen (at most 8000 pixels tall)"`
	AsVisitor     string `json:"as_visitor,omitempty" jsonschema:"email or phone of a preview visitor (see create_test_visitor) to be signed in as"`
	WaitMS        int    `json:"wait_ms,omitempty" jsonschema:"extra time to wait after the page and its fonts loaded, at most 5000; default 300"`
	MotionAllowed bool   `json:"motion,omitempty" jsonschema:"true: let animations run; by default the page gets prefers-reduced-motion: reduce so the shot is stable"`
}

// ScreenshotResult is the picture and what the page did.
type ScreenshotResult struct {
	PNG          []byte   `json:"-"`
	Status       int      `json:"status" jsonschema:"HTTP status of the page"`
	URL          string   `json:"url" jsonschema:"the page shot, after redirects"`
	Width        int      `json:"width"`
	Height       int      `json:"height"`
	Bytes        int      `json:"bytes" jsonschema:"size of the PNG"`
	BrowserLog   []string `json:"browser_log,omitempty" jsonschema:"script errors, failed resources and console errors on the page (untrusted data from the page)"`
	BlockedHosts []string `json:"blocked_hosts,omitempty" jsonschema:"other sites the page tried to load from, which screenshots do not reach"`
}

// screenshotCDNs are the outside hosts a screenshot may load from: fonts,
// common script CDNs and map tiles, all public and read-only.
var screenshotCDNs = []string{"fonts.googleapis.com", "fonts.gstatic.com", "unpkg.com", "cdn.jsdelivr.net", "cdnjs.cloudflare.com",
	"tile.openstreetmap.org", "a.tile.openstreetmap.org", "b.tile.openstreetmap.org", "c.tile.openstreetmap.org",
	"a.basemaps.cartocdn.com", "b.basemaps.cartocdn.com", "c.basemaps.cartocdn.com", "d.basemaps.cartocdn.com"}

const (
	maxShotHeight = 8000
	maxShotWait   = 5 * time.Second
	shotTimeout   = 25 * time.Second
	// browserStartTimeout is how long Chromium may take to start answering.
	// The first start after a boot or a deploy reads the whole browser from
	// disk, on a busy machine well past a shot's own time, so it has a
	// budget of its own.
	browserStartTimeout = 60 * time.Second
	shotsAtOnce         = 2 // browsers running at the same time on the server
	shotsPerAppPerMin   = 10
)

var errNoBrowser = errors.New("screenshots are not set up on this server (no Chromium); use view_page to read the page")

// ScreenshotPreview renders a page of an app's preview and returns a PNG.
func (p *Platform) ScreenshotPreview(ctx context.Context, app string, in ScreenshotInput) (ScreenshotResult, error) {
	if p.cfg.Chromium == "" && p.cfg.ChromiumSocket == "" {
		return ScreenshotResult{}, errNoBrowser
	}
	if _, err := p.Info(app); err != nil {
		return ScreenshotResult{}, err
	}
	st, err := p.loadStage(app, Preview)
	if err != nil || st == nil {
		return ScreenshotResult{}, errors.New("preview has no version yet: call apply_change or create_app with from_example first")
	}
	_, path, err := p.testTarget(app, Preview, in.Path)
	if err != nil {
		return ScreenshotResult{}, err
	}
	if strings.HasPrefix(path, AuthPrefix) {
		return ScreenshotResult{}, errors.New("the sign-in pages are the platform's own; screenshot the app's pages")
	}
	w, h, mobile, err := shotViewport(in)
	if err != nil {
		return ScreenshotResult{}, err
	}
	wait := 300 * time.Millisecond
	if in.WaitMS > 0 {
		wait = min(time.Duration(in.WaitMS)*time.Millisecond, maxShotWait)
	}
	if !p.allowShot(app) {
		return ScreenshotResult{}, fmt.Errorf("at most %d screenshots a minute per app; wait a little", shotsPerAppPerMin)
	}

	var visitorToken string
	if in.AsVisitor != "" {
		id, err := p.previewVisitorID(ctx, st, in.AsVisitor)
		if err != nil {
			return ScreenshotResult{}, err
		}
		visitorToken = secretToken()
		if _, err := st.admin.ExecContext(ctx, `INSERT INTO rx_sessions (hash, user_id, expires_at) VALUES (?, ?, ?)`, sha(visitorToken), id, time.Now().Add(browserStartTimeout+shotTimeout+10*time.Second).Unix()); err != nil {
			return ScreenshotResult{}, err
		}
		defer st.admin.Exec(`DELETE FROM rx_sessions WHERE hash = ?`, sha(visitorToken))
	}

	select {
	case p.shots <- struct{}{}:
		defer func() { <-p.shots }()
	case <-ctx.Done():
		return ScreenshotResult{}, ctx.Err()
	}
	// The browser lives for the start and the shot; each has its own limit.
	ctx, cancel := context.WithTimeout(ctx, browserStartTimeout+shotTimeout)
	defer cancel()
	b, err := p.startBrowser(ctx)
	if err != nil {
		return ScreenshotResult{}, err
	}
	defer b.close()
	startCtx, started := context.WithTimeout(ctx, browserStartTimeout)
	_, err = b.call(startCtx, "", "Browser.getVersion", nil)
	started()
	if err != nil {
		return ScreenshotResult{}, fmt.Errorf("browser: did not start: %w", err)
	}
	ctx, cancelShot := context.WithTimeout(ctx, shotTimeout)
	defer cancelShot()
	shot := &previewShot{p: p, app: app, host: p.host(app, Preview), visitorToken: visitorToken}
	return shot.take(ctx, b, p.URL(app, Preview)+path, w, h, mobile, in.FullPage, !in.MotionAllowed, wait)
}

func shotViewport(in ScreenshotInput) (w, h int, mobile bool, err error) {
	switch in.Viewport {
	case "", "mobile":
		w, h, mobile = 390, 844, true
	case "tablet":
		w, h, mobile = 820, 1180, true
	case "desktop":
		w, h = 1366, 900
	default:
		return 0, 0, false, fmt.Errorf("viewport must be mobile, tablet or desktop, or give width and height")
	}
	if in.Width != 0 || in.Height != 0 {
		if in.Width < 320 || in.Width > 1920 || in.Height < 320 || in.Height > 2000 {
			return 0, 0, false, errors.New("width must be 320-1920 and height 320-2000")
		}
		w, h, mobile = in.Width, in.Height, in.Width < 768
	}
	return w, h, mobile, nil
}

func (p *Platform) allowShot(app string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := "shot:" + app
	w := p.limits[key]
	if w == nil || time.Since(w.start) > time.Minute {
		w = &window{start: time.Now()}
		p.limits[key] = w
	}
	w.count++
	return w.count <= shotsPerAppPerMin
}

func (p *Platform) previewVisitorID(ctx context.Context, st *stage, who string) (int64, error) {
	addr, _ := normalizeEmail(who)
	phone, _ := whatsapp.Normalize(who)
	var id int64
	if err := st.admin.QueryRowContext(ctx, `SELECT id FROM rx_users WHERE email = ? OR phone = ?`, addr, phone).Scan(&id); err != nil {
		return 0, fmt.Errorf("no visitor %q in preview: make one with create_test_visitor", who)
	}
	return id, nil
}

// previewShot answers the browser's requests for one screenshot.
type previewShot struct {
	p            *Platform
	app, host    string
	visitorToken string

	mu       sync.Mutex
	status   int
	finalURL string
	log      []string
	blocked  map[string]bool
}

func (s *previewShot) note(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.log) < 20 {
		if len(line) > 400 {
			line = line[:400] + "…"
		}
		s.log = append(s.log, line)
	}
}

func (s *previewShot) take(ctx context.Context, b *browser, target string, w, h int, mobile, full, reduce bool, wait time.Duration) (ScreenshotResult, error) {
	tgt, err := b.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil {
		return ScreenshotResult{}, err
	}
	var t struct {
		TargetID string `json:"targetId"`
	}
	json.Unmarshal(tgt, &t)
	att, err := b.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": t.TargetID, "flatten": true})
	if err != nil {
		return ScreenshotResult{}, err
	}
	var a struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(att, &a)
	sid := a.SessionID

	loaded := make(chan struct{}, 1)
	b.onEvent(func(ev cdpMessage) {
		if ev.SessionID != sid {
			return
		}
		switch ev.Method {
		case "Fetch.requestPaused":
			go s.answer(ctx, b, sid, ev.Params)
		case "Page.loadEventFired":
			select {
			case loaded <- struct{}{}:
			default:
			}
		case "Runtime.exceptionThrown":
			var e struct {
				ExceptionDetails struct {
					Text      string `json:"text"`
					Exception struct {
						Description string `json:"description"`
					} `json:"exception"`
				} `json:"exceptionDetails"`
			}
			json.Unmarshal(ev.Params, &e)
			s.note("script error: " + e.ExceptionDetails.Text + " " + e.ExceptionDetails.Exception.Description)
		case "Runtime.consoleAPICalled":
			var c struct {
				Type string `json:"type"`
				Args []struct {
					Value       any    `json:"value"`
					Description string `json:"description"`
				} `json:"args"`
			}
			json.Unmarshal(ev.Params, &c)
			if c.Type == "error" || c.Type == "warning" {
				var parts []string
				for _, x := range c.Args {
					if x.Value != nil {
						parts = append(parts, fmt.Sprint(x.Value))
					} else {
						parts = append(parts, x.Description)
					}
				}
				s.note("console." + c.Type + ": " + strings.Join(parts, " "))
			}
		}
	})
	steps := []struct {
		method string
		params map[string]any
	}{
		{"Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}}},
		{"Runtime.enable", nil},
		{"Page.enable", nil},
		{"Emulation.setDeviceMetricsOverride", map[string]any{"width": w, "height": h, "deviceScaleFactor": 1, "mobile": mobile}},
		{"Emulation.setTouchEmulationEnabled", map[string]any{"enabled": mobile}},
	}
	if reduce {
		steps = append(steps, struct {
			method string
			params map[string]any
		}{"Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-reduced-motion", "value": "reduce"}}}})
	}
	if mobile {
		steps = append(steps, struct {
			method string
			params map[string]any
		}{"Emulation.setUserAgentOverride", map[string]any{"userAgent": "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1 RenoxiumPreview"}})
	}
	for _, st := range steps {
		if _, err := b.call(ctx, sid, st.method, st.params); err != nil {
			return ScreenshotResult{}, fmt.Errorf("browser: %s: %w", st.method, err)
		}
	}
	if _, err := b.call(ctx, sid, "Page.navigate", map[string]any{"url": target}); err != nil {
		return ScreenshotResult{}, fmt.Errorf("browser: open page: %w", err)
	}
	select {
	case <-loaded:
	case <-time.After(10 * time.Second):
		s.note("the page did not finish loading within 10 s; shot taken as it was")
	case <-ctx.Done():
		return ScreenshotResult{}, errors.New("the screenshot took too long")
	}
	fctx, fcancel := context.WithTimeout(ctx, 3*time.Second)
	b.call(fctx, sid, "Runtime.evaluate", map[string]any{"expression": "document.fonts.ready.then(() => 1)", "awaitPromise": true})
	fcancel()
	time.Sleep(wait)

	params := map[string]any{"format": "png"}
	outW, outH := w, h
	if full {
		raw, err := b.call(ctx, sid, "Page.getLayoutMetrics", nil)
		if err == nil {
			var m struct {
				CSSContentSize struct {
					Height float64 `json:"height"`
				} `json:"cssContentSize"`
			}
			json.Unmarshal(raw, &m)
			outH = min(max(int(m.CSSContentSize.Height+0.5), h), maxShotHeight)
			params["captureBeyondViewport"] = true
			params["clip"] = map[string]any{"x": 0, "y": 0, "width": w, "height": outH, "scale": 1}
		}
	}
	raw, err := b.call(ctx, sid, "Page.captureScreenshot", params)
	if err != nil {
		return ScreenshotResult{}, fmt.Errorf("browser: screenshot: %w", err)
	}
	var shot struct {
		Data string `json:"data"`
	}
	json.Unmarshal(raw, &shot)
	png, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil || len(png) == 0 {
		return ScreenshotResult{}, errors.New("browser: empty screenshot")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res := ScreenshotResult{PNG: png, Status: s.status, URL: s.finalURL, Width: outW, Height: outH, Bytes: len(png), BrowserLog: s.log}
	for h := range s.blocked {
		res.BlockedHosts = append(res.BlockedHosts, h)
	}
	return res, nil
}

// answer serves one paused browser request: the preview in this process,
// public CDNs from the network, anything else refused.
func (s *previewShot) answer(ctx context.Context, b *browser, sid string, raw json.RawMessage) {
	var ev struct {
		RequestID    string `json:"requestId"`
		ResourceType string `json:"resourceType"`
		Request      struct {
			URL     string            `json:"url"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
		} `json:"request"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	fail := func() {
		b.call(ctx, sid, "Fetch.failRequest", map[string]any{"requestId": ev.RequestID, "errorReason": "BlockedByClient"})
	}
	u, err := url.Parse(ev.Request.URL)
	if err != nil {
		fail()
		return
	}
	host := strings.ToLower(u.Hostname())
	ownHost := strings.ToLower(s.host)
	if i := strings.LastIndexByte(ownHost, ':'); i > 0 {
		ownHost = ownHost[:i]
	}
	if host != ownHost {
		if u.Scheme == "https" && isScreenshotCDN(host) {
			b.call(ctx, sid, "Fetch.continueRequest", map[string]any{"requestId": ev.RequestID})
			return
		}
		s.mu.Lock()
		if s.blocked == nil {
			s.blocked = map[string]bool{}
		}
		if len(s.blocked) < 20 {
			s.blocked[host] = true
		}
		s.mu.Unlock()
		fail()
		return
	}
	// The preview, answered here. Only GETs: a screenshot shows pages, it
	// does not submit forms. Live updates and browser reports are left out.
	path := u.EscapedPath()
	if strings.HasPrefix(path, AuthPrefix) && ev.ResourceType == "Document" {
		// The page sent the browser to sign in: say so in the picture.
		s.mu.Lock()
		s.status, s.finalURL = http.StatusUnauthorized, ev.Request.URL
		s.mu.Unlock()
		s.note("the page needs a signed-in visitor and redirected to " + u.RequestURI() + "; pass as_visitor (see create_test_visitor)")
		page := `<!doctype html><meta name="viewport" content="width=device-width"><body style="font:16px system-ui;padding:24px;color:#333">` +
			`<h1 style="font-size:20px">Sign-in page</h1><p>This page needs a signed-in visitor. Screenshots do not open the sign-in pages: take it again with as_visitor.</p>`
		b.call(ctx, sid, "Fetch.fulfillRequest", map[string]any{"requestId": ev.RequestID, "responseCode": 200,
			"responseHeaders": []map[string]string{{"name": "Content-Type", "value": "text/html; charset=utf-8"}}, "body": base64.StdEncoding.EncodeToString([]byte(page))})
		return
	}
	if ev.Request.Method != http.MethodGet || strings.HasPrefix(path, "/_rx/live/") || path == "/_rx/log" || strings.HasPrefix(path, AuthPrefix) {
		fail()
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req := httptest.NewRequestWithContext(rctx, http.MethodGet, "http://"+s.host+u.RequestURI(), nil)
	for k, v := range ev.Request.Headers {
		if !strings.EqualFold(k, "Cookie") && !strings.EqualFold(k, "Host") {
			req.Header.Set(k, v)
		}
	}
	if len(s.p.cfg.PreviewKey) > 0 {
		req.AddCookie(s.p.previewCookie(s.app))
	}
	if s.visitorToken != "" {
		req.AddCookie(&http.Cookie{Name: s.p.cookieName(visitorSessionCookie), Value: s.visitorToken})
	}
	w := httptest.NewRecorder()
	s.p.ServeApp(w, req)
	if ev.ResourceType == "Document" {
		s.mu.Lock()
		s.status, s.finalURL = w.Code, ev.Request.URL
		s.mu.Unlock()
		if w.Code >= 400 {
			s.note(fmt.Sprintf("the page answered %d", w.Code))
		}
	} else if w.Code >= 400 {
		s.note(fmt.Sprintf("failed to load %s: %d", u.RequestURI(), w.Code))
	}
	headers := []map[string]string{}
	for k, vs := range w.Header() {
		if strings.EqualFold(k, "Set-Cookie") {
			continue // the session is the shot's own; the page's cookies are not kept
		}
		for _, v := range vs {
			headers = append(headers, map[string]string{"name": k, "value": v})
		}
	}
	b.call(ctx, sid, "Fetch.fulfillRequest", map[string]any{"requestId": ev.RequestID, "responseCode": w.Code,
		"responseHeaders": headers, "body": base64.StdEncoding.EncodeToString(w.Body.Bytes())})
}

func isScreenshotCDN(host string) bool {
	for _, h := range screenshotCDNs {
		if host == h {
			return true
		}
	}
	return false
}

// --- a small DevTools protocol client over --remote-debugging-pipe ---

type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type browser struct {
	in      io.WriteCloser // the browser reads commands from here
	stop    func()         // ends the browser and removes what it wrote
	writeMu sync.Mutex
	next    atomic.Int64
	mu      sync.Mutex
	waiting map[int64]chan cdpMessage
	handler func(cdpMessage)
	done    chan struct{}
	stderr  *tailBuffer
}

// why gives the end of the browser's error output, for an error message.
func (b *browser) why() string {
	if b.stderr == nil {
		return "" // a browser behind the helper: its errors are in the helper's journal
	}
	if t := strings.TrimSpace(b.stderr.String()); t != "" {
		lines := strings.Split(t, "\n")
		if len(lines) > 6 {
			lines = lines[len(lines)-6:]
		}
		return " (Chromium: " + strings.Join(lines, " | ") + ")"
	}
	return ""
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// errUnsandboxed is why a browser is not started inside the platform's
// own process when Chromium cannot sandbox the pages it renders.
var errUnsandboxed = errors.New("screenshots are not set up safely on this server: Chromium would render pages without its sandbox inside the platform's own process; run the screenshot helper (docs/deploy-ubuntu.md, \"Screenshots for agents\")")

// startBrowser gives a fresh browser: from the screenshot helper when one
// is configured (its own service and user, away from the apps' data), or
// started here.
func (p *Platform) startBrowser(ctx context.Context) (*browser, error) {
	if p.cfg.ChromiumSocket != "" {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "unix", p.cfg.ChromiumSocket)
		if err != nil {
			return nil, fmt.Errorf("screenshot helper: %w", err)
		}
		b := &browser{in: conn, stop: func() { conn.Close() }, waiting: map[int64]chan cdpMessage{}, done: make(chan struct{})}
		go b.read(conn)
		return b, nil
	}
	// Chromium's sandbox cannot run as root. A page is written by whoever
	// owns the app, so without the sandbox a browser bug would give that
	// person this process and every app's data: only where the operator
	// said this is a development machine.
	noSandbox := os.Geteuid() == 0 || p.cfg.ChromiumNoSandbox
	if noSandbox && !p.cfg.ChromiumNoSandbox {
		return nil, errUnsandboxed
	}
	c, err := spawnChromium(ctx, p.cfg.Chromium, noSandbox)
	if err != nil {
		return nil, err
	}
	b := &browser{in: c.in, stop: c.stop, waiting: map[int64]chan cdpMessage{}, done: make(chan struct{}), stderr: c.stderr}
	go b.read(c.out)
	return b, nil
}

// chromiumProcess is a running headless Chromium and its DevTools pipe.
type chromiumProcess struct {
	in     io.WriteCloser // commands to the browser (its fd 3)
	out    io.ReadCloser  // messages from the browser (its fd 4)
	stderr *tailBuffer
	stop   func() // kills the browser and removes its profile directory
}

// spawnChromium starts a headless Chromium driven over a pipe (no port is
// opened), with a throwaway profile and a DNS that resolves only the CDNs.
func spawnChromium(ctx context.Context, chromium string, noSandbox bool) (*chromiumProcess, error) {
	dir, err := os.MkdirTemp("", "rx-shot-")
	if err != nil {
		return nil, err
	}
	toBrowser, browserIn, err := os.Pipe()
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	browserOut, fromBrowser, err := os.Pipe()
	if err != nil {
		toBrowser.Close()
		browserIn.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	rules := "MAP * ~NOTFOUND"
	for _, h := range screenshotCDNs {
		rules += ", EXCLUDE " + h
	}
	args := []string{"--headless=new", "--remote-debugging-pipe", "--user-data-dir=" + dir,
		"--no-first-run", "--no-default-browser-check", "--disable-gpu", "--disable-extensions", "--disable-sync",
		"--disable-background-networking", "--disable-component-update", "--disable-default-apps", "--mute-audio",
		"--hide-scrollbars", "--force-color-profile=srgb", "--disable-dev-shm-usage",
		"--host-resolver-rules=" + rules, "--disable-features=Translate,MediaRouter,OptimizationHints", "about:blank"}
	if noSandbox {
		args = append(args, "--no-sandbox")
	}
	cmd := exec.CommandContext(ctx, chromium, args...)
	cmd.ExtraFiles = []*os.File{toBrowser, fromBrowser} // fd 3: commands in, fd 4: messages out
	stderr := &tailBuffer{max: 4 << 10}                 // why a browser that stops stopped
	cmd.Stdout, cmd.Stderr = io.Discard, stderr
	cmd.Env = []string{"HOME=" + dir, "PATH=/usr/bin:/bin"}
	if err := cmd.Start(); err != nil {
		toBrowser.Close()
		browserIn.Close()
		browserOut.Close()
		fromBrowser.Close()
		os.RemoveAll(dir)
		return nil, fmt.Errorf("start Chromium: %w", err)
	}
	toBrowser.Close()
	fromBrowser.Close()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			browserIn.Close()
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			cmd.Wait()
			os.RemoveAll(dir)
		})
	}
	return &chromiumProcess{in: browserIn, out: browserOut, stderr: stderr, stop: stop}, nil
}

func (b *browser) read(r io.ReadCloser) {
	defer close(b.done)
	defer r.Close()
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		raw, err := br.ReadBytes(0)
		if err != nil {
			return
		}
		var m cdpMessage
		if json.Unmarshal(bytes.TrimSuffix(raw, []byte{0}), &m) != nil {
			continue
		}
		if m.ID != 0 {
			b.mu.Lock()
			ch := b.waiting[m.ID]
			delete(b.waiting, m.ID)
			b.mu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		b.mu.Lock()
		h := b.handler
		b.mu.Unlock()
		if h != nil {
			h(m)
		}
	}
}

func (b *browser) onEvent(h func(cdpMessage)) {
	b.mu.Lock()
	b.handler = h
	b.mu.Unlock()
}

func (b *browser) call(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	id := b.next.Add(1)
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if session != "" {
		msg["sessionId"] = session
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	ch := make(chan cdpMessage, 1)
	b.mu.Lock()
	b.waiting[id] = ch
	b.mu.Unlock()
	b.writeMu.Lock()
	_, err = b.in.Write(append(raw, 0))
	b.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("browser closed: %w%s", err, b.why())
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, errors.New(m.Error.Message)
		}
		return m.Result, nil
	case <-b.done:
		return nil, errors.New("browser closed" + b.why())
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.waiting, id)
		b.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (b *browser) close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	b.call(ctx, "", "Browser.close", nil)
	cancel()
	b.in.Close()
	select {
	case <-b.done:
	case <-time.After(2 * time.Second):
	}
	b.stop()
}
