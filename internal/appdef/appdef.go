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
}

// Table is a tenant database table.
type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
}

// Column is a table column. Type is one of text, integer, real, boolean,
// datetime, json or image; an image column holds the URL of an uploaded file.
type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type" jsonschema:"one of: text, integer, real, boolean, datetime, json, image (holds the url returned by upload_file)"`
	Required bool   `json:"required,omitempty"`
	Unique   bool   `json:"unique,omitempty"`
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

// ColumnTypes maps definition types to SQLite column types.
var ColumnTypes = map[string]string{
	"text": "TEXT", "integer": "INTEGER", "real": "REAL",
	"boolean": "INTEGER", "datetime": "TEXT", "json": "TEXT", "image": "TEXT",
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
				add("table %q: column %q: unknown type %q (use text, integer, real, boolean, datetime, json or image)", t.Name, c.Name, c.Type)
			}
		}
	}
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

	if a.OnPayment != "" {
		if _, ok := a.Scripts[a.OnPayment]; !ok {
			add("on_payment: script %q is not defined", a.OnPayment)
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
