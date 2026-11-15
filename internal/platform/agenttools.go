package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
)

// TestRequestInput is a request an agent sends to its own app.
type TestRequestInput struct {
	Stage  string            `json:"stage,omitempty" jsonschema:"preview (default) or live; live accepts only GET so tests never change live data"`
	Method string            `json:"method,omitempty" jsonschema:"GET (default) or POST"`
	Path   string            `json:"path" jsonschema:"path with optional query string, e.g. /products/2 or /search?q=oud"`
	Form   map[string]string `json:"form,omitempty" jsonschema:"form fields for a POST, sent as application/x-www-form-urlencoded"`
	JSON   any               `json:"json,omitempty" jsonschema:"JSON body for a POST instead of form (sent as application/json)"`
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
