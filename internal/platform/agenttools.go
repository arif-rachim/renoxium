package platform

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/files"
	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// TestRequestInput is a request an agent sends to its own app.
type TestRequestInput struct {
	Stage  string              `json:"stage,omitempty" jsonschema:"preview (default) or live; live accepts only GET, run read-only, so tests never change the live app"`
	Method string              `json:"method,omitempty" jsonschema:"GET (default) or POST; POST only to the preview"`
	Path   string              `json:"path,omitempty" jsonschema:"path with optional query string, e.g. /products/2 or /search?q=oud; required unless job or admin is set"`
	Form   map[string]string   `json:"form,omitempty" jsonschema:"form fields for a POST, sent as application/x-www-form-urlencoded"`
	JSON   any                 `json:"json,omitempty" jsonschema:"JSON body for a POST instead of form (sent as application/json)"`
	Files  map[string]TestFile `json:"files,omitempty" jsonschema:"files for a POST by field name, sent as multipart/form-data together with form; e.g. {\"file\": {...}} for POST /_rx/upload with user_email"`
	// UserEmail sends the request as a signed-in visitor (preview only).
	Job       string `json:"job,omitempty" jsonschema:"run this scheduled job once on preview instead of sending a request (path is then ignored); jobs never run on preview by themselves"`
	UserEmail string `json:"user_email,omitempty" jsonschema:"send the request signed in as the visitor with this email or WhatsApp number; preview only; the visitor must exist in preview (sign up through /_rx/auth/signup or /_rx/auth/phone first, or it may come from live data)"`

	// Session keeps cookies between calls (preview only), Follow follows
	// redirects within the preview's own hosts.
	Session string `json:"session,omitempty" jsonschema:"name of a cookie jar kept between calls, e.g. s1 (preview only, kept 1 hour): cookies the app or its sign-in pages set are sent again on later calls with the same session, so a real sign-up or sign-in can be followed through"`
	Follow  bool   `json:"follow_redirects,omitempty" jsonschema:"follow redirects (up to 8, as GET) within the preview and its sign-in host, keeping the session's cookies; the response is the last page, redirects lists the hops and logs has every hop's log"`

	// Admin makes an admin panel change on preview (see TestAdminInput).
	Admin *TestAdminInput `json:"admin,omitempty" jsonschema:"instead of a request, add, change or delete a preview row as the owner would in the admin panel (same checks), then run on_admin_change; path is then ignored"`
}

// liveTestKey marks the context of a test_request GET to live, whose
// script runs read-only (see scriptHost.liveTest).
type liveTestKey struct{}

func isLiveTest(ctx context.Context) bool { v, _ := ctx.Value(liveTestKey{}).(bool); return v }

// TestFile is a file sent with a test request.
type TestFile struct {
	Name   string `json:"name" jsonschema:"file name, e.g. photo.jpg"`
	Type   string `json:"type,omitempty" jsonschema:"Content-Type of the part; the platform ignores it and detects the type from the content"`
	Base64 string `json:"base64" jsonschema:"file content, base64-encoded; at most 10 MB"`
}

// TestResponse is what the app answered, with the log of that request.
type TestResponse struct {
	Status      int            `json:"status"`
	ContentType string         `json:"content_type,omitempty"`
	Location    string         `json:"location,omitempty" jsonschema:"redirect target, not followed"`
	Body        string         `json:"body" jsonschema:"response body without the platform's telemetry script; long <style> blocks shortened to a note of their size; cut at 20000 characters"`
	Truncated   bool           `json:"truncated,omitempty"`
	RequestID   string         `json:"request_id"`
	Logs        []applog.Event `json:"logs" jsonschema:"server log events of this request, including script console output and errors"`
	// Redirects lists the hops followed with follow_redirects.
	Redirects []string `json:"redirects,omitempty" jsonschema:"redirects followed, in order, as status and target"`
	// Admin is set for test_request {admin}: the row after the change.
	Admin *TestAdminResult `json:"admin,omitempty"`
	// WhatsApp lists the whatsapp.send messages of this request.
	WhatsApp []TestWhatsApp `json:"whatsapp,omitempty" jsonschema:"WhatsApp messages to customers this request would have sent from the live app; preview never sends them"`
}

// maxTestBody is where a test response's body is cut; tests that run a page's
// last script raise it.
var maxTestBody = 20000

// styleBlock matches an inline stylesheet. test_request leaves out the CSS of long ones (a design system's stylesheet can
// fill the whole body budget) so the agent still sees the page's content.
var styleBlock = regexp.MustCompile(`(?is)(<style[^>]*>)(.*?)(</style>)`)

const maxTestStyle = 1000

func shortenStyles(html string) string {
	return styleBlock.ReplaceAllStringFunc(html, func(m string) string {
		p := styleBlock.FindStringSubmatch(m)
		if len(p[2]) <= maxTestStyle {
			return m
		}
		return p[1] + fmt.Sprintf("/* %d bytes of CSS left out */", len(p[2])) + p[3]
	})
}

// TestRequest sends one request to an app stage, as a visitor would.
func (p *Platform) TestRequest(ctx context.Context, app string, in TestRequestInput) (TestResponse, error) {
	if _, err := p.Info(app); err != nil {
		return TestResponse{}, err
	}
	stage := in.Stage
	if stage == "" {
		stage = Preview
	}
	if in.Job != "" {
		return p.testJob(ctx, app, stage, in.Job)
	}
	if in.Admin != nil {
		return p.testAdmin(ctx, app, stage, *in.Admin)
	}
	method := strings.ToUpper(in.Method)
	if method == "" {
		method = http.MethodGet
	}
	switch {
	case stage != Preview && stage != Live:
		return TestResponse{}, errors.New("stage must be preview or live")
	case method != http.MethodGet && method != http.MethodPost:
		return TestResponse{}, errors.New("method must be GET or POST")
	case stage == Live && method != http.MethodGet:
		return TestResponse{}, errors.New("only GET requests may go to live; send POST and other requests to the preview (stage preview), so tests never change the live app")
	case stage == Live && (in.Session != "" || in.Follow):
		return TestResponse{}, errors.New("session and follow_redirects work only on preview")
	}
	host, path, err := p.testTarget(app, stage, in.Path)
	if err != nil {
		return TestResponse{}, err
	}
	if stage == Live && strings.HasPrefix(path, AuthPrefix) {
		// Sign-in links (email confirmation, password reset, sign-out) change visitor accounts even on GET.
		return TestResponse{}, errors.New("sign-in pages can only be tested on preview")
	}
	var jar *testJar
	if in.Session != "" {
		if jar, err = p.testSession(app, in.Session); err != nil {
			return TestResponse{}, err
		}
	}
	var visitorToken string
	if in.UserEmail != "" {
		if stage != Preview {
			return TestResponse{}, errors.New("as_visitor works only on preview")
		}
		st, err := p.loadStage(app, Preview)
		if err != nil || st == nil {
			return TestResponse{}, errors.New("preview has no version yet: call apply_change first")
		}
		addr, _ := normalizeEmail(in.UserEmail)
		phone, _ := whatsapp.Normalize(in.UserEmail)
		var id int64
		if err := st.admin.QueryRowContext(ctx, `SELECT id FROM rx_users WHERE email = ? OR phone = ?`, addr, phone).Scan(&id); err != nil {
			return TestResponse{}, fmt.Errorf("no visitor %q in preview: make one with create_test_visitor, or sign up with POST /_rx/auth/signup", in.UserEmail)
		}
		visitorToken = secretToken()
		if _, err := st.admin.ExecContext(ctx, `INSERT INTO rx_sessions (hash, user_id, expires_at) VALUES (?, ?, ?)`, sha(visitorToken), id, time.Now().Add(5*time.Minute).Unix()); err != nil {
			return TestResponse{}, err
		}
		defer st.admin.Exec(`DELETE FROM rx_sessions WHERE hash = ?`, sha(visitorToken))
	}

	var body []byte
	ctype := ""
	switch {
	case len(in.Files) > 0:
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for k, v := range in.Form {
			mw.WriteField(k, v)
		}
		for field, f := range in.Files {
			data := strings.TrimRight(strings.Join(strings.Fields(f.Base64), ""), "=")
			if base64.RawStdEncoding.DecodedLen(len(data)) > files.MaxSize+1 {
				return TestResponse{}, fmt.Errorf("files.%s: %w", field, files.ErrTooLarge)
			}
			raw, err := base64.RawStdEncoding.DecodeString(data)
			if err != nil {
				return TestResponse{}, fmt.Errorf("files.%s: base64 is not valid", field)
			}
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": field, "filename": f.Name}))
			if f.Type != "" {
				h.Set("Content-Type", f.Type)
			} else {
				h.Set("Content-Type", "application/octet-stream")
			}
			part, err := mw.CreatePart(h)
			if err != nil {
				return TestResponse{}, err
			}
			part.Write(raw)
		}
		mw.Close()
		body, ctype = buf.Bytes(), mw.FormDataContentType()
	case in.JSON != nil:
		raw, err := json.Marshal(in.JSON)
		if err != nil {
			return TestResponse{}, err
		}
		body, ctype = raw, "application/json"
	case len(in.Form) > 0:
		ctype = "application/x-www-form-urlencoded"
	}
	if stage == Live {
		ctx = context.WithValue(ctx, liveTestKey{}, true)
	}
	one := testHop{method: method, host: host, path: path, body: body, ctype: ctype, form: in.Form, files: len(in.Files) > 0}
	out, at, err := p.testSend(ctx, app, stage, one, jar, visitorToken)
	if err != nil || !in.Follow {
		return out, err
	}
	for hops := 0; out.Location != "" && out.Status >= 300 && out.Status < 400; hops++ {
		if hops == maxTestRedirects {
			out.Redirects = append(out.Redirects, "stopped after too many redirects")
			break
		}
		next, err := url.Parse(out.Location)
		if err != nil {
			break
		}
		base, _ := url.Parse("http://" + at + strings.SplitN(one.path, "?", 2)[0])
		target := base.ResolveReference(next)
		h, pth, err := p.testTarget(app, stage, target.String())
		if err != nil {
			out.Redirects = append(out.Redirects, fmt.Sprintf("%d %s (not followed: another site)", out.Status, out.Location))
			break
		}
		chain := append(out.Redirects, fmt.Sprintf("%d %s", out.Status, out.Location))
		logs, sent := out.Logs, out.WhatsApp
		one = testHop{method: http.MethodGet, host: h, path: pth}
		if out, at, err = p.testSend(ctx, app, stage, one, jar, visitorToken); err != nil {
			return out, err
		}
		out.Redirects, out.Logs = chain, append(logs, out.Logs...) // the log of every hop, in order
		out.WhatsApp = append(sent, out.WhatsApp...)
	}
	return out, nil
}

// testHop is one request of a test, after the input is checked.
type testHop struct {
	method, host, path, ctype string
	body                      []byte
	form                      map[string]string // urlencoded form fields, when body is empty
	files                     bool
}

// testSend sends one request to an app stage, as a visitor's browser would.
// host "" means the stage's own host, or its sign-in host for sign-in pages.
// It also returns the host the request went to.
func (p *Platform) testSend(ctx context.Context, app, stage string, hop testHop, jar *testJar, visitorToken string) (TestResponse, string, error) {
	host, path := hop.host, hop.path
	// Sign-in pages live on the stage's sign-in host; a test request goes
	// straight there, as a visitor's browser would after the redirect.
	authPath := strings.SplitN(path, "?", 2)[0]
	signIn := strings.HasPrefix(authPath, AuthPrefix) && authPath != AuthPrefix+"callback" && authPath != AuthPrefix+"logout"
	if host == "" {
		host = p.host(app, stage)
		// Without a session every sign-in request goes straight to the
		// sign-in host. In a session a GET starts on the app's host, as a
		// link would (it sets the hand-over state), and a POST goes to the
		// sign-in host, where the form is.
		if signIn && (jar == nil || hop.method == http.MethodPost) {
			host = p.idHost(app, stage)
		}
	}
	onID := host == p.idHost(app, stage)
	body := hop.body
	// Test requests carry a valid CSRF pair, so platform forms such as
	// /_rx/auth/signup can be exercised too. A session uses its own.
	csrfName := p.cookieName(visitorCSRFCookie)
	csrf := ""
	if hop.method == http.MethodPost && !hop.files && (strings.HasPrefix(path, AuthPrefix) || strings.HasPrefix(path, PayPrefix)) {
		if csrf = jar.get(host, csrfName); csrf == "" {
			csrf = secretToken()
		}
	}
	if len(body) == 0 && len(hop.form) > 0 {
		form := url.Values{}
		for k, v := range hop.form {
			form.Set(k, v)
		}
		if csrf != "" && form.Get("csrf") == "" {
			form.Set("csrf", csrf)
		}
		body = []byte(form.Encode())
	}
	req := httptest.NewRequestWithContext(ctx, hop.method, "http://"+host+path, bytes.NewReader(body))
	if hop.ctype != "" {
		req.Header.Set("Content-Type", hop.ctype)
	}
	if strings.SplitN(path, "?", 2)[0] == MCPPath {
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	if csrf != "" {
		req.AddCookie(&http.Cookie{Name: csrfName, Value: csrf})
	}
	// A GET carries the state in its query, as after the app host's redirect.
	if flow := p.cookieName(authFlowCookie); signIn && onID && jar.get(host, flow) == "" && (jar == nil || hop.method == http.MethodPost) {
		state := secretToken()
		req.AddCookie(&http.Cookie{Name: flow, Value: state})
		// A session that posts the form without opening the page first
		// gets the hand-over state a browser would hold, so the sign-in
		// finishes on the app host.
		jar.set(p.host(app, stage), p.cookieName(authStateCookie), state)
	}
	if visitorToken != "" && !onID {
		req.AddCookie(&http.Cookie{Name: p.cookieName(visitorSessionCookie), Value: visitorToken})
	}
	jar.add(req, host)
	if stage == Preview && len(p.cfg.PreviewKey) > 0 {
		if _, err := req.Cookie(p.cookieName("rx_preview")); err != nil {
			req.AddCookie(p.previewCookie(app))
		}
	}
	w := httptest.NewRecorder()
	p.ServeApp(w, req)
	jar.keep(host, w.Result().Cookies())

	rid := w.Header().Get("X-Request-Id")
	out := TestResponse{Status: w.Code, ContentType: w.Header().Get("Content-Type"), Location: w.Header().Get("Location"), RequestID: rid}
	text := w.Body.String()
	if rid != "" {
		text = strings.Replace(text, strings.Replace(telemetryJS, "__RID__", template.JSEscapeString(rid), 1), "", 1)
	}
	if !utf8.ValidString(text) {
		text = fmt.Sprintf("(%d bytes of binary content)", w.Body.Len())
	} else if strings.HasPrefix(out.ContentType, "text/html") {
		text = shortenStyles(text)
	}
	if len(text) > maxTestBody {
		cut := maxTestBody
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text, out.Truncated = text[:cut], true
	}
	out.Body = text
	out.WhatsApp = p.waOutbox.take(rid)
	out.Logs = []applog.Event{}
	if rid != "" {
		if l, err := p.log(app); err == nil {
			if events, err := l.Query(applog.Filter{RequestID: rid, Limit: 50}); err == nil {
				out.Logs = events
			}
		}
	}
	return out, host, nil
}

// Patch changes parts of an app's newest definition. Unset fields are kept.
type Patch struct {
	BaseVersion     int                         `json:"base_version,omitempty" jsonschema:"the version this patch was written against; the patch is refused if a newer version exists. Omit to patch the newest version."`
	Templates       map[string]string           `json:"templates,omitempty" jsonschema:"templates to add or replace, by name"`
	Scripts         map[string]string           `json:"scripts,omitempty" jsonschema:"scripts to add or replace, by name"`
	DeleteTemplates []string                    `json:"delete_templates,omitempty" jsonschema:"names of templates to remove"`
	DeleteScripts   []string                    `json:"delete_scripts,omitempty" jsonschema:"names of scripts to remove"`
	Routes          []appdef.Route              `json:"routes,omitempty" jsonschema:"if set, replaces the whole route list"`
	Tools           []appdef.Tool               `json:"tools,omitempty" jsonschema:"if set, replaces the whole MCP tool list"`
	Jobs            []appdef.Job                `json:"jobs,omitempty" jsonschema:"if set, replaces the whole job list"`
	Hosts           []appdef.Host               `json:"hosts,omitempty" jsonschema:"if set, replaces the whole host list"`
	Secrets         []appdef.Secret             `json:"secrets,omitempty" jsonschema:"if set, replaces the whole secret list"`
	Tables          []appdef.Table              `json:"tables,omitempty" jsonschema:"tables to add, or columns to add to existing tables (matched by name); existing columns are kept, and a calendar given here replaces the table's calendar"`
	Seed            map[string][]map[string]any `json:"seed,omitempty" jsonschema:"seed rows for new tables, by table name"`
	Lib             map[string]string           `json:"lib,omitempty" jsonschema:"lib entries (shared script code) to add or replace, by name"`
	DeleteLib       []string                    `json:"delete_lib,omitempty" jsonschema:"names of lib entries to remove"`
	OnAdminChange   *string                     `json:"on_admin_change,omitempty" jsonschema:"if set, replaces on_admin_change; empty turns it off"`
}

// ApplyPatch merges a patch into the newest version and applies the result
// to preview exactly like ApplyChange.
func (p *Platform) ApplyPatch(ctx context.Context, app string, patch Patch) (ApplyResult, error) {
	if _, err := p.Info(app); err != nil {
		return ApplyResult{}, err
	}
	def, n, err := p.Definition(app, 0)
	if errors.Is(err, ErrNotFound) {
		def, n = &appdef.App{}, 0
	} else if err != nil {
		return ApplyResult{}, err
	}
	if patch.BaseVersion != 0 && patch.BaseVersion != n {
		return ApplyResult{Problems: []string{fmt.Sprintf("version %d is the newest, not %d: read it with get_app and patch that", n, patch.BaseVersion)}}, nil
	}
	if def.Templates == nil {
		def.Templates = map[string]string{}
	}
	if def.Scripts == nil {
		def.Scripts = map[string]string{}
	}
	for k, v := range patch.Templates {
		def.Templates[k] = v
	}
	for k, v := range patch.Scripts {
		def.Scripts[k] = v
	}
	if len(patch.Lib) > 0 && def.Lib == nil {
		def.Lib = map[string]string{}
	}
	for k, v := range patch.Lib {
		def.Lib[k] = v
	}
	if patch.OnAdminChange != nil {
		def.OnAdminChange = *patch.OnAdminChange
	}
	var problems []string
	for _, k := range patch.DeleteTemplates {
		if _, ok := def.Templates[k]; !ok {
			problems = append(problems, fmt.Sprintf("delete_templates: %q does not exist", k))
		}
		delete(def.Templates, k)
	}
	for _, k := range patch.DeleteScripts {
		if _, ok := def.Scripts[k]; !ok {
			problems = append(problems, fmt.Sprintf("delete_scripts: %q does not exist", k))
		}
		delete(def.Scripts, k)
	}
	for _, k := range patch.DeleteLib {
		if _, ok := def.Lib[k]; !ok {
			problems = append(problems, fmt.Sprintf("delete_lib: %q does not exist", k))
		}
		delete(def.Lib, k)
	}
	if len(problems) > 0 {
		return ApplyResult{Problems: problems}, nil
	}
	if patch.Routes != nil {
		def.Routes = patch.Routes
	}
	if patch.Tools != nil {
		def.Tools = patch.Tools
	}
	if patch.Jobs != nil {
		def.Jobs = patch.Jobs
	}
	if patch.Hosts != nil {
		def.Hosts = patch.Hosts
	}
	if patch.Secrets != nil {
		def.Secrets = patch.Secrets
	}
	for _, t := range patch.Tables {
		merged := false
		for i := range def.Tables {
			if def.Tables[i].Name != t.Name {
				continue
			}
			merged = true
			if t.Calendar != nil {
				def.Tables[i].Calendar = t.Calendar
			}
			for _, c := range t.Columns {
				exists := false
				for _, old := range def.Tables[i].Columns {
					exists = exists || old.Name == c.Name
				}
				if !exists {
					def.Tables[i].Columns = append(def.Tables[i].Columns, c)
				}
			}
		}
		if !merged {
			def.Tables = append(def.Tables, t)
		}
	}
	if len(patch.Seed) > 0 && def.Seed == nil {
		def.Seed = map[string][]map[string]any{}
	}
	for k, v := range patch.Seed {
		def.Seed[k] = v
	}
	return p.ApplyChange(ctx, app, def)
}

// JobRun is the result of one scheduled job tried on preview.
type JobRun struct {
	Job      string `json:"job"`
	Schedule string `json:"schedule" jsonschema:"when it runs on the live site, e.g. every 15m or daily at 21:00 Asia/Dubai"`
	TestResponse
}

// TryScheduledJobs runs every job of the preview's definition once, in the
// order listed, as their schedules would on live. It takes no job name:
// the jobs are whatever the saved definition lists.
func (p *Platform) TryScheduledJobs(ctx context.Context, app string) ([]JobRun, error) {
	if _, err := p.Info(app); err != nil {
		return nil, err
	}
	st, err := p.loadStage(app, Preview)
	if err != nil || st == nil {
		return nil, errors.New("preview has no version yet: call apply_change first")
	}
	if len(st.def.Jobs) == 0 {
		return nil, errors.New("the app's definition lists no scheduled jobs")
	}
	out := make([]JobRun, 0, len(st.def.Jobs))
	for _, j := range st.def.Jobs {
		res, err := p.testJob(ctx, app, Preview, j.Name)
		if err != nil {
			return out, err
		}
		schedule := "every " + j.Every
		if j.DailyAt != "" {
			tz := j.Timezone
			if tz == "" {
				tz = "Asia/Dubai"
			}
			schedule = "daily at " + j.DailyAt + " " + tz
		}
		out = append(out, JobRun{Job: j.Name, Schedule: schedule, TestResponse: res})
	}
	return out, nil
}

// testJob runs one job on preview, as the scheduler would on live.
func (p *Platform) testJob(ctx context.Context, app, stage, name string) (TestResponse, error) {
	if stage != Preview {
		return TestResponse{}, errors.New("jobs can only be run by hand on preview; live runs them on their schedule")
	}
	st, err := p.loadStage(app, Preview)
	if err != nil || st == nil {
		return TestResponse{}, errors.New("preview has no version yet: call apply_change first")
	}
	for _, j := range st.def.Jobs {
		if j.Name != name {
			continue
		}
		rid := newRequestID()
		out := TestResponse{Status: http.StatusOK, ContentType: "text/plain; charset=utf-8", RequestID: rid}
		text, err := p.runJob(ctx, st, j, time.Now(), rid)
		if err != nil {
			out.Status, text = http.StatusInternalServerError, err.Error()
		}
		out.Body = text
		out.WhatsApp = p.waOutbox.take(rid)
		out.Logs = []applog.Event{}
		if l, err := p.log(app); err == nil {
			if events, err := l.Query(applog.Filter{RequestID: rid, Limit: 50}); err == nil {
				out.Logs = events
			}
		}
		return out, nil
	}
	return TestResponse{}, fmt.Errorf("preview has no job %q", name)
}
