package platform

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// After a change is applied, every GET route of the app is rendered once on
// preview, so template and script errors show up for the agent rather than
// for the first visitor:
//
//   - routes with parameters (/s/:id, /bookings/:id) get values from the
//     first row of the table the path names (s -> services, bookings ->
//     bookings, :booking_id -> bookings), from seed rows or preview data;
//   - routes with "auth": "user" are rendered signed in, as the visitor who
//     owns that row (its user_id) or else as a throwaway check visitor;
//   - an HTML page that shows "<no value>" (a missing key in a template) is
//     reported as a warning with the text around it.
//
// Checks only read the preview: a GET script that writes changes preview
// data, which the next change replaces anyway.

// maxCheckedRoutes caps the routes rendered per change.
const maxCheckedRoutes = 60

func (p *Platform) checkRoutes(ctx context.Context, st *stage, def *appdef.App) []Check {
	checks, _ := p.checkRoutesSeen(ctx, st, def)
	return checks
}

// checkRoutesSeen is checkRoutes that also says which templates the checked
// pages rendered; nil when that is not known.
func (p *Platform) checkRoutesSeen(ctx context.Context, st *stage, def *appdef.App) (checks []Check, seen map[string]bool) {
	if strict := newStrictChecker(def); strict != nil {
		ctx = context.WithValue(ctx, strictKey{}, strict)
		seen = strict.seen
	}
	var checkUser int64 // a throwaway visitor for signed-in routes, made on first need
	defer func() {
		if checkUser != 0 {
			st.admin.Exec(`DELETE FROM rx_sessions WHERE user_id = ?`, checkUser)
			st.admin.Exec(`DELETE FROM rx_users WHERE id = ?`, checkUser)
		}
	}()
	for _, r := range def.Routes {
		if r.Method != http.MethodGet {
			continue
		}
		if len(checks) == maxCheckedRoutes {
			checks = append(checks, Check{Route: "…", Note: fmt.Sprintf("only the first %d GET routes are checked", maxCheckedRoutes)})
			break
		}
		c := Check{Route: "GET " + r.Path}
		candidates := []filled{{path: r.Path}}
		if r.HasParams() {
			candidates = p.fillParams(ctx, st, def, r.Path)
			if len(candidates) == 0 {
				c.Note = "not rendered: no row found to fill its parameters (add seed rows to check it)"
				checks = append(checks, c)
				continue
			}
		}
		for i, cand := range candidates {
			as := cand.user
			if r.Auth == "user" && as == 0 {
				if checkUser == 0 {
					checkUser = p.makeCheckVisitor(ctx, st)
				}
				as = checkUser
			}
			out := p.checkOnce(ctx, st, cand.path, as)
			c.Status, c.Error, c.Path, c.Warnings = out.status, out.errMsg, "", nil
			if cand.path != r.Path {
				c.Path = cand.path
			}
			c.As = ""
			if r.Auth == "user" || cand.user != 0 {
				if as == checkUser {
					c.As = "a signed-in check visitor"
				} else {
					c.As = fmt.Sprintf("visitor %d, who owns the row", as)
				}
			}
			c.Warnings = out.warnings
			if strings.HasPrefix(out.contentType, "text/html") {
				c.Warnings = append(c.Warnings, noValueWarnings(string(out.body))...)
			}
			// Another row may fit when this one is not found (a row of a
			// different table, or one this visitor may not see).
			if out.status != http.StatusNotFound || i == len(candidates)-1 {
				break
			}
		}
		if c.Status == http.StatusNotFound && r.HasParams() {
			c.Note = "the page answered 404 for the rows tried; check it with view_page if the parameter is not a row id"
		}
		checks = append(checks, c)
	}
	return checks, seen
}

// filled is a route path with its parameters filled in, and the visitor
// who owns the row used, if any.
type filled struct {
	path string
	user int64
}

// fillParams returns up to three ways to fill a path's parameters, best
// first.
func (p *Platform) fillParams(ctx context.Context, st *stage, def *appdef.App, path string) []filled {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	var out []filled
	for attempt := 0; attempt < 3; attempt++ {
		vals := make([]string, len(segs))
		copy(vals, segs)
		var user int64
		ok := true
		for i, seg := range segs {
			if !strings.HasPrefix(seg, ":") {
				continue
			}
			param := seg[1:]
			stem := strings.TrimSuffix(param, "_id")
			if param == "id" || stem == param {
				if i > 0 && !strings.HasPrefix(segs[i-1], ":") {
					stem = segs[i-1]
				}
			}
			tables := matchTables(def, stem)
			// /services/by/:slug: an earlier segment names the table.
			for k := i - 2; len(tables) == 0 && k >= 0; k-- {
				if !strings.HasPrefix(segs[k], ":") {
					tables = matchTables(def, segs[k])
				}
			}
			// :slug, :code: a table with a column of that name.
			if len(tables) == 0 && param != "id" && !strings.HasSuffix(param, "_id") {
				for _, t := range def.Tables {
					for _, c := range t.Columns {
						if c.Name == param {
							tables = append(tables, t.Name)
						}
					}
				}
			}
			if attempt >= len(tables) {
				ok = false
				break
			}
			v, owner := firstRowValue(ctx, st.admin, def, tables[attempt], param)
			if v == "" {
				ok = false
				break
			}
			vals[i] = url.PathEscape(v)
			if owner != 0 {
				user = owner
			}
		}
		if !ok {
			break
		}
		out = append(out, filled{path: "/" + strings.Join(vals, "/"), user: user})
	}
	return out
}

// matchTables orders the app's tables by how well their name fits a path
// segment: services for "services", "service" or "s"; crews for "crew".
func matchTables(def *appdef.App, stem string) []string {
	stem = strings.ToLower(stem)
	type scored struct {
		name  string
		score int
	}
	var all []scored
	for _, t := range def.Tables {
		n, score := t.Name, 0
		switch {
		case n == stem:
			score = 4
		case n == stem+"s" || n == stem+"es" || strings.TrimSuffix(stem, "y")+"ies" == n:
			score = 3
		case strings.HasPrefix(n, stem) && stem != "":
			score = 1
		}
		if score > 0 {
			all = append(all, scored{n, score})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = s.name
	}
	return out
}

// firstRowValue reads the parameter's value from the table's first row:
// its id for :id and :<x>_id, else the column named like the parameter.
// owner is the row's user_id when the table has one.
func firstRowValue(ctx context.Context, db *sql.DB, def *appdef.App, table, param string) (value string, owner int64) {
	col := param
	if param == "id" || strings.HasSuffix(param, "_id") {
		col = "id"
	}
	hasUser := false
	known := col == "id"
	for _, t := range def.Tables {
		if t.Name != table {
			continue
		}
		for _, c := range t.Columns {
			known = known || c.Name == col
			hasUser = hasUser || c.Name == "user_id"
		}
	}
	if !known {
		return "", 0
	}
	sel := `CAST("` + col + `" AS TEXT), 0`
	if hasUser {
		sel = `CAST("` + col + `" AS TEXT), coalesce(user_id, 0)`
	}
	var v sql.NullString
	db.QueryRowContext(ctx, `SELECT `+sel+` FROM "`+table+`" WHERE "`+col+`" IS NOT NULL ORDER BY id LIMIT 1`).Scan(&v, &owner)
	if owner != 0 {
		var exists int
		if db.QueryRowContext(ctx, `SELECT 1 FROM rx_users WHERE id = ?`, owner).Scan(&exists) != nil {
			owner = 0
		}
	}
	return v.String, owner
}

// makeCheckVisitor adds a visitor to the preview for the length of the
// checks.
func (p *Platform) makeCheckVisitor(ctx context.Context, st *stage) int64 {
	res, err := st.admin.ExecContext(ctx, `INSERT INTO rx_users (email, name, verified_at) VALUES (?, 'Check Visitor', ?)`,
		"check-"+strings.ToLower(secretToken()[:10])+"@example.com", now())
	if err != nil {
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

// checkOnce renders one path on preview, signed in as user when not 0.
func (p *Platform) checkOnce(ctx context.Context, st *stage, path string, user int64) result {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if user != 0 {
		token := secretToken()
		if _, err := st.admin.ExecContext(ctx, `INSERT INTO rx_sessions (hash, user_id, expires_at) VALUES (?, ?, ?)`, sha(token), user, time.Now().Add(time.Minute).Unix()); err == nil {
			defer st.admin.Exec(`DELETE FROM rx_sessions WHERE hash = ?`, sha(token))
			req.AddCookie(&http.Cookie{Name: p.cookieName(visitorSessionCookie), Value: token})
		}
	}
	return p.handle(st, req, newRequestID())
}

// strictKey carries, while a change is checked, a strictChecker: the app's
// templates set to fail on a missing map key, so a page that silently
// renders an empty value for a key its data lacks is reported.
type strictKey struct{}

// missingKeyRE picks the parts of a missing-key error:
// `template: service:4:20: executing "service" at <.s.price>: map has no entry for key "price"`.
var missingKeyRE = regexp.MustCompile(`template: ([^:]+):(\d+):(\d+): executing "[^"]*" at <([^>]*)>: map has no entry for key "([^"]*)"`)

// strictChecker finds keys a template prints that its data does not have.
// Missing keys in if, with and range are how templates handle optional
// values, so those are rewritten to read the key leniently and the page is
// rendered again, until it renders or a printed key is missing. Rewrites
// are kept for the next pages of the same check.
type strictChecker struct {
	src  map[string]string
	tmpl *template.Template
	seen map[string]bool // templates that ran to their end on a checked page
}

const maxStrictPasses = 40

func newStrictChecker(def *appdef.App) *strictChecker {
	c := &strictChecker{src: map[string]string{}, seen: map[string]bool{}}
	for k, v := range def.Templates {
		c.src[k] = v
	}
	if c.parse() != nil {
		return nil
	}
	return c
}

func (c *strictChecker) parse() error {
	root := template.New("").Funcs(template.FuncMap{"rxget": lenientGet,
		"rxseen": func(n string) string { c.seen[n] = true; return "" }}).Option("missingkey=error")
	for n, src := range c.src {
		// A mark at the end, so line and column numbers in errors stay the
		// template's own: it tells which templates the checked pages render.
		t, err := root.New(n).Parse(src + `{{rxseen "` + strings.ReplaceAll(n, `"`, ``) + `"}}`)
		if err != nil {
			return err
		}
		t.Option("missingkey=error")
	}
	c.tmpl = root
	return nil
}

// lenientGet reads keys from nested maps, nil when one is missing.
func lenientGet(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		if v, ok = m[k]; !ok {
			return nil
		}
	}
	return v
}

func (c *strictChecker) missing(name string, data any) []string {
	var warns []string
	for pass := 0; pass < maxStrictPasses; pass++ {
		t := c.tmpl.Lookup(name)
		if t == nil {
			return warns
		}
		err := t.Execute(io.Discard, data)
		if err == nil {
			return warns
		}
		m := missingKeyRE.FindStringSubmatch(err.Error())
		if m == nil {
			return warns
		}
		tname, node, key := m[1], m[4], m[5]
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		src, ok := c.src[tname]
		if !ok {
			return warns
		}
		off := nodeAt(src, line, col, node)
		if off < 0 {
			return warns
		}
		if !inCondition(src, off) {
			warns = append(warns, fmt.Sprintf("%s:%d:%d: {{%s}} prints nothing because the data has no key %q (check the spelling or the data the script returns)", tname, line, col, node, key))
			if len(warns) == 3 {
				return warns
			}
		}
		repl, ok := lenientCall(node)
		if !ok {
			return warns
		}
		c.src[tname] = src[:off] + repl + src[off+len(node):]
		if c.parse() != nil {
			return warns
		}
	}
	return warns
}

// nodeAt finds node in src at the line and column of an execution error;
// the column text/template reports can be off by one, so the nearest
// occurrence on that line is taken.
func nodeAt(src string, line, col int, node string) int {
	start := lineOffset(src, line)
	if start < 0 {
		return -1
	}
	end := strings.IndexByte(src[start:], '\n')
	if end < 0 {
		end = len(src) - start
	}
	text, best := src[start:start+end], -1
	for i := 0; ; {
		j := strings.Index(text[i:], node)
		if j < 0 {
			break
		}
		j += i
		if best < 0 || abs(j-(col-1)) < abs(best-(col-1)) {
			best = j
		}
		i = j + 1
	}
	if best < 0 {
		return -1
	}
	return start + best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func lineOffset(src string, line int) int {
	off := 0
	for l := 1; l < line; l++ {
		i := strings.IndexByte(src[off:], '\n')
		if i < 0 {
			return -1
		}
		off += i + 1
	}
	return off
}

// inCondition reports whether the action around off starts with if, with,
// range or else, or assigns a variable: places where a missing key is an
// optional value, not a mistake.
func inCondition(src string, off int) bool {
	start := strings.LastIndex(src[:off], "{{")
	if start < 0 {
		return false
	}
	head := strings.TrimLeft(src[start+2:off], "- \t\n")
	for _, w := range []string{"if ", "with ", "range ", "else "} {
		if strings.HasPrefix(head, w) {
			return true
		}
	}
	return strings.Contains(head, ":=") || strings.Contains(head, "=")
}

// lenientCall turns a field chain such as .s.price or $x.name into a call
// of rxget that gives nil instead of failing.
func lenientCall(node string) (string, bool) {
	base, rest := ".", node
	switch {
	case strings.HasPrefix(node, "$."):
		base, rest = "$", node[1:]
	case strings.HasPrefix(node, "$"):
		i := strings.IndexByte(node, '.')
		if i < 0 {
			return "", false
		}
		base, rest = node[:i], node[i:]
	case !strings.HasPrefix(node, "."):
		return "", false
	}
	var keys []string
	for _, k := range strings.Split(strings.TrimPrefix(rest, "."), ".") {
		if k == "" {
			return "", false
		}
		keys = append(keys, strconv.Quote(k))
	}
	return "(rxget " + base + " " + strings.Join(keys, " ") + ")", true
}

// checkMissingKeys renders a page that rendered once more with the strict
// templates of a check, and notes printed keys its data lacks. Outside
// checks it returns out as it is.
func checkMissingKeys(ctx context.Context, out result, name string, data any) result {
	c, _ := ctx.Value(strictKey{}).(*strictChecker)
	if c == nil || out.errMsg != "" {
		return out
	}
	out.warnings = append(out.warnings, c.missing(name, data)...)
	return out
}

// noValueWarnings finds "<no value>" in a rendered page: a template read a
// key its data does not have.
func noValueWarnings(page string) []string {
	const marker = "&lt;no value&gt;"
	var out []string
	for at, n := 0, 0; ; n++ {
		i := strings.Index(page[at:], marker)
		if i < 0 {
			break
		}
		i += at
		if n < 3 {
			from := max(0, i-80)
			before := strings.Join(strings.Fields(page[from:i]), " ")
			out = append(out, fmt.Sprintf("<no value> on the page (a template reads a key its data does not have), after: …%s", before))
		}
		at = i + len(marker)
		if n == 2 {
			if rest := strings.Count(page[at:], marker); rest > 0 {
				out = append(out, fmt.Sprintf("and %d more <no value>", rest))
			}
			break
		}
	}
	// Outside text, html/template writes it unescaped (attributes, scripts).
	if len(out) == 0 && strings.Contains(page, "<no value>") {
		out = append(out, "<no value> on the page (a template reads a key its data does not have)")
	}
	return out
}
