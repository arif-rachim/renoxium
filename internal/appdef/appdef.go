// Package appdef defines the declarative description of a Renoxium app:
// tables, templates, server scripts and routes. Agents submit this as JSON.
package appdef

import (
	"encoding/json"
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MaxSize is the largest accepted definition, in bytes of JSON.
const MaxSize = 1 << 20

// App is one version of an app definition.
type App struct {
	Tables    []Table                     `json:"tables,omitempty" jsonschema:"database tables; every table automatically gets id and created_at columns"`
	Templates map[string]string           `json:"templates,omitempty" jsonschema:"named Go html/template sources; may contain any HTML, CSS and browser JavaScript"`
	Scripts   map[string]string           `json:"scripts,omitempty" jsonschema:"named server-side JavaScript sources, each defining function handle(req)"`
	Routes    []Route                     `json:"routes" jsonschema:"HTTP routes served by the app"`
	OnPayment string                      `json:"on_payment,omitempty" jsonschema:"script run when a payment started with payments.checkout is paid, fails or expires; req.body holds the payment"`
	Seed      map[string][]map[string]any `json:"seed,omitempty" jsonschema:"rows inserted into a table when that table is first created, keyed by table name"`
	Hosts     []Host                      `json:"hosts,omitempty" jsonschema:"external HTTPS hosts scripts may call with http.fetch; each needs the owner's approval in the admin panel before it works"`
	Secrets   []Secret                    `json:"secrets,omitempty" jsonschema:"API keys the owner enters in the admin panel; http.fetch adds them to requests without scripts ever seeing them"`
	Jobs      []Job                       `json:"jobs,omitempty" jsonschema:"scripts run on a schedule on the live app, e.g. cancel unpaid orders every 15 minutes or send a daily report"`
	Tools     []Tool                      `json:"tools,omitempty" jsonschema:"public MCP tools served at /mcp on the app's address, so shoppers' AI assistants (ChatGPT, Claude) can use the app; anyone can call them, without signing in"`
	// Lib and OnAdminChange: shared script code and the admin-change hook.
	Lib           map[string]string `json:"lib,omitempty" jsonschema:"shared server JavaScript by name (helper functions and constants used by several scripts); evaluated in name order before every script, job, tool and hook; must not define handle"`
	OnAdminChange string            `json:"on_admin_change,omitempty" jsonschema:"script run right after the owner or staff adds, changes or deletes a row in the admin panel; req.body holds {action, table, id, row, before, by}"`
}

// MaxLibs is how many lib entries an app may have.
const MaxLibs = 20

// LibNames returns the names of the app's lib entries in the order they
// are evaluated.
func (a *App) LibNames() []string {
	names := make([]string, 0, len(a.Lib))
	for n := range a.Lib {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Tool is a public MCP tool backed by a script. The script gets req.method
// "TOOL", req.path "/mcp/<name>" and the arguments in req.body, with no
// signed-in visitor; it returns {json: value} or {text: "..."}.
type Tool struct {
	Name        string      `json:"name" jsonschema:"lowercase name, e.g. search_products"`
	Description string      `json:"description" jsonschema:"what the tool does and when to use it, for the AI assistant that calls it"`
	Script      string      `json:"script" jsonschema:"script that handles the call"`
	Params      []ToolParam `json:"params,omitempty"`
	ReadOnly    bool        `json:"read_only,omitempty" jsonschema:"true when the tool only reads data (search, details); assistants then call it without asking the user first"`
}

// ToolParam is one argument of a Tool.
type ToolParam struct {
	Name        string `json:"name"`
	Type        string `json:"type" jsonschema:"string, number, integer or boolean"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Job is a script run on a schedule on the live app, with no visitor. The
// script gets req.method "JOB", req.path "/_rx/jobs/<name>" and req.body
// {job, scheduled_at}. Set Every or DailyAt, not both.
type Job struct {
	Name     string `json:"name" jsonschema:"lowercase name, e.g. cancel_unpaid"`
	Script   string `json:"script"`
	Every    string `json:"every,omitempty" jsonschema:"run repeatedly, e.g. 15m, 1h or 6h (Go duration; at least 1m, 5m on the Free plan)"`
	DailyAt  string `json:"daily_at,omitempty" jsonschema:"run once a day at this local time, HH:MM, e.g. 08:00"`
	Timezone string `json:"timezone,omitempty" jsonschema:"IANA time zone for daily_at; default Asia/Dubai"`
}

// Host is an external site an app's scripts may call.
type Host struct {
	Host    string `json:"host" jsonschema:"exact host name, e.g. api.example.com (HTTPS on port 443 only)"`
	Purpose string `json:"purpose" jsonschema:"one sentence the owner reads before allowing it, e.g. Calculates Aramex delivery prices"`
}

// Secret is an API key kept by the platform. http.fetch({secret: name})
// sends it to its host in a header (with an optional prefix such as
// "Bearer") or as a query parameter.
type Secret struct {
	Name   string `json:"name" jsonschema:"upper-case name, e.g. ARAMEX_API_KEY"`
	Host   string `json:"host" jsonschema:"the only host it is ever sent to; must be in hosts"`
	Header string `json:"header,omitempty" jsonschema:"header that carries it, e.g. Authorization or X-Api-Key"`
	Prefix string `json:"prefix,omitempty" jsonschema:"text before the key in the header, e.g. Bearer; Basic means the owner enters user:password"`
	Query  string `json:"query,omitempty" jsonschema:"query parameter that carries it instead of a header, e.g. api_key"`
	Help   string `json:"help,omitempty" jsonschema:"where the owner finds the key, e.g. Aramex developer portal, API keys page"`
}

var (
	hostNameRE   = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	secretNameRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	headerNameRE = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	queryNameRE  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
)

// MaxJobs is how many jobs an app may have.
const MaxJobs = 10

// JobInterval returns how often an Every job runs.
func (j Job) JobInterval() (time.Duration, error) { return time.ParseDuration(j.Every) }

// ToolParamTypes are the accepted ToolParam types.
var ToolParamTypes = map[string]bool{"string": true, "number": true, "integer": true, "boolean": true}

// MaxTools is how many tools an app may have.
const MaxTools = 20

// Table is a tenant database table.
type Table struct {
	Name    string   `json:"name"`
	Label   string   `json:"label,omitempty" jsonschema:"name shown to the owner in the admin panel, e.g. Orders; defaults to the table name"`
	Columns []Column `json:"columns"`
	// Calendar adds a day and week schedule of the table to the admin panel.
	Calendar *Calendar `json:"calendar,omitempty" jsonschema:"show this table to the owner as a day and week schedule too, e.g. bookings by staff"`
}

// Column is a table column. Type is one of text, integer, real, boolean,
// datetime, json or image; an image column holds the URL of an uploaded file.
type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type" jsonschema:"one of: text, integer, real, boolean, datetime, json, image (holds the url returned by upload_file), file (holds the id of a visitor's private upload from /_rx/upload)"`
	Required bool   `json:"required,omitempty"`
	Unique   bool   `json:"unique,omitempty"`
	// Admin panel only; they do not change the database.
	Label   string   `json:"label,omitempty" jsonschema:"name shown to the owner in the admin panel, e.g. Customer name; defaults to the column name"`
	Help    string   `json:"help,omitempty" jsonschema:"one short sentence shown under the field in the admin panel"`
	Options []string `json:"options,omitempty" jsonschema:"text columns only: the allowed values, shown as a drop-down in the admin panel, e.g. [\"pending\",\"paid\",\"shipped\"]"`
}

// Route maps a method and path to a template or a script.
type Route struct {
	Method   string `json:"method" jsonschema:"GET or POST"`
	Path     string `json:"path" jsonschema:"URL path; segments starting with ':' are parameters, e.g. /products/:id"`
	Template string `json:"template,omitempty" jsonschema:"render this template directly (set this or script, not both)"`
	Script   string `json:"script,omitempty" jsonschema:"run this script (set this or template, not both)"`
	Auth     string `json:"auth,omitempty" jsonschema:"set to user so only signed-in visitors reach the route; others are sent to the sign-in page"`
}

var identRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

var toolNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ColumnTypes maps definition types to SQLite column types.
var ColumnTypes = map[string]string{
	"text": "TEXT", "integer": "INTEGER", "real": "REAL",
	"boolean": "INTEGER", "datetime": "TEXT", "json": "TEXT", "image": "TEXT",
	"file": "TEXT", // the id of a visitor's private upload (/_rx/upload)
}

// ValidIdent reports whether s is usable as a table or column name.
func ValidIdent(s string) bool {
	return identRE.MatchString(s) && !strings.HasPrefix(s, "rx_") && !strings.HasPrefix(s, "sqlite_")
}

// Parse decodes and validates a definition. Problems are returned as a list
// of human-readable messages meant for the building agent.
func Parse(data []byte) (*App, []string) {
	if len(data) > MaxSize {
		return nil, []string{fmt.Sprintf("definition is %d bytes; the limit is %d", len(data), MaxSize)}
	}
	var a App
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, []string{"definition is not valid JSON: " + err.Error()}
	}
	return &a, a.Validate()
}

// Validate checks structure, names, references and template syntax.
func (a *App) Validate() []string {
	var p []string
	add := func(format string, args ...any) { p = append(p, fmt.Sprintf(format, args...)) }

	tables := map[string]bool{}
	for _, t := range a.Tables {
		if !ValidIdent(t.Name) {
			add("table %q: name must match [a-z][a-z0-9_]* and not start with rx_ or sqlite_", t.Name)
		}
		if tables[t.Name] {
			add("table %q is defined twice", t.Name)
		}
		tables[t.Name] = true
		cols := map[string]bool{}
		if len(t.Columns) == 0 {
			add("table %q has no columns", t.Name)
		}
		for _, c := range t.Columns {
			switch {
			case c.Name == "id" || c.Name == "created_at":
				add("table %q: column %q is added automatically; remove it", t.Name, c.Name)
			case !ValidIdent(c.Name):
				add("table %q: column %q: name must match [a-z][a-z0-9_]*", t.Name, c.Name)
			case cols[c.Name]:
				add("table %q: column %q is defined twice", t.Name, c.Name)
			}
			cols[c.Name] = true
			if _, ok := ColumnTypes[c.Type]; !ok {
				add("table %q: column %q: unknown type %q (use text, integer, real, boolean, datetime, json, image or file)", t.Name, c.Name, c.Type)
			}
			if len(c.Options) > 0 && c.Type != "text" {
				add("table %q: column %q: options are only for text columns", t.Name, c.Name)
			}
			if len(c.Options) > 100 || len(c.Label) > 80 || len(c.Help) > 300 {
				add("table %q: column %q: keep label under 80 characters, help under 300 and at most 100 options", t.Name, c.Name)
			}
		}
	}
	p = append(p, a.validateCalendars()...)
	for name, rows := range a.Seed {
		if !tables[name] {
			add("seed: table %q is not defined", name)
		}
		for i, row := range rows {
			for col := range row {
				if !ValidIdent(col) {
					add("seed %q row %d: invalid column name %q", name, i, col)
				}
			}
		}
	}

	if _, err := a.ParseTemplates(); err != nil {
		add("templates: %v", err)
	}

	hosts := map[string]bool{}
	if len(a.Hosts) > 20 {
		add("hosts: at most 20")
	}
	for _, h := range a.Hosts {
		switch {
		case !hostNameRE.MatchString(h.Host) || len(h.Host) > 253:
			add("host %q: give the exact lowercase host name, e.g. api.example.com (no scheme, path, port or IP address)", h.Host)
		case hosts[h.Host]:
			add("host %q is listed twice", h.Host)
		case strings.TrimSpace(h.Purpose) == "" || len(h.Purpose) > 300:
			add("host %q: purpose is required (at most 300 characters); the owner reads it before allowing the host", h.Host)
		}
		hosts[h.Host] = true
	}
	secretNames := map[string]bool{}
	for _, s := range a.Secrets {
		switch {
		case !secretNameRE.MatchString(s.Name) || secretNames[s.Name]:
			add("secret %q: name must be unique and match [A-Z][A-Z0-9_]*", s.Name)
		case !hosts[s.Host]:
			add("secret %q: host %q must be listed in hosts", s.Name, s.Host)
		case (s.Header == "") == (s.Query == ""):
			add("secret %q: set either header or query", s.Name)
		case s.Header != "" && !headerNameRE.MatchString(s.Header):
			add("secret %q: header %q is not a valid header name", s.Name, s.Header)
		case s.Query != "" && (!queryNameRE.MatchString(s.Query) || s.Prefix != ""):
			add("secret %q: query must be a plain parameter name, without prefix", s.Name)
		case len(s.Prefix) > 20 || strings.ContainsAny(s.Prefix, "\r\n:"):
			add("secret %q: prefix must be a short word such as Bearer", s.Name)
		case len(s.Help) > 300:
			add("secret %q: help is at most 300 characters", s.Name)
		}
		secretNames[s.Name] = true
	}

	jobNames := map[string]bool{}
	if len(a.Jobs) > MaxJobs {
		add("jobs: at most %d jobs", MaxJobs)
	}
	for _, j := range a.Jobs {
		switch {
		case !toolNameRE.MatchString(j.Name):
			add("job %q: name must match [a-z][a-z0-9_]{0,63}", j.Name)
		case jobNames[j.Name]:
			add("job %q is defined twice", j.Name)
		}
		jobNames[j.Name] = true
		if _, ok := a.Scripts[j.Script]; !ok {
			add("job %q: script %q is not defined", j.Name, j.Script)
		}
		switch {
		case (j.Every == "") == (j.DailyAt == ""):
			add("job %q: set either every (e.g. 15m) or daily_at (e.g. 08:00)", j.Name)
		case j.Every != "":
			if d, err := j.JobInterval(); err != nil || d < time.Minute || d > 7*24*time.Hour {
				add("job %q: every must be a duration from 1m to 168h, e.g. 15m or 2h", j.Name)
			}
		default:
			if _, err := time.Parse("15:04", j.DailyAt); err != nil || len(j.DailyAt) != 5 {
				add("job %q: daily_at must be HH:MM, e.g. 08:00", j.Name)
			}
		}
		if j.Timezone != "" {
			if _, err := time.LoadLocation(j.Timezone); err != nil {
				add("job %q: unknown timezone %q (use an IANA name such as Asia/Dubai)", j.Name, j.Timezone)
			}
		}
	}

	toolNames := map[string]bool{}
	if len(a.Tools) > MaxTools {
		add("tools: at most %d tools", MaxTools)
	}
	for _, t := range a.Tools {
		switch {
		case !toolNameRE.MatchString(t.Name):
			add("tool %q: name must match [a-z][a-z0-9_]{0,63}", t.Name)
		case toolNames[t.Name]:
			add("tool %q is defined twice", t.Name)
		}
		toolNames[t.Name] = true
		if strings.TrimSpace(t.Description) == "" || len(t.Description) > 2000 {
			add("tool %q: description is required (at most 2000 characters)", t.Name)
		}
		if _, ok := a.Scripts[t.Script]; !ok {
			add("tool %q: script %q is not defined", t.Name, t.Script)
		}
		if len(t.Params) > 20 {
			add("tool %q: at most 20 params", t.Name)
		}
		params := map[string]bool{}
		for _, prm := range t.Params {
			if !ValidIdent(prm.Name) || params[prm.Name] {
				add("tool %q: param %q: name must match [a-z][a-z0-9_]* and be unique", t.Name, prm.Name)
			}
			params[prm.Name] = true
			if !ToolParamTypes[prm.Type] {
				add("tool %q: param %q: type must be string, number, integer or boolean", t.Name, prm.Name)
			}
		}
	}

	if a.OnPayment != "" {
		if _, ok := a.Scripts[a.OnPayment]; !ok {
			add("on_payment: script %q is not defined", a.OnPayment)
		}
	}
	if a.OnAdminChange != "" {
		if _, ok := a.Scripts[a.OnAdminChange]; !ok {
			add("on_admin_change: script %q is not defined", a.OnAdminChange)
		}
	}
	if len(a.Lib) > MaxLibs {
		add("lib: at most %d entries; put related helpers together", MaxLibs)
	}
	for _, n := range a.LibNames() {
		if !toolNameRE.MatchString(n) {
			add("lib %q: name must match [a-z][a-z0-9_]{0,63}", n)
		}
	}
	if len(a.Routes) == 0 {
		add("routes: at least one route is required")
	}
	seen := map[string]bool{}
	for i, r := range a.Routes {
		where := fmt.Sprintf("route %d (%s %s)", i, r.Method, r.Path)
		if r.Method != "GET" && r.Method != "POST" {
			add("%s: method must be GET or POST", where)
		}
		switch {
		case !strings.HasPrefix(r.Path, "/"):
			add("%s: path must start with /", where)
		case strings.HasPrefix(r.Path, "/_rx/") || r.Path == "/mcp":
			add("%s: paths under /_rx/ and /mcp are reserved for the platform", where)
		}
		if r.Auth != "" && r.Auth != "user" {
			add("%s: auth must be empty or \"user\"", where)
		}
		key := r.Method + " " + r.Path
		if seen[key] {
			add("%s: defined twice", where)
		}
		seen[key] = true
		switch {
		case (r.Template == "") == (r.Script == ""):
			add("%s: set exactly one of template or script", where)
		case r.Template != "":
			if _, ok := a.Templates[r.Template]; !ok {
				add("%s: template %q is not defined", where, r.Template)
			}
		default:
			if _, ok := a.Scripts[r.Script]; !ok {
				add("%s: script %q is not defined", where, r.Script)
			}
		}
	}
	return p
}

// ParseTemplates builds one template set containing every named template,
// so templates can include each other with {{template "name" .}}.
func (a *App) ParseTemplates() (*template.Template, error) {
	root := template.New("")
	names := make([]string, 0, len(a.Templates))
	for n := range a.Templates {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if n == "" {
			return nil, fmt.Errorf("a template has an empty name")
		}
		if _, err := root.New(n).Parse(a.Templates[n]); err != nil {
			return nil, fmt.Errorf("%q: %v", n, err)
		}
	}
	return root, nil
}

// Match finds the route for a method and path and returns its path parameters.
func (a *App) Match(method, path string) (*Route, map[string]string) {
	want := splitPath(path)
	for i := range a.Routes {
		r := &a.Routes[i]
		if r.Method != method {
			continue
		}
		have := splitPath(r.Path)
		if len(have) != len(want) {
			continue
		}
		params := map[string]string{}
		ok := true
		for j, seg := range have {
			if strings.HasPrefix(seg, ":") {
				params[seg[1:]] = want[j]
			} else if seg != want[j] {
				ok = false
				break
			}
		}
		if ok {
			return r, params
		}
	}
	return nil, nil
}

// HasParams reports whether the route path contains parameters.
func (r *Route) HasParams() bool { return strings.Contains(r.Path, "/:") }

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}
