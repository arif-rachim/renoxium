package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// TestRequestInput is a request an agent sends to its own app.
type TestRequestInput struct {
	Stage  string            `json:"stage,omitempty" jsonschema:"preview (default) or live; live accepts only GET so tests never change live data"`
	Method string            `json:"method,omitempty" jsonschema:"GET (default) or POST"`
	Path   string            `json:"path" jsonschema:"path with optional query string, e.g. /products/2 or /search?q=oud"`
	Form   map[string]string `json:"form,omitempty" jsonschema:"form fields for a POST, sent as application/x-www-form-urlencoded"`
	JSON   any               `json:"json,omitempty" jsonschema:"JSON body for a POST instead of form (sent as application/json)"`
	// UserEmail sends the request as a signed-in visitor (preview only).
	Job       string `json:"job,omitempty" jsonschema:"run this scheduled job once on preview instead of sending a request (path is then ignored); jobs never run on preview by themselves"`
	UserEmail string `json:"user_email,omitempty" jsonschema:"send the request signed in as the visitor with this email or WhatsApp number; preview only; the visitor must exist in preview (sign up through /_rx/auth/signup or /_rx/auth/phone first, or it may come from live data)"`
}

// TestResponse is what the app answered, with the log of that request.
type TestResponse struct {
	Status      int            `json:"status"`
	ContentType string         `json:"content_type,omitempty"`
	Location    string         `json:"location,omitempty" jsonschema:"redirect target, not followed"`
	Body        string         `json:"body" jsonschema:"response body without the platform's telemetry script; cut at 20000 characters"`
	Truncated   bool           `json:"truncated,omitempty"`
	RequestID   string         `json:"request_id"`
	Logs        []applog.Event `json:"logs" jsonschema:"server log events of this request, including script console output and errors"`
}

const maxTestBody = 20000

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
		return TestResponse{}, errors.New("POST is only allowed on preview, so tests never change live data")
	case !strings.HasPrefix(in.Path, "/") || strings.HasPrefix(in.Path, "//"):
		return TestResponse{}, errors.New("path must start with /")
	}

	var body *bytes.Reader
	ctype := ""
	switch {
	case in.JSON != nil:
		raw, err := json.Marshal(in.JSON)
		if err != nil {
			return TestResponse{}, err
		}
		body, ctype = bytes.NewReader(raw), "application/json"
	case len(in.Form) > 0:
		form := url.Values{}
		for k, v := range in.Form {
			form.Set(k, v)
		}
		body, ctype = bytes.NewReader([]byte(form.Encode())), "application/x-www-form-urlencoded"
	default:
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequestWithContext(ctx, method, "http://"+p.host(app, stage)+in.Path, body)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if strings.SplitN(in.Path, "?", 2)[0] == MCPPath {
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	if stage == Preview && len(p.cfg.PreviewKey) > 0 {
		req.AddCookie(p.previewCookie(app))
	}
	if in.UserEmail != "" {
		if stage != Preview {
			return TestResponse{}, errors.New("user_email works only on preview")
		}
		st, err := p.loadStage(app, Preview)
		if err != nil || st == nil {
			return TestResponse{}, errors.New("preview has no version yet: call apply_change first")
		}
		addr, _ := normalizeEmail(in.UserEmail)
		phone, _ := whatsapp.Normalize(in.UserEmail)
		var id int64
		if err := st.admin.QueryRowContext(ctx, `SELECT id FROM rx_users WHERE email = ? OR phone = ?`, addr, phone).Scan(&id); err != nil {
			return TestResponse{}, fmt.Errorf("no visitor %q in preview: sign up first with POST /_rx/auth/signup", in.UserEmail)
		}
		token := secretToken()
		if _, err := st.admin.ExecContext(ctx, `INSERT INTO rx_sessions (hash, user_id, expires_at) VALUES (?, ?, ?)`, sha(token), id, time.Now().Add(time.Minute).Unix()); err != nil {
			return TestResponse{}, err
		}
		defer st.admin.Exec(`DELETE FROM rx_sessions WHERE hash = ?`, sha(token))
		req.AddCookie(&http.Cookie{Name: p.cookieName(visitorSessionCookie), Value: token})
	}
	// Test requests carry a valid CSRF pair, so platform forms such as
	// /_rx/auth/signup can be exercised too.
	if method == http.MethodPost && (strings.HasPrefix(in.Path, AuthPrefix) || strings.HasPrefix(in.Path, PayPrefix)) {
		token := secretToken()
		req.AddCookie(&http.Cookie{Name: p.cookieName(visitorCSRFCookie), Value: token})
		if len(in.Form) > 0 {
			form := url.Values{"csrf": {token}}
			for k, v := range in.Form {
				form.Set(k, v)
			}
			req.Body = io.NopCloser(strings.NewReader(form.Encode()))
			req.ContentLength = int64(len(form.Encode()))
		}
	}
	w := httptest.NewRecorder()
	p.ServeApp(w, req)

	rid := w.Header().Get("X-Request-Id")
	out := TestResponse{Status: w.Code, ContentType: w.Header().Get("Content-Type"), Location: w.Header().Get("Location"), RequestID: rid}
	text := w.Body.String()
	if rid != "" {
		text = strings.Replace(text, strings.Replace(telemetryJS, "__RID__", template.JSEscapeString(rid), 1), "", 1)
	}
	if !utf8.ValidString(text) {
		text = fmt.Sprintf("(%d bytes of binary content)", w.Body.Len())
	}
	if len(text) > maxTestBody {
		cut := maxTestBody
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text, out.Truncated = text[:cut], true
	}
	out.Body = text
	out.Logs = []applog.Event{}
	if rid != "" {
		if l, err := p.log(app); err == nil {
			if events, err := l.Query(applog.Filter{RequestID: rid, Limit: 50}); err == nil {
				out.Logs = events
			}
		}
	}
	return out, nil
}

// Patch changes parts of an app's newest definition. Unset fields are kept.
type Patch struct {
	BaseVersion     int                         `json:"base_version,omitempty" jsonschema:"the version this patch was written against; the patch is refused if a newer version exists. Omit to patch the newest version."`
	Templates       map[string]string           `json:"templates,omitempty" jsonschema:"templates to add or replace, by name"`
	Scripts         map[string]string           `json:"scripts,omitempty" jsonschema:"scripts to add or replace, by name"`
	DeleteTemplates []string                    `json:"delete_templates,omitempty"`
	DeleteScripts   []string                    `json:"delete_scripts,omitempty"`
	Routes          []appdef.Route              `json:"routes,omitempty" jsonschema:"if set, replaces the whole route list"`
	Tools           []appdef.Tool               `json:"tools,omitempty" jsonschema:"if set, replaces the whole MCP tool list"`
	Jobs            []appdef.Job                `json:"jobs,omitempty" jsonschema:"if set, replaces the whole job list"`
	Tables          []appdef.Table              `json:"tables,omitempty" jsonschema:"tables to add, or columns to add to existing tables (matched by name); existing columns are kept"`
	Seed            map[string][]map[string]any `json:"seed,omitempty" jsonschema:"seed rows for new tables, by table name"`
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
	for _, t := range patch.Tables {
		merged := false
		for i := range def.Tables {
			if def.Tables[i].Name != t.Name {
				continue
			}
			merged = true
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
