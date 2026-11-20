package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
)

// Partial reads and edits let an agent work on a 100-200 KB definition
// without sending or receiving all of it: an outline of names and sizes,
// the parts it asks for by path ("templates.css", "scripts.book"), and
// exact text replacements inside one entry.

// Outline lists what a definition holds, with sizes in bytes, so an agent
// can decide which parts to read.
type Outline struct {
	Bytes         int            `json:"bytes" jsonschema:"size of the whole definition as JSON"`
	Templates     map[string]int `json:"templates,omitempty" jsonschema:"template name to size in bytes; read one with only [\"templates.<name>\"]"`
	Scripts       map[string]int `json:"scripts,omitempty"`
	Lib           map[string]int `json:"lib,omitempty"`
	Tables        []TableOutline `json:"tables,omitempty"`
	Seed          map[string]int `json:"seed,omitempty" jsonschema:"table name to number of seed rows"`
	Routes        []string       `json:"routes,omitempty" jsonschema:"METHOD path -> template or script, with (auth) for routes that need a signed-in visitor"`
	Jobs          []string       `json:"jobs,omitempty"`
	Tools         []string       `json:"tools,omitempty"`
	Hosts         []string       `json:"hosts,omitempty"`
	OnPayment     string         `json:"on_payment,omitempty"`
	OnAdminChange string         `json:"on_admin_change,omitempty"`
}

// TableOutline is a table's name and column names.
type TableOutline struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

// OutlineOf summarises a definition.
func OutlineOf(def *appdef.App) Outline {
	raw, _ := json.Marshal(def)
	o := Outline{Bytes: len(raw), Templates: sizes(def.Templates), Scripts: sizes(def.Scripts), Lib: sizes(def.Lib),
		OnPayment: def.OnPayment, OnAdminChange: def.OnAdminChange}
	for _, t := range def.Tables {
		to := TableOutline{Name: t.Name, Columns: []string{}}
		for _, c := range t.Columns {
			to.Columns = append(to.Columns, c.Name+" "+c.Type)
		}
		o.Tables = append(o.Tables, to)
	}
	if len(def.Seed) > 0 {
		o.Seed = map[string]int{}
		for k, rows := range def.Seed {
			o.Seed[k] = len(rows)
		}
	}
	for _, r := range def.Routes {
		target := "script " + r.Script
		if r.Template != "" {
			target = "template " + r.Template
		}
		line := r.Method + " " + r.Path + " -> " + target
		if r.Auth != "" {
			line += " (auth " + r.Auth + ")"
		}
		o.Routes = append(o.Routes, line)
	}
	for _, j := range def.Jobs {
		o.Jobs = append(o.Jobs, j.Name+" -> script "+j.Script)
	}
	for _, t := range def.Tools {
		o.Tools = append(o.Tools, t.Name+" -> script "+t.Script)
	}
	for _, h := range def.Hosts {
		o.Hosts = append(o.Hosts, h.Host)
	}
	return o
}

func sizes(m map[string]string) map[string]int {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = len(v)
	}
	return out
}

// PartPaths explains the paths SelectParts accepts, for error messages and
// tool descriptions.
const PartPaths = `templates, templates.<name>, scripts, scripts.<name>, lib, lib.<name>, tables, tables.<name>, seed, seed.<table>, routes, jobs, tools, hosts, secrets, on_payment, on_admin_change, brand`

// MaxParts is how many paths one read may ask for.
const MaxParts = 40

// SelectParts returns the requested parts of a definition by path, e.g.
// {"templates.css": "...", "seed.texts": [...]}. Unknown paths are errors,
// so a typo is not mistaken for an empty part.
func SelectParts(def *appdef.App, only []string) (map[string]any, error) {
	if len(only) > MaxParts {
		return nil, fmt.Errorf("only: at most %d paths at once", MaxParts)
	}
	out := map[string]any{}
	var problems []string
	for _, path := range only {
		v, err := selectPart(def, strings.TrimSpace(path))
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		out[path] = v
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s (paths: %s)", strings.Join(problems, "; "), PartPaths)
	}
	return out, nil
}

func selectPart(def *appdef.App, path string) (any, error) {
	kind, name, named := strings.Cut(path, ".")
	entry := func(m map[string]string) (any, error) {
		if !named {
			return m, nil
		}
		v, ok := m[name]
		if !ok {
			return nil, fmt.Errorf("%s: no %s called %q (have: %s)", path, strings.TrimSuffix(kind, "s"), name, strings.Join(keys(m), ", "))
		}
		return v, nil
	}
	switch kind {
	case "templates":
		return entry(def.Templates)
	case "scripts":
		return entry(def.Scripts)
	case "lib":
		return entry(def.Lib)
	case "tables":
		if !named {
			return def.Tables, nil
		}
		for _, t := range def.Tables {
			if t.Name == name {
				return t, nil
			}
		}
		return nil, fmt.Errorf("%s: no table called %q", path, name)
	case "seed":
		if !named {
			return def.Seed, nil
		}
		rows, ok := def.Seed[name]
		if !ok {
			return nil, fmt.Errorf("%s: no seed rows for table %q", path, name)
		}
		return rows, nil
	}
	if named {
		return nil, fmt.Errorf("%s: %s has no named parts", path, kind)
	}
	switch kind {
	case "routes":
		return def.Routes, nil
	case "jobs":
		return def.Jobs, nil
	case "tools":
		return def.Tools, nil
	case "hosts":
		return def.Hosts, nil
	case "secrets":
		return def.Secrets, nil
	case "on_payment":
		return def.OnPayment, nil
	case "on_admin_change":
		return def.OnAdminChange, nil
	case "brand":
		return def.Brand, nil
	}
	return nil, fmt.Errorf("%q is not a part of a definition", path)
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Edit replaces one exact piece of text inside one template, script or lib
// entry.
type Edit struct {
	Target  string `json:"target" jsonschema:"templates.<name>, scripts.<name> or lib.<name>, e.g. templates.css, scripts.book, lib.dates; a bare name such as css means templates.<name> when that template exists, else scripts.<name>"`
	OldText string `json:"old_text" jsonschema:"exact text to replace, including spaces and line breaks; must occur exactly once in the target. Empty only to create a new entry"`
	NewText string `json:"new_text" jsonschema:"replacement text; empty deletes old_text"`
}

// MaxEdits is how many edits one call may make.
const MaxEdits = 50

// EditResult is ApplyResult plus what each edit changed.
type EditResult struct {
	ApplyResult
	Applied []string `json:"applied,omitempty" jsonschema:"one line per edit: the target and the line it changed"`
}

// ApplyEdits makes exact text replacements in the newest definition and
// applies the result to preview exactly like ApplyChange. All edits apply
// or none: any edit that does not match exactly once rejects the call.
// base must be the newest version, so an agent never edits over a change it
// has not seen.
func (p *Platform) ApplyEdits(ctx context.Context, app string, base int, edits []Edit) (EditResult, error) {
	if _, err := p.Info(app); err != nil {
		return EditResult{}, err
	}
	if len(edits) == 0 {
		return EditResult{ApplyResult: ApplyResult{Problems: []string{"edits is empty"}}}, nil
	}
	if len(edits) > MaxEdits {
		return EditResult{ApplyResult: ApplyResult{Problems: []string{fmt.Sprintf("at most %d edits per call", MaxEdits)}}}, nil
	}
	def, n, err := p.Definition(app, 0)
	if errors.Is(err, ErrNotFound) {
		return EditResult{ApplyResult: ApplyResult{Problems: []string{"the app has no definition yet: call create_app with from_example, or apply_change"}}}, nil
	} else if err != nil {
		return EditResult{}, err
	}
	if base != n {
		return EditResult{ApplyResult: ApplyResult{Problems: []string{fmt.Sprintf("base_version must be the newest version, %d (you sent %d): read what changed with get_app {only: [...]} and edit that", n, base)}}}, nil
	}
	applied, problems := editDefinition(def, edits)
	if len(problems) > 0 {
		return EditResult{ApplyResult: ApplyResult{Problems: problems}}, nil
	}
	res, err := p.ApplyChange(ctx, app, def)
	return EditResult{ApplyResult: res, Applied: applied}, err
}

// editDefinition applies edits to def in order; later edits see earlier
// ones. It reports every edit that does not apply.
func editDefinition(def *appdef.App, edits []Edit) (applied, problems []string) {
	if def.Templates == nil {
		def.Templates = map[string]string{}
	}
	if def.Scripts == nil {
		def.Scripts = map[string]string{}
	}
	if def.Lib == nil {
		def.Lib = map[string]string{}
	}
	for i, e := range edits {
		label := fmt.Sprintf("edits[%d] (%s)", i, e.Target)
		kind, name, ok := strings.Cut(e.Target, ".")
		if !ok { // a bare name: a template if there is one, else a script
			kind, name = "templates", e.Target
			if _, isTemplate := def.Templates[name]; !isTemplate {
				if _, isScript := def.Scripts[name]; isScript {
					kind = "scripts"
				}
			}
		}
		var m map[string]string
		switch kind {
		case "templates":
			m = def.Templates
		case "scripts":
			m = def.Scripts
		case "lib":
			m = def.Lib
		default:
			problems = append(problems, label+": target must be templates.<name>, scripts.<name> or lib.<name>")
			continue
		}
		cur, exists := m[name]
		switch {
		case name == "":
			problems = append(problems, label+": the target needs a name")
			continue
		case e.OldText == "" && exists && cur == "":
			m[name] = e.NewText // an empty slot such as css_extra
			applied = append(applied, fmt.Sprintf("%s.%s: filled (%d bytes)", kind, name, len(e.NewText)))
			continue
		case e.OldText == "" && exists:
			problems = append(problems, label+": old_text is empty but "+kind+"."+name+" exists; give the exact text to replace")
			continue
		case e.OldText == "":
			m[name] = e.NewText
			applied = append(applied, fmt.Sprintf("%s.%s: created (%d bytes)", kind, name, len(e.NewText)))
			continue
		case !exists:
			problems = append(problems, fmt.Sprintf("%s: there is no %s.%s (have: %s)", label, kind, name, strings.Join(keys(m), ", ")))
			continue
		}
		switch c := strings.Count(cur, e.OldText); c {
		case 1:
			at := strings.Index(cur, e.OldText)
			m[name] = cur[:at] + e.NewText + cur[at+len(e.OldText):]
			applied = append(applied, fmt.Sprintf("%s.%s: line %d changed", kind, name, strings.Count(cur[:at], "\n")+1))
		case 0:
			problems = append(problems, label+": old_text was not found; it must match exactly, spaces and line breaks included"+nearMiss(cur, e.OldText))
		default:
			problems = append(problems, fmt.Sprintf("%s: old_text occurs %d times; include more of the surrounding text so it occurs once", label, c))
		}
	}
	return applied, problems
}

// nearMiss points at a line that matches old_text's first line when only
// spacing differs, the usual reason an edit misses.
func nearMiss(text, old string) string {
	first := strings.TrimSpace(strings.SplitN(old, "\n", 2)[0])
	if first == "" {
		return ""
	}
	squash := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	want := squash(first)
	for i, line := range strings.Split(text, "\n") {
		if strings.Contains(squash(line), want) {
			l := strings.TrimSpace(line)
			if len(l) > 160 {
				l = l[:160] + "…"
			}
			return fmt.Sprintf(" (line %d is similar: %q)", i+1, l)
		}
	}
	return ""
}

// CreateFromExample creates an app and applies a finished example to its
// preview, so an agent never has to send an example's definition back.
func (p *Platform) CreateFromExample(ctx context.Context, owner, name, example string) (AppInfo, ApplyResult, error) {
	e, ok := examples.Get(example)
	if !ok {
		names := []string{}
		for _, x := range examples.All() {
			names = append(names, x.Name)
		}
		return AppInfo{}, ApplyResult{}, fmt.Errorf("no example called %q; choose one of: %s", example, strings.Join(names, ", "))
	}
	def, problems := appdef.Parse(e.JSON)
	if len(problems) > 0 {
		return AppInfo{}, ApplyResult{}, fmt.Errorf("example %s: %s", example, strings.Join(problems, "; "))
	}
	info, err := p.CreateApp(owner, name)
	if err != nil {
		return AppInfo{}, ApplyResult{}, err
	}
	res, err := p.ApplyChange(ctx, name, def)
	if err != nil {
		return info, res, err
	}
	info, err = p.Info(name)
	return info, res, err
}
