package platform

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
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
	Stage  string              `json:"stage,omitempty" jsonschema:"preview (default) or live; live accepts only GET so tests never change live data"`
	Method string              `json:"method,omitempty" jsonschema:"GET (default) or POST"`
	Path   string              `json:"path,omitempty" jsonschema:"path with optional query string, e.g. /products/2 or /search?q=oud; required unless job or admin is set"`
	Form   map[string]string   `json:"form,omitempty" jsonschema:"form fields for a POST, sent as application/x-www-form-urlencoded"`
	JSON   any                 `json:"json,omitempty" jsonschema:"JSON body for a POST instead of form (sent as application/json)"`
	Files  map[string]TestFile `json:"files,omitempty" jsonschema:"files for a POST by field name, sent as multipart/form-data together with form; e.g. {\"file\": {...}} for POST /_rx/upload with user_email"`
	// UserEmail sends the request as a signed-in visitor (preview only).
	Job       string `json:"job,omitempty" jsonschema:"run this scheduled job once on preview instead of sending a request (path is then ignored); jobs never run on preview by themselves"`
	UserEmail string `json:"user_email,omitempty" jsonschema:"send the request signed in as the visitor with this email or WhatsApp number; preview only; the visitor must exist in preview (sign up through /_rx/auth/signup or /_rx/auth/phone first, or it may come from live data)"`

	// Admin makes an admin panel change on preview (see TestAdminInput).
	Admin *TestAdminInput `json:"admin,omitempty" jsonschema:"instead of a request, add, change or delete a preview row as the owner would in the admin panel (same checks), then run on_admin_change; path is then ignored"`
}

// TestFile is a file sent with a test request.
type TestFile struct {
	Name   string `json:"name" jsonschema:"file name, e.g. licence.jpg"`
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
	// Admin is set for test_request {admin}: the row after the change.
	Admin *TestAdminResult `json:"admin,omitempty"`
}

const maxTestBody = 20000

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
		return TestResponse{}, errors.New("POST is only allowed on preview, so tests never change live data")
	case !strings.HasPrefix(in.Path, "/") || strings.HasPrefix(in.Path, "//"):
		return TestResponse{}, errors.New("path must start with /")
	}

	var body *bytes.Reader
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
		body, ctype = bytes.NewReader(buf.Bytes()), mw.FormDataContentType()
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
	// Sign-in pages live on the stage's sign-in host; a test request goes
	// straight there, as a visitor's browser would after the redirect.
	host := p.host(app, stage)
	authPath := strings.SplitN(in.Path, "?", 2)[0]
	signIn := strings.HasPrefix(authPath, AuthPrefix) && authPath != AuthPrefix+"callback" && authPath != AuthPrefix+"logout"
	if signIn {
		host = p.idHost(app, stage)
	}
	req := httptest.NewRequestWithContext(ctx, method, "http://"+host+in.Path, body)
	if signIn {
		req.AddCookie(&http.Cookie{Name: p.cookieName(authFlowCookie), Value: secretToken()})
	}
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
	if method == http.MethodPost && len(in.Files) == 0 && (strings.HasPrefix(in.Path, AuthPrefix) || strings.HasPrefix(in.Path, PayPrefix)) {
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
	Hosts           []appdef.Host               `json:"hosts,omitempty" jsonschema:"if set, replaces the whole host list"`
	Secrets         []appdef.Secret             `json:"secrets,omitempty" jsonschema:"if set, replaces the whole secret list"`
	Tables          []appdef.Table              `json:"tables,omitempty" jsonschema:"tables to add, or columns to add to existing tables (matched by name); existing columns are kept, and a calendar given here replaces the table's calendar"`
	Seed            map[string][]map[string]any `json:"seed,omitempty" jsonschema:"seed rows for new tables, by table name"`
	Lib             map[string]string           `json:"lib,omitempty" jsonschema:"lib entries (shared script code) to add or replace, by name"`
	DeleteLib       []string                    `json:"delete_lib,omitempty"`
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
