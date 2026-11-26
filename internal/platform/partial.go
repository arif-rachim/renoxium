package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
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
	return p.CreateFromTemplate(ctx, owner, name, example, "")
}

// CreateFromTemplate is CreateFromExample with the business's name put in
// the template's settings row (its brand name, English and Arabic), so the
// preview already carries it; "" keeps the template's sample name.
func (p *Platform) CreateFromTemplate(ctx context.Context, owner, name, example, business string) (AppInfo, ApplyResult, error) {
	return p.CreateCustomized(ctx, owner, name, example, Customize{Business: business})
}

// Customize is the business's own data for a new app from an example, so
// one call makes the app with its name, settings and products in place.
type Customize struct {
	Business string                      // the business's name, put in the settings row's name columns
	Settings map[string]any              // columns of the settings row (the example's brand table) to set
	Rows     map[string][]map[string]any // tables whose starting rows are replaced, e.g. products
	// ForBusiness makes the app for a real business: the example's own
	// legal name, address and contacts are cleared, and what else is still
	// the example's is reported (see Leftovers).
	ForBusiness bool
}

// Leftovers is what a new app still has from its example, for the
// assistant to replace with the owner's own or to ask about.
type Leftovers struct {
	Cleared    []string       `json:"cleared_from_example,omitempty" jsonschema:"the example business's legal name, address and contacts, cleared because the call did not give the owner's; ask the owner for theirs and set them in the settings row"`
	Kept       []string       `json:"kept_from_example,omitempty" jsonschema:"settings the example still provides (column: value): texts and photos of the example business; ask the owner, then set them in the settings row"`
	SampleRows map[string]int `json:"sample_rows_from_example,omitempty" jsonschema:"tables still holding the example's sample rows (table: number of rows), e.g. its products or reviews; replace them with the owner's or remove them"`
}

// identityColumnRE names the settings columns that identify the example's
// business: never left for another business.
var identityColumnRE = regexp.MustCompile(`^(legal_name|address|whatsapp|phone|mobile|email|licen[cs]e|trn|tax_number|vat_number|bank_details|iban|office_reg_value|lat|lng|instagram|facebook|tiktok|website|map_url)(_ar)?$`)

// leftovers clears def's settings row of the example's identity (unless
// set) and lists what else is the example's.
func (c Customize) leftovers(def *appdef.App, set map[string]bool) Leftovers {
	var out Leftovers
	if def.Brand == nil || len(def.Seed[def.Brand.Table]) == 0 {
		return out
	}
	row := def.Seed[def.Brand.Table][0]
	var settings appdef.Table
	for _, t := range def.Tables {
		if t.Name == def.Brand.Table {
			settings = t
		}
	}
	for _, col := range settings.Columns {
		v, ok := row[col.Name]
		if !ok || set[col.Name] || v == nil || fmt.Sprint(v) == "" {
			continue
		}
		if c.Business != "" && (col.Name == def.Brand.Name || col.Name == def.Brand.NameAr) {
			continue
		}
		switch {
		case identityColumnRE.MatchString(col.Name):
			row[col.Name] = ""
			out.Cleared = append(out.Cleared, col.Name)
		case (col.Type == "text" || col.Type == "image") && col.Name != "font" && col.Name != def.Brand.Color && len(col.Options) == 0:
			text := fmt.Sprint(v)
			if r := []rune(text); len(r) > 60 {
				text = string(r[:57]) + "..."
			}
			out.Kept = append(out.Kept, col.Name+": "+text)
		}
	}
	for table, rows := range def.Seed {
		if table == def.Brand.Table || table == "texts" || len(rows) == 0 || c.Rows[table] != nil {
			continue
		}
		if out.SampleRows == nil {
			out.SampleRows = map[string]int{}
		}
		out.SampleRows[table] = len(rows)
	}
	return out
}

// customize puts c into def's seed rows. Every table and column must exist,
// so a mistake is reported before an app is made.
func (c Customize) apply(def *appdef.App) error {
	tables := map[string]appdef.Table{}
	for _, t := range def.Tables {
		tables[t.Name] = t
	}
	check := func(table string, row map[string]any) error {
		t := tables[table]
		var names []string
		for _, col := range t.Columns {
			names = append(names, col.Name)
		}
		for k := range row {
			if !slices.Contains(names, k) {
				return fmt.Errorf("%s has no column %q; its columns are: %s", table, k, strings.Join(names, ", "))
			}
		}
		return nil
	}
	if len(c.Settings) > 0 {
		if def.Brand == nil || len(def.Seed[def.Brand.Table]) == 0 {
			return errors.New("this example has no settings row; set its data with edit_app")
		}
		if err := check(def.Brand.Table, c.Settings); err != nil {
			return fmt.Errorf("settings: %w", err)
		}
		for k, v := range c.Settings {
			def.Seed[def.Brand.Table][0][k] = v
		}
	}
	for table, rows := range c.Rows {
		if _, ok := tables[table]; !ok {
			var names []string
			for _, t := range def.Tables {
				names = append(names, t.Name)
			}
			return fmt.Errorf("rows: the example has no table %q; its tables are: %s", table, strings.Join(names, ", "))
		}
		if def.Brand != nil && table == def.Brand.Table {
			return fmt.Errorf("rows: %s is the settings row; give its values in settings", table)
		}
		for i, row := range rows {
			if err := check(table, row); err != nil {
				return fmt.Errorf("rows %s[%d]: %w", table, i, err)
			}
		}
		if def.Seed == nil {
			def.Seed = map[string][]map[string]any{}
		}
		def.Seed[table] = rows
	}
	return nil
}

// CreateCustomized creates an app from an example with the business's own
// data (see Customize) and applies it to the preview, checked like
// apply_change.
func (p *Platform) CreateCustomized(ctx context.Context, owner, name, example string, c Customize) (AppInfo, ApplyResult, error) {
	info, res, _, err := p.CreateForBusiness(ctx, owner, name, example, c)
	return info, res, err
}

// CreateForBusiness is CreateCustomized that also says what the new app
// still has from its example (with c.ForBusiness).
func (p *Platform) CreateForBusiness(ctx context.Context, owner, name, example string, c Customize) (AppInfo, ApplyResult, Leftovers, error) {
	var left Leftovers
	business := c.Business
	e, ok := examples.Get(example)
	if !ok {
		names := []string{}
		for _, x := range examples.All() {
			names = append(names, x.Name)
		}
		return AppInfo{}, ApplyResult{}, left, fmt.Errorf("no example called %q; choose one of: %s", example, strings.Join(names, ", "))
	}
	def, problems := appdef.Parse(e.JSON)
	if len(problems) > 0 {
		return AppInfo{}, ApplyResult{}, left, fmt.Errorf("example %s: %s", example, strings.Join(problems, "; "))
	}
	if business = strings.TrimSpace(business); business != "" && def.Brand != nil {
		if rows := def.Seed[def.Brand.Table]; len(rows) > 0 {
			rows[0][def.Brand.Name] = business
			if def.Brand.NameAr != "" {
				rows[0][def.Brand.NameAr] = business // until the owner's assistant writes it in Arabic
			}
		}
	}
	plain, _ := appdef.Parse(e.JSON) // the example with only the business's name, if the data is refused
	if plain.Brand != nil && def.Brand != nil {
		if rows, ok := def.Seed[def.Brand.Table]; ok && len(rows) > 0 && len(plain.Seed[plain.Brand.Table]) > 0 {
			plain.Seed[plain.Brand.Table][0][def.Brand.Name] = rows[0][def.Brand.Name]
			if def.Brand.NameAr != "" {
				plain.Seed[plain.Brand.Table][0][def.Brand.NameAr] = rows[0][def.Brand.NameAr]
			}
		}
	}
	custom := len(c.Settings) > 0 || len(c.Rows) > 0
	if err := c.apply(def); err != nil {
		return AppInfo{}, ApplyResult{}, left, err
	}
	if c.ForBusiness {
		set := map[string]bool{}
		for k := range c.Settings {
			set[k] = true
		}
		left = c.leftovers(def, set)
		Customize{Business: business}.leftovers(plain, nil) // the fallback never keeps them either
	}
	info, err := p.CreateApp(owner, name)
	if err != nil {
		return AppInfo{}, ApplyResult{}, left, err
	}
	res, err := p.ApplyChange(ctx, name, def)
	if err != nil {
		return info, res, left, err
	}
	if !res.OK && custom {
		// The app exists now: give it the example, and say what was refused.
		problems := res.Problems
		if res, err = p.ApplyChange(ctx, name, plain); err != nil {
			return info, res, left, err
		}
		info, _ = p.Info(name)
		return info, res, left, fmt.Errorf("app %s was created from %s, but its settings and rows were refused: %s; set them with edit_app (base_version %d)",
			name, example, strings.Join(problems, "; "), res.Version)
	}
	info, err = p.Info(name)
	return info, res, left, err
}
